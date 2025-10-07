"""Role presets for roles_v1.

Keep this minimal and deterministic. The orchestrator passes a role via
context (e.g. context["role"]). We map that to a system prompt and a
conservative tool allowlist. This file intentionally avoids dynamic I/O.
"""

from typing import Dict


_PRESETS: Dict[str, Dict[str, object]] = {
    "analysis": {
        "system_prompt": (
            "You are an analytical assistant. Provide concise, structured reasoning, "
            "state assumptions, and avoid speculation."
        ),
        "allowed_tools": ["web_search", "code_reader"],
        "caps": {"max_tokens": 1200, "temperature": 0.2},
    },
    "research": {
        "system_prompt": (
            "You are a research assistant. Gather facts, cite sources briefly, and "
            "summarize objectively."
        ),
        "allowed_tools": ["web_search"],
        "caps": {"max_tokens": 1600, "temperature": 0.3},
    },
    "writer": {
        "system_prompt": (
            "You are a technical writer. Produce clear, helpful, and organized prose."
        ),
        "allowed_tools": ["code_reader"],
        "caps": {"max_tokens": 1800, "temperature": 0.6},
    },
    "critic": {
        "system_prompt": (
            "You are a critical reviewer. Point out flaws, risks, and suggest actionable fixes."
        ),
        "allowed_tools": ["code_reader"],
        "caps": {"max_tokens": 800, "temperature": 0.2},
    },
    # Default/generalist role
    "generalist": {
        "system_prompt": "You are a helpful AI assistant.",
        "allowed_tools": [],
        "caps": {"max_tokens": 1200, "temperature": 0.7},
    },
}


def get_role_preset(name: str) -> Dict[str, object]:
    """Return a role preset by name with safe default fallback.

    Names are matched case-insensitively; unknown names map to "generalist".
    """
    key = (name or "").strip().lower() or "generalist"
    return _PRESETS.get(key, _PRESETS["generalist"]).copy()


def render_system_prompt(prompt: str, context: Dict[str, object]) -> str:
    """Render a system prompt by substituting ${variable} placeholders from context.

    Variable resolution order (whitelisted keys only):
    1. context["prompt_params"][key]
    2. context["tool_parameters"][key]

    Non-whitelisted context keys (like "role", "system_prompt") are ignored.
    Missing variables are replaced with empty strings.

    Args:
        prompt: System prompt string with optional ${variable} placeholders
        context: Context dictionary containing prompt_params or tool_parameters

    Returns:
        Rendered prompt with variables substituted
    """
    import re
    from typing import Any

    # Whitelist of context keys to use for variable substitution
    ALLOWED_SOURCES = ["prompt_params", "tool_parameters"]

    # Build variable lookup from whitelisted sources
    variables: Dict[str, str] = {}
    for source in ALLOWED_SOURCES:
        if source in context and isinstance(context[source], dict):
            for key, value in context[source].items():
                if key not in variables:  # First occurrence wins (prompt_params > tool_parameters)
                    variables[key] = str(value) if value is not None else ""

    # Substitute ${variable} patterns
    def substitute(match: Any) -> str:
        var_name = match.group(1)
        return variables.get(var_name, "")  # Missing variables -> empty string

    return re.sub(r"\$\{(\w+)\}", substitute, prompt)
