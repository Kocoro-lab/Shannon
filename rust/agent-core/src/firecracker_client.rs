use anyhow::{anyhow, Result};
use once_cell::sync::Lazy;
use serde::{Deserialize, Serialize};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

/// Health check cache duration in seconds
const HEALTH_CHECK_CACHE_SECS: u64 = 30;

/// Default timeout for /execute calls (matches executor default)
const EXECUTE_TIMEOUT_SECS: u64 = 300;

/// Shared state for health check caching across clones
struct HealthCheckState {
    is_healthy: AtomicBool,
    last_check_epoch_secs: AtomicU64,
}

/// Global singleton client to preserve health cache across calls (P1 fix)
static GLOBAL_CLIENT: Lazy<FirecrackerExecutorClient> =
    Lazy::new(|| FirecrackerExecutorClient::new_internal());

#[derive(Debug, Serialize)]
pub struct FirecrackerExecuteRequest {
    pub code: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub session_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub timeout_seconds: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub workspace_path: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub stdin: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct FirecrackerExecuteResponse {
    pub success: bool,
    pub stdout: String,
    pub stderr: String,
    pub exit_code: i32,
    pub error: Option<String>,
    pub duration_ms: Option<u64>,
}

#[derive(Clone)]
pub struct FirecrackerExecutorClient {
    base_url: String,
    http: reqwest::Client,
    health_state: Arc<HealthCheckState>,
    execute_timeout_secs: u64,
}

impl FirecrackerExecutorClient {
    /// Get the global singleton client (preserves health cache)
    pub fn from_env() -> &'static Self {
        &GLOBAL_CLIENT
    }

    fn new_internal() -> Self {
        let base_url = std::env::var("FIRECRACKER_EXECUTOR_URL")
            .unwrap_or_else(|_| "http://firecracker-executor:9001".to_string());
        let execute_timeout_secs = std::env::var("FIRECRACKER_EXECUTE_TIMEOUT_SECS")
            .ok()
            .and_then(|v| v.parse().ok())
            .unwrap_or(EXECUTE_TIMEOUT_SECS);
        Self {
            base_url,
            http: reqwest::Client::new(),
            health_state: Arc::new(HealthCheckState {
                is_healthy: AtomicBool::new(false),
                last_check_epoch_secs: AtomicU64::new(0),
            }),
            execute_timeout_secs,
        }
    }

    /// Perform a health check against the Firecracker executor service.
    /// Returns true if the service is healthy, false otherwise.
    pub async fn health_check(&self) -> Result<bool> {
        let url = format!("{}/health", self.base_url.trim_end_matches('/'));
        match self
            .http
            .get(&url)
            .timeout(Duration::from_secs(5))
            .send()
            .await
        {
            Ok(resp) if resp.status().is_success() => Ok(true),
            Ok(resp) => {
                tracing::debug!(
                    "Firecracker health check failed with status: {}",
                    resp.status()
                );
                Ok(false)
            }
            Err(e) => {
                tracing::debug!("Firecracker health check error: {}", e);
                Ok(false)
            }
        }
    }

    /// Get the executor URL for logging
    pub fn base_url(&self) -> &str {
        &self.base_url
    }

    /// Check if Firecracker executor is available, using cached result.
    /// The health check is performed at most once every HEALTH_CHECK_CACHE_SECS seconds.
    pub async fn is_available(&self) -> bool {
        let now_secs = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();

        let last_check = self
            .health_state
            .last_check_epoch_secs
            .load(Ordering::Relaxed);

        // If cache is still valid, return cached result
        if now_secs.saturating_sub(last_check) < HEALTH_CHECK_CACHE_SECS {
            return self.health_state.is_healthy.load(Ordering::Relaxed);
        }

        // Perform fresh health check
        let healthy = self.health_check().await.unwrap_or(false);

        // Update cache
        self.health_state
            .is_healthy
            .store(healthy, Ordering::Relaxed);
        self.health_state
            .last_check_epoch_secs
            .store(now_secs, Ordering::Relaxed);

        healthy
    }

    pub async fn execute(
        &self,
        req: FirecrackerExecuteRequest,
    ) -> Result<FirecrackerExecuteResponse> {
        let url = format!("{}/execute", self.base_url.trim_end_matches('/'));

        // P1 fix: Add timeout to /execute call
        // Use request-specified timeout or default, plus buffer for network
        let timeout_secs = req
            .timeout_seconds
            .map(|t| t as u64)
            .unwrap_or(self.execute_timeout_secs);
        let total_timeout = Duration::from_secs(timeout_secs + 30); // 30s buffer

        let resp = self
            .http
            .post(url)
            .json(&req)
            .timeout(total_timeout)
            .send()
            .await?;

        // P1 fix: Propagate 503 status as error to trigger WASI fallback
        if resp.status() == reqwest::StatusCode::SERVICE_UNAVAILABLE {
            let body = resp.text().await.unwrap_or_default();
            return Err(anyhow!("firecracker executor unavailable (503): {}", body));
        }

        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return Err(anyhow!(
                "firecracker executor error (status={}): {}",
                status,
                body
            ));
        }
        let parsed = resp.json::<FirecrackerExecuteResponse>().await?;
        Ok(parsed)
    }

    /// Download a file from session workspace
    pub async fn download_file(
        &self,
        session_id: &str,
        file_path: &str,
        workspace_path: &str,
    ) -> Result<FirecrackerDownloadResponse> {
        let url = format!("{}/workspace/download", self.base_url.trim_end_matches('/'));

        let req = FirecrackerDownloadRequest {
            session_id: session_id.to_string(),
            file_path: file_path.to_string(),
            workspace_path: workspace_path.to_string(),
        };

        let resp = self
            .http
            .post(url)
            .json(&req)
            .timeout(Duration::from_secs(60))
            .send()
            .await?;

        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return Err(anyhow!(
                "firecracker download error (status={}): {}",
                status,
                body
            ));
        }

        let parsed = resp.json::<FirecrackerDownloadResponse>().await?;
        Ok(parsed)
    }

    /// List files in session workspace
    pub async fn list_files(
        &self,
        session_id: &str,
        workspace_path: &str,
        path: Option<&str>,
    ) -> Result<FirecrackerListFilesResponse> {
        let url = format!("{}/workspace/list", self.base_url.trim_end_matches('/'));

        let req = FirecrackerListFilesRequest {
            session_id: session_id.to_string(),
            workspace_path: workspace_path.to_string(),
            path: path.map(|s| s.to_string()),
        };

        let resp = self
            .http
            .post(url)
            .json(&req)
            .timeout(Duration::from_secs(30))
            .send()
            .await?;

        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return Err(anyhow!(
                "firecracker list files error (status={}): {}",
                status,
                body
            ));
        }

        let parsed = resp.json::<FirecrackerListFilesResponse>().await?;
        Ok(parsed)
    }
}

#[derive(Debug, Serialize)]
pub struct FirecrackerDownloadRequest {
    pub session_id: String,
    pub file_path: String,
    pub workspace_path: String,
}

#[derive(Debug, Deserialize)]
pub struct FirecrackerDownloadResponse {
    pub success: bool,
    pub content: Option<String>,
    pub content_type: Option<String>,
    pub size_bytes: Option<u64>,
    pub error: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct FirecrackerListFilesRequest {
    pub session_id: String,
    pub workspace_path: String,
    pub path: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct FirecrackerListFilesResponse {
    pub success: bool,
    pub files: Vec<FirecrackerFileInfo>,
    pub error: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct FirecrackerFileInfo {
    pub name: String,
    pub path: String,
    pub is_dir: bool,
    pub size_bytes: u64,
}
