//! Database schema definitions.
//!
//! Contains SurrealDB schema (SurrealQL) and common types for all backends.

/// SurrealDB schema for Shannon.
///
/// This schema is used for embedded/desktop mode with SurrealDB.
pub const SURREALDB_SCHEMA: &str = r#"
-- Runs table
DEFINE TABLE runs SCHEMAFULL;
DEFINE FIELD id ON runs TYPE string;
DEFINE FIELD user_id ON runs TYPE string;
DEFINE FIELD session_id ON runs TYPE option<string>;
DEFINE FIELD query ON runs TYPE string;
DEFINE FIELD status ON runs TYPE string;
DEFINE FIELD strategy ON runs TYPE string;
DEFINE FIELD result ON runs TYPE option<object>;
DEFINE FIELD error ON runs TYPE option<string>;
DEFINE FIELD token_usage ON runs TYPE option<object>;
DEFINE FIELD created_at ON runs TYPE datetime DEFAULT time::now();
DEFINE FIELD updated_at ON runs TYPE datetime DEFAULT time::now();
DEFINE FIELD completed_at ON runs TYPE option<datetime>;
DEFINE INDEX idx_user ON runs COLUMNS user_id;
DEFINE INDEX idx_status ON runs COLUMNS status;
DEFINE INDEX idx_session ON runs COLUMNS session_id;

-- Memories table (for RAG and conversation history)
DEFINE TABLE memories SCHEMAFULL;
DEFINE FIELD id ON memories TYPE string;
DEFINE FIELD conversation_id ON memories TYPE string;
DEFINE FIELD role ON memories TYPE string;
DEFINE FIELD content ON memories TYPE string;
DEFINE FIELD embedding ON memories TYPE option<array<float>>;
DEFINE FIELD metadata ON memories TYPE option<object>;
DEFINE FIELD created_at ON memories TYPE datetime DEFAULT time::now();
DEFINE INDEX idx_conversation ON memories COLUMNS conversation_id;

-- Workflow events (for Durable event sourcing)
DEFINE TABLE workflow_events SCHEMAFULL;
DEFINE FIELD workflow_id ON workflow_events TYPE string;
DEFINE FIELD event_idx ON workflow_events TYPE int;
DEFINE FIELD event_type ON workflow_events TYPE string;
DEFINE FIELD data ON workflow_events TYPE option<bytes>;
DEFINE FIELD created_at ON workflow_events TYPE datetime DEFAULT time::now();
DEFINE INDEX idx_workflow ON workflow_events COLUMNS workflow_id, event_idx UNIQUE;

-- Users table (multi-tenant)
DEFINE TABLE users SCHEMAFULL;
DEFINE FIELD id ON users TYPE string;
DEFINE FIELD email ON users TYPE string;
DEFINE FIELD name ON users TYPE option<string>;
DEFINE FIELD password_hash ON users TYPE option<string>;
DEFINE FIELD api_keys ON users TYPE option<array<object>>;
DEFINE FIELD settings ON users TYPE option<object>;
DEFINE FIELD created_at ON users TYPE datetime DEFAULT time::now();
DEFINE FIELD updated_at ON users TYPE datetime DEFAULT time::now();
DEFINE INDEX idx_email ON users COLUMNS email UNIQUE;

-- Sessions table
DEFINE TABLE sessions SCHEMAFULL;
DEFINE FIELD id ON sessions TYPE string;
DEFINE FIELD user_id ON sessions TYPE string;
DEFINE FIELD title ON sessions TYPE option<string>;
DEFINE FIELD context ON sessions TYPE option<object>;
DEFINE FIELD message_count ON sessions TYPE int DEFAULT 0;
DEFINE FIELD token_usage ON sessions TYPE option<object>;
DEFINE FIELD created_at ON sessions TYPE datetime DEFAULT time::now();
DEFINE FIELD updated_at ON sessions TYPE datetime DEFAULT time::now();
DEFINE INDEX idx_user_sessions ON sessions COLUMNS user_id;

-- Sync state table (for CRDT sync)
DEFINE TABLE sync_state SCHEMAFULL;
DEFINE FIELD device_id ON sync_state TYPE string;
DEFINE FIELD last_sync_at ON sync_state TYPE datetime;
DEFINE FIELD state_vector ON sync_state TYPE bytes;
DEFINE FIELD created_at ON sync_state TYPE datetime DEFAULT time::now();
DEFINE INDEX idx_device ON sync_state COLUMNS device_id UNIQUE;
"#;

/// SQLite schema for mobile mode.
pub const SQLITE_SCHEMA: &str = r#"
-- Runs table
CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    session_id TEXT,
    query TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    strategy TEXT NOT NULL DEFAULT 'standard',
    result TEXT,
    error TEXT,
    token_usage TEXT,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now')),
    completed_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_runs_user ON runs(user_id);
CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);
CREATE INDEX IF NOT EXISTS idx_runs_session ON runs(session_id);

-- Memories table
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    embedding BLOB,
    metadata TEXT,
    created_at TEXT DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_memories_conversation ON memories(conversation_id);

-- Workflow events table
CREATE TABLE IF NOT EXISTS workflow_events (
    workflow_id TEXT NOT NULL,
    event_idx INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    data BLOB,
    created_at TEXT DEFAULT (datetime('now')),
    PRIMARY KEY (workflow_id, event_idx)
);

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    name TEXT,
    password_hash TEXT,
    api_keys TEXT,
    settings TEXT,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

-- Sessions table
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    title TEXT,
    context TEXT,
    message_count INTEGER DEFAULT 0,
    token_usage TEXT,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

-- Sync state table
CREATE TABLE IF NOT EXISTS sync_state (
    device_id TEXT PRIMARY KEY,
    last_sync_at TEXT,
    state_vector BLOB,
    created_at TEXT DEFAULT (datetime('now'))
);
"#;
