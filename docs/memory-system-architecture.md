# Shannon Memory System Architecture

> **Version 3.0** - Enhanced Supervisor Memory Update: Added strategic memory system with learning capabilities, decomposition pattern tracking, and failure recognition

## Overview

Shannon's memory system provides intelligent context retention and retrieval across user sessions, enabling agents to maintain conversational continuity and leverage historical interactions for improved responses. The v2.0 improvements reduce storage by 50%, improve retrieval accuracy, and add diversity to search results.

## Architecture Components

### 1. Storage Layers

#### PostgreSQL
- **Session Context**: Stores session-level state and metadata
- **Execution Persistence**: Agent and tool execution history via `agent_executions` and `tool_executions` tables
- **Task Tracking**: High-level task and workflow metadata
- **User Management**: Authentication and authorization data

#### Redis
- **Session Cache**: Fast access to active session data
- **Token Budgets**: Real-time token usage tracking
- **Compression State**: Tracks when context compression was last performed

#### Qdrant (Vector Store)
- **All Vector Operations**: Embeddings and vector similarity searches
- **Semantic Memory**: High-performance vector similarity search
- **Collection-based Organization**: Separate collections (task_embeddings, summaries, tool_results, cases, document_chunks)
- **Hybrid Search**: Combines recency and semantic relevance
- **Optimized Indexing**: Payload indexes on session_id, tenant_id, user_id, agent_id, qa_id, is_chunked, timestamp
- **Chunked Storage**: Efficient handling of long Q&A pairs with deterministic IDs

### 2. Memory Types

#### Hierarchical Memory
Combines multiple retrieval strategies for optimal context:
- **Recent Memory**: Last N interactions from current session
- **Semantic Memory**: Contextually relevant memories based on query similarity
- **Compressed Summaries**: Condensed representations of older conversations

#### Session Memory
Simple chronological retrieval of recent interactions within a session.

#### Agent Memory
Individual agent execution records including:
- Input queries and generated responses
- Token usage and model information
- Tool executions and their results
- Performance metrics for strategy selection

#### Enhanced Supervisor Memory (NEW)
Strategic memory for intelligent task decomposition:
- **Decomposition Patterns**: Successful task breakdowns for reuse
- **Strategy Performance**: Aggregated metrics per strategy type
- **Team Compositions**: Successful agent team configurations
- **Failure Patterns**: Known failures with mitigation strategies
- **User Preferences**: Inferred expertise and interaction style

### 3. Persistence Layer

#### Agent Execution Tracking
- Workflow ID, Agent ID, and Task ID correlation
- Success/failure states with error details
- Token consumption and duration metrics
- Strategy metadata for performance analytics

#### Tool Execution Logging
- Tool name, parameters, and outputs
- Success/failure tracking
- Associated agent and workflow context

## Key Features

### Advanced Chunking System
- **Intelligent Text Chunking**: Automatically splits long answers (>2000 tokens) into manageable chunks
- **Idempotent storage**: Chunks include qa_id and chunk_index in payload for deduplication
- **Batch Embeddings**: Processes all chunks in a single API call for efficiency
- **Smart Reconstruction**: Reassembles full answers from ordered chunk texts
- **Overlap Strategy**: 200-token overlap between chunks for context preservation

### Context Compression
- **Automatic Triggers**: Based on message count and token estimates
- **Rate Limiting**: Prevents excessive compression operations
- **Model-aware Thresholds**: Different limits for various model tiers
- **Fire-and-forget Storage**: Non-blocking persistence of compressed summaries

### Memory Retrieval Strategies

#### Hierarchical Retrieval (Default)
1. Fetches recent messages from session
2. Performs semantic search for relevant historical context
3. Merges and deduplicates results
4. Injects into agent context as `agent_memory`

#### Fallback Chain
1. Primary: Hierarchical memory (if enabled via version gate)
2. Secondary: Simple session memory
3. Tertiary: No memory injection (for new sessions)

### Version Gating
All memory features are protected by Temporal workflow version gates:
- `memory_retrieval_v1`: Hierarchical memory system
- `session_memory_v1`: Basic session memory
- `context_compress_v1`: Context compression
- **Note**: `performance_selection_v1` mentioned in docs but NOT implemented yet

## Implementation Details

### Database Schema

#### Core Tables (PostgreSQL)
- `sessions`: Session metadata and context
- `agent_executions`: Detailed agent execution records with performance metrics
- `tool_executions`: Tool invocation history
- `tasks`: High-level task tracking
- `task_executions`: Workflow execution history (legacy, being phased out)
- `decomposition_patterns`: Successful task decomposition history (NEW)
- `strategy_performance`: Aggregated strategy performance metrics (NEW)
- `team_compositions`: Successful agent team configurations (NEW)
- `failure_patterns`: Known failure patterns and mitigations (NEW)
- `user_preferences`: Inferred user interaction preferences (NEW)
- **Note**: Vectorized Q&A pairs are stored in Qdrant, not PostgreSQL

#### Indexes for Performance
- Composite indexes for analytics queries (PostgreSQL)
- Time-based indexes for recent retrieval (PostgreSQL)
- Vector indexes handled by Qdrant's HNSW algorithm

### Workflow Integration

#### Memory Injection Points
1. **SimpleTaskWorkflow**: Before agent execution (lines 76-124)
2. **Strategy Workflows**: Research, React, Scientific, Exploratory (all have memory)
3. **SequentialExecution**: Before each agent step
4. **ParallelExecution**: Shared context for all agents
5. **SupervisorWorkflow**: Enhanced memory with strategic insights (v2)

#### Persistence Patterns
- **Fire-and-forget**: Detached Temporal activities
- **Non-blocking**: Doesn't affect workflow success
- **Retry Logic**: 3 attempts with exponential backoff
- **Batch Operations**: Efficient bulk inserts

### Memory Lifecycle

1. **Creation**: Agent responses stored with embeddings
2. **Retrieval**: Query-based selection using hybrid search
3. **Compression**: Periodic summarization of old conversations
4. **Expiration**: Optional TTL-based cleanup (configurable)

## Usage Instructions

### Enabling Memory Features

Memory features are automatically enabled for sessions with valid session IDs. The system will:
1. Retrieve relevant context before agent execution
2. Persist successful responses for future use
3. Compress context when thresholds are exceeded
4. Track performance metrics for strategy optimization

### Configuration

#### Environment Variables
- `QDRANT_URL`: Vector store endpoint
- `OPENAI_API_KEY`: For embedding generation
- `REDIS_URL`: Session cache location
- `DATABASE_URL`: PostgreSQL connection

#### Memory System Settings (config/shannon.yaml)
```yaml
vector:
  enabled: true
  expected_embedding_dim: 1536  # OpenAI dimension
  mmr_enabled: false             # Set to true for diversity
  mmr_lambda: 0.7                # 0.0 (diversity) to 1.0 (relevance)
  mmr_pool_multiplier: 3         # Fetch 3x candidates

embeddings:
  chunking:
    enabled: true
    max_tokens: 2000             # Chunk size
    overlap_tokens: 200          # Overlap between chunks
    min_chunk_tokens: 100        # Minimum chunk size
```

#### Default Thresholds
- **Compression Triggers**: 20+ messages or model-specific token limits
- **Retrieval Limits**: Top 5 recent + Top 5 semantic matches + Top 3 summaries
- **Semantic Threshold**: 0.75 similarity score minimum
- **Summary Limits**: Configurable via `SummaryTopK` (default: 3)

### Memory Quality

#### PII Redaction
- Automatic PII detection and removal before storage
- Configurable redaction levels per tenant
- Maintains semantic meaning while protecting privacy

#### Deduplication
- Prevents duplicate memories in retrieval results
- Uses Qdrant point IDs when available
- Falls back to composite key: query + answer[0:100] (NOT content hashing)
- QA grouping for chunked content reassembly
- Timestamp-based ordering for conflicts

## Migration Path

### Phase 1: Basic Memory (Completed)
- Session context storage in Qdrant
- Simple retrieval patterns
- Agent execution persistence in PostgreSQL
- Hierarchical memory combining recent + semantic
- Token-based context compression

### Phase 2: Advanced Memory (Completed)
- Semantic search with similarity thresholds
- Chunking for long documents
- MMR diversity support (configurable)
- Batch embedding processing
- Fire-and-forget persistence patterns

### Phase 3: Intelligent Selection (Planned - Current Focus)
- Epsilon-greedy strategy selection based on `agent_executions` data
- Performance-based routing using historical success rates
- A/B testing framework

### Phase 4: Advanced Features (Future)
- Cross-session memory sharing
- User-specific memory profiles
- Federated memory across deployments

## Monitoring and Observability

### Metrics
- Memory retrieval latency
- Compression frequency and duration
- Token usage before/after compression
- Cache hit rates
- **Chunking Metrics**:
  - `shannon_chunks_per_qa`: Distribution of chunks per Q&A pair
  - `shannon_chunk_size_tokens`: Token size distribution of chunks
  - `shannon_chunked_qa_pairs_total`: Count of chunked Q&A pairs by session
  - `shannon_retrieval_token_budget`: Token budget used in retrieval
  - `shannon_chunk_aggregation_latency_seconds`: Chunk reconstruction performance
- **Embedding Metrics**:
  - `shannon_embedding_requests_total`: Total embedding requests (single vs batch)
  - `shannon_embedding_latency_seconds`: Embedding generation latency

### Debugging
- Workflow replay for memory operations
- Persistence success/failure logs
- Memory injection visibility in traces

## Performance Optimizations (v2.0)

### Intelligent Chunking System
- **Automatic splitting**: Long answers (>2000 tokens) split into overlapping chunks
- **50% storage reduction**: Only stores chunk text, not full 20KB answers
- **Deduplication**: qa_id and chunk_index stored in payload for idempotency
- **Efficient reconstruction**: Ordered chunk aggregation preserves context

### Batch Embedding Pipeline
- **5x faster processing**: Single API call for multiple chunks
- **Smart caching**: LRU (2048 entries) + Redis for embedding reuse
- **Reduced costs**: N chunks → 1 API call instead of N calls

### MMR Diversity (Optional)
- **Configurable diversity**: `mmr_lambda` balances relevance vs diversity
- **Pool expansion**: Fetch 3x candidates, re-rank for diversity
- **Better coverage**: Prevents redundant similar results
- **Configuration**: Set `mmr_enabled: true` in `config/shannon.yaml`

### Indexing Strategy
- **50-90% faster filtering**: Payload indexes on all filter fields
- **Index fields**: session_id, tenant_id, user_id, agent_id, qa_id, is_chunked, timestamp
- **Optimized HNSW**: m=16, ef_construct=100 for balance of speed/accuracy

### Validation & Safety
- **Dimension validation**: Startup check ensures 1536D (OpenAI) compatibility
- **Graceful degradation**: Falls back if validation fails
- **Comprehensive metrics**: Full observability via Prometheus

### Qdrant API Compatibility
- **Dual endpoint support**: Uses `/points/query` (modern) with fallback to `/points/search` (legacy)
- **Response format handling**: Automatically detects nested (`result.points`) vs flat (`result`) structures
- **Filter compatibility**: Supports both session-based and tenant-based filtering
- **Vector retrieval**: MMR diversity requires `with_vector: true` for re-ranking

## Best Practices

1. **Session Management**: Always provide session IDs for memory benefits
2. **Token Budgets**: Monitor compression triggers to optimize thresholds
3. **Memory Relevance**: Tune semantic thresholds based on use case
4. **Performance**: Use fire-and-forget persistence to avoid blocking
5. **Privacy**: Enable PII redaction for sensitive domains
6. **Chunking Configuration**: Adjust chunk size/overlap based on content type
7. **Monitoring**: Track chunking metrics to optimize thresholds

## Limitations

- Memory retrieval adds latency (mitigated by caching)
- Vector similarity may miss exact keyword matches
- Compression is lossy (preserves key points, not verbatim)
- Cross-session memory requires explicit user consent

## Examples

### Example 1: Simple Session Memory

**User Session**: `session-123`

```
Turn 1:
User: "My name is Alice and I work at TechCorp"
Agent: "Nice to meet you, Alice! How can I help you at TechCorp today?"

Turn 2:
User: "What's my name?"
Agent: [Retrieves memory] "Your name is Alice, and you work at TechCorp."
```

**Behind the scenes:**
1. Turn 1 response stored in Qdrant `task_embeddings` collection with embedding
2. Turn 2 triggers memory retrieval via `FetchHierarchicalMemory` activity
3. Context injected: `{agent_memory: [{q: "My name is Alice...", a: "Nice to meet you..."}]}`

### Example 2: Hierarchical Memory with Semantic Search

**Long conversation about debugging a Python app:**

```
Turn 1-10: Discussion about Python syntax errors
Turn 11-20: Talking about database connections
Turn 21-30: Configuring Docker containers
...
Turn 50:
User: "What was that syntax error we fixed earlier?"
Agent: [Hierarchical retrieval activates]
  - Recent memory: Turns 45-49 (Docker discussion)
  - Semantic search: Turns 3-5 (Python syntax errors) - HIGH RELEVANCE
  "Earlier, we fixed a syntax error in your list comprehension on line 42..."
```

**Memory selection:**
- Recent: 5 most recent exchanges
- Semantic: Top 5 matches for "syntax error" (similarity > 0.75)
- Merged and deduplicated before injection

### Example 3: Context Compression in Action

**Session with 25+ messages hitting token limits:**

```
Before compression (8,000 tokens):
- Full conversation history
- Detailed technical discussions
- Code snippets and explanations

Compression triggered:
- System detects 25 messages + 8K tokens
- Compression activity summarizes to 2,000 tokens
- Summary stored in database

After compression:
- Summary: "User debugging Python app with PostgreSQL. Fixed syntax errors
  in list comprehensions, resolved connection pooling issues, configured
  Docker networking. Key decisions: Using SQLAlchemy ORM, Alpine Linux base..."
- Recent 5 messages kept verbatim
- Total context: 3,000 tokens (62% reduction)
```

### Example 4: Parallel Agent Execution with Shared Memory

**Complex task with supervisor pattern:**

```
User: "Analyze our API performance and suggest optimizations"

Supervisor creates 3 parallel subtasks:
1. Agent-1: Analyze response times [Gets memory context]
2. Agent-2: Review database queries [Gets same memory context]
3. Agent-3: Check caching strategy [Gets same memory context]

Shared memory context:
- Previous performance discussions
- Known bottlenecks mentioned before
- Technology stack details from earlier

All agents see: "Previous analysis showed N+1 query problems..."
```

### Example 5: Chunked Long Answer Storage

**Agent provides a detailed technical explanation (5000+ tokens):**

```
User: "Explain how distributed consensus algorithms work"
Agent: [Generates 5000-token detailed explanation about Raft, Paxos, etc.]

Behind the scenes:
1. Answer detected as >2000 tokens, triggers chunking
2. Split into 3 chunks with 200-token overlap:
   - Chunk 0: Introduction and Raft basics (0-2000 tokens)
   - Chunk 1: Raft details and Paxos intro (1800-3800 tokens)
   - Chunk 2: Paxos details and comparisons (3600-5000 tokens)
3. Batch embedding generates 3 vectors in single API call
4. Stored with deterministic IDs:
   - "qa_abc123:0", "qa_abc123:1", "qa_abc123:2"
5. Metrics recorded: 3 chunks, ~1666 tokens/chunk average

Later retrieval:
User: "What did you say about leader election?"
System:
1. Semantic search finds chunks with qa_id="qa_abc123"
2. Chunks ordered by chunk_index
3. Full answer reconstructed from chunk_text fields
4. Relevant portion extracted and returned
```

### Example 6: Cross-Session Memory (ILLUSTRATIVE - Not Implemented)

**Note: This example shows a potential future feature. Currently, sessions are isolated and cannot access each other's memories.**

```
New Session: session-456
User: "Continue where we left off with the optimization work"

// FUTURE IMPLEMENTATION WOULD:
// 1. No recent memory in new session
// 2. Fetch user's previous sessions (with consent)
// 3. Retrieve relevant memories from session-123
// 4. Inject historical context

// CURRENT REALITY:
Agent: "I don't have access to previous sessions. Could you remind me
what optimization work you're referring to?"
```

**Session Isolation**: Each session's memories are strictly isolated for privacy. Cross-session memory sharing would require explicit consent mechanisms not yet implemented.

## Summary

Shannon's memory system provides a robust foundation for context-aware AI interactions, balancing performance, relevance, and privacy. The hierarchical architecture ensures agents have access to the most pertinent information while managing resource constraints effectively.

## Technical Notes

### Storage Architecture Clarification
- **Vector Storage**: Qdrant handles all vector embeddings and similarity search operations
- **Relational Storage**: PostgreSQL stores structured data (sessions, tasks, executions)
- **Cache Layer**: Redis provides fast session access and token tracking
- **No pgvector**: While the PostgreSQL container includes pgvector capability, it is not used in the current architecture

### What's NOT Implemented Yet
Despite being mentioned in this document:
1. **Cross-session memory retrieval** - Sessions are strictly isolated
2. **Content-based hashing for deduplication** - Uses embedding similarity (95% threshold) instead
3. **Performance-based agent selection** - Metrics collected but routing not yet automated
4. **User consent mechanisms** - No API for cross-session memory access with consent
5. **User preference inference accuracy metric** - Defined but not yet calculated/updated

## Recent Enhancements (v3.0)

### Enhanced Supervisor Memory System

#### Core Components
- **DecompositionAdvisor**: Intelligent task breakdown suggestions based on historical patterns
- **Strategy Performance Tracking**: Real-time aggregation of strategy success metrics
- **Failure Pattern Recognition**: Proactive identification and mitigation of known issues
- **User Preference Learning**: Adaptive behavior based on inferred expertise and style

#### Database Schema Additions
```sql
-- New tables for supervisor memory
decomposition_patterns    -- Stores successful task decompositions
strategy_performance      -- Aggregated performance metrics per strategy
team_compositions        -- Successful agent team configurations
failure_patterns         -- Known failure patterns with mitigations
user_preferences         -- Inferred user interaction preferences
```

#### Key Features
1. **Pattern Learning**:
   - Stores successful decompositions with embeddings
   - Reuses patterns for similar queries (>80% similarity)
   - Tracks success rate and performance metrics

2. **Strategy Optimization**:
   - Epsilon-greedy selection (10% exploration, 90% exploitation)
   - Balances speed vs accuracy based on user preference
   - Tracks tokens, duration, and success rate per strategy

3. **Failure Avoidance**:
   - Pre-configured patterns: rate_limit, context_overflow, ambiguous_request
   - Dynamic pattern detection from query indicators
   - Automatic mitigation suggestions

4. **Near-Duplicate Detection**:
   - 95% similarity threshold prevents redundant storage
   - Error response filtering (skips error messages)
   - Saliency filtering (ignores responses <50 chars)
   - Metrics: `shannon_memory_writes_skipped_total`

#### Version Gating
- Uses `supervisor_memory_v2` for backward compatibility
- Graceful fallback to basic memory on failure
- Non-blocking persistence patterns

#### Integration Status
- ✅ Memory structures implemented
- ✅ Database schema created
- ✅ Activities registered
- ✅ Version gating in place
- ✅ DecompositionAdvisor fully integrated with supervisor workflow
- ✅ RecordDecomposition activity integrated and called on workflow completion
- ✅ Circuit breaker pattern implemented for database resilience
- ✅ Configurable thresholds via environment variables
- ✅ Comprehensive metrics for monitoring and observability
- ✅ Unit tests for all new components

### Performance Improvements
- **Deduplication**: Reduces storage by ~30% by filtering duplicates
- **Error Filtering**: Prevents storing non-valuable error responses
- **Batch Operations**: Efficient bulk embedding generation
- **Fire-and-forget**: Non-blocking persistence for learning

## Privacy and Data Governance

### PII Handling Policy

The supervisor memory system collects and stores user interaction patterns to improve task decomposition and strategy selection. This section outlines our approach to handling Personally Identifiable Information (PII).

#### Data Collection
The system collects the following types of data:
- **Query Patterns**: User queries and task descriptions (may contain project names, business logic)
- **Interaction History**: Conversation context and Q&A pairs
- **Performance Metrics**: Task success rates, execution times, token usage
- **User Preferences**: Inferred expertise level and speed/accuracy preferences

#### PII Protection Measures

1. **Data Minimization**
   - Store only essential fields needed for pattern matching
   - Avoid storing raw user inputs when patterns suffice
   - Aggregate metrics rather than individual events where possible

2. **Anonymization**
   - User IDs are stored as UUIDs, not real names or emails
   - Session IDs are randomly generated and temporary
   - No correlation between sessions without explicit user consent

3. **Redaction Before Storage**
   - Sensitive patterns detected and redacted:
     - Email addresses replaced with `[EMAIL]`
     - Phone numbers replaced with `[PHONE]`
     - API keys/tokens replaced with `[REDACTED]`
   - Configurable redaction rules via environment variables

4. **Access Control**
   - Database access restricted to service accounts
   - Read/write permissions separated
   - Audit logging for all data access

#### Data Retention Policy

1. **Conversation History**
   - Retained for 30 days by default
   - Configurable via `MEMORY_RETENTION_DAYS` environment variable
   - Automatic cleanup job runs daily

2. **Decomposition Patterns**
   - Aggregated patterns retained for 90 days
   - Individual patterns purged after 30 days
   - Success metrics kept indefinitely in aggregated form

3. **User Preferences**
   - Updated on each interaction
   - Reset when session expires (24 hours of inactivity)
   - No long-term profiling without explicit opt-in

4. **Right to Erasure**
   - Users can request deletion via API endpoint
   - Cascade deletion removes all associated data
   - Audit trail maintained for compliance

#### Compliance Considerations

- **GDPR**: Right to access, rectification, and erasure supported
- **CCPA**: User data disclosure and deletion mechanisms in place
- **SOC2**: Audit logging and access controls implemented
- **HIPAA**: Not storing health information; additional safeguards needed if deployed in healthcare

#### Configuration

Environment variables for PII handling:
```bash
# Data retention settings
MEMORY_RETENTION_DAYS=30
PATTERN_RETENTION_DAYS=90
AGGREGATE_RETENTION_DAYS=365

# PII redaction
ENABLE_PII_REDACTION=true
REDACT_EMAILS=true
REDACT_PHONES=true
REDACT_API_KEYS=true

# Audit logging
ENABLE_AUDIT_LOGGING=true
AUDIT_LOG_PATH=/var/log/shannon/audit.log
```

#### Monitoring and Alerts

- Metric: `shannon_pii_redactions_total` - Count of PII redactions
- Metric: `shannon_data_retention_cleanups_total` - Cleanup job executions
- Alert: Unusual data access patterns detected
- Alert: Retention cleanup job failures