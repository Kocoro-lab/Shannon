# Embedded API Startup Flow - Axum Server in Tauri

This document explains how the Axum web server starts within the Shannon Tauri desktop application, providing a local-first embedded API.

## 📋 Table of Contents

1. [Overview](#overview)
2. [Startup Flow](#startup-flow)
3. [Key Components](#key-components)
4. [Code Walkthrough](#code-walkthrough)
5. [Configuration](#configuration)
6. [Port Management](#port-management)
7. [Lifecycle Events](#lifecycle-events)
8. [Debugging](#debugging)

---

## Overview

The Shannon desktop application embeds a full Axum-based HTTP server that provides the Shannon API locally. This enables:

- **Local-first operation** - Works offline with SQLite in embedded mode
- **Zero external dependencies** - No remote API required
- **Native performance** - Rust-powered API server
- **Deterministic port fallback** - Scans ports 1906-1915 in order
- **Graceful lifecycle** - Proper startup, ready signaling, and shutdown

**Architecture:**
```
Tauri Desktop App
├── Frontend (Next.js)
│   └── Connects to http://127.0.0.1:{selected_port}
└── Backend (Rust)
    ├── Tauri Window Manager
    └── Embedded Axum Server
        ├── Shannon API (routes, handlers)
        ├── SQLite (local database)
        └── IPC Communication
```

---

## Startup Flow

### High-Level Sequence

```
1. Tauri App Initialization (lib.rs)
   │
   ├─→ 2. Setup Hook (desktop feature enabled)
   │    │
   │    ├─→ 3. Initialize IPC Logger
   │    │
   │    ├─→ 4. Create Data Directory
   │    │
   │    ├─→ 5. Spawn Server Thread
   │    │    │
   │    │    ├─→ 6. Create Tokio Runtime
   │    │    │
   │    │    └─→ 7. Call start_embedded_api()
   │    │         │
   │    │         ├─→ 8. Load Shannon Config
   │    │         │
   │    │         ├─→ 9. Create Axum App
   │    │         │
   │    │         ├─→ 10. Bind TCP Listener (port 0)
   │    │         │
   │    │         ├─→ 11. Get OS-Assigned Port
   │    │         │
   │    │         ├─→ 12. Emit "server-ready" Event
   │    │         │
   │    │         └─→ 13. Start Axum Server
   │    │              │
   │    │              └─→ 14. Run with Graceful Shutdown
   │    │
   │    └─→ 15. Wait for Ready Signal (5s timeout)
   │
   └─→ 16. Frontend Connects to Embedded API
```

### Detailed Flow

1. **Application Start** (`src/main.rs`)
   ```rust
   fn main() {
       app_lib::run();
   }
   ```

2. **Tauri Builder Setup** (`src/lib.rs:122-135`)
   ```rust
   pub fn run() {
       let mut builder = tauri::Builder::default()
           .plugin(tauri_plugin_shell::init())
           .plugin(tauri_plugin_dialog::init())
           .plugin(tauri_plugin_store::Builder::default().build());
       
       #[cfg(feature = "desktop")]
       let embedded_state = TauriEmbeddedState::new();
       
       builder = builder.setup(move |app| {
           // Setup logic here...
       });
   }
   ```

3. **Desktop Mode Detection** (`src/lib.rs:154-207`)
   ```rust
   #[cfg(feature = "desktop")]
   {
       log::info!("Desktop mode enabled, starting embedded Shannon API...");
       
       // Get app data directory
       let app_data_dir = app.path().app_data_dir()?;
       
       // Spawn server in background thread
       std::thread::spawn(move || {
           let rt = tokio::runtime::Runtime::new()?;
           rt.block_on(async move {
               match start_embedded_api(Some(0), app_handle, ipc_logger).await {
                   Ok(handle) => {
                       log::info!("✅ Embedded API started on port {}", handle.port());
                       state_clone.set_handle(handle);
                   }
                   Err(e) => log::error!("❌ Failed to start: {}", e),
               }
           });
       });
   }
   ```

4. **Server Initialization** (`src/embedded_api.rs:208-460`)
   ```rust
   pub async fn start_embedded_api(
       port: Option<u16>,
       app_handle: tauri::AppHandle,
       ipc_logger: IpcLogger,
   ) -> Result<EmbeddedApiHandle, String> {
       // Set environment variables
       std::env::set_var("SHANNON_MODE", "embedded");
       std::env::set_var("WORKFLOW_ENGINE", "durable");
       std::env::set_var("DATABASE_DRIVER", "surrealdb");
       
       // Load configuration
       let config = AppConfig::load()?;
       
       // Create Shannon API application
       let app = shannon_api::server::create_app(config).await?;
       
       // Bind to port 0 (OS chooses available port)
       let listener = tokio::net::TcpListener::bind("0.0.0.0:0").await?;
       let bound_port = listener.local_addr()?.port();
       
       // Emit ready event to frontend
       app_handle.emit("server-ready", json!({
           "url": format!("http://127.0.0.1:{}", bound_port),
           "port": bound_port
       }))?;
       
       // Start Axum server
       let server = axum::serve(listener, app);
       server.with_graceful_shutdown(shutdown_signal).await?;
       
       Ok(EmbeddedApiHandle { should_run, port: bound_port })
   }
   ```

---

## Key Components

### 1. `lib.rs` - Application Entry Point

**Location:** `src-tauri/src/lib.rs`

**Responsibilities:**
- Initialize Tauri application
- Detect desktop/mobile mode via feature flags
- Spawn background thread for server
- Create Tokio runtime for async operations
- Wait for server ready signal

**Key Code:**
```rust
#[cfg(feature = "desktop")]
{
    let (ready_tx, ready_rx) = std::sync::mpsc::channel::<bool>();
    
    std::thread::spawn(move || {
        let rt = tokio::runtime::Runtime::new().unwrap();
        rt.block_on(async move {
            match embedded_api::start_embedded_api(Some(0), app_handle, ipc_logger).await {
                Ok(handle) => {
                    ready_tx.send(true);
                    state_clone.set_handle(handle);
                }
                Err(e) => {
                    log::error!("Failed to start: {}", e);
                    ready_tx.send(false);
                }
            }
        });
    });
    
    // Wait up to 5 seconds for ready signal
    ready_rx.recv_timeout(Duration::from_secs(5))?;
}
```

### 2. `embedded_api.rs` - Server Implementation

**Location:** `src-tauri/src/embedded_api.rs`

**Responsibilities:**
- Load Shannon API configuration
- Create Axum application with routes
- Bind TCP listener to dynamic port
- Emit lifecycle events via IPC
- Manage graceful shutdown

**Key Structures:**

```rust
/// State of the embedded API server
pub enum EmbeddedApiState {
    Stopped,
    Starting,
    Running,
    ShuttingDown,
    Failed,
}

/// Handle to control the embedded server
pub struct EmbeddedApiHandle {
    pub should_run: Arc<AtomicBool>,
    port: u16,
}

/// Embedded state with database connection
pub struct EmbeddedState {
    pub db: Arc<Surreal<surrealdb::engine::local::Db>>,
    pub data_dir: PathBuf,
    pub api_handle: Option<EmbeddedApiHandle>,
    pub state: EmbeddedApiState,
}
```

### 3. `shannon_api::server` - Shannon API Core

**Location:** `rust/shannon-api/src/server.rs` (in main Shannon codebase)

**Responsibilities:**
- Define API routes (tasks, workflows, models)
- Create Axum router with middleware
- Configure authentication and CORS
- Set up request/response handlers

**Integration:**
```rust
use shannon_api::server::create_app;

let config = AppConfig::load()?;
let app = create_app(config).await?;
```

### 4. IPC Logger - Communication Layer

**Location:** `src-tauri/src/ipc_logger.rs`

**Responsibilities:**
- Send log messages to frontend
- Emit lifecycle state changes
- Provide structured logging
- Enable real-time monitoring

**Usage:**
```rust
ipc_logger.log(
    LogLevel::Info,
    Component::EmbeddedApi,
    "🚀 Starting embedded Shannon API"
);

ipc_logger.state_change(
    Some(LifecyclePhase::Starting),
    LifecyclePhase::Ready,
    Some(format!("Server ready on port {}", port)),
    Some(port),
);
```

---

## Code Walkthrough

### Step 1: Application Initialization

**File:** `src-tauri/src/lib.rs`

```rust
pub fn run() {
    #[allow(unused_mut)]
    let mut builder = tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_store::Builder::default().build());
    
    // Create embedded state early
    #[cfg(feature = "desktop")]
    let embedded_state = TauriEmbeddedState::new();
    #[cfg(feature = "desktop")]
    let embedded_state_for_setup = embedded_state.clone();
    
    builder = builder.setup(move |app| {
        // Setup hook runs here...
    });
}
```

**What happens:**
- Tauri builder is created with plugins
- Embedded state is instantiated (when desktop feature enabled)
- Setup hook is registered

### Step 2: Setup Hook Execution

**File:** `src-tauri/src/lib.rs:136-153`

```rust
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
    let ipc_logger = IpcLogger::new(app.handle().clone());
    
    #[cfg(feature = "desktop")]
    {
        app.manage(ipc_logger.clone());
    }
    
    // Desktop mode: Start embedded API
    #[cfg(feature = "desktop")]
    {
        // ... server startup code ...
    }
    
    Ok(())
});
```

**What happens:**
- Logging is configured (debug mode only)
- IPC logger is created for frontend communication
- Desktop-specific initialization begins

### Step 3: Data Directory Setup

**File:** `src-tauri/src/lib.rs:157-169`

```rust
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
}
```

**What happens:**
- Retrieves platform-specific app data directory
  - macOS: `~/Library/Application Support/ai.prometheusags.planet/`
  - Linux: `~/.local/share/ai.prometheusags.planet/`
  - Windows: `C:\Users\{user}\AppData\Roaming\ai.prometheusags.planet\`
- Creates directory if it doesn't exist
- This is where SurrealDB data files will be stored

### Step 4: Server Thread Spawn

**File:** `src-tauri/src/lib.rs:171-200`

```rust
// Wait for server to be ready (via event)
let (ready_tx_thread, ready_rx_thread) = std::sync::mpsc::channel::<bool>();
let state_clone = embedded_state_for_setup.clone();

// Start the server in a background thread with dynamic port (0 = OS chooses)
let app_handle_for_thread = app.handle().clone();
let ipc_logger_for_thread = ipc_logger.clone();

std::thread::spawn(move || {
    let rt = tokio::runtime::Runtime::new()
        .expect("Failed to create tokio runtime");
    
    rt.block_on(async move {
         log::info!("🚀 Starting embedded Shannon API with dynamic port...");
         
         // Use port 0 to let OS assign an available port
         match embedded_api::start_embedded_api(Some(0), app_handle_for_thread, ipc_logger_for_thread).await {
             Ok(handle) => {
                  log::info!("✅ Embedded API started on port {}", handle.port());
                  let _ = ready_tx_thread.send(true);
                  state_clone.set_handle(handle);
             },
             Err(e) => {
                 log::error!("❌ Failed to start embedded API: {}", e);
                 let _ = ready_tx_thread.send(false);
             }
         }
    });
});

// Wait for thread to confirm dispatch
match ready_rx_thread.recv_timeout(std::time::Duration::from_secs(5)) {
    Ok(true) => log::info!("Embedded API startup sequence initiated"),
    _ => log::error!("Embedded API startup failed to initiate"),
}
```

**What happens:**
- Creates a channel for ready signaling
- Spawns OS thread for server (Tauri requires non-async setup)
- Creates Tokio runtime within thread
- Calls `start_embedded_api()` with port 0 (dynamic allocation)
- Main thread waits up to 5 seconds for ready signal
- State is updated with server handle

### Step 5: Server Configuration

**File:** `src-tauri/src/embedded_api.rs:208-280`

```rust
pub async fn start_embedded_api(
    port: Option<u16>,
    app_handle: tauri::AppHandle,
    ipc_logger: crate::ipc_logger::IpcLogger,
) -> Result<EmbeddedApiHandle, String> {
    use crate::ipc_events::{Component, LifecyclePhase, LogLevel};
    use shannon_api::config::AppConfig;
    use tauri::Emitter;
    use tokio::sync::Mutex;

    let requested_port = port.unwrap_or(DEFAULT_EMBEDDED_PORT);
    
    // Use Arc<Mutex> to share the actual bound port between tasks
    let actual_port = Arc::new(Mutex::new(requested_port));
    let actual_port_clone = actual_port.clone();
    
    // Create handle initially with requested port
    let should_run = Arc::new(AtomicBool::new(true));
    let should_run_clone = should_run.clone();

    // Clone IPC logger for the background task
    let ipc_logger_clone = ipc_logger.clone();

    // Spawn the server in a background task
    tokio::spawn(async move {
        // Log starting state
        ipc_logger_clone.state_change(
            Some(LifecyclePhase::Initializing),
            LifecyclePhase::Starting,
            Some(format!("Starting embedded Shannon API on port {}", requested_port)),
            Some(requested_port),
        );

        ipc_logger_clone.log(
            LogLevel::Info,
            Component::EmbeddedApi,
            format!("🚀 Starting embedded Shannon API (requested port: {})", requested_port),
        );

        // Load configuration with embedded mode defaults
        ipc_logger_clone.log(
            LogLevel::Debug,
            Component::EmbeddedApi,
            "Setting embedded mode environment variables",
        );
        
        std::env::set_var("SHANNON_MODE", "embedded");
        std::env::set_var("WORKFLOW_ENGINE", "durable");
        std::env::set_var("DATABASE_DRIVER", "surrealdb");

        ipc_logger_clone.log(
            LogLevel::Info,
            Component::EmbeddedApi,
            "Loading configuration...",
        );

        let config = match AppConfig::load() {
            Ok(c) => {
                ipc_logger_clone.log(
                    LogLevel::Info,
                    Component::EmbeddedApi,
                    format!("✅ Configuration loaded (mode: {})", c.deployment.mode),
                );
                c
            }
            Err(e) => {
                ipc_logger_clone.log(
                    LogLevel::Error,
                    Component::EmbeddedApi,
                    format!("❌ Failed to load config: {}", e),
                );
                ipc_logger_clone.state_change(
                    Some(LifecyclePhase::Starting),
                    LifecyclePhase::Failed,
                    Some(format!("Config load failed: {}", e)),
                    None,
                );
                return;
            }
        };
        // ... continues ...
    });
}
```

**What happens:**
- Sets up shared state for port tracking
- Creates atomic bool for shutdown signaling
- Spawns Tokio task for async server operations
- Logs state change to "Starting"
- Sets environment variables for embedded mode
- Loads Shannon API configuration from files

### Step 6: Axum App Creation

**File:** `src-tauri/src/embedded_api.rs:282-310`

```rust
// Create the application
ipc_logger_clone.log(
    LogLevel::Info,
    Component::EmbeddedApi,
    "Creating Shannon API application...",
);

let start_time = std::time::Instant::now();
let app = match shannon_api::server::create_app(config).await {
    Ok(a) => {
        let duration_ms = start_time.elapsed().as_millis();
        ipc_logger_clone.log(
            LogLevel::Info,
            Component::EmbeddedApi,
            format!("✅ Application created successfully ({}ms)", duration_ms),
        );
        a
    }
    Err(e) => {
        ipc_logger_clone.log(
            LogLevel::Error,
            Component::EmbeddedApi,
            format!("❌ Failed to create app: {}", e),
        );
        ipc_logger_clone.state_change(
            Some(LifecyclePhase::Starting),
            LifecyclePhase::Failed,
            Some(format!("App creation failed: {}", e)),
            None,
        );
        return;
    }
};
```

**What happens:**
- Calls `shannon_api::server::create_app()` from core Shannon API
- This creates the Axum router with all routes:
  - `/v1/tasks` - Task submission and management
  - `/v1/workflows` - Workflow execution
  - `/v1/models` - Model information
  - `/health` - Health check endpoint
- Measures and logs initialization time
- Returns early on failure with error logging

### Step 7: TCP Listener Binding

**File:** `src-tauri/src/embedded_api.rs:312-355`

```rust
// Bind to localhost or 0.0.0.0 based on passed port
// If port is 0, OS picks a free port
let addr = format!("0.0.0.0:{requested_port}");
ipc_logger_clone.log(
    LogLevel::Info,
    Component::EmbeddedApi,
    format!("Binding server to {}...", addr),
);

let listener = match tokio::net::TcpListener::bind(&addr).await {
    Ok(l) => {
        ipc_logger_clone.log(
            LogLevel::Info,
            Component::EmbeddedApi,
            format!("✅ Server bound to {}", addr),
        );
        l
    }
    Err(e) => {
        ipc_logger_clone.log(
            LogLevel::Error,
            Component::EmbeddedApi,
            format!("❌ Failed to bind to {}: {}", addr, e),
        );
        ipc_logger_clone.state_change(
            Some(LifecyclePhase::Starting),
            LifecyclePhase::Failed,
            Some(format!("Bind failed: {}", e)),
            None,
        );
        return;
    }
};

// Get the actual port assigned by OS
let local_addr = match listener.local_addr() {
    Ok(a) => a,
    Err(e) => {
        ipc_logger_clone.log(
            LogLevel::Error,
            Component::EmbeddedApi,
            format!("❌ Failed to get local addr: {}", e),
        );
        return;
    }
};
let bound_port = local_addr.port();

ipc_logger_clone.log(
    LogLevel::Info,
    Component::EmbeddedApi,
    format!("🎉 Embedded Shannon API listening on {}", local_addr),
);

// Update the shared actual port
*actual_port_clone.lock().await = bound_port;
```

**What happens:**
- Binds TCP listener to `0.0.0.0:0` (all interfaces, dynamic port)
- Port 0 tells OS to assign any available port automatically
- Retrieves the actual assigned port from listener
- Updates shared state with actual port
- Logs success with actual bound address

### Step 8: Ready Event Emission

**File:** `src-tauri/src/embedded_api.rs:357-397`

```rust
// Log state change to Ready
ipc_logger_clone.state_change(
    Some(LifecyclePhase::Starting),
    LifecyclePhase::Ready,
    Some(format!("Server ready on port {}", bound_port)),
    Some(bound_port),
);

// Emit ready event to Tauri frontend with actual bound port
let _ = app_handle.emit("server-ready", serde_json::json!({
    "url": format!("http://127.0.0.1:{}", bound_port),
    "port": bound_port
}));

ipc_logger_clone.log(
    LogLevel::Info,
    Component::EmbeddedApi,
    format!("✅ Server ready event emitted (port: {})", bound_port),
);

// Also emit a legacy event just in case
let _ = app_handle.emit("embedded-api-ready", bound_port);
```

**What happens:**
- Logs state transition to "Ready"
- Emits `server-ready` event to frontend with:
  - Full URL: `http://127.0.0.1:{port}`
  - Port number
- Frontend listens for this event to know where to connect
- Also emits legacy event for backward compatibility

### Step 9: Axum Server Start

**File:** `src-tauri/src/embedded_api.rs:399-441`

```rust
// Run the server with graceful shutdown
let server = axum::serve(listener, app);

// Create shutdown signal based on should_run flag
let shutdown_logger = ipc_logger_clone.clone();
let shutdown_signal = async move {
    while should_run_clone.load(Ordering::SeqCst) {
        tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
    }
    shutdown_logger.log(
        LogLevel::Info,
        Component::EmbeddedApi,
        "Embedded API received shutdown signal",
    );
    shutdown_logger.state_change(
        Some(LifecyclePhase::Ready),
        LifecyclePhase::ShuttingDown,
        Some("Graceful shutdown initiated"),
        Some(bound_port),
    );
};

if let Err(e) = server.with_graceful_shutdown(shutdown_signal).await {
    ipc_logger_clone.log(
        LogLevel::Error,
        Component::EmbeddedApi,
        format!("❌ Embedded API server error: {}", e),
    );
}

ipc_logger_clone.log(
    LogLevel::Info,
    Component::EmbeddedApi,
    "Embedded Shannon API stopped",
);
ipc_logger_clone.state_change(
    Some(LifecyclePhase::ShuttingDown),
    LifecyclePhase::Stopped,
    Some("Server stopped"),
    None,
);
```

**What happens:**
- Creates Axum server with listener and app
- Sets up graceful shutdown future that:
  - Polls `should_run` flag every 100ms
  - Triggers shutdown when flag is set to false
  - Logs shutdown initiation
- Starts server with `.await` (blocks until shutdown)
- Logs final state change to "Stopped"

### Step 10: Handle Return

**File:** `src-tauri/src/embedded_api.rs:443-460`

```rust
    }); // End of tokio::spawn
    
    // Wait briefly for the server to bind and update the port
    tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
    
    // Get the actual bound port
    let final_port = *actual_port.lock().await;
    
    // Create handle with actual port
    let handle = EmbeddedApiHandle {
        should_run,
        port: final_port,
    };
    
    Ok(handle)
}
```

**What happens:**
- After spawning server task, wait 100ms for binding
- Retrieve actual port from shared state
- Create handle with:
  - `should_run` - AtomicBool for shutdown control
  - `port` - Actual OS-assigned port
- Return handle to caller

---

## Configuration

### Environment Variables

Set automatically by `start_embedded_api()`:

```rust
std::env::set_var("SHANNON_MODE", "embedded");
std::env::set_var("WORKFLOW_ENGINE", "durable");
std::env::set_var("DATABASE_DRIVER", "surrealdb");
```

**Purpose:**
- `SHANNON_MODE=embedded` - Enables local-first features
- `WORKFLOW_ENGINE=durable` - Use durable workflows (no external Temporal)
- `DATABASE_DRIVER=surrealdb` - Use embedded SurrealDB instead of PostgreSQL

### Shannon API Configuration

Loaded from standard config files:

```yaml
# config/shannon.yaml (or loaded from embedded resources)
deployment:
  mode: embedded
  
database:
  driver: surrealdb
  path: "{app_data_dir}/surrealdb"
  
workflow:
  engine: durable
  
server:
  host: 0.0.0.0
  port: 0  # Dynamic allocation
```

### Feature Flags

**Cargo.toml:**
```toml
[features]
default = ["desktop"]
desktop = [
    "dep:shannon-api",
    "dep:tokio",
    "dep:surrealdb",
    "dep:axum",
]
mobile = [
    "dep:shannon-api",
    "dep:tokio",
    "dep:rusqlite",
]
```

**Build commands:**
```bash
# Desktop mode (default) - Axum + SurrealDB
cargo build --features desktop

# Mobile mode - Lighter weight SQLite
cargo build --features mobile

# No embedded features
cargo build --no-default-features
```

---

## Port Management

### Deterministic Port Fallback

**Why a fixed range?**
- Predictable local discovery for the renderer
- Clear fallback behavior when the default port is busy
- Avoids missed IPC events by enabling proactive polling

**How it works:**
1. Attempt port `1906`
2. If unavailable, try `1907` through `1915` in order
3. Emit `server-port-selected` once a port is chosen
4. Emit `server-ready` after readiness checks pass

**Example:**
```rust
let port = select_available_port(1906..=1915, Some(1906)).await?;
let base_url = format!("http://127.0.0.1:{}", port);

app_handle.emit("server-port-selected", json!({
    "url": base_url,
    "port": port
}));
```

### Frontend Connection

**Frontend code** (typically in `lib/tauri-api.ts`):

```typescript
import { listen } from '@tauri-apps/api/event';

let apiBaseUrl = 'http://127.0.0.1:8080'; // Default fallback

// Listen for server-ready event
listen('server-ready', (event) => {
  const { url, port } = event.payload;
  apiBaseUrl = url;
  console.log(`Embedded API ready at ${url}`);
});

// Make requests to embedded API
async function callApi(endpoint: string) {
  const response = await fetch(`${apiBaseUrl}${endpoint}`);
  return response.json();
}
```

---

## Lifecycle Events

### IPC Event Flow

```
Initializing → Starting → Ready → Running
                   ↓
                Failed
                   
Ready → ShuttingDown → Stopped
```

### Event Structure

**State Change Event:**
```typescript
interface StateChangeEvent {
  from_phase?: LifecyclePhase;
  to_phase: LifecyclePhase;
  message?: string;
  port?: number;
  timestamp: string;
}
```

**Log Event:**
```typescript
interface LogEvent {
  level: 'Debug' | 'Info' | 'Warn' | 'Error';
  component: 'EmbeddedApi' | 'Database' | 'Workflow';
  message: string;
  timestamp: string;
}
```

### Frontend Listeners

```typescript
import { listen } from '@tauri-apps/api/event';

// Listen for server ready
listen('server-ready', (event) => {
  const { url, port } = event.payload;
  console.log(`Server ready at ${url}`);
  // Update API client configuration
  setApiBaseUrl(url);
});

// Listen for lifecycle changes
listen('lifecycle-state-change', (event) => {
  const { to_phase, message, port } = event.payload;
  console.log(`Lifecycle: ${to_phase} - ${message}`);
  updateServerStatus(to_phase);
});

// Listen for log messages
listen('ipc-log', (event) => {
  const { level, component, message } = event.payload;
  console.log(`[${level}] ${component}: ${message}`);
  appendToDebugConsole(event.payload);
});
```

---

## Debugging

### Viewing Server Logs

**Terminal output:**
```bash
# Run with logging
RUST_LOG=debug npm run tauri:dev

# Or use Zed task: "tauri: dev (with logging)"
```

**Expected output:**
```
[INFO] Desktop mode enabled, starting embedded Shannon API...
[INFO] Data directory: /Users/{user}/Library/Application Support/ai.prometheusags.planet
[INFO] 🚀 Starting embedded Shannon API with dynamic port...
[INFO] Setting embedde
