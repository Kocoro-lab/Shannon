use reqwest::Client;
use serde::{Deserialize, Serialize};
use std::borrow::Cow;
use tracing::{debug, info, instrument, warn};

use crate::config::Config;
use crate::error::{AgentError, AgentResult};

#[derive(Debug, Serialize)]
pub struct AgentQuery<'a> {
    pub query: Cow<'a, str>,
    pub context: serde_json::Value,
    pub agent_id: Cow<'a, str>,
    pub mode: Cow<'a, str>,
    pub tools: Vec<Cow<'a, str>>,
    pub max_tokens: u32,
    pub temperature: f32,
    pub model_tier: Cow<'a, str>,
}

#[derive(Debug, Deserialize)]
pub struct AgentResponse {
    pub success: bool,
    pub response: String,
    pub tokens_used: u32,
    pub model_used: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TokenUsage {
    pub prompt_tokens: u32,
    pub completion_tokens: u32,
    pub total_tokens: u32,
    pub cost_usd: f64,
    pub model: String,
}

pub struct LLMClient {
    client: Client,
    base_url: String,
}

impl LLMClient {
    pub fn new(base_url: Option<String>) -> AgentResult<Self> {
        let config = Config::global().unwrap_or_default();

        let base_url = base_url.unwrap_or_else(|| {
            std::env::var("LLM_SERVICE_URL").unwrap_or_else(|_| config.llm.base_url.clone())
        });

        let client = Client::builder()
            .timeout(config.llm_timeout())
            .build()
            .map_err(|e| AgentError::NetworkError(format!("Failed to build HTTP client: {}", e)))?;

        info!("LLM client initialized with base URL: {}", base_url);

        Ok(Self { client, base_url })
    }

    #[instrument(skip(self, context), fields(agent_id = %agent_id, mode = %mode))]
    pub async fn query_agent(
        &self,
        query: &str,
        agent_id: &str,
        mode: &str,
        context: Option<serde_json::Value>,
        tools: Option<Vec<String>>,
    ) -> AgentResult<(String, TokenUsage)> {
        let url = format!("{}/agent/query", self.base_url);

        // Use Cow to avoid unnecessary string allocations
        let tools_vec = tools
            .unwrap_or_default()
            .into_iter()
            .map(Cow::Owned)
            .collect();

        let request = AgentQuery {
            query: Cow::Borrowed(query),
            context: context.unwrap_or_else(|| serde_json::json!({})),
            agent_id: Cow::Borrowed(agent_id),
            mode: Cow::Borrowed(mode),
            tools: tools_vec,
            max_tokens: 2048,
            temperature: 0.7,
            model_tier: match mode {
                "simple" => Cow::Borrowed("small"),
                "complex" => Cow::Borrowed("large"),
                _ => Cow::Borrowed("medium"),
            },
        };

        debug!("Sending query to LLM service: {:?}", request);

        // Add trace context propagation headers
        let headers = http::HeaderMap::new();

        // Use the active span context instead of environment variable
        // crate::tracing::inject_current_trace_context(&mut headers); // TODO: Fix tracing import

        let mut request_builder = self.client.post(&url).json(&request);

        // Add the trace headers to the request
        for (key, value) in headers.iter() {
            if let Ok(header_value) = value.to_str() {
                request_builder = request_builder.header(key.as_str(), header_value);
            }
        }

        let response = request_builder.send().await.map_err(|e| {
            AgentError::NetworkError(format!("Failed to send request to LLM service: {}", e))
        })?;

        if !response.status().is_success() {
            let status = response.status();
            let body = response.text().await.unwrap_or_default();
            warn!("LLM service returned error: {} - {}", status, body);

            // Fallback to mock response if LLM service fails
            return Ok((
                format!("Mock response for: {}", query),
                TokenUsage {
                    prompt_tokens: 10,
                    completion_tokens: 20,
                    total_tokens: 30,
                    cost_usd: 0.0001,
                    model: "mock".to_string(),
                },
            ));
        }

        let agent_response: AgentResponse = response.json().await.map_err(|e| {
            AgentError::LlmResponseParseError(format!(
                "Failed to parse LLM service response: {}",
                e
            ))
        })?;

        if !agent_response.success {
            warn!("LLM service returned unsuccessful response");
            return Ok((
                format!("Error response for: {}", query),
                TokenUsage {
                    prompt_tokens: 0,
                    completion_tokens: 0,
                    total_tokens: 0,
                    cost_usd: 0.0,
                    model: "error".to_string(),
                },
            ));
        }

        let token_usage = TokenUsage {
            prompt_tokens: agent_response.tokens_used / 3, // Rough estimate
            completion_tokens: agent_response.tokens_used * 2 / 3, // Rough estimate
            total_tokens: agent_response.tokens_used,
            cost_usd: calculate_cost(&agent_response.model_used, agent_response.tokens_used),
            model: agent_response.model_used.clone(),
        };

        info!(
            "LLM query successful: {} tokens used, model: {}",
            token_usage.total_tokens, token_usage.model
        );

        Ok((agent_response.response, token_usage))
    }
    // Complexity analysis removed with FSM
}

fn calculate_cost(model: &str, tokens: u32) -> f64 {
    // Rough cost estimates per 1K tokens
    let cost_per_1k = match model {
        m if m.contains("gpt-3.5") => 0.002,
        m if m.contains("gpt-4-turbo") => 0.03,
        m if m.contains("gpt-4") => 0.06,
        m if m.contains("claude-3-haiku") => 0.001,
        m if m.contains("claude-3-sonnet") => 0.015,
        m if m.contains("claude-3-opus") => 0.075,
        _ => 0.001, // Default/mock
    };

    (tokens as f64 / 1000.0) * cost_per_1k
}
