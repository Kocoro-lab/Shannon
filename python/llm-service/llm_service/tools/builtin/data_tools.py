"""
Data Processing Tools - diff_files and json_query for swarm agents.

These tools help agents compare files and extract data from JSON without
reading entire files into LLM context.
"""

import difflib
import json
import logging
import os
import re
from pathlib import Path
from typing import Any, Dict, List, Optional

from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult
from .file_ops import _get_session_workspace, _get_allowed_dirs, _is_allowed

logger = logging.getLogger(__name__)

# --- Safety limits ---
_MAX_FILE_SIZE_BYTES = 10 * 1024 * 1024  # 10MB
_MAX_DIFF_OUTPUT_LINES = 500
_MAX_JSON_RESULTS = 1000


def _resolve_and_check(
    file_path: str,
    session_context: Optional[Dict],
) -> tuple[Optional[Path], Optional[ToolResult]]:
    """Resolve a relative path within session workspace and check access.

    Returns (resolved_path, None) on success or (None, error_result) on failure.
    """
    workspace = _get_session_workspace(session_context)
    raw = workspace / file_path

    # Symlink escape check — must check BEFORE resolve() which follows symlinks
    if raw.is_symlink():
        return None, ToolResult(
            success=False, output=None, error="Symlinks are not allowed"
        )

    resolved = raw.resolve()

    allowed_dirs = _get_allowed_dirs(session_context)
    if not any(_is_allowed(resolved, base) for base in allowed_dirs):
        return None, ToolResult(
            success=False,
            output=None,
            error=f"Access denied: {file_path} is outside session workspace",
        )

    if not resolved.exists():
        return None, ToolResult(
            success=False, output=None, error=f"File not found: {file_path}"
        )

    if not resolved.is_file():
        return None, ToolResult(
            success=False, output=None, error=f"Not a file: {file_path}"
        )

    if resolved.stat().st_size > _MAX_FILE_SIZE_BYTES:
        return None, ToolResult(
            success=False,
            output=None,
            error=f"File too large: {file_path} (max {_MAX_FILE_SIZE_BYTES // 1024 // 1024}MB)",
        )

    return resolved, None


# ---------------------------------------------------------------------------
# diff_files
# ---------------------------------------------------------------------------


class DiffFilesTool(Tool):
    """Compare two files and return a unified diff."""

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="diff_files",
            version="1.0.0",
            description=(
                "Compare two files in the session workspace and return a unified diff. "
                "Useful for reviewing changes between file versions."
            ),
            category="file",
            author="Shannon",
            requires_auth=False,
            rate_limit=50,
            timeout_seconds=10,
            memory_limit_mb=256,
            sandboxed=True,
            session_aware=True,
            dangerous=False,
            cost_per_use=0.0,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="path_a",
                type=ToolParameterType.STRING,
                description="Path to the first file (relative to workspace)",
                required=True,
            ),
            ToolParameter(
                name="path_b",
                type=ToolParameterType.STRING,
                description="Path to the second file (relative to workspace)",
                required=True,
            ),
            ToolParameter(
                name="context_lines",
                type=ToolParameterType.INTEGER,
                description="Number of context lines around each change (default: 3)",
                required=False,
                default=3,
                min_value=0,
                max_value=20,
            ),
        ]

    async def _execute_impl(
        self,
        session_context: Optional[Dict] = None,
        observer: Optional[Any] = None,
        **kwargs,
    ) -> ToolResult:
        path_a = kwargs["path_a"]
        path_b = kwargs["path_b"]
        context_lines = kwargs.get("context_lines", 3)

        # Resolve and validate both paths
        resolved_a, err_a = _resolve_and_check(path_a, session_context)
        if err_a:
            return err_a

        resolved_b, err_b = _resolve_and_check(path_b, session_context)
        if err_b:
            return err_b

        try:
            text_a = resolved_a.read_text(encoding="utf-8", errors="replace")
            text_b = resolved_b.read_text(encoding="utf-8", errors="replace")
        except Exception as e:
            return ToolResult(success=False, output=None, error=f"Read error: {e}")

        lines_a = text_a.splitlines(keepends=True)
        lines_b = text_b.splitlines(keepends=True)

        diff = list(
            difflib.unified_diff(
                lines_a,
                lines_b,
                fromfile=path_a,
                tofile=path_b,
                n=context_lines,
            )
        )

        if not diff:
            return ToolResult(
                success=True,
                output="Files are identical",
                metadata={
                    "path_a": path_a,
                    "path_b": path_b,
                    "identical": True,
                    "changes": 0,
                },
            )

        # Truncate very long diffs
        truncated = len(diff) > _MAX_DIFF_OUTPUT_LINES
        if truncated:
            diff = diff[:_MAX_DIFF_OUTPUT_LINES]

        # Count additions and deletions
        additions = sum(1 for l in diff if l.startswith("+") and not l.startswith("+++"))
        deletions = sum(1 for l in diff if l.startswith("-") and not l.startswith("---"))

        output = "".join(diff)
        if truncated:
            output += f"\n... (truncated, showing first {_MAX_DIFF_OUTPUT_LINES} lines)\n"

        return ToolResult(
            success=True,
            output=output,
            metadata={
                "path_a": path_a,
                "path_b": path_b,
                "identical": False,
                "additions": additions,
                "deletions": deletions,
                "truncated": truncated,
            },
        )


# ---------------------------------------------------------------------------
# json_query
# ---------------------------------------------------------------------------

# Regex for parsing dot-path expressions like ".results[0].name" or ".data[].id"
_PATH_TOKEN_RE = re.compile(
    r"""
    \.(\w+)          # .fieldName
    |
    \[(\d+)\]        # [integer_index]
    |
    \[\]             # [] — iterate all elements
    """,
    re.VERBOSE,
)


def _json_extract(data: Any, expression: str, max_results: int) -> list:
    """Walk *data* following a simple dot-path expression.

    Supported syntax:
        .field          — object key lookup
        [N]             — array index
        []              — fan-out over array (produces multiple results)
        .field1.field2  — chained access

    Returns a flat list of matched values (may be empty).
    """
    if not expression or expression == ".":
        return [data]

    # Tokenize
    tokens: list[tuple[str, str | int | None]] = []
    pos = 0
    expr = expression
    # Allow leading dot or not
    if expr.startswith("."):
        pass  # will be consumed by regex
    else:
        expr = "." + expr  # normalise

    for m in _PATH_TOKEN_RE.finditer(expr):
        field = m.group(1)
        idx_str = m.group(2)
        if field is not None:
            tokens.append(("field", field))
        elif idx_str is not None:
            tokens.append(("index", int(idx_str)))
        else:
            tokens.append(("fan", None))

    if not tokens:
        return [data]

    # Walk
    current: list = [data]
    for kind, key in tokens:
        next_level: list = []
        for item in current:
            if kind == "field":
                if isinstance(item, dict) and key in item:
                    next_level.append(item[key])
            elif kind == "index":
                if isinstance(item, (list, tuple)) and -len(item) <= key < len(item):
                    next_level.append(item[key])
            elif kind == "fan":
                if isinstance(item, (list, tuple)):
                    next_level.extend(item)
        current = next_level
        if len(current) > max_results:
            current = current[:max_results]
            break

    return current


class JsonQueryTool(Tool):
    """Query JSON files using dot-path expressions without loading entire content into LLM context."""

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="json_query",
            version="1.0.0",
            description=(
                "Query a JSON file using dot-path expressions (e.g. .results[].name). "
                "Extracts specific fields without loading the full file into context."
            ),
            category="data",
            author="Shannon",
            requires_auth=False,
            rate_limit=50,
            timeout_seconds=10,
            memory_limit_mb=256,
            sandboxed=True,
            session_aware=True,
            dangerous=False,
            cost_per_use=0.0,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="path",
                type=ToolParameterType.STRING,
                description="Path to the JSON file (relative to workspace)",
                required=True,
            ),
            ToolParameter(
                name="expression",
                type=ToolParameterType.STRING,
                description=(
                    "Dot-path expression to extract data. Examples: "
                    "'.results[].name', '.data[0].id', '.config.timeout'"
                ),
                required=True,
            ),
            ToolParameter(
                name="max_results",
                type=ToolParameterType.INTEGER,
                description="Maximum number of results to return (default: 100)",
                required=False,
                default=100,
                min_value=1,
                max_value=_MAX_JSON_RESULTS,
            ),
        ]

    async def _execute_impl(
        self,
        session_context: Optional[Dict] = None,
        observer: Optional[Any] = None,
        **kwargs,
    ) -> ToolResult:
        file_path = kwargs["path"]
        expression = kwargs["expression"]
        max_results = kwargs.get("max_results", 100)

        # Resolve and validate path
        resolved, err = _resolve_and_check(file_path, session_context)
        if err:
            return err

        # Read and parse JSON
        try:
            raw = resolved.read_text(encoding="utf-8")
        except Exception as e:
            return ToolResult(success=False, output=None, error=f"Read error: {e}")

        try:
            data = json.loads(raw)
        except json.JSONDecodeError as e:
            return ToolResult(
                success=False,
                output=None,
                error=f"Invalid JSON in {file_path}: {e}",
            )

        # Execute query
        try:
            results = _json_extract(data, expression, max_results)
        except Exception as e:
            return ToolResult(
                success=False,
                output=None,
                error=f"Query error on expression '{expression}': {e}",
            )

        if not results:
            return ToolResult(
                success=True,
                output=[],
                metadata={
                    "path": file_path,
                    "expression": expression,
                    "count": 0,
                },
            )

        # Unwrap single result
        output = results[0] if len(results) == 1 else results

        return ToolResult(
            success=True,
            output=output,
            metadata={
                "path": file_path,
                "expression": expression,
                "count": len(results),
                "truncated": len(results) >= max_results,
            },
        )
