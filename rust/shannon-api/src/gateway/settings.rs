//! Settings and API key management endpoints.

use axum::{
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
    routing::{get, post},
    Extension, Json, Router,
};
use serde::{Deserialize, Serialize};

use crate::database::settings::{ApiKeyRepository, SettingsRepository};
use crate::gateway::auth::AuthenticatedUser;
use crate::AppState;

/// Settings routes.
pub fn router() -> Router<AppState> {
    Router::new()
        // General settings
        .route("/api/v1/settings", get(list_settings).post(set_setting))
        .route(
            "/api/v1/settings/{key}",
            get(get_setting).delete(delete_setting),
        )
        // API keys
        .route("/api/v1/settings/api-keys", get(list_api_keys))
        .route(
            "/api/v1/settings/api-keys/{provider}",
            post(set_api_key).delete(delete_api_key),
        )
}

/// Setting request body.
#[derive(Debug, Deserialize)]
pub struct SetSettingRequest {
    /// Setting key.
    pub key: String,
    /// Setting value.
    pub value: String,
    /// Type of the setting: 'string', 'number', 'boolean', 'json'.
    #[serde(default = "default_setting_type")]
    pub setting_type: String,
    /// Whether to encrypt the value.
    #[serde(default)]
    pub encrypted: bool,
}

fn default_setting_type() -> String {
    "string".to_string()
}

/// API key request body.
#[derive(Debug, Deserialize)]
pub struct SetApiKeyRequest {
    /// API key value.
    pub api_key: String,
}

/// API key response.
#[derive(Debug, Serialize)]
pub struct SetApiKeyResponse {
    /// Provider name.
    pub provider: String,
    /// Masked key.
    pub masked_key: String,
    /// Success message.
    pub message: String,
}

/// List all settings for the current user.
///
/// # Errors
///
/// Returns 500 if database query fails.
async fn list_settings(
    State(state): State<AppState>,
    Extension(user): Extension<AuthenticatedUser>,
) -> impl IntoResponse {
    tracing::debug!("📋 Listing settings - user_id={}", user.user_id);

    #[cfg(feature = "embedded")]
    let db = state.database.as_ref().ok_or_else(|| {
        tracing::error!("Database not available");
        ()
    });

    #[cfg(feature = "embedded")]
    let result = match db {
        Ok(crate::database::Database::Hybrid(backend)) => {
            backend.list_settings(&user.user_id).await
        }
        _ => Err(anyhow::anyhow!("Database backend not available")),
    };

    #[cfg(not(feature = "embedded"))]
    let result: Result<Vec<_>, _> = Err(anyhow::anyhow!("Embedded feature not enabled"));

    match result {
        Ok(settings) => {
            tracing::info!(
                "✅ Settings listed - user_id={}, count={}",
                user.user_id,
                settings.len()
            );
            (StatusCode::OK, Json(settings))
        }
        Err(e) => {
            tracing::error!(
                "❌ Failed to list settings - user_id={}, error={}",
                user.user_id,
                e
            );
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(vec![]), // Return empty array on error
            )
        }
    }
}

/// Get a single setting by key.
///
/// # Errors
///
/// Returns 404 if setting not found, 500 if database query fails.
async fn get_setting(
    State(state): State<AppState>,
    Extension(user): Extension<AuthenticatedUser>,
    Path(key): Path<String>,
) -> impl IntoResponse {
    tracing::debug!("🔍 Getting setting - user_id={}, key={}", user.user_id, key);

    #[cfg(feature = "embedded")]
    let result = match state.database.as_ref() {
        Some(crate::database::Database::Hybrid(backend)) => {
            backend.get_setting(&user.user_id, &key).await
        }
        _ => Err(anyhow::anyhow!("Database backend not available")),
    };

    #[cfg(not(feature = "embedded"))]
    let result: Result<Option<_>, _> = Err(anyhow::anyhow!("Embedded feature not enabled"));

    match result {
        Ok(Some(setting)) => {
            tracing::info!(
                "✅ Setting retrieved - user_id={}, key={}",
                user.user_id,
                key
            );
            (StatusCode::OK, Json(serde_json::to_value(setting).unwrap())).into_response()
        }
        Ok(None) => {
            tracing::warn!(
                "⚠️  Setting not found - user_id={}, key={}",
                user.user_id,
                key
            );
            (
                StatusCode::NOT_FOUND,
                Json(serde_json::json!({
                    "error": "not_found",
                    "message": format!("Setting '{}' not found", key)
                })),
            )
                .into_response()
        }
        Err(e) => {
            tracing::error!(
                "❌ Failed to get setting - user_id={}, key={}, error={}",
                user.user_id,
                key,
                e
            );
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({
                    "error": "internal_error",
                    "message": "Failed to retrieve setting"
                })),
            )
                .into_response()
        }
    }
}

/// Create or update a setting.
///
/// # Errors
///
/// Returns 400 if invalid request, 500 if database operation fails.
async fn set_setting(
    State(state): State<AppState>,
    Extension(user): Extension<AuthenticatedUser>,
    Json(req): Json<SetSettingRequest>,
) -> impl IntoResponse {
    tracing::info!(
        "💾 Setting value - user_id={}, key={}, type={}",
        user.user_id,
        req.key,
        req.setting_type
    );

    // Validate setting type
    if !matches!(
        req.setting_type.as_str(),
        "string" | "number" | "boolean" | "json"
    ) {
        tracing::warn!(
            "⚠️  Invalid setting type - user_id={}, key={}, type={}",
            user.user_id,
            req.key,
            req.setting_type
        );
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({
                "error": "invalid_type",
                "message": format!("Invalid setting type '{}'. Must be one of: string, number, boolean, json", req.setting_type)
            })),
        );
    }

    #[cfg(feature = "embedded")]
    let result = match state.database.as_ref() {
        Some(crate::database::Database::Hybrid(backend)) => {
            backend
                .set_setting(
                    &user.user_id,
                    &req.key,
                    &req.value,
                    &req.setting_type,
                    req.encrypted,
                )
                .await
        }
        _ => Err(anyhow::anyhow!("Database backend not available")),
    };

    #[cfg(not(feature = "embedded"))]
    let result: Result<(), _> = Err(anyhow::anyhow!("Embedded feature not enabled"));

    match result {
        Ok(()) => {
            tracing::info!(
                "✅ Setting saved - user_id={}, key={}",
                user.user_id,
                req.key
            );
            (
                StatusCode::OK,
                Json(serde_json::json!({
                    "key": req.key,
                    "message": "Setting saved successfully"
                })),
            )
        }
        Err(e) => {
            tracing::error!(
                "❌ Failed to save setting - user_id={}, key={}, error={}",
                user.user_id,
                req.key,
                e
            );
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({
                    "error": "internal_error",
                    "message": "Failed to save setting"
                })),
            )
        }
    }
}

/// Delete a setting.
///
/// # Errors
///
/// Returns 404 if setting not found, 500 if database operation fails.
async fn delete_setting(
    State(state): State<AppState>,
    Extension(user): Extension<AuthenticatedUser>,
    Path(key): Path<String>,
) -> impl IntoResponse {
    tracing::info!(
        "🗑️  Deleting setting - user_id={}, key={}",
        user.user_id,
        key
    );

    #[cfg(feature = "embedded")]
    let result = match state.database.as_ref() {
        Some(crate::database::Database::Hybrid(backend)) => {
            backend.delete_setting(&user.user_id, &key).await
        }
        _ => Err(anyhow::anyhow!("Database backend not available")),
    };

    #[cfg(not(feature = "embedded"))]
    let result: Result<bool, _> = Err(anyhow::anyhow!("Embedded feature not enabled"));

    match result {
        Ok(true) => {
            tracing::info!("✅ Setting deleted - user_id={}, key={}", user.user_id, key);
            (
                StatusCode::OK,
                Json(serde_json::json!({
                    "deleted": true,
                    "key": key
                })),
            )
        }
        Ok(false) => {
            tracing::warn!(
                "⚠️  Setting not found for deletion - user_id={}, key={}",
                user.user_id,
                key
            );
            (
                StatusCode::NOT_FOUND,
                Json(serde_json::json!({
                    "error": "not_found",
                    "message": format!("Setting '{}' not found", key)
                })),
            )
        }
        Err(e) => {
            tracing::error!(
                "❌ Failed to delete setting - user_id={}, key={}, error={}",
                user.user_id,
                key,
                e
            );
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({
                    "error": "internal_error",
                    "message": "Failed to delete setting"
                })),
            )
        }
    }
}

/// List all API keys (with masked values).
///
/// # Errors
///
/// Returns 500 if database query fails.
async fn list_api_keys(
    State(state): State<AppState>,
    Extension(user): Extension<AuthenticatedUser>,
) -> impl IntoResponse {
    tracing::debug!("🔑 Listing API keys - user_id={}", user.user_id);

    #[cfg(feature = "embedded")]
    let result = match state.database.as_ref() {
        Some(crate::database::Database::Hybrid(backend)) => {
            backend.list_providers(&user.user_id).await
        }
        _ => Err(anyhow::anyhow!("Database backend not available")),
    };

    #[cfg(not(feature = "embedded"))]
    let result: Result<Vec<_>, _> = Err(anyhow::anyhow!("Embedded feature not enabled"));

    match result {
        Ok(providers) => {
            tracing::info!(
                "✅ API keys listed - user_id={}, count={}",
                user.user_id,
                providers.len()
            );
            (StatusCode::OK, Json(providers))
        }
        Err(e) => {
            tracing::error!(
                "❌ Failed to list API keys - user_id={}, error={}",
                user.user_id,
                e
            );
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(vec![]), // Return empty array on error
            )
        }
    }
}

/// Set an API key for a provider.
///
/// # Errors
///
/// Returns 400 if invalid request, 500 if database operation fails.
async fn set_api_key(
    State(state): State<AppState>,
    Extension(user): Extension<AuthenticatedUser>,
    Path(provider): Path<String>,
    Json(req): Json<SetApiKeyRequest>,
) -> impl IntoResponse {
    tracing::info!(
        "🔐 Setting API key - user_id={}, provider={}",
        user.user_id,
        provider
    );

    // Validate provider
    let valid_providers = ["openai", "anthropic", "google", "groq", "xai"];
    if !valid_providers.contains(&provider.as_str()) {
        tracing::warn!(
            "⚠️  Invalid provider - user_id={}, provider={}",
            user.user_id,
            provider
        );
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({
                "error": "invalid_provider",
                "message": format!("Invalid provider '{}'. Must be one of: {}", provider, valid_providers.join(", "))
            })),
        )
            .into_response();
    }

    // Validate key format (basic check)
    if req.api_key.is_empty() || req.api_key.len() < 10 {
        tracing::warn!(
            "⚠️  Invalid API key format - user_id={}, provider={}",
            user.user_id,
            provider
        );
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({
                "error": "invalid_key",
                "message": "API key is too short or empty"
            })),
        )
            .into_response();
    }

    #[cfg(feature = "embedded")]
    let result = match state.database.as_ref() {
        Some(crate::database::Database::Hybrid(backend)) => {
            backend
                .set_api_key(&user.user_id, &provider, &req.api_key)
                .await
        }
        _ => Err(anyhow::anyhow!("Database backend not available")),
    };

    #[cfg(not(feature = "embedded"))]
    let result: Result<String, _> = Err(anyhow::anyhow!("Embedded feature not enabled"));

    match result {
        Ok(masked_key) => {
            tracing::info!(
                "✅ API key saved - user_id={}, provider={}, masked={}",
                user.user_id,
                provider,
                masked_key
            );
            (
                StatusCode::OK,
                Json(SetApiKeyResponse {
                    provider: provider.clone(),
                    masked_key,
                    message: format!("API key for '{}' saved successfully", provider),
                }),
            )
                .into_response()
        }
        Err(e) => {
            tracing::error!(
                "❌ Failed to save API key - user_id={}, provider={}, error={}",
                user.user_id,
                provider,
                e
            );
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({
                    "error": "internal_error",
                    "message": "Failed to save API key"
                })),
            )
                .into_response()
        }
    }
}

/// Delete an API key for a provider.
///
/// # Errors
///
/// Returns 404 if key not found, 500 if database operation fails.
async fn delete_api_key(
    State(state): State<AppState>,
    Extension(user): Extension<AuthenticatedUser>,
    Path(provider): Path<String>,
) -> impl IntoResponse {
    tracing::info!(
        "🗑️  Deleting API key - user_id={}, provider={}",
        user.user_id,
        provider
    );

    #[cfg(feature = "embedded")]
    let result = match state.database.as_ref() {
        Some(crate::database::Database::Hybrid(backend)) => {
            backend.delete_api_key(&user.user_id, &provider).await
        }
        _ => Err(anyhow::anyhow!("Database backend not available")),
    };

    #[cfg(not(feature = "embedded"))]
    let result: Result<bool, _> = Err(anyhow::anyhow!("Embedded feature not enabled"));

    match result {
        Ok(true) => {
            tracing::info!(
                "✅ API key deleted - user_id={}, provider={}",
                user.user_id,
                provider
            );
            (
                StatusCode::OK,
                Json(serde_json::json!({
                    "deleted": true,
                    "provider": provider
                })),
            )
        }
        Ok(false) => {
            tracing::warn!(
                "⚠️  API key not found for deletion - user_id={}, provider={}",
                user.user_id,
                provider
            );
            (
                StatusCode::NOT_FOUND,
                Json(serde_json::json!({
                    "error": "not_found",
                    "message": format!("API key for provider '{}' not found", provider)
                })),
            )
        }
        Err(e) => {
            tracing::error!(
                "❌ Failed to delete API key - user_id={}, provider={}, error={}",
                user.user_id,
                provider,
                e
            );
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({
                    "error": "internal_error",
                    "message": "Failed to delete API key"
                })),
            )
        }
    }
}
