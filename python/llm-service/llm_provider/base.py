"""
Base LLM Provider Abstraction Layer
Provides a unified interface for multiple LLM providers with token management,
caching, and model tiering support.
"""

from abc import ABC, abstractmethod
from dataclasses import dataclass
from typing import Dict, List, Optional, Any, Union, AsyncIterator
from enum import Enum
import asyncio
import hashlib
import json
import time
from datetime import datetime, timedelta


class ModelTier(Enum):
    """Model tier classification for cost optimization"""

    SMALL = "small"  # Fast, cheap models for simple tasks
    MEDIUM = "medium"  # Balanced performance/cost
    LARGE = "large"  # High-capability models for complex tasks


@dataclass
class ModelConfig:
    """Configuration for a specific model"""

    provider: str
    model_id: str
    tier: ModelTier
    max_tokens: int
    context_window: int
    input_price_per_1k: float
    output_price_per_1k: float
    supports_functions: bool = True
    supports_streaming: bool = True
    supports_vision: bool = False
    timeout: int = 60

    @property
    def full_name(self) -> str:
        return f"{self.provider}:{self.model_id}"


@dataclass
class TokenUsage:
    """Token usage tracking"""

    input_tokens: int
    output_tokens: int
    total_tokens: int
    estimated_cost: float

    def __add__(self, other: "TokenUsage") -> "TokenUsage":
        return TokenUsage(
            input_tokens=self.input_tokens + other.input_tokens,
            output_tokens=self.output_tokens + other.output_tokens,
            total_tokens=self.total_tokens + other.total_tokens,
            estimated_cost=self.estimated_cost + other.estimated_cost,
        )


@dataclass
class CompletionRequest:
    """Unified completion request format"""

    messages: List[Dict[str, Any]]
    model_tier: ModelTier = ModelTier.SMALL
    model: Optional[str] = None
    temperature: float = 0.7
    max_tokens: Optional[int] = None
    top_p: float = 1.0
    frequency_penalty: float = 0.0
    presence_penalty: float = 0.0
    stop: Optional[List[str]] = None
    functions: Optional[List[Dict]] = None
    function_call: Optional[Union[str, Dict]] = None
    stream: bool = False
    user: Optional[str] = None
    seed: Optional[int] = None
    response_format: Optional[Dict] = None

    # Shannon-specific parameters
    session_id: Optional[str] = None
    task_id: Optional[str] = None
    agent_id: Optional[str] = None
    cache_key: Optional[str] = None
    cache_ttl: int = 3600  # 1 hour default
    max_tokens_budget: Optional[int] = None

    def generate_cache_key(self) -> str:
        """Generate a cache key for this request"""
        if self.cache_key:
            return self.cache_key

        # Create deterministic hash of request parameters
        key_data = {
            "messages": self.messages,
            "model_tier": self.model_tier.value,
            "model": self.model,
            "temperature": self.temperature,
            "max_tokens": self.max_tokens,
            "functions": self.functions,
            "seed": self.seed,
        }

        key_json = json.dumps(key_data, sort_keys=True)
        return hashlib.sha256(key_json.encode()).hexdigest()


@dataclass
class CompletionResponse:
    """Unified completion response format"""

    content: str
    model: str
    provider: str
    usage: TokenUsage
    finish_reason: str
    function_call: Optional[Dict] = None

    # Metadata
    request_id: Optional[str] = None
    created_at: datetime = None
    latency_ms: Optional[int] = None
    cached: bool = False

    def __post_init__(self):
        if self.created_at is None:
            self.created_at = datetime.utcnow()


class LLMProvider(ABC):
    """Abstract base class for LLM providers"""

    def __init__(self, config: Dict[str, Any]):
        self.config = config
        self.models: Dict[str, ModelConfig] = {}
        self._initialize_models()

    @abstractmethod
    def _initialize_models(self):
        """Initialize available models for this provider"""
        pass

    @abstractmethod
    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        """Generate a completion for the given request"""
        pass

    @abstractmethod
    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator[str]:
        """Stream a completion for the given request"""
        pass

    @abstractmethod
    def count_tokens(self, messages: List[Dict[str, Any]], model: str) -> int:
        """Count tokens for the given messages"""
        pass

    def select_model_for_tier(
        self, tier: ModelTier, max_tokens: Optional[int] = None
    ) -> ModelConfig:
        """Select the best model for the given tier and constraints"""
        tier_models = [m for m in self.models.values() if m.tier == tier]

        if not tier_models:
            raise ValueError(f"No models available for tier {tier}")

        # Filter by max_tokens if specified
        if max_tokens:
            tier_models = [m for m in tier_models if m.context_window >= max_tokens]
            if not tier_models:
                raise ValueError(
                    f"No models in tier {tier} support {max_tokens} tokens"
                )

        # Return the first suitable model (could be enhanced with more logic)
        return tier_models[0]

    def estimate_cost(self, input_tokens: int, output_tokens: int, model: str) -> float:
        """Estimate cost for the given token usage"""
        if model not in self.models:
            return 0.0

        model_config = self.models[model]
        input_cost = (input_tokens / 1000) * model_config.input_price_per_1k
        output_cost = (output_tokens / 1000) * model_config.output_price_per_1k
        return input_cost + output_cost

    def _load_models_from_config(self, allow_empty: bool = False) -> None:
        """Populate provider models from configuration dictionary."""

        models_cfg = self.config.get("models") or {}

        if not models_cfg:
            if allow_empty:
                return
            raise ValueError(
                f"{self.__class__.__name__} requires model definitions in configuration"
            )

        provider_name = (
            self.config.get("provider_name")
            or self.config.get("name")
            or self.config.get("type")
            or self.__class__.__name__.replace("Provider", "").lower()
        )

        for alias, raw_meta in models_cfg.items():
            meta = raw_meta or {}
            self.models[alias] = self._make_model_config(provider_name, alias, meta)

    def _make_model_config(
        self, provider_name: str, alias: str, meta: Dict[str, Any]
    ) -> ModelConfig:
        """Create a ModelConfig object from raw metadata."""

        tier_value = meta.get("tier", "medium")
        if isinstance(tier_value, ModelTier):
            tier_enum = tier_value
        else:
            tier_enum = ModelTier(str(tier_value).lower())

        model_id = meta.get("model_id") or alias
        context_window = int(meta.get("context_window", meta.get("max_context", 8192)))
        max_tokens = int(meta.get("max_tokens", meta.get("max_output_tokens", context_window)))

        input_price = meta.get("input_price_per_1k")
        output_price = meta.get("output_price_per_1k")

        input_price = float(input_price) if input_price is not None else 0.0
        output_price = float(output_price) if output_price is not None else 0.0

        return ModelConfig(
            provider=str(meta.get("provider") or provider_name),
            model_id=model_id,
            tier=tier_enum,
            max_tokens=max_tokens,
            context_window=context_window,
            input_price_per_1k=input_price,
            output_price_per_1k=output_price,
            supports_functions=bool(meta.get("supports_functions", True)),
            supports_streaming=bool(meta.get("supports_streaming", True)),
            supports_vision=bool(meta.get("supports_vision", False)),
            timeout=int(meta.get("timeout", 60)),
        )

    def resolve_model_config(self, request: CompletionRequest) -> ModelConfig:
        """Resolve the model configuration for a request, honoring overrides."""

        if request.model:
            requested = request.model

            # Allow provider:model syntax
            if ":" in requested:
                _, requested = requested.split(":", 1)

            # Direct key lookup (alias)
            if requested in self.models:
                return self.models[requested]

            # Match by vendor model_id
            for config in self.models.values():
                if config.model_id == request.model or config.model_id == requested:
                    return config

            raise ValueError(
                f"Model '{request.model}' not available for provider {self.__class__.__name__}"
            )

        return self.select_model_for_tier(request.model_tier, request.max_tokens)


class LLMProviderRegistry:
    """Registry for managing multiple LLM providers"""

    def __init__(self):
        self.providers: Dict[str, LLMProvider] = {}
        self.default_provider = None
        self.tier_routing: Dict[ModelTier, List[str]] = {
            ModelTier.SMALL: [],
            ModelTier.MEDIUM: [],
            ModelTier.LARGE: [],
        }

    def register_provider(
        self, name: str, provider: LLMProvider, is_default: bool = False
    ):
        """Register a new provider"""
        self.providers[name] = provider

        if is_default:
            self.default_provider = name

        # Update tier routing
        for model in provider.models.values():
            provider_model = f"{name}:{model.model_id}"
            if provider_model not in self.tier_routing[model.tier]:
                self.tier_routing[model.tier].append(provider_model)

    def get_provider(self, name: str) -> LLMProvider:
        """Get a specific provider"""
        if name not in self.providers:
            raise ValueError(f"Provider {name} not registered")
        return self.providers[name]

    def select_provider_for_request(
        self, request: CompletionRequest
    ) -> tuple[str, LLMProvider]:
        """Select the best provider for a given request"""
        # Get available providers for the tier
        tier_providers = self.tier_routing.get(request.model_tier, [])

        if not tier_providers:
            # Fall back to default provider
            if self.default_provider:
                return self.default_provider, self.providers[self.default_provider]
            raise ValueError(f"No providers available for tier {request.model_tier}")

        # Simple round-robin or could implement more sophisticated routing
        # For now, return the first available
        provider_model = tier_providers[0]
        provider_name = provider_model.split(":")[0]

        return provider_name, self.providers[provider_name]


class CacheManager:
    """Simple in-memory cache for LLM responses"""

    def __init__(self, max_size: int = 1000):
        self.cache: Dict[str, tuple[CompletionResponse, datetime]] = {}
        self.max_size = max_size
        self.hits = 0
        self.misses = 0

    def get(self, key: str) -> Optional[CompletionResponse]:
        """Get a cached response"""
        if key in self.cache:
            response, expiry = self.cache[key]
            if datetime.utcnow() < expiry:
                self.hits += 1
                response.cached = True
                return response
            else:
                # Expired, remove from cache
                del self.cache[key]

        self.misses += 1
        return None

    def set(self, key: str, response: CompletionResponse, ttl: int = 3600):
        """Cache a response"""
        # Simple LRU: remove oldest if at capacity
        if len(self.cache) >= self.max_size:
            oldest_key = min(self.cache.keys(), key=lambda k: self.cache[k][1])
            del self.cache[oldest_key]

        expiry = datetime.utcnow() + timedelta(seconds=ttl)
        self.cache[key] = (response, expiry)

    @property
    def hit_rate(self) -> float:
        """Calculate cache hit rate"""
        total = self.hits + self.misses
        return self.hits / total if total > 0 else 0.0


class TokenCounter:
    """Token counting utilities for different models"""

    @staticmethod
    def count_messages_tokens(messages: List[Dict[str, Any]], model: str) -> int:
        """
        Estimate token count for messages.
        This is a simplified version - real implementation would use
        provider-specific tokenizers.

        Improved estimation based on empirical data:
        - Average English word is ~4.7 characters
        - Average token is ~0.75 words (due to BPE splitting)
        - Therefore: 1 token ≈ 3.5 characters (not 4)
        """
        total_chars = 0
        for message in messages:
            content = message.get("content", "")
            if isinstance(content, str):
                total_chars += len(content)
            elif isinstance(content, list):
                # Handle multi-modal content
                for item in content:
                    if isinstance(item, dict) and "text" in item:
                        total_chars += len(item["text"])

        # More accurate: 1 token per 3.5 characters
        base_tokens = int(total_chars / 3.5)

        # Add overhead for message structure
        message_overhead = len(messages) * 4  # ~4 tokens per message for role/structure

        return base_tokens + message_overhead

    @staticmethod
    def count_functions_tokens(functions: List[Dict]) -> int:
        """Estimate token count for function definitions"""
        if not functions:
            return 0

        # Serialize functions to estimate size
        functions_str = json.dumps(functions)
        # Use improved estimation: 1 token per 3.5 chars
        return int(len(functions_str) / 3.5)


class RateLimiter:
    """Rate limiting for API calls"""

    def __init__(self, requests_per_minute: int = 60):
        self.requests_per_minute = requests_per_minute
        self.requests = []

    async def acquire(self):
        """Acquire permission to make a request"""
        now = time.time()

        # Remove old requests outside the window
        self.requests = [r for r in self.requests if now - r < 60]

        # Check if we're at the limit
        if len(self.requests) >= self.requests_per_minute:
            # Calculate wait time
            oldest_request = min(self.requests)
            wait_time = 60 - (now - oldest_request) + 0.1
            if wait_time > 0:
                await asyncio.sleep(wait_time)

            # Recursive call after waiting
            return await self.acquire()

        # Add current request
        self.requests.append(now)
