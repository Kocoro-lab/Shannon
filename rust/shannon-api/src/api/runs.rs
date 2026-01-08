//! Runs API endpoints.

use axum::{
    extract::{Path, State},
    routing::{get, post},
    Json, Router,
};
use serde::Serialize;

use crate::domain::Run;
use crate::AppState;

/// Create the runs router.
pub fn router() -> Router<AppState> {
    Router::new()
        .route("/v1/runs", get(list_runs))
        .route("/v1/runs/{run_id}", get(get_run))
        .route("/v1/runs/{run_id}/cancel", post(cancel_run))
}

/// List active runs.
async fn list_runs(State(state): State<AppState>) -> Json<Vec<RunResponse>> {
    let runs = state.run_manager.list_active_runs();
    Json(runs.into_iter().map(RunResponse::from).collect())
}

/// Get a specific run.
async fn get_run(
    State(state): State<AppState>,
    Path(run_id): Path<String>,
) -> Result<Json<RunResponse>, axum::http::StatusCode> {
    state
        .run_manager
        .get_run(&run_id)
        .map(|run| Json(RunResponse::from(run)))
        .ok_or(axum::http::StatusCode::NOT_FOUND)
}

/// Cancel a run.
async fn cancel_run(
    State(state): State<AppState>,
    Path(run_id): Path<String>,
) -> Result<Json<CancelResponse>, axum::http::StatusCode> {
    if state.run_manager.cancel_run(&run_id) {
        Ok(Json(CancelResponse {
            run_id,
            cancelled: true,
        }))
    } else {
        Err(axum::http::StatusCode::NOT_FOUND)
    }
}

/// Run response.
#[derive(Debug, Serialize)]
pub struct RunResponse {
    pub id: String,
    pub session_id: Option<String>,
    pub status: String,
    pub query: String,
    pub result: Option<String>,
    pub error: Option<String>,
    pub tokens_used: u32,
    pub cost_usd: f64,
    pub model: Option<String>,
    pub created_at: String,
    pub updated_at: String,
    pub completed_at: Option<String>,
}

impl From<Run> for RunResponse {
    fn from(run: Run) -> Self {
        Self {
            id: run.id,
            session_id: run.session_id,
            status: run.status.to_string(),
            query: run.query,
            result: run.result,
            error: run.error,
            tokens_used: run.tokens_used,
            cost_usd: run.cost_usd,
            model: run.model,
            created_at: run.created_at.to_rfc3339(),
            updated_at: run.updated_at.to_rfc3339(),
            completed_at: run.completed_at.map(|t| t.to_rfc3339()),
        }
    }
}

/// Cancel response.
#[derive(Debug, Serialize)]
pub struct CancelResponse {
    pub run_id: String,
    pub cancelled: bool,
}
