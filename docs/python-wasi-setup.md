# Python WASI Setup Guide for Shannon

## Quick Setup

### 1. Download Python WASI Interpreter

```bash
# Run the setup script
./scripts/setup_python_wasi.sh

# Or manually download
mkdir -p wasm-interpreters
curl -L -o wasm-interpreters/python-3.11.4.wasm \
  https://github.com/vmware-labs/webassembly-language-runtimes/releases/download/python%2F3.11.4%2B20230714-11be424/python-3.11.4.wasm
```

### 2. Configure Environment

Add to your `.env` file:
```bash
# Python WASI interpreter path
PYTHON_WASI_WASM_PATH=/opt/wasm-interpreters/python-3.11.4.wasm
```

### 3. Restart Services

```bash
# Rebuild and restart services
docker compose -f deploy/compose/compose.yml down
docker compose -f deploy/compose/compose.yml up -d --build
```

## Testing Python WASI Execution

### Test 1: Simple Python Code
```bash
./scripts/submit_task.sh "Execute this Python code: print('Hello from WASI Python')"
```

### Test 2: Mathematical Operations
```bash
./scripts/submit_task.sh "Execute Python code to calculate factorial of 10"
```

### Test 3: Data Processing
```bash
./scripts/submit_task.sh "Use Python to generate the first 20 Fibonacci numbers"
```

## How It Works

1. **Python Code Submission**: User submits Python code via the orchestrator
2. **LLM Service Processing**: The LLM service identifies Python execution need
3. **WASI Sandbox Execution**: Code runs in the secure WASI sandbox via Agent Core
4. **Result Return**: Output is captured and returned to the user

## Architecture

```
User Request → Orchestrator → LLM Service → Agent Core (WASI) → Python.wasm
                                               ↓
                                        Secure Sandbox
                                          - No network
                                          - Read-only FS
                                          - Memory limits
```

## Python Interpreter Options

### Option 1: Python.wasm (Recommended)
- **Size**: ~20MB
- **Python Version**: 3.11.4
- **Standard Library**: Full CPython
- **Download**: See setup script

### Option 2: RustPython (Lightweight)
- **Size**: 5-10MB
- **Compatibility**: ~95% CPython
- **Best for**: Simple scripts

```bash
# Download RustPython
curl -L -o wasm-interpreters/rustpython.wasm \
  https://github.com/RustPython/RustPython/releases/latest/download/rustpython.wasm
```

### Option 3: Pyodide (Data Science)
- **Size**: 20MB+
- **Features**: NumPy, Pandas support
- **Note**: Requires adaptation for WASI

## Configuration Details

### Docker Compose Configuration

The `deploy/compose/compose.yml` already includes:
```yaml
services:
  agent-core:
    volumes:
      - ./wasm-interpreters:/opt/wasm-interpreters:ro
    environment:
      - PYTHON_WASI_WASM_PATH=/opt/wasm-interpreters/python-3.11.4.wasm

  llm-service:
    volumes:
      - ./wasm-interpreters:/opt/wasm-interpreters:ro
    environment:
      - PYTHON_WASI_WASM_PATH=/opt/wasm-interpreters/python-3.11.4.wasm
```

### Local Development

For local development without Docker:
```bash
export PYTHON_WASI_WASM_PATH=$(pwd)/wasm-interpreters/python-3.11.4.wasm
```

## Limitations

### Current Limitations
- No pip/package installation at runtime
- Limited to standard library
- No native C extensions
- No network access (security feature)
- Read-only filesystem access
- 256MB memory limit (configurable)

### Supported Python Features
- ✅ Standard library (math, json, datetime, etc.)
- ✅ File operations (read-only)
- ✅ String manipulation
- ✅ Data structures
- ✅ Regular expressions
- ✅ Base64 encoding/decoding

### Not Supported
- ❌ pip install
- ❌ Network requests
- ❌ Database connections
- ❌ GUI libraries
- ❌ System calls
- ❌ Multi-threading

## Troubleshooting

### Issue: "Python WASI interpreter not found"
**Solution**: Ensure the WASM file is downloaded and path is correct
```bash
ls -la wasm-interpreters/python-3.11.4.wasm
```

### Issue: "Execution timeout"
**Solution**: Python startup takes ~100-500ms. Complex operations may need timeout adjustment:
```yaml
# In config/shannon.yaml
wasi:
  execution_timeout_ms: 60000  # 60 seconds
```

### Issue: "Memory limit exceeded"
**Solution**: Increase memory limit in config:
```yaml
wasi:
  memory_limit_bytes: 536870912  # 512MB
```

### Issue: "Module not found"
**Solution**: Only standard library modules are available. External packages must be pre-bundled.

## Advanced Usage

### Custom Python Environment

To create a custom Python environment with additional packages:

1. Build custom Python WASM with packages
2. Replace the default interpreter
3. Update PYTHON_WASI_WASM_PATH

### Direct WASI Testing

Test Python WASM directly:
```bash
cd rust/agent-core
echo 'print("Test")' | wasmtime run \
  --dir=/tmp \
  /path/to/python-3.11.4.wasm -- -c 'import sys; exec(sys.stdin.read())'
```

## Security Notes

The WASI sandbox provides strong isolation:
- **Memory Safety**: Separate linear memory space
- **No Network**: Network capabilities not granted
- **Filesystem**: Read-only access to whitelisted paths
- **Resource Limits**: CPU, memory, and time limits enforced
- **No System Calls**: Cannot spawn processes or access system

## Performance Considerations

- **Startup Time**: ~100-500ms for Python interpreter
- **Memory Usage**: Base ~50MB + your code
- **Execution Speed**: ~50-70% of native Python
- **Best Practices**:
  - Keep code simple and focused
  - Avoid large data structures
  - Use efficient algorithms
  - Minimize imports

## Examples

### Example 1: Data Processing
```python
# Fibonacci sequence
def fibonacci(n):
    fib = [0, 1]
    for i in range(2, n):
        fib.append(fib[-1] + fib[-2])
    return fib[:n]

print(fibonacci(20))
```

### Example 2: JSON Processing
```python
import json

data = {"users": [{"name": "Alice", "age": 30}, {"name": "Bob", "age": 25}]}
processed = json.dumps(data, indent=2)
print(processed)
```

### Example 3: Mathematical Computation
```python
import math

# Calculate compound interest
principal = 1000
rate = 0.05
time = 10
amount = principal * math.pow((1 + rate), time)
print(f"Amount after {time} years: ${amount:.2f}")
```

## Next Steps

1. Run `./scripts/setup_python_wasi.sh` to download the interpreter
2. Test with simple Python code submissions
3. Monitor logs for any issues
4. Adjust resource limits as needed

For more details, see:
- [WASI Setup Guide](../rust/agent-core/WASI_SETUP.md)
- [Agent Core Architecture](agent-core-architecture.md)
- [Multi-Agent Workflow](multi-agent-workflow-architecture.md)