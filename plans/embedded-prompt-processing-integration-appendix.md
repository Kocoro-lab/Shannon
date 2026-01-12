# Embedded Prompt Processing Integration - Appendix

This appendix contains additional sections for the main integration plan document.

## Performance Standards (Continued)

2. **Throughput Targets**
   - Concurrent workflows: 10+
   - Events per second: 1000+
   - Database writes: 100+ per second

3. **Resource Limits**
   - Maximum prompt length: 100KB
   - Maximum response length: 1MB
   - Maximum workflow duration: 5 minutes
   - Event buffer size: 256 events per workflow

---

## Success Criteria

### Phase 1 Success Criteria

**Task Submission → Workflow Integration**

✅ **Functional Requirements**:
1. User can submit "What is 2+2?" via UI
2. Task submission returns workflow_id within 50ms
3. Workflow instance created in durable-shannon
4. Settings retrieved from database
5. API key presence validated

✅ **Technical Requirements**:
1. Zero compilation errors
2. Zero compilation warnings
3. 100% test coverage for task submission code
4. Integration test: submit_task_creates_workflow()
5. Integration test: submit_task_validates_api_key()

✅ **Acceptance Test**:
```bash
# Submit task via API
curl -X POST http://localhost:1906/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"prompt": "What is 2+2?"}'

# Expected response (within 50ms):
{
  "task_id": "uuid-here",
  "workflow_id": "task-uuid-timestamp",
  "status": "running"
}
```

---

### Phase 2 Success Criteria

**Workflow → LLM Integration**

✅ **Functional Requirements**:
1. Workflow retrieves API key from database
2. API key decrypted correctly
3. LLM request sent to provider (OpenAI, Anthropic, etc.)
4. Streaming response handled
5. Final response returned

✅ **Technical Requirements**:
1. Zero compilation errors
2. Zero compilation warnings
3. 100% test coverage for LLM activity
4. Integration test for each provider (5 tests)
5. Error handling tests (missing key, invalid key, timeout)

✅ **Acceptance Test**:
```bash
# Set API key
curl -X POST http://localhost:1906/api/v1/settings \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "openai",
    "api_key": "sk-test-key",
    "model": "gpt-4"
  }'

# Submit task
curl -X POST http://localhost:1906/api/v1/tasks \
  -d '{"prompt": "What is 2+2?"}'

# Expected: LLM response received from OpenAI
# Workflow completes within 5 seconds
```

---

### Phase 3 Success Criteria

**Event Emission Integration**

✅ **Functional Requirements**:
1. Workflow emits WORKFLOW_STARTED event
2. Workflow emits AGENT_THINKING event
3. Workflow emits LLM_PROMPT event
4. Workflow emits LLM_PARTIAL events (streaming)
5. Workflow emits LLM_OUTPUT event
6. Workflow emits WORKFLOW_COMPLETED event
7. Events persisted to SQLite (filtered)

✅ **Technical Requirements**:
1. Zero compilation errors
2. Zero compilation warnings
3. 100% test coverage for event system
4. All 26 event types supported
5. Event ordering guaranteed (seq)

✅ **Acceptance Test**:
```bash
# Submit task and verify events
TASK_ID=$(curl -X POST http://localhost:1906/api/v1/tasks \
  -d '{"prompt": "What is 2+2?"}' | jq -r '.task_id')

# Query events from database
sqlite3 shannon.sqlite "SELECT event_type, seq FROM event_logs WHERE workflow_id LIKE '%${TASK_ID}%' ORDER BY seq"

# Expected output:
# WORKFLOW_STARTED|1
# AGENT_THINKING|2
# LLM_PROMPT|3
# LLM_OUTPUT|10
# WORKFLOW_COMPLETED|11
# (LLM_PARTIAL events NOT persisted per spec)
```

---

### Phase 4 Success Criteria

**Streaming Integration**

✅ **Functional Requirements**:
1. SSE endpoint streams events in real-time
2. UI receives events within 100ms of emission
3. Event types mapped correctly (thread.message.delta, etc.)
4. Client reconnection works
5. Progressive UI updates

✅ **Technical Requirements**:
1. Zero compilation errors
2. Zero compilation warnings
3. 100% test coverage for streaming
4. SSE format follows docs/streaming-api.md spec
5. Event ordering preserved

✅ **Acceptance Test**:
```bash
# Stream events via SSE
curl -N http://localhost:1906/api/v1/tasks/${TASK_ID}/stream

# Expected output:
event: workflow.started
data: {"workflow_id":"...","type":"WORKFLOW_STARTED","seq":1}

event: agent.thinking
data: {"workflow_id":"...","type":"AGENT_THINKING","message":"What is 2+2?","seq":2}

event: llm.prompt
data: {"workflow_id":"...","type":"LLM_PROMPT","message":"What is 2+2?","seq":3}

event: thread.message.delta
data: {"workflow_id":"...","delta":"2","seq":4}

event: thread.message.delta
data: {"workflow_id":"...","delta":" + ","seq":5}

event: thread.message.completed
data: {"workflow_id":"...","response":"2 + 2 equals 4","seq":10}

event: workflow.completed
data: {"workflow_id":"...","type":"WORKFLOW_COMPLETED","seq":11}
```

**UI Acceptance Test**:
1. User types "What is 2+2?" and presses Enter
2. Loading indicator appears immediately
3. "Thinking..." message appears within 100ms
4. Streaming text appears progressively
5. Final response "2 + 2 equals 4" displayed
6. Debug console shows all events with timestamps
7. Total time < 5 seconds

---

### Phase 5 Success Criteria

**Debug Logging Integration**

✅ **Functional Requirements**:
1. All workflow stages logged with context
2. Logs forwarded to IPC event system
3. Debug console displays logs in real-time
4. Logs color-coded by level (DEBUG, INFO, WARN, ERROR)
5. Log filtering and search works

✅ **Technical Requirements**:
1. Zero compilation errors
2. Zero compilation warnings
3. 100% test coverage for logging
4. Structured logging with tracing
5. Log retention configurable

✅ **Acceptance Test**:

**Backend Logs** (visible in terminal):
```
2026-01-11T21:30:00Z INFO  [shannon_api] Starting embedded API server port=1906
2026-01-11T21:30:05Z INFO  [durable_shannon::workflows::simple_task] Starting workflow execution workflow_id=task-123 prompt="What is 2+2?"
2026-01-11T21:30:05Z DEBUG [durable_shannon::activities::llm] Retrieved user settings provider=openai model=gpt-4
2026-01-11T21:30:06Z INFO  [durable_shannon::activities::llm] Sending LLM request provider=openai model=gpt-4
2026-01-11T21:30:08Z INFO  [durable_shannon::activities::llm] Received LLM response tokens=10 latency=2s
2026-01-11T21:30:08Z INFO  [durable_shannon::workflows::simple_task] Workflow completed successfully workflow_id=task-123 result_length=15
```

**UI Debug Console** (visible in desktop app):
- Events tab shows 11 events with timestamps
- Logs tab shows 6 log entries with color coding
- Combined tab shows merged timeline
- Search for "workflow_id=task-123" returns 6 results
- Export button generates logs.txt file

---

### Phase 6 Success Criteria

**End-to-End Integration**

✅ **Functional Requirements**:
1. Complete flow works: UI → submission → execution → streaming → display
2. All 5 providers work (OpenAI, Anthropic, Google, Groq, xAI)
3. Error scenarios handled gracefully
4. Performance targets met
5. Documentation complete

✅ **Technical Requirements**:
1. Zero compilation errors
2. Zero compilation warnings
3. 100% code coverage for new code
4. All integration tests pass
5. All e2e tests pass
6. Performance tests pass

✅ **Final Acceptance Test**:

**Happy Path** (90 second demo):
1. Start desktop app
2. Navigate to Settings → API Keys
3. Enter OpenAI API key: "sk-test-key"
4. Click Save (encrypted, stored in SQLite)
5. Navigate to Chat
6. Type "What is 2+2?" and press Enter
7. Observe in real-time:
   - Loading indicator (0ms)
   - "Thinking..." message (100ms)
   - Streaming text "2" → " + " → "2" → " equals" → " 4" (2s)
   - Final response displayed (3s)
8. Click Debug Console tab
9. See 11 events with timestamps
10. See 6 log entries with context
11. Export logs to file
12. Success! ✅

**Error Scenarios**:
1. Submit without API key → Clear error: "OpenAI API key required"
2. Submit with invalid key → Clear error: "Invalid API key"
3. Network timeout → Clear error: "Request timeout after 30s"
4. Provider outage → Fallback to Anthropic (if configured)
5. All error states tested and working

**Performance**:
- Task submission: 23ms (target: <50ms) ✅
- LLM first token: 1.2s (target: <2s) ✅
- Event streaming: 45ms avg (target: <100ms) ✅
- Memory usage: 67MB (target: <100MB) ✅
- Concurrent workflows: 15 (target: 10+) ✅

**Documentation**:
- ✅ Architecture diagrams updated
- ✅ API reference complete
- ✅ Integration guide written
- ✅ Troubleshooting guide updated
- ✅ Code examples provided

---

## Testing Strategy

### Testing Pyramid

```
                     ┌─────────────┐
                     │   E2E (10)  │  Complete user flows
                     └─────────────┘
                   ┌───────────────────┐
                   │ Integration (50)  │  Component interactions
                   └───────────────────┘
               ┌─────────────────────────────┐
               │     Unit Tests (200+)       │  Individual functions
               └─────────────────────────────┘
```

### Unit Tests (200+ tests)

**Location**: `src/*/tests.rs` or `#[cfg(test)] mod tests`

**Coverage**:
- Every public function
- Every error path
- Every edge case
- Every data structure

**Example**:
```rust
#[cfg(test)]
mod tests {
    use super::*;
    
    #[test]
    fn test_parse_task_request_valid() {
        let request = TaskRequest {
            prompt: "What is 2+2?".to_string(),
            model: Some("gpt-4".to_string()),
            provider: Some("openai".to_string()),
        };
        
        assert_eq!(request.prompt, "What is 2+2?");
        assert_eq!(request.model, Some("gpt-4".to_string()));
    }
    
    #[test]
    fn test_parse_task_request_empty_prompt() {
        let request = TaskRequest {
            prompt: "".to_string(),
            model: None,
            provider: None,
        };
        
        let result = validate_task_request(&request);
        assert!(result.is_err());
        assert!(matches!(result.unwrap_err(), ValidationError::EmptyPrompt));
    }
}
```

---

### Integration Tests (50 tests)

**Location**: `tests/integration/*.rs`

**Coverage**:
- Task submission → workflow creation
- Workflow → LLM execution
- Event emission → streaming
- Settings → encryption → retrieval
- Error handling across components

**Example**:
```rust
// tests/integration/task_submission.rs
#[tokio::test]
async fn test_submit_task_with_valid_settings() {
    // Setup
    let app = spawn_test_app().await;
    let settings = UserSettings {
        default_provider: "openai".to_string(),
        default_model: "gpt-4".to_string(),
        openai_api_key: Some(encrypt("sk-test-key")),
        ..Default::default()
    };
    app.database.save_settings(&settings).await.unwrap();
    
    // Execute
    let response = app.client
        .post("/api/v1/tasks")
        .json(&json!({"prompt": "What is 2+2?"}))
        .send()
        .await
        .unwrap();
    
    // Assert
    assert_eq!(response.status(), 200);
    let task: TaskResponse = response.json().await.unwrap();
    assert!(!task.task_id.is_empty());
    assert!(!task.workflow_id.is_empty());
    assert_eq!(task.status, "running");
}
```

---

## Risk Analysis

### Technical Risks

#### Risk 1: Durable Workflow Engine Complexity
**Probability**: Medium  
**Impact**: High  
**Description**: Implementing a deterministic workflow engine in Rust without Temporal.

**Mitigation**:
- Start with simple workflow (Phase 1)
- Use proven patterns from Go orchestrator
- Comprehensive testing at each stage
- Consider using existing Rust workflow libraries

**Contingency**:
- If too complex, integrate with Go orchestrator via gRPC
- Use simpler state machine for Phase 1 MVP
- Defer advanced features (multi-agent, retries) to later phases

---

#### Risk 2: Event Bus Performance
**Probability**: Low  
**Impact**: Medium  
**Description**: Event bus may not handle high throughput (1000+ events/sec).

**Mitigation**:
- Use `tokio::sync::broadcast` (proven, high-performance)
- Bounded channels to prevent memory growth
- Drop slow subscribers rather than blocking
- Monitor performance in Phase 3

**Contingency**:
- Use Redis Streams for event distribution (like Go orchestrator)
- Separate event persistence from event streaming
- Implement backpressure handling

---

#### Risk 3: Provider API Changes
**Probability**: Medium  
**Impact**: Medium  
**Description**: LLM provider APIs may change (rate limits, formats, auth).

**Mitigation**:
- Mock all provider interactions in tests
- Version provider request/response formats
- Monitor provider status pages
- Add fallback to alternate providers

**Contingency**:
- Implement provider adapters for easy updates
- Use client libraries where available (e.g., `async-openai`)
- Document provider-specific quirks

---

## Implementation Timeline

### Week 1: Phase 1 - Task → Workflow Integration
- **Days 1-2**: Instantiate workflow engine, add to AppState
- **Days 3-4**: Implement task submission handler
- **Day 5**: Create simple workflow definition
- **Days 6-7**: Integration tests and documentation

**Deliverable**: Task submission creates workflow instance

---

### Week 2: Phase 2 - Workflow → LLM Integration
- **Days 1-2**: Implement settings retrieval activity
- **Days 3-4**: Implement LLM activity with OpenAI
- **Day 5**: Add Anthropic, Google, Groq, xAI providers
- **Days 6-7**: Integration tests for all providers

**Deliverable**: LLM requests work for all providers

---

### Week 3: Phase 3 - Event Emission Integration
- **Days 1-2**: Create event bus and emitter
- **Days 3-4**: Implement event persistence
- **Day 5**: Wire event bus into AppState and workflows
- **Days 6-7**: Integration tests and verification

**Deliverable**: Events emitted and persisted correctly

---

### Week 4: Phase 4 - Streaming Integration
- **Days 1-2**: Implement SSE stream handler
- **Days 3-4**: Map event types to SSE names
- **Day 5**: Implement UI SSE client
- **Days 6-7**: Integration tests and UI testing

**Deliverable**: Real-time streaming to UI works

---

### Week 5: Phase 5 - Debug Logging Integration
- **Days 1-2**: Add structured logging to workflows
- **Days 3-4**: Forward logs to IPC
- **Day 5**: Update debug console UI
- **Days 6-7**: Add metrics and testing

**Deliverable**: Full visibility into workflow execution

---

### Week 6: Phase 6 - Testing & QA
- **Days 1-2**: E2E integration tests
- **Days 3-4**: Provider-specific and error tests
- **Day 5**: Performance testing and optimization
- **Days 6-7**: Documentation and final review

**Deliverable**: 100% integration complete, all tests pass

---

## Appendix

### Key Files Reference

**Shannon API (Rust)**:
- `rust/shannon-api/src/main.rs` - Application entry point
- `rust/shannon-api/src/gateway/tasks.rs` - Task submission handler
- `rust/shannon-api/src/gateway/streaming.rs` - SSE streaming handler
- `rust/shannon-api/src/database/settings.rs` - Settings management
- `rust/shannon-api/src/events/bus.rs` - Event bus (NEW)
- `rust/shannon-api/src/events/storage.rs` - Event persistence (NEW)

**Durable Shannon (Workflow Engine)**:
- `rust/durable-shannon/src/worker/mod.rs` - Workflow worker
- `rust/durable-shannon/src/workflows/simple_task.rs` - Simple task workflow (NEW)
- `rust/durable-shannon/src/activities/llm.rs` - LLM activity (NEW)
- `rust/durable-shannon/src/activities/settings.rs` - Settings activity (NEW)
- `rust/durable-shannon/src/events/emitter.rs` - Event emitter (NEW)

**Desktop App (TypeScript/React)**:
- `desktop/components/chat-input.tsx` - Chat input component
- `desktop/components/debug-console.tsx` - Debug console
- `desktop/lib/shannon/streaming.ts` - SSE client (NEW)
- `desktop/lib/shannon/settings.ts` - Settings client

---

### Database Schema

**Event Logs Table** (new):
```sql
CREATE TABLE event_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workflow_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    agent_id TEXT,
    message TEXT,
    timestamp TEXT NOT NULL,
    seq INTEGER NOT NULL,
    UNIQUE(workflow_id, seq)
);

CREATE INDEX idx_event_logs_workflow_id ON event_logs(workflow_id);
CREATE INDEX idx_event_logs_timestamp ON event_logs(timestamp);
```

**Task Results Table** (new):
```sql
CREATE TABLE task_results (
    task_id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    prompt TEXT NOT NULL,
    result TEXT,
    status TEXT NOT NULL,
    provider TEXT,
    model TEXT,
    tokens_used INTEGER,
    cost_usd REAL,
    created_at DATETIME NOT NULL,
    completed_at DATETIME
);

CREATE INDEX idx_task_results_workflow_id ON task_results(workflow_id);
CREATE INDEX idx_task_results_created_at ON task_results(created_at);
```

---

## Sign-Off

This specification defines the complete integration path for embedded prompt processing in Shannon desktop application. Upon completion of all 6 phases, users will be able to:

1. ✅ Submit prompts via UI
2. ✅ Receive streaming responses in real-time
3. ✅ View all workflow steps in debug console
4. ✅ Monitor logs and metrics
5. ✅ Handle errors gracefully
6. ✅ Use any of 5 LLM providers

**Target**: 100% end-to-end functionality with zero compilation errors/warnings and 100% test coverage.

**Next Steps**: Begin Phase 1 implementation in Code mode.

---

*Document Version*: 1.0  
*Last Updated*: 2026-01-11  
*Status*: Ready for Implementation
