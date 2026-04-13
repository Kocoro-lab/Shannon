import pytest

from shannon.client import AsyncShannonClient
from shannon.models import EventType


@pytest.mark.asyncio
async def test_async_stream_emits_done_event_for_done_sse_payload(monkeypatch):
    class StubResponse:
        status_code = 200

        async def __aenter__(self):
            return self

        async def __aexit__(self, exc_type, exc, tb):
            return False

        async def aiter_lines(self):
            for line in [
                'event: WORKFLOW_COMPLETED',
                'data: {"type":"WORKFLOW_COMPLETED","workflow_id":"wf-1","message":"done"}',
                "",
                "event: done",
                "data: [DONE]",
                "",
            ]:
                yield line

    class StubHTTPClient:
        def __init__(self, timeout=None):
            self.timeout = timeout

        async def __aenter__(self):
            return self

        async def __aexit__(self, exc_type, exc, tb):
            return False

        def stream(self, method, url, params=None, headers=None):
            return StubResponse()

    monkeypatch.setattr("shannon.client.httpx.AsyncClient", StubHTTPClient)

    client = AsyncShannonClient(base_url="http://example")
    events = []

    async for event in client.stream("wf-1", timeout=5, total_timeout=5):
        events.append(event)

    assert [event.type for event in events] == [
        "WORKFLOW_COMPLETED",
        EventType.STREAM_END.value,
    ]
    assert events[1].message == "[DONE]"
