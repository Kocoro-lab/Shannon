"""Shannon SDK HTTP client implementation."""

from __future__ import annotations

import asyncio
import json
from datetime import datetime
from typing import Any, AsyncIterator, Dict, Iterator, List, Optional, Union

import httpx

from shannon import errors


def _parse_timestamp(ts_str: str) -> datetime:
    """Parse ISO timestamp with variable decimal places."""
    if not ts_str:
        return datetime.now()

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
            error_msg = error_data.get("error", response.text)
        except Exception:
            error_msg = response.text or f"HTTP {response.status_code}"

        if response.status_code == 401:
            raise errors.AuthenticationError(
                error_msg, code=str(response.status_code)
            )
        elif response.status_code == 404:
            if "task" in error_msg.lower():
                raise errors.TaskNotFoundError(error_msg, code="404")
            elif "session" in error_msg.lower():
                raise errors.SessionNotFoundError(error_msg, code="404")
            else:
                raise errors.ShannonError(error_msg, code="404")
        elif response.status_code == 400:
            raise errors.ValidationError(error_msg, code="400")
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
        timeout: Optional[float] = None,
    ) -> TaskHandle:
        """
        Submit a task to Shannon.

        Args:
            query: Task query/description
            session_id: Session ID for continuity (optional)
            context: Additional context dictionary
            timeout: Request timeout in seconds

        Returns:
            TaskHandle with task_id, workflow_id

        Raises:
            ValidationError: Invalid parameters
            ConnectionError: Failed to connect to Shannon
            AuthenticationError: Authentication failed
        """
        client = await self._ensure_client()

        payload = {"query": query}
        if session_id:
            payload["session_id"] = session_id
        if context:
            payload["context"] = context

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
        timeout: Optional[float] = None,
    ) -> tuple[TaskHandle, str]:
        """
        Submit task and get stream URL in one call.

        Args:
            query: Task query/description
            session_id: Session ID for continuity
            context: Additional context
            timeout: Request timeout

        Returns:
            Tuple of (TaskHandle, stream_url)

        Raises:
            ValidationError: Invalid parameters
            ConnectionError: Failed to connect
        """
        client = await self._ensure_client()

        payload = {"query": query}
        if session_id:
            payload["session_id"] = session_id
        if context:
            payload["context"] = context

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

            # Parse result
            result = None
            if data.get("response"):
                if isinstance(data["response"], dict):
                    result = data["response"].get("result")
                else:
                    result = data["response"]

            return TaskStatus(
                task_id=data["task_id"],
                status=status,
                progress=data.get("progress", 0.0),
                result=result,
                error_message=data.get("error", ""),
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
                sessions.append(SessionSummary(
                    session_id=session_data["session_id"],
                    user_id=session_data["user_id"],
                    created_at=_parse_timestamp(session_data["created_at"]),
                    updated_at=_parse_timestamp(session_data["created_at"]),  # Use created_at if updated not present
                    message_count=session_data.get("task_count", 0),
                    total_tokens_used=session_data.get("tokens_used", 0),
                    is_active=True,  # Assume active if returned
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

            return Session(
                session_id=data["session_id"],
                user_id=data["user_id"],
                created_at=_parse_timestamp(data["created_at"]),
                updated_at=_parse_timestamp(data.get("updated_at", data["created_at"])),
                total_tokens_used=data.get("tokens_used", 0),
                total_cost_usd=data.get("cost_usd", 0.0),
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

        async for event in self._stream_sse(
            workflow_id,
            types=type_filters,
            last_event_id=last_event_id,
            reconnect=reconnect,
            max_retries=max_retries,
            traceparent=traceparent,
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
    ) -> AsyncIterator[Event]:
        """Stream events via HTTP SSE."""
        retries = 0
        last_resume_id = last_event_id

        while True:
            try:
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

                async with httpx.AsyncClient(timeout=None) as client:
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
                        event_data = []
                        event_id = None

                        async for line in response.aiter_lines():
                            if not line:
                                # Empty line = event boundary
                                if event_data:
                                    data_str = "\n".join(event_data)
                                    try:
                                        event_json = json.loads(data_str)
                                        event = self._parse_sse_event(event_json, event_id)

                                        # Update resume point
                                        if event.stream_id:
                                            last_resume_id = event.stream_id
                                        elif event.seq:
                                            last_resume_id = str(event.seq)

                                        yield event
                                    except json.JSONDecodeError:
                                        pass  # Skip malformed events

                                    event_data = []
                                    event_id = None
                                continue

                            # Parse SSE line
                            if line.startswith("id:"):
                                event_id = line[3:].strip()
                            elif line.startswith("data:"):
                                event_data.append(line[5:].strip())
                            elif line.startswith(":"):
                                # Comment, ignore
                                pass

                # Stream ended normally
                break

            except (httpx.HTTPError, errors.ConnectionError) as e:
                if not reconnect or retries >= max_retries:
                    raise errors.ConnectionError(
                        f"SSE stream failed: {str(e)}",
                        details={"http_error": str(e)},
                    )

                # Exponential backoff
                retries += 1
                wait_time = min(2**retries, 30)  # Cap at 30 seconds
                await asyncio.sleep(wait_time)

    def _parse_sse_event(self, data: Dict[str, Any], event_id: Optional[str] = None) -> Event:
        """Parse SSE event data into Event model."""
        # Parse timestamp
        ts = datetime.now()
        if "timestamp" in data:
            try:
                ts = _parse_timestamp(str(data["timestamp"]))
            except Exception:
                pass

        return Event(
            type=data.get("type", ""),
            workflow_id=data.get("workflow_id", ""),
            message=data.get("message", ""),
            agent_id=data.get("agent_id"),
            timestamp=ts,
            seq=data.get("seq", 0),
            stream_id=data.get("stream_id") or event_id,
        )

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
        self._loop: Optional[asyncio.AbstractEventLoop] = None

    def _get_loop(self) -> asyncio.AbstractEventLoop:
        """Get or create event loop."""
        if self._loop is None or self._loop.is_closed():
            try:
                self._loop = asyncio.get_event_loop()
            except RuntimeError:
                self._loop = asyncio.new_event_loop()
                asyncio.set_event_loop(self._loop)
        return self._loop

    def _run(self, coro):
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

    def close(self):
        """Close HTTP client."""
        self._run(self._async_client.close())

    def __enter__(self):
        """Context manager entry."""
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        """Context manager exit."""
        self.close()
