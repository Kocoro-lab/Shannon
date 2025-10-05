"""Test core client functionality."""

import pytest
from unittest.mock import AsyncMock, MagicMock, patch
from datetime import datetime

from shannon import AsyncShannonClient, ShannonClient
from shannon.models import TaskHandle, TaskStatus, TaskStatusEnum, ExecutionMetrics
from shannon import errors

# Import proto types for mocking
from shannon.generated.orchestrator import orchestrator_pb2
from shannon.generated.common import common_pb2
import grpc


@pytest.fixture
def mock_grpc_channel():
    """Create mock gRPC channel."""
    channel = AsyncMock()
    channel.close = AsyncMock()
    return channel


@pytest.fixture
async def client(mock_grpc_channel):
    """Create AsyncShannonClient with mocked channel."""
    with patch("grpc.aio.insecure_channel", return_value=mock_grpc_channel):
        client = AsyncShannonClient(grpc_endpoint="localhost:50052")
        yield client
        # Don't await close since we mocked the channel
        client._channel = None


@pytest.mark.asyncio
async def test_submit_task_success(client, mock_grpc_channel):
    """Test successful task submission."""
    # Mock response
    mock_response = orchestrator_pb2.SubmitTaskResponse(
        task_id="task-123",
        workflow_id="workflow-123",
    )

    # Mock stub
    mock_stub = AsyncMock()
    mock_stub.SubmitTask.return_value = mock_response
    client._orchestrator_stub = mock_stub

    # Submit task
    handle = await client.submit_task("What is 2+2?", user_id="test-user")

    # Verify
    assert isinstance(handle, TaskHandle)
    assert handle.task_id == "task-123"
    assert handle.workflow_id == "workflow-123"

    # Verify stub was called
    mock_stub.SubmitTask.assert_called_once()


@pytest.mark.asyncio
async def test_get_status_with_metrics(client):
    """Test get_status with metrics parsing."""
    # Create response with nested TokenUsage
    token_usage = common_pb2.TokenUsage(
        prompt_tokens=100,
        completion_tokens=50,
        total_tokens=150,
        cost_usd=0.0075,
    )

    metrics = common_pb2.ExecutionMetrics(
        latency_ms=2500,
        token_usage=token_usage,
        cache_hit=False,
    )

    mock_response = orchestrator_pb2.GetTaskStatusResponse(
        task_id="task-123",
        workflow_id="workflow-123",
        status=common_pb2.TaskStatus.COMPLETED,
        result="2+2 equals 4",
        metrics=metrics,
    )

    mock_stub = AsyncMock()
    mock_stub.GetTaskStatus.return_value = mock_response
    client._orchestrator_stub = mock_stub

    # Get status
    status = await client.get_status("task-123", include_details=True)

    # Verify
    assert isinstance(status, TaskStatus)
    assert status.task_id == "task-123"
    assert status.status == TaskStatusEnum.COMPLETED
    assert status.result == "2+2 equals 4"

    # Verify metrics parsing
    assert status.metrics is not None
    assert status.metrics.tokens_used == 150
    assert status.metrics.cost_usd == 0.0075
    assert status.metrics.duration_seconds == 2.5  # 2500ms -> 2.5s


@pytest.mark.asyncio
async def test_get_status_not_found(client):
    """Test get_status with NOT_FOUND error."""
    # Create mock RPC error
    mock_error = MagicMock()
    mock_error.code = MagicMock(return_value=grpc.StatusCode.NOT_FOUND)
    mock_error.details = MagicMock(return_value="Task not found")

    mock_stub = AsyncMock()
    mock_stub.GetTaskStatus.side_effect = mock_error
    client._orchestrator_stub = mock_stub

    # Should raise TaskNotFoundError
    with pytest.raises(errors.TaskNotFoundError) as exc_info:
        await client.get_status("nonexistent-task")

    assert "not found" in str(exc_info.value).lower()


@pytest.mark.asyncio
async def test_cancel_task(client):
    """Test task cancellation."""
    mock_response = orchestrator_pb2.CancelTaskResponse(success=True)

    mock_stub = AsyncMock()
    mock_stub.CancelTask.return_value = mock_response
    client._orchestrator_stub = mock_stub

    # Cancel task
    success = await client.cancel("task-123", reason="User requested")

    # Verify
    assert success is True
    mock_stub.CancelTask.assert_called_once()


@pytest.mark.asyncio
async def test_approve_request(client):
    """Test approval submission."""
    mock_response = orchestrator_pb2.ApproveTaskResponse(success=True)

    mock_stub = AsyncMock()
    mock_stub.ApproveTask.return_value = mock_response
    client._orchestrator_stub = mock_stub

    # Submit approval
    success = await client.approve(
        approval_id="approval-123",
        workflow_id="workflow-123",
        approved=True,
        feedback="Looks good",
    )

    # Verify
    assert success is True
    mock_stub.ApproveTask.assert_called_once()

    # Verify request fields
    call_args = mock_stub.ApproveTask.call_args
    request = call_args[0][0]
    assert request.approval_id == "approval-123"
    assert request.approved is True
    assert request.feedback == "Looks good"


@pytest.mark.asyncio
async def test_get_pending_approvals(client):
    """Test fetching pending approvals."""
    from shannon.generated.orchestrator import orchestrator_pb2
    from google.protobuf.timestamp_pb2 import Timestamp

    # Create mock approvals
    ts = Timestamp()
    ts.GetCurrentTime()

    approval1 = orchestrator_pb2.PendingApprovalItem(
        approval_id="approval-1",
        workflow_id="workflow-1",
        message="Approve this action?",
        requested_at=ts,
    )

    mock_response = orchestrator_pb2.GetPendingApprovalsResponse(
        approvals=[approval1]
    )

    mock_stub = AsyncMock()
    mock_stub.GetPendingApprovals.return_value = mock_response
    client._orchestrator_stub = mock_stub

    # Get approvals
    approvals = await client.get_pending_approvals(user_id="test-user")

    # Verify
    assert len(approvals) == 1
    assert approvals[0].approval_id == "approval-1"
    assert approvals[0].message == "Approve this action?"


@pytest.mark.asyncio
async def test_create_session(client):
    """Test session creation."""
    from shannon.generated.session import session_pb2
    from google.protobuf.timestamp_pb2 import Timestamp

    ts = Timestamp()
    ts.GetCurrentTime()

    mock_response = session_pb2.CreateSessionResponse(
        session_id="session-123",
        user_id="test-user",
        created_at=ts,
        ttl_seconds=3600,
    )

    mock_stub = AsyncMock()
    mock_stub.CreateSession.return_value = mock_response
    client._session_stub = mock_stub

    # Create session
    session = await client.create_session(
        user_id="test-user",
        ttl_seconds=3600,
        max_history=50,
    )

    # Verify
    assert session.session_id == "session-123"
    assert session.user_id == "test-user"
    mock_stub.CreateSession.assert_called_once()


@pytest.mark.asyncio
async def test_unauthenticated_error(client):
    """Test UNAUTHENTICATED error mapping."""
    mock_error = MagicMock()
    mock_error.code = MagicMock(return_value=grpc.StatusCode.UNAUTHENTICATED)
    mock_error.details = MagicMock(return_value="Invalid API key")

    mock_stub = AsyncMock()
    mock_stub.SubmitTask.side_effect = mock_error
    client._orchestrator_stub = mock_stub

    # Should raise AuthenticationError
    with pytest.raises(errors.AuthenticationError):
        await client.submit_task("test query")


@pytest.mark.asyncio
async def test_invalid_argument_error(client):
    """Test INVALID_ARGUMENT error mapping."""
    mock_error = MagicMock()
    mock_error.code = MagicMock(return_value=grpc.StatusCode.INVALID_ARGUMENT)
    mock_error.details = MagicMock(return_value="Missing required field")

    mock_stub = AsyncMock()
    mock_stub.SubmitTask.side_effect = mock_error
    client._orchestrator_stub = mock_stub

    # Should raise ValidationError
    with pytest.raises(errors.ValidationError):
        await client.submit_task("")


def test_sync_client_submit():
    """Test sync wrapper for submit_task."""
    with patch("shannon.client.AsyncShannonClient") as MockAsyncClient:
        mock_async = MockAsyncClient.return_value
        mock_async.submit_task = AsyncMock(
            return_value=TaskHandle(
                task_id="task-123",
                workflow_id="workflow-123",
            )
        )

        # Create sync client
        client = ShannonClient()

        # Submit task (will use event loop)
        with patch("asyncio.new_event_loop") as mock_loop_ctor:
            mock_loop = MagicMock()
            mock_loop_ctor.return_value = mock_loop
            mock_loop.run_until_complete.return_value = TaskHandle(
                task_id="task-123",
                workflow_id="workflow-123",
            )

            handle = client.submit_task("test query")

            # Verify
            assert handle.task_id == "task-123"
