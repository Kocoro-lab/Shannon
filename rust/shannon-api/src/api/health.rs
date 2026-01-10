//! Health check endpoints.

use axum::{routing::get, Json, Router};
use serde::Serialize;

use crate::AppState;

/// Create the health router.
pub fn router() -> Router<AppState> {
    Router::new()
        .route("/health", get(health_check))
        .route("/ready", get(readiness_check))
        .route("/startup", get(startup_check))
}

/// Health check response.
#[derive(Debug, Serialize)]
struct HealthResponse {
    status: &'static str,
    version: &'static str,
}

/// Basic health check.
async fn health_check() -> Json<HealthResponse> {
    Json(HealthResponse {
        status: "ok",
        version: env!("CARGO_PKG_VERSION"),
    })
}

/// Readiness check response.
#[derive(Debug, Serialize)]
struct ReadinessResponse {
    status: &'static str,
    providers: ProvidersStatus,
}

#[derive(Debug, Serialize)]
struct ProvidersStatus {
    openai: bool,
    anthropic: bool,
}

/// Readiness check.
async fn readiness_check() -> Json<ReadinessResponse> {
    // In a real implementation, we'd check provider connectivity
    Json(ReadinessResponse {
        status: "ready",
        providers: ProvidersStatus {
            openai: true,
            anthropic: true,
        },
    })
}

/// Startup check response.
#[derive(Debug, Serialize)]
struct StartupResponse {
    status: &'static str,
    version: &'static str,
    startup_complete: bool,
    components: ComponentsStatus,
}

#[derive(Debug, Serialize)]
struct ComponentsStatus {
    database: bool,
    llm_client: bool,
    workflow_engine: bool,
    auth: bool,
}

/// Startup verification check.
///
/// This endpoint is used by the embedded API to verify that the Shannon API server
/// is fully initialized and ready to accept requests during the startup sequence.
async fn startup_check() -> Json<StartupResponse> {
    // In a real implementation, we'd check component initialization status
    Json(StartupResponse {
        status: "startup_complete",
        version: env!("CARGO_PKG_VERSION"),
        startup_complete: true,
        components: ComponentsStatus {
            database: true,
            llm_client: true,
            workflow_engine: true,
            auth: true,
        },
    })
}
