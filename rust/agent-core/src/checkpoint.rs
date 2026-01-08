//! Checkpoint and restore capabilities for sandbox execution.
//!
//! This module provides the foundation for persisting and resuming sandbox state,
//! enabling long-running research tasks to survive failures and restarts.
//!
//! # Overview
//!
//! The checkpoint system supports:
//! - Saving execution state at defined checkpoints
//! - Restoring from saved checkpoints to resume execution
//! - Configurable checkpoint storage (filesystem, S3, etc.)
//!
//! # Future Integration
//!
//! When microsandbox is integrated, this module will provide the interface
//! for its native checkpoint/restore capabilities.

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Serialize};

/// Checkpoint metadata.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CheckpointMetadata {
    /// Unique checkpoint identifier.
    pub id: String,
    /// Job/task ID this checkpoint belongs to.
    pub job_id: String,
    /// Checkpoint name/label.
    pub name: String,
    /// When the checkpoint was created.
    pub created_at: u64,
    /// Size in bytes.
    pub size_bytes: u64,
    /// Execution step/iteration number.
    pub step: u32,
    /// Custom metadata.
    pub custom: HashMap<String, String>,
}

impl CheckpointMetadata {
    /// Create new checkpoint metadata.
    pub fn new(job_id: impl Into<String>, name: impl Into<String>, step: u32) -> Self {
        let timestamp = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_secs())
            .unwrap_or(0);

        let job_id_str = job_id.into();
        let id = format!("{}-{}-{}", job_id_str, step, timestamp);

        Self {
            id,
            job_id: job_id_str,
            name: name.into(),
            created_at: timestamp,
            size_bytes: 0,
            step,
            custom: HashMap::new(),
        }
    }

    /// Add custom metadata.
    pub fn with_custom(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.custom.insert(key.into(), value.into());
        self
    }
}

/// Checkpoint data to be persisted.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CheckpointData {
    /// Metadata about this checkpoint.
    pub metadata: CheckpointMetadata,
    /// Serialized execution state (implementation-specific).
    pub state: Vec<u8>,
    /// Environment variables at checkpoint time.
    pub env_vars: HashMap<String, String>,
    /// Working directory contents (optional, for file-based state).
    pub files: HashMap<String, Vec<u8>>,
}

impl CheckpointData {
    /// Create a new checkpoint.
    pub fn new(metadata: CheckpointMetadata, state: Vec<u8>) -> Self {
        Self {
            metadata,
            state,
            env_vars: HashMap::new(),
            files: HashMap::new(),
        }
    }

    /// Add environment variables.
    pub fn with_env(mut self, env: HashMap<String, String>) -> Self {
        self.env_vars = env;
        self
    }

    /// Add a file to the checkpoint.
    pub fn with_file(mut self, path: impl Into<String>, contents: Vec<u8>) -> Self {
        self.files.insert(path.into(), contents);
        self
    }
}

/// Trait for checkpoint storage backends.
#[async_trait::async_trait]
pub trait CheckpointStore: Send + Sync {
    /// Save a checkpoint.
    async fn save(&self, checkpoint: CheckpointData) -> anyhow::Result<String>;

    /// Load a checkpoint by ID.
    async fn load(&self, checkpoint_id: &str) -> anyhow::Result<CheckpointData>;

    /// List checkpoints for a job.
    async fn list(&self, job_id: &str) -> anyhow::Result<Vec<CheckpointMetadata>>;

    /// Delete a checkpoint.
    async fn delete(&self, checkpoint_id: &str) -> anyhow::Result<()>;

    /// Get the latest checkpoint for a job.
    async fn latest(&self, job_id: &str) -> anyhow::Result<Option<CheckpointMetadata>>;
}

/// Filesystem-based checkpoint store.
pub struct FilesystemCheckpointStore {
    /// Base directory for checkpoints.
    base_dir: PathBuf,
}

impl FilesystemCheckpointStore {
    /// Create a new filesystem checkpoint store.
    pub fn new(base_dir: impl AsRef<Path>) -> Self {
        Self {
            base_dir: base_dir.as_ref().to_path_buf(),
        }
    }

    /// Get the path for a checkpoint.
    fn checkpoint_path(&self, checkpoint_id: &str) -> PathBuf {
        self.base_dir.join(format!("{}.checkpoint", checkpoint_id))
    }

    /// Get the metadata path for a checkpoint.
    fn metadata_path(&self, checkpoint_id: &str) -> PathBuf {
        self.base_dir.join(format!("{}.meta.json", checkpoint_id))
    }
}

#[async_trait::async_trait]
impl CheckpointStore for FilesystemCheckpointStore {
    async fn save(&self, checkpoint: CheckpointData) -> anyhow::Result<String> {
        // Ensure directory exists
        tokio::fs::create_dir_all(&self.base_dir).await?;

        let checkpoint_id = checkpoint.metadata.id.clone();

        // Save metadata
        let metadata_json = serde_json::to_string_pretty(&checkpoint.metadata)?;
        tokio::fs::write(self.metadata_path(&checkpoint_id), metadata_json).await?;

        // Save checkpoint data
        let data = bincode::serialize(&checkpoint)?;
        tokio::fs::write(self.checkpoint_path(&checkpoint_id), data).await?;

        Ok(checkpoint_id)
    }

    async fn load(&self, checkpoint_id: &str) -> anyhow::Result<CheckpointData> {
        let data = tokio::fs::read(self.checkpoint_path(checkpoint_id)).await?;
        let checkpoint: CheckpointData = bincode::deserialize(&data)?;
        Ok(checkpoint)
    }

    async fn list(&self, job_id: &str) -> anyhow::Result<Vec<CheckpointMetadata>> {
        let mut checkpoints = Vec::new();

        let mut entries = tokio::fs::read_dir(&self.base_dir).await?;
        while let Some(entry) = entries.next_entry().await? {
            let path = entry.path();
            if path.extension().map_or(false, |e| e == "json") {
                let content = tokio::fs::read_to_string(&path).await?;
                if let Ok(metadata) = serde_json::from_str::<CheckpointMetadata>(&content) {
                    if metadata.job_id == job_id {
                        checkpoints.push(metadata);
                    }
                }
            }
        }

        // Sort by step (most recent first)
        checkpoints.sort_by(|a, b| b.step.cmp(&a.step));

        Ok(checkpoints)
    }

    async fn delete(&self, checkpoint_id: &str) -> anyhow::Result<()> {
        let _ = tokio::fs::remove_file(self.checkpoint_path(checkpoint_id)).await;
        let _ = tokio::fs::remove_file(self.metadata_path(checkpoint_id)).await;
        Ok(())
    }

    async fn latest(&self, job_id: &str) -> anyhow::Result<Option<CheckpointMetadata>> {
        let checkpoints = self.list(job_id).await?;
        Ok(checkpoints.into_iter().next())
    }
}

/// Manager for checkpoint operations.
pub struct CheckpointManager {
    store: Box<dyn CheckpointStore>,
}

impl CheckpointManager {
    /// Create a new checkpoint manager with the given store.
    pub fn new(store: impl CheckpointStore + 'static) -> Self {
        Self {
            store: Box::new(store),
        }
    }

    /// Create a checkpoint manager with filesystem storage.
    pub fn with_filesystem(base_dir: impl AsRef<Path>) -> Self {
        Self::new(FilesystemCheckpointStore::new(base_dir))
    }

    /// Create a checkpoint.
    pub async fn checkpoint(
        &self,
        job_id: &str,
        name: &str,
        step: u32,
        state: Vec<u8>,
    ) -> anyhow::Result<String> {
        let metadata = CheckpointMetadata::new(job_id, name, step);
        let checkpoint = CheckpointData::new(metadata, state);
        self.store.save(checkpoint).await
    }

    /// Restore from the latest checkpoint.
    pub async fn restore(&self, job_id: &str) -> anyhow::Result<Option<CheckpointData>> {
        if let Some(metadata) = self.store.latest(job_id).await? {
            let checkpoint = self.store.load(&metadata.id).await?;
            Ok(Some(checkpoint))
        } else {
            Ok(None)
        }
    }

    /// Restore from a specific checkpoint.
    pub async fn restore_from(&self, checkpoint_id: &str) -> anyhow::Result<CheckpointData> {
        self.store.load(checkpoint_id).await
    }

    /// List checkpoints for a job.
    pub async fn list(&self, job_id: &str) -> anyhow::Result<Vec<CheckpointMetadata>> {
        self.store.list(job_id).await
    }

    /// Clean up old checkpoints, keeping only the N most recent.
    pub async fn cleanup(&self, job_id: &str, keep: usize) -> anyhow::Result<usize> {
        let checkpoints = self.store.list(job_id).await?;
        let mut deleted = 0;

        for checkpoint in checkpoints.into_iter().skip(keep) {
            self.store.delete(&checkpoint.id).await?;
            deleted += 1;
        }

        Ok(deleted)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;

    #[tokio::test]
    async fn test_checkpoint_save_load() {
        let temp_dir = TempDir::new().unwrap();
        let manager = CheckpointManager::with_filesystem(temp_dir.path());

        // Create a checkpoint
        let checkpoint_id = manager
            .checkpoint("test-job", "step-1", 1, b"test state".to_vec())
            .await
            .unwrap();

        // Restore it
        let restored = manager.restore("test-job").await.unwrap().unwrap();
        assert_eq!(restored.state, b"test state".to_vec());
        assert_eq!(restored.metadata.step, 1);
    }

    #[tokio::test]
    async fn test_checkpoint_cleanup() {
        let temp_dir = TempDir::new().unwrap();
        let manager = CheckpointManager::with_filesystem(temp_dir.path());

        // Create multiple checkpoints
        for i in 1..=5 {
            manager
                .checkpoint("test-job", &format!("step-{}", i), i, vec![i as u8])
                .await
                .unwrap();
        }

        // Cleanup, keeping only 2
        let deleted = manager.cleanup("test-job", 2).await.unwrap();
        assert_eq!(deleted, 3);

        // Verify only 2 remain
        let remaining = manager.list("test-job").await.unwrap();
        assert_eq!(remaining.len(), 2);
    }
}
