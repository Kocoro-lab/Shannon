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
        default="small", description="Model tier: small, medium, or large"
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
                is_reason_step = query.query.strip().startswith("REASON (")
                if isinstance(query.context, dict) and not is_reason_step:
                    is_research = (
                        query.context.get("force_research")
                        or query.context.get("research_strategy")
                        or query.context.get("research_mode")
                        or query.context.get("workflow_type") == "research"
                    )
                    if is_research:
                        research_instruction = (
                            "\n\nRESEARCH MODE: Do not rely only on web_search snippets. "
                            "For each important question, use web_search to find sources, "
                            "then call web_fetch on the top 3-5 relevant URLs to read the full content before answering. "
                            "This ensures you have comprehensive information, not just summaries."
                            "\n\nTOOL USAGE (CRITICAL):"
                            "\n- Invoke tools ONLY via native function calling (no XML/JSON stubs like <web_fetch> or <function_calls>)."
                            "\n- When you have multiple URLs, prefer web_fetch(urls=[...]) to batch fetch instead of calling web_fetch repeatedly."
                            "\n- Do NOT claim in text which tools/providers you used; the system records tool usage."
                            "\n\nCOMPANY/ENTITY RESEARCH: When researching a company or organization:"
                            "\n- FIRST try web_fetch on the likely official domain (e.g., 'companyname.com', 'companyname.io')"
                            "\n- Try alternative domains: products may have different names (e.g., Ptmind → ptengine.com)"
                            "\n- Search for '[company] site:linkedin.com' or '[company] site:crunchbase.com'"
                            "\n- For Asian companies, try Japanese/Chinese name variants"
                            "\n- If standard searches return only competitors/unrelated results, this indicates a search strategy problem - try direct URL fetches"
                            "\n\nSOURCE EVALUATION AND CONFLICT RESOLUTION:"
                            "\n1. VERIFICATION: Search results are leads, not verified facts. Verify key claims via web_fetch."
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
                    )
                )

                allowed_keys = session_allowed | (task_allowed if is_task_scoped else set())

                safe_items = [
                    (k, v)
                    for k, v in context_without_history.items()
                    if k in allowed_keys and v is not None
                ]
                if safe_items:
                    context_str = "\n".join([f"{k}: {v}" for k, v in safe_items])
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
                            "content": f"I'll execute the {forced_calls[0]['name']} tool to help with this task.",
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

            max_tool_iterations = _get_budget("max_tool_iterations", 3)
            max_total_tool_calls = _get_budget("max_total_tool_calls", 5)
            max_total_tool_output_chars = _get_budget("max_total_tool_output_chars", 60000)
            max_urls_to_fetch = _get_budget("max_urls_to_fetch", 10)
            max_consecutive_tool_failures = _get_budget("max_consecutive_tool_failures", 2)
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
            followup_instruction = (
                "If the information is insufficient, you may call another tool; otherwise, answer the original query."
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
                    if rr.get("tool") in fetch_tools and rr.get("success"):
                        fetch_success = True

                if raw_records and not all(r.get("success") for r in raw_records):
                    consecutive_tool_failures += 1
                else:
                    consecutive_tool_failures = 0

                total_tool_output_chars += len(tool_results or "")
                loop_iterations += 1

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

            # DR forced fetch if needed (search done but no fetch success)
            if (
                research_mode
                and search_urls
                and not fetch_success
                and fetch_tools.intersection(set(effective_allowed_tools or []))
            ):
                try:
                    deduped_urls = []
                    seen = set()
                    for u in search_urls:
                        if u and u not in seen:
                            seen.add(u)
                            deduped_urls.append(u)
                    urls_to_fetch = deduped_urls[:max_urls_to_fetch]
                    if urls_to_fetch:
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
                        if any(r.get("tool") in fetch_tools and r.get("success") for r in policy_raw):
                            fetch_success = True
                except Exception as e:
                    logger.warning(f"DR forced fetch failed: {e}")

            # Final interpretation pass if we executed any tools or budgets were hit
            if tool_execution_records and (
                stop_reason != "no_tool_call"
                or did_forced_fetch
                or not (response_text and str(response_text).strip())
            ):
                interpretation_result = await request.app.state.providers.generate_completion(
                    messages=messages,
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
                response_text = interpretation_result.get("output_text", last_tool_results)
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
                response=result["response"],
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
                response=result["response"],
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
                }
                sanitized_context = {k: v for k, v in context.items() if k in safe_keys}
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
            tool_execution_records.append(
                {
                    "tool": tool_name,
                    "success": bool(result and result.success),
                    "output": _sanitize_payload(
                        result.output if result else None, max_str=2000, max_items=20
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
            '      "suggested_tools": ["web_search"],\n'
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
        RESEARCH_SUPERVISOR_IDENTITY = (
            "You are the lead research supervisor planning a comprehensive strategy.\n"
            "IMPORTANT: Process queries in ANY language including English, Chinese, Japanese, Korean, etc.\n\n"
            "# Planning Phase:\n"
            "1. Analyze the research brief carefully\n"
            "2. Break down into clear, SPECIFIC subtasks (avoid acronyms)\n"
            "3. Prefer PARALLEL subtasks when possible; keep dependencies minimal\n"
            "4. Each subtask gets COMPLETE STANDALONE INSTRUCTIONS\n\n"
            "# Dependency Rules (CRITICAL):\n"
            "- Dependencies are HARD blockers only: add a dependency ONLY if the subtask cannot be executed without the upstream output.\n"
            "- Do NOT add dependencies for convenience, readability, or optional context reuse.\n"
            "- If two subtasks can start from the same public sources/URLs independently, they MUST have empty dependencies [].\n"
            "- Avoid dependency chains (A→B→C) unless truly required; prefer shallow DAGs.\n"
            "- For website/docs analysis queries, default to 3–6 parallel subtasks by section/theme (e.g., overview, architecture, API, tutorials) WITHOUT dependencies.\n"
            "- If a discovery/index step is needed (e.g., find navigation/TOC paths), make it ONE small upstream task and keep other tasks independent unless they truly require its output.\n\n"
            "# Task Contract Requirements (MANDATORY):\n"
            "Every research subtask MUST include ALL of the following contract fields:\n"
            "- output_format: {type, required_fields, optional_fields}\n"
            "- source_guidance: {required: [...], optional: [...], avoid: [...]}\n"
            "- search_budget: {max_queries, max_fetches}\n"
            "- boundaries: {in_scope: [...], out_of_scope: [...]}\n\n"
            "CRITICAL: If you lack information to fill a contract field, use defaults:\n"
            "- output_format: {type: 'narrative', required_fields: [], optional_fields: []}\n"
            "- source_guidance: {required: ['official', 'aggregator'], optional: ['news'], avoid: ['social']}\n"
            "- search_budget: {max_queries: 10, max_fetches: 20}\n"
            "- boundaries: {in_scope: [...explicitly list...], out_of_scope: [...at least 1 exclusion...]}\n\n"
            "Subtasks missing ANY contract field will be considered INVALID output.\n\n"
            "# Detailed Task Description Requirements:\n"
            "Each subtask description MUST include ALL elements below, using HIGH-DENSITY format (≤5 lines, 1 sentence per element):\n"
            "1. **Objective** (1 sentence): Single most important goal\n"
            "2. **Starting Points** (1 sentence): Specific URLs/paths/sites/queries to try first (be concrete)\n"
            "3. **Key Questions** (1 sentence): 2-3 questions to answer\n"
            "4. **Scope** (1 sentence): What to INCLUDE + what to EXCLUDE\n"
            "5. **Tools** (1 sentence): Tool priority order\n\n"
            "GOOD EXAMPLE (high-density, 5 lines):\n"
            "\"Research TSMC's current production capacity. Start: tsmc.com/ir quarterly report, search 'TSMC fab construction 2025'. "
            "Answer: (1) current wafer capacity, (2) new fabs, (3) 2026 projection. "
            "Include: manufacturing capacity only. Exclude: financial performance. "
            "Tools: web_fetch (investor reports) → web_search (news).\"\n\n"
            "BAD EXAMPLES:\n"
            "- Too vague: \"Research TSMC\"\n"
            "- Too verbose: Long paragraphs explaining background, multiple unrelated points\n\n"
            "# Research Breakdown Guidelines:\n"
            "- Simple queries (factual, narrow scope): 1-2 subtasks, complexity_score < 0.3\n"
            "- Complex queries (multi-faceted, analytical): 3-5 subtasks, complexity_score >= 0.3\n"
            "- Ensure logical dependencies are clear\n"
            "- Prioritize high-value information sources\n"
            "- Quality over quantity: Focus on tasks yielding authoritative, relevant sources\n\n"
            "# Scaling Rules (Task Count by Query Type):\n"
            "- **Comparison queries** ('compare A vs B'): Create ONE subtask per entity being compared\n"
            "  Example: 'Compare LangChain vs AutoGen vs CrewAI' → 3 subtasks (one per framework)\n"
            "- **List/ranking queries** ('top 10 X', 'best Y'): Use SINGLE comprehensive subtask\n"
            "  Example: 'List top 10 AI frameworks' → 1 subtask with broad search scope\n"
            "- **Analysis queries** ('analyze market for X'): Split by major dimensions\n"
            "  Example: 'Analyze EV market' → [market size, key players, trends, regulations]\n"
            "- **Explanation queries** ('what is X', 'how does Y work'): Usually 1-2 subtasks\n"
            "  Example: 'Explain quantum computing' → 1 subtask (or 2 if very complex: principles + applications)\n\n"
            "**Anti-patterns to avoid:**\n"
            "- DO NOT create subtasks that overlap significantly in scope\n"
            "- DO NOT split tasks that are too granular (combine related questions)\n"
            "- DO NOT create unnecessary dependencies (minimize sequential constraints)\n"
            "- NEVER create more than 10 subtasks unless strictly necessary (more subtasks = more overhead = slower results)\n"
            "- If task seems to require many subtasks, RESTRUCTURE to consolidate similar topics\n\n"
            "NOTE: You MAY include an optional 'parent_area' string field per subtask when grouping by research areas is applicable.\n\n"
        )

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
