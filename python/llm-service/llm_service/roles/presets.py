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

