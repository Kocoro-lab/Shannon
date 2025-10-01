"""
Minimal OpenAPI 3.x parser for Shannon tool generation.
Supports OpenAPI 3.0 and 3.1 with MVP feature set.
"""

from typing import Any, Dict, List, Optional, Tuple
import re
import copy
import ipaddress
import socket


class OpenAPIParseError(Exception):
    """Raised when OpenAPI spec is invalid or unsupported."""
    pass


def _is_private_ip(hostname: str) -> bool:
    """
    Check if hostname resolves to a private/internal IP address.

    Blocks:
    - Private IPs (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)
    - Loopback (127.0.0.0/8, ::1)
    - Link-local (169.254.0.0/16, fe80::/10)
    - Cloud metadata (169.254.169.254, metadata.google.internal)
    """
    # Block known metadata hostnames
    if hostname.lower() in ["metadata.google.internal", "metadata", "169.254.169.254"]:
        return True

    try:
        # Try to resolve hostname to IP
        ip_str = socket.gethostbyname(hostname)
        ip = ipaddress.ip_address(ip_str)

        # Check if private/reserved
        return (
            ip.is_private or
            ip.is_loopback or
            ip.is_link_local or
            ip.is_reserved or
            ip.is_multicast
        )
    except (socket.gaierror, ValueError):
        # Can't resolve or invalid IP - allow (will fail later with proper error)
        return False


def resolve_ref(spec: Dict[str, Any], ref_path: str) -> Any:
    """
    Resolve a $ref pointer in the OpenAPI spec.

    Args:
        spec: Full OpenAPI specification
        ref_path: Reference path like "#/components/schemas/Pet"

    Returns:
        The resolved object from the spec

    Raises:
        OpenAPIParseError: If reference cannot be resolved
    """
    if not ref_path.startswith("#/"):
        raise OpenAPIParseError(f"Only local references supported (starting with #/), got: {ref_path}")

    parts = ref_path[2:].split("/")  # Skip "#/"
    current = spec

    try:
        for part in parts:
            # Handle escaped characters in JSON Pointer (RFC 6901)
            part = part.replace("~1", "/").replace("~0", "~")
            current = current[part]
        return current
    except (KeyError, TypeError) as e:
        raise OpenAPIParseError(f"Failed to resolve $ref '{ref_path}': {e}")


def resolve_refs_in_schema(schema: Any, spec: Dict[str, Any], visited: Optional[set] = None) -> Any:
    """
    Recursively resolve all $ref references in a schema.

    Args:
        schema: Schema object (may contain $ref)
        spec: Full OpenAPI specification
        visited: Set of visited $ref paths to detect cycles

    Returns:
        Schema with all $ref references resolved

    Raises:
        OpenAPIParseError: If circular reference detected
    """
    if visited is None:
        visited = set()

    if not isinstance(schema, dict):
        return schema

    # Check for $ref
    if "$ref" in schema:
        ref_path = schema["$ref"]

        # Detect circular references
        if ref_path in visited:
            raise OpenAPIParseError(f"Circular reference detected: {ref_path}")

        visited.add(ref_path)

        # Resolve the reference
        resolved = resolve_ref(spec, ref_path)

        # Deep copy to avoid modifying the original spec
        resolved = copy.deepcopy(resolved)

        # Recursively resolve nested refs
        resolved = resolve_refs_in_schema(resolved, spec, visited)

        visited.remove(ref_path)

        # Merge properties from the original schema (excluding $ref)
        # OpenAPI 3.1 allows sibling properties alongside $ref
        result = resolved
        for key, value in schema.items():
            if key != "$ref":
                result[key] = value

        return result

    # Recursively resolve refs in nested structures
    result = {}
    for key, value in schema.items():
        if isinstance(value, dict):
            result[key] = resolve_refs_in_schema(value, spec, visited)
        elif isinstance(value, list):
            result[key] = [
                resolve_refs_in_schema(item, spec, visited) if isinstance(item, dict) else item
                for item in value
            ]
        else:
            result[key] = value

    return result


def validate_spec(spec: Dict[str, Any]) -> None:
    """
    Validate minimal required fields for OpenAPI 3.x spec.

    Raises:
        OpenAPIParseError: If spec is invalid or unsupported
    """
    if not isinstance(spec, dict):
        raise OpenAPIParseError("Spec must be a dictionary")

    # Check OpenAPI version
    version = spec.get("openapi", "")
    if not version.startswith("3."):
        raise OpenAPIParseError(f"Unsupported OpenAPI version: {version}. Only 3.x supported.")

    # Check required top-level fields
    if "info" not in spec:
        raise OpenAPIParseError("Missing required field: info")
    if "paths" not in spec or not spec["paths"]:
        raise OpenAPIParseError("Missing or empty paths")

    # Validate servers if present
    servers = spec.get("servers", [])
    if servers and not isinstance(servers, list):
        raise OpenAPIParseError("servers must be an array")


def extract_base_url(spec: Dict[str, Any], override_base_url: Optional[str] = None, spec_url: Optional[str] = None) -> str:
    """
    Extract base URL from OpenAPI spec.

    Args:
        spec: OpenAPI specification dict
        override_base_url: Optional override from config
        spec_url: Optional URL where spec was fetched from (for relative server URLs)

    Returns:
        Base URL for API requests
    """
    if override_base_url:
        return override_base_url.rstrip("/")

    servers = spec.get("servers", [])
    if not servers:
        raise OpenAPIParseError("No servers defined in spec and no base_url override provided")

    # Use first server
    server = servers[0]
    url = server.get("url", "")
    if not url:
        raise OpenAPIParseError("Server URL is empty")

    # Handle server variables (use defaults, no substitution yet)
    variables = server.get("variables", {})
    for var_name, var_spec in variables.items():
        default = var_spec.get("default", "")
        placeholder = "{" + var_name + "}"
        url = url.replace(placeholder, default)

    # Handle relative URLs (common in PetStore and other examples)
    if url.startswith("/"):
        if spec_url:
            # Extract scheme://host from spec_url
            from urllib.parse import urlparse
            parsed = urlparse(spec_url)
            base = f"{parsed.scheme}://{parsed.netloc}"
            url = base + url
        else:
            raise OpenAPIParseError(
                f"Server URL '{url}' is relative but no spec_url provided to resolve it"
            )

    # SSRF protection: Block private/internal IPs after resolution
    from urllib.parse import urlparse
    parsed_url = urlparse(url)
    hostname = parsed_url.hostname
    if hostname and _is_private_ip(hostname):
        raise OpenAPIParseError(
            f"Server URL '{url}' resolves to private/internal IP address. "
            "This is blocked for security (SSRF protection). "
            "Public APIs only."
        )

    return url.rstrip("/")


def extract_operations(
    spec: Dict[str, Any],
    operations_filter: Optional[List[str]] = None,
    tags_filter: Optional[List[str]] = None
) -> List[Dict[str, Any]]:
    """
    Extract operations from OpenAPI spec.

    Args:
        spec: OpenAPI specification dict
        operations_filter: Optional list of operationIds to include
        tags_filter: Optional list of tags to filter by

    Returns:
        List of operation dicts with keys: method, path, operation, operation_id
    """
    operations = []
    paths = spec.get("paths", {})

    http_methods = ["get", "post", "put", "patch", "delete", "head", "options"]

    for path, path_item in paths.items():
        if not isinstance(path_item, dict):
            continue

        for method in http_methods:
            if method not in path_item:
                continue

            operation = path_item[method]
            if not isinstance(operation, dict):
                continue

            # Get or generate operationId
            operation_id = operation.get("operationId")
            if not operation_id:
                # Generate stable name: method_path (sanitized)
                sanitized_path = re.sub(r'[^a-zA-Z0-9_]', '_', path.strip("/"))
                operation_id = f"{method}_{sanitized_path}"

            # Filter by operationId if specified
            if operations_filter and operation_id not in operations_filter:
                continue

            # Filter by tags if specified
            if tags_filter:
                op_tags = operation.get("tags", [])
                if not any(tag in tags_filter for tag in op_tags):
                    continue

            operations.append({
                "method": method.upper(),
                "path": path,
                "operation": operation,
                "operation_id": operation_id
            })

    # Check for explosion
    if len(operations) > 200:
        raise OpenAPIParseError(
            f"Spec contains {len(operations)} operations. "
            "Use operations or tags filter to limit scope (max 200)."
        )

    return operations


def map_openapi_type_to_tool_type(openapi_type: str, openapi_format: Optional[str] = None) -> str:
    """
    Map OpenAPI primitive types to Shannon ToolParameterType.

    Args:
        openapi_type: OpenAPI type (string, integer, number, boolean, array, object)
        openapi_format: Optional format (int32, int64, float, double, etc.)

    Returns:
        Shannon ToolParameterType name
    """
    type_map = {
        "string": "string",
        "integer": "integer",
        "number": "float",
        "boolean": "boolean",
        "array": "array",
        "object": "object",
    }

    result = type_map.get(openapi_type, "string")

    # Handle integer formats
    if openapi_type == "integer":
        return "integer"

    # Handle number formats
    if openapi_type == "number":
        return "float"

    return result


def extract_parameters(operation: Dict[str, Any], spec: Dict[str, Any]) -> List[Dict[str, Any]]:
    """
    Extract path and query parameters from operation.
    MVP: Only support primitive types (string, integer, number, boolean).

    Args:
        operation: OpenAPI operation object
        spec: Full OpenAPI spec (for resolving $ref)

    Returns:
        List of parameter dicts with keys: name, type, required, description, location
    """
    params = []

    # Extract path and query parameters
    parameters = operation.get("parameters", [])
    for param in parameters:
        if not isinstance(param, dict):
            continue

        # Resolve $ref if present
        if "$ref" in param:
            try:
                param = resolve_refs_in_schema(param, spec)
            except OpenAPIParseError:
                # Skip parameters we can't resolve
                continue

        location = param.get("in", "")
        if location not in ["path", "query"]:
            # Skip header and cookie params for MVP
            continue

        name = param.get("name")
        if not name:
            continue

        required = param.get("required", False)
        if location == "path":
            required = True  # Path params are always required

        description = param.get("description", "")

        # Get schema (resolve $ref if present)
        schema = param.get("schema", {})
        if "$ref" in schema:
            try:
                schema = resolve_refs_in_schema(schema, spec)
            except OpenAPIParseError:
                # Fallback to string type if ref can't be resolved
                schema = {"type": "string"}

        param_type = schema.get("type", "string")
        param_format = schema.get("format")

        # Map to tool type
        tool_type = map_openapi_type_to_tool_type(param_type, param_format)

        # Extract enum if present
        enum_values = schema.get("enum")

        params.append({
            "name": name,
            "type": tool_type,
            "required": required,
            "description": description,
            "location": location,
            "enum": enum_values
        })

    return params


def extract_request_body(operation: Dict[str, Any]) -> Optional[Dict[str, Any]]:
    """
    Extract request body parameter from operation.
    MVP: Only support application/json content type.

    Args:
        operation: OpenAPI operation object

    Returns:
        Request body parameter dict or None if no body
    """
    request_body = operation.get("requestBody")
    if not request_body:
        return None

    content = request_body.get("content", {})

    # Check for application/json
    json_content = content.get("application/json")
    if not json_content:
        # No JSON body for MVP
        return None

    required = request_body.get("required", False)
    description = request_body.get("description", "Request body")

    # For MVP, treat entire body as a single object parameter
    # Don't try to flatten nested schemas
    return {
        "name": "body",
        "type": "object",
        "required": required,
        "description": description,
        "location": "body"
    }


def deduplicate_operation_ids(operations: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """
    Deduplicate operation IDs by appending numeric suffixes.

    Args:
        operations: List of operation dicts

    Returns:
        Operations with unique operation_ids
    """
    seen = {}
    result = []

    for op in operations:
        op_id = op["operation_id"]
        original_id = op_id

        # Check for collision
        if op_id in seen:
            counter = 2
            while f"{original_id}_{counter}" in seen:
                counter += 1
            op_id = f"{original_id}_{counter}"
            op["operation_id"] = op_id

        seen[op_id] = True
        result.append(op)

    return result