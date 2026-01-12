//! Workflow engine abstraction layer.
//!
//! This module provides a unified interface for workflow execution that
//! abstracts over different backend engines:
//! - **Durable**: Rust-native WASM workflow engine for embedded mode
//! - **Temporal**: Production-grade workflow engine for cloud mode
//!
//! The abstraction allows Shannon to run in both embedded (Tauri) and cloud
//! (Docker/K8s) environments with the same application logic.

pub mod advanced;
pub mod control;
pub mod embedded;
pub mod engine;
pub mod multiagent;
pub mod patterns;
pub mod task;
pub mod tracking;

pub use embedded::{EmbeddedWorkflowEngine, EventBus, WorkflowEvent};
pub use engine::{WorkflowEngine, WorkflowEngineType};
pub use patterns::{CognitivePattern, PatternContext, PatternRegistry, PatternResult};
pub use task::{Task, TaskHandle, TaskResult, TaskState};
pub use tracking::{ModelUsageBreakdown, TaskResultWithMetadata, UsageTracker};

use crate::config::deployment::WorkflowConfig;

/// Create a workflow engine based on configuration.
///
/// # Errors
///
/// Returns an error if the engine cannot be initialized.
pub async fn create_engine(
    config: &WorkflowConfig,
    #[cfg(feature = "embedded")]
    #[cfg(feature = "embedded")]
    event_log: Option<Box<dyn durable_shannon::EventLog>>,
) -> anyhow::Result<WorkflowEngine> {
    WorkflowEngine::from_config(
        config,
        #[cfg(feature = "embedded")]
        event_log,
    )
    .await
}
