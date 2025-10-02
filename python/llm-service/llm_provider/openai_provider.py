"""
OpenAI Provider Implementation
"""

import os
from typing import Dict, List, Any, AsyncIterator
import openai
from openai import AsyncOpenAI
import tiktoken

from .base import LLMProvider, CompletionRequest, CompletionResponse, TokenUsage


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
        """Load OpenAI model configurations from YAML-driven config."""

        self._load_models_from_config()

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

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
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

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
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

    async def generate_embedding(
        self, text: str, model: str = "text-embedding-3-small"
    ) -> List[float]:
        """Generate embeddings using OpenAI API."""

        try:
            response = await self.client.embeddings.create(model=model, input=text)
            return response.data[0].embedding
        except openai.APIError as e:
            raise Exception(f"OpenAI embedding error: {e}")
