//! Cognitive workflow patterns for Shannon AI platform.
//!
//! This crate provides implementations of various cognitive patterns:
//!
//! - **Chain of Thought (CoT)**: Step-by-step reasoning
//! - **ReAct**: Reason-Act-Observe loops
//! - **Tree of Thoughts (ToT)**: Branching exploration with backtracking
//! - **Debate**: Multi-perspective argumentation
//! - **Reflection**: Self-critique and improvement
//! - **Research**: Deep investigation with citations
//!
//! # Architecture
//!
//! Each pattern is implemented as a workflow that can be:
//! 1. Executed natively for testing
//! 2. Compiled to WASM for durable execution
//!
//! Patterns use activities from `durable-shannon` for LLM calls and tool execution.
//!
//! # Usage
//!
//! ```rust,ignore
//! use workflow_patterns::{ChainOfThought, ResearchInput, ResearchOutput};
//!
//! let input = ResearchInput {
//!     query: "What are the benefits of Rust?".to_string(),
//!     max_iterations: Some(5),
//!     model: None,
//! };
//!
//! let output = ChainOfThought::execute(input).await?;
//! println!("Answer: {}", output.answer);
//! ```

pub mod chain_of_thought;
pub mod debate;
pub mod react;
pub mod reflection;
pub mod research;
pub mod tree_of_thoughts;

// Re-exports
pub use chain_of_thought::ChainOfThought;
pub use debate::Debate;
pub use react::ReAct;
pub use reflection::Reflection;
pub use research::Research;
pub use tree_of_thoughts::TreeOfThoughts;

/// Input for cognitive patterns.
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct PatternInput {
    /// The query or question to process.
    pub query: String,
    /// Optional context for the query.
    pub context: Option<String>,
    /// Maximum iterations/steps.
    pub max_iterations: Option<usize>,
    /// Model to use for LLM calls.
    pub model: Option<String>,
    /// Temperature for sampling.
    pub temperature: Option<f32>,
    /// Additional configuration.
    pub config: Option<serde_json::Value>,
}

impl PatternInput {
    /// Create a new pattern input.
    #[must_use]
    pub fn new(query: impl Into<String>) -> Self {
        Self {
            query: query.into(),
            context: None,
            max_iterations: None,
            model: None,
            temperature: None,
            config: None,
        }
    }

    /// Set the context.
    #[must_use]
    pub fn with_context(mut self, context: impl Into<String>) -> Self {
        self.context = Some(context.into());
        self
    }

    /// Set the maximum iterations.
    #[must_use]
    pub fn with_max_iterations(mut self, max: usize) -> Self {
        self.max_iterations = Some(max);
        self
    }

    /// Set the model.
    #[must_use]
    pub fn with_model(mut self, model: impl Into<String>) -> Self {
        self.model = Some(model.into());
        self
    }
}

/// Output from cognitive patterns.
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct PatternOutput {
    /// The final answer or result.
    pub answer: String,
    /// Confidence score (0-1).
    pub confidence: f64,
    /// Reasoning steps taken.
    pub reasoning_steps: Vec<ReasoningStep>,
    /// Sources cited (for research patterns).
    pub sources: Vec<Source>,
    /// Token usage statistics.
    pub token_usage: TokenUsage,
    /// Execution duration in milliseconds.
    pub duration_ms: u64,
}

/// A reasoning step in the process.
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct ReasoningStep {
    /// Step number.
    pub step: usize,
    /// Type of step (thought, action, observation, etc.).
    pub step_type: String,
    /// Content of the step.
    pub content: String,
    /// Timestamp.
    pub timestamp: chrono::DateTime<chrono::Utc>,
}

/// A source or citation.
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct Source {
    /// Source title.
    pub title: String,
    /// Source URL (if available).
    pub url: Option<String>,
    /// Relevant snippet.
    pub snippet: Option<String>,
    /// Confidence in this source.
    pub confidence: f64,
}

/// Token usage statistics.
#[derive(Debug, Clone, Default, serde::Serialize, serde::Deserialize)]
pub struct TokenUsage {
    /// Prompt tokens used.
    pub prompt_tokens: u32,
    /// Completion tokens used.
    pub completion_tokens: u32,
    /// Total tokens used.
    pub total_tokens: u32,
    /// Estimated cost in USD.
    pub cost_usd: f64,
}

/// Trait for cognitive patterns.
#[async_trait::async_trait]
pub trait CognitivePattern: Send + Sync {
    /// Pattern name.
    fn name(&self) -> &'static str;

    /// Execute the pattern.
    async fn execute(&self, input: PatternInput) -> anyhow::Result<PatternOutput>;

    /// Get default configuration.
    fn default_config(&self) -> serde_json::Value {
        serde_json::json!({})
    }
}
