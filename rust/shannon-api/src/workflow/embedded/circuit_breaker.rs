//! Circuit breaker for protecting against cascading failures.
//!
//! Implements the circuit breaker pattern to prevent repeated attempts to
//! failing services. Tracks failure rates and transitions between Open,
//! Closed, and HalfOpen states.
//!
//! # States
//!
//! - **Closed**: Normal operation, requests pass through
//! - **Open**: Too many failures, all requests fail fast
//! - **HalfOpen**: Testing if service recovered, limited requests allowed
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::embedded::CircuitBreaker;
//!
//! let breaker = CircuitBreaker::new(5, 60); // 5 failures, 60s cooldown
//!
//! if breaker.is_request_allowed().await {
//!     match call_service().await {
//!         Ok(result) => {
//!             breaker.record_success().await;
//!             Ok(result)
//!         }
//!         Err(e) => {
//!             breaker.record_failure().await;
//!             Err(e)
//!         }
//!     }
//! } else {
//!     Err(anyhow::anyhow!("Circuit breaker open"))
//! }
//! ```

use std::sync::Arc;
use std::time::{Duration, Instant};

use parking_lot::RwLock;

/// Circuit breaker state.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CircuitBreakerState {
    /// Normal operation, all requests pass through.
    Closed,

    /// Too many failures, fail fast without attempting request.
    Open,

    /// Testing recovery, allow limited requests.
    HalfOpen,
}

impl CircuitBreakerState {
    /// Convert state to string representation.
    #[must_use]
    pub const fn as_str(&self) -> &'static str {
        match self {
            Self::Closed => "closed",
            Self::Open => "open",
            Self::HalfOpen => "half_open",
        }
    }
}

/// Inner state for circuit breaker.
#[derive(Debug)]
struct CircuitBreakerInner {
    /// Current state.
    state: CircuitBreakerState,

    /// Number of consecutive failures.
    failure_count: u32,

    /// Number of consecutive successes in HalfOpen state.
    success_count: u32,

    /// When circuit was opened (for cooldown calculation).
    opened_at: Option<Instant>,

    /// Last state transition time.
    last_transition: Instant,
}

/// Circuit breaker for protecting against cascading failures.
///
/// # Thread Safety
///
/// This type is thread-safe and can be shared across threads using [`Arc`].
#[derive(Clone)]
pub struct CircuitBreaker {
    /// Failure threshold before opening circuit.
    failure_threshold: u32,

    /// Cooldown period before transitioning to HalfOpen (seconds).
    cooldown_seconds: u64,

    /// Number of successful requests needed to close circuit from HalfOpen.
    success_threshold: u32,

    /// Inner mutable state.
    inner: Arc<RwLock<CircuitBreakerInner>>,
}

impl CircuitBreaker {
    /// Create a new circuit breaker.
    ///
    /// # Arguments
    ///
    /// * `failure_threshold` - Number of failures before opening circuit (default: 5)
    /// * `cooldown_seconds` - Cooldown period before testing recovery (default: 60)
    ///
    /// # Example
    ///
    /// ```rust,ignore
    /// let breaker = CircuitBreaker::new(5, 60);
    /// ```
    #[must_use]
    pub fn new(failure_threshold: u32, cooldown_seconds: u64) -> Self {
        Self {
            failure_threshold,
            cooldown_seconds,
            success_threshold: 3, // 3 successes needed to close from HalfOpen
            inner: Arc::new(RwLock::new(CircuitBreakerInner {
                state: CircuitBreakerState::Closed,
                failure_count: 0,
                success_count: 0,
                opened_at: None,
                last_transition: Instant::now(),
            })),
        }
    }

    /// Check if a request is allowed through the circuit breaker.
    ///
    /// # Returns
    ///
    /// - `true` if request should proceed (Closed or HalfOpen state)
    /// - `false` if request should be rejected (Open state)
    #[must_use]
    pub fn is_request_allowed(&self) -> bool {
        let mut inner = self.inner.write();

        match inner.state {
            CircuitBreakerState::Closed => true,
            CircuitBreakerState::Open => {
                // Check if cooldown period has elapsed
                if let Some(opened_at) = inner.opened_at {
                    let elapsed = opened_at.elapsed();
                    if elapsed >= Duration::from_secs(self.cooldown_seconds) {
                        // Transition to HalfOpen
                        tracing::info!(
                            cooldown_seconds = self.cooldown_seconds,
                            "Circuit breaker transitioning to HalfOpen"
                        );
                        inner.state = CircuitBreakerState::HalfOpen;
                        inner.success_count = 0;
                        inner.last_transition = Instant::now();
                        true
                    } else {
                        false
                    }
                } else {
                    false
                }
            }
            CircuitBreakerState::HalfOpen => true,
        }
    }

    /// Record a successful request.
    ///
    /// In HalfOpen state, successful requests count towards closing the circuit.
    pub fn record_success(&self) {
        let mut inner = self.inner.write();

        match inner.state {
            CircuitBreakerState::Closed => {
                // Reset failure count on success
                if inner.failure_count > 0 {
                    inner.failure_count = 0;
                }
            }
            CircuitBreakerState::HalfOpen => {
                inner.success_count += 1;
                tracing::debug!(
                    success_count = inner.success_count,
                    success_threshold = self.success_threshold,
                    "Circuit breaker recorded success in HalfOpen"
                );

                if inner.success_count >= self.success_threshold {
                    // Transition to Closed
                    tracing::info!("Circuit breaker closing after successful recovery test");
                    inner.state = CircuitBreakerState::Closed;
                    inner.failure_count = 0;
                    inner.success_count = 0;
                    inner.opened_at = None;
                    inner.last_transition = Instant::now();
                }
            }
            CircuitBreakerState::Open => {
                // Should not happen, but reset counts
                inner.failure_count = 0;
            }
        }
    }

    /// Record a failed request.
    ///
    /// In Closed state, failures count towards opening the circuit.
    /// In HalfOpen state, any failure immediately opens the circuit again.
    pub fn record_failure(&self) {
        let mut inner = self.inner.write();

        match inner.state {
            CircuitBreakerState::Closed => {
                inner.failure_count += 1;
                tracing::debug!(
                    failure_count = inner.failure_count,
                    failure_threshold = self.failure_threshold,
                    "Circuit breaker recorded failure"
                );

                if inner.failure_count >= self.failure_threshold {
                    // Transition to Open
                    tracing::warn!(
                        failure_count = inner.failure_count,
                        failure_threshold = self.failure_threshold,
                        cooldown_seconds = self.cooldown_seconds,
                        "Circuit breaker opening due to failures"
                    );
                    inner.state = CircuitBreakerState::Open;
                    inner.opened_at = Some(Instant::now());
                    inner.last_transition = Instant::now();
                }
            }
            CircuitBreakerState::HalfOpen => {
                // Any failure in HalfOpen immediately opens circuit
                tracing::warn!("Circuit breaker reopening due to failure in HalfOpen state");
                inner.state = CircuitBreakerState::Open;
                inner.failure_count = self.failure_threshold; // Max failures
                inner.success_count = 0;
                inner.opened_at = Some(Instant::now());
                inner.last_transition = Instant::now();
            }
            CircuitBreakerState::Open => {
                // Already open, nothing to do
            }
        }
    }

    /// Get current circuit breaker state.
    #[must_use]
    pub fn state(&self) -> CircuitBreakerState {
        self.inner.read().state
    }

    /// Get current failure count.
    #[must_use]
    pub fn failure_count(&self) -> u32 {
        self.inner.read().failure_count
    }

    /// Reset circuit breaker to closed state.
    ///
    /// This is typically used for testing or manual recovery.
    pub fn reset(&self) {
        let mut inner = self.inner.write();
        tracing::info!("Circuit breaker manually reset");
        inner.state = CircuitBreakerState::Closed;
        inner.failure_count = 0;
        inner.success_count = 0;
        inner.opened_at = None;
        inner.last_transition = Instant::now();
    }

    /// Get time until circuit breaker will transition to HalfOpen.
    ///
    /// Returns `None` if circuit is not Open or cooldown has elapsed.
    #[must_use]
    pub fn time_until_half_open(&self) -> Option<Duration> {
        let inner = self.inner.read();

        if inner.state != CircuitBreakerState::Open {
            return None;
        }

        if let Some(opened_at) = inner.opened_at {
            let elapsed = opened_at.elapsed();
            let cooldown = Duration::from_secs(self.cooldown_seconds);

            if elapsed < cooldown {
                Some(cooldown - elapsed)
            } else {
                Some(Duration::ZERO)
            }
        } else {
            None
        }
    }
}

impl std::fmt::Debug for CircuitBreaker {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let inner = self.inner.read();
        f.debug_struct("CircuitBreaker")
            .field("failure_threshold", &self.failure_threshold)
            .field("cooldown_seconds", &self.cooldown_seconds)
            .field("state", &inner.state)
            .field("failure_count", &inner.failure_count)
            .field("success_count", &inner.success_count)
            .finish()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_circuit_breaker_initial_state() {
        let breaker = CircuitBreaker::new(5, 60);
        assert_eq!(breaker.state(), CircuitBreakerState::Closed);
        assert_eq!(breaker.failure_count(), 0);
        assert!(breaker.is_request_allowed());
    }

    #[test]
    fn test_circuit_breaker_opens_after_threshold() {
        let breaker = CircuitBreaker::new(3, 60);

        // Record 3 failures to reach threshold
        breaker.record_failure();
        assert_eq!(breaker.state(), CircuitBreakerState::Closed);

        breaker.record_failure();
        assert_eq!(breaker.state(), CircuitBreakerState::Closed);

        breaker.record_failure();
        assert_eq!(breaker.state(), CircuitBreakerState::Open);
        assert!(!breaker.is_request_allowed());
    }

    #[test]
    fn test_circuit_breaker_success_resets_failures() {
        let breaker = CircuitBreaker::new(5, 60);

        breaker.record_failure();
        breaker.record_failure();
        assert_eq!(breaker.failure_count(), 2);

        breaker.record_success();
        assert_eq!(breaker.failure_count(), 0);
        assert_eq!(breaker.state(), CircuitBreakerState::Closed);
    }

    #[test]
    fn test_circuit_breaker_half_open_transition() {
        let breaker = CircuitBreaker::new(3, 0); // 0 cooldown for testing

        // Open circuit
        breaker.record_failure();
        breaker.record_failure();
        breaker.record_failure();
        assert_eq!(breaker.state(), CircuitBreakerState::Open);

        // Wait for cooldown (instant in this case)
        std::thread::sleep(Duration::from_millis(10));

        // Next request should transition to HalfOpen
        assert!(breaker.is_request_allowed());
        assert_eq!(breaker.state(), CircuitBreakerState::HalfOpen);
    }

    #[test]
    fn test_circuit_breaker_half_open_closes_after_successes() {
        let breaker = CircuitBreaker::new(3, 0);

        // Open circuit
        breaker.record_failure();
        breaker.record_failure();
        breaker.record_failure();

        // Transition to HalfOpen
        std::thread::sleep(Duration::from_millis(10));
        assert!(breaker.is_request_allowed());

        // Record 3 successes (success_threshold)
        breaker.record_success();
        assert_eq!(breaker.state(), CircuitBreakerState::HalfOpen);

        breaker.record_success();
        assert_eq!(breaker.state(), CircuitBreakerState::HalfOpen);

        breaker.record_success();
        assert_eq!(breaker.state(), CircuitBreakerState::Closed);
    }

    #[test]
    fn test_circuit_breaker_half_open_reopens_on_failure() {
        let breaker = CircuitBreaker::new(3, 0);

        // Open circuit
        breaker.record_failure();
        breaker.record_failure();
        breaker.record_failure();

        // Transition to HalfOpen
        std::thread::sleep(Duration::from_millis(10));
        assert!(breaker.is_request_allowed());
        assert_eq!(breaker.state(), CircuitBreakerState::HalfOpen);

        // Any failure in HalfOpen immediately opens circuit
        breaker.record_failure();
        assert_eq!(breaker.state(), CircuitBreakerState::Open);
    }

    #[test]
    fn test_circuit_breaker_reset() {
        let breaker = CircuitBreaker::new(3, 60);

        // Open circuit
        breaker.record_failure();
        breaker.record_failure();
        breaker.record_failure();
        assert_eq!(breaker.state(), CircuitBreakerState::Open);

        // Reset
        breaker.reset();
        assert_eq!(breaker.state(), CircuitBreakerState::Closed);
        assert_eq!(breaker.failure_count(), 0);
        assert!(breaker.is_request_allowed());
    }

    #[test]
    fn test_circuit_breaker_time_until_half_open() {
        let breaker = CircuitBreaker::new(3, 60);

        // Closed state - no time
        assert!(breaker.time_until_half_open().is_none());

        // Open circuit
        breaker.record_failure();
        breaker.record_failure();
        breaker.record_failure();

        // Should have time remaining
        let time = breaker.time_until_half_open();
        assert!(time.is_some());
        assert!(time.unwrap() <= Duration::from_secs(60));
    }

    #[test]
    fn test_circuit_breaker_state_as_str() {
        assert_eq!(CircuitBreakerState::Closed.as_str(), "closed");
        assert_eq!(CircuitBreakerState::Open.as_str(), "open");
        assert_eq!(CircuitBreakerState::HalfOpen.as_str(), "half_open");
    }
}
