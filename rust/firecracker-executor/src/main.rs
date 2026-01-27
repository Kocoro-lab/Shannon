use anyhow::Result;
use tracing::info;
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

#[tokio::main]
async fn main() -> Result<()> {
    // Initialize tracing
    tracing_subscriber::registry()
        .with(tracing_subscriber::EnvFilter::new(
            std::env::var("RUST_LOG").unwrap_or_else(|_| "info".into()),
        ))
        .with(tracing_subscriber::fmt::layer())
        .init();

    info!("Firecracker Executor Service starting...");
    info!("NOTE: Full implementation requires bare-metal/EC2 host with KVM");

    // TODO: Initialize VM pool manager
    // TODO: Start gRPC server

    info!("Firecracker Executor Service is a stub - implement on KVM-enabled host");

    Ok(())
}
