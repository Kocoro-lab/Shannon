//! Structured logging utilities for Shannon API.
//!
//! This module provides operation timing and structured logging helpers
//! for tracking initialization phases, operation performance, and detailed
//! context during server startup and runtime.

use std::time::Instant;

/// Operation timer for measuring and logging execution duration.
///
/// This timer automatically logs the operation start and provides methods
/// to log the completion with context (success or error).
///
/// # Examples
///
/// ```rust,ignore
/// use shannon_api::logging::OpTimer;
///
/// let timer = OpTimer::new("database", "connection");
/// // ... perform operation ...
/// timer.finish(); // Logs duration
/// ```
#[derive(Debug)]
pub struct OpTimer {
    /// Component being timed (e.g., "orchestrator", "database").
    component: String,
    /// Operation being performed (e.g., "initialization", "connection").
    operation: String,
    /// Start time of the operation.
    start: Instant,
}

impl OpTimer {
    /// Creates a new operation timer and logs the start.
    ///
    /// # Parameters
    ///
    /// - `component`: The component being timed (e.g., "database")
    /// - `operation`: The operation being performed (e.g., "initialization")
    ///
    /// # Examples
    ///
    /// ```rust,ignore
    /// let timer = OpTimer::new("redis", "connection");
    /// ```
    #[must_use]
    pub fn new(component: impl Into<String>, operation: impl Into<String>) -> Self {
        let component = component.into();
        let operation = operation.into();

        tracing::debug!(
            component = %component,
            operation = %operation,
            "Operation started"
        );

        Self {
            component,
            operation,
            start: Instant::now(),
        }
    }

    /// Finishes the timer and logs the duration.
    ///
    /// # Examples
    ///
    /// ```rust,ignore
    /// let timer = OpTimer::new("llm", "initialization");
    /// // ... perform operation ...
    /// timer.finish();
    /// ```
    pub fn finish(self) {
        let duration_ms = self.start.elapsed().as_millis();

        tracing::info!(
            component = %self.component,
            operation = %self.operation,
            duration_ms = duration_ms,
            "Operation completed"
        );
    }

    /// Finishes the timer with result-aware logging.
    ///
    /// Logs success or error based on the result, including error context
    /// when the operation fails.
    ///
    /// # Parameters
    ///
    /// - `result`: The result of the operation
    ///
    /// # Examples
    ///
    /// ```rust,ignore
    /// let timer = OpTimer::new("database", "query");
    /// let result = database.query("SELECT * FROM users").await;
    /// timer.finish_with_result(result.as_ref().map(|_| ()));
    /// ```
    pub fn finish_with_result<T, E: std::fmt::Display>(self, result: Result<&T, &E>) {
        let duration_ms = self.start.elapsed().as_millis();

        match result {
            Ok(_) => {
                tracing::info!(
                    component = %self.component,
                    operation = %self.operation,
                    duration_ms = duration_ms,
                    "Operation completed successfully"
                );
            }
            Err(e) => {
                tracing::error!(
                    component = %self.component,
                    operation = %self.operation,
                    duration_ms = duration_ms,
                    error = %e,
                    "Operation failed"
                );
            }
        }
    }
}

/// Macro for logging initialization steps with consistent formatting.
///
/// # Examples
///
/// ```rust,ignore
/// log_init_step!(1, 8, "LLM Settings", "OpenAI (gpt-4)");
/// log_init_step!(2, 8, "Tool Registry", "12 tools loaded");
/// ```
#[macro_export]
macro_rules! log_init_step {
    ($step:expr, $total:expr, $name:expr, $detail:expr) => {
        tracing::info!(
            step = $step,
            total = $total,
            "[{}/{}] {} - {}",
            $step,
            $total,
            $name,
            $detail
        );
    };
    ($step:expr, $total:expr, $name:expr) => {
        tracing::info!(
            step = $step,
            total = $total,
            "[{}/{}] {}",
            $step,
            $total,
            $name
        );
    };
}

/// Macro for logging warnings during initialization.
///
/// # Examples
///
/// ```rust,ignore
/// log_init_warning!("No OpenAI API key configured");
/// ```
#[macro_export]
macro_rules! log_init_warning {
    ($msg:expr) => {
        tracing::warn!("⚠️  {}", $msg);
    };
    ($msg:expr, $($arg:tt)*) => {
        tracing::warn!("⚠️  {}", format!($msg, $($arg)*));
    };
}

/// Macro for logging successful completion of major phases.
///
/// # Examples
///
/// ```rust,ignore
/// log_success!("Shannon API server created successfully");
/// ```
#[macro_export]
macro_rules! log_success {
    ($msg:expr) => {
        tracing::info!("✅ {}", $msg);
    };
    ($msg:expr, $($arg:tt)*) => {
        tracing::info!("✅ {}", format!($msg, $($arg)*));
    };
}

/// Macro for logging startup banners.
///
/// # Examples
///
/// ```rust,ignore
/// log_banner!("Shannon API v1.0.0", "Starting in embedded mode");
/// ```
#[macro_export]
macro_rules! log_banner {
    ($title:expr) => {
        tracing::info!("");
        tracing::info!("═══════════════════════════════════════════════════");
        tracing::info!("  {}", $title);
        tracing::info!("═══════════════════════════════════════════════════");
        tracing::info!("");
    };
    ($title:expr, $subtitle:expr) => {
        tracing::info!("");
        tracing::info!("═══════════════════════════════════════════════════");
        tracing::info!("  {}", $title);
        tracing::info!("  {}", $subtitle);
        tracing::info!("═══════════════════════════════════════════════════");
        tracing::info!("");
    };
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_op_timer_creation() {
        let timer = OpTimer::new("test_component", "test_operation");
        assert_eq!(timer.component, "test_component");
        assert_eq!(timer.operation, "test_operation");
    }

    #[test]
    fn test_op_timer_finish() {
        let timer = OpTimer::new("test", "operation");
        timer.finish();
        // Timer should complete without panicking
    }

    #[test]
    fn test_op_timer_finish_with_result_ok() {
        let timer = OpTimer::new("test", "operation");
        let result: Result<i32, String> = Ok(42);
        timer.finish_with_result(result.as_ref().map(|_| ()));
    }

    #[test]
    fn test_op_timer_finish_with_result_err() {
        let timer = OpTimer::new("test", "operation");
        let result: Result<i32, String> = Err("test error".to_string());
        timer.finish_with_result(result.as_ref().map(|_| ()));
    }
}
