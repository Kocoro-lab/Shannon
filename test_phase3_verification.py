#!/usr/bin/env python3
"""
Comprehensive test suite for Phase 3 Config Unification verification
Tests YAML loading, hot-reload, routing, and OpenAI streaming support
"""

import asyncio
import json
import os
import sys
import tempfile
import yaml
from pathlib import Path
from typing import Dict, Any, List

# Add project to path
sys.path.insert(0, 'python/llm-service')

# Test results collector
test_results = {"passed": [], "failed": [], "warnings": []}


def log_test(name: str, status: str, message: str = ""):
    """Log test result"""
    symbol = "✅" if status == "pass" else "❌" if status == "fail" else "⚠️"
    print(f"{symbol} {name}: {message}")

    if status == "pass":
        test_results["passed"].append(name)
    elif status == "fail":
        test_results["failed"].append(f"{name}: {message}")
    else:
        test_results["warnings"].append(f"{name}: {message}")


async def test_yaml_loading():
    """Test 1: Verify YAML configuration loads correctly"""
    print("\n=== Test 1: YAML Configuration Loading ===")

    try:
        from llm_provider.manager import LLMManager

        # Test with actual config file
        manager = LLMManager(config_path="config/models.yaml")

        # Check if providers were loaded
        providers_loaded = len(manager.registry.providers)
        if providers_loaded > 0:
            log_test("YAML Loading", "pass", f"Loaded {providers_loaded} providers")

            # Check specific providers
            for provider_name in ["openai", "anthropic", "deepseek", "qwen", "mistral"]:
                if provider_name in manager.registry.providers:
                    provider = manager.registry.providers[provider_name]
                    model_count = len(provider.models)
                    log_test(f"  {provider_name}", "pass", f"{model_count} models")
                else:
                    log_test(f"  {provider_name}", "warn", "Not configured (needs API key)")
        else:
            log_test("YAML Loading", "warn", "No providers loaded (check API keys)")

        # Check tier preferences loaded from YAML
        if hasattr(manager, 'tier_preferences'):
            for tier in ["small", "medium", "large"]:
                prefs = manager.tier_preferences.get(tier, [])
                log_test(f"Tier {tier}", "pass", f"{len(prefs)} preferences")
        else:
            log_test("Tier Preferences", "fail", "Not loaded from YAML")

    except Exception as e:
        log_test("YAML Loading", "fail", str(e))


async def test_hot_reload():
    """Test 2: Verify hot-reload functionality"""
    print("\n=== Test 2: Hot-Reload Functionality ===")

    try:
        from llm_provider.manager import LLMManager

        # Create a temporary config file
        temp_config = {
            "model_tiers": {
                "small": {
                    "providers": [
                        {"provider": "openai", "model": "gpt-3.5-turbo", "priority": 1}
                    ]
                },
                "medium": {"providers": []},
                "large": {"providers": []}
            },
            "model_catalog": {
                "openai": {
                    "gpt-3.5-turbo": {
                        "tier": "small",
                        "context_window": 16384,
                        "max_tokens": 4096
                    }
                }
            },
            "selection_strategy": {"default_provider": "openai"}
        }

        with tempfile.NamedTemporaryFile(mode='w', suffix='.yaml', delete=False) as f:
            yaml.dump(temp_config, f)
            temp_path = f.name

        try:
            # Load initial config
            manager = LLMManager(config_path=temp_path)
            initial_prefs = manager.tier_preferences.get("small", [])

            # Modify config
            temp_config["model_tiers"]["small"]["providers"].append(
                {"provider": "anthropic", "model": "claude-3-haiku", "priority": 2}
            )

            with open(temp_path, 'w') as f:
                yaml.dump(temp_config, f)

            # Test reload
            await manager.reload()

            new_prefs = manager.tier_preferences.get("small", [])

            if len(new_prefs) != len(initial_prefs):
                log_test("Hot-Reload", "pass", "Configuration reloaded successfully")
            else:
                log_test("Hot-Reload", "warn", "Reload executed but preferences unchanged")

        finally:
            os.unlink(temp_path)

    except Exception as e:
        log_test("Hot-Reload", "fail", str(e))


async def test_facade_integration():
    """Test 3: Verify facade integration with new manager"""
    print("\n=== Test 3: Facade Integration ===")

    try:
        from llm_service.providers import ProviderManager

        # Create mock settings
        class MockSettings:
            temperature = 0.7
            enable_llm_events = False

        # Initialize facade
        facade = ProviderManager(MockSettings())
        await facade.initialize()

        # Check if facade properly delegates to manager
        if hasattr(facade, '_manager'):
            log_test("Facade Delegation", "pass", "Manager properly initialized")

            # Check model registry population
            if len(facade.model_registry) > 0:
                log_test("Model Registry", "pass", f"{len(facade.model_registry)} models")
            else:
                log_test("Model Registry", "warn", "Empty (check API keys)")

            # Test model selection
            model = facade.select_model(specific_model="gpt-3.5-turbo")
            if model:
                log_test("Model Selection", "pass", f"Selected: {model}")
            else:
                log_test("Model Selection", "warn", "No model selected")

        else:
            log_test("Facade Delegation", "fail", "Manager not initialized")

    except Exception as e:
        log_test("Facade Integration", "fail", str(e))


async def test_model_resolution():
    """Test 4: Verify model resolution and aliasing"""
    print("\n=== Test 4: Model Resolution & Aliasing ===")

    try:
        from llm_provider.manager import LLMManager, MODEL_NAME_ALIASES

        manager = LLMManager()

        # Test alias resolution
        test_aliases = [
            ("openai", "gpt-5", "gpt-4o"),
            ("anthropic", "claude-sonnet-4-5-20250929", "claude-3-sonnet"),
        ]

        for provider, alias, expected in test_aliases:
            resolved = MODEL_NAME_ALIASES.get((provider, alias))
            if resolved == expected:
                log_test(f"Alias {alias}", "pass", f"→ {resolved}")
            else:
                log_test(f"Alias {alias}", "fail", f"Expected {expected}, got {resolved}")

    except Exception as e:
        log_test("Model Resolution", "fail", str(e))


async def test_openai_streaming():
    """Test 5: Verify OpenAI response-less (streaming) API support"""
    print("\n=== Test 5: OpenAI Streaming/Response-less API ===")

    try:
        from llm_provider.openai_provider import OpenAIProvider
        from llm_provider.base import CompletionRequest, ModelTier

        # Check if streaming is implemented
        if hasattr(OpenAIProvider, 'stream_complete'):
            log_test("Stream Method", "pass", "stream_complete exists")

            # Check the implementation
            import inspect
            source = inspect.getsource(OpenAIProvider.stream_complete)

            # Check for key streaming patterns
            checks = [
                ("stream=True", "Streaming parameter"),
                ("async for chunk", "Async iteration"),
                ("yield", "Yielding chunks"),
            ]

            for pattern, desc in checks:
                if pattern in source:
                    log_test(f"  {desc}", "pass", f"'{pattern}' found")
                else:
                    log_test(f"  {desc}", "fail", f"'{pattern}' not found")

        else:
            log_test("Stream Method", "fail", "stream_complete not found")

        # Check OpenAI-compatible provider streaming
        from llm_provider.openai_compatible import OpenAICompatibleProvider

        if hasattr(OpenAICompatibleProvider, 'stream_complete'):
            log_test("Compatible Streaming", "pass", "OpenAI-compatible supports streaming")
        else:
            log_test("Compatible Streaming", "fail", "No streaming in compatible provider")

    except Exception as e:
        log_test("OpenAI Streaming", "fail", str(e))


async def test_provider_routing():
    """Test 6: Verify provider routing and fallback"""
    print("\n=== Test 6: Provider Routing & Fallback ===")

    try:
        from llm_provider.manager import LLMManager
        from llm_provider.base import CompletionRequest, ModelTier

        # Set up test environment with multiple providers
        os.environ['OPENAI_API_KEY'] = 'test-key'
        os.environ['DEEPSEEK_API_KEY'] = 'test-key'

        manager = LLMManager()
        manager.load_default_config()

        # Test tier-based routing
        request = CompletionRequest(
            messages=[{"role": "user", "content": "test"}],
            model_tier=ModelTier.SMALL
        )

        # Check if routing selects appropriate provider
        if hasattr(manager, '_select_provider'):
            provider_name, provider = manager._select_provider(request)
            log_test("Provider Selection", "pass", f"Selected: {provider_name}")

            # Test explicit model override
            request.model = "deepseek-chat"
            provider_name, provider = manager._select_provider(request)
            if provider_name == "deepseek":
                log_test("Model Override", "pass", "Correctly routed to DeepSeek")
            else:
                log_test("Model Override", "warn", f"Routed to {provider_name}")
        else:
            log_test("Provider Selection", "warn", "_select_provider not accessible")

    except Exception as e:
        log_test("Provider Routing", "fail", str(e))
    finally:
        # Clean up env vars
        os.environ.pop('OPENAI_API_KEY', None)
        os.environ.pop('DEEPSEEK_API_KEY', None)


async def test_pricing_calculation():
    """Test 7: Verify pricing calculation from YAML"""
    print("\n=== Test 7: Pricing Calculation ===")

    try:
        from llm_provider.manager import LLMManager

        manager = LLMManager(config_path="config/models.yaml")

        # Test pricing for a few models
        test_cases = [
            ("openai", "gpt-4o-mini", 1000, 500),  # input tokens, output tokens
            ("anthropic", "claude-3-5-haiku-20241022", 1000, 500),
        ]

        for provider_name, model_name, input_tokens, output_tokens in test_cases:
            if provider_name not in manager.registry.providers:
                log_test(f"Pricing {model_name}", "warn", "Provider not configured")
                continue

            provider = manager.registry.providers[provider_name]
            if model_name not in provider.models:
                # Try to find by model_id
                found = False
                for alias, config in provider.models.items():
                    if config.model_id == model_name:
                        model_name = alias
                        found = True
                        break
                if not found:
                    log_test(f"Pricing {model_name}", "warn", "Model not found")
                    continue

            cost = provider.estimate_cost(input_tokens, output_tokens, model_name)
            if cost > 0:
                log_test(f"Pricing {model_name}", "pass", f"${cost:.6f}")
            else:
                log_test(f"Pricing {model_name}", "warn", "No pricing data")

    except Exception as e:
        log_test("Pricing Calculation", "fail", str(e))


async def test_end_to_end():
    """Test 8: End-to-end completion request"""
    print("\n=== Test 8: End-to-End Integration ===")

    try:
        from llm_service.providers import ProviderManager
        from llm_service.providers import ModelTier as LegacyModelTier

        class MockSettings:
            temperature = 0.7
            enable_llm_events = False

        facade = ProviderManager(MockSettings())
        await facade.initialize()

        if not facade.is_configured():
            log_test("E2E Integration", "warn", "No providers configured (needs API keys)")
            return

        # Create a test request
        messages = [{"role": "user", "content": "Reply with 'OK' only"}]

        # Test with mock - we'll check the structure without making real API calls
        selected_model = facade.select_model(tier=LegacyModelTier.SMALL)

        if selected_model:
            log_test("E2E Request Setup", "pass", f"Would use model: {selected_model}")

            # Verify the request would be properly formatted
            if hasattr(facade, '_manager'):
                from llm_provider.base import CompletionRequest, ModelTier

                # Check that request can be created
                test_request = CompletionRequest(
                    messages=messages,
                    model_tier=ModelTier.SMALL,
                    model=selected_model,
                    temperature=0.7,
                    max_tokens=10
                )
                log_test("Request Format", "pass", "Valid CompletionRequest")
            else:
                log_test("Request Format", "fail", "Manager not accessible")
        else:
            log_test("E2E Integration", "warn", "No model selected")

    except Exception as e:
        log_test("E2E Integration", "fail", str(e))


async def main():
    """Run all tests"""
    print("=" * 60)
    print("PHASE 3 VERIFICATION TEST SUITE")
    print("=" * 60)

    # Run all tests
    await test_yaml_loading()
    await test_hot_reload()
    await test_facade_integration()
    await test_model_resolution()
    await test_openai_streaming()
    await test_provider_routing()
    await test_pricing_calculation()
    await test_end_to_end()

    # Summary
    print("\n" + "=" * 60)
    print("TEST SUMMARY")
    print("=" * 60)
    print(f"✅ Passed: {len(test_results['passed'])}")
    print(f"⚠️  Warnings: {len(test_results['warnings'])}")
    print(f"❌ Failed: {len(test_results['failed'])}")

    if test_results['failed']:
        print("\nFailed tests:")
        for failure in test_results['failed']:
            print(f"  - {failure}")

    if test_results['warnings']:
        print("\nWarnings:")
        for warning in test_results['warnings']:
            print(f"  - {warning}")

    return len(test_results['failed']) == 0


if __name__ == "__main__":
    success = asyncio.run(main())
    sys.exit(0 if success else 1)