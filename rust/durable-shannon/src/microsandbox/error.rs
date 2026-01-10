//! Errors for the MicroSandbox MicroVM subsystem.

use thiserror::Error;

#[derive(Debug, Error)]
pub enum MicroVmError {
    #[error("MicroVM profile error: {0}")]
    Profile(String),

    #[error("MicroVM launch error: {0}")]
    Launch(String),

    #[error("MicroVM I/O error: {0}")]
    Io(String),

    #[error("MicroVM communication error: {0}")]
    Bridge(String),

    #[error("MicroVM policy violation: {0}")]
    Policy(String),

    #[error("WASM sandbox error: {0}")]
    Wasm(String),

    #[error("Config error: {0}")]
    Config(String),

    #[error("Unexpected MicroVM error: {0}")]
    Unexpected(String),
}

pub type Result<T> = std::result::Result<T, MicroVmError>;
