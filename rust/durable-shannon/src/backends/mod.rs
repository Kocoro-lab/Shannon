//! Event log backends for durable workflow persistence.
//!
//! Backends implement the `EventLog` trait to provide durable storage
//! for workflow events, enabling replay and recovery.



use async_trait::async_trait;

use crate::Event;

/// Event log trait for durable workflow persistence.
///
/// Implementations provide storage for workflow events with support for:
/// - Appending new events
/// - Replaying events for recovery
/// - Querying workflow state
#[async_trait]
pub trait EventLog: Send + Sync {
    /// Append an event to the log.
    ///
    /// Returns the event index within the workflow.
    async fn append(&self, workflow_id: &str, event: Event) -> anyhow::Result<u64>;

    /// Replay all events for a workflow.
    ///
    /// Returns events in order of occurrence.
    async fn replay(&self, workflow_id: &str) -> anyhow::Result<Vec<Event>>;

    /// Get the next event index for a workflow.
    async fn next_index(&self, workflow_id: &str) -> anyhow::Result<u64>;

    /// Check if a workflow exists.
    async fn exists(&self, workflow_id: &str) -> anyhow::Result<bool>;

    /// Delete all events for a workflow.
    async fn delete(&self, workflow_id: &str) -> anyhow::Result<u64>;

    /// Get the current state of a workflow (last checkpoint).
    async fn get_checkpoint(&self, workflow_id: &str) -> anyhow::Result<Option<Vec<u8>>>;

    /// Compact old events (keep only latest checkpoint and subsequent events).
    async fn compact(&self, workflow_id: &str) -> anyhow::Result<u64>;
}

/// In-memory event log for testing.
#[derive(Debug, Default)]
pub struct InMemoryEventLog {
    events: parking_lot::RwLock<std::collections::HashMap<String, Vec<Event>>>,
}

impl InMemoryEventLog {
    /// Create a new in-memory event log.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl EventLog for InMemoryEventLog {
    async fn append(&self, workflow_id: &str, event: Event) -> anyhow::Result<u64> {
        let mut events = self.events.write();
        let workflow_events = events.entry(workflow_id.to_string()).or_default();
        let idx = workflow_events.len() as u64;
        workflow_events.push(event);
        Ok(idx)
    }

    async fn replay(&self, workflow_id: &str) -> anyhow::Result<Vec<Event>> {
        let events = self.events.read();
        Ok(events.get(workflow_id).cloned().unwrap_or_default())
    }

    async fn next_index(&self, workflow_id: &str) -> anyhow::Result<u64> {
        let events = self.events.read();
        Ok(events.get(workflow_id).map_or(0, |e| e.len() as u64))
    }

    async fn exists(&self, workflow_id: &str) -> anyhow::Result<bool> {
        let events = self.events.read();
        Ok(events.contains_key(workflow_id))
    }

    async fn delete(&self, workflow_id: &str) -> anyhow::Result<u64> {
        let mut events = self.events.write();
        Ok(events.remove(workflow_id).map_or(0, |e| e.len() as u64))
    }

    async fn get_checkpoint(&self, workflow_id: &str) -> anyhow::Result<Option<Vec<u8>>> {
        let events = self.events.read();
        if let Some(workflow_events) = events.get(workflow_id) {
            for event in workflow_events.iter().rev() {
                if let Event::Checkpoint { state } = event {
                    return Ok(Some(state.clone()));
                }
            }
        }
        Ok(None)
    }

    async fn compact(&self, _workflow_id: &str) -> anyhow::Result<u64> {
        // No-op for in-memory
        Ok(0)
    }
}

#[async_trait]
impl<T: EventLog + ?Sized> EventLog for Box<T> {
    async fn append(&self, workflow_id: &str, event: Event) -> anyhow::Result<u64> {
        (**self).append(workflow_id, event).await
    }

    async fn replay(&self, workflow_id: &str) -> anyhow::Result<Vec<Event>> {
        (**self).replay(workflow_id).await
    }

    async fn next_index(&self, workflow_id: &str) -> anyhow::Result<u64> {
        (**self).next_index(workflow_id).await
    }

    async fn exists(&self, workflow_id: &str) -> anyhow::Result<bool> {
        (**self).exists(workflow_id).await
    }

    async fn delete(&self, workflow_id: &str) -> anyhow::Result<u64> {
        (**self).delete(workflow_id).await
    }

    async fn get_checkpoint(&self, workflow_id: &str) -> anyhow::Result<Option<Vec<u8>>> {
        (**self).get_checkpoint(workflow_id).await
    }

    async fn compact(&self, workflow_id: &str) -> anyhow::Result<u64> {
        (**self).compact(workflow_id).await
    }
}
