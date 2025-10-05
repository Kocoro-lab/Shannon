"""Basic integration tests for Shannon SDK."""

import os
import pytest
import time

from shannon import ShannonClient, TaskStatusEnum
from shannon.errors import TaskNotFoundError


@pytest.fixture
def client():
    """Create test client."""
    client = ShannonClient(
        grpc_endpoint=os.getenv("SHANNON_GRPC_ENDPOINT", "localhost:50052"),
        http_endpoint=os.getenv("SHANNON_HTTP_ENDPOINT", "http://localhost:8081"),
        api_key=os.getenv("SHANNON_API_KEY", ""),
    )
    yield client
    client.close()


def test_submit_and_get_status(client):
    """Test basic task submission and status retrieval."""
    # Submit a simple calculator task
    handle = client.submit_task(
        "What is 10 + 15?",
        user_id="test-user",
        session_id="test-session",
    )

    assert handle.task_id
    assert handle.workflow_id
    print(f"Task submitted: {handle.task_id}")

    # Get initial status
    status = client.get_status(handle.task_id)
    assert status.task_id == handle.task_id
    assert status.status in [TaskStatusEnum.QUEUED, TaskStatusEnum.RUNNING]
    print(f"Initial status: {status.status}")

    # Wait for completion (with timeout)
    max_wait = 60  # 60 seconds
    start = time.time()
    while time.time() - start < max_wait:
        status = client.get_status(handle.task_id, include_details=True)
        print(f"Status: {status.status} ({status.progress:.1%})")

        if status.status in [
            TaskStatusEnum.COMPLETED,
            TaskStatusEnum.FAILED,
            TaskStatusEnum.CANCELLED,
        ]:
            break

        time.sleep(2)

    # Verify completion
    assert status.status in [TaskStatusEnum.COMPLETED, TaskStatusEnum.FAILED]

    if status.status == TaskStatusEnum.COMPLETED:
        print(f"Result: {status.result}")
        assert status.result is not None
        # Calculator task should return "25" or similar
        assert "25" in status.result

        # Check metrics if available
        if status.metrics:
            print(f"Tokens used: {status.metrics.tokens_used}")
            print(f"Cost: ${status.metrics.cost_usd:.4f}")
            assert status.metrics.tokens_used > 0
    else:
        print(f"Task failed: {status.error_message}")
        pytest.fail(f"Task failed: {status.error_message}")


def test_task_not_found(client):
    """Test getting status of non-existent task."""
    with pytest.raises(TaskNotFoundError):
        client.get_status("non-existent-task-id")


def test_cancel_task(client):
    """Test task cancellation."""
    # Submit a task
    handle = client.submit_task(
        "Count to 1000 slowly",
        user_id="test-user",
    )

    # Wait a moment for it to start
    time.sleep(1)

    # Cancel it
    success = client.cancel(handle.task_id, reason="Testing cancellation")
    assert success

    # Verify it's cancelled
    time.sleep(2)
    status = client.get_status(handle.task_id)
    print(f"Status after cancel: {status.status}")
    # Should be cancelled or possibly completed if it finished very quickly
    assert status.status in [TaskStatusEnum.CANCELLED, TaskStatusEnum.COMPLETED]


def test_context_manager(client):
    """Test client as context manager."""
    with ShannonClient(grpc_endpoint="localhost:50052") as test_client:
        handle = test_client.submit_task("Test query", user_id="test")
        assert handle.task_id


if __name__ == "__main__":
    # Run tests manually for development
    import sys

    pytest.main([__file__, "-v", "-s"] + sys.argv[1:])
