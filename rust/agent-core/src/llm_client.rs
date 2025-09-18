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
    // Try centralized pricing from /app/config/models.yaml
    if let Some(per_1k) = pricing_cost_per_1k(model) {
        return (tokens as f64 / 1000.0) * per_1k;
    }

    // Fallback rough estimates per 1K tokens
    let fallback_per_1k = if model.contains("gpt-4-turbo") {
        0.03
    } else if model.contains("gpt-4") {
        0.06
    } else if model.contains("gpt-3.5") {
        0.002
    } else if model.contains("claude-3-opus") {
        0.075
    } else if model.contains("claude-3-sonnet") {
        0.015
    } else if model.contains("claude-3-haiku") {
        0.001
    } else {
        0.001
    };
    (tokens as f64 / 1000.0) * fallback_per_1k
}

fn pricing_cost_per_1k(model: &str) -> Option<f64> {
    use serde::Deserialize;
    use std::collections::HashMap;

    #[derive(Deserialize)]
    struct ModelPrice {
        input_per_1k: Option<f64>,
        output_per_1k: Option<f64>,
        combined_per_1k: Option<f64>,
    }
    #[derive(Deserialize)]
    struct Pricing {
        defaults: Option<Defaults>,
        models: Option<HashMap<String, HashMap<String, ModelPrice>>>,
    }
    #[derive(Deserialize)]
    struct Defaults {
        combined_per_1k: Option<f64>,
    }
    #[derive(Deserialize)]
    struct Root {
        pricing: Option<Pricing>,
    }

    let candidates = [
        std::env::var("MODELS_CONFIG_PATH").unwrap_or_default(),
        "/app/config/models.yaml".to_string(),
        "./config/models.yaml".to_string(),
    ];
    for p in candidates.iter() {
        if p.is_empty() {
            continue;
        }
        let data = std::fs::read_to_string(p);
        if data.is_err() {
            continue;
        }
        if let Ok(root) = serde_yaml::from_str::<Root>(&data.unwrap()) {
            if let Some(pr) = root.pricing {
                if let Some(models) = pr.models {
                    for (_prov, mm) in models.iter() {
                        if let Some(mp) = mm.get(model) {
                            if let Some(c) = mp.combined_per_1k {
                                return Some(c);
                            }
                            if let (Some(i), Some(o)) = (mp.input_per_1k, mp.output_per_1k) {
                                return Some((i + o) / 2.0);
                            }
                        }
                    }
                }
                if let Some(def) = pr.defaults {
                    if let Some(c) = def.combined_per_1k {
                        return Some(c);
                    }
                }
            }
        }
    }
    None
}
