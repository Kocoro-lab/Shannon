//! gRPC service for WASI-isolated file operations.

use crate::safe_commands::SafeCommand;
use crate::workspace::WorkspaceManager;
use std::path::PathBuf;
use std::time::Instant;
use tonic::{Request, Response, Status};
use std::time::Duration;
use tracing::{info, warn};

// Include generated proto code
pub mod proto {
    tonic::include_proto!("shannon.sandbox");
}

use proto::sandbox_service_server::{SandboxService, SandboxServiceServer};
use proto::*;

/// Configuration for the sandbox service.
pub struct SandboxConfig {
    /// Maximum file size for reads (default 10MB)
    pub max_read_bytes: usize,
    /// Maximum workspace size per session (default 100MB)
    pub max_workspace_bytes: u64,
    /// Command timeout in seconds (default 30)
    pub command_timeout_seconds: u32,
}

impl Default for SandboxConfig {
    fn default() -> Self {
        Self {
            max_read_bytes: 10 * 1024 * 1024,        // 10MB
            max_workspace_bytes: 100 * 1024 * 1024, // 100MB
            command_timeout_seconds: 30,
        }
    }
}

/// Implementation of the SandboxService.
pub struct SandboxServiceImpl {
    workspace_mgr: WorkspaceManager,
    config: SandboxConfig,
}

impl SandboxServiceImpl {
    /// Create a new sandbox service with the given workspace base directory.
    pub fn new(workspaces_dir: PathBuf) -> Self {
        Self {
            workspace_mgr: WorkspaceManager::new(workspaces_dir),
            config: SandboxConfig::default(),
        }
    }

    /// Create from environment.
    pub fn from_env() -> Self {
        Self {
            workspace_mgr: WorkspaceManager::from_env(),
            config: SandboxConfig::default(),
        }
    }

    /// Create with custom config.
    pub fn with_config(workspaces_dir: PathBuf, config: SandboxConfig) -> Self {
        Self {
            workspace_mgr: WorkspaceManager::new(workspaces_dir),
            config,
        }
    }

    /// Create a tonic service.
    pub fn into_service(self) -> SandboxServiceServer<Self> {
        SandboxServiceServer::new(self)
    }

    /// Resolve a path within a session's workspace.
    fn resolve_path(&self, session_id: &str, path: &str) -> Result<PathBuf, Status> {
        let workspace = self
            .workspace_mgr
            .get_workspace(session_id)
            .map_err(|e| Status::invalid_argument(format!("Invalid session: {}", e)))?;

        // Handle empty path as workspace root
        let relative = if path.is_empty() { "." } else { path };

        // SECURITY: Early rejection of path traversal patterns (defense in depth)
        // This catches attempts even when target/parent don't exist yet
        if relative.contains("..") {
            warn!(
                session_id = %session_id,
                path = %path,
                violation = "path_traversal",
                "Security violation: path traversal pattern detected"
            );
            return Err(Status::permission_denied("Path traversal not allowed"));
        }

        // Block absolute paths - all paths must be relative to workspace
        if std::path::Path::new(relative).is_absolute() {
            warn!(
                session_id = %session_id,
                path = %path,
                violation = "absolute_path",
                "Security violation: absolute path not allowed"
            );
            return Err(Status::permission_denied("Absolute paths not allowed"));
        }

        let target = workspace.join(relative);

        // For existing paths, verify they're within workspace
        if target.exists() {
            let canonical = target
                .canonicalize()
                .map_err(|e| Status::not_found(format!("Path error: {}", e)))?;

            if !canonical.starts_with(&workspace) {
                warn!(
                    session_id = %session_id,
                    path = %path,
                    resolved = %canonical.display(),
                    violation = "path_escape",
                    "Security violation: path escapes workspace"
                );
                return Err(Status::permission_denied("Path escapes workspace"));
            }
            return Ok(canonical);
        }

        // For non-existing paths (writes), validate parent
        if let Some(parent) = target.parent() {
            if parent.exists() {
                let canonical_parent = parent
                    .canonicalize()
                    .map_err(|e| Status::internal(format!("Parent path error: {}", e)))?;

                if !canonical_parent.starts_with(&workspace) {
                    warn!(
                        session_id = %session_id,
                        path = %path,
                        resolved_parent = %canonical_parent.display(),
                        violation = "parent_path_escape",
                        "Security violation: parent path escapes workspace"
                    );
                    return Err(Status::permission_denied("Path escapes workspace"));
                }
            }
        }

        Ok(target)
    }

    /// Check workspace quota before write operations.
    fn check_quota(&self, session_id: &str, additional_bytes: u64) -> Result<(), Status> {
        let current_size = self
            .workspace_mgr
            .get_workspace_size(session_id)
            .map_err(|e| Status::internal(format!("Quota check failed: {}", e)))?;

        if current_size + additional_bytes > self.config.max_workspace_bytes {
            return Err(Status::resource_exhausted(format!(
                "Workspace quota exceeded: {} + {} > {} bytes",
                current_size, additional_bytes, self.config.max_workspace_bytes
            )));
        }
        Ok(())
    }
}

#[tonic::async_trait]
impl SandboxService for SandboxServiceImpl {
    async fn file_read(
        &self,
        request: Request<FileReadRequest>,
    ) -> Result<Response<FileReadResponse>, Status> {
        let req = request.into_inner();
        info!(
            session_id = %req.session_id,
            path = %req.path,
            operation = "file_read",
            "Sandbox file read operation"
        );

        let target = self.resolve_path(&req.session_id, &req.path)?;

        if !target.is_file() {
            return Ok(Response::new(FileReadResponse {
                success: false,
                error: "Path is not a file".to_string(),
                ..Default::default()
            }));
        }

        // Check file size
        let metadata = std::fs::metadata(&target)
            .map_err(|e| Status::not_found(format!("File not found: {}", e)))?;

        let max_bytes = if req.max_bytes > 0 {
            req.max_bytes as usize
        } else {
            self.config.max_read_bytes
        };

        if metadata.len() as usize > max_bytes {
            return Ok(Response::new(FileReadResponse {
                success: false,
                error: format!(
                    "File too large: {} bytes (max {})",
                    metadata.len(),
                    max_bytes
                ),
                ..Default::default()
            }));
        }

        // Read file
        let content = std::fs::read_to_string(&target)
            .map_err(|e| Status::internal(format!("Read error: {}", e)))?;

        let file_type = target
            .extension()
            .and_then(|e| e.to_str())
            .unwrap_or("")
            .to_string();

        info!(
            session_id = %req.session_id,
            path = %req.path,
            bytes = content.len(),
            operation = "file_read",
            success = true,
            "File read completed"
        );

        Ok(Response::new(FileReadResponse {
            success: true,
            content,
            error: String::new(),
            size_bytes: metadata.len() as i64,
            file_type,
        }))
    }

    async fn file_write(
        &self,
        request: Request<FileWriteRequest>,
    ) -> Result<Response<FileWriteResponse>, Status> {
        let req = request.into_inner();
        let bytes_to_write = req.content.len();
        info!(
            session_id = %req.session_id,
            path = %req.path,
            bytes = bytes_to_write,
            append = req.append,
            operation = "file_write",
            "Sandbox file write operation"
        );

        // Check quota
        self.check_quota(&req.session_id, req.content.len() as u64)?;

        let workspace = self
            .workspace_mgr
            .get_workspace(&req.session_id)
            .map_err(|e| Status::invalid_argument(format!("Invalid session: {}", e)))?;

        let target = workspace.join(&req.path);

        // Security: Validate ALL path components BEFORE any directory creation
        // to prevent symlink attacks via parent directory manipulation
        fn validate_path_components(workspace: &std::path::Path, target: &std::path::Path, session_id: &str) -> Result<(), Status> {
            // Check that target path string doesn't contain suspicious patterns
            let target_str = target.to_string_lossy();
            if target_str.contains("..") {
                warn!(
                    session_id = %session_id,
                    path = %target_str,
                    violation = "path_traversal",
                    operation = "file_write",
                    "Security violation: path traversal attempt detected"
                );
                return Err(Status::permission_denied("Path traversal not allowed"));
            }

            // Validate each existing component is within workspace
            let mut current = workspace.to_path_buf();
            for component in target.strip_prefix(workspace).unwrap_or(target).components() {
                use std::path::Component;
                match component {
                    Component::Normal(name) => {
                        current = current.join(name);
                        if current.exists() {
                            // Check for symlinks pointing outside workspace
                            let metadata = std::fs::symlink_metadata(&current)
                                .map_err(|e| Status::internal(format!("Path check error: {}", e)))?;
                            if metadata.file_type().is_symlink() {
                                let link_target = std::fs::read_link(&current)
                                    .map_err(|e| Status::internal(format!("Symlink read error: {}", e)))?;
                                // Resolve symlink and verify it's within workspace
                                let resolved = if link_target.is_absolute() {
                                    link_target
                                } else {
                                    current.parent().unwrap_or(workspace).join(&link_target)
                                };
                                // SECURITY FIX: Symlink must be resolvable - fail closed on unresolvable symlinks
                                let canonical = resolved.canonicalize().map_err(|_| {
                                    warn!(
                                        session_id = %session_id,
                                        path = %current.display(),
                                        violation = "unresolvable_symlink",
                                        operation = "file_write",
                                        "Security violation: symlink target cannot be resolved"
                                    );
                                    Status::permission_denied("Symlink target cannot be resolved")
                                })?;
                                if !canonical.starts_with(workspace) {
                                    warn!(
                                        session_id = %session_id,
                                        path = %current.display(),
                                        symlink_target = %canonical.display(),
                                        violation = "symlink_escape",
                                        operation = "file_write",
                                        "Security violation: symlink escapes workspace"
                                    );
                                    return Err(Status::permission_denied("Symlink escapes workspace"));
                                }
                            }
                        }
                    }
                    Component::ParentDir => {
                        warn!(
                            session_id = %session_id,
                            path = %target.display(),
                            violation = "parent_traversal",
                            operation = "file_write",
                            "Security violation: parent directory traversal attempt"
                        );
                        return Err(Status::permission_denied("Parent directory traversal not allowed"));
                    }
                    _ => {}
                }
            }
            Ok(())
        }

        validate_path_components(&workspace, &target, &req.session_id)?;

        // Create parent directories if requested (now safe after validation)
        if req.create_dirs {
            if let Some(parent) = target.parent() {
                std::fs::create_dir_all(parent)
                    .map_err(|e| Status::internal(format!("Failed to create directories: {}", e)))?;
            }
        } else if let Some(parent) = target.parent() {
            if !parent.exists() {
                return Ok(Response::new(FileWriteResponse {
                    success: false,
                    error: "Parent directory does not exist".to_string(),
                    ..Default::default()
                }));
            }
        }

        // Post-creation verification (defense in depth)
        if target.exists() {
            let canonical = target.canonicalize().map_err(|e| {
                Status::internal(format!("Path resolution error: {}", e))
            })?;
            if !canonical.starts_with(&workspace) {
                warn!(
                    session_id = %req.session_id,
                    path = %req.path,
                    violation = "post_creation_escape",
                    operation = "file_write",
                    "Security violation: path escape detected after creation"
                );
                return Err(Status::permission_denied("Path escapes workspace"));
            }
        }

        // Write file
        let bytes_written = if req.append {
            use std::io::Write;
            let mut file = std::fs::OpenOptions::new()
                .create(true)
                .append(true)
                .open(&target)
                .map_err(|e| Status::internal(format!("Open error: {}", e)))?;
            file.write_all(req.content.as_bytes())
                .map_err(|e| Status::internal(format!("Write error: {}", e)))?;
            req.content.len() as i64
        } else {
            std::fs::write(&target, &req.content)
                .map_err(|e| Status::internal(format!("Write error: {}", e)))?;
            req.content.len() as i64
        };

        // Return relative path within workspace
        let resolved = target.canonicalize().unwrap_or(target);
        let relative = resolved
            .strip_prefix(&workspace)
            .unwrap_or(&resolved)
            .to_string_lossy()
            .to_string();

        info!(
            session_id = %req.session_id,
            path = %req.path,
            bytes_written = bytes_written,
            operation = "file_write",
            success = true,
            "File write completed"
        );

        Ok(Response::new(FileWriteResponse {
            success: true,
            bytes_written,
            error: String::new(),
            absolute_path: relative,
        }))
    }

    async fn file_list(
        &self,
        request: Request<FileListRequest>,
    ) -> Result<Response<FileListResponse>, Status> {
        let req = request.into_inner();
        info!(
            session_id = %req.session_id,
            path = %req.path,
            pattern = %req.pattern,
            recursive = req.recursive,
            operation = "file_list",
            "Sandbox file list operation"
        );

        let target = self.resolve_path(&req.session_id, &req.path)?;
        let workspace = self
            .workspace_mgr
            .get_workspace(&req.session_id)
            .map_err(|e| Status::invalid_argument(format!("Invalid session: {}", e)))?;

        if !target.is_dir() {
            return Ok(Response::new(FileListResponse {
                success: false,
                error: "Path is not a directory".to_string(),
                ..Default::default()
            }));
        }

        let mut entries = Vec::new();
        let mut file_count = 0i32;
        let mut dir_count = 0i32;

        fn collect_entries(
            dir: &std::path::Path,
            workspace: &std::path::Path,
            pattern: &str,
            recursive: bool,
            include_hidden: bool,
            entries: &mut Vec<FileEntry>,
            file_count: &mut i32,
            dir_count: &mut i32,
        ) -> Result<(), std::io::Error> {
            for entry in std::fs::read_dir(dir)? {
                let entry = entry?;
                let name = entry.file_name().to_string_lossy().to_string();

                // Skip hidden files unless requested
                if !include_hidden && name.starts_with('.') {
                    continue;
                }

                // Apply pattern filter
                if !pattern.is_empty() && !glob_match(pattern, &name) {
                    continue;
                }

                let metadata = entry.metadata()?;
                let path = entry.path();
                let relative = path
                    .strip_prefix(workspace)
                    .unwrap_or(&path)
                    .to_string_lossy()
                    .to_string();

                let is_file = metadata.is_file();
                if is_file {
                    *file_count += 1;
                } else {
                    *dir_count += 1;
                }

                entries.push(FileEntry {
                    name,
                    path: relative,
                    is_file,
                    size_bytes: if is_file { metadata.len() as i64 } else { 0 },
                    modified_time: metadata
                        .modified()
                        .ok()
                        .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
                        .map(|d| d.as_secs() as i64)
                        .unwrap_or(0),
                });

                if recursive && !is_file {
                    collect_entries(
                        &path,
                        workspace,
                        pattern,
                        recursive,
                        include_hidden,
                        entries,
                        file_count,
                        dir_count,
                    )?;
                }
            }
            Ok(())
        }

        collect_entries(
            &target,
            &workspace,
            &req.pattern,
            req.recursive,
            req.include_hidden,
            &mut entries,
            &mut file_count,
            &mut dir_count,
        )
        .map_err(|e| Status::internal(format!("List error: {}", e)))?;

        // Sort by name
        entries.sort_by(|a, b| a.name.cmp(&b.name));

        info!(
            session_id = %req.session_id,
            path = %req.path,
            file_count = file_count,
            dir_count = dir_count,
            operation = "file_list",
            success = true,
            "File list completed"
        );

        Ok(Response::new(FileListResponse {
            success: true,
            entries,
            error: String::new(),
            file_count,
            dir_count,
        }))
    }

    async fn execute_command(
        &self,
        request: Request<CommandRequest>,
    ) -> Result<Response<CommandResponse>, Status> {
        let req = request.into_inner();
        info!(
            session_id = %req.session_id,
            command = %req.command,
            operation = "execute_command",
            "Sandbox command execution"
        );

        let workspace = self
            .workspace_mgr
            .get_workspace(&req.session_id)
            .map_err(|e| Status::invalid_argument(format!("Invalid session: {}", e)))?;

        // Parse command
        let cmd = match SafeCommand::parse(&req.command) {
            Ok(c) => c,
            Err(e) => {
                warn!(
                    session_id = %req.session_id,
                    command = %req.command,
                    error = %e,
                    operation = "execute_command",
                    "Command not allowed"
                );
                return Ok(Response::new(CommandResponse {
                    success: false,
                    error: format!("Command not allowed: {}", e),
                    exit_code: 1,
                    ..Default::default()
                }));
            }
        };

        let start = Instant::now();

        // Enforce timeout (use request timeout or config default, max 30s)
        let timeout_secs = if req.timeout_seconds > 0 {
            req.timeout_seconds.min(self.config.command_timeout_seconds as i32) as u64
        } else {
            self.config.command_timeout_seconds as u64
        };
        let timeout = Duration::from_secs(timeout_secs);

        // Execute command with timeout using spawn_blocking for sync operations
        let result = tokio::time::timeout(timeout, async {
            tokio::task::spawn_blocking(move || cmd.execute(&workspace))
                .await
                .map_err(|e| anyhow::anyhow!("Task panicked: {}", e))?
        })
        .await;

        let execution_time_ms = start.elapsed().as_millis() as i64;

        match result {
            Ok(Ok(output)) => {
                info!(
                    session_id = %req.session_id,
                    command = %req.command,
                    exit_code = output.exit_code,
                    execution_time_ms = execution_time_ms,
                    operation = "execute_command",
                    success = output.exit_code == 0,
                    "Command execution completed"
                );
                Ok(Response::new(CommandResponse {
                    success: output.exit_code == 0,
                    stdout: output.stdout,
                    stderr: output.stderr,
                    exit_code: output.exit_code,
                    error: String::new(),
                    execution_time_ms,
                }))
            }
            Ok(Err(e)) => {
                warn!(
                    session_id = %req.session_id,
                    command = %req.command,
                    error = %e,
                    execution_time_ms = execution_time_ms,
                    operation = "execute_command",
                    "Command execution error"
                );
                Ok(Response::new(CommandResponse {
                    success: false,
                    stdout: String::new(),
                    stderr: e.to_string(),
                    exit_code: 1,
                    error: format!("Execution error: {}", e),
                    execution_time_ms,
                }))
            }
            Err(_) => {
                warn!(
                    session_id = %req.session_id,
                    command = %req.command,
                    timeout_secs = timeout_secs,
                    operation = "execute_command",
                    "Command timed out"
                );
                Ok(Response::new(CommandResponse {
                    success: false,
                    stdout: String::new(),
                    stderr: format!("Command timed out after {}s", timeout_secs),
                    exit_code: 124, // Standard timeout exit code
                    error: format!("Command timed out after {}s", timeout_secs),
                    execution_time_ms,
                }))
            }
        }
    }
}

/// Simple glob pattern matching (no regex to avoid DoS).
fn glob_match(pattern: &str, name: &str) -> bool {
    if pattern.is_empty() {
        return true;
    }
    glob_match_recursive(pattern.as_bytes(), name.as_bytes())
}

/// Recursive glob matcher without regex (prevents ReDoS attacks).
fn glob_match_recursive(pattern: &[u8], name: &[u8]) -> bool {
    match (pattern.first(), name.first()) {
        (None, None) => true,
        (Some(b'*'), _) => {
            // '*' matches zero or more characters
            glob_match_recursive(&pattern[1..], name)
                || (!name.is_empty() && glob_match_recursive(pattern, &name[1..]))
        }
        (Some(b'?'), Some(_)) => {
            // '?' matches exactly one character
            glob_match_recursive(&pattern[1..], &name[1..])
        }
        (Some(p), Some(n)) if *p == *n => {
            glob_match_recursive(&pattern[1..], &name[1..])
        }
        _ => false,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_file_read_write_roundtrip() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());

        // Write
        let write_req = FileWriteRequest {
            session_id: "test-session".to_string(),
            path: "hello.txt".to_string(),
            content: "Hello, World!".to_string(),
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        let write_resp = service
            .file_write(tonic::Request::new(write_req))
            .await
            .unwrap();
        assert!(write_resp.into_inner().success);

        // Read
        let read_req = FileReadRequest {
            session_id: "test-session".to_string(),
            path: "hello.txt".to_string(),
            max_bytes: 0,
            encoding: "utf-8".to_string(),
        };
        let read_resp = service
            .file_read(tonic::Request::new(read_req))
            .await
            .unwrap();
        let inner = read_resp.into_inner();
        assert!(inner.success);
        assert_eq!(inner.content, "Hello, World!");
    }

    #[tokio::test]
    async fn test_file_list() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());

        // Create files by getting workspace first
        let workspace = temp.path().join("test-session");
        std::fs::create_dir_all(&workspace).unwrap();
        std::fs::write(workspace.join("file1.txt"), "content1").unwrap();
        std::fs::write(workspace.join("file2.txt"), "content2").unwrap();
        std::fs::create_dir(workspace.join("subdir")).unwrap();

        let req = FileListRequest {
            session_id: "test-session".to_string(),
            path: "".to_string(),
            pattern: "".to_string(),
            recursive: false,
            include_hidden: false,
        };
        let resp = service.file_list(tonic::Request::new(req)).await.unwrap();
        let inner = resp.into_inner();

        assert!(inner.success);
        assert_eq!(inner.file_count, 2);
        assert_eq!(inner.dir_count, 1);
    }

    #[tokio::test]
    async fn test_execute_command() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());

        // Create workspace with file
        let workspace = temp.path().join("test-session");
        std::fs::create_dir_all(&workspace).unwrap();
        std::fs::write(workspace.join("test.txt"), "hello world").unwrap();

        let req = CommandRequest {
            session_id: "test-session".to_string(),
            command: "cat test.txt".to_string(),
            timeout_seconds: 5,
        };
        let resp = service
            .execute_command(tonic::Request::new(req))
            .await
            .unwrap();
        let inner = resp.into_inner();

        assert!(inner.success);
        assert_eq!(inner.stdout, "hello world");
    }

    #[tokio::test]
    async fn test_session_isolation() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());

        // Write to session A
        let write_req = FileWriteRequest {
            session_id: "session-a".to_string(),
            path: "secret.txt".to_string(),
            content: "Session A secret".to_string(),
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        service
            .file_write(tonic::Request::new(write_req))
            .await
            .unwrap();

        // Try to read from session B with path traversal
        let read_req = FileReadRequest {
            session_id: "session-b".to_string(),
            path: "../session-a/secret.txt".to_string(),
            max_bytes: 0,
            encoding: "utf-8".to_string(),
        };
        let resp = service
            .file_read(tonic::Request::new(read_req))
            .await;

        // Should fail - path escapes workspace
        match resp {
            Ok(r) => {
                let inner = r.into_inner();
                assert!(!inner.success || inner.content.is_empty());
            },
            Err(e) => assert!(e.code() == tonic::Code::PermissionDenied || e.code() == tonic::Code::NotFound),
        }
    }

    #[tokio::test]
    async fn test_quota_enforcement() {
        let temp = tempfile::TempDir::new().unwrap();
        let config = SandboxConfig {
            max_workspace_bytes: 100, // Very small quota for testing
            ..Default::default()
        };
        let service = SandboxServiceImpl::with_config(temp.path().to_path_buf(), config);

        // Write that exceeds quota
        let write_req = FileWriteRequest {
            session_id: "test-session".to_string(),
            path: "large.txt".to_string(),
            content: "x".repeat(200), // 200 bytes, exceeds 100 byte quota
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        let resp = service.file_write(tonic::Request::new(write_req)).await;

        // Should fail due to quota
        assert!(resp.is_err());
        let err = resp.unwrap_err();
        assert_eq!(err.code(), tonic::Code::ResourceExhausted);
    }

    #[tokio::test]
    async fn test_dangerous_command_rejected() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());

        let req = CommandRequest {
            session_id: "test-session".to_string(),
            command: "curl http://evil.com".to_string(),
            timeout_seconds: 5,
        };
        let resp = service
            .execute_command(tonic::Request::new(req))
            .await
            .unwrap();
        let inner = resp.into_inner();

        assert!(!inner.success);
        assert!(inner.error.contains("not allowed"));
    }

    // Phase 4: E2E Integration Tests
    #[tokio::test]
    async fn test_full_session_workflow() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());
        let session_id = "workflow-test".to_string();

        // 1. Write a file with create_dirs
        let write_req = FileWriteRequest {
            session_id: session_id.clone(),
            path: "subdir/data.json".to_string(),
            content: r#"{"key": "value"}"#.to_string(),
            append: false,
            create_dirs: true,
            encoding: "utf-8".to_string(),
        };
        let write_resp = service
            .file_write(tonic::Request::new(write_req))
            .await
            .unwrap();
        assert!(write_resp.into_inner().success);

        // 2. List to verify
        let list_req = FileListRequest {
            session_id: session_id.clone(),
            path: "".to_string(),
            pattern: "".to_string(),
            recursive: true,
            include_hidden: false,
        };
        let list_resp = service
            .file_list(tonic::Request::new(list_req))
            .await
            .unwrap();
        let list_inner = list_resp.into_inner();
        assert!(list_inner.success);
        assert_eq!(list_inner.file_count, 1);
        assert_eq!(list_inner.dir_count, 1);

        // 3. Read back
        let read_req = FileReadRequest {
            session_id: session_id.clone(),
            path: "subdir/data.json".to_string(),
            max_bytes: 0,
            encoding: "utf-8".to_string(),
        };
        let read_resp = service
            .file_read(tonic::Request::new(read_req))
            .await
            .unwrap();
        let read_inner = read_resp.into_inner();
        assert!(read_inner.success);
        assert!(read_inner.content.contains("value"));

        // 4. Execute command to process the file
        let cmd_req = CommandRequest {
            session_id: session_id.clone(),
            command: "cat subdir/data.json".to_string(),
            timeout_seconds: 5,
        };
        let cmd_resp = service
            .execute_command(tonic::Request::new(cmd_req))
            .await
            .unwrap();
        let cmd_inner = cmd_resp.into_inner();
        assert!(cmd_inner.success);
        assert!(cmd_inner.stdout.contains("value"));
    }

    #[tokio::test]
    async fn test_cross_session_isolation() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());

        // Create file in session-alpha
        let write_req = FileWriteRequest {
            session_id: "session-alpha".to_string(),
            path: "confidential.txt".to_string(),
            content: "TOP SECRET DATA".to_string(),
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        let write_resp = service
            .file_write(tonic::Request::new(write_req))
            .await
            .unwrap();
        assert!(write_resp.into_inner().success);

        // Attempt various escape patterns from session-beta
        let escape_attempts = vec![
            "../session-alpha/confidential.txt",
            "../../session-alpha/confidential.txt",
            "./../session-alpha/confidential.txt",
            "foo/../../session-alpha/confidential.txt",
        ];

        for attempt in escape_attempts {
            let read_req = FileReadRequest {
                session_id: "session-beta".to_string(),
                path: attempt.to_string(),
                max_bytes: 0,
                encoding: "utf-8".to_string(),
            };
            let resp = service.file_read(tonic::Request::new(read_req)).await;

            // All should fail
            match resp {
                Ok(r) => {
                    let inner = r.into_inner();
                    assert!(
                        !inner.success || inner.content.is_empty(),
                        "Escape attempt succeeded unexpectedly: {}",
                        attempt
                    );
                }
                Err(e) => {
                    assert!(
                        e.code() == tonic::Code::PermissionDenied
                            || e.code() == tonic::Code::NotFound,
                        "Unexpected error code for {}: {:?}",
                        attempt,
                        e.code()
                    );
                }
            }
        }
    }
}
