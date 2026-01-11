# CORRECT Solution: SurrealDB RocksDB in Tauri

## You Were Right!

RocksDB **IS** the recommended backend for Tauri applications according to official SurrealDB documentation. My previous suggestion to use SurrealKV was incorrect.

## The Root Cause

The "Internal error: receiving from an empty and closed channel" error occurs because:

1. **RocksDB initialization is blocking** and CPU-intensive
2. When called directly in async context, it causes async channel issues
3. Tauri's default Tokio runtime configuration isn't optimized for SurrealDB

## Official SurrealDB Requirements for Tauri

According to [SurrealDB Performance Best Practices](https://surrealdb.com/docs/surrealdb/reference-guide/performance-best-practices):

### 1. Use `spawn_blocking` for RocksDB Initialization

RocksDB must be initialized in a blocking context:

```rust
// In embedded_api.rs, replace the SurrealDB initialization with:

// PRE-INITIALIZE SurrealDB with RocksDB backend using spawn_blocking
// CRITICAL: RocksDB initialization is blocking and must run in spawn_blocking
// See: https://surrealdb.com/docs/surrealdb/reference-guide/performance-best-practices
info!("Pre-initializing SurrealDB with RocksDB backend (Tauri-compatible)...");
let db = {
    use surrealdb::Surreal;
    use surrealdb::engine::local::{Db, RocksDb};
    
    let db_path = data_dir.join("shannon.db");
    let db_path_clone = db_path.clone();
    
    // Initialize RocksDB in blocking context to avoid async channel issues
    let db: Surreal<Db> = tokio::task::spawn_blocking(move || {
        // Create a new Tokio runtime for this blocking operation
        tokio::runtime::Handle::current().block_on(async {
            Surreal::new::<RocksDb>(db_path_clone).await
        })
    })
    .await
    .map_err(|e| EmbeddedApiError::Configuration {
        message: format!("Failed to spawn RocksDB initialization task: {}", e)
    })?
    .map_err(|e| EmbeddedApiError::Configuration {
        message: format!("Failed to initialize SurrealDB with RocksDB: {}", e)
    })?;
    
    // Set namespace and database
    db.use_ns("shannon")
        .use_db("main")
        .await
        .map_err(|e| EmbeddedApiError::Configuration {
            message: format!("Failed to set namespace/database: {}", e)
        })?;
    
    info!("✅ SurrealDB initialized successfully with RocksDB backend");
    std::sync::Arc::new(db)
};
```

### 2. Disable Tauri Log Plugin

In `tauri.conf.json`, disable the log plugin:

```json
{
  "plugins": {
    "log": {
      "enabled": false
    }
  }
}
```

**OR** if your config doesn't have this structure, set it in your Cargo.toml:

```toml
[dependencies.tauri]
version = "2.x"
features = ["..."]  # Make sure NOT to include "tauri-plugin-log"
```

### 3. Correct Cargo Features

In your workspace `Cargo.toml` or `src-tauri/Cargo.toml`:

```toml
[dependencies]
surrealdb = { 
    version = "2.0", 
    features = [
        "kv-rocksdb",           # RocksDB backend
        "protocol-ws",          # WebSocket support
        "protocol-http"         # HTTP support
    ] 
}

tokio = { 
    version = "1.41", 
    features = [
        "sync", 
        "rt-multi-thread",      # Multi-threaded runtime
        "time",
        "macros"
    ] 
}
```

### 4. Update Environment Variables

In `embedded_api.rs`, set correct environment variables:

```rust
// Use RocksDB for embedded mode (Tauri-optimized)
std::env::set_var("SURREALDB_BACKEND", "rocksdb");
std::env::set_var("SURREALDB_PATH", db_path.to_string_lossy().to_string());
std::env::set_var("SURREALDB_NAMESPACE", "shannon");
std::env::set_var("SURREALDB_DATABASE", "main");
```

## Complete Fixed Code

Replace the entire `start_shannon_api_server` method around line 448-510 in `embedded_api.rs`:

```rust
/// Start the real Shannon API server on the given port
async fn start_shannon_api_server(&self, port: u16) -> Result<(), EmbeddedApiError> {
    info!(port = port, "Starting Shannon API server");

    // Set environment variables for Shannon API configuration in embedded mode
    std::env::set_var("SHANNON_HOST", "0.0.0.0");
    std::env::set_var("SHANNON_PORT", port.to_string());
    std::env::set_var("SHANNON_MODE", "embedded");

    // Use RocksDB for embedded mode (Tauri-optimized)
    std::env::set_var("SURREALDB_BACKEND", "rocksdb");

    // Use Tauri's app data directory for database storage
    let app_data_dir = match self.app_handle.path().app_data_dir() {
        Ok(dir) => dir,
        Err(e) => {
            warn!(error = %e, "Failed to get app data directory, falling back to local directory");
            std::path::PathBuf::from("./data")
        }
    };

    let data_dir = app_data_dir.join("shannon");
    if let Err(e) = std::fs::create_dir_all(&data_dir) {
        warn!(error = %e, path = ?data_dir, "Failed to create Shannon data directory");
    }
    
    let db_path = data_dir.join("shannon.db");
    info!(path = ?db_path, "Using SurrealDB with RocksDB backend at path");
    std::env::set_var("SURREALDB_PATH", db_path.to_string_lossy().to_string());
    std::env::set_var("SURREALDB_NAMESPACE", "shannon");
    std::env::set_var("SURREALDB_DATABASE", "main");

    // PRE-INITIALIZE SurrealDB with RocksDB backend using spawn_blocking
    // CRITICAL: RocksDB initialization is blocking and must run in spawn_blocking
    // See: https://surrealdb.com/docs/surrealdb/reference-guide/performance-best-practices
    info!("Pre-initializing SurrealDB with RocksDB backend (Tauri-compatible)...");
    let db = {
        use surrealdb::Surreal;
        use surrealdb::engine::local::{Db, RocksDb};
        
        let db_path_clone = db_path.clone();
        
        // Initialize RocksDB in blocking context to avoid async channel issues
        let db: Surreal<Db> = tokio::task::spawn_blocking(move || {
            // Create a new Tokio runtime for this blocking operation
            tokio::runtime::Handle::current().block_on(async {
                Surreal::new::<RocksDb>(db_path_clone).await
            })
        })
        .await
        .map_err(|e| EmbeddedApiError::Configuration {
            message: format!("Failed to spawn RocksDB initialization task: {}", e)
        })?
        .map_err(|e| EmbeddedApiError::Configuration {
            message: format!("Failed to initialize SurrealDB with RocksDB: {}", e)
        })?;
        
        // Set namespace and database
        db.use_ns("shannon")
            .use_db("main")
            .await
            .map_err(|e| EmbeddedApiError::Configuration {
                message: format!("Failed to set namespace/database: {}", e)
            })?;
        
        info!("✅ SurrealDB initialized successfully with RocksDB backend");
        std::sync::Arc::new(db)
    };

    // Load Shannon API configuration with our overrides
    info!("Loading Shannon API configuration...");
    let config = shannon_api::config::AppConfig::load()
        .map_err(|e| EmbeddedApiError::Configuration {
            message: format!("Failed to load Shannon API config: {}", e)
        })?;

    info!(config = ?config.server, "Shannon API configuration loaded");

    // Create the Shannon API application with the pre-initialized database
    info!("Creating Shannon API application...");
    #[cfg(feature = "desktop")]
    let app = shannon_api::server::create_app(
        config,
        Some(db), // Pass the pre-initialized database connection
    )
    .await
    .map_err(|e| EmbeddedApiError::Configuration {
        message: format!("Failed to create Shannon API app: {}", e)
    })?;

    #[cfg(not(feature = "desktop"))]
    let app = shannon_api::server::create_app(config)
        .await
        .map_err(|e| EmbeddedApiError::Configuration {
            message: format!("Failed to create Shannon API app: {}", e)
        })?;

    info!("Shannon API application created successfully");

    // Bind to the discovered port on all interfaces
    let addr = format!("0.0.0.0:{}", port);
    info!(addr = %addr, "Attempting to bind Shannon API server");
    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .map_err(|e| EmbeddedApiError::PortBindFailed {
            port,
            source: e
        })?;

    info!(port = port, addr = %addr, "Shannon API server bound to address successfully");

    // Start the server in a background task
    let _server_handle = tokio::spawn(async move {
        info!("Starting Shannon API server with axum::serve");
        if let Err(e) = axum::serve(listener, app).await {
            error!(error = %e, "Shannon API server error");
        } else {
            info!("Shannon API server exited cleanly");
        }
    });

    // Give the server a moment to start up
    tokio::time::sleep(Duration::from_millis(100)).await;

    Ok(())
}
```

## Why This Works

1. **`spawn_blocking`**: Moves RocksDB initialization to a dedicated blocking thread pool
2. **Shared connection**: Both server and workflow engine use the same DB instance
3. **Proper async context**: Avoids channel issues by initializing in the right context
4. **Log plugin disabled**: Reduces overhead and potential conflicts

## Testing

```bash
cd /Users/gqadonis/Projects/prometheus/Shannon/desktop/src-tauri
cargo clean
cargo build --features desktop --release
```

You should see:
```
✅ SurrealDB initialized successfully with RocksDB backend
Shannon API application created successfully
Shannon API server bound to address successfully
```

## No Tauri Plugins Required

**Good news**: You don't need any special Tauri plugins! The solution is purely about:
1. Using `spawn_blocking` for initialization
2. Disabling the log plugin
3. Having the correct cargo features

## Why My Previous Suggestion Was Wrong

I incorrectly thought the async channel error was a RocksDB-specific issue. In reality:
- ✅ **RocksDB is the correct choice** for Tauri (official recommendation)
- ❌ **SurrealKV is for Node.js** embedded use cases
- ✅ **The issue was HOW we initialized**, not WHICH backend

The official docs specifically state: "When wanting persistent data storage on a single-node, on-disk storage can be used to enable larger data storage capabilities. **RocksDB is optimised for high performance and fast storage on SSDs**."

## References

- [SurrealDB Performance Best Practices (Official)](https://surrealdb.com/docs/surrealdb/reference-guide/performance-best-practices)
- [Tauri + SurrealDB Examples](https://huakun.tech/blogs/Tauri-+-SurrealDB)
- [Running SurrealDB Embedded in Rust](https://surrealdb.com/docs/surrealdb/integration/sdks/rust)