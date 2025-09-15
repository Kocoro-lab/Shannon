# E2E Test Suite Documentation

## Overview
This directory contains end-to-end tests for the Shannon platform, focusing on tool execution and workflow integration.

## Test Files Created

### 1. calculator_test.sh
Tests the calculator tool functionality:
- Simple arithmetic (handled by LLM directly)
- Complex calculations (should trigger calculator tool)
- Direct calculator tool API execution
- Verifies tool registration

**Key Finding**: Simple math is intentionally handled by LLM directly, not tools.

### 2. python_execution_test.sh
Tests Python code execution through WASI sandbox:
- Direct WASM module compilation and execution
- Base64-encoded WASM payload testing
- Workflow integration with code_executor
- Fibonacci WASM module execution

**Key Finding**: System correctly processes WASM but lacks Python-to-WASM bridge.

### 3. python_interpreter_test.sh
Tests full Python interpreter integration:
- Checks for Python WASM interpreter (~20MB)
- Tests limitations of current architecture
- Documents requirements for proper Python support

**Key Finding**: code_executor expects standalone WASM, not interpreters needing arguments.

### 4. submit_and_get_response.sh
Helper script for task submission:
- Submits tasks via gRPC
- Polls for completion
- Retrieves results from database

## Running Tests

```bash
# Run individual tests
./tests/e2e/calculator_test.sh
./tests/e2e/python_execution_test.sh
./tests/e2e/python_interpreter_test.sh

# Prerequisites
- All services running (make dev)
- wat2wasm installed (brew install wabt)
- Python WASM interpreter (optional, ~20MB)
```

## Architecture Insights

### Tool Execution Flow
1. Decomposition suggests tools (e.g., "code_executor")
2. Rust core handles certain tools directly (calculator, code_executor)
3. Other tools forwarded to LLM service's /tools/execute
4. WASI sandbox executes WASM modules with restrictions

### Current Limitations
- No Python-to-WASM transpilation
- python_wasi_runner mentioned in docs but not implemented
- WASI sandbox can't pass command-line arguments to interpreters
- Python interpreter requires special handling not yet supported

## Future Improvements

1. **Implement python_wasi_runner tool**
   - Handle Python interpreter invocation
   - Pass code via stdin or arguments
   - Manage interpreter lifecycle

2. **Enhanced WASI Executor**
   - Support command-line arguments
   - File system mounting for scripts
   - Better interpreter integration

3. **Python Transpilation**
   - Direct Python → WASM conversion
   - Avoid interpreter overhead
   - Better performance for simple scripts