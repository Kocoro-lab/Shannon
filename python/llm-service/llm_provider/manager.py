"""
Unified LLM Manager
Orchestrates multiple providers with caching, routing, and token management
"""

import os
import yaml
from typing import Dict, List, Any, Optional
from datetime import datetime
import logging

from .base import (
    LLMProvider,
    LLMProviderRegistry,
    CompletionRequest,
    CompletionResponse,
    ModelTier,
    CacheManager,
    RateLimiter,
    TokenUsage,
)
from .openai_provider import OpenAIProvider
from .anthropic_provider import AnthropicProvider
from .openai_compatible import OpenAICompatibleProvider
from .google_provider import GoogleProvider
from .groq_provider import GroqProvider


class LLMManager:
    """
    Main LLM management class that handles:
    - Provider registration and routing
    - Model tiering and selection
    - Caching and rate limiting
    - Token budget enforcement
    - Usage tracking and reporting
    """

    def __init__(self, config_path: Optional[str] = None):
        self.logger = logging.getLogger(__name__)
        self.registry = LLMProviderRegistry()
        self.cache = CacheManager(max_size=1000)
        self.rate_limiters: Dict[str, RateLimiter] = {}
        self._pricing_overrides: Optional[Dict[str, Any]] = None
        self._config_path: Optional[str] = None

        # Token budget tracking
        self.session_usage: Dict[str, TokenUsage] = {}
        self.task_usage: Dict[str, TokenUsage] = {}

        # Load configuration
        if config_path:
            self.load_config(config_path)
        else:
            # Try unified config first (MODELS_CONFIG_PATH → /app/config/models.yaml → ./config/models.yaml)
            auto_paths = [
                os.getenv("MODELS_CONFIG_PATH", "").strip(),
                "/app/config/models.yaml",
                "./config/models.yaml",
            ]
            cfg_path = next((p for p in auto_paths if p and os.path.exists(p)), None)
            if cfg_path:
                self.load_config(cfg_path)
            else:
                self.load_default_config()
        # Apply centralized pricing overrides after providers are loaded
        try:
            self._load_and_apply_pricing_overrides()
        except Exception as e:
            self.logger.warning(f"Pricing overrides not applied: {e}")

    def load_config(self, config_path: str):
        """Load configuration from YAML file. Supports both unified and legacy formats."""
        self._config_path = config_path
        with open(config_path, "r") as f:
            config = yaml.safe_load(f) or {}

        if "model_catalog" in config or "model_tiers" in config:
            # Unified config format (config/models.yaml)
            providers_cfg, routing_cfg, caching_cfg = self._translate_unified_config(config)
            self._initialize_providers(providers_cfg)
            self._configure_routing(routing_cfg)
            self._configure_caching(caching_cfg)
        else:
            # Legacy format
            self._initialize_providers(config.get("providers", {}))
            self._configure_routing(config.get("routing", {}))
            self._configure_caching(config.get("caching", {}))

    def load_default_config(self):
        """Load default configuration from environment variables"""
        config = {
            "providers": {},
            "routing": {
                "default_provider": "openai",
                "tier_preferences": {
                    "small": ["openai:gpt-3.5-turbo", "anthropic:claude-3-haiku"],
                    "medium": ["openai:gpt-4", "anthropic:claude-3-sonnet"],
                    "large": ["openai:gpt-4-turbo", "anthropic:claude-3-opus"],
                },
            },
            "caching": {"enabled": True, "max_size": 1000, "default_ttl": 3600},
        }

        # Initialize providers from environment
        if os.getenv("OPENAI_API_KEY"):
            config["providers"]["openai"] = {
                "type": "openai",
                "api_key": os.getenv("OPENAI_API_KEY"),
            }

        if os.getenv("ANTHROPIC_API_KEY"):
            config["providers"]["anthropic"] = {
                "type": "anthropic",
                "api_key": os.getenv("ANTHROPIC_API_KEY"),
            }

        # DeepSeek (OpenAI-compatible)
        if os.getenv("DEEPSEEK_API_KEY"):
            config["providers"]["deepseek"] = {
                "type": "openai_compatible",
                "api_key": os.getenv("DEEPSEEK_API_KEY"),
                "base_url": "https://api.deepseek.com",
                "models": {
                    "deepseek-chat": {
                        "tier": "small",
                        "context_window": 32768,
                        "input_price_per_1k": 0.0001,
                        "output_price_per_1k": 0.0002,
                    },
                    "deepseek-coder": {
                        "tier": "medium",
                        "context_window": 16384,
                        "input_price_per_1k": 0.0001,
                        "output_price_per_1k": 0.0002,
                    },
                },
            }

        # Qwen (OpenAI-compatible)
        if os.getenv("QWEN_API_KEY"):
            config["providers"]["qwen"] = {
                "type": "openai_compatible",
                "api_key": os.getenv("QWEN_API_KEY"),
                "base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1",
                "models": {
                    "qwen-turbo": {
                        "tier": "small",
                        "context_window": 8192,
                        "input_price_per_1k": 0.0003,
                        "output_price_per_1k": 0.0006,
                    },
                    "qwen-plus": {
                        "tier": "medium",
                        "context_window": 32768,
                        "input_price_per_1k": 0.0008,
                        "output_price_per_1k": 0.002,
                    },
                    "qwen-max": {
                        "tier": "large",
                        "context_window": 32768,
                        "input_price_per_1k": 0.002,
                        "output_price_per_1k": 0.006,
                    },
                },
            }

        # Google Gemini
        if os.getenv("GOOGLE_API_KEY"):
            config["providers"]["google"] = {
                "type": "google",
                "api_key": os.getenv("GOOGLE_API_KEY"),
            }

        # Groq (High-performance inference)
        if os.getenv("GROQ_API_KEY"):
            config["providers"]["groq"] = {
                "type": "groq",
                "api_key": os.getenv("GROQ_API_KEY"),
            }

        self._initialize_providers(config["providers"])
        self._configure_routing(config["routing"])
        self._configure_caching(config["caching"])

    def _initialize_providers(self, providers_config: Dict):
        """Initialize all configured providers"""
        # Reset registry and limiters when re-initializing
        self.registry = LLMProviderRegistry()
        self.rate_limiters = {}
        for name, config in providers_config.items():
            provider_type = config.get("type")

            try:
                if provider_type == "openai":
                    provider = OpenAIProvider(config)
                elif provider_type == "anthropic":
                    provider = AnthropicProvider(config)
                elif provider_type == "openai_compatible":
                    provider = OpenAICompatibleProvider(config)
                elif provider_type == "google":
                    provider = GoogleProvider(config)
                elif provider_type == "groq":
                    provider = GroqProvider(config)
                else:
                    self.logger.warning(f"Unknown provider type: {provider_type}")
                    continue

                self.registry.register_provider(
                    name, provider, is_default=(name == config.get("default"))
                )

                # Initialize rate limiter for provider
                rpm = config.get("requests_per_minute", 60)
                self.rate_limiters[name] = RateLimiter(rpm)

                self.logger.info(f"Initialized provider: {name}")
                # If pricing overrides already loaded, apply immediately
                try:
                    self._apply_pricing_overrides_for_provider(name, provider)
                except Exception as e:
                    self.logger.warning(
                        f"Failed to apply pricing overrides for {name}: {e}"
                    )

            except Exception as e:
                self.logger.error(f"Failed to initialize provider {name}: {e}")

    def _translate_unified_config(self, cfg: Dict[str, Any]) -> tuple[Dict[str, Any], Dict[str, Any], Dict[str, Any]]:
        """Translate unified config (model_catalog/model_tiers/selection_strategy) to internal structures."""
        model_catalog = cfg.get("model_catalog", {}) or {}
        provider_settings = cfg.get("provider_settings", {}) or {}
        model_tiers = cfg.get("model_tiers", {}) or {}
        selection = cfg.get("selection_strategy", {}) or {}
        prompt_cache = cfg.get("prompt_cache", {}) or {}

        # Provider type + env var mapping
        type_map = {
            "openai": ("openai", "OPENAI_API_KEY"),
            "anthropic": ("anthropic", "ANTHROPIC_API_KEY"),
            "google": ("google", "GOOGLE_API_KEY"),
            "groq": ("groq", "GROQ_API_KEY"),
            # OpenAI-compatible providers we support
            "deepseek": ("openai_compatible", "DEEPSEEK_API_KEY"),
            "qwen": ("openai_compatible", "QWEN_API_KEY"),
            # Others exist in config but not yet implemented here: mistral/xai/meta/cohere/bedrock/ollama
        }

        providers_cfg: Dict[str, Any] = {}
        for prov_name, models in model_catalog.items():
            if prov_name not in type_map:
                # Skip providers without a concrete implementation in this service
                continue
            ptype, env_key = type_map[prov_name]
            p_cfg: Dict[str, Any] = {"type": ptype, "models": {}}

            # API key from env if present
            api_key = os.getenv(env_key)
            if api_key:
                p_cfg["api_key"] = api_key

            # Base URL from provider_settings for OpenAI-compatible providers
            if ptype == "openai_compatible":
                base_url = (provider_settings.get(prov_name, {}) or {}).get("base_url")
                if base_url:
                    p_cfg["base_url"] = base_url

            # Copy over model metadata
            for alias, meta in (models or {}).items():
                p_cfg["models"][alias] = dict(meta or {})

            providers_cfg[prov_name] = p_cfg

        # Build routing preferences from model_tiers (ordered by priority)
        tier_prefs: Dict[str, List[str]] = {}
        for tier_name, tier_cfg in model_tiers.items():
            items = tier_cfg.get("providers", []) or []
            # Sort by 'priority' (lower is higher priority); if absent, keep order
            try:
                items = sorted(items, key=lambda x: int(x.get("priority", 9999)))
            except Exception:
                pass
            tier_prefs[tier_name] = [
                f"{it.get('provider')}:{it.get('model')}" for it in items if it.get("provider") and it.get("model")
            ]

        routing_cfg = {
            "default_provider": selection.get("default_provider", "openai"),
            "tier_preferences": tier_prefs,
        }

        caching_cfg = {
            "enabled": bool(prompt_cache.get("enabled", True)),
            "default_ttl": int(prompt_cache.get("ttl_seconds", 3600) or 3600),
            # Keep default size; unified file tracks size in MB for a different cache
            "max_size": 1000,
        }

        return providers_cfg, routing_cfg, caching_cfg

    def _configure_routing(self, routing_config: Dict):
        """Configure routing preferences"""
        self.routing_config = routing_config
        self.tier_preferences = routing_config.get("tier_preferences", {})

    def _configure_caching(self, caching_config: Dict):
        """Configure caching settings"""
        if caching_config.get("enabled", True):
            max_size = caching_config.get("max_size", 1000)
            self.cache = CacheManager(max_size=max_size)
        else:
            self.cache = None

        self.default_cache_ttl = caching_config.get("default_ttl", 3600)

    def _load_and_apply_pricing_overrides(self):
        """Load pricing overrides from /app/config/models.yaml and apply to providers."""
        config_path = os.getenv("MODELS_CONFIG_PATH", "/app/config/models.yaml")
        if not os.path.exists(config_path):
            return
        with open(config_path, "r") as f:
            cfg = yaml.safe_load(f) or {}
        pricing = cfg.get("pricing") or {}
        models = pricing.get("models") or {}
        if not models:
            return
        self._pricing_overrides = models
        for name, provider in self.registry.providers.items():
            self._apply_pricing_overrides_for_provider(name, provider)

    def _apply_pricing_overrides_for_provider(
        self, provider_name: str, provider: LLMProvider
    ):
        if not self._pricing_overrides:
            return
        prov_map = self._pricing_overrides.get(provider_name)
        if not prov_map:
            return
        # Update known models' pricing if present
        for key, model_cfg in provider.models.items():
            override = prov_map.get(model_cfg.model_id) or prov_map.get(key)
            if not override:
                continue
            ip = override.get("input_per_1k")
            op = override.get("output_per_1k")
            if isinstance(ip, (int, float)):
                model_cfg.input_price_per_1k = float(ip)
            if isinstance(op, (int, float)):
                model_cfg.output_price_per_1k = float(op)

    async def complete(
        self,
        messages: List[Dict[str, Any]],
        model_tier: ModelTier = ModelTier.SMALL,
        **kwargs,
    ) -> CompletionResponse:
        """
        Main completion method with automatic provider selection,
        caching, and rate limiting
        """

        # Create request object
        request = CompletionRequest(messages=messages, model_tier=model_tier, **kwargs)

        # Check cache if enabled
        if self.cache and not request.stream:
            cache_key = request.generate_cache_key()
            cached_response = self.cache.get(cache_key)
            if cached_response:
                self.logger.info(
                    f"Cache hit for request (hit rate: {self.cache.hit_rate:.2%})"
                )
                return cached_response

        # Select provider based on request
        provider_name, provider = self._select_provider(request)

        # Apply rate limiting
        if provider_name in self.rate_limiters:
            await self.rate_limiters[provider_name].acquire()

        # Track token budget if session/task specified
        if request.session_id:
            await self._check_session_budget(request)

        # Make the actual API call
        try:
            response = await provider.complete(request)

            # Update usage tracking
            self._update_usage_tracking(request, response)

            # Cache the response if applicable
            if self.cache and not request.stream:
                cache_ttl = request.cache_ttl or self.default_cache_ttl
                self.cache.set(cache_key, response, cache_ttl)

            return response

        except Exception as e:
            self.logger.error(f"Provider {provider_name} failed: {e}")

            # Try fallback provider if available
            fallback = self._get_fallback_provider(provider_name, request.model_tier)
            if fallback:
                self.logger.info(f"Trying fallback provider: {fallback[0]}")
                return await fallback[1].complete(request)

            raise

    def _select_provider(self, request: CompletionRequest) -> tuple[str, LLMProvider]:
        """Select the best provider for a request"""

        # Check tier preferences
        tier_prefs = self.tier_preferences.get(request.model_tier.value, [])

        for pref in tier_prefs:
            if ":" in pref:
                provider_name, model_id = pref.split(":", 1)
                if provider_name in self.registry.providers:
                    provider = self.registry.providers[provider_name]
                    # Check if provider has the model
                    if model_id in provider.models:
                        return provider_name, provider
            else:
                # Just provider name, use any model in tier
                if pref in self.registry.providers:
                    return pref, self.registry.providers[pref]

        # Fall back to registry's selection
        return self.registry.select_provider_for_request(request)

    def _get_fallback_provider(
        self, failed_provider: str, tier: ModelTier
    ) -> Optional[tuple[str, LLMProvider]]:
        """Get a fallback provider if the primary fails"""

        tier_prefs = self.tier_preferences.get(tier.value, [])

        for pref in tier_prefs:
            provider_name = pref.split(":")[0] if ":" in pref else pref
            if (
                provider_name != failed_provider
                and provider_name in self.registry.providers
            ):
                return provider_name, self.registry.providers[provider_name]

        return None

    async def _check_session_budget(self, request: CompletionRequest):
        """Check and enforce session-level token budget"""

        if request.session_id not in self.session_usage:
            self.session_usage[request.session_id] = TokenUsage(0, 0, 0, 0.0)

        # Get current usage
        current_usage = self.session_usage[request.session_id]

        # Enforce token budget limits
        max_tokens_per_session = 100000  # Default limit, should be configurable
        if hasattr(request, "max_tokens_budget"):
            max_tokens_per_session = request.max_tokens_budget

        if current_usage.total_tokens >= max_tokens_per_session:
            raise ValueError(
                f"Session {request.session_id} exceeded token budget: "
                f"{current_usage.total_tokens}/{max_tokens_per_session} tokens used"
            )

        self.logger.info(
            f"Session {request.session_id} usage: "
            f"{current_usage.total_tokens} tokens, "
            f"${current_usage.estimated_cost:.4f}"
        )

    def _update_usage_tracking(
        self, request: CompletionRequest, response: CompletionResponse
    ):
        """Update usage tracking for sessions and tasks"""

        # Update session usage
        if request.session_id:
            if request.session_id not in self.session_usage:
                self.session_usage[request.session_id] = TokenUsage(0, 0, 0, 0.0)
            self.session_usage[request.session_id] += response.usage

        # Update task usage
        if request.task_id:
            if request.task_id not in self.task_usage:
                self.task_usage[request.task_id] = TokenUsage(0, 0, 0, 0.0)
            self.task_usage[request.task_id] += response.usage

    def get_usage_report(
        self, session_id: Optional[str] = None, task_id: Optional[str] = None
    ) -> Dict[str, Any]:
        """Get usage report for a session or task"""

        report = {
            "timestamp": datetime.utcnow().isoformat(),
            "cache_hit_rate": self.cache.hit_rate if self.cache else 0.0,
        }

        if session_id and session_id in self.session_usage:
            usage = self.session_usage[session_id]
            report["session"] = {
                "id": session_id,
                "input_tokens": usage.input_tokens,
                "output_tokens": usage.output_tokens,
                "total_tokens": usage.total_tokens,
                "estimated_cost": usage.estimated_cost,
            }

        if task_id and task_id in self.task_usage:
            usage = self.task_usage[task_id]
            report["task"] = {
                "id": task_id,
                "input_tokens": usage.input_tokens,
                "output_tokens": usage.output_tokens,
                "total_tokens": usage.total_tokens,
                "estimated_cost": usage.estimated_cost,
            }

        return report

    def get_provider_status(self) -> Dict[str, Any]:
        """Get status of all registered providers"""

        status = {}
        for name, provider in self.registry.providers.items():
            status[name] = {
                "available": True,  # Could add health checks
                "models": list(provider.models.keys()),
                "rate_limit": {
                    "requests_per_minute": self.rate_limiters[name].requests_per_minute
                    if name in self.rate_limiters
                    else None
                },
            }

        return status

    async def reload(self) -> None:
        """Hot-reload configuration if a config path was provided or discovered."""
        try:
            if self._config_path and os.path.exists(self._config_path):
                self.load_config(self._config_path)
            else:
                # Fall back to auto-detection or env defaults
                auto_paths = [
                    os.getenv("MODELS_CONFIG_PATH", "").strip(),
                    "/app/config/models.yaml",
                    "./config/models.yaml",
                ]
                cfg_path = next((p for p in auto_paths if p and os.path.exists(p)), None)
                if cfg_path:
                    self.load_config(cfg_path)
                else:
                    self.load_default_config()

            # Re-apply centralized pricing if available
            try:
                self._load_and_apply_pricing_overrides()
            except Exception as e:
                self.logger.warning(f"Pricing overrides not applied on reload: {e}")
        except Exception as e:
            self.logger.error(f"Reload failed: {e}")

    async def generate_embedding(self, text: str, model: Optional[str] = None) -> List[float]:
        """Generate embeddings via the first capable provider (prefers OpenAI)."""
        # Prefer OpenAI if available
        if "openai" in self.registry.providers:
            provider = self.registry.providers["openai"]
            gen = getattr(provider, "generate_embedding", None)
            if gen:
                return await gen(text, model or "text-embedding-3-small")

        # Fallback to any provider exposing generate_embedding
        for provider in self.registry.providers.values():
            gen = getattr(provider, "generate_embedding", None)
            if gen:
                return await gen(text, model)

        raise ValueError("No embedding-capable providers are configured")


# Singleton instance
_manager_instance: Optional[LLMManager] = None


def get_llm_manager(config_path: Optional[str] = None) -> LLMManager:
    """Get or create the singleton LLM manager instance"""
    global _manager_instance

    if _manager_instance is None:
        _manager_instance = LLMManager(config_path)

    return _manager_instance
