"""
Test suite for File Operation Tools with session isolation.

Tests path validation, session workspace creation, and security boundaries.
"""

import asyncio
import os
import tempfile
import pytest
from pathlib import Path
from unittest.mock import patch, AsyncMock

from llm_service.tools.builtin.file_ops import (
    FileReadTool,
    FileWriteTool,
    FileListTool,
    FileSearchTool,
    FileEditTool,
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


class TestFileSearchTool:
    """Test FileSearchTool structure, validation, and search functionality."""

    @pytest.fixture
    def tool(self):
        return FileSearchTool()

    def test_tool_metadata(self, tool):
        """Test that metadata is correctly defined."""
        assert tool.metadata.name == "file_search"
        assert "search" in tool.metadata.description.lower()
        assert "grep" in tool.metadata.description.lower()
        assert tool.metadata.category == "file"

    def test_tool_parameters(self, tool):
        """Test that parameters are correctly defined."""
        params = tool._get_parameters()
        param_names = [p.name for p in params]
        assert "query" in param_names
        assert "path" in param_names
        assert "max_results" in param_names
        # query should be required
        query_param = next(p for p in params if p.name == "query")
        assert query_param.required is True
        # path and max_results should be optional
        path_param = next(p for p in params if p.name == "path")
        assert path_param.required is False
        max_param = next(p for p in params if p.name == "max_results")
        assert max_param.required is False

    @pytest.mark.asyncio
    async def test_search_empty_query_rejected(self, tool, tmp_path):
        """Test that empty query is rejected."""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="",
            )
        assert result.success is False
        assert "empty" in result.error.lower()

    @pytest.mark.asyncio
    async def test_search_query_too_long_rejected(self, tool, tmp_path):
        """Test that overly long query is rejected."""
        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="x" * 201,
            )
        assert result.success is False
        assert "too long" in result.error.lower()

    @pytest.mark.asyncio
    async def test_search_finds_matching_lines(self, tool, tmp_path):
        """Test that search returns correct matches."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        (session_dir / "hello.txt").write_text("Hello World\nGoodbye World\nHello Again\n")
        (session_dir / "other.txt").write_text("No match here\n")

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="hello",
            )

        assert result.success is True
        assert len(result.output) == 2
        # Check structure of match entries
        for match in result.output:
            assert "file" in match
            assert "line" in match
            assert "content" in match
        # Verify case-insensitive match
        contents = [m["content"] for m in result.output]
        assert "Hello World" in contents
        assert "Hello Again" in contents

    @pytest.mark.asyncio
    async def test_search_respects_max_results(self, tool, tmp_path):
        """Test that max_results limits output."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        # Create file with many matching lines
        lines = "\n".join([f"match line {i}" for i in range(50)])
        (session_dir / "many.txt").write_text(lines)

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="match",
                max_results=5,
            )

        assert result.success is True
        assert len(result.output) == 5
        assert result.metadata["truncated"] is True

    @pytest.mark.asyncio
    async def test_search_skips_hidden_files(self, tool, tmp_path):
        """Test that hidden files are skipped."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        (session_dir / ".hidden").write_text("secret match\n")
        (session_dir / "visible.txt").write_text("visible match\n")

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="match",
            )

        assert result.success is True
        assert len(result.output) == 1
        assert result.output[0]["file"] == "visible.txt"

    @pytest.mark.asyncio
    async def test_search_skips_binary_files(self, tool, tmp_path):
        """Test that binary file extensions are skipped."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        (session_dir / "image.png").write_text("match in binary\n")
        (session_dir / "code.py").write_text("match in code\n")

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="match",
            )

        assert result.success is True
        assert len(result.output) == 1
        assert result.output[0]["file"] == "code.py"

    @pytest.mark.asyncio
    async def test_search_outside_workspace_rejected(self, tool, tmp_path):
        """Test that searching outside workspace is rejected."""
        with patch.dict(
            os.environ,
            {
                "SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path),
                "SHANNON_DEV_ALLOW_CWD": "",
            },
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="test",
                path="/etc",
            )

        assert result.success is False
        assert "not allowed" in result.error

    @pytest.mark.asyncio
    async def test_search_nonexistent_directory(self, tool, tmp_path):
        """Test searching a nonexistent subdirectory."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="test",
                path="nonexistent",
            )

        assert result.success is False
        assert "not found" in result.error.lower()

    @pytest.mark.asyncio
    async def test_search_recursive_subdirectories(self, tool, tmp_path):
        """Test that search recurses into subdirectories."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        sub = session_dir / "subdir"
        sub.mkdir()
        (session_dir / "top.txt").write_text("findme top\n")
        (sub / "nested.txt").write_text("findme nested\n")

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="findme",
            )

        assert result.success is True
        assert len(result.output) == 2
        files = {m["file"] for m in result.output}
        assert "top.txt" in files
        assert os.path.join("subdir", "nested.txt") in files

    # --- New: regex support ---

    @pytest.mark.asyncio
    async def test_search_regex_basic(self, tool, tmp_path):
        """Test regex pattern matching."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        (session_dir / "code.py").write_text(
            "def hello_world():\n"
            "def goodbye_world():\n"
            "class MyClass:\n"
        )

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query=r"def \w+_world",
                regex=True,
            )

        assert result.success is True
        assert len(result.output) == 2

    @pytest.mark.asyncio
    async def test_search_regex_false_uses_substring(self, tool, tmp_path):
        """Test that regex=False (default) does plain substring match."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        (session_dir / "data.txt").write_text("def \\w+_world\nhello\n")

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query=r"def \w+_world",
                regex=False,
            )

        assert result.success is True
        assert len(result.output) == 1  # literal match

    @pytest.mark.asyncio
    async def test_search_invalid_regex_returns_error(self, tool, tmp_path):
        """Test that invalid regex pattern returns error."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        (session_dir / "data.txt").write_text("test\n")

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query=r"[invalid(",
                regex=True,
            )

        assert result.success is False
        assert "regex" in result.error.lower() or "pattern" in result.error.lower()

    # --- New: include (glob filter) ---

    @pytest.mark.asyncio
    async def test_search_include_filters_by_extension(self, tool, tmp_path):
        """Test that include parameter filters files by glob pattern."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        (session_dir / "code.py").write_text("findme in python\n")
        (session_dir / "notes.txt").write_text("findme in text\n")
        (session_dir / "data.json").write_text("findme in json\n")

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="findme",
                include="*.py",
            )

        assert result.success is True
        assert len(result.output) == 1
        assert result.output[0]["file"] == "code.py"

    @pytest.mark.asyncio
    async def test_search_include_none_searches_all(self, tool, tmp_path):
        """Test that include=None searches all files."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        (session_dir / "a.py").write_text("match\n")
        (session_dir / "b.txt").write_text("match\n")

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="match",
            )

        assert result.success is True
        assert len(result.output) == 2

    # --- New: context_lines ---

    @pytest.mark.asyncio
    async def test_search_context_lines(self, tool, tmp_path):
        """Test that context_lines returns surrounding lines."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        (session_dir / "code.py").write_text(
            "line1\nline2\nMATCH_HERE\nline4\nline5\n"
        )

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="MATCH_HERE",
                context_lines=1,
            )

        assert result.success is True
        assert len(result.output) == 1
        match = result.output[0]
        # context should include before/after lines
        assert "context" in match
        assert len(match["context"]) == 3  # 1 before + match + 1 after
        assert match["context"][0]["content"] == "line2"
        assert match["context"][1]["content"] == "MATCH_HERE"
        assert match["context"][2]["content"] == "line4"

    @pytest.mark.asyncio
    async def test_search_context_lines_zero_no_context(self, tool, tmp_path):
        """Test that context_lines=0 returns no context field."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        (session_dir / "f.txt").write_text("before\nmatch\nafter\n")

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="match",
                context_lines=0,
            )

        assert result.success is True
        assert len(result.output) == 1
        assert "context" not in result.output[0]

    @pytest.mark.asyncio
    async def test_search_context_lines_at_file_boundary(self, tool, tmp_path):
        """Test context_lines at start/end of file doesn't crash."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        (session_dir / "f.txt").write_text("MATCH\nsecond\n")

        with patch.dict(
            os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}
        ):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="MATCH",
                context_lines=2,
            )

        assert result.success is True
        match = result.output[0]
        assert "context" in match
        # Should have match line + 1 after (no before since match is line 1)
        assert match["context"][0]["content"] == "MATCH"
        assert len(match["context"]) <= 3  # at most 2+1+0


class TestFileReadOffsetLimit:
    """Test FileReadTool offset/limit for line-range reads.

    Uses mock to bypass WASI sandbox so we test the Python implementation directly.
    """

    @pytest.fixture
    def tool(self):
        return FileReadTool()

    @pytest.fixture(autouse=True)
    def _disable_sandbox(self):
        """Disable sandbox for all tests in this class."""
        with patch(
            "llm_service.tools.builtin.file_ops.is_sandbox_enabled", return_value=False
        ):
            yield

    def _make_file(self, session_dir, name="lines.txt", num_lines=10):
        """Helper: create a file with numbered lines."""
        session_dir.mkdir(exist_ok=True)
        content = "\n".join(f"Line {i}" for i in range(1, num_lines + 1)) + "\n"
        f = session_dir / name
        f.write_text(content)
        return f

    @pytest.mark.asyncio
    async def test_offset_only(self, tool, tmp_path):
        """Read from offset to end of file."""
        f = self._make_file(tmp_path / "test-session")
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                offset=8,
            )
        assert result.success is True
        # Lines 8-10 should appear, with line numbers
        assert "8\tLine 8" in result.output
        assert "9\tLine 9" in result.output
        assert "10\tLine 10" in result.output
        # Line 7 should NOT appear
        assert "Line 7" not in result.output
        assert result.metadata["offset"] == 8

    @pytest.mark.asyncio
    async def test_limit_only(self, tool, tmp_path):
        """Read first N lines."""
        f = self._make_file(tmp_path / "test-session")
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                limit=3,
            )
        assert result.success is True
        assert "1\tLine 1" in result.output
        assert "3\tLine 3" in result.output
        assert "Line 4" not in result.output
        assert result.metadata["limit"] == 3

    @pytest.mark.asyncio
    async def test_offset_and_limit(self, tool, tmp_path):
        """Read specific range: lines 4-6."""
        f = self._make_file(tmp_path / "test-session")
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                offset=4,
                limit=3,
            )
        assert result.success is True
        assert "4\tLine 4" in result.output
        assert "5\tLine 5" in result.output
        assert "6\tLine 6" in result.output
        assert "Line 3" not in result.output
        assert "Line 7" not in result.output

    @pytest.mark.asyncio
    async def test_offset_beyond_file(self, tool, tmp_path):
        """Offset past end of file returns empty output."""
        f = self._make_file(tmp_path / "test-session", num_lines=5)
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                offset=100,
            )
        assert result.success is True
        assert result.output == ""

    @pytest.mark.asyncio
    async def test_no_offset_limit_returns_full_content(self, tool, tmp_path):
        """Without offset/limit, returns raw content (no line numbers)."""
        session_dir = tmp_path / "test-session"
        session_dir.mkdir()
        f = session_dir / "plain.txt"
        f.write_text("Hello\nWorld\n")
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
            )
        assert result.success is True
        assert result.output == "Hello\nWorld\n"
        assert "offset" not in result.metadata
        assert "limit" not in result.metadata

    @pytest.mark.asyncio
    async def test_total_lines_in_metadata(self, tool, tmp_path):
        """Metadata should report total_lines regardless of offset/limit."""
        f = self._make_file(tmp_path / "test-session", num_lines=20)
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                offset=5,
                limit=3,
            )
        assert result.success is True
        assert result.metadata["total_lines"] == 20


class TestFileEditTool:
    """Test FileEditTool for text replacement, insertion, deletion."""

    @pytest.fixture
    def tool(self):
        return FileEditTool()

    def _make_file(self, session_dir, name="edit_me.txt", content=""):
        """Helper: create a file with given content."""
        session_dir.mkdir(exist_ok=True)
        f = session_dir / name
        f.write_text(content)
        return f

    @pytest.mark.asyncio
    async def test_simple_replace(self, tool, tmp_path):
        """Replace one occurrence of text."""
        f = self._make_file(
            tmp_path / "test-session",
            content="def old_func():\n    pass\n",
        )
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                old_text="def old_func():",
                new_text="def new_func():",
            )
        assert result.success is True
        assert result.output["message"] == "Replaced 1 occurrence(s)"
        assert f.read_text() == "def new_func():\n    pass\n"

    @pytest.mark.asyncio
    async def test_delete_text(self, tool, tmp_path):
        """Delete text by replacing with empty string."""
        f = self._make_file(
            tmp_path / "test-session",
            content="line1\nDELETE_ME\nline3\n",
        )
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                old_text="DELETE_ME\n",
                new_text="",
            )
        assert result.success is True
        assert f.read_text() == "line1\nline3\n"

    @pytest.mark.asyncio
    async def test_insert_text(self, tool, tmp_path):
        """Insert text by matching an anchor and expanding it."""
        f = self._make_file(
            tmp_path / "test-session",
            content="def main():\n    pass\n",
        )
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                old_text="def main():\n    pass",
                new_text="def main():\n    print('hello')\n    pass",
            )
        assert result.success is True
        assert "print('hello')" in f.read_text()

    @pytest.mark.asyncio
    async def test_multiple_matches_rejected(self, tool, tmp_path):
        """Multiple matches should be rejected when replace_all=false."""
        f = self._make_file(
            tmp_path / "test-session",
            content="foo\nbar\nfoo\nbaz\n",
        )
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                old_text="foo",
                new_text="qux",
            )
        assert result.success is False
        assert "matches 2 locations" in result.error
        # File should be unchanged
        assert f.read_text() == "foo\nbar\nfoo\nbaz\n"

    @pytest.mark.asyncio
    async def test_replace_all(self, tool, tmp_path):
        """replace_all=true should replace all occurrences."""
        f = self._make_file(
            tmp_path / "test-session",
            content="foo\nbar\nfoo\nbaz\n",
        )
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                old_text="foo",
                new_text="qux",
                replace_all=True,
            )
        assert result.success is True
        assert result.output["message"] == "Replaced 2 occurrence(s)"
        assert f.read_text() == "qux\nbar\nqux\nbaz\n"

    @pytest.mark.asyncio
    async def test_old_text_not_found(self, tool, tmp_path):
        """Should fail when old_text doesn't exist in file."""
        f = self._make_file(
            tmp_path / "test-session",
            content="hello world\n",
        )
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                old_text="nonexistent text",
                new_text="replacement",
            )
        assert result.success is False
        assert "not found" in result.error

    @pytest.mark.asyncio
    async def test_file_not_found(self, tool, tmp_path):
        """Should fail when file doesn't exist."""
        (tmp_path / "test-session").mkdir(exist_ok=True)
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(tmp_path / "test-session" / "no_such_file.txt"),
                old_text="a",
                new_text="b",
            )
        assert result.success is False
        assert "not found" in result.error.lower()

    @pytest.mark.asyncio
    async def test_outside_workspace_rejected(self, tool, tmp_path):
        """Should reject edits outside session workspace."""
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
                old_text="root",
                new_text="hacked",
            )
        assert result.success is False
        assert "not allowed" in result.error

    @pytest.mark.asyncio
    async def test_empty_old_text_rejected(self, tool, tmp_path):
        """Should reject empty old_text."""
        f = self._make_file(tmp_path / "test-session", content="content\n")
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                old_text="",
                new_text="something",
            )
        assert result.success is False
        assert "empty" in result.error.lower()

    @pytest.mark.asyncio
    async def test_identical_old_new_rejected(self, tool, tmp_path):
        """Should reject when old_text == new_text."""
        f = self._make_file(tmp_path / "test-session", content="same\n")
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                old_text="same",
                new_text="same",
            )
        assert result.success is False
        assert "identical" in result.error.lower()

    @pytest.mark.asyncio
    async def test_multiline_replace(self, tool, tmp_path):
        """Replace spanning multiple lines."""
        original = "class Foo:\n    def bar(self):\n        return 1\n"
        f = self._make_file(tmp_path / "test-session", content=original)
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                old_text="    def bar(self):\n        return 1",
                new_text="    def bar(self):\n        return 42",
            )
        assert result.success is True
        assert "return 42" in f.read_text()
        assert "return 1" not in f.read_text()

    @pytest.mark.asyncio
    async def test_metadata_fields(self, tool, tmp_path):
        """Verify metadata contains expected fields after edit."""
        f = self._make_file(tmp_path / "test-session", content="aaa bbb ccc\n")
        with patch.dict(os.environ, {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}):
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path=str(f),
                old_text="bbb",
                new_text="BBB",
            )
        assert result.success is True
        assert result.metadata["replacements"] == 1
        assert result.metadata["old_length"] == 3
        assert result.metadata["new_length"] == 3
        assert "file_size_after" in result.metadata
        assert "path" in result.metadata

    def test_tool_metadata(self, tool):
        """Verify tool metadata matches expected values."""
        assert tool.metadata.name == "file_edit"
        assert tool.metadata.category == "file"
        assert tool.metadata.dangerous is True
        assert tool.metadata.session_aware is True

    def test_tool_parameters(self, tool):
        """Verify parameter definitions."""
        params = tool._get_parameters()
        param_names = [p.name for p in params]
        assert "path" in param_names
        assert "old_text" in param_names
        assert "new_text" in param_names
        assert "replace_all" in param_names
        # path, old_text, new_text are required
        required = {p.name for p in params if p.required}
        assert required == {"path", "old_text", "new_text"}
        # replace_all is optional with default false
        ra = next(p for p in params if p.name == "replace_all")
        assert ra.required is False
        assert ra.default is False


class TestFileSearchSandboxProxy:
    """Test FileSearchTool sandbox proxy."""

    @pytest.fixture
    def tool(self):
        return FileSearchTool()

    @pytest.mark.asyncio
    async def test_search_uses_sandbox_when_enabled(self, tool, tmp_path):
        """Test that file_search proxies to sandbox when enabled."""
        with patch("llm_service.tools.builtin.file_ops.is_sandbox_enabled", return_value=True), \
             patch("llm_service.tools.builtin.file_ops.get_sandbox_client") as mock_client:
            mock_instance = mock_client.return_value
            mock_instance.file_search = AsyncMock(return_value=(
                True,
                [{"file": "test.py", "line": 1, "content": "hello"}],
                "",
                {"files_scanned": 1, "matches_found": 1, "truncated": False},
            ))
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                query="hello",
            )
        assert result.success is True
        assert len(result.output) == 1
        mock_instance.file_search.assert_called_once()


class TestFileEditSandboxProxy:
    """Test FileEditTool sandbox proxy."""

    @pytest.mark.asyncio
    async def test_edit_uses_sandbox_when_enabled(self, tmp_path):
        """Test that file_edit proxies to sandbox when enabled."""
        tool = FileEditTool()
        with patch("llm_service.tools.builtin.file_ops.is_sandbox_enabled", return_value=True), \
             patch("llm_service.tools.builtin.file_ops.get_sandbox_client") as mock_client:
            mock_instance = mock_client.return_value
            mock_instance.file_edit = AsyncMock(return_value=(
                True, "...new_func...", "",
                {"replacements": 1, "file_size_after": 100},
            ))
            result = await tool._execute_impl(
                session_context={"session_id": "test-session"},
                path="test.py",
                old_text="old_func",
                new_text="new_func",
            )
        assert result.success is True
        mock_instance.file_edit.assert_called_once()
