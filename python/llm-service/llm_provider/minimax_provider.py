"""
MiniMax LLM Provider Implementation

MiniMax exposes an OpenAI-compatible chat completions API.
Key constraints:
  - Temperature must be in (0.0, 1.0] — values <= 0 are clamped to 0.01
  - M2.7 models may emit <think>...</think> blocks; these are stripped
  - API base URL: https://api.minimax.io/v1
"""

from __future__ import annotations

import os
import re
import time
from typing import Any, AsyncIterator, Dict, List, Optional

from openai import AsyncOpenAI
from tenacity import retry, stop_after_attempt, wait_exponential

from .base import (
    CompletionRequest,
    CompletionResponse,
    LLMProvider,
    TokenCounter,
    TokenUsage,
    prepare_openai_messages,
)

# Regex to strip <think>...</think> blocks emitted by reasoning-capable models
_THINK_TAG_RE = re.compile(r"<think>.*?</think>", re.DOTALL)

_MINIMAX_BASE_URL = "https://api.minimax.io/v1"
_TEMP_MIN = 0.01  # MiniMax requires temperature > 0


def _clamp_temperature(temp: Optional[float]) -> float:
    """Ensure temperature is in (0.0, 1.0] as required by MiniMax."""
    if temp is None:
        return 0.7
    if temp <= 0.0:
        return _TEMP_MIN
    if temp > 1.0:
        return 1.0
    return temp


def _strip_think_tags(text: str) -> str:
    """Remove <think>...</think> reasoning blocks from model output."""
    return _THINK_TAG_RE.sub("", text).strip()


class MiniMaxProvider(LLMProvider):
    """MiniMax LLM provider using the OpenAI-compatible chat completions API."""

    def __init__(self, config: Dict[str, Any]):
        api_key = config.get("api_key") or os.getenv("MINIMAX_API_KEY")
        if not api_key:
            raise ValueError("MiniMax API key not provided (set MINIMAX_API_KEY)")

        base_url = config.get("base_url", _MINIMAX_BASE_URL).rstrip("/")
        timeout = int(config.get("timeout", 60) or 60)

        self.client = AsyncOpenAI(api_key=api_key, base_url=base_url, timeout=timeout)
        self.base_url = base_url

        effective_config = dict(config)
        effective_config.setdefault("name", "minimax")

        super().__init__(effective_config)

    # ------------------------------------------------------------------
    # Model initialisation
    # ------------------------------------------------------------------

    def _initialize_models(self) -> None:
        self._load_models_from_config(allow_empty=True)
        if not self.models:
            self._add_default_models()

    def _add_default_models(self) -> None:
        """Fallback catalog when models.yaml is not loaded."""
        defaults: Dict[str, Dict[str, Any]] = {
            "MiniMax-M3": {
                "model_id": "MiniMax-M3",
                "tier": "medium",
                "context_window": 524288,
                "max_tokens": 131072,
                "supports_functions": True,
                "supports_streaming": True,
                "supports_vision": True,
            },
            "MiniMax-M2.7": {
                "model_id": "MiniMax-M2.7",
                "tier": "medium",
                "context_window": 204800,
                "max_tokens": 4096,
                "supports_functions": True,
                "supports_streaming": True,
            },
            "MiniMax-M2.7-highspeed": {
                "model_id": "MiniMax-M2.7-highspeed",
                "tier": "small",
                "context_window": 204800,
                "max_tokens": 4096,
                "supports_functions": True,
                "supports_streaming": True,
            },
        }
        for alias, meta in defaults.items():
            self.models[alias] = self._make_model_config("minimax", alias, meta)

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    def count_tokens(self, messages: List[Dict[str, Any]], model: str) -> int:
        return TokenCounter.count_messages_tokens(messages, model)

    def _resolve_alias(self, model_id: str) -> str:
        for alias, cfg in self.models.items():
            if cfg.model_id == model_id:
                return alias
        return model_id

    # ------------------------------------------------------------------
    # Completion
    # ------------------------------------------------------------------

    @retry(
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=0.5, min=1, max=8),
    )
    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        model_config = self.resolve_model_config(request)
        model_id = model_config.model_id
        model_alias = self._resolve_alias(model_id)

        messages = prepare_openai_messages(request.messages)

        payload: Dict[str, Any] = {
            "model": model_id,
            "messages": messages,
            "temperature": _clamp_temperature(request.temperature),
        }

        if request.max_tokens:
            payload["max_tokens"] = min(request.max_tokens, model_config.max_tokens)

        if request.stop:
            payload["stop"] = request.stop

        if request.functions and model_config.supports_functions:
            payload["functions"] = request.functions
            if request.function_call:
                payload["function_call"] = request.function_call

        if request.seed is not None:
            payload["seed"] = request.seed

        # MiniMax does not support response_format with json_object; omit it.
        # (Sending an unsupported response_format causes a 400 error.)

        if request.user:
            payload["user"] = request.user

        start = time.time()
        try:
            response = await self.client.chat.completions.create(**payload)
        except Exception as exc:
            raise Exception(f"MiniMax API error ({self.base_url}): {exc}")
        latency_ms = int((time.time() - start) * 1000)

        choice = response.choices[0]
        message = choice.message

        raw_content = (message.content or "") if message else ""
        content = _strip_think_tags(raw_content)

        prompt_tokens = completion_tokens = total_tokens = 0
        usage = getattr(response, "usage", None)
        if usage:
            try:
                prompt_tokens = int(getattr(usage, "prompt_tokens", 0))
                completion_tokens = int(getattr(usage, "completion_tokens", 0))
                total_tokens = int(
                    getattr(usage, "total_tokens", prompt_tokens + completion_tokens)
                )
            except Exception:
                prompt_tokens = completion_tokens = total_tokens = 0

        if total_tokens == 0:
            prompt_tokens = self.count_tokens(request.messages, model_id)
            completion_tokens = self.count_tokens(
                [{"role": "assistant", "content": content}], model_id
            )
            total_tokens = prompt_tokens + completion_tokens

        cost = self.estimate_cost(prompt_tokens, completion_tokens, model_alias)

        function_call: Optional[Dict[str, Any]] = None
        if message and hasattr(message, "function_call") and message.function_call:
            function_call = message.function_call  # type: ignore[assignment]

        return CompletionResponse(
            content=content,
            model=model_id,
            provider=self.config.get("name", "minimax"),
            usage=TokenUsage(
                input_tokens=prompt_tokens,
                output_tokens=completion_tokens,
                total_tokens=total_tokens,
                estimated_cost=cost,
            ),
            finish_reason=getattr(choice, "finish_reason", None) or "stop",
            function_call=function_call,
            request_id=getattr(response, "id", None),
            latency_ms=latency_ms,
        )

    # ------------------------------------------------------------------
    # Streaming
    # ------------------------------------------------------------------

    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator[str]:
        model_config = self.resolve_model_config(request)
        model_id = model_config.model_id

        messages = prepare_openai_messages(request.messages)

        payload: Dict[str, Any] = {
            "model": model_id,
            "messages": messages,
            "temperature": _clamp_temperature(request.temperature),
            "stream": True,
        }

        if request.max_tokens:
            payload["max_tokens"] = request.max_tokens

        # Buffer to strip think-tags that may span multiple chunks
        buffer = ""
        in_think = False

        try:
            stream = await self.client.chat.completions.create(**payload)
            async for chunk in stream:
                if chunk.choices and chunk.choices[0].delta.content:
                    delta = chunk.choices[0].delta.content
                    buffer += delta

                    # Flush safe (non-think) portions
                    while True:
                        if in_think:
                            end = buffer.find("</think>")
                            if end == -1:
                                break
                            buffer = buffer[end + len("</think>"):]
                            in_think = False
                        else:
                            start = buffer.find("<think>")
                            if start == -1:
                                text = buffer
                                buffer = ""
                                if text:
                                    yield text
                                break
                            if start > 0:
                                yield buffer[:start]
                            buffer = buffer[start + len("<think>"):]
                            in_think = True

                if getattr(chunk, "usage", None):
                    yield {
                        "usage": {
                            "total_tokens": chunk.usage.total_tokens,
                            "input_tokens": chunk.usage.prompt_tokens,
                            "output_tokens": chunk.usage.completion_tokens,
                        },
                        "model": getattr(chunk, "model", model_id),
                        "provider": "minimax",
                    }

            # Flush any remaining safe content
            if buffer and not in_think:
                yield buffer

        except Exception as exc:
            raise Exception(f"MiniMax streaming error ({self.base_url}): {exc}")
