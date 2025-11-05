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

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model = model_config.model_id

        # Choose API route based on model capabilities and request intent
        route = self._choose_api_route(request, model_config)
        if route == "responses":
            if hasattr(self.client, "responses") and hasattr(
                self.client.responses, "create"
            ):
                return await self._complete_responses_api(request, model)
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
            if request.function_call:
                api_request["function_call"] = request.function_call
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
            raise Exception(f"OpenAI API error: {e}")

        latency_ms = int((time.time() - start_time) * 1000)

        choice = response.choices[0]
        message = choice.message

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
                [{"role": "assistant", "content": message.content or ""}], model
            )
            total_tokens = prompt_tokens + completion_tokens

        # Use configured alias for cost lookup when available
        cost = self.estimate_cost(
            prompt_tokens, completion_tokens, self._resolve_alias(model)
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
            content=message.content or "",
            model=model,
            provider="openai",
            usage=TokenUsage(
                input_tokens=prompt_tokens,
                output_tokens=completion_tokens,
                total_tokens=total_tokens,
                estimated_cost=cost,
            ),
            finish_reason=choice.finish_reason,
            function_call=normalized_fc,
            request_id=response.id,
            latency_ms=latency_ms,
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
            if request.function_call:
                api_request["function_call"] = request.function_call
        if request.seed is not None:
            api_request["seed"] = request.seed
        if request.response_format:
            api_request["response_format"] = request.response_format
        if request.user:
            api_request["user"] = request.user

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

    def _choose_api_route(self, request: CompletionRequest, model_config) -> str:
        """Heuristic selection between Responses vs Chat APIs.

        - GPT-5-pro requires Responses API (only available there)
        - Prefer Chat for tool calling and strict JSON mode (more mature behavior).
        - Prefer Chat for GPT-5 and GPT-4.1 families (standard chat completions).
        - Prefer Responses for reasoning‑heavy tasks when supported and signaled by complexity.
        """
        caps = getattr(model_config, "capabilities", None)
        model_name = getattr(model_config, "model_id", "")

        has_tools = bool(request.functions)
        wants_json = bool(
            request.response_format
            and request.response_format.get("type") == "json_object"
        )
        high_complexity = (request.complexity_score or 0.0) >= 0.7

        # GPT-5-pro only works with Responses API
        if model_name.startswith("gpt-5-pro"):
            return "responses"

        # GPT-5 family works best with Responses API
        if model_name.startswith("gpt-5"):
            return "responses"

        # GPT-4.1 family uses standard chat completions API
        if model_name.startswith("gpt-4."):
            return "chat"

        if has_tools and getattr(caps, "supports_tools", True):
            return "chat"
        if wants_json and getattr(caps, "supports_json_mode", True):
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

        # Map OpenAI chat-style messages to Responses input blocks
        inputs: List[Dict[str, Any]] = []
        for msg in request.messages:
            role = msg.get("role", "user")
            text = msg.get("content", "")
            if not isinstance(text, str):
                text = str(text)
            content_block = {"type": "input_text", "text": text}
            inputs.append({"role": role, "content": [content_block]})

        params: Dict[str, Any] = {
            "model": model,
            "input": inputs,
            "max_output_tokens": request.max_tokens or 2048,
        }
        # Limit reasoning budget so the model produces actual output text
        params["reasoning"] = {"effort": "low"}
        # Tools / response_format are not fully aligned; pass through best‑effort
        if request.response_format:
            params["response_format"] = request.response_format
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
        response = await self.client.responses.create(**params)

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

        latency_ms = int((time.time() - start_time) * 1000)
        cost = self.estimate_cost(
            input_tokens, output_tokens, self._resolve_alias(model)
        )

        return CompletionResponse(
            content=content,
            model=raw.get("model", model),
            provider="openai",
            usage=TokenUsage(
                input_tokens=input_tokens,
                output_tokens=output_tokens,
                total_tokens=total_tokens,
                estimated_cost=cost,
            ),
            finish_reason="stop",
            function_call=None,
            request_id=raw.get("id"),
            latency_ms=latency_ms,
        )
