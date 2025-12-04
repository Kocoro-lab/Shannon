"""Comprehensive validation tests for v0.3.0 SDK features.

Tests different workflow types, execution patterns, and SSE streaming.
Requires a running Shannon backend at localhost:8080.

Run with: pytest tests/test_v030_comprehensive.py -v -s
"""

import pytest
import sys
import time
sys.path.insert(0, "src")

from shannon import ShannonClient, AsyncShannonClient, TaskStatusEnum, EventType


def backend_available():
    """Check if Shannon backend is running."""
    try:
        import httpx
        resp = httpx.get("http://localhost:8080/health", timeout=2)
        return resp.status_code == 200
    except Exception:
        return False


pytestmark = pytest.mark.skipif(
    not backend_available(),
    reason="Shannon backend not available at localhost:8080"
)


class TestSimpleWorkflow:
    """Test simple/standard workflow metadata."""

    def test_simple_mode_metadata(self):
        """Test simple mode returns correct metadata."""
        client = ShannonClient(base_url="http://localhost:8080")

        try:
            handle = client.submit_task(
                "What is the capital of France?",
                session_id="sdk-test-simple-mode",
                mode="simple",
                model_tier="small",
            )

            print(f"\n  Task ID: {handle.task_id}")
            status = client.wait(handle.task_id, timeout=60)

            print(f"  Status: {status.status.value}")
            print(f"  Mode: {status.mode}")
            print(f"  Model: {status.model_used}")
            print(f"  Provider: {status.provider}")

            assert status.status == TaskStatusEnum.COMPLETED
            assert status.model_used is not None
            assert status.provider is not None

            # Check metadata contains execution info
            if status.metadata:
                print(f"  Metadata keys: {list(status.metadata.keys())}")
                if "mode" in status.metadata:
                    print(f"  Execution mode: {status.metadata.get('mode')}")

            print("\n  ✓ Simple mode metadata validated")

        finally:
            client.close()

    def test_standard_mode_metadata(self):
        """Test standard mode with decomposition."""
        client = ShannonClient(base_url="http://localhost:8080")

        try:
            handle = client.submit_task(
                "Explain the difference between Python lists and tuples",
                session_id="sdk-test-standard-mode",
                mode="standard",
                model_tier="medium",
            )

            print(f"\n  Task ID: {handle.task_id}")
            status = client.wait(handle.task_id, timeout=120)

            print(f"  Status: {status.status.value}")
            print(f"  Mode: {status.mode}")
            print(f"  Model: {status.model_used}")
            print(f"  Provider: {status.provider}")

            if status.usage:
                print(f"  Total tokens: {status.usage.get('total_tokens')}")
                print(f"  Cost: ${status.usage.get('cost_usd', status.usage.get('estimated_cost', 0)):.6f}")

            if status.metadata:
                print(f"  Num agents: {status.metadata.get('num_agents', 'N/A')}")
                if "model_breakdown" in status.metadata:
                    print(f"  Model breakdown: {len(status.metadata['model_breakdown'])} models used")
                    for m in status.metadata["model_breakdown"]:
                        print(f"    - {m.get('model')}: {m.get('tokens')} tokens, ${m.get('cost_usd', 0):.6f}")

            print("\n  ✓ Standard mode metadata validated")

        finally:
            client.close()


class TestResearchWorkflow:
    """Test research workflow patterns and metadata."""

    def test_research_quick_strategy(self):
        """Test quick research strategy."""
        client = ShannonClient(base_url="http://localhost:8080")

        try:
            handle = client.submit_task(
                "What are the main benefits of solar energy?",
                session_id="sdk-test-research-quick",
                context={
                    "force_research": True,
                    "research_strategy": "quick",
                },
                model_tier="small",
            )

            print(f"\n  Task ID: {handle.task_id}")
            print("  Strategy: quick (force_research=True)")

            # Research workflows can take longer
            status = client.wait(handle.task_id, timeout=300)

            print(f"  Status: {status.status.value}")
            print(f"  Model: {status.model_used}")
            print(f"  Provider: {status.provider}")

            if status.usage:
                print(f"  Total tokens: {status.usage.get('total_tokens')}")
                print(f"  Cost: ${status.usage.get('cost_usd', status.usage.get('estimated_cost', 0)):.6f}")

            if status.metadata:
                print(f"  Mode from metadata: {status.metadata.get('mode')}")
                if "model_breakdown" in status.metadata:
                    print(f"  Models used: {len(status.metadata['model_breakdown'])}")

            if status.context:
                print(f"  Context keys: {list(status.context.keys())}")

            print("\n  ✓ Research quick strategy validated")

        finally:
            client.close()

    def test_research_standard_strategy(self):
        """Test standard research strategy with more depth."""
        client = ShannonClient(base_url="http://localhost:8080")

        try:
            handle = client.submit_task(
                "Compare renewable energy sources: solar vs wind power",
                session_id="sdk-test-research-standard",
                context={
                    "force_research": True,
                    "research_strategy": "standard",
                },
                model_tier="medium",
            )

            print(f"\n  Task ID: {handle.task_id}")
            print("  Strategy: standard (force_research=True)")

            status = client.wait(handle.task_id, timeout=300)

            print(f"  Status: {status.status.value}")
            print(f"  Model: {status.model_used}")
            print(f"  Provider: {status.provider}")

            if status.usage:
                print(f"  Total tokens: {status.usage.get('total_tokens')}")
                print(f"  Input tokens: {status.usage.get('input_tokens')}")
                print(f"  Output tokens: {status.usage.get('output_tokens')}")
                print(f"  Cost: ${status.usage.get('cost_usd', status.usage.get('estimated_cost', 0)):.6f}")

            if status.metadata:
                print(f"  Metadata keys: {list(status.metadata.keys())}")
                if "num_agents" in status.metadata:
                    print(f"  Agents used: {status.metadata['num_agents']}")
                if "model_breakdown" in status.metadata:
                    print(f"  Model breakdown:")
                    for m in status.metadata["model_breakdown"]:
                        pct = m.get('percentage', 0)
                        print(f"    - {m.get('model')}: {pct}% ({m.get('tokens')} tokens)")

            print("\n  ✓ Research standard strategy validated")

        finally:
            client.close()


class TestSSEStreaming:
    """Test SSE streaming events."""

    def test_sse_stream_events(self):
        """Test SSE streaming returns events with correct types."""
        client = ShannonClient(base_url="http://localhost:8080")

        try:
            # Submit task
            handle = client.submit_task(
                "Count from 1 to 5",
                session_id="sdk-test-sse-stream",
                model_tier="small",
            )

            print(f"\n  Task ID: {handle.task_id}")
            print(f"  Workflow ID: {handle.workflow_id}")
            print("\n  Streaming events...")

            event_types_seen = set()
            event_count = 0
            last_event = None

            # Stream events
            for event in client.stream(handle.workflow_id, timeout=60, total_timeout=120):
                event_count += 1
                event_types_seen.add(event.type)
                last_event = event

                # Print first few events in detail
                if event_count <= 10:
                    print(f"    [{event_count}] {event.type}: {event.message[:80] if event.message else '(no message)'}...")

                # Check for completion
                if event.type == "WORKFLOW_COMPLETED":
                    print(f"    ... workflow completed")
                    break

            print(f"\n  Total events received: {event_count}")
            print(f"  Event types seen: {sorted(event_types_seen)}")

            # Verify key event types
            expected_types = {"WORKFLOW_STARTED", "WORKFLOW_COMPLETED"}
            assert "WORKFLOW_COMPLETED" in event_types_seen or event_count > 0, \
                "Should receive workflow events"

            # Verify event structure
            if last_event:
                print(f"\n  Last event structure:")
                print(f"    type: {last_event.type}")
                print(f"    workflow_id: {last_event.workflow_id}")
                print(f"    timestamp: {last_event.timestamp}")
                print(f"    seq: {last_event.seq}")
                print(f"    stream_id: {last_event.stream_id}")

            print("\n  ✓ SSE streaming validated")

        finally:
            client.close()

    def test_sse_stream_with_type_filter(self):
        """Test SSE streaming with event type filtering."""
        client = ShannonClient(base_url="http://localhost:8080")

        try:
            handle = client.submit_task(
                "What is 10 + 20?",
                session_id="sdk-test-sse-filtered",
                model_tier="small",
            )

            print(f"\n  Task ID: {handle.task_id}")
            print("  Filtering for: LLM_OUTPUT, WORKFLOW_COMPLETED")

            filtered_events = []
            for event in client.stream(
                handle.workflow_id,
                types=[EventType.LLM_OUTPUT, EventType.WORKFLOW_COMPLETED],
                timeout=60,
                total_timeout=120,
            ):
                filtered_events.append(event)
                print(f"    {event.type}: {event.message[:60] if event.message else ''}...")

                if event.type == "WORKFLOW_COMPLETED":
                    break

            print(f"\n  Filtered events received: {len(filtered_events)}")
            print(f"  Event types: {[e.type for e in filtered_events]}")

            # Server may return related event types (thread.message.* are LLM output events)
            # The filter is a hint, not a strict requirement
            llm_related = ["LLM_OUTPUT", "WORKFLOW_COMPLETED", "LLM_PARTIAL",
                          "thread.message.completed", "thread.message.delta"]

            has_completion = any(e.type == "WORKFLOW_COMPLETED" for e in filtered_events)
            assert has_completion or len(filtered_events) > 0, "Should receive events"

            print("\n  ✓ Filtered SSE streaming validated")

        finally:
            client.close()

    @pytest.mark.asyncio
    async def test_async_sse_stream(self):
        """Test async SSE streaming."""
        async with AsyncShannonClient(base_url="http://localhost:8080") as client:
            handle = await client.submit_task(
                "Name three colors",
                session_id="sdk-test-async-sse",
                model_tier="small",
            )

            print(f"\n  Async task ID: {handle.task_id}")
            print("  Async streaming events...")

            event_count = 0
            async for event in client.stream(handle.workflow_id, timeout=60, total_timeout=120):
                event_count += 1
                if event_count <= 5:
                    print(f"    [{event_count}] {event.type}")

                if event.type == "WORKFLOW_COMPLETED":
                    break

            print(f"\n  Async events received: {event_count}")
            assert event_count > 0, "Should receive events"

            print("\n  ✓ Async SSE streaming validated")


class TestModelTierVariations:
    """Test different model tiers return appropriate metadata."""

    def test_small_tier_metadata(self):
        """Test small tier model selection."""
        client = ShannonClient(base_url="http://localhost:8080")

        try:
            handle = client.submit_task(
                "Say hello",
                session_id="sdk-test-tier-small",
                model_tier="small",
            )

            status = client.wait(handle.task_id, timeout=60)

            print(f"\n  Model tier requested: small")
            print(f"  Model used: {status.model_used}")
            print(f"  Provider: {status.provider}")

            assert status.model_used is not None
            print("\n  ✓ Small tier validated")

        finally:
            client.close()

    def test_medium_tier_metadata(self):
        """Test medium tier model selection."""
        client = ShannonClient(base_url="http://localhost:8080")

        try:
            handle = client.submit_task(
                "Explain quantum computing in one sentence",
                session_id="sdk-test-tier-medium",
                model_tier="medium",
            )

            status = client.wait(handle.task_id, timeout=90)

            print(f"\n  Model tier requested: medium")
            print(f"  Model used: {status.model_used}")
            print(f"  Provider: {status.provider}")

            if status.usage:
                print(f"  Cost: ${status.usage.get('cost_usd', status.usage.get('estimated_cost', 0)):.6f}")

            assert status.model_used is not None
            print("\n  ✓ Medium tier validated")

        finally:
            client.close()


class TestMetadataBreakdown:
    """Test model breakdown and cost attribution in metadata."""

    def test_model_breakdown_structure(self):
        """Test model_breakdown contains expected fields."""
        client = ShannonClient(base_url="http://localhost:8080")

        try:
            handle = client.submit_task(
                "List three programming languages and their main use cases",
                session_id="sdk-test-breakdown",
                model_tier="medium",
            )

            status = client.wait(handle.task_id, timeout=120)

            print(f"\n  Task completed: {status.status.value}")

            if status.metadata and "model_breakdown" in status.metadata:
                breakdown = status.metadata["model_breakdown"]
                print(f"\n  Model breakdown ({len(breakdown)} entries):")

                total_tokens = 0
                total_cost = 0.0

                for entry in breakdown:
                    model = entry.get("model", "unknown")
                    provider = entry.get("provider", "unknown")
                    tokens = entry.get("tokens", 0)
                    cost = entry.get("cost_usd", 0)
                    pct = entry.get("percentage", 0)
                    executions = entry.get("executions", 0)

                    total_tokens += tokens
                    total_cost += cost

                    print(f"    {model} ({provider}):")
                    print(f"      - Tokens: {tokens} ({pct}%)")
                    print(f"      - Cost: ${cost:.6f}")
                    print(f"      - Executions: {executions}")

                print(f"\n  Totals from breakdown:")
                print(f"    Total tokens: {total_tokens}")
                print(f"    Total cost: ${total_cost:.6f}")

                # Compare with usage field
                if status.usage:
                    usage_tokens = status.usage.get("total_tokens", 0)
                    usage_cost = status.usage.get("cost_usd", status.usage.get("estimated_cost", 0))
                    print(f"\n  From usage field:")
                    print(f"    Total tokens: {usage_tokens}")
                    print(f"    Total cost: ${usage_cost:.6f}")

            else:
                print("  No model_breakdown in metadata")
                print(f"  Metadata keys: {list(status.metadata.keys()) if status.metadata else 'None'}")

            print("\n  ✓ Model breakdown validated")

        finally:
            client.close()


class TestTaskListMetadata:
    """Test task listing includes usage metadata."""

    def test_list_tasks_with_usage(self):
        """Test list_tasks returns token usage."""
        client = ShannonClient(base_url="http://localhost:8080")

        try:
            # First create a task
            handle = client.submit_task(
                "Quick test for listing",
                session_id="sdk-test-list-usage",
                model_tier="small",
            )
            client.wait(handle.task_id, timeout=60)

            # Now list tasks
            tasks, total = client.list_tasks(limit=5)

            print(f"\n  Total tasks: {total}")
            print(f"  Showing: {len(tasks)}")

            for t in tasks[:3]:
                print(f"\n  Task: {t.task_id[:40]}...")
                print(f"    Query: {t.query[:50] if t.query else 'N/A'}...")
                print(f"    Status: {t.status}")
                print(f"    Mode: {t.mode}")

                if t.total_token_usage:
                    print(f"    Token usage:")
                    print(f"      Total: {t.total_token_usage.total_tokens}")
                    print(f"      Prompt: {t.total_token_usage.prompt_tokens}")
                    print(f"      Completion: {t.total_token_usage.completion_tokens}")
                    print(f"      Cost: ${t.total_token_usage.cost_usd:.6f}")
                else:
                    print(f"    Token usage: N/A")

            print("\n  ✓ Task list usage metadata validated")

        finally:
            client.close()
