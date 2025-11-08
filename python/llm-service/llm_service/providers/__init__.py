from typing import Dict, List, Optional, Any
import logging
from enum import Enum

from llm_provider.manager import get_llm_manager, LLMManager
from llm_provider.base import (
    ModelTier as CoreModelTier,
    CompletionResponse,
    TokenUsage as CoreTokenUsage,
)

from .base import ModelInfo, ModelTier as LegacyModelTier

logger = logging.getLogger(__name__)


class ProviderType(Enum):
    OPENAI = "openai"
    ANTHROPIC = "anthropic"
    GOOGLE = "google"
    DEEPSEEK = "deepseek"
    QWEN = "qwen"
    BEDROCK = "bedrock"
    OLLAMA = "ollama"
    GROQ = "groq"
    MISTRAL = "mistral"
    XAI = "xai"


_PROVIDER_NAME_MAP: Dict[str, ProviderType] = {
    "openai": ProviderType.OPENAI,
    "anthropic": ProviderType.ANTHROPIC,
    "google": ProviderType.GOOGLE,
    "deepseek": ProviderType.DEEPSEEK,
    "qwen": ProviderType.QWEN,
    "bedrock": ProviderType.BEDROCK,
    "ollama": ProviderType.OLLAMA,
    "groq": ProviderType.GROQ,
    "mistral": ProviderType.MISTRAL,
    "xai": ProviderType.XAI,
}


class _ProviderAdapter:
    """Thin adapter that exposes list_models while delegating other attributes."""

    def __init__(
        self, provider_type: ProviderType, provider: Any, models: List[ModelInfo]
    ):
        self._provider_type = provider_type
        self._provider = provider
        self._models = models

    def list_models(self) -> List[ModelInfo]:
        return list(self._models)

    def __getattr__(self, item: str) -> Any:
        return getattr(self._provider, item)


class ProviderManager:
    """Facade that keeps legacy API surface while delegating to LLMManager."""

    def __init__(self, settings):
        self.settings = settings
        self._manager: LLMManager = get_llm_manager()
        self.providers: Dict[ProviderType, _ProviderAdapter] = {}
        self.model_registry: Dict[str, ModelInfo] = {}
        self.tier_models: Dict[LegacyModelTier, List[str]] = {
            LegacyModelTier.SMALL: [],
            LegacyModelTier.MEDIUM: [],
            LegacyModelTier.LARGE: [],
        }
        self.session_tokens: Dict[str, int] = {}
        self.max_tokens_per_session = 100000
        self._emitter = None

    async def initialize(self) -> None:
        """Load provider metadata from the unified manager."""
        self._refresh_registry()

    async def reload(self) -> None:
        """Hot-reload provider configuration from the unified manager."""

        await self._manager.reload()
        self._refresh_registry()

    def _refresh_registry(self) -> None:
        self.providers.clear()
        self.model_registry.clear()
        for tier in self.tier_models:
            self.tier_models[tier] = []

        for name, provider in self._manager.registry.providers.items():
            provider_type = _PROVIDER_NAME_MAP.get(name)
            models = self._collect_models(provider_type, provider)
            if provider_type:
                self.providers[provider_type] = _ProviderAdapter(
                    provider_type, provider, models
                )

    def _collect_models(
        self, provider_type: Optional[ProviderType], provider: Any
    ) -> List[ModelInfo]:
        models: List[ModelInfo] = []

        for alias, config in provider.models.items():
            info = self._build_model_info(provider_type, alias, config)
            models.append(info)
            self.model_registry[alias] = info
            if config.model_id != alias:
                self.model_registry[config.model_id] = info
            if info.tier not in self.tier_models:
                self.tier_models[info.tier] = []
            if alias not in self.tier_models[info.tier]:
                self.tier_models[info.tier].append(alias)

        return models

    @staticmethod
    def _build_model_info(
        provider_type: Optional[ProviderType], alias: str, config: Any
    ) -> ModelInfo:
        legacy_tier = LegacyModelTier(config.tier.value)
        provider_value: Any = (
            provider_type if provider_type else (config.provider or "unknown")
        )

        return ModelInfo(
            id=alias,
            name=alias,
            provider=provider_value,
            tier=legacy_tier,
            context_window=config.context_window,
            cost_per_1k_prompt_tokens=config.input_price_per_1k,
            cost_per_1k_completion_tokens=config.output_price_per_1k,
            supports_tools=getattr(config, "supports_functions", True),
            supports_streaming=getattr(config, "supports_streaming", True),
            available=True,
        )

    async def close(self) -> None:
        """Close adapters if underlying providers expose close hooks."""
        for adapter in self.providers.values():
            close = getattr(adapter, "close", None)
            if close:
                await close()

    def set_emitter(self, emitter) -> None:
        self._emitter = emitter

    def select_model(
        self, tier: LegacyModelTier = None, specific_model: str = None
    ) -> Optional[str]:
        if specific_model and specific_model in self.model_registry:
            return specific_model

        tier = tier or LegacyModelTier.SMALL

        preferred = self.tier_models.get(tier, [])
        if preferred:
            return preferred[0]

        # Fallback to any available tier in order
        for fallback_tier in (
            LegacyModelTier.SMALL,
            LegacyModelTier.MEDIUM,
            LegacyModelTier.LARGE,
        ):
            candidates = self.tier_models.get(fallback_tier, [])
            if candidates:
                return candidates[0]

        return None

    async def generate_completion(
        self,
        messages: List[dict],
        tier: LegacyModelTier = None,
        specific_model: str = None,
        provider_override: Optional[str] = None,
        **kwargs,
    ) -> dict:
        params = dict(kwargs)

        session_id = params.get("session_id")
        workflow_id = (
            params.pop("workflow_id", None)
            or params.pop("workflowId", None)
            or params.pop("WORKFLOW_ID", None)
        )
        agent_id = (
            params.get("agent_id")
            or params.pop("agentId", None)
            or params.pop("AGENT_ID", None)
        )

        tier = tier or LegacyModelTier.SMALL
        try:
            core_tier = CoreModelTier(tier.value)
        except ValueError:
            core_tier = CoreModelTier.SMALL

        manager_kwargs: Dict[str, Any] = {}

        # Recognized request fields passed through to CompletionRequest
        passthrough_fields = {
            "temperature",
            "max_tokens",
            "top_p",
            "frequency_penalty",
            "presence_penalty",
            "stop",
            "response_format",
            "seed",
            "user",
            "function_call",
            "stream",
            "cache_key",
            "cache_ttl",
            "session_id",
            "task_id",
            "agent_id",
            "max_tokens_budget",
        }

        for field in list(params.keys()):
            if field in passthrough_fields and params[field] is not None:
                manager_kwargs[field] = params.pop(field)

        if "temperature" not in manager_kwargs or manager_kwargs["temperature"] is None:
            manager_kwargs["temperature"] = self.settings.temperature

        # Anthropic models don't support both temperature and top_p.
        # Only drop top_p when explicitly targeting Anthropic.
        if (
            "temperature" in manager_kwargs
            and "top_p" in manager_kwargs
            and isinstance(provider_override, str)
            and provider_override.lower() == "anthropic"
        ):
            manager_kwargs.pop("top_p", None)

        if agent_id and "agent_id" not in manager_kwargs:
            manager_kwargs["agent_id"] = agent_id

        tools = params.pop("tools", None)
        if tools:
            # Normalize OpenAI tools schema → legacy functions schema for providers
            # that expect functions. Each tool is {"type":"function","function":{...}}
            functions: List[Dict[str, Any]] = []
            try:
                for t in tools:
                    if isinstance(t, dict) and t.get("type") == "function" and isinstance(t.get("function"), dict):
                        functions.append(t["function"])
                    elif isinstance(t, dict):
                        # Already a function schema
                        functions.append(t)
            except Exception:
                # Best-effort; fall back to passing through
                functions = tools  # type: ignore[assignment]
            if functions:
                manager_kwargs["functions"] = functions

        if specific_model:
            manager_kwargs["model"] = specific_model
        if provider_override:
            manager_kwargs["provider_override"] = provider_override

        response: CompletionResponse = await self._manager.complete(
            messages=messages,
            model_tier=core_tier,
            **manager_kwargs,
        )

        result = self._serialize_completion(response)

        if session_id and result.get("usage"):
            total_tokens = result["usage"].get("total_tokens")
            if total_tokens is not None:
                self.session_tokens[session_id] = (
                    self.session_tokens.get(session_id, 0) + total_tokens
                )
                logger.info(
                    "Session %s token usage: %s",
                    session_id,
                    self.session_tokens[session_id],
                )

        if self.settings.enable_llm_events and self._emitter and workflow_id:
            self._emit_events(
                workflow_id=workflow_id,
                agent_id=agent_id,
                messages=messages,
                response=result,
            )

        return result

    def _serialize_completion(self, response: CompletionResponse) -> Dict[str, Any]:
        usage = self._serialize_usage(response.usage)

        # Normalize provider to a non-null string to avoid None → null propagation
        provider = response.provider if isinstance(response.provider, str) and response.provider else "unknown"

        result = {
            "provider": provider,
            "model": response.model,
            "output_text": response.content,
            "usage": usage,
            "finish_reason": response.finish_reason,
            "function_call": response.function_call,
            "request_id": response.request_id,
            "latency_ms": response.latency_ms,
            "cached": response.cached,
        }

        # Add effective_max_completion if available (for continuation trigger logic)
        if response.effective_max_completion is not None:
            result["effective_max_completion"] = response.effective_max_completion

        return result

    @staticmethod
    def _serialize_usage(usage: Optional[CoreTokenUsage]) -> Dict[str, Any]:
        if not usage:
            return {}
        return {
            "input_tokens": usage.input_tokens,
            "output_tokens": usage.output_tokens,
            "total_tokens": usage.total_tokens,
            "cost_usd": usage.estimated_cost,
        }

    def _extract_user_query(self, content: str) -> str:
        """Extract user query from message content, omitting tools.

        Industry best practice: Don't stream input prompts or tool definitions.
        Only extract the actual user query for display purposes.

        Args:
            content: Message content (plain string or JSON-serialized dict)

        Returns:
            User query text, or empty string to skip emission
        """
        import json

        # Try to parse as JSON (agent execution request)
        try:
            data = json.loads(content) if isinstance(content, str) and content.strip().startswith("{") else None
            if isinstance(data, dict):
                # Skip agent execution requests with tools entirely
                if "tools" in data:
                    return ""
                # Extract task field if present
                if "task" in data:
                    return str(data["task"])
                # Otherwise skip
                return ""
        except (json.JSONDecodeError, TypeError):
            pass

        # Plain string content - return as-is
        return content if isinstance(content, str) else ""

    def _emit_events(
        self,
        workflow_id: str,
        agent_id: Optional[str],
        messages: List[dict],
        response: Dict[str, Any],
    ) -> None:
        try:
            last_user = next(
                (
                    m.get("content", "")
                    for m in reversed(messages)
                    if m.get("role") == "user"
                ),
                "",
            )
        except Exception:
            last_user = ""

        if last_user:
            # Extract user query only, omit tools (industry best practice)
            prompt_text = self._extract_user_query(last_user)

            # Only emit LLM_PROMPT if we have valid query text
            if prompt_text:
                payload = {
                    "provider": response.get("provider"),
                    "model": response.get("model"),
                }
                try:
                    self._emitter.emit(
                        workflow_id,
                        "LLM_PROMPT",
                        agent_id=agent_id,
                        message=prompt_text[:500],  # Industry standard: 500 chars max
                        payload=payload,
                    )
                except Exception:
                    logger.debug("Failed to emit LLM_PROMPT", exc_info=True)

        output_text = response.get("output_text") or ""
        if not output_text:
            return

        if self.settings.enable_llm_partials:
            chunk = max(int(self.settings.partial_chunk_chars), 1)
            total = (len(output_text) + chunk - 1) // chunk
            for idx, start in enumerate(range(0, len(output_text), chunk)):
                try:
                    self._emitter.emit(
                        workflow_id,
                        "LLM_PARTIAL",
                        agent_id=agent_id,
                        message=output_text[start : start + chunk],
                        payload={"chunk_index": idx, "total_chunks": total},
                    )
                except Exception:
                    logger.debug("Failed to emit LLM_PARTIAL", exc_info=True)

        usage_payload = response.get("usage") or {}
        try:
            self._emitter.emit(
                workflow_id,
                "LLM_OUTPUT",
                agent_id=agent_id,
                message=output_text[:4000],
                payload={
                    "provider": response.get("provider"),
                    "model": response.get("model"),
                    "usage": usage_payload,
                },
            )
        except Exception:
            logger.debug("Failed to emit LLM_OUTPUT", exc_info=True)

    async def generate_embedding(self, text: str, model: str = None) -> List[float]:
        return await self._manager.generate_embedding(text, model)

    def is_configured(self) -> bool:
        return bool(self._manager.registry.providers)

    def get_provider(self, tier: str = "small") -> Any:
        if self.providers:
            return next(iter(self.providers.values()))
        return None

    def get_model_info(self, model_id: str) -> Optional[ModelInfo]:
        return self.model_registry.get(model_id)

    def list_available_models(self, tier: LegacyModelTier = None) -> List[ModelInfo]:
        if tier:
            ids = self.tier_models.get(tier, [])
            return [self.model_registry[mid] for mid in ids]
        return list(self.model_registry.values())
