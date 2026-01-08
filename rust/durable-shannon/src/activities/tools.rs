//! Tool execution activities.
//!
//! Provides activities for executing tools like web search, code execution, etc.

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

use super::{Activity, ActivityContext, ActivityResult};

/// Tool execution request.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolRequest {
    /// Tool name.
    pub tool_name: String,
    /// Tool parameters.
    pub parameters: serde_json::Value,
    /// Timeout override in seconds.
    pub timeout_secs: Option<u64>,
}

/// Tool execution response.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolResponse {
    /// Tool output.
    pub output: serde_json::Value,
    /// Execution duration in milliseconds.
    pub duration_ms: u64,
    /// Whether the tool was cached.
    pub cached: bool,
}

/// Tool execution activity.
pub struct ToolExecuteActivity {
    /// HTTP client for tool calls.
    client: reqwest::Client,
    /// Tool service base URL.
    base_url: String,
}

impl ToolExecuteActivity {
    /// Create a new tool execute activity.
    #[must_use]
    pub fn new(base_url: String) -> Self {
        Self {
            client: reqwest::Client::new(),
            base_url,
        }
    }

    /// Create with default settings (local Shannon API).
    #[must_use]
    pub fn local() -> Self {
        Self::new("http://127.0.0.1:8765".to_string())
    }
}

impl std::fmt::Debug for ToolExecuteActivity {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ToolExecuteActivity")
            .field("base_url", &self.base_url)
            .finish()
    }
}

#[async_trait]
impl Activity for ToolExecuteActivity {
    fn name(&self) -> &'static str {
        "tool_execute"
    }

    async fn execute(
        &self,
        ctx: &ActivityContext,
        input: serde_json::Value,
    ) -> ActivityResult {
        let request: ToolRequest = match serde_json::from_value(input) {
            Ok(r) => r,
            Err(e) => return ActivityResult::failure(format!("Invalid input: {e}"), false),
        };

        tracing::debug!(
            "Tool execute activity {} running tool {}",
            ctx.activity_id,
            request.tool_name
        );

        let start = std::time::Instant::now();

        // Build tool call request
        let body = serde_json::json!({
            "tool": request.tool_name,
            "parameters": request.parameters
        });

        let timeout = request.timeout_secs.unwrap_or(ctx.timeout_secs);

        // Execute tool call
        let response = match tokio::time::timeout(
            std::time::Duration::from_secs(timeout),
            self.client
                .post(format!("{}/api/v1/tools/execute", self.base_url))
                .json(&body)
                .send(),
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
                return ActivityResult::failure(
                    format!("Tool {} timed out after {}s", request.tool_name, timeout),
                    true,
                )
            }
        };

        let duration_ms = start.elapsed().as_millis() as u64;

        if !response.status().is_success() {
            let status = response.status();
            let error_text = response.text().await.unwrap_or_default();
            return ActivityResult::failure(
                format!("Tool {} failed with {}: {}", request.tool_name, status, error_text),
                status.is_server_error(),
            );
        }

        let output: serde_json::Value = match response.json().await {
            Ok(o) => o,
            Err(e) => return ActivityResult::failure(format!("Invalid response: {e}"), true),
        };

        ActivityResult::success(ToolResponse {
            output,
            duration_ms,
            cached: false,
        })
    }
}

/// Web search activity.
pub struct WebSearchActivity {
    tool_activity: ToolExecuteActivity,
}

impl WebSearchActivity {
    /// Create a new web search activity.
    #[must_use]
    pub fn new(base_url: String) -> Self {
        Self {
            tool_activity: ToolExecuteActivity::new(base_url),
        }
    }
}

impl std::fmt::Debug for WebSearchActivity {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("WebSearchActivity").finish()
    }
}

#[async_trait]
impl Activity for WebSearchActivity {
    fn name(&self) -> &'static str {
        "web_search"
    }

    async fn execute(
        &self,
        ctx: &ActivityContext,
        input: serde_json::Value,
    ) -> ActivityResult {
        let query = input["query"].as_str().unwrap_or_default();
        let num_results = input["num_results"].as_u64().unwrap_or(10);

        let tool_input = serde_json::json!({
            "tool_name": "web_search",
            "parameters": {
                "query": query,
                "num_results": num_results
            }
        });

        self.tool_activity.execute(ctx, tool_input).await
    }
}
