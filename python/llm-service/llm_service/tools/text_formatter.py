"""
Tool text formatters for the /tools/execute API.

Generates LLM-friendly text representations of tool output.
Used by the direct tool execution endpoint (CLI, external clients).
Does NOT affect orchestrated workflows (agent loop has its own formatting).

Design: A generic fallback handles most tools automatically by stripping
metadata and formatting common output shapes (dicts, lists, scalars).
Per-tool overrides exist only for tools with unusual structures (e.g.
web_search with nested results + relevance filtering).
"""

import html
import json
import logging
from typing import Any, Dict, List, Optional

logger = logging.getLogger(__name__)

# Blocklist: internal metadata keys stripped from output before presenting to LLMs.
# Evolve this set as new tools are added — err on the side of stripping.
# If a key should never reach an LLM (internal routing, debug info, cost fields),
# add it here rather than per-tool.
_STRIP_KEYS = frozenset({
    "tool_source", "method", "fetch_method", "status_code", "blocked_reason",
    "provider", "provider_used", "strategy", "attempts", "partial_success",
    "failure_summary", "urls_attempted", "urls_succeeded", "urls_failed",
    "pages_fetched", "truncated", "char_count", "word_count",
    "trade_count", "vwap", "adjusted_close",
    "extracted", "extraction_model", "extraction_tokens", "extraction_cost_usd",
})

# Max total text length
_MAX_TEXT_LEN = 6000


def format_tool_text(
    tool_name: str, output: Any, metadata: Optional[Dict[str, Any]] = None
) -> Optional[str]:
    """
    Generate an LLM-friendly text representation of tool output.

    Uses per-tool formatter if registered, otherwise falls back to
    generic formatting. Never raises — returns None on failure.
    """
    formatter = _FORMATTERS.get(tool_name, _format_generic)
    try:
        result = formatter(output, metadata)
        if result and len(result) > _MAX_TEXT_LEN:
            result = result[:_MAX_TEXT_LEN] + "..."
        return result
    except Exception:
        logger.debug("text_formatter failed for %s, falling back", tool_name, exc_info=True)
        return None


# ---------------------------------------------------------------------------
# Generic fallback — handles any tool without specific registration
# ---------------------------------------------------------------------------


def _format_generic(output: Any, metadata: Optional[Dict[str, Any]]) -> Optional[str]:
    """Format any tool output by stripping metadata keys and presenting content."""
    if output is None:
        return None

    # Scalar — just stringify
    if isinstance(output, (str, int, float, bool)):
        return str(output)

    # List of items — format each
    if isinstance(output, list):
        if not output:
            return "No results."
        parts: List[str] = []
        for i, item in enumerate(output[:20], 1):
            if isinstance(item, dict):
                parts.append(f"{i}. {_format_dict_compact(item)}")
            else:
                parts.append(f"{i}. {str(item)}")
        return "\n".join(parts)

    # Dict — the most common case
    if isinstance(output, dict):
        return _format_dict_smart(output)

    return str(output)


def _format_dict_smart(d: Dict[str, Any]) -> str:
    """Intelligently format a dict based on its shape."""
    # If it has a 'content' key, it's likely a fetched page / document
    if "content" in d and isinstance(d["content"], str):
        parts: List[str] = []
        if d.get("title"):
            parts.append(f"Title: {d['title']}")
        if d.get("url"):
            parts.append(f"URL: {d['url']}")
        content = d["content"]
        if len(content) > 3000:
            content = content[:3000] + "..."
        if parts:
            parts.append("")
        parts.append(content)
        return "\n".join(parts)

    # If it has a 'data' key with a list, format as entries
    if "data" in d and isinstance(d["data"], list):
        items = d["data"]
        if not items:
            return "No data."
        parts = []
        for i, item in enumerate(items[:20], 1):
            if isinstance(item, dict):
                parts.append(f"{i}. {_format_dict_compact(item)}")
            else:
                parts.append(f"{i}. {item}")
        return "\n".join(parts)

    # If it has a 'results' key with a list, format as entries
    if "results" in d and isinstance(d["results"], list):
        items = d["results"]
        if not items:
            return "No results."
        parts = []
        for i, item in enumerate(items[:20], 1):
            if isinstance(item, dict):
                parts.append(f"{i}. {_format_dict_compact(item)}")
            else:
                parts.append(f"{i}. {item}")
        return "\n".join(parts)

    # Default: strip metadata keys and compact-print remaining
    return _format_dict_compact(d)


def _format_dict_compact(d: Dict[str, Any]) -> str:
    """Format a dict by stripping metadata keys and presenting key: value pairs."""
    clean = {k: v for k, v in d.items() if k not in _STRIP_KEYS and v is not None}
    if not clean:
        return "{}"
    # If only one key, just show the value
    if len(clean) == 1:
        val = next(iter(clean.values()))
        return str(val) if not isinstance(val, dict) else json.dumps(val, ensure_ascii=False)
    # Format as key: value lines, truncating long values
    lines: List[str] = []
    for k, v in clean.items():
        if isinstance(v, str) and len(v) > 500:
            v = v[:500] + "..."
        elif isinstance(v, (dict, list)):
            s = json.dumps(v, ensure_ascii=False)
            if len(s) > 500:
                s = s[:500] + "..."
            v = s
        lines.append(f"{k}: {v}")
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# Per-tool overrides — only for tools with non-obvious output structures
# ---------------------------------------------------------------------------


def _format_web_search(output: Any, metadata: Optional[Dict[str, Any]]) -> Optional[str]:
    """Format web_search output — needs custom handling for nested results + html unescape."""
    if not isinstance(output, dict):
        if isinstance(output, list):
            results = output  # fallback scrape path returns bare list
        else:
            return None
    else:
        results = output.get("results")
        if not isinstance(results, list):
            return None

    if not results:
        return "No search results found."

    query = output.get("query", "") if isinstance(output, dict) else ""
    lines: List[str] = []
    if query:
        lines.append(f"Search results for: {query}\n")

    for i, item in enumerate(results[:7], 1):
        if not isinstance(item, dict):
            continue
        title = html.unescape(item.get("title", ""))
        url = item.get("url", "")
        snippet = item.get("snippet", "")
        date = item.get("published_date", "")

        raw = item.get("markdown") if isinstance(item.get("markdown"), str) else None
        if not raw:
            raw = item.get("content", "")
        content = html.unescape(raw) if raw else html.unescape(snippet)
        if len(content) > 1500:
            content = content[:1500] + "..."

        entry = f"{i}. {title}"
        if date:
            entry += f" ({date[:10]})"
        entry += f"\n   {url}"
        if content:
            entry += f"\n   {content}"
        lines.append(entry)

    return "\n\n".join(lines)


def _format_web_fetch(output: Any, metadata: Optional[Dict[str, Any]]) -> Optional[str]:
    """Format web_fetch — batch scaling + explicit single-URL handling.

    Content cleaning is firecrawl's job (onlyMainContent + excludeTags).
    This formatter focuses on truncation budget and batch scaling only.
    """
    if not isinstance(output, dict):
        return None

    # Multi-URL response has 'pages' key (batch fetch)
    if "pages" in output and isinstance(output["pages"], list):
        pages = output["pages"]
        num_pages = sum(1 for p in pages if isinstance(p, dict) and p.get("success", False))
        # Scale per-page cap: more pages = less per page to stay within total budget
        per_page_cap = min(3000, _MAX_TEXT_LEN // max(num_pages, 1) - 200)

        parts: List[str] = []
        for item in pages:
            if not isinstance(item, dict):
                continue
            if not item.get("success", False):
                parts.append(f"[Failed] {item.get('url', '?')}: {item.get('error', 'unknown')}")
                continue
            title = item.get("title", "")
            url = item.get("url", "")
            # Prefer snippet for batch (already clean, 500 chars) when cap is tight
            if per_page_cap <= 1500 and item.get("snippet"):
                content = item["snippet"]
            else:
                content = item.get("content", "")
                if len(content) > per_page_cap:
                    content = content[:per_page_cap] + "..."
            entry = f"Title: {title}\nURL: {url}\n\n{content}" if title else f"URL: {url}\n\n{content}"
            parts.append(entry)
        return "\n\n---\n\n".join(parts) if parts else None

    # Single-URL: delegate to generic dict handler (has content/title/url)
    return _format_generic(output, metadata)


# ---------------------------------------------------------------------------
# Registry — per-tool overrides only; all other tools use _format_generic
# ---------------------------------------------------------------------------

_FORMATTERS: Dict[str, Any] = {
    "web_search": _format_web_search,
    "web_fetch": _format_web_fetch,
    "web_crawl": _format_web_fetch,
    "web_subpage_fetch": _format_web_fetch,
}
