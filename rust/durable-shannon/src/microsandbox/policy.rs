//! MicroSandbox Capability Policy System
//!
//! The MicroSandbox uses a capability-based security model.
//!
//! WASM modules can only access capabilities explicitly granted
//! by their MicroVmProfile policy. This system controls:
//!   • File system access (none, read-only paths, read-write paths)
//!   • Network access (blocked, allowlist, denylist)
//!   • Environment variable exposure
//!   • Max memory, max CPU time
//!   • Allowed imports (future)
//!
//! This system is unified across macOS, Windows, Linux, and Browser.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

use crate::microsandbox::error::*;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum FileSystemCapability {
    /// Completely disable WASI filesystem access (default)
    None,

    /// Read-only access to specific directories
    ReadOnly(Vec<String>),

    /// Read-write access to specific directories
    ReadWrite(Vec<String>),
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum NetworkCapability {
    /// No outbound network allowed
    BlockAll,

    /// Allow outbound requests to the following hostnames / domains
    AllowList(Vec<String>),

    /// Allow all outbound requests (dangerous)
    AllowAll,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum EnvironmentCapability {
    /// No env vars inside WASM (default)
    None,

    /// Only the following environment variables visible
    AllowList(HashMap<String, String>),

    /// All environment variables visible (unsafe)
    AllowAll,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SandboxCapabilities {
    /// File system access scope
    pub fs: FileSystemCapability,

    /// Network access restrictions
    pub net: NetworkCapability,

    /// Environment visibility
    pub env: EnvironmentCapability,

    /// Maximum memory (MB) allocated to WASM runtime
    pub max_memory_mb: u32,

    /// Maximum execution time (ms)
    pub timeout_ms: u64,

    /// Maximum CPU “fuel” budget (wasmtime)
    pub cpu_fuel: Option<u64>,

    /// Optional metadata
    pub metadata: Option<serde_json::Value>,
}

impl Default for SandboxCapabilities {
    fn default() -> Self {
        Self {
            fs: FileSystemCapability::None,
            net: NetworkCapability::BlockAll,
            env: EnvironmentCapability::None,
            max_memory_mb: 256,
            timeout_ms: 5000,
            cpu_fuel: Some(10_000_000),
            metadata: None,
        }
    }
}

impl SandboxCapabilities {
    /// Validate that config is sane.
    pub fn validate(&self) -> Result<()> {
        if self.max_memory_mb == 0 {
            return Err(MicroVmError::Policy("max_memory_mb must be > 0".into()));
        }
        if self.timeout_ms == 0 {
            return Err(MicroVmError::Policy("timeout_ms must be > 0".into()));
        }
        Ok(())
    }

    /// Check if the profile allows filesystem access to path.
    pub fn check_fs_access(&self, path: &str, write: bool) -> Result<()> {
        match &self.fs {
            FileSystemCapability::None => Err(MicroVmError::Policy(format!(
                "Filesystem access denied: {}",
                path
            ))),

            FileSystemCapability::ReadOnly(dirs) => {
                if write {
                    return Err(MicroVmError::Policy(format!(
                        "Write access denied: {}",
                        path
                    )));
                }
                if dirs.iter().any(|p| path.starts_with(p)) {
                    Ok(())
                } else {
                    Err(MicroVmError::Policy(format!(
                        "Read access denied: {}",
                        path
                    )))
                }
            }

            FileSystemCapability::ReadWrite(dirs) => {
                if dirs.iter().any(|p| path.starts_with(p)) {
                    Ok(())
                } else {
                    Err(MicroVmError::Policy(format!(
                        "FS path not in allowlist: {}",
                        path
                    )))
                }
            }
        }
    }

    /// Check network access permission
    pub fn check_network_access(&self, host: &str) -> Result<()> {
        match &self.net {
            NetworkCapability::BlockAll => {
                Err(MicroVmError::Policy(format!("Network denied to {}", host)))
            }

            NetworkCapability::AllowList(list) => {
                if list.contains(&host.to_string()) {
                    Ok(())
                } else {
                    Err(MicroVmError::Policy(format!(
                        "Host {} not in allowlist",
                        host
                    )))
                }
            }

            NetworkCapability::AllowAll => Ok(()),
        }
    }

    /// Compute the CPU fuel budget for wasmtime.
    pub fn cpu_budget(&self) -> u64 {
        self.cpu_fuel.unwrap_or(10_000_000)
    }
}
