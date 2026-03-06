"""
OpenAI Provider Implementation
Adds support for OpenAI Responses API with fallback to Chat Completions.
Prefers provider-reported token usage; falls back to estimation only if needed.
"""

import os
from typing import Dict, List, Any, AsyncIterator
import openai
from openai import AsyncOpenAI
from tenacity import retry, stop_after_attempt, wait_exponential
import tiktoken

from .base import LLMProvider, CompletionRequest, CompletionResponse, TokenUsage


class OpenAIProvider(LLMProvider):
    """OpenAI API provider implementation"""

    def __init__(self, config: Dict[str, Any]):
        # Initialize OpenAI client
        api_key = config.get("api_key") or os.getenv("OPENAI_API_KEY")
        if not api_key:
            raise ValueError("OpenAI API key not provided")

        self.organization = config.get("organization")
        timeout = int(config.get("timeout", 60) or 60)

        # Pass organization and timeout at construction time
        self.client = AsyncOpenAI(
            api_key=api_key,
            organization=self.organization,
            timeout=timeout,
        )

        # Token encoders for different models
        self.encoders = {}

        super().__init__(config)

    def _resolve_alias(self, model_id: str) -> str:
        """Return the configured alias for a given vendor model_id, if any."""
        for alias, cfg in self.models.items():
            if getattr(cfg, "model_id", None) == model_id:
                return alias
        return model_id

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

    @retry(
        stop=stop_after_attempt(3), wait=wait_exponential(multiplier=0.5, min=1, max=8)
    )
    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        """Generate a completion using OpenAI API (Responses API preferred)."""
        import logging
        logger = logging.getLogger(__name__)

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model = model_config.model_id

        # Choose API route based on model capabilities and request intent
        route = self._choose_api_route(request, model_config)
        if route == "responses":
            if hasattr(self.client, "responses") and hasattr(
                self.client.responses, "create"
            ):
                try:
                    return await self._complete_responses_api(request, model)
                except Exception as e:
                    # Log the actual error from Responses API
                    logger.warning(
                        f"Responses API failed for model {model}, falling back to Chat Completions API. "
                        f"Error: {type(e).__name__}: {str(e)}"
                    )
                    # Fall through to Chat Completions API
            # If Responses not available, fall through to chat

        # Fallback: Chat Completions API
        import time

        api_request = {
            "model": model,
            "messages": request.messages,
        }

        # GPT-5 family (excluding gpt-5-pro) has restricted parameter support
        is_gpt5_chat = model.startswith("gpt-5") and not model.startswith("gpt-5-pro")

        # Only include sampling parameters if NOT GPT-5 chat models
        # GPT-5 chat models only support default values (temperature=1, etc)
        if not is_gpt5_chat:
            api_request["temperature"] = request.temperature
            api_request["top_p"] = request.top_p
            api_request["frequency_penalty"] = request.frequency_penalty
            api_request["presence_penalty"] = request.presence_penalty

        # Compute a safe max completion tokens based on context window headroom
        # This prevents requesting more tokens than the model can return given the prompt size
        prompt_tokens_est = self.count_tokens(request.messages, model)
        # Reserve a small safety margin for tool metadata or post-processing
        safety_margin = 256
        # Model-configured maxima
        model_context = getattr(model_config, "context_window", 8192)
        model_max_output = getattr(model_config, "max_tokens", model_context)
        # Requested maximum, if provided
        requested_max = int(request.max_tokens) if request.max_tokens else model_max_output
        # Available headroom for completion
        headroom = max(0, model_context - prompt_tokens_est - safety_margin)
        adjusted_max = max(1, min(requested_max, model_max_output, headroom))

        # Debug: Log the calculation for GPT-5
        if model.startswith("gpt-5"):
            logger.info(
                f"Token limit calculation for {model}: "
                f"request.max_tokens={request.max_tokens}, model_max_output={model_max_output}, "
                f"requested_max={requested_max}, headroom={headroom}, adjusted_max={adjusted_max}"
            )

        # Debug logging for token limits
        if adjusted_max < 100:
            logger.warning(
                f"Very low max_completion_tokens for model {model}: adjusted_max={adjusted_max}, "
                f"prompt_tokens={prompt_tokens_est}, model_context={model_context}, "
                f"requested_max={requested_max}, headroom={headroom}"
            )

        # GPT-5 family uses max_completion_tokens instead of max_tokens
        if adjusted_max:
            if is_gpt5_chat:
                api_request["max_completion_tokens"] = adjusted_max
                logger.info(
                    f"GPT-5 Chat API request for {model}: max_completion_tokens={adjusted_max}, "
                    f"prompt_tokens_est={prompt_tokens_est}, context_window={model_context}"
                )
            else:
                api_request["max_tokens"] = adjusted_max

        if request.stop:
            api_request["stop"] = request.stop
        if request.functions:
            api_request["functions"] = request.functions
            # Translate function_call to tool_choice for OpenAI API
            if request.function_call:
                if request.function_call == "any":
                    api_request["tool_choice"] = "required"  # Force tool use
                elif request.function_call == "auto":
                    api_request["tool_choice"] = "auto"
                elif request.function_call == "none":
                    api_request["tool_choice"] = "none"
                else:
                    # Specific function name
                    api_request["tool_choice"] = {"type": "function", "function": {"name": request.function_call}}
        if request.seed is not None:
            api_request["seed"] = request.seed
        if request.response_format:
            api_request["response_format"] = request.response_format
        if request.user:
            api_request["user"] = request.user

        start_time = time.time()
        try:
            response = await self.client.chat.completions.create(**api_request)
        except openai.APIError as e:
            logger.error(
                f"OpenAI Chat Completions API error for model {model}: "
                f"Status={getattr(e, 'status_code', 'unknown')}, "
                f"Type={type(e).__name__}, "
                f"Message={str(e)}"
            )
            raise Exception(f"OpenAI Chat Completions API error: {e}") from e
        except Exception as e:
            logger.error(
                f"Unexpected error calling Chat Completions API for model {model}: "
                f"Type={type(e).__name__}, "
                f"Message={str(e)}"
            )
            raise

        latency_ms = int((time.time() - start_time) * 1000)

        choice = response.choices[0]
        message = choice.message

        # Debug: Log raw message structure for GPT-5 models
        if model.startswith("gpt-5"):
            raw_content = getattr(message, "content", None)
            # Get all message attributes to see what's available
            msg_attrs = [attr for attr in dir(message) if not attr.startswith('_')]
            logger.info(
                f"GPT-5 message attributes: {msg_attrs}"
            )
            logger.info(
                f"GPT-5 raw message.content: type={type(raw_content)}, "
                f"len={len(raw_content) if isinstance(raw_content, str) else 'N/A'}, "
                f"is_empty_string={raw_content == ''}, "
                f"value_preview={raw_content[:200] if raw_content else '(empty or None)'}"
            )
            # Check for alternative content fields
            for alt_field in ['reasoning_content', 'thinking', 'internal_thoughts', 'output', 'text']:
                if hasattr(message, alt_field):
                    alt_value = getattr(message, alt_field, None)
                    logger.info(f"GPT-5 message.{alt_field}: {type(alt_value)}, preview: {str(alt_value)[:100]}")

        # Normalize message content: some models (e.g., GPT‑5/4.1) may return
        # content as a list of parts instead of a plain string. Extract the
        # text segments to avoid returning an empty string with non‑zero tokens.
        def _extract_text_from_message(msg) -> str:
            try:
                content = getattr(msg, "content", None)

                # 1. Plain string content
                if isinstance(content, str) and content.strip():
                    return content

                # 2. List of content parts (each may have a .text attribute or be a dict)
                if isinstance(content, list):
                    parts: List[str] = []
                    for part in content:
                        try:
                            # Try part.text attribute
                            text = getattr(part, "text", None)
                            # Try part["text"] dict key
                            if not text and isinstance(part, dict):
                                text = part.get("text")
                            # Try part["output_text"] for GPT-5
                            if not text and isinstance(part, dict):
                                text = part.get("output_text")
                            if isinstance(text, str) and text.strip():
                                parts.append(text.strip())
                        except Exception:
                            # Be permissive; ignore malformed parts
                            pass
                    if parts:
                        return "\n\n".join(parts).strip()

                # 3. Some SDK variants expose a single object with .text
                if hasattr(content, "text"):
                    txt = getattr(content, "text", "")
                    if txt:
                        return txt

                # 4. GPT-5 fallback: try model_dump() to inspect structured content
                if hasattr(msg, "model_dump"):
                    try:
                        dump = msg.model_dump()
                        # Check if content is a list in the dump
                        if isinstance(dump.get("content"), list):
                            parts: List[str] = []
                            for item in dump["content"]:
                                if isinstance(item, dict):
                                    # Try output_text, text, or any text field
                                    for field in ["output_text", "text", "content"]:
                                        if field in item and isinstance(item[field], str) and item[field].strip():
                                            parts.append(item[field].strip())
                                            break
                            if parts:
                                logger.info(f"Extracted content from model_dump() list parts: {len(parts)} parts")
                                return "\n\n".join(parts).strip()
                    except Exception as e:
                        logger.warning(f"Failed to extract from model_dump(): {e}")

                # 5. GPT-5 reasoning fields
                for alt_field in ["reasoning_content", "output", "thinking"]:
                    if hasattr(msg, alt_field):
                        alt_value = getattr(msg, alt_field, None)
                        if isinstance(alt_value, str) and alt_value.strip():
                            logger.info(f"Extracted content from message.{alt_field}")
                            return alt_value
                        elif isinstance(alt_value, list):
                            # Handle list of reasoning parts
                            parts: List[str] = []
                            for part in alt_value:
                                if isinstance(part, dict) and "text" in part:
                                    parts.append(str(part["text"]))
                                elif isinstance(part, str):
                                    parts.append(part)
                            if parts:
                                logger.info(f"Extracted content from message.{alt_field} list")
                                return "\n\n".join(parts).strip()

            except Exception as e:
                logger.warning(f"Content extraction failed: {e}")

            return ""

        content_text = _extract_text_from_message(message)

        # Special case: finish_reason == "function_call" or "tool_calls"
        # Content is in message.tool_calls array (new format) or message.function_call (old format)
        if not content_text or not content_text.strip():
            if choice.finish_reason in ["function_call", "tool_calls"]:
                # Try new format first (tool_calls array)
                tool_calls = getattr(message, "tool_calls", None)
                if tool_calls and len(tool_calls) > 0:
                    tool_descriptions = []
                    for tc in tool_calls:
                        try:
                            func_name = getattr(tc.function, "name", "unknown")
                            func_args = getattr(tc.function, "arguments", "{}")
                            tool_descriptions.append(f"Tool: {func_name}, Args: {func_args}")
                        except Exception:
                            pass
                    if tool_descriptions:
                        content_text = "Tool calls:\n" + "\n".join(tool_descriptions)
                        logger.info(f"Extracted {len(tool_calls)} tool calls from message.tool_calls for finish_reason={choice.finish_reason}")

                # Try old format if new format didn't work (function_call object)
                if not content_text or not content_text.strip():
                    function_call = getattr(message, "function_call", None)
                    if function_call:
                        try:
                            func_name = getattr(function_call, "name", "unknown")
                            func_args = getattr(function_call, "arguments", "{}")
                            content_text = f"Tool call: {func_name}, Args: {func_args}"
                            logger.info(f"Extracted function call from message.function_call (old format) for finish_reason={choice.finish_reason}")
                        except Exception:
                            pass

        # Guard: If content is still empty but completion_tokens > 0, handle gracefully
        completion_tokens_actual = int(getattr(response.usage, "completion_tokens", 0))
        if (not content_text or not content_text.strip()) and completion_tokens_actual > 0:
            # Special case: finish_reason == "length" means model hit token limit
            # GPT-5 reasoning models may consume all tokens for internal reasoning
            if choice.finish_reason == "length":
                logger.warning(
                    f"Model {model} hit token limit with empty content. "
                    f"finish_reason: length, "
                    f"prompt_tokens: {getattr(response.usage, 'prompt_tokens', 'N/A')}, "
                    f"completion_tokens: {completion_tokens_actual}. "
                    f"Returning partial content message."
                )
                # Return a partial content message instead of raising an exception
                content_text = (
                    f"[Incomplete response: Model hit token limit ({completion_tokens_actual} tokens used). "
                    f"The response was truncated. Consider increasing max_tokens or simplifying the prompt.]"
                )
            else:
                # For other finish_reasons with empty content, this is an error
                logger.error(
                    f"Empty content after all extraction attempts for model {model}. "
                    f"finish_reason: {choice.finish_reason}, "
                    f"prompt_tokens: {getattr(response.usage, 'prompt_tokens', 'N/A')}, "
                    f"completion_tokens: {completion_tokens_actual}"
                )
                # Dump message structure for debugging
                if hasattr(message, "model_dump"):
                    try:
                        dump = message.model_dump()
                        logger.error(f"Message dump: {dump}")
                    except Exception as e:
                        logger.error(f"Failed to dump message: {e}")
                # This is an error condition - content should exist with non-zero tokens
                raise Exception(
                    f"GPT-5 model {model} returned {completion_tokens_actual} completion tokens "
                    f"but content extraction failed. finish_reason={choice.finish_reason}"
                )

        # Debug logging for empty responses (only when completion_tokens == 0 or None content in function_call)
        if not content_text or not content_text.strip():
            logger.warning(
                f"Empty response from Chat Completions API for model {model}. "
                f"message.content type: {type(message.content)}, "
                f"message.content value: {message.content}, "
                f"finish_reason: {choice.finish_reason}, "
                f"prompt_tokens: {getattr(response.usage, 'prompt_tokens', 'N/A')}, "
                f"completion_tokens: {completion_tokens_actual}"
            )

        # Prefer provider usage for tokens
        try:
            prompt_tokens = int(getattr(response.usage, "prompt_tokens", 0))
            completion_tokens = int(getattr(response.usage, "completion_tokens", 0))
            total_tokens = int(
                getattr(
                    response.usage, "total_tokens", prompt_tokens + completion_tokens
                )
            )
        except Exception:
            # Fallback to estimation only if needed
            prompt_tokens = self.count_tokens(request.messages, model)
            completion_tokens = self.count_tokens(
                [{"role": "assistant", "content": content_text}], model
            )
            total_tokens = prompt_tokens + completion_tokens

        # Extract cache metrics (OpenAI automatic caching)
        cache_read = 0
        try:
            details = getattr(response.usage, "prompt_tokens_details", None)
            if details:
                cache_read = int(getattr(details, "cached_tokens", 0) or 0)
        except Exception:
            pass

        # Use configured alias for cost lookup when available
        cost = self.estimate_cost(
            prompt_tokens, completion_tokens, self._resolve_alias(model),
            cache_read_tokens=cache_read,
        )

        # Normalize function/tool call information to a plain dict for JSON safety
        normalized_fc = None
        try:
            # Newer SDKs expose structured tool calls; prefer those when present
            if hasattr(message, "tool_calls") and message.tool_calls:
                # Take the first function tool call for compatibility
                tc = message.tool_calls[0]
                fn = getattr(tc, "function", None)
                if fn is not None:
                    # Pydantic v2 objects have model_dump(); fall back to attrs
                    if hasattr(fn, "model_dump"):
                        data = fn.model_dump()
                        # Ensure arguments is JSON-string or object as returned by SDK
                        normalized_fc = {
                            "name": data.get("name"),
                            "arguments": data.get("arguments"),
                        }
                    else:
                        normalized_fc = {
                            "name": getattr(fn, "name", None),
                            "arguments": getattr(fn, "arguments", None),
                        }
            elif hasattr(message, "function_call") and message.function_call:
                fc = message.function_call
                if hasattr(fc, "model_dump"):
                    data = fc.model_dump()
                    normalized_fc = {
                        "name": data.get("name"),
                        "arguments": data.get("arguments"),
                    }
                else:
                    normalized_fc = {
                        "name": getattr(fc, "name", None),
                        "arguments": getattr(fc, "arguments", None),
                    }
        except Exception:
            # Be permissive – missing/invalid function call info should not fail the request
            normalized_fc = None

        return CompletionResponse(
            content=content_text,
            model=model,
            provider="openai",
            usage=TokenUsage(
                input_tokens=prompt_tokens,
                output_tokens=completion_tokens,
                total_tokens=total_tokens,
                estimated_cost=cost,
                cache_read_tokens=cache_read,
            ),
            finish_reason=choice.finish_reason,
            function_call=normalized_fc,
            request_id=response.id,
            latency_ms=latency_ms,
            effective_max_completion=adjusted_max,
        )

    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator[str]:
        """Stream a completion using OpenAI API"""

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model = model_config.model_id

        # Prepare API request (align parameters with non-streaming variant)
        api_request = {
            "model": model,
            "messages": request.messages,
            "stream": True,
        }

        # GPT-5 family (excluding gpt-5-pro) has restricted parameter support
        is_gpt5_chat = model.startswith("gpt-5") and not model.startswith("gpt-5-pro")

        # Only include sampling parameters if NOT GPT-5 chat models
        # GPT-5 chat models only support default values (temperature=1, etc)
        if not is_gpt5_chat:
            api_request["temperature"] = request.temperature
            api_request["top_p"] = request.top_p
            api_request["frequency_penalty"] = request.frequency_penalty
            api_request["presence_penalty"] = request.presence_penalty

        # GPT-5 family uses max_completion_tokens instead of max_tokens
        if request.max_tokens:
            if is_gpt5_chat:
                api_request["max_completion_tokens"] = request.max_tokens
            else:
                api_request["max_tokens"] = request.max_tokens

        if request.stop:
            api_request["stop"] = request.stop
        if request.functions:
            api_request["functions"] = request.functions
            # Translate function_call to tool_choice for OpenAI API
            if request.function_call:
                if request.function_call == "any":
                    api_request["tool_choice"] = "required"  # Force tool use
                elif request.function_call == "auto":
                    api_request["tool_choice"] = "auto"
                elif request.function_call == "none":
                    api_request["tool_choice"] = "none"
                else:
                    # Specific function name
                    api_request["tool_choice"] = {"type": "function", "function": {"name": request.function_call}}
        if request.seed is not None:
            api_request["seed"] = request.seed
        if request.response_format:
            api_request["response_format"] = request.response_format
        if request.user:
            api_request["user"] = request.user

        # Make streaming API call
        # Request usage statistics for streaming
        # Add stream options for usage
        api_request["stream_options"] = {"include_usage": True}

        # Helper to extract text from streaming delta (handles GPT-5 formats)
        def _extract_delta_content(delta) -> str:
            """Extract text content from streaming delta, handling GPT-5 formats."""
            # 1. Standard delta.content (string)
            content = getattr(delta, "content", None)
            if isinstance(content, str) and content:
                return content

            # 2. delta.content as list (GPT-5 may return structured content)
            if isinstance(content, list):
                parts = []
                for part in content:
                    text = None
                    if hasattr(part, "text"):
                        text = getattr(part, "text", None)
                    elif isinstance(part, dict):
                        text = part.get("text") or part.get("output_text")
                    if text:
                        parts.append(str(text))
                if parts:
                    return "".join(parts)

            # 3. GPT-5 reasoning/output fields
            for field in ["reasoning_content", "output", "thinking"]:
                value = getattr(delta, field, None)
                if isinstance(value, str) and value:
                    return value
                elif isinstance(value, list):
                    parts = []
                    for part in value:
                        if isinstance(part, dict) and "text" in part:
                            parts.append(str(part["text"]))
                        elif isinstance(part, str):
                            parts.append(part)
                    if parts:
                        return "".join(parts)

            return ""

        try:
            stream = await self.client.chat.completions.create(**api_request)

            # Accumulate tool/function calls across streaming deltas.
            # OpenAI streams tool calls incrementally (name/arguments chunks).
            tool_calls_by_index: Dict[int, Dict[str, Any]] = {}
            last_model = model
            yielded_final_meta = False

            def _accumulate_tool_call(index: int, name: Any, arguments_part: Any) -> None:
                entry = tool_calls_by_index.get(index, {"name": None, "arguments": ""})
                if isinstance(name, str) and name:
                    entry["name"] = name
                if isinstance(arguments_part, str) and arguments_part:
                    entry["arguments"] = (entry.get("arguments") or "") + arguments_part
                tool_calls_by_index[index] = entry

            async for chunk in stream:
                # Track actual model from chunks when available
                if getattr(chunk, "model", None):
                    last_model = chunk.model

                if chunk.choices:
                    delta = chunk.choices[0].delta

                    # Newer SDK: tool_calls streaming (array of calls)
                    for tc in getattr(delta, "tool_calls", None) or []:
                        try:
                            idx = getattr(tc, "index", None)
                            if idx is None:
                                continue
                            fn = getattr(tc, "function", None)
                            if fn is None:
                                continue
                            _accumulate_tool_call(
                                int(idx),
                                getattr(fn, "name", None),
                                getattr(fn, "arguments", None),
                            )
                        except Exception:
                            continue

                    # Older SDK: function_call streaming (single call)
                    fc = getattr(delta, "function_call", None)
                    if fc is not None:
                        try:
                            _accumulate_tool_call(
                                0, getattr(fc, "name", None), getattr(fc, "arguments", None)
                            )
                        except Exception:
                            pass

                    text = _extract_delta_content(delta)
                    if text:
                        yield text

                # Check for usage in the chunk (usually the last one)
                if chunk.usage:
                    stream_cache_read = 0
                    try:
                        stream_details = getattr(chunk.usage, "prompt_tokens_details", None)
                        if stream_details:
                            stream_cache_read = int(getattr(stream_details, "cached_tokens", 0) or 0)
                    except Exception:
                        pass
                    meta = {
                        "usage": {
                            "total_tokens": chunk.usage.total_tokens,
                            "input_tokens": chunk.usage.prompt_tokens,
                            "output_tokens": chunk.usage.completion_tokens,
                            "cache_read_tokens": stream_cache_read,
                        },
                        "model": last_model,
                        "provider": "openai",
                    }
                    # Include (possibly multiple) tool calls when present.
                    if tool_calls_by_index:
                        ordered = [
                            tool_calls_by_index[i]
                            for i in sorted(tool_calls_by_index.keys())
                            if tool_calls_by_index[i].get("name")
                        ]
                        if ordered:
                            meta["function_call"] = {
                                "name": ordered[0].get("name"),
                                "arguments": ordered[0].get("arguments"),
                            }
                            meta["function_calls"] = ordered
                    yielded_final_meta = True
                    yield meta

            # If usage wasn't emitted but we did see tool calls, emit tool call metadata anyway.
            if not yielded_final_meta and tool_calls_by_index:
                ordered = [
                    tool_calls_by_index[i]
                    for i in sorted(tool_calls_by_index.keys())
                    if tool_calls_by_index[i].get("name")
                ]
                if ordered:
                    yield {
                        "model": last_model,
                        "provider": "openai",
                        "function_call": {
                            "name": ordered[0].get("name"),
                            "arguments": ordered[0].get("arguments"),
                        },
                        "function_calls": ordered,
                    }

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

    def _choose_api_route(self, request: CompletionRequest, model_config) -> str:
        """Heuristic selection between Responses vs Chat APIs.

        - GPT-5-pro requires Responses API (only available there)
        - Prefer Chat for tool calling and strict JSON mode (more mature behavior).
        - Prefer Chat for GPT-5 and GPT-4.1 families (standard chat completions).
        - Prefer Responses for reasoning‑heavy tasks when supported and signaled by complexity.
        """
        # Always use Responses API when chaining from a previous response
        if getattr(request, "previous_response_id", None):
            return "responses"

        caps = getattr(model_config, "capabilities", None)
        model_name = getattr(model_config, "model_id", "")

        has_tools = bool(request.functions)
        wants_json = bool(
            request.response_format
            and request.response_format.get("type") == "json_object"
        )
        high_complexity = (request.complexity_score or 0.0) >= 0.7

        # Check requirements that need Chat Completions API BEFORE model family checks
        # Responses API doesn't support response_format or tools properly
        if has_tools and getattr(caps, "supports_tools", True):
            return "chat"
        if wants_json and getattr(caps, "supports_json_mode", True):
            return "chat"

        # GPT-5 family: Use Chat API by default (more reliable than Responses API)
        # Responses API has strict content type validation and empty output issues with reasoning models
        if model_name.startswith("gpt-5"):
            return "chat"

        # GPT-4.1 family uses standard chat completions API
        if model_name.startswith("gpt-4."):
            return "chat"

        if high_complexity and getattr(caps, "supports_reasoning", False):
            return "responses"
        # Default preference: Chat Completions API
        return "chat"

    async def _complete_responses_api(
        self, request: CompletionRequest, model: str
    ) -> CompletionResponse:
        """Call OpenAI Responses API and normalize to CompletionResponse."""
        import time

        # Get model config for token limits
        model_config = self.resolve_model_config(request)

        # Map OpenAI chat-style messages to Responses input blocks
        inputs: List[Dict[str, Any]] = []
        for msg in request.messages:
            role = msg.get("role", "user")
            text = msg.get("content", "")
            if not isinstance(text, str):
                text = str(text)
            content_block = {"type": "input_text", "text": text}
            inputs.append({"role": role, "content": [content_block]})

        # Clamp max_output_tokens to model limits (same as Chat API path)
        prompt_tokens_est = self.count_tokens(request.messages, model)
        safety_margin = 256
        model_context = getattr(model_config, "context_window", 8192)
        model_max_output = getattr(model_config, "max_tokens", model_context)
        requested_max = int(request.max_tokens) if request.max_tokens else model_max_output
        headroom = max(0, model_context - prompt_tokens_est - safety_margin)
        adjusted_max = max(1, min(requested_max, model_max_output, headroom))

        params: Dict[str, Any] = {
            "model": model,
            "max_output_tokens": adjusted_max,
        }

        # Chain from previous response for cache reuse (OpenAI keeps
        # the full conversation server-side, so we only send the new message).
        if request.previous_response_id:
            params["previous_response_id"] = request.previous_response_id
            # Only send the last user message as incremental input
            last_user = [m for m in request.messages if m.get("role") == "user"]
            if last_user:
                params["input"] = [{"role": "user", "content": [{"type": "input_text", "text": last_user[-1].get("content", "")}]}]
            else:
                params["input"] = inputs
        else:
            params["input"] = inputs
            params["store"] = True  # Enable server-side storage for future chaining

        # No reasoning parameter = thinking disabled by default
        # Note: Responses API doesn't support response_format parameter
        # If needed, the fallback to Chat Completions API will handle it
        if request.functions:
            # Minimal pass-through using function blocks
            tools: List[Dict[str, Any]] = []
            for fn in request.functions:
                if not isinstance(fn, dict):
                    continue
                name = fn.get("name")
                if not name:
                    continue
                tools.append(
                    {
                        "type": "function",
                        "name": name,
                        "description": fn.get("description"),
                        "parameters": fn.get("parameters", {}),
                    }
                )
            if tools:
                params["tools"] = tools

        start_time = time.time()
        try:
            response = await self.client.responses.create(**params)
        except openai.APIError as e:
            import logging
            logger = logging.getLogger(__name__)
            logger.error(
                f"OpenAI Responses API error for model {model}: "
                f"Status={getattr(e, 'status_code', 'unknown')}, "
                f"Type={type(e).__name__}, "
                f"Message={str(e)}"
            )
            raise Exception(f"OpenAI Responses API error: {e}") from e
        except Exception as e:
            import logging
            logger = logging.getLogger(__name__)
            logger.error(
                f"Unexpected error calling Responses API for model {model}: "
                f"Type={type(e).__name__}, "
                f"Message={str(e)}"
            )
            raise

        # Prefer output_text when Responses API provides it directly
        direct_text = getattr(response, "output_text", None)
        if isinstance(direct_text, str) and direct_text.strip():
            content = direct_text.strip()
            try:
                raw = response.model_dump()
            except Exception:
                raw = {
                    "output": getattr(response, "output", None),
                    "usage": getattr(response, "usage", None),
                    "id": getattr(response, "id", None),
                    "model": getattr(response, "model", model),
                }
        else:
            # Extract text blocks; usage may be a dict-like
            text_parts: List[str] = []
            try:
                raw = response.model_dump()
            except Exception:
                raw = {
                    "output": getattr(response, "output", None),
                    "usage": getattr(response, "usage", None),
                    "id": getattr(response, "id", None),
                    "model": getattr(response, "model", model),
                }

            out = raw.get("output") or []
            if isinstance(out, list):
                for item in out:
                    if isinstance(item, dict):
                        if item.get("type") in ("output_text", "text"):
                            val = item.get("content") or item.get("text")
                            if isinstance(val, str) and val.strip():
                                text_parts.append(val.strip())
                        elif item.get("type") == "message":
                            for block in item.get("content", []) or []:
                                if isinstance(block, dict) and block.get("type") in (
                                    "output_text",
                                    "text",
                                ):
                                    val = block.get("text")
                                    if isinstance(val, str) and val.strip():
                                        text_parts.append(val.strip())

            content = "\n\n".join(text_parts).strip()

        usage = raw.get("usage") or {}
        try:
            input_tokens = int(usage.get("input_tokens", 0))
            output_tokens = int(usage.get("output_tokens", 0))
            total_tokens = int(usage.get("total_tokens", input_tokens + output_tokens))
        except Exception:
            # Fallback to estimation
            input_tokens = self.count_tokens(request.messages, model)
            output_tokens = self.count_tokens(
                [{"role": "assistant", "content": content}], model
            )
            total_tokens = input_tokens + output_tokens

        cache_read = 0
        try:
            # Responses API uses input_tokens_details (not prompt_tokens_details)
            details = getattr(response.usage, "input_tokens_details", None) or \
                      getattr(response.usage, "prompt_tokens_details", None)
            if details:
                cache_read = int(getattr(details, "cached_tokens", 0) or 0)
        except Exception:
            pass

        latency_ms = int((time.time() - start_time) * 1000)
        cost = self.estimate_cost(
            input_tokens, output_tokens, self._resolve_alias(model),
            cache_read_tokens=cache_read,
        )

        # For Responses API, max_output_tokens is the effective limit
        effective_max = params.get("max_output_tokens", 2048)

        return CompletionResponse(
            content=content,
            model=raw.get("model", model),
            provider="openai",
            usage=TokenUsage(
                input_tokens=input_tokens,
                output_tokens=output_tokens,
                total_tokens=total_tokens,
                estimated_cost=cost,
                cache_read_tokens=cache_read,
            ),
            finish_reason="stop",
            function_call=None,
            request_id=raw.get("id"),
            latency_ms=latency_ms,
            effective_max_completion=effective_max,
        )
