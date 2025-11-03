"""
Groq Provider Implementation
High-performance LLM inference using Groq's LPU (Language Processing Unit)
"""

import os
from typing import Dict, Any, Optional, AsyncIterator
from tenacity import retry, stop_after_attempt, wait_exponential
import logging
from openai import AsyncOpenAI

from .base import (
    LLMProvider,
    ModelConfig,
    ModelTier,
    CompletionRequest,
    CompletionResponse,
    TokenUsage,
)


class GroqProvider(LLMProvider):
    """Provider for Groq's high-performance LLM inference"""

    def __init__(self, config: Dict[str, Any]):
        self.logger = logging.getLogger(__name__)
        # Get API key from config or environment
        self.api_key = config.get("api_key") or os.getenv("GROQ_API_KEY")
        if not self.api_key:
            raise ValueError("Groq API key not provided")

        # Validate API key format (Groq keys typically start with 'gsk_' and are 56+ chars)
        if len(self.api_key) < 40:
            raise ValueError("Invalid Groq API key format - too short")
        if not self.api_key.startswith(
            ("gsk_", "sk-", "test-")
        ):  # gsk_ for Groq, sk-/test- for testing
            self.logger.warning("Groq API key does not match expected format")

        # Initialize OpenAI-compatible client with Groq's base URL
        self.client = AsyncOpenAI(
            api_key=self.api_key, base_url="https://api.groq.com/openai/v1"
        )

        super().__init__(config)

    def _initialize_models(self):
        """Initialize available Groq models from configuration."""

        self._load_models_from_config()

    @retry(
        stop=stop_after_attempt(3), wait=wait_exponential(multiplier=0.5, min=1, max=8)
    )
    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        """Execute a completion request using Groq"""

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model_id = model_config.model_id

        # Prepare OpenAI-compatible request
        completion_params = {
            "model": model_id,
            "messages": request.messages,
            "temperature": request.temperature,
            "max_tokens": request.max_tokens or model_config.max_tokens,
            "top_p": request.top_p,
            "frequency_penalty": request.frequency_penalty,
            "presence_penalty": request.presence_penalty,
            "stream": False,
        }

        if request.stop:
            completion_params["stop"] = request.stop

        if request.seed is not None:
            completion_params["seed"] = request.seed

        if request.response_format:
            completion_params["response_format"] = request.response_format

        # Functions are supported by some models
        if request.functions and model_config.supports_functions:
            completion_params["functions"] = request.functions
            if request.function_call:
                completion_params["function_call"] = request.function_call

        try:
            # Execute completion
            response = await self.client.chat.completions.create(**completion_params)

            # Extract response
            choice = response.choices[0]
            content = choice.message.content or ""

            # Handle function calls if present
            if (
                hasattr(choice.message, "function_call")
                and choice.message.function_call
            ):
                content = {
                    "function_call": {
                        "name": choice.message.function_call.name,
                        "arguments": choice.message.function_call.arguments,
                    }
                }

            # Calculate usage
            usage = TokenUsage(
                input_tokens=response.usage.prompt_tokens,
                output_tokens=response.usage.completion_tokens,
                total_tokens=response.usage.total_tokens,
                estimated_cost=self._calculate_cost(
                    response.usage.prompt_tokens,
                    response.usage.completion_tokens,
                    model_config,
                ),
            )

            return CompletionResponse(
                content=content,
                model=model_id,
                usage=usage,
                finish_reason=choice.finish_reason,
                raw_response=response,
            )

        except Exception as e:
            self.logger.error(f"Groq completion failed: {e}")
            raise

    async def complete_stream(
        self, request: CompletionRequest
    ) -> AsyncIterator[CompletionResponse]:
        """Stream a completion response from Groq (provider-specific chunks)."""

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model_id = model_config.model_id

        # Prepare request
        completion_params = {
            "model": model_id,
            "messages": request.messages,
            "temperature": request.temperature,
            "max_tokens": request.max_tokens or model_config.max_tokens,
            "top_p": request.top_p,
            "frequency_penalty": request.frequency_penalty,
            "presence_penalty": request.presence_penalty,
            "stream": True,
        }

        if request.stop:
            completion_params["stop"] = request.stop

        if request.seed is not None:
            completion_params["seed"] = request.seed

        if request.functions and model_config.supports_functions:
            completion_params["functions"] = request.functions
            if request.function_call:
                completion_params["function_call"] = request.function_call

        try:
            # Stream response
            stream = await self.client.chat.completions.create(**completion_params)

            input_tokens = 0
            output_tokens = 0

            async for chunk in stream:
                if chunk.choices and chunk.choices[0].delta.content:
                    yield CompletionResponse(
                        content=chunk.choices[0].delta.content,
                        model=model_id,
                        usage=None,
                        finish_reason=None,
                        raw_response=chunk,
                    )

                # Track token usage from chunks if available
                if hasattr(chunk, "usage") and chunk.usage:
                    input_tokens = chunk.usage.prompt_tokens or input_tokens
                    output_tokens = chunk.usage.completion_tokens or output_tokens

            # Final response with usage
            if input_tokens > 0 or output_tokens > 0:
                usage = TokenUsage(
                    input_tokens=input_tokens,
                    output_tokens=output_tokens,
                    total_tokens=input_tokens + output_tokens,
                    estimated_cost=self._calculate_cost(
                        input_tokens, output_tokens, model_config
                    ),
                )

                yield CompletionResponse(
                    content="",
                    model=model_id,
                    usage=usage,
                    finish_reason="stop",
                    raw_response=None,
                )

        except Exception as e:
            self.logger.error(f"Groq streaming failed: {e}")
            raise

    # NOTE: Tier-to-model fallback removed. Selection now relies on resolve_model_config
    # backed by models.yaml via the manager's configuration.

    def _calculate_cost(
        self, input_tokens: int, output_tokens: int, model_config: ModelConfig
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

    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator[str]:
        """Normalized streaming: yield text chunks only."""
        async for chunk in self.complete_stream(request):
            if chunk and isinstance(chunk.content, str) and chunk.content:
                yield chunk.content
