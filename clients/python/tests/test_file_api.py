from types import SimpleNamespace

import pytest

from shannon import ShannonClient, errors
from shannon.client import AsyncShannonClient


@pytest.mark.asyncio
async def test_async_list_session_files_parses_response_body():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "success": True,
                "files": [
                    {
                        "name": "report.md",
                        "path": "reports/report.md",
                        "is_dir": False,
                        "size_bytes": 128,
                    }
                ],
            }

    class StubClient:
        async def get(self, url, params=None, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    files = await client.list_session_files("session-1", path="reports")

    assert len(files) == 1
    assert files[0].path == "reports/report.md"
    assert files[0].size_bytes == 128


@pytest.mark.asyncio
async def test_async_download_session_file_quotes_nested_path():
    captured = {}

    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "success": True,
                "content": "# Report",
                "content_type": "text/markdown",
                "size_bytes": 8,
            }

    class StubClient:
        async def get(self, url, headers=None, timeout=None):
            captured["url"] = url
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    result = await client.download_session_file("session-1", "reports/q1 report.md")

    assert captured["url"].endswith("/api/v1/sessions/session-1/files/reports/q1%20report.md")
    assert result.content == "# Report"
    assert result.content_type == "text/markdown"


@pytest.mark.asyncio
async def test_async_list_memory_files_checks_success_flag():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "success": True,
                "files": [
                    {
                        "name": "profile.md",
                        "path": "profile.md",
                        "is_dir": False,
                        "size_bytes": 64,
                    }
                ],
            }

    class StubClient:
        async def get(self, url, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    files = await client.list_memory_files()

    assert len(files) == 1
    assert files[0].name == "profile.md"


@pytest.mark.asyncio
async def test_async_list_session_files_raises_on_404():
    class StubResponse:
        status_code = 404
        request = SimpleNamespace(url=SimpleNamespace(path="/api/v1/sessions/session-1/files"))
        text = "session not found"

        def json(self):
            return {"error": "session not found"}

    class StubClient:
        async def get(self, url, params=None, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    with pytest.raises(errors.SessionNotFoundError, match="session not found"):
        await client.list_session_files("session-1")


@pytest.mark.asyncio
async def test_async_download_memory_file_raises_on_unsuccessful_body():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "success": False,
                "error": "memory backend unavailable",
            }

    class StubClient:
        async def get(self, url, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    with pytest.raises(errors.ShannonError, match="memory backend unavailable"):
        await client.download_memory_file("profile.md")


def test_sync_download_memory_file_wraps_async_result():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "success": True,
                "content": "hello",
                "content_type": "text/plain",
                "size_bytes": 5,
            }

    class StubClient:
        async def get(self, url, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = ShannonClient(base_url="http://example")
    client._async_client._ensure_client = _ensure_client  # type: ignore

    result = client.download_memory_file("profile.md")

    assert result.content == "hello"
    assert result.size_bytes == 5
