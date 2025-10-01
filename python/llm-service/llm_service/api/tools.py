"""
Tools API endpoints for Shannon platform
"""

from fastapi import APIRouter, HTTPException, Depends, Request
import os
import yaml
from pydantic import BaseModel, Field
from typing import List, Dict, Any, Optional
import logging

from ..tools import ToolRegistry, get_registry
from ..tools.mcp import create_mcp_tool_class
from ..tools.openapi_tool import load_openapi_tools_from_config
from ..mcp_client import HttpStatelessClient
from ..tools.builtin import (
    WebSearchTool,
    CalculatorTool,
    FileReadTool,
    FileWriteTool,
    PythonWasiExecutorTool,
)

logger = logging.getLogger(__name__)
router = APIRouter(prefix="/tools", tags=["tools"])

# Simple in-memory TTL cache for tool selection
_SELECT_CACHE: Dict[str, Dict[str, Any]] = {}
_SELECT_TTL_SECONDS = 300  # 5 minutes


class ToolExecuteRequest(BaseModel):
    """Request to execute a tool"""
    tool_name: str = Field(..., description="Name of the tool to execute")
    parameters: Dict[str, Any] = Field(..., description="Tool parameters")
    
    class Config:
        schema_extra = {
            "example": {
                "tool_name": "calculator",
                "parameters": {
                    "expression": "2 + 2"
                }
            }
        }


class ToolExecuteResponse(BaseModel):
    """Response from tool execution"""
    success: bool
    output: Any
    error: Optional[str] = None
    metadata: Optional[Dict[str, Any]] = None
    execution_time_ms: Optional[int] = None


class ToolSchemaResponse(BaseModel):
    """Tool schema information"""
    name: str
    description: str
    parameters: Dict[str, Any]


class ToolSelectRequest(BaseModel):
    """Request to select tools for a task"""
    task: str = Field(..., description="Natural language task")
    context: Optional[Dict[str, Any]] = Field(default=None, description="Optional context map")
    exclude_dangerous: bool = Field(default=True)
    max_tools: int = Field(default=2, ge=0, le=8)


class ToolCall(BaseModel):
    tool_name: str
    parameters: Dict[str, Any] = Field(default_factory=dict)


class ToolSelectResponse(BaseModel):
    selected_tools: List[str] = Field(default_factory=list)
    calls: List[ToolCall] = Field(default_factory=list)
    provider_used: Optional[str] = None


class MCPParamDef(BaseModel):
    name: str
    type: str = Field(default="object")
    description: Optional[str] = None
    required: Optional[bool] = False
    default: Optional[Any] = None


class MCPRegisterRequest(BaseModel):
    name: str = Field(..., description="Name to register the tool as")
    url: str = Field(..., description="MCP HTTP endpoint URL")
    func_name: str = Field(..., description="Remote function name")
    description: Optional[str] = Field(default="MCP remote function")
    category: Optional[str] = Field(default="mcp")
    headers: Optional[Dict[str, str]] = Field(default=None)
    parameters: Optional[List[MCPParamDef]] = Field(default=None)


class MCPRegisterResponse(BaseModel):
    success: bool
    tool_name: str
    message: Optional[str] = None


class OpenAPIRegisterRequest(BaseModel):
    name: str = Field(..., description="Name to register the tool collection as")
    spec_url: Optional[str] = Field(default=None, description="URL to OpenAPI spec (JSON or YAML)")
    spec_inline: Optional[str] = Field(default=None, description="Inline OpenAPI spec (JSON or YAML)")
    auth_type: str = Field(default="none", description="Auth type: none|api_key|bearer|basic")
    auth_config: Optional[Dict[str, str]] = Field(default=None, description="Auth configuration")
    category: Optional[str] = Field(default="api", description="Tool category")
    base_cost_per_use: Optional[float] = Field(default=0.001, description="Cost per operation")
    operations: Optional[List[str]] = Field(default=None, description="Filter to specific operationIds")
    tags: Optional[List[str]] = Field(default=None, description="Filter by tags")
    base_url: Optional[str] = Field(default=None, description="Override base URL from spec")
    rate_limit: Optional[int] = Field(default=30, description="Requests per minute")
    timeout_seconds: Optional[float] = Field(default=30.0, description="Request timeout in seconds")
    max_response_bytes: Optional[int] = Field(default=10 * 1024 * 1024, description="Maximum response size in bytes")


class OpenAPIRegisterResponse(BaseModel):
    success: bool
    collection_name: str
    operations_registered: List[str] = Field(default_factory=list)
    message: Optional[str] = None
    # Echo back selected config values for verification
    rate_limit: Optional[int] = None
    timeout_seconds: Optional[float] = None
    max_response_bytes: Optional[int] = None


class OpenAPIValidateRequest(BaseModel):
    spec_url: Optional[str] = Field(default=None, description="URL to OpenAPI spec")
    spec_inline: Optional[str] = Field(default=None, description="Inline OpenAPI spec")


class OpenAPIValidateResponse(BaseModel):
    valid: bool
    operations_count: int
    operations: List[Dict[str, str]] = Field(default_factory=list)
    base_url: Optional[str] = None
    errors: List[str] = Field(default_factory=list)


def _load_mcp_tools_from_config():
    """Load MCP tool definitions from config file"""
    config_path = os.getenv("CONFIG_PATH", "/app/config/shannon.yaml")
    if not os.path.exists(config_path):
        logger.debug(f"Config file not found at {config_path}, skipping MCP config load")
        return

    try:
        with open(config_path, 'r') as f:
            config = yaml.safe_load(f)

        mcp_tools = config.get('mcp_tools', {})
        registry = get_registry()

        for tool_name, tool_config in mcp_tools.items():
            if not tool_config or not tool_config.get('enabled', True):
                continue

            # Expand env vars in headers
            headers = tool_config.get('headers', {})
            for key, value in headers.items():
                if isinstance(value, str) and value.startswith('${') and value.endswith('}'):
                    env_var = value[2:-1]
                    headers[key] = os.getenv(env_var, '')

            # Convert parameters to expected format
            params = tool_config.get('parameters', [])
            if params and isinstance(params, list):
                # Already in list format from YAML
                pass

            tool_class = create_mcp_tool_class(
                name=tool_name,
                url=tool_config['url'],
                func_name=tool_config['func_name'],
                description=tool_config.get('description', f'MCP tool {tool_name}'),
                category=tool_config.get('category', 'mcp'),
                headers=headers if headers else None,
                parameters=params if params else None,
            )

            registry.register(tool_class, override=True)
            logger.info(f"Loaded MCP tool from config: {tool_name}")

    except Exception as e:
        logger.error(f"Failed to load MCP tools from config: {e}")


def _load_openapi_tools_from_config():
    """Load OpenAPI tool definitions from config file"""
    config_path = os.getenv("CONFIG_PATH", "/app/config/shannon.yaml")
    if not os.path.exists(config_path):
        logger.debug(f"Config file not found at {config_path}, skipping OpenAPI config load")
        return

    try:
        with open(config_path, 'r') as f:
            config = yaml.safe_load(f)

        tool_classes = load_openapi_tools_from_config(config)
        registry = get_registry()

        for tool_class in tool_classes:
            try:
                registry.register(tool_class, override=True)
            except Exception as e:
                logger.error(f"Failed to register OpenAPI tool {tool_class.__name__}: {e}")

        if tool_classes:
            logger.info(f"Loaded {len(tool_classes)} OpenAPI tools from config")

    except Exception as e:
        logger.error(f"Failed to load OpenAPI tools from config: {e}")


@router.on_event("startup")
async def startup_event():
    """Initialize and register built-in tools on startup"""
    registry = get_registry()

    # Register built-in tools
    tools_to_register = [
        WebSearchTool,
        CalculatorTool,
        FileReadTool,
        FileWriteTool,
        PythonWasiExecutorTool,
    ]

    for tool_class in tools_to_register:
        try:
            registry.register(tool_class)
            logger.info(f"Registered tool: {tool_class.__name__}")
        except Exception as e:
            logger.error(f"Failed to register {tool_class.__name__}: {e}")

    # Load MCP tools from config
    _load_mcp_tools_from_config()

    # Load OpenAPI tools from config
    _load_openapi_tools_from_config()

    logger.info(f"Tool registry initialized with {len(registry.list_tools())} tools")


@router.get("/list", response_model=List[str])
async def list_tools(
    category: Optional[str] = None,
    exclude_dangerous: bool = True,
) -> List[str]:
    """
    List available tools
    
    Args:
        category: Filter by category (e.g., "search", "calculation", "file")
        exclude_dangerous: Whether to exclude dangerous tools
    """
    registry = get_registry()
    
    if category:
        # Filter by category
        tools = registry.list_tools_by_category(category)
    else:
        tools = registry.list_tools()
    
    # Apply danger filter if requested
    if exclude_dangerous:
        filtered = []
        for tool_name in tools:
            tool = registry.get_tool(tool_name)
            if tool and not tool.metadata.dangerous:
                filtered.append(tool_name)
        tools = filtered
    
    return tools


@router.get("/categories", response_model=List[str])
async def list_categories() -> List[str]:
    """List all tool categories"""
    registry = get_registry()
    return registry.list_categories()


@router.get("/{tool_name}/schema", response_model=ToolSchemaResponse)
async def get_tool_schema(tool_name: str) -> ToolSchemaResponse:
    """
    Get schema for a specific tool
    
    Args:
        tool_name: Name of the tool
    """
    registry = get_registry()
    schema = registry.get_tool_schema(tool_name)
    
    if not schema:
        raise HTTPException(status_code=404, detail=f"Tool '{tool_name}' not found")
    
    return ToolSchemaResponse(
        name=schema["name"],
        description=schema["description"],
        parameters=schema["parameters"]
    )


@router.get("/schemas", response_model=List[ToolSchemaResponse])
async def get_all_schemas(
    category: Optional[str] = None,
    exclude_dangerous: bool = True,
) -> List[ToolSchemaResponse]:
    """
    Get schemas for all available tools
    
    Args:
        category: Filter by category
        exclude_dangerous: Whether to exclude dangerous tools
    """
    registry = get_registry()
    
    # Get filtered tool names
    if category:
        tool_names = registry.list_tools_by_category(category)
    else:
        tool_names = registry.list_tools()
    
    # Build schemas
    schemas = []
    for tool_name in tool_names:
        tool = registry.get_tool(tool_name)
        if not tool:
            continue
        
        # Skip dangerous tools if requested
        if exclude_dangerous and tool.metadata.dangerous:
            continue
        
        schema = tool.get_schema()
        schemas.append(ToolSchemaResponse(
            name=schema["name"],
            description=schema["description"],
            parameters=schema["parameters"]
        ))
    
    return schemas


@router.post("/mcp/register", response_model=MCPRegisterResponse)
async def register_mcp_tool(req: MCPRegisterRequest, request: Request) -> MCPRegisterResponse:
    """Register a remote MCP function as a local Tool.

    After registration, the tool is accessible via /tools/execute with the given name.
    If `parameters` is omitted, the tool accepts a single OBJECT parameter `args`.
    """
    # Admin token gate (optional): if MCP_REGISTER_TOKEN is set, require token
    admin_token = os.getenv("MCP_REGISTER_TOKEN", "").strip()
    if admin_token:
        auth = request.headers.get("Authorization", "")
        x_token = request.headers.get("X-Admin-Token", "")
        bearer_ok = auth.startswith("Bearer ") and auth.split(" ", 1)[1] == admin_token
        header_ok = (x_token == admin_token)
        if not (bearer_ok or header_ok):
            raise HTTPException(status_code=401, detail="Unauthorized")

    registry = get_registry()

    # Convert parameter defs (if provided) to plain dicts for tool class factory
    param_defs = None
    if req.parameters:
        param_defs = [p.dict() for p in req.parameters]

    tool_class = create_mcp_tool_class(
        name=req.name,
        func_name=req.func_name,
        url=req.url,
        headers=req.headers,
        description=req.description or "MCP remote function",
        category=req.category or "mcp",
        parameters=param_defs,
    )

    try:
        registry.register(tool_class, override=True)
    except Exception as e:
        return MCPRegisterResponse(success=False, tool_name=req.name, message=str(e))

    return MCPRegisterResponse(success=True, tool_name=req.name, message="Registered")


@router.post("/openapi/validate", response_model=OpenAPIValidateResponse)
async def validate_openapi_spec(req: OpenAPIValidateRequest) -> OpenAPIValidateResponse:
    """
    Validate an OpenAPI spec and preview operations without registering.

    Args:
        req: Validation request with spec URL or inline spec

    Returns:
        Validation response with operations list and errors
    """
    from ..tools.openapi_tool import _fetch_spec_from_url, OpenAPILoader
    from ..tools.openapi_parser import OpenAPIParseError

    errors = []

    try:
        # Get spec
        spec_url_for_loader = None
        if req.spec_url:
            spec = _fetch_spec_from_url(req.spec_url)
            spec_url_for_loader = req.spec_url
        elif req.spec_inline:
            import yaml
            spec = yaml.safe_load(req.spec_inline)
        else:
            return OpenAPIValidateResponse(
                valid=False,
                operations_count=0,
                errors=["Must provide either spec_url or spec_inline"]
            )

        # Create temporary loader to validate
        loader = OpenAPILoader(
            name="__validate__",
            spec=spec,
            auth_type="none",
            auth_config={},
            spec_url=spec_url_for_loader,
        )

        # Extract operations
        operations = []
        for op_data in loader.operations:
            operations.append({
                "operation_id": op_data["operation_id"],
                "method": op_data["method"],
                "path": op_data["path"],
                "description": op_data["operation"].get("summary", "")
            })

        return OpenAPIValidateResponse(
            valid=True,
            operations_count=len(operations),
            operations=operations,
            base_url=loader.base_url,
            errors=[]
        )

    except OpenAPIParseError as e:
        return OpenAPIValidateResponse(
            valid=False,
            operations_count=0,
            errors=[f"Parse error: {str(e)}"]
        )
    except Exception as e:
        return OpenAPIValidateResponse(
            valid=False,
            operations_count=0,
            errors=[f"Validation error: {str(e)}"]
        )


@router.post("/openapi/register", response_model=OpenAPIRegisterResponse)
async def register_openapi_tools(req: OpenAPIRegisterRequest, request: Request) -> OpenAPIRegisterResponse:
    """
    Register OpenAPI spec as Shannon tools dynamically.

    After registration, each operation is accessible via /tools/execute with its operationId.
    Requires admin token if MCP_REGISTER_TOKEN is set (same security model as MCP).

    Args:
        req: Registration request with spec and configuration
        request: FastAPI request for auth header access

    Returns:
        Registration response with list of registered operations
    """
    from ..tools.openapi_tool import _fetch_spec_from_url, OpenAPILoader
    from ..tools.openapi_parser import OpenAPIParseError

    # Admin token gate (reuse MCP_REGISTER_TOKEN)
    admin_token = os.getenv("MCP_REGISTER_TOKEN", "").strip()
    if admin_token:
        auth = request.headers.get("Authorization", "")
        x_token = request.headers.get("X-Admin-Token", "")
        bearer_ok = auth.startswith("Bearer ") and auth.split(" ", 1)[1] == admin_token
        header_ok = (x_token == admin_token)
        if not (bearer_ok or header_ok):
            raise HTTPException(status_code=401, detail="Unauthorized")

    try:
        # Get spec
        spec_url_for_loader = None
        if req.spec_url:
            spec = _fetch_spec_from_url(req.spec_url)
            spec_url_for_loader = req.spec_url
        elif req.spec_inline:
            import yaml
            spec = yaml.safe_load(req.spec_inline)
        else:
            return OpenAPIRegisterResponse(
                success=False,
                collection_name=req.name,
                message="Must provide either spec_url or spec_inline"
            )

        # Create loader
        loader = OpenAPILoader(
            name=req.name,
            spec=spec,
            auth_type=req.auth_type,
            auth_config=req.auth_config or {},
            category=req.category or "api",
            base_cost_per_use=req.base_cost_per_use or 0.001,
            operations_filter=req.operations,
            tags_filter=req.tags,
            base_url_override=req.base_url,
            rate_limit=req.rate_limit or 30,
            timeout_seconds=req.timeout_seconds or 30.0,
            max_response_bytes=req.max_response_bytes or (10 * 1024 * 1024),
            spec_url=spec_url_for_loader,
        )

        # Generate and register tools
        tool_classes = loader.generate_tools()
        registry = get_registry()

        registered_ops = []
        for tool_class in tool_classes:
            try:
                registry.register(tool_class, override=True)
                # Extract operation_id from class name
                temp_instance = tool_class()
                registered_ops.append(temp_instance.metadata.name)
            except Exception as e:
                logger.error(f"Failed to register OpenAPI tool {tool_class.__name__}: {e}")

        return OpenAPIRegisterResponse(
            success=True,
            collection_name=req.name,
            operations_registered=registered_ops,
            message=f"Registered {len(registered_ops)} operations",
            rate_limit=loader.rate_limit,
            timeout_seconds=loader.timeout_seconds,
            max_response_bytes=loader.max_response_bytes,
        )

    except OpenAPIParseError as e:
        return OpenAPIRegisterResponse(
            success=False,
            collection_name=req.name,
            message=f"Parse error: {str(e)}"
        )
    except Exception as e:
        return OpenAPIRegisterResponse(
            success=False,
            collection_name=req.name,
            message=f"Registration error: {str(e)}"
        )


@router.post("/execute", response_model=ToolExecuteResponse)
async def execute_tool(request: ToolExecuteRequest) -> ToolExecuteResponse:
    """
    Execute a tool with given parameters

    Args:
        request: Tool execution request
    """
    registry = get_registry()
    tool = registry.get_tool(request.tool_name)

    if not tool:
        raise HTTPException(status_code=404, detail=f"Tool '{request.tool_name}' not found")

    try:
        # Execute the tool and return raw results
        result = await tool.execute(**request.parameters)

        return ToolExecuteResponse(
            success=result.success,
            output=result.output,
            error=result.error,
            metadata=result.metadata,
            execution_time_ms=result.execution_time_ms,
        )
    except Exception as e:
        logger.error(f"Tool execution error for {request.tool_name}: {e}")
        return ToolExecuteResponse(
            success=False,
            output=None,
            error=str(e),
        )


@router.post("/batch-execute", response_model=List[ToolExecuteResponse])
async def batch_execute_tools(requests: List[ToolExecuteRequest]) -> List[ToolExecuteResponse]:
    """
    Execute multiple tools in batch (sequentially for now)
    
    Args:
        requests: List of tool execution requests
    """
    results = []
    
    for request in requests:
        try:
            result = await execute_tool(request)
            results.append(result)
        except HTTPException as e:
            # Add error result for missing tools
            results.append(ToolExecuteResponse(
                success=False,
                output=None,
                error=e.detail,
            ))
    
    return results


@router.get("/{tool_name}/metadata")
async def get_tool_metadata(tool_name: str) -> Dict[str, Any]:
    """
    Get detailed metadata for a tool
    
    Args:
        tool_name: Name of the tool
    """
    registry = get_registry()
    metadata = registry.get_tool_metadata(tool_name)
    
    if not metadata:
        raise HTTPException(status_code=404, detail=f"Tool '{tool_name}' not found")
    
    return {
        "name": metadata.name,
        "version": metadata.version,
        "description": metadata.description,
        "category": metadata.category,
        "author": metadata.author,
        "requires_auth": metadata.requires_auth,
        "rate_limit": metadata.rate_limit,
        "timeout_seconds": metadata.timeout_seconds,
        "memory_limit_mb": metadata.memory_limit_mb,
        "sandboxed": metadata.sandboxed,
        "dangerous": metadata.dangerous,
        "cost_per_use": metadata.cost_per_use,
    }


@router.post("/select", response_model=ToolSelectResponse)
async def select_tools(req: Request, body: ToolSelectRequest) -> ToolSelectResponse:
    """LLM-backed tool selection with safe fallback.

    Returns selected tool names and suggested calls.
    """
    registry = get_registry()

    # Cache key ignores context to keep things simple and safe
    cache_key = f"{body.task}|{body.exclude_dangerous}|{body.max_tools}"
    import time
    now = time.time()
    cached = _SELECT_CACHE.get(cache_key)
    if cached and (now - cached.get("ts", 0)) <= _SELECT_TTL_SECONDS:
        data = cached.get("data", {})
        try:
            # Fast path: reconstruct Pydantic response
            return ToolSelectResponse(**data)
        except Exception:
            pass

    # Gather available tools (respect danger filter)
    tool_names = registry.list_tools()
    filtered_tools: List[str] = []
    for name in tool_names:
        tool = registry.get_tool(name)
        if not tool:
            continue
        if body.exclude_dangerous and getattr(tool.metadata, "dangerous", False):
            continue
        filtered_tools.append(name)

    # Early exit if none
    if not filtered_tools or body.max_tools == 0:
        return ToolSelectResponse(selected_tools=[], calls=[], provider_used=None)

    # Try LLM-based selection when providers are configured
    providers = getattr(req.app.state, 'providers', None)
    if providers and providers.is_configured():
        try:
            # Build concise tool descriptions to keep prompt small
            tools_summary = []
            for name in filtered_tools:
                tool = registry.get_tool(name)
                if not tool:
                    continue
                tools_summary.append({
                    "name": name,
                    "description": tool.metadata.description,
                    "parameters": list(tool.get_schema().get("parameters", {}).get("properties", {}).keys())
                })

            sys = (
                "You are a tool selection assistant. Read the task and choose at most N suitable tools. "
                "Return compact JSON only with fields: {\"selected_tools\": [names], \"calls\": [{\"tool_name\": name, \"parameters\": object}]}. "
                "Only include tools from the provided list and prefer zero or minimal arguments."
            )
            user = {
                "task": body.task,
                "context_keys": list((body.context or {}).keys())[:5],
                "tools": tools_summary,
                "max_tools": body.max_tools,
            }

            # Ask a small model to return JSON; avoid provider-specific tool_call plumbing
            wf_id = req.headers.get('X-Parent-Workflow-ID') or req.headers.get('X-Workflow-ID') or req.headers.get('x-workflow-id')
            ag_id = req.headers.get('X-Agent-ID') or req.headers.get('x-agent-id')

            result = await providers.generate_completion(
                messages=[{"role": "system", "content": sys}, {"role": "user", "content": str(user)}],
                max_tokens=300,
                temperature=0.1,
                response_format={"type": "json_object"},
                workflow_id=wf_id,
                agent_id=ag_id,
            )

            import json as _json
            raw = result.get("output_text", "")
            data = None
            try:
                data = _json.loads(raw)
            except Exception:
                # lenient fallback: try to find first {...}
                import re
                m = re.search(r"\{[\s\S]*\}", raw)
                if m:
                    try:
                        data = _json.loads(m.group(0))
                    except Exception:
                        data = None

            if isinstance(data, dict):
                selected = [s for s in data.get("selected_tools", []) if s in filtered_tools][: body.max_tools]
                calls_in = data.get("calls", []) or []
                calls: List[ToolCall] = []
                for c in calls_in:
                    try:
                        name = str(c.get("tool_name"))
                        if name and name in filtered_tools:
                            params = c.get("parameters") or {}
                            if not isinstance(params, dict):
                                params = {}
                            calls.append(ToolCall(tool_name=name, parameters=params))
                    except Exception:
                        continue
                # If calls empty but selected present, synthesize empty-arg calls
                if not calls and selected:
                    calls = [ToolCall(tool_name=n, parameters={}) for n in selected]
                resp = ToolSelectResponse(
                    selected_tools=selected,
                    calls=calls,
                    provider_used=result.get("provider"),
                )
                _SELECT_CACHE[cache_key] = {"ts": now, "data": resp.dict()}
                return resp
        except Exception as e:
            logger.warning(f"Tool selection LLM fallback due to error: {e}")

    # Heuristic fallback: very small, safe defaults
    task_lower = body.task.lower()
    selected: List[str] = []
    calls: List[ToolCall] = []

    def add(name: str, params: Dict[str, Any]):
        nonlocal selected, calls
        if name in filtered_tools and name not in selected and len(selected) < body.max_tools:
            selected.append(name)
            calls.append(ToolCall(tool_name=name, parameters=params))

    # No fallback pattern matching - trust the LLM's decision
    # If LLM providers aren't configured or fail, return empty selection

    resp = ToolSelectResponse(selected_tools=selected, calls=calls, provider_used=None)
    _SELECT_CACHE[cache_key] = {"ts": now, "data": resp.dict()}
    return resp
