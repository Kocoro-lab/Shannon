use crate::database::{lancedb::LanceDbBackend, StorageBackend};
use std::path::PathBuf;
use tempfile::tempdir;

#[tokio::test]
async fn test_lancedb_init() {
    // Setup temp directory
    let dir = tempdir().expect("failed to create temp dir");
    let path = dir.path().to_path_buf();

    // Create backend
    let backend = LanceDbBackend::new(path.clone());

    // Test initialization
    let result = backend.init().await;
    assert!(result.is_ok(), "Failed to initialize LanceDB: {:?}", result.err());

    // Test health check
    let health = backend.health_check().await;
    assert!(health.is_ok(), "Health check failed");

    // Verify file creation matches expectation
    let db_path = path.join("lancedb");
    assert!(db_path.exists(), "LanceDB directory not created");
}
