//! Workflow metadata storage and management.
//!
//! Provides CRUD operations for workflow metadata, checkpoints, and state management.
//! Complements the event log with structured workflow information.
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::database::WorkflowStore;
//!
//! let store = WorkflowStore::new("./shannon.db").await?;
//!
//! // Create workflow
//! let workflow = store.create_workflow("wf-123", "user-1", "session-1", "chain_of_thought").await?;
//!
//! // Save checkpoint
//! store.save_checkpoint("wf-123", &state_bytes).await?;
//!
//! // Load checkpoint
//! let state = store.load_checkpoint("wf-123").await?;
//! ```

use std::path::PathBuf;

use anyhow::{Context, Result};
use rusqlite::{params, Connection, OptionalExtension};
use serde::{Deserialize, Serialize};
use tokio::task;

/// Workflow status enum.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum WorkflowStatus {
    /// Workflow is pending execution.
    Pending,
    /// Workflow is currently running.
    Running,
    /// Workflow is paused.
    Paused,
    /// Workflow completed successfully.
    Completed,
    /// Workflow failed with error.
    Failed,
    /// Workflow was cancelled by user.
    Cancelled,
}

impl WorkflowStatus {
    /// Convert status to string for database storage.
    #[must_use]
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Pending => "pending",
            Self::Running => "running",
            Self::Paused => "paused",
            Self::Completed => "completed",
            Self::Failed => "failed",
            Self::Cancelled => "cancelled",
        }
    }
    
    /// Parse status from database string.
    ///
    /// # Errors
    ///
    /// Returns error if status string is invalid.
    #[allow(clippy::should_implement_trait, reason = "Different signature than std::str::FromStr")]
    pub fn from_str(s: &str) -> Result<Self> {
        match s {
            "pending" => Ok(Self::Pending),
            "running" => Ok(Self::Running),
            "paused" => Ok(Self::Paused),
            "completed" => Ok(Self::Completed),
            "failed" => Ok(Self::Failed),
            "cancelled" => Ok(Self::Cancelled),
            _ => anyhow::bail!("Invalid workflow status: {s}"),
        }
    }
}

/// Workflow metadata.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorkflowMetadata {
    /// Unique workflow identifier.
    pub workflow_id: String,
    /// User who submitted the workflow.
    pub user_id: String,
    /// Session identifier for conversation context.
    pub session_id: Option<String>,
    /// Workflow pattern type (e.g., `chain_of_thought`, `research`).
    pub pattern_type: String,
    /// Current workflow status.
    pub status: WorkflowStatus,
    /// Input query or task description.
    pub input: String,
    /// Final output (if completed).
    pub output: Option<String>,
    /// Error message (if failed).
    pub error: Option<String>,
    /// Creation timestamp (Unix epoch seconds).
    pub created_at: i64,
    /// Last update timestamp (Unix epoch seconds).
    pub updated_at: i64,
    /// Completion timestamp (Unix epoch seconds).
    pub completed_at: Option<i64>,
}

/// Workflow checkpoint.
#[derive(Debug, Clone)]
pub struct WorkflowCheckpoint {
    /// Workflow identifier.
    pub workflow_id: String,
    /// Checkpoint sequence number.
    pub sequence: u64,
    /// Serialized workflow state.
    pub state: Vec<u8>,
    /// Creation timestamp (Unix epoch seconds).
    pub created_at: i64,
    /// Checksum for corruption detection (CRC32).
    pub checksum: u32,
}

/// Workflow metadata store.
///
/// Provides CRUD operations for workflows, sessions, and checkpoints.
/// Uses `SQLite` for durable storage with write-ahead logging.
///
/// # Thread Safety
///
/// All operations use `tokio::spawn_blocking` to ensure database
/// operations run on a dedicated thread pool, making the store
/// safe for concurrent async access.
#[derive(Debug, Clone)]
pub struct WorkflowStore {
    /// Path to `SQLite` database file.
    db_path: PathBuf,
}

impl WorkflowStore {
    /// Create a new workflow store.
    ///
    /// # Arguments
    ///
    /// * `path` - Path to `SQLite` database file
    ///
    /// # Errors
    ///
    /// Returns error if database cannot be opened or schema migration fails.
    ///
    /// # Example
    ///
    /// ```rust,ignore
    /// let store = WorkflowStore::new("./shannon.db").await?;
    /// ```
    pub async fn new<P: Into<PathBuf>>(path: P) -> Result<Self> {
        let db_path = path.into();
        
        let store = Self { db_path };
        
        // Initialize schema
        store.migrate_schema().await?;
        
        Ok(store)
    }
    
    /// Migrate database schema to latest version.
    ///
    /// Creates all required tables and indexes.
    async fn migrate_schema(&self) -> Result<()> {
        let db_path = self.db_path.clone();
        
        task::spawn_blocking(move || -> Result<()> {
            let conn = Connection::open(&db_path)
                .context("Failed to open database")?;
            
            // Enable WAL mode
            conn.pragma_update(None, "journal_mode", "WAL")
                .context("Failed to enable WAL mode")?;
            
            // Create workflows table
            conn.execute(
                r"
                CREATE TABLE IF NOT EXISTS workflows (
                    workflow_id TEXT PRIMARY KEY,
                    user_id TEXT NOT NULL,
                    session_id TEXT,
                    pattern_type TEXT NOT NULL,
                    status TEXT NOT NULL DEFAULT 'pending',
                    input TEXT NOT NULL,
                    output TEXT,
                    error TEXT,
                    created_at INTEGER NOT NULL,
                    updated_at INTEGER NOT NULL,
                    completed_at INTEGER
                )
                ",
                [],
            )
            .context("Failed to create workflows table")?;
            
            // Create indexes
            conn.execute(
                "CREATE INDEX IF NOT EXISTS idx_workflows_user ON workflows(user_id)",
                [],
            )?;
            conn.execute(
                "CREATE INDEX IF NOT EXISTS idx_workflows_status ON workflows(status)",
                [],
            )?;
            conn.execute(
                "CREATE INDEX IF NOT EXISTS idx_workflows_session ON workflows(session_id)",
                [],
            )?;
            
            // Create checkpoints table
            conn.execute(
                r"
                CREATE TABLE IF NOT EXISTS workflow_checkpoints (
                    workflow_id TEXT NOT NULL,
                    sequence INTEGER NOT NULL,
                    state BLOB NOT NULL,
                    checksum INTEGER NOT NULL,
                    created_at INTEGER NOT NULL,
                    PRIMARY KEY (workflow_id, sequence)
                )
                ",
                [],
            )
            .context("Failed to create workflow_checkpoints table")?;
            
            conn.execute(
                "CREATE INDEX IF NOT EXISTS idx_checkpoints_workflow ON workflow_checkpoints(workflow_id)",
                [],
            )?;
            
            Ok(())
        })
        .await
        .context("Failed to spawn blocking task")??;
        
        Ok(())
    }
    
    /// Create a new workflow.
    ///
    /// # Errors
    ///
    /// Returns error if workflow already exists or database operation fails.
    pub async fn create_workflow(
        &self,
        workflow_id: &str,
        user_id: &str,
        session_id: Option<&str>,
        pattern_type: &str,
        input: &str,
    ) -> Result<WorkflowMetadata> {
        let workflow_id = workflow_id.to_string();
        let user_id = user_id.to_string();
        let session_id = session_id.map(ToString::to_string);
        let pattern_type = pattern_type.to_string();
        let input = input.to_string();
        let db_path = self.db_path.clone();
        
        task::spawn_blocking(move || -> Result<WorkflowMetadata> {
            let conn = Connection::open(&db_path)?;
            
            let now = chrono::Utc::now().timestamp();
            
            conn.execute(
                r"
                INSERT INTO workflows (workflow_id, user_id, session_id, pattern_type, status, input, created_at, updated_at)
                VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)
                ",
                params![
                    &workflow_id,
                    &user_id,
                    &session_id,
                    &pattern_type,
                    WorkflowStatus::Pending.as_str(),
                    &input,
                    now,
                    now
                ],
            )
            .context("Failed to insert workflow")?;
            
            Ok(WorkflowMetadata {
                workflow_id,
                user_id,
                session_id,
                pattern_type,
                status: WorkflowStatus::Pending,
                input,
                output: None,
                error: None,
                created_at: now,
                updated_at: now,
                completed_at: None,
            })
        })
        .await?
        .context("Failed to spawn blocking task")
    }
    
    /// Get workflow by ID.
    ///
    /// # Errors
    ///
    /// Returns error if workflow not found or database operation fails.
    pub async fn get_workflow(&self, workflow_id: &str) -> Result<Option<WorkflowMetadata>> {
        let workflow_id = workflow_id.to_string();
        let db_path = self.db_path.clone();
        
        task::spawn_blocking(move || -> Result<Option<WorkflowMetadata>> {
            let conn = Connection::open(&db_path)?;
            
            let workflow: Option<WorkflowMetadata> = conn
                .query_row(
                    r"
                    SELECT workflow_id, user_id, session_id, pattern_type, status, input, output, error,
                           created_at, updated_at, completed_at
                    FROM workflows
                    WHERE workflow_id = ?1
                    ",
                    params![&workflow_id],
                    |row| {
                        Ok(WorkflowMetadata {
                            workflow_id: row.get(0)?,
                            user_id: row.get(1)?,
                            session_id: row.get(2)?,
                            pattern_type: row.get(3)?,
                            status: WorkflowStatus::from_str(&row.get::<_, String>(4)?).unwrap(),
                            input: row.get(5)?,
                            output: row.get(6)?,
                            error: row.get(7)?,
                            created_at: row.get(8)?,
                            updated_at: row.get(9)?,
                            completed_at: row.get(10)?,
                        })
                    },
                )
                .optional()
                .context("Failed to query workflow")?;
            
            Ok(workflow)
        })
        .await?
        .context("Failed to spawn blocking task")
    }
    
    /// Update workflow status.
    ///
    /// # Errors
    ///
    /// Returns error if workflow not found or database operation fails.
    pub async fn update_status(&self, workflow_id: &str, status: WorkflowStatus) -> Result<()> {
        let workflow_id = workflow_id.to_string();
        let db_path = self.db_path.clone();
        
        task::spawn_blocking(move || -> Result<()> {
            let conn = Connection::open(&db_path)?;
            
            let now = chrono::Utc::now().timestamp();
            let completed_at = matches!(status, WorkflowStatus::Completed | WorkflowStatus::Failed | WorkflowStatus::Cancelled)
                .then_some(now);
            
            conn.execute(
                r"
                UPDATE workflows
                SET status = ?1, updated_at = ?2, completed_at = ?3
                WHERE workflow_id = ?4
                ",
                params![status.as_str(), now, completed_at, &workflow_id],
            )
            .context("Failed to update workflow status")?;
            
            Ok(())
        })
        .await?
        .context("Failed to spawn blocking task")
    }
    
    /// Update workflow output.
    ///
    /// # Errors
    ///
    /// Returns error if database operation fails.
    pub async fn update_output(&self, workflow_id: &str, output: &str) -> Result<()> {
        let workflow_id = workflow_id.to_string();
        let output = output.to_string();
        let db_path = self.db_path.clone();
        
        task::spawn_blocking(move || -> Result<()> {
            let conn = Connection::open(&db_path)?;
            
            let now = chrono::Utc::now().timestamp();
            
            conn.execute(
                "UPDATE workflows SET output = ?1, updated_at = ?2 WHERE workflow_id = ?3",
                params![&output, now, &workflow_id],
            )
            .context("Failed to update workflow output")?;
            
            Ok(())
        })
        .await?
        .context("Failed to spawn blocking task")
    }
    
    /// Update workflow error.
    ///
    /// # Errors
    ///
    /// Returns error if database operation fails.
    pub async fn update_error(&self, workflow_id: &str, error: &str) -> Result<()> {
        let workflow_id = workflow_id.to_string();
        let error = error.to_string();
        let db_path = self.db_path.clone();
        
        task::spawn_blocking(move || -> Result<()> {
            let conn = Connection::open(&db_path)?;
            
            let now = chrono::Utc::now().timestamp();
            
            conn.execute(
                "UPDATE workflows SET error = ?1, updated_at = ?2 WHERE workflow_id = ?3",
                params![&error, now, &workflow_id],
            )
            .context("Failed to update workflow error")?;
            
            Ok(())
        })
        .await?
        .context("Failed to spawn blocking task")
    }
    
    /// List workflows with optional filtering and limit.
    ///
    /// # Errors
    ///
    /// Returns error if database operation fails.
    pub async fn list_workflows(
        &self,
        session_id: Option<String>,
        limit: usize,
    ) -> Result<Vec<WorkflowMetadata>> {
        let db_path = self.db_path.clone();

        task::spawn_blocking(move || -> Result<Vec<WorkflowMetadata>> {
            let conn = Connection::open(&db_path)?;

            let mut query = "
                SELECT workflow_id, user_id, session_id, pattern_type, status, input, output, error,
                       created_at, updated_at, completed_at
                FROM workflows
            "
            .to_string();

            if session_id.is_some() {
                query.push_str(" WHERE session_id = ?1");
            }
            query.push_str(" ORDER BY created_at DESC LIMIT ?2");

            let mut stmt = conn.prepare(&query)?;

            let limit_i64 = limit as i64;
            let workflows = if let Some(ref session) = session_id {
                stmt.query_map(params![session, limit_i64], |row| {
                    Ok(WorkflowMetadata {
                        workflow_id: row.get(0)?,
                        user_id: row.get(1)?,
                        session_id: row.get(2)?,
                        pattern_type: row.get(3)?,
                        status: WorkflowStatus::from_str(&row.get::<_, String>(4)?).unwrap(),
                        input: row.get(5)?,
                        output: row.get(6)?,
                        error: row.get(7)?,
                        created_at: row.get(8)?,
                        updated_at: row.get(9)?,
                        completed_at: row.get(10)?,
                    })
                })?
                .collect::<Result<Vec<_>, _>>()?
            } else {
                stmt.query_map(params![None::<String>, limit_i64], |row| {
                    Ok(WorkflowMetadata {
                        workflow_id: row.get(0)?,
                        user_id: row.get(1)?,
                        session_id: row.get(2)?,
                        pattern_type: row.get(3)?,
                        status: WorkflowStatus::from_str(&row.get::<_, String>(4)?).unwrap(),
                        input: row.get(5)?,
                        output: row.get(6)?,
                        error: row.get(7)?,
                        created_at: row.get(8)?,
                        updated_at: row.get(9)?,
                        completed_at: row.get(10)?,
                    })
                })?
                .collect::<Result<Vec<_>, _>>()?
            };

            Ok(workflows)
        })
        .await?
        .context("Failed to spawn blocking task")
    }

    /// List workflows by status.
    ///
    /// # Errors
    ///
    /// Returns error if database operation fails.
    pub async fn list_by_status(&self, status: WorkflowStatus) -> Result<Vec<WorkflowMetadata>> {
        self.list_workflows(None, 100).await.map(|list| {
            list.into_iter().filter(|w| w.status == status).collect()
        })
    }

    /// List workflows by session.
    ///
    /// # Errors
    ///
    /// Returns error if database operation fails.
    pub async fn list_by_session(&self, session_id: &str) -> Result<Vec<WorkflowMetadata>> {
        self.list_workflows(Some(session_id.to_string()), 100).await
    }

    /// Delete a workflow and its checkpoints.
    ///
    /// # Errors
    ///
    /// Returns error if database operation fails.
    pub async fn delete_workflow(&self, workflow_id: &str) -> Result<()> {
        let workflow_id = workflow_id.to_string();
        let db_path = self.db_path.clone();

        task::spawn_blocking(move || -> Result<()> {
            let conn = Connection::open(&db_path)?;

            // Delete in transaction
            conn.execute("BEGIN IMMEDIATE", [])?;

            match (|| -> Result<()> {
                conn.execute(
                    "DELETE FROM workflow_checkpoints WHERE workflow_id = ?1",
                    params![&workflow_id],
                )?;

                conn.execute(
                    "DELETE FROM workflows WHERE workflow_id = ?1",
                    params![&workflow_id],
                )?;

                Ok(())
            })() {
                Ok(()) => {
                    conn.execute("COMMIT", [])?;
                    Ok(())
                }
                Err(e) => {
                    conn.execute("ROLLBACK", []).ok();
                    Err(e)
                }
            }
        })
        .await?
        .context("Failed to spawn blocking task")
    }
    
    /// Save a workflow checkpoint.
    ///
    /// # Arguments
    ///
    /// * `workflow_id` - Workflow identifier
    /// * `sequence` - Checkpoint sequence number
    /// * `state` - Serialized workflow state
    ///
    /// # Errors
    ///
    /// Returns error if database operation fails.
    pub async fn save_checkpoint(&self, workflow_id: &str, sequence: u64, state: &[u8]) -> Result<()> {
        let workflow_id = workflow_id.to_string();
        let state = state.to_vec();
        let db_path = self.db_path.clone();
        
        task::spawn_blocking(move || -> Result<()> {
            let conn = Connection::open(&db_path)?;
            
            // Calculate CRC32 checksum
            let checksum = crc32fast::hash(&state);
            let now = chrono::Utc::now().timestamp();
            
            // Safe cast: sequence is always non-negative
            #[allow(clippy::cast_possible_wrap, reason = "sequence is always small enough for i64")]
            let sequence_i64 = sequence as i64;
            
            // Safe cast: checksum is u32, will always fit in i64
            #[allow(clippy::cast_lossless, reason = "u32 to i64 is always lossless")]
            let checksum_i64 = i64::from(checksum);
            
            conn.execute(
                r"
                INSERT INTO workflow_checkpoints (workflow_id, sequence, state, checksum, created_at)
                VALUES (?1, ?2, ?3, ?4, ?5)
                ON CONFLICT(workflow_id, sequence) DO UPDATE SET
                    state = excluded.state,
                    checksum = excluded.checksum,
                    created_at = excluded.created_at
                ",
                params![&workflow_id, sequence_i64, &state, checksum_i64, now],
            )
            .context("Failed to save checkpoint")?;
            
            Ok(())
        })
        .await?
        .context("Failed to spawn blocking task")
    }
    
    /// Load the latest checkpoint for a workflow.
    ///
    /// # Errors
    ///
    /// Returns error if database operation fails or checkpoint is corrupted.
    pub async fn load_checkpoint(&self, workflow_id: &str) -> Result<Option<WorkflowCheckpoint>> {
        let workflow_id = workflow_id.to_string();
        let db_path = self.db_path.clone();
        
        task::spawn_blocking(move || -> Result<Option<WorkflowCheckpoint>> {
            let conn = Connection::open(&db_path)?;
            
            let checkpoint: Option<(i64, Vec<u8>, i64, i64)> = conn
                .query_row(
                    r"
                    SELECT sequence, state, checksum, created_at
                    FROM workflow_checkpoints
                    WHERE workflow_id = ?1
                    ORDER BY sequence DESC
                    LIMIT 1
                    ",
                    params![&workflow_id],
                    |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?, row.get(3)?)),
                )
                .optional()?;
            
            if let Some((sequence, state, stored_checksum, created_at)) = checkpoint {
                // Verify checksum
                let computed_checksum = crc32fast::hash(&state);
                
                // Safe cast: stored_checksum came from u32 originally, truncation is impossible
                #[allow(clippy::cast_sign_loss, reason = "checksum was originally stored as u32")]
                #[allow(clippy::cast_possible_truncation, reason = "checksum is u32, cannot truncate from i64")]
                let stored_checksum_u32 = stored_checksum as u32;
                
                if computed_checksum != stored_checksum_u32 {
                    anyhow::bail!("Checkpoint corruption detected for workflow {workflow_id}");
                }
                
                // Safe cast: sequence is always non-negative
                #[allow(clippy::cast_sign_loss, reason = "sequence is always non-negative")]
                let sequence_u64 = sequence as u64;
                
                Ok(Some(WorkflowCheckpoint {
                    workflow_id,
                    sequence: sequence_u64,
                    state,
                    created_at,
                    checksum: computed_checksum,
                }))
            } else {
                Ok(None)
            }
        })
        .await?
        .context("Failed to spawn blocking task")
    }
    
    /// Delete old checkpoints, keeping only the latest N.
    ///
    /// # Arguments
    ///
    /// * `workflow_id` - Workflow identifier
    /// * `keep_count` - Number of recent checkpoints to keep
    ///
    /// # Errors
    ///
    /// Returns error if database operation fails.
    pub async fn prune_checkpoints(&self, workflow_id: &str, keep_count: usize) -> Result<u64> {
        let workflow_id = workflow_id.to_string();
        let db_path = self.db_path.clone();
        
        task::spawn_blocking(move || -> Result<u64> {
            let conn = Connection::open(&db_path)?;
            
            // Safe cast: keep_count is always reasonable size
            #[allow(clippy::cast_possible_wrap, reason = "keep_count is always small")]
            let keep_count_i64 = keep_count as i64;
            
            let deleted = conn.execute(
                r"
                DELETE FROM workflow_checkpoints
                WHERE workflow_id = ?1
                AND sequence NOT IN (
                    SELECT sequence FROM workflow_checkpoints
                    WHERE workflow_id = ?1
                    ORDER BY sequence DESC
                    LIMIT ?2
                )
                ",
                params![&workflow_id, keep_count_i64],
            )?;
            
            Ok(deleted as u64)
        })
        .await?
        .context("Failed to spawn blocking task")
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::NamedTempFile;
    
    async fn create_test_store() -> (WorkflowStore, NamedTempFile) {
        let temp_file = NamedTempFile::new().unwrap();
        let store = WorkflowStore::new(temp_file.path()).await.unwrap();
        (store, temp_file)
    }
    
    #[tokio::test]
    async fn test_create_workflow() {
        let (store, _temp) = create_test_store().await;
        
        let workflow = store
            .create_workflow("wf-1", "user-1", Some("session-1"), "chain_of_thought", "test query")
            .await
            .unwrap();
        
        assert_eq!(workflow.workflow_id, "wf-1");
        assert_eq!(workflow.user_id, "user-1");
        assert_eq!(workflow.session_id, Some("session-1".to_string()));
        assert_eq!(workflow.status, WorkflowStatus::Pending);
    }
    
    #[tokio::test]
    async fn test_get_workflow() {
        let (store, _temp) = create_test_store().await;
        
        store
            .create_workflow("wf-1", "user-1", None, "research", "test query")
            .await
            .unwrap();
        
        let workflow = store.get_workflow("wf-1").await.unwrap();
        assert!(workflow.is_some());
        assert_eq!(workflow.unwrap().pattern_type, "research");
    }
    
    #[tokio::test]
    async fn test_get_nonexistent_workflow() {
        let (store, _temp) = create_test_store().await;
        
        let workflow = store.get_workflow("nonexistent").await.unwrap();
        assert!(workflow.is_none());
    }
    
    #[tokio::test]
    async fn test_update_status() {
        let (store, _temp) = create_test_store().await;
        
        store
            .create_workflow("wf-1", "user-1", None, "chain_of_thought", "test")
            .await
            .unwrap();
        
        store.update_status("wf-1", WorkflowStatus::Running).await.unwrap();
        
        let workflow = store.get_workflow("wf-1").await.unwrap().unwrap();
        assert_eq!(workflow.status, WorkflowStatus::Running);
    }
    
    #[tokio::test]
    async fn test_update_output() {
        let (store, _temp) = create_test_store().await;
        
        store
            .create_workflow("wf-1", "user-1", None, "chain_of_thought", "test")
            .await
            .unwrap();
        
        store.update_output("wf-1", "test output").await.unwrap();
        
        let workflow = store.get_workflow("wf-1").await.unwrap().unwrap();
        assert_eq!(workflow.output, Some("test output".to_string()));
    }
    
    #[tokio::test]
    async fn test_list_by_status() {
        let (store, _temp) = create_test_store().await;
        
        store.create_workflow("wf-1", "user-1", None, "cot", "q1").await.unwrap();
        store.create_workflow("wf-2", "user-1", None, "cot", "q2").await.unwrap();
        store.update_status("wf-1", WorkflowStatus::Running).await.unwrap();
        
        let pending = store.list_by_status(WorkflowStatus::Pending).await.unwrap();
        assert_eq!(pending.len(), 1);
        assert_eq!(pending[0].workflow_id, "wf-2");
        
        let running = store.list_by_status(WorkflowStatus::Running).await.unwrap();
        assert_eq!(running.len(), 1);
        assert_eq!(running[0].workflow_id, "wf-1");
    }
    
    #[tokio::test]
    async fn test_list_by_session() {
        let (store, _temp) = create_test_store().await;
        
        store.create_workflow("wf-1", "user-1", Some("sess-1"), "cot", "q1").await.unwrap();
        store.create_workflow("wf-2", "user-1", Some("sess-1"), "cot", "q2").await.unwrap();
        store.create_workflow("wf-3", "user-1", Some("sess-2"), "cot", "q3").await.unwrap();
        
        let session1 = store.list_by_session("sess-1").await.unwrap();
        assert_eq!(session1.len(), 2);
    }
    
    #[tokio::test]
    async fn test_delete_workflow() {
        let (store, _temp) = create_test_store().await;
        
        store.create_workflow("wf-1", "user-1", None, "cot", "test").await.unwrap();
        store.delete_workflow("wf-1").await.unwrap();
        
        let workflow = store.get_workflow("wf-1").await.unwrap();
        assert!(workflow.is_none());
    }
    
    #[tokio::test]
    async fn test_save_checkpoint() {
        let (store, _temp) = create_test_store().await;
        
        store.create_workflow("wf-1", "user-1", None, "cot", "test").await.unwrap();
        
        let state = vec![1, 2, 3, 4, 5];
        store.save_checkpoint("wf-1", 0, &state).await.unwrap();
        
        let checkpoint = store.load_checkpoint("wf-1").await.unwrap();
        assert!(checkpoint.is_some());
        assert_eq!(checkpoint.unwrap().state, state);
    }
    
    #[tokio::test]
    async fn test_load_nonexistent_checkpoint() {
        let (store, _temp) = create_test_store().await;
        
        let checkpoint = store.load_checkpoint("nonexistent").await.unwrap();
        assert!(checkpoint.is_none());
    }
    
    #[tokio::test]
    async fn test_checkpoint_multiple_versions() {
        let (store, _temp) = create_test_store().await;
        
        store.create_workflow("wf-1", "user-1", None, "cot", "test").await.unwrap();
        
        // Save multiple checkpoint versions
        store.save_checkpoint("wf-1", 0, &vec![1, 2, 3]).await.unwrap();
        store.save_checkpoint("wf-1", 1, &vec![4, 5, 6]).await.unwrap();
        store.save_checkpoint("wf-1", 2, &vec![7, 8, 9]).await.unwrap();
        
        // Load should return the latest checkpoint
        let checkpoint = store.load_checkpoint("wf-1").await.unwrap();
        assert!(checkpoint.is_some());
        let cp = checkpoint.unwrap();
        assert_eq!(cp.sequence, 2);
        assert_eq!(cp.state, vec![7, 8, 9]);
        
        // Verify checksum is calculated correctly
        assert_eq!(cp.checksum, crc32fast::hash(&vec![7, 8, 9]));
    }
    
    #[tokio::test]
    async fn test_prune_checkpoints() {
        let (store, _temp) = create_test_store().await;
        
        store.create_workflow("wf-1", "user-1", None, "cot", "test").await.unwrap();
        
        // Save 5 checkpoints
        for i in 0..5 {
            store.save_checkpoint("wf-1", i, &vec![i as u8]).await.unwrap();
        }
        
        // Prune to keep only last 3
        let deleted = store.prune_checkpoints("wf-1", 3).await.unwrap();
        assert_eq!(deleted, 2); // Deleted 2 oldest checkpoints
    }
}
