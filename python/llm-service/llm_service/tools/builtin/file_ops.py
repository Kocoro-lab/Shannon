"""
File Operation Tools - Safe file read/write operations
"""

import os
import json
import yaml
import aiofiles
from pathlib import Path
from typing import List, Optional

from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult


class FileReadTool(Tool):
    """
    Safe file reading tool with sandboxing support
    """
    
    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="file_read",
            version="1.0.0",
            description="Read contents of a file",
            category="file",
            author="Shannon",
            requires_auth=False,
            rate_limit=100,
            timeout_seconds=10,
            memory_limit_mb=256,
            sandboxed=True,
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
    
    async def _execute_impl(self, **kwargs) -> ToolResult:
        """
        Read file contents safely
        """
        file_path = kwargs["path"]
        encoding = kwargs.get("encoding", "utf-8")
        max_size_mb = kwargs.get("max_size_mb", 10)
        
        try:
            # Validate path
            path = Path(file_path)

            # Resolve canonical path to avoid symlink escapes
            try:
                path_absolute = path.resolve(strict=True)
            except FileNotFoundError:
                return ToolResult(success=False, output=None, error=f"File not found: {file_path}")

            # Allowlist of readable directories
            allowed_dirs = [Path("/tmp").resolve(), Path.cwd().resolve()]
            workspace = os.getenv("SHANNON_WORKSPACE")
            if workspace:
                allowed_dirs.append(Path(workspace).resolve())

            # Ensure path is within an allowed directory
            def _is_allowed(target: Path, base: Path) -> bool:
                try:
                    target.relative_to(base)
                    return True
                except Exception:
                    return False

            if not any(_is_allowed(path_absolute, base) for base in allowed_dirs):
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Reading {path_absolute} is not allowed. Use workspace or /tmp directory."
                )

            # Check if it's a file (not directory)
            if not path_absolute.is_file():
                return ToolResult(success=False, output=None, error=f"Path is not a file: {file_path}")

            # Check file size
            file_size_mb = path_absolute.stat().st_size / (1024 * 1024)
            if file_size_mb > max_size_mb:
                return ToolResult(success=False, output=None, error=f"File too large: {file_size_mb:.2f}MB (max: {max_size_mb}MB)")

            # Read file
            async with aiofiles.open(path, mode='r', encoding=encoding) as f:
                content = await f.read()
            
            # Detect and parse structured formats
            file_extension = path.suffix.lower()
            parsed_content = content
            
            if file_extension == '.json':
                try:
                    parsed_content = json.loads(content)
                except json.JSONDecodeError:
                    pass  # Return as plain text if not valid JSON
            elif file_extension in ['.yaml', '.yml']:
                try:
                    parsed_content = yaml.safe_load(content)
                except yaml.YAMLError:
                    pass  # Return as plain text if not valid YAML
            
            return ToolResult(
                success=True,
                output=parsed_content,
                metadata={
                    "path": str(path_absolute),
                    "size_bytes": path_absolute.stat().st_size,
                    "encoding": encoding,
                    "file_type": file_extension,
                }
            )
            
        except UnicodeDecodeError:
            return ToolResult(
                success=False,
                output=None,
                error=f"Unable to decode file with encoding: {encoding}"
            )
        except Exception as e:
            return ToolResult(
                success=False,
                output=None,
                error=f"Error reading file: {str(e)}"
            )


class FileWriteTool(Tool):
    """
    Safe file writing tool with sandboxing support
    """
    
    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="file_write",
            version="1.0.0",
            description="Write content to a file",
            category="file",
            author="Shannon",
            requires_auth=True,  # Writing requires auth
            rate_limit=50,
            timeout_seconds=10,
            memory_limit_mb=256,
            sandboxed=True,
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
                default=False,
            ),
        ]
    
    async def _execute_impl(self, **kwargs) -> ToolResult:
        """
        Write content to file safely
        """
        file_path = kwargs["path"]
        content = kwargs["content"]
        mode = kwargs.get("mode", "overwrite")
        encoding = kwargs.get("encoding", "utf-8")
        create_dirs = kwargs.get("create_dirs", False)
        
        try:
            path = Path(file_path)

            # Canonicalize to avoid symlink escapes
            path_absolute = path.resolve()

            # Allow writing only within these directories
            allowed_dirs = [Path("/tmp").resolve(), Path.cwd().resolve()]
            workspace = os.getenv("SHANNON_WORKSPACE", None)
            if workspace:
                allowed_dirs.append(Path(workspace).resolve())

            def _is_allowed(target: Path, base: Path) -> bool:
                try:
                    target.relative_to(base)
                    return True
                except Exception:
                    return False

            if not any(_is_allowed(path_absolute, base) for base in allowed_dirs):
                return ToolResult(success=False, output=None, error=f"Writing to {path_absolute} is not allowed. Use workspace or /tmp directory.")
            
            # Create parent directories if requested
            if create_dirs:
                path.parent.mkdir(parents=True, exist_ok=True)
            elif not path.parent.exists():
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Parent directory does not exist: {path.parent}"
                )
            
            # Determine write mode
            write_mode = 'w' if mode == "overwrite" else 'a'
            
            # Write file
            async with aiofiles.open(path, mode=write_mode, encoding=encoding) as f:
                await f.write(content)
            
            # Get file stats after writing
            stats = path.stat()
            
            return ToolResult(
                success=True,
                output=str(path),
                metadata={
                    "path": str(path),
                    "size_bytes": stats.st_size,
                    "mode": mode,
                    "encoding": encoding,
                    "created_dirs": create_dirs,
                }
            )
            
        except Exception as e:
            return ToolResult(
                success=False,
                output=None,
                error=f"Error writing file: {str(e)}"
            )


class FileListTool(Tool):
    """
    List files in a directory
    """
    
    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="file_list",
            version="1.0.0",
            description="List files in a directory",
            category="file",
            author="Shannon",
            requires_auth=False,
            rate_limit=100,
            timeout_seconds=5,
            memory_limit_mb=128,
            sandboxed=True,
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
    
    async def _execute_impl(self, **kwargs) -> ToolResult:
        """
        List files in directory
        """
        dir_path = kwargs["path"]
        pattern = kwargs.get("pattern", "*")
        recursive = kwargs.get("recursive", False)
        include_hidden = kwargs.get("include_hidden", False)
        
        try:
            path = Path(dir_path)
            
            if not path.exists():
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Directory not found: {dir_path}"
                )
            
            if not path.is_dir():
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Path is not a directory: {dir_path}"
                )
            
            # List files
            files = []
            
            if recursive:
                glob_pattern = f"**/{pattern}"
                file_iter = path.rglob(pattern)
            else:
                file_iter = path.glob(pattern)
            
            for file_path in file_iter:
                # Skip hidden files if not requested
                if not include_hidden and file_path.name.startswith('.'):
                    continue
                
                if file_path.is_file():
                    files.append({
                        "name": file_path.name,
                        "path": str(file_path),
                        "size_bytes": file_path.stat().st_size,
                        "is_file": True,
                    })
                elif file_path.is_dir():
                    files.append({
                        "name": file_path.name,
                        "path": str(file_path),
                        "is_file": False,
                    })
            
            return ToolResult(
                success=True,
                output=files,
                metadata={
                    "directory": str(path),
                    "pattern": pattern,
                    "recursive": recursive,
                    "file_count": sum(1 for f in files if f["is_file"]),
                    "dir_count": sum(1 for f in files if not f["is_file"]),
                }
            )
            
        except Exception as e:
            return ToolResult(
                success=False,
                output=None,
                error=f"Error listing directory: {str(e)}"
            )
