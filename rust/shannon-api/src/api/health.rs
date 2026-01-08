//! Health check endpoints.

use axum::{routing::get, Json, Router};
use serde::Serialize;

use crate::AppState;

/// Create the health router.
pub fn router() -> Router<AppState> {
    Router::new()
        .route("/health", get(health_check))
        .route("/ready", get(readiness_check))
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
