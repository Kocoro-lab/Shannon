//! Workflow engine implementations.
//!
//! Provides the `WorkflowEngine` enum that abstracts over different
//! workflow backend implementations (Durable for embedded, Temporal for cloud).
//!
//! # Embedded Mode (Durable)
//!
//! In embedded mode, workflows run entirely in-process using the `durable-shannon`
//! crate. No external orchestrator or network connections are required.
//!
//! # Cloud Mode (Temporal)
//!
//! In cloud mode, workflows are coordinated by Temporal via the Go orchestrator
//! service. This requires gRPC connectivity to the orchestrator.

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::{broadcast, RwLock};

use super::task::{Task, TaskHandle, TaskResult, TaskState};
use crate::config::deployment::WorkflowConfig;

#[cfg(feature = "grpc")]
use crate::gateway::grpc_client::{OrchestratorClient, OrchestratorClientConfig};

/// Workflow engine type.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum WorkflowEngineType {
    /// Durable WASM-based engine for embedded mode.
    Durable,
    /// Temporal engine for cloud mode.
    Temporal,
}

impl std::fmt::Display for WorkflowEngineType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Durable => write!(f, "durable"),
            Self::Temporal => write!(f, "temporal"),
        }
    }
}

/// Trait for workflow engine implementations.
#[async_trait]
pub trait WorkflowEngineImpl: Send + Sync {
    /// Get the engine type.
    fn engine_type(&self) -> WorkflowEngineType;

    /// Submit a task for execution.
    async fn submit(&self, task: Task) -> anyhow::Result<TaskHandle>;

    /// Get the current status of a task.
    async fn status(&self, task_id: &str) -> anyhow::Result<TaskHandle>;

    /// Get the result of a completed task.
    async fn result(&self, task_id: &str) -> anyhow::Result<TaskResult>;

    /// Cancel a running task.
    async fn cancel(&self, task_id: &str, reason: Option<&str>) -> anyhow::Result<bool>;

    /// Pause a running task.
    async fn pause(&self, task_id: &str, reason: Option<&str>) -> anyhow::Result<bool>;

    /// Resume a paused task.
    async fn resume(&self, task_id: &str, reason: Option<&str>) -> anyhow::Result<bool>;

    /// Subscribe to task events.
    async fn subscribe(&self, task_id: &str) -> anyhow::Result<broadcast::Receiver<TaskEvent>>;

    /// Health check.
    async fn health(&self) -> anyhow::Result<bool>;
}

/// Task event for streaming updates.
#[derive(Debug, Clone)]
pub enum TaskEvent {
    /// Task state changed.
    StateChanged {
        task_id: String,
        old_state: TaskState,
        new_state: TaskState,
    },
    /// Progress updated.
    Progress {
        task_id: String,
        percent: u8,
        message: Option<String>,
    },
    /// Partial result available.
    PartialResult { task_id: String, content: String },
    /// Task completed.
    Completed { task_id: String, result: TaskResult },
    /// Task failed.
    Failed { task_id: String, error: String },
}

/// Workflow engine that abstracts over Durable and Temporal.
#[derive(Clone)]
pub enum WorkflowEngine {
    /// Durable engine for embedded mode (placeholder - requires durable-shannon crate).
    Durable(Arc<DurableEngine>),
    /// Temporal engine for cloud mode (via gRPC to Go orchestrator).
    Temporal(Arc<TemporalEngine>),
}

impl std::fmt::Debug for WorkflowEngine {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Durable(engine) => f.debug_tuple("Durable").field(&engine.engine_type()).finish(),
            Self::Temporal(engine) => {
                f.debug_tuple("Temporal").field(&engine.engine_type()).finish()
            }
        }
    }
}

impl WorkflowEngine {
    /// Create a workflow engine from configuration.
    pub async fn from_config(config: &WorkflowConfig) -> anyhow::Result<Self> {
        match config {
            WorkflowConfig::Durable {
                wasm_dir,
                max_concurrent,
                ..
            } => {
                let engine = DurableEngine::new(wasm_dir.clone(), *max_concurrent).await?;
                Ok(Self::Durable(Arc::new(engine)))
            }
            WorkflowConfig::Temporal {
                endpoint,
                namespace,
                task_queue,
            } => {
                let engine =
                    TemporalEngine::new(endpoint.clone(), namespace.clone(), task_queue.clone())
                        .await?;
                Ok(Self::Temporal(Arc::new(engine)))
            }
        }
    }

    /// Get the engine type.
    #[must_use]
    pub fn engine_type(&self) -> WorkflowEngineType {
        match self {
            Self::Durable(_) => WorkflowEngineType::Durable,
            Self::Temporal(_) => WorkflowEngineType::Temporal,
        }
    }

    /// Submit a task for execution.
    pub async fn submit(&self, task: Task) -> anyhow::Result<TaskHandle> {
        match self {
            Self::Durable(engine) => engine.submit(task).await,
            Self::Temporal(engine) => engine.submit(task).await,
        }
    }

    /// Get the current status of a task.
    pub async fn status(&self, task_id: &str) -> anyhow::Result<TaskHandle> {
        match self {
            Self::Durable(engine) => engine.status(task_id).await,
            Self::Temporal(engine) => engine.status(task_id).await,
        }
    }

    /// Get the result of a completed task.
    pub async fn result(&self, task_id: &str) -> anyhow::Result<TaskResult> {
        match self {
            Self::Durable(engine) => engine.result(task_id).await,
            Self::Temporal(engine) => engine.result(task_id).await,
        }
    }

    /// Cancel a running task.
    pub async fn cancel(&self, task_id: &str, reason: Option<&str>) -> anyhow::Result<bool> {
        match self {
            Self::Durable(engine) => engine.cancel(task_id, reason).await,
            Self::Temporal(engine) => engine.cancel(task_id, reason).await,
        }
    }

    /// Health check.
    pub async fn health(&self) -> anyhow::Result<bool> {
        match self {
            Self::Durable(engine) => engine.health().await,
            Self::Temporal(engine) => engine.health().await,
        }
    }
}

/// Durable engine implementation for embedded mode.
///
/// This engine runs workflows entirely in-process without any network calls.
/// It maintains its own task registry and executes workflows using local resources.
///
/// When the `embedded` feature is enabled, this integrates with `durable-shannon`
/// for WASM-based workflow execution with durable state.
pub struct DurableEngine {
    /// Directory containing WASM workflow patterns.
    wasm_dir: PathBuf,
    /// Maximum concurrent workflows.
    max_concurrent: usize,
    /// Active tasks registry (wrapped in Arc for spawned tasks).
    tasks: Arc<RwLock<HashMap<String, DurableTask>>>,
    /// Event channels for task updates (wrapped in Arc for spawned tasks).
    channels: Arc<RwLock<HashMap<String, broadcast::Sender<TaskEvent>>>>,
}

/// Internal task representation for the Durable engine.
struct DurableTask {
    /// Task ID.
    id: String,
    /// Workflow ID.
    workflow_id: String,
    /// Current state.
    state: TaskState,
    /// Progress (0-100).
    progress: u8,
    /// Status message.
    message: Option<String>,
    /// Result content (when completed).
    result: Option<String>,
    /// Error message (when failed).
    error: Option<String>,
    /// Original task data.
    task: Task,
    /// Start time.
    started_at: chrono::DateTime<chrono::Utc>,
}

impl DurableEngine {
    /// Create a new Durable engine for embedded mode.
    ///
    /// This initializes the local workflow execution environment.
    /// No network connections are established.
    pub async fn new(
        wasm_dir: PathBuf,
        max_concurrent: usize,
    ) -> anyhow::Result<Self> {
        tracing::info!(
            "Initializing Durable engine (embedded mode) with WASM dir: {:?}, max_concurrent: {}",
            wasm_dir,
            max_concurrent
        );

        // Ensure WASM directory exists
        if !wasm_dir.exists() {
            std::fs::create_dir_all(&wasm_dir)?;
            tracing::info!("Created WASM directory: {:?}", wasm_dir);
        }

        Ok(Self {
            wasm_dir,
            max_concurrent,
            tasks: Arc::new(RwLock::new(HashMap::new())),
            channels: Arc::new(RwLock::new(HashMap::new())),
        })
    }

    /// Execute a task in the background.
    ///
    /// This spawns a tokio task that executes the workflow locally.
    async fn execute_task(&self, task: Task, tx: broadcast::Sender<TaskEvent>) {
        let task_id = task.id.clone();
        let workflow_id = format!("durable-{}", task.id);

        // Update task state to running
        {
            let mut tasks = self.tasks.write().await;
            if let Some(t) = tasks.get_mut(&task_id) {
                t.state = TaskState::Running;
            }
        }

        // Notify state change
        let _ = tx.send(TaskEvent::StateChanged {
            task_id: task_id.clone(),
            old_state: TaskState::Pending,
            new_state: TaskState::Running,
        });

        // Execute the workflow locally
        // For now, this simulates workflow execution
        // TODO: Integrate with durable-shannon::EmbeddedWorker for WASM execution
        let result = self.run_local_workflow(&task, &tx).await;

        // Update final state
        {
            let mut tasks = self.tasks.write().await;
            if let Some(t) = tasks.get_mut(&task_id) {
                match &result {
                    Ok(content) => {
                        t.state = TaskState::Completed;
                        t.progress = 100;
                        t.result = Some(content.clone());
                        t.message = Some("Completed successfully".to_string());
                    }
                    Err(e) => {
                        t.state = TaskState::Failed;
                        t.error = Some(e.to_string());
                        t.message = Some(format!("Failed: {e}"));
                    }
                }
            }
        }

        // Send completion/failure event
        match result {
            Ok(content) => {
                let _ = tx.send(TaskEvent::Completed {
                    task_id: task_id.clone(),
                    result: TaskResult {
                        task_id,
                        state: TaskState::Completed,
                        content: Some(content),
                        data: None,
                        error: None,
                        token_usage: None,
                        duration_ms: 0,
                        sources: Vec::new(),
                    },
                });
            }
            Err(e) => {
                let _ = tx.send(TaskEvent::Failed {
                    task_id,
                    error: e.to_string(),
                });
            }
        }
    }

    /// Run a workflow locally.
    ///
    /// This is the core execution logic for embedded mode.
    /// Currently implements a basic execution loop; will be replaced
    /// with durable-shannon integration for full WASM workflow support.
    async fn run_local_workflow(
        &self,
        task: &Task,
        tx: &broadcast::Sender<TaskEvent>,
    ) -> anyhow::Result<String> {
        let task_id = &task.id;

        tracing::info!(
            "Executing local workflow for task {} with strategy {:?}",
            task_id,
            task.strategy
        );

        // Simulate progress updates
        for i in 1..=10 {
            tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
            
            // Update progress
            {
                let mut tasks = self.tasks.write().await;
                if let Some(t) = tasks.get_mut(task_id) {
                    // Check if cancelled
                    if t.state == TaskState::Cancelled {
                        return Err(anyhow::anyhow!("Task was cancelled"));
                    }
                    t.progress = i * 10;
                }
            }

            let _ = tx.send(TaskEvent::Progress {
                task_id: task_id.clone(),
                percent: i * 10,
                message: Some(format!("Processing step {i}/10")),
            });
        }

        // Return a placeholder result
        // In the full implementation, this would use the LLM orchestrator
        // and cognitive patterns from workflow-patterns crate
        Ok(format!(
            "Local workflow completed for query: '{}'. \
            Strategy: {:?}. \
            Note: Full WASM workflow integration pending.",
            task.query,
            task.strategy
        ))
    }
}

#[async_trait]
impl WorkflowEngineImpl for DurableEngine {
    fn engine_type(&self) -> WorkflowEngineType {
        WorkflowEngineType::Durable
    }

    async fn submit(&self, task: Task) -> anyhow::Result<TaskHandle> {
        // Check concurrency limit
        {
            let tasks = self.tasks.read().await;
            let active = tasks.values().filter(|t| t.state == TaskState::Running).count();
            if active >= self.max_concurrent {
                anyhow::bail!(
                    "Maximum concurrent workflows ({}) reached",
                    self.max_concurrent
                );
            }
        }

        let task_id = task.id.clone();
        let workflow_id = format!("durable-{}", task.id);

        // Create event channel
        let (tx, _rx) = broadcast::channel(64);

        // Register task
        {
            let mut tasks = self.tasks.write().await;
            tasks.insert(
                task_id.clone(),
                DurableTask {
                    id: task_id.clone(),
                    workflow_id: workflow_id.clone(),
                    state: TaskState::Pending,
                    progress: 0,
                    message: Some("Task submitted".to_string()),
                    result: None,
                    error: None,
                    task: task.clone(),
                    started_at: chrono::Utc::now(),
                },
            );
        }

        // Store channel
        {
            let mut channels = self.channels.write().await;
            channels.insert(task_id.clone(), tx.clone());
        }

        // Spawn execution task
        let engine = self.wasm_dir.clone();
        let task_clone = task.clone();
        let tx_clone = tx.clone();
        
        // We need to get self reference for the spawned task
        // Since we can't move self, we'll use a simple approach
        let tasks_ref = self.tasks.clone();
        let channels_ref = self.channels.clone();
        
        tokio::spawn(async move {
            // Simple inline execution for now
            // In a full implementation, this would call durable-shannon
            let task_id = task_clone.id.clone();

            // Update to running
            {
                let mut tasks = tasks_ref.write().await;
                if let Some(t) = tasks.get_mut(&task_id) {
                    t.state = TaskState::Running;
                }
            }

            let _ = tx_clone.send(TaskEvent::StateChanged {
                task_id: task_id.clone(),
                old_state: TaskState::Pending,
                new_state: TaskState::Running,
            });

            // Simulate work
            for i in 1..=10 {
                tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
                
                {
                    let mut tasks = tasks_ref.write().await;
                    if let Some(t) = tasks.get_mut(&task_id) {
                        if t.state == TaskState::Cancelled {
                            let _ = tx_clone.send(TaskEvent::Failed {
                                task_id: task_id.clone(),
                                error: "Task cancelled".to_string(),
                            });
                            return;
                        }
                        t.progress = i * 10;
                    }
                }

                let _ = tx_clone.send(TaskEvent::Progress {
                    task_id: task_id.clone(),
                    percent: i * 10,
                    message: Some(format!("Step {i}/10")),
                });
            }

            // Complete
            let result = format!(
                "Completed local workflow for: '{}'. Strategy: {:?}.",
                task_clone.query,
                task_clone.strategy
            );

            {
                let mut tasks = tasks_ref.write().await;
                if let Some(t) = tasks.get_mut(&task_id) {
                    t.state = TaskState::Completed;
                    t.progress = 100;
                    t.result = Some(result.clone());
                }
            }

            let _ = tx_clone.send(TaskEvent::Completed {
                task_id: task_id.clone(),
                result: TaskResult {
                    task_id,
                    state: TaskState::Completed,
                    content: Some(result),
                    data: None,
                    error: None,
                    token_usage: None,
                    duration_ms: 1000,
                    sources: Vec::new(),
                },
            });
        });

        tracing::info!(
            "Submitted task {} to Durable engine (embedded mode)",
            task_id
        );

        Ok(TaskHandle {
            task_id,
            workflow_id,
            state: TaskState::Pending,
            progress: 0,
            message: Some("Task submitted to local workflow engine".to_string()),
        })
    }

    async fn status(&self, task_id: &str) -> anyhow::Result<TaskHandle> {
        let tasks = self.tasks.read().await;
        let task = tasks
            .get(task_id)
            .ok_or_else(|| anyhow::anyhow!("Task not found: {}", task_id))?;

        Ok(TaskHandle {
            task_id: task.id.clone(),
            workflow_id: task.workflow_id.clone(),
            state: task.state,
            progress: task.progress,
            message: task.message.clone(),
        })
    }

    async fn result(&self, task_id: &str) -> anyhow::Result<TaskResult> {
        let tasks = self.tasks.read().await;
        let task = tasks
            .get(task_id)
            .ok_or_else(|| anyhow::anyhow!("Task not found: {}", task_id))?;

        if task.state != TaskState::Completed && task.state != TaskState::Failed {
            anyhow::bail!("Task {} is not yet complete (state: {:?})", task_id, task.state);
        }

        Ok(TaskResult {
            task_id: task.id.clone(),
            state: task.state,
            content: task.result.clone(),
            data: None,
            error: task.error.clone(),
            token_usage: None,
            duration_ms: chrono::Utc::now()
                .signed_duration_since(task.started_at)
                .num_milliseconds() as u64,
            sources: Vec::new(),
        })
    }

    async fn cancel(&self, task_id: &str, reason: Option<&str>) -> anyhow::Result<bool> {
        let mut tasks = self.tasks.write().await;
        if let Some(task) = tasks.get_mut(task_id) {
            if task.state == TaskState::Running || task.state == TaskState::Pending {
                task.state = TaskState::Cancelled;
                task.message = reason.map(String::from);
                tracing::info!("Cancelled task {}: {:?}", task_id, reason);
                return Ok(true);
            }
        }
        Ok(false)
    }

    async fn pause(&self, task_id: &str, reason: Option<&str>) -> anyhow::Result<bool> {
        let mut tasks = self.tasks.write().await;
        if let Some(task) = tasks.get_mut(task_id) {
            if task.state == TaskState::Running {
                task.state = TaskState::Paused;
                task.message = reason.map(String::from);
                tracing::info!("Paused task {}: {:?}", task_id, reason);
                return Ok(true);
            }
        }
        Ok(false)
    }

    async fn resume(&self, task_id: &str, reason: Option<&str>) -> anyhow::Result<bool> {
        let mut tasks = self.tasks.write().await;
        if let Some(task) = tasks.get_mut(task_id) {
            if task.state == TaskState::Paused {
                task.state = TaskState::Running;
                task.message = reason.map(String::from);
                tracing::info!("Resumed task {}: {:?}", task_id, reason);
                return Ok(true);
            }
        }
        Ok(false)
    }

    async fn subscribe(&self, task_id: &str) -> anyhow::Result<broadcast::Receiver<TaskEvent>> {
        let channels = self.channels.read().await;
        let tx = channels
            .get(task_id)
            .ok_or_else(|| anyhow::anyhow!("Task not found: {}", task_id))?;
        Ok(tx.subscribe())
    }

    async fn health(&self) -> anyhow::Result<bool> {
        // Local engine is always healthy if we got here
        Ok(true)
    }
}

// ============================================================================
// Temporal Engine (Cloud Mode) - Only available with grpc feature
// ============================================================================

/// Temporal engine implementation (via gRPC to Go orchestrator).
///
/// This engine is only available when the `grpc` feature is enabled.
/// It connects to the Go orchestrator service which coordinates with Temporal.
#[cfg(feature = "grpc")]
pub struct TemporalEngine {
    client: OrchestratorClient,
    _namespace: String,
    _task_queue: String,
}

#[cfg(feature = "grpc")]
impl TemporalEngine {
    /// Create a new Temporal engine.
    ///
    /// This establishes a gRPC connection to the Go orchestrator service.
    pub async fn new(
        endpoint: String,
        namespace: String,
        task_queue: String,
    ) -> anyhow::Result<Self> {
        tracing::info!(
            "Initializing Temporal engine (cloud mode) at {}, namespace: {}, queue: {}",
            endpoint,
            namespace,
            task_queue
        );

        let client = OrchestratorClient::new(OrchestratorClientConfig {
            endpoint,
            timeout_secs: 300,
            tls_enabled: false,
            connect_timeout_secs: 10,
        })
        .await?;

        Ok(Self {
            client,
            _namespace: namespace,
            _task_queue: task_queue,
        })
    }
}

#[cfg(feature = "grpc")]
#[async_trait]
impl WorkflowEngineImpl for TemporalEngine {
    fn engine_type(&self) -> WorkflowEngineType {
        WorkflowEngineType::Temporal
    }

    async fn submit(&self, task: Task) -> anyhow::Result<TaskHandle> {
        use crate::gateway::grpc_client::{SubmitTaskRequest, TaskMetadata};

        let request = SubmitTaskRequest {
            metadata: TaskMetadata {
                task_id: task.id.clone(),
                user_id: task.user_id.clone(),
                session_id: task.session_id.clone(),
                tenant_id: None,
                labels: task.labels.clone(),
                max_agents: task.max_agents.map(|v| v as i32),
                token_budget: task.token_budget,
            },
            query: task.query.clone(),
            context: task.context.clone(),
            auto_decompose: matches!(
                task.strategy,
                super::task::Strategy::Complex | super::task::Strategy::Research
            ),
            require_approval: task.require_approval,
        };

        let response = self.client.submit_task(request).await?;

        Ok(TaskHandle {
            task_id: response.task_id,
            workflow_id: response.workflow_id,
            state: TaskState::Pending,
            progress: 0,
            message: response.message,
        })
    }

    async fn status(&self, task_id: &str) -> anyhow::Result<TaskHandle> {
        use crate::gateway::grpc_client::GetTaskStatusRequest;

        let request = GetTaskStatusRequest {
            task_id: task_id.to_string(),
            include_details: false,
        };

        let response = self.client.get_task_status(request).await?;

        let state = match response.status {
            crate::gateway::grpc_client::TaskStatus::Queued => TaskState::Pending,
            crate::gateway::grpc_client::TaskStatus::Running => TaskState::Running,
            crate::gateway::grpc_client::TaskStatus::Completed => TaskState::Completed,
            crate::gateway::grpc_client::TaskStatus::Failed => TaskState::Failed,
            crate::gateway::grpc_client::TaskStatus::Cancelled => TaskState::Cancelled,
            crate::gateway::grpc_client::TaskStatus::Timeout => TaskState::Timeout,
            crate::gateway::grpc_client::TaskStatus::Paused => TaskState::Paused,
            crate::gateway::grpc_client::TaskStatus::Unspecified => TaskState::Pending,
        };

        Ok(TaskHandle {
            task_id: response.task_id,
            workflow_id: String::new(),
            state,
            progress: (response.progress * 100.0) as u8,
            message: response.error_message,
        })
    }

    async fn result(&self, task_id: &str) -> anyhow::Result<TaskResult> {
        use crate::gateway::grpc_client::GetTaskStatusRequest;

        let request = GetTaskStatusRequest {
            task_id: task_id.to_string(),
            include_details: true,
        };

        let response = self.client.get_task_status(request).await?;

        let state = match response.status {
            crate::gateway::grpc_client::TaskStatus::Completed => TaskState::Completed,
            crate::gateway::grpc_client::TaskStatus::Failed => TaskState::Failed,
            _ => TaskState::Running,
        };

        Ok(TaskResult {
            task_id: response.task_id,
            state,
            content: response.result,
            data: None,
            error: response.error_message,
            token_usage: response.metrics.map(|m| super::task::TokenUsage {
                prompt_tokens: m.token_usage.as_ref().map_or(0, |u| u.prompt_tokens as u32),
                completion_tokens: m.token_usage.as_ref().map_or(0, |u| u.completion_tokens as u32),
                total_tokens: m.token_usage.as_ref().map_or(0, |u| u.total_tokens as u32),
                cost_usd: m.token_usage.as_ref().map_or(0.0, |u| u.cost_usd),
            }),
            duration_ms: response.metrics.map_or(0, |m| m.latency_ms as u64),
            sources: Vec::new(),
        })
    }

    async fn cancel(&self, task_id: &str, reason: Option<&str>) -> anyhow::Result<bool> {
        use crate::gateway::grpc_client::CancelTaskRequest;

        let request = CancelTaskRequest {
            task_id: task_id.to_string(),
            reason: reason.map(String::from),
        };

        let response = self.client.cancel_task(request).await?;
        Ok(response.success)
    }

    async fn pause(&self, task_id: &str, reason: Option<&str>) -> anyhow::Result<bool> {
        self.client.pause_task(task_id, reason).await
    }

    async fn resume(&self, task_id: &str, reason: Option<&str>) -> anyhow::Result<bool> {
        self.client.resume_task(task_id, reason).await
    }

    async fn subscribe(&self, _task_id: &str) -> anyhow::Result<broadcast::Receiver<TaskEvent>> {
        // TODO: Implement streaming subscription via gRPC streaming
        let (tx, rx) = broadcast::channel(16);
        drop(tx);
        Ok(rx)
    }

    async fn health(&self) -> anyhow::Result<bool> {
        self.client.health_check().await
    }
}

// Stub for when grpc feature is not enabled
#[cfg(not(feature = "grpc"))]
pub struct TemporalEngine;

#[cfg(not(feature = "grpc"))]
impl TemporalEngine {
    pub async fn new(
        _endpoint: String,
        _namespace: String,
        _task_queue: String,
    ) -> anyhow::Result<Self> {
        anyhow::bail!(
            "Temporal engine requires the 'grpc' feature. \
            Use embedded mode (Durable engine) or enable the 'grpc' feature for cloud mode."
        )
    }
}

#[cfg(not(feature = "grpc"))]
#[async_trait]
impl WorkflowEngineImpl for TemporalEngine {
    fn engine_type(&self) -> WorkflowEngineType {
        WorkflowEngineType::Temporal
    }

    async fn submit(&self, _task: Task) -> anyhow::Result<TaskHandle> {
        anyhow::bail!("Temporal engine not available - grpc feature not enabled")
    }

    async fn status(&self, _task_id: &str) -> anyhow::Result<TaskHandle> {
        anyhow::bail!("Temporal engine not available - grpc feature not enabled")
    }

    async fn result(&self, _task_id: &str) -> anyhow::Result<TaskResult> {
        anyhow::bail!("Temporal engine not available - grpc feature not enabled")
    }

    async fn cancel(&self, _task_id: &str, _reason: Option<&str>) -> anyhow::Result<bool> {
        anyhow::bail!("Temporal engine not available - grpc feature not enabled")
    }

    async fn pause(&self, _task_id: &str, _reason: Option<&str>) -> anyhow::Result<bool> {
        anyhow::bail!("Temporal engine not available - grpc feature not enabled")
    }

    async fn resume(&self, _task_id: &str, _reason: Option<&str>) -> anyhow::Result<bool> {
        anyhow::bail!("Temporal engine not available - grpc feature not enabled")
    }

    async fn subscribe(&self, _task_id: &str) -> anyhow::Result<broadcast::Receiver<TaskEvent>> {
        anyhow::bail!("Temporal engine not available - grpc feature not enabled")
    }

    async fn health(&self) -> anyhow::Result<bool> {
        anyhow::bail!("Temporal engine not available - grpc feature not enabled")
    }
}
