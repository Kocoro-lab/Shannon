//! Tool definitions and schemas.

use serde::{Deserialize, Serialize};

/// A tool definition for LLM function calling.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolDefinition {
    /// Tool name.
    pub name: String,
    /// Tool description.
    pub description: String,
    /// JSON schema for tool parameters.
    pub parameters: serde_json::Value,
    /// Whether this tool is enabled.
    #[serde(default = "default_enabled")]
    pub enabled: bool,
}

fn default_enabled() -> bool {
    true
}

impl ToolDefinition {
    /// Create a new tool definition.
    pub fn new(
        name: impl Into<String>,
        description: impl Into<String>,
        parameters: serde_json::Value,
    ) -> Self {
        Self {
            name: name.into(),
            description: description.into(),
            parameters,
            enabled: true,
        }
    }

    /// Convert to OpenAI function schema format.
    pub fn to_openai_schema(&self) -> serde_json::Value {
        serde_json::json!({
            "type": "function",
            "function": {
                "name": self.name,
                "description": self.description,
                "parameters": self.parameters
            }
        })
    }
}

/// Result of a tool execution.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolResult {
    /// Whether the execution was successful.
    pub success: bool,
    /// Output content.
    pub content: String,
    /// Error message if failed.
    pub error: Option<String>,
    /// Execution time in milliseconds.
    pub execution_time_ms: u64,
}

impl ToolResult {
    /// Create a successful result.
    pub fn success(content: impl Into<String>, execution_time_ms: u64) -> Self {
        Self {
            success: true,
            content: content.into(),
            error: None,
            execution_time_ms,
        }
    }

    /// Create a failed result.
    pub fn failure(error: impl Into<String>, execution_time_ms: u64) -> Self {
        let error_str = error.into();
        Self {
            success: false,
            content: format!("Error: {}", error_str),
            error: Some(error_str),
            execution_time_ms,
        }
    }
}
