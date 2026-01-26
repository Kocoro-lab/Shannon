"""
Test suite for File Operation Tools with session isolation.

Tests path validation, session workspace creation, and security boundaries.
"""

import asyncio
import os
import tempfile
import pytest
from pathlib import Path
from unittest.mock import patch

from llm_service.tools.builtin.file_ops import (
    FileReadTool,
    FileWriteTool,
    FileListTool,
    _get_session_workspace,
    _get_allowed_dirs,
    _is_allowed,
)


class TestSessionWorkspace:
    """Test session workspace helpers"""

    def test_get_session_workspace_default(self, tmp_path):
        """Test workspace creation with default session ID"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            workspace = _get_session_workspace(None)
            assert workspace.name == "default"
            assert workspace.parent == tmp_path
            assert workspace.exists()

    def test_get_session_workspace_with_session_id(self, tmp_path):
        """Test workspace creation with explicit session ID"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            workspace = _get_session_workspace({"session_id": "test-session-123"})
            assert workspace.name == "test-session-123"
            assert workspace.parent == tmp_path
            assert workspace.exists()

    def test_get_allowed_dirs_includes_session_workspace(self, tmp_path):
        """Test that allowed dirs includes session workspace"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            allowed = _get_allowed_dirs({"session_id": "test-session"})
            session_workspace = tmp_path / "test-session"
            assert session_workspace in allowed

    def test_get_allowed_dirs_includes_shannon_workspace(self, tmp_path):
        """Test that SHANNON_WORKSPACE is included when set"""
        with patch.dict(
            os.environ,
            {
                "SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path),
                "SHANNON_WORKSPACE": str(tmp_path / "shared"),
            },
        ):
            (tmp_path / "shared").mkdir()
            allowed = _get_allowed_dirs()
            assert (tmp_path / "shared").resolve() in allowed

    def test_get_allowed_dirs_cwd_disabled_by_default(self, tmp_path):
        """Test that cwd is not allowed by default"""
        with patch.dict(
            os.environ,
            {
                "SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path),
                "SHANNON_DEV_ALLOW_CWD": "",
            },
            clear=True,
        ):
            allowed = _get_allowed_dirs()
            assert Path.cwd().resolve() not in allowed

    def test_get_allowed_dirs_cwd_enabled(self, tmp_path):
        """Test that cwd is allowed when SHANNON_DEV_ALLOW_CWD=1"""
        with patch.dict(
            os.environ,
            {
                "SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path),
                "SHANNON_DEV_ALLOW_CWD": "1",
            },
        ):
            allowed = _get_allowed_dirs()
            assert Path.cwd().resolve() in allowed


class TestIsAllowed:
    """Test path validation helper"""

    def test_path_within_base_is_allowed(self, tmp_path):
        """Test that paths within base are allowed"""
        (tmp_path / "subdir").mkdir()
        target = (tmp_path / "subdir" / "file.txt").resolve()
        assert _is_allowed(target, tmp_path.resolve()) is True

    def test_path_outside_base_is_rejected(self, tmp_path):
        """Test that paths outside base are rejected"""
        target = Path("/etc/passwd").resolve()
        assert _is_allowed(target, tmp_path.resolve()) is False

    def test_path_traversal_is_rejected(self, tmp_path):
        """Test that path traversal attempts are rejected"""
        # Create a resolved path that would escape
        target = (tmp_path / ".." / "etc" / "passwd").resolve()
        assert _is_allowed(target, tmp_path.resolve()) is False


class TestFileReadTool:
    """Test FileReadTool with session isolation"""

    @pytest.fixture
    def tool(self):
        return FileReadTool()

    @pytest.mark.asyncio
    async def test_read_file_in_session_workspace(self, tool, tmp_path):
        """Test reading a file from session workspace"""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        test_file = session_dir / "test.txt"
        test_file.write_text("Hello, World!")

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(test_file),
            )

        assert result.success is True
        assert result.output == "Hello, World!"
        assert result.metadata["path"] == str(test_file)

    @pytest.mark.asyncio
    async def test_read_file_outside_workspace_rejected(self, tool, tmp_path):
        """Test that reading files outside workspace is rejected"""
        with patch.dict(
            os.environ,
            {
                "SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path),
                "SHANNON_DEV_ALLOW_CWD": "",
            },
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path="/etc/passwd",
            )

        assert result.success is False
        assert "not allowed" in result.error

    @pytest.mark.asyncio
    async def test_read_nonexistent_file(self, tool, tmp_path):
        """Test reading a file that doesn't exist"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(tmp_path / "test-session" / "nonexistent.txt"),
            )

        assert result.success is False
        assert "not found" in result.error.lower()

    @pytest.mark.asyncio
    async def test_read_json_file_parses(self, tool, tmp_path):
        """Test that JSON files are parsed automatically"""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        test_file = session_dir / "data.json"
        test_file.write_text('{"name": "test", "value": 42}')

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(test_file),
            )

        assert result.success is True
        assert result.output == {"name": "test", "value": 42}

    @pytest.mark.asyncio
    async def test_read_file_in_tmp_with_legacy_flag(self, tool, tmp_path):
        """Test reading from /tmp when SHANNON_ALLOW_GLOBAL_TMP is enabled"""
        # Use /tmp explicitly (not tempfile which may use platform-specific dirs)
        tmp_file = Path("/tmp") / f"test_read_{os.getpid()}.txt"
        tmp_file.write_text("tmp content")

        try:
            with patch.dict(
                os.environ,
                {
                    "SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path),
                    "SHANNON_ALLOW_GLOBAL_TMP": "1",
                },
            ):
                result = await tool._execute_impl(
                    session_context={"session_id": "test"},
                    path=str(tmp_file),
                )

            assert result.success is True
            assert result.output == "tmp content"
        finally:
            tmp_file.unlink(missing_ok=True)

    @pytest.mark.asyncio
    async def test_read_file_in_tmp_blocked_by_default(self, tool, tmp_path):
        """Test that reading from /tmp is blocked by default for session isolation"""
        # Use /tmp explicitly (not tempfile which may use platform-specific dirs)
        tmp_file = Path("/tmp") / f"test_blocked_{os.getpid()}.txt"
        tmp_file.write_text("tmp content")

        try:
            with patch.dict(
                os.environ,
                {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)},
                clear=False,
            ):
                # Remove the env var if it exists
                os.environ.pop("SHANNON_ALLOW_GLOBAL_TMP", None)
                result = await tool._execute_impl(
                    session_context={"session_id": "test"},
                    path=str(tmp_file),
                )

            assert result.success is False
            assert "not allowed" in result.error
        finally:
            tmp_file.unlink(missing_ok=True)


class TestFileWriteTool:
    """Test FileWriteTool with session isolation"""

    @pytest.fixture
    def tool(self):
        return FileWriteTool()

    @pytest.mark.asyncio
    async def test_write_file_in_session_workspace(self, tool, tmp_path):
        """Test writing a file to session workspace"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(tmp_path / "test-session" / "output.txt"),
                content="Written content",
                create_dirs=True,
            )

        assert result.success is True
        written_file = tmp_path / "test-session" / "output.txt"
        assert written_file.exists()
        assert written_file.read_text() == "Written content"

    @pytest.mark.asyncio
    async def test_write_file_outside_workspace_rejected(self, tool, tmp_path):
        """Test that writing files outside workspace is rejected"""
        with patch.dict(
            os.environ,
            {
                "SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path),
                "SHANNON_DEV_ALLOW_CWD": "",
            },
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path="/etc/test.txt",
                content="should fail",
            )

        assert result.success is False
        assert "not allowed" in result.error

    @pytest.mark.asyncio
    async def test_write_append_mode(self, tool, tmp_path):
        """Test appending to a file"""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        test_file = session_dir / "append.txt"
        test_file.write_text("Line 1\n")

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(test_file),
                content="Line 2\n",
                mode="append",
            )

        assert result.success is True
        assert test_file.read_text() == "Line 1\nLine 2\n"

    @pytest.mark.asyncio
    async def test_write_without_create_dirs_fails(self, tool, tmp_path):
        """Test that writing to nonexistent dir fails without create_dirs"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(tmp_path / "test-session" / "subdir" / "file.txt"),
                content="content",
                create_dirs=False,
            )

        assert result.success is False
        assert "Parent directory does not exist" in result.error


class TestFileListTool:
    """Test FileListTool with session isolation"""

    @pytest.fixture
    def tool(self):
        return FileListTool()

    @pytest.mark.asyncio
    async def test_list_files_in_session_workspace(self, tool, tmp_path):
        """Test listing files in session workspace"""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        (session_dir / "file1.txt").touch()
        (session_dir / "file2.py").touch()
        (session_dir / "subdir").mkdir()

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(session_dir),
            )

        assert result.success is True
        assert result.metadata["file_count"] == 2
        assert result.metadata["dir_count"] == 1

    @pytest.mark.asyncio
    async def test_list_files_with_pattern(self, tool, tmp_path):
        """Test listing files with pattern filter"""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        (session_dir / "file1.txt").touch()
        (session_dir / "file2.py").touch()
        (session_dir / "file3.txt").touch()

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(session_dir),
                pattern="*.txt",
            )

        assert result.success is True
        assert result.metadata["file_count"] == 2

    @pytest.mark.asyncio
    async def test_list_files_outside_workspace_rejected(self, tool, tmp_path):
        """Test that listing outside workspace is rejected"""
        with patch.dict(
            os.environ,
            {
                "SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path),
                "SHANNON_DEV_ALLOW_CWD": "",
            },
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path="/etc",
            )

        assert result.success is False
        assert "not allowed" in result.error

    @pytest.mark.asyncio
    async def test_list_nonexistent_directory(self, tool, tmp_path):
        """Test listing a nonexistent directory"""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(tmp_path / "nonexistent"),
            )

        assert result.success is False
        assert "not found" in result.error.lower()


class TestSessionIsolation:
    """Test session isolation between different sessions"""

    @pytest.mark.asyncio
    async def test_sessions_have_separate_workspaces(self, tmp_path):
        """Test that different sessions have isolated workspaces"""
        write_tool = FileWriteTool()

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            # Write file in session A
            result_a = await write_tool._execute_impl(
                session_context={"session_id": "session-a"},
                path=str(tmp_path / "session-a" / "secret.txt"),
                content="Session A secret",
                create_dirs=True,
            )
            assert result_a.success is True

            # Write file in session B
            result_b = await write_tool._execute_impl(
                session_context={"session_id": "session-b"},
                path=str(tmp_path / "session-b" / "secret.txt"),
                content="Session B secret",
                create_dirs=True,
            )
            assert result_b.success is True

            # Verify workspaces are separate
            assert (tmp_path / "session-a" / "secret.txt").read_text() == "Session A secret"
            assert (tmp_path / "session-b" / "secret.txt").read_text() == "Session B secret"

            # Verify session A cannot write to session B's workspace
            # Cross-session access is now blocked by default for security
            result_cross = await write_tool._execute_impl(
                session_context={"session_id": "session-a"},
                path=str(tmp_path / "session-b" / "intruder.txt"),
                content="Should be blocked - cross-session access",
                create_dirs=True,
            )
            # Session isolation is enforced: session-a cannot access session-b's workspace
            assert result_cross.success is False
            assert "not allowed" in result_cross.error
