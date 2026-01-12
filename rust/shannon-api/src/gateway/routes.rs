//! API route definitions for the gateway.

use axum::{extract::State, http::StatusCode, response::IntoResponse, routing::get, Json, Router};
use serde::Serialize;

use crate::AppState;

/// Gateway-specific routes.
pub fn router() -> Router<AppState> {
    Router::new()
        .route("/api/v1/info", get(get_api_info))
        .route("/api/v1/capabilities", get(get_capabilities))
        .route(
            "/api/v1/auth/me",
            get(crate::gateway::auth::get_current_user_embedded),
        )
}

/// API info response.
#[derive(Debug, Serialize)]
pub struct ApiInfo {
    pub name: String,
    pub version: String,
    pub description: String,
    pub endpoints: Vec<EndpointInfo>,
}

/// Endpoint information.
#[derive(Debug, Serialize)]
pub struct EndpointInfo {
    pub path: String,
    pub method: String,
    pub description: String,
}

/// Get API information.
pub async fn get_api_info() -> impl IntoResponse {
    let info = ApiInfo {
        name: "Shannon API".to_string(),
        version: env!("CARGO_PKG_VERSION").to_string(),
        description: "Unified Rust Gateway and LLM Service for Shannon AI Platform".to_string(),
        endpoints: vec![
            EndpointInfo {
                path: "/api/v1/chat/completions".to_string(),
                method: "POST".to_string(),
                description: "OpenAI-compatible chat completions".to_string(),
            },
            EndpointInfo {
                path: "/api/v1/sessions".to_string(),
                method: "POST".to_string(),
                description: "Create a new session".to_string(),
            },
            EndpointInfo {
                path: "/api/v1/tasks".to_string(),
                method: "POST".to_string(),
                description: "Submit a new task".to_string(),
            },
            EndpointInfo {
                path: "/api/v1/tasks/{id}".to_string(),
                method: "GET".to_string(),
                description: "Get task status".to_string(),
            },
            EndpointInfo {
                path: "/api/v1/tasks/{id}/stream".to_string(),
                method: "GET".to_string(),
                description: "Stream task events via SSE".to_string(),
            },
        ],
    };

    (StatusCode::OK, Json(info))
}

/// Capabilities response.
#[derive(Debug, Serialize)]
pub struct Capabilities {
    pub streaming: bool,
    pub websocket: bool,
    pub sse: bool,
    pub tool_calling: bool,
    pub multi_modal: bool,
    pub providers: Vec<String>,
}

/// Get API capabilities.
pub async fn get_capabilities(State(state): State<AppState>) -> impl IntoResponse {
    let mut providers = Vec::new();

    if state.config.providers.openai.api_key.is_some() {
        providers.push("openai".to_string());
    }
    if state.config.providers.anthropic.api_key.is_some() {
        providers.push("anthropic".to_string());
    }
    if state.config.providers.google.api_key.is_some() {
        providers.push("google".to_string());
    }
    if state.config.providers.groq.api_key.is_some() {
        providers.push("groq".to_string());
    }
    if state.config.providers.xai.api_key.is_some() {
        providers.push("xai".to_string());
    }

    let capabilities = Capabilities {
        streaming: true,
        websocket: true,
        sse: true,
        tool_calling: true,
        multi_modal: true,
        providers,
    };

    (StatusCode::OK, Json(capabilities))
}
