//! Session workspace management for WASI sandbox isolation.
//!
//! Each session gets an isolated directory that can be mounted
//! into WASI sandboxes with read-write permissions.

use anyhow::{anyhow, Result};
use std::path::{Path, PathBuf};
use tracing::{debug, info};

/// Manages per-session workspace directories.
pub struct WorkspaceManager {
    base_dir: PathBuf,
}

impl WorkspaceManager {
    /// Create a new workspace manager with the given base directory.
    pub fn new(base_dir: PathBuf) -> Self {
        Self { base_dir }
    }

    /// Create from environment variable or default.
    pub fn from_env() -> Self {
        let base = std::env::var("SHANNON_SESSION_WORKSPACES_DIR")
            .unwrap_or_else(|_| "/tmp/shannon-sessions".to_string());
        Self::new(PathBuf::from(base))
    }

    /// Get the base directory.
    pub fn base_dir(&self) -> &Path {
        &self.base_dir
    }

    /// Validate session ID format (alphanumeric + hyphen only).
    fn validate_session_id(session_id: &str) -> Result<()> {
        if session_id.is_empty() {
            return Err(anyhow!("Session ID cannot be empty"));
        }
        if session_id.len() > 128 {
            return Err(anyhow!("Session ID too long (max 128 chars)"));
        }
        if !session_id
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || c == '-' || c == '_')
        {
            return Err(anyhow!(
                "Session ID must contain only alphanumeric, hyphen, or underscore"
            ));
        }
        // Reject path traversal attempts
        if session_id.contains("..") || session_id.starts_with('.') {
            return Err(anyhow!("Session ID cannot contain path traversal"));
        }
        Ok(())
    }

    /// Get or create workspace directory for a session.
    ///
    /// Security: Validates that the workspace path doesn't escape the base directory
    /// by checking the canonical path BEFORE and AFTER creation to prevent TOCTOU attacks.
    pub fn get_workspace(&self, session_id: &str) -> Result<PathBuf> {
        Self::validate_session_id(session_id)?;

        // Canonicalize base_dir first to establish trusted root
        let canonical_base = if self.base_dir.exists() {
            self.base_dir.canonicalize()?
        } else {
            std::fs::create_dir_all(&self.base_dir)?;
            self.base_dir.canonicalize()?
        };

        // Construct workspace path within canonical base (prevents symlink injection)
        let workspace = canonical_base.join(session_id);

        // Pre-creation check: Verify the path we're about to create is within base
        // This catches cases where session_id might be manipulated
        if !workspace.starts_with(&canonical_base) {
            return Err(anyhow!("Workspace path escapes base directory"));
        }

        // Check if workspace already exists
        if workspace.exists() {
            // Verify it's a directory, not a symlink to outside
            let metadata = std::fs::symlink_metadata(&workspace)?;
            if metadata.file_type().is_symlink() {
                return Err(anyhow!("Workspace is a symlink (potential attack)"));
            }
            if !metadata.is_dir() {
                return Err(anyhow!("Workspace path exists but is not a directory"));
            }
        } else {
            // Create workspace directory (handle race: another request may create it first)
            match std::fs::create_dir(&workspace) {
                Ok(_) => info!("Created workspace for session: {}", session_id),
                Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {
                    // Race condition: another request created it between exists() and create_dir()
                    let metadata = std::fs::symlink_metadata(&workspace)?;
                    if metadata.file_type().is_symlink() {
                        return Err(anyhow!("Workspace is a symlink (potential attack)"));
                    }
                    if !metadata.is_dir() {
                        return Err(anyhow!("Workspace path exists but is not a directory"));
                    }
                }
                Err(e) => return Err(e.into()),
            }
        }

        // Post-creation verification (defense in depth)
        let canonical = workspace.canonicalize()?;
        if !canonical.starts_with(&canonical_base) {
            // This should never happen if pre-creation checks passed
            // but we check anyway for defense in depth
            std::fs::remove_dir(&workspace).ok(); // Cleanup
            return Err(anyhow!("Workspace path escapes base directory after creation"));
        }

        debug!("Workspace path for {}: {:?}", session_id, canonical);
        Ok(canonical)
    }

    /// Check if a path is within a session's workspace.
    pub fn is_within_workspace(&self, session_id: &str, path: &Path) -> Result<bool> {
        let workspace = self.get_workspace(session_id)?;

        // Handle both existing and non-existing paths
        let check_path = if path.exists() {
            path.canonicalize()?
        } else {
            // For non-existing paths, canonicalize the parent and join
            let parent = path.parent().ok_or_else(|| anyhow!("Invalid path"))?;
            if parent.exists() {
                parent.canonicalize()?.join(path.file_name().unwrap_or_default())
            } else {
                return Ok(false);
            }
        };

        Ok(check_path.starts_with(&workspace))
    }

    /// Get workspace size in bytes.
    pub fn get_workspace_size(&self, session_id: &str) -> Result<u64> {
        let workspace = self.get_workspace(session_id)?;
        dir_size(&workspace)
    }

    /// Delete a session's workspace.
    pub fn delete_workspace(&self, session_id: &str) -> Result<()> {
        Self::validate_session_id(session_id)?;
        let workspace = self.base_dir.join(session_id);

        if workspace.exists() {
            std::fs::remove_dir_all(&workspace)?;
            info!("Deleted workspace for session: {}", session_id);
        }
        Ok(())
    }

    /// List all session workspaces.
    pub fn list_workspaces(&self) -> Result<Vec<String>> {
        let mut sessions = Vec::new();

        if !self.base_dir.exists() {
            return Ok(sessions);
        }

        for entry in std::fs::read_dir(&self.base_dir)? {
            let entry = entry?;
            if entry.file_type()?.is_dir() {
                if let Some(name) = entry.file_name().to_str() {
                    sessions.push(name.to_string());
                }
            }
        }
        Ok(sessions)
    }
}

/// Maximum number of entries to visit during a size scan to prevent DoS
/// via deeply nested or symlink-looped directory trees.
const MAX_DIR_WALK_ENTRIES: usize = 50_000;

/// Calculate total size of a directory recursively.
///
/// Security: Skips symlinks entirely to prevent escape outside the workspace
/// and caps the total number of visited entries to avoid DoS from deeply nested trees.
fn dir_size(path: &Path) -> Result<u64> {
    let mut size = 0u64;
    let mut remaining = MAX_DIR_WALK_ENTRIES;
    dir_size_inner(path, &mut size, &mut remaining)?;
    Ok(size)
}

fn dir_size_inner(path: &Path, size: &mut u64, remaining: &mut usize) -> Result<()> {
    if !path.exists() {
        return Ok(());
    }

    for entry in std::fs::read_dir(path)? {
        if *remaining == 0 {
            return Err(anyhow!("Workspace walk exceeded {} entries", MAX_DIR_WALK_ENTRIES));
        }
        *remaining -= 1;

        let entry = entry?;
        // Use symlink_metadata to avoid following symlinks
        let metadata = entry.metadata().ok();
        let sym_meta = std::fs::symlink_metadata(entry.path()).ok();

        // Skip symlinks entirely (prevents escape + infinite loops)
        if sym_meta.is_some_and(|m| m.file_type().is_symlink()) {
            continue;
        }

        if let Some(meta) = metadata {
            if meta.is_file() {
                *size += meta.len();
            } else if meta.is_dir() {
                dir_size_inner(&entry.path(), size, remaining)?;
            }
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;

    #[test]
    fn test_workspace_creation() {
        let temp = TempDir::new().unwrap();
        let mgr = WorkspaceManager::new(temp.path().to_path_buf());

        let ws = mgr.get_workspace("test-session-123").unwrap();
        assert!(ws.exists());
        assert!(ws.is_dir());
        assert!(ws.ends_with("test-session-123"));
    }

    #[test]
    fn test_invalid_session_id_rejected() {
        let temp = TempDir::new().unwrap();
        let mgr = WorkspaceManager::new(temp.path().to_path_buf());

        assert!(mgr.get_workspace("../escape").is_err());
        assert!(mgr.get_workspace("session;rm -rf").is_err());
        assert!(mgr.get_workspace("").is_err());
    }

    #[test]
    fn test_session_isolation() {
        let temp = TempDir::new().unwrap();
        let mgr = WorkspaceManager::new(temp.path().to_path_buf());

        let ws_a = mgr.get_workspace("session-a").unwrap();
        let ws_b = mgr.get_workspace("session-b").unwrap();

        assert_ne!(ws_a, ws_b);
        assert!(ws_a.ends_with("session-a"));
        assert!(ws_b.ends_with("session-b"));
    }

    #[test]
    fn test_path_within_workspace() {
        let temp = TempDir::new().unwrap();
        let mgr = WorkspaceManager::new(temp.path().to_path_buf());

        // Create workspace and a file
        let ws = mgr.get_workspace("test-session").unwrap();
        let test_file = ws.join("test.txt");
        std::fs::write(&test_file, "hello").unwrap();

        assert!(mgr.is_within_workspace("test-session", &test_file).unwrap());
        assert!(!mgr.is_within_workspace("test-session", Path::new("/etc/passwd")).unwrap());
    }

    #[test]
    fn test_workspace_size() {
        let temp = TempDir::new().unwrap();
        let mgr = WorkspaceManager::new(temp.path().to_path_buf());

        let ws = mgr.get_workspace("test-session").unwrap();
        std::fs::write(ws.join("file1.txt"), "hello").unwrap();
        std::fs::write(ws.join("file2.txt"), "world").unwrap();

        let size = mgr.get_workspace_size("test-session").unwrap();
        assert_eq!(size, 10); // "hello" + "world"
    }

    #[test]
    fn test_delete_workspace() {
        let temp = TempDir::new().unwrap();
        let mgr = WorkspaceManager::new(temp.path().to_path_buf());

        let ws = mgr.get_workspace("to-delete").unwrap();
        std::fs::write(ws.join("file.txt"), "data").unwrap();
        assert!(ws.exists());

        mgr.delete_workspace("to-delete").unwrap();
        assert!(!ws.exists());
    }

    #[test]
    fn test_list_workspaces() {
        let temp = TempDir::new().unwrap();
        let mgr = WorkspaceManager::new(temp.path().to_path_buf());

        mgr.get_workspace("session-a").unwrap();
        mgr.get_workspace("session-b").unwrap();
        mgr.get_workspace("session-c").unwrap();

        let mut workspaces = mgr.list_workspaces().unwrap();
        workspaces.sort();

        assert_eq!(workspaces, vec!["session-a", "session-b", "session-c"]);
    }
}
