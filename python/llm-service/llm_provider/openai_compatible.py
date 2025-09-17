"""
OpenAI-Compatible Provider Implementation
For providers that implement OpenAI's API (DeepSeek, Qwen, local models, etc.)
"""

import asyncio
import os
from typing import Dict, List, Any, Optional, AsyncIterator
from openai import AsyncOpenAI

from .openai_responses import (
    build_extra_body,
    determine_finish_reason,
    extract_output_text,
    normalize_response_tool_calls,
    prepare_responses_input,
    prepare_tool_choice,
    prepare_tools,
    select_primary_function_call,
)

from .base import (
    LLMProvider, ModelConfig, ModelTier, CompletionRequest, 
    CompletionResponse, TokenUsage, TokenCounter
)


class OpenAICompatibleProvider(LLMProvider):
    """Provider for OpenAI-compatible APIs"""
    
    def __init__(self, config: Dict[str, Any]):
        # Get API configuration
        self.api_key = config.get("api_key", "dummy")  # Some providers don't need keys
        self.base_url = config.get("base_url", "http://localhost:11434/v1")  # Default for Ollama
        
        # Initialize OpenAI client with custom base URL
        self.client = AsyncOpenAI(
            api_key=self.api_key,
            base_url=self.base_url
        )
        
        # Store model configurations from config
        self.model_configs = config.get("models", {})
        
        super().__init__(config)
    
    def _initialize_models(self):
        """Initialize models from configuration"""
        
        # Parse model configurations
        for model_id, model_config in self.model_configs.items():
            tier_str = model_config.get("tier", "medium")
            tier = ModelTier[tier_str.upper()] if isinstance(tier_str, str) else tier_str
            
            self.models[model_id] = ModelConfig(
                provider=self.config.get("name", "openai_compatible"),
                model_id=model_id,
                tier=tier,
                max_tokens=model_config.get("max_tokens", 4096),
                context_window=model_config.get("context_window", 8192),
                input_price_per_1k=model_config.get("input_price_per_1k", 0.001),
                output_price_per_1k=model_config.get("output_price_per_1k", 0.002),
                supports_functions=model_config.get("supports_functions", True),
                supports_streaming=model_config.get("supports_streaming", True),
                supports_vision=model_config.get("supports_vision", False),
                timeout=model_config.get("timeout", 60),
            )
        
        # If no models configured, add some defaults
        if not self.models:
            self._add_default_models()
    
    def _add_default_models(self):
        """Add default model configurations for common providers"""
        
        # Detect provider type from base URL
        if "deepseek" in self.base_url.lower():
            self._add_deepseek_models()
        elif "dashscope" in self.base_url.lower() or "qwen" in self.base_url.lower():
            self._add_qwen_models()
        elif "localhost" in self.base_url or "ollama" in self.base_url.lower():
            self._add_ollama_models()
        else:
            # Generic OpenAI-compatible model
            self.models["default"] = ModelConfig(
                provider="openai_compatible",
                model_id="default",
                tier=ModelTier.MEDIUM,
                max_tokens=4096,
                context_window=8192,
                input_price_per_1k=0.001,
                output_price_per_1k=0.002,
            )
    
    def _add_deepseek_models(self):
        """Add DeepSeek model configurations"""
        
        self.models["deepseek-chat"] = ModelConfig(
            provider="deepseek",
            model_id="deepseek-chat",
            tier=ModelTier.SMALL,
            max_tokens=4096,
            context_window=32768,
            input_price_per_1k=0.0001,
            output_price_per_1k=0.0002,
        )
        
        self.models["deepseek-coder"] = ModelConfig(
            provider="deepseek",
            model_id="deepseek-coder",
            tier=ModelTier.MEDIUM,
            max_tokens=4096,
            context_window=16384,
            input_price_per_1k=0.0001,
            output_price_per_1k=0.0002,
        )
        
        self.models["deepseek-v3"] = ModelConfig(
            provider="deepseek",
            model_id="deepseek-v3",
            tier=ModelTier.MEDIUM,
            max_tokens=8192,
            context_window=64000,
            input_price_per_1k=0.001,
            output_price_per_1k=0.002,
        )
    
    def _add_qwen_models(self):
        """Add Qwen model configurations"""
        
        self.models["qwen-turbo"] = ModelConfig(
            provider="qwen",
            model_id="qwen-turbo",
            tier=ModelTier.SMALL,
            max_tokens=4096,
            context_window=8192,
            input_price_per_1k=0.0003,
            output_price_per_1k=0.0006,
        )
        
        self.models["qwen-plus"] = ModelConfig(
            provider="qwen",
            model_id="qwen-plus",
            tier=ModelTier.MEDIUM,
            max_tokens=8192,
            context_window=32768,
            input_price_per_1k=0.0008,
            output_price_per_1k=0.002,
        )
        
        self.models["qwen-max"] = ModelConfig(
            provider="qwen",
            model_id="qwen-max",
            tier=ModelTier.LARGE,
            max_tokens=8192,
            context_window=32768,
            input_price_per_1k=0.002,
            output_price_per_1k=0.006,
        )
        
        self.models["qwq-32b"] = ModelConfig(
            provider="qwen",
            model_id="qwq-32b-preview",
            tier=ModelTier.LARGE,
            max_tokens=32768,
            context_window=32768,
            input_price_per_1k=0.001,
            output_price_per_1k=0.003,
        )
    
    def _add_ollama_models(self):
        """Add Ollama model configurations"""
        
        # Common Ollama models
        self.models["llama2"] = ModelConfig(
            provider="ollama",
            model_id="llama2",
            tier=ModelTier.SMALL,
            max_tokens=4096,
            context_window=4096,
            input_price_per_1k=0.0,  # Local models have no cost
            output_price_per_1k=0.0,
        )
        
        self.models["mistral"] = ModelConfig(
            provider="ollama",
            model_id="mistral",
            tier=ModelTier.SMALL,
            max_tokens=8192,
            context_window=8192,
            input_price_per_1k=0.0,
            output_price_per_1k=0.0,
        )
        
        self.models["codellama"] = ModelConfig(
            provider="ollama",
            model_id="codellama",
            tier=ModelTier.MEDIUM,
            max_tokens=4096,
            context_window=4096,
            input_price_per_1k=0.0,
            output_price_per_1k=0.0,
        )
    
    def count_tokens(self, messages: List[Dict[str, Any]], model: str) -> int:
        """
        Count tokens for the model.
        Uses generic estimation since tokenizers vary by provider.
        """
        return TokenCounter.count_messages_tokens(messages, model)
    
    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        """Generate a completion using the OpenAI-compatible API"""
        
        # Select model based on tier
        model_config = self.select_model_for_tier(request.model_tier, request.max_tokens)
        model = model_config.model_id
        
        # Count input tokens (estimation)
        input_tokens = self.count_tokens(request.messages, model)
        
        # Prepare API request
        api_request: Dict[str, Any] = {
            "model": model,
            "input": prepare_responses_input(request.messages),
            "temperature": request.temperature,
            "top_p": request.top_p,
        }

        if request.max_tokens:
            api_request["max_output_tokens"] = request.max_tokens

        if request.user:
            api_request["user"] = request.user

        if request.functions and model_config.supports_functions:
            tools = prepare_tools(request.functions)
            if tools:
                api_request["tools"] = tools
                tool_choice = prepare_tool_choice(request.function_call)
                if tool_choice:
                    api_request["tool_choice"] = tool_choice

        if request.response_format:
            api_request["text"] = {"format": request.response_format}

        extra_body = build_extra_body(
            stop=request.stop,
            frequency_penalty=request.frequency_penalty,
            presence_penalty=request.presence_penalty,
            seed=request.seed,
        )
        if extra_body:
            api_request["extra_body"] = extra_body
        
        # Make API call
        import time
        start_time = time.time()
        
        try:
            response = await self.client.responses.create(**api_request)
        except Exception as e:
            raise Exception(f"OpenAI-compatible API error ({self.base_url}): {e}")

        latency_ms = int((time.time() - start_time) * 1000)

        content = extract_output_text(response)
        usage = getattr(response, "usage", None)

        if usage:
            output_tokens = getattr(usage, "output_tokens", 0)
            total_tokens = getattr(usage, "total_tokens", output_tokens)
        else:
            output_tokens = self.count_tokens([{"role": "assistant", "content": content}], model)
            total_tokens = input_tokens + output_tokens

        tool_calls = normalize_response_tool_calls(response)
        function_call = select_primary_function_call(tool_calls)
        finish_reason = determine_finish_reason(response)

        # Calculate cost
        cost = self.estimate_cost(input_tokens, output_tokens, model)

        # Build response
        return CompletionResponse(
            content=content or "",
            model=model,
            provider=self.config.get("name", "openai_compatible"),
            usage=TokenUsage(
                input_tokens=input_tokens,
                output_tokens=output_tokens,
                total_tokens=total_tokens,
                estimated_cost=cost,
            ),
            finish_reason=finish_reason,
            function_call=function_call,
            request_id=getattr(response, 'id', None),
            latency_ms=latency_ms,
        )
    
    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator[str]:
        """Stream a completion using the OpenAI-compatible API"""
        
        # Select model based on tier
        model_config = self.select_model_for_tier(request.model_tier, request.max_tokens)
        model = model_config.model_id
        
        # Prepare API request
        api_request: Dict[str, Any] = {
            "model": model,
            "input": prepare_responses_input(request.messages),
            "temperature": request.temperature,
        }

        if request.max_tokens:
            api_request["max_output_tokens"] = request.max_tokens

        if request.functions and model_config.supports_functions:
            tools = prepare_tools(request.functions)
            if tools:
                api_request["tools"] = tools
                tool_choice = prepare_tool_choice(request.function_call)
                if tool_choice:
                    api_request["tool_choice"] = tool_choice

        if request.response_format:
            api_request["text"] = {"format": request.response_format}

        extra_body = build_extra_body(
            stop=request.stop,
            frequency_penalty=request.frequency_penalty,
            presence_penalty=request.presence_penalty,
            seed=request.seed,
        )
        if extra_body:
            api_request["extra_body"] = extra_body

        # Make streaming API call
        try:
            async with self.client.responses.stream(**api_request) as stream:
                async for event in stream:
                    event_type = getattr(event, "type", None)
                    if event_type == "response.output_text.delta":
                        yield getattr(event, "delta", "")
                    elif event_type == "error":
                        message = getattr(event, "message", "streaming error")
                        raise Exception(f"OpenAI-compatible streaming error ({self.base_url}): {message}")

        except Exception as e:
            raise Exception(f"OpenAI-compatible streaming error ({self.base_url}): {e}")