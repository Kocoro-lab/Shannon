//! Database repository implementations.
//!
//! Provides trait-based abstractions for data access that work across
//! different database backends (SurrealDB, PostgreSQL, SQLite).

use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use std::path::Path;

use crate::config::deployment::DeploymentDatabaseConfig;
use crate::workflow::task::TokenUsage;

/// Run record stored in the database.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Run {
    /// Unique run identifier.
    pub id: String,
    /// User who created the run.
    pub user_id: String,
    /// Optional session ID.
    pub session_id: Option<String>,
    /// Original query.
    pub query: String,
    /// Current status.
    pub status: String,
    /// Execution strategy.
    pub strategy: String,
    /// Result content (JSON).
    pub result: Option<serde_json::Value>,
    /// Error message if failed.
    pub error: Option<String>,
    /// Token usage statistics.
    pub token_usage: Option<TokenUsage>,
    /// Creation timestamp.
    pub created_at: chrono::DateTime<chrono::Utc>,
    /// Last update timestamp.
    pub updated_at: chrono::DateTime<chrono::Utc>,
    /// Completion timestamp.
    pub completed_at: Option<chrono::DateTime<chrono::Utc>>,
}

/// Memory record for conversation history.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Memory {
    /// Unique memory identifier.
    pub id: String,
    /// Conversation/session ID.
    pub conversation_id: String,
    /// Message role (user, assistant, system).
    pub role: String,
    /// Message content.
    pub content: String,
    /// Optional embedding vector.
    pub embedding: Option<Vec<f32>>,
    /// Additional metadata.
    pub metadata: Option<serde_json::Value>,
    /// Creation timestamp.
    pub created_at: chrono::DateTime<chrono::Utc>,
}

/// Session record for conversation tracking.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Session {
    /// Unique session identifier.
    pub session_id: String,
    /// User who owns the session.
    pub user_id: String,
    /// Optional session title/name.
    pub title: Option<String>,
    /// Number of tasks in this session.
    pub task_count: i32,
    /// Total tokens used in session.
    pub tokens_used: i32,
    /// Optional token budget for session.
    pub token_budget: Option<i32>,
    /// Additional context as JSON.
    pub context: Option<serde_json::Value>,
    /// Creation timestamp.
    pub created_at: chrono::DateTime<chrono::Utc>,
    /// Last update timestamp.
    pub updated_at: chrono::DateTime<chrono::Utc>,
    /// Last activity timestamp.
    pub last_activity_at: Option<chrono::DateTime<chrono::Utc>>,
}

/// Repository trait for session operations.
#[async_trait]
pub trait SessionRepository: Send + Sync {
    /// Create a new session.
    async fn create_session(&self, session: &Session) -> anyhow::Result<String>;

    /// Get a session by ID.
    async fn get_session(&self, session_id: &str) -> anyhow::Result<Option<Session>>;

    /// Update an existing session.
    async fn update_session(&self, session: &Session) -> anyhow::Result<()>;

    /// List sessions for a user.
    async fn list_sessions(
        &self,
        user_id: &str,
        limit: usize,
        offset: usize,
    ) -> anyhow::Result<Vec<Session>>;

    /// Delete a session.
    async fn delete_session(&self, session_id: &str) -> anyhow::Result<bool>;
}

/// Repository trait for run operations.
#[async_trait]
pub trait RunRepository: Send + Sync {
    /// Create a new run.
    async fn create_run(&self, run: &Run) -> anyhow::Result<String>;

    /// Get a run by ID.
    async fn get_run(&self, id: &str) -> anyhow::Result<Option<Run>>;

    /// Update an existing run.
    async fn update_run(&self, run: &Run) -> anyhow::Result<()>;

    /// List runs for a user.
    async fn list_runs(
        &self,
        user_id: &str,
        limit: usize,
        offset: usize,
    ) -> anyhow::Result<Vec<Run>>;

    /// Delete a run.
    async fn delete_run(&self, id: &str) -> anyhow::Result<bool>;
}

/// Repository trait for memory operations.
#[async_trait]
pub trait MemoryRepository: Send + Sync {
    /// Store a memory.
    async fn store_memory(&self, memory: &Memory) -> anyhow::Result<String>;

    /// Get memories for a conversation.
    async fn get_conversation(
        &self,
        conversation_id: &str,
        limit: usize,
    ) -> anyhow::Result<Vec<Memory>>;

    /// Search memories by embedding similarity.
    async fn search_memories(
        &self,
        embedding: &[f32],
        limit: usize,
        threshold: f32,
    ) -> anyhow::Result<Vec<Memory>>;

    /// Delete memories for a conversation.
    async fn delete_conversation(&self, conversation_id: &str) -> anyhow::Result<u64>;
}

/// Database abstraction over different backends.
#[derive(Clone)]
pub enum Database {
    /// SQLite for mobile mode.
    #[cfg(feature = "embedded-mobile")]
    SQLite(SQLiteClient),
    /// Hybrid (SQLite + USearch) for desktop mode.
    #[cfg(feature = "usearch")]
    Hybrid(crate::database::hybrid::HybridBackend),
    /// In-memory store for testing.
    InMemory(InMemoryStore),
}

impl std::fmt::Debug for Database {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(_) => write!(f, "Database::SQLite"),
            #[cfg(feature = "usearch")]
            Self::Hybrid(_) => write!(f, "Database::Hybrid"),
            Self::InMemory(_) => write!(f, "Database::InMemory"),
        }
    }
}

impl Database {
    /// Create a database from configuration.
    pub async fn from_config(config: &DeploymentDatabaseConfig) -> anyhow::Result<Self> {
        match config {
            #[cfg(feature = "embedded")]
            DeploymentDatabaseConfig::Embedded { path } => {
                let db_path = std::path::PathBuf::from(path);
                // Default to Hybrid backend now
                let backend = crate::database::hybrid::HybridBackend::new(db_path);
                backend.init().await?;
                Ok(Self::Hybrid(backend))
            }
            #[cfg(feature = "embedded-mobile")]
            DeploymentDatabaseConfig::SQLite { path } => {
                let client = SQLiteClient::new(path).await?;
                Ok(Self::SQLite(client))
            }
            // Fallback to in-memory if features not enabled
            #[allow(unreachable_patterns)]
            _ => {
                tracing::warn!(
                    "Database feature not enabled for config {:?}, using in-memory store",
                    std::mem::discriminant(config)
                );
                Ok(Self::InMemory(InMemoryStore::new()))
            }
        }
    }

    /// Create an in-memory database for testing.
    #[must_use]
    pub fn in_memory() -> Self {
        Self::InMemory(InMemoryStore::new())
    }

    /// Get control state for a workflow (only available in Hybrid backend).
    pub async fn get_control_state(
        &self,
        workflow_id: &str,
    ) -> anyhow::Result<Option<crate::database::hybrid::ControlState>> {
        match self {
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.get_control_state(workflow_id).await,
            _ => {
                tracing::warn!("get_control_state only available with Hybrid backend");
                Ok(None)
            }
        }
    }

    /// Update pause state for a workflow (only available in Hybrid backend).
    pub async fn update_pause(
        &self,
        workflow_id: &str,
        paused: bool,
        reason: Option<String>,
        by: Option<String>,
    ) -> anyhow::Result<()> {
        match self {
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.update_pause(workflow_id, paused, reason, by).await,
            _ => {
                tracing::warn!("update_pause only available with Hybrid backend");
                Ok(())
            }
        }
    }

    /// Update cancel state for a workflow (only available in Hybrid backend).
    pub async fn update_cancel(
        &self,
        workflow_id: &str,
        cancelled: bool,
        reason: Option<String>,
        by: Option<String>,
    ) -> anyhow::Result<()> {
        match self {
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => {
                client
                    .update_cancel(workflow_id, cancelled, reason, by)
                    .await
            }
            _ => {
                tracing::warn!("update_cancel only available with Hybrid backend");
                Ok(())
            }
        }
    }
}

#[async_trait]
impl RunRepository for Database {
    async fn create_run(&self, run: &Run) -> anyhow::Result<String> {
        match self {
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.create_run(run).await,
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.create_run(run).await,
            Self::InMemory(store) => store.create_run(run).await,
        }
    }

    async fn get_run(&self, id: &str) -> anyhow::Result<Option<Run>> {
        match self {
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.get_run(id).await,
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.get_run(id).await,
            Self::InMemory(store) => store.get_run(id).await,
        }
    }

    async fn update_run(&self, run: &Run) -> anyhow::Result<()> {
        match self {
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.update_run(run).await,
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.update_run(run).await,
            Self::InMemory(store) => store.update_run(run).await,
        }
    }

    async fn list_runs(
        &self,
        user_id: &str,
        limit: usize,
        offset: usize,
    ) -> anyhow::Result<Vec<Run>> {
        match self {
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.list_runs(user_id, limit, offset).await,
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.list_runs(user_id, limit, offset).await,
            Self::InMemory(store) => store.list_runs(user_id, limit, offset).await,
        }
    }

    async fn delete_run(&self, id: &str) -> anyhow::Result<bool> {
        match self {
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.delete_run(id).await,
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.delete_run(id).await,
            Self::InMemory(store) => store.delete_run(id).await,
        }
    }
}

#[async_trait]
impl MemoryRepository for Database {
    async fn store_memory(&self, memory: &Memory) -> anyhow::Result<String> {
        match self {
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.store_memory(memory).await,
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.store_memory(memory).await,
            Self::InMemory(store) => store.store_memory(memory).await,
        }
    }

    async fn get_conversation(
        &self,
        conversation_id: &str,
        limit: usize,
    ) -> anyhow::Result<Vec<Memory>> {
        match self {
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.get_conversation(conversation_id, limit).await,
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.get_conversation(conversation_id, limit).await,
            Self::InMemory(store) => store.get_conversation(conversation_id, limit).await,
        }
    }

    async fn search_memories(
        &self,
        embedding: &[f32],
        limit: usize,
        threshold: f32,
    ) -> anyhow::Result<Vec<Memory>> {
        match self {
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.search_memories(embedding, limit, threshold).await,
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.search_memories(embedding, limit, threshold).await,
            Self::InMemory(store) => store.search_memories(embedding, limit, threshold).await,
        }
    }

    async fn delete_conversation(&self, conversation_id: &str) -> anyhow::Result<u64> {
        match self {
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.delete_conversation(conversation_id).await,
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.delete_conversation(conversation_id).await,
            Self::InMemory(store) => store.delete_conversation(conversation_id).await,
        }
    }
}

#[async_trait]
impl SessionRepository for Database {
    async fn create_session(&self, session: &Session) -> anyhow::Result<String> {
        match self {
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.create_session(session).await,
            _ => {
                tracing::warn!("create_session not supported in this database mode");
                Ok(session.session_id.clone())
            }
        }
    }

    async fn get_session(&self, session_id: &str) -> anyhow::Result<Option<Session>> {
        match self {
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.get_session(session_id).await,
            _ => {
                tracing::warn!("get_session not supported in this database mode");
                Ok(None)
            }
        }
    }

    async fn update_session(&self, session: &Session) -> anyhow::Result<()> {
        match self {
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.update_session(session).await,
            _ => {
                tracing::warn!("update_session not supported in this database mode");
                Ok(())
            }
        }
    }

    async fn list_sessions(
        &self,
        user_id: &str,
        limit: usize,
        offset: usize,
    ) -> anyhow::Result<Vec<Session>> {
        match self {
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.list_sessions(user_id, limit, offset).await,
            _ => {
                tracing::warn!("list_sessions not supported in this database mode");
                Ok(Vec::new())
            }
        }
    }

    async fn delete_session(&self, session_id: &str) -> anyhow::Result<bool> {
        match self {
            #[cfg(feature = "usearch")]
            Self::Hybrid(client) => client.delete_session(session_id).await,
            _ => {
                tracing::warn!("delete_session not supported in this database mode");
                Ok(false)
            }
        }
    }
}

// ============================================================================
// SurrealDB Client (placeholder - requires surrealdb feature)
// ============================================================================

#[cfg(feature = "embedded")]
#[derive(Debug, Clone)]
pub struct SurrealDBClient {
    // db: surrealdb::Surreal<surrealdb::engine::local::Db>,
    _placeholder: std::marker::PhantomData<()>,
}

#[cfg(feature = "embedded")]
impl SurrealDBClient {
    pub async fn new(_path: &Path, _namespace: &str, _database: &str) -> anyhow::Result<Self> {
        // TODO: Implement actual SurrealDB connection
        Ok(Self {
            _placeholder: std::marker::PhantomData,
        })
    }
}

#[cfg(feature = "embedded")]
#[async_trait]
impl RunRepository for SurrealDBClient {
    async fn create_run(&self, run: &Run) -> anyhow::Result<String> {
        Ok(run.id.clone())
    }
    async fn get_run(&self, _id: &str) -> anyhow::Result<Option<Run>> {
        Ok(None)
    }
    async fn update_run(&self, _run: &Run) -> anyhow::Result<()> {
        Ok(())
    }
    async fn list_runs(
        &self,
        _user_id: &str,
        _limit: usize,
        _offset: usize,
    ) -> anyhow::Result<Vec<Run>> {
        Ok(Vec::new())
    }
    async fn delete_run(&self, _id: &str) -> anyhow::Result<bool> {
        Ok(false)
    }
}

#[cfg(feature = "embedded")]
#[async_trait]
impl MemoryRepository for SurrealDBClient {
    async fn store_memory(&self, memory: &Memory) -> anyhow::Result<String> {
        Ok(memory.id.clone())
    }
    async fn get_conversation(
        &self,
        _conversation_id: &str,
        _limit: usize,
    ) -> anyhow::Result<Vec<Memory>> {
        Ok(Vec::new())
    }
    async fn search_memories(
        &self,
        _embedding: &[f32],
        _limit: usize,
        _threshold: f32,
    ) -> anyhow::Result<Vec<Memory>> {
        Ok(Vec::new())
    }
    async fn delete_conversation(&self, _conversation_id: &str) -> anyhow::Result<u64> {
        Ok(0)
    }
}

// ============================================================================
// SQLite Client (placeholder - requires embedded-mobile feature)
// ============================================================================

#[cfg(feature = "embedded-mobile")]
#[derive(Clone)]
pub struct SQLiteClient {
    _placeholder: std::marker::PhantomData<()>,
}

#[cfg(feature = "embedded-mobile")]
impl SQLiteClient {
    pub async fn new(_path: &Path) -> anyhow::Result<Self> {
        Ok(Self {
            _placeholder: std::marker::PhantomData,
        })
    }
}

#[cfg(feature = "embedded-mobile")]
#[async_trait]
impl RunRepository for SQLiteClient {
    async fn create_run(&self, run: &Run) -> anyhow::Result<String> {
        Ok(run.id.clone())
    }
    async fn get_run(&self, _id: &str) -> anyhow::Result<Option<Run>> {
        Ok(None)
    }
    async fn update_run(&self, _run: &Run) -> anyhow::Result<()> {
        Ok(())
    }
    async fn list_runs(
        &self,
        _user_id: &str,
        _limit: usize,
        _offset: usize,
    ) -> anyhow::Result<Vec<Run>> {
        Ok(Vec::new())
    }
    async fn delete_run(&self, _id: &str) -> anyhow::Result<bool> {
        Ok(false)
    }
}

#[cfg(feature = "embedded-mobile")]
#[async_trait]
impl MemoryRepository for SQLiteClient {
    async fn store_memory(&self, memory: &Memory) -> anyhow::Result<String> {
        Ok(memory.id.clone())
    }
    async fn get_conversation(
        &self,
        _conversation_id: &str,
        _limit: usize,
    ) -> anyhow::Result<Vec<Memory>> {
        Ok(Vec::new())
    }
    async fn search_memories(
        &self,
        _embedding: &[f32],
        _limit: usize,
        _threshold: f32,
    ) -> anyhow::Result<Vec<Memory>> {
        Ok(Vec::new())
    }
    async fn delete_conversation(&self, _conversation_id: &str) -> anyhow::Result<u64> {
        Ok(0)
    }
}

// ============================================================================
// In-Memory Store (for testing)
// ============================================================================

/// In-memory store for testing.
#[derive(Debug, Clone, Default)]
pub struct InMemoryStore {
    runs: std::sync::Arc<parking_lot::RwLock<std::collections::HashMap<String, Run>>>,
    memories: std::sync::Arc<parking_lot::RwLock<Vec<Memory>>>,
}

impl InMemoryStore {
    /// Create a new in-memory store.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl RunRepository for InMemoryStore {
    async fn create_run(&self, run: &Run) -> anyhow::Result<String> {
        let mut runs = self.runs.write();
        runs.insert(run.id.clone(), run.clone());
        Ok(run.id.clone())
    }

    async fn get_run(&self, id: &str) -> anyhow::Result<Option<Run>> {
        let runs = self.runs.read();
        Ok(runs.get(id).cloned())
    }

    async fn update_run(&self, run: &Run) -> anyhow::Result<()> {
        let mut runs = self.runs.write();
        runs.insert(run.id.clone(), run.clone());
        Ok(())
    }

    async fn list_runs(
        &self,
        user_id: &str,
        limit: usize,
        offset: usize,
    ) -> anyhow::Result<Vec<Run>> {
        let runs = self.runs.read();
        let filtered: Vec<_> = runs
            .values()
            .filter(|r| r.user_id == user_id)
            .skip(offset)
            .take(limit)
            .cloned()
            .collect();
        Ok(filtered)
    }

    async fn delete_run(&self, id: &str) -> anyhow::Result<bool> {
        let mut runs = self.runs.write();
        Ok(runs.remove(id).is_some())
    }
}

#[async_trait]
impl MemoryRepository for InMemoryStore {
    async fn store_memory(&self, memory: &Memory) -> anyhow::Result<String> {
        let mut memories = self.memories.write();
        memories.push(memory.clone());
        Ok(memory.id.clone())
    }

    async fn get_conversation(
        &self,
        conversation_id: &str,
        limit: usize,
    ) -> anyhow::Result<Vec<Memory>> {
        let memories = self.memories.read();
        let filtered: Vec<_> = memories
            .iter()
            .filter(|m| m.conversation_id == conversation_id)
            .take(limit)
            .cloned()
            .collect();
        Ok(filtered)
    }

    async fn search_memories(
        &self,
        _embedding: &[f32],
        limit: usize,
        _threshold: f32,
    ) -> anyhow::Result<Vec<Memory>> {
        // Simple implementation - return first N memories
        let memories = self.memories.read();
        Ok(memories.iter().take(limit).cloned().collect())
    }

    async fn delete_conversation(&self, conversation_id: &str) -> anyhow::Result<u64> {
        let mut memories = self.memories.write();
        let before = memories.len();
        memories.retain(|m| m.conversation_id != conversation_id);
        Ok((before - memories.len()) as u64)
    }
}
