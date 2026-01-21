"""Deep Research role presets for ResearchWorkflow.

This module contains specialized roles for:
- deep_research_agent: Main subtask agent for deep research
- research_refiner: Query expansion and research planning
- domain_discovery: Company domain identification
- domain_prefetch: Website content pre-fetching
"""

from .deep_research_agent import DEEP_RESEARCH_AGENT_PRESET
from .research_refiner import RESEARCH_REFINER_PRESET
from .domain_discovery import DOMAIN_DISCOVERY_PRESET
from .domain_prefetch import DOMAIN_PREFETCH_PRESET

__all__ = [
    "DEEP_RESEARCH_AGENT_PRESET",
    "RESEARCH_REFINER_PRESET",
    "DOMAIN_DISCOVERY_PRESET",
    "DOMAIN_PREFETCH_PRESET",
]
