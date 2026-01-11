use app_lib::database::{hybrid::HybridBackend, StorageBackend};
use std::path::PathBuf;
use tempfile::tempdir;

#[tokio::test]
async fn test_hybrid_init() {
    // Setup temp directory
    let dir = tempdir().expect("failed to create temp dir");
    let path = dir.path().to_path_buf();

    // Create backend
    let backend = HybridBackend::new(path.clone());

    // Test initialization
    let result = backend.init().await;
    assert!(result.is_ok(), "Failed to initialize Hybrid Backend: {:?}", result.err());

    // Test health check
    let health = backend.health_check().await;
    assert!(health.is_ok(), "Health check failed");

    // Verify file creation matches expectation
    let db_path = path.join("shannon.sqlite");
    // let vector_path = path.join("shannon.usearch"); // Index might not create file until save/write
    assert!(db_path.exists(), "SQLite database not created");
}
