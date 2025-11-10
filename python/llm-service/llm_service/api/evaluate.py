from fastapi import APIRouter, Request
from pydantic import BaseModel, Field
from typing import List, Optional, Dict, Any
import json
import re

router = APIRouter()


class EvalAgentResult(BaseModel):
    agent_id: str
    response: str = ""
    success: bool = True
    error: Optional[str] = None


class EvaluationRequest(BaseModel):
    original_query: str
    results: List[EvalAgentResult]
    context: Optional[Dict[str, Any]] = Field(default_factory=dict)


class EvaluationResponse(BaseModel):
    should_replan: bool
    reason: str = ""
    issues: List[str] = Field(default_factory=list)
    hint: str = ""


def _heuristic_evaluate(body: EvaluationRequest) -> EvaluationResponse:
    if not body.results:
        return EvaluationResponse(
            should_replan=True,
            reason="No agent results",
            issues=["empty_results"],
            hint="Regenerate plan with different subtasks",
        )

    # If any agent failed hard or produced empty output, suggest replanning
    failures = [
        r
        for r in body.results
        if (not r.success) or (not r.response.strip()) or (r.error and r.error.strip())
    ]
    if failures:
        return EvaluationResponse(
            should_replan=True,
            reason="One or more subtasks failed or returned empty output",
            issues=["task_failure"],
            hint="Adjust subtasks or ordering",
        )

    # Very short combined output -> likely poor quality
    total_chars = sum(len(r.response) for r in body.results)
    if total_chars < 200:
        return EvaluationResponse(
            should_replan=True,
            reason="Results appear too short",
            issues=["low_content"],
            hint="Increase depth or add validation subtask",
        )

    # Default: no replanning needed
    return EvaluationResponse(should_replan=False, reason="Sufficient quality detected")


def _extract_json_block(text: str) -> Optional[Dict[str, Any]]:
    try:
        return json.loads(text)
    except Exception:
        pass
    m = re.search(r"```(?:json)?\s*(\{[\s\S]*?\})\s*```", text, re.IGNORECASE)
    if m:
        try:
            return json.loads(m.group(1))
        except Exception:
            return None
    m = re.search(r"\{[\s\S]*\}", text)
    if m:
        try:
            return json.loads(m.group(0))
        except Exception:
            return None
    return None


@router.post("/agent/evaluate", response_model=EvaluationResponse)
async def evaluate_results(
    request: Request, body: EvaluationRequest
) -> EvaluationResponse:
    providers = getattr(request.app.state, "providers", None)

    # If no providers configured, use heuristics
    if not providers or not providers.is_configured():
        return _heuristic_evaluate(body)

    # Build model prompt with comprehensive quality criteria
    sys = (
        "You are a research quality evaluator assessing the completeness and accuracy of research findings.\n\n"
        "# Evaluation Criteria:\n\n"
        "## Coverage (40%):\n"
        "- Does the answer directly address the user's query?\n"
        "- Are context, subtopics, and nuances covered?\n"
        "- Are critical aspects addressed comprehensively?\n\n"
        "## Evidence Quality (30%):\n"
        "- Are claims supported by inline citations [1], [2]?\n"
        "- Are sources authoritative (.gov, .edu, peer-reviewed, reputable)?\n"
        "- Is source diversity adequate (multiple perspectives)?\n\n"
        "## Citation Integrity (20%):\n"
        "- Are all citations numbered sequentially without gaps?\n"
        "- Are all cited sources included in a Sources section?\n"
        "- Are URLs valid and complete?\n\n"
        "## Clarity (10%):\n"
        "- Is the report well-structured with clear headings?\n"
        "- Are findings presented clearly with bullet points?\n"
        "- Are conflicts or uncertainties flagged explicitly?\n\n"
        "# Pass/Fail Thresholds:\n"
        "- PASS: Coverage ≥80%, Evidence ≥80%, Citations ≥90%, Clarity ≥70%\n"
        "- FAIL: Provide specific feedback on gaps\n\n"
        "# Response Format:\n"
        'Return JSON only: {"should_replan": bool, "reason": string, "issues": [string], "hint": string}\n'
        "If revisions needed:\n"
        "1. What's missing or unclear?\n"
        "2. Which claims lack citations?\n"
        "3. What sources need verification?\n"
    )
    # Summarize results compactly
    max_items = 6
    parts = []
    for i, r in enumerate(body.results[:max_items]):
        status = "ok" if r.success else f"fail:{r.error or ''}"
        snippet = (r.response or "").strip()
        if len(snippet) > 200:
            snippet = snippet[:200] + "..."
        parts.append(f"[{r.agent_id} {status}] {snippet}")
    user = f"Query: {body.original_query}\nResults:\n" + "\n".join(parts)

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
            max_tokens=250,
            temperature=0.0,
            response_format={"type": "json_object"},
            workflow_id=wf_id,
            agent_id=ag_id,
        )
        raw = result.get("output_text", "")
        data = _extract_json_block(raw)
        if not data:
            return _heuristic_evaluate(body)
        should = bool(data.get("should_replan", False))
        reason = str(data.get("reason", ""))
        issues = (
            list(data.get("issues", [])) if isinstance(data.get("issues"), list) else []
        )
        hint = str(data.get("hint", ""))
        return EvaluationResponse(
            should_replan=should, reason=reason, issues=issues, hint=hint
        )
    except Exception:
        return _heuristic_evaluate(body)
