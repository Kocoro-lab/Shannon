# Comprehensive Analysis: Embedded Vector Databases for Tauri

## Executive Summary

After extensive research, **SQLite + sqlite-vec is the recommended solution** for both desktop and mobile Tauri applications. This provides:
- ✅ Truly embedded (in-process, no separate server)
- ✅ Cross-platform (macOS, Windows, Linux, iOS, Android)
- ✅ Small binary size (<1MB overhead)
- ✅ Proven reliability (trillion+ deployments)
- ✅ Vector search via sqlite-vec extension
- ✅ Single codebase for desktop and mobile
- ✅ Official Tauri plugin support (tauri-plugin-sql)

---

## Detailed Options Analysis

### Option 1: SQLite + sqlite-vec ⭐ RECOMMENDED

**What it is**: SQLite with sqlite-vec extension for vector similarity search

**Key Facts**:
- **Size**: ~1MB additional overhead
- **Performance**: "Fast enough" vector search, pure C implementation
- **Extension**: sqlite-vec (successor to sqlite-vss)
- **Maturity**: SQLite = battle-tested (1 trillion+ deployments), sqlite-vec = stable v0.1.0+
- **Rust Support**: Excellent via `rusqlite` + `sqlite-vec` crate

#### Advantages ✅
1. **Truly Embedded**: Runs in-process, no separate database server
2. **Universal**: Same code works on desktop AND mobile
3. **Zero Dependencies**: No external libraries or installations needed
4. **Minimal Size**: Entire database + vector extension < 1MB
5. **Official Support**: `tauri-plugin-sql` provides first-class SQLite support
6. **Reliability**: SQLite is the most deployed database in the world
7. **Simple Deployment**: Single file database, easy backup/sync

#### Disadvantages ⚠️
1. **Not Postgres**: Different SQL dialect (but 99% compatible for basic operations)
2. **Single Writer**: Only one write transaction at a time (rarely an issue for desktop apps)
3. **Limited Concurrency**: Simpler concurrency model than Postgres

#### Implementation Example

```rust
// Cargo.toml
[dependencies]
rusqlite = { version = "0.32", features = ["bundled"] }
sqlite-vec = "0.1"
tauri = { version = "2.x", features = [] }

// src-tauri/src/lib.rs
use rusqlite::{Connection, Result};
use sqlite_vec::load_vec;

fn init_database() -> Result<Connection> {
    let conn = Connection::open("shannon.db")?;
    
    // Load sqlite-vec extension
    load_vec(&conn)?;
    
    // Create table with vector column
    conn.execute(
        "CREATE TABLE IF NOT EXISTS embeddings (
            id INTEGER PRIMARY KEY,
            content TEXT,
            vector BLOB
        )",
        [],
    )?;
    
    // Create vector index
    conn.execute(
        "CREATE VIRTUAL TABLE vec_index USING vec0(
            embedding FLOAT[384]
        )",
        [],
    )?;
    
    Ok(conn)
}

// Vector search query
fn search_similar(conn: &Connection, query_vector: &[f32], limit: usize) -> Result<Vec<i64>> {
    let mut stmt = conn.prepare(
        "SELECT id FROM embeddings
         ORDER BY vec_distance_cosine(vector, ?) 
         LIMIT ?"
    )?;
    
    let ids = stmt.query_map([query_vector, &[limit as f32]], |row| {
        row.get(0)
    })?;
    
    ids.collect()
}
```

#### Mobile Compatibility
```rust
// Works identically on iOS and Android with Tauri Mobile
#[cfg(mobile)]
use tauri::plugin::mobile::PluginInvokeContext;

// Database location automatically handled by Tauri
let db_path = app.path_resolver()
    .app_data_dir()
    .unwrap()
    .join("shannon.db");
```

---

### Option 2: postgresql_embedded + pgvector

**What it is**: Native Postgres binaries bundled and managed by Rust wrapper

**Key Facts**:
- **Size**: ~30-50MB additional overhead
- **Performance**: Full Postgres performance
- **Extension**: pgvector (industry standard)
- **Maturity**: Postgres = extremely mature, postgresql_embedded = proven
- **Rust Support**: Good via `postgresql_embedded` + `tokio-postgres`

#### Advantages ✅
1. **Full Postgres**: All Postgres features and extensions
2. **pgvector**: Industry-standard vector extension
3. **Familiar**: Same database as your server/cloud deployment
4. **ACID Compliance**: Full transactional guarantees
5. **Proven in Production**: ElectricSQL's Tauri demo used this approach

#### Disadvantages ⚠️
1. **Not Truly Embedded**: Runs as separate process (via fork/exec)
2. **Large Binary**: 30-50MB of Postgres binaries bundled
3. **Complex Setup**: More moving parts, process management required
4. **Not Suitable for Mobile**: Too heavy for iOS/Android
5. **Platform-Specific Binaries**: Need different builds for each OS
6. **Startup Time**: Process spawn adds latency

#### Implementation Example

```rust
// Cargo.toml
[dependencies]
postgresql_embedded = { version = "0.19", features = ["bundled"] }
tokio-postgres = "0.7"
tokio = { version = "1", features = ["full"] }

// src-tauri/src/lib.rs
use postgresql_embedded::{PostgreSQL, Settings};
use tokio_postgres::NoTls;

async fn init_postgres() -> Result<(), Box<dyn std::error::Error>> {
    // Configure embedded Postgres
    let settings = Settings {
        // Customize installation directory
        installation_dir: PathBuf::from("./postgres"),
        ..Default::default()
    };
    
    let mut postgresql = PostgreSQL::new(settings);
    
    // Download and install Postgres (first run only)
    postgresql.setup().await?;
    
    // Start Postgres server
    postgresql.start().await?;
    
    // Get connection URL
    let database_url = postgresql.settings().url("shannon");
    
    // Connect and setup
    let (client, connection) = tokio_postgres::connect(&database_url, NoTls).await?;
    
    // Spawn connection handler
    tokio::spawn(async move {
        if let Err(e) = connection.await {
            eprintln!("connection error: {}", e);
        }
    });
    
    // Install pgvector extension
    client.execute("CREATE EXTENSION IF NOT EXISTS vector", &[]).await?;
    
    // Create table with vector column
    client.execute(
        "CREATE TABLE IF NOT EXISTS embeddings (
            id SERIAL PRIMARY KEY,
            content TEXT,
            embedding vector(384)
        )",
        &[],
    ).await?;
    
    Ok(())
}

// CRITICAL: Must stop Postgres on app shutdown
async fn shutdown_postgres(postgresql: &mut PostgreSQL) -> Result<(), Box<dyn std::error::Error>> {
    postgresql.stop().await?;
    Ok(())
}
```

#### Platform-Specific Considerations

**Desktop**:
```rust
// Works on macOS, Windows, Linux
// Each platform needs its own Postgres binaries (handled by crate)
#[cfg(target_os = "macos")]
const POSTGRES_ARCH: &str = "x86_64-apple-darwin";

#[cfg(target_os = "windows")]
const POSTGRES_ARCH: &str = "x86_64-pc-windows-msvc";

#[cfg(target_os = "linux")]
const POSTGRES_ARCH: &str = "x86_64-unknown-linux-gnu";
```

**Mobile**:
```rust
// NOT RECOMMENDED FOR MOBILE
// Binary size and process management make this impractical
#[cfg(mobile)]
compile_error!("Use SQLite for mobile platforms instead");
```

---

### Option 3: PGlite (WASM Postgres) 🚧 Future Option

**What it is**: PostgreSQL compiled to WebAssembly, packaged as TypeScript library

**Key Facts**:
- **Size**: 3MB gzipped
- **Performance**: Good for most use cases
- **Extension**: pgvector included
- **Maturity**: Stable, 4M+ weekly downloads
- **Rust Support**: ❌ Not yet available (as of Jan 2025)

#### Status
- **Desktop**: Cannot use in Tauri Rust backend (TypeScript only)
- **Mobile**: Cannot use in Tauri Rust backend
- **Future**: [Issue #470](https://github.com/electric-sql/pglite/issues/470) tracks Rust bindings request

#### If/When Rust Bindings Available

```rust
// HYPOTHETICAL - Not yet possible
// Cargo.toml
[dependencies]
pglite-rs = "0.1" // Does not exist yet

// Would offer best of both worlds:
// ✅ Postgres compatibility
// ✅ Embedded (WASM, no separate process)
// ✅ Small size (3MB)
// ✅ pgvector support
```

**Monitor**: Watch [electric-sql/pglite#470](https://github.com/electric-sql/pglite/issues/470) for Rust binding updates

---

### Option 4: DuckDB + VSS Extension

**What it is**: Modern OLAP database with vector similarity search extension

**Key Facts**:
- **Size**: ~10MB
- **Performance**: Excellent for analytics
- **Extension**: Built-in VSS (HNSW indexes)
- **Maturity**: DuckDB = mature, VSS = experimental
- **Rust Support**: Excellent via `duckdb` crate

#### Advantages ✅
1. **In-Process**: Truly embedded like SQLite
2. **Modern**: Designed for 2020s workloads
3. **Analytics-First**: Excellent for OLAP queries
4. **VSS Built-in**: HNSW indexes for fast vector search
5. **Rust-Friendly**: Great FFI bindings

#### Disadvantages ⚠️
1. **OLAP Focus**: Optimized for analytics, not transactional workloads
2. **Experimental Persistence**: VSS index persistence still experimental
3. **Larger Size**: ~10MB vs SQLite's <1MB
4. **Less Mobile-Friendly**: Not as optimized for constrained environments

#### When to Consider
- Your app is analytics-heavy
- You're doing lots of aggregations and joins
- You can tolerate experimental vector persistence

#### Implementation Example

```rust
// Cargo.toml
[dependencies]
duckdb = { version = "1.1", features = ["bundled"] }

// src-tauri/src/lib.rs
use duckdb::{Connection, Result};

fn init_duckdb() -> Result<Connection> {
    let conn = Connection::open("shannon.duckdb")?;
    
    // Install VSS extension
    conn.execute("INSTALL vss;", [])?;
    conn.execute("LOAD vss;", [])?;
    
    // Enable experimental persistence
    conn.execute("SET GLOBAL hnsw_enable_experimental_persistence = true;", [])?;
    
    // Create table with array column for vectors
    conn.execute(
        "CREATE TABLE embeddings (
            id INTEGER PRIMARY KEY,
            content TEXT,
            embedding FLOAT[384]
        )",
        [],
    )?;
    
    // Create HNSW index
    conn.execute(
        "CREATE INDEX vec_idx ON embeddings 
         USING HNSW (embedding) 
         WITH (metric = 'cosine')",
        [],
    )?;
    
    Ok(conn)
}
```

---

## Detailed Comparison Matrix

| Feature | SQLite + sqlite-vec | postgresql_embedded | PGlite | DuckDB + VSS |
|---------|-------------------|-------------------|--------|--------------|
| **Truly Embedded** | ✅ Yes (in-process) | ❌ No (separate process) | ✅ Yes (WASM) | ✅ Yes (in-process) |
| **Binary Size** | ~1MB | ~30-50MB | ~3MB | ~10MB |
| **Desktop Support** | ✅ Excellent | ✅ Good | ❌ No Rust bindings | ✅ Good |
| **Mobile Support** | ✅ Excellent | ❌ Not suitable | ❌ No Rust bindings | ⚠️ Possible but heavy |
| **Vector Search** | ✅ sqlite-vec | ✅ pgvector | ✅ pgvector | ✅ VSS (HNSW) |
| **Maturity** | ✅ Both very mature | ✅ Very mature | ✅ Mature (4M+ downloads) | ⚠️ VSS experimental |
| **Rust Support** | ✅ Excellent | ✅ Good | ❌ Not available | ✅ Excellent |
| **Setup Complexity** | ✅ Very simple | ⚠️ Complex | N/A | ✅ Simple |
| **Postgres Compatible** | ❌ No | ✅ Yes | ✅ Yes | ❌ No |
| **Tauri Plugin** | ✅ Official | ❌ No | ❌ No | ❌ No |
| **Single File DB** | ✅ Yes | ❌ No (pgdata dir) | ✅ Yes | ✅ Yes |
| **ACID Guarantees** | ✅ Full | ✅ Full | ✅ Full | ✅ Full |
| **Concurrent Writes** | ⚠️ Single writer | ✅ Multiple | ✅ Single user | ⚠️ Single writer |

---

## Real-World Production Examples

### ElectricSQL + Tauri (Proven Architecture)

**Project**: [electric-tauri-postgres](https://github.com/electric-sql/electric-tauri-postgres)

**Stack**:
- Desktop: `postgresql_embedded` + `pgvector`
- Sync: ElectricSQL for cloud sync
- Frontend: React + TypeScript

**Lessons Learned**:
1. Process management is critical (startup/shutdown)
2. Platform-specific binaries increase complexity
3. Works well but adds 30-50MB to app size
4. Not feasible for mobile

**Quote from ElectricSQL Blog**:
> "We took Postgres, bundled it with pgvector and compiled it to run cross platform inside the Rust backend of a Tauri app."

### Best Practice: SQLite + sqlite-vec

**Why Top Teams Choose SQLite**:
1. **Simplicity**: Single file, no process management
2. **Reliability**: Most deployed database in the world
3. **Performance**: Fast enough for 99% of use cases
4. **Mobile-First**: Same code works everywhere

**sqlite-vec Advantages**:
- Pure C, zero dependencies
- ~500KB compiled size
- HNSW-inspired algorithm
- Actively maintained (sponsored by Mozilla, Fly.io, Turso)

---

## Recommended Implementation Strategy

### Phase 1: Start with SQLite + sqlite-vec

```rust
// src-tauri/Cargo.toml
[dependencies]
tauri = { version = "2", features = ["desktop", "mobile"] }
rusqlite = { version = "0.32", features = ["bundled"] }
sqlite-vec = "0.1"
serde = { version = "1.0", features = ["derive"] }
serde_json = "1.0"

// src-tauri/src/database/mod.rs
pub struct Database {
    conn: rusqlite::Connection,
}

impl Database {
    pub fn new(db_path: &Path) -> Result<Self> {
        let conn = rusqlite::Connection::open(db_path)?;
        
        // Load sqlite-vec extension
        sqlite_vec::load_vec(&conn)?;
        
        // Initialize schema
        Self::init_schema(&conn)?;
        
        Ok(Self { conn })
    }
    
    fn init_schema(conn: &Connection) -> Result<()> {
        conn.execute_batch(
            "CREATE TABLE IF NOT EXISTS documents (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                content TEXT NOT NULL,
                metadata TEXT,
                created_at INTEGER NOT NULL DEFAULT (unixepoch())
            );
            
            CREATE VIRTUAL TABLE IF NOT EXISTS vec_documents USING vec0(
                document_id INTEGER,
                embedding FLOAT[384]
            );"
        )?;
        Ok(())
    }
    
    pub fn insert_document(&self, content: &str, embedding: &[f32]) -> Result<i64> {
        let id = self.conn.query_row(
            "INSERT INTO documents (content) VALUES (?) RETURNING id",
            [content],
            |row| row.get(0)
        )?;
        
        self.conn.execute(
            "INSERT INTO vec_documents (document_id, embedding) VALUES (?, ?)",
            rusqlite::params![id, embedding],
        )?;
        
        Ok(id)
    }
    
    pub fn search_similar(&self, query_embedding: &[f32], limit: usize) -> Result<Vec<(i64, f32)>> {
        let mut stmt = self.conn.prepare(
            "SELECT v.document_id, v.distance
             FROM vec_documents v
             WHERE v.embedding MATCH ?
             ORDER BY v.distance
             LIMIT ?"
        )?;
        
        let results = stmt.query_map(
            rusqlite::params![query_embedding, limit],
            |row| Ok((row.get(0)?, row.get(1)?))
        )?;
        
        results.collect()
    }
}
```

### Phase 2: Add Tauri Commands

```rust
// src-tauri/src/commands.rs
use tauri::State;

#[tauri::command]
async fn add_document(
    db: State<'_, Database>,
    content: String,
    embedding: Vec<f32>,
) -> Result<i64, String> {
    db.insert_document(&content, &embedding)
        .map_err(|e| e.to_string())
}

#[tauri::command]
async fn search_documents(
    db: State<'_, Database>,
    query_embedding: Vec<f32>,
    limit: usize,
) -> Result<Vec<(i64, f32)>, String> {
    db.search_similar(&query_embedding, limit)
        .map_err(|e| e.to_string())
}
```

### Phase 3: Initialize in Tauri

```rust
// src-tauri/src/lib.rs
#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .setup(|app| {
            let app_data_dir = app.path().app_data_dir()?;
            std::fs::create_dir_all(&app_data_dir)?;
            
            let db_path = app_data_dir.join("shannon.db");
            let db = Database::new(&db_path)?;
            
            app.manage(db);
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            add_document,
            search_documents
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
```

---

## Migration Path from SurrealDB

### Step 1: Schema Mapping

```sql
-- SurrealDB schema
DEFINE TABLE documents SCHEMAFULL;
DEFINE FIELD content ON documents TYPE string;
DEFINE FIELD embedding ON documents TYPE array;

-- SQLite equivalent
CREATE TABLE documents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    content TEXT NOT NULL,
    metadata TEXT  -- Store JSON for flexible fields
);

CREATE VIRTUAL TABLE vec_documents USING vec0(
    document_id INTEGER,
    embedding FLOAT[384]
);
```

### Step 2: Data Export/Import

```rust
// Export from SurrealDB
async fn export_from_surrealdb(surreal_conn: &Surreal<Db>) -> Result<Vec<Document>> {
    let docs: Vec<Document> = surreal_conn
        .select("documents")
        .await?;
    Ok(docs)
}

// Import to SQLite
fn import_to_sqlite(sqlite_conn: &Connection, docs: Vec<Document>) -> Result<()> {
    let tx = sqlite_conn.transaction()?;
    
    for doc in docs {
        let id: i64 = tx.query_row(
            "INSERT INTO documents (content, metadata) VALUES (?, ?) RETURNING id",
            rusqlite::params![doc.content, serde_json::to_string(&doc.metadata)?],
            |row| row.get(0)
        )?;
        
        tx.execute(
            "INSERT INTO vec_documents (document_id, embedding) VALUES (?, ?)",
            rusqlite::params![id, doc.embedding],
        )?;
    }
    
    tx.commit()?;
    Ok(())
}
```

---

## Final Recommendation

### For Your Shannon Project

**Use SQLite + sqlite-vec for both desktop and mobile**

#### Why This Choice:
1. ✅ **Proven Reliability**: SQLite has trillion+ deployments
2. ✅ **Single Codebase**: Same database logic for desktop and mobile
3. ✅ **Small Binary**: <1MB overhead vs SurrealDB's complexity
4. ✅ **No Process Management**: Truly embedded, no separate server
5. ✅ **Official Tauri Support**: `tauri-plugin-sql` provides seamless integration
6. ✅ **Vector Search**: sqlite-vec is stable, fast, and actively maintained
7. ✅ **Simple Deployment**: Single file database, easy backup/restore
8. ✅ **Mobile-Ready**: Works perfectly on iOS and Android

#### Trade-offs Accepted:
- ⚠️ Not Postgres (but SQL is 99% compatible)
- ⚠️ Single writer (rarely an issue for desktop/mobile apps)
- ⚠️ Less "fancy" than SurrealDB (but that's a feature, not a bug)

#### Implementation Timeline:
- **Week 1**: Replace SurrealDB with SQLite + sqlite-vec
- **Week 2**: Test on macOS, Windows, Linux
- **Week 3**: Test mobile builds (iOS/Android)
- **Week 4**: Production deployment

---

## Additional Resources

### Documentation
- **SQLite**: https://www.sqlite.org/docs.html
- **sqlite-vec**: https://github.com/asg017/sqlite-vec
- **rusqlite**: https://docs.rs/rusqlite/
- **Tauri SQL Plugin**: https://v2.tauri.app/plugin/sql/

### Examples
- **ElectricSQL Tauri Demo**: https://github.com/electric-sql/electric-tauri-postgres
- **sqlite-vec Examples**: https://github.com/asg017/sqlite-vec/tree/main/examples

### Community
- **Tauri Discord**: https://discord.gg/tauri
- **sqlite-vec Discussions**: https://github.com/asg017/sqlite-vec/discussions