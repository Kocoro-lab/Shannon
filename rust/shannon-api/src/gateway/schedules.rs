//! Schedule management API endpoints.
//!
//! Provides CRUD operations for managing scheduled tasks.

use axum::{
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
    Json,
};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::scheduler::{CronParser, Schedule, ScheduleExecutor};

/// Request to create a new schedule.
#[derive(Debug, Deserialize)]
pub struct CreateScheduleRequest {
    /// Optional schedule name.
    pub name: Option<String>,
    /// Cron expression.
    pub cron: String,
    /// Task query to execute.
    pub query: String,
    /// Task strategy.
    pub strategy: String,
}

/// Request to update a schedule.
#[derive(Debug, Deserialize)]
pub struct UpdateScheduleRequest {
    /// Optional new name.
    pub name: Option<String>,
    /// Optional new cron expression.
    pub cron: Option<String>,
    /// Optional new query.
    pub query: Option<String>,
    /// Optional new strategy.
    pub strategy: Option<String>,
    /// Optional enabled flag.
    pub enabled: Option<bool>,
}

/// Schedule response.
#[derive(Debug, Serialize)]
pub struct ScheduleResponse {
    /// Schedule ID.
    pub id: String,
    /// Schedule name.
    pub name: Option<String>,
    /// Cron expression.
    pub cron: String,
    /// Task query.
    pub query: String,
    /// Task strategy.
    pub strategy: String,
    /// Enabled flag.
    pub enabled: bool,
    /// Creation timestamp.
    pub created_at: String,
    /// Last update timestamp.
    pub updated_at: String,
    /// Last run timestamp.
    pub last_run_at: Option<String>,
    /// Next run timestamp.
    pub next_run_at: Option<String>,
}

impl From<Schedule> for ScheduleResponse {
    fn from(schedule: Schedule) -> Self {
        Self {
            id: schedule.id,
            name: schedule.name,
            cron: schedule.cron,
            query: schedule.query,
            strategy: schedule.strategy,
            enabled: schedule.enabled,
            created_at: schedule.created_at.to_rfc3339(),
            updated_at: schedule.updated_at.to_rfc3339(),
            last_run_at: schedule.last_run_at.map(|dt| dt.to_rfc3339()),
            next_run_at: schedule.next_run_at.map(|dt| dt.to_rfc3339()),
        }
    }
}

/// Create a new schedule.
///
/// # Endpoint
///
/// `POST /api/v1/schedules`
pub async fn create_schedule(
    State(executor): State<ScheduleExecutor>,
    Json(req): Json<CreateScheduleRequest>,
) -> Result<impl IntoResponse, (StatusCode, String)> {
    // Validate cron expression
    let cron_expr = CronParser::parse(&req.cron).map_err(|e| {
        (
            StatusCode::BAD_REQUEST,
            format!("Invalid cron expression: {e}"),
        )
    })?;

    let now = chrono::Utc::now();
    let schedule = Schedule {
        id: Uuid::new_v4().to_string(),
        user_id: "embedded_user".to_string(), // TODO: Extract from auth context
        name: req.name,
        cron: req.cron,
        query: req.query,
        strategy: req.strategy,
        enabled: true,
        created_at: now,
        updated_at: now,
        last_run_at: None,
        next_run_at: cron_expr.next_after(&now),
    };

    executor
        .add_schedule(schedule.clone())
        .await
        .map_err(|e| (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()))?;

    Ok((StatusCode::CREATED, Json(ScheduleResponse::from(schedule))))
}

/// List all schedules.
///
/// # Endpoint
///
/// `GET /api/v1/schedules`
pub async fn list_schedules(
    State(executor): State<ScheduleExecutor>,
) -> Result<impl IntoResponse, (StatusCode, String)> {
    let schedules = executor.list_schedules().await;
    let responses: Vec<ScheduleResponse> = schedules.into_iter().map(Into::into).collect();
    Ok(Json(responses))
}

/// Get a schedule by ID.
///
/// # Endpoint
///
/// `GET /api/v1/schedules/:id`
pub async fn get_schedule(
    State(executor): State<ScheduleExecutor>,
    Path(id): Path<String>,
) -> Result<impl IntoResponse, (StatusCode, String)> {
    let schedule = executor
        .get_schedule(&id)
        .await
        .ok_or_else(|| (StatusCode::NOT_FOUND, "Schedule not found".to_string()))?;

    Ok(Json(ScheduleResponse::from(schedule)))
}

/// Update a schedule.
///
/// # Endpoint
///
/// `PATCH /api/v1/schedules/:id`
pub async fn update_schedule(
    State(executor): State<ScheduleExecutor>,
    Path(id): Path<String>,
    Json(req): Json<UpdateScheduleRequest>,
) -> Result<impl IntoResponse, (StatusCode, String)> {
    let mut schedule = executor
        .get_schedule(&id)
        .await
        .ok_or_else(|| (StatusCode::NOT_FOUND, "Schedule not found".to_string()))?;

    // Update fields
    if let Some(name) = req.name {
        schedule.name = Some(name);
    }
    if let Some(cron) = req.cron {
        // Validate new cron expression
        let cron_expr = CronParser::parse(&cron).map_err(|e| {
            (
                StatusCode::BAD_REQUEST,
                format!("Invalid cron expression: {e}"),
            )
        })?;
        schedule.cron = cron;
        schedule.next_run_at = cron_expr.next_after(&chrono::Utc::now());
    }
    if let Some(query) = req.query {
        schedule.query = query;
    }
    if let Some(strategy) = req.strategy {
        schedule.strategy = strategy;
    }
    if let Some(enabled) = req.enabled {
        schedule.enabled = enabled;
    }

    schedule.updated_at = chrono::Utc::now();

    executor
        .add_schedule(schedule.clone())
        .await
        .map_err(|e| (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()))?;

    Ok(Json(ScheduleResponse::from(schedule)))
}

/// Delete a schedule.
///
/// # Endpoint
///
/// `DELETE /api/v1/schedules/:id`
pub async fn delete_schedule(
    State(executor): State<ScheduleExecutor>,
    Path(id): Path<String>,
) -> Result<impl IntoResponse, (StatusCode, String)> {
    let deleted = executor.remove_schedule(&id).await;
    if deleted {
        Ok(StatusCode::NO_CONTENT)
    } else {
        Err((StatusCode::NOT_FOUND, "Schedule not found".to_string()))
    }
}

/// Get runs for a schedule.
///
/// # Endpoint
///
/// `GET /api/v1/schedules/:id/runs`
pub async fn get_schedule_runs(
    State(executor): State<ScheduleExecutor>,
    Path(id): Path<String>,
) -> Result<impl IntoResponse, (StatusCode, String)> {
    // Verify schedule exists
    executor
        .get_schedule(&id)
        .await
        .ok_or_else(|| (StatusCode::NOT_FOUND, "Schedule not found".to_string()))?;

    let runs = executor.get_runs(&id, 100).await;
    Ok(Json(runs))
}
