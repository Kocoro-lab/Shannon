//! Configuration management for Shannon API.
//!
//! This module provides configuration loading from environment variables,
//! config files, and command-line arguments, with comprehensive validation.
//!
//! # Validation
//!
//! Use [`ConfigValidator`] to validate configuration combinations before startup:
//!
//! ```rust,ignore
//! use shannon_api::config::{AppConfig, ConfigValidator};
//!
//! let config = AppConfig::load()?;
//! ConfigValidator::validate(&config)?;
//! ```
//!
//! # Deployment Modes
//!
//! - **Embedded**: Self-contained desktop/mobile with Durable + SurrealDB
//! - **Cloud**: Multi-tenant with Temporal + PostgreSQL
//! - **Hybrid**: Local-first with optional cloud sync
//! - **Mesh**: P2P sync between devices
//! - **MeshCloud**: P2P sync with cloud backup

pub mod deployment;
pub mod error;
pub mod validator;

pub use deployment::{
    DeploymentConfig, DeploymentDatabaseConfig, DeploymentMode, SyncConfig, SyncScope,
    TurnServer, WorkflowConfig,
};
pub use error::{ConfigResult, ConfigurationError};
pub use validator::ConfigValidator;

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Main application configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AppConfig {
    /// Deployment configuration (mode, workflow engine, database driver).
    #[serde(default)]
    pub deployment: DeploymentConfig,
    /// Server configuration.
    #[serde(default)]
    pub server: ServerConfig,
    /// Gateway configuration (auth, rate limiting).
    #[serde(default)]
    pub gateway: GatewayConfig,
    /// Database configuration (legacy - use deployment.database).
    #[serde(default)]
    pub database: DatabaseConfig,
    /// Redis configuration.
    #[serde(default)]
    pub redis: RedisConfig,
    /// Orchestrator (gRPC) configuration.
    #[serde(default)]
    pub orchestrator: OrchestratorConfig,
    /// LLM provider configurations.
    #[serde(default)]
    pub providers: ProvidersConfig,
    /// Default LLM settings.
    #[serde(default)]
    pub llm: LlmConfig,
    /// Logging configuration.
    #[serde(default)]
    pub logging: LoggingConfig,
}

impl Default for AppConfig {
    fn default() -> Self {
        Self {
            deployment: DeploymentConfig::default(),
            server: ServerConfig::default(),
            gateway: GatewayConfig::default(),
            database: DatabaseConfig::default(),
            redis: RedisConfig::default(),
            orchestrator: OrchestratorConfig::default(),
            providers: ProvidersConfig::default(),
            llm: LlmConfig::default(),
            logging: LoggingConfig::default(),
        }
    }
}

impl AppConfig {
    /// Load configuration from environment and config files.
    ///
    /// This method loads configuration from multiple sources in order:
    /// 1. Default values
    /// 2. Config files (shannon-api.yaml, shannon.yaml)
    /// 3. Environment variables
    ///
    /// After loading, the configuration is validated. Use [`Self::load_unchecked`]
    /// to skip validation.
    pub fn load() -> anyhow::Result<Self> {
        let config = Self::load_unchecked()?;
        
        // Validate configuration
        ConfigValidator::validate(&config).map_err(|e| {
            anyhow::anyhow!("Configuration validation failed:\n\n{}", e)
        })?;
        
        Ok(config)
    }

    /// Load configuration without validation.
    ///
    /// This is useful for testing or when you want to handle validation separately.
    pub fn load_unchecked() -> anyhow::Result<Self> {
        // Load .env file if present
        let _ = dotenvy::dotenv();

        let config = config::Config::builder()
            // Start with defaults
            .set_default("server.host", "0.0.0.0")?
            .set_default("server.port", 8080)?
            .set_default("server.admin_port", 8081)?
            .set_default("llm.model", "gpt-4o")?
            .set_default("llm.max_tokens", 4096)?
            .set_default("llm.temperature", 0.7)?
            // Add config file if it exists
            .add_source(
                config::File::with_name("config/shannon-api")
                    .required(false),
            )
            .add_source(
                config::File::with_name("config/shannon")
                    .required(false),
            )
            // Override with environment variables
            .add_source(
                config::Environment::with_prefix("SHANNON")
                    .separator("__")
                    .try_parsing(true),
            )
            .build()?;

        let app_config: AppConfig = config.try_deserialize().unwrap_or_default();
        
        // Override with specific environment variables
        let mut app_config = app_config;
        
        // Load deployment configuration from environment
        app_config.deployment = DeploymentConfig::from_env();
        
        // Provider API keys
        if let Ok(key) = std::env::var("OPENAI_API_KEY") {
            app_config.providers.openai.api_key = Some(key);
        }
        if let Ok(key) = std::env::var("ANTHROPIC_API_KEY") {
            app_config.providers.anthropic.api_key = Some(key);
        }
        if let Ok(key) = std::env::var("GOOGLE_API_KEY") {
            app_config.providers.google.api_key = Some(key);
        }
        if let Ok(key) = std::env::var("GROQ_API_KEY") {
            app_config.providers.groq.api_key = Some(key);
        }
        if let Ok(key) = std::env::var("XAI_API_KEY") {
            app_config.providers.xai.api_key = Some(key);
        }

        // Gateway secrets
        if let Ok(secret) = std::env::var("JWT_SECRET") {
            app_config.gateway.jwt_secret = Some(secret);
        }
        if let Ok(url) = std::env::var("POSTGRES_URL") {
            app_config.database.url = Some(url);
        }
        if let Ok(url) = std::env::var("REDIS_URL") {
            app_config.redis.url = Some(url);
        }
        if let Ok(addr) = std::env::var("ORCHESTRATOR_GRPC") {
            app_config.orchestrator.grpc_address = addr;
        }

        Ok(app_config)
    }
}

/// Server configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerConfig {
    /// Host to bind to.
    #[serde(default = "default_host")]
    pub host: String,
    /// Main API port.
    #[serde(default = "default_port")]
    pub port: u16,
    /// Admin/metrics port.
    #[serde(default = "default_admin_port")]
    pub admin_port: u16,
    /// Request timeout in seconds.
    #[serde(default = "default_timeout")]
    pub timeout_secs: u64,
    /// Maximum concurrent requests.
    #[serde(default = "default_max_connections")]
    pub max_connections: usize,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    8080
}

fn default_admin_port() -> u16 {
    8081
}

fn default_timeout() -> u64 {
    300
}

fn default_max_connections() -> usize {
    10000
}

impl Default for ServerConfig {
    fn default() -> Self {
        Self {
            host: default_host(),
            port: default_port(),
            admin_port: default_admin_port(),
            timeout_secs: default_timeout(),
            max_connections: default_max_connections(),
        }
    }
}

/// Gateway configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GatewayConfig {
    /// JWT secret for token signing/validation.
    pub jwt_secret: Option<String>,
    /// JWT expiration in seconds.
    #[serde(default = "default_jwt_expiry")]
    pub jwt_expiry_secs: u64,
    /// Enable API key authentication.
    #[serde(default = "default_true")]
    pub api_key_auth_enabled: bool,
    /// Enable JWT authentication.
    #[serde(default = "default_true")]
    pub jwt_auth_enabled: bool,
    /// Rate limit requests per minute per user.
    #[serde(default = "default_rate_limit")]
    pub rate_limit_per_minute: u32,
    /// Rate limit burst size.
    #[serde(default = "default_rate_burst")]
    pub rate_limit_burst: u32,
    /// Enable idempotency key support.
    #[serde(default = "default_true")]
    pub idempotency_enabled: bool,
    /// Idempotency key TTL in seconds.
    #[serde(default = "default_idempotency_ttl")]
    pub idempotency_ttl_secs: u64,
}

fn default_jwt_expiry() -> u64 {
    86400 // 24 hours
}

fn default_true() -> bool {
    true
}

fn default_rate_limit() -> u32 {
    60
}

fn default_rate_burst() -> u32 {
    10
}

fn default_idempotency_ttl() -> u64 {
    86400 // 24 hours
}

impl Default for GatewayConfig {
    fn default() -> Self {
        Self {
            jwt_secret: None,
            jwt_expiry_secs: default_jwt_expiry(),
            api_key_auth_enabled: true,
            jwt_auth_enabled: true,
            rate_limit_per_minute: default_rate_limit(),
            rate_limit_burst: default_rate_burst(),
            idempotency_enabled: true,
            idempotency_ttl_secs: default_idempotency_ttl(),
        }
    }
}

/// Database configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DatabaseConfig {
    /// PostgreSQL connection URL.
    pub url: Option<String>,
    /// Maximum connection pool size.
    #[serde(default = "default_pool_size")]
    pub max_connections: u32,
    /// Minimum connection pool size.
    #[serde(default = "default_min_connections")]
    pub min_connections: u32,
    /// Connection acquire timeout in seconds.
    #[serde(default = "default_acquire_timeout")]
    pub acquire_timeout_secs: u64,
}

fn default_pool_size() -> u32 {
    100
}

fn default_min_connections() -> u32 {
    5
}

fn default_acquire_timeout() -> u64 {
    30
}

impl Default for DatabaseConfig {
    fn default() -> Self {
        Self {
            url: None,
            max_connections: default_pool_size(),
            min_connections: default_min_connections(),
            acquire_timeout_secs: default_acquire_timeout(),
        }
    }
}

/// Redis configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RedisConfig {
    /// Redis connection URL.
    pub url: Option<String>,
    /// Connection pool size.
    #[serde(default = "default_redis_pool")]
    pub pool_size: u32,
}

fn default_redis_pool() -> u32 {
    10
}

impl Default for RedisConfig {
    fn default() -> Self {
        Self {
            url: None,
            pool_size: default_redis_pool(),
        }
    }
}

/// Orchestrator gRPC configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OrchestratorConfig {
    /// gRPC address for the orchestrator.
    #[serde(default = "default_orchestrator_addr")]
    pub grpc_address: String,
    /// Enable TLS for gRPC connection.
    #[serde(default)]
    pub tls_enabled: bool,
    /// Request timeout in seconds.
    #[serde(default = "default_grpc_timeout")]
    pub timeout_secs: u64,
}

fn default_orchestrator_addr() -> String {
    "http://localhost:50052".to_string()
}

fn default_grpc_timeout() -> u64 {
    300
}

impl Default for OrchestratorConfig {
    fn default() -> Self {
        Self {
            grpc_address: default_orchestrator_addr(),
            tls_enabled: false,
            timeout_secs: default_grpc_timeout(),
        }
    }
}

/// LLM provider configurations.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct ProvidersConfig {
    /// OpenAI configuration.
    #[serde(default)]
    pub openai: ProviderConfig,
    /// Anthropic configuration.
    #[serde(default)]
    pub anthropic: ProviderConfig,
    /// Google configuration.
    #[serde(default)]
    pub google: ProviderConfig,
    /// Groq configuration.
    #[serde(default)]
    pub groq: ProviderConfig,
    /// xAI configuration.
    #[serde(default)]
    pub xai: ProviderConfig,
    /// Custom providers.
    #[serde(default)]
    pub custom: HashMap<String, ProviderConfig>,
}

/// Individual provider configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct ProviderConfig {
    /// API key for the provider.
    pub api_key: Option<String>,
    /// Base URL override.
    pub base_url: Option<String>,
    /// Organization ID (for OpenAI).
    pub organization: Option<String>,
    /// Default model for this provider.
    pub default_model: Option<String>,
    /// Whether this provider is enabled.
    #[serde(default = "default_enabled")]
    pub enabled: bool,
}

fn default_enabled() -> bool {
    true
}

/// Default LLM settings.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LlmConfig {
    /// Default model to use.
    #[serde(default = "default_model")]
    pub model: String,
    /// Maximum tokens to generate.
    #[serde(default = "default_max_tokens")]
    pub max_tokens: u32,
    /// Temperature for sampling.
    #[serde(default = "default_temperature")]
    pub temperature: f32,
    /// Maximum tool loop iterations.
    #[serde(default = "default_max_tool_iterations")]
    pub max_tool_iterations: usize,
    /// Whether to enable streaming by default.
    #[serde(default = "default_streaming")]
    pub streaming: bool,
}

fn default_model() -> String {
    "gpt-4o".to_string()
}

fn default_max_tokens() -> u32 {
    4096
}

fn default_temperature() -> f32 {
    0.7
}

fn default_max_tool_iterations() -> usize {
    10
}

fn default_streaming() -> bool {
    true
}

impl Default for LlmConfig {
    fn default() -> Self {
        Self {
            model: default_model(),
            max_tokens: default_max_tokens(),
            temperature: default_temperature(),
            max_tool_iterations: default_max_tool_iterations(),
            streaming: default_streaming(),
        }
    }
}

/// Logging configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LoggingConfig {
    /// Log level.
    #[serde(default = "default_log_level")]
    pub level: String,
    /// Whether to use JSON format.
    #[serde(default)]
    pub json: bool,
    /// Whether to enable OpenTelemetry.
    #[serde(default)]
    pub otlp_enabled: bool,
    /// OTLP endpoint.
    pub otlp_endpoint: Option<String>,
}

fn default_log_level() -> String {
    "info".to_string()
}

impl Default for LoggingConfig {
    fn default() -> Self {
        Self {
            level: default_log_level(),
            json: false,
            otlp_enabled: false,
            otlp_endpoint: None,
        }
    }
}
