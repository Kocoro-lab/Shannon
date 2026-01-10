//! Embedded workflow worker.
//!
//! Provides an in-process workflow execution engine for Tauri desktop/mobile apps.

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::Arc;

use tokio::sync::{broadcast, Mutex, RwLock};

use crate::backends::EventLog;
use crate::Event;

/// Workflow state for tracking execution.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum WorkflowState {
    /// Workflow is pending execution.
    Pending,
    /// Workflow is currently executing.
    Running,
    /// Workflow completed successfully.
    Completed,
    /// Workflow failed.
    Failed,
    /// Workflow was cancelled.
    Cancelled,
}

/// Handle for tracking a submitted workflow.
#[derive(Debug)]
pub struct WorkflowHandle {
    /// Workflow ID.
    pub workflow_id: String,
    /// Current state.
    pub state: WorkflowState,
    /// Progress (0-100).
    pub progress: u8,
    /// Event sender for subscribing (we store sender so we can resubscribe).
    event_tx: broadcast::Sender<WorkflowEvent>,
}

impl WorkflowHandle {
    /// Wait for the workflow to complete.
    pub async fn result(&mut self) -> anyhow::Result<serde_json::Value> {
        let mut rx = self.event_tx.subscribe();
        loop {
            match rx.recv().await {
                Ok(event) => match event {
                    WorkflowEvent::Completed { output, .. } => return Ok(output),
                    WorkflowEvent::Failed { error, .. } => anyhow::bail!(error),
                    WorkflowEvent::Progress { percent, .. } => {
                        self.progress = percent;
                    }
                    _ => {}
                },
                Err(broadcast::error::RecvError::Closed) => {
                    anyhow::bail!("Workflow channel closed unexpectedly")
                }
                Err(broadcast::error::RecvError::Lagged(_)) => {
                    // Skip lagged messages
                    continue;
                }
            }
        }
    }

    /// Subscribe to workflow events.
    #[must_use]
    pub fn subscribe(&self) -> broadcast::Receiver<WorkflowEvent> {
        self.event_tx.subscribe()
    }
}

/// Workflow event for streaming updates.
#[derive(Debug, Clone)]
pub enum WorkflowEvent {
    /// Workflow started.
    Started { workflow_id: String },
    /// Progress updated.
    Progress {
        workflow_id: String,
        percent: u8,
        message: Option<String>,
    },
    /// Activity completed.
    ActivityCompleted {
        workflow_id: String,
        activity_id: String,
        output: serde_json::Value,
    },
    /// Workflow completed.
    Completed {
        workflow_id: String,
        output: serde_json::Value,
    },
    /// Workflow failed.
    Failed { workflow_id: String, error: String },
}

/// Embedded workflow worker.
///
/// Executes WASM workflows in-process with durable state management.
pub struct EmbeddedWorker<E: EventLog> {
    /// Event log for persistence.
    event_log: Arc<E>,
    /// Directory containing WASM workflow modules.
    wasm_dir: PathBuf,
    /// Maximum concurrent workflows.
    max_concurrent: usize,
    /// Active workflows.
    workflows: RwLock<HashMap<String, WorkflowInfo>>,
    /// Event broadcast channels.
    channels: Mutex<HashMap<String, broadcast::Sender<WorkflowEvent>>>,
}

/// Information about an active workflow.
struct WorkflowInfo {
    _workflow_type: String,
    state: WorkflowState,
    _started_at: chrono::DateTime<chrono::Utc>,
}

impl<E: EventLog + 'static> EmbeddedWorker<E> {
    /// Create a new embedded worker.
    pub async fn new(event_log: Arc<E>, wasm_dir: PathBuf, max_concurrent: usize) -> anyhow::Result<Self> {
        tracing::info!(
            "Creating embedded worker with WASM dir: {:?}, max_concurrent: {}",
            wasm_dir,
            max_concurrent
        );

        // Ensure WASM directory exists
        if !wasm_dir.exists() {
            std::fs::create_dir_all(&wasm_dir)?;
        }

        Ok(Self {
            event_log,
            wasm_dir,
            max_concurrent,
            workflows: RwLock::new(HashMap::new()),
            channels: Mutex::new(HashMap::new()),
        })
    }

    /// Submit a workflow for execution.
    pub async fn submit(
        &self,
        workflow_type: &str,
        input: serde_json::Value,
    ) -> anyhow::Result<WorkflowHandle> {
        let workflow_id = uuid::Uuid::new_v4().to_string();

        // Check concurrency limit
        {
            let workflows = self.workflows.read().await;
            let active = workflows
                .values()
                .filter(|w| w.state == WorkflowState::Running)
                .count();
            if active >= self.max_concurrent {
                anyhow::bail!(
                    "Max concurrent workflows ({}) reached",
                    self.max_concurrent
                );
            }
        }

        // Create event channel
        let (tx, _rx) = broadcast::channel(64);
        {
            let mut channels = self.channels.lock().await;
            channels.insert(workflow_id.clone(), tx.clone());
        }

        // Record workflow start event
        self.event_log
            .append(
                &workflow_id,
                Event::WorkflowStarted {
                    workflow_id: workflow_id.clone(),
                    workflow_type: workflow_type.to_string(),
                    input: input.clone(),
                    timestamp: chrono::Utc::now(),
                },
            )
            .await?;

        // Register workflow
        {
            let mut workflows = self.workflows.write().await;
            workflows.insert(
                workflow_id.clone(),
                WorkflowInfo {
                    _workflow_type: workflow_type.to_string(),
                    state: WorkflowState::Running,
                    _started_at: chrono::Utc::now(),
                },
            );
        }

        // Notify start
        let _ = tx.send(WorkflowEvent::Started {
            workflow_id: workflow_id.clone(),
        });

        // Spawn execution task
        let worker_event_log = Arc::clone(&self.event_log);
        let worker_workflow_id = workflow_id.clone();
        let worker_workflow_type = workflow_type.to_string();
        let worker_tx = tx.clone();
        let wasm_dir = self.wasm_dir.clone();

        tokio::spawn(async move {
            let result = Self::execute_workflow(
                worker_event_log,
                wasm_dir,
                &worker_workflow_id,
                &worker_workflow_type,
                input,
                worker_tx.clone(),
            )
            .await;

            match result {
                Ok(output) => {
                    let _ = worker_tx.send(WorkflowEvent::Completed {
                        workflow_id: worker_workflow_id,
                        output,
                    });
                }
                Err(e) => {
                    let _ = worker_tx.send(WorkflowEvent::Failed {
                        workflow_id: worker_workflow_id,
                        error: e.to_string(),
                    });
                }
            }
        });

        Ok(WorkflowHandle {
            workflow_id,
            state: WorkflowState::Running,
            progress: 0,
            event_tx: tx,
        })
    }

    /// Execute a workflow (placeholder implementation).
    /// Execute a workflow using MicroSandbox.
    async fn execute_workflow(
        event_log: Arc<E>,
        wasm_dir: PathBuf,
        workflow_id: &str,
        workflow_type: &str,
        input: serde_json::Value,
        tx: broadcast::Sender<WorkflowEvent>,
    ) -> anyhow::Result<serde_json::Value> {
        use crate::microsandbox::{WasmSandbox, SandboxCapabilities};

        let wasm_path = wasm_dir.join(format!("{}.wasm", workflow_type));
        
        let wasm_bytes = if wasm_path.exists() {
             tracing::info!("Loading WASM workflow from {:?}", wasm_path);
             tokio::fs::read(&wasm_path).await?
        } else {
             tracing::warn!("WASM module not found at {:?}. Falling back to simulation for testing.", wasm_path);
             // TODO: Remove fallback once we have real WASM modules
             // Simulate progress
            for i in 1..=10 {
                tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
                let _ = tx.send(WorkflowEvent::Progress {
                    workflow_id: workflow_id.to_string(),
                    percent: i * 10,
                    message: Some(format!("Step {i}/10")),
                });
            }

            // Record completion
            let output = serde_json::json!({
                "status": "completed", 
                "workflow_type": workflow_type,
                "message": "Workflow simulation (WASM not found)" 
            });

            event_log.append(
                workflow_id,
                Event::WorkflowCompleted {
                    output: output.clone(),
                    timestamp: chrono::Utc::now(),
                },
            ).await?;

            return Ok(output);
        };

        // Initialize Sandbox
        let sandbox = WasmSandbox::load(&wasm_bytes)?;
        
        // Define Capabilities (TODO: make configurable per workflow)
        let caps = SandboxCapabilities {
            timeout_ms: 30_000, // 30s timeout
            max_memory_mb: 512,
            ..Default::default()
        };

        let mut process = sandbox.instantiate(caps).await?;

        // Execute Entrypoint
        // We assume the WASM module exports a function `run_workflow(input: String) -> String`
        // or uses the component model. For this MVP, we use the JSON interface pattern.
        tracing::info!("Starting WASM workflow {} (PID: {})", workflow_id, process.pid());
        
        let output = match process.call_json("run_workflow", &input).await {
            Ok(res) => res,
            Err(e) => {
                 tracing::error!("WASM execution failed: {}", e);
                 // Record failure
                 event_log.append(
                    workflow_id,
                    Event::WorkflowFailed {
                        error: e.to_string(),
                        timestamp: chrono::Utc::now(),
                    },
                 ).await?;
                 return Err(anyhow::anyhow!(e));
            }
        };
        
        process.kill();

        // Record completion
        event_log.append(
            workflow_id,
            Event::WorkflowCompleted {
                output: output.clone(),
                timestamp: chrono::Utc::now(),
            },
        ).await?;

        Ok(output)
    }

    /// Get the status of a workflow.
    pub async fn status(&self, workflow_id: &str) -> anyhow::Result<WorkflowState> {
        let workflows = self.workflows.read().await;
        workflows
            .get(workflow_id)
            .map(|w| w.state)
            .ok_or_else(|| anyhow::anyhow!("Workflow not found: {}", workflow_id))
    }

    /// Cancel a workflow.
    pub async fn cancel(&self, workflow_id: &str) -> anyhow::Result<bool> {
        let mut workflows = self.workflows.write().await;
        if let Some(info) = workflows.get_mut(workflow_id) {
            if info.state == WorkflowState::Running {
                info.state = WorkflowState::Cancelled;
                return Ok(true);
            }
        }
        Ok(false)
    }

    /// Replay a workflow from its event log.
    pub async fn replay(&self, workflow_id: &str) -> anyhow::Result<Vec<Event>> {
        self.event_log.replay(workflow_id).await
    }
}

impl<E: EventLog> std::fmt::Debug for EmbeddedWorker<E> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("EmbeddedWorker")
            .field("wasm_dir", &self.wasm_dir)
            .field("max_concurrent", &self.max_concurrent)
            .finish()
    }
}
