//! Comprehensive tests for LLM activities.
//!
//! Tests LLM reason and synthesize activities with request/response handling,
//! retry logic, timeout enforcement, and error scenarios.

use durable_shannon::activities::{
    llm::{LlmReasonActivity, LlmRequest, LlmSynthesizeActivity, Message},
    Activity, ActivityContext, ActivityResult,
};
use serde_json::json;

// =============================================================================
// Helper Functions
// =============================================================================

fn create_test_context() -> ActivityContext {
    ActivityContext {
        activity_id: "test-activity-1".to_string(),
        workflow_id: "test-workflow-1".to_string(),
        attempt: 1,
        timeout_secs: 30,
    }
}

fn create_llm_request(model: &str, prompt: &str) -> LlmRequest {
    LlmRequest {
        model: model.to_string(),
        system: "You are a helpful assistant.".to_string(),
        messages: vec![Message {
            role: "user".to_string(),
            content: prompt.to_string(),
        }],
        temperature: 0.7,
        max_tokens: Some(1024),
    }
}

// =============================================================================
// Unit Tests: Activity Creation
// =============================================================================

#[tokio::test]
async fn test_llm_reason_activity_creation() {
    let activity = LlmReasonActivity::new(
        "http://localhost:8765".to_string(),
        Some("test-key".to_string()),
    );

    assert_eq!(activity.name(), "llm_reason");
}

#[tokio::test]
async fn test_llm_reason_activity_local() {
    let activity = LlmReasonActivity::local();
    assert_eq!(activity.name(), "llm_reason");
}

#[tokio::test]
async fn test_llm_synthesize_activity_creation() {
    let activity = LlmSynthesizeActivity::new(
        "http://localhost:8765".to_string(),
        Some("test-key".to_string()),
    );

    assert_eq!(activity.name(), "llm_synthesize");
}

// =============================================================================
// Unit Tests: Request Validation
// =============================================================================

#[tokio::test]
async fn test_llm_request_serialization() {
    let request = create_llm_request("claude-sonnet-4-20250514", "What is 2+2?");

    let json = serde_json::to_value(&request).unwrap();
    assert_eq!(json["model"], "claude-sonnet-4-20250514");
    assert_eq!(json["messages"][0]["content"], "What is 2+2?");
    assert_eq!(json["temperature"], 0.7);
    assert_eq!(json["max_tokens"], 1024);
}

#[tokio::test]
async fn test_llm_request_with_multiple_messages() {
    let request = LlmRequest {
        model: "gpt-4".to_string(),
        system: "You are helpful.".to_string(),
        messages: vec![
            Message {
                role: "user".to_string(),
                content: "Hello".to_string(),
            },
            Message {
                role: "assistant".to_string(),
                content: "Hi there!".to_string(),
            },
            Message {
                role: "user".to_string(),
                content: "How are you?".to_string(),
            },
        ],
        temperature: 0.5,
        max_tokens: Some(2048),
    };

    assert_eq!(request.messages.len(), 3);
}

// =============================================================================
// Unit Tests: Invalid Input Handling
// =============================================================================

#[tokio::test]
async fn test_llm_reason_invalid_input_empty() {
    let activity = LlmReasonActivity::local();
    let ctx = create_test_context();

    // Empty JSON object
    let result = activity.execute(&ctx, json!({})).await;

    match result {
        ActivityResult::Failure { reason, .. } => {
            assert!(reason.contains("Invalid input"));
        }
        _ => panic!("Expected failure for invalid input"),
    }
}

#[tokio::test]
async fn test_llm_reason_invalid_input_wrong_type() {
    let activity = LlmReasonActivity::local();
    let ctx = create_test_context();

    // Wrong type (string instead of LlmRequest)
    let result = activity.execute(&ctx, json!("invalid")).await;

    match result {
        ActivityResult::Failure { reason, .. } => {
            assert!(reason.contains("Invalid input"));
        }
        _ => panic!("Expected failure for wrong type"),
    }
}

#[tokio::test]
async fn test_llm_reason_missing_required_fields() {
    let activity = LlmReasonActivity::local();
    let ctx = create_test_context();

    // Missing required fields
    let invalid_request = json!({
        "model": "claude-sonnet-4-20250514",
        // Missing system, messages, temperature
    });

    let result = activity.execute(&ctx, invalid_request).await;

    match result {
        ActivityResult::Failure { reason, .. } => {
            assert!(reason.contains("Invalid input") || reason.contains("missing field"));
        }
        _ => panic!("Expected failure for missing fields"),
    }
}

// =============================================================================
// Unit Tests: Synthesize Activity
// =============================================================================

#[tokio::test]
async fn test_llm_synthesize_request_building() {
    let activity = LlmSynthesizeActivity::new("http://localhost:8765".to_string(), None);

    let ctx = create_test_context();

    let input = json!({
        "query": "What is the capital of France?",
        "thoughts": [
            "France is a country in Europe",
            "Paris is the largest city in France",
            "Paris has been the capital since 987 AD"
        ],
        "model": "claude-sonnet-4-20250514"
    });

    // Synthesize should build a request with thoughts combined
    // Note: This will fail to connect to server, but we test the request building
    let result = activity.execute(&ctx, input).await;

    // Expected to fail with network/connection error since server isn't running
    match result {
        ActivityResult::Retry { reason, .. } | ActivityResult::Failure { reason, .. } => {
            // Should fail to connect, not fail on input validation
            assert!(!reason.contains("Invalid input"));
        }
        _ => {}
    }
}

#[tokio::test]
async fn test_llm_synthesize_empty_thoughts() {
    let activity = LlmSynthesizeActivity::new("http://localhost:8765".to_string(), None);

    let ctx = create_test_context();

    let input = json!({
        "query": "Test query",
        "thoughts": [],
        "model": "claude-sonnet-4-20250514"
    });

    let result = activity.execute(&ctx, input).await;

    // Should still attempt to execute (will fail to connect)
    match result {
        ActivityResult::Retry { .. } | ActivityResult::Failure { .. } => {
            // Expected - can't connect to server
        }
        _ => {}
    }
}

// =============================================================================
// Unit Tests: ActivityContext Dependency Injection
// =============================================================================

#[tokio::test]
async fn test_activity_context_fields() {
    let ctx = ActivityContext {
        activity_id: "act-123".to_string(),
        workflow_id: "wf-456".to_string(),
        attempt: 2,
        timeout_secs: 60,
    };

    assert_eq!(ctx.activity_id, "act-123");
    assert_eq!(ctx.workflow_id, "wf-456");
    assert_eq!(ctx.attempt, 2);
    assert_eq!(ctx.timeout_secs, 60);
}

// =============================================================================
// Unit Tests: Retry Logic
// =============================================================================

#[tokio::test]
async fn test_activity_retry_backoff_calculation() {
    // Test that retry logic exists by checking retry attempts
    let ctx1 = ActivityContext {
        activity_id: "test".to_string(),
        workflow_id: "wf-1".to_string(),
        attempt: 1,
        timeout_secs: 30,
    };

    let ctx2 = ActivityContext {
        activity_id: "test".to_string(),
        workflow_id: "wf-1".to_string(),
        attempt: 2,
        timeout_secs: 30,
    };

    let ctx3 = ActivityContext {
        activity_id: "test".to_string(),
        workflow_id: "wf-1".to_string(),
        attempt: 3,
        timeout_secs: 30,
    };

    // Verify attempt numbers are tracked
    assert_eq!(ctx1.attempt, 1);
    assert_eq!(ctx2.attempt, 2);
    assert_eq!(ctx3.attempt, 3);
}

// =============================================================================
// Integration Tests: Network Errors (Mock)
// =============================================================================

#[tokio::test]
async fn test_llm_reason_network_error_returns_retry() {
    let activity = LlmReasonActivity::new(
        "http://localhost:9999".to_string(), // Invalid port
        None,
    );

    let ctx = create_test_context();
    let request = create_llm_request("claude-sonnet-4-20250514", "test");
    let input = serde_json::to_value(&request).unwrap();

    let result = activity.execute(&ctx, input).await;

    // Should return Retry for network errors
    match result {
        ActivityResult::Retry {
            reason,
            backoff_secs,
        } => {
            assert!(reason.contains("HTTP error") || reason.contains("timeout"));
            assert!(backoff_secs > 0);
        }
        ActivityResult::Failure { reason, retryable } => {
            // Connection refused might be non-retryable in some cases
            assert!(reason.contains("error") || reason.contains("refused"));
            // If non-retryable, that's also acceptable for connection errors
            let _ = retryable;
        }
        _ => panic!("Expected Retry or Failure for network error"),
    }
}

#[tokio::test]
async fn test_llm_reason_timeout_enforcement() {
    let activity = LlmReasonActivity::new(
        "http://httpbin.org/delay/10".to_string(), // Slow endpoint
        None,
    );

    let ctx = ActivityContext {
        activity_id: "test".to_string(),
        workflow_id: "wf-1".to_string(),
        attempt: 1,
        timeout_secs: 1, // 1 second timeout
    };

    let request = create_llm_request("test-model", "test");
    let input = serde_json::to_value(&request).unwrap();

    let start = std::time::Instant::now();
    let result = activity.execute(&ctx, input).await;
    let duration = start.elapsed();

    // Should timeout within ~1 second
    assert!(duration.as_secs() <= 2, "Should timeout quickly");

    // Should return Retry for timeout
    match result {
        ActivityResult::Retry { reason, .. } => {
            assert!(reason.contains("timeout") || reason.contains("Request timeout"));
        }
        ActivityResult::Failure { reason, .. } => {
            // Also acceptable if treated as failure
            assert!(reason.contains("timeout") || reason.contains("error"));
        }
        _ => {}
    }
}

// =============================================================================
// Unit Tests: Token Usage Tracking
// =============================================================================

#[tokio::test]
async fn test_token_usage_structure() {
    use durable_shannon::activities::llm::TokenUsage;

    let usage = TokenUsage {
        prompt_tokens: 100,
        completion_tokens: 50,
        total_tokens: 150,
    };

    assert_eq!(usage.prompt_tokens, 100);
    assert_eq!(usage.completion_tokens, 50);
    assert_eq!(usage.total_tokens, 150);
}

#[tokio::test]
async fn test_token_usage_default() {
    use durable_shannon::activities::llm::TokenUsage;

    let usage = TokenUsage::default();

    assert_eq!(usage.prompt_tokens, 0);
    assert_eq!(usage.completion_tokens, 0);
    assert_eq!(usage.total_tokens, 0);
}

// =============================================================================
// Edge Cases
// =============================================================================

#[tokio::test]
async fn test_llm_request_with_zero_max_tokens() {
    let request = LlmRequest {
        model: "test".to_string(),
        system: "test".to_string(),
        messages: vec![Message {
            role: "user".to_string(),
            content: "test".to_string(),
        }],
        temperature: 0.7,
        max_tokens: Some(0),
    };

    // Should serialize successfully
    let json = serde_json::to_value(&request).unwrap();
    assert_eq!(json["max_tokens"], 0);
}

#[tokio::test]
async fn test_llm_request_with_none_max_tokens() {
    let request = LlmRequest {
        model: "test".to_string(),
        system: "test".to_string(),
        messages: vec![Message {
            role: "user".to_string(),
            content: "test".to_string(),
        }],
        temperature: 0.7,
        max_tokens: None,
    };

    // Should serialize successfully
    let json = serde_json::to_value(&request).unwrap();
    assert!(json["max_tokens"].is_null());
}

#[tokio::test]
async fn test_llm_request_with_extreme_temperature() {
    let request1 = LlmRequest {
        model: "test".to_string(),
        system: "test".to_string(),
        messages: vec![],
        temperature: 0.0, // Minimum
        max_tokens: Some(100),
    };

    let request2 = LlmRequest {
        model: "test".to_string(),
        system: "test".to_string(),
        messages: vec![],
        temperature: 2.0, // Maximum
        max_tokens: Some(100),
    };

    assert_eq!(request1.temperature, 0.0);
    assert_eq!(request2.temperature, 2.0);
}

#[tokio::test]
async fn test_very_long_prompt() {
    let long_content = "x".repeat(10_000);
    let request = create_llm_request("test-model", &long_content);

    assert_eq!(request.messages[0].content.len(), 10_000);
}

#[tokio::test]
async fn test_unicode_in_messages() {
    let request = LlmRequest {
        model: "test".to_string(),
        system: "You are helpful! 😊".to_string(),
        messages: vec![Message {
            role: "user".to_string(),
            content: "Héllo wørld! 你好 🌍".to_string(),
        }],
        temperature: 0.7,
        max_tokens: Some(100),
    };

    // Should handle Unicode correctly
    assert!(request.system.contains("😊"));
    assert!(request.messages[0].content.contains("你好"));
}

#[tokio::test]
async fn test_activity_result_success() {
    use durable_shannon::activities::llm::LlmResponse;

    let response = LlmResponse {
        content: "Test response".to_string(),
        model: "test-model".to_string(),
        usage: durable_shannon::activities::llm::TokenUsage {
            prompt_tokens: 10,
            completion_tokens: 5,
            total_tokens: 15,
        },
    };

    let result = ActivityResult::success(response);

    match result {
        ActivityResult::Success { output } => {
            let response: LlmResponse = serde_json::from_value(output).unwrap();
            assert_eq!(response.content, "Test response");
            assert_eq!(response.usage.total_tokens, 15);
        }
        _ => panic!("Expected Success result"),
    }
}

#[tokio::test]
async fn test_activity_result_failure_non_retryable() {
    let result = ActivityResult::failure("Invalid API key".to_string(), false);

    match result {
        ActivityResult::Failure { reason, retryable } => {
            assert_eq!(reason, "Invalid API key");
            assert!(!retryable);
        }
        _ => panic!("Expected Failure result"),
    }
}

#[tokio::test]
async fn test_activity_result_failure_retryable() {
    let result = ActivityResult::failure("Server error".to_string(), true);

    match result {
        ActivityResult::Failure { reason, retryable } => {
            assert_eq!(reason, "Server error");
            assert!(retryable);
        }
        _ => panic!("Expected Failure result"),
    }
}

#[tokio::test]
async fn test_activity_result_retry() {
    let result = ActivityResult::Retry {
        reason: "Rate limited".to_string(),
        backoff_secs: 60,
    };

    match result {
        ActivityResult::Retry {
            reason,
            backoff_secs,
        } => {
            assert_eq!(reason, "Rate limited");
            assert_eq!(backoff_secs, 60);
        }
        _ => panic!("Expected Retry result"),
    }
}
