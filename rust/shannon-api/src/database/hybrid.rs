use crate::database::repository::{Memory, MemoryRepository, Run, RunRepository};
use anyhow::{Context, Result};
use async_trait::async_trait;
use chrono::{DateTime, Utc};
use rusqlite::{params, Connection};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use usearch::{Index, IndexOptions, MetricKind, ScalarKind};

/// Hybrid Backend: SQLite + USearch
/// - SQLite: Stores metadata, sessions, runs, memories (Relational)
/// - USearch: Stores embedding vectors (Vector Index)
#[derive(Clone)]
pub struct HybridBackend {
    db_path: PathBuf,
    vector_path: PathBuf,
    // Coarse-grained locking for embedded usage
    pub(crate) sqlite: Arc<Mutex<Option<Connection>>>,
    index: Arc<Mutex<Option<Index>>>,
}

impl std::fmt::Debug for HybridBackend {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let sqlite_ready = self
            .sqlite
            .lock()
            .map(|guard| guard.is_some())
            .unwrap_or(false);
        f.debug_struct("HybridBackend")
            .field("db_path", &self.db_path)
            .field("vector_path", &self.vector_path)
            .field("sqlite", &sqlite_ready)
            .finish()
    }
}

/// Control state for workflow.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ControlState {
    pub workflow_id: String,
    pub is_paused: bool,
    pub is_cancelled: bool,
    pub paused_at: Option<DateTime<Utc>>,
    pub pause_reason: Option<String>,
    pub paused_by: Option<String>,
    pub cancel_reason: Option<String>,
    pub cancelled_by: Option<String>,
    pub updated_at: DateTime<Utc>,
}

impl HybridBackend {
    pub fn new(data_dir: PathBuf) -> Self {
        let db_path = data_dir.join("shannon.sqlite");
        let vector_path = data_dir.join("shannon.usearch");
        Self {
            db_path,
            vector_path,
            sqlite: Arc::new(Mutex::new(None)),
            index: Arc::new(Mutex::new(None)),
        }
    }

    pub async fn init(&self) -> Result<()> {
        let this_sqlite = self.sqlite.clone();
        let this_index = self.index.clone();
        let db_path = self.db_path.clone();
        let vector_path = self.vector_path.clone();

        tokio::task::spawn_blocking(move || -> Result<()> {
             // --- SQLite Logic ---
             {
                 let mut guard = this_sqlite.lock().unwrap();
                 if guard.is_none() {
                     if let Some(parent) = db_path.parent() {
                        std::fs::create_dir_all(parent)?;
                     }
                     let conn = Connection::open(&db_path)?;
                     // Enable WAL mode for concurrency
                     conn.pragma_update(None, "journal_mode", "WAL")?;

                     conn.execute_batch(
                        "-- Embeddings/Memories Table
                        CREATE TABLE IF NOT EXISTS memories (
                            id TEXT PRIMARY KEY,
                            conversation_id TEXT NOT NULL,
                            vector_id INTEGER, -- u64 hash for USearch
                            role TEXT NOT NULL,
                            content TEXT NOT NULL,
                            metadata TEXT,
                            created_at DATETIME NOT NULL
                        );
                        CREATE INDEX IF NOT EXISTS idx_memories_conv ON memories(conversation_id);
                        CREATE INDEX IF NOT EXISTS idx_memories_vector ON memories(vector_id);

                        -- Runs Table
                        CREATE TABLE IF NOT EXISTS runs (
                            id TEXT PRIMARY KEY,
                            user_id TEXT NOT NULL,
                            session_id TEXT,
                            status TEXT NOT NULL,
                            created_at DATETIME NOT NULL,
                            data JSON NOT NULL -- Stores the full Run object
                        );
                        CREATE INDEX IF NOT EXISTS idx_runs_user ON runs(user_id);
                        CREATE INDEX IF NOT EXISTS idx_runs_user_status ON runs(user_id, status);
                        CREATE INDEX IF NOT EXISTS idx_runs_session ON runs(session_id) WHERE session_id IS NOT NULL;
                        CREATE INDEX IF NOT EXISTS idx_runs_created_desc ON runs(user_id, created_at DESC);

                        -- Workflow Events Table
                        CREATE TABLE IF NOT EXISTS workflow_events (
                            workflow_id TEXT NOT NULL,
                            event_index INTEGER NOT NULL,
                            event_type TEXT NOT NULL,
                            data BLOB NOT NULL,
                            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
                            PRIMARY KEY (workflow_id, event_index)
                        );

                        -- Workflow Control State Table
                        CREATE TABLE IF NOT EXISTS workflow_control_state (
                            workflow_id TEXT PRIMARY KEY,
                            is_paused BOOLEAN NOT NULL DEFAULT 0,
                            is_cancelled BOOLEAN NOT NULL DEFAULT 0,
                            paused_at TEXT,
                            pause_reason TEXT,
                            paused_by TEXT,
                            cancel_reason TEXT,
                            cancelled_by TEXT,
                            updated_at TEXT NOT NULL
                        );

                        -- User Settings Table (for embedded user settings)
                        CREATE TABLE IF NOT EXISTS user_settings (
                            user_id TEXT NOT NULL DEFAULT 'embedded_user',
                            setting_key TEXT NOT NULL,
                            setting_value TEXT NOT NULL,
                            setting_type TEXT NOT NULL, -- 'string', 'number', 'boolean', 'json'
                            encrypted BOOLEAN NOT NULL DEFAULT 0,
                            created_at TEXT NOT NULL,
                            updated_at TEXT NOT NULL,
                            PRIMARY KEY (user_id, setting_key)
                        );
                        CREATE INDEX IF NOT EXISTS idx_user_settings_user_id ON user_settings(user_id);

                        -- API Keys Table (encrypted storage)
                        CREATE TABLE IF NOT EXISTS api_keys (
                            user_id TEXT NOT NULL DEFAULT 'embedded_user',
                            provider TEXT NOT NULL, -- 'openai', 'anthropic', 'google', 'groq', 'xai'
                            api_key TEXT NOT NULL, -- Encrypted value
                            is_active BOOLEAN NOT NULL DEFAULT 1,
                            created_at TEXT NOT NULL,
                            updated_at TEXT NOT NULL,
                            last_used_at TEXT,
                            PRIMARY KEY (user_id, provider)
                        );
                        CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);
                        CREATE INDEX IF NOT EXISTS idx_api_keys_active ON api_keys(user_id, is_active);

                        -- Users Table (for JWT authentication in embedded mode)
                        CREATE TABLE IF NOT EXISTS users (
                            user_id TEXT PRIMARY KEY,
                            username TEXT NOT NULL UNIQUE,
                            email TEXT,
                            created_at TEXT NOT NULL,
                            last_login_at TEXT
                        );
                        CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

                        -- Sessions Table (for conversation/chat sessions)
                        CREATE TABLE IF NOT EXISTS sessions (
                            session_id TEXT PRIMARY KEY,
                            user_id TEXT NOT NULL DEFAULT 'embedded_user',
                            title TEXT,
                            task_count INTEGER NOT NULL DEFAULT 0,
                            tokens_used INTEGER NOT NULL DEFAULT 0,
                            token_budget INTEGER,
                            context_json TEXT,
                            created_at TEXT NOT NULL,
                            updated_at TEXT NOT NULL,
                            last_activity_at TEXT
                        );
                        CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
                        CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at DESC);
                        "
                     )?;
                     *guard = Some(conn);
                 }
             }

             // --- USearch Logic ---
             {
                 let mut guard = this_index.lock().unwrap();
                 if guard.is_none() {
                    let options = IndexOptions {
                        dimensions: 1536,
                        metric: MetricKind::Cos,
                        quantization: ScalarKind::F32,
                        connectivity: 16,
                        expansion_add: 128,
                        expansion_search: 64,
                        multi: false,
                    };
                    let index = Index::new(&options)?;
                    if vector_path.exists() {
                        index.load(&vector_path.to_string_lossy())?;
                    }
                    *guard = Some(index);
                 }
             }
             Ok(())
        })
        .await
        .context("Tokio spawn_blocking failed")??;
        
        Ok(())
    }

    /// Get control state for a workflow.
    pub async fn get_control_state(&self, workflow_id: &str) -> Result<Option<ControlState>> {
        let workflow_id = workflow_id.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<Option<ControlState>> {
            let guard = sqlite.lock().unwrap();
            let conn = guard
                .as_ref()
                .ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;

            let mut stmt = conn.prepare(
                "SELECT workflow_id, is_paused, is_cancelled, paused_at, pause_reason, paused_by,
                        cancel_reason, cancelled_by, updated_at
                 FROM workflow_control_state WHERE workflow_id = ?1",
            )?;

            let mut rows = stmt.query(params![workflow_id])?;

            if let Some(row) = rows.next()? {
                Ok(Some(ControlState {
                    workflow_id: row.get(0)?,
                    is_paused: row.get(1)?,
                    is_cancelled: row.get(2)?,
                    paused_at: row.get::<_, Option<String>>(3)?.map(|s| parse_datetime(s)),
                    pause_reason: row.get(4)?,
                    paused_by: row.get(5)?,
                    cancel_reason: row.get(6)?,
                    cancelled_by: row.get(7)?,
                    updated_at: parse_datetime(row.get::<_, String>(8)?),
                }))
            } else {
                Ok(None)
            }
        })
        .await
        .context("Tokio spawn_blocking failed")?
    }

    /// Set control state for a workflow.
    pub async fn set_control_state(&self, state: &ControlState) -> Result<()> {
        let state = state.clone();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<()> {
            let guard = sqlite.lock().unwrap();
            let conn = guard
                .as_ref()
                .ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;

            conn.execute(
                "INSERT INTO workflow_control_state
                 (workflow_id, is_paused, is_cancelled, paused_at, pause_reason, paused_by,
                  cancel_reason, cancelled_by, updated_at)
                 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)
                 ON CONFLICT(workflow_id) DO UPDATE SET
                   is_paused = excluded.is_paused,
                   is_cancelled = excluded.is_cancelled,
                   paused_at = excluded.paused_at,
                   pause_reason = excluded.pause_reason,
                   paused_by = excluded.paused_by,
                   cancel_reason = excluded.cancel_reason,
                   cancelled_by = excluded.cancelled_by,
                   updated_at = excluded.updated_at",
                params![
                    state.workflow_id,
                    state.is_paused,
                    state.is_cancelled,
                    state.paused_at.map(|dt| dt.to_rfc3339()),
                    state.pause_reason,
                    state.paused_by,
                    state.cancel_reason,
                    state.cancelled_by,
                    state.updated_at.to_rfc3339()
                ],
            )?;
            Ok(())
        })
        .await
        .context("Tokio spawn_blocking failed")?
    }

    /// Update pause state for a workflow.
    pub async fn update_pause(
        &self,
        workflow_id: &str,
        paused: bool,
        reason: Option<String>,
        by: Option<String>,
    ) -> Result<()> {
        let workflow_id = workflow_id.to_string();
        let sqlite = self.sqlite.clone();
        let now = Utc::now();

        tokio::task::spawn_blocking(move || -> Result<()> {
            let guard = sqlite.lock().unwrap();
            let conn = guard
                .as_ref()
                .ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;

            if paused {
                conn.execute(
                    "INSERT INTO workflow_control_state
                     (workflow_id, is_paused, is_cancelled, paused_at, pause_reason, paused_by, updated_at)
                     VALUES (?1, ?2, 0, ?3, ?4, ?5, ?6)
                     ON CONFLICT(workflow_id) DO UPDATE SET
                       is_paused = excluded.is_paused,
                       paused_at = excluded.paused_at,
                       pause_reason = excluded.pause_reason,
                       paused_by = excluded.paused_by,
                       updated_at = excluded.updated_at",
                    params![
                        workflow_id,
                        paused,
                        now.to_rfc3339(),
                        reason,
                        by,
                        now.to_rfc3339()
                    ],
                )?;
            } else {
                conn.execute(
                    "INSERT INTO workflow_control_state
                     (workflow_id, is_paused, is_cancelled, updated_at)
                     VALUES (?1, ?2, 0, ?3)
                     ON CONFLICT(workflow_id) DO UPDATE SET
                       is_paused = excluded.is_paused,
                       paused_at = NULL,
                       pause_reason = NULL,
                       paused_by = NULL,
                       updated_at = excluded.updated_at",
                    params![workflow_id, paused, now.to_rfc3339()],
                )?;
            }
            Ok(())
        })
        .await
        .context("Tokio spawn_blocking failed")?
    }

    /// Update cancel state for a workflow.
    pub async fn update_cancel(
        &self,
        workflow_id: &str,
        cancelled: bool,
        reason: Option<String>,
        by: Option<String>,
    ) -> Result<()> {
        let workflow_id = workflow_id.to_string();
        let sqlite = self.sqlite.clone();
        let now = Utc::now();

        tokio::task::spawn_blocking(move || -> Result<()> {
            let guard = sqlite.lock().unwrap();
            let conn = guard
                .as_ref()
                .ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;

            conn.execute(
                "INSERT INTO workflow_control_state
                 (workflow_id, is_paused, is_cancelled, cancel_reason, cancelled_by, updated_at)
                 VALUES (?1, 0, ?2, ?3, ?4, ?5)
                 ON CONFLICT(workflow_id) DO UPDATE SET
                   is_cancelled = excluded.is_cancelled,
                   cancel_reason = excluded.cancel_reason,
                   cancelled_by = excluded.cancelled_by,
                   updated_at = excluded.updated_at",
                params![workflow_id, cancelled, reason, by, now.to_rfc3339()],
            )?;
            Ok(())
        })
        .await
        .context("Tokio spawn_blocking failed")?
    }
    
    /// Count total runs matching filters (for pagination).
    pub async fn count_runs(
        &self,
        user_id: &str,
        status_filter: Option<&str>,
        session_filter: Option<&str>,
    ) -> Result<usize> {
        let user_id = user_id.to_string();
        let status_filter = status_filter.map(String::from);
        let session_filter = session_filter.map(String::from);
        let sqlite = self.sqlite.clone();
        
        tokio::task::spawn_blocking(move || -> Result<usize> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            let mut sql = "SELECT COUNT(*) FROM runs WHERE user_id = ?1".to_string();
            let mut bind_idx = 2;
            
            if status_filter.is_some() {
                sql.push_str(&format!(" AND status = ?{}", bind_idx));
                bind_idx += 1;
            }
            
            if session_filter.is_some() {
                sql.push_str(&format!(" AND session_id = ?{}", bind_idx));
            }
            
            let mut stmt = conn.prepare(&sql)?;
            
            // Build params dynamically
            let count: i64 = match (status_filter.as_ref(), session_filter.as_ref()) {
                (Some(status), Some(session)) => {
                    stmt.query_row(params![user_id, status, session], |row| row.get(0))?
                }
                (Some(status), None) => {
                    stmt.query_row(params![user_id, status], |row| row.get(0))?
                }
                (None, Some(session)) => {
                    stmt.query_row(params![user_id, session], |row| row.get(0))?
                }
                (None, None) => {
                    stmt.query_row(params![user_id], |row| row.get(0))?
                }
            };
            
            Ok(count as usize)
        }).await?
    }
    
    /// List runs with filtering support (enhanced version).
    pub async fn list_runs_filtered(
        &self,
        user_id: &str,
        limit: usize,
        offset: usize,
        status_filter: Option<&str>,
        session_filter: Option<&str>,
    ) -> Result<Vec<Run>> {
        let user_id = user_id.to_string();
        let status_filter = status_filter.map(String::from);
        let session_filter = session_filter.map(String::from);
        let sqlite = self.sqlite.clone();
        
        tokio::task::spawn_blocking(move || -> Result<Vec<Run>> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            // Build SQL with dynamic filters
            let mut sql = "SELECT data FROM runs WHERE user_id = ?1".to_string();
            let mut params_vec: Vec<Box<dyn rusqlite::ToSql>> = vec![Box::new(user_id.clone())];
            
            if let Some(ref status) = status_filter {
                sql.push_str(&format!(" AND status = ?{}", params_vec.len() + 1));
                params_vec.push(Box::new(status.clone()));
            }
            
            if let Some(ref session) = session_filter {
                sql.push_str(&format!(" AND session_id = ?{}", params_vec.len() + 1));
                params_vec.push(Box::new(session.clone()));
            }
            
            sql.push_str(&format!(" ORDER BY created_at DESC LIMIT ?{} OFFSET ?{}",
                params_vec.len() + 1, params_vec.len() + 2));
            params_vec.push(Box::new(limit as i64));
            params_vec.push(Box::new(offset as i64));
            
            // Execute query
            let mut stmt = conn.prepare(&sql)?;
            let param_refs: Vec<&dyn rusqlite::ToSql> = params_vec.iter().map(|b| &**b as &dyn rusqlite::ToSql).collect();
            
            let rows = stmt.query_map(&param_refs[..], |row| {
                row.get::<_, String>(0)
            })?;

            let mut runs = Vec::new();
            for row_result in rows {
                let data = row_result?;
                if let Ok(run) = serde_json::from_str::<Run>(&data) {
                    runs.push(run);
                }
            }
            Ok(runs)
        }).await?
    }
}

// Helper struct for Search Result matching existing desktop trait, but here we implement MemoryRepository directly
#[derive(Debug)]
pub struct SearchResultInternal {
    pub id: String,
    pub score: f32,
}

#[cfg(any(feature = "embedded", feature = "embedded-mobile"))]
#[async_trait]
impl durable_shannon::EventLog for HybridBackend {
    async fn append(&self, workflow_id: &str, event: durable_shannon::Event) -> Result<u64> {
        let workflow_id = workflow_id.to_string();
        let event_type = event.event_type().to_string();
        let data = event.serialize()?;
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<u64> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            // Get next index
            let mut stmt = conn.prepare("SELECT COALESCE(MAX(event_index), -1) + 1 FROM workflow_events WHERE workflow_id = ?1")?;
            let next_idx: i64 = stmt.query_row(params![workflow_id], |row| row.get(0))?;
            
            conn.execute(
                "INSERT INTO workflow_events (workflow_id, event_index, event_type, data) VALUES (?1, ?2, ?3, ?4)",
                params![workflow_id, next_idx, event_type, data]
            )?;
            Ok(next_idx as u64)
        }).await?
    }

    async fn replay(&self, workflow_id: &str) -> Result<Vec<durable_shannon::Event>> {
        let workflow_id = workflow_id.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<Vec<durable_shannon::Event>> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            let mut stmt = conn.prepare(
                "SELECT data FROM workflow_events WHERE workflow_id = ?1 ORDER BY event_index ASC"
            )?;
            
            let rows = stmt.query_map(params![workflow_id], |row| {
                let data: Vec<u8> = row.get(0)?;
                Ok(data)
            })?;

            let mut events = Vec::new();
            for r in rows {
                let data = r?;
                let event = durable_shannon::Event::deserialize(&data)?;
                events.push(event);
            }
            Ok(events)
        }).await?
    }

    async fn next_index(&self, workflow_id: &str) -> Result<u64> {
        let workflow_id = workflow_id.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<u64> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            let mut stmt = conn.prepare("SELECT COUNT(*) FROM workflow_events WHERE workflow_id = ?1")?;
            let count: i64 = stmt.query_row(params![workflow_id], |row| row.get(0))?;
            Ok(count as u64)
        }).await?
    }

    async fn exists(&self, workflow_id: &str) -> Result<bool> {
        let workflow_id = workflow_id.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<bool> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            let mut stmt = conn.prepare("SELECT 1 FROM workflow_events WHERE workflow_id = ?1 LIMIT 1")?;
            Ok(stmt.exists(params![workflow_id])?)
        }).await?
    }

    async fn delete(&self, workflow_id: &str) -> Result<u64> {
        let workflow_id = workflow_id.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<u64> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            let count = conn.execute("DELETE FROM workflow_events WHERE workflow_id = ?1", params![workflow_id])?;
            Ok(count as u64)
        }).await?
    }

    async fn get_checkpoint(&self, workflow_id: &str) -> Result<Option<Vec<u8>>> {
        let workflow_id = workflow_id.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<Option<Vec<u8>>> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            // Find last checkpoint (inefficient scan if not optimized, but OK for now)
            // Ideally we'd have a separate column or table for checkpoints, or filter by event_type 'checkpoint'
            let mut stmt = conn.prepare(
                "SELECT data FROM workflow_events WHERE workflow_id = ?1 AND event_type = 'checkpoint' ORDER BY event_index DESC LIMIT 1"
            )?;
            
            let mut rows = stmt.query(params![workflow_id])?;
            if let Some(row) = rows.next()? {
                let data: Vec<u8> = row.get(0)?;
                // Need to deserialize event to get state? No, Event::Checkpoint { state }
                let event = durable_shannon::Event::deserialize(&data)?;
                if let durable_shannon::Event::Checkpoint { state } = event {
                    Ok(Some(state))
                } else {
                    Ok(None)
                }
            } else {
                Ok(None)
            }
        }).await?
    }

    async fn compact(&self, _workflow_id: &str) -> Result<u64> {
        // Basic compaction: Keep everything for now, or implement strict retention logic
        // This is a placeholder
        Ok(0)
    }
}

#[async_trait]
impl RunRepository for HybridBackend {
    async fn create_run(&self, run: &Run) -> Result<String> {
        let run_json = serde_json::to_string(run)?;
        let run_clone = run.clone();
        let sqlite = self.sqlite.clone();
        let created_at = format_datetime(run_clone.created_at);

        tokio::task::spawn_blocking(move || -> Result<String> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            conn.execute(
                "INSERT OR REPLACE INTO runs (id, user_id, session_id, status, created_at, data) VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
                params![run_clone.id, run_clone.user_id, run_clone.session_id, run_clone.status, created_at, run_json]
            )?;
            Ok(run_clone.id)
        }).await?
    }

    async fn get_run(&self, id: &str) -> Result<Option<Run>> {
        let id = id.to_string();
        let sqlite = self.sqlite.clone();
        
        tokio::task::spawn_blocking(move || -> Result<Option<Run>> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            let mut stmt = conn.prepare("SELECT data FROM runs WHERE id = ?1")?;
            let mut rows = stmt.query(params![id])?;
            
            if let Some(row) = rows.next()? {
                let data: String = row.get(0)?;
                let run: Run = serde_json::from_str(&data)?;
                Ok(Some(run))
            } else {
                Ok(None)
            }
        }).await?
    }

    async fn update_run(&self, run: &Run) -> Result<()> {
        let run_json = serde_json::to_string(run)?;
        let run_clone = run.clone();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<()> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            conn.execute(
                "UPDATE runs SET status = ?1, data = ?2 WHERE id = ?3",
                params![run_clone.status, run_json, run_clone.id]
            )?;
            Ok(())
        }).await?
    }

    async fn list_runs(&self, user_id: &str, limit: usize, offset: usize) -> Result<Vec<Run>> {
        let user_id = user_id.to_string();
        let sqlite = self.sqlite.clone();
        
        tokio::task::spawn_blocking(move || -> Result<Vec<Run>> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            let mut stmt = conn.prepare("SELECT data FROM runs WHERE user_id = ?1 ORDER BY created_at DESC LIMIT ?2 OFFSET ?3")?;
            let rows = stmt.query_map(params![user_id, limit as i64, offset as i64], |row| {
                let data: String = row.get(0)?;
                Ok(data)
            })?;

            let mut runs = Vec::new();
            for item in rows {
                let data = item?;
                if let Ok(run) = serde_json::from_str::<Run>(&data) {
                    runs.push(run);
                }
            }
            Ok(runs)
        }).await?
    }

    async fn delete_run(&self, id: &str) -> Result<bool> {
        let id = id.to_string();
        let sqlite = self.sqlite.clone();
        
        tokio::task::spawn_blocking(move || -> Result<bool> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            let count = conn.execute("DELETE FROM runs WHERE id = ?1", params![id])?;
            Ok(count > 0)
        }).await?
    }
}

#[async_trait]
impl MemoryRepository for HybridBackend {
    async fn store_memory(&self, memory: &Memory) -> Result<String> {
        let memory_clone = memory.clone();
        let sqlite = self.sqlite.clone();
        let index = self.index.clone();
        let vector_path = self.vector_path.clone();
        let created_at = format_datetime(memory_clone.created_at);

        // Calculate vector_id hash if embedding exists
        let vector_id = if memory.embedding.is_some() {
             use std::collections::hash_map::DefaultHasher;
             use std::hash::{Hash, Hasher};
             let mut hasher = DefaultHasher::new();
             memory.id.hash(&mut hasher);
             Some(hasher.finish())
        } else {
             None
        };

        tokio::task::spawn_blocking(move || -> Result<String> {
             // 1. SQLite
             {
                 let guard = sqlite.lock().unwrap();
                 let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
                 
                 let metadata_str = memory_clone.metadata.as_ref().map(|m| m.to_string());
                 
                 conn.execute(
                     "INSERT OR REPLACE INTO memories (id, conversation_id, vector_id, role, content, metadata, created_at) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)",
                     params![
                         memory_clone.id,
                         memory_clone.conversation_id,
                         vector_id.map(|v| v as i64),
                         memory_clone.role,
                         memory_clone.content,
                         metadata_str,
                         created_at
                     ]
                 )?;
             }

             // 2. USearch
             if let (Some(vid), Some(vec)) = (vector_id, &memory_clone.embedding) {
                 let guard = index.lock().unwrap();
                 if let Some(idx) = guard.as_ref() {
                     idx.add(vid, vec)?;
                     idx.save(&vector_path.to_string_lossy())?;
                 }
             }

             Ok(memory_clone.id)
        }).await?
    }

    async fn get_conversation(&self, conversation_id: &str, limit: usize) -> Result<Vec<Memory>> {
        let conversation_id = conversation_id.to_string();
        let sqlite = self.sqlite.clone();
        
        tokio::task::spawn_blocking(move || -> Result<Vec<Memory>> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            let mut stmt = conn.prepare(
                "SELECT id, conversation_id, role, content, metadata, created_at FROM memories WHERE conversation_id = ?1 ORDER BY created_at ASC LIMIT ?2"
            )?;
            
            let rows = stmt.query_map(params![conversation_id, limit as i64], |row| {
                let created_at = parse_datetime(row.get::<_, String>(5)?);
                Ok(Memory {
                    id: row.get(0)?,
                    conversation_id: row.get(1)?,
                    role: row.get(2)?,
                    content: row.get(3)?,
                    embedding: None, // We don't load embeddings on listing to save bandwidth
                    metadata: row.get::<_, Option<String>>(4)?.map(|s| serde_json::from_str(&s).unwrap_or(Value::Null)),
                    created_at,
                })
            })?;
            
            let mut memories = Vec::new();
            for r in rows {
                if let Ok(m) = r {
                    memories.push(m);
                }
            }
            Ok(memories)
        }).await?
    }

    async fn search_memories(&self, embedding: &[f32], limit: usize, _threshold: f32) -> Result<Vec<Memory>> {
        let query_vector = embedding.to_vec();
        let this_sqlite = self.sqlite.clone();
        let this_index = self.index.clone();

        tokio::task::spawn_blocking(move || -> Result<Vec<Memory>> {
            // 1. Search Vector Index
            let matches = {
                let guard = this_index.lock().unwrap();
                if let Some(index) = guard.as_ref() {
                    index.search(&query_vector, limit)?
                } else {
                    return Ok(vec![]);
                }
            };

            // 2. Hydrate from SQLite
            let mut results = Vec::new();
            let guard = this_sqlite.lock().unwrap();
            if let Some(conn) = guard.as_ref() {
                let mut stmt = conn.prepare(
                    "SELECT id, conversation_id, role, content, metadata, created_at FROM memories WHERE vector_id = ?1"
                )?;
                
                for key in matches.keys {
                    let vector_id = key as i64;
                    let mut rows = stmt.query(params![vector_id])?;
                    if let Some(row) = rows.next()? {
                        let created_at = parse_datetime(row.get::<_, String>(5)?);
                        results.push(Memory {
                            id: row.get(0)?,
                            conversation_id: row.get(1)?,
                            role: row.get(2)?,
                            content: row.get(3)?,
                            embedding: None,
                            metadata: row.get::<_, Option<String>>(4)?.map(|s| serde_json::from_str(&s).unwrap_or(Value::Null)),
                            created_at,
                        });
                    }
                }
            }
            Ok(results)
        }).await?
    }

    async fn delete_conversation(&self, conversation_id: &str) -> Result<u64> {
         let conversation_id = conversation_id.to_string();
         let sqlite = self.sqlite.clone();
         
         tokio::task::spawn_blocking(move || -> Result<u64> {
             let guard = sqlite.lock().unwrap();
             let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
             
             let count = conn.execute("DELETE FROM memories WHERE conversation_id = ?1", params![conversation_id])?;
             // Note: We don't delete from USearch as it doesn't support deletion efficiently yet
             // We can ignore orphaned vectors or rebuild index periodically.
             Ok(count as u64)
         }).await?
    }
}

#[async_trait]
impl crate::database::repository::SessionRepository for HybridBackend {
    async fn create_session(&self, session: &crate::database::repository::Session) -> Result<String> {
        let session_clone = session.clone();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<String> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            let context_json = session_clone.context.as_ref().map(|c| c.to_string());
            
            conn.execute(
                "INSERT INTO sessions
                 (session_id, user_id, title, task_count, tokens_used, token_budget, context_json, created_at, updated_at, last_activity_at)
                 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)",
                params![
                    session_clone.session_id,
                    session_clone.user_id,
                    session_clone.title,
                    session_clone.task_count,
                    session_clone.tokens_used,
                    session_clone.token_budget,
                    context_json,
                    format_datetime(session_clone.created_at),
                    format_datetime(session_clone.updated_at),
                    session_clone.last_activity_at.map(format_datetime),
                ]
            )?;
            Ok(session_clone.session_id)
        }).await?
    }

    async fn get_session(&self, session_id: &str) -> Result<Option<crate::database::repository::Session>> {
        let session_id = session_id.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<Option<crate::database::repository::Session>> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            let mut stmt = conn.prepare(
                "SELECT session_id, user_id, title, task_count, tokens_used, token_budget, context_json,
                        created_at, updated_at, last_activity_at
                 FROM sessions WHERE session_id = ?1"
            )?;
            
            let mut rows = stmt.query(params![session_id])?;
            
            if let Some(row) = rows.next()? {
                Ok(Some(crate::database::repository::Session {
                    session_id: row.get(0)?,
                    user_id: row.get(1)?,
                    title: row.get(2)?,
                    task_count: row.get(3)?,
                    tokens_used: row.get(4)?,
                    token_budget: row.get(5)?,
                    context: row.get::<_, Option<String>>(6)?.map(|s| serde_json::from_str(&s).unwrap_or(serde_json::Value::Null)),
                    created_at: parse_datetime(row.get::<_, String>(7)?),
                    updated_at: parse_datetime(row.get::<_, String>(8)?),
                    last_activity_at: row.get::<_, Option<String>>(9)?.map(parse_datetime),
                }))
            } else {
                Ok(None)
            }
        }).await?
    }

    async fn update_session(&self, session: &crate::database::repository::Session) -> Result<()> {
        let session_clone = session.clone();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<()> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            let context_json = session_clone.context.as_ref().map(|c| c.to_string());
            
            conn.execute(
                "UPDATE sessions SET
                    user_id = ?1, title = ?2, task_count = ?3, tokens_used = ?4,
                    token_budget = ?5, context_json = ?6, updated_at = ?7, last_activity_at = ?8
                 WHERE session_id = ?9",
                params![
                    session_clone.user_id,
                    session_clone.title,
                    session_clone.task_count,
                    session_clone.tokens_used,
                    session_clone.token_budget,
                    context_json,
                    format_datetime(session_clone.updated_at),
                    session_clone.last_activity_at.map(format_datetime),
                    session_clone.session_id,
                ]
            )?;
            Ok(())
        }).await?
    }

    async fn list_sessions(&self, user_id: &str, limit: usize, offset: usize) -> Result<Vec<crate::database::repository::Session>> {
        let user_id = user_id.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<Vec<crate::database::repository::Session>> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            let mut stmt = conn.prepare(
                "SELECT session_id, user_id, title, task_count, tokens_used, token_budget, context_json,
                        created_at, updated_at, last_activity_at
                 FROM sessions
                 WHERE user_id = ?1
                 ORDER BY updated_at DESC
                 LIMIT ?2 OFFSET ?3"
            )?;
            
            let rows = stmt.query_map(params![user_id, limit as i64, offset as i64], |row| {
                Ok(crate::database::repository::Session {
                    session_id: row.get(0)?,
                    user_id: row.get(1)?,
                    title: row.get(2)?,
                    task_count: row.get(3)?,
                    tokens_used: row.get(4)?,
                    token_budget: row.get(5)?,
                    context: row.get::<_, Option<String>>(6)?.map(|s| serde_json::from_str(&s).unwrap_or(serde_json::Value::Null)),
                    created_at: parse_datetime(row.get::<_, String>(7)?),
                    updated_at: parse_datetime(row.get::<_, String>(8)?),
                    last_activity_at: row.get::<_, Option<String>>(9)?.map(parse_datetime),
                })
            })?;
            
            let mut sessions = Vec::new();
            for row in rows {
                if let Ok(session) = row {
                    sessions.push(session);
                }
            }
            Ok(sessions)
        }).await?
    }

    async fn delete_session(&self, session_id: &str) -> Result<bool> {
        let session_id = session_id.to_string();
        let sqlite = self.sqlite.clone();

        tokio::task::spawn_blocking(move || -> Result<bool> {
            let guard = sqlite.lock().unwrap();
            let conn = guard.as_ref().ok_or_else(|| anyhow::anyhow!("SQLite not initialized"))?;
            
            let count = conn.execute("DELETE FROM sessions WHERE session_id = ?1", params![session_id])?;
            Ok(count > 0)
        }).await?
    }
}

fn format_datetime(value: DateTime<Utc>) -> String {
    value.to_rfc3339()
}

fn parse_datetime(value: String) -> DateTime<Utc> {
    DateTime::parse_from_rfc3339(&value)
        .map(|dt| dt.with_timezone(&Utc))
        .unwrap_or_else(|_| Utc::now())
}
