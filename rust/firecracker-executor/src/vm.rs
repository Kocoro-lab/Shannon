//! Single VM lifecycle management

use anyhow::Result;

/// Represents a single Firecracker microVM
pub struct VM {
    pub id: String,
    pub state: VMState,
}

#[derive(Debug, Clone, PartialEq)]
pub enum VMState {
    Creating,
    Warm,
    Busy,
    Tainted,
    Destroyed,
}

impl VM {
    pub fn new(id: String) -> Self {
        Self {
            id,
            state: VMState::Creating,
        }
    }

    pub async fn start(&mut self) -> Result<()> {
        // TODO: Start Firecracker process
        self.state = VMState::Warm;
        Ok(())
    }

    pub async fn execute(&mut self, _code: &str) -> Result<String> {
        // TODO: Execute code in VM
        self.state = VMState::Busy;
        Ok(String::new())
    }

    pub async fn destroy(&mut self) -> Result<()> {
        // TODO: Kill Firecracker process
        self.state = VMState::Destroyed;
        Ok(())
    }
}
