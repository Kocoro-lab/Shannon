//! Workflow control signal events.
//!
//! This module provides control signal event emission for workflow lifecycle
//! management including pause, resume, and cancellation operations.
//!
//! Events are emitted during control operations to provide real-time visibility
//! into workflow state transitions and integrate with durable checkpoints.

use crate::events::NormalizedEvent;
use tokio::sync::broadcast;

/// Control signal event types for workflow management.
#[derive(Debug, Clone)]
pub enum ControlSignalEvent {
    /// Workflow is in the process of pausing (T120).
    WorkflowPausing {
        /// Workflow ID being paused.
        workflow_id: String,
        /// Optional reason for pausing.
        reason: Option<String>,
    },

    /// Workflow has been successfully paused (T121).
    WorkflowPaused {
        /// Workflow ID that was paused.
        workflow_id: String,
        /// Checkpoint ID for resumption.
        checkpoint_id: Option<String>,
    },

    /// Workflow has been resumed (T122).
    WorkflowResumed {
        /// Workflow ID being resumed.
        workflow_id: String,
        /// Checkpoint ID resumed from.
        checkpoint_id: Option<String>,
        /// Optional reason for resuming.
        reason: Option<String>,
    },

    /// Workflow is in the process of cancelling (T123).
    WorkflowCancelling {
        /// Workflow ID being cancelled.
        workflow_id: String,
        /// Optional reason for cancellation.
        reason: Option<String>,
    },

    /// Workflow has been successfully cancelled (T124).
    WorkflowCancelled {
        /// Workflow ID that was cancelled.
        workflow_id: String,
        /// Final checkpoint before cancellation.
        final_checkpoint: Option<String>,
    },
}

impl ControlSignalEvent {
    /// Convert to a normalized event for streaming.
    #[must_use]
    pub fn to_normalized(&self) -> NormalizedEvent {
        match self {
            Self::WorkflowPausing {
                workflow_id,
                reason,
            } => NormalizedEvent::WorkflowPausing {
                workflow_id: workflow_id.clone(),
                reason: reason.clone(),
            },
            Self::WorkflowPaused {
                workflow_id,
                checkpoint_id,
            } => NormalizedEvent::WorkflowPaused {
                workflow_id: workflow_id.clone(),
                checkpoint_id: checkpoint_id.clone(),
            },
            Self::WorkflowResumed {
                workflow_id,
                checkpoint_id,
                reason,
            } => NormalizedEvent::WorkflowResumed {
                workflow_id: workflow_id.clone(),
                checkpoint_id: checkpoint_id.clone(),
                reason: reason.clone(),
            },
            Self::WorkflowCancelling {
                workflow_id,
                reason,
            } => NormalizedEvent::WorkflowCancelling {
                workflow_id: workflow_id.clone(),
                reason: reason.clone(),
            },
            Self::WorkflowCancelled {
                workflow_id,
                final_checkpoint,
            } => NormalizedEvent::WorkflowCancelled {
                workflow_id: workflow_id.clone(),
                final_checkpoint: final_checkpoint.clone(),
            },
        }
    }

    /// Emit this control signal event to a broadcast channel.
    ///
    /// # Errors
    ///
    /// Returns error if the channel is closed or full.
    pub fn emit(&self, tx: &broadcast::Sender<NormalizedEvent>) -> Result<(), String> {
        tx.send(self.to_normalized())
            .map(|_| ())
            .map_err(|e| format!("Failed to emit control signal event: {e}"))
    }
}

/// Helper for emitting workflow pausing event.
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_workflow_pausing(
    tx: &broadcast::Sender<NormalizedEvent>,
    workflow_id: impl Into<String>,
    reason: Option<impl Into<String>>,
) -> Result<(), String> {
    let event = ControlSignalEvent::WorkflowPausing {
        workflow_id: workflow_id.into(),
        reason: reason.map(Into::into),
    };
    event.emit(tx)
}

/// Helper for emitting workflow paused event.
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_workflow_paused(
    tx: &broadcast::Sender<NormalizedEvent>,
    workflow_id: impl Into<String>,
    checkpoint_id: Option<impl Into<String>>,
) -> Result<(), String> {
    let event = ControlSignalEvent::WorkflowPaused {
        workflow_id: workflow_id.into(),
        checkpoint_id: checkpoint_id.map(Into::into),
    };
    event.emit(tx)
}

/// Helper for emitting workflow resumed event.
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_workflow_resumed(
    tx: &broadcast::Sender<NormalizedEvent>,
    workflow_id: impl Into<String>,
    checkpoint_id: Option<impl Into<String>>,
    reason: Option<impl Into<String>>,
) -> Result<(), String> {
    let event = ControlSignalEvent::WorkflowResumed {
        workflow_id: workflow_id.into(),
        checkpoint_id: checkpoint_id.map(Into::into),
        reason: reason.map(Into::into),
    };
    event.emit(tx)
}

/// Helper for emitting workflow cancelling event.
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_workflow_cancelling(
    tx: &broadcast::Sender<NormalizedEvent>,
    workflow_id: impl Into<String>,
    reason: Option<impl Into<String>>,
) -> Result<(), String> {
    let event = ControlSignalEvent::WorkflowCancelling {
        workflow_id: workflow_id.into(),
        reason: reason.map(Into::into),
    };
    event.emit(tx)
}

/// Helper for emitting workflow cancelled event.
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_workflow_cancelled(
    tx: &broadcast::Sender<NormalizedEvent>,
    workflow_id: impl Into<String>,
    final_checkpoint: Option<impl Into<String>>,
) -> Result<(), String> {
    let event = ControlSignalEvent::WorkflowCancelled {
        workflow_id: workflow_id.into(),
        final_checkpoint: final_checkpoint.map(Into::into),
    };
    event.emit(tx)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_control_signal_event_conversion() {
        let event = ControlSignalEvent::WorkflowPausing {
            workflow_id: "wf-123".to_string(),
            reason: Some("User requested".to_string()),
        };

        let normalized = event.to_normalized();
        match normalized {
            NormalizedEvent::WorkflowPausing {
                workflow_id,
                reason,
            } => {
                assert_eq!(workflow_id, "wf-123");
                assert_eq!(reason, Some("User requested".to_string()));
            }
            _ => panic!("Expected WorkflowPausing event"),
        }
    }

    #[test]
    fn test_emit_helpers() {
        let (tx, _rx) = broadcast::channel(16);

        assert!(emit_workflow_pausing(&tx, "wf-123", Some("test")).is_ok());
        assert!(emit_workflow_paused(&tx, "wf-123", Some("cp-1")).is_ok());

        let reason: Option<&str> = None;
        assert!(emit_workflow_resumed(&tx, "wf-123", Some("cp-1"), reason).is_ok());

        assert!(emit_workflow_cancelling(&tx, "wf-123", Some("test")).is_ok());
        assert!(emit_workflow_cancelled(&tx, "wf-123", Some("cp-final")).is_ok());
    }
}
