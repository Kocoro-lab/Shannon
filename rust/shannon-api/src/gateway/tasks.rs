//! Task submission and status endpoints.

use axum::{
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
    routing::{get, post},
    Json, Router,
};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::AppState;

/// Task routes.
pub fn router() -> Router<AppState> {
    Router::new()
        .route("/api/v1/tasks", post(submit_task))
        .route("/api/v1/tasks/{id}", get(get_task_status))
        .route("/api/v1/tasks/{id}/cancel", post(cancel_task))
        .route("/api/v1/tasks/{id}/progress", get(get_task_progress))
        .route("/api/v1/tasks/{id}/output", get(get_task_output))
}

/// Task submission request.
#[derive(Debug, Deserialize)]
pub struct SubmitTaskRequest {
    /// Task description/prompt.
    pub prompt: String,
    /// Optional session ID to associate with.
    #[serde(default)]
    pub session_id: Option<String>,
    /// Task type (e.g., "chat", "research", "code").
    #[serde(default = "default_task_type")]
    pub task_type: String,
    /// Model to use.
    #[serde(default)]
    pub model: Option<String>,
    /// Maximum tokens for response.
    #[serde(default)]
    pub max_tokens: Option<u32>,
    /// Temperature for sampling.
    #[serde(default)]
    pub temperature: Option<f32>,
    /// System prompt override.
    #[serde(default)]
    pub system_prompt: Option<String>,
    /// Available tools.
    #[serde(default)]
    pub tools: Option<Vec<serde_json::Value>>,
    /// Additional metadata.
    #[serde(default)]
    pub metadata: Option<serde_json::Value>,
}

fn default_task_type() -> String {
    "chat".to_string()
}

/// Task status.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum TaskStatus {
    Pending,
    Running,
    Completed,
    Failed,
    Cancelled,
}

/// Task response.
#[derive(Debug, Serialize)]
pub struct TaskResponse {
    #[serde(rename = "task_id")]
    pub id: String,
    pub status: TaskStatus,
    pub task_type: String,
    pub created_at: String,
    pub updated_at: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub started_at: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub completed_at: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub session_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

/// Task progress response.
#[derive(Debug, Serialize)]
pub struct TaskProgressResponse {
    #[serde(rename = "task_id")]
    pub id: String,
    pub status: TaskStatus,
    pub progress_percent: u8,
    pub current_step: Option<String>,
    pub total_steps: Option<u32>,
    pub completed_steps: Option<u32>,
    pub estimated_remaining_secs: Option<u64>,
    pub subtasks: Vec<SubtaskProgress>,
}

/// Subtask progress.
#[derive(Debug, Serialize)]
pub struct SubtaskProgress {
    pub id: String,
    pub name: String,
    pub status: TaskStatus,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub output: Option<String>,
}

/// Submit a new task.
pub async fn submit_task(
    State(state): State<AppState>,
    Json(req): Json<SubmitTaskRequest>,
) -> impl IntoResponse {
    let task_id = Uuid::new_v4().to_string();
    let now = chrono::Utc::now().to_rfc3339();

    // Create task record
    let task = serde_json::json!({
        "id": task_id,
        "status": "pending",
        "task_type": req.task_type,
        "prompt": req.prompt,
        "session_id": req.session_id,
        "model": req.model,
        "max_tokens": req.max_tokens,
        "temperature": req.temperature,
        "system_prompt": req.system_prompt,
        "tools": req.tools,
        "metadata": req.metadata,
        "created_at": now,
        "updated_at": now,
        "progress": {
            "percent": 0,
            "current_step": null,
            "subtasks": []
        }
    });

    // Store task in Redis if available
    if let Some(ref redis) = state.redis {
        let mut redis = redis.clone();
        let key = format!("task:{}", task_id);
        
        match redis::AsyncCommands::set_ex::<_, _, ()>(
            &mut redis,
            &key,
            task.to_string(),
            86400, // 24 hours TTL
        )
        .await
        {
            Ok(_) => {
                tracing::info!("Task {} stored in Redis", task_id);
            }
            Err(e) => {
                tracing::error!("Failed to store task in Redis: {}", e);
            }
        }

        // Also add to task queue for processing
        let queue_key = "task_queue";
        let _ = redis::AsyncCommands::lpush::<_, _, ()>(&mut redis, queue_key, &task_id).await;
    }

    // Submit to workflow engine (local Durable or remote Temporal based on mode)
    let workflow_task = crate::workflow::Task {
        id: task_id.clone(),
        query: req.prompt.clone(),
        user_id: "default".to_string(), // TODO: Get from auth context
        session_id: req.session_id.clone(),
        context: req.metadata.clone().unwrap_or(serde_json::json!({})),
        strategy: if req.task_type == "research" {
            crate::workflow::task::Strategy::Research
        } else {
            crate::workflow::task::Strategy::Simple
        },
        labels: std::collections::HashMap::new(),
        max_agents: None,
        token_budget: None,
        require_approval: false,
    };

    match state.workflow_engine.submit(workflow_task).await {
        Ok(handle) => {
            tracing::info!(
                "Task {} submitted to workflow engine ({}), workflow_id: {}",
                task_id,
                state.workflow_engine.engine_type(),
                handle.workflow_id
            );
            
            // Update task status in Redis with workflow_id
            if let Some(ref redis) = state.redis {
                let mut redis = redis.clone();
                let key = format!("task:{}", task_id);
                let mut task_with_workflow = task.clone();
                task_with_workflow["workflow_id"] = serde_json::Value::String(handle.workflow_id);
                task_with_workflow["status"] = serde_json::Value::String("running".to_string());
                task_with_workflow["started_at"] = serde_json::Value::String(chrono::Utc::now().to_rfc3339());
                let _ = redis::AsyncCommands::set_ex::<_, _, ()>(
                    &mut redis,
                    &key,
                    task_with_workflow.to_string(),
                    86400,
                )
                .await;
            }
        }
        Err(e) => {
            tracing::error!("Failed to submit task {} to workflow engine: {}", task_id, e);
            
            // Update task status to failed in Redis
            if let Some(ref redis) = state.redis {
                let mut redis = redis.clone();
                let key = format!("task:{}", task_id);
                let mut failed_task = task.clone();
                failed_task["status"] = serde_json::Value::String("failed".to_string());
                failed_task["error"] = serde_json::Value::String(format!("Failed to submit to workflow engine: {}", e));
                let _ = redis::AsyncCommands::set_ex::<_, _, ()>(
                    &mut redis,
                    &key,
                    failed_task.to_string(),
                    86400,
                )
                .await;
            }
            
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(TaskResponse {
                    id: task_id,
                    status: TaskStatus::Failed,
                    task_type: req.task_type,
                    created_at: now.clone(),
                    updated_at: now,
                    started_at: None,
                    completed_at: None,
                    session_id: req.session_id,
                    error: Some(format!("Failed to submit to workflow engine: {}", e)),
                }),
            );
        }
    }

    let response = TaskResponse {
        id: task_id,
        status: TaskStatus::Pending,
        task_type: req.task_type,
        created_at: now.clone(),
        updated_at: now,
        started_at: None,
        completed_at: None,
        session_id: req.session_id,
        error: None,
    };

    (StatusCode::ACCEPTED, Json(response))
}

/// Get task status.
pub async fn get_task_status(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    // First check in-memory run manager (works in embedded mode)
    if let Some(run) = state.run_manager.get_run(&id) {
        use crate::domain::RunStatus;
        let status = match run.status {
            RunStatus::Pending => TaskStatus::Pending,
            RunStatus::Running => TaskStatus::Running,
            RunStatus::Completed => TaskStatus::Completed,
            RunStatus::Failed => TaskStatus::Failed,
            RunStatus::Cancelled => TaskStatus::Cancelled,
        };

        let response = TaskResponse {
            id: run.id.clone(),
            status,
            task_type: "chat".to_string(),
            created_at: run.created_at.to_rfc3339(),
            updated_at: run.updated_at.to_rfc3339(),
            started_at: Some(run.created_at.to_rfc3339()),
            completed_at: run.completed_at.map(|t| t.to_rfc3339()),
            session_id: run.session_id.clone(),
            error: run.error.clone(),
        };

        return (StatusCode::OK, Json(serde_json::to_value(response).unwrap()));
    }

    // Fall back to Redis if available
    if let Some(ref redis) = state.redis {
        let mut redis = redis.clone();
        let key = format!("task:{}", id);
        
        match redis::AsyncCommands::get::<_, Option<String>>(&mut redis, &key).await {
            Ok(Some(data)) => {
                if let Ok(task) = serde_json::from_str::<serde_json::Value>(&data) {
                    let status = match task["status"].as_str().unwrap_or("pending") {
                        "pending" => TaskStatus::Pending,
                        "running" => TaskStatus::Running,
                        "completed" => TaskStatus::Completed,
                        "failed" => TaskStatus::Failed,
                        "cancelled" => TaskStatus::Cancelled,
                        _ => TaskStatus::Pending,
                    };

                    let response = TaskResponse {
                        id: task["id"].as_str().unwrap_or(&id).to_string(),
                        status,
                        task_type: task["task_type"].as_str().unwrap_or("chat").to_string(),
                        created_at: task["created_at"].as_str().unwrap_or_default().to_string(),
                        updated_at: task["updated_at"].as_str().unwrap_or_default().to_string(),
                        started_at: task["started_at"].as_str().map(String::from),
                        completed_at: task["completed_at"].as_str().map(String::from),
                        session_id: task["session_id"].as_str().map(String::from),
                        error: task["error"].as_str().map(String::from),
                    };

                    return (StatusCode::OK, Json(serde_json::to_value(response).unwrap()));
                }
            }
            _ => {}
        }
    }

    (
        StatusCode::NOT_FOUND,
        Json(serde_json::json!({
            "error": "not_found",
            "message": format!("Task {} not found", id)
        })),
    )
}

/// Cancel a task.
pub async fn cancel_task(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    if let Some(ref redis) = state.redis {
        let mut redis = redis.clone();
        let key = format!("task:{}", id);
        
        // Get existing task
        match redis::AsyncCommands::get::<_, Option<String>>(&mut redis, &key).await {
            Ok(Some(data)) => {
                if let Ok(mut task) = serde_json::from_str::<serde_json::Value>(&data) {
                    // Check if task can be cancelled
                    let current_status = task["status"].as_str().unwrap_or("pending");
                    if current_status == "completed" || current_status == "failed" || current_status == "cancelled" {
                        return (
                            StatusCode::CONFLICT,
                            Json(serde_json::json!({
                                "error": "cannot_cancel",
                                "message": format!("Task {} is already {}", id, current_status)
                            })),
                        );
                    }

                    // Update task status
                    task["status"] = serde_json::Value::String("cancelled".to_string());
                    task["updated_at"] = serde_json::Value::String(chrono::Utc::now().to_rfc3339());
                    task["completed_at"] = serde_json::Value::String(chrono::Utc::now().to_rfc3339());

                    // Save back
                    let _ = redis::AsyncCommands::set_ex::<_, _, ()>(
                        &mut redis,
                        &key,
                        task.to_string(),
                        86400,
                    )
                    .await;

                    // Send cancel signal to workflow engine
                    if let Err(e) = state.workflow_engine.cancel(&id, Some("User requested cancellation")).await {
                        tracing::warn!("Failed to cancel task {} in workflow engine: {}", id, e);
                    }

                    return (
                        StatusCode::OK,
                        Json(serde_json::json!({
                            "cancelled": true,
                            "id": id
                        })),
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
            "message": format!("Task {} not found", id)
        })),
    )
}

/// Get task progress.
pub async fn get_task_progress(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    if let Some(ref redis) = state.redis {
        let mut redis = redis.clone();
        let key = format!("task:{}", id);
        
        match redis::AsyncCommands::get::<_, Option<String>>(&mut redis, &key).await {
            Ok(Some(data)) => {
                if let Ok(task) = serde_json::from_str::<serde_json::Value>(&data) {
                    let status = match task["status"].as_str().unwrap_or("pending") {
                        "pending" => TaskStatus::Pending,
                        "running" => TaskStatus::Running,
                        "completed" => TaskStatus::Completed,
                        "failed" => TaskStatus::Failed,
                        "cancelled" => TaskStatus::Cancelled,
                        _ => TaskStatus::Pending,
                    };

                    let progress = &task["progress"];
                    let subtasks = progress["subtasks"]
                        .as_array()
                        .map(|arr| {
                            arr.iter()
                                .filter_map(|s| {
                                    Some(SubtaskProgress {
                                        id: s["id"].as_str()?.to_string(),
                                        name: s["name"].as_str()?.to_string(),
                                        status: match s["status"].as_str()? {
                                            "pending" => TaskStatus::Pending,
                                            "running" => TaskStatus::Running,
                                            "completed" => TaskStatus::Completed,
                                            "failed" => TaskStatus::Failed,
                                            "cancelled" => TaskStatus::Cancelled,
                                            _ => TaskStatus::Pending,
                                        },
                                        output: s["output"].as_str().map(String::from),
                                    })
                                })
                                .collect()
                        })
                        .unwrap_or_default();

                    let response = TaskProgressResponse {
                        id: id.clone(),
                        status,
                        progress_percent: progress["percent"].as_u64().unwrap_or(0) as u8,
                        current_step: progress["current_step"].as_str().map(String::from),
                        total_steps: progress["total_steps"].as_u64().map(|v| v as u32),
                        completed_steps: progress["completed_steps"].as_u64().map(|v| v as u32),
                        estimated_remaining_secs: progress["estimated_remaining_secs"].as_u64(),
                        subtasks,
                    };

                    return (StatusCode::OK, Json(serde_json::to_value(response).unwrap()));
                }
            }
            _ => {}
        }
    }

    (
        StatusCode::NOT_FOUND,
        Json(serde_json::json!({
            "error": "not_found",
            "message": format!("Task {} not found", id)
        })),
    )
}

/// Get task output.
pub async fn get_task_output(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    if let Some(ref redis) = state.redis {
        let mut redis = redis.clone();
        let key = format!("task:{}", id);
        
        match redis::AsyncCommands::get::<_, Option<String>>(&mut redis, &key).await {
            Ok(Some(data)) => {
                if let Ok(task) = serde_json::from_str::<serde_json::Value>(&data) {
                    let status = task["status"].as_str().unwrap_or("pending");
                    
                    if status != "completed" {
                        return (
                            StatusCode::BAD_REQUEST,
                            Json(serde_json::json!({
                                "error": "task_not_completed",
                                "message": format!("Task {} is not yet completed (status: {})", id, status)
                            })),
                        );
                    }

                    return (
                        StatusCode::OK,
                        Json(serde_json::json!({
                            "id": id,
                            "output": task["output"],
                            "completed_at": task["completed_at"],
                            "usage": task["usage"]
                        })),
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
            "message": format!("Task {} not found", id)
        })),
    )
}
