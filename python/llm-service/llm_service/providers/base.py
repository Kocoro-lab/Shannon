from abc import ABC, abstractmethod
from typing import List, Any
from enum import Enum
from dataclasses import dataclass


class ModelTier(Enum):
    SMALL = "small"
    MEDIUM = "medium"
    LARGE = "large"


@dataclass
class ModelInfo:
    """Information about an LLM model"""

    id: str
    name: str
    provider: Any  # ProviderType
    tier: ModelTier
    context_window: int
    cost_per_1k_prompt_tokens: float
    cost_per_1k_completion_tokens: float
    supports_tools: bool
    supports_streaming: bool
    available: bool


@dataclass
class TokenUsage:
    """Token usage statistics"""

    prompt_tokens: int
    completion_tokens: int
    total_tokens: int
    cost_usd: float
    model: str


class LLMProvider(ABC):
    """Base class for LLM providers"""

    @abstractmethod
    async def initialize(self):
        """Initialize the provider"""
        pass

    @abstractmethod
    async def close(self):
        """Close provider connections"""
        pass

    @abstractmethod
    async def generate_completion(
        self,
        messages: List[dict],
        model: str,
        temperature: float = 0.7,
        max_tokens: int = 2000,
        **kwargs,
    ) -> dict:
        """Generate a completion"""
        pass

    @abstractmethod
    async def generate_embedding(self, text: str, model: str = None) -> List[float]:
        """Generate text embedding"""
        pass

    @abstractmethod
    def list_models(self) -> List[ModelInfo]:
        """List available models"""
        pass

    @abstractmethod
    def calculate_cost(
        self, prompt_tokens: int, completion_tokens: int, model: str
    ) -> float:
        """Calculate cost for token usage"""
        pass
