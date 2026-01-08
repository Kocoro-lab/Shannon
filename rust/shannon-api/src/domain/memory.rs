//! Conversation memory and context management.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::llm::Message;

/// A conversation session.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Session {
    /// Unique session identifier.
    pub id: String,
    /// User ID this session belongs to.
    pub user_id: Option<String>,
    /// Session title.
    pub title: Option<String>,
    /// Messages in this session.
    pub messages: Vec<Message>,
    /// Total tokens used in this session.
    pub total_tokens: u32,
    /// When the session was created.
    pub created_at: DateTime<Utc>,
    /// When the session was last updated.
    pub updated_at: DateTime<Utc>,
}

impl Session {
    /// Create a new session.
    pub fn new() -> Self {
        let now = Utc::now();
        Self {
            id: Uuid::new_v4().to_string(),
            user_id: None,
            title: None,
            messages: Vec::new(),
            total_tokens: 0,
            created_at: now,
            updated_at: now,
        }
    }

    /// Create a session with a specific ID.
    pub fn with_id(id: impl Into<String>) -> Self {
        let now = Utc::now();
        Self {
            id: id.into(),
            user_id: None,
            title: None,
            messages: Vec::new(),
            total_tokens: 0,
            created_at: now,
            updated_at: now,
        }
    }

    /// Add a message to the session.
    pub fn add_message(&mut self, message: Message) {
        self.messages.push(message);
        self.updated_at = Utc::now();
    }

    /// Update token count.
    pub fn add_tokens(&mut self, tokens: u32) {
        self.total_tokens += tokens;
        self.updated_at = Utc::now();
    }

    /// Get the last N messages.
    pub fn last_messages(&self, n: usize) -> &[Message] {
        let start = self.messages.len().saturating_sub(n);
        &self.messages[start..]
    }
}

impl Default for Session {
    fn default() -> Self {
        Self::new()
    }
}

/// Memory entry for long-term storage.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MemoryEntry {
    /// Unique memory identifier.
    pub id: String,
    /// Session ID this memory belongs to.
    pub session_id: Option<String>,
    /// Memory content.
    pub content: String,
    /// Memory type.
    pub memory_type: MemoryType,
    /// Importance score (0.0 to 1.0).
    pub importance: f32,
    /// Embedding vector (if computed).
    pub embedding: Option<Vec<f32>>,
    /// When the memory was created.
    pub created_at: DateTime<Utc>,
    /// When the memory was last accessed.
    pub last_accessed_at: DateTime<Utc>,
    /// Access count.
    pub access_count: u32,
}

impl MemoryEntry {
    /// Create a new memory entry.
    pub fn new(content: impl Into<String>, memory_type: MemoryType) -> Self {
        let now = Utc::now();
        Self {
            id: Uuid::new_v4().to_string(),
            session_id: None,
            content: content.into(),
            memory_type,
            importance: 0.5,
            embedding: None,
            created_at: now,
            last_accessed_at: now,
            access_count: 0,
        }
    }

    /// Mark as accessed.
    pub fn access(&mut self) {
        self.last_accessed_at = Utc::now();
        self.access_count += 1;
    }
}

/// Type of memory entry.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum MemoryType {
    /// Factual information.
    Fact,
    /// User preference.
    Preference,
    /// Conversation summary.
    Summary,
    /// Task or action item.
    Task,
    /// General context.
    Context,
}
