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
        .setup(|app| {
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
                log::info!("Embedded Shannon API will be available at http://127.0.0.1:8765");

                // Note: The actual API server is started asynchronously
                // The frontend should poll is_embedded_api_running() before making requests
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
            .manage(TauriEmbeddedState::new())
            .invoke_handler(tauri::generate_handler![
                embedded_api::commands::get_embedded_api_url,
                embedded_api::commands::is_embedded_api_running,
                embedded_api::commands::stop_embedded_api,
                embedded_api::commands::submit_research,
                embedded_api::commands::get_run_status,
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
