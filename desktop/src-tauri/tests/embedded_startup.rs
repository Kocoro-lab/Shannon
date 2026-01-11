use shannon_api::config::AppConfig;
use shannon_api::database::{hybrid::HybridBackend, Database};
use shannon_api::server::create_app;
use tokio::net::TcpListener;

#[tokio::test]
async fn embedded_startup_reaches_ready() {
    let temp_dir = tempfile::tempdir().expect("failed to create temp dir");
    let data_dir = temp_dir.path().to_path_buf();
    let sqlite_path = data_dir.join("shannon.sqlite");

    std::env::set_var("SHANNON_MODE", "embedded");
    std::env::set_var("DATABASE_DRIVER", "sqlite");
    std::env::set_var("SQLITE_PATH", sqlite_path.to_string_lossy().to_string());

    let backend = HybridBackend::new(data_dir.clone());
    backend.init().await.expect("hybrid backend init failed");

    let config = AppConfig::load().expect("failed to load config");
    let app = create_app(config, Some(Database::Hybrid(backend)))
        .await
        .expect("failed to create app");

    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("failed to bind");
    let port = listener.local_addr().unwrap().port();

    let server = axum::serve(listener, app);
    let handle = tokio::spawn(async move {
        let _ = server.await;
    });

    tokio::time::sleep(tokio::time::Duration::from_millis(200)).await;

    let client = reqwest::Client::new();
    for endpoint in ["health", "ready", "startup"] {
        let resp = client
            .get(format!("http://127.0.0.1:{}/{}", port, endpoint))
            .send()
            .await
            .expect("failed to request endpoint");
        assert!(resp.status().is_success(), "endpoint {} failed", endpoint);
    }

    handle.abort();
}
