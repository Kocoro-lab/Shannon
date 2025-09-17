"""
OpenAI Provider Implementation
"""

import asyncio
import os
from typing import Dict, List, Any, Optional, AsyncIterator
import openai
from openai import AsyncOpenAI
import tiktoken

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


class OpenAIProvider(LLMProvider):
    """OpenAI API provider implementation"""
    
    def __init__(self, config: Dict[str, Any]):
        # Initialize OpenAI client
        api_key = config.get("api_key") or os.getenv("OPENAI_API_KEY")
        if not api_key:
            raise ValueError("OpenAI API key not provided")
        
        self.client = AsyncOpenAI(api_key=api_key)
        self.organization = config.get("organization")
        if self.organization:
            self.client.organization = self.organization
        
        # Token encoders for different models
        self.encoders = {}
        
        super().__init__(config)
    
    def _initialize_models(self):
        """Initialize OpenAI model configurations"""
        
        # GPT-4 models
        self.models["gpt-4-turbo"] = ModelConfig(
            provider="openai",
            model_id="gpt-4-turbo-preview",
            tier=ModelTier.LARGE,
            max_tokens=4096,
            context_window=128000,
            input_price_per_1k=0.01,
            output_price_per_1k=0.03,
            supports_functions=True,
            supports_streaming=True,
            supports_vision=True,
        )
        
        self.models["gpt-4"] = ModelConfig(
            provider="openai",
            model_id="gpt-4",
            tier=ModelTier.LARGE,
            max_tokens=8192,
            context_window=8192,
            input_price_per_1k=0.03,
            output_price_per_1k=0.06,
            supports_functions=True,
            supports_streaming=True,
        )
        
        self.models["gpt-4-32k"] = ModelConfig(
            provider="openai",
            model_id="gpt-4-32k",
            tier=ModelTier.LARGE,
            max_tokens=32768,
            context_window=32768,
            input_price_per_1k=0.06,
            output_price_per_1k=0.12,
            supports_functions=True,
            supports_streaming=True,
        )
        
        # GPT-3.5 models
        self.models["gpt-3.5-turbo"] = ModelConfig(
            provider="openai",
            model_id="gpt-3.5-turbo",
            tier=ModelTier.SMALL,
            max_tokens=4096,
            context_window=16385,
            input_price_per_1k=0.0005,
            output_price_per_1k=0.0015,
            supports_functions=True,
            supports_streaming=True,
        )
        
        self.models["gpt-3.5-turbo-16k"] = ModelConfig(
            provider="openai",
            model_id="gpt-3.5-turbo-16k",
            tier=ModelTier.SMALL,
            max_tokens=16384,
            context_window=16384,
            input_price_per_1k=0.003,
            output_price_per_1k=0.004,
            supports_functions=True,
            supports_streaming=True,
        )
    
    def _get_encoder(self, model: str):
        """Get or create token encoder for a model"""
        if model not in self.encoders:
            try:
                self.encoders[model] = tiktoken.encoding_for_model(model)
            except KeyError:
                # Fall back to cl100k_base encoding
                self.encoders[model] = tiktoken.get_encoding("cl100k_base")
        return self.encoders[model]
    
    def count_tokens(self, messages: List[Dict[str, Any]], model: str) -> int:
        """Count tokens using tiktoken"""
        encoder = self._get_encoder(model)
        
        # Token counting logic based on OpenAI's guidelines
        tokens_per_message = 3  # Every message follows <im_start>{role/name}\n{content}<im_end>\n
        tokens_per_name = 1  # If there's a name, the role is omitted
        
        num_tokens = 0
        for message in messages:
            num_tokens += tokens_per_message
            for key, value in message.items():
                if isinstance(value, str):
                    num_tokens += len(encoder.encode(value))
                    if key == "name":
                        num_tokens += tokens_per_name
        
        num_tokens += 3  # Every reply is primed with <im_start>assistant<im_sep>
        return num_tokens
    
    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        """Generate a completion using OpenAI API"""
        
        # Select model based on tier
        model_config = self.select_model_for_tier(request.model_tier, request.max_tokens)
        model = model_config.model_id
        
        # Count input tokens
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
        except openai.APIError as e:
            raise Exception(f"OpenAI API error: {e}")

        latency_ms = int((time.time() - start_time) * 1000)

        # Extract response content
        usage = getattr(response, "usage", None)
        output_tokens = getattr(usage, "output_tokens", 0) if usage else 0
        total_tokens = getattr(usage, "total_tokens", 0) if usage else 0

        # Calculate cost
        cost = self.estimate_cost(input_tokens, output_tokens, model)

        content = extract_output_text(response)
        tool_calls = normalize_response_tool_calls(response)
        function_call = select_primary_function_call(tool_calls)
        finish_reason = determine_finish_reason(response)

        # Build response
        return CompletionResponse(
            content=content or "",
            model=model,
            provider="openai",
            usage=TokenUsage(
                input_tokens=input_tokens,
                output_tokens=output_tokens,
                total_tokens=total_tokens,
                estimated_cost=cost,
            ),
            finish_reason=finish_reason,
            function_call=function_call,
            request_id=response.id,
            latency_ms=latency_ms,
        )
    
    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator[str]:
        """Stream a completion using OpenAI API"""
        
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
                        raise Exception(f"OpenAI API error: {message}")

        except openai.APIError as e:
            raise Exception(f"OpenAI API error: {e}")