//! LLM activity types for workflow execution.
//!
//! Provides activities for calling LLM providers with retry and fallback logic.

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

use super::{Activity, ActivityContext, ActivityResult};

/// LLM request for reasoning activities.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LlmRequest {
    /// Model to use.
    pub model: String,
    /// System prompt.
    pub system: String,
    /// Messages to send.
    pub messages: Vec<Message>,
    /// Temperature for sampling.
    pub temperature: f32,
    /// Maximum tokens to generate.
    pub max_tokens: Option<u32>,
}

/// Message in a conversation.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Message {
    /// Role (user, assistant, system).
    pub role: String,
    /// Message content.
    pub content: String,
}

/// LLM response.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LlmResponse {
    /// Generated content.
    pub content: String,
    /// Model used.
    pub model: String,
    /// Token usage.
    pub usage: TokenUsage,
}

/// Token usage statistics.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct TokenUsage {
    /// Prompt tokens.
    pub prompt_tokens: u32,
    /// Completion tokens.
    pub completion_tokens: u32,
    /// Total tokens.
    pub total_tokens: u32,
}

/// LLM Reason activity for generating reasoning steps.
pub struct LlmReasonActivity {
    /// HTTP client for API calls.
    client: reqwest::Client,
    /// API base URL.
    base_url: String,
    /// API key.
    api_key: Option<String>,
}

impl LlmReasonActivity {
    /// Create a new LLM reason activity.
    #[must_use]
    pub fn new(base_url: String, api_key: Option<String>) -> Self {
        Self {
            client: reqwest::Client::new(),
            base_url,
            api_key,
        }
    }

    /// Create with default settings (uses local Shannon API).
    #[must_use]
    pub fn local() -> Self {
        Self::new("http://127.0.0.1:8765".to_string(), None)
    }
}

impl std::fmt::Debug for LlmReasonActivity {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("LlmReasonActivity")
            .field("base_url", &self.base_url)
            .finish()
    }
}

#[async_trait]
impl Activity for LlmReasonActivity {
    fn name(&self) -> &'static str {
        "llm_reason"
    }

    async fn execute(
        &self,
        ctx: &ActivityContext,
        input: serde_json::Value,
    ) -> ActivityResult {
        let request: LlmRequest = match serde_json::from_value(input) {
            Ok(r) => r,
            Err(e) => return ActivityResult::failure(format!("Invalid input: {e}"), false),
        };

        tracing::debug!(
            "LLM reason activity {} executing with model {}",
            ctx.activity_id,
            request.model
        );

        // Build API request
        let mut api_request = self
            .client
            .post(format!("{}/api/v1/chat/completions", self.base_url))
            .header("Content-Type", "application/json");

        if let Some(ref key) = self.api_key {
            api_request = api_request.header("Authorization", format!("Bearer {key}"));
        }

        // Build messages array
        let mut messages = vec![serde_json::json!({
            "role": "system",
            "content": request.system
        })];
        for m in &request.messages {
            messages.push(serde_json::json!({
                "role": m.role,
                "content": m.content
            }));
        }

        let body = serde_json::json!({
            "model": request.model,
            "messages": messages,
            "temperature": request.temperature,
            "max_tokens": request.max_tokens.unwrap_or(4096)
        });

        // Execute request with timeout
        let response = match tokio::time::timeout(
            std::time::Duration::from_secs(ctx.timeout_secs),
            api_request.json(&body).send(),
        )
        .await
        {
            Ok(Ok(resp)) => resp,
            Ok(Err(e)) => {
                return ActivityResult::Retry {
                    reason: format!("HTTP error: {e}"),
                    backoff_secs: self.retry_backoff(ctx.attempt).as_secs(),
                }
            }
            Err(_) => {
                return ActivityResult::Retry {
                    reason: "Request timeout".to_string(),
                    backoff_secs: self.retry_backoff(ctx.attempt).as_secs(),
                }
            }
        };

        if !response.status().is_success() {
            let status = response.status();
            let error_text = response.text().await.unwrap_or_default();

            // Rate limit errors are retryable
            if status.as_u16() == 429 {
                return ActivityResult::Retry {
                    reason: "Rate limited".to_string(),
                    backoff_secs: 60,
                };
            }

            return ActivityResult::failure(
                format!("API error {status}: {error_text}"),
                status.is_server_error(),
            );
        }

        // Parse response
        let api_response: serde_json::Value = match response.json().await {
            Ok(r) => r,
            Err(e) => return ActivityResult::failure(format!("Invalid response: {e}"), true),
        };

        // Extract content
        let content = api_response["choices"][0]["message"]["content"]
            .as_str()
            .unwrap_or_default()
            .to_string();

        let usage = TokenUsage {
            prompt_tokens: api_response["usage"]["prompt_tokens"]
                .as_u64()
                .unwrap_or(0) as u32,
            completion_tokens: api_response["usage"]["completion_tokens"]
                .as_u64()
                .unwrap_or(0) as u32,
            total_tokens: api_response["usage"]["total_tokens"].as_u64().unwrap_or(0)
                as u32,
        };

        ActivityResult::success(LlmResponse {
            content,
            model: request.model,
            usage,
        })
    }
}

/// LLM Synthesize activity for combining reasoning into a final answer.
pub struct LlmSynthesizeActivity {
    /// Underlying reason activity.
    reason: LlmReasonActivity,
}

impl LlmSynthesizeActivity {
    /// Create a new synthesize activity.
    #[must_use]
    pub fn new(base_url: String, api_key: Option<String>) -> Self {
        Self {
            reason: LlmReasonActivity::new(base_url, api_key),
        }
    }
}

impl std::fmt::Debug for LlmSynthesizeActivity {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("LlmSynthesizeActivity").finish()
    }
}

#[async_trait]
impl Activity for LlmSynthesizeActivity {
    fn name(&self) -> &'static str {
        "llm_synthesize"
    }

    async fn execute(
        &self,
        ctx: &ActivityContext,
        input: serde_json::Value,
    ) -> ActivityResult {
        // Extract synthesis request
        let thoughts = input["thoughts"]
            .as_array()
            .map(|a| {
                a.iter()
                    .filter_map(|v| v.as_str())
                    .collect::<Vec<_>>()
                    .join("\n\n")
            })
            .unwrap_or_default();

        let query = input["query"].as_str().unwrap_or_default();

        // Build synthesis prompt
        let system = "You are a synthesis expert. Given a series of reasoning steps, \
            synthesize them into a clear, coherent final answer.";

        let messages = vec![Message {
            role: "user".to_string(),
            content: format!(
                "Original question: {query}\n\n\
                Reasoning steps:\n{thoughts}\n\n\
                Please synthesize these into a final, well-structured answer."
            ),
        }];

        let request = LlmRequest {
            model: input["model"]
                .as_str()
                .unwrap_or("claude-sonnet-4-20250514")
                .to_string(),
            system: system.to_string(),
            messages,
            temperature: 0.3,
            max_tokens: Some(4096),
        };

        self.reason
            .execute(ctx, serde_json::to_value(request).unwrap())
            .await
    }
}
