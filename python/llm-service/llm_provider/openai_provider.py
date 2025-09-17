"""
OpenAI Provider Implementation
"""

import os
from typing import Dict, List, Any, AsyncIterator
import openai
from openai import AsyncOpenAI
import tiktoken

from .base import (
    LLMProvider,
    ModelConfig,
    ModelTier,
    CompletionRequest,
    CompletionResponse,
    TokenUsage,
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
        tokens_per_message = (
            3  # Every message follows <im_start>{role/name}\n{content}<im_end>\n
        )
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
        model_config = self.select_model_for_tier(
            request.model_tier, request.max_tokens
        )
        model = model_config.model_id

        # Count input tokens
        input_tokens = self.count_tokens(request.messages, model)

        # Prepare API request
        api_request = {
            "model": model,
            "messages": request.messages,
            "temperature": request.temperature,
            "top_p": request.top_p,
            "frequency_penalty": request.frequency_penalty,
            "presence_penalty": request.presence_penalty,
        }

        if request.max_tokens:
            api_request["max_tokens"] = request.max_tokens

        if request.stop:
            api_request["stop"] = request.stop

        if request.functions:
            api_request["functions"] = request.functions
            if request.function_call:
                api_request["function_call"] = request.function_call

        if request.seed is not None:
            api_request["seed"] = request.seed

        if request.response_format:
            api_request["response_format"] = request.response_format

        if request.user:
            api_request["user"] = request.user

        # Make API call
        import time

        start_time = time.time()

        try:
            response = await self.client.chat.completions.create(**api_request)
        except openai.APIError as e:
            raise Exception(f"OpenAI API error: {e}")

        latency_ms = int((time.time() - start_time) * 1000)

        # Extract response
        choice = response.choices[0]
        message = choice.message

        # Count output tokens
        output_tokens = response.usage.completion_tokens
        total_tokens = response.usage.total_tokens

        # Calculate cost
        cost = self.estimate_cost(input_tokens, output_tokens, model)

        # Build response
        return CompletionResponse(
            content=message.content or "",
            model=model,
            provider="openai",
            usage=TokenUsage(
                input_tokens=input_tokens,
                output_tokens=output_tokens,
                total_tokens=total_tokens,
                estimated_cost=cost,
            ),
            finish_reason=choice.finish_reason,
            function_call=message.function_call
            if hasattr(message, "function_call")
            else None,
            request_id=response.id,
            latency_ms=latency_ms,
        )

    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator[str]:
        """Stream a completion using OpenAI API"""

        # Select model based on tier
        model_config = self.select_model_for_tier(
            request.model_tier, request.max_tokens
        )
        model = model_config.model_id

        # Prepare API request
        api_request = {
            "model": model,
            "messages": request.messages,
            "temperature": request.temperature,
            "stream": True,
        }

        if request.max_tokens:
            api_request["max_tokens"] = request.max_tokens

        # Make streaming API call
        try:
            stream = await self.client.chat.completions.create(**api_request)

            async for chunk in stream:
                if chunk.choices[0].delta.content:
                    yield chunk.choices[0].delta.content

        except openai.APIError as e:
            raise Exception(f"OpenAI API error: {e}")
