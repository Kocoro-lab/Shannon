import asyncio
from typing import Any, Dict, List, Optional

import httpx
import pytest

from llm_service.api.tools import _builtin_tool_classes
from llm_service.tools.builtin.xquik_search import (
    XquikSearchTool,
    xquik_search_enabled,
)


class FakeResponse:
    def __init__(
        self,
        payload: Any,
        status_code: int = 200,
        json_error: Optional[ValueError] = None,
    ) -> None:
        self.payload = payload
        self.status_code = status_code
        self.json_error = json_error

    def json(self) -> Any:
        if self.json_error:
            raise self.json_error
        return self.payload


class FakeAsyncClient:
    def __init__(
        self,
        factory: "FakeClientFactory",
        timeout: int,
    ) -> None:
        self.factory = factory
        self.timeout = timeout

    async def __aenter__(self) -> "FakeAsyncClient":
        return self

    async def __aexit__(self, *args: Any) -> None:
        return None

    async def get(self, url: str, **kwargs: Any) -> FakeResponse:
        self.factory.calls.append({"url": url, "timeout": self.timeout, **kwargs})
        if self.factory.error:
            raise self.factory.error
        return self.factory.response


class FakeClientFactory:
    def __init__(
        self,
        response: Optional[FakeResponse] = None,
        error: Optional[httpx.HTTPError] = None,
    ) -> None:
        self.response = response or FakeResponse(
            {"tweets": [], "has_more": False, "next_cursor": None}
        )
        self.error = error
        self.calls: List[Dict[str, Any]] = []

    def __call__(self, timeout: int) -> FakeAsyncClient:
        return FakeAsyncClient(self, timeout)


def _run(tool: XquikSearchTool, **kwargs: Any) -> Any:
    return asyncio.run(tool.execute(**kwargs))


def test_xquik_search_requires_explicit_enablement(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.delenv("XQUIK_SEARCH_ENABLED", raising=False)
    assert xquik_search_enabled() is False

    monkeypatch.setenv("XQUIK_SEARCH_ENABLED", "true")
    tool = XquikSearchTool(client_factory=FakeClientFactory())

    assert xquik_search_enabled() is True
    assert tool.metadata.dangerous is True
    assert tool.metadata.session_aware is False


def test_xquik_search_registration_follows_enablement(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("XQUIK_SEARCH_ENABLED", "false")
    disabled_names = {tool_class.__name__ for tool_class in _builtin_tool_classes()}

    monkeypatch.setenv("XQUIK_SEARCH_ENABLED", "true")
    enabled_names = {tool_class.__name__ for tool_class in _builtin_tool_classes()}

    assert "XquikSearchTool" not in disabled_names
    assert "XquikSearchTool" in enabled_names


def test_xquik_search_requires_api_key(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.delenv("XQUIK_API_KEY", raising=False)

    result = _run(
        XquikSearchTool(client_factory=FakeClientFactory()),
        query="AI agents",
    )

    assert result.success is False
    assert result.error == "Set XQUIK_API_KEY before using Xquik search."


def test_xquik_search_calls_normalized_paid_read_contract(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("XQUIK_API_KEY", "test-key")
    factory = FakeClientFactory(
        FakeResponse(
            {
                "tweets": [{"id": "1", "text": "hello"}],
                "has_more": True,
                "next_cursor": "next-page",
            }
        )
    )

    result = _run(
        XquikSearchTool(client_factory=factory),
        query="AI agents",
        allowed_handles="OpenAI,anthropic",
        from_date="2026-06-01",
        to_date="2026-06-30T12:00:00+03:00",
        limit=500,
        cursor="current-page",
        top=True,
    )

    assert result.success is True
    assert result.output["tweets"] == [{"id": "1", "text": "hello"}]
    assert result.output["has_more"] is True
    assert result.output["next_cursor"] == "next-page"
    assert "untrusted" in result.output["content_warning"]
    assert result.metadata["requested_limit"] == 200
    assert result.metadata["paid_read"] is True
    assert len(factory.calls) == 1

    call = factory.calls[0]
    assert call["url"] == "https://xquik.com/api/v1/x/tweets/search"
    assert call["headers"]["x-api-key"] == "test-key"
    assert call["headers"]["xquik-api-contract"] == "2026-04-29"
    assert call["params"] == {
        "q": "(from:OpenAI OR from:anthropic) (AI agents)",
        "queryType": "Top",
        "limit": "200",
        "cursor": "current-page",
        "sinceDate": "2026-06-01",
        "untilTime": "2026-06-30T09:00:00Z",
    }


def test_xquik_search_rejects_invalid_filters(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("XQUIK_API_KEY", "test-key")
    tool = XquikSearchTool(client_factory=FakeClientFactory())

    invalid_handle = _run(
        tool,
        query="AI",
        allowed_handles="https://x.com/openai",
    )
    conflicting_handles = _run(
        tool,
        query="AI",
        allowed_handles="openai",
        excluded_handles="anthropic",
    )
    invalid_date = _run(tool, query="AI", from_date="2026-02-30")
    missing_timezone = _run(
        tool,
        query="AI",
        from_date="2026-06-01T12:00:00",
    )

    assert invalid_handle.error == "Use valid X handles without URLs."
    assert (
        conflicting_handles.error
        == "Choose allowed handles or excluded handles, not both."
    )
    assert invalid_date.error == "Use a valid YYYY-MM-DD date."
    assert missing_timezone.error == "Include a timezone in ISO 8601 timestamps."


def test_xquik_search_sanitizes_api_errors(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("XQUIK_API_KEY", "test-key")
    factory = FakeClientFactory(
        FakeResponse(
            {
                "error": {"code": "insufficient_credits"},
                "message": "private-upstream-detail",
            },
            status_code=402,
        )
    )

    result = _run(
        XquikSearchTool(client_factory=factory),
        query="AI agents",
    )

    assert result.success is False
    assert result.error == (
        "Xquik credits are insufficient. Review the account before retrying."
    )
    assert "private-upstream-detail" not in result.error
    assert result.metadata == {
        "status_code": 402,
        "error_code": "insufficient_credits",
    }


def test_xquik_search_handles_timeout_and_invalid_json(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("XQUIK_API_KEY", "test-key")
    timeout_result = _run(
        XquikSearchTool(
            client_factory=FakeClientFactory(error=httpx.TimeoutException("timed out"))
        ),
        query="AI agents",
    )
    invalid_json_result = _run(
        XquikSearchTool(
            client_factory=FakeClientFactory(
                FakeResponse(None, json_error=ValueError("invalid"))
            )
        ),
        query="AI agents",
    )

    assert timeout_result.error == "Xquik search timed out after 30s. Retry later."
    assert invalid_json_result.error == "Xquik returned invalid JSON. Retry later."


def test_xquik_search_uses_safe_environment_defaults(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("XQUIK_SEARCH_RATE_LIMIT", "invalid")
    monkeypatch.setenv("XQUIK_SEARCH_TIMEOUT", "999")

    tool = XquikSearchTool(client_factory=FakeClientFactory())

    assert tool.metadata.rate_limit == 30
    assert tool.metadata.timeout_seconds == 120
