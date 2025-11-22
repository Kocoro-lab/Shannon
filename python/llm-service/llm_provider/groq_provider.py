"""
Groq Provider Implementation
High-performance LLM inference using Groq's LPU (Language Processing Unit).
Aligned with OpenAI, xAI, and Google provider patterns for consistency.
"""

import os
import time
from typing import Dict, List, Any, AsyncIterator
from tenacity import retry, stop_after_attempt, wait_exponential
import logging
from openai import AsyncOpenAI

from .base import (
    LLMProvider,
    CompletionRequest,
    CompletionResponse,
    TokenUsage,
    TokenCounter,
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

    def _resolve_alias(self, model_id: str) -> str:
        """Return the configured alias for a given vendor model_id, if any."""
        for alias, cfg in self.models.items():
            if cfg.model_id == model_id:
                return alias
        return model_id

    def count_tokens(self, messages: List[Dict[str, Any]], model: str) -> int:
        """Count tokens for messages using TokenCounter."""
        return TokenCounter.count_messages_tokens(messages, model)

    @retry(
        stop=stop_after_attempt(3), wait=wait_exponential(multiplier=0.5, min=1, max=8)
    )
    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        """Execute a completion request using Groq with native async support."""

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model_id = model_config.model_id
        model_alias = self._resolve_alias(model_id)

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

        start_time = time.time()
        try:
            # Execute completion
            response = await self.client.chat.completions.create(**completion_params)
            
            latency_ms = int((time.time() - start_time) * 1000)

            # Extract response
            choice = response.choices[0]
            message = choice.message
            content = message.content or ""

            # Handle function calls if present
            function_call = None
            if hasattr(message, "function_call") and message.function_call:
                function_call = {
                    "name": message.function_call.name,
                    "arguments": message.function_call.arguments,
                }

            # Extract token usage from response
            try:
                prompt_tokens = int(getattr(response.usage, "prompt_tokens", 0))
                completion_tokens = int(getattr(response.usage, "completion_tokens", 0))
                total_tokens = int(
                    getattr(response.usage, "total_tokens", prompt_tokens + completion_tokens)
                )
            except Exception:
                # Fallback to estimation if usage not provided
                prompt_tokens = self.count_tokens(request.messages, model_id)
                completion_tokens = self.count_tokens(
                    [{"role": "assistant", "content": content}], model_id
                )
                total_tokens = prompt_tokens + completion_tokens

            # Use base class estimate_cost with alias for proper lookup
            cost = self.estimate_cost(prompt_tokens, completion_tokens, model_alias)

            return CompletionResponse(
                content=content,
                model=model_id,
                provider="groq",
                usage=TokenUsage(
                    input_tokens=prompt_tokens,
                    output_tokens=completion_tokens,
                    total_tokens=total_tokens,
                    estimated_cost=cost,
                ),
                finish_reason=choice.finish_reason or "stop",
                function_call=function_call,
                request_id=getattr(response, "id", None),
                latency_ms=latency_ms,
            )

        except Exception as e:
            self.logger.error(f"Groq completion failed: {e}")
            raise Exception(f"Groq API error: {e}")

    async def complete_stream(
        self, request: CompletionRequest
    ) -> AsyncIterator[CompletionResponse]:
        """Stream a completion response from Groq (provider-specific chunks)."""

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model_id = model_config.model_id
        model_alias = self._resolve_alias(model_id)

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
                        provider="groq",
                        usage=TokenUsage(
                            input_tokens=0,
                            output_tokens=0,
                            total_tokens=0,
                            estimated_cost=0.0,
                        ),
                        finish_reason=None,
                        function_call=None,
                        request_id=None,
                        latency_ms=None,
                    )

                # Track token usage from chunks if available
                if hasattr(chunk, "usage") and chunk.usage:
                    input_tokens = chunk.usage.prompt_tokens or input_tokens
                    output_tokens = chunk.usage.completion_tokens or output_tokens

            # Final response with usage
            if input_tokens > 0 or output_tokens > 0:
                total_tokens = input_tokens + output_tokens
                cost = self.estimate_cost(input_tokens, output_tokens, model_alias)

                yield CompletionResponse(
                    content="",
                    model=model_id,
                    provider="groq",
                    usage=TokenUsage(
                        input_tokens=input_tokens,
                        output_tokens=output_tokens,
                        total_tokens=total_tokens,
                        estimated_cost=cost,
                    ),
                    finish_reason="stop",
                    function_call=None,
                    request_id=None,
                    latency_ms=None,
                )

        except Exception as e:
            self.logger.error(f"Groq streaming failed: {e}")
            raise Exception(f"Groq streaming error: {e}")

    # NOTE: Tier-to-model fallback removed. Selection now relies on resolve_model_config
    # backed by models.yaml via the manager's configuration.

    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator[str]:
        """Normalized streaming: yield text chunks and usage metadata."""
        async for chunk in self.complete_stream(request):
            if chunk and isinstance(chunk.content, str) and chunk.content:
                yield chunk.content
            # Yield usage metadata when available (final chunk with empty content)
            elif chunk and chunk.usage and chunk.usage.total_tokens > 0:
                yield {
                    "usage": {
                        "total_tokens": chunk.usage.total_tokens,
                        "input_tokens": chunk.usage.input_tokens,
                        "output_tokens": chunk.usage.output_tokens,
                    },
                    "model": chunk.model,
                    "provider": chunk.provider,
                }
