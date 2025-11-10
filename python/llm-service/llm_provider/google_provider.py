"""
Google Gemini Provider Implementation
Provides access to Google's Gemini models via the Google AI Python SDK.
Aligned with OpenAI and xAI provider patterns for consistency.
"""

import logging
import os
import time
from typing import Dict, List, Any, AsyncIterator
from tenacity import retry, stop_after_attempt, wait_exponential
import google.generativeai as genai
from google.generativeai.types import HarmCategory, HarmBlockThreshold

from .base import (
    LLMProvider,
    CompletionRequest,
    CompletionResponse,
    TokenUsage,
    TokenCounter,
)


class GoogleProvider(LLMProvider):
    """Provider for Google Gemini models"""

    def __init__(self, config: Dict[str, Any]):
        self.logger = logging.getLogger(__name__)
        
        # Get API key from config or environment
        api_key = config.get("api_key") or os.getenv("GOOGLE_API_KEY")
        if not api_key:
            raise ValueError("Google API key not provided")

        # Validate API key format (Google AI keys typically start with 'AIza' and are 39 chars)
        if len(api_key) < 30:
            raise ValueError("Invalid Google API key format - too short")
        if not api_key.startswith(("AIza", "sk-", "test-")):
            self.logger.warning("Google API key does not match expected format")

        # Configure the Google AI SDK
        genai.configure(api_key=api_key)
        self.api_key = api_key

        # Store model instances (will be populated during initialization)
        self.model_instances = {}

        # Safety settings - permissive by default to avoid blocking legitimate queries
        # Can be customized via config for production use
        self.safety_settings = config.get(
            "safety_settings",
            {
                HarmCategory.HARM_CATEGORY_HATE_SPEECH: HarmBlockThreshold.BLOCK_NONE,
                HarmCategory.HARM_CATEGORY_SEXUALLY_EXPLICIT: HarmBlockThreshold.BLOCK_NONE,
                HarmCategory.HARM_CATEGORY_HARASSMENT: HarmBlockThreshold.BLOCK_NONE,
                HarmCategory.HARM_CATEGORY_DANGEROUS_CONTENT: HarmBlockThreshold.BLOCK_NONE,
            },
        )

        super().__init__(config)

    def _initialize_models(self):
        """Initialize available Google Gemini models from configuration."""
        self._load_models_from_config()

        # Initialize model instances
        for alias, model_config in self.models.items():
            try:
                self.model_instances[alias] = genai.GenerativeModel(
                    model_config.model_id
                )
            except Exception as e:
                self.logger.warning(
                    f"Failed to initialize model {model_config.model_id}: {e}"
                )

    def _resolve_alias(self, model_id: str) -> str:
        """Return the configured alias for a given vendor model_id, if any."""
        for alias, cfg in self.models.items():
            if cfg.model_id == model_id:
                return alias
        return model_id

    def _convert_messages_to_gemini_format(
        self, messages: List[Dict[str, Any]]
    ) -> List[Dict[str, Any]]:
        """Convert OpenAI-style messages to Gemini format with proper system message handling.
        
        Gemini API:
        - Uses "user" and "model" roles (not "assistant")
        - Doesn't support "system" role natively
        - Requires content to be a list of parts
        """
        gemini_messages = []
        system_parts = []

        # First pass: collect system messages
        for msg in messages:
            role = msg.get("role")
            content = msg.get("content", "")
            
            # Handle content that might be list or string
            if isinstance(content, list):
                # Flatten multi-part content to text only
                text_parts = []
                for part in content:
                    if isinstance(part, dict) and part.get("type") == "text":
                        text_parts.append(part.get("text", ""))
                    elif isinstance(part, str):
                        text_parts.append(part)
                content = " ".join(text_parts)
            elif not isinstance(content, str):
                content = str(content)
            
            if role == "system" and content:
                system_parts.append(f"System: {content}")

        # Second pass: build Gemini messages
        first_user_msg = True
        for msg in messages:
            role = msg.get("role")
            content = msg.get("content", "")
            
            # Skip system messages (already collected)
            if role == "system":
                continue
            
            # Handle content that might be list or string
            if isinstance(content, list):
                text_parts = []
                for part in content:
                    if isinstance(part, dict) and part.get("type") == "text":
                        text_parts.append(part.get("text", ""))
                    elif isinstance(part, str):
                        text_parts.append(part)
                content = " ".join(text_parts)
            elif not isinstance(content, str):
                content = str(content)
            
            # Skip empty messages
            if not content:
                continue

            if role == "user":
                # Prepend system messages to first user message
                if first_user_msg and system_parts:
                    system_prefix = "\n\n".join(system_parts)
                    content = f"{system_prefix}\n\n{content}"
                    first_user_msg = False
                gemini_messages.append({"role": "user", "parts": [content]})
            elif role == "assistant":
                gemini_messages.append({"role": "model", "parts": [content]})

        return gemini_messages

    def _create_generation_config(self, request: CompletionRequest) -> Dict[str, Any]:
        """Create Gemini generation configuration from request"""
        config = {
            "temperature": request.temperature,
            "top_p": request.top_p,
            "max_output_tokens": request.max_tokens or 2048,
        }

        if request.stop:
            config["stop_sequences"] = request.stop

        return config

    @retry(
        stop=stop_after_attempt(3), wait=wait_exponential(multiplier=0.5, min=1, max=8)
    )
    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        """Execute a completion request with native async support."""

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model_id = model_config.model_id
        model_alias = self._resolve_alias(model_id)

        if model_alias not in self.model_instances:
            raise ValueError(f"Model {model_alias} not initialized")

        model = self.model_instances[model_alias]

        # Convert messages to Gemini format
        gemini_messages = self._convert_messages_to_gemini_format(request.messages)

        # Create generation config
        generation_config = self._create_generation_config(request)

        start_time = time.time()
        try:
            # Use native async method if available
            if hasattr(model, "generate_content_async"):
                response = await model.generate_content_async(
                    gemini_messages,
                    generation_config=generation_config,
                    safety_settings=self.safety_settings,
                )
            else:
                # Fallback to sync method in executor
                import asyncio
                loop = asyncio.get_event_loop()
                response = await loop.run_in_executor(
                    None,
                    lambda: model.generate_content(
                        gemini_messages,
                        generation_config=generation_config,
                        safety_settings=self.safety_settings,
                    ),
                )
            
            latency_ms = int((time.time() - start_time) * 1000)

            # Extract text from response
            if hasattr(response, "text") and response.text:
                content = response.text
            else:
                # Handle blocked or empty responses
                content = "Response was blocked or empty"
                self.logger.warning(f"Empty or blocked response from Gemini: {response}")

            # Extract tokens from usage metadata (Gemini provides accurate counts)
            try:
                usage_metadata = getattr(response, "usage_metadata", None)
                if usage_metadata:
                    input_tokens = int(getattr(usage_metadata, "prompt_token_count", 0))
                    output_tokens = int(getattr(usage_metadata, "candidates_token_count", 0))
                    total_tokens = int(getattr(usage_metadata, "total_token_count", input_tokens + output_tokens))
                else:
                    # Fallback to estimation
                    input_tokens = self._estimate_tokens(str(request.messages))
                    output_tokens = self._estimate_tokens(content)
                    total_tokens = input_tokens + output_tokens
            except Exception:
                # Fallback to estimation
                input_tokens = self._estimate_tokens(str(request.messages))
                output_tokens = self._estimate_tokens(content)
                total_tokens = input_tokens + output_tokens

            # Use base class estimate_cost with alias for proper lookup
            cost = self.estimate_cost(input_tokens, output_tokens, model_alias)

            # Extract request ID if available
            request_id = None
            if hasattr(response, "model_version"):
                request_id = f"gemini-{response.model_version}"
            elif hasattr(response, "_result"):
                request_id = str(getattr(response._result, "id", None))

            # Determine finish reason
            finish_reason = "stop"
            if hasattr(response, "candidates") and response.candidates:
                first_candidate = response.candidates[0]
                if hasattr(first_candidate, "finish_reason"):
                    finish_reason = str(first_candidate.finish_reason).lower()

            # Create response
            return CompletionResponse(
                content=content,
                model=model_id,
                provider="google",
                usage=TokenUsage(
                    input_tokens=input_tokens,
                    output_tokens=output_tokens,
                    total_tokens=total_tokens,
                    estimated_cost=cost,
                ),
                finish_reason=finish_reason,
                function_call=None,  # Gemini doesn't use OpenAI-style function calls
                request_id=request_id,
                latency_ms=latency_ms,
            )

        except Exception as e:
            self.logger.error(f"Google completion failed: {e}")
            raise Exception(f"Google Gemini API error: {e}")

    async def complete_stream(
        self, request: CompletionRequest
    ) -> AsyncIterator[CompletionResponse]:
        """Stream a completion response (returns CompletionResponse chunks)."""

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model_id = model_config.model_id
        model_alias = self._resolve_alias(model_id)

        if model_alias not in self.model_instances:
            raise ValueError(f"Model {model_alias} not initialized")

        model = self.model_instances[model_alias]

        # Convert messages to Gemini format
        gemini_messages = self._convert_messages_to_gemini_format(request.messages)

        # Create generation config
        generation_config = self._create_generation_config(request)

        try:
            # Use native async streaming if available
            if hasattr(model, "generate_content_async"):
                response_stream = await model.generate_content_async(
                    gemini_messages,
                    generation_config=generation_config,
                    safety_settings=self.safety_settings,
                    stream=True,
                )
            else:
                # Fallback to sync method in executor
                import asyncio
                loop = asyncio.get_event_loop()
                response_stream = await loop.run_in_executor(
                    None,
                    lambda: model.generate_content(
                        gemini_messages,
                        generation_config=generation_config,
                        safety_settings=self.safety_settings,
                        stream=True,
                    ),
                )

            # Stream chunks
            accumulated_text = ""
            for chunk in response_stream:
                if hasattr(chunk, "text") and chunk.text:
                    accumulated_text += chunk.text

                    yield CompletionResponse(
                        content=chunk.text,
                        model=model_id,
                        provider="google",
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

            # Final response with usage
            if hasattr(response_stream, "_done") and response_stream._done:
                final_response = response_stream._done
                try:
                    usage_metadata = getattr(final_response, "usage_metadata", None)
                    if usage_metadata:
                        input_tokens = int(getattr(usage_metadata, "prompt_token_count", 0))
                        output_tokens = int(getattr(usage_metadata, "candidates_token_count", 0))
                    else:
                        input_tokens = self._estimate_tokens(str(request.messages))
                        output_tokens = self._estimate_tokens(accumulated_text)
                except Exception:
                    input_tokens = self._estimate_tokens(str(request.messages))
                    output_tokens = self._estimate_tokens(accumulated_text)

                total_tokens = input_tokens + output_tokens
                cost = self.estimate_cost(input_tokens, output_tokens, model_alias)

                yield CompletionResponse(
                    content="",
                    model=model_id,
                    provider="google",
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
            self.logger.error(f"Google streaming failed: {e}")
            raise Exception(f"Google Gemini streaming error: {e}")

    # NOTE: Tier-to-model fallback removed. Selection now relies on resolve_model_config
    # backed by models.yaml via the manager's configuration.

    def _estimate_tokens(self, text: str) -> int:
        """Estimate token count for text (rough heuristic)."""
        # Rough estimation: 1 token per 4 characters
        return len(text) // 4

    def count_tokens(self, messages: List[Dict[str, Any]], model: str) -> int:
        """Count tokens for messages using Gemini's native counter or TokenCounter fallback."""
        # Try to use Gemini's native token counting if available
        model_alias = self._resolve_alias(model)
        if model_alias in self.model_instances:
            try:
                model_instance = self.model_instances[model_alias]
                # Convert messages to text for counting
                text_parts = []
                for msg in messages:
                    content = msg.get("content", "")
                    if isinstance(content, list):
                        for part in content:
                            if isinstance(part, dict) and part.get("type") == "text":
                                text_parts.append(part.get("text", ""))
                            elif isinstance(part, str):
                                text_parts.append(part)
                    else:
                        text_parts.append(str(content))
                combined_text = " ".join(text_parts)
                return model_instance.count_tokens(combined_text).total_tokens
            except Exception as e:
                self.logger.warning(f"Failed to count tokens with Gemini model: {e}")

        # Fallback to TokenCounter
        return TokenCounter.count_messages_tokens(messages, model)

    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator[str]:
        """Stream text chunks only (normalized streaming)."""
        # Use complete_stream and yield only text portions
        async for chunk in self.complete_stream(request):
            if chunk and isinstance(chunk.content, str) and chunk.content:
                yield chunk.content
