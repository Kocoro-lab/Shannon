use anyhow::{Context, Result};
use prometheus::{
    register_counter_vec, register_gauge, register_histogram_vec, CounterVec, Encoder, Gauge,
    HistogramVec, TextEncoder,
};
use std::sync::OnceLock;
use std::time::Instant;
use tokio::io::AsyncWriteExt;

// Task metrics
pub static TASKS_TOTAL: OnceLock<CounterVec> = OnceLock::new();
pub static TASK_DURATION: OnceLock<HistogramVec> = OnceLock::new();
pub static TASK_TOKENS: OnceLock<HistogramVec> = OnceLock::new();

// FSM metrics
pub static FSM_STATE_TRANSITIONS: OnceLock<CounterVec> = OnceLock::new();
pub static FSM_CURRENT_STATE: OnceLock<Gauge> = OnceLock::new();

// Memory metrics
pub static MEMORY_POOL_USED_BYTES: OnceLock<Gauge> = OnceLock::new();
pub static MEMORY_POOL_TOTAL_BYTES: OnceLock<Gauge> = OnceLock::new();

// Tool metrics
pub static TOOL_EXECUTIONS: OnceLock<CounterVec> = OnceLock::new();
pub static TOOL_DURATION: OnceLock<HistogramVec> = OnceLock::new();
pub static TOOL_SELECTION_DURATION: OnceLock<HistogramVec> = OnceLock::new();

// gRPC metrics
pub static GRPC_REQUESTS: OnceLock<CounterVec> = OnceLock::new();
pub static GRPC_REQUEST_DURATION: OnceLock<HistogramVec> = OnceLock::new();

// Enforcement metrics
pub static ENFORCEMENT_DROPS: OnceLock<CounterVec> = OnceLock::new(); // labels: reason
pub static ENFORCEMENT_ALLOWED: OnceLock<CounterVec> = OnceLock::new(); // labels: outcome

// Thread-safe initialization result
static INIT_RESULT: OnceLock<Result<()>> = OnceLock::new();

pub struct TaskTimer {
    start: Instant,
    mode: String,
}

impl TaskTimer {
    pub fn new(mode: &str) -> Self {
        Self {
            start: Instant::now(),
            mode: mode.to_string(),
        }
    }

    pub fn complete(self, status: &str, tokens: Option<i32>) {
        let duration = self.start.elapsed().as_secs_f64();
        let mode_string = self.mode.to_string();
        let status_string = status.to_string();

        if let Some(tasks_total) = TASKS_TOTAL.get() {
            tasks_total.with_label_values(&[&mode_string, &status_string]).inc();
        }
        if let Some(task_duration) = TASK_DURATION.get() {
            task_duration
                .with_label_values(&[&mode_string])
                .observe(duration);
        }

        if let Some(token_count) = tokens {
            if let Some(task_tokens) = TASK_TOKENS.get() {
                let default_string = "default".to_string();
                task_tokens
                    .with_label_values(&[&mode_string, &default_string])
                    .observe(token_count as f64);
            }
        }
    }
}

pub fn get_metrics() -> String {
    let encoder = TextEncoder::new();
    let metric_families = prometheus::gather();
    let mut buffer = Vec::new();
    // If encoding fails, return empty metrics rather than panic
    if encoder.encode(&metric_families, &mut buffer).is_err() {
        return String::new();
    }
    String::from_utf8(buffer).unwrap_or_else(|_| String::new())
}

// Thread-safe metrics initialization with proper synchronization
pub fn init_metrics() -> Result<()> {
    match INIT_RESULT.get_or_init(init_metrics_internal) {
        Ok(()) => Ok(()),
        Err(e) => Err(anyhow::anyhow!("Metrics initialization failed: {}", e)),
    }
}

// Internal initialization function - only called once
fn init_metrics_internal() -> Result<()> {
    // Check if already initialized (defensive programming)
    if TASKS_TOTAL.get().is_some() {
        return Ok(()); // Already initialized, not an error
    }

    // Task metrics
    let tasks_total = register_counter_vec!(
        "agent_core_tasks_total",
        "Total number of tasks processed",
        &["mode", "status"]
    )
    .context("Failed to register TASKS_TOTAL metric")?;

    let task_duration = register_histogram_vec!(
        "agent_core_task_duration_seconds",
        "Task execution duration in seconds",
        &["mode"]
    )
    .context("Failed to register TASK_DURATION metric")?;

    let task_tokens = register_histogram_vec!(
        "agent_core_task_tokens",
        "Number of tokens used per task",
        &["mode", "model"]
    )
    .context("Failed to register TASK_TOKENS metric")?;

    // FSM metrics
    let fsm_state_transitions = register_counter_vec!(
        "agent_core_fsm_transitions_total",
        "Total FSM state transitions",
        &["from_state", "to_state"]
    )
    .context("Failed to register FSM_STATE_TRANSITIONS metric")?;

    let fsm_current_state = register_gauge!(
        "agent_core_fsm_current_state",
        "Current FSM state (encoded as number)"
    )
    .context("Failed to register FSM_CURRENT_STATE metric")?;

    // Memory metrics
    let memory_pool_used_bytes = register_gauge!(
        "agent_core_memory_pool_used_bytes",
        "Memory pool used bytes"
    )
    .context("Failed to register MEMORY_POOL_USED_BYTES metric")?;

    let memory_pool_total_bytes = register_gauge!(
        "agent_core_memory_pool_total_bytes",
        "Memory pool total bytes"
    )
    .context("Failed to register MEMORY_POOL_TOTAL_BYTES metric")?;

    // Tool metrics
    let tool_executions = register_counter_vec!(
        "agent_core_tool_executions_total",
        "Total tool executions",
        &["tool_name", "status"]
    )
    .context("Failed to register TOOL_EXECUTIONS metric")?;

    let tool_duration = register_histogram_vec!(
        "agent_core_tool_duration_seconds",
        "Tool execution duration in seconds",
        &["tool_name"]
    )
    .context("Failed to register TOOL_DURATION metric")?;

    let tool_selection_duration = register_histogram_vec!(
        "agent_core_tool_selection_duration_seconds",
        "Tool selection latency in seconds",
        &["status"]
    )
    .context("Failed to register TOOL_SELECTION_DURATION metric")?;

    // gRPC metrics
    let grpc_requests = register_counter_vec!(
        "agent_core_grpc_requests_total",
        "Total gRPC requests",
        &["method", "status"]
    )
    .context("Failed to register GRPC_REQUESTS metric")?;

    let grpc_request_duration = register_histogram_vec!(
        "agent_core_grpc_request_duration_seconds",
        "gRPC request duration in seconds",
        &["method"]
    )
    .context("Failed to register GRPC_REQUEST_DURATION metric")?;

    // Enforcement metrics
    let enforcement_drops = register_counter_vec!(
        "agent_core_enforcement_drops_total",
        "Total requests dropped by enforcement layer",
        &["reason"]
    )
    .context("Failed to register ENFORCEMENT_DROPS metric")?;

    let enforcement_allowed = register_counter_vec!(
        "agent_core_enforcement_allowed_total",
        "Total requests allowed by enforcement layer",
        &["outcome"]
    )
    .context("Failed to register ENFORCEMENT_ALLOWED metric")?;

    // Now set all OnceLocks - these should never fail since we're in a Once guard
    TASKS_TOTAL
        .set(tasks_total)
        .map_err(|_| anyhow::anyhow!("Failed to set TASKS_TOTAL"))?;
    TASK_DURATION
        .set(task_duration)
        .map_err(|_| anyhow::anyhow!("Failed to set TASK_DURATION"))?;
    TASK_TOKENS
        .set(task_tokens)
        .map_err(|_| anyhow::anyhow!("Failed to set TASK_TOKENS"))?;
    FSM_STATE_TRANSITIONS
        .set(fsm_state_transitions)
        .map_err(|_| anyhow::anyhow!("Failed to set FSM_STATE_TRANSITIONS"))?;
    FSM_CURRENT_STATE
        .set(fsm_current_state)
        .map_err(|_| anyhow::anyhow!("Failed to set FSM_CURRENT_STATE"))?;
    MEMORY_POOL_USED_BYTES
        .set(memory_pool_used_bytes)
        .map_err(|_| anyhow::anyhow!("Failed to set MEMORY_POOL_USED_BYTES"))?;
    MEMORY_POOL_TOTAL_BYTES
        .set(memory_pool_total_bytes)
        .map_err(|_| anyhow::anyhow!("Failed to set MEMORY_POOL_TOTAL_BYTES"))?;
    TOOL_EXECUTIONS
        .set(tool_executions)
        .map_err(|_| anyhow::anyhow!("Failed to set TOOL_EXECUTIONS"))?;
    TOOL_DURATION
        .set(tool_duration)
        .map_err(|_| anyhow::anyhow!("Failed to set TOOL_DURATION"))?;
    TOOL_SELECTION_DURATION
        .set(tool_selection_duration)
        .map_err(|_| anyhow::anyhow!("Failed to set TOOL_SELECTION_DURATION"))?;
    GRPC_REQUESTS
        .set(grpc_requests)
        .map_err(|_| anyhow::anyhow!("Failed to set GRPC_REQUESTS"))?;
    GRPC_REQUEST_DURATION
        .set(grpc_request_duration)
        .map_err(|_| anyhow::anyhow!("Failed to set GRPC_REQUEST_DURATION"))?;
    ENFORCEMENT_DROPS
        .set(enforcement_drops)
        .map_err(|_| anyhow::anyhow!("Failed to set ENFORCEMENT_DROPS"))?;
    ENFORCEMENT_ALLOWED
        .set(enforcement_allowed)
        .map_err(|_| anyhow::anyhow!("Failed to set ENFORCEMENT_ALLOWED"))?;

    // Set initial values for gauges
    if let Some(memory_total) = MEMORY_POOL_TOTAL_BYTES.get() {
        memory_total.set(512.0 * 1024.0 * 1024.0); // 512MB default
    }
    if let Some(memory_used) = MEMORY_POOL_USED_BYTES.get() {
        memory_used.set(0.0);
    }
    if let Some(fsm_state) = FSM_CURRENT_STATE.get() {
        fsm_state.set(0.0);
    }

    Ok(())
}

// Compatibility wrapper for old init_metrics() calls without Result
pub fn init_metrics_legacy() {
    if let Err(e) = init_metrics() {
        tracing::warn!("Failed to initialize metrics: {}", e);
    }
}

// Start metrics server with proper error handling
pub async fn start_metrics_server(port: u16) -> Result<()> {
    use std::net::SocketAddr;
    use tokio::net::TcpListener;

    // Initialize metrics before starting server
    init_metrics().context("Failed to initialize metrics")?;

    let addr = SocketAddr::from(([0, 0, 0, 0], port));
    let listener = TcpListener::bind(addr)
        .await
        .context("Failed to bind metrics server")?;

    tracing::info!("Metrics server listening on http://0.0.0.0:{}", port);

    loop {
        match listener.accept().await {
            Ok((mut stream, _)) => {
                tokio::spawn(async move {
                    let body = get_metrics();
                    let resp = format!(
                        "HTTP/1.1 200 OK\r\nContent-Type: text/plain; version=0.0.4\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                        body.len(),
                        body
                    );
                    if let Err(e) = stream.write_all(resp.as_bytes()).await {
                        tracing::error!("Metrics write error: {:?}", e);
                    }
                    let _ = stream.shutdown().await;
                });
            }
            Err(e) => {
                tracing::error!("Failed to accept connection: {:?}", e);
                // Continue accepting connections despite errors
            }
        }
    }
}
