import pytest

from shannon import ShannonClient
from shannon.client import AsyncShannonClient
from shannon.models import OpenAIChatMessage, OpenAIShannonOptions


@pytest.mark.asyncio
async def test_async_list_openai_models_parses_response_body():
    class StubResponse:
        status_code = 200
        headers = {}

        def json(self):
            return {
                "object": "list",
                "data": [
                    {
                        "id": "shannon-chat",
                        "object": "model",
                        "created": 123,
                        "owned_by": "shannon",
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

    models = await client.list_openai_models()

    assert len(models) == 1
    assert models[0].id == "shannon-chat"
    assert models[0].owned_by == "shannon"


@pytest.mark.asyncio
async def test_async_get_openai_model_reads_description_header():
    class StubResponse:
        status_code = 200
        headers = {"X-Model-Description": "General chat model"}

        def json(self):
            return {
                "id": "shannon-chat",
                "object": "model",
                "created": 123,
                "owned_by": "shannon",
            }

    class StubClient:
        async def get(self, url, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    model = await client.get_openai_model("shannon-chat")

    assert model.id == "shannon-chat"
    assert model.description == "General chat model"


@pytest.mark.asyncio
async def test_async_create_chat_completion_parses_usage_and_session_headers():
    captured = {}

    class StubResponse:
        status_code = 200
        headers = {
            "X-Session-ID": "session-1",
            "X-Shannon-Session-ID": "shannon-session-1",
        }

        def json(self):
            return {
                "id": "chatcmpl-123",
                "object": "chat.completion",
                "created": 123,
                "model": "shannon-chat",
                "choices": [
                    {
                        "index": 0,
                        "message": {
                            "role": "assistant",
                            "content": "hello world",
                        },
                        "finish_reason": "stop",
                    }
                ],
                "usage": {
                    "prompt_tokens": 10,
                    "completion_tokens": 20,
                    "total_tokens": 30,
                },
            }

    class StubClient:
        async def post(self, url, json=None, headers=None, timeout=None):
            captured["json"] = json or {}
            captured["headers"] = headers or {}
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = AsyncShannonClient(base_url="http://example")
    client._ensure_client = _ensure_client  # type: ignore

    completion = await client.create_chat_completion(
        [OpenAIChatMessage(role="user", content="hello")],
        model="shannon-chat",
        temperature=0.1,
        session_id="session-1",
        shannon_options=OpenAIShannonOptions(
            research_strategy="deep",
            context={"source": "sdk"},
        ),
    )

    assert captured["json"]["messages"] == [{"role": "user", "content": "hello"}]
    assert captured["json"]["shannon_options"]["research_strategy"] == "deep"
    assert captured["headers"]["X-Session-ID"] == "session-1"
    assert completion.choices[0].message is not None
    assert completion.choices[0].message.content == "hello world"
    assert completion.usage is not None
    assert completion.usage.total_tokens == 30
    assert completion.session_id == "session-1"
    assert completion.shannon_session_id == "shannon-session-1"


@pytest.mark.asyncio
async def test_async_stream_chat_completion_parses_chunks():
    async def _stream_sse_request(url, payload, headers, timeout=None):
        yield (
            '{"id":"chatcmpl-123","object":"chat.completion.chunk","created":123,'
            '"model":"shannon-chat","choices":[{"index":0,"delta":{"role":"assistant"}}]}',
            {"X-Session-ID": "session-1"},
        )
        yield (
            '{"id":"chatcmpl-123","object":"chat.completion.chunk","created":123,'
            '"model":"shannon-chat","choices":[{"index":0,"delta":{"content":"hello"}}],'
            '"shannon_events":[{"type":"AGENT_THINKING","agent_id":"alpha"}],'
            '"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}',
            {"X-Session-ID": "session-1"},
        )

    client = AsyncShannonClient(base_url="http://example")
    client._stream_sse_request = _stream_sse_request  # type: ignore

    chunks = []
    async for chunk in client.stream_chat_completion(
        [OpenAIChatMessage(role="user", content="hello")],
        include_usage=True,
    ):
        chunks.append(chunk)

    assert len(chunks) == 2
    assert chunks[0].choices[0].delta is not None
    assert chunks[0].choices[0].delta.role == "assistant"
    assert chunks[1].choices[0].delta is not None
    assert chunks[1].choices[0].delta.content == "hello"
    assert chunks[1].usage is not None
    assert chunks[1].usage.total_tokens == 15
    assert chunks[1].shannon_events[0].type == "AGENT_THINKING"
    assert chunks[1].session_id == "session-1"


def test_sync_create_completion_wraps_async_result():
    class StubResponse:
        status_code = 200

        def json(self):
            return {
                "id": "cmpl-123",
                "provider": "openai",
                "model": "gpt-5-mini",
            }

    class StubClient:
        async def post(self, url, json=None, headers=None, timeout=None):
            return StubResponse()

    async def _ensure_client():
        return StubClient()

    client = ShannonClient(base_url="http://example")
    client._async_client._ensure_client = _ensure_client  # type: ignore

    result = client.create_completion({"messages": [{"role": "user", "content": "hi"}]})

    assert result["id"] == "cmpl-123"
    assert result["provider"] == "openai"
