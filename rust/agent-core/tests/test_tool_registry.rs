use shannon_agent_core::tool_registry::{ToolCapability, ToolDiscoveryRequest, ToolRegistry};

#[test]
fn test_tool_registry_initialization() {
    let registry = ToolRegistry::new();

    // Should have default tools registered
    let all_tools = registry.list_all_tools();
    assert!(all_tools.len() >= 3, "Should have at least 3 default tools");

    // Check for specific default tools
    let tool_ids: Vec<String> = all_tools.iter().map(|t| t.id.clone()).collect();
    assert!(tool_ids.contains(&"calculator".to_string()));
    assert!(tool_ids.contains(&"web_search".to_string()));
    assert!(tool_ids.contains(&"code_executor".to_string()));
}

#[test]
fn test_tool_discovery_by_query() {
    let registry = ToolRegistry::new();

    // Search for calculator
    let request = ToolDiscoveryRequest {
        query: Some("calc".to_string()),
        categories: None,
        tags: None,
        exclude_dangerous: None,
        max_results: None,
    };

    let results = registry.discover_tools(request);
    assert_eq!(results.len(), 1);
    assert_eq!(results[0].id, "calculator");
}

#[test]
fn test_tool_discovery_by_category() {
    let registry = ToolRegistry::new();

    // Search by category
    let request = ToolDiscoveryRequest {
        query: None,
        categories: Some(vec!["search".to_string()]),
        tags: None,
        exclude_dangerous: None,
        max_results: None,
    };

    let results = registry.discover_tools(request);
    assert_eq!(results.len(), 1);
    assert_eq!(results[0].id, "web_search");
}

#[test]
fn test_exclude_dangerous_tools() {
    let registry = ToolRegistry::new();

    // Get all tools including dangerous ones
    let request_all = ToolDiscoveryRequest {
        query: None,
        categories: None,
        tags: None,
        exclude_dangerous: Some(false),
        max_results: None,
    };

    let all_results = registry.discover_tools(request_all);
    let dangerous_count = all_results.iter().filter(|t| t.is_dangerous).count();
    assert!(
        dangerous_count > 0,
        "Should have at least one dangerous tool"
    );

    // Exclude dangerous tools
    let request_safe = ToolDiscoveryRequest {
        query: None,
        categories: None,
        tags: None,
        exclude_dangerous: Some(true),
        max_results: None,
    };

    let safe_results = registry.discover_tools(request_safe);
    let safe_dangerous_count = safe_results.iter().filter(|t| t.is_dangerous).count();
    assert_eq!(
        safe_dangerous_count, 0,
        "Should have no dangerous tools when excluded"
    );
}

#[test]
fn test_tool_registration() {
    let registry = ToolRegistry::new();

    // Register a new tool
    let custom_tool = ToolCapability {
        id: "custom_tool".to_string(),
        name: "Custom Tool".to_string(),
        description: "A custom test tool".to_string(),
        category: "test".to_string(),
        input_schema: serde_json::json!({}),
        output_schema: serde_json::json!({}),
        required_permissions: vec![],
        estimated_duration_ms: 100,
        is_dangerous: false,
        version: "1.0.0".to_string(),
        author: "test".to_string(),
        tags: vec!["test".to_string()],
        examples: vec![],
        rate_limit: None,
        cache_ttl_ms: None,
    };

    registry.register_tool(custom_tool.clone());

    // Verify it was registered
    let retrieved = registry.get_tool("custom_tool");
    assert!(retrieved.is_some());
    assert_eq!(retrieved.unwrap().name, "Custom Tool");
}

#[test]
fn test_max_results_limit() {
    let registry = ToolRegistry::new();

    // Get all tools with limit
    let request = ToolDiscoveryRequest {
        query: None,
        categories: None,
        tags: None,
        exclude_dangerous: None,
        max_results: Some(2),
    };

    let results = registry.discover_tools(request);
    assert_eq!(results.len(), 2, "Should limit results to 2");
}
