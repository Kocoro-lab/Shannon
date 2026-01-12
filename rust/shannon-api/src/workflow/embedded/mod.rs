//! Embedded workflow engine for Tauri desktop application.
//!
//! Provides workflow orchestration without external dependencies:
//! - No Temporal (uses Durable + SQLite)
//! - No orchestrator service (in-process execution)
//! - No PostgreSQL (uses SQLite for state)
//!
//! # Architecture
//!
//! ```text
//! User Request → Gateway → EmbeddedWorkflowEngine
//!                             ├─> Event Log (SQLite)
//!                             ├─> Workflow Store (SQLite)
//!                             ├─> Event Bus (tokio channels)
//!                             └─> Pattern Execution (CoT, ToT, etc.)
//! ```
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::embedded::EmbeddedWorkflowEngine;
//!
//! let engine = EmbeddedWorkflowEngine::new("./shannon.db").await?;
//!
//! // Submit workflow
//! let workflow_id = engine.submit_task("chain_of_thought", input).await?;
//!
//! // Stream events
//! let mut events = engine.stream_events(&workflow_id).await?;
//! while let Some(event) = events.next().await {
//!     println!("Event: {:?}", event);
//! }
//! ```

pub mod circuit_breaker;
pub mod engine;
pub mod event_bus;
pub mod export;
pub mod import;
pub mod optimizations;
pub mod recovery;
pub mod replay;
pub mod router;
pub mod session;

pub use circuit_breaker::{CircuitBreaker, CircuitBreakerState};
pub use engine::{EmbeddedWorkflowEngine, EngineHealth};
pub use event_bus::{EventBus, WorkflowEvent};
pub use export::{ExportManager, WorkflowExport};
pub use import::ImportManager;
pub use optimizations::{BufferPool, EventBatcher, ParallelExecutor, PoolStats};
pub use recovery::{RecoveredWorkflow, RecoveryManager};
pub use replay::{ReplayManager, ReplayMode, ReplayResult, WorkflowHistory};
pub use router::{ComplexityScore, WorkflowRouter};
pub use session::{Session, SessionManager};
