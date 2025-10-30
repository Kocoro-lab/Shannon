"""HTTP error handling tests (no network)."""

import pytest
import httpx

from shannon.client import AsyncShannonClient
from shannon import errors


def make_response(status: int, body: str = "", url: str = "http://test") -> httpx.Response:
    req = httpx.Request("GET", url)
    return httpx.Response(status, request=req, content=body.encode("utf-8"))


@pytest.mark.asyncio
async def test_error_mapping_authentication():
    c = AsyncShannonClient()
    resp = make_response(401, '{"error":"Unauthorized"}')
    with pytest.raises(errors.AuthenticationError):
        c._handle_http_error(resp)


@pytest.mark.asyncio
async def test_error_mapping_task_not_found():
    c = AsyncShannonClient()
    resp = make_response(404, '{"error":"Task not found"}', url="http://test/api/v1/tasks/123")
    with pytest.raises(errors.TaskNotFoundError):
        c._handle_http_error(resp)


@pytest.mark.asyncio
async def test_error_mapping_session_not_found():
    c = AsyncShannonClient()
    resp = make_response(404, '{"error":"Session not found"}', url="http://test/api/v1/sessions/abc")
    with pytest.raises(errors.SessionNotFoundError):
        c._handle_http_error(resp)


@pytest.mark.asyncio
async def test_error_mapping_validation():
    c = AsyncShannonClient()
    resp = make_response(400, '{"error":"Bad input"}')
    with pytest.raises(errors.ValidationError):
        c._handle_http_error(resp)


@pytest.mark.asyncio
async def test_error_mapping_server_error():
    c = AsyncShannonClient()
    for code in (500, 502, 503):
        resp = make_response(code, '{"error":"Internal"}')
        with pytest.raises(errors.ServerError):
            c._handle_http_error(resp)


@pytest.mark.asyncio
async def test_error_mapping_permission_and_ratelimit():
    c = AsyncShannonClient()
    resp_forbidden = make_response(403, '{"error":"Forbidden"}')
    with pytest.raises(errors.PermissionDeniedError):
        c._handle_http_error(resp_forbidden)
    resp_rl = make_response(429, '{"error":"Rate limit"}')
    with pytest.raises(errors.RateLimitError):
        c._handle_http_error(resp_rl)


@pytest.mark.asyncio
async def test_timeout_wrapped(monkeypatch):
    class FakeClient:
        async def get(self, *args, **kwargs):  # mimics httpx.AsyncClient.get
            raise httpx.ReadTimeout("timeout")

    c = AsyncShannonClient()

    async def fake_ensure():
        return FakeClient()

    monkeypatch.setattr(c, "_ensure_client", fake_ensure)

    with pytest.raises(errors.ConnectionError):
        await c.get_status("task-1")
