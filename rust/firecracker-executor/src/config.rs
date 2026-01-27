//! Configuration for Firecracker executor

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FirecrackerConfig {
    pub binary_path: String,
    pub jailer_path: String,
    pub kernel_path: String,
    pub rootfs_path: String,
    pub pool: PoolConfig,
    pub defaults: VMDefaults,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PoolConfig {
    pub warm_count: u32,
    pub max_count: u32,
    pub idle_timeout_secs: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct VMDefaults {
    pub memory_mb: u32,
    pub vcpu_count: u32,
    pub timeout_seconds: u32,
}

impl Default for FirecrackerConfig {
    fn default() -> Self {
        Self {
            binary_path: "/usr/local/bin/firecracker".to_string(),
            jailer_path: "/usr/local/bin/jailer".to_string(),
            kernel_path: "/var/lib/firecracker/kernel/vmlinux".to_string(),
            rootfs_path: "/var/lib/firecracker/images/python-datascience.ext4".to_string(),
            pool: PoolConfig {
                warm_count: 3,
                max_count: 20,
                idle_timeout_secs: 300,
            },
            defaults: VMDefaults {
                memory_mb: 1024,
                vcpu_count: 2,
                timeout_seconds: 300,
            },
        }
    }
}
