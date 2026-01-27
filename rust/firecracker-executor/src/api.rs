//! Firecracker REST API client

use anyhow::Result;
use reqwest::Client;
use serde::Serialize;

/// Client for Firecracker's REST API
pub struct FirecrackerApiClient {
    client: Client,
    socket_path: String,
}

#[derive(Debug, Serialize)]
pub struct MachineConfig {
    pub vcpu_count: u32,
    pub mem_size_mib: u32,
}

#[derive(Debug, Serialize)]
pub struct BootSource {
    pub kernel_image_path: String,
    pub boot_args: String,
}

#[derive(Debug, Serialize)]
pub struct Drive {
    pub drive_id: String,
    pub path_on_host: String,
    pub is_root_device: bool,
    pub is_read_only: bool,
}

impl FirecrackerApiClient {
    pub fn new(socket_path: String) -> Self {
        Self {
            client: Client::new(),
            socket_path,
        }
    }

    pub async fn set_machine_config(&self, _config: MachineConfig) -> Result<()> {
        // TODO: PUT /machine-config
        let _ = &self.client; // Suppress unused warning
        let _ = &self.socket_path; // Suppress unused warning
        Ok(())
    }

    pub async fn set_boot_source(&self, _boot: BootSource) -> Result<()> {
        // TODO: PUT /boot-source
        Ok(())
    }

    pub async fn add_drive(&self, _drive: Drive) -> Result<()> {
        // TODO: PUT /drives/{drive_id}
        Ok(())
    }

    pub async fn start(&self) -> Result<()> {
        // TODO: PUT /actions {"action_type": "InstanceStart"}
        Ok(())
    }
}
