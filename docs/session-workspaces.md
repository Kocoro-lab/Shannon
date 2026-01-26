# Shannon Session Workspaces

## Overview

Session workspaces provide isolated filesystem environments for each user session. All file operations are scoped to the session's workspace directory, ensuring that different sessions cannot access each other's files.

This isolation is critical for:
- **Multi-tenant security**: Preventing data leakage between users
- **Reproducibility**: Each session starts with a clean workspace
- **Resource management**: Per-session disk quotas prevent runaway usage

## Role Requirements for File Tools

**Important**: File tools are gated by role. You must specify the appropriate role in the request context to access file operations.

| Role | File Tools Available |
|------|---------------------|
| `developer` | `file_read`, `file_write`, `file_list`, `bash`, `python_executor` |
| `generalist` (default) | `file_read`, `file_list`, `bash` (no `file_write`) |
| `critic` | `file_read`, `file_list`, `bash` |

**To write files, you must use `role: "developer"`:**

```json
{
  "query": "Create a file called test.txt with content 'Hello'",
  "session_id": "my-session-123",
  "context": {
    "role": "developer"
  }
}
```

Without `role: "developer"`, the agent cannot use `file_write` and the request will either fail or the LLM won't have the tool available.

### Example: File Persistence Across Requests

```bash
# First request - create file (requires developer role)
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Write '\''test data'\'' to data.txt",
    "session_id": "persist-test",
    "context": {"role": "developer"}
  }'

# Second request - read same file (same session_id)
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Read data.txt and show contents",
    "session_id": "persist-test",
    "context": {"role": "developer"}
  }'
```

Files persist within a session - subsequent requests with the same `session_id` can access previously created files.

## Architecture

### Directory Structure

```
/tmp/shannon-sessions/              # SHANNON_SESSION_WORKSPACES_DIR (base)
├── session-abc123/                 # Session A's workspace
│   ├── code/
│   │   └── main.py
│   ├── data/
│   │   └── input.csv
│   └── output.txt
├── session-def456/                 # Session B's workspace
│   └── results.json
└── session-xyz789/                 # Session C's workspace
    └── notes.md
```

Each session gets a dedicated subdirectory named after its `session_id`. The workspace is created automatically on first file operation.

### Component Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Tool Request                                       │
│  {"tool": "file_write", "path": "data.txt", "content": "Hello"}             │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                    Python llm-service (file_ops.py)                          │
│                                                                              │
│  Check: is_sandbox_enabled()?                                                │
│    ├── Yes → Proxy to Rust agent-core via gRPC                              │
│    └── No  → Execute locally with Python path validation                     │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
            ┌───────────────────────┴───────────────────────┐
            │                                               │
            ▼ (WASI mode)                                   ▼ (Python mode)
┌───────────────────────────────┐           ┌───────────────────────────────┐
│   Rust agent-core             │           │   Python Path Validation       │
│   (sandbox_service.rs)        │           │                                │
│                               │           │   - Resolve canonical path     │
│   - WorkspaceManager          │           │   - Check against allowlist    │
│   - SafeCommand execution     │           │   - Execute if permitted       │
│   - Capability-based security │           │                                │
└───────────────────────────────┘           └───────────────────────────────┘
```

## Security Model

Shannon implements two security layers for file operations. One or both may be active depending on configuration.

### Phase 2: Python Path Validation (Fallback)

When `SHANNON_USE_WASI_SANDBOX=0`, file operations are handled directly in Python with path validation:

1. **Path canonicalization**: All paths are resolved using `Path.resolve()` to eliminate symlinks and `..` components
2. **Allowlist enforcement**: Resolved paths must fall within allowed directories:
   - Session workspace: `{SHANNON_SESSION_WORKSPACES_DIR}/{session_id}/`
   - Optional shared workspace: `SHANNON_WORKSPACE`
   - Dev mode only: Current working directory (if `SHANNON_DEV_ALLOW_CWD=1`)
3. **Symlink protection**: Symlinks that point outside allowed directories are rejected

**Example of blocked path traversal:**
```python
# Request: {"path": "../../../etc/passwd"}
# Resolved: /etc/passwd
# Result: DENIED (not within session workspace)
```

### Phase 3: WASI Sandbox (Default, Enhanced Security)

When `SHANNON_USE_WASI_SANDBOX=1` (default in docker-compose), file operations are proxied to the Rust agent-core service which provides additional security:

1. **Capability-based security**: The WASI runtime only grants access to the session workspace directory
2. **Memory limits**: Sandbox execution is constrained by memory limits
3. **Native command implementation**: Shell commands (`ls`, `cat`, etc.) are implemented in pure Rust, not by spawning shell processes
4. **Metacharacter blocking**: Shell metacharacters are rejected at parse time

**Security properties:**
- No shell injection possible (commands are parsed, not executed via shell)
- No network access from sandbox
- Memory-limited execution
- Read-only access to system files (none by default)

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SHANNON_USE_WASI_SANDBOX` | `0` | Enable WASI sandbox mode. Set to `1`, `true`, or `yes` to enable. |
| `SHANNON_SESSION_WORKSPACES_DIR` | `/tmp/shannon-sessions` | Base directory for all session workspaces |
| `SHANNON_MAX_WORKSPACE_SIZE_MB` | `100` | Per-session disk quota in megabytes |
| `SHANNON_WORKSPACE` | (unset) | Optional shared workspace accessible to all sessions |
| `SHANNON_DEV_ALLOW_CWD` | `0` | Dev only: allow access to current working directory |
| `SHANNON_ALLOW_GLOBAL_TMP` | `0` | Allow access to global `/tmp` (disabled by default for isolation) |

### Docker Compose Configuration

Both `agent-core` and `llm-service` must share the same session workspaces volume:

```yaml
volumes:
  shannon-sessions:

services:
  agent-core:
    volumes:
      - shannon-sessions:/tmp/shannon-sessions
    environment:
      - SHANNON_SESSION_WORKSPACES_DIR=/tmp/shannon-sessions

  llm-service:
    volumes:
      - shannon-sessions:/tmp/shannon-sessions
    environment:
      - SHANNON_USE_WASI_SANDBOX=${SHANNON_USE_WASI_SANDBOX:-0}
      - SHANNON_SESSION_WORKSPACES_DIR=/tmp/shannon-sessions
```

## Using File Tools

### file_read

Read contents of a file from the session workspace.

**Parameters:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `path` | string | yes | - | Path to the file (relative to workspace) |
| `encoding` | string | no | `utf-8` | File encoding (`utf-8`, `ascii`, `latin-1`) |
| `max_size_mb` | integer | no | `10` | Maximum file size to read (1-100 MB) |

**Example:**
```json
{
  "tool": "file_read",
  "path": "data/config.json"
}
```

**Response:**
```json
{
  "success": true,
  "output": {"key": "value"},
  "metadata": {
    "path": "/tmp/shannon-sessions/my-session/data/config.json",
    "size_bytes": 42,
    "encoding": "utf-8",
    "file_type": ".json"
  }
}
```

JSON and YAML files are automatically parsed into structured data.

### file_write

Write content to a file in the session workspace.

**Parameters:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `path` | string | yes | - | Path where to write the file |
| `content` | string | yes | - | Content to write |
| `mode` | string | no | `overwrite` | `overwrite` or `append` |
| `encoding` | string | no | `utf-8` | File encoding |
| `create_dirs` | boolean | no | `false` | Create parent directories if needed |

**Example:**
```json
{
  "tool": "file_write",
  "path": "output/results.txt",
  "content": "Analysis complete.\nTotal items: 42",
  "create_dirs": true
}
```

**Response:**
```json
{
  "success": true,
  "output": "/tmp/shannon-sessions/my-session/output/results.txt",
  "metadata": {
    "path": "/tmp/shannon-sessions/my-session/output/results.txt",
    "size_bytes": 35,
    "mode": "overwrite",
    "encoding": "utf-8",
    "created_dirs": true
  }
}
```

### file_list

List files in a directory within the session workspace.

**Parameters:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `path` | string | yes | - | Directory path to list |
| `pattern` | string | no | `*` | Glob pattern (e.g., `*.txt`, `*.py`) |
| `recursive` | boolean | no | `false` | Include subdirectories |
| `include_hidden` | boolean | no | `false` | Include hidden files (`.xxx`) |

**Example:**
```json
{
  "tool": "file_list",
  "path": ".",
  "pattern": "*.py",
  "recursive": true
}
```

**Response:**
```json
{
  "success": true,
  "output": [
    {"name": "main.py", "path": "main.py", "is_file": true, "size_bytes": 1024},
    {"name": "utils.py", "path": "lib/utils.py", "is_file": true, "size_bytes": 512}
  ],
  "metadata": {
    "directory": "/tmp/shannon-sessions/my-session",
    "pattern": "*.py",
    "recursive": true,
    "file_count": 2,
    "dir_count": 0
  }
}
```

## Sandbox Commands (WASI Mode)

When `SHANNON_USE_WASI_SANDBOX=1`, these safe commands are available for direct execution. Commands are implemented natively in Rust (not via shell), eliminating shell injection risks.

### Read Operations

| Command | Description | Example |
|---------|-------------|---------|
| `ls` | List directory contents | `ls -la data/` |
| `cat` | Print file contents | `cat config.json` |
| `head` | Print first N lines | `head -n 10 log.txt` |
| `tail` | Print last N lines | `tail -n 20 log.txt` |
| `wc` | Count lines/words/bytes | `wc data.csv` |
| `pwd` | Print working directory | `pwd` |

### Write Operations

| Command | Description | Example |
|---------|-------------|---------|
| `mkdir` | Create directory | `mkdir -p output/reports` |
| `rm` | Remove file/directory | `rm -r temp/` |
| `cp` | Copy file | `cp source.txt backup.txt` |
| `mv` | Move/rename file | `mv old.txt new.txt` |
| `touch` | Create empty file | `touch marker.txt` |

### Search Operations

| Command | Description | Example |
|---------|-------------|---------|
| `find` | Find files by name | `find . -name "*.py"` |
| `grep` | Search for pattern | `grep -i error log.txt` |

### Utilities

| Command | Description | Example |
|---------|-------------|---------|
| `echo` | Print text | `echo "Hello World"` |

### Command Flags

| Command | Supported Flags |
|---------|-----------------|
| `ls` | `-a` (all), `-l` (long format) |
| `head` | `-n NUM` (line count) |
| `tail` | `-n NUM` (line count) |
| `mkdir` | `-p` (create parents) |
| `rm` | `-r` (recursive) |
| `grep` | `-i` (ignore case) |
| `find` | `-name PATTERN` |

## Blocked Shell Metacharacters

The following shell metacharacters are **blocked** to prevent command injection:

| Character | Meaning | Why Blocked |
|-----------|---------|-------------|
| `\|` | Pipe | Prevents chaining to other commands |
| `;` | Command separator | Prevents running additional commands |
| `&&` | AND operator | Prevents conditional execution |
| `\|\|` | OR operator | Prevents conditional execution |
| `>` | Output redirect | Prevents writing to arbitrary files |
| `<` | Input redirect | Prevents reading from arbitrary files |
| `>>` | Append redirect | Prevents appending to arbitrary files |
| `$(` | Command substitution | Prevents embedded command execution |
| `` ` `` | Backtick substitution | Prevents embedded command execution |
| `\n` | Newline | Prevents multi-line injection |
| `\r` | Carriage return | Prevents injection via CR |

**Example of blocked command:**
```
cat file.txt | grep secret     # REJECTED: pipe character
ls; rm -rf /                   # REJECTED: semicolon
echo $(whoami)                 # REJECTED: command substitution
```

## Quota Enforcement

Each session workspace has a configurable disk quota (default: 100MB).

### How Quota Works

1. Before each write operation, the current workspace size is calculated
2. If adding the new content would exceed the quota, the operation fails
3. No partial writes occur - either the entire write succeeds or nothing is written

### Checking Workspace Size

The `WorkspaceManager` provides a `get_workspace_size()` method that recursively calculates the total size of all files in a session workspace.

### Error Response

When quota is exceeded:
```json
{
  "success": false,
  "error": "Workspace quota exceeded: 102.5MB > 100MB limit"
}
```

### Recommendations

- Use `file_list` to audit workspace contents before large writes
- Clean up temporary files when no longer needed
- Request quota increase via configuration if legitimate needs exceed default

## Session ID Validation

Session IDs are validated to prevent path traversal attacks:

| Rule | Example Valid | Example Invalid |
|------|---------------|-----------------|
| Alphanumeric, hyphen, underscore only | `session-123`, `user_abc` | `session/../etc` |
| Max 128 characters | (any <=128 char string) | (>128 char string) |
| Cannot start with `.` | `session.1` | `.hidden-session` |
| Cannot contain `..` | `my-session` | `session..test` |

## Cleanup

### Automatic Cleanup

Session workspaces are automatically cleaned up based on session TTL (Time-To-Live). When a session expires:

1. The session record is marked for deletion
2. The workspace directory and all contents are removed
3. Cleanup runs periodically as a background process

### Manual Cleanup

Administrators can manually delete a session workspace:

```rust
let mgr = WorkspaceManager::from_env();
mgr.delete_workspace("session-abc123")?;
```

### Listing All Workspaces

To see all active session workspaces:

```rust
let mgr = WorkspaceManager::from_env();
let sessions = mgr.list_workspaces()?;
// Returns: ["session-abc123", "session-def456", ...]
```

## Troubleshooting

### File Not Found

```
Error: File not found: data.txt
```
- Verify the file exists in the session workspace
- Use `file_list` to see available files
- Check that the path is relative to workspace root

### Access Denied

```
Error: Reading /etc/passwd is not allowed. Use session workspace.
```
- Absolute paths outside the workspace are rejected
- Path traversal attempts (`../`) are blocked
- Use paths relative to the workspace root

### Sandbox Proto Not Available

```
Error: Sandbox proto not available
```
- WASI sandbox is enabled but agent-core is not running
- Check that agent-core container is healthy
- Verify gRPC connectivity between services

### Quota Exceeded

```
Error: Workspace quota exceeded
```
- Clean up unused files with `rm`
- Increase `SHANNON_MAX_WORKSPACE_SIZE_MB` if needed
- Check for unexpectedly large files

## Known Limitations

### Session ID Requires `role: developer`

File tools (`file_read`, `file_write`, `file_list`) are gated by role. Without `role: "developer"` in the request context, the LLM may prefer `python_executor` which cannot write files.

**Solution**: Always include `"context": {"role": "developer"}` when submitting tasks that need file operations.

### WASI Python Sandbox vs Session Workspace

There are two separate sandboxes:
1. **Python WASI Sandbox** (`python_executor` tool) - For code execution, read-only, no file I/O
2. **Session Workspace** (`file_*` tools) - For file persistence, isolated per session

The `python_executor` tool cannot write files. Use `file_write` for file operations.

## Key Source Files

| Component | File | Purpose |
|-----------|------|---------|
| Workspace Manager | `rust/agent-core/src/workspace.rs` | Session directory management |
| Safe Commands | `rust/agent-core/src/safe_commands.rs` | Native command implementations |
| Sandbox Service | `rust/agent-core/src/sandbox_service.rs` | gRPC service for sandbox operations |
| Python File Tools | `python/llm-service/llm_service/tools/builtin/file_ops.py` | File read/write/list tools |
| Sandbox Client | `python/llm-service/llm_service/tools/builtin/sandbox_client.py` | gRPC client for sandbox proxy |
