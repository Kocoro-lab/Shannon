import json
import logging

from fastapi import APIRouter, Request, HTTPException
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, Field
from typing import Any, List, Optional, Dict

from ..providers.base import ModelTier
from llm_provider.base import extract_text_from_content, sanitize_completion_messages
from ..metrics import metrics, TimedOperation

logger = logging.getLogger(__name__)

router = APIRouter()


class CompletionRequest(BaseModel):
    messages: List[Dict[str, Any]]
    model_tier: Optional[str] = Field(
        default="small", description="Model tier: small, medium, large"
    )
    specific_model: Optional[str] = Field(
        default=None, description="Specific model to use"
    )
    temperature: Optional[float] = Field(default=0.7, ge=0.0, le=2.0)
    max_tokens: Optional[int] = Field(default=8192, ge=1, le=32000)
    tools: Optional[List[dict]] = None
    cache_key: Optional[str] = None
    stream: Optional[bool] = Field(default=False, description="Enable SSE streaming")
    thinking: Optional[Dict[str, Any]] = Field(default=None, description="Anthropic extended thinking config")
    reasoning_effort: Optional[str] = Field(default=None, description="OpenAI reasoning effort (minimal/low/medium/high)")


@router.post("/")
async def generate_completion(request: Request, body: CompletionRequest):
    """Generate a completion using the LLM service"""
    providers = request.app.state.providers

    # If no providers are configured, return a simple mock response to keep local dev flows working
    try:
        if not providers or not providers.is_configured():
            # Simple deterministic mock based on last user message
            user_text = ""
            for m in reversed(body.messages or []):
                if m.get("role") == "user":
                    user_text = extract_text_from_content(m.get("content", ""))
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

    # Convert tier string to enum
    tier = None
    if body.model_tier:
        try:
            tier = ModelTier(body.model_tier.lower())
        except ValueError:
            raise HTTPException(
                status_code=400, detail=f"Invalid model tier: {body.model_tier}"
            )

    # Defensive sanitization: strip malformed history entries that cause provider 400/500s
    original_count = len(body.messages)
    body.messages = sanitize_completion_messages(body.messages)
    if len(body.messages) != original_count:
        logger.warning(
            f"Sanitized completion messages: {original_count} → {len(body.messages)} "
            f"(dropped {original_count - len(body.messages)} malformed entries)"
        )

    if body.stream:
        return StreamingResponse(
            _stream_completion(request, body, providers, tier),
            media_type="text/event-stream",
            headers={
                "Cache-Control": "no-cache",
                "Connection": "keep-alive",
                "X-Accel-Buffering": "no",
            },
        )

    with TimedOperation("llm_completion", "llm") as timer:
        try:
            wf_id = (
                request.headers.get("X-Parent-Workflow-ID")
                or request.headers.get("X-Workflow-ID")
                or request.headers.get("x-workflow-id")
            )
            ag_id = request.headers.get("X-Agent-ID") or request.headers.get(
                "x-agent-id"
            )
            # Generate completion
            result = await providers.generate_completion(
                messages=body.messages,
                tier=tier,
                specific_model=body.specific_model,
                temperature=body.temperature,
                max_tokens=body.max_tokens,
                tools=body.tools,
                workflow_id=wf_id,
                agent_id=ag_id,
                thinking=body.thinking,
                reasoning_effort=body.reasoning_effort,
                cache_source="completions_proxy",
            )
        except Exception as e:
            metrics.record_error("CompletionError", "llm")
            raise HTTPException(status_code=500, detail=str(e))

    # After timing context exits, record metrics with final duration
    usage = result.get("usage", {}) or {}
    prompt_tokens = usage.get("input_tokens", usage.get("prompt_tokens", 0))
    completion_tokens = usage.get("output_tokens", usage.get("completion_tokens", 0))
    cost = usage.get("cost_usd", usage.get("cost", 0.0))

    # Check if response was cached (manager sets this flag)
    cache_hit = result.get("cached", False)

    metrics.record_llm_request(
        provider=result.get("provider", "unknown"),
        model=result.get("model", "unknown"),
        tier=body.model_tier or "unknown",
        cache_hit=cache_hit,
        duration=timer.duration or 0.0,
        prompt_tokens=prompt_tokens,
        completion_tokens=completion_tokens,
        cost=cost,
    )

    return result


async def _stream_completion(request, body, providers, tier):
    """SSE generator for streaming completions."""
    import time

    start_time = time.time()
    full_text = ""
    final_meta = None

    try:
        async for chunk in providers.stream_completion(
            messages=body.messages,
            tier=tier,
            specific_model=body.specific_model,
            temperature=body.temperature,
            max_tokens=body.max_tokens,
            tools=body.tools,
            thinking=body.thinking,
            reasoning_effort=body.reasoning_effort,
            cache_source="completions_proxy_stream",
        ):
            if isinstance(chunk, str):
                full_text += chunk
                event = {"type": "content_delta", "text": chunk}
                yield f"data: {json.dumps(event)}\n\n"
            elif isinstance(chunk, dict):
                # Final metadata from provider (usage, model, function_calls)
                final_meta = chunk

        # Build the done event with full response
        done_event = {
            "type": "done",
            "output_text": full_text,
        }

        if final_meta:
            done_event["provider"] = final_meta.get("provider", "unknown")
            done_event["model"] = final_meta.get("model", "unknown")
            done_event["usage"] = final_meta.get("usage")
            if final_meta.get("finish_reason"):
                done_event["finish_reason"] = final_meta["finish_reason"]
            if final_meta.get("function_call"):
                done_event["function_call"] = final_meta["function_call"]
            if final_meta.get("function_calls"):
                done_event["tool_calls"] = final_meta["function_calls"]

        yield f"data: {json.dumps(done_event)}\n\n"
        yield "data: [DONE]\n\n"

        # Record metrics
        duration = time.time() - start_time
        usage = (final_meta or {}).get("usage", {})
        metrics.record_llm_request(
            provider=(final_meta or {}).get("provider", "unknown"),
            model=(final_meta or {}).get("model", "unknown"),
            tier=body.model_tier or "unknown",
            cache_hit=False,
            duration=duration,
            prompt_tokens=usage.get("input_tokens", 0),
            completion_tokens=usage.get("output_tokens", 0),
            cost=usage.get("cost_usd", 0.0),
        )

    except Exception as e:
        logger.error(f"Streaming completion error: {e}", exc_info=True)
        error_event = {"type": "error", "message": str(e)}
        yield f"data: {json.dumps(error_event)}\n\n"
        yield "data: [DONE]\n\n"
