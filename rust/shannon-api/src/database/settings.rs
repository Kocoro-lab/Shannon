//! Settings and API key repository.
//!
//! This module provides storage and retrieval of user settings and encrypted
//! API keys for LLM providers.

use crate::database::encryption::KeyManager;
use crate::database::hybrid::HybridBackend;
use anyhow::{Context, Result};
use async_trait::async_trait;
use chrono::{DateTime, Utc};
use rusqlite::params;
use serde::{Deserialize, Serialize};

/// User setting domain object.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UserSetting {
    /// User ID that owns this setting
    pub user_id: String,
    /// Setting key (unique per user)
    pub setting_key: String,
    /// Setting value (stored as string, parsed by client)
    pub setting_value: String,
    /// Type of the setting: 'string', 'number', 'boolean', 'json'
    pub setting_type: String,
    /// Whether the value is encrypted
    pub encrypted: bool,
    /// When the setting was created
    pub created_at: DateTime<Utc>,
    /// When the setting was last updated
    pub updated_at: DateTime<Utc>,
}

/// API key information (masked, for listing).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ApiKeyInfo {
    /// Provider name
    pub provider: String,
    /// Whether the key is configured
    pub is_configured: bool,
    /// Masked key (e.g., "sk-...xyz")
    pub masked_key: Option<String>,
    /// Whether the key is active
    pub is_active: bool,
    /// When the key was last used
    pub last_used_at: Option<DateTime<Utc>>,
    /// When the key was created
    pub created_at: Option<DateTime<Utc>>,
}

/// API key with decrypted value.
#[derive(Debug, Clone)]
pub struct ApiKey {
    /// User ID that owns this key
    pub user_id: String,
    /// Provider name
    pub provider: String,
    /// Decrypted API key value
    pub api_key: String,
    /// Whether the key is active
    pub is_active: bool,
    /// When the key was created
    pub created_at: DateTime<Utc>,
    /// When the key was last updated
    pub updated_at: DateTime<Utc>,
    /// When the key was last used
    pub last_used_at: Option<DateTime<Utc>>,
}

/// Repository trait for user settings.
#[async_trait]
pub trait SettingsRepository: Send + Sync {
    /// Get a single setting by key.
    async fn get_setting(&self, user_id: &str, key: &str) -> Result<Option<UserSetting>>;

    /// List all settings for a user.
    async fn list_settings(&self, user_id: &str) -> Result<Vec<UserSetting>>;

    /// Set a setting value (create or update).
    async fn set_setting(
        &self,
        user_id: &str,
        key: &str,
        value: &str,
        setting_type: &str,
        encrypted: bool,
    ) -> Result<()>;

    /// Delete a setting.
    async fn delete_setting(&self, user_id: &str, key: &str) -> Result<bool>;
}

/// Repository trait for API key management.
#[async_trait]
pub trait ApiKeyRepository: Send + Sync {
    /// Get an API key for a provider (decrypted).
    async fn get_api_key(&self, user_id: &str, provider: &str) -> Result<Option<ApiKey>>;

    /// List all providers with API key information (masked).
    async fn list_providers(&self, user_id: &str) -> Result<Vec<ApiKeyInfo>>;

    /// Set an API key for a provider (encrypts before storing).
    async fn set_api_key(&self, user_id: &str, provider: &str, api_key: &str) -> Result<String>;

    /// Delete an API key for a provider.
    async fn delete_api_key(&self, user_id: &str, provider: &str) -> Result<bool>;

    /// Mark an API key as used (updates last_used_at).
    async fn mark_key_used(&self, user_id: &str, provider: &str) -> Result<()>;
}

#[async_trait]
impl SettingsRepository for HybridBackend {
    async fn get_setting(&self, user_id: &str, key: &str) -> Result<Option<UserSetting>> {
        let user_id = user_id.to_string();
        let key = key.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<Option<UserSetting>> {
            let guard = sqlite.lock().unwrap();
            let conn = guard
                .as_ref()
                .ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;

            let mut stmt = conn.prepare(
                "SELECT user_id, setting_key, setting_value, setting_type, encrypted, created_at, updated_at
                 FROM user_settings WHERE user_id = ?1 AND setting_key = ?2",
            )?;

            let mut rows = stmt.query(params![user_id, key])?;

            if let Some(row) = rows.next()? {
                Ok(Some(UserSetting {
                    user_id: row.get(0)?,
                    setting_key: row.get(1)?,
                    setting_value: row.get(2)?,
                    setting_type: row.get(3)?,
                    encrypted: row.get(4)?,
                    created_at: parse_datetime(row.get::<_, String>(5)?),
                    updated_at: parse_datetime(row.get::<_, String>(6)?),
                }))
            } else {
                Ok(None)
            }
        })
        .await
        .context("Tokio spawn_blocking failed")?
    }

    async fn list_settings(&self, user_id: &str) -> Result<Vec<UserSetting>> {
        let user_id = user_id.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<Vec<UserSetting>> {
            let guard = sqlite.lock().unwrap();
            let conn = guard
                .as_ref()
                .ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;

            let mut stmt = conn.prepare(
                "SELECT user_id, setting_key, setting_value, setting_type, encrypted, created_at, updated_at
                 FROM user_settings WHERE user_id = ?1 ORDER BY setting_key ASC",
            )?;

            let rows = stmt.query_map(params![user_id], |row| {
                Ok(UserSetting {
                    user_id: row.get(0)?,
                    setting_key: row.get(1)?,
                    setting_value: row.get(2)?,
                    setting_type: row.get(3)?,
                    encrypted: row.get(4)?,
                    created_at: parse_datetime(row.get::<_, String>(5)?),
                    updated_at: parse_datetime(row.get::<_, String>(6)?),
                })
            })?;

            let mut settings = Vec::new();
            for item in rows {
                settings.push(item?);
            }
            Ok(settings)
        })
        .await
        .context("Tokio spawn_blocking failed")?
    }

    async fn set_setting(
        &self,
        user_id: &str,
        key: &str,
        value: &str,
        setting_type: &str,
        encrypted: bool,
    ) -> Result<()> {
        let user_id = user_id.to_string();
        let key = key.to_string();
        let value = value.to_string();
        let setting_type = setting_type.to_string();
        let sqlite = self.sqlite.clone();
        let now = Utc::now().to_rfc3339();

        tokio::task::spawn_blocking(move || -> Result<()> {
            let guard = sqlite.lock().unwrap();
            let conn = guard
                .as_ref()
                .ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;

            conn.execute(
                "INSERT INTO user_settings (user_id, setting_key, setting_value, setting_type, encrypted, created_at, updated_at)
                 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?6)
                 ON CONFLICT(user_id, setting_key) DO UPDATE SET
                   setting_value = excluded.setting_value,
                   setting_type = excluded.setting_type,
                   encrypted = excluded.encrypted,
                   updated_at = excluded.updated_at",
                params![user_id, key, value, setting_type, encrypted, now],
            )?;
            Ok(())
        })
        .await
        .context("Tokio spawn_blocking failed")?
    }

    async fn delete_setting(&self, user_id: &str, key: &str) -> Result<bool> {
        let user_id = user_id.to_string();
        let key = key.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<bool> {
            let guard = sqlite.lock().unwrap();
            let conn = guard
                .as_ref()
                .ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;

            let count = conn.execute(
                "DELETE FROM user_settings WHERE user_id = ?1 AND setting_key = ?2",
                params![user_id, key],
            )?;
            Ok(count > 0)
        })
        .await
        .context("Tokio spawn_blocking failed")?
    }
}

#[async_trait]
impl ApiKeyRepository for HybridBackend {
    async fn get_api_key(&self, user_id: &str, provider: &str) -> Result<Option<ApiKey>> {
        let user_id = user_id.to_string();
        let provider = provider.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<Option<ApiKey>> {
            let guard = sqlite.lock().unwrap();
            let conn = guard
                .as_ref()
                .ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;

            let mut stmt = conn.prepare(
                "SELECT user_id, provider, api_key, is_active, created_at, updated_at, last_used_at
                 FROM api_keys WHERE user_id = ?1 AND provider = ?2",
            )?;

            let mut rows = stmt.query(params![user_id, provider])?;

            if let Some(row) = rows.next()? {
                let encrypted_key: String = row.get(2)?;
                
                // Decrypt the API key
                let key_manager = KeyManager::from_default_path()?;
                let decrypted_key = key_manager
                    .decrypt(&encrypted_key)
                    .context("Failed to decrypt API key")?;

                Ok(Some(ApiKey {
                    user_id: row.get(0)?,
                    provider: row.get(1)?,
                    api_key: decrypted_key,
                    is_active: row.get(3)?,
                    created_at: parse_datetime(row.get::<_, String>(4)?),
                    updated_at: parse_datetime(row.get::<_, String>(5)?),
                    last_used_at: row.get::<_, Option<String>>(6)?.map(|s| parse_datetime(s)),
                }))
            } else {
                Ok(None)
            }
        })
        .await
        .context("Tokio spawn_blocking failed")?
    }

    async fn list_providers(&self, user_id: &str) -> Result<Vec<ApiKeyInfo>> {
        let user_id = user_id.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<Vec<ApiKeyInfo>> {
            let guard = sqlite.lock().unwrap();
            let conn = guard
                .as_ref()
                .ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;

            let mut stmt = conn.prepare(
                "SELECT provider, api_key, is_active, created_at, last_used_at
                 FROM api_keys WHERE user_id = ?1",
            )?;

            let key_manager = KeyManager::from_default_path()?;

            let rows = stmt.query_map(params![user_id], |row| {
                let encrypted_key: String = row.get(1)?;
                
                // Decrypt to get the actual key for masking
                let masked_key = key_manager
                    .decrypt(&encrypted_key)
                    .ok()
                    .map(|key| key_manager.mask_key(&key));

                Ok(ApiKeyInfo {
                    provider: row.get(0)?,
                    is_configured: true,
                    masked_key,
                    is_active: row.get(2)?,
                    created_at: row.get::<_, Option<String>>(3)?.map(|s| parse_datetime(s)),
                    last_used_at: row.get::<_, Option<String>>(4)?.map(|s| parse_datetime(s)),
                })
            })?;

            let mut providers = Vec::new();
            for item in rows {
                providers.push(item?);
            }
            Ok(providers)
        })
        .await
        .context("Tokio spawn_blocking failed")?
    }

    async fn set_api_key(&self, user_id: &str, provider: &str, api_key: &str) -> Result<String> {
        let user_id = user_id.to_string();
        let provider = provider.to_string();
        let api_key = api_key.to_string();
        let sqlite = self.sqlite.clone();
        let now = Utc::now().to_rfc3339();

        tokio::task::spawn_blocking(move || -> Result<String> {
            let guard = sqlite.lock().unwrap();
            let conn = guard
                .as_ref()
                .ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;

            // Encrypt the API key
            let key_manager = KeyManager::from_default_path()?;
            let encrypted_key = key_manager
                .encrypt(&api_key)
                .context("Failed to encrypt API key")?;
            let masked_key = key_manager.mask_key(&api_key);

            conn.execute(
                "INSERT INTO api_keys (user_id, provider, api_key, is_active, created_at, updated_at)
                 VALUES (?1, ?2, ?3, ?4, ?5, ?5)
                 ON CONFLICT(user_id, provider) DO UPDATE SET
                   api_key = excluded.api_key,
                   updated_at = excluded.updated_at",
                params![user_id, provider, encrypted_key, true, now],
            )?;
            
            Ok(masked_key)
        })
        .await
        .context("Tokio spawn_blocking failed")?
    }

    async fn delete_api_key(&self, user_id: &str, provider: &str) -> Result<bool> {
        let user_id = user_id.to_string();
        let provider = provider.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<bool> {
            let guard = sqlite.lock().unwrap();
            let conn = guard
                .as_ref()
                .ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;

            let count = conn.execute(
                "DELETE FROM api_keys WHERE user_id = ?1 AND provider = ?2",
                params![user_id, provider],
            )?;
            Ok(count > 0)
        })
        .await
        .context("Tokio spawn_blocking failed")?
    }

    async fn mark_key_used(&self, user_id: &str, provider: &str) -> Result<()> {
        let user_id = user_id.to_string();
        let provider = provider.to_string();
        let sqlite = self.sqlite.clone();
        let now = Utc::now().to_rfc3339();

        tokio::task::spawn_blocking(move || -> Result<()> {
            let guard = sqlite.lock().unwrap();
            let conn = guard
                .as_ref()
                .ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;

            conn.execute(
                "UPDATE api_keys SET last_used_at = ?1 WHERE user_id = ?2 AND provider = ?3",
                params![now, user_id, provider],
            )?;
            Ok(())
        })
        .await
        .context("Tokio spawn_blocking failed")?
    }
}

/// Parse datetime from RFC3339 string.
fn parse_datetime(value: String) -> DateTime<Utc> {
    DateTime::parse_from_rfc3339(&value)
        .map(|dt| dt.with_timezone(&Utc))
        .unwrap_or_else(|_| Utc::now())
}
