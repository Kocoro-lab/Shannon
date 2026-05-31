"""
Unit tests for the LiteLLM gateway provider.

Unit tests run without external dependencies by mocking ``litellm.acompletion``.
Live integration tests live in a separate scratch script and are not part of
the suite.
"""

from __future__ import annotations

import asyncio
import unittest
from types import SimpleNamespace
from typing import Any, Dict, List
from unittest.mock import patch

from llm_provider.base import (
    CompletionRequest,
    ModelTier,
)
from llm_provider.litellm_provider import LiteLLMProvider


# ===========================================================================
# Helpers
# ===========================================================================


def _make_provider(extra_config: Dict[str, Any] | None = None) -> LiteLLMProvider:
    """Build a LiteLLMProvider with a minimal valid config."""
    config: Dict[str, Any] = {
        "name": "litellm",
        "models": {
            "anthropic/claude-3-5-sonnet-20241022": {
                "tier": "large",
                "model_id": "anthropic/claude-3-5-sonnet-20241022",
                "context_window": 200000,
                "max_tokens": 8192,
                "input_price_per_1k": 0.003,
                "output_price_per_1k": 0.015,
            },
            "openai/gpt-4o-mini": {
                "tier": "small",
                "model_id": "openai/gpt-4o-mini",
                "context_window": 128000,
                "max_tokens": 16384,
                "input_price_per_1k": 0.00015,
                "output_price_per_1k": 0.0006,
            },
        },
    }
    if extra_config:
        config.update(extra_config)
    return LiteLLMProvider(config)


def _completion_response(
    content: str,
    prompt_tokens: int = 10,
    completion_tokens: int = 5,
    tool_calls: List[Dict[str, str]] | None = None,
) -> SimpleNamespace:
    """Build a mock OpenAI-shaped response from ``litellm.acompletion``."""

    usage = SimpleNamespace(
        prompt_tokens=prompt_tokens,
        completion_tokens=completion_tokens,
        total_tokens=prompt_tokens + completion_tokens,
        prompt_tokens_details=SimpleNamespace(cached_tokens=0),
    )

    raw_tool_calls = None
    if tool_calls:
        raw_tool_calls = [
            SimpleNamespace(
                id=tc["id"],
                function=SimpleNamespace(
                    name=tc["function"]["name"],
                    arguments=tc["function"]["arguments"],
                ),
            )
            for tc in tool_calls
        ]

    message = SimpleNamespace(
        content=content,
        tool_calls=raw_tool_calls,
        function_call=None,
    )
    choice = SimpleNamespace(message=message, finish_reason="stop")
    return SimpleNamespace(
        choices=[choice],
        usage=usage,
        id="chatcmpl-test",
        model="anthropic/claude-3-5-sonnet-20241022",
    )


async def _async_iter(items: List[Any]):
    for item in items:
        yield item


def _stream_chunk(
    *,
    content: str | None = None,
    reasoning: str | None = None,
    tool_calls: List[Any] | None = None,
    function_call: Any = None,
    finish_reason: str | None = None,
    usage: Dict[str, Any] | None = None,
) -> SimpleNamespace:
    """Build one streaming chunk. ``usage`` may include cache fields nested as needed."""
    delta = SimpleNamespace(
        content=content,
        reasoning_content=reasoning,
        reasoning=None,
        tool_calls=tool_calls,
        function_call=function_call,
    )
    choice = SimpleNamespace(delta=delta, finish_reason=finish_reason)
    choices = (
        [choice]
        if (
            content is not None
            or reasoning is not None
            or tool_calls
            or function_call
            or finish_reason
        )
        else []
    )
    chunk_usage = None
    if usage is not None:
        chunk_usage = SimpleNamespace(
            prompt_tokens=usage.get("prompt_tokens", 0),
            completion_tokens=usage.get("completion_tokens", 0),
            total_tokens=usage.get(
                "total_tokens",
                usage.get("prompt_tokens", 0) + usage.get("completion_tokens", 0),
            ),
            prompt_tokens_details=SimpleNamespace(
                cached_tokens=usage.get("cached_tokens", 0)
            )
            if "cached_tokens" in usage
            else None,
            cache_read_input_tokens=usage.get("cache_read_input_tokens", 0),
            cache_creation_input_tokens=usage.get("cache_creation_input_tokens", 0),
            cache_creation=SimpleNamespace(
                ephemeral_5m_input_tokens=usage.get("cache_creation_5m", 0),
                ephemeral_1h_input_tokens=usage.get("cache_creation_1h", 0),
            )
            if "cache_creation_5m" in usage or "cache_creation_1h" in usage
            else None,
        )
    return SimpleNamespace(choices=choices, usage=chunk_usage, model="m")


def _content_chunk(text: str | None, usage: Dict[str, int] | None = None):
    return _stream_chunk(content=text, usage=usage)


def _usage_only_chunk(usage: Dict[str, int]):
    return _stream_chunk(usage=usage)


def _tc_delta(
    index: int,
    *,
    tc_id: str | None = None,
    name: str | None = None,
    arguments: str | None = None,
):
    return SimpleNamespace(
        index=index,
        id=tc_id,
        function=SimpleNamespace(name=name, arguments=arguments),
    )


# ===========================================================================
# Configuration / initialization
# ===========================================================================


class TestLiteLLMInitialization(unittest.TestCase):
    def test_initializes_models_from_config(self):
        provider = _make_provider()
        self.assertIn("anthropic/claude-3-5-sonnet-20241022", provider.models)
        self.assertIn("openai/gpt-4o-mini", provider.models)

    def test_drop_params_default_on(self):
        provider = _make_provider()
        self.assertIs(provider.default_kwargs.get("drop_params"), True)

    def test_drop_params_can_be_disabled(self):
        provider = _make_provider({"litellm_kwargs": {"drop_params": False}})
        self.assertIs(provider.default_kwargs.get("drop_params"), False)

    def test_user_litellm_kwargs_merged(self):
        provider = _make_provider(
            {"litellm_kwargs": {"num_retries": 3, "timeout": 120}}
        )
        self.assertEqual(provider.default_kwargs.get("num_retries"), 3)
        self.assertEqual(provider.default_kwargs.get("timeout"), 120)
        # default still preserved
        self.assertIs(provider.default_kwargs.get("drop_params"), True)

    def test_missing_models_raises(self):
        with self.assertRaises(ValueError):
            LiteLLMProvider({"name": "litellm"})

    def test_resolves_model_alias_with_provider_prefix(self):
        provider = _make_provider()
        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model="anthropic/claude-3-5-sonnet-20241022",
            model_tier=ModelTier.LARGE,
        )
        config = provider.resolve_model_config(request)
        self.assertEqual(config.model_id, "anthropic/claude-3-5-sonnet-20241022")

    def test_unknown_model_raises(self):
        provider = _make_provider()
        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model="nonexistent/model",
        )
        with self.assertRaises(ValueError):
            provider.resolve_model_config(request)


# ===========================================================================
# Request construction
# ===========================================================================


class TestRequestKwargs(unittest.TestCase):
    def test_temperature_and_max_tokens_forwarded(self):
        provider = _make_provider()
        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            temperature=0.3,
            max_tokens=512,
            model_tier=ModelTier.SMALL,
        )
        kwargs = provider._request_kwargs(request, "openai/gpt-4o-mini")
        self.assertEqual(kwargs["temperature"], 0.3)
        self.assertEqual(kwargs["max_tokens"], 512)
        self.assertIs(kwargs["drop_params"], True)
        self.assertEqual(kwargs["model"], "openai/gpt-4o-mini")

    def test_zero_temperature_forwarded(self):
        # Edge case — must not fall through `if request.temperature:` truthy gate.
        provider = _make_provider()
        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            temperature=0.0,
            model_tier=ModelTier.SMALL,
        )
        kwargs = provider._request_kwargs(request, "openai/gpt-4o-mini")
        self.assertEqual(kwargs["temperature"], 0.0)

    def test_tools_forwarded_in_openai_format(self):
        provider = _make_provider()
        request = CompletionRequest(
            messages=[{"role": "user", "content": "weather?"}],
            functions=[
                {
                    "name": "get_weather",
                    "description": "Get the weather",
                    "parameters": {"type": "object", "properties": {}},
                }
            ],
            function_call="auto",
            model_tier=ModelTier.SMALL,
        )
        kwargs = provider._request_kwargs(request, "openai/gpt-4o-mini")
        self.assertIn("tools", kwargs)
        self.assertEqual(kwargs["tools"][0]["type"], "function")
        self.assertEqual(kwargs["tools"][0]["function"]["name"], "get_weather")
        self.assertEqual(kwargs["tool_choice"], "auto")

    def test_provider_credentials_override_env(self):
        provider = _make_provider(
            {"api_key": "sk-test", "base_url": "https://example.invalid/v1"}
        )
        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model_tier=ModelTier.SMALL,
        )
        kwargs = provider._request_kwargs(request, "openai/gpt-4o-mini")
        self.assertEqual(kwargs["api_key"], "sk-test")
        self.assertEqual(kwargs["api_base"], "https://example.invalid/v1")

    def test_no_credentials_omitted_from_kwargs(self):
        provider = _make_provider()
        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model_tier=ModelTier.SMALL,
        )
        kwargs = provider._request_kwargs(request, "openai/gpt-4o-mini")
        self.assertNotIn("api_key", kwargs)
        self.assertNotIn("api_base", kwargs)


# ===========================================================================
# complete() — non-streaming
# ===========================================================================


class TestComplete(unittest.TestCase):
    def test_complete_returns_text_and_usage(self):
        provider = _make_provider()
        request = CompletionRequest(
            messages=[{"role": "user", "content": "What is 2+2?"}],
            model="openai/gpt-4o-mini",
            model_tier=ModelTier.SMALL,
        )

        async def run():
            with patch(
                "litellm.acompletion",
                return_value=_completion_response(
                    "4", prompt_tokens=10, completion_tokens=2
                ),
            ):
                return await provider.complete(request)

        response = asyncio.run(run())
        self.assertEqual(response.content, "4")
        self.assertEqual(response.usage.input_tokens, 10)
        self.assertEqual(response.usage.output_tokens, 2)
        self.assertEqual(response.usage.total_tokens, 12)
        self.assertEqual(response.finish_reason, "stop")

    def test_complete_propagates_tool_calls(self):
        provider = _make_provider()
        request = CompletionRequest(
            messages=[{"role": "user", "content": "weather?"}],
            model="openai/gpt-4o-mini",
            model_tier=ModelTier.SMALL,
        )
        tool_calls = [
            {
                "id": "call_123",
                "function": {
                    "name": "get_weather",
                    "arguments": '{"city":"Tokyo"}',
                },
            }
        ]

        async def run():
            with patch(
                "litellm.acompletion",
                return_value=_completion_response("", tool_calls=tool_calls),
            ):
                return await provider.complete(request)

        response = asyncio.run(run())
        self.assertIsNotNone(response.tool_calls)
        self.assertEqual(response.tool_calls[0]["id"], "call_123")
        self.assertEqual(response.tool_calls[0]["function"]["name"], "get_weather")
        self.assertEqual(
            response.function_call,
            {"name": "get_weather", "arguments": '{"city":"Tokyo"}'},
        )

    def test_complete_raises_on_litellm_failure(self):
        provider = _make_provider()
        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model="openai/gpt-4o-mini",
            model_tier=ModelTier.SMALL,
        )

        async def run():
            with patch("litellm.acompletion", side_effect=RuntimeError("dead network")):
                return await provider.complete(request)

        with self.assertRaisesRegex(Exception, "LiteLLM completion error"):
            asyncio.run(run())


# ===========================================================================
# stream_complete() — provider-shape coverage
# ===========================================================================


class TestStreamComplete(unittest.TestCase):
    def _collect(self, provider, chunks, request):
        async def run():
            with patch("litellm.acompletion", return_value=_async_iter(chunks)):
                items = []
                async for item in provider.stream_complete(request):
                    items.append(item)
                return items

        return asyncio.run(run())

    def test_openai_style_separate_usage_chunk(self):
        """Azure / OpenAI with stream_options=include_usage emits a trailing
        usage-only chunk. We yield content text, then a single usage dict."""
        provider = _make_provider()
        request = CompletionRequest(
            messages=[{"role": "user", "content": "count"}],
            model="openai/gpt-4o-mini",
            model_tier=ModelTier.SMALL,
        )
        items = self._collect(
            provider,
            [
                _content_chunk("1"),
                _content_chunk(", 2"),
                _content_chunk(", 3"),
                _content_chunk(None),  # finish-only chunk, no content
                _usage_only_chunk(
                    {"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8}
                ),
            ],
            request,
        )
        # 3 text deltas + 1 usage dict
        text_items = [x for x in items if isinstance(x, str)]
        usage_items = [x for x in items if isinstance(x, dict)]
        self.assertEqual(text_items, ["1", ", 2", ", 3"])
        self.assertEqual(len(usage_items), 1)
        self.assertEqual(usage_items[0]["usage"]["total_tokens"], 8)

    def test_anthropic_style_inline_usage(self):
        """Anthropic via LiteLLM attaches usage on the last content chunk."""
        provider = _make_provider()
        request = CompletionRequest(
            messages=[{"role": "user", "content": "count"}],
            model="anthropic/claude-3-5-sonnet-20241022",
            model_tier=ModelTier.LARGE,
        )
        items = self._collect(
            provider,
            [
                _content_chunk("1"),
                _content_chunk(", 2"),
                _content_chunk(
                    ", 3",
                    usage={
                        "prompt_tokens": 5,
                        "completion_tokens": 3,
                        "total_tokens": 8,
                    },
                ),
            ],
            request,
        )
        text_items = [x for x in items if isinstance(x, str)]
        usage_items = [x for x in items if isinstance(x, dict)]
        self.assertEqual(text_items, ["1", ", 2", ", 3"])
        # Exactly one usage dict — no duplicate
        self.assertEqual(len(usage_items), 1)
        self.assertEqual(usage_items[0]["usage"]["input_tokens"], 5)
        self.assertEqual(usage_items[0]["usage"]["output_tokens"], 3)

    def test_finish_only_chunk_does_not_emit_text(self):
        provider = _make_provider()
        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model="openai/gpt-4o-mini",
            model_tier=ModelTier.SMALL,
        )
        items = self._collect(
            provider,
            [
                _content_chunk("hi"),
                _content_chunk(None),  # finish chunk with no content
            ],
            request,
        )
        text_items = [x for x in items if isinstance(x, str)]
        self.assertEqual(text_items, ["hi"])

    def test_stream_raises_on_litellm_failure(self):
        provider = _make_provider()
        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model="openai/gpt-4o-mini",
            model_tier=ModelTier.SMALL,
        )

        async def run():
            with patch("litellm.acompletion", side_effect=RuntimeError("network fail")):
                async for _ in provider.stream_complete(request):
                    pass

        with self.assertRaisesRegex(Exception, "LiteLLM streaming error"):
            asyncio.run(run())


# ===========================================================================
# count_tokens
# ===========================================================================


class TestCountTokens(unittest.TestCase):
    def test_count_tokens_falls_back_when_litellm_fails(self):
        provider = _make_provider()
        with patch("litellm.token_counter", side_effect=Exception("no tokenizer")):
            count = provider.count_tokens(
                [{"role": "user", "content": "hello world"}], "unknown/model"
            )
        self.assertGreater(count, 0)

    def test_count_tokens_uses_litellm_when_available(self):
        provider = _make_provider()
        with patch("litellm.token_counter", return_value=42):
            count = provider.count_tokens(
                [{"role": "user", "content": "hi"}],
                "openai/gpt-4o-mini",
            )
        self.assertEqual(count, 42)


# ===========================================================================
# Review-feedback fixes (Shannon PR #172)
# ===========================================================================
# The first round of this provider missed six correctness behaviors that
# openai_provider / anthropic_provider already had. The tests below pin the
# fixes so we don't regress.


class TestAliasResolution(unittest.TestCase):
    """Fix #6: cost lookup must use the configured alias, not the model_id."""

    def test_resolve_alias_returns_alias_when_model_id_matches(self):
        provider = LiteLLMProvider(
            {
                "name": "litellm",
                "models": {
                    "sonnet": {
                        "tier": "large",
                        "model_id": "anthropic/claude-3-5-sonnet-20241022",
                        "context_window": 200000,
                        "max_tokens": 8192,
                        "input_price_per_1k": 0.003,
                        "output_price_per_1k": 0.015,
                    },
                },
            }
        )
        alias = provider._resolve_alias("anthropic/claude-3-5-sonnet-20241022")
        self.assertEqual(alias, "sonnet")

    def test_resolve_alias_passes_through_unknown_model(self):
        provider = _make_provider()
        unknown = "some/model-not-in-config"
        self.assertEqual(provider._resolve_alias(unknown), unknown)

    def test_complete_uses_alias_for_cost(self):
        """Cost must be non-zero when only an alias maps to the runtime model_id."""
        provider = LiteLLMProvider(
            {
                "name": "litellm",
                "models": {
                    "sonnet": {
                        "tier": "large",
                        "model_id": "anthropic/claude-3-5-sonnet-20241022",
                        "context_window": 200000,
                        "max_tokens": 8192,
                        "input_price_per_1k": 0.003,
                        "output_price_per_1k": 0.015,
                    },
                },
            }
        )
        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model="anthropic/claude-3-5-sonnet-20241022",
            model_tier=ModelTier.LARGE,
        )

        async def run():
            with patch(
                "litellm.acompletion",
                return_value=_completion_response(
                    "ok", prompt_tokens=1000, completion_tokens=500
                ),
            ):
                return await provider.complete(request)

        out = asyncio.run(run())
        # 1k input @ $0.003/1k + 500 output @ $0.015/1k = $0.0105
        self.assertGreater(out.usage.estimated_cost, 0.0)


class TestTimeoutForwarding(unittest.TestCase):
    """Fix #4: ModelConfig.timeout must reach litellm.acompletion."""

    def test_default_timeout_is_60(self):
        provider = _make_provider()
        self.assertEqual(provider.timeout, 60)

    def test_custom_timeout_from_config(self):
        provider = _make_provider({"timeout": 120})
        self.assertEqual(provider.timeout, 120)

    def test_timeout_in_request_kwargs(self):
        provider = _make_provider({"timeout": 90})
        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model_tier=ModelTier.SMALL,
        )
        kwargs = provider._request_kwargs(request, "openai/gpt-4o-mini")
        self.assertEqual(kwargs["timeout"], 90)


class TestContentExtraction(unittest.TestCase):
    """Fix #5: thinking-model responses where content is empty must surface
    via reasoning_content / output / thinking fallbacks."""

    def test_falls_back_to_reasoning_content_when_content_empty(self):
        provider = _make_provider()
        message = SimpleNamespace(
            content="",
            reasoning_content="The answer after reasoning is 42.",
            tool_calls=None,
            function_call=None,
        )
        choice = SimpleNamespace(message=message, finish_reason="stop")
        usage = SimpleNamespace(prompt_tokens=10, completion_tokens=20, total_tokens=30)
        response = SimpleNamespace(choices=[choice], usage=usage, id="test")

        request = CompletionRequest(
            messages=[{"role": "user", "content": "ask"}],
            model="openai/gpt-4o-mini",
            model_tier=ModelTier.SMALL,
        )

        async def run():
            with patch("litellm.acompletion", return_value=response):
                return await provider.complete(request)

        out = asyncio.run(run())
        self.assertIn("42", out.content)

    def test_extracts_text_from_list_content_parts(self):
        provider = _make_provider()
        message = SimpleNamespace(
            content=[
                SimpleNamespace(text="part one. "),
                SimpleNamespace(text="part two."),
            ],
            reasoning_content=None,
            tool_calls=None,
            function_call=None,
        )
        choice = SimpleNamespace(message=message, finish_reason="stop")
        usage = SimpleNamespace(prompt_tokens=10, completion_tokens=5, total_tokens=15)
        response = SimpleNamespace(choices=[choice], usage=usage, id="test")

        request = CompletionRequest(
            messages=[{"role": "user", "content": "ask"}],
            model_tier=ModelTier.SMALL,
        )

        async def run():
            with patch("litellm.acompletion", return_value=response):
                return await provider.complete(request)

        out = asyncio.run(run())
        self.assertIn("part one", out.content)
        self.assertIn("part two", out.content)


class TestCacheTokenParsing(unittest.TestCase):
    """Fix #3: parse Anthropic cache_creation per-TTL fields plus OpenAI
    prompt_tokens_details.cached_tokens fallback."""

    def test_anthropic_cache_creation_5m_and_1h(self):
        from llm_provider.litellm_provider import _cache_tokens

        usage = SimpleNamespace(
            prompt_tokens=1000,
            completion_tokens=500,
            cache_read_input_tokens=200,
            cache_creation_input_tokens=300,
            cache_creation=SimpleNamespace(
                ephemeral_5m_input_tokens=180,
                ephemeral_1h_input_tokens=120,
            ),
        )
        cache_read, cache_creation, cache_5m, cache_1h = _cache_tokens(usage)
        self.assertEqual(cache_read, 200)
        self.assertEqual(cache_creation, 300)
        self.assertEqual(cache_5m, 180)
        self.assertEqual(cache_1h, 120)

    def test_openai_prompt_tokens_details_fallback(self):
        from llm_provider.litellm_provider import _cache_tokens

        usage = SimpleNamespace(
            prompt_tokens=1000,
            completion_tokens=500,
            prompt_tokens_details=SimpleNamespace(cached_tokens=400),
        )
        cache_read, cache_creation, cache_5m, cache_1h = _cache_tokens(usage)
        self.assertEqual(cache_read, 400)
        self.assertEqual(cache_creation, 0)
        self.assertEqual(cache_5m, 0)
        self.assertEqual(cache_1h, 0)

    def test_no_usage_returns_zeros(self):
        from llm_provider.litellm_provider import _cache_tokens

        cache_read, cache_creation, cache_5m, cache_1h = _cache_tokens(None)
        self.assertEqual((cache_read, cache_creation, cache_5m, cache_1h), (0, 0, 0, 0))

    def test_complete_surfaces_anthropic_cache_in_token_usage(self):
        provider = _make_provider()
        message = SimpleNamespace(content="hi", tool_calls=None, function_call=None)
        choice = SimpleNamespace(message=message, finish_reason="stop")
        usage = SimpleNamespace(
            prompt_tokens=1000,
            completion_tokens=500,
            cache_read_input_tokens=200,
            cache_creation_input_tokens=300,
            cache_creation=SimpleNamespace(
                ephemeral_5m_input_tokens=180,
                ephemeral_1h_input_tokens=120,
            ),
        )
        response = SimpleNamespace(choices=[choice], usage=usage, id="test")

        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model="anthropic/claude-3-5-sonnet-20241022",
            model_tier=ModelTier.LARGE,
        )

        async def run():
            with patch("litellm.acompletion", return_value=response):
                return await provider.complete(request)

        out = asyncio.run(run())
        self.assertEqual(out.usage.cache_read_tokens, 200)
        self.assertEqual(out.usage.cache_creation_tokens, 300)
        self.assertEqual(out.usage.cache_creation_5m_tokens, 180)
        self.assertEqual(out.usage.cache_creation_1h_tokens, 120)


class TestStreamingToolCalls(unittest.TestCase):
    """Fix #1 + #2: streaming tool_calls accumulation and final meta payload."""

    def _run_stream(self, provider, request, chunks):
        async def go():
            with patch("litellm.acompletion", return_value=_async_iter(chunks)):
                items = []
                async for item in provider.stream_complete(request):
                    items.append(item)
                return items

        return asyncio.run(go())

    def test_streaming_accumulates_tool_call_deltas(self):
        provider = _make_provider()
        chunks = [
            _stream_chunk(
                tool_calls=[
                    _tc_delta(
                        0, tc_id="call_abc", name="get_weather", arguments='{"city":'
                    )
                ]
            ),
            _stream_chunk(tool_calls=[_tc_delta(0, arguments='"Tokyo"}')]),
            _stream_chunk(
                finish_reason="tool_calls",
                usage={"prompt_tokens": 20, "completion_tokens": 10},
            ),
        ]
        request = CompletionRequest(
            messages=[{"role": "user", "content": "weather"}],
            model="anthropic/claude-3-5-sonnet-20241022",
            model_tier=ModelTier.LARGE,
            functions=[{"name": "get_weather", "parameters": {}}],
        )
        items = self._run_stream(provider, request, chunks)
        meta = next((i for i in items if isinstance(i, dict)), None)
        self.assertIsNotNone(meta)
        self.assertEqual(meta["function_call"]["name"], "get_weather")
        # Arguments reassembled from the two fragments.
        self.assertEqual(meta["function_call"]["arguments"], '{"city":"Tokyo"}')
        self.assertEqual(meta["function_calls"][0]["id"], "call_abc")

    def test_streaming_final_meta_has_required_fields(self):
        """Fix #2: cost_usd, cache_read_tokens, finish_reason all present."""
        provider = _make_provider()
        chunks = [
            _stream_chunk(content="hello"),
            _stream_chunk(
                finish_reason="stop",
                usage={
                    "prompt_tokens": 100,
                    "completion_tokens": 50,
                    "cached_tokens": 20,
                },
            ),
        ]
        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model="openai/gpt-4o-mini",
            model_tier=ModelTier.SMALL,
        )
        items = self._run_stream(provider, request, chunks)
        meta = next((i for i in items if isinstance(i, dict)), None)
        self.assertIsNotNone(meta)
        self.assertEqual(meta["usage"]["cache_read_tokens"], 20)
        self.assertEqual(meta["finish_reason"], "stop")
        self.assertGreater(meta["usage"]["cost_usd"], 0.0)

    def test_streaming_yields_reasoning_as_text(self):
        """Fix #5 (streaming side): delta.reasoning_content surfaces as text."""
        provider = _make_provider()
        chunks = [
            _stream_chunk(reasoning="thinking step 1. "),
            _stream_chunk(
                content="The answer is 42.",
                finish_reason="stop",
                usage={"prompt_tokens": 10, "completion_tokens": 5},
            ),
        ]
        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model_tier=ModelTier.SMALL,
        )
        items = self._run_stream(provider, request, chunks)
        joined = "".join(i for i in items if isinstance(i, str))
        self.assertIn("thinking step 1", joined)
        self.assertIn("The answer is 42", joined)


if __name__ == "__main__":
    unittest.main()
