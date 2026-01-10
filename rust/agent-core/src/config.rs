use serde::{Deserialize, Serialize};
use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::RwLock;
use std::time::Duration;

use crate::error::{AgentError, AgentResult};

/// Global configuration instance
static CONFIG: RwLock<Option<Config>> = RwLock::new(None);

/// Agent configuration with resource limits and timeouts
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Config {
    /// WASI sandbox configuration
    pub wasi: WasiConfig,

    /// Memory pool configuration
    pub memory: MemoryConfig,

    /// Tool execution configuration
    pub tools: ToolConfig,

    /// LLM service configuration
    pub llm: LlmConfig,

    /// FSM configuration
    pub fsm: FsmConfig,

    /// Metrics configuration
    pub metrics: MetricsConfig,

    /// Request enforcement configuration
    #[serde(default = "EnforcementConfig::default")]
    pub enforcement: EnforcementConfig,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WasiConfig {
    /// Memory limit in bytes (default: 256MB)
    #[serde(default = "default_wasi_memory")]
    pub memory_limit_bytes: usize,

    /// Execution timeout in seconds (default: 30s)
    #[serde(default = "default_wasi_timeout")]
    pub execution_timeout_secs: u64,

    /// Maximum fuel for WASM execution (default: 1 billion)
    #[serde(default = "default_wasi_fuel")]
    pub max_fuel: u64,

    /// Enable filesystem access (default: true)
    #[serde(default = "default_true")]
    pub enable_filesystem: bool,

    /// Allowed filesystem paths
    #[serde(default = "default_wasi_paths")]
    pub allowed_paths: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MemoryConfig {
    /// Total memory pool size in bytes (default: 1GB)
    #[serde(default = "default_memory_pool_size")]
    pub pool_size_bytes: usize,

    /// Maximum allocation size in bytes (default: 100MB)
    #[serde(default = "default_max_allocation")]
    pub max_allocation_bytes: usize,

    /// TTL for cache entries in seconds (default: 3600s)
    #[serde(default = "default_memory_ttl")]
    pub cache_ttl_secs: u64,

    /// Enable memory pressure monitoring (default: true)
    #[serde(default = "default_true")]
    pub enable_pressure_monitoring: bool,

    /// Memory pressure threshold (0.0-1.0, default: 0.9)
    #[serde(default = "default_pressure_threshold")]
    pub pressure_threshold: f64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolConfig {
    /// Default tool execution timeout in seconds (default: 60s)
    #[serde(default = "default_tool_timeout")]
    pub default_timeout_secs: u64,

    /// Maximum concurrent tool executions (default: 5)
    #[serde(default = "default_max_concurrent")]
    pub max_concurrent_executions: usize,

    /// Enable tool result caching (default: true)
    #[serde(default = "default_true")]
    pub enable_caching: bool,

    /// Cache TTL in seconds (default: 300s)
    #[serde(default = "default_tool_cache_ttl")]
    pub cache_ttl_secs: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LlmConfig {
    /// LLM service base URL
    #[serde(default = "default_llm_url")]
    pub base_url: String,

    /// Request timeout in seconds (default: 30s)
    #[serde(default = "default_llm_timeout")]
    pub request_timeout_secs: u64,

    /// Maximum retries on failure (default: 3)
    #[serde(default = "default_max_retries")]
    pub max_retries: u32,

    /// Retry delay in milliseconds (default: 1000ms)
    #[serde(default = "default_retry_delay")]
    pub retry_delay_ms: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FsmConfig {
    /// Maximum FSM iterations (default: 10)
    #[serde(default = "default_max_iterations")]
    pub max_iterations: u32,

    /// Time budget in milliseconds (default: 30000ms)
    #[serde(default = "default_time_budget")]
    pub time_budget_ms: u64,

    /// Enable belief state persistence (default: false)
    #[serde(default = "default_false")]
    pub persist_belief_state: bool,

    /// State transition timeout in seconds (default: 5s)
    #[serde(default = "default_transition_timeout")]
    pub transition_timeout_secs: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MetricsConfig {
    /// Metrics server port (default: 2113)
    #[serde(default = "default_metrics_port")]
    pub port: u16,

    /// Enable detailed metrics (default: true)
    #[serde(default = "default_true")]
    pub enable_detailed: bool,

    /// Metrics collection interval in seconds (default: 10s)
    #[serde(default = "default_metrics_interval")]
    pub collection_interval_secs: u64,
}

// Default value functions
fn default_wasi_memory() -> usize {
    256 * 1024 * 1024
} // 256MB
fn default_wasi_timeout() -> u64 {
    30
}
fn default_wasi_fuel() -> u64 {
    1_000_000_000
}
fn default_wasi_paths() -> Vec<String> {
    vec!["/tmp".to_string()]
}
fn default_memory_pool_size() -> usize {
    1024 * 1024 * 1024
} // 1GB
fn default_max_allocation() -> usize {
    100 * 1024 * 1024
} // 100MB
fn default_memory_ttl() -> u64 {
    3600
}
fn default_pressure_threshold() -> f64 {
    0.9
}
fn default_tool_timeout() -> u64 {
    60
}
fn default_max_concurrent() -> usize {
    5
}
fn default_tool_cache_ttl() -> u64 {
    300
}
fn default_llm_url() -> String {
    "http://llm-service:8000".to_string()
}
fn default_llm_timeout() -> u64 {
    30
}
fn default_max_retries() -> u32 {
    3
}
fn default_retry_delay() -> u64 {
    1000
}
fn default_max_iterations() -> u32 {
    10
}
fn default_time_budget() -> u64 {
    30000
}
fn default_transition_timeout() -> u64 {
    5
}
fn default_metrics_port() -> u16 {
    2113
}
fn default_metrics_interval() -> u64 {
    10
}
fn default_true() -> bool {
    true
}
fn default_false() -> bool {
    false
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EnforcementConfig {
    #[serde(default = "default_enforce_timeout")]
    pub per_request_timeout_secs: u64,
    #[serde(default = "default_enforce_max_tokens")]
    pub per_request_max_tokens: usize,
    #[serde(default = "default_rate_rps")]
    pub rate_limit_per_key_rps: u32,
    #[serde(default = "default_cb_error_threshold")]
    pub circuit_breaker_error_threshold: f64, // 0.0..1.0
    #[serde(default = "default_cb_window")]
    pub circuit_breaker_rolling_window_secs: u64,
    #[serde(default = "default_cb_min_requests")]
    pub circuit_breaker_min_requests: u32,
    // Optional Redis backend for distributed rate limiting
    #[serde(default)]
    pub rate_redis_url: Option<String>,
    #[serde(default = "default_rate_redis_prefix")]
    pub rate_redis_prefix: String,
    #[serde(default = "default_rate_redis_ttl")]
    pub rate_redis_ttl_secs: u64,
}

fn default_enforce_timeout() -> u64 {
    30
}
fn default_enforce_max_tokens() -> usize {
    4096
}
fn default_rate_rps() -> u32 {
    10
}
fn default_cb_error_threshold() -> f64 {
    0.5
}
fn default_cb_window() -> u64 {
    30
}
fn default_cb_min_requests() -> u32 {
    20
}
fn default_rate_redis_prefix() -> String {
    "rate:".to_string()
}
fn default_rate_redis_ttl() -> u64 {
    60
}

impl Default for EnforcementConfig {
    fn default() -> Self {
        Self {
            per_request_timeout_secs: default_enforce_timeout(),
            per_request_max_tokens: default_enforce_max_tokens(),
            rate_limit_per_key_rps: default_rate_rps(),
            circuit_breaker_error_threshold: default_cb_error_threshold(),
            circuit_breaker_rolling_window_secs: default_cb_window(),
            circuit_breaker_min_requests: default_cb_min_requests(),
            rate_redis_url: None,
            rate_redis_prefix: default_rate_redis_prefix(),
            rate_redis_ttl_secs: default_rate_redis_ttl(),
        }
    }
}

impl Default for Config {
    fn default() -> Self {
        Config {
            wasi: WasiConfig {
                memory_limit_bytes: default_wasi_memory(),
                execution_timeout_secs: default_wasi_timeout(),
                max_fuel: default_wasi_fuel(),
                enable_filesystem: true,
                allowed_paths: default_wasi_paths(),
            },
            memory: MemoryConfig {
                pool_size_bytes: default_memory_pool_size(),
                max_allocation_bytes: default_max_allocation(),
                cache_ttl_secs: default_memory_ttl(),
                enable_pressure_monitoring: true,
                pressure_threshold: default_pressure_threshold(),
            },
            tools: ToolConfig {
                default_timeout_secs: default_tool_timeout(),
                max_concurrent_executions: default_max_concurrent(),
                enable_caching: true,
                cache_ttl_secs: default_tool_cache_ttl(),
            },
            llm: LlmConfig {
                base_url: default_llm_url(),
                request_timeout_secs: default_llm_timeout(),
                max_retries: default_max_retries(),
                retry_delay_ms: default_retry_delay(),
            },
            fsm: FsmConfig {
                max_iterations: default_max_iterations(),
                time_budget_ms: default_time_budget(),
                persist_belief_state: false,
                transition_timeout_secs: default_transition_timeout(),
            },
            metrics: MetricsConfig {
                port: default_metrics_port(),
                enable_detailed: true,
                collection_interval_secs: default_metrics_interval(),
            },
            enforcement: EnforcementConfig::default(),
        }
    }
}

impl Config {
    /// Load configuration from file or environment
    pub fn load() -> AgentResult<Self> {
        // Check for config file path in environment
        if let Ok(config_path) = env::var("AGENT_CONFIG_PATH") {
            Self::from_file(&config_path)
        } else if Path::new("/app/config/agent.yaml").exists() {
            // Try default container path
            Self::from_file("/app/config/agent.yaml")
        } else if Path::new("config/agent.yaml").exists() {
            // Try local path
            Self::from_file("config/agent.yaml")
        } else {
            // Use defaults with environment overrides
            Ok(Self::from_env(Self::default()))
        }
    }

    /// Load configuration from a YAML file
    pub fn from_file(path: &str) -> AgentResult<Self> {
        let content = fs::read_to_string(path).map_err(|e| {
            AgentError::ConfigurationError(format!("Failed to read config file: {}", e))
        })?;

        let mut config: Config = serde_yaml::from_str(&content).map_err(|e| {
            AgentError::ConfigurationError(format!("Failed to parse config: {}", e))
        })?;

        // Apply environment overrides
        config = Self::from_env(config);

        Ok(config)
    }

    /// Override configuration with environment variables
    pub fn from_env(mut config: Config) -> Self {
        config = apply_feature_defaults(config);

        // WASI overrides
        if let Ok(v) = env::var("WASI_MEMORY_LIMIT_MB") {
            if let Ok(mb) = v.parse::<usize>() {
                config.wasi.memory_limit_bytes = mb * 1024 * 1024;
            }
        }
        if let Ok(v) = env::var("WASI_TIMEOUT_SECONDS") {
            if let Ok(secs) = v.parse::<u64>() {
                config.wasi.execution_timeout_secs = secs;
            }
        }

        // Memory pool overrides
        if let Ok(v) = env::var("MEMORY_POOL_SIZE_MB") {
            if let Ok(mb) = v.parse::<usize>() {
                config.memory.pool_size_bytes = mb * 1024 * 1024;
            }
        }

        // LLM service overrides
        if let Ok(v) = env::var("LLM_SERVICE_URL") {
            config.llm.base_url = v;
        }
        if let Ok(v) = env::var("LLM_TIMEOUT_SECONDS") {
            if let Ok(secs) = v.parse::<u64>() {
                config.llm.request_timeout_secs = secs;
            }
        }

        // Metrics overrides
        if let Ok(v) = env::var("METRICS_PORT") {
            if let Ok(port) = v.parse::<u16>() {
                config.metrics.port = port;
            }
        }

        // Enforcement overrides
        if let Ok(v) = env::var("ENFORCE_TIMEOUT_SECONDS") {
            if let Ok(secs) = v.parse::<u64>() {
                config.enforcement.per_request_timeout_secs = secs;
            }
        }
        if let Ok(v) = env::var("ENFORCE_MAX_TOKENS") {
            if let Ok(n) = v.parse::<usize>() {
                config.enforcement.per_request_max_tokens = n;
            }
        }
        if let Ok(v) = env::var("ENFORCE_RATE_RPS") {
            if let Ok(n) = v.parse::<u32>() {
                config.enforcement.rate_limit_per_key_rps = n;
            }
        }
        if let Ok(v) = env::var("ENFORCE_CB_ERROR_THRESHOLD") {
            if let Ok(f) = v.parse::<f64>() {
                config.enforcement.circuit_breaker_error_threshold = f;
            }
        }
        if let Ok(v) = env::var("ENFORCE_CB_WINDOW_SECONDS") {
            if let Ok(secs) = v.parse::<u64>() {
                config.enforcement.circuit_breaker_rolling_window_secs = secs;
            }
        }
        if let Ok(v) = env::var("ENFORCE_CB_MIN_REQUESTS") {
            if let Ok(n) = v.parse::<u32>() {
                config.enforcement.circuit_breaker_min_requests = n;
            }
        }
        if let Ok(v) = env::var("ENFORCE_RATE_REDIS_URL") {
            if !v.is_empty() {
                config.enforcement.rate_redis_url = Some(v);
            }
        }
        if let Ok(v) = env::var("ENFORCE_RATE_REDIS_PREFIX") {
            if !v.is_empty() {
                config.enforcement.rate_redis_prefix = v;
            }
        }
        if let Ok(v) = env::var("ENFORCE_RATE_REDIS_TTL") {
            if let Ok(secs) = v.parse::<u64>() {
                config.enforcement.rate_redis_ttl_secs = secs;
            }
        }

        config
    }

    /// Get the global configuration instance
    pub fn global() -> AgentResult<Config> {
        let guard = CONFIG
            .read()
            .map_err(|e| AgentError::InternalError(format!("Config lock poisoned: {}", e)))?;

        if let Some(ref config) = *guard {
            Ok(config.clone())
        } else {
            drop(guard);
            Self::initialize()
        }
    }

    /// Initialize the global configuration
    pub fn initialize() -> AgentResult<Config> {
        let config = Self::load()?;

        let mut guard = CONFIG
            .write()
            .map_err(|e| AgentError::InternalError(format!("Config lock poisoned: {}", e)))?;

        *guard = Some(config.clone());
        Ok(config)
    }

    /// Update the global configuration
    #[allow(dead_code)]
    pub fn update(config: Config) -> AgentResult<()> {
        let mut guard = CONFIG
            .write()
            .map_err(|e| AgentError::InternalError(format!("Config lock poisoned: {}", e)))?;

        *guard = Some(config);
        Ok(())
    }

    /// Get WASI sandbox timeout as Duration
    pub fn wasi_timeout(&self) -> Duration {
        Duration::from_secs(self.wasi.execution_timeout_secs)
    }

    /// Get LLM request timeout as Duration
    pub fn llm_timeout(&self) -> Duration {
        Duration::from_secs(self.llm.request_timeout_secs)
    }

    /// Get memory cache TTL as Duration
    #[allow(dead_code)]
    pub fn memory_ttl(&self) -> Duration {
        Duration::from_secs(self.memory.cache_ttl_secs)
    }

    /// Get tool execution timeout as Duration
    #[allow(dead_code)]
    pub fn tool_timeout(&self) -> Duration {
        Duration::from_secs(self.tools.default_timeout_secs)
    }
}

#[derive(Debug, Deserialize)]
struct FeatureOverrides {
    workflows: Option<FeatureWorkflows>,
    enforcement: Option<FeatureEnforcement>,
}

#[derive(Debug, Deserialize)]
struct FeatureWorkflows {
    #[serde(default)]
    synthesis: Option<FeatureSynthesis>,
    #[serde(default)]
    tool_execution: Option<FeatureToolExecution>,
}

#[derive(Debug, Deserialize)]
struct FeatureSynthesis {
    #[serde(default)]
    bypass_single_result: Option<bool>,
}

#[derive(Debug, Deserialize)]
struct FeatureToolExecution {
    #[serde(default)]
    parallelism: Option<usize>,
    #[serde(default)]
    auto_selection: Option<bool>,
}

#[derive(Debug, Deserialize)]
struct FeatureEnforcement {
    #[serde(default)]
    timeout_seconds: Option<u64>,
    #[serde(default)]
    max_tokens: Option<usize>,
    #[serde(default)]
    rate_limiting: Option<FeatureRateLimiting>,
    #[serde(default)]
    circuit_breaker: Option<FeatureCircuitBreaker>,
}

#[derive(Debug, Deserialize)]
struct FeatureRateLimiting {
    #[serde(default)]
    rps: Option<u32>,
}

#[derive(Debug, Deserialize)]
struct FeatureCircuitBreaker {
    #[serde(default)]
    error_threshold: Option<f64>,
    #[serde(default)]
    min_requests: Option<u32>,
    #[serde(default)]
    window_seconds: Option<u64>,
}

fn apply_feature_defaults(mut config: Config) -> Config {
    if let Some(features) = load_feature_overrides() {
        if let Some(workflows) = features.workflows {
            if let Some(tool_exec) = workflows.tool_execution {
                if let Some(parallelism) = tool_exec.parallelism {
                    if parallelism > 0 {
                        config.tools.max_concurrent_executions = parallelism;
                    }
                }
            }
        }

        if let Some(enforcement) = features.enforcement {
            if let Some(timeout) = enforcement.timeout_seconds {
                if timeout > 0 {
                    config.enforcement.per_request_timeout_secs = timeout;
                }
            }
            if let Some(max_tokens) = enforcement.max_tokens {
                if max_tokens > 0 {
                    config.enforcement.per_request_max_tokens = max_tokens;
                }
            }
            if let Some(rate) = enforcement.rate_limiting {
                if let Some(rps) = rate.rps {
                    if rps > 0 {
                        config.enforcement.rate_limit_per_key_rps = rps;
                    }
                }
            }
            if let Some(cb) = enforcement.circuit_breaker {
                if let Some(threshold) = cb.error_threshold {
                    if threshold >= 0.0 {
                        config.enforcement.circuit_breaker_error_threshold = threshold;
                    }
                }
                if let Some(min_requests) = cb.min_requests {
                    if min_requests > 0 {
                        config.enforcement.circuit_breaker_min_requests = min_requests;
                    }
                }
                if let Some(window) = cb.window_seconds {
                    if window > 0 {
                        config.enforcement.circuit_breaker_rolling_window_secs = window;
                    }
                }
            }
        }
    }

    config
}

fn load_feature_overrides() -> Option<FeatureOverrides> {
    let path = features_path()?;
    let content = fs::read_to_string(&path).ok()?;
    serde_yaml::from_str::<FeatureOverrides>(&content).ok()
}

fn features_path() -> Option<String> {
    if let Ok(env_path) = env::var("CONFIG_PATH") {
        if !env_path.trim().is_empty() {
            let candidate = PathBuf::from(&env_path);
            if candidate.is_dir() {
                let file = candidate.join("features.yaml");
                if file.exists() {
                    return Some(file.to_string_lossy().to_string());
                }
            } else if candidate.exists() {
                return Some(env_path);
            }
        }
    }

    let defaults = ["/app/config/features.yaml", "config/features.yaml"];

    for path in defaults.iter() {
        if Path::new(path).exists() {
            return Some(path.to_string());
        }
    }

    None
}

#[cfg(test)]
mod tests {
    use super::*;
    use serial_test::serial;

    #[test]
    fn test_default_config() {
        let config = Config::default();

        assert_eq!(config.wasi.memory_limit_bytes, 256 * 1024 * 1024);
        assert_eq!(config.wasi.execution_timeout_secs, 30);
        assert_eq!(config.memory.pool_size_bytes, 1024 * 1024 * 1024);
        assert_eq!(config.llm.base_url, "http://llm-service:8000");
        assert_eq!(config.metrics.port, 2113);
    }

    #[test]
    #[serial]
    fn test_env_overrides() {
        // Clean up first to ensure no interference from other tests
        // SAFETY: These are test environment variables and we're in a controlled test environment
        unsafe {
            env::remove_var("WASI_MEMORY_LIMIT_MB");
            env::remove_var("LLM_SERVICE_URL");
            env::remove_var("METRICS_PORT");
        }

        // Set test values
        // SAFETY: These are test environment variables and we're in a controlled test environment
        unsafe {
            env::set_var("WASI_MEMORY_LIMIT_MB", "512");
            env::set_var("LLM_SERVICE_URL", "http://custom:9000");
            env::set_var("METRICS_PORT", "3000");
        }

        let config = Config::from_env(Config::default());

        assert_eq!(config.wasi.memory_limit_bytes, 512 * 1024 * 1024);
        assert_eq!(config.llm.base_url, "http://custom:9000");
        assert_eq!(config.metrics.port, 3000);

        // Clean up
        // SAFETY: These are test environment variables and we're in a controlled test environment
        unsafe {
            env::remove_var("WASI_MEMORY_LIMIT_MB");
            env::remove_var("LLM_SERVICE_URL");
            env::remove_var("METRICS_PORT");
        }
    }

    #[test]
    fn test_duration_helpers() {
        let config = Config::default();

        assert_eq!(config.wasi_timeout(), Duration::from_secs(30));
        assert_eq!(config.llm_timeout(), Duration::from_secs(30));
        assert_eq!(config.memory_ttl(), Duration::from_secs(3600));
        assert_eq!(config.tool_timeout(), Duration::from_secs(60));
    }

    #[test]
    #[serial]
    fn test_global_config() {
        // Clear any existing environment variables that might interfere
        // SAFETY: These are test environment variables and we're in a controlled test environment
        unsafe {
            env::remove_var("AGENT_CORE_METRICS_PORT");
            env::remove_var("METRICS_PORT");
            env::remove_var("AGENT_CONFIG_PATH");
        }

        let config = Config::global().expect("Should load global config");
        assert!(config.wasi.memory_limit_bytes > 0);

        // Update global config
        let mut new_config = config.clone();
        new_config.metrics.port = 9999;
        Config::update(new_config.clone()).expect("Should update config");

        let updated = Config::global().expect("Should get updated config");

        // The global config should now have the updated value
        // Note: Config::global() uses the static CONFIG if it's already initialized,
        // so our update should be reflected
        assert_eq!(
            updated.metrics.port, 9999,
            "Updated config should have port 9999"
        );
    }
}
