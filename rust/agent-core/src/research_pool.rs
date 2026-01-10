//! Research task pool for managing long-running research operations.
//!
//! This module provides a worker pool for executing research tasks in isolated
//! environments, with support for checkpointing and resume capabilities.

use std::collections::HashMap;
use std::sync::Arc;
use std::time::{Duration, Instant};

use async_trait::async_trait;
use crossbeam_channel::{bounded, Receiver, Sender};
use parking_lot::RwLock;
use serde::{Deserialize, Serialize};
use tokio::sync::oneshot;
use uuid::Uuid;

/// A research job to be executed.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ResearchJob {
    /// Unique job identifier.
    pub id: String,
    /// The research query.
    pub query: String,
    /// Additional context.
    pub context: serde_json::Value,
    /// Maximum execution time in seconds.
    pub timeout_secs: u64,
    /// Priority (higher = more urgent).
    pub priority: u32,
}

impl ResearchJob {
    /// Create a new research job.
    pub fn new(query: impl Into<String>) -> Self {
        Self {
            id: Uuid::new_v4().to_string(),
            query: query.into(),
            context: serde_json::Value::Null,
            timeout_secs: 300, // 5 minutes default
            priority: 0,
        }
    }

    /// Set the context.
    pub fn with_context(mut self, context: serde_json::Value) -> Self {
        self.context = context;
        self
    }

    /// Set the timeout.
    pub fn with_timeout(mut self, secs: u64) -> Self {
        self.timeout_secs = secs;
        self
    }

    /// Set the priority.
    pub fn with_priority(mut self, priority: u32) -> Self {
        self.priority = priority;
        self
    }
}

/// Result of a research job.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ResearchResult {
    /// Job ID this result belongs to.
    pub job_id: String,
    /// Whether the job succeeded.
    pub success: bool,
    /// Result content.
    pub content: String,
    /// Error message if failed.
    pub error: Option<String>,
    /// Execution time in milliseconds.
    pub execution_time_ms: u64,
    /// Tokens used.
    pub tokens_used: u32,
    /// Checkpoint path for resume.
    pub checkpoint_path: Option<String>,
}

impl ResearchResult {
    /// Create a successful result.
    pub fn success(job_id: impl Into<String>, content: impl Into<String>, execution_time_ms: u64) -> Self {
        Self {
            job_id: job_id.into(),
            success: true,
            content: content.into(),
            error: None,
            execution_time_ms,
            tokens_used: 0,
            checkpoint_path: None,
        }
    }

    /// Create a failed result.
    pub fn failure(job_id: impl Into<String>, error: impl Into<String>, execution_time_ms: u64) -> Self {
        Self {
            job_id: job_id.into(),
            success: false,
            content: String::new(),
            error: Some(error.into()),
            execution_time_ms,
            tokens_used: 0,
            checkpoint_path: None,
        }
    }
}

/// Job status for tracking.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum JobStatus {
    /// Job is queued.
    Queued,
    /// Job is currently running.
    Running,
    /// Job completed successfully.
    Completed,
    /// Job failed.
    Failed,
    /// Job was cancelled.
    Cancelled,
}

/// Internal job state.
struct JobState {
    job: ResearchJob,
    status: JobStatus,
    submitted_at: Instant,
    started_at: Option<Instant>,
    result_sender: Option<oneshot::Sender<ResearchResult>>,
}

impl std::fmt::Debug for JobState {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("JobState")
            .field("job", &self.job)
            .field("status", &self.status)
            .field("submitted_at", &self.submitted_at)
            .field("started_at", &self.started_at)
            .field("result_sender", &"<oneshot::Sender<ResearchResult>>")
            .finish()
    }
}

/// Trait for executing research jobs.
#[async_trait]
pub trait JobExecutor: Send + Sync {
    /// Execute a research job.
    async fn execute(&self, job: ResearchJob) -> ResearchResult;

    /// Check if the executor supports checkpointing.
    fn supports_checkpoint(&self) -> bool {
        false
    }

    /// Create a checkpoint for a running job.
    async fn checkpoint(&self, _job_id: &str, _path: &str) -> anyhow::Result<()> {
        Err(anyhow::anyhow!("Checkpointing not supported"))
    }

    /// Restore a job from a checkpoint.
    async fn restore(&self, _path: &str) -> anyhow::Result<ResearchJob> {
        Err(anyhow::anyhow!("Checkpointing not supported"))
    }
}

/// Default executor that calls the LLM service.
pub struct LlmExecutor {
    llm_service_url: String,
    client: reqwest::Client,
}

impl std::fmt::Debug for LlmExecutor {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("LlmExecutor")
            .field("llm_service_url", &self.llm_service_url)
            .field("client", &"<reqwest::Client>")
            .finish()
    }
}

impl LlmExecutor {
    /// Create a new LLM executor.
    pub fn new(llm_service_url: impl Into<String>) -> Self {
        Self {
            llm_service_url: llm_service_url.into(),
            client: reqwest::Client::builder()
                .timeout(Duration::from_secs(600))
                .build()
                .expect("Failed to create HTTP client"),
        }
    }
}

#[async_trait]
impl JobExecutor for LlmExecutor {
    async fn execute(&self, job: ResearchJob) -> ResearchResult {
        let start = Instant::now();
        
        let url = format!("{}/agent/research", self.llm_service_url);
        
        let response = self.client
            .post(&url)
            .json(&serde_json::json!({
                "query": job.query,
                "context": job.context,
                "timeout_secs": job.timeout_secs,
            }))
            .send()
            .await;

        let execution_time_ms = start.elapsed().as_millis() as u64;

        match response {
            Ok(resp) => {
                if resp.status().is_success() {
                    match resp.text().await {
                        Ok(content) => ResearchResult::success(job.id, content, execution_time_ms),
                        Err(e) => ResearchResult::failure(job.id, format!("Failed to read response: {}", e), execution_time_ms),
                    }
                } else {
                    let error = resp.text().await.unwrap_or_else(|_| "Unknown error".to_string());
                    ResearchResult::failure(job.id, error, execution_time_ms)
                }
            }
            Err(e) => ResearchResult::failure(job.id, format!("Request failed: {}", e), execution_time_ms),
        }
    }
}

/// Configuration for the research pool.
#[derive(Debug, Clone)]
pub struct ResearchPoolConfig {
    /// Number of worker threads.
    pub worker_count: usize,
    /// Maximum queue size.
    pub max_queue_size: usize,
    /// Default job timeout in seconds.
    pub default_timeout_secs: u64,
    /// Checkpoint directory.
    pub checkpoint_dir: Option<String>,
}

impl Default for ResearchPoolConfig {
    fn default() -> Self {
        Self {
            worker_count: 4,
            max_queue_size: 100,
            default_timeout_secs: 300,
            checkpoint_dir: None,
        }
    }
}

/// A pool of workers for executing research jobs.
pub struct ResearchPool {
    /// Configuration.
    config: ResearchPoolConfig,
    /// Job executor.
    executor: Arc<dyn JobExecutor>,
    /// Active jobs by ID.
    jobs: Arc<RwLock<HashMap<String, JobState>>>,
    /// Completed results by ID.
    results: Arc<RwLock<HashMap<String, ResearchResult>>>,
    /// Job submission channel.
    job_sender: Sender<ResearchJob>,
    /// Shutdown signal.
    shutdown: Arc<RwLock<bool>>,
}

impl std::fmt::Debug for ResearchPool {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ResearchPool")
            .field("config", &self.config)
            .field("executor", &"<Arc<dyn JobExecutor>>")
            .field("jobs", &self.jobs)
            .field("results", &self.results)
            .field("job_sender", &"<Sender<ResearchJob>>")
            .field("shutdown", &self.shutdown)
            .finish()
    }
}

impl ResearchPool {
    /// Create a new research pool.
    pub fn new(config: ResearchPoolConfig, executor: Arc<dyn JobExecutor>) -> Self {
        let (job_sender, job_receiver) = bounded::<ResearchJob>(config.max_queue_size);
        
        let pool = Self {
            config: config.clone(),
            executor: executor.clone(),
            jobs: Arc::new(RwLock::new(HashMap::new())),
            results: Arc::new(RwLock::new(HashMap::new())),
            job_sender,
            shutdown: Arc::new(RwLock::new(false)),
        };

        // Spawn worker tasks
        for worker_id in 0..config.worker_count {
            let receiver = job_receiver.clone();
            let executor = executor.clone();
            let jobs = pool.jobs.clone();
            let results = pool.results.clone();
            let shutdown = pool.shutdown.clone();

            tokio::spawn(async move {
                Self::worker_loop(worker_id, receiver, executor, jobs, results, shutdown).await;
            });
        }

        pool
    }

    /// Worker loop that processes jobs.
    async fn worker_loop(
        worker_id: usize,
        receiver: Receiver<ResearchJob>,
        executor: Arc<dyn JobExecutor>,
        jobs: Arc<RwLock<HashMap<String, JobState>>>,
        results: Arc<RwLock<HashMap<String, ResearchResult>>>,
        shutdown: Arc<RwLock<bool>>,
    ) {
        tracing::info!("Worker {} started", worker_id);

        loop {
            // Check for shutdown
            if *shutdown.read() {
                tracing::info!("Worker {} shutting down", worker_id);
                break;
            }

            // Try to receive a job with timeout
            match receiver.recv_timeout(Duration::from_millis(100)) {
                Ok(job) => {
                    let job_id = job.id.clone();
                    tracing::info!("Worker {} processing job {}", worker_id, job_id);

                    // Update job status to running
                    {
                        let mut jobs = jobs.write();
                        if let Some(state) = jobs.get_mut(&job_id) {
                            state.status = JobStatus::Running;
                            state.started_at = Some(Instant::now());
                        }
                    }

                    // Execute the job
                    let result = executor.execute(job).await;
                    
                    // Store result and update job status
                    {
                        let mut jobs = jobs.write();
                        let mut results = results.write();

                        if let Some(state) = jobs.get_mut(&job_id) {
                            state.status = if result.success {
                                JobStatus::Completed
                            } else {
                                JobStatus::Failed
                            };

                            // Send result through oneshot channel if waiting
                            if let Some(sender) = state.result_sender.take() {
                                let _ = sender.send(result.clone());
                            }
                        }

                        results.insert(job_id.clone(), result);
                    }

                    tracing::info!("Worker {} completed job {}", worker_id, job_id);
                }
                Err(crossbeam_channel::RecvTimeoutError::Timeout) => {
                    // No job available, continue
                }
                Err(crossbeam_channel::RecvTimeoutError::Disconnected) => {
                    tracing::warn!("Worker {} channel disconnected", worker_id);
                    break;
                }
            }
        }
    }

    /// Submit a job for execution.
    pub fn submit(&self, job: ResearchJob) -> anyhow::Result<String> {
        let job_id = job.id.clone();

        // Store job state
        {
            let mut jobs = self.jobs.write();
            jobs.insert(
                job_id.clone(),
                JobState {
                    job: job.clone(),
                    status: JobStatus::Queued,
                    submitted_at: Instant::now(),
                    started_at: None,
                    result_sender: None,
                },
            );
        }

        // Send to worker pool
        self.job_sender
            .try_send(job)
            .map_err(|e| anyhow::anyhow!("Failed to submit job: {}", e))?;

        Ok(job_id)
    }

    /// Submit a job and wait for the result.
    pub async fn submit_and_wait(&self, job: ResearchJob) -> anyhow::Result<ResearchResult> {
        let job_id = job.id.clone();
        let (tx, rx) = oneshot::channel();

        // Store job state with result sender
        {
            let mut jobs = self.jobs.write();
            jobs.insert(
                job_id.clone(),
                JobState {
                    job: job.clone(),
                    status: JobStatus::Queued,
                    submitted_at: Instant::now(),
                    started_at: None,
                    result_sender: Some(tx),
                },
            );
        }

        // Send to worker pool
        self.job_sender
            .try_send(job)
            .map_err(|e| anyhow::anyhow!("Failed to submit job: {}", e))?;

        // Wait for result
        rx.await
            .map_err(|_| anyhow::anyhow!("Job was cancelled or worker died"))
    }

    /// Get job status.
    pub fn get_status(&self, job_id: &str) -> Option<JobStatus> {
        let jobs = self.jobs.read();
        jobs.get(job_id).map(|s| s.status)
    }

    /// Get job result if available.
    pub fn get_result(&self, job_id: &str) -> Option<ResearchResult> {
        let results = self.results.read();
        results.get(job_id).cloned()
    }

    /// Cancel a job.
    pub fn cancel(&self, job_id: &str) -> bool {
        let mut jobs = self.jobs.write();
        if let Some(state) = jobs.get_mut(job_id) {
            if state.status == JobStatus::Queued {
                state.status = JobStatus::Cancelled;
                return true;
            }
        }
        false
    }

    /// Get the number of queued jobs.
    pub fn queue_length(&self) -> usize {
        self.job_sender.len()
    }

    /// Get the number of active jobs.
    pub fn active_count(&self) -> usize {
        let jobs = self.jobs.read();
        jobs.values()
            .filter(|s| s.status == JobStatus::Running)
            .count()
    }

    /// Shutdown the pool.
    pub fn shutdown(&self) {
        *self.shutdown.write() = true;
    }
}

impl Drop for ResearchPool {
    fn drop(&mut self) {
        self.shutdown();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    struct MockExecutor;

    #[async_trait]
    impl JobExecutor for MockExecutor {
        async fn execute(&self, job: ResearchJob) -> ResearchResult {
            tokio::time::sleep(Duration::from_millis(10)).await;
            ResearchResult::success(job.id, "Mock result", 10)
        }
    }

    #[tokio::test]
    async fn test_research_pool() {
        let config = ResearchPoolConfig {
            worker_count: 2,
            max_queue_size: 10,
            ..Default::default()
        };

        let executor = Arc::new(MockExecutor);
        let pool = ResearchPool::new(config, executor);

        let job = ResearchJob::new("Test query");
        let job_id = pool.submit(job).unwrap();

        // Wait a bit for the job to complete
        tokio::time::sleep(Duration::from_millis(100)).await;

        let result = pool.get_result(&job_id);
        assert!(result.is_some());
        assert!(result.unwrap().success);
    }
}
