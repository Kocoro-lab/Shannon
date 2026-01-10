use futures::TryStreamExt;
use reqwest::Client;
use serde::{Deserialize, Serialize};
use std::borrow::Cow;
use std::pin::Pin;
use tokio::io::{AsyncBufReadExt, BufReader};
use tokio_stream::wrappers::LinesStream;
use tokio_stream::StreamExt;
use tokio_util::io::StreamReader;
use tracing::{debug, info, instrument, warn};

use crate::config::Config;
use crate::error::{AgentError, AgentResult};

#[derive(Debug, Serialize)]
pub struct AgentQuery<'a> {
    pub query: Cow<'a, str>,
    pub context: serde_json::Value,
    pub agent_id: Cow<'a, str>,
    pub mode: Cow<'a, str>,
    #[serde(rename = "allowed_tools")]
    pub tools: Vec<Cow<'a, str>>,
    pub max_tokens: u32,
    pub temperature: f32,
    pub model_tier: Cow<'a, str>,
    pub stream: bool,
}

#[derive(Debug, Deserialize)]
pub struct AgentResponse {
    pub success: bool,
    pub response: String,
    pub tokens_used: u32,
    pub model_used: String,
    #[serde(default)]
    pub provider: String,
    #[serde(default = "default_finish_reason")]
    pub finish_reason: String,
    #[serde(default)]
    pub metadata: Option<serde_json::Value>,
}

#[derive(Debug, Deserialize)]
pub struct StreamResponseLine {
    pub event: Option<String>,
    pub delta: Option<String>,
    pub content: Option<String>,
    pub response: Option<String>,
    pub model: Option<String>,
    pub provider: Option<String>,
    pub finish_reason: Option<String>,
    pub tokens_used: Option<u32>,
    pub input_tokens: Option<u32>,
    pub output_tokens: Option<u32>,
    pub cost_usd: Option<f64>,
    pub usage: Option<StreamUsage>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct StreamUsage {
    pub total_tokens: Option<u32>,
    pub input_tokens: Option<u32>,
    pub output_tokens: Option<u32>,
    pub cost_usd: Option<f64>,
    pub model: Option<String>,
    pub provider: Option<String>,
}

#[derive(Debug)]
pub struct StreamFinal {
    pub response: String,
    pub model_used: Option<String>,
    pub provider: Option<String>,
    pub finish_reason: Option<String>,
    pub input_tokens: Option<u32>,
    pub output_tokens: Option<u32>,
    pub total_tokens: Option<u32>,
    pub cost_usd: Option<f64>,
}

#[derive(Debug)]
pub struct StreamChunk {
    pub delta: Option<String>,
    pub final_message: Option<StreamFinal>,
}

fn default_finish_reason() -> String {
    "stop".to_string()
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TokenUsage {
    pub prompt_tokens: u32,
    pub completion_tokens: u32,
    pub total_tokens: u32,
    pub cost_usd: f64,
    pub model: String,
    pub provider: String,
}

#[derive(Debug)]
pub struct AgentQueryResult {
    pub response: String,
    pub usage: TokenUsage,
    pub metadata: Option<serde_json::Value>,
}

#[derive(Debug)]
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
    ) -> AgentResult<AgentQueryResult> {
        let url = format!("{}/agent/query", self.base_url);

        // Use Cow to avoid unnecessary string allocations
        let tools_vec = tools
            .unwrap_or_default()
            .into_iter()
            .map(Cow::Owned)
            .collect();

        // Determine model_tier, allowing context override
        let ctx_val = context.clone().unwrap_or_else(|| serde_json::json!({}));
        let tier_from_mode = match mode {
            "simple" => "small".to_string(),
            "complex" => "large".to_string(),
            _ => "medium".to_string(),
        };
        let mut effective_tier = tier_from_mode.clone();
        if let Some(obj) = ctx_val.as_object() {
            if let Some(mt) = obj.get("model_tier").and_then(|v| v.as_str()) {
                let mt_l = mt.to_lowercase();
                if mt_l == "small" || mt_l == "medium" || mt_l == "large" {
                    effective_tier = mt_l;
                }
            }
            if let Some(po) = obj.get("provider_override").and_then(|v| v.as_str()) {
                debug!("Provider override present in context: {}", po);
            }
        }

        // Check for max_tokens override in context, otherwise use 16384 default (increased for GPT-5 reasoning models)
        let max_tokens = if let Some(obj) = ctx_val.as_object() {
            obj.get("max_tokens")
                .and_then(|v| {
                    v.as_u64().or_else(|| {
                        v.as_f64()
                            .filter(|&f| f.is_finite() && f >= 0.0 && f <= u64::MAX as f64)
                            .map(|f| f as u64)
                    })
                })
                .map(|n| n as u32)
                .unwrap_or(16384)
        } else {
            16384
        };

        debug!(
            "LLMClient tier selection: mode_default={}, effective_tier={}, max_tokens={}",
            tier_from_mode, effective_tier, max_tokens
        );

        let request = AgentQuery {
            query: Cow::Borrowed(query),
            context: ctx_val,
            agent_id: Cow::Borrowed(agent_id),
            mode: Cow::Borrowed(mode),
            tools: tools_vec,
            max_tokens,
            temperature: 0.7,
            model_tier: Cow::Owned(effective_tier),
            stream: false,
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

            // Always surface errors for observability (removed dev mock fallback)
            return Err(AgentError::HttpError {
                status: status.as_u16(),
                message: format!("LLM service error: {} - {}", status, body),
            });
        }

        let agent_response: AgentResponse = response.json().await.map_err(|e| {
            AgentError::LlmResponseParseError(format!(
                "Failed to parse LLM service response: {}",
                e
            ))
        })?;

        if !agent_response.success {
            warn!("LLM service returned unsuccessful response");
            return Ok(AgentQueryResult {
                response: format!("Error response for: {}", query),
                usage: TokenUsage {
                    prompt_tokens: 0,
                    completion_tokens: 0,
                    total_tokens: 0,
                    cost_usd: 0.0,
                    model: "error".to_string(),
                    provider: "unknown".to_string(),
                },
                metadata: agent_response.metadata,
            });
        }

        let token_usage = TokenUsage {
            prompt_tokens: agent_response.tokens_used / 3, // Rough estimate
            completion_tokens: agent_response.tokens_used * 2 / 3, // Rough estimate
            total_tokens: agent_response.tokens_used,
            cost_usd: calculate_cost(&agent_response.model_used, agent_response.tokens_used),
            model: agent_response.model_used.clone(),
            provider: agent_response.provider.clone(),
        };

        info!(
            "LLM query successful: {} tokens used, model: {}",
            token_usage.total_tokens, token_usage.model
        );

        Ok(AgentQueryResult {
            response: agent_response.response,
            usage: token_usage,
            metadata: agent_response.metadata,
        })
    }

    #[instrument(skip(self, context), fields(agent_id = %agent_id, mode = %mode))]
    pub async fn stream_query_agent(
        &self,
        query: &str,
        agent_id: &str,
        mode: &str,
        context: Option<serde_json::Value>,
        tools: Option<Vec<String>>,
    ) -> AgentResult<Pin<Box<dyn tokio_stream::Stream<Item = AgentResult<StreamChunk>> + Send>>>
    {
        let url = format!("{}/agent/query", self.base_url);

        let tools_vec = tools
            .unwrap_or_default()
            .into_iter()
            .map(Cow::Owned)
            .collect();

        let ctx_val = context.clone().unwrap_or_else(|| serde_json::json!({}));
        let tier_from_mode = match mode {
            "simple" => "small".to_string(),
            "complex" => "large".to_string(),
            _ => "medium".to_string(),
        };
        let mut effective_tier = tier_from_mode.clone();
        if let Some(obj) = ctx_val.as_object() {
            if let Some(mt) = obj.get("model_tier").and_then(|v| v.as_str()) {
                let mt_l = mt.to_lowercase();
                if mt_l == "small" || mt_l == "medium" || mt_l == "large" {
                    effective_tier = mt_l;
                }
            }
        }

        let max_tokens = if let Some(obj) = ctx_val.as_object() {
            obj.get("max_tokens")
                .and_then(|v| {
                    v.as_u64().or_else(|| {
                        v.as_f64()
                            .filter(|&f| f.is_finite() && f >= 0.0 && f <= u64::MAX as f64)
                            .map(|f| f as u64)
                    })
                })
                .map(|n| n as u32)
                .unwrap_or(16384)
        } else {
            16384
        };

        let request = AgentQuery {
            query: Cow::Borrowed(query),
            context: ctx_val,
            agent_id: Cow::Borrowed(agent_id),
            mode: Cow::Borrowed(mode),
            tools: tools_vec,
            max_tokens,
            temperature: 0.7,
            model_tier: Cow::Owned(effective_tier),
            stream: true,
        };

        let headers = http::HeaderMap::new();
        let mut request_builder = self.client.post(&url).json(&request);
        for (key, value) in headers.iter() {
            if let Ok(header_value) = value.to_str() {
                request_builder = request_builder.header(key.as_str(), header_value);
            }
        }

        let response = request_builder.send().await.map_err(|e| {
            AgentError::NetworkError(format!(
                "Failed to send streaming request to LLM service: {}",
                e
            ))
        })?;

        if !response.status().is_success() {
            let status = response.status();
            let body = response.text().await.unwrap_or_default();
            warn!(
                "LLM streaming service returned error: {} - {}",
                status, body
            );

            return Err(AgentError::HttpError {
                status: status.as_u16(),
                message: format!("LLM streaming service error: {} - {}", status, body),
            });
        }

        let byte_stream = response
            .bytes_stream()
            .map_err(|e| std::io::Error::new(std::io::ErrorKind::Other, e));
        let reader = StreamReader::new(byte_stream);
        let lines = LinesStream::new(BufReader::new(reader).lines());

        let mapped = lines.map(|line_res| match line_res {
            Ok(line) => {
                if line.trim().is_empty() {
                    return Ok(StreamChunk {
                        delta: None,
                        final_message: None,
                    });
                }
                let parsed: StreamResponseLine = serde_json::from_str(&line).map_err(|e| {
                    AgentError::LlmResponseParseError(format!(
                        "Failed to parse stream chunk: {}",
                        e
                    ))
                })?;

                let usage = parsed.usage.clone();

                let is_final = parsed
                    .event
                    .as_deref()
                    .map(|ev| ev == "thread.message.completed")
                    .unwrap_or(false)
                    || parsed.response.is_some();

                let delta = parsed.delta.clone().or(parsed.content.clone());

                let final_message = if is_final {
                    let total_tokens = usage
                        .as_ref()
                        .and_then(|u| u.total_tokens)
                        .or(parsed.tokens_used);
                    let input_tokens = usage
                        .as_ref()
                        .and_then(|u| u.input_tokens)
                        .or(parsed.input_tokens);
                    let output_tokens = usage
                        .as_ref()
                        .and_then(|u| u.output_tokens)
                        .or(parsed.output_tokens);
                    let cost_usd = usage.as_ref().and_then(|u| u.cost_usd).or(parsed.cost_usd);
                    let model_used = parsed
                        .model
                        .or_else(|| usage.as_ref().and_then(|u| u.model.clone()));
                    let provider = parsed
                        .provider
                        .or_else(|| usage.as_ref().and_then(|u| u.provider.clone()));
                    Some(StreamFinal {
                        response: parsed.response.unwrap_or_default(),
                        model_used,
                        provider,
                        finish_reason: parsed.finish_reason,
                        input_tokens,
                        output_tokens,
                        total_tokens,
                        cost_usd,
                    })
                } else {
                    None
                };

                Ok(StreamChunk {
                    delta,
                    final_message,
                })
            }
            Err(e) => Err(AgentError::NetworkError(format!(
                "Stream read error: {}",
                e
            ))),
        });

        Ok(Box::pin(mapped))
    }
    // Complexity analysis removed with FSM
}

fn calculate_cost(model: &str, tokens: u32) -> f64 {
    // Try centralized pricing from /app/config/models.yaml (returns model price or default)
    if let Some(per_1k) = pricing_cost_per_1k(model) {
        return (tokens as f64 / 1000.0) * per_1k;
    }
    // Fallback to 0.0 for self-hosted/custom models without pricing config
    // warn!(
    //     "No pricing found for model '{}' in config/models.yaml - defaulting to $0.00 cost. \
    //      Add pricing configuration if this model should be tracked.",
    //     model
    // );
    0.0
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
