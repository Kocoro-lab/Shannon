from typing import List, Dict, Any, Optional
import logging
from openai import AsyncOpenAI
from tenacity import retry, stop_after_attempt, wait_exponential

from .base import LLMProvider, ModelInfo, ModelTier, TokenUsage

logger = logging.getLogger(__name__)

class OpenAIProvider(LLMProvider):
    """OpenAI LLM provider (modern models only)"""
    
    # Strict, modern-only registry (seed, can be augmented dynamically)
    MODELS = {
        # 4o family (2024/2025)
        "gpt-4o-mini": ModelInfo(
            id="gpt-4o-mini",
            name="GPT-4o Mini",
            provider=None,
            tier=ModelTier.SMALL,
            context_window=128000,
            cost_per_1k_prompt_tokens=0.0,  # TODO: update with actual pricing
            cost_per_1k_completion_tokens=0.0,
            supports_tools=True,
            supports_streaming=True,
            available=True,
        ),
        "gpt-4o": ModelInfo(
            id="gpt-4o",
            name="GPT-4o",
            provider=None,
            tier=ModelTier.MEDIUM,  # Changed from LARGE to MEDIUM for better tier distribution
            context_window=128000,
            cost_per_1k_prompt_tokens=0.005,  # Updated with actual pricing
            cost_per_1k_completion_tokens=0.015,
            supports_tools=True,
            supports_streaming=True,
            available=True,
        ),
    }
    
    def __init__(self, api_key: str):
        self.api_key = api_key
        self.client = None
        # Instance-level model registry, initialized from class seed
        self._models: Dict[str, ModelInfo] = {k: v for k, v in self.MODELS.items()}
        
    async def initialize(self):
        """Initialize OpenAI client"""
        self.client = AsyncOpenAI(api_key=self.api_key)
        # Set provider reference in models
        from . import ProviderType
        for model in self._models.values():
            model.provider = ProviderType.OPENAI
        # Attempt dynamic discovery to capture newest models
        await self._maybe_discover_models()
    
    async def close(self):
        """Close OpenAI client"""
        if self.client:
            await self.client.close()
    
    @retry(stop=stop_after_attempt(3), wait=wait_exponential(multiplier=1, min=2, max=10))
    async def generate_completion(
        self,
        messages: List[dict],
        model: str = "gpt-4o-mini",
        temperature: float = 0.7,
        max_tokens: int = 2000,
        tools: List[dict] = None,
        **kwargs
    ) -> dict:
        """Generate completion using OpenAI API"""
        try:
            # Use Chat Completions API for all models
            # Note: Responses API doesn't exist in OpenAI SDK, removed to avoid warnings
            return await self._chat_completions_call(messages, model, temperature, max_tokens, tools, **kwargs)
            
        except Exception as e:
            logger.error(f"OpenAI completion error: {e}")
            raise

    # Removed: _responses_api_call method - OpenAI SDK doesn't have responses API
    # This was likely confused with a beta or internal API that doesn't exist
    # All calls now use the standard Chat Completions API
    
    async def _responses_api_call_removed(
        self,
        messages: List[dict],
        model: str,
        temperature: float,
        max_tokens: int,
        tools: Optional[List[dict]] = None,
        **kwargs,
    ) -> dict:
        """Call the OpenAI Responses API and normalize output."""
        # Convert chat messages to Responses input format
        inputs = []
        for msg in messages:
            role = msg.get("role", "user")
            content = msg.get("content", "")
            inputs.append({
                "role": role,
                "content": [{"type": "text", "text": content}],
            })

        request_params = {
            "model": model,
            "input": inputs,
            "temperature": temperature,
            "max_output_tokens": max_tokens,
        }
        if tools:
            request_params["tools"] = tools
            request_params["tool_choice"] = "auto"
        request_params.update(kwargs)

        response = await self.client.responses.create(**request_params)

        # Extract text
        completion_text = getattr(response, "output_text", None)
        if not completion_text:
            # Fallback: walk output blocks and concatenate message text
            completion_text = ""
            output = getattr(response, "output", []) or []
            for item in output:
                if isinstance(item, dict):
                    if item.get("type") == "message":
                        for block in item.get("content", []) or []:
                            if block.get("type") == "text":
                                completion_text += block.get("text", "")

        # Usage normalization
        usage = getattr(response, "usage", None)
        prompt_tokens = getattr(usage, "input_tokens", 0) if usage else 0
        completion_tokens = getattr(usage, "output_tokens", 0) if usage else 0
        total_tokens = prompt_tokens + completion_tokens
        cost = self.calculate_cost(prompt_tokens, completion_tokens, model)

        # Tool calls normalization (Responses may return tool call outputs)
        tool_calls = self._normalize_tool_calls_from_responses(response)

        return {
            "completion": completion_text or "",
            "tool_calls": tool_calls,
            "finish_reason": getattr(response, "finish_reason", None) or "stop",
            "usage": TokenUsage(
                prompt_tokens=prompt_tokens,
                completion_tokens=completion_tokens,
                total_tokens=total_tokens,
                cost_usd=cost,
                model=model,
            ).__dict__,
            "cache_hit": False,
        }

    async def _chat_completions_call(
        self,
        messages: List[dict],
        model: str,
        temperature: float,
        max_tokens: int,
        tools: Optional[List[dict]] = None,
        **kwargs,
    ) -> dict:
        """Call the Chat Completions API and normalize output."""
        request_params = {
            "model": model,
            "messages": messages,
            "temperature": temperature,
            "max_tokens": max_tokens,
        }
        if tools:
            request_params["tools"] = tools
            request_params["tool_choice"] = "auto"
        request_params.update(kwargs)

        response = await self.client.chat.completions.create(**request_params)
        choice = response.choices[0]
        usage = response.usage
        prompt_tokens = getattr(usage, "prompt_tokens", 0)
        completion_tokens = getattr(usage, "completion_tokens", 0)
        total_tokens = getattr(usage, "total_tokens", prompt_tokens + completion_tokens)
        cost = self.calculate_cost(prompt_tokens, completion_tokens, model)

        # Normalize tool calls from ChatCompletions
        tool_calls = self._normalize_tool_calls_from_chat(choice)

        return {
            "completion": getattr(choice.message, "content", ""),
            "tool_calls": tool_calls,
            "finish_reason": getattr(choice, "finish_reason", None),
            "usage": TokenUsage(
                prompt_tokens=prompt_tokens,
                completion_tokens=completion_tokens,
                total_tokens=total_tokens,
                cost_usd=cost,
                model=model,
            ).__dict__,
            "cache_hit": False,
        }

    def _normalize_tool_calls_from_chat(self, choice: Any) -> Optional[List[Dict[str, Any]]]:
        """Normalize OpenAI ChatCompletions tool calls to a consistent schema."""
        calls = getattr(getattr(choice, "message", object()), "tool_calls", None)
        if not calls:
            return None
        normalized = []
        for c in calls:
            fn = getattr(c, "function", None)
            if not fn:
                continue
            name = getattr(fn, "name", None)
            args = getattr(fn, "arguments", "{}")
            try:
                import json as _json
                parsed = _json.loads(args) if isinstance(args, str) else args
            except Exception:
                parsed = {"_raw": args}
            normalized.append({
                "type": "function",
                "name": name,
                "arguments": parsed,
            })
        return normalized or None

    async def _maybe_discover_models(self) -> None:
        """Best-effort dynamic model discovery via OpenAI models.list()."""
        try:
            listing = await self.client.models.list()
            items = getattr(listing, "data", []) or []
            for m in items:
                mid = getattr(m, "id", None)
                if not isinstance(mid, str):
                    continue
                # Filter to current families we care about
                if not (mid.startswith("gpt-4o") or mid.startswith("o") or mid.startswith("gpt-4.1")):
                    continue
                # Skip if already in seed models (preserve configured settings)
                if mid in self._models:
                    logger.debug(f"Skipping {mid} - already in seed models")
                    continue
                # Tier heuristic
                tier = ModelTier.SMALL if ("mini" in mid or "small" in mid) else ModelTier.LARGE
                from . import ProviderType
                info = ModelInfo(
                    id=mid,
                    name=mid,
                    provider=ProviderType.OPENAI,  # Set provider correctly for discovered models
                    tier=tier,
                    context_window=128000,
                    cost_per_1k_prompt_tokens=0.0,
                    cost_per_1k_completion_tokens=0.0,
                    supports_tools=True,
                    supports_streaming=True,
                    available=True,
                )
                self._models[mid] = info
        except Exception as e:
            logger.info(f"OpenAI dynamic model discovery skipped: {e}")

    def list_models(self) -> List[ModelInfo]:
        """Return cached model list (seed + dynamic)."""
        return list(self._models.values())

    def _normalize_tool_calls_from_responses(self, response: Any) -> Optional[List[Dict[str, Any]]]:
        """Best-effort normalization for Responses API outputs with tool calls."""
        output = getattr(response, "output", None)
        if not output:
            return None
        normalized: List[Dict[str, Any]] = []
        try:
            for item in output:
                # Some SDKs emit objects; others dicts. Handle dicts here.
                if isinstance(item, dict) and item.get("type") == "tool_call":
                    name = item.get("name") or item.get("tool_name")
                    args = item.get("arguments") or item.get("input") or {}
                    normalized.append({
                        "type": "function",
                        "name": name,
                        "arguments": args,
                    })
                elif isinstance(item, dict) and item.get("type") == "message":
                    # Look for nested tool calls in content blocks
                    for block in item.get("content", []) or []:
                        if isinstance(block, dict) and block.get("type") in ("tool_call", "tool_calls"):
                            calls = block.get("calls") or [block]
                            for c in calls:
                                name = c.get("name")
                                args = c.get("arguments") or {}
                                normalized.append({
                                    "type": "function",
                                    "name": name,
                                    "arguments": args,
                                })
        except Exception:
            return normalized or None
        return normalized or None
    
    @retry(stop=stop_after_attempt(3), wait=wait_exponential(multiplier=1, min=2, max=10))
    async def generate_embedding(self, text: str, model: str = "text-embedding-3-small") -> List[float]:
        """Generate text embedding"""
        try:
            response = await self.client.embeddings.create(
                input=text,
                model=model
            )
            return response.data[0].embedding
        except Exception as e:
            logger.error(f"OpenAI embedding error: {e}")
            raise
    
    def calculate_cost(self, prompt_tokens: int, completion_tokens: int, model: str) -> float:
        """Calculate cost for OpenAI usage"""
        model_info = self._models.get(model)
        if not model_info:
            return 0.0
        
        prompt_cost = (prompt_tokens / 1000) * model_info.cost_per_1k_prompt_tokens
        completion_cost = (completion_tokens / 1000) * model_info.cost_per_1k_completion_tokens
        
        return round(prompt_cost + completion_cost, 6)
