//! gRPC client for communicating with the Go Orchestrator service.
//!
//! This module provides a proper Tonic gRPC client for submitting tasks,
//! checking status, and managing workflows via the Orchestrator's gRPC API.
//!
//! This module requires the `grpc` feature to be enabled for full functionality.
//! Without the feature, stub implementations are provided.

use std::collections::HashMap;
use std::time::Duration;

use serde::{Deserialize, Serialize};

#[cfg(feature = "grpc")]
use tonic::transport::{Channel, Endpoint};

// Re-export generated protobuf types
#[cfg(feature = "grpc")]
pub mod proto {
    pub mod common {
        include!("../proto/shannon.common.rs");
    }
    pub mod orchestrator {
        include!("../proto/shannon.orchestrator.rs");
    }
}

#[cfg(feature = "grpc")]
use proto::orchestrator::orchestrator_service_client::OrchestratorServiceClient;

/// Configuration for the Orchestrator gRPC client.
#[derive(Debug, Clone)]
pub struct OrchestratorClientConfig {
    /// gRPC endpoint address (e.g., "http://localhost:50052").
    pub endpoint: String,
    /// Request timeout in seconds.
    pub timeout_secs: u64,
    /// Enable TLS.
    pub tls_enabled: bool,
    /// Connection timeout in seconds.
    pub connect_timeout_secs: u64,
}

impl Default for OrchestratorClientConfig {
    fn default() -> Self {
        Self {
            endpoint: "http://localhost:50052".to_string(),
            timeout_secs: 300,
            tls_enabled: false,
            connect_timeout_secs: 10,
        }
    }
}

impl OrchestratorClientConfig {
    /// Load configuration from environment variables.
    pub fn from_env() -> Self {
        Self {
            endpoint: std::env::var("ORCHESTRATOR_GRPC")
                .unwrap_or_else(|_| "http://localhost:50052".to_string()),
            timeout_secs: std::env::var("ORCHESTRATOR_TIMEOUT_SECS")
                .ok()
                .and_then(|s| s.parse().ok())
                .unwrap_or(300),
            tls_enabled: std::env::var("ORCHESTRATOR_TLS")
                .map(|s| s.to_lowercase() == "true")
                .unwrap_or(false),
            connect_timeout_secs: std::env::var("ORCHESTRATOR_CONNECT_TIMEOUT_SECS")
                .ok()
                .and_then(|s| s.parse().ok())
                .unwrap_or(10),
        }
    }
}

/// Task metadata for submission.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TaskMetadata {
    pub task_id: String,
    pub user_id: String,
    pub session_id: Option<String>,
    pub tenant_id: Option<String>,
    #[serde(default)]
    pub labels: HashMap<String, String>,
    pub max_agents: Option<i32>,
    pub token_budget: Option<f64>,
}

/// Execution mode for tasks.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum ExecutionMode {
    Unspecified,
    Simple,
    Standard,
    Complex,
}

/// Task status from orchestrator.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum TaskStatus {
    Unspecified,
    Queued,
    Running,
    Completed,
    Failed,
    Cancelled,
    Timeout,
    Paused,
}

/// Token usage information.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct TokenUsage {
    pub prompt_tokens: i32,
    pub completion_tokens: i32,
    pub total_tokens: i32,
    pub cost_usd: f64,
    pub model: String,
}

/// Execution metrics.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct ExecutionMetrics {
    pub latency_ms: i64,
    pub token_usage: Option<TokenUsage>,
    pub cache_hit: bool,
    pub cache_score: f64,
    pub agents_used: i32,
    pub mode: Option<ExecutionMode>,
}

/// Submit task request.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SubmitTaskRequest {
    pub metadata: TaskMetadata,
    pub query: String,
    #[serde(default)]
    pub context: serde_json::Value,
    pub auto_decompose: bool,
    pub require_approval: bool,
}

/// Submit task response.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SubmitTaskResponse {
    #[serde(default)]
    pub workflow_id: String,
    pub task_id: String,
    pub status: String,
    #[serde(default)]
    pub message: Option<String>,
}

/// Get task status request.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GetTaskStatusRequest {
    pub task_id: String,
    pub include_details: bool,
}

/// Agent task status.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AgentTaskStatus {
    pub agent_id: String,
    pub task_id: String,
    pub status: TaskStatus,
    pub result: Option<String>,
    pub token_usage: Option<TokenUsage>,
}

/// Get task status response.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GetTaskStatusResponse {
    pub task_id: String,
    pub status: TaskStatus,
    pub progress: f64,
    pub result: Option<String>,
    pub metrics: Option<ExecutionMetrics>,
    pub agent_statuses: Vec<AgentTaskStatus>,
    pub error_message: Option<String>,
}

/// Cancel task request.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CancelTaskRequest {
    pub task_id: String,
    pub reason: Option<String>,
}

/// Cancel task response.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CancelTaskResponse {
    pub success: bool,
    pub message: Option<String>,
}

/// Orchestrator gRPC client.
///
/// This client provides methods for interacting with the Go Orchestrator
/// service via gRPC.
pub struct OrchestratorClient {
    config: OrchestratorClientConfig,
    #[cfg(feature = "grpc")]
    client: Option<OrchestratorServiceClient<Channel>>,
    // HTTP client for fallback (when gRPC feature is disabled or connection fails)
    http_client: reqwest::Client,
}

impl OrchestratorClient {
    /// Create a new Orchestrator client.
    pub async fn new(config: OrchestratorClientConfig) -> anyhow::Result<Self> {
        let http_client = reqwest::Client::builder()
            .timeout(Duration::from_secs(config.timeout_secs))
            .build()
            .expect("Failed to create HTTP client");

        #[cfg(feature = "grpc")]
        {
            let endpoint = Endpoint::from_shared(config.endpoint.clone())?
                .timeout(Duration::from_secs(config.timeout_secs))
                .connect_timeout(Duration::from_secs(config.connect_timeout_secs));

            match endpoint.connect().await {
                Ok(channel) => {
                    let client = OrchestratorServiceClient::new(channel);
                    tracing::info!(
                        "Connected to orchestrator via gRPC at {}",
                        config.endpoint
                    );
                    return Ok(Self {
                        config,
                        client: Some(client),
                        http_client,
                    });
                }
                Err(e) => {
                    tracing::warn!(
                        "Failed to connect to orchestrator via gRPC: {}. Will retry on each request.",
                        e
                    );
                }
            }
        }

        Ok(Self {
            config,
            #[cfg(feature = "grpc")]
            client: None,
            http_client,
        })
    }

    /// Create a client with default configuration from environment.
    pub async fn from_env() -> anyhow::Result<Self> {
        Self::new(OrchestratorClientConfig::from_env()).await
    }

    /// Create a client with default configuration.
    pub async fn with_endpoint(endpoint: &str) -> anyhow::Result<Self> {
        Self::new(OrchestratorClientConfig {
            endpoint: endpoint.to_string(),
            ..Default::default()
        })
        .await
    }

    /// Get or create the gRPC client with lazy connection.
    #[cfg(feature = "grpc")]
    async fn get_client(&self) -> anyhow::Result<OrchestratorServiceClient<Channel>> {
        if let Some(ref client) = self.client {
            return Ok(client.clone());
        }

        // Try to connect
        let endpoint = Endpoint::from_shared(self.config.endpoint.clone())?
            .timeout(Duration::from_secs(self.config.timeout_secs))
            .connect_timeout(Duration::from_secs(self.config.connect_timeout_secs));

        let channel = endpoint.connect().await?;
        Ok(OrchestratorServiceClient::new(channel))
    }

    /// Submit a task to the orchestrator via gRPC.
    pub async fn submit_task(
        &self,
        request: SubmitTaskRequest,
    ) -> anyhow::Result<SubmitTaskResponse> {
        #[cfg(feature = "grpc")]
        {
            let mut client = self.get_client().await?;

            // Convert to protobuf types
            let proto_request = proto::orchestrator::SubmitTaskRequest {
                metadata: Some(proto::common::TaskMetadata {
                    task_id: request.metadata.task_id.clone(),
                    user_id: request.metadata.user_id.clone(),
                    session_id: request.metadata.session_id.clone().unwrap_or_default(),
                    tenant_id: request.metadata.tenant_id.clone().unwrap_or_default(),
                    created_at: None,
                    labels: request.metadata.labels.clone(),
                    max_agents: request.metadata.max_agents.unwrap_or(5),
                    token_budget: request.metadata.token_budget.unwrap_or(0.0),
                }),
                query: request.query.clone(),
                context: Some(serde_json_to_prost_struct(&request.context)),
                auto_decompose: request.auto_decompose,
                manual_decomposition: None,
                session_context: None,
                require_approval: request.require_approval,
            };

            let response = client
                .submit_task(tonic::Request::new(proto_request))
                .await?;

            let inner = response.into_inner();
            return Ok(SubmitTaskResponse {
                workflow_id: inner.workflow_id,
                task_id: inner.task_id,
                status: format!("{:?}", inner.status()),
                message: if inner.message.is_empty() {
                    None
                } else {
                    Some(inner.message)
                },
            });
        }

        #[cfg(not(feature = "grpc"))]
        {
            anyhow::bail!("gRPC feature is not enabled")
        }
    }

    /// Get the status of a task via gRPC.
    pub async fn get_task_status(
        &self,
        request: GetTaskStatusRequest,
    ) -> anyhow::Result<GetTaskStatusResponse> {
        #[cfg(feature = "grpc")]
        {
            let mut client = self.get_client().await?;

            let proto_request = proto::orchestrator::GetTaskStatusRequest {
                task_id: request.task_id.clone(),
                include_details: request.include_details,
            };

            let response = client
                .get_task_status(tonic::Request::new(proto_request))
                .await?;

            let inner = response.into_inner();
            return Ok(GetTaskStatusResponse {
                task_id: inner.task_id,
                status: proto_status_to_local(inner.status()),
                progress: inner.progress,
                result: if inner.result.is_empty() {
                    None
                } else {
                    Some(inner.result)
                },
                metrics: inner.metrics.map(proto_metrics_to_local),
                agent_statuses: inner
                    .agent_statuses
                    .into_iter()
                    .map(proto_agent_status_to_local)
                    .collect(),
                error_message: if inner.error_message.is_empty() {
                    None
                } else {
                    Some(inner.error_message)
                },
            });
        }

        #[cfg(not(feature = "grpc"))]
        {
            anyhow::bail!("gRPC feature is not enabled")
        }
    }

    /// Cancel a task via gRPC.
    pub async fn cancel_task(
        &self,
        request: CancelTaskRequest,
    ) -> anyhow::Result<CancelTaskResponse> {
        #[cfg(feature = "grpc")]
        {
            let mut client = self.get_client().await?;

            let proto_request = proto::orchestrator::CancelTaskRequest {
                task_id: request.task_id.clone(),
                reason: request.reason.clone().unwrap_or_default(),
            };

            let response = client
                .cancel_task(tonic::Request::new(proto_request))
                .await?;

            let inner = response.into_inner();
            return Ok(CancelTaskResponse {
                success: inner.success,
                message: if inner.message.is_empty() {
                    None
                } else {
                    Some(inner.message)
                },
            });
        }

        #[cfg(not(feature = "grpc"))]
        {
            anyhow::bail!("gRPC feature is not enabled")
        }
    }

    /// Pause a task via gRPC.
    pub async fn pause_task(&self, task_id: &str, reason: Option<&str>) -> anyhow::Result<bool> {
        #[cfg(feature = "grpc")]
        {
            let mut client = self.get_client().await?;

            let proto_request = proto::orchestrator::PauseTaskRequest {
                task_id: task_id.to_string(),
                reason: reason.unwrap_or_default().to_string(),
            };

            let response = client
                .pause_task(tonic::Request::new(proto_request))
                .await?;

            return Ok(response.into_inner().success);
        }

        #[cfg(not(feature = "grpc"))]
        {
            anyhow::bail!("gRPC feature is not enabled")
        }
    }

    /// Resume a paused task via gRPC.
    pub async fn resume_task(&self, task_id: &str, reason: Option<&str>) -> anyhow::Result<bool> {
        #[cfg(feature = "grpc")]
        {
            let mut client = self.get_client().await?;

            let proto_request = proto::orchestrator::ResumeTaskRequest {
                task_id: task_id.to_string(),
                reason: reason.unwrap_or_default().to_string(),
            };

            let response = client
                .resume_task(tonic::Request::new(proto_request))
                .await?;

            return Ok(response.into_inner().success);
        }

        #[cfg(not(feature = "grpc"))]
        {
            anyhow::bail!("gRPC feature is not enabled")
        }
    }

    /// Check if the orchestrator is healthy via gRPC.
    pub async fn health_check(&self) -> anyhow::Result<bool> {
        // Try to get task status for a non-existent task
        // If we get a response (even error), the service is up
        #[cfg(feature = "grpc")]
        {
            match self.get_client().await {
                Ok(_) => return Ok(true),
                Err(_) => return Ok(false),
            }
        }

        #[cfg(not(feature = "grpc"))]
        {
            // Fallback to HTTP health check
            let url = format!(
                "{}/health",
                self.config
                    .endpoint
                    .replace(":50052", ":8081")
                    .replace("50052", "8081")
            );

            match self
                .http_client
                .get(&url)
                .timeout(Duration::from_secs(5))
                .send()
                .await
            {
                Ok(response) => Ok(response.status().is_success()),
                Err(_) => Ok(false),
            }
        }
    }
}

impl std::fmt::Debug for OrchestratorClient {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("OrchestratorClient")
            .field("endpoint", &self.config.endpoint)
            .field("timeout_secs", &self.config.timeout_secs)
            .finish()
    }
}

// Helper functions for protobuf conversion

#[cfg(feature = "grpc")]
fn serde_json_to_prost_struct(value: &serde_json::Value) -> prost_types::Struct {
    use prost_types::value::Kind;
    use prost_types::Value;

    fn convert_value(v: &serde_json::Value) -> Value {
        let kind = match v {
            serde_json::Value::Null => Kind::NullValue(0),
            serde_json::Value::Bool(b) => Kind::BoolValue(*b),
            serde_json::Value::Number(n) => Kind::NumberValue(n.as_f64().unwrap_or(0.0)),
            serde_json::Value::String(s) => Kind::StringValue(s.clone()),
            serde_json::Value::Array(arr) => Kind::ListValue(prost_types::ListValue {
                values: arr.iter().map(convert_value).collect(),
            }),
            serde_json::Value::Object(obj) => Kind::StructValue(prost_types::Struct {
                fields: obj
                    .iter()
                    .map(|(k, v)| (k.clone(), convert_value(v)))
                    .collect(),
            }),
        };
        Value { kind: Some(kind) }
    }

    match value {
        serde_json::Value::Object(obj) => prost_types::Struct {
            fields: obj
                .iter()
                .map(|(k, v)| (k.clone(), convert_value(v)))
                .collect(),
        },
        _ => prost_types::Struct::default(),
    }
}

#[cfg(feature = "grpc")]
fn proto_status_to_local(status: proto::orchestrator::TaskStatus) -> TaskStatus {
    match status {
        proto::orchestrator::TaskStatus::Unspecified => TaskStatus::Unspecified,
        proto::orchestrator::TaskStatus::Queued => TaskStatus::Queued,
        proto::orchestrator::TaskStatus::Running => TaskStatus::Running,
        proto::orchestrator::TaskStatus::Completed => TaskStatus::Completed,
        proto::orchestrator::TaskStatus::Failed => TaskStatus::Failed,
        proto::orchestrator::TaskStatus::Cancelled => TaskStatus::Cancelled,
        proto::orchestrator::TaskStatus::Timeout => TaskStatus::Timeout,
        proto::orchestrator::TaskStatus::Paused => TaskStatus::Paused,
    }
}

#[cfg(feature = "grpc")]
fn proto_metrics_to_local(metrics: proto::common::ExecutionMetrics) -> ExecutionMetrics {
    ExecutionMetrics {
        latency_ms: metrics.latency_ms,
        token_usage: metrics.token_usage.map(|u| TokenUsage {
            prompt_tokens: u.prompt_tokens,
            completion_tokens: u.completion_tokens,
            total_tokens: u.total_tokens,
            cost_usd: u.cost_usd,
            model: u.model,
        }),
        cache_hit: metrics.cache_hit,
        cache_score: metrics.cache_score,
        agents_used: metrics.agents_used,
        mode: Some(match metrics.mode() {
            proto::common::ExecutionMode::Unspecified => ExecutionMode::Unspecified,
            proto::common::ExecutionMode::Simple => ExecutionMode::Simple,
            proto::common::ExecutionMode::Standard => ExecutionMode::Standard,
            proto::common::ExecutionMode::Complex => ExecutionMode::Complex,
        }),
    }
}

#[cfg(feature = "grpc")]
fn proto_agent_status_to_local(status: proto::orchestrator::AgentTaskStatus) -> AgentTaskStatus {
    AgentTaskStatus {
        agent_id: status.agent_id,
        task_id: status.task_id,
        status: proto_status_to_local(status.status()),
        result: if status.result.is_empty() {
            None
        } else {
            Some(status.result)
        },
        token_usage: status.token_usage.map(|u| TokenUsage {
            prompt_tokens: u.prompt_tokens,
            completion_tokens: u.completion_tokens,
            total_tokens: u.total_tokens,
            cost_usd: u.cost_usd,
            model: u.model,
        }),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_config_default() {
        let config = OrchestratorClientConfig::default();
        assert_eq!(config.endpoint, "http://localhost:50052");
        assert_eq!(config.timeout_secs, 300);
        assert!(!config.tls_enabled);
    }

    #[test]
    fn test_task_metadata() {
        let metadata = TaskMetadata {
            task_id: "test-123".to_string(),
            user_id: "user-1".to_string(),
            session_id: Some("session-1".to_string()),
            tenant_id: None,
            labels: HashMap::new(),
            max_agents: Some(5),
            token_budget: Some(10000.0),
        };
        assert_eq!(metadata.task_id, "test-123");
    }
}
