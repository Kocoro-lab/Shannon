"""
Anthropic Claude Provider Implementation
"""

import hashlib
import json
import os
import logging
import time
from typing import Dict, List, Any, AsyncIterator, Optional
import anthropic
from anthropic import AsyncAnthropic

from .base import (
    LLMProvider,
    CompletionRequest,
    CompletionResponse,
    TokenUsage,
    TokenCounter,
    extract_text_from_content,
    _ensure_tool_id,
)

logger = logging.getLogger(__name__)

# Optional prompt-cache metrics (per-source observability).
# Labels: provider, model, source. `source` comes from CompletionRequest.cache_source
# so we can see which call sites (agent_loop/decompose/tool_select/synthesis) are
# paying the 2.0x 1h write premium without enough reads to break even.
try:
    from prometheus_client import Counter as _PromCounter

    _CACHE_METRICS_ENABLED = True
    ANTHROPIC_CACHE_READ_TOKENS = _PromCounter(
        "anthropic_cache_read_tokens_total",
        "Anthropic prompt cache read tokens (billed at 0.1x input)",
        labelnames=("provider", "model", "source"),
    )
    ANTHROPIC_CACHE_WRITE_5M_TOKENS = _PromCounter(
        "anthropic_cache_write_5m_tokens_total",
        "Anthropic prompt cache write tokens at 5min TTL (billed at 1.25x input)",
        labelnames=("provider", "model", "source"),
    )
    ANTHROPIC_CACHE_WRITE_1H_TOKENS = _PromCounter(
        "anthropic_cache_write_1h_tokens_total",
        "Anthropic prompt cache write tokens at 1h TTL (billed at 2.0x input)",
        labelnames=("provider", "model", "source"),
    )
except Exception:
    _CACHE_METRICS_ENABLED = False


def _record_cache_metrics(
    provider: str,
    model: str,
    source: Optional[str],
    cache_read: int,
    cache_creation: int,
    cache_creation_1h: int,
) -> None:
    """Emit per-source cache metrics. Cheap no-op if prometheus is unavailable."""
    if not _CACHE_METRICS_ENABLED:
        return
    src = source or "unknown"
    try:
        if cache_read:
            ANTHROPIC_CACHE_READ_TOKENS.labels(provider, model, src).inc(cache_read)
        cache_5m = max(0, cache_creation - cache_creation_1h)
        if cache_5m:
            ANTHROPIC_CACHE_WRITE_5M_TOKENS.labels(provider, model, src).inc(cache_5m)
        if cache_creation_1h:
            ANTHROPIC_CACHE_WRITE_1H_TOKENS.labels(provider, model, src).inc(cache_creation_1h)
    except Exception:
        # Metrics must never break the request path
        pass


CACHE_BREAK_MARKER = "<!-- cache_break -->"
VOLATILE_MARKER = "<!-- volatile -->"

# Anthropic prompt cache TTL. 1h reduces re-creation cost for long-running
# workflows (swarm 10-30min, research 5-15min). Write premium is 2x (vs 1.25x
# for 5min) but amortized over 12x longer TTL — net positive from ~3 calls.
# Ordering: system(1h) ≥ tools(1h) ≥ messages(1h) — monotonic non-increasing ✓
CACHE_TTL_LONG = {"type": "ephemeral", "ttl": "1h"}
CACHE_TTL_SHORT = {"type": "ephemeral"}


class CacheBreakDetector:
    """Tracks API request state across calls to detect cache-breaking changes.

    Compares system prompt hash, tool name set, and model ID between sequential
    calls. When a change is detected, returns a dict describing what changed.
    Designed for per-provider-instance use (one detector per AnthropicProvider).
    """

    def __init__(self):
        self._prev_system_hash: str = ""
        self._prev_tool_names: list[str] = []
        self._prev_model: str = ""
        self.call_count: int = 0

    def check(
        self,
        system_text: str,
        tool_names: list[str],
        model: str,
    ) -> Optional[dict]:
        """Compare current state against previous. Returns None if no break.

        Returns dict with keys: changed (list[str]), tools_added, tools_removed,
        prev_model, new_model, system_char_delta.
        """
        self.call_count += 1

        sys_hash = hashlib.md5(system_text.encode()).hexdigest()[:12]
        sorted_names = sorted(tool_names)

        if not self._prev_system_hash:
            # First call — record state, no comparison possible
            self._prev_system_hash = sys_hash
            self._prev_tool_names = sorted_names
            self._prev_model = model
            return None

        changed = []
        result = {}

        if sys_hash != self._prev_system_hash:
            changed.append("system")
            result["system_char_delta"] = len(system_text)

        if sorted_names != self._prev_tool_names:
            changed.append("tools")
            prev_set = set(self._prev_tool_names)
            curr_set = set(sorted_names)
            result["tools_added"] = sorted(curr_set - prev_set)
            result["tools_removed"] = sorted(prev_set - curr_set)

        if model != self._prev_model:
            changed.append("model")
            result["prev_model"] = self._prev_model
            result["new_model"] = model

        # Update state
        self._prev_system_hash = sys_hash
        self._prev_tool_names = sorted_names
        self._prev_model = model

        if changed:
            result["changed"] = changed
            result["call_count"] = self.call_count
            return result
        return None


class AnthropicProvider(LLMProvider):
    """Anthropic Claude API provider implementation"""

    def __init__(self, config: Dict[str, Any]):
        # Initialize Anthropic client
        api_key = config.get("api_key") or os.getenv("ANTHROPIC_API_KEY")
        if not api_key:
            raise ValueError("Anthropic API key not provided")

        # Support custom base_url for Anthropic-compatible providers (e.g. MiniMax)
        base_url = config.get("base_url")
        self.client = AsyncAnthropic(api_key=api_key, base_url=base_url)

        # Tool schema freeze: cache converted tool schemas by name set to prevent
        # description-drift cache breaks. Legitimate schema changes take effect on
        # process restart (acceptable for Docker deployment model).
        self._frozen_tools: list = []
        self._frozen_tools_key: str = ""
        self._cache_break_detector = CacheBreakDetector()

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
                    raw_stable = parts[0]
                    volatile = parts[1]
                    if raw_stable.strip():
                        claude_messages.append({
                            "role": "user",
                            "content": [
                                {"type": "text", "text": raw_stable, "cache_control": CACHE_TTL_LONG},
                                {"type": "text", "text": volatile},
                            ]
                        })
                    else:
                        # No stable prefix — skip cache_control to avoid Anthropic 400
                        # on empty text blocks (happens when ShanClaw has no sticky context)
                        claude_messages.append({"role": "user", "content": volatile})
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
                        tc_id = _ensure_tool_id(tc.get("id", ""))
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
                    "tool_use_id": _ensure_tool_id(message.get("tool_call_id", "")),
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

        # NOTE: Previously added cache_control to last assistant message (explicit
        # breakpoint). Removed because the breakpoint MOVES each iteration, causing
        # new cache creation every turn and zero cache reads. Caching is now handled
        # by explicit system message breakpoint (line ~238) only.

        return system_message, claude_messages

    def _split_system_message(self, system_message: str) -> list[dict]:
        """Split system message at <!-- volatile --> marker.

        Returns a list of content blocks for the Anthropic API 'system' parameter.
        The stable prefix (before marker) gets cache_control; volatile suffix does not.
        If no marker is present, returns a single cached block (backward compatible).
        """
        if VOLATILE_MARKER in system_message:
            stable, volatile = system_message.split(VOLATILE_MARKER, 1)
            # 1h TTL: system(1h) ≥ tools(1h) ≥ messages(1h) — monotonic ✓
            # (SDK 0.64.0 required for 1h TTL support)
            blocks = []
            if stable.strip():
                blocks.append({"type": "text", "text": stable.strip(), "cache_control": CACHE_TTL_LONG})
            if volatile.strip():
                blocks.append({"type": "text", "text": volatile.strip()})
            if blocks:
                return blocks
        return [{"type": "text", "text": system_message, "cache_control": CACHE_TTL_LONG}]

    def _convert_functions_to_tools(self, functions: List[Dict]) -> List[Dict]:
        """Convert OpenAI function format to Claude tools format.

        Frozen by tool name set: same names → return cached schemas (prevents
        description-drift cache breaks). Different name set → rebuild.
        """
        # Cache key: sorted tool names
        key = str(sorted(
            f.get("name") or (f.get("function") or {}).get("name", "")
            for f in functions
        ))

        if self._frozen_tools and self._frozen_tools_key == key:
            return [dict(t) for t in self._frozen_tools]

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
        # Sort by name for cache prefix stability across requests
        tools.sort(key=lambda t: t["name"])

        self._frozen_tools = tools
        self._frozen_tools_key = key
        return [dict(t) for t in tools]

    def reset_tool_cache(self):
        """Clear frozen tool schemas. Call on explicit tool set change."""
        self._frozen_tools = []
        self._frozen_tools_key = ""



    def _convert_attachments_for_anthropic(self, messages: List[Dict]) -> List[Dict]:
        """Convert shannon_attachment blocks in user messages to Anthropic-native format.

        shannon_attachment is a Shannon-internal marker that must never be sent raw
        to any LLM API. This converts them to Anthropic image/document blocks.
        """
        for msg in messages:
            content = msg.get("content")
            if isinstance(content, list):
                msg["content"] = [
                    self._shannon_att_to_anthropic(b)
                    if isinstance(b, dict) and b.get("type") == "shannon_attachment"
                    else b
                    for b in content
                ]
        return messages

    @staticmethod
    def _shannon_att_to_anthropic(block: Dict) -> Dict:
        """Convert a single shannon_attachment block to Anthropic format."""
        media_type = block["media_type"]
        # URL-based: route by media type
        if block.get("source") == "url":
            if media_type == "application/pdf":
                # Anthropic doesn't support URL-based PDF documents; degrade to text
                return {"type": "text", "text": f"[PDF document: {block['url']}]"}
            return {
                "type": "image",
                "source": {"type": "url", "url": block["url"]},
            }
        if media_type == "application/pdf":
            return {
                "type": "document",
                "source": {
                    "type": "base64",
                    "media_type": media_type,
                    "data": block["data"],
                },
            }
        if not media_type.startswith("image/"):
            # Unsupported binary: degrade to text
            fname = block.get("filename", "file")
            return {"type": "text", "text": f"[Unsupported attachment: {fname} ({media_type})]"}
        return {
            "type": "image",
            "source": {
                "type": "base64",
                "media_type": media_type,
                "data": block["data"],
            },
        }

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

        # Convert shannon_attachment blocks to Anthropic-native format
        claude_messages = self._convert_attachments_for_anthropic(claude_messages)

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

        # SDK 0.64.0+ rejects non-streaming requests that may take >10 minutes.
        # Cap max_tokens to avoid this for non-streaming complete() calls.
        # Haiku ~800 tok/s → 8192 tokens ≈ 10s. 16384 is safe for all models.
        MAX_NON_STREAMING = 16384
        if adjusted_max > MAX_NON_STREAMING and not request.stream:
            logger.info(f"Capping max_tokens {adjusted_max} → {MAX_NON_STREAMING} for non-streaming Anthropic call")
            adjusted_max = MAX_NON_STREAMING

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
            api_request["system"] = self._split_system_message(system_message)

        if request.stop:
            api_request["stop_sequences"] = request.stop

        # Handle functions/tools
        if request.functions and model_config.supports_functions:
            tools = self._convert_functions_to_tools(request.functions)
            if tools:
                tools[-1]["cache_control"] = CACHE_TTL_LONG
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

        # NOTE: Automatic caching (top-level cache_control) tested extensively but
        # NET NEGATIVE for swarm: 25% write premium on every call, but parallel agents
        # constantly change shared state (team roster, task board, workspace) → prefix
        # breaks between nearly every call → ~7% hit rate → net -17% cost increase.
        # Explicit system message cache_control (line 190) is sufficient: Sonnet/Lead
        # (>1024 threshold, ~8K stable system prompt) benefits; Haiku agents (<4096
        # threshold) silently skip without paying write premium.

        # Structured outputs: inject output_config for constrained JSON decoding.
        # SDK <0.42 doesn't have native output_config param; pass via extra_body.
        if request.output_config:
            api_request["extra_body"] = {"output_config": request.output_config}
            schema_keys = list(request.output_config.get("format", {}).get("schema", {}).get("properties", {}).keys())
            logger.info(f"Anthropic structured output enabled: schema keys={schema_keys}")

        # Extended thinking: force temperature=1 and pass config via extra_body
        # (SDK 0.40.0 doesn't accept 'thinking' as a named kwarg in stream())
        if request.thinking:
            thinking_config = dict(request.thinking)
            # Adaptive thinking only supported on Opus models.
            # For other models, convert adaptive → enabled with a budget.
            if thinking_config.get("type") == "adaptive" and "opus" not in model.lower():
                thinking_config = {"type": "enabled", "budget_tokens": thinking_config.get("budget_tokens", 10000)}
                logger.info(f"Converted adaptive thinking to enabled (budget=10000) for model {model}")
            api_request["temperature"] = 1
            api_request.pop("top_p", None)
            extra = api_request.get("extra_body", {})
            extra["thinking"] = thinking_config
            api_request["extra_body"] = extra

        # Cache break detection
        system_text = system_message or ""
        tool_names = [t["name"] for t in api_request.get("tools", [])]
        break_info = self._cache_break_detector.check(
            system_text=system_text,
            tool_names=tool_names,
            model=model,
        )
        if break_info:
            parts = [f"Cache break detected (call #{break_info['call_count']}): changed={break_info['changed']}"]
            if "tools_added" in break_info:
                parts.append(f"tools_added={break_info['tools_added']}")
            if "tools_removed" in break_info:
                parts.append(f"tools_removed={break_info['tools_removed']}")
            if "model" in break_info.get("changed", []):
                parts.append(f"model={break_info.get('prev_model', '')}→{break_info.get('new_model', '')}")
            logger.warning(" ".join(parts))

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
            # Handle both Anthropic SDK objects (.type attr) and plain dicts (MiniMax compat)
            block_type = getattr(content_block, "type", None) or (content_block.get("type") if isinstance(content_block, dict) else None)
            if block_type == "text":
                content = getattr(content_block, "text", None) or (content_block.get("text", "") if isinstance(content_block, dict) else "")
            elif block_type == "thinking":
                pass  # Consumed but not relayed to client in v1
            elif block_type == "tool_use":
                block_id = getattr(content_block, "id", None) or (content_block.get("id") if isinstance(content_block, dict) else None)
                block_name = getattr(content_block, "name", None) or (content_block.get("name") if isinstance(content_block, dict) else None)
                block_input = getattr(content_block, "input", None) or (content_block.get("input") if isinstance(content_block, dict) else None)
                function_calls.append({
                    "id": block_id,
                    "name": block_name,
                    "arguments": block_input,
                })

        function_call = function_calls[0] if function_calls else None

        # Get token usage — handle both SDK objects and dicts (MiniMax compat)
        usage = response.usage
        if isinstance(usage, dict):
            output_tokens = usage.get("output_tokens", 0)
            input_tokens = usage.get("input_tokens", 0)
            cache_read = usage.get("cache_read_input_tokens", 0) or 0
            cache_creation = usage.get("cache_creation_input_tokens", 0) or 0
        else:
            output_tokens = usage.output_tokens
            input_tokens = usage.input_tokens
            cache_read = getattr(usage, "cache_read_input_tokens", 0) or 0
            cache_creation = getattr(usage, "cache_creation_input_tokens", 0) or 0
        # Extract per-TTL cache creation breakdown (1h vs 5min)
        cache_creation_1h = 0
        if isinstance(usage, dict):
            cc = usage.get("cache_creation")
            if isinstance(cc, dict):
                cache_creation_1h = cc.get("ephemeral_1h_input_tokens", 0) or 0
        else:
            cc = getattr(usage, "cache_creation", None)
            if cc is not None:
                cache_creation_1h = getattr(cc, "ephemeral_1h_input_tokens", 0) or 0

        total_tokens = input_tokens + output_tokens
        if cache_read > 0 or cache_creation > 0:
            logger.info(f"Anthropic prompt cache: read={cache_read}, creation={cache_creation} (1h={cache_creation_1h}), input={input_tokens}")
        logger.info(f"Anthropic complete: model={model}, structured_output={bool(request.output_config)}, input={input_tokens}, output={output_tokens}")

        # Calculate cost (including prompt cache pricing)
        cost = self.estimate_cost(
            input_tokens, output_tokens, model,
            cache_read_tokens=cache_read, cache_creation_tokens=cache_creation,
            cache_creation_1h_tokens=cache_creation_1h,
        )

        _record_cache_metrics(
            self.config.get("name", "anthropic"),
            model,
            request.cache_source,
            cache_read,
            cache_creation,
            cache_creation_1h,
        )

        # Build response
        return CompletionResponse(
            content=content,
            model=model,
            provider=self.config.get("name", "anthropic"),
            usage=TokenUsage(
                input_tokens=input_tokens,
                output_tokens=output_tokens,
                total_tokens=total_tokens,
                estimated_cost=cost,
                cache_read_tokens=cache_read,
                cache_creation_tokens=cache_creation,
                call_sequence=self._cache_break_detector.call_count,
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

                # Check for tool use in the final message. Parity with the
                # non-stream complete() path: handle both SDK objects and dicts
                # so Anthropic-compatible providers (e.g. MiniMax) don't drop
                # tool calls that arrive as dict-shaped content blocks.
                function_calls = []
                if final_message and hasattr(final_message, "content"):
                    for content_block in final_message.content:
                        block_type = getattr(content_block, "type", None) or (
                            content_block.get("type") if isinstance(content_block, dict) else None
                        )
                        if block_type != "tool_use":
                            continue
                        block_id = getattr(content_block, "id", None) or (
                            content_block.get("id") if isinstance(content_block, dict) else None
                        )
                        block_name = getattr(content_block, "name", None) or (
                            content_block.get("name") if isinstance(content_block, dict) else None
                        )
                        block_input = getattr(content_block, "input", None) or (
                            content_block.get("input") if isinstance(content_block, dict) else None
                        )
                        function_calls.append({
                            "id": block_id,
                            "name": block_name,
                            "arguments": block_input,
                        })

                if final_message and hasattr(final_message, "usage"):
                    # Handle both SDK objects and dicts (MiniMax / Anthropic-compat
                    # provider parity with the non-stream complete() path above).
                    usage = final_message.usage
                    if isinstance(usage, dict):
                        input_tokens = usage.get("input_tokens", 0) or 0
                        output_tokens = usage.get("output_tokens", 0) or 0
                        cache_read = usage.get("cache_read_input_tokens", 0) or 0
                        cache_creation = usage.get("cache_creation_input_tokens", 0) or 0
                        cc = usage.get("cache_creation")
                        cache_creation_1h = (
                            cc.get("ephemeral_1h_input_tokens", 0) or 0
                            if isinstance(cc, dict) else 0
                        )
                    else:
                        input_tokens = usage.input_tokens
                        output_tokens = usage.output_tokens
                        cache_read = getattr(usage, "cache_read_input_tokens", 0) or 0
                        cache_creation = getattr(usage, "cache_creation_input_tokens", 0) or 0
                        # Per-TTL cache creation breakdown (1h vs 5min). Without
                        # this the 1h TTL slice silently drops from cost accounting.
                        cache_creation_1h = 0
                        cc = getattr(usage, "cache_creation", None)
                        if cc is not None:
                            cache_creation_1h = getattr(cc, "ephemeral_1h_input_tokens", 0) or 0
                    cost = self.estimate_cost(
                        input_tokens,
                        output_tokens,
                        model,
                        cache_read_tokens=cache_read,
                        cache_creation_tokens=cache_creation,
                        cache_creation_1h_tokens=cache_creation_1h,
                    )
                    _record_cache_metrics(
                        self.config.get("name", "anthropic"),
                        model,
                        request.cache_source,
                        cache_read,
                        cache_creation,
                        cache_creation_1h,
                    )
                    result = {
                        "usage": {
                            "total_tokens": input_tokens + output_tokens,
                            "input_tokens": input_tokens,
                            "output_tokens": output_tokens,
                            "cache_read_tokens": cache_read,
                            "cache_creation_tokens": cache_creation,
                            "cache_creation_1h_tokens": cache_creation_1h,
                            "cost_usd": cost,
                            "call_sequence": self._cache_break_detector.call_count,
                        },
                        "model": final_message.model,
                        "provider": self.config.get("name", "anthropic"),
                        "finish_reason": final_message.stop_reason or "stop",
                    }
                    if function_calls:
                        result["function_call"] = function_calls[0]
                        result["function_calls"] = function_calls
                    yield result

        except anthropic.APIError as e:
            raise Exception(f"Anthropic API error: {e}")
