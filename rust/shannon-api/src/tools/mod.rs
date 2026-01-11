//! Tool execution infrastructure.
//!
//! This module provides the tool registry and built-in tool implementations.

pub mod cache;
pub mod registry;
pub mod security;

pub use cache::{CacheKey, CacheStats, CachedResult, ToolCache};
pub use registry::ToolRegistry as AdvancedToolRegistry;
pub use security::{SecurityPolicy, ToolSecurity};

use std::collections::HashMap;
use std::sync::Arc;

use async_trait::async_trait;
use parking_lot::RwLock;

use crate::domain::ToolDefinition;

/// Trait for executable tools.
#[async_trait]
pub trait Tool: Send + Sync {
    /// Get the tool definition.
    fn definition(&self) -> ToolDefinition;

    /// Execute the tool with the given arguments.
    async fn execute(&self, arguments: &str) -> anyhow::Result<String>;
}

/// Registry of available tools.
#[derive(Default)]
pub struct ToolRegistry {
    tools: RwLock<HashMap<String, Arc<dyn Tool>>>,
}

impl std::fmt::Debug for ToolRegistry {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let tools = self.tools.read();
        f.debug_struct("ToolRegistry")
            .field("tools", &tools.keys().collect::<Vec<_>>())
            .finish()
    }
}

impl ToolRegistry {
    /// Create a new empty tool registry.
    pub fn new() -> Self {
        Self {
            tools: RwLock::new(HashMap::new()),
        }
    }

    /// Create a registry with default built-in tools.
    pub fn with_defaults() -> Self {
        let registry = Self::new();

        // Register built-in tools
        registry.register(Arc::new(CalculatorTool));
        registry.register(Arc::new(CurrentTimeTool));

        registry
    }

    /// Register a tool.
    pub fn register(&self, tool: Arc<dyn Tool>) {
        let mut tools = self.tools.write();
        let name = tool.definition().name.clone();
        tools.insert(name, tool);
    }

    /// Get a tool by name.
    pub fn get(&self, name: &str) -> Option<Arc<dyn Tool>> {
        let tools = self.tools.read();
        tools.get(name).cloned()
    }

    /// Get all tool definitions.
    pub fn get_definitions(&self) -> Vec<ToolDefinition> {
        let tools = self.tools.read();
        tools.values().map(|t| t.definition()).collect()
    }

    /// Get tool schemas in OpenAI format.
    pub fn get_tool_schemas(&self) -> Vec<serde_json::Value> {
        self.get_definitions()
            .iter()
            .filter(|d| d.enabled)
            .map(|d| d.to_openai_schema())
            .collect()
    }

    /// Execute a tool by name.
    pub async fn execute(&self, name: &str, arguments: &str) -> anyhow::Result<String> {
        let tool = self
            .get(name)
            .ok_or_else(|| anyhow::anyhow!("Tool not found: {}", name))?;

        tool.execute(arguments).await
    }

    /// List all registered tool names.
    pub fn list_tools(&self) -> Vec<String> {
        let tools = self.tools.read();
        tools.keys().cloned().collect()
    }
}

/// Calculator tool for basic math operations.
#[derive(Debug)]
pub struct CalculatorTool;

#[async_trait]
impl Tool for CalculatorTool {
    fn definition(&self) -> ToolDefinition {
        ToolDefinition::new(
            "calculator",
            "Evaluate a mathematical expression. Supports basic arithmetic (+, -, *, /), powers (^), and common functions (sin, cos, tan, sqrt, ln, log, abs).",
            serde_json::json!({
                "type": "object",
                "properties": {
                    "expression": {
                        "type": "string",
                        "description": "The mathematical expression to evaluate"
                    }
                },
                "required": ["expression"]
            }),
        )
    }

    async fn execute(&self, arguments: &str) -> anyhow::Result<String> {
        let args: serde_json::Value = serde_json::from_str(arguments)?;
        let expression = args["expression"]
            .as_str()
            .ok_or_else(|| anyhow::anyhow!("Missing expression"))?;

        // Simple expression evaluation using meval would go here
        // For now, we'll return a placeholder
        let result = format!(
            "Evaluated: {} (calculator implementation pending)",
            expression
        );
        Ok(result)
    }
}

/// Current time tool.
#[derive(Debug)]
pub struct CurrentTimeTool;

#[async_trait]
impl Tool for CurrentTimeTool {
    fn definition(&self) -> ToolDefinition {
        ToolDefinition::new(
            "current_time",
            "Get the current date and time in various formats.",
            serde_json::json!({
                "type": "object",
                "properties": {
                    "timezone": {
                        "type": "string",
                        "description": "Timezone (e.g., 'UTC', 'America/New_York'). Defaults to UTC."
                    },
                    "format": {
                        "type": "string",
                        "description": "Output format: 'iso', 'rfc2822', 'unix', or custom strftime format. Defaults to 'iso'."
                    }
                },
                "required": []
            }),
        )
    }

    async fn execute(&self, arguments: &str) -> anyhow::Result<String> {
        let args: serde_json::Value = serde_json::from_str(arguments).unwrap_or_default();
        let format = args["format"].as_str().unwrap_or("iso");

        let now = chrono::Utc::now();

        let result = match format {
            "iso" => now.to_rfc3339(),
            "rfc2822" => now.to_rfc2822(),
            "unix" => now.timestamp().to_string(),
            custom => now.format(custom).to_string(),
        };

        Ok(serde_json::json!({
            "current_time": result,
            "timezone": "UTC"
        })
        .to_string())
    }
}
