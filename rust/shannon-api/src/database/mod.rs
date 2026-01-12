//! Database abstraction layer.
//!
//! This module provides a unified interface for database operations that
//! abstracts over different backend implementations:
//! - **SurrealDB**: Multi-model database for embedded desktop mode
//! - **PostgreSQL**: Relational database for cloud mode
//! - **SQLite**: Lightweight database for mobile mode
//!
//! The abstraction allows Shannon to run in both embedded (Tauri) and cloud
//! (Docker/K8s) environments with the same application logic.

pub mod encryption;
pub mod hybrid;
pub mod repository;
pub mod schema;
pub mod settings;
pub mod workflow_store;

pub use encryption::KeyManager;
pub use hybrid::ControlState;
pub use repository::{Database, MemoryRepository, RunRepository};
pub use settings::{ApiKey, ApiKeyInfo, ApiKeyRepository, SettingsRepository, UserSetting};
pub use workflow_store::{WorkflowCheckpoint, WorkflowMetadata, WorkflowStatus, WorkflowStore};

use crate::config::deployment::DeploymentDatabaseConfig;

/// Create a database instance from configuration.
///
/// # Errors
///
/// Returns an error if the database connection cannot be established.
pub async fn create_database(config: &DeploymentDatabaseConfig) -> anyhow::Result<Database> {
    Database::from_config(config).await
}
