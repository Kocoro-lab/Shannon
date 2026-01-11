// Copyright (c) 2025 Prometheus AGS
//
// IPC Event Handling System for Shannon Embedded API Server
//
// This module provides real-time communication between the Tauri backend
// and Next.js frontend through structured IPC events as defined in
// contracts/ipc-events.json.

use crate::embedded_api::{ServerState, ServerStatus};
use chrono::Utc;
use serde::{Deserialize, Serialize};
use std::collections::VecDeque;
use std::sync::{Arc, Mutex};
use tauri::{AppHandle, Emitter};
use thiserror::Error;
use tracing::{debug, error, info, instrument, warn};

// ============================================================================
// IPC EVENT PAYLOAD TYPES
// ============================================================================

/// Server state change event payload
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerStateChangePayload {
    /// Previous server state
    pub from: ServerState,
    /// New server state
    pub to: ServerState,
    /// ISO 8601 timestamp of state change
    pub timestamp: String,
    /// Port number if available, null during discovery
    pub port: Option<u16>,
    /// Complete server URL when available
    pub base_url: Option<String>,
    /// Error message if state change due to failure
    pub error: Option<String>,
    /// Current restart attempt number if restarting
    pub restart_attempt: Option<u8>,
    /// Milliseconds until next restart attempt
    pub next_retry_delay_ms: Option<u64>,
}

/// Server port selected event payload
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerPortSelectedPayload {
    /// Selected port number
    pub port: u16,
    /// Base URL derived from the selected port
    pub base_url: String,
    /// ISO 8601 timestamp of selection
    pub timestamp: String,
}

/// Real-time log event payload
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerLogPayload {
    /// ISO 8601 timestamp of log event
    pub timestamp: String,
    /// Log level
    pub level: LogLevel,
    /// Source component that generated the log
    pub component: Component,
    /// Human-readable log message
    pub message: String,
    /// Optional structured context data
    #[serde(skip_serializing_if = "Option::is_none")]
    pub context: Option<std::collections::HashMap<String, String>>,
    /// Detailed error information if log level is error
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<ErrorInfo>,
    /// Operation duration in milliseconds for performance tracking
    #[serde(skip_serializing_if = "Option::is_none")]
    pub duration_ms: Option<u64>,
    /// Unique ID for deduplication
    #[serde(skip_serializing_if = "Option::is_none")]
    pub id: Option<String>,
    /// Target module/logger name
    #[serde(skip_serializing_if = "Option::is_none")]
    pub target: Option<String>,
}

/// Health check result payload
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerHealthPayload {
    /// Health check endpoint that was tested
    pub endpoint: String,
    /// Overall health status
    pub status: HealthStatus,
    /// When health check was performed
    pub timestamp: String,
    /// Response time in milliseconds
    pub response_time_ms: u64,
    /// Detailed health check results by component
    #[serde(skip_serializing_if = "Option::is_none")]
    pub details: Option<HealthDetails>,
}

/// Server restart attempt payload
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerRestartAttemptPayload {
    /// Current restart attempt number
    pub attempt: u8,
    /// Maximum allowed restart attempts
    pub max_attempts: u8,
    /// Whether restart attempt was successful
    pub success: bool,
    /// When restart attempt was made
    pub timestamp: String,
    /// Error message if restart failed
    pub error: Option<String>,
    /// Milliseconds until next attempt (null if successful or no more attempts)
    pub next_delay_ms: Option<u64>,
    /// Port used for restart attempt
    pub port: Option<u16>,
}

// ============================================================================
// SUPPORTING TYPES
// ============================================================================

/// Log level enumeration
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum LogLevel {
    Error,
    Warn,
    Info,
    Debug,
    Trace,
}

/// Component enumeration
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum Component {
    EmbeddedApi,
    HttpServer,
    Database,
    LlmClient,
    WorkflowEngine,
    Auth,
    Ipc,
    HealthCheck,
}

/// Health status enumeration
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum HealthStatus {
    Healthy,
    Degraded,
    Unhealthy,
}

/// Error information structure
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ErrorInfo {
    /// Error type
    #[serde(rename = "type")]
    pub error_type: String,
    /// Error message
    pub message: String,
    /// Stack trace if available
    #[serde(skip_serializing_if = "Option::is_none")]
    pub stack: Option<String>,
}

/// Health check details
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct HealthDetails {
    /// Database health status
    #[serde(skip_serializing_if = "Option::is_none")]
    pub database: Option<DatabaseHealth>,
    /// LLM provider status
    #[serde(skip_serializing_if = "Option::is_none")]
    pub llm_provider: Option<LlmProviderHealth>,
    /// Workflow engine status
    #[serde(skip_serializing_if = "Option::is_none")]
    pub workflow_engine: Option<WorkflowEngineHealth>,
}

/// Database health information
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DatabaseHealth {
    /// Database health status
    pub status: String,
    /// Response time in milliseconds
    pub response_time_ms: u64,
}

/// LLM provider health information
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LlmProviderHealth {
    /// Provider configuration status
    pub status: String,
    /// Provider name if configured
    #[serde(skip_serializing_if = "Option::is_none")]
    pub provider: Option<String>,
}

/// Workflow engine health information
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorkflowEngineHealth {
    /// Engine status
    pub status: String,
}

// ============================================================================
// IPC COMMAND TYPES
// ============================================================================

/// Parameters for get_recent_logs command
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GetRecentLogsParams {
    /// Number of recent log entries to retrieve
    #[serde(skip_serializing_if = "Option::is_none")]
    pub count: Option<usize>,
    /// Optional log level filter
    #[serde(skip_serializing_if = "Option::is_none")]
    pub level_filter: Option<LogLevel>,
}

/// Parameters for restart_embedded_server command
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RestartServerParams {
    /// Force restart even if server is healthy
    #[serde(default)]
    pub force: bool,
}

/// Response for restart_embedded_server command
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RestartServerResponse {
    /// Whether restart request was accepted
    pub accepted: bool,
    /// Reason if restart was rejected
    pub reason: Option<String>,
}

/// Response for get_server_status command
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerStatusResponse {
    pub state: ServerState,
    pub port: Option<u16>,
    pub base_url: Option<String>,
    pub uptime_seconds: u64,
    pub restart_count: u32,
    pub last_error: Option<String>,
}

// ============================================================================
// IPC EVENT CONSTANTS
// ============================================================================

/// IPC event names as constants
pub mod events {
    pub const SERVER_STATE_CHANGE: &str = "server-state-change";
    pub const SERVER_LOG: &str = "server-log";
    pub const SERVER_HEALTH: &str = "server-health";
    pub const SERVER_RESTART_ATTEMPT: &str = "server-restart-attempt";
    pub const SERVER_PORT_SELECTED: &str = "server-port-selected";
    pub const SERVER_READY: &str = "server-ready";
    pub const RENDERER_READY: &str = "renderer-ready";
}

/// IPC command names as constants
pub mod commands {
    pub const GET_SERVER_STATUS: &str = "get_server_status";
    pub const GET_RECENT_LOGS: &str = "get_recent_logs";
    pub const RESTART_EMBEDDED_SERVER: &str = "restart_embedded_server";
}

// ============================================================================
// VALIDATION CONSTANTS
// ============================================================================

/// Valid port range for embedded server
pub const PORT_RANGE_MIN: u16 = 1906;
pub const PORT_RANGE_MAX: u16 = 1915;

/// Server startup timeout in milliseconds
pub const STARTUP_TIMEOUT_MS: u64 = 5000;

/// Maximum log buffer size for circular buffer
pub const MAX_LOG_BUFFER_SIZE: usize = 1000;

/// Health check interval in milliseconds
pub const HEALTH_CHECK_INTERVAL_MS: u64 = 30000;

/// Maximum restart attempts
pub const MAX_RESTART_ATTEMPTS: u8 = 3;

/// Exponential backoff delays in milliseconds
pub const RESTART_DELAYS_MS: [u64; 3] = [1000, 2000, 4000];

// ============================================================================
// ERRORS
// ============================================================================

/// IPC event handling errors
#[derive(Debug, Error)]
pub enum IpcError {
    #[error("Failed to emit IPC event '{event}': {source}")]
    EmitFailed {
        event: String,
        source: tauri::Error,
    },

    #[error("Failed to serialize payload: {0}")]
    SerializationFailed(#[from] serde_json::Error),

    #[error("Log buffer overflow - dropping old entries")]
    LogBufferOverflow,

    #[error("Invalid log level filter: {level}")]
    InvalidLogLevel { level: String },
}

// ============================================================================
// IPC EVENT EMITTER TRAIT
// ============================================================================

/// Trait for emitting IPC events to the frontend
#[async_trait::async_trait]
pub trait IpcEventEmitter: Send + Sync + std::fmt::Debug {
    /// Emit server state change event
    async fn emit_server_state_change(&self, payload: ServerStateChangePayload) -> Result<(), IpcError>;

    /// Emit server log event
    async fn emit_server_log(&self, payload: ServerLogPayload) -> Result<(), IpcError>;

    /// Emit server health event
    async fn emit_server_health(&self, payload: ServerHealthPayload) -> Result<(), IpcError>;

    /// Emit server restart attempt event
    async fn emit_server_restart_attempt(&self, payload: ServerRestartAttemptPayload) -> Result<(), IpcError>;
}

// ============================================================================
// TAURI IPC EVENT EMITTER
// ============================================================================

/// Tauri-specific implementation of IpcEventEmitter
#[derive(Debug)]
pub struct TauriEventEmitter {
    app_handle: AppHandle,
}

impl TauriEventEmitter {
    /// Create a new TauriEventEmitter
    pub fn new(app_handle: AppHandle) -> Self {
        Self { app_handle }
    }

    /// Emit a generic IPC event
    #[instrument(skip(self, payload), fields(event = %event_name))]
    async fn emit_event<T: Serialize + Send + Sync + Clone>(
        &self,
        event_name: &str,
        payload: T,
    ) -> Result<(), IpcError> {
        debug!("Emitting IPC event: {}", event_name);

        self.app_handle
            .emit(event_name, payload)
            .map_err(|source| IpcError::EmitFailed {
                event: event_name.to_string(),
                source,
            })?;

        debug!("Successfully emitted IPC event: {}", event_name);
        Ok(())
    }
}

#[async_trait::async_trait]
impl IpcEventEmitter for TauriEventEmitter {
    async fn emit_server_state_change(&self, payload: ServerStateChangePayload) -> Result<(), IpcError> {
        info!(
            "Server state transition: {} → {} (port: {:?})",
            payload.from, payload.to, payload.port
        );
        self.emit_event(events::SERVER_STATE_CHANGE, payload).await
    }

    async fn emit_server_log(&self, payload: ServerLogPayload) -> Result<(), IpcError> {
        self.emit_event(events::SERVER_LOG, payload).await
    }

    async fn emit_server_health(&self, payload: ServerHealthPayload) -> Result<(), IpcError> {
        self.emit_event(events::SERVER_HEALTH, payload).await
    }

    async fn emit_server_restart_attempt(&self, payload: ServerRestartAttemptPayload) -> Result<(), IpcError> {
        warn!(
            "Server restart attempt {} of {}: success={}",
            payload.attempt, payload.max_attempts, payload.success
        );
        self.emit_event(events::SERVER_RESTART_ATTEMPT, payload).await
    }
}

// ============================================================================
// LOG BUFFER MANAGER
// ============================================================================

/// Circular buffer for storing recent log entries
#[derive(Debug)]
#[derive(Clone)]
pub struct LogBuffer {
    /// Ring buffer of log entries
    entries: Arc<Mutex<VecDeque<ServerLogPayload>>>,
    /// Maximum buffer size
    max_size: usize,
}

impl LogBuffer {
    /// Create a new log buffer with default size
    pub fn new() -> Self {
        Self::with_capacity(MAX_LOG_BUFFER_SIZE)
    }

    /// Create a new log buffer with specified capacity
    pub fn with_capacity(capacity: usize) -> Self {
        Self {
            entries: Arc::new(Mutex::new(VecDeque::with_capacity(capacity))),
            max_size: capacity,
        }
    }

    /// Add a log entry to the buffer
    #[instrument(skip(self, entry), fields(level = ?entry.level, component = ?entry.component))]
    pub fn push(&self, entry: ServerLogPayload) -> Result<(), IpcError> {
        let mut entries = self.entries.lock().map_err(|_| {
            error!("Failed to acquire log buffer lock");
            IpcError::LogBufferOverflow
        })?;

        // Remove oldest entries if buffer is full
        while entries.len() >= self.max_size {
            entries.pop_front();
            debug!("Dropped oldest log entry due to buffer overflow");
        }

        entries.push_back(entry);
        debug!("Added log entry to buffer (current size: {})", entries.len());
        Ok(())
    }

    /// Get recent log entries
    #[instrument(skip(self), fields(count = count.unwrap_or_else(|| self.max_size)))]
    pub fn get_recent(&self, count: Option<usize>, level_filter: Option<LogLevel>) -> Result<Vec<ServerLogPayload>, IpcError> {
        let entries = self.entries.lock().map_err(|_| {
            error!("Failed to acquire log buffer lock");
            IpcError::LogBufferOverflow
        })?;

        let take_count = count.unwrap_or(self.max_size).min(entries.len());
        let mut result: Vec<_> = entries
            .iter()
            .rev() // Most recent first
            .take(take_count)
            .filter(|entry| {
                level_filter.map_or(true, |filter| entry.level == filter)
            })
            .cloned()
            .collect();

        // Restore chronological order (oldest first)
        result.reverse();

        debug!(
            "Retrieved {} log entries from buffer (filter: {:?})",
            result.len(),
            level_filter
        );
        Ok(result)
    }

    /// Clear all log entries
    pub fn clear(&self) -> Result<(), IpcError> {
        let mut entries = self.entries.lock().map_err(|_| {
            error!("Failed to acquire log buffer lock");
            IpcError::LogBufferOverflow
        })?;

        let cleared_count = entries.len();
        entries.clear();
        info!("Cleared {} log entries from buffer", cleared_count);
        Ok(())
    }

    /// Get current buffer size
    pub fn len(&self) -> usize {
        self.entries.lock().map_or(0, |entries| entries.len())
    }

    /// Check if buffer is empty
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
}

impl Default for LogBuffer {
    fn default() -> Self {
        Self::new()
    }
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

/// Create a server state change payload
pub fn create_state_change_payload(
    from: ServerState,
    to: ServerState,
    port: Option<u16>,
    base_url: Option<String>,
    error: Option<String>,
    restart_attempt: Option<u8>,
    next_retry_delay_ms: Option<u64>,
) -> ServerStateChangePayload {
    ServerStateChangePayload {
        from,
        to,
        timestamp: Utc::now().to_rfc3339(),
        port,
        base_url,
        error,
        restart_attempt,
        next_retry_delay_ms,
    }
}

/// Create a server log payload
pub fn create_log_payload(
    level: LogLevel,
    component: Component,
    message: String,
    context: Option<std::collections::HashMap<String, String>>,
    error: Option<ErrorInfo>,
    duration_ms: Option<u64>,
) -> ServerLogPayload {
    ServerLogPayload {
        timestamp: Utc::now().to_rfc3339(),
        level,
        component,
        message,
        context,
        error,
        duration_ms,
        id: Some(uuid::Uuid::new_v4().to_string()),
        target: None,
    }
}

/// Create a server health payload
pub fn create_health_payload(
    endpoint: String,
    status: HealthStatus,
    response_time_ms: u64,
    details: Option<HealthDetails>,
) -> ServerHealthPayload {
    ServerHealthPayload {
        endpoint,
        status,
        timestamp: Utc::now().to_rfc3339(),
        response_time_ms,
        details,
    }
}

/// Create a server restart attempt payload
pub fn create_restart_attempt_payload(
    attempt: u8,
    max_attempts: u8,
    success: bool,
    error: Option<String>,
    next_delay_ms: Option<u64>,
    port: Option<u16>,
) -> ServerRestartAttemptPayload {
    ServerRestartAttemptPayload {
        attempt,
        max_attempts,
        success,
        timestamp: Utc::now().to_rfc3339(),
        error,
        next_delay_ms,
        port,
    }
}

/// Convert tracing level to IPC log level
impl From<tracing::Level> for LogLevel {
    fn from(level: tracing::Level) -> Self {
        match level {
            tracing::Level::ERROR => LogLevel::Error,
            tracing::Level::WARN => LogLevel::Warn,
            tracing::Level::INFO => LogLevel::Info,
            tracing::Level::DEBUG => LogLevel::Debug,
            tracing::Level::TRACE => LogLevel::Trace,
        }
    }
}

/// Convert server status to response format
impl From<ServerStatus> for ServerStatusResponse {
    fn from(status: ServerStatus) -> Self {
        ServerStatusResponse {
            state: status.state,
            port: status.port,
            base_url: status.base_url,
            uptime_seconds: status.uptime_seconds,
            restart_count: status.restart_count as u32,
            last_error: status.last_error,
        }
    }
}

// ============================================================================
// MOCK IMPLEMENTATION FOR TESTING
// ============================================================================

#[cfg(test)]
pub mod mock {
    use super::*;
    use tokio::sync::Mutex as AsyncMutex;

    /// Mock implementation of IpcEventEmitter for testing
    #[derive(Debug, Default)]
    pub struct MockEventEmitter {
        pub state_changes: Arc<AsyncMutex<Vec<ServerStateChangePayload>>>,
        pub logs: Arc<AsyncMutex<Vec<ServerLogPayload>>>,
        pub health_checks: Arc<AsyncMutex<Vec<ServerHealthPayload>>>,
        pub restart_attempts: Arc<AsyncMutex<Vec<ServerRestartAttemptPayload>>>,
    }

    impl MockEventEmitter {
        pub fn new() -> Self {
            Self::default()
        }

        /// Get all recorded state changes
        pub async fn get_state_changes(&self) -> Vec<ServerStateChangePayload> {
            self.state_changes.lock().await.clone()
        }

        /// Get all recorded logs
        pub async fn get_logs(&self) -> Vec<ServerLogPayload> {
            self.logs.lock().await.clone()
        }

        /// Get all recorded health checks
        pub async fn get_health_checks(&self) -> Vec<ServerHealthPayload> {
            self.health_checks.lock().await.clone()
        }

        /// Get all recorded restart attempts
        pub async fn get_restart_attempts(&self) -> Vec<ServerRestartAttemptPayload> {
            self.restart_attempts.lock().await.clone()
        }

        /// Clear all recorded events
        pub async fn clear(&self) {
            self.state_changes.lock().await.clear();
            self.logs.lock().await.clear();
            self.health_checks.lock().await.clear();
            self.restart_attempts.lock().await.clear();
        }
    }

    #[async_trait::async_trait]
    impl IpcEventEmitter for MockEventEmitter {
        async fn emit_server_state_change(&self, payload: ServerStateChangePayload) -> Result<(), IpcError> {
            self.state_changes.lock().await.push(payload);
            Ok(())
        }

        async fn emit_server_log(&self, payload: ServerLogPayload) -> Result<(), IpcError> {
            self.logs.lock().await.push(payload);
            Ok(())
        }

        async fn emit_server_health(&self, payload: ServerHealthPayload) -> Result<(), IpcError> {
            self.health_checks.lock().await.push(payload);
            Ok(())
        }

        async fn emit_server_restart_attempt(&self, payload: ServerRestartAttemptPayload) -> Result<(), IpcError> {
            self.restart_attempts.lock().await.push(payload);
            Ok(())
        }
    }
}

// ============================================================================
// TESTS
// ============================================================================

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_log_buffer_basic_operations() {
        let buffer = LogBuffer::new();
        assert!(buffer.is_empty());
        assert_eq!(buffer.len(), 0);

        let entry = create_log_payload(
            LogLevel::Info,
            Component::EmbeddedApi,
            "Test message".to_string(),
            None,
            None,
            None,
        );

        buffer.push(entry).unwrap();
        assert!(!buffer.is_empty());
        assert_eq!(buffer.len(), 1);

        let entries = buffer.get_recent(None, None).unwrap();
        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].message, "Test message");
    }

    #[test]
    fn test_log_buffer_overflow() {
        let buffer = LogBuffer::with_capacity(2);

        // Add three entries
        for i in 0..3 {
            let entry = create_log_payload(
                LogLevel::Info,
                Component::EmbeddedApi,
                format!("Message {}", i),
                None,
                None,
                None,
            );
            buffer.push(entry).unwrap();
        }

        // Should only have 2 entries (oldest dropped)
        assert_eq!(buffer.len(), 2);
        let entries = buffer.get_recent(None, None).unwrap();
        assert_eq!(entries[0].message, "Message 1");
        assert_eq!(entries[1].message, "Message 2");
    }

    #[test]
    fn test_log_buffer_filtering() {
        let buffer = LogBuffer::new();

        // Add entries with different log levels
        buffer.push(create_log_payload(
            LogLevel::Error,
            Component::EmbeddedApi,
            "Error message".to_string(),
            None,
            None,
            None,
        )).unwrap();

        buffer.push(create_log_payload(
            LogLevel::Info,
            Component::EmbeddedApi,
            "Info message".to_string(),
            None,
            None,
            None,
        )).unwrap();

        // Filter by error level
        let error_entries = buffer.get_recent(None, Some(LogLevel::Error)).unwrap();
        assert_eq!(error_entries.len(), 1);
        assert_eq!(error_entries[0].message, "Error message");

        // Filter by info level
        let info_entries = buffer.get_recent(None, Some(LogLevel::Info)).unwrap();
        assert_eq!(info_entries.len(), 1);
        assert_eq!(info_entries[0].message, "Info message");
    }

    #[tokio::test]
    async fn test_mock_event_emitter() {
        let emitter = mock::MockEventEmitter::new();

        let state_change = create_state_change_payload(
            ServerState::Starting,
            ServerState::Ready,
            Some(1906),
            Some("http://localhost:1906".to_string()),
            None,
            None,
            None,
        );

        emitter.emit_server_state_change(state_change.clone()).await.unwrap();

        let recorded = emitter.get_state_changes().await;
        assert_eq!(recorded.len(), 1);
        assert_eq!(recorded[0].from, ServerState::Starting);
        assert_eq!(recorded[0].to, ServerState::Ready);
        assert_eq!(recorded[0].port, Some(1906));
    }

    #[test]
    fn test_payload_creation() {
        let state_change = create_state_change_payload(
            ServerState::Idle,
            ServerState::Starting,
            None,
            None,
            None,
            None,
            None,
        );

        assert_eq!(state_change.from, ServerState::Idle);
        assert_eq!(state_change.to, ServerState::Starting);
        assert!(state_change.timestamp.len() > 0);
        assert_eq!(state_change.port, None);
    }
}
