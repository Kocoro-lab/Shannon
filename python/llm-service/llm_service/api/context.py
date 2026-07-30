import logging
from fastapi import APIRouter, Request, HTTPException
from pydantic import BaseModel, Field
from typing import List, Dict, Any, Optional

router = APIRouter()
logger = logging.getLogger(__name__)


class Message(BaseModel):
    role: str
    content: str


class CompressRequest(BaseModel):
    messages: List[Message]
    target_tokens: Optional[int] = Field(default=400, ge=64, le=2000)
    compress_all_messages: bool = False


class CompressResponse(BaseModel):
    summary: str
    tokens_saved: int = 0
    model_used: str = "unknown"
    usage: Dict[str, Any] = Field(default_factory=dict)


def _messages_to_compress(body: CompressRequest) -> List[Message]:
    """Keep legacy callers on the prior 20-message request scope."""
    if body.compress_all_messages:
        return body.messages
    return body.messages[-20:]


@router.post("/context/compress", response_model=CompressResponse)
async def compress_context(request: Request, body: CompressRequest) -> CompressResponse:
    providers = getattr(request.app.state, "providers", None)
    if not body.messages:
        raise HTTPException(status_code=400, detail="messages required")

    messages = _messages_to_compress(body)

    # If no providers, fallback to heuristic: join and truncate
    if not providers or not providers.is_configured():
        joined = "\n".join([m.content for m in messages])
        # Very simple heuristic compression
        summary = joined[: min(len(joined), 1000)]
        if len(joined) > 1000:
            summary += "..."
        return CompressResponse(
            summary=summary, tokens_saved=max(0, len(joined) - len(summary))
        )

    # Build prompt for compact summary
    sys = (
        "You compress long conversations into a concise, factual summary. "
        "Capture key decisions, entities, intents, and unresolved items."
    )
    user = "\n".join([f"{m.role}: {m.content}" for m in messages])

    try:
        from ..providers.base import ModelTier

        wf_id = (
            request.headers.get("X-Parent-Workflow-ID")
            or request.headers.get("X-Workflow-ID")
            or request.headers.get("x-workflow-id")
        )
        ag_id = request.headers.get("X-Agent-ID") or request.headers.get("x-agent-id")

        result = await providers.generate_completion(
            messages=[
                {"role": "system", "content": sys},
                {"role": "user", "content": user},
            ],
            tier=ModelTier.SMALL,
            max_tokens=int(body.target_tokens or 400),
            temperature=0.2,
            workflow_id=wf_id,
            agent_id=ag_id,
            cache_source="context_summary",
        )
        completion = (result.get("output_text") or "").strip()
        usage = result.get("usage", {})
        tokens = usage.get("total_tokens", 0)
        est_original = max(tokens * 2, 1000)  # loose heuristic
        saved = max(0, est_original - tokens)
        return CompressResponse(
            summary=completion or "",
            tokens_saved=saved,
            model_used=result.get("model", "unknown"),
            usage=usage,
        )
    except Exception as exc:
        logger.error("Context compression failed: %s", type(exc).__name__)
        raise HTTPException(
            status_code=500,
            detail="context compression failed",
        ) from None
