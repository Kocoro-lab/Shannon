//! Embedded Shannon API for local-first operation.
//!
//! This module provides lifecycle management for the embedded Shannon API
//! when running in Tauri desktop/mobile applications. It supports:
//!
//! - SurrealDB for desktop (RocksDB backend)
//! - SQLite for mobile (lightweight)
//! - Durable workflow engine for embedded execution
//! - P2P sync capabilities (future)

use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

#[cfg(feature = "desktop")]
use surrealdb::Surreal;

/// Default port for embedded API server.
pub const DEFAULT_EMBEDDED_PORT: u16 = 8765;

/// State of the embedded API server.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EmbeddedApiState {
    /// Server is not started.
    Stopped,
    /// Server is starting up.
    Starting,
    /// Server is running and accepting requests.
    Running,
    /// Server is shutting down.
    ShuttingDown,
    /// Server failed to start.
    Failed,
}

impl std::fmt::Display for EmbeddedApiState {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Stopped => write!(f, "stopped"),
            Self::Starting => write!(f, "starting"),
            Self::Running => write!(f, "running"),
            Self::ShuttingDown => write!(f, "shutting_down"),
            Self::Failed => write!(f, "failed"),
        }
    }
}

/// Embedded state container for Tauri.
///
/// This holds all the embedded components:
/// - Database connection (SurrealDB or SQLite)
/// - Workflow engine (Durable)
/// - Shannon API instance
#[cfg(feature = "desktop")]
pub struct EmbeddedState {
    /// Database connection.
    pub db: Arc<Surreal<surrealdb::engine::local::Db>>,
    /// Data directory path.
    pub data_dir: PathBuf,
    /// API server handle.
    pub api_handle: Option<EmbeddedApiHandle>,
    /// Current state.
    pub state: EmbeddedApiState,
}

#[cfg(feature = "desktop")]
impl EmbeddedState {
    /// Initialize the embedded state.
    ///
    /// This sets up SurrealDB and prepares the environment for embedded operation.
    pub async fn initialize(app_data_dir: &std::path::Path) -> anyhow::Result<Self> {
        use surrealdb::engine::local::RocksDb;

        tracing::info!("Initializing embedded state at {:?}", app_data_dir);

        // Ensure data directory exists
        std::fs::create_dir_all(app_data_dir)?;

        // Initialize SurrealDB with RocksDB backend
        let db_path = app_data_dir.join("shannon.db");
        let db = Surreal::new::<RocksDb>(db_path.clone()).await?;

        // Select namespace and database
        db.use_ns("shannon").use_db("main").await?;

        // Schema migrations are handled by the Shannon API when it initializes
        // The embedded API server will create tables as needed

        tracing::info!("SurrealDB initialized at {:?}", db_path);

        Ok(Self {
            db: Arc::new(db),
            data_dir: app_data_dir.to_path_buf(),
            api_handle: None,
            state: EmbeddedApiState::Stopped,
        })
    }

    /// Start the embedded API server.
    pub async fn start_api(&mut self, port: u16) -> anyhow::Result<()> {
        self.state = EmbeddedApiState::Starting;

        match start_embedded_api(Some(port)).await {
            Ok(handle) => {
                self.api_handle = Some(handle);
                self.state = EmbeddedApiState::Running;
                Ok(())
            }
            Err(e) => {
                self.state = EmbeddedApiState::Failed;
                anyhow::bail!("Failed to start embedded API: {}", e)
            }
        }
    }

    /// Stop the embedded API server.
    pub fn stop_api(&mut self) {
        if let Some(handle) = &self.api_handle {
            handle.stop();
        }
        self.state = EmbeddedApiState::Stopped;
    }

    /// Get the API base URL.
    pub fn api_url(&self) -> Option<String> {
        self.api_handle.as_ref().map(|h| h.base_url())
    }
}

/// Handle to the embedded API server.
#[derive(Clone)]
pub struct EmbeddedApiHandle {
    /// Whether the server should be running.
    pub should_run: Arc<AtomicBool>,
    /// Current port the server is listening on.
    port: u16,
}

impl std::fmt::Debug for EmbeddedApiHandle {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("EmbeddedApiHandle")
            .field("port", &self.port)
            .field("running", &self.should_run())
            .finish()
    }
}

impl EmbeddedApiHandle {
    /// Create a new handle with default port.
    #[must_use]
    pub fn new() -> Self {
        Self {
            should_run: Arc::new(AtomicBool::new(false)),
            port: DEFAULT_EMBEDDED_PORT,
        }
    }

    /// Create a new handle with a custom port.
    #[must_use]
    pub fn with_port(port: u16) -> Self {
        Self {
            should_run: Arc::new(AtomicBool::new(false)),
            port,
        }
    }

    /// Get the port the server is listening on.
    #[must_use]
    pub fn port(&self) -> u16 {
        self.port
    }

    /// Get the base URL for the embedded API.
    #[must_use]
    pub fn base_url(&self) -> String {
        format!("http://127.0.0.1:{}", self.port)
    }

    /// Signal the server to stop.
    pub fn stop(&self) {
        self.should_run.store(false, Ordering::SeqCst);
    }

    /// Check if the server should be running.
    #[must_use]
    pub fn should_run(&self) -> bool {
        self.should_run.load(Ordering::SeqCst)
    }
}

impl Default for EmbeddedApiHandle {
    fn default() -> Self {
        Self::new()
    }
}

/// Start the embedded Shannon API server.
///
/// This function spawns a background task that runs the Shannon API server
/// on localhost. It returns a handle that can be used to control the server.
#[cfg(feature = "desktop")]
pub async fn start_embedded_api(port: Option<u16>) -> Result<EmbeddedApiHandle, String> {
    use shannon_api::config::AppConfig;

    let handle = EmbeddedApiHandle::with_port(port.unwrap_or(DEFAULT_EMBEDDED_PORT));
    let should_run = handle.should_run.clone();
    let listen_port = handle.port;

    // Mark as should run
    should_run.store(true, Ordering::SeqCst);

    // Spawn the server in a background task
    tokio::spawn(async move {
        tracing::info!("Starting embedded Shannon API on port {}", listen_port);

        // Load configuration with embedded mode defaults
        std::env::set_var("SHANNON_MODE", "embedded");
        std::env::set_var("WORKFLOW_ENGINE", "durable");
        std::env::set_var("DATABASE_DRIVER", "surrealdb");

        let config = match AppConfig::load() {
            Ok(c) => c,
            Err(e) => {
                tracing::error!("Failed to load config: {}", e);
                return;
            }
        };

        // Create the application
        let app = match shannon_api::server::create_app(config).await {
            Ok(a) => a,
            Err(e) => {
                tracing::error!("Failed to create app: {}", e);
                return;
            }
        };

        // Bind to localhost only for security
        let addr = format!("127.0.0.1:{listen_port}");
        let listener = match tokio::net::TcpListener::bind(&addr).await {
            Ok(l) => l,
            Err(e) => {
                tracing::error!("Failed to bind to {}: {}", addr, e);
                return;
            }
        };

        tracing::info!("Embedded Shannon API listening on {}", addr);

        // Run the server with graceful shutdown
        let server = axum::serve(listener, app);

        // Create shutdown signal based on should_run flag
        let shutdown_signal = async move {
            while should_run.load(Ordering::SeqCst) {
                tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
            }
            tracing::info!("Embedded API received shutdown signal");
        };

        if let Err(e) = server.with_graceful_shutdown(shutdown_signal).await {
            tracing::error!("Embedded API server error: {}", e);
        }

        tracing::info!("Embedded Shannon API stopped");
    });

    Ok(handle)
}

/// Start the embedded API (stub for non-desktop builds).
#[cfg(not(feature = "desktop"))]
pub async fn start_embedded_api(_port: Option<u16>) -> Result<EmbeddedApiHandle, String> {
    Err("Embedded API not enabled. Build with --features desktop".to_string())
}

// ============================================================================
// TAURI COMMANDS
// ============================================================================

/// Tauri commands for controlling the embedded API.
pub mod commands {
    use super::*;
    use std::sync::Mutex;
    use tauri::State;

    /// State wrapper for the embedded API handle.
    /// Uses Arc internally to allow sharing across async boundaries.
    #[derive(Clone)]
    pub struct TauriEmbeddedState {
        handle: Arc<Mutex<Option<EmbeddedApiHandle>>>,
    }

    impl TauriEmbeddedState {
        #[must_use]
        pub fn new() -> Self {
            Self {
                handle: Arc::new(Mutex::new(None)),
            }
        }

        /// Set the API handle.
        pub fn set_handle(&self, handle: EmbeddedApiHandle) {
            if let Ok(mut h) = self.handle.lock() {
                *h = Some(handle);
            }
        }
    }

    impl Default for TauriEmbeddedState {
        fn default() -> Self {
            Self::new()
        }
    }

    /// Get the embedded API base URL.
    #[tauri::command]
    pub fn get_embedded_api_url(state: State<'_, TauriEmbeddedState>) -> Option<String> {
        state
            .handle
            .lock()
            .ok()
            .and_then(|h| h.as_ref().map(EmbeddedApiHandle::base_url))
    }

    /// Check if the embedded API is running.
    #[tauri::command]
    pub fn is_embedded_api_running(state: State<'_, TauriEmbeddedState>) -> bool {
        state
            .handle
            .lock()
            .ok()
            .and_then(|h| h.as_ref().map(EmbeddedApiHandle::should_run))
            .unwrap_or(false)
    }

    /// Stop the embedded API.
    #[tauri::command]
    pub fn stop_embedded_api(state: State<'_, TauriEmbeddedState>) {
        if let Ok(handle) = state.handle.lock() {
            if let Some(h) = handle.as_ref() {
                h.stop();
            }
        }
    }

    /// Submit a research query to the embedded API.
    #[tauri::command]
    pub async fn submit_research(
        state: State<'_, TauriEmbeddedState>,
        query: String,
        strategy: String,
    ) -> Result<String, String> {
        let base_url = state
            .handle
            .lock()
            .ok()
            .and_then(|h| h.as_ref().map(EmbeddedApiHandle::base_url))
            .ok_or_else(|| "Embedded API not running".to_string())?;

        // Submit to embedded API
        let client = reqwest::Client::new();
        let response = client
            .post(format!("{base_url}/api/v1/tasks"))
            .json(&serde_json::json!({
                "prompt": query,
                "task_type": strategy,
            }))
            .send()
            .await
            .map_err(|e| e.to_string())?;

        let body: serde_json::Value = response.json().await.map_err(|e| e.to_string())?;
        body["id"]
            .as_str()
            .map(String::from)
            .ok_or_else(|| "No task ID in response".to_string())
    }

    /// Get the status of a research run.
    #[tauri::command]
    pub async fn get_run_status(
        state: State<'_, TauriEmbeddedState>,
        run_id: String,
    ) -> Result<serde_json::Value, String> {
        let base_url = state
            .handle
            .lock()
            .ok()
            .and_then(|h| h.as_ref().map(EmbeddedApiHandle::base_url))
            .ok_or_else(|| "Embedded API not running".to_string())?;

        let client = reqwest::Client::new();
        let response = client
            .get(format!("{base_url}/api/v1/tasks/{run_id}"))
            .send()
            .await
            .map_err(|e| e.to_string())?;

        response.json().await.map_err(|e| e.to_string())
    }

    /// Save an API key to the settings store.
    /// 
    /// # Arguments
    /// * `provider` - Either "openai" or "anthropic"
    /// * `api_key` - The API key to save
    #[tauri::command]
    pub async fn save_api_key(
        app_handle: tauri::AppHandle,
        provider: String,
        api_key: String,
    ) -> Result<(), String> {
        use tauri_plugin_store::StoreExt;
        
        let store_key = match provider.to_lowercase().as_str() {
            "openai" => "openai_api_key",
            "anthropic" => "anthropic_api_key",
            _ => return Err(format!("Unknown provider: {}", provider)),
        };
        
        let env_key = match provider.to_lowercase().as_str() {
            "openai" => "OPENAI_API_KEY",
            "anthropic" => "ANTHROPIC_API_KEY",
            _ => return Err(format!("Unknown provider: {}", provider)),
        };
        
        // Save to store for persistence
        let store = app_handle
            .store("settings.json")
            .map_err(|e| format!("Failed to open store: {}", e))?;
        
        store
            .set(store_key, serde_json::Value::String(api_key.clone()));
        
        store
            .save()
            .map_err(|e| format!("Failed to save store: {}", e))?;
        
        // Also set in environment for immediate use
        std::env::set_var(env_key, &api_key);
        
        log::info!("Saved {} API key to settings", provider);
        
        Ok(())
    }

    /// Get stored API key (returns masked version for display).
    #[tauri::command]
    pub async fn get_api_key_status(
        app_handle: tauri::AppHandle,
    ) -> Result<serde_json::Value, String> {
        use tauri_plugin_store::StoreExt;
        
        let store = app_handle
            .store("settings.json")
            .map_err(|e| format!("Failed to open store: {}", e))?;
        
        let openai_configured = store
            .get("openai_api_key")
            .and_then(|v| v.as_str().map(|s| !s.is_empty() && s.starts_with("sk-")))
            .unwrap_or(false)
            || std::env::var("OPENAI_API_KEY").map(|k| k.starts_with("sk-")).unwrap_or(false);
        
        let anthropic_configured = store
            .get("anthropic_api_key")
            .and_then(|v| v.as_str().map(|s| !s.is_empty() && s.starts_with("sk-ant-")))
            .unwrap_or(false)
            || std::env::var("ANTHROPIC_API_KEY").map(|k| k.starts_with("sk-ant-")).unwrap_or(false);
        
        Ok(serde_json::json!({
            "openai_configured": openai_configured,
            "anthropic_configured": anthropic_configured,
        }))
    }
}
