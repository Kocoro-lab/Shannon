import json
import logging
import re
from typing import Dict, List, Optional

from fastapi import APIRouter, Request
from pydantic import BaseModel, Field

from ..providers.base import ModelTier

router = APIRouter()
logger = logging.getLogger(__name__)


class MemoryExtractRequest(BaseModel):
    query: str = Field(..., description="Original user query")
    result: str = Field(..., description="Task result text to extract from")


class MemoryExtractResponse(BaseModel):
    worth_remembering: bool = Field(default=False)
    title: str = Field(default="")
    summary: str = Field(default="")
    content: str = Field(default="")
    suggested_path: str = Field(default="")
    tags: List[str] = Field(default_factory=list)
    triples: List[Dict[str, str]] = Field(default_factory=list)


def _extract_json_block(text: str) -> Optional[dict]:
    """Extract JSON from model response, handling fenced blocks."""
    try:
        return json.loads(text)
    except Exception:
        pass
    fence = re.search(r"```(?:json)?\s*(\{[\s\S]*?\})\s*```", text, re.IGNORECASE)
    blob = fence.group(1) if fence else None
    if not blob:
        brace = re.search(r"\{[\s\S]*\}", text)
        blob = brace.group(0) if brace else None
    if not blob:
        return None
    try:
        return json.loads(blob)
    except Exception:
        return None


_SYSTEM_PROMPT = """You are a memory extraction specialist. Given a user query and its result, decide whether the result contains knowledge worth persisting to the user's long-term memory.

## When to remember:
- User preferences and explicit decisions that recur across sessions
- Reusable factual knowledge (entity relationships, naming, responsibilities, constraints)
- Technical patterns, architecture choices, and configuration defaults that are likely to repeat
- Named entity relationships (people/teams/tools/services/domains and how they connect)
- General domain facts that are broadly reusable and stable over time

## When NOT to remember:
- One-off research findings or ad-hoc experiments
- Transient facts (stock prices, build numbers, timestamps, debug logs, temporary IDs)
- Pure confirmations/acknowledgments without reusable implications
- Very short or empty results

## Extraction target:
Always extract stable facts as a list of triples in the `triples` field.
Each triple is `{"h": "...", "r": "...", "t": "..."}` and should be:
- concise, normalized, and lowercase snake_case where practical
- specific, high-signal, and reusable
- non-duplicate if the same fact appears multiple times

## Response format (JSON only):
{
  "worth_remembering": true/false,
  "title": "Short descriptive title (3-8 words)",
  "summary": "2-3 bullet points (markdown `- ` prefix) capturing the key facts, so an agent can decide whether to read the full file without opening it. Each bullet should be a specific, concrete detail — not a vague description. Max 400 chars total.",
  "content": "Markdown content to persist (key facts, bullet points, etc.)",
  "suggested_path": "filename.md (lowercase, hyphens, no directories)",
  "tags": ["tag1", "tag2"],
  "triples": [{"h": "...", "r": "...", "t": "..."}]
}

If worth_remembering is false, other fields can be empty strings."""


@router.post("/extract", response_model=MemoryExtractResponse)
async def extract_memory(request: Request, body: MemoryExtractRequest):
    """Extract memorable knowledge from a task result."""
    workflow_id = request.headers.get("X-Workflow-ID", "")
    agent_id = request.headers.get("X-Agent-ID", "")

    providers = getattr(request.app.state, "providers", None)

    # If no providers configured, can't extract — return not worth remembering
    if not providers or not providers.is_configured():
        logger.debug("memory.extract: no providers configured", extra={
            "workflow_id": workflow_id, "agent_id": agent_id,
        })
        return MemoryExtractResponse(worth_remembering=False)

    # Truncate inputs to control cost
    query = body.query[:1000]
    result = body.result[:4000]

    user_content = f"## User Query:\n{query}\n\n## Task Result:\n{result}"

    try:
        llm_result = await providers.generate_completion(
            messages=[
                {"role": "system", "content": _SYSTEM_PROMPT},
                {"role": "user", "content": user_content},
            ],
            tier=ModelTier.SMALL,
            max_tokens=1500,
            temperature=0.0,
            response_format={"type": "json_object"},
        )

        raw = llm_result.get("output_text", "")
        data = _extract_json_block(raw)
        if not data:
            logger.warning("memory.extract: failed to parse LLM response", extra={
                "workflow_id": workflow_id, "agent_id": agent_id,
            })
            return MemoryExtractResponse(worth_remembering=False)

        worth = bool(data.get("worth_remembering", False))
        logger.info("memory.extract: done", extra={
            "workflow_id": workflow_id, "agent_id": agent_id,
            "worth_remembering": worth,
            "title": str(data.get("title", ""))[:60],
        })

        return MemoryExtractResponse(
            worth_remembering=worth,
            title=str(data.get("title", "")),
            summary=str(data.get("summary", "")),
            content=str(data.get("content", "")),
            suggested_path=str(data.get("suggested_path", "")),
            tags=list(data.get("tags", [])),
            triples=list(data.get("triples", [])),
        )

    except Exception as e:
        logger.warning("memory.extract: provider error: %s", e, extra={
            "workflow_id": workflow_id, "agent_id": agent_id,
        })
        return MemoryExtractResponse(worth_remembering=False)
