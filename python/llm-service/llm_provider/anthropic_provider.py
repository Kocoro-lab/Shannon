"""
Anthropic Claude Provider Implementation
"""

import os
from typing import Dict, List, Any, AsyncIterator
import anthropic
from anthropic import AsyncAnthropic

from .base import (
    LLMProvider,
    ModelConfig,
    ModelTier,
    CompletionRequest,
    CompletionResponse,
    TokenUsage,
    TokenCounter,
)


class AnthropicProvider(LLMProvider):
    """Anthropic Claude API provider implementation"""

    def __init__(self, config: Dict[str, Any]):
        # Initialize Anthropic client
        api_key = config.get("api_key") or os.getenv("ANTHROPIC_API_KEY")
        if not api_key:
            raise ValueError("Anthropic API key not provided")

        self.client = AsyncAnthropic(api_key=api_key)

        super().__init__(config)

    def _initialize_models(self):
        """Initialize Anthropic model configurations"""

        # Claude 3 models
        self.models["claude-3-opus"] = ModelConfig(
            provider="anthropic",
            model_id="claude-3-opus-20240229",
            tier=ModelTier.LARGE,
            max_tokens=4096,
            context_window=200000,
            input_price_per_1k=0.015,
            output_price_per_1k=0.075,
            supports_functions=True,
            supports_streaming=True,
            supports_vision=True,
        )

        self.models["claude-3-sonnet"] = ModelConfig(
            provider="anthropic",
            model_id="claude-3-sonnet-20240229",
            tier=ModelTier.MEDIUM,
            max_tokens=4096,
            context_window=200000,
            input_price_per_1k=0.003,
            output_price_per_1k=0.015,
            supports_functions=True,
            supports_streaming=True,
            supports_vision=True,
        )

        self.models["claude-3-haiku"] = ModelConfig(
            provider="anthropic",
            model_id="claude-3-haiku-20240307",
            tier=ModelTier.SMALL,
            max_tokens=4096,
            context_window=200000,
            input_price_per_1k=0.00025,
            output_price_per_1k=0.00125,
            supports_functions=True,
            supports_streaming=True,
            supports_vision=True,
        )

        # Claude 2 models (legacy)
        self.models["claude-2.1"] = ModelConfig(
            provider="anthropic",
            model_id="claude-2.1",
            tier=ModelTier.MEDIUM,
            max_tokens=4096,
            context_window=200000,
            input_price_per_1k=0.008,
            output_price_per_1k=0.024,
            supports_functions=False,
            supports_streaming=True,
            supports_vision=False,
        )

    def count_tokens(self, messages: List[Dict[str, Any]], model: str) -> int:
        """
        Count tokens for Claude models.
        Note: Anthropic doesn't provide a public tokenizer, so we estimate.
        """
        # Use the base token counter for estimation
        return TokenCounter.count_messages_tokens(messages, model)

    def _convert_messages_to_claude_format(
        self, messages: List[Dict[str, Any]]
    ) -> tuple[str, List[Dict]]:
        """Convert OpenAI-style messages to Claude format"""
        system_message = ""
        claude_messages = []

        for message in messages:
            role = message["role"]
            content = message["content"]

            if role == "system":
                # Claude uses a separate system parameter
                system_message = content
            elif role == "user":
                claude_messages.append({"role": "user", "content": content})
            elif role == "assistant":
                claude_messages.append({"role": "assistant", "content": content})
            elif role == "function":
                # Convert function results to user messages
                claude_messages.append(
                    {"role": "user", "content": f"Function result: {content}"}
                )

        return system_message, claude_messages

    def _convert_functions_to_tools(self, functions: List[Dict]) -> List[Dict]:
        """Convert OpenAI function format to Claude tools format"""
        tools = []
        for func in functions:
            tool = {
                "name": func["name"],
                "description": func.get("description", ""),
                "input_schema": {
                    "type": "object",
                    "properties": func.get("parameters", {}).get("properties", {}),
                    "required": func.get("parameters", {}).get("required", []),
                },
            }
            tools.append(tool)
        return tools

    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        """Generate a completion using Anthropic API"""

        # Select model based on tier
        model_config = self.select_model_for_tier(
            request.model_tier, request.max_tokens
        )
        model = model_config.model_id

        # Count input tokens (estimation)
        _ = self.count_tokens(request.messages, model)  # Reserved for future use

        # Convert messages to Claude format
        system_message, claude_messages = self._convert_messages_to_claude_format(
            request.messages
        )

        # Prepare API request
        api_request = {
            "model": model,
            "messages": claude_messages,
            "max_tokens": request.max_tokens or 4096,
            "temperature": request.temperature,
            "top_p": request.top_p,
        }

        if system_message:
            api_request["system"] = system_message

        if request.stop:
            api_request["stop_sequences"] = request.stop

        # Handle functions/tools
        if request.functions and model_config.supports_functions:
            api_request["tools"] = self._convert_functions_to_tools(request.functions)

            # Handle function calling
            if request.function_call:
                if isinstance(request.function_call, str):
                    if request.function_call == "auto":
                        api_request["tool_choice"] = {"type": "auto"}
                    elif request.function_call == "none":
                        api_request["tool_choice"] = {"type": "none"}
                elif isinstance(request.function_call, dict):
                    api_request["tool_choice"] = {
                        "type": "tool",
                        "name": request.function_call.get("name"),
                    }

        # Make API call
        import time

        start_time = time.time()

        try:
            response = await self.client.messages.create(**api_request)
        except anthropic.APIError as e:
            raise Exception(f"Anthropic API error: {e}")

        latency_ms = int((time.time() - start_time) * 1000)

        # Extract response content
        content = ""
        function_call = None

        for content_block in response.content:
            if content_block.type == "text":
                content = content_block.text
            elif content_block.type == "tool_use":
                # Convert tool use to function call format
                function_call = {
                    "name": content_block.name,
                    "arguments": content_block.input,
                }

        # Get token usage
        output_tokens = response.usage.output_tokens
        total_tokens = response.usage.input_tokens + output_tokens

        # Calculate cost
        cost = self.estimate_cost(response.usage.input_tokens, output_tokens, model)

        # Build response
        return CompletionResponse(
            content=content,
            model=model,
            provider="anthropic",
            usage=TokenUsage(
                input_tokens=response.usage.input_tokens,
                output_tokens=output_tokens,
                total_tokens=total_tokens,
                estimated_cost=cost,
            ),
            finish_reason=response.stop_reason or "stop",
            function_call=function_call,
            request_id=response.id,
            latency_ms=latency_ms,
        )

    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator[str]:
        """Stream a completion using Anthropic API"""

        # Select model based on tier
        model_config = self.select_model_for_tier(
            request.model_tier, request.max_tokens
        )
        model = model_config.model_id

        # Convert messages to Claude format
        system_message, claude_messages = self._convert_messages_to_claude_format(
            request.messages
        )

        # Prepare API request
        api_request = {
            "model": model,
            "messages": claude_messages,
            "max_tokens": request.max_tokens or 4096,
            "temperature": request.temperature,
            "stream": True,
        }

        if system_message:
            api_request["system"] = system_message

        # Make streaming API call
        try:
            async with self.client.messages.stream(**api_request) as stream:
                async for text in stream.text_stream:
                    yield text

        except anthropic.APIError as e:
            raise Exception(f"Anthropic API error: {e}")
