"""Agent API endpoints for HTTP communication with Agent-Core."""

import logging
from typing import Dict, Any, Optional, List
from fastapi import APIRouter, HTTPException, Request
from pydantic import BaseModel, Field
from fastapi.responses import JSONResponse
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

                role_name = None
                if isinstance(query.context, dict):
                    role_name = query.context.get("role") or query.context.get(
                        "agent_type"
                    )

                # Default to deep_research_agent for research workflows
                if not role_name and isinstance(query.context, dict):
                    if query.context.get("force_research") or query.context.get("workflow_type") == "research":
                        role_name = "deep_research_agent"

                preset = get_role_preset(str(role_name) if role_name else "generalist")

                # Check for system_prompt in context first, then fall back to preset
                system_prompt = None
                if isinstance(query.context, dict) and "system_prompt" in query.context:
                    system_prompt = str(query.context.get("system_prompt"))

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

                # Add language instruction if target_language is specified in context
                if isinstance(query.context, dict) and "target_language" in query.context:
                    target_lang = query.context.get("target_language")
                    if target_lang and target_lang != "English":
                        language_instruction = f"\n\nCRITICAL: Respond in {target_lang}. The user's query is in {target_lang}. You MUST respond in the SAME language. DO NOT translate to English."
                        system_prompt = language_instruction + "\n\n" + system_prompt

                cap_overrides = preset.get("caps") or {}
                # Precedence: caller values win; fall back to role caps only if missing
                # This avoids capping synthesis/composition calls to small role defaults (e.g., 1200)
                # GPT-5 models need more tokens for reasoning + output (default 4096 instead of 2048)
                default_max_tokens = 4096  # Increased for GPT-5 reasoning models
                try:
                    max_tokens = int(query.max_tokens) if query.max_tokens is not None else int(cap_overrides.get("max_tokens") or default_max_tokens)
                    logger.info(f"Agent query max_tokens: query.max_tokens={query.max_tokens}, cap_overrides={cap_overrides.get('max_tokens')}, final={max_tokens}")
                except Exception:
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

            # Add remaining context to system prompt if there's any
            # Exclude large/duplicate fields that are already embedded in the user query
            if context_without_history:
                excluded_keys = {
                    "history",
                    "system_prompt",
                    "available_citations",
                    "previous_response",
                    "reflection_feedback",
                    "previous_results",
                }
                safe_items = [
                    (k, v)
                    for k, v in context_without_history.items()
                    if k not in excluded_keys and v is not None
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
                requested = query.allowed_tools
                preset_allowed = list(preset.get("allowed_tools", []))
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
                tool_results = await _execute_and_format_tools(
                    forced_calls,
                    effective_allowed_tools or [],
                    query.query,
                    request,
                    query.context,
                )

                # Add messages and interpret results with LLM (no tools enabled)
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
                        "content": f"Tool execution result:\n{tool_results}\n\nBased on this result, please provide a clear and complete answer to the original query.",
                    }
                )

                interpretation_result = (
                    await request.app.state.providers.generate_completion(
                        messages=messages,
                        tier=tier,
                        specific_model=model_override,
                        provider_override=provider_override,
                        max_tokens=max_tokens,
                        temperature=temperature,
                        response_format=response_format,
                        tools=None,
                        workflow_id=request.headers.get("X-Workflow-ID")
                        or request.headers.get("x-workflow-id"),
                        agent_id=query.agent_id,
                    )
                )

                response_text = interpretation_result.get("output_text", tool_results)
                total_tokens = interpretation_result.get("usage", {}).get(
                    "total_tokens", 0
                )

                result = {
                    "response": response_text,
                    "tokens_used": total_tokens,
                    "model_used": interpretation_result.get("model", "unknown"),
                }

                return AgentResponse(
                    success=True,
                    response=result["response"],
                    tokens_used=result["tokens_used"],
                    model_used=result["model_used"],
                    provider=interpretation_result.get("provider") or "unknown",
                    finish_reason=interpretation_result.get("finish_reason", "stop"),
                    metadata={
                        "agent_id": query.agent_id,
                        "mode": query.mode,
                        "allowed_tools": effective_allowed_tools,
                        "role": (query.context or {}).get("role")
                        if isinstance(query.context, dict)
                        else None,
                        "finish_reason": interpretation_result.get("finish_reason", "stop"),
                    },
                )

            # When force_tools enabled and tools available, force model to use a tool
            # "any" forces the model to use at least one tool, "auto" only allows tools but doesn't force
            function_call = (
                "any"
                if (force_tools and effective_allowed_tools)
                else ("auto" if effective_allowed_tools else None)
            )

            result_data = await request.app.state.providers.generate_completion(
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
            )

            # Process the response (Responses API shape)
            response_text = result_data.get("output_text", "")

            # Extract tool calls from function_call field (unified provider response format)
            tool_calls_from_output = []
            function_call = result_data.get("function_call")
            if function_call and isinstance(function_call, dict):
                name = function_call.get("name")
                if name:
                    args = function_call.get("arguments") or {}
                    # Handle JSON string arguments from some providers
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
                    logger.warning(
                        f"Skipping malformed tool call without name: {function_call}"
                    )

            # Execute tools if requested
            total_tokens = result_data.get("usage", {}).get("total_tokens", 0)

            if tool_calls_from_output and effective_allowed_tools:
                tool_results = await _execute_and_format_tools(
                    tool_calls_from_output,
                    effective_allowed_tools,
                    query.query,
                    request,
                    query.context,
                )
                if tool_results:
                    # Re-engage LLM to interpret tool results
                    # Add the assistant's tool call and the tool results to conversation
                    messages.append(
                        {
                            "role": "assistant",
                            "content": f"I'll execute the {tool_calls_from_output[0]['name']} tool to help with this task.",
                        }
                    )
                    messages.append(
                        {
                            "role": "user",
                            "content": f"Tool execution result:\n{tool_results}\n\nBased on this result, please provide a clear and complete answer to the original query.",
                        }
                    )

                    # Call LLM again to interpret the tool results
                    logger.info(
                        "Tool execution completed, re-engaging LLM for interpretation"
                    )
                    interpretation_result = (
                        await request.app.state.providers.generate_completion(
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
                    )

                    # Use the interpretation as the final response
                    response_text = interpretation_result.get(
                        "output_text", tool_results
                    )

                    # Add tokens from interpretation pass
                    interpretation_tokens = interpretation_result.get("usage", {}).get(
                        "total_tokens", 0
                    )
                    total_tokens += interpretation_tokens

                    logger.info(
                        f"Tool result interpretation: original_tokens={result_data.get('usage', {}).get('total_tokens', 0)}, "
                        f"interpretation_tokens={interpretation_tokens}, total={total_tokens}"
                    )

            # Optional fallback: tool auto-selection when enabled and no tool calls were chosen
            try:
                tool_autoselect = bool(
                    isinstance(query.context, dict)
                    and query.context.get("tool_autoselect")
                )
            except Exception:
                tool_autoselect = False

            if (
                (not tool_calls_from_output)
                and effective_allowed_tools
                and tool_autoselect
            ):
                try:
                    from ..tools import get_registry

                    registry = get_registry()
                    tools_summary = []
                    for name in effective_allowed_tools:
                        tool = registry.get_tool(name)
                        if not tool:
                            continue
                        schema = tool.get_schema() or {}
                        props = list(
                            (schema.get("parameters", {}) or {})
                            .get("properties", {})
                            .keys()
                        )
                        tools_summary.append(
                            {
                                "name": name,
                                "description": tool.metadata.description,
                                "parameters": props,
                            }
                        )

                    sys = (
                        "You are a tool selection assistant. Read the task and choose suitable tools. "
                        'Return compact JSON only: {"selected_tools": [names], "calls": [{"tool_name": name, "parameters": object}]}. '
                        "Only include tools from the provided list. Prefer minimal arguments."
                    )
                    user_obj = {
                        "task": query.query,
                        "context_keys": list((query.context or {}).keys())[:5],
                        "tools": tools_summary,
                        "max_tools": 1,
                    }
                    selection = await request.app.state.providers.generate_completion(
                        messages=[
                            {"role": "system", "content": sys},
                            {"role": "user", "content": str(user_obj)},
                        ],
                        tier=tier,
                        max_tokens=4096,
                        temperature=0.1,
                        response_format={"type": "json_object"},
                        workflow_id=request.headers.get("X-Workflow-ID")
                        or request.headers.get("x-workflow-id"),
                        agent_id=query.agent_id,
                    )
                    import json as _json

                    raw = selection.get("output_text", "")
                    calls = []
                    try:
                        data = _json.loads(raw)
                        for c in data.get("calls", []) or []:
                            name = c.get("tool_name")
                            if name in effective_allowed_tools:
                                calls.append(
                                    {
                                        "name": name,
                                        "arguments": c.get("parameters") or {},
                                    }
                                )
                    except Exception:
                        calls = []

                    if calls:
                        auto_results = await _execute_and_format_tools(
                            calls,
                            effective_allowed_tools,
                            query.query,
                            request,
                            query.context,
                        )
                        # Add the selection + results to the dialogue and interpret
                        if calls:
                            messages.append(
                                {
                                    "role": "assistant",
                                    "content": f"I'll execute the {calls[0]['name']} tool to help.",
                                }
                            )
                        messages.append(
                            {
                                "role": "user",
                                "content": f"Tool execution result:\n{auto_results}\n\nBased on this result, provide a clear and complete answer.",
                            }
                        )
                        interpretation_result = (
                            await request.app.state.providers.generate_completion(
                                messages=messages,
                                tier=tier,
                                specific_model=model_override,
                                provider_override=provider_override,
                                max_tokens=max_tokens,
                                temperature=temperature,
                                response_format=response_format,
                                tools=None,
                                workflow_id=request.headers.get("X-Workflow-ID")
                                or request.headers.get("x-workflow-id"),
                                agent_id=query.agent_id,
                            )
                        )
                        response_text = interpretation_result.get(
                            "output_text", auto_results
                        )
                        total_tokens = interpretation_result.get("usage", {}).get(
                            "total_tokens", 0
                        )
                        result = {
                            "response": response_text,
                            "tokens_used": total_tokens,
                            "model_used": interpretation_result.get("model", "unknown"),
                        }
                        return AgentResponse(
                            success=True,
                            response=result["response"],
                            tokens_used=result["tokens_used"],
                            model_used=result["model_used"],
                            provider=interpretation_result.get("provider") or "unknown",
                            finish_reason=interpretation_result.get("finish_reason", "stop"),
                            metadata={
                                "agent_id": query.agent_id,
                                "mode": query.mode,
                                "allowed_tools": effective_allowed_tools,
                                "role": (query.context or {}).get("role")
                                if isinstance(query.context, dict)
                                else None,
                                "finish_reason": interpretation_result.get("finish_reason", "stop"),
                                "requested_max_tokens": max_tokens,
                                "input_tokens": interpretation_result.get("usage", {}).get("prompt_tokens")
                                or interpretation_result.get("usage", {}).get("input_tokens"),
                                "output_tokens": interpretation_result.get("usage", {}).get("completion_tokens")
                                or interpretation_result.get("usage", {}).get("output_tokens"),
                            },
                        )
                except Exception as e:
                    logger.warning(f"tool_autoselect failed: {e}")

            result = {
                "response": response_text,
                "tokens_used": total_tokens,
                "model_used": result_data.get("model", "unknown"),
            }

            return AgentResponse(
                success=True,
                response=result["response"],
                tokens_used=result["tokens_used"],
                model_used=result["model_used"],
                provider=result_data.get("provider") or "unknown",
                finish_reason=result_data.get("finish_reason", "stop"),
                metadata={
                    "agent_id": query.agent_id,
                    "mode": query.mode,
                    "allowed_tools": effective_allowed_tools,
                    "role": (query.context or {}).get("role")
                    if isinstance(query.context, dict)
                    else None,
                    "finish_reason": result_data.get("finish_reason", "stop"),
                    "requested_max_tokens": max_tokens,
                    "input_tokens": (result_data.get("usage", {}) or {}).get("input_tokens"),
                    "output_tokens": (result_data.get("usage", {}) or {}).get("output_tokens"),
                    "effective_max_completion": result_data.get("effective_max_completion"),
                },
            )
        else:
            # Use mock provider for testing
            logger.info("Using mock provider (no API keys configured)")
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
                    "role": (query.context or {}).get("role")
                    if isinstance(query.context, dict)
                    else None,
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
) -> str:
    """Execute tool calls and format results into natural language."""
    if not tool_calls:
        return ""

    from ..tools import get_registry

    registry = get_registry()

    formatted_results = []

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

            # Emit TOOL_INVOKED event
            if emitter and wf_id:
                try:
                    emitter.emit(
                        wf_id,
                        "TOOL_INVOKED",
                        agent_id=agent_id,
                        message=f"Executing {tool_name}",
                        payload={"tool": tool_name, "params": args},
                    )
                except Exception:
                    pass

            # Sanitize session context before passing to tools
            if isinstance(context, dict):
                safe_keys = {"session_id", "user_id", "prompt_params"}
                sanitized_context = {k: v for k, v in context.items() if k in safe_keys}
            else:
                sanitized_context = None

            logger.info(
                f"Executing tool {tool_name} with context keys: {list(sanitized_context.keys()) if isinstance(sanitized_context, dict) else 'None'}, args: {args}"
            )
            result = await tool.execute(session_context=sanitized_context, **args)

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
                    formatted_results.append(f"{tool_name} result:\n{formatted_output}")
            else:
                formatted_results.append(f"Error executing {tool_name}: {result.error}")

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
                        },
                    )
                except Exception:
                    pass

        except Exception as e:
            logger.error(f"Error executing tool {tool_name}: {e}")
            formatted_results.append(f"Failed to execute {tool_name}")

    return "\n\n".join(formatted_results) if formatted_results else ""


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

        # System prompt for pure LLM-driven decomposition
        sys = (
            "You are the lead research supervisor planning a comprehensive strategy.\n"
            "IMPORTANT: Process queries in ANY language including English, Chinese, Japanese, Korean, etc.\n\n"
            "# Planning Phase:\n"
            "1. Analyze the research brief carefully\n"
            "2. Break down into clear, SPECIFIC subtasks (avoid acronyms)\n"
            "3. Bias toward SINGLE subtask workflow unless clear parallelization opportunity exists\n"
            "4. Each subtask gets COMPLETE STANDALONE INSTRUCTIONS\n\n"
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
            "- DO NOT create unnecessary dependencies (minimize sequential constraints)\n\n"
            "CRITICAL: Each subtask MUST have these EXACT fields: id, description, dependencies, estimated_tokens, suggested_tools, tool_parameters\n"
            "NEVER return null for subtasks field - always provide at least one subtask.\n\n"
            "TOOL SELECTION GUIDELINES:\n"
            "Default: Use NO TOOLS unless explicitly required.\n\n"
            "USE TOOLS WHEN:\n"
            "- web_search: ONLY for specific real-time data queries\n"
            "- calculator: ONLY for mathematical computations beyond basic arithmetic\n"
            "- file_read: ONLY when explicitly asked to read/open a specific file\n"
            "- python_executor: For executing Python code, data analysis, or programming tasks\n"
            "- code_executor: ONLY for executing provided WASM code (do not use for Python)\n\n"
            "If unsure, default to NO TOOLS. Set suggested_tools to [] for direct LLM response.\n\n"
            "Return ONLY valid JSON with this EXACT structure (no additional text).\n"
            "You MAY include an optional 'parent_area' string field per subtask when grouping by research areas is applicable.\n"
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
            '      "tool_parameters": {}\n'
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
            '      "tool_parameters": {"tool": "web_search", "query": "Apple stock AAPL trend analysis forecast"}\n'
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
            "- For subtasks with non-empty dependencies, DO NOT prefill tool_parameters; set it to {} and avoid placeholders (the agent will use previous_results to construct exact parameters).\n"
            "- Let the semantic meaning of the query guide tool selection\n"
        )

        # Enhance decomposition prompt with tool availability (generic approach)
        decompose_system_prompt = sys  # Start with base decomposition prompt

        # If a role is specified in context, prepend role-specific system prompt
        role_name = None
        if query.context and "role" in query.context:
            role_name = query.context.get("role")
            if role_name:
                from ..roles.presets import get_role_preset, render_system_prompt

                role_preset = get_role_preset(role_name)
                role_system_prompt = role_preset.get("system_prompt", "")
                # Render with context for variable substitution
                rendered_role_prompt = render_system_prompt(
                    role_system_prompt, query.context or {}
                )
                # Prepend role prompt before decomposition instructions
                decompose_system_prompt = rendered_role_prompt + "\n\n" + sys
                logger.info(
                    f"Decompose: Applied role preset '{role_name}' to system prompt"
                )

        # If tools are available, add a generic tool-aware hint
        if available_tools:
            tool_hint = (
                f"\n\nAVAILABLE TOOLS: {', '.join(available_tools)}\n"
                "When the query requires data retrieval, external APIs, or specific operations that match available tools,\n"
                "create tool-based subtasks with suggested_tools and tool_parameters.\n"
                "Set complexity_score >= 0.5 for queries that need tool execution.\n"
            )
            decompose_system_prompt = decompose_system_prompt + tool_hint

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
                logger.debug(f"JSON parse error: {parse_err}")
                # Try to find first {...} in response
                import re

                match = re.search(r"\{.*\}", raw, re.DOTALL)
                if match:
                    try:
                        data = _json.loads(match.group())
                    except Exception:
                        pass

            if not data:
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

                subtask = Subtask(
                    id=st.get("id", f"task-{len(subtasks) + 1}"),
                    description=st.get("description", ""),
                    dependencies=st.get("dependencies", []),
                    estimated_tokens=st.get("estimated_tokens", 300),
                    task_type=task_type,
                    parent_area=str(st.get("parent_area", "")) if st.get("parent_area") is not None else "",
                    suggested_tools=suggested_tools,
                    tool_parameters=tool_params,
                )
                subtasks.append(subtask)
                total_tokens += subtask.estimated_tokens

            # Extract extended fields
            exec_strategy = data.get("execution_strategy", "sequential")
            agent_types = data.get("agent_types", [])
            concurrency_limit = data.get("concurrency_limit", 1)
            token_estimates = data.get("token_estimates", {})

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
