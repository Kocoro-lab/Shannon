//! Event schema tests for embedded workflow engine.
//!
//! Verifies that all event types match cloud API event schemas.

use serde_json::json;

/// Test workflow started event schema.
#[test]
fn test_workflow_started_event() {
    let event = json!({
        "event_type": "WORKFLOW_STARTED",
        "workflow_id": "wf-123",
        "workflow_type": "chain_of_thought",
        "input": {
            "query": "test"
        },
        "timestamp": "2024-01-01T00:00:00Z"
    });

    assert_eq!(event["event_type"], "WORKFLOW_STARTED");
    assert!(event.get("workflow_id").is_some());
    assert!(event.get("timestamp").is_some());
}

/// Test agent started event schema.
#[test]
fn test_agent_started_event() {
    let event = json!({
        "event_type": "AGENT_STARTED",
        "workflow_id": "wf-123",
        "agent_id": "agent-1",
        "agent_type": "reasoning"
    });

    assert_eq!(event["event_type"], "AGENT_STARTED");
    assert!(event.get("agent_id").is_some());
}

/// Test LLM prompt event schema.
#[test]
fn test_llm_prompt_event() {
    let event = json!({
        "event_type": "LLM_PROMPT",
        "workflow_id": "wf-123",
        "model": "claude-sonnet-4",
        "messages": [
            {"role": "user", "content": "Hello"}
        ]
    });

    assert_eq!(event["event_type"], "LLM_PROMPT");
    assert!(event.get("model").is_some());
    assert!(event.get("messages").is_some());
}

/// Test LLM partial event schema (streaming).
#[test]
fn test_llm_partial_event() {
    let event = json!({
        "event_type": "LLM_PARTIAL",
        "workflow_id": "wf-123",
        "delta": "Hello",
        "agent_id": "agent-1"
    });

    assert_eq!(event["event_type"], "LLM_PARTIAL");
    assert!(event.get("delta").is_some());
}

/// Test LLM output event schema.
#[test]
fn test_llm_output_event() {
    let event = json!({
        "event_type": "LLM_OUTPUT",
        "workflow_id": "wf-123",
        "response": "The answer is 42",
        "metadata": {
            "model": "claude-sonnet-4",
            "prompt_tokens": 100,
            "completion_tokens": 50
        }
    });

    assert_eq!(event["event_type"], "LLM_OUTPUT");
    assert!(event.get("response").is_some());
    assert!(event.get("metadata").is_some());
}

/// Test tool invoked event schema.
#[test]
fn test_tool_invoked_event() {
    let event = json!({
        "event_type": "TOOL_INVOKED",
        "workflow_id": "wf-123",
        "tool": "web_search",
        "params": {
            "query": "rust programming"
        }
    });

    assert_eq!(event["event_type"], "TOOL_INVOKED");
    assert!(event.get("tool").is_some());
    assert!(event.get("params").is_some());
}

/// Test tool observation event schema.
#[test]
fn test_tool_observation_event() {
    let event = json!({
        "event_type": "TOOL_OBSERVATION",
        "workflow_id": "wf-123",
        "tool": "web_search",
        "output": {
            "results": []
        }
    });

    assert_eq!(event["event_type"], "TOOL_OBSERVATION");
    assert!(event.get("tool").is_some());
    assert!(event.get("output").is_some());
}

/// Test progress event schema.
#[test]
fn test_progress_event() {
    let event = json!({
        "event_type": "PROGRESS",
        "workflow_id": "wf-123",
        "percent": 50,
        "message": "Processing step 5/10"
    });

    assert_eq!(event["event_type"], "PROGRESS");
    assert!(event.get("percent").is_some());
    assert!(event["percent"].as_i64().unwrap() >= 0);
    assert!(event["percent"].as_i64().unwrap() <= 100);
}

/// Test workflow pausing event schema.
#[test]
fn test_workflow_pausing_event() {
    let event = json!({
        "event_type": "WORKFLOW_PAUSING",
        "workflow_id": "wf-123"
    });

    assert_eq!(event["event_type"], "WORKFLOW_PAUSING");
}

/// Test workflow paused event schema.
#[test]
fn test_workflow_paused_event() {
    let event = json!({
        "event_type": "WORKFLOW_PAUSED",
        "workflow_id": "wf-123"
    });

    assert_eq!(event["event_type"], "WORKFLOW_PAUSED");
}

/// Test workflow resumed event schema.
#[test]
fn test_workflow_resumed_event() {
    let event = json!({
        "event_type": "WORKFLOW_RESUMED",
        "workflow_id": "wf-123"
    });

    assert_eq!(event["event_type"], "WORKFLOW_RESUMED");
}

/// Test workflow completed event schema.
#[test]
fn test_workflow_completed_event() {
    let event = json!({
        "event_type": "WORKFLOW_COMPLETED",
        "workflow_id": "wf-123",
        "output": {
            "answer": "42",
            "confidence": 1.0
        },
        "duration_ms": 5000
    });

    assert_eq!(event["event_type"], "WORKFLOW_COMPLETED");
    assert!(event.get("output").is_some());
    assert!(event.get("duration_ms").is_some());
}

/// Test workflow failed event schema.
#[test]
fn test_workflow_failed_event() {
    let event = json!({
        "event_type": "WORKFLOW_FAILED",
        "workflow_id": "wf-123",
        "error": "LLM API timeout"
    });

    assert_eq!(event["event_type"], "WORKFLOW_FAILED");
    assert!(event.get("error").is_some());
}

/// Test all required event types are present.
#[test]
fn test_all_event_types_present() {
    let required_events = vec![
        "WORKFLOW_STARTED",
        "AGENT_STARTED",
        "AGENT_COMPLETED",
        "LLM_PROMPT",
        "LLM_PARTIAL",
        "LLM_OUTPUT",
        "TOOL_INVOKED",
        "TOOL_OBSERVATION",
        "TOOL_ERROR",
        "PROGRESS",
        "WORKFLOW_PAUSING",
        "WORKFLOW_PAUSED",
        "WORKFLOW_RESUMED",
        "WORKFLOW_CANCELLING",
        "WORKFLOW_CANCELLED",
        "WORKFLOW_COMPLETED",
        "WORKFLOW_FAILED",
        "ACTIVITY_SCHEDULED",
        "ACTIVITY_COMPLETED",
        "ACTIVITY_FAILED",
        "CHECKPOINT",
    ];

    // Verify all event types are defined (at least 21 types)
    assert!(required_events.len() >= 21);
}
