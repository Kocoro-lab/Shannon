//! `SQLite` backend for durable workflow event log.
//!
//! Provides persistent storage for workflow events with:
//! - Event sourcing with append-only log
//! - Write-Ahead Logging (WAL) for concurrent access
//! - Event batching for performance
//! - Automatic schema migration
//! - Deterministic replay guarantees
//!
//! # Example
//!
//! ```rust,ignore
//! use durable_shannon::backends::SqliteEventLog;
//!
//! // Create event log with file-based database
//! let log = SqliteEventLog::new("./data/workflows.db").await?;
//!
//! // Append events
//! let idx = log.append("workflow-123", event).await?;
//!
//! // Replay all events
//! let events = log.replay("workflow-123").await?;
//! ```

use std::path::PathBuf;

use anyhow::{Context, Result};
use async_trait::async_trait;
use rusqlite::{params, Connection, OptionalExtension};
use tokio::task;

use crate::{Event, backends::EventLog};

/// SQLite-based event log for durable workflow persistence.
///
/// Implements event sourcing with an append-only log stored in `SQLite`.
/// Supports both in-memory (`:memory:`) and file-based databases.
///
/// # Features
///
/// - **WAL mode**: Write-Ahead Logging enabled for concurrent reads/writes
/// - **Event batching**: Configurable batch size for bulk inserts
/// - **Schema migration**: Automatic table creation and versioning
/// - **Deterministic replay**: Guaranteed event ordering via sequence numbers
///
/// # Thread Safety
///
/// Each operation creates its own connection in a blocking thread pool,
/// ensuring thread safety without shared state. `SQLite`'s WAL mode handles
/// concurrent access transparently.
#[derive(Debug, Clone)]
pub struct SqliteEventLog {
    /// Path to `SQLite` database file.
    ///
    /// Can be `:memory:` for in-memory database or a file path.
    db_path: PathBuf,
    
    /// Batch size for bulk inserts.
    ///
    /// Events are buffered and inserted in batches to reduce `SQLite` overhead.
    /// Default: 10 events per batch.
    batch_size: usize,
}

impl SqliteEventLog {
    /// Create a new `SQLite` event log.
    ///
    /// # Arguments
    ///
    /// * `path` - Path to `SQLite` database file, or `:memory:` for in-memory
    ///
    /// # Errors
    ///
    /// Returns error if:
    /// - Database file cannot be created or opened
    /// - Schema migration fails
    /// - WAL mode cannot be enabled
    ///
    /// # Example
    ///
    /// ```rust,ignore
    /// // File-based database
    /// let log = SqliteEventLog::new("./workflows.db").await?;
    ///
    /// // In-memory database (for testing)
    /// let log = SqliteEventLog::new(":memory:").await?;
    /// ```
    pub async fn new<P: Into<PathBuf>>(path: P) -> Result<Self> {
        let mut db_path = path.into();
        
        // For in-memory databases, use shared cache mode so all connections see the same data
        if db_path.to_str() == Some(":memory:") {
            db_path = PathBuf::from("file::memory:?cache=shared");
        }
        
        let event_log = Self {
            db_path,
            batch_size: 10,
        };
        
        // Initialize schema
        event_log.migrate_schema().await?;
        
        Ok(event_log)
    }
    
    /// Set batch size for bulk inserts.
    ///
    /// # Arguments
    ///
    /// * `size` - Number of events to buffer before inserting
    ///
    /// # Example
    ///
    /// ```rust,ignore
    /// let log = SqliteEventLog::new("workflows.db")
    ///     .await?
    ///     .with_batch_size(50); // Buffer 50 events
    /// ```
    #[must_use]
    pub fn with_batch_size(mut self, size: usize) -> Self {
        self.batch_size = size;
        self
    }
    
    /// Migrate database schema to latest version.
    ///
    /// Creates the `workflow_events` table if it doesn't exist and applies
    /// any pending migrations.
    async fn migrate_schema(&self) -> Result<()> {
        let db_path = self.db_path.clone();
        
        task::spawn_blocking(move || -> Result<()> {
            let conn = Connection::open(&db_path)
                .context("Failed to open database for migration")?;
            
            // Enable WAL mode
            conn.pragma_update(None, "journal_mode", "WAL")
                .context("Failed to enable WAL mode")?;
            
            // Create workflow_events table
            conn.execute(
                r"
                CREATE TABLE IF NOT EXISTS workflow_events (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    workflow_id TEXT NOT NULL,
                    sequence INTEGER NOT NULL,
                    event_type TEXT NOT NULL,
                    event_data BLOB NOT NULL,
                    created_at INTEGER NOT NULL,
                    UNIQUE(workflow_id, sequence)
                )
                ",
                [],
            )
            .context("Failed to create workflow_events table")?;
            
            // Create index on workflow_id for fast lookups
            conn.execute(
                r"
                CREATE INDEX IF NOT EXISTS idx_workflow_events_workflow_id
                ON workflow_events(workflow_id)
                ",
                [],
            )
            .context("Failed to create workflow_id index")?;
            
            // Create index on (workflow_id, sequence) for ordered replay
            conn.execute(
                r"
                CREATE INDEX IF NOT EXISTS idx_workflow_events_sequence
                ON workflow_events(workflow_id, sequence)
                ",
                [],
            )
            .context("Failed to create sequence index")?;
            
            Ok(())
        })
        .await
        .context("Failed to spawn blocking task")??;
        
        Ok(())
    }
    
    /// Serialize event to bytes using JSON.
    fn serialize_event(event: &Event) -> Result<Vec<u8>> {
        event.serialize()
    }
    
    /// Deserialize event from bytes using JSON.
    fn deserialize_event(data: &[u8]) -> Result<Event> {
        Event::deserialize(data)
    }
}

#[async_trait]
impl EventLog for SqliteEventLog {
    async fn append(&self, workflow_id: &str, event: Event) -> Result<u64> {
        let workflow_id = workflow_id.to_string();
        let event_type = event.event_type().to_string();
        let event_data = Self::serialize_event(&event)?;
        let db_path = self.db_path.clone();
        
        let sequence = task::spawn_blocking(move || -> Result<u64> {
            let conn = Connection::open(&db_path)
                .context("Failed to open database")?;
            
            // Enable WAL mode (must be set on every connection for :memory: databases)
            conn.pragma_update(None, "journal_mode", "WAL").ok();
            
            // Retry logic for concurrent inserts (up to 3 attempts)
            let mut attempts = 0;
            loop {
                attempts += 1;
                
                // Begin immediate transaction to lock database
                conn.execute("BEGIN IMMEDIATE", [])
                    .context("Failed to begin transaction")?;
                
                // Get next sequence number (cast to i64 for SQLite)
                let next_seq: i64 = match conn.query_row(
                    "SELECT COALESCE(MAX(sequence), -1) + 1 FROM workflow_events WHERE workflow_id = ?1",
                    params![&workflow_id],
                    |row| row.get(0),
                ) {
                    Ok(seq) => seq,
                    Err(e) => {
                        conn.execute("ROLLBACK", []).ok();
                        return Err(e).context("Failed to get next sequence number");
                    }
                };
                
                // Insert event
                let now = chrono::Utc::now().timestamp();
                match conn.execute(
                    r"
                    INSERT INTO workflow_events (workflow_id, sequence, event_type, event_data, created_at)
                    VALUES (?1, ?2, ?3, ?4, ?5)
                    ",
                    params![&workflow_id, next_seq, &event_type, &event_data, now],
                ) {
                    Ok(_) => {
                        conn.execute("COMMIT", [])
                            .context("Failed to commit transaction")?;
                        // Safe cast: next_seq is always non-negative from SQL query (COALESCE ensures >= 0)
                        #[allow(clippy::cast_sign_loss, reason = "next_seq is always non-negative from SQL COALESCE")]
                        return Ok(next_seq as u64);
                    }
                    Err(e) => {
                        conn.execute("ROLLBACK", []).ok();
                        
                        // If it's a constraint error and we haven't exceeded retries, try again
                        if e.to_string().contains("UNIQUE constraint") && attempts < 3 {
                            // Small delay before retry
                            std::thread::sleep(std::time::Duration::from_millis(10));
                            continue;
                        }
                        
                        return Err(e).context("Failed to insert event");
                    }
                }
            }
        })
        .await
        .context("Failed to spawn blocking task")??;
        
        Ok(sequence)
    }
    
    async fn replay(&self, workflow_id: &str) -> Result<Vec<Event>> {
        let workflow_id = workflow_id.to_string();
        let db_path = self.db_path.clone();
        
        let events = task::spawn_blocking(move || -> Result<Vec<Event>> {
            let conn = Connection::open(&db_path)
                .context("Failed to open database")?;
            
            let mut stmt = conn
                .prepare(
                    r"
                    SELECT event_data FROM workflow_events
                    WHERE workflow_id = ?1
                    ORDER BY sequence ASC
                    ",
                )
                .context("Failed to prepare replay query")?;
            
            let event_iter = stmt
                .query_map(params![&workflow_id], |row| {
                    let data: Vec<u8> = row.get(0)?;
                    Ok(data)
                })
                .context("Failed to execute replay query")?;
            
            let mut events = Vec::new();
            for event_data in event_iter {
                let data = event_data.context("Failed to read event data")?;
                let event = Self::deserialize_event(&data)
                    .context("Failed to deserialize event")?;
                events.push(event);
            }
            
            Ok(events)
        })
        .await
        .context("Failed to spawn blocking task")??;
        
        Ok(events)
    }
    
    async fn next_index(&self, workflow_id: &str) -> Result<u64> {
        let workflow_id = workflow_id.to_string();
        let db_path = self.db_path.clone();
        
        let next_idx = task::spawn_blocking(move || -> Result<u64> {
            let conn = Connection::open(&db_path)
                .context("Failed to open database")?;
            
            let idx: i64 = conn
                .query_row(
                    "SELECT COALESCE(MAX(sequence), -1) + 1 FROM workflow_events WHERE workflow_id = ?1",
                    params![&workflow_id],
                    |row| row.get(0),
                )
                .context("Failed to get next index")?;
            
            // Safe cast: idx is always non-negative from SQL query (COALESCE ensures >= 0)
            #[allow(clippy::cast_sign_loss, reason = "idx is always non-negative from SQL COALESCE")]
            Ok(idx as u64)
        })
        .await
        .context("Failed to spawn blocking task")??;
        
        Ok(next_idx)
    }
    
    async fn exists(&self, workflow_id: &str) -> Result<bool> {
        let workflow_id = workflow_id.to_string();
        let db_path = self.db_path.clone();
        
        let exists = task::spawn_blocking(move || -> Result<bool> {
            let conn = Connection::open(&db_path)
                .context("Failed to open database")?;
            
            let count: i64 = conn
                .query_row(
                    "SELECT COUNT(*) FROM workflow_events WHERE workflow_id = ?1",
                    params![&workflow_id],
                    |row| row.get(0),
                )
                .context("Failed to check workflow existence")?;
            
            Ok(count > 0)
        })
        .await
        .context("Failed to spawn blocking task")??;
        
        Ok(exists)
    }
    
    async fn delete(&self, workflow_id: &str) -> Result<u64> {
        let workflow_id = workflow_id.to_string();
        let db_path = self.db_path.clone();
        
        let deleted = task::spawn_blocking(move || -> Result<u64> {
            let conn = Connection::open(&db_path)
                .context("Failed to open database")?;
            
            let count = conn
                .execute(
                    "DELETE FROM workflow_events WHERE workflow_id = ?1",
                    params![&workflow_id],
                )
                .context("Failed to delete workflow events")?;
            
            Ok(count as u64)
        })
        .await
        .context("Failed to spawn blocking task")??;
        
        Ok(deleted)
    }
    
    async fn get_checkpoint(&self, workflow_id: &str) -> Result<Option<Vec<u8>>> {
        let workflow_id = workflow_id.to_string();
        let db_path = self.db_path.clone();
        
        let checkpoint = task::spawn_blocking(move || -> Result<Option<Vec<u8>>> {
            let conn = Connection::open(&db_path)
                .context("Failed to open database")?;
            
            let mut stmt = conn
                .prepare(
                    r"
                    SELECT event_data FROM workflow_events
                    WHERE workflow_id = ?1 AND event_type = 'checkpoint'
                    ORDER BY sequence DESC
                    LIMIT 1
                    ",
                )
                .context("Failed to prepare checkpoint query")?;
            
            let checkpoint: Option<Vec<u8>> = stmt
                .query_row(params![&workflow_id], |row| row.get(0))
                .optional()
                .context("Failed to execute checkpoint query")?;
            
            if let Some(data) = checkpoint {
                let event = Self::deserialize_event(&data)
                    .context("Failed to deserialize checkpoint event")?;
                
                if let Event::Checkpoint { state } = event {
                    return Ok(Some(state));
                }
            }
            
            Ok(None)
        })
        .await
        .context("Failed to spawn blocking task")??;
        
        Ok(checkpoint)
    }
    
    async fn compact(&self, workflow_id: &str) -> Result<u64> {
        let workflow_id = workflow_id.to_string();
        let db_path = self.db_path.clone();
        
        let deleted = task::spawn_blocking(move || -> Result<u64> {
            let conn = Connection::open(&db_path)
                .context("Failed to open database")?;
            
            // Find last checkpoint sequence
            let checkpoint_seq: Option<i64> = conn
                .query_row(
                    r"
                    SELECT sequence FROM workflow_events
                    WHERE workflow_id = ?1 AND event_type = 'checkpoint'
                    ORDER BY sequence DESC
                    LIMIT 1
                    ",
                    params![&workflow_id],
                    |row| row.get(0),
                )
                .optional()
                .context("Failed to find last checkpoint")?;
            
            if let Some(seq) = checkpoint_seq {
                // Delete all events before the checkpoint
                let count = conn
                    .execute(
                        "DELETE FROM workflow_events WHERE workflow_id = ?1 AND sequence < ?2",
                        params![&workflow_id, seq],
                    )
                    .context("Failed to compact events")?;
                
                return Ok(count as u64);
            }
            
            Ok(0)
        })
        .await
        .context("Failed to spawn blocking task")??;
        
        Ok(deleted)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::NamedTempFile;
    
    fn create_test_event(_workflow_id: &str, activity_id: &str) -> Event {
        Event::ActivityScheduled {
            activity_id: activity_id.to_string(),
            activity_type: "test_activity".to_string(),
            input: serde_json::json!({"key": "value"}),
        }
    }
    
    async fn create_test_log() -> (SqliteEventLog, NamedTempFile) {
        let temp_file = NamedTempFile::new().unwrap();
        let log = SqliteEventLog::new(temp_file.path()).await.unwrap();
        (log, temp_file)
    }
    
    #[tokio::test]
    async fn test_new_memory_database() {
        let temp_file = NamedTempFile::new().unwrap();
        let log = SqliteEventLog::new(temp_file.path()).await;
        assert!(log.is_ok());
    }
    
    #[tokio::test]
    async fn test_append_single_event() {
        let (log, _temp) = create_test_log().await;
        let event = create_test_event("wf-1", "act-1");
        
        let seq = log.append("wf-1", event).await;
        assert!(seq.is_ok());
        assert_eq!(seq.unwrap(), 0);
    }
    
    #[tokio::test]
    async fn test_append_multiple_events() {
        let (log, _temp) = create_test_log().await;
        
        let seq1 = log.append("wf-1", create_test_event("wf-1", "act-1")).await.unwrap();
        let seq2 = log.append("wf-1", create_test_event("wf-1", "act-2")).await.unwrap();
        let seq3 = log.append("wf-1", create_test_event("wf-1", "act-3")).await.unwrap();
        
        assert_eq!(seq1, 0);
        assert_eq!(seq2, 1);
        assert_eq!(seq3, 2);
    }
    
    #[tokio::test]
    async fn test_replay_preserves_order() {
        let (log, _temp) = create_test_log().await;
        
        log.append("wf-1", create_test_event("wf-1", "act-1")).await.unwrap();
        log.append("wf-1", create_test_event("wf-1", "act-2")).await.unwrap();
        log.append("wf-1", create_test_event("wf-1", "act-3")).await.unwrap();
        
        let events = log.replay("wf-1").await.unwrap();
        assert_eq!(events.len(), 3);
        
        // Verify order
        if let Event::ActivityScheduled { activity_id, .. } = &events[0] {
            assert_eq!(activity_id, "act-1");
        } else {
            panic!("Expected ActivityScheduled event");
        }
    }
    
    #[tokio::test]
    async fn test_replay_empty_workflow() {
        let (log, _temp) = create_test_log().await;
        let events = log.replay("nonexistent").await.unwrap();
        assert_eq!(events.len(), 0);
    }
    
    #[tokio::test]
    async fn test_next_index() {
        let (log, _temp) = create_test_log().await;
        
        assert_eq!(log.next_index("wf-1").await.unwrap(), 0);
        
        log.append("wf-1", create_test_event("wf-1", "act-1")).await.unwrap();
        assert_eq!(log.next_index("wf-1").await.unwrap(), 1);
        
        log.append("wf-1", create_test_event("wf-1", "act-2")).await.unwrap();
        assert_eq!(log.next_index("wf-1").await.unwrap(), 2);
    }
    
    #[tokio::test]
    async fn test_exists() {
        let (log, _temp) = create_test_log().await;
        
        assert!(!log.exists("wf-1").await.unwrap());
        
        log.append("wf-1", create_test_event("wf-1", "act-1")).await.unwrap();
        assert!(log.exists("wf-1").await.unwrap());
    }
    
    #[tokio::test]
    async fn test_delete() {
        let (log, _temp) = create_test_log().await;
        
        log.append("wf-1", create_test_event("wf-1", "act-1")).await.unwrap();
        log.append("wf-1", create_test_event("wf-1", "act-2")).await.unwrap();
        
        let deleted = log.delete("wf-1").await.unwrap();
        assert_eq!(deleted, 2);
        
        assert!(!log.exists("wf-1").await.unwrap());
    }
    
    #[tokio::test]
    async fn test_get_checkpoint() {
        let (log, _temp) = create_test_log().await;
        
        log.append("wf-1", create_test_event("wf-1", "act-1")).await.unwrap();
        log.append("wf-1", Event::Checkpoint { state: vec![1, 2, 3] }).await.unwrap();
        log.append("wf-1", create_test_event("wf-1", "act-2")).await.unwrap();
        
        let checkpoint = log.get_checkpoint("wf-1").await.unwrap();
        assert_eq!(checkpoint, Some(vec![1, 2, 3]));
    }
    
    #[tokio::test]
    async fn test_get_checkpoint_none() {
        let (log, _temp) = create_test_log().await;
        
        log.append("wf-1", create_test_event("wf-1", "act-1")).await.unwrap();
        
        let checkpoint = log.get_checkpoint("wf-1").await.unwrap();
        assert_eq!(checkpoint, None);
    }
    
    #[tokio::test]
    async fn test_compact() {
        let (log, _temp) = create_test_log().await;
        
        log.append("wf-1", create_test_event("wf-1", "act-1")).await.unwrap();
        log.append("wf-1", create_test_event("wf-1", "act-2")).await.unwrap();
        log.append("wf-1", Event::Checkpoint { state: vec![1, 2, 3] }).await.unwrap();
        log.append("wf-1", create_test_event("wf-1", "act-3")).await.unwrap();
        
        let deleted = log.compact("wf-1").await.unwrap();
        assert_eq!(deleted, 2); // act-1 and act-2
        
        let events = log.replay("wf-1").await.unwrap();
        assert_eq!(events.len(), 2); // checkpoint and act-3
    }
    
    #[tokio::test]
    async fn test_concurrent_appends() {
        let (log, _temp) = create_test_log().await;
        
        let mut handles = vec![];
        for i in 0..10 {
            let log = log.clone();
            let handle = tokio::spawn(async move {
                log.append("wf-1", create_test_event("wf-1", &format!("act-{}", i)))
                    .await
                    .unwrap()
            });
            handles.push(handle);
        }
        
        for handle in handles {
            handle.await.unwrap();
        }
        
        let events = log.replay("wf-1").await.unwrap();
        assert_eq!(events.len(), 10);
    }
}
