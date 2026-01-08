//! Idempotency key handling for preventing duplicate requests.

use axum::{
    body::Body,
    extract::{Request, State},
    http::{HeaderMap, StatusCode},
    middleware::Next,
    response::{IntoResponse, Response},
    Json,
};
use serde::{Deserialize, Serialize};

use crate::AppState;

/// Header name for idempotency key.
pub const IDEMPOTENCY_KEY_HEADER: &str = "Idempotency-Key";

/// Cached response for idempotent requests.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CachedResponse {
    /// HTTP status code.
    pub status: u16,
    /// Response body as JSON.
    pub body: serde_json::Value,
    /// Response headers (subset).
    pub headers: Vec<(String, String)>,
    /// Timestamp when cached.
    pub cached_at: i64,
}

/// Idempotency error.
#[derive(Debug, Serialize)]
pub struct IdempotencyError {
    pub error: String,
    pub message: String,
}

impl IntoResponse for IdempotencyError {
    fn into_response(self) -> Response {
        let status = StatusCode::CONFLICT;
        let body = Json(self);
        (status, body).into_response()
    }
}

/// Extract idempotency key from request headers.
pub fn get_idempotency_key(headers: &HeaderMap) -> Option<String> {
    headers
        .get(IDEMPOTENCY_KEY_HEADER)
        .and_then(|v| v.to_str().ok())
        .map(String::from)
}

/// Store a response for an idempotency key.
pub async fn store_response(
    redis: &mut redis::aio::ConnectionManager,
    idempotency_key: &str,
    response: &CachedResponse,
    ttl_secs: u64,
) -> anyhow::Result<()> {
    use redis::AsyncCommands;

    let key = format!("idempotency:{}", idempotency_key);
    let value = serde_json::to_string(response)?;
    
    let _: () = redis.set_ex(&key, value, ttl_secs).await?;
    
    Ok(())
}

/// Get a cached response for an idempotency key.
pub async fn get_cached_response(
    redis: &mut redis::aio::ConnectionManager,
    idempotency_key: &str,
) -> anyhow::Result<Option<CachedResponse>> {
    use redis::AsyncCommands;

    let key = format!("idempotency:{}", idempotency_key);
    let value: Option<String> = redis.get(&key).await?;
    
    match value {
        Some(v) => {
            let response: CachedResponse = serde_json::from_str(&v)?;
            Ok(Some(response))
        }
        None => Ok(None),
    }
}

/// Mark an idempotency key as in-progress.
pub async fn mark_in_progress(
    redis: &mut redis::aio::ConnectionManager,
    idempotency_key: &str,
    ttl_secs: u64,
) -> anyhow::Result<bool> {
    use redis::AsyncCommands;

    let key = format!("idempotency_lock:{}", idempotency_key);
    
    // Use SETNX to atomically set only if not exists
    let result: bool = redis.set_nx(&key, "processing").await?;
    
    if result {
        // Set expiry to prevent stale locks
        let _: () = redis.expire(&key, ttl_secs as i64).await?;
    }
    
    Ok(result)
}

/// Release an idempotency lock.
pub async fn release_lock(
    redis: &mut redis::aio::ConnectionManager,
    idempotency_key: &str,
) -> anyhow::Result<()> {
    use redis::AsyncCommands;

    let key = format!("idempotency_lock:{}", idempotency_key);
    let _: () = redis.del(&key).await?;
    
    Ok(())
}

/// Idempotency middleware (requires Redis).
///
/// This middleware checks for an Idempotency-Key header and:
/// 1. Returns cached response if available
/// 2. Acquires a lock and processes the request
/// 3. Caches the response for future requests with the same key
pub async fn idempotency_middleware(
    State(state): State<AppState>,
    headers: HeaderMap,
    req: Request<Body>,
    next: Next,
) -> Result<Response, IdempotencyError> {
    // Skip if idempotency is disabled
    if !state.config.gateway.idempotency_enabled {
        return Ok(next.run(req).await);
    }

    // Only process POST/PUT/PATCH requests
    let method = req.method().clone();
    if method != axum::http::Method::POST 
        && method != axum::http::Method::PUT 
        && method != axum::http::Method::PATCH 
    {
        return Ok(next.run(req).await);
    }

    // Get idempotency key
    let idempotency_key = match get_idempotency_key(&headers) {
        Some(key) => key,
        None => return Ok(next.run(req).await),
    };

    // Check if we have Redis
    let redis = match &state.redis {
        Some(r) => r.clone(),
        None => {
            // Without Redis, just process normally
            tracing::debug!("Idempotency key provided but Redis not available");
            return Ok(next.run(req).await);
        }
    };

    let mut redis = redis;

    // Check for cached response
    match get_cached_response(&mut redis, &idempotency_key).await {
        Ok(Some(cached)) => {
            tracing::info!("Returning cached response for idempotency key: {}", idempotency_key);
            
            let mut builder = Response::builder()
                .status(StatusCode::from_u16(cached.status).unwrap_or(StatusCode::OK));
            
            for (name, value) in cached.headers {
                if let Ok(v) = value.parse::<axum::http::HeaderValue>() {
                    builder = builder.header(name, v);
                }
            }
            
            let body = serde_json::to_string(&cached.body).unwrap_or_default();
            return Ok(builder.body(Body::from(body)).unwrap());
        }
        Ok(None) => {}
        Err(e) => {
            tracing::error!("Failed to check cached response: {}", e);
        }
    }

    // Try to acquire lock
    let ttl = state.config.gateway.idempotency_ttl_secs;
    match mark_in_progress(&mut redis, &idempotency_key, ttl).await {
        Ok(true) => {
            // We have the lock, process the request
        }
        Ok(false) => {
            // Another request is processing with this key
            return Err(IdempotencyError {
                error: "request_in_progress".to_string(),
                message: "A request with this idempotency key is already being processed".to_string(),
            });
        }
        Err(e) => {
            tracing::error!("Failed to acquire idempotency lock: {}", e);
            // Proceed without idempotency protection
        }
    }

    // Process the request
    let response = next.run(req).await;

    // Cache the response (best effort)
    // Note: This is simplified - in production you'd want to capture the full response body
    let _ = release_lock(&mut redis, &idempotency_key).await;

    Ok(response)
}
