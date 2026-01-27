//! VM Pool Manager - manages warm pool of Firecracker VMs

use anyhow::Result;
use tracing::info;

use crate::config::FirecrackerConfig;

pub struct VMPoolManager {
    config: FirecrackerConfig,
}

impl VMPoolManager {
    pub fn new(config: FirecrackerConfig) -> Result<Self> {
        info!("Initializing VM Pool Manager (stub)");
        Ok(Self { config })
    }

    pub async fn execute(&self, session_id: &str, code: &str) -> Result<ExecuteResult> {
        // TODO: Implement VM claim, execute, release
        info!("Execute stub called for session_id={}", session_id);
        let _ = code; // Suppress unused warning
        Ok(ExecuteResult {
            success: false,
            stdout: String::new(),
            stderr: "Firecracker executor not implemented - requires KVM host".to_string(),
            exit_code: 1,
            execution_time_ms: 0,
        })
    }

    pub fn pool_status(&self) -> PoolStatus {
        PoolStatus {
            warm_count: 0,
            busy_count: 0,
            total_count: 0,
            max_count: self.config.pool.max_count,
        }
    }
}

pub struct ExecuteResult {
    pub success: bool,
    pub stdout: String,
    pub stderr: String,
    pub exit_code: i32,
    pub execution_time_ms: u64,
}

pub struct PoolStatus {
    pub warm_count: u32,
    pub busy_count: u32,
    pub total_count: u32,
    pub max_count: u32,
}
