"""
OpenAPI tool loader for Shannon.
Dynamically converts OpenAPI 3.x specifications into Shannon tools.

TODO: Add comprehensive tests for:
  - OpenAPI tool execution with different auth types (bearer, API key, basic)
  - Circuit breaker behavior (failure threshold, recovery)
  - Rate limiting enforcement
  - Header injection and body parameter processing
  - Vendor adapter loading and body transformation
  - Error handling for malformed requests/responses
"""

from __future__ import annotations

import asyncio
import os
import time
from typing import Any, Dict, List, Optional, Type
from urllib.parse import urlparse, quote
import logging

import httpx

from .base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult
from .openapi_parser import (
    OpenAPIParseError,
    validate_spec,
    extract_base_url,
    extract_operations,
    extract_parameters,
    extract_request_body,
    deduplicate_operation_ids,
)

logger = logging.getLogger(__name__)
from .vendor_adapters import get_vendor_adapter  # optional vendor-specific shaping


# Simple circuit breaker (same as MCP)
class _SimpleBreaker:
    def __init__(self, failure_threshold: int = 5, recovery_timeout: float = 60.0):
        self.failure_threshold = failure_threshold
        self.recovery_timeout = recovery_timeout
        self.failures = 0
        self.open_until: float = 0.0
        self.half_open = False

    def allow(self, now: float) -> bool:
        if self.open_until > now:
            return False
        if self.open_until != 0.0 and self.open_until <= now:
            # move to half-open and allow one trial
            self.half_open = True
            self.open_until = 0.0
        return True

    def on_success(self) -> None:
        self.failures = 0
        self.half_open = False
        self.open_until = 0.0

    def on_failure(self, now: float) -> None:
        self.failures += 1
        if self.failures >= self.failure_threshold:
            self.open_until = now + self.recovery_timeout
            self.half_open = False


_breakers: Dict[str, _SimpleBreaker] = {}


def _validate_domain(url: str, allowed_domains: List[str]) -> None:
    """
    Validate URL against allowed domains (mirrors MCP logic).

    Args:
        url: URL to validate
        allowed_domains: List of allowed domains or ["*"] for wildcard

    Raises:
        ValueError: If domain not allowed
    """
    host = urlparse(url).hostname or ""

    # Wildcard bypasses validation
    if "*" in allowed_domains:
        return

    # Check exact match or suffix match (subdomains)
    if not any(host == d or host.endswith("." + d) for d in allowed_domains):
        raise ValueError(
            f"OpenAPI URL host '{host}' not in allowed domains: {allowed_domains}"
        )


class OpenAPILoader:
    """
    Loads OpenAPI spec and generates Shannon Tool classes for each operation.
    """

    def __init__(
        self,
        name: str,
        spec: Dict[str, Any],
        auth_type: str = "none",
        auth_config: Optional[Dict[str, str]] = None,
        category: str = "api",
        base_cost_per_use: float = 0.001,
        operations_filter: Optional[List[str]] = None,
        tags_filter: Optional[List[str]] = None,
        base_url_override: Optional[str] = None,
        rate_limit: int = 30,
        timeout_seconds: float = 30.0,
        max_response_bytes: int = 10 * 1024 * 1024,
        spec_url: Optional[str] = None,
    ):
        """
        Initialize OpenAPI loader.

        Args:
            name: Tool collection name (used as prefix)
            spec: OpenAPI specification dict
            auth_type: Authentication type (none|api_key|bearer|basic)
            auth_config: Auth configuration dict
            category: Tool category
            base_cost_per_use: Base cost per operation
            operations_filter: Optional list of operationIds to include
            tags_filter: Optional list of tags to filter by
            base_url_override: Optional base URL override
            rate_limit: Requests per minute (default: 30, enforceable)
            timeout_seconds: Request timeout in seconds (default: 30)
            max_response_bytes: Max response size (default: 10MB)
            spec_url: Optional URL where spec was fetched from (for relative server URLs)
        """
        self.name = name
        self.spec = spec
        self.auth_type = auth_type
        self.auth_config = auth_config or {}
        self.category = category
        self.base_cost_per_use = base_cost_per_use
        self.operations_filter = operations_filter
        self.tags_filter = tags_filter
        self.base_url_override = base_url_override
        self.rate_limit = rate_limit
        self.timeout_seconds = timeout_seconds
        self.max_response_bytes = max_response_bytes
        self.spec_url = spec_url

        # Validate spec
        validate_spec(spec)

        # Extract base URL
        self.base_url = extract_base_url(spec, base_url_override, spec_url)

        # Validate domain
        allowed_domains = [
            d.strip()
            for d in os.getenv("OPENAPI_ALLOWED_DOMAINS", "localhost,127.0.0.1").split(
                ","
            )
            if d.strip()
        ]
        _validate_domain(self.base_url, allowed_domains)

        # Extract operations
        operations = extract_operations(spec, operations_filter, tags_filter)
        self.operations = deduplicate_operation_ids(operations)

        logger.info(
            f"OpenAPI loader '{name}': loaded {len(self.operations)} operations from {self.base_url}"
        )

    def generate_tools(self) -> List[Type[Tool]]:
        """
        Generate Tool classes for each operation.

        Returns:
            List of Tool class types
        """
        tool_classes = []

        for op_data in self.operations:
            tool_class = self._create_tool_class(op_data)
            tool_classes.append(tool_class)

        return tool_classes

    def _create_tool_class(self, op_data: Dict[str, Any]) -> Type[Tool]:
        """
        Create a Tool class for a single OpenAPI operation.

        Args:
            op_data: Operation data dict from parser

        Returns:
            Tool class type
        """
        operation_id = op_data["operation_id"]
        method = op_data["method"]
        path = op_data["path"]
        operation = op_data["operation"]

        # Extract parameters (pass spec for $ref resolution)
        params = extract_parameters(operation, self.spec)
        body_param = extract_request_body(operation, self.spec)
        if body_param:
            params.extend(body_param)

        # Build tool parameters
        tool_params = []
        for p in params:
            param_type_str = p["type"]
            param_type = getattr(
                ToolParameterType, param_type_str.upper(), ToolParameterType.STRING
            )

            tool_params.append(
                ToolParameter(
                    name=p["name"],
                    type=param_type,
                    description=p["description"],
                    required=p["required"],
                    enum=p.get("enum"),
                )
            )

        # Capture variables for closure
        base_url = self.base_url
        auth_type = self.auth_type
        auth_config = self.auth_config
        auth_vendor = (auth_config.get("vendor") or os.getenv("OPENAPI_VENDOR", "")).lower()
        vendor_adapter = get_vendor_adapter(auth_vendor)
        category = self.category
        base_cost = self.base_cost_per_use
        rate_limit = self.rate_limit
        timeout_seconds = self.timeout_seconds
        max_response_bytes = self.max_response_bytes
        loader_name = self.name
        build_auth_headers = self._build_auth_headers

        # Get or create circuit breaker for this base_url
        breaker_key = f"openapi:{base_url}"
        if breaker_key not in _breakers:
            _breakers[breaker_key] = _SimpleBreaker()
        breaker = _breakers[breaker_key]

        # Create Tool class dynamically
        class _OpenAPITool(Tool):
            def _get_metadata(self) -> ToolMetadata:
                summary = operation.get("summary", "")
                description = operation.get("description", summary)
                if not description:
                    description = f"{method} {path}"

                return ToolMetadata(
                    name=operation_id,
                    version="1.0.0",
                    description=description,
                    category=category,
                    author=f"OpenAPI:{loader_name}",
                    requires_auth=False,
                    timeout_seconds=int(timeout_seconds),
                    memory_limit_mb=128,
                    sandboxed=False,
                    session_aware=True,
                    dangerous=False,
                    cost_per_use=base_cost,
                    rate_limit=rate_limit,
                )

            def _get_parameters(self) -> List[ToolParameter]:
                return tool_params

            async def _execute_impl(
                self, session_context: Optional[Dict] = None, **kwargs
            ) -> ToolResult:
                now = time.time()

                logger.info(
                    f"OpenAPI tool {operation_id} _execute_impl called with session_context: {session_context is not None}, kwargs keys: {list(kwargs.keys())}"
                )

                # Circuit breaker check
                if not breaker.allow(now):
                    return ToolResult(
                        success=False,
                        output=None,
                        error=f"Circuit breaker open for {base_url} (too many failures)",
                    )

                # Build auth headers with session context for dynamic resolution
                headers = build_auth_headers(session_context)

                # Process header parameters from OpenAPI spec
                # Headers can be: explicit values, dynamic from body, or from kwargs
                for param in params:
                    if param["location"] == "header":
                        header_name = param["name"]
                        # Check if provided in kwargs
                        if header_name in kwargs:
                            headers[header_name] = str(kwargs[header_name])
                        # Check if it's a required header without a provided value
                        elif param["required"]:
                            # Try to resolve from body if it references a body field
                            # Example: sid header might need to come from profileId in body
                            # For now, we'll let it be resolved later via dynamic header resolution
                            pass

                # Build URL with path parameters (URL-encoded)
                url = base_url + path
                for param in params:
                    if param["location"] == "path" and param["name"] in kwargs:
                        placeholder = "{" + param["name"] + "}"
                        # URL-encode path parameter value
                        encoded_value = quote(str(kwargs[param["name"]]), safe="")
                        url = url.replace(placeholder, encoded_value)

                # Build query parameters
                query_params = {}
                for param in params:
                    if param["location"] == "query" and param["name"] in kwargs:
                        query_params[param["name"]] = kwargs[param["name"]]

                # Build request body
                json_body = None
                if body_param and "body" in kwargs:
                    json_body = kwargs["body"]
                    # Log initial body BEFORE any modifications
                    logger.warning(f"🟢 [{operation_id}] INITIAL json_body from kwargs:")
                    logger.warning(f"  ALL KEYS: {list(json_body.keys()) if isinstance(json_body, dict) else 'NOT A DICT'}")
                    # Keep OpenAPI layer generic: do not inject tool-specific defaults

                # Inject prompt_params from session_context into request body if available
                prompt_params = None
                if session_context and "prompt_params" in session_context:
                    prompt_params = session_context["prompt_params"]
                    logger.info(
                        f"OpenAPI tool {operation_id}: Found prompt_params = {prompt_params}"
                    )

                # Merge prompt_params into the body (generic, no vendor semantics)
                if prompt_params:
                    if json_body is None:
                        json_body = {}
                    # Merge prompt_params into request body (fill missing or empty fields)
                    if isinstance(json_body, dict) and isinstance(prompt_params, dict):
                        def _is_empty(val: Any) -> bool:
                            if val is None:
                                return True
                            if isinstance(val, str) and val.strip() == "":
                                return True
                            if isinstance(val, dict) and not val:
                                return True
                            # Arrays: treat [] as valid; do not override intentionally empty lists
                            return False

                        for key, value in prompt_params.items():
                            # Only fill if json_body field is missing or empty
                            # AND the prompt_params value is not empty
                            if (key not in json_body or _is_empty(json_body.get(key))) and not _is_empty(value):
                                json_body[key] = value

                        # Common aliasing between snake_case and camelCase for typical fields
                        try:
                            def to_snake(s: str) -> str:
                                out = []
                                for ch in s:
                                    if ch.isupper():
                                        out.append("_" + ch.lower())
                                    else:
                                        out.append(ch)
                                res = "".join(out)
                                return res[1:] if res.startswith("_") else res

                            def to_camel(s: str) -> str:
                                parts = s.split("_")
                                return parts[0] + "".join(p.capitalize() for p in parts[1:])

                            # No vendor-specific mapping here; handled by vendor adapter below

                            # Generic aliasing for all prompt keys (fill when missing or empty)
                            # Don't overwrite with empty values
                            for k, v in list(prompt_params.items()):
                                snake = to_snake(k)
                                camel = to_camel(k)
                                if (snake not in json_body or _is_empty(json_body.get(snake))) and snake != k and not _is_empty(v):
                                    json_body[snake] = v
                                if (camel not in json_body or _is_empty(json_body.get(camel))) and camel != k and not _is_empty(v):
                                    json_body[camel] = v
                        except Exception:
                            pass

                        logger.info(
                            f"OpenAPI tool {operation_id}: Injected prompt_params into body. Keys: {list(json_body.keys())}"
                        )

                        # Apply vendor-specific shaping via adapter (when available)
                        if vendor_adapter and isinstance(json_body, dict):
                            try:
                                json_body = vendor_adapter.transform_body(json_body, operation_id, prompt_params)
                                logger.info(f"Vendor adapter '{auth_vendor}' applied to body for {operation_id}")
                            except Exception:
                                pass

                        # Vendor/domain-specific request shaping is handled via vendor adapters; keep core generic