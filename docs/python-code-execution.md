# Python Code Execution in Shannon

## Overview

Shannon provides secure Python code execution through WebAssembly System Interface (WASI), ensuring complete sandboxing and resource isolation. This document covers the setup, usage, and architecture of the Python execution system.

## Critical Setup Requirements ⚠️

### Prerequisites - MUST Complete Before Starting

#### 1. Download Python WASI Interpreter (REQUIRED)

```bash
# This MUST be run BEFORE starting services
./scripts/setup_python_wasi.sh

# Verify the file was downloaded (should be ~20MB)
ls -lh wasm-interpreters/python-3.11.4.wasm
```

#### 2. Understanding WebAssembly Table Limits

**Important**: "Table limits" are WebAssembly runtime concepts:

- **WebAssembly Tables**: Memory structures that store function references in WASM modules
- **Python Needs Large Tables**: CPython has thousands of internal functions (5413+ entries)
- **Default Limit**: Default should be over than 5000+, Python needs 10000+

#### 3. Required Configuration Changes

The following MUST be configured in `rust/agent-core/src/wasi_sandbox.rs`:

```rust
// In WasiSandbox::with_config()
execution_timeout: app_config.wasi_timeout(),
// Python WASM requires larger table limits (5413+ elements)
table_elements_limit: 10000,  // CRITICAL: Default 1024 is too small!
instances_limit: 10,           // Multiple WASM instances needed
tables_limit: 10,              // Multiple function tables
memories_limit: 4,             // Memory regions
```

#### 4. Build and Start Services

```bash
# Rebuild agent-core with the configuration changes
docker compose -f deploy/compose/compose.yml build --no-cache agent-core

# Rebuild llm-service to include Python executor
docker compose -f deploy/compose/compose.yml build llm-service

# Start all services
make dev
```

## Quick Start (After Setup)

### 2. Execute Python Code

```bash
# Simple execution
./scripts/submit_task.sh "Execute Python: print('Hello, Shannon!')"

# Mathematical computation
./scripts/submit_task.sh "Execute Python code to calculate factorial of 10"

# Data processing
./scripts/submit_task.sh "Use Python to generate the first 20 Fibonacci numbers"
```

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                     User Request                          │
│           "Execute Python: print(2+2)"                    │
└────────────────────┬─────────────────────────────────────┘
                     ▼
┌──────────────────────────────────────────────────────────┐
│                 Orchestrator (Go)                         │
│    Routes to LLM Service based on complexity              │
└────────────────────┬─────────────────────────────────────┘
                     ▼
┌──────────────────────────────────────────────────────────┐
│                LLM Service (Python)                       │
│  ┌──────────────────────────────────────────────────┐    │
│  │         PythonWasiExecutorTool                   │    │
│  │  • Detects Python execution need                 │    │
│  │  • Prepares execution context                    │    │
│  │  • Manages session state (optional)              │    │
│  │  • Caches interpreter for performance            │    │
│  └────────────────┬─────────────────────────────────┘    │
└──────────────────┼────────────────────────────────────────┘
                   ▼
┌──────────────────────────────────────────────────────────┐
│               Agent Core (Rust)                           │
│  ┌──────────────────────────────────────────────────┐    │
│  │          WASI Sandbox (Wasmtime)                 │    │
│  │  ┌────────────────────────────────────────────┐  │    │
│  │  │    Python.wasm (CPython 3.11.4)            │  │    │
│  │  │  • Full standard library                   │  │    │
│  │  │  • Memory limit: 256MB                     │  │    │
│  │  │  • CPU limit: Configurable                 │  │    │
│  │  │  • Timeout: 30s (max 60s)                  │  │    │
│  │  │  • No network access                       │  │    │
│  │  │  • Read-only filesystem                    │  │    │
│  │  └────────────────────────────────────────────┘  │    │
│  └──────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────┘
```

## Features

### ✅ Production Features

- **Full Python Standard Library**: Complete CPython 3.11.4 with all built-in modules
- **Session Persistence**: Maintain variables across executions with session IDs
- **Performance Optimization**: Cached interpreter reduces startup from 500ms to 50ms
- **Resource Limits**: Configurable memory, CPU, and timeout limits
- **Security Isolation**: True sandboxing via WASI - no network, no filesystem writes
- **Output Streaming**: Progressive output for long-running computations
- **Error Handling**: Comprehensive error messages and timeout protection

### 🚀 Advanced Capabilities

- **Persistent Sessions**: Variables and imports persist across executions
- **Custom Timeouts**: Adjustable from 1 to 60 seconds per execution
- **Stdin Support**: Provide input data to Python scripts
- **Performance Metrics**: Execution time tracking and reporting

## Usage Examples

### Basic Python Execution

```python
# Request
"Execute Python: print('Hello, World!')"

# Output
Hello, World!
```

### Mathematical Computation

```python
# Request
"Execute Python code to calculate the factorial of 10"

# Generated Code
import math
result = math.factorial(10)
print(f"10! = {result}")

# Output
10! = 3628800
```

### Data Processing

```python
# Request
"Generate the first 20 Fibonacci numbers using Python"

# Generated Code
def fibonacci(n):
    fib = [0, 1]
    for i in range(2, n):
        fib.append(fib[-1] + fib[-2])
    return fib

result = fibonacci(20)
print(f"First 20 Fibonacci numbers: {result}")

# Output
First 20 Fibonacci numbers: [0, 1, 1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233, 377, 610, 987, 1597, 2584, 4181]
```

### Session Persistence

```python
# Request 1 (with session_id: "data-analysis")
"Execute Python: data = [1, 2, 3, 4, 5]"

# Request 2 (same session_id)
"Execute Python: import statistics; print(statistics.mean(data))"

# Output
3
```

## API Integration

### Direct API Call

```bash
curl -X POST http://localhost:8000/agent/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Execute Python: print(sum(range(100)))",
    "tools": ["python_executor"],
    "mode": "standard"
  }'
```

### Tool Parameters

```json
{
  "tool": "python_executor",
  "parameters": {
    "code": "print('Hello')",
    "session_id": "optional-session-id",
    "timeout_seconds": 30,
    "stdin": "optional input data"
  }
}
```

## Configuration

### Environment Variables

```bash
# Python WASI interpreter path (required)
PYTHON_WASI_WASM_PATH=/opt/wasm-interpreters/python-3.11.4.wasm

# Agent Core address (for gRPC communication)
AGENT_CORE_ADDR=agent-core:50051
```

### Resource Limits Configuration

#### In Code (rust/agent-core/src/wasi_sandbox.rs)

```rust
// CRITICAL: These values MUST be set for Python WASM to work!
pub fn with_config(app_config: &Config) -> Result<Self> {
    // ...
    Ok(Self {
        // ...
        // Python WASM requires larger table limits (5413+ elements)
        table_elements_limit: 10000,  // MUST be ≥ 5413 for Python
        instances_limit: 10,           // MUST be ≥ 4
        tables_limit: 10,              // MUST be ≥ 4
        memories_limit: 4,             // MUST be ≥ 2
    })
}
```

#### Environment Variables (docker-compose.yml)

```yaml
agent-core:
  environment:
    - WASI_MEMORY_LIMIT_MB=512      # Memory for Python execution
    - WASI_TIMEOUT_SECONDS=60        # Max execution time
    - PYTHON_WASI_WASM_PATH=/opt/wasm-interpreters/python-3.11.4.wasm

llm-service:
  environment:
    - PYTHON_WASI_WASM_PATH=/opt/wasm-interpreters/python-3.11.4.wasm
    - AGENT_CORE_ADDR=agent-core:50051
```

## Security Model

### Sandboxing Guarantees

| Feature              | Status      | Description                                |
| -------------------- | ----------- | ------------------------------------------ |
| **Memory Isolation** | ✅ Enforced  | Separate linear memory space per execution |
| **Network Access**   | ❌ Blocked   | No network capabilities granted            |
| **Filesystem**       | 🔒 Read-only | Limited to whitelisted paths               |
| **Process Spawning** | ❌ Blocked   | Cannot create subprocesses                 |
| **Resource Limits**  | ✅ Enforced  | Memory, CPU, and time limits               |
| **System Calls**     | ❌ Blocked   | No access to host system calls             |

### Why WASI?

1. **True Isolation**: WebAssembly provides hardware-level isolation
2. **Deterministic**: Same code produces same results across platforms
3. **Resource Control**: Fine-grained control over memory and CPU usage
4. **No Escape**: Cannot break out of sandbox even with malicious code
5. **Industry Standard**: Used by Cloudflare Workers, Fastly, and others

## Supported Python Features

### ✅ Fully Supported

- All Python 3.11 syntax and features
- Standard library modules:
  - `math`, `statistics`, `random`
  - `json`, `csv`, `xml`
  - `datetime`, `calendar`, `time`
  - `re`, `string`, `textwrap`
  - `collections`, `itertools`, `functools`
  - `hashlib`, `base64`, `binascii`
  - `decimal`, `fractions`
  - `pathlib`, `os.path` (read-only)

### ⚠️ Limited Support

- `io`: Only in-memory operations
- `sqlite3`: In-memory databases only
- File operations: Read-only access to `/tmp`

### ❌ Not Supported

- Network operations (`requests`, `urllib`, `socket`)
- Package installation (`pip install`)
- Native extensions (C modules)
- GUI libraries (`tkinter`, `pygame`)
- Multiprocessing/threading
- System operations (`subprocess`, `os.system`)

## Performance Characteristics

| Operation                     | Time   | Memory |
| ----------------------------- | ------ | ------ |
| Interpreter Load (first time) | ~500ms | 20MB   |
| Interpreter Load (cached)     | ~50ms  | 0MB    |
| Simple print                  | ~100ms | 50MB   |
| Factorial(1000)               | ~150ms | 55MB   |
| Sort 10,000 numbers           | ~200ms | 60MB   |
| Process 1MB JSON              | ~300ms | 80MB   |

## Troubleshooting

### Common Issues and Solutions

#### 1. "Failed to instantiate WASM module: table minimum size of 5413 elements exceeds table limits"

**Cause**: WebAssembly table limits are too small for Python WASM.

**Solution**: Update `rust/agent-core/src/wasi_sandbox.rs`:

```rust
table_elements_limit: 10000,  // Increase from default 1024
```

Then rebuild: `docker compose build agent-core`

#### 2. "Python WASI interpreter not found"

**Cause**: Python WASM file not downloaded.

**Solution**:

```bash
# Download the interpreter
./scripts/setup_python_wasi.sh

# Verify it exists (should be ~20MB)
ls -lh wasm-interpreters/python-3.11.4.wasm
```

#### 3. "can't open file '//import sys; exec(sys.stdin.read())': [Errno 44] No such file or directory"

**Cause**: Incorrect argv format for Python WASM.

**Solution**: Ensure `python_wasi_executor.py` uses correct argv:

```python
"argv": ["python", "-c", "import sys; exec(sys.stdin.read())"],  # Note: argv[0] must be "python"
```

#### 4. "Execution timeout"

**Cause**: Python code takes longer than timeout limit.

**Solution**:

```python
# Increase timeout in request (max 60 seconds)
{
  "code": "long_running_code()",
  "timeout_seconds": 60
}
```

#### 5. "Memory limit exceeded"

**Cause**: Python code uses more than allocated memory.

**Solution**: Increase memory limit in environment variables:

```bash
# In docker-compose.yml for agent-core
environment:
  - WASI_MEMORY_LIMIT_MB=512  # Increase from 256
```

#### 6. "Module not found"

**Cause**: Trying to import external packages (numpy, pandas, etc.)

**Solution**: Only Python standard library is available. External packages must be reimplemented using stdlib.

#### 7. "WASI execution error" with no details

**Cause**: Various issues - check agent-core logs.

**Solution**:

```bash
# Check detailed logs
docker compose logs agent-core --tail 100 | grep -E "WASI|Python|code_executor"

# Enable debug logging in docker-compose.yml
environment:
  - RUST_LOG=debug,shannon_agent_core::wasi_sandbox=debug
```

## Best Practices

1. **Keep Code Simple**: Complex operations increase execution time
2. **Use Sessions Wisely**: Sessions consume memory - clear when done
3. **Handle Timeouts**: Long computations should be broken into steps
4. **Minimize Imports**: Each import adds overhead
5. **Batch Operations**: Process data in chunks for better performance

## Comparison with Alternatives

| Feature             | Shannon WASI | Docker Container | AWS Lambda  | Google Colab |
| ------------------- | ------------ | ---------------- | ----------- | ------------ |
| **Security**        | ⭐⭐⭐⭐⭐        | ⭐⭐⭐              | ⭐⭐⭐⭐        | ⭐⭐⭐          |
| **Startup Time**    | 50-100ms     | 1-5s             | 100-500ms   | 5-10s        |
| **Memory Overhead** | 50MB         | 200MB+           | 128MB+      | 1GB+         |
| **Package Support** | Stdlib only  | Full             | Full        | Full         |
| **Network Access**  | No           | Yes              | Yes         | Yes          |
| **Cost**            | Minimal      | Medium           | Per-request | Free/Paid    |
| **Deterministic**   | Yes          | No               | Mostly      | No           |

## Future Roadmap

### Planned Enhancements

1. **Package Support**: Pre-compiled NumPy, Pandas via Pyodide
2. **Multi-language**: JavaScript (QuickJS), Ruby (ruby.wasm)
3. **Debugging**: Step-through debugging support
4. **Visualization**: Matplotlib output via base64 images
5. **Distributed Execution**: Multi-node execution for large tasks

## Technical Details

### Interpreter: CPython 3.11.4

- **Source**: [VMware WebAssembly Language Runtimes](https://github.com/vmware-labs/webassembly-language-runtimes)
- **Size**: 20MB compressed
- **Python Version**: 3.11.4
- **Build Date**: 2023-07-14
- **Compatibility**: 100% CPython compatible

### WASI Runtime: Wasmtime

- **Version**: Latest stable
- **Features**: WASI Preview 1
- **Security**: Capability-based security model
- **Performance**: Near-native execution speed

## Getting Help

### Resources

- [WebAssembly Specification](https://webassembly.github.io/spec/)
- [WASI Documentation](https://wasi.dev/)
- [Python WASM Guide](https://github.com/vmware-labs/webassembly-language-runtimes/tree/main/python)
- [Shannon Documentation](https://github.com/Kocoro-lab/shannon)

### Support

- GitHub Issues: [shannon/issues](https://github.com/Kocoro-lab/shannon/issues)
- Documentation: [docs/](../docs/)

---

*Last updated: January 2025*