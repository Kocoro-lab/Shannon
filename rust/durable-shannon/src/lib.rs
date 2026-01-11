//! Shannon-specific extensions to the Durable workflow engine.
//!
//! This crate provides:
//! - SurrealDB backend for event log persistence
//! - Embedded worker mode for Tauri desktop/mobile
//! - Custom activity types for LLM calls and tool execution
//!
//! # Architecture
//!
//! Durable is a WASM-based workflow engine that provides:
//! - Event sourcing for durable execution
//! - Automatic replay on failure
//! - Checkpoint-based state management
//!
//! Shannon extends Durable with:
//! - SurrealDB storage backend (instead of PostgreSQL)
//! - Embedded worker that runs in-process
//! - LLM activity types with retry and fallback logic
//!
//! # Usage
//!
//! ```rust,ignore
//! use durable_shannon::EmbeddedWorker;
//!
//! // Initialize the event log
//! // let event_log = MyEventLog::new("./data/workflows.db").await?;
//!
//! // Create an embedded worker
//! let worker = EmbeddedWorker::new(event_log, "./wasm/patterns").await?;
//!
//! // Submit a workflow
//! let handle = worker.submit("chain_of_thought", input).await?;
//!
//! // Wait for result
//! let result = handle.result().await?;
//! ```

pub mod activities;
pub mod backends;
pub mod microsandbox;
pub mod worker;

// Re-exports
pub use backends::EventLog;
pub use worker::EmbeddedWorker;

/// Prelude for convenient imports.
pub mod prelude {
    pub use crate::activities::{Activity, ActivityContext, ActivityResult};
    pub use crate::backends::EventLog;
    pub use crate::worker::{EmbeddedWorker, WorkflowHandle};
}

/// Workflow event types.
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub enum Event {
    /// Workflow started.
    WorkflowStarted {
        workflow_id: String,
        workflow_type: String,
        input: serde_json::Value,
        timestamp: chrono::DateTime<chrono::Utc>,
    },
    /// Activity scheduled.
    ActivityScheduled {
        activity_id: String,
        activity_type: String,
        input: serde_json::Value,
    },
    /// Activity completed.
    ActivityCompleted {
        activity_id: String,
        output: serde_json::Value,
        duration_ms: u64,
    },
    /// Activity failed.
    ActivityFailed {
        activity_id: String,
        error: String,
        retryable: bool,
    },
    /// Checkpoint created.
    Checkpoint { state: Vec<u8> },
    /// Workflow completed.
    WorkflowCompleted {
        output: serde_json::Value,
        timestamp: chrono::DateTime<chrono::Utc>,
    },
    /// Workflow failed.
    WorkflowFailed {
        error: String,
        timestamp: chrono::DateTime<chrono::Utc>,
    },
}

impl Event {
    /// Get the event type as a string.
    #[must_use]
    pub fn event_type(&self) -> &'static str {
        match self {
            Self::WorkflowStarted { .. } => "workflow_started",
            Self::ActivityScheduled { .. } => "activity_scheduled",
            Self::ActivityCompleted { .. } => "activity_completed",
            Self::ActivityFailed { .. } => "activity_failed",
            Self::Checkpoint { .. } => "checkpoint",
            Self::WorkflowCompleted { .. } => "workflow_completed",
            Self::WorkflowFailed { .. } => "workflow_failed",
        }
    }

    /// Serialize the event to bytes.
    pub fn serialize(&self) -> anyhow::Result<Vec<u8>> {
        Ok(bincode::serialize(self)?)
    }

    /// Deserialize an event from bytes.
    pub fn deserialize(data: &[u8]) -> anyhow::Result<Self> {
        Ok(bincode::deserialize(data)?)
    }
}
