"""Claim verification API for cross-referencing synthesis against citations."""

import logging
import json
import math
from typing import List, Dict, Any, Optional
from fastapi import APIRouter, Request
from pydantic import BaseModel, Field
from pydantic.config import ConfigDict

logger = logging.getLogger(__name__)

router = APIRouter()


class Citation(BaseModel):
    """Citation structure matching Go orchestrator."""
    model_config = ConfigDict(extra="ignore")

    url: str = ""
    title: str = ""
    source: str = ""
    content: Optional[str] = None
    snippet: Optional[str] = None
    credibility_score: float = 0.5
    quality_score: float = 0.5


class ClaimVerification(BaseModel):
    """Verification result for a single claim."""
    claim: str
    supporting_citations: List[int] = Field(default_factory=list)
    conflicting_citations: List[int] = Field(default_factory=list)
    confidence: float = 0.0


class ConflictReport(BaseModel):
    """Report of conflicting information between sources."""
    claim: str
    source1: int
    source1_text: str
    source2: int
    source2_text: str


class VerificationResult(BaseModel):
    """Overall verification result."""
    overall_confidence: float
    total_claims: int
    supported_claims: int
    unsupported_claims: List[str] = Field(default_factory=list)
    conflicts: List[ConflictReport] = Field(default_factory=list)
    claim_details: List[ClaimVerification] = Field(default_factory=list)


async def verify_claims(
    answer: str,
    citations: List[Dict[str, Any]],
    llm_client: Any
) -> VerificationResult:
    """
    Verify factual claims in synthesis against collected citations.

    Args:
        answer: Synthesis result containing claims
        citations: List of citation dicts from orchestrator
        llm_client: LLM client for claim extraction

    Returns:
        VerificationResult with confidence scores and unsupported claims
    """

    # Parse citations (be tolerant of partial/mismatched fields)
    citation_objs: List[Citation] = []
    for idx, raw in enumerate(citations or []):
        try:
            citation_objs.append(Citation(**(raw or {})))
        except Exception as e:
            logger.warning(f"[verification] Failed to parse citation[{idx}]: {e}")

    # Step 1: Extract factual claims using LLM
    claims = await _extract_claims(answer, llm_client)
    logger.info(f"[verification] Extracted {len(claims)} claims from synthesis")

    if not claims:
        return VerificationResult(
            overall_confidence=1.0,  # No claims = nothing to verify
            total_claims=0,
            supported_claims=0
        )

    # Step 2: Cross-reference each claim against citations
    claim_verifications = []
    for claim in claims:
        verification = await _verify_single_claim(claim, citation_objs, llm_client)
        claim_verifications.append(verification)

    # Step 3: Calculate aggregate metrics
    supported = sum(1 for cv in claim_verifications if cv.confidence >= 0.7)
    unsupported = [cv.claim for cv in claim_verifications if cv.confidence < 0.5]

    # Geometric mean: harsher on gaps than arithmetic
    if claim_verifications:
        mean_conf = sum(cv.confidence for cv in claim_verifications) / len(claim_verifications)
        coverage = supported / max(1, len(claim_verifications))
        overall_conf = math.sqrt(max(0.0, min(1.0, mean_conf)) * max(0.0, min(1.0, coverage)))
    else:
        overall_conf = 1.0

    # Step 4: Detect conflicts (claims with both supporting AND conflicting citations)
    conflicts = _detect_conflicts(claim_verifications, citation_objs)

    logger.info(f"[verification] Overall confidence: {overall_conf:.2f}, " +
                f"Supported: {supported}/{len(claim_verifications)}, " +
                f"Unsupported: {len(unsupported)}, Conflicts: {len(conflicts)}")

    return VerificationResult(
        overall_confidence=overall_conf,
        total_claims=len(claims),
        supported_claims=supported,
        unsupported_claims=unsupported,
        conflicts=conflicts,
        claim_details=claim_verifications
    )


async def _extract_claims(answer: str, providers: Any) -> List[str]:
    """Extract factual claims from synthesis using LLM."""

    prompt = f"""Extract all factual claims from the following text.
A factual claim is a statement that can be verified against sources.

Text:
{answer[:3000]}

Instructions:
1. Extract only factual claims (not opinions or interpretations)
2. Each claim should be a single, verifiable statement
3. Return as a numbered list
4. Limit to the 10 most important claims

Output format:
1. [First claim]
2. [Second claim]
...
"""

    try:
        # Use LLM to extract claims
        from llm_service.providers.base import ModelTier

        result = await providers.generate_completion(
            messages=[{"role": "user", "content": prompt}],
            tier=ModelTier.SMALL,
            max_tokens=8000,
            temperature=0.0  # Deterministic extraction
        )

        response = result.get("output_text", "")

        # Parse numbered list
        claims = []
        for line in response.split('\n'):
            line = line.strip()
            if line and len(line) > 3:
                # Match patterns like "1. ", "1) ", or just starting with digit
                if line[0].isdigit():
                    # Find the claim text after number and separator
                    for sep in ['. ', ') ', ': ']:
                        if sep in line:
                            claim = line.split(sep, 1)[1].strip()
                            if claim:
                                claims.append(claim)
                            break

        logger.debug(f"[verification] Extracted {len(claims)} claims")
        return claims[:10]  # Limit to top 10

    except Exception as e:
        logger.error(f"[verification] Failed to extract claims: {e}")
        return []


async def _verify_single_claim(
    claim: str,
    citations: List[Citation],
    providers: Any
) -> ClaimVerification:
    """Verify a single claim against available citations."""

    if not citations:
        return ClaimVerification(claim=claim, confidence=0.0)

    # Build citation context (limit content length)
    citation_context = "\n\n".join([
        f"[{i+1}] {(c.title or c.source or c.url)}\n{((c.content or c.snippet) or '')[:500]}"
        for i, c in enumerate(citations[:15])  # Limit to 15 citations
    ])

    prompt = f"""Verify the following claim against the provided sources.

Claim: {claim}

Sources:
{citation_context}

For each source, determine if it:
- SUPPORTS the claim (provides evidence for it)
- CONFLICTS with the claim (contradicts it)
- NEUTRAL (doesn't address the claim)

Output JSON format:
{{
    "supporting": [1, 3],  // Citation numbers that support
    "conflicting": [2],    // Citation numbers that conflict
    "confidence": 0.85     // 0.0-1.0 confidence in claim
}}

IMPORTANT: Only output the JSON, nothing else.
"""

    try:
        # Use LLM for verification
        from llm_service.providers.base import ModelTier

        result = await providers.generate_completion(
            messages=[{"role": "user", "content": prompt}],
            tier=ModelTier.SMALL,
            max_tokens=500,
            temperature=0.0
        )

        response = result.get("output_text", "")

        # Try to extract JSON from response
        response = response.strip()

        # Find JSON object in response
        json_start = response.find('{')
        json_end = response.rfind('}') + 1
        if json_start != -1 and json_end > json_start:
            json_str = response[json_start:json_end]
            result = json.loads(json_str)
        else:
            result = json.loads(response)

        supporting = result.get("supporting", [])
        conflicting = result.get("conflicting", [])
        base_confidence = result.get("confidence", 0.5)

        # Weight confidence by citation credibility and count with diminishing returns (log2)
        if supporting:
            valid_indices = [i-1 for i in supporting if 0 < i <= len(citations)]
            if valid_indices:
                credibility_weights = [citations[i].credibility_score for i in valid_indices]
                avg_cred = (sum(credibility_weights) / len(credibility_weights)) if credibility_weights else 0.5
                # Diminishing returns for multiple sources
                num_sources = len(valid_indices)
                bonus = min(0.25, 0.2 * math.log2(max(1, num_sources)))  # cap 25%
                confidence = base_confidence * avg_cred * (1.0 + bonus if num_sources > 1 else 1.0)
            else:
                confidence = base_confidence * 0.5
        else:
            confidence = 0.0

        # Weighted conflict penalty proportional to conflict strength
        if conflicting:
            conf_indices = [i-1 for i in conflicting if 0 < i <= len(citations)]
            sup_indices = [i-1 for i in supporting if 0 < i <= len(citations)]
            conflict_weight = sum(citations[i].credibility_score for i in conf_indices) if conf_indices else 0.0
            support_weight = sum(citations[i].credibility_score for i in sup_indices) if sup_indices else 0.0
            denom = conflict_weight + support_weight
            if denom > 0:
                conflict_ratio = conflict_weight / denom
                penalty = 0.3 * conflict_ratio  # up to -30%
            else:
                penalty = 0.2
            confidence *= (1.0 - penalty)

        # Clamp to [0,1]
        if confidence < 0:
            confidence = 0.0
        if confidence > 1:
            confidence = 1.0

        return ClaimVerification(
            claim=claim,
            supporting_citations=supporting,
            conflicting_citations=conflicting,
            confidence=confidence
        )

    except (json.JSONDecodeError, KeyError, IndexError, ValueError) as e:
        logger.warning(f"[verification] Failed to parse LLM response for claim: {e}")
        # Fallback: assume moderate confidence if we can't verify
        return ClaimVerification(claim=claim, confidence=0.5)
    except Exception as e:
        logger.error(f"[verification] Unexpected error verifying claim: {e}")
        return ClaimVerification(claim=claim, confidence=0.5)


def _detect_conflicts(
    verifications: List[ClaimVerification],
    citations: List[Citation]
) -> List[ConflictReport]:
    """Detect conflicting information across sources."""

    conflicts = []
    for v in verifications:
        if v.supporting_citations and v.conflicting_citations:
            # This claim has both supporting AND conflicting citations
            src1 = v.supporting_citations[0]
            src2 = v.conflicting_citations[0]

            if 0 < src1 <= len(citations) and 0 < src2 <= len(citations):
                conflicts.append(ConflictReport(
                    claim=v.claim,
                    source1=src1,
                    source1_text=citations[src1-1].title,
                    source2=src2,
                    source2_text=citations[src2-1].title
                ))

    return conflicts


# ======================================================================
# FastAPI Endpoint
# ======================================================================

class VerifyClaimsRequest(BaseModel):
    """Request body for claim verification endpoint."""
    answer: str
    citations: List[Dict[str, Any]]


@router.post("/api/verify_claims")
async def verify_claims_endpoint(request: Request, body: VerifyClaimsRequest):
    """
    Verify factual claims in synthesis against collected citations.

    POST /api/verify_claims
    {
        "answer": "synthesis text with claims",
        "citations": [{"url": "...", "title": "...", "content": "..."}]
    }

    Returns:
    {
        "overall_confidence": 0.82,
        "total_claims": 10,
        "supported_claims": 8,
        "unsupported_claims": ["unsupported claim text"],
        "conflicts": [],
        "claim_details": [...]
    }
    """
    try:
        # Get LLM providers from app state
        providers = request.app.state.providers

        # Use verify_claims function (it uses providers.generate_completion internally)
        result = await verify_claims(
            answer=body.answer,
            citations=body.citations,
            llm_client=providers  # Pass providers as llm_client
        )

        return result.dict()

    except Exception as e:
        logger.error(f"[verify_claims_endpoint] Error: {e}", exc_info=True)
        # Return a safe default response on error
        return VerificationResult(
            overall_confidence=0.5,
            total_claims=0,
            supported_claims=0,
            unsupported_claims=[],
            conflicts=[]
        ).dict()
