"""Test CLI commands."""

import pytest
from unittest.mock import MagicMock, patch
from datetime import datetime
import sys
from io import StringIO

from shannon import cli
from shannon.models import TaskHandle, TaskStatus, TaskStatusEnum, ExecutionMetrics, PendingApproval, Session


@pytest.fixture
def mock_client():
    """Create mock ShannonClient."""
    client = MagicMock()
    client.close = MagicMock()
    return client


def test_cli_submit(mock_client, capsys):
    """Test submit command."""
    # Mock response
    mock_client.submit_task.return_value = TaskHandle(
        task_id="task-123",
        workflow_id="workflow-123",
    )

    with patch("shannon.cli.ShannonClient", return_value=mock_client):
        sys.argv = ["shannon", "submit", "What is 2+2?", "--user-id", "test-user"]
        cli.main()

    captured = capsys.readouterr()
    assert "task-123" in captured.out
    assert "workflow-123" in captured.out
    mock_client.submit_task.assert_called_once()


def test_cli_submit_with_wait(mock_client, capsys):
    """Test submit with --wait flag."""
    mock_client.submit_task.return_value = TaskHandle(
        task_id="task-123",
        workflow_id="workflow-123",
    )

    # Mock status progression
    mock_client.get_status.side_effect = [
        TaskStatus(
            task_id="task-123",
            status=TaskStatusEnum.RUNNING,
            progress=0.5,
        ),
        TaskStatus(
            task_id="task-123",
            status=TaskStatusEnum.COMPLETED,
            progress=1.0,
            result="4",
        ),
    ]

    with patch("shannon.cli.ShannonClient", return_value=mock_client):
        with patch("time.sleep"):  # Speed up polling
            sys.argv = ["shannon", "submit", "What is 2+2?", "--wait"]
            cli.main()

    captured = capsys.readouterr()
    assert "COMPLETED" in captured.out or "Result: 4" in captured.out


def test_cli_status(mock_client, capsys):
    """Test status command."""
    mock_client.get_status.return_value = TaskStatus(
        task_id="task-123",
        status=TaskStatusEnum.COMPLETED,
        result="The answer is 4",
        metrics=ExecutionMetrics(
            tokens_used=150,
            cost_usd=0.0075,
            duration_seconds=2.5,
        ),
    )

    with patch("shannon.cli.ShannonClient", return_value=mock_client):
        sys.argv = ["shannon", "status", "task-123"]
        cli.main()

    captured = capsys.readouterr()
    assert "task-123" in captured.out
    assert "COMPLETED" in captured.out
    assert "The answer is 4" in captured.out
    assert "150" in captured.out  # tokens
    assert "0.0075" in captured.out  # cost


def test_cli_cancel(mock_client, capsys):
    """Test cancel command."""
    mock_client.cancel.return_value = True

    with patch("shannon.cli.ShannonClient", return_value=mock_client):
        sys.argv = ["shannon", "cancel", "task-123", "--reason", "User requested"]
        cli.main()

    captured = capsys.readouterr()
    assert "cancelled" in captured.out.lower()
    mock_client.cancel.assert_called_once()


def test_cli_stream(mock_client, capsys):
    """Test stream command."""
    from shannon.models import Event

    # Mock events
    events = [
        Event(
            type="WORKFLOW_STARTED",
            workflow_id="workflow-123",
            message="Started",
            timestamp=datetime.now(),
        ),
        Event(
            type="LLM_PARTIAL",
            workflow_id="workflow-123",
            message="Thinking...",
            timestamp=datetime.now(),
        ),
        Event(
            type="WORKFLOW_COMPLETED",
            workflow_id="workflow-123",
            message="Done",
            timestamp=datetime.now(),
        ),
    ]

    mock_client.stream.return_value = iter(events)

    with patch("shannon.cli.ShannonClient", return_value=mock_client):
        sys.argv = ["shannon", "stream", "workflow-123"]
        cli.main()

    captured = capsys.readouterr()
    assert "WORKFLOW_STARTED" in captured.out
    assert "LLM_PARTIAL" in captured.out
    assert "WORKFLOW_COMPLETED" in captured.out


def test_cli_approvals(mock_client, capsys):
    """Test approvals command."""
    mock_client.get_pending_approvals.return_value = [
        PendingApproval(
            approval_id="approval-1",
            workflow_id="workflow-123",
            message="Approve this?",
            requested_at=datetime.now(),
        )
    ]

    with patch("shannon.cli.ShannonClient", return_value=mock_client):
        sys.argv = ["shannon", "approvals", "--user-id", "test-user"]
        cli.main()

    captured = capsys.readouterr()
    assert "approval-1" in captured.out
    assert "Approve this?" in captured.out


def test_cli_approve(mock_client, capsys):
    """Test approve command."""
    mock_client.approve.return_value = True

    with patch("shannon.cli.ShannonClient", return_value=mock_client):
        sys.argv = [
            "shannon",
            "approve",
            "approval-1",
            "workflow-123",
            "--approve",
            "--feedback",
            "LGTM",
        ]
        cli.main()

    captured = capsys.readouterr()
    assert "approved" in captured.out.lower()
    mock_client.approve.assert_called_once_with(
        approval_id="approval-1",
        workflow_id="workflow-123",
        approved=True,
        feedback="LGTM",
    )


def test_cli_approve_reject(mock_client, capsys):
    """Test approve --reject flag."""
    mock_client.approve.return_value = True

    with patch("shannon.cli.ShannonClient", return_value=mock_client):
        sys.argv = [
            "shannon",
            "approve",
            "approval-1",
            "workflow-123",
            "--reject",
        ]
        cli.main()

    captured = capsys.readouterr()
    assert "rejected" in captured.out.lower()

    # Verify approved=False was passed
    call_args = mock_client.approve.call_args
    assert call_args.kwargs["approved"] is False


def test_cli_session_create(mock_client, capsys):
    """Test session-create command."""
    mock_client.create_session.return_value = Session(
        session_id="session-123",
        user_id="test-user",
        created_at=datetime.now(),
        updated_at=datetime.now(),
    )

    with patch("shannon.cli.ShannonClient", return_value=mock_client):
        sys.argv = [
            "shannon",
            "session-create",
            "--user-id",
            "test-user",
            "--ttl-seconds",
            "7200",
        ]
        cli.main()

    captured = capsys.readouterr()
    assert "session-123" in captured.out
    mock_client.create_session.assert_called_once()


def test_cli_session_list(mock_client, capsys):
    """Test session-list command."""
    from shannon.models import SessionSummary
    mock_client.list_sessions.return_value = [
        SessionSummary(
            session_id="session-1",
            user_id="test-user",
            created_at=datetime.now(),
            updated_at=datetime.now(),
            message_count=5,
            total_tokens_used=0,
            is_active=True,
        ),
        SessionSummary(
            session_id="session-2",
            user_id="test-user",
            created_at=datetime.now(),
            updated_at=datetime.now(),
            message_count=10,
            total_tokens_used=0,
            is_active=True,
        ),
    ]

    with patch("shannon.cli.ShannonClient", return_value=mock_client):
        sys.argv = ["shannon", "session-list", "--user-id", "test-user"]
        cli.main()

    captured = capsys.readouterr()
    assert "session-1" in captured.out
    assert "session-2" in captured.out

    # Verify active_only=True by default
    call_args = mock_client.list_sessions.call_args
    assert call_args[1]["active_only"] is True


def test_cli_session_list_all(mock_client, capsys):
    """Test session-list --all flag."""
    mock_client.list_sessions.return_value = []

    with patch("shannon.cli.ShannonClient", return_value=mock_client):
        sys.argv = ["shannon", "session-list", "--user-id", "test-user", "--all"]
        cli.main()

    # Verify active_only=False when --all
    call_args = mock_client.list_sessions.call_args
    assert call_args[1]["active_only"] is False


def test_cli_session_add_message(mock_client, capsys):
    """Test session-add-message command."""
    mock_client.add_message.return_value = True

    with patch("shannon.cli.ShannonClient", return_value=mock_client):
        sys.argv = [
            "shannon",
            "session-add-message",
            "session-123",
            "--role",
            "user",
            "--content",
            "Hello",
        ]
        cli.main()

    captured = capsys.readouterr()
    assert "Added" in captured.out
    mock_client.add_message.assert_called_once_with(
        "session-123", role="user", content="Hello"
    )


def test_cli_authentication_flags(mock_client):
    """Test --api-key and --bearer-token flags."""
    with patch("shannon.cli.ShannonClient") as MockClient:
        MockClient.return_value = mock_client
        mock_client.submit_task.return_value = TaskHandle(
            task_id="task-123", workflow_id="workflow-123"
        )

        sys.argv = [
            "shannon",
            "--api-key",
            "sk_test_123",
            "--bearer-token",
            "bearer_token_456",
            "submit",
            "test",
        ]
        cli.main()

        # Verify client was created with auth
        call_args = MockClient.call_args
        assert call_args.kwargs["api_key"] == "sk_test_123"
        assert call_args.kwargs["bearer_token"] == "bearer_token_456"


def test_cli_endpoint_flags(mock_client):
    """Test --endpoint and --http-endpoint flags."""
    with patch("shannon.cli.ShannonClient") as MockClient:
        MockClient.return_value = mock_client
        mock_client.submit_task.return_value = TaskHandle(
            task_id="task-123", workflow_id="workflow-123"
        )

        sys.argv = [
            "shannon",
            "--endpoint",
            "custom-grpc:50052",
            "--http-endpoint",
            "http://custom-http:8081",
            "submit",
            "test",
        ]
        cli.main()

        # Verify endpoints
        call_args = MockClient.call_args
        assert call_args.kwargs["grpc_endpoint"] == "custom-grpc:50052"
        assert call_args.kwargs["http_endpoint"] == "http://custom-http:8081"
