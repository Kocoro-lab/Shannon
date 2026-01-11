//! Multi-agent coordination events.
//!
//! This module provides event types for multi-agent workflows including
//! role assignment, delegation, team management, and progress tracking.

use crate::events::NormalizedEvent;
use serde::{Deserialize, Serialize};
use tokio::sync::broadcast;

/// Multi-agent coordination event types.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum MultiAgentEvent {
    /// Agent role assigned (T135).
    RoleAssigned {
        /// Agent ID receiving the role.
        agent_id: String,
        /// Role name assigned.
        role: String,
        /// Optional role description.
        #[serde(skip_serializing_if = "Option::is_none")]
        description: Option<String>,
    },

    /// Task delegated to an agent (T136).
    Delegation {
        /// Source agent ID (delegator).
        from_agent: String,
        /// Target agent ID (delegatee).
        to_agent: String,
        /// Task being delegated.
        task_description: String,
        /// Optional priority.
        #[serde(skip_serializing_if = "Option::is_none")]
        priority: Option<String>,
    },

    /// Agent progress update (T137).
    Progress {
        /// Agent ID reporting progress.
        agent_id: String,
        /// Progress percentage (0-100).
        percent: u8,
        /// Optional progress message.
        #[serde(skip_serializing_if = "Option::is_none")]
        message: Option<String>,
        /// Current step or phase.
        #[serde(skip_serializing_if = "Option::is_none")]
        current_step: Option<String>,
    },

    /// Team recruited for collaborative work (T138).
    TeamRecruited {
        /// Team ID.
        team_id: String,
        /// List of agent IDs in the team.
        agents: Vec<String>,
        /// Team purpose or goal.
        #[serde(skip_serializing_if = "Option::is_none")]
        purpose: Option<String>,
    },

    /// Team retired after completion (T139).
    TeamRetired {
        /// Team ID.
        team_id: String,
        /// Reason for retirement.
        #[serde(skip_serializing_if = "Option::is_none")]
        reason: Option<String>,
        /// Team completion status.
        #[serde(skip_serializing_if = "Option::is_none")]
        completion_status: Option<String>,
    },

    /// Team status update (T140).
    TeamStatus {
        /// Team ID.
        team_id: String,
        /// Current team status.
        status: String,
        /// Active agent count.
        active_agents: usize,
        /// Optional progress percentage.
        #[serde(skip_serializing_if = "Option::is_none")]
        progress: Option<u8>,
    },
}

impl MultiAgentEvent {
    /// Convert to a normalized event for streaming.
    #[must_use]
    pub fn to_normalized(&self) -> NormalizedEvent {
        match self {
            Self::RoleAssigned {
                agent_id,
                role,
                description,
            } => NormalizedEvent::RoleAssigned {
                agent_id: agent_id.clone(),
                role: role.clone(),
                description: description.clone(),
            },
            Self::Delegation {
                from_agent,
                to_agent,
                task_description,
                priority,
            } => NormalizedEvent::Delegation {
                from_agent: from_agent.clone(),
                to_agent: to_agent.clone(),
                task_description: task_description.clone(),
                priority: priority.clone(),
            },
            Self::Progress {
                agent_id,
                percent,
                message,
                current_step,
            } => NormalizedEvent::Progress {
                agent_id: agent_id.clone(),
                percent: *percent,
                message: message.clone(),
                current_step: current_step.clone(),
            },
            Self::TeamRecruited {
                team_id,
                agents,
                purpose,
            } => NormalizedEvent::TeamRecruited {
                team_id: team_id.clone(),
                agents: agents.clone(),
                purpose: purpose.clone(),
            },
            Self::TeamRetired {
                team_id,
                reason,
                completion_status,
            } => NormalizedEvent::TeamRetired {
                team_id: team_id.clone(),
                reason: reason.clone(),
                completion_status: completion_status.clone(),
            },
            Self::TeamStatus {
                team_id,
                status,
                active_agents,
                progress,
            } => NormalizedEvent::TeamStatus {
                team_id: team_id.clone(),
                status: status.clone(),
                active_agents: *active_agents,
                progress: *progress,
            },
        }
    }

    /// Emit this multi-agent event to a broadcast channel.
    ///
    /// # Errors
    ///
    /// Returns error if the channel is closed or full.
    pub fn emit(&self, tx: &broadcast::Sender<NormalizedEvent>) -> Result<(), String> {
        tx.send(self.to_normalized())
            .map(|_| ())
            .map_err(|e| format!("Failed to emit multi-agent event: {e}"))
    }
}

/// Helper for emitting role assigned event (T135).
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_role_assigned(
    tx: &broadcast::Sender<NormalizedEvent>,
    agent_id: impl Into<String>,
    role: impl Into<String>,
    description: Option<impl Into<String>>,
) -> Result<(), String> {
    let event = MultiAgentEvent::RoleAssigned {
        agent_id: agent_id.into(),
        role: role.into(),
        description: description.map(Into::into),
    };
    event.emit(tx)
}

/// Helper for emitting delegation event (T136).
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_delegation(
    tx: &broadcast::Sender<NormalizedEvent>,
    from_agent: impl Into<String>,
    to_agent: impl Into<String>,
    task_description: impl Into<String>,
    priority: Option<impl Into<String>>,
) -> Result<(), String> {
    let event = MultiAgentEvent::Delegation {
        from_agent: from_agent.into(),
        to_agent: to_agent.into(),
        task_description: task_description.into(),
        priority: priority.map(Into::into),
    };
    event.emit(tx)
}

/// Helper for emitting progress event (T137).
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_progress(
    tx: &broadcast::Sender<NormalizedEvent>,
    agent_id: impl Into<String>,
    percent: u8,
    message: Option<impl Into<String>>,
    current_step: Option<impl Into<String>>,
) -> Result<(), String> {
    let event = MultiAgentEvent::Progress {
        agent_id: agent_id.into(),
        percent,
        message: message.map(Into::into),
        current_step: current_step.map(Into::into),
    };
    event.emit(tx)
}

/// Helper for emitting team recruited event (T138).
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_team_recruited(
    tx: &broadcast::Sender<NormalizedEvent>,
    team_id: impl Into<String>,
    agents: Vec<String>,
    purpose: Option<impl Into<String>>,
) -> Result<(), String> {
    let event = MultiAgentEvent::TeamRecruited {
        team_id: team_id.into(),
        agents,
        purpose: purpose.map(Into::into),
    };
    event.emit(tx)
}

/// Helper for emitting team retired event (T139).
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_team_retired(
    tx: &broadcast::Sender<NormalizedEvent>,
    team_id: impl Into<String>,
    reason: Option<impl Into<String>>,
    completion_status: Option<impl Into<String>>,
) -> Result<(), String> {
    let event = MultiAgentEvent::TeamRetired {
        team_id: team_id.into(),
        reason: reason.map(Into::into),
        completion_status: completion_status.map(Into::into),
    };
    event.emit(tx)
}

/// Helper for emitting team status event (T140).
///
/// # Errors
///
/// Returns error if emission fails.
pub fn emit_team_status(
    tx: &broadcast::Sender<NormalizedEvent>,
    team_id: impl Into<String>,
    status: impl Into<String>,
    active_agents: usize,
    progress: Option<u8>,
) -> Result<(), String> {
    let event = MultiAgentEvent::TeamStatus {
        team_id: team_id.into(),
        status: status.into(),
        active_agents,
        progress,
    };
    event.emit(tx)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_role_assigned_event() {
        let (tx, mut rx) = broadcast::channel(16);

        emit_role_assigned(
            &tx,
            "agent-1",
            "researcher",
            Some("Deep research specialist"),
        )
        .unwrap();

        let event = rx.try_recv().unwrap();
        match event {
            NormalizedEvent::RoleAssigned {
                agent_id,
                role,
                description,
            } => {
                assert_eq!(agent_id, "agent-1");
                assert_eq!(role, "researcher");
                assert_eq!(description, Some("Deep research specialist".to_string()));
            }
            _ => panic!("Expected RoleAssigned event"),
        }
    }

    #[test]
    fn test_delegation_event() {
        let (tx, mut rx) = broadcast::channel(16);

        emit_delegation(
            &tx,
            "agent-1",
            "agent-2",
            "Search for information",
            Some("high"),
        )
        .unwrap();

        let event = rx.try_recv().unwrap();
        match event {
            NormalizedEvent::Delegation {
                from_agent,
                to_agent,
                task_description,
                priority,
            } => {
                assert_eq!(from_agent, "agent-1");
                assert_eq!(to_agent, "agent-2");
                assert_eq!(task_description, "Search for information");
                assert_eq!(priority, Some("high".to_string()));
            }
            _ => panic!("Expected Delegation event"),
        }
    }

    #[test]
    fn test_team_events() {
        let (tx, mut rx) = broadcast::channel(16);

        // Test team recruited
        emit_team_recruited(
            &tx,
            "team-1",
            vec!["agent-1".to_string(), "agent-2".to_string()],
            Some("Research team"),
        )
        .unwrap();

        match rx.try_recv().unwrap() {
            NormalizedEvent::TeamRecruited {
                team_id,
                agents,
                purpose,
            } => {
                assert_eq!(team_id, "team-1");
                assert_eq!(agents.len(), 2);
                assert_eq!(purpose, Some("Research team".to_string()));
            }
            _ => panic!("Expected TeamRecruited event"),
        }

        // Test team status
        emit_team_status(&tx, "team-1", "active", 2, Some(50)).unwrap();

        match rx.try_recv().unwrap() {
            NormalizedEvent::TeamStatus {
                team_id,
                status,
                active_agents,
                progress,
            } => {
                assert_eq!(team_id, "team-1");
                assert_eq!(status, "active");
                assert_eq!(active_agents, 2);
                assert_eq!(progress, Some(50));
            }
            _ => panic!("Expected TeamStatus event"),
        }

        // Test team retired
        emit_team_retired(&tx, "team-1", Some("Task completed"), Some("success")).unwrap();

        match rx.try_recv().unwrap() {
            NormalizedEvent::TeamRetired {
                team_id,
                reason,
                completion_status,
            } => {
                assert_eq!(team_id, "team-1");
                assert_eq!(reason, Some("Task completed".to_string()));
                assert_eq!(completion_status, Some("success".to_string()));
            }
            _ => panic!("Expected TeamRetired event"),
        }
    }
}
