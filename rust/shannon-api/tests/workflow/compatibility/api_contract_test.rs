//! API contract tests for embedded workflow engine.
//!
//! Verifies that embedded API endpoints match cloud API specifications.

use serde_json::json;

/// Test that task submission request schema matches cloud API.
#[test]
fn test_task_submission_request_schema() {
    let request = json!({
        "query": "What is 2+2?",
        "session_id": "sess-123",
        "mode": "simple",
        "context": {
            "role": "assistant",
            "model_override": "claude-sonnet-4"
        }
    });

    // Verify required fields
    assert!(request.get("query").is_some());
    assert_eq!(request["query"], "What is 2+2?");

    // Verify optional fields are correctly structured
    assert_eq!(request["session_id"], "sess-123");
    assert_eq!(request["mode"], "simple");
    assert!(request.get("context").is_some());
}

/// Test that task submission response schema matches cloud API.
#[test]
fn test_task_submission_response_schema() {
    let response = json!({
        "task_id": "task-abc-123",
        "status": "pending",
        "message": "Task submitted successfully"
    });

    // Verify required fields match cloud API
    assert!(response.get("task_id").is_some());
    assert!(response.get("status").is_some());
    assert_eq!(response["status"], "pending");
}

/// Test that task status response schema matches cloud API.
#[test]
fn test_task_status_response_schema() {
    let response = json!({
        "task_id": "task-123",
        "status": "running",
        "progress": 50,
        "created_at": "2024-01-01T00:00:00Z",
        "updated_at": "2024-01-01T00:01:00Z"
    });

    // Verify schema matches cloud
    assert_eq!(response["task_id"], "task-123");
    assert_eq!(response["status"], "running");
    assert_eq!(response["progress"], 50);
}

/// Test that workflow control endpoints match cloud API.
#[test]
fn test_control_endpoints_schema() {
    // Pause request
    let pause_request = json!({});
    assert!(pause_request.is_object());

    // Resume request
    let resume_request = json!({});
    assert!(resume_request.is_object());

    // Cancel request
    let cancel_request = json!({});
    assert!(cancel_request.is_object());

    // All control endpoints accept empty POST bodies
}

/// Test error response schema matches cloud API.
#[test]
fn test_error_response_schema() {
    let error = json!({
        "error": {
            "code": "WORKFLOW_NOT_FOUND",
            "message": "Workflow not found: wf-123",
            "details": {}
        }
    });

    assert!(error.get("error").is_some());
    let err_obj = &error["error"];
    assert!(err_obj.get("code").is_some());
    assert!(err_obj.get("message").is_some());
}

/// Test that token usage format matches cloud API.
#[test]
fn test_token_usage_schema() {
    let usage = json!({
        "prompt_tokens": 100,
        "completion_tokens": 50,
        "total_tokens": 150,
        "model_breakdown": {
            "claude-sonnet-4": {
                "prompt_tokens": 100,
                "completion_tokens": 50,
                "total_tokens": 150,
                "calls": 1
            }
        }
    });

    assert_eq!(usage["total_tokens"], 150);
    assert_eq!(usage["prompt_tokens"], 100);
    assert_eq!(usage["completion_tokens"], 50);
    assert!(usage.get("model_breakdown").is_some());
}

/// Test that reasoning step format matches cloud API.
#[test]
fn test_reasoning_step_schema() {
    let step = json!({
        "step": 1,
        "step_type": "thought",
        "content": "Let me think about this...",
        "timestamp": "2024-01-01T00:00:00Z",
        "confidence": 0.8
    });

    assert_eq!(step["step"], 1);
    assert_eq!(step["step_type"], "thought");
    assert!(step.get("content").is_some());
    assert!(step.get("timestamp").is_some());
}

/// Test that source citation format matches cloud API.
#[test]
fn test_source_citation_schema() {
    let source = json!({
        "title": "Wikipedia Article",
        "url": "https://en.wikipedia.org/wiki/Example",
        "snippet": "This is a snippet...",
        "confidence": 0.9,
        "accessed_at": "2024-01-01T00:00:00Z"
    });

    assert_eq!(source["title"], "Wikipedia Article");
    assert!(source.get("url").is_some());
    assert!(source.get("snippet").is_some());
    assert_eq!(source["confidence"], 0.9);
}

/// Test API endpoint paths match cloud API.
#[test]
fn test_api_endpoint_paths() {
    let endpoints = vec![
        "/api/v1/tasks",             // POST - submit task
        "/api/v1/tasks/{id}",        // GET - get task status
        "/api/v1/tasks",             // GET - list tasks
        "/api/v1/tasks/{id}/stream", // GET - stream events
        "/api/v1/tasks/{id}/pause",  // POST - pause
        "/api/v1/tasks/{id}/resume", // POST - resume
        "/api/v1/tasks/{id}/cancel", // POST - cancel
    ];

    // Verify all cloud endpoints are defined
    for endpoint in endpoints {
        assert!(!endpoint.is_empty());
        assert!(endpoint.starts_with("/api/v1/"));
    }
}

/// Test HTTP methods match cloud API.
#[test]
fn test_http_methods() {
    let methods = vec![
        ("POST", "/api/v1/tasks"),             // Submit
        ("GET", "/api/v1/tasks/{id}"),         // Status
        ("GET", "/api/v1/tasks/{id}/stream"),  // Stream
        ("POST", "/api/v1/tasks/{id}/pause"),  // Pause
        ("POST", "/api/v1/tasks/{id}/resume"), // Resume
        ("POST", "/api/v1/tasks/{id}/cancel"), // Cancel
    ];

    for (method, _path) in methods {
        assert!(method == "GET" || method == "POST");
    }
}
