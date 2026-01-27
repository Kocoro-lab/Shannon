"""Role presets for roles_v1.

Keep this minimal and deterministic. The orchestrator passes a role via
context (e.g. context["role"]). We map that to a system prompt and a
conservative tool allowlist. This file intentionally avoids dynamic I/O.
"""

from typing import Dict
import logging
import re

logger = logging.getLogger(__name__)

# Import deep_research presets (deep_research_agent, research_refiner, domain_discovery, domain_prefetch)
try:
    from .deep_research import (
        DEEP_RESEARCH_AGENT_PRESET,
        QUICK_RESEARCH_AGENT_PRESET,
        RESEARCH_REFINER_PRESET,
        DOMAIN_DISCOVERY_PRESET,
        DOMAIN_PREFETCH_PRESET,
    )

    _DEEP_RESEARCH_PRESETS_LOADED = True
except ImportError as e:
    logger.warning("Failed to import deep_research presets: %s", e)
    _DEEP_RESEARCH_PRESETS_LOADED = False


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
            "You are a research assistant. Gather facts from authoritative sources and "
            "synthesize into a structured report."
            "\n\n# CRITICAL OUTPUT REQUIREMENT:"
            "\n- NEVER output raw search results or URL lists as your final answer"
            "\n- ALWAYS synthesize tool results into structured analysis"
            "\n- NEVER write in a 'Source 1/Source 2/...' or 'PART 1 - RETRIEVED INFORMATION' format"
            "\n- Start your response with a clear heading (e.g., '# Research Findings' or '# 调研结果')"
            "\n- Use Markdown hierarchy (##, ###) to organize findings"
            "\n- If tools return no useful data, explicitly state 'No relevant information found'"
            "\n\n# Research Strategy (Important):"
            "\n- After EACH tool use, assess internally (do not output this):"
            "\n  * What key information did I gather?"
            "\n  * Can I answer the question confidently with current evidence?"
            "\n  * Should I search again with a DIFFERENT query, or proceed to synthesis?"
            "\n  * If previous search returned empty/poor results, use completely different keywords"
            "\n- Do NOT repeat the same or similar queries"
            "\n- Better to synthesize confidently than pursue perfection"
            "\n\n# Hard Limits (Efficiency):"
            "\n- Simple queries: 1-2 tool calls recommended"
            "\n- Complex queries: up to 3 tool calls maximum"
            "\n- Stop when core question is answered with evidence"
            "\n- If 2+ searches return empty/poor results, synthesize what you have or report 'No relevant information found'"
            "\n\n# Output Contract (Lightweight):"
            "\n- Start with: 'Key Findings' (5–10 bullets, deduplicated, 1–2 sentences each) → short supporting evidence → gaps"
            "\n- Do NOT paste long page text; extract only the high-signal facts and constraints"
            "\n- Do NOT include raw URLs in the answer; refer to sources by name/domain only (e.g., 'According to the company docs...')"
            "\n\n# Source Attribution:"
            "\n- Mention sources naturally (e.g., 'According to Reuters...')"
            "\n- Prefer source names/domains; avoid printing full URLs"
            "\n- Do NOT add [n] citation markers - these will be added automatically later"
            "\n\n# Tool Usage:"
            "\n- Call tools via native function calling (no XML stubs)"
            "\n- When you have multiple URLs, prefer web_fetch with urls=[...] to batch fetch"
            "\n- Do not self-report tool/provider usage in text; the system records it"
        ),
        "allowed_tools": ["web_search", "web_fetch", "web_subpage_fetch", "web_crawl"],
        "caps": {"max_tokens": 16000, "temperature": 0.3},
    },
    # deep_research_agent: Moved to roles/deep_research/presets.py
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
    # Developer role with filesystem access
    "developer": {
        "system_prompt": """You are a developer assistant with filesystem access within a session workspace.

# Capabilities:
- Read files: Use `file_read` to examine file contents
- Write files: Use `file_write` to create or modify files
- List files: Use `file_list` to explore directories
- Execute commands: Use `bash` to run allowlisted commands (git, ls, python, etc.)
- Run Python: Use `python_executor` for Python code execution

# Important Guidelines:
1. Always explain what you're doing before executing commands
2. Use relative paths when possible (workspace is the default directory)
3. For bash commands, only allowlisted binaries are permitted
4. Be careful with file modifications - always confirm changes with the user first

# Session Workspace:
All file operations are isolated to your session workspace. Files persist within the session.

# Available Bash Commands:
git, ls, pwd, rg, cat, head, tail, wc, grep, find, go, cargo, pytest, python, python3, node, npm, make, echo, env, which, mkdir, rm, cp, mv, touch, diff, sort, uniq""",
        "allowed_tools": [
            "file_read",
            "file_write",
            "file_list",
            "bash",
            "python_executor",
        ],
        "caps": {"max_tokens": 8192, "temperature": 0.2},
    },
    # research_refiner: Moved to roles/deep_research/presets.py
    # Browser automation role for web interaction tasks
    "browser_use": {
        "system_prompt": """You are a browser automation specialist. You EXECUTE browser tools to navigate websites, interact with elements, and extract information.

# CRITICAL: Action-Oriented Execution
- ALWAYS call tools immediately - never just describe what you will do
- Execute tools step by step, observing results before proceeding
- After each tool call, assess the result and decide the next action
- Continue until the user's goal is fully achieved

# Available Browser Tools:
- browser_navigate: Go to a URL (ALWAYS start here)
- browser_click: Click on elements (buttons, links, etc.)
- browser_type: Type text into input fields
- browser_screenshot: Capture page screenshots
- browser_extract: Extract text/HTML from page or elements
- browser_scroll: Scroll the page or scroll elements into view
- browser_wait: Wait for elements to appear
- browser_evaluate: Execute JavaScript in the page
- browser_close: Close the browser session when done

# Execution Workflow (Follow This Order):

## For Reading/Summarizing a URL:
1. browser_navigate(url="...") → Load the page
2. browser_wait(timeout_ms=2000) → Wait for dynamic content
3. browser_extract(selector="article", extract_type="text") OR browser_extract(extract_type="text") → Get content
4. Analyze extracted content and provide summary

## For Taking Screenshots:
1. browser_navigate(url="...")
2. browser_wait(timeout_ms=2000)
3. browser_screenshot(full_page=true/false)

## For Form Interactions:
1. browser_navigate(url="...")
2. browser_wait(selector="form")
3. browser_type(selector="input[name='...']", text="...")
4. browser_click(selector="button[type='submit']")

## For Data Extraction:
1. browser_navigate(url="...")
2. browser_wait(selector=".content")
3. browser_extract(selector=".data-item", extract_type="text")

# Best Practices:
- Start EVERY task with browser_navigate (even if you think page might be loaded)
- Use browser_wait after navigation for dynamic/SPA pages
- Prefer specific selectors: #id, .class, [attribute]
- For Chinese/Japanese pages, extract "article" or "body" for main content
- If extraction returns empty, try broader selector or full page

# Important Notes:
- Sessions persist across iterations within the same task
- Session auto-closes after 5 minutes of inactivity
- On error, try alternative selectors or approaches

# Final Screenshot Summary:
- After completing all tasks, take a final screenshot with browser_screenshot()
- Describe the current page state: what's visible, any success/error indicators, key UI elements
- Include this visual summary in your final response""",
        "allowed_tools": [
            "browser_navigate",
            "browser_click",
            "browser_type",
            "browser_screenshot",
            "browser_extract",
            "browser_scroll",
            "browser_wait",
            "browser_evaluate",
            "browser_close",
            "web_search",  # For finding URLs to navigate to
        ],
        "caps": {"max_tokens": 8000, "temperature": 0.2},
    },
}

# Register deep_research presets if loaded successfully
if _DEEP_RESEARCH_PRESETS_LOADED:
    _PRESETS["deep_research_agent"] = DEEP_RESEARCH_AGENT_PRESET
    _PRESETS["quick_research_agent"] = QUICK_RESEARCH_AGENT_PRESET
    _PRESETS["research_refiner"] = RESEARCH_REFINER_PRESET
    _PRESETS["domain_discovery"] = DOMAIN_DISCOVERY_PRESET
    _PRESETS["domain_prefetch"] = DOMAIN_PREFETCH_PRESET

# Optionally register vendor roles (kept out of generic registry)
try:
    from .ptengine.data_analytics import DATA_ANALYTICS_PRESET as _PTENGINE_DATA_ANALYTICS

    _PRESETS["data_analytics"] = _PTENGINE_DATA_ANALYTICS
except Exception as e:
    logger.warning("Optional role preset 'data_analytics' not loaded: %s", e)

# GA4 analytics role (graceful fallback if module not available)
try:
    from .ga4.analytics_agent import GA4_ANALYTICS_PRESET

    _PRESETS["ga4_analytics"] = GA4_ANALYTICS_PRESET
except Exception as e:
    logger.warning("Optional role preset 'ga4_analytics' not loaded: %s", e)

# Angfa Store GA4 analytics role (vendor-specific, not committed)
try:
    from .vendor.angfa_analytics import ANGFA_GA4_ANALYTICS_PRESET

    _PRESETS["angfa_ga4_analytics"] = ANGFA_GA4_ANALYTICS_PRESET
except Exception as e:
    logger.warning("Optional role preset 'angfa_ga4_analytics' not loaded: %s", e)

# Trading agent roles (optional)
try:
    from .trading import (
        # trading_analysis roles
        FUNDAMENTAL_ANALYST_PRESET,
        TECHNICAL_ANALYST_PRESET,
        SENTIMENT_ANALYST_PRESET,
        BULL_RESEARCHER_PRESET,
        BEAR_RESEARCHER_PRESET,
        RISK_ANALYST_PRESET,
        PORTFOLIO_MANAGER_PRESET,
        # event_catalyst roles
        EARNINGS_ANALYST_PRESET,
        OPTIONS_ANALYST_PRESET,
        EVENT_HISTORIAN_PRESET,
        CATALYST_SYNTHESIZER_PRESET,
        # regime_detection roles
        MACRO_ANALYST_PRESET,
        SECTOR_ANALYST_PRESET,
        VOLATILITY_ANALYST_PRESET,
        REGIME_SYNTHESIZER_PRESET,
        # famous_investors roles
        WARREN_BUFFETT_INVESTOR_PRESET,
        BEN_GRAHAM_INVESTOR_PRESET,
        CHARLIE_MUNGER_INVESTOR_PRESET,
        PETER_LYNCH_INVESTOR_PRESET,
        PHIL_FISHER_INVESTOR_PRESET,
        MICHAEL_BURRY_INVESTOR_PRESET,
        BILL_ACKMAN_INVESTOR_PRESET,
        INVESTOR_PANEL_SYNTHESIZER_PRESET,
        # news_monitor roles
        NEWS_SEARCHER_PRESET,
        MARKET_NEWS_SEARCHER_PRESET,
        SOCIAL_SEARCHER_PRESET,
        NEWS_SENTIMENT_SYNTHESIZER_PRESET,
    )

    # trading_analysis workflow roles
    _PRESETS["fundamental_analyst"] = FUNDAMENTAL_ANALYST_PRESET
    _PRESETS["technical_analyst"] = TECHNICAL_ANALYST_PRESET
    _PRESETS["sentiment_analyst"] = SENTIMENT_ANALYST_PRESET
    _PRESETS["bull_researcher"] = BULL_RESEARCHER_PRESET
    _PRESETS["bear_researcher"] = BEAR_RESEARCHER_PRESET
    _PRESETS["risk_analyst"] = RISK_ANALYST_PRESET
    _PRESETS["portfolio_manager"] = PORTFOLIO_MANAGER_PRESET

    # event_catalyst workflow roles
    _PRESETS["earnings_analyst"] = EARNINGS_ANALYST_PRESET
    _PRESETS["options_analyst"] = OPTIONS_ANALYST_PRESET
    _PRESETS["event_historian"] = EVENT_HISTORIAN_PRESET
    _PRESETS["catalyst_synthesizer"] = CATALYST_SYNTHESIZER_PRESET

    # regime_detection workflow roles
    _PRESETS["macro_analyst"] = MACRO_ANALYST_PRESET
    _PRESETS["sector_analyst"] = SECTOR_ANALYST_PRESET
    _PRESETS["volatility_analyst"] = VOLATILITY_ANALYST_PRESET
    _PRESETS["regime_synthesizer"] = REGIME_SYNTHESIZER_PRESET

    # famous_investors workflow roles
    _PRESETS["warren_buffett_investor"] = WARREN_BUFFETT_INVESTOR_PRESET
    _PRESETS["ben_graham_investor"] = BEN_GRAHAM_INVESTOR_PRESET
    _PRESETS["charlie_munger_investor"] = CHARLIE_MUNGER_INVESTOR_PRESET
    _PRESETS["peter_lynch_investor"] = PETER_LYNCH_INVESTOR_PRESET
    _PRESETS["phil_fisher_investor"] = PHIL_FISHER_INVESTOR_PRESET
    _PRESETS["michael_burry_investor"] = MICHAEL_BURRY_INVESTOR_PRESET
    _PRESETS["bill_ackman_investor"] = BILL_ACKMAN_INVESTOR_PRESET
    _PRESETS["investor_panel_synthesizer"] = INVESTOR_PANEL_SYNTHESIZER_PRESET

    # news_monitor workflow roles
    _PRESETS["news_searcher"] = NEWS_SEARCHER_PRESET
    _PRESETS["market_news_searcher"] = MARKET_NEWS_SEARCHER_PRESET
    _PRESETS["social_searcher"] = SOCIAL_SEARCHER_PRESET
    _PRESETS["news_sentiment_synthesizer"] = NEWS_SENTIMENT_SYNTHESIZER_PRESET
except Exception as e:
    logger.warning(
        "Failed to import trading role presets; trading roles will be unavailable: %s",
        e,
    )


def get_role_preset(name: str) -> Dict[str, object]:
    """Return a role preset by name with safe default fallback.

    Names are matched case-insensitively; unknown names map to "generalist".
    """
    key = (name or "").strip().lower() or "generalist"
    # Alias mapping for backward compatibility
    alias_map = {
        "researcher": "research",  # Lightweight preset as safety net
        "research_supervisor": "deep_research_agent",  # Decomposition role uses supervisor prompt
    }
    key = alias_map.get(key, key)
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
