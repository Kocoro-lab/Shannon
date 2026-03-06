//! Stub memory manager for OSS builds.
//! Enterprise builds provide persistent per-user memory directories.

use std::path::PathBuf;

/// Manages per-user persistent memory directories.
/// OSS stub: always returns a temp-based path (no-op in practice).
pub struct MemoryManager {
    base_dir: PathBuf,
}

impl MemoryManager {
    /// Create a MemoryManager from environment configuration.
    /// Reads SHANNON_MEMORY_DIR or defaults to /tmp/shannon-memory.
    pub fn from_env() -> Self {
        let base = std::env::var("SHANNON_MEMORY_DIR")
            .unwrap_or_else(|_| "/tmp/shannon-memory".to_string());
        Self {
            base_dir: PathBuf::from(base),
        }
    }

    /// Get the memory directory for a given user.
    /// Creates it if it doesn't exist.
    pub fn get_memory_dir(&self, user_id: &str) -> Result<PathBuf, String> {
        if user_id.is_empty() {
            return Err("user_id is empty".to_string());
        }
        // Sanitize user_id to prevent path traversal
        let safe_id: String = user_id
            .chars()
            .map(|c| if c.is_alphanumeric() || c == '-' || c == '_' { c } else { '_' })
            .collect();
        let dir = self.base_dir.join(&safe_id);
        if let Err(e) = std::fs::create_dir_all(&dir) {
            return Err(format!("Failed to create memory dir: {}", e));
        }
        Ok(dir)
    }
}
