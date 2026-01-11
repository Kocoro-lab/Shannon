//! Session management endpoints.

use axum::{
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
    routing::{delete, get, post},
    Json, Router,
};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::database::repository::SessionRepository;
use crate::AppState;

/// Session routes.
pub fn router() -> Router<AppState> {
    Router::new()
        .route("/api/v1/sessions", get(list_sessions))
        .route("/api/v1/sessions", post(create_session))
        .route("/api/v1/sessions/{id}", get(get_session))
        .route("/api/v1/sessions/{id}", delete(delete_session))
        .route("/api/v1/sessions/{id}/history", get(get_session_history))
        .route("/api/v1/sessions/{id}/events", get(get_session_events))
        .route("/api/v1/sessions/{id}/messages", get(get_session_messages))
        .route("/api/v1/sessions/{id}/messages", post(add_session_message))
}

/// List sessions query parameters.
#[derive(Debug, Deserialize)]
pub struct ListSessionsQuery {
    #[serde(default = "default_limit")]
    pub limit: u32,
    #[serde(default)]
    pub offset: u32,
}

fn default_limit() -> u32 {
    20
}

/// List sessions response.
#[derive(Debug, Serialize)]
pub struct ListSessionsResponse {
    pub sessions: Vec<SessionResponse>,
    pub total_count: u32,
}

/// List all sessions.
///
/// # Errors
///
/// Returns 503 if database not available, 500 if database operation fails.
pub async fn list_sessions(
    State(state): State<AppState>,
    Query(query): Query<ListSessionsQuery>,
) -> impl IntoResponse {
    tracing::debug!(
        "📋 Listing sessions - limit={}, offset={}",
        query.limit,
        query.offset
    );

    // Check if database is available (embedded mode)
    let Some(ref database) = state.database else {
        tracing::warn!("❌ Database not available for session listing");
        let response = ListSessionsResponse {
            sessions: vec![],
            total_count: 0,
        };
        return (StatusCode::OK, Json(response));
    };

    // Use SessionRepository trait
    match database
        .list_sessions("embedded_user", query.limit as usize, query.offset as usize)
        .await
    {
        Ok(sessions) => {
            let total_count = sessions.len() as u32;
            let session_responses: Vec<SessionResponse> = sessions
                .into_iter()
                .map(|s| SessionResponse {
                    id: s.session_id,
                    name: s.title,
                    created_at: s.created_at.to_rfc3339(),
                    updated_at: s.updated_at.to_rfc3339(),
                    message_count: s.task_count as u32,
                    model: None,
                    metadata: s.context,
                })
                .collect();

            tracing::info!(
                "✅ Sessions listed - count={}, total={}",
                session_responses.len(),
                total_count
            );

            let response = ListSessionsResponse {
                sessions: session_responses,
                total_count,
            };

            (StatusCode::OK, Json(response))
        }
        Err(e) => {
            tracing::error!("❌ Failed to list sessions - error={}", e);
            let response = ListSessionsResponse {
                sessions: vec![],
                total_count: 0,
            };
            (StatusCode::INTERNAL_SERVER_ERROR, Json(response))
        }
    }
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
///
/// # Errors
///
/// Returns 503 if database not available, 500 if database operation fails.
pub async fn create_session(
    State(state): State<AppState>,
    Json(req): Json<CreateSessionRequest>,
) -> impl IntoResponse {
    let session_id = Uuid::new_v4().to_string();
    let now = chrono::Utc::now();

    tracing::info!("📝 Creating new session - session_id={}", session_id);

    // Check if database is available (embedded mode)
    let Some(ref database) = state.database else {
        tracing::warn!("❌ Database not available for session creation");
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "not_available",
                "message": "Session creation requires embedded mode"
            })),
        )
            .into_response();
    };

    let session = crate::database::repository::Session {
        session_id: session_id.clone(),
        user_id: "embedded_user".to_string(),
        title: req.name.clone(),
        task_count: 0,
        tokens_used: 0,
        token_budget: None,
        context: req.metadata.clone(),
        created_at: now,
        updated_at: now,
        last_activity_at: Some(now),
    };

    match database.create_session(&session).await {
        Ok(_) => {
            tracing::info!("✅ Session created - session_id={}", session_id);

            let response = SessionResponse {
                id: session_id,
                name: req.name,
                created_at: now.to_rfc3339(),
                updated_at: now.to_rfc3339(),
                message_count: 0,
                model: req.model,
                metadata: req.metadata,
            };

            (
                StatusCode::CREATED,
                Json(serde_json::to_value(response).unwrap()),
            )
                .into_response()
        }
        Err(e) => {
            tracing::error!("❌ Failed to create session - error={}", e);
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({
                    "error": "internal_error",
                    "message": "Failed to create session"
                })),
            )
                .into_response()
        }
    }
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
            Ok(Some(data)) => match serde_json::from_str::<serde_json::Value>(&data) {
                Ok(session) => {
                    let response = SessionResponse {
                        id: session["id"].as_str().unwrap_or(&id).to_string(),
                        name: session["name"].as_str().map(String::from),
                        created_at: session["created_at"]
                            .as_str()
                            .unwrap_or_default()
                            .to_string(),
                        updated_at: session["updated_at"]
                            .as_str()
                            .unwrap_or_default()
                            .to_string(),
                        message_count: session["messages"]
                            .as_array()
                            .map(|m| m.len() as u32)
                            .unwrap_or(0),
                        model: session["model"].as_str().map(String::from),
                        metadata: session.get("metadata").cloned(),
                    };
                    return (
                        StatusCode::OK,
                        Json(serde_json::to_value(response).unwrap()),
                    );
                }
                Err(_) => {}
            },
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

/// Get session history (tasks associated with session).
///
/// # Errors
///
/// Returns 404 if session not found.
pub async fn get_session_history(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    tracing::debug!("📜 Getting session history - session_id={}", id);

    // Get all tasks for this session from run_manager
    let all_runs = state.run_manager.list_active_runs();
    let session_runs: Vec<_> = all_runs
        .into_iter()
        .filter(|r| r.session_id.as_deref() == Some(&id))
        .map(|r| {
            use crate::domain::RunStatus;
            serde_json::json!({
                "task_id": r.id,
                "status": match r.status {
                    RunStatus::Pending => "pending",
                    RunStatus::Running => "running",
                    RunStatus::Completed => "completed",
                    RunStatus::Failed => "failed",
                    RunStatus::Cancelled => "cancelled",
                },
                "query": r.query,
                "created_at": r.created_at.to_rfc3339(),
                "completed_at": r.completed_at.map(|t| t.to_rfc3339()),
            })
        })
        .collect();

    tracing::info!(
        "✅ Session history retrieved - session_id={}, task_count={}",
        id,
        session_runs.len()
    );

    (
        StatusCode::OK,
        Json(serde_json::json!({
            "session_id": id,
            "tasks": session_runs,
            "total_count": session_runs.len()
        })),
    )
}

/// Get session events stream endpoint.
///
/// # Errors
///
/// Returns 501 as this is not yet implemented.
pub async fn get_session_events(
    State(_state): State<AppState>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    tracing::debug!("📡 Session events requested - session_id={}", id);

    // Placeholder - will be implemented with streaming infrastructure
    (
        StatusCode::NOT_IMPLEMENTED,
        Json(serde_json::json!({
            "error": "not_implemented",
            "message": "Session events streaming not yet implemented"
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
                    session["updated_at"] =
                        serde_json::Value::String(chrono::Utc::now().to_rfc3339());

                    // Save back
                    let _ = redis::AsyncCommands::set_ex::<_, _, ()>(
                        &mut redis,
                        &key,
                        session.to_string(),
                        86400 * 7,
                    )
                    .await;

                    return (
                        StatusCode::CREATED,
                        Json(serde_json::to_value(message).unwrap()),
                    );
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
