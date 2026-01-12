//! Embedded workflow engine orchestration.
//!
//! Coordinates workflow execution by integrating:
//! - Event Log (durable state)
//! - Workflow Store (metadata)
//! - Event Bus (real-time streaming)
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::embedded::EmbeddedWorkflowEngine;
//!
//! let engine = EmbeddedWorkflowEngine::new("./shannon.db").await?;
//!
//! // Submit workflow
//! let workflow_id = engine.submit_task("user-1", "session-1", input).await?;
//!
//! // Stream events
//! let events = engine.stream_events(&workflow_id);
//!
//! // Control operations
//! engine.pause_workflow(&workflow_id).await?;
//! engine.resume_workflow(&workflow_id).await?;
//! engine.cancel_workflow(&workflow_id).await?;
//! ```

use std::path::{Path, PathBuf};
use std::sync::Arc;

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use tokio::sync::broadcast;
use uuid::Uuid;

use crate::database::{WorkflowMetadata, WorkflowStatus, WorkflowStore};
use durable_shannon::SqliteEventLog;

use super::event_bus::{EventBus, WorkflowEvent};
use super::replay::ReplayManager;

/// Maximum concurrent workflows.
///
/// Limits resource usage on desktop/mobile devices.
const MAX_CONCURRENT_WORKFLOWS: usize = 10;

/// Embedded workflow engine for Tauri desktop application.
///
/// Provides workflow orchestration without external dependencies:
/// - No `Temporal` (uses `Durable` + `SQLite`)
/// - No orchestrator service (in-process)
/// - No `PostgreSQL` (uses `SQLite`)
///
/// # Architecture
///
/// ```text
/// EmbeddedWorkflowEngine
/// ├─> EventLog (SqliteEventLog)      ← Event sourcing
/// ├─> WorkflowStore (SQLite)         ← Metadata & checkpoints
/// └─> EventBus (tokio channels)      ← Real-time streaming
/// ```
#[derive(Debug, Clone)]
pub struct EmbeddedWorkflowEngine {
    /// Event log for durable state (will be used in pattern execution).
    #[expect(dead_code, reason = "Will be used in P1.5+ for pattern execution")]
    event_log: Arc<SqliteEventLog>,

    /// Workflow metadata store.
    workflow_store: Arc<WorkflowStore>,

    /// Event bus for real-time streaming.
    event_bus: Arc<EventBus>,

    /// Database path for convenience.
    db_path: PathBuf,
}

impl EmbeddedWorkflowEngine {
    /// Create a new embedded workflow engine.
    ///
    /// # Arguments
    ///
    /// * `db_path` - Path to `SQLite` database file
    ///
    /// # Errors
    ///
    /// Returns error if database cannot be initialized.
    ///
    /// # Example
    ///
    /// ```rust,ignore
    /// let engine = EmbeddedWorkflowEngine::new("./shannon.db").await?;
    /// ```
    pub async fn new<P: AsRef<Path>>(db_path: P) -> Result<Self> {
        let db_path = db_path.as_ref().to_path_buf();

        // Initialize components
        let event_log = SqliteEventLog::new(&db_path)
            .await
            .context("Failed to initialize event log")?;

        let workflow_store = WorkflowStore::new(&db_path)
            .await
            .context("Failed to initialize workflow store")?;

        let event_bus = EventBus::new();

        Ok(Self {
            event_log: Arc::new(event_log),
            workflow_store: Arc::new(workflow_store),
            event_bus: Arc::new(event_bus),
            db_path,
        })
    }

    /// Submit a new workflow for execution.
    ///
    /// # Arguments
    ///
    /// * `user_id` - User identifier
    /// * `session_id` - Optional session identifier for conversation context
    /// * `pattern_type` - Cognitive pattern to use (e.g., `chain_of_thought`)
    /// * `input` - Task description or query
    ///
    /// # Errors
    ///
    /// Returns error if workflow cannot be created or too many concurrent workflows.
    ///
    /// # Example
    ///
    /// ```rust,ignore
    /// let workflow_id = engine.submit_task(
    ///     "user-123",
    ///     Some("session-456"),
    ///     "chain_of_thought",
    ///     "What is the capital of France?"
    /// ).await?;
    /// ```
    pub async fn submit_task(
        &self,
        user_id: &str,
        session_id: Option<&str>,
        pattern_type: &str,
        input: &str,
    ) -> Result<String> {
        // Check concurrent workflow limit
        let running = self
            .workflow_store
            .list_by_status(WorkflowStatus::Running)
            .await?;
        if running.len() >= MAX_CONCURRENT_WORKFLOWS {
            anyhow::bail!("Too many concurrent workflows (max: {MAX_CONCURRENT_WORKFLOWS})");
        }

        // Generate workflow ID
        let workflow_id = Uuid::new_v4().to_string();

        // Create workflow in store
        self.workflow_store
            .create_workflow(&workflow_id, user_id, session_id, pattern_type, input)
            .await
            .context("Failed to create workflow")?;

        // Broadcast workflow started event
        self.event_bus.broadcast(
            &workflow_id,
            WorkflowEvent::WorkflowStarted {
                workflow_id: workflow_id.clone(),
                pattern_type: pattern_type.to_string(),
                timestamp: chrono::Utc::now(),
            },
        )?;

        // Update status to running
        self.workflow_store
            .update_status(&workflow_id, WorkflowStatus::Running)
            .await
            .context("Failed to update workflow status")?;

        // Broadcast status change
        self.event_bus.broadcast(
            &workflow_id,
            WorkflowEvent::WorkflowStatusChanged {
                workflow_id: workflow_id.clone(),
                old_status: "pending".to_string(),
                new_status: "running".to_string(),
                timestamp: chrono::Utc::now(),
            },
        )?;

        Ok(workflow_id)
    }

    /// Stream events for a workflow.
    ///
    /// Returns a broadcast receiver that will receive all future events
    /// for the specified workflow.
    ///
    /// # Example
    ///
    /// ```rust,ignore
    /// let mut events = engine.stream_events("workflow-123");
    /// while let Ok(event) = events.recv().await {
    ///     println!("Event: {:?}", event);
    /// }
    /// ```
    #[must_use]
    pub fn stream_events(&self, workflow_id: &str) -> broadcast::Receiver<WorkflowEvent> {
        self.event_bus.subscribe(workflow_id)
    }

    /// Pause a running workflow.
    ///
    /// # Errors
    ///
    /// Returns error if workflow not found or not in running state.
    pub async fn pause_workflow(&self, workflow_id: &str) -> Result<()> {
        // Broadcast pausing event
        self.event_bus.broadcast(
            workflow_id,
            WorkflowEvent::WorkflowPausing {
                workflow_id: workflow_id.to_string(),
                timestamp: chrono::Utc::now(),
            },
        )?;

        // Update status
        self.workflow_store
            .update_status(workflow_id, WorkflowStatus::Paused)
            .await
            .context("Failed to update workflow status")?;

        // Broadcast paused event
        self.event_bus.broadcast(
            workflow_id,
            WorkflowEvent::WorkflowPaused {
                workflow_id: workflow_id.to_string(),
                timestamp: chrono::Utc::now(),
            },
        )?;

        Ok(())
    }

    /// Resume a paused workflow.
    ///
    /// # Errors
    ///
    /// Returns error if workflow not found or not in paused state.
    pub async fn resume_workflow(&self, workflow_id: &str) -> Result<()> {
        // Broadcast resuming event
        self.event_bus.broadcast(
            workflow_id,
            WorkflowEvent::WorkflowResuming {
                workflow_id: workflow_id.to_string(),
                timestamp: chrono::Utc::now(),
            },
        )?;

        // Update status
        self.workflow_store
            .update_status(workflow_id, WorkflowStatus::Running)
            .await
            .context("Failed to update workflow status")?;

        Ok(())
    }

    /// Cancel a workflow.
    ///
    /// # Errors
    ///
    /// Returns error if workflow not found.
    pub async fn cancel_workflow(&self, workflow_id: &str) -> Result<()> {
        // Broadcast cancelling event
        self.event_bus.broadcast(
            workflow_id,
            WorkflowEvent::WorkflowCancelling {
                workflow_id: workflow_id.to_string(),
                timestamp: chrono::Utc::now(),
            },
        )?;

        // Update status
        self.workflow_store
            .update_status(workflow_id, WorkflowStatus::Cancelled)
            .await
            .context("Failed to update workflow status")?;

        // Broadcast cancelled event
        self.event_bus.broadcast(
            workflow_id,
            WorkflowEvent::WorkflowCancelled {
                workflow_id: workflow_id.to_string(),
                timestamp: chrono::Utc::now(),
            },
        )?;

        // Cleanup event bus
        self.event_bus.cleanup(workflow_id);

        Ok(())
    }

    /// Get workflow metadata.
    ///
    /// # Errors
    ///
    /// Returns error if database query fails.
    pub async fn get_workflow(&self, workflow_id: &str) -> Result<Option<WorkflowMetadata>> {
        self.workflow_store.get_workflow(workflow_id).await
    }

    /// List workflows with optional filtering.
    ///
    /// # Errors
    ///
    /// Returns error if database query fails.
    pub async fn list_workflows(
        &self,
        session_id: Option<String>,
        limit: usize,
    ) -> Result<Vec<WorkflowMetadata>> {
        self.workflow_store.list_workflows(session_id, limit).await
    }

    /// Export workflow to JSON.
    ///
    /// # Errors
    ///
    /// Returns error if export fails.
    pub async fn export_workflow(&self, workflow_id: &str) -> Result<String> {
        let replay = ReplayManager::new(self.workflow_store.clone(), &self.db_path).await?;
        replay.export_workflow_json(workflow_id).await
    }

    /// Get engine health status.
    ///
    /// Returns information about active workflows and system resources.
    #[must_use]
    pub fn health(&self) -> EngineHealth {
        EngineHealth {
            active_channels: self.event_bus.active_channels(),
            max_concurrent_workflows: MAX_CONCURRENT_WORKFLOWS,
            db_path: self.db_path.clone(),
        }
    }
}

/// Engine health information.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EngineHealth {
    /// Number of active event channels.
    pub active_channels: usize,
    /// Maximum concurrent workflows allowed.
    pub max_concurrent_workflows: usize,
    /// Database path.
    pub db_path: PathBuf,
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::NamedTempFile;

    async fn create_test_engine() -> (EmbeddedWorkflowEngine, NamedTempFile) {
        let temp_file = NamedTempFile::new().unwrap();
        let engine = EmbeddedWorkflowEngine::new(temp_file.path()).await.unwrap();
        (engine, temp_file)
    }

    #[tokio::test]
    async fn test_new_engine() {
        let (engine, _temp) = create_test_engine().await;
        let health = engine.health();
        assert_eq!(health.active_channels, 0);
    }

    #[tokio::test]
    async fn test_submit_task() {
        let (engine, _temp) = create_test_engine().await;

        let workflow_id = engine
            .submit_task(
                "user-1",
                Some("session-1"),
                "chain_of_thought",
                "test query",
            )
            .await
            .unwrap();

        assert!(!workflow_id.is_empty());

        // Verify workflow was created
        let workflow = engine.get_workflow(&workflow_id).await.unwrap();
        assert!(workflow.is_some());
        assert_eq!(workflow.unwrap().status, WorkflowStatus::Running);
    }

    #[tokio::test]
    async fn test_submit_task_emits_events() {
        let (engine, _temp) = create_test_engine().await;

        let workflow_id = engine
            .submit_task("user-1", None, "research", "test query")
            .await
            .unwrap();

        // Subscribe after submission (should still get future events)
        let mut rx = engine.stream_events(&workflow_id);

        // Broadcast a progress event
        engine
            .event_bus
            .broadcast(
                &workflow_id,
                WorkflowEvent::Progress {
                    workflow_id: workflow_id.clone(),
                    step: "test step".to_string(),
                    percentage: 25.0,
                    message: None,
                },
            )
            .unwrap();

        // Should receive the event
        let event = rx.recv().await.unwrap();
        assert_eq!(event.workflow_id(), workflow_id.as_str());
    }

    #[tokio::test]
    async fn test_pause_workflow() {
        let (engine, _temp) = create_test_engine().await;

        let workflow_id = engine
            .submit_task("user-1", None, "cot", "test")
            .await
            .unwrap();

        engine.pause_workflow(&workflow_id).await.unwrap();

        let workflow = engine.get_workflow(&workflow_id).await.unwrap().unwrap();
        assert_eq!(workflow.status, WorkflowStatus::Paused);
    }

    #[tokio::test]
    async fn test_resume_workflow() {
        let (engine, _temp) = create_test_engine().await;

        let workflow_id = engine
            .submit_task("user-1", None, "cot", "test")
            .await
            .unwrap();

        engine.pause_workflow(&workflow_id).await.unwrap();
        engine.resume_workflow(&workflow_id).await.unwrap();

        let workflow = engine.get_workflow(&workflow_id).await.unwrap().unwrap();
        assert_eq!(workflow.status, WorkflowStatus::Running);
    }

    #[tokio::test]
    async fn test_cancel_workflow() {
        let (engine, _temp) = create_test_engine().await;

        let workflow_id = engine
            .submit_task("user-1", None, "cot", "test")
            .await
            .unwrap();

        engine.cancel_workflow(&workflow_id).await.unwrap();

        let workflow = engine.get_workflow(&workflow_id).await.unwrap().unwrap();
        assert_eq!(workflow.status, WorkflowStatus::Cancelled);

        // Event bus should be cleaned up
        assert_eq!(engine.health().active_channels, 0);
    }

    #[tokio::test]
    async fn test_max_concurrent_workflows() {
        let (engine, _temp) = create_test_engine().await;

        // Submit max workflows
        for i in 0..MAX_CONCURRENT_WORKFLOWS {
            engine
                .submit_task("user-1", None, "cot", &format!("query{i}"))
                .await
                .unwrap();
        }

        // Next submission should fail
        let result = engine.submit_task("user-1", None, "cot", "overflow").await;
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("Too many"));
    }

    #[tokio::test]
    async fn test_stream_events_multiple_subscribers() {
        let (engine, _temp) = create_test_engine().await;

        let workflow_id = engine
            .submit_task("user-1", None, "cot", "test")
            .await
            .unwrap();

        let mut rx1 = engine.stream_events(&workflow_id);
        let mut rx2 = engine.stream_events(&workflow_id);

        engine
            .event_bus
            .broadcast(
                &workflow_id,
                WorkflowEvent::Progress {
                    workflow_id: workflow_id.clone(),
                    step: "test".to_string(),
                    percentage: 50.0,
                    message: None,
                },
            )
            .unwrap();

        // Both subscribers receive the event
        assert!(rx1.recv().await.is_ok());
        assert!(rx2.recv().await.is_ok());
    }

    #[tokio::test]
    async fn test_health_check() {
        let (engine, _temp) = create_test_engine().await;

        let health = engine.health();
        assert_eq!(health.max_concurrent_workflows, MAX_CONCURRENT_WORKFLOWS);
        assert_eq!(health.active_channels, 0);

        // Submit workflow
        let _workflow_id = engine
            .submit_task("user-1", None, "cot", "test")
            .await
            .unwrap();

        // Should have active channel now
        let health = engine.health();
        assert_eq!(health.active_channels, 1);
    }

    #[tokio::test]
    async fn test_concurrent_workflow_submission() {
        let (engine, _temp) = create_test_engine().await;

        let mut handles = vec![];
        for i in 0..5 {
            let engine = engine.clone();
            let handle = tokio::spawn(async move {
                engine
                    .submit_task("user-1", None, "cot", &format!("query{i}"))
                    .await
                    .unwrap()
            });
            handles.push(handle);
        }

        for handle in handles {
            let workflow_id = handle.await.unwrap();
            assert!(!workflow_id.is_empty());
        }

        // All 5 workflows should be running
        let running = engine
            .workflow_store
            .list_by_status(WorkflowStatus::Running)
            .await
            .unwrap();
        assert_eq!(running.len(), 5);
    }
}
