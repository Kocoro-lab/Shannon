//! Workflow task types and handles.
//!
//! Provides types for task submission, status tracking, and result retrieval.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Task state enumeration.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum TaskState {
    /// Task is waiting to be processed.
    Pending,
    /// Task is currently being executed.
    Running,
    /// Task completed successfully.
    Completed,
    /// Task failed with an error.
    Failed,
    /// Task was cancelled.
    Cancelled,
    /// Task timed out.
    Timeout,
    /// Task is paused.
    Paused,
}

impl std::fmt::Display for TaskState {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Pending => write!(f, "pending"),
            Self::Running => write!(f, "running"),
            Self::Completed => write!(f, "completed"),
            Self::Failed => write!(f, "failed"),
            Self::Cancelled => write!(f, "cancelled"),
            Self::Timeout => write!(f, "timeout"),
            Self::Paused => write!(f, "paused"),
        }
    }
}

/// Research strategy for task execution.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Strategy {
    /// Simple direct execution.
    Simple,
    /// Standard single-agent execution.
    Standard,
    /// Complex multi-agent DAG execution.
    Complex,
    /// Chain of Thought reasoning.
    ChainOfThought,
    /// ReAct reasoning and acting.
    React,
    /// Tree of Thoughts exploration.
    TreeOfThoughts,
    /// Deep research with citations.
    Research,
    /// Scientific method with hypothesis testing.
    Scientific,
}

impl Default for Strategy {
    fn default() -> Self {
        Self::Standard
    }
}

impl std::fmt::Display for Strategy {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let s = match self {
            Self::Simple => "simple",
            Self::Standard => "standard",
            Self::Complex => "complex",
            Self::ChainOfThought => "chain_of_thought",
            Self::React => "react",
            Self::TreeOfThoughts => "tree_of_thoughts",
            Self::Research => "research",
            Self::Scientific => "scientific",
        };
        write!(f, "{}", s)
    }
}

impl std::str::FromStr for Strategy {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "simple" => Ok(Self::Simple),
            "standard" => Ok(Self::Standard),
            "complex" => Ok(Self::Complex),
            "chain_of_thought" | "chainofthought" | "cot" => Ok(Self::ChainOfThought),
            "react" => Ok(Self::React),
            "tree_of_thoughts" | "treeofthoughts" | "tot" => Ok(Self::TreeOfThoughts),
            "research" => Ok(Self::Research),
            "scientific" => Ok(Self::Scientific),
            _ => Err(format!("Unknown strategy: {s}")),
        }
    }
}

/// Task to be submitted to the workflow engine.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Task {
    /// Unique task identifier.
    pub id: String,
    /// User who submitted the task.
    pub user_id: String,
    /// Optional session for conversation continuity.
    pub session_id: Option<String>,
    /// The query or prompt to process.
    pub query: String,
    /// Execution strategy.
    pub strategy: Strategy,
    /// Additional context for the task.
    pub context: serde_json::Value,
    /// Task labels/metadata.
    pub labels: HashMap<String, String>,
    /// Maximum number of agents to use.
    pub max_agents: Option<u32>,
    /// Token budget for this task.
    pub token_budget: Option<f64>,
    /// Whether to require human approval.
    pub require_approval: bool,
}

impl Task {
    /// Create a new task with the given query.
    #[must_use]
    pub fn new(
        id: impl Into<String>,
        user_id: impl Into<String>,
        query: impl Into<String>,
    ) -> Self {
        Self {
            id: id.into(),
            user_id: user_id.into(),
            session_id: None,
            query: query.into(),
            strategy: Strategy::default(),
            context: serde_json::Value::Object(serde_json::Map::new()),
            labels: HashMap::new(),
            max_agents: None,
            token_budget: None,
            require_approval: false,
        }
    }

    /// Set the session ID.
    #[must_use]
    pub fn with_session(mut self, session_id: impl Into<String>) -> Self {
        self.session_id = Some(session_id.into());
        self
    }

    /// Set the strategy.
    #[must_use]
    pub fn with_strategy(mut self, strategy: Strategy) -> Self {
        self.strategy = strategy;
        self
    }

    /// Set the context.
    #[must_use]
    pub fn with_context(mut self, context: serde_json::Value) -> Self {
        self.context = context;
        self
    }
}

/// Handle for tracking a submitted task.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TaskHandle {
    /// Task ID.
    pub task_id: String,
    /// Workflow run ID (engine-specific).
    pub workflow_id: String,
    /// Current state.
    pub state: TaskState,
    /// Progress percentage (0-100).
    pub progress: u8,
    /// Optional message from the engine.
    pub message: Option<String>,
}

impl TaskHandle {
    /// Create a new task handle.
    #[must_use]
    pub fn new(task_id: impl Into<String>, workflow_id: impl Into<String>) -> Self {
        Self {
            task_id: task_id.into(),
            workflow_id: workflow_id.into(),
            state: TaskState::Pending,
            progress: 0,
            message: None,
        }
    }

    /// Check if the task is in a terminal state.
    #[must_use]
    pub fn is_terminal(&self) -> bool {
        matches!(
            self.state,
            TaskState::Completed | TaskState::Failed | TaskState::Cancelled | TaskState::Timeout
        )
    }
}

/// Result of a completed task.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TaskResult {
    /// Task ID.
    pub task_id: String,
    /// Final state.
    pub state: TaskState,
    /// Result content (for completed tasks).
    pub content: Option<String>,
    /// Structured output data.
    pub data: Option<serde_json::Value>,
    /// Error message (for failed tasks).
    pub error: Option<String>,
    /// Token usage statistics.
    pub token_usage: Option<TokenUsage>,
    /// Execution duration in milliseconds.
    pub duration_ms: u64,
    /// Sources and citations (for research tasks).
    pub sources: Vec<Source>,
}

/// Token usage statistics.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct TokenUsage {
    /// Input/prompt tokens.
    pub prompt_tokens: u32,
    /// Output/completion tokens.
    pub completion_tokens: u32,
    /// Total tokens.
    pub total_tokens: u32,
    /// Estimated cost in USD.
    pub cost_usd: f64,
}

/// Source or citation from research.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Source {
    /// Source title.
    pub title: String,
    /// Source URL.
    pub url: Option<String>,
    /// Relevant snippet.
    pub snippet: Option<String>,
    /// Confidence score (0-1).
    pub confidence: f64,
}
