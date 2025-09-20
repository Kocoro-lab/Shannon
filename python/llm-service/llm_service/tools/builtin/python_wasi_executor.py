"""
Python WASI Executor Tool - Production Implementation

This tool provides secure Python code execution via WebAssembly System Interface (WASI).
It uses a full CPython 3.11.4 interpreter compiled to WebAssembly for true sandboxing.

Features:
- Full Python standard library support
- Memory and CPU resource limits
- Timeout protection
- Secure filesystem isolation
- Output streaming capability
- Session persistence (optional)
"""

import os
import json
import asyncio
import ast
import logging
from typing import Dict, Any, Optional, List
from dataclasses import dataclass, field
import time
import hashlib

import grpc
from google.protobuf import struct_pb2

from ...grpc_gen.agent import agent_pb2, agent_pb2_grpc
from ...grpc_gen.common import common_pb2
from ..base import (
    Tool,
    ToolMetadata,
    ToolParameter,
    ToolParameterType,
    ToolResult,
)
from ...config import Settings

logger = logging.getLogger(__name__)


@dataclass
class ExecutionSession:
    """Represents a persistent Python execution session"""
    session_id: str
    variables: Dict[str, Any] = field(default_factory=dict)
    imports: List[str] = field(default_factory=list)
    last_accessed: float = field(default_factory=time.time)
    execution_count: int = 0


class PythonWasiExecutorTool(Tool):
    """
    Production Python executor using WASI sandbox.

    This tool executes Python code in a secure WebAssembly sandbox using
    a full CPython interpreter compiled to WASM. It provides complete
    Python standard library support while maintaining strict security isolation.
    """

    # Class-level interpreter cache for performance
    _interpreter_cache: Optional[bytes] = None
    _cache_hash: Optional[str] = None
    _sessions: Dict[str, ExecutionSession] = {}
    _session_lock: asyncio.Lock = asyncio.Lock()  # Thread-safe session access
    _max_sessions: int = 100
    _session_timeout: int = int(os.getenv("PYTHON_WASI_SESSION_TIMEOUT", "3600"))  # Default 1 hour

    def __init__(self):
        super().__init__()
        self.settings = Settings()
        self.interpreter_path = os.getenv(
            "PYTHON_WASI_WASM_PATH",
            "/opt/wasm-interpreters/python-3.11.4.wasm"
        )
        self.agent_core_addr = os.getenv("AGENT_CORE_ADDR", "agent-core:50051")
        self._load_interpreter_cache()

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="python_executor",
            version="2.0.0",
            description="Execute Python code in secure WASI sandbox with full stdlib support",
            category="code",
            author="Shannon",
            requires_auth=False,
            rate_limit=self.settings.python_executor_rate_limit,  # Configurable via PYTHON_EXECUTOR_RATE_LIMIT env var
            timeout_seconds=30,
            memory_limit_mb=256,
            sandboxed=True,
            dangerous=False,  # Safe due to WASI isolation
            cost_per_use=0.001,  # Minimal cost for compute
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="code",
                type=ToolParameterType.STRING,
                description="Python code to execute (full stdlib available)",
                required=True,
            ),
            ToolParameter(
                name="session_id",
                type=ToolParameterType.STRING,
                description="Optional session ID for persistent variables across executions",
                required=False,
            ),
            ToolParameter(
                name="timeout_seconds",
                type=ToolParameterType.INTEGER,
                description="Execution timeout in seconds (default: 30, max: 60)",
                required=False,
                default=30,
            ),
            ToolParameter(
                name="stdin",
                type=ToolParameterType.STRING,
                description="Optional input data to provide via stdin",
                required=False,
                default="",
            ),
        ]

    def _load_interpreter_cache(self):
        """Load and cache the Python WASM interpreter for performance"""
        try:
            # Check if interpreter exists
            if not os.path.exists(self.interpreter_path):
                logger.warning(f"Python WASI interpreter not found at {self.interpreter_path}")
                return

            # Calculate hash to detect changes
            with open(self.interpreter_path, "rb") as f:
                content = f.read()
                new_hash = hashlib.sha256(content).hexdigest()

            # Cache if new or changed
            if self._cache_hash != new_hash:
                self._interpreter_cache = content
                self._cache_hash = new_hash
                logger.info(f"Cached Python WASI interpreter ({len(content)} bytes)")
        except Exception as e:
            logger.error(f"Failed to cache interpreter: {e}")

    async def _get_or_create_session(self, session_id: Optional[str]) -> Optional[ExecutionSession]:
        """Get or create a persistent execution session (thread-safe)"""
        if not session_id:
            return None

        async with self._session_lock:
            # Clean expired sessions
            current_time = time.time()
            expired = [
                sid for sid, sess in self._sessions.items()
                if current_time - sess.last_accessed > self._session_timeout
            ]
            for sid in expired:
                del self._sessions[sid]

            # Get or create session
            if session_id not in self._sessions:
                if len(self._sessions) >= self._max_sessions:
                    # Remove oldest session
                    oldest = min(self._sessions.items(), key=lambda x: x[1].last_accessed)
                    del self._sessions[oldest[0]]

                self._sessions[session_id] = ExecutionSession(session_id=session_id)

            session = self._sessions[session_id]
            session.last_accessed = current_time
            session.execution_count += 1

            return session

    def _prepare_code_with_session(self, code: str, session: Optional[ExecutionSession]) -> str:
        """Prepare code with session context if available"""
        if not session:
            return code

        # Build session context
        context_lines = []

        # Restore imports
        for imp in session.imports:
            context_lines.append(imp)

        # Restore variables
        if session.variables:
            context_lines.append("# Restored session variables")
            for name, value in session.variables.items():
                if isinstance(value, (int, float, str, bool, list, dict)):
                    context_lines.append(f"{name} = {repr(value)}")

        # Combine with new code
        if context_lines:
            full_code = "\n".join(context_lines) + "\n\n" + code
        else:
            full_code = code

        # Add code to capture new variables
        capture_code = """
# Capture session state
import sys
import json
_session_vars = {k: v for k, v in globals().items()
                 if not k.startswith('_') and k not in ['sys', 'json']}
print("__SESSION_STATE__", json.dumps({
    k: repr(v) for k, v in _session_vars.items()
    if isinstance(v, (int, float, str, bool, list, dict))
}), sep=":", end="__END_SESSION__")
"""

        return full_code + "\n" + capture_code

    async def _extract_session_state(self, output: str, session: Optional[ExecutionSession]) -> str:
        """Extract and store session state from output (thread-safe).

        Note: Session state persistence is limited to Python literals that can be
        evaluated with ast.literal_eval (int, float, str, bool, list, dict, tuple, None).
        Complex objects (classes, functions, etc.) will be stored as string representations
        but won't be restored as functional objects in future sessions.
        """
        if not session or "__SESSION_STATE__" not in output:
            return output

        try:
            # Extract session state
            parts = output.split("__SESSION_STATE__:")
            if len(parts) == 2:
                clean_output = parts[0]
                state_part = parts[1].split("__END_SESSION__")[0]

                # Parse state
                state = json.loads(state_part)

                # Use lock when modifying session variables
                async with self._session_lock:
                    for name, repr_value in state.items():
                        try:
                            # Safely evaluate the repr'd value using ast.literal_eval
                            # This limits persistence to Python literals only
                            session.variables[name] = ast.literal_eval(repr_value)
                        except (ValueError, SyntaxError):
                            # For complex objects, store string representation
                            session.variables[name] = repr_value
                            logger.debug(f"Session variable '{name}' stored as string (not a Python literal)")

                return clean_output
        except Exception as e:
            logger.debug(f"Failed to extract session state: {e}")

        return output

    async def _execute_impl(
        self,
        session_context: Optional[Dict] = None,
        **kwargs
    ) -> ToolResult:
        """Execute Python code in WASI sandbox"""

        code = kwargs.get("code", "")
        session_id = kwargs.get("session_id")
        timeout = min(kwargs.get("timeout_seconds", 30), 60)  # Max 60 seconds
        # stdin = kwargs.get("stdin", "")  # Not used currently, but kept for future use

        if not code:
            return ToolResult(
                success=False,
                output=None,
                error="No code provided to execute",
            )

        try:
            # Get or create session
            session = await self._get_or_create_session(session_id)

            # Prepare code with session context
            if session:
                code = self._prepare_code_with_session(code, session)
                logger.debug(f"Executing in session {session_id} (run #{session.execution_count})")

            # Prepare execution request with proper structure for agent-core
            # Note: Using file path instead of base64 due to gRPC 4MB message limit
            # Python WASM needs argv[0] to be program name, then -c flag to execute stdin
            tool_params = {
                "tool": "code_executor",  # Required field for agent-core
                "wasm_path": self.interpreter_path,  # Use file path (Python.wasm is 20MB)
                "stdin": code,  # Pass Python code as stdin
                "argv": ["python", "-c", "import sys; exec(sys.stdin.read())"],  # Python argv format
            }

            # Build gRPC request - agent-core expects tool_parameters directly in context
            ctx = struct_pb2.Struct()
            ctx.update({"tool_parameters": tool_params})

            req = agent_pb2.ExecuteTaskRequest(
                query=f"Execute Python code (session: {session_id or 'none'})",
                context=ctx,
                available_tools=["code_executor"],
            )

            if hasattr(common_pb2, "ExecutionMode"):
                req.mode = int(common_pb2.ExecutionMode.EXECUTION_MODE_SIMPLE)

            # Execute via Agent Core with timeout
            start_time = time.time()

            async with grpc.aio.insecure_channel(self.agent_core_addr) as channel:
                stub = agent_pb2_grpc.AgentServiceStub(channel)

                # Use asyncio timeout for better control
                try:
                    resp = await asyncio.wait_for(
                        stub.ExecuteTask(req),
                        timeout=timeout
                    )
                except asyncio.TimeoutError:
                    return ToolResult(
                        success=False,
                        output=None,
                        error=f"Execution timeout after {timeout} seconds",
                        metadata={"timeout": True, "session_id": session_id}
                    )

            execution_time = time.time() - start_time

            # Process response
            if hasattr(resp, "result") and resp.result:
                output = resp.result

                # Extract session state if applicable
                if session:
                    output = await self._extract_session_state(output, session)

                return ToolResult(
                    success=True,
                    output=output,
                    metadata={
                        "execution_time_ms": int(execution_time * 1000),
                        "session_id": session_id,
                        "execution_count": session.execution_count if session else 1,
                        "interpreter": "CPython 3.11.4 (WASI)",
                    }
                )
            else:
                error_msg = resp.error_message if hasattr(resp, "error_message") else "Unknown error"
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Execution failed: {error_msg}",
                    metadata={"session_id": session_id}
                )

        except grpc.RpcError as e:
            logger.error(f"gRPC error: {e.code()}: {e.details()}")
            return ToolResult(
                success=False,
                output=None,
                error=f"Communication error: {e.details()}",
            )
        except Exception as e:
            logger.error(f"Execution error: {e}", exc_info=True)
            return ToolResult(
                success=False,
                output=None,
                error=f"Execution failed: {str(e)}",
            )

    @classmethod
    def clear_sessions(cls):
        """Clear all execution sessions"""
        cls._sessions.clear()
        logger.info("Cleared all Python execution sessions")

    @classmethod
    def get_session_info(cls, session_id: str) -> Optional[Dict[str, Any]]:
        """Get information about a session"""
        if session_id in cls._sessions:
            session = cls._sessions[session_id]
            return {
                "session_id": session.session_id,
                "variables": list(session.variables.keys()),
                "imports": session.imports,
                "execution_count": session.execution_count,
                "last_accessed": session.last_accessed,
            }
        return None


# Export the tool class
__all__ = ["PythonWasiExecutorTool"]