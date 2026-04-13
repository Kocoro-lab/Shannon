"""Shannon SDK HTTP client implementation."""

from __future__ import annotations

import asyncio
import json
from datetime import datetime
import logging
import re
from typing import Any, AsyncIterator, Dict, Iterator, List, Optional, Union, Literal
from urllib.parse import quote

import httpx

from shannon import errors

logger = logging.getLogger(__name__)
_ID_RE = re.compile(r'^[A-Za-z0-9:_\-.]{1,128}$')
MAX_QUERY_LEN = 10000


def _parse_timestamp(ts_str: str) -> datetime:
    """Parse ISO timestamp with variable decimal places."""
    if not ts_str:
        raise ValueError("empty timestamp")

    # Replace Z with +00:00
    ts_str = ts_str.replace("Z", "+00:00")

    try:
        return datetime.fromisoformat(ts_str)
    except ValueError:
        # Handle timestamps with more than 6 decimal places
        # Python's fromisoformat accepts 0-6 decimal places
        if "." in ts_str:
            # Split into parts
            if "+" in ts_str:
                date_time, tz = ts_str.rsplit("+", 1)
                tz = "+" + tz
            elif ts_str.count("-") > 2:  # Has timezone with -
                date_time, tz = ts_str.rsplit("-", 1)
                tz = "-" + tz
            else:
                date_time = ts_str
                tz = ""

            # Split datetime into date+time and microseconds
            if "." in date_time:
                base, microseconds = date_time.split(".", 1)
                # Pad or truncate to 6 digits
                if len(microseconds) < 6:
                    microseconds = microseconds.ljust(6, "0")
                elif len(microseconds) > 6:
                    microseconds = microseconds[:6]
                ts_str = f"{base}.{microseconds}{tz}"

        return datetime.fromisoformat(ts_str)


from shannon.models import (
    AgentExecution,
    AgentInfo,
    ControlState,
    DownloadedFile,
    Event,
    EventType,
    FileEntry,
    OpenAIChatChoice,
    OpenAIChatCompletion,
    OpenAIChatCompletionChunk,
    OpenAIChatDelta,
    OpenAIChatMessage,
    OpenAIModel,
    OpenAIShannonEvent,
    OpenAIShannonOptions,
    OpenAIUsage,
    ReviewRound,
    ReviewState,
    Schedule,
    ScheduleRun,
    ScheduleSummary,
    Session,
    SessionEventTurn,
    SessionHistoryItem,
    SessionSummary,
    Skill,
    SkillDetail,
    SkillVersion,
    TaskHandle,
    TaskStatus,
    TaskStatusEnum,
    TaskSummary,
    SwarmMessageResult,
    ToolDetail,
    ToolExecutionResult,
    ToolSchema,
    ToolUsage,
    TokenUsage,
)


class AsyncShannonClient:
    """Async Shannon client using HTTP Gateway API."""

    def __init__(
        self,
        base_url: str = "http://localhost:8080",
        api_key: Optional[str] = None,
        bearer_token: Optional[str] = None,
        default_timeout: float = 30.0,
    ):
        """
        Initialize Shannon async HTTP client.

        Args:
            base_url: Gateway base URL (default: http://localhost:8080)
            api_key: API key for authentication (e.g., sk_xxx)
            bearer_token: JWT bearer token (alternative to api_key)
            default_timeout: Default timeout in seconds
        """
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.bearer_token = bearer_token
        self.default_timeout = default_timeout
        self._http_client: Optional[httpx.AsyncClient] = None

    async def _ensure_client(self) -> httpx.AsyncClient:
        """Ensure HTTP client is initialized."""
        if self._http_client is None:
            self._http_client = httpx.AsyncClient(timeout=self.default_timeout)
        return self._http_client

    def _get_headers(self, extra: Optional[Dict[str, str]] = None) -> Dict[str, str]:
        """Build HTTP headers with authentication."""
        headers = {"Content-Type": "application/json"}

        if self.bearer_token:
            headers["Authorization"] = f"Bearer {self.bearer_token}"
        elif self.api_key:
            headers["X-API-Key"] = self.api_key

        if extra:
            headers.update(extra)

        return headers

    def _handle_http_error(self, response: httpx.Response) -> None:
        """Handle HTTP error responses."""
        try:
            error_data = response.json()
            error_obj = error_data.get("error", response.text)
            if isinstance(error_obj, dict):
                error_msg = error_obj.get("message", response.text)
            else:
                error_msg = error_obj
        except Exception:
            error_msg = response.text or f"HTTP {response.status_code}"

        if response.status_code == 401:
            raise errors.AuthenticationError(
                error_msg, code=str(response.status_code)
            )
        elif response.status_code == 403:
            raise errors.PermissionDeniedError(error_msg, code="403")
        elif response.status_code == 429:
            raise errors.RateLimitError(error_msg, code="429")
        elif response.status_code == 404:
            path = ""
            try:
                path = response.request.url.path
            except Exception:
                path = ""
            if "/tasks/" in path:
                raise errors.TaskNotFoundError(error_msg, code="404")
            if "/sessions/" in path:
                raise errors.SessionNotFoundError(error_msg, code="404")
            raise errors.ShannonError(error_msg, code="404")
        elif response.status_code == 400:
            raise errors.ValidationError(error_msg, code="400")
        elif 500 <= response.status_code <= 503:
            raise errors.ServerError(error_msg, code=str(response.status_code))
        else:
            raise errors.ConnectionError(
                f"HTTP {response.status_code}: {error_msg}",
                code=str(response.status_code),
            )

    # ===== Task Operations =====

    async def submit_task(
        self,
        query: str,
        *,
        session_id: Optional[str] = None,
        context: Optional[Dict[str, Any]] = None,
        idempotency_key: Optional[str] = None,
        traceparent: Optional[str] = None,
        model_tier: Optional[Literal["small", "medium", "large"]] = None,
        model_override: Optional[str] = None,
        provider_override: Optional[str] = None,
        mode: Optional[Literal["simple", "standard", "complex", "supervisor"]] = None,
        force_swarm: bool = False,
        timeout: Optional[float] = None,
    ) -> TaskHandle:
        """
        Submit a task to Shannon.

        Args:
            query: Task query/description
            session_id: Session ID for continuity (optional)
            context: Additional context dictionary
            force_swarm: Force swarm multi-agent workflow
            timeout: Request timeout in seconds

        Returns:
            TaskHandle with task_id, workflow_id

        Raises:
            ValidationError: Invalid parameters
            ConnectionError: Failed to connect to Shannon
            AuthenticationError: Authentication failed
        """
        client = await self._ensure_client()

        # Validate before sending
        if not isinstance(query, str) or not query.strip():
            raise errors.ValidationError("Query is required", code="400")
        if len(query) > MAX_QUERY_LEN:
            raise errors.ValidationError("Query too long (max 10000 chars)", code="400")
        if session_id is not None and not _ID_RE.match(session_id):
            raise errors.ValidationError("Invalid session_id format", code="400")

        payload = {"query": query}
        if session_id:
            payload["session_id"] = session_id
        if context:
            payload["context"] = context
        if force_swarm:
            payload.setdefault("context", {})["force_swarm"] = True
        if mode:
            payload["mode"] = mode
        if model_tier:
            payload["model_tier"] = model_tier
        if model_override:
            payload["model_override"] = model_override
        if provider_override:
            payload["provider_override"] = provider_override

        try:
            extra_headers: Dict[str, str] = {}
            if idempotency_key:
                extra_headers["Idempotency-Key"] = idempotency_key
            if traceparent:
                extra_headers["traceparent"] = traceparent

            response = await client.post(
                f"{self.base_url}/api/v1/tasks",
                json=payload,
                headers=self._get_headers(extra_headers),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            # Prefer header workflow ID if present
            wf_id = response.headers.get("X-Workflow-ID") or data.get("workflow_id") or data.get("task_id")
            sess_id = response.headers.get("X-Session-ID") or session_id
            handle = TaskHandle(
                task_id=data["task_id"],
                workflow_id=wf_id or data["task_id"],
                session_id=sess_id,
            )
            handle._set_client(self)
            return handle

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to submit task: {str(e)}", details={"http_error": str(e)}
            )

    async def submit_and_stream(
        self,
        query: str,
        *,
        session_id: Optional[str] = None,
        context: Optional[Dict[str, Any]] = None,
        idempotency_key: Optional[str] = None,
        traceparent: Optional[str] = None,
        model_tier: Optional[Literal["small", "medium", "large"]] = None,
        model_override: Optional[str] = None,
        provider_override: Optional[str] = None,
        mode: Optional[Literal["simple", "standard", "complex", "supervisor"]] = None,
        force_swarm: bool = False,
        timeout: Optional[float] = None,
    ) -> tuple[TaskHandle, str]:
        """
        Submit task and get stream URL in one call.

        Args:
            query: Task query/description
            session_id: Session ID for continuity
            context: Additional context
            force_swarm: Force swarm multi-agent workflow
            timeout: Request timeout

        Returns:
            Tuple of (TaskHandle, stream_url)

        Raises:
            ValidationError: Invalid parameters
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        # Validate before sending
        if not isinstance(query, str) or not query.strip():
            raise errors.ValidationError("Query is required", code="400")
        if len(query) > MAX_QUERY_LEN:
            raise errors.ValidationError("Query too long (max 10000 chars)", code="400")
        if session_id is not None and not _ID_RE.match(session_id):
            raise errors.ValidationError("Invalid session_id format", code="400")

        payload = {"query": query}
        if session_id:
            payload["session_id"] = session_id
        if context:
            payload["context"] = context
        if force_swarm:
            payload.setdefault("context", {})["force_swarm"] = True
        if mode:
            payload["mode"] = mode
        if model_tier:
            payload["model_tier"] = model_tier
        if model_override:
            payload["model_override"] = model_override
        if provider_override:
            payload["provider_override"] = provider_override

        try:
            extra_headers: Dict[str, str] = {}
            if idempotency_key:
                extra_headers["Idempotency-Key"] = idempotency_key
            if traceparent:
                extra_headers["traceparent"] = traceparent

            response = await client.post(
                f"{self.base_url}/api/v1/tasks/stream",
                json=payload,
                headers=self._get_headers(extra_headers),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code not in [200, 201]:
                self._handle_http_error(response)

            data = response.json()
            # Prefer header workflow ID if present
            wf_id = response.headers.get("X-Workflow-ID") or data.get("workflow_id") or data.get("task_id")
            sess_id = response.headers.get("X-Session-ID") or session_id
            handle = TaskHandle(
                task_id=data["task_id"],
                workflow_id=wf_id or data["task_id"],
                session_id=sess_id,
            )
            handle._set_client(self)

            stream_url = data.get("stream_url", f"{self.base_url}/api/v1/stream/sse?workflow_id={data['task_id']}")

            return handle, stream_url

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to submit task: {str(e)}", details={"http_error": str(e)}
            )

    async def get_status(
        self, task_id: str, timeout: Optional[float] = None
    ) -> TaskStatus:
        """
        Get current task status.

        Args:
            task_id: Task ID
            timeout: Request timeout in seconds

        Returns:
            TaskStatus with status, progress, result

        Raises:
            TaskNotFoundError: Task not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/tasks/{task_id}",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()

            # Parse status
            status_str = data.get("status", "").replace("TASK_STATUS_", "")
            try:
                status = TaskStatusEnum[status_str]
            except KeyError:
                status = TaskStatusEnum.FAILED

            # Parse result (check both result and response fields)
            result = None
            # Prioritize direct result field (current API format)
            if data.get("result"):
                result = data["result"]
            # Fall back to response field for backward compatibility
            elif data.get("response"):
                if isinstance(data["response"], dict):
                    result = data["response"].get("result")
                else:
                    result = data["response"]

            # Parse timestamps
            created_at = None
            if data.get("created_at"):
                try:
                    created_at = _parse_timestamp(data["created_at"])
                except (ValueError, TypeError) as e:
                    logger.warning(f"Failed to parse created_at timestamp: {e}")

            updated_at = None
            if data.get("updated_at"):
                try:
                    updated_at = _parse_timestamp(data["updated_at"])
                except (ValueError, TypeError) as e:
                    logger.warning(f"Failed to parse updated_at timestamp: {e}")

            return TaskStatus(
                task_id=data["task_id"],
                status=status,
                workflow_id=data.get("workflow_id"),
                progress=data.get("progress", 0.0),
                result=result,
                error_message=data.get("error", ""),
                created_at=created_at,
                updated_at=updated_at,
                query=data.get("query"),
                session_id=data.get("session_id"),
                mode=data.get("mode"),
                context=data.get("context"),
                model_used=data.get("model_used"),
                provider=data.get("provider"),
                usage=data.get("usage"),
                metadata=data.get("metadata"),
            )

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get task status: {str(e)}", details={"http_error": str(e)}
            )

    async def list_tasks(
        self,
        *,
        limit: int = 20,
        offset: int = 0,
        status: Optional[str] = None,
        session_id: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> tuple[List[TaskSummary], int]:
        """
        List tasks with pagination and filters.

        Args:
            limit: Number of tasks to return (1-100)
            offset: Number of tasks to skip
            status: Filter by status (QUEUED, RUNNING, COMPLETED, FAILED, etc.)
            session_id: Filter by session ID
            timeout: Request timeout

        Returns:
            Tuple of (tasks list, total_count)

        Raises:
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        params = {"limit": limit, "offset": offset}
        if status:
            params["status"] = status
        if session_id:
            params["session_id"] = session_id

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/tasks",
                params=params,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            tasks = []

            for task_data in data.get("tasks", []):
                token_usage = None
                if task_data.get("total_token_usage"):
                    tu = task_data["total_token_usage"]
                    token_usage = TokenUsage(
                        total_tokens=tu.get("total_tokens", 0),
                        cost_usd=tu.get("cost_usd", 0.0),
                        prompt_tokens=tu.get("prompt_tokens", 0),
                        completion_tokens=tu.get("completion_tokens", 0),
                    )

                tasks.append(TaskSummary(
                    task_id=task_data["task_id"],
                    query=task_data["query"],
                    status=task_data["status"],
                    mode=task_data.get("mode", ""),
                    created_at=_parse_timestamp(task_data["created_at"]),
                    completed_at=_parse_timestamp(task_data.get("completed_at")) if task_data.get("completed_at") else None,
                    total_token_usage=token_usage,
                ))

            return tasks, data.get("total_count", len(tasks))

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to list tasks: {str(e)}", details={"http_error": str(e)}
            )

    async def get_task_events(
        self, task_id: str, timeout: Optional[float] = None
    ) -> List[Event]:
        """
        Get persistent event history for a task.

        Args:
            task_id: Task ID
            timeout: Request timeout

        Returns:
            List of Event objects

        Raises:
            TaskNotFoundError: Task not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/tasks/{task_id}/events",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            events = []

            for event_data in data.get("events", []):
                events.append(Event(
                    type=event_data.get("type", ""),
                    workflow_id=event_data.get("workflow_id", task_id),
                    message=event_data.get("message", ""),
                    agent_id=event_data.get("agent_id"),
                    timestamp=_parse_timestamp(event_data["timestamp"]),
                    seq=event_data.get("seq", 0),
                    stream_id=event_data.get("stream_id"),
                ))

            return events

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get task events: {str(e)}", details={"http_error": str(e)}
            )

    async def get_task_timeline(
        self, task_id: str, timeout: Optional[float] = None
    ) -> Dict[str, Any]:
        """
        Get Temporal workflow timeline (deterministic event history).

        Args:
            task_id: Task ID (also workflow ID)
            timeout: Request timeout

        Returns:
            Timeline data dictionary

        Raises:
            TaskNotFoundError: Task not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/tasks/{task_id}/timeline",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            return response.json()

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get task timeline: {str(e)}", details={"http_error": str(e)}
            )

    async def wait(
        self, task_id: str, timeout: Optional[float] = None, poll_interval: float = 2.0
    ) -> TaskStatus:
        """
        Wait for task completion by polling status.

        Args:
            task_id: Task ID
            timeout: Maximum time to wait in seconds
            poll_interval: Time between status checks in seconds

        Returns:
            Final TaskStatus when task completes

        Raises:
            TaskTimeoutError: Task did not complete within timeout
            TaskNotFoundError: Task not found
            ConnectionError: Failed to connect
        """
        import time
        start_time = time.time()

        while True:
            status = await self.get_status(task_id, timeout=timeout)

            if status.status in [
                TaskStatusEnum.COMPLETED,
                TaskStatusEnum.FAILED,
                TaskStatusEnum.CANCELLED,
                TaskStatusEnum.TIMEOUT,
            ]:
                return status

            if timeout and (time.time() - start_time) >= timeout:
                raise errors.TaskTimeoutError(
                    f"Task {task_id} did not complete within {timeout}s",
                    code="TIMEOUT",
                )

            await asyncio.sleep(poll_interval)

    async def cancel(
        self, task_id: str, reason: Optional[str] = None, timeout: Optional[float] = None
    ) -> bool:
        """
        Cancel a running task.

        Args:
            task_id: Task ID to cancel
            reason: Optional cancellation reason
            timeout: Request timeout in seconds

        Returns:
            True if cancelled successfully

        Raises:
            TaskNotFoundError: Task not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        payload = {}
        if reason:
            payload["reason"] = reason

        try:
            response = await client.post(
                f"{self.base_url}/api/v1/tasks/{task_id}/cancel",
                json=payload,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            # Gateway returns 202 Accepted
            if response.status_code not in (200, 202):
                self._handle_http_error(response)

            data = response.json()
            return data.get("success", False)

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to cancel task: {str(e)}", details={"http_error": str(e)}
            )

    async def pause_task(
        self, task_id: str, reason: Optional[str] = None, timeout: Optional[float] = None
    ) -> bool:
        """
        Pause a running task at safe checkpoints.

        Args:
            task_id: Task ID to pause
            reason: Optional pause reason
            timeout: Request timeout in seconds

        Returns:
            True if pause request was accepted
        """
        client = await self._ensure_client()

        payload: Dict[str, Any] = {}
        if reason:
            payload["reason"] = reason

        try:
            response = await client.post(
                f"{self.base_url}/api/v1/tasks/{task_id}/pause",
                json=payload,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            # Gateway returns 202 Accepted for async operations
            if response.status_code not in (200, 202):
                self._handle_http_error(response)

            # Handle empty response body (202 Accepted may have no body)
            try:
                data = response.json()
                return data.get("success", False)
            except (json.JSONDecodeError, ValueError):
                # Empty or malformed response - default to True for successful status codes
                return True

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to pause task: {str(e)}", details={"http_error": str(e)}
            )

    async def resume_task(
        self, task_id: str, reason: Optional[str] = None, timeout: Optional[float] = None
    ) -> bool:
        """
        Resume a previously paused task.

        Args:
            task_id: Task ID to resume
            reason: Optional resume reason
            timeout: Request timeout in seconds

        Returns:
            True if resume request was accepted
        """
        client = await self._ensure_client()

        payload: Dict[str, Any] = {}
        if reason:
            payload["reason"] = reason

        try:
            response = await client.post(
                f"{self.base_url}/api/v1/tasks/{task_id}/resume",
                json=payload,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code not in (200, 202):
                self._handle_http_error(response)

            # Handle empty response body (202 Accepted may have no body)
            try:
                data = response.json()
                return data.get("success", False)
            except (json.JSONDecodeError, ValueError):
                # Empty or malformed response - default to True for successful status codes
                return True

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to resume task: {str(e)}", details={"http_error": str(e)}
            )

    async def get_control_state(
        self, task_id: str, timeout: Optional[float] = None
    ) -> ControlState:
        """
        Get current pause/cancel control state for a task.

        Args:
            task_id: Task/workflow ID
            timeout: Request timeout in seconds

        Returns:
            ControlState with pause/cancel flags and metadata
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/tasks/{task_id}/control-state",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()

            paused_at = None
            ts = data.get("paused_at")
            if ts:
                try:
                    paused_at = _parse_timestamp(ts)
                except (ValueError, TypeError) as e:
                    logger.warning(
                        f"Failed to parse paused_at timestamp '{ts}': {e}",
                        extra={"task_id": task_id, "timestamp": ts},
                    )
                    paused_at = None

            return ControlState(
                is_paused=bool(data.get("is_paused", False)),
                is_cancelled=bool(data.get("is_cancelled", False)),
                paused_at=paused_at,
                pause_reason=data.get("pause_reason"),
                paused_by=data.get("paused_by"),
                cancel_reason=data.get("cancel_reason"),
                cancelled_by=data.get("cancelled_by"),
            )

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get control state: {str(e)}",
                details={"http_error": str(e)},
            )

    async def approve(
        self,
        approval_id: str,
        workflow_id: str,
        *,
        approved: bool = True,
        feedback: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> bool:
        """
        Approve or reject a pending approval request.

        Args:
            approval_id: Approval ID
            workflow_id: Workflow ID
            approved: True to approve, False to reject
            feedback: Optional feedback message
            timeout: Request timeout in seconds

        Returns:
            True if approval was successfully recorded

        Raises:
            ValidationError: Invalid parameters
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        payload = {
            "approval_id": approval_id,
            "workflow_id": workflow_id,
            "approved": approved,
        }
        if feedback:
            payload["feedback"] = feedback

        try:
            response = await client.post(
                f"{self.base_url}/api/v1/approvals/decision",
                json=payload,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            return data.get("success", False)

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to submit approval: {str(e)}", details={"http_error": str(e)}
            )

    # ===== HITL Review Operations =====

    async def get_review_state(
        self, workflow_id: str, *, timeout: Optional[float] = None
    ) -> ReviewState:
        """
        Get the current review state for a workflow.

        Args:
            workflow_id: Workflow ID
            timeout: Request timeout in seconds

        Returns:
            ReviewState with status, round, version, and conversation rounds

        Raises:
            TaskNotFoundError: Workflow not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/tasks/{workflow_id}/review",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                if response.status_code == 404:
                    try:
                        error_data = response.json()
                        error_msg = error_data.get("error", response.text)
                    except Exception:
                        error_msg = response.text or f"HTTP 404"
                    raise errors.TaskNotFoundError(error_msg, code="404")
                self._handle_http_error(response)

            data = response.json()

            rounds = []
            for r in data.get("rounds", []):
                ts = None
                if r.get("timestamp"):
                    try:
                        ts = _parse_timestamp(r["timestamp"])
                    except (ValueError, TypeError):
                        pass
                rounds.append(ReviewRound(
                    role=r.get("role", ""),
                    message=r.get("message", ""),
                    timestamp=ts,
                ))

            return ReviewState(
                status=data.get("status", ""),
                round=data.get("round", 0),
                version=data.get("version", 0),
                current_plan=data.get("current_plan"),
                rounds=rounds,
                query=data.get("query"),
            )

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get review state: {str(e)}", details={"http_error": str(e)}
            )

    async def submit_review_feedback(
        self,
        workflow_id: str,
        message: str,
        *,
        version: Optional[int] = None,
        timeout: Optional[float] = None,
    ) -> ReviewState:
        """
        Submit feedback during a review cycle.

        Args:
            workflow_id: Workflow ID
            message: Feedback message
            version: Optional version for optimistic concurrency (If-Match header)
            timeout: Request timeout in seconds

        Returns:
            Updated ReviewState

        Raises:
            TaskNotFoundError: Workflow not found
            ValidationError: Version conflict (409)
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        payload = {"action": "feedback", "message": message}

        extra_headers: Dict[str, str] = {}
        if version is not None:
            extra_headers["If-Match"] = str(version)

        try:
            response = await client.post(
                f"{self.base_url}/api/v1/tasks/{workflow_id}/review",
                json=payload,
                headers=self._get_headers(extra_headers),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code == 409:
                try:
                    error_data = response.json()
                    error_msg = error_data.get("error", response.text)
                except Exception:
                    error_msg = response.text or "Version conflict"
                raise errors.ValidationError(error_msg, code="409")

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()

            rounds = []
            for r in data.get("rounds", []):
                ts = None
                if r.get("timestamp"):
                    try:
                        ts = _parse_timestamp(r["timestamp"])
                    except (ValueError, TypeError):
                        pass
                rounds.append(ReviewRound(
                    role=r.get("role", ""),
                    message=r.get("message", ""),
                    timestamp=ts,
                ))

            return ReviewState(
                status=data.get("status", ""),
                round=data.get("round", 0),
                version=data.get("version", 0),
                current_plan=data.get("current_plan"),
                rounds=rounds,
                query=data.get("query"),
            )

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to submit review feedback: {str(e)}",
                details={"http_error": str(e)},
            )

    async def approve_review(
        self,
        workflow_id: str,
        *,
        version: Optional[int] = None,
        timeout: Optional[float] = None,
    ) -> Dict[str, Any]:
        """
        Approve a review, allowing the workflow to proceed.

        Args:
            workflow_id: Workflow ID
            version: Optional version for optimistic concurrency (If-Match header)
            timeout: Request timeout in seconds

        Returns:
            Dict with status and message (e.g. {"status": "approved", "message": "..."})

        Raises:
            TaskNotFoundError: Workflow not found
            ValidationError: Version conflict (409)
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        payload = {"action": "approve"}

        extra_headers: Dict[str, str] = {}
        if version is not None:
            extra_headers["If-Match"] = str(version)

        try:
            response = await client.post(
                f"{self.base_url}/api/v1/tasks/{workflow_id}/review",
                json=payload,
                headers=self._get_headers(extra_headers),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code == 409:
                try:
                    error_data = response.json()
                    error_msg = error_data.get("error", response.text)
                except Exception:
                    error_msg = response.text or "Version conflict"
                raise errors.ValidationError(error_msg, code="409")

            if response.status_code != 200:
                self._handle_http_error(response)

            return response.json()

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to approve review: {str(e)}",
                details={"http_error": str(e)},
            )

    # ===== Session Management (HTTP Gateway) =====

    async def list_sessions(
        self,
        *,
        limit: int = 20,
        offset: int = 0,
        timeout: Optional[float] = None,
    ) -> tuple[List[SessionSummary], int]:
        """
        List sessions with pagination.

        Args:
            limit: Number of sessions to return (1-100)
            offset: Number of sessions to skip
            timeout: Request timeout

        Returns:
            Tuple of (sessions list, total_count)

        Raises:
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        params = {"limit": limit, "offset": offset}

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/sessions",
                params=params,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            sessions = []

            for session_data in data.get("sessions", []):
                context = session_data.get("context")
                # Parse timestamps
                created_at = _parse_timestamp(session_data["created_at"])
                updated_at = None
                if session_data.get("updated_at"):
                    try:
                        updated_at = _parse_timestamp(session_data["updated_at"])
                    except (ValueError, TypeError) as e:
                        logger.warning(f"Failed to parse session updated_at timestamp: {e}")
                        updated_at = created_at
                else:
                    updated_at = created_at

                expires_at = None
                if session_data.get("expires_at"):
                    try:
                        expires_at = _parse_timestamp(session_data["expires_at"])
                    except (ValueError, TypeError) as e:
                        logger.warning(f"Failed to parse session expires_at timestamp: {e}")

                last_activity_at = None
                if session_data.get("last_activity_at"):
                    try:
                        last_activity_at = _parse_timestamp(session_data["last_activity_at"])
                    except (ValueError, TypeError) as e:
                        logger.warning(f"Failed to parse session last_activity_at timestamp: {e}")

                sessions.append(SessionSummary(
                    session_id=self._resolve_session_id(session_data),
                    user_id=session_data["user_id"],
                    created_at=created_at,
                    updated_at=updated_at,
                    title=self._resolve_session_title(session_data),
                    message_count=session_data.get("task_count", 0),
                    total_tokens_used=session_data.get("tokens_used", 0),
                    token_budget=session_data.get("token_budget"),
                    expires_at=expires_at,
                    context=context,
                    last_activity_at=last_activity_at,
                    is_active=session_data.get("is_active", True),
                    successful_tasks=session_data.get("successful_tasks", 0),
                    failed_tasks=session_data.get("failed_tasks", 0),
                    success_rate=session_data.get("success_rate", 0.0),
                    total_cost_usd=session_data.get("total_cost_usd", 0.0),
                    average_cost_per_task=session_data.get("average_cost_per_task", 0.0),
                    budget_utilization=session_data.get("budget_utilization", 0.0),
                    budget_remaining=session_data.get("budget_remaining"),
                    is_near_budget_limit=session_data.get("is_near_budget_limit", False),
                    latest_task_query=session_data.get("latest_task_query"),
                    latest_task_status=session_data.get("latest_task_status"),
                    is_research_session=session_data.get("is_research_session", False),
                    first_task_mode=session_data.get("first_task_mode"),
                ))

            return sessions, data.get("total_count", len(sessions))

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to list sessions: {str(e)}", details={"http_error": str(e)}
            )

    async def get_session(
        self, session_id: str, timeout: Optional[float] = None
    ) -> Session:
        """
        Get session details.

        Args:
            session_id: Session ID (UUID or external_id)
            timeout: Request timeout

        Returns:
            Session object

        Raises:
            SessionNotFoundError: Session not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/sessions/{session_id}",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()

            # Parse timestamps
            created_at = _parse_timestamp(data["created_at"])
            updated_at = created_at
            if data.get("updated_at"):
                try:
                    updated_at = _parse_timestamp(data["updated_at"])
                except (ValueError, TypeError) as e:
                    logger.warning(f"Failed to parse session updated_at timestamp: {e}")

            expires_at = None
            if data.get("expires_at"):
                try:
                    expires_at = _parse_timestamp(data["expires_at"])
                except (ValueError, TypeError) as e:
                    logger.warning(f"Failed to parse session expires_at timestamp: {e}")

            return Session(
                session_id=self._resolve_session_id(data),
                user_id=data["user_id"],
                created_at=created_at,
                updated_at=updated_at,
                title=self._resolve_session_title(data),
                context=data.get("context"),
                total_tokens_used=data.get("tokens_used", 0),
                total_cost_usd=data.get("cost_usd", 0.0),
                token_budget=data.get("token_budget"),
                task_count=data.get("task_count", 0),
                expires_at=expires_at,
                is_research_session=data.get("is_research_session", False),
                research_strategy=data.get("research_strategy"),
            )

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get session: {str(e)}", details={"http_error": str(e)}
            )

    async def get_session_history(
        self, session_id: str, timeout: Optional[float] = None
    ) -> List[SessionHistoryItem]:
        """
        Get session conversation history (all tasks in session).

        Args:
            session_id: Session ID
            timeout: Request timeout

        Returns:
            List of SessionHistoryItem objects

        Raises:
            SessionNotFoundError: Session not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/sessions/{session_id}/history",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            history = []

            # Gateway returns tasks under "tasks" with started_at/completed_at and total_tokens
            for item in data.get("tasks", []):
                created_at_val = item.get("started_at") or item.get("created_at")
                history.append(SessionHistoryItem(
                    task_id=item["task_id"] if "task_id" in item else item.get("id", ""),
                    query=item.get("query", ""),
                    result=item.get("result"),
                    status=item.get("status", ""),
                    created_at=_parse_timestamp(created_at_val) if created_at_val else datetime.now(),
                    completed_at=_parse_timestamp(item.get("completed_at")) if item.get("completed_at") else None,
                    tokens_used=item.get("total_tokens", 0),
                ))

            return history

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get session history: {str(e)}", details={"http_error": str(e)}
            )

    async def get_session_events(
        self,
        session_id: str,
        *,
        limit: int = 10,
        offset: int = 0,
        timeout: Optional[float] = None,
    ) -> tuple[List[SessionEventTurn], int]:
        """
        Get session events grouped by turn (task).

        Args:
            session_id: Session ID
            limit: Number of turns to return (1-100)
            offset: Number of turns to skip
            timeout: Request timeout

        Returns:
            Tuple of (turns list, total_count)

        Raises:
            SessionNotFoundError: Session not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        params = {"limit": limit, "offset": offset}

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/sessions/{session_id}/events",
                params=params,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            turns = []

            for turn_data in data.get("turns", []):
                events = []
                for event_data in turn_data.get("events", []):
                    events.append(Event(
                        type=event_data.get("type", ""),
                        workflow_id=event_data.get("workflow_id", ""),
                        message=event_data.get("message", ""),
                        agent_id=event_data.get("agent_id"),
                        timestamp=_parse_timestamp(event_data["timestamp"]),
                        seq=event_data.get("seq", 0),
                        stream_id=event_data.get("stream_id"),
                    ))

                turns.append(SessionEventTurn(
                    turn=turn_data["turn"],
                    task_id=turn_data["task_id"],
                    user_query=turn_data["user_query"],
                    final_output=turn_data.get("final_output"),
                    timestamp=_parse_timestamp(turn_data["timestamp"]),
                    events=events,
                    metadata=turn_data.get("metadata", {}),
                ))

            return turns, data.get("count", len(turns))

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get session events: {str(e)}", details={"http_error": str(e)}
            )

    async def update_session_title(
        self, session_id: str, title: str, timeout: Optional[float] = None
    ) -> bool:
        """
        Update session title.

        Args:
            session_id: Session ID (UUID or external_id)
            title: New title (max 60 chars)
            timeout: Request timeout

        Returns:
            True if updated successfully

        Raises:
            SessionNotFoundError: Session not found
            ValidationError: Invalid title
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        payload = {"title": title}

        try:
            response = await client.patch(
                f"{self.base_url}/api/v1/sessions/{session_id}",
                json=payload,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            return True

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to update session title: {str(e)}", details={"http_error": str(e)}
            )

    async def delete_session(
        self, session_id: str, timeout: Optional[float] = None
    ) -> bool:
        """
        Delete a session (soft delete).

        Args:
            session_id: Session ID
            timeout: Request timeout

        Returns:
            True if deleted successfully

        Raises:
            SessionNotFoundError: Session not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        try:
            response = await client.delete(
                f"{self.base_url}/api/v1/sessions/{session_id}",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            # 204 No Content is success
            if response.status_code not in [200, 204]:
                self._handle_http_error(response)

            return True

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to delete session: {str(e)}", details={"http_error": str(e)}
            )

    # ===== File Access =====

    async def list_session_files(
        self,
        session_id: str,
        *,
        path: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> List[FileEntry]:
        """
        List files in a session workspace.

        Args:
            session_id: Session ID
            path: Optional workspace subdirectory
            timeout: Request timeout in seconds

        Returns:
            List of FileEntry objects
        """
        client = await self._ensure_client()

        params: Dict[str, Any] = {}
        if path:
            params["path"] = path

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/sessions/{session_id}/files",
                params=params,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            if not data.get("success", True):
                raise errors.ShannonError(data.get("error", "Failed to list session files"))

            files = []
            for item in data.get("files", []):
                files.append(FileEntry(
                    name=item.get("name", ""),
                    path=item.get("path", ""),
                    is_dir=item.get("is_dir", False),
                    size_bytes=item.get("size_bytes", 0),
                ))

            return files

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to list session files: {str(e)}",
                details={"http_error": str(e)},
            )

    async def download_session_file(
        self,
        session_id: str,
        path: str,
        *,
        timeout: Optional[float] = None,
    ) -> DownloadedFile:
        """
        Download a file from a session workspace.

        Text files are returned as plain content. Binary files are base64-encoded
        by the gateway and returned as-is in the content field.
        """
        client = await self._ensure_client()
        encoded_path = quote(path, safe="/")

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/sessions/{session_id}/files/{encoded_path}",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            if not data.get("success", True):
                raise errors.ShannonError(data.get("error", "Failed to download session file"))

            content = data.get("content")
            if content is None:
                raise errors.ShannonError("Session file response missing content")

            return DownloadedFile(
                content=content,
                content_type=data.get("content_type"),
                size_bytes=data.get("size_bytes"),
            )

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to download session file: {str(e)}",
                details={"http_error": str(e)},
            )

    async def list_memory_files(
        self, *, timeout: Optional[float] = None
    ) -> List[FileEntry]:
        """
        List files in the authenticated user's memory directory.
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/memory/files",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            if not data.get("success", True):
                raise errors.ShannonError(data.get("error", "Failed to list memory files"))

            files = []
            for item in data.get("files", []):
                files.append(FileEntry(
                    name=item.get("name", ""),
                    path=item.get("path", ""),
                    is_dir=item.get("is_dir", False),
                    size_bytes=item.get("size_bytes", 0),
                ))

            return files

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to list memory files: {str(e)}",
                details={"http_error": str(e)},
            )

    async def download_memory_file(
        self, path: str, *, timeout: Optional[float] = None
    ) -> DownloadedFile:
        """
        Download a file from the authenticated user's memory directory.

        Text files are returned as plain content. Binary files are base64-encoded
        by the gateway and returned as-is in the content field.
        """
        client = await self._ensure_client()
        encoded_path = quote(path, safe="/")

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/memory/files/{encoded_path}",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            if not data.get("success", True):
                raise errors.ShannonError(data.get("error", "Failed to download memory file"))

            content = data.get("content")
            if content is None:
                raise errors.ShannonError("Memory file response missing content")

            return DownloadedFile(
                content=content,
                content_type=data.get("content_type"),
                size_bytes=data.get("size_bytes"),
            )

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to download memory file: {str(e)}",
                details={"http_error": str(e)},
            )

    # ===== Agents =====

    async def list_agents(
        self, *, timeout: Optional[float] = None
    ) -> List[AgentInfo]:
        """
        List deterministic agents exposed by the gateway.
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/agents",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            agents = []
            for item in data.get("agents", []):
                agents.append(AgentInfo(
                    id=item.get("id", ""),
                    name=item.get("name", ""),
                    description=item.get("description", ""),
                    category=item.get("category", ""),
                    tool=item.get("tool", ""),
                    input_schema=item.get("input_schema", {}) or {},
                    cost_per_call=item.get("cost_per_call", 0.0),
                ))

            return agents

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to list agents: {str(e)}", details={"http_error": str(e)}
            )

    async def get_agent(
        self, agent_id: str, *, timeout: Optional[float] = None
    ) -> AgentInfo:
        """
        Get a deterministic agent definition.
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/agents/{agent_id}",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            return AgentInfo(
                id=data.get("id", ""),
                name=data.get("name", ""),
                description=data.get("description", ""),
                category=data.get("category", ""),
                tool=data.get("tool", ""),
                input_schema=data.get("input_schema", {}) or {},
                cost_per_call=data.get("cost_per_call", 0.0),
            )

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get agent: {str(e)}", details={"http_error": str(e)}
            )

    async def execute_agent(
        self,
        agent_id: str,
        input_data: Dict[str, Any],
        *,
        session_id: Optional[str] = None,
        stream: bool = False,
        timeout: Optional[float] = None,
    ) -> AgentExecution:
        """
        Execute a deterministic agent.
        """
        client = await self._ensure_client()

        payload: Dict[str, Any] = {"input": input_data, "stream": stream}
        if session_id:
            payload["session_id"] = session_id

        try:
            response = await client.post(
                f"{self.base_url}/api/v1/agents/{agent_id}",
                json=payload,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code not in [200, 202]:
                self._handle_http_error(response)

            data = response.json()
            task_id = data.get("task_id", "")
            workflow_id = response.headers.get("X-Workflow-ID") or data.get("workflow_id", "")
            if not task_id:
                raise errors.ShannonError("Agent execution response missing task_id")
            if not workflow_id:
                raise errors.ShannonError("Agent execution response missing workflow_id")

            created_at = None
            if data.get("created_at"):
                try:
                    created_at = _parse_timestamp(data["created_at"])
                except (ValueError, TypeError) as e:
                    logger.warning(f"Failed to parse agent created_at timestamp: {e}")

            execution = AgentExecution(
                task_id=task_id,
                workflow_id=workflow_id,
                agent_id=data.get("agent_id", agent_id),
                status=data.get("status", ""),
                created_at=created_at,
                session_id=response.headers.get("X-Session-ID") or session_id,
            )
            execution._set_client(self)
            return execution

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to execute agent: {str(e)}", details={"http_error": str(e)}
            )

    async def send_swarm_message(
        self,
        workflow_id: str,
        message: str,
        *,
        timeout: Optional[float] = None,
    ) -> SwarmMessageResult:
        """
        Send a message to a running swarm workflow.
        """
        client = await self._ensure_client()

        try:
            response = await client.post(
                f"{self.base_url}/api/v1/swarm/{workflow_id}/message",
                json={"message": message},
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            success = bool(data.get("success", False))
            if not success:
                raise errors.ShannonError(data.get("error", "Failed to send swarm message"))

            return SwarmMessageResult(success=success, status=data.get("status"))

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to send swarm message: {str(e)}", details={"http_error": str(e)}
            )

    # ===== OpenAI-Compatible API =====

    def _serialize_openai_message(
        self, message: Union[OpenAIChatMessage, Dict[str, Any]]
    ) -> Dict[str, Any]:
        """Serialize an OpenAI-compatible message payload."""
        if isinstance(message, OpenAIChatMessage):
            payload = {
                "role": message.role,
                "content": message.content,
            }
            if message.name:
                payload["name"] = message.name
            return payload

        if isinstance(message, dict):
            return dict(message)

        raise errors.ValidationError("messages must contain dicts or OpenAIChatMessage objects", code="400")

    def _serialize_openai_shannon_options(
        self,
        shannon_options: Optional[Union[OpenAIShannonOptions, Dict[str, Any]]],
    ) -> Optional[Dict[str, Any]]:
        """Serialize Shannon-specific OpenAI extensions."""
        if shannon_options is None:
            return None

        if isinstance(shannon_options, OpenAIShannonOptions):
            payload: Dict[str, Any] = {}
            if shannon_options.context:
                payload["context"] = shannon_options.context
            if shannon_options.agent:
                payload["agent"] = shannon_options.agent
            if shannon_options.agent_input:
                payload["agent_input"] = shannon_options.agent_input
            if shannon_options.role:
                payload["role"] = shannon_options.role
            if shannon_options.research_strategy:
                payload["research_strategy"] = shannon_options.research_strategy
            if shannon_options.model_tier:
                payload["model_tier"] = shannon_options.model_tier
            return payload

        if isinstance(shannon_options, dict):
            return dict(shannon_options)

        raise errors.ValidationError(
            "shannon_options must be a dict or OpenAIShannonOptions",
            code="400",
        )

    def _build_openai_chat_payload(
        self,
        messages: List[Union[OpenAIChatMessage, Dict[str, Any]]],
        *,
        model: Optional[str] = None,
        stream: bool = False,
        max_tokens: Optional[int] = None,
        temperature: Optional[float] = None,
        top_p: Optional[float] = None,
        n: Optional[int] = None,
        stop: Optional[List[str]] = None,
        presence_penalty: Optional[float] = None,
        frequency_penalty: Optional[float] = None,
        user: Optional[str] = None,
        include_usage: bool = False,
        shannon_options: Optional[Union[OpenAIShannonOptions, Dict[str, Any]]] = None,
    ) -> Dict[str, Any]:
        """Build an OpenAI-compatible chat completion payload."""
        if not messages:
            raise errors.ValidationError("messages array is required", code="400")

        payload: Dict[str, Any] = {
            "messages": [self._serialize_openai_message(message) for message in messages],
        }
        if model:
            payload["model"] = model
        if stream:
            payload["stream"] = True
        if max_tokens is not None:
            payload["max_tokens"] = max_tokens
        if temperature is not None:
            payload["temperature"] = temperature
        if top_p is not None:
            payload["top_p"] = top_p
        if n is not None:
            payload["n"] = n
        if stop:
            payload["stop"] = stop
        if presence_penalty is not None:
            payload["presence_penalty"] = presence_penalty
        if frequency_penalty is not None:
            payload["frequency_penalty"] = frequency_penalty
        if user:
            payload["user"] = user
        if stream and include_usage:
            payload["stream_options"] = {"include_usage": True}

        serialized_shannon_options = self._serialize_openai_shannon_options(shannon_options)
        if serialized_shannon_options:
            payload["shannon_options"] = serialized_shannon_options

        return payload

    def _parse_openai_usage(self, data: Optional[Dict[str, Any]]) -> Optional[OpenAIUsage]:
        """Parse OpenAI-compatible usage metadata."""
        if not data:
            return None

        return OpenAIUsage(
            prompt_tokens=data.get("prompt_tokens", 0),
            completion_tokens=data.get("completion_tokens", 0),
            total_tokens=data.get("total_tokens", 0),
        )

    def _parse_openai_chat_message(
        self, data: Optional[Dict[str, Any]]
    ) -> Optional[OpenAIChatMessage]:
        """Parse an OpenAI-compatible message object."""
        if not data:
            return None

        return OpenAIChatMessage(
            role=data.get("role", ""),
            content=data.get("content"),
            name=data.get("name"),
        )

    def _parse_openai_chat_delta(
        self, data: Optional[Dict[str, Any]]
    ) -> Optional[OpenAIChatDelta]:
        """Parse an OpenAI-compatible delta object."""
        if not data:
            return None

        return OpenAIChatDelta(
            role=data.get("role"),
            content=data.get("content"),
        )

    def _parse_openai_chat_choices(
        self, items: Optional[List[Dict[str, Any]]]
    ) -> List[OpenAIChatChoice]:
        """Parse OpenAI-compatible choice entries."""
        choices: List[OpenAIChatChoice] = []
        for item in items or []:
            choices.append(OpenAIChatChoice(
                index=item.get("index", 0),
                message=self._parse_openai_chat_message(item.get("message")),
                delta=self._parse_openai_chat_delta(item.get("delta")),
                finish_reason=item.get("finish_reason"),
            ))
        return choices

    def _parse_openai_shannon_events(
        self, items: Optional[List[Dict[str, Any]]]
    ) -> List[OpenAIShannonEvent]:
        """Parse Shannon streaming events embedded in OpenAI chunks."""
        events: List[OpenAIShannonEvent] = []
        for item in items or []:
            events.append(OpenAIShannonEvent(
                type=item.get("type", ""),
                agent_id=item.get("agent_id"),
                message=item.get("message"),
                timestamp=item.get("timestamp"),
                payload=item.get("payload", {}) or {},
            ))
        return events

    def _parse_openai_chat_completion(
        self,
        data: Dict[str, Any],
        response_headers: Optional[Dict[str, str]] = None,
    ) -> OpenAIChatCompletion:
        """Parse a non-streaming OpenAI-compatible chat completion."""
        response_headers = response_headers or {}
        return OpenAIChatCompletion(
            id=data.get("id", ""),
            object=data.get("object", "chat.completion"),
            created=data.get("created", 0),
            model=data.get("model", ""),
            choices=self._parse_openai_chat_choices(data.get("choices")),
            usage=self._parse_openai_usage(data.get("usage")),
            system_fingerprint=data.get("system_fingerprint"),
            session_id=response_headers.get("X-Session-ID"),
            shannon_session_id=response_headers.get("X-Shannon-Session-ID"),
        )

    def _parse_openai_chat_completion_chunk(
        self,
        data: Dict[str, Any],
        response_headers: Optional[Dict[str, str]] = None,
    ) -> OpenAIChatCompletionChunk:
        """Parse a streaming OpenAI-compatible chat completion chunk."""
        response_headers = response_headers or {}
        return OpenAIChatCompletionChunk(
            id=data.get("id", ""),
            object=data.get("object", "chat.completion.chunk"),
            created=data.get("created", 0),
            model=data.get("model", ""),
            choices=self._parse_openai_chat_choices(data.get("choices")),
            usage=self._parse_openai_usage(data.get("usage")),
            system_fingerprint=data.get("system_fingerprint"),
            shannon_events=self._parse_openai_shannon_events(data.get("shannon_events")),
            session_id=response_headers.get("X-Session-ID"),
            shannon_session_id=response_headers.get("X-Shannon-Session-ID"),
        )

    def _parse_openai_model(
        self,
        data: Dict[str, Any],
        *,
        description: Optional[str] = None,
    ) -> OpenAIModel:
        """Parse a model object from the OpenAI-compatible models API."""
        return OpenAIModel(
            id=data.get("id", ""),
            object=data.get("object", "model"),
            created=data.get("created", 0),
            owned_by=data.get("owned_by", "shannon"),
            description=description,
        )

    def _resolve_session_id(self, data: Dict[str, Any]) -> str:
        """Prefer external session IDs when the gateway stores UUID-backed sessions."""
        context = data.get("context")
        if isinstance(context, dict):
            external_id = context.get("external_id")
            if isinstance(external_id, str) and external_id.strip():
                return external_id
        return data.get("session_id", "")

    def _resolve_session_title(self, data: Dict[str, Any]) -> Optional[str]:
        """Read session title from the top level or from session context."""
        title = data.get("title")
        if isinstance(title, str) and title.strip():
            return title

        context = data.get("context")
        if isinstance(context, dict):
            context_title = context.get("title")
            if isinstance(context_title, str) and context_title.strip():
                return context_title

        return title

    async def _stream_sse_request(
        self,
        url: str,
        payload: Dict[str, Any],
        headers: Dict[str, str],
        *,
        timeout: Optional[float] = None,
    ) -> AsyncIterator[tuple[str, Dict[str, str]]]:
        """Send a POST request and yield SSE event payloads as strings."""
        sse_timeout = httpx.Timeout(timeout, connect=self.default_timeout) if timeout is not None else httpx.Timeout(None, connect=self.default_timeout)

        try:
            async with httpx.AsyncClient(timeout=sse_timeout) as client:
                async with client.stream("POST", url, json=payload, headers=headers) as response:
                    if response.status_code != 200:
                        await response.aread()
                        self._handle_http_error(response)

                    response_headers = dict(response.headers)
                    event_data: List[str] = []

                    async for line in response.aiter_lines():
                        if not line:
                            if event_data:
                                data_str = "\n".join(event_data)
                                if data_str == "[DONE]":
                                    return
                                yield data_str, response_headers
                                event_data = []
                            continue

                        if line.startswith("data:"):
                            event_data.append(line[5:].lstrip())

                    if event_data:
                        data_str = "\n".join(event_data)
                        if data_str != "[DONE]":
                            yield data_str, response_headers

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"OpenAI-compatible stream failed: {str(e)}",
                details={"http_error": str(e)},
            )

    async def list_openai_models(
        self, *, timeout: Optional[float] = None
    ) -> List[OpenAIModel]:
        """
        List models from the OpenAI-compatible models endpoint.
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/v1/models",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            return [self._parse_openai_model(item) for item in data.get("data", [])]

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to list OpenAI models: {str(e)}",
                details={"http_error": str(e)},
            )

    async def get_openai_model(
        self, model: str, *, timeout: Optional[float] = None
    ) -> OpenAIModel:
        """
        Get a model from the OpenAI-compatible models endpoint.
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/v1/models/{model}",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            return self._parse_openai_model(
                data,
                description=response.headers.get("X-Model-Description"),
            )

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get OpenAI model: {str(e)}",
                details={"http_error": str(e)},
            )

    async def create_chat_completion(
        self,
        messages: List[Union[OpenAIChatMessage, Dict[str, Any]]],
        *,
        model: Optional[str] = None,
        max_tokens: Optional[int] = None,
        temperature: Optional[float] = None,
        top_p: Optional[float] = None,
        n: Optional[int] = None,
        stop: Optional[List[str]] = None,
        presence_penalty: Optional[float] = None,
        frequency_penalty: Optional[float] = None,
        user: Optional[str] = None,
        shannon_options: Optional[Union[OpenAIShannonOptions, Dict[str, Any]]] = None,
        session_id: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> OpenAIChatCompletion:
        """
        Create a non-streaming chat completion via the OpenAI-compatible API.
        """
        client = await self._ensure_client()
        payload = self._build_openai_chat_payload(
            messages,
            model=model,
            max_tokens=max_tokens,
            temperature=temperature,
            top_p=top_p,
            n=n,
            stop=stop,
            presence_penalty=presence_penalty,
            frequency_penalty=frequency_penalty,
            user=user,
            shannon_options=shannon_options,
        )
        extra_headers = {"X-Session-ID": session_id} if session_id else None

        try:
            response = await client.post(
                f"{self.base_url}/v1/chat/completions",
                json=payload,
                headers=self._get_headers(extra=extra_headers),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            return self._parse_openai_chat_completion(data, dict(response.headers))

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to create chat completion: {str(e)}",
                details={"http_error": str(e)},
            )

    async def stream_chat_completion(
        self,
        messages: List[Union[OpenAIChatMessage, Dict[str, Any]]],
        *,
        model: Optional[str] = None,
        max_tokens: Optional[int] = None,
        temperature: Optional[float] = None,
        top_p: Optional[float] = None,
        n: Optional[int] = None,
        stop: Optional[List[str]] = None,
        presence_penalty: Optional[float] = None,
        frequency_penalty: Optional[float] = None,
        user: Optional[str] = None,
        include_usage: bool = False,
        shannon_options: Optional[Union[OpenAIShannonOptions, Dict[str, Any]]] = None,
        session_id: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> AsyncIterator[OpenAIChatCompletionChunk]:
        """
        Stream chat completion chunks from the OpenAI-compatible API.
        """
        payload = self._build_openai_chat_payload(
            messages,
            model=model,
            stream=True,
            max_tokens=max_tokens,
            temperature=temperature,
            top_p=top_p,
            n=n,
            stop=stop,
            presence_penalty=presence_penalty,
            frequency_penalty=frequency_penalty,
            user=user,
            include_usage=include_usage,
            shannon_options=shannon_options,
        )
        extra_headers = {"X-Session-ID": session_id} if session_id else None

        async for data_str, response_headers in self._stream_sse_request(
            f"{self.base_url}/v1/chat/completions",
            payload,
            self._get_headers(extra=extra_headers),
            timeout=timeout,
        ):
            try:
                data = json.loads(data_str)
            except json.JSONDecodeError as e:
                raise errors.ShannonError(
                    f"Malformed OpenAI chat completion chunk: {e}",
                ) from e

            yield self._parse_openai_chat_completion_chunk(data, response_headers)

    async def create_completion(
        self,
        payload: Dict[str, Any],
        *,
        timeout: Optional[float] = None,
    ) -> Dict[str, Any]:
        """
        Call the thin OpenAI-compatible /v1/completions proxy directly.
        """
        if not isinstance(payload, dict):
            raise errors.ValidationError("payload must be a JSON object", code="400")

        client = await self._ensure_client()

        try:
            response = await client.post(
                f"{self.base_url}/v1/completions",
                json=payload,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            return response.json()

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to create completion: {str(e)}",
                details={"http_error": str(e)},
            )

    async def stream_completion(
        self,
        payload: Dict[str, Any],
        *,
        timeout: Optional[float] = None,
    ) -> AsyncIterator[Dict[str, Any]]:
        """
        Stream raw JSON events from the thin /v1/completions proxy.
        """
        if not isinstance(payload, dict):
            raise errors.ValidationError("payload must be a JSON object", code="400")

        stream_payload = dict(payload)
        stream_payload["stream"] = True

        async for data_str, _ in self._stream_sse_request(
            f"{self.base_url}/v1/completions",
            stream_payload,
            self._get_headers(),
            timeout=timeout,
        ):
            try:
                yield json.loads(data_str)
            except json.JSONDecodeError:
                yield {"data": data_str}

    # ===== Tools =====

    async def list_tools(
        self,
        *,
        category: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> List[ToolSchema]:
        """
        List available tools for direct execution.

        Args:
            category: Optional tool category filter
            timeout: Request timeout in seconds

        Returns:
            List of tool schemas
        """
        client = await self._ensure_client()

        params: Dict[str, Any] = {}
        if category:
            params["category"] = category

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/tools",
                params=params,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            tool_items = data if isinstance(data, list) else data.get("tools", [])

            tools = []
            for item in tool_items:
                tools.append(ToolSchema(
                    name=item.get("name", ""),
                    description=item.get("description", ""),
                    parameters=item.get("parameters", {}),
                ))

            return tools

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to list tools: {str(e)}", details={"http_error": str(e)}
            )

    async def get_tool(
        self, name: str, *, timeout: Optional[float] = None
    ) -> ToolDetail:
        """
        Get direct-execution tool metadata and parameter schema.

        Args:
            name: Tool name
            timeout: Request timeout in seconds

        Returns:
            ToolDetail for the requested tool
        """
        client = await self._ensure_client()
        encoded_name = quote(name, safe="")

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/tools/{encoded_name}",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            tool = data.get("tool", data)

            return ToolDetail(
                name=tool.get("name", ""),
                description=tool.get("description", ""),
                parameters=tool.get("parameters", {}),
                category=tool.get("category"),
                version=tool.get("version"),
                timeout_seconds=tool.get("timeout_seconds"),
                cost_per_use=tool.get("cost_per_use"),
            )

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get tool: {str(e)}", details={"http_error": str(e)}
            )

    async def execute_tool(
        self,
        name: str,
        *,
        arguments: Optional[Dict[str, Any]] = None,
        session_id: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> ToolExecutionResult:
        """
        Execute a tool directly through the gateway.

        Args:
            name: Tool name
            arguments: Tool arguments
            session_id: Optional session ID for tool context
            timeout: Request timeout in seconds

        Returns:
            ToolExecutionResult with success, output, metadata, and usage
        """
        client = await self._ensure_client()
        encoded_name = quote(name, safe="")

        payload: Dict[str, Any] = {"arguments": arguments or {}}
        if session_id:
            payload["session_id"] = session_id

        try:
            response = await client.post(
                f"{self.base_url}/api/v1/tools/{encoded_name}/execute",
                json=payload,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            usage = None
            if isinstance(data.get("usage"), dict):
                usage = ToolUsage(
                    tokens=data["usage"].get("tokens", 0),
                    cost_usd=data["usage"].get("cost_usd", 0.0),
                )

            return ToolExecutionResult(
                success=bool(data.get("success", False)),
                output=data.get("output"),
                text=data.get("text"),
                error=data.get("error"),
                metadata=data.get("metadata", {}) or {},
                execution_time_ms=data.get("execution_time_ms"),
                usage=usage,
            )

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to execute tool: {str(e)}", details={"http_error": str(e)}
            )

    # ===== Skills =====

    async def list_skills(
        self,
        *,
        category: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> List[Skill]:
        """
        List available skills.

        Args:
            category: Filter by skill category
            timeout: Request timeout in seconds

        Returns:
            List of Skill objects

        Raises:
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        params: Dict[str, Any] = {}
        if category:
            params["category"] = category

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/skills",
                params=params,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            skills = []

            for s in data.get("skills") or []:
                skills.append(Skill(
                    name=s.get("name", ""),
                    version=s.get("version", ""),
                    category=s.get("category", ""),
                    description=s.get("description", ""),
                    requires_tools=s.get("requires_tools", []),
                    dangerous=s.get("dangerous", False),
                    enabled=s.get("enabled", True),
                ))

            return skills

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to list skills: {str(e)}", details={"http_error": str(e)}
            )

    async def get_skill(
        self, name: str, *, timeout: Optional[float] = None
    ) -> SkillDetail:
        """
        Get detailed information about a skill.

        Args:
            name: Skill name
            timeout: Request timeout in seconds

        Returns:
            SkillDetail object

        Raises:
            ShannonError: Skill not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/skills/{name}",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            s = data.get("skill", data)

            # Backend may return PascalCase (Go struct) or lowercase keys
            def _g(key: str, default=None):
                return s.get(key, s.get(key[0].upper() + key[1:], default))

            return SkillDetail(
                name=_g("name", ""),
                version=_g("version", ""),
                category=_g("category", ""),
                description=_g("description", ""),
                author=_g("author"),
                requires_tools=_g("requires_tools", s.get("RequiresTools", [])),
                requires_role=_g("requires_role", s.get("RequiresRole")),
                budget_max=_g("budget_max", s.get("BudgetMax")),
                dangerous=_g("dangerous", s.get("Dangerous", False)),
                enabled=_g("enabled", s.get("Enabled", True)),
                content=_g("content", s.get("Content")),
                metadata=_g("metadata", s.get("Metadata")),
            )

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get skill: {str(e)}", details={"http_error": str(e)}
            )

    async def get_skill_versions(
        self, name: str, *, timeout: Optional[float] = None
    ) -> List[SkillVersion]:
        """
        Get all versions of a skill.

        Args:
            name: Skill name
            timeout: Request timeout in seconds

        Returns:
            List of SkillVersion objects

        Raises:
            ShannonError: Skill not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/skills/{name}/versions",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            versions = []

            for v in data.get("versions", []):
                versions.append(SkillVersion(
                    name=v.get("name", ""),
                    version=v.get("version", ""),
                    category=v.get("category", ""),
                    description=v.get("description", ""),
                    requires_tools=v.get("requires_tools", []),
                    dangerous=v.get("dangerous", False),
                    enabled=v.get("enabled", True),
                ))

            return versions

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get skill versions: {str(e)}", details={"http_error": str(e)}
            )

    # ===== Schedule Management =====

    async def create_schedule(
        self,
        name: str,
        cron_expression: str,
        task_query: str,
        *,
        description: Optional[str] = None,
        timezone: Optional[str] = None,
        task_context: Optional[Dict[str, str]] = None,
        max_budget_per_run_usd: Optional[float] = None,
        timeout_seconds: Optional[int] = None,
        timeout: Optional[float] = None,
    ) -> Dict[str, Any]:
        """
        Create a new scheduled task.

        Args:
            name: Schedule name
            cron_expression: Cron expression (e.g., "0 9 * * 1-5" for weekdays at 9am)
            task_query: The query to execute on each run
            description: Optional description
            timezone: Timezone for cron (default: UTC)
            task_context: Context dict for task execution (e.g., force_research, research_strategy)
            max_budget_per_run_usd: Maximum budget per execution
            timeout_seconds: Timeout per execution
            timeout: Request timeout

        Returns:
            Dict with schedule_id, message, next_run_at

        Raises:
            ValidationError: Invalid parameters
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        payload: Dict[str, Any] = {
            "name": name,
            "cron_expression": cron_expression,
            "task_query": task_query,
        }
        if description:
            payload["description"] = description
        if timezone:
            payload["timezone"] = timezone
        if task_context:
            payload["task_context"] = task_context
        if max_budget_per_run_usd is not None:
            payload["max_budget_per_run_usd"] = max_budget_per_run_usd
        if timeout_seconds is not None:
            payload["timeout_seconds"] = timeout_seconds

        try:
            response = await client.post(
                f"{self.base_url}/api/v1/schedules",
                json=payload,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code not in (200, 201):
                self._handle_http_error(response)

            return response.json()

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to create schedule: {str(e)}", details={"http_error": str(e)}
            )

    async def get_schedule(
        self, schedule_id: str, timeout: Optional[float] = None
    ) -> Schedule:
        """
        Get schedule details.

        Args:
            schedule_id: Schedule ID
            timeout: Request timeout

        Returns:
            Schedule object

        Raises:
            ShannonError: Schedule not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/schedules/{schedule_id}",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()

            return Schedule(
                schedule_id=data.get("schedule_id") or data.get("id", ""),
                name=data.get("name", ""),
                cron_expression=data.get("cron_expression", ""),
                task_query=data.get("task_query", ""),
                user_id=data.get("user_id", ""),
                status=data.get("status", ""),
                created_at=_parse_timestamp(data["created_at"]),
                updated_at=_parse_timestamp(data.get("updated_at") or data["created_at"]),
                description=data.get("description"),
                timezone=data.get("timezone"),
                task_context=data.get("task_context"),
                max_budget_per_run_usd=data.get("max_budget_per_run_usd"),
                timeout_seconds=data.get("timeout_seconds"),
                next_run_at=_parse_timestamp(data["next_run_at"]) if data.get("next_run_at") else None,
                last_run_at=_parse_timestamp(data["last_run_at"]) if data.get("last_run_at") else None,
                total_runs=data.get("total_runs", 0),
                successful_runs=data.get("successful_runs", 0),
                failed_runs=data.get("failed_runs", 0),
                paused_at=_parse_timestamp(data["paused_at"]) if data.get("paused_at") else None,
                pause_reason=data.get("pause_reason"),
            )

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get schedule: {str(e)}", details={"http_error": str(e)}
            )

    async def list_schedules(
        self,
        *,
        page: int = 1,
        page_size: int = 50,
        status: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> tuple[List[ScheduleSummary], int]:
        """
        List schedules with pagination.

        Args:
            page: Page number (1-indexed)
            page_size: Number of schedules per page (1-100)
            status: Filter by status (ACTIVE, PAUSED)
            timeout: Request timeout

        Returns:
            Tuple of (schedules list, total_count)

        Raises:
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        params: Dict[str, Any] = {"page": page, "page_size": page_size}
        if status:
            params["status"] = status

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/schedules",
                params=params,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            schedules = []

            for sched_data in data.get("schedules", []):
                schedules.append(ScheduleSummary(
                    schedule_id=sched_data.get("schedule_id") or sched_data.get("id", ""),
                    name=sched_data.get("name", ""),
                    status=sched_data.get("status", ""),
                    cron_expression=sched_data.get("cron_expression", ""),
                    task_query=sched_data.get("task_query", ""),
                    created_at=_parse_timestamp(sched_data["created_at"]),
                    next_run_at=_parse_timestamp(sched_data["next_run_at"]) if sched_data.get("next_run_at") else None,
                    last_run_at=_parse_timestamp(sched_data["last_run_at"]) if sched_data.get("last_run_at") else None,
                    total_runs=sched_data.get("total_runs", 0),
                    successful_runs=sched_data.get("successful_runs", 0),
                    failed_runs=sched_data.get("failed_runs", 0),
                ))

            return schedules, data.get("total_count", len(schedules))

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to list schedules: {str(e)}", details={"http_error": str(e)}
            )

    async def update_schedule(
        self,
        schedule_id: str,
        *,
        name: Optional[str] = None,
        description: Optional[str] = None,
        cron_expression: Optional[str] = None,
        timezone: Optional[str] = None,
        task_query: Optional[str] = None,
        task_context: Optional[Dict[str, str]] = None,
        max_budget_per_run_usd: Optional[float] = None,
        timeout_seconds: Optional[int] = None,
        clear_task_context: bool = False,
        timeout: Optional[float] = None,
    ) -> Dict[str, Any]:
        """
        Update a schedule.

        Args:
            schedule_id: Schedule ID
            name: New name
            description: New description
            cron_expression: New cron expression
            timezone: New timezone
            task_query: New task query
            task_context: New task context (or set clear_task_context=True to clear)
            max_budget_per_run_usd: New budget limit
            timeout_seconds: New timeout
            clear_task_context: Set True to clear task_context
            timeout: Request timeout

        Returns:
            Updated schedule data

        Raises:
            ShannonError: Schedule not found
            ValidationError: Invalid parameters
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        payload: Dict[str, Any] = {}
        if name is not None:
            payload["name"] = name
        if description is not None:
            payload["description"] = description
        if cron_expression is not None:
            payload["cron_expression"] = cron_expression
        if timezone is not None:
            payload["timezone"] = timezone
        if task_query is not None:
            payload["task_query"] = task_query
        if task_context is not None:
            payload["task_context"] = task_context
        if max_budget_per_run_usd is not None:
            payload["max_budget_per_run_usd"] = max_budget_per_run_usd
        if timeout_seconds is not None:
            payload["timeout_seconds"] = timeout_seconds
        if clear_task_context:
            payload["clear_task_context"] = True

        try:
            response = await client.put(
                f"{self.base_url}/api/v1/schedules/{schedule_id}",
                json=payload,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            return response.json()

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to update schedule: {str(e)}", details={"http_error": str(e)}
            )

    async def pause_schedule(
        self, schedule_id: str, reason: Optional[str] = None, timeout: Optional[float] = None
    ) -> bool:
        """
        Pause a schedule.

        Args:
            schedule_id: Schedule ID
            reason: Optional pause reason
            timeout: Request timeout

        Returns:
            True if paused successfully

        Raises:
            ShannonError: Schedule not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        payload: Dict[str, Any] = {}
        if reason:
            payload["reason"] = reason

        try:
            response = await client.post(
                f"{self.base_url}/api/v1/schedules/{schedule_id}/pause",
                json=payload,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code not in (200, 202):
                self._handle_http_error(response)

            return True

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to pause schedule: {str(e)}", details={"http_error": str(e)}
            )

    async def resume_schedule(
        self, schedule_id: str, reason: Optional[str] = None, timeout: Optional[float] = None
    ) -> bool:
        """
        Resume a paused schedule.

        Args:
            schedule_id: Schedule ID
            reason: Optional resume reason
            timeout: Request timeout

        Returns:
            True if resumed successfully

        Raises:
            ShannonError: Schedule not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        payload: Dict[str, Any] = {}
        if reason:
            payload["reason"] = reason

        try:
            response = await client.post(
                f"{self.base_url}/api/v1/schedules/{schedule_id}/resume",
                json=payload,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code not in (200, 202):
                self._handle_http_error(response)

            return True

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to resume schedule: {str(e)}", details={"http_error": str(e)}
            )

    async def delete_schedule(
        self, schedule_id: str, timeout: Optional[float] = None
    ) -> bool:
        """
        Delete a schedule.

        Args:
            schedule_id: Schedule ID
            timeout: Request timeout

        Returns:
            True if deleted successfully

        Raises:
            ShannonError: Schedule not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        try:
            response = await client.delete(
                f"{self.base_url}/api/v1/schedules/{schedule_id}",
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code not in (200, 204):
                self._handle_http_error(response)

            return True

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to delete schedule: {str(e)}", details={"http_error": str(e)}
            )

    async def get_schedule_runs(
        self,
        schedule_id: str,
        *,
        page: int = 1,
        page_size: int = 20,
        timeout: Optional[float] = None,
    ) -> tuple[List[ScheduleRun], int]:
        """
        Get execution history for a schedule.

        Args:
            schedule_id: Schedule ID
            page: Page number (1-indexed)
            page_size: Number of runs per page (1-100)
            timeout: Request timeout

        Returns:
            Tuple of (runs list, total_count)

        Raises:
            ShannonError: Schedule not found
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        params = {"page": page, "page_size": page_size}

        try:
            response = await client.get(
                f"{self.base_url}/api/v1/schedules/{schedule_id}/runs",
                params=params,
                headers=self._get_headers(),
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            data = response.json()
            runs = []

            for run_data in data.get("runs", []):
                runs.append(ScheduleRun(
                    workflow_id=run_data.get("workflow_id", ""),
                    query=run_data.get("query", ""),
                    status=run_data.get("status", ""),
                    triggered_at=_parse_timestamp(run_data["triggered_at"]),
                    result=run_data.get("result"),
                    error_message=run_data.get("error_message"),
                    model_used=run_data.get("model_used"),
                    provider=run_data.get("provider"),
                    total_tokens=run_data.get("total_tokens", 0),
                    total_cost_usd=run_data.get("total_cost_usd", 0.0),
                    duration_ms=run_data.get("duration_ms"),
                    started_at=_parse_timestamp(run_data["started_at"]) if run_data.get("started_at") else None,
                    completed_at=_parse_timestamp(run_data["completed_at"]) if run_data.get("completed_at") else None,
                ))

            return runs, data.get("total_count", len(runs))

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get schedule runs: {str(e)}", details={"http_error": str(e)}
            )

    # ===== Health & Discovery =====

    async def health(self, timeout: Optional[float] = None) -> Dict[str, Any]:
        """
        Check service health status.

        Args:
            timeout: Request timeout

        Returns:
            Health status dictionary

        Raises:
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/health",
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            return response.json()

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get health status: {str(e)}", details={"http_error": str(e)}
            )

    async def readiness(self, timeout: Optional[float] = None) -> Dict[str, Any]:
        """
        Check service readiness status.

        Args:
            timeout: Request timeout

        Returns:
            Readiness status dictionary

        Raises:
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/readiness",
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            return response.json()

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get readiness status: {str(e)}", details={"http_error": str(e)}
            )

    async def get_openapi_spec(self, timeout: Optional[float] = None) -> Dict[str, Any]:
        """
        Get OpenAPI 3.0 specification.

        Args:
            timeout: Request timeout

        Returns:
            OpenAPI spec dictionary

        Raises:
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        try:
            response = await client.get(
                f"{self.base_url}/openapi.json",
                timeout=timeout or self.default_timeout,
            )

            if response.status_code != 200:
                self._handle_http_error(response)

            return response.json()

        except httpx.HTTPError as e:
            raise errors.ConnectionError(
                f"Failed to get OpenAPI spec: {str(e)}", details={"http_error": str(e)}
            )

    # ===== Streaming (SSE and WebSocket) =====

    async def stream(
        self,
        workflow_id: str,
        *,
        types: Optional[List[Union[str, EventType]]] = None,
        last_event_id: Optional[str] = None,
        reconnect: bool = True,
        max_retries: int = 5,
        traceparent: Optional[str] = None,
        timeout: Optional[float] = None,
        total_timeout: Optional[float] = None,
    ) -> AsyncIterator[Event]:
        """
        Stream events from a workflow execution via SSE.

        Args:
            workflow_id: Workflow ID to stream
            types: Optional list of event types to filter
            last_event_id: Resume from event ID
            reconnect: Auto-reconnect on connection loss
            max_retries: Maximum reconnection attempts

        Yields:
            Event objects

        Raises:
            ConnectionError: Failed to connect after retries
        """
        # Convert EventType enums to strings
        type_filters = None
        if types:
            type_filters = [t.value if isinstance(t, EventType) else t for t in types]

        # Validate timeouts
        if timeout is not None and timeout < 0:
            raise errors.ValidationError("timeout must be >= 0", code="400")
        if total_timeout is not None and total_timeout < 0:
            raise errors.ValidationError("total_timeout must be >= 0", code="400")

        async for event in self._stream_sse(
            workflow_id,
            types=type_filters,
            last_event_id=last_event_id,
            reconnect=reconnect,
            max_retries=max_retries,
            traceparent=traceparent,
            timeout=timeout,
            total_timeout=total_timeout,
        ):
            yield event

    async def _stream_sse(
        self,
        workflow_id: str,
        *,
        types: Optional[List[str]] = None,
        last_event_id: Optional[str] = None,
        reconnect: bool = True,
        max_retries: int = 5,
        traceparent: Optional[str] = None,
        timeout: Optional[float] = None,
        total_timeout: Optional[float] = None,
    ) -> AsyncIterator[Event]:
        """Stream events via HTTP SSE."""
        import time
        start_time = time.time()
        retries = 0
        last_resume_id = last_event_id
        server_retry_ms: Optional[int] = None

        while True:
            try:
                # Check absolute timeout
                if total_timeout is not None and (time.time() - start_time) >= total_timeout:
                    raise errors.ConnectionError("SSE stream timed out", code="TIMEOUT")

                # Build query params
                params = {"workflow_id": workflow_id}
                if types:
                    params["types"] = ",".join(types)
                if last_resume_id:
                    params["last_event_id"] = last_resume_id

                # Build headers
                headers = {}
                if self.bearer_token:
                    headers["Authorization"] = f"Bearer {self.bearer_token}"
                elif self.api_key:
                    headers["X-API-Key"] = self.api_key

                if last_resume_id:
                    headers["Last-Event-ID"] = last_resume_id
                if traceparent:
                    headers["traceparent"] = traceparent

                url = f"{self.base_url}/api/v1/stream/sse"

                sse_timeout = httpx.Timeout(timeout, connect=self.default_timeout) if timeout is not None else httpx.Timeout(None, connect=self.default_timeout)
                async with httpx.AsyncClient(timeout=sse_timeout) as client:
                    async with client.stream("GET", url, params=params, headers=headers) as response:
                        # Gateway may return 404 (not found) or 400 after completion
                        if response.status_code in (404, 400):
                            break
                        if response.status_code != 200:
                            raise errors.ConnectionError(
                                f"SSE stream failed: HTTP {response.status_code}",
                                code=str(response.status_code),
                            )

                        # Parse SSE stream
                        event_data: List[str] = []
                        event_id: Optional[str] = None
                        event_name: Optional[str] = None

                        async for line in response.aiter_lines():
                            if not line:
                                # Empty line = event boundary
                                if event_data:
                                    data_str = "\n".join(event_data)
                                    if data_str == "[DONE]":
                                        yield Event(
                                            type=event_name or EventType.STREAM_END.value,
                                            workflow_id=workflow_id,
                                            message=data_str,
                                            timestamp=datetime.now(),
                                            seq=0,
                                            stream_id=event_id,
                                        )
                                        return
                                    try:
                                        event_json = json.loads(data_str)
                                        # If server provided event name and no type in JSON, use it
                                        if event_name and isinstance(event_json, dict) and not event_json.get("type"):
                                            event_json["type"] = event_name
                                        event = self._parse_sse_event(event_json, event_id)

                                        # Update resume point
                                        if event.stream_id:
                                            last_resume_id = event.stream_id
                                        elif event.seq:
                                            last_resume_id = str(event.seq)

                                        yield event
                                    except json.JSONDecodeError:
                                        logger.debug("Malformed SSE event data", extra={"data": data_str, "event_id": event_id})

                                    event_data = []
                                    event_id = None
                                    event_name = None
                                continue

                            # Parse SSE line
                            if line.startswith("id:"):
                                event_id = line[3:].strip()
                            elif line.startswith("event:"):
                                event_name = line[6:].strip()
                            elif line.startswith("retry:"):
                                try:
                                    server_retry_ms = int(line[6:].strip())
                                except ValueError:
                                    server_retry_ms = None
                            elif line.startswith("data:"):
                                event_data.append(line[5:].strip())
                            elif line.startswith(":"):
                                # Comment, ignore
                                pass

                        if event_data:
                            data_str = "\n".join(event_data)
                            if data_str == "[DONE]":
                                yield Event(
                                    type=event_name or EventType.STREAM_END.value,
                                    workflow_id=workflow_id,
                                    message=data_str,
                                    timestamp=datetime.now(),
                                    seq=0,
                                    stream_id=event_id,
                                )
                                return

                # Stream ended normally
                break

            except (httpx.HTTPError, errors.ConnectionError) as e:
                if not reconnect or retries >= max_retries:
                    raise errors.ConnectionError(
                        f"SSE stream failed: {str(e)}",
                        details={"http_error": str(e)},
                    )

                # Exponential backoff with absolute timeout check
                retries += 1
                exp_wait = min(2**retries, 30)
                if server_retry_ms is not None:
                    wait_time = max(exp_wait, min(server_retry_ms / 1000.0, 30.0))
                else:
                    wait_time = exp_wait
                if total_timeout is not None:
                    elapsed = time.time() - start_time
                    if elapsed + wait_time > total_timeout:
                        wait_time = max(0.0, total_timeout - elapsed)
                if wait_time <= 0:
                    raise errors.ConnectionError("SSE stream timed out", code="TIMEOUT")
                await asyncio.sleep(wait_time)

    def _parse_sse_event(self, data: Dict[str, Any], event_id: Optional[str] = None) -> Event:
        """Parse SSE event data into Event model."""
        # Parse timestamp
        ts = datetime.now()
        if "timestamp" in data:
            try:
                ts = _parse_timestamp(str(data["timestamp"]))
            except (ValueError, TypeError) as e:
                logger.warning(f"Failed to parse event timestamp: {e}")

        return Event(
            type=data.get("type", ""),
            workflow_id=data.get("workflow_id", ""),
            message=data.get("message", ""),
            agent_id=data.get("agent_id"),
            timestamp=ts,
            seq=data.get("seq", 0),
            stream_id=data.get("stream_id") or event_id,
        )

    async def stream_ws(
        self,
        workflow_id: str,
        *,
        types: Optional[List[Union[str, EventType]]] = None,
        last_event_id: Optional[str] = None,
        traceparent: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> AsyncIterator[Event]:
        """
        Stream events via WebSocket if the optional 'websockets' package is installed.

        Note: SSE is the preferred and default streaming mechanism. WebSocket is
        provided for environments that need WS specifically.
        """
        try:
            import websockets  # type: ignore
        except Exception:
            raise errors.ValidationError(
                "WebSocket streaming requires 'websockets'. Install with: pip install websockets",
                code="MISSING_DEP",
            )

        # Build ws url
        base = self.base_url.replace("https://", "wss://").replace("http://", "ws://")
        params: List[str] = [f"workflow_id={workflow_id}"]
        if types:
            t = [t.value if isinstance(t, EventType) else t for t in types]
            params.append("types=" + ",".join(t))
        if last_event_id:
            params.append(f"last_event_id={last_event_id}")
        qs = "&".join(params)
        uri = f"{base}/api/v1/stream/ws?{qs}"

        # Headers
        headers: List[tuple[str, str]] = []
        if self.bearer_token:
            headers.append(("Authorization", f"Bearer {self.bearer_token}"))
        elif self.api_key:
            headers.append(("X-API-Key", self.api_key))
        if traceparent:
            headers.append(("traceparent", traceparent))

        # Timeout handling: websockets.connect has 'open_timeout' and 'close_timeout'
        connect_kwargs = {}
        if timeout is not None:
            connect_kwargs["open_timeout"] = timeout

        async with websockets.connect(uri, additional_headers=headers, **connect_kwargs) as ws:
            async for message in ws:
                try:
                    data = json.loads(message)
                except Exception:
                    continue
                yield self._parse_sse_event(data)

    async def close(self):
        """Close HTTP client."""
        if self._http_client:
            await self._http_client.aclose()
            self._http_client = None

    async def __aenter__(self):
        """Async context manager entry."""
        return self

    async def __aexit__(self, exc_type, exc_val, exc_tb):
        """Async context manager exit."""
        await self.close()


class ShannonClient:
    """Synchronous wrapper around AsyncShannonClient."""

    def __init__(
        self,
        base_url: str = "http://localhost:8080",
        api_key: Optional[str] = None,
        bearer_token: Optional[str] = None,
        default_timeout: float = 30.0,
    ):
        """Initialize synchronous Shannon client."""
        self._async_client = AsyncShannonClient(
            base_url=base_url,
            api_key=api_key,
            bearer_token=bearer_token,
            default_timeout=default_timeout,
        )
        import threading
        self._loop: Optional[asyncio.AbstractEventLoop] = None
        self._loop_lock = threading.Lock()

    def _get_loop(self) -> asyncio.AbstractEventLoop:
        """Get or create event loop."""
        with self._loop_lock:
            if self._loop is None or self._loop.is_closed():
                try:
                    self._loop = asyncio.get_event_loop()
                except RuntimeError:
                    self._loop = asyncio.new_event_loop()
                    asyncio.set_event_loop(self._loop)
            return self._loop

    def _run(self, coro: Any) -> Any:
        """Run async coroutine synchronously."""
        loop = self._get_loop()
        return loop.run_until_complete(coro)

    # Task operations
    def submit_task(
        self,
        query: str,
        *,
        session_id: Optional[str] = None,
        context: Optional[Dict[str, Any]] = None,
        idempotency_key: Optional[str] = None,
        traceparent: Optional[str] = None,
        model_tier: Optional[Literal["small", "medium", "large"]] = None,
        model_override: Optional[str] = None,
        provider_override: Optional[str] = None,
        mode: Optional[Literal["simple", "standard", "complex", "supervisor"]] = None,
        force_swarm: bool = False,
        timeout: Optional[float] = None,
    ) -> TaskHandle:
        """Submit a task (blocking)."""
        handle = self._run(
            self._async_client.submit_task(
                query,
                session_id=session_id,
                context=context,
                idempotency_key=idempotency_key,
                traceparent=traceparent,
                model_tier=model_tier,
                model_override=model_override,
                provider_override=provider_override,
                mode=mode,
                force_swarm=force_swarm,
                timeout=timeout,
            )
        )
        handle._set_client(self)
        return handle

    def submit_and_stream(
        self,
        query: str,
        *,
        session_id: Optional[str] = None,
        context: Optional[Dict[str, Any]] = None,
        idempotency_key: Optional[str] = None,
        traceparent: Optional[str] = None,
        model_tier: Optional[Literal["small", "medium", "large"]] = None,
        model_override: Optional[str] = None,
        provider_override: Optional[str] = None,
        mode: Optional[Literal["simple", "standard", "complex", "supervisor"]] = None,
        force_swarm: bool = False,
        timeout: Optional[float] = None,
    ) -> tuple[TaskHandle, str]:
        """Submit task and get stream URL (blocking)."""
        handle, url = self._run(
            self._async_client.submit_and_stream(
                query,
                session_id=session_id,
                context=context,
                idempotency_key=idempotency_key,
                traceparent=traceparent,
                model_tier=model_tier,
                model_override=model_override,
                provider_override=provider_override,
                mode=mode,
                force_swarm=force_swarm,
                timeout=timeout,
            )
        )
        handle._set_client(self)
        return handle, url

    def get_status(
        self, task_id: str, timeout: Optional[float] = None
    ) -> TaskStatus:
        """Get task status (blocking)."""
        return self._run(self._async_client.get_status(task_id, timeout))

    def list_tasks(
        self,
        *,
        limit: int = 20,
        offset: int = 0,
        status: Optional[str] = None,
        session_id: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> tuple[List[TaskSummary], int]:
        """List tasks (blocking)."""
        return self._run(
            self._async_client.list_tasks(
                limit=limit, offset=offset, status=status, session_id=session_id, timeout=timeout
            )
        )

    def get_task_events(
        self, task_id: str, timeout: Optional[float] = None
    ) -> List[Event]:
        """Get task events (blocking)."""
        return self._run(self._async_client.get_task_events(task_id, timeout))

    def get_task_timeline(
        self, task_id: str, timeout: Optional[float] = None
    ) -> Dict[str, Any]:
        """Get task timeline (blocking)."""
        return self._run(self._async_client.get_task_timeline(task_id, timeout))

    def wait(
        self, task_id: str, timeout: Optional[float] = None, poll_interval: float = 2.0
    ) -> TaskStatus:
        """Wait for task completion (blocking)."""
        return self._run(
            self._async_client.wait(task_id, timeout=timeout, poll_interval=poll_interval)
        )

    def cancel(
        self, task_id: str, reason: Optional[str] = None, timeout: Optional[float] = None
    ) -> bool:
        """Cancel task (blocking)."""
        return self._run(self._async_client.cancel(task_id, reason, timeout))

    def pause_task(
        self, task_id: str, reason: Optional[str] = None, timeout: Optional[float] = None
    ) -> bool:
        """Pause task at safe checkpoints (blocking)."""
        return self._run(self._async_client.pause_task(task_id, reason, timeout))

    def resume_task(
        self, task_id: str, reason: Optional[str] = None, timeout: Optional[float] = None
    ) -> bool:
        """Resume a previously paused task (blocking)."""
        return self._run(self._async_client.resume_task(task_id, reason, timeout))

    def get_control_state(
        self, task_id: str, timeout: Optional[float] = None
    ) -> ControlState:
        """Get pause/cancel control state for a task (blocking)."""
        return self._run(self._async_client.get_control_state(task_id, timeout))

    def approve(
        self,
        approval_id: str,
        workflow_id: str,
        *,
        approved: bool = True,
        feedback: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> bool:
        """Approve task (blocking)."""
        return self._run(
            self._async_client.approve(
                approval_id, workflow_id, approved=approved, feedback=feedback, timeout=timeout
            )
        )

    # HITL Review operations
    def get_review_state(
        self, workflow_id: str, *, timeout: Optional[float] = None
    ) -> ReviewState:
        """Get review state for a workflow (blocking)."""
        return self._run(self._async_client.get_review_state(workflow_id, timeout=timeout))

    def submit_review_feedback(
        self,
        workflow_id: str,
        message: str,
        *,
        version: Optional[int] = None,
        timeout: Optional[float] = None,
    ) -> ReviewState:
        """Submit review feedback (blocking)."""
        return self._run(
            self._async_client.submit_review_feedback(
                workflow_id, message, version=version, timeout=timeout
            )
        )

    def approve_review(
        self,
        workflow_id: str,
        *,
        version: Optional[int] = None,
        timeout: Optional[float] = None,
    ) -> Dict[str, Any]:
        """Approve a review (blocking)."""
        return self._run(
            self._async_client.approve_review(workflow_id, version=version, timeout=timeout)
        )

    # Session operations
    def list_sessions(
        self,
        *,
        limit: int = 20,
        offset: int = 0,
        timeout: Optional[float] = None,
    ) -> tuple[List[SessionSummary], int]:
        """List sessions (blocking)."""
        return self._run(
            self._async_client.list_sessions(limit=limit, offset=offset, timeout=timeout)
        )

    def get_session(
        self, session_id: str, timeout: Optional[float] = None
    ) -> Session:
        """Get session details (blocking)."""
        return self._run(self._async_client.get_session(session_id, timeout))

    def get_session_history(
        self, session_id: str, timeout: Optional[float] = None
    ) -> List[SessionHistoryItem]:
        """Get session history (blocking)."""
        return self._run(self._async_client.get_session_history(session_id, timeout))

    def get_session_events(
        self,
        session_id: str,
        *,
        limit: int = 10,
        offset: int = 0,
        timeout: Optional[float] = None,
    ) -> tuple[List[SessionEventTurn], int]:
        """Get session events (blocking)."""
        return self._run(
            self._async_client.get_session_events(
                session_id, limit=limit, offset=offset, timeout=timeout
            )
        )

    def update_session_title(
        self, session_id: str, title: str, timeout: Optional[float] = None
    ) -> bool:
        """Update session title (blocking)."""
        return self._run(self._async_client.update_session_title(session_id, title, timeout))

    def delete_session(
        self, session_id: str, timeout: Optional[float] = None
    ) -> bool:
        """Delete session (blocking)."""
        return self._run(self._async_client.delete_session(session_id, timeout))

    # File operations
    def list_session_files(
        self,
        session_id: str,
        *,
        path: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> List[FileEntry]:
        """List session workspace files (blocking)."""
        return self._run(
            self._async_client.list_session_files(
                session_id,
                path=path,
                timeout=timeout,
            )
        )

    def download_session_file(
        self, session_id: str, path: str, *, timeout: Optional[float] = None
    ) -> DownloadedFile:
        """Download a session workspace file (blocking)."""
        return self._run(
            self._async_client.download_session_file(
                session_id,
                path,
                timeout=timeout,
            )
        )

    def list_memory_files(
        self, *, timeout: Optional[float] = None
    ) -> List[FileEntry]:
        """List memory files (blocking)."""
        return self._run(self._async_client.list_memory_files(timeout=timeout))

    def download_memory_file(
        self, path: str, *, timeout: Optional[float] = None
    ) -> DownloadedFile:
        """Download a memory file (blocking)."""
        return self._run(self._async_client.download_memory_file(path, timeout=timeout))

    # Agent operations
    def list_agents(
        self, *, timeout: Optional[float] = None
    ) -> List[AgentInfo]:
        """List deterministic agents (blocking)."""
        return self._run(self._async_client.list_agents(timeout=timeout))

    def get_agent(
        self, agent_id: str, *, timeout: Optional[float] = None
    ) -> AgentInfo:
        """Get deterministic agent details (blocking)."""
        return self._run(self._async_client.get_agent(agent_id, timeout=timeout))

    def execute_agent(
        self,
        agent_id: str,
        input_data: Dict[str, Any],
        *,
        session_id: Optional[str] = None,
        stream: bool = False,
        timeout: Optional[float] = None,
    ) -> AgentExecution:
        """Execute a deterministic agent (blocking)."""
        return self._run(
            self._async_client.execute_agent(
                agent_id,
                input_data,
                session_id=session_id,
                stream=stream,
                timeout=timeout,
            )
        )

    def send_swarm_message(
        self,
        workflow_id: str,
        message: str,
        *,
        timeout: Optional[float] = None,
    ) -> SwarmMessageResult:
        """Send a message to a running swarm workflow (blocking)."""
        return self._run(
            self._async_client.send_swarm_message(
                workflow_id,
                message,
                timeout=timeout,
            )
        )

    def list_openai_models(
        self, *, timeout: Optional[float] = None
    ) -> List[OpenAIModel]:
        """List models from the OpenAI-compatible models endpoint."""
        return self._run(self._async_client.list_openai_models(timeout=timeout))

    def get_openai_model(
        self, model: str, *, timeout: Optional[float] = None
    ) -> OpenAIModel:
        """Get a model from the OpenAI-compatible models endpoint."""
        return self._run(self._async_client.get_openai_model(model, timeout=timeout))

    def create_chat_completion(
        self,
        messages: List[Union[OpenAIChatMessage, Dict[str, Any]]],
        *,
        model: Optional[str] = None,
        max_tokens: Optional[int] = None,
        temperature: Optional[float] = None,
        top_p: Optional[float] = None,
        n: Optional[int] = None,
        stop: Optional[List[str]] = None,
        presence_penalty: Optional[float] = None,
        frequency_penalty: Optional[float] = None,
        user: Optional[str] = None,
        shannon_options: Optional[Union[OpenAIShannonOptions, Dict[str, Any]]] = None,
        session_id: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> OpenAIChatCompletion:
        """Create a non-streaming chat completion via the OpenAI-compatible API."""
        return self._run(
            self._async_client.create_chat_completion(
                messages,
                model=model,
                max_tokens=max_tokens,
                temperature=temperature,
                top_p=top_p,
                n=n,
                stop=stop,
                presence_penalty=presence_penalty,
                frequency_penalty=frequency_penalty,
                user=user,
                shannon_options=shannon_options,
                session_id=session_id,
                timeout=timeout,
            )
        )

    def stream_chat_completion(
        self,
        messages: List[Union[OpenAIChatMessage, Dict[str, Any]]],
        *,
        model: Optional[str] = None,
        max_tokens: Optional[int] = None,
        temperature: Optional[float] = None,
        top_p: Optional[float] = None,
        n: Optional[int] = None,
        stop: Optional[List[str]] = None,
        presence_penalty: Optional[float] = None,
        frequency_penalty: Optional[float] = None,
        user: Optional[str] = None,
        include_usage: bool = False,
        shannon_options: Optional[Union[OpenAIShannonOptions, Dict[str, Any]]] = None,
        session_id: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> Iterator[OpenAIChatCompletionChunk]:
        """Stream chat completion chunks from the OpenAI-compatible API."""
        loop = self._get_loop()

        async def _async_gen():
            async for chunk in self._async_client.stream_chat_completion(
                messages,
                model=model,
                max_tokens=max_tokens,
                temperature=temperature,
                top_p=top_p,
                n=n,
                stop=stop,
                presence_penalty=presence_penalty,
                frequency_penalty=frequency_penalty,
                user=user,
                include_usage=include_usage,
                shannon_options=shannon_options,
                session_id=session_id,
                timeout=timeout,
            ):
                yield chunk

        async_gen = _async_gen()
        try:
            while True:
                try:
                    yield loop.run_until_complete(async_gen.__anext__())
                except StopAsyncIteration:
                    break
        finally:
            loop.run_until_complete(async_gen.aclose())

    def create_completion(
        self,
        payload: Dict[str, Any],
        *,
        timeout: Optional[float] = None,
    ) -> Dict[str, Any]:
        """Call the thin OpenAI-compatible /v1/completions proxy directly."""
        return self._run(self._async_client.create_completion(payload, timeout=timeout))

    def stream_completion(
        self,
        payload: Dict[str, Any],
        *,
        timeout: Optional[float] = None,
    ) -> Iterator[Dict[str, Any]]:
        """Stream raw JSON events from the thin /v1/completions proxy."""
        loop = self._get_loop()

        async def _async_gen():
            async for item in self._async_client.stream_completion(
                payload,
                timeout=timeout,
            ):
                yield item

        async_gen = _async_gen()
        try:
            while True:
                try:
                    yield loop.run_until_complete(async_gen.__anext__())
                except StopAsyncIteration:
                    break
        finally:
            loop.run_until_complete(async_gen.aclose())

    # Tool operations
    def list_tools(
        self,
        *,
        category: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> List[ToolSchema]:
        """List direct-execution tools (blocking)."""
        return self._run(self._async_client.list_tools(category=category, timeout=timeout))

    def get_tool(
        self, name: str, *, timeout: Optional[float] = None
    ) -> ToolDetail:
        """Get direct-execution tool details (blocking)."""
        return self._run(self._async_client.get_tool(name, timeout=timeout))

    def execute_tool(
        self,
        name: str,
        *,
        arguments: Optional[Dict[str, Any]] = None,
        session_id: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> ToolExecutionResult:
        """Execute a tool directly (blocking)."""
        return self._run(
            self._async_client.execute_tool(
                name,
                arguments=arguments,
                session_id=session_id,
                timeout=timeout,
            )
        )

    # Skills operations
    def list_skills(
        self,
        *,
        category: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> List[Skill]:
        """List available skills (blocking)."""
        return self._run(self._async_client.list_skills(category=category, timeout=timeout))

    def get_skill(
        self, name: str, *, timeout: Optional[float] = None
    ) -> SkillDetail:
        """Get skill details (blocking)."""
        return self._run(self._async_client.get_skill(name, timeout=timeout))

    def get_skill_versions(
        self, name: str, *, timeout: Optional[float] = None
    ) -> List[SkillVersion]:
        """Get skill versions (blocking)."""
        return self._run(self._async_client.get_skill_versions(name, timeout=timeout))

    # Schedule operations
    def create_schedule(
        self,
        name: str,
        cron_expression: str,
        task_query: str,
        *,
        description: Optional[str] = None,
        timezone: Optional[str] = None,
        task_context: Optional[Dict[str, str]] = None,
        max_budget_per_run_usd: Optional[float] = None,
        timeout_seconds: Optional[int] = None,
        timeout: Optional[float] = None,
    ) -> Dict[str, Any]:
        """Create a scheduled task (blocking)."""
        return self._run(
            self._async_client.create_schedule(
                name,
                cron_expression,
                task_query,
                description=description,
                timezone=timezone,
                task_context=task_context,
                max_budget_per_run_usd=max_budget_per_run_usd,
                timeout_seconds=timeout_seconds,
                timeout=timeout,
            )
        )

    def get_schedule(
        self, schedule_id: str, timeout: Optional[float] = None
    ) -> Schedule:
        """Get schedule details (blocking)."""
        return self._run(self._async_client.get_schedule(schedule_id, timeout))

    def list_schedules(
        self,
        *,
        page: int = 1,
        page_size: int = 50,
        status: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> tuple[List[ScheduleSummary], int]:
        """List schedules (blocking)."""
        return self._run(
            self._async_client.list_schedules(
                page=page, page_size=page_size, status=status, timeout=timeout
            )
        )

    def update_schedule(
        self,
        schedule_id: str,
        *,
        name: Optional[str] = None,
        description: Optional[str] = None,
        cron_expression: Optional[str] = None,
        timezone: Optional[str] = None,
        task_query: Optional[str] = None,
        task_context: Optional[Dict[str, str]] = None,
        max_budget_per_run_usd: Optional[float] = None,
        timeout_seconds: Optional[int] = None,
        clear_task_context: bool = False,
        timeout: Optional[float] = None,
    ) -> Dict[str, Any]:
        """Update a schedule (blocking)."""
        return self._run(
            self._async_client.update_schedule(
                schedule_id,
                name=name,
                description=description,
                cron_expression=cron_expression,
                timezone=timezone,
                task_query=task_query,
                task_context=task_context,
                max_budget_per_run_usd=max_budget_per_run_usd,
                timeout_seconds=timeout_seconds,
                clear_task_context=clear_task_context,
                timeout=timeout,
            )
        )

    def pause_schedule(
        self, schedule_id: str, reason: Optional[str] = None, timeout: Optional[float] = None
    ) -> bool:
        """Pause a schedule (blocking)."""
        return self._run(self._async_client.pause_schedule(schedule_id, reason, timeout))

    def resume_schedule(
        self, schedule_id: str, reason: Optional[str] = None, timeout: Optional[float] = None
    ) -> bool:
        """Resume a paused schedule (blocking)."""
        return self._run(self._async_client.resume_schedule(schedule_id, reason, timeout))

    def delete_schedule(
        self, schedule_id: str, timeout: Optional[float] = None
    ) -> bool:
        """Delete a schedule (blocking)."""
        return self._run(self._async_client.delete_schedule(schedule_id, timeout))

    def get_schedule_runs(
        self,
        schedule_id: str,
        *,
        page: int = 1,
        page_size: int = 20,
        timeout: Optional[float] = None,
    ) -> tuple[List[ScheduleRun], int]:
        """Get schedule execution history (blocking)."""
        return self._run(
            self._async_client.get_schedule_runs(
                schedule_id, page=page, page_size=page_size, timeout=timeout
            )
        )

    # Health & Discovery
    def health(self, timeout: Optional[float] = None) -> Dict[str, Any]:
        """Get health status (blocking)."""
        return self._run(self._async_client.health(timeout))

    def readiness(self, timeout: Optional[float] = None) -> Dict[str, Any]:
        """Get readiness status (blocking)."""
        return self._run(self._async_client.readiness(timeout))

    def get_openapi_spec(self, timeout: Optional[float] = None) -> Dict[str, Any]:
        """Get OpenAPI spec (blocking)."""
        return self._run(self._async_client.get_openapi_spec(timeout))

    # Streaming
    def stream(
        self,
        workflow_id: str,
        *,
        types: Optional[List[Union[str, EventType]]] = None,
        last_event_id: Optional[str] = None,
        reconnect: bool = True,
        max_retries: int = 5,
        traceparent: Optional[str] = None,
        timeout: Optional[float] = None,
        total_timeout: Optional[float] = None,
    ) -> Iterator[Event]:
        """
        Stream events (blocking iterator).

        Returns synchronous iterator over events.
        """
        loop = self._get_loop()

        async def _async_gen():
            async for event in self._async_client.stream(
                workflow_id,
                types=types,
                last_event_id=last_event_id,
                reconnect=reconnect,
                max_retries=max_retries,
                traceparent=traceparent,
                timeout=timeout,
                total_timeout=total_timeout,
            ):
                yield event

        # Convert async generator to sync iterator
        async_gen = _async_gen()
        try:
            while True:
                try:
                    yield loop.run_until_complete(async_gen.__anext__())
                except StopAsyncIteration:
                    break
        finally:
            loop.run_until_complete(async_gen.aclose())

    def stream_ws(
        self,
        workflow_id: str,
        *,
        types: Optional[List[Union[str, EventType]]] = None,
        last_event_id: Optional[str] = None,
        traceparent: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> Iterator[Event]:
        """WebSocket streaming (blocking iterator). Requires 'websockets'."""
        loop = self._get_loop()

        async def _async_gen():
            async for event in self._async_client.stream_ws(
                workflow_id,
                types=types,
                last_event_id=last_event_id,
                traceparent=traceparent,
                timeout=timeout,
            ):
                yield event

        async_gen = _async_gen()
        try:
            while True:
                try:
                    yield loop.run_until_complete(async_gen.__anext__())
                except StopAsyncIteration:
                    break
        finally:
            loop.run_until_complete(async_gen.aclose())

    def close(self):
        """Close HTTP client and cleanup event loop when appropriate."""
        try:
            if self._loop is None or self._loop.is_closed():
                # Run close without persisting a new loop
                asyncio.run(self._async_client.close())
            else:
                self._run(self._async_client.close())
        except RuntimeError:
            # Fallback if asyncio.run() not allowed in current context
            self._run(self._async_client.close())

    def __enter__(self):
        """Context manager entry."""
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        """Context manager exit."""
        self.close()
