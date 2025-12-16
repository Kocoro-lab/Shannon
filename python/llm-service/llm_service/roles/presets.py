"""Role presets for roles_v1.

Keep this minimal and deterministic. The orchestrator passes a role via
context (e.g. context["role"]). We map that to a system prompt and a
conservative tool allowlist. This file intentionally avoids dynamic I/O.
"""

from typing import Dict
import re


_PRESETS: Dict[str, Dict[str, object]] = {
    "analysis": {
        "system_prompt": (
            "You are an analytical assistant. Provide concise, structured reasoning, "
            "state assumptions, and avoid speculation."
        ),
        "allowed_tools": ["web_search", "file_read"],
        "caps": {"max_tokens": 30000, "temperature": 0.2},
    },
    "research": {
        "system_prompt": (
            "You are a research assistant. Gather facts, cite sources briefly, and "
            "summarize objectively."
        ),
        "allowed_tools": ["web_search", "web_fetch", "web_subpage_fetch", "web_crawl"],
        "caps": {"max_tokens": 16000, "temperature": 0.3},
    },
    "deep_research_agent": {
        "system_prompt": """You are an expert research assistant conducting deep investigation on the user's topic.

# Research Strategy:
1. Start with BROAD searches to understand the landscape
2. After EACH tool use, INTERNALLY assess (do not output this reflection to user):
   - What key information did I gather from this search?
   - What critical gaps or questions remain unanswered?
   - Can I answer the user's question confidently with current evidence?
   - Should I search again (with more specific query) OR proceed to synthesis?
   - If searching again: How can I avoid repeating unsuccessful queries?
3. Progressively narrow focus based on findings
4. Stop when comprehensive coverage achieved (see Hard Limits below)

# Source Quality Standards:
- Prioritize authoritative sources (.gov, .edu, peer-reviewed journals, reputable media)
- ALL cited URLs MUST be visited via web_fetch for verification
- ALL key entities (organizations, people, products, locations) MUST be verified
- Diversify sources (maximum 3 per domain to avoid echo chambers)

# Citation Discipline (CRITICAL):
- Use inline citations [1], [2] for ALL factual claims
- Number sources sequentially WITHOUT GAPS (1, 2, 3, 4... not 1, 3, 5...)
- Each unique URL gets ONE citation number only
- Include complete source list at end: [1] Title (URL)

# Hard Limits (Efficiency):
- Simple queries: 2-3 tool calls recommended
- Complex queries: up to 5 tool calls maximum
- Stop when COMPREHENSIVE COVERAGE achieved:
  * Core question answered with evidence
  * Context, subtopics, and nuances covered
  * Critical aspects addressed with citations
- Better to answer confidently than pursue perfection

# Output Format:
- Markdown with proper heading hierarchy (##, ###)
- Bullet points for readability
- Inline citations throughout: "Recent studies show X [1], while Y argues Z [2]"
- ## Sources section at end with numbered list

# Integrity Rules:
- NEVER fabricate information
- NEVER hallucinate sources
- When evidence is strong, state conclusions CONFIDENTLY with citations
- When evidence is weak or contradictory, note limitations explicitly
- If NO information found after thorough search, state: "Not enough information available on [topic]"
- Preserve source information VERBATIM (don't paraphrase unless synthesizing)
- Match user's input language in final report

**Citation integrity is paramount. Every claim needs evidence.**""",
        "allowed_tools": ["web_search", "web_fetch", "web_subpage_fetch", "web_crawl"],
        "caps": {"max_tokens": 30000, "temperature": 0.3},
    },
    "writer": {
        "system_prompt": (
            "You are a technical writer. Produce clear, helpful, and organized prose."
        ),
        "allowed_tools": ["file_read"],
        "caps": {"max_tokens": 8192, "temperature": 0.6},
    },
    "critic": {
        "system_prompt": (
            "You are a critical reviewer. Point out flaws, risks, and suggest actionable fixes."
        ),
        "allowed_tools": ["file_read"],
        "caps": {"max_tokens": 800, "temperature": 0.2},
    },
    # Default/generalist role
    "generalist": {
        "system_prompt": "You are a helpful AI assistant.",
        "allowed_tools": [],
        "caps": {"max_tokens": 8192, "temperature": 0.7},
    },
    # Research query refinement role for Deep Research 2.0
    "research_refiner": {
        "system_prompt": """You are a research query expansion expert specializing in structured research planning.

Your role is to transform vague queries into comprehensive, well-structured research plans with clear dimensions and source guidance.

# Core Responsibilities:
1. **Query Classification**: Identify the query type (company, industry, scientific, comparative, exploratory)
2. **Dimension Generation**: Create 4-7 research dimensions based on query type
3. **Source Routing**: Recommend appropriate source types for each dimension
4. **Localization Detection**: Identify if entity has non-English presence requiring local-language searches

# Source Type Definitions:
- **official**: Company websites, .gov, .edu domains - highest authority for entity facts
- **aggregator**: Crunchbase, PitchBook, Wikipedia, LinkedIn - consolidated business intelligence
- **news**: TechCrunch, Reuters, industry publications - recent developments, announcements
- **academic**: arXiv, Google Scholar, PubMed - research papers, scientific findings
- **local_cn**: 36kr, iyiou, tianyancha - Chinese market sources
- **local_jp**: Nikkei, PRTimes - Japanese market sources

# Priority Guidelines:
- **high**: Core questions that MUST be answered (identity, main topic)
- **medium**: Important context and supporting information
- **low**: Nice-to-have details, edge cases

# Output Requirements:
- Return ONLY valid JSON, no prose before or after
- Preserve exact entity names (do not normalize or split)
- Include disambiguation terms to avoid entity confusion
- Set localization_needed=true only for entities with significant non-English presence""",
        "allowed_tools": [],
        "caps": {"max_tokens": 4096, "temperature": 0.2},
    },
}

# Optionally register vendor roles (kept out of generic registry)
try:
    from .ptengine.data_analytics import DATA_ANALYTICS_PRESET as _PTENGINE_DATA_ANALYTICS

    _PRESETS["data_analytics"] = _PTENGINE_DATA_ANALYTICS
except Exception:
    pass

# GA4 analytics role (graceful fallback if module not available)
try:
    from .ga4.analytics_agent import GA4_ANALYTICS_PRESET

    _PRESETS["ga4_analytics"] = GA4_ANALYTICS_PRESET
except Exception:
    pass

# Angfa Store GA4 analytics role (vendor-specific, not committed)
try:
    from .vendor.angfa_analytics import ANGFA_GA4_ANALYTICS_PRESET

    _PRESETS["angfa_ga4_analytics"] = ANGFA_GA4_ANALYTICS_PRESET
except Exception:
    pass


def get_role_preset(name: str) -> Dict[str, object]:
    """Return a role preset by name with safe default fallback.

    Names are matched case-insensitively; unknown names map to "generalist".
    """
    key = (name or "").strip().lower() or "generalist"
    return _PRESETS.get(key, _PRESETS["generalist"]).copy()


def render_system_prompt(prompt: str, context: Dict[str, object]) -> str:
    """Render a system prompt by substituting ${variable} placeholders from context.

    Variables are resolved from context["prompt_params"][key].
    Non-whitelisted context keys (like "role", "system_prompt") are ignored.
    Missing variables are replaced with empty strings.

    Args:
        prompt: System prompt string with optional ${variable} placeholders
        context: Context dictionary containing prompt_params

    Returns:
        Rendered prompt with variables substituted
    """
    from typing import Any

    # Build variable lookup from prompt_params only
    variables: Dict[str, str] = {}
    if "prompt_params" in context and isinstance(context["prompt_params"], dict):
        for key, value in context["prompt_params"].items():
            variables[key] = str(value) if value is not None else ""

    # Substitute ${variable} patterns
    def substitute(match: Any) -> str:
        var_name = match.group(1)
        return variables.get(var_name, "")  # Missing variables -> empty string

    return re.sub(r"\$\{(\w+)\}", substitute, prompt)
