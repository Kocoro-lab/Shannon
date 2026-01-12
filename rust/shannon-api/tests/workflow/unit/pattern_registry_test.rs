//! Comprehensive unit tests for `PatternRegistry`.
//!
//! Tests pattern registration, lookup, execution with timing and metrics,
//! retry logic, error handling, and timeout enforcement.

use shannon_api::workflow::patterns::{
    CognitivePattern, PatternContext, PatternRegistry, PatternResult,
};
use async_trait::async_trait;
use parking_lot::RwLock;
use std::sync::Arc;
use std::time::Duration;
use tokio::time::timeout;

// =============================================================================
// Mock Patterns for Testing
// =============================================================================

/// Mock pattern that succeeds immediately.
#[derive(Debug)]
struct SuccessPattern {
    name: String,
}

impl SuccessPattern {
    fn new(name: &str) -> Self {
        Self {
            name: name.to_string(),
        }
    }
}

#[async_trait]
impl CognitivePattern for SuccessPattern {
    fn name(&self) -> &str {
        &self.name
    }

    async fn execute(&self, _ctx: &PatternContext, input: &str) -> anyhow::Result<PatternResult> {
        Ok(PatternResult {
            output: format!("Success result for: {input}"),
            reasoning_steps: vec![],
            sources: vec![],
            token_usage: None,
        })
    }
}

/// Mock pattern that always fails.
#[derive(Debug)]
struct FailingPattern {
    name: String,
    error_message: String,
}

impl FailingPattern {
    fn new(name: &str, error_message: &str) -> Self {
        Self {
            name: name.to_string(),
            error_message: error_message.to_string(),
        }
    }
}

#[async_trait]
impl CognitivePattern for FailingPattern {
    fn name(&self) -> &str {
        &self.name
    }

    async fn execute(&self, _ctx: &PatternContext, _input: &str) -> anyhow::Result<PatternResult> {
        anyhow::bail!("{}", self.error_message)
    }
}

/// Mock pattern that fails with retryable errors then succeeds.
#[derive(Debug)]
struct RetryablePattern {
    name: String,
    attempt_counter: Arc<RwLock<usize>>,
    succeed_on_attempt: usize,
}

impl RetryablePattern {
    fn new(name: &str, succeed_on_attempt: usize) -> Self {
        Self {
            name: name.to_string(),
            attempt_counter: Arc::new(RwLock::new(0)),
            succeed_on_attempt,
        }
    }
}

#[async_trait]
impl CognitivePattern for RetryablePattern {
    fn name(&self) -> &str {
        &self.name
    }

    async fn execute(&self, _ctx: &PatternContext, _input: &str) -> anyhow::Result<PatternResult> {
        let mut counter = self.attempt_counter.write();
        *counter += 1;
        let attempt = *counter;
        drop(counter);

        if attempt < self.succeed_on_attempt {
            anyhow::bail!("Network error (transient failure)")
        } else {
            Ok(PatternResult {
                output: format!("Success after {} attempts", attempt),
                reasoning_steps: vec![],
                sources: vec![],
                token_usage: None,
            })
        }
    }
}

/// Mock pattern that times out.
#[derive(Debug)]
struct SlowPattern {
    name: String,
    delay_secs: u64,
}

impl SlowPattern {
    fn new(name: &str, delay_secs: u64) -> Self {
        Self {
            name: name.to_string(),
            delay_secs,
        }
    }
}

#[async_trait]
impl CognitivePattern for SlowPattern {
    fn name(&self) -> &str {
        &self.name
    }

    async fn execute(&self, _ctx: &PatternContext, _input: &str) -> anyhow::Result<PatternResult> {
        tokio::time::sleep(Duration::from_secs(self.delay_secs)).await;
        Ok(PatternResult {
            output: "Slow result".to_string(),
            reasoning_steps: vec![],
            sources: vec![],
            token_usage: None,
        })
    }
}

// =============================================================================
// Unit Tests: Registry Creation
// =============================================================================

#[tokio::test]
async fn test_registry_creation() {
    let registry = PatternRegistry::new();
    assert_eq!(registry.list_patterns().len(), 0);
}

#[tokio::test]
async fn test_registry_default() {
    let registry = PatternRegistry::default();
    assert_eq!(registry.list_patterns().len(), 0);
}

// =============================================================================
// Unit Tests: Pattern Registration
// =============================================================================

#[tokio::test]
async fn test_register_single_pattern() {
    let registry = PatternRegistry::new();
    let pattern = Arc::new(SuccessPattern::new("test_pattern"));
    
    registry.register(pattern);
    
    assert_eq!(registry.list_patterns().len(), 1);
    assert!(registry.has_pattern("test_pattern"));
}

#[tokio::test]
async fn test_register_multiple_patterns() {
    let registry = PatternRegistry::new();
    
    registry.register(Arc::new(SuccessPattern::new("pattern_1")));
    registry.register(Arc::new(SuccessPattern::new("pattern_2")));
    registry.register(Arc::new(SuccessPattern::new("pattern_3")));
    
    assert_eq!(registry.list_patterns().len(), 3);
    assert!(registry.has_pattern("pattern_1"));
    assert!(registry.has_pattern("pattern_2"));
    assert!(registry.has_pattern("pattern_3"));
}

#[tokio::test]
async fn test_register_overwrites_existing() {
    let registry = PatternRegistry::new();
    
    registry.register(Arc::new(SuccessPattern::new("test")));
    registry.register(Arc::new(SuccessPattern::new("test"))); // Same name
    
    assert_eq!(registry.list_patterns().len(), 1);
}

// =============================================================================
// Unit Tests: Pattern Lookup
// =============================================================================

#[tokio::test]
async fn test_lookup_existing_pattern() {
    let registry = PatternRegistry::new();
    registry.register(Arc::new(SuccessPattern::new("test_pattern")));
    
    let result = registry.get("test_pattern");
    assert!(result.is_ok());
    assert_eq!(result.unwrap().name(), "test_pattern");
}

#[tokio::test]
async fn test_lookup_nonexistent_pattern() {
    let registry = PatternRegistry::new();
    
    let result = registry.get("nonexistent");
    assert!(result.is_err());
    assert!(result.unwrap_err().to_string().contains("not found"));
}

#[tokio::test]
async fn test_has_pattern() {
    let registry = PatternRegistry::new();
    registry.register(Arc::new(SuccessPattern::new("exists")));
    
    assert!(registry.has_pattern("exists"));
    assert!(!registry.has_pattern("does_not_exist"));
}

#[tokio::test]
async fn test_list_patterns() {
    let registry = PatternRegistry::new();
    
    registry.register(Arc::new(SuccessPattern::new("alpha")));
    registry.register(Arc::new(SuccessPattern::new("beta")));
    registry.register(Arc::new(SuccessPattern::new("gamma")));
    
    let patterns = registry.list_patterns();
    assert_eq!(patterns.len(), 3);
    assert!(patterns.contains(&"alpha".to_string()));
    assert!(patterns.contains(&"beta".to_string()));
    assert!(patterns.contains(&"gamma".to_string()));
}

// =============================================================================
// Unit Tests: Pattern Execution
// =============================================================================

#[tokio::test]
async fn test_execute_pattern_success() {
    let registry = PatternRegistry::new();
    registry.register(Arc::new(SuccessPattern::new("test")));
    
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
    let result = registry.execute("test", &ctx, "test input").await;
    
    assert!(result.is_ok());
    let output = result.unwrap();
    assert_eq!(output.output, "Success result for: test input");
}

#[tokio::test]
async fn test_execute_pattern_with_timing() {
    let registry = PatternRegistry::new();
    registry.register(Arc::new(SuccessPattern::new("test")));
    
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
    
    let start = std::time::Instant::now();
    let result = registry.execute("test", &ctx, "input").await;
    let duration = start.elapsed();
    
    assert!(result.is_ok());
    assert!(duration < Duration::from_secs(1), "Should complete quickly");
}

#[tokio::test]
async fn test_execute_nonexistent_pattern() {
    let registry = PatternRegistry::new();
    
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
    let result = registry.execute("nonexistent", &ctx, "input").await;
    
    assert!(result.is_err());
    assert!(result.unwrap_err().to_string().contains("not found"));
}

// =============================================================================
// Unit Tests: Retry Logic
// =============================================================================

#[tokio::test]
async fn test_retry_on_transient_failure() {
    let registry = PatternRegistry::new();
    
    // Pattern succeeds on 2nd attempt
    let pattern = Arc::new(RetryablePattern::new("retryable", 2));
    registry.register(pattern.clone());
    
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None)
        .with_timeout(10); // Short timeout for test
    
    let result = registry.execute("retryable", &ctx, "input").await;
    
    assert!(result.is_ok());
    let output = result.unwrap();
    assert!(output.output.contains("Success after 2 attempts"));
}

#[tokio::test]
async fn test_retry_succeeds_on_third_attempt() {
    let registry = PatternRegistry::new();
    
    // Pattern succeeds on 3rd attempt (last retry)
    let pattern = Arc::new(RetryablePattern::new("retryable", 3));
    registry.register(pattern);
    
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None)
        .with_timeout(10);
    
    let result = registry.execute("retryable", &ctx, "input").await;
    
    assert!(result.is_ok());
    assert!(result.unwrap().output.contains("Success after 3 attempts"));
}

#[tokio::test]
async fn test_max_retries_exceeded() {
    let registry = PatternRegistry::new();
    
    // Pattern never succeeds (needs 10 attempts but max is 3)
    let pattern = Arc::new(RetryablePattern::new("retryable", 10));
    registry.register(pattern);
    
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None)
        .with_timeout(10);
    
    let result = registry.execute("retryable", &ctx, "input").await;
    
    assert!(result.is_err());
    assert!(result.unwrap_err().to_string().contains("Network error"));
}

#[tokio::test]
async fn test_non_retryable_error_fails_immediately() {
    let registry = PatternRegistry::new();
    
    // Non-retryable error (doesn't contain network/timeout/rate limit)
    let pattern = Arc::new(FailingPattern::new("failing", "Invalid input error"));
    registry.register(pattern);
    
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
    
    let start = std::time::Instant::now();
    let result = registry.execute("failing", &ctx, "input").await;
    let duration = start.elapsed();
    
    assert!(result.is_err());
    // Should fail immediately without retries (no exponential backoff)
    assert!(duration < Duration::from_secs(1));
}

// =============================================================================
// Unit Tests: Timeout Handling
// =============================================================================

#[tokio::test]
async fn test_timeout_enforcement() {
    let registry = PatternRegistry::new();
    
    // Pattern takes 5 seconds
    let pattern = Arc::new(SlowPattern::new("slow", 5));
    registry.register(pattern);
    
    // Context with 1 second timeout
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None)
        .with_timeout(1);
    
    let result = registry.execute("slow", &ctx, "input").await;
    
    assert!(result.is_err());
    assert!(result.unwrap_err().to_string().contains("timeout"));
}

#[tokio::test]
async fn test_execution_within_timeout() {
    let registry = PatternRegistry::new();
    
    // Pattern takes 1 second
    let pattern = Arc::new(SlowPattern::new("slow", 1));
    registry.register(pattern);
    
    // Context with 5 second timeout
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None)
        .with_timeout(5);
    
    let result = registry.execute("slow", &ctx, "input").await;
    
    assert!(result.is_ok());
}

// =============================================================================
// Unit Tests: Pattern Context
// =============================================================================

#[tokio::test]
async fn test_pattern_context_creation() {
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
    
    assert_eq!(ctx.workflow_id, "wf-1");
    assert_eq!(ctx.user_id, "user-1");
    assert_eq!(ctx.session_id, None);
    assert_eq!(ctx.max_iterations, 5); // Default
    assert_eq!(ctx.timeout_seconds, 300); // Default 5 minutes
}

#[tokio::test]
async fn test_pattern_context_with_session() {
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), Some("sess-1".into()));
    
    assert_eq!(ctx.session_id, Some("sess-1".to_string()));
}

#[tokio::test]
async fn test_pattern_context_builder() {
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None)
        .with_max_iterations(10)
        .with_timeout(600);
    
    assert_eq!(ctx.max_iterations, 10);
    assert_eq!(ctx.timeout_seconds, 600);
}

// =============================================================================
// Integration Tests: Concurrent Execution
// =============================================================================

#[tokio::test]
async fn test_concurrent_pattern_execution() {
    let registry = PatternRegistry::new();
    registry.register(Arc::new(SuccessPattern::new("test")));
    
    let mut handles = vec![];
    
    for i in 0..10 {
        let registry = registry.clone();
        let handle = tokio::spawn(async move {
            let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
            registry.execute("test", &ctx, &format!("input {i}")).await
        });
        handles.push(handle);
    }
    
    for handle in handles {
        let result = handle.await.unwrap();
        assert!(result.is_ok());
    }
}

#[tokio::test]
async fn test_registry_thread_safety() {
    let registry = Arc::new(PatternRegistry::new());
    
    // Register patterns from multiple threads
    let mut handles = vec![];
    
    for i in 0..5 {
        let registry = Arc::clone(&registry);
        let handle = std::thread::spawn(move || {
            registry.register(Arc::new(SuccessPattern::new(&format!("pattern_{i}"))));
        });
        handles.push(handle);
    }
    
    for handle in handles {
        handle.join().unwrap();
    }
    
    assert_eq!(registry.list_patterns().len(), 5);
}

// =============================================================================
// Edge Cases
// =============================================================================

#[tokio::test]
async fn test_empty_input() {
    let registry = PatternRegistry::new();
    registry.register(Arc::new(SuccessPattern::new("test")));
    
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
    let result = registry.execute("test", &ctx, "").await;
    
    assert!(result.is_ok());
}

#[tokio::test]
async fn test_very_long_input() {
    let registry = PatternRegistry::new();
    registry.register(Arc::new(SuccessPattern::new("test")));
    
    let long_input = "x".repeat(10_000);
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
    let result = registry.execute("test", &ctx, &long_input).await;
    
    assert!(result.is_ok());
}

#[tokio::test]
async fn test_special_characters_in_input() {
    let registry = PatternRegistry::new();
    registry.register(Arc::new(SuccessPattern::new("test")));
    
    let special_input = "Test with émojis 🎉 and spëcial chars!";
    let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
    let result = registry.execute("test", &ctx, special_input).await;
    
    assert!(result.is_ok());
}

#[tokio::test]
async fn test_pattern_name_case_sensitivity() {
    let registry = PatternRegistry::new();
    registry.register(Arc::new(SuccessPattern::new("TestPattern")));
    
    assert!(registry.has_pattern("TestPattern"));
    assert!(!registry.has_pattern("testpattern"));
    assert!(!registry.has_pattern("TESTPATTERN"));
}
