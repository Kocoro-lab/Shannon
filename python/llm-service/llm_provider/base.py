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
import re
import time
import uuid
from datetime import datetime, timedelta


class ModelTier(Enum):
    """Model tier classification for cost optimization"""

    SMALL = "small"  # Fast, cheap models for simple tasks
    MEDIUM = "medium"  # Balanced performance/cost
    LARGE = "large"  # High-capability models for complex tasks


@dataclass
class ModelCapabilities:
    """Capabilities for a specific model (for dynamic API selection)."""

    supports_tools: bool = True
    supports_json_mode: bool = True
    supports_reasoning: bool = False
    supports_vision: bool = False
    supports_streaming: bool = True
    max_parallel_tools: int = 1


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
    capabilities: ModelCapabilities = None

    @property
    def full_name(self) -> str:
        return f"{self.provider}:{self.model_id}"


def compute_token_cost(
    input_per_1k: float,
    output_per_1k: float,
    input_tokens: int,
    output_tokens: int,
    cache_read_tokens: int = 0,
    cache_creation_tokens: int = 0,
    cache_creation_1h_tokens: int = 0,
    provider: str = "openai",
) -> float:
    """Pure cache-aware token cost (USD).

    Single source of truth for provider cache semantics. Mirrors Go's
    pricing.CostForSplitWithCache. Used by LLMProvider.estimate_cost and by
    tools (e.g. x_search) that compute cost without a full provider instance.
    """
    input_cost = (input_tokens / 1000) * input_per_1k
    output_cost = (output_tokens / 1000) * output_per_1k
    if provider in ("anthropic", "minimax"):
        # Cache tokens are separate from input_tokens.
        input_cost += (cache_read_tokens / 1000) * input_per_1k * 0.1
        cache_5m = max(0, cache_creation_tokens - cache_creation_1h_tokens)
        input_cost += (cache_5m / 1000) * input_per_1k * 1.25
        input_cost += (cache_creation_1h_tokens / 1000) * input_per_1k * 2.0
    elif provider in ("kimi", "xai") and cache_read_tokens > 0:
        # Cached tokens included in input at full price; bill at 25% (75% discount).
        input_cost -= (cache_read_tokens / 1000) * input_per_1k * 0.75
    elif cache_read_tokens > 0:
        # OpenAI default: cached tokens in input at full price, bill at 50%.
        input_cost -= (cache_read_tokens / 1000) * input_per_1k * 0.5
    return input_cost + output_cost


@dataclass
class TokenUsage:
    """Token usage tracking"""

    input_tokens: int
    output_tokens: int
    total_tokens: int
    estimated_cost: float
    cache_read_tokens: int = 0
    cache_creation_tokens: int = 0
    call_sequence: int = 0  # Monotonic counter: which LLM call within this provider session

    def __add__(self, other: "TokenUsage") -> "TokenUsage":
        return TokenUsage(
            input_tokens=self.input_tokens + other.input_tokens,
            output_tokens=self.output_tokens + other.output_tokens,
            total_tokens=self.total_tokens + other.total_tokens,
            estimated_cost=self.estimated_cost + other.estimated_cost,
            cache_read_tokens=self.cache_read_tokens + other.cache_read_tokens,
            cache_creation_tokens=self.cache_creation_tokens + other.cache_creation_tokens,
            call_sequence=max(self.call_sequence, other.call_sequence),
        )


@dataclass
class CompletionRequest:
    """Unified completion request format"""

    messages: List[Dict[str, Any]]
    model_tier: ModelTier = ModelTier.SMALL
    model: Optional[str] = None
    temperature: float = 0.7
    max_tokens: Optional[int] = None
    top_p: Optional[float] = None  # None means API default; explicit 1.0 if needed
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
    complexity_score: Optional[float] = (
        None  # Optional signal for dynamic API selection
    )
    # Provider override (e.g., "openai", "anthropic"). Optional.
    provider_override: Optional[str] = None
    # OpenAI Responses API: chain from previous response for cache reuse
    previous_response_id: Optional[str] = None
    # Anthropic structured outputs: output_config for constrained JSON decoding
    output_config: Optional[Dict] = None
    # Anthropic extended thinking config (e.g. {"type": "enabled", "budget_tokens": 5000})
    thinking: Optional[Dict] = None
    # OpenAI reasoning effort for o-models (minimal/low/medium/high)
    reasoning_effort: Optional[str] = None
    # Prompt-cache observability label — distinguishes call sites (e.g.
    # "agent_loop", "decompose", "tool_select", "synthesis"). Unset → "unknown".
    cache_source: Optional[str] = None

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
        if self.thinking:
            key_data["thinking"] = self.thinking
        if self.reasoning_effort:
            key_data["reasoning_effort"] = self.reasoning_effort

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
    tool_calls: Optional[List[Dict]] = None

    # Metadata
    request_id: Optional[str] = None  # Also used as response_id for OpenAI Responses API chaining
    created_at: datetime = None
    latency_ms: Optional[int] = None
    cached: bool = False
    effective_max_completion: Optional[int] = None  # Actual max completion after provider headroom clamp

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

    def estimate_cost(
        self,
        input_tokens: int,
        output_tokens: int,
        model: str,
        cache_read_tokens: int = 0,
        cache_creation_tokens: int = 0,
        cache_creation_1h_tokens: int = 0,
    ) -> float:
        """Estimate cost for the given token usage, including prompt cache pricing.

        Anthropic: input_tokens excludes cached tokens; cache_read at 10%, cache_creation at 125% (5min) or 200% (1h).
        OpenAI: input_tokens includes cached tokens; cache_read at 50% (discount applied here).
        """
        if model not in self.models:
            return 0.0

        model_config = self.models[model]
        return compute_token_cost(
            input_per_1k=model_config.input_price_per_1k,
            output_per_1k=model_config.output_price_per_1k,
            input_tokens=input_tokens,
            output_tokens=output_tokens,
            cache_read_tokens=cache_read_tokens,
            cache_creation_tokens=cache_creation_tokens,
            cache_creation_1h_tokens=cache_creation_1h_tokens,
            provider=model_config.provider,
        )

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
        max_tokens = int(
            meta.get("max_tokens", meta.get("max_output_tokens", context_window))
        )

        input_price = meta.get("input_price_per_1k")
        output_price = meta.get("output_price_per_1k")

        input_price = float(input_price) if input_price is not None else 0.0
        output_price = float(output_price) if output_price is not None else 0.0

        # Build capabilities
        capabilities = ModelCapabilities(
            supports_tools=bool(meta.get("supports_functions", True)),
            supports_json_mode=bool(meta.get("supports_json_mode", True)),
            supports_reasoning=bool(meta.get("supports_reasoning", False)),
            supports_vision=bool(meta.get("supports_vision", False)),
            supports_streaming=bool(meta.get("supports_streaming", True)),
            max_parallel_tools=int(meta.get("max_parallel_tools", 1)),
        )

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
            capabilities=capabilities,
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

    def delete(self, key: str) -> None:
        """Delete a cache entry if present"""
        try:
            if key in self.cache:
                del self.cache[key]
        except Exception:
            # Best-effort; ignore
            pass

    @property
    def hit_rate(self) -> float:
        """Calculate cache hit rate"""
        total = self.hits + self.misses
        return self.hits / total if total > 0 else 0.0


def extract_text_from_content(content) -> str:
    """Extract text from content that may be a string or list of content blocks."""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return " ".join(
            part.get("text", "") if isinstance(part, dict) and part.get("type") == "text"
            else part if isinstance(part, str)
            else ""
            for part in content
        )
    return str(content) if content else ""


def translate_content_for_openai(content):
    """Translate Anthropic-style content blocks to OpenAI format.

    Handles: text, image (base64 + url), image_url passthrough,
    shannon_attachment (internal marker), plain strings in lists,
    and unknown block types (passed through).
    """
    if isinstance(content, str):
        return content
    if not isinstance(content, list):
        return str(content) if content else ""
    blocks = []
    for block in content:
        if isinstance(block, str):
            blocks.append({"type": "text", "text": block})
        elif not isinstance(block, dict):
            continue
        elif block.get("type") == "shannon_attachment":
            media_type = block["media_type"]
            if block.get("source") == "url":
                # URL-based: route by media type
                if media_type == "application/pdf":
                    # OpenAI doesn't support URL-based PDF; degrade to text
                    blocks.append({"type": "text", "text": f"[PDF document: {block['url']}]"})
                else:
                    blocks.append({"type": "image_url", "image_url": {"url": block["url"]}})
            elif media_type == "application/pdf":
                data_uri = f"data:{media_type};base64,{block['data']}"
                blocks.append({
                    "type": "file",
                    "file": {
                        "filename": block.get("filename", "document.pdf"),
                        "file_data": data_uri,
                    },
                })
            elif media_type.startswith("image/"):
                data_uri = f"data:{media_type};base64,{block['data']}"
                blocks.append({"type": "image_url", "image_url": {"url": data_uri}})
            else:
                # Unsupported binary type: degrade to text description
                fname = block.get("filename", "file")
                blocks.append({"type": "text", "text": f"[Unsupported attachment: {fname} ({media_type})]"})
        elif block.get("type") == "image":
            source = block.get("source", {})
            if source.get("type") == "url":
                url = source.get("url", "")
            else:
                url = f"data:{source.get('media_type', 'image/png')};base64,{source.get('data', '')}"
            blocks.append({"type": "image_url", "image_url": {"url": url}})
        else:
            # text, image_url, and any future block types — pass through
            blocks.append(block)
    return blocks


_TOOL_CALL_PLACEHOLDER = re.compile(r'^\[tool_call:\s*\w+\]$')
_ERROR_MARKER = re.compile(r'^\[error:\s*.*\]$')
_FRIENDLY_ERROR_PREFIXES = (
    "I apologize", "I'm sorry", "Sorry, I encountered",
    "The request timed out", "Rate limit", "An error occurred",
)


def sanitize_completion_messages(messages: list) -> list:
    """Defensive sanitizer for /v1/completions message history.

    Strips malformed entries that cause provider API 400/500 errors:
    - Tool call placeholder strings ([tool_call: web_search])
    - Error marker strings ([error: agent failed to respond])
    - FriendlyAgentError boilerplate messages
    - Orphaned role:'tool' messages without preceding tool_calls/tool_use
    - Consecutive same-role messages (keeps last)
    """
    if not messages:
        return messages

    cleaned = []
    for msg in messages:
        role = msg.get("role", "")
        content = msg.get("content")

        # Skip messages with tool-call placeholder text
        if isinstance(content, str):
            stripped = content.strip()
            if _TOOL_CALL_PLACEHOLDER.match(stripped):
                continue
            if _ERROR_MARKER.match(stripped):
                continue
            if any(stripped.startswith(p) for p in _FRIENDLY_ERROR_PREFIXES) and len(stripped) < 500:
                continue

        cleaned.append(msg)

    # Drop orphaned role:'tool' messages (no preceding tool_calls/tool_use)
    result = []
    for i, msg in enumerate(cleaned):
        if msg.get("role") == "tool":
            # Look back for a valid tool-calling assistant message
            has_preceding_tool_call = False
            for j in range(len(result) - 1, -1, -1):
                prev = result[j]
                if prev.get("role") == "tool":
                    continue  # skip other tool results in this batch
                if prev.get("role") == "assistant":
                    if prev.get("tool_calls") or prev.get("function_call"):
                        has_preceding_tool_call = True
                    elif isinstance(prev.get("content"), list):
                        has_preceding_tool_call = any(
                            isinstance(b, dict) and b.get("type") == "tool_use"
                            for b in prev["content"]
                        )
                break
            if not has_preceding_tool_call:
                continue  # drop orphaned tool result
        result.append(msg)

    # Merge consecutive same-role messages (keep last, except tool)
    if len(result) > 1:
        merged = [result[0]]
        for msg in result[1:]:
            if msg.get("role") == merged[-1].get("role") and msg.get("role") != "tool":
                merged[-1] = msg  # keep the later message
            else:
                merged.append(msg)
    else:
        merged = list(result)

    # Strip orphaned tool_calls/tool_use from assistant messages whose
    # tool_result was dropped by earlier sanitization phases.
    # Both Anthropic and OpenAI reject tool_use without a matching tool_result.
    final = []
    for i, msg in enumerate(merged):
        if msg.get("role") != "assistant":
            final.append(msg)
            continue

        # Collect all tool call IDs from this assistant message
        tool_call_ids = set()
        if msg.get("tool_calls"):
            for tc in msg["tool_calls"]:
                tc_id = tc.get("id", "")
                if tc_id:
                    tool_call_ids.add(tc_id)
        if msg.get("function_call"):
            fc_name = msg["function_call"].get("name", "")
            tool_call_ids.add(f"fc_{fc_name}" if fc_name else "__fc__")
        if isinstance(msg.get("content"), list):
            for b in msg["content"]:
                if isinstance(b, dict) and b.get("type") == "tool_use":
                    tu_id = b.get("id", "")
                    if tu_id:
                        tool_call_ids.add(tu_id)

        if not tool_call_ids:
            final.append(msg)
            continue

        # Collect all tool result IDs from the immediately following messages
        result_ids = set()
        for j in range(i + 1, len(merged)):
            next_msg = merged[j]
            if next_msg.get("role") == "tool":
                tc_id = next_msg.get("tool_call_id", "")
                if tc_id:
                    result_ids.add(tc_id)
                continue  # keep scanning for more tool results in this batch
            if next_msg.get("role") == "user" and isinstance(next_msg.get("content"), list):
                for b in next_msg["content"]:
                    if isinstance(b, dict) and b.get("type") == "tool_result":
                        tu_id = b.get("tool_use_id", "")
                        if tu_id:
                            result_ids.add(tu_id)
                break
            break  # next message is something else entirely

        if tool_call_ids <= result_ids:
            # All tool calls have matching results
            final.append(msg)
        elif result_ids:
            # Partial match: strip only the unsatisfied tool calls
            satisfied = result_ids
            new_msg = dict(msg)
            if new_msg.get("tool_calls"):
                new_msg["tool_calls"] = [
                    tc for tc in new_msg["tool_calls"]
                    if tc.get("id", "") in satisfied
                ]
                if not new_msg["tool_calls"]:
                    del new_msg["tool_calls"]
            if new_msg.get("function_call") and f"fc_{new_msg['function_call'].get('name', '')}" not in satisfied:
                del new_msg["function_call"]
            if isinstance(new_msg.get("content"), list):
                new_msg["content"] = [
                    b for b in new_msg["content"]
                    if not (isinstance(b, dict) and b.get("type") == "tool_use"
                            and b.get("id", "") not in satisfied)
                ]
                if not new_msg["content"]:
                    new_msg["content"] = ""
            final.append(new_msg)
        else:
            # No matching results at all — strip all tool invocations, keep text
            stripped = {k: v for k, v in msg.items()
                        if k not in ("tool_calls", "function_call")}
            if isinstance(stripped.get("content"), list):
                stripped["content"] = [
                    b for b in stripped["content"]
                    if not (isinstance(b, dict) and b.get("type") == "tool_use")
                ]
                if all(isinstance(b, dict) and b.get("type") == "text" for b in stripped["content"]):
                    text = " ".join(b.get("text", "") for b in stripped["content"]).strip()
                    stripped["content"] = text if text else ""
                elif not stripped["content"]:
                    stripped["content"] = ""
            if not stripped.get("content") and stripped.get("content") != 0:
                continue
            final.append(stripped)

    return final


_TOOL_ID_PATTERN = re.compile(r'^[a-zA-Z0-9_-]+$')


def _ensure_tool_id(raw_id: str) -> str:
    """Ensure a tool ID is non-empty and contains only safe characters.

    Anthropic requires ^[a-zA-Z0-9_-]+$. OpenAI accepts any non-empty string.
    Returns raw_id if valid, otherwise generates a fallback.
    """
    if raw_id and _TOOL_ID_PATTERN.match(raw_id):
        return raw_id
    if raw_id:
        # Deterministic replacement for malformed IDs (e.g. dots, colons)
        suffix = hashlib.md5(raw_id.encode()).hexdigest()[:16]
    else:
        # Random fallback for missing/empty IDs
        suffix = uuid.uuid4().hex[:16]
    return f"tool_{suffix}"


def _has_function_call_tool_mismatch(messages):
    """Check if messages have legacy function_call + role:'tool' that OpenAI rejects.

    OpenAI requires role:'tool' messages to follow assistant messages with
    'tool_calls' (modern format), not 'function_call' (legacy format).
    """
    has_tool_role = any(m.get("role") == "tool" for m in messages)
    has_function_call = any(
        m.get("role") == "assistant" and m.get("function_call") and not m.get("tool_calls")
        for m in messages
    )
    return has_tool_role and has_function_call


def _upgrade_function_call_to_tool_calls(msg):
    """Convert assistant message from legacy function_call to modern tool_calls format.

    Input:  {"role": "assistant", "content": "...", "function_call": {"name": "...", "arguments": "..."}}
    Output: {"role": "assistant", "content": "...", "tool_calls": [{"id": "...", "type": "function", "function": {...}}]}
    """
    import json as _json

    fc = msg.get("function_call", {})
    if not fc:
        return msg

    name = fc.get("name", "")
    arguments = fc.get("arguments", "{}")
    if isinstance(arguments, dict):
        arguments = _json.dumps(arguments)

    tool_call_id = _ensure_tool_id(f"fc_{name}")
    upgraded = {k: v for k, v in msg.items() if k != "function_call"}
    upgraded["tool_calls"] = [{
        "id": tool_call_id,
        "type": "function",
        "function": {"name": name, "arguments": arguments},
    }]
    return upgraded


def prepare_openai_messages(messages):
    """Translate messages for OpenAI API, handling cross-provider format differences.

    Handles:
    - Anthropic-native content blocks (tool_use → tool_calls, tool_result → role:"tool")
    - Legacy function_call → tool_calls upgrade when role:"tool" messages present
    - role:"tool" messages pass through (already OpenAI-native)
    """
    import json as _json

    # Check if we need to upgrade function_call → tool_calls
    needs_fc_upgrade = _has_function_call_tool_mismatch(messages)

    # Fast path: all string content, no cross-provider blocks to convert
    if all(isinstance(msg.get("content"), (str, type(None))) for msg in messages
           if msg.get("role") != "tool"):
        if not needs_fc_upgrade:
            return messages
        # Only need function_call → tool_calls upgrade
        return [
            _upgrade_function_call_to_tool_calls(m) if m.get("role") == "assistant" and m.get("function_call") else m
            for m in messages
        ]

    result = []
    for msg in messages:
        role = msg.get("role", "")
        content = msg.get("content")

        # role:"tool" is already OpenAI-native, pass through
        if role == "tool":
            result.append(msg)
            continue

        # Assistant messages with legacy function_call → upgrade to tool_calls
        if needs_fc_upgrade and role == "assistant" and msg.get("function_call") and not msg.get("tool_calls"):
            result.append(_upgrade_function_call_to_tool_calls(msg))
            continue

        # Assistant messages with Anthropic tool_use content blocks
        if role == "assistant" and isinstance(content, list):
            tool_uses = [b for b in content if isinstance(b, dict) and b.get("type") == "tool_use"]
            if tool_uses:
                text_parts = []
                for b in content:
                    if isinstance(b, dict) and b.get("type") == "text":
                        text_parts.append(b.get("text", ""))
                    elif isinstance(b, str):
                        text_parts.append(b)
                text_content = "".join(text_parts) or ""
                openai_tool_calls = []
                for tu in tool_uses:
                    args = tu.get("input", {})
                    openai_tool_calls.append({
                        "id": _ensure_tool_id(tu.get("id", "")),
                        "type": "function",
                        "function": {
                            "name": tu.get("name", ""),
                            "arguments": _json.dumps(args) if isinstance(args, dict) else str(args),
                        },
                    })
                result.append({"role": "assistant", "content": text_content, "tool_calls": openai_tool_calls})
                continue

        # User messages with Anthropic tool_result content blocks
        if role == "user" and isinstance(content, list):
            tool_results = [b for b in content if isinstance(b, dict) and b.get("type") == "tool_result"]
            if tool_results:
                non_tool = [b for b in content if not (isinstance(b, dict) and b.get("type") == "tool_result")]
                for tr in tool_results:
                    tr_content = tr.get("content", "")
                    result.append({
                        "role": "tool",
                        "tool_call_id": _ensure_tool_id(tr.get("tool_use_id", "")),
                        "content": tr_content if isinstance(tr_content, str) else str(tr_content),
                    })
                if non_tool:
                    result.append({**msg, "content": translate_content_for_openai(non_tool)})
                continue

        # Default: translate content (handles images, etc.)
        # Guard: normalize None content to "" for assistant+tool_calls messages
        # (strict OpenAI-compatible providers reject null content in that shape)
        if isinstance(content, (str, type(None))):
            if content is None and role == "assistant" and msg.get("tool_calls"):
                result.append({**msg, "content": ""})
            else:
                result.append(msg)
        else:
            result.append({**msg, "content": translate_content_for_openai(content)})

    return result


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
        image_tokens = 0
        for message in messages:
            content = message.get("content", "")
            if isinstance(content, str):
                total_chars += len(content)
            elif isinstance(content, list):
                for item in content:
                    if isinstance(item, dict):
                        if item.get("type") in ("image", "image_url"):
                            image_tokens += 256  # midpoint: ~85 low-detail, ~765 high-detail
                        elif "text" in item:
                            total_chars += len(item["text"])

        # More accurate: 1 token per 3.5 characters
        base_tokens = int(total_chars / 3.5) + image_tokens

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
    """Rate limiting for API calls with RPM + concurrency control"""

    def __init__(self, requests_per_minute: int = 60, max_concurrent: int = 0):
        self.requests_per_minute = requests_per_minute
        self.requests = []
        # Concurrency semaphore: 0 = unlimited, >0 = limit concurrent API calls
        self._semaphore = asyncio.Semaphore(max_concurrent) if max_concurrent > 0 else None

    async def acquire(self):
        """Acquire permission to make a request (concurrency + RPM check).

        When max_concurrent > 0, this blocks until a concurrency slot opens,
        then checks the RPM window. The semaphore is released automatically
        after a brief hold period (simulating request duration awareness)
        via a background task, so callers don't need to explicitly release.
        """
        while True:
            # Concurrency gate: block until a slot is available
            if self._semaphore:
                await self._semaphore.acquire()

            now = time.time()

            # rpm=0 means unlimited — skip RPM tracking entirely
            if self.requests_per_minute <= 0:
                if self._semaphore:
                    asyncio.get_running_loop().call_later(15.0, self._safe_release)
                return

            # Remove old requests outside the window
            self.requests = [r for r in self.requests if now - r < 60]

            # Check if we're at the limit
            if len(self.requests) >= self.requests_per_minute:
                # Release concurrency slot before retry to avoid deadlock
                if self._semaphore:
                    self._semaphore.release()
                # Calculate wait time
                oldest_request = min(self.requests)
                wait_time = 60 - (now - oldest_request) + 0.1
                if wait_time > 0:
                    await asyncio.sleep(wait_time)
                continue

            # Add current request
            self.requests.append(now)

            # Auto-release concurrency slot after 15 seconds (LLM calls typically 5-20s)
            # This prevents deadlocks without requiring callers to release
            if self._semaphore:
                asyncio.get_running_loop().call_later(15.0, self._safe_release)
            return

    def _safe_release(self):
        """Release concurrency slot (called by timer)"""
        if self._semaphore:
            try:
                self._semaphore.release()
            except ValueError:
                pass  # Already released
