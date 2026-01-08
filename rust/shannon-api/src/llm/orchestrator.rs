//! LLM orchestrator with tool loop execution.
//!
//! The orchestrator manages the complete lifecycle of an LLM interaction:
//! 1. Send user message to the LLM
//! 2. Stream the response, detecting tool calls
//! 3. Execute tool calls via configured tool registry
//! 4. Feed tool results back to the LLM
//! 5. Repeat until the model produces a final response

use std::collections::BTreeMap;
use std::sync::Arc;

use futures::{Stream, StreamExt};

use crate::events::{NormalizedEvent, StreamEvent, ToolCallAccumulator};
use crate::llm::providers::create_driver;
use crate::llm::{LlmDriver, LlmRequest, LlmSettings, Message, ToolCall, ToolCallFunction};
use crate::tools::ToolRegistry;

/// Maximum number of tool loop iterations to prevent infinite loops.
const MAX_TOOL_ITERATIONS: usize = 10;

/// LLM orchestrator with tool loop execution.
#[derive(Clone)]
pub struct Orchestrator {
    settings: LlmSettings,
    driver: Arc<dyn LlmDriver>,
    tools: Arc<ToolRegistry>,
    max_iterations: usize,
}

impl std::fmt::Debug for Orchestrator {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Orchestrator")
            .field("settings", &self.settings)
            .field("max_iterations", &self.max_iterations)
            .finish()
    }
}

impl Orchestrator {
    /// Create a new orchestrator with the given settings and tool registry.
    pub fn new(settings: LlmSettings, tools: Arc<ToolRegistry>) -> Self {
        let driver = create_driver(settings.clone());

        Self {
            settings,
            driver,
            tools,
            max_iterations: MAX_TOOL_ITERATIONS,
        }
    }

    /// Create a new orchestrator with a custom max iterations limit.
    pub fn with_max_iterations(mut self, max: usize) -> Self {
        self.max_iterations = max;
        self
    }

    /// Get the LLM settings.
    pub fn settings(&self) -> &LlmSettings {
        &self.settings
    }

    /// Get the tool registry.
    pub fn tools(&self) -> &ToolRegistry {
        &self.tools
    }

    /// Stream a chat response with automatic tool execution.
    ///
    /// This method handles the complete tool loop:
    /// 1. Streams LLM response
    /// 2. Detects and accumulates tool calls
    /// 3. Executes tools when complete
    /// 4. Feeds results back to LLM
    /// 5. Repeats until done
    pub async fn chat(
        &self,
        messages: Vec<Message>,
    ) -> anyhow::Result<impl Stream<Item = StreamEvent> + Send + 'static + use<>> {
        self.chat_with_tools(messages, self.tools.get_tool_schemas()).await
    }

    /// Stream a chat response with specific tools.
    pub async fn chat_with_tools(
        &self,
        messages: Vec<Message>,
        tools: Vec<serde_json::Value>,
    ) -> anyhow::Result<impl Stream<Item = StreamEvent> + Send + 'static + use<>> {
        let driver = self.driver.clone();
        let tool_registry = self.tools.clone();
        let max_iterations = self.max_iterations;

        let stream = async_stream::stream! {
            let mut conversation = messages;
            let mut iteration = 0;
            let mut seq = 0u64;

            loop {
                if iteration >= max_iterations {
                    yield StreamEvent::new(seq, NormalizedEvent::error(
                        format!("Maximum tool iterations ({}) exceeded", max_iterations)
                    ));
                    seq += 1;
                    yield StreamEvent::new(seq, NormalizedEvent::done());
                    break;
                }

                // Create request
                let req = LlmRequest::new(conversation.clone()).with_tools(tools.clone());

                // Stream response
                let response_stream = match driver.stream(req).await {
                    Ok(s) => s,
                    Err(e) => {
                        yield StreamEvent::new(seq, NormalizedEvent::error(e.to_string()));
                        seq += 1;
                        yield StreamEvent::new(seq, NormalizedEvent::done());
                        break;
                    }
                };

                // Collect response and detect tool calls
                let mut content_buffer = String::new();
                let mut tool_accumulators: BTreeMap<usize, ToolCallAccumulator> = BTreeMap::new();
                let mut has_tool_calls = false;
                let mut is_done = false;

                futures::pin_mut!(response_stream);

                while let Some(event_result) = response_stream.next().await {
                    match event_result {
                        Ok(event) => {
                            match &event {
                                NormalizedEvent::MessageDelta { content, .. } => {
                                    content_buffer.push_str(content);
                                }
                                NormalizedEvent::ToolCallDelta { index, id, name, arguments } => {
                                    has_tool_calls = true;
                                    let acc = tool_accumulators.entry(*index).or_default();
                                    acc.apply_delta(id.clone(), name.clone(), arguments.clone());
                                }
                                NormalizedEvent::Done { .. } => {
                                    is_done = true;
                                }
                                _ => {}
                            }

                            // Forward all events
                            yield StreamEvent::new(seq, event);
                            seq += 1;
                        }
                        Err(e) => {
                            yield StreamEvent::new(seq, NormalizedEvent::error(e.to_string()));
                            seq += 1;
                        }
                    }
                }

                // If no tool calls, we're done
                if !has_tool_calls {
                    if !is_done {
                        yield StreamEvent::new(seq, NormalizedEvent::done());
                    }
                    break;
                }

                // Build complete tool calls
                let tool_calls: Vec<ToolCall> = tool_accumulators
                    .into_values()
                    .filter_map(|acc| {
                        if let (Some(id), Some(name)) = (acc.id, acc.name) {
                            Some(ToolCall {
                                id,
                                call_type: "function".to_string(),
                                function: ToolCallFunction {
                                    name,
                                    arguments: acc.arguments,
                                },
                            })
                        } else {
                            None
                        }
                    })
                    .collect();

                if tool_calls.is_empty() {
                    if !is_done {
                        yield StreamEvent::new(seq, NormalizedEvent::done());
                    }
                    break;
                }

                // Emit tool call complete events
                for tc in &tool_calls {
                    yield StreamEvent::new(seq, NormalizedEvent::ToolCallComplete {
                        id: tc.id.clone(),
                        name: tc.function.name.clone(),
                        arguments: tc.function.arguments.clone(),
                    });
                    seq += 1;
                }

                // Add assistant message with tool calls
                conversation.push(Message {
                    role: crate::llm::MessageRole::Assistant,
                    content: crate::llm::MessageContent::Text(content_buffer.clone()),
                    tool_call_id: None,
                    tool_calls: Some(tool_calls.clone()),
                });

                // Execute tools and add results
                for tc in tool_calls {
                    let result = tool_registry.execute(&tc.function.name, &tc.function.arguments).await;

                    let (content, success) = match result {
                        Ok(output) => (output, true),
                        Err(e) => (format!("Tool error: {}", e), false),
                    };

                    // Emit tool result event
                    yield StreamEvent::new(seq, NormalizedEvent::ToolResult {
                        tool_call_id: tc.id.clone(),
                        name: tc.function.name.clone(),
                        content: content.clone(),
                        success,
                    });
                    seq += 1;

                    // Add tool result to conversation
                    conversation.push(Message::tool_result(tc.id, content));
                }

                iteration += 1;
            }
        };

        Ok(stream)
    }

    /// Simple one-shot chat without tool execution.
    pub async fn chat_simple(
        &self,
        messages: Vec<Message>,
    ) -> anyhow::Result<impl Stream<Item = StreamEvent>> {
        let driver = self.driver.clone();

        let stream = async_stream::stream! {
            let req = LlmRequest::new(messages);
            let mut seq = 0u64;

            match driver.stream(req).await {
                Ok(response_stream) => {
                    futures::pin_mut!(response_stream);

                    while let Some(event_result) = response_stream.next().await {
                        match event_result {
                            Ok(event) => {
                                yield StreamEvent::new(seq, event);
                                seq += 1;
                            }
                            Err(e) => {
                                yield StreamEvent::new(seq, NormalizedEvent::error(e.to_string()));
                                seq += 1;
                            }
                        }
                    }
                }
                Err(e) => {
                    yield StreamEvent::new(seq, NormalizedEvent::error(e.to_string()));
                    seq += 1;
                    yield StreamEvent::new(seq, NormalizedEvent::done());
                }
            }
        };

        Ok(stream)
    }
}
