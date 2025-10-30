"""Exception hierarchy for Shannon SDK."""

from typing import Any, Dict, Optional


class ShannonError(Exception):
    """Base exception for all Shannon SDK errors."""

    def __init__(
        self,
        message: str,
        code: Optional[str] = None,
        details: Optional[Dict[str, Any]] = None,
    ):
        super().__init__(message)
        self.message = message
        self.code = code
        self.details = details or {}

    def __str__(self) -> str:
        if self.code:
            return f"[{self.code}] {self.message}"
        return self.message


class ConnectionError(ShannonError):
    """Connection to Shannon service failed."""

    pass


class AuthenticationError(ShannonError):
    """Authentication failed (invalid token or credentials)."""

    pass


class TaskError(ShannonError):
    """Base exception for task-related errors."""

    pass


class TaskNotFoundError(TaskError):
    """Task does not exist."""

    pass


class TaskTimeoutError(TaskError):
    """Task exceeded timeout."""

    pass


class TaskCancelledError(TaskError):
    """Task was cancelled."""

    pass


class SessionError(ShannonError):
    """Base exception for session-related errors."""

    pass


class SessionNotFoundError(SessionError):
    """Session does not exist."""

    pass


class SessionExpiredError(SessionError):
    """Session has expired."""

    pass


class ValidationError(ShannonError):
    """Invalid parameters provided."""

    pass

class PermissionDeniedError(ShannonError):
    """Forbidden (authorization) error."""

    pass

class RateLimitError(ShannonError):
    """Too many requests / rate limited."""

    pass

class ServerError(ShannonError):
    """Upstream server error (5xx)."""

    pass

class TemplateError(ShannonError):
    """Base exception for template-related errors."""

    pass


class TemplateNotFoundError(TemplateError):
    """Template does not exist."""

    pass
