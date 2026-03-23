"""
File Operation Tools - Safe file read/write/edit operations with session isolation
"""

import fnmatch
import logging
import os
import json
import re
import yaml
import aiofiles

logger = logging.getLogger(__name__)
from pathlib import Path
from typing import Any, Dict, List, Optional

from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult
from .sandbox_client import is_sandbox_enabled, get_sandbox_client


def _validate_session_id(session_id: str) -> str:
    """Validate session_id to prevent path traversal attacks.

    Args:
        session_id: Raw session ID from request

    Returns:
        Sanitized session ID safe for use in paths

    Raises:
        ValueError: If session_id contains path traversal attempts or invalid characters
    """
    if not session_id:
        return "default"

    # Limit length to prevent filesystem issues (check early)
    if len(session_id) > 128:
        raise ValueError("Invalid session_id: too long (max 128 chars)")

    # SECURITY: Match Go/Rust validation - ASCII alphanumeric + hyphen + underscore only.
    # No period allowed — prevents hidden files and aligns with isValidSessionID in lead_file_read.go.
    if not all(c.isascii() and (c.isalnum() or c in "-_") for c in session_id):
        raise ValueError(
            "Invalid session_id: must contain only ASCII alphanumeric, hyphen, or underscore"
        )

    # Block path traversal patterns (defense in depth — period not in allowlist,
    # but guard against future regressions)
    if ".." in session_id:
        raise ValueError("Invalid session_id: path traversal not allowed")

    return session_id


def _validate_user_id(user_id: str) -> str:
    """Validate user_id to prevent path traversal attacks.

    Args:
        user_id: Raw user ID from request/session context.

    Returns:
        Sanitized user ID safe for filesystem paths.

    Raises:
        ValueError: If user_id contains traversal attempts or invalid characters.
    """
    if not isinstance(user_id, str) or not user_id:
        raise ValueError("Invalid user_id: empty")

    if len(user_id) > 128:
        raise ValueError("Invalid user_id: too long (max 128 chars)")

    if not all(c.isascii() and (c.isalnum() or c in "-_") for c in user_id):
        raise ValueError(
            "Invalid user_id: must contain only ASCII alphanumeric, hyphen, or underscore"
        )

    if ".." in user_id or user_id.startswith("."):
        raise ValueError("Invalid user_id: path traversal not allowed")

    return user_id


def _get_session_workspace(session_context: Optional[Dict] = None) -> Path:
    """Get or create session workspace directory.

    Args:
        session_context: Optional session context containing session_id

    Returns:
        Path to session workspace directory (created if needed)

    Raises:
        ValueError: If session_id is invalid
    """
    raw_session_id = (session_context or {}).get("session_id", "default")
    session_id = _validate_session_id(raw_session_id)

    base_dir = Path(
        os.getenv("SHANNON_SESSION_WORKSPACES_DIR", "/tmp/shannon-sessions")
    ).resolve()
    session_workspace = base_dir / session_id

    # Double-check the resolved path is within base_dir (defense in depth)
    session_workspace_resolved = session_workspace.resolve()
    if not str(session_workspace_resolved).startswith(str(base_dir)):
        raise ValueError(f"Invalid session_id: path escape attempt detected")

    session_workspace.mkdir(parents=True, exist_ok=True)
    return session_workspace


def _get_allowed_dirs(session_context: Optional[Dict] = None) -> List[Path]:
    """Get list of allowed directories for file operations.

    Args:
        session_context: Optional session context containing session_id

    Returns:
        List of allowed base directories
    """
    allowed_dirs = [_get_session_workspace(session_context)]

    # Add user persistent memory directory if user_id is available
    user_id = (session_context or {}).get("user_id", "")
    if user_id:
        validated_user_id = _validate_user_id(user_id)
        memory_base = Path(
            os.getenv("SHANNON_USER_MEMORY_DIR", "/tmp/shannon-users")
        ).resolve()
        memory_dir = memory_base / validated_user_id / "memory"
        # Create memory dir on first access (Rust MemoryManager may not have run yet)
        memory_dir.mkdir(parents=True, exist_ok=True)
        allowed_dirs.append(memory_dir)

    # Add SHANNON_WORKSPACE if set
    if workspace := os.getenv("SHANNON_WORKSPACE"):
        allowed_dirs.append(Path(workspace).resolve())

    # Dev-only: allow cwd when explicitly enabled
    if os.getenv("SHANNON_DEV_ALLOW_CWD") in ("1", "true", "yes"):
        allowed_dirs.append(Path.cwd().resolve())

    # Legacy /tmp support - DISABLED by default for session isolation security
    # Enable only if explicitly needed via SHANNON_ALLOW_GLOBAL_TMP=1
    if os.getenv("SHANNON_ALLOW_GLOBAL_TMP") in ("1", "true", "yes"):
        allowed_dirs.append(Path("/tmp").resolve())

    return allowed_dirs


def _is_allowed(target: Path, base: Path) -> bool:
    """Check if target path is within base directory.

    Args:
        target: Path to check (should be resolved)
        base: Allowed base directory (should be resolved)

    Returns:
        True if target is within base, False otherwise
    """
    try:
        target.relative_to(base)
        return True
    except ValueError:
        return False


class FileReadTool(Tool):
    """
    Safe file reading tool with sandboxing support and session isolation
    """

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="file_read",
            version="1.0.0",
            description="Read contents of a file from session workspace",
            category="file",
            author="Shannon",
            requires_auth=False,
            rate_limit=100,
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
                description="Path to the file to read",
                required=True,
            ),
            ToolParameter(
                name="offset",
                type=ToolParameterType.INTEGER,
                description="Line number to start reading from (1-based). Omit to read from beginning.",
                required=False,
                default=0,
            ),
            ToolParameter(
                name="limit",
                type=ToolParameterType.INTEGER,
                description="Maximum number of lines to read. Omit to read entire file.",
                required=False,
                default=0,
            ),
            ToolParameter(
                name="encoding",
                type=ToolParameterType.STRING,
                description="File encoding",
                required=False,
                default="utf-8",
                enum=["utf-8", "ascii", "latin-1"],
            ),
            ToolParameter(
                name="max_size_mb",
                type=ToolParameterType.INTEGER,
                description="Maximum file size in MB to read",
                required=False,
                default=10,
                min_value=1,
                max_value=100,
            ),
        ]

    async def _execute_impl(
        self,
        session_context: Optional[Dict] = None,
        observer: Optional[Any] = None,
        **kwargs,
    ) -> ToolResult:
        """
        Read file contents safely with session isolation.

        Args:
            session_context: Optional session context containing session_id
            observer: Optional callback for status updates (unused)
            **kwargs: Tool parameters (path, encoding, max_size_mb)
        """
        file_path = kwargs["path"]
        encoding = kwargs.get("encoding", "utf-8")
        max_size_mb = kwargs.get("max_size_mb", 10)
        offset = kwargs.get("offset", 0)
        limit = kwargs.get("limit", 0)

        # Proxy to WASI sandbox if enabled
        if is_sandbox_enabled():
            session_id = (session_context or {}).get("session_id", "default")
            user_id = (session_context or {}).get("user_id", "")
            client = get_sandbox_client()
            success, content, error, metadata = await client.file_read(
                session_id=session_id,
                user_id=user_id,
                path=file_path,
                max_bytes=max_size_mb * 1024 * 1024,
                encoding=encoding,
            )
            if success:
                logger.info(
                    "Sandbox file_read success",
                    extra={
                        "session_id": session_id,
                        "path": file_path,
                        "sandbox_enabled": True,
                    },
                )
                # Try to parse JSON/YAML like the Python implementation
                if file_path.endswith(".json"):
                    try:
                        content = json.loads(content)
                    except json.JSONDecodeError:
                        pass
                elif file_path.endswith((".yaml", ".yml")):
                    try:
                        content = yaml.safe_load(content)
                    except yaml.YAMLError:
                        pass
                return ToolResult(
                    success=True,
                    output=content,
                    metadata={"path": file_path, **metadata},
                )
            else:
                logger.warning(
                    "Sandbox file_read failed",
                    extra={
                        "session_id": session_id,
                        "path": file_path,
                        "sandbox_enabled": True,
                        "error": error,
                    },
                )
                return ToolResult(success=False, output=None, error=error)

        # Fall through to Python implementation if sandbox not enabled
        try:
            # Validate path
            path = Path(file_path)

            # Resolve canonical path to avoid symlink escapes
            try:
                path_absolute = path.resolve(strict=True)
            except FileNotFoundError:
                return ToolResult(
                    success=False, output=None, error=f"File not found: {file_path}"
                )

            # Get allowed directories based on session context
            allowed_dirs = _get_allowed_dirs(session_context)

            if not any(_is_allowed(path_absolute, base) for base in allowed_dirs):
                session_id = (session_context or {}).get("session_id", "default")
                logger.warning(
                    "file_read access denied",
                    extra={
                        "session_id": session_id,
                        "path": str(path_absolute),
                        "sandbox_enabled": is_sandbox_enabled(),
                    },
                )
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Reading {path_absolute} is not allowed. Use session workspace.",
                )

            # Check if it's a file (not directory)
            if not path_absolute.is_file():
                return ToolResult(
                    success=False, output=None, error=f"Path is not a file: {file_path}"
                )

            # Check file size
            file_size_mb = path_absolute.stat().st_size / (1024 * 1024)
            if file_size_mb > max_size_mb:
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"File too large: {file_size_mb:.2f}MB (max: {max_size_mb}MB)",
                )

            # Read file
            async with aiofiles.open(path, mode="r", encoding=encoding) as f:
                content = await f.read()

            file_extension = path.suffix.lower()
            total_lines = content.count("\n") + (1 if content and not content.endswith("\n") else 0)

            # Apply offset/limit for line-range reads
            if offset > 0 or limit > 0:
                lines = content.splitlines(keepends=True)
                start = max(0, offset - 1)  # 1-based to 0-based
                end = start + limit if limit > 0 else len(lines)
                selected = lines[start:end]
                # Format with line numbers
                numbered = []
                for i, line in enumerate(selected, start=start + 1):
                    numbered.append(f"{i:>6}\t{line.rstrip()}")
                parsed_content = "\n".join(numbered)
            else:
                # Full file read — detect and parse structured formats
                parsed_content = content
                if file_extension == ".json":
                    try:
                        parsed_content = json.loads(content)
                    except json.JSONDecodeError:
                        pass
                elif file_extension in [".yaml", ".yml"]:
                    try:
                        parsed_content = yaml.safe_load(content)
                    except yaml.YAMLError:
                        pass

            session_id = (session_context or {}).get("session_id", "default")
            logger.info(
                "file_read success",
                extra={
                    "session_id": session_id,
                    "path": str(path_absolute),
                    "sandbox_enabled": is_sandbox_enabled(),
                },
            )
            metadata = {
                "path": str(path_absolute),
                "size_bytes": path_absolute.stat().st_size,
                "encoding": encoding,
                "file_type": file_extension,
                "total_lines": total_lines,
            }
            if offset > 0:
                metadata["offset"] = offset
            if limit > 0:
                metadata["limit"] = limit
            return ToolResult(
                success=True,
                output=parsed_content,
                metadata=metadata,
            )

        except UnicodeDecodeError:
            return ToolResult(
                success=False,
                output=None,
                error=f"Unable to decode file with encoding: {encoding}",
            )
        except Exception as e:
            return ToolResult(
                success=False, output=None, error=f"Error reading file: {str(e)}"
            )


class FileWriteTool(Tool):
    """
    Safe file writing tool with sandboxing support and session isolation
    """

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="file_write",
            version="1.0.0",
            description="Write content to a file in session workspace",
            category="file",
            author="Shannon",
            requires_auth=True,  # Writing requires auth
            rate_limit=50,
            timeout_seconds=10,
            memory_limit_mb=256,
            sandboxed=True,
            session_aware=True,
            dangerous=True,  # File writing is potentially dangerous
            cost_per_use=0.001,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="path",
                type=ToolParameterType.STRING,
                description="Path where to write the file",
                required=True,
            ),
            ToolParameter(
                name="content",
                type=ToolParameterType.STRING,
                description="Content to write to the file",
                required=True,
            ),
            ToolParameter(
                name="mode",
                type=ToolParameterType.STRING,
                description="Write mode: 'overwrite' replaces existing file, 'append' adds to end",
                required=False,
                default="overwrite",
                enum=["overwrite", "append"],
            ),
            ToolParameter(
                name="encoding",
                type=ToolParameterType.STRING,
                description="File encoding",
                required=False,
                default="utf-8",
                enum=["utf-8", "ascii", "latin-1"],
            ),
            ToolParameter(
                name="create_dirs",
                type=ToolParameterType.BOOLEAN,
                description="Create parent directories if they don't exist",
                required=False,
                default=True,
            ),
        ]

    async def _execute_impl(
        self,
        session_context: Optional[Dict] = None,
        observer: Optional[Any] = None,
        **kwargs,
    ) -> ToolResult:
        """
        Write content to file safely with session isolation.

        Args:
            session_context: Optional session context containing session_id
            observer: Optional callback for status updates (unused)
            **kwargs: Tool parameters (path, content, mode, encoding, create_dirs)
        """
        file_path = kwargs["path"]
        content = kwargs["content"]
        mode = kwargs.get("mode", "overwrite")
        encoding = kwargs.get("encoding", "utf-8")
        create_dirs = kwargs.get("create_dirs", True)

        # Proxy to WASI sandbox if enabled
        if is_sandbox_enabled():
            session_id = (session_context or {}).get("session_id", "default")
            user_id = (session_context or {}).get("user_id", "")
            client = get_sandbox_client()
            success, bytes_written, error, metadata = await client.file_write(
                session_id=session_id,
                user_id=user_id,
                path=file_path,
                content=content,
                append=(mode == "append"),
                create_dirs=create_dirs,
                encoding=encoding,
            )
            if success:
                logger.info(
                    "Sandbox file_write success",
                    extra={
                        "session_id": session_id,
                        "path": file_path,
                        "sandbox_enabled": True,
                        "content_length": len(content),
                    },
                )
                return ToolResult(
                    success=True,
                    output=file_path,
                    metadata={
                        "path": file_path,
                        "size_bytes": bytes_written,
                        "mode": mode,
                        "encoding": encoding,
                        "created_dirs": create_dirs,
                        **metadata,
                    },
                )
            else:
                logger.warning(
                    "Sandbox file_write failed",
                    extra={
                        "session_id": session_id,
                        "path": file_path,
                        "sandbox_enabled": True,
                        "error": error,
                    },
                )
                return ToolResult(success=False, output=None, error=error)

        # Fall through to Python implementation if sandbox not enabled
        try:
            path = Path(file_path)

            # Canonicalize to avoid symlink escapes (don't use strict=True for writes)
            path_absolute = path.resolve()

            # Get allowed directories based on session context
            allowed_dirs = _get_allowed_dirs(session_context)

            if not any(_is_allowed(path_absolute, base) for base in allowed_dirs):
                session_id = (session_context or {}).get("session_id", "default")
                logger.warning(
                    "file_write access denied",
                    extra={
                        "session_id": session_id,
                        "path": str(path_absolute),
                        "sandbox_enabled": is_sandbox_enabled(),
                    },
                )
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Writing to {path_absolute} is not allowed. Use session workspace.",
                )

            # Create parent directories if requested
            if create_dirs:
                path.parent.mkdir(parents=True, exist_ok=True)
            elif not path.parent.exists():
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Parent directory does not exist: {path.parent}",
                )

            # Determine write mode
            write_mode = "w" if mode == "overwrite" else "a"

            # Write file
            async with aiofiles.open(path, mode=write_mode, encoding=encoding) as f:
                await f.write(content)

            # Get file stats after writing
            stats = path.stat()

            session_id = (session_context or {}).get("session_id", "default")
            logger.info(
                "file_write success",
                extra={
                    "session_id": session_id,
                    "path": str(path),
                    "sandbox_enabled": is_sandbox_enabled(),
                    "content_length": len(content),
                },
            )
            return ToolResult(
                success=True,
                output=str(path),
                metadata={
                    "path": str(path),
                    "size_bytes": stats.st_size,
                    "mode": mode,
                    "encoding": encoding,
                    "created_dirs": create_dirs,
                },
            )

        except Exception as e:
            return ToolResult(
                success=False, output=None, error=f"Error writing file: {str(e)}"
            )


class FileListTool(Tool):
    """
    List files in a directory with session isolation
    """

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="file_list",
            version="1.0.0",
            description="List files in a directory within session workspace",
            category="file",
            author="Shannon",
            requires_auth=False,
            rate_limit=100,
            timeout_seconds=5,
            memory_limit_mb=128,
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
                description="Directory path to list",
                required=True,
            ),
            ToolParameter(
                name="pattern",
                type=ToolParameterType.STRING,
                description="File pattern to match (e.g., '*.txt', '*.py')",
                required=False,
                default="*",
            ),
            ToolParameter(
                name="recursive",
                type=ToolParameterType.BOOLEAN,
                description="List files recursively in subdirectories",
                required=False,
                default=False,
            ),
            ToolParameter(
                name="include_hidden",
                type=ToolParameterType.BOOLEAN,
                description="Include hidden files (starting with .)",
                required=False,
                default=False,
            ),
        ]

    async def _execute_impl(
        self,
        session_context: Optional[Dict] = None,
        observer: Optional[Any] = None,
        **kwargs,
    ) -> ToolResult:
        """
        List files in directory with session isolation.

        Args:
            session_context: Optional session context containing session_id
            observer: Optional callback for status updates (unused)
            **kwargs: Tool parameters (path, pattern, recursive, include_hidden)
        """
        dir_path = kwargs["path"]
        pattern = kwargs.get("pattern", "*")
        recursive = kwargs.get("recursive", False)
        include_hidden = kwargs.get("include_hidden", False)

        # Proxy to WASI sandbox if enabled
        if is_sandbox_enabled():
            session_id = (session_context or {}).get("session_id", "default")
            user_id = (session_context or {}).get("user_id", "")
            client = get_sandbox_client()
            success, entries, error, metadata = await client.file_list(
                session_id=session_id,
                user_id=user_id,
                path=dir_path,
                pattern=pattern,
                recursive=recursive,
                include_hidden=include_hidden,
            )
            if success:
                logger.info(
                    "Sandbox file_list success",
                    extra={
                        "session_id": session_id,
                        "path": dir_path,
                        "sandbox_enabled": True,
                    },
                )
                return ToolResult(
                    success=True,
                    output=entries,
                    metadata={
                        "directory": dir_path,
                        "pattern": pattern,
                        "recursive": recursive,
                        **metadata,
                    },
                )
            else:
                logger.warning(
                    "Sandbox file_list failed",
                    extra={
                        "session_id": session_id,
                        "path": dir_path,
                        "sandbox_enabled": True,
                        "error": error,
                    },
                )
                return ToolResult(success=False, output=None, error=error)

        # Fall through to Python implementation if sandbox not enabled
        try:
            path = Path(dir_path)

            # Resolve and validate path
            try:
                path_absolute = path.resolve(strict=True)
            except FileNotFoundError:
                return ToolResult(
                    success=False, output=None, error=f"Directory not found: {dir_path}"
                )

            # Get allowed directories based on session context
            allowed_dirs = _get_allowed_dirs(session_context)

            if not any(_is_allowed(path_absolute, base) for base in allowed_dirs):
                session_id = (session_context or {}).get("session_id", "default")
                logger.warning(
                    "file_list access denied",
                    extra={
                        "session_id": session_id,
                        "path": str(path_absolute),
                        "sandbox_enabled": is_sandbox_enabled(),
                    },
                )
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Listing {path_absolute} is not allowed. Use session workspace.",
                )

            if not path_absolute.is_dir():
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Path is not a directory: {dir_path}",
                )

            # List files
            files = []

            if recursive:
                # Use rglob for recursive search
                file_iter = path.rglob(pattern)
            else:
                file_iter = path.glob(pattern)

            for file_path in file_iter:
                # Skip hidden files if not requested
                if not include_hidden and file_path.name.startswith("."):
                    continue

                if file_path.is_file():
                    files.append(
                        {
                            "name": file_path.name,
                            "path": str(file_path),
                            "size_bytes": file_path.stat().st_size,
                            "is_file": True,
                        }
                    )
                elif file_path.is_dir():
                    files.append(
                        {
                            "name": file_path.name,
                            "path": str(file_path),
                            "is_file": False,
                        }
                    )

            session_id = (session_context or {}).get("session_id", "default")
            logger.info(
                "file_list success",
                extra={
                    "session_id": session_id,
                    "path": str(path),
                    "sandbox_enabled": is_sandbox_enabled(),
                },
            )
            return ToolResult(
                success=True,
                output=files,
                metadata={
                    "directory": str(path),
                    "pattern": pattern,
                    "recursive": recursive,
                    "file_count": sum(1 for f in files if f["is_file"]),
                    "dir_count": sum(1 for f in files if not f["is_file"]),
                },
            )

        except Exception as e:
            return ToolResult(
                success=False, output=None, error=f"Error listing directory: {str(e)}"
            )


# Binary file extensions to skip during content search
_BINARY_EXTENSIONS = frozenset({
    ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".svg", ".webp",
    ".mp3", ".mp4", ".wav", ".avi", ".mov", ".mkv", ".flac",
    ".zip", ".tar", ".gz", ".bz2", ".xz", ".7z", ".rar",
    ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
    ".exe", ".dll", ".so", ".dylib", ".o", ".a", ".pyc", ".pyo",
    ".wasm", ".class", ".jar",
    ".sqlite", ".db", ".bin", ".dat",
})


class FileSearchTool(Tool):
    """
    Search file contents in the workspace for a text query (grep equivalent).
    Returns matching lines with file paths and line numbers.
    """

    # Safety limits
    _MAX_QUERY_LENGTH = 200
    _MAX_FILES_TO_SCAN = 1000
    _MAX_FILE_SIZE_BYTES = 500 * 1024  # 500KB
    _MAX_CONTEXT_LINES = 5

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="file_search",
            version="1.0.0",
            description="Search file contents in the workspace for a text query (grep equivalent). Returns matching lines with file paths and line numbers.",
            category="file",
            author="Shannon",
            requires_auth=False,
            rate_limit=50,
            timeout_seconds=15,
            memory_limit_mb=256,
            sandboxed=True,
            session_aware=True,
            dangerous=False,
            cost_per_use=0.0,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="query",
                type=ToolParameterType.STRING,
                description="Text to search for (case-insensitive, max 200 chars)",
                required=True,
            ),
            ToolParameter(
                name="path",
                type=ToolParameterType.STRING,
                description="Directory to search in (default: workspace root)",
                required=False,
                default=".",
            ),
            ToolParameter(
                name="max_results",
                type=ToolParameterType.INTEGER,
                description="Maximum matching lines to return (default: 20)",
                required=False,
                default=20,
            ),
            ToolParameter(
                name="regex",
                type=ToolParameterType.BOOLEAN,
                description="Treat query as a regular expression (default: false)",
                required=False,
                default=False,
            ),
            ToolParameter(
                name="include",
                type=ToolParameterType.STRING,
                description="Glob pattern to filter files (e.g., '*.py', '*.txt')",
                required=False,
                default=None,
            ),
            ToolParameter(
                name="context_lines",
                type=ToolParameterType.INTEGER,
                description="Number of lines before/after each match to include (0-5, default: 0)",
                required=False,
                default=0,
            ),
        ]

    async def _execute_impl(
        self,
        session_context: Optional[Dict] = None,
        observer: Optional[Any] = None,
        **kwargs,
    ) -> ToolResult:
        """
        Search file contents for a text query with session isolation.

        Args:
            session_context: Optional session context containing session_id
            observer: Optional callback for status updates (unused)
            **kwargs: Tool parameters (query, path, max_results)
        """
        query = kwargs.get("query", "")
        search_path = kwargs.get("path", ".")
        max_results = kwargs.get("max_results", 20)
        use_regex = kwargs.get("regex", False)
        include = kwargs.get("include", None)
        context_lines = min(kwargs.get("context_lines", 0), self._MAX_CONTEXT_LINES)

        # Proxy to WASI sandbox if enabled
        if is_sandbox_enabled():
            session_id = (session_context or {}).get("session_id", "default")
            client = get_sandbox_client()
            success, matches, error, metadata = await client.file_search(
                session_id=session_id,
                query=query,
                path=search_path,
                max_results=max_results,
                regex=use_regex,
                include=include or "",
                context_lines=context_lines,
            )
            if success:
                logger.info("Sandbox file_search success", extra={"session_id": session_id, "query": query, "sandbox_enabled": True})
                return ToolResult(success=True, output=matches, metadata={"query": query, "directory": search_path, **metadata})
            else:
                logger.warning("Sandbox file_search failed", extra={"session_id": session_id, "query": query, "sandbox_enabled": True, "error": error})
                return ToolResult(success=False, output=None, error=error)

        # Validate query
        if not query or not query.strip():
            return ToolResult(
                success=False, output=None, error="Search query cannot be empty"
            )

        if len(query) > self._MAX_QUERY_LENGTH:
            return ToolResult(
                success=False,
                output=None,
                error=f"Search query too long (max {self._MAX_QUERY_LENGTH} chars)",
            )

        # Compile regex or prepare substring matcher
        if use_regex:
            try:
                pattern = re.compile(query, re.IGNORECASE)
            except re.error as e:
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Invalid regex pattern: {e}",
                )
            match_fn = lambda line: pattern.search(line) is not None
        else:
            query_lower = query.lower()
            match_fn = lambda line: query_lower in line.lower()

        try:
            # Resolve workspace directory
            workspace = _get_session_workspace(session_context)

            # Resolve search path relative to workspace
            if search_path in (".", "", None):
                search_dir = workspace
            else:
                search_dir = (workspace / search_path).resolve()

            # Verify search dir is within allowed directories
            allowed_dirs = _get_allowed_dirs(session_context)
            if not any(_is_allowed(search_dir, base) for base in allowed_dirs):
                session_id = (session_context or {}).get("session_id", "default")
                logger.warning(
                    "file_search access denied",
                    extra={
                        "session_id": session_id,
                        "path": str(search_dir),
                        "sandbox_enabled": is_sandbox_enabled(),
                    },
                )
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Searching {search_dir} is not allowed. Use session workspace.",
                )

            if not search_dir.exists():
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Directory not found: {search_path}",
                )

            if not search_dir.is_dir():
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Path is not a directory: {search_path}",
                )

            # Walk files and search contents
            matches = []
            files_scanned = 0

            for file_path in sorted(search_dir.rglob("*")):
                if len(matches) >= max_results:
                    break

                if files_scanned >= self._MAX_FILES_TO_SCAN:
                    break

                # Skip directories
                if not file_path.is_file():
                    continue

                # Skip hidden files/directories
                parts = file_path.relative_to(search_dir).parts
                if any(part.startswith(".") for part in parts):
                    continue

                # Skip binary files by extension
                if file_path.suffix.lower() in _BINARY_EXTENSIONS:
                    continue

                # Apply include glob filter on filename
                if include and not fnmatch.fnmatch(file_path.name, include):
                    continue

                # Skip files that are too large
                try:
                    if file_path.stat().st_size > self._MAX_FILE_SIZE_BYTES:
                        continue
                except OSError:
                    continue

                files_scanned += 1

                # Read and search file contents
                try:
                    async with aiofiles.open(
                        file_path, mode="r", encoding="utf-8", errors="ignore"
                    ) as f:
                        if context_lines > 0:
                            # Read all lines for context support
                            all_lines = [
                                l.rstrip("\n\r") async for l in f
                            ]
                            for line_idx, line in enumerate(all_lines):
                                if match_fn(line):
                                    rel_path = str(
                                        file_path.relative_to(workspace)
                                    )
                                    start = max(0, line_idx - context_lines)
                                    end = min(
                                        len(all_lines),
                                        line_idx + context_lines + 1,
                                    )
                                    ctx = [
                                        {
                                            "line": start + i + 1,
                                            "content": all_lines[start + i],
                                        }
                                        for i in range(end - start)
                                    ]
                                    matches.append(
                                        {
                                            "file": rel_path,
                                            "line": line_idx + 1,
                                            "content": line,
                                            "context": ctx,
                                        }
                                    )
                                    if len(matches) >= max_results:
                                        break
                        else:
                            line_num = 0
                            async for line in f:
                                line_num += 1
                                if match_fn(line.rstrip("\n\r")):
                                    rel_path = str(
                                        file_path.relative_to(workspace)
                                    )
                                    matches.append(
                                        {
                                            "file": rel_path,
                                            "line": line_num,
                                            "content": line.rstrip("\n\r"),
                                        }
                                    )
                                    if len(matches) >= max_results:
                                        break
                except (UnicodeDecodeError, OSError, PermissionError):
                    # Skip files that can't be read as text
                    continue

            session_id = (session_context or {}).get("session_id", "default")
            logger.info(
                "file_search success",
                extra={
                    "session_id": session_id,
                    "query": query,
                    "path": str(search_dir),
                    "files_scanned": files_scanned,
                    "matches_found": len(matches),
                    "sandbox_enabled": is_sandbox_enabled(),
                },
            )

            return ToolResult(
                success=True,
                output=matches,
                metadata={
                    "query": query,
                    "directory": str(search_dir),
                    "files_scanned": files_scanned,
                    "matches_found": len(matches),
                    "max_results": max_results,
                    "truncated": len(matches) >= max_results,
                },
            )

        except Exception as e:
            return ToolResult(
                success=False,
                output=None,
                error=f"Error searching files: {str(e)}",
            )


class FileEditTool(Tool):
    """
    Edit file contents by replacing exact text matches.
    Supports insert (anchor + new text), delete (old_text → empty), and replace.
    Uses string matching (not line numbers) for safe concurrent edits.
    """

    _MAX_OLD_TEXT_LENGTH = 10000
    _MAX_NEW_TEXT_LENGTH = 50000
    _MAX_FILE_SIZE_BYTES = 10 * 1024 * 1024  # 10MB

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="file_edit",
            version="1.0.0",
            description=(
                "Edit a file by replacing exact text. Use old_text to match existing content "
                "and new_text to replace it. Set new_text to empty string to delete. "
                "old_text must match exactly one location in the file (unless replace_all=true)."
            ),
            category="file",
            author="Shannon",
            requires_auth=True,
            rate_limit=50,
            timeout_seconds=10,
            memory_limit_mb=256,
            sandboxed=True,
            session_aware=True,
            dangerous=True,
            cost_per_use=0.001,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="path",
                type=ToolParameterType.STRING,
                description="Path to the file to edit",
                required=True,
            ),
            ToolParameter(
                name="old_text",
                type=ToolParameterType.STRING,
                description="Exact text to find and replace. Must match file content exactly (including whitespace/indentation).",
                required=True,
            ),
            ToolParameter(
                name="new_text",
                type=ToolParameterType.STRING,
                description="Text to replace old_text with. Use empty string to delete the matched text.",
                required=True,
            ),
            ToolParameter(
                name="replace_all",
                type=ToolParameterType.BOOLEAN,
                description="Replace all occurrences (default: false, requires unique match)",
                required=False,
                default=False,
            ),
        ]

    async def _execute_impl(
        self,
        session_context: Optional[Dict] = None,
        observer: Optional[Any] = None,
        **kwargs,
    ) -> ToolResult:
        file_path = kwargs["path"]
        old_text = kwargs["old_text"]
        new_text = kwargs["new_text"]
        replace_all = kwargs.get("replace_all", False)

        if not old_text:
            return ToolResult(success=False, output=None, error="old_text cannot be empty")

        if old_text == new_text:
            return ToolResult(success=False, output=None, error="old_text and new_text are identical")

        if len(old_text) > self._MAX_OLD_TEXT_LENGTH:
            return ToolResult(
                success=False, output=None,
                error=f"old_text too long ({len(old_text)} chars, max {self._MAX_OLD_TEXT_LENGTH})",
            )
        if len(new_text) > self._MAX_NEW_TEXT_LENGTH:
            return ToolResult(
                success=False, output=None,
                error=f"new_text too long ({len(new_text)} chars, max {self._MAX_NEW_TEXT_LENGTH})",
            )

        # Proxy to WASI sandbox if enabled
        if is_sandbox_enabled():
            session_id = (session_context or {}).get("session_id", "default")
            client = get_sandbox_client()
            success, snippet, error, metadata = await client.file_edit(
                session_id=session_id,
                path=file_path,
                old_text=old_text,
                new_text=new_text,
                replace_all=replace_all,
            )
            if success:
                logger.info("Sandbox file_edit success", extra={"session_id": session_id, "path": file_path, "sandbox_enabled": True})
                return ToolResult(success=True, output={"message": f"Replaced {metadata.get('replacements', 1)} occurrence(s)", "snippet": snippet}, metadata={"path": file_path, **metadata})
            else:
                logger.warning("Sandbox file_edit failed", extra={"session_id": session_id, "path": file_path, "sandbox_enabled": True, "error": error})
                return ToolResult(success=False, output=None, error=error)

        try:
            path = Path(file_path)

            # Resolve and validate path
            try:
                path_absolute = path.resolve(strict=True)
            except FileNotFoundError:
                return ToolResult(success=False, output=None, error=f"File not found: {file_path}")

            allowed_dirs = _get_allowed_dirs(session_context)
            if not any(_is_allowed(path_absolute, base) for base in allowed_dirs):
                return ToolResult(
                    success=False, output=None,
                    error=f"Editing {path_absolute} is not allowed. Use session workspace.",
                )

            if not path_absolute.is_file():
                return ToolResult(success=False, output=None, error=f"Not a file: {file_path}")

            if path_absolute.stat().st_size > self._MAX_FILE_SIZE_BYTES:
                return ToolResult(
                    success=False, output=None,
                    error=f"File too large (max {self._MAX_FILE_SIZE_BYTES // (1024*1024)}MB)",
                )

            # Read current content
            async with aiofiles.open(path_absolute, mode="r", encoding="utf-8") as f:
                content = await f.read()

            # Count occurrences
            count = content.count(old_text)

            if count == 0:
                # Provide context to help the agent fix the match
                snippet = old_text[:100].replace("\n", "\\n")
                return ToolResult(
                    success=False, output=None,
                    error=f"old_text not found in file. No match for: {snippet}...",
                )

            if count > 1 and not replace_all:
                return ToolResult(
                    success=False, output=None,
                    error=(
                        f"old_text matches {count} locations. "
                        "Provide more surrounding context to make the match unique, "
                        "or set replace_all=true to replace all occurrences."
                    ),
                )

            # Perform replacement
            if replace_all:
                new_content = content.replace(old_text, new_text)
            else:
                new_content = content.replace(old_text, new_text, 1)

            # Write back
            async with aiofiles.open(path_absolute, mode="w", encoding="utf-8") as f:
                await f.write(new_content)

            session_id = (session_context or {}).get("session_id", "default")
            logger.info(
                "file_edit success",
                extra={
                    "session_id": session_id,
                    "path": str(path_absolute),
                    "replacements": count if replace_all else 1,
                },
            )

            # Show a snippet around the edit for confirmation
            edit_pos = new_content.find(new_text) if new_text else content.find(old_text)
            context_start = max(0, edit_pos - 50)
            context_end = min(len(new_content), edit_pos + len(new_text) + 50) if new_text else min(len(content), edit_pos + 50)
            snippet = new_content[context_start:context_end] if new_text else "(text deleted)"

            return ToolResult(
                success=True,
                output={
                    "message": f"Replaced {count if replace_all else 1} occurrence(s)",
                    "snippet": snippet,
                },
                metadata={
                    "path": str(path_absolute),
                    "replacements": count if replace_all else 1,
                    "old_length": len(old_text),
                    "new_length": len(new_text),
                    "file_size_after": len(new_content),
                },
            )

        except UnicodeDecodeError:
            return ToolResult(success=False, output=None, error="File is not valid UTF-8")
        except Exception as e:
            return ToolResult(success=False, output=None, error=f"Error editing file: {str(e)}")


class FileDeleteTool(Tool):
    """
    Delete file(s) or empty directories from the session workspace.
    Supports glob patterns for batch deletion. Cannot delete non-empty directories.
    Only operates within /workspace — /memory/ paths are rejected.
    """

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="file_delete",
            version="1.0.0",
            description=(
                "Delete a file, empty directory, or batch of files matching a glob pattern "
                "from the session workspace. Cannot delete non-empty directories or /memory/ paths."
            ),
            category="file",
            author="Shannon",
            requires_auth=True,
            rate_limit=50,
            timeout_seconds=10,
            memory_limit_mb=256,
            sandboxed=True,
            session_aware=True,
            dangerous=True,
            cost_per_use=0.001,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="path",
                type=ToolParameterType.STRING,
                description="Path to delete (file or empty directory). When pattern is set, this is the base directory to search in.",
                required=True,
            ),
            ToolParameter(
                name="pattern",
                type=ToolParameterType.STRING,
                description="Glob pattern for batch deletion (e.g. '*.tmp', '*.log'). When set, deletes all matching files under path.",
                required=False,
                default="",
            ),
            ToolParameter(
                name="recursive",
                type=ToolParameterType.BOOLEAN,
                description="Search subdirectories when using pattern (default: false)",
                required=False,
                default=False,
            ),
        ]

    async def _execute_impl(
        self,
        session_context: Optional[Dict] = None,
        observer: Optional[Any] = None,
        **kwargs,
    ) -> ToolResult:
        target_path = kwargs["path"]
        pattern = kwargs.get("pattern", "")
        recursive = kwargs.get("recursive", False)

        # Proxy to WASI sandbox if enabled
        if is_sandbox_enabled():
            session_id = (session_context or {}).get("session_id", "default")
            client = get_sandbox_client()
            success, deleted_count, deleted_paths, error = await client.file_delete(
                session_id=session_id,
                path=target_path,
                pattern=pattern,
                recursive=recursive,
            )
            if success:
                logger.info(
                    "Sandbox file_delete success",
                    extra={
                        "session_id": session_id,
                        "path": target_path,
                        "deleted_count": deleted_count,
                        "sandbox_enabled": True,
                    },
                )
                return ToolResult(
                    success=True,
                    output={
                        "message": f"Deleted {deleted_count} item(s)",
                        "deleted_paths": deleted_paths,
                    },
                    metadata={
                        "path": target_path,
                        "pattern": pattern,
                        "deleted_count": deleted_count,
                    },
                )
            else:
                logger.warning(
                    "Sandbox file_delete failed",
                    extra={
                        "session_id": session_id,
                        "path": target_path,
                        "sandbox_enabled": True,
                        "error": error,
                    },
                )
                return ToolResult(success=False, output=None, error=error)

        # Local filesystem fallback — workspace only (not memory)
        # Explicit /memory/ guard — matches Rust hard-reject pattern (sandbox_service.rs:1223)
        if target_path.startswith("/memory/") or target_path.startswith("/memory") \
                or target_path.startswith("memory/") or target_path == "memory":
            return ToolResult(
                success=False,
                output=None,
                error="Deleting from /memory/ is not allowed. file_delete only works in workspace.",
            )
        try:
            workspace = _get_session_workspace(session_context)

            if pattern:
                # Glob batch mode
                base_dir = (workspace / target_path).resolve()
                if not _is_allowed(base_dir, workspace):
                    return ToolResult(
                        success=False,
                        output=None,
                        error=f"Deleting from {target_path} is not allowed. Use session workspace.",
                    )
                if not base_dir.is_dir():
                    return ToolResult(
                        success=False,
                        output=None,
                        error=f"Base path is not a directory: {target_path}",
                    )

                if recursive:
                    matches = list(base_dir.rglob(pattern))
                else:
                    matches = list(base_dir.glob(pattern))

                # Filter to workspace-allowed paths only
                matches = [m for m in matches if _is_allowed(m.resolve(), workspace)]

                # Sort by depth descending so files are deleted before parent dirs
                matches.sort(key=lambda p: len(p.parts), reverse=True)

                deleted_paths = []
                for match in matches:
                    try:
                        if match.is_file() or match.is_symlink():
                            match.unlink()
                            deleted_paths.append(str(match.relative_to(workspace)))
                        elif match.is_dir():
                            # Only delete empty directories
                            try:
                                match.rmdir()
                                deleted_paths.append(str(match.relative_to(workspace)))
                            except OSError:
                                pass  # Non-empty directory, skip
                    except OSError:
                        continue

                session_id = (session_context or {}).get("session_id", "default")
                logger.info(
                    "file_delete glob success",
                    extra={
                        "session_id": session_id,
                        "path": target_path,
                        "pattern": pattern,
                        "deleted_count": len(deleted_paths),
                    },
                )
                return ToolResult(
                    success=True,
                    output={
                        "message": f"Deleted {len(deleted_paths)} item(s)",
                        "deleted_paths": deleted_paths,
                    },
                    metadata={
                        "path": target_path,
                        "pattern": pattern,
                        "deleted_count": len(deleted_paths),
                    },
                )
            else:
                # Single target mode
                path = (workspace / target_path).resolve()
                if not _is_allowed(path, workspace):
                    return ToolResult(
                        success=False,
                        output=None,
                        error=f"Deleting {target_path} is not allowed. Use session workspace.",
                    )

                if not path.exists():
                    # Idempotent: not-found is success
                    return ToolResult(
                        success=True,
                        output={"message": "Path does not exist (nothing to delete)", "deleted_paths": []},
                        metadata={"path": target_path, "deleted_count": 0},
                    )

                rel_path = str(path.relative_to(workspace))

                if path.is_file() or path.is_symlink():
                    path.unlink()
                elif path.is_dir():
                    try:
                        path.rmdir()  # Only empty directories
                    except OSError:
                        return ToolResult(
                            success=False,
                            output=None,
                            error=f"Directory is not empty: {target_path}. Delete files inside first.",
                        )
                else:
                    return ToolResult(
                        success=False,
                        output=None,
                        error=f"Unsupported file type: {target_path}",
                    )

                session_id = (session_context or {}).get("session_id", "default")
                logger.info(
                    "file_delete success",
                    extra={
                        "session_id": session_id,
                        "path": target_path,
                        "deleted": rel_path,
                    },
                )
                return ToolResult(
                    success=True,
                    output={
                        "message": f"Deleted: {rel_path}",
                        "deleted_paths": [rel_path],
                    },
                    metadata={"path": target_path, "deleted_count": 1},
                )

        except Exception as e:
            return ToolResult(
                success=False, output=None, error=f"Error deleting file: {str(e)}"
            )
