"""
Bash Executor Tool - Execute allowlisted commands with session isolation

Design note: Uses asyncio.create_subprocess_exec() with shell=False + an allowlist
of binaries to avoid shell injection. Blocklists alone are not sufficient.
"""

import asyncio
import os
import shlex
from pathlib import Path
from typing import Any, Dict, List, Optional

from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult
from .file_ops import _validate_session_id


class BashExecutorTool(Tool):
    """Execute allowlisted commands in a session workspace (shell=False).

    Security model:
    - Uses shell=False to prevent shell injection
    - Allowlist of approved binaries only
    - Blocks shell metacharacters as defense-in-depth
    - Executes in session-isolated workspace directory
    - Timeout enforcement to prevent hanging commands
    """

    # Safe environment variables to pass to subprocesses
    # SECURITY: Do NOT add API keys, tokens, or credentials here
    SAFE_ENV_VARS = {
        # System essentials
        "PATH",
        "HOME",
        "USER",
        "SHELL",
        "LANG",
        "LC_ALL",
        "LC_CTYPE",
        "TERM",
        "TZ",
        # Build tools
        "GOPATH",
        "GOROOT",
        "CARGO_HOME",
        "RUSTUP_HOME",
        "NODE_PATH",
        "NPM_CONFIG_PREFIX",
        "PYTHONPATH",
        "VIRTUAL_ENV",
        # Git (no tokens)
        "GIT_AUTHOR_NAME",
        "GIT_AUTHOR_EMAIL",
        "GIT_COMMITTER_NAME",
        "GIT_COMMITTER_EMAIL",
        # Shannon session context (safe)
        "SHANNON_SESSION_ID",
        "SHANNON_SESSION_WORKSPACES_DIR",
    }

    # Keep this small and boring; expand via config/env only if needed
    ALLOWED_BINARIES = {
        "git",
        "ls",
        "pwd",
        "rg",
        "cat",
        "head",
        "tail",
        "wc",
        "grep",
        "find",
        "go",
        "cargo",
        "pytest",
        "python",
        "python3",
        "node",
        "npm",
        "make",
        "echo",
        "which",
        "mkdir",
        "rm",
        "cp",
        "mv",
        "touch",
        "diff",
        "sort",
        "uniq",
    }

    # Disallow shell metacharacters (defense-in-depth on top of shell=False)
    DISALLOWED_SUBSTRINGS = ["|", ";", "&&", "||", ">", "<", "$(", "`", "\n", "\r", "$(("]

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="bash",
            version="1.0.0",
            description="Execute allowlisted commands with output capture in a session workspace. Uses shell=False for security.",
            category="system",
            author="Shannon",
            requires_auth=True,
            rate_limit=20,  # 20 executions per minute
            timeout_seconds=30,
            memory_limit_mb=512,
            sandboxed=False,  # Runs on the host (not WASI)
            session_aware=True,
            dangerous=True,  # Command execution is inherently dangerous
            cost_per_use=0.002,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="command",
                type=ToolParameterType.STRING,
                description="Command to execute. Must start with an allowed binary (git, ls, python, etc.).",
                required=True,
            ),
            ToolParameter(
                name="timeout",
                type=ToolParameterType.INTEGER,
                description="Command timeout in seconds (max 30)",
                required=False,
                default=10,
                min_value=1,
                max_value=30,
            ),
        ]

    async def _execute_impl(
        self,
        session_context: Optional[Dict] = None,
        observer: Optional[Any] = None,
        **kwargs,
    ) -> ToolResult:
        """Execute command safely with session isolation.

        Args:
            session_context: Optional session context containing session_id
            observer: Optional callback for status updates (unused)
            **kwargs: Tool parameters (command, timeout)

        Returns:
            ToolResult with stdout, stderr, and exit_code
        """
        command = kwargs["command"]
        timeout = kwargs.get("timeout", 10)

        # Check for shell metacharacters (defense-in-depth)
        for substr in self.DISALLOWED_SUBSTRINGS:
            if substr in command:
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Shell features are not allowed. Found '{substr}' in command.",
                )

        # Parse args (shell=False execution)
        try:
            argv = shlex.split(command)
        except ValueError as e:
            return ToolResult(
                success=False,
                output=None,
                error=f"Invalid command quoting: {str(e)}",
            )

        if not argv:
            return ToolResult(success=False, output=None, error="Empty command")

        # Check if binary is in allowlist
        binary = argv[0]
        if binary not in self.ALLOWED_BINARIES:
            allowed_list = ", ".join(sorted(self.ALLOWED_BINARIES))
            return ToolResult(
                success=False,
                output=None,
                error=f"Command '{binary}' is not allowed. Allowed commands: {allowed_list}",
            )

        # Determine working directory (session workspace)
        raw_session_id = (session_context or {}).get("session_id", "default")
        try:
            session_id = _validate_session_id(raw_session_id)
        except ValueError as e:
            return ToolResult(
                success=False,
                output=None,
                error=str(e),
                metadata={"command": command},
            )

        base_dir = Path(
            os.getenv("SHANNON_SESSION_WORKSPACES_DIR", "/tmp/shannon-sessions")
        ).resolve()
        workspace = base_dir / session_id

        # Defense in depth: verify resolved path is within base_dir
        workspace_resolved = workspace.resolve()
        if not str(workspace_resolved).startswith(str(base_dir)):
            return ToolResult(
                success=False,
                output=None,
                error="Invalid session_id: path escape attempt detected",
                metadata={"command": command},
            )

        workspace.mkdir(parents=True, exist_ok=True)

        try:
            # Create subprocess with shell=False (using create_subprocess_exec, not shell)
            # This is the safe pattern - no shell interpretation of arguments
            # SECURITY: Only pass safe env vars to prevent secret exfiltration
            # API keys, tokens, and credentials are NOT passed to subprocesses
            safe_env = {
                k: v for k, v in os.environ.items()
                if k in self.SAFE_ENV_VARS
            }
            # Override HOME to isolate subprocess and add session context
            safe_env["HOME"] = str(workspace)
            safe_env["SHANNON_SESSION_ID"] = session_id

            proc = await asyncio.create_subprocess_exec(
                *argv,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                cwd=str(workspace),
                env=safe_env,
            )

            try:
                stdout, stderr = await asyncio.wait_for(
                    proc.communicate(), timeout=timeout
                )
            except asyncio.TimeoutError:
                proc.kill()
                await proc.wait()
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Command timed out after {timeout} seconds",
                    metadata={
                        "command": argv,
                        "working_dir": str(workspace),
                        "timed_out": True,
                    },
                )

            stdout_str = stdout.decode("utf-8", errors="replace")
            stderr_str = stderr.decode("utf-8", errors="replace")

            return ToolResult(
                success=proc.returncode == 0,
                output={
                    "stdout": stdout_str,
                    "stderr": stderr_str,
                    "exit_code": proc.returncode,
                },
                error=stderr_str if proc.returncode != 0 else None,
                metadata={
                    "command": argv,
                    "working_dir": str(workspace),
                    "exit_code": proc.returncode,
                    "timed_out": False,
                },
            )

        except FileNotFoundError:
            return ToolResult(
                success=False,
                output=None,
                error=f"Command '{binary}' not found in PATH",
            )
        except PermissionError:
            return ToolResult(
                success=False,
                output=None,
                error=f"Permission denied executing '{binary}'",
            )
        except Exception as e:
            return ToolResult(
                success=False,
                output=None,
                error=f"Execution error: {str(e)}",
            )
