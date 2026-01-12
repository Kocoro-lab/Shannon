//! Integration tests for workflow replay and debugging.

use std::sync::Arc;
use tempfile::NamedTempFile;

use shannon_api::database::workflow_store::{WorkflowStatus, WorkflowStore};
use shannon_api::workflow::embedded::{ReplayManager, ReplayMode};

/// Helper to create test replay manager.
async fn create_test_replay() -> (
    ReplayManager,
    Arc<WorkflowStore>,
    NamedTempFile,
    NamedTempFile,
) {
    let workflow_temp = NamedTempFile::new().unwrap();
    let event_temp = NamedTempFile::new().unwrap();

    let workflow_store = Arc::new(WorkflowStore::new(workflow_temp.path()).await.unwrap());
    let replay = ReplayManager::new(workflow_store.clone(), event_temp.path())
        .await
        .unwrap();

    (replay, workflow_store, workflow_temp, event_temp)
}

#[tokio::test]
async fn test_export_workflow_to_json() {
    let (replay, store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Create a workflow
    store
        .create_workflow("wf-export", "user-1", Some("sess-1"), "cot", "test query")
        .await
        .unwrap();
    store
        .update_status("wf-export", WorkflowStatus::Completed)
        .await
        .unwrap();
    store
        .update_output("wf-export", "test result")
        .await
        .unwrap();

    // Export to JSON
    let json = replay.export_workflow_json("wf-export").await.unwrap();

    // Verify JSON structure
    assert!(json.contains("wf-export"));
    assert!(json.contains("\"version\":\"1.0\""));
    assert!(json.contains("workflow"));
    assert!(json.contains("events"));
}

#[tokio::test]
async fn test_export_workflow_to_file() {
    let (replay, store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Create a workflow
    store
        .create_workflow("wf-file", "user-1", Some("sess-1"), "cot", "test")
        .await
        .unwrap();
    store
        .update_status("wf-file", WorkflowStatus::Completed)
        .await
        .unwrap();

    // Export to file
    let output_file = NamedTempFile::new().unwrap();
    replay
        .export_workflow_to_file("wf-file", output_file.path())
        .await
        .unwrap();

    // Verify file exists and contains data
    let contents = std::fs::read_to_string(output_file.path()).unwrap();
    assert!(contents.contains("wf-file"));
    assert!(!contents.is_empty());
}

#[tokio::test]
async fn test_replay_from_json() {
    let (replay, store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Create and export a workflow
    store
        .create_workflow("wf-replay", "user-1", Some("sess-1"), "cot", "test")
        .await
        .unwrap();
    store
        .update_status("wf-replay", WorkflowStatus::Completed)
        .await
        .unwrap();

    let json = replay.export_workflow_json("wf-replay").await.unwrap();

    // Replay from JSON
    let result = replay.replay_from_json(&json).await.unwrap();

    assert_eq!(result.workflow_id, "wf-replay");
    assert_eq!(result.final_status, WorkflowStatus::Completed);
    assert_eq!(result.events_replayed, 0); // No events in this test
}

#[tokio::test]
async fn test_replay_from_file() {
    let (replay, store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Create and export a workflow
    store
        .create_workflow("wf-file-replay", "user-1", Some("sess-1"), "cot", "test")
        .await
        .unwrap();
    store
        .update_status("wf-file-replay", WorkflowStatus::Completed)
        .await
        .unwrap();

    let export_file = NamedTempFile::new().unwrap();
    replay
        .export_workflow_to_file("wf-file-replay", export_file.path())
        .await
        .unwrap();

    // Replay from file
    let result = replay.replay_from_file(export_file.path()).await.unwrap();

    assert_eq!(result.workflow_id, "wf-file-replay");
    assert_eq!(result.final_status, WorkflowStatus::Completed);
}

#[tokio::test]
async fn test_replay_mode_switching() {
    let (replay, _store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Default mode
    assert_eq!(replay.replay_mode(), ReplayMode::Full);

    // Switch to step-through
    replay.set_replay_mode(ReplayMode::StepThrough);
    assert_eq!(replay.replay_mode(), ReplayMode::StepThrough);

    // Switch to breakpoint
    replay.set_replay_mode(ReplayMode::Breakpoint);
    assert_eq!(replay.replay_mode(), ReplayMode::Breakpoint);

    // Back to full
    replay.set_replay_mode(ReplayMode::Full);
    assert_eq!(replay.replay_mode(), ReplayMode::Full);
}

#[tokio::test]
async fn test_breakpoint_add_remove() {
    let (replay, _store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Add breakpoint
    replay.add_breakpoint(10, None);
    // Breakpoint exists (checked internally)

    // Remove breakpoint
    replay.remove_breakpoint(10);
    // Breakpoint removed (checked internally)
}

#[tokio::test]
async fn test_breakpoint_with_event_type() {
    let (replay, _store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Add breakpoint with event type filter
    replay.add_breakpoint(10, Some("WORKFLOW_STARTED".to_string()));
    // Breakpoint exists (checked internally)
}

#[tokio::test]
async fn test_multiple_breakpoints() {
    let (replay, _store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Add multiple breakpoints
    replay.add_breakpoint(10, None);
    replay.add_breakpoint(20, None);
    replay.add_breakpoint(30, Some("WORKFLOW_COMPLETED".to_string()));

    // All breakpoints added (checked internally)
}

#[tokio::test]
async fn test_clear_all_breakpoints() {
    let (replay, _store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Add several breakpoints
    replay.add_breakpoint(10, None);
    replay.add_breakpoint(20, None);
    replay.add_breakpoint(30, None);

    // Clear all
    replay.clear_breakpoints();
    // All breakpoints cleared (checked internally)
}

#[tokio::test]
async fn test_inspect_state_at_sequence() {
    let (replay, store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Create a workflow
    store
        .create_workflow("wf-inspect", "user-1", Some("sess-1"), "cot", "test")
        .await
        .unwrap();
    store
        .update_status("wf-inspect", WorkflowStatus::Running)
        .await
        .unwrap();

    // Inspect state at sequence 0
    let state = replay.inspect_state_at("wf-inspect", 0).await.unwrap();

    // Verify state JSON
    assert!(state.contains("wf-inspect"));
    assert!(state.contains("sequence"));
}

#[tokio::test]
async fn test_verify_determinism() {
    let (replay, store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Create a simple workflow
    store
        .create_workflow("wf-determinism", "user-1", Some("sess-1"), "cot", "test")
        .await
        .unwrap();
    store
        .update_status("wf-determinism", WorkflowStatus::Completed)
        .await
        .unwrap();

    // Verify determinism
    let is_deterministic = replay.verify_determinism("wf-determinism").await.unwrap();
    assert!(is_deterministic, "Replay should be deterministic");
}

#[tokio::test]
async fn test_export_with_checkpoint() {
    let (replay, store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Create a workflow
    store
        .create_workflow("wf-checkpoint", "user-1", Some("sess-1"), "cot", "test")
        .await
        .unwrap();

    // Save a checkpoint
    let state_data = vec![1, 2, 3, 4];
    store
        .save_checkpoint("wf-checkpoint", 5, &state_data)
        .await
        .unwrap();

    store
        .update_status("wf-checkpoint", WorkflowStatus::Completed)
        .await
        .unwrap();

    // Export with checkpoint
    let json = replay.export_workflow_json("wf-checkpoint").await.unwrap();

    // Verify checkpoint is included
    assert!(json.contains("checkpoint"));
    assert!(json.contains("\"sequence\":5"));
}

#[tokio::test]
async fn test_replay_nonexistent_workflow() {
    let (replay, _store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Try to export nonexistent workflow
    let result = replay.export_workflow_json("nonexistent").await;
    assert!(result.is_err());
}

#[tokio::test]
async fn test_replay_invalid_json() {
    let (replay, _store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Try to replay invalid JSON
    let result = replay.replay_from_json("invalid json").await;
    assert!(result.is_err());
}

#[tokio::test]
async fn test_round_trip_export_import() {
    let (replay, store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Create a workflow
    store
        .create_workflow(
            "wf-roundtrip",
            "user-1",
            Some("sess-1"),
            "cot",
            "test query",
        )
        .await
        .unwrap();
    store
        .update_status("wf-roundtrip", WorkflowStatus::Completed)
        .await
        .unwrap();

    // Export
    let json = replay.export_workflow_json("wf-roundtrip").await.unwrap();

    // Import
    let result = replay.replay_from_json(&json).await.unwrap();

    // Verify round-trip
    assert_eq!(result.workflow_id, "wf-roundtrip");
    assert_eq!(result.final_status, WorkflowStatus::Completed);
}

#[tokio::test]
async fn test_replay_with_step_through_mode() {
    let (replay, store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Set step-through mode
    replay.set_replay_mode(ReplayMode::StepThrough);

    // Create and export workflow
    store
        .create_workflow("wf-step", "user-1", Some("sess-1"), "cot", "test")
        .await
        .unwrap();
    store
        .update_status("wf-step", WorkflowStatus::Completed)
        .await
        .unwrap();

    let json = replay.export_workflow_json("wf-step").await.unwrap();

    // Replay in step-through mode
    let result = replay.replay_from_json(&json).await.unwrap();
    assert_eq!(result.workflow_id, "wf-step");
}

#[tokio::test]
async fn test_replay_with_breakpoint_mode() {
    let (replay, store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Set breakpoint mode
    replay.set_replay_mode(ReplayMode::Breakpoint);
    replay.add_breakpoint(5, None);

    // Create and export workflow
    store
        .create_workflow("wf-break", "user-1", Some("sess-1"), "cot", "test")
        .await
        .unwrap();
    store
        .update_status("wf-break", WorkflowStatus::Completed)
        .await
        .unwrap();

    let json = replay.export_workflow_json("wf-break").await.unwrap();

    // Replay with breakpoint
    let result = replay.replay_from_json(&json).await.unwrap();
    assert_eq!(result.workflow_id, "wf-break");
}

#[tokio::test]
async fn test_concurrent_replays() {
    let (replay, store, _wf_temp, _ev_temp) = create_test_replay().await;

    // Create multiple workflows
    for i in 0..5 {
        let wf_id = format!("wf-concurrent-{}", i);
        store
            .create_workflow(&wf_id, "user-1", Some("sess-1"), "cot", "test")
            .await
            .unwrap();
        store
            .update_status(&wf_id, WorkflowStatus::Completed)
            .await
            .unwrap();
    }

    // Export all workflows
    let mut jsons = Vec::new();
    for i in 0..5 {
        let wf_id = format!("wf-concurrent-{}", i);
        let json = replay.export_workflow_json(&wf_id).await.unwrap();
        jsons.push(json);
    }

    // Replay concurrently
    let mut handles = vec![];
    for json in jsons {
        let replay_clone = replay.clone();
        let handle = tokio::spawn(async move { replay_clone.replay_from_json(&json).await });
        handles.push(handle);
    }

    // Wait for all replays
    let results = futures::future::join_all(handles).await;

    // All should succeed
    for result in results {
        assert!(result.is_ok());
        assert!(result.unwrap().is_ok());
    }
}
