# Embedded Workflow Engine - Quickstart Guide

**Feature ID**: 002-embedded-workflow-engine  
**Version**: 1.0

---

## Prerequisites

### System Requirements
- **Operating System**: macOS 11+, Windows 10+, or Linux (Ubuntu 20.04+)
- **Rust**: 1.91.1 or higher
- **Node.js**: 18+ (for desktop app)
- **Disk Space**: ~2GB for development, ~500MB for runtime
- **Memory**: 4GB minimum, 8GB recommended

### Development Tools
- Rust toolchain with wasm32-wasi target
- cargo-tarpaulin (for coverage)
- SQLite 3.35+
- Git

### API Keys
At least one LLM provider API key:
- OpenAI API key, OR
- Anthropic API key, OR  
- Google AI API key, OR
- Groq API key

---

## Quick Setup

### 1. Clone and Setup

```bash
# Navigate to Shannon project root
cd /Users/gqadonis/Projects/prometheus/Shannon

# Install WASM target
rustup target add wasm32-wasi

# Setup Python WASI interpreter (for tool execution)
./scripts/setup_python_wasi.sh
```

### 2. Configure Environment

```bash
# Copy example environment file
cp .env.example .env

# Edit .env and add your API keys
# Minimum one of:
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
GOOGLE_API_KEY=...
GROQ_API_KEY=...
```

### 3. Build Core Components

```bash
# Build shannon-api with embedded workflow feature
cd rust/shannon-api
cargo build --features embedded-workflow

# Build durable-shannon worker
cd ../durable-shannon
cargo build

# Build workflow patterns
cd ../workflow-patterns
cargo build
```

### 4. Run Tests

```bash
# Run all tests for embedded workflow
cd /Users/gqadonis/Projects/prometheus/Shannon

# Phase 1 tests (foundation)
cargo test -p shannon-api --test embedded_startup
cargo test -p shannon-api --test embedded_port_fallback
cargo test -p shannon-api --test event_bus_test

# Durable worker tests
cargo test -p durable-shannon --test sqlite_test
cargo test -p durable-shannon --test workflow_store_test

# Pattern tests
cargo test -p workflow-patterns
```

---

## Validation Steps

### Phase 1: Foundation MVP

#### Step 1.1: SQLite Event Log (P1.1)

```bash
cd rust/durable-shannon
cargo test --test sqlite_test

# Expected: All tests pass
# ✓ Event log creation
# ✓ Event append
# ✓ Event replay
# ✓ Concurrent writes
```

#### Step 1.2: Workflow Database Schema (P1.2)

```bash
cd rust/shannon-api
cargo test --test workflow_store_test

# Expected: All tests pass
# ✓ Workflow CRUD operations
# ✓ Checkpoint save/load
# ✓ Migration execution
```

#### Step 1.3: Event Bus (P1.3)

```bash
cd rust/shannon-api
cargo test --test event_bus_test

# Expected: All tests pass
# ✓ Multi-subscriber support
# ✓ Event broadcasting
# ✓ Backpressure handling
```

#### Step 1.4: Engine Integration (P1.4-P1.12)

```bash
# Start desktop app in embedded mode
cd desktop
npm run tauri:dev

# In UI:
# 1. Submit a simple query: "What is 2+2?"
# 2. Verify CoT pattern executes
# 3. Verify events stream to UI
# 4. Verify workflow completes successfully
```

### Phase 2: Advanced Features

#### Step 2.1: Pattern Execution

```bash
# Test each pattern
cargo test -p workflow-patterns --test pattern_test

# Expected: All patterns pass
# ✓ Chain of Thought
# ✓ Tree of Thoughts
# ✓ Research
# ✓ ReAct
# ✓ Debate
# ✓ Reflection
```

#### Step 2.2: Deep Research

```bash
# In desktop app, submit research query:
# "Research the latest developments in AI agent frameworks"

# Verify:
# ✓ Multiple search rounds
# ✓ Coverage score improves
# ✓ Gap identification works
# ✓ Final report has citations
# ✓ Completes in <15 minutes
```

### Phase 3: Production Ready

#### Step 3.1: End-to-End Tests

```bash
cd /Users/gqadonis/Projects/prometheus/Shannon
cargo test --test e2e_task_lifecycle
cargo test --test e2e_streaming
cargo test --test e2e_api_keys

# Expected: All E2E tests pass
```

#### Step 3.2: Performance Benchmarks

```bash
cd rust/shannon-api
cargo bench

# Expected benchmarks:
# ✓ Cold start: <250ms (P99)
# ✓ Simple task: <7s (P95)
# ✓ Event latency: <150ms (P99)
# ✓ Event throughput: >3K/s
```

#### Step 3.3: Quality Audit

```bash
# Run quality checks
make fmt lint test coverage

# Expected:
# ✓ Zero compilation warnings
# ✓ Zero clippy warnings
# ✓ 100% tests passing
# ✓ ≥90% overall coverage
# ✓ 100% core engine coverage
```

---

## Integration Scenarios

### Scenario 1: Simple Task Workflow

**Objective**: Verify basic CoT pattern execution end-to-end.

**Steps**:
1. Start desktop app: `cd desktop && npm run tauri:dev`
2. Submit query: "Explain quantum entanglement in simple terms"
3. Observe:
   - Workflow starts immediately
   - Reasoning steps appear in UI
   - Final answer displays with confidence score
   - Workflow completes in <10 seconds

**Expected Result**:
- Status: Completed
- Reasoning steps: 3-5
- Confidence: >0.7
- No errors

### Scenario 2: Research Workflow

**Objective**: Verify research pattern with web search and citation.

**Steps**:
1. Submit query: "What are the key features of Manus.ai?"
2. Set mode: "research"
3. Observe:
   - Query decomposition
   - Web searches execute
   - Sources collected (8+ sources)
   - Synthesis with citations
   - Workflow completes in <5 minutes

**Expected Result**:
- Status: Completed
- Sources: ≥8 with URLs
- Citations: Present in answer
- Coverage score: ≥0.8

### Scenario 3: Pause and Resume

**Objective**: Verify workflow control signals.

**Steps**:
1. Submit complex query (10+ second execution)
2. Click "Pause" button mid-execution
3. Verify workflow pauses (events stop)
4. Click "Resume" button
5. Verify workflow continues from pause point
6. Workflow completes successfully

**Expected Result**:
- Pause: Workflow state transitions to Paused
- Resume: Workflow resumes from exact pause point
- No data loss or corruption

### Scenario 4: Crash Recovery

**Objective**: Verify workflow recovery after app crash.

**Steps**:
1. Submit long-running research query (5+ minutes)
2. Force-quit app mid-execution (kill process)
3. Restart app
4. Verify workflow recovery:
   - Workflow listed in history as "Running"
   - Click to resume
   - Workflow continues from last checkpoint
   - Completes successfully

**Expected Result**:
- Recovery time: <5 seconds
- No duplicate work
- Completes with correct output

### Scenario 5: Session Continuity

**Objective**: Verify session persistence across app restarts.

**Steps**:
1. Submit workflow and let it complete
2. Close app normally
3. Restart app
4. Navigate to workflow history
5. Verify completed workflow is visible
6. Open workflow details
7. Verify full event history and output accessible

**Expected Result**:
- Workflow history persists
- Event timeline complete
- Output and sources intact
- No data loss

---

## Troubleshooting

### Issue 1: Tests Failing

**Symptom**: `cargo test` shows failures

**Solutions**:
```bash
# Clean build
cargo clean
cargo build

# Check Rust version
rustc --version  # Should be 1.91.1+

# Update dependencies
cargo update

# Run single test with output
cargo test test_name -- --nocapture
```

### Issue 2: WASM Compilation Errors

**Symptom**: Cannot compile workflow patterns to WASM

**Solutions**:
```bash
# Install WASM target
rustup target add wasm32-wasi

# Verify target installed
rustup target list | grep wasm32-wasi

# Build for WASM
cd rust/workflow-patterns
cargo build --target wasm32-wasi
```

### Issue 3: SQLite Errors

**Symptom**: "database is locked" errors

**Solutions**:
```bash
# Check SQLite version
sqlite3 --version  # Should be 3.35+

# Enable WAL mode in code (already done):
# PRAGMA journal_mode=WAL;
# PRAGMA synchronous=NORMAL;

# If persistent, delete database and recreate:
rm ~/.shannon/embedded.db
```

### Issue 4: LLM API Errors

**Symptom**: "API key not found" or "rate limit" errors

**Solutions**:
```bash
# Verify API keys in .env
cat .env | grep API_KEY

# Test API key manually
curl https://api.openai.com/v1/models \
  -H "Authorization: Bearer $OPENAI_API_KEY"

# Check rate limits in provider dashboard
```

### Issue 5: High Memory Usage

**Symptom**: App using >500MB memory

**Solutions**:
```bash
# Check concurrent workflow limit
# Should be ≤10

# Verify event buffer size
# Should be 256 events per workflow

# Check for memory leaks with valgrind (Linux only):
valgrind --leak-check=full ./target/debug/shannon-api
```

---

## Performance Validation

### Benchmark Execution

```bash
# Run all benchmarks
cd rust/shannon-api
cargo bench --bench cold_start_bench
cargo bench --bench task_latency_bench
cargo bench --bench event_throughput_bench
cargo bench --bench memory_bench

# Generate HTML reports
open target/criterion/report/index.html
```

### Expected Results

| Benchmark | Target | Acceptable | Status |
|-----------|--------|-----------|--------|
| Cold Start | <200ms | <250ms | ⏱️ Measure |
| Simple Task | <5s | <7s | ⏱️ Measure |
| Research Task | <15min | <20min | ⏱️ Measure |
| Event Latency | <100ms | <150ms | ⏱️ Measure |
| Throughput | >5K/s | >3K/s | ⏱️ Measure |
| Memory | <150MB | <200MB | ⏱️ Measure |

---

## Next Steps After Validation

Once all validation steps pass:

1. **Review Implementation Report**: Check [`specs/002-embedded-workflow-engine/IMPLEMENTATION_REPORT.md`](IMPLEMENTATION_REPORT.md) for completion summary

2. **Run Quality Audit**: Execute final quality checks before deployment

3. **Update Documentation**: Ensure all docs reflect actual implementation

4. **Prepare Release**: Create release notes and changelog

5. **Deploy**: Follow deployment guide in [`docs/embedded-workflow-engine.md`](../../docs/embedded-workflow-engine.md)

---

## Support

For issues or questions:
- Check [Troubleshooting Guide](../../docs/troubleshooting.md)
- Review [Architecture Docs](../../docs/rust-architecture.md)
- Check [Coding Standards](../../docs/coding-standards/RUST.md)
