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

    async fn execute(&self, ctx: &ActivityContext, input: serde_json::Value) -> ActivityResult {
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
                format!(
                    "Tool {} failed with {}: {}",
                    request.tool_name, status, error_text
                ),
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

    async fn execute(&self, ctx: &ActivityContext, input: serde_json::Value) -> ActivityResult {
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

/// Calculator activity for mathematical operations.
pub struct CalculatorActivity;

impl std::fmt::Debug for CalculatorActivity {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("CalculatorActivity").finish()
    }
}

#[async_trait]
impl Activity for CalculatorActivity {
    fn name(&self) -> &'static str {
        "calculator"
    }

    async fn execute(&self, _ctx: &ActivityContext, input: serde_json::Value) -> ActivityResult {
        let expression = input["expression"].as_str().unwrap_or_default();

        if expression.is_empty() {
            return ActivityResult::failure("Expression cannot be empty", false);
        }

        // Simple calculator implementation (in production, use meval or similar)
        match Self::evaluate_simple(expression) {
            Ok(result) => ActivityResult::success(serde_json::json!({
                "result": result,
                "expression": expression
            })),
            Err(e) => ActivityResult::failure(format!("Calculation error: {e}"), false),
        }
    }
}

impl CalculatorActivity {
    /// Simple expression evaluator (supports +, -, *, /).
    fn evaluate_simple(expr: &str) -> Result<f64, String> {
        // Remove whitespace
        let expr = expr.replace(' ', "");

        // Try direct parse first
        if let Ok(num) = expr.parse::<f64>() {
            return Ok(num);
        }

        // Simple operator precedence (demo - production should use proper parser)
        if let Some(pos) = expr.rfind('+') {
            let left = Self::evaluate_simple(&expr[..pos])?;
            let right = Self::evaluate_simple(&expr[pos + 1..])?;
            return Ok(left + right);
        }

        if let Some(pos) = expr.rfind('-') {
            if pos > 0 {
                // Not a negative number
                let left = Self::evaluate_simple(&expr[..pos])?;
                let right = Self::evaluate_simple(&expr[pos + 1..])?;
                return Ok(left - right);
            }
        }

        if let Some(pos) = expr.rfind('*') {
            let left = Self::evaluate_simple(&expr[..pos])?;
            let right = Self::evaluate_simple(&expr[pos + 1..])?;
            return Ok(left * right);
        }

        if let Some(pos) = expr.rfind('/') {
            let left = Self::evaluate_simple(&expr[..pos])?;
            let right = Self::evaluate_simple(&expr[pos + 1..])?;
            if right == 0.0 {
                return Err("Division by zero".to_string());
            }
            return Ok(left / right);
        }

        Err(format!("Invalid expression: {expr}"))
    }
}

/// Web fetch activity for retrieving web page content.
pub struct WebFetchActivity {
    /// HTTP client.
    client: reqwest::Client,
}

impl WebFetchActivity {
    /// Create a new web fetch activity.
    #[must_use]
    pub fn new() -> Self {
        Self {
            client: reqwest::Client::builder()
                .timeout(std::time::Duration::from_secs(30))
                .build()
                .unwrap(),
        }
    }
}

impl Default for WebFetchActivity {
    fn default() -> Self {
        Self::new()
    }
}

impl std::fmt::Debug for WebFetchActivity {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("WebFetchActivity").finish()
    }
}

#[async_trait]
impl Activity for WebFetchActivity {
    fn name(&self) -> &'static str {
        "web_fetch"
    }

    async fn execute(&self, ctx: &ActivityContext, input: serde_json::Value) -> ActivityResult {
        let url = input["url"].as_str().unwrap_or_default();

        if url.is_empty() {
            return ActivityResult::failure("URL cannot be empty", false);
        }

        // Validate URL
        if !url.starts_with("http://") && !url.starts_with("https://") {
            return ActivityResult::failure("Invalid URL scheme (must be http or https)", false);
        }

        tracing::debug!(
            activity_id = %ctx.activity_id,
            url,
            "Fetching web content"
        );

        // Fetch content
        let response = match self.client.get(url).send().await {
            Ok(r) => r,
            Err(e) => {
                return ActivityResult::Retry {
                    reason: format!("HTTP error: {e}"),
                    backoff_secs: self.retry_backoff(ctx.attempt).as_secs(),
                }
            }
        };

        if !response.status().is_success() {
            return ActivityResult::failure(
                format!("HTTP {} from {}", response.status(), url),
                response.status().is_server_error(),
            );
        }

        let content = match response.text().await {
            Ok(c) => c,
            Err(e) => {
                return ActivityResult::failure(format!("Failed to read response: {e}"), true)
            }
        };

        // Truncate if too large
        let max_content_len = 50_000;
        let truncated = content.len() > max_content_len;
        let content = if truncated {
            &content[..max_content_len]
        } else {
            &content
        };

        ActivityResult::success(serde_json::json!({
            "url": url,
            "content": content,
            "truncated": truncated,
            "length": content.len()
        }))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_calculator_simple_addition() {
        let result = CalculatorActivity::evaluate_simple("2+3");
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), 5.0);
    }

    #[test]
    fn test_calculator_simple_subtraction() {
        let result = CalculatorActivity::evaluate_simple("10-3");
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), 7.0);
    }

    #[test]
    fn test_calculator_simple_multiplication() {
        let result = CalculatorActivity::evaluate_simple("4*5");
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), 20.0);
    }

    #[test]
    fn test_calculator_simple_division() {
        let result = CalculatorActivity::evaluate_simple("20/4");
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), 5.0);
    }

    #[test]
    fn test_calculator_division_by_zero() {
        let result = CalculatorActivity::evaluate_simple("10/0");
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("Division by zero"));
    }

    #[test]
    fn test_calculator_single_number() {
        let result = CalculatorActivity::evaluate_simple("42");
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), 42.0);
    }

    #[test]
    fn test_calculator_invalid_expression() {
        let result = CalculatorActivity::evaluate_simple("abc");
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_calculator_activity() {
        let calc = CalculatorActivity;
        let ctx = ActivityContext::default();

        let input = serde_json::json!({
            "expression": "2+2"
        });

        let result = calc.execute(&ctx, input).await;
        assert!(result.is_success());
    }

    #[tokio::test]
    async fn test_calculator_empty_expression() {
        let calc = CalculatorActivity;
        let ctx = ActivityContext::default();

        let input = serde_json::json!({
            "expression": ""
        });

        let result = calc.execute(&ctx, input).await;
        assert!(!result.is_success());
    }

    #[tokio::test]
    async fn test_web_fetch_invalid_url() {
        let fetch = WebFetchActivity::new();
        let ctx = ActivityContext::default();

        let input = serde_json::json!({
            "url": ""
        });

        let result = fetch.execute(&ctx, input).await;
        assert!(!result.is_success());
    }

    #[tokio::test]
    async fn test_web_fetch_invalid_scheme() {
        let fetch = WebFetchActivity::new();
        let ctx = ActivityContext::default();

        let input = serde_json::json!({
            "url": "ftp://example.com"
        });

        let result = fetch.execute(&ctx, input).await;
        assert!(!result.is_success());
    }
}
