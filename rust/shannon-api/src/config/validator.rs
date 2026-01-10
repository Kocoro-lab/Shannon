//! Configuration validation for Shannon API.
//!
//! This module validates configuration combinations at startup,
//! ensuring incompatible settings are rejected with helpful error messages.

use super::deployment::{DeploymentConfig, DeploymentDatabaseConfig, DeploymentMode, SyncConfig, WorkflowConfig};
use super::error::{ConfigResult, ConfigurationError};
use super::AppConfig;

/// Configuration validator that checks for valid configuration combinations.
///
/// The validator enforces the following rules:
///
/// | Mode     | Workflow Engine | Database   | Valid |
/// |----------|----------------|------------|-------|
/// | embedded | durable        | surrealdb  | YES   |
/// | embedded | durable        | sqlite     | YES   |
/// | embedded | durable        | postgresql | NO    |
/// | embedded | temporal       | any        | NO    |
/// | cloud    | temporal       | postgresql | YES   |
/// | cloud    | temporal       | surrealdb  | NO    |
/// | cloud    | durable        | any        | NO    |
/// | hybrid   | durable        | surrealdb  | YES   |
/// | hybrid   | temporal       | any        | NO    |
/// | mesh     | durable        | surrealdb  | YES   |
/// | mesh-cloud | durable      | surrealdb  | YES   |
#[derive(Debug)]
pub struct ConfigValidator;

impl ConfigValidator {
    /// Validate the entire application configuration.
    ///
    /// Returns `Ok(())` if valid, or a `ConfigurationError` with all issues.
    pub fn validate(config: &AppConfig) -> ConfigResult<()> {
        let mut errors = Vec::new();

        // Validate deployment config combinations
        if let Err(e) = Self::validate_deployment(&config.deployment) {
            match e {
                ConfigurationError::Multiple(errs) => errors.extend(errs),
                e => errors.push(e),
            }
        }

        // Validate LLM configuration
        if let Err(e) = Self::validate_llm_config(config) {
            errors.push(e);
        }

        // Validate infrastructure requirements for cloud mode
        if config.deployment.is_cloud() {
            if let Err(e) = Self::validate_cloud_infrastructure(config) {
                match e {
                    ConfigurationError::Multiple(errs) => errors.extend(errs),
                    e => errors.push(e),
                }
            }
        }

        if errors.is_empty() {
            Ok(())
        } else if errors.len() == 1 {
            Err(errors.remove(0))
        } else {
            Err(ConfigurationError::multiple(errors))
        }
    }

    /// Validate the deployment configuration.
    pub fn validate_deployment(config: &DeploymentConfig) -> ConfigResult<()> {
        let mut errors = Vec::new();

        // Validate mode + workflow combination
        if let Err(e) = Self::validate_mode_workflow_combination(config.mode, &config.workflow) {
            errors.push(e);
        }

        // Validate mode + database combination
        if let Err(e) = Self::validate_mode_database_combination(config.mode, &config.database) {
            errors.push(e);
        }

        // Validate sync requirements
        if let Err(e) = Self::validate_sync_requirements(config.mode, &config.sync) {
            errors.push(e);
        }

        if errors.is_empty() {
            Ok(())
        } else if errors.len() == 1 {
            Err(errors.remove(0))
        } else {
            Err(ConfigurationError::multiple(errors))
        }
    }

    /// Validate mode and workflow engine combination.
    pub fn validate_mode_workflow_combination(
        mode: DeploymentMode,
        workflow: &WorkflowConfig,
    ) -> ConfigResult<()> {
        match (mode, workflow) {
            // Embedded mode MUST use Durable
            (DeploymentMode::Embedded, WorkflowConfig::Temporal { .. }) => {
                Err(ConfigurationError::incompatible(
                    "SHANNON_MODE=embedded",
                    "WORKFLOW_ENGINE=temporal",
                    "Embedded mode cannot use Temporal workflow engine because it requires \
                    external infrastructure. Set WORKFLOW_ENGINE=durable or switch to \
                    SHANNON_MODE=cloud for multi-tenant deployment.",
                ))
            }

            // Cloud mode MUST use Temporal
            (DeploymentMode::Cloud, WorkflowConfig::Durable { .. }) => {
                Err(ConfigurationError::incompatible(
                    "SHANNON_MODE=cloud",
                    "WORKFLOW_ENGINE=durable",
                    "Cloud mode requires Temporal for distributed workflow coordination. \
                    Set WORKFLOW_ENGINE=temporal and configure TEMPORAL_ENDPOINT, or use \
                    SHANNON_MODE=embedded for local-only operation.",
                ))
            }

            // Hybrid mode uses Durable for local-first operation
            (DeploymentMode::Hybrid, WorkflowConfig::Temporal { .. }) => {
                Err(ConfigurationError::incompatible(
                    "SHANNON_MODE=hybrid",
                    "WORKFLOW_ENGINE=temporal",
                    "Hybrid mode is local-first and requires the Durable workflow engine. \
                    Set WORKFLOW_ENGINE=durable. Temporal is only used in cloud mode.",
                ))
            }

            // Mesh modes use Durable
            (DeploymentMode::Mesh, WorkflowConfig::Temporal { .. }) |
            (DeploymentMode::MeshCloud, WorkflowConfig::Temporal { .. }) => {
                Err(ConfigurationError::incompatible(
                    format!("SHANNON_MODE={}", mode),
                    "WORKFLOW_ENGINE=temporal",
                    "Mesh sync modes are local-first and require the Durable workflow engine. \
                    Set WORKFLOW_ENGINE=durable.",
                ))
            }

            // All other combinations are valid
            _ => Ok(()),
        }
    }

    /// Validate mode and database combination.
    pub fn validate_mode_database_combination(
        mode: DeploymentMode,
        database: &DeploymentDatabaseConfig,
    ) -> ConfigResult<()> {
        match (mode, database) {
            // Embedded mode cannot use PostgreSQL
            (DeploymentMode::Embedded, DeploymentDatabaseConfig::PostgreSQL { .. }) => {
                Err(ConfigurationError::incompatible(
                    "SHANNON_MODE=embedded",
                    "DATABASE_DRIVER=postgresql",
                    "Embedded mode with PostgreSQL requires cloud infrastructure. \
                    Use DATABASE_DRIVER=surrealdb for local embedded storage, or switch to \
                    SHANNON_MODE=cloud for full cloud deployment.",
                ))
            }

            // Cloud mode MUST use PostgreSQL
            (DeploymentMode::Cloud, DeploymentDatabaseConfig::SurrealDB { .. }) => {
                Err(ConfigurationError::incompatible(
                    "SHANNON_MODE=cloud",
                    "DATABASE_DRIVER=surrealdb",
                    "Cloud mode requires PostgreSQL for production-grade storage. \
                    Set DATABASE_DRIVER=postgresql and provide DATABASE_URL.",
                ))
            }

            (DeploymentMode::Cloud, DeploymentDatabaseConfig::SQLite { .. }) => {
                Err(ConfigurationError::incompatible(
                    "SHANNON_MODE=cloud",
                    "DATABASE_DRIVER=sqlite",
                    "Cloud mode requires PostgreSQL for production-grade storage. \
                    SQLite is only suitable for mobile embedded mode. \
                    Set DATABASE_DRIVER=postgresql and provide DATABASE_URL.",
                ))
            }

            // Hybrid/Mesh modes use local databases
            (DeploymentMode::Hybrid | DeploymentMode::Mesh | DeploymentMode::MeshCloud, 
             DeploymentDatabaseConfig::PostgreSQL { .. }) => {
                Err(ConfigurationError::incompatible(
                    format!("SHANNON_MODE={}", mode),
                    "DATABASE_DRIVER=postgresql",
                    "Local-first modes (hybrid, mesh) require local embedded storage. \
                    Use DATABASE_DRIVER=surrealdb or DATABASE_DRIVER=sqlite.",
                ))
            }

            // All other combinations are valid
            _ => Ok(()),
        }
    }

    /// Validate sync configuration requirements.
    pub fn validate_sync_requirements(
        mode: DeploymentMode,
        sync: &SyncConfig,
    ) -> ConfigResult<()> {
        match (mode, sync) {
            // Cloud mode shouldn't have P2P sync enabled
            (DeploymentMode::Cloud, SyncConfig::Mesh { .. }) |
            (DeploymentMode::Cloud, SyncConfig::MeshCloud { .. }) => {
                Err(ConfigurationError::incompatible(
                    "SHANNON_MODE=cloud",
                    "SYNC_MODE=mesh",
                    "Cloud mode is centralized and doesn't support P2P mesh sync. \
                    P2P sync is for local-first modes (embedded, hybrid, mesh).",
                ))
            }

            // Mesh mode requires sync configuration
            (DeploymentMode::Mesh, SyncConfig::Disabled) => {
                Err(ConfigurationError::missing_required(
                    "Sync configuration",
                    "Mesh mode requires P2P sync to function",
                    "SYNC_MODE=mesh with SYNC_SIGNALING_SERVER configured",
                ))
            }

            // MeshCloud mode requires cloud backup config
            (DeploymentMode::MeshCloud, SyncConfig::Mesh { .. }) => {
                Err(ConfigurationError::missing_required(
                    "Cloud backup configuration",
                    "MeshCloud mode requires cloud backup endpoint",
                    "SYNC_MODE=mesh-cloud with SYNC_CLOUD_ENDPOINT and SYNC_CLOUD_API_KEY",
                ))
            }

            // All other combinations are valid
            _ => Ok(()),
        }
    }

    /// Validate LLM configuration.
    pub fn validate_llm_config(config: &AppConfig) -> ConfigResult<()> {
        let has_anthropic = config.providers.anthropic.api_key.as_ref()
            .map(|k| !k.is_empty())
            .unwrap_or(false);
        let has_openai = config.providers.openai.api_key.as_ref()
            .map(|k| !k.is_empty())
            .unwrap_or(false);
        let has_google = config.providers.google.api_key.as_ref()
            .map(|k| !k.is_empty())
            .unwrap_or(false);
        let has_groq = config.providers.groq.api_key.as_ref()
            .map(|k| !k.is_empty())
            .unwrap_or(false);
        let has_xai = config.providers.xai.api_key.as_ref()
            .map(|k| !k.is_empty())
            .unwrap_or(false);

        if !has_anthropic && !has_openai && !has_google && !has_groq && !has_xai {
            return Err(ConfigurationError::missing_required(
                "LLM API Key",
                "Making LLM requests for research and conversation",
                "Set at least one of: ANTHROPIC_API_KEY, OPENAI_API_KEY, GOOGLE_API_KEY, \
                GROQ_API_KEY, or XAI_API_KEY",
            ));
        }

        Ok(())
    }

    /// Validate cloud infrastructure requirements.
    pub fn validate_cloud_infrastructure(config: &AppConfig) -> ConfigResult<()> {
        let mut errors = Vec::new();

        // Check Temporal endpoint
        if matches!(config.deployment.workflow, WorkflowConfig::Temporal { .. }) {
            // Temporal endpoint is set with defaults, so just check if it's reachable
            // This is done at runtime, not here
        }

        // Check database URL for PostgreSQL
        if let DeploymentDatabaseConfig::PostgreSQL { url, .. } = &config.deployment.database {
            if url.is_empty() || url == "postgres://localhost:5432/shannon" {
                // Check if DATABASE_URL env var is set
                if std::env::var("DATABASE_URL").is_err() {
                    errors.push(ConfigurationError::missing_required(
                        "PostgreSQL connection URL",
                        "Cloud mode database access",
                        "DATABASE_URL (e.g., postgres://user:pass@host:5432/dbname)",
                    ));
                }
            }
        }

        // Check Redis URL for cloud mode (optional but recommended)
        if config.redis.url.is_none() {
            tracing::warn!(
                "Redis not configured for cloud mode. \
                Rate limiting and session management will use in-memory fallback."
            );
        }

        if errors.is_empty() {
            Ok(())
        } else if errors.len() == 1 {
            Err(errors.remove(0))
        } else {
            Err(ConfigurationError::multiple(errors))
        }
    }

    /// Validate feature flags at compile time.
    ///
    /// This checks that the required features are enabled for the current configuration.
    #[cfg(feature = "grpc")]
    pub fn validate_feature_flags_for_temporal() -> ConfigResult<()> {
        Ok(())
    }

    #[cfg(not(feature = "grpc"))]
    pub fn validate_feature_flags_for_temporal() -> ConfigResult<()> {
        Err(ConfigurationError::feature_unavailable(
            "Temporal workflow engine",
            "The 'grpc' feature is not enabled in this build",
            "Use WORKFLOW_ENGINE=durable for embedded mode, or rebuild with \
            --features grpc for cloud mode with Temporal",
        ))
    }

    /// Validate that the build has the right features for embedded mode.
    #[cfg(feature = "embedded")]
    pub fn validate_feature_flags_for_embedded() -> ConfigResult<()> {
        Ok(())
    }

    #[cfg(not(feature = "embedded"))]
    pub fn validate_feature_flags_for_embedded() -> ConfigResult<()> {
        Err(ConfigurationError::feature_unavailable(
            "Embedded mode (SurrealDB + Durable)",
            "The 'embedded' feature is not enabled in this build",
            "Rebuild with --features embedded for local mode, or use \
            SHANNON_MODE=cloud with --features grpc",
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn create_test_config(
        mode: DeploymentMode,
        workflow: WorkflowConfig,
        database: DeploymentDatabaseConfig,
    ) -> AppConfig {
        let mut config = AppConfig::default();
        config.deployment.mode = mode;
        config.deployment.workflow = workflow;
        config.deployment.database = database;
        // Add a dummy API key for LLM validation
        config.providers.anthropic.api_key = Some("test-key".to_string());
        config
    }

    // ===== VALID COMBINATIONS =====

    #[test]
    fn test_embedded_durable_surrealdb_valid() {
        let config = create_test_config(
            DeploymentMode::Embedded,
            WorkflowConfig::default(), // Durable
            DeploymentDatabaseConfig::default(), // SurrealDB
        );
        assert!(ConfigValidator::validate(&config).is_ok());
    }

    #[test]
    fn test_embedded_durable_sqlite_valid() {
        let config = create_test_config(
            DeploymentMode::Embedded,
            WorkflowConfig::default(),
            DeploymentDatabaseConfig::SQLite {
                path: "./data/test.sqlite".into(),
            },
        );
        assert!(ConfigValidator::validate(&config).is_ok());
    }

    #[test]
    fn test_cloud_temporal_postgresql_valid() {
        let config = create_test_config(
            DeploymentMode::Cloud,
            WorkflowConfig::Temporal {
                endpoint: "temporal:7233".to_string(),
                namespace: "test".to_string(),
                task_queue: "test-queue".to_string(),
            },
            DeploymentDatabaseConfig::PostgreSQL {
                url: "postgres://localhost/test".to_string(),
                max_connections: 10,
            },
        );
        assert!(ConfigValidator::validate(&config).is_ok());
    }

    #[test]
    fn test_hybrid_durable_surrealdb_valid() {
        let config = create_test_config(
            DeploymentMode::Hybrid,
            WorkflowConfig::default(),
            DeploymentDatabaseConfig::default(),
        );
        assert!(ConfigValidator::validate(&config).is_ok());
    }

    #[test]
    fn test_mesh_durable_surrealdb_valid() {
        let mut config = create_test_config(
            DeploymentMode::Mesh,
            WorkflowConfig::default(),
            DeploymentDatabaseConfig::default(),
        );
        // Mesh mode needs sync config
        config.deployment.sync = SyncConfig::Mesh {
            device_id: None,
            signaling_server: "wss://signal.test".to_string(),
            ice_servers: vec!["stun:stun.l.google.com:19302".to_string()],
            turn_server: None,
            scope: Default::default(),
        };
        assert!(ConfigValidator::validate(&config).is_ok());
    }

    // ===== INVALID COMBINATIONS =====

    #[test]
    fn test_embedded_temporal_invalid() {
        let config = create_test_config(
            DeploymentMode::Embedded,
            WorkflowConfig::Temporal {
                endpoint: "temporal:7233".to_string(),
                namespace: "test".to_string(),
                task_queue: "test-queue".to_string(),
            },
            DeploymentDatabaseConfig::default(),
        );
        let err = ConfigValidator::validate(&config).unwrap_err();
        let msg = err.to_string();
        assert!(msg.contains("embedded"));
        assert!(msg.contains("temporal"));
        assert!(msg.contains("WORKFLOW_ENGINE=durable") || msg.contains("SHANNON_MODE=cloud"));
    }

    #[test]
    fn test_cloud_durable_invalid() {
        let config = create_test_config(
            DeploymentMode::Cloud,
            WorkflowConfig::default(), // Durable
            DeploymentDatabaseConfig::PostgreSQL {
                url: "postgres://localhost/test".to_string(),
                max_connections: 10,
            },
        );
        let err = ConfigValidator::validate(&config).unwrap_err();
        let msg = err.to_string();
        assert!(msg.contains("cloud"));
        assert!(msg.contains("durable") || msg.contains("Durable"));
        assert!(msg.contains("Temporal"));
    }

    #[test]
    fn test_cloud_surrealdb_invalid() {
        let config = create_test_config(
            DeploymentMode::Cloud,
            WorkflowConfig::Temporal {
                endpoint: "temporal:7233".to_string(),
                namespace: "test".to_string(),
                task_queue: "test-queue".to_string(),
            },
            DeploymentDatabaseConfig::default(), // SurrealDB
        );
        let err = ConfigValidator::validate(&config).unwrap_err();
        let msg = err.to_string();
        assert!(msg.contains("cloud"));
        assert!(msg.contains("surrealdb") || msg.contains("SurrealDB"));
        assert!(msg.contains("PostgreSQL"));
    }

    #[test]
    fn test_embedded_postgresql_invalid() {
        let config = create_test_config(
            DeploymentMode::Embedded,
            WorkflowConfig::default(),
            DeploymentDatabaseConfig::PostgreSQL {
                url: "postgres://localhost/test".to_string(),
                max_connections: 10,
            },
        );
        let err = ConfigValidator::validate(&config).unwrap_err();
        let msg = err.to_string();
        assert!(msg.contains("embedded"));
        assert!(msg.contains("postgresql") || msg.contains("PostgreSQL"));
    }

    #[test]
    fn test_hybrid_temporal_invalid() {
        let config = create_test_config(
            DeploymentMode::Hybrid,
            WorkflowConfig::Temporal {
                endpoint: "temporal:7233".to_string(),
                namespace: "test".to_string(),
                task_queue: "test-queue".to_string(),
            },
            DeploymentDatabaseConfig::default(),
        );
        let err = ConfigValidator::validate(&config).unwrap_err();
        let msg = err.to_string();
        assert!(msg.contains("hybrid"));
        assert!(msg.contains("Durable"));
    }

    #[test]
    fn test_missing_api_key() {
        let mut config = create_test_config(
            DeploymentMode::Embedded,
            WorkflowConfig::default(),
            DeploymentDatabaseConfig::default(),
        );
        // Remove all API keys
        config.providers.anthropic.api_key = None;
        config.providers.openai.api_key = None;
        
        let err = ConfigValidator::validate(&config).unwrap_err();
        let msg = err.to_string();
        assert!(msg.contains("LLM API Key"));
        assert!(msg.contains("ANTHROPIC_API_KEY") || msg.contains("OPENAI_API_KEY"));
    }

    #[test]
    fn test_error_messages_contain_fix_hints() {
        // Test that all error messages contain actionable information
        let invalid_configs = vec![
            create_test_config(
                DeploymentMode::Embedded,
                WorkflowConfig::Temporal {
                    endpoint: "temporal:7233".to_string(),
                    namespace: "test".to_string(),
                    task_queue: "test-queue".to_string(),
                },
                DeploymentDatabaseConfig::default(),
            ),
            create_test_config(
                DeploymentMode::Cloud,
                WorkflowConfig::default(),
                DeploymentDatabaseConfig::PostgreSQL {
                    url: "postgres://localhost/test".to_string(),
                    max_connections: 10,
                },
            ),
        ];

        for config in invalid_configs {
            let err = ConfigValidator::validate(&config).unwrap_err();
            let msg = err.to_string();
            // Every error should have actionable content
            assert!(
                msg.contains("Set ") || 
                msg.contains("Use ") || 
                msg.contains("switch to") ||
                msg.contains("Rebuild"),
                "Error missing fix hint: {}", msg
            );
        }
    }

    #[test]
    fn test_mesh_mode_requires_sync_config() {
        let config = create_test_config(
            DeploymentMode::Mesh,
            WorkflowConfig::default(),
            DeploymentDatabaseConfig::default(),
        );
        // Sync is disabled by default
        let err = ConfigValidator::validate(&config).unwrap_err();
        let msg = err.to_string();
        assert!(msg.contains("Sync") || msg.contains("sync"));
    }
}
