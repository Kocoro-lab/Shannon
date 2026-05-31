"""LiteLLM AI Gateway provider; embedded SDK, no proxy server."""

import logging
import time
from typing import Any, AsyncIterator, Dict, List, Optional, Tuple

from .anthropic_provider import _record_cache_metrics
from .base import (
    CompletionRequest,
    CompletionResponse,
    LLMProvider,
    TokenCounter,
    TokenUsage,
    prepare_openai_messages,
)

logger = logging.getLogger(__name__)


def _extract_text(message: Any) -> str:
    """Pull text from a chat message, falling back to reasoning fields when content is empty."""
    content = getattr(message, "content", None)
    if isinstance(content, str) and content.strip():
        return content
    if isinstance(content, list):
        parts = []
        for p in content:
            text = getattr(p, "text", None) or (
                p.get("text") if isinstance(p, dict) else None
            )
            if isinstance(text, str) and text.strip():
                parts.append(text.strip())
        if parts:
            return "\n\n".join(parts)
    for field in ("reasoning_content", "output", "thinking"):
        value = getattr(message, field, None)
        if isinstance(value, str) and value.strip():
            return value
        if isinstance(value, list):
            parts = []
            for p in value:
                if isinstance(p, dict) and isinstance(p.get("text"), str):
                    parts.append(p["text"])
                elif isinstance(p, str):
                    parts.append(p)
            if parts:
                return "\n\n".join(parts)
    return ""


def _cache_tokens(usage: Any) -> Tuple[int, int, int, int]:
    """Return ``(read, creation, 5m, 1h)`` from a litellm response.usage object.

    Anthropic-shape fields (``cache_read_input_tokens`` / ``cache_creation_input_tokens``
    plus the per-TTL ``cache_creation.ephemeral_*`` split) take priority. Falls
    back to OpenAI's ``prompt_tokens_details.cached_tokens`` when the
    Anthropic-side read field is zero.
    """
    if usage is None:
        return 0, 0, 0, 0
    cache_read = int(getattr(usage, "cache_read_input_tokens", 0) or 0)
    cache_creation = int(getattr(usage, "cache_creation_input_tokens", 0) or 0)
    cc = getattr(usage, "cache_creation", None)
    cache_5m = (
        int(getattr(cc, "ephemeral_5m_input_tokens", 0) or 0) if cc is not None else 0
    )
    cache_1h = (
        int(getattr(cc, "ephemeral_1h_input_tokens", 0) or 0) if cc is not None else 0
    )
    if cache_read == 0:
        ptd = getattr(usage, "prompt_tokens_details", None)
        if ptd is not None:
            cache_read = int(getattr(ptd, "cached_tokens", 0) or 0)
    return cache_read, cache_creation, cache_5m, cache_1h


class LiteLLMProvider(LLMProvider):
    """One Shannon provider, 100+ upstream backends via ``litellm.acompletion``."""

    def __init__(self, config: Dict[str, Any]):
        try:
            import litellm  # noqa: F401
        except ImportError as exc:
            raise ImportError(
                "litellm is required for LiteLLMProvider. "
                'Install via `pip install "litellm>=1.60,<1.85"`.'
            ) from exc

        self.api_key: Optional[str] = config.get("api_key")
        self.base_url: Optional[str] = config.get("base_url") or config.get("api_base")
        self.timeout: int = int(config.get("timeout", 60) or 60)
        merged: Dict[str, Any] = {"drop_params": True}
        merged.update(config.get("litellm_kwargs") or {})
        self.default_kwargs: Dict[str, Any] = merged

        super().__init__(config)

    def _initialize_models(self) -> None:
        self._load_models_from_config(allow_empty=False)

    def _resolve_alias(self, model_id: str) -> str:
        """Return the configured alias whose model_id matches, else passthrough."""
        for alias, cfg in self.models.items():
            if getattr(cfg, "model_id", None) == model_id:
                return alias
        return model_id

    def _request_kwargs(self, request: CompletionRequest, model: str) -> Dict[str, Any]:
        messages = prepare_openai_messages(request.messages)

        kwargs: Dict[str, Any] = dict(self.default_kwargs)
        kwargs["model"] = model
        kwargs["messages"] = messages
        kwargs["timeout"] = self.timeout

        if request.temperature is not None:
            kwargs["temperature"] = request.temperature
        if request.max_tokens:
            kwargs["max_tokens"] = request.max_tokens
        if request.top_p is not None:
            kwargs["top_p"] = request.top_p
        if request.frequency_penalty:
            kwargs["frequency_penalty"] = request.frequency_penalty
        if request.presence_penalty:
            kwargs["presence_penalty"] = request.presence_penalty
        if request.stop:
            kwargs["stop"] = request.stop
        if request.seed is not None:
            kwargs["seed"] = request.seed
        if request.response_format:
            kwargs["response_format"] = request.response_format
        if request.user:
            kwargs["user"] = request.user

        if request.functions:
            tools: List[Dict[str, Any]] = []
            for fn in request.functions:
                if isinstance(fn, dict) and fn.get("type") == "function":
                    tools.append(fn)
                elif isinstance(fn, dict):
                    tools.append({"type": "function", "function": fn})
            if tools:
                kwargs["tools"] = tools
                if request.function_call == "auto":
                    kwargs["tool_choice"] = "auto"
                elif request.function_call == "none":
                    kwargs["tool_choice"] = "none"
                elif (
                    isinstance(request.function_call, dict)
                    and "name" in request.function_call
                ):
                    kwargs["tool_choice"] = {
                        "type": "function",
                        "function": {"name": request.function_call["name"]},
                    }

        if self.api_key:
            kwargs["api_key"] = self.api_key
        if self.base_url:
            kwargs["api_base"] = self.base_url
        return kwargs

    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        import litellm

        model_config = self.resolve_model_config(request)
        model = model_config.model_id
        alias = self._resolve_alias(model)
        kwargs = self._request_kwargs(request, model)

        start_time = time.time()
        try:
            response = await litellm.acompletion(**kwargs)
        except Exception as e:
            raise Exception(f"LiteLLM completion error (model={model}): {e}")
        latency_ms = int((time.time() - start_time) * 1000)

        choice = response.choices[0]
        message = choice.message
        content_text = _extract_text(message)

        usage_obj = getattr(response, "usage", None)
        prompt_tokens = (
            int(getattr(usage_obj, "prompt_tokens", 0) or 0) if usage_obj else 0
        )
        completion_tokens = (
            int(getattr(usage_obj, "completion_tokens", 0) or 0) if usage_obj else 0
        )
        if prompt_tokens == 0 and completion_tokens == 0:
            prompt_tokens = self.count_tokens(request.messages, model)
            completion_tokens = self.count_tokens(
                [{"role": "assistant", "content": content_text}], model
            )
        total_tokens = prompt_tokens + completion_tokens

        cache_read, cache_creation, cache_5m, cache_1h = _cache_tokens(usage_obj)
        if cache_read or cache_creation:
            logger.info(
                f"LiteLLM prompt cache: read={cache_read}, creation={cache_creation} "
                f"(5m={cache_5m}, 1h={cache_1h}), input={prompt_tokens}, model={model}"
            )

        cost = self.estimate_cost(
            prompt_tokens,
            completion_tokens,
            alias,
            cache_read_tokens=cache_read,
            cache_creation_tokens=cache_creation,
            cache_creation_1h_tokens=cache_1h,
        )

        _record_cache_metrics(
            self.config.get("name", "litellm"),
            model,
            request.cache_source,
            cache_read,
            cache_creation,
            cache_1h,
        )

        function_call: Optional[Dict[str, Any]] = None
        tool_calls: Optional[List[Dict[str, Any]]] = None
        raw_tool_calls = getattr(message, "tool_calls", None)
        if raw_tool_calls:
            tool_calls = []
            for tc in raw_tool_calls:
                fn = getattr(tc, "function", None)
                if not fn:
                    continue
                tool_calls.append(
                    {
                        "id": getattr(tc, "id", "") or "",
                        "type": "function",
                        "function": {
                            "name": getattr(fn, "name", "") or "",
                            "arguments": getattr(fn, "arguments", "") or "",
                        },
                    }
                )
            if tool_calls:
                first_fn = getattr(raw_tool_calls[0], "function", None)
                if first_fn:
                    function_call = {
                        "name": getattr(first_fn, "name", "") or "",
                        "arguments": getattr(first_fn, "arguments", "") or "",
                    }

        return CompletionResponse(
            content=content_text,
            model=model,
            provider=self.config.get("name", "litellm"),
            usage=TokenUsage(
                input_tokens=prompt_tokens,
                output_tokens=completion_tokens,
                total_tokens=total_tokens,
                estimated_cost=cost,
                cache_read_tokens=cache_read,
                cache_creation_tokens=cache_creation,
                cache_creation_5m_tokens=cache_5m,
                cache_creation_1h_tokens=cache_1h,
            ),
            finish_reason=getattr(choice, "finish_reason", "stop") or "stop",
            function_call=function_call,
            tool_calls=tool_calls,
            request_id=getattr(response, "id", None),
            latency_ms=latency_ms,
        )

    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator:
        import litellm

        model_config = self.resolve_model_config(request)
        model = model_config.model_id
        alias = self._resolve_alias(model)
        kwargs = self._request_kwargs(request, model)
        kwargs["stream"] = True
        kwargs.setdefault("stream_options", {"include_usage": True})

        try:
            stream = await litellm.acompletion(**kwargs)

            tool_calls_by_index: Dict[int, Dict[str, Any]] = {}
            last_finish_reason: Optional[str] = None
            yielded_final_meta = False

            def _accumulate(index: int, tc_id: Any, name: Any, args_part: Any) -> None:
                entry = tool_calls_by_index.get(
                    index, {"id": None, "name": None, "arguments": ""}
                )
                if isinstance(tc_id, str) and tc_id:
                    entry["id"] = tc_id
                if isinstance(name, str) and name:
                    entry["name"] = name
                if isinstance(args_part, str) and args_part:
                    entry["arguments"] = (entry.get("arguments") or "") + args_part
                tool_calls_by_index[index] = entry

            async for chunk in stream:
                choices = getattr(chunk, "choices", None) or []
                if choices:
                    choice = choices[0]
                    if getattr(choice, "finish_reason", None):
                        last_finish_reason = choice.finish_reason
                    delta = getattr(choice, "delta", None)
                    if delta is not None:
                        for tc in getattr(delta, "tool_calls", None) or []:
                            idx = getattr(tc, "index", None)
                            if idx is None:
                                continue
                            fn = getattr(tc, "function", None)
                            if fn is None:
                                continue
                            _accumulate(
                                int(idx),
                                getattr(tc, "id", None),
                                getattr(fn, "name", None),
                                getattr(fn, "arguments", None),
                            )
                        fc = getattr(delta, "function_call", None)
                        if fc is not None:
                            _accumulate(
                                0,
                                None,
                                getattr(fc, "name", None),
                                getattr(fc, "arguments", None),
                            )
                        text = (
                            getattr(delta, "content", None)
                            or getattr(delta, "reasoning_content", None)
                            or getattr(delta, "reasoning", None)
                        )
                        if isinstance(text, str) and text:
                            yield text

                usage_obj = getattr(chunk, "usage", None)
                if usage_obj is not None:
                    cache_read, cache_creation, cache_5m, cache_1h = _cache_tokens(
                        usage_obj
                    )
                    prompt_tokens = int(getattr(usage_obj, "prompt_tokens", 0) or 0)
                    completion_tokens = int(
                        getattr(usage_obj, "completion_tokens", 0) or 0
                    )
                    total_tokens = int(
                        getattr(
                            usage_obj, "total_tokens", prompt_tokens + completion_tokens
                        )
                        or (prompt_tokens + completion_tokens)
                    )

                    cost = self.estimate_cost(
                        prompt_tokens,
                        completion_tokens,
                        alias,
                        cache_read_tokens=cache_read,
                        cache_creation_tokens=cache_creation,
                        cache_creation_1h_tokens=cache_1h,
                    )
                    _record_cache_metrics(
                        self.config.get("name", "litellm"),
                        model,
                        request.cache_source,
                        cache_read,
                        cache_creation,
                        cache_1h,
                    )

                    meta: Dict[str, Any] = {
                        "usage": {
                            "total_tokens": total_tokens,
                            "input_tokens": prompt_tokens,
                            "output_tokens": completion_tokens,
                            "cache_read_tokens": cache_read,
                            "cache_creation_tokens": cache_creation,
                            "cache_creation_5m_tokens": cache_5m,
                            "cache_creation_1h_tokens": cache_1h,
                            "cost_usd": cost,
                        },
                        "model": model,
                        "provider": self.config.get("name", "litellm"),
                        "finish_reason": last_finish_reason or "stop",
                    }
                    if tool_calls_by_index:
                        ordered = [
                            tool_calls_by_index[i]
                            for i in sorted(tool_calls_by_index)
                            if tool_calls_by_index[i].get("name")
                        ]
                        if ordered:
                            meta["function_call"] = {
                                "name": ordered[0].get("name"),
                                "arguments": ordered[0].get("arguments"),
                            }
                            meta["function_calls"] = ordered
                    yielded_final_meta = True
                    yield meta

            if not yielded_final_meta and tool_calls_by_index:
                ordered = [
                    tool_calls_by_index[i]
                    for i in sorted(tool_calls_by_index)
                    if tool_calls_by_index[i].get("name")
                ]
                if ordered:
                    yield {
                        "model": model,
                        "provider": self.config.get("name", "litellm"),
                        "finish_reason": last_finish_reason or "tool_calls",
                        "function_call": {
                            "name": ordered[0].get("name"),
                            "arguments": ordered[0].get("arguments"),
                        },
                        "function_calls": ordered,
                    }
        except Exception as e:
            raise Exception(f"LiteLLM streaming error (model={model}): {e}")

    def count_tokens(self, messages: List[Dict[str, Any]], model: str) -> int:
        try:
            import litellm

            return int(litellm.token_counter(model=model, messages=messages))
        except Exception:
            return TokenCounter.count_messages_tokens(messages, model)
