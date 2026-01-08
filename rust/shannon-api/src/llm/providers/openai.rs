//! OpenAI and OpenAI-compatible provider driver.
//!
//! This driver supports OpenAI, Groq, xAI, and any OpenAI-compatible API.

use crate::events::NormalizedEvent;
use crate::llm::{LlmDriver, LlmRequest, LlmSettings, Message, MessageContent, Provider};
use async_trait::async_trait;
use futures::{Stream, StreamExt};
use reqwest::Client;
use serde::Deserialize;
use std::pin::Pin;

/// OpenAI-compatible API driver.
#[derive(Debug, Clone)]
pub struct OpenAiDriver {
    settings: LlmSettings,
    client: Client,
}

impl OpenAiDriver {
    /// Create a new OpenAI driver.
    pub fn new(settings: LlmSettings) -> Self {
        let client = Client::builder()
            .timeout(std::time::Duration::from_secs(300))
            .build()
            .expect("Failed to create HTTP client");

        Self { settings, client }
    }

    /// Build the API URL.
    fn api_url(&self) -> String {
        format!(
            "{}/v1/chat/completions",
            self.settings.base_url.trim_end_matches('/')
        )
    }

    /// Convert messages to OpenAI format.
    fn convert_messages(messages: &[Message]) -> Vec<serde_json::Value> {
        messages
            .iter()
            .map(|msg| {
                let mut obj = serde_json::json!({
                    "role": match msg.role {
                        crate::llm::MessageRole::System => "system",
                        crate::llm::MessageRole::User => "user",
                        crate::llm::MessageRole::Assistant => "assistant",
                        crate::llm::MessageRole::Tool => "tool",
                    },
                });

                // Add content
                match &msg.content {
                    MessageContent::Text(text) => {
                        obj["content"] = serde_json::Value::String(text.clone());
                    }
                    MessageContent::Parts(parts) => {
                        obj["content"] = serde_json::to_value(parts).unwrap_or_default();
                    }
                }

                // Add tool call ID for tool messages
                if let Some(ref tool_call_id) = msg.tool_call_id {
                    obj["tool_call_id"] = serde_json::Value::String(tool_call_id.clone());
                }

                // Add tool calls for assistant messages
                if let Some(ref tool_calls) = msg.tool_calls {
                    obj["tool_calls"] = serde_json::to_value(tool_calls).unwrap_or_default();
                }

                obj
            })
            .collect()
    }
}

#[async_trait]
impl LlmDriver for OpenAiDriver {
    async fn stream(
        &self,
        req: LlmRequest,
    ) -> anyhow::Result<Pin<Box<dyn Stream<Item = anyhow::Result<NormalizedEvent>> + Send>>> {
        let model = req.model.as_ref().unwrap_or(&self.settings.model);
        let temperature = req.temperature.unwrap_or(self.settings.temperature);
        let max_tokens = req.max_tokens.unwrap_or(self.settings.max_tokens);

        let mut body = serde_json::json!({
            "model": model,
            "messages": Self::convert_messages(&req.messages),
            "temperature": temperature,
            "max_tokens": max_tokens,
            "stream": true,
            "stream_options": {
                "include_usage": true
            }
        });

        // Add tools if present
        if !req.tools.is_empty() {
            body["tools"] = serde_json::Value::Array(req.tools.clone());
        }

        // Add parallel tool calls setting if specified
        if let Some(parallel) = self.settings.parallel_tool_calls {
            body["parallel_tool_calls"] = serde_json::Value::Bool(parallel);
        }

        let mut request = self.client.post(self.api_url()).json(&body);

        // Add authorization header
        if let Some(ref api_key) = self.settings.api_key {
            request = request.header("Authorization", format!("Bearer {}", api_key));
        }

        let response = request.send().await?;

        if !response.status().is_success() {
            let status = response.status();
            let text = response.text().await.unwrap_or_default();
            anyhow::bail!("OpenAI API error ({}): {}", status, text);
        }

        let stream = response.bytes_stream();
        
        let event_stream = async_stream::stream! {
            let mut buffer = String::new();
            
            futures::pin_mut!(stream);
            
            while let Some(chunk_result) = stream.next().await {
                let chunk = match chunk_result {
                    Ok(c) => c,
                    Err(e) => {
                        yield Err(anyhow::anyhow!("Stream error: {}", e));
                        continue;
                    }
                };

                let chunk_str = match std::str::from_utf8(&chunk) {
                    Ok(s) => s,
                    Err(e) => {
                        yield Err(anyhow::anyhow!("UTF-8 error: {}", e));
                        continue;
                    }
                };

                buffer.push_str(chunk_str);

                // Process complete SSE lines
                while let Some(pos) = buffer.find("\n\n") {
                    let line = buffer[..pos].to_string();
                    buffer = buffer[pos + 2..].to_string();

                    for data_line in line.lines() {
                        if let Some(data) = data_line.strip_prefix("data: ") {
                            if data.trim() == "[DONE]" {
                                yield Ok(NormalizedEvent::done());
                                continue;
                            }

                            match serde_json::from_str::<OpenAiStreamChunk>(data) {
                                Ok(chunk) => {
                                    for event in chunk.to_normalized_events() {
                                        yield Ok(event);
                                    }
                                }
                                Err(e) => {
                                    tracing::warn!("Failed to parse chunk: {} - {}", e, data);
                                }
                            }
                        }
                    }
                }
            }
        };

        Ok(Box::pin(event_stream))
    }

    fn provider(&self) -> Provider {
        self.settings.provider
    }

    fn settings(&self) -> &LlmSettings {
        &self.settings
    }
}

/// OpenAI streaming response chunk.
#[derive(Debug, Deserialize)]
struct OpenAiStreamChunk {
    #[allow(dead_code)]
    id: Option<String>,
    choices: Option<Vec<OpenAiChoice>>,
    usage: Option<OpenAiUsage>,
    model: Option<String>,
}

#[derive(Debug, Deserialize)]
struct OpenAiChoice {
    delta: Option<OpenAiDelta>,
    finish_reason: Option<String>,
    #[allow(dead_code)]
    index: Option<usize>,
}

#[derive(Debug, Deserialize)]
struct OpenAiDelta {
    role: Option<String>,
    content: Option<String>,
    tool_calls: Option<Vec<OpenAiToolCallDelta>>,
}

#[derive(Debug, Deserialize)]
struct OpenAiToolCallDelta {
    index: usize,
    id: Option<String>,
    function: Option<OpenAiFunctionDelta>,
}

#[derive(Debug, Deserialize)]
struct OpenAiFunctionDelta {
    name: Option<String>,
    arguments: Option<String>,
}

#[derive(Debug, Deserialize)]
struct OpenAiUsage {
    prompt_tokens: u32,
    completion_tokens: u32,
    total_tokens: u32,
}

impl OpenAiStreamChunk {
    fn to_normalized_events(&self) -> Vec<NormalizedEvent> {
        let mut events = Vec::new();

        // Handle choices
        if let Some(ref choices) = self.choices {
            for choice in choices {
                if let Some(ref delta) = choice.delta {
                    // Handle content delta
                    if let Some(ref content) = delta.content {
                        if !content.is_empty() {
                            if let Some(ref role) = delta.role {
                                events.push(NormalizedEvent::message_delta_with_role(
                                    content.clone(),
                                    role.clone(),
                                ));
                            } else {
                                events.push(NormalizedEvent::message_delta(content.clone()));
                            }
                        }
                    }

                    // Handle tool call deltas
                    if let Some(ref tool_calls) = delta.tool_calls {
                        for tc in tool_calls {
                            events.push(NormalizedEvent::ToolCallDelta {
                                index: tc.index,
                                id: tc.id.clone(),
                                name: tc.function.as_ref().and_then(|f| f.name.clone()),
                                arguments: tc.function.as_ref().and_then(|f| f.arguments.clone()),
                            });
                        }
                    }
                }

                // Handle finish reason
                if let Some(ref reason) = choice.finish_reason {
                    events.push(NormalizedEvent::done_with_reason(reason.clone()));
                }
            }
        }

        // Handle usage
        if let Some(ref usage) = self.usage {
            events.push(NormalizedEvent::Usage {
                prompt_tokens: usage.prompt_tokens,
                completion_tokens: usage.completion_tokens,
                total_tokens: usage.total_tokens,
                model: self.model.clone(),
            });
        }

        events
    }
}
