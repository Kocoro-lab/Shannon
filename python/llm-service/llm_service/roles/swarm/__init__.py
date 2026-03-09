"""Swarm V2 prompt definitions.

All swarm-related prompts live here, separated from API endpoint logic.
- agent_protocol.py: Agent's core protocol (actions, phases, rules)
- lead_protocol.py: Lead's orchestration and evaluation prompts
- role_prompts.py: Role-specific prompt snippets for swarm agents
"""

from .agent_protocol import AGENT_LOOP_SYSTEM_PROMPT, COMMON_PROTOCOL_BASE, get_work_protocol
from .lead_protocol import LEAD_SYSTEM_PROMPT
from .role_prompts import SWARM_ROLE_PROMPTS, get_swarm_role_catalog

__all__ = [
    "AGENT_LOOP_SYSTEM_PROMPT",
    "COMMON_PROTOCOL_BASE",
    "get_work_protocol",
    "LEAD_SYSTEM_PROMPT",
    "SWARM_ROLE_PROMPTS",
    "get_swarm_role_catalog",
]
