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
use crate::logging::OpTimer;
use crate::runtime::RunManager;
use crate::tools::ToolRegistry;
use crate::workflow::WorkflowEngine;
use crate::{log_banner, log_init_step, log_init_warning, log_success, AppState};

/// Shannon API version (from Cargo.toml).
const VERSION: &str = env!("CARGO_PKG_VERSION");

/// Create the application with all routes and middleware.
pub async fn create_app(
    config: AppConfig,
    #[cfg(feature = "embedded")] existing_db: Option<
        std::sync::Arc<surrealdb::Surreal<surrealdb::engine::local::Db>>,
    >,
) -> anyhow::Result<Router> {
    // Start overall timer
    let overall_timer = OpTimer::new("server", "create_app");

    // Log startup banner
    log_banner!(
        format!("🚀 Shannon API v{}", VERSION),
        format!(
            "Mode: {} | Engine: {}",
            config.deployment.mode,
            config.deployment.workflow.engine_name()
        )
    );

    // [1/8] Create LLM settings from config
    let step_timer = OpTimer::new("server", "llm_settings");
    let llm_settings = create_llm_settings(&config);
    let provider_info = format!(
        "{} ({}) {}",
        match llm_settings.provider {
            Provider::OpenAi => "⚙️ OpenAI",
            Provider::Anthropic => "⚙️ Anthropic",
            Provider::Google => "⚙️ Google",
            Provider::Groq => "⚙️ Groq",
            Provider::Xai => "⚙️ xAI",
            Provider::Custom => "⚙️ Custom",
        },
        llm_settings.model,
        if llm_settings.api_key.is_some() {
            "✓"
        } else {
            "✗ No API key"
        }
    );
    log_init_step!(1, 8, "LLM Settings", provider_info);

    // Warn if no API key is configured
    if llm_settings.api_key.is_none() {
        log_init_warning!(
            "No API key configured for provider: {:?}. LLM requests will fail.",
            llm_settings.provider
        );
    }
    step_timer.finish();

    // [2/8] Create tool registry
    let step_timer = OpTimer::new("server", "tool_registry");
    let tools = Arc::new(ToolRegistry::with_defaults());
    let tool_count = tools.list_tools().len();
    log_init_step!(2, 8, "Tool Registry", format!("🔧 {} tools", tool_count));
    step_timer.finish();

    // [3/8] Create orchestrator
    let step_timer = OpTimer::new("server", "orchestrator");
    let orchestrator = Arc::new(Orchestrator::new(llm_settings, tools));
    log_init_step!(3, 8, "Orchestrator", "🎭 LLM coordination ready");
    step_timer.finish();

    // [4/8] Create run manager
    let step_timer = OpTimer::new("server", "run_manager");
    let run_manager = Arc::new(RunManager::new(orchestrator.clone()));
    log_init_step!(4, 8, "Run Manager", "🏃 Task lifecycle manager ready");
    step_timer.finish();

    // [5/8] Initialize Redis connection if configured (only for cloud mode)
    let step_timer = OpTimer::new("server", "redis");
    let redis = if config.deployment.is_embedded() {
        log_init_step!(5, 8, "Redis", "💾 Skipped (embedded mode)");
        None
    } else if let Some(ref redis_url) = config.redis.url {
        match init_redis(redis_url).await {
            Ok(conn) => {
                log_init_step!(5, 8, "Redis", format!("💾 Connected to {}", redis_url));
                Some(conn)
            }
            Err(e) => {
                log_init_warning!(
                    "Failed to connect to Redis: {}. Using in-memory fallback.",
                    e
                );
                log_init_step!(5, 8, "Redis", "💾 In-memory fallback");
                None
            }
        }
    } else {
        log_init_step!(5, 8, "Redis", "💾 Not configured (in-memory fallback)");
        None
    };
    step_timer.finish();

    // [6/8] Initialize embedded DB for direct use (Auth, etc.) if enabled
    let step_timer = OpTimer::new("server", "surrealdb");
    #[cfg(feature = "embedded")]
    let surreal = if config.deployment.is_embedded() {
        // Use existing database connection if provided, otherwise create new one
        if let Some(existing_conn) = existing_db {
            log_init_step!(6, 8, "SurrealDB", "🗄️  Using shared connection");
            Some((*existing_conn).clone())
        } else {
            let db_path =
                std::env::var("SURREALDB_PATH").unwrap_or_else(|_| "./data/shannon.db".to_string());

            // Select SurrealDB backend based on SURREALDB_BACKEND environment variable
            let backend = std::env::var("SURREALDB_BACKEND")
                .unwrap_or_else(|_| "rocksdb".to_string())
                .to_lowercase();

            // Create a client instance with the specified backend
            let db_result = match backend.as_str() {
                "surrealkv" | "kv" => {
                    // Try SurrealKv backend for better Tauri compatibility
                    surrealdb::Surreal::new::<surrealdb::engine::local::SurrealKv>(db_path.clone()).await
                }
                "rocksdb" | "rocks" | _ => {
                    // Default to RocksDb backend
                    surrealdb::Surreal::new::<surrealdb::engine::local::RocksDb>(db_path.clone()).await
                }
            };

            match db_result {
                Ok(db) => {
                    // Select default namespace/database
                    if let Err(e) = db.use_ns("shannon").use_db("main").await {
                        log_init_warning!("Failed to select SurrealDB ns/db: {}", e);
                        log_init_step!(6, 8, "SurrealDB", "🗄️  Failed to initialize");
                        None
                    } else {
                        log_init_step!(6, 8, "SurrealDB", format!("🗄️  Embedded at {}", db_path));
                        Some(db)
                    }
                }
                Err(e) => {
                    log_init_warning!("Failed to connect to embedded SurrealDB: {}", e);
                    log_init_step!(6, 8, "SurrealDB", "🗄️  Connection failed");
                    None
                }
            }
        }
    } else {
        log_init_step!(6, 8, "SurrealDB", "🗄️  Skipped (cloud mode)");
        None
    };
    #[cfg(not(feature = "embedded"))]
    let _surreal: Option<()> = {
        log_init_step!(6, 8, "SurrealDB", "🗄️  Not available (feature not enabled)");
        None
    };
    step_timer.finish();

    // [7/8] Create workflow engine based on deployment mode
    let step_timer = OpTimer::new("server", "workflow_engine");
    let workflow_engine = create_workflow_engine(
        &config,
        #[cfg(feature = "embedded")]
        surreal.clone(),
    )
    .await?;
    let engine_info = format!(
        "⚡ {} ({})",
        workflow_engine.engine_type(),
        config.deployment.mode
    );
    log_init_step!(7, 8, "Workflow Engine", engine_info);
    step_timer.finish();

    // Create app state
    let state = AppState {
        config: Arc::new(config.clone()),
        orchestrator,
        run_manager,
        redis,
        workflow_engine,
        #[cfg(feature = "embedded")]
        surreal,
    };

    // [8/8] Build main API router with middleware
    let step_timer = OpTimer::new("server", "router");
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
        .layer(axum::middleware::from_fn_with_state(
            state.clone(),
            gateway::auth::auth_middleware,
        ))
        .with_state(state);

    log_init_step!(8, 8, "Router", "🌐 Routes + middleware configured");
    step_timer.finish();

    // Log success banner
    overall_timer.finish();
    log_success!("Shannon API server created successfully");
    tracing::info!("");

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
async fn create_workflow_engine(
    config: &AppConfig,
    #[cfg(feature = "embedded")] surreal_conn: Option<
        surrealdb::Surreal<surrealdb::engine::local::Db>,
    >,
) -> anyhow::Result<WorkflowEngine> {
    WorkflowEngine::from_config(
        &config.deployment.workflow,
        #[cfg(feature = "embedded")]
        surreal_conn,
    )
    .await
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
