//! Anthropic Claude API driver.

use crate::events::NormalizedEvent;
use crate::llm::{LlmDriver, LlmRequest, LlmSettings, Message, MessageContent, Provider};
use async_trait::async_trait;
use futures::{Stream, StreamExt};
use reqwest::Client;
use serde::Deserialize;
use std::pin::Pin;

/// Anthropic Claude API driver.
#[derive(Debug, Clone)]
pub struct AnthropicDriver {
    settings: LlmSettings,
    client: Client,
}

impl AnthropicDriver {
    /// Create a new Anthropic driver.
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
            "{}/v1/messages",
            self.settings.base_url.trim_end_matches('/')
        )
    }

    /// Convert messages to Anthropic format.
    fn convert_messages(messages: &[Message]) -> (Option<String>, Vec<serde_json::Value>) {
        let mut system_prompt = None;
        let mut converted = Vec::new();

        for msg in messages {
            match msg.role {
                crate::llm::MessageRole::System => {
                    if let Some(text) = msg.content.as_text() {
                        system_prompt = Some(text.to_string());
                    }
                }
                crate::llm::MessageRole::User => {
                    let content = match &msg.content {
                        MessageContent::Text(text) => text.clone(),
                        MessageContent::Parts(_) => {
                            msg.content.as_text().unwrap_or_default().to_string()
                        }
                    };
                    converted.push(serde_json::json!({
                        "role": "user",
                        "content": content
                    }));
                }
                crate::llm::MessageRole::Assistant => {
                    let content = match &msg.content {
                        MessageContent::Text(text) => text.clone(),
                        MessageContent::Parts(_) => {
                            msg.content.as_text().unwrap_or_default().to_string()
                        }
                    };
                    converted.push(serde_json::json!({
                        "role": "assistant",
                        "content": content
                    }));
                }
                crate::llm::MessageRole::Tool => {
                    // Anthropic uses tool_result content blocks
                    if let (Some(tool_call_id), Some(text)) = (&msg.tool_call_id, msg.content.as_text()) {
                        converted.push(serde_json::json!({
                            "role": "user",
                            "content": [{
                                "type": "tool_result",
                                "tool_use_id": tool_call_id,
                                "content": text
                            }]
                        }));
                    }
                }
            }
        }

        (system_prompt, converted)
    }

    /// Convert tools to Anthropic format.
    fn convert_tools(tools: &[serde_json::Value]) -> Vec<serde_json::Value> {
        tools
            .iter()
            .filter_map(|tool| {
                let function = tool.get("function")?;
                Some(serde_json::json!({
                    "name": function.get("name")?,
                    "description": function.get("description").unwrap_or(&serde_json::Value::String("".to_string())),
                    "input_schema": function.get("parameters").unwrap_or(&serde_json::json!({"type": "object", "properties": {}}))
                }))
            })
            .collect()
    }
}

#[async_trait]
impl LlmDriver for AnthropicDriver {
    async fn stream(
        &self,
        req: LlmRequest,
    ) -> anyhow::Result<Pin<Box<dyn Stream<Item = anyhow::Result<NormalizedEvent>> + Send>>> {
        let model = req.model.as_ref().unwrap_or(&self.settings.model);
        let max_tokens = req.max_tokens.unwrap_or(self.settings.max_tokens);

        let (system_prompt, messages) = Self::convert_messages(&req.messages);

        let mut body = serde_json::json!({
            "model": model,
            "messages": messages,
            "max_tokens": max_tokens,
            "stream": true
        });

        if let Some(system) = system_prompt {
            body["system"] = serde_json::Value::String(system);
        }

        // Add tools if present
        if !req.tools.is_empty() {
            body["tools"] = serde_json::Value::Array(Self::convert_tools(&req.tools));
        }

        let api_key = self.settings.api_key.as_ref()
            .ok_or_else(|| anyhow::anyhow!("Anthropic API key required"))?;

        let response = self
            .client
            .post(self.api_url())
            .header("x-api-key", api_key)
            .header("anthropic-version", "2023-06-01")
            .header("content-type", "application/json")
            .json(&body)
            .send()
            .await?;

        if !response.status().is_success() {
            let status = response.status();
            let text = response.text().await.unwrap_or_default();
            anyhow::bail!("Anthropic API error ({}): {}", status, text);
        }

        let stream = response.bytes_stream();

        let event_stream = async_stream::stream! {
            let mut buffer = String::new();
            let mut input_tokens = 0u32;
            let mut output_tokens = 0u32;

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
                            match serde_json::from_str::<AnthropicEvent>(data) {
                                Ok(event) => {
                                    match event.event_type.as_str() {
                                        "message_start" => {
                                            if let Some(msg) = event.message {
                                                if let Some(usage) = msg.usage {
                                                    input_tokens = usage.input_tokens;
                                                }
                                            }
                                        }
                                        "content_block_delta" => {
                                            if let Some(delta) = event.delta {
                                                if delta.delta_type == "text_delta" {
                                                    if let Some(text) = delta.text {
                                                        yield Ok(NormalizedEvent::message_delta(text));
                                                    }
                                                } else if delta.delta_type == "input_json_delta" {
                                                    if let Some(json) = delta.partial_json {
                                                        yield Ok(NormalizedEvent::ToolCallDelta {
                                                            index: event.index.unwrap_or(0),
                                                            id: None,
                                                            name: None,
                                                            arguments: Some(json),
                                                        });
                                                    }
                                                }
                                            }
                                        }
                                        "content_block_start" => {
                                            if let Some(block) = event.content_block {
                                                if block.block_type == "tool_use" {
                                                    yield Ok(NormalizedEvent::ToolCallDelta {
                                                        index: event.index.unwrap_or(0),
                                                        id: block.id,
                                                        name: block.name,
                                                        arguments: None,
                                                    });
                                                }
                                            }
                                        }
                                        "message_delta" => {
                                            if let Some(delta) = event.delta {
                                                if let Some(reason) = delta.stop_reason {
                                                    yield Ok(NormalizedEvent::done_with_reason(reason));
                                                }
                                            }
                                            if let Some(usage) = event.usage {
                                                output_tokens = usage.output_tokens.unwrap_or(0);
                                            }
                                        }
                                        "message_stop" => {
                                            yield Ok(NormalizedEvent::Usage {
                                                prompt_tokens: input_tokens,
                                                completion_tokens: output_tokens,
                                                total_tokens: input_tokens + output_tokens,
                                                model: None,
                                            });
                                            yield Ok(NormalizedEvent::done());
                                        }
                                        "error" => {
                                            if let Some(error) = event.error {
                                                yield Ok(NormalizedEvent::error(error.message));
                                            }
                                        }
                                        _ => {}
                                    }
                                }
                                Err(e) => {
                                    tracing::warn!("Failed to parse Anthropic event: {} - {}", e, data);
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
        Provider::Anthropic
    }

    fn settings(&self) -> &LlmSettings {
        &self.settings
    }
}

/// Anthropic SSE event.
#[derive(Debug, Deserialize)]
struct AnthropicEvent {
    #[serde(rename = "type")]
    event_type: String,
    index: Option<usize>,
    message: Option<AnthropicMessage>,
    content_block: Option<AnthropicContentBlock>,
    delta: Option<AnthropicDelta>,
    usage: Option<AnthropicUsage>,
    error: Option<AnthropicError>,
}

#[derive(Debug, Deserialize)]
struct AnthropicMessage {
    usage: Option<AnthropicUsage>,
}

#[derive(Debug, Deserialize)]
struct AnthropicContentBlock {
    #[serde(rename = "type")]
    block_type: String,
    id: Option<String>,
    name: Option<String>,
}

#[derive(Debug, Deserialize)]
struct AnthropicDelta {
    #[serde(rename = "type")]
    delta_type: String,
    text: Option<String>,
    partial_json: Option<String>,
    stop_reason: Option<String>,
}

#[derive(Debug, Deserialize)]
struct AnthropicUsage {
    input_tokens: u32,
    output_tokens: Option<u32>,
}

#[derive(Debug, Deserialize)]
struct AnthropicError {
    message: String,
}
