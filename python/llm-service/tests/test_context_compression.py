from typing import Any, Dict, List

from fastapi import FastAPI
from fastapi.testclient import TestClient

from llm_service.api.context import router


class RecordingProviders:
    def __init__(self) -> None:
        self.calls: List[Dict[str, Any]] = []

    def is_configured(self) -> bool:
        return True

    async def generate_completion(self, **kwargs: Any) -> Dict[str, Any]:
        self.calls.append(kwargs)
        return {
            "output_text": "middle summary",
            "model": "test-model",
            "usage": {"total_tokens": 10},
        }


class FailingProviders:
    def is_configured(self) -> bool:
        return True

    async def generate_completion(self, **kwargs: Any) -> Dict[str, Any]:
        raise RuntimeError("secret-provider-detail")


def _client(providers: Any = None) -> TestClient:
    app = FastAPI()
    app.include_router(router)
    if providers is not None:
        app.state.providers = providers
    return TestClient(app)


def _messages(count: int) -> List[Dict[str, str]]:
    return [
        {
            "role": "user" if index % 2 == 0 else "assistant",
            "content": f"message-{index}",
        }
        for index in range(count)
    ]


def test_context_compression_summarizes_all_preselected_messages() -> None:
    providers = RecordingProviders()

    response = _client(providers).post(
        "/context/compress",
        json={
            "messages": _messages(7)[3:],
            "target_tokens": 400,
            "compress_all_messages": True,
        },
    )

    assert response.status_code == 200
    assert response.json()["summary"] == "middle summary"
    assert len(providers.calls) == 1
    prompt = providers.calls[0]["messages"][1]["content"]
    assert "message-3" in prompt
    assert "message-6" in prompt


def test_context_compression_rejects_empty_preselected_messages() -> None:
    providers = RecordingProviders()

    response = _client(providers).post(
        "/context/compress",
        json={
            "messages": [],
            "compress_all_messages": True,
        },
    )

    assert response.status_code == 400
    assert response.json()["detail"] == "messages required"
    assert providers.calls == []


def test_context_compression_keeps_legacy_recent_history_default() -> None:
    response = _client().post(
        "/context/compress",
        json={"messages": _messages(30)},
    )

    assert response.status_code == 200
    assert response.json()["summary"].startswith("message-10\n")
    assert "message-9" not in response.json()["summary"]


def test_context_compression_hides_provider_error_details() -> None:
    response = _client(FailingProviders()).post(
        "/context/compress",
        json={"messages": _messages(3)},
    )

    assert response.status_code == 500
    assert response.json()["detail"] == "context compression failed"
    assert "secret-provider-detail" not in response.text
