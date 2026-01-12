//! Cognitive pattern trait definitions.
//!
//! Defines the core `CognitivePattern` trait that all patterns must implement.
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::patterns::{CognitivePattern, PatternContext, PatternResult};
//!
//! struct MyPattern;
//!
//! #[async_trait]
//! impl CognitivePattern for MyPattern {
//!     fn name(&self) -> &str {
//!         "my_pattern"
//!     }
//!     
//!     async fn execute(&self, ctx: &PatternContext, input: &str) -> anyhow::Result<PatternResult> {
//!         // Pattern logic here
//!         Ok(PatternResult {
//!             output: "result".to_string(),
//!             reasoning_steps: vec![],
//!             sources: vec![],
//!             token_usage: None,
//!         })
//!     }
//! }
//! ```

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

/// Context provided to patterns during execution.
///
/// Contains dependencies and configuration needed by patterns.
#[derive(Debug, Clone)]
pub struct PatternContext {
    /// Workflow identifier for tracking.
    pub workflow_id: String,

    /// User identifier.
    pub user_id: String,

    /// Session identifier for conversation context.
    pub session_id: Option<String>,

    /// Maximum iterations for iterative patterns.
    pub max_iterations: usize,

    /// Timeout for pattern execution (seconds).
    pub timeout_seconds: u64,
}

impl PatternContext {
    /// Create a new pattern context.
    #[must_use]
    pub fn new(workflow_id: String, user_id: String, session_id: Option<String>) -> Self {
        Self {
            workflow_id,
            user_id,
            session_id,
            max_iterations: 5,
            timeout_seconds: 300, // 5 minutes default
        }
    }

    /// Set maximum iterations.
    #[must_use]
    pub fn with_max_iterations(mut self, max_iterations: usize) -> Self {
        self.max_iterations = max_iterations;
        self
    }

    /// Set timeout in seconds.
    #[must_use]
    pub fn with_timeout(mut self, timeout_seconds: u64) -> Self {
        self.timeout_seconds = timeout_seconds;
        self
    }
}

/// Pattern execution result.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PatternResult {
    /// Final output or answer.
    pub output: String,

    /// Reasoning steps taken (for CoT, ToT).
    pub reasoning_steps: Vec<ReasoningStep>,

    /// Sources cited (for Research).
    pub sources: Vec<Source>,

    /// Token usage statistics.
    pub token_usage: Option<TokenUsage>,
}

/// Reasoning step in a cognitive pattern.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReasoningStep {
    /// Step number (0-indexed).
    pub step: usize,

    /// Step description or thought.
    pub content: String,

    /// Confidence score (0.0-1.0).
    pub confidence: Option<f32>,

    /// Timestamp.
    #[serde(with = "chrono::serde::ts_seconds")]
    pub timestamp: chrono::DateTime<chrono::Utc>,
}

/// Source citation.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Source {
    /// Source URL or identifier.
    pub url: String,

    /// Source title.
    pub title: Option<String>,

    /// Excerpt or snippet.
    pub excerpt: Option<String>,

    /// Relevance score (0.0-1.0).
    pub relevance: Option<f32>,
}

/// Token usage statistics.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TokenUsage {
    /// Prompt tokens consumed.
    pub prompt_tokens: usize,

    /// Completion tokens generated.
    pub completion_tokens: usize,

    /// Total tokens (prompt + completion).
    pub total_tokens: usize,
}

/// Cognitive pattern trait.
///
/// All cognitive patterns (CoT, ToT, Research, etc.) must implement this trait.
///
/// # Thread Safety
///
/// Patterns must be `Send + Sync` to enable concurrent execution.
#[async_trait]
pub trait CognitivePattern: Send + Sync + std::fmt::Debug {
    /// Get the pattern name.
    ///
    /// Used for pattern lookup and registration.
    fn name(&self) -> &str;

    /// Execute the pattern with the given input.
    ///
    /// # Arguments
    ///
    /// * `ctx` - Pattern execution context
    /// * `input` - Input query or task description
    ///
    /// # Errors
    ///
    /// Returns error if pattern execution fails.
    async fn execute(&self, ctx: &PatternContext, input: &str) -> anyhow::Result<PatternResult>;

    /// Get pattern description.
    ///
    /// Optional method for documentation and UI display.
    fn description(&self) -> Option<&str> {
        None
    }

    /// Validate input before execution.
    ///
    /// Optional method for input validation.
    ///
    /// # Errors
    ///
    /// Returns error if input is invalid.
    fn validate_input(&self, _input: &str) -> anyhow::Result<()> {
        Ok(())
    }
}
