"""
Session-aware file operations tool that tracks files created/modified in a session
"""

import os
import aiofiles
from typing import Any, Dict, List, Optional
from pathlib import Path

from llm_service.tools.base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult


class SessionFileWriteTool(Tool):
    """
    Session-aware file write tool that tracks files created during a session.
    Useful for maintaining context about what files were created/modified in a conversation.
    """
    
    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="session_file_write",
            version="1.0.0",
            description="Write content to a file with session tracking",
            category="file",
            session_aware=True,  # This tool is session-aware
            sandboxed=True,
            rate_limit=100,
        )
    
    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="path",
                type=ToolParameterType.STRING,
                description="File path to write to",
                required=True
            ),
            ToolParameter(
                name="content",
                type=ToolParameterType.STRING,
                description="Content to write to the file",
                required=True
            ),
            ToolParameter(
                name="mode",
                type=ToolParameterType.STRING,
                description="Write mode: 'w' for overwrite, 'a' for append",
                required=False,
                default="w",
                enum=["w", "a"]
            ),
        ]
    
    async def _execute_impl(self, session_context: Optional[Dict] = None, **kwargs) -> ToolResult:
        """
        Write to a file and track it in session context.
        """
        path = kwargs['path']
        content = kwargs['content']
        mode = kwargs.get('mode', 'w')
        
        try:
            # Create parent directories if they don't exist
            file_path = Path(path)
            file_path.parent.mkdir(parents=True, exist_ok=True)
            
            # Write the file
            async with aiofiles.open(path, mode) as f:
                await f.write(content)
            
            # Track in session context if available
            session_info = {}
            if session_context:
                session_id = session_context.get('session_id', 'unknown')
                files_created = session_context.get('files_created', [])
                
                # Add this file to the session's created files list
                if path not in files_created:
                    files_created.append(path)
                
                session_info = {
                    "session_id": session_id,
                    "files_in_session": len(files_created),
                    "session_user": session_context.get('user_id', 'unknown')
                }
            
            return ToolResult(
                success=True,
                output=f"Successfully wrote {len(content)} bytes to {path}",
                metadata={
                    "path": path,
                    "size": len(content),
                    "mode": mode,
                    "session_tracked": bool(session_context),
                    **session_info
                }
            )
            
        except Exception as e:
            return ToolResult(
                success=False,
                output=None,
                error=f"Failed to write file: {str(e)}"
            )


class SessionFileListTool(Tool):
    """
    List files created or modified during the current session.
    """
    
    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="session_file_list",
            version="1.0.0",
            description="List all files created/modified in the current session",
            category="file",
            session_aware=True,
            sandboxed=True,
            rate_limit=100,
        )
    
    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="filter_pattern",
                type=ToolParameterType.STRING,
                description="Optional glob pattern to filter files",
                required=False,
                default="*"
            ),
        ]
    
    async def _execute_impl(self, session_context: Optional[Dict] = None, **kwargs) -> ToolResult:
        """
        List files from session context.
        """
        filter_pattern = kwargs.get('filter_pattern', '*')
        
        if not session_context:
            return ToolResult(
                success=True,
                output="No session context available",
                metadata={"files": [], "session_tracked": False}
            )
        
        try:
            files_created = session_context.get('files_created', [])
            
            # Apply filter if provided
            if filter_pattern and filter_pattern != '*':
                from fnmatch import fnmatch
                files_created = [f for f in files_created if fnmatch(f, filter_pattern)]
            
            # Get file info for each file
            file_info = []
            for file_path in files_created:
                if os.path.exists(file_path):
                    stat = os.stat(file_path)
                    file_info.append({
                        "path": file_path,
                        "size": stat.st_size,
                        "exists": True
                    })
                else:
                    file_info.append({
                        "path": file_path,
                        "exists": False
                    })
            
            return ToolResult(
                success=True,
                output=f"Found {len(file_info)} files in session",
                metadata={
                    "files": file_info,
                    "session_id": session_context.get('session_id', 'unknown'),
                    "total_files": len(files_created),
                    "matched_files": len(file_info),
                    "session_tracked": True
                }
            )
            
        except Exception as e:
            return ToolResult(
                success=False,
                output=None,
                error=f"Failed to list session files: {str(e)}"
            )