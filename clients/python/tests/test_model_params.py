import pytest

from shannon.client import AsyncShannonClient, ShannonClient


@pytest.mark.asyncio
async def test_async_submit_task_includes_model_params():
    captured = {}

    class StubResponse:
        def __init__(self):
            self.status_code = 200
            self.headers = {"X-Workflow-ID": "wf-1"}

        def json(self):
            return {"task_id": "t-1"}

    class StubClient:
        async def post(self, url, json=None, headers=None, timeout=None):
            captured["url"] = url
            captured["json"] = json or {}
            captured["headers"] = headers or {}
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    ac = AsyncShannonClient(base_url="http://example")
    # Monkeypatch ensure_client
    ac._ensure_client = _ensure_client  # type: ignore

    handle = await ac.submit_task(
        "Do it",
        model_tier="small",
        model_override="gpt-5-nano-2025-08-07",
        provider_override="openai",
        mode="simple",
    )

    assert handle.workflow_id == "wf-1"
    assert captured["json"]["model_tier"] == "small"
    assert captured["json"]["model_override"] == "gpt-5-nano-2025-08-07"
    assert captured["json"]["provider_override"] == "openai"
    assert captured["json"]["mode"] == "simple"


@pytest.mark.asyncio
async def test_async_submit_and_stream_includes_model_params():
    captured = {}

    class StubResponse:
        def __init__(self):
            self.status_code = 201
            self.headers = {"X-Workflow-ID": "wf-2"}

        def json(self):
            return {"task_id": "t-2", "stream_url": "/api/v1/stream/sse?workflow_id=wf-2"}

    class StubClient:
        async def post(self, url, json=None, headers=None, timeout=None):
            captured["url"] = url
            captured["json"] = json or {}
            captured["headers"] = headers or {}
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    ac = AsyncShannonClient(base_url="http://example")
    ac._ensure_client = _ensure_client  # type: ignore

    handle, stream_url = await ac.submit_and_stream(
        "Start stream",
        model_tier="medium",
        model_override="gpt-5-mini-2025-08-07",
        provider_override="openai",
        mode="standard",
    )

    assert stream_url.endswith("workflow_id=wf-2")
    assert captured["json"]["model_tier"] == "medium"
    assert captured["json"]["model_override"] == "gpt-5-mini-2025-08-07"
    assert captured["json"]["provider_override"] == "openai"
    assert captured["json"]["mode"] == "standard"


def test_sync_submit_task_includes_model_params():
    captured = {}

    class StubResponse:
        def __init__(self):
            self.status_code = 200
            self.headers = {"X-Workflow-ID": "wf-3"}

        def json(self):
            return {"task_id": "t-3"}

    class StubClient:
        async def post(self, url, json=None, headers=None, timeout=None):
            captured["url"] = url
            captured["json"] = json or {}
            captured["headers"] = headers or {}
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    c = ShannonClient(base_url="http://example")
    # Monkeypatch underlying async ensure_client
    c._async_client._ensure_client = _ensure_client  # type: ignore

    handle = c.submit_task(
        "Sync do it",
        model_tier="large",
        model_override="gpt-5-2025-08-07",
        provider_override="openai",
        mode="complex",
    )

    assert handle.workflow_id == "wf-3"
    assert captured["json"]["model_tier"] == "large"
    assert captured["json"]["model_override"] == "gpt-5-2025-08-07"
    assert captured["json"]["provider_override"] == "openai"
    assert captured["json"]["mode"] == "complex"

