# Python Executor

Shannon provides a secure Python execution environment with two backend options: WASI (WebAssembly) for fast, lightweight execution and Firecracker for full data science workloads.

## Backend Comparison

| Feature | WASI | Firecracker |
|---------|------|-------------|
| **Boot Time** | ~10ms | ~150ms |
| **Packages** | Python stdlib only | numpy, pandas, scipy, scikit-learn, matplotlib, polars, pyarrow |
| **Memory** | 256MB default | 1GB default |
| **Timeout** | 30s default | 300s default |
| **Isolation** | WebAssembly sandbox | microVM with KVM |
| **Use Case** | Quick scripts, calculations | Data analysis, ML, large datasets |
| **Requirements** | None | KVM-enabled host (bare-metal/EC2) |

## Configuration

### Global Configuration (agent.yaml)

The Python executor is configured in `config/agent.yaml` (mounted at `/app/config/agent.yaml` in Docker):

```yaml
python_executor:
  # Default mode: "wasi" or "firecracker"
  mode: "wasi"

  # WASI-specific limits
  wasi:
    memory_limit_mb: 256
    timeout_seconds: 30

  # Firecracker-specific limits
  firecracker:
    memory_mb: 1024
    vcpu_count: 2
    timeout_seconds: 300
    pool_warm_count: 3    # VMs kept warm for fast startup
    pool_max_count: 20    # Maximum concurrent VMs

  # Workspace limits (shared by both executors)
  workspace:
    max_size_mb: 500
    max_file_count: 10000
    retention_hours: 24
```

### Environment Variables

Override configuration via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `PYTHON_EXECUTOR_MODE` | `wasi` | Executor backend: `wasi` or `firecracker` |
| `FIRECRACKER_MEMORY_MB` | `1024` | Firecracker VM memory (MB) |
| `FIRECRACKER_VCPU_COUNT` | `2` | Firecracker vCPU count |
| `FIRECRACKER_TIMEOUT_SECONDS` | `300` | Firecracker execution timeout |
| `FIRECRACKER_POOL_WARM_COUNT` | `3` | Warm VMs in pool |
| `FIRECRACKER_POOL_MAX_COUNT` | `20` | Maximum VM pool size |
| `WORKSPACE_MAX_SIZE_MB` | `500` | Max workspace size per session |
| `SHANNON_SESSION_WORKSPACES_DIR` | `/tmp/shannon-sessions` | Session workspace directory |

### Per-Request Configuration

> **Note:** Firecracker is not yet implemented. The `python_executor_mode` parameter is accepted but currently ignored; all requests use WASI.

```bash
curl -X POST http://localhost:8080/api/v1/tasks -d '{
  "query": "Analyze this CSV and compute correlations",
  "context": {
    "python_executor_mode": "firecracker"
  }
}'
```

### Role-Based Defaults

> **Note:** Firecracker routing is not yet wired. All roles currently fall back to WASI regardless of the configured mode.

| Role | Default Mode | Rationale |
|------|--------------|-----------|
| `general` | wasi | Quick responses |
| `developer` | wasi | Script execution |
| `data_analyst` | wasi (firecracker planned) | Requires pandas, numpy, scipy (not yet available) |
| `researcher` | wasi | Primarily web search |

## File Persistence with /workspace/

Both backends mount a session workspace at `/workspace/` with read-write access. Files written here persist for the session duration.

### Writing Files

```python
# Write a file to workspace
with open('/workspace/results.csv', 'w') as f:
    f.write('col1,col2\n1,2\n3,4\n')
print('File saved to /workspace/results.csv')
```

### Reading Files

```python
# Read a previously saved file
with open('/workspace/results.csv', 'r') as f:
    content = f.read()
print(content)
```

### Cross-Tool Access

Files written by `python_executor` can be read in subsequent Python executions within the same session. The `file_read` tool shares the same session workspace, so files are accessible via relative paths (e.g., write `/workspace/results.csv` in Python, then `file_read` with `path: "results.csv"`). Absolute `/workspace/...` paths are rejected by `file_read`; use relative paths instead.

### Workspace Lifecycle

- **Created**: When session starts and Python executor is first used
- **Location**: `{SHANNON_SESSION_WORKSPACES_DIR}/{session_id}/`
- **Cleanup**: Automatic after `retention_hours` (default: 24h)
- **Isolation**: Each session has its own workspace

## Resource Limits

### WASI Limits

| Resource | Default | Override |
|----------|---------|----------|
| Memory | 256MB | `WASI_MEMORY_LIMIT_MB` |
| Timeout | 30s | `WASI_TIMEOUT_SECONDS` |
| Fuel (instructions) | 1B | `wasi.max_fuel` in config |

### Firecracker Limits

| Resource | Default | Override |
|----------|---------|----------|
| Memory | 1024MB | `FIRECRACKER_MEMORY_MB` |
| vCPUs | 2 | `FIRECRACKER_VCPU_COUNT` |
| Timeout | 300s | `FIRECRACKER_TIMEOUT_SECONDS` |

### Workspace Limits

| Resource | Default | Description |
|----------|---------|-------------|
| Max Size | 500MB | Total size of all files |
| Max Files | 10000 | Maximum file count |
| Retention | 24h | Auto-cleanup after idle |

## Security Model

### WASI Sandbox

- **Memory isolation**: Linear memory with bounds checking
- **Syscall filtering**: Only whitelisted WASI syscalls
- **Filesystem**: Read-only except `/workspace/`
- **No network**: Network access disabled
- **Fuel metering**: CPU instruction limits prevent infinite loops

### Firecracker Isolation

- **Hardware virtualization**: Full VM with KVM
- **Jailer**: Chroot + seccomp + cgroups
- **virtiofs**: Workspace mounted via virtio, not direct host access
- **No network**: No external network by default
- **Resource cgroups**: Memory and CPU hard limits

### Session Isolation

- Each session gets a unique workspace directory
- Workspaces are not shared between sessions
- Canonical path resolution prevents symlink escapes
- Session IDs are server-generated (UUID) or user-scoped; the orchestrator validates ownership before passing to agent-core
- Symlinks inside workspaces are skipped during size calculations to prevent escape

## Example Usage

### Quick Calculation (WASI)

```bash
./scripts/submit_task.sh "Calculate the factorial of 20"
```

Uses WASI backend automatically (default mode).

### Data Analysis (Firecracker)

```bash
curl -X POST http://localhost:8080/api/v1/tasks -d '{
  "query": "Load the CSV from /workspace/data.csv, compute summary statistics, and save results to /workspace/stats.json",
  "context": {
    "python_executor_mode": "firecracker",
    "role": "data_analyst"
  }
}'
```

### Multi-Step with Persistence

1. First task writes data:
```bash
curl -X POST http://localhost:8080/api/v1/tasks -d '{
  "query": "Generate sample data with 1000 rows and save to /workspace/sample.csv",
  "session_id": "analysis-session-1"
}'
```

2. Second task reads and processes:
```bash
curl -X POST http://localhost:8080/api/v1/tasks -d '{
  "query": "Read /workspace/sample.csv and compute the mean of each column",
  "session_id": "analysis-session-1"
}'
```

## Firecracker Setup (Production)

Firecracker requires a KVM-enabled host (bare-metal EC2, dedicated host).

### Prerequisites

```bash
# Check KVM availability
ls -la /dev/kvm

# Download Firecracker binaries
./scripts/download_firecracker.sh

# Setup host environment
./scripts/setup_firecracker_host.sh

# Build rootfs with data science packages
./scripts/build_firecracker_rootfs.sh
```

### Required Infrastructure

| Component | Location |
|-----------|----------|
| Firecracker binary | `/usr/local/bin/firecracker` |
| Jailer binary | `/usr/local/bin/jailer` |
| Kernel | `/var/lib/firecracker/kernel/vmlinux` |
| Rootfs | `/var/lib/firecracker/images/python-datascience.ext4` |
| Jailer dir | `/srv/jailer/` |

## Troubleshooting

### WASI: "Module uses more memory than allowed"

Increase memory limit:
```bash
export WASI_MEMORY_LIMIT_MB=512
```

### WASI: "Out of fuel"

Long-running computation exceeded instruction limit. Use Firecracker for heavy workloads:
```json
{"context": {"python_executor_mode": "firecracker"}}
```

### Firecracker: "KVM not available"

Firecracker requires hardware virtualization. Check:
```bash
ls -la /dev/kvm
# Should show crw-rw---- root kvm /dev/kvm
```

If missing, you need a KVM-enabled host (EC2 bare-metal, dedicated instance).

### Workspace: "File not found"

Verify session ID is consistent across requests:
```bash
# Both requests must use same session_id
curl -X POST ... -d '{"session_id": "my-session", ...}'
```

### Workspace: "Permission denied"

Files must be written to `/workspace/`, not elsewhere:
```python
# Correct
open('/workspace/output.txt', 'w')

# Wrong - will fail
open('/tmp/output.txt', 'w')
```

## Known Limitations

1. **WASI stdlib only**: No pip packages in WASI mode. Use Firecracker for numpy/pandas.

2. **Firecracker requires KVM**: Cannot run in containers or on macOS. Use WASI for local development.

3. **No network in sandbox**: Neither backend has network access. Use web_search tool for web requests.

4. **Workspace quota enforcement**: Direct WASI writes bypass quota checks. Mitigated by time-limited sessions.

5. **Firecracker pool cold start**: First request may be slow if pool is empty. Configure `pool_warm_count` appropriately.
