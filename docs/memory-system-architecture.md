# Shannon Memory System Architecture

> **Version 3.0** - Enhanced Supervisor Memory with Learning Capabilities

## Overview

Shannon's memory system provides intelligent context retention and retrieval across user sessions, enabling agents to maintain conversational continuity and leverage historical interactions for improved responses.

## Architecture Components

### 1. Storage Layers

#### PostgreSQL
- **Session Context**: Session-level state and metadata
- **Execution Persistence**: Agent and tool execution history
- **Task Tracking**: High-level task and workflow metadata
- **User Management**: Authentication and authorization

#### Redis
- **Session Cache**: Fast access to active session data
- **Token Budgets**: Real-time token usage tracking
- **Compression State**: Tracks context compression status

#### Qdrant (Vector Store)
- **Semantic Memory**: High-performance vector similarity search
- **Collection Organization**: task_embeddings, summaries, tool_results, cases, document_chunks, decomposition_patterns
- **Hybrid Search**: Combines recency and semantic relevance
- **Chunked Storage**: Efficient handling of long Q&A pairs

### 2. Memory Types

#### Hierarchical Memory
Combines multiple retrieval strategies:
- **Recent Memory**: Last N interactions from current session
- **Semantic Memory**: Contextually relevant based on query similarity
- **Compressed Summaries**: Condensed representations of older conversations

#### Session Memory
Chronological retrieval of recent interactions within a session.

#### Agent Memory
Individual agent execution records including:
- Input queries and generated responses
- Token usage and model information
- Tool executions and results
- Performance metrics for strategy selection

#### Enhanced Supervisor Memory
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
- **Intelligent Text Chunking**: Splits long answers (>2000 tokens)
- **Idempotent Storage**: Deduplication via qa_id and chunk_index
- **Batch Embeddings**: Processes all chunks in single API call
- **Smart Reconstruction**: Reassembles full answers from chunks
- **Overlap Strategy**: 200-token overlap for context preservation

### Context Compression
- **Automatic Triggers**: Based on message count and token estimates
- **Rate Limiting**: Prevents excessive compression operations
- **Model-aware Thresholds**: Different limits for various model tiers
- **Fire-and-forget Storage**: Non-blocking persistence

### Memory Retrieval Strategies

#### Hierarchical Retrieval (Default)
1. Fetches recent messages from session
2. Performs semantic search for relevant history
3. Merges and deduplicates results
4. Injects into agent context as `agent_memory`

#### Fallback Chain
1. Primary: Hierarchical memory
2. Secondary: Simple session memory
3. Tertiary: No memory injection (new sessions)

### Version Gating
Memory features protected by Temporal workflow version gates:
- `memory_retrieval_v1`: Hierarchical memory system
- `session_memory_v1`: Basic session memory
- `context_compress_v1`: Context compression
- `supervisor_memory_v2`: Enhanced supervisor memory
- **[NOT IMPLEMENTED]** `performance_selection_v1`: Performance-based routing

## Database Schema

### Core Tables

#### Primary Tables (PostgreSQL)
- `task_executions`: Central task tracking with full metrics (88+ rows, actively used)
- `sessions`: Session metadata and context with external_id support
- `agent_executions`: Agent execution records with performance metrics
- `tool_executions`: Tool invocation history
- `users`: User management

#### Supervisor Memory Tables (PostgreSQL)
- `failure_patterns`: Known failure patterns (3 rows, **ACTIVELY USED**)
  - Tracks: rate_limit, context_overflow, ambiguous_request patterns
  - Includes mitigation strategies and severity levels
- `decomposition_patterns`: Task decomposition history (PostgreSQL table exists; active persistence is in Qdrant collection)
- `strategy_performance`: Aggregated performance metrics (0 rows, aggregation pending)
- `team_compositions`: Agent team configurations (0 rows, writes pending)
- `user_preferences`: User interaction preferences (0 rows, inference pending)

#### Legacy Tables
- `tasks`: Deprecated, replaced by task_executions (historical rows only)

### Workflow Integration Points

1. **SimpleTaskWorkflow**: Before agent execution
2. **Strategy Workflows**: Research, React, Scientific, Exploratory
3. **SequentialExecution**: Before each agent step
4. **ParallelExecution**: Shared context for all agents
5. **SupervisorWorkflow**: Enhanced memory with strategic insights

### Memory Lifecycle

1. **Creation**: Agent responses stored with embeddings
2. **Retrieval**: Query-based selection using hybrid search
3. **Compression**: Periodic summarization of old conversations
4. **Expiration**: Optional TTL-based cleanup

## Performance Optimizations

### Intelligent Chunking
- **50% storage reduction**: Only stores chunk text
- **Deduplication**: qa_id and chunk_index for idempotency
- **Efficient reconstruction**: Ordered chunk aggregation

### Batch Processing
- **5x faster**: Single API call for multiple chunks
- **Smart caching**: LRU (2048 entries) + Redis
- **Reduced costs**: N chunks → 1 API call

### MMR Diversity (Optional)
- **Configurable diversity**: Balance relevance vs diversity
- **Pool expansion**: Fetch 3x candidates, re-rank
- **Better coverage**: Prevents redundant results

### Indexing Strategy
- **50-90% faster filtering**: Payload indexes on filter fields
- **Optimized HNSW**: m=16, ef_construct=100
- **Comprehensive indexing**: session_id, tenant_id, user_id, agent_id

## Privacy and Data Governance

### PII Protection
- **Data Minimization**: Store only essential fields
- **Anonymization**: UUIDs instead of real identities
- **Redaction**: Automatic PII detection and removal
- **Access Control**: Role-based permissions

### Data Retention
- **Conversation History**: 30-day default retention
- **Decomposition Patterns**: 90-day retention
- **User Preferences**: Session-based, 24-hour expiry
- **Right to Erasure**: User deletion API supported

## Limitations

- Memory retrieval adds latency (mitigated by caching)
- Vector similarity may miss exact keyword matches
- Compression is lossy (preserves key points)
- **[NOT IMPLEMENTED]** Cross-session memory requires explicit consent

## What's NOT Implemented Yet

1. **Cross-session memory retrieval** - Sessions strictly isolated
2. **Content-based hashing for deduplication** - Uses embedding similarity
3. **Performance-based agent selection** - Metrics collected but not used for routing
4. **User consent mechanisms** - No API for cross-session access
5. **User preference inference accuracy metric** - Defined but not calculated
6. **PostgreSQL decomposition pattern writes** - Active storage is in Qdrant; Postgres table writes reserved for future aggregation
7. **Strategy performance aggregation** - Metrics collected but aggregation pending

## Migration Status (2025-01)

### Completed
- **task_executions**: Now primary source of truth (88+ rows, growing)
- **Session linkage**: Non-UUID session IDs supported via external_id mapping
- **Supervisor memory**: Queries fixed to join with task_executions
- **Data flow**: All new tasks write to task_executions with proper metrics

### Pending Implementation
- **Decomposition pattern storage (PostgreSQL)**: Qdrant writes active; Postgres table writes optional/future
- **Strategy aggregation**: Performance metrics collected but not aggregated
- **User preference inference**: Logic defined but not active
- **Team composition tracking**: Schema exists, writes not implemented

## Summary

Shannon's memory system provides a robust foundation for context-aware AI interactions. The core infrastructure is operational with `task_executions` as the primary data store, supervisor memory retrieval working, and P2P agent coordination active. While some advanced features like decomposition pattern persistence and strategy aggregation await implementation, the system successfully maintains session continuity and learns from task executions to improve future performance.
