//! Integration tests for Tauri workflow commands.

use app_lib::workflow::{
    SubmitWorkflowRequest, SubmitWorkflowResponse, WorkflowEngineState, WorkflowStatusResponse,
};

#[test]
fn test_workflow_engine_state_default() {
    let state = WorkflowEngineState::default();
    // State should be initialized
    #[cfg(feature = "desktop")]
    assert!(state.engine().is_err());
}

#[test]
fn test_submit_workflow_request_creation() {
    let request = SubmitWorkflowRequest {
        pattern_type: "chain_of_thought".to_string(),
        query: "Explain quantum computing".to_string(),
        session_id: Some("sess-123".to_string()),
        mode: Some("default".to_string()),
        model: Some("claude-sonnet-4".to_string()),
    };

    assert_eq!(request.pattern_type, "chain_of_thought");
    assert_eq!(request.query, "Explain quantum computing");
    assert_eq!(request.session_id, Some("sess-123".to_string()));
}

#[test]
fn test_submit_workflow_request_minimal() {
    let request = SubmitWorkflowRequest {
        pattern_type: "research".to_string(),
        query: "What is Rust?".to_string(),
        session_id: None,
        mode: None,
        model: None,
    };

    assert!(request.session_id.is_none());
    assert!(request.mode.is_none());
    assert!(request.model.is_none());
}

#[test]
fn test_submit_workflow_response_serialization() {
    let response = SubmitWorkflowResponse {
        workflow_id: "wf-abc-123".to_string(),
        status: "running".to_string(),
        submitted_at: "2024-01-01T00:00:00Z".to_string(),
    };

    let json = serde_json::to_string(&response).unwrap();
    let parsed: SubmitWorkflowResponse = serde_json::from_str(&json).unwrap();

    assert_eq!(parsed.workflow_id, "wf-abc-123");
    assert_eq!(parsed.status, "running");
}

#[test]
fn test_workflow_status_response_with_output() {
    let response = WorkflowStatusResponse {
        workflow_id: "wf-123".to_string(),
        status: "completed".to_string(),
        progress: 100,
        output: Some("Answer: 42".to_string()),
        error: None,
    };

    assert_eq!(response.progress, 100);
    assert!(response.output.is_some());
    assert!(response.error.is_none());
}

#[test]
fn test_workflow_status_response_with_error() {
    let response = WorkflowStatusResponse {
        workflow_id: "wf-456".to_string(),
        status: "failed".to_string(),
        progress: 50,
        output: None,
        error: Some("LLM API timeout".to_string()),
    };

    assert_eq!(response.status, "failed");
    assert!(response.output.is_none());
    assert!(response.error.is_some());
}

#[test]
fn test_workflow_request_json_roundtrip() {
    let original = SubmitWorkflowRequest {
        pattern_type: "tree_of_thoughts".to_string(),
        query: "Plan a project".to_string(),
        session_id: Some("sess-789".to_string()),
        mode: Some("exploratory".to_string()),
        model: None,
    };

    let json = serde_json::to_string(&original).unwrap();
    let parsed: SubmitWorkflowRequest = serde_json::from_str(&json).unwrap();

    assert_eq!(parsed.pattern_type, original.pattern_type);
    assert_eq!(parsed.query, original.query);
    assert_eq!(parsed.session_id, original.session_id);
    assert_eq!(parsed.mode, original.mode);
}

#[test]
fn test_workflow_status_json_roundtrip() {
    let original = WorkflowStatusResponse {
        workflow_id: "wf-test".to_string(),
        status: "running".to_string(),
        progress: 75,
        output: Some("Partial result".to_string()),
        error: None,
    };

    let json = serde_json::to_string(&original).unwrap();
    let parsed: WorkflowStatusResponse = serde_json::from_str(&json).unwrap();

    assert_eq!(parsed.workflow_id, original.workflow_id);
    assert_eq!(parsed.status, original.status);
    assert_eq!(parsed.progress, original.progress);
}

#[cfg(feature = "desktop")]
#[test]
fn test_workflow_engine_state_engine_not_initialized() {
    let state = WorkflowEngineState::new();
    let result = state.engine();
    assert!(result.is_err());
    assert!(result.unwrap_err().contains("not initialized"));
}

#[test]
fn test_multiple_workflow_requests() {
    let requests = vec![
        SubmitWorkflowRequest {
            pattern_type: "chain_of_thought".to_string(),
            query: "Query 1".to_string(),
            session_id: Some("sess-1".to_string()),
            mode: None,
            model: None,
        },
        SubmitWorkflowRequest {
            pattern_type: "research".to_string(),
            query: "Query 2".to_string(),
            session_id: Some("sess-1".to_string()),
            mode: None,
            model: None,
        },
        SubmitWorkflowRequest {
            pattern_type: "tree_of_thoughts".to_string(),
            query: "Query 3".to_string(),
            session_id: Some("sess-1".to_string()),
            mode: None,
            model: None,
        },
    ];

    for request in requests {
        let json = serde_json::to_string(&request).unwrap();
        assert!(json.contains(&request.pattern_type));
    }
}
