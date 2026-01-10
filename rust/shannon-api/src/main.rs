//! Shannon API - Main Entry Point
//!
//! A unified high-performance Rust API for the Shannon AI platform,
//! combining Gateway and LLM service functionality.

use clap::Parser;
use mimalloc::MiMalloc;
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt, EnvFilter};

use shannon_api::config::AppConfig;
use shannon_api::server::create_app;

// Use mimalloc for better performance
#[global_allocator]
static GLOBAL: MiMalloc = MiMalloc;

/// Command-line arguments.
#[derive(Parser, Debug)]
#[command(name = "shannon-api")]
#[command(about = "Shannon API - Unified Rust Gateway and LLM Service")]
#[command(version)]
struct Args {
    /// Host to bind to.
    #[arg(long, env = "SHANNON_API_HOST", default_value = "0.0.0.0")]
    host: String,

    /// Port to listen on.
    #[arg(short, long, env = "SHANNON_API_PORT", default_value = "8080")]
    port: u16,

    /// Admin/metrics port.
    #[arg(long, env = "SHANNON_API_ADMIN_PORT", default_value = "8081")]
    admin_port: u16,

    /// Log level.
    #[arg(long, env = "RUST_LOG", default_value = "info")]
    log_level: String,

    /// Config file path.
    #[arg(short, long, env = "SHANNON_API_CONFIG")]
    config: Option<String>,

    /// Enable embedded mode (for Tauri integration).
    #[arg(long, env = "SHANNON_EMBEDDED_MODE")]
    embedded: bool,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    // Parse command-line arguments
    let args = Args::parse();

    // Initialize tracing
    init_tracing(&args.log_level);

    tracing::info!(
        "Starting Shannon API v{} (unified Gateway + LLM Service)",
        env!("CARGO_PKG_VERSION")
    );

    // Load configuration
    let config = AppConfig::load()?;
    tracing::info!("Configuration loaded");

    // Create the application
    let app = create_app(
        config,
        #[cfg(feature = "embedded")]
        None, // No existing database connection in standalone mode
    )
    .await?;
    tracing::info!("Application initialized");

    // Determine bind address based on mode
    let addr = if args.embedded {
        format!("127.0.0.1:{}", args.port)
    } else {
        format!("{}:{}", args.host, args.port)
    };

    let listener = tokio::net::TcpListener::bind(&addr).await?;
    tracing::info!("Listening on {}", addr);

    if args.embedded {
        tracing::info!("Running in embedded mode (Tauri compatible)");
    }

    // Run the server
    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal())
        .await?;

    tracing::info!("Server shut down gracefully");
    Ok(())
}

/// Initialize tracing/logging.
fn init_tracing(log_level: &str) {
    let filter = EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new(log_level));

    tracing_subscriber::registry()
        .with(filter)
        .with(tracing_subscriber::fmt::layer())
        .init();
}

/// Graceful shutdown signal handler.
async fn shutdown_signal() {
    let ctrl_c = async {
        tokio::signal::ctrl_c()
            .await
            .expect("Failed to install Ctrl+C handler");
    };

    #[cfg(unix)]
    let terminate = async {
        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
            .expect("Failed to install signal handler")
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {
            tracing::info!("Received Ctrl+C, shutting down...");
        }
        _ = terminate => {
            tracing::info!("Received SIGTERM, shutting down...");
        }
    }
}
