from fastapi import APIRouter, Request, HTTPException
from pydantic import BaseModel, Field
from typing import List, Optional, Dict, Any

from ..providers.base import ModelTier
from ..metrics import metrics, TimedOperation

router = APIRouter()

class CompletionRequest(BaseModel):
    messages: List[Dict[str, str]]
    model_tier: Optional[str] = Field(default="small", description="Model tier: small, medium, large")
    specific_model: Optional[str] = Field(default=None, description="Specific model to use")
    temperature: Optional[float] = Field(default=0.7, ge=0.0, le=2.0)
    max_tokens: Optional[int] = Field(default=2000, ge=1, le=32000)
    tools: Optional[List[dict]] = None
    cache_key: Optional[str] = None

@router.post("/")
async def generate_completion(request: Request, body: CompletionRequest):
    """Generate a completion using the LLM service"""
    cache = request.app.state.cache
    providers = request.app.state.providers
    
    # If no providers are configured, return a simple mock response to keep local dev flows working
    try:
        if not providers or not providers.is_configured():
            # Simple deterministic mock based on last user message
            user_text = ""
            for m in reversed(body.messages or []):
                if m.get("role") == "user":
                    user_text = m.get("content") or ""
                    break
            reply = (
                "(mock) I received your request and would normally ask the configured LLM. "
                "No providers are configured, so this is a placeholder response."
            )
            # Minimal usage estimate
            prompt_tokens = max(len(user_text.split()), 1)
            completion_tokens = max(len(reply.split()), 1)
            total_tokens = prompt_tokens + completion_tokens
            result = {
                "provider": "mock",
                "model": "mock-model-v1",
                "output_text": reply,
                "usage": {
                    "input_tokens": prompt_tokens,
                    "output_tokens": completion_tokens,
                    "total_tokens": total_tokens,
                    "cost_usd": 0.0,
                },
                "cache_hit": False,
            }
            return result
    except Exception:
        # If provider check fails unexpectedly, fall through to normal path which will error gracefully
        pass
    
    # Check cache first
    cache_result = await cache.get(
        messages=body.messages,
        model=body.specific_model or body.model_tier,
        temperature=body.temperature,
        max_tokens=body.max_tokens
    )
    
    if cache_result:
        cache_result["cache_hit"] = True
        # Record cache hit metrics
        usage = cache_result.get("usage", {}) or {}
        prompt_tokens = usage.get("input_tokens", usage.get("prompt_tokens", 0))
        completion_tokens = usage.get("output_tokens", usage.get("completion_tokens", 0))
        metrics.record_llm_request(
            provider=cache_result.get("provider", "unknown"),
            model=cache_result.get("model", "unknown"),
            tier=body.model_tier or "unknown",
            cache_hit=True,
            duration=0.0,
            prompt_tokens=prompt_tokens,
            completion_tokens=completion_tokens,
            cost=0.0,
        )
        return cache_result
    
    # Convert tier string to enum
    tier = None
    if body.model_tier:
        try:
            tier = ModelTier(body.model_tier.lower())
        except ValueError:
            raise HTTPException(status_code=400, detail=f"Invalid model tier: {body.model_tier}")
    
    with TimedOperation("llm_completion", "llm") as timer:
        try:
            wf_id = request.headers.get('X-Parent-Workflow-ID') or request.headers.get('X-Workflow-ID') or request.headers.get('x-workflow-id')
            ag_id = request.headers.get('X-Agent-ID') or request.headers.get('x-agent-id')
            # Generate completion
            result = await providers.generate_completion(
                messages=body.messages,
                tier=tier,
                specific_model=body.specific_model,
                temperature=body.temperature,
                max_tokens=body.max_tokens,
                tools=body.tools,
                workflow_id=wf_id,
                agent_id=ag_id
            )
        except Exception as e:
            metrics.record_error("CompletionError", "llm")
            raise HTTPException(status_code=500, detail=str(e))

    # After timing context exits, record metrics with final duration
    usage = result.get("usage", {}) or {}
    prompt_tokens = usage.get("input_tokens", usage.get("prompt_tokens", 0))
    completion_tokens = usage.get("output_tokens", usage.get("completion_tokens", 0))
    cost = usage.get("cost_usd", usage.get("cost", 0.0))

    metrics.record_llm_request(
        provider=result.get("provider", "unknown"),
        model=result.get("model", "unknown"),
        tier=body.model_tier or "unknown",
        cache_hit=False,
        duration=timer.duration or 0.0,
        prompt_tokens=prompt_tokens,
        completion_tokens=completion_tokens,
        cost=cost
    )

    # Cache the result
    await cache.set(
        messages=body.messages,
        model=result.get("model", body.specific_model or body.model_tier or "unknown"),
        response=result,
        temperature=body.temperature,
        max_tokens=body.max_tokens
    )
    return result
