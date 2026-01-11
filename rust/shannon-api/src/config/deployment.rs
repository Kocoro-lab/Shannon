//! Deployment configuration for Shannon API.
//!
//! This module provides configuration for different deployment modes:
//! - Embedded: Self-contained Tauri desktop/mobile with Durable + SurrealDB
//! - Cloud: Multi-tenant with Temporal + PostgreSQL
//! - Hybrid: Local-first with optional cloud sync
//! - Mesh: P2P sync between devices
//! - MeshCloud: P2P sync with cloud relay

use serde::{Deserialize, Serialize};
use std::path::PathBuf;

/// Deployment configuration for the Shannon platform.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DeploymentConfig {
    /// The deployment mode.
    #[serde(default)]
    pub mode: DeploymentMode,
    /// Workflow engine configuration.
    #[serde(default)]
    pub workflow: WorkflowConfig,
    /// Database configuration.
    #[serde(default)]
    pub database: DeploymentDatabaseConfig,
    /// Sync configuration.
    #[serde(default)]
    pub sync: SyncConfig,
}

impl Default for DeploymentConfig {
    fn default() -> Self {
        Self {
            mode: DeploymentMode::default(),
            workflow: WorkflowConfig::default(),
            database: DeploymentDatabaseConfig::default(),
            sync: SyncConfig::default(),
        }
    }
}

impl DeploymentConfig {
    /// Load deployment configuration from environment variables.
    pub fn from_env() -> Self {
        let mode = std::env::var("SHANNON_MODE")
            .ok()
            .and_then(|s| s.parse().ok())
            .unwrap_or_default();

        let workflow = WorkflowConfig::from_env();
        let database = DeploymentDatabaseConfig::from_env();
        let sync = SyncConfig::from_env();

        Self {
            mode,
            workflow,
            database,
            sync,
        }
    }

    /// Check if running in embedded mode.
    #[must_use]
    pub fn is_embedded(&self) -> bool {
        matches!(self.mode, DeploymentMode::Embedded)
    }

    /// Check if running in cloud mode.
    #[must_use]
    pub fn is_cloud(&self) -> bool {
        matches!(self.mode, DeploymentMode::Cloud)
    }

    /// Check if sync is enabled.
    #[must_use]
    pub fn is_sync_enabled(&self) -> bool {
        !matches!(self.sync, SyncConfig::Disabled)
    }
}

/// Deployment mode determines the runtime behavior.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum DeploymentMode {
    /// Self-contained desktop/mobile app with Durable + SurrealDB.
    #[default]
    Embedded,
    /// Multi-tenant cloud deployment with Temporal + PostgreSQL.
    Cloud,
    /// Local-first with optional cloud sync.
    Hybrid,
    /// P2P sync between devices without cloud.
    Mesh,
    /// P2P sync with cloud relay/backup.
    MeshCloud,
}

impl std::str::FromStr for DeploymentMode {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "embedded" => Ok(Self::Embedded),
            "cloud" => Ok(Self::Cloud),
            "hybrid" => Ok(Self::Hybrid),
            "mesh" => Ok(Self::Mesh),
            "mesh-cloud" | "meshcloud" | "mesh_cloud" => Ok(Self::MeshCloud),
            _ => Err(format!("Unknown deployment mode: {s}")),
        }
    }
}

impl std::fmt::Display for DeploymentMode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Embedded => write!(f, "embedded"),
            Self::Cloud => write!(f, "cloud"),
            Self::Hybrid => write!(f, "hybrid"),
            Self::Mesh => write!(f, "mesh"),
            Self::MeshCloud => write!(f, "mesh-cloud"),
        }
    }
}

/// Workflow engine configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "engine", rename_all = "lowercase")]
pub enum WorkflowConfig {
    /// Durable workflow engine (embedded/mobile).
    Durable {
        /// Directory containing WASM workflow patterns.
        #[serde(default = "default_wasm_dir")]
        wasm_dir: PathBuf,
        /// Maximum concurrent workflows.
        #[serde(default = "default_max_concurrent")]
        max_concurrent: usize,
        /// Event log retention in days.
        #[serde(default = "default_event_retention")]
        event_log_retention_days: u32,
    },
    /// Temporal workflow engine (cloud).
    Temporal {
        /// Temporal server endpoint.
        #[serde(default = "default_temporal_endpoint")]
        endpoint: String,
        /// Temporal namespace.
        #[serde(default = "default_temporal_namespace")]
        namespace: String,
        /// Task queue name.
        #[serde(default = "default_task_queue")]
        task_queue: String,
    },
}

fn default_wasm_dir() -> PathBuf {
    PathBuf::from("./wasm/patterns")
}

fn default_max_concurrent() -> usize {
    10
}

fn default_event_retention() -> u32 {
    30
}

fn default_temporal_endpoint() -> String {
    "temporal:7233".to_string()
}

fn default_temporal_namespace() -> String {
    "shannon".to_string()
}

fn default_task_queue() -> String {
    "research-tasks".to_string()
}

impl Default for WorkflowConfig {
    fn default() -> Self {
        Self::Durable {
            wasm_dir: default_wasm_dir(),
            max_concurrent: default_max_concurrent(),
            event_log_retention_days: default_event_retention(),
        }
    }
}

impl WorkflowConfig {
    /// Load workflow configuration from environment variables.
    pub fn from_env() -> Self {
        let engine = std::env::var("WORKFLOW_ENGINE").unwrap_or_else(|_| "durable".to_string());

        match engine.to_lowercase().as_str() {
            "temporal" => Self::Temporal {
                endpoint: std::env::var("TEMPORAL_ENDPOINT")
                    .unwrap_or_else(|_| default_temporal_endpoint()),
                namespace: std::env::var("TEMPORAL_NAMESPACE")
                    .unwrap_or_else(|_| default_temporal_namespace()),
                task_queue: std::env::var("TEMPORAL_TASK_QUEUE")
                    .unwrap_or_else(|_| default_task_queue()),
            },
            _ => Self::Durable {
                wasm_dir: std::env::var("DURABLE_WASM_DIR")
                    .map(PathBuf::from)
                    .unwrap_or_else(|_| default_wasm_dir()),
                max_concurrent: std::env::var("DURABLE_MAX_CONCURRENT")
                    .ok()
                    .and_then(|s| s.parse().ok())
                    .unwrap_or_else(default_max_concurrent),
                event_log_retention_days: std::env::var("DURABLE_EVENT_RETENTION_DAYS")
                    .ok()
                    .and_then(|s| s.parse().ok())
                    .unwrap_or_else(default_event_retention),
            },
        }
    }

    /// Check if using Durable engine.
    #[must_use]
    pub fn is_durable(&self) -> bool {
        matches!(self, Self::Durable { .. })
    }

    /// Check if using Temporal engine.
    #[must_use]
    pub fn is_temporal(&self) -> bool {
        matches!(self, Self::Temporal { .. })
    }

    /// Returns the workflow engine name.
    #[must_use]
    pub fn engine_name(&self) -> &'static str {
        match self {
            Self::Durable { .. } => "durable",
            Self::Temporal { .. } => "temporal",
        }
    }
}

/// Database configuration for deployment.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "driver", rename_all = "lowercase")]
pub enum DeploymentDatabaseConfig {
    /// SurrealDB for embedded/desktop deployments.
    /// Embedded database (SQLite + USearch) for desktop.
    Embedded {
        /// Path to the database file.
        #[serde(default = "default_embedded_path")]
        path: PathBuf,
    },
    /// PostgreSQL for cloud deployments.
    PostgreSQL {
        /// Connection URL.
        url: String,
        /// Maximum connections.
        #[serde(default = "default_pg_max_connections")]
        max_connections: u32,
    },
    /// SQLite for mobile deployments.
    SQLite {
        /// Path to the database file.
        #[serde(default = "default_sqlite_path")]
        path: PathBuf,
    },
}

fn default_embedded_path() -> PathBuf {
    PathBuf::from("./data/shannon.sqlite")
}

fn default_pg_max_connections() -> u32 {
    20
}

fn default_sqlite_path() -> PathBuf {
    PathBuf::from("./data/shannon.sqlite")
}

impl Default for DeploymentDatabaseConfig {
    fn default() -> Self {
        Self::Embedded {
            path: default_embedded_path(),
        }
    }
}

impl DeploymentDatabaseConfig {
    /// Load database configuration from environment variables.
    pub fn from_env() -> Self {
        let driver = std::env::var("DATABASE_DRIVER").unwrap_or_else(|_| "embedded".to_string());

        match driver.to_lowercase().as_str() {
            "postgresql" | "postgres" => {
                let url = std::env::var("DATABASE_URL")
                    .unwrap_or_else(|_| "postgres://localhost:5432/shannon".to_string());
                let max_connections = std::env::var("DATABASE_MAX_CONNECTIONS")
                    .ok()
                    .and_then(|s| s.parse().ok())
                    .unwrap_or_else(default_pg_max_connections);
                Self::PostgreSQL {
                    url,
                    max_connections,
                }
            }
            "sqlite" => {
                let path = std::env::var("SQLITE_PATH")
                    .map(PathBuf::from)
                    .unwrap_or_else(|_| default_sqlite_path());
                Self::SQLite { path }
            }
            _ => {
                let path = std::env::var("SHANNON_DB_PATH")
                    .map(PathBuf::from)
                    .unwrap_or_else(|_| default_embedded_path());
                Self::Embedded { path }
            }
        }
    }
}

/// Sync configuration for P2P and cloud sync.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "mode", rename_all = "lowercase")]
pub enum SyncConfig {
    /// Sync is disabled.
    Disabled,
    /// P2P mesh sync via WebRTC.
    Mesh {
        /// Device identity (generated on first run if not set).
        device_id: Option<String>,
        /// Signaling server for peer discovery.
        #[serde(default = "default_signaling_server")]
        signaling_server: String,
        /// ICE servers for NAT traversal.
        #[serde(default = "default_ice_servers")]
        ice_servers: Vec<String>,
        /// TURN relay server (optional).
        turn_server: Option<TurnServer>,
        /// What data to sync.
        #[serde(default)]
        scope: SyncScope,
    },
    /// P2P mesh with cloud backup.
    MeshCloud {
        /// Device identity.
        device_id: Option<String>,
        /// Signaling server.
        #[serde(default = "default_signaling_server")]
        signaling_server: String,
        /// ICE servers.
        #[serde(default = "default_ice_servers")]
        ice_servers: Vec<String>,
        /// TURN server.
        turn_server: Option<TurnServer>,
        /// Sync scope.
        #[serde(default)]
        scope: SyncScope,
        /// Cloud backup endpoint.
        cloud_endpoint: String,
        /// Cloud API key.
        cloud_api_key: String,
        /// Backup interval in seconds.
        #[serde(default = "default_backup_interval")]
        backup_interval_secs: u64,
    },
    /// Cloud-only sync (no P2P).
    CloudOnly {
        /// Cloud sync endpoint.
        api_endpoint: String,
        /// API key.
        api_key: String,
        /// Sync interval in seconds.
        #[serde(default = "default_sync_interval")]
        sync_interval_secs: u64,
    },
}

fn default_signaling_server() -> String {
    "wss://signal.shannon.io".to_string()
}

fn default_ice_servers() -> Vec<String> {
    vec!["stun:stun.l.google.com:19302".to_string()]
}

fn default_backup_interval() -> u64 {
    300 // 5 minutes
}

fn default_sync_interval() -> u64 {
    30
}

impl Default for SyncConfig {
    fn default() -> Self {
        Self::Disabled
    }
}

impl SyncConfig {
    /// Load sync configuration from environment variables.
    pub fn from_env() -> Self {
        let mode = std::env::var("SYNC_MODE").unwrap_or_else(|_| "disabled".to_string());

        match mode.to_lowercase().as_str() {
            "mesh" => Self::Mesh {
                device_id: std::env::var("SYNC_DEVICE_ID").ok(),
                signaling_server: std::env::var("SYNC_SIGNALING_SERVER")
                    .unwrap_or_else(|_| default_signaling_server()),
                ice_servers: std::env::var("SYNC_ICE_SERVERS")
                    .map(|s| s.split(',').map(String::from).collect())
                    .unwrap_or_else(|_| default_ice_servers()),
                turn_server: TurnServer::from_env(),
                scope: SyncScope::from_env(),
            },
            "mesh-cloud" | "meshcloud" | "mesh_cloud" => Self::MeshCloud {
                device_id: std::env::var("SYNC_DEVICE_ID").ok(),
                signaling_server: std::env::var("SYNC_SIGNALING_SERVER")
                    .unwrap_or_else(|_| default_signaling_server()),
                ice_servers: std::env::var("SYNC_ICE_SERVERS")
                    .map(|s| s.split(',').map(String::from).collect())
                    .unwrap_or_else(|_| default_ice_servers()),
                turn_server: TurnServer::from_env(),
                scope: SyncScope::from_env(),
                cloud_endpoint: std::env::var("SYNC_CLOUD_ENDPOINT")
                    .unwrap_or_else(|_| "https://api.shannon.io/sync".to_string()),
                cloud_api_key: std::env::var("SYNC_CLOUD_API_KEY").unwrap_or_default(),
                backup_interval_secs: std::env::var("SYNC_CLOUD_BACKUP_INTERVAL_SECS")
                    .ok()
                    .and_then(|s| s.parse().ok())
                    .unwrap_or_else(default_backup_interval),
            },
            "cloud-only" | "cloudonly" | "cloud_only" => Self::CloudOnly {
                api_endpoint: std::env::var("SYNC_CLOUD_ENDPOINT")
                    .unwrap_or_else(|_| "https://api.shannon.io/sync".to_string()),
                api_key: std::env::var("SYNC_CLOUD_API_KEY").unwrap_or_default(),
                sync_interval_secs: std::env::var("SYNC_INTERVAL_SECS")
                    .ok()
                    .and_then(|s| s.parse().ok())
                    .unwrap_or_else(default_sync_interval),
            },
            _ => Self::Disabled,
        }
    }
}

/// TURN server configuration for NAT traversal fallback.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TurnServer {
    /// TURN server URL.
    pub url: String,
    /// Username for authentication.
    pub username: Option<String>,
    /// Credential for authentication.
    pub credential: Option<String>,
}

impl TurnServer {
    /// Load TURN server from environment variables.
    fn from_env() -> Option<Self> {
        let url = std::env::var("SYNC_TURN_SERVER").ok()?;
        Some(Self {
            url,
            username: std::env::var("SYNC_TURN_USERNAME").ok(),
            credential: std::env::var("SYNC_TURN_CREDENTIAL").ok(),
        })
    }
}

/// What data to sync between devices.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct SyncScope {
    /// Sync research runs.
    #[serde(default = "default_true")]
    pub runs: bool,
    /// Sync conversation memories.
    #[serde(default = "default_true")]
    pub memories: bool,
    /// Sync user settings.
    #[serde(default = "default_true")]
    pub settings: bool,
    /// Sync workflow definitions.
    #[serde(default)]
    pub workflows: bool,
}

fn default_true() -> bool {
    true
}

impl SyncScope {
    /// Load sync scope from environment variables.
    fn from_env() -> Self {
        Self {
            runs: std::env::var("SYNC_SCOPE_RUNS")
                .map(|s| s.to_lowercase() == "true")
                .unwrap_or(true),
            memories: std::env::var("SYNC_SCOPE_MEMORIES")
                .map(|s| s.to_lowercase() == "true")
                .unwrap_or(true),
            settings: std::env::var("SYNC_SCOPE_SETTINGS")
                .map(|s| s.to_lowercase() == "true")
                .unwrap_or(true),
            workflows: std::env::var("SYNC_SCOPE_WORKFLOWS")
                .map(|s| s.to_lowercase() == "true")
                .unwrap_or(false),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_deployment_mode_parsing() {
        assert_eq!(
            "embedded".parse::<DeploymentMode>().unwrap(),
            DeploymentMode::Embedded
        );
        assert_eq!(
            "cloud".parse::<DeploymentMode>().unwrap(),
            DeploymentMode::Cloud
        );
        assert_eq!(
            "hybrid".parse::<DeploymentMode>().unwrap(),
            DeploymentMode::Hybrid
        );
        assert_eq!(
            "mesh".parse::<DeploymentMode>().unwrap(),
            DeploymentMode::Mesh
        );
        assert_eq!(
            "mesh-cloud".parse::<DeploymentMode>().unwrap(),
            DeploymentMode::MeshCloud
        );
    }

    #[test]
    fn test_default_config() {
        let config = DeploymentConfig::default();
        assert!(config.is_embedded());
        assert!(!config.is_cloud());
        assert!(!config.is_sync_enabled());
    }
}
