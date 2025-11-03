import asyncio
import unittest

from llm_provider.base import (
    LLMProvider,
    CompletionRequest,
    CompletionResponse,
    TokenUsage,
    ModelConfig,
    ModelTier,
)
from llm_provider.manager import LLMManager


class FakeProvider(LLMProvider):
    def __init__(self, name: str, models: dict[str, ModelConfig]):
        self._name = name
        self.models = models

    def _initialize_models(self):
        # noop: tests inject models directly
        return

    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        model = request.model or next(iter(self.models.keys()))
        return CompletionResponse(
            content="ok",
            model=model,
            provider=self._name,
            usage=TokenUsage(0, 0, 0, 0.0),
            finish_reason="stop",
        )

    async def stream_complete(self, request: CompletionRequest):
        yield "ok"

    def count_tokens(self, messages, model: str) -> int:
        return 0


def mk_model(provider: str, model_id: str, tier: ModelTier) -> ModelConfig:
    return ModelConfig(
        provider=provider,
        model_id=model_id,
        tier=tier,
        max_tokens=4096,
        context_window=8192,
        input_price_per_1k=0.0,
        output_price_per_1k=0.0,
    )


class ProviderSelectionTests(unittest.TestCase):
    def test_provider_override_selects_provider(self):
        async def run():
            mgr = LLMManager()
            openai = FakeProvider("openai", {"gpt-5-2025-08-07": mk_model("openai", "gpt-5-2025-08-07", ModelTier.MEDIUM)})
            anthropic = FakeProvider("anthropic", {"claude-sonnet-4-5-20250929": mk_model("anthropic", "claude-sonnet-4-5-20250929", ModelTier.MEDIUM)})
            mgr.registry.providers = {"openai": openai, "anthropic": anthropic}
            req = CompletionRequest(messages=[{"role": "user", "content": "hi"}], model_tier=ModelTier.SMALL, provider_override="anthropic")
            name, prov = mgr._select_provider(req)
            self.assertEqual(name, "anthropic")
            self.assertIsInstance(prov, FakeProvider)

        asyncio.run(run())

    def test_direct_model_used_when_available(self):
        async def run():
            mgr = LLMManager()
            anthropic = FakeProvider(
                "anthropic",
                {"claude-sonnet-4-5-20250929": mk_model("anthropic", "claude-sonnet-4-5-20250929", ModelTier.MEDIUM)},
            )
            mgr.registry.providers = {"anthropic": anthropic}
            mgr.registry.default_provider = "anthropic"
            mgr.registry.tier_routing = {ModelTier.MEDIUM: ["anthropic"]}
            resp = await mgr.complete(
                messages=[{"role": "user", "content": "hi"}],
                model_tier=ModelTier.MEDIUM,
                model="claude-sonnet-4-5-20250929",
            )
            self.assertEqual(resp.model, "claude-sonnet-4-5-20250929")
            self.assertEqual(resp.provider, "anthropic")

        asyncio.run(run())

if __name__ == "__main__":
    unittest.main()
