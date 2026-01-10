"""Legacy provider base definitions - re-exports from core for backward compatibility."""

from dataclasses import dataclass
from typing import Any

# Re-export ModelTier from canonical source to eliminate duplicate enum
from llm_provider.base import ModelTier

__all__ = ["ModelTier", "ModelInfo"]


@dataclass
class ModelInfo:
    """Model information for legacy API."""

    id: str
    name: str
    provider: Any  # Can be ProviderType enum or string
    tier: ModelTier
    context_window: int
    cost_per_1k_prompt_tokens: float
    cost_per_1k_completion_tokens: float
    supports_tools: bool = True
    supports_streaming: bool = True
    available: bool = True
