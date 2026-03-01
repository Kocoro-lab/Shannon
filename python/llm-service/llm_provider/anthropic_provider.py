"""
Anthropic Claude Provider Implementation
"""

import json
import os
import logging
import time
from typing import Dict, List, Any, AsyncIterator
import anthropic
from anthropic import AsyncAnthropic

from .base import (
    LLMProvider,
    CompletionRequest,
    CompletionResponse,
    TokenUsage,
    TokenCounter,
    extract_text_from_content,
)

logger = logging.getLogger(__name__)


CACHE_BREAK_MARKER = "<!-- cache_break -->"


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
                # Claude uses a separate system parameter (must be a string)
                system_message = extract_text_from_content(content)
            elif role == "user":
                if isinstance(content, str) and CACHE_BREAK_MARKER in content:
                    parts = content.split(CACHE_BREAK_MARKER, 1)
                    claude_messages.append({
                        "role": "user",
                        "content": [
                            {"type": "text", "text": parts[0], "cache_control": {"type": "ephemeral"}},
                            {"type": "text", "text": parts[1]},
                        ]
                    })
                else:
                    claude_messages.append({"role": "user", "content": content})
            elif role == "assistant":
                # Anthropic rejects assistant messages ending with whitespace
                if isinstance(content, str):
                    content = content.rstrip()
                # Convert OpenAI-style tool_calls field to Anthropic tool_use content blocks
                tool_calls = message.get("tool_calls")
                if tool_calls and isinstance(tool_calls, list):
                    blocks = []
                    if content:
                        text = content if isinstance(content, str) else str(content)
                        if text.strip():
                            blocks.append({"type": "text", "text": text})
                    for tc in tool_calls:
                        if not isinstance(tc, dict):
                            continue
                        # Handle OpenAI format: {"id":"...", "type":"function", "function":{"name":"...", "arguments":"..."}}
                        # and Shannon format: {"id":"...", "name":"...", "arguments":...}
                        fn = tc.get("function", {}) if tc.get("type") == "function" else tc
                        tc_id = tc.get("id", "")
                        name = fn.get("name", "") if isinstance(fn, dict) else tc.get("name", "")
                        arguments = fn.get("arguments", {}) if isinstance(fn, dict) else tc.get("arguments", {})
                        if isinstance(arguments, str):
                            try:
                                arguments = json.loads(arguments)
                            except (json.JSONDecodeError, TypeError):
                                arguments = {}
                        blocks.append({
                            "type": "tool_use",
                            "id": tc_id,
                            "name": name,
                            "input": arguments,
                        })
                    claude_messages.append({"role": "assistant", "content": blocks if blocks else content})
                else:
                    claude_messages.append({"role": "assistant", "content": content})
            elif role == "tool":
                # Convert OpenAI-style tool result to Anthropic tool_result content block
                tool_result_block = {
                    "type": "tool_result",
                    "tool_use_id": message.get("tool_call_id", ""),
                    "content": content if isinstance(content, str) else str(content or ""),
                }
                # Merge into previous user message to maintain Anthropic's alternating role requirement
                if claude_messages and claude_messages[-1]["role"] == "user":
                    prev_content = claude_messages[-1]["content"]
                    if isinstance(prev_content, list):
                        prev_content.append(tool_result_block)
                    elif isinstance(prev_content, str):
                        claude_messages[-1]["content"] = [
                            {"type": "text", "text": prev_content},
                            tool_result_block,
                        ]
                    else:
                        claude_messages[-1]["content"] = [tool_result_block]
                else:
                    claude_messages.append({
                        "role": "user",
                        "content": [tool_result_block],
                    })
            elif role == "function":
                # Convert function results to user messages
                claude_messages.append(
                    {"role": "user", "content": f"Function result: {content}"}
                )

        # Add cache_control to the last assistant message so the conversation
        # history prefix is cached across turns (Anthropic prompt caching).
        # Uses one of 4 allowed breakpoints (system + tools use 2 already).
        # Guard: only add for conversations with prior history.
        # Anthropic silently skips caching if the prefix is < 1024 tokens.
        if len(claude_messages) >= 3:
            for i in range(len(claude_messages) - 1, -1, -1):
                if claude_messages[i]["role"] == "assistant":
                    msg_content = claude_messages[i].get("content", "")
                    if isinstance(msg_content, str):
                        claude_messages[i]["content"] = [
                            {"type": "text", "text": msg_content, "cache_control": {"type": "ephemeral"}}
                        ]
                    elif isinstance(msg_content, list) and msg_content:
                        # Only text blocks accept cache_control; tool_use blocks do not.
                        for block in reversed(msg_content):
                            if isinstance(block, dict) and block.get("type") == "text":
                                block["cache_control"] = {"type": "ephemeral"}
                                break
                    break

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



    def _build_api_request(self, request: CompletionRequest, model_config) -> Dict[str, Any]:
        """Build the Anthropic API request dict shared by complete() and stream_complete().

        Handles: message conversion, context window headroom, temperature/top_p
        mutual exclusivity, system message, stop sequences, tools, and output_config.
        """
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

        api_request: Dict[str, Any] = {
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
            api_request["system"] = [
                {
                    "type": "text",
                    "text": system_message,
                    "cache_control": {"type": "ephemeral"},
                }
            ]

        if request.stop:
            api_request["stop_sequences"] = request.stop

        # Handle functions/tools
        if request.functions and model_config.supports_functions:
            tools = self._convert_functions_to_tools(request.functions)
            if tools:
                tools[-1]["cache_control"] = {"type": "ephemeral"}
            api_request["tools"] = tools

            # Handle function calling / tool_choice
            if request.function_call:
                if isinstance(request.function_call, str):
                    if request.function_call == "auto":
                        api_request["tool_choice"] = {"type": "auto"}
                    elif request.function_call == "any":
                        # Force model to use at least one tool
                        api_request["tool_choice"] = {"type": "any"}
                    elif request.function_call == "none":
                        api_request["tool_choice"] = {"type": "none"}
                elif isinstance(request.function_call, dict):
                    api_request["tool_choice"] = {
                        "type": "tool",
                        "name": request.function_call.get("name"),
                    }

        # Structured outputs: inject output_config for constrained JSON decoding.
        # SDK <0.42 doesn't have native output_config param; pass via extra_body.
        if request.output_config:
            api_request["extra_body"] = {"output_config": request.output_config}
            schema_keys = list(request.output_config.get("format", {}).get("schema", {}).get("properties", {}).keys())
            logger.info(f"Anthropic structured output enabled: schema keys={schema_keys}")

        # Extended thinking: force temperature=1 and pass config via extra_body
        # (SDK 0.40.0 doesn't accept 'thinking' as a named kwarg in stream())
        if request.thinking:
            api_request["temperature"] = 1
            api_request.pop("top_p", None)
            extra = api_request.get("extra_body", {})
            extra["thinking"] = request.thinking
            api_request["extra_body"] = extra

        return api_request

    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        """Generate a completion using Anthropic API"""

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model = model_config.model_id

        api_request = self._build_api_request(request, model_config)

        # Make API call
        start_time = time.time()

        try:
            create_kwargs = dict(api_request)
            if request.thinking:
                create_kwargs["extra_headers"] = {"anthropic-beta": "interleaved-thinking-2025-05-14"}
            response = await self.client.messages.create(**create_kwargs)
        except anthropic.APIError as e:
            raise Exception(f"Anthropic API error: {e}")

        latency_ms = int((time.time() - start_time) * 1000)

        # Extract response content
        content = ""
        function_calls = []

        for content_block in response.content:
            if content_block.type == "text":
                content = content_block.text
            elif content_block.type == "thinking":
                pass  # Consumed but not relayed to client in v1
            elif content_block.type == "tool_use":
                function_calls.append({
                    "id": content_block.id,
                    "name": content_block.name,
                    "arguments": content_block.input,
                })

        function_call = function_calls[0] if function_calls else None

        # Get token usage
        output_tokens = response.usage.output_tokens
        total_tokens = response.usage.input_tokens + output_tokens
        cache_read = getattr(response.usage, "cache_read_input_tokens", 0) or 0
        cache_creation = getattr(response.usage, "cache_creation_input_tokens", 0) or 0
        if cache_read > 0 or cache_creation > 0:
            logger.info(f"Anthropic prompt cache: read={cache_read}, creation={cache_creation}, input={response.usage.input_tokens}")
        logger.info(f"Anthropic complete: model={model}, structured_output={bool(request.output_config)}, input={response.usage.input_tokens}, output={output_tokens}")

        # Calculate cost (including prompt cache pricing)
        cost = self.estimate_cost(
            response.usage.input_tokens, output_tokens, model,
            cache_read_tokens=cache_read, cache_creation_tokens=cache_creation,
        )

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
                cache_read_tokens=cache_read,
                cache_creation_tokens=cache_creation,
            ),
            finish_reason=response.stop_reason or "stop",
            function_call=function_call,
            tool_calls=function_calls if function_calls else None,
            request_id=response.id,
            latency_ms=latency_ms,
        )

    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator[str]:
        """Stream a completion using Anthropic API"""

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model = model_config.model_id

        api_request = self._build_api_request(request, model_config)

        # Make streaming API call
        try:
            stream_kwargs = dict(api_request)
            if request.thinking:
                stream_kwargs["extra_headers"] = {"anthropic-beta": "interleaved-thinking-2025-05-14"}
            async with self.client.messages.stream(**stream_kwargs) as stream:
                async for text in stream.text_stream:
                    yield text

                # After streaming completes, get the final message with usage and tool calls
                final_message = await stream.get_final_message()

                # Check for tool use in the final message
                function_calls = []
                if final_message and hasattr(final_message, "content"):
                    for content_block in final_message.content:
                        if hasattr(content_block, "type") and content_block.type == "tool_use":
                            function_calls.append({
                                "id": content_block.id,
                                "name": content_block.name,
                                "arguments": content_block.input,
                            })

                if final_message and hasattr(final_message, "usage"):
                    cache_read = getattr(final_message.usage, "cache_read_input_tokens", 0) or 0
                    cache_creation = getattr(final_message.usage, "cache_creation_input_tokens", 0) or 0
                    cost = self.estimate_cost(
                        final_message.usage.input_tokens,
                        final_message.usage.output_tokens,
                        model,
                        cache_read_tokens=cache_read,
                        cache_creation_tokens=cache_creation,
                    )
                    result = {
                        "usage": {
                            "total_tokens": final_message.usage.input_tokens + final_message.usage.output_tokens,
                            "input_tokens": final_message.usage.input_tokens,
                            "output_tokens": final_message.usage.output_tokens,
                            "cache_read_tokens": cache_read,
                            "cache_creation_tokens": cache_creation,
                            "cost_usd": cost,
                        },
                        "model": final_message.model,
                        "provider": "anthropic",
                        "finish_reason": final_message.stop_reason or "stop",
                    }
                    if function_calls:
                        result["function_call"] = function_calls[0]
                        result["function_calls"] = function_calls
                    yield result

        except anthropic.APIError as e:
            raise Exception(f"Anthropic API error: {e}")
