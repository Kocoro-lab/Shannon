//! gRPC service for WASI-isolated file operations.

use crate::memory_manager::MemoryManager;
use crate::safe_commands::SafeCommand;
use crate::workspace::WorkspaceManager;
use regex::Regex;
use std::path::PathBuf;
use std::time::Duration;
use std::time::Instant;
use tonic::{Request, Response, Status};
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
    /// Maximum memory store size per user (default 10MB)
    pub max_memory_bytes: u64,
    /// Command timeout in seconds (default 30)
    pub command_timeout_seconds: u32,
}

impl Default for SandboxConfig {
    fn default() -> Self {
        Self {
            max_read_bytes: 10 * 1024 * 1024,       // 10MB
            max_workspace_bytes: 100 * 1024 * 1024, // 100MB
            max_memory_bytes: 10 * 1024 * 1024,     // 10MB
            command_timeout_seconds: 30,
        }
    }
}

/// Implementation of the SandboxService.
pub struct SandboxServiceImpl {
    workspace_mgr: WorkspaceManager,
    memory_mgr: MemoryManager,
    config: SandboxConfig,
}

impl SandboxServiceImpl {
    /// Create a new sandbox service with the given workspace base directory.
    pub fn new(workspaces_dir: PathBuf) -> Self {
        Self {
            workspace_mgr: WorkspaceManager::new(workspaces_dir),
            memory_mgr: MemoryManager::from_env(),
            config: SandboxConfig::default(),
        }
    }

    /// Create from environment.
    pub fn from_env() -> Self {
        Self {
            workspace_mgr: WorkspaceManager::from_env(),
            memory_mgr: MemoryManager::from_env(),
            config: SandboxConfig::default(),
        }
    }

    /// Create with custom config.
    pub fn with_config(workspaces_dir: PathBuf, config: SandboxConfig) -> Self {
        Self {
            workspace_mgr: WorkspaceManager::new(workspaces_dir),
            memory_mgr: MemoryManager::from_env(),
            config,
        }
    }

    /// Create a tonic service.
    pub fn into_service(self) -> SandboxServiceServer<Self> {
        SandboxServiceServer::new(self)
    }

    /// Resolve a path within a session's workspace or user's memory directory.
    ///
    /// Paths prefixed with `/memory/` resolve to the user's persistent memory directory.
    /// All other paths resolve to the session workspace (with `/workspace/` prefix stripped).
    fn resolve_path(&self, session_id: &str, user_id: &str, path: &str) -> Result<PathBuf, Status> {
        // Check if path targets /memory/ (user persistent memory)
        if path.starts_with("/memory/") || path == "/memory" {
            let memory_subpath = path
                .strip_prefix("/memory/")
                .or_else(|| path.strip_prefix("/memory"))
                .unwrap_or("");

            if user_id.is_empty() {
                return Err(Status::invalid_argument(
                    "Cannot resolve /memory/ path without user_id",
                ));
            }

            let memory_dir = self
                .memory_mgr
                .get_memory_dir(user_id)
                .map_err(|e| Status::invalid_argument(format!("Invalid user for memory: {}", e)))?;

            if memory_subpath.is_empty() {
                return Ok(memory_dir);
            }

            // Security: reject path traversal in subpath
            if memory_subpath.contains("..") {
                warn!(
                    user_id = %user_id,
                    path = %path,
                    violation = "memory_path_traversal",
                    "Security violation: path traversal in memory path"
                );
                return Err(Status::permission_denied(
                    "Path traversal not allowed in /memory",
                ));
            }

            let target = memory_dir.join(memory_subpath);

            // For existing paths, verify they're within memory dir
            if target.exists() {
                let canonical = target
                    .canonicalize()
                    .map_err(|e| Status::not_found(format!("Path error: {}", e)))?;

                if !canonical.starts_with(&memory_dir) {
                    warn!(
                        user_id = %user_id,
                        path = %path,
                        resolved = %canonical.display(),
                        violation = "memory_path_escape",
                        "Security violation: path escapes memory directory"
                    );
                    return Err(Status::permission_denied("Path escapes memory directory"));
                }
                return Ok(canonical);
            }

            // For non-existing paths, validate parent
            if let Some(parent) = target.parent() {
                if parent.exists() {
                    let canonical_parent = parent
                        .canonicalize()
                        .map_err(|e| Status::internal(format!("Parent path error: {}", e)))?;

                    if !canonical_parent.starts_with(&memory_dir) {
                        warn!(
                            user_id = %user_id,
                            path = %path,
                            resolved_parent = %canonical_parent.display(),
                            violation = "memory_parent_escape",
                            "Security violation: parent path escapes memory directory"
                        );
                        return Err(Status::permission_denied("Path escapes memory directory"));
                    }
                }
            }

            return Ok(target);
        }

        // Normalize /workspace/ prefix (used by Firecracker VMs) to relative path
        // This allows consistent path handling between python_executor and file_read/file_write
        let normalized_path = path
            .strip_prefix("/workspace/")
            .or_else(|| path.strip_prefix("/workspace"))
            .unwrap_or(path);

        // Security: Reject absolute paths upfront (after normalization)
        let req_path = std::path::Path::new(normalized_path);
        if req_path.is_absolute() {
            warn!(
                session_id = %session_id,
                path = %path,
                normalized = %normalized_path,
                violation = "absolute_path",
                "Security violation: absolute path not allowed"
            );
            return Err(Status::permission_denied(
                "Absolute paths not allowed; use relative paths within session workspace",
            ));
        }

        let workspace = self
            .workspace_mgr
            .get_workspace(session_id)
            .map_err(|e| Status::invalid_argument(format!("Invalid session: {}", e)))?;

        // Handle empty path as workspace root
        let relative = if normalized_path.is_empty() {
            "."
        } else {
            normalized_path
        };
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
    fn check_workspace_quota(&self, session_id: &str, additional_bytes: u64) -> Result<(), Status> {
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

    /// Check memory quota before /memory write operations.
    fn check_memory_quota(&self, user_id: &str, additional_bytes: u64) -> Result<(), Status> {
        let current_size = self
            .memory_mgr
            .get_memory_size(user_id)
            .map_err(|e| Status::invalid_argument(format!("Cannot check memory quota: {}", e)))?;

        if current_size + additional_bytes > self.config.max_memory_bytes {
            return Err(Status::resource_exhausted(format!(
                "Memory quota exceeded: {} + {} > {} bytes",
                current_size, additional_bytes, self.config.max_memory_bytes
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

        let target = self.resolve_path(&req.session_id, &req.user_id, &req.path)?;

        if !target.exists() {
            return Ok(Response::new(FileReadResponse {
                success: false,
                error: format!("File not found: {}. Use file_list(\".\") to discover correct file paths.", req.path),
                ..Default::default()
            }));
        }

        if !target.is_file() {
            return Ok(Response::new(FileReadResponse {
                success: false,
                error: format!("Path is a directory, not a file: {}. Use file_list(\"{}\") to see its contents.", req.path, req.path),
                ..Default::default()
            }));
        }

        // Check file size
        let metadata = std::fs::metadata(&target)
            .map_err(|e| Status::not_found(format!("File not found: {}", e)))?;

        // Cap max_bytes at configured limit to prevent bypass
        let max_bytes = if req.max_bytes > 0 {
            std::cmp::min(req.max_bytes as usize, self.config.max_read_bytes)
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

        let is_memory_path = req.path.starts_with("/memory/") || req.path == "/memory";
        if is_memory_path {
            self.check_memory_quota(&req.user_id, req.content.len() as u64)?;
        } else {
            self.check_workspace_quota(&req.session_id, req.content.len() as u64)?;
        }

        // Handle /memory/ prefix — resolve via user memory directory
        if is_memory_path {
            let target = self.resolve_path(&req.session_id, &req.user_id, &req.path)?;

            // Create parent directories if requested
            if req.create_dirs {
                if let Some(parent) = target.parent() {
                    std::fs::create_dir_all(parent).map_err(|e| {
                        Status::internal(format!("Failed to create parent dirs: {}", e))
                    })?;
                }
            }

            let bytes_written = if req.append {
                use std::io::Write;
                let mut f = std::fs::OpenOptions::new()
                    .append(true)
                    .create(true)
                    .open(&target)
                    .map_err(|e| Status::internal(format!("Failed to open for append: {}", e)))?;
                let content = req.content.as_bytes();
                f.write_all(content)
                    .map_err(|e| Status::internal(format!("Write failed: {}", e)))?;
                content.len() as i64
            } else {
                let content = req.content.as_bytes();
                std::fs::write(&target, content)
                    .map_err(|e| Status::internal(format!("Write failed: {}", e)))?;
                content.len() as i64
            };

            info!(
                user_id = %req.user_id,
                path = %req.path,
                bytes = bytes_written,
                "Memory file write completed"
            );

            return Ok(Response::new(FileWriteResponse {
                success: true,
                bytes_written,
                absolute_path: target.to_string_lossy().to_string(),
                ..Default::default()
            }));
        }

        // Normalize /workspace/ prefix (used by Firecracker VMs) to relative path
        let normalized_path = req
            .path
            .strip_prefix("/workspace/")
            .or_else(|| req.path.strip_prefix("/workspace"))
            .unwrap_or(&req.path);

        // Security: Reject absolute paths upfront (after normalization)
        let req_path = std::path::Path::new(normalized_path);
        if req_path.is_absolute() {
            warn!(
                session_id = %req.session_id,
                path = %req.path,
                normalized = %normalized_path,
                violation = "absolute_path",
                operation = "file_write",
                "Security violation: absolute path not allowed"
            );
            return Ok(Response::new(FileWriteResponse {
                success: false,
                error: "Absolute paths not allowed; use relative paths within session workspace"
                    .to_string(),
                ..Default::default()
            }));
        }

        let workspace = self
            .workspace_mgr
            .get_workspace(&req.session_id)
            .map_err(|e| Status::invalid_argument(format!("Invalid session: {}", e)))?;

        let target = workspace.join(normalized_path);

        // Security: Validate ALL path components BEFORE any directory creation
        // to prevent symlink attacks via parent directory manipulation
        fn validate_path_components(
            workspace: &std::path::Path,
            target: &std::path::Path,
            session_id: &str,
        ) -> Result<(), Status> {
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
            for component in target
                .strip_prefix(workspace)
                .unwrap_or(target)
                .components()
            {
                use std::path::Component;
                match component {
                    Component::Normal(name) => {
                        current = current.join(name);
                        if current.exists() {
                            // Check for symlinks pointing outside workspace
                            let metadata = std::fs::symlink_metadata(&current).map_err(|e| {
                                Status::internal(format!("Path check error: {}", e))
                            })?;
                            if metadata.file_type().is_symlink() {
                                let link_target = std::fs::read_link(&current).map_err(|e| {
                                    Status::internal(format!("Symlink read error: {}", e))
                                })?;
                                // Resolve symlink and verify it's within workspace
                                let resolved = if link_target.is_absolute() {
                                    link_target
                                } else {
                                    current.parent().unwrap_or(workspace).join(&link_target)
                                };
                                // Symlink must be resolvable and within workspace
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
                                    return Err(Status::permission_denied(
                                        "Symlink escapes workspace",
                                    ));
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
                        return Err(Status::permission_denied(
                            "Parent directory traversal not allowed",
                        ));
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
                std::fs::create_dir_all(parent).map_err(|e| {
                    Status::internal(format!("Failed to create directories: {}", e))
                })?;
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
            let canonical = target
                .canonicalize()
                .map_err(|e| Status::internal(format!("Path resolution error: {}", e)))?;
            if !canonical.starts_with(&workspace) {
                warn!(
                    session_id = %req.session_id,
                    path = %req.path,
                    "Path escape detected after creation"
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

        let target = self.resolve_path(&req.session_id, &req.user_id, &req.path)?;
        let workspace = if req.path.starts_with("/memory/") || req.path == "/memory" {
            self.memory_mgr
                .get_memory_dir(&req.user_id)
                .map_err(|e| Status::invalid_argument(format!("Invalid user for memory: {}", e)))?
        } else {
            self.workspace_mgr
                .get_workspace(&req.session_id)
                .map_err(|e| Status::invalid_argument(format!("Invalid session: {}", e)))?
        };

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

                let metadata = entry.metadata()?;
                let path = entry.path();
                let is_file = metadata.is_file();

                // Apply pattern filter to FILES only — always traverse directories
                // when recursive. This prevents pattern="*.md" from skipping
                // directories like "findings/" and never seeing their contents.
                let matches_pattern = pattern.is_empty() || glob_match(pattern, &name);
                if !matches_pattern && is_file {
                    continue;
                }

                // For non-recursive mode, skip non-matching directories too
                if !matches_pattern && !recursive {
                    continue;
                }

                let relative = path
                    .strip_prefix(workspace)
                    .unwrap_or(&path)
                    .to_string_lossy()
                    .to_string();

                // Only add matching entries to results
                if matches_pattern {
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
                }

                // Always recurse into directories when recursive=true,
                // regardless of pattern match
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
            user_id = %req.user_id,
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
                return Ok(Response::new(CommandResponse {
                    success: false,
                    error: format!("Command not allowed: {}", e),
                    exit_code: 1,
                    ..Default::default()
                }));
            }
        };
        // Resolve memory access only when the command references /memory.
        let memory_workspace = if cmd.uses_memory() {
            if req.user_id.is_empty() {
                return Ok(Response::new(CommandResponse {
                    success: false,
                    error: "Cannot access /memory without authenticated user_id".to_string(),
                    exit_code: 1,
                    ..Default::default()
                }));
            } else {
                match self.memory_mgr.get_memory_dir(&req.user_id) {
                    Ok(dir) => Some(dir),
                    Err(e) => {
                        return Err(Status::invalid_argument(format!(
                            "Invalid user for memory: {}",
                            e
                        )))
                    }
                }
            }
        } else {
            None
        };

        let start = Instant::now();

        // Enforce timeout (use request timeout or config default, max 30s)
        let timeout_secs = if req.timeout_seconds > 0 {
            req.timeout_seconds
                .min(self.config.command_timeout_seconds as i32) as u64
        } else {
            self.config.command_timeout_seconds as u64
        };
        let timeout = Duration::from_secs(timeout_secs);

        // Execute command with timeout using spawn_blocking for sync operations
        let result = tokio::time::timeout(timeout, async {
            let memory_workspace = memory_workspace;
            tokio::task::spawn_blocking(move || match memory_workspace {
                Some(dir) => cmd.execute_with_memory(&workspace, Some(&dir)),
                None => cmd.execute(&workspace),
            })
            .await
            .map_err(|e| anyhow::anyhow!("Task panicked: {}", e))?
        })
        .await;

        let execution_time_ms = start.elapsed().as_millis() as i64;

        match result {
            Ok(Ok(output)) => Ok(Response::new(CommandResponse {
                success: output.exit_code == 0,
                stdout: output.stdout,
                stderr: output.stderr,
                exit_code: output.exit_code,
                error: String::new(),
                execution_time_ms,
            })),
            Ok(Err(e)) => Ok(Response::new(CommandResponse {
                success: false,
                stdout: String::new(),
                stderr: e.to_string(),
                exit_code: 1,
                error: format!("Execution error: {}", e),
                execution_time_ms,
            })),
            Err(_) => {
                warn!(
                    session_id = %req.session_id,
                    command = %req.command,
                    timeout_secs = timeout_secs,
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

    async fn file_search(
        &self,
        request: Request<FileSearchRequest>,
    ) -> Result<Response<FileSearchResponse>, Status> {
        let req = request.into_inner();
        info!(
            session_id = %req.session_id,
            query = %req.query,
            path = %req.path,
            regex = req.regex,
            include = %req.include,
            context_lines = req.context_lines,
            operation = "file_search",
            "Sandbox file search operation"
        );

        // Validate query
        if req.query.is_empty() {
            return Ok(Response::new(FileSearchResponse {
                success: false,
                error: "Search query cannot be empty".to_string(),
                ..Default::default()
            }));
        }
        if req.query.len() > 200 {
            return Ok(Response::new(FileSearchResponse {
                success: false,
                error: "Search query too long (max 200 chars)".to_string(),
                ..Default::default()
            }));
        }

        let context_lines = req.context_lines.clamp(0, 5) as usize;
        let max_results = if req.max_results > 0 { req.max_results as usize } else { 100 };

        let target = self.resolve_path(&req.session_id, "", &req.path)?;
        let workspace = self
            .workspace_mgr
            .get_workspace(&req.session_id)
            .map_err(|e| Status::invalid_argument(format!("Invalid session: {}", e)))?;

        if !target.is_dir() {
            return Ok(Response::new(FileSearchResponse {
                success: false,
                error: "Path is not a directory".to_string(),
                ..Default::default()
            }));
        }

        // Build matcher
        let compiled_regex: Option<Regex> = if req.regex {
            match Regex::new(&format!("(?i){}", &req.query)) {
                Ok(r) => Some(r),
                Err(e) => {
                    return Ok(Response::new(FileSearchResponse {
                        success: false,
                        error: format!("Invalid regex: {}", e),
                        ..Default::default()
                    }));
                }
            }
        } else {
            None
        };
        let query_lower = req.query.to_lowercase();

        let mut matches = Vec::new();
        let mut files_scanned: i32 = 0;
        let mut truncated = false;

        const MAX_FILES: usize = 1000;
        const MAX_FILE_SIZE: u64 = 500 * 1024; // 500KB

        // Known binary extensions to skip
        fn is_binary_ext(ext: &str) -> bool {
            matches!(
                ext,
                "png" | "jpg" | "jpeg" | "gif" | "bmp" | "ico" | "svg"
                    | "pdf" | "zip" | "tar" | "gz" | "bz2" | "xz" | "7z"
                    | "exe" | "dll" | "so" | "dylib" | "o" | "a"
                    | "wasm" | "pyc" | "class" | "jar"
                    | "mp3" | "mp4" | "avi" | "mov" | "wav"
                    | "ttf" | "otf" | "woff" | "woff2" | "eot"
                    | "sqlite" | "db"
            )
        }

        fn walk_and_search(
            dir: &std::path::Path,
            workspace: &std::path::Path,
            include_pattern: &str,
            compiled_regex: &Option<Regex>,
            query_lower: &str,
            context_lines: usize,
            max_results: usize,
            matches: &mut Vec<SearchMatch>,
            files_scanned: &mut i32,
            truncated: &mut bool,
        ) -> Result<(), std::io::Error> {
            if *files_scanned as usize >= MAX_FILES || matches.len() >= max_results {
                *truncated = true;
                return Ok(());
            }

            let entries = match std::fs::read_dir(dir) {
                Ok(e) => e,
                Err(_) => return Ok(()),
            };

            for entry in entries {
                let entry = entry?;
                let name = entry.file_name().to_string_lossy().to_string();

                // Skip hidden
                if name.starts_with('.') {
                    continue;
                }

                let path = entry.path();
                let metadata = match entry.metadata() {
                    Ok(m) => m,
                    Err(_) => continue,
                };

                if metadata.is_dir() {
                    walk_and_search(
                        &path,
                        workspace,
                        include_pattern,
                        compiled_regex,
                        query_lower,
                        context_lines,
                        max_results,
                        matches,
                        files_scanned,
                        truncated,
                    )?;
                    continue;
                }

                if !metadata.is_file() {
                    continue;
                }

                // Apply include glob filter on filename
                if !include_pattern.is_empty() && !glob_match(include_pattern, &name) {
                    continue;
                }

                // Skip binary extensions
                let ext = path.extension().and_then(|e| e.to_str()).unwrap_or("").to_lowercase();
                if is_binary_ext(&ext) {
                    continue;
                }

                // Skip large files
                if metadata.len() > MAX_FILE_SIZE {
                    continue;
                }

                *files_scanned += 1;
                if *files_scanned as usize > MAX_FILES {
                    *truncated = true;
                    return Ok(());
                }

                // Read file content
                let content = match std::fs::read_to_string(&path) {
                    Ok(c) => c,
                    Err(_) => continue, // skip non-UTF8 / unreadable
                };

                let lines: Vec<&str> = content.lines().collect();
                let relative = path
                    .strip_prefix(workspace)
                    .unwrap_or(&path)
                    .to_string_lossy()
                    .to_string();

                for (idx, line) in lines.iter().enumerate() {
                    let is_match = if let Some(ref re) = compiled_regex {
                        re.is_match(line)
                    } else {
                        line.to_lowercase().contains(query_lower)
                    };

                    if is_match {
                        // Collect context lines
                        let mut context = Vec::new();
                        if context_lines > 0 {
                            let start = idx.saturating_sub(context_lines);
                            let end = std::cmp::min(idx + context_lines + 1, lines.len());
                            for ci in start..end {
                                if ci == idx {
                                    continue;
                                }
                                context.push(ContextLine {
                                    line: (ci + 1) as i32,
                                    content: lines[ci].to_string(),
                                });
                            }
                        }

                        matches.push(SearchMatch {
                            file: relative.clone(),
                            line: (idx + 1) as i32,
                            content: line.to_string(),
                            context,
                        });

                        if matches.len() >= max_results {
                            *truncated = true;
                            return Ok(());
                        }
                    }
                }
            }
            Ok(())
        }

        walk_and_search(
            &target,
            &workspace,
            &req.include,
            &compiled_regex,
            &query_lower,
            context_lines,
            max_results,
            &mut matches,
            &mut files_scanned,
            &mut truncated,
        )
        .map_err(|e| Status::internal(format!("Search error: {}", e)))?;

        let matches_found = matches.len() as i32;

        Ok(Response::new(FileSearchResponse {
            success: true,
            matches,
            error: String::new(),
            files_scanned,
            matches_found,
            truncated,
        }))
    }

    async fn file_edit(
        &self,
        request: Request<FileEditRequest>,
    ) -> Result<Response<FileEditResponse>, Status> {
        let req = request.into_inner();
        info!(
            session_id = %req.session_id,
            path = %req.path,
            replace_all = req.replace_all,
            operation = "file_edit",
            "Sandbox file edit operation"
        );

        let target = self.resolve_path(&req.session_id, "", &req.path)?;

        if !target.exists() {
            return Ok(Response::new(FileEditResponse {
                success: false,
                error: format!("File not found: {}. Use file_list(\".\") to discover correct file paths.", req.path),
                ..Default::default()
            }));
        }

        if !target.is_file() {
            return Ok(Response::new(FileEditResponse {
                success: false,
                error: format!("Path is a directory, not a file: {}", req.path),
                ..Default::default()
            }));
        }

        if req.old_text.is_empty() {
            return Ok(Response::new(FileEditResponse {
                success: false,
                error: "old_text cannot be empty".to_string(),
                ..Default::default()
            }));
        }

        let content = std::fs::read_to_string(&target)
            .map_err(|e| Status::internal(format!("Read error: {}", e)))?;

        let (new_content, replacements) = if req.replace_all {
            let count = content.matches(&req.old_text).count() as i32;
            (content.replace(&req.old_text, &req.new_text), count)
        } else {
            if let Some(pos) = content.find(&req.old_text) {
                let mut result = String::with_capacity(content.len());
                result.push_str(&content[..pos]);
                result.push_str(&req.new_text);
                result.push_str(&content[pos + req.old_text.len()..]);
                (result, 1)
            } else {
                return Ok(Response::new(FileEditResponse {
                    success: false,
                    error: "old_text not found in file".to_string(),
                    ..Default::default()
                }));
            }
        };

        if replacements == 0 {
            return Ok(Response::new(FileEditResponse {
                success: false,
                error: "old_text not found in file".to_string(),
                ..Default::default()
            }));
        }

        std::fs::write(&target, &new_content)
            .map_err(|e| Status::internal(format!("Write error: {}", e)))?;

        // Build a short snippet around the first replacement (char-boundary safe)
        let snippet = if let Some(pos) = new_content.find(&req.new_text) {
            let raw_start = pos.saturating_sub(40);
            let raw_end = std::cmp::min(pos + req.new_text.len() + 40, new_content.len());
            // Find safe char boundaries to avoid panic on multi-byte UTF-8
            let start = new_content[..raw_start + 1]
                .char_indices()
                .map(|(i, _)| i)
                .take_while(|&i| i <= raw_start)
                .last()
                .unwrap_or(0);
            let end = new_content[raw_end..]
                .char_indices()
                .map(|(i, _)| raw_end + i)
                .next()
                .unwrap_or(new_content.len());
            new_content[start..end].to_string()
        } else {
            String::new()
        };

        Ok(Response::new(FileEditResponse {
            success: true,
            error: String::new(),
            replacements,
            snippet,
            file_size_after: new_content.len() as i64,
        }))
    }

    async fn file_delete(
        &self,
        request: Request<FileDeleteRequest>,
    ) -> Result<Response<FileDeleteResponse>, Status> {
        let req = request.into_inner();
        info!(
            session_id = %req.session_id,
            path = %req.path,
            pattern = %req.pattern,
            recursive = req.recursive,
            operation = "file_delete",
            "Sandbox file delete operation"
        );

        // Hard-reject /memory/ paths
        if req.path.starts_with("/memory/") || req.path.starts_with("/memory")
            || req.path.starts_with("memory/") || req.path == "memory"
        {
            return Ok(Response::new(FileDeleteResponse {
                success: false,
                error: "Deleting from /memory/ is not allowed. file_delete only works in workspace.".to_string(),
                deleted_count: 0,
                deleted_paths: Vec::new(),
            }));
        }

        // Reject path traversal attempts
        if req.path.contains("..") {
            warn!(
                session_id = %req.session_id,
                path = %req.path,
                violation = "path_traversal",
                operation = "file_delete",
                "Security violation: path traversal attempt detected"
            );
            return Err(Status::permission_denied("Path traversal not allowed"));
        }

        let target = self.resolve_path(&req.session_id, "", &req.path)?;
        let workspace = self
            .workspace_mgr
            .get_workspace(&req.session_id)
            .map_err(|e| Status::invalid_argument(format!("Invalid session: {}", e)))?;

        let pattern = req.pattern.as_str();

        // --- Mode 1: Single target (no pattern) ---
        if pattern.is_empty() {
            if !target.exists() {
                return Ok(Response::new(FileDeleteResponse {
                    success: true,
                    error: String::new(),
                    deleted_count: 0,
                    deleted_paths: Vec::new(),
                }));
            }

            let relative = target
                .strip_prefix(&workspace)
                .unwrap_or(&target)
                .to_string_lossy()
                .to_string();

            if target.is_file() || target.is_symlink() {
                std::fs::remove_file(&target)
                    .map_err(|e| Status::internal(format!("Delete error: {}", e)))?;
                return Ok(Response::new(FileDeleteResponse {
                    success: true,
                    error: String::new(),
                    deleted_count: 1,
                    deleted_paths: vec![relative],
                }));
            }

            if target.is_dir() {
                match std::fs::remove_dir(&target) {
                    Ok(()) => {
                        return Ok(Response::new(FileDeleteResponse {
                            success: true,
                            error: String::new(),
                            deleted_count: 1,
                            deleted_paths: vec![relative],
                        }));
                    }
                    Err(e) => {
                        return Ok(Response::new(FileDeleteResponse {
                            success: false,
                            error: format!("Directory not empty or cannot delete: {}", e),
                            deleted_count: 0,
                            deleted_paths: Vec::new(),
                        }));
                    }
                }
            }

            return Ok(Response::new(FileDeleteResponse {
                success: false,
                error: format!("Unknown file type at path: {}", req.path),
                deleted_count: 0,
                deleted_paths: Vec::new(),
            }));
        }

        // --- Mode 2: Glob pattern ---
        if !target.is_dir() {
            return Ok(Response::new(FileDeleteResponse {
                success: false,
                error: format!("Path must be a directory when using pattern, got: {}", req.path),
                deleted_count: 0,
                deleted_paths: Vec::new(),
            }));
        }

        let mut deleted_paths = Vec::new();
        let mut errors = Vec::new();

        fn collect_and_delete(
            dir: &std::path::Path,
            canonical_workspace: &std::path::Path,
            pattern: &str,
            recursive: bool,
            deleted_paths: &mut Vec<String>,
            errors: &mut Vec<String>,
        ) -> Result<(), std::io::Error> {
            for entry in std::fs::read_dir(dir)? {
                let entry = entry?;
                let name = entry.file_name().to_string_lossy().to_string();
                let path = entry.path();
                let sym_metadata = match path.symlink_metadata() {
                    Ok(m) => m,
                    Err(_) => continue,
                };

                // Use path.metadata() (follows symlinks) to detect symlink-to-directory.
                // symlink_metadata().is_dir() is always false for symlinks themselves.
                let is_dir = if sym_metadata.is_symlink() {
                    path.metadata().map(|m| m.is_dir()).unwrap_or(false)
                } else {
                    sym_metadata.is_dir()
                };

                if is_dir && recursive {
                    // Symlink escape prevention: canonicalize before recursing
                    // and verify the resolved path is still within the workspace.
                    if sym_metadata.is_symlink() {
                        match path.canonicalize() {
                            Ok(canonical) if canonical.starts_with(canonical_workspace) => {
                                collect_and_delete(&canonical, canonical_workspace, pattern, recursive, deleted_paths, errors)?;
                            }
                            Ok(canonical) => {
                                errors.push(format!("{}: symlink escapes workspace (-> {})", path.display(), canonical.display()));
                                continue;
                            }
                            Err(e) => {
                                errors.push(format!("{}: cannot resolve symlink: {}", path.display(), e));
                                continue;
                            }
                        }
                    } else {
                        collect_and_delete(&path, canonical_workspace, pattern, recursive, deleted_paths, errors)?;
                    }
                    if glob_match(pattern, &name) {
                        if let Ok(()) = std::fs::remove_dir(&path) {
                            let relative = path
                                .strip_prefix(canonical_workspace)
                                .unwrap_or(&path)
                                .to_string_lossy()
                                .to_string();
                            deleted_paths.push(relative);
                        }
                    }
                    continue;
                }

                if !glob_match(pattern, &name) {
                    continue;
                }

                let relative = path
                    .strip_prefix(canonical_workspace)
                    .unwrap_or(&path)
                    .to_string_lossy()
                    .to_string();

                if sym_metadata.is_file() || sym_metadata.is_symlink() {
                    match std::fs::remove_file(&path) {
                        Ok(()) => deleted_paths.push(relative),
                        Err(e) => errors.push(format!("{}: {}", relative, e)),
                    }
                } else if is_dir {
                    match std::fs::remove_dir(&path) {
                        Ok(()) => deleted_paths.push(relative),
                        Err(e) => errors.push(format!("{}: {}", relative, e)),
                    }
                }
            }
            Ok(())
        }

        // Canonicalize workspace for symlink containment checks
        let canonical_workspace = workspace.canonicalize()
            .map_err(|e| Status::internal(format!("Workspace canonicalize error: {}", e)))?;

        collect_and_delete(&target, &canonical_workspace, pattern, req.recursive, &mut deleted_paths, &mut errors)
            .map_err(|e| Status::internal(format!("Delete walk error: {}", e)))?;

        let deleted_count = deleted_paths.len() as i32;
        let error_msg = if errors.is_empty() {
            String::new()
        } else {
            format!("Some deletions failed: {}", errors.join("; "))
        };

        Ok(Response::new(FileDeleteResponse {
            success: errors.is_empty(),
            error: error_msg,
            deleted_count,
            deleted_paths,
        }))
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
        (Some(p), Some(n)) if *p == *n => glob_match_recursive(&pattern[1..], &name[1..]),
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
            user_id: String::new(),
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
            user_id: String::new(),
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
            user_id: String::new(),
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
            user_id: String::new(),
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
            user_id: String::new(),
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
            user_id: String::new(),
        };
        let resp = service.file_read(tonic::Request::new(read_req)).await;

        // Should fail - path escapes workspace
        match resp {
            Ok(r) => {
                let inner = r.into_inner();
                assert!(!inner.success || inner.content.is_empty());
            }
            Err(e) => assert!(
                e.code() == tonic::Code::PermissionDenied || e.code() == tonic::Code::NotFound
            ),
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
            user_id: String::new(),
        };
        let resp = service.file_write(tonic::Request::new(write_req)).await;

        // Should fail due to quota
        assert!(resp.is_err());
        let err = resp.unwrap_err();
        assert_eq!(err.code(), tonic::Code::ResourceExhausted);
    }

    #[tokio::test]
    async fn test_memory_quota_enforcement() {
        let temp = tempfile::TempDir::new().unwrap();
        let config = SandboxConfig {
            max_memory_bytes: 100, // Very small memory quota for testing
            ..Default::default()
        };
        let service = SandboxServiceImpl::with_config(temp.path().to_path_buf(), config);

        // Write to /memory/ path that exceeds memory quota
        let write_req = FileWriteRequest {
            session_id: "test-session".to_string(),
            path: "/memory/notes.txt".to_string(),
            content: "x".repeat(200), // 200 bytes, exceeds 100 byte memory quota
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
            user_id: "test-memory-quota-user".to_string(),
        };
        let resp = service.file_write(tonic::Request::new(write_req)).await;

        // Should fail due to memory quota (not workspace quota)
        assert!(resp.is_err());
        let err = resp.unwrap_err();
        assert_eq!(err.code(), tonic::Code::ResourceExhausted);
        assert!(
            err.message().contains("Memory quota"),
            "Expected memory quota error, got: {}",
            err.message()
        );
    }

    #[tokio::test]
    async fn test_dangerous_command_rejected() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());

        let req = CommandRequest {
            session_id: "test-session".to_string(),
            command: "curl http://evil.com".to_string(),
            timeout_seconds: 5,
            user_id: String::new(),
        };
        let resp = service
            .execute_command(tonic::Request::new(req))
            .await
            .unwrap();
        let inner = resp.into_inner();

        assert!(!inner.success);
        assert!(inner.error.contains("not allowed"));
    }

    #[tokio::test]
    async fn test_file_delete_single_file() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());
        let workspace = temp.path().join("test-session");
        std::fs::create_dir_all(&workspace).unwrap();
        std::fs::write(workspace.join("to_delete.txt"), "delete me").unwrap();

        let req = FileDeleteRequest {
            session_id: "test-session".to_string(),
            path: "to_delete.txt".to_string(),
            pattern: String::new(),
            recursive: false,
        };
        let resp = service.file_delete(tonic::Request::new(req)).await.unwrap();
        let inner = resp.into_inner();
        assert!(inner.success);
        assert_eq!(inner.deleted_count, 1);
        assert_eq!(inner.deleted_paths, vec!["to_delete.txt"]);
        assert!(!workspace.join("to_delete.txt").exists());
    }

    #[tokio::test]
    async fn test_file_delete_empty_dir() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());
        let workspace = temp.path().join("test-session");
        std::fs::create_dir_all(workspace.join("empty_dir")).unwrap();

        let req = FileDeleteRequest {
            session_id: "test-session".to_string(),
            path: "empty_dir".to_string(),
            pattern: String::new(),
            recursive: false,
        };
        let resp = service.file_delete(tonic::Request::new(req)).await.unwrap();
        let inner = resp.into_inner();
        assert!(inner.success);
        assert_eq!(inner.deleted_count, 1);
        assert!(!workspace.join("empty_dir").exists());
    }

    #[tokio::test]
    async fn test_file_delete_non_empty_dir_rejected() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());
        let workspace = temp.path().join("test-session");
        std::fs::create_dir_all(workspace.join("non_empty")).unwrap();
        std::fs::write(workspace.join("non_empty/file.txt"), "content").unwrap();

        let req = FileDeleteRequest {
            session_id: "test-session".to_string(),
            path: "non_empty".to_string(),
            pattern: String::new(),
            recursive: false,
        };
        let resp = service.file_delete(tonic::Request::new(req)).await.unwrap();
        let inner = resp.into_inner();
        assert!(!inner.success);
        assert!(inner.error.contains("not empty") || inner.error.contains("Directory"));
        assert!(workspace.join("non_empty").exists());
    }

    #[tokio::test]
    async fn test_file_delete_not_found_idempotent() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());
        let workspace = temp.path().join("test-session");
        std::fs::create_dir_all(&workspace).unwrap();

        let req = FileDeleteRequest {
            session_id: "test-session".to_string(),
            path: "nonexistent.txt".to_string(),
            pattern: String::new(),
            recursive: false,
        };
        let resp = service.file_delete(tonic::Request::new(req)).await.unwrap();
        let inner = resp.into_inner();
        assert!(inner.success);
        assert_eq!(inner.deleted_count, 0);
    }

    #[tokio::test]
    async fn test_file_delete_memory_path_rejected() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());

        let req = FileDeleteRequest {
            session_id: "test-session".to_string(),
            path: "/memory/notes.md".to_string(),
            pattern: String::new(),
            recursive: false,
        };
        let resp = service.file_delete(tonic::Request::new(req)).await.unwrap();
        let inner = resp.into_inner();
        assert!(!inner.success);
        assert!(inner.error.to_lowercase().contains("memory"));
    }

    #[tokio::test]
    async fn test_file_delete_glob_pattern() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());
        let workspace = temp.path().join("test-session");
        std::fs::create_dir_all(&workspace).unwrap();
        std::fs::write(workspace.join("a.tmp"), "temp1").unwrap();
        std::fs::write(workspace.join("b.tmp"), "temp2").unwrap();
        std::fs::write(workspace.join("keep.txt"), "keep").unwrap();

        let req = FileDeleteRequest {
            session_id: "test-session".to_string(),
            path: ".".to_string(),
            pattern: "*.tmp".to_string(),
            recursive: false,
        };
        let resp = service.file_delete(tonic::Request::new(req)).await.unwrap();
        let inner = resp.into_inner();
        assert!(inner.success);
        assert_eq!(inner.deleted_count, 2);
        assert!(!workspace.join("a.tmp").exists());
        assert!(!workspace.join("b.tmp").exists());
        assert!(workspace.join("keep.txt").exists());
    }

    #[tokio::test]
    async fn test_file_delete_glob_recursive() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());
        let workspace = temp.path().join("test-session");
        std::fs::create_dir_all(workspace.join("sub")).unwrap();
        std::fs::write(workspace.join("a.tmp"), "temp1").unwrap();
        std::fs::write(workspace.join("sub/b.tmp"), "temp2").unwrap();
        std::fs::write(workspace.join("sub/keep.txt"), "keep").unwrap();

        let req = FileDeleteRequest {
            session_id: "test-session".to_string(),
            path: ".".to_string(),
            pattern: "*.tmp".to_string(),
            recursive: true,
        };
        let resp = service.file_delete(tonic::Request::new(req)).await.unwrap();
        let inner = resp.into_inner();
        assert!(inner.success);
        assert_eq!(inner.deleted_count, 2);
        assert!(!workspace.join("a.tmp").exists());
        assert!(!workspace.join("sub/b.tmp").exists());
        assert!(workspace.join("sub/keep.txt").exists());
    }

    #[tokio::test]
    async fn test_file_delete_path_traversal_rejected() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());
        let workspace = temp.path().join("test-session");
        std::fs::create_dir_all(&workspace).unwrap();

        let req = FileDeleteRequest {
            session_id: "test-session".to_string(),
            path: "../../../etc/passwd".to_string(),
            pattern: String::new(),
            recursive: false,
        };
        let resp = service.file_delete(tonic::Request::new(req)).await;
        assert!(resp.is_err());
    }

    /// Verify that collect_and_delete detects symlinks pointing to directories
    /// outside the workspace (symlink escape via symlink-to-directory).
    ///
    /// Before the fix, symlink_metadata().is_dir() was always false for symlinks,
    /// so the escape-check branch was dead code and the symlink was silently skipped.
    #[cfg(unix)]
    #[tokio::test]
    async fn test_file_delete_symlink_to_external_dir_detected() {
        let temp = tempfile::TempDir::new().unwrap();
        let service = SandboxServiceImpl::new(temp.path().to_path_buf());
        let workspace = temp.path().join("test-session");
        std::fs::create_dir_all(&workspace).unwrap();

        // Create an external directory with a sensitive file (outside workspace).
        let external_dir = temp.path().join("external");
        std::fs::create_dir_all(&external_dir).unwrap();
        std::fs::write(external_dir.join("secret.txt"), "secret").unwrap();

        // Create a symlink inside the workspace pointing to the external directory.
        let symlink_path = workspace.join("escape_link");
        std::os::unix::fs::symlink(&external_dir, &symlink_path).unwrap();

        // Attempt a recursive glob delete starting from the workspace root.
        // With the bug, the symlink-to-dir escape check was dead code and the
        // symlink would be silently skipped (no error, no deletion).
        // With the fix, collect_and_delete must detect the escape and report an error.
        let req = FileDeleteRequest {
            session_id: "test-session".to_string(),
            path: ".".to_string(),
            pattern: "*".to_string(),
            recursive: true,
        };
        let resp = service.file_delete(tonic::Request::new(req)).await.unwrap();
        let inner = resp.into_inner();

        // The response should report the symlink escape in its error field.
        assert!(
            !inner.error.is_empty(),
            "Expected symlink escape error, but error field was empty. \
             success={}, deleted_paths={:?}",
            inner.success,
            inner.deleted_paths,
        );
        assert!(
            inner.error.contains("symlink escapes workspace"),
            "Error message should mention symlink escape, got: {}",
            inner.error,
        );

        // The external directory must not have been modified.
        assert!(
            external_dir.join("secret.txt").exists(),
            "External file should not have been deleted",
        );
    }
}
