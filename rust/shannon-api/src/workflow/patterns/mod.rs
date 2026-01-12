//! Cognitive pattern management and execution.
//!
//! Provides infrastructure for registering, looking up, and executing
//! cognitive patterns (CoT, ToT, Research, ReAct, Debate, Reflection).
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::patterns::{PatternRegistry, PatternContext};
//!
//! let registry = PatternRegistry::new();
//!
//! // Register patterns
//! registry.register(Box::new(ChainOfThought::new()));
//! registry.register(Box::new(Research::new()));
//!
//! // Execute pattern
//! let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
//! let result = registry.execute("chain_of_thought", &ctx, "input").await?;
//! ```

pub mod base;
pub mod chain_of_thought;
pub mod debate;
pub mod react;
pub mod reflection;
pub mod research;
pub mod tree_of_thoughts;

pub use base::{
    CognitivePattern, PatternContext, PatternResult, ReasoningStep, Source, TokenUsage,
};
pub use chain_of_thought::ChainOfThought;
pub use debate::Debate;
pub use react::ReAct;
pub use reflection::Reflection;
pub use research::Research;
pub use tree_of_thoughts::TreeOfThoughts;

use std::collections::HashMap;
use std::sync::Arc;
use std::time::{Duration, Instant};

use anyhow::{Context as AnyhowContext, Result};
use parking_lot::RwLock;

/// Maximum retry attempts for transient failures.
const MAX_RETRIES: usize = 3;

/// Pattern registry for managing cognitive patterns.
///
/// Provides registration, lookup, and execution of cognitive patterns
/// with built-in retry logic, timeout enforcement, and metrics tracking.
///
/// # Thread Safety
///
/// Uses `parking_lot::RwLock` for efficient concurrent access. Multiple
/// threads can execute patterns concurrently.
#[derive(Clone)]
pub struct PatternRegistry {
    /// Registered patterns indexed by name.
    patterns: Arc<RwLock<HashMap<String, Arc<dyn CognitivePattern>>>>,
}

impl std::fmt::Debug for PatternRegistry {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("PatternRegistry")
            .field("pattern_count", &self.patterns.read().len())
            .finish()
    }
}

impl PatternRegistry {
    /// Create a new pattern registry.
    #[must_use]
    pub fn new() -> Self {
        Self {
            patterns: Arc::new(RwLock::new(HashMap::new())),
        }
    }

    /// Register a cognitive pattern.
    ///
    /// # Example
    ///
    /// ```rust,ignore
    /// registry.register(Box::new(ChainOfThought::new()));
    /// ```
    pub fn register(&self, pattern: Arc<dyn CognitivePattern>) {
        let name = pattern.name().to_string();
        let mut patterns = self.patterns.write();
        patterns.insert(name, pattern);
    }

    /// Get a pattern by name.
    ///
    /// # Errors
    ///
    /// Returns error if pattern not found.
    pub fn get(&self, name: &str) -> Result<Arc<dyn CognitivePattern>> {
        let patterns = self.patterns.read();
        patterns
            .get(name)
            .cloned()
            .ok_or_else(|| anyhow::anyhow!("Pattern not found: {name}"))
    }

    /// List all registered pattern names.
    #[must_use]
    pub fn list_patterns(&self) -> Vec<String> {
        let patterns = self.patterns.read();
        patterns.keys().cloned().collect()
    }

    /// Check if a pattern is registered.
    #[must_use]
    pub fn has_pattern(&self, name: &str) -> bool {
        let patterns = self.patterns.read();
        patterns.contains_key(name)
    }

    /// Execute a pattern with retry logic and timing.
    ///
    /// Automatically retries transient failures up to `MAX_RETRIES` times
    /// with exponential backoff. Tracks execution time and logs metrics.
    ///
    /// # Arguments
    ///
    /// * `pattern_name` - Name of pattern to execute
    /// * `ctx` - Pattern execution context
    /// * `input` - Input query or task description
    ///
    /// # Errors
    ///
    /// Returns error if pattern not found, execution fails after retries,
    /// or timeout is exceeded.
    ///
    /// # Example
    ///
    /// ```rust,ignore
    /// let result = registry.execute("chain_of_thought", &ctx, "query").await?;
    /// ```
    pub async fn execute(
        &self,
        pattern_name: &str,
        ctx: &PatternContext,
        input: &str,
    ) -> Result<PatternResult> {
        let pattern = self.get(pattern_name)?;

        // Validate input
        pattern.validate_input(input)?;

        // Execute with retry logic
        let start = Instant::now();
        let mut last_error = None;

        for attempt in 0..MAX_RETRIES {
            match self.execute_with_timeout(&pattern, ctx, input).await {
                Ok(result) => {
                    let duration = start.elapsed();
                    tracing::info!(
                        pattern = pattern_name,
                        workflow_id = %ctx.workflow_id,
                        duration_ms = duration.as_millis(),
                        attempt = attempt + 1,
                        "Pattern execution succeeded"
                    );
                    return Ok(result);
                }
                Err(e) => {
                    last_error = Some(e);

                    // Check if error is retryable
                    if !self.is_retryable_error(last_error.as_ref().unwrap()) {
                        break;
                    }

                    // Don't retry on last attempt
                    if attempt < MAX_RETRIES - 1 {
                        // Exponential backoff: 2^attempt seconds
                        #[allow(
                            clippy::cast_possible_truncation,
                            reason = "attempt is always < 32"
                        )]
                        let backoff = Duration::from_secs(2_u64.pow(attempt as u32));
                        tracing::warn!(
                            pattern = pattern_name,
                            workflow_id = %ctx.workflow_id,
                            attempt = attempt + 1,
                            backoff_ms = backoff.as_millis(),
                            error = %last_error.as_ref().unwrap(),
                            "Pattern execution failed, retrying"
                        );
                        tokio::time::sleep(backoff).await;
                    }
                }
            }
        }

        let duration = start.elapsed();
        tracing::error!(
            pattern = pattern_name,
            workflow_id = %ctx.workflow_id,
            duration_ms = duration.as_millis(),
            attempts = MAX_RETRIES,
            "Pattern execution failed after all retries"
        );

        Err(last_error.unwrap())
    }

    /// Execute pattern with timeout enforcement.
    async fn execute_with_timeout(
        &self,
        pattern: &Arc<dyn CognitivePattern>,
        ctx: &PatternContext,
        input: &str,
    ) -> Result<PatternResult> {
        let timeout = Duration::from_secs(ctx.timeout_seconds);

        tokio::time::timeout(timeout, pattern.execute(ctx, input))
            .await
            .context("Pattern execution timeout")?
    }

    /// Check if an error is retryable.
    ///
    /// Transient errors (network, timeout, rate limit) are retryable.
    /// Logic errors (invalid input, permanent failures) are not.
    fn is_retryable_error(&self, error: &anyhow::Error) -> bool {
        let error_str = error.to_string().to_lowercase();

        // Retryable: network, timeout, rate limit
        error_str.contains("network")
            || error_str.contains("timeout")
            || error_str.contains("rate limit")
            || error_str.contains("too many requests")
            || error_str.contains("temporary")
            || error_str.contains("transient")
    }
}

impl Default for PatternRegistry {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use ::async_trait::async_trait;

    // Mock pattern for testing
    #[derive(Debug)]
    struct MockPattern {
        name: String,
        should_fail: bool,
    }

    impl MockPattern {
        fn new(name: &str, should_fail: bool) -> Self {
            Self {
                name: name.to_string(),
                should_fail,
            }
        }
    }

    #[async_trait]
    impl CognitivePattern for MockPattern {
        fn name(&self) -> &str {
            &self.name
        }

        async fn execute(&self, _ctx: &PatternContext, input: &str) -> Result<PatternResult> {
            if self.should_fail {
                anyhow::bail!("Mock pattern failure");
            }

            Ok(PatternResult {
                output: format!("Result for: {input}"),
                reasoning_steps: vec![],
                sources: vec![],
                token_usage: None,
            })
        }
    }

    #[tokio::test]
    async fn test_new_registry() {
        let registry = PatternRegistry::new();
        assert_eq!(registry.list_patterns().len(), 0);
    }

    #[tokio::test]
    async fn test_register_pattern() {
        let registry = PatternRegistry::new();
        let pattern = Arc::new(MockPattern::new("test_pattern", false));

        registry.register(pattern);

        assert_eq!(registry.list_patterns().len(), 1);
        assert!(registry.has_pattern("test_pattern"));
    }

    #[tokio::test]
    async fn test_get_pattern() {
        let registry = PatternRegistry::new();
        let pattern = Arc::new(MockPattern::new("test_pattern", false));
        registry.register(pattern);

        let retrieved = registry.get("test_pattern");
        assert!(retrieved.is_ok());
        assert_eq!(retrieved.unwrap().name(), "test_pattern");
    }

    #[tokio::test]
    async fn test_get_nonexistent_pattern() {
        let registry = PatternRegistry::new();

        let result = registry.get("nonexistent");
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("not found"));
    }

    #[tokio::test]
    async fn test_execute_pattern() {
        let registry = PatternRegistry::new();
        let pattern = Arc::new(MockPattern::new("test_pattern", false));
        registry.register(pattern);

        let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
        let result = registry.execute("test_pattern", &ctx, "test input").await;

        assert!(result.is_ok());
        assert_eq!(result.unwrap().output, "Result for: test input");
    }

    #[tokio::test]
    async fn test_execute_with_retry() {
        let registry = PatternRegistry::new();

        // Pattern that fails with retryable error
        #[derive(Debug)]
        struct RetryablePattern {
            attempt: Arc<RwLock<usize>>,
        }

        #[async_trait]
        impl CognitivePattern for RetryablePattern {
            fn name(&self) -> &str {
                "retryable"
            }

            async fn execute(&self, _ctx: &PatternContext, _input: &str) -> Result<PatternResult> {
                let mut attempt = self.attempt.write();
                *attempt += 1;

                // Succeed on 3rd attempt
                if *attempt >= 3 {
                    Ok(PatternResult {
                        output: "success after retries".to_string(),
                        reasoning_steps: vec![],
                        sources: vec![],
                        token_usage: None,
                    })
                } else {
                    anyhow::bail!("Network error (transient)")
                }
            }
        }

        let pattern = Arc::new(RetryablePattern {
            attempt: Arc::new(RwLock::new(0)),
        });
        registry.register(pattern);

        let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
        let result = registry.execute("retryable", &ctx, "test").await;

        assert!(result.is_ok());
        assert_eq!(result.unwrap().output, "success after retries");
    }

    #[tokio::test]
    async fn test_execute_max_retries_exceeded() {
        let registry = PatternRegistry::new();
        let pattern = Arc::new(MockPattern::new("failing_pattern", true));
        registry.register(pattern);

        let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
        let result = registry.execute("failing_pattern", &ctx, "test").await;

        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_list_patterns() {
        let registry = PatternRegistry::new();

        registry.register(Arc::new(MockPattern::new("pattern1", false)));
        registry.register(Arc::new(MockPattern::new("pattern2", false)));
        registry.register(Arc::new(MockPattern::new("pattern3", false)));

        let patterns = registry.list_patterns();
        assert_eq!(patterns.len(), 3);
        assert!(patterns.contains(&"pattern1".to_string()));
        assert!(patterns.contains(&"pattern2".to_string()));
        assert!(patterns.contains(&"pattern3".to_string()));
    }

    #[tokio::test]
    async fn test_pattern_context_builder() {
        let ctx = PatternContext::new("wf-1".into(), "user-1".into(), Some("sess-1".into()))
            .with_max_iterations(10)
            .with_timeout(600);

        assert_eq!(ctx.workflow_id, "wf-1");
        assert_eq!(ctx.max_iterations, 10);
        assert_eq!(ctx.timeout_seconds, 600);
    }
}
