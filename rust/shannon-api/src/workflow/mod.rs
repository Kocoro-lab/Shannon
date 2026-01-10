//! Workflow engine abstraction layer.
//!
//! This module provides a unified interface for workflow execution that
//! abstracts over different backend engines:
//! - **Durable**: Rust-native WASM workflow engine for embedded mode
//! - **Temporal**: Production-grade workflow engine for cloud mode
//!
//! The abstraction allows Shannon to run in both embedded (Tauri) and cloud
//! (Docker/K8s) environments with the same application logic.

pub mod engine;
pub mod task;

pub use engine::{WorkflowEngine, WorkflowEngineType};
pub use task::{Task, TaskHandle, TaskResult, TaskState};

use crate::config::deployment::WorkflowConfig;

/// Create a workflow engine based on configuration.
///
/// # Errors
///
/// Returns an error if the engine cannot be initialized.
pub async fn create_engine(
    config: &WorkflowConfig,
    #[cfg(feature = "embedded")]
    surreal_conn: Option<surrealdb::Surreal<surrealdb::engine::local::Db>>,
) -> anyhow::Result<WorkflowEngine> {
    WorkflowEngine::from_config(config, #[cfg(feature = "embedded")] surreal_conn).await
}
