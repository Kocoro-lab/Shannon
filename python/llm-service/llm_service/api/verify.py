"""Claim verification API for cross-referencing synthesis against citations."""

import logging
import json
import math
import re
from collections import Counter
from typing import List, Dict, Any, Optional, Tuple
from fastapi import APIRouter, Request
from pydantic import BaseModel, Field
from pydantic.config import ConfigDict

logger = logging.getLogger(__name__)

# CJK character pattern for language detection and tokenization
CJK_PATTERN = re.compile(r'[\u4e00-\u9fff\u3040-\u309f\u30a0-\u30ff]')

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


# ============================================================================
# V2 Models with three-category classification
# ============================================================================

class ClaimVerificationV2(BaseModel):
    """V2 Verification result with three-category classification."""
    claim: str
    verdict: str = "insufficient_evidence"  # "supported" | "unsupported" | "insufficient_evidence"
    supporting_citations: List[int] = Field(default_factory=list)
    conflicting_citations: List[int] = Field(default_factory=list)
    confidence: float = 0.0
    retrieval_scores: Dict[int, float] = Field(default_factory=dict)  # citation_id → relevance
    reasoning: str = ""


class VerificationResultV2(BaseModel):
    """V2 Overall verification result with three-category breakdown."""
    overall_confidence: float
    total_claims: int

    # Three-category counts
    supported_claims: int
    unsupported_claims: int
    insufficient_evidence_claims: int

    # Lists for each category
    supported_claim_texts: List[str] = Field(default_factory=list)
    unsupported_claim_texts: List[str] = Field(default_factory=list)
    insufficient_claim_texts: List[str] = Field(default_factory=list)

    # Details
    claim_details: List[ClaimVerificationV2] = Field(default_factory=list)
    conflicts: List[ConflictReport] = Field(default_factory=list)

    # Quality metrics
    evidence_coverage: float = 0.0  # % of claims with relevant citations found
    avg_retrieval_score: float = 0.0  # Average top-1 retrieval relevance


# ============================================================================
# V2 Helper Functions: Language Detection & BM25 Retrieval
# ============================================================================

def detect_language(text: str) -> str:
    """
    Detect if text is primarily CJK (Chinese/Japanese/Korean) or Latin-based.

    Args:
        text: Text to analyze (first ~500 chars recommended)

    Returns:
        "zh" for CJK-dominant text, "en" otherwise
    """
    if not text:
        return "en"

    # Count CJK characters vs total alphanumeric
    cjk_count = len(CJK_PATTERN.findall(text))
    # Count Latin letters
    latin_count = len(re.findall(r'[a-zA-Z]', text))

    total = cjk_count + latin_count
    if total == 0:
        return "en"

    # If >30% CJK, treat as CJK-dominant
    if cjk_count / total > 0.3:
        return "zh"
    return "en"


def tokenize(text: str) -> List[str]:
    """
    Tokenize text supporting both CJK (character-level) and Latin (word-level).

    Args:
        text: Text to tokenize

    Returns:
        List of tokens (CJK chars + Latin words)
    """
    if not text:
        return []

    tokens = []

    # CJK: character-level tokenization
    for char in text:
        if CJK_PATTERN.match(char):
            tokens.append(char)

    # Latin: word-level tokenization (lowercase)
    words = re.findall(r'\b\w+\b', text.lower())
    tokens.extend(words)

    return tokens


class CorpusStats:
    """P1-5: Corpus statistics for IDF calculation."""
    def __init__(self):
        self.doc_freq: Dict[str, int] = {}  # term → number of documents containing it
        self.total_docs: int = 0
        self.avg_doc_len: float = 200.0

    @classmethod
    def from_citations(cls, citations: List["Citation"]) -> "CorpusStats":
        """Compute corpus statistics from a list of citations."""
        stats = cls()
        stats.total_docs = len(citations)

        if not citations:
            return stats

        total_tokens = 0
        for c in citations:
            text = f"{c.title or ''} {c.content or c.snippet or ''}"
            tokens = tokenize(text)
            total_tokens += len(tokens)

            # Count document frequency (each term counted once per document)
            unique_terms = set(tokens)
            for term in unique_terms:
                stats.doc_freq[term] = stats.doc_freq.get(term, 0) + 1

        if stats.total_docs > 0:
            stats.avg_doc_len = total_tokens / stats.total_docs
            if stats.avg_doc_len <= 0:
                stats.avg_doc_len = 200.0

        return stats


def bm25_score(
    query_tokens: List[str],
    doc_tokens: List[str],
    k1: float = 1.5,
    b: float = 0.75,
    avg_doc_len: float = 200.0,
    corpus_stats: Optional[CorpusStats] = None
) -> float:
    """
    Compute BM25 score between query and document.

    P1-5: Now includes IDF weighting when corpus_stats is provided.

    Args:
        query_tokens: Tokenized query (claim)
        doc_tokens: Tokenized document (citation content)
        k1: Term frequency saturation parameter
        b: Length normalization parameter
        avg_doc_len: Estimated average document length (used if corpus_stats not provided)
        corpus_stats: Optional corpus statistics for IDF calculation

    Returns:
        BM25 relevance score (higher = more relevant)
    """
    if not query_tokens or not doc_tokens:
        return 0.0

    doc_freq = Counter(doc_tokens)
    doc_len = len(doc_tokens)

    # Use corpus stats if available
    if corpus_stats and corpus_stats.total_docs > 0:
        avg_doc_len = corpus_stats.avg_doc_len
        N = corpus_stats.total_docs
    else:
        N = 0  # No IDF if no corpus stats
    if avg_doc_len <= 0:
        avg_doc_len = 200.0

    score = 0.0
    for term in set(query_tokens):
        tf = doc_freq.get(term, 0)
        if tf > 0:
            # TF component (BM25 saturation)
            tf_component = (tf * (k1 + 1)) / (tf + k1 * (1 - b + b * doc_len / avg_doc_len))

            # IDF component (P1-5: proper inverse document frequency)
            if N > 0 and corpus_stats:
                n = corpus_stats.doc_freq.get(term, 0)
                # BM25 IDF formula: log((N - n + 0.5) / (n + 0.5) + 1)
                # The +1 inside log prevents negative scores for very common terms
                idf = math.log((N - n + 0.5) / (n + 0.5) + 1)
            else:
                idf = 1.0  # No IDF weighting without corpus stats

            score += idf * tf_component

    return score


def retrieve_relevant_citations(
    claim: str,
    citations: List[Citation],
    top_k: int = 5,
    corpus_stats: Optional[CorpusStats] = None
) -> List[Tuple[int, Citation, float]]:
    """
    Retrieve top-k most relevant citations for a claim using BM25 scoring.

    P1-5: Now uses IDF weighting via corpus_stats for better precision.

    Args:
        claim: The claim to find evidence for
        citations: List of all available citations
        top_k: Number of top citations to return
        corpus_stats: Optional pre-computed corpus stats (computed if not provided)

    Returns:
        List of (original_1based_index, citation, relevance_score) tuples
    """
    if not citations or not claim:
        return []

    # Tokenize claim
    claim_tokens = tokenize(claim)
    if not claim_tokens:
        return []

    # P1-5: Compute corpus stats if not provided
    if corpus_stats is None:
        corpus_stats = CorpusStats.from_citations(citations)

    # Score each citation
    scored: List[Tuple[int, Citation, float]] = []
    for idx, c in enumerate(citations):
        # Combine title + content/snippet for matching
        citation_text = f"{c.title or ''} {c.content or c.snippet or ''}"
        citation_tokens = tokenize(citation_text)

        # BM25 scoring with IDF
        score = bm25_score(claim_tokens, citation_tokens, corpus_stats=corpus_stats)

        # Boost by credibility (0.5 baseline + 0.5 * credibility)
        score *= (0.5 + 0.5 * c.credibility_score)

        # 1-indexed for citation references
        scored.append((idx + 1, c, score))

    # Sort by score descending, take top-k
    scored.sort(key=lambda x: x[2], reverse=True)

    # Filter out zero-score citations
    scored = [(idx, c, s) for idx, c, s in scored if s > 0]

    return scored[:top_k]


def _extract_cited_numbers(text: str, max_citations: int, limit: int = 50) -> set:
    """
    Extract citation numbers referenced in text (e.g., [1], [37]).

    Args:
        text: Text potentially containing citation references
        max_citations: Maximum valid citation number (len(citations))
        limit: Maximum number of unique citations to extract (prevent OOM)

    Returns:
        Set of valid citation numbers found in text
    """
    cited_numbers = set()
    for match in re.findall(r'\[(\d+)\]', text):
        try:
            num = int(match)
            if 0 < num <= max_citations:
                cited_numbers.add(num)
            if len(cited_numbers) >= limit:
                break
        except ValueError:
            continue
    return cited_numbers


def _build_relevant_citations(
    citations: List["Citation"],
    cited_numbers: set,
    min_count: int = 10,
    max_count: int = 25
) -> List[Tuple[int, "Citation"]]:
    """
    Build a list of relevant citations preserving original indices.

    Args:
        citations: Full list of Citation objects
        cited_numbers: Set of citation numbers actually referenced in answer
        min_count: Minimum citations to include (pad with top-K if needed)
        max_count: Maximum citations to include

    Returns:
        List of (original_index, citation) tuples
    """
    relevant = []

    # First, add all actually cited citations (preserving original index)
    for num in sorted(cited_numbers):
        if num <= len(citations):
            relevant.append((num, citations[num - 1]))
        if len(relevant) >= max_count:
            break

    # If we have fewer than min_count, pad with top-K fallback
    if len(relevant) < min_count:
        existing_nums = {num for num, _ in relevant}
        for i, c in enumerate(citations[:20]):  # Check first 20
            idx = i + 1
            if idx not in existing_nums:
                relevant.append((idx, c))
                if len(relevant) >= min_count:
                    break

    return relevant


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

    # Extract which citation numbers are actually referenced in the answer
    cited_numbers = _extract_cited_numbers(answer, len(citation_objs), limit=50)
    logger.debug(f"[verification] Found {len(cited_numbers)} unique citation references in answer: {sorted(cited_numbers)[:10]}...")

    # Build relevant citations with original indices preserved
    relevant_citations = _build_relevant_citations(citation_objs, cited_numbers, min_count=10, max_count=25)
    logger.debug(f"[verification] Using {len(relevant_citations)} relevant citations for verification")

    # Step 1: Extract factual claims using LLM
    claims = await _extract_claims(answer, llm_client)
    logger.info(f"[verification] Extracted {len(claims)} claims from synthesis")

    if not claims:
        return VerificationResult(
            overall_confidence=1.0,  # No claims = nothing to verify
            total_claims=0,
            supported_claims=0
        )

    # Step 2: Cross-reference each claim against citations (using relevant citations with original indices)
    claim_verifications = []
    for claim in claims:
        verification = await _verify_single_claim(claim, relevant_citations, llm_client)
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

    # P0 fix: Detect language and use appropriate prompt
    # This ensures claims are extracted correctly for CJK text
    lang = detect_language(answer[:500])

    if lang == "zh":
        prompt = f"""从以下文本中提取所有事实性陈述。
事实性陈述是指可以通过来源验证的声明。

文本:
{answer[:8000]}

指令:
1. 只提取事实性陈述（非观点或解释）
2. 每个陈述应是单一、可验证的声明
3. 以编号列表形式返回
4. 限制在最重要的 10 个陈述

输出格式:
1. [第一个陈述]
2. [第二个陈述]
...
"""
    else:
        prompt = f"""Extract all factual claims from the following text.
A factual claim is a statement that can be verified against sources.

Text:
{answer[:8000]}

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

        # max_tokens=8000: Claims extraction typically produces ~1500-2000 tokens
        # (10 claims × ~100-150 tokens each + JSON/list formatting overhead).
        # Previous value of 2000 caused truncation; 8000 provides 4x safety margin.
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
    indexed_citations: List[Tuple[int, Citation]],
    providers: Any
) -> ClaimVerification:
    """
    Verify a single claim against available citations.

    Args:
        claim: The factual claim to verify
        indexed_citations: List of (original_index, citation) tuples preserving original numbering
        providers: LLM provider for verification
    """

    if not indexed_citations:
        return ClaimVerification(claim=claim, confidence=0.0)

    # Build mapping from original index to citation for lookup
    idx_to_citation = {idx: c for idx, c in indexed_citations}

    # Build citation context using ORIGINAL indices (e.g., [1], [37], [42])
    citation_context = "\n\n".join([
        f"[{idx}] {(c.title or c.source or c.url)}\n{((c.content or c.snippet) or '')[:500]}"
        for idx, c in indexed_citations
    ])

    # List valid citation numbers for the prompt
    valid_nums = sorted(idx_to_citation.keys())

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
    "supporting": [{valid_nums[0]}],  // Citation numbers that support (use actual numbers shown above)
    "conflicting": [],    // Citation numbers that conflict
    "confidence": 0.85     // 0.0-1.0 confidence in claim
}}

IMPORTANT:
- Only use citation numbers from the sources above: {valid_nums}
- Only output the JSON, nothing else.
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

        # Filter to only valid citation numbers and get credibility weights
        valid_supporting = [n for n in supporting if n in idx_to_citation]
        valid_conflicting = [n for n in conflicting if n in idx_to_citation]

        # Weight confidence by citation credibility and count with diminishing returns (log2)
        if valid_supporting:
            credibility_weights = [idx_to_citation[n].credibility_score for n in valid_supporting]
            avg_cred = (sum(credibility_weights) / len(credibility_weights)) if credibility_weights else 0.5
            # Diminishing returns for multiple sources
            num_sources = len(valid_supporting)
            bonus = min(0.25, 0.2 * math.log2(max(1, num_sources)))  # cap 25%
            confidence = base_confidence * avg_cred * (1.0 + bonus if num_sources > 1 else 1.0)
        else:
            confidence = 0.0

        # Weighted conflict penalty proportional to conflict strength
        if valid_conflicting:
            conflict_weight = sum(idx_to_citation[n].credibility_score for n in valid_conflicting)
            support_weight = sum(idx_to_citation[n].credibility_score for n in valid_supporting) if valid_supporting else 0.0
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
            supporting_citations=valid_supporting,
            conflicting_citations=valid_conflicting,
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


# ============================================================================
# V2 Verification Functions (Three-category with BM25 retrieval)
# ============================================================================

async def _verify_single_claim_v2(
    claim: str,
    all_citations: List[Citation],
    providers: Any,
    corpus_stats: Optional[CorpusStats] = None
) -> ClaimVerificationV2:
    """
    Verify a single claim with evidence retrieval + three-category output.

    Uses BM25 to find relevant citations, then LLM judges:
    - supported: At least one source explicitly supports the claim
    - unsupported: A source explicitly contradicts the claim
    - insufficient_evidence: Sources don't directly address the claim

    P1-5: Accepts pre-computed corpus_stats for efficient IDF-weighted retrieval.
    """

    # Step 1: Retrieve top-5 relevant citations via BM25 (with IDF)
    relevant = retrieve_relevant_citations(claim, all_citations, top_k=5, corpus_stats=corpus_stats)

    if not relevant:
        return ClaimVerificationV2(
            claim=claim,
            verdict="insufficient_evidence",
            confidence=0.3,
            reasoning="No relevant citations found via retrieval"
        )

    # Step 2: Build context from relevant citations only
    citation_context = "\n\n".join([
        f"[{idx}] (relevance: {score:.2f}) {c.title or c.source or c.url}\n{(c.content or c.snippet or '')[:500]}"
        for idx, c, score in relevant
    ])

    valid_nums = [idx for idx, _, _ in relevant]
    retrieval_scores = {idx: score for idx, _, score in relevant}

    # Step 3: Language-aware prompt
    lang = detect_language(claim)

    if lang == "zh":
        prompt = f"""判断以下陈述是否被来源支持。

陈述: {claim}

相关来源 (按相关性排序):
{citation_context}

## 输出要求
输出 JSON:
{{
    "verdict": "supported" | "unsupported" | "insufficient_evidence",
    "supporting": [citation_ids...],
    "conflicting": [citation_ids...],
    "confidence": 0.0-1.0,
    "reasoning": "简短解释"
}}

## 判定标准
- **supported**: 至少一个来源明确支持该陈述（有直接证据）
- **unsupported**: 有来源明确反驳该陈述（有矛盾证据）
- **insufficient_evidence**: 来源不直接涉及该陈述，或证据不足以判断

只使用以下 citation ID: {valid_nums}
只输出 JSON，无其他内容。
"""
    else:
        prompt = f"""Judge whether the following claim is supported by sources.

Claim: {claim}

Relevant sources (ranked by relevance):
{citation_context}

## Output format
{{
    "verdict": "supported" | "unsupported" | "insufficient_evidence",
    "supporting": [citation_ids...],
    "conflicting": [citation_ids...],
    "confidence": 0.0-1.0,
    "reasoning": "brief explanation"
}}

## Judgment criteria
- **supported**: At least one source explicitly supports the claim (direct evidence)
- **unsupported**: A source explicitly contradicts the claim (conflicting evidence)
- **insufficient_evidence**: Sources don't directly address the claim, or evidence is inconclusive

Only use citation IDs: {valid_nums}
Output JSON only.
"""

    try:
        from llm_service.providers.base import ModelTier

        result = await providers.generate_completion(
            messages=[{"role": "user", "content": prompt}],
            tier=ModelTier.SMALL,
            max_tokens=500,
            temperature=0.0
        )

        response = result.get("output_text", "").strip()

        # Parse JSON
        json_start = response.find('{')
        json_end = response.rfind('}') + 1
        if json_start != -1 and json_end > json_start:
            parsed = json.loads(response[json_start:json_end])
        else:
            parsed = json.loads(response)

        verdict = parsed.get("verdict", "insufficient_evidence")
        # Normalize verdict
        if verdict not in ("supported", "unsupported", "insufficient_evidence"):
            verdict = "insufficient_evidence"

        supporting = [n for n in parsed.get("supporting", []) if n in valid_nums]
        conflicting = [n for n in parsed.get("conflicting", []) if n in valid_nums]
        base_confidence = float(parsed.get("confidence", 0.5))
        reasoning = parsed.get("reasoning", "")

        # Adjust confidence based on verdict and retrieval quality
        if verdict == "supported" and supporting:
            # Boost by retrieval score of best supporting citation
            max_retrieval = max(retrieval_scores.get(n, 0) for n in supporting)
            # Weighted: 60% LLM confidence + 40% retrieval quality (normalized)
            confidence = 0.6 * base_confidence + 0.4 * min(1.0, max_retrieval / 5.0)
            # Ensure minimum confidence for supported claims
            confidence = max(confidence, 0.5)
        elif verdict == "unsupported" and conflicting:
            # High confidence in contradiction
            confidence = base_confidence * 0.9
            confidence = max(confidence, 0.6)
        else:
            # insufficient_evidence - moderate confidence
            confidence = 0.3 + 0.2 * base_confidence

        # Clamp to [0, 1]
        confidence = max(0.0, min(1.0, confidence))

        return ClaimVerificationV2(
            claim=claim,
            verdict=verdict,
            supporting_citations=supporting,
            conflicting_citations=conflicting,
            confidence=confidence,
            retrieval_scores=retrieval_scores,
            reasoning=reasoning
        )

    except (json.JSONDecodeError, KeyError, ValueError) as e:
        logger.warning(f"[verification_v2] Failed to parse LLM response: {e}")
        return ClaimVerificationV2(
            claim=claim,
            verdict="insufficient_evidence",
            confidence=0.3,
            reasoning=f"Parse error: {str(e)}"
        )
    except Exception as e:
        logger.error(f"[verification_v2] Unexpected error: {e}")
        return ClaimVerificationV2(
            claim=claim,
            verdict="insufficient_evidence",
            confidence=0.3,
            reasoning=f"Error: {str(e)}"
        )


def _detect_conflicts_v2(
    verifications: List[ClaimVerificationV2],
    citations: List[Citation]
) -> List[ConflictReport]:
    """Detect conflicting information across sources for V2 verifications."""
    conflicts = []
    for v in verifications:
        if v.supporting_citations and v.conflicting_citations:
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


async def verify_claims_v2(
    answer: str,
    citations: List[Dict[str, Any]],
    llm_client: Any
) -> VerificationResultV2:
    """
    V2: Verify claims with BM25 evidence retrieval and three-category output.

    Improvements over V1:
    - BM25 retrieval finds relevant citations before LLM judgment
    - Three-category output: supported / unsupported / insufficient_evidence
    - Language-aware prompts for CJK and Latin text
    - Better confidence calibration

    Args:
        answer: Synthesis result containing claims
        citations: List of citation dicts from orchestrator
        llm_client: LLM client for claim extraction and verification

    Returns:
        VerificationResultV2 with three-category breakdown and quality metrics
    """

    # Parse citations
    citation_objs: List[Citation] = []
    for idx, raw in enumerate(citations or []):
        try:
            citation_objs.append(Citation(**(raw or {})))
        except Exception as e:
            logger.warning(f"[verification_v2] Failed to parse citation[{idx}]: {e}")

    # Extract claims (reuse existing function)
    claims = await _extract_claims(answer, llm_client)
    logger.info(f"[verification_v2] Extracted {len(claims)} claims from synthesis")

    if not claims:
        return VerificationResultV2(
            overall_confidence=1.0,
            total_claims=0,
            supported_claims=0,
            unsupported_claims=0,
            insufficient_evidence_claims=0,
            evidence_coverage=1.0,
            avg_retrieval_score=0.0
        )

    # P1-5: Pre-compute corpus stats once for all claims (IDF efficiency)
    corpus_stats = CorpusStats.from_citations(citation_objs)
    logger.debug(f"[verification_v2] Corpus stats: {corpus_stats.total_docs} docs, "
                 f"{len(corpus_stats.doc_freq)} unique terms, avg_len={corpus_stats.avg_doc_len:.1f}")

    # Verify each claim with V2 logic
    verifications: List[ClaimVerificationV2] = []
    for claim in claims:
        v = await _verify_single_claim_v2(claim, citation_objs, llm_client, corpus_stats=corpus_stats)
        verifications.append(v)

    # Aggregate by verdict
    supported = sum(1 for v in verifications if v.verdict == "supported")
    unsupported = sum(1 for v in verifications if v.verdict == "unsupported")
    insufficient = sum(1 for v in verifications if v.verdict == "insufficient_evidence")

    # Collect claim texts by category
    supported_texts = [v.claim for v in verifications if v.verdict == "supported"]
    unsupported_texts = [v.claim for v in verifications if v.verdict == "unsupported"]
    insufficient_texts = [v.claim for v in verifications if v.verdict == "insufficient_evidence"]

    # Calculate quality metrics
    # Evidence coverage: % of claims that got a definitive verdict (not insufficient)
    evidence_coverage = (supported + unsupported) / len(verifications) if verifications else 0.0

    # Average top-1 retrieval score
    retrieval_scores = []
    for v in verifications:
        if v.retrieval_scores:
            retrieval_scores.append(max(v.retrieval_scores.values()))
    avg_retrieval = sum(retrieval_scores) / len(retrieval_scores) if retrieval_scores else 0.0

    # Overall confidence (weighted by verdict type)
    if verifications:
        conf_scores = []
        for v in verifications:
            if v.verdict == "supported":
                conf_scores.append(v.confidence)
            elif v.verdict == "unsupported":
                # Unsupported claims reduce overall confidence
                conf_scores.append(1.0 - v.confidence)
            else:
                # Insufficient evidence is neutral
                conf_scores.append(0.5)
        overall_conf = sum(conf_scores) / len(conf_scores)
    else:
        overall_conf = 1.0

    # Detect conflicts
    conflicts = _detect_conflicts_v2(verifications, citation_objs)

    logger.info(f"[verification_v2] Results: supported={supported}, unsupported={unsupported}, "
                f"insufficient={insufficient}, overall_conf={overall_conf:.2f}, "
                f"evidence_coverage={evidence_coverage:.2f}")

    return VerificationResultV2(
        overall_confidence=overall_conf,
        total_claims=len(claims),
        supported_claims=supported,
        unsupported_claims=unsupported,
        insufficient_evidence_claims=insufficient,
        supported_claim_texts=supported_texts,
        unsupported_claim_texts=unsupported_texts,
        insufficient_claim_texts=insufficient_texts,
        claim_details=verifications,
        conflicts=conflicts,
        evidence_coverage=evidence_coverage,
        avg_retrieval_score=avg_retrieval
    )


# ======================================================================
# FastAPI Endpoint
# ======================================================================

class VerifyClaimsRequest(BaseModel):
    """Request body for claim verification endpoint."""
    answer: str
    citations: List[Dict[str, Any]]
    use_v2: bool = True  # Default to V2


@router.post("/api/verify_claims")
async def verify_claims_endpoint(request: Request, body: VerifyClaimsRequest):
    """
    Verify factual claims in synthesis against collected citations.

    POST /api/verify_claims
    {
        "answer": "synthesis text with claims",
        "citations": [{"url": "...", "title": "...", "content": "..."}],
        "use_v2": true  // optional, defaults to true
    }

    V1 Returns (use_v2=false):
    {
        "overall_confidence": 0.82,
        "total_claims": 10,
        "supported_claims": 8,
        "unsupported_claims": ["unsupported claim text"],
        "conflicts": [],
        "claim_details": [...]
    }

    V2 Returns (use_v2=true, default):
    {
        "overall_confidence": 0.72,
        "total_claims": 10,
        "supported_claims": 5,
        "unsupported_claims": 1,
        "insufficient_evidence_claims": 4,
        "supported_claim_texts": [...],
        "unsupported_claim_texts": [...],
        "insufficient_claim_texts": [...],
        "claim_details": [...],  // with verdict, retrieval_scores, reasoning
        "conflicts": [],
        "evidence_coverage": 0.6,
        "avg_retrieval_score": 3.2
    }
    """
    try:
        # Get LLM providers from app state
        providers = request.app.state.providers

        if body.use_v2:
            # V2: BM25 retrieval + three-category output
            result = await verify_claims_v2(
                answer=body.answer,
                citations=body.citations,
                llm_client=providers
            )
            return result.model_dump()
        else:
            # V1: Legacy verification
            result = await verify_claims(
                answer=body.answer,
                citations=body.citations,
                llm_client=providers
            )
            return result.model_dump()

    except Exception as e:
        logger.error(f"[verify_claims_endpoint] Error: {e}", exc_info=True)
        # Return a safe default response on error
        if body.use_v2:
            return VerificationResultV2(
                overall_confidence=0.5,
                total_claims=0,
                supported_claims=0,
                unsupported_claims=0,
                insufficient_evidence_claims=0,
                evidence_coverage=0.0,
                avg_retrieval_score=0.0
            ).model_dump()
        else:
            return VerificationResult(
                overall_confidence=0.5,
                total_claims=0,
                supported_claims=0,
                unsupported_claims=[],
                conflicts=[]
            ).model_dump()
