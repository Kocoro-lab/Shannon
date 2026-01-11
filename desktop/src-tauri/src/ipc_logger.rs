use crate::ipc_events::{events, LogLevel, ServerLogPayload};
use chrono::Utc;
use std::sync::{Arc, Mutex};
use tauri::{AppHandle, Emitter};
use tracing::{Event, Subscriber};
use tracing_subscriber::layer::Context;
use tracing_subscriber::Layer;
use tracing_subscriber::prelude::*;

pub async fn setup_ipc_logging(app_handle: AppHandle) -> Result<IpcLogLayer, Box<dyn std::error::Error>> {
    let layer = IpcLogLayer::new(app_handle);
    // Use the `tracing_subscriber` registry to register the layer.
    // Note: If a global subscriber is already set (e.g. by tauri-plugin-log in lib.rs), this might fail or be ignored execution-wise.
    // However, since this is called in a separate thread/context, we attempt to init it.
    // If it fails, we assume logging is handled elsewhere or we attach differently.
    // For now, simpler is better: try init.
    // Actually, creating a new registry and trying to set it as global default:
    let registry = tracing_subscriber::registry().with(layer.clone());

    // We ignore the error if it's already set, but we still return the layer so implementation can use it manually if needed.
    let _ = registry.try_init();

    Ok(layer)
}


/// Custom Tracing Layer that emits logs to the Tauri frontend via IPC
#[derive(Clone, Debug)]
pub struct IpcLogLayer {
    app_handle: Option<AppHandle>,
    log_buffer: Arc<Mutex<Vec<ServerLogPayload>>>,
}

impl IpcLogLayer {
    pub fn new(app_handle: AppHandle) -> Self {
        Self {
            app_handle: Some(app_handle),
            log_buffer: Arc::new(Mutex::new(Vec::new())),
        }
    }

    /// Create a detached logger for background threads before app handle is ready
    /// Logs will be buffered until an app handle is provided (not implemented here for simplicity)
    pub fn new_detached() -> Self {
        Self {
            app_handle: None,
            log_buffer: Arc::new(Mutex::new(Vec::new())),
        }
    }

    /// Retrieve recent logs from the buffer
    pub async fn get_recent_logs(&self, count: Option<usize>, level_filter: Option<LogLevel>) -> Vec<ServerLogPayload> {
        let buffer = self.log_buffer.lock().unwrap();

        let iter = buffer.iter().rev();

        // Apply level filter if provided
        let filtered_iter = if let Some(filter) = level_filter {
            // Very basic filtering: identical level matching for now
            // In production, you'd want Level::WARN >= Level::INFO logic
            iter.filter(|log| log.level == filter).collect::<Vec<_>>().into_iter()
        } else {
            iter.collect::<Vec<_>>().into_iter()
        };

        // Take count
        let count = count.unwrap_or(100);
        filtered_iter.take(count).cloned().collect()
    }

    pub fn drain_buffer(&self) -> Vec<ServerLogPayload> {
        let mut buffer = self.log_buffer.lock().unwrap();
        buffer.drain(..).collect()
    }

    pub fn buffer_len(&self) -> usize {
        self.log_buffer.lock().unwrap().len()
    }

    pub fn emit_buffered_logs(&self) {
        let drained = self.drain_buffer();
        if let Some(handle) = &self.app_handle {
            for payload in drained {
                let _ = handle.emit(events::SERVER_LOG, payload);
            }
        }
    }

    fn emit_log(&self, payload: ServerLogPayload) {
        // 1. Add to buffer
        {
            let mut buffer = self.log_buffer.lock().unwrap();
            buffer.push(payload.clone());
            // Limit buffer size to prevent memory leaks
            if buffer.len() > 1000 {
                buffer.remove(0);
            }
        }

        // 2. Emit to frontend if handle exists
        if let Some(handle) = &self.app_handle {
             // We clone because emit is async in Tauri 2.0 but we are in a sync trace call
            let _ = handle.emit("server-log", payload);
        }
    }
}

impl<S> Layer<S> for IpcLogLayer
where
    S: Subscriber,
{
    fn on_event(&self, event: &Event<'_>, _ctx: Context<'_, S>) {
        let meta = event.metadata();

        // Map Tracing Level to our LogLevel
        let level = match *meta.level() {
            tracing::Level::ERROR => LogLevel::Error,
            tracing::Level::WARN => LogLevel::Warn,
            tracing::Level::INFO => LogLevel::Info,
            tracing::Level::DEBUG => LogLevel::Debug,
            tracing::Level::TRACE => LogLevel::Trace,
        };

        // Visitor to extract message and fields
        let mut visitor = LogVisitor::default();
        event.record(&mut visitor);

        let payload = ServerLogPayload {
            id: Some(uuid::Uuid::new_v4().to_string()),
            timestamp: Utc::now().to_rfc3339(),
            level,
            message: visitor.message,
            target: Some(meta.target().to_string()),
            error: None,
            component: crate::ipc_events::Component::EmbeddedApi, // Default to EmbeddedApi
            context: None,
            duration_ms: None,
        };

        self.emit_log(payload);
    }
}

// Internal Visitor to extract message from Tracing Event
#[derive(Default)]
struct LogVisitor {
    message: String,
}

impl tracing::field::Visit for LogVisitor {
    fn record_debug(&mut self, field: &tracing::field::Field, value: &dyn std::fmt::Debug) {
        if field.name() == "message" {
            self.message = format!("{:?}", value);
        } else {
            // Append other fields as key=value pair to message
            // Ideally this would go into structured data
            if !self.message.is_empty() {
                self.message.push_str(&format!(", {}={:?}", field.name(), value));
            } else {
                self.message.push_str(&format!("{}={:?}", field.name(), value));
            }
        }
    }

    // Override simple Display for Strings to avoid quotes
    fn record_str(&mut self, field: &tracing::field::Field, value: &str) {
         if field.name() == "message" {
            self.message = value.to_string();
        } else {
             if !self.message.is_empty() {
                self.message.push_str(&format!(", {}={}", field.name(), value));
            } else {
                self.message.push_str(&format!("{}={}", field.name(), value));
            }
        }
    }
}
