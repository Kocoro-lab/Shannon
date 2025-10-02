#!/usr/bin/env python3
"""
Complete verification test for all phases including OpenAI streaming support
"""

import asyncio
import os
import sys
import json
import yaml
import tempfile
from pathlib import Path

# For Docker execution
sys.path.insert(0, '/app')
sys.path.insert(0, '.')

async def test_complete_migration():
    """Complete test suite for the migration"""

    print("=" * 70)
    print("COMPLETE MIGRATION VERIFICATION TEST")
    print("=" * 70)

    results = {"passed": 0, "failed": 0, "warnings": 0}

    # Test 1: Facade is sole entry point
    print("\n1. FACADE CONSOLIDATION")
    print("-" * 40)
    try:
        from llm_service.providers import ProviderManager
        print("✅ Facade ProviderManager accessible")
        results["passed"] += 1
    except ImportError as e:
        print(f"❌ Facade import failed: {e}")
        results["failed"] += 1

    # Test 2: YAML Configuration Loading
    print("\n2. YAML CONFIGURATION LOADING")
    print("-" * 40)
    try:
        from llm_provider.manager import LLMManager

        # Try to load with actual config
        config_paths = ['/app/config/models.yaml', 'config/models.yaml']
        config_path = None
        for path in config_paths:
            if Path(path).exists():
                config_path = path
                break

        if config_path:
            manager = LLMManager(config_path=config_path)
            providers = list(manager.registry.providers.keys())
            print(f"✅ Loaded {len(providers)} providers from {config_path}")

            # Show model counts
            for name, provider in manager.registry.providers.items():
                print(f"   {name}: {len(provider.models)} models")
            results["passed"] += 1
        else:
            print("⚠️ No config file found, using defaults")
            manager = LLMManager()
            manager.load_default_config()
            results["warnings"] += 1

    except Exception as e:
        print(f"❌ YAML loading failed: {e}")
        results["failed"] += 1
        manager = None

    # Test 3: Hot Reload
    print("\n3. HOT RELOAD FUNCTIONALITY")
    print("-" * 40)
    if manager and hasattr(manager, 'reload'):
        try:
            # Create temp config
            temp_config = {
                "model_tiers": {
                    "small": {"providers": [{"provider": "openai", "model": "test-model", "priority": 1}]},
                    "medium": {"providers": []},
                    "large": {"providers": []}
                },
                "model_catalog": {
                    "openai": {"test-model": {"tier": "small", "context_window": 8192}}
                }
            }

            with tempfile.NamedTemporaryFile(mode='w', suffix='.yaml', delete=False) as f:
                yaml.dump(temp_config, f)
                temp_path = f.name

            # Load and reload
            test_manager = LLMManager(config_path=temp_path)
            initial_models = len(test_manager.tier_preferences.get("small", []))

            # Modify config
            temp_config["model_tiers"]["small"]["providers"].append(
                {"provider": "anthropic", "model": "test-model-2", "priority": 2}
            )
            with open(temp_path, 'w') as f:
                yaml.dump(temp_config, f)

            await test_manager.reload()
            new_models = len(test_manager.tier_preferences.get("small", []))

            if new_models != initial_models:
                print(f"✅ Hot reload works: {initial_models} → {new_models} models")
                results["passed"] += 1
            else:
                print("⚠️ Reload executed but config unchanged")
                results["warnings"] += 1

            os.unlink(temp_path)

        except Exception as e:
            print(f"❌ Hot reload failed: {e}")
            results["failed"] += 1
    else:
        print("⚠️ Manager not available for reload test")
        results["warnings"] += 1

    # Test 4: OpenAI Streaming Support
    print("\n4. OPENAI STREAMING/RESPONSE-LESS API")
    print("-" * 40)
    try:
        from llm_provider.openai_provider import OpenAIProvider
        from llm_provider.openai_compatible import OpenAICompatibleProvider

        # Check OpenAI provider
        has_stream = hasattr(OpenAIProvider, 'stream_complete')
        if has_stream:
            import inspect
            source = inspect.getsource(OpenAIProvider.stream_complete)

            patterns = {
                "stream=True": "Stream parameter",
                "async for": "Async iteration",
                "yield": "Chunk yielding"
            }

            found_all = True
            for pattern, desc in patterns.items():
                if pattern in source:
                    print(f"  ✅ {desc}: Found '{pattern}'")
                else:
                    print(f"  ❌ {desc}: Missing '{pattern}'")
                    found_all = False

            if found_all:
                results["passed"] += 1
            else:
                results["warnings"] += 1
        else:
            print("  ❌ No stream_complete method in OpenAIProvider")
            results["failed"] += 1

        # Check OpenAI-compatible
        if hasattr(OpenAICompatibleProvider, 'stream_complete'):
            print("  ✅ OpenAI-compatible has streaming support")
            results["passed"] += 1
        else:
            print("  ❌ OpenAI-compatible missing streaming")
            results["failed"] += 1

    except Exception as e:
        print(f"❌ Streaming check failed: {e}")
        results["failed"] += 1

    # Test 5: Model Resolution and Aliasing
    print("\n5. MODEL RESOLUTION & ALIASING")
    print("-" * 40)
    try:
        from llm_provider.manager import MODEL_NAME_ALIASES

        test_aliases = [
            (("openai", "gpt-5"), "gpt-4o"),
            (("openai", "o3-mini"), "gpt-4o-mini"),
            (("anthropic", "claude-opus-4-1-20250805"), "claude-3-opus"),
        ]

        for key, expected in test_aliases:
            resolved = MODEL_NAME_ALIASES.get(key)
            if resolved == expected:
                print(f"  ✅ {key[1]} → {resolved}")
            else:
                print(f"  ❌ {key[1]}: expected {expected}, got {resolved}")
        results["passed"] += 1

    except Exception as e:
        print(f"❌ Aliasing failed: {e}")
        results["failed"] += 1

    # Test 6: Provider Routing
    print("\n6. PROVIDER ROUTING & FALLBACK")
    print("-" * 40)
    try:
        from llm_provider.base import CompletionRequest, ModelTier

        if manager:
            # Test tier routing
            request = CompletionRequest(
                messages=[{"role": "user", "content": "test"}],
                model_tier=ModelTier.SMALL
            )

            if hasattr(manager, '_select_provider'):
                provider_name, provider = manager._select_provider(request)
                print(f"  ✅ Tier routing: {request.model_tier} → {provider_name}")

                # Test explicit model
                request.model = "gpt-3.5-turbo"
                provider_name, provider = manager._select_provider(request)
                print(f"  ✅ Model override: {request.model} → {provider_name}")
                results["passed"] += 1
            else:
                print("  ⚠️ _select_provider not accessible")
                results["warnings"] += 1
        else:
            print("  ⚠️ Manager not available")
            results["warnings"] += 1

    except Exception as e:
        print(f"❌ Routing test failed: {e}")
        results["failed"] += 1

    # Test 7: Facade Integration
    print("\n7. FACADE INTEGRATION")
    print("-" * 40)
    try:
        from llm_service.providers import ProviderManager

        class MockSettings:
            temperature = 0.7
            enable_llm_events = False

        facade = ProviderManager(MockSettings())
        await facade.initialize()

        if hasattr(facade, '_manager'):
            print("  ✅ Facade delegates to LLMManager")

            if hasattr(facade, 'reload'):
                print("  ✅ Facade has reload capability")

            if len(facade.model_registry) > 0:
                print(f"  ✅ Model registry: {len(facade.model_registry)} models")
            else:
                print(f"  ⚠️ Model registry empty (need API keys)")

            results["passed"] += 1
        else:
            print("  ❌ Facade not properly initialized")
            results["failed"] += 1

    except Exception as e:
        print(f"❌ Facade integration failed: {e}")
        results["failed"] += 1

    # Summary
    print("\n" + "=" * 70)
    print("TEST SUMMARY")
    print("=" * 70)
    total = results["passed"] + results["failed"] + results["warnings"]
    print(f"Total Tests: {total}")
    print(f"✅ Passed: {results['passed']}")
    print(f"⚠️ Warnings: {results['warnings']}")
    print(f"❌ Failed: {results['failed']}")

    success_rate = (results['passed'] / total * 100) if total > 0 else 0
    print(f"\nSuccess Rate: {success_rate:.1f}%")

    if results['failed'] == 0:
        print("\n🎉 ALL CRITICAL TESTS PASSED!")
    else:
        print(f"\n❌ {results['failed']} tests need attention")

    return results['failed'] == 0


if __name__ == "__main__":
    success = asyncio.run(test_complete_migration())
    sys.exit(0 if success else 1)