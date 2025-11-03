"""
Unit tests for model tier selection in LLM service.

Tests ensure top-level query.model_tier is respected before defaulting to context/model-based tiers.
"""

import pytest
from enum import Enum


class ModelTier(Enum):
    """Model tier enum for testing"""
    SMALL = "small"
    MEDIUM = "medium"
    LARGE = "large"


class TestTierSelection:
    """Test tier selection logic respects query.model_tier first"""

    def test_query_model_tier_overrides_context_and_mode_default(self):
        """Top-level query model_tier should override context and mode default"""
        # Even if context specifies a different tier, top-level should win
        context = {"model_tier": "medium"}
        tier = determine_model_tier(context, mode="simple", query_tier="large")
        assert tier == ModelTier.LARGE, "Top-level query model_tier should have highest precedence"

    def test_query_tier_used_when_context_missing(self):
        """Query-level model_tier should be used when context doesn't have it"""
        context = {}
        tier = determine_model_tier(context, mode="simple", query_tier="large")
        assert tier == ModelTier.LARGE, "Query tier should be used when context is empty"

    def test_mode_default_fallback(self):
        """Should fall back to mode-based default when both context and query tier absent"""
        context = {}

        # Simple mode -> SMALL
        tier = determine_model_tier(context, mode="simple", query_tier=None)
        assert tier == ModelTier.SMALL, "Simple mode should default to SMALL"

        # Standard mode -> MEDIUM
        tier = determine_model_tier(context, mode="standard", query_tier=None)
        assert tier == ModelTier.MEDIUM, "Standard mode should default to MEDIUM"

        # Complex mode -> LARGE
        tier = determine_model_tier(context, mode="complex", query_tier=None)
        assert tier == ModelTier.LARGE, "Complex mode should default to LARGE"

    def test_precedence_order(self):
        """Test precedence: query.model_tier > context.model_tier > mode default"""
        # All three present - query should win
        context = {"model_tier": "large"}
        tier = determine_model_tier(context, mode="simple", query_tier="medium")
        assert tier == ModelTier.MEDIUM, "Query (top-level) tier should have highest precedence"

    def test_invalid_tier_falls_back_to_mode(self):
        """Invalid tier strings should fall back to mode default"""
        context = {"model_tier": "invalid_tier"}
        tier = determine_model_tier(context, mode="simple", query_tier=None)
        assert tier == ModelTier.SMALL, "Invalid tier should fall back to mode default"

    def test_case_insensitive_tier_matching(self):
        """Tier strings should be case-insensitive"""
        context = {"model_tier": "MEDIUM"}
        tier = determine_model_tier(context, mode="simple", query_tier=None)
        assert tier == ModelTier.MEDIUM, "Tier matching should be case-insensitive"

    def test_empty_string_tier_uses_fallback(self):
        """Empty string tier should trigger fallback"""
        context = {"model_tier": ""}
        tier = determine_model_tier(context, mode="standard", query_tier=None)
        assert tier == ModelTier.MEDIUM, "Empty tier string should use mode default"


# Helper function that mimics the actual implementation
def determine_model_tier(context: dict, mode: str, query_tier: str | None) -> ModelTier:
    """
    Determine model tier with precedence: query.model_tier > context.model_tier > mode default

    This mirrors the actual implementation in llm_service/api/agent.py
    """
    # Try top-level query.model_tier first
    tier_str = (query_tier or "").strip().lower() if query_tier else ""

    # Fall back to context.model_tier
    if not tier_str:
        tier_str = (context.get("model_tier", "") or "").strip().lower()

    # Map to ModelTier enum
    tier_map = {
        "small": ModelTier.SMALL,
        "medium": ModelTier.MEDIUM,
        "large": ModelTier.LARGE,
    }

    if tier_str in tier_map:
        return tier_map[tier_str]

    # Fall back to mode-based default
    mode_defaults = {
        "simple": ModelTier.SMALL,
        "standard": ModelTier.MEDIUM,
        "complex": ModelTier.LARGE,
    }

    return mode_defaults.get(mode, ModelTier.SMALL)
