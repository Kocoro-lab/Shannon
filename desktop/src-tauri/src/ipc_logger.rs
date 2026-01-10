use crate::ipc_events::{IpcEventEmitter, ServerLogPayload, LogBuffer, Component, LogLevel};
use anyhow::Result;
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::Mutex;
use tracing::{Event, Subscriber};
use tracing_subscriber::{layer::Context, Layer};

/// Real-time log streaming layer for tracing that emits logs via IPC to the frontend.
///
/// This layer captures all tracing events and forwards them to the frontend via IPC,
/// providing real-time visibility into the embedded API server's operation for debugging
/// and monitoring purposes.
///
/// # Examples
///
/// ```rust,no_run
/// use tracing_subscriber::{Registry, prelude::*};
/// use crate::ipc_logger::IpcLogLayer;
/// use crate::ipc_events::TauriEventEmitter;
/// use tauri::AppHandle;
///
/// // Setup with tracing subscriber
/// let app_handle = app_handle.clone();
/// let event_emitter = Arc::new(TauriEventEmitter::new(app_handle));
/// let ipc_layer = IpcLogLayer::new(event_emitter);
///
/// Registry::default()
///     .with(ipc_layer)
///     .init();
/// ```
#[derive(Debug)]
#[derive(Clone)]
pub struct IpcLogLayer {
    /// Event emitter for sending logs to frontend
    event_emitter: Arc<dyn IpcEventEmitter>,
    /// Circular buffer for storing recent log entries
    log_buffer: LogBuffer,
    /// Component mapping for structured logs
    component_map: Arc<Mutex<HashMap<String, Component>>>,
}

impl IpcLogLayer {
    /// Creates a new IPC log layer with the specified event emitter.
    ///
    /// # Arguments
    /// * `event_emitter` - The IPC event emitter to send logs to the frontend
    ///
    /// # Returns
    /// A new `IpcLogLayer` instance ready to be used with tracing-subscriber
    pub fn new(event_emitter: Arc<dyn IpcEventEmitter>) -> Self {
        Self {
            event_emitter,
            log_buffer: LogBuffer::with_capacity(1000), // Max 1000 entries as per specification
            component_map: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    /// Retrieves recent log entries from the circular buffer.
    ///
    /// # Arguments
    /// * `count` - Maximum number of recent entries to retrieve
    /// * `level_filter` - Optional log level filter
    ///
    /// # Returns
    /// A vector of recent log entries matching the criteria
    pub async fn get_recent_logs(&self, count: Option<usize>, level_filter: Option<LogLevel>) -> Vec<ServerLogPayload> {
        self.log_buffer.get_recent(count, level_filter).unwrap_or_else(|e| {
            eprintln!("Failed to get recent logs: {}", e);
            vec![]
        })
    }

    /// Registers a component mapping for structured logging.
    ///
    /// This allows the logger to map trace targets to specific components for better
    /// organization and filtering in the debug console.
    ///
    /// # Arguments
    /// * `target` - The tracing target string (e.g., "shannon_api::server")
    /// * `component` - The component this target should map to
    pub async fn register_component(&self, target: &str, component: Component) {
        let mut map = self.component_map.lock().await;
        map.insert(target.to_string(), component);
    }

    /// Converts a tracing Level to our LogLevel enum.
    fn convert_level(level: &tracing::Level) -> LogLevel {
        match *level {
            tracing::Level::ERROR => LogLevel::Error,
            tracing::Level::WARN => LogLevel::Warn,
            tracing::Level::INFO => LogLevel::Info,
            tracing::Level::DEBUG => LogLevel::Debug,
            tracing::Level::TRACE => LogLevel::Trace,
        }
    }

    /// Determines the component from the tracing target.
    #[cfg(test)]
    async fn determine_component(&self, target: &str) -> Component {
        let component_map = self.component_map.lock().await;

        // Check for exact match first
        if let Some(component) = component_map.get(target) {
            return *component;
        }

        // Fallback to pattern matching based on target
        if target.contains("embedded_api") || target.contains("ipc") {
            Component::EmbeddedApi
        } else if target.contains("http") || target.contains("server") {
            Component::HttpServer
        } else if target.contains("database") || target.contains("db") {
            Component::Database
        } else if target.contains("llm") {
            Component::LlmClient
        } else if target.contains("workflow") {
            Component::WorkflowEngine
        } else if target.contains("auth") {
            Component::Auth
        } else if target.contains("health") {
            Component::HealthCheck
        } else {
            Component::EmbeddedApi // Default fallback
        }
    }

    /// Extracts structured context from a tracing event.
    fn extract_context(event: &Event<'_>) -> Option<HashMap<String, String>> {
        let mut context = HashMap::new();

        // Visit all fields in the event
        let mut visitor = ContextVisitor::new(&mut context);
        event.record(&mut visitor);

        if context.is_empty() {
            None
        } else {
            Some(context)
        }
    }

    /// Extracts error information from the event context.
    fn extract_error(context: &Option<HashMap<String, String>>) -> Option<crate::ipc_events::ErrorInfo> {
        if let Some(ctx) = context {
            if let Some(error_msg) = ctx.get("error.message") {
                return Some(crate::ipc_events::ErrorInfo {
                    error_type: ctx.get("error.type").cloned().unwrap_or_else(|| "Unknown".to_string()),
                    message: error_msg.clone(),
                    stack: ctx.get("error.stack").cloned(),
                });
            }
        }
        None
    }

    /// Extracts duration from context if present.
    fn extract_duration(context: &Option<HashMap<String, String>>) -> Option<u64> {
        context.as_ref()
            .and_then(|ctx| ctx.get("duration_ms"))
            .and_then(|duration_str| duration_str.parse().ok())
    }
}

impl<S> Layer<S> for IpcLogLayer
where
    S: Subscriber + for<'a> tracing_subscriber::registry::LookupSpan<'a>,
{
    fn on_event(&self, event: &Event<'_>, _ctx: Context<'_, S>) {
        let level = Self::convert_level(event.metadata().level());
        let target = event.metadata().target();

        // Clone necessary data for async processing
        let event_emitter = self.event_emitter.clone();
        let log_buffer = self.log_buffer.clone();
        let component_map = self.component_map.clone();
        let target = target.to_string();

        // Extract message and context
        let message = format!("{}", event.metadata().name());
        let context = Self::extract_context(event);
        let error = Self::extract_error(&context);
        let duration_ms = Self::extract_duration(&context);

        // Spawn async task to handle the log emission
        tokio::spawn(async move {
            let component = {
                let map = component_map.lock().await;
                if let Some(comp) = map.get(&target) {
                    *comp
                } else {
                    // Determine component based on target pattern
                    if target.contains("embedded_api") || target.contains("ipc") {
                        Component::EmbeddedApi
                    } else if target.contains("http") || target.contains("server") {
                        Component::HttpServer
                    } else if target.contains("database") || target.contains("db") {
                        Component::Database
                    } else if target.contains("llm") {
                        Component::LlmClient
                    } else if target.contains("workflow") {
                        Component::WorkflowEngine
                    } else if target.contains("auth") {
                        Component::Auth
                    } else if target.contains("health") {
                        Component::HealthCheck
                    } else {
                        Component::EmbeddedApi
                    }
                }
            };

            let payload = ServerLogPayload {
                timestamp: chrono::Utc::now().to_rfc3339(),
                level,
                component,
                message,
                context,
                error,
                duration_ms,
            };

            // Add to buffer
            if let Err(e) = log_buffer.push(payload.clone()) {
                eprintln!("Failed to add log to buffer: {}", e);
            }

            // Emit to frontend (don't block on errors)
            if let Err(e) = event_emitter.emit_server_log(payload).await {
                eprintln!("Failed to emit log via IPC: {}", e);
            }
        });
    }
}

/// Visitor for extracting field values from tracing events.
struct ContextVisitor<'a> {
    context: &'a mut HashMap<String, String>,
}

impl<'a> ContextVisitor<'a> {
    fn new(context: &'a mut HashMap<String, String>) -> Self {
        Self { context }
    }
}

impl<'a> tracing::field::Visit for ContextVisitor<'a> {
    fn record_debug(&mut self, field: &tracing::field::Field, value: &dyn std::fmt::Debug) {
        self.context.insert(field.name().to_string(), format!("{:?}", value));
    }

    fn record_str(&mut self, field: &tracing::field::Field, value: &str) {
        self.context.insert(field.name().to_string(), value.to_string());
    }

    fn record_bool(&mut self, field: &tracing::field::Field, value: bool) {
        self.context.insert(field.name().to_string(), value.to_string());
    }

    fn record_i64(&mut self, field: &tracing::field::Field, value: i64) {
        self.context.insert(field.name().to_string(), value.to_string());
    }

    fn record_u64(&mut self, field: &tracing::field::Field, value: u64) {
        self.context.insert(field.name().to_string(), value.to_string());
    }

    fn record_f64(&mut self, field: &tracing::field::Field, value: f64) {
        self.context.insert(field.name().to_string(), value.to_string());
    }
}

/// Utility function to initialize the IPC logging system.
///
/// This is a convenience function that sets up the tracing subscriber with the IPC layer
/// and registers common component mappings.
///
/// # Arguments
/// * `event_emitter` - The IPC event emitter to use for log streaming
///
/// # Returns
/// The configured IPC log layer that can be used to retrieve recent logs
///
/// # Examples
///
/// ```rust,no_run
/// use crate::ipc_logger::setup_ipc_logging;
/// use crate::ipc_events::TauriEventEmitter;
///
/// let event_emitter = Arc::new(TauriEventEmitter::new(app_handle));
/// let ipc_layer = setup_ipc_logging(event_emitter).await?;
///
/// // Later, retrieve recent logs
/// let recent_logs = ipc_layer.get_recent_logs(Some(50), None).await;
/// ```
pub async fn setup_ipc_logging(event_emitter: Arc<dyn IpcEventEmitter>) -> Result<Arc<IpcLogLayer>> {
    use tracing_subscriber::{Registry, prelude::*};

    let ipc_layer = Arc::new(IpcLogLayer::new(event_emitter));

    // Register common component mappings
    ipc_layer.register_component("shannon_desktop::embedded_api", Component::EmbeddedApi).await;
    ipc_layer.register_component("shannon_desktop::ipc_events", Component::Ipc).await;
    ipc_layer.register_component("shannon_desktop::ipc_logger", Component::Ipc).await;
    ipc_layer.register_component("shannon_api::server", Component::HttpServer).await;
    ipc_layer.register_component("shannon_api::gateway", Component::HttpServer).await;
    ipc_layer.register_component("shannon_api::database", Component::Database).await;
    ipc_layer.register_component("shannon_api::llm_client", Component::LlmClient).await;
    ipc_layer.register_component("shannon_api::workflow", Component::WorkflowEngine).await;
    ipc_layer.register_component("shannon_api::auth", Component::Auth).await;
    ipc_layer.register_component("shannon_api::health", Component::HealthCheck).await;

    // Initialize tracing subscriber with the IPC layer
    Registry::default()
        .with((*ipc_layer).clone())
        .init();

    Ok(ipc_layer)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ipc_events::{IpcError, mock::MockEventEmitter};
    use std::sync::atomic::{AtomicUsize, Ordering};
    use tokio::sync::Mutex as AsyncMutex;
    use tracing::{info, error, debug};

    #[tokio::test]
    async fn test_ipc_log_layer_creation() {
        let mock_emitter = Arc::new(MockEventEmitter::new());
        let layer = IpcLogLayer::new(mock_emitter);

        // Verify layer is created successfully
        assert!(layer.log_buffer.len() == 0);
    }

    #[tokio::test]
    async fn test_component_registration() {
        let mock_emitter = Arc::new(MockEventEmitter::new());
        let layer = IpcLogLayer::new(mock_emitter);

        layer.register_component("test::module", Component::Database).await;

        let component = layer.determine_component("test::module").await;
        assert_eq!(component, Component::Database);
    }

    #[tokio::test]
    async fn test_component_pattern_matching() {
        let mock_emitter = Arc::new(MockEventEmitter::new());
        let layer = IpcLogLayer::new(mock_emitter);

        // Test various pattern matches
        assert_eq!(layer.determine_component("embedded_api::server").await, Component::EmbeddedApi);
        assert_eq!(layer.determine_component("http::handler").await, Component::HttpServer);
        assert_eq!(layer.determine_component("database::connection").await, Component::Database);
        assert_eq!(layer.determine_component("llm::client").await, Component::LlmClient);
        assert_eq!(layer.determine_component("workflow::engine").await, Component::WorkflowEngine);
        assert_eq!(layer.determine_component("auth::middleware").await, Component::Auth);
        assert_eq!(layer.determine_component("health::check").await, Component::HealthCheck);

        // Test fallback
        assert_eq!(layer.determine_component("unknown::module").await, Component::EmbeddedApi);
    }

    #[tokio::test]
    async fn test_level_conversion() {
        assert_eq!(IpcLogLayer::convert_level(&tracing::Level::ERROR), LogLevel::Error);
        assert_eq!(IpcLogLayer::convert_level(&tracing::Level::WARN), LogLevel::Warn);
        assert_eq!(IpcLogLayer::convert_level(&tracing::Level::INFO), LogLevel::Info);
        assert_eq!(IpcLogLayer::convert_level(&tracing::Level::DEBUG), LogLevel::Debug);
        assert_eq!(IpcLogLayer::convert_level(&tracing::Level::TRACE), LogLevel::Trace);
    }

    #[tokio::test]
    async fn test_get_recent_logs() {
        let mock_emitter = Arc::new(MockEventEmitter::new());
        let layer = IpcLogLayer::new(mock_emitter);

        // Initially empty
        let logs = layer.get_recent_logs(None, None).await;
        assert_eq!(logs.len(), 0);

        // Add some test logs directly to buffer
        let test_log = ServerLogPayload {
            timestamp: chrono::Utc::now().to_rfc3339(),
            level: LogLevel::Info,
            component: Component::EmbeddedApi,
            message: "Test message".to_string(),
            context: None,
            error: None,
            duration_ms: None,
        };

        layer.log_buffer.push(test_log.clone()).unwrap();

        let logs = layer.get_recent_logs(Some(1), None).await;
        assert_eq!(logs.len(), 1);
        assert_eq!(logs[0].message, "Test message");
    }

    #[tokio::test]
    async fn test_setup_ipc_logging() {
        let mock_emitter = Arc::new(MockEventEmitter::new());
        let result = setup_ipc_logging(mock_emitter).await;

        assert!(result.is_ok());
        let layer = result.unwrap();

        // Test that component mappings were registered
        assert_eq!(layer.determine_component("shannon_desktop::embedded_api").await, Component::EmbeddedApi);
        assert_eq!(layer.determine_component("shannon_api::server").await, Component::HttpServer);
        assert_eq!(layer.determine_component("shannon_api::database").await, Component::Database);
    }

    /// Test that the context visitor correctly extracts field values
    #[test]
    fn test_context_visitor() {
        let mut context = HashMap::new();
        let mut visitor = ContextVisitor::new(&mut context);

        // Test different field types
        let field = tracing::field::Field::new("test_str", tracing::callsite::Identifier(&()));
        visitor.record_str(&field, "test_value");

        let field = tracing::field::Field::new("test_bool", tracing::callsite::Identifier(&()));
        visitor.record_bool(&field, true);

        let field = tracing::field::Field::new("test_i64", tracing::callsite::Identifier(&()));
        visitor.record_i64(&field, 42);

        assert_eq!(context.get("test_str").unwrap(), "test_value");
        assert_eq!(context.get("test_bool").unwrap(), "true");
        assert_eq!(context.get("test_i64").unwrap(), "42");
    }

    #[test]
    fn test_extract_error() {
        let mut context = HashMap::new();
        context.insert("error.type".to_string(), "ConnectionError".to_string());
        context.insert("error.message".to_string(), "Failed to connect".to_string());
        context.insert("error.stack".to_string(), "at line 42".to_string());

        let error = IpcLogLayer::extract_error(&Some(context));

        assert!(error.is_some());
        let error = error.unwrap();
        assert_eq!(error.error_type, "ConnectionError");
        assert_eq!(error.message, "Failed to connect");
        assert_eq!(error.stack, Some("at line 42".to_string()));

        // Test with no error context
        let no_error = IpcLogLayer::extract_error(&None);
        assert!(no_error.is_none());
    }

    #[test]
    fn test_extract_duration() {
        let mut context = HashMap::new();
        context.insert("duration_ms".to_string(), "150".to_string());

        let duration = IpcLogLayer::extract_duration(&Some(context));
        assert_eq!(duration, Some(150));

        // Test with invalid duration
        let mut invalid_context = HashMap::new();
        invalid_context.insert("duration_ms".to_string(), "invalid".to_string());

        let invalid_duration = IpcLogLayer::extract_duration(&Some(invalid_context));
        assert_eq!(invalid_duration, None);

        // Test with no duration
        let no_duration = IpcLogLayer::extract_duration(&None);
        assert_eq!(no_duration, None);
    }
}