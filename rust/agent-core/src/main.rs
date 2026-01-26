use shannon_agent_core::tracing as trace_mod;

use anyhow::Result;
use tonic::transport::Server;
use tracing::info;

use shannon_agent_core::grpc_server::proto::agent::agent_service_server::AgentServiceServer;
use shannon_agent_core::grpc_server::AgentServiceImpl;
use shannon_agent_core::sandbox_service::SandboxServiceImpl;

#[tokio::main]
async fn main() -> Result<()> {
    // Initialize OpenTelemetry tracing
    if let Err(e) = trace_mod::init_tracing() {
        eprintln!("Failed to initialize tracing: {}", e);
        // Fall back to basic logging
    }

    info!("Starting Shannon Agent Core service");

    // Load configuration and get metrics port
    let config = shannon_agent_core::config::Config::global().unwrap_or_default();
    let metrics_port = config.metrics.port;

    // Start metrics server
    tokio::spawn(async move {
        if let Err(e) = shannon_agent_core::metrics::start_metrics_server(metrics_port).await {
            tracing::error!("Failed to start metrics server: {}", e);
        }
    });

    let addr = "0.0.0.0:50051".parse()?;
    let agent_service = AgentServiceImpl::new()?;

    // Initialize sandbox service (uses SHANNON_SESSION_WORKSPACES_DIR env var)
    let sandbox_service = SandboxServiceImpl::from_env();
    info!("Sandbox service initialized with workspace manager");

    // Build reflection service
    let reflection_service = tonic_reflection::server::Builder::configure()
        .register_encoded_file_descriptor_set(
            shannon_agent_core::grpc_server::proto::FILE_DESCRIPTOR_SET,
        )
        .build_v1()
        .unwrap();

    info!("Agent Core listening on {} with reflection enabled", addr);

    Server::builder()
        .add_service(AgentServiceServer::new(agent_service))
        .add_service(sandbox_service.into_service())
        .add_service(reflection_service)
        .serve(addr)
        .await?;

    Ok(())
}

// Removed legacy metrics port lookup - now using centralized config::Config
