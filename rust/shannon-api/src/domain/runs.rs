//! Run state machine and lifecycle management.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

/// A run represents a single execution of an agent task.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Run {
    /// Unique run identifier.
    pub id: String,
    /// Session ID this run belongs to.
    pub session_id: Option<String>,
    /// User ID who initiated the run.
    pub user_id: Option<String>,
    /// Current status of the run.
    pub status: RunStatus,
    /// The original query/prompt.
    pub query: String,
    /// Final result (when completed).
    pub result: Option<String>,
    /// Error message (when failed).
    pub error: Option<String>,
    /// Tokens used.
    pub tokens_used: u32,
    /// Cost in USD.
    pub cost_usd: f64,
    /// Model used.
    pub model: Option<String>,
    /// When the run was created.
    pub created_at: DateTime<Utc>,
    /// When the run was last updated.
    pub updated_at: DateTime<Utc>,
    /// When the run completed.
    pub completed_at: Option<DateTime<Utc>>,
}

impl Run {
    /// Create a new run.
    pub fn new(query: impl Into<String>) -> Self {
        let now = Utc::now();
        Self {
            id: Uuid::new_v4().to_string(),
            session_id: None,
            user_id: None,
            status: RunStatus::Pending,
            query: query.into(),
            result: None,
            error: None,
            tokens_used: 0,
            cost_usd: 0.0,
            model: None,
            created_at: now,
            updated_at: now,
            completed_at: None,
        }
    }

    /// Set the session ID.
    pub fn with_session(mut self, session_id: impl Into<String>) -> Self {
        self.session_id = Some(session_id.into());
        self
    }

    /// Set the user ID.
    pub fn with_user(mut self, user_id: impl Into<String>) -> Self {
        self.user_id = Some(user_id.into());
        self
    }

    /// Start the run.
    pub fn start(&mut self) {
        self.status = RunStatus::Running;
        self.updated_at = Utc::now();
    }

    /// Complete the run with a result.
    pub fn complete(&mut self, result: impl Into<String>) {
        self.status = RunStatus::Completed;
        self.result = Some(result.into());
        self.completed_at = Some(Utc::now());
        self.updated_at = Utc::now();
    }

    /// Fail the run with an error.
    pub fn fail(&mut self, error: impl Into<String>) {
        self.status = RunStatus::Failed;
        self.error = Some(error.into());
        self.completed_at = Some(Utc::now());
        self.updated_at = Utc::now();
    }

    /// Cancel the run.
    pub fn cancel(&mut self) {
        self.status = RunStatus::Cancelled;
        self.completed_at = Some(Utc::now());
        self.updated_at = Utc::now();
    }

    /// Update token usage.
    pub fn add_tokens(&mut self, tokens: u32, cost: f64) {
        self.tokens_used += tokens;
        self.cost_usd += cost;
        self.updated_at = Utc::now();
    }
}

/// Status of a run.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum RunStatus {
    /// Run is pending.
    Pending,
    /// Run is currently executing.
    Running,
    /// Run completed successfully.
    Completed,
    /// Run failed with an error.
    Failed,
    /// Run was cancelled.
    Cancelled,
}

impl std::fmt::Display for RunStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Pending => write!(f, "pending"),
            Self::Running => write!(f, "running"),
            Self::Completed => write!(f, "completed"),
            Self::Failed => write!(f, "failed"),
            Self::Cancelled => write!(f, "cancelled"),
        }
    }
}
