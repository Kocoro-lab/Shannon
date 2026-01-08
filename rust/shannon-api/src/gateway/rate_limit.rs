//! Rate limiting middleware using governor.

use std::sync::Arc;

use axum::{
    body::Body,
    extract::{Request, State},
    http::StatusCode,
    middleware::Next,
    response::{IntoResponse, Response},
    Json,
};
use governor::{
    clock::DefaultClock,
    middleware::NoOpMiddleware,
    state::{InMemoryState, NotKeyed},
    Quota, RateLimiter,
};
use parking_lot::Mutex;
use serde::Serialize;

use crate::AppState;

/// Rate limiter type alias.
pub type GlobalRateLimiter = RateLimiter<NotKeyed, InMemoryState, DefaultClock, NoOpMiddleware>;

/// Per-user rate limiter using a simple in-memory map.
pub struct UserRateLimiters {
    limiters: Mutex<std::collections::HashMap<String, Arc<GlobalRateLimiter>>>,
    quota: Quota,
}

impl UserRateLimiters {
    /// Create a new user rate limiter collection.
    pub fn new(requests_per_minute: u32, burst: u32) -> Self {
        let quota = Quota::per_minute(std::num::NonZeroU32::new(requests_per_minute).unwrap())
            .allow_burst(std::num::NonZeroU32::new(burst).unwrap());

        Self {
            limiters: Mutex::new(std::collections::HashMap::new()),
            quota,
        }
    }

    /// Get or create a rate limiter for a user.
    pub fn get_or_create(&self, user_id: &str) -> Arc<GlobalRateLimiter> {
        let mut limiters = self.limiters.lock();
        
        if let Some(limiter) = limiters.get(user_id) {
            return limiter.clone();
        }

        let limiter = Arc::new(RateLimiter::direct(self.quota));
        limiters.insert(user_id.to_string(), limiter.clone());
        limiter
    }
}

/// Rate limit error response.
#[derive(Debug, Clone, Serialize)]
pub struct RateLimitError {
    pub error: String,
    pub message: String,
    pub retry_after_secs: Option<u64>,
}

impl IntoResponse for RateLimitError {
    fn into_response(self) -> Response {
        let status = StatusCode::TOO_MANY_REQUESTS;
        let retry_after = self.retry_after_secs;
        let body = Json(self);
        
        let mut response = (status, body).into_response();
        
        if let Some(secs) = retry_after {
            response.headers_mut().insert(
                "Retry-After",
                secs.to_string().parse().unwrap(),
            );
        }
        
        response
    }
}

/// Global rate limiting middleware.
pub async fn global_rate_limit_middleware(
    State(_state): State<AppState>,
    req: Request<Body>,
    next: Next,
) -> Result<Response, RateLimitError> {
    // Create a global rate limiter if needed
    static GLOBAL_LIMITER: std::sync::OnceLock<GlobalRateLimiter> = std::sync::OnceLock::new();
    
    let limiter = GLOBAL_LIMITER.get_or_init(|| {
        let quota = Quota::per_second(std::num::NonZeroU32::new(1000).unwrap())
            .allow_burst(std::num::NonZeroU32::new(100).unwrap());
        RateLimiter::direct(quota)
    });

    match limiter.check() {
        Ok(_) => Ok(next.run(req).await),
        Err(not_until) => {
            // Use the earliest_possible method to get estimated wait time
            let wait_duration = not_until.wait_time_from(governor::clock::Clock::now(&governor::clock::DefaultClock::default()));
            Err(RateLimitError {
                error: "rate_limit_exceeded".to_string(),
                message: "Global rate limit exceeded. Please try again later.".to_string(),
                retry_after_secs: Some(wait_duration.as_secs()),
            })
        }
    }
}

/// Per-user rate limiting middleware.
pub async fn user_rate_limit_middleware(
    State(state): State<AppState>,
    req: Request<Body>,
    next: Next,
) -> Result<Response, RateLimitError> {
    // Get user ID from request extensions (set by auth middleware)
    let user_id = req
        .extensions()
        .get::<super::auth::AuthenticatedUser>()
        .map(|u| u.user_id.clone())
        .unwrap_or_else(|| "anonymous".to_string());

    // Get or create rate limiter for this user
    static USER_LIMITERS: std::sync::OnceLock<UserRateLimiters> = std::sync::OnceLock::new();
    
    let limiters = USER_LIMITERS.get_or_init(|| {
        UserRateLimiters::new(
            state.config.gateway.rate_limit_per_minute,
            state.config.gateway.rate_limit_burst,
        )
    });

    let limiter = limiters.get_or_create(&user_id);

    match limiter.check() {
        Ok(_) => Ok(next.run(req).await),
        Err(not_until) => {
            let wait_duration = not_until.wait_time_from(governor::clock::Clock::now(&governor::clock::DefaultClock::default()));
            Err(RateLimitError {
                error: "rate_limit_exceeded".to_string(),
                message: format!("Rate limit exceeded for user {}. Please try again later.", user_id),
                retry_after_secs: Some(wait_duration.as_secs()),
            })
        }
    }
}

/// Check rate limit without blocking (for Redis-based limiting).
pub async fn check_rate_limit_redis(
    user_id: &str,
    redis: &mut redis::aio::ConnectionManager,
    limit: u32,
    window_secs: u64,
) -> Result<(), RateLimitError> {
    use redis::AsyncCommands;

    let key = format!("rate_limit:{}:{}", user_id, chrono::Utc::now().timestamp() / window_secs as i64);
    
    let count: i64 = redis.incr(&key, 1).await.map_err(|e| {
        tracing::error!("Redis rate limit error: {}", e);
        RateLimitError {
            error: "internal_error".to_string(),
            message: "Failed to check rate limit".to_string(),
            retry_after_secs: None,
        }
    })?;

    // Set expiry on first increment
    if count == 1 {
        let _: () = redis.expire(&key, window_secs as i64).await.unwrap_or(());
    }

    if count as u32 > limit {
        return Err(RateLimitError {
            error: "rate_limit_exceeded".to_string(),
            message: format!("Rate limit exceeded. Maximum {} requests per {} seconds.", limit, window_secs),
            retry_after_secs: Some(window_secs),
        });
    }

    Ok(())
}
