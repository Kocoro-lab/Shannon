"""Basic import and model tests for HTTP-only SDK (no network)."""

import pytest

from shannon import (
    ShannonClient,
    AsyncShannonClient,
    EventType,
    TaskStatusEnum,
    errors,
)
from shannon.models import Event
from datetime import datetime


def test_imports_and_enums():
    # Enums
    assert isinstance(EventType.WORKFLOW_STARTED, EventType)
    assert EventType.LLM_PARTIAL.value == "LLM_PARTIAL"
    assert TaskStatusEnum.COMPLETED.value == "COMPLETED"


def test_error_hierarchy():
    base = errors.ShannonError("oops")
    assert isinstance(base, Exception)
    assert issubclass(errors.TaskNotFoundError, errors.TaskError)
    assert issubclass(errors.TaskError, errors.ShannonError)
    assert issubclass(errors.AuthenticationError, errors.ShannonError)


def test_sync_client_init():
    c = ShannonClient(base_url="http://localhost:8080")
    # Verify key methods exist (no network calls)
    for name in [
        "submit_task",
        "get_status",
        "list_tasks",
        "get_task_events",
        "get_task_timeline",
        "list_sessions",
        "get_session",
        "get_session_history",
        "get_session_events",
        "update_session_title",
        "delete_session",
        "stream",
        "approve",
    ]:
        assert hasattr(c, name), f"Missing method: {name}"
    c.close()


@pytest.mark.asyncio
async def test_async_client_init():
    ac = AsyncShannonClient(base_url="http://localhost:8080")
    assert ac.base_url.endswith(":8080")
    await ac.close()


def test_event_model_basic():
    e = Event(
        type=EventType.LLM_OUTPUT.value,
        workflow_id="wf-1",
        message="hello",
        timestamp=datetime.now(),
        seq=1,
        stream_id="1",
    )
    assert e.id == "1"
    assert e.payload is None

