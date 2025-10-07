"""Shannon SDK client implementation."""

from __future__ import annotations

import asyncio
import json
import re
import time
from datetime import datetime
from typing import Any, AsyncIterator, Dict, Iterator, List, Optional, Union

import grpc
import httpx
from google.protobuf import struct_pb2, timestamp_pb2

from shannon import errors
from shannon.generated.common import common_pb2
from shannon.generated.orchestrator import orchestrator_pb2, orchestrator_pb2_grpc
from shannon.generated.session import session_pb2, session_pb2_grpc
from shannon.generated.orchestrator import streaming_pb2, streaming_pb2_grpc
from shannon.models import (
    Event,
    EventType,
    PendingApproval,
    Session,
    SessionSummary,
    TaskHandle,
    TaskStatus,
    TaskStatusEnum,
    ExecutionMetrics,
    AgentTaskStatus,
    ConversationMessage,
)


def _dict_to_struct(d: Optional[Dict[str, Any]]) -> struct_pb2.Struct:
    """Convert Python dict to protobuf Struct."""
    s = struct_pb2.Struct()
    if d:
        s.update(d)
    return s


def _struct_to_dict(s: struct_pb2.Struct) -> Dict[str, Any]:
    """Convert protobuf Struct to Python dict."""
    return dict(s)


def _task_status_from_proto(proto_status: int) -> TaskStatusEnum:
    """Convert proto TaskStatus to SDK enum."""
    mapping = {
        orchestrator_pb2.TASK_STATUS_QUEUED: TaskStatusEnum.QUEUED,
        orchestrator_pb2.TASK_STATUS_RUNNING: TaskStatusEnum.RUNNING,
        orchestrator_pb2.TASK_STATUS_COMPLETED: TaskStatusEnum.COMPLETED,
        orchestrator_pb2.TASK_STATUS_FAILED: TaskStatusEnum.FAILED,
        orchestrator_pb2.TASK_STATUS_CANCELLED: TaskStatusEnum.CANCELLED,
        orchestrator_pb2.TASK_STATUS_TIMEOUT: TaskStatusEnum.TIMEOUT,
    }
    return mapping.get(proto_status, TaskStatusEnum.FAILED)


class AsyncShannonClient:
    """Async Shannon client for task submission and streaming."""

    def __init__(
        self,
        grpc_endpoint: str = "localhost:50052",
        http_endpoint: str = "http://localhost:8081",
        api_key: Optional[str] = None,
        bearer_token: Optional[str] = None,
        use_tls: bool = False,
        default_timeout: float = 30.0,
    ):
        """
        Initialize Shannon async client.

        Args:
            grpc_endpoint: gRPC endpoint (default: localhost:50052)
            http_endpoint: HTTP endpoint for SSE (default: http://localhost:8081)
            api_key: API key for authentication (e.g., sk_xxx)
            bearer_token: JWT bearer token (alternative to api_key)
            use_tls: Enable TLS for gRPC
            default_timeout: Default timeout in seconds
        """
        self.grpc_endpoint = grpc_endpoint
        self.http_endpoint = http_endpoint
        self.api_key = api_key
        self.bearer_token = bearer_token
        self.use_tls = use_tls
        self.default_timeout = default_timeout

        # gRPC channel and stubs (lazy initialized)
        self._channel: Optional[grpc.aio.Channel] = None
        self._orchestrator_stub: Optional[orchestrator_pb2_grpc.OrchestratorServiceStub] = None
        self._session_stub: Optional[session_pb2_grpc.SessionServiceStub] = None
        self._streaming_stub: Optional[streaming_pb2_grpc.StreamingServiceStub] = None

    async def _ensure_channel(self) -> grpc.aio.Channel:
        """Ensure gRPC channel is initialized."""
        if self._channel is None:
            if self.use_tls:
                credentials = grpc.ssl_channel_credentials()
                self._channel = grpc.aio.secure_channel(self.grpc_endpoint, credentials)
            else:
                self._channel = grpc.aio.insecure_channel(self.grpc_endpoint)

            self._orchestrator_stub = orchestrator_pb2_grpc.OrchestratorServiceStub(
                self._channel
            )
            self._session_stub = session_pb2_grpc.SessionServiceStub(self._channel)
            self._streaming_stub = streaming_pb2_grpc.StreamingServiceStub(self._channel)

        return self._channel

    def _get_metadata(self) -> List[tuple]:
        """Get gRPC metadata for authentication."""
        metadata = []
        if self.bearer_token:
            metadata.append(("authorization", f"Bearer {self.bearer_token}"))
        elif self.api_key:
            metadata.append(("x-api-key", self.api_key))
        return metadata

    async def submit_task(
        self,
        query: str,
        *,
        session_id: Optional[str] = None,
        user_id: str = "default",
        context: Optional[Dict[str, Any]] = None,
        require_approval: bool = False,
        template_name: Optional[str] = None,
        template_version: Optional[str] = None,
        disable_ai: bool = False,
        labels: Optional[Dict[str, str]] = None,
        timeout: Optional[float] = None,
    ) -> TaskHandle:
        """
        Submit a task to Shannon.

        Args:
            query: Task query/description
            session_id: Session ID for continuity (optional)
            user_id: User ID (default: "default")
            context: Additional context dictionary
            require_approval: Require human approval before execution
            template_name: Template name (passed via metadata labels)
            template_version: Template version
            disable_ai: Disable AI processing (template-only mode)
            labels: Custom labels for workflow routing (e.g., {"workflow": "supervisor"})
            timeout: Request timeout in seconds

        Returns:
            TaskHandle with task_id, workflow_id, run_id

        Raises:
            ValidationError: Invalid parameters
            ConnectionError: Failed to connect to Shannon
            AuthenticationError: Authentication failed
        """
        await self._ensure_channel()

        # Build metadata
        # Make a shallow copy to avoid mutating the caller's dict
        label_dict = dict(labels) if labels else {}
        if template_name:
            label_dict["template"] = template_name
        if template_version:
            label_dict["template_version"] = template_version
        if disable_ai:
            label_dict["disable_ai"] = "true"

        metadata_pb = common_pb2.TaskMetadata(
            user_id=user_id,
            session_id=session_id or "",
            labels=label_dict,
        )

        # Build context
        context_dict = context or {}
        if template_name:
            context_dict["template"] = template_name
        if template_version:
            context_dict["template_version"] = template_version

        request = orchestrator_pb2.SubmitTaskRequest(
            metadata=metadata_pb,
            query=query,
            context=_dict_to_struct(context_dict),
            require_approval=require_approval,
        )

        try:
            response: orchestrator_pb2.SubmitTaskResponse = (
                await self._orchestrator_stub.SubmitTask(
                    request,
                    metadata=self._get_metadata(),
                    timeout=timeout or self.default_timeout,
                )
            )

            handle = TaskHandle(
                task_id=response.task_id,
                workflow_id=response.workflow_id,
                run_id="",  # Will be populated by workflow
            )
            handle._set_client(self)
            return handle

        except grpc.RpcError as e:
            if e.code() == grpc.StatusCode.UNAUTHENTICATED:
                raise errors.AuthenticationError(
                    "Authentication failed", code=str(e.code()), details={"grpc_error": str(e)}
                )
            elif e.code() == grpc.StatusCode.INVALID_ARGUMENT:
                raise errors.ValidationError(
                    "Invalid parameters", code=str(e.code()), details={"grpc_error": str(e)}
                )
            else:
                raise errors.ConnectionError(
                    f"Failed to submit task: {e.details()}",
                    code=str(e.code()),
                    details={"grpc_error": str(e)},
                )

    async def get_status(
        self, task_id: str, include_details: bool = True, timeout: Optional[float] = None
    ) -> TaskStatus:
        """
        Get current task status.

        Args:
            task_id: Task ID
            include_details: Include detailed metrics and agent statuses
            timeout: Request timeout in seconds

        Returns:
            TaskStatus with status, progress, result, metrics

        Raises:
            TaskNotFoundError: Task not found
            ConnectionError: Failed to connect
        """
        await self._ensure_channel()

        request = orchestrator_pb2.GetTaskStatusRequest(
            task_id=task_id, include_details=include_details
        )

        try:
            response: orchestrator_pb2.GetTaskStatusResponse = (
                await self._orchestrator_stub.GetTaskStatus(
                    request,
                    metadata=self._get_metadata(),
                    timeout=timeout or self.default_timeout,
                )
            )

            # Parse metrics
            metrics = None
            if response.HasField("metrics"):
                # Extract token usage from nested TokenUsage message
                tokens_used = 0
                cost_usd = 0.0
                if response.metrics.HasField("token_usage"):
                    tokens_used = response.metrics.token_usage.total_tokens
                    cost_usd = response.metrics.token_usage.cost_usd

                metrics = ExecutionMetrics(
                    tokens_used=tokens_used,
                    cost_usd=cost_usd,
                    duration_seconds=response.metrics.latency_ms / 1000.0,  # Convert ms to seconds
                    llm_calls=0,  # Not in proto, will be 0
                    tool_calls=0,  # Not in proto, will be 0
                )

            # Parse agent statuses
            agent_statuses = []
            for agent_status in response.agent_statuses:
                agent_statuses.append(
                    AgentTaskStatus(
                        agent_id=agent_status.agent_id,
                        task_id=agent_status.task_id,
                        status=str(agent_status.status),
                        progress=agent_status.progress,
                        result=agent_status.result if agent_status.result else None,
                        error_message=agent_status.error_message
                        if agent_status.error_message
                        else None,
                    )
                )

            return TaskStatus(
                task_id=response.task_id,
                status=_task_status_from_proto(response.status),
                progress=response.progress,
                result=response.result if response.result else None,
                error_message=response.error_message if response.error_message else None,
                metrics=metrics,
                agent_statuses=agent_statuses,
            )

        except grpc.RpcError as e:
            if e.code() == grpc.StatusCode.NOT_FOUND:
                raise errors.TaskNotFoundError(
                    f"Task {task_id} not found", code=str(e.code())
                )
            else:
                raise errors.ConnectionError(
                    f"Failed to get task status: {e.details()}",
                    code=str(e.code()),
                    details={"grpc_error": str(e)},
                )

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
        await self._ensure_channel()

        request = orchestrator_pb2.CancelTaskRequest(
            task_id=task_id, reason=reason or ""
        )

        try:
            response: orchestrator_pb2.CancelTaskResponse = (
                await self._orchestrator_stub.CancelTask(
                    request,
                    metadata=self._get_metadata(),
                    timeout=timeout or self.default_timeout,
                )
            )
            return response.success

        except grpc.RpcError as e:
            if e.code() == grpc.StatusCode.NOT_FOUND:
                raise errors.TaskNotFoundError(
                    f"Task {task_id} not found", code=str(e.code())
                )
            else:
                raise errors.ConnectionError(
                    f"Failed to cancel task: {e.details()}",
                    code=str(e.code()),
                    details={"grpc_error": str(e)},
                )

    async def approve(
        self,
        approval_id: str,
        workflow_id: str,
        *,
        run_id: Optional[str] = None,
        approved: bool = True,
        feedback: Optional[str] = None,
        modified_action: Optional[str] = None,
        approved_by: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> bool:
        """
        Approve or reject a pending approval request.

        Args:
            approval_id: Approval ID
            workflow_id: Workflow ID
            run_id: Run ID (optional, for workflow context)
            approved: True to approve, False to reject
            feedback: Optional feedback message
            modified_action: Optional modified action to execute
            approved_by: Optional approver identifier
            timeout: Request timeout in seconds

        Returns:
            True if approval was successfully recorded

        Raises:
            ValidationError: Invalid parameters
            ConnectionError: Failed to connect
        """
        await self._ensure_channel()

        request = orchestrator_pb2.ApproveTaskRequest(
            approval_id=approval_id,
            workflow_id=workflow_id,
            run_id=run_id or "",
            approved=approved,
            feedback=feedback or "",
            modified_action=modified_action or "",
            approved_by=approved_by or "",
        )

        try:
            response: orchestrator_pb2.ApproveTaskResponse = (
                await self._orchestrator_stub.ApproveTask(
                    request,
                    metadata=self._get_metadata(),
                    timeout=timeout or self.default_timeout,
                )
            )
            return response.success

        except grpc.RpcError as e:
            if e.code() == grpc.StatusCode.INVALID_ARGUMENT:
                raise errors.ValidationError(
                    f"Invalid approval request: {e.details()}",
                    code=str(e.code()),
                    details={"grpc_error": str(e)},
                )
            else:
                raise errors.ConnectionError(
                    f"Failed to submit approval: {e.details()}",
                    code=str(e.code()),
                    details={"grpc_error": str(e)},
                )

    async def get_pending_approvals(
        self,
        *,
        user_id: Optional[str] = None,
        session_id: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> List[PendingApproval]:
        """
        Get list of pending approval requests.

        Args:
            user_id: Filter by user ID (optional)
            session_id: Filter by session ID (optional)
            timeout: Request timeout in seconds

        Returns:
            List of PendingApproval objects

        Raises:
            ConnectionError: Failed to connect
        """
        await self._ensure_channel()

        request = orchestrator_pb2.GetPendingApprovalsRequest(
            user_id=user_id or "",
            session_id=session_id or "",
        )

        try:
            response: orchestrator_pb2.GetPendingApprovalsResponse = (
                await self._orchestrator_stub.GetPendingApprovals(
                    request,
                    metadata=self._get_metadata(),
                    timeout=timeout or self.default_timeout,
                )
            )

            approvals = []
            for approval in response.approvals:
                approvals.append(
                    PendingApproval(
                        approval_id=approval.approval_id,
                        workflow_id=approval.workflow_id,
                        run_id=None,  # Not in proto
                        message=f"{approval.proposed_action}: {approval.reason}",
                        requested_at=datetime.fromtimestamp(
                            approval.requested_at.seconds
                            + approval.requested_at.nanos / 1e9
                        ),
                        context=_struct_to_dict(approval.metadata)
                        if approval.HasField("metadata")
                        else None,
                    )
                )

            return approvals

        except grpc.RpcError as e:
            raise errors.ConnectionError(
                f"Failed to get pending approvals: {e.details()}",
                code=str(e.code()),
                details={"grpc_error": str(e)},
            )

    # ===== Session Management =====

    async def create_session(
        self,
        user_id: str,
        *,
        initial_context: Optional[Dict[str, Any]] = None,
        max_history: int = 50,
        ttl_seconds: int = 3600,
        timeout: Optional[float] = None,
    ) -> Session:
        """
        Create a new session for multi-turn conversations.

        Args:
            user_id: User ID
            initial_context: Initial context dictionary
            max_history: Maximum messages in history
            ttl_seconds: Session TTL in seconds
            timeout: Request timeout

        Returns:
            Session object

        Raises:
            ValidationError: Invalid parameters
            ConnectionError: Failed to connect
        """
        await self._ensure_channel()

        request = session_pb2.CreateSessionRequest(
            user_id=user_id,
            initial_context=_dict_to_struct(initial_context),
            max_history=max_history,
            ttl_seconds=ttl_seconds,
        )

        try:
            response: session_pb2.CreateSessionResponse = (
                await self._session_stub.CreateSession(
                    request,
                    metadata=self._get_metadata(),
                    timeout=timeout or self.default_timeout,
                )
            )

            return Session(
                session_id=response.session_id,
                user_id=user_id,
                created_at=datetime.fromtimestamp(
                    response.created_at.seconds + response.created_at.nanos / 1e9
                ),
                updated_at=datetime.fromtimestamp(
                    response.created_at.seconds + response.created_at.nanos / 1e9
                ),
                max_history=max_history,
                ttl_seconds=ttl_seconds,
            )

        except grpc.RpcError as e:
            raise errors.ConnectionError(
                f"Failed to create session: {e.details()}",
                code=str(e.code()),
                details={"grpc_error": str(e)},
            )

    async def get_session(
        self,
        session_id: str,
        *,
        include_history: bool = True,
        timeout: Optional[float] = None,
    ) -> Session:
        """
        Get session details.

        Args:
            session_id: Session ID
            include_history: Include message history
            timeout: Request timeout

        Returns:
            Session object

        Raises:
            SessionNotFoundError: Session not found
            ConnectionError: Failed to connect
        """
        await self._ensure_channel()

        request = session_pb2.GetSessionRequest(
            session_id=session_id,
            include_history=include_history,
        )

        try:
            response: session_pb2.GetSessionResponse = (
                await self._session_stub.GetSession(
                    request,
                    metadata=self._get_metadata(),
                    timeout=timeout or self.default_timeout,
                )
            )

            session = response.session
            history = []
            if include_history:
                for msg in session.history:
                    history.append(
                        ConversationMessage(
                            role=msg.role,
                            content=msg.content,
                            timestamp=datetime.fromtimestamp(
                                msg.timestamp.seconds + msg.timestamp.nanos / 1e9
                            )
                            if msg.HasField("timestamp")
                            else None,
                            tokens_used=msg.tokens_used,
                        )
                    )

            return Session(
                session_id=session.id,
                user_id=session.user_id,
                created_at=datetime.fromtimestamp(
                    session.created_at.seconds + session.created_at.nanos / 1e9
                ),
                updated_at=datetime.fromtimestamp(
                    session.last_active.seconds + session.last_active.nanos / 1e9
                ),
                history=history,
                persistent_context=_struct_to_dict(session.context)
                if session.HasField("context")
                else {},
                total_tokens_used=session.metrics.total_tokens
                if session.HasField("metrics")
                else 0,
                total_cost_usd=session.metrics.total_cost_usd
                if session.HasField("metrics")
                else 0.0,
            )

        except grpc.RpcError as e:
            if e.code() == grpc.StatusCode.NOT_FOUND:
                raise errors.SessionNotFoundError(
                    f"Session {session_id} not found", code=str(e.code())
                )
            else:
                raise errors.ConnectionError(
                    f"Failed to get session: {e.details()}",
                    code=str(e.code()),
                    details={"grpc_error": str(e)},
                )

    async def update_session(
        self,
        session_id: str,
        *,
        context_updates: Optional[Dict[str, Any]] = None,
        extend_ttl_seconds: Optional[int] = None,
        timeout: Optional[float] = None,
    ) -> bool:
        """
        Update session context or extend TTL.

        Args:
            session_id: Session ID
            context_updates: Context fields to update
            extend_ttl_seconds: Extend TTL by this many seconds
            timeout: Request timeout

        Returns:
            True if updated successfully

        Raises:
            SessionNotFoundError: Session not found
            ConnectionError: Failed to connect
        """
        await self._ensure_channel()

        request = session_pb2.UpdateSessionRequest(
            session_id=session_id,
            context_updates=_dict_to_struct(context_updates),
            extend_ttl_seconds=extend_ttl_seconds or 0,
        )

        try:
            response: session_pb2.UpdateSessionResponse = (
                await self._session_stub.UpdateSession(
                    request,
                    metadata=self._get_metadata(),
                    timeout=timeout or self.default_timeout,
                )
            )
            return response.success

        except grpc.RpcError as e:
            if e.code() == grpc.StatusCode.NOT_FOUND:
                raise errors.SessionNotFoundError(
                    f"Session {session_id} not found", code=str(e.code())
                )
            else:
                raise errors.ConnectionError(
                    f"Failed to update session: {e.details()}",
                    code=str(e.code()),
                    details={"grpc_error": str(e)},
                )

    async def delete_session(
        self, session_id: str, timeout: Optional[float] = None
    ) -> bool:
        """
        Delete a session.

        Args:
            session_id: Session ID
            timeout: Request timeout

        Returns:
            True if deleted successfully

        Raises:
            SessionNotFoundError: Session not found
            ConnectionError: Failed to connect
        """
        await self._ensure_channel()

        request = session_pb2.DeleteSessionRequest(session_id=session_id)

        try:
            response: session_pb2.DeleteSessionResponse = (
                await self._session_stub.DeleteSession(
                    request,
                    metadata=self._get_metadata(),
                    timeout=timeout or self.default_timeout,
                )
            )
            return response.success

        except grpc.RpcError as e:
            if e.code() == grpc.StatusCode.NOT_FOUND:
                raise errors.SessionNotFoundError(
                    f"Session {session_id} not found", code=str(e.code())
                )
            else:
                raise errors.ConnectionError(
                    f"Failed to delete session: {e.details()}",
                    code=str(e.code()),
                    details={"grpc_error": str(e)},
                )

    async def list_sessions(
        self,
        user_id: str,
        *,
        active_only: bool = True,
        limit: int = 50,
        offset: int = 0,
        timeout: Optional[float] = None,
    ) -> List[SessionSummary]:
        """
        List user sessions.

        Args:
            user_id: User ID
            active_only: Only active (non-expired) sessions
            limit: Maximum sessions to return
            offset: Offset for pagination
            timeout: Request timeout

        Returns:
            List of SessionSummary objects

        Raises:
            ConnectionError: Failed to connect
        """
        await self._ensure_channel()

        request = session_pb2.ListSessionsRequest(
            user_id=user_id,
            active_only=active_only,
            limit=limit,
            offset=offset,
        )

        try:
            response: session_pb2.ListSessionsResponse = (
                await self._session_stub.ListSessions(
                    request,
                    metadata=self._get_metadata(),
                    timeout=timeout or self.default_timeout,
                )
            )

            sessions = []
            for summary in response.sessions:
                sessions.append(
                    SessionSummary(
                        session_id=summary.id,
                        user_id=summary.user_id,
                        created_at=datetime.fromtimestamp(
                            summary.created_at.seconds + summary.created_at.nanos / 1e9
                        ),
                        updated_at=datetime.fromtimestamp(
                            summary.last_active.seconds + summary.last_active.nanos / 1e9
                        ),
                        message_count=summary.message_count,
                        total_tokens_used=summary.total_tokens,
                        is_active=summary.is_active,
                    )
                )

            return sessions

        except grpc.RpcError as e:
            raise errors.ConnectionError(
                f"Failed to list sessions: {e.details()}",
                code=str(e.code()),
                details={"grpc_error": str(e)},
            )

    async def add_message(
        self,
        session_id: str,
        role: str,
        content: str,
        *,
        metadata: Optional[Dict[str, Any]] = None,
        timeout: Optional[float] = None,
    ) -> bool:
        """
        Add message to session history.

        Args:
            session_id: Session ID
            role: Message role ("user", "assistant", "system")
            content: Message content
            metadata: Optional metadata
            timeout: Request timeout

        Returns:
            True if added successfully

        Raises:
            SessionNotFoundError: Session not found
            ConnectionError: Failed to connect
        """
        await self._ensure_channel()

        message = session_pb2.Message(
            role=role,
            content=content,
            metadata=_dict_to_struct(metadata) if metadata else None,
        )

        request = session_pb2.AddMessageRequest(
            session_id=session_id,
            message=message,
        )

        try:
            response: session_pb2.AddMessageResponse = (
                await self._session_stub.AddMessage(
                    request,
                    metadata=self._get_metadata(),
                    timeout=timeout or self.default_timeout,
                )
            )
            return response.success

        except grpc.RpcError as e:
            if e.code() == grpc.StatusCode.NOT_FOUND:
                raise errors.SessionNotFoundError(
                    f"Session {session_id} not found", code=str(e.code())
                )
            else:
                raise errors.ConnectionError(
                    f"Failed to add message: {e.details()}",
                    code=str(e.code()),
                    details={"grpc_error": str(e)},
                )

    async def clear_history(
        self,
        session_id: str,
        *,
        keep_context: bool = True,
        timeout: Optional[float] = None,
    ) -> bool:
        """
        Clear session history.

        Args:
            session_id: Session ID
            keep_context: Keep persistent context, only clear messages
            timeout: Request timeout

        Returns:
            True if cleared successfully

        Raises:
            SessionNotFoundError: Session not found
            ConnectionError: Failed to connect
        """
        await self._ensure_channel()

        request = session_pb2.ClearHistoryRequest(
            session_id=session_id,
            keep_context=keep_context,
        )

        try:
            response: session_pb2.ClearHistoryResponse = (
                await self._session_stub.ClearHistory(
                    request,
                    metadata=self._get_metadata(),
                    timeout=timeout or self.default_timeout,
                )
            )
            return response.success

        except grpc.RpcError as e:
            if e.code() == grpc.StatusCode.NOT_FOUND:
                raise errors.SessionNotFoundError(
                    f"Session {session_id} not found", code=str(e.code())
                )
            else:
                raise errors.ConnectionError(
                    f"Failed to clear history: {e.details()}",
                    code=str(e.code()),
                    details={"grpc_error": str(e)},
                )

    async def stream(
        self,
        workflow_id: str,
        *,
        types: Optional[List[Union[str, EventType]]] = None,
        last_stream_id: Optional[str] = None,
        last_event_id: Optional[Union[str, int]] = None,
        use_grpc: Optional[bool] = None,
        reconnect: bool = True,
        max_retries: int = 5,
    ) -> AsyncIterator[Event]:
        """
        Stream events from a workflow execution.

        Args:
            workflow_id: Workflow ID to stream
            types: Optional list of event types to filter (EventType enum or strings)
            last_stream_id: Resume from Redis stream ID (preferred)
            last_event_id: Resume from numeric event ID (fallback)
            use_grpc: Force transport (True=gRPC, False=SSE, None=auto-fallback)
            reconnect: Auto-reconnect on connection loss
            max_retries: Maximum reconnection attempts

        Yields:
            Event objects

        Raises:
            ConnectionError: Failed to connect after retries
            ValidationError: Invalid parameters
        """
        # Convert EventType enums to strings
        type_filters = None
        if types:
            type_filters = [t.value if isinstance(t, EventType) else t for t in types]

        # Auto-select transport with fallback
        if use_grpc is None:
            try:
                async for event in self._stream_grpc(
                    workflow_id,
                    types=type_filters,
                    last_stream_id=last_stream_id,
                    last_event_id=last_event_id,
                    reconnect=reconnect,
                    max_retries=max_retries,
                ):
                    yield event
            except (grpc.RpcError, errors.ConnectionError) as e:
                # Fall back to SSE
                async for event in self._stream_sse(
                    workflow_id,
                    types=type_filters,
                    last_stream_id=last_stream_id,
                    last_event_id=last_event_id,
                    reconnect=reconnect,
                    max_retries=max_retries,
                ):
                    yield event
        elif use_grpc:
            async for event in self._stream_grpc(
                workflow_id,
                types=type_filters,
                last_stream_id=last_stream_id,
                last_event_id=last_event_id,
                reconnect=reconnect,
                max_retries=max_retries,
            ):
                yield event
        else:
            async for event in self._stream_sse(
                workflow_id,
                types=type_filters,
                last_stream_id=last_stream_id,
                last_event_id=last_event_id,
                reconnect=reconnect,
                max_retries=max_retries,
            ):
                yield event

    async def _stream_grpc(
        self,
        workflow_id: str,
        *,
        types: Optional[List[str]] = None,
        last_stream_id: Optional[str] = None,
        last_event_id: Optional[Union[str, int]] = None,
        reconnect: bool = True,
        max_retries: int = 5,
    ) -> AsyncIterator[Event]:
        """Stream events via gRPC StreamingService."""
        await self._ensure_channel()

        retries = 0
        last_resume_id = last_stream_id
        last_resume_seq = int(last_event_id) if last_event_id else 0

        while True:
            try:
                request = streaming_pb2.StreamRequest(
                    workflow_id=workflow_id,
                    types=types or [],
                    last_stream_id=last_resume_id or "",
                    last_event_id=last_resume_seq,
                )

                stream = self._streaming_stub.StreamTaskExecution(
                    request, metadata=self._get_metadata()
                )

                async for update in stream:
                    # Convert proto TaskUpdate to Event
                    event = Event(
                        type=update.type,
                        workflow_id=update.workflow_id,
                        message=update.message,
                        agent_id=update.agent_id if update.agent_id else None,
                        timestamp=datetime.fromtimestamp(
                            update.timestamp.seconds + update.timestamp.nanos / 1e9
                        ),
                        seq=update.seq,
                        stream_id=update.stream_id if update.stream_id else None,
                    )

                    # Update resume points
                    if event.stream_id:
                        last_resume_id = event.stream_id
                    if event.seq:
                        last_resume_seq = event.seq

                    yield event

                # Stream ended normally
                break

            except grpc.RpcError as e:
                if not reconnect or retries >= max_retries:
                    raise errors.ConnectionError(
                        f"gRPC stream failed: {e.details()}",
                        code=str(e.code()),
                        details={"grpc_error": str(e)},
                    )

                # Exponential backoff
                retries += 1
                wait_time = min(2**retries, 30)  # Cap at 30 seconds
                await asyncio.sleep(wait_time)

    async def _stream_sse(
        self,
        workflow_id: str,
        *,
        types: Optional[List[str]] = None,
        last_stream_id: Optional[str] = None,
        last_event_id: Optional[Union[str, int]] = None,
        reconnect: bool = True,
        max_retries: int = 5,
    ) -> AsyncIterator[Event]:
        """Stream events via HTTP SSE."""
        retries = 0
        last_resume_id = last_stream_id or (str(last_event_id) if last_event_id else None)

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
                    headers["x-api-key"] = self.api_key

                if last_resume_id:
                    headers["Last-Event-ID"] = last_resume_id

                url = f"{self.http_endpoint}/stream/sse"

                async with httpx.AsyncClient(timeout=None) as client:
                    async with client.stream("GET", url, params=params, headers=headers) as response:
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
        # Handle ISO timestamps with optional trailing 'Z'
        ts = None
        if "timestamp" in data:
            ts_str = str(data["timestamp"]).strip()
            try:
                # Python's fromisoformat doesn't accept trailing 'Z'; convert to +00:00
                if ts_str.endswith("Z"):
                    ts_str = ts_str[:-1] + "+00:00"
                ts = datetime.fromisoformat(ts_str)
            except Exception:
                ts = None

        return Event(
            type=data.get("type", ""),
            workflow_id=data.get("workflow_id", ""),
            message=data.get("message", ""),
            agent_id=data.get("agent_id"),
            timestamp=ts or datetime.now(),
            seq=data.get("seq", 0),
            stream_id=data.get("stream_id") or event_id,
        )

    async def close(self):
        """Close gRPC channel."""
        if self._channel:
            await self._channel.close()
            self._channel = None
            self._orchestrator_stub = None
            self._session_stub = None
            self._streaming_stub = None

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
        grpc_endpoint: str = "localhost:50052",
        http_endpoint: str = "http://localhost:8081",
        api_key: Optional[str] = None,
        bearer_token: Optional[str] = None,
        use_tls: bool = False,
        default_timeout: float = 30.0,
    ):
        """Initialize synchronous Shannon client."""
        self._async_client = AsyncShannonClient(
            grpc_endpoint=grpc_endpoint,
            http_endpoint=http_endpoint,
            api_key=api_key,
            bearer_token=bearer_token,
            use_tls=use_tls,
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

    def submit_task(
        self,
        query: str,
        *,
        session_id: Optional[str] = None,
        user_id: str = "default",
        context: Optional[Dict[str, Any]] = None,
        require_approval: bool = False,
        template_name: Optional[str] = None,
        template_version: Optional[str] = None,
        disable_ai: bool = False,
        labels: Optional[Dict[str, str]] = None,
        timeout: Optional[float] = None,
    ) -> TaskHandle:
        """Submit a task (blocking). See AsyncShannonClient.submit_task for details."""
        return self._run(
            self._async_client.submit_task(
                query,
                session_id=session_id,
                user_id=user_id,
                context=context,
                require_approval=require_approval,
                template_name=template_name,
                template_version=template_version,
                disable_ai=disable_ai,
                labels=labels,
                timeout=timeout,
            )
        )

    def get_status(
        self, task_id: str, include_details: bool = True, timeout: Optional[float] = None
    ) -> TaskStatus:
        """Get task status (blocking). See AsyncShannonClient.get_status for details."""
        return self._run(
            self._async_client.get_status(task_id, include_details, timeout)
        )

    def cancel(
        self, task_id: str, reason: Optional[str] = None, timeout: Optional[float] = None
    ) -> bool:
        """Cancel task (blocking). See AsyncShannonClient.cancel for details."""
        return self._run(self._async_client.cancel(task_id, reason, timeout))

    def approve(
        self,
        approval_id: str,
        workflow_id: str,
        *,
        run_id: Optional[str] = None,
        approved: bool = True,
        feedback: Optional[str] = None,
        modified_action: Optional[str] = None,
        approved_by: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> bool:
        """Approve task (blocking). See AsyncShannonClient.approve for details."""
        return self._run(
            self._async_client.approve(
                approval_id,
                workflow_id,
                run_id=run_id,
                approved=approved,
                feedback=feedback,
                modified_action=modified_action,
                approved_by=approved_by,
                timeout=timeout,
            )
        )

    def get_pending_approvals(
        self,
        *,
        user_id: Optional[str] = None,
        session_id: Optional[str] = None,
        timeout: Optional[float] = None,
    ) -> List[PendingApproval]:
        """Get pending approvals (blocking). See AsyncShannonClient.get_pending_approvals for details."""
        return self._run(
            self._async_client.get_pending_approvals(
                user_id=user_id,
                session_id=session_id,
                timeout=timeout,
            )
        )

    def stream(
        self,
        workflow_id: str,
        *,
        types: Optional[List[Union[str, EventType]]] = None,
        last_stream_id: Optional[str] = None,
        last_event_id: Optional[Union[str, int]] = None,
        use_grpc: Optional[bool] = None,
        reconnect: bool = True,
        max_retries: int = 5,
    ) -> Iterator[Event]:
        """
        Stream events (blocking iterator). See AsyncShannonClient.stream for details.

        Returns synchronous iterator over events.
        """
        loop = self._get_loop()

        async def _async_gen():
            async for event in self._async_client.stream(
                workflow_id,
                types=types,
                last_stream_id=last_stream_id,
                last_event_id=last_event_id,
                use_grpc=use_grpc,
                reconnect=reconnect,
                max_retries=max_retries,
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
        """Close gRPC channel."""
        self._run(self._async_client.close())

    def __enter__(self):
        """Context manager entry."""
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        """Context manager exit."""
        self.close()

    # ===== Session Management (sync wrappers) =====

    def create_session(
        self,
        user_id: str,
        *,
        initial_context: Optional[Dict[str, Any]] = None,
        max_history: int = 50,
        ttl_seconds: int = 3600,
        timeout: Optional[float] = None,
    ) -> Session:
        """Create a new session (blocking)."""
        return self._run(
            self._async_client.create_session(
                user_id,
                initial_context=initial_context,
                max_history=max_history,
                ttl_seconds=ttl_seconds,
                timeout=timeout,
            )
        )

    def get_session(
        self, session_id: str, *, include_history: bool = True, timeout: Optional[float] = None
    ) -> Session:
        """Get session details (blocking)."""
        return self._run(
            self._async_client.get_session(
                session_id, include_history=include_history, timeout=timeout
            )
        )

    def update_session(
        self,
        session_id: str,
        *,
        context_updates: Optional[Dict[str, Any]] = None,
        extend_ttl_seconds: Optional[int] = None,
        timeout: Optional[float] = None,
    ) -> bool:
        """Update session context or TTL (blocking)."""
        return self._run(
            self._async_client.update_session(
                session_id,
                context_updates=context_updates,
                extend_ttl_seconds=extend_ttl_seconds,
                timeout=timeout,
            )
        )

    def delete_session(self, session_id: str, timeout: Optional[float] = None) -> bool:
        """Delete a session (blocking)."""
        return self._run(self._async_client.delete_session(session_id, timeout))

    def list_sessions(
        self,
        user_id: str,
        *,
        active_only: bool = True,
        limit: int = 50,
        offset: int = 0,
        timeout: Optional[float] = None,
    ) -> List[SessionSummary]:
        """List user sessions (blocking)."""
        return self._run(
            self._async_client.list_sessions(
                user_id,
                active_only=active_only,
                limit=limit,
                offset=offset,
                timeout=timeout,
            )
        )

    def add_message(
        self,
        session_id: str,
        role: str,
        content: str,
        *,
        metadata: Optional[Dict[str, Any]] = None,
        timeout: Optional[float] = None,
    ) -> bool:
        """Add message to session (blocking)."""
        return self._run(
            self._async_client.add_message(
                session_id,
                role,
                content,
                metadata=metadata,
                timeout=timeout,
            )
        )

    def clear_history(
        self,
        session_id: str,
        *,
        keep_context: bool = True,
        timeout: Optional[float] = None,
    ) -> bool:
        """Clear session history (blocking)."""
        return self._run(
            self._async_client.clear_history(
                session_id,
                keep_context=keep_context,
                timeout=timeout,
            )
        )
