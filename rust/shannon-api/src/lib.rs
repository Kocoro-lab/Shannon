//! Shannon API - Unified Rust Gateway and LLM Service
//!
//! This crate provides a high-performance unified API for the Shannon AI platform,
//! combining both the HTTP Gateway and LLM service functionality into a single
//! Rust-based solution that offers:
//!
//! - **Gateway**: Authentication, rate limiting, session management
//! - **Multi-provider LLM support**: OpenAI, Anthropic, Google, Groq, and more
//! - **Streaming**: First-class SSE and WebSocket streaming
//! - **MCP Integration**: Model Context Protocol for tool discovery and execution
//! - **Tool Loop**: Automatic tool call handling with configurable iteration limits
//! - **Cross-platform**: Can be embedded in Tauri for desktop/mobile deployments
//!
//! # Architecture
//!
//! The service is organized into several key modules:
//!
//! - [`config`]: Configuration management and environment loading
//! - [`gateway`]: Authentication, rate limiting, and session management
//! - [`llm`]: LLM driver abstractions and provider implementations
//! - [`events`]: Normalized streaming event model
//! - [`domain`]: Core domain models (runs, tools, memory)
//! - [`runtime`]: Execution runtime and run lifecycle management
//! - [`tools`]: Built-in tool implementations
//! - [`api`]: HTTP API endpoints
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::{config::AppConfig, server::create_app};
//!
//! #[tokio::main]
//! async fn main() -> anyhow::Result<()> {
//!     let config = AppConfig::load()?;
//!     let app = create_app(config).await?;
//!     
//!     let listener = tokio::net::TcpListener::bind("0.0.0.0:8080").await?;
//!     axum::serve(listener, app).await?;
//!     Ok(())
//! }
//! ```
//!
//! # Embedding in Tauri
//!
//! ```rust,ignore
//! use shannon_api::{config::AppConfig, server::create_app};
//!
//! pub async fn start_embedded_api() -> anyhow::Result<()> {
//!     let config = AppConfig::load()?;
//!     let app = create_app(config).await?;
//!     
//!     // Bind to localhost for embedded use
//!     let listener = tokio::net::TcpListener::bind("127.0.0.1:8765").await?;
//!     axum::serve(listener, app).await?;
//!     Ok(())
//! }
//! ```

#![allow(clippy::module_name_repetitions)]
#![allow(clippy::missing_errors_doc)]

pub mod api;
pub mod config;
pub mod database;
pub mod domain;
pub mod events;
pub mod gateway;
pub mod llm;
pub mod logging;
pub mod runtime;
pub mod server;
pub mod tools;
pub mod workflow;

use std::sync::Arc;

use config::AppConfig;
use llm::orchestrator::Orchestrator;
use runtime::manager::RunManager;
use workflow::WorkflowEngine;

/// Application state shared across all handlers.
#[derive(Clone)]
pub struct AppState {
    /// Application configuration.
    pub config: Arc<AppConfig>,
    /// LLM orchestrator for chat interactions.
    pub orchestrator: Arc<Orchestrator>,
    /// Run manager for task lifecycle.
    pub run_manager: Arc<RunManager>,
    /// Redis connection pool for sessions and rate limiting.
    pub redis: Option<redis::aio::ConnectionManager>,
    /// Workflow engine for task execution.
    ///
    /// This is the primary interface for submitting and managing workflows.
    /// In embedded mode, this uses the local Durable engine.
    /// In cloud mode, this uses Temporal via the Go orchestrator.
    pub workflow_engine: WorkflowEngine,
    /// Embedded SurrealDB connection (only available in embedded mode).
    /// Used for authentication, rate limiting, and direct DB access.
    #[cfg(feature = "embedded")]
    pub surreal: Option<surrealdb::Surreal<surrealdb::engine::local::Db>>,
}

impl std::fmt::Debug for AppState {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("AppState")
            .field("config", &"AppConfig")
            .field("orchestrator", &"Orchestrator")
            .field("run_manager", &"RunManager")
            .field("redis", &self.redis.is_some())
            .field("workflow_engine", &self.workflow_engine)
            .finish()
    }
}
