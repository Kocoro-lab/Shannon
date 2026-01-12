//! Task submission and status endpoints.

use axum::{
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
    routing::{get, post},
    Json, Router,
};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::logging::OpTimer;
use crate::AppState;

/// Task routes.
pub fn router() -> Router<AppState> {
    Router::new()
        .route("/api/v1/tasks", get(list_tasks))
        .route("/api/v1/tasks", post(submit_task))
        .route("/api/v1/tasks/{id}", get(get_task_status))
        .route("/api/v1/tasks/{id}/cancel", post(cancel_task))
        .route("/api/v1/tasks/{id}/pause", post(pause_task))
        .route("/api/v1/tasks/{id}/resume", post(resume_task))
        .route("/api/v1/tasks/{id}/progress", get(get_task_progress))
        .route("/api/v1/tasks/{id}/output", get(get_task_output))
        .route("/api/v1/tasks/{id}/control-state", get(get_control_state))
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
    /// System prompt override (deprecated - use context.system_prompt).
    #[serde(default)]
    pub system_prompt: Option<String>,
    /// Available tools.
    #[serde(default)]
    pub tools: Option<Vec<serde_json::Value>>,
    /// Additional metadata.
    #[serde(default)]
    pub metadata: Option<serde_json::Value>,
    /// Task context with comprehensive parameters.
    #[serde(default)]
    pub context: Option<crate::domain::TaskContext>,
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
#[derive(Debug, Clone, Serialize)]
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

/// Query parameters for task list.
#[derive(Debug, Deserialize)]
pub struct ListTasksQuery {
    /// Maximum number of tasks to return.
    #[serde(default = "default_list_limit")]
    pub limit: usize,
    /// Number of tasks to skip.
    #[serde(default)]
    pub offset: usize,
    /// Filter by status.
    #[serde(default)]
    pub status: Option<String>,
    /// Filter by session ID.
    #[serde(default)]
    pub session_id: Option<String>,
}

fn default_list_limit() -> usize {
    20
}

/// Task list response.
#[derive(Debug, Serialize)]
pub struct TaskListResponse {
    pub tasks: Vec<TaskResponse>,
    pub total_count: usize,
    pub limit: usize,
    pub offset: usize,
}

/// List tasks with pagination and filtering.
///
/// # Errors
///
/// Returns 500 if database operation fails.
pub async fn list_tasks(
    State(state): State<AppState>,
    Query(query): Query<ListTasksQuery>,
) -> impl IntoResponse {
    let timer = OpTimer::new("gateway", "list_tasks");

    tracing::debug!(
        "📋 Listing tasks - limit={}, offset={}, status={:?}, session={:?}",
        query.limit,
        query.offset,
        query.status,
        query.session_id
    );

    let mut tasks = Vec::new();
    let mut total_count = 0;

    // Strategy: Database-first for persistence, merge with in-memory for active tasks
    if let Some(ref database) = state.database {
        // Get persistent tasks from database (get extra to account for merging)
        match database
            .list_runs_filtered(
                "embedded_user",
                query.limit * 2, // Fetch extra to handle deduplication
                query.offset,
                query.status.as_deref(),
                query.session_id.as_deref(),
            )
            .await
        {
            Ok(db_runs) => {
                tracing::debug!("📚 Retrieved {} runs from database", db_runs.len());

                // Get accurate total count
                total_count = match database
                    .count_runs(
                        "embedded_user",
                        query.status.as_deref(),
                        query.session_id.as_deref(),
                    )
                    .await
                {
                    Ok(count) => count,
                    Err(e) => {
                        tracing::warn!("⚠️  Failed to count runs: {}", e);
                        db_runs.len()
                    }
                };

                // Convert DB runs to task responses
                for run in db_runs {
                    tasks.push(run_to_task_response(&run));
                }
            }
            Err(e) => {
                tracing::error!("❌ Failed to list runs from database: {}", e);
            }
        }
    }

    // Merge with in-memory active runs (override database for active tasks)
    let active_runs = state.run_manager.list_active_runs();
    let active_runs_count = active_runs.len();
    tracing::debug!("🏃 Found {} active runs in memory", active_runs_count);

    for run in &active_runs {
        use crate::domain::RunStatus;

        // Apply filters (status and session)
        if let Some(ref filter_status) = query.status {
            let status_str = match run.status {
                RunStatus::Pending => "pending",
                RunStatus::Running => "running",
                RunStatus::Completed => "completed",
                RunStatus::Failed => "failed",
                RunStatus::Cancelled => "cancelled",
            };
            if status_str != filter_status.as_str() {
                continue;
            }
        }

        if let Some(ref filter_session) = query.session_id {
            if run.session_id.as_deref() != Some(filter_session.as_str()) {
                continue;
            }
        }

        // Check if already in list (by ID), update if so, otherwise add
        if let Some(existing) = tasks.iter_mut().find(|t| t.id == run.id) {
            // Update with latest active state
            *existing = run_to_task_response_from_manager(&run);
            tracing::trace!("🔄 Updated task from active run - id={}", run.id);
        } else {
            // New active task not in DB yet
            tasks.push(run_to_task_response_from_manager(&run));
            tracing::trace!("➕ Added new active task - id={}", run.id);
        }
    }

    // Sort by created_at DESC (most recent first)
    tasks.sort_by(|a, b| b.created_at.cmp(&a.created_at));

    // Calculate counts before moving
    let tasks_before_pagination = tasks.len();

    // Apply pagination after merging
    let paginated: Vec<_> = tasks
        .into_iter()
        .skip(query.offset)
        .take(query.limit)
        .collect();

    // Update total count to include any active runs not yet in DB
    total_count = total_count.max(paginated.len() + query.offset);

    timer.finish();

    tracing::info!(
        "✅ Task list retrieved - returned={}, total={}, from_db={}, active={}",
        paginated.len(),
        total_count,
        tasks_before_pagination,
        active_runs_count
    );

    (
        StatusCode::OK,
        Json(TaskListResponse {
            tasks: paginated,
            total_count,
            limit: query.limit,
            offset: query.offset,
        }),
    )
}

/// Convert a Run from database to TaskResponse.
fn run_to_task_response(run: &crate::database::repository::Run) -> TaskResponse {
    let status = match run.status.as_str() {
        "pending" => TaskStatus::Pending,
        "running" => TaskStatus::Running,
        "completed" => TaskStatus::Completed,
        "failed" => TaskStatus::Failed,
        "cancelled" => TaskStatus::Cancelled,
        _ => TaskStatus::Pending,
    };

    TaskResponse {
        id: run.id.clone(),
        status,
        task_type: run.strategy.clone(),
        created_at: run.created_at.to_rfc3339(),
        updated_at: run.updated_at.to_rfc3339(),
        started_at: Some(run.created_at.to_rfc3339()),
        completed_at: run.completed_at.map(|t| t.to_rfc3339()),
        session_id: run.session_id.clone(),
        error: run.error.clone(),
    }
}

/// Convert a Run from in-memory manager to TaskResponse.
fn run_to_task_response_from_manager(run: &crate::domain::Run) -> TaskResponse {
    use crate::domain::RunStatus;

    let status = match run.status {
        RunStatus::Pending => TaskStatus::Pending,
        RunStatus::Running => TaskStatus::Running,
        RunStatus::Completed => TaskStatus::Completed,
        RunStatus::Failed => TaskStatus::Failed,
        RunStatus::Cancelled => TaskStatus::Cancelled,
    };

    TaskResponse {
        id: run.id.clone(),
        status,
        task_type: "chat".to_string(),
        created_at: run.created_at.to_rfc3339(),
        updated_at: run.updated_at.to_rfc3339(),
        started_at: Some(run.created_at.to_rfc3339()),
        completed_at: run.completed_at.map(|t| t.to_rfc3339()),
        session_id: run.session_id.clone(),
        error: run.error.clone(),
    }
}

/// Submit a new task.
pub async fn submit_task(
    State(state): State<AppState>,
    Json(req): Json<SubmitTaskRequest>,
) -> impl IntoResponse {
    let timer = OpTimer::new("gateway", "submit_task");
    let task_id = Uuid::new_v4().to_string();
    let now = chrono::Utc::now().to_rfc3339();

    let prompt_preview = if req.prompt.len() > 100 {
        format!("{}...", &req.prompt[..100])
    } else {
        req.prompt.clone()
    };

    // Merge context parameters with top-level parameters (top-level takes priority)
    let mut context = req.context.unwrap_or_default();

    // Top-level system_prompt overrides context.system_prompt
    if let Some(ref system_prompt) = req.system_prompt {
        context.system_prompt = Some(system_prompt.clone());
    }

    // Top-level model overrides context.model_override
    if let Some(ref model) = req.model {
        context.model_override = Some(model.clone());
    }

    // Validate context parameters
    if let Err(e) = context.validate() {
        tracing::warn!("❌ Invalid context parameters - error={}", e);
        timer.finish();
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({
                "error": "invalid_context",
                "message": e
            })),
        )
            .into_response();
    }

    tracing::info!(
        "📥 New task submission - task_id={}, type={}, prompt_len={}, session_id={:?}, tier={:?}, strategy={:?}",
        task_id,
        req.task_type,
        req.prompt.len(),
        req.session_id,
        context.model_tier,
        context.research_strategy
    );
    tracing::debug!("📝 Task prompt preview: {}", prompt_preview);

    if context.role.is_some() {
        tracing::debug!("🎭 Role-based execution enabled - role={:?}", context.role);
    }

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

    // In embedded mode (no Redis), register with run_manager for tracking
    if state.redis.is_none() {
        tracing::debug!("🏠 Embedded mode detected - registering with run_manager");

        let registration_timer = OpTimer::new("run_manager", "registration");
        match state
            .run_manager
            .start_run_with_id(
                task_id.clone(),
                req.prompt.clone(),
                req.session_id.clone(),
                None, // user_id
            )
            .await
        {
            Ok(_) => {
                registration_timer.finish();
                tracing::info!("✅ Task registered with run_manager - task_id={}", task_id);
            }
            Err(e) => {
                tracing::error!(
                    "❌ Failed to register task with run_manager - task_id={}, error={}",
                    task_id,
                    e
                );
            }
        }
    }

    // Store task in Redis if available
    if let Some(ref redis) = state.redis {
        let redis_timer = OpTimer::new("redis", "store_task");
        let mut redis = redis.clone();
        let key = format!("task:{}", task_id);

        tracing::debug!("💾 Storing task in Redis - key={}", key);

        match redis::AsyncCommands::set_ex::<_, _, ()>(
            &mut redis,
            &key,
            task.to_string(),
            86400, // 24 hours TTL
        )
        .await
        {
            Ok(_) => {
                redis_timer.finish();
                tracing::info!("✅ Task stored in Redis - task_id={}, ttl=24h", task_id);
            }
            Err(e) => {
                tracing::error!(
                    "❌ Failed to store task in Redis - task_id={}, error={}",
                    task_id,
                    e
                );
            }
        }

        // Also add to task queue for processing
        let queue_key = "task_queue";
        tracing::trace!(
            "📋 Adding task to queue - queue={}, task_id={}",
            queue_key,
            task_id
        );
        let _ = redis::AsyncCommands::lpush::<_, _, ()>(&mut redis, queue_key, &task_id).await;
    }

    // Submit to workflow engine (local Durable or remote Temporal based on mode)
    let strategy = if req.task_type == "research" {
        crate::workflow::task::Strategy::Research
    } else {
        crate::workflow::task::Strategy::Simple
    };

    tracing::debug!(
        "🎬 Preparing workflow submission - task_id={}, strategy={:?}, engine={}",
        task_id,
        strategy,
        state.workflow_engine.engine_type()
    );

    let workflow_task = crate::workflow::Task {
        id: task_id.clone(),
        query: req.prompt.clone(),
        user_id: "default".to_string(), // TODO: Get from auth context
        session_id: req.session_id.clone(),
        context: req.metadata.clone().unwrap_or(serde_json::json!({})),
        strategy,
        labels: std::collections::HashMap::new(),
        max_agents: None,
        token_budget: None,
        require_approval: false,
    };

    let workflow_timer = OpTimer::new("workflow_engine", "submit");
    match state.workflow_engine.submit(workflow_task).await {
        Ok(handle) => {
            workflow_timer.finish();

            tracing::info!(
                "✅ Task submitted to workflow engine - task_id={}, engine={}, workflow_id={}",
                task_id,
                state.workflow_engine.engine_type(),
                handle.workflow_id
            );

            // Update task status in Redis with workflow_id
            if let Some(ref redis) = state.redis {
                let mut redis = redis.clone();
                let key = format!("task:{}", task_id);

                tracing::debug!(
                    "💾 Updating task status in Redis - task_id={}, status=running",
                    task_id
                );

                let mut task_with_workflow = task.clone();
                task_with_workflow["workflow_id"] = serde_json::Value::String(handle.workflow_id);
                task_with_workflow["status"] = serde_json::Value::String("running".to_string());
                task_with_workflow["started_at"] =
                    serde_json::Value::String(chrono::Utc::now().to_rfc3339());

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
            tracing::error!(
                "❌ Failed to submit task to workflow engine - task_id={}, engine={}, error={}",
                task_id,
                state.workflow_engine.engine_type(),
                e
            );

            // Update task status to failed in Redis
            if let Some(ref redis) = state.redis {
                let mut redis = redis.clone();
                let key = format!("task:{}", task_id);

                tracing::debug!(
                    "💾 Updating task status in Redis - task_id={}, status=failed",
                    task_id
                );

                let mut failed_task = task.clone();
                failed_task["status"] = serde_json::Value::String("failed".to_string());
                failed_task["error"] = serde_json::Value::String(format!(
                    "Failed to submit to workflow engine: {}",
                    e
                ));

                let _ = redis::AsyncCommands::set_ex::<_, _, ()>(
                    &mut redis,
                    &key,
                    failed_task.to_string(),
                    86400,
                )
                .await;
            }

            timer.finish();

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
            )
                .into_response();
        }
    }

    timer.finish();

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

    tracing::info!(
        "✅ Task submission complete - task_id={}, status=pending",
        response.id
    );

    (StatusCode::ACCEPTED, Json(response)).into_response()
}

/// Get task status.
pub async fn get_task_status(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    tracing::debug!("🔍 Looking up task status - task_id={}", id);

    // First check in-memory run manager (works in embedded mode)
    if let Some(run) = state.run_manager.get_run(&id) {
        use crate::domain::RunStatus;

        tracing::debug!(
            "✅ Found task in run_manager - task_id={}, status={:?}",
            id,
            run.status
        );

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

        tracing::info!(
            "✅ Task status retrieved (run_manager) - task_id={}, status={:?}",
            id,
            status
        );

        return (
            StatusCode::OK,
            Json(serde_json::to_value(response).unwrap()),
        );
    }

    tracing::debug!("Task not in run_manager, checking Redis - task_id={}", id);

    // Fall back to Redis if available
    if let Some(ref redis) = state.redis {
        let mut redis = redis.clone();
        let key = format!("task:{}", id);

        tracing::trace!("💾 Querying Redis - key={}", key);

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

                    tracing::info!(
                        "✅ Task status retrieved (Redis) - task_id={}, status={:?}",
                        id,
                        status
                    );

                    return (
                        StatusCode::OK,
                        Json(serde_json::to_value(response).unwrap()),
                    );
                }
            }
            Ok(None) => {
                tracing::debug!("Task not found in Redis - task_id={}", id);
            }
            Err(e) => {
                tracing::error!("❌ Redis error - task_id={}, error={}", id, e);
            }
        }
    }

    tracing::warn!("❌ Task not found - task_id={}", id);

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
    let timer = OpTimer::new("gateway", "cancel_task");

    tracing::info!("🛑 Task cancellation request - task_id={}", id);

    if let Some(ref redis) = state.redis {
        let mut redis = redis.clone();
        let key = format!("task:{}", id);

        tracing::debug!("💾 Fetching task from Redis - key={}", key);

        // Get existing task
        match redis::AsyncCommands::get::<_, Option<String>>(&mut redis, &key).await {
            Ok(Some(data)) => {
                if let Ok(mut task) = serde_json::from_str::<serde_json::Value>(&data) {
                    // Check if task can be cancelled
                    let current_status = task["status"].as_str().unwrap_or("pending");

                    tracing::debug!(
                        "Current task status - task_id={}, status={}",
                        id,
                        current_status
                    );

                    if current_status == "completed"
                        || current_status == "failed"
                        || current_status == "cancelled"
                    {
                        tracing::warn!(
                            "❌ Cannot cancel task - task_id={}, status={}",
                            id,
                            current_status
                        );

                        timer.finish();

                        return (
                            StatusCode::CONFLICT,
                            Json(serde_json::json!({
                                "error": "cannot_cancel",
                                "message": format!("Task {} is already {}", id, current_status)
                            })),
                        );
                    }

                    tracing::debug!(
                        "💾 Updating task status in Redis - task_id={}, new_status=cancelled",
                        id
                    );

                    // Update task status
                    task["status"] = serde_json::Value::String("cancelled".to_string());
                    task["updated_at"] = serde_json::Value::String(chrono::Utc::now().to_rfc3339());
                    task["completed_at"] =
                        serde_json::Value::String(chrono::Utc::now().to_rfc3339());

                    // Save back
                    let _ = redis::AsyncCommands::set_ex::<_, _, ()>(
                        &mut redis,
                        &key,
                        task.to_string(),
                        86400,
                    )
                    .await;

                    // Send cancel signal to workflow engine
                    tracing::debug!(
                        "📞 Sending cancellation to workflow engine - task_id={}",
                        id
                    );

                    let cancel_timer = OpTimer::new("workflow_engine", "cancel");
                    match state
                        .workflow_engine
                        .cancel(&id, Some("User requested cancellation"))
                        .await
                    {
                        Ok(cancelled) => {
                            cancel_timer.finish();
                            tracing::info!(
                                "✅ Workflow engine cancellation - task_id={}, success={}",
                                id,
                                cancelled
                            );
                        }
                        Err(e) => {
                            tracing::warn!(
                                "⚠️  Failed to cancel task in workflow engine - task_id={}, error={}",
                                id,
                                e
                            );
                        }
                    }

                    timer.finish();

                    tracing::info!("✅ Task cancelled successfully - task_id={}", id);

                    return (
                        StatusCode::OK,
                        Json(serde_json::json!({
                            "cancelled": true,
                            "id": id
                        })),
                    );
                }
            }
            Ok(None) => {
                tracing::debug!("Task not found in Redis - task_id={}", id);
            }
            Err(e) => {
                tracing::error!("❌ Redis error - task_id={}, error={}", id, e);
            }
        }
    }

    timer.finish();

    tracing::warn!("❌ Task not found for cancellation - task_id={}", id);

    (
        StatusCode::NOT_FOUND,
        Json(serde_json::json!({
            "error": "not_found",
            "message": format!("Task {} not found", id)
        })),
    )
}

/// Task control response.
#[derive(Debug, Serialize)]
pub struct TaskControlResponse {
    pub success: bool,
    pub task_id: String,
    pub action: String,
    pub message: Option<String>,
}

/// Pause a task.
///
/// # Errors
///
/// Returns 404 if task not found, 409 if task cannot be paused, 503 if database not available.
pub async fn pause_task(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    let timer = OpTimer::new("gateway", "pause_task");

    tracing::info!("⏸️  Task pause request - task_id={}", id);

    // Ensure database is available (embedded mode only)
    let Some(ref database) = state.database else {
        tracing::warn!("❌ Database not available - embedded mode required");
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "not_available",
                "message": "Pause functionality requires embedded mode"
            })),
        )
            .into_response();
    };

    // Update control state in database
    match database
        .update_pause(
            &id,
            true,
            Some("User requested pause".to_string()),
            Some("user".to_string()),
        )
        .await
    {
        Ok(_) => {
            tracing::info!("✅ Task paused successfully - task_id={}", id);

            timer.finish();

            (
                StatusCode::OK,
                Json(TaskControlResponse {
                    success: true,
                    task_id: id.clone(),
                    action: "pause".to_string(),
                    message: Some("Task paused".to_string()),
                }),
            )
                .into_response()
        }
        Err(e) => {
            tracing::error!("❌ Failed to pause task - task_id={}, error={}", id, e);

            timer.finish();

            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({
                    "error": "internal_error",
                    "message": "Failed to pause task"
                })),
            )
                .into_response()
        }
    }
}

/// Resume a paused task.
///
/// # Errors
///
/// Returns 404 if task not found, 409 if task is not paused, 503 if database not available.
pub async fn resume_task(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    let timer = OpTimer::new("gateway", "resume_task");

    tracing::info!("▶️  Task resume request - task_id={}", id);

    // Ensure database is available (embedded mode only)
    let Some(ref database) = state.database else {
        tracing::warn!("❌ Database not available - embedded mode required");
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "not_available",
                "message": "Resume functionality requires embedded mode"
            })),
        )
            .into_response();
    };

    // Update control state in database
    match database.update_pause(&id, false, None, None).await {
        Ok(_) => {
            tracing::info!("✅ Task resumed successfully - task_id={}", id);

            timer.finish();

            (
                StatusCode::OK,
                Json(TaskControlResponse {
                    success: true,
                    task_id: id.clone(),
                    action: "resume".to_string(),
                    message: Some("Task resumed".to_string()),
                }),
            )
                .into_response()
        }
        Err(e) => {
            tracing::error!("❌ Failed to resume task - task_id={}, error={}", id, e);

            timer.finish();

            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({
                    "error": "internal_error",
                    "message": "Failed to resume task"
                })),
            )
                .into_response()
        }
    }
}

/// Get task progress.
pub async fn get_task_progress(
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

        // Calculate progress based on status
        let progress_percent = match run.status {
            RunStatus::Completed => 100,
            RunStatus::Running => 50, // In progress
            RunStatus::Failed | RunStatus::Cancelled => 100,
            RunStatus::Pending => 0,
        };

        let response = TaskProgressResponse {
            id: id.clone(),
            status,
            progress_percent,
            current_step: Some(format!("{:?}", run.status)),
            total_steps: Some(1),
            completed_steps: Some(if run.status == RunStatus::Completed {
                1
            } else {
                0
            }),
            estimated_remaining_secs: None,
            subtasks: vec![],
        };

        return (
            StatusCode::OK,
            Json(serde_json::to_value(response).unwrap()),
        );
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

                    return (
                        StatusCode::OK,
                        Json(serde_json::to_value(response).unwrap()),
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

/// Get control state for a task/workflow.
///
/// # Errors
///
/// Returns 404 if workflow not found, 500 if database operation fails.
pub async fn get_control_state(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    tracing::debug!("🔍 Getting control state - task_id={}", id);

    // Ensure database is available (embedded mode only)
    let Some(ref database) = state.database else {
        tracing::warn!("❌ Database not available - embedded mode required");
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "not_available",
                "message": "Control state API requires embedded mode"
            })),
        )
            .into_response();
    };

    match database.get_control_state(&id).await {
        Ok(Some(control_state)) => {
            tracing::info!(
                "✅ Control state retrieved - task_id={}, paused={}, cancelled={}",
                id,
                control_state.is_paused,
                control_state.is_cancelled
            );
            (
                StatusCode::OK,
                Json(serde_json::to_value(control_state).unwrap()),
            )
                .into_response()
        }
        Ok(None) => {
            tracing::debug!("⚠️  Control state not found - task_id={}", id);
            (
                StatusCode::NOT_FOUND,
                Json(serde_json::json!({
                    "error": "not_found",
                    "message": format!("Control state for task {} not found", id)
                })),
            )
                .into_response()
        }
        Err(e) => {
            tracing::error!(
                "❌ Failed to get control state - task_id={}, error={}",
                id,
                e
            );
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({
                    "error": "internal_error",
                    "message": "Failed to retrieve control state"
                })),
            )
                .into_response()
        }
    }
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
