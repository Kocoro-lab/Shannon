"""
Unit and integration tests for the MiniMax LLM provider.

Unit tests run without external dependencies by mocking the OpenAI client.
Integration tests are skipped unless MINIMAX_API_KEY is set in the environment.
"""

from __future__ import annotations  # PEP 604 `X | None` syntax on Python 3.9

import asyncio
import os
import sys
import types
import unittest
from unittest.mock import AsyncMock, MagicMock, patch

from llm_provider.minimax_provider import (  # noqa: E402
    MiniMaxProvider,
    _clamp_temperature,
    _strip_think_tags,
)
from llm_provider.base import (  # noqa: E402
    CompletionRequest,
    ModelTier,
)


# ===========================================================================
# Helpers
# ===========================================================================

def _make_provider(extra_config: dict | None = None) -> MiniMaxProvider:
    """Create a MiniMaxProvider with a dummy API key (no real network calls)."""
    config = {
        "api_key": "test-key",
        "base_url": "https://api.minimax.io/v1",
        "name": "minimax",
    }
    if extra_config:
        config.update(extra_config)
    return MiniMaxProvider(config)


def _make_fake_response(content: str, model: str = "MiniMax-M2.7") -> MagicMock:
    """Build a mock OpenAI-style chat completion response."""
    usage = MagicMock()
    usage.prompt_tokens = 10
    usage.completion_tokens = 20
    usage.total_tokens = 30

    message = MagicMock()
    message.content = content
    message.function_call = None

    choice = MagicMock()
    choice.message = message
    choice.finish_reason = "stop"

    response = MagicMock()
    response.choices = [choice]
    response.usage = usage
    response.id = "chatcmpl-test"
    response.model = model

    return response


# ===========================================================================
# Unit tests — temperature clamping
# ===========================================================================

class TestClampTemperature(unittest.TestCase):
    def test_zero_clamped_to_min(self):
        self.assertAlmostEqual(_clamp_temperature(0.0), 0.01)

    def test_negative_clamped_to_min(self):
        self.assertAlmostEqual(_clamp_temperature(-1.0), 0.01)

    def test_above_one_clamped_to_one(self):
        self.assertAlmostEqual(_clamp_temperature(1.5), 1.0)

    def test_valid_value_unchanged(self):
        self.assertAlmostEqual(_clamp_temperature(0.7), 0.7)

    def test_none_returns_default(self):
        self.assertAlmostEqual(_clamp_temperature(None), 0.7)

    def test_exactly_one_accepted(self):
        self.assertAlmostEqual(_clamp_temperature(1.0), 1.0)


# ===========================================================================
# Unit tests — think-tag stripping
# ===========================================================================

class TestStripThinkTags(unittest.TestCase):
    def test_strips_single_block(self):
        text = "<think>internal reasoning</think>Final answer."
        self.assertEqual(_strip_think_tags(text), "Final answer.")

    def test_strips_multiline_block(self):
        text = "<think>\nline1\nline2\n</think>Result"
        self.assertEqual(_strip_think_tags(text), "Result")

    def test_strips_multiple_blocks(self):
        text = "<think>a</think>mid<think>b</think>end"
        self.assertEqual(_strip_think_tags(text), "midend")

    def test_no_tags_unchanged(self):
        text = "Hello, world!"
        self.assertEqual(_strip_think_tags(text), "Hello, world!")

    def test_empty_string(self):
        self.assertEqual(_strip_think_tags(""), "")


# ===========================================================================
# Unit tests — model initialisation
# ===========================================================================

class TestMiniMaxProviderModels(unittest.TestCase):
    def test_default_models_registered(self):
        provider = _make_provider()
        self.assertIn("MiniMax-M3", provider.models)
        self.assertIn("MiniMax-M2.7", provider.models)
        self.assertIn("MiniMax-M2.7-highspeed", provider.models)

    def test_model_tiers(self):
        provider = _make_provider()
        self.assertEqual(provider.models["MiniMax-M3"].tier, ModelTier.MEDIUM)
        self.assertEqual(provider.models["MiniMax-M2.7"].tier, ModelTier.MEDIUM)
        self.assertEqual(provider.models["MiniMax-M2.7-highspeed"].tier, ModelTier.SMALL)

    def test_context_window(self):
        provider = _make_provider()
        self.assertEqual(provider.models["MiniMax-M3"].context_window, 524288)
        for alias in ("MiniMax-M2.7", "MiniMax-M2.7-highspeed"):
            self.assertEqual(provider.models[alias].context_window, 204800)

    def test_missing_api_key_raises(self):
        with patch.dict(os.environ, {}, clear=False):
            env_backup = os.environ.pop("MINIMAX_API_KEY", None)
            try:
                with self.assertRaises(ValueError):
                    MiniMaxProvider({"base_url": "https://api.minimax.io/v1"})
            finally:
                if env_backup is not None:
                    os.environ["MINIMAX_API_KEY"] = env_backup

    def test_models_from_config(self):
        """Custom model catalog overrides defaults."""
        config = {
            "api_key": "test-key",
            "models": {
                "custom-model": {
                    "model_id": "custom-model",
                    "tier": "small",
                    "context_window": 8192,
                    "max_tokens": 2048,
                }
            },
        }
        provider = MiniMaxProvider(config)
        self.assertIn("custom-model", provider.models)
        self.assertNotIn("MiniMax-M3", provider.models)
        self.assertNotIn("MiniMax-M2.7", provider.models)


# ===========================================================================
# Unit tests — complete() with mocked client
# ===========================================================================

class TestMiniMaxProviderComplete(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.provider = _make_provider()

    async def test_basic_completion(self):
        fake_resp = _make_fake_response("Hello from MiniMax!")
        self.provider.client = MagicMock()
        self.provider.client.chat = MagicMock()
        self.provider.client.chat.completions = MagicMock()
        self.provider.client.chat.completions.create = AsyncMock(return_value=fake_resp)

        request = CompletionRequest(
            messages=[{"role": "user", "content": "Say hello"}],
            model="MiniMax-M2.7",
        )
        response = await self.provider.complete(request)

        self.assertEqual(response.content, "Hello from MiniMax!")
        self.assertEqual(response.provider, "minimax")
        self.assertEqual(response.model, "MiniMax-M2.7")

    async def test_think_tags_stripped(self):
        fake_resp = _make_fake_response("<think>secret</think>Public answer")
        self.provider.client = MagicMock()
        self.provider.client.chat.completions.create = AsyncMock(return_value=fake_resp)

        request = CompletionRequest(
            messages=[{"role": "user", "content": "Question"}],
            model="MiniMax-M2.7",
        )
        response = await self.provider.complete(request)
        self.assertEqual(response.content, "Public answer")

    async def test_temperature_clamped_in_payload(self):
        fake_resp = _make_fake_response("ok")
        create_mock = AsyncMock(return_value=fake_resp)
        self.provider.client = MagicMock()
        self.provider.client.chat.completions.create = create_mock

        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model="MiniMax-M2.7",
            temperature=0.0,  # should be clamped
        )
        await self.provider.complete(request)

        call_kwargs = create_mock.call_args.kwargs
        self.assertGreater(call_kwargs["temperature"], 0.0)
        self.assertLessEqual(call_kwargs["temperature"], 1.0)

    async def test_token_usage_recorded(self):
        fake_resp = _make_fake_response("answer")
        self.provider.client = MagicMock()
        self.provider.client.chat.completions.create = AsyncMock(return_value=fake_resp)

        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model="MiniMax-M2.7",
        )
        response = await self.provider.complete(request)
        self.assertEqual(response.usage.input_tokens, 10)
        self.assertEqual(response.usage.output_tokens, 20)
        self.assertEqual(response.usage.total_tokens, 30)

    async def test_api_error_raised(self):
        self.provider.client = MagicMock()
        self.provider.client.chat.completions.create = AsyncMock(
            side_effect=Exception("connection refused")
        )

        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model="MiniMax-M2.7",
        )
        with self.assertRaises(Exception):
            await self.provider.complete(request)

    async def test_highspeed_model_selectable(self):
        fake_resp = _make_fake_response("fast reply", model="MiniMax-M2.7-highspeed")
        self.provider.client = MagicMock()
        self.provider.client.chat.completions.create = AsyncMock(return_value=fake_resp)

        request = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            model="MiniMax-M2.7-highspeed",
        )
        response = await self.provider.complete(request)
        self.assertEqual(response.model, "MiniMax-M2.7-highspeed")

    async def test_count_tokens(self):
        provider = _make_provider()
        messages = [{"role": "user", "content": "Hello"}]
        count = provider.count_tokens(messages, "MiniMax-M2.7")
        self.assertIsInstance(count, int)
        self.assertGreater(count, 0)


# ===========================================================================
# Unit tests — manager integration (no real providers)
# ===========================================================================

class TestManagerMiniMaxRegistration(unittest.TestCase):
    def test_minimax_type_recognized_in_manager(self):
        """Manager should instantiate MiniMaxProvider for type='minimax'."""
        import sys
        import types as _types

        # Stub out heavy deps before importing manager
        anth = _types.ModuleType("anthropic")
        anth.AsyncAnthropic = MagicMock
        sys.modules.setdefault("anthropic", anth)
        sys.modules.setdefault("tiktoken", _types.ModuleType("tiktoken"))

        # Force fresh import so stubs take effect
        for mod in list(sys.modules.keys()):
            if "llm_provider.manager" in mod:
                del sys.modules[mod]

        from llm_provider.manager import LLMManager

        mgr = LLMManager()

        config = {
            "type": "minimax",
            "api_key": "test-key",
            "base_url": "https://api.minimax.io/v1",
            "name": "minimax",
            "models": {
                "MiniMax-M2.7": {
                    "model_id": "MiniMax-M2.7",
                    "tier": "medium",
                    "context_window": 204800,
                    "max_tokens": 4096,
                }
            },
        }
        mgr._initialize_providers({"minimax": config})
        self.assertIn("minimax", mgr.registry.providers)
        provider = mgr.registry.providers["minimax"]
        self.assertIsInstance(provider, MiniMaxProvider)

    def test_provider_type_enum_has_minimax(self):
        from llm_service.providers import ProviderType
        self.assertIn("minimax", [p.value for p in ProviderType])

    def test_provider_name_map_has_minimax(self):
        from llm_service.providers import _PROVIDER_NAME_MAP, ProviderType
        self.assertEqual(_PROVIDER_NAME_MAP.get("minimax"), ProviderType.MINIMAX)


# ===========================================================================
# Integration tests — require real MINIMAX_API_KEY
# ===========================================================================

INTEGRATION = os.getenv("MINIMAX_API_KEY") is not None


@unittest.skipUnless(INTEGRATION, "Set MINIMAX_API_KEY to run integration tests")
class TestMiniMaxIntegration(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        # Create provider fresh so it uses real openai client
        self.provider = MiniMaxProvider.__new__(MiniMaxProvider)
        self.provider.config = {
            "api_key": os.environ["MINIMAX_API_KEY"],
            "base_url": "https://api.minimax.io/v1",
            "name": "minimax",
        }
        from openai import AsyncOpenAI
        self.provider.client = AsyncOpenAI(
            api_key=os.environ["MINIMAX_API_KEY"],
            base_url="https://api.minimax.io/v1",
        )
        self.provider.base_url = "https://api.minimax.io/v1"
        self.provider.models = {}
        self.provider._add_default_models()

    async def test_real_completion(self):
        request = CompletionRequest(
            messages=[{"role": "user", "content": "Reply with the single word: pong"}],
            model="MiniMax-M2.7",
            temperature=0.3,
            max_tokens=500,
        )
        response = await self.provider.complete(request)
        self.assertIn("pong", response.content.lower())
        self.assertGreater(response.usage.total_tokens, 0)

    async def test_real_streaming(self):
        request = CompletionRequest(
            messages=[{"role": "user", "content": "Count to 3, one word per line"}],
            model="MiniMax-M2.7-highspeed",
            temperature=0.5,
            max_tokens=500,
        )
        chunks = []
        async for chunk in self.provider.stream_complete(request):
            if isinstance(chunk, str):
                chunks.append(chunk)
        self.assertTrue(len(chunks) > 0)
        full = "".join(chunks)
        self.assertTrue(len(full) > 0)


if __name__ == "__main__":
    unittest.main()
