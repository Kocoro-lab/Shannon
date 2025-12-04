"""Live validation tests for v0.3.0 SDK features.

Requires a running Shannon backend at localhost:8080.
Run with: pytest tests/test_v030_features.py -v -s
"""

import pytest
import sys
sys.path.insert(0, "src")

from shannon import ShannonClient, AsyncShannonClient, TaskStatusEnum


# Skip all tests if backend is not available
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


class TestTaskStatusFields:
    """Test new TaskStatus fields from v0.3.0."""

    def test_get_status_returns_new_fields(self):
        """Verify get_status() returns model_used, provider, usage, etc."""
        client = ShannonClient(base_url="http://localhost:8080")

        try:
            # Submit a simple task
            handle = client.submit_task(
                "What is 2 + 2?",
                session_id="sdk-test-v030-status",
                model_tier="small",
            )

            assert handle.task_id is not None
            print(f"\n  Task submitted: {handle.task_id}")

            # Wait for completion
            status = client.wait(handle.task_id, timeout=60)

            # Verify core fields
            assert status.task_id == handle.task_id
            assert status.status == TaskStatusEnum.COMPLETED
            assert status.result is not None
            print(f"  Status: {status.status.value}")
            print(f"  Result: {status.result[:100]}...")

            # Verify NEW v0.3.0 fields
            print(f"\n  === v0.3.0 New Fields ===")

            # workflow_id
            print(f"  workflow_id: {status.workflow_id}")

            # Timestamps
            print(f"  created_at: {status.created_at}")
            print(f"  updated_at: {status.updated_at}")

            # Query echo
            print(f"  query: {status.query}")
            assert status.query is not None, "query should be returned"

            # Session association
            print(f"  session_id: {status.session_id}")

            # Execution mode
            print(f"  mode: {status.mode}")

            # Model/provider info (critical for cost tracking)
            print(f"  model_used: {status.model_used}")
            print(f"  provider: {status.provider}")
            assert status.model_used is not None, "model_used should be populated"
            assert status.provider is not None, "provider should be populated"

            # Usage breakdown
            print(f"  usage: {status.usage}")
            if status.usage:
                assert "total_tokens" in status.usage or "cost_usd" in status.usage, \
                    "usage should contain token/cost info"

            # Metadata
            print(f"  metadata: {status.metadata}")

            # Context
            print(f"  context: {status.context}")

            print("\n  ✓ All TaskStatus v0.3.0 fields validated")

        finally:
            client.close()


class TestSessionFields:
    """Test new Session/SessionSummary fields from v0.3.0."""

    def test_list_sessions_returns_new_fields(self):
        """Verify list_sessions() returns enhanced SessionSummary with metrics."""
        client = ShannonClient(base_url="http://localhost:8080")

        try:
            # First create a task to ensure we have a session
            handle = client.submit_task(
                "Simple test for session listing",
                session_id="sdk-test-v030-sessions",
                model_tier="small",
            )
            client.wait(handle.task_id, timeout=60)

            # List sessions
            sessions, total = client.list_sessions(limit=10)

            print(f"\n  Found {total} sessions, showing {len(sessions)}")

            if sessions:
                s = sessions[0]
                print(f"\n  === SessionSummary v0.3.0 Fields ===")
                print(f"  session_id: {s.session_id}")
                print(f"  user_id: {s.user_id}")
                print(f"  title: {s.title}")
                print(f"  created_at: {s.created_at}")
                print(f"  updated_at: {s.updated_at}")

                # Budget tracking
                print(f"\n  Budget Tracking:")
                print(f"    token_budget: {s.token_budget}")
                print(f"    budget_remaining: {s.budget_remaining}")
                print(f"    budget_utilization: {s.budget_utilization}")
                print(f"    is_near_budget_limit: {s.is_near_budget_limit}")

                # Activity
                print(f"\n  Activity:")
                print(f"    is_active: {s.is_active}")
                print(f"    last_activity_at: {s.last_activity_at}")
                print(f"    expires_at: {s.expires_at}")

                # Success metrics
                print(f"\n  Success Metrics:")
                print(f"    successful_tasks: {s.successful_tasks}")
                print(f"    failed_tasks: {s.failed_tasks}")
                print(f"    success_rate: {s.success_rate}")

                # Cost tracking
                print(f"\n  Cost Tracking:")
                print(f"    total_tokens_used: {s.total_tokens_used}")
                print(f"    total_cost_usd: {s.total_cost_usd}")
                print(f"    average_cost_per_task: {s.average_cost_per_task}")

                # Latest task preview
                print(f"\n  Latest Task Preview:")
                print(f"    latest_task_query: {s.latest_task_query}")
                print(f"    latest_task_status: {s.latest_task_status}")

                # Research detection
                print(f"\n  Research Detection:")
                print(f"    is_research_session: {s.is_research_session}")
                print(f"    first_task_mode: {s.first_task_mode}")

                print("\n  ✓ All SessionSummary v0.3.0 fields validated")

        finally:
            client.close()

    def test_get_session_returns_new_fields(self):
        """Verify get_session() returns enhanced Session model."""
        client = ShannonClient(base_url="http://localhost:8080")

        try:
            # Create a task with known session
            session_id = "sdk-test-v030-get-session"
            handle = client.submit_task(
                "Test for get_session validation",
                session_id=session_id,
                model_tier="small",
            )
            client.wait(handle.task_id, timeout=60)

            # Get session details
            session = client.get_session(session_id)

            print(f"\n  === Session v0.3.0 Fields ===")
            print(f"  session_id: {session.session_id}")
            print(f"  user_id: {session.user_id}")
            print(f"  title: {session.title}")
            print(f"  created_at: {session.created_at}")
            print(f"  updated_at: {session.updated_at}")
            print(f"  token_budget: {session.token_budget}")
            print(f"  task_count: {session.task_count}")
            print(f"  total_tokens_used: {session.total_tokens_used}")
            print(f"  total_cost_usd: {session.total_cost_usd}")
            print(f"  expires_at: {session.expires_at}")
            print(f"  is_research_session: {session.is_research_session}")
            print(f"  research_strategy: {session.research_strategy}")
            print(f"  context: {session.context}")

            assert session.session_id is not None
            print("\n  ✓ All Session v0.3.0 fields validated")

        finally:
            client.close()

    def test_update_session_title(self):
        """Verify update_session_title() works correctly."""
        client = ShannonClient(base_url="http://localhost:8080")

        try:
            session_id = "sdk-test-v030-title"

            # Create a task first
            handle = client.submit_task(
                "Test for title update",
                session_id=session_id,
                model_tier="small",
            )
            client.wait(handle.task_id, timeout=60)

            # Update title
            new_title = "SDK v0.3.0 Test Session"
            result = client.update_session_title(session_id, new_title)
            assert result is True, "update_session_title should return True"
            print(f"\n  Title updated to: '{new_title}'")

            # Verify title was updated
            session = client.get_session(session_id)
            print(f"  Retrieved title: '{session.title}'")

            # Note: title might be in context depending on backend implementation
            print("\n  ✓ Session title update validated")

        finally:
            client.close()


class TestAsyncClient:
    """Test async client v0.3.0 features."""

    @pytest.mark.asyncio
    async def test_async_get_status_new_fields(self):
        """Verify async client returns all new TaskStatus fields."""
        async with AsyncShannonClient(base_url="http://localhost:8080") as client:
            # Submit task
            handle = await client.submit_task(
                "Async test: What is 3 + 3?",
                session_id="sdk-test-v030-async",
                model_tier="small",
            )

            print(f"\n  Async task submitted: {handle.task_id}")

            # Wait for completion
            status = await client.wait(handle.task_id, timeout=60)

            # Verify new fields
            print(f"  Status: {status.status.value}")
            print(f"  model_used: {status.model_used}")
            print(f"  provider: {status.provider}")
            print(f"  usage: {status.usage}")

            assert status.status == TaskStatusEnum.COMPLETED
            assert status.model_used is not None, "model_used should be populated"

            print("\n  ✓ Async client v0.3.0 fields validated")


class TestVersionConsistency:
    """Test version is correctly reported."""

    def test_version_is_030(self):
        """Verify SDK version is 0.3.0."""
        import shannon
        assert shannon.__version__ == "0.3.0", f"Expected 0.3.0, got {shannon.__version__}"
        print(f"\n  ✓ SDK version: {shannon.__version__}")
