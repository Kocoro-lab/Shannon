# Comprehensive Logging and IPC Communication Plan

## Executive Summary

This plan addresses the critical need for comprehensive logging throughout the Shannon embedded server startup sequence and request lifecycle. The system will provide real-time visibility into server state through IPC events sent to the Tauri UI, enabling developers to diagnose issues quickly and understand system behavior at every stage.

## Goals

1. **Complete Visibility**: Log every significant state transition, operation, and decision point
2. **Unified Debug Console**: Single UI location to view all logs with filtering and search
3. **IPC Integration**: Server logs streamed to UI in real-time via Tauri events
4. **Structured Logging**: Consistent format with severity levels, context, and timestamps
5. **Performance Tracking**: Measure and log timing for all critical operations
6. **Error Context**: Full error traces with state information for debugging

## Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                    Tauri Desktop App                     │
│  ┌───────────────────────────────────────────────────┐  │
│  │          UI Debug Console Component               │  │
│  │  - Real-time log display                         │  │
│  │  - Filtering (level, component, search)          │  │
│  │  - Export/save logs                              │  │
│  │  - State timeline visualization                  │  │
│  └───────────────────────────────────────────────────┘  │
│                          ▲                              │
│                          │ IPC Events                   │
│                          │                              │
│  ┌───────────────────────────────────────────────────┐  │
│  │            Rust Backend (Tauri)                   │  │
│  │  - IPC event emitter                             │  │
│  │  - Log buffer (recent history)                   │  │
│  │  - Event batching/throttling                     │  │
│  └───────────────────────────────────────────────────┘  │
│                          ▲                              │
│                          │ Function Calls               │
└──────────────────────────┼──────────────────────────────┘
                           │
┌──────────────────────────┼──────────────────────────────┐
│                   Shannon API (Rust)                     │
│  ┌───────────────────────────────────────────────────┐  │
│  │        Enhanced Logging Layer                     │  │
│  │  - Structured log events                         │  │
│  │  - Performance timing                            │  │
│  │  - Context propagation                           │  │
│  │  - IPC forwarding (when in Tauri mode)          │  │
│  └───────────────────────────────────────────────────┘  │
│                                                          │
│  ┌─────────────┬─────────────┬────────────┬──────────┐  │
│  │   Server    │  Workflow   │    Run     │   LLM    │  │
│  │ Initialization│  Engine   │  Manager   │Orchestrator│ │
│  │             │             │            │          │  │
│  │ • Config    │ • Durable   │ • Execute  │ • Chat   │  │
│  │ • Redis     │ • Temporal  │ • Events   │ • Tools  │  │
│  │ • SurrealDB │ • Submit    │ • Errors   │ • Stream │  │
│  │ • Routes    │ • Status    │            │          │  │
│  └─────────────┴─────────────┴────────────┴──────────┘  │
└─────────────────────────────────────────────────────────┘
```

## Part 1: IPC Event System

### 1.1 Event Types Definition

**File**: `desktop/src-tauri/src/ipc_events.rs` (NEW)

```rust
//! IPC event types for server lifecycle and logging.
//!
//! These events are emitted from the embedded Shannon API to the Tauri
//! frontend, providing real-time visibility into server state and operations.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Log level severity.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum LogLevel {
    Trace,
    Debug,
    Info,
    Warn,
    Error,
    Critical,
}

/// Server lifecycle phase.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum LifecyclePhase {
    Initializing,
    ConfigLoading,
    DatabaseConnecting,
    WorkflowEngineStarting,
    RoutesRegistering,
    ServerBinding,
    ServerListening,
    Ready,
    ShuttingDown,
    Stopped,
    Failed,
}

/// Component that emitted the log.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Component {
    EmbeddedApi,
    ShannonApi,
    Server,
    Config,
    Database,
    WorkflowEngine,
    RunManager,
    Orchestrator,
    Gateway,
    TaskHandler,
    ToolExecutor,
    Other(String),
}

/// Server log event sent via IPC.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerLogEvent {
    /// ISO 8601 timestamp.
    pub timestamp: String,
    /// Log level.
    pub level: LogLevel,
    /// Component that generated the log.
    pub component: Component,
    /// Human-readable message.
    pub message: String,
    /// Optional structured context data.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub context: Option<HashMap<String, serde_json::Value>>,
    /// Optional error details.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<ErrorDetails>,
    /// Duration in milliseconds (for timed operations).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub duration_ms: Option<u64>,
}

/// Error details for logging.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ErrorDetails {
    /// Error message.
    pub message: String,
    /// Error type/kind.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub kind: Option<String>,
    /// Stack trace if available.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub backtrace: Option<String>,
    /// Error source chain.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub source: Option<String>,
}

/// Server state change event.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StateChangeEvent {
    pub timestamp: String,
    pub from_phase: LifecyclePhase,
    pub to_phase: LifecyclePhase,
    pub context: Option<HashMap<String, serde_json::Value>>,
}

/// Request lifecycle event.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RequestEvent {
    pub timestamp: String,
    pub request_id: String,
    pub event_type: RequestEventType,
    pub message: String,
    pub context: Option<HashMap<String, serde_json::Value>>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum RequestEventType {
    Received,
    AuthCheck,
    Validated,
    TaskSubmitted,
    WorkflowStarted,
    LlmCalled,
    ToolExecuted,
    StreamingStarted,
    StreamingChunk,
    Completed,
    Failed,
}

/// Health check result.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct HealthCheckEvent {
    pub timestamp: String,
    pub component: Component,
    pub healthy: bool,
    pub message: Option<String>,
    pub latency_ms: Option<u64>,
}

impl ServerLogEvent {
    /// Create a new log event.
    pub fn new(level: LogLevel, component: Component, message: impl Into<String>) -> Self {
        Self {
            timestamp: chrono::Utc::now().to_rfc3339(),
            level,
            component,
            message: message.into(),
            context: None,
            error: None,
            duration_ms: None,
        }
    }

    /// Add context data.
    pub fn with_context(mut self, key: &str, value: serde_json::Value) -> Self {
        self.context
            .get_or_insert_with(HashMap::new)
            .insert(key.to_string(), value);
        self
    }

    /// Add error details.
    pub fn with_error(mut self, error: &dyn std::error::Error) -> Self {
        self.error = Some(ErrorDetails {
            message: error.to_string(),
            kind: Some(std::any::type_name_of_val(error).to_string()),
            backtrace: None, // TODO: Capture backtrace if available
            source: error.source().map(|s| s.to_string()),
        });
        self
    }

    /// Add duration timing.
    pub fn with_duration(mut self, duration_ms: u64) -> Self {
        self.duration_ms = Some(duration_ms);
        self
    }
}
```

### 1.2 IPC Event Emitter

**File**: `desktop/src-tauri/src/ipc_logger.rs` (NEW)

```rust
//! IPC logger that forwards logs to the Tauri frontend.

use std::sync::Arc;
use tauri::{AppHandle, Emitter};
use parking_lot::RwLock;
use std::collections::VecDeque;

use crate::ipc_events::{ServerLogEvent, StateChangeEvent, RequestEvent, HealthCheckEvent};

const MAX_LOG_BUFFER: usize = 1000;
const IPC_EVENT_LOG: &str = "server-log";
const IPC_EVENT_STATE: &str = "server-state-change";
const IPC_EVENT_REQUEST: &str = "server-request";
const IPC_EVENT_HEALTH: &str = "server-health";

/// IPC logger that sends events to the frontend.
#[derive(Clone)]
pub struct IpcLogger {
    app_handle: AppHandle,
    buffer: Arc<RwLock<VecDeque<ServerLogEvent>>>,
}

impl IpcLogger {
    /// Create a new IPC logger.
    pub fn new(app_handle: AppHandle) -> Self {
        Self {
            app_handle,
            buffer: Arc::new(RwLock::new(VecDeque::with_capacity(MAX_LOG_BUFFER))),
        }
    }

    /// Emit a log event.
    pub fn log(&self, event: ServerLogEvent) {
        // Add to buffer
        {
            let mut buffer = self.buffer.write();
            if buffer.len() >= MAX_LOG_BUFFER {
                buffer.pop_front();
            }
            buffer.push_back(event.clone());
        }

        // Emit to frontend
        let _ = self.app_handle.emit(IPC_EVENT_LOG, &event);
        
        // Also log to console
        match event.level {
            crate::ipc_events::LogLevel::Trace => tracing::trace!("{}", event.message),
            crate::ipc_events::LogLevel::Debug => tracing::debug!("{}", event.message),
            crate::ipc_events::LogLevel::Info => tracing::info!("{}", event.message),
            crate::ipc_events::LogLevel::Warn => tracing::warn!("{}", event.message),
            crate::ipc_events::LogLevel::Error => tracing::error!("{}", event.message),
            crate::ipc_events::LogLevel::Critical => tracing::error!("CRITICAL: {}", event.message),
        }
    }

    /// Emit a state change event.
    pub fn state_change(&self, event: StateChangeEvent) {
        let _ = self.app_handle.emit(IPC_EVENT_STATE, &event);
    }

    /// Emit a request event.
    pub fn request(&self, event: RequestEvent) {
        let _ = self.app_handle.emit(IPC_EVENT_REQUEST, &event);
    }

    /// Emit a health check event.
    pub fn health(&self, event: HealthCheckEvent) {
        let _ = self.app_handle.emit(IPC_EVENT_HEALTH, &event);
    }

    /// Get recent logs from buffer.
    pub fn recent_logs(&self, count: usize) -> Vec<ServerLogEvent> {
        let buffer = self.buffer.read();
        buffer
            .iter()
            .rev()
            .take(count)
            .cloned()
            .collect()
    }

    /// Clear the log buffer.
    pub fn clear(&self) {
        self.buffer.write().clear();
    }
}
```

### 1.3 Tauri Commands for Log Access

**Add to `desktop/src-tauri/src/lib.rs`**:

```rust
/// Get recent server logs.
#[tauri::command]
pub fn get_recent_logs(
    logger: State<'_, Option<IpcLogger>>,
    count: Option<usize>,
) -> Result<Vec<ServerLogEvent>, String> {
    logger
        .as_ref()
        .ok_or_else(|| "Logger not initialized".to_string())
        .map(|l| l.recent_logs(count.unwrap_or(100)))
}

/// Clear server logs.
#[tauri::command]
pub fn clear_server_logs(
    logger: State<'_, Option<IpcLogger>>,
) -> Result<(), String> {
    logger
        .as_ref()
        .ok_or_else(|| "Logger not initialized".to_string())
        .map(|l| l.clear())
}
```

## Part 2: Shannon API Logging Enhancements

### 2.1 Structured Logging Module

**File**: `rust/shannon-api/src/logging.rs` (NEW)

```rust
//! Structured logging utilities for Shannon API.
//!
//! Provides consistent logging patterns across all components with
//! optional IPC forwarding when running in Tauri embedded mode.

use std::sync::Arc;
use std::time::Instant;
use parking_lot::RwLock;

/// Global IPC forwarder (set when running in Tauri).
static IPC_FORWARDER: once_cell::sync::OnceCell<Arc<dyn IpcForwarder>> = once_cell::sync::OnceCell::new();

/// Trait for forwarding logs to IPC.
pub trait IpcForwarder: Send + Sync {
    fn forward_log(&self, level: &str, component: &str, message: &str, context: Option<serde_json::Value>);
    fn forward_state(&self, from: &str, to: &str, context: Option<serde_json::Value>);
}

/// Set the global IPC forwarder (called from Tauri).
pub fn set_ipc_forwarder(forwarder: Arc<dyn IpcForwarder>) {
    let _ = IPC_FORWARDER.set(forwarder);
}

/// Log with IPC forwarding.
#[macro_export]
macro_rules! log_with_ipc {
    (trace, $component:expr, $($arg:tt)*) => {
        {
            tracing::trace!($($arg)*);
            if let Some(fwd) = $crate::logging::IPC_FORWARDER.get() {
                fwd.forward_log("trace", $component, &format!($($arg)*), None);
            }
        }
    };
    (debug, $component:expr, $($arg:tt)*) => {
        {
            tracing::debug!($($arg)*);
            if let Some(fwd) = $crate::logging::IPC_FORWARDER.get() {
                fwd.forward_log("debug", $component, &format!($($arg)*), None);
            }
        }
    };
    (info, $component:expr, $($arg:tt)*) => {
        {
            tracing::info!($($arg)*);
            if let Some(fwd) = $crate::logging::IPC_FORWARDER.get() {
                fwd.forward_log("info", $component, &format!($($arg)*), None);
            }
        }
    };
    (warn, $component:expr, $($arg:tt)*) => {
        {
            tracing::warn!($($arg)*);
            if let Some(fwd) = $crate::logging::IPC_FORWARDER.get() {
                fwd.forward_log("warn", $component, &format!($($arg)*), None);
            }
        }
    };
    (error, $component:expr, $($arg:tt)*) => {
        {
            tracing::error!($($arg)*);
            if let Some(fwd) = $crate::logging::IPC_FORWARDER.get() {
                fwd.forward_log("error", $component, &format!($($arg)*), None);
            }
        }
    };
}

/// Timer for measuring operation duration.
pub struct OpTimer {
    start: Instant,
    operation: String,
    component: String,
}

impl OpTimer {
    pub fn new(component: impl Into<String>, operation: impl Into<String>) -> Self {
        let op = operation.into();
        let comp = component.into();
        tracing::debug!(component = %comp, operation = %op, "Starting operation");
        Self {
            start: Instant::now(),
            operation: op,
            component: comp,
        }
    }

    pub fn finish(self) {
        let duration = self.start.elapsed();
        tracing::info!(
            component = %self.component,
            operation = %self.operation,
            duration_ms = duration.as_millis(),
            "Operation completed"
        );
    }

    pub fn finish_with_result<T, E>(self, result: &Result<T, E>) -> &Result<T, E>
    where
        E: std::fmt::Display,
    {
        let duration = self.start.elapsed();
        match result {
            Ok(_) => {
                tracing::info!(
                    component = %self.component,
                    operation = %self.operation,
                    duration_ms = duration.as_millis(),
                    "Operation succeeded"
                );
            }
            Err(e) => {
                tracing::error!(
                    component = %self.component,
                    operation = %self.operation,
                    duration_ms = duration.as_millis(),
                    error = %e,
                    "Operation failed"
                );
            }
        }
        result
    }
}
```

### 2.2 Enhanced Server Initialization Logging

**Update `rust/shannon-api/src/server.rs`**:

Add comprehensive logging at each initialization step:

```rust
pub async fn create_app(config: AppConfig) -> anyhow::Result<Router> {
    use crate::logging::OpTimer;
    
    let _timer = OpTimer::new("server", "create_app");
    
    tracing::info!("========================================");
    tracing::info!("Shannon API Server Initialization");
    tracing::info!("========================================");
    tracing::info!("Mode: {}", config.deployment.mode);
    tracing::info!("Version: {}", env!("CARGO_PKG_VERSION"));
    tracing::info!("========================================");

    // Step 1: LLM Settings
    tracing::info!("⚙️  [1/8] Creating LLM settings...");
    let llm_timer = OpTimer::new("server", "create_llm_settings");
    let llm_settings = create_llm_settings(&config);
    llm_timer.finish();
    tracing::info!("✅ LLM provider configured: {:?}", llm_settings.provider);
    tracing::info!("   Model: {}", llm_settings.model);
    tracing::info!("   Base URL: {}", llm_settings.base_url);
    if llm_settings.api_key.is_none() {
        tracing::warn!("⚠️  No API key configured - requests will fail!");
    }

    // Step 2: Tool Registry
    tracing::info!("🔧 [2/8] Creating tool registry...");
    let tools = Arc::new(ToolRegistry::with_defaults());
    tracing::info!("✅ Tool registry created with {} tools", tools.count());

    // Step 3: Orchestrator
    tracing::info!("🎭 [3/8] Creating LLM orchestrator...");
    let orchestrator = Arc::new(Orchestrator::new(llm_settings, tools));
    tracing::info!("✅ Orchestrator initialized");

    // Step 4: Run Manager
    tracing::info!("🏃 [4/8] Creating run manager...");
    let run_manager = Arc::new(RunManager::new(orchestrator.clone()));
    tracing::info!("✅ Run manager initialized");

    // Step 5: Redis (conditional)
    tracing::info!("💾 [5/8] Initializing Redis connection...");
    let redis = if config.deployment.is_embedded() {
        tracing::info!("   Embedded mode - Redis not required");
        None
    } else if let Some(ref redis_url) = config.redis.url {
        tracing::info!("   Connecting to Redis at {}...", redis_url);
        let redis_timer = OpTimer::new("server", "init_redis");
        match init_redis(redis_url).await {
            Ok(conn) => {
                redis_timer.finish();
                tracing::info!("✅ Redis connection established");
                Some(conn)
            }
            Err(e) => {
                tracing::warn!("⚠️  Failed to connect to Redis: {}", e);
                tracing::warn!("   Rate limiting and sessions will use in-memory fallback");
                None
            }
        }
    } else {
        tracing::info!("   Redis not configured - using in-memory fallback");
        None
    };

    // Step 6: SurrealDB (embedded only)
    tracing::info!("🗄️  [6/8] Initializing embedded database...");
    #[cfg(feature = "embedded")]
    let surreal = if config.deployment.is_embedded() {
        let db_path = std::env::var("SURREALDB_PATH")
            .unwrap_or_else(|_| "./data/shannon.db".to_string());
        tracing::info!("   Connecting to SurrealDB at {}...", db_path);
        let db_timer = OpTimer::new("server", "init_surrealdb");
        
        match surrealdb::Surreal::new::<surrealdb::engine::local::RocksDb>(db_path.clone()).await {
            Ok(db) => {
                if let Err(e) = db.use_ns("shannon").use_db("main").await {
                    tracing::error!("❌ Failed to select SurrealDB namespace/database: {}", e);
                    return Err(anyhow::anyhow!("Database initialization failed: {}", e));
                }
                db_timer.finish();
                tracing::info!("✅ SurrealDB connection established");
                Some(db)
            }
            Err(e) => {
                tracing::error!("❌ Failed to connect to embedded SurrealDB: {}", e);
                return Err(anyhow::anyhow!("Database initialization failed: {}", e));
            }
        }
    } else {
        tracing::info!("   Cloud mode - embedded database not used");
        None
    };

    // Step 7: Workflow Engine
    tracing::info!("⚡ [7/8] Initializing workflow engine...");
    let wf_timer = OpTimer::new("server", "create_workflow_engine");
    let workflow_engine = create_workflow_engine(
        &config,
        #[cfg(feature = "embedded")]
        surreal.clone(),
    ).await?;
    wf_timer.finish();
    tracing::info!("✅ Workflow engine initialized: {} (mode: {})",
        workflow_engine.engine_type(),
        config.deployment.mode
    );

    // Step 8: Build Router
    tracing::info!("🌐 [8/8] Building HTTP router...");
    
    let state = AppState {
        config: Arc::new(config.clone()),
        orchestrator,
        run_manager,
        redis,
        workflow_engine,
        #[cfg(feature = "embedded")]
        surreal,
    };

    let api_router = Router::new()
        .merge(api::create_router())
        .merge(gateway::create_router());
    
    tracing::info!("   Registered API routes");

    let app = api_router
        .layer(CorsLayer::new().allow_origin(Any).allow_methods(Any).allow_headers(Any))
        .layer(TimeoutLayer::with_status_code(
            axum::http::StatusCode::REQUEST_TIMEOUT,
            Duration::from_secs(config.server.timeout_secs),
        ))
        .layer(TraceLayer::new_for_http())
        .layer(axum::middleware::from_fn_with_state(
            state.clone(),
            gateway::auth::auth_middleware,
        ))
        .with_state(state);
    
    tracing::info!("   Configured middleware layers");
    tracing::info!("✅ Router built successfully");

    tracing::info!("========================================");
    tracing::info!("✨ Shannon API Ready for Connections");
    tracing::info!("========================================");

    Ok(app)
}
```

### 2.3 Enhanced Workflow Engine Logging

**Update `rust/shannon-api/src/workflow/engine.rs`**:

Add logging to all workflow operations:

```rust
impl DurableEngine {
    pub async fn new(...) -> anyhow::Result<Self> {
        tracing::info!("Initializing Durable workflow engine");
        tracing::debug!("   WASM directory: {:?}", wasm_dir);
        tracing::debug!("   Max concurrent workflows: {}", max_concurrent);

        if !wasm_dir.exists() {
            tracing::info!("   Creating WASM directory...");
            std::fs::create_dir_all(&wasm_dir)?;
            tracing::info!("   ✅ WASM directory created");
        } else {
            tracing::debug!("   WASM directory exists");
        }

        #[cfg(feature = "embedded")]
        {
            tracing::info!("   Initializing SurrealDB event log...");
            let event_log = if let Some(conn) = surreal_conn {
                tracing::debug!("   Using shared SurrealDB connection");
                SurrealDBEventLog::from_db(Arc::new(conn))
            } else {
                let db_path = std::env::var("SURREALDB_PATH")
                    .unwrap_or_else(|_| "./data/shannon.db".to_string());
                tracing::warn!("   No shared connection - creating new one at {}", db_path);
                SurrealDBEventLog::new(std::path::Path::new(&db_path)).await?
            };
            tracing::info!("   ✅ Event log initialized");
            
            tracing::info!("   Creating embedded worker...");
            let worker = EmbeddedWorker::new(
                Arc::new(event_log),
                wasm_dir.clone(),
                max_concurrent
            ).await?;
            tracing::info!("   ✅ Embedded worker created");
            
            Ok(Self {
                _wasm_dir: wasm_dir,
                max_concurrent,
                tasks: Arc::new(RwLock::new(HashMap::new())),
                channels: Arc::new(RwLock::new(HashMap::new())),
                worker: Arc::new(worker),
            })
        }
        // ... rest of implementation
    }
}

#[async_trait]
impl WorkflowEngineImpl for DurableEngine {
    async fn submit(&self, task: Task) -> anyhow::Result<TaskHandle> {
        tracing::info!("📥 Submitting task {} to Durable engine", task.id);
        tracing::debug!("   Query: {}", task.query);
        tracing::debug!("   Strategy: {:?}", task.strategy);
        tracing::debug!("   User: {}", task.user_id);
        
        // Check concurrency
        let active_count = {
            let tasks = self.tasks.read().await;
            tasks.values().filter(|t| t.state == TaskState::Running).count()
        };
        
        tracing::debug!("   Active workflows: {}/{}", active_count, self.max_concurrent);
        
        if active_count >= self.max_concurrent {
            tracing::warn!("❌ Task {} rejected: max concurrent limit reached", task.id);
            anyhow::bail!("Maximum concurrent workflows ({}) reached", self.max_concurrent);
        }
        
        // ... rest of submit implementation
        
        tracing::info!("✅ Task {} submitted successfully", task.id);
        Ok(handle)
    }
}
```

### 2.4 Enhanced Task Submission Logging

**Update `rust/shannon-api/src/gateway/tasks.rs`**:

```rust
pub async fn submit_task(
    State(state): State<AppState>,
    Json(req): Json<SubmitTaskRequest>,
) -> impl IntoResponse {
    let task_id = Uuid::new_v4().to_string();
    
    tracing::info!("🔵 New task submission: {}", task_id);
    tracing::debug!("   Type: {}", req.task_type);
    tracing::debug!("   Prompt length: {} chars", req.prompt.len());
    tracing::debug!("   Session: {:?}", req.session_id);
    tracing::debug!("   Model: {:?}", req.model);
    
    // Create task record
    let now = chrono::Utc::now().to_rfc3339();
    tracing::debug!("   Task created at: {}", now);
    
    // ... rest of task creation
    
    // Register with run manager (embedded mode)
    if state.redis.is_none() {
        tracing::info!("   📝 Registering with run manager (embedded mode)");
        match state.run_manager.start_run_with_id(...).await {
            Ok(_) => tracing::info!("   ✅ Task registered with run manager"),
            Err(e) => tracing::error!("   ❌ Failed to register: {}", e),
        }
    }
    
    // Store in Redis (cloud mode)
    if let Some(ref redis) = state.redis {
        tracing::debug!("   💾 Storing in Redis");
        // ... redis operations
        tracing::debug!("   ✅ Stored in Redis");
    }
    
    // Submit to workflow engine
    tracing::info!("   ⚡ Submitting to workflow engine ({})", state.workflow_engine.engine_type());
    let workflow_task = crate::workflow::Task {
        // ... task construction
    };
    
    match state.workflow_engine.submit(workflow_task).await {
        Ok(handle) => {
            tracing::info!("   ✅ Task submitted to workflow engine");
            tracing::info!("      Workflow ID: {}", handle.workflow_id);
            tracing::info!("      State: {:?}", handle.state);
            // ... update task status
        }
        Err(e) => {
            tracing::error!("   ❌ Workflow submission failed: {}", e);
            // ... handle error
        }
    }
    
    tracing::info!("🟢 Task submission complete: {}", task_id);
    (StatusCode::ACCEPTED, Json(response))
}
```

### 2.5 Enhanced Run Manager Logging

**Update `rust/shannon-api/src/runtime/manager.rs`**:

```rust
async fn execute_run(...) -> anyhow::Result<()> {
    tracing::info!("🎬 Starting run execution: {}", run_id);
    tracing::debug!("   Session: {}", session_id);
    tracing::debug!("   Message count: {}", messages.len());
    
    let mut content_buffer = String::new();
    let mut total_tokens = 0u32;

    tracing::info!("   📞 Calling LLM orchestrator");
    let stream_start = std::time::Instant::now();
    let stream = orchestrator.chat(messages).await?;
    tracing::debug!("   ✅ Stream established in {:?}", stream_start.elapsed());
    
    futures::pin_mut!(stream);
    
    let mut chunk_count = 0;
    while let Some(event) = stream.next().await {
        chunk_count += 1;
        
        if chunk_count % 10 == 0 {
            tracing::trace!("   📦 Received {} chunks", chunk_count);
        }
        
        // Collect content
        if let NormalizedEvent::MessageDelta { ref content, .. } = event.event {
            content_buffer.push_str(content);
        }
        
        // Collect usage
        if let NormalizedEvent::Usage { total_tokens: tokens, .. } = event.event {
            total_tokens = tokens;
            tracing::debug!("   📊 Token usage: {}", tokens);
        }
        
        // Forward event
        let _ = sender.send(event);
    }
    
    tracing::info!("   ✅ Stream completed: {} chunks, {} tokens", chunk_count, total_tokens);
    tracing::info!("   Response length: {} chars", content_buffer.len());
    
    // Update session
    tracing::debug!("   💾 Updating session history");
    {
        let mut sessions = sessions.write();
        if let Some(session) = sessions.get_mut(&session_id) {
            session.add_message(Message::assistant(&content_buffer));
            session.add_tokens(total_tokens);
            tracing::debug!("   ✅ Session updated");
        }
    }
    
    // Complete run
    tracing::debug!("   ✅ Marking run as complete");
    {
        let mut runs = active_runs.write();
        if let Some(state) = runs.get_mut(&run_id) {
            state.run.complete(&content_buffer);
            state.run.add_tokens(total_tokens, 0.0);
        }
    }
    
    tracing::info!("🎉 Run completed successfully: {}", run_id);
    Ok(())
}
```

## Part 3: Frontend Integration

### 3.1 TypeScript Event Types

**File**: `desktop/lib/ipc-events.ts` (NEW)

```typescript
/**
 * IPC event types matching Rust definitions.
 */

export type LogLevel = 'trace' | 'debug' | 'info' | 'warn' | 'error' | 'critical';

export type LifecyclePhase =
  | 'initializing'
  | 'config_loading'
  | 'database_connecting'
  | 'workflow_engine_starting'
  | 'routes_registering'
  | 'server_binding'
  | 'server_listening'
  | 'ready'
  | 'shutting_down'
  | 'stopped'
  | 'failed';

export type Component =
  | 'embedded_api'
  | 'shannon_api'
  | 'server'
  | 'config'
  | 'database'
  | 'workflow_engine'
  | 'run_manager'
  | 'orchestrator'
  | 'gateway'
  | 'task_handler'
  | 'tool_executor'
  | string;

export interface ErrorDetails {
  message: string;
  kind?: string;
  backtrace?: string;
  source?: string;
}

export interface ServerLogEvent {
  timestamp: string;
  level: LogLevel;
  component: Component;
  message: string;
  context?: Record<string, any>;
  error?: ErrorDetails;
  duration_ms?: number;
}

export interface StateChangeEvent {
  timestamp: string;
  from_phase: LifecyclePhase;
  to_phase: LifecyclePhase;
  context?: Record<string, any>;
}

export type RequestEventType =
  | 'received'
  | 'auth_check'
  | 'validated'
  | 'task_submitted'
  | 'workflow_started'
  | 'llm_called'
  | 'tool_executed'
  | 'streaming_started'
  | 'streaming_chunk'
  | 'completed'
  | 'failed';

export interface RequestEvent {
  timestamp: string;
  request_id: string;
  event_type: RequestEventType;
  message: string;
  context?: Record<string, any>;
}

export interface HealthCheckEvent {
  timestamp: string;
  component: Component;
  healthy: boolean;
  message?: string;
  latency_ms?: number;
}
```

### 3.2 Enhanced Server Context with Logging

**Update `desktop/lib/server-context.tsx`**:

```typescript
import { ServerLogEvent, StateChangeEvent, RequestEvent } from './ipc-events';

interface ServerContextValue extends ServerState {
  // ... existing fields
  logs: ServerLogEvent[];
  stateHistory: StateChangeEvent[];
  addLog: (log: ServerLogEvent) => void;
  clearLogs: () => void;
  getLogsByLevel: (level: LogLevel) => ServerLogEvent[];
  getLogsByComponent: (component: Component) => ServerLogEvent[];
}

export function ServerProvider({ children }: { children: ReactNode }) {
  const [logs, setLogs] = useState<ServerLogEvent[]>([]);
  const [stateHistory, setStateHistory] = useState<StateChangeEvent[]>([]);
  
  // ... existing state
  
  const addLog = useCallback((log: ServerLogEvent) => {
    setLogs(prev => [...prev, log].slice(-1000)); // Keep last 1000 logs
  }, []);
  
  const clearLogs = useCallback(() => {
    setLogs([]);
  }, []);
  
  const getLogsByLevel = useCallback((level: LogLevel) => {
    return logs.filter(log => log.level === level);
  }, [logs]);
  
  const getLogsByComponent = useCallback((component: Component) => {
    return logs.filter(log => log.component === component);
  }, [logs]);

  useEffect(() => {
    if (!isTauri) return;

    let unlistenLog: (() => void) | undefined;
    let unlistenState: (() => void) | undefined;

    const initializeListeners = async () => {
      const { listen } = await import('@tauri-apps/api/event');

      // Listen for log events
      unlistenLog = await listen<ServerLogEvent>('server-log', (event) => {
        console.log('[ServerLog]', event.payload);
        addLog(event.payload);
      });

      // Listen for state change events
      unlistenState = await listen<StateChangeEvent>('server-state-change', (event) => {
        console.log('[StateChange]', event.payload);
        setStateHistory(prev => [...prev, event.payload]);
        
        // Update server status based on state
        if (event.payload.to_phase === 'ready') {
          updateStatus('ready');
        } else if (event.payload.to_phase === 'failed') {
          updateStatus('failed');
        }
      });
    };

    initializeListeners();

    return () => {
      if (unlistenLog) unlistenLog();
      if (unlistenState) unlistenState();
    };
  }, [isTauri, addLog, updateStatus]);

  const contextValue: ServerContextValue = {
    ...state,
    isReady: state.status === 'ready',
    isTauri,
    updateStatus,
    logs,
    stateHistory,
    addLog,
    clearLogs,
    getLogsByLevel,
    getLogsByComponent,
  };

  return (
    <ServerContext.Provider value={contextValue}>
      {children}
    </ServerContext.Provider>
  );
}
```

### 3.3 Debug Console Component

**File**: `desktop/components/debug-console.tsx` (NEW)

```typescript
'use client';

import React, { useState, useMemo } from 'react';
import { useServer } from '@/lib/server-context';
import { ServerLogEvent, LogLevel, Component } from '@/lib/ipc-events';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ScrollArea } from '@/components/ui/scroll-area';

const LOG_LEVEL_COLORS: Record<LogLevel, string> = {
  trace: 'bg-gray-500',
  debug: 'bg-blue-500',
  info: 'bg-green-500',
  warn: 'bg-yellow-500',
  error: 'bg-red-500',
  critical: 'bg-purple-500',
};

export function DebugConsole() {
  const { logs, clearLogs, stateHistory } = useServer();
  const [levelFilter, setLevelFilter] = useState<LogLevel | 'all'>('all');
  const [componentFilter, setComponentFilter] = useState<Component | 'all'>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [autoScroll, setAutoScroll] = useState(true);

  const filteredLogs = useMemo(() => {
    return logs.filter(log => {
      if (levelFilter !== 'all' && log.level !== levelFilter) return false;
      if (componentFilter !== 'all' && log.component !== componentFilter) return false;
      if (searchQuery && !log.message.toLowerCase().includes(searchQuery.toLowerCase())) {
        return false;
      }
      return true;
    });
  }, [logs, levelFilter, componentFilter, searchQuery]);

  const components = useMemo(() => {
    const comps = new Set(logs.map(log => log.component));
    return Array.from(comps).sort();
  }, [logs]);

  return (
    <Card className="h-full flex flex-col">
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle>Server Debug Console</CardTitle>
          <div className="flex gap-2">
            <Badge variant="outline">{logs.length} logs</Badge>
            <Button size="sm" variant="outline" onClick={clearLogs}>
              Clear
            </Button>
          </div>
        </div>
      </CardHeader>
      
      <CardContent className="flex-1 flex flex-col gap-4 overflow-hidden">
        {/* Filters */}
        <div className="flex gap-2 flex-wrap">
          <select
            value={levelFilter}
            onChange={(e) => setLevelFilter(e.target.value as LogLevel | 'all')}
            className="border rounded px-2 py-1 text-sm"
          >
            <option value="all">All Levels</option>
            <option value="trace">Trace</option>
            <option value="debug">Debug</option>
            <option value="info">Info</option>
            <option value="warn">Warn</option>
            <option value="error">Error</option>
            <option value="critical">Critical</option>
          </select>

          <select
            value={componentFilter}
            onChange={(e) => setComponentFilter(e.target.value as Component | 'all')}
            className="border rounded px-2 py-1 text-sm"
          >
            <option value="all">All Components</option>
            {components.map(comp => (
              <option key={comp} value={comp}>{comp}</option>
            ))}
          </select>

          <Input
            placeholder="Search logs..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="flex-1 min-w-[200px]"
          />

          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={autoScroll}
              onChange={(e) => setAutoScroll(e.target.checked)}
            />
            Auto-scroll
          </label>
        </div>

        {/* State Timeline */}
        {stateHistory.length > 0 && (
          <div className="border-b pb-2">
            <div className="text-sm font-semibold mb-1">State Timeline:</div>
            <div className="flex gap-1 flex-wrap text-xs">
              {stateHistory.map((state, i) => (
                <Badge key={i} variant="secondary">
                  {state.from_phase} → {state.to_phase}
                </Badge>
              ))}
            </div>
          </div>
        )}

        {/* Log Display */}
        <ScrollArea className="flex-1 border rounded">
          <div className="p-2 space-y-1 font-mono text-xs">
            {filteredLogs.length === 0 ? (
              <div className="text-gray-500 text-center py-4">
                No logs matching filters
              </div>
            ) : (
              filteredLogs.map((log, i) => (
                <LogEntry key={i} log={log} />
              ))
            )}
          </div>
        </ScrollArea>

        {/* Stats */}
        <div className="text-xs text-gray-600 flex gap-4">
          <span>Total: {logs.length}</span>
          <span>Filtered: {filteredLogs.length}</span>
          <span>Errors: {logs.filter(l => l.level === 'error' || l.level === 'critical').length}</span>
          <span>Warnings: {logs.filter(l => l.level === 'warn').length}</span>
        </div>
      </CardContent>
    </Card>
  );
}

function LogEntry({ log }: { log: ServerLogEvent }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className="hover:bg-gray-50 p-1 rounded cursor-pointer" onClick={() => setExpanded(!expanded)}>
      <div className="flex items-start gap-2">
        <Badge className={LOG_LEVEL_COLORS[log.level]} variant="default">
          {log.level}
        </Badge>
        <span className="text-gray-500 text-[10px]">
          {new Date(log.timestamp).toLocaleTimeString()}
        </span>
        <Badge variant="outline" className="text-[10px]">
          {log.component}
        </Badge>
        {log.duration_ms && (
          <Badge variant="secondary" className="text-[10px]">
            {log.duration_ms}ms
          </Badge>
        )}
        <span className="flex-1">{log.message}</span>
      </div>
      
      {expanded && (log.context || log.error) && (
        <div className="mt-1 ml-4 p-2 bg-gray-100 rounded text-[10px]">
          {log.error && (
            <div className="mb-2">
              <div className="font-semibold text-red-600">Error:</div>
              <div>{log.error.message}</div>
              {log.error.kind && <div className="text-gray-600">Kind: {log.error.kind}</div>}
              {log.error.source && <div className="text-gray-600">Source: {log.error.source}</div>}
            </div>
          )}
          {log.context && (
            <div>
              <div className="font-semibold">Context:</div>
              <pre>{JSON.stringify(log.context, null, 2)}</pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
```

## Part 4: Testing Strategy

### 4.1 Startup Sequence Test

Create comprehensive test script that:
1. Monitors all IPC events during startup
2. Verifies each lifecycle phase is reached
3. Checks timing of each phase
4. Validates no errors occur during normal startup

### 4.2 Request Lifecycle Test

Create test that submits a simple task and validates:
1. Task received event
2. Auth check event
3. Workflow submission event
4. LLM call event
5. Streaming events
6. Completion event

All events logged with proper context and timing.

## Implementation Priority

1. **Phase 1** (Critical): IPC event types and emitter infrastructure
2. **Phase 2** (High): Server initialization logging enhancements
3. **Phase 3** (High): Workflow engine and task submission logging
4. **Phase 4** (Medium): Run manager and orchestrator logging
5. **Phase 5** (Medium): Frontend debug console
6. **Phase 6** (Low): Advanced features (export, search, filtering)

## Success Criteria

✅ Every major operation logged with structured events
✅ All logs visible in UI debug console in real-time
✅ State transitions clearly tracked and visualized
✅ Error context always includes full details
✅ Performance timing captured for all operations
✅ Zero mystery about "why server didn't start"
✅ Developers can diagnose issues in <5 minutes

## Next Steps

1. Review and approve this plan
2. Create detailed implementation tickets for each phase
3. Begin Phase 1 implementation
4. Iterate based on feedback and real-world usage
