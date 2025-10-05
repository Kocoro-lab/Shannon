"""Core functionality tests - simplified and working."""

import pytest
from shannon import ShannonClient, AsyncShannonClient
from shannon.models import EventType, TaskStatusEnum
from shannon import errors


def test_imports():
    """Test that all public APIs are importable."""
    from shannon import (
        ShannonClient,
        AsyncShannonClient,
        Event,
        EventType,
        TaskStatusEnum,
        TaskHandle,
        TaskStatus,
        PendingApproval,
        Session,
    )
    from shannon.errors import (
        ShannonError,
        ConnectionError,
        AuthenticationError,
        TaskError,
        TaskNotFoundError,
        SessionError,
        ValidationError,
    )
    assert True  # If imports work, test passes


def test_event_type_enum():
    """Test EventType is proper enum."""
    assert isinstance(EventType.WORKFLOW_STARTED, EventType)
    assert EventType.WORKFLOW_STARTED.value == "WORKFLOW_STARTED"
    assert EventType.LLM_PARTIAL.value == "LLM_PARTIAL"
    assert len([e for e in EventType]) >= 17  # At least 17 event types


def test_task_status_enum():
    """Test TaskStatusEnum values."""
    assert TaskStatusEnum.QUEUED.value == "QUEUED"
    assert TaskStatusEnum.RUNNING.value == "RUNNING"
    assert TaskStatusEnum.COMPLETED.value == "COMPLETED"
    assert TaskStatusEnum.FAILED.value == "FAILED"
    assert TaskStatusEnum.CANCELLED.value == "CANCELLED"
    assert TaskStatusEnum.TIMEOUT.value == "TIMEOUT"


def test_error_hierarchy():
    """Test exception hierarchy."""
    # Base error
    base_err = errors.ShannonError("test")
    assert isinstance(base_err, Exception)

    # Specific errors inherit from base
    assert issubclass(errors.TaskNotFoundError, errors.TaskError)
    assert issubclass(errors.TaskError, errors.ShannonError)
    assert issubclass(errors.AuthenticationError, errors.ShannonError)
    assert issubclass(errors.ValidationError, errors.ShannonError)


def test_client_initialization():
    """Test client can be initialized."""
    client = ShannonClient(
        grpc_endpoint="localhost:50052",
        http_endpoint="http://localhost:8081",
        api_key="test_key",
    )
    assert client._async_client.grpc_endpoint == "localhost:50052"
    assert client._async_client.http_endpoint == "http://localhost:8081"
    client.close()


@pytest.mark.asyncio
async def test_async_client_initialization():
    """Test async client initialization."""
    client = AsyncShannonClient(
        grpc_endpoint="localhost:50052",
        http_endpoint="http://localhost:8081",
    )
    assert client.grpc_endpoint == "localhost:50052"
    assert client.http_endpoint == "http://localhost:8081"
    # Don't actually connect in unit test
    client._channel = None
    await client.close()


def test_event_model():
    """Test Event model."""
    from shannon.models import Event
    from datetime import datetime

    event = Event(
        type=EventType.LLM_PARTIAL.value,
        workflow_id="test-workflow",
        message="Test message",
        timestamp=datetime.now(),
        seq=1,
        stream_id="0-1",
    )

    assert event.type == "LLM_PARTIAL"
    assert event.workflow_id == "test-workflow"
    assert event.id == "0-1"  # Uses stream_id if available


def test_execution_metrics_model():
    """Test ExecutionMetrics model."""
    from shannon.models import ExecutionMetrics

    metrics = ExecutionMetrics(
        tokens_used=150,
        cost_usd=0.0075,
        duration_seconds=2.5,
        llm_calls=1,
        tool_calls=2,
    )

    assert metrics.tokens_used == 150
    assert metrics.cost_usd == 0.0075
    assert metrics.duration_seconds == 2.5


def test_task_handle_model():
    """Test TaskHandle model."""
    from shannon.models import TaskHandle

    handle = TaskHandle(
        task_id="task-123",
        workflow_id="workflow-123",
    )

    assert handle.task_id == "task-123"
    assert handle.workflow_id == "workflow-123"


def test_session_model():
    """Test Session model."""
    from shannon.models import Session
    from datetime import datetime

    session = Session(
        session_id="session-123",
        user_id="test-user",
        created_at=datetime.now(),
        updated_at=datetime.now(),
    )

    assert session.session_id == "session-123"
    assert session.user_id == "test-user"


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
