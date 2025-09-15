from __future__ import annotations

import os
import time
from collections import defaultdict, deque
from typing import Any, Dict, List, Optional, Type

from ..mcp_client import HttpStatelessClient
from .base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult


_TYPE_MAP = {
    "string": ToolParameterType.STRING,
    "integer": ToolParameterType.INTEGER,
    "float": ToolParameterType.FLOAT,
    "boolean": ToolParameterType.BOOLEAN,
    "array": ToolParameterType.ARRAY,
    "object": ToolParameterType.OBJECT,
}


def _to_param(defn: Dict[str, Any]) -> ToolParameter:
    t = str(defn.get("type", "object")).lower()
    return ToolParameter(
        name=str(defn.get("name", "arg")),
        type=_TYPE_MAP.get(t, ToolParameterType.OBJECT),
        description=str(defn.get("description", "")),
        required=bool(defn.get("required", False)),
        default=defn.get("default"),
    )


def create_mcp_tool_class(
    *,
    name: str,
    func_name: str,
    url: str,
    headers: Optional[Dict[str, str]] = None,
    description: str = "MCP remote function",
    category: str = "mcp",
    parameters: Optional[List[Dict[str, Any]]] = None,
) -> Type[Tool]:
    """
    Create a Tool subclass that invokes an MCP HTTP endpoint.

    The generated tool name is `name`; calling it POSTs {function: func_name, args: kwargs}
    to `url` and returns the JSON response as the tool output.
    """

    params = parameters or []
    tool_params = [_to_param(p) for p in params]

    # Simple per-tool rate limiter (process-local)
    _tool_requests: Dict[str, deque] = defaultdict(deque)
    _rate_default = int(os.getenv("MCP_RATE_LIMIT_DEFAULT", "60"))  # requests per minute

    class _McpTool(Tool):  # type: ignore
        _client = HttpStatelessClient(name=name, url=url, headers=headers or {})

        def _get_metadata(self) -> ToolMetadata:
            # Allow per-tool cost via env MCP_COST_<NAME>
            cost_env = f"MCP_COST_{name.upper()}"
            try:
                cost_per_use = float(os.getenv(cost_env, "0.001"))
            except Exception:
                cost_per_use = 0.001
            return ToolMetadata(
                name=name,
                version="1.0.0",
                description=description,
                category=category,
                author="MCP",
                requires_auth=False,
                timeout_seconds=15,
                memory_limit_mb=128,
                sandboxed=False,
                session_aware=False,
                dangerous=False,
                cost_per_use=cost_per_use,
            )

        def _get_parameters(self) -> List[ToolParameter]:
            # If no schema provided, accept a single OBJECT parameter named "args"
            return tool_params or [
                ToolParameter(
                    name="args",
                    type=ToolParameterType.OBJECT,
                    description="Arguments object passed to the MCP function",
                    required=False,
                )
            ]

        async def _execute_impl(self, session_context=None, **kwargs) -> ToolResult:
            try:
                # Rate limiting per tool name
                now = time.time()
                dq = _tool_requests[name]
                limit = _rate_default if _rate_default > 0 else 60
                # evict old entries
                window = 60.0
                while dq and dq[0] < now - window:
                    dq.popleft()
                if len(dq) >= limit:
                    return ToolResult(success=False, output=None, error=f"Rate limit exceeded: {limit}/min")
                dq.append(now)

                # If schema not provided, expect a single OBJECT parameter "args"
                call_args: Dict[str, Any]
                if tool_params:
                    call_args = kwargs
                else:
                    call_args = kwargs.get("args", {}) or {}
                    if not isinstance(call_args, dict):
                        return ToolResult(success=False, output=None, error="'args' must be an object")

                result = await self._client._invoke(func_name, **call_args)
                return ToolResult(success=True, output=result)
            except Exception as e:
                return ToolResult(success=False, output=None, error=str(e))

    _McpTool.__name__ = f"McpTool_{name}"
    return _McpTool
