//! Configuration error types with actionable user messages.
//!
//! This module provides rich error types that help users understand
//! what went wrong and how to fix configuration issues.

use std::fmt;

/// Configuration errors with detailed, actionable messages.
///
/// Each error variant includes enough context for users to understand
/// what went wrong and how to fix it.
#[derive(Debug, Clone)]
pub enum ConfigurationError {
    /// Invalid configuration value.
    Invalid {
        /// What is wrong.
        message: String,
        /// How to fix it.
        fix_hint: String,
    },
    /// Two settings that cannot be used together.
    Incompatible {
        /// First setting.
        setting1: String,
        /// Second setting.
        setting2: String,
        /// Why they're incompatible.
        reason: String,
    },
    /// A required configuration is missing.
    MissingRequired {
        /// The missing setting name.
        setting: String,
        /// What feature requires this setting.
        context: String,
        /// Environment variable to set.
        env_var: String,
    },
    /// A feature is not available in the current configuration.
    FeatureUnavailable {
        /// The unavailable feature.
        feature: String,
        /// Why it's unavailable.
        reason: String,
        /// What to use instead.
        alternative: String,
    },
    /// A service connection failed.
    ConnectionFailed {
        /// The service that failed.
        service: String,
        /// The endpoint that was tried.
        endpoint: String,
        /// The error message.
        error: String,
        /// Troubleshooting steps.
        troubleshooting: String,
    },
    /// Multiple errors occurred.
    Multiple(Vec<ConfigurationError>),
}

impl std::error::Error for ConfigurationError {}

impl fmt::Display for ConfigurationError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Invalid { message, fix_hint } => {
                write!(
                    f,
                    "Invalid configuration: {message}\n\nHow to fix: {fix_hint}"
                )
            }
            Self::Incompatible {
                setting1,
                setting2,
                reason,
            } => {
                write!(
                    f,
                    "Incompatible settings: {setting1} cannot be used with {setting2}\n\n\
                    Reason: {reason}"
                )
            }
            Self::MissingRequired {
                setting,
                context,
                env_var,
            } => {
                write!(
                    f,
                    "Missing required configuration: {setting}\n\n\
                    Required for: {context}\n\
                    Set via: {env_var}"
                )
            }
            Self::FeatureUnavailable {
                feature,
                reason,
                alternative,
            } => {
                write!(
                    f,
                    "Feature not available: {feature}\n\n\
                    Reason: {reason}\n\
                    Alternative: {alternative}"
                )
            }
            Self::ConnectionFailed {
                service,
                endpoint,
                error,
                troubleshooting,
            } => {
                write!(
                    f,
                    "Connection failed: {service}\n\n\
                    Endpoint: {endpoint}\n\
                    Error: {error}\n\n\
                    Check: {troubleshooting}"
                )
            }
            Self::Multiple(errors) => {
                writeln!(f, "Multiple configuration errors:")?;
                for (i, err) in errors.iter().enumerate() {
                    writeln!(f, "\n{}. {}", i + 1, err)?;
                }
                Ok(())
            }
        }
    }
}

impl ConfigurationError {
    /// Create an invalid configuration error.
    #[must_use]
    pub fn invalid(message: impl Into<String>, fix_hint: impl Into<String>) -> Self {
        Self::Invalid {
            message: message.into(),
            fix_hint: fix_hint.into(),
        }
    }

    /// Create an incompatible settings error.
    #[must_use]
    pub fn incompatible(
        setting1: impl Into<String>,
        setting2: impl Into<String>,
        reason: impl Into<String>,
    ) -> Self {
        Self::Incompatible {
            setting1: setting1.into(),
            setting2: setting2.into(),
            reason: reason.into(),
        }
    }

    /// Create a missing required configuration error.
    #[must_use]
    pub fn missing_required(
        setting: impl Into<String>,
        context: impl Into<String>,
        env_var: impl Into<String>,
    ) -> Self {
        Self::MissingRequired {
            setting: setting.into(),
            context: context.into(),
            env_var: env_var.into(),
        }
    }

    /// Create a feature unavailable error.
    #[must_use]
    pub fn feature_unavailable(
        feature: impl Into<String>,
        reason: impl Into<String>,
        alternative: impl Into<String>,
    ) -> Self {
        Self::FeatureUnavailable {
            feature: feature.into(),
            reason: reason.into(),
            alternative: alternative.into(),
        }
    }

    /// Create a connection failed error.
    #[must_use]
    pub fn connection_failed(
        service: impl Into<String>,
        endpoint: impl Into<String>,
        error: impl Into<String>,
        troubleshooting: impl Into<String>,
    ) -> Self {
        Self::ConnectionFailed {
            service: service.into(),
            endpoint: endpoint.into(),
            error: error.into(),
            troubleshooting: troubleshooting.into(),
        }
    }

    /// Create a multiple errors wrapper.
    #[must_use]
    pub fn multiple(errors: Vec<ConfigurationError>) -> Self {
        Self::Multiple(errors)
    }

    /// Check if this is a multiple errors wrapper.
    #[must_use]
    pub fn is_multiple(&self) -> bool {
        matches!(self, Self::Multiple(_))
    }

    /// Get the number of errors (1 for single errors, N for multiple).
    #[must_use]
    pub fn count(&self) -> usize {
        match self {
            Self::Multiple(errors) => errors.len(),
            _ => 1,
        }
    }
}

/// Result type for configuration validation.
pub type ConfigResult<T> = Result<T, ConfigurationError>;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_invalid_error_display() {
        let err = ConfigurationError::invalid(
            "SHANNON_MODE has invalid value 'foo'",
            "Set SHANNON_MODE to one of: embedded, cloud, hybrid, mesh, mesh-cloud",
        );
        let msg = err.to_string();
        assert!(msg.contains("Invalid configuration"));
        assert!(msg.contains("SHANNON_MODE"));
        assert!(msg.contains("How to fix"));
    }

    #[test]
    fn test_incompatible_error_display() {
        let err = ConfigurationError::incompatible(
            "SHANNON_MODE=embedded",
            "WORKFLOW_ENGINE=temporal",
            "Embedded mode requires the Durable workflow engine",
        );
        let msg = err.to_string();
        assert!(msg.contains("Incompatible"));
        assert!(msg.contains("embedded"));
        assert!(msg.contains("temporal"));
        assert!(msg.contains("Durable"));
    }

    #[test]
    fn test_missing_required_error_display() {
        let err = ConfigurationError::missing_required(
            "LLM API Key",
            "Making LLM requests",
            "ANTHROPIC_API_KEY or OPENAI_API_KEY",
        );
        let msg = err.to_string();
        assert!(msg.contains("Missing required"));
        assert!(msg.contains("LLM API Key"));
        assert!(msg.contains("ANTHROPIC_API_KEY"));
    }

    #[test]
    fn test_feature_unavailable_error_display() {
        let err = ConfigurationError::feature_unavailable(
            "Temporal workflow engine",
            "The 'grpc' feature is not enabled in this build",
            "Use WORKFLOW_ENGINE=durable for embedded mode",
        );
        let msg = err.to_string();
        assert!(msg.contains("not available"));
        assert!(msg.contains("Temporal"));
        assert!(msg.contains("Alternative"));
    }

    #[test]
    fn test_connection_failed_error_display() {
        let err = ConfigurationError::connection_failed(
            "PostgreSQL",
            "postgres://localhost:5432/shannon",
            "Connection refused",
            "Ensure PostgreSQL is running and accessible",
        );
        let msg = err.to_string();
        assert!(msg.contains("Connection failed"));
        assert!(msg.contains("PostgreSQL"));
        assert!(msg.contains("localhost:5432"));
    }

    #[test]
    fn test_multiple_errors_display() {
        let errors = vec![
            ConfigurationError::invalid("Error 1", "Fix 1"),
            ConfigurationError::invalid("Error 2", "Fix 2"),
        ];
        let err = ConfigurationError::multiple(errors);
        let msg = err.to_string();
        assert!(msg.contains("Multiple configuration errors"));
        assert!(msg.contains("1."));
        assert!(msg.contains("2."));
        assert_eq!(err.count(), 2);
    }
}
