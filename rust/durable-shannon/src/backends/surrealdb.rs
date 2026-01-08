//! SurrealDB event log backend.
//!
//! Provides durable workflow event storage using SurrealDB with RocksDB backend.

use std::path::Path;
use std::sync::Arc;

use async_trait::async_trait;
use surrealdb::engine::local::{Db, RocksDb};
use surrealdb::Surreal;

use super::EventLog;
use crate::Event;

/// SurrealDB-backed event log for embedded workflows.
#[derive(Clone)]
pub struct SurrealDBEventLog {
    db: Arc<Surreal<Db>>,
}

impl std::fmt::Debug for SurrealDBEventLog {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("SurrealDBEventLog").finish()
    }
}

/// Workflow event record in SurrealDB.
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
struct WorkflowEventRecord {
    workflow_id: String,
    event_idx: u64,
    event_type: String,
    data: Vec<u8>,
    created_at: chrono::DateTime<chrono::Utc>,
}

impl SurrealDBEventLog {
    /// Create a new `SurrealDB` event log at the given path.
    ///
    /// # Errors
    ///
    /// Returns an error if the database cannot be opened or schema cannot be created.
    pub async fn new(path: &Path) -> anyhow::Result<Self> {
        tracing::info!("Opening SurrealDB event log at {:?}", path);

        // Ensure parent directory exists
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)?;
        }

        let db = Surreal::new::<RocksDb>(path).await?;
        db.use_ns("shannon").use_db("workflows").await?;

        // Create schema
        db.query(
            r"
            DEFINE TABLE workflow_events SCHEMAFULL;
            DEFINE FIELD workflow_id ON workflow_events TYPE string;
            DEFINE FIELD event_idx ON workflow_events TYPE int;
            DEFINE FIELD event_type ON workflow_events TYPE string;
            DEFINE FIELD data ON workflow_events TYPE bytes;
            DEFINE FIELD created_at ON workflow_events TYPE datetime DEFAULT time::now();
            DEFINE INDEX idx_workflow ON workflow_events COLUMNS workflow_id, event_idx UNIQUE;
        ",
        )
        .await?;

        Ok(Self { db: Arc::new(db) })
    }

    /// Create from an existing SurrealDB connection.
    #[must_use]
    pub fn from_db(db: Arc<Surreal<Db>>) -> Self {
        Self { db }
    }
}

#[async_trait]
impl EventLog for SurrealDBEventLog {
    async fn append(&self, workflow_id: &str, event: Event) -> anyhow::Result<u64> {
        let idx = self.next_index(workflow_id).await?;

        let record = WorkflowEventRecord {
            workflow_id: workflow_id.to_string(),
            event_idx: idx,
            event_type: event.event_type().to_string(),
            data: event.serialize()?,
            created_at: chrono::Utc::now(),
        };

        let id = format!("{workflow_id}_{idx}");
        let _: Option<WorkflowEventRecord> = self
            .db
            .create(("workflow_events", id))
            .content(record)
            .await?;

        Ok(idx)
    }

    async fn replay(&self, workflow_id: &str) -> anyhow::Result<Vec<Event>> {
        let wid = workflow_id.to_string();
        let records: Vec<WorkflowEventRecord> = self
            .db
            .query("SELECT * FROM workflow_events WHERE workflow_id = $wid ORDER BY event_idx")
            .bind(("wid", wid))
            .await?
            .take(0)?;

        let mut events = Vec::with_capacity(records.len());
        for record in records {
            events.push(Event::deserialize(&record.data)?);
        }

        Ok(events)
    }

    async fn next_index(&self, workflow_id: &str) -> anyhow::Result<u64> {
        let wid = workflow_id.to_string();
        let result: Option<u64> = self
            .db
            .query("SELECT math::max(event_idx) FROM workflow_events WHERE workflow_id = $wid")
            .bind(("wid", wid))
            .await?
            .take(0)?;

        Ok(result.map_or(0, |max| max + 1))
    }

    async fn exists(&self, workflow_id: &str) -> anyhow::Result<bool> {
        let wid = workflow_id.to_string();
        let count: Option<u64> = self
            .db
            .query("SELECT count() FROM workflow_events WHERE workflow_id = $wid GROUP ALL")
            .bind(("wid", wid))
            .await?
            .take(0)?;

        Ok(count.unwrap_or(0) > 0)
    }

    async fn delete(&self, workflow_id: &str) -> anyhow::Result<u64> {
        let wid = workflow_id.to_string();
        let result: Vec<WorkflowEventRecord> = self
            .db
            .query("DELETE workflow_events WHERE workflow_id = $wid RETURN BEFORE")
            .bind(("wid", wid))
            .await?
            .take(0)?;

        Ok(result.len() as u64)
    }

    async fn get_checkpoint(&self, workflow_id: &str) -> anyhow::Result<Option<Vec<u8>>> {
        let wid = workflow_id.to_string();
        let records: Vec<WorkflowEventRecord> = self
            .db
            .query("SELECT * FROM workflow_events WHERE workflow_id = $wid AND event_type = 'checkpoint' ORDER BY event_idx DESC LIMIT 1")
            .bind(("wid", wid))
            .await?
            .take(0)?;

        if let Some(record) = records.into_iter().next() {
            let event = Event::deserialize(&record.data)?;
            if let Event::Checkpoint { state } = event {
                return Ok(Some(state));
            }
        }

        Ok(None)
    }

    async fn compact(&self, workflow_id: &str) -> anyhow::Result<u64> {
        let wid = workflow_id.to_string();
        // Find the latest checkpoint
        let checkpoint_idx: Option<u64> = self
            .db
            .query("SELECT math::max(event_idx) FROM workflow_events WHERE workflow_id = $wid AND event_type = 'checkpoint'")
            .bind(("wid", wid.clone()))
            .await?
            .take(0)?;

        if let Some(idx) = checkpoint_idx {
            if idx > 0 {
                // Delete all events before the checkpoint
                let result: Vec<WorkflowEventRecord> = self
                    .db
                    .query("DELETE workflow_events WHERE workflow_id = $wid AND event_idx < $idx RETURN BEFORE")
                    .bind(("wid", wid))
                    .bind(("idx", idx))
                    .await?
                    .take(0)?;

                return Ok(result.len() as u64);
            }
        }

        Ok(0)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_event_serialization() {
        let event = Event::WorkflowStarted {
            workflow_id: "test-1".to_string(),
            workflow_type: "chain_of_thought".to_string(),
            input: serde_json::json!({"query": "test"}),
            timestamp: chrono::Utc::now(),
        };

        let data = event.serialize().unwrap();
        let restored = Event::deserialize(&data).unwrap();

        assert_eq!(event.event_type(), restored.event_type());
    }
}
