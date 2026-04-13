import pytest

from shannon import ShannonClient, errors
from shannon.client import AsyncShannonClient


@pytest.mark.asyncio
async def test_async_list_agents_parses_response_body():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "agents": [
                    {
                        "id": "keyword_extract",
                        "name": "Keyword Extractor",
                        "description": "Extract keywords",
                        "category": "text",
                        "tool": "keyword_extract",
                        "input_schema": {"type": "object"},
                        "cost_per_call": 0.01,
                    }
                ]
            }

    class StubClient:
        async def get(self, url, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    agents = await client.list_agents()

    assert len(agents) == 1
    assert agents[0].id == "keyword_extract"
    assert agents[0].cost_per_call == 0.01


@pytest.mark.asyncio
async def test_async_execute_agent_uses_headers_and_body():
    captured = {}

    class StubResponse:
        status_code = 202
        headers = {
            "X-Workflow-ID": "workflow-123",
            "X-Session-ID": "session-1",
        }

        def json(self):
            return {
                "task_id": "task-123",
                "agent_id": "keyword_extract",
                "status": "QUEUED",
                "created_at": "2026-04-13T12:34:56Z",
            }

    class StubClient:
        async def post(self, url, json=None, headers=None, timeout=None):
            captured["url"] = url
            captured["json"] = json or {}
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    execution = await client.execute_agent(
        "keyword_extract",
        {"text": "hello world"},
        session_id="session-1",
        stream=True,
    )

    assert captured["json"] == {
        "input": {"text": "hello world"},
        "session_id": "session-1",
        "stream": True,
    }
    assert execution.task_id == "task-123"
    assert execution.workflow_id == "workflow-123"
    assert execution.session_id == "session-1"
    assert execution.created_at is not None


@pytest.mark.asyncio
async def test_async_send_swarm_message_checks_success_flag():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "success": True,
                "status": "delivered",
            }

    class StubClient:
        async def post(self, url, json=None, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    result = await client.send_swarm_message("workflow-123", "continue")

    assert result.success is True
    assert result.status == "delivered"


@pytest.mark.asyncio
async def test_async_send_swarm_message_raises_on_unsuccessful_body():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "success": False,
                "error": "workflow is not running",
            }

    class StubClient:
        async def post(self, url, json=None, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    with pytest.raises(errors.ShannonError, match="workflow is not running"):
        await client.send_swarm_message("workflow-123", "continue")


def test_sync_get_agent_wraps_async_result():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "id": "keyword_extract",
                "name": "Keyword Extractor",
                "description": "Extract keywords",
                "category": "text",
                "tool": "keyword_extract",
                "input_schema": {"type": "object"},
                "cost_per_call": 0.01,
            }

    class StubClient:
        async def get(self, url, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = ShannonClient(base_url="http://example")
    client._async_client._ensure_client = _ensure_client  # type: ignore

    agent = client.get_agent("keyword_extract")

    assert agent.name == "Keyword Extractor"
    assert agent.tool == "keyword_extract"
