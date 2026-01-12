//! Integration tests for error recovery and retry logic.

use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::Arc;
use std::time::Duration;

use anyhow::{anyhow, Result};
use tempfile::NamedTempFile;
use tokio::time::sleep;

use shannon_api::database::workflow_store::{WorkflowStatus, WorkflowStore};
use shannon_api::workflow::embedded::{
    CircuitBreaker, CircuitBreakerState, RecoveryManager,
};
use shannon_api::workflow::embedded::recovery::{ErrorType, RetryConfig};

/// Helper to create test recovery manager.
async fn create_test_recovery() -> (
    RecoveryManager,
    Arc<WorkflowStore>,
    NamedTempFile,
    NamedTempFile,
) {
    let workflow_temp = NamedTempFile::new().unwrap();
    let event_temp = NamedTempFile::new().unwrap();

    let workflow_store = Arc::new(WorkflowStore::new(workflow_temp.path()).await.unwrap());
    let recovery = RecoveryManager::new(workflow_store.clone(), event_temp.path())
        .await
        .unwrap();

    (recovery, workflow_store, workflow_temp, event_temp)
}

#[tokio::test]
async fn test_error_classification_network() {
    let error = anyhow!("Connection refused");
    let error_type = ErrorType::classify(&error);
    assert_eq!(error_type, ErrorType::Network);
    assert!(error_type.is_retryable());
}

#[tokio::test]
async fn test_error_classification_timeout() {
    let error = anyhow!("Operation timed out");
    let error_type = ErrorType::classify(&error);
    assert_eq!(error_type, ErrorType::Timeout);
    assert!(error_type.is_retryable());
}

#[tokio::test]
async fn test_error_classification_rate_limit() {
    let error = anyhow!("Too many requests (429)");
    let error_type = ErrorType::classify(&error);
    assert_eq!(error_type, ErrorType::RateLimit);
    assert!(error_type.is_retryable());
}

#[tokio::test]
async fn test_error_classification_permanent() {
    let error = anyhow!("Unauthorized (401)");
    let error_type = ErrorType::classify(&error);
    assert_eq!(error_type, ErrorType::Permanent);
    assert!(!error_type.is_retryable());
}

#[tokio::test]
async fn test_retry_delay_exponential_backoff() {
    let error_type = ErrorType::Network;

    let delay0 = error_type.retry_delay(0);
    let delay1 = error_type.retry_delay(1);
    let delay2 = error_type.retry_delay(2);

    assert_eq!(delay0, Duration::from_secs(1)); // 2^0 * 1 = 1s
    assert_eq!(delay1, Duration::from_secs(2)); // 2^1 * 1 = 2s
    assert_eq!(delay2, Duration::from_secs(4)); // 2^2 * 1 = 4s
}

#[tokio::test]
async fn test_retry_delay_capped_at_max() {
    let error_type = ErrorType::Network;

    // High attempt number should cap at 60s
    let delay = error_type.retry_delay(10);
    assert_eq!(delay, Duration::from_secs(60));
}

#[tokio::test]
async fn test_retry_success_after_transient_failure() {
    let (recovery, _store, _wf_temp, _ev_temp) = create_test_recovery().await;

    let attempt = Arc::new(AtomicU32::new(0));
    let attempt_clone = attempt.clone();

    let result = recovery
        .with_retry(
            || {
                let attempt_clone = attempt_clone.clone();
                async move {
                    let current = attempt_clone.fetch_add(1, Ordering::SeqCst);
                    if current < 2 {
                        // Fail first 2 attempts
                        Err(anyhow!("Connection refused"))
                    } else {
                        // Succeed on 3rd attempt
                        Ok(42)
                    }
                }
            },
            "test-wf",
            "test_operation",
        )
        .await;

    assert!(result.is_ok());
    assert_eq!(result.unwrap(), 42);
    assert_eq!(attempt.load(Ordering::SeqCst), 3);
}

#[tokio::test]
async fn test_retry_fails_after_max_retries() {
    let (recovery, _store, _wf_temp, _ev_temp) = create_test_recovery().await;

    let attempt = Arc::new(AtomicU32::new(0));
    let attempt_clone = attempt.clone();

    let result = recovery
        .with_retry(
            || {
                let attempt_clone = attempt_clone.clone();
                async move {
                    attempt_clone.fetch_add(1, Ordering::SeqCst);
                    Err::<(), _>(anyhow!("Connection refused"))
                }
            },
            "test-wf",
            "test_operation",
        )
        .await;

    assert!(result.is_err());
    // Should try: initial + 3 retries = 4 total attempts
    assert_eq!(attempt.load(Ordering::SeqCst), 4);
}

#[tokio::test]
async fn test_retry_stops_on_permanent_error() {
    let (recovery, _store, _wf_temp, _ev_temp) = create_test_recovery().await;

    let attempt = Arc::new(AtomicU32::new(0));
    let attempt_clone = attempt.clone();

    let result = recovery
        .with_retry(
            || {
                let attempt_clone = attempt_clone.clone();
                async move {
                    attempt_clone.fetch_add(1, Ordering::SeqCst);
                    Err::<(), _>(anyhow!("Unauthorized (401)"))
                }
            },
            "test-wf",
            "test_operation",
        )
        .await;

    assert!(result.is_err());
    // Should only try once (no retries for permanent errors)
    assert_eq!(attempt.load(Ordering::SeqCst), 1);
}

#[tokio::test]
async fn test_circuit_breaker_opens_after_failures() {
    let (recovery, _store, _wf_temp, _ev_temp) = create_test_recovery().await;

    // Fail 5 times to open circuit
    for i in 0..5 {
        let _ = recovery
            .with_retry(
                || async { Err::<(), _>(anyhow!("Connection refused")) },
                "test-wf",
                &format!("test_op_{}", i),
            )
            .await;
    }

    // Circuit should be open now
    assert_eq!(
        recovery.circuit_breaker_state(),
        CircuitBreakerState::Open
    );
}

#[tokio::test]
async fn test_circuit_breaker_rejects_when_open() {
    let (recovery, _store, _wf_temp, _ev_temp) = create_test_recovery().await;

    // Open circuit
    for _ in 0..5 {
        let _ = recovery
            .with_retry(
                || async { Err::<(), _>(anyhow!("Connection refused")) },
                "test-wf",
                "test_operation",
            )
            .await;
    }

    // Next request should be rejected
    let result = recovery
        .with_retry(
            || async { Ok(42) },
            "test-wf",
            "test_operation",
        )
        .await;

    assert!(result.is_err());
    assert!(result.unwrap_err().to_string().contains("Circuit breaker"));
}

#[tokio::test]
async fn test_circuit_breaker_transitions_to_half_open() {
    // Create recovery with 0 cooldown for testing
    let workflow_temp = NamedTempFile::new().unwrap();
    let event_temp = NamedTempFile::new().unwrap();
    let workflow_store = Arc::new(WorkflowStore::new(workflow_temp.path()).await.unwrap());

    let retry_config = RetryConfig {
        max_retries: 3,
        base_delay_seconds: 0,
        max_delay_seconds: 1,
        checkpoint_on_retry: false,
    };

    let recovery = RecoveryManager::with_retry_config(
        workflow_store.clone(),
        event_temp.path(),
        retry_config,
    )
    .await
    .unwrap();

    // Open circuit
    for _ in 0..5 {
        let _ = recovery
            .with_retry(
                || async { Err::<(), _>(anyhow!("Connection refused")) },
                "test-wf",
                "test_operation",
            )
            .await;
    }

    assert_eq!(
        recovery.circuit_breaker_state(),
        CircuitBreakerState::Open
    );

    // Circuit breaker should allow testing after cooldown (instant in this test)
    sleep(Duration::from_millis(100)).await;

    // Attempt should transition to HalfOpen
    let _ = recovery
        .with_retry(
            || async { Ok(42) },
            "test-wf",
            "test_operation",
        )
        .await;

    // After successful request, circuit should be closed
    assert_eq!(
        recovery.circuit_breaker_state(),
        CircuitBreakerState::Closed
    );
}

#[tokio::test]
async fn test_circuit_breaker_reset() {
    let (recovery, _store, _wf_temp, _ev_temp) = create_test_recovery().await;

    // Open circuit
    for _ in 0..5 {
        let _ = recovery
            .with_retry(
                || async { Err::<(), _>(anyhow!("Connection refused")) },
                "test-wf",
                "test_operation",
            )
            .await;
    }

    assert_eq!(
        recovery.circuit_breaker_state(),
        CircuitBreakerState::Open
    );

    // Reset circuit
    recovery.reset_circuit_breaker();

    assert_eq!(
        recovery.circuit_breaker_state(),
        CircuitBreakerState::Closed
    );

    // Should allow requests now
    let result = recovery
        .with_retry(
            || async { Ok(42) },
            "test-wf",
            "test_operation",
        )
        .await;

    assert!(result.is_ok());
}

#[tokio::test]
async fn test_custom_retry_config() {
    let workflow_temp = NamedTempFile::new().unwrap();
    let event_temp = NamedTempFile::new().unwrap();
    let workflow_store = Arc::new(WorkflowStore::new(workflow_temp.path()).await.unwrap());

    let custom_config = RetryConfig {
        max_retries: 1, // Only 1 retry
        base_delay_seconds: 0,
        max_delay_seconds: 1,
        checkpoint_on_retry: false,
    };

    let recovery = RecoveryManager::with_retry_config(
        workflow_store,
        event_temp.path(),
        custom_config,
    )
    .await
    .unwrap();

    let attempt = Arc::new(AtomicU32::new(0));
    let attempt_clone = attempt.clone();

    let result = recovery
        .with_retry(
            || {
                let attempt_clone = attempt_clone.clone();
                async move {
                    attempt_clone.fetch_add(1, Ordering::SeqCst);
                    Err::<(), _>(anyhow!("Connection refused"))
                }
            },
            "test-wf",
            "test_operation",
        )
        .await;

    assert!(result.is_err());
    // Should try: initial + 1 retry = 2 total attempts
    assert_eq!(attempt.load(Ordering::SeqCst), 2);
}

#[tokio::test]
async fn test_recover_with_retry_protection() {
    let (recovery, store, _wf_temp, _ev_temp) = create_test_recovery().await;

    // Create a workflow
    store
        .create_workflow("wf-retry", "user-1", Some("sess-1"), "cot", "test")
        .await
        .unwrap();

    store
        .update_status("wf-retry", WorkflowStatus::Running)
        .await
        .unwrap();

    // Recovery should work with retry protection
    let recovered = recovery.recover_workflow("wf-retry").await.unwrap();

    assert_eq!(recovered.workflow.workflow_id, "wf-retry");
    assert_eq!(
        recovery.circuit_breaker_state(),
        CircuitBreakerState::Closed
    );
}

#[tokio::test]
async fn test_error_type_retry_delay_differences() {
    let network = ErrorType::Network;
    let timeout = ErrorType::Timeout;
    let rate_limit = ErrorType::RateLimit;

    // Network: base 1s
    assert_eq!(network.retry_delay(0), Duration::from_secs(1));

    // Timeout: base 2s
    assert_eq!(timeout.retry_delay(0), Duration::from_secs(2));

    // RateLimit: base 5s
    assert_eq!(rate_limit.retry_delay(0), Duration::from_secs(5));
}

#[tokio::test]
async fn test_concurrent_retry_operations() {
    let (recovery, _store, _wf_temp, _ev_temp) = create_test_recovery().await;

    let recovery = Arc::new(recovery);
    let mut handles = vec![];

    // Run 10 concurrent operations
    for i in 0..10 {
        let recovery_clone = recovery.clone();
        let handle = tokio::spawn(async move {
            let attempt = Arc::new(AtomicU32::new(0));
            let attempt_clone = attempt.clone();

            recovery_clone
                .with_retry(
                    || {
                        let attempt_clone = attempt_clone.clone();
                        async move {
                            let current = attempt_clone.fetch_add(1, Ordering::SeqCst);
                            if current < 1 {
                                Err(anyhow!("Connection refused"))
                            } else {
                                Ok(i)
                            }
                        }
                    },
                    &format!("wf-{}", i),
                    "concurrent_op",
                )
                .await
        });
        handles.push(handle);
    }

    // Wait for all operations
    let results: Vec<_> = futures::future::join_all(handles).await;

    // All should succeed
    for result in results {
        assert!(result.is_ok());
        assert!(result.unwrap().is_ok());
    }
}
