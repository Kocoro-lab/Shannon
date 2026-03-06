"""
Test suite for data processing tools: diff_files and json_query.
"""

import asyncio
import json
import os
import pytest
from pathlib import Path
from unittest.mock import patch

from llm_service.tools.builtin.data_tools import (
    DiffFilesTool,
    JsonQueryTool,
    _json_extract,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _run(coro):
    """Run an async function synchronously for testing."""
    return asyncio.get_event_loop().run_until_complete(coro)


def _make_session(tmp_path):
    """Create a session context pointing to tmp_path as workspace."""
    return {"session_id": "test-session"}


def _env_patch(tmp_path):
    """Return a patch dict that points workspaces at tmp_path."""
    return {"SHANNON_SESSION_WORKSPACES_DIR": str(tmp_path)}


# ---------------------------------------------------------------------------
# _json_extract unit tests (pure function, no I/O)
# ---------------------------------------------------------------------------

class TestJsonExtract:
    def test_root(self):
        assert _json_extract({"a": 1}, ".", 100) == [{"a": 1}]

    def test_single_field(self):
        assert _json_extract({"name": "Alice"}, ".name", 100) == ["Alice"]

    def test_nested_field(self):
        data = {"a": {"b": {"c": 42}}}
        assert _json_extract(data, ".a.b.c", 100) == [42]

    def test_array_index(self):
        data = {"items": ["x", "y", "z"]}
        assert _json_extract(data, ".items[1]", 100) == ["y"]

    def test_fan_out(self):
        data = {"users": [{"name": "A"}, {"name": "B"}, {"name": "C"}]}
        assert _json_extract(data, ".users[].name", 100) == ["A", "B", "C"]

    def test_fan_out_with_max(self):
        data = {"items": list(range(50))}
        result = _json_extract(data, ".items[]", 10)
        assert len(result) == 10
        assert result == list(range(10))

    def test_missing_field(self):
        assert _json_extract({"a": 1}, ".b", 100) == []

    def test_index_out_of_range(self):
        assert _json_extract({"items": [1]}, ".items[5]", 100) == []

    def test_empty_expression(self):
        assert _json_extract(42, "", 100) == [42]

    def test_no_leading_dot(self):
        """Expression without leading dot should still work."""
        data = {"name": "test"}
        assert _json_extract(data, "name", 100) == ["test"]

    def test_nested_array_fan(self):
        data = {
            "data": [
                {"tags": ["a", "b"]},
                {"tags": ["c"]},
            ]
        }
        result = _json_extract(data, ".data[].tags[]", 100)
        assert result == ["a", "b", "c"]

    def test_complex_path(self):
        data = {
            "response": {
                "results": [
                    {"id": 1, "meta": {"score": 0.9}},
                    {"id": 2, "meta": {"score": 0.7}},
                ]
            }
        }
        result = _json_extract(data, ".response.results[].meta.score", 100)
        assert result == [0.9, 0.7]


# ---------------------------------------------------------------------------
# DiffFilesTool
# ---------------------------------------------------------------------------

class TestDiffFilesTool:
    def setup_method(self):
        self.tool = DiffFilesTool()

    def test_metadata(self):
        assert self.tool.metadata.name == "diff_files"
        assert self.tool.metadata.category == "file"

    def test_identical_files(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()
            (ws / "a.txt").write_text("hello\nworld\n")
            (ws / "b.txt").write_text("hello\nworld\n")

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path_a="a.txt",
                path_b="b.txt",
            ))

            assert result.success
            assert result.output == "Files are identical"
            assert result.metadata["identical"] is True

    def test_different_files(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()
            (ws / "a.txt").write_text("line1\nline2\nline3\n")
            (ws / "b.txt").write_text("line1\nmodified\nline3\n")

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path_a="a.txt",
                path_b="b.txt",
            ))

            assert result.success
            assert "-line2" in result.output
            assert "+modified" in result.output
            assert result.metadata["identical"] is False
            assert result.metadata["additions"] == 1
            assert result.metadata["deletions"] == 1

    def test_file_not_found(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()
            (ws / "a.txt").write_text("content")

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path_a="a.txt",
                path_b="nonexistent.txt",
            ))

            assert not result.success
            assert "not found" in result.error.lower()

    def test_path_traversal_blocked(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()
            (ws / "a.txt").write_text("ok")

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path_a="a.txt",
                path_b="../../etc/passwd",
            ))

            assert not result.success

    def test_symlink_escape_blocked(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()
            (ws / "a.txt").write_text("line1\nline2\n")

            # Create a file outside the workspace and a symlink inside pointing to it
            outside = tmp_path / "outside.txt"
            outside.write_text("secret\n")
            (ws / "link.txt").symlink_to(outside)

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path_a="a.txt",
                path_b="link.txt",
            ))

            assert not result.success
            assert "symlink" in result.error.lower()

    def test_context_lines(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()
            (ws / "a.txt").write_text("1\n2\n3\n4\n5\n6\n7\n8\n")
            (ws / "b.txt").write_text("1\n2\nX\n4\n5\n6\n7\n8\n")

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path_a="a.txt",
                path_b="b.txt",
                context_lines=0,
            ))

            assert result.success
            # With 0 context lines, output should be minimal
            lines = [l for l in result.output.split("\n") if l.startswith("@@")]
            assert len(lines) == 1


# ---------------------------------------------------------------------------
# JsonQueryTool
# ---------------------------------------------------------------------------

class TestJsonQueryTool:
    def setup_method(self):
        self.tool = JsonQueryTool()

    def test_metadata(self):
        assert self.tool.metadata.name == "json_query"
        assert self.tool.metadata.category == "data"

    def test_simple_field(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()
            (ws / "data.json").write_text(json.dumps({"name": "Alice", "age": 30}))

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path="data.json",
                expression=".name",
            ))

            assert result.success
            assert result.output == "Alice"

    def test_fan_out(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()
            data = {"users": [{"name": "A"}, {"name": "B"}, {"name": "C"}]}
            (ws / "data.json").write_text(json.dumps(data))

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path="data.json",
                expression=".users[].name",
            ))

            assert result.success
            assert result.output == ["A", "B", "C"]
            assert result.metadata["count"] == 3

    def test_nested_path(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()
            data = {"response": {"data": {"value": 42}}}
            (ws / "data.json").write_text(json.dumps(data))

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path="data.json",
                expression=".response.data.value",
            ))

            assert result.success
            assert result.output == 42

    def test_missing_field_returns_empty(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()
            (ws / "data.json").write_text(json.dumps({"a": 1}))

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path="data.json",
                expression=".nonexistent",
            ))

            assert result.success
            assert result.output == []
            assert result.metadata["count"] == 0

    def test_invalid_json(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()
            (ws / "bad.json").write_text("not json at all {{{")

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path="bad.json",
                expression=".field",
            ))

            assert not result.success
            assert "Invalid JSON" in result.error

    def test_file_not_found(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path="missing.json",
                expression=".field",
            ))

            assert not result.success
            assert "not found" in result.error.lower()

    def test_max_results_truncation(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()
            data = {"items": list(range(200))}
            (ws / "big.json").write_text(json.dumps(data))

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path="big.json",
                expression=".items[]",
                max_results=5,
            ))

            assert result.success
            assert len(result.output) == 5
            assert result.metadata["truncated"] is True

    def test_root_expression(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()
            data = {"key": "value"}
            (ws / "data.json").write_text(json.dumps(data))

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path="data.json",
                expression=".",
            ))

            assert result.success
            assert result.output == {"key": "value"}

    def test_array_index(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()
            data = {"items": ["a", "b", "c"]}
            (ws / "data.json").write_text(json.dumps(data))

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path="data.json",
                expression=".items[2]",
            ))

            assert result.success
            assert result.output == "c"

    def test_path_traversal_blocked(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path="../../../etc/passwd",
                expression=".",
            ))

            assert not result.success

    def test_symlink_escape_blocked(self, tmp_path):
        with patch.dict(os.environ, _env_patch(tmp_path)):
            ws = tmp_path / "test-session"
            ws.mkdir()

            outside = tmp_path / "secret.json"
            outside.write_text('{"key": "secret"}')
            (ws / "link.json").symlink_to(outside)

            result = _run(self.tool.execute(
                session_context=_make_session(tmp_path),
                path="link.json",
                expression=".",
            ))

            assert not result.success
            assert "symlink" in result.error.lower()
