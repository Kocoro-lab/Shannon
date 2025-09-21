from typing import Dict, List, Optional, Any
import logging
from enum import Enum

from .openai_provider import OpenAIProvider
from .anthropic_provider import AnthropicProvider
from .base import LLMProvider, ModelInfo, ModelTier

logger = logging.getLogger(__name__)

class ProviderType(Enum):
    OPENAI = "openai"
    ANTHROPIC = "anthropic"
    GOOGLE = "google"
    DEEPSEEK = "deepseek"
    QWEN = "qwen"
    BEDROCK = "bedrock"
    OLLAMA = "ollama"

class ProviderManager:
    """Manages multiple LLM providers with intelligent routing"""
    
    def __init__(self, settings):
        self.settings = settings
        self.providers: Dict[ProviderType, LLMProvider] = {}
        self.model_registry: Dict[str, ModelInfo] = {}
        self.tier_models: Dict[ModelTier, List[str]] = {
            ModelTier.SMALL: [],
            ModelTier.MEDIUM: [],
            ModelTier.LARGE: []
        }
        # Token budget tracking per session
        self.session_tokens: Dict[str, int] = {}
        self.max_tokens_per_session = 100000  # Default limit
        self._emitter = None
        
    async def initialize(self):
        """Initialize available providers"""
        # Initialize OpenAI
        if self.settings.openai_api_key:
            provider = OpenAIProvider(self.settings.openai_api_key)
            await provider.initialize()
            self.providers[ProviderType.OPENAI] = provider
            self._register_models(provider)
            logger.info("OpenAI provider initialized")
        
        # Initialize Anthropic
        if self.settings.anthropic_api_key:
            provider = AnthropicProvider(self.settings.anthropic_api_key)
            await provider.initialize()
            self.providers[ProviderType.ANTHROPIC] = provider
            self._register_models(provider)
            logger.info("Anthropic provider initialized")
        
        # Additional providers would be initialized here
        
        if not self.providers:
            logger.warning("No LLM providers configured - using mock provider")
            # Could add a mock provider for testing
    
    def _register_models(self, provider: LLMProvider):
        """Register models from a provider"""
        models = provider.list_models()
        for model in models:
            self.model_registry[model.id] = model
            self.tier_models[model.tier].append(model.id)
    
    async def close(self):
        """Close all providers"""
        for provider in self.providers.values():
            await provider.close()

    def set_emitter(self, emitter):
        """Inject shared event emitter (optional)."""
        self._emitter = emitter
    
    def select_model(self, tier: ModelTier = None, specific_model: str = None) -> Optional[str]:
        """Select a model based on tier or specific request"""
        if specific_model:
            if specific_model in self.model_registry:
                return specific_model
            # If specific model not found, log warning and fall back to tier
            logger.warning(f"Requested model '{specific_model}' not available, falling back to tier selection")
        
        tier = tier or ModelTier.SMALL
        available_models = self.tier_models.get(tier, [])
        
        if not available_models:
            # Fall back to next tier
            if tier == ModelTier.SMALL and self.tier_models[ModelTier.MEDIUM]:
                return self.tier_models[ModelTier.MEDIUM][0]
            elif tier == ModelTier.MEDIUM and self.tier_models[ModelTier.LARGE]:
                return self.tier_models[ModelTier.LARGE][0]
            # Last resort: any available model
            for models in self.tier_models.values():
                if models:
                    return models[0]
        
        return available_models[0] if available_models else None
    
    async def generate_completion(
        self,
        messages: List[dict],
        tier: ModelTier = None,
        specific_model: str = None,
        **kwargs
    ) -> dict:
        """Generate completion using appropriate provider and model with unified event emission."""
        # Check token budget if session_id provided
        session_id = kwargs.get('session_id')
        if session_id:
            current_usage = self.session_tokens.get(session_id, 0)
            max_tokens = kwargs.get('max_tokens_budget', self.max_tokens_per_session)
            
            if current_usage >= max_tokens:
                raise ValueError(
                    f"Session {session_id} exceeded token budget: "
                    f"{current_usage}/{max_tokens} tokens used"
                )
        
        model_id = self.select_model(tier, specific_model)
        if not model_id:
            raise ValueError("No models available")
        
        model_info = self.model_registry[model_id]
        provider = self.providers.get(model_info.provider)
        
        if not provider:
            raise ValueError(f"Provider {model_info.provider} not available")
        
        # Unified event emission (extract + remove tracking keys so they don't reach vendor SDKs)
        wf_id = kwargs.pop('workflow_id', None) or kwargs.pop('workflowId', None) or kwargs.pop('WORKFLOW_ID', None)
        agent_id = kwargs.pop('agent_id', None) or kwargs.pop('agentId', None) or kwargs.pop('AGENT_ID', None)

        if self.settings.enable_llm_events and self._emitter and wf_id:
            # Emit sanitized prompt (use last user message)
            try:
                last_user = next((m.get('content','') for m in reversed(messages) if m.get('role')=='user'), '')
                payload = {
                    "provider": provider.__class__.__name__.replace('Provider','').lower(),
                    "model": model_id,
                }
                self._emitter.emit(wf_id, 'LLM_PROMPT', agent_id=agent_id, message=(last_user[:2000] if last_user else ''), payload=payload)
            except Exception:
                pass

        result = await provider.generate_completion(
            messages=messages,
            model=model_id,
            **kwargs
        )
        
        # Track token usage for session (Responses usage has input_tokens/output_tokens)
        if session_id and 'usage' in result:
            usage = result['usage'] or {}
            total_tokens = usage.get('total_tokens')
            if total_tokens is None:
                it = usage.get('input_tokens', 0)
                ot = usage.get('output_tokens', 0)
                total_tokens = it + ot
            self.session_tokens[session_id] = self.session_tokens.get(session_id, 0) + total_tokens
            logger.info(f"Session {session_id} token usage: {self.session_tokens[session_id]}")
        
        # Ensure provider tag present for observability; model should already be included by provider
        result["provider"] = result.get("provider", model_info.provider.value)
        
        # Emit partials + output (normalize for non-streaming)
        if self.settings.enable_llm_events and self._emitter and wf_id:
            try:
                text = result.get('output_text') or ''
                if text:
                    if self.settings.enable_llm_partials:
                        n = max(int(self.settings.partial_chunk_chars), 1)
                        total = (len(text) + n - 1) // n
                        for ix, i in enumerate(range(0, len(text), n)):
                            self._emitter.emit(wf_id, 'LLM_PARTIAL', agent_id=agent_id, message=text[i:i+n], payload={"chunk_index": ix, "total_chunks": total})
                    usage = result.get('usage') or {}
                    payload = {
                        "provider": provider.__class__.__name__.replace('Provider','').lower(),
                        "model": result.get('model', model_id),
                        "usage": usage,
                    }
                    self._emitter.emit(wf_id, 'LLM_OUTPUT', agent_id=agent_id, message=text[:4000], payload=payload)
            except Exception:
                pass

        return result
    
    async def generate_embedding(self, text: str, model: str = None) -> List[float]:
        """Generate text embedding"""
        # Default to OpenAI for embeddings if available
        if ProviderType.OPENAI in self.providers:
            provider = self.providers[ProviderType.OPENAI]
            return await provider.generate_embedding(text, model)
        
        # Fall back to first available provider with embedding support
        for provider in self.providers.values():
            if hasattr(provider, 'generate_embedding'):
                return await provider.generate_embedding(text, model)
        
        raise ValueError("No embedding providers available")
    
    def is_configured(self) -> bool:
        """Check if any providers are configured"""
        return len(self.providers) > 0
    
    def get_provider(self, tier: str = "small") -> Any:
        """Get a provider for the specified tier"""
        # This is a simplified version - in production would select based on tier
        if self.providers:
            return list(self.providers.values())[0]
        return None
    
    def get_model_info(self, model_id: str) -> Optional[ModelInfo]:
        """Get information about a specific model"""
        return self.model_registry.get(model_id)
    
    def list_available_models(self, tier: ModelTier = None) -> List[ModelInfo]:
        """List available models, optionally filtered by tier"""
        if tier:
            model_ids = self.tier_models.get(tier, [])
            return [self.model_registry[id] for id in model_ids]
        return list(self.model_registry.values())
