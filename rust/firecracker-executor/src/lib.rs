//! Firecracker Executor Service
//!
//! Manages a pool of Firecracker microVMs for Python code execution
//! with full data science package support.

pub mod config;
pub mod manager;
pub mod vm;
pub mod api;

pub use config::FirecrackerConfig;
pub use manager::VMPoolManager;
