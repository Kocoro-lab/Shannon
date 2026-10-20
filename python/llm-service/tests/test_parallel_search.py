"""Exercise the web_search resolver and callers over the real MCP SDK transport."""

import asyncio
import json
import uuid

import httpx
import pytest
from llm_service.api.agent import _execute_and_format_tools
from llm_service.api.tools import ToolExecuteRequest, execute_tool
from llm_service.tools import parallel_search
from llm_service.tools.builtin.web_search import (
    ParallelSearchProvider,
    SearchAPISearchProvider,
    SerpAPISearchProvider,
    WebSearchTool,
)
from llm_service.tools.registry import ToolRegistry

KEYS = (
    "GOOGLE_SEARCH_API_KEY",
    "GOOGLE_SEARCH_ENGINE_ID",
    "SERPER_API_KEY",
    "SERPAPI_API_KEY",
    "SEARCHAPI_API_KEY",
    "BING_API_KEY",
    "EXA_API_KEY",
    "FIRECRAWL_API_KEY",
    "PARALLEL_API_KEY",
)


@pytest.fixture(autouse=True)
def isolated_environment(monkeypatch):
    for key in (
        *KEYS,
        "WEB_SEARCH_PROVIDER",
        "MCP_ALLOWED_DOMAINS",
        "MCP_TIMEOUT_SECONDS",
    ):
        monkeypatch.delenv(key, raising=False)
    monkeypatch.setenv("WEB_SEARCH_RATE_LIMIT", "100")


@pytest.fixture
def mcp_server(monkeypatch):
    """Mock only HTTP, retaining MCP initialization, discovery and tool dispatch."""
    requests = []
    state = {"failure": None, "structured": False}

    async def handle(request):
        requests.append(request)
        assert str(request.url) == parallel_search.MCP_URL
        assert request.headers["user-agent"] == parallel_search.USER_AGENT
        assert "authorization" not in request.headers
        body = json.loads(request.content)
        method = body["method"]
        if method == "notifications/initialized":
            return httpx.Response(202)
        if method == "initialize":
            result = {
                "protocolVersion": "2025-06-18",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "fixture", "version": "1"},
            }
        elif method == "tools/list":
            result = {
                "tools": [
                    {
                        "name": "web_search",
                        "description": "Search",
                        "inputSchema": {"type": "object"},
                    }
                ]
            }
        else:
            assert method == "tools/call"
            assert body["params"]["name"] == "web_search"
            args = body["params"]["arguments"]
            assert args["objective"] == args["search_queries"][0]
            uuid.UUID(args["session_id"])
            if state["failure"] == "http":
                return httpx.Response(429, text="quota exceeded")
            if state["failure"] == "timeout":
                raise httpx.ReadTimeout("secret transport details", request=request)
            if state["failure"] == "wait":
                await asyncio.sleep(10)
            data = {
                "results": [
                    {"url": "javascript:alert(1)", "title": "Unsafe URL"},
                    {
                        "url": "https://example.org/research",
                        "title": "Research reference",
                        "excerpts": ["An excerpt for the agent to consume."],
                    },
                    {"url": "https://example.net", "excerpts": ["Second result"]},
                ]
            }
            result = {
                "content": [{"type": "text", "text": json.dumps(data)}],
                "isError": state["failure"] == "tool",
            }
            if state["structured"]:
                result["structuredContent"] = data
            if state["failure"] == "malformed":
                result["content"][0]["text"] = "invalid JSON"
        return httpx.Response(
            200, json={"jsonrpc": "2.0", "id": body["id"], "result": result}
        )

    def factory(headers=None, timeout=None, auth=None):
        return httpx.AsyncClient(
            transport=httpx.MockTransport(handle),
            headers=headers,
            timeout=timeout,
            auth=auth,
        )

    monkeypatch.setattr(parallel_search, "_http_client", factory)
    registry = ToolRegistry()
    registry.register(WebSearchTool)
    monkeypatch.setattr("llm_service.tools.registry._global_registry", registry)
    return requests, state, registry


def tool_calls(requests):
    bodies = [json.loads(request.content) for request in requests]
    return [
        body["params"]["arguments"] for body in bodies if body["method"] == "tools/call"
    ]


@pytest.mark.parametrize("choice", [None, "auto", "", "parallel"])
def test_fresh_default(choice, monkeypatch):
    if choice is not None:
        monkeypatch.setenv("WEB_SEARCH_PROVIDER", choice)
    tool = WebSearchTool()
    assert isinstance(tool.provider, ParallelSearchProvider)
    assert not tool.metadata.requires_auth


def test_configured_credentials_and_overrides(monkeypatch):
    monkeypatch.setenv("SERPAPI_API_KEY", "a" * 20)
    monkeypatch.setenv("SEARCHAPI_API_KEY", "b" * 20)
    assert isinstance(WebSearchTool().provider, SerpAPISearchProvider)
    monkeypatch.setenv("WEB_SEARCH_PROVIDER", "searchapi")
    assert isinstance(WebSearchTool().provider, SearchAPISearchProvider)
    monkeypatch.setenv("WEB_SEARCH_PROVIDER", "parallel")
    assert isinstance(WebSearchTool().provider, ParallelSearchProvider)


@pytest.mark.parametrize("choice", ["google", "serpapi", "typo"])
def test_explicit_missing_provider_does_not_enable_parallel(choice, monkeypatch):
    monkeypatch.setenv("WEB_SEARCH_PROVIDER", choice)
    assert WebSearchTool().provider is None


def test_invalid_configured_credentials_do_not_enable_parallel(monkeypatch):
    monkeypatch.setenv("SERPAPI_API_KEY", "invalid")
    assert WebSearchTool().provider is None


@pytest.mark.asyncio
@pytest.mark.parametrize("structured", [False, True])
async def test_api_caller_consumes_mcp_results(mcp_server, monkeypatch, structured):
    requests, state, _ = mcp_server
    state["structured"] = structured
    monkeypatch.setenv("PARALLEL_API_KEY", "c" * 20)
    result = await execute_tool(
        ToolExecuteRequest(
            tool_name="web_search",
            parameters={"query": "research reference", "max_results": 1},
            session_context={"session_id": "conversation-a", "user_id": "private-user"},
        )
    )
    assert result.success
    assert result.output["result_count"] == 1
    assert result.output["results"][0]["source"] == "parallel"
    assert "https://example.org/research" in result.text
    assert "An excerpt for the agent" in result.text
    assert "javascript:" not in result.text
    assert "private-user" not in json.dumps(tool_calls(requests))
    assert {json.loads(r.content)["method"] for r in requests} == {
        "initialize",
        "notifications/initialized",
        "tools/list",
        "tools/call",
    }


@pytest.mark.asyncio
async def test_agent_dispatch_permissions_and_consumption(mcp_server):
    requests, _, _ = mcp_server
    call = [{"name": "web_search", "arguments": {"query": "research reference"}}]
    denied = await _execute_and_format_tools(call, allowed_tools=[])
    assert not requests
    assert not denied[0]
    allowed = await _execute_and_format_tools(
        call, allowed_tools=["web_search"], context={"session_id": "agent-conversation"}
    )
    assert "https://example.org/research" in allowed[0]
    assert "An excerpt for the agent" in allowed[0]
    assert len(tool_calls(requests)) == 1


@pytest.mark.asyncio
async def test_interleaved_conversations_followup_retry_and_reset(mcp_server):
    requests, state, registry = mcp_server
    tool = registry.get_tool("web_search")

    async def search(session):
        return await tool.execute(
            query="research reference", session_context={"session_id": session}
        )

    await asyncio.gather(search("conversation-a"), search("conversation-b"))
    first = tool_calls(requests)
    ids = {args["session_id"] for args in first}
    assert len(ids) == 2
    state["failure"] = "tool"
    assert not (await search("conversation-a")).success
    state["failure"] = None
    assert (await search("conversation-a")).success
    await search("new-chat")
    calls = tool_calls(requests)
    assert calls[2]["session_id"] == calls[3]["session_id"]
    assert calls[2]["session_id"] in ids
    assert calls[4]["session_id"] not in ids
    assert (await tool.execute(query="standalone")).success
    assert tool_calls(requests)[-1]["session_id"] not in ids


@pytest.mark.asyncio
async def test_independent_runtime_owners_isolate_common_session_names(mcp_server):
    requests, _, registry = mcp_server
    first = registry.get_tool("web_search")
    other_registry = ToolRegistry()
    other_registry.register(WebSearchTool)
    second = other_registry.get_tool("web_search")
    context = {"session_id": "demo"}
    for tool in (first, second, first, second):
        result = await tool.execute(query="research reference", session_context=context)
        assert result.success
    calls = tool_calls(requests)
    assert calls[0]["session_id"] != calls[1]["session_id"]
    assert calls[0]["session_id"] == calls[2]["session_id"]
    assert calls[1]["session_id"] == calls[3]["session_id"]


@pytest.mark.asyncio
@pytest.mark.parametrize("choice", ["none", "off", "disabled"])
async def test_disabled_never_contacts_network(mcp_server, monkeypatch, choice):
    requests, _, _ = mcp_server
    monkeypatch.setenv("WEB_SEARCH_PROVIDER", choice)
    result = await WebSearchTool().execute(query="site:example.org")
    assert not result.success
    assert "disabled" in result.error
    assert not requests


@pytest.mark.asyncio
@pytest.mark.parametrize("domains", ["localhost", "", "example.org,notparallel.ai"])
async def test_mcp_domain_permission_denies_network(mcp_server, monkeypatch, domains):
    requests, _, _ = mcp_server
    monkeypatch.setenv("MCP_ALLOWED_DOMAINS", domains)
    result = await execute_tool(
        ToolExecuteRequest(
            tool_name="web_search", parameters={"query": "research reference"}
        )
    )
    assert not result.success
    assert "MCP_ALLOWED_DOMAINS" in result.error
    assert not requests


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "domains",
    ["search.parallel.ai", "parallel.ai", "*", "localhost, SEARCH.PARALLEL.AI"],
)
async def test_mcp_domain_permission_allows_configured_host(
    mcp_server, monkeypatch, domains
):
    requests, _, _ = mcp_server
    monkeypatch.setenv("MCP_ALLOWED_DOMAINS", domains)
    result = await execute_tool(
        ToolExecuteRequest(
            tool_name="web_search", parameters={"query": "research reference"}
        )
    )
    assert result.success
    assert tool_calls(requests)


def test_explicit_mcp_timeout_is_preserved(monkeypatch):
    monkeypatch.setenv("MCP_TIMEOUT_SECONDS", "3.5")
    assert WebSearchTool().provider.client.timeout == 3.5


@pytest.mark.asyncio
@pytest.mark.parametrize("failure", ["http", "tool", "timeout", "malformed"])
async def test_errors_are_reported_without_fallback(mcp_server, failure):
    requests, state, registry = mcp_server
    state["failure"] = failure
    tool = registry.get_tool("web_search")
    result = await tool.execute(query="research reference")
    assert not result.success
    assert "temporarily unavailable" in result.error
    assert "secret" not in result.error
    assert isinstance(tool.provider, ParallelSearchProvider)
    assert len(tool_calls(requests)) == 1


@pytest.mark.asyncio
async def test_deadline_and_cancellation(mcp_server, monkeypatch):
    requests, state, registry = mcp_server
    state["failure"] = "wait"
    monkeypatch.setattr(ParallelSearchProvider, "DEFAULT_TIMEOUT", 0.05)
    tool = registry.get_tool("web_search")
    result = await tool.execute(query="research reference")
    assert not result.success
    assert "timeout" in result.error
    monkeypatch.setattr(tool.provider.client, "timeout", 20)
    task = asyncio.create_task(tool.execute(query="research reference"))
    while len(tool_calls(requests)) < 2:
        await asyncio.sleep(0.001)
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task
