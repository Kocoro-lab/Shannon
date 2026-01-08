//! HTTP server setup and middleware.

use std::sync::Arc;
use std::time::Duration;

use axum::Router;
use tower_http::{
    cors::{Any, CorsLayer},
    timeout::TimeoutLayer,
    trace::TraceLayer,
};

use crate::api;
use crate::config::AppConfig;
use crate::gateway;
use crate::llm::orchestrator::Orchestrator;
use crate::llm::{LlmSettings, Provider};
use crate::runtime::RunManager;
use crate::tools::ToolRegistry;
use crate::workflow::WorkflowEngine;
use crate::AppState;

/// Create the application with all routes and middleware.
pub async fn create_app(config: AppConfig) -> anyhow::Result<Router> {
    // Create LLM settings from config
    let llm_settings = create_llm_settings(&config);

    // Create tool registry
    let tools = Arc::new(ToolRegistry::with_defaults());

    // Create orchestrator
    let orchestrator = Arc::new(Orchestrator::new(llm_settings, tools));

    // Create run manager
    let run_manager = Arc::new(RunManager::new(orchestrator.clone()));

    // Initialize Redis connection if configured (only for cloud mode)
    let redis = if config.deployment.is_embedded() {
        tracing::info!("Running in embedded mode - Redis not required");
        None
    } else if let Some(ref redis_url) = config.redis.url {
        match init_redis(redis_url).await {
            Ok(conn) => {
                tracing::info!("Redis connection established");
                Some(conn)
            }
            Err(e) => {
                tracing::warn!("Failed to connect to Redis: {}. Rate limiting and sessions will be limited.", e);
                None
            }
        }
    } else {
        tracing::info!("Redis not configured. Rate limiting and sessions will use in-memory fallback.");
        None
    };

    // Create workflow engine based on deployment mode
    // In embedded mode: uses Durable engine (local, no network)
    // In cloud mode: uses Temporal engine (via gRPC to orchestrator)
    let workflow_engine = create_workflow_engine(&config).await?;

    tracing::info!(
        "Workflow engine initialized: {} (mode: {})",
        workflow_engine.engine_type(),
        config.deployment.mode
    );

    // Create app state
    let state = AppState {
        config: Arc::new(config.clone()),
        orchestrator,
        run_manager,
        redis,
        workflow_engine,
    };

    // Build main API router
    let api_router = Router::new()
        .merge(api::create_router())
        .merge(gateway::create_router());

    // Build router with middleware
    let app = api_router
        .layer(
            CorsLayer::new()
                .allow_origin(Any)
                .allow_methods(Any)
                .allow_headers(Any),
        )
        .layer(TimeoutLayer::with_status_code(
            axum::http::StatusCode::REQUEST_TIMEOUT,
            Duration::from_secs(config.server.timeout_secs),
        ))
        .layer(TraceLayer::new_for_http())
        .with_state(state);

    Ok(app)
}

/// Initialize Redis connection.
async fn init_redis(url: &str) -> anyhow::Result<redis::aio::ConnectionManager> {
    let client = redis::Client::open(url)?;
    let conn = redis::aio::ConnectionManager::new(client).await?;
    Ok(conn)
}

/// Create workflow engine based on deployment configuration.
///
/// In embedded mode, this creates a local Durable engine that runs workflows
/// in-process without any network calls. The Go orchestrator is NOT required.
///
/// In cloud mode, this creates a Temporal engine that connects to the Go
/// orchestrator via gRPC for workflow coordination.
async fn create_workflow_engine(config: &AppConfig) -> anyhow::Result<WorkflowEngine> {
    WorkflowEngine::from_config(&config.deployment.workflow).await
}

/// Create LLM settings from app config.
fn create_llm_settings(config: &AppConfig) -> LlmSettings {
    // Determine provider and settings based on available API keys
    let (provider, api_key, base_url) = if config.providers.openai.api_key.is_some() {
        (
            Provider::OpenAi,
            config.providers.openai.api_key.clone(),
            config
                .providers
                .openai
                .base_url
                .clone()
                .unwrap_or_else(|| Provider::OpenAi.default_base_url().to_string()),
        )
    } else if config.providers.anthropic.api_key.is_some() {
        (
            Provider::Anthropic,
            config.providers.anthropic.api_key.clone(),
            config
                .providers
                .anthropic
                .base_url
                .clone()
                .unwrap_or_else(|| Provider::Anthropic.default_base_url().to_string()),
        )
    } else if config.providers.groq.api_key.is_some() {
        (
            Provider::Groq,
            config.providers.groq.api_key.clone(),
            config
                .providers
                .groq
                .base_url
                .clone()
                .unwrap_or_else(|| Provider::Groq.default_base_url().to_string()),
        )
    } else {
        // Default to OpenAI without key (will fail on requests)
        (
            Provider::OpenAi,
            None,
            Provider::OpenAi.default_base_url().to_string(),
        )
    };

    LlmSettings {
        base_url,
        api_key,
        model: config.llm.model.clone(),
        provider,
        max_tokens: config.llm.max_tokens,
        temperature: config.llm.temperature,
        parallel_tool_calls: Some(true),
    }
}
