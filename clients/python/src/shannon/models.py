"""Data models for Shannon SDK."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from typing import Any, Dict, List, Optional


class EventType(str, Enum):
    """Event types emitted during task execution."""

    WORKFLOW_STARTED = "WORKFLOW_STARTED"
    WORKFLOW_COMPLETED = "WORKFLOW_COMPLETED"
    AGENT_STARTED = "AGENT_STARTED"
    AGENT_COMPLETED = "AGENT_COMPLETED"
    LLM_PROMPT = "LLM_PROMPT"
    LLM_PARTIAL = "LLM_PARTIAL"
    LLM_OUTPUT = "LLM_OUTPUT"
    TOOL_INVOKED = "TOOL_INVOKED"
    TOOL_OBSERVATION = "TOOL_OBSERVATION"
    APPROVAL_REQUESTED = "APPROVAL_REQUESTED"
    APPROVAL_DECISION = "APPROVAL_DECISION"
    DELEGATION = "DELEGATION"
    TEAM_RECRUITED = "TEAM_RECRUITED"
    ROLE_ASSIGNED = "ROLE_ASSIGNED"
    ERROR_OCCURRED = "ERROR_OCCURRED"
    ERROR_RECOVERY = "ERROR_RECOVERY"
    STATUS_UPDATE = "STATUS_UPDATE"


class TaskStatusEnum(str, Enum):
    """Task execution status."""

    QUEUED = "QUEUED"
    RUNNING = "RUNNING"
    COMPLETED = "COMPLETED"
    FAILED = "FAILED"
    CANCELLED = "CANCELLED"
    TIMEOUT = "TIMEOUT"


@dataclass
class Event:
    """Streaming event from task execution."""

    type: str
    workflow_id: str
    message: str
    timestamp: datetime
    agent_id: Optional[str] = None
    seq: int = 0
    stream_id: Optional[str] = None
    _raw_payload: Optional[bytes] = None

    @property
    def id(self) -> str:
        """Event ID for resume (prefers stream_id over seq)."""
        return self.stream_id if self.stream_id else str(self.seq)

    @property
    def payload(self) -> Optional[Dict[str, Any]]:
        """Parse structured payload from message or raw data."""
        if self._raw_payload:
            try:
                return json.loads(self._raw_payload)
            except (json.JSONDecodeError, TypeError):
                return None
        # Try parsing message as JSON for certain event types
        if self.type in [EventType.APPROVAL_REQUESTED, EventType.TOOL_INVOKED]:
            try:
                return json.loads(self.message)
            except (json.JSONDecodeError, TypeError):
                return None
        return None


@dataclass
class TaskHandle:
    """Handle to a submitted task."""

    task_id: str
    workflow_id: str
    run_id: Optional[str] = None
    session_id: Optional[str] = None

    def __post_init__(self):
        """Store reference to client for convenience methods."""
        self._client: Optional[Any] = None

    def _set_client(self, client: Any) -> None:
        """Internal: set client reference for convenience methods."""
        self._client = client

    def wait(self, timeout: Optional[float] = None) -> TaskStatus:
        """Wait for task completion (blocking)."""
        if not self._client:
            raise RuntimeError("TaskHandle not associated with a client")
        return self._client.wait(self.task_id, timeout=timeout)

    def result(self, timeout: Optional[float] = None) -> Optional[str]:
        """Get task result (blocking)."""
        status = self.wait(timeout=timeout)
        return status.result

    def stream(self, types: Optional[List[str]] = None):
        """Stream events from this task."""
        if not self._client:
            raise RuntimeError("TaskHandle not associated with a client")
        return self._client.stream(self.workflow_id, types=types)

    def cancel(self, reason: Optional[str] = None) -> bool:
        """Cancel this task."""
        if not self._client:
            raise RuntimeError("TaskHandle not associated with a client")
        return self._client.cancel(self.task_id, reason=reason)


@dataclass
class ExecutionMetrics:
    """Task execution metrics."""

    tokens_used: int = 0
    cost_usd: float = 0.0
    duration_seconds: float = 0.0
    llm_calls: int = 0
    tool_calls: int = 0


@dataclass
class AgentTaskStatus:
    """Status of an individual agent task."""

    agent_id: str
    task_id: str
    status: str
    progress: float = 0.0
    result: Optional[str] = None
    error_message: Optional[str] = None


@dataclass
class TaskStatus:
    """Current status of a task."""

    task_id: str
    status: TaskStatusEnum
    progress: float = 0.0
    result: Optional[str] = None
    error_message: Optional[str] = None
    metrics: Optional[ExecutionMetrics] = None
    agent_statuses: List[AgentTaskStatus] = field(default_factory=list)


@dataclass
class PendingApproval:
    """Pending approval request."""

    approval_id: str
    workflow_id: str
    run_id: Optional[str]
    message: str
    requested_at: datetime
    context: Optional[Dict[str, Any]] = None


@dataclass
class ConversationMessage:
    """Message in a conversation history."""

    role: str  # "user", "assistant", "system"
    content: str
    timestamp: Optional[datetime] = None
    tokens_used: int = 0


@dataclass
class Session:
    """Session for multi-turn conversations."""

    session_id: str
    user_id: str
    created_at: datetime
    updated_at: datetime
    history: List[ConversationMessage] = field(default_factory=list)
    persistent_context: Dict[str, Any] = field(default_factory=dict)
    files_created: List[str] = field(default_factory=list)
    tools_used: List[str] = field(default_factory=list)
    total_tokens_used: int = 0
    total_cost_usd: float = 0.0
    max_history: int = 50
    ttl_seconds: int = 3600


@dataclass
class SessionSummary:
    """Summary of a session (without full history)."""

    session_id: str
    user_id: str
    created_at: datetime
    updated_at: datetime
    message_count: int
    total_tokens_used: int
    is_active: bool


@dataclass
class TokenUsage:
    """Token usage breakdown."""

    total_tokens: int = 0
    cost_usd: float = 0.0
    prompt_tokens: int = 0
    completion_tokens: int = 0


@dataclass
class TaskSummary:
    """Summary of a task (for list operations)."""

    task_id: str
    query: str
    status: str
    mode: str
    created_at: datetime
    completed_at: Optional[datetime] = None
    total_token_usage: Optional[TokenUsage] = None


@dataclass
class SessionHistoryItem:
    """Task in session history."""

    task_id: str
    query: str
    result: Optional[str]
    status: str
    created_at: datetime
    completed_at: Optional[datetime] = None
    tokens_used: int = 0


@dataclass
class SessionEventTurn:
    """Single turn in session events (grouped by task)."""

    turn: int
    task_id: str
    user_query: str
    final_output: Optional[str]
    timestamp: datetime
    events: List[Event]
    metadata: Dict[str, Any] = field(default_factory=dict)
