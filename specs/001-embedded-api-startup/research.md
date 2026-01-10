# Research: Embedded API Server Startup with IPC Communication

**Feature**: 001-embedded-api-startup
**Date**: 2026-01-09

## Research Findings

### 1. Frontend Testing Framework for Next.js in Tauri

**Decision**: Vitest + React Testing Library with Tauri IPC mocking
**Rationale**:
- Vitest provides 5-10x faster performance than Jest for complex real-time components
- Native ESM support aligns with Next.js 16+ and modern React patterns
- Excellent TypeScript support without additional configuration
- `@tauri-apps/api/mocks` enables comprehensive IPC communication testing

**Implementation**:
- Unit/integration testing with Vitest + React Testing Library
- Mock IPC calls using `@tauri-apps/api/mocks`
- Test real-time components like DebugConsole, LogEntry, useServerLogs hook
- Optional Playwright for E2E testing

**Alternatives Considered**: Jest was evaluated but rejected due to performance and ESM compatibility issues

---

### 2. Tauri IPC Best Practices for Real-time Communication

**Decision**: Continue with event-based IPC architecture, consider channels for high-volume scenarios
**Rationale**:
- Shannon's current event system is well-designed for logging/monitoring use case
- Events are perfect for fire-and-forget communication with multiple consumers
- Existing performance optimizations (circular buffers, non-blocking emission) are excellent

**Key Patterns**:
- **Events**: Small payloads (<1KB), fire-and-forget, multi-consumer (Shannon's current approach)
- **Commands**: Request/response patterns, type safety, error handling
- **Channels**: High-frequency data (>1000 events/second), ordered delivery

**Performance Thresholds**:
- Use events for <400 events/second (Shannon's typical load)
- Consider channels for >1000 events/second
- Implement batching for very high volumes

**Alternatives Considered**: Command-based approach was evaluated but rejected for real-time streaming needs

---

### 3. Port Discovery and Binding in Rust

**Decision**: Enhanced sequential port discovery with robust error classification
**Rationale**:
- Sequential discovery (1906→1915) provides predictable behavior
- Tokio TcpListener offers excellent cross-platform compatibility
- Error classification enables intelligent retry vs. failure decisions

**Implementation Strategy**:
```rust
async fn find_available_port(start: u16, end: u16) -> Result<(TcpListener, u16), PortBindError>
```

**Key Features**:
- Cross-platform error handling (Windows WSAEADDRINUSE, POSIX EADDRINUSE)
- Configurable port ranges via Shannon config system
- Comprehensive error types with actionable troubleshooting guidance
- Integration with existing logging patterns

**Alternatives Considered**:
- Random port selection: rejected due to unpredictability
- OS-assigned ports (bind to 0): rejected due to need for specific range

---

### 4. Health Check Endpoint Patterns for Embedded Servers

**Decision**: Implement comprehensive readiness checks with `/health`, `/ready`, and `/startup` endpoints
**Rationale**:
- Shannon needs to verify server readiness for chat functionality
- Standard health check patterns enable reliable monitoring
- Detailed dependency verification prevents false-ready states

**Endpoint Strategy**:
- **`/health`**: Liveness probe (basic server responsiveness) - ✅ already implemented
- **`/ready`**: Readiness probe (can handle chat requests) - needs enhancement
- **`/startup`**: Startup probe (initialization complete) - new endpoint

**Readiness Checks**:
- Database connection (SurrealDB in embedded mode)
- LLM provider API key configuration
- Workflow engine operational status
- Tool registry populated
- Memory/resource availability

**Alternatives Considered**: Basic ping endpoint was rejected as insufficient for complex startup verification

---

### 5. Exponential Backoff Retry Implementation

**Decision**: Implement retry manager with configurable exponential backoff matching Shannon patterns
**Rationale**:
- Shannon already has established retry patterns in durable activities
- Three attempts with 1s, 2s, 4s delays provides reasonable recovery time
- Integration with existing error handling and IPC logging maintains consistency

**Configuration**:
- Max attempts: 3 (per requirement)
- Backoff pattern: 2^(attempt-1) seconds with optional jitter
- Error classification: retry on transient errors, fail fast on configuration issues

**Implementation Features**:
- State management for restart tracking
- Structured logging with attempt context
- Circuit breaker integration for health monitoring
- Non-blocking async delays using tokio::time::sleep

**Alternatives Considered**: Fixed delay intervals were rejected in favor of exponential backoff for better system load distribution

---

### 6. Log Streaming Architectures in Desktop Applications

**Decision**: Enhance existing Shannon IPC logging with adaptive optimizations
**Rationale**:
- Current Shannon architecture is already well-designed with excellent foundations
- Circular buffering (1000 events) effectively prevents memory leaks
- IPC event system provides optimal performance for typical log volumes

**Current Strengths**:
- Thread-safe Arc<RwLock<>> pattern
- Structured event types with rich metadata
- Performance-optimized frontend with debounced search
- Strong tracing-rs integration

**Enhancement Areas**:
- **Hierarchical Buffering**: Different retention by log level (errors: 200, normal: 800)
- **Batched IPC**: Group events for high-volume scenarios (>1000 events/sec)
- **Memory Management**: Adaptive buffer sizing and compression for historical data
- **Frontend Optimizations**: Virtual scrolling for large log sets

**Alternatives Considered**: Complete architecture overhaul was rejected due to the strength of existing implementation

---

## Architecture Decisions Summary

| Component | Technology Choice | Primary Alternative |
|-----------|------------------|-------------------|
| Testing | Vitest + React Testing Library | Jest + RTL |
| IPC Pattern | Events (current) | Commands |
| Port Discovery | Sequential with error classification | Random/OS-assigned |
| Health Checks | Multi-endpoint strategy | Simple ping |
| Retry Logic | Exponential backoff with state | Fixed intervals |
| Log Streaming | Enhanced current architecture | Complete rewrite |

---

## Next Steps

All research tasks completed successfully. Proceeding to **Phase 1: Design & Contracts** to create:
- `data-model.md` - Entity definitions and relationships
- `contracts/` - API contract specifications
- `quickstart.md` - Implementation guidance
- Agent context updates