//! Advanced workflow events.
//!
//! This module provides event types for advanced workflow features including
//! budget tracking, research synthesis, reflection, and approval workflows.

use crate::events::NormalizedEvent;
use serde::{Deserialize, Serialize};
use tokio::sync::broadcast;

/// Advanced workflow event types.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum AdvancedEvent {
    /// Token budget threshold warning (T141).
    BudgetThreshold {
        /// Workflow ID.
        workflow_id: String,
        /// Current token usage.
        current_tokens: u32,
        /// Budget limit.
        budget_limit: u32,
        /// Percentage of budget used (0-100).
        percent_used: u8,
        /// Severity level: info, warning, critical.
        severity: String,
    },

    /// Research synthesis event (T142).
    Synthesis {
        /// Workflow ID.
        workflow_id: String,
        /// Synthesis type (e.g., "preliminary", "final").
        synthesis_type: String,
        /// Number of sources synthesized.
        source_count: usize,
        /// Optional synthesis summary.
        #[serde(skip_serializing_if = "Option::is_none")]
        summary: Option<String>,
    },

    /// Reflection step event (T143).
    Reflection {
        /// Workflow ID.
        workflow_id: String,
        /// Agent ID performing reflection.
        agent_id: String,
        /// Reflection type (e.g., "self_critique", "plan_review").
        reflection_type: String,
        /// Reflection insights or findings.
        #[serde(skip_serializing_if = "Option::is_none")]
        insights: Option<String>,
    },

    /// Approval requested event (T144).
    ApprovalRequested {
        /// Workflow ID.
        workflow_id: String,
        /// Approval request ID.
        request_id: String,
        /// What needs approval.
        description: String,
        /// Optional context or reasoning.
        #[serde(skip_serializing_if = "Option::is_none")]
        context: Option<String>,
        /// Optional timeout for approval.
        #[serde(skip_serializing_if = "Option::is_none")]
        timeout_seconds: Option<u64>,
    },

    /// Approval decision event (T145).
    ApprovalDecision {
        /// Workflow ID.
        workflow_id: String,
        /// Approval request ID this decision is for.
        request_id: String,
        /// Decision: approved or rejected.
        approved: bool,
        /// Optional decision reason or comment.
        #[serde(skip_serializing_if = "Option::is_none")]
        reason: Option<String>,
        /// Who made the decision (user ID or system).
        #[serde(skip_serializing_if = "Option::is_none")]
        decided_by: Option<String>,
    },
}

impl AdvancedEvent {
    /// Convert to a normalized event for streaming.
    #[must_use]
    pub fn to_normalized(&self) -> NormalizedEvent {
        match self {
            Self::BudgetThreshold {
                workflow_id,
                current_tokens,
                budget_limit,
                percent_used,
                severity,
            } => NormalizedEvent::BudgetThreshold {
                workflow_id: workflow_id.clone(),
                current_tokens: *current_tokens,
                budget_limit: *budget_limit,
                percent_used: *percent_used,
                severity: severity.clone(),
            },
            Self::Synthesis {
                workflow_id,
                synthesis_type,
                source_count,
                summary,
            } => NormalizedEvent::Synthesis {
                workflow_id: workflow_id.clone(),
                synthesis_type: synthesis_type.clone(),
                source_count: *source_count,
                summary: summary.clone(),
            },
            Self::Reflection {
                workflow_id,
                agent_id,
                reflection_type,
                insights,
            } => NormalizedEvent::Reflection {
                workflow_id: workflow_id.clone(),
                agent_id: agent_id.clone(),
                reflection_type: reflection_type.clone(),
                insights: insights.clone(),
            },
            Self::ApprovalRequested {
                workflow_id,
                request_id,
                description,
                context,
                timeout_seconds,
            } => NormalizedEvent::ApprovalRequested {
                workflow_id: workflow_id.clone(),
                request_id: request_id.clone(),
                description: description.clone(),
                context: context.clone(),
                timeout_seconds: *timeout_seconds,
            },
            Self::ApprovalDecision {
                workflow_id,
                request_id,
                approved,
                reason,
                decided_by,
            } => NormalizedEvent::ApprovalDecision {
                workflow_id: workflow_id.clone(),
                request_id: request_id.clone(),
                approved: *approved,
                reason: reason.clone(),
                decided_by: decided_by.clone(),
            },
        }
    }

    /// Emit this advanced event to a broadcast channel.
    ///
    /// # Errors
    ///
    /// Returns error if the channel is closed or full.
    pub fn emit(&self, tx: &broadcast::Sender<NormalizedEvent>) -> Result<(), String> {
        tx.send(self.to_normalized())
            .map(|_| ())
            .map_err(|e| format!("Failed to emit advanced event: {e}"))
    }
}

/// Helper for emitting budget threshold event (T141).
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_budget_threshold(
    tx: &broadcast::Sender<NormalizedEvent>,
    workflow_id: impl Into<String>,
    current_tokens: u32,
    budget_limit: u32,
    percent_used: u8,
    severity: impl Into<String>,
) -> Result<(), String> {
    let event = AdvancedEvent::BudgetThreshold {
        workflow_id: workflow_id.into(),
        current_tokens,
        budget_limit,
        percent_used,
        severity: severity.into(),
    };
    event.emit(tx)
}

/// Helper for emitting synthesis event (T142).
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_synthesis(
    tx: &broadcast::Sender<NormalizedEvent>,
    workflow_id: impl Into<String>,
    synthesis_type: impl Into<String>,
    source_count: usize,
    summary: Option<impl Into<String>>,
) -> Result<(), String> {
    let event = AdvancedEvent::Synthesis {
        workflow_id: workflow_id.into(),
        synthesis_type: synthesis_type.into(),
        source_count,
        summary: summary.map(Into::into),
    };
    event.emit(tx)
}

/// Helper for emitting reflection event (T143).
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_reflection(
    tx: &broadcast::Sender<NormalizedEvent>,
    workflow_id: impl Into<String>,
    agent_id: impl Into<String>,
    reflection_type: impl Into<String>,
    insights: Option<impl Into<String>>,
) -> Result<(), String> {
    let event = AdvancedEvent::Reflection {
        workflow_id: workflow_id.into(),
        agent_id: agent_id.into(),
        reflection_type: reflection_type.into(),
        insights: insights.map(Into::into),
    };
    event.emit(tx)
}

/// Helper for emitting approval requested event (T144).
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_approval_requested(
    tx: &broadcast::Sender<NormalizedEvent>,
    workflow_id: impl Into<String>,
    request_id: impl Into<String>,
    description: impl Into<String>,
    context: Option<impl Into<String>>,
    timeout_seconds: Option<u64>,
) -> Result<(), String> {
    let event = AdvancedEvent::ApprovalRequested {
        workflow_id: workflow_id.into(),
        request_id: request_id.into(),
        description: description.into(),
        context: context.map(Into::into),
        timeout_seconds,
    };
    event.emit(tx)
}

/// Helper for emitting approval decision event (T145).
///
/// # Errors
///
/// Returns error if emission fails.
#[expect(
    clippy::fn_params_excessive_bools,
    reason = "approved is a clear boolean flag"
)]
pub fn emit_approval_decision(
    tx: &broadcast::Sender<NormalizedEvent>,
    workflow_id: impl Into<String>,
    request_id: impl Into<String>,
    approved: bool,
    reason: Option<impl Into<String>>,
    decided_by: Option<impl Into<String>>,
) -> Result<(), String> {
    let event = AdvancedEvent::ApprovalDecision {
        workflow_id: workflow_id.into(),
        request_id: request_id.into(),
        approved,
        reason: reason.map(Into::into),
        decided_by: decided_by.map(Into::into),
    };
    event.emit(tx)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_budget_threshold_event() {
        let (tx, mut rx) = broadcast::channel(16);

        emit_budget_threshold(&tx, "workflow-1", 8000, 10000, 80, "warning").unwrap();

        let event = rx.try_recv().unwrap();
        match event {
            NormalizedEvent::BudgetThreshold {
                workflow_id,
                current_tokens,
                budget_limit,
                percent_used,
                severity,
            } => {
                assert_eq!(workflow_id, "workflow-1");
                assert_eq!(current_tokens, 8000);
                assert_eq!(budget_limit, 10000);
                assert_eq!(percent_used, 80);
                assert_eq!(severity, "warning");
            }
            _ => panic!("Expected BudgetThreshold event"),
        }
    }

    #[test]
    fn test_synthesis_event() {
        let (tx, mut rx) = broadcast::channel(16);

        emit_synthesis(
            &tx,
            "workflow-1",
            "final",
            5,
            Some("Synthesized findings from 5 sources"),
        )
        .unwrap();

        let event = rx.try_recv().unwrap();
        match event {
            NormalizedEvent::Synthesis {
                workflow_id,
                synthesis_type,
                source_count,
                summary,
            } => {
                assert_eq!(workflow_id, "workflow-1");
                assert_eq!(synthesis_type, "final");
                assert_eq!(source_count, 5);
                assert_eq!(
                    summary,
                    Some("Synthesized findings from 5 sources".to_string())
                );
            }
            _ => panic!("Expected Synthesis event"),
        }
    }

    #[test]
    fn test_reflection_event() {
        let (tx, mut rx) = broadcast::channel(16);

        emit_reflection(
            &tx,
            "workflow-1",
            "agent-1",
            "self_critique",
            Some("Identified potential improvements"),
        )
        .unwrap();

        let event = rx.try_recv().unwrap();
        match event {
            NormalizedEvent::Reflection {
                workflow_id,
                agent_id,
                reflection_type,
                insights,
            } => {
                assert_eq!(workflow_id, "workflow-1");
                assert_eq!(agent_id, "agent-1");
                assert_eq!(reflection_type, "self_critique");
                assert_eq!(
                    insights,
                    Some("Identified potential improvements".to_string())
                );
            }
            _ => panic!("Expected Reflection event"),
        }
    }

    #[test]
    fn test_approval_workflow() {
        let (tx, mut rx) = broadcast::channel(16);

        // Test approval requested
        emit_approval_requested(
            &tx,
            "workflow-1",
            "req-1",
            "Execute high-cost operation",
            Some("Will use 5000 tokens"),
            Some(300),
        )
        .unwrap();

        match rx.try_recv().unwrap() {
            NormalizedEvent::ApprovalRequested {
                workflow_id,
                request_id,
                description,
                context,
                timeout_seconds,
            } => {
                assert_eq!(workflow_id, "workflow-1");
                assert_eq!(request_id, "req-1");
                assert_eq!(description, "Execute high-cost operation");
                assert_eq!(context, Some("Will use 5000 tokens".to_string()));
                assert_eq!(timeout_seconds, Some(300));
            }
            _ => panic!("Expected ApprovalRequested event"),
        }

        // Test approval decision
        emit_approval_decision(
            &tx,
            "workflow-1",
            "req-1",
            true,
            Some("Approved for research purposes"),
            Some("user-123"),
        )
        .unwrap();

        match rx.try_recv().unwrap() {
            NormalizedEvent::ApprovalDecision {
                workflow_id,
                request_id,
                approved,
                reason,
                decided_by,
            } => {
                assert_eq!(workflow_id, "workflow-1");
                assert_eq!(request_id, "req-1");
                assert!(approved);
                assert_eq!(reason, Some("Approved for research purposes".to_string()));
                assert_eq!(decided_by, Some("user-123".to_string()));
            }
            _ => panic!("Expected ApprovalDecision event"),
        }
    }
}
