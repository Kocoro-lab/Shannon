"""Basic import and model tests for HTTP-only SDK (no network)."""

import inspect
from datetime import datetime

import pytest
import shannon

from shannon import (
    ShannonClient,
    AsyncShannonClient,
    EventType,
    TaskStatusEnum,
    errors,
)
from shannon.models import (
    AgentExecution,
    AgentInfo,
    ConversationMessage,
    DownloadedFile,
    Event,
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
    Skill,
    SkillDetail,
    SkillVersion,
    SwarmMessageResult,
    ToolDetail,
    ToolExecutionResult,
    ToolSchema,
    ToolUsage,
)


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


def test_package_version():
    assert shannon.__version__ == "0.7.0"


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
        "submit_and_stream",
        "list_session_files",
        "download_session_file",
        "list_memory_files",
        "download_memory_file",
        "list_agents",
        "get_agent",
        "execute_agent",
        "send_swarm_message",
        "list_openai_models",
        "get_openai_model",
        "create_chat_completion",
        "stream_chat_completion",
        "create_completion",
        "stream_completion",
        "stream",
        "approve",
        "list_tools",
        "get_tool",
        "execute_tool",
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


def test_conversation_message_export_and_creation():
    msg = ConversationMessage(role="user", content="hello", tokens_used=12)
    assert hasattr(shannon, "ConversationMessage")
    assert msg.role == "user"
    assert msg.tokens_used == 12


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


def test_tool_schema_creation():
    tool = ToolSchema(
        name="web_search",
        description="Search the web",
        parameters={"type": "object", "properties": {"query": {"type": "string"}}},
    )
    assert tool.name == "web_search"
    assert tool.parameters["type"] == "object"


def test_tool_detail_and_execution_result_creation():
    detail = ToolDetail(
        name="calculator",
        description="Evaluate expressions",
        parameters={"type": "object"},
        category="math",
        version="1.2.0",
        timeout_seconds=10,
        cost_per_use=0.0,
    )
    usage = ToolUsage(tokens=123, cost_usd=0.001)
    result = ToolExecutionResult(
        success=True,
        output={"value": 4},
        text="4",
        metadata={"source": "builtin"},
        execution_time_ms=5,
        usage=usage,
    )

    assert detail.category == "math"
    assert detail.timeout_seconds == 10
    assert result.success is True
    assert result.output == {"value": 4}
    assert result.usage == usage


def test_file_entry_and_downloaded_file_creation():
    entry = FileEntry(name="report.md", path="reports/report.md", is_dir=False, size_bytes=128)
    downloaded = DownloadedFile(
        content="# Report",
        content_type="text/markdown",
        size_bytes=8,
    )

    assert entry.path == "reports/report.md"
    assert entry.size_bytes == 128
    assert downloaded.content_type == "text/markdown"
    assert downloaded.content == "# Report"


def test_agent_models_creation():
    agent = AgentInfo(
        id="keyword_extract",
        name="Keyword Extractor",
        description="Extracts keywords from text",
        category="text",
        tool="keyword_extract",
        input_schema={"type": "object"},
        cost_per_call=0.01,
    )
    execution = AgentExecution(
        task_id="task-123",
        workflow_id="workflow-123",
        agent_id="keyword_extract",
        status="QUEUED",
    )
    message = SwarmMessageResult(success=True, status="delivered")

    assert agent.tool == "keyword_extract"
    assert agent.cost_per_call == 0.01
    assert execution.workflow_id == "workflow-123"
    assert message.success is True
    assert message.status == "delivered"


def test_openai_models_creation():
    message = OpenAIChatMessage(role="user", content="hello")
    delta = OpenAIChatDelta(role="assistant", content="world")
    usage = OpenAIUsage(prompt_tokens=10, completion_tokens=20, total_tokens=30)
    event = OpenAIShannonEvent(type="AGENT_STARTED", agent_id="alpha")
    options = OpenAIShannonOptions(
        context={"source": "sdk"},
        research_strategy="deep",
    )
    choice = OpenAIChatChoice(
        index=0,
        message=OpenAIChatMessage(role="assistant", content="hello world"),
        delta=delta,
        finish_reason="stop",
    )
    completion = OpenAIChatCompletion(
        id="chatcmpl-123",
        object="chat.completion",
        created=123,
        model="shannon-chat",
        choices=[choice],
        usage=usage,
        session_id="session-1",
    )
    chunk = OpenAIChatCompletionChunk(
        id="chatcmpl-123",
        object="chat.completion.chunk",
        created=123,
        model="shannon-chat",
        choices=[choice],
        shannon_events=[event],
    )
    model = OpenAIModel(id="shannon-chat", description="General chat")

    assert message.content == "hello"
    assert delta.content == "world"
    assert usage.total_tokens == 30
    assert event.agent_id == "alpha"
    assert options.research_strategy == "deep"
    assert completion.session_id == "session-1"
    assert chunk.shannon_events[0].type == "AGENT_STARTED"
    assert model.description == "General chat"


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


def test_sync_client_has_agents_methods():
    c = ShannonClient(base_url="http://localhost:8080")
    assert hasattr(c, "list_agents"), "Missing method: list_agents"
    assert hasattr(c, "get_agent"), "Missing method: get_agent"
    assert hasattr(c, "execute_agent"), "Missing method: execute_agent"
    assert hasattr(c, "send_swarm_message"), "Missing method: send_swarm_message"
    c.close()


@pytest.mark.asyncio
async def test_async_client_has_agents_methods():
    ac = AsyncShannonClient(base_url="http://localhost:8080")
    assert hasattr(ac, "list_agents"), "Missing method: list_agents"
    assert hasattr(ac, "get_agent"), "Missing method: get_agent"
    assert hasattr(ac, "execute_agent"), "Missing method: execute_agent"
    assert hasattr(ac, "send_swarm_message"), "Missing method: send_swarm_message"
    await ac.close()


def test_submit_task_has_swarm_param():
    c = ShannonClient(base_url="http://localhost:8080")
    sig = inspect.signature(c.submit_task)
    assert "force_swarm" in sig.parameters, (
        "submit_task missing force_swarm parameter"
    )
    c.close()
