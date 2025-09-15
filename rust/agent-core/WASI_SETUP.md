# WASI Code Executor Setup Guide

## Overview

Shannon's Rust Agent Core provides a secure WebAssembly System Interface (WASI) sandbox for code execution. This sandbox offers:

- **Security Isolation**: Memory-safe execution with resource limits
- **Deterministic Execution**: Consistent behavior across platforms
- **Resource Control**: Configurable memory, CPU, and timeout limits
- **Filesystem Safety**: Read-only access with whitelisted paths
- **Network Isolation**: No network access at WASI capability level

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Platform-Specific Setup](#platform-specific-setup)
3. [Basic Usage](#basic-usage)
4. [Advanced Usage](#advanced-usage)
5. [Integration Guide](#integration-guide)
6. [Configuration](#configuration)
7. [Troubleshooting](#troubleshooting)
8. [Security Considerations](#security-considerations)
9. [Examples](#examples)

## Prerequisites

### Required Components

- Rust 1.70+ with cargo
- Docker & Docker Compose (for full platform)
- WebAssembly toolchain (platform-specific, see below)

### WASI Limitations

⚠️ **Important**: The WASI sandbox executes WebAssembly modules only. It does NOT include:
- Python interpreter
- JavaScript runtime
- Shell/bash commands
- Native binaries

To execute Python or other languages, you need to compile them to WebAssembly first (e.g., using Pyodide for Python).

## Platform-Specific Setup

### macOS Setup

```bash
# Option 1: Install wabt (WebAssembly Binary Toolkit)
brew install wabt

# Verify installation
wat2wasm --version

# Option 2: Install wasm-tools (Rust-based)
cargo install wasm-tools

# Option 3: Install wasmtime CLI (includes additional tools)
brew install wasmtime
```

### Linux Setup

```bash
# Ubuntu/Debian
sudo apt-get update
sudo apt-get install wabt

# Fedora/RHEL
sudo dnf install wabt

# Arch Linux
sudo pacman -S wabt

# Alternative: Install from GitHub releases
wget https://github.com/WebAssembly/wabt/releases/download/1.0.34/wabt-1.0.34-linux.tar.gz
tar xzf wabt-1.0.34-linux.tar.gz
export PATH=$PATH:$(pwd)/wabt-1.0.34/bin
```

### Windows Setup

```powershell
# Option 1: Using Chocolatey
choco install wabt

# Option 2: Using Scoop
scoop install wabt

# Option 3: Download from GitHub
# Visit: https://github.com/WebAssembly/wabt/releases
# Download wabt-1.0.34-windows.zip and extract to PATH
```

## Basic Usage

### 1. Compile WebAssembly Text Format to Binary

```bash
# Compile the example hello-wasi.wat file
wat2wasm docs/assets/hello-wasi.wat -o /tmp/hello-wasi.wasm

# Verify the generated WASM file
file /tmp/hello-wasi.wasm
# Output: /tmp/hello-wasi.wasm: WebAssembly (wasm) binary module version 0x1 (MVP)
```

### 2. Test with Example CLI

```bash
cd rust/agent-core

# Run the WASI hello example
cargo run --example wasi_hello -- /tmp/hello-wasi.wasm

# Expected output:
# Hello from WASI!
```

### 3. Test via Rust Tests

```bash
cd rust/agent-core

# Run the base64 payload test
RUST_LOG=info cargo test test_code_executor_with_base64_payload -- --nocapture

# Run all WASI-related tests
cargo test wasi -- --nocapture
```

## Python WASI Integration

### Overview

Shannon supports executing Python code in the WASI sandbox through two approaches:

1. **Python Interpreter WASM**: Using a full Python interpreter compiled to WebAssembly
2. **Python-to-WASM Transpilation**: Converting simple Python code directly to WASM

### Python Interpreter Options

#### 1. Python.wasm (VMware Labs)
- **Size**: ~20MB
- **Version**: Python 3.11.4
- **Compatibility**: Full CPython standard library
- **Best for**: Standard Python scripts

```bash
# Download Python WASI interpreter
mkdir -p /tmp/python-wasi
cd /tmp/python-wasi
curl -LO https://github.com/vmware-labs/webassembly-language-runtimes/releases/download/python%2F3.11.4%2B20230714-11be424/python-3.11.4.wasm

# Verify download
ls -lh python-3.11.4.wasm
# Should show ~20MB file
```

#### 2. Pyodide
- **Size**: 20MB+ (core + packages)
- **Features**: NumPy, Pandas, SciPy support
- **Best for**: Data science workloads

```bash
# Download Pyodide (browser-focused, needs adaptation)
curl -LO https://github.com/pyodide/pyodide/releases/latest/download/pyodide-core.tar.bz2
tar -xjf pyodide-core.tar.bz2
```

#### 3. RustPython WASM
- **Size**: 5-10MB
- **Compatibility**: ~95% CPython compatible
- **Best for**: Lightweight scripts, Rust ecosystem

```bash
# Download RustPython WASM
curl -LO https://github.com/RustPython/RustPython/releases/latest/download/rustpython.wasm
```

### Integration with Shannon

#### Method 1: Using python_wasi_runner Tool

The `python_wasi_runner` tool in the LLM service bridges Python code execution to the Rust WASI sandbox:

```python
# In Shannon's workflow, the tool expects:
{
    "tool": "python_wasi_runner",
    "parameters": {
        "code": "print('Hello from Python')",
        "interpreter_wasm_path": "/tmp/python.wasm",  # OR
        "interpreter_wasm_base64": "<base64-encoded-wasm>",
        "stdin": "",  # Optional input
        "argv": []    # Optional arguments
    }
}
```

#### Method 2: Direct code_executor Usage

For direct WASI execution, use the Rust `code_executor` tool:

```python
# The code_executor expects:
{
    "tool": "code_executor",
    "parameters": {
        "wasm_base64": "<base64-encoded-wasm>",  # OR
        "wasm_path": "/path/to/module.wasm",
        "stdin": "input data"
    }
}
```

### Setup Instructions

#### For Docker Deployment

1. **Copy Python WASM to container**:
```bash
# Download locally first
curl -LO https://github.com/vmware-labs/webassembly-language-runtimes/releases/download/python%2F3.11.4%2B20230714-11be424/python-3.11.4.wasm

# Copy to LLM service container
docker cp python-3.11.4.wasm shannon-llm-service-1:/opt/python.wasm

# Copy to Agent Core container (if needed)
docker cp python-3.11.4.wasm shannon-agent-core-1:/opt/python.wasm
```

2. **Set environment variable in docker-compose.yml**:
```yaml
services:
  llm-service:
    environment:
      - PYTHON_WASI_WASM_PATH=/opt/python.wasm

  agent-core:
    volumes:
      - ./wasm-interpreters:/opt/wasm-interpreters:ro
```

3. **Or mount as volume**:
```yaml
services:
  llm-service:
    volumes:
      - /tmp/python-wasi/python-3.11.4.wasm:/opt/python.wasm:ro
```

#### For Local Development

```bash
# Set environment variable
export PYTHON_WASI_WASM_PATH=/tmp/python-wasi/python-3.11.4.wasm

# Or configure in .env file
echo "PYTHON_WASI_WASM_PATH=/tmp/python-wasi/python-3.11.4.wasm" >> .env
```

### Testing Python WASI

#### Test 1: Simple Print Statement
```bash
# Submit via API
./scripts/submit_task.sh 'Use python_wasi_runner to execute: print("Hello Shannon")'
```

#### Test 2: Mathematical Computation
```bash
# Create test script
cat > /tmp/test.py << 'EOF'
import math
result = math.factorial(10)
print(f"10! = {result}")
EOF

# Execute via Shannon
./scripts/submit_task.sh 'Execute the Python code that calculates factorial of 10'
```

#### Test 3: Direct WASI Test
```bash
# Test Python WASM directly with our WASI sandbox
cd rust/agent-core
echo 'print("Test")' | cargo run --example wasi_hello -- /tmp/python-wasi/python-3.11.4.wasm -- -c 'import sys; exec(sys.stdin.read())'
```

### Limitations and Considerations

#### Current Limitations

1. **Python.wasm Limitations**:
   - No pip/package installation at runtime
   - Limited to pre-compiled standard library
   - No native extensions (C modules)
   - No network access (WASI security)
   - No filesystem write access (configurable)

2. **Performance Considerations**:
   - ~20MB interpreter must be loaded for each execution
   - Startup overhead: ~100-500ms
   - Memory usage: 256MB limit (configurable)

3. **Integration Status**:
   - `python_wasi_runner` tool exists but needs proper argv handling
   - Full Python interpreter invocation is complex
   - Simple expressions work best

#### Future Improvements

1. **Interpreter Caching**: Cache loaded interpreter in memory
2. **Package Support**: Pre-bundle common packages
3. **Streaming Output**: Support real-time output streaming
4. **Debugging**: Add Python debugger support

### Alternative: Python-to-WASM Transpilation

For simple Python expressions, consider transpiling directly to WASM:

```python
# Simple Python code
print("Hello")
print(2 + 2)

# Transpiles to WAT:
(module
  (import "wasi_snapshot_preview1" "fd_write" ...)
  (memory 1)
  (data (i32.const 100) "Hello\n")
  (data (i32.const 110) "4\n")
  (func $main (export "_start")
    ;; Print "Hello"
    ;; Print "4"
  )
)
```

This approach is faster but limited to basic operations.

## Advanced Usage

### Using Base64-Encoded WASM

```bash
# Encode WASM file as base64
BASE64_WASM=$(base64 -b 0 /tmp/hello-wasi.wasm)  # macOS
# OR
BASE64_WASM=$(base64 -w 0 /tmp/hello-wasi.wasm)  # Linux

# Use in tool execution (example)
echo "Base64 WASM: ${BASE64_WASM:0:50}..."  # Show first 50 chars
```

### Creating Custom WASM Modules

#### Simple Example (WAT format)

Create `custom.wat`:
```wat
(module
  (import "wasi_snapshot_preview1" "fd_write"
    (func $fd_write (param i32 i32 i32 i32) (result i32)))

  (memory 1)
  (export "memory" (memory 0))

  (data (i32.const 8) "Custom WASI Module\n")

  (func $main (export "_start")
    ;; Create iovec structure
    (i32.store (i32.const 0) (i32.const 8))   ;; buf pointer
    (i32.store (i32.const 4) (i32.const 19))  ;; buf length

    ;; Write to stdout (fd=1)
    (call $fd_write
      (i32.const 1)  ;; stdout
      (i32.const 0)  ;; iovs pointer
      (i32.const 1)  ;; iovs_len
      (i32.const 20) ;; nwritten pointer
    )
    drop
  )
)
```

Compile and run:
```bash
wat2wasm custom.wat -o custom.wasm
cargo run --example wasi_hello -- custom.wasm
```

#### Using Rust to Create WASM

Create `rust-wasm/Cargo.toml`:
```toml
[package]
name = "wasi-example"
version = "0.1.0"
edition = "2021"

[dependencies]

[lib]
crate-type = ["cdylib"]
```

Create `rust-wasm/src/main.rs`:
```rust
fn main() {
    println!("Hello from Rust WASI!");
}
```

Build and run:
```bash
cd rust-wasm
cargo build --target wasm32-wasi --release
cd ../rust/agent-core
cargo run --example wasi_hello -- ../../rust-wasm/target/wasm32-wasi/release/wasi_example.wasm
```

## Integration Guide

### Via gRPC Tool Execution

The WASI sandbox integrates with Shannon's tool system:

```protobuf
// Tool execution request
message ToolCallRequest {
  string tool_name = 1;  // Use "code_executor"
  google.protobuf.Struct parameters = 2;
}

// Parameters for code_executor:
// - wasm_base64: Base64-encoded WASM module (preferred)
// - wasm_path: Path to WASM file (alternative)
// - stdin: Optional input data
```

### Example gRPC Call

```bash
# Using grpcurl (requires agent-core running on port 50051)
grpcurl -plaintext -d '{
  "tool_name": "code_executor",
  "parameters": {
    "wasm_base64": "'$(base64 -b 0 /tmp/hello-wasi.wasm)'",
    "stdin": ""
  }
}' localhost:50051 shannon.agent.AgentService/ExecuteTool
```

### Integration with Orchestrator

The orchestrator can route code execution tasks to the WASI sandbox:

```go
// In Go orchestrator
result, err := agentClient.ExecuteTool(ctx, &pb.ToolCallRequest{
    ToolName: "code_executor",
    Parameters: map[string]interface{}{
        "wasm_base64": base64WasmModule,
        "stdin": inputData,
    },
})
```

## Configuration

### Resource Limits

Edit `config/shannon.yaml`:

```yaml
wasi:
  # Memory limit in bytes (default: 256MB)
  memory_limit_bytes: 268435456

  # CPU fuel limit (computational steps)
  max_fuel: 100000000

  # Execution timeout in milliseconds
  execution_timeout_ms: 30000

  # Allowed filesystem paths (read-only)
  allowed_paths:
    - "/tmp"
    - "/data/readonly"

  # Environment variables (if needed)
  env_vars:
    HOME: "/tmp"
    PATH: "/usr/bin"
```

### Security Settings

```yaml
wasi:
  # Sandbox security options
  security:
    # Disable network capabilities
    allow_network: false

    # File system access mode
    fs_readonly: true

    # Process spawning
    allow_subprocess: false

    # Clock access
    allow_clock: true

    # Random number generation
    allow_random: true
```

## Troubleshooting

### Common Issues

#### 1. "wat2wasm: command not found"

**Solution**: Install WebAssembly tools
```bash
# macOS
brew install wabt

# Linux
sudo apt-get install wabt
```

#### 2. "WASM module too large"

**Issue**: Default size limit is 50MB

**Solution**: Split your module or optimize size
```bash
# Check WASM size
ls -lh your-module.wasm

# Optimize with wasm-opt
wasm-opt -Os input.wasm -o output.wasm
```

#### 3. "Execution timeout"

**Issue**: Module exceeded 30-second timeout

**Solution**: Increase timeout in config or optimize code
```yaml
wasi:
  execution_timeout_ms: 60000  # 60 seconds
```

#### 4. "Memory limit exceeded"

**Issue**: Module requested more than 256MB memory

**Solution**: Increase limit or reduce memory usage
```yaml
wasi:
  memory_limit_bytes: 536870912  # 512MB
```

### Debug Logging

Enable detailed logging:
```bash
# Set Rust log level
export RUST_LOG=shannon_agent_core=debug,wasi_sandbox=trace

# Run with verbose output
cargo run --example wasi_hello -- /tmp/test.wasm
```

## Security Considerations

### Sandbox Guarantees

✅ **Enforced**:
- Memory isolation (separate linear memory)
- CPU limits (fuel mechanism)
- No network access (capability not granted)
- Read-only filesystem (configurable paths)
- No process spawning
- Resource limits (memory, time, fuel)

⚠️ **Not Enforced**:
- Side-channel timing attacks
- Speculative execution vulnerabilities
- Resource exhaustion via legitimate operations

### Best Practices

1. **Validate Input**: Always validate WASM modules before execution
2. **Set Limits**: Configure appropriate resource limits
3. **Audit Modules**: Review WASM source before deployment
4. **Monitor Usage**: Track resource consumption metrics
5. **Update Dependencies**: Keep Wasmtime runtime updated

## Examples

### Example 1: Hello World

Located at: `docs/assets/hello-wasi.wat`

```bash
# Compile and run
wat2wasm docs/assets/hello-wasi.wat -o /tmp/hello.wasm
cd rust/agent-core
cargo run --example wasi_hello -- /tmp/hello.wasm
```

### Example 2: Reading Input

Create `input-reader.wat`:
```wat
(module
  (import "wasi_snapshot_preview1" "fd_read"
    (func $fd_read (param i32 i32 i32 i32) (result i32)))
  (import "wasi_snapshot_preview1" "fd_write"
    (func $fd_write (param i32 i32 i32 i32) (result i32)))

  (memory 1)
  (export "memory" (memory 0))

  (func $main (export "_start")
    ;; Read from stdin and echo to stdout
    ;; Implementation details...
  )
)
```

### Example 3: Base64 Integration Test

```bash
cd rust/agent-core

# Run the existing test
cargo test test_code_executor_with_base64_payload -- --nocapture

# View test implementation
cat src/tools.rs | grep -A 50 "test_code_executor_with_base64_payload"
```

## Performance Benchmarks

Typical execution times on modern hardware:

| Operation | Time | Memory |
|-----------|------|--------|
| WASM load & validate | ~1ms | 1MB |
| Simple hello world | ~5ms | 4MB |
| Complex computation (factorial 1000) | ~50ms | 8MB |
| Memory-intensive (array sorting) | ~200ms | 64MB |

## Future Enhancements

### Planned Features

1. **Language Support**:
   - Python via Pyodide
   - JavaScript via QuickJS
   - Ruby via ruby.wasm

2. **Capabilities**:
   - Controlled network access
   - Persistent storage
   - Inter-module communication

3. **Developer Tools**:
   - WASM debugger integration
   - Performance profiler
   - Module marketplace

## Additional Resources

### Documentation
- [WebAssembly Specification](https://webassembly.github.io/spec/)
- [WASI Documentation](https://wasi.dev/)
- [Wasmtime Book](https://docs.wasmtime.dev/)
- [WABT Tools Guide](https://github.com/WebAssembly/wabt)

### Examples
- [WASI Examples Repository](https://github.com/wasmtime/wasmtime/tree/main/examples)
- [Rust WASM Tutorial](https://rustwasm.github.io/book/)
- [AssemblyScript for WASM](https://www.assemblyscript.org/)

### Community
- [WebAssembly Discord](https://discord.gg/webassembly)
- [WASI Subgroup](https://github.com/WebAssembly/WASI)
- [Bytecode Alliance](https://bytecodealliance.org/)

---

For questions or issues, please refer to the main Shannon documentation or open an issue in the repository.