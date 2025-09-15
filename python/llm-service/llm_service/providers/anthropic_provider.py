from typing import List, Dict, Any
import logging
from anthropic import AsyncAnthropic
from tenacity import retry, stop_after_attempt, wait_exponential

from .base import LLMProvider, ModelInfo, ModelTier, TokenUsage

logger = logging.getLogger(__name__)

class AnthropicProvider(LLMProvider):
    """Anthropic (Claude) LLM provider — modern models only"""
    
    MODELS = {
        # 3.5 family (2024/2025)
        "claude-3-5-haiku-latest": ModelInfo(
            id="claude-3-5-haiku-latest",
            name="Claude 3.5 Haiku",
            provider=None,
            tier=ModelTier.SMALL,
            context_window=200000,
            cost_per_1k_prompt_tokens=0.0,  # TODO: update with actual pricing
            cost_per_1k_completion_tokens=0.0,
            supports_tools=True,
            supports_streaming=True,
            available=True,
        ),
        "claude-3-5-sonnet-latest": ModelInfo(
            id="claude-3-5-sonnet-latest",
            name="Claude 3.5 Sonnet",
            provider=None,
            tier=ModelTier.LARGE,
            context_window=200000,
            cost_per_1k_prompt_tokens=0.0,  # TODO: update with actual pricing
            cost_per_1k_completion_tokens=0.0,
            supports_tools=True,
            supports_streaming=True,
            available=True,
        ),
    }
    
    def __init__(self, api_key: str):
        self.api_key = api_key
        self.client = None
        
    async def initialize(self):
        """Initialize Anthropic client"""
        self.client = AsyncAnthropic(api_key=self.api_key)
        # Set provider reference in models
        from . import ProviderType
        for model in self.MODELS.values():
            model.provider = ProviderType.ANTHROPIC
    
    async def close(self):
        """Close Anthropic client"""
        # Anthropic client doesn't need explicit closing
        pass
    
    @retry(stop=stop_after_attempt(3), wait=wait_exponential(multiplier=1, min=2, max=10))
    async def generate_completion(
        self,
        messages: List[dict],
        model: str = "claude-3-5-haiku-latest",
        temperature: float = 0.7,
        max_tokens: int = 2000,
        tools: List[dict] = None,
        **kwargs
    ) -> dict:
        """Generate completion using Anthropic API"""
        try:
            # Convert messages to Anthropic format
            system_message = None
            user_messages = []
            
            for msg in messages:
                if msg["role"] == "system":
                    system_message = msg["content"]
                else:
                    user_messages.append({
                        "role": msg["role"],
                        "content": msg["content"]
                    })
            
            # Build request
            request_params = {
                "model": model,
                "messages": user_messages,
                "max_tokens": max_tokens,
                "temperature": temperature,
            }
            
            if system_message:
                request_params["system"] = system_message
            
            if tools:
                request_params["tools"] = self._convert_tools(tools)
            
            # Add additional parameters
            request_params.update(kwargs)
            
            # Call Anthropic API
            response = await self.client.messages.create(**request_params)
            
            # Extract response
            content = response.content[0].text if response.content else ""

            # Normalize tool calls if present (Claude 3.5 tool_use blocks)
            tool_calls = None
            try:
                blocks = getattr(response, "content", []) or []
                calls = []
                for b in blocks:
                    # SDK block may expose .type/.name/.input attributes or dict-like
                    b_type = getattr(b, "type", None) or (b.get("type") if isinstance(b, dict) else None)
                    if b_type == "tool_use":
                        name = getattr(b, "name", None) or (b.get("name") if isinstance(b, dict) else None)
                        args = getattr(b, "input", None) or (b.get("input") if isinstance(b, dict) else {})
                        calls.append({"type": "function", "name": name, "arguments": args})
                if calls:
                    tool_calls = calls
            except Exception:
                tool_calls = None
            
            # Calculate cost (Anthropic uses input/output tokens)
            cost = self.calculate_cost(
                response.usage.input_tokens,
                response.usage.output_tokens,
                model
            )
            
            result = {
                "completion": content,
                "tool_calls": tool_calls,
                "finish_reason": response.stop_reason,
                "usage": TokenUsage(
                    prompt_tokens=response.usage.input_tokens,
                    completion_tokens=response.usage.output_tokens,
                    total_tokens=response.usage.input_tokens + response.usage.output_tokens,
                    cost_usd=cost,
                    model=model
                ).__dict__,
                "cache_hit": False
            }
            
            return result
            
        except Exception as e:
            logger.error(f"Anthropic completion error: {e}")
            raise
    
    async def generate_embedding(self, text: str, model: str = None) -> List[float]:
        """Anthropic doesn't provide embedding models"""
        raise NotImplementedError("Anthropic does not provide embedding models")
    
    def list_models(self) -> List[ModelInfo]:
        """List available Anthropic models"""
        return list(self.MODELS.values())
    
    def calculate_cost(self, prompt_tokens: int, completion_tokens: int, model: str) -> float:
        """Calculate cost for Anthropic usage"""
        model_info = self.MODELS.get(model)
        if not model_info:
            return 0.0
        
        prompt_cost = (prompt_tokens / 1000) * model_info.cost_per_1k_prompt_tokens
        completion_cost = (completion_tokens / 1000) * model_info.cost_per_1k_completion_tokens
        
        return round(prompt_cost + completion_cost, 6)
    
    def _convert_tools(self, openai_tools: List[dict]) -> List[dict]:
        """Convert OpenAI tool format to Anthropic format"""
        # Simplified conversion - would need full implementation
        anthropic_tools = []
        for tool in openai_tools:
            anthropic_tools.append({
                "name": tool["function"]["name"],
                "description": tool["function"]["description"],
                "input_schema": tool["function"]["parameters"]
            })
        return anthropic_tools
