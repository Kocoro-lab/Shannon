import pytest

from shannon import ShannonClient
from shannon.client import AsyncShannonClient


@pytest.mark.asyncio
async def test_async_list_sessions_prefers_external_id_and_context_title():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "sessions": [
                    {
                        "session_id": "3b15df8f-aaaa-bbbb-cccc-1234567890ab",
                        "user_id": "user-1",
                        "created_at": "2026-04-13T12:00:00Z",
                        "updated_at": "2026-04-13T12:01:00Z",
                        "task_count": 1,
                        "tokens_used": 25,
                        "context": {
                            "external_id": "demo-session",
                            "title": "Demo Title",
                        },
                    }
                ],
                "total_count": 1,
            }

    class StubClient:
        async def get(self, url, params=None, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    sessions, total = await client.list_sessions()

    assert total == 1
    assert len(sessions) == 1
    assert sessions[0].session_id == "demo-session"
    assert sessions[0].title == "Demo Title"


@pytest.mark.asyncio
async def test_async_get_session_prefers_external_id_and_context_title():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "session_id": "3b15df8f-aaaa-bbbb-cccc-1234567890ab",
                "user_id": "user-1",
                "created_at": "2026-04-13T12:00:00Z",
                "updated_at": "2026-04-13T12:01:00Z",
                "tokens_used": 25,
                "task_count": 1,
                "context": {
                    "external_id": "demo-session",
                    "title": "Demo Title",
                },
            }

    class StubClient:
        async def get(self, url, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    session = await client.get_session("demo-session")

    assert session.session_id == "demo-session"
    assert session.title == "Demo Title"


def test_sync_get_session_uses_external_id():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "session_id": "3b15df8f-aaaa-bbbb-cccc-1234567890ab",
                "user_id": "user-1",
                "created_at": "2026-04-13T12:00:00Z",
                "updated_at": "2026-04-13T12:01:00Z",
                "tokens_used": 25,
                "task_count": 1,
                "context": {
                    "external_id": "demo-session",
                    "title": "Demo Title",
                },
            }

    class StubClient:
        async def get(self, url, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = ShannonClient(base_url="http://example")
    client._async_client._ensure_client = _ensure_client  # type: ignore

    session = client.get_session("demo-session")

    assert session.session_id == "demo-session"
    assert session.title == "Demo Title"
