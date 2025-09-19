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
    term_matches = sum(1 for term in query_terms if term in content_text) / len(query_terms) if query_terms else 0

    # Weight the scores
    relevance = (title_score * 0.4) + (term_matches * 0.6)

    # Boost if source is official or highly relevant
    url = result.get("url", "").lower()
    if any(term in url for term in query_terms):
        relevance += 0.2

    return min(relevance, 1.0)


def filter_relevant_results(query: str, results: List[Dict[str, Any]], threshold: float = 0.3) -> List[Dict[str, Any]]:
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


class AgentQuery(BaseModel):
    """Query from an agent."""
    query: str = Field(..., description="The query or task description")
    context: Optional[Dict[str, Any]] = Field(default_factory=dict, description="Context for the query")
    agent_id: Optional[str] = Field(default="default", description="Agent identifier")
    mode: Optional[str] = Field(default="standard", description="Execution mode: simple, standard, or complex")
    tools: Optional[List[str]] = Field(default_factory=list, description="Available tools for this query")
    max_tokens: Optional[int] = Field(default=2048, description="Maximum tokens for response")
    temperature: Optional[float] = Field(default=0.7, description="Temperature for generation")
    model_tier: Optional[str] = Field(default="small", description="Model tier: small, medium, or large")


class AgentResponse(BaseModel):
    """Response to an agent query."""
    success: bool = Field(..., description="Whether the query was processed successfully")
    response: str = Field(..., description="The generated response")
    tokens_used: int = Field(..., description="Number of tokens used")
    model_used: str = Field(..., description="Model that was used")
    metadata: Dict[str, Any] = Field(default_factory=dict, description="Additional metadata")


class MockProvider:
    """Mock LLM provider for testing without API keys."""
    
    def __init__(self):
        self.responses = {
            "hello": "Hello! I'm a mock agent ready to help with your task.",
            "test": "This is a test response from the mock provider.",
            "analyze": "I've analyzed your request. The complexity is moderate and can be handled with standard execution mode.",
            "default": "I understand your request. Here's my mock response for testing purposes."
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
            "model_used": "mock-model-v1"
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
        
        # Check if we have real providers configured
        if hasattr(request.app.state, 'providers') and request.app.state.providers.is_configured():
            # Use real provider - convert query to messages format
            # Roles v1: choose system prompt from role preset if provided in context
            try:
                from ..roles.presets import get_role_preset
                role_name = None
                if isinstance(query.context, dict):
                    role_name = query.context.get("role") or query.context.get("agent_type")
                preset = get_role_preset(str(role_name) if role_name else "generalist")
                system_prompt = str(preset.get("system_prompt") or "You are a helpful AI assistant.")
                cap_overrides = preset.get("caps") or {}
                # Allow role caps to softly override token/temperature bounds if caller didn't specify
                max_tokens = int(cap_overrides.get("max_tokens") or query.max_tokens)
                temperature = float(cap_overrides.get("temperature") or query.temperature)
            except Exception:
                system_prompt = "You are a helpful AI assistant."
                max_tokens = query.max_tokens
                temperature = query.temperature

            messages = [
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": query.query}
            ]
            
            # Add context if provided
            if query.context:
                context_str = "\n".join([f"{k}: {v}" for k, v in query.context.items()])
                messages[0]["content"] += f"\n\nContext:\n{context_str}"
            
            # Get the appropriate model tier
            from ..providers.base import ModelTier
            tier_map = {
                "small": ModelTier.SMALL,
                "medium": ModelTier.MEDIUM,
                "large": ModelTier.LARGE
            }
            tier = tier_map.get(query.model_tier, ModelTier.SMALL)
            
            # Generate completion with tools if specified
            tools_param = None
            if query.tools:
                # Convert tool names to OpenAI-style tool definitions
                tools_param = []
                for tool_name in query.tools:
                    if tool_name == "web_search":
                        tools_param.append({
                            "type": "function",
                            "function": {
                                "name": "web_search",
                                "description": "Search the web for information",
                                "parameters": {
                                    "type": "object",
                                    "properties": {
                                        "query": {"type": "string", "description": "Search query"},
                                        "max_results": {"type": "integer", "description": "Max results", "minimum": 1, "maximum": 10}
                                    },
                                    "required": ["query"]
                                }
                            }
                        })
                    elif tool_name == "calculator":
                        tools_param.append({
                            "type": "function",
                            "function": {
                                "name": "calculator",
                                "description": "Evaluate a mathematical expression",
                                "parameters": {
                                    "type": "object",
                                    "properties": {
                                        "expression": {"type": "string", "description": "Math expression to evaluate"}
                                    },
                                    "required": ["expression"]
                                }
                            }
                        })
                    elif tool_name == "file_read":
                        tools_param.append({
                            "type": "function",
                            "function": {
                                "name": "file_read",
                                "description": "Read contents of a file (sandboxed)",
                                "parameters": {
                                    "type": "object",
                                    "properties": {
                                        "path": {"type": "string", "description": "Path to the file"},
                                        "encoding": {"type": "string", "enum": ["utf-8", "ascii", "latin-1"]}
                                    },
                                    "required": ["path"]
                                }
                            }
                        })
                    elif tool_name == "code_executor":
                        tools_param.append({
                            "type": "function",
                            "function": {
                                "name": "code_executor",
                                "description": "Execute WebAssembly (WASM) bytecode in a secure sandbox. IMPORTANT: This tool ONLY executes pre-compiled WASM binary modules (.wasm files), NOT source code like Python/JavaScript/etc. To execute Python code: 1) First compile it to WASM using py2wasm or similar tools, 2) Then provide the compiled WASM bytecode via wasm_base64 (as a base64-encoded string) or wasm_path (file path to .wasm file). Never pass 'language' or 'code' parameters to this tool.",
                                "parameters": {
                                    "type": "object",
                                    "properties": {
                                        "wasm_base64": {"type": "string", "description": "Base64-encoded WASM bytecode. This must be the binary content of a .wasm file encoded as base64, NOT source code."},
                                        "wasm_path":   {"type": "string", "description": "Absolute file path to a compiled .wasm module on disk (e.g., /tmp/module.wasm)"},
                                        "stdin":       {"type": "string", "description": "Optional text input to pass to the WASM module's stdin"}
                                    },
                                    "anyOf": [
                                        {"required": ["wasm_base64"]},
                                        {"required": ["wasm_path"]}
                                    ]
                                }
                            }
                        })
                    elif tool_name == "python_executor":
                        tools_param.append({
                            "type": "function",
                            "function": {
                                "name": "python_executor",
                                "description": "Execute Python code in a secure WASI sandbox environment. Supports full Python 3.11 standard library. Use this tool to run Python scripts, perform calculations, data processing, or any Python programming task. IMPORTANT: Always use print() statements to output results.",
                                "parameters": {
                                    "type": "object",
                                    "properties": {
                                        "code": {"type": "string", "description": "Python source code to execute. Must include print() statements to produce visible output."},
                                        "session_id": {"type": "string", "description": "Optional session ID for maintaining persistent state across executions"},
                                        "stdin": {"type": "string", "description": "Optional input data to provide via stdin to the Python script"}
                                    },
                                    "required": ["code"]
                                }
                            }
                        })

            result_data = await request.app.state.providers.generate_completion(
                messages=messages,
                tier=tier,
                max_tokens=max_tokens,
                temperature=temperature,
                tools=tools_param
            )
            
            # Process the response (Responses API shape)
            response_text = result_data.get("output_text", "")

            # Extract tool calls from Responses output items if any
            tool_calls_from_output = []
            try:
                for item in (result_data.get("output") or []):
                    if isinstance(item, dict) and item.get("type") == "tool_call":
                        name = item.get("name")
                        args = item.get("arguments") or {}
                        tool_calls_from_output.append({"name": name, "arguments": args})
            except Exception:
                tool_calls_from_output = []

            # Execute tools if requested
            if tool_calls_from_output and query.tools:
                tool_results = await _execute_and_format_tools(
                    tool_calls_from_output,
                    query.tools,
                    query.query,
                    request
                )
                if tool_results:
                    # Append formatted tool results to response
                    response_text = f"{response_text}\n\n{tool_results}" if response_text else tool_results
            
            result = {
                "response": response_text,
                "tokens_used": result_data.get("usage", {}).get("total_tokens", 0),
                "model_used": result_data.get("model", "unknown")
            }
        else:
            # Use mock provider for testing
            logger.info("Using mock provider (no API keys configured)")
            result = await mock_provider.generate(
                query.query,
                context=query.context,
                max_tokens=query.max_tokens,
                temperature=query.temperature
            )
        
        return AgentResponse(
            success=True,
            response=result["response"],
            tokens_used=result["tokens_used"],
            model_used=result["model_used"],
            metadata={
                "agent_id": query.agent_id,
                "mode": query.mode,
                "tools": query.tools,
                "role": (query.context or {}).get("role") if isinstance(query.context, dict) else None,
            }
        )
        
    except Exception as e:
        logger.error(f"Error processing agent query: {e}")
        raise HTTPException(status_code=500, detail=str(e))


async def _execute_and_format_tools(tool_calls: List[Dict[str, Any]], allowed_tools: List[str], query: str = "", request=None) -> str:
    """Execute tool calls and format results into natural language."""
    if not tool_calls:
        return ""
    
    from ..tools import get_registry
    registry = get_registry()
    
    formatted_results = []
    
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
            
            result = await tool.execute(**args)
            
            if result.success:
                # Format based on tool type
                if tool_name == "web_search":
                    # Format web search results with full content for AI consumption
                    if isinstance(result.output, list) and result.output:
                        # Filter results by relevance to the query
                        query = args.get("query", "")
                        filtered_results = filter_relevant_results(query, result.output) if query else result.output[:5]

                        # Include full content for AI to synthesize
                        search_results = []
                        for i, item in enumerate(filtered_results, 1):
                            title = item.get("title", "")
                            content = item.get("content", "")
                            snippet = item.get("snippet", "")
                            url = item.get("url", "")
                            date = item.get("published_date", "")

                            # Clean HTML entities
                            title = html.unescape(title)
                            content = html.unescape(content) if content else html.unescape(snippet)

                            if title and url:
                                # Use full content if available, otherwise use snippet
                                text_content = content[:1500] if content else snippet[:500]

                                result_text = f"**{title}**"
                                if date:
                                    result_text += f" ({date[:10]})"
                                result_text += f"\n{text_content}"
                                if len(content) > 1500 or len(snippet) > 500:
                                    result_text += "..."
                                result_text += f"\nSource: {url}\n"

                                search_results.append(result_text)

                        if search_results:
                            # Return formatted results with content for the orchestrator to synthesize
                            # The orchestrator's synthesis activity will handle creating the final answer
                            formatted = "Web Search Results:\n\n" + "\n---\n\n".join(search_results)
                            formatted_results.append(formatted)
                        else:
                            formatted_results.append("No relevant search results found.")
                    elif result.output:
                        formatted_results.append(f"Search results: {result.output}")
                elif tool_name == "calculator":
                    formatted_results.append(f"Calculation result: {result.output}")
                else:
                    # Generic formatting for other tools
                    formatted_results.append(f"{tool_name} result: {result.output}")
            else:
                formatted_results.append(f"Error executing {tool_name}: {result.error}")
                
        except Exception as e:
            logger.error(f"Error executing tool {tool_name}: {e}")
            formatted_results.append(f"Failed to execute {tool_name}")
    
    return "\n\n".join(formatted_results) if formatted_results else ""


class Subtask(BaseModel):
    id: str
    description: str
    dependencies: List[str] = []
    estimated_tokens: int = 0
    # LLM-native tool selection
    suggested_tools: List[str] = Field(default_factory=list, description="Tools suggested by LLM for this subtask")
    tool_parameters: Dict[str, Any] = Field(default_factory=dict, description="Pre-structured parameters for tool execution")

class DecompositionResponse(BaseModel):
    mode: str
    complexity_score: float
    subtasks: List[Subtask]
    total_estimated_tokens: int
    # Extended planning schema (plan_schema_v2)
    execution_strategy: str = Field(default="parallel", description="parallel|sequential|hybrid")
    agent_types: List[str] = Field(default_factory=list)
    concurrency_limit: int = Field(default=5)
    token_estimates: Dict[str, int] = Field(default_factory=dict)
    # Cognitive routing fields for intelligent strategy selection
    cognitive_strategy: str = Field(default="decompose", description="direct|decompose|exploratory|react|research")
    confidence: float = Field(default=0.8, ge=0.0, le=1.0, description="Confidence in strategy selection")
    fallback_strategy: str = Field(default="decompose", description="Fallback if primary strategy fails")


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
        providers = getattr(request.app.state, 'providers', None)
        settings = getattr(request.app.state, 'settings', None)

        if not providers or not providers.is_configured():
            logger.error("LLM providers not configured")
            raise HTTPException(
                status_code=503,
                detail="LLM service not configured"
            )

        from ..providers.base import ModelTier

        # System prompt for pure LLM-driven decomposition
        sys = (
            "You are a planning assistant. Analyze the user's task and determine if it needs decomposition.\n"
            "IMPORTANT: Process queries in ANY language including English, Chinese, Japanese, Korean, etc.\n"
            "For SIMPLE queries (single action, direct answer, or basic calculation), set complexity_score < 0.3 and provide a single subtask.\n"
            "For COMPLEX queries (multiple steps, dependencies), set complexity_score >= 0.3 and decompose into multiple subtasks.\n\n"

            "CRITICAL: Each subtask MUST have these EXACT fields: id, description, dependencies, estimated_tokens, suggested_tools, tool_parameters\n"
            "NEVER return null for subtasks field - always provide at least one subtask.\n\n"

            "TOOL SELECTION GUIDELINES:\n"
            "Default: Use NO TOOLS unless explicitly required. Most queries can be answered with your knowledge.\n\n"
            "USE TOOLS ONLY WHEN:\n"
            "- web_search: ONLY for specific real-time data queries like:\n"
            "  * Current stock prices, market data, or financial metrics\n"
            "  * Recent news events or breaking developments\n"
            "  * Current weather, sports scores, or time-sensitive information\n"
            "  * NOT for: general knowledge, concepts, explanations, analysis, or guidance\n"
            "- calculator: ONLY for complex mathematical computations beyond basic arithmetic\n"
            "- file_read: ONLY when explicitly asked to read/open a specific file\n"
            "- python_executor: For executing Python code, data analysis, or programming tasks\n"
            "- code_executor: ONLY for executing provided WASM code (do not use for Python)\n\n"
            "DO NOT USE TOOLS FOR:\n"
            "- General knowledge questions or explanations\n"
            "- Analysis, recommendations, or strategic advice\n"
            "- Monitoring guidance or best practices\n"
            "- Conceptual or theoretical discussions\n"
            "- When you can provide a thoughtful response with your training data\n\n"
            "If unsure, default to NO TOOLS. Set suggested_tools to [] for direct LLM response.\n\n"

            "Return ONLY valid JSON with this EXACT structure (no additional text):\n"
            "{\n"
            "  \"mode\": \"simple\",\n"
            "  \"complexity_score\": 0.2,\n"
            "  \"subtasks\": [\n"
            "    {\n"
            "      \"id\": \"task-1\",\n"
            "      \"description\": \"Task description\",\n"
            "      \"dependencies\": [],\n"
            "      \"estimated_tokens\": 500,\n"
            "      \"suggested_tools\": [],\n"
            "      \"tool_parameters\": {}\n"
            "    }\n"
            "  ],\n"
            "  \"execution_strategy\": \"sequential\",\n"
            "  \"concurrency_limit\": 1,\n"
            "  \"token_estimates\": {\"task-1\": 500},\n"
            "  \"total_estimated_tokens\": 500\n"
            "}\n\n"

            "Available tools:\n"
            "- python_executor: Execute Python code in sandbox (params: tool, code)\n"
            "- code_executor: Execute WASM bytecode (NOTE: Do NOT suggest this - it's for pre-compiled WASM only)\n"
            "- calculator: Perform calculations (params: tool, expression)\n"
            "- web_search: Search the web (params: tool, query)\n"
            "- file_reader: Read files (params: tool, path)\n\n"

            "IMPORTANT: Use python_executor for Python code execution tasks. Never suggest code_executor unless user\n"
            "explicitly provides WASM bytecode. For general code writing (without execution), handle directly.\n\n"

            "Example for Chinese stock query '分析苹果股票走势':\n"
            "{\n"
            "  \"mode\": \"standard\",\n"
            "  \"complexity_score\": 0.5,\n"
            "  \"subtasks\": [\n"
            "    {\n"
            "      \"id\": \"task-1\",\n"
            "      \"description\": \"Search for Apple stock trend analysis\",\n"
            "      \"dependencies\": [],\n"
            "      \"estimated_tokens\": 800,\n"
            "      \"suggested_tools\": [\"web_search\"],\n"
            "      \"tool_parameters\": {\"tool\": \"web_search\", \"query\": \"Apple stock AAPL trend analysis forecast\"}\n"
            "    }\n"
            "  ],\n"
            "  \"execution_strategy\": \"sequential\",\n"
            "  \"concurrency_limit\": 1,\n"
            "  \"token_estimates\": {\"task-1\": 800},\n"
            "  \"total_estimated_tokens\": 800\n"
            "}\n\n"

            "Rules:\n"
            "- mode: must be \"simple\", \"standard\", or \"complex\"\n"
            "- complexity_score: number between 0.0 and 1.0\n"
            "- dependencies: array of task ID strings or empty array []\n"
            "- suggested_tools: empty array [] if no tools needed, otherwise list tool names\n"
            "- tool_parameters: empty object {} if no tools, otherwise parameters for the tool\n"
            "- For subtasks with non-empty dependencies, DO NOT prefill tool_parameters; set it to {} and avoid placeholders (the agent will use previous_results to construct exact parameters).\n"
            "- Let the semantic meaning of the query guide tool selection\n"
        )

        ctx_keys = list((query.context or {}).keys())[:5]
        tools = ",".join(query.tools or [])
        user = f"Query: {query.query}\nContext keys: {ctx_keys}\nAvailable tools: {tools}"

        try:
            result = await providers.generate_completion(
                messages=[{"role": "system", "content": sys}, {"role": "user", "content": user}],
                tier=ModelTier.SMALL,
                max_tokens=400,
                temperature=0.1,
                response_format={"type": "json_object"},
                specific_model=(settings.decomposition_model_id if settings and settings.decomposition_model_id else None),
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
                match = re.search(r'\{.*\}', raw, re.DOTALL)
                if match:
                    try:
                        data = _json.loads(match.group())
                    except:
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
                logger.warning(f"Invalid decomposition: complexity={score} but no subtasks. Creating fallback subtask.")
                # Create a generic subtask without pattern matching - let LLM decide tools
                subtasks_raw = [{
                    "id": "task-1",
                    "description": query.query[:200],
                    "dependencies": [],
                    "estimated_tokens": 500,
                    "suggested_tools": [],
                    "tool_parameters": {}
                }]

            for st in subtasks_raw:
                if not isinstance(st, dict):
                    continue

                # Extract tool information if present
                suggested_tools = st.get("suggested_tools", [])
                tool_params = st.get("tool_parameters", {})
                deps = st.get("dependencies", []) or []

                # Log tool analysis by LLM
                if suggested_tools:
                    logger.info(f"LLM tool analysis: suggested_tools={suggested_tools}, tool_parameters={tool_params}")
                    # For dependent subtasks, clear tool_parameters to avoid placeholders
                    if isinstance(deps, list) and len(deps) > 0:
                        tool_params = {}
                    else:
                        # Add the tool name to parameters if not present and tools are suggested
                        if suggested_tools and "tool" not in tool_params and len(suggested_tools) > 0:
                            tool_params["tool"] = suggested_tools[0]

                subtask = Subtask(
                    id=st.get("id", f"task-{len(subtasks)+1}"),
                    description=st.get("description", ""),
                    dependencies=st.get("dependencies", []),
                    estimated_tokens=st.get("estimated_tokens", 300),
                    suggested_tools=suggested_tools,
                    tool_parameters=tool_params
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
            )

        except Exception as e:
            logger.error(f"LLM decomposition failed: {e}")
            # Return error instead of using heuristics
            raise HTTPException(
                status_code=503,
                detail=f"LLM service unavailable for decomposition: {str(e)}"
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
            "small": ["mock-model-v1", "gpt-3.5-turbo"],
            "medium": ["gpt-4"],
            "large": ["gpt-4-turbo"]
        },
        "default_tier": "small",
        "mock_enabled": True
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
