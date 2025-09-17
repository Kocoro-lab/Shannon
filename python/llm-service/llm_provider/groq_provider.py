"""
Groq Provider Implementation
High-performance LLM inference using Groq's LPU (Language Processing Unit)
"""

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


class GroqProvider(LLMProvider):
    """Provider for Groq's high-performance LLM inference"""
    
    def __init__(self, config: Dict[str, Any]):
        # Get API key from config or environment
        self.api_key = config.get("api_key") or os.getenv("GROQ_API_KEY")
        if not self.api_key:
            raise ValueError("Groq API key not provided")
        
        # Validate API key format (Groq keys typically start with 'gsk_' and are 56+ chars)
        if len(self.api_key) < 40:
            raise ValueError("Invalid Groq API key format - too short")
        if not self.api_key.startswith(('gsk_', 'sk-', 'test-')):  # gsk_ for Groq, sk-/test- for testing
            import logging
            logger = logging.getLogger(__name__)
            logger.warning("Groq API key does not match expected format")
        
        # Initialize OpenAI-compatible client with Groq's base URL
        self.client = AsyncOpenAI(
            api_key=self.api_key,
            base_url="https://api.groq.com/openai/v1"
        )
        
        super().__init__(config)
    
    def _initialize_models(self):
        """Initialize available Groq models"""
        
        # Mixtral 8x7B - Fast, efficient mixture of experts model
        self.models["mixtral-8x7b-32768"] = ModelConfig(
            provider="groq",
            model_id="mixtral-8x7b-32768",
            tier=ModelTier.MEDIUM,
            max_tokens=32768,
            context_window=32768,
            input_price_per_1k=0.00027,  # $0.27 per 1M tokens
            output_price_per_1k=0.00027,  # $0.27 per 1M tokens
            supports_functions=True,
            supports_streaming=True,
            supports_vision=False,
        )
        
        # Llama 3 70B - Large, highly capable model
        self.models["llama3-70b-8192"] = ModelConfig(
            provider="groq",
            model_id="llama3-70b-8192",
            tier=ModelTier.LARGE,
            max_tokens=8192,
            context_window=8192,
            input_price_per_1k=0.00059,  # $0.59 per 1M tokens
            output_price_per_1k=0.00079,  # $0.79 per 1M tokens
            supports_functions=True,
            supports_streaming=True,
            supports_vision=False,
        )
        
        # Llama 3 8B - Small, fast model
        self.models["llama3-8b-8192"] = ModelConfig(
            provider="groq",
            model_id="llama3-8b-8192",
            tier=ModelTier.SMALL,
            max_tokens=8192,
            context_window=8192,
            input_price_per_1k=0.00005,  # $0.05 per 1M tokens
            output_price_per_1k=0.00010,  # $0.10 per 1M tokens
            supports_functions=True,
            supports_streaming=True,
            supports_vision=False,
        )
        
        # Llama 3.1 70B - Updated version with improvements
        self.models["llama-3.1-70b-versatile"] = ModelConfig(
            provider="groq",
            model_id="llama-3.1-70b-versatile",
            tier=ModelTier.LARGE,
            max_tokens=8192,
            context_window=131072,  # 128K context window
            input_price_per_1k=0.00059,  # $0.59 per 1M tokens
            output_price_per_1k=0.00079,  # $0.79 per 1M tokens
            supports_functions=True,
            supports_streaming=True,
            supports_vision=False,
        )
        
        # Llama 3.1 8B - Small model with large context
        self.models["llama-3.1-8b-instant"] = ModelConfig(
            provider="groq",
            model_id="llama-3.1-8b-instant",
            tier=ModelTier.SMALL,
            max_tokens=8192,
            context_window=131072,  # 128K context window
            input_price_per_1k=0.00005,  # $0.05 per 1M tokens
            output_price_per_1k=0.00010,  # $0.10 per 1M tokens
            supports_functions=True,
            supports_streaming=True,
            supports_vision=False,
        )
        
        # Gemma 7B - Google's open model
        self.models["gemma-7b-it"] = ModelConfig(
            provider="groq",
            model_id="gemma-7b-it",
            tier=ModelTier.SMALL,
            max_tokens=8192,
            context_window=8192,
            input_price_per_1k=0.00010,  # $0.10 per 1M tokens
            output_price_per_1k=0.00010,  # $0.10 per 1M tokens
            supports_functions=False,
            supports_streaming=True,
            supports_vision=False,
        )
        
        # Gemma 2 9B - Newer version with improvements
        self.models["gemma2-9b-it"] = ModelConfig(
            provider="groq",
            model_id="gemma2-9b-it",
            tier=ModelTier.SMALL,
            max_tokens=8192,
            context_window=8192,
            input_price_per_1k=0.00020,  # $0.20 per 1M tokens
            output_price_per_1k=0.00020,  # $0.20 per 1M tokens
            supports_functions=False,
            supports_streaming=True,
            supports_vision=False,
        )
    
    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        """Execute a completion request using Groq"""
        
        # Select model based on tier
        model_id = self._select_model_for_tier(request.model_tier)
        model_config = self.models[model_id]
        
        # Prepare OpenAI-compatible request
        completion_params: Dict[str, Any] = {
            "model": model_id,
            "input": prepare_responses_input(request.messages),
            "temperature": request.temperature,
            "top_p": request.top_p,
        }

        max_tokens = request.max_tokens or model_config.max_tokens
        if max_tokens:
            completion_params["max_output_tokens"] = max_tokens

        if request.response_format:
            completion_params["text"] = {"format": request.response_format}

        if request.functions and model_config.supports_functions:
            tools = prepare_tools(request.functions)
            if tools:
                completion_params["tools"] = tools
                tool_choice = prepare_tool_choice(request.function_call)
                if tool_choice:
                    completion_params["tool_choice"] = tool_choice

        extra_body = build_extra_body(
            stop=request.stop,
            frequency_penalty=request.frequency_penalty,
            presence_penalty=request.presence_penalty,
            seed=request.seed,
        )
        if extra_body:
            completion_params["extra_body"] = extra_body

        try:
            # Execute completion
            response = await self.client.responses.create(**completion_params)

            content = extract_output_text(response)
            usage = getattr(response, "usage", None)

            input_tokens = getattr(usage, "input_tokens", 0) if usage else 0
            output_tokens = getattr(usage, "output_tokens", 0) if usage else 0
            total_tokens = getattr(usage, "total_tokens", input_tokens + output_tokens)

            normalized_calls = normalize_response_tool_calls(response)
            function_call = select_primary_function_call(normalized_calls)
            finish_reason = determine_finish_reason(response)

            usage_record = TokenUsage(
                input_tokens=input_tokens,
                output_tokens=output_tokens,
                total_tokens=total_tokens,
                estimated_cost=self._calculate_cost(
                    input_tokens,
                    output_tokens,
                    model_config,
                ),
            )

            return CompletionResponse(
                content=content,
                model=model_id,
                usage=usage_record,
                finish_reason=finish_reason,
                function_call=function_call,
                raw_response=response,
            )

        except Exception as e:
            self.logger.error(f"Groq completion failed: {e}")
            raise
    
    async def complete_stream(
        self, 
        request: CompletionRequest
    ) -> AsyncIterator[CompletionResponse]:
        """Stream a completion response from Groq"""
        
        # Select model based on tier
        model_id = self._select_model_for_tier(request.model_tier)
        model_config = self.models[model_id]
        
        # Prepare request
        completion_params: Dict[str, Any] = {
            "model": model_id,
            "input": prepare_responses_input(request.messages),
            "temperature": request.temperature,
            "top_p": request.top_p,
        }

        max_tokens = request.max_tokens or model_config.max_tokens
        if max_tokens:
            completion_params["max_output_tokens"] = max_tokens

        if request.functions and model_config.supports_functions:
            tools = prepare_tools(request.functions)
            if tools:
                completion_params["tools"] = tools
                tool_choice = prepare_tool_choice(request.function_call)
                if tool_choice:
                    completion_params["tool_choice"] = tool_choice

        if request.response_format:
            completion_params["text"] = {"format": request.response_format}

        extra_body = build_extra_body(
            stop=request.stop,
            frequency_penalty=request.frequency_penalty,
            presence_penalty=request.presence_penalty,
            seed=request.seed,
        )
        if extra_body:
            completion_params["extra_body"] = extra_body

        try:
            # Stream response
            async with self.client.responses.stream(**completion_params) as stream:
                async for event in stream:
                    event_type = getattr(event, "type", None)
                    if event_type == "response.output_text.delta":
                        yield CompletionResponse(
                            content=getattr(event, "delta", ""),
                            model=model_id,
                            usage=None,
                            finish_reason=None,
                            raw_response=event,
                        )
                    elif event_type == "error":
                        message = getattr(event, "message", "streaming error")
                        raise Exception(f"Groq streaming failed: {message}")

                final_response = stream.get_final_response()
                usage = getattr(final_response, "usage", None)
                if usage:
                    input_tokens = getattr(usage, "input_tokens", 0)
                    output_tokens = getattr(usage, "output_tokens", 0)
                    total_tokens = getattr(usage, "total_tokens", input_tokens + output_tokens)
                    usage_record = TokenUsage(
                        input_tokens=input_tokens,
                        output_tokens=output_tokens,
                        total_tokens=total_tokens,
                        estimated_cost=self._calculate_cost(
                            input_tokens,
                            output_tokens,
                            model_config,
                        ),
                    )

                    yield CompletionResponse(
                        content="",
                        model=model_id,
                        usage=usage_record,
                        finish_reason=determine_finish_reason(final_response),
                        raw_response=final_response,
                    )

        except Exception as e:
            self.logger.error(f"Groq streaming failed: {e}")
            raise
    
    def _select_model_for_tier(self, tier: ModelTier) -> str:
        """Select appropriate Groq model based on tier"""
        
        # Tier-based selection with Groq's fastest models
        tier_models = {
            ModelTier.SMALL: "llama-3.1-8b-instant",  # Fastest with large context
            ModelTier.MEDIUM: "mixtral-8x7b-32768",   # Good balance
            ModelTier.LARGE: "llama-3.1-70b-versatile",  # Most capable
        }
        
        model_id = tier_models.get(tier, "mixtral-8x7b-32768")
        
        # Check if model is available
        if model_id not in self.models:
            # Fallback to any available model
            if self.models:
                model_id = list(self.models.keys())[0]
                self.logger.warning(f"Model for tier {tier} not found, using {model_id}")
            else:
                raise ValueError("No models available")
        
        return model_id
    
    def _calculate_cost(
        self, 
        input_tokens: int, 
        output_tokens: int, 
        model_config: ModelConfig
    ) -> float:
        """Calculate cost based on token usage"""
        input_cost = (input_tokens / 1000) * model_config.input_price_per_1k
        output_cost = (output_tokens / 1000) * model_config.output_price_per_1k
        return input_cost + output_cost
    
    def count_tokens(self, text: str, model: Optional[str] = None) -> int:
        """Count tokens in text - uses estimation for Groq"""
        # Groq uses similar tokenization to Llama models
        # Rough estimation: 1 token per 4 characters
        return len(text) // 4