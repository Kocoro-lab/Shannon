//! Shannon Desktop Application
//!
//! This is the Tauri-based desktop client for the Shannon AI platform.
//! It supports multiple deployment modes:
//!
//! 1. **Desktop Mode** (`--features desktop`): Full embedded stack with:
//!    - SurrealDB (RocksDB backend) for local storage
//!    - Durable workflow engine for task execution
//!    - Local-first operation with optional P2P sync
//!
//! 2. **Mobile Mode** (`--features mobile`): Lightweight embedded stack with:
//!    - SQLite for local storage
//!    - Same Durable workflow engine
//!    - Battery-optimized sync
//!
//! 3. **Cloud Mode** (`--features cloud`): Thin client connecting to:
//!    - Remote Shannon API server
//!    - No local storage or processing
//!
//! # Architecture
//!
//! ```text
//! ┌─────────────────────────────────────────────┐
//! │             Tauri Shell                     │
//! │  ┌───────────────────────────────────────┐  │
//! │  │          Next.js Frontend             │  │
//! │  └───────────────────────────────────────┘  │
//! │                    ↓                        │
//! │  ┌───────────────────────────────────────┐  │
//! │  │      Embedded Shannon API             │  │
//! │  │  ┌─────────────┬─────────────────┐    │  │
//! │  │  │ Durable     │ SurrealDB/      │    │  │
//! │  │  │ Workflows   │ SQLite          │    │  │
//! │  │  └─────────────┴─────────────────┘    │  │
//! │  └───────────────────────────────────────┘  │
//! └─────────────────────────────────────────────┘
//! ```

pub mod embedded_api;
pub mod ipc_events;
pub mod ipc_logger;

#[cfg(feature = "desktop")]
use embedded_api::commands::TauriEmbeddedState;
#[cfg(feature = "desktop")]
use ipc_logger::IpcLogLayer;

use tauri::{
    menu::{Menu, MenuItem, PredefinedMenuItem, Submenu},
    App, Manager, State,
};

/// Store key for persisted API keys
#[cfg(feature = "desktop")]
const _STORE_KEY_OPENAI: &str = "openai_api_key";
#[cfg(feature = "desktop")]
const _STORE_KEY_ANTHROPIC: &str = "anthropic_api_key";
#[cfg(feature = "desktop")]
const _SETTINGS_STORE: &str = "settings.json";

/// Create the application menu with developer tools.
fn create_app_menu(app: &App) -> Result<Menu<tauri::Wry>, tauri::Error> {
    let menu = Menu::new(app)?;

    // File menu
    let file_menu = Submenu::new(app, "File", true)?;
    file_menu.append(&PredefinedMenuItem::quit(app, Some("Quit"))?)?;

    // View menu
    let view_menu = Submenu::new(app, "View", true)?;

    // Add Developer Tools menu item
    let dev_tools_item = MenuItem::with_id(
        app,
        "dev_tools",
        "Developer Tools",
        true,
        Some("CmdOrCtrl+Shift+I"),
    )?;
    view_menu.append(&dev_tools_item)?;
    view_menu.append(&PredefinedMenuItem::separator(app)?)?;
    view_menu.append(&PredefinedMenuItem::fullscreen(app, None)?)?;

    // Edit menu (standard shortcuts)
    let edit_menu = Submenu::new(app, "Edit", true)?;
    edit_menu.append(&PredefinedMenuItem::undo(app, None)?)?;
    edit_menu.append(&PredefinedMenuItem::redo(app, None)?)?;
    edit_menu.append(&PredefinedMenuItem::separator(app)?)?;
    edit_menu.append(&PredefinedMenuItem::cut(app, None)?)?;
    edit_menu.append(&PredefinedMenuItem::copy(app, None)?)?;
    edit_menu.append(&PredefinedMenuItem::paste(app, None)?)?;
    edit_menu.append(&PredefinedMenuItem::select_all(app, None)?)?;

    // Append all menus
    menu.append(&file_menu)?;
    menu.append(&edit_menu)?;
    menu.append(&view_menu)?;

    Ok(menu)
}

/// Tauri command to get recent server logs.
///
/// # Errors
///
/// Returns an error if the IPC logger state is not found.
#[cfg(feature = "desktop")]
#[tauri::command]
async fn get_recent_logs(count: usize, logger: State<'_, IpcLogLayer>) -> Result<Vec<ipc_events::ServerLogPayload>, String> {
    Ok(logger.get_recent_logs(Some(count), None).await)
}

/// Tauri command to clear all server logs.
#[cfg(feature = "desktop")]
#[tauri::command]
async fn clear_server_logs(_logger: State<'_, IpcLogLayer>) -> Result<(), String> {
    // IpcLogLayer doesn't have a clear method, so we'll use the log buffer
    // The clear functionality should be implemented on the layer itself
    Ok(())
}

/// Get current embedded server status.
#[cfg(feature = "desktop")]
#[tauri::command]
async fn get_server_status(state: State<'_, TauriEmbeddedState>) -> Result<ipc_events::ServerStatusResponse, String> {
    state
        .get_server_status()
        .await
        .map_err(|e| e.to_string())
}

/// Get recent log entries with optional filtering.
#[cfg(feature = "desktop")]
#[tauri::command]
async fn get_recent_server_logs(
    count: Option<usize>,
    level_filter: Option<String>,
    state: State<'_, TauriEmbeddedState>
) -> Result<Vec<ipc_events::ServerLogPayload>, String> {
    let level = level_filter
        .and_then(|l| match l.to_lowercase().as_str() {
            "error" => Some(ipc_events::LogLevel::Error),
            "warn" => Some(ipc_events::LogLevel::Warn),
            "info" => Some(ipc_events::LogLevel::Info),
            "debug" => Some(ipc_events::LogLevel::Debug),
            "trace" => Some(ipc_events::LogLevel::Trace),
            _ => None,
        });

    state
        .get_recent_logs(count, level)
        .await
        .map_err(|e| e.to_string())
}

/// Restart the embedded server with optional force flag.
#[cfg(feature = "desktop")]
#[tauri::command]
async fn restart_embedded_server(
    force: Option<bool>,
    state: State<'_, TauriEmbeddedState>
) -> Result<ipc_events::RestartServerResponse, String> {
    state
        .restart_server(force.unwrap_or(false))
        .await
        .map_err(|e| e.to_string())
}

/// Main entry point for the Tauri application.
#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    #[allow(unused_mut)]
    let mut builder = tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_store::Builder::default().build());

    // Create state early so it's available in setup
    #[cfg(feature = "desktop")]
    let embedded_state = TauriEmbeddedState::new();
    #[cfg(feature = "desktop")]
    let embedded_state_for_setup = embedded_state.clone();

    builder = builder.setup(move |app| {
        // Initialize logging
        if cfg!(debug_assertions) {
            app.handle().plugin(
                tauri_plugin_log::Builder::default()
                    .level(log::LevelFilter::Info)
                    .build(),
            )?;
        }

        // Initialize IPC logger for desktop mode
        #[cfg(feature = "desktop")]
        let ipc_event_emitter = std::sync::Arc::new(ipc_events::TauriEventEmitter::new(app.handle().clone()));

        #[cfg(feature = "desktop")]
        let ipc_logger = IpcLogLayer::new(ipc_event_emitter.clone());

        #[cfg(feature = "desktop")]
        {
            // Store IPC logger in app state
            app.manage(ipc_logger.clone());
        }

        // Desktop mode: Start embedded API with SurrealDB
        #[cfg(feature = "desktop")]
        {
            log::info!("Desktop mode enabled, starting embedded Shannon API...");

            // Get app data directory
            let app_data_dir = app
                .path()
                .app_data_dir()
                .expect("Failed to get app data directory");

            // Create data directory if it doesn't exist
            if let Err(e) = std::fs::create_dir_all(&app_data_dir) {
                log::error!("Failed to create data directory: {}", e);
            }

            log::info!("Data directory: {:?}", app_data_dir);

            // Clone state and handles for async task
            let state_clone = embedded_state_for_setup.clone();
            let app_handle_for_thread = app.handle().clone();
            let ipc_logger_for_thread = ipc_logger.clone();

            // Start the server in a background thread - don't wait for it
            std::thread::spawn(move || {
                let rt = tokio::runtime::Runtime::new().expect("Failed to create tokio runtime");

                rt.block_on(async move {
                    log::info!("🚀 Starting embedded Shannon API with dynamic port...");

                    // Use port 1906 as the preferred starting port
                    match embedded_api::start_embedded_api(
                        Some(1906),
                        app_handle_for_thread,
                        ipc_logger_for_thread,
                    )
                    .await
                    {
                        Ok(handle) => {
                            log::info!("✅ Embedded API started on port {:?}", handle.port());
                            state_clone.set_handle(handle);
                        }
                        Err(e) => {
                            log::error!("❌ Failed to start embedded API: {}", e);
                        }
                    }
                });
            });

            log::info!("Embedded API startup initiated in background thread");

            // The frontend will now wait for "server-ready" event.
        }

        // Mobile mode: Start embedded API with SQLite
        #[cfg(feature = "mobile")]
        {
            log::info!("Mobile mode enabled, starting lightweight embedded API...");
            log::info!("Using SQLite for local storage");
        }

        // Cloud mode: Just log that we're connecting to remote
        #[cfg(feature = "cloud")]
        {
            log::info!("Cloud mode enabled, connecting to remote Shannon API...");
        }

        // Create application menu with developer tools
        let menu = create_app_menu(app)?;
        app.set_menu(menu)?;

        // Handle menu events
        app.on_menu_event(|app, event| match event.id().as_ref() {
            "dev_tools" => {
                if let Some(window) = app.get_webview_window("main") {
                    window.open_devtools();
                }
            }
            "quit" => {
                app.exit(0);
            }
            _ => {}
        });

        Ok(())
    });

    // Register embedded API commands for desktop mode
    #[cfg(feature = "desktop")]
    {
        builder = builder
            .manage(embedded_state)
            .invoke_handler(tauri::generate_handler![
                embedded_api::commands::get_embedded_api_url,
                embedded_api::commands::is_embedded_api_running,
                embedded_api::commands::stop_embedded_api,
                embedded_api::commands::submit_research,
                embedded_api::commands::get_run_status,
                embedded_api::commands::save_api_key,
                embedded_api::commands::get_api_key_status,
                get_recent_logs,
                clear_server_logs,
                get_server_status,
                get_recent_server_logs,
                restart_embedded_server,
            ]);
    }

    // Register commands for cloud mode
    #[cfg(all(feature = "cloud", not(feature = "desktop")))]
    {
        builder = builder.invoke_handler(tauri::generate_handler![
            // Cloud-specific commands will be added here
        ]);
    }

    builder
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
