//! Normalized streaming event model.
//!
//! This module provides a unified event model for all LLM interactions,
//! normalizing events from different providers into a consistent format.
//! Also includes workflow-level and agent-level events for comprehensive observability.

use serde::{Deserialize, Serialize};
use uuid::Uuid;

/// A normalized streaming event from any LLM provider or workflow engine.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum NormalizedEvent {
    // === LLM Events ===
    /// LLM prompt being sent (T114).
    LlmPrompt {
        /// The prompt being sent to the LLM.
        prompt: String,
        /// Model being used.
        model: String,
        /// Provider being used.
        #[serde(skip_serializing_if = "Option::is_none")]
        provider: Option<String>,
    },

    /// Partial message content (streaming delta) (T115 - LLM_PARTIAL).
    MessageDelta {
        /// The content delta.
        content: String,
        /// Optional role (usually only on first delta).
        #[serde(skip_serializing_if = "Option::is_none")]
        role: Option<String>,
    },

    /// Message completed (T116 - LLM_OUTPUT).
    MessageComplete {
        /// Full message content.
        content: String,
        /// Message role.
        role: String,
        /// Finish reason.
        #[serde(skip_serializing_if = "Option::is_none")]
        finish_reason: Option<String>,
    },

    // === Tool Events ===
    /// Tool call delta (streaming).
    ToolCallDelta {
        /// Tool call index.
        index: usize,
        /// Tool call ID (may be partial).
        #[serde(skip_serializing_if = "Option::is_none")]
        id: Option<String>,
        /// Tool name (may be partial).
        #[serde(skip_serializing_if = "Option::is_none")]
        name: Option<String>,
        /// Arguments delta (JSON string fragment).
        #[serde(skip_serializing_if = "Option::is_none")]
        arguments: Option<String>,
    },

    /// Tool call completed (T117 - TOOL_INVOKED).
    ToolCallComplete {
        /// Tool call ID.
        id: String,
        /// Tool name.
        name: String,
        /// Complete arguments as JSON string.
        arguments: String,
    },

    /// Tool execution result (T118 - TOOL_OBSERVATION).
    ToolResult {
        /// Tool call ID this result corresponds to.
        tool_call_id: String,
        /// Tool name.
        name: String,
        /// Result content.
        content: String,
        /// Whether the tool execution was successful.
        success: bool,
    },

    /// Tool execution error (T119 - TOOL_ERROR).
    ToolError {
        /// Tool call ID.
        tool_call_id: String,
        /// Tool name.
        name: String,
        /// Error message.
        error: String,
    },

    // === Workflow Events ===
    /// Workflow started (T090).
    WorkflowStarted {
        /// Workflow ID.
        workflow_id: String,
        /// Workflow type/strategy.
        workflow_type: String,
    },

    /// Workflow completed successfully (T091).
    WorkflowCompleted {
        /// Workflow ID.
        workflow_id: String,
        /// Result summary.
        #[serde(skip_serializing_if = "Option::is_none")]
        result: Option<String>,
    },

    /// Workflow failed (T092).
    WorkflowFailed {
        /// Workflow ID.
        workflow_id: String,
        /// Error message.
        error: String,
    },

    // === Agent Events ===
    /// Agent started (T093).
    AgentStarted {
        /// Agent ID.
        agent_id: String,
        /// Agent role/type.
        #[serde(skip_serializing_if = "Option::is_none")]
        role: Option<String>,
    },

    /// Agent completed (T094).
    AgentCompleted {
        /// Agent ID.
        agent_id: String,
        /// Agent output.
        #[serde(skip_serializing_if = "Option::is_none")]
        output: Option<String>,
    },

    /// Agent failed (T095).
    AgentFailed {
        /// Agent ID.
        agent_id: String,
        /// Error message.
        error: String,
    },

    // === Misc Events ===
    /// Thinking/reasoning content (for models that support it).
    Thinking {
        /// Thinking content.
        content: String,
    },

    /// Usage statistics.
    Usage {
        /// Prompt tokens used.
        prompt_tokens: u32,
        /// Completion tokens used.
        completion_tokens: u32,
        /// Total tokens used.
        total_tokens: u32,
        /// Model used.
        #[serde(skip_serializing_if = "Option::is_none")]
        model: Option<String>,
    },

    /// Error event (T096 - ERROR_OCCURRED).
    Error {
        /// Error message.
        message: String,
        /// Error code.
        #[serde(skip_serializing_if = "Option::is_none")]
        code: Option<String>,
    },

    // === Control Signal Events ===
    /// Workflow is pausing (T120).
    WorkflowPausing {
        /// Workflow ID.
        workflow_id: String,
        /// Optional reason for pausing.
        #[serde(skip_serializing_if = "Option::is_none")]
        reason: Option<String>,
    },

    /// Workflow has been paused (T121).
    WorkflowPaused {
        /// Workflow ID.
        workflow_id: String,
        /// Checkpoint ID for resumption.
        #[serde(skip_serializing_if = "Option::is_none")]
        checkpoint_id: Option<String>,
    },

    /// Workflow has been resumed (T122).
    WorkflowResumed {
        /// Workflow ID.
        workflow_id: String,
        /// Checkpoint ID resumed from.
        #[serde(skip_serializing_if = "Option::is_none")]
        checkpoint_id: Option<String>,
        /// Optional reason for resuming.
        #[serde(skip_serializing_if = "Option::is_none")]
        reason: Option<String>,
    },

    /// Workflow is cancelling (T123).
    WorkflowCancelling {
        /// Workflow ID.
        workflow_id: String,
        /// Optional reason for cancellation.
        #[serde(skip_serializing_if = "Option::is_none")]
        reason: Option<String>,
    },

    /// Workflow has been cancelled (T124).
    WorkflowCancelled {
        /// Workflow ID.
        workflow_id: String,
        /// Final checkpoint before cancellation.
        #[serde(skip_serializing_if = "Option::is_none")]
        final_checkpoint: Option<String>,
    },

    // === Multi-Agent Events ===
    /// Agent role assigned (T135).
    RoleAssigned {
        /// Agent ID.
        agent_id: String,
        /// Role name.
        role: String,
        /// Optional description.
        #[serde(skip_serializing_if = "Option::is_none")]
        description: Option<String>,
    },

    /// Task delegated (T136).
    Delegation {
        /// Source agent ID.
        from_agent: String,
        /// Target agent ID.
        to_agent: String,
        /// Task description.
        task_description: String,
        /// Optional priority.
        #[serde(skip_serializing_if = "Option::is_none")]
        priority: Option<String>,
    },

    /// Agent progress update (T137).
    Progress {
        /// Agent ID.
        agent_id: String,
        /// Progress percentage (0-100).
        percent: u8,
        /// Optional message.
        #[serde(skip_serializing_if = "Option::is_none")]
        message: Option<String>,
        /// Current step.
        #[serde(skip_serializing_if = "Option::is_none")]
        current_step: Option<String>,
    },

    /// Team recruited (T138).
    TeamRecruited {
        /// Team ID.
        team_id: String,
        /// Agent IDs in team.
        agents: Vec<String>,
        /// Team purpose.
        #[serde(skip_serializing_if = "Option::is_none")]
        purpose: Option<String>,
    },

    /// Team retired (T139).
    TeamRetired {
        /// Team ID.
        team_id: String,
        /// Retirement reason.
        #[serde(skip_serializing_if = "Option::is_none")]
        reason: Option<String>,
        /// Completion status.
        #[serde(skip_serializing_if = "Option::is_none")]
        completion_status: Option<String>,
    },

    /// Team status update (T140).
    TeamStatus {
        /// Team ID.
        team_id: String,
        /// Current status.
        status: String,
        /// Active agent count.
        active_agents: usize,
        /// Optional progress.
        #[serde(skip_serializing_if = "Option::is_none")]
        progress: Option<u8>,
    },

    // === Advanced Events ===
    /// Token budget threshold warning (T141).
    BudgetThreshold {
        /// Workflow ID.
        workflow_id: String,
        /// Current token usage.
        current_tokens: u32,
        /// Budget limit.
        budget_limit: u32,
        /// Percentage used (0-100).
        percent_used: u8,
        /// Severity level.
        severity: String,
    },

    /// Research synthesis event (T142).
    Synthesis {
        /// Workflow ID.
        workflow_id: String,
        /// Synthesis type.
        synthesis_type: String,
        /// Source count.
        source_count: usize,
        /// Optional summary.
        #[serde(skip_serializing_if = "Option::is_none")]
        summary: Option<String>,
    },

    /// Reflection step event (T143).
    Reflection {
        /// Workflow ID.
        workflow_id: String,
        /// Agent ID.
        agent_id: String,
        /// Reflection type.
        reflection_type: String,
        /// Optional insights.
        #[serde(skip_serializing_if = "Option::is_none")]
        insights: Option<String>,
    },

    /// Approval requested (T144).
    ApprovalRequested {
        /// Workflow ID.
        workflow_id: String,
        /// Request ID.
        request_id: String,
        /// Description.
        description: String,
        /// Optional context.
        #[serde(skip_serializing_if = "Option::is_none")]
        context: Option<String>,
        /// Optional timeout.
        #[serde(skip_serializing_if = "Option::is_none")]
        timeout_seconds: Option<u64>,
    },

    /// Approval decision (T145).
    ApprovalDecision {
        /// Workflow ID.
        workflow_id: String,
        /// Request ID.
        request_id: String,
        /// Approved flag.
        approved: bool,
        /// Optional reason.
        #[serde(skip_serializing_if = "Option::is_none")]
        reason: Option<String>,
        /// Optional decider.
        #[serde(skip_serializing_if = "Option::is_none")]
        decided_by: Option<String>,
    },

    /// Stream done signal.
    Done {
        /// Finish reason.
        #[serde(skip_serializing_if = "Option::is_none")]
        finish_reason: Option<String>,
    },
}

impl NormalizedEvent {
    /// Create a message delta event.
    pub fn message_delta(content: impl Into<String>) -> Self {
        Self::MessageDelta {
            content: content.into(),
            role: None,
        }
    }

    /// Create a message delta event with role.
    pub fn message_delta_with_role(content: impl Into<String>, role: impl Into<String>) -> Self {
        Self::MessageDelta {
            content: content.into(),
            role: Some(role.into()),
        }
    }

    /// Create an error event.
    pub fn error(message: impl Into<String>) -> Self {
        Self::Error {
            message: message.into(),
            code: None,
        }
    }

    /// Create a done event.
    pub fn done() -> Self {
        Self::Done {
            finish_reason: None,
        }
    }

    /// Create a done event with finish reason.
    pub fn done_with_reason(reason: impl Into<String>) -> Self {
        Self::Done {
            finish_reason: Some(reason.into()),
        }
    }
}

/// A stream event with metadata for SSE.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StreamEvent {
    /// Unique event ID.
    pub id: String,
    /// Sequence number.
    pub seq: u64,
    /// The normalized event.
    #[serde(flatten)]
    pub event: NormalizedEvent,
    /// Timestamp.
    pub timestamp: chrono::DateTime<chrono::Utc>,
}

impl StreamEvent {
    /// Create a new stream event.
    pub fn new(seq: u64, event: NormalizedEvent) -> Self {
        Self {
            id: Uuid::new_v4().to_string(),
            seq,
            event,
            timestamp: chrono::Utc::now(),
        }
    }

    /// Get the SSE event type for this event.
    pub fn event_type(&self) -> &'static str {
        match &self.event {
            // LLM events
            NormalizedEvent::LlmPrompt { .. } => "llm.prompt",
            NormalizedEvent::MessageDelta { .. } => "thread.message.delta",
            NormalizedEvent::MessageComplete { .. } => "thread.message.completed",

            // Tool events
            NormalizedEvent::ToolCallDelta { .. } => "tool_call.delta",
            NormalizedEvent::ToolCallComplete { .. } => "tool_call.complete",
            NormalizedEvent::ToolResult { .. } => "tool.result",
            NormalizedEvent::ToolError { .. } => "tool.error",

            // Workflow events
            NormalizedEvent::WorkflowStarted { .. } => "workflow.started",
            NormalizedEvent::WorkflowCompleted { .. } => "workflow.completed",
            NormalizedEvent::WorkflowFailed { .. } => "workflow.failed",

            // Agent events
            NormalizedEvent::AgentStarted { .. } => "agent.started",
            NormalizedEvent::AgentCompleted { .. } => "agent.completed",
            NormalizedEvent::AgentFailed { .. } => "agent.failed",

            // Control signal events
            NormalizedEvent::WorkflowPausing { .. } => "workflow.pausing",
            NormalizedEvent::WorkflowPaused { .. } => "workflow.paused",
            NormalizedEvent::WorkflowResumed { .. } => "workflow.resumed",
            NormalizedEvent::WorkflowCancelling { .. } => "workflow.cancelling",
            NormalizedEvent::WorkflowCancelled { .. } => "workflow.cancelled",

            // Multi-agent events
            NormalizedEvent::RoleAssigned { .. } => "agent.role_assigned",
            NormalizedEvent::Delegation { .. } => "agent.delegation",
            NormalizedEvent::Progress { .. } => "agent.progress",
            NormalizedEvent::TeamRecruited { .. } => "team.recruited",
            NormalizedEvent::TeamRetired { .. } => "team.retired",
            NormalizedEvent::TeamStatus { .. } => "team.status",

            // Advanced events
            NormalizedEvent::BudgetThreshold { .. } => "workflow.budget_threshold",
            NormalizedEvent::Synthesis { .. } => "workflow.synthesis",
            NormalizedEvent::Reflection { .. } => "workflow.reflection",
            NormalizedEvent::ApprovalRequested { .. } => "workflow.approval_requested",
            NormalizedEvent::ApprovalDecision { .. } => "workflow.approval_decision",

            // Misc events
            NormalizedEvent::Thinking { .. } => "thinking",
            NormalizedEvent::Usage { .. } => "usage",
            NormalizedEvent::Error { .. } => "error",
            NormalizedEvent::Done { .. } => "done",
        }
    }
}

/// Accumulator for streaming tool calls.
#[derive(Debug, Default, Clone)]
pub struct ToolCallAccumulator {
    /// Tool call ID.
    pub id: Option<String>,
    /// Tool name.
    pub name: Option<String>,
    /// Arguments accumulated so far.
    pub arguments: String,
}

impl ToolCallAccumulator {
    /// Create a new accumulator.
    pub fn new() -> Self {
        Self::default()
    }

    /// Apply a delta to this accumulator.
    pub fn apply_delta(
        &mut self,
        id: Option<String>,
        name: Option<String>,
        arguments: Option<String>,
    ) {
        if let Some(id) = id {
            self.id = Some(id);
        }
        if let Some(name) = name {
            self.name = Some(name);
        }
        if let Some(args) = arguments {
            self.arguments.push_str(&args);
        }
    }

    /// Check if this tool call is complete.
    pub fn is_complete(&self) -> bool {
        self.id.is_some() && self.name.is_some()
    }

    /// Convert to a complete tool call event.
    pub fn to_complete(&self) -> Option<NormalizedEvent> {
        match (&self.id, &self.name) {
            (Some(id), Some(name)) => Some(NormalizedEvent::ToolCallComplete {
                id: id.clone(),
                name: name.clone(),
                arguments: self.arguments.clone(),
            }),
            _ => None,
        }
    }
}
