"""Basic import and model tests for HTTP-only SDK (no network)."""

import inspect

import pytest

from shannon import (
    ShannonClient,
    AsyncShannonClient,
    EventType,
    TaskStatusEnum,
    errors,
)
from shannon.models import (
    Event,
    ReviewRound,
    ReviewState,
    Skill,
    SkillDetail,
    SkillVersion,
)
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
        "cancel",
        "pause_task",
        "resume_task",
        "get_control_state",
        "list_sessions",
        "get_session",
        "get_session_history",
        "get_session_events",
        "update_session_title",
        "delete_session",
        "stream",
        "approve",
        # Schedule methods (v0.5.0)
        "create_schedule",
        "get_schedule",
        "list_schedules",
        "update_schedule",
        "pause_schedule",
        "resume_schedule",
        "delete_schedule",
        "get_schedule_runs",
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


# --- Review model tests (v0.6.0) ---


def test_review_round_creation():
    ts = datetime.now()
    rr = ReviewRound(role="user", message="Please revise the intro", timestamp=ts)
    assert rr.role == "user"
    assert rr.message == "Please revise the intro"
    assert rr.timestamp == ts


def test_review_state_creation():
    rounds = [
        ReviewRound(role="assistant", message="Here is the plan"),
        ReviewRound(role="user", message="Looks good"),
    ]
    rs = ReviewState(
        status="reviewing",
        round=2,
        version=1,
        current_plan="Draft plan text",
        rounds=rounds,
        query="Summarize quarterly results",
    )
    assert rs.status == "reviewing"
    assert rs.round == 2
    assert rs.version == 1
    assert rs.current_plan == "Draft plan text"
    assert len(rs.rounds) == 2
    assert rs.rounds[0].role == "assistant"
    assert rs.query == "Summarize quarterly results"


# --- Skill model tests (v0.6.0) ---


def test_skill_creation():
    s = Skill(
        name="web_research",
        version="1.0.0",
        category="research",
        description="Search the web and summarize findings",
    )
    assert s.name == "web_research"
    assert s.version == "1.0.0"
    assert s.category == "research"
    assert s.requires_tools == []
    assert s.dangerous is False
    assert s.enabled is True


def test_skill_detail_creation():
    sd = SkillDetail(
        name="data_analysis",
        version="2.1.0",
        category="analytics",
        description="Analyze datasets with pandas",
        author="team-shannon",
        requires_tools=["python_executor", "file_read"],
        requires_role="data_analytics",
        budget_max=5000,
        dangerous=False,
        enabled=True,
        content="Step 1: Load data\nStep 2: Analyze",
        metadata={"last_updated": "2026-02-13"},
    )
    assert sd.name == "data_analysis"
    assert sd.version == "2.1.0"
    assert sd.author == "team-shannon"
    assert sd.requires_tools == ["python_executor", "file_read"]
    assert sd.requires_role == "data_analytics"
    assert sd.budget_max == 5000
    assert sd.content == "Step 1: Load data\nStep 2: Analyze"
    assert sd.metadata == {"last_updated": "2026-02-13"}


def test_skill_version_creation():
    sv = SkillVersion(
        name="summarizer",
        version="0.3.0",
        category="text",
        description="Summarize long documents",
        requires_tools=["web_search"],
        dangerous=False,
        enabled=True,
    )
    assert sv.name == "summarizer"
    assert sv.version == "0.3.0"
    assert sv.category == "text"
    assert sv.requires_tools == ["web_search"]


# --- Method existence tests (v0.6.0) ---


def test_sync_client_has_review_methods():
    c = ShannonClient(base_url="http://localhost:8080")
    assert hasattr(c, "get_review_state"), "Missing method: get_review_state"
    assert hasattr(c, "submit_review_feedback"), "Missing method: submit_review_feedback"
    assert hasattr(c, "approve_review"), "Missing method: approve_review"
    c.close()


def test_sync_client_has_skills_methods():
    c = ShannonClient(base_url="http://localhost:8080")
    assert hasattr(c, "list_skills"), "Missing method: list_skills"
    assert hasattr(c, "get_skill"), "Missing method: get_skill"
    assert hasattr(c, "get_skill_versions"), "Missing method: get_skill_versions"
    c.close()


@pytest.mark.asyncio
async def test_async_client_has_review_methods():
    ac = AsyncShannonClient(base_url="http://localhost:8080")
    assert hasattr(ac, "get_review_state"), "Missing method: get_review_state"
    assert hasattr(ac, "submit_review_feedback"), "Missing method: submit_review_feedback"
    assert hasattr(ac, "approve_review"), "Missing method: approve_review"
    await ac.close()


@pytest.mark.asyncio
async def test_async_client_has_skills_methods():
    ac = AsyncShannonClient(base_url="http://localhost:8080")
    assert hasattr(ac, "list_skills"), "Missing method: list_skills"
    assert hasattr(ac, "get_skill"), "Missing method: get_skill"
    assert hasattr(ac, "get_skill_versions"), "Missing method: get_skill_versions"
    await ac.close()


def test_submit_task_has_swarm_param():
    c = ShannonClient(base_url="http://localhost:8080")
    sig = inspect.signature(c.submit_task)
    assert "force_swarm" in sig.parameters, (
        "submit_task missing force_swarm parameter"
    )
    c.close()
