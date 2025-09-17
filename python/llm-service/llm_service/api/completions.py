from fastapi import APIRouter, Request, HTTPException
from pydantic import BaseModel, Field
from typing import List, Optional, Dict

from ..providers.base import ModelTier
from ..metrics import metrics, TimedOperation

router = APIRouter()


class CompletionRequest(BaseModel):
    messages: List[Dict[str, str]]
    model_tier: Optional[str] = Field(
        default="small", description="Model tier: small, medium, large"
    )
    specific_model: Optional[str] = Field(
        default=None, description="Specific model to use"
    )
    temperature: Optional[float] = Field(default=0.7, ge=0.0, le=2.0)
    max_tokens: Optional[int] = Field(default=2000, ge=1, le=32000)
    tools: Optional[List[dict]] = None
    cache_key: Optional[str] = None


class CompletionResponse(BaseModel):
    completion: str
    model_used: str
    provider: str
    usage: dict
    cache_hit: bool
    finish_reason: str


@router.post("/", response_model=CompletionResponse)
async def generate_completion(request: Request, body: CompletionRequest):
    """Generate a completion using the LLM service"""
    cache = request.app.state.cache
    providers = request.app.state.providers

    # Check cache first
    cache_result = await cache.get(
        messages=body.messages,
        model=body.specific_model or body.model_tier,
        temperature=body.temperature,
        max_tokens=body.max_tokens,
    )

    if cache_result:
        cache_result["cache_hit"] = True

        # Record cache hit metrics
        usage = cache_result.get("usage", {})
        prompt_tokens = usage.get("prompt_tokens", 0)
        completion_tokens = usage.get("completion_tokens", 0)
        cost = usage.get("cost", 0.0)

        metrics.record_llm_request(
            provider=cache_result.get("provider", "unknown"),
            model=cache_result.get("model_used", "unknown"),
            tier=body.model_tier or "unknown",
            cache_hit=True,
            duration=0.0,  # Cache hits are essentially instant
            prompt_tokens=prompt_tokens,
            completion_tokens=completion_tokens,
            cost=0.0,  # Cache hits don't incur cost
        )

        return CompletionResponse(**cache_result)

    # Convert tier string to enum
    tier = None
    if body.model_tier:
        try:
            tier = ModelTier(body.model_tier.lower())
        except ValueError:
            raise HTTPException(
                status_code=400, detail=f"Invalid model tier: {body.model_tier}"
            )

    with TimedOperation("llm_completion", "llm") as timer:
        try:
            # Generate completion
            result = await providers.generate_completion(
                messages=body.messages,
                tier=tier,
                specific_model=body.specific_model,
                temperature=body.temperature,
                max_tokens=body.max_tokens,
                tools=body.tools,
            )
        except Exception as e:
            metrics.record_error("CompletionError", "llm")
            raise HTTPException(status_code=500, detail=str(e))

    # After timing context exits, record metrics with final duration
    usage = result.get("usage", {})
    prompt_tokens = usage.get("prompt_tokens", 0)
    completion_tokens = usage.get("completion_tokens", 0)
    cost = usage.get("cost", 0.0)

    metrics.record_llm_request(
        provider=result.get("provider", "unknown"),
        model=result.get("model_used", "unknown"),
        tier=body.model_tier or "unknown",
        cache_hit=False,
        duration=timer.duration or 0.0,
        prompt_tokens=prompt_tokens,
        completion_tokens=completion_tokens,
        cost=cost,
    )

    # Cache the result
    await cache.set(
        messages=body.messages,
        model=result["model_used"],
        response=result,
        temperature=body.temperature,
        max_tokens=body.max_tokens,
    )

    return CompletionResponse(**result)
