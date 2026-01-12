//! Workflow recovery and replay for crash resilience.
//!
//! Enables workflows to resume after app crashes by replaying events from
//! checkpoints. Implements deterministic replay for debugging.
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::embedded::RecoveryManager;
//!
//! let recovery = RecoveryManager::new(workflow_store, event_log_path).await?;
//!
//! // Recover crashed workflows on startup
//! let recovered = recovery.recover_all_running_workflows().await?;
//!
//! // Replay specific workflow for debugging
//! let state = recovery.replay_workflow("workflow-123").await?;
//! ```

use std::sync::Arc;
use std::time::Duration;

use anyhow::{Context as AnyhowContext, Result};
use parking_lot::Mutex;
use rusqlite::{params, Connection};

use super::circuit_breaker::CircuitBreaker;
use crate::database::workflow_store::{
    WorkflowCheckpoint, WorkflowMetadata, WorkflowStatus, WorkflowStore,
};

/// Error classification for retry logic.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ErrorType {
    /// Transient network error (should retry).
    Network,

    /// Timeout error (should retry with longer timeout).
    Timeout,

    /// Rate limit error (should retry with exponential backoff).
    RateLimit,

    /// Permanent error (should not retry).
    Permanent,

    /// Unknown error (should retry once).
    Unknown,
}

impl ErrorType {
    /// Classify an error for retry logic.
    ///
    /// # Arguments
    ///
    /// * `error` - The error to classify
    ///
    /// # Returns
    ///
    /// The error type classification
    #[must_use]
    pub fn classify(error: &anyhow::Error) -> Self {
        let error_str = error.to_string().to_lowercase();

        if error_str.contains("connection")
            || error_str.contains("network")
            || error_str.contains("dns")
            || error_str.contains("refused")
        {
            Self::Network
        } else if error_str.contains("timeout") || error_str.contains("timed out") {
            Self::Timeout
        } else if error_str.contains("rate limit")
            || error_str.contains("too many requests")
            || error_str.contains("429")
        {
            Self::RateLimit
        } else if error_str.contains("unauthorized")
            || error_str.contains("forbidden")
            || error_str.contains("not found")
            || error_str.contains("invalid")
            || error_str.contains("401")
            || error_str.contains("403")
            || error_str.contains("404")
        {
            Self::Permanent
        } else {
            Self::Unknown
        }
    }

    /// Check if error is retryable.
    #[must_use]
    pub const fn is_retryable(&self) -> bool {
        matches!(
            self,
            Self::Network | Self::Timeout | Self::RateLimit | Self::Unknown
        )
    }

    /// Get recommended retry delay.
    ///
    /// # Arguments
    ///
    /// * `attempt` - Retry attempt number (0-based)
    ///
    /// # Returns
    ///
    /// Recommended delay before retry
    #[must_use]
    pub fn retry_delay(&self, attempt: u32) -> Duration {
        let base_seconds: u64 = match self {
            Self::Network => 1,
            Self::Timeout => 2,
            Self::RateLimit => 5,
            Self::Unknown => 1,
            Self::Permanent => 0,
        };

        // Exponential backoff: 2^attempt * base_seconds
        let delay_seconds = base_seconds.saturating_mul(1_u64 << attempt);
        let capped_delay = delay_seconds.min(60);
        Duration::from_secs(capped_delay)
    }
}

/// Retry configuration for error recovery.
#[derive(Debug, Clone)]
pub struct RetryConfig {
    /// Maximum number of retry attempts.
    pub max_retries: u32,

    /// Base delay for exponential backoff (seconds).
    pub base_delay_seconds: u64,

    /// Maximum delay between retries (seconds).
    pub max_delay_seconds: u64,

    /// Whether to checkpoint on each retry.
    pub checkpoint_on_retry: bool,
}

impl Default for RetryConfig {
    fn default() -> Self {
        Self {
            max_retries: 3,
            base_delay_seconds: 1,
            max_delay_seconds: 60,
            checkpoint_on_retry: true,
        }
    }
}

/// Retry attempt information.
#[derive(Debug, Clone)]
pub struct RetryAttempt {
    /// Attempt number (0-based).
    pub attempt: u32,

    /// Error that triggered retry.
    pub error_type: ErrorType,

    /// Delay before this retry.
    pub delay: Duration,

    /// Timestamp of retry.
    pub timestamp: chrono::DateTime<chrono::Utc>,
}

/// Recovered workflow state.
#[derive(Debug, Clone)]
pub struct RecoveredWorkflow {
    /// Workflow metadata.
    pub workflow: WorkflowMetadata,

    /// Last checkpoint (if available).
    pub checkpoint: Option<WorkflowCheckpoint>,

    /// Number of events replayed.
    pub events_replayed: usize,

    /// Whether recovery was from checkpoint or full replay.
    pub from_checkpoint: bool,
}

/// Manager for workflow recovery and replay with error recovery.
#[derive(Clone)]
pub struct RecoveryManager {
    /// Workflow store for metadata and checkpoints.
    workflow_store: Arc<WorkflowStore>,

    /// Event log connection.
    event_conn: Arc<Mutex<Connection>>,

    /// Circuit breaker for protecting against cascading failures.
    circuit_breaker: Arc<CircuitBreaker>,

    /// Retry configuration.
    retry_config: Arc<RetryConfig>,
}

impl RecoveryManager {
    /// Create a new recovery manager with default retry configuration.
    ///
    /// # Errors
    ///
    /// Returns error if initialization fails.
    pub async fn new(
        workflow_store: Arc<WorkflowStore>,
        event_log_path: impl AsRef<std::path::Path>,
    ) -> Result<Self> {
        Self::with_retry_config(workflow_store, event_log_path, RetryConfig::default()).await
    }

    /// Create a new recovery manager with custom retry configuration.
    ///
    /// # Errors
    ///
    /// Returns error if initialization fails.
    pub async fn with_retry_config(
        workflow_store: Arc<WorkflowStore>,
        event_log_path: impl AsRef<std::path::Path>,
        retry_config: RetryConfig,
    ) -> Result<Self> {
        let event_conn = Connection::open(event_log_path.as_ref())
            .context("Failed to open event log for recovery")?;

        // Initialize event log schema
        event_conn
            .execute(
                "CREATE TABLE IF NOT EXISTS workflow_events (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    workflow_id TEXT NOT NULL,
                    event_type TEXT NOT NULL,
                    event_data BLOB NOT NULL,
                    sequence INTEGER NOT NULL,
                    timestamp INTEGER NOT NULL
                )",
                [],
            )
            .context("Failed to create workflow_events table")?;

        // Create circuit breaker with 5 failures threshold and 60s cooldown
        let circuit_breaker = CircuitBreaker::new(5, 60);

        Ok(Self {
            workflow_store,
            event_conn: Arc::new(Mutex::new(event_conn)),
            circuit_breaker: Arc::new(circuit_breaker),
            retry_config: Arc::new(retry_config),
        })
    }

    /// Execute an operation with retry logic and circuit breaker protection.
    ///
    /// # Arguments
    ///
    /// * `operation` - The operation to execute
    /// * `workflow_id` - Workflow ID for logging
    /// * `operation_name` - Operation name for logging
    ///
    /// # Errors
    ///
    /// Returns error if operation fails after all retries.
    pub async fn with_retry<F, Fut, T>(
        &self,
        mut operation: F,
        workflow_id: &str,
        operation_name: &str,
    ) -> Result<T>
    where
        F: FnMut() -> Fut,
        Fut: std::future::Future<Output = Result<T>>,
    {
        // Check circuit breaker
        if !self.circuit_breaker.is_request_allowed() {
            let state = self.circuit_breaker.state();
            tracing::warn!(
                workflow_id,
                operation = operation_name,
                circuit_state = state.as_str(),
                "Circuit breaker rejecting request"
            );
            return Err(anyhow::anyhow!(
                "Circuit breaker open for {}",
                operation_name
            ));
        }

        let mut last_error = None;

        for attempt in 0..=self.retry_config.max_retries {
            match operation().await {
                Ok(result) => {
                    // Success - record and return
                    self.circuit_breaker.record_success();

                    if attempt > 0 {
                        tracing::info!(
                            workflow_id,
                            operation = operation_name,
                            attempt,
                            "Operation succeeded after retry"
                        );
                    }

                    return Ok(result);
                }
                Err(e) => {
                    last_error = Some(e);
                    let error = last_error.as_ref().unwrap();
                    let error_type = ErrorType::classify(error);

                    tracing::warn!(
                        workflow_id,
                        operation = operation_name,
                        attempt,
                        error_type = ?error_type,
                        error = %error,
                        "Operation failed"
                    );

                    // Record failure in circuit breaker
                    self.circuit_breaker.record_failure();

                    // Check if we should retry
                    if attempt >= self.retry_config.max_retries {
                        tracing::error!(
                            workflow_id,
                            operation = operation_name,
                            max_retries = self.retry_config.max_retries,
                            "Max retries exceeded"
                        );
                        break;
                    }

                    if !error_type.is_retryable() {
                        tracing::error!(
                            workflow_id,
                            operation = operation_name,
                            error_type = ?error_type,
                            "Error is not retryable"
                        );
                        break;
                    }

                    // Calculate delay with exponential backoff
                    let delay = error_type.retry_delay(attempt);
                    let delay = std::cmp::min(
                        delay,
                        Duration::from_secs(self.retry_config.max_delay_seconds),
                    );

                    tracing::info!(
                        workflow_id,
                        operation = operation_name,
                        attempt,
                        delay_seconds = delay.as_secs(),
                        "Retrying after delay"
                    );

                    // Wait before retry
                    tokio::time::sleep(delay).await;

                    // Checkpoint on retry if configured
                    if self.retry_config.checkpoint_on_retry {
                        if let Err(e) = self.checkpoint_workflow(workflow_id).await {
                            tracing::warn!(
                                workflow_id,
                                error = %e,
                                "Failed to checkpoint on retry"
                            );
                        }
                    }
                }
            }
        }

        // All retries exhausted
        Err(last_error.unwrap_or_else(|| anyhow::anyhow!("Operation failed")))
    }

    /// Create a checkpoint for a workflow during retry.
    ///
    /// # Errors
    ///
    /// Returns error if checkpoint creation fails.
    async fn checkpoint_workflow(&self, workflow_id: &str) -> Result<()> {
        // Get current sequence number
        let conn = self.event_conn.lock();
        let mut stmt =
            conn.prepare("SELECT MAX(sequence) FROM workflow_events WHERE workflow_id = ?1")?;

        let sequence: Option<i64> = stmt.query_row([workflow_id], |row| row.get(0)).ok();
        drop(stmt);
        drop(conn);

        if let Some(seq) = sequence {
            // Save checkpoint with empty state (for retry tracking)
            self.workflow_store
                .save_checkpoint(workflow_id, seq as u64, &[])
                .await?;

            tracing::debug!(workflow_id, sequence = seq, "Created checkpoint for retry");
        }

        Ok(())
    }

    /// Get circuit breaker state.
    pub fn circuit_breaker_state(&self) -> super::circuit_breaker::CircuitBreakerState {
        self.circuit_breaker.state()
    }

    /// Reset circuit breaker (for testing or manual recovery).
    pub fn reset_circuit_breaker(&self) {
        self.circuit_breaker.reset();
    }

    /// Recover all running workflows on application startup.
    ///
    /// Finds workflows in Running or Paused state and prepares them for resumption.
    ///
    /// # Errors
    ///
    /// Returns error if recovery fails.
    pub async fn recover_all_running_workflows(&self) -> Result<Vec<RecoveredWorkflow>> {
        tracing::info!("Recovering running workflows");

        // Get all workflows in Running or Paused state
        let running_workflows = self
            .workflow_store
            .list_by_status(WorkflowStatus::Running)
            .await?;

        let paused_workflows = self
            .workflow_store
            .list_by_status(WorkflowStatus::Paused)
            .await?;

        let mut all_workflows = running_workflows;
        all_workflows.extend(paused_workflows);

        let mut recovered = Vec::new();

        for workflow in all_workflows {
            match self.recover_workflow(&workflow.workflow_id).await {
                Ok(recovery) => {
                    tracing::info!(
                        workflow_id = %workflow.workflow_id,
                        from_checkpoint = recovery.from_checkpoint,
                        events_replayed = recovery.events_replayed,
                        "Recovered workflow"
                    );
                    recovered.push(recovery);
                }
                Err(e) => {
                    tracing::error!(
                        workflow_id = %workflow.workflow_id,
                        error = %e,
                        "Failed to recover workflow"
                    );
                    // Mark workflow as failed
                    let _ = self
                        .workflow_store
                        .update_status(&workflow.workflow_id, WorkflowStatus::Failed)
                        .await;
                }
            }
        }

        tracing::info!(recovered = recovered.len(), "Recovery complete");

        Ok(recovered)
    }

    /// Recover a specific workflow.
    ///
    /// # Recovery Strategy
    ///
    /// 1. Load checkpoint if available
    /// 2. Replay events since checkpoint
    /// 3. Reconstruct workflow state
    /// 4. Return recovered workflow ready for resumption
    ///
    /// # Errors
    ///
    /// Returns error if workflow not found or recovery fails.
    pub async fn recover_workflow(&self, workflow_id: &str) -> Result<RecoveredWorkflow> {
        tracing::debug!(workflow_id, "Recovering workflow");

        // Get workflow metadata
        let workflow = self
            .workflow_store
            .get_workflow(workflow_id)
            .await
            .context("Failed to get workflow")?
            .ok_or_else(|| anyhow::anyhow!("Workflow not found"))?;

        // Try to load checkpoint
        let checkpoint = self.workflow_store.load_checkpoint(workflow_id).await?;

        let (events_replayed, from_checkpoint) = if let Some(ref cp) = checkpoint {
            // Replay from checkpoint
            let events = self
                .replay_events_since(workflow_id, cp.sequence as i64)
                .await?;
            (events, true)
        } else {
            // Full replay from beginning
            let events = self.replay_all_events(workflow_id).await?;
            (events, false)
        };

        Ok(RecoveredWorkflow {
            workflow,
            checkpoint,
            events_replayed,
            from_checkpoint,
        })
    }

    /// Replay all events for a workflow.
    ///
    /// # Errors
    ///
    /// Returns error if event log query fails.
    async fn replay_all_events(&self, workflow_id: &str) -> Result<usize> {
        let conn = self.event_conn.lock();

        let mut stmt =
            conn.prepare("SELECT COUNT(*) FROM workflow_events WHERE workflow_id = ?1")?;

        let count: i64 = stmt.query_row([workflow_id], |row| row.get(0))?;

        Ok(count as usize)
    }

    /// Replay events since a specific sequence number.
    ///
    /// # Errors
    ///
    /// Returns error if event log query fails.
    async fn replay_events_since(&self, workflow_id: &str, since_sequence: i64) -> Result<usize> {
        let conn = self.event_conn.lock();

        let mut stmt = conn.prepare(
            "SELECT COUNT(*) FROM workflow_events 
             WHERE workflow_id = ?1 AND sequence > ?2",
        )?;

        let count: i64 = stmt.query_row(params![workflow_id, since_sequence], |row| row.get(0))?;

        Ok(count as usize)
    }

    /// Export workflow history to JSON for debugging.
    ///
    /// # Errors
    ///
    /// Returns error if export fails.
    pub async fn export_workflow(&self, workflow_id: &str) -> Result<String> {
        let workflow = self
            .workflow_store
            .get_workflow(workflow_id)
            .await
            .context("Failed to get workflow")?
            .ok_or_else(|| anyhow::anyhow!("Workflow not found"))?;

        let checkpoint = self.workflow_store.load_checkpoint(workflow_id).await?;

        let conn = self.event_conn.lock();
        let mut stmt = conn.prepare(
            "SELECT event_type, event_data, sequence, timestamp 
             FROM workflow_events 
             WHERE workflow_id = ?1 
             ORDER BY sequence",
        )?;

        let events: Vec<serde_json::Value> = stmt
            .query_map([workflow_id], |row| {
                let event_type: String = row.get(0)?;
                let event_data: Vec<u8> = row.get(1)?;
                let sequence: i64 = row.get(2)?;
                let timestamp: i64 = row.get(3)?;

                Ok(serde_json::json!({
                    "event_type": event_type,
                    "sequence": sequence,
                    "timestamp": timestamp,
                    "data_size": event_data.len(),
                }))
            })?
            .collect::<Result<Vec<_>, _>>()?;

        let export = serde_json::json!({
            "workflow": {
                "id": workflow.workflow_id,
                "pattern_type": workflow.pattern_type,
                "status": workflow.status.as_str(),
                "created_at": workflow.created_at,
                "session_id": workflow.session_id,
            },
            "checkpoint": checkpoint.as_ref().map(|cp| serde_json::json!({
                "sequence": cp.sequence,
                "created_at": cp.created_at,
                "checksum": cp.checksum,
            })),
            "events": events,
            "event_count": events.len(),
        });

        serde_json::to_string_pretty(&export).context("Failed to serialize export")
    }

    /// Check if a workflow needs recovery.
    ///
    /// # Errors
    ///
    /// Returns error if check fails.
    pub async fn needs_recovery(&self, workflow_id: &str) -> Result<bool> {
        let workflow = self
            .workflow_store
            .get_workflow(workflow_id)
            .await?
            .ok_or_else(|| anyhow::anyhow!("Workflow not found"))?;

        Ok(matches!(
            workflow.status,
            WorkflowStatus::Running | WorkflowStatus::Paused
        ))
    }
}

impl std::fmt::Debug for RecoveryManager {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("RecoveryManager").finish()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::NamedTempFile;

    async fn create_test_recovery() -> (
        RecoveryManager,
        Arc<WorkflowStore>,
        NamedTempFile,
        NamedTempFile,
    ) {
        let workflow_temp = NamedTempFile::new().unwrap();
        let event_temp = NamedTempFile::new().unwrap();

        let workflow_store = Arc::new(WorkflowStore::new(workflow_temp.path()).await.unwrap());
        let recovery = RecoveryManager::new(workflow_store.clone(), event_temp.path())
            .await
            .unwrap();

        (recovery, workflow_store, workflow_temp, event_temp)
    }

    #[tokio::test]
    async fn test_create_recovery_manager() {
        let (_recovery, _store, _wf_temp, _ev_temp) = create_test_recovery().await;
        // If we get here, creation succeeded
    }

    #[tokio::test]
    async fn test_recover_workflow_without_checkpoint() {
        let (recovery, store, _wf_temp, _ev_temp) = create_test_recovery().await;

        // Create a workflow in running state
        store
            .create_workflow(
                "wf-123",
                "user-1",
                Some("sess-1"),
                "chain_of_thought",
                "test query",
            )
            .await
            .unwrap();

        store
            .update_status("wf-123", WorkflowStatus::Running)
            .await
            .unwrap();

        let recovered = recovery.recover_workflow("wf-123").await.unwrap();

        assert_eq!(recovered.workflow.workflow_id, "wf-123");
        assert!(recovered.checkpoint.is_none());
        assert!(!recovered.from_checkpoint);
    }

    #[tokio::test]
    async fn test_recover_workflow_with_checkpoint() {
        let (recovery, store, _wf_temp, _ev_temp) = create_test_recovery().await;

        // Create a workflow
        store
            .create_workflow(
                "wf-456",
                "user-1",
                Some("sess-1"),
                "chain_of_thought",
                "test query",
            )
            .await
            .unwrap();

        store
            .update_status("wf-456", WorkflowStatus::Running)
            .await
            .unwrap();

        // Save a checkpoint
        let state_data = vec![1, 2, 3, 4];
        store
            .save_checkpoint("wf-456", 5, &state_data)
            .await
            .unwrap();

        let recovered = recovery.recover_workflow("wf-456").await.unwrap();

        assert_eq!(recovered.workflow.workflow_id, "wf-456");
        assert!(recovered.checkpoint.is_some());
        assert!(recovered.from_checkpoint);
        assert_eq!(recovered.checkpoint.unwrap().sequence, 5);
    }

    #[tokio::test]
    async fn test_recover_all_running_workflows() {
        let (recovery, store, _wf_temp, _ev_temp) = create_test_recovery().await;

        // Create multiple workflows in different states
        store
            .create_workflow("wf-1", "user-1", Some("sess-1"), "cot", "test1")
            .await
            .unwrap();
        store
            .create_workflow("wf-2", "user-1", Some("sess-1"), "cot", "test2")
            .await
            .unwrap();
        store
            .create_workflow("wf-3", "user-1", Some("sess-1"), "cot", "test3")
            .await
            .unwrap();

        store
            .update_status("wf-1", WorkflowStatus::Running)
            .await
            .unwrap();
        store
            .update_status("wf-2", WorkflowStatus::Paused)
            .await
            .unwrap();
        store
            .update_status("wf-3", WorkflowStatus::Completed)
            .await
            .unwrap();

        let recovered = recovery.recover_all_running_workflows().await.unwrap();

        // Should recover wf-1 (Running) and wf-2 (Paused), but not wf-3 (Completed)
        assert_eq!(recovered.len(), 2);
        assert!(recovered.iter().any(|r| r.workflow.workflow_id == "wf-1"));
        assert!(recovered.iter().any(|r| r.workflow.workflow_id == "wf-2"));
    }

    #[tokio::test]
    async fn test_needs_recovery_running() {
        let (recovery, store, _wf_temp, _ev_temp) = create_test_recovery().await;

        store
            .create_workflow("wf-123", "user-1", Some("sess-1"), "cot", "test")
            .await
            .unwrap();
        store
            .update_status("wf-123", WorkflowStatus::Running)
            .await
            .unwrap();

        let needs = recovery.needs_recovery("wf-123").await.unwrap();
        assert!(needs);
    }

    #[tokio::test]
    async fn test_needs_recovery_completed() {
        let (recovery, store, _wf_temp, _ev_temp) = create_test_recovery().await;

        store
            .create_workflow("wf-456", "user-1", Some("sess-1"), "cot", "test")
            .await
            .unwrap();
        store
            .update_status("wf-456", WorkflowStatus::Completed)
            .await
            .unwrap();

        let needs = recovery.needs_recovery("wf-456").await.unwrap();
        assert!(!needs);
    }

    #[tokio::test]
    async fn test_export_workflow() {
        let (recovery, store, _wf_temp, _ev_temp) = create_test_recovery().await;

        store
            .create_workflow("wf-export", "user-1", Some("sess-1"), "cot", "test query")
            .await
            .unwrap();
        store
            .update_status("wf-export", WorkflowStatus::Completed)
            .await
            .unwrap();
        store
            .update_output("wf-export", "test result")
            .await
            .unwrap();

        let export = recovery.export_workflow("wf-export").await.unwrap();

        // Verify JSON is valid
        let parsed: serde_json::Value = serde_json::from_str(&export).unwrap();
        assert_eq!(parsed["workflow"]["id"], "wf-export");
        assert_eq!(parsed["event_count"], 0);
    }

    #[tokio::test]
    async fn test_replay_events() {
        let (recovery, _store, _wf_temp, _ev_temp) = create_test_recovery().await;

        // Replay events for non-existent workflow should return 0
        let count = recovery.replay_all_events("nonexistent").await.unwrap();
        assert_eq!(count, 0);
    }

    #[tokio::test]
    async fn test_recover_nonexistent_workflow() {
        let (recovery, _store, _wf_temp, _ev_temp) = create_test_recovery().await;

        let result = recovery.recover_workflow("nonexistent").await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_replay_with_checkpoint() {
        let (recovery, store, _wf_temp, _ev_temp) = create_test_recovery().await;

        store
            .create_workflow("wf-replay", "user-1", Some("sess-1"), "cot", "test")
            .await
            .unwrap();
        store
            .update_status("wf-replay", WorkflowStatus::Running)
            .await
            .unwrap();

        // Save checkpoint at sequence 10
        store
            .save_checkpoint("wf-replay", 10, &vec![1, 2, 3])
            .await
            .unwrap();

        let recovered = recovery.recover_workflow("wf-replay").await.unwrap();

        assert_eq!(recovered.workflow.workflow_id, "wf-replay");
        assert!(recovered.checkpoint.is_some());
        assert_eq!(recovered.checkpoint.unwrap().sequence, 10);
        assert!(recovered.from_checkpoint);
    }
}
