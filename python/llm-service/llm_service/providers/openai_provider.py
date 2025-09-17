from typing import List, Dict, Any, Optional
import json
import logging
from openai import AsyncOpenAI
from tenacity import retry, stop_after_attempt, wait_exponential

from llm_provider.openai_responses import (
    build_extra_body,
    determine_finish_reason,
    extract_output_text,
    normalize_response_tool_calls,
    prepare_responses_input,
    prepare_tool_choice,
    prepare_tools,
    select_primary_function_call,
)

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
            return await self._responses_call(messages, model, temperature, max_tokens, tools, **kwargs)

        except Exception as e:
            logger.error(f"OpenAI completion error: {e}")
            raise

    async def _responses_call(
        self,
        messages: List[dict],
        model: str,
        temperature: float,
        max_tokens: int,
        tools: Optional[List[dict]] = None,
        **kwargs,
    ) -> dict:
        """Call the OpenAI Responses API and normalize output."""

        request_params: Dict[str, Any] = {
            "model": model,
            "input": prepare_responses_input(messages),
            "temperature": temperature,
        }

        top_p = kwargs.get("top_p")
        if top_p is not None:
            request_params["top_p"] = top_p

        if max_tokens:
            request_params["max_output_tokens"] = max_tokens

        if tools:
            prepared_tools = prepare_tools(tools)
            if prepared_tools:
                request_params["tools"] = prepared_tools
                tool_choice = prepare_tool_choice(kwargs.get("tool_choice") or kwargs.get("function_call"))
                if tool_choice:
                    request_params["tool_choice"] = tool_choice

        response_format = kwargs.get("response_format")
        if response_format:
            request_params["text"] = {"format": response_format}

        for field in ("instructions", "parallel_tool_calls", "previous_response_id", "reasoning", "store", "metadata"):
            value = kwargs.get(field)
            if value is not None:
                request_params[field] = value

        user = kwargs.get("user")
        if user:
            request_params["user"] = user

        extra_body = build_extra_body(
            stop=kwargs.get("stop"),
            frequency_penalty=kwargs.get("frequency_penalty"),
            presence_penalty=kwargs.get("presence_penalty"),
            seed=kwargs.get("seed"),
        )
        if extra_body:
            request_params["extra_body"] = extra_body

        response = await self.client.responses.create(**request_params)

        completion_text = extract_output_text(response) or ""

        usage = getattr(response, "usage", None)
        prompt_tokens = getattr(usage, "input_tokens", 0) if usage else 0
        completion_tokens = getattr(usage, "output_tokens", 0) if usage else 0
        total_tokens = getattr(usage, "total_tokens", prompt_tokens + completion_tokens)
        cost = self.calculate_cost(prompt_tokens, completion_tokens, model)

        tool_calls = self._normalize_tool_calls_from_responses(response)
        finish_reason = determine_finish_reason(response)

        return {
            "completion": completion_text,
            "tool_calls": tool_calls,
            "finish_reason": finish_reason,
            "usage": TokenUsage(
                prompt_tokens=prompt_tokens,
                completion_tokens=completion_tokens,
                total_tokens=total_tokens,
                cost_usd=cost,
                model=model,
            ).__dict__,
            "cache_hit": False,
        }

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
        calls = normalize_response_tool_calls(response)
        if not calls:
            return None

        normalized: List[Dict[str, Any]] = []
        for call in calls:
            name = call.get("name")
            if not name:
                continue
            arguments = call.get("arguments")
            if isinstance(arguments, str):
                try:
                    parsed = json.loads(arguments)
                except Exception:
                    parsed = {"_raw": arguments}
            else:
                parsed = arguments
            normalized.append(
                {
                    "type": "function",
                    "name": name,
                    "arguments": parsed,
                }
            )
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
