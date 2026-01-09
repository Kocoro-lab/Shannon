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

#[cfg(feature = "desktop")]
use embedded_api::commands::TauriEmbeddedState;

use tauri::{
    menu::{Menu, MenuItem, PredefinedMenuItem, Submenu},
    App, Manager,
};

/// Store key for persisted API keys
#[cfg(feature = "desktop")]
const STORE_KEY_OPENAI: &str = "openai_api_key";
#[cfg(feature = "desktop")]
const STORE_KEY_ANTHROPIC: &str = "anthropic_api_key";
#[cfg(feature = "desktop")]
const SETTINGS_STORE: &str = "settings.json";

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
                
                // Use a channel to signal when the server is ready
                let (ready_tx, ready_rx) = std::sync::mpsc::channel::<bool>();
                
                let state_clone = embedded_state_for_setup.clone();
                let data_dir = app_data_dir.clone();
                
                // Start the server in a background thread
                std::thread::spawn(move || {
                    let rt = tokio::runtime::Runtime::new()
                        .expect("Failed to create tokio runtime");
                    
                    rt.block_on(async move {
                        // Set environment for embedded mode FIRST
                        std::env::set_var("SHANNON_MODE", "embedded");
                        std::env::set_var("WORKFLOW_ENGINE", "durable");
                        std::env::set_var("DATABASE_DRIVER", "surrealdb");
                        std::env::set_var("SURREALDB_PATH", data_dir.join("shannon.db").to_string_lossy().to_string());
                        
                        // Check for API key - if not set, use a placeholder
                        // The Settings UI will allow users to configure this
                        if std::env::var("OPENAI_API_KEY").is_err() && 
                           std::env::var("ANTHROPIC_API_KEY").is_err() {
                            log::warn!("No LLM API key found. Set via Settings in the app.");
                            // Set a placeholder so config loading doesn't fail
                            std::env::set_var("OPENAI_API_KEY", "CONFIGURE_IN_SETTINGS");
                        }
                        
                        log::info!("Loading configuration...");
                        
                        // Load configuration without strict validation for embedded mode
                        let config = match shannon_api::config::AppConfig::load_unchecked() {
                            Ok(c) => c,
                            Err(e) => {
                                log::error!("Failed to load config: {}", e);
                                let _ = ready_tx.send(false);
                                return;
                            }
                        };
                        
                        log::info!("Creating embedded API server...");
                        
                        // Create the application
                        let app = match shannon_api::server::create_app(config).await {
                            Ok(a) => a,
                            Err(e) => {
                                log::error!("Failed to create app: {}", e);
                                let _ = ready_tx.send(false);
                                return;
                            }
                        };
                        
                        // Bind to localhost
                        let addr = "127.0.0.1:8765";
                        let listener = match tokio::net::TcpListener::bind(addr).await {
                            Ok(l) => l,
                            Err(e) => {
                                log::error!("Failed to bind to {}: {}", addr, e);
                                let _ = ready_tx.send(false);
                                return;
                            }
                        };
                        
                        // Create and store the handle
                        let handle = embedded_api::EmbeddedApiHandle::with_port(8765);
                        handle.should_run.store(true, std::sync::atomic::Ordering::SeqCst);
                        state_clone.set_handle(handle.clone());
                        
                        log::info!("Embedded Shannon API listening on {}", addr);
                        
                        // Signal that we're ready
                        let _ = ready_tx.send(true);
                        
                        // Run the server (this blocks until shutdown)
                        if let Err(e) = axum::serve(listener, app).await {
                            log::error!("Embedded API server error: {}", e);
                        }
                    });
                });
                
                // Wait for server to be ready (with timeout)
                log::info!("Waiting for embedded API to be ready...");
                match ready_rx.recv_timeout(std::time::Duration::from_secs(30)) {
                    Ok(true) => {
                        log::info!("Embedded API is ready!");
                    }
                    Ok(false) => {
                        log::error!("Embedded API failed to start");
                    }
                    Err(_) => {
                        log::error!("Timeout waiting for embedded API to start");
                    }
                }
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
