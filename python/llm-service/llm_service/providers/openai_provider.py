from typing import List, Dict, Any, Optional
import json
import logging
from openai import AsyncOpenAI
from tenacity import retry, stop_after_attempt, wait_exponential

from .base import LLMProvider, ModelInfo, ModelTier, TokenUsage

logger = logging.getLogger(__name__)

class OpenAIProvider(LLMProvider):
    """OpenAI LLM provider (modern models only)"""
    
    # Strict, modern-only registry (seed, can be augmented dynamically)
    MODELS = {
        # 4o family (2024/2025)
        "gpt-4o-mini": ModelInfo(
            id="gpt-4o-mini",
            name="GPT-4o Mini",
            provider=None,
            tier=ModelTier.SMALL,
            context_window=128000,
            cost_per_1k_prompt_tokens=0.0,  # TODO: update with actual pricing
            cost_per_1k_completion_tokens=0.0,
            supports_tools=True,
            supports_streaming=True,
            available=True,
        ),
        "gpt-4o": ModelInfo(
            id="gpt-4o",
            name="GPT-4o",
            provider=None,
            tier=ModelTier.MEDIUM,  # Changed from LARGE to MEDIUM for better tier distribution
            context_window=128000,
            cost_per_1k_prompt_tokens=0.005,  # Updated with actual pricing
            cost_per_1k_completion_tokens=0.015,
            supports_tools=True,
            supports_streaming=True,
            available=True,
        ),
    }
    
    def __init__(self, api_key: str):
        self.api_key = api_key
        self.client = None
        # Instance-level model registry, initialized from class seed
        self._models: Dict[str, ModelInfo] = {k: v for k, v in self.MODELS.items()}
        
    async def initialize(self):
        """Initialize OpenAI client"""
        self.client = AsyncOpenAI(api_key=self.api_key)
        # Set provider reference in models
        from . import ProviderType
        for model in self._models.values():
            model.provider = ProviderType.OPENAI
        # Attempt dynamic discovery to capture newest models
        await self._maybe_discover_models()
    
    async def close(self):
        """Close OpenAI client"""
        if self.client:
            await self.client.close()
    
    @retry(stop=stop_after_attempt(3), wait=wait_exponential(multiplier=1, min=2, max=10))
    async def generate_completion(
        self,
        messages: List[dict],
        model: str = "gpt-4o-mini",
        temperature: float = 0.7,
        max_tokens: int = 2000,
        tools: List[dict] = None,
        **kwargs
    ) -> dict:
        """Generate a completion and return a Responses-style payload."""
        try:
            response_format = kwargs.pop("response_format", None)

            if not hasattr(self.client, "responses") or not hasattr(self.client.responses, "create"):
                logger.error(
                    "OpenAI Responses API is unavailable on this client instance",
                    extra={"model": model, "tools": bool(tools)},
                )
                raise RuntimeError("OpenAI Responses API is not available on the configured client")

            logger.info(
                "OpenAIProvider using Responses API (forced)",
                extra={"model": model, "tools": bool(tools)},
            )

            inputs: List[Dict[str, Any]] = []
            for msg in messages:
                role = msg.get("role", "user")
                text = msg.get("content", "")
                content_block_type = "input_text" if role in ("system", "user") else "output_text"
                inputs.append(
                    {
                        "role": role,
                        "content": [{"type": content_block_type, "text": text}],
                    }
                )

            request_params: Dict[str, Any] = {
                "model": model,
                "input": inputs,
                "max_output_tokens": max_tokens,
                "store": False,
            }

            if not str(model).startswith("gpt-5"):
                request_params["temperature"] = temperature
            else:
                request_params["temperature"] = 1

            if response_format is not None:
                logger.info(
                    "response_format is not yet supported for Responses API; ignoring",
                    extra={"model": model},
                )

            formatted_tools: List[Dict[str, Any]] = []
            if tools:
                for tool in tools:
                    if not isinstance(tool, dict):
                        logger.warning(
                            "Skipping tool with unsupported format",
                            extra={"tool": tool},
                        )
                        continue

                    if "function" in tool:
                        func_block = tool.get("function") or {}
                        name = func_block.get("name")
                        if not name:
                            logger.warning(
                                "Skipping function tool without name",
                                extra={"tool": tool},
                            )
                            continue
                        formatted_tools.append(
                            {
                                "type": "function",
                                "name": name,
                                "description": func_block.get("description")
                                or tool.get("description"),
                                "parameters": func_block.get("parameters") or {},
                            }
                        )
                        continue

                    name = tool.get("tool") or tool.get("name")
                    if not name:
                        logger.warning(
                            "Skipping tool without name",
                            extra={"tool": tool},
                        )
                        continue

                    formatted_tools.append(
                        {
                            "type": tool.get("type", "function"),
                            "name": name,
                            "description": tool.get("description"),
                            "parameters": tool.get("parameters") or {},
                        }
                    )

                if formatted_tools:
                    request_params["tools"] = formatted_tools
                    request_params["tool_choice"] = "auto"
                    logger.debug(
                        "Translated tools for Responses API",
                        extra={"tools": formatted_tools},
                    )
            if str(model).startswith("gpt-5"):
                request_params["reasoning"] = {"effort": "medium"}

            request_params.update(kwargs)

            try:
                response = await self.client.responses.create(**request_params)
            except Exception:
                sanitized_messages = []
                for item in request_params.get("input", []):
                    if not isinstance(item, dict):
                        continue
                    content_blocks = item.get("content", []) or []
                    previews: List[str] = []
                    for block in content_blocks:
                        text_value = None
                        if isinstance(block, dict):
                            text_value = block.get("text")
                        elif isinstance(block, str):
                            text_value = block
                        if text_value is None:
                            continue
                        previews.append(str(text_value)[:200])
                    sanitized_messages.append(
                        {
                            "role": item.get("role"),
                            "content_preview": previews,
                        }
                    )

                logger.exception(
                    "OpenAI Responses API call failed",
                    extra={
                        "model": model,
                        "temperature": request_params.get("temperature"),
                        "max_output_tokens": request_params.get("max_output_tokens"),
                        "tools": bool(request_params.get("tools")),
                        "response_format": response_format,
                        "input_preview": sanitized_messages,
                        "additional_params": {
                            k: v
                            for k, v in request_params.items()
                            if k not in {"input", "tools"}
                        },
                    },
                )
                raise

            try:
                raw_result: Dict[str, Any] = response.model_dump()
            except Exception:
                raw_result = {
                    "id": getattr(response, "id", None),
                    "model": getattr(response, "model", model),
                    "output": getattr(response, "output", None),
                    "usage": getattr(response, "usage", None),
                }

            logger.info("OpenAI Responses raw payload (model=%s): %s", model, raw_result)

            # Normalize output blocks and assemble plain-text string
            output_blocks: List[Dict[str, Any]] = []
            text_segments: List[str] = []

            def append_text(value: Optional[str]) -> None:
                if not value:
                    return
                text = value.strip()
                if not text:
                    return
                text_segments.append(text)
                output_blocks.append({"type": "output_text", "text": text})

            for item in raw_result.get("output", []) or []:
                item_type = item.get("type")
                if item_type == "output_text":
                    content = item.get("content")
                    if isinstance(content, str):
                        append_text(content)
                    elif isinstance(content, list):
                        pieces = []
                        for block in content:
                            if isinstance(block, dict) and block.get("text"):
                                pieces.append(str(block["text"]))
                            elif isinstance(block, str):
                                pieces.append(block)
                        append_text("\n".join(pieces))
                elif item_type == "message":
                    for block in item.get("content", []) or []:
                        if isinstance(block, dict) and block.get("type") in {"output_text", "text"}:
                            append_text(str(block.get("text", "")))
                        elif isinstance(block, str):
                            append_text(block)
                elif item_type in {"function_call", "tool_call"}:
                    name = item.get("name") or item.get("function") or item.get("call_id")
                    arguments = item.get("arguments")
                    if isinstance(arguments, str):
                        try:
                            arguments = json.loads(arguments)
                        except Exception:
                            pass
                    output_blocks.append(
                        {
                            "type": "tool_call",
                            "name": name,
                            "arguments": arguments,
                        }
                    )

            # Fallback: if no explicit message text captured, try response.output_text property
            if not text_segments:
                fallback_text = getattr(response, "output_text", None)
                append_text(fallback_text)

            usage_dict: Optional[Dict[str, Any]] = None
            if isinstance(raw_result.get("usage"), dict):
                usage_dict = raw_result.get("usage")
                if usage_dict and not usage_dict.get("total_tokens"):
                    prompt = usage_dict.get("input_tokens", 0)
                    completion = usage_dict.get("output_tokens", 0)
                    usage_dict["total_tokens"] = prompt + completion

            normalized_result = {
                "id": raw_result.get("id"),
                "model": raw_result.get("model", model),
                "output": output_blocks if output_blocks else raw_result.get("output"),
                "output_text": "\n\n".join(text_segments).strip(),
                "usage": usage_dict or raw_result.get("usage"),
            }

            logger.info(
                "OpenAI Responses API call succeeded",
                extra={
                    "model": normalized_result.get("model"),
                    "tools": bool(tools),
                    "input_tokens": usage_dict.get("input_tokens") if usage_dict else None,
                    "output_tokens": usage_dict.get("output_tokens") if usage_dict else None,
                },
            )

            from . import ProviderType
            normalized_result["provider"] = ProviderType.OPENAI.value
            return normalized_result

        except Exception as e:
            logger.exception("OpenAI completion error")
            raise

    async def _maybe_discover_models(self) -> None:
        """Best-effort dynamic model discovery via OpenAI models.list()."""
        try:
            listing = await self.client.models.list()
            items = getattr(listing, "data", []) or []
            for m in items:
                mid = getattr(m, "id", None)
                if not isinstance(mid, str):
                    continue
                # Filter to current families we care about
                if not (mid.startswith("gpt-4o") or mid.startswith("o") or mid.startswith("gpt-4.1")):
                    continue
                # Skip if already in seed models (preserve configured settings)
                if mid in self._models:
                    logger.debug(f"Skipping {mid} - already in seed models")
                    continue
                # Tier heuristic
                tier = ModelTier.SMALL if ("mini" in mid or "small" in mid) else ModelTier.LARGE
                from . import ProviderType
                info = ModelInfo(
                    id=mid,
                    name=mid,
                    provider=ProviderType.OPENAI,  # Set provider correctly for discovered models
                    tier=tier,
                    context_window=128000,
                    cost_per_1k_prompt_tokens=0.0,
                    cost_per_1k_completion_tokens=0.0,
                    supports_tools=True,
                    supports_streaming=True,
                    available=True,
                )
                self._models[mid] = info
        except Exception as e:
            logger.info(f"OpenAI dynamic model discovery skipped: {e}")

    def list_models(self) -> List[ModelInfo]:
        """Return cached model list (seed + dynamic)."""
        return list(self._models.values())

    def _normalize_tool_calls_from_responses(self, response: Any) -> Optional[List[Dict[str, Any]]]:
        """Best-effort normalization for Responses API outputs with tool calls."""
        output = getattr(response, "output", None)
        if not output:
            return None
        normalized: List[Dict[str, Any]] = []
        try:
            for item in output:
                # Some SDKs emit objects; others dicts. Handle dicts here.
                if isinstance(item, dict) and item.get("type") == "tool_call":
                    name = item.get("name") or item.get("tool_name")
                    args = item.get("arguments") or item.get("input") or {}
                    normalized.append({
                        "type": "function",
                        "name": name,
                        "arguments": args,
                    })
                elif isinstance(item, dict) and item.get("type") == "message":
                    # Look for nested tool calls in content blocks
                    for block in item.get("content", []) or []:
                        if isinstance(block, dict) and block.get("type") in ("tool_call", "tool_calls"):
                            calls = block.get("calls") or [block]
                            for c in calls:
                                name = c.get("name")
                                args = c.get("arguments") or {}
                                normalized.append({
                                    "type": "function",
                                    "name": name,
                                    "arguments": args,
                                })
        except Exception:
            return normalized or None
        return normalized or None
    
    @retry(stop=stop_after_attempt(3), wait=wait_exponential(multiplier=1, min=2, max=10))
    async def generate_embedding(self, text: str, model: str = "text-embedding-3-small") -> List[float]:
        """Generate text embedding"""
        try:
            response = await self.client.embeddings.create(
                input=text,
                model=model
            )
            return response.data[0].embedding
        except Exception as e:
            logger.error(f"OpenAI embedding error: {e}")
            raise
    
    def calculate_cost(self, prompt_tokens: int, completion_tokens: int, model: str) -> float:
        """Calculate cost for OpenAI usage"""
        model_info = self._models.get(model)
        if not model_info:
            return 0.0
        
        prompt_cost = (prompt_tokens / 1000) * model_info.cost_per_1k_prompt_tokens
        completion_cost = (completion_tokens / 1000) * model_info.cost_per_1k_completion_tokens
        
        return round(prompt_cost + completion_cost, 6)
