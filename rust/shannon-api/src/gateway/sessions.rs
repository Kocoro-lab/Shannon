//! Session management endpoints.

use axum::{
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
    routing::{delete, get, post},
    Json, Router,
};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::AppState;

/// Session routes.
pub fn router() -> Router<AppState> {
    Router::new()
        .route("/api/v1/sessions", post(create_session))
        .route("/api/v1/sessions/{id}", get(get_session))
        .route("/api/v1/sessions/{id}", delete(delete_session))
        .route("/api/v1/sessions/{id}/messages", get(get_session_messages))
        .route("/api/v1/sessions/{id}/messages", post(add_session_message))
}

/// Create session request.
#[derive(Debug, Deserialize)]
pub struct CreateSessionRequest {
    /// Optional session name.
    #[serde(default)]
    pub name: Option<String>,
    /// Optional system prompt.
    #[serde(default)]
    pub system_prompt: Option<String>,
    /// Optional model override.
    #[serde(default)]
    pub model: Option<String>,
    /// Optional metadata.
    #[serde(default)]
    pub metadata: Option<serde_json::Value>,
}

/// Session response.
#[derive(Debug, Serialize)]
pub struct SessionResponse {
    pub id: String,
    pub name: Option<String>,
    pub created_at: String,
    pub updated_at: String,
    pub message_count: u32,
    pub model: Option<String>,
    pub metadata: Option<serde_json::Value>,
}

/// Create a new session.
pub async fn create_session(
    State(state): State<AppState>,
    Json(req): Json<CreateSessionRequest>,
) -> impl IntoResponse {
    let session_id = Uuid::new_v4().to_string();
    let now = chrono::Utc::now().to_rfc3339();

    // Store session in Redis if available
    if let Some(ref redis) = state.redis {
        let session = serde_json::json!({
            "id": session_id,
            "name": req.name,
            "system_prompt": req.system_prompt,
            "model": req.model,
            "metadata": req.metadata,
            "created_at": now,
            "updated_at": now,
            "messages": [],
        });

        let mut redis = redis.clone();
        let key = format!("session:{}", session_id);
        
        match redis::AsyncCommands::set_ex::<_, _, ()>(
            &mut redis,
            &key,
            session.to_string(),
            86400 * 7, // 7 days TTL
        )
        .await
        {
            Ok(_) => {}
            Err(e) => {
                tracing::error!("Failed to store session in Redis: {}", e);
            }
        }
    }

    let response = SessionResponse {
        id: session_id,
        name: req.name,
        created_at: now.clone(),
        updated_at: now,
        message_count: 0,
        model: req.model,
        metadata: req.metadata,
    };

    (StatusCode::CREATED, Json(response))
}

/// Get session by ID.
pub async fn get_session(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    if let Some(ref redis) = state.redis {
        let mut redis = redis.clone();
        let key = format!("session:{}", id);
        
        match redis::AsyncCommands::get::<_, Option<String>>(&mut redis, &key).await {
            Ok(Some(data)) => {
                match serde_json::from_str::<serde_json::Value>(&data) {
                    Ok(session) => {
                        let response = SessionResponse {
                            id: session["id"].as_str().unwrap_or(&id).to_string(),
                            name: session["name"].as_str().map(String::from),
                            created_at: session["created_at"].as_str().unwrap_or_default().to_string(),
                            updated_at: session["updated_at"].as_str().unwrap_or_default().to_string(),
                            message_count: session["messages"]
                                .as_array()
                                .map(|m| m.len() as u32)
                                .unwrap_or(0),
                            model: session["model"].as_str().map(String::from),
                            metadata: session.get("metadata").cloned(),
                        };
                        return (StatusCode::OK, Json(serde_json::to_value(response).unwrap()));
                    }
                    Err(_) => {}
                }
            }
            _ => {}
        }
    }

    (
        StatusCode::NOT_FOUND,
        Json(serde_json::json!({
            "error": "not_found",
            "message": format!("Session {} not found", id)
        })),
    )
}

/// Delete a session.
pub async fn delete_session(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    if let Some(ref redis) = state.redis {
        let mut redis = redis.clone();
        let key = format!("session:{}", id);
        
        match redis::AsyncCommands::del::<_, i32>(&mut redis, &key).await {
            Ok(count) if count > 0 => {
                return (
                    StatusCode::OK,
                    Json(serde_json::json!({
                        "deleted": true,
                        "id": id
                    })),
                );
            }
            _ => {}
        }
    }

    (
        StatusCode::NOT_FOUND,
        Json(serde_json::json!({
            "error": "not_found",
            "message": format!("Session {} not found", id)
        })),
    )
}

/// Message in a session.
#[derive(Debug, Serialize, Deserialize)]
pub struct SessionMessage {
    pub id: String,
    pub role: String,
    pub content: String,
    pub created_at: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub tool_calls: Option<serde_json::Value>,
}

/// Get session messages.
pub async fn get_session_messages(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    if let Some(ref redis) = state.redis {
        let mut redis = redis.clone();
        let key = format!("session:{}", id);
        
        match redis::AsyncCommands::get::<_, Option<String>>(&mut redis, &key).await {
            Ok(Some(data)) => {
                if let Ok(session) = serde_json::from_str::<serde_json::Value>(&data) {
                    let messages = session["messages"].clone();
                    return (StatusCode::OK, Json(messages));
                }
            }
            _ => {}
        }
    }

    (
        StatusCode::NOT_FOUND,
        Json(serde_json::json!({
            "error": "not_found",
            "message": format!("Session {} not found", id)
        })),
    )
}

/// Add message request.
#[derive(Debug, Deserialize)]
pub struct AddMessageRequest {
    pub role: String,
    pub content: String,
    #[serde(default)]
    pub tool_calls: Option<serde_json::Value>,
}

/// Add a message to a session.
pub async fn add_session_message(
    State(state): State<AppState>,
    Path(id): Path<String>,
    Json(req): Json<AddMessageRequest>,
) -> impl IntoResponse {
    if let Some(ref redis) = state.redis {
        let mut redis = redis.clone();
        let key = format!("session:{}", id);
        
        // Get existing session
        match redis::AsyncCommands::get::<_, Option<String>>(&mut redis, &key).await {
            Ok(Some(data)) => {
                if let Ok(mut session) = serde_json::from_str::<serde_json::Value>(&data) {
                    let message = SessionMessage {
                        id: Uuid::new_v4().to_string(),
                        role: req.role,
                        content: req.content,
                        created_at: chrono::Utc::now().to_rfc3339(),
                        tool_calls: req.tool_calls,
                    };

                    // Add message to array
                    if let Some(messages) = session["messages"].as_array_mut() {
                        messages.push(serde_json::to_value(&message).unwrap());
                    }

                    // Update timestamp
                    session["updated_at"] = serde_json::Value::String(chrono::Utc::now().to_rfc3339());

                    // Save back
                    let _ = redis::AsyncCommands::set_ex::<_, _, ()>(
                        &mut redis,
                        &key,
                        session.to_string(),
                        86400 * 7,
                    )
                    .await;

                    return (StatusCode::CREATED, Json(serde_json::to_value(message).unwrap()));
                }
            }
            _ => {}
        }
    }

    (
        StatusCode::NOT_FOUND,
        Json(serde_json::json!({
            "error": "not_found",
            "message": format!("Session {} not found", id)
        })),
    )
}
