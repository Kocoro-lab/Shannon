"""
Google Gemini Provider Implementation
Provides access to Google's Gemini models via the Google AI Python SDK
"""

import asyncio
import os
from typing import Dict, List, Any, Optional, AsyncIterator
import google.generativeai as genai
from google.generativeai.types import HarmCategory, HarmBlockThreshold

from .base import (
    LLMProvider, ModelConfig, ModelTier, CompletionRequest,
    CompletionResponse, TokenUsage, TokenCounter
)


class GoogleProvider(LLMProvider):
    """Provider for Google Gemini models"""
    
    def __init__(self, config: Dict[str, Any]):
        # Get API key from config or environment
        self.api_key = config.get("api_key") or os.getenv("GOOGLE_API_KEY")
        if not self.api_key:
            raise ValueError("Google API key not provided")
        
        # Validate API key format (Google AI keys typically start with 'AIza' and are 39 chars)
        if len(self.api_key) < 30:
            raise ValueError("Invalid Google API key format - too short")
        if not self.api_key.startswith(('AIza', 'sk-', 'test-')):  # AIza for Google, sk-/test- for testing
            import logging
            logger = logging.getLogger(__name__)
            logger.warning("Google API key does not match expected format")
        
        # Configure the Google AI SDK
        genai.configure(api_key=self.api_key)
        
        # Store model instances
        self.model_instances = {}
        
        # Safety settings - can be customized via config
        self.safety_settings = config.get("safety_settings", {
            HarmCategory.HARM_CATEGORY_HATE_SPEECH: HarmBlockThreshold.BLOCK_NONE,
            HarmCategory.HARM_CATEGORY_SEXUALLY_EXPLICIT: HarmBlockThreshold.BLOCK_NONE,
            HarmCategory.HARM_CATEGORY_HARASSMENT: HarmBlockThreshold.BLOCK_NONE,
            HarmCategory.HARM_CATEGORY_DANGEROUS_CONTENT: HarmBlockThreshold.BLOCK_NONE,
        })
        
        super().__init__(config)
    
    def _initialize_models(self):
        """Initialize available Google Gemini models"""
        
        # Gemini Pro (balanced model)
        self.models["gemini-pro"] = ModelConfig(
            provider="google",
            model_id="gemini-pro",
            tier=ModelTier.MEDIUM,
            max_tokens=2048,
            context_window=32768,
            input_price_per_1k=0.0005,  # $0.50 per 1M tokens
            output_price_per_1k=0.0015,  # $1.50 per 1M tokens
            supports_functions=True,
            supports_streaming=True,
            supports_vision=False,
        )
        
        # Gemini Pro Vision (multimodal)
        self.models["gemini-pro-vision"] = ModelConfig(
            provider="google",
            model_id="gemini-pro-vision",
            tier=ModelTier.MEDIUM,
            max_tokens=2048,
            context_window=16384,
            input_price_per_1k=0.0005,
            output_price_per_1k=0.0015,
            supports_functions=False,
            supports_streaming=True,
            supports_vision=True,
        )
        
        # Gemini 1.5 Flash (fast, efficient)
        self.models["gemini-1.5-flash"] = ModelConfig(
            provider="google",
            model_id="gemini-1.5-flash",
            tier=ModelTier.SMALL,
            max_tokens=8192,
            context_window=1048576,  # 1M token context window
            input_price_per_1k=0.00035,  # $0.35 per 1M tokens
            output_price_per_1k=0.00105,  # $1.05 per 1M tokens
            supports_functions=True,
            supports_streaming=True,
            supports_vision=True,
        )
        
        # Gemini 1.5 Pro (most capable)
        self.models["gemini-1.5-pro"] = ModelConfig(
            provider="google",
            model_id="gemini-1.5-pro",
            tier=ModelTier.LARGE,
            max_tokens=8192,
            context_window=1048576,  # 1M token context window
            input_price_per_1k=0.00125,  # $1.25 per 1M tokens
            output_price_per_1k=0.00375,  # $3.75 per 1M tokens
            supports_functions=True,
            supports_streaming=True,
            supports_vision=True,
        )
        
        # Initialize model instances
        for model_id in self.models:
            try:
                self.model_instances[model_id] = genai.GenerativeModel(model_id)
            except Exception as e:
                self.logger.warning(f"Failed to initialize model {model_id}: {e}")
    
    def _convert_messages_to_gemini_format(self, messages: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
        """Convert OpenAI-style messages to Gemini format"""
        gemini_messages = []
        
        for msg in messages:
            role = msg.get("role")
            content = msg.get("content")
            
            # Map roles: OpenAI uses "system", "user", "assistant"
            # Gemini uses "user" and "model"
            if role == "system":
                # Gemini doesn't have a system role, prepend to first user message
                # or create a user message
                gemini_messages.append({
                    "role": "user",
                    "parts": [f"System: {content}"]
                })
            elif role == "user":
                gemini_messages.append({
                    "role": "user",
                    "parts": [content]
                })
            elif role == "assistant":
                gemini_messages.append({
                    "role": "model",
                    "parts": [content]
                })
        
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
    
    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        """Execute a completion request"""
        
        # Select model based on tier
        model_id = self._select_model_for_tier(request.model_tier)
        model_config = self.models[model_id]
        
        if model_id not in self.model_instances:
            raise ValueError(f"Model {model_id} not initialized")
        
        model = self.model_instances[model_id]
        
        # Convert messages to Gemini format
        gemini_messages = self._convert_messages_to_gemini_format(request.messages)
        
        # Create generation config
        generation_config = self._create_generation_config(request)
        
        try:
            # Generate response
            # Note: Google's SDK is synchronous, so we run it in an executor
            loop = asyncio.get_event_loop()
            response = await loop.run_in_executor(
                None,
                lambda: model.generate_content(
                    gemini_messages,
                    generation_config=generation_config,
                    safety_settings=self.safety_settings,
                )
            )
            
            # Extract text from response
            if response.text:
                content = response.text
            else:
                # Handle blocked or empty responses
                content = "Response was blocked or empty"
            
            # Count tokens (Gemini provides token counts)
            input_tokens = response.usage_metadata.prompt_token_count if hasattr(response, 'usage_metadata') else self._estimate_tokens(str(request.messages))
            output_tokens = response.usage_metadata.candidates_token_count if hasattr(response, 'usage_metadata') else self._estimate_tokens(content)
            
            # Calculate cost
            cost = self._calculate_cost(input_tokens, output_tokens, model_config)
            
            # Create response
            return CompletionResponse(
                content=content,
                model=model_id,
                usage=TokenUsage(
                    input_tokens=input_tokens,
                    output_tokens=output_tokens,
                    total_tokens=input_tokens + output_tokens,
                    estimated_cost=cost
                ),
                finish_reason="stop",
                raw_response=response,
            )
            
        except Exception as e:
            self.logger.error(f"Google completion failed: {e}")
            raise
    
    async def complete_stream(
        self, 
        request: CompletionRequest
    ) -> AsyncIterator[CompletionResponse]:
        """Stream a completion response"""
        
        # Select model based on tier
        model_id = self._select_model_for_tier(request.model_tier)
        
        if model_id not in self.model_instances:
            raise ValueError(f"Model {model_id} not initialized")
        
        model = self.model_instances[model_id]
        
        # Convert messages to Gemini format
        gemini_messages = self._convert_messages_to_gemini_format(request.messages)
        
        # Create generation config
        generation_config = self._create_generation_config(request)
        
        try:
            # Generate streaming response
            loop = asyncio.get_event_loop()
            response_stream = await loop.run_in_executor(
                None,
                lambda: model.generate_content(
                    gemini_messages,
                    generation_config=generation_config,
                    safety_settings=self.safety_settings,
                    stream=True,
                )
            )
            
            # Stream chunks
            accumulated_text = ""
            for chunk in response_stream:
                if chunk.text:
                    accumulated_text += chunk.text
                    
                    yield CompletionResponse(
                        content=chunk.text,
                        model=model_id,
                        usage=None,  # Usage calculated at the end
                        finish_reason=None,
                        raw_response=chunk,
                    )
            
            # Final response with usage
            if hasattr(response_stream, '_done') and response_stream._done:
                final_response = response_stream._done
                input_tokens = final_response.usage_metadata.prompt_token_count if hasattr(final_response, 'usage_metadata') else self._estimate_tokens(str(request.messages))
                output_tokens = final_response.usage_metadata.candidates_token_count if hasattr(final_response, 'usage_metadata') else self._estimate_tokens(accumulated_text)
                
                model_config = self.models[model_id]
                cost = self._calculate_cost(input_tokens, output_tokens, model_config)
                
                yield CompletionResponse(
                    content="",
                    model=model_id,
                    usage=TokenUsage(
                        input_tokens=input_tokens,
                        output_tokens=output_tokens,
                        total_tokens=input_tokens + output_tokens,
                        estimated_cost=cost
                    ),
                    finish_reason="stop",
                    raw_response=final_response,
                )
                
        except Exception as e:
            self.logger.error(f"Google streaming failed: {e}")
            raise
    
    def _select_model_for_tier(self, tier: ModelTier) -> str:
        """Select appropriate model based on tier"""
        tier_models = {
            ModelTier.SMALL: "gemini-1.5-flash",
            ModelTier.MEDIUM: "gemini-pro",
            ModelTier.LARGE: "gemini-1.5-pro",
        }
        
        model_id = tier_models.get(tier, "gemini-pro")
        
        # Check if model is available
        if model_id not in self.models:
            # Fallback to any available model
            if self.models:
                model_id = list(self.models.keys())[0]
                self.logger.warning(f"Model for tier {tier} not found, using {model_id}")
            else:
                raise ValueError("No models available")
        
        return model_id
    
    def _estimate_tokens(self, text: str) -> int:
        """Estimate token count for text"""
        # Rough estimation: 1 token per 4 characters
        return len(text) // 4
    
    def _calculate_cost(
        self, 
        input_tokens: int, 
        output_tokens: int, 
        model_config: ModelConfig
    ) -> float:
        """Calculate cost based on token usage"""
        input_cost = (input_tokens / 1000) * model_config.input_price_per_1k
        output_cost = (output_tokens / 1000) * model_config.output_price_per_1k
        return input_cost + output_cost
    
    def count_tokens(self, text: str, model: Optional[str] = None) -> int:
        """Count tokens in text"""
        # Use Gemini's count_tokens method if available
        if model and model in self.model_instances:
            try:
                model_instance = self.model_instances[model]
                return model_instance.count_tokens(text).total_tokens
            except Exception as e:
                self.logger.warning(f"Failed to count tokens with model: {e}")
        
        # Fallback to estimation
        return self._estimate_tokens(text)