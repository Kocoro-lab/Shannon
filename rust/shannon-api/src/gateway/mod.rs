//! Gateway functionality - Authentication, Rate Limiting, Sessions, Tasks.
//!
//! This module provides the HTTP gateway layer for Shannon, handling:
//! - API key and JWT authentication
//! - Rate limiting and idempotency
//! - Session management
//! - Task submission and status tracking
//! - SSE and WebSocket streaming
//! - gRPC client for Orchestrator communication

pub mod auth;
pub mod grpc_client;
pub mod idempotency;
pub mod rate_limit;
pub mod routes;
pub mod sessions;
pub mod streaming;
pub mod tasks;

use axum::Router;

use crate::AppState;

/// Create the gateway router with all gateway-specific routes.
pub fn create_router() -> Router<AppState> {
    Router::new()
        .merge(routes::router())
        .merge(sessions::router())
        .merge(tasks::router())
        .merge(streaming::router())
}
