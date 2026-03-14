"""
gRPC client for agent-core SandboxService.

When SHANNON_USE_WASI_SANDBOX=1, file tools proxy to this service
instead of executing locally.
"""

import logging
import os
from typing import List, Optional, Tuple

logger = logging.getLogger(__name__)

import grpc

# Proto imports - generated from sandbox.proto
try:
    from llm_service.grpc_gen.sandbox import sandbox_pb2, sandbox_pb2_grpc
    _PROTO_AVAILABLE = True
except ImportError:
    _PROTO_AVAILABLE = False


class SandboxClient:
    """Client for agent-core SandboxService."""

    # Default timeout for gRPC calls (seconds)
    DEFAULT_TIMEOUT = 30
    # Maximum allowed command timeout (prevents DoS via unbounded waits)
    MAX_COMMAND_TIMEOUT = 60

    def __init__(self, address: Optional[str] = None, timeout: Optional[int] = None):
        self.address = address or os.getenv(
            "AGENT_CORE_ADDR", "agent-core:50051"
        )
        self.timeout = timeout or self.DEFAULT_TIMEOUT
        self._channel: Optional[grpc.aio.Channel] = None
        self._stub = None

    async def _ensure_connected(self):
        """Lazy connection to sandbox service."""
        if self._channel is None:
            self._channel = grpc.aio.insecure_channel(self.address)
            if _PROTO_AVAILABLE:
                self._stub = sandbox_pb2_grpc.SandboxServiceStub(self._channel)

    async def file_read(
        self,
        session_id: str,
        path: str,
        user_id: str = "",
        max_bytes: int = 0,
        encoding: str = "utf-8",
    ) -> Tuple[bool, str, str, dict]:
        """
        Read a file from session workspace.

        Returns:
            (success, content, error, metadata)
        """
        logger.debug("Sandbox gRPC file_read request", extra={"session_id": session_id, "path": path, "max_bytes": max_bytes})
        await self._ensure_connected()

        if not _PROTO_AVAILABLE or self._stub is None:
            return (False, "", "Sandbox proto not available", {})

        try:
            request = sandbox_pb2.FileReadRequest(
                session_id=session_id,
                user_id=user_id,
                path=path,
                max_bytes=max_bytes,
                encoding=encoding,
            )
            response = await self._stub.FileRead(request, timeout=self.timeout)
            logger.info("Sandbox gRPC file_read completed", extra={"session_id": session_id, "operation": "file_read", "success": response.success})
            return (
                response.success,
                response.content,
                response.error,
                {
                    "size_bytes": response.size_bytes,
                    "file_type": response.file_type,
                },
            )
        except grpc.RpcError as e:
            logger.info("Sandbox gRPC file_read completed", extra={"session_id": session_id, "operation": "file_read", "success": False})
            return (False, "", f"gRPC error: {e.details()}", {})

    async def file_write(
        self,
        session_id: str,
        path: str,
        content: str,
        user_id: str = "",
        append: bool = False,
        create_dirs: bool = False,
        encoding: str = "utf-8",
    ) -> Tuple[bool, int, str, dict]:
        """
        Write a file to session workspace.

        Returns:
            (success, bytes_written, error, metadata)
        """
        logger.debug("Sandbox gRPC file_write request", extra={"session_id": session_id, "path": path, "append": append, "content_len": len(content)})
        await self._ensure_connected()

        if not _PROTO_AVAILABLE or self._stub is None:
            return (False, 0, "Sandbox proto not available", {})

        try:
            request = sandbox_pb2.FileWriteRequest(
                session_id=session_id,
                user_id=user_id,
                path=path,
                content=content,
                append=append,
                create_dirs=create_dirs,
                encoding=encoding,
            )
            response = await self._stub.FileWrite(request, timeout=self.timeout)
            logger.info("Sandbox gRPC file_write completed", extra={"session_id": session_id, "operation": "file_write", "success": response.success})
            return (
                response.success,
                response.bytes_written,
                response.error,
                {
                    "absolute_path": response.absolute_path,
                },
            )
        except grpc.RpcError as e:
            logger.info("Sandbox gRPC file_write completed", extra={"session_id": session_id, "operation": "file_write", "success": False})
            return (False, 0, f"gRPC error: {e.details()}", {})

    async def file_list(
        self,
        session_id: str,
        user_id: str = "",
        path: str = "",
        pattern: str = "",
        recursive: bool = False,
        include_hidden: bool = False,
    ) -> Tuple[bool, List[dict], str, dict]:
        """
        List files in session workspace.

        Returns:
            (success, entries, error, metadata)
        """
        logger.debug("Sandbox gRPC file_list request", extra={"session_id": session_id, "path": path, "pattern": pattern, "recursive": recursive})
        await self._ensure_connected()

        if not _PROTO_AVAILABLE or self._stub is None:
            return (False, [], "Sandbox proto not available", {})

        try:
            request = sandbox_pb2.FileListRequest(
                session_id=session_id,
                user_id=user_id,
                path=path,
                pattern=pattern,
                recursive=recursive,
                include_hidden=include_hidden,
            )
            response = await self._stub.FileList(request, timeout=self.timeout)
            entries = [
                {
                    "name": e.name,
                    "path": e.path,
                    "is_file": e.is_file,
                    "size_bytes": e.size_bytes,
                    "modified_time": e.modified_time,
                }
                for e in response.entries
            ]
            logger.info("Sandbox gRPC file_list completed", extra={"session_id": session_id, "operation": "file_list", "success": response.success})
            return (
                response.success,
                entries,
                response.error,
                {
                    "file_count": response.file_count,
                    "dir_count": response.dir_count,
                },
            )
        except grpc.RpcError as e:
            logger.info("Sandbox gRPC file_list completed", extra={"session_id": session_id, "operation": "file_list", "success": False})
            return (False, [], f"gRPC error: {e.details()}", {})

    async def file_search(
        self,
        session_id: str,
        query: str,
        path: str = ".",
        max_results: int = 20,
        regex: bool = False,
        include: str = "",
        context_lines: int = 0,
    ) -> Tuple[bool, List[dict], str, dict]:
        """
        Search file contents in session workspace.

        Returns:
            (success, matches, error, metadata)
        """
        logger.debug("Sandbox gRPC file_search request", extra={"session_id": session_id, "query": query, "path": path})
        await self._ensure_connected()

        if not _PROTO_AVAILABLE or self._stub is None:
            return (False, [], "Sandbox proto not available", {})

        try:
            request = sandbox_pb2.FileSearchRequest(
                session_id=session_id,
                query=query,
                path=path,
                max_results=max_results,
                regex=regex,
                include=include,
                context_lines=context_lines,
            )
            response = await self._stub.FileSearch(request, timeout=self.timeout)
            matches = []
            for m in response.matches:
                entry = {
                    "file": m.file,
                    "line": m.line,
                    "content": m.content,
                }
                if m.context:
                    entry["context"] = [
                        {"line": c.line, "content": c.content}
                        for c in m.context
                    ]
                matches.append(entry)
            logger.info("Sandbox gRPC file_search completed", extra={"session_id": session_id, "operation": "file_search", "success": response.success})
            return (
                response.success,
                matches,
                response.error,
                {
                    "files_scanned": response.files_scanned,
                    "matches_found": response.matches_found,
                    "truncated": response.truncated,
                },
            )
        except grpc.RpcError as e:
            logger.info("Sandbox gRPC file_search completed", extra={"session_id": session_id, "operation": "file_search", "success": False})
            return (False, [], f"gRPC error: {e.details()}", {})

    async def file_edit(
        self,
        session_id: str,
        path: str,
        old_text: str,
        new_text: str,
        replace_all: bool = False,
    ) -> Tuple[bool, str, str, dict]:
        """
        Edit a file in session workspace by replacing text.

        Returns:
            (success, snippet, error, metadata)
        """
        logger.debug("Sandbox gRPC file_edit request", extra={"session_id": session_id, "path": path})
        await self._ensure_connected()

        if not _PROTO_AVAILABLE or self._stub is None:
            return (False, "", "Sandbox proto not available", {})

        try:
            request = sandbox_pb2.FileEditRequest(
                session_id=session_id,
                path=path,
                old_text=old_text,
                new_text=new_text,
                replace_all=replace_all,
            )
            response = await self._stub.FileEdit(request, timeout=self.timeout)
            logger.info("Sandbox gRPC file_edit completed", extra={"session_id": session_id, "operation": "file_edit", "success": response.success})
            return (
                response.success,
                response.snippet,
                response.error,
                {
                    "replacements": response.replacements,
                    "file_size_after": response.file_size_after,
                },
            )
        except grpc.RpcError as e:
            logger.info("Sandbox gRPC file_edit completed", extra={"session_id": session_id, "operation": "file_edit", "success": False})
            return (False, "", f"gRPC error: {e.details()}", {})

    async def file_delete(
        self,
        session_id: str,
        path: str,
        pattern: str = "",
        recursive: bool = False,
    ) -> Tuple[bool, int, List[str], str]:
        """
        Delete file(s) or empty directory from session workspace.

        Returns:
            (success, deleted_count, deleted_paths, error)
        """
        logger.debug("Sandbox gRPC file_delete request", extra={"session_id": session_id, "path": path, "pattern": pattern})
        await self._ensure_connected()

        if not _PROTO_AVAILABLE or self._stub is None:
            return (False, 0, [], "Sandbox proto not available")

        try:
            request = sandbox_pb2.FileDeleteRequest(
                session_id=session_id,
                path=path,
                pattern=pattern,
                recursive=recursive,
            )
            response = await self._stub.FileDelete(request, timeout=self.timeout)
            logger.info("Sandbox gRPC file_delete completed", extra={"session_id": session_id, "operation": "file_delete", "success": response.success})
            return (
                response.success,
                response.deleted_count,
                list(response.deleted_paths),
                response.error,
            )
        except grpc.RpcError as e:
            logger.info("Sandbox gRPC file_delete completed", extra={"session_id": session_id, "operation": "file_delete", "success": False})
            return (False, 0, [], f"gRPC error: {e.details()}")

    async def execute_command(
        self,
        session_id: str,
        command: str,
        user_id: str = "",
        timeout_seconds: int = 30,
    ) -> Tuple[bool, str, str, int, str, dict]:
        """
        Execute a safe command in session workspace.

        Returns:
            (success, stdout, stderr, exit_code, error, metadata)
        """
        logger.debug("Sandbox gRPC execute_command request", extra={"session_id": session_id, "command": command, "timeout_seconds": timeout_seconds})
        await self._ensure_connected()

        if not _PROTO_AVAILABLE or self._stub is None:
            return (False, "", "", 1, "Sandbox proto not available", {})

        try:
            request = sandbox_pb2.CommandRequest(
                session_id=session_id,
                user_id=user_id,
                command=command,
                timeout_seconds=timeout_seconds,
            )
            # Handle timeout: 0 means "use server default" (30s in Rust)
            # Clamp positive values to prevent DoS via unbounded waits
            if timeout_seconds <= 0:
                effective_timeout = self.DEFAULT_TIMEOUT  # Match Rust's default
            else:
                effective_timeout = min(timeout_seconds, self.MAX_COMMAND_TIMEOUT)
            cmd_timeout = effective_timeout + 5  # Allow 5s buffer for gRPC overhead
            response = await self._stub.ExecuteCommand(request, timeout=cmd_timeout)
            logger.info("Sandbox gRPC execute_command completed", extra={"session_id": session_id, "operation": "execute_command", "success": response.success})
            return (
                response.success,
                response.stdout,
                response.stderr,
                response.exit_code,
                response.error,
                {
                    "execution_time_ms": response.execution_time_ms,
                },
            )
        except grpc.RpcError as e:
            logger.info("Sandbox gRPC execute_command completed", extra={"session_id": session_id, "operation": "execute_command", "success": False})
            return (False, "", "", 1, f"gRPC error: {e.details()}", {})

    async def close(self):
        """Close the gRPC channel."""
        if self._channel:
            await self._channel.close()
            self._channel = None
            self._stub = None


# Singleton client instance
_client: Optional[SandboxClient] = None


def get_sandbox_client() -> SandboxClient:
    """Get or create the sandbox client singleton."""
    global _client
    if _client is None:
        _client = SandboxClient()
    return _client


def is_sandbox_enabled() -> bool:
    """Check if WASI sandbox is enabled."""
    return os.getenv("SHANNON_USE_WASI_SANDBOX", "0") in ("1", "true", "yes")
