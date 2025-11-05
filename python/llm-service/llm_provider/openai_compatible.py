"""
OpenAI-Compatible Provider Implementation
For providers that implement OpenAI's API (DeepSeek, Qwen, local models, etc.)
"""

from typing import Dict, List, Any, AsyncIterator
from openai import AsyncOpenAI
from tenacity import retry, stop_after_attempt, wait_exponential

from .base import (
    LLMProvider,
    ModelConfig,
    ModelTier,
    CompletionRequest,
    CompletionResponse,
    TokenUsage,
    TokenCounter,
)


class OpenAICompatibleProvider(LLMProvider):
    """Provider for OpenAI-compatible APIs"""

    def __init__(self, config: Dict[str, Any]):
        # Get API configuration
        self.api_key = config.get("api_key", "dummy")  # Some providers don't need keys
        self.base_url = config.get(
            "base_url", "http://localhost:11434/v1"
        )  # Default for Ollama

        # Initialize OpenAI client with custom base URL
        self.client = AsyncOpenAI(api_key=self.api_key, base_url=self.base_url)

        super().__init__(config)

    def _initialize_models(self):
        """Initialize models from configuration"""

        self._load_models_from_config(allow_empty=True)

        # If no models configured, add some defaults for developer convenience
        if not self.models:
            self._add_default_models()

    def _add_default_models(self):
        """Add default model configurations for common providers"""

        # Detect provider type from base URL
        if "deepseek" in self.base_url.lower():
            self._add_deepseek_models()
        elif "dashscope" in self.base_url.lower() or "qwen" in self.base_url.lower():
            self._add_qwen_models()
        elif "localhost" in self.base_url or "ollama" in self.base_url.lower():
            self._add_ollama_models()
        else:
            # Generic OpenAI-compatible model
            self.models["default"] = ModelConfig(
                provider="openai_compatible",
                model_id="default",
                tier=ModelTier.MEDIUM,
                max_tokens=4096,
                context_window=8192,
                input_price_per_1k=0.001,
                output_price_per_1k=0.002,
            )

    def _add_deepseek_models(self):
        """Add DeepSeek model configurations"""

        self.models["deepseek-chat"] = ModelConfig(
            provider="deepseek",
            model_id="deepseek-chat",
            tier=ModelTier.SMALL,
            max_tokens=4096,
            context_window=32768,
            input_price_per_1k=0.0001,
            output_price_per_1k=0.0002,
        )

        self.models["deepseek-coder"] = ModelConfig(
            provider="deepseek",
            model_id="deepseek-coder",
            tier=ModelTier.MEDIUM,
            max_tokens=4096,
            context_window=16384,
            input_price_per_1k=0.0001,
            output_price_per_1k=0.0002,
        )

        self.models["deepseek-v3"] = ModelConfig(
            provider="deepseek",
            model_id="deepseek-v3",
            tier=ModelTier.MEDIUM,
            max_tokens=8192,
            context_window=64000,
            input_price_per_1k=0.001,
            output_price_per_1k=0.002,
        )

    def _add_qwen_models(self):
        """Add Qwen model configurations"""

        self.models["qwen-turbo"] = ModelConfig(
            provider="qwen",
            model_id="qwen-turbo",
            tier=ModelTier.SMALL,
            max_tokens=4096,
            context_window=8192,
            input_price_per_1k=0.0003,
            output_price_per_1k=0.0006,
        )

        self.models["qwen-plus"] = ModelConfig(
            provider="qwen",
            model_id="qwen-plus",
            tier=ModelTier.MEDIUM,
            max_tokens=8192,
            context_window=32768,
            input_price_per_1k=0.0008,
            output_price_per_1k=0.002,
        )

        self.models["qwen-max"] = ModelConfig(
            provider="qwen",
            model_id="qwen-max",
            tier=ModelTier.LARGE,
            max_tokens=8192,
            context_window=32768,
            input_price_per_1k=0.002,
            output_price_per_1k=0.006,
        )

        self.models["qwq-32b"] = ModelConfig(
            provider="qwen",
            model_id="qwq-32b-preview",
            tier=ModelTier.LARGE,
            max_tokens=32768,
            context_window=32768,
            input_price_per_1k=0.001,
            output_price_per_1k=0.003,
        )

    def _add_ollama_models(self):
        """Add Ollama model configurations"""

        # Common Ollama models
        self.models["llama2"] = ModelConfig(
            provider="ollama",
            model_id="llama2",
            tier=ModelTier.SMALL,
            max_tokens=4096,
            context_window=4096,
            input_price_per_1k=0.0,  # Local models have no cost
            output_price_per_1k=0.0,
        )

        self.models["mistral"] = ModelConfig(
            provider="ollama",
            model_id="mistral",
            tier=ModelTier.SMALL,
            max_tokens=8192,
            context_window=8192,
            input_price_per_1k=0.0,
            output_price_per_1k=0.0,
        )

        self.models["codellama"] = ModelConfig(
            provider="ollama",
            model_id="codellama",
            tier=ModelTier.MEDIUM,
            max_tokens=4096,
            context_window=4096,
            input_price_per_1k=0.0,
            output_price_per_1k=0.0,
        )

    def _resolve_alias(self, model_id: str) -> str:
        """Return the configured alias for a given vendor model_id, if any."""
        for alias, cfg in self.models.items():
            if cfg.model_id == model_id:
                return alias
        return model_id

    def count_tokens(self, messages: List[Dict[str, Any]], model: str) -> int:
        """
        Count tokens for the model.
        Uses generic estimation since tokenizers vary by provider.
        """
        return TokenCounter.count_messages_tokens(messages, model)

    @retry(
        stop=stop_after_attempt(3), wait=wait_exponential(multiplier=0.5, min=1, max=8)
    )
    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        """Generate a completion using the OpenAI-compatible API"""

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model = model_config.model_id
        model_alias = self._resolve_alias(model)

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

        if request.functions and model_config.supports_functions:
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
        except Exception as e:
            raise Exception(f"OpenAI-compatible API error ({self.base_url}): {e}")

        latency_ms = int((time.time() - start_time) * 1000)

        # Extract response
        choice = response.choices[0]
        message = choice.message

        # Normalize content: some compatible providers may return a list of
        # content parts rather than a plain string. Extract text segments.
        def _extract_text_from_message(msg) -> str:
            try:
                content = getattr(msg, "content", None)
                if isinstance(content, str):
                    return content or ""
                if isinstance(content, list):
                    parts: List[str] = []
                    for part in content:
                        try:
                            text = getattr(part, "text", None)
                            if not text and isinstance(part, dict):
                                text = part.get("text")
                            if isinstance(text, str) and text.strip():
                                parts.append(text.strip())
                        except Exception:
                            pass
                    return "\n\n".join(parts).strip()
                if hasattr(content, "text"):
                    txt = getattr(content, "text", "")
                    return txt or ""
            except Exception:
                pass
            return ""

        content_text = _extract_text_from_message(message)

        # Handle token usage (some providers might not return this)
        prompt_tokens = 0
        completion_tokens = 0
        total_tokens = 0
        if hasattr(response, "usage") and response.usage:
            try:
                prompt_tokens = int(getattr(response.usage, "prompt_tokens", 0))
                completion_tokens = int(getattr(response.usage, "completion_tokens", 0))
                total_tokens = int(
                    getattr(
                        response.usage,
                        "total_tokens",
                        prompt_tokens + completion_tokens,
                    )
                )
            except Exception:
                prompt_tokens = 0
                completion_tokens = 0
                total_tokens = 0
        if total_tokens == 0:
            # Estimate if not provided
            prompt_tokens = self.count_tokens(request.messages, model)
            completion_tokens = self.count_tokens(
                [{"role": "assistant", "content": content_text}], model
            )
            total_tokens = prompt_tokens + completion_tokens

        # Calculate cost using alias for proper lookup
        cost = self.estimate_cost(prompt_tokens, completion_tokens, model_alias)

        # Build response
        return CompletionResponse(
            content=content_text,
            model=model,
            provider=self.config.get("name", "openai_compatible"),
            usage=TokenUsage(
                input_tokens=prompt_tokens,
                output_tokens=completion_tokens,
                total_tokens=total_tokens,
                estimated_cost=cost,
            ),
            finish_reason=choice.finish_reason
            if hasattr(choice, "finish_reason")
            else "stop",
            function_call=message.function_call
            if hasattr(message, "function_call")
            else None,
            request_id=response.id if hasattr(response, "id") else None,
            latency_ms=latency_ms,
        )

    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator[str]:
        """Stream a completion using the OpenAI-compatible API"""

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

        except Exception as e:
            raise Exception(f"OpenAI-compatible streaming error ({self.base_url}): {e}")
