//! MicroSandbox: Secure MicroVM Runtime for WASM
//!
//! Provides a sandboxed environment for executing untrusted WASM modules
//! with strict resource and capability limits.

pub mod error;
pub mod policy;
pub mod wasm_sandbox;

pub use error::{MicroVmError, Result};
pub use policy::{
    EnvironmentCapability, FileSystemCapability, NetworkCapability, SandboxCapabilities,
};
pub use wasm_sandbox::{WasmProcess, WasmSandbox};
