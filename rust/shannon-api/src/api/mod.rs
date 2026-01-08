//! HTTP API endpoints.

pub mod chat;
pub mod health;
pub mod runs;

use axum::Router;

use crate::AppState;

/// Create the API router.
pub fn create_router() -> Router<AppState> {
    Router::new()
        .merge(health::router())
        .merge(chat::router())
        .merge(runs::router())
}
