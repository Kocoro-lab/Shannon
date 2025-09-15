use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use tracing::{info, instrument};

/// Tool capability metadata
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolCapability {
    pub id: String,
    pub name: String,
    pub description: String,
    pub category: String,
    pub input_schema: serde_json::Value,
    pub output_schema: serde_json::Value,
    pub required_permissions: Vec<String>,
    pub estimated_duration_ms: u64,
    pub is_dangerous: bool,
    pub version: String,
    pub author: String,
    pub tags: Vec<String>,
    pub examples: Vec<ToolExample>,
    pub rate_limit: Option<RateLimit>,
    pub cache_ttl_ms: Option<u64>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolExample {
    pub description: String,
    pub input: serde_json::Value,
    pub output: serde_json::Value,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RateLimit {
    pub requests_per_minute: u32,
    pub requests_per_hour: u32,
}

/// Tool discovery request
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolDiscoveryRequest {
    pub query: Option<String>,
    pub categories: Option<Vec<String>>,
    pub tags: Option<Vec<String>>,
    pub exclude_dangerous: Option<bool>,
    pub max_results: Option<usize>,
}

/// Tool discovery response
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolDiscoveryResponse {
    pub tools: Vec<ToolCapability>,
}

/// Tool registry for capability management
pub struct ToolRegistry {
    tools: Arc<RwLock<HashMap<String, ToolCapability>>>,
}

impl ToolRegistry {
    pub fn new() -> Self {
        let mut registry = Self {
            tools: Arc::new(RwLock::new(HashMap::new())),
        };

        // Initialize with default tools
        registry.init_default_tools();
        registry
    }

    fn init_default_tools(&mut self) {
        // Calculator tool
        let calculator = ToolCapability {
            id: "calculator".to_string(),
            name: "Calculator".to_string(),
            description: "Perform mathematical calculations".to_string(),
            category: "calculation".to_string(),
            input_schema: serde_json::json!({
                "type": "object",
                "properties": {
                    "expression": {
                        "type": "string",
                        "description": "Mathematical expression to evaluate"
                    }
                },
                "required": ["expression"]
            }),
            output_schema: serde_json::json!({
                "type": "object",
                "properties": {
                    "result": {
                        "type": "number"
                    }
                }
            }),
            required_permissions: vec![],
            estimated_duration_ms: 100,
            is_dangerous: false,
            version: "1.0.0".to_string(),
            author: "shannon-core".to_string(),
            tags: vec!["math".to_string(), "calculation".to_string()],
            examples: vec![ToolExample {
                description: "Simple addition".to_string(),
                input: serde_json::json!({"expression": "2 + 2"}),
                output: serde_json::json!({"result": 4}),
            }],
            rate_limit: None,
            cache_ttl_ms: Some(3600000), // 1 hour
        };

        // Web search tool
        let web_search = ToolCapability {
            id: "web_search".to_string(),
            name: "Web Search".to_string(),
            description: "Search the web for information".to_string(),
            category: "search".to_string(),
            input_schema: serde_json::json!({
                "type": "object",
                "properties": {
                    "query": {
                        "type": "string",
                        "description": "Search query"
                    },
                    "max_results": {
                        "type": "integer",
                        "description": "Maximum number of results"
                    }
                },
                "required": ["query"]
            }),
            output_schema: serde_json::json!({
                "type": "array",
                "items": {
                    "type": "object",
                    "properties": {
                        "title": {"type": "string"},
                        "url": {"type": "string"},
                        "snippet": {"type": "string"}
                    }
                }
            }),
            required_permissions: vec!["internet".to_string()],
            estimated_duration_ms: 2000,
            is_dangerous: false,
            version: "1.0.0".to_string(),
            author: "shannon-core".to_string(),
            tags: vec![
                "search".to_string(),
                "web".to_string(),
                "internet".to_string(),
            ],
            examples: vec![],
            rate_limit: Some(RateLimit {
                requests_per_minute: 60,
                requests_per_hour: 1000,
            }),
            cache_ttl_ms: Some(600000), // 10 minutes
        };

        // Code executor tool (WASI)
        let code_executor = ToolCapability {
            id: "code_executor".to_string(),
            name: "Code Executor".to_string(),
            description: "Execute code in a secure WASI sandbox".to_string(),
            category: "code".to_string(),
            input_schema: serde_json::json!({
                "type": "object",
                "properties": {
                    "wasm_path": {
                        "type": "string",
                        "description": "Path to WASM module"
                    },
                    "wasm_base64": {
                        "type": "string",
                        "description": "Base64-encoded WASM module"
                    },
                    "stdin": {
                        "type": "string",
                        "description": "Standard input for the program"
                    }
                }
            }),
            output_schema: serde_json::json!({
                "type": "string",
                "description": "Program output"
            }),
            required_permissions: vec!["wasi".to_string(), "filesystem".to_string()],
            estimated_duration_ms: 5000,
            is_dangerous: true,
            version: "1.0.0".to_string(),
            author: "shannon-core".to_string(),
            tags: vec![
                "code".to_string(),
                "wasi".to_string(),
                "sandbox".to_string(),
            ],
            examples: vec![],
            rate_limit: Some(RateLimit {
                requests_per_minute: 10,
                requests_per_hour: 100,
            }),
            cache_ttl_ms: None,
        };

        self.register_tool(calculator);
        self.register_tool(web_search);
        self.register_tool(code_executor);
    }

    /// Register a new tool capability
    #[instrument(skip(self, capability), fields(tool_id = %capability.id))]
    pub fn register_tool(&self, capability: ToolCapability) {
        let mut tools = self.tools.write().unwrap();
        info!(
            "Registering tool: {} ({})",
            capability.id, capability.category
        );
        tools.insert(capability.id.clone(), capability);
    }

    /// Get a specific tool capability
    pub fn get_tool(&self, tool_id: &str) -> Option<ToolCapability> {
        let tools = self.tools.read().unwrap();
        tools.get(tool_id).cloned()
    }

    /// List all available tools
    pub fn list_all_tools(&self) -> Vec<ToolCapability> {
        let tools = self.tools.read().unwrap();
        tools.values().cloned().collect()
    }

    /// Discover tools based on query and filters
    #[instrument(skip(self, request))]
    pub fn discover_tools(&self, request: ToolDiscoveryRequest) -> Vec<ToolCapability> {
        let tools = self.tools.read().unwrap();
        let mut matching_tools = Vec::new();

        for tool in tools.values() {
            // Apply filters
            if let Some(exclude_dangerous) = request.exclude_dangerous {
                if exclude_dangerous && tool.is_dangerous {
                    continue;
                }
            }

            if let Some(categories) = &request.categories {
                if !categories.contains(&tool.category) {
                    continue;
                }
            }

            if let Some(tags) = &request.tags {
                let has_tag = tags.iter().any(|t| tool.tags.contains(t));
                if !has_tag {
                    continue;
                }
            }

            // Calculate relevance score
            let mut matches = false;

            if let Some(query) = &request.query {
                let query_lower = query.to_lowercase();

                if tool.name.to_lowercase().contains(&query_lower)
                    || tool.description.to_lowercase().contains(&query_lower)
                    || tool.category.to_lowercase().contains(&query_lower)
                    || tool
                        .tags
                        .iter()
                        .any(|t| t.to_lowercase().contains(&query_lower))
                {
                    matches = true;
                }
            } else {
                matches = true; // No query means match all
            }

            if matches {
                matching_tools.push(tool.clone());
            }
        }

        // Apply max_results limit if specified
        if let Some(max) = request.max_results {
            matching_tools.truncate(max);
        }

        matching_tools
    }

    /// Get statistics about registered tools
    pub fn get_stats(&self) -> HashMap<String, usize> {
        let tools = self.tools.read().unwrap();
        let mut stats = HashMap::new();

        stats.insert("total".to_string(), tools.len());

        // Count by category
        let mut category_counts: HashMap<String, usize> = HashMap::new();
        for tool in tools.values() {
            *category_counts.entry(tool.category.clone()).or_insert(0) += 1;
        }

        for (category, count) in category_counts {
            stats.insert(format!("category_{}", category), count);
        }

        stats
    }
}

impl Default for ToolRegistry {
    fn default() -> Self {
        Self::new()
    }
}
