"""Legacy provider base definitions for backward compatibility."""

from dataclasses import dataclass
from enum import Enum
from typing import Any


class ModelTier(Enum):
    """Model tier enumeration for legacy API."""

    SMALL = "small"
    MEDIUM = "medium"
    LARGE = "large"


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
