import asyncio
import sys
import types

from llm_provider.base import (
    LLMProvider,
    CompletionRequest,
    CompletionResponse,
    TokenUsage,
    ModelTier,
    ModelConfig,
)


def _import_manager_with_stubs():
    """Import LLMManager after stubbing heavy provider deps (anthropic, openai)."""
    # Provide stub modules so manager's provider imports don't require external packages
    anth = types.ModuleType("anthropic")

    class _AsyncAnthropic:  # minimal placeholder
        def __init__(self, *a, **kw):
            pass

    anth.AsyncAnthropic = _AsyncAnthropic
    sys.modules["anthropic"] = anth

    oi = types.ModuleType("openai")

    class _AsyncOpenAI:  # minimal placeholder
        def __init__(self, *a, **kw):
            pass

    oi.AsyncOpenAI = _AsyncOpenAI
    sys.modules["openai"] = oi
    # Stub tiktoken as it's imported by openai_provider
    sys.modules.setdefault("tiktoken", types.ModuleType("tiktoken"))
    # Ensure re-import uses our stubs
    if "llm_provider.manager" in sys.modules:
        del sys.modules["llm_provider.manager"]
    from llm_provider.manager import LLMManager  # noqa: E402

    return LLMManager


class DummyProvider(LLMProvider):
    def _initialize_models(self):
        self.models["dummy-small"] = ModelConfig(
            provider="dummy",
            model_id="dummy-small",
            tier=ModelTier.SMALL,
            max_tokens=2048,
            context_window=2048,
            input_price_per_1k=0.0001,
            output_price_per_1k=0.0002,
        )

    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        usage = TokenUsage(1, 2, 3, 0.000001)
        return CompletionResponse(
            content="ok",
            model="dummy-small",
            provider="dummy",
            usage=usage,
            finish_reason="stop",
        )

    async def stream_complete(self, request: CompletionRequest):
        yield "ok"

    def count_tokens(self, messages, model: str) -> int:
        return sum(len(m.get("content", "")) for m in messages)


def test_manager_routing_and_cache():
    async def _run():
        LLMManager = _import_manager_with_stubs()
        mgr = LLMManager()
        # Inject dummy provider as default
        dummy = DummyProvider({})
        mgr.registry.register_provider("dummy", dummy, is_default=True)
        mgr.tier_preferences = {"small": ["dummy:dummy-small"]}

        req_msgs = [{"role": "user", "content": "hello"}]
        # First call: no cache
        resp1 = await mgr.complete(req_msgs, ModelTier.SMALL)
        assert resp1.content == "ok"
        # Second call: should hit cache
        resp2 = await mgr.complete(req_msgs, ModelTier.SMALL)
        assert resp2.content == "ok"
        # Ensure cache tracked a hit
        assert mgr.cache is not None
        assert mgr.cache.hit_rate > 0.0

    asyncio.run(_run())


class FailingProvider(DummyProvider):
    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        raise RuntimeError("forced failure")


def test_manager_fallback_on_failure():
    async def _run():
        LLMManager = _import_manager_with_stubs()
        mgr = LLMManager()
        failing = FailingProvider({})
        backup = DummyProvider({})
        mgr.registry.register_provider("primary", failing, is_default=True)
        mgr.registry.register_provider("backup", backup, is_default=False)
        mgr.tier_preferences = {"small": ["primary:dummy-small", "backup:dummy-small"]}

        resp = await mgr.complete([{"role": "user", "content": "hi"}], ModelTier.SMALL)
        # Should have used backup after primary failure
        assert resp.provider == "dummy"
        assert resp.model == "dummy-small"

    asyncio.run(_run())
