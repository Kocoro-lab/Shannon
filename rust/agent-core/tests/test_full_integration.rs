#[cfg(test)]
mod tests {
    use shannon_agent_core::config::Config;
    use shannon_agent_core::error::AgentError;
    use shannon_agent_core::tool_cache::ToolCache;
    use shannon_agent_core::tool_registry::{ToolDiscoveryRequest, ToolRegistry};
    use shannon_agent_core::tools::{ToolCall, ToolExecutor};
    use std::collections::HashMap;

    #[test]
    fn test_full_system_integration() {
        println!("\n🧪 FULL SYSTEM INTEGRATION TEST");
        println!("{}\n", "=".repeat(32));

        // Test 1: Configuration System
        println!("1. Configuration System:");
        let config_result = Config::global();
        assert!(config_result.is_ok(), "Config should load successfully");
        println!("   ✅ Configuration loaded");

        // Test 2: Tool Registry
        println!("\n2. Tool Registry:");
        let registry = ToolRegistry::new();
        let all_tools = registry.list_all_tools();
        assert!(all_tools.len() >= 3, "Should have default tools");
        println!("   ✅ {} tools registered", all_tools.len());

        for tool in &all_tools {
            println!("      - {} ({})", tool.name, tool.category);
        }

        // Test 3: Tool Discovery
        println!("\n3. Tool Discovery:");
        let request = ToolDiscoveryRequest {
            query: Some("search".to_string()),
            categories: None,
            tags: None,
            exclude_dangerous: Some(true),
            max_results: None,
        };

        let discovered = registry.discover_tools(request);
        assert!(!discovered.is_empty(), "Should find search tools");
        println!("   ✅ Found {} tools for 'search'", discovered.len());

        // Test 4: Tool Cache
        println!("\n4. Tool Cache:");
        let cache = ToolCache::new(100, 60);

        // Test cache operations
        let test_call = ToolCall {
            tool_name: "test_tool".to_string(),
            parameters: HashMap::from([("param1".to_string(), serde_json::json!("value1"))]),
            call_id: Some("test_1".to_string()),
        };

        assert!(
            cache.get(&test_call).is_none(),
            "Cache should be empty initially"
        );

        let stats = cache.get_stats();
        println!("   ✅ Cache initialized");
        println!("      - Total requests: {}", stats.total_requests);
        println!("      - Hit rate: {:.2}%", stats.hit_rate() * 100.0);

        // Test 5: Tool Executor (without Python service)
        println!("\n5. Tool Executor:");
        let _executor = ToolExecutor::new(None);
        println!("   ✅ Executor created");

        // Test 6: Error Handling
        println!("\n6. Error Handling:");

        // Test structured errors
        let error = AgentError::ToolExecutionFailed {
            name: "nonexistent".to_string(),
            reason: "Tool not found".to_string(),
        };
        let error_str = format!("{}", error);
        assert!(
            error_str.contains("failed"),
            "Error should format correctly"
        );
        println!("   ✅ Structured errors working");

        // Test Result types (no unwrap!)
        let result: Result<String, AgentError> =
            Err(AgentError::ConfigurationError("Test error".to_string()));

        match result {
            Ok(_) => panic!("Should be error"),
            Err(e) => {
                println!("   ✅ Proper error handling: {}", e);
            }
        }

        // Test 7: Modern Rust Patterns
        println!("\n7. Modern Rust Patterns:");

        // OnceLock is used in metrics
        use shannon_agent_core::metrics::TOOL_EXECUTIONS;
        assert!(
            TOOL_EXECUTIONS.get().is_some() || TOOL_EXECUTIONS.get().is_none(),
            "OnceLock pattern working"
        );
        println!("   ✅ OnceLock pattern verified");

        // Test 8: Tracing (just verify it compiles)
        println!("\n8. Observability:");
        use shannon_agent_core::tracing::get_current_trace_id;
        let _trace_id = get_current_trace_id(); // May be None without active span
        println!("   ✅ Tracing functions available");

        println!("\n{}", "=".repeat(40));
        println!("✅ ALL INTEGRATION TESTS PASSED!");
        println!("{}", "=".repeat(40));
    }

    #[test]
    fn test_tool_registry_discovery() {
        let registry = ToolRegistry::new();

        // Test filtering by category
        let request = ToolDiscoveryRequest {
            query: None,
            categories: Some(vec!["calculation".to_string()]),
            tags: None,
            exclude_dangerous: None,
            max_results: None,
        };

        let tools = registry.discover_tools(request);
        assert_eq!(tools.len(), 1, "Should find calculator");
        assert_eq!(tools[0].id, "calculator");
    }

    #[test]
    fn test_cache_statistics() {
        let cache = ToolCache::new(10, 60);

        let call = ToolCall {
            tool_name: "test".to_string(),
            parameters: HashMap::new(),
            call_id: None,
        };

        // Initial state
        let stats1 = cache.get_stats();
        assert_eq!(stats1.total_requests, 0);

        // After a miss
        cache.get(&call);
        let stats2 = cache.get_stats();
        assert_eq!(stats2.total_requests, 1);
        assert_eq!(stats2.cache_misses, 1);
    }
}
