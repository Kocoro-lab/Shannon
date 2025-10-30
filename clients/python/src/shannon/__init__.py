"""Shannon SDK - Python client for Shannon multi-agent AI platform."""

__version__ = "0.2.0a1"

from shannon.client import AsyncShannonClient, ShannonClient
from shannon.models import (
    Event,
    EventType,
    PendingApproval,
    Session,
    SessionEventTurn,
    SessionHistoryItem,
    SessionSummary,
    TaskHandle,
    TaskStatus,
    TaskStatusEnum,
    TaskSummary,
    TokenUsage,
)
from shannon.errors import (
    AuthenticationError,
    ConnectionError,
    PermissionDeniedError,
    RateLimitError,
    SessionError,
    SessionExpiredError,
    SessionNotFoundError,
    ShannonError,
    ServerError,
    TaskCancelledError,
    TaskError,
    TaskNotFoundError,
    TaskTimeoutError,
    TemplateError,
    TemplateNotFoundError,
    ValidationError,
)

__all__ = [
    # Client
    "AsyncShannonClient",
    "ShannonClient",
    # Models
    "Event",
    "EventType",
    "PendingApproval",
    "Session",
    "SessionEventTurn",
    "SessionHistoryItem",
    "SessionSummary",
    "TaskHandle",
    "TaskStatus",
    "TaskStatusEnum",
    "TaskSummary",
    "TokenUsage",
    # Errors
    "AuthenticationError",
    "ConnectionError",
    "PermissionDeniedError",
    "RateLimitError",
    "SessionError",
    "SessionExpiredError",
    "SessionNotFoundError",
    "ShannonError",
    "ServerError",
    "TaskCancelledError",
    "TaskError",
    "TaskNotFoundError",
    "TaskTimeoutError",
    "TemplateError",
    "TemplateNotFoundError",
    "ValidationError",
]
