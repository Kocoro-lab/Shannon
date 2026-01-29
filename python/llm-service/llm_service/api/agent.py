"""Agent API endpoints for HTTP communication with Agent-Core."""

import logging
import os
from typing import Dict, Any, Optional, List, Tuple
from fastapi import APIRouter, HTTPException, Request
from pydantic import BaseModel, Field
from fastapi.responses import JSONResponse, StreamingResponse
import html
from difflib import SequenceMatcher

logger = logging.getLogger(__name__)

router = APIRouter()


def strip_markdown_json_wrapper(text: str, *, expect_json: bool) -> str:
    """Strip markdown code fences when they wrap JSON.

    Only strips wrappers for JSON-looking content (```json ... ``` or ``` ... ``` with
    a JSON object/array body). This avoids breaking normal markdown/code outputs.
    """
    if not expect_json or not text or not isinstance(text, str):
        return text

    trimmed = text.strip()
    if not trimmed.startswith("```"):
        return text

    first_newline = trimmed.find("\n")
    if first_newline == -1:
        return text

    fence = trimmed[:first_newline].strip()
    lang = fence[3:].strip().lower()
    if lang not in ("", "json"):
        return text

    body = trimmed[first_newline + 1 :].strip()
    if not body.endswith("```"):
        return text

    body = body[:-3].strip()
    if not body or body[0] not in "{[":
        return text

    return body


def _response_format_expects_json(response_format: Any) -> bool:
    if not isinstance(response_format, dict):
        return False
    rf_type = response_format.get("type")
    return isinstance(rf_type, str) and rf_type.startswith("json")


def calculate_relevance_score(query: str, result: Dict[str, Any]) -> float:
    """Calculate relevance score for a search result based on query match."""
    query_lower = query.lower()

    # Extract text fields
    title = result.get("title", "").lower()
    content = result.get("content", "").lower()
    snippet = result.get("snippet", "").lower()

    # Calculate similarity scores
    title_score = SequenceMatcher(None, query_lower, title).ratio()

    # Check for exact query terms in content
    query_terms = query_lower.split()
    content_text = content or snippet
    term_matches = (
        sum(1 for term in query_terms if term in content_text) / len(query_terms)
        if query_terms
        else 0
    )

    # Weight the scores
    relevance = (title_score * 0.4) + (term_matches * 0.6)

    # Boost if source is official or highly relevant
    url = result.get("url", "").lower()
    if any(term in url for term in query_terms):
        relevance += 0.2

    return min(relevance, 1.0)


def filter_relevant_results(
    query: str, results: List[Dict[str, Any]], threshold: float = 0.3
) -> List[Dict[str, Any]]:
    """Filter and rank search results by relevance to the query."""
    if not results:
        return results

    # Calculate relevance scores
    scored_results = []
    for result in results:
        score = calculate_relevance_score(query, result)
        if score >= threshold:
            result_copy = result.copy()
            result_copy["relevance_score"] = score
            scored_results.append(result_copy)

    # Sort by relevance score
    scored_results.sort(key=lambda x: x.get("relevance_score", 0), reverse=True)

    return scored_results[:5]  # Return top 5 most relevant


# ============================================================================
# Interpretation Pass Helper Functions (P0 Fix for content loss)
# ============================================================================

# Interpretation prompts for summarizing tool results
INTERPRETATION_PROMPT_SOURCES = """=== CRITICAL INSTRUCTION ===

You MUST summarize the ACTUAL CONTENT from the tool results above.
You MUST assess each source's RELEVANCE to the original query.

=== RELEVANCE-AWARE OUTPUT ===

For EACH source, first determine its relevance to the query:

**HIGH RELEVANCE** (source directly addresses the query topic):
- Provide detailed summary with preserved data points
- Use tables for comparisons/metrics (saves space, improves clarity)
- Use bullet lists for key facts
- Include all specific numbers, dates, names, conclusions

**LOW RELEVANCE** (source is off-topic, tangential, or operational):
- Write ONE concise line explaining why it's not relevant
- Format: "## Source N: [URL] - [TYPE] page, [brief reason why not relevant to query]"
- Examples of LOW relevance: support FAQs, API docs, login pages, navigation-only pages, error pages
- Do NOT expand further on LOW relevance sources

=== EVIDENCE-ONLY CONSTRAINT (CRITICAL) ===

STRICT RULES - violation causes output rejection:
1. Every URL you mention MUST appear in the tool results above
2. If a tool returned an error or empty content, report it as-is: "## Source N: [URL] - FAILED: [error message]"
3. DO NOT infer, guess, or fabricate any data not present in tool results
4. If tool says "Site Error", "Access Denied", "404", "no content" → report the failure, nothing more

CORRECT example:
- Tool result: "web_subpage_fetch failed: Site Error Detected"
- Your output: "## Source 2: example.com - FAILED: Site error, no content retrieved"

WRONG example (causes rejection):
- Tool result: "web_subpage_fetch failed: Site Error"
- Your output: "## Source 2: example.com - Company founded in 2015..." ← FABRICATION, FORBIDDEN

=== CONCISENESS TECHNIQUES ===

For HIGH relevance sources, prefer compact formats:

Table format (for metrics/comparisons):
| Attribute | Value |
|-----------|-------|
| Founded | 2010 |
| Employees | 5000+ |

Bullet format (for facts):
- Key product: Payments platform
- Headquarters: San Francisco

=== OUTPUT FORMAT ===

# PART 1 - RETRIEVED INFORMATION

## Source 1: [URL]
[If HIGH relevance: detailed summary with tables/bullets]
[If LOW relevance: one-line explanation]

## Source 2: [URL]
...

# PART 2 - NOTES (optional)
[Conflicts between sources, data gaps, failed fetches summary]

=== HANDLING INFORMATION SCARCITY ===

When search results lack specific information about the query topic:
1. Explicitly state: "关于[主题]的公开信息有限" or "Limited public information available about [topic]"
2. Still provide comprehensive analysis of what WAS found, even if tangentially related
3. Explain what types of information were NOT found (e.g., "No funding rounds disclosed", "Leadership details not publicly available")
4. Suggest what additional sources might help (e.g., "Company registry filings may contain more details")

This ensures the output remains informative even when target information is scarce.

=== FORBIDDEN ===
- Future action verbs ('I will fetch...', 'I need to search...')
- URLs not present in tool results
- Inferred/fabricated data when tool returned errors or empty content
- Detailed summaries of LOW relevance sources

ONLY summarize what was ALREADY retrieved."""

INTERPRETATION_PROMPT_GENERAL = """=== CRITICAL INSTRUCTION ===

You MUST answer the original query using ONLY the tool results above.

RULES:
- Provide a clear, complete answer (not raw tool logs).
- Do NOT use "Source 1/Source 2" or "PART 1 - RETRIEVED INFORMATION" format.
- Do NOT mention tool names or the tool-calling process.
- If the tool results are insufficient, say so explicitly.
- Do NOT invent facts, sources, or URLs.

ONLY use information that was ALREADY retrieved."""

SOURCE_FORMAT_ROLES = {"deep_research_agent", "research", "domain_prefetch"}


def should_use_source_format(role: Optional[str]) -> bool:
    """Only research roles use Source format output."""
    if not role:
        return False
    return role.strip().lower() in SOURCE_FORMAT_ROLES


def generate_tool_digest(tool_results: str, tool_records: List[Dict[str, Any]], max_chars: int = 3000) -> str:
    """
    Generate a human-readable digest from tool results.
    Used as fallback when interpretation pass fails.

    This is NOT raw JSON - it extracts key information in readable format.
    """
    lines: List[str] = []
    failures: List[str] = []

    def _summarize_failure(record: Dict[str, Any]) -> str:
        tool_name = record.get("tool", "unknown")
        err = record.get("error") or ""
        tool_input = record.get("tool_input") or {}
        target = ""
        if isinstance(tool_input, dict):
            if tool_input.get("url"):
                target = f"url={tool_input.get('url')}"
            elif tool_input.get("urls"):
                target = f"urls={tool_input.get('urls')}"
            elif tool_input.get("query"):
                target = f"query={tool_input.get('query')}"
        if target and err:
            return f"- {tool_name} FAILED ({target}): {err}"
        if err:
            return f"- {tool_name} FAILED: {err}"
        if target:
            return f"- {tool_name} FAILED ({target})"
        return f"- {tool_name} FAILED"

    # Process tool execution records for structured extraction
    for record in tool_records:
        tool_name = record.get("tool", "unknown")
        success = record.get("success", False)
        output = record.get("output", {})

        if not success:
            failures.append(_summarize_failure(record))
            continue

        if tool_name == "web_search":
            lines.append("## Search Results")
            results = output.get("results", []) if isinstance(output, dict) else []
            for i, r in enumerate(results[:5]):  # Top 5 results
                title = r.get("title", "")
                snippet = r.get("snippet", "")
                url = r.get("url", "")
                if title or snippet:
                    lines.append(f"- **{title}**: {snippet[:200]}...")
                    if url:
                        lines.append(f"  Source: {url}")
            lines.append("")

        elif tool_name == "web_fetch":
            lines.append("## Fetched Content")
            pages = []
            if isinstance(output, dict):
                pages = output.get("pages", []) if isinstance(output.get("pages"), list) else []
                # Single-URL mode: {url, title, content, ...}
                if not pages and any(k in output for k in ("url", "title", "content")):
                    pages = [output]

            for page in (pages or [])[:3]:  # Top 3 pages
                if isinstance(page, dict) and page.get("success") is False:
                    continue
                title = page.get("title", "Untitled") if isinstance(page, dict) else "Untitled"
                content = page.get("content", "") if isinstance(page, dict) else ""
                url = page.get("url", "") if isinstance(page, dict) else ""
                if content and len(content) > 50:
                    if content.startswith("%PDF"):
                        lines.append(f"- **{title}** (PDF document)")
                    else:
                        preview = content[:500].replace("\n", " ").strip()
                        lines.append(f"- **{title}**: {preview}...")
                    if url:
                        lines.append(f"  Source: {url}")
            lines.append("")

        elif tool_name == "web_subpage_fetch":
            lines.append("## Subpage Fetch")
            if isinstance(output, dict):
                url = output.get("url", "")
                title = output.get("title", "") or "Untitled"
                pages_fetched = output.get("pages_fetched", None)
                content = output.get("content", "") or ""
                if content.startswith("%PDF"):
                    lines.append(f"- **{title}** (PDF document)")
                else:
                    preview = content[:800].replace("\n", " ").strip()
                    suffix = f" ({pages_fetched} pages)" if isinstance(pages_fetched, int) else ""
                    lines.append(f"- **{title}**{suffix}: {preview}...")
                if url:
                    lines.append(f"  Source: {url}")
            lines.append("")

        elif tool_name == "web_crawl":
            lines.append("## Crawl")
            if isinstance(output, dict):
                url = output.get("url") or ""
                content = output.get("content") or ""
                preview = str(content)[:800].replace("\n", " ").strip()
                lines.append(f"- {preview}...")
                if url:
                    lines.append(f"  Source: {url}")
            lines.append("")

    if failures:
        lines.append("## Tool Failures")
        lines.extend(failures[:20])
        lines.append("")

    # Fallback: parse raw tool_results if no records
    if not lines and tool_results:
        # Prefer preserving escaped newlines (\\n) rather than stripping backslashes (which turns \\n into 'n')
        text = str(tool_results)
        text = text.replace("\\n", "\n").replace("\\t", "\t").replace("\\r", "\r")
        lines.append("## Tool Output Summary")
        lines.append(text[:max_chars])

    digest = "\n".join(lines)
    return digest[:max_chars] if len(digest) > max_chars else digest


def build_interpretation_messages(
    system_prompt: str,
    original_query: str,
    tool_results_summary: str,
    interpretation_prompt: str = INTERPRETATION_PROMPT_GENERAL,
    interpretation_system_prompt: Optional[str] = None,
) -> List[Dict[str, str]]:
    """
    Build clean messages for interpretation pass.

    P0 Fix: Instead of reusing entire messages history (which contains
    "I'll execute..." patterns), build fresh messages with only:
    - System prompt (prefer interpretation-specific if available)
    - Original query
    - Aggregated tool results
    - Strong interpretation instruction

    Args:
        system_prompt: The tool-loop system prompt (fallback if no interpretation-specific prompt).
        interpretation_prompt: Custom prompt for specific roles (e.g., domain_discovery uses JSON-only prompt).
        interpretation_system_prompt: Separate system prompt for interpretation phase,
            stripped of tool-loop instructions (OODA, tool usage patterns, coverage tracking).
            When provided, this replaces system_prompt to prevent the LLM from
            following tool-planning instructions during synthesis.
    """
    effective_system = interpretation_system_prompt or system_prompt
    return [
        {"role": "system", "content": effective_system},
        {
            "role": "user",
            "content": (
                f"Original query: {original_query}\n\n"
                f"=== TOOL RESULTS ===\n{tool_results_summary}\n\n"
                f"=== YOUR TASK ===\n{interpretation_prompt}"
            )
        }
    ]


def aggregate_tool_results(tool_records: List[Dict[str, Any]], max_chars: int = 50000) -> str:
    """
    Aggregate tool execution results into a readable summary.

    This creates a clean summary of all tool outputs without the
    back-and-forth "I'll execute..." history.
    """
    import json

    parts = []
    total_chars = 0

    for record in tool_records:
        tool_name = record.get("tool", "unknown")
        success = record.get("success", False)
        output = record.get("output", {})
        tool_input = record.get("tool_input") or {}
        error = record.get("error") or ""

        part_header = f"\n### {tool_name} result:\n"
        if not success:
            part_header = f"\n### {tool_name} failed:\n"
            target = ""
            if isinstance(tool_input, dict):
                if tool_input.get("url"):
                    target = f"url={tool_input.get('url')}"
                elif tool_input.get("urls"):
                    target = f"urls={tool_input.get('urls')}"
                elif tool_input.get("query"):
                    target = f"query={tool_input.get('query')}"
            lines = []
            if target:
                lines.append(f"- Input: {target}")
            if error:
                lines.append(f"- Error: {error}")
            else:
                lines.append("- Error: (none)")
            part = part_header + ("\n".join(lines) + "\n")
            if total_chars + len(part) > max_chars:
                break
            parts.append(part)
            total_chars += len(part)
            continue

        if tool_name == "web_search":
            # Handle both dict format {"results": [...]} and direct array format [...]
            if isinstance(output, dict):
                results = output.get("results", [])
            elif isinstance(output, list):
                results = output
            else:
                results = []

            part_content = ""
            if not results:
                part_content = "(No search results found)\n"
            else:
                for r in results[:8]:
                    title = r.get("title", "")
                    snippet = r.get("snippet", "")
                    url = r.get("url", "")
                    part_content += f"- {title}: {snippet}\n  URL: {url}\n"
            part = part_header + part_content

        elif tool_name == "web_fetch":
            part_content = ""

            if isinstance(output, dict):
                # Check if this is batch mode (has "pages" key) or single URL mode
                if "pages" in output:
                    # Batch mode: {pages: [...], succeeded, failed, ...}
                    pages = output.get("pages", [])
                    for page in pages:
                        if not page.get("success", True):  # Default to True if not specified
                            continue
                        title = page.get("title", "Untitled")
                        content = page.get("content", "")
                        url = page.get("url", "")

                        # Skip binary content (PDFs)
                        if content and not content.startswith("%PDF"):
                            content_preview = content[:6000] if len(content) > 6000 else content
                            part_content += f"**{title}** ({url}):\n{content_preview}\n\n"
                else:
                    # Single URL mode: {url, title, content, ...}
                    title = output.get("title", "Untitled")
                    content = output.get("content", "")
                    url = output.get("url", "")

                    # Skip binary content (PDFs)
                    if content and not content.startswith("%PDF"):
                        content_preview = content[:8000] if len(content) > 8000 else content
                        part_content = f"**{title}** ({url}):\n{content_preview}\n\n"
                    elif not content:
                        part_content = f"**{title}** ({url}): No content retrieved\n"

            if not part_content:
                part_content = "(No content fetched)\n"
            part = part_header + part_content

        elif tool_name == "web_subpage_fetch":
            # web_subpage_fetch returns merged multi-page content with separators
            # Critical: Extract content from EACH subpage, not just truncate from start
            # This ensures important pages like /leadership (often at end) are preserved
            if isinstance(output, dict):
                title = output.get("title", "")
                content = output.get("content", "")
                url = output.get("url", "")
                pages_fetched = output.get("pages_fetched", 1)
                metadata_urls = output.get("metadata", {}).get("urls", [])

                if content and not content.startswith("%PDF"):
                    import re
                    # Parse content by subpage separators to extract each page
                    # Format: "# Main Page: URL\n...\n---\n\n## Subpage N: URL\n..."
                    page_sections = []

                    # Split by subpage markers: "---\n\n## Subpage" or "# Main Page:"
                    main_match = re.search(r'^# Main Page: ([^\n]+)\n(.+?)(?=\n---\n\n## Subpage|$)', content, re.DOTALL)
                    if main_match:
                        main_url = main_match.group(1)
                        main_content = main_match.group(2).strip()
                        # Limit main page to 4000 chars (increased for domain prefetch)
                        page_sections.append(f"**Main ({main_url})**: {main_content[:4000]}")

                    # Extract subpages - these often contain important info
                    subpage_pattern = re.compile(r'## Subpage \d+: ([^\n]+)\n(?:\*\*[^*]+\*\*\n)?\n(.+?)(?=\n---\n\n## Subpage|$)', re.DOTALL)
                    for match in subpage_pattern.finditer(content):
                        sub_url = match.group(1)
                        sub_content = match.group(2).strip()
                        # Prioritize key paths with more content (leadership, team, about)
                        priority_paths = ['/leadership', '/team', '/management', '/about', '/executive']
                        is_priority = any(p in sub_url.lower() for p in priority_paths)
                        max_sub_chars = 5000 if is_priority else 3000
                        page_sections.append(f"**{sub_url}**: {sub_content[:max_sub_chars]}")

                    # Build final content preserving snippets from all pages
                    if page_sections:
                        part_content = "\n\n".join(page_sections)
                        # Total limit for multi-page (increased for domain prefetch: 10 pages × 3K)
                        if len(part_content) > 30000:
                            part_content = part_content[:30000] + "..."
                        part = part_header + f"**{title}** ({url}, {pages_fetched} pages):\n{part_content}\n\n"
                    else:
                        # Fallback if parsing fails
                        part = part_header + f"**{title}** ({url}, {pages_fetched} pages):\n{content[:6000]}\n\n"
                else:
                    part = part_header + f"**{title}** ({url}): No readable content\n"
            else:
                part = part_header + str(output)[:3000]

        elif tool_name == "web_crawl":
            # web_crawl returns comprehensive multi-page crawl results
            # Format: "# Main Page: URL\n...\n---\n\n## Page N: URL\n..."
            # Critical: Extract content from EACH page to preserve blog posts, articles etc.
            if isinstance(output, dict):
                title = output.get("title", "")
                content = output.get("content", "")
                url = output.get("url", "")
                pages_fetched = output.get("pages_fetched", 1)
                char_count = output.get("char_count", 0)
                metadata = output.get("metadata", {})
                crawled_urls = metadata.get("urls", [])

                if content and not content.startswith("%PDF"):
                    import re
                    page_sections = []

                    # Parse main page: "# Main Page: URL\n..."
                    main_match = re.search(r'^# Main Page: ([^\n]+)\n(.+?)(?=\n---\n\n## Page \d+:|$)', content, re.DOTALL)
                    if main_match:
                        main_url = main_match.group(1)
                        main_content = main_match.group(2).strip()
                        # Main page gets more space (5000 chars)
                        page_sections.append(f"**Main ({main_url})**: {main_content[:5000]}")

                    # Parse subsequent pages: "## Page N: URL\n..."
                    page_pattern = re.compile(r'## Page \d+: ([^\n]+)\n(.+?)(?=\n---\n\n## Page \d+:|$)', re.DOTALL)
                    for match in page_pattern.finditer(content):
                        page_url = match.group(1)
                        page_content = match.group(2).strip()

                        # Prioritize important pages with more content
                        priority_paths = ['/blog', '/article', '/post', '/about', '/team', '/leadership']
                        is_priority = any(p in page_url.lower() for p in priority_paths)

                        # Skip sitemap.xml raw content (usually not useful for interpretation)
                        is_sitemap = 'sitemap.xml' in page_url.lower()

                        if is_sitemap:
                            # Just note the sitemap exists, don't include raw XML
                            page_sections.append(f"**{page_url}**: (sitemap with {len(crawled_urls)} URLs)")
                        elif is_priority:
                            # Priority pages get 4000 chars each
                            page_sections.append(f"**{page_url}**: {page_content[:4000]}")
                        else:
                            # Other pages get 2000 chars each
                            page_sections.append(f"**{page_url}**: {page_content[:2000]}")

                    # Build final content
                    if page_sections:
                        part_content = "\n\n".join(page_sections)
                        # Total limit for crawl: 40000 chars (crawl typically fetches more pages)
                        if len(part_content) > 40000:
                            part_content = part_content[:40000] + "\n\n[...truncated, see full crawl output]"
                        part = part_header + f"**{title}** ({url}, {pages_fetched} pages, {char_count} chars):\n{part_content}\n\n"
                    else:
                        # Fallback if parsing fails - still preserve substantial content
                        part = part_header + f"**{title}** ({url}, {pages_fetched} pages):\n{content[:10000]}\n\n"
                else:
                    part = part_header + f"**{title}** ({url}): No readable content\n"
            else:
                part = part_header + str(output)[:5000]

        else:
            # Generic output
            if isinstance(output, dict):
                part = part_header + json.dumps(output, ensure_ascii=False, indent=2)[:1500]
            else:
                part = part_header + str(output)[:1500]

        if total_chars + len(part) > max_chars:
            break
        parts.append(part)
        total_chars += len(part)

    return "\n".join(parts) if parts else "No tool results available."


def validate_interpretation_output(
    output: str,
    total_tool_output_chars: int,
    *,
    expect_sources_format: bool
) -> Tuple[bool, str]:
    """
    Validate interpretation pass output quality.

    Returns (is_valid, reason) tuple.

    For source format (research roles): strict validation with format checks.
    For general format (other roles): lenient validation, only check basics.
    """
    stripped = output.strip()

    # Basic check: must have some content
    if not stripped or len(stripped) < 50:
        return False, "too_short"

    # Multi-language "continuation" pattern detection (all formats)
    continuation_patterns_en = ("I'll ", "I need to ", "Let me ", "I will ", "I should ")
    continuation_patterns_zh = ("我将", "我需要", "让我", "我要", "我继续")
    continuation_patterns_ja = ("私は", "必要が", "させて", "続けて", "取得します")

    is_continuation = (
        stripped.startswith(continuation_patterns_en) or
        stripped.startswith(continuation_patterns_zh) or
        stripped.startswith(continuation_patterns_ja)
    )

    if is_continuation:
        return False, "continuation_pattern"

    # OODA loop pattern detection: interpretation output should be synthesis, not planning.
    # The OODA framework (Observe/Orient/Decide/Act) belongs to the tool loop phase.
    # If interpretation outputs OODA sections, the system prompt contaminated the synthesis.
    ooda_section_markers = ("## Observe", "## Orient", "## Decide", "## Act",
                            "# Observe", "# Orient", "# Decide", "# Act")
    ooda_section_count = sum(1 for marker in ooda_section_markers if marker in stripped)
    if ooda_section_count >= 2:
        return False, "ooda_pattern"

    # Source format: strict validation
    if expect_sources_format:
        # Lowered from 2000 to 500: when public info is scarce, LLM may honestly
        # produce shorter summaries. Previous threshold caused excessive fallbacks.
        min_length = min(500, max(200, int(total_tool_output_chars * 0.1)))
        if len(stripped) < min_length:
            return False, f"too_short (len={len(stripped)} < min={min_length})"
        # Check format
        has_correct_format = stripped.startswith(("# PART 1", "# PART", "## PART", "PART 1"))
        if not has_correct_format and len(stripped) < 500:
            return False, "no_format_and_short"

    # General format: lenient validation - no format or length ratio checks
    # Just ensure it's not a continuation pattern and has basic content

    return True, "valid"


def build_task_contract_instructions(context: Dict[str, Any]) -> str:
    """
    Deep Research 2.0: Build task contract instructions for agent execution.

    Extracts task contract fields from context and returns instructions
    to append to the system prompt.
    """
    if not isinstance(context, dict):
        return ""

    instructions = []

    # Output format instructions
    output_format = context.get("output_format")
    if output_format and isinstance(output_format, dict):
        format_type = output_format.get("type", "narrative")
        required_fields = output_format.get("required_fields", [])
        optional_fields = output_format.get("optional_fields", [])

        instructions.append(f"\n## Output Format: {format_type}")
        if required_fields:
            instructions.append(f"REQUIRED fields: {', '.join(required_fields)}")
        if optional_fields:
            instructions.append(f"OPTIONAL fields: {', '.join(optional_fields)}")

    # Source guidance instructions
    source_guidance = context.get("source_guidance")
    if source_guidance and isinstance(source_guidance, dict):
        required_sources = source_guidance.get("required", [])
        optional_sources = source_guidance.get("optional", [])
        avoid_sources = source_guidance.get("avoid", [])

        instructions.append("\n## Source Guidance")
        if required_sources:
            instructions.append(f"PRIORITIZE sources from: {', '.join(required_sources)}")
        if optional_sources:
            instructions.append(f"May also use: {', '.join(optional_sources)}")
        if avoid_sources:
            instructions.append(f"AVOID sources like: {', '.join(avoid_sources)}")

    # Search budget instructions
    search_budget = context.get("search_budget")
    if search_budget and isinstance(search_budget, dict):
        max_queries = search_budget.get("max_queries", 10)
        max_fetches = search_budget.get("max_fetches", 20)

        instructions.append("\n## Search Budget")
        instructions.append(f"Maximum {max_queries} web_search calls, {max_fetches} web_fetch calls")
        instructions.append("Be efficient - focus on high-value sources first")

    # Boundary instructions
    boundaries = context.get("boundaries")
    if boundaries and isinstance(boundaries, dict):
        in_scope = boundaries.get("in_scope", [])
        out_of_scope = boundaries.get("out_of_scope", [])

        instructions.append("\n## Scope Boundaries")
        if in_scope:
            instructions.append(f"FOCUS ON: {', '.join(in_scope)}")
        if out_of_scope:
            instructions.append(f"DO NOT cover: {', '.join(out_of_scope)}")

    if instructions:
        return "\n\n--- TASK CONTRACT ---" + "\n".join(instructions)
    return ""


class ForcedToolCall(BaseModel):
    tool: str = Field(..., description="Tool name to execute")
    parameters: Dict[str, Any] = Field(
        default_factory=dict, description="Parameters for the tool"
    )


class AgentQuery(BaseModel):
    """Query from an agent."""

    query: str = Field(..., description="The query or task description")
    context: Optional[Dict[str, Any]] = Field(
        default_factory=dict, description="Context for the query"
    )
    agent_id: Optional[str] = Field(default="default", description="Agent identifier")
    mode: Optional[str] = Field(
        default="standard", description="Execution mode: simple, standard, or complex"
    )
    allowed_tools: Optional[List[str]] = Field(
        default=None,
        description="Allowlist of tools available for this query. None means use role preset, [] means no tools.",
    )
    forced_tool_calls: Optional[List[ForcedToolCall]] = Field(
        default=None,
        description="Explicit sequence of tool calls to execute before interpretation",
    )
    max_tokens: Optional[int] = Field(
        default=None, description="Maximum tokens for response (None = use role/tier defaults, typically 4096 for GPT-5)"
    )
    temperature: Optional[float] = Field(
        default=0.7, description="Temperature for generation"
    )
    model_tier: Optional[str] = Field(
        default=None, description="Model tier: small, medium, or large (None = use context or default to small)"
    )
    model_override: Optional[str] = Field(
        default=None,
        description="Override the default model selection with a specific model ID",
    )
    stream: Optional[bool] = Field(
        default=False,
        description="Enable streaming responses (returns SSE-style chunked deltas)",
    )


class AgentResponse(BaseModel):
    """Response to an agent query."""

    success: bool = Field(
        ..., description="Whether the query was processed successfully"
    )
    response: str = Field(..., description="The generated response")
    tokens_used: int = Field(..., description="Number of tokens used")
    model_used: str = Field(..., description="Model that was used")
    provider: str = Field(default="unknown", description="Provider that served the request")
    finish_reason: str = Field(default="stop", description="Reason the model stopped generating (stop, length, content_filter, etc.)")
    metadata: Dict[str, Any] = Field(
        default_factory=dict, description="Additional metadata"
    )


class MockProvider:
    """Mock LLM provider for testing without API keys."""

    def __init__(self):
        self.responses = {
            "hello": "Hello! I'm a mock agent ready to help with your task.",
            "test": "This is a test response from the mock provider.",
            "analyze": "I've analyzed your request. The complexity is moderate and can be handled with standard execution mode.",
            "default": "I understand your request. Here's my mock response for testing purposes.",
        }

    async def generate(self, query: str, **kwargs) -> Dict[str, Any]:
        """Generate a mock response."""
        # Simple keyword matching for deterministic responses
        response_text = self.responses.get("default")
        for keyword, response in self.responses.items():
            if keyword.lower() in query.lower():
                response_text = response
                break

        return {
            "response": response_text,
            "tokens_used": len(response_text.split()) * 2,  # Rough token estimate
            "model_used": "mock-model-v1",
        }


# Global mock provider instance
mock_provider = MockProvider()


@router.post("/agent/query", response_model=AgentResponse)
async def agent_query(request: Request, query: AgentQuery):
    """
    Process a query from an agent.

    This endpoint provides HTTP-based communication for Agent-Core,
    as an alternative to gRPC during development.
    """
    try:
        logger.info(f"Received agent query: {query.query[:100]}...")
        # Ensure allowed_tools metadata is always defined for responses
        effective_allowed_tools: List[str] = []

        # Check if we have real providers configured
        if (
            hasattr(request.app.state, "providers")
            and request.app.state.providers.is_configured()
        ):
            # Use real provider - convert query to messages format
            # Roles v1: choose system prompt from role preset if provided in context
            try:
                from ..roles.presets import get_role_preset, render_system_prompt

                requested_role = None
                role_name = None
                if isinstance(query.context, dict):
                    requested_role = query.context.get("role") or query.context.get(
                        "agent_type"
                    )
                    role_name = requested_role

                # Default to deep_research_agent for research workflows
                if not role_name and isinstance(query.context, dict):
                    if query.context.get("force_research") or query.context.get("workflow_type") == "research":
                        role_name = "deep_research_agent"

                effective_role = str(role_name).strip() if role_name else "generalist"
                preset = get_role_preset(effective_role)

                # Check for system_prompt in context first, then fall back to preset
                system_prompt = None
                if isinstance(query.context, dict) and "system_prompt" in query.context:
                    system_prompt = str(query.context.get("system_prompt"))

                system_prompt_source = "context.system_prompt" if system_prompt else f"role_preset:{effective_role}"
                if not system_prompt:
                    system_prompt = str(
                        preset.get("system_prompt") or "You are a helpful AI assistant."
                    )

                # Render templated system prompt using context parameters
                try:
                    system_prompt = render_system_prompt(
                        system_prompt, query.context or {}
                    )
                except Exception as e:
                    # On any rendering issue, keep original system_prompt
                    logger.warning(f"System prompt rendering failed: {e}")

                # Inject current date for time awareness
                # Skip for citation_agent (it only inserts [n] markers, no reasoning needed)
                skip_date_injection = query.agent_id == "citation_agent"
                if isinstance(query.context, dict) and query.context.get("agent_id") == "citation_agent":
                    skip_date_injection = True
                
                if not skip_date_injection:
                    # Read from context (set by Go orchestrator) or fallback to local time
                    current_date = None
                    if isinstance(query.context, dict):
                        # Try context["current_date"] first, then prompt_params
                        current_date = query.context.get("current_date")
                        if not current_date:
                            prompt_params = query.context.get("prompt_params")
                            if isinstance(prompt_params, dict):
                                current_date = prompt_params.get("current_date")
                    if not current_date:
                        from datetime import datetime, timezone
                        current_date = datetime.now(timezone.utc).strftime("%Y-%m-%d")
                    
                    # Prepend date to system prompt
                    date_prefix = f"Current date: {current_date} (UTC).\n\n"
                    system_prompt = date_prefix + system_prompt

                # Add language instruction if target_language is specified in context
                if isinstance(query.context, dict) and "target_language" in query.context:
                    target_lang = query.context.get("target_language")
                    if target_lang and target_lang != "English":
                        language_instruction = f"\n\nCRITICAL: Respond in {target_lang}. The user's query is in {target_lang}. You MUST respond in the SAME language. DO NOT translate to English."
                        system_prompt = language_instruction + "\n\n" + system_prompt


                # Add research-mode instruction for deep content retrieval
                # EXCEPTION: Do NOT inject for REASON steps (no tools, pure reasoning)
                # EXCEPTION: Do NOT inject for specialized roles with strict output format
                is_reason_step = query.query.strip().startswith("REASON (")
                skip_research_injection_roles = {"domain_discovery", "domain_prefetch", "citation_agent"}
                role = query.context.get("role") if isinstance(query.context, dict) else None
                skip_research_injection = role in skip_research_injection_roles

                if isinstance(query.context, dict) and not is_reason_step and not skip_research_injection:
                    is_research = (
                        query.context.get("force_research")
                        or query.context.get("research_strategy")
                        or query.context.get("research_mode")
                        or query.context.get("workflow_type") == "research"
                    )
                    if is_research:
                        research_instruction = (
                            "\n\nRESEARCH MODE - MANDATORY FETCH POLICY:"
                            "\n- Search snippets are LEADS, NOT verified content. You MUST fetch to verify."
                            "\n- After EACH web_search, you MUST call web_fetch on ALL potentially relevant URLs."
                            "\n- Only skip URLs that are CLEARLY irrelevant (e.g., wrong language, completely different topic, broken links)."
                            "\n- Use batch fetch: web_fetch(urls=[url1, url2, ...]) for efficiency."
                            "\n- Minimum: fetch at least 5-8 URLs per search, or ALL results if fewer."
                            "\n- Do NOT stop fetching just because you found 'enough' - more sources = better research."
                            "\n\nTOOL USAGE (CRITICAL):"
                            "\n- Invoke tools ONLY via native function calling (no XML/JSON stubs like <web_fetch> or <function_calls>)."
                            "\n- ALWAYS use batch fetch: web_fetch(urls=[...]) instead of single URL calls."
                            "\n- Do NOT claim in text which tools/providers you used; the system records tool usage."
                            "\n\nCOMPANY/ENTITY RESEARCH: When researching a company or organization:"
                            "\n- FIRST try web_fetch on the likely official domain (e.g., 'companyname.com', 'companyname.io')"
                            "\n- Try alternative domains: products may have different names (e.g., Ptmind → ptengine.com)"
                            "\n- Search for '[company] site:linkedin.com' or '[company] site:crunchbase.com'"
                            "\n- For Asian companies, try Japanese/Chinese name variants"
                            "\n- If standard searches return only competitors/unrelated results, this indicates a search strategy problem - try direct URL fetches"
                            "\n\nSOURCE EVALUATION AND CONFLICT RESOLUTION:"
                            "\n1. VERIFICATION (MANDATORY): Search snippets are NOT facts. You MUST web_fetch EVERY source before citing it."
                            "\n2. SPECULATIVE LANGUAGE: Mark uncertain claims (reportedly, allegedly, may, sources suggest)."
                            "\n3. SOURCE PRIORITY (highest to lowest):"
                            "\n   - Official sources (company website, .gov, .edu, investor relations)"
                            "\n   - Authoritative aggregators (Crunchbase, LinkedIn, Wikipedia)"
                            "\n   - News outlets (Reuters, Bloomberg, TechCrunch)"
                            "\n   - Blog posts, forums, social media"
                            "\n4. TIME PRIORITY:"
                            "\n   - For DYNAMIC topics (pricing, team, products, market data): prefer sources from last 6-12 months"
                            "\n   - For STATIC topics (founding date, history): any authoritative source"
                            "\n   - When search results include 'date'/'published_date' field, use it; otherwise note 'date unknown'"
                            "\n5. CONFLICT HANDLING (MANDATORY when sources disagree):"
                            "\n   - LIST all conflicting claims with their sources and dates"
                            "\n   - RANK by: (1) source authority, (2) recency"
                            "\n   - EXPLICITLY STATE which version you prioritize and WHY"
                            "\n   - Format: 'According to [Official Site, Dec 2024]: X. However, [News, Jun 2023] reported Y.'"
                            "\n   - NEVER silently choose one version without disclosure"
                            "\n6. OUTPUT TEMPORAL MARKERS:"
                            "\n   - Include 'As of [date]...' for time-sensitive facts"
                            "\n   - Note when information may be outdated: '[Note: This data is from 2022]'"
                        )
                        system_prompt = system_prompt + research_instruction
                        logger.info("Applied RESEARCH MODE instruction to system prompt")

                # Log when research_mode injection is skipped for specialized roles
                if skip_research_injection and role:
                    logger.info(f"Skipped research_mode injection for specialized role: {role}")

                # REASON step: Add explicit instruction to prevent stub output
                if is_reason_step:
                    reason_instruction = (
                        "\n\nIMPORTANT: This is a REASONING step. You have NO tools available."
                        "\n- Output ONLY your reasoning and decision (search/no_search)."
                        "\n- Do NOT output any tool calls, XML tags, JSON, or function call stubs."
                        "\n- Do NOT use <function_calls>, <invoke>, <web_fetch>, or similar markup."
                        "\n- Simply provide your reasoning in plain text."
                    )
                    system_prompt = system_prompt + reason_instruction
                    logger.info("Applied REASON step instruction (no tools, no stubs)")

                # Deep Research 2.0: Add task contract instructions if present in context
                if isinstance(query.context, dict):
                    task_contract_instructions = build_task_contract_instructions(query.context)
                    if task_contract_instructions:
                        system_prompt = system_prompt + task_contract_instructions
                        logger.info("Applied Deep Research 2.0 task contract instructions to system prompt")

                cap_overrides = preset.get("caps") or {}
                # GPT-5 models need more tokens for reasoning + output (default 4096 instead of 2048)
                default_max_tokens = 4096  # Increased for GPT-5 reasoning models
                try:
                    # Check query.max_tokens, then context.max_tokens (set by budget.go), then role caps
                    if query.max_tokens is not None:
                        max_tokens = int(query.max_tokens)
                    elif isinstance(query.context, dict) and query.context.get("max_tokens"):
                        max_tokens = int(query.context.get("max_tokens"))
                    else:
                        max_tokens = int(cap_overrides.get("max_tokens") or default_max_tokens)
                    logger.info(f"Agent query max_tokens: final={max_tokens}")
                except Exception as e:
                    logger.warning(f"Failed to parse max_tokens: {e}, using default")
                    max_tokens = int(cap_overrides.get("max_tokens") or default_max_tokens)
                    logger.info(f"Agent query max_tokens (exception path): final={max_tokens}")
                try:
                    temperature = float(query.temperature) if query.temperature is not None else float(cap_overrides.get("temperature") or 0.7)
                except Exception:
                    temperature = float(cap_overrides.get("temperature") or 0.7)
            except Exception:
                system_prompt = "You are a helpful AI assistant."
                max_tokens = query.max_tokens
                temperature = query.temperature

            messages = [{"role": "system", "content": system_prompt}]

            # Rehydrate history from context if present
            history_rehydrated = False
            logger.info(
                f"Context keys: {list(query.context.keys()) if isinstance(query.context, dict) else 'Invalid context type'}"
            )
            if query.context and "history" in query.context:
                history_str = str(query.context.get("history", ""))
                logger.info(
                    f"History string length: {len(history_str)}, preview: {history_str[:100] if history_str else 'Empty'}"
                )
                if history_str:
                    # Parse the history string format: "role: content\n"
                    for line in history_str.strip().split("\n"):
                        if ": " in line:
                            role, content = line.split(": ", 1)
                            # Only add user and assistant messages to maintain conversation flow
                            if role.lower() in ["user", "assistant"]:
                                messages.append(
                                    {"role": role.lower(), "content": content}
                                )
                                history_rehydrated = True

                    # Remove history from context to avoid duplication
                    context_without_history = {
                        k: v for k, v in query.context.items() if k != "history"
                    }
                else:
                    context_without_history = query.context
            else:
                context_without_history = query.context if query.context else {}

            # Add current query as the final user message
            messages.append({"role": "user", "content": query.query})

            # Add semantic context to system prompt (WHITELIST approach for security)
            # Only include fields explicitly meant for LLM consumption.
            # Session-scoped fields are minimal; task-scoped fields are included only when a workflow/task marker is present.
            if context_without_history:
                def _truncate_text(text: str, max_chars: int) -> str:
                    if len(text) <= max_chars:
                        return text
                    return text[:max_chars] + "\n...[TRUNCATED]"

                def _format_template_results(value: Any) -> str:
                    if not isinstance(value, dict):
                        return _truncate_text(str(value), 20000)
                    node_ids = sorted([str(k) for k in value.keys()])
                    max_nodes = 20
                    per_node_max_chars = 12000
                    parts: List[str] = []
                    for idx, node_id in enumerate(node_ids):
                        if idx >= max_nodes:
                            parts.append(f"... ({len(node_ids) - max_nodes} more nodes omitted)")
                            break
                        node_output = value.get(node_id)
                        node_text = (
                            node_output if isinstance(node_output, str) else str(node_output)
                        )
                        parts.append(f"[{node_id}]\n{_truncate_text(node_text, per_node_max_chars)}")
                    return "\n" + "\n\n".join(parts) if parts else ""

                def _format_dependency_results(value: Any) -> str:
                    if not isinstance(value, dict):
                        return _truncate_text(str(value), 20000)
                    dep_ids = sorted([str(k) for k in value.keys()])
                    max_deps = 20
                    per_dep_max_chars = 8000
                    parts: List[str] = []
                    for idx, dep_id in enumerate(dep_ids):
                        if idx >= max_deps:
                            parts.append(f"... ({len(dep_ids) - max_deps} more deps omitted)")
                            break
                        dep_val = value.get(dep_id)
                        dep_text = dep_val if isinstance(dep_val, str) else str(dep_val)
                        parts.append(f"[{dep_id}]\n{_truncate_text(dep_text, per_dep_max_chars)}")
                    return "\n" + "\n\n".join(parts) if parts else ""

                def _format_context_value(key: str, value: Any) -> str:
                    if key == "template_results":
                        return _format_template_results(value)
                    if key == "dependency_results":
                        return _format_dependency_results(value)
                    return _truncate_text(str(value), 20000)

                session_allowed = {
                    "agent_memory",    # Conversation memory items (injected by workflows)
                    "context_summary", # Compressed context history (injected by workflows)
                }
                task_allowed = {
                    # ReAct / dependency context (transient)
                    "observations",
                    "thoughts",
                    "actions",
                    "current_thought",
                    "iteration",
                    "previous_results",
                    # Research hints (transient)
                    "exact_queries",
                    "official_domains",
                    "disambiguation_terms",
                    "canonical_name",
                    # Template workflow: upstream node outputs
                    "template_results",
                    "dependency_results",
                    # Research planning context (from Refiner)
                    "research_areas",
                    "research_dimensions",
                    "target_languages",
                }

                # Treat context as task-scoped if workflow metadata is present
                is_task_scoped = any(
                    key in context_without_history
                    for key in (
                        "parent_workflow_id",
                        "workflow_id",
                        "task_id",
                        "force_research",
                        "research_strategy",
                        "previous_results",
                        # Template workflow markers
                        "template_results",
                        "dependency_results",
                        "template_node_id",
                    )
                )

                allowed_keys = session_allowed | (task_allowed if is_task_scoped else set())

                safe_items = [
                    (k, v)
                    for k, v in context_without_history.items()
                    if k in allowed_keys and v is not None
                ]
                if safe_items:
                    context_str = "\n".join(
                        [f"{k}: {_format_context_value(k, v)}" for k, v in safe_items]
                    )
                    messages[0]["content"] += f"\n\nContext:\n{context_str}"

            # Optional JSON enforcement passthrough: allow callers to request JSON via context
            response_format = None
            try:
                if isinstance(query.context, dict):
                    rf = query.context.get("response_format")
                    if isinstance(rf, dict) and rf:
                        response_format = rf
            except Exception:
                response_format = None
            expects_json_response = _response_format_expects_json(response_format)

            # Soft enforcement: if caller requests tool usage and tools are allowed, nudge the model
            force_tools = False
            try:
                if isinstance(query.context, dict):
                    force_tools = bool(query.context.get("force_tools"))
            except Exception:
                force_tools = False

            # Log for debugging
            logger.info(
                f"Prepared {len(messages)} messages for LLM (history_rehydrated={history_rehydrated})"
            )

            # Get the appropriate model tier
            from ..providers.base import ModelTier

            tier_map = {
                "small": ModelTier.SMALL,
                "medium": ModelTier.MEDIUM,
                "large": ModelTier.LARGE,
            }
            # Precedence: explicit top-level query.model_tier > context.model_tier > default
            # Always honor top-level, including "small" when explicitly provided
            tier = None

            # 1) Top-level override takes precedence when provided and valid
            if isinstance(query.model_tier, str):
                top_level_tier = query.model_tier.lower().strip()
                mapped_tier = tier_map.get(top_level_tier, None)
                if mapped_tier is not None:
                    tier = mapped_tier

            # 2) Fallback to context if top-level not set/invalid
            if tier is None and isinstance(query.context, dict):
                ctx_tier_raw = query.context.get("model_tier")
                if isinstance(ctx_tier_raw, str):
                    ctx_tier = ctx_tier_raw.lower().strip()
                    tier = tier_map.get(ctx_tier, None)

            # 3) Final fallback to default
            if tier is None:
                tier = ModelTier.SMALL

            # Check for model override (from query field, context, or role preset)
            model_override = query.model_override or (
                query.context.get("model_override") if query.context else None
            )
            # Optional provider override (from context or role preset)
            try:
                provider_override = (
                    query.context.get("provider_override") if query.context else None
                )
            except Exception:
                provider_override = None
            # Allow role preset to specify provider preference when not explicitly set
            if not provider_override and preset and "provider_override" in preset:
                try:
                    provider_override = str(preset.get("provider_override")).strip() or None
                except Exception:
                    provider_override = None
            # Apply role preset's preferred_model if no explicit override
            if not model_override and preset and "preferred_model" in preset:
                model_override = preset.get("preferred_model")
                logger.info(f"Using role preset preferred model: {model_override}")
            elif model_override:
                logger.info(f"Using model override: {model_override}")
            else:
                chosen = query.model_tier or ((query.context or {}).get("model_tier") if isinstance(query.context, dict) else None)
                logger.info(f"Using tier-based selection (top-level>context): {chosen or 'small'} -> {tier}")

            # Resolve effective allowed tools: request.allowed_tools (intersect with preset when present)
            effective_allowed_tools: List[str] = []
            try:
                from ..tools import get_registry

                registry = get_registry()
                # Use preset only if allowed_tools is None (not provided), not if it's [] (explicitly empty)
                # IMPORTANT: allowed_tools=[] means "explicitly no tools" - do NOT override with preset
                # This is critical for REASON steps in ReactLoop which must not have tools available
                requested = query.allowed_tools
                preset_allowed = list(preset.get("allowed_tools", []))

                # Check for explicit "use preset tools" flag in context (opt-in bypass)
                use_preset_tools_override = (
                    isinstance(query.context, dict)
                    and query.context.get("use_preset_tools") is True
                )

                # Only use preset if:
                # 1. requested is None (not provided), OR
                # 2. explicit use_preset_tools=True override in context
                if requested is not None and len(requested) == 0:
                    if use_preset_tools_override and preset and len(preset_allowed) > 0:
                        logger.info(f"Explicit use_preset_tools override - using role preset tools: {preset_allowed}")
                        requested = None
                    else:
                        # allowed_tools=[] explicitly means NO tools - respect this
                        logger.info("allowed_tools=[] explicitly set - no tools will be available")

                if requested is None:
                    base = preset_allowed
                else:
                    # When the role preset defines an allowlist, cap requested tools by it
                    if preset_allowed:
                        base = [t for t in requested if t in preset_allowed]
                        dropped = [t for t in (requested or []) if t not in base]
                        if dropped:
                            logger.warning(
                                f"Dropping tools not permitted by role preset: {dropped}"
                            )
                    else:
                        base = requested
                available = set(registry.list_tools())
                # Intersect with registry; warn on unknown
                unknown = [t for t in base if t not in available]
                if unknown:
                    logger.warning(f"Dropping unknown tools from allowlist: {unknown}")
                effective_allowed_tools = [t for t in base if t in available]

                # Task-type based tool filtering (limit to 5 most relevant tools)
                # This prevents LLM choice paralysis from too many tools
                # Enable via context: tool_filtering_enabled=true, task_type=research|coding|analysis|browser|file
                tool_filtering_enabled = (
                    query.context
                    and query.context.get("tool_filtering_enabled", False)
                )
                if tool_filtering_enabled and len(effective_allowed_tools) > 5:
                    task_type = query.context.get("task_type", "general") if query.context else "general"
                    max_tools = query.context.get("max_tools", 5) if query.context else 5
                    original_count = len(effective_allowed_tools)
                    effective_allowed_tools = registry.filter_tools_by_task_type(
                        task_type=task_type,
                        allowed_tools=effective_allowed_tools,
                        max_tools=max_tools,
                    )
                    logger.info(
                        f"Tool filtering applied: task_type={task_type}, "
                        f"reduced from {original_count} to {len(effective_allowed_tools)} tools"
                    )
            except Exception as e:
                logger.warning(f"Failed to compute effective allowed tools: {e}")
                effective_allowed_tools = query.allowed_tools or []

            # Collect structured tool executions for upstream observability/persistence
            tool_execution_records: List[Dict[str, Any]] = []
            seed_raw_tool_results: List[Dict[str, Any]] = []
            seed_search_urls: List[str] = []
            seed_fetch_success = False
            seed_last_tool_results = ""
            seed_loop_function_call: Optional[str] = None

            # Generate completion with tools if specified
            if effective_allowed_tools:
                logger.info(f"Allowed tools: {effective_allowed_tools}")
                if force_tools:
                    try:
                        messages[0]["content"] += (
                            "\n\nYou must use one of these tools to retrieve factual data: "
                            + ", ".join(effective_allowed_tools)
                            + ". Do not fabricate values."
                        )
                    except Exception:
                        pass
            tools_param = None
            if effective_allowed_tools:
                # Dynamically fetch tool schemas from registry for ALL tools (built-in and OpenAPI)
                tools_param = []
                for tool_name in effective_allowed_tools:
                    tool = registry.get_tool(tool_name)
                    if not tool:
                        logger.warning(f"Tool '{tool_name}' not found in registry")
                        continue

                    # Get schema from tool (works for both built-in and OpenAPI tools)
                    schema = tool.get_schema()
                    if schema:
                        tools_param.append({"type": "function", "function": schema})
                        logger.info(
                            f"✅ Added tool schema for '{tool_name}': {schema.get('name')}"
                        )
                    else:
                        logger.warning(f"Tool '{tool_name}' has no schema")

                logger.info(
                    f"Prepared {len(tools_param) if tools_param else 0} tool schemas to pass to LLM"
                )

            # If forced_tool_calls are provided, execute them sequentially then interpret
            if query.forced_tool_calls:
                if query.stream:
                    raise HTTPException(
                        status_code=400,
                        detail="forced_tool_calls are not supported with stream=true",
                    )
                # Validate tools against effective allowlist
                forced_calls = []
                for c in query.forced_tool_calls or []:
                    if (
                        effective_allowed_tools
                        and c.tool not in effective_allowed_tools
                    ):
                        raise HTTPException(
                            status_code=400,
                            detail=f"Forced tool '{c.tool}' is not allowed for this request",
                        )
                    forced_calls.append(
                        {"name": c.tool, "arguments": c.parameters or {}}
                    )

                logger.info(
                    f"Executing forced tool sequence: {[fc['name'] for fc in forced_calls]}"
                )
                tool_results, exec_records, raw_records = await _execute_and_format_tools(
                    forced_calls,
                    effective_allowed_tools or [],
                    query.query,
                    request,
                    query.context,
                )
                tool_execution_records.extend(exec_records)

                # Seed tool-loop context from forced tool executions (e.g., precomputed web_search)
                seed_raw_tool_results.extend(raw_records)
                for rr in raw_records:
                    if rr.get("tool") == "web_search" and rr.get("success"):
                        seed_search_urls.extend(
                            _extract_urls_from_search_output(rr.get("output"))
                        )
                    if rr.get("tool") in {"web_fetch", "web_subpage_fetch", "web_crawl"} and rr.get("success"):
                        seed_fetch_success = True
                seed_last_tool_results = tool_results or ""
                seed_loop_function_call = "auto" if tools_param else None

                # Add messages and continue with the normal tool loop (tools enabled)
                if forced_calls:
                    messages.append(
                        {
                            "role": "assistant",
                            "content": f"[Executed {forced_calls[0]['name']}]",
                        }
                    )
                messages.append(
                    {
                        "role": "user",
                        "content": (
                            f"Tool execution result:\n{tool_results}\n\n"
                            "If the information is insufficient, you may call another tool; otherwise, answer the original query."
                        ),
                    }
                )

            # When force_tools enabled and tools available, force model to use a tool
            # "any" forces the model to use at least one tool, "auto" only allows tools but doesn't force
            function_call = (
                "any"
                if (force_tools and effective_allowed_tools)
                else ("auto" if effective_allowed_tools else None)
            )

            if query.stream:
                providers = getattr(request.app.state, "providers", None)
                if not providers or not providers.is_configured():
                    raise HTTPException(
                        status_code=503, detail="LLM service not configured"
                    )
                logger.info(
                    f"[stream] agent_id={query.agent_id} mode={query.mode} tools={bool(effective_allowed_tools)}"
                )

                async def event_stream():
                    import json as _json

                    dumps = _json.dumps

                    buffer: List[str] = []
                    total_tokens = None
                    input_tokens = None
                    output_tokens = None
                    cost_usd = None
                    model_used = None
                    provider_used = None
                    async for chunk in providers.stream_completion(
                        messages=messages,
                        tier=tier,
                        specific_model=model_override,
                        provider_override=provider_override,
                        max_tokens=max_tokens,
                        temperature=temperature,
                        response_format=response_format,
                        tools=tools_param,
                        function_call=function_call,
                        workflow_id=request.headers.get("X-Workflow-ID")
                        or request.headers.get("x-workflow-id"),
                        agent_id=query.agent_id,
                    ):
                        if not chunk:
                            continue
                        if isinstance(chunk, dict):
                            # Optional structured chunk with usage/model info
                            delta = chunk.get("delta") or chunk.get("content") or ""
                            if chunk.get("usage"):
                                usage = chunk["usage"]
                                total_tokens = usage.get("total_tokens", total_tokens)
                                input_tokens = usage.get("input_tokens", input_tokens)
                                output_tokens = usage.get("output_tokens", output_tokens)
                                cost_usd = usage.get("cost_usd", cost_usd)
                            model_used = chunk.get("model") or model_used
                            provider_used = chunk.get("provider") or provider_used
                            if delta:
                                buffer.append(delta)
                                logger.debug(
                                    f"[stream] delta len={len(delta)} agent_id={query.agent_id}"
                                )
                                yield dumps(
                                    {
                                        "event": "thread.message.delta",
                                        "delta": delta,
                                        "agent_id": query.agent_id,
                                    },
                                    ensure_ascii=False,
                                ) + "\n"
                        else:
                            buffer.append(chunk)
                            yield dumps(
                                {
                                    "event": "thread.message.delta",
                                    "delta": chunk,
                                    "agent_id": query.agent_id,
                                },
                                ensure_ascii=False,
                            ) + "\n"

                    final_text = "".join(buffer)
                    yield dumps(
                        {
                            "event": "thread.message.completed",
                            "response": final_text,
                            "agent_id": query.agent_id,
                            "model": model_used or model_override or "",
                            "provider": provider_used or provider_override or "",
                            "usage": {
                                "total_tokens": total_tokens,
                                "input_tokens": input_tokens,
                                "output_tokens": output_tokens,
                                "cost_usd": cost_usd,
                            },
                        },
                        ensure_ascii=False,
                    ) + "\n"

                return StreamingResponse(event_stream(), media_type="text/event-stream")

            # -----------------------------
            # Non-stream: multi-tool loop
            # -----------------------------
            def _get_budget(name: str, default_val: int) -> int:
                try:
                    if isinstance(query.context, dict) and query.context.get(name) is not None:
                        val = int(query.context.get(name))
                        if val > 0:
                            return val
                except Exception:
                    pass
                try:
                    env_key = name.upper()
                    env_val = os.getenv(env_key)
                    if env_val:
                        val = int(env_val)
                        if val > 0:
                            return val
                except Exception:
                    pass
                return default_val

            # Detect research mode first to apply appropriate limits
            research_mode = (
                isinstance(query.context, dict)
                and (
                    query.context.get("force_research")
                    or query.context.get("research_strategy")
                    or query.context.get("research_mode")
                    or query.context.get("workflow_type") == "research"
                    or query.context.get("role") == "deep_research_agent"
                )
            )

            # Base defaults for non-research mode
            default_iterations = 3
            default_total_calls = 5
            default_output_chars = 60000

            # Research mode: high limits to avoid premature truncation
            # LLM controls actual usage via research budget in prompt
            if research_mode:
                default_iterations = 20  # Sufficient headroom for OODA loop
                default_total_calls = 25  # Support parallel tool calls
                default_output_chars = 300000  # Rich tool outputs

            max_tool_iterations = _get_budget("max_tool_iterations", default_iterations)
            max_total_tool_calls = _get_budget("max_total_tool_calls", default_total_calls)
            max_total_tool_output_chars = _get_budget("max_total_tool_output_chars", default_output_chars)
            max_urls_to_fetch = _get_budget("max_urls_to_fetch", 10)
            max_consecutive_tool_failures = _get_budget("max_consecutive_tool_failures", 2)
            # Followup instruction for non-research mode (research mode builds dynamically in loop)
            base_followup_instruction = (
                "If the information is insufficient, you may call another tool with a DIFFERENT strategy "
                "(e.g., broader/narrower terms, different keywords, alternative sources). "
                "Do NOT retry the same query if it returned empty or poor results. "
                "If 2+ attempts yield no useful data, synthesize what you have or report 'No relevant information found'."
            )

            total_tokens = 0
            total_input_tokens = 0
            total_output_tokens = 0
            total_cost_usd = 0.0
            total_tool_output_chars = 0
            loop_iterations = 0
            consecutive_tool_failures = 0
            stop_reason = "unknown"
            did_forced_fetch = False
            response_text = ""
            raw_tool_results: List[Dict[str, Any]] = list(seed_raw_tool_results)
            search_urls: List[str] = list(seed_search_urls)
            fetch_success = bool(seed_fetch_success)
            last_tool_results = seed_last_tool_results
            last_result_data: Optional[Dict[str, Any]] = None

            fetch_tools = {"web_fetch", "web_subpage_fetch", "web_crawl"}
            loop_function_call = seed_loop_function_call or function_call

            # P0 Fix: Track unique URLs for fetch ratio enforcement (Codex approved)
            fetched_urls: set = set()  # Successfully fetched URLs
            failed_fetch_urls: set = set()  # Failed fetch URLs (exclude from retry)
            search_count = 0  # Count of successful web_search calls

            # Strategy-specific fetch ratios (Codex recommendation)
            strategy_ratios = {"deep": 0.7, "academic": 0.7, "standard": 0.5, "quick": 0.3}
            research_strategy = (
                query.context.get("research_strategy", "standard")
                if isinstance(query.context, dict) else "standard"
            )
            min_fetch_ratio = strategy_ratios.get(research_strategy, 0.5)

            while True:
                result_data = await request.app.state.providers.generate_completion(
                    messages=messages,
                    tier=tier,
                    specific_model=model_override,
                    provider_override=provider_override,
                    max_tokens=max_tokens,
                    temperature=temperature,
                    response_format=response_format,
                    tools=tools_param,
                    function_call=loop_function_call,
                    workflow_id=request.headers.get("X-Workflow-ID")
                    or request.headers.get("x-workflow-id"),
                    agent_id=query.agent_id,
                )
                last_result_data = result_data

                response_text = result_data.get("output_text", "") or response_text
                usage = result_data.get("usage", {}) or {}
                try:
                    total_tokens += int(usage.get("total_tokens") or 0)
                    total_input_tokens += int(usage.get("input_tokens") or 0)
                    total_output_tokens += int(usage.get("output_tokens") or 0)
                    total_cost_usd += float(usage.get("cost_usd") or 0.0)
                except Exception:
                    pass

                # Extract tool calls from function_call field (unified provider response format)
                tool_calls_from_output = []
                fc = result_data.get("function_call")
                if fc and isinstance(fc, dict):
                    name = fc.get("name")
                    if name:
                        args = fc.get("arguments") or {}
                        if isinstance(args, str):
                            import json

                            try:
                                args = json.loads(args)
                            except json.JSONDecodeError:
                                logger.warning(f"Failed to parse arguments as JSON: {args}")
                                args = {}
                        tool_calls_from_output.append({"name": name, "arguments": args})
                        logger.info(
                            f"✅ Parsed tool call: {name} with args: {list(args.keys()) if isinstance(args, dict) else 'N/A'}"
                        )
                    else:
                        logger.warning(f"Skipping malformed tool call without name: {fc}")

                # Fallback: try to parse XML-style tool calls from output_text
                # Some LLMs output <web_search> XML instead of native function calling
                if not tool_calls_from_output and response_text:
                    import re as _re
                    xml_tool_patterns = [
                        (r'<web_search[^>]*>.*?query["\s:=]+([^"<]+)', 'web_search'),
                        (r'<web_fetch[^>]*>.*?url[s]?["\s:=]+([^"<\]]+)', 'web_fetch'),
                    ]
                    for pattern, tool_name in xml_tool_patterns:
                        match = _re.search(pattern, str(response_text), _re.IGNORECASE | _re.DOTALL)
                        if match and tool_name in (effective_allowed_tools or []):
                            extracted_arg = match.group(1).strip().strip('"\'')
                            if tool_name == 'web_search':
                                tool_calls_from_output.append({"name": tool_name, "arguments": {"query": extracted_arg}})
                            elif tool_name == 'web_fetch':
                                tool_calls_from_output.append({"name": tool_name, "arguments": {"urls": [extracted_arg]}})
                            logger.info(f"🔧 Recovered XML tool call: {tool_name} from output_text")
                            break  # Only recover one tool call per iteration

                if not tool_calls_from_output or not effective_allowed_tools:
                    stop_reason = "no_tool_call"
                    break

                tool_results, exec_records, raw_records = await _execute_and_format_tools(
                    tool_calls_from_output,
                    effective_allowed_tools,
                    query.query,
                    request,
                    query.context,
                )
                last_tool_results = tool_results
                tool_execution_records.extend(exec_records)
                raw_tool_results.extend(raw_records)

                if loop_function_call == "any":
                    loop_function_call = "auto"

                for rr in raw_records:
                    if rr.get("tool") == "web_search" and rr.get("success"):
                        search_urls.extend(_extract_urls_from_search_output(rr.get("output")))
                        search_count += 1  # P0 Fix: Track search count for ratio

                    # P0 Fix: Track unique fetched/failed URLs for ratio enforcement
                    if rr.get("tool") in fetch_tools:
                        # Extract URLs from fetch input
                        fetch_input = rr.get("input", {})
                        if isinstance(fetch_input, dict):
                            input_urls = fetch_input.get("urls", [])
                            if isinstance(input_urls, str):
                                input_urls = [input_urls]
                        else:
                            input_urls = []

                        if rr.get("success"):
                            fetch_success = True
                            fetched_urls.update(input_urls)
                        else:
                            failed_fetch_urls.update(input_urls)

                if raw_records and not all(r.get("success") for r in raw_records):
                    consecutive_tool_failures += 1
                else:
                    consecutive_tool_failures = 0

                total_tool_output_chars += len(tool_results or "")
                loop_iterations += 1

                # Build dynamic followup instruction for research mode
                if research_mode:
                    current_iteration = loop_iterations
                    # Simple, non-repetitive followup - system prompt has full OODA guidance
                    followup_instruction = (
                        f"**[ITERATION {current_iteration}/{max_tool_iterations}]**\n\n"
                        f"OODA: Observe results → Orient (check research_areas gaps) → Decide → Act\n"
                        f"When ready, output final synthesis starting with '## Key Findings'.\n"
                    )
                else:
                    # Non-research mode: use base instruction
                    followup_instruction = base_followup_instruction

                messages.append(
                    {
                        "role": "assistant",
                        "content": f"I'll execute the {tool_calls_from_output[0]['name']} tool to help with this task.",
                    }
                )
                messages.append(
                    {
                        "role": "user",
                        "content": f"Tool execution result:\n{tool_results}\n\n{followup_instruction}",
                    }
                )

                if consecutive_tool_failures >= max_consecutive_tool_failures:
                    stop_reason = "consecutive_tool_failures"
                    logger.info(
                        f"Stopping tool loop due to consecutive failures: {consecutive_tool_failures}"
                    )
                    break

                if (
                    loop_iterations >= max_tool_iterations
                    or len(tool_execution_records) >= max_total_tool_calls
                    or total_tool_output_chars >= max_total_tool_output_chars
                ):
                    stop_reason = "budget"
                    logger.info(
                        f"Stopping tool loop due to budget: iterations={loop_iterations}, tool_calls={len(tool_execution_records)}, chars={total_tool_output_chars}"
                    )
                    break

                continue

            # P0 Fix: DR forced fetch with ratio-based enforcement (Codex approved)
            # Calculate fetch ratio and determine if more fetches needed
            unique_fetch_count = len(fetched_urls)

            # Guard: Only enforce ratio when search_count >= 3 (Codex recommendation)
            # Guard: Division by zero protection
            needs_more_fetch = (
                search_count == 0 or  # Early guard per Codex
                not fetch_success or
                (search_count >= 3 and unique_fetch_count / search_count < min_fetch_ratio)
            )

            if (
                research_mode
                and search_urls
                and needs_more_fetch
                and fetch_tools.intersection(set(effective_allowed_tools or []))
            ):
                try:
                    # Dedupe and exclude already-fetched and failed URLs
                    deduped_urls = []
                    seen = set()
                    for u in search_urls:
                        if u and u not in seen and u not in fetched_urls and u not in failed_fetch_urls:
                            seen.add(u)
                            deduped_urls.append(u)

                    # Short-circuit: if all unfetched URLs have failed, exit early (Codex suggestion)
                    if not deduped_urls:
                        logger.info(f"DR policy: no eligible URLs to fetch (all fetched or failed)")
                    else:
                        urls_to_fetch = deduped_urls[:max_urls_to_fetch]

                    if deduped_urls and urls_to_fetch:
                        did_forced_fetch = True
                        logger.info(f"DR policy: auto-fetching URLs={len(urls_to_fetch)} after search")
                        policy_calls = [{"name": "web_fetch", "arguments": {"urls": urls_to_fetch}}]
                        policy_results, policy_execs, policy_raw = await _execute_and_format_tools(
                            policy_calls,
                            effective_allowed_tools,
                            query.query,
                            request,
                            query.context,
                        )
                        last_tool_results = policy_results or last_tool_results
                        tool_execution_records.extend(policy_execs)
                        raw_tool_results.extend(policy_raw)
                        if policy_results:
                            messages.append(
                                {
                                    "role": "assistant",
                                    "content": "Executing web_fetch on search results for evidence.",
                                }
                            )
                            messages.append(
                                {
                                    "role": "user",
                                    "content": f"Tool execution result:\n{policy_results}\n\n{followup_instruction}",
                                }
                            )
                        # P0 Fix: Update URL tracking for forced fetch results
                        for rr in policy_raw:
                            if rr.get("tool") in fetch_tools:
                                if rr.get("success"):
                                    fetch_success = True
                                    fetched_urls.update(urls_to_fetch)
                                else:
                                    failed_fetch_urls.update(urls_to_fetch)
                except Exception as e:
                    logger.warning(f"DR forced fetch failed: {e}")
                    failed_fetch_urls.update(urls_to_fetch if 'urls_to_fetch' in dir() else [])

            # P2 Telemetry: Log fetch ratio for observability (Codex requirement)
            import json as _json_telemetry
            final_fetch_count = len(fetched_urls)
            fetch_ratio = final_fetch_count / search_count if search_count > 0 else 0.0
            logger.info(_json_telemetry.dumps({
                "event": "fetch_ratio",
                "agent_id": query.agent_id,
                "search_count": search_count,
                "fetch_count": final_fetch_count,
                "failed_fetch_count": len(failed_fetch_urls),
                "ratio": round(fetch_ratio, 2),
                "min_required": min_fetch_ratio,
                "strategy": research_strategy,
                "ratio_met": fetch_ratio >= min_fetch_ratio if search_count >= 3 else True,
            }, ensure_ascii=False))

            # Final interpretation pass if we executed any tools or budgets were hit
            # P0 Fix: Always run interpretation pass if any tool executed successfully
            # This ensures tool results are summarized into Response, not just "I'll execute..."
            # NOTE: domain_discovery now uses interpretation_pass to generate final JSON
            # (previously skipped, causing "I'll execute..." output instead of JSON)
            skip_interpretation_roles: set[str] = set()  # No roles skip interpretation
            role = query.context.get("role") if isinstance(query.context, dict) else None
            skip_interpretation = role in skip_interpretation_roles

            has_successful_tool = any(r.get("success") for r in tool_execution_records)
            if not skip_interpretation and tool_execution_records and (
                has_successful_tool  # Any successful tool execution requires summarization
                or stop_reason != "no_tool_call"
                or did_forced_fetch
                or not (response_text and str(response_text).strip())
            ):
                # P0 Fix v4: Build CLEAN interpretation messages without history noise
                # Root cause: Reusing `messages` brings "I'll execute..." patterns that
                # contaminate LLM output. Instead, build fresh messages with only:
                # - System prompt (defines role)
                # - Original query (the task)
                # - Aggregated tool results (what was retrieved)
                # - Strong interpretation instruction (role-specific prompt)
                tool_results_summary = aggregate_tool_results(tool_execution_records)

                # Select interpretation prompt from role preset or use default
                # Roles can define custom interpretation_prompt in their preset
                use_source_format = should_use_source_format(role)
                interp_prompt = (
                    INTERPRETATION_PROMPT_SOURCES
                    if use_source_format
                    else INTERPRETATION_PROMPT_GENERAL
                )
                role_preset_for_interp = None
                interp_system_prompt = None
                if role:
                    try:
                        from ..roles.presets import get_role_preset
                        role_preset_for_interp = get_role_preset(role)
                        custom_interp = role_preset_for_interp.get("interpretation_prompt")
                        if custom_interp:
                            interp_prompt = custom_interp
                        # Use interpretation-specific system prompt if available.
                        # This strips tool-loop instructions (OODA, tool patterns) that
                        # can contaminate interpretation output with planning instead of synthesis.
                        custom_sys = role_preset_for_interp.get("interpretation_system_prompt")
                        if custom_sys:
                            interp_system_prompt = custom_sys
                    except Exception:
                        pass

                # Add iteration context to help LLM know it's time to synthesize
                iteration_context = ""
                if research_mode and loop_iterations >= max_tool_iterations:
                    iteration_context = (
                        f"\n\n**IMPORTANT**: You have completed {loop_iterations} research iterations "
                        f"(maximum allowed). This is your FINAL OUTPUT. "
                        f"Generate your comprehensive synthesis now, starting with '## Key Findings'."
                    )

                interpretation_messages = build_interpretation_messages(
                    system_prompt=system_prompt,
                    original_query=query.query,
                    tool_results_summary=tool_results_summary,
                    interpretation_prompt=interp_prompt + iteration_context,
                    interpretation_system_prompt=interp_system_prompt,
                )
                interpretation_result = await request.app.state.providers.generate_completion(
                    messages=interpretation_messages,
                    tier=tier,
                    specific_model=model_override,
                    provider_override=provider_override,
                    max_tokens=max_tokens,
                    temperature=temperature,
                    response_format=response_format,
                    tools=None,  # No tools for interpretation pass
                    workflow_id=request.headers.get("X-Workflow-ID")
                    or request.headers.get("x-workflow-id"),
                    agent_id=query.agent_id,
                )
                raw_interpretation = interpretation_result.get("output_text", "")

                # P0 Fix v4: Validate interpretation output and fallback to digest if invalid
                # Never fallback to raw JSON - use generate_tool_digest() for human-readable fallback
                # Roles can set skip_output_validation=True in preset (e.g., domain_discovery for short JSON)
                skip_validation = False
                if role:
                    try:
                        skip_validation = role_preset_for_interp.get("skip_output_validation", False) if role_preset_for_interp else False
                    except Exception:
                        pass

                if skip_validation:
                    is_valid, validation_reason = True, f"{role}_preset_skip"
                else:
                    is_valid, validation_reason = validate_interpretation_output(
                        raw_interpretation or "",
                        total_tool_output_chars,
                        expect_sources_format=use_source_format,
                    )

                if is_valid:
                    response_text = raw_interpretation
                    logger.info(f"[interpretation_pass] VALID for agent={query.agent_id}, response_len={len(str(response_text))}, preview={str(response_text)[:200]}")
                    # Log compression metrics for analysis
                    if total_tool_output_chars > 0:
                        compression_ratio = 1 - (len(response_text) / total_tool_output_chars)
                        research_strategy = query.context.get("research_strategy", "unknown") if isinstance(query.context, dict) else "unknown"
                        logger.info(
                            f"[interpretation_metrics] agent={query.agent_id}, "
                            f"tool_chars={total_tool_output_chars}, output_chars={len(response_text)}, "
                            f"compression_ratio={compression_ratio:.2f}, strategy={research_strategy}"
                        )
                else:
                    # Fallback to human-readable digest (NOT raw JSON)
                    fallback_digest = generate_tool_digest(
                        last_tool_results or "",
                        tool_execution_records,
                        max_chars=30000  # Increased from default 3000 to avoid truncation
                    )
                    response_text = fallback_digest if fallback_digest else raw_interpretation
                    logger.warning(
                        f"[interpretation_pass] INVALID for agent={query.agent_id}, "
                        f"reason={validation_reason}, raw_len={len(raw_interpretation or '')}, "
                        f"fallback_len={len(response_text)}, preview={str(response_text)[:200]}"
                    )
                i_usage = interpretation_result.get("usage", {}) or {}
                try:
                    total_tokens += int(i_usage.get("total_tokens") or 0)
                    total_input_tokens += int(i_usage.get("input_tokens") or 0)
                    total_output_tokens += int(i_usage.get("output_tokens") or 0)
                    total_cost_usd += float(i_usage.get("cost_usd") or 0.0)
                except Exception:
                    pass
                result_data = interpretation_result
            else:
                result_data = last_result_data or {}
                skip_reason = f"role={role}" if skip_interpretation else f"has_successful_tool={has_successful_tool}, stop_reason={stop_reason}"
                logger.info(f"[interpretation_pass] SKIPPED for agent={query.agent_id}, {skip_reason}, response_preview={str(response_text)[:100]}")

            # Stub Guard: Clean any pseudo tool-call stubs from final output
            # These can appear when LLM outputs XML/JSON tool calls instead of native function calling
            stub_patterns = [
                r"<function_calls>",
                r"<invoke\s",
                r"</invoke>",
                r"<web_fetch[>\s]",
                r"<web_search[>\s]",
                r"<web_crawl[>\s]",
                r'"tool"\s*:\s*"web_',
                r'"name"\s*:\s*"web_',
            ]
            import re as _re
            stub_detected = any(_re.search(p, str(response_text), _re.IGNORECASE) for p in stub_patterns)

            if stub_detected:
                logger.warning("Stub Guard: detected pseudo tool-call stub in response, running interpretation pass")
                try:
                    # Run interpretation pass to get clean final answer
                    stub_cleanup_result = await request.app.state.providers.generate_completion(
                        messages=messages + [
                            {"role": "assistant", "content": str(response_text)},
                            {"role": "user", "content": (
                                "Your previous response contained tool call markup (XML tags or JSON) that should not appear in the final output. "
                                "Please provide your final answer in clean text without any <function_calls>, <invoke>, <web_fetch>, or similar markup. "
                                "Summarize any tool results you mentioned and provide a direct answer."
                            )}
                        ],
                        tier=tier,
                        specific_model=model_override,
                        provider_override=provider_override,
                        max_tokens=max_tokens,
                        temperature=temperature,
                        response_format=response_format,
                        tools=None,  # No tools for cleanup pass
                        workflow_id=request.headers.get("X-Workflow-ID")
                        or request.headers.get("x-workflow-id"),
                        agent_id=query.agent_id,
                    )
                    cleaned_text = stub_cleanup_result.get("output_text", "")
                    if cleaned_text and not any(_re.search(p, cleaned_text, _re.IGNORECASE) for p in stub_patterns):
                        response_text = cleaned_text
                        logger.info("Stub Guard: successfully cleaned response via interpretation pass")
                        # Update token counts
                        cleanup_usage = stub_cleanup_result.get("usage", {}) or {}
                        try:
                            total_tokens += int(cleanup_usage.get("total_tokens") or 0)
                            total_input_tokens += int(cleanup_usage.get("input_tokens") or 0)
                            total_output_tokens += int(cleanup_usage.get("output_tokens") or 0)
                            total_cost_usd += float(cleanup_usage.get("cost_usd") or 0.0)
                        except Exception:
                            pass
                    else:
                        # Fallback: strip stub patterns via regex
                        logger.warning("Stub Guard: interpretation pass still contains stubs, falling back to regex strip")
                        response_text = _re.sub(r"<function_calls>[\s\S]*?</function_calls>", "", str(response_text))
                        response_text = _re.sub(r"<invoke[\s\S]*?</invoke>", "", response_text)
                        response_text = _re.sub(r"<web_fetch[\s\S]*?>[\s\S]*?(?:</web_fetch>)?", "", response_text)
                        response_text = _re.sub(r"<web_search[\s\S]*?>[\s\S]*?(?:</web_search>)?", "", response_text)
                        response_text = response_text.strip()
                except Exception as e:
                    logger.error(f"Stub Guard cleanup failed: {e}, falling back to regex strip")
                    response_text = _re.sub(r"<function_calls>[\s\S]*?</function_calls>", "", str(response_text))
                    response_text = _re.sub(r"<invoke[\s\S]*?</invoke>", "", response_text)
                    response_text = response_text.strip()

            logger.info(f"[final_response] agent={query.agent_id}, len={len(str(response_text))}, preview={str(response_text)[:200]}")
            result = {
                "response": response_text,
                "tokens_used": total_tokens,
                "model_used": (result_data or {}).get("model", "unknown"),
            }

            tools_used = sorted(
                {rec.get("tool") for rec in tool_execution_records if rec.get("tool")}
            )

            return AgentResponse(
                success=True,
                response=strip_markdown_json_wrapper(
                    result["response"], expect_json=expects_json_response
                ),
                tokens_used=result["tokens_used"],
                model_used=result["model_used"],
                provider=(result_data or {}).get("provider") or "unknown",
                finish_reason=(result_data or {}).get("finish_reason", "stop"),
                metadata={
                    "agent_id": query.agent_id,
                    "mode": query.mode,
                    "allowed_tools": effective_allowed_tools,
                    "role": effective_role,
                    "requested_role": requested_role,
                    "system_prompt_source": system_prompt_source,
                    "provider": (result_data or {}).get("provider") or "unknown",
                    "finish_reason": (result_data or {}).get("finish_reason", "stop"),
                    "requested_max_tokens": max_tokens,
                    "input_tokens": total_input_tokens,
                    "output_tokens": total_output_tokens,
                    "cost_usd": total_cost_usd,
                    "effective_max_completion": (result_data or {}).get("effective_max_completion"),
                    "tools_used": tools_used,
                    "tool_executions": tool_execution_records,
                },
            )
        else:
            # Use mock provider for testing
            logger.info("Using mock provider (no API keys configured)")
            requested_role = None
            effective_role = "generalist"
            system_prompt_source = "mock_provider"
            if isinstance(query.context, dict):
                requested_role = query.context.get("role") or query.context.get("agent_type")
                if requested_role:
                    effective_role = str(requested_role).strip()
                elif query.context.get("force_research") or query.context.get("workflow_type") == "research":
                    effective_role = "deep_research_agent"
            result = await mock_provider.generate(
                query.query,
                context=query.context,
                max_tokens=query.max_tokens,
                temperature=query.temperature,
            )

            return AgentResponse(
                success=True,
                response=strip_markdown_json_wrapper(
                    result["response"],
                    expect_json=_response_format_expects_json(
                        query.context.get("response_format")
                        if isinstance(query.context, dict)
                        else None
                    ),
                ),
                tokens_used=result["tokens_used"],
                model_used=result["model_used"],
                provider="mock",
                finish_reason="stop",
                metadata={
                    "agent_id": query.agent_id,
                    "mode": query.mode,
                    "allowed_tools": effective_allowed_tools,
                    "role": effective_role,
                    "requested_role": requested_role,
                    "system_prompt_source": system_prompt_source,
                    "finish_reason": "stop",
                },
            )

    except Exception as e:
        import traceback
        logger.error(f"Error processing agent query: {e}\n{traceback.format_exc()}")
        raise HTTPException(status_code=500, detail=str(e))


async def _execute_and_format_tools(
    tool_calls: List[Dict[str, Any]],
    allowed_tools: List[str],
    query: str = "",
    request=None,
    context: Optional[Dict[str, Any]] = None,
) -> Tuple[str, List[Dict[str, Any]], List[Dict[str, Any]]]:
    """Execute tool calls and format results into natural language."""
    if not tool_calls:
        return "", [], []

    from ..tools import get_registry

    registry = get_registry()

    formatted_results = []
    tool_execution_records: List[Dict[str, Any]] = []
    raw_tool_results: List[Dict[str, Any]] = []

    # Set up event emitter and workflow/agent IDs for tool events
    emitter = None
    try:
        providers = getattr(request.app.state, "providers", None) if request else None
        emitter = getattr(providers, "_emitter", None) if providers else None
    except Exception:
        emitter = None

    wf_id = None
    agent_id = None
    if request:
        wf_id = (
            request.headers.get("X-Parent-Workflow-ID")
            or request.headers.get("X-Workflow-ID")
            or request.headers.get("x-workflow-id")
        )
        agent_id = request.headers.get("X-Agent-ID") or request.headers.get(
            "x-agent-id"
        )
    # Fallback to context when headers are missing (e.g., forced tool execution)
    if not wf_id and isinstance(context, dict):
        wf_id = context.get("workflow_id") or context.get("parent_workflow_id")
    if not agent_id and isinstance(context, dict):
        agent_id = context.get("agent_id")

    def _audit(event: str, tool_name: str, *, success: Optional[bool] = None, error: Any = None, duration_ms: Any = None) -> None:
        payload = {
            "workflow_id": wf_id,
            "agent_id": agent_id,
            "tool": tool_name,
            "event": event,
        }
        if success is not None:
            payload["success"] = success
        if error is not None:
            payload["error"] = str(error)
        if duration_ms is not None:
            payload["duration_ms"] = duration_ms
        try:
            logger.info(f"[tool_audit] {payload}")
        except Exception:
            pass

    def _sanitize_payload(
        value: Any,
        *,
        max_str: int = 1000,
        max_items: int = 50,
        depth: int = 0,
        max_depth: int = 4,
        redact_keys: Optional[List[str]] = None,
    ) -> Any:
        """Recursively sanitize payloads to avoid secret leaks and stream floods."""
        if redact_keys is None:
            redact_keys = ["api_key", "token", "secret", "password", "credential", "auth"]

        if depth > max_depth:
            return "[TRUNCATED]"

        # Strings: truncate
        if isinstance(value, str):
            return value if len(value) <= max_str else value[:max_str] + "..."

        # Dicts: redact secret-looking keys, limit size, recurse
        if isinstance(value, dict):
            out = {}
            for idx, (k, v) in enumerate(value.items()):
                if idx >= max_items:
                    out["..."] = "[TRUNCATED]"
                    break
                if isinstance(k, str) and any(sk in k.lower() for sk in redact_keys):
                    out[k] = "[REDACTED]"
                    continue
                out[k] = _sanitize_payload(
                    v,
                    max_str=max_str,
                    max_items=max_items,
                    depth=depth + 1,
                    max_depth=max_depth,
                    redact_keys=redact_keys,
                )
            return out

        # Lists/Tuples: cap length, recurse
        if isinstance(value, (list, tuple)):
            out_list = []
            for idx, item in enumerate(value):
                if idx >= max_items:
                    out_list.append("[TRUNCATED]")
                    break
                out_list.append(
                    _sanitize_payload(
                        item,
                        max_str=max_str,
                        max_items=max_items,
                        depth=depth + 1,
                        max_depth=max_depth,
                        redact_keys=redact_keys,
                    )
                )
            return out_list

        # Other primitives: return as-is
        return value

    for call in tool_calls:
        tool_name = call.get("name")
        if tool_name not in allowed_tools:
            continue

        tool = registry.get_tool(tool_name)
        if not tool:
            continue

        try:
            # Execute the tool
            args = call.get("arguments", {})
            if isinstance(args, str):
                import json

                args = json.loads(args)

            # Special handling for code_executor - translate common mistakes
            if tool_name == "code_executor":
                # Check if LLM mistakenly passed source code instead of WASM
                if "language" in args or "code" in args:
                    # LLM is trying to execute source code, not WASM
                    lang = args.get("language", "unknown")
                    formatted_results.append(
                        f"Error: The code_executor tool only executes compiled WASM bytecode, not {lang} source code. "
                        f"To execute {lang} code, it must first be compiled to WebAssembly (.wasm format). "
                        f"For Python, use py2wasm. For C/C++, use emscripten. For Rust, use wasm-pack."
                    )
                    continue

                # Check if we have valid WASM parameters
                if not args.get("wasm_base64") and not args.get("wasm_path"):
                    formatted_results.append(
                        "Error: code_executor requires either 'wasm_base64' (base64-encoded WASM) or 'wasm_path' (path to .wasm file)"
                    )
                    continue

            # Drop convenience/unknown parameters to avoid validation failures
            if isinstance(args, dict):
                allowed = {p.name for p in tool.parameters}
                if "tool" in args and args.get("tool") == tool_name:
                    args = {k: v for k, v in args.items() if k != "tool"}
                unknown_keys = set(args.keys()) - allowed
                if unknown_keys:
                    logger.warning(
                        f"Dropping unknown parameters for {tool_name}: {sorted(unknown_keys)}"
                    )
                    args = {k: v for k, v in args.items() if k in allowed}

            # Merge tool_parameters from context (orchestrator hints) with LLM-provided args
            # This ensures critical parameters like target_paths are always included
            if isinstance(context, dict) and isinstance(args, dict):
                ctx_tool_params = context.get("tool_parameters", {})
                if ctx_tool_params and ctx_tool_params.get("tool") == tool_name:
                    # For array parameters like target_paths, merge instead of replace
                    for key in ["target_paths", "target_keywords"]:
                        if key in ctx_tool_params and key in allowed:
                            ctx_value = ctx_tool_params.get(key)
                            llm_value = args.get(key)
                            if ctx_value:
                                if key == "target_paths" and isinstance(ctx_value, list):
                                    # Merge: LLM paths first, then add orchestrator paths not already present
                                    if isinstance(llm_value, list):
                                        merged = list(llm_value)
                                        for p in ctx_value:
                                            if p not in merged:
                                                merged.append(p)
                                        args[key] = merged
                                    else:
                                        args[key] = list(ctx_value)
                                    logger.info(f"Merged {key} from orchestrator: {args[key]}")
                                elif key == "target_keywords" and isinstance(ctx_value, str) and not llm_value:
                                    args[key] = ctx_value
                    # Handle query parameter: use orchestrator's query if LLM didn't provide one
                    # This ensures domain_discovery and similar agents use the intended search query
                    if "query" in ctx_tool_params and "query" in allowed:
                        ctx_query = ctx_tool_params.get("query")
                        llm_query = args.get("query")
                        if ctx_query and not llm_query:
                            args["query"] = ctx_query
                            logger.info(f"Using orchestrator query for {tool_name}: {ctx_query}")

            # Emit TOOL_INVOKED event
            if emitter and wf_id:
                try:
                    sanitized_params = _sanitize_payload(args)
                    emitter.emit(
                        wf_id,
                        "TOOL_INVOKED",
                        agent_id=agent_id,
                        message=f"Executing {tool_name}",
                        payload={"tool": tool_name, "params": sanitized_params},
                    )
                except Exception:
                    pass

            # Define observer for intermediate updates
            def tool_observer(event_name: str, payload: Any):
                if emitter and wf_id:
                    try:
                        # Ensure payload is a dict
                        if not isinstance(payload, dict):
                            payload = {"data": payload}

                        # Add tool name and phase to payload
                        payload["tool"] = tool_name
                        payload["intermediate"] = True
                        payload["event"] = event_name

                        # Sanitize payload (redact secrets, truncate strings, cap collections)
                        payload = _sanitize_payload(payload, max_str=2000, max_items=50)

                        # Truncate message for stream safety
                        msg = str(payload.get("message", ""))
                        if len(msg) > 1000:
                            msg = msg[:1000] + "..."
                            payload["message"] = msg

                        emitter.emit(
                            wf_id,
                            "TOOL_OBSERVATION",
                            agent_id=agent_id,
                            message=msg,
                            payload=payload,
                        )
                    except Exception:
                        pass

            # Sanitize session context before passing to tools
            if isinstance(context, dict):
                safe_keys = {
                    "session_id",
                    "user_id",
                    "agent_id",  # For tool rate limiting fallback key
                    "prompt_params",
                    "official_domains",
                    # Controls for auto-fetch in web_search
                    "auto_fetch_k",
                    "auto_fetch_subpages",
                    "auto_fetch_max_length",
                    "auto_fetch_official_subpages",
                    # Lightweight research flag for tool-level gating
                    "research_mode",
                    # GA4 OAuth credentials (per-request auth from frontend)
                    "ga4_access_token",
                    "ga4_property_id",
                    # Orchestrator-provided tool parameters for merging
                    "tool_parameters",
                    # Trading agents: upstream node results from TemplateWorkflow
                    "template_results",
                    "dependency_results",
                    # Python executor mode configuration
                    "python_executor_mode",  # "wasi" or "firecracker"
                    "python_executor",       # Per-request config overrides
                }
                sanitized_context = {k: v for k, v in context.items() if k in safe_keys}
                for key in ("template_results", "dependency_results"):
                    if key in sanitized_context and sanitized_context[key] is not None:
                        val = sanitized_context[key]
                        if isinstance(val, dict):
                            trimmed: Dict[str, Any] = {}
                            for idx, (k, v) in enumerate(val.items()):
                                if idx >= 20:
                                    trimmed["..."] = "[TRUNCATED]"
                                    break
                                text = v if isinstance(v, str) else str(v)
                                trimmed[str(k)] = text if len(text) <= 4000 else text[:4000] + "..."
                            sanitized_context[key] = trimmed
                        else:
                            text = val if isinstance(val, str) else str(val)
                            sanitized_context[key] = text if len(text) <= 4000 else text[:4000] + "..."
            else:
                sanitized_context = None

            logger.info(
                f"Executing tool {tool_name} with context keys: {list(sanitized_context.keys()) if isinstance(sanitized_context, dict) else 'None'}, args: {args}"
            )

            # Execute with observer
            start_time = __import__("time").time()
            result = await tool.execute(
                session_context=sanitized_context, observer=tool_observer, **args
            )
            duration_ms = int((__import__("time").time() - start_time) * 1000)
            _audit(
                "tool_end",
                tool_name,
                success=bool(result and result.success),
                error=(result.error if result else None),
                duration_ms=duration_ms,
            )

            if result.success:
                # Format based on tool type
                if tool_name == "web_search":
                    # Format web search results with full content for AI consumption
                    if isinstance(result.output, list) and result.output:
                        # Filter results by relevance to the query
                        query = args.get("query", "")
                        filtered_results = (
                            filter_relevant_results(query, result.output)
                            if query
                            else result.output[:5]
                        )

                        # Include full content for AI to synthesize
                        search_results = []
                        for i, item in enumerate(filtered_results, 1):
                            title = item.get("title", "")
                            snippet = item.get("snippet", "")
                            url = item.get("url", "")
                            date = item.get("published_date", "")
                            # Prefer markdown when available (e.g., Firecrawl), else use content, else snippet
                            markdown = (
                                item.get("markdown")
                                if isinstance(item.get("markdown"), str)
                                else None
                            )
                            raw_content = (
                                markdown if markdown else item.get("content", "")
                            )

                            # Clean HTML entities for title and text
                            title = html.unescape(title)
                            content_or_snippet = raw_content if raw_content else snippet
                            content_or_snippet = (
                                html.unescape(content_or_snippet)
                                if content_or_snippet
                                else ""
                            )

                            if title and url:
                                # Use up to 1500 chars to give LLM enough context
                                text_content = (
                                    content_or_snippet[:1500]
                                    if content_or_snippet
                                    else ""
                                )

                                result_text = f"**{title}**"
                                if date:
                                    result_text += f" ({date[:10]})"
                                if text_content:
                                    result_text += f"\n{text_content}"
                                    if (
                                        len(content_or_snippet) > 1500
                                        or len(snippet) > 500
                                    ):
                                        result_text += "..."
                                result_text += f"\nSource: {url}\n"

                                search_results.append(result_text)

                        if search_results:
                            # Return formatted results with content for the orchestrator to synthesize
                            # The orchestrator's synthesis activity will handle creating the final answer
                            formatted = "Web Search Results:\n\n" + "\n---\n\n".join(
                                search_results
                            )
                            formatted_results.append(formatted)
                        else:
                            formatted_results.append(
                                "No relevant search results found."
                            )
                    elif result.output:
                        formatted_results.append(f"Search results: {result.output}")
                elif tool_name == "calculator":
                    formatted_results.append(f"Calculation result: {result.output}")
                else:
                    # Generic formatting for other tools
                    import json as _json_fmt

                    if isinstance(result.output, dict):
                        formatted_output = _json_fmt.dumps(
                            result.output, indent=2, ensure_ascii=False
                        )
                    else:
                        formatted_output = str(result.output)
                    formatted = f"{tool_name} result:\n{formatted_output}"

                    # Include concise metadata only when it affects epistemic confidence.
                    if isinstance(result.metadata, dict) and result.metadata:
                        failed_count = None
                        try:
                            failure_summary = result.metadata.get("failure_summary") or {}
                            failed_count = int(failure_summary.get("failed_count"))
                        except Exception:
                            failed_count = None

                        include_meta = (
                            (not result.success)
                            or (result.metadata.get("partial_success") is True)
                            or (failed_count is not None and failed_count > 0)
                            or (
                                isinstance(result.metadata.get("attempts"), list)
                                and len(result.metadata.get("attempts") or []) > 1
                            )
                        )
                        if include_meta:
                            meta_keys = [
                                "provider",
                                "strategy",
                                "fetch_method",
                                "provider_used",
                                "attempts",
                                "partial_success",
                                "failure_summary",
                                "urls_attempted",
                                "urls_succeeded",
                                "urls_failed",
                            ]
                            compact_meta = {
                                k: result.metadata.get(k)
                                for k in meta_keys
                                if k in result.metadata
                            }
                            formatted_meta = _json_fmt.dumps(
                                _sanitize_payload(compact_meta, max_str=1000, max_items=20),
                                indent=2,
                                ensure_ascii=False,
                            )
                            formatted += f"\n\n{tool_name} metadata:\n{formatted_meta}"

                    formatted_results.append(formatted)
            else:
                formatted_results.append(f"Error executing {tool_name}: {result.error}")

            sanitized_result_metadata = (
                _sanitize_payload(result.metadata, max_str=2000, max_items=20)
                if (result and isinstance(result.metadata, dict) and result.metadata)
                else {}
            )

            # Emit TOOL_OBSERVATION event (success or failure)
            if emitter and wf_id:
                try:
                    msg = (
                        str(result.output)
                        if result and result.success
                        else (result.error or "")
                    )
                    emitter.emit(
                        wf_id,
                        "TOOL_OBSERVATION",
                        agent_id=agent_id,
                        message=(msg[:2000] if msg else ""),
                        payload={
                            "tool": tool_name,
                            "success": bool(result and result.success),
                            "metadata": sanitized_result_metadata,
                            "usage": {
                                "duration_ms": duration_ms,
                                "tokens": result.tokens_used if result and result.tokens_used else 0,
                            },
                        },
                    )
                except Exception:
                    pass

            # Record execution for upstream observability/persistence
            # Use higher max_str for content-rich tools (web_fetch, web_subpage_fetch)
            # to preserve multi-page content for citation extraction and storage
            content_rich_tools = {"web_fetch", "web_subpage_fetch", "web_crawl"}
            output_max_str = 100000 if tool_name in content_rich_tools else 2000
            tool_execution_records.append(
                {
                    "tool": tool_name,
                    "success": bool(result and result.success),
                    "output": _sanitize_payload(
                        result.output if result else None, max_str=output_max_str, max_items=20
                    ),
                    "error": result.error if result else None,
                    "metadata": sanitized_result_metadata,
                    "duration_ms": duration_ms,
                    "tokens_used": result.tokens_used if result else None,
                    "tool_input": _sanitize_payload(args, max_str=2000, max_items=20),
                }
            )

            raw_tool_results.append(
                {
                    "tool": tool_name,
                    "success": bool(result and result.success),
                    "output": result.output if result else None,
                    "error": result.error if result else None,
                    "metadata": result.metadata if result else {},
                }
            )

        except Exception as e:
            logger.error(f"Error executing tool {tool_name}: {e}")
            formatted_results.append(f"Failed to execute {tool_name}")
            
            # Emit failure TOOL_OBSERVATION
            if emitter and wf_id:
                try:
                    emitter.emit(
                        wf_id,
                        "TOOL_OBSERVATION",
                        agent_id=agent_id,
                        message=f"Tool execution failed: {str(e)}",
                        payload={
                            "tool": tool_name,
                            "success": False,
                            "error": str(e),
                        },
                    )
                except Exception:
                    pass

            tool_execution_records.append(
                {
                    "tool": tool_name,
                    "success": False,
                    "output": None,
                    "error": str(e),
                    "metadata": {},
                    "duration_ms": None,
                    "tokens_used": None,
                }
            )

            raw_tool_results.append(
                {
                    "tool": tool_name,
                    "success": False,
                    "output": None,
                    "error": str(e),
                    "metadata": {},
                }
            )

    return (
        "\n\n".join(formatted_results) if formatted_results else "",
        tool_execution_records,
        raw_tool_results,
    )


def _extract_urls_from_search_output(output: Any) -> List[str]:
    urls: List[str] = []
    try:
        candidates: Any = []
        if isinstance(output, list):
            candidates = output
        elif isinstance(output, dict):
            candidates = (
                output.get("results")
                or output.get("data")
                or output.get("items")
                or []
            )
        if isinstance(candidates, list):
            for item in candidates:
                if isinstance(item, dict):
                    url = item.get("url") or item.get("link")
                    if url and isinstance(url, str):
                        url = url.strip()
                        if url.startswith(("http://", "https://")):
                            urls.append(url)
    except Exception:
        return []
    deduped: List[str] = []
    seen = set()
    for u in urls:
        if u and u not in seen:
            seen.add(u)
            deduped.append(u)
    return deduped


class OutputFormatSpec(BaseModel):
    """Deep Research 2.0: Expected output structure for a subtask."""
    type: str = Field(default="narrative", description="'structured', 'narrative', or 'list'")
    required_fields: List[str] = Field(default_factory=list, description="Fields that must be present")
    optional_fields: List[str] = Field(default_factory=list, description="Nice-to-have fields")


class SourceGuidanceSpec(BaseModel):
    """Deep Research 2.0: Source type recommendations for a subtask."""
    required: List[str] = Field(default_factory=list, description="Must use these source types")
    optional: List[str] = Field(default_factory=list, description="May use these source types")
    avoid: List[str] = Field(default_factory=list, description="Should not use these source types")


class SearchBudgetSpec(BaseModel):
    """Deep Research 2.0: Search limits for a subtask."""
    max_queries: int = Field(default=10, description="Maximum web_search calls")
    max_fetches: int = Field(default=20, description="Maximum web_fetch calls")


class BoundariesSpec(BaseModel):
    """Deep Research 2.0: Scope boundaries for a subtask."""
    in_scope: List[str] = Field(default_factory=list, description="Topics explicitly within scope")
    out_of_scope: List[str] = Field(default_factory=list, description="Topics to avoid")


class Subtask(BaseModel):
    id: str
    description: str
    dependencies: List[str] = []
    estimated_tokens: int = 0
    task_type: str = Field(
        default="", description="Optional structured subtask type, e.g., 'synthesis'"
    )
    # Optional grouping for research-area-driven decomposition
    parent_area: Optional[str] = Field(
        default="", description="Top-level research area that this subtask belongs to"
    )
    # LLM-native tool selection
    suggested_tools: List[str] = Field(
        default_factory=list, description="Tools suggested by LLM for this subtask"
    )
    tool_parameters: Dict[str, Any] = Field(
        default_factory=dict, description="Pre-structured parameters for tool execution"
    )
    # Deep Research 2.0: Task Contract fields
    output_format: Optional[OutputFormatSpec] = Field(
        default=None, description="Expected output structure"
    )
    source_guidance: Optional[SourceGuidanceSpec] = Field(
        default=None, description="Source type recommendations"
    )
    search_budget: Optional[SearchBudgetSpec] = Field(
        default=None, description="Search limits"
    )
    boundaries: Optional[BoundariesSpec] = Field(
        default=None, description="Scope boundaries"
    )


class DecompositionResponse(BaseModel):
    mode: str
    complexity_score: float
    subtasks: List[Subtask]
    total_estimated_tokens: int
    # Extended planning schema (plan_schema_v2)
    execution_strategy: str = Field(
        default="parallel", description="parallel|sequential|hybrid"
    )
    agent_types: List[str] = Field(default_factory=list)
    concurrency_limit: int = Field(default=5)
    token_estimates: Dict[str, int] = Field(default_factory=dict)
    # Cognitive routing fields for intelligent strategy selection
    cognitive_strategy: str = Field(
        default="decompose", description="direct|decompose|exploratory|react|research"
    )
    confidence: float = Field(
        default=0.8, ge=0.0, le=1.0, description="Confidence in strategy selection"
    )
    fallback_strategy: str = Field(
        default="decompose", description="Fallback if primary strategy fails"
    )
    # Usage and provider/model metadata (optional; used for accurate cost tracking)
    input_tokens: int = 0
    output_tokens: int = 0
    total_tokens: int = 0
    cost_usd: float = 0.0
    model_used: str = ""
    provider: str = ""


class ResearchPlanRequest(BaseModel):
    """Request for generating a human-friendly research plan."""
    query: str = Field(..., description="The research query")
    context: Optional[Dict[str, Any]] = Field(default_factory=dict, description="Task context")
    conversation: Optional[List[Dict[str, Any]]] = Field(
        default_factory=list,
        description="Previous conversation rounds for multi-turn refinement"
    )
    is_final_round: bool = Field(
        default=False,
        description="Whether this is the final allowed round (LLM must finalize the plan)"
    )


class ResearchPlanResponse(BaseModel):
    """Response with the generated research plan."""
    message: str = Field(..., description="Human-friendly research plan text")
    intent: str = Field(default="feedback", description="LLM intent: 'feedback' (asking Qs), 'ready' (plan proposed), or 'execute' (user wants to proceed)")
    round: int = Field(default=1, description="Conversation round number")
    model: str = Field(default="", description="Model used for generation")
    provider: str = Field(default="", description="Provider used")
    input_tokens: int = Field(default=0, description="Input tokens consumed")
    output_tokens: int = Field(default=0, description="Output tokens consumed")


@router.post("/agent/research-plan", response_model=ResearchPlanResponse)
async def generate_research_plan(
    request: Request, body: ResearchPlanRequest
) -> ResearchPlanResponse:
    """Generate a human-friendly research plan for HITL review."""
    providers = getattr(request.app.state, "providers", None)

    if not providers or not providers.is_configured():
        raise HTTPException(status_code=503, detail="LLM providers not configured")

    # Unified system prompt — covers all rounds (clarification, direction, approval)
    system_prompt = (
        "You are a research intake analyst for Shannon, an automated deep-research system.\n\n"

        "SYSTEM CAPABILITIES\n\n"
        "After you hand off, the research system will:\n"
        "- Search and extract information from the public web, including news, official sites,\n"
        "  financial filings, academic papers, and region-specific sources\n"
        "- For company research: automatically discover and deeply read official websites,\n"
        "  investor relations pages, and product documentation\n"
        "- Run multiple research agents in parallel, with iterative gap-filling\n"
        "  for under-covered areas\n"
        "- Produce a structured long-form report with inline citations linked\n"
        "  to source URLs\n\n"
        "Limitations:\n"
        "- No access to paywalled, login-required, or proprietary content\n"
        "- No real-time data feeds or live monitoring\n"
        "- Output is a one-time report, not an ongoing feed\n\n"

        "YOUR ROLE\n\n"
        "You are the intake step before research execution. Your job is to align on\n"
        "WHAT to research and WHY before the system spends resources.\n"
        "You are having a multi-turn conversation with the user. Each turn, you assess\n"
        "the state and choose exactly one path.\n\n"

        "DECISION LOGIC\n\n"

        "PATH A → [INTENT:feedback]\n"
        "Condition: You genuinely lack critical information to define a useful research\n"
        "direction. Something important is unknown — their purpose, which aspects\n"
        "matter, or what depth they need.\n"
        "Behavior:\n"
        "- State what you DO understand from the query (never re-ask what was already said)\n"
        "- Ask 1-3 targeted questions about what is ACTUALLY missing\n"
        "- Do NOT propose a research direction yet\n\n"

        "PATH B → [INTENT:ready]\n"
        "Condition: You have enough information to propose a concrete research direction.\n"
        "This includes when the original query was already specific enough — do NOT\n"
        "force clarification questions when the query is clear.\n"
        "Behavior:\n"
        "- Summarize your understanding of their need (1-2 sentences)\n"
        "- Describe the research direction: what areas to cover, in what priority,\n"
        "  what to skip or treat as secondary, and why — grounded in their stated purpose\n"
        "- Keep the conversational part concise (under 300 words)\n"
        "- End by inviting adjustment — not asking permission.\n"
        "  Good: 'You can approve to start, or tell me what to adjust.'\n"
        "  Bad: 'Would you like me to proceed?'\n"
        "Note: Do NOT output any structured blocks like [RESEARCH_BRIEF]. The downstream system will extract structure from your conversational response.\n\n"

        "PATH C → [INTENT:execute]\n"
        "Condition: The user's latest message UNAMBIGUOUSLY signals they want to\n"
        "proceed with the current direction, in any language. No refinements,\n"
        "no 'but also...' qualifiers.\n"
        "The message expresses unconditional approval — e.g. agreement, confirmation,\n"
        "or a direct request to start execution. Short affirmatives count.\n"
        "Mixed messages that approve but also request changes ('good but add X')\n"
        "do NOT count — treat those as PATH B input.\n"
        "Behavior:\n"
        "- Respond with a SHORT confirmation (1-2 sentences)\n"
        "- Do NOT repeat the plan, list steps, or output [RESEARCH_BRIEF]\n\n"

        "OUTPUT RULES\n"
        "- Reply in the SAME LANGUAGE as the user's query\n"
        "- Be concise and direct — no filler, no over-explaining\n"
        "- Never output subtask lists, search queries, or execution steps —\n"
        "  that is the downstream system's job, not yours\n"
        "- Never fabricate results or pretend to run research\n"
        "- If the user asks for something the system cannot do, say so honestly\n"
        "  and suggest what IS feasible\n"
        "- Exactly ONE [INTENT:...] tag at the very end, on its own line\n"
        "- Do NOT echo system capabilities back to the user: no word-count estimates,\n"
        "  no data-source disclaimers ('publicly available', 'within accessible sources'),\n"
        "  no caveats about what the system cannot access.\n"
        "  Your job is to discuss WHAT to research — the system's HOW is not the user's concern.\n"
    )

    # Final round override: force a direction proposal, no more questions
    if body.is_final_round:
        system_prompt += (
            "\n\nFINAL ROUND: This is the last round of discussion. "
            "You MUST output a definitive research direction based on everything discussed. "
            "Do NOT ask more questions. Output [INTENT:ready]."
        )

    # Build messages: system prompt + conversation history (or initial query)
    messages = [{"role": "system", "content": system_prompt}]

    if body.conversation:
        for turn in body.conversation:
            messages.append({
                "role": turn.get("role", "user"),
                "content": turn.get("message", ""),
            })
    else:
        # First round: user query + optional context
        user_content = f"Research topic: {body.query}"
        if body.context:
            ctx_str = ", ".join(
                f"{k}: {v}" for k, v in body.context.items()
                if k not in ("force_research", "require_review", "review_timeout")
            )
            if ctx_str:
                user_content += f"\n\nAdditional context: {ctx_str}"
        messages.append({"role": "user", "content": user_content})

    round_num = len([t for t in (body.conversation or []) if t.get("role") == "user"]) + 1

    try:
        from ..providers.base import ModelTier
        result = await providers.generate_completion(
            messages=messages,
            tier=ModelTier.SMALL,
            max_tokens=2048,
            temperature=0.5,
        )
        plan_text = result.get("output_text", "")
        usage = result.get("usage", {})

        # Parse intent tag from LLM response (all rounds — unified prompt may
        # output [INTENT:ready] even on round 1 if the query is specific enough)
        import re
        detected_intent = "feedback"
        intent_match = re.search(r"\[INTENT:(feedback|ready|execute)\]", plan_text)
        if intent_match:
            detected_intent = intent_match.group(1)
            # Strip the intent tag from the displayed message
            plan_text = re.sub(r"\s*\[INTENT:(?:feedback|ready|execute)\]\s*$", "", plan_text).strip()

        return ResearchPlanResponse(
            message=plan_text,
            intent=detected_intent,
            round=round_num,
            model=result.get("model", ""),
            provider=result.get("provider", ""),
            input_tokens=usage.get("input_tokens", 0),
            output_tokens=usage.get("output_tokens", 0),
        )
    except Exception as e:
        logger.error(f"Research plan generation failed: {e}")
        raise HTTPException(status_code=500, detail=f"Failed to generate research plan: {e}")


@router.post("/agent/decompose", response_model=DecompositionResponse)
async def decompose_task(request: Request, query: AgentQuery) -> DecompositionResponse:
    """
    Decompose a complex task into subtasks using pure LLM approach.

    This endpoint analyzes a query and returns a task decomposition
    for the orchestrator to execute. Tool selection is entirely
    determined by the LLM without any pattern matching.
    """
    try:
        logger.info(f"Decomposing task: {query.query[:100]}...")

        # Get LLM providers
        providers = getattr(request.app.state, "providers", None)
        settings = getattr(request.app.state, "settings", None)

        if not providers or not providers.is_configured():
            logger.error("LLM providers not configured")
            raise HTTPException(status_code=503, detail="LLM service not configured")

        from ..providers.base import ModelTier
        from ..tools import get_registry

        # Load actual tool schemas from registry for precise parameter guidance
        registry = get_registry()
        available_tools = query.allowed_tools or []
        tool_schemas_text = ""

        # Respect role preset's allowed_tools if role is specified
        role_preset_tools = []
        if query.context and "role" in query.context:
            role_name = query.context.get("role")
            if role_name:
                from ..roles.presets import get_role_preset
                role_preset = get_role_preset(role_name)
                role_preset_tools = list(role_preset.get("allowed_tools", []))
                logger.info(f"Decompose: Role '{role_name}' restricts tools to: {role_preset_tools}")

        # Auto-load all tools from registry when no specific tools provided
        # This ensures MCP and OpenAPI tools appear in decomposition even when
        # orchestrator doesn't pass AvailableTools (current limitation)
        if not available_tools:
            # If role preset defines allowed_tools, use those as base
            if role_preset_tools:
                available_tools = role_preset_tools
                logger.info(
                    f"Decompose: Using {len(available_tools)} tools from role preset"
                )
            else:
                all_tool_names = registry.list_tools()
                # Filter out dangerous tools for safety
                available_tools = []
                for name in all_tool_names:
                    tool = registry.get_tool(name)
                    if tool and not getattr(tool.metadata, "dangerous", False):
                        available_tools.append(name)
                logger.info(
                    f"Decompose: Auto-loaded {len(available_tools)} tools from registry (orchestrator provided none)"
                )
        elif role_preset_tools:
            # Cap requested tools by role preset if preset defines restrictions
            available_tools = [t for t in available_tools if t in role_preset_tools]
            logger.info(f"Decompose: Capped tools by role preset to: {available_tools}")

        if available_tools:
            tool_schemas_text = "\n\nAVAILABLE TOOLS WITH EXACT PARAMETER SCHEMAS:\n"
            for tool_name in available_tools:
                tool = registry.get_tool(tool_name)
                if tool:
                    metadata = tool.metadata
                    params = tool.parameters
                    param_details = []
                    required_params = []

                    for p in params:
                        param_str = f'"{p.name}": {p.type.value}'
                        if p.description:
                            param_str += f" - {p.description}"
                        param_details.append(param_str)
                        if p.required:
                            required_params.append(p.name)

                    tool_schemas_text += f"\n{tool_name}:\n"
                    tool_schemas_text += f"  Description: {metadata.description}\n"
                    tool_schemas_text += f"  Parameters: {', '.join(param_details)}\n"
                    if required_params:
                        tool_schemas_text += (
                            f"  Required: {', '.join(required_params)}\n"
                        )
        else:
            tool_schemas_text = "\n\nDefault tools: web_search, calculator, python_executor, file_read\n"

        # ================================================================
        # PROMPT CONSTANTS: Identity prompts + Common decomposition suffix
        # ================================================================

        # Common decomposition instructions (appended to all identity prompts)
        COMMON_DECOMPOSITION_SUFFIX = (
            "CRITICAL: Each subtask MUST have these EXACT fields: id, description, dependencies, estimated_tokens, suggested_tools, tool_parameters\n"
            "NEVER return null for subtasks field - always provide at least one subtask.\n\n"
            "TOOL SELECTION GUIDELINES:\n"
            "Default: Use NO TOOLS unless the task requires external data retrieval or computation.\n\n"
            "## WEB RESEARCH STRATEGY: Search First, Then Fetch\n\n"
            "### STEP 1 - SEARCH (discover relevant pages):\n"
            "- web_search: DEFAULT first step for any web research task\n"
            "  → Use for: 'find info about X', 'research Y', 'what is Z on site W'\n"
            "  → For specific domain: use site_filter parameter OR query='site:example.com [topic]'\n"
            "  → Returns: list of relevant URLs with snippets\n"
            "- CRITICAL: Search task response MUST include 'Top URLs:' section listing 3-8 most relevant URLs\n"
            "  (This is required because dependent tasks read URLs from your response text)\n\n"
            "### STEP 2 - FETCH (read content from search results):\n"
            "- web_fetch: Read single pages FROM SEARCH RESULTS\n"
            "  → Task depends on search task via dependencies field\n"
            "  → Agent reads URLs from search task's response, selects top 3-5 most relevant\n"
            "  → Example decomposition:\n"
            "    Task-1: web_search('site:cloudflare.com container pricing') → outputs Top URLs in response\n"
            "    Task-2: web_fetch (dependencies=['task-1']) → reads URLs from task-1 response, fetches them\n\n"
            "### DIRECT FETCH (skip search) ONLY WHEN:\n"
            "1. User provides SPECIFIC URL: 'read https://example.com/pricing' → web_fetch directly\n"
            "2. Quick homepage check: 'what does company X do' → web_fetch homepage only\n"
            "3. Following up on URL from previous conversation/search\n\n"
            "## THREE FETCH TOOLS - Choose Correctly:\n\n"
            "### web_fetch (single page):\n"
            "- USE: After search, to read specific URLs from search results\n"
            "- USE: When user provides exact URL (https://...)\n"
            "- NOT FOR: Discovering what pages exist on a site\n\n"
            "### web_subpage_fetch (multi-page targeted):\n"
            "- USE ONLY WHEN user specifies MULTIPLE explicit sections:\n"
            "  → 'get pricing, docs, and about pages from stripe.com'\n"
            "  → 'fetch /ir, /news, /press from tesla.com'\n"
            "- Set target_paths parameter with explicit paths: ['/pricing', '/docs', '/about']\n"
            "- NOT FOR: 'find info about X on site Y' (use search → fetch instead)\n"
            "- NOT FOR: Vague requests without explicit page names\n\n"
            "### web_crawl (multi-page exploratory):\n"
            "- USE ONLY WHEN user wants broad site discovery:\n"
            "  → 'crawl this site', 'explore the website', 'scan all pages'\n"
            "  → 'what pages does this site have', 'audit the domain'\n"
            "- USE: Unknown site structure, need comprehensive coverage\n"
            "- NOT FOR: Targeted research with specific questions\n\n"
            "## COMPANY/ENTITY RESEARCH WORKFLOW:\n"
            "1. Search first: '[company] [topic]' or use site_filter='[company].com' with query='[topic]'\n"
            "2. Include intent keywords in search: pricing, cost, plan, tier, 价格, 定价, 套餐 (if relevant)\n"
            "3. Fetch: Top relevant URLs from search results\n"
            "4. Business directories: 'site:crunchbase.com [company]', 'site:linkedin.com [company]'\n"
            "5. Asian companies: Include Japanese/Chinese name variants in searches\n\n"
            "## OTHER TOOLS:\n"
            "- calculator: For mathematical computations beyond basic arithmetic\n"
            "- file_read: When explicitly asked to read/open a specific local file\n"
            "- python_executor: For executing Python code, data analysis, or programming tasks\n"
            "- code_executor: ONLY for executing provided WASM code (do not use for Python)\n\n"
            "## Deep Research 2.0: Task Contracts (Optional, but REQUIRED for research workflows)\n"
            "For research workflows, you MAY include these fields to define explicit task boundaries:\n"
            "- output_format: {type: 'structured'|'narrative', required_fields: [...], optional_fields: [...]}\n"
            "- source_guidance: {required: ['official', 'aggregator'], optional: ['news'], avoid: ['social']}\n"
            "- search_budget: {max_queries: 5, max_fetches: 10}\n"
            "- boundaries: {in_scope: ['topic1', 'topic2'], out_of_scope: ['topic3']}\n\n"
            "Source type values: 'official' (company/.gov/.edu), 'aggregator' (crunchbase/wikipedia), "
            "'news' (recent articles), 'academic' (arxiv/papers), 'github', 'financial', 'local_cn', 'local_jp'\n\n"
            "Return ONLY valid JSON with this EXACT structure (no additional text):\n"
            "{\n"
            '  "mode": "standard",\n'
            '  "complexity_score": 0.5,\n'
            '  "subtasks": [\n'
            "    {\n"
            '      "id": "task-1",\n'
            '      "description": "Task description",\n'
            '      "dependencies": [],\n'
            '      "estimated_tokens": 500,\n'
            '      "suggested_tools": [],\n'
            '      "tool_parameters": {},\n'
            '      "output_format": {"type": "narrative", "required_fields": [], "optional_fields": []},\n'
            '      "source_guidance": {"required": ["official"], "optional": ["news"]},\n'
            '      "search_budget": {"max_queries": 10, "max_fetches": 20},\n'
            '      "boundaries": {"in_scope": ["topic"], "out_of_scope": []}\n'
            "    }\n"
            "  ],\n"
            '  "execution_strategy": "sequential",\n'
            '  "concurrency_limit": 1,\n'
            '  "token_estimates": {"task-1": 500},\n'
            '  "total_estimated_tokens": 500\n'
            "}\n\n"
            "CRITICAL: Tool parameters MUST use EXACT parameter names from schemas. See available tools below.\n\n"
            "IMPORTANT: Use python_executor for Python code execution tasks. Never suggest code_executor unless user\n"
            "explicitly provides WASM bytecode. For general code writing (without execution), handle directly.\n\n"
            f"{tool_schemas_text}\n\n"
            "Example for a stock query 'Analyze Apple stock trend':\n"
            "{\n"
            '  "mode": "standard",\n'
            '  "complexity_score": 0.5,\n'
            '  "subtasks": [\n'
            "    {\n"
            '      "id": "task-1",\n'
            '      "description": "Search for Apple stock trend analysis forecast",\n'
            '      "dependencies": [],\n'
            '      "estimated_tokens": 800,\n'
            '      "suggested_tools": ["web_search", "web_fetch"],\n'
            '      "tool_parameters": {"tool": "web_search", "query": "Apple stock AAPL trend analysis forecast"},\n'
            '      "output_format": {"type": "narrative", "required_fields": [], "optional_fields": []},\n'
            '      "source_guidance": {"required": ["news", "financial"], "optional": ["aggregator"]},\n'
            '      "search_budget": {"max_queries": 10, "max_fetches": 20},\n'
            '      "boundaries": {"in_scope": ["stock price", "market analysis"], "out_of_scope": ["company history"]}\n'
            "    }\n"
            "  ],\n"
            '  "execution_strategy": "sequential",\n'
            '  "concurrency_limit": 1,\n'
            '  "token_estimates": {"task-1": 800},\n'
            '  "total_estimated_tokens": 800\n'
            "}\n\n"
            "Rules:\n"
            '- mode: must be "simple", "standard", or "complex"\n'
            "- complexity_score: number between 0.0 and 1.0\n"
            "- dependencies: array of task ID strings or empty array []\n"
            "- suggested_tools: empty array [] if no tools needed, otherwise list tool names\n"
            "- tool_parameters: empty object {} if no tools, otherwise parameters for the tool\n"
            "- source_guidance: (optional) object with required/optional/avoid source type arrays\n"
            "- boundaries: (optional) object with in_scope/out_of_scope topic arrays\n"
            "- For subtasks with non-empty dependencies, DO NOT prefill tool_parameters; set it to {} and avoid placeholders (the agent will use previous_results to construct exact parameters).\n"
            "- Let the semantic meaning of the query guide tool selection\n"
        )

        # General planning identity (default for non-research tasks)
        GENERAL_PLANNING_IDENTITY = (
            "You are a planning assistant. Analyze the user's task and determine if it needs decomposition.\n"
            "IMPORTANT: Process queries in ANY language including English, Chinese, Japanese, Korean, etc.\n\n"
            "For SIMPLE queries (single action, direct answer, or basic calculation), set complexity_score < 0.3 and provide a single subtask.\n"
            "For COMPLEX queries (multiple steps, dependencies), set complexity_score >= 0.3 and decompose into multiple subtasks.\n\n"
        )

        # Research supervisor identity (for deep research workflows)
        # Defined in roles/deep_research/research_supervisor.py
        from ..roles.deep_research import RESEARCH_SUPERVISOR_IDENTITY, DOMAIN_ANALYSIS_HINT

        # ================================================================
        # PRIORITY-BASED PROMPT SELECTION (IDENTITY + COMMON_SUFFIX)
        # ================================================================
        # Priority order (highest to lowest):
        # 1. Explicit user override (future: context.decomposition_prompt)
        # 2. Deep research context (force_research, workflow_type=="research", role=="deep_research_agent")
        # 3. Role preset (data_analytics, code_assistant, etc.)
        # 4. General default (simple planning assistant)
        #
        # All prompts follow the pattern: IDENTITY_PROMPT + COMMON_DECOMPOSITION_SUFFIX
        # This ensures all branches get the JSON schema and decomposition instructions.

        identity_prompt = None
        prompt_source = "default"

        # Check for explicit override (future-proofing)
        if isinstance(query.context, dict) and query.context.get("decomposition_prompt"):
            identity_prompt = query.context.get("decomposition_prompt")
            prompt_source = "explicit_override"
            logger.info("Decompose: Using explicit override prompt from context")

        # Check for deep research context
        elif isinstance(query.context, dict) and (
            query.context.get("force_research")
            or query.context.get("workflow_type") == "research"
            or query.context.get("role") in ["deep_research_agent", "research_supervisor"]
        ):
            identity_prompt = RESEARCH_SUPERVISOR_IDENTITY
            prompt_source = "research"
            logger.info("Decompose: Using research supervisor identity")

        # NOTE: Role presets are NOT used for decomposition
        # Role-specific prompts are designed for agent execution (answering questions),
        # not for task decomposition. Using role presets here causes conflicts - for
        # example, data_analytics role explicitly requires "dataResult" format,
        # which conflicts with the "subtasks" format required for decomposition.
        #
        # Therefore, we skip role preset selection and fall through to the general
        # planning identity. Role information (allowed_tools) is still respected
        # via the available_tools filtering done earlier in this function.

        # Fallback to general planning identity
        if identity_prompt is None:
            identity_prompt = GENERAL_PLANNING_IDENTITY
            prompt_source = "general"
            logger.info("Decompose: Using general planning identity")

        # Combine identity with common decomposition suffix
        decompose_system_prompt = identity_prompt + COMMON_DECOMPOSITION_SUFFIX

        # FIRST DECISION: Domain Analysis (for company queries)
        # This comes before tool hints because it's the first planning decision (task-1)
        if isinstance(query.context, dict):
            query_type = query.context.get("query_type")
        else:
            query_type = None
        if isinstance(query_type, str) and query_type.strip().lower() == "company":
            decompose_system_prompt = decompose_system_prompt + DOMAIN_ANALYSIS_HINT

        # If tools are available, add a generic tool-aware hint
        if available_tools:
            tool_hint = (
                f"\n\nAVAILABLE TOOLS: {', '.join(available_tools)}\n"
                "When the query requires data retrieval, external APIs, or specific operations that match available tools,\n"
                "create tool-based subtasks with suggested_tools and tool_parameters.\n"
                "Set complexity_score >= 0.5 for queries that need tool execution.\n"
            )
            decompose_system_prompt = decompose_system_prompt + tool_hint

        # Strategy-specific scaling for research workflows
        research_strategy = None
        if isinstance(query.context, dict):
            research_strategy = query.context.get("research_strategy")

        if isinstance(research_strategy, str) and research_strategy and prompt_source == "research":
            strategy_key = research_strategy.strip().lower()
            strategy_guidance = {
                "quick": (
                    "\n\nRESEARCH STRATEGY: quick\n"
                    "- Override the generic simple/complex ranges for this query.\n"
                    "- Prefer 1–3 broad subtasks that cover the main question.\n"
                    "- Focus on a high-level overview instead of exhaustive coverage.\n"
                    "- Avoid splitting into many narrow subtasks.\n"
                    "- Aim for complexity_score < 0.4.\n"
                ),
                "standard": (
                    "\n\nRESEARCH STRATEGY: standard\n"
                    "- Override the generic simple/complex ranges for this query.\n"
                    "- Prefer 3–5 focused subtasks that cover the key dimensions of the query.\n"
                    "- Balance breadth and depth; avoid unnecessary fragmentation.\n"
                    "- Aim for complexity_score between 0.4 and 0.6.\n"
                ),
                "deep": (
                    "\n\nRESEARCH STRATEGY: deep\n"
                    "- Override the generic simple/complex ranges for this query.\n"
                    "- Prefer 5–8 specialized subtasks that each explore a distinct aspect.\n"
                    "- Include follow-up subtasks when clarification or cross-checking is needed.\n"
                    "- Aim for complexity_score between 0.6 and 0.8.\n"
                ),
                "academic": (
                    "\n\nRESEARCH STRATEGY: academic\n"
                    "- Override the generic simple/complex ranges for this query.\n"
                    "- Prefer 8–12 comprehensive subtasks that cover all major aspects of the brief.\n"
                    "- Include methodology/background, main analysis, and verification/limitations subtasks when relevant.\n"
                    "- Aim for complexity_score >= 0.7.\n"
                ),
            }

            if strategy_key in strategy_guidance:
                decompose_system_prompt = decompose_system_prompt + strategy_guidance[strategy_key]

        # If research_areas provided, instruct the planner to decompose 1→N per area and add parent_area
        if isinstance(query.context, dict) and query.context.get("research_areas"):
            areas = query.context.get("research_areas") or []
            if isinstance(areas, list) and areas:
                try:
                    area_list = [str(a) for a in areas if str(a).strip()]
                except Exception:
                    area_list = []
                if area_list:
                    areas_hint = (
                        "\n\nRESEARCH AREA DECOMPOSITION:\n"
                        f"- The user identified {len(area_list)} research areas.\n"
                        "- Create 1–3 subtasks per area (break complex areas into focused steps).\n"
                        f"- Set 'parent_area' for grouping; valid values: {area_list}.\n"
                        "- Keep descriptions concise and ACTION-FIRST; start with a verb, not the area name.\n"
                        "- Example: parent_area='Financial Performance' → description='Analyze Q3 revenue trends'.\n"
                        "- Include 'parent_area' in each subtask JSON when research_areas are provided.\n"
                        "\nDESCRIPTION STYLE:\n"
                        "- Start with action verb, not area name.\n"
                        "- ❌ 'Company profile and history: Company profile and history includes…'\n"
                        "- ✅ 'Analyze founding story, key milestones, and strategic pivots (Company Profile)'.\n"
                        "- ✅ 'Compare market share vs. top 3 competitors (Competitive Landscape)'.\n"
                    )
                    decompose_system_prompt = decompose_system_prompt + areas_hint

        # Dynamic HITL hint: only appended when a confirmed_plan exists from review.
        # When review is OFF, this section is skipped — zero impact on non-review flows.
        if query.context and query.context.get("confirmed_plan"):
            hitl_hint = (
                "\n\nUSER REVIEW CONTEXT (HITL):\n"
                "A research brief from an interactive review session is provided below.\n"
                "You MUST use it to guide subtask allocation:\n\n"
                "1. ALLOCATE subtasks proportionally:\n"
                "   - priority_focus areas → 2-3 subtasks each, higher search_budget (max_queries: 15)\n"
                "   - secondary_focus areas → 1 subtask each, standard search_budget\n"
                "   - skip areas → NO subtask, or fold into another as a minor point\n"
                "2. ENCODE purpose into subtask descriptions:\n"
                "   - Include contextual framing: '(for interview preparation)' or '(for investment analysis)'\n"
                "   - This helps the executing agent choose appropriate depth/perspective\n"
                "3. SET boundaries.out_of_scope to include skip topics\n"
                "4. ADJUST depth by knowledge_level:\n"
                "   - beginner → include foundational/introductory subtask\n"
                "   - intermediate → skip basics, focus on analysis\n"
                "   - expert → deep-dive only, assume domain knowledge\n"
                "5. PRESERVE system tasks:\n"
                "   - If a domain_analysis task is applicable (see FIRST DECISION section above),\n"
                "     you MUST still include it as task-1 with task_type: 'domain_analysis'.\n"
                "   - domain_analysis is a system-level task, NOT a content subtask —\n"
                "     it is independent of the brief's priority/skip areas.\n"
            )
            decompose_system_prompt += hitl_hint

        # Inject current date for time awareness in decomposition
        current_date = None
        if query.context and isinstance(query.context, dict):
            current_date = query.context.get("current_date")
            if not current_date:
                prompt_params = query.context.get("prompt_params")
                if isinstance(prompt_params, dict):
                    current_date = prompt_params.get("current_date")
        if not current_date:
            from datetime import datetime, timezone
            current_date = datetime.now(timezone.utc).strftime("%Y-%m-%d")
        
        date_prefix = f"Current date: {current_date} (UTC).\n\n"
        decompose_system_prompt = date_prefix + decompose_system_prompt

        # Build messages with history rehydration for context awareness
        messages = [{"role": "system", "content": decompose_system_prompt}]


        # Rehydrate history from context if present (same as agent_query endpoint)
        history_rehydrated = False
        if query.context and "history" in query.context:
            history_str = str(query.context.get("history", ""))
            if history_str:
                # Parse the history string format: "role: content\n"
                for line in history_str.strip().split("\n"):
                    if ": " in line:
                        role, content = line.split(": ", 1)
                        # Only add user and assistant messages to maintain conversation flow
                        if role.lower() in ["user", "assistant"]:
                            messages.append({"role": role.lower(), "content": content})
                            history_rehydrated = True

        # Add the current query
        ctx_keys = list(query.context.keys())[:5] if isinstance(query.context, dict) else []
        tools = ",".join(query.allowed_tools or [])
        user = (
            f"Query: {query.query}\nContext keys: {ctx_keys}\nAvailable tools: {tools}"
        )
        messages.append({"role": "user", "content": user})

        # HITL Phase 1: inject structured output from Refine (priority_focus is the switch)
        # When priority_focus exists, Refine has already parsed confirmed_plan into structured format.
        # Decompose consumes these structured fields directly for subtask allocation.
        if query.context and query.context.get("priority_focus"):
            # New path: consume Refine's structured HITL output
            priority_focus = query.context.get("priority_focus", [])
            secondary_focus = query.context.get("secondary_focus", [])
            skip_areas = query.context.get("skip_areas", [])
            user_intent = query.context.get("user_intent", {})
            confirmed_plan = query.context.get("confirmed_plan", "")

            # Build structured brief section from Refine output
            brief_parts = []
            if confirmed_plan:
                brief_parts.append(f"Research Direction:\n{confirmed_plan}")

            brief_parts.append("\n## Structured Focus Areas (from Refine)")
            if priority_focus:
                brief_parts.append(f"priority_focus: {', '.join(priority_focus)}")
            if secondary_focus:
                brief_parts.append(f"secondary_focus: {', '.join(secondary_focus)}")
            if skip_areas:
                brief_parts.append(f"skip: {', '.join(skip_areas)}")

            if user_intent:
                intent_parts = []
                if user_intent.get("purpose"):
                    intent_parts.append(f"purpose={user_intent['purpose']}")
                if user_intent.get("depth"):
                    intent_parts.append(f"depth={user_intent['depth']}")
                if intent_parts:
                    brief_parts.append(f"user_intent: {', '.join(intent_parts)}")

            brief_section = "\n".join(brief_parts)

            messages.append({
                "role": "user",
                "content": (
                    f"USER REVIEW BRIEF:\n{brief_section}\n\n"
                    "Decompose following this brief. "
                    "Prioritize subtasks toward priority_focus areas."
                )
            })
        elif query.context and query.context.get("confirmed_plan"):
            # Legacy fallback: confirmed_plan exists but Refine hasn't structured it yet
            # This handles edge cases during migration or when Refine fails to parse
            confirmed_plan = query.context["confirmed_plan"]

            brief_section = f"Research Direction:\n{confirmed_plan}"
            # Fallback: extract user messages from raw conversation
            review_conv = query.context.get("review_conversation")
            if review_conv:
                conv_list = review_conv if isinstance(review_conv, list) else []
                if isinstance(review_conv, str):
                    try:
                        import json as _json
                        conv_list = _json.loads(review_conv)
                    except Exception:
                        conv_list = []
                user_messages = [r["message"] for r in conv_list if r.get("role") == "user"]
                if user_messages:
                    brief_section += "\n\nUser clarifications during review:\n" + "\n".join(
                        f"- {msg}" for msg in user_messages
                    )

            messages.append({
                "role": "user",
                "content": (
                    f"USER REVIEW BRIEF:\n{brief_section}\n\n"
                    "Decompose following this brief. "
                    "Prioritize subtasks toward priority_focus areas."
                )
            })

        logger.info(
            f"Decompose: Prepared {len(messages)} messages (history_rehydrated={history_rehydrated})"
        )

        try:
            result = await providers.generate_completion(
                messages=messages,
                tier=ModelTier.SMALL,
                max_tokens=8192,  # Increased from 4096 to prevent truncation on complex decompositions
                temperature=0.1,
                response_format={"type": "json_object"},
                specific_model=(
                    settings.decomposition_model_id
                    if settings and settings.decomposition_model_id
                    else None
                ),
            )

            import json as _json

            raw = result.get("output_text", "")
            logger.debug(f"LLM raw response: {raw[:500]}")

            data = None
            try:
                data = _json.loads(raw)
            except Exception as parse_err:
                logger.warning(f"JSON parse error: {parse_err}, response_length={len(raw)}, starts_with_brace={raw.strip().startswith('{') if raw else False}")
                # Try to find first {...} in response
                import re

                match = re.search(r"\{.*\}", raw, re.DOTALL)
                if match:
                    try:
                        data = _json.loads(match.group())
                    except Exception:
                        pass

            if not data:
                # Log only metadata to avoid PII exposure
                logger.error(f"Decomposition failed: LLM did not return valid JSON. response_length={len(raw)}, response_type={'empty' if not raw else 'text' if not raw.strip().startswith('{') else 'malformed_json'}")
                raise ValueError("LLM did not return valid JSON")

            # Extract fields with defaults
            mode = data.get("mode", "standard")
            score = float(data.get("complexity_score", 0.5))
            subtasks_raw = data.get("subtasks", [])

            # Parse subtasks
            subtasks = []
            total_tokens = 0

            # Validation: Check if subtasks is null or empty but complexity suggests it should have tasks
            if (not subtasks_raw or subtasks_raw is None) and score >= 0.3:
                logger.warning(
                    f"Invalid decomposition: complexity={score} but no subtasks. Creating fallback subtask."
                )
                # Create a generic subtask without pattern matching - let LLM decide tools
                subtasks_raw = [
                    {
                        "id": "task-1",
                        "description": query.query[:200],
                        "dependencies": [],
                        "estimated_tokens": 500,
                        "suggested_tools": [],
                        "tool_parameters": {},
                    }
                ]

            for st in subtasks_raw:
                if not isinstance(st, dict):
                    continue

                # Extract tool information if present
                suggested_tools = st.get("suggested_tools", [])
                tool_params = st.get("tool_parameters", {})
                deps = st.get("dependencies", []) or []

                # Log tool analysis by LLM
                if suggested_tools:
                    logger.info(
                        f"LLM tool analysis: suggested_tools={suggested_tools}, tool_parameters={tool_params}"
                    )
                    # For dependent subtasks, clear tool_parameters to avoid placeholders
                    if isinstance(deps, list) and len(deps) > 0:
                        tool_params = {}
                    else:
                        # Add the tool name to parameters if not present and tools are suggested
                        if (
                            suggested_tools
                            and "tool" not in tool_params
                            and len(suggested_tools) > 0
                        ):
                            tool_params["tool"] = suggested_tools[0]

                # Determine structured task type when available or infer for synthesis-like tasks
                task_type = str(st.get("task_type") or "")
                if not task_type:
                    desc_lower = str(st.get("description", "")).strip().lower()
                    if (
                        "synthesize" in desc_lower
                        or "synthesis" in desc_lower
                        or "summarize" in desc_lower
                        or "summary" in desc_lower
                        or "combine" in desc_lower
                        or "aggregate" in desc_lower
                    ):
                        task_type = "synthesis"

                # Keep tool_params as-is without template resolution

                # Deep Research 2.0: Parse task contract fields
                output_format = None
                source_guidance = None
                search_budget = None
                boundaries = None

                if st.get("output_format") and isinstance(st.get("output_format"), dict):
                    try:
                        output_format = OutputFormatSpec(**st["output_format"])
                    except Exception as e:
                        logger.warning(f"Failed to parse output_format: {e}")

                if st.get("source_guidance") and isinstance(st.get("source_guidance"), dict):
                    try:
                        source_guidance = SourceGuidanceSpec(**st["source_guidance"])
                    except Exception as e:
                        logger.warning(f"Failed to parse source_guidance: {e}")

                if st.get("search_budget") and isinstance(st.get("search_budget"), dict):
                    try:
                        search_budget = SearchBudgetSpec(**st["search_budget"])
                    except Exception as e:
                        logger.warning(f"Failed to parse search_budget: {e}")

                if st.get("boundaries") and isinstance(st.get("boundaries"), dict):
                    try:
                        boundaries = BoundariesSpec(**st["boundaries"])
                    except Exception as e:
                        logger.warning(f"Failed to parse boundaries: {e}")

                subtask = Subtask(
                    id=st.get("id", f"task-{len(subtasks) + 1}"),
                    description=st.get("description", ""),
                    dependencies=st.get("dependencies", []),
                    estimated_tokens=st.get("estimated_tokens", 300),
                    task_type=task_type,
                    parent_area=str(st.get("parent_area", "")) if st.get("parent_area") is not None else "",
                    suggested_tools=suggested_tools,
                    tool_parameters=tool_params,
                    output_format=output_format,
                    source_guidance=source_guidance,
                    search_budget=search_budget,
                    boundaries=boundaries,
                )
                subtasks.append(subtask)
                total_tokens += subtask.estimated_tokens

                # Log task contract fields for debugging
                if any([output_format, source_guidance, search_budget, boundaries]):
                    logger.info(
                        f"Deep Research 2.0 task contract for {subtask.id}: "
                        f"output_format={output_format}, source_guidance={source_guidance}, "
                        f"search_budget={search_budget}, boundaries={boundaries}"
                    )


            # Extract extended fields
            exec_strategy = data.get("execution_strategy", "sequential")
            agent_types = data.get("agent_types", [])
            concurrency_limit = data.get("concurrency_limit", 1)
            token_estimates = data.get("token_estimates", {})

            # ================================================================
            # Option 3: Post-parse backfill for missing Task Contract fields
            # ================================================================
            # Ensure research workflows have complete contract fields even if LLM omits them
            is_research_workflow = (
                query.context
                and isinstance(query.context, dict)
                and (
                    query.context.get("force_research") is True
                    or query.context.get("workflow_type") == "research"
                    or query.context.get("role") == "deep_research_agent"
                )
            )

            if is_research_workflow and subtasks:
                logger.info(
                    f"Post-parse backfill: Detected research workflow, checking {len(subtasks)} subtasks for missing contract fields"
                )
                backfilled_count = 0

                for subtask in subtasks:
                    # Backfill output_format if missing
                    if not subtask.output_format:
                        subtask.output_format = OutputFormatSpec(
                            type="narrative", required_fields=[], optional_fields=[]
                        )
                        logger.info(
                            f"Backfilled output_format for subtask {subtask.id} with default narrative"
                        )
                        backfilled_count += 1

                    # Backfill search_budget if missing
                    if not subtask.search_budget:
                        subtask.search_budget = SearchBudgetSpec(
                            max_queries=10, max_fetches=20
                        )
                        logger.info(
                            f"Backfilled search_budget for subtask {subtask.id} with default limits"
                        )
                        backfilled_count += 1

                if backfilled_count > 0:
                    logger.info(
                        f"Post-parse backfill completed: {backfilled_count} fields backfilled across {len(subtasks)} subtasks"
                    )

            # Extract cognitive routing fields from data
            cognitive_strategy = data.get("cognitive_strategy", "decompose")
            confidence = data.get("confidence", 0.8)
            fallback_strategy = data.get("fallback_strategy", "decompose")

            usage = result.get("usage") or {}
            in_tok = int(usage.get("input_tokens") or 0)
            out_tok = int(usage.get("output_tokens") or 0)
            tot_tok = int(usage.get("total_tokens") or (in_tok + out_tok))
            cost_usd = float(usage.get("cost_usd") or 0.0)
            model_used = str(result.get("model") or "")
            provider = str(result.get("provider") or "unknown")

            return DecompositionResponse(
                mode=mode,
                complexity_score=score,
                subtasks=subtasks,
                total_estimated_tokens=total_tokens,
                execution_strategy=exec_strategy,
                agent_types=agent_types,
                concurrency_limit=concurrency_limit,
                token_estimates=token_estimates,
                cognitive_strategy=cognitive_strategy,
                confidence=confidence,
                fallback_strategy=fallback_strategy,
                input_tokens=in_tok,
                output_tokens=out_tok,
                total_tokens=tot_tok,
                cost_usd=cost_usd,
                model_used=model_used,
                provider=provider,
            )

        except Exception as e:
            logger.error(f"LLM decomposition failed: {e}")
            # Return error instead of using heuristics
            raise HTTPException(
                status_code=503,
                detail=f"LLM service unavailable for decomposition: {str(e)}",
            )

    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Error decomposing task: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@router.get("/agent/models")
async def list_models(request: Request):
    """List available models and their tiers."""
    return {
        "models": {
            "small": ["mock-model-v1", "gpt-5-nano-2025-08-07"],
            "medium": ["gpt-5-2025-08-07"],
            "large": ["gpt-5-pro-2025-10-06"],
        },
        "default_tier": "small",
        "mock_enabled": True,
    }


@router.get("/roles")
async def list_roles() -> JSONResponse:
    """Expose role presets for cross-service sync (roles_v1)."""
    try:
        from ..roles.presets import _PRESETS as PRESETS

        # Return safe subset: system_prompt, allowed_tools, caps
        out = {}
        for name, cfg in PRESETS.items():
            out[name] = {
                "system_prompt": cfg.get("system_prompt", ""),
                "allowed_tools": list(cfg.get("allowed_tools", [])),
                "caps": cfg.get("caps", {}),
            }
        return JSONResponse(content=out)
    except Exception as e:
        return JSONResponse(status_code=500, content={"error": str(e)})
