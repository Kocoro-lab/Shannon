//! Authentication middleware for JWT and API key validation.

use axum::{
    body::Body,
    extract::{Request, State},
    http::{header::AUTHORIZATION, StatusCode},
    middleware::Next,
    response::{IntoResponse, Response},
    Json,
};
use serde::{Deserialize, Serialize};

#[cfg(feature = "gateway")]
use jsonwebtoken::{decode, encode, DecodingKey, EncodingKey, Header, Validation};

#[cfg(feature = "embedded")]
use surrealdb::engine::local::Db;

use crate::config::AppConfig;

/// Authentication error response.
#[derive(Debug, Serialize)]
pub struct AuthError {
    pub error: String,
    pub message: String,
}

impl IntoResponse for AuthError {
    fn into_response(self) -> Response {
        let status = StatusCode::UNAUTHORIZED;
        let body = Json(self);
        (status, body).into_response()
    }
}

/// JWT claims structure.
#[derive(Debug, Serialize, Deserialize)]
pub struct Claims {
    /// Subject (user ID).
    pub sub: String,
    /// Expiration time (Unix timestamp).
    pub exp: i64,
    /// Issued at (Unix timestamp).
    pub iat: i64,
    /// Optional tenant ID for multi-tenancy.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub tenant_id: Option<String>,
    /// Optional roles.
    #[serde(default)]
    pub roles: Vec<String>,
}

/// Authenticated user information extracted from the request.
#[derive(Debug, Clone)]
pub struct AuthenticatedUser {
    /// User ID (from JWT subject or API key owner).
    pub user_id: String,
    /// Authentication method used.
    pub auth_method: AuthMethod,
    /// Optional tenant ID.
    pub tenant_id: Option<String>,
    /// User roles.
    pub roles: Vec<String>,
}

/// Authentication method.
#[derive(Debug, Clone, PartialEq)]
pub enum AuthMethod {
    /// JWT token authentication.
    Jwt,
    /// API key authentication.
    ApiKey,
    /// No authentication (public endpoint).
    None,
}

/// Generate a JWT token.
#[cfg(feature = "gateway")]
pub fn generate_jwt(
    user_id: &str,
    tenant_id: Option<&str>,
    roles: Vec<String>,
    secret: &str,
    expiry_secs: u64,
) -> anyhow::Result<String> {
    let now = chrono::Utc::now().timestamp();
    let claims = Claims {
        sub: user_id.to_string(),
        exp: now + expiry_secs as i64,
        iat: now,
        tenant_id: tenant_id.map(String::from),
        roles,
    };

    let token = encode(
        &Header::default(),
        &claims,
        &EncodingKey::from_secret(secret.as_bytes()),
    )?;

    Ok(token)
}

/// Validate a JWT token.
#[cfg(feature = "gateway")]
pub fn validate_jwt(token: &str, secret: &str) -> anyhow::Result<Claims> {
    let token_data = decode::<Claims>(
        token,
        &DecodingKey::from_secret(secret.as_bytes()),
        &Validation::default(),
    )?;

    Ok(token_data.claims)
}

/// Stub for non-gateway builds.
#[cfg(not(feature = "gateway"))]
pub fn generate_jwt(
    _user_id: &str,
    _tenant_id: Option<&str>,
    _roles: Vec<String>,
    _secret: &str,
    _expiry_secs: u64,
) -> anyhow::Result<String> {
    Err(anyhow::anyhow!("JWT support requires 'gateway' feature"))
}

#[cfg(not(feature = "gateway"))]
pub fn validate_jwt(_token: &str, _secret: &str) -> anyhow::Result<Claims> {
    Err(anyhow::anyhow!("JWT support requires 'gateway' feature"))
}

/// Validate an API key.
///
/// Returns the user ID associated with the API key if valid.
pub async fn validate_api_key(
    api_key: &str,
    _config: &AppConfig,
    redis: Option<&redis::aio::ConnectionManager>,
    #[cfg(feature = "embedded")]
    surreal: Option<&surrealdb::Surreal<Db>>,
) -> Result<AuthenticatedUser, AuthError> {
    // First check Redis cache if available
    if let Some(_redis) = redis {
        // TODO: Implement Redis-based API key validation
        // For now, fall through to direct validation
    }

    #[cfg(feature = "embedded")]
    if let Some(db) = surreal {
         // Query user by API key and active status
         // We select specific fields to construct the AuthenticatedUser
         // Assuming table 'users' with fields: id, tenant_id, roles (array), api_key
         let sql = "SELECT id, tenant_id, roles FROM users WHERE api_key = $key LIMIT 1";
         
         let mut response = db.query(sql)
            .bind(("key", api_key.to_string()))
            .await
            .map_err(|e| AuthError {
                error: "db_error".to_string(),
                message: format!("Database error: {}", e),
            })?;
            
         // Define a struct for deserialization
         #[derive(Deserialize)]
         struct UserRecord {
             id: surrealdb::sql::Thing,
             tenant_id: Option<String>,
             roles: Option<Vec<String>>,
         }
         
         let users: Vec<UserRecord> = response.take(0).map_err(|e| AuthError {
             error: "db_error".to_string(),
             message: format!("Failed to parse user record: {}", e),
         })?;
         
         if let Some(user) = users.into_iter().next() {
             return Ok(AuthenticatedUser {
                 user_id: user.id.to_string(),
                 auth_method: AuthMethod::ApiKey,
                 tenant_id: user.tenant_id,
                 roles: user.roles.unwrap_or_else(|| vec!["user".to_string()]),
             });
         }
    }

    // For development, accept any key that starts with "sk-"
    // In production, this should validate against a database
    if api_key.starts_with("sk-") {
        return Ok(AuthenticatedUser {
            user_id: "api_key_user".to_string(),
            auth_method: AuthMethod::ApiKey,
            tenant_id: None,
            roles: vec!["user".to_string()],
        });
    }

    // Check for test API keys in development mode
    if cfg!(debug_assertions) && api_key.starts_with("test-") {
        return Ok(AuthenticatedUser {
            user_id: "test_user".to_string(),
            auth_method: AuthMethod::ApiKey,
            tenant_id: None,
            roles: vec!["user".to_string()],
        });
    }

    Err(AuthError {
        error: "invalid_api_key".to_string(),
        message: "The provided API key is invalid or expired".to_string(),
    })
}

/// Authentication middleware that validates JWT or API key.
pub async fn auth_middleware(
    State(state): State<crate::AppState>,
    mut req: Request<Body>,
    next: Next,
) -> Result<Response, AuthError> {
    // Skip auth for health endpoints
    let path = req.uri().path();
    if path == "/health" || path == "/ready" || path == "/metrics" {
        return Ok(next.run(req).await);
    }

    // Extract authorization header
    let auth_header = req
        .headers()
        .get(AUTHORIZATION)
        .and_then(|h| h.to_str().ok());

    let user = match auth_header {
        Some(header) if header.starts_with("Bearer ") => {
            let token = &header[7..];
            
            // Check if it's an API key (starts with sk-) or JWT
            if token.starts_with("sk-") || token.starts_with("test-") {
                // API key authentication
                #[cfg(feature = "embedded")]
                {
                    validate_api_key(token, &state.config, state.redis.as_ref(), state.surreal.as_ref()).await?
                }
                #[cfg(not(feature = "embedded"))]
                {
                    validate_api_key(token, &state.config, state.redis.as_ref()).await?
                }
            } else {
                // JWT authentication
                #[cfg(feature = "gateway")]
                {
                    let secret = state.config.gateway.jwt_secret.as_ref().ok_or_else(|| AuthError {
                        error: "configuration_error".to_string(),
                        message: "JWT secret not configured".to_string(),
                    })?;

                    let claims = validate_jwt(token, secret).map_err(|e| AuthError {
                        error: "invalid_token".to_string(),
                        message: format!("JWT validation failed: {}", e),
                    })?;

                    AuthenticatedUser {
                        user_id: claims.sub,
                        auth_method: AuthMethod::Jwt,
                        tenant_id: claims.tenant_id,
                        roles: claims.roles,
                    }
                }
                #[cfg(not(feature = "gateway"))]
                {
                    return Err(AuthError {
                        error: "not_supported".to_string(),
                        message: "JWT authentication requires 'gateway' feature".to_string(),
                    });
                }
            }
        }
        _ => {
            return Err(AuthError {
                error: "missing_auth".to_string(),
                message: "Authorization header is required".to_string(),
            });
        }
    };

    // Add authenticated user to request extensions
    req.extensions_mut().insert(user);

    Ok(next.run(req).await)
}

/// Extract authenticated user from request extensions.
pub fn get_authenticated_user(req: &Request<Body>) -> Option<&AuthenticatedUser> {
    req.extensions().get::<AuthenticatedUser>()
}
