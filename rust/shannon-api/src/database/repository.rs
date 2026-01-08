//! Database repository implementations.
//!
//! Provides trait-based abstractions for data access that work across
//! different database backends (SurrealDB, PostgreSQL, SQLite).

use std::path::Path;

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

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
    async fn list_runs(&self, user_id: &str, limit: usize, offset: usize)
        -> anyhow::Result<Vec<Run>>;

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
    /// SurrealDB for desktop embedded mode.
    #[cfg(feature = "embedded")]
    SurrealDB(SurrealDBClient),
    /// PostgreSQL for cloud mode.
    #[cfg(feature = "database")]
    PostgreSQL(PostgreSQLClient),
    /// SQLite for mobile mode.
    #[cfg(feature = "embedded-mobile")]
    SQLite(SQLiteClient),
    /// In-memory store for testing.
    InMemory(InMemoryStore),
}

impl std::fmt::Debug for Database {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            #[cfg(feature = "embedded")]
            Self::SurrealDB(_) => write!(f, "Database::SurrealDB"),
            #[cfg(feature = "database")]
            Self::PostgreSQL(_) => write!(f, "Database::PostgreSQL"),
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(_) => write!(f, "Database::SQLite"),
            Self::InMemory(_) => write!(f, "Database::InMemory"),
        }
    }
}

impl Database {
    /// Create a database from configuration.
    pub async fn from_config(config: &DeploymentDatabaseConfig) -> anyhow::Result<Self> {
        match config {
            #[cfg(feature = "embedded")]
            DeploymentDatabaseConfig::SurrealDB {
                path,
                namespace,
                database,
            } => {
                let client = SurrealDBClient::new(path, namespace, database).await?;
                Ok(Self::SurrealDB(client))
            }
            #[cfg(feature = "database")]
            DeploymentDatabaseConfig::PostgreSQL {
                url,
                max_connections,
            } => {
                let client = PostgreSQLClient::new(url, *max_connections).await?;
                Ok(Self::PostgreSQL(client))
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
}

#[async_trait]
impl RunRepository for Database {
    async fn create_run(&self, run: &Run) -> anyhow::Result<String> {
        match self {
            #[cfg(feature = "embedded")]
            Self::SurrealDB(client) => client.create_run(run).await,
            #[cfg(feature = "database")]
            Self::PostgreSQL(client) => client.create_run(run).await,
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.create_run(run).await,
            Self::InMemory(store) => store.create_run(run).await,
        }
    }

    async fn get_run(&self, id: &str) -> anyhow::Result<Option<Run>> {
        match self {
            #[cfg(feature = "embedded")]
            Self::SurrealDB(client) => client.get_run(id).await,
            #[cfg(feature = "database")]
            Self::PostgreSQL(client) => client.get_run(id).await,
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.get_run(id).await,
            Self::InMemory(store) => store.get_run(id).await,
        }
    }

    async fn update_run(&self, run: &Run) -> anyhow::Result<()> {
        match self {
            #[cfg(feature = "embedded")]
            Self::SurrealDB(client) => client.update_run(run).await,
            #[cfg(feature = "database")]
            Self::PostgreSQL(client) => client.update_run(run).await,
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.update_run(run).await,
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
            #[cfg(feature = "embedded")]
            Self::SurrealDB(client) => client.list_runs(user_id, limit, offset).await,
            #[cfg(feature = "database")]
            Self::PostgreSQL(client) => client.list_runs(user_id, limit, offset).await,
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.list_runs(user_id, limit, offset).await,
            Self::InMemory(store) => store.list_runs(user_id, limit, offset).await,
        }
    }

    async fn delete_run(&self, id: &str) -> anyhow::Result<bool> {
        match self {
            #[cfg(feature = "embedded")]
            Self::SurrealDB(client) => client.delete_run(id).await,
            #[cfg(feature = "database")]
            Self::PostgreSQL(client) => client.delete_run(id).await,
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.delete_run(id).await,
            Self::InMemory(store) => store.delete_run(id).await,
        }
    }
}

#[async_trait]
impl MemoryRepository for Database {
    async fn store_memory(&self, memory: &Memory) -> anyhow::Result<String> {
        match self {
            #[cfg(feature = "embedded")]
            Self::SurrealDB(client) => client.store_memory(memory).await,
            #[cfg(feature = "database")]
            Self::PostgreSQL(client) => client.store_memory(memory).await,
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.store_memory(memory).await,
            Self::InMemory(store) => store.store_memory(memory).await,
        }
    }

    async fn get_conversation(
        &self,
        conversation_id: &str,
        limit: usize,
    ) -> anyhow::Result<Vec<Memory>> {
        match self {
            #[cfg(feature = "embedded")]
            Self::SurrealDB(client) => client.get_conversation(conversation_id, limit).await,
            #[cfg(feature = "database")]
            Self::PostgreSQL(client) => client.get_conversation(conversation_id, limit).await,
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.get_conversation(conversation_id, limit).await,
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
            #[cfg(feature = "embedded")]
            Self::SurrealDB(client) => client.search_memories(embedding, limit, threshold).await,
            #[cfg(feature = "database")]
            Self::PostgreSQL(client) => client.search_memories(embedding, limit, threshold).await,
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.search_memories(embedding, limit, threshold).await,
            Self::InMemory(store) => store.search_memories(embedding, limit, threshold).await,
        }
    }

    async fn delete_conversation(&self, conversation_id: &str) -> anyhow::Result<u64> {
        match self {
            #[cfg(feature = "embedded")]
            Self::SurrealDB(client) => client.delete_conversation(conversation_id).await,
            #[cfg(feature = "database")]
            Self::PostgreSQL(client) => client.delete_conversation(conversation_id).await,
            #[cfg(feature = "embedded-mobile")]
            Self::SQLite(client) => client.delete_conversation(conversation_id).await,
            Self::InMemory(store) => store.delete_conversation(conversation_id).await,
        }
    }
}

// ============================================================================
// SurrealDB Client (placeholder - requires surrealdb feature)
// ============================================================================

#[cfg(feature = "embedded")]
#[derive(Clone)]
pub struct SurrealDBClient {
    // db: surrealdb::Surreal<surrealdb::engine::local::Db>,
    _placeholder: std::marker::PhantomData<()>,
}

#[cfg(feature = "embedded")]
impl SurrealDBClient {
    pub async fn new(
        _path: &Path,
        _namespace: &str,
        _database: &str,
    ) -> anyhow::Result<Self> {
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
    async fn list_runs(&self, _user_id: &str, _limit: usize, _offset: usize) -> anyhow::Result<Vec<Run>> {
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
    async fn get_conversation(&self, _conversation_id: &str, _limit: usize) -> anyhow::Result<Vec<Memory>> {
        Ok(Vec::new())
    }
    async fn search_memories(&self, _embedding: &[f32], _limit: usize, _threshold: f32) -> anyhow::Result<Vec<Memory>> {
        Ok(Vec::new())
    }
    async fn delete_conversation(&self, _conversation_id: &str) -> anyhow::Result<u64> {
        Ok(0)
    }
}

// ============================================================================
// PostgreSQL Client (placeholder - requires database feature)
// ============================================================================

#[cfg(feature = "database")]
#[derive(Clone)]
pub struct PostgreSQLClient {
    // pool: sqlx::PgPool,
    _placeholder: std::marker::PhantomData<()>,
}

#[cfg(feature = "database")]
impl PostgreSQLClient {
    pub async fn new(_url: &str, _max_connections: u32) -> anyhow::Result<Self> {
        // TODO: Implement actual PostgreSQL connection
        Ok(Self {
            _placeholder: std::marker::PhantomData,
        })
    }
}

#[cfg(feature = "database")]
#[async_trait]
impl RunRepository for PostgreSQLClient {
    async fn create_run(&self, run: &Run) -> anyhow::Result<String> {
        Ok(run.id.clone())
    }
    async fn get_run(&self, _id: &str) -> anyhow::Result<Option<Run>> {
        Ok(None)
    }
    async fn update_run(&self, _run: &Run) -> anyhow::Result<()> {
        Ok(())
    }
    async fn list_runs(&self, _user_id: &str, _limit: usize, _offset: usize) -> anyhow::Result<Vec<Run>> {
        Ok(Vec::new())
    }
    async fn delete_run(&self, _id: &str) -> anyhow::Result<bool> {
        Ok(false)
    }
}

#[cfg(feature = "database")]
#[async_trait]
impl MemoryRepository for PostgreSQLClient {
    async fn store_memory(&self, memory: &Memory) -> anyhow::Result<String> {
        Ok(memory.id.clone())
    }
    async fn get_conversation(&self, _conversation_id: &str, _limit: usize) -> anyhow::Result<Vec<Memory>> {
        Ok(Vec::new())
    }
    async fn search_memories(&self, _embedding: &[f32], _limit: usize, _threshold: f32) -> anyhow::Result<Vec<Memory>> {
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
    async fn list_runs(&self, _user_id: &str, _limit: usize, _offset: usize) -> anyhow::Result<Vec<Run>> {
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
    async fn get_conversation(&self, _conversation_id: &str, _limit: usize) -> anyhow::Result<Vec<Memory>> {
        Ok(Vec::new())
    }
    async fn search_memories(&self, _embedding: &[f32], _limit: usize, _threshold: f32) -> anyhow::Result<Vec<Memory>> {
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
#[derive(Clone, Default)]
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
