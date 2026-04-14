"""Lead Agent decision endpoint for Swarm V2."""

import asyncio
import json as json_module
import logging
import re
from typing import Any, Dict, List, Optional

from fastapi import APIRouter, HTTPException, Request
from pydantic import BaseModel, Field

from .agent import strip_markdown_json_wrapper
from ..providers.base import ModelTier

logger = logging.getLogger(__name__)

router = APIRouter()


# ── Models ──────────────────────────────────────────────────────────────


class LeadEvent(BaseModel):
    type: str = Field(..., description="Event type: agent_completed, agent_idle, help_request, checkpoint, human_input")
    agent_id: str = Field(default="", description="Agent that triggered the event")
    result_summary: str = Field(default="", description="Summary of agent's result")
    human_message: str = Field(default="", description="Message from human user (for human_input events)")
    completion_report: Dict[str, Any] = Field(default_factory=dict, description="Structured agent output: summary, files_written, key_findings")
    file_contents: List[Dict[str, Any]] = Field(default_factory=list, description="Lead file_read results")
    tool_results: List[Dict[str, Any]] = Field(default_factory=list, description="Lead tool_call results")


class AgentState(BaseModel):
    agent_id: str
    status: str  # "running", "idle", "completed"
    current_task: str = ""
    iterations_used: int = 0
    role: str = ""  # Agent's assigned role (researcher, analyst, synthesis_writer, etc.)
    files_written: List[str] = Field(default_factory=list)  # Files written by agent


class LeadBudget(BaseModel):
    total_llm_calls: int = 0
    remaining_llm_calls: int = 200
    total_tokens: int = 0
    remaining_tokens: int = 1000000
    elapsed_seconds: int = 0
    max_wall_clock_seconds: int = 1800


class LeadDecisionRequest(BaseModel):
    workflow_id: str
    event: LeadEvent
    task_list: List[Dict[str, Any]] = Field(default_factory=list)
    agent_states: List[AgentState] = Field(default_factory=list)
    budget: LeadBudget = Field(default_factory=LeadBudget)
    history: List[Dict[str, Any]] = Field(default_factory=list)  # Recent 5 Lead decisions
    messages: List[Dict[str, Any]] = Field(default_factory=list)  # Agent→Lead mailbox messages
    original_query: str = Field(default="", description="User's original query for language context")
    conversation_history: List[Dict[str, Any]] = Field(default_factory=list)  # Session history for multi-turn context
    workspace_files: List[str] = Field(default_factory=list, description="File paths in workspace")
    hitl_messages: List[str] = Field(default_factory=list, description="All HITL messages received during execution")
    lead_model_override: str = Field(default="", description="Explicit model for Lead (e.g. MiniMax-M2.7)")
    lead_provider_override: str = Field(default="", description="Explicit provider for Lead (e.g. minimax)")


class LeadAction(BaseModel):
    type: str  # interim_reply, spawn_agent, assign_task, send_message, broadcast, revise_plan, file_read, shutdown_agent, noop, done, reply, synthesize
    task_id: str = ""
    agent_id: str = ""
    role: str = ""
    task_description: str = ""
    to: str = ""
    content: str = ""
    model_tier: str = ""  # small, medium, large — Lead can specify per agent
    create: List[Dict[str, Any]] = Field(default_factory=list)
    cancel: List[str] = Field(default_factory=list)
    update: List[Dict[str, Any]] = Field(default_factory=list)  # revise_plan: update existing task descriptions
    path: str = ""  # file_read target path
    tool: str = ""  # tool_call: web_search, web_fetch, calculator
    tool_params: Dict[str, Any] = Field(default_factory=dict)  # tool_call: tool arguments


# JSON Schema for Anthropic structured output (output_config.format).
# Defined for future use — currently disabled due to Anthropic grammar compilation timeout (~30s on first request).
LEAD_DECISION_SCHEMA = {
    "format": {
        "type": "json_schema",
        "schema": {
            "type": "object",
            "properties": {
                "decision_summary": {"type": "string"},
                "user_summary": {"type": "string"},
                "actions": {
                    "type": "array",
                    "items": {
                        "type": "object",
                        "properties": {
                            "type": {
                                "type": "string",
                                "enum": [
                                    "interim_reply", "spawn_agent", "assign_task",
                                    "send_message", "broadcast", "revise_plan",
                                    "file_read", "file_list", "shutdown_agent", "noop",
                                    "done", "reply", "synthesize", "tool_call",
                                ],
                            },
                            "task_id": {"type": "string"},
                            "agent_id": {"type": "string"},
                            "role": {"type": "string"},
                            "task_description": {"type": "string"},
                            "to": {"type": "string"},
                            "content": {"type": "string"},
                            "model_tier": {"type": "string"},
                            "create": {
                                "type": "array",
                                "items": {
                                    "type": "object",
                                    "properties": {
                                        "id": {"type": "string"},
                                        "description": {"type": "string"},
                                        "depends_on": {
                                            "type": "array",
                                            "items": {"type": "string"},
                                        },
                                    },
                                    "required": ["id", "description", "depends_on"],
                                    "additionalProperties": False,
                                },
                            },
                            "cancel": {
                                "type": "array",
                                "items": {"type": "string"},
                            },
                            "update": {
                                "type": "array",
                                "items": {
                                    "type": "object",
                                    "properties": {
                                        "id": {"type": "string"},
                                        "description": {"type": "string"},
                                    },
                                    "required": ["id", "description"],
                                    "additionalProperties": False,
                                },
                            },
                            "path": {"type": "string"},
                            "tool": {"type": "string"},
                            # Intentionally "string" — forces LLM to output JSON-encoded string
                            # which _parse_tool_params() converts to Dict. Nested "object" type
                            # degrades constrained decoding quality for complex tool arguments.
                            "tool_params": {"type": "string"},
                        },
                        "required": ["type", "task_id", "agent_id", "role", "task_description", "to", "content", "model_tier", "create", "cancel", "update", "path", "tool", "tool_params"],
                        "additionalProperties": False,
                    },
                },
            },
            "required": ["decision_summary", "user_summary", "actions"],
            "additionalProperties": False,
        },
    }
}


class LeadDecisionResponse(BaseModel):
    decision_summary: str = ""
    user_summary: str = ""
    actions: List[LeadAction] = Field(default_factory=list)
    tokens_used: int = 0
    input_tokens: int = 0
    output_tokens: int = 0
    cache_read_tokens: int = 0
    cache_creation_tokens: int = 0
    model_used: str = ""
    provider: str = ""


# ── System Prompt (single source of truth: roles/swarm/lead_protocol.py) ──

from llm_service.roles.swarm.lead_protocol import LEAD_SYSTEM_PROMPT


# ── Auto-link: match spawn_agent to tasks by description ────────────────
#
# Anthropic structured output skips task_id in spawn_agent actions because
# constrained decoding favors keys with trivial values (cancel:[], path:"")
# over keys needing cross-action reasoning (task_id requires referencing
# revise_plan's create array). This function matches by description instead.


def _parse_tool_params(raw) -> Dict[str, Any]:
    """Parse tool_params from string (schema) or dict (fallback)."""
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, str) and raw.strip():
        try:
            parsed = json_module.loads(raw)
            if isinstance(parsed, dict):
                return parsed
        except json_module.JSONDecodeError:
            pass
    return {}


def _auto_link_task_ids(
    actions: List["LeadAction"],
    task_list_from_body: List[Dict[str, Any]],
) -> None:
    """Inject task_id into spawn_agent actions missing it, by matching descriptions."""
    # 1. Collect available tasks: from revise_plan in this response + pending in task_list
    available: Dict[str, str] = {}  # task_id → description

    for a in actions:
        if a.type == "revise_plan":
            for t in a.create:
                tid = t.get("id", "")
                desc = t.get("description", "")
                if tid and desc:
                    available[tid] = desc

    for t in (task_list_from_body or []):
        status = t.get("status", "")
        if status in ("", "pending"):
            tid = t.get("id", "")
            desc = t.get("description", "")
            if tid and desc and tid not in available:
                available[tid] = desc

    if not available:
        return

    # 2. Match each spawn_agent (missing task_id) to best task by description overlap
    claimed: set = set()
    for a in actions:
        if a.type == "spawn_agent" and not a.task_id and a.task_description:
            best_tid = _best_match(a.task_description, available, claimed)
            if best_tid:
                a.task_id = best_tid
                claimed.add(best_tid)
                logger.info(
                    f"Auto-linked spawn_agent to task: task_id={best_tid} "
                    f"role={a.role} desc='{a.task_description[:60]}'"
                )

    # 3. Warn on any remaining unlinked spawn_agent
    for a in actions:
        if a.type == "spawn_agent" and not a.task_id:
            logger.warning(
                f"spawn_agent still missing task_id after auto-link: "
                f"role={a.role} desc='{a.task_description[:80]}'"
            )


def _best_match(
    spawn_desc: str,
    available: Dict[str, str],
    claimed: set,
) -> str:
    """Find the best matching task_id for a spawn_agent description."""
    spawn_lower = spawn_desc.lower()
    spawn_words = set(re.findall(r'\w+', spawn_lower))

    best_tid = ""
    best_score = 0.0

    for tid, task_desc in available.items():
        if tid in claimed:
            continue
        task_lower = task_desc.lower()

        # Exact substring match (strongest signal — LLM often copies description)
        if task_lower in spawn_lower or spawn_lower.startswith(task_lower):
            return tid

        # Word overlap ratio (strip punctuation via \w+ regex)
        task_words = set(re.findall(r'\w+', task_lower))
        if not task_words:
            continue
        overlap = len(spawn_words & task_words)
        score = overlap / len(task_words)

        if score > best_score:
            best_score = score
            best_tid = tid

    # Require at least 60% word overlap (raised from 50% to reduce false positives on short descriptions)
    return best_tid if best_score >= 0.6 else ""


# ── Prompt Builder (extracted for testability) ─────────────────────────


def _build_lead_user_prompt(body: LeadDecisionRequest) -> str:
    """Build the user prompt for Lead decision. Extracted for testability."""
    from datetime import datetime, timezone

    user_parts: List[str] = []

    # Inject current date for temporal awareness (search quality + freshness judgment)
    current_date = datetime.now(timezone.utc).strftime("%Y-%m-%d")
    user_parts.append(f"The current date is {current_date} (UTC).")

    # Inject original query so Lead always knows the user's language
    if body.original_query and body.event.type != "initial_plan":
        user_parts.append(f"## Original User Query\n{body.original_query}\n\n⚠ IMPORTANT: Reply in the SAME LANGUAGE as the query above.")

    # Inject session conversation history for multi-turn context
    if body.conversation_history:
        conv_lines = []
        for msg in body.conversation_history:
            role = msg.get("role", "unknown")
            content = str(msg.get("content", ""))
            label = "User" if role == "user" else "Assistant"
            conv_lines.append(f"**{label}**: {content}")
        user_parts.append(
            "## Conversation History (previous turns in this session)\n"
            + "\n\n".join(conv_lines)
            + "\n\n⚠ The current query is a FOLLOW-UP to this conversation. "
            "Workspace may contain files from previous research — use file_read to check before re-researching."
        )

    # Inject accumulated HITL messages so Lead never forgets user requests
    if body.hitl_messages and body.event.type != "human_input":
        hitl_section = "## User Requests During Execution\n"
        for i, msg in enumerate(body.hitl_messages, 1):
            hitl_section += f"{i}. \"{msg}\"\n"
        hitl_section += "\n⚠ These are MANDATORY user requests. Any pending tasks created for these requests MUST be completed before calling done."
        user_parts.append(hitl_section)

    if body.event.type == "human_input":
        user_parts.append(
            f"## Event\n[HUMAN INPUT] The user has sent a message during execution:\n\"{body.event.human_message}\"\n\nTreat this with HIGH PRIORITY. Consider adjusting the plan, spawning new agents, or reassigning idle agents based on their feedback."
        )
    else:
        event_lines = [f"**{body.event.type}**: {body.event.agent_id}"]
        report = body.event.completion_report or {}
        summary = report.get("summary") or body.event.result_summary
        if summary:
            # closing_checkpoint includes workspace file contents — needs higher limit
            summary_limit = 30000 if body.event.type == "closing_checkpoint" else 10000
            event_lines.append(f"  Summary: {str(summary)[:summary_limit]}")
        files = report.get("files_written", [])
        if files:
            event_lines.append(f"  Files written: {', '.join(str(f) for f in files[:10])}")
        findings = report.get("key_findings", [])
        if findings:
            for f in findings[:10]:
                event_lines.append(f"  - {str(f)[:1000]}")
        tools = report.get("tools_used", "")
        if tools:
            event_lines.append(f"  Tools used: {tools}")
        user_parts.append("## Event\n" + "\n".join(event_lines))

    # File contents from Lead's file_read actions (independent verification)
    if body.event.file_contents:
        fc_lines = []
        for fc in body.event.file_contents[:5]:
            path = fc.get("path", "")
            content = str(fc.get("content", ""))[:4000]
            trunc = " [truncated]" if fc.get("truncated") else ""
            error = fc.get("error", "")
            if error:
                fc_lines.append(f"  File [{path}]: ERROR — {error}")
            else:
                fc_lines.append(f"  File [{path}]{trunc}:")
                for line in content.split("\n")[:50]:
                    fc_lines.append(f"    {line}")
        user_parts.append("## File Preview (from your file_read)\n" + "\n".join(fc_lines))

    # Tool results from Lead's direct tool_call actions
    if body.event.tool_results:
        tr_lines = []
        for tr in body.event.tool_results[:5]:
            tool_name = tr.get("tool", "unknown")
            error = tr.get("error", "")
            output = str(tr.get("output", ""))[:4000]
            if error:
                tr_lines.append(f"  **{tool_name}**: ERROR — {error}")
            else:
                tr_lines.append(f"  **{tool_name}**:\n{output}")
        user_parts.append("## Tool Results (from your direct execution)\n" + "\n\n".join(tr_lines))

    # Task Board
    if body.task_list:
        tl_lines = []
        for t in body.task_list:
            status = t.get("status", "pending").upper()
            owner = t.get("owner", "unassigned")
            desc = t.get("description", "")
            tid = t.get("id", "")
            tl_lines.append(f"- {tid} [{status}] {owner}: {desc}")
        user_parts.append("## Task Board\n" + "\n".join(tl_lines))

    # Agent States — with explicit running count to prevent premature done
    if body.agent_states:
        running_agents = [a for a in body.agent_states if a.status == "running"]
        idle_agents = [a for a in body.agent_states if a.status == "idle"]
        completed_agents = [a for a in body.agent_states if a.status == "completed"]
        as_lines = []
        for a in body.agent_states:
            line = f"- {a.agent_id} [{a.status}] role={a.role} task=\"{a.current_task}\" iterations={a.iterations_used}"
            if a.files_written:
                line += f" files=[{', '.join(a.files_written)}]"
            as_lines.append(line)
        summary_line = f"Total: {len(body.agent_states)} agents — {len(running_agents)} running, {len(idle_agents)} idle, {len(completed_agents)} completed"
        if running_agents:
            summary_line += f"\n⚠ RUNNING AGENTS: {', '.join(a.agent_id for a in running_agents)} — do NOT call done while agents are running, use noop instead"
        user_parts.append("## Agent States\n" + summary_line + "\n" + "\n".join(as_lines))

    # Workspace file inventory (helps Lead plan file-level operations)
    if body.workspace_files:
        ws_lines = [f"## Workspace Files ({len(body.workspace_files)} files)"]
        for fp in body.workspace_files[:30]:  # Cap display at 30 files
            ws_lines.append(f"- {fp}")
        if len(body.workspace_files) > 30:
            ws_lines.append(f"... and {len(body.workspace_files) - 30} more")
        user_parts.append("\n".join(ws_lines))

    # Budget with time phase awareness
    budget = body.budget
    total_calls = budget.total_llm_calls + budget.remaining_llm_calls
    budget_pct = (budget.total_llm_calls / max(total_calls, 1)) * 100
    time_pct = (budget.elapsed_seconds / max(budget.max_wall_clock_seconds, 1)) * 100
    remaining_seconds = max(budget.max_wall_clock_seconds - budget.elapsed_seconds, 0)
    elapsed_min = budget.elapsed_seconds // 60
    elapsed_sec = budget.elapsed_seconds % 60
    remaining_min = remaining_seconds // 60
    remaining_sec = remaining_seconds % 60

    budget_lines = [
        f"## Budget ({budget_pct:.0f}% calls used, {time_pct:.0f}% time used)",
        f"- LLM calls: {budget.total_llm_calls} used, {budget.remaining_llm_calls} remaining",
        f"- Tokens: {budget.total_tokens} used, {budget.remaining_tokens} remaining",
        f"- Time: {elapsed_min}m{elapsed_sec:02d}s elapsed, {remaining_min}m{remaining_sec:02d}s remaining (max {budget.max_wall_clock_seconds // 60}m)",
    ]

    # Time phase alert — matches TIME MANAGEMENT thresholds in system prompt
    if time_pct >= 80:
        budget_lines.append("EMERGENCY: >80% time used — call done ASAP once no agents running")
    elif time_pct >= 60:
        budget_lines.append("WRAP-UP PHASE: >60% time — broadcast wrap-up to running agents, no new tasks")
    elif time_pct >= 33:
        budget_lines.append("FOCUS PHASE: >33% time — no new task creation, let agents finish")

    user_parts.append("\n".join(budget_lines))

    # Agent->Lead messages (escalations, requests from agents)
    if body.messages:
        msg_lines = []
        for msg in body.messages[:10]:  # Cap at 10 messages
            from_agent = msg.get("from", "unknown")
            msg_type = msg.get("type", "info")
            payload = msg.get("payload", {})
            text = payload.get("message", str(payload))[:500] if isinstance(payload, dict) else str(payload)[:500]
            msg_lines.append(f"- From {from_agent} ({msg_type}): {text}")
        user_parts.append("## Agent Messages (agents sent these to you)\n" + "\n".join(msg_lines))

    # Decision History
    if body.history:
        hist_lines = []
        for h in body.history[-5:]:
            hist_lines.append(f"- {h.get('decision_summary', 'no summary')}")
        user_parts.append("## Your Recent Decisions\n" + "\n".join(hist_lines))

    user_parts.append("Decide your next action(s). Return ONLY valid JSON.")
    return "\n\n".join(user_parts)


# ── Endpoint ────────────────────────────────────────────────────────────


@router.post("/lead/decide", response_model=LeadDecisionResponse)
async def lead_decide(request: Request, body: LeadDecisionRequest) -> LeadDecisionResponse:
    """LLM-powered Lead decision for Swarm V2 orchestration."""
    logger.info(f"Lead decide called: workflow={body.workflow_id} event={body.event.type}")

    providers = getattr(request.app.state, "providers", None)
    if not providers or not providers.is_configured():
        raise HTTPException(status_code=503, detail="LLM providers not configured")

    try:
        user_prompt = _build_lead_user_prompt(body)

        # Call LLM using the same pattern as agent_loop_step
        # closing_checkpoint may produce a long reply (full synthesis report),
        # so give it more output budget to avoid JSON truncation.
        lead_max_tokens = 16000 if body.event.type == "closing_checkpoint" else 4096
        lead_messages = [
            {"role": "system", "content": LEAD_SYSTEM_PROMPT},
            {"role": "user", "content": user_prompt},
        ]

        # Lead model/provider override: use explicit values if specified, otherwise tier-based
        lead_specific_model = body.lead_model_override or None
        lead_provider_override = body.lead_provider_override or None
        lead_kwargs = {
            "messages": lead_messages,
            "temperature": 0.3,
            "max_tokens": lead_max_tokens,
            "cache_source": "lead_decide",
        }
        if lead_specific_model:
            lead_kwargs["specific_model"] = lead_specific_model
            logger.info(f"Lead using model override: {lead_specific_model}")
        else:
            lead_kwargs["tier"] = ModelTier.MEDIUM
        if lead_provider_override:
            lead_kwargs["provider_override"] = lead_provider_override
            logger.info(f"Lead using provider override: {lead_provider_override}")

        # Try structured output first; fall back to prompt-based on failure or timeout.
        # Only attempt structured output for Anthropic provider — MiniMax/Kimi don't support output_config.
        use_structured = lead_provider_override in (None, "", "anthropic")
        if use_structured:
            try:
                result = await asyncio.wait_for(
                    providers.generate_completion(
                        **lead_kwargs,
                        output_config=LEAD_DECISION_SCHEMA,
                    ),
                    timeout=30,
                )
                logger.info(
                    f"Lead decide: structured output succeeded, "
                    f"finish_reason={result.get('finish_reason')}, "
                    f"output_tokens={(result.get('usage') or {}).get('output_tokens')}, "
                    f"input_tokens={(result.get('usage') or {}).get('input_tokens')}"
                )
            except Exception as e:
                logger.warning(f"Structured output failed, falling back to prompt-based: {e}")
                result = await asyncio.wait_for(
                    providers.generate_completion(**lead_kwargs),
                    timeout=60,
                )
        else:
            logger.info(f"Skipping structured output for non-Anthropic provider: {lead_provider_override}")
            # OpenAI-compatible providers (Kimi) support json_object mode to force valid JSON
            non_anthropic_kwargs = dict(lead_kwargs)
            if lead_provider_override not in ("minimax",):
                non_anthropic_kwargs["response_format"] = {"type": "json_object"}
                logger.info("Using response_format=json_object for OpenAI-compatible Lead")
            result = await asyncio.wait_for(
                providers.generate_completion(**non_anthropic_kwargs),
                timeout=60,
            )

        raw_text = result.get("output_text", "") or ""
        usage = result.get("usage", {}) or {}
        tokens_used = int(usage.get("total_tokens") or 0)
        input_tokens = int(usage.get("input_tokens") or 0)
        output_tokens = int(usage.get("output_tokens") or 0)
        cache_read_tokens = int(usage.get("cache_read_tokens") or 0)
        cache_creation_tokens = int(usage.get("cache_creation_tokens") or 0)
        model_used = result.get("model", "") or ""
        provider_name = result.get("provider", "") or ""

        # Parse response
        cleaned = strip_markdown_json_wrapper(raw_text, expect_json=True).strip()
        try:
            data = json_module.loads(cleaned)
        except json_module.JSONDecodeError:
            # Fallback: extract outermost JSON object from prose text
            # Non-Claude models often wrap JSON in reasoning/markdown text
            data = None
            first_brace = raw_text.find("{")
            last_brace = raw_text.rfind("}")
            if first_brace != -1 and last_brace > first_brace:
                candidate = raw_text[first_brace:last_brace + 1]
                try:
                    data = json_module.loads(candidate)
                    logger.info(f"Lead JSON extracted via bracket fallback (chars {first_brace}-{last_brace})")
                except json_module.JSONDecodeError:
                    pass

            if data is None:
                stop_reason = result.get("stop_reason", "") or result.get("finish_reason", "") or ""
                logger.warning(
                    f"Lead returned non-JSON: error=no valid JSON found, stop_reason={stop_reason}, "
                    f"len={len(raw_text)}, tail=...{raw_text[-200:]}"
                )
                return LeadDecisionResponse(
                    decision_summary="Failed to parse LLM response, defaulting to checkpoint wait",
                    actions=[LeadAction(type="noop")],
                    tokens_used=tokens_used,
                    input_tokens=input_tokens,
                    output_tokens=output_tokens,
                    cache_read_tokens=cache_read_tokens,
                    cache_creation_tokens=cache_creation_tokens,
                    model_used=model_used,
                    provider=provider_name,
                )

        # Guard: some providers may return a list instead of dict
        if isinstance(data, list):
            data = {"decision_summary": "", "user_summary": "", "actions": data}

        decision_summary = str(data.get("decision_summary", ""))[:500]
        user_summary = str(data.get("user_summary", ""))[:500]
        raw_actions = data.get("actions", [])
        actions: List[LeadAction] = []
        for ra in raw_actions:
            if isinstance(ra, dict):
                actions.append(
                    LeadAction(
                        type=ra.get("type", ""),
                        task_id=ra.get("task_id", ""),
                        agent_id=ra.get("agent_id", ""),
                        role=ra.get("role", ""),
                        task_description=ra.get("task_description", ""),
                        to=ra.get("to", ""),
                        content=ra.get("content", ""),
                        model_tier=ra.get("model_tier", ""),
                        create=ra.get("create", []),
                        cancel=ra.get("cancel", []),
                        update=ra.get("update", []),
                        path=ra.get("path", ""),
                        tool=ra.get("tool", ""),
                        tool_params=_parse_tool_params(ra.get("tool_params", "")),
                    )
                )

        # ── Auto-link: inject task_id into spawn_agent by description matching ──
        # Structured output's constrained decoding skips task_id (requires cross-action
        # reasoning to reference revise_plan task IDs). We match descriptions instead.
        _auto_link_task_ids(actions, body.task_list)

        # Diagnostic: log what Lead actually returned
        action_types = [a.type for a in actions]
        agent_state_summary = [(a.agent_id, a.status) for a in body.agent_states]
        logger.info(
            f"Lead response: event={body.event.type} agent={body.event.agent_id} "
            f"decision='{decision_summary[:100]}' "
            f"raw_action_count={len(raw_actions)} action_types={action_types} "
            f"agent_states={agent_state_summary}"
        )

        if not actions:
            logger.warning(
                f"Lead returned empty actions (fallback to noop). "
                f"raw_actions={raw_actions} decision='{decision_summary[:200]}'"
            )
            actions = [LeadAction(type="noop")]

        return LeadDecisionResponse(
            decision_summary=decision_summary,
            user_summary=user_summary,
            actions=actions,
            tokens_used=tokens_used,
            input_tokens=input_tokens,
            output_tokens=output_tokens,
            cache_read_tokens=cache_read_tokens,
            cache_creation_tokens=cache_creation_tokens,
            model_used=model_used,
            provider=provider_name,
        )

    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Lead decide failed: {e}", exc_info=True)
        raise HTTPException(status_code=500, detail=f"Lead decide failed: {str(e)}")
