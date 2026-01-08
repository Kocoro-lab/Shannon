//! Normalized streaming event model.
//!
//! This module provides a unified event model for all LLM interactions,
//! normalizing events from different providers into a consistent format.

use serde::{Deserialize, Serialize};
use uuid::Uuid;

/// A normalized streaming event from any LLM provider.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum NormalizedEvent {
    /// Partial message content (streaming delta).
    MessageDelta {
        /// The content delta.
        content: String,
        /// Optional role (usually only on first delta).
        #[serde(skip_serializing_if = "Option::is_none")]
        role: Option<String>,
    },

    /// Message completed.
    MessageComplete {
        /// Full message content.
        content: String,
        /// Message role.
        role: String,
        /// Finish reason.
        #[serde(skip_serializing_if = "Option::is_none")]
        finish_reason: Option<String>,
    },

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

    /// Tool call completed.
    ToolCallComplete {
        /// Tool call ID.
        id: String,
        /// Tool name.
        name: String,
        /// Complete arguments as JSON string.
        arguments: String,
    },

    /// Tool execution result.
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

    /// Error event.
    Error {
        /// Error message.
        message: String,
        /// Error code.
        #[serde(skip_serializing_if = "Option::is_none")]
        code: Option<String>,
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
        Self::Done { finish_reason: None }
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
            NormalizedEvent::MessageDelta { .. } => "thread.message.delta",
            NormalizedEvent::MessageComplete { .. } => "thread.message.completed",
            NormalizedEvent::ToolCallDelta { .. } => "tool_call.delta",
            NormalizedEvent::ToolCallComplete { .. } => "tool_call.complete",
            NormalizedEvent::ToolResult { .. } => "tool_result",
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
    pub fn apply_delta(&mut self, id: Option<String>, name: Option<String>, arguments: Option<String>) {
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
