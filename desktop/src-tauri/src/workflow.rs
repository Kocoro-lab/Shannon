//! Tauri workflow commands for embedded workflow engine.
//!
//! Provides frontend-backend communication for workflow operations using
//! Tauri 2.x command patterns with async state management.
//!
//! # Architecture
//!
//! - Uses `State<'_, WorkflowEngineState>` for shared engine access
//! - Commands are async for non-blocking operations
//! - Returns `Result<T, String>` for frontend-compatible errors
//! - Compatible with both embedded and cloud modes via feature flags

use std::sync::Arc;

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Emitter, State};

#[cfg(feature = "desktop")]
use shannon_api::workflow::embedded::{EmbeddedWorkflowEngine, WorkflowEvent};

/// Workflow submission request.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SubmitWorkflowRequest {
    /// Pattern type to execute.
    pub pattern_type: String,

    /// User query.
    pub query: String,

    /// Optional session ID for context.
    pub session_id: Option<String>,

    /// Optional mode override.
    pub mode: Option<String>,

    /// Optional model override.
    pub model: Option<String>,
}

/// Workflow submission response.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SubmitWorkflowResponse {
    /// Workflow ID for tracking.
    pub workflow_id: String,

    /// Initial status.
    pub status: String,

    /// Timestamp.
    pub submitted_at: String,
}

/// Workflow status response.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorkflowStatusResponse {
    /// Workflow ID.
    pub workflow_id: String,

    /// Current status.
    pub status: String,

    /// Progress percentage (0-100).
    pub progress: u8,

    /// Output if completed.
    pub output: Option<String>,

    /// Error if failed.
    pub error: Option<String>,
}

/// Workflow history entry.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorkflowHistoryEntry {
    /// Workflow ID.
    pub workflow_id: String,

    /// Pattern type.
    pub pattern_type: String,

    /// Status.
    pub status: String,

    /// Created timestamp.
    pub created_at: String,

    /// Completed timestamp.
    pub completed_at: Option<String>,
}

/// Workflow engine state for Tauri commands.
#[derive(Clone)]
pub struct WorkflowEngineState {
    #[cfg(feature = "desktop")]
    engine: Option<Arc<EmbeddedWorkflowEngine>>,
}

impl WorkflowEngineState {
    /// Create a new workflow engine state.
    #[must_use]
    pub fn new() -> Self {
        Self {
            #[cfg(feature = "desktop")]
            engine: None,
        }
    }

    /// Set the embedded workflow engine.
    #[cfg(feature = "desktop")]
    pub fn set_engine(&mut self, engine: Arc<EmbeddedWorkflowEngine>) {
        self.engine = Some(engine);
    }

    /// Get the embedded workflow engine.
    #[cfg(feature = "desktop")]
    pub fn engine(&self) -> Result<&Arc<EmbeddedWorkflowEngine>, String> {
        self.engine
            .as_ref()
            .ok_or_else(|| "Workflow engine not initialized".to_string())
    }
}

impl Default for WorkflowEngineState {
    fn default() -> Self {
        Self::new()
    }
}

/// Submit a workflow for execution.
///
/// # Errors
///
/// Returns error if workflow submission fails.
#[tauri::command]
pub async fn submit_workflow(
    request: SubmitWorkflowRequest,
    _app: AppHandle,
    #[cfg(feature = "desktop")] state: State<'_, WorkflowEngineState>,
) -> Result<SubmitWorkflowResponse, String> {
    #[cfg(feature = "desktop")]
    {
        let engine = state.engine()?;

        // Create task input
        let task_input = serde_json::json!({
            "query": request.query,
            "session_id": request.session_id.clone(),
            "mode": request.mode,
            "model": request.model,
        });
        let input_str = task_input.to_string();

        // Submit workflow
        let workflow_id = engine
            .submit_task(
                "user",
                request.session_id.as_deref(),
                &request.pattern_type,
                &input_str,
            )
            .await
            .map_err(|e| e.to_string())?;

        Ok(SubmitWorkflowResponse {
            workflow_id,
            status: "running".to_string(),
            submitted_at: chrono::Utc::now().to_rfc3339(),
        })
    }

    #[cfg(not(feature = "desktop"))]
    {
        let _ = (request, _app);
        Err("Workflow engine not available in cloud mode".to_string())
    }
}

/// Get workflow status.
///
/// # Errors
///
/// Returns error if workflow not found.
#[tauri::command]
pub async fn get_workflow_status(
    workflow_id: String,
    #[cfg(feature = "desktop")] state: State<'_, WorkflowEngineState>,
) -> Result<WorkflowStatusResponse, String> {
    #[cfg(feature = "desktop")]
    {
        let engine = state.engine()?;

        let workflow = engine
            .get_workflow(&workflow_id)
            .await
            .map_err(|e| e.to_string())?
            .ok_or_else(|| "Workflow not found".to_string())?;

        Ok(WorkflowStatusResponse {
            workflow_id: workflow_id.clone(),
            status: format!("{:?}", workflow.status),
            progress: 0, // TODO: Get actual progress
            output: workflow.output,
            error: workflow.error,
        })
    }

    #[cfg(not(feature = "desktop"))]
    {
        let _ = workflow_id;
        Err("Workflow engine not available in cloud mode".to_string())
    }
}

/// Stream workflow events.
///
/// Note: For Tauri 2.x, event streaming is better handled via Tauri events
/// rather than command return values. This command initiates streaming.
///
/// # Errors
///
/// Returns error if workflow not found.
#[tauri::command]
pub async fn stream_workflow_events(
    workflow_id: String,
    app: AppHandle,
    #[cfg(feature = "desktop")] state: State<'_, WorkflowEngineState>,
) -> Result<(), String> {
    #[cfg(feature = "desktop")]
    {
        let engine = state.engine()?;

        // Subscribe to workflow events
        let mut events = engine.stream_events(&workflow_id);

        // Spawn task to emit events to frontend
        tauri::async_runtime::spawn(async move {
            while let Ok(event) = events.recv().await {
                let event_name = format!("workflow-event-{}", workflow_id);
                let _ = app.emit(&event_name, &event);
            }
        });

        Ok(())
    }

    #[cfg(not(feature = "desktop"))]
    {
        let _ = (workflow_id, app);
        Err("Workflow engine not available in cloud mode".to_string())
    }
}

/// Pause a running workflow.
///
/// # Errors
///
/// Returns error if workflow cannot be paused.
#[tauri::command]
pub async fn pause_workflow(
    workflow_id: String,
    #[cfg(feature = "desktop")] state: State<'_, WorkflowEngineState>,
) -> Result<(), String> {
    #[cfg(feature = "desktop")]
    {
        let engine = state.engine()?;

        engine
            .pause_workflow(&workflow_id)
            .await
            .map_err(|e| e.to_string())?;

        Ok(())
    }

    #[cfg(not(feature = "desktop"))]
    {
        let _ = workflow_id;
        Err("Workflow engine not available in cloud mode".to_string())
    }
}

/// Resume a paused workflow.
///
/// # Errors
///
/// Returns error if workflow cannot be resumed.
#[tauri::command]
pub async fn resume_workflow(
    workflow_id: String,
    #[cfg(feature = "desktop")] state: State<'_, WorkflowEngineState>,
) -> Result<(), String> {
    #[cfg(feature = "desktop")]
    {
        let engine = state.engine()?;

        engine
            .resume_workflow(&workflow_id)
            .await
            .map_err(|e| e.to_string())?;

        Ok(())
    }

    #[cfg(not(feature = "desktop"))]
    {
        let _ = workflow_id;
        Err("Workflow engine not available in cloud mode".to_string())
    }
}

/// Cancel a running workflow.
///
/// # Errors
///
/// Returns error if workflow cannot be cancelled.
#[tauri::command]
pub async fn cancel_workflow(
    workflow_id: String,
    #[cfg(feature = "desktop")] state: State<'_, WorkflowEngineState>,
) -> Result<(), String> {
    #[cfg(feature = "desktop")]
    {
        let engine = state.engine()?;

        engine
            .cancel_workflow(&workflow_id)
            .await
            .map_err(|e| e.to_string())?;

        Ok(())
    }

    #[cfg(not(feature = "desktop"))]
    {
        let _ = workflow_id;
        Err("Workflow engine not available in cloud mode".to_string())
    }
}

/// Get workflow history for a session or user.
///
/// # Errors
///
/// Returns error if history retrieval fails.
#[tauri::command]
pub async fn get_workflow_history(
    session_id: Option<String>,
    limit: Option<usize>,
    #[cfg(feature = "desktop")] state: State<'_, WorkflowEngineState>,
) -> Result<Vec<WorkflowHistoryEntry>, String> {
    #[cfg(feature = "desktop")]
    {
        let engine = state.engine()?;

        let workflows = engine
            .list_workflows(session_id, limit.unwrap_or(50))
            .await
            .map_err(|e| e.to_string())?;

        let history = workflows
            .into_iter()
            .map(|w| WorkflowHistoryEntry {
                workflow_id: w.workflow_id,
                pattern_type: w.pattern_type,
                status: format!("{:?}", w.status),
                created_at: chrono::DateTime::from_timestamp(w.created_at, 0)
                    .unwrap_or_else(chrono::Utc::now)
                    .to_rfc3339(),
                completed_at: w.completed_at.and_then(|ts| {
                    chrono::DateTime::from_timestamp(ts, 0).map(|dt| dt.to_rfc3339())
                }),
            })
            .collect();

        Ok(history)
    }

    #[cfg(not(feature = "desktop"))]
    {
        let _ = (session_id, limit);
        Err("Workflow engine not available in cloud mode".to_string())
    }
}

/// Export workflow to JSON for debugging.
///
/// # Errors
///
/// Returns error if export fails.
#[tauri::command]
pub async fn export_workflow(
    workflow_id: String,
    #[cfg(feature = "desktop")] state: State<'_, WorkflowEngineState>,
) -> Result<String, String> {
    #[cfg(feature = "desktop")]
    {
        let engine = state.engine()?;

        engine
            .export_workflow(&workflow_id)
            .await
            .map_err(|e| e.to_string())
    }

    #[cfg(not(feature = "desktop"))]
    {
        let _ = workflow_id;
        Err("Workflow engine not available in cloud mode".to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_workflow_engine_state_creation() {
        let state = WorkflowEngineState::new();
        assert!(state.engine().is_err());
    }

    #[test]
    fn test_submit_workflow_request_serialization() {
        let request = SubmitWorkflowRequest {
            pattern_type: "chain_of_thought".to_string(),
            query: "What is 2+2?".to_string(),
            session_id: Some("sess-123".to_string()),
            mode: None,
            model: None,
        };

        let json = serde_json::to_string(&request).unwrap();
        assert!(json.contains("chain_of_thought"));
        assert!(json.contains("What is 2+2?"));
    }

    #[test]
    fn test_workflow_status_response_serialization() {
        let response = WorkflowStatusResponse {
            workflow_id: "wf-123".to_string(),
            status: "running".to_string(),
            progress: 50,
            output: None,
            error: None,
        };

        let json = serde_json::to_string(&response).unwrap();
        assert!(json.contains("wf-123"));
        assert!(json.contains("\"progress\":50"));
    }
}
