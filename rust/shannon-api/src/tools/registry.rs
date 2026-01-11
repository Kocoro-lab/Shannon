//! Tool registration system.
//!
//! This module provides a registry for tools that can be used
//! during workflow execution.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::RwLock;

/// A tool definition.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Tool {
    /// Unique tool name.
    pub name: String,
    /// Tool description.
    pub description: String,
    /// JSON schema for parameters.
    pub parameters_schema: serde_json::Value,
    /// Whether the tool is enabled.
    pub enabled: bool,
    /// Optional tags for categorization.
    pub tags: Vec<String>,
}

/// Tool registry for managing available tools.
#[derive(Clone)]
pub struct ToolRegistry {
    /// Registered tools.
    tools: Arc<RwLock<HashMap<String, Tool>>>,
}

impl ToolRegistry {
    /// Create a new tool registry.
    #[must_use]
    pub fn new() -> Self {
        Self {
            tools: Arc::new(RwLock::new(HashMap::new())),
        }
    }

    /// Register a tool.
    ///
    /// # Errors
    ///
    /// Returns an error if the tool is invalid.
    pub async fn register(&self, tool: Tool) -> anyhow::Result<()> {
        if tool.name.is_empty() {
            anyhow::bail!("Tool name cannot be empty");
        }

        let mut tools = self.tools.write().await;
        tools.insert(tool.name.clone(), tool);
        Ok(())
    }

    /// Unregister a tool by name.
    pub async fn unregister(&self, name: &str) -> bool {
        let mut tools = self.tools.write().await;
        tools.remove(name).is_some()
    }

    /// Get a tool by name.
    pub async fn get(&self, name: &str) -> Option<Tool> {
        let tools = self.tools.read().await;
        tools.get(name).cloned()
    }

    /// List all registered tools.
    pub async fn list_all(&self) -> Vec<Tool> {
        let tools = self.tools.read().await;
        tools.values().cloned().collect()
    }

    /// List enabled tools.
    pub async fn list_enabled(&self) -> Vec<Tool> {
        let tools = self.tools.read().await;
        tools.values().filter(|t| t.enabled).cloned().collect()
    }

    /// Check if a tool is registered.
    pub async fn has_tool(&self, name: &str) -> bool {
        let tools = self.tools.read().await;
        tools.contains_key(name)
    }

    /// Enable or disable a tool.
    pub async fn set_enabled(&self, name: &str, enabled: bool) -> bool {
        let mut tools = self.tools.write().await;
        if let Some(tool) = tools.get_mut(name) {
            tool.enabled = enabled;
            true
        } else {
            false
        }
    }
}

impl Default for ToolRegistry {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_register_tool() {
        let registry = ToolRegistry::new();
        let tool = Tool {
            name: "test_tool".to_string(),
            description: "A test tool".to_string(),
            parameters_schema: serde_json::json!({}),
            enabled: true,
            tags: vec!["test".to_string()],
        };

        registry.register(tool.clone()).await.unwrap();
        let retrieved = registry.get("test_tool").await.unwrap();
        assert_eq!(retrieved.name, "test_tool");
    }

    #[tokio::test]
    async fn test_unregister_tool() {
        let registry = ToolRegistry::new();
        let tool = Tool {
            name: "test_tool".to_string(),
            description: "A test tool".to_string(),
            parameters_schema: serde_json::json!({}),
            enabled: true,
            tags: vec![],
        };

        registry.register(tool).await.unwrap();
        assert!(registry.unregister("test_tool").await);
        assert!(registry.get("test_tool").await.is_none());
    }
}
