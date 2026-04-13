#!/usr/bin/env python3
"""
E2E verification of Python SDK against live local Shannon stack.

Requires: SHANNON_BASE_URL (default http://localhost:8080), all services running.
Usage: uv run --extra dev python tests/e2e_verify.py
"""

import asyncio
import json
import os
import sys
import time
import traceback

# Add src to path for editable install
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))

from shannon import (
    AsyncShannonClient,
    ShannonClient,
    TaskStatusEnum,
)

BASE_URL = os.environ.get("SHANNON_BASE_URL", "http://localhost:8080")

PASS = 0
FAIL = 0
SKIP = 0
ISSUES = []


def report(name: str, passed: bool, detail: str = "", skip: bool = False):
    global PASS, FAIL, SKIP
    if skip:
        SKIP += 1
        print(f"  SKIP  {name}: {detail}")
        return
    if passed:
        PASS += 1
        print(f"  PASS  {name}")
    else:
        FAIL += 1
        ISSUES.append((name, detail))
        print(f"  FAIL  {name}: {detail}")


async def test_health(client: AsyncShannonClient):
    """Test health and readiness endpoints."""
    print("\n--- Health ---")
    h = await client.health()
    report("health()", h.get("status") == "healthy", f"got: {h}")

    r = await client.readiness()
    report("readiness()", r.get("status") == "ready", f"got: {r}")


async def test_task_lifecycle(client: AsyncShannonClient):
    """Test submit, status, wait, result."""
    print("\n--- Task Lifecycle ---")

    handle = await client.submit_task("What is 2+2? Reply with just the number.")
    report(
        "submit_task() returns handle",
        handle.task_id != "" and handle.workflow_id != "",
        f"task_id={handle.task_id}, workflow_id={handle.workflow_id}",
    )

    status = await client.get_status(handle.task_id)
    report(
        "get_status() returns valid status",
        status.status in TaskStatusEnum.__members__.values(),
        f"status={status.status}",
    )

    # Wait for completion (60s timeout)
    final = await client.wait(handle.task_id, timeout=60)
    report(
        "wait() completes",
        final.status == TaskStatusEnum.COMPLETED,
        f"status={final.status}, error={final.error_message}",
    )
    report(
        "result is non-empty",
        final.result is not None and len(final.result) > 0,
        f"result={final.result!r:.100}",
    )
    report(
        "model_used is populated",
        final.model_used is not None and len(final.model_used) > 0,
        f"model_used={final.model_used}",
    )
    report(
        "usage dict is populated",
        final.usage is not None and isinstance(final.usage, dict),
        f"usage={final.usage}",
    )

    return handle


async def test_task_cancel(client: AsyncShannonClient):
    """Test cancel on a task."""
    print("\n--- Task Cancel ---")
    handle = await client.submit_task(
        "Write a 5000 word essay about the history of computing.",
        context={"force_research": "true"},
    )
    # Give it a moment to start
    await asyncio.sleep(2)

    cancelled = await client.cancel(handle.task_id, reason="E2E test cancel")
    report("cancel() succeeds", cancelled is True, f"returned: {cancelled}")

    await asyncio.sleep(1)
    status = await client.get_status(handle.task_id)
    report(
        "status after cancel is CANCELLED or FAILED",
        status.status in (TaskStatusEnum.CANCELLED, TaskStatusEnum.FAILED),
        f"status={status.status}",
    )


async def test_streaming(client: AsyncShannonClient):
    """Test SSE streaming."""
    print("\n--- SSE Streaming ---")
    handle, stream_url = await client.submit_and_stream("What is the capital of France?")
    report(
        "submit_and_stream() returns handle + stream_url",
        handle.workflow_id != "" and stream_url is not None,
        f"workflow_id={handle.workflow_id}",
    )

    events = []
    event_types = set()
    try:
        async for event in client.stream(handle.workflow_id, timeout=60):
            events.append(event)
            event_types.add(event.type)
    except Exception as e:
        report("stream() completes", False, f"error: {e}")
        return

    report("stream() received events", len(events) > 0, f"count={len(events)}")
    report(
        "stream includes completion event",
        "WORKFLOW_COMPLETED" in event_types,
        f"types={event_types}",
    )
    report(
        "stream includes done event",
        "done" in event_types,
        f"types={event_types}",
    )


async def test_sessions(client: AsyncShannonClient):
    """Test session management."""
    print("\n--- Sessions ---")
    session_id = f"e2e-test-{int(time.time())}"

    # Submit task with session to create it
    handle = await client.submit_task(
        "Say hello", session_id=session_id
    )
    await client.wait(handle.task_id, timeout=60)

    # List sessions
    sessions, total = await client.list_sessions()
    report("list_sessions() returns list", len(sessions) > 0, f"total={total}")

    found = any(s.session_id == session_id for s in sessions)
    report(
        "created session appears in list",
        found,
        f"looking for {session_id}, first 5 ids: {[s.session_id for s in sessions[:5]]}",
    )

    session = await client.get_session(session_id)
    report(
        "get_session() returns session",
        session.session_id == session_id,
        f"requested={session_id}, got={session.session_id}",
    )

    # Update title
    new_title = "E2E Test Session"
    await client.update_session_title(session_id, new_title)
    session2 = await client.get_session(session_id)
    report(
        "update_session_title() works",
        session2.title == new_title,
        f"title={session2.title}",
    )

    # Get history
    history = await client.get_session_history(session_id)
    report("get_session_history() returns items", len(history) > 0, f"count={len(history)}")

    # Delete session
    deleted = await client.delete_session(session_id)
    report("delete_session() succeeds", deleted is True)


async def test_tools_api(client: AsyncShannonClient):
    """Test Tool API (new in this PR)."""
    print("\n--- Tools API ---")

    tools = await client.list_tools()
    report("list_tools() returns list", isinstance(tools, list), f"count={len(tools)}")

    if len(tools) == 0:
        report("get_tool()", False, skip=True, detail="no tools available")
        report("execute_tool()", False, skip=True, detail="no tools available")
        return

    # Verify Tool model fields
    t = tools[0]
    report(
        "Tool model has name+description",
        hasattr(t, "name") and hasattr(t, "description") and t.name != "",
        f"name={t.name}",
    )

    # Get tool detail
    detail = await client.get_tool(t.name)
    report(
        "get_tool() returns ToolDetail",
        detail.name == t.name and hasattr(detail, "parameters"),
        f"name={detail.name}, has_params={detail.parameters is not None}",
    )

    # Try to find a safe tool to execute (web_search or calculator)
    safe_tools = [x for x in tools if x.name in ("web_search", "calculator", "searchapi_search")]
    if safe_tools:
        tool_name = safe_tools[0].name
        if tool_name in ("web_search", "searchapi_search"):
            args = {"query": "Shannon AI platform"}
        else:
            args = {"expression": "2+2"}

        result = await client.execute_tool(tool_name, arguments=args)
        report(
            f"execute_tool({tool_name}) returns result",
            hasattr(result, "success"),
            f"success={result.success}, has_output={result.output is not None}",
        )
    else:
        report("execute_tool()", False, skip=True, detail="no safe tool found to execute")


async def test_openai_compat(client: AsyncShannonClient):
    """Test OpenAI-compatible endpoints (new in this PR)."""
    print("\n--- OpenAI-Compatible ---")

    # List models
    models = await client.list_openai_models()
    report("list_openai_models() returns list", isinstance(models, list), f"count={len(models)}")

    if len(models) > 0:
        m = models[0]
        report(
            "OpenAIModel has id field",
            hasattr(m, "id") and m.id != "",
            f"id={m.id}",
        )

        # Get specific model
        detail = await client.get_openai_model(m.id)
        report("get_openai_model() returns model", detail.id == m.id)

    # Chat completions
    messages = [{"role": "user", "content": "Say 'hello' and nothing else."}]
    completion = await client.create_chat_completion(messages=messages)
    report(
        "create_chat_completion() returns OpenAIChatCompletion",
        hasattr(completion, "id") and hasattr(completion, "choices"),
        f"id={completion.id}, choices={len(completion.choices)}",
    )
    if completion.choices:
        choice = completion.choices[0]
        has_content = (
            choice.message is not None
            and choice.message.content is not None
        )
        report(
            "ChatCompletion has message content",
            has_content,
            f"content={choice.message.content!r:.80}" if has_content else "no message",
        )

    # Streaming chat completions
    chunks = []
    async for chunk in client.stream_chat_completion(
        messages=[{"role": "user", "content": "Say 'hi'"}]
    ):
        chunks.append(chunk)

    report(
        "stream_chat_completion() yields chunks",
        len(chunks) > 0,
        f"count={len(chunks)}",
    )
    if chunks:
        c = chunks[0]
        report(
            "OpenAIChatCompletionChunk has id",
            hasattr(c, "id") and c.id is not None,
            f"id={c.id}",
        )


async def test_agents_api(client: AsyncShannonClient):
    """Test Agents API (new in this PR)."""
    print("\n--- Agents API ---")

    agents = await client.list_agents()
    report("list_agents() returns list", isinstance(agents, list), f"count={len(agents)}")

    if len(agents) == 0:
        report("get_agent()", False, skip=True, detail="no agents registered")
        return

    a = agents[0]
    report(
        "AgentInfo model has id+name",
        hasattr(a, "id") and hasattr(a, "name") and a.id != "",
        f"id={a.id}, name={a.name}",
    )

    detail = await client.get_agent(a.id)
    report("get_agent() returns AgentInfo", detail.id == a.id)

    def build_value(schema, field_name: str):
        field_type = schema.get("type")
        normalized = field_name.lower()
        if field_type == "string":
            if "url" in normalized:
                return "https://example.com"
            if any(token in normalized for token in ["query", "text", "prompt", "content", "topic", "keyword"]):
                return "Shannon platform"
            return "test"
        if field_type == "integer":
            return 1
        if field_type == "number":
            return 1.0
        if field_type == "boolean":
            return True
        if field_type == "array":
            return []
        if field_type == "object":
            return {}
        return "test"

    agent_to_run = None
    for candidate in agents:
        schema = candidate.input_schema or {}
        if not schema.get("required"):
            agent_to_run = candidate
            break

    if agent_to_run is None:
        agent_to_run = a

    schema = agent_to_run.input_schema or {}
    properties = schema.get("properties", {}) or {}
    agent_input = {}
    for field_name in schema.get("required", []):
        agent_input[field_name] = build_value(properties.get(field_name, {}), field_name)

    try:
        execution = await client.execute_agent(agent_to_run.id, agent_input)
    except Exception as e:
        report(
            "execute_agent() accepts schema-compatible input",
            False,
            f"agent={agent_to_run.id}, error={e}",
        )
        return

    report(
        "execute_agent() returns execution handle",
        execution.task_id != "" and execution.workflow_id != "",
        f"agent={agent_to_run.id}, task_id={execution.task_id}",
    )

    try:
        status = await client.get_status(execution.task_id)
        report(
            "executed agent task is trackable",
            status.task_id == execution.task_id,
            f"status={status.status}",
        )
    finally:
        try:
            await client.cancel(execution.task_id, reason="E2E cleanup")
        except Exception:
            pass


async def test_skills_api(client: AsyncShannonClient):
    """Test Skills API."""
    print("\n--- Skills API ---")

    skills = await client.list_skills()
    report("list_skills() returns list", isinstance(skills, list), f"count={len(skills)}")

    if len(skills) == 0:
        report("get_skill()", False, skip=True, detail="no skills registered")
        return

    s = skills[0]
    report(
        "Skill model has name+category",
        hasattr(s, "name") and hasattr(s, "category"),
        f"name={s.name}, category={s.category}",
    )

    detail = await client.get_skill(s.name)
    report(
        "get_skill() returns SkillDetail",
        detail.name == s.name and hasattr(detail, "content"),
    )


async def test_control_signals(client: AsyncShannonClient):
    """Test pause/resume/control-state."""
    print("\n--- Control Signals ---")

    # Submit a long-running task
    handle = await client.submit_task(
        "Write a detailed analysis of quantum computing applications in cryptography. "
        "Cover at least 10 different applications with examples.",
        context={"force_research": "true"},
    )
    # Give it time to start
    await asyncio.sleep(3)

    status = await client.get_status(handle.task_id)
    if status.status != TaskStatusEnum.RUNNING:
        report(
            "task is running before pause",
            False,
            f"status={status.status} (may have completed too fast)",
        )
        return

    # Pause
    paused = await client.pause_task(handle.task_id, reason="E2E test pause")
    report("pause_task() succeeds", paused is True, f"returned: {paused}")

    await asyncio.sleep(1)

    # Check control state
    state = await client.get_control_state(handle.task_id)
    report(
        "get_control_state() shows paused",
        state.is_paused is True,
        f"is_paused={state.is_paused}, reason={state.pause_reason}",
    )

    # Resume
    resumed = await client.resume_task(handle.task_id, reason="E2E test resume")
    report("resume_task() succeeds", resumed is True, f"returned: {resumed}")

    # Cancel to clean up
    await asyncio.sleep(1)
    await client.cancel(handle.task_id, reason="E2E cleanup")


async def test_swarm_message(client: AsyncShannonClient):
    """Test follow-up messaging for swarm workflows."""
    print("\n--- Swarm Messaging ---")

    handle = await client.submit_task(
        "Research recent developments in battery technology and summarize the tradeoffs.",
        context={"force_swarm": True, "force_research": True, "research_strategy": "standard"},
    )

    try:
        status = None
        for _ in range(10):
            await asyncio.sleep(2)
            status = await client.get_status(handle.task_id)
            if status.status == TaskStatusEnum.RUNNING:
                break

        if status is None or status.status != TaskStatusEnum.RUNNING:
            report(
                "send_swarm_message() can be exercised on a running swarm",
                False,
                skip=True,
                detail=f"status={status.status if status else 'unknown'}",
            )
            return

        result = await client.send_swarm_message(
            handle.workflow_id,
            "Please emphasize cost and supply chain constraints.",
        )
        report(
            "send_swarm_message() succeeds",
            result.success is True,
            f"status={result.status}",
        )
    finally:
        try:
            await client.cancel(handle.task_id, reason="E2E cleanup")
        except Exception:
            pass


async def test_file_apis(client: AsyncShannonClient):
    """Test workspace and memory file endpoints."""
    print("\n--- File APIs ---")

    session_id = f"e2e-files-{int(time.time())}"
    handle = await client.submit_task(
        "Create a short markdown summary titled 'E2E File Test'.",
        session_id=session_id,
        model_tier="small",
    )
    await client.wait(handle.task_id, timeout=60)

    workspace_files = await client.list_session_files(session_id)
    report(
        "list_session_files() returns list",
        isinstance(workspace_files, list),
        f"count={len(workspace_files)}",
    )

    if workspace_files:
        first_file = next((entry for entry in workspace_files if not entry.is_dir), None)
        if first_file is not None:
            downloaded = await client.download_session_file(session_id, first_file.path)
            report(
                "download_session_file() returns content",
                downloaded.content is not None,
                f"path={first_file.path}, size={downloaded.size_bytes}",
            )
        else:
            report("download_session_file()", False, skip=True, detail="no file entries to download")
    else:
        report("download_session_file()", False, skip=True, detail="workspace empty")

    memory_files = await client.list_memory_files()
    report(
        "list_memory_files() returns list",
        isinstance(memory_files, list),
        f"count={len(memory_files)}",
    )

    if memory_files:
        first_memory_file = next((entry for entry in memory_files if not entry.is_dir), None)
        if first_memory_file is not None:
            downloaded = await client.download_memory_file(first_memory_file.path)
            report(
                "download_memory_file() returns content",
                downloaded.content is not None,
                f"path={first_memory_file.path}, size={downloaded.size_bytes}",
            )
        else:
            report("download_memory_file()", False, skip=True, detail="no file entries to download")
    else:
        report("download_memory_file()", False, skip=True, detail="memory empty")


async def test_task_list_and_events(client: AsyncShannonClient, handle):
    """Test list_tasks and get_task_events using the handle from task lifecycle."""
    print("\n--- Task List & Events ---")

    tasks, total = await client.list_tasks()
    report("list_tasks() returns list", len(tasks) > 0, f"total={total}")

    if tasks:
        t = tasks[0]
        report(
            "TaskSummary has expected fields",
            hasattr(t, "task_id") and hasattr(t, "query") and hasattr(t, "status"),
            f"task_id={t.task_id}",
        )

    if handle:
        events = await client.get_task_events(handle.task_id)
        report(
            "get_task_events() returns events",
            isinstance(events, list) and len(events) > 0,
            f"count={len(events)}",
        )


async def main():
    print(f"Shannon Python SDK E2E Verification")
    print(f"Target: {BASE_URL}")
    print(f"{'=' * 60}")

    client = AsyncShannonClient(base_url=BASE_URL)

    try:
        await test_health(client)
        handle = await test_task_lifecycle(client)
        await test_task_list_and_events(client, handle)
        await test_task_cancel(client)
        await test_streaming(client)
        await test_sessions(client)
        await test_tools_api(client)
        await test_openai_compat(client)
        await test_agents_api(client)
        await test_skills_api(client)
        await test_control_signals(client)
        await test_swarm_message(client)
        await test_file_apis(client)
    except Exception as e:
        print(f"\n  FATAL: Unhandled exception: {e}")
        traceback.print_exc()
    finally:
        await client.close()

    print(f"\n{'=' * 60}")
    print(f"Results: {PASS} passed, {FAIL} failed, {SKIP} skipped")
    if ISSUES:
        print(f"\nFailures:")
        for name, detail in ISSUES:
            print(f"  - {name}: {detail}")
    print()

    return 1 if FAIL > 0 else 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
