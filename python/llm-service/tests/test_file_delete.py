"""
Test suite for FileDeleteTool with session isolation.

Tests single-target deletion, glob batch deletion, access control,
and idempotent behavior.
"""

import os
import pytest
from pathlib import Path
from unittest.mock import patch

from llm_service.tools.builtin.file_ops import FileDeleteTool, _get_session_workspace


@pytest.fixture
def tool():
    return FileDeleteTool()


@pytest.fixture
def workspace(tmp_path):
    """Create a temporary workspace with test files."""
    ws = tmp_path / "workspaces" / "test-session"
    ws.mkdir(parents=True)
    return ws


@pytest.fixture
def session_context(workspace):
    return {"session_id": "test-session"}


@pytest.fixture
def env_patch(workspace):
    """Patch environment to use tmp workspace and disable sandbox."""
    parent = workspace.parent
    return patch.dict(os.environ, {
        "SHANNON_SESSION_WORKSPACES_DIR": str(parent),
        "SHANNON_USE_WASI_SANDBOX": "0",
    })


class TestFileDeleteSingleTarget:
    """Tests for single file/directory deletion."""

    @pytest.mark.asyncio
    async def test_delete_single_file(self, tool, workspace, session_context, env_patch):
        """Delete a single file successfully."""
        target = workspace / "temp.txt"
        target.write_text("temporary data")

        with env_patch:
            result = await tool._execute_impl(session_context=session_context, path="temp.txt")

        assert result.success is True
        assert result.metadata["deleted_count"] == 1
        assert not target.exists()

    @pytest.mark.asyncio
    async def test_delete_nonexistent_is_idempotent(self, tool, workspace, session_context, env_patch):
        """Deleting a non-existent file returns success with count 0."""
        with env_patch:
            result = await tool._execute_impl(session_context=session_context, path="does-not-exist.txt")

        assert result.success is True
        assert result.metadata["deleted_count"] == 0

    @pytest.mark.asyncio
    async def test_delete_empty_directory(self, tool, workspace, session_context, env_patch):
        """Delete an empty directory."""
        empty_dir = workspace / "empty-dir"
        empty_dir.mkdir()

        with env_patch:
            result = await tool._execute_impl(session_context=session_context, path="empty-dir")

        assert result.success is True
        assert result.metadata["deleted_count"] == 1
        assert not empty_dir.exists()

    @pytest.mark.asyncio
    async def test_delete_nonempty_directory_fails(self, tool, workspace, session_context, env_patch):
        """Non-empty directory deletion fails with descriptive error."""
        nonempty = workspace / "nonempty"
        nonempty.mkdir()
        (nonempty / "file.txt").write_text("content")

        with env_patch:
            result = await tool._execute_impl(session_context=session_context, path="nonempty")

        assert result.success is False
        assert "not empty" in result.error.lower()

    @pytest.mark.asyncio
    async def test_delete_outside_workspace_rejected(self, tool, workspace, session_context, env_patch):
        """Path traversal outside workspace is rejected."""
        with env_patch:
            result = await tool._execute_impl(session_context=session_context, path="../../../etc/passwd")

        assert result.success is False
        assert "not allowed" in result.error.lower()


class TestFileDeleteGlobBatch:
    """Tests for glob pattern batch deletion."""

    @pytest.mark.asyncio
    async def test_glob_delete_matching_files(self, tool, workspace, session_context, env_patch):
        """Delete all files matching a glob pattern."""
        (workspace / "a.tmp").write_text("temp1")
        (workspace / "b.tmp").write_text("temp2")
        (workspace / "keep.txt").write_text("important")

        with env_patch:
            result = await tool._execute_impl(
                session_context=session_context, path=".", pattern="*.tmp"
            )

        assert result.success is True
        assert result.metadata["deleted_count"] == 2
        assert not (workspace / "a.tmp").exists()
        assert not (workspace / "b.tmp").exists()
        assert (workspace / "keep.txt").exists()

    @pytest.mark.asyncio
    async def test_glob_no_matches(self, tool, workspace, session_context, env_patch):
        """Glob with no matches returns success with count 0."""
        (workspace / "keep.txt").write_text("important")

        with env_patch:
            result = await tool._execute_impl(
                session_context=session_context, path=".", pattern="*.nonexistent"
            )

        assert result.success is True
        assert result.metadata["deleted_count"] == 0

    @pytest.mark.asyncio
    async def test_glob_recursive(self, tool, workspace, session_context, env_patch):
        """Recursive glob deletes files in subdirectories."""
        sub = workspace / "subdir"
        sub.mkdir()
        (workspace / "top.log").write_text("log1")
        (sub / "nested.log").write_text("log2")

        with env_patch:
            result = await tool._execute_impl(
                session_context=session_context, path=".", pattern="*.log", recursive=True
            )

        assert result.success is True
        assert result.metadata["deleted_count"] == 2
        assert not (workspace / "top.log").exists()
        assert not (sub / "nested.log").exists()

    @pytest.mark.asyncio
    async def test_glob_base_not_directory_fails(self, tool, workspace, session_context, env_patch):
        """Glob with non-directory base path fails."""
        (workspace / "file.txt").write_text("data")

        with env_patch:
            result = await tool._execute_impl(
                session_context=session_context, path="file.txt", pattern="*.tmp"
            )

        assert result.success is False
        assert "not a directory" in result.error.lower()

    @pytest.mark.asyncio
    async def test_glob_outside_workspace_rejected(self, tool, workspace, session_context, env_patch):
        """Glob with base path outside workspace is rejected."""
        with env_patch:
            result = await tool._execute_impl(
                session_context=session_context, path="../../", pattern="*"
            )

        assert result.success is False
        assert "not allowed" in result.error.lower()
