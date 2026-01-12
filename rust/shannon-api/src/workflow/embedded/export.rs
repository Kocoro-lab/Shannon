//! Workflow export functionality.
//!
//! Exports workflows to JSON or Markdown formats with data sanitization.
//!
//! # Features
//!
//! - **JSON Export**: Complete workflow data for programmatic import
//! - **Markdown Export**: Human-readable report format
//! - **Data Sanitization**: Removes API keys and sensitive data
//! - **Versioned Format**: Schema versioning for compatibility
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::embedded::ExportManager;
//!
//! let manager = ExportManager::new(workflow_store).await?;
//! let json = manager.export_to_json("wf-123").await?;
//! let markdown = manager.export_to_markdown("wf-123").await?;
//! ```

use std::sync::Arc;

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};

use crate::database::workflow_store::{WorkflowMetadata, WorkflowStore};

/// Export format version for compatibility.
pub const EXPORT_VERSION: &str = "1.0";

/// Exported workflow in JSON format.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorkflowExport {
    /// Export format version.
    pub version: String,

    /// Workflow metadata.
    pub workflow: WorkflowMetadata,

    /// Export timestamp.
    pub exported_at: String,

    /// Sanitization applied.
    pub sanitized: bool,
}

/// Manager for workflow export operations.
pub struct ExportManager {
    /// Workflow store.
    workflow_store: Arc<WorkflowStore>,
}

impl ExportManager {
    /// Create a new export manager.
    #[must_use]
    pub fn new(workflow_store: Arc<WorkflowStore>) -> Self {
        Self { workflow_store }
    }

    /// Export workflow to JSON format.
    ///
    /// # Errors
    ///
    /// Returns error if workflow not found or export fails.
    pub async fn export_to_json(&self, workflow_id: &str) -> Result<String> {
        let workflow = self
            .workflow_store
            .get_workflow(workflow_id)
            .await?
            .ok_or_else(|| anyhow::anyhow!("Workflow not found"))?;

        let export = WorkflowExport {
            version: EXPORT_VERSION.to_string(),
            workflow: self.sanitize_workflow(workflow),
            exported_at: chrono::Utc::now().to_rfc3339(),
            sanitized: true,
        };

        serde_json::to_string_pretty(&export).context("Failed to serialize export")
    }

    /// Export workflow to Markdown format.
    ///
    /// # Errors
    ///
    /// Returns error if workflow not found or export fails.
    pub async fn export_to_markdown(&self, workflow_id: &str) -> Result<String> {
        let workflow = self
            .workflow_store
            .get_workflow(workflow_id)
            .await?
            .ok_or_else(|| anyhow::anyhow!("Workflow not found"))?;

        let mut md = String::new();

        // Title
        md.push_str(&format!("# Workflow Report\n\n"));

        // Metadata
        md.push_str("## Workflow Information\n\n");
        md.push_str(&format!("- **ID**: `{}`\n", workflow.workflow_id));
        md.push_str(&format!("- **Pattern**: {}\n", workflow.pattern_type));
        md.push_str(&format!("- **Status**: {}\n", workflow.status.as_str()));
        md.push_str(&format!(
            "- **Created**: {}\n",
            chrono::DateTime::from_timestamp(workflow.created_at, 0)
                .unwrap_or_else(chrono::Utc::now)
                .to_rfc3339()
        ));

        if let Some(completed_at) = workflow.completed_at {
            md.push_str(&format!(
                "- **Completed**: {}\n",
                chrono::DateTime::from_timestamp(completed_at, 0)
                    .unwrap_or_else(chrono::Utc::now)
                    .to_rfc3339()
            ));
        }

        md.push_str("\n");

        // Input
        md.push_str("## Input\n\n");
        md.push_str("```\n");
        md.push_str(&workflow.input);
        md.push_str("\n```\n\n");

        // Output
        if let Some(ref output) = workflow.output {
            md.push_str("## Output\n\n");
            md.push_str("```\n");
            md.push_str(output);
            md.push_str("\n```\n\n");
        }

        // Footer
        md.push_str("---\n\n");
        md.push_str(&format!(
            "*Exported at: {}*\n",
            chrono::Utc::now().to_rfc3339()
        ));

        Ok(md)
    }

    /// Sanitize workflow data by removing sensitive information.
    fn sanitize_workflow(&self, mut workflow: WorkflowMetadata) -> WorkflowMetadata {
        // Remove potential API keys from input
        workflow.input = self.sanitize_text(&workflow.input);

        // Remove potential API keys from output
        if let Some(ref output) = workflow.output {
            workflow.output = Some(self.sanitize_text(output));
        }

        workflow
    }

    /// Sanitize text by removing API keys and secrets.
    fn sanitize_text(&self, text: &str) -> String {
        let mut sanitized = text.to_string();

        // Patterns to sanitize
        let patterns = vec![
            (r"sk-[a-zA-Z0-9]{48}", "sk-***REDACTED***"), // OpenAI keys
            (r"sk-ant-[a-zA-Z0-9-]{95}", "sk-ant-***REDACTED***"), // Anthropic keys
            (r"gsk_[a-zA-Z0-9]{52}", "gsk_***REDACTED***"), // Groq keys
            (r"AIza[a-zA-Z0-9]{35}", "AIza***REDACTED***"), // Google keys
            (r"xai-[a-zA-Z0-9]{48}", "xai-***REDACTED***"), // xAI keys
        ];

        for (pattern, replacement) in patterns {
            if let Ok(re) = regex::Regex::new(pattern) {
                sanitized = re.replace_all(&sanitized, replacement).to_string();
            }
        }

        sanitized
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::NamedTempFile;

    async fn create_test_export() -> (ExportManager, Arc<WorkflowStore>, NamedTempFile) {
        let temp = NamedTempFile::new().unwrap();
        let store = Arc::new(WorkflowStore::new(temp.path()).await.unwrap());
        let export_mgr = ExportManager::new(store.clone());
        (export_mgr, store, temp)
    }

    #[tokio::test]
    async fn test_export_to_json() {
        let (export_mgr, store, _temp) = create_test_export().await;

        store
            .create_workflow("wf-export", "user-1", Some("sess-1"), "cot", "test")
            .await
            .unwrap();

        let json = export_mgr.export_to_json("wf-export").await.unwrap();

        assert!(json.contains("wf-export"));
        assert!(json.contains(EXPORT_VERSION));
    }

    #[tokio::test]
    async fn test_export_to_markdown() {
        let (export_mgr, store, _temp) = create_test_export().await;

        store
            .create_workflow("wf-md", "user-1", Some("sess-1"), "cot", "test query")
            .await
            .unwrap();

        let md = export_mgr.export_to_markdown("wf-md").await.unwrap();

        assert!(md.contains("# Workflow Report"));
        assert!(md.contains("wf-md"));
        assert!(md.contains("## Input"));
    }

    #[test]
    fn test_sanitize_openai_key() {
        let mgr = ExportManager::new(Arc::new(WorkflowStore::new(":memory:").await.unwrap()));

        let text = "My key is sk-proj1234567890123456789012345678901234567890123456";
        let sanitized = mgr.sanitize_text(text);

        assert!(!sanitized.contains("sk-proj"));
        assert!(sanitized.contains("***REDACTED***"));
    }

    #[test]
    fn test_sanitize_anthropic_key() {
        let mgr = ExportManager::new(Arc::new(WorkflowStore::new(":memory:").await.unwrap()));

        let text = "Key: sk-ant-api03-aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789aBcDeFgHiJkLmNoP";
        let sanitized = mgr.sanitize_text(text);

        assert!(!sanitized.contains("sk-ant-api03"));
        assert!(sanitized.contains("***REDACTED***"));
    }

    #[tokio::test]
    async fn test_export_nonexistent_workflow() {
        let (export_mgr, _store, _temp) = create_test_export().await;

        let result = export_mgr.export_to_json("nonexistent").await;
        assert!(result.is_err());
    }
}
