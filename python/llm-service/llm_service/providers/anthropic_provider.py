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
    
    def __init__(self, api_key: str, base_url: str = None):
        self.api_key = api_key
        self.base_url = base_url
        self.client = None
        
    async def initialize(self):
        """Initialize Anthropic client"""
        client_kwargs = {"api_key": self.api_key}
        if self.base_url:
            client_kwargs["base_url"] = self.base_url
        self.client = AsyncAnthropic(**client_kwargs)
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
        """Generate completion using Anthropic API.

        Returns a dict shaped like the Responses API output
        (with keys like model, output_text, output, usage).
        """
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
            
            # Add additional parameters (drop unsupported response_format)
            if "response_format" in kwargs:
                kwargs.pop("response_format", None)
            request_params.update(kwargs)
            
            # Call Anthropic API
            response = await self.client.messages.create(**request_params)

            # Extract assistant text
            content_text = response.content[0].text if response.content else ""

            # Build Responses-like output array
            output_items: List[Dict[str, Any]] = [
                {
                    "type": "message",
                    "role": "assistant",
                    "content": [{"type": "output_text", "text": content_text}],
                }
            ]

            # Normalize tool calls if present (Claude 3.5 tool_use blocks) into Responses-style tool_call items
            try:
                blocks = getattr(response, "content", []) or []
                for b in blocks:
                    b_type = getattr(b, "type", None) or (b.get("type") if isinstance(b, dict) else None)
                    if b_type == "tool_use":
                        name = getattr(b, "name", None) or (b.get("name") if isinstance(b, dict) else None)
                        args = getattr(b, "input", None) or (b.get("input") if isinstance(b, dict) else {})
                        output_items.append({
                            "type": "tool_call",
                            "name": name,
                            "arguments": args,
                        })
            except Exception:
                pass

            # Usage normalization
            usage_obj = getattr(response, "usage", None)
            in_tok = getattr(usage_obj, "input_tokens", 0) if usage_obj else 0
            out_tok = getattr(usage_obj, "output_tokens", 0) if usage_obj else 0
            total_tok = in_tok + out_tok
            cost = self.calculate_cost(in_tok, out_tok, model)

            from . import ProviderType
            result: Dict[str, Any] = {
                "id": getattr(response, "id", None),
                "model": model,
                "output_text": content_text,
                "output": output_items,
                "usage": {
                    "input_tokens": in_tok,
                    "output_tokens": out_tok,
                    "total_tokens": total_tok,
                    "cost_usd": cost,
                },
                "stop_reason": getattr(response, "stop_reason", None),
                "provider": ProviderType.ANTHROPIC.value,
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
