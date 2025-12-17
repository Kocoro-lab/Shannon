"""
Anthropic Claude Provider Implementation
"""

import os
import logging
from typing import Dict, List, Any, AsyncIterator
import anthropic
from anthropic import AsyncAnthropic

from .base import (
    LLMProvider,
    CompletionRequest,
    CompletionResponse,
    TokenUsage,
    TokenCounter,
)

logger = logging.getLogger(__name__)


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
        """Load Anthropic model configurations from YAML-driven config."""

        self._load_models_from_config()

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
            # Handle both OpenAI format ({"type": "function", "function": {...}})
            # and direct function schema format ({"name": "...", ...})
            if func.get("type") == "function" and "function" in func:
                func = func["function"]

            # Skip if function schema doesn't have required 'name' field
            if "name" not in func:
                continue

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

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model = model_config.model_id

        # Count input tokens (estimation)
        _ = self.count_tokens(request.messages, model)  # Reserved for future use

        # Convert messages to Claude format
        system_message, claude_messages = self._convert_messages_to_claude_format(
            request.messages
        )

        # Compute safe max_tokens based on context window headroom (OpenAI-style)
        prompt_tokens_est = self.count_tokens(request.messages, model)
        safety_margin = 256
        model_context = getattr(model_config, "context_window", 200000)
        model_max_output = getattr(model_config, "max_tokens", 8192)
        requested_max = int(request.max_tokens) if request.max_tokens else model_max_output
        headroom = model_context - prompt_tokens_est - safety_margin
        
        # Check if there's sufficient context window headroom
        if headroom <= 0:
            raise ValueError(
                f"Insufficient context window: prompt uses ~{prompt_tokens_est} tokens, "
                f"max context is {model_context}, leaving no room for output. "
                f"Please reduce prompt length."
            )
        
        adjusted_max = min(requested_max, model_max_output, headroom)
        
        api_request = {
            "model": model,
            "messages": claude_messages,
            "max_tokens": adjusted_max,
        }

        # Anthropic API requires temperature and top_p to be mutually exclusive.
        # Note: `0.0` is a valid temperature; do not use truthiness checks here.
        if request.temperature is not None and request.top_p is not None:
            # Prefer temperature when both are present.
            api_request["temperature"] = request.temperature
            logger.warning(
                "Anthropic API: both temperature and top_p were set; "
                "using temperature and ignoring top_p"
            )
        elif request.temperature is not None:
            api_request["temperature"] = request.temperature
        elif request.top_p is not None:
            api_request["top_p"] = request.top_p
        # If neither is set, omit both and let the API defaults apply.

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

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model = model_config.model_id

        # Convert messages to Claude format
        system_message, claude_messages = self._convert_messages_to_claude_format(
            request.messages
        )

        # Compute safe max_tokens based on context window headroom (OpenAI-style)
        prompt_tokens_est = self.count_tokens(request.messages, model)
        safety_margin = 256
        model_context = getattr(model_config, "context_window", 200000)
        model_max_output = getattr(model_config, "max_tokens", 8192)
        requested_max = int(request.max_tokens) if request.max_tokens else model_max_output
        headroom = model_context - prompt_tokens_est - safety_margin
        
        # Check if there's sufficient context window headroom
        if headroom <= 0:
            raise ValueError(
                f"Insufficient context window: prompt uses ~{prompt_tokens_est} tokens, "
                f"max context is {model_context}, leaving no room for output. "
                f"Please reduce prompt length."
            )
        
        adjusted_max = min(requested_max, model_max_output, headroom)
        
        api_request = {
            "model": model,
            "messages": claude_messages,
            "max_tokens": adjusted_max,
            "temperature": request.temperature,
        }

        if system_message:
            api_request["system"] = system_message

        # Make streaming API call
        try:
            async with self.client.messages.stream(**api_request) as stream:
                async for text in stream.text_stream:
                    yield text

                # After streaming completes, get the final message with usage
                final_message = await stream.get_final_message()
                if final_message and hasattr(final_message, "usage"):
                    yield {
                        "usage": {
                            "total_tokens": final_message.usage.input_tokens + final_message.usage.output_tokens,
                            "input_tokens": final_message.usage.input_tokens,
                            "output_tokens": final_message.usage.output_tokens,
                        },
                        "model": final_message.model,
                        "provider": "anthropic",
                    }

        except anthropic.APIError as e:
            raise Exception(f"Anthropic API error: {e}")
