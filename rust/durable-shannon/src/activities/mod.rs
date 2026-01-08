//! Activity types for workflow execution.
//!
//! Activities are the building blocks of workflows - they perform actual work
//! like calling LLMs, executing tools, or fetching data.

pub mod llm;
pub mod tools;

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

/// Activity execution context.
#[derive(Debug, Clone)]
pub struct ActivityContext {
    /// Workflow ID.
    pub workflow_id: String,
    /// Activity ID.
    pub activity_id: String,
    /// Attempt number (for retries).
    pub attempt: u32,
    /// Maximum attempts.
    pub max_attempts: u32,
    /// Timeout in seconds.
    pub timeout_secs: u64,
}

impl Default for ActivityContext {
    fn default() -> Self {
        Self {
            workflow_id: String::new(),
            activity_id: uuid::Uuid::new_v4().to_string(),
            attempt: 1,
            max_attempts: 3,
            timeout_secs: 60,
        }
    }
}

/// Result of activity execution.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum ActivityResult {
    /// Activity completed successfully.
    Success(serde_json::Value),
    /// Activity failed with an error.
    Failure {
        error: String,
        retryable: bool,
    },
    /// Activity needs to be retried.
    Retry {
        reason: String,
        backoff_secs: u64,
    },
}

impl ActivityResult {
    /// Create a success result.
    #[must_use]
    pub fn success(value: impl Serialize) -> Self {
        Self::Success(serde_json::to_value(value).unwrap_or(serde_json::Value::Null))
    }

    /// Create a failure result.
    #[must_use]
    pub fn failure(error: impl Into<String>, retryable: bool) -> Self {
        Self::Failure {
            error: error.into(),
            retryable,
        }
    }

    /// Create a retry result.
    #[must_use]
    pub fn retry(reason: impl Into<String>, backoff_secs: u64) -> Self {
        Self::Retry {
            reason: reason.into(),
            backoff_secs,
        }
    }

    /// Check if the result is a success.
    #[must_use]
    pub fn is_success(&self) -> bool {
        matches!(self, Self::Success(_))
    }
}

/// Activity trait for implementing custom activities.
#[async_trait]
pub trait Activity: Send + Sync {
    /// Activity name for identification.
    fn name(&self) -> &'static str;

    /// Execute the activity.
    async fn execute(
        &self,
        ctx: &ActivityContext,
        input: serde_json::Value,
    ) -> ActivityResult;

    /// Check if the activity can be retried for the given error.
    fn can_retry(&self, _error: &str) -> bool {
        true
    }

    /// Get the retry backoff for the given attempt.
    fn retry_backoff(&self, attempt: u32) -> std::time::Duration {
        // Exponential backoff: 1s, 2s, 4s, 8s, ...
        std::time::Duration::from_secs(2u64.pow(attempt.saturating_sub(1)))
    }
}

/// Registry of available activities.
#[derive(Default)]
pub struct ActivityRegistry {
    activities: std::collections::HashMap<String, Box<dyn Activity>>,
}

impl ActivityRegistry {
    /// Create a new activity registry.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Register an activity.
    pub fn register<A: Activity + 'static>(&mut self, activity: A) {
        self.activities
            .insert(activity.name().to_string(), Box::new(activity));
    }

    /// Get an activity by name.
    #[must_use]
    pub fn get(&self, name: &str) -> Option<&dyn Activity> {
        self.activities.get(name).map(AsRef::as_ref)
    }

    /// Execute an activity by name.
    pub async fn execute(
        &self,
        name: &str,
        ctx: &ActivityContext,
        input: serde_json::Value,
    ) -> ActivityResult {
        match self.get(name) {
            Some(activity) => activity.execute(ctx, input).await,
            None => ActivityResult::failure(format!("Activity not found: {name}"), false),
        }
    }
}

impl std::fmt::Debug for ActivityRegistry {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ActivityRegistry")
            .field("activities", &self.activities.keys().collect::<Vec<_>>())
            .finish()
    }
}
