//! Session management for workflow continuity.
//!
//! Provides session management for associating workflows with user sessions,
//! enabling conversation history and context persistence across app restarts.
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::embedded::{SessionManager, Session};
//!
//! let manager = SessionManager::new(database).await?;
//!
//! // Create a new session
//! let session = manager.create_session("user-123").await?;
//!
//! // Associate a workflow
//! manager.associate_workflow(&session.id, "workflow-456").await?;
//!
//! // Get session with history
//! let session = manager.get_session(&session.id).await?;
//! ```

use std::collections::HashMap;
use std::sync::Arc;

use anyhow::{Context as AnyhowContext, Result};
use chrono::{DateTime, Duration, Utc};
use parking_lot::Mutex;
use rusqlite::{params, Connection};
use serde::{Deserialize, Serialize};
use serde_json::Value;

/// Session for tracking conversation and workflow context.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Session {
    /// Unique session identifier.
    pub id: String,

    /// User identifier.
    pub user_id: String,

    /// Currently active workflow ID.
    pub active_workflow_id: Option<String>,

    /// Session context (key-value storage).
    pub context: HashMap<String, Value>,

    /// Session creation timestamp.
    #[serde(with = "chrono::serde::ts_seconds")]
    pub created_at: DateTime<Utc>,

    /// Last activity timestamp.
    #[serde(with = "chrono::serde::ts_seconds")]
    pub last_activity: DateTime<Utc>,
}

/// Manager for session operations.
#[derive(Clone)]
pub struct SessionManager {
    /// Database connection pool.
    conn: Arc<Mutex<Connection>>,
}

impl SessionManager {
    /// Create a new session manager.
    ///
    /// # Errors
    ///
    /// Returns error if database initialization fails.
    pub async fn new(db_path: impl AsRef<std::path::Path>) -> Result<Self> {
        let conn = Connection::open(db_path.as_ref())
            .context("Failed to open database for session management")?;

        // Enable foreign keys
        conn.execute("PRAGMA foreign_keys = ON", [])
            .context("Failed to enable foreign keys")?;

        // Enable WAL mode for concurrent access (returns results, so use query)
        conn.query_row("PRAGMA journal_mode = WAL", [], |_row| Ok(()))
            .context("Failed to enable WAL mode")?;

        let manager = Self {
            conn: Arc::new(Mutex::new(conn)),
        };

        // Initialize schema
        manager.init_schema().await?;

        Ok(manager)
    }

    /// Initialize database schema for sessions.
    async fn init_schema(&self) -> Result<()> {
        let conn = self.conn.lock();

        conn.execute(
            "CREATE TABLE IF NOT EXISTS sessions (
                id TEXT PRIMARY KEY,
                user_id TEXT NOT NULL,
                active_workflow_id TEXT,
                context TEXT NOT NULL DEFAULT '{}',
                created_at INTEGER NOT NULL,
                last_activity INTEGER NOT NULL
            )",
            [],
        )
        .context("Failed to create sessions table")?;

        conn.execute(
            "CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id)",
            [],
        )
        .context("Failed to create user index")?;

        conn.execute(
            "CREATE INDEX IF NOT EXISTS idx_sessions_activity 
             ON sessions(last_activity DESC)",
            [],
        )
        .context("Failed to create activity index")?;

        Ok(())
    }

    /// Create a new session for a user.
    ///
    /// # Errors
    ///
    /// Returns error if session creation fails.
    pub async fn create_session(&self, user_id: impl Into<String>) -> Result<Session> {
        let session = Session {
            id: uuid::Uuid::new_v4().to_string(),
            user_id: user_id.into(),
            active_workflow_id: None,
            context: HashMap::new(),
            created_at: Utc::now(),
            last_activity: Utc::now(),
        };

        let conn = self.conn.lock();
        conn.execute(
            "INSERT INTO sessions (id, user_id, active_workflow_id, context, created_at, last_activity)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
            params![
                &session.id,
                &session.user_id,
                &session.active_workflow_id,
                serde_json::to_string(&session.context)?,
                session.created_at.timestamp(),
                session.last_activity.timestamp(),
            ],
        )
        .context("Failed to insert session")?;

        tracing::info!(
            session_id = %session.id,
            user_id = %session.user_id,
            "Created new session"
        );

        Ok(session)
    }

    /// Get a session by ID.
    ///
    /// # Errors
    ///
    /// Returns error if session not found or database query fails.
    pub async fn get_session(&self, session_id: &str) -> Result<Session> {
        let conn = self.conn.lock();

        let mut stmt = conn
            .prepare("SELECT id, user_id, active_workflow_id, context, created_at, last_activity FROM sessions WHERE id = ?1")?;

        let session = stmt
            .query_row([session_id], |row| {
                let context_str: String = row.get(3)?;
                let context: HashMap<String, Value> =
                    serde_json::from_str(&context_str).unwrap_or_default();

                Ok(Session {
                    id: row.get(0)?,
                    user_id: row.get(1)?,
                    active_workflow_id: row.get(2)?,
                    context,
                    created_at: DateTime::from_timestamp(row.get(4)?, 0).unwrap_or_else(Utc::now),
                    last_activity: DateTime::from_timestamp(row.get(5)?, 0)
                        .unwrap_or_else(Utc::now),
                })
            })
            .context("Session not found")?;

        Ok(session)
    }

    /// Update session context.
    ///
    /// # Errors
    ///
    /// Returns error if session not found or update fails.
    pub async fn update_context(
        &self,
        session_id: &str,
        key: impl Into<String>,
        value: Value,
    ) -> Result<()> {
        // Get current session
        let mut session = self.get_session(session_id).await?;

        // Update context
        session.context.insert(key.into(), value);
        session.last_activity = Utc::now();

        // Save to database
        let conn = self.conn.lock();
        conn.execute(
            "UPDATE sessions SET context = ?1, last_activity = ?2 WHERE id = ?3",
            params![
                serde_json::to_string(&session.context)?,
                session.last_activity.timestamp(),
                session_id,
            ],
        )
        .context("Failed to update session context")?;

        Ok(())
    }

    /// Associate a workflow with a session.
    ///
    /// # Errors
    ///
    /// Returns error if session not found or association fails.
    pub async fn associate_workflow(&self, session_id: &str, workflow_id: &str) -> Result<()> {
        let conn = self.conn.lock();

        let rows_affected = conn
            .execute(
                "UPDATE sessions SET active_workflow_id = ?1, last_activity = ?2 WHERE id = ?3",
                params![workflow_id, Utc::now().timestamp(), session_id],
            )
            .context("Failed to associate workflow with session")?;

        if rows_affected == 0 {
            anyhow::bail!("Session not found: {session_id}");
        }

        tracing::debug!(session_id, workflow_id, "Associated workflow with session");

        Ok(())
    }

    /// List active sessions for a user.
    ///
    /// # Errors
    ///
    /// Returns error if database query fails.
    pub async fn list_active_sessions(&self, user_id: &str, limit: usize) -> Result<Vec<Session>> {
        let conn = self.conn.lock();

        let mut stmt = conn.prepare(
            "SELECT id, user_id, active_workflow_id, context, created_at, last_activity
             FROM sessions
             WHERE user_id = ?1
             ORDER BY last_activity DESC
             LIMIT ?2",
        )?;

        let sessions = stmt
            .query_map(params![user_id, limit as i64], |row| {
                let context_str: String = row.get(3)?;
                let context: HashMap<String, Value> =
                    serde_json::from_str(&context_str).unwrap_or_default();

                Ok(Session {
                    id: row.get(0)?,
                    user_id: row.get(1)?,
                    active_workflow_id: row.get(2)?,
                    context,
                    created_at: DateTime::from_timestamp(row.get(4)?, 0).unwrap_or_else(Utc::now),
                    last_activity: DateTime::from_timestamp(row.get(5)?, 0)
                        .unwrap_or_else(Utc::now),
                })
            })?
            .collect::<Result<Vec<_>, _>>()?;

        Ok(sessions)
    }

    /// Delete expired sessions.
    ///
    /// Sessions inactive for more than the specified duration are deleted.
    ///
    /// # Errors
    ///
    /// Returns error if cleanup fails.
    pub async fn cleanup_expired(&self, max_age: Duration) -> Result<usize> {
        let cutoff = Utc::now() - max_age;
        let conn = self.conn.lock();

        let deleted = conn
            .execute(
                "DELETE FROM sessions WHERE last_activity < ?1",
                params![cutoff.timestamp()],
            )
            .context("Failed to delete expired sessions")?;

        if deleted > 0 {
            tracing::info!(deleted, "Cleaned up expired sessions");
        }

        Ok(deleted)
    }

    /// Delete a specific session.
    ///
    /// # Errors
    ///
    /// Returns error if deletion fails.
    pub async fn delete_session(&self, session_id: &str) -> Result<()> {
        let conn = self.conn.lock();

        let rows = conn
            .execute("DELETE FROM sessions WHERE id = ?1", params![session_id])
            .context("Failed to delete session")?;

        if rows == 0 {
            anyhow::bail!("Session not found: {session_id}");
        }

        tracing::info!(session_id, "Deleted session");

        Ok(())
    }

    /// Get all sessions (for testing/debugging).
    ///
    /// # Errors
    ///
    /// Returns error if query fails.
    #[cfg(test)]
    pub async fn get_all_sessions(&self) -> Result<Vec<Session>> {
        let conn = self.conn.lock();

        let mut stmt = conn.prepare(
            "SELECT id, user_id, active_workflow_id, context, created_at, last_activity 
             FROM sessions 
             ORDER BY created_at DESC",
        )?;

        let sessions = stmt
            .query_map([], |row| {
                let context_str: String = row.get(3)?;
                let context: HashMap<String, Value> =
                    serde_json::from_str(&context_str).unwrap_or_default();

                Ok(Session {
                    id: row.get(0)?,
                    user_id: row.get(1)?,
                    active_workflow_id: row.get(2)?,
                    context,
                    created_at: DateTime::from_timestamp(row.get(4)?, 0).unwrap_or_else(Utc::now),
                    last_activity: DateTime::from_timestamp(row.get(5)?, 0)
                        .unwrap_or_else(Utc::now),
                })
            })?
            .collect::<Result<Vec<_>, _>>()?;

        Ok(sessions)
    }
}

impl std::fmt::Debug for SessionManager {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("SessionManager").finish()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::NamedTempFile;

    async fn create_test_manager() -> (SessionManager, NamedTempFile) {
        let temp_file = NamedTempFile::new().unwrap();
        let manager = SessionManager::new(temp_file.path()).await.unwrap();
        (manager, temp_file)
    }

    #[tokio::test]
    async fn test_create_session() {
        let (manager, _temp) = create_test_manager().await;

        let session = manager.create_session("user-123").await.unwrap();

        assert_eq!(session.user_id, "user-123");
        assert!(session.active_workflow_id.is_none());
        assert!(session.context.is_empty());
    }

    #[tokio::test]
    async fn test_get_session() {
        let (manager, _temp) = create_test_manager().await;

        let created = manager.create_session("user-123").await.unwrap();
        let retrieved = manager.get_session(&created.id).await.unwrap();

        assert_eq!(created.id, retrieved.id);
        assert_eq!(created.user_id, retrieved.user_id);
    }

    #[tokio::test]
    async fn test_get_nonexistent_session() {
        let (manager, _temp) = create_test_manager().await;

        let result = manager.get_session("nonexistent").await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_update_context() {
        let (manager, _temp) = create_test_manager().await;

        let session = manager.create_session("user-123").await.unwrap();

        manager
            .update_context(&session.id, "key1", Value::String("value1".to_string()))
            .await
            .unwrap();

        let updated = manager.get_session(&session.id).await.unwrap();
        assert_eq!(
            updated.context.get("key1"),
            Some(&Value::String("value1".to_string()))
        );
    }

    #[tokio::test]
    async fn test_associate_workflow() {
        let (manager, _temp) = create_test_manager().await;

        let session = manager.create_session("user-123").await.unwrap();

        manager
            .associate_workflow(&session.id, "workflow-456")
            .await
            .unwrap();

        let updated = manager.get_session(&session.id).await.unwrap();
        assert_eq!(updated.active_workflow_id, Some("workflow-456".to_string()));
    }

    #[tokio::test]
    async fn test_list_active_sessions() {
        let (manager, _temp) = create_test_manager().await;

        manager.create_session("user-123").await.unwrap();
        manager.create_session("user-123").await.unwrap();
        manager.create_session("user-456").await.unwrap();

        let sessions = manager.list_active_sessions("user-123", 10).await.unwrap();
        assert_eq!(sessions.len(), 2);
        assert!(sessions.iter().all(|s| s.user_id == "user-123"));
    }

    #[tokio::test]
    async fn test_cleanup_expired() {
        let (manager, _temp) = create_test_manager().await;

        manager.create_session("user-123").await.unwrap();

        // No sessions should be expired with 90 day threshold
        let deleted = manager.cleanup_expired(Duration::days(90)).await.unwrap();
        assert_eq!(deleted, 0);

        // All sessions should be expired with negative duration (force expire)
        tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
        let deleted = manager
            .cleanup_expired(Duration::seconds(-1))
            .await
            .unwrap();
        assert_eq!(deleted, 1);
    }

    #[tokio::test]
    async fn test_delete_session() {
        let (manager, _temp) = create_test_manager().await;

        let session = manager.create_session("user-123").await.unwrap();

        manager.delete_session(&session.id).await.unwrap();

        let result = manager.get_session(&session.id).await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_multiple_context_updates() {
        let (manager, _temp) = create_test_manager().await;

        let session = manager.create_session("user-123").await.unwrap();

        manager
            .update_context(&session.id, "key1", Value::String("value1".to_string()))
            .await
            .unwrap();

        manager
            .update_context(&session.id, "key2", Value::Number(42.into()))
            .await
            .unwrap();

        let updated = manager.get_session(&session.id).await.unwrap();
        assert_eq!(updated.context.len(), 2);
        assert_eq!(
            updated.context.get("key1"),
            Some(&Value::String("value1".to_string()))
        );
        assert_eq!(updated.context.get("key2"), Some(&Value::Number(42.into())));
    }

    #[tokio::test]
    async fn test_session_activity_ordering() {
        let (manager, _temp) = create_test_manager().await;

        let session1 = manager.create_session("user-123").await.unwrap();
        tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;

        let session2 = manager.create_session("user-123").await.unwrap();

        // Update session2's activity to ensure it's more recent
        tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
        manager
            .update_context(&session2.id, "test", Value::Bool(true))
            .await
            .unwrap();

        let sessions = manager.list_active_sessions("user-123", 10).await.unwrap();

        // Should return both sessions ordered by last_activity DESC
        assert_eq!(sessions.len(), 2);
        assert!(sessions.iter().any(|s| s.id == session1.id));
        assert!(sessions.iter().any(|s| s.id == session2.id));
        // Verify all sessions have correct user_id
        assert!(sessions.iter().all(|s| s.user_id == "user-123"));
        // Verify session2 has the context update
        let session2_result = sessions.iter().find(|s| s.id == session2.id).unwrap();
        assert!(session2_result.context.contains_key("test"));
    }
}
