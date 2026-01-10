use serial_test::serial;
use shannon_api::config::AppConfig;
use shannon_api::server::create_app;
use tokio::net::TcpListener;

#[tokio::test]
#[serial]
async fn test_embedded_server_startup() {
    // Setup environment for embedded mode
    let temp_dir = tempfile::tempdir().unwrap();
    let db_path = temp_dir.path().join("shannon.db");
    std::env::set_var("SHANNON_MODE", "embedded");
    std::env::set_var("WORKFLOW_ENGINE", "durable");
    std::env::set_var("SURREALDB_PATH", db_path.to_str().unwrap());

    // Load config
    let config = AppConfig::load().expect("Failed to load config");
    assert!(config.deployment.is_embedded());

    // Create app
    let app = create_app(
        config,
        #[cfg(feature = "embedded")]
        None, // No existing database connection in tests
    )
    .await
    .expect("Failed to create app");

    // Bind to port 0
    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("Failed to bind");
    let port = listener.local_addr().unwrap().port();

    // Run in background
    let server = axum::serve(listener, app);
    let handle = tokio::spawn(async move {
        server.await.expect("Server error");
    });

    // Wait a bit
    tokio::time::sleep(tokio::time::Duration::from_millis(200)).await;

    // Test Health
    let client = reqwest::Client::new();
    let resp = client
        .get(format!("http://127.0.0.1:{}/health", port))
        .send()
        .await
        .expect("Failed to request health");

    assert!(resp.status().is_success());
    let body: serde_json::Value = resp.json().await.expect("Failed to parse JSON");
    assert_eq!(body["status"], "ok");

    // Cleanup
    handle.abort();
}

#[tokio::test]
#[serial]
async fn test_auth_rejection() {
    // Setup environment for embedded mode
    let temp_dir = tempfile::tempdir().unwrap();
    let db_path = temp_dir.path().join("shannon.db");
    std::env::set_var("SHANNON_MODE", "embedded");
    std::env::set_var("SURREALDB_PATH", db_path.to_str().unwrap());

    let config = AppConfig::load().expect("Failed to load config");
    let app = create_app(
        config,
        #[cfg(feature = "embedded")]
        None,
    )
    .await
    .expect("Failed to create app");
    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("Failed to bind");
    let port = listener.local_addr().unwrap().port();

    let server = axum::serve(listener, app);
    let handle = tokio::spawn(async move {
        server.await.expect("Server error");
    });

    tokio::time::sleep(tokio::time::Duration::from_millis(200)).await;

    // Test Protected Endpoint without Auth
    let client = reqwest::Client::new();
    let resp = client
        .get(format!("http://127.0.0.1:{}/api/v1/sessions", port))
        .send()
        .await
        .expect("Failed to request sessions");

    // Should be unauthorized
    assert_eq!(resp.status(), reqwest::StatusCode::UNAUTHORIZED);

    handle.abort();
}

#[tokio::test]
#[serial]
async fn test_task_submission_embedded() {
    // Setup environment
    let temp_dir = tempfile::tempdir().unwrap();
    let db_path = temp_dir.path().join("shannon.db");
    std::env::set_var("SHANNON_MODE", "embedded");
    std::env::set_var("SURREALDB_PATH", db_path.to_str().unwrap());

    // Mock API key in env if necessary, or inject into DB
    // Testing full flow might fail if we don't insert a user into DB first.
    // For now, let's use the hardcoded "sk-" fallback which works in debug builds

    let config = AppConfig::load().expect("Failed to load config");
    let app = create_app(
        config,
        #[cfg(feature = "embedded")]
        None,
    )
    .await
    .expect("Failed to create app");
    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("Failed to bind");
    let port = listener.local_addr().unwrap().port();

    let server = axum::serve(listener, app);
    let handle = tokio::spawn(async move {
        server.await.expect("Server error");
    });

    tokio::time::sleep(tokio::time::Duration::from_millis(200)).await;

    // Submit Validation Task
    let client = reqwest::Client::new();
    let resp = client
        .post(format!("http://127.0.0.1:{}/api/v1/tasks", port))
        .header("Authorization", "Bearer sk-test-key")
        .json(&serde_json::json!({
            "prompt": "Test task",
            "strategy": "simple"
        }))
        .send()
        .await
        .expect("Failed to submit task");

    if !resp.status().is_success() {
        let err_text = resp.text().await.unwrap();
        println!("Submit error: {}", err_text);
        panic!("Failed to submit task");
    }

    assert!(resp.status().is_success());
    let body: serde_json::Value = resp.json().await.unwrap();
    let task_id = body["task_id"].as_str().unwrap();
    println!("Submitted task: {}", task_id);

    // Poll for status
    let status_resp = client
        .get(format!(
            "http://127.0.0.1:{}/api/v1/tasks/{}",
            port, task_id
        ))
        .header("Authorization", "Bearer sk-test-key")
        .send()
        .await
        .unwrap();

    assert!(status_resp.status().is_success());
    let status_body: serde_json::Value = status_resp.json().await.unwrap();
    let state = status_body["status"].as_str().unwrap();
    println!("Task state: {}", state);

    handle.abort();
}
