import pytest

from shannon import ShannonClient, errors
from shannon.client import AsyncShannonClient


@pytest.mark.asyncio
async def test_async_list_tools_parses_array_response():
    class StubResponse:
        status_code = 200

        def json(self):
            return [
                {
                    "name": "web_search",
                    "description": "Search the web",
                    "parameters": {"type": "object"},
                }
            ]

    class StubClient:
        async def get(self, url, params=None, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    tools = await client.list_tools(category="research")

    assert len(tools) == 1
    assert tools[0].name == "web_search"
    assert tools[0].parameters == {"type": "object"}


@pytest.mark.asyncio
async def test_async_list_tools_parses_wrapped_response():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "tools": [
                    {
                        "name": "calculator",
                        "description": "Evaluate expressions",
                        "parameters": {"type": "object"},
                    }
                ]
            }

    class StubClient:
        async def get(self, url, params=None, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    tools = await client.list_tools()

    assert len(tools) == 1
    assert tools[0].name == "calculator"


@pytest.mark.asyncio
async def test_async_get_tool_parses_detail_response():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "name": "calculator",
                "description": "Evaluate expressions",
                "category": "math",
                "version": "1.0.0",
                "parameters": {"type": "object"},
                "timeout_seconds": 15,
                "cost_per_use": 0.0,
            }

    class StubClient:
        async def get(self, url, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    tool = await client.get_tool("calculator")

    assert tool.name == "calculator"
    assert tool.category == "math"
    assert tool.timeout_seconds == 15
    assert tool.parameters == {"type": "object"}


@pytest.mark.asyncio
async def test_async_get_tool_quotes_special_characters():
    captured = {}

    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "name": "browser/use",
                "description": "Browser automation",
                "parameters": {"type": "object"},
            }

    class StubClient:
        async def get(self, url, headers=None, timeout=None):
            captured["url"] = url
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    await client.get_tool("browser/use")

    assert captured["url"].endswith("/api/v1/tools/browser%2Fuse")


@pytest.mark.asyncio
async def test_async_execute_tool_parses_body_and_usage():
    captured = {}

    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "success": True,
                "output": {"result": 42},
                "text": "42",
                "metadata": {"source": "builtin"},
                "execution_time_ms": 8,
                "usage": {"tokens": 250, "cost_usd": 0.0025},
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

    result = await client.execute_tool(
        "calculator",
        arguments={"expression": "6 * 7"},
        session_id="session-1",
    )

    assert captured["json"] == {
        "arguments": {"expression": "6 * 7"},
        "session_id": "session-1",
    }
    assert result.success is True
    assert result.output == {"result": 42}
    assert result.text == "42"
    assert result.usage is not None
    assert result.usage.tokens == 250
    assert result.usage.cost_usd == 0.0025


@pytest.mark.asyncio
async def test_async_execute_tool_quotes_special_characters():
    captured = {}

    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "success": True,
                "output": {"ok": True},
            }

    class StubClient:
        async def post(self, url, json=None, headers=None, timeout=None):
            captured["url"] = url
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    await client.execute_tool("browser/use", arguments={})

    assert captured["url"].endswith("/api/v1/tools/browser%2Fuse/execute")


@pytest.mark.asyncio
async def test_async_get_tool_raises_on_404():
    class StubURL:
        path = "/api/v1/tools/calculator"

    class StubRequest:
        url = StubURL()

    class StubResponse:
        status_code = 404
        request = StubRequest()
        text = "tool not found"

        def json(self):
            return {"error": "tool not found"}

    class StubClient:
        async def get(self, url, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    with pytest.raises(errors.ShannonError, match="tool not found"):
        await client.get_tool("calculator")


def test_sync_execute_tool_wraps_async_result():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "success": False,
                "error": "invalid input",
                "metadata": {"source": "builtin"},
            }

    class StubClient:
        async def post(self, url, json=None, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = ShannonClient(base_url="http://example")
    client._async_client._ensure_client = _ensure_client  # type: ignore

    result = client.execute_tool("calculator", arguments={"expression": "("})

    assert result.success is False
    assert result.error == "invalid input"
    assert result.metadata == {"source": "builtin"}
