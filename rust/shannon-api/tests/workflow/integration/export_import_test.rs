//! Integration tests for workflow export and import.

use std::sync::Arc;
use tempfile::NamedTempFile;

use shannon_api::database::workflow_store::{WorkflowStatus, WorkflowStore};
use shannon_api::workflow::embedded::{ExportManager, ImportManager};

async fn create_test_managers() -> (
    ExportManager,
    ImportManager,
    Arc<WorkflowStore>,
    NamedTempFile,
) {
    let temp = NamedTempFile::new().unwrap();
    let store = Arc::new(WorkflowStore::new(temp.path()).await.unwrap());
    let export_mgr = ExportManager::new(store.clone());
    let import_mgr = ImportManager::new(store.clone());
    (export_mgr, import_mgr, store, temp)
}

#[tokio::test]
async fn test_export_import_roundtrip() {
    let (export_mgr, import_mgr, store, _temp) = create_test_managers().await;

    // Create original workflow
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
    store
        .update_output("wf-roundtrip", "test result")
        .await
        .unwrap();

    // Export
    let json = export_mgr.export_to_json("wf-roundtrip").await.unwrap();

    // Import
    let workflow_id = import_mgr.import_from_json(&json).await.unwrap();
    assert_eq!(workflow_id, "wf-roundtrip");

    // Verify
    let imported = store.get_workflow(&workflow_id).await.unwrap().unwrap();
    assert_eq!(imported.pattern_type, "cot");
    assert_eq!(imported.status, WorkflowStatus::Completed);
}

#[tokio::test]
async fn test_export_sanitizes_api_keys() {
    let (export_mgr, _import_mgr, store, _temp) = create_test_managers().await;

    // Create workflow with API key in input
    let input_with_key = "Use API key: sk-proj1234567890123456789012345678901234567890123456";
    store
        .create_workflow(
            "wf-sanitize",
            "user-1",
            Some("sess-1"),
            "cot",
            input_with_key,
        )
        .await
        .unwrap();

    // Export
    let json = export_mgr.export_to_json("wf-sanitize").await.unwrap();

    // Verify key is redacted
    assert!(!json.contains("sk-proj"));
    assert!(json.contains("REDACTED"));
}

#[tokio::test]
async fn test_export_to_markdown_format() {
    let (export_mgr, _import_mgr, store, _temp) = create_test_managers().await;

    store
        .create_workflow("wf-md", "user-1", Some("sess-1"), "cot", "test")
        .await
        .unwrap();

    let md = export_mgr.export_to_markdown("wf-md").await.unwrap();

    assert!(md.contains("# Workflow Report"));
    assert!(md.contains("## Workflow Information"));
    assert!(md.contains("## Input"));
    assert!(md.contains("wf-md"));
}

#[tokio::test]
async fn test_import_validates_json() {
    let (_export_mgr, import_mgr, _store, _temp) = create_test_managers().await;

    let invalid = "not json";
    let result = import_mgr.validate_json(invalid);
    assert!(result.is_err());

    let valid = r#"{"version":"1.0","workflow":{"workflow_id":"wf-1","user_id":"u1","session_id":null,"pattern_type":"cot","status":"Pending","input":"test","output":null,"created_at":0,"completed_at":null},"exported_at":"2024-01-01T00:00:00Z","sanitized":true}"#;
    let result = import_mgr.validate_json(valid);
    assert!(result.is_ok());
}

#[tokio::test]
async fn test_batch_import() {
    let (export_mgr, import_mgr, store, _temp) = create_test_managers().await;

    // Create multiple workflows
    for i in 1..=3 {
        let wf_id = format!("wf-batch-{}", i);
        store
            .create_workflow(&wf_id, "user-1", Some("sess-1"), "cot", "test")
            .await
            .unwrap();
    }

    // Export all
    let mut exports = Vec::new();
    for i in 1..=3 {
        let wf_id = format!("wf-batch-{}", i);
        let json = export_mgr.export_to_json(&wf_id).await.unwrap();
        exports.push(json);
    }

    // Create batch JSON
    let batch_json = format!("[{}]", exports.join(","));

    // Batch import
    let imported = import_mgr.batch_import(&batch_json).await.unwrap();
    assert_eq!(imported.len(), 3);
}

#[tokio::test]
async fn test_export_import_file_operations() {
    let (export_mgr, import_mgr, store, _temp) = create_test_managers().await;

    store
        .create_workflow("wf-file", "user-1", Some("sess-1"), "cot", "test")
        .await
        .unwrap();

    // Export to file
    let export_file = NamedTempFile::new().unwrap();
    let json = export_mgr.export_to_json("wf-file").await.unwrap();
    std::fs::write(export_file.path(), json).unwrap();

    // Import from file
    let workflow_id = import_mgr
        .import_from_file(export_file.path())
        .await
        .unwrap();
    assert_eq!(workflow_id, "wf-file");
}

#[tokio::test]
async fn test_export_preserves_metadata() {
    let (export_mgr, _import_mgr, store, _temp) = create_test_managers().await;

    store
        .create_workflow("wf-meta", "user-1", Some("sess-1"), "cot", "test")
        .await
        .unwrap();
    store
        .update_status("wf-meta", WorkflowStatus::Completed)
        .await
        .unwrap();

    let json = export_mgr.export_to_json("wf-meta").await.unwrap();
    let parsed: serde_json::Value = serde_json::from_str(&json).unwrap();

    assert_eq!(parsed["workflow"]["workflow_id"], "wf-meta");
    assert_eq!(parsed["workflow"]["pattern_type"], "cot");
    assert_eq!(parsed["version"], "1.0");
}
