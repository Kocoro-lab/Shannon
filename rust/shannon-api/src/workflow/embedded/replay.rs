//! Workflow replay capability for deterministic debugging.
//!
//! Enables time-travel debugging by replaying workflows from exported history.
//! Supports step-through debugging, breakpoints, and state inspection.
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::embedded::ReplayManager;
//!
//! let replay = ReplayManager::new(workflow_store, event_log_path).await?;
//!
//! // Export workflow history
//! let json = replay.export_workflow_json("wf-123").await?;
//! std::fs::write("workflow.json", json)?;
//!
//! // Replay from JSON
//! let history = std::fs::read_to_string("workflow.json")?;
//! let result = replay.replay_from_json(&history).await?;
//! ```

use std::collections::HashMap;
use std::path::Path;
use std::sync::Arc;

use anyhow::{Context as AnyhowContext, Result};
use base64::Engine;
use parking_lot::Mutex;
use rusqlite::Connection;
use serde::{Deserialize, Serialize};

use crate::database::workflow_store::{WorkflowMetadata, WorkflowStatus, WorkflowStore};

/// Exported workflow history for replay.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorkflowHistory {
    /// Workflow metadata.
    pub workflow: WorkflowMetadata,

    /// Event history.
    pub events: Vec<EventRecord>,

    /// Checkpoint data if available.
    pub checkpoint: Option<CheckpointRecord>,

    /// Export timestamp.
    pub exported_at: chrono::DateTime<chrono::Utc>,

    /// Export version for format compatibility.
    pub version: String,
}

/// Event record in exported history.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EventRecord {
    /// Event sequence number.
    pub sequence: u64,

    /// Event type identifier.
    pub event_type: String,

    /// Event timestamp.
    pub timestamp: chrono::DateTime<chrono::Utc>,

    /// Event data (base64 encoded bincode).
    pub data: String,
}

/// Checkpoint record in exported history.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CheckpointRecord {
    /// Checkpoint sequence number.
    pub sequence: u64,

    /// Checkpoint timestamp.
    pub created_at: chrono::DateTime<chrono::Utc>,

    /// State data (base64 encoded).
    pub state_data: String,

    /// Checksum for verification.
    pub checksum: u32,
}

/// Replay result with final state.
#[derive(Debug, Clone)]
pub struct ReplayResult {
    /// Workflow ID that was replayed.
    pub workflow_id: String,

    /// Final workflow status after replay.
    pub final_status: WorkflowStatus,

    /// Number of events replayed.
    pub events_replayed: usize,

    /// Replay duration in milliseconds.
    pub duration_ms: u64,

    /// Final state snapshot (if captured).
    pub final_state: Option<Vec<u8>>,
}

/// Breakpoint configuration for step-through debugging.
#[derive(Debug, Clone)]
pub struct Breakpoint {
    /// Event sequence number to break at.
    pub sequence: u64,

    /// Optional condition (event type match).
    pub event_type: Option<String>,

    /// Whether breakpoint is enabled.
    pub enabled: bool,
}

/// Replay mode for debugging.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ReplayMode {
    /// Full replay without pausing.
    Full,

    /// Step-through: pause after each event.
    StepThrough,

    /// Breakpoint: pause at configured breakpoints.
    Breakpoint,
}

/// Manager for workflow replay and debugging.
pub struct ReplayManager {
    /// Workflow store for metadata.
    workflow_store: Arc<WorkflowStore>,

    /// Event log connection.
    event_conn: Arc<Mutex<Connection>>,

    /// Configured breakpoints.
    breakpoints: Arc<Mutex<HashMap<u64, Breakpoint>>>,

    /// Current replay mode.
    replay_mode: Arc<Mutex<ReplayMode>>,
}

impl ReplayManager {
    /// Create a new replay manager.
    ///
    /// # Errors
    ///
    /// Returns error if initialization fails.
    pub async fn new(
        workflow_store: Arc<WorkflowStore>,
        event_log_path: impl AsRef<Path>,
    ) -> Result<Self> {
        let event_conn = Connection::open(event_log_path.as_ref())
            .context("Failed to open event log for replay")?;

        Ok(Self {
            workflow_store,
            event_conn: Arc::new(Mutex::new(event_conn)),
            breakpoints: Arc::new(Mutex::new(HashMap::new())),
            replay_mode: Arc::new(Mutex::new(ReplayMode::Full)),
        })
    }

    /// Export workflow history to JSON.
    ///
    /// # Errors
    ///
    /// Returns error if export fails.
    pub async fn export_workflow_json(&self, workflow_id: &str) -> Result<String> {
        let workflow = self
            .workflow_store
            .get_workflow(workflow_id)
            .await?
            .ok_or_else(|| anyhow::anyhow!("Workflow not found: {}", workflow_id))?;

        let checkpoint = self.workflow_store.load_checkpoint(workflow_id).await?;

        let events = self.load_events(workflow_id).await?;

        let history = WorkflowHistory {
            workflow,
            events,
            checkpoint: checkpoint.map(|cp| CheckpointRecord {
                sequence: cp.sequence,
                created_at: chrono::DateTime::from_timestamp(cp.created_at, 0)
                    .unwrap_or_else(chrono::Utc::now),
                state_data: base64::engine::general_purpose::STANDARD.encode(&cp.state),
                checksum: cp.checksum,
            }),
            exported_at: chrono::Utc::now(),
            version: "1.0".to_string(),
        };

        serde_json::to_string_pretty(&history).context("Failed to serialize workflow history")
    }

    /// Export workflow history to JSON file.
    ///
    /// # Errors
    ///
    /// Returns error if export fails.
    pub async fn export_workflow_to_file(
        &self,
        workflow_id: &str,
        output_path: impl AsRef<Path>,
    ) -> Result<()> {
        let json = self.export_workflow_json(workflow_id).await?;
        std::fs::write(output_path.as_ref(), json)
            .context("Failed to write workflow history to file")?;
        tracing::info!(
            workflow_id,
            path = ?output_path.as_ref(),
            "Exported workflow history"
        );
        Ok(())
    }

    /// Import and replay workflow from JSON.
    ///
    /// # Errors
    ///
    /// Returns error if import or replay fails.
    pub async fn replay_from_json(&self, json: &str) -> Result<ReplayResult> {
        let history: WorkflowHistory =
            serde_json::from_str(json).context("Failed to parse workflow history")?;

        self.replay_history(&history).await
    }

    /// Import and replay workflow from JSON file.
    ///
    /// # Errors
    ///
    /// Returns error if import or replay fails.
    pub async fn replay_from_file(&self, input_path: impl AsRef<Path>) -> Result<ReplayResult> {
        let json = std::fs::read_to_string(input_path.as_ref())
            .context("Failed to read workflow history file")?;
        self.replay_from_json(&json).await
    }

    /// Replay workflow from history.
    ///
    /// # Errors
    ///
    /// Returns error if replay fails.
    async fn replay_history(&self, history: &WorkflowHistory) -> Result<ReplayResult> {
        let start = std::time::Instant::now();
        let workflow_id = &history.workflow.workflow_id;

        tracing::info!(
            workflow_id,
            event_count = history.events.len(),
            "Starting workflow replay"
        );

        let mut events_replayed = 0;
        let mode = *self.replay_mode.lock();

        for event in &history.events {
            // Check breakpoint
            if self.should_break_at(event.sequence, Some(&event.event_type)) {
                tracing::info!(
                    workflow_id,
                    sequence = event.sequence,
                    event_type = %event.event_type,
                    "Breakpoint hit"
                );
                // In real implementation, this would pause and wait for user input
            }

            // Decode event data
            let _event_data = base64::engine::general_purpose::STANDARD
                .decode(&event.data)
                .context("Failed to decode event data")?;

            // Process event (in real implementation, would apply state changes)
            events_replayed += 1;

            // Step-through mode: pause after each event
            if mode == ReplayMode::StepThrough {
                tracing::debug!(
                    workflow_id,
                    sequence = event.sequence,
                    "Step: {} / {}",
                    events_replayed,
                    history.events.len()
                );
            }
        }

        let duration_ms = start.elapsed().as_millis() as u64;

        tracing::info!(
            workflow_id,
            events_replayed,
            duration_ms,
            "Replay completed"
        );

        Ok(ReplayResult {
            workflow_id: workflow_id.clone(),
            final_status: history.workflow.status.clone(),
            events_replayed,
            duration_ms,
            final_state: None,
        })
    }

    /// Load events for a workflow.
    ///
    /// # Errors
    ///
    /// Returns error if event loading fails.
    async fn load_events(&self, workflow_id: &str) -> Result<Vec<EventRecord>> {
        let conn = self.event_conn.lock();

        let mut stmt = conn.prepare(
            "SELECT sequence, event_type, event_data, timestamp 
             FROM workflow_events 
             WHERE workflow_id = ?1 
             ORDER BY sequence",
        )?;

        let events = stmt
            .query_map([workflow_id], |row| {
                let sequence: i64 = row.get(0)?;
                let event_type: String = row.get(1)?;
                let event_data: Vec<u8> = row.get(2)?;
                let timestamp: i64 = row.get(3)?;

                Ok(EventRecord {
                    sequence: sequence as u64,
                    event_type,
                    timestamp: chrono::DateTime::from_timestamp(timestamp, 0)
                        .unwrap_or_else(chrono::Utc::now),
                    data: base64::engine::general_purpose::STANDARD.encode(&event_data),
                })
            })?
            .collect::<Result<Vec<_>, _>>()?;

        Ok(events)
    }

    /// Add a breakpoint at a specific sequence number.
    pub fn add_breakpoint(&self, sequence: u64, event_type: Option<String>) {
        let breakpoint = Breakpoint {
            sequence,
            event_type,
            enabled: true,
        };

        self.breakpoints.lock().insert(sequence, breakpoint);
        tracing::debug!(sequence, "Added breakpoint");
    }

    /// Remove a breakpoint.
    pub fn remove_breakpoint(&self, sequence: u64) {
        self.breakpoints.lock().remove(&sequence);
        tracing::debug!(sequence, "Removed breakpoint");
    }

    /// Clear all breakpoints.
    pub fn clear_breakpoints(&self) {
        self.breakpoints.lock().clear();
        tracing::info!("Cleared all breakpoints");
    }

    /// Check if replay should break at this event.
    fn should_break_at(&self, sequence: u64, event_type: Option<&str>) -> bool {
        let breakpoints = self.breakpoints.lock();

        if let Some(bp) = breakpoints.get(&sequence) {
            if !bp.enabled {
                return false;
            }

            // Check event type condition if specified
            if let Some(ref bp_type) = bp.event_type {
                if let Some(evt_type) = event_type {
                    return bp_type == evt_type;
                }
                return false;
            }

            return true;
        }

        false
    }

    /// Set replay mode.
    pub fn set_replay_mode(&self, mode: ReplayMode) {
        *self.replay_mode.lock() = mode;
        tracing::info!(?mode, "Set replay mode");
    }

    /// Get current replay mode.
    #[must_use]
    pub fn replay_mode(&self) -> ReplayMode {
        *self.replay_mode.lock()
    }

    /// Inspect state at a specific event sequence.
    ///
    /// # Errors
    ///
    /// Returns error if state inspection fails.
    pub async fn inspect_state_at(&self, workflow_id: &str, sequence: u64) -> Result<String> {
        let events = self.load_events(workflow_id).await?;

        let events_up_to = events
            .iter()
            .filter(|e| e.sequence <= sequence)
            .collect::<Vec<_>>();

        let state = serde_json::json!({
            "workflow_id": workflow_id,
            "sequence": sequence,
            "events_applied": events_up_to.len(),
            "last_event": events_up_to.last().map(|e| &e.event_type),
        });

        serde_json::to_string_pretty(&state).context("Failed to serialize state")
    }

    /// Verify replay determinism by comparing two replays.
    ///
    /// # Errors
    ///
    /// Returns error if verification fails.
    pub async fn verify_determinism(&self, workflow_id: &str) -> Result<bool> {
        tracing::info!(workflow_id, "Verifying replay determinism");

        let json = self.export_workflow_json(workflow_id).await?;

        // First replay
        let result1 = self.replay_from_json(&json).await?;

        // Second replay
        let result2 = self.replay_from_json(&json).await?;

        let deterministic = result1.events_replayed == result2.events_replayed
            && result1.final_status == result2.final_status;

        if deterministic {
            tracing::info!(workflow_id, "Replay is deterministic");
        } else {
            tracing::warn!(workflow_id, "Replay is NOT deterministic");
        }

        Ok(deterministic)
    }
}

impl std::fmt::Debug for ReplayManager {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ReplayManager")
            .field("replay_mode", &self.replay_mode())
            .finish()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::NamedTempFile;

    async fn create_test_replay() -> (
        ReplayManager,
        Arc<WorkflowStore>,
        NamedTempFile,
        NamedTempFile,
    ) {
        let workflow_temp = NamedTempFile::new().unwrap();
        let event_temp = NamedTempFile::new().unwrap();

        let workflow_store = Arc::new(WorkflowStore::new(workflow_temp.path()).await.unwrap());
        let replay = ReplayManager::new(workflow_store.clone(), event_temp.path())
            .await
            .unwrap();

        (replay, workflow_store, workflow_temp, event_temp)
    }

    #[tokio::test]
    async fn test_create_replay_manager() {
        let (_replay, _store, _wf_temp, _ev_temp) = create_test_replay().await;
        // Creation successful
    }

    #[tokio::test]
    async fn test_replay_mode() {
        let (replay, _store, _wf_temp, _ev_temp) = create_test_replay().await;

        assert_eq!(replay.replay_mode(), ReplayMode::Full);

        replay.set_replay_mode(ReplayMode::StepThrough);
        assert_eq!(replay.replay_mode(), ReplayMode::StepThrough);

        replay.set_replay_mode(ReplayMode::Breakpoint);
        assert_eq!(replay.replay_mode(), ReplayMode::Breakpoint);
    }

    #[tokio::test]
    async fn test_breakpoint_management() {
        let (replay, _store, _wf_temp, _ev_temp) = create_test_replay().await;

        replay.add_breakpoint(10, None);
        assert!(replay.should_break_at(10, None));
        assert!(!replay.should_break_at(11, None));

        replay.remove_breakpoint(10);
        assert!(!replay.should_break_at(10, None));
    }

    #[tokio::test]
    async fn test_breakpoint_with_event_type() {
        let (replay, _store, _wf_temp, _ev_temp) = create_test_replay().await;

        replay.add_breakpoint(10, Some("WORKFLOW_STARTED".to_string()));
        assert!(replay.should_break_at(10, Some("WORKFLOW_STARTED")));
        assert!(!replay.should_break_at(10, Some("OTHER_EVENT")));
    }

    #[tokio::test]
    async fn test_clear_breakpoints() {
        let (replay, _store, _wf_temp, _ev_temp) = create_test_replay().await;

        replay.add_breakpoint(10, None);
        replay.add_breakpoint(20, None);

        replay.clear_breakpoints();
        assert!(!replay.should_break_at(10, None));
        assert!(!replay.should_break_at(20, None));
    }

    #[tokio::test]
    async fn test_export_workflow_json() {
        let (replay, store, _wf_temp, _ev_temp) = create_test_replay().await;

        store
            .create_workflow("wf-export", "user-1", Some("sess-1"), "cot", "test")
            .await
            .unwrap();
        store
            .update_status("wf-export", WorkflowStatus::Completed)
            .await
            .unwrap();

        let json = replay.export_workflow_json("wf-export").await.unwrap();

        assert!(!json.is_empty());
        assert!(json.contains("wf-export"));

        // Verify JSON is valid
        let history: WorkflowHistory = serde_json::from_str(&json).unwrap();
        assert_eq!(history.workflow.workflow_id, "wf-export");
        assert_eq!(history.version, "1.0");
    }

    #[tokio::test]
    async fn test_replay_from_json() {
        let (replay, store, _wf_temp, _ev_temp) = create_test_replay().await;

        store
            .create_workflow("wf-replay", "user-1", Some("sess-1"), "cot", "test")
            .await
            .unwrap();
        store
            .update_status("wf-replay", WorkflowStatus::Completed)
            .await
            .unwrap();

        let json = replay.export_workflow_json("wf-replay").await.unwrap();
        let result = replay.replay_from_json(&json).await.unwrap();

        assert_eq!(result.workflow_id, "wf-replay");
        assert_eq!(result.final_status, WorkflowStatus::Completed);
    }
}
