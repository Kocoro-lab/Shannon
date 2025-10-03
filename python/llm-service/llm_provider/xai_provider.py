"""xAI Provider implementation.

Provides a thin wrapper around the xAI (Grok) REST API which is intentionally
OpenAI-compatible. We keep the logic simple and reuse the OpenAI chat
completions surface while accounting for a few xAI-specific quirks (reasoning
models ignoring certain parameters, Live Search surcharges, etc.).
"""

from __future__ import annotations

import os
import time
from datetime import datetime
from typing import Any, AsyncIterator, Dict, List, Optional

from openai import AsyncOpenAI
from tenacity import retry, stop_after_attempt, wait_exponential

from .base import (
    CompletionRequest,
    CompletionResponse,
    LLMProvider,
    TokenCounter,
    TokenUsage,
)

# Live Search pricing: $25 per 1k sources → $0.025 per source
_LIVE_SEARCH_PRICE_PER_SOURCE = 0.025


class XAIProvider(LLMProvider):
    """xAI provider using the OpenAI-compatible chat completions API."""

    def __init__(self, config: Dict[str, Any]):
        api_key = config.get("api_key") or os.getenv("XAI_API_KEY")
        if not api_key:
            raise ValueError("xAI API key not provided")

        base_url = config.get("base_url", "https://api.x.ai/v1").rstrip("/")
        timeout = int(config.get("timeout", 60) or 60)

        self.client = AsyncOpenAI(api_key=api_key, base_url=base_url, timeout=timeout)
        self.base_url = base_url

        # Preserve original config but ensure a provider name for downstream logs
        effective_config = dict(config)
        effective_config.setdefault("name", "xai")

        super().__init__(effective_config)

    def _initialize_models(self) -> None:
        self._load_models_from_config(allow_empty=True)
        if not self.models:
            self._add_default_models()

    def _add_default_models(self) -> None:
        """Populate a minimal catalog if models were not provided via config."""

        defaults: Dict[str, Dict[str, Any]] = {
            "grok-4": {
                "model_id": "grok-4",
                "tier": "medium",
                "context_window": 131072,
                "max_tokens": 8192,
                "supports_functions": True,
                "supports_streaming": True,
                "supports_reasoning": True,
            },
            "grok-4-fast": {
                "model_id": "grok-4-fast",
                "tier": "small",
                "context_window": 65536,
                "max_tokens": 8192,
                "supports_functions": True,
                "supports_streaming": True,
                "supports_reasoning": False,
            },
        }

        for alias, meta in defaults.items():
            self.models[alias] = self._make_model_config("xai", alias, meta)

    def count_tokens(self, messages: List[Dict[str, Any]], model: str) -> int:
        return TokenCounter.count_messages_tokens(messages, model)

    def _resolve_alias(self, model_id: str) -> str:
        for alias, cfg in self.models.items():
            if cfg.model_id == model_id:
                return alias
        return model_id

    def _supports_reasoning(self, model_alias: str) -> bool:
        config = self.models.get(model_alias)
        if not config:
            return False
        caps = getattr(config, "capabilities", None)
        return bool(getattr(caps, "supports_reasoning", False))

    @retry(stop=stop_after_attempt(3), wait=wait_exponential(multiplier=0.5, min=1, max=8))
    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        model_config = self.resolve_model_config(request)
        model_id = model_config.model_id
        model_alias = self._resolve_alias(model_id)
        supports_reasoning = self._supports_reasoning(model_alias)

        payload: Dict[str, Any] = {
            "model": model_id,
            "messages": request.messages,
            "temperature": request.temperature,
            "top_p": request.top_p,
        }

        if request.max_tokens:
            payload["max_tokens"] = request.max_tokens

        if request.functions and model_config.supports_functions:
            payload["functions"] = request.functions
            if request.function_call:
                payload["function_call"] = request.function_call

        if request.response_format:
            payload["response_format"] = request.response_format

        if request.user:
            payload["user"] = request.user

        if request.seed is not None:
            payload["seed"] = request.seed

        # xAI reasoning models reject frequency/presence penalties and custom stop sequences
        if not supports_reasoning:
            payload["frequency_penalty"] = request.frequency_penalty
            payload["presence_penalty"] = request.presence_penalty
            if request.stop:
                payload["stop"] = request.stop

        start = time.time()
        try:
            response = await self.client.chat.completions.create(**payload)
        except Exception as exc:
            raise Exception(f"xAI API error ({self.base_url}): {exc}")
        latency_ms = int((time.time() - start) * 1000)

        choice = response.choices[0]
        message = choice.message
        content = (message.content or "") if message else ""

        prompt_tokens = 0
        completion_tokens = 0
        total_tokens = 0
        num_sources_used = 0

        usage = getattr(response, "usage", None)
        if usage:
            try:
                prompt_tokens = int(getattr(usage, "prompt_tokens", 0))
                completion_tokens = int(getattr(usage, "completion_tokens", 0))
                total_tokens = int(
                    getattr(usage, "total_tokens", prompt_tokens + completion_tokens)
                )
                num_sources_used = int(getattr(usage, "num_sources_used", 0) or 0)
            except Exception:
                prompt_tokens = completion_tokens = total_tokens = 0

        if total_tokens == 0:
            prompt_tokens = self.count_tokens(request.messages, model_id)
            completion_tokens = self.count_tokens(
                [{"role": "assistant", "content": content}], model_id
            )
            total_tokens = prompt_tokens + completion_tokens

        base_cost = self.estimate_cost(prompt_tokens, completion_tokens, model_alias)
        live_search_cost = num_sources_used * _LIVE_SEARCH_PRICE_PER_SOURCE
        estimated_cost = base_cost + live_search_cost

        finish_reason = getattr(choice, "finish_reason", None) or "stop"
        function_call: Optional[Dict[str, Any]] = None
        if message and hasattr(message, "function_call"):
            function_call = message.function_call  # type: ignore[assignment]
        elif message and hasattr(message, "tool_calls"):
            tool_calls = getattr(message, "tool_calls", None)
            if tool_calls:
                first_tool = tool_calls[0]
                if isinstance(first_tool, dict):
                    function_call = first_tool

        created_ts = getattr(response, "created", None)
        created_at = (
            datetime.utcfromtimestamp(created_ts) if isinstance(created_ts, (int, float)) else None
        )

        usage_payload = TokenUsage(
            input_tokens=prompt_tokens,
            output_tokens=completion_tokens,
            total_tokens=total_tokens,
            estimated_cost=estimated_cost,
        )

        response_obj = CompletionResponse(
            content=content,
            model=model_id,
            provider="xai",
            usage=usage_payload,
            finish_reason=finish_reason,
            function_call=function_call,
            request_id=getattr(response, "id", None),
            latency_ms=latency_ms,
        )

        if created_at:
            response_obj.created_at = created_at

        return response_obj

    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator[str]:
        model_config = self.resolve_model_config(request)
        model_id = model_config.model_id
        supports_reasoning = self._supports_reasoning(self._resolve_alias(model_id))

        payload: Dict[str, Any] = {
            "model": model_id,
            "messages": request.messages,
            "temperature": request.temperature,
            "stream": True,
        }

        if request.max_tokens:
            payload["max_tokens"] = request.max_tokens

        if request.functions and model_config.supports_functions:
            payload["functions"] = request.functions
            if request.function_call:
                payload["function_call"] = request.function_call

        if request.response_format:
            payload["response_format"] = request.response_format

        if request.user:
            payload["user"] = request.user

        if request.seed is not None:
            payload["seed"] = request.seed

        if not supports_reasoning and request.stop:
            payload["stop"] = request.stop

        try:
            stream = await self.client.chat.completions.create(**payload)
            async for chunk in stream:
                delta = chunk.choices[0].delta
                if delta and getattr(delta, "content", None):
                    yield delta.content
        except Exception as exc:
            raise Exception(f"xAI streaming error ({self.base_url}): {exc}")
