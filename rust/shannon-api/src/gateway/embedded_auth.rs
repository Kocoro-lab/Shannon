//! JWT authentication for embedded mode.
//!
//! Provides JWT token generation and validation for local users in embedded deployments.
//! Tokens are signed with a configurable secret and have configurable expiry.

use anyhow::{Context, Result};
use chrono::{Duration, Utc};
use serde::{Deserialize, Serialize};

#[cfg(feature = "gateway")]
use jsonwebtoken::{decode, encode, DecodingKey, EncodingKey, Header, Validation};

/// JWT claims for embedded users.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EmbeddedClaims {
    /// User ID (subject).
    pub sub: String,
    /// Username.
    pub username: String,
    /// Email (optional).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub email: Option<String>,
    /// Issued at timestamp.
    pub iat: i64,
    /// Expiration timestamp.
    pub exp: i64,
}

impl EmbeddedClaims {
    /// Create new claims for a user.
    ///
    /// # Parameters
    /// - `user_id`: Unique user identifier
    /// - `username`: Username
    /// - `email`: Optional email address
    /// - `expiry_days`: Token validity in days
    pub fn new(user_id: String, username: String, email: Option<String>, expiry_days: i64) -> Self {
        let now = Utc::now();
        let exp = now + Duration::days(expiry_days);

        Self {
            sub: user_id,
            username,
            email,
            iat: now.timestamp(),
            exp: exp.timestamp(),
        }
    }
}

/// Generate a JWT token for an embedded user.
///
/// # Parameters
/// - `user_id`: Unique identifier for the user
/// - `username`: Username
/// - `email`: Optional email address
/// - `secret`: JWT signing secret
/// - `expiry_days`: Token expiry in days (default: 7 for embedded mode)
///
/// # Returns
/// JWT token string
///
/// # Errors
/// Returns error if token encoding fails
#[cfg(feature = "gateway")]
pub fn generate_embedded_jwt(
    user_id: &str,
    username: &str,
    email: Option<&str>,
    secret: &str,
    expiry_days: i64,
) -> Result<String> {
    let claims = EmbeddedClaims::new(
        user_id.to_string(),
        username.to_string(),
        email.map(String::from),
        expiry_days,
    );

    let token = encode(
        &Header::default(),
        &claims,
        &EncodingKey::from_secret(secret.as_bytes()),
    )
    .context("Failed to encode JWT token")?;

    Ok(token)
}

/// Validate a JWT token and extract claims.
///
/// # Parameters
/// - `token`: JWT token string
/// - `secret`: JWT signing secret
///
/// # Returns
/// Embedded claims if validation succeeds
///
/// # Errors
/// Returns error if token is invalid, expired, or malformed
#[cfg(feature = "gateway")]
pub fn validate_embedded_jwt(token: &str, secret: &str) -> Result<EmbeddedClaims> {
    let token_data = decode::<EmbeddedClaims>(
        token,
        &DecodingKey::from_secret(secret.as_bytes()),
        &Validation::default(),
    )
    .context("Failed to validate JWT token")?;

    Ok(token_data.claims)
}

/// Stub implementations for builds without gateway feature.
#[cfg(not(feature = "gateway"))]
pub fn generate_embedded_jwt(
    _user_id: &str,
    _username: &str,
    _email: Option<&str>,
    _secret: &str,
    _expiry_days: i64,
) -> Result<String> {
    Err(anyhow::anyhow!("JWT support requires 'gateway' feature"))
}

#[cfg(not(feature = "gateway"))]
pub fn validate_embedded_jwt(_token: &str, _secret: &str) -> Result<EmbeddedClaims> {
    Err(anyhow::anyhow!("JWT support requires 'gateway' feature"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    #[cfg(feature = "gateway")]
    fn test_jwt_roundtrip() {
        let secret = "test-secret-key";
        let user_id = "user-123";
        let username = "testuser";
        let email = Some("test@example.com");

        // Generate token
        let token = generate_embedded_jwt(user_id, username, email, secret, 7)
            .expect("Failed to generate JWT");

        // Validate token
        let claims = validate_embedded_jwt(&token, secret).expect("Failed to validate JWT");

        assert_eq!(claims.sub, user_id);
        assert_eq!(claims.username, username);
        assert_eq!(claims.email.as_deref(), email);
    }

    #[test]
    #[cfg(feature = "gateway")]
    fn test_jwt_expiry() {
        let secret = "test-secret-key";

        // Create claims with past expiry
        let mut claims =
            EmbeddedClaims::new("user-123".to_string(), "testuser".to_string(), None, 7);
        claims.exp = Utc::now().timestamp() - 3600; // Expired 1 hour ago

        let token = encode(
            &Header::default(),
            &claims,
            &EncodingKey::from_secret(secret.as_bytes()),
        )
        .expect("Failed to encode token");

        // Should fail validation due to expiry
        let result = validate_embedded_jwt(&token, secret);
        assert!(result.is_err());
    }

    #[test]
    #[cfg(feature = "gateway")]
    fn test_jwt_wrong_secret() {
        let secret = "test-secret-key";
        let wrong_secret = "wrong-secret";

        let token = generate_embedded_jwt("user-123", "testuser", None, secret, 7)
            .expect("Failed to generate JWT");

        // Should fail with wrong secret
        let result = validate_embedded_jwt(&token, wrong_secret);
        assert!(result.is_err());
    }
}
