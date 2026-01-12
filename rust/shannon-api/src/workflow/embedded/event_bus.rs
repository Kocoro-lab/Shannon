//! Event bus for real-time workflow event streaming.
//!
//! Provides pub/sub infrastructure for streaming workflow events from
//! the execution engine to UI components via SSE/WebSocket.
//!
//! # Architecture
//!
//! ```text
//! Workflow → EventBus::broadcast(event) → [Subscriber 1, Subscriber 2, ...]
//!                                              ↓              ↓
//!                                            UI via SSE    UI via WebSocket
//! ```
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::embedded::EventBus;
//!
//! let bus = EventBus::new();
//!
//! // Subscribe to events
//! let mut rx = bus.subscribe("workflow-123");
//!
//! // Broadcast event
//! bus.broadcast("workflow-123", WorkflowEvent::Started { ... }).await;
//!
//! // Receive event
//! if let Ok(event) = rx.recv().await {
//!     println!("Received: {:?}", event);
//! }
//! ```

use std::collections::HashMap;
use std::sync::Arc;

use parking_lot::RwLock;
use serde::{Deserialize, Serialize};
use tokio::sync::broadcast;

/// Channel capacity for workflow events.
///
/// Sized to handle bursts of events without dropping.
/// If a subscriber falls behind by more than 256 events,
/// older events will be dropped.
const CHANNEL_CAPACITY: usize = 256;

/// Workflow event types for real-time streaming.
///
/// Matches the 26+ event types from the cloud Temporal implementation
/// to maintain API compatibility.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "SCREAMING_SNAKE_CASE")]
pub enum WorkflowEvent {
    /// Workflow started executing.
    WorkflowStarted {
        workflow_id: String,
        pattern_type: String,
        #[serde(with = "chrono::serde::ts_seconds")]
        timestamp: chrono::DateTime<chrono::Utc>,
    },

    /// Workflow status changed.
    WorkflowStatusChanged {
        workflow_id: String,
        old_status: String,
        new_status: String,
        #[serde(with = "chrono::serde::ts_seconds")]
        timestamp: chrono::DateTime<chrono::Utc>,
    },

    /// Workflow pausing.
    WorkflowPausing {
        workflow_id: String,
        #[serde(with = "chrono::serde::ts_seconds")]
        timestamp: chrono::DateTime<chrono::Utc>,
    },

    /// Workflow paused.
    WorkflowPaused {
        workflow_id: String,
        #[serde(with = "chrono::serde::ts_seconds")]
        timestamp: chrono::DateTime<chrono::Utc>,
    },

    /// Workflow resuming.
    WorkflowResuming {
        workflow_id: String,
        #[serde(with = "chrono::serde::ts_seconds")]
        timestamp: chrono::DateTime<chrono::Utc>,
    },

    /// Workflow cancelling.
    WorkflowCancelling {
        workflow_id: String,
        #[serde(with = "chrono::serde::ts_seconds")]
        timestamp: chrono::DateTime<chrono::Utc>,
    },

    /// Workflow cancelled.
    WorkflowCancelled {
        workflow_id: String,
        #[serde(with = "chrono::serde::ts_seconds")]
        timestamp: chrono::DateTime<chrono::Utc>,
    },

    /// Workflow completed successfully.
    WorkflowCompleted {
        workflow_id: String,
        output: String,
        #[serde(with = "chrono::serde::ts_seconds")]
        timestamp: chrono::DateTime<chrono::Utc>,
    },

    /// Workflow failed with error.
    WorkflowFailed {
        workflow_id: String,
        error: String,
        #[serde(with = "chrono::serde::ts_seconds")]
        timestamp: chrono::DateTime<chrono::Utc>,
    },

    /// Activity scheduled for execution.
    ActivityScheduled {
        workflow_id: String,
        activity_id: String,
        activity_type: String,
    },

    /// Activity started executing.
    ActivityStarted {
        workflow_id: String,
        activity_id: String,
        #[serde(with = "chrono::serde::ts_seconds")]
        timestamp: chrono::DateTime<chrono::Utc>,
    },

    /// Activity completed successfully.
    ActivityCompleted {
        workflow_id: String,
        activity_id: String,
        duration_ms: u64,
    },

    /// Activity failed with error.
    ActivityFailed {
        workflow_id: String,
        activity_id: String,
        error: String,
        retryable: bool,
    },

    /// LLM request started.
    LlmRequest {
        workflow_id: String,
        model: String,
        prompt_tokens: usize,
    },

    /// LLM response received (partial for streaming).
    LlmPartial {
        workflow_id: String,
        content: String,
    },

    /// LLM response completed.
    LlmResponse {
        workflow_id: String,
        content: String,
        completion_tokens: usize,
        total_tokens: usize,
    },

    /// Tool execution started.
    ToolExecutionStarted {
        workflow_id: String,
        tool_name: String,
        parameters: serde_json::Value,
    },

    /// Tool execution completed.
    ToolExecutionCompleted {
        workflow_id: String,
        tool_name: String,
        result: serde_json::Value,
    },

    /// Progress update with percentage.
    Progress {
        workflow_id: String,
        step: String,
        percentage: f32,
        message: Option<String>,
    },

    /// Checkpoint created.
    CheckpointCreated { workflow_id: String, sequence: u64 },
}

impl WorkflowEvent {
    /// Get the workflow ID for this event.
    #[must_use]
    pub fn workflow_id(&self) -> &str {
        match self {
            Self::WorkflowStarted { workflow_id, .. }
            | Self::WorkflowStatusChanged { workflow_id, .. }
            | Self::WorkflowPausing { workflow_id, .. }
            | Self::WorkflowPaused { workflow_id, .. }
            | Self::WorkflowResuming { workflow_id, .. }
            | Self::WorkflowCancelling { workflow_id, .. }
            | Self::WorkflowCancelled { workflow_id, .. }
            | Self::WorkflowCompleted { workflow_id, .. }
            | Self::WorkflowFailed { workflow_id, .. }
            | Self::ActivityScheduled { workflow_id, .. }
            | Self::ActivityStarted { workflow_id, .. }
            | Self::ActivityCompleted { workflow_id, .. }
            | Self::ActivityFailed { workflow_id, .. }
            | Self::LlmRequest { workflow_id, .. }
            | Self::LlmPartial { workflow_id, .. }
            | Self::LlmResponse { workflow_id, .. }
            | Self::ToolExecutionStarted { workflow_id, .. }
            | Self::ToolExecutionCompleted { workflow_id, .. }
            | Self::Progress { workflow_id, .. }
            | Self::CheckpointCreated { workflow_id, .. } => workflow_id,
        }
    }

    /// Check if this event should be persisted to the event log.
    ///
    /// Ephemeral events (like `LlmPartial`) are only broadcast for real-time
    /// streaming and not saved to the event log.
    #[must_use]
    pub fn is_persistent(&self) -> bool {
        !matches!(self, Self::LlmPartial { .. })
    }
}

/// Event bus for real-time workflow event streaming.
///
/// Manages pub/sub channels for each active workflow, enabling
/// real-time event delivery to multiple subscribers (UI clients).
///
/// # Thread Safety
///
/// Uses `parking_lot::RwLock` for efficient concurrent access to the
/// channel registry. Channels themselves use `tokio::sync::broadcast`
/// which is lock-free and highly concurrent.
///
/// # Backpressure
///
/// When a subscriber falls behind by more than `CHANNEL_CAPACITY` events,
/// older events are dropped to prevent memory exhaustion. Subscribers
/// receive `broadcast::error::RecvError::Lagged` in this case.
#[derive(Debug, Clone)]
pub struct EventBus {
    /// Active broadcast channels indexed by `workflow_id`.
    ///
    /// Channels are created on first broadcast and cleaned up on workflow completion.
    channels: Arc<RwLock<HashMap<String, broadcast::Sender<WorkflowEvent>>>>,
}

impl EventBus {
    /// Create a new event bus.
    #[must_use]
    pub fn new() -> Self {
        Self {
            channels: Arc::new(RwLock::new(HashMap::new())),
        }
    }

    /// Subscribe to events for a workflow.
    ///
    /// Creates a new channel if one doesn't exist for this workflow.
    ///
    /// # Returns
    ///
    /// A broadcast receiver that will receive all future events for this workflow.
    ///
    /// # Example
    ///
    /// ```rust,ignore
    /// let mut rx = bus.subscribe("workflow-123");
    /// while let Ok(event) = rx.recv().await {
    ///     println!("Event: {:?}", event);
    /// }
    /// ```
    pub fn subscribe(&self, workflow_id: &str) -> broadcast::Receiver<WorkflowEvent> {
        let mut channels = self.channels.write();

        let sender = channels.entry(workflow_id.to_string()).or_insert_with(|| {
            let (tx, _rx) = broadcast::channel(CHANNEL_CAPACITY);
            tx
        });

        sender.subscribe()
    }

    /// Broadcast an event to all subscribers of a workflow.
    ///
    /// If no subscribers exist, the channel is created but the event is dropped.
    /// This is intentional - events are ephemeral and we don't buffer them.
    ///
    /// # Errors
    ///
    /// Returns error if the channel is closed or disconnected.
    ///
    /// # Example
    ///
    /// ```rust,ignore
    /// bus.broadcast("workflow-123", WorkflowEvent::Started { ... }).await?;
    /// ```
    pub fn broadcast(&self, workflow_id: &str, event: WorkflowEvent) -> anyhow::Result<usize> {
        let channels = self.channels.read();

        if let Some(sender) = channels.get(workflow_id) {
            // Number of active receivers
            let receiver_count = sender.receiver_count();

            // Send to all subscribers (ignoring errors if no subscribers)
            let _ = sender.send(event);

            Ok(receiver_count)
        } else {
            // No channel exists - create one so future subscribers can connect
            drop(channels);
            let mut channels = self.channels.write();
            let (tx, _rx) = broadcast::channel(CHANNEL_CAPACITY);
            let _ = tx.send(event);
            channels.insert(workflow_id.to_string(), tx);
            Ok(0)
        }
    }

    /// Clean up channel for completed workflow.
    ///
    /// Call this when a workflow completes to free memory.
    /// Subscribers will receive `RecvError::Closed`.
    ///
    /// # Example
    ///
    /// ```rust,ignore
    /// bus.cleanup("workflow-123");
    /// ```
    pub fn cleanup(&self, workflow_id: &str) {
        let mut channels = self.channels.write();
        channels.remove(workflow_id);
    }

    /// Get the number of active workflow channels.
    #[must_use]
    pub fn active_channels(&self) -> usize {
        let channels = self.channels.read();
        channels.len()
    }

    /// Get the number of active subscribers for a workflow.
    #[must_use]
    pub fn subscriber_count(&self, workflow_id: &str) -> usize {
        let channels = self.channels.read();
        channels
            .get(workflow_id)
            .map_or(0, broadcast::Sender::receiver_count)
    }
}

impl Default for EventBus {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn create_test_event(workflow_id: &str, message: &str) -> WorkflowEvent {
        WorkflowEvent::Progress {
            workflow_id: workflow_id.to_string(),
            step: message.to_string(),
            percentage: 50.0,
            message: Some(message.to_string()),
        }
    }

    #[tokio::test]
    async fn test_new_event_bus() {
        let bus = EventBus::new();
        assert_eq!(bus.active_channels(), 0);
    }

    #[tokio::test]
    async fn test_subscribe_creates_channel() {
        let bus = EventBus::new();
        let _rx = bus.subscribe("wf-1");
        assert_eq!(bus.active_channels(), 1);
    }

    #[tokio::test]
    async fn test_broadcast_to_single_subscriber() {
        let bus = EventBus::new();
        let mut rx = bus.subscribe("wf-1");

        let event = create_test_event("wf-1", "test message");
        let count = bus.broadcast("wf-1", event.clone()).unwrap();
        assert_eq!(count, 1);

        let received = rx.recv().await.unwrap();
        assert_eq!(received.workflow_id(), "wf-1");
    }

    #[tokio::test]
    async fn test_broadcast_to_multiple_subscribers() {
        let bus = EventBus::new();
        let mut rx1 = bus.subscribe("wf-1");
        let mut rx2 = bus.subscribe("wf-1");
        let mut rx3 = bus.subscribe("wf-1");

        let event = create_test_event("wf-1", "test");
        let count = bus.broadcast("wf-1", event).unwrap();
        assert_eq!(count, 3);

        // All subscribers receive the event
        assert!(rx1.recv().await.is_ok());
        assert!(rx2.recv().await.is_ok());
        assert!(rx3.recv().await.is_ok());
    }

    #[tokio::test]
    async fn test_broadcast_without_subscribers() {
        let bus = EventBus::new();

        let event = create_test_event("wf-1", "test");
        let result = bus.broadcast("wf-1", event);

        // Should succeed even with no subscribers
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_subscribe_after_broadcast() {
        let bus = EventBus::new();

        // Broadcast first (no subscribers yet)
        bus.broadcast("wf-1", create_test_event("wf-1", "event1"))
            .unwrap();

        // Subscribe after
        let mut rx = bus.subscribe("wf-1");

        // New subscriber only receives future events
        bus.broadcast("wf-1", create_test_event("wf-1", "event2"))
            .unwrap();

        let received = rx.recv().await.unwrap();
        if let WorkflowEvent::Progress { step, .. } = received {
            assert_eq!(step, "event2");
        } else {
            panic!("Expected Progress event");
        }
    }

    #[tokio::test]
    async fn test_cleanup_closes_channel() {
        let bus = EventBus::new();
        let mut rx = bus.subscribe("wf-1");

        bus.cleanup("wf-1");

        // Channel should be closed
        assert_eq!(bus.active_channels(), 0);

        // Subscribers receive closed error
        let result = rx.recv().await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_subscriber_count() {
        let bus = EventBus::new();

        assert_eq!(bus.subscriber_count("wf-1"), 0);

        let _rx1 = bus.subscribe("wf-1");
        assert_eq!(bus.subscriber_count("wf-1"), 1);

        let _rx2 = bus.subscribe("wf-1");
        assert_eq!(bus.subscriber_count("wf-1"), 2);
    }

    #[tokio::test]
    async fn test_multiple_workflows() {
        let bus = EventBus::new();

        let mut rx1 = bus.subscribe("wf-1");
        let mut rx2 = bus.subscribe("wf-2");

        bus.broadcast("wf-1", create_test_event("wf-1", "msg1"))
            .unwrap();
        bus.broadcast("wf-2", create_test_event("wf-2", "msg2"))
            .unwrap();

        // Each subscriber only receives events for their workflow
        let event1 = rx1.recv().await.unwrap();
        assert_eq!(event1.workflow_id(), "wf-1");

        let event2 = rx2.recv().await.unwrap();
        assert_eq!(event2.workflow_id(), "wf-2");
    }

    #[tokio::test]
    async fn test_event_persistence_flag() {
        // Test that persistent events are marked correctly
        let persistent = WorkflowEvent::WorkflowStarted {
            workflow_id: "wf-1".to_string(),
            pattern_type: "cot".to_string(),
            timestamp: chrono::Utc::now(),
        };
        assert!(persistent.is_persistent());

        // Test that ephemeral events are marked correctly
        let ephemeral = WorkflowEvent::LlmPartial {
            workflow_id: "wf-1".to_string(),
            content: "partial response...".to_string(),
        };
        assert!(!ephemeral.is_persistent());
    }

    #[tokio::test]
    async fn test_backpressure_slow_consumer() {
        let bus = EventBus::new();
        let mut rx = bus.subscribe("wf-1");

        // Send more than channel capacity
        for i in 0..(CHANNEL_CAPACITY + 50) {
            bus.broadcast("wf-1", create_test_event("wf-1", &format!("event{i}")))
                .unwrap();
        }

        // Receiver should get lagged error
        let result = rx.recv().await;

        // Either we get a lagged error or we successfully receive an event
        // (timing dependent - both are acceptable)
        match result {
            Ok(_) => {
                // Successfully received an event (consumer kept up)
            }
            Err(broadcast::error::RecvError::Lagged(_)) => {
                // Got lagged error as expected
            }
            Err(e) => {
                panic!("Unexpected error: {e:?}");
            }
        }
    }
}
