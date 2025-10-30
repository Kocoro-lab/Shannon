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
    resp = make_response(404, '{"error":"Task not found"}')
    with pytest.raises(errors.TaskNotFoundError):
        c._handle_http_error(resp)


@pytest.mark.asyncio
async def test_error_mapping_session_not_found():
    c = AsyncShannonClient()
    resp = make_response(404, '{"error":"Session not found"}')
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
    resp = make_response(500, '{"error":"Internal"}')
    with pytest.raises(errors.ConnectionError):
        c._handle_http_error(resp)


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

