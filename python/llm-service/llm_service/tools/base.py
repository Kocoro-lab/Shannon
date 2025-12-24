"""
Base Tool interface for Shannon platform
"""

from abc import ABC, abstractmethod
from dataclasses import dataclass
from typing import Any, Dict, List, Optional, Union
from enum import Enum
import json
from datetime import datetime


class ToolParameterType(Enum):
    """Supported parameter types for tools"""

    STRING = "string"
    INTEGER = "integer"
    FLOAT = "float"
    BOOLEAN = "boolean"
    ARRAY = "array"
    OBJECT = "object"
    FILE = "file"  # For file paths or file content


@dataclass
class ToolParameter:
    """Definition of a tool parameter"""

    name: str
    type: ToolParameterType
    description: str
    required: bool = True
    default: Any = None
    enum: Optional[List[Any]] = None  # For enumerated values
    min_value: Optional[Union[int, float]] = None
    max_value: Optional[Union[int, float]] = None
    pattern: Optional[str] = None  # Regex pattern for validation


@dataclass
class ToolMetadata:
    """Metadata about a tool"""

    name: str
    version: str
    description: str
    category: str  # e.g., "search", "calculation", "file", "database"
    author: str = "Shannon"
    requires_auth: bool = False
    rate_limit: Optional[int] = None  # Requests per minute
    timeout_seconds: int = 30
    memory_limit_mb: int = 512
    sandboxed: bool = True  # Whether to run in sandbox
    session_aware: bool = False  # Whether tool uses session context
    dangerous: bool = False  # Requires extra confirmation
    cost_per_use: float = 0.0  # Cost in USD per invocation
    input_examples: Optional[List[Dict[str, Any]]] = None  # Examples for tool usage (Anthropic-specific)


@dataclass
class ToolResult:
    """Result from tool execution"""

    success: bool
    output: Any
    error: Optional[str] = None
    metadata: Optional[Dict[str, Any]] = None
    execution_time_ms: Optional[int] = None
    tokens_used: Optional[int] = None

    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary for serialization"""
        return {
            "success": self.success,
            "output": self.output,
            "error": self.error,
            "metadata": self.metadata or {},
            "execution_time_ms": self.execution_time_ms,
            "tokens_used": self.tokens_used,
        }

    def to_json(self) -> str:
        """Convert to JSON string"""
        return json.dumps(self.to_dict())


class Tool(ABC):
    """Abstract base class for all tools"""

    def __init__(self):
        self.metadata = self._get_metadata()
        self.parameters = self._get_parameters()
        self._execution_count = 0
        # Track rate limits per session/user instead of globally
        self._execution_tracker = {}  # {session_id: last_execution_time}

    @abstractmethod
    def _get_metadata(self) -> ToolMetadata:
        """Return tool metadata"""
        pass

    @abstractmethod
    def _get_parameters(self) -> List[ToolParameter]:
        """Return list of tool parameters"""
        pass

    @abstractmethod
    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        """
        Actual tool execution implementation.
        All parameters are passed as keyword arguments.
        Session context is optionally provided for session-aware tools.
        """
        pass

    async def execute(
        self,
        session_context: Optional[Dict] = None,
        observer: Optional[Any] = None,
        **kwargs,
    ) -> ToolResult:
        """
        Execute the tool with given parameters.
        Handles validation, logging, and error handling.

        Args:
            session_context: Optional session context containing history, user info, etc.
            observer: Optional callback(event_name, payload) for intermediate status updates
            **kwargs: Tool-specific parameters
        """
        import time

        start_time = time.time()

        # Reset request ID for this execution (ensures unique key per execute call)
        self._reset_request_id()

        try:
            # Coerce and validate parameters
            kwargs = self._coerce_parameters(kwargs)
            self._validate_parameters(kwargs)

            # Extract session_id and agent_id from context
            session_id = None
            agent_id = None
            if session_context and isinstance(session_context, dict):
                session_id = session_context.get("session_id")
                agent_id = session_context.get("agent_id")

            # Get consistent tracker key for this execution
            tracker_key = self._get_tracker_key(session_id, agent_id)

            # Check rate limits (skip for high-rate tools like calculator)
            if self.metadata.rate_limit and self.metadata.rate_limit < 100:
                retry_after = self._get_retry_after(tracker_key)
                if retry_after is not None:
                    # Rate limited - return remaining wait time
                    # NOTE: Do NOT update tracker on rejection to avoid extending wait window
                    return ToolResult(
                        success=False,
                        output=None,
                        error=f"Rate limit exceeded. Retry after {retry_after:.1f}s",
                        metadata={"retry_after_seconds": int(retry_after) + 1},
                        execution_time_ms=int((time.time() - start_time) * 1000),
                    )

            # Execute the tool with session context if tool is session-aware
            if self.metadata.session_aware:
                result = await self._execute_impl(
                    session_context=session_context, observer=observer, **kwargs
                )
            else:
                result = await self._execute_impl(
                    session_context=None, observer=observer, **kwargs
                )

            # Track execution (both success and failure from _execute_impl)
            self._execution_count += 1
            self._execution_tracker[tracker_key] = datetime.now()

            # Clean up old entries (keep only last 100 sessions)
            if len(self._execution_tracker) > 100:
                # Remove oldest entries
                sorted_keys = sorted(
                    self._execution_tracker.items(), key=lambda x: x[1]
                )
                for key, _ in sorted_keys[: len(sorted_keys) - 100]:
                    del self._execution_tracker[key]

            # Add execution time
            execution_time = int((time.time() - start_time) * 1000)
            result.execution_time_ms = execution_time

            return result

        except Exception as e:
            return ToolResult(
                success=False,
                output=None,
                error=str(e),
                execution_time_ms=int((time.time() - start_time) * 1000),
            )

    def _coerce_parameters(self, kwargs: Dict[str, Any]) -> Dict[str, Any]:
        """Best-effort coercion of incoming parameters to expected types.
        - INTEGER: accept floats with integral value (e.g., 3.0) and numeric strings
        - FLOAT: accept ints and numeric strings
        - BOOLEAN: accept common string forms ("true"/"false")
        Other types pass through unchanged.
        """
        if not kwargs:
            return {}
        out = dict(kwargs)
        spec = {p.name: p for p in self.parameters}
        for name, param in spec.items():
            if name not in out:
                continue
            val = out[name]
            try:
                if param.type == ToolParameterType.INTEGER:
                    if isinstance(val, float) and float(val).is_integer():
                        out[name] = int(val)
                    elif isinstance(val, str):
                        s = val.strip()
                        # Handle simple numeric strings (no locale/separators)
                        if s.isdigit() or (s.startswith("-") and s[1:].isdigit()):
                            out[name] = int(s)
                    # Clamp to allowed min/max to avoid validation failures on oversized inputs
                    if isinstance(out[name], int):
                        if param.max_value is not None and out[name] > param.max_value:
                            out[name] = param.max_value
                        if param.min_value is not None and out[name] < param.min_value:
                            out[name] = param.min_value
                elif param.type == ToolParameterType.FLOAT:
                    if isinstance(val, int):
                        out[name] = float(val)
                    elif isinstance(val, str):
                        s = val.strip()
                        out[name] = float(s)
                    if isinstance(out[name], float):
                        if param.max_value is not None and out[name] > param.max_value:
                            out[name] = float(param.max_value)
                        if param.min_value is not None and out[name] < param.min_value:
                            out[name] = float(param.min_value)
                elif param.type == ToolParameterType.BOOLEAN:
                    if isinstance(val, str):
                        s = val.strip().lower()
                        if s in ("true", "1", "yes", "y"):
                            out[name] = True
                        elif s in ("false", "0", "no", "n"):
                            out[name] = False
            except Exception:
                # If coercion fails, keep original and let validation raise
                pass
        return out

    def _validate_parameters(self, kwargs: Dict[str, Any]) -> None:
        """Validate input parameters against tool definition"""
        # Check required parameters
        for param in self.parameters:
            if param.required and param.name not in kwargs:
                raise ValueError(f"Required parameter '{param.name}' is missing")

            if param.name in kwargs:
                value = kwargs[param.name]

                # Type validation
                if not self._validate_type(value, param.type):
                    raise TypeError(
                        f"Parameter '{param.name}' expects type {param.type.value}, "
                        f"got {type(value).__name__}"
                    )

                # Enum validation
                if param.enum and value not in param.enum:
                    raise ValueError(
                        f"Parameter '{param.name}' must be one of {param.enum}"
                    )

                # Range validation
                if param.min_value is not None and value < param.min_value:
                    raise ValueError(
                        f"Parameter '{param.name}' must be >= {param.min_value}"
                    )
                if param.max_value is not None and value > param.max_value:
                    raise ValueError(
                        f"Parameter '{param.name}' must be <= {param.max_value}"
                    )

                # Pattern validation
                if param.pattern:
                    import re

                    if not re.match(param.pattern, str(value)):
                        raise ValueError(
                            f"Parameter '{param.name}' does not match pattern {param.pattern}"
                        )

        # Check for unknown parameters
        known_params = {p.name for p in self.parameters}
        unknown = set(kwargs.keys()) - known_params
        if unknown:
            raise ValueError(f"Unknown parameters: {unknown}")

    def _validate_type(self, value: Any, expected_type: ToolParameterType) -> bool:
        """Validate value against expected type"""
        type_map = {
            ToolParameterType.STRING: str,
            ToolParameterType.INTEGER: int,
            ToolParameterType.FLOAT: (int, float),
            ToolParameterType.BOOLEAN: bool,
            ToolParameterType.ARRAY: list,
            ToolParameterType.OBJECT: dict,
            ToolParameterType.FILE: str,  # File paths are strings
        }

        expected = type_map.get(expected_type)
        if expected:
            return isinstance(value, expected)
        return False

    def _get_tracker_key(
        self, session_id: Optional[str], agent_id: Optional[str] = None
    ) -> str:
        """Generate consistent tracker key for rate limiting.

        Uses session_id if available, then agent_id as fallback.
        This avoids the threading.get_ident() issue where all asyncio coroutines
        share the same thread ID in a single-threaded event loop.

        Args:
            session_id: Optional session ID from context
            agent_id: Optional agent ID for fallback tracking

        Returns:
            A unique key for tracking rate limits
        """
        if session_id:
            return f"session:{session_id}"
        if agent_id:
            return f"agent:{agent_id}"
        # For requests without session_id or agent_id, use request-scoped tracking
        # This prevents false rate limiting across concurrent requests
        # NOTE: Per-request UUID means no cross-request rate limiting without context
        import uuid

        if not hasattr(self, "_current_request_id") or self._current_request_id is None:
            self._current_request_id = uuid.uuid4().hex[:8]
        return f"request:{self._current_request_id}"

    def _reset_request_id(self) -> None:
        """Reset request ID for next execution.

        Called at the start of each execute() to ensure each execution
        gets a unique tracker key when no session_id is available.
        """
        self._current_request_id = None

    def _get_retry_after(self, tracker_key: str) -> Optional[float]:
        """Check rate limit and return remaining wait time if limited.

        Args:
            tracker_key: The key to use for tracking (session or request based)

        Returns:
            None if execution is allowed, or remaining seconds to wait if rate limited
        """
        if not self.metadata.rate_limit:
            return None

        # Skip rate limiting for high-throughput tools (>= 60 req/min)
        if self.metadata.rate_limit >= 60:
            return None

        if tracker_key not in self._execution_tracker:
            return None

        last_execution = self._execution_tracker[tracker_key]

        # Calculate minimum interval between calls
        from datetime import timedelta

        min_interval = timedelta(seconds=60.0 / self.metadata.rate_limit)
        elapsed = datetime.now() - last_execution

        if elapsed >= min_interval:
            return None

        # Return remaining time to wait
        remaining = min_interval - elapsed
        return remaining.total_seconds()

    def get_schema(self) -> Dict[str, Any]:
        """
        Get JSON schema for this tool (compatible with OpenAI function calling)
        """
        properties = {}
        required = []

        for param in self.parameters:
            prop = {
                "type": param.type.value,
                "description": param.description,
            }

            # Add items for ARRAY type (required by OpenAI schema validation)
            if param.type == ToolParameterType.ARRAY:
                prop["items"] = {"type": "string"}  # Default to string, can be extended

            if param.enum:
                prop["enum"] = param.enum
            if param.min_value is not None:
                prop["minimum"] = param.min_value
            if param.max_value is not None:
                prop["maximum"] = param.max_value
            if param.pattern:
                prop["pattern"] = param.pattern
            if param.default is not None:
                prop["default"] = param.default

            properties[param.name] = prop

            if param.required:
                required.append(param.name)

        return {
            "name": self.metadata.name,
            "description": self.metadata.description,
            "parameters": {
                "type": "object",
                "properties": properties,
                "required": required,
            },
        }

    def __repr__(self) -> str:
        return f"<Tool: {self.metadata.name} v{self.metadata.version}>'"
