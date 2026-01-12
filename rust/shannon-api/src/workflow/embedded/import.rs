//! Workflow import functionality.
//!
//! Imports workflows from JSON format and restores state.
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::embedded::ImportManager;
//!
//! let manager = ImportManager::new(workflow_store).await?;
//! let workflow_id = manager.import_from_json(&json_data).await?;
//! ```

use std::sync::Arc;

use anyhow::{Context, Result};
use serde_json::Value;

use crate::database::workflow_store::{WorkflowStatus, WorkflowStore};

use super::export::WorkflowExport;

/// Manager for workflow import operations.
pub struct ImportManager {
    /// Workflow store.
    workflow_store: Arc<WorkflowStore>,
}

impl ImportManager {
    /// Create a new import manager.
    #[must_use]
    pub fn new(workflow_store: Arc<WorkflowStore>) -> Self {
        Self { workflow_store }
    }

    /// Import workflow from JSON.
    ///
    /// # Errors
    ///
    /// Returns error if JSON is invalid or import fails.
    pub async fn import_from_json(&self, json: &str) -> Result<String> {
        let export: WorkflowExport =
            serde_json::from_str(json).context("Failed to parse workflow export")?;

        // Validate version compatibility
        if export.version != super::export::EXPORT_VERSION {
            tracing::warn!(
                export_version = export.version,
                current_version = super::export::EXPORT_VERSION,
                "Import version mismatch, proceeding anyway"
            );
        }

        let workflow = &export.workflow;

        // Create workflow in database
        self.workflow_store
            .create_workflow(
                &workflow.workflow_id,
                &workflow.user_id,
                workflow.session_id.as_deref(),
                &workflow.pattern_type,
                &workflow.input,
            )
            .await?;

        // Update status if not pending
        if workflow.status != WorkflowStatus::Pending {
            self.workflow_store
                .update_status(&workflow.workflow_id, workflow.status.clone())
                .await?;
        }

        // Update output if present
        if let Some(ref output) = workflow.output {
            self.workflow_store
                .update_output(&workflow.workflow_id, output)
                .await?;
        }

        tracing::info!(
            workflow_id = %workflow.workflow_id,
            "Imported workflow successfully"
        );

        Ok(workflow.workflow_id.clone())
    }

    /// Import workflow from JSON file.
    ///
    /// # Errors
    ///
    /// Returns error if file read fails or import fails.
    pub async fn import_from_file(&self, path: impl AsRef<std::path::Path>) -> Result<String> {
        let json = std::fs::read_to_string(path.as_ref()).context("Failed to read import file")?;
        self.import_from_json(&json).await
    }

    /// Validate JSON export format without importing.
    ///
    /// # Errors
    ///
    /// Returns error if JSON is invalid.
    pub fn validate_json(&self, json: &str) -> Result<()> {
        let _export: WorkflowExport =
            serde_json::from_str(json).context("Invalid workflow export format")?;
        Ok(())
    }

    /// Batch import multiple workflows from JSON array.
    ///
    /// # Errors
    ///
    /// Returns error if any import fails.
    pub async fn batch_import(&self, json: &str) -> Result<Vec<String>> {
        let exports: Vec<WorkflowExport> =
            serde_json::from_str(json).context("Failed to parse batch export")?;

        let mut imported = Vec::new();

        for export in exports {
            let json = serde_json::to_string(&export)?;
            let workflow_id = self.import_from_json(&json).await?;
            imported.push(workflow_id);
        }

        tracing::info!(count = imported.len(), "Batch import completed");

        Ok(imported)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::NamedTempFile;

    async fn create_test_import() -> (ImportManager, Arc<WorkflowStore>, NamedTempFile) {
        let temp = NamedTempFile::new().unwrap();
        let store = Arc::new(WorkflowStore::new(temp.path()).await.unwrap());
        let import_mgr = ImportManager::new(store.clone());
        (import_mgr, store, temp)
    }

    #[tokio::test]
    async fn test_import_from_json() {
        let (import_mgr, store, _temp) = create_test_import().await;

        // Create export
        store
            .create_workflow("wf-orig", "user-1", Some("sess-1"), "cot", "test")
            .await
            .unwrap();

        let export_mgr = super::super::export::ExportManager::new(store.clone());
        let json = export_mgr.export_to_json("wf-orig").await.unwrap();

        // Import
        let workflow_id = import_mgr.import_from_json(&json).await.unwrap();
        assert_eq!(workflow_id, "wf-orig");

        // Verify imported
        let imported = store.get_workflow(&workflow_id).await.unwrap();
        assert!(imported.is_some());
    }

    #[tokio::test]
    async fn test_validate_json() {
        let (import_mgr, _store, _temp) = create_test_import().await;

        let valid_json = r#"{
            "version": "1.0",
            "workflow": {
                "workflow_id": "wf-123",
                "user_id": "user-1",
                "session_id": null,
                "pattern_type": "cot",
                "status": "Pending",
                "input": "test",
                "output": null,
                "created_at": 0,
                "completed_at": null
            },
            "exported_at": "2024-01-01T00:00:00Z",
            "sanitized": true
        }"#;

        let result = import_mgr.validate_json(valid_json);
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_validate_invalid_json() {
        let (import_mgr, _store, _temp) = create_test_import().await;

        let invalid_json = "not json";
        let result = import_mgr.validate_json(invalid_json);
        assert!(result.is_err());
    }
}
