"""
LiteLLM AI Gateway Provider

Routes every completion through ``litellm.acompletion`` so a single Shannon
provider entry can talk to OpenAI, Anthropic, Vertex AI, Bedrock, Azure,
Cohere, Mistral, Groq, Ollama and 90+ other backends without configuring each
SDK individually. The model is selected via the standard LiteLLM
provider-prefixed name (``anthropic/...``, ``vertex_ai/...``,
``bedrock/...``, ``azure/...``, ``groq/...``) and credentials are resolved
either from provider-specific environment variables (``ANTHROPIC_API_KEY``,
``OPENAI_API_KEY``, ``AWS_ACCESS_KEY_ID``, ...) or from the optional
``api_key`` / ``base_url`` set on this provider.

This is *embedded* — Shannon imports the litellm Python SDK directly. It
does **not** require running a separate LiteLLM proxy server.

``drop_params=True`` is enabled by default so kwargs that some providers
reject (``frequency_penalty`` / ``presence_penalty`` on Anthropic, Gemini,
Bedrock; ``response_format`` on Bedrock; etc.) are silently dropped instead
of raising ``UnsupportedParamsError``. Override via
``litellm_kwargs={"drop_params": False}`` in the provider config.
"""

import logging
import time
from typing import Any, AsyncIterator, Dict, List, Optional

from .base import (
    CompletionRequest,
    CompletionResponse,
    LLMProvider,
    TokenCounter,
    TokenUsage,
    prepare_openai_messages,
)

logger = logging.getLogger(__name__)


class LiteLLMProvider(LLMProvider):
    """LiteLLM-backed provider — one Shannon provider, 100+ upstream backends."""

    def __init__(self, config: Dict[str, Any]):
        try:
            import litellm  # noqa: F401
        except ImportError as exc:
            raise ImportError(
                "litellm is required for LiteLLMProvider. "
                'Install it via `pip install "litellm>=1.60,<1.85"`.'
            ) from exc

        # Optional shared credentials/endpoint. When unset, LiteLLM resolves
        # them per request from provider-specific env vars.
        self.api_key: Optional[str] = config.get("api_key")
        self.base_url: Optional[str] = config.get("base_url") or config.get("api_base")

        # Default litellm kwargs forwarded on every call. drop_params=True is
        # the central compatibility lever — see module docstring.
        merged_kwargs: Dict[str, Any] = {"drop_params": True}
        user_kwargs = config.get("litellm_kwargs") or {}
        if isinstance(user_kwargs, dict):
            merged_kwargs.update(user_kwargs)
        self.default_kwargs: Dict[str, Any] = merged_kwargs

        super().__init__(config)

    def _initialize_models(self) -> None:
        """Populate ``self.models`` from the provider's ``models`` config dict."""
        # allow_empty=False: a litellm provider entry with no models is a
        # config bug — without at least one model the tier router can't pick
        # anything. Surface that loudly during init.
        self._load_models_from_config(allow_empty=False)

    def _request_kwargs(self, request: CompletionRequest, model: str) -> Dict[str, Any]:
        """Build the kwargs dict passed to ``litellm.acompletion``."""

        # Translate cross-provider content blocks (image, attachments, tool
        # results) into OpenAI-format. LiteLLM understands OpenAI-format input
        # and converts to each upstream's native shape.
        messages = prepare_openai_messages(request.messages)

        kwargs: Dict[str, Any] = dict(self.default_kwargs)
        kwargs["model"] = model
        kwargs["messages"] = messages

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

        # OpenAI-format tools. LiteLLM passes them through to providers that
        # support function calling; drop_params handles those that don't.
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

        # Provider-level credentials override per-request env-var resolution
        # so users with a single shared key (e.g. a private LiteLLM-proxy or
        # OpenAI-compatible endpoint) can configure once.
        if self.api_key:
            kwargs["api_key"] = self.api_key
        if self.base_url:
            kwargs["api_base"] = self.base_url

        return kwargs

    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        import litellm

        model_config = self.resolve_model_config(request)
        model = model_config.model_id
        kwargs = self._request_kwargs(request, model)

        start_time = time.time()
        try:
            response = await litellm.acompletion(**kwargs)
        except Exception as e:
            raise Exception(f"LiteLLM completion error (model={model}): {e}")
        latency_ms = int((time.time() - start_time) * 1000)

        choice = response.choices[0]
        message = choice.message
        content_text = getattr(message, "content", None) or ""

        # Token usage — fall back to local estimation if the upstream didn't
        # return a usage block (rare, but possible with some self-hosted
        # endpoints proxied through LiteLLM).
        prompt_tokens = 0
        completion_tokens = 0
        cache_read_tokens = 0
        usage_obj = getattr(response, "usage", None)
        if usage_obj is not None:
            try:
                prompt_tokens = int(getattr(usage_obj, "prompt_tokens", 0) or 0)
                completion_tokens = int(getattr(usage_obj, "completion_tokens", 0) or 0)
                ptd = getattr(usage_obj, "prompt_tokens_details", None)
                if ptd is not None:
                    cache_read_tokens = int(getattr(ptd, "cached_tokens", 0) or 0)
            except Exception:
                prompt_tokens = 0
                completion_tokens = 0
        if prompt_tokens == 0 and completion_tokens == 0:
            prompt_tokens = self.count_tokens(request.messages, model)
            completion_tokens = self.count_tokens(
                [{"role": "assistant", "content": content_text}], model
            )
        total_tokens = prompt_tokens + completion_tokens

        cost = self.estimate_cost(
            prompt_tokens,
            completion_tokens,
            model,
            cache_read_tokens=cache_read_tokens,
        )

        # Normalize tool / function call output back into Shannon's shape.
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
                first = raw_tool_calls[0]
                fn = getattr(first, "function", None)
                if fn:
                    function_call = {
                        "name": getattr(fn, "name", "") or "",
                        "arguments": getattr(fn, "arguments", "") or "",
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
                cache_read_tokens=cache_read_tokens,
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
        kwargs = self._request_kwargs(request, model)
        kwargs["stream"] = True
        # Ask OpenAI/Azure for a final usage chunk; LiteLLM ignores it for
        # providers that don't support stream_options, no harm done.
        kwargs.setdefault("stream_options", {"include_usage": True})

        try:
            stream = await litellm.acompletion(**kwargs)
            async for chunk in stream:
                # Yield text deltas first
                choices = getattr(chunk, "choices", None) or []
                if choices:
                    delta = getattr(choices[0], "delta", None)
                    if delta is not None:
                        text = getattr(delta, "content", None)
                        if text:
                            yield text

                # Final usage (either inline on last content chunk for
                # Anthropic, or in a trailing choices=[] chunk for OpenAI)
                usage = getattr(chunk, "usage", None)
                if usage is not None:
                    yield {
                        "usage": {
                            "input_tokens": int(
                                getattr(usage, "prompt_tokens", 0) or 0
                            ),
                            "output_tokens": int(
                                getattr(usage, "completion_tokens", 0) or 0
                            ),
                            "total_tokens": int(getattr(usage, "total_tokens", 0) or 0),
                        },
                        "model": getattr(chunk, "model", model),
                        "provider": self.config.get("name", "litellm"),
                    }
        except Exception as e:
            raise Exception(f"LiteLLM streaming error (model={model}): {e}")

    def count_tokens(self, messages: List[Dict[str, Any]], model: str) -> int:
        """Estimate token count.

        LiteLLM exposes ``litellm.token_counter`` which knows tokenizers for
        every supported model. Fall back to Shannon's heuristic if the call
        fails (e.g. an exotic custom model with no tokenizer mapping).
        """
        try:
            import litellm

            return int(litellm.token_counter(model=model, messages=messages))
        except Exception:
            return TokenCounter.count_messages_tokens(messages, model)
