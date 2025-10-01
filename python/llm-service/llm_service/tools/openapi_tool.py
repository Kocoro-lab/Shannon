"""
OpenAPI tool loader for Shannon.
Dynamically converts OpenAPI 3.x specifications into Shannon tools.
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
            for d in os.getenv("OPENAPI_ALLOWED_DOMAINS", "localhost,127.0.0.1").split(",")
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
        body_param = extract_request_body(operation)
        if body_param:
            params.append(body_param)

        # Build tool parameters
        tool_params = []
        for p in params:
            param_type_str = p["type"]
            param_type = getattr(ToolParameterType, param_type_str.upper(), ToolParameterType.STRING)

            tool_params.append(
                ToolParameter(
                    name=p["name"],
                    type=param_type,
                    description=p["description"],
                    required=p["required"],
                    enum=p.get("enum"),
                )
            )

        # Build headers with auth
        headers = self._build_auth_headers()

        # Capture variables for closure
        base_url = self.base_url
        auth_type = self.auth_type
        auth_config = self.auth_config
        category = self.category
        base_cost = self.base_cost_per_use
        rate_limit = self.rate_limit
        timeout_seconds = self.timeout_seconds
        max_response_bytes = self.max_response_bytes
        loader_name = self.name

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
                    session_aware=False,
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

                # Circuit breaker check
                if not breaker.allow(now):
                    return ToolResult(
                        success=False,
                        output=None,
                        error=f"Circuit breaker open for {base_url} (too many failures)"
                    )

                # Build URL with path parameters (URL-encoded)
                url = base_url + path
                for param in params:
                    if param["location"] == "path" and param["name"] in kwargs:
                        placeholder = "{" + param["name"] + "}"
                        # URL-encode path parameter value
                        encoded_value = quote(str(kwargs[param["name"]]), safe='')
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

                # Add query-based API key if configured
                request_headers = dict(headers)
                # Add Accept header to prefer JSON responses
                request_headers["Accept"] = "application/json"

                if auth_type == "api_key" and auth_config.get("api_key_location") == "query":
                    api_key_name = auth_config.get("api_key_name", "api_key")
                    api_key_value = auth_config.get("api_key_value", "")
                    # Resolve from env if starts with $
                    if api_key_value.startswith("$"):
                        env_var = api_key_value[1:]
                        api_key_value = os.getenv(env_var, "")
                    query_params[api_key_name] = api_key_value

                # Retry logic with exponential backoff (matches MCP)
                retries = int(os.getenv("OPENAPI_RETRIES", "3"))
                last_exception = None

                for attempt in range(1, retries + 1):
                    try:
                        # Execute request with configured timeout
                        start_time = time.time()
                        async with httpx.AsyncClient(timeout=timeout_seconds) as client:
                            response = await client.request(
                                method=method,
                                url=url,
                                params=query_params,
                                json=json_body,
                                headers=request_headers,
                            )
                            response.raise_for_status()

                            # Check response size
                            content_length = len(response.content)
                            if content_length > max_response_bytes:
                                breaker.on_failure(now)
                                return ToolResult(
                                    success=False,
                                    output=None,
                                    error=f"Response too large: {content_length} bytes (max {max_response_bytes})"
                                )

                            # Parse response
                            content_type = response.headers.get("content-type", "")
                            if "application/json" in content_type:
                                result = response.json()
                            else:
                                # Plain text for MVP
                                result = response.text

                            duration_ms = int((time.time() - start_time) * 1000)

                            # Success: reset breaker
                            breaker.on_success()

                            return ToolResult(
                                success=True,
                                output=result,
                                execution_time_ms=duration_ms,
                            )

                    except httpx.HTTPStatusError as e:
                        last_exception = e
                        breaker.on_failure(now)
                        if attempt >= retries:
                            return ToolResult(
                                success=False,
                                output=None,
                                error=f"HTTP {e.response.status_code}: {e.response.text[:200]}",
                            )
                        # Exponential backoff: 0.5s, 1s, 2s
                        delay = min(2.0 ** (attempt - 1) * 0.5, 5.0)
                        await asyncio.sleep(delay)

                    except Exception as e:
                        last_exception = e
                        breaker.on_failure(now)
                        if attempt >= retries:
                            return ToolResult(success=False, output=None, error=str(e))
                        # Exponential backoff
                        delay = min(2.0 ** (attempt - 1) * 0.5, 5.0)
                        await asyncio.sleep(delay)

                # Fallback (should never reach here)
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Request failed after {retries} retries: {str(last_exception)}"
                )

        _OpenAPITool.__name__ = f"OpenAPITool_{operation_id}"
        return _OpenAPITool

    def _build_auth_headers(self) -> Dict[str, str]:
        """
        Build authentication headers based on auth_type and auth_config.

        Returns:
            Headers dict
        """
        headers = {}

        if self.auth_type == "bearer":
            token = self.auth_config.get("token", "")
            # Resolve from env if starts with $
            if token.startswith("$"):
                env_var = token[1:]
                token = os.getenv(env_var, "")
            if token:
                headers["Authorization"] = f"Bearer {token}"

        elif self.auth_type == "api_key":
            location = self.auth_config.get("api_key_location", "header")
            if location == "header":
                key_name = self.auth_config.get("api_key_name", "X-API-Key")
                key_value = self.auth_config.get("api_key_value", "")
                # Resolve from env if starts with $
                if key_value.startswith("$"):
                    env_var = key_value[1:]
                    key_value = os.getenv(env_var, "")
                if key_value:
                    headers[key_name] = key_value

        elif self.auth_type == "basic":
            username = self.auth_config.get("username", "")
            password = self.auth_config.get("password", "")
            # Resolve from env
            if username.startswith("$"):
                username = os.getenv(username[1:], "")
            if password.startswith("$"):
                password = os.getenv(password[1:], "")
            if username and password:
                import base64
                creds = f"{username}:{password}"
                encoded = base64.b64encode(creds.encode()).decode()
                headers["Authorization"] = f"Basic {encoded}"

        return headers


def load_openapi_tools_from_config(config: Dict[str, Any]) -> List[Type[Tool]]:
    """
    Load OpenAPI tools from shannon.yaml configuration.

    Args:
        config: Configuration dict from shannon.yaml

    Returns:
        List of Tool class types
    """
    tool_classes = []
    openapi_tools_config = config.get("openapi_tools")

    # Handle None or empty config (all tools commented out)
    if not openapi_tools_config:
        return tool_classes

    for tool_name, tool_config in openapi_tools_config.items():
        if not tool_config.get("enabled", False):
            logger.info(f"OpenAPI tool collection '{tool_name}' is disabled, skipping")
            continue

        try:
            # Get spec (URL or inline)
            spec_url = tool_config.get("spec_url")
            spec_inline = tool_config.get("spec_inline")

            spec_url_for_loader = None
            if spec_url:
                spec = _fetch_spec_from_url(spec_url)
                spec_url_for_loader = spec_url
            elif spec_inline:
                import yaml
                spec = yaml.safe_load(spec_inline)
            else:
                logger.error(f"OpenAPI tool '{tool_name}': must provide spec_url or spec_inline")
                continue

            # Create loader
            loader = OpenAPILoader(
                name=tool_name,
                spec=spec,
                auth_type=tool_config.get("auth_type", "none"),
                auth_config=tool_config.get("auth_config", {}),
                category=tool_config.get("category", "api"),
                base_cost_per_use=tool_config.get("base_cost_per_use", 0.001),
                operations_filter=tool_config.get("operations"),
                tags_filter=tool_config.get("tags"),
                base_url_override=tool_config.get("base_url"),
                # Default to 30 so Tool base rate limiting is enforced
                rate_limit=tool_config.get("rate_limit", 30),
                timeout_seconds=tool_config.get("timeout_seconds", 30.0),
                max_response_bytes=tool_config.get("max_response_bytes", 10 * 1024 * 1024),
                spec_url=spec_url_for_loader,
            )

            # Generate tools
            tools = loader.generate_tools()
            tool_classes.extend(tools)

            logger.info(f"Loaded {len(tools)} tools from OpenAPI collection '{tool_name}'")

        except OpenAPIParseError as e:
            logger.error(f"Failed to parse OpenAPI spec for '{tool_name}': {e}")
        except Exception as e:
            logger.error(f"Failed to load OpenAPI tools from '{tool_name}': {e}")

    return tool_classes


def _fetch_spec_from_url(url: str) -> Dict[str, Any]:
    """
    Fetch OpenAPI spec from URL with size limit and domain validation.

    Args:
        url: URL to OpenAPI spec (JSON or YAML)

    Returns:
        Parsed spec dict

    Raises:
        ValueError: If fetch fails or spec too large
    """
    # Validate domain
    allowed_domains = [
        d.strip()
        for d in os.getenv("OPENAPI_ALLOWED_DOMAINS", "localhost,127.0.0.1").split(",")
        if d.strip()
    ]
    _validate_domain(url, allowed_domains)

    # Fetch with size limit
    max_size = int(os.getenv("OPENAPI_MAX_SPEC_SIZE", str(5 * 1024 * 1024)))  # 5MB default
    timeout = float(os.getenv("OPENAPI_FETCH_TIMEOUT", "30"))

    try:
        import httpx
        import yaml

        with httpx.Client(timeout=timeout) as client:
            response = client.get(url)
            response.raise_for_status()

            content_length = len(response.content)
            if content_length > max_size:
                raise ValueError(
                    f"Spec size ({content_length} bytes) exceeds max ({max_size} bytes)"
                )

            # Try JSON first, fall back to YAML
            try:
                return response.json()
            except Exception:
                return yaml.safe_load(response.text)

    except Exception as e:
        raise ValueError(f"Failed to fetch OpenAPI spec from {url}: {e}")
