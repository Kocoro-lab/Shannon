use opentelemetry::{global, trace::TracerProvider};
use opentelemetry_otlp::{SpanExporter, WithExportConfig};
use opentelemetry_sdk::{trace, Resource};
use opentelemetry_semantic_conventions::resource::{SERVICE_NAME, SERVICE_VERSION};
use std::env;
use std::time::Duration;
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

use crate::error::{AgentError, AgentResult};

/// Initialize OpenTelemetry tracing with OTLP exporter
pub fn init_tracing() -> AgentResult<()> {
    // Get configuration from environment
    let service_name =
        env::var("OTEL_SERVICE_NAME").unwrap_or_else(|_| "shannon-agent-core".to_string());
    let endpoint = env::var("OTEL_EXPORTER_OTLP_ENDPOINT")
        .unwrap_or_else(|_| "http://localhost:4317".to_string());
    let enabled = env::var("OTEL_ENABLED")
        .unwrap_or_else(|_| "true".to_string())
        .parse::<bool>()
        .unwrap_or(true);

    if !enabled {
        // Just use basic tracing without OpenTelemetry
        init_basic_tracing()?;
        return Ok(());
    }

    // Create OTLP exporter
    let exporter = SpanExporter::builder()
        .with_tonic()
        .with_endpoint(endpoint.clone())
        .with_timeout(Duration::from_secs(3))
        .build()
        .map_err(|e| AgentError::InternalError(format!("Failed to create exporter: {}", e)))?;

    // Build trace provider
    let tracer_provider = trace::TracerProvider::builder()
        .with_resource(Resource::new(vec![
            opentelemetry::KeyValue::new(SERVICE_NAME, service_name.clone()),
            opentelemetry::KeyValue::new(SERVICE_VERSION, env!("CARGO_PKG_VERSION")),
        ]))
        .with_batch_exporter(exporter, opentelemetry_sdk::runtime::Tokio)
        .build();

    // Set global tracer provider
    global::set_tracer_provider(tracer_provider.clone());

    // Create OpenTelemetry layer for tracing-subscriber
    let otel_layer =
        tracing_opentelemetry::layer().with_tracer(tracer_provider.tracer("shannon-agent-core"));

    // Initialize tracing subscriber with OpenTelemetry layer
    let subscriber = tracing_subscriber::registry()
        .with(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "shannon_agent_core=info,tower_http=info".into()),
        )
        .with(tracing_subscriber::fmt::layer())
        .with(otel_layer);

    subscriber.init();

    tracing::info!(
        service = service_name,
        endpoint = endpoint,
        "OpenTelemetry tracing initialized"
    );

    Ok(())
}

/// Initialize basic tracing without OpenTelemetry
fn init_basic_tracing() -> AgentResult<()> {
    let subscriber = tracing_subscriber::registry().with(
        tracing_subscriber::EnvFilter::try_from_default_env()
            .unwrap_or_else(|_| "shannon_agent_core=info,tower_http=info".into()),
    );

    if env::var("LOG_FORMAT").unwrap_or_default() == "json" {
        subscriber
            .with(tracing_subscriber::fmt::layer().json())
            .init();
    } else {
        subscriber.with(tracing_subscriber::fmt::layer()).init();
    }

    Ok(())
}

/// Shutdown OpenTelemetry tracing gracefully
pub fn shutdown_tracing() {
    global::shutdown_tracer_provider();
}

/// Extract trace context from HTTP headers for cross-service tracing
pub fn extract_trace_context(headers: &http::HeaderMap) -> opentelemetry::Context {
    use opentelemetry::propagation::{Extractor, TextMapPropagator};
    use opentelemetry_sdk::propagation::TraceContextPropagator;

    struct HeaderExtractor<'a>(&'a http::HeaderMap);

    impl<'a> Extractor for HeaderExtractor<'a> {
        fn get(&self, key: &str) -> Option<&str> {
            self.0.get(key).and_then(|v| v.to_str().ok())
        }

        fn keys(&self) -> Vec<&str> {
            self.0.keys().map(|k| k.as_str()).collect()
        }
    }

    let propagator = TraceContextPropagator::new();
    propagator.extract(&HeaderExtractor(headers))
}

/// Inject trace context into HTTP headers for cross-service tracing
/// Uses the provided context explicitly
pub fn inject_trace_context(context: &opentelemetry::Context, headers: &mut http::HeaderMap) {
    use opentelemetry::propagation::{Injector, TextMapPropagator};
    use opentelemetry_sdk::propagation::TraceContextPropagator;

    struct HeaderInjector<'a>(&'a mut http::HeaderMap);

    impl<'a> Injector for HeaderInjector<'a> {
        fn set(&mut self, key: &str, value: String) {
            if let Ok(header_name) = http::header::HeaderName::from_bytes(key.as_bytes()) {
                if let Ok(header_value) = http::header::HeaderValue::from_str(&value) {
                    self.0.insert(header_name, header_value);
                }
            }
        }
    }

    let propagator = TraceContextPropagator::new();
    propagator.inject_context(context, &mut HeaderInjector(headers));
}

/// Inject current active span's trace context into HTTP headers
/// This is the preferred method as it automatically uses the active span
pub fn inject_current_trace_context(headers: &mut http::HeaderMap) {
    use opentelemetry::propagation::{Injector, TextMapPropagator};
    use opentelemetry_sdk::propagation::TraceContextPropagator;
    use tracing_opentelemetry::OpenTelemetrySpanExt;

    struct HeaderInjector<'a>(&'a mut http::HeaderMap);

    impl<'a> Injector for HeaderInjector<'a> {
        fn set(&mut self, key: &str, value: String) {
            if let Ok(header_name) = http::header::HeaderName::from_bytes(key.as_bytes()) {
                if let Ok(header_value) = http::header::HeaderValue::from_str(&value) {
                    self.0.insert(header_name, header_value);
                }
            }
        }
    }

    // Get the current span's context
    let current_span = tracing::Span::current();
    let context = current_span.context();

    let propagator = TraceContextPropagator::new();
    propagator.inject_context(&context, &mut HeaderInjector(headers));
}

/// Get the current trace ID from the active span
pub fn get_current_trace_id() -> Option<String> {
    use opentelemetry::trace::TraceContextExt;
    use tracing_opentelemetry::OpenTelemetrySpanExt;

    let current_span = tracing::Span::current();
    let context = current_span.context();
    let span = context.span();
    let span_context = span.span_context();

    if span_context.is_valid() {
        Some(format!("{:032x}", span_context.trace_id()))
    } else {
        None
    }
}

/// Create a span that's properly linked to the current trace context
pub fn create_linked_span(name: &str) -> tracing::Span {
    use tracing_opentelemetry::OpenTelemetrySpanExt;

    let current_span = tracing::Span::current();
    let parent_context = current_span.context();

    // Create a new span with the parent context
    let span = tracing::info_span!("span", name = name);
    span.set_parent(parent_context);
    span
}

/// Create a new span with automatic context propagation
#[macro_export]
macro_rules! span {
    ($name:expr) => {
        tracing::info_span!($name)
    };
    ($name:expr, $($field:tt)*) => {
        tracing::info_span!($name, $($field)*)
    };
}

/// Instrument an async function with a span
#[macro_export]
macro_rules! instrument_async {
    ($name:expr, $future:expr) => {
        async move {
            let _span = $crate::span!($name).entered();
            $future.await
        }
    };
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_init_basic_tracing() {
        env::set_var("OTEL_ENABLED", "false");
        let result = init_tracing();
        assert!(result.is_ok());
        env::remove_var("OTEL_ENABLED");
    }

    #[test]
    fn test_header_extraction() {
        let mut headers = http::HeaderMap::new();
        headers.insert(
            "traceparent",
            "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
                .parse()
                .unwrap(),
        );

        let _context = extract_trace_context(&headers);
        // Context extracted; no strict assertion necessary in this smoke test.
    }
}
