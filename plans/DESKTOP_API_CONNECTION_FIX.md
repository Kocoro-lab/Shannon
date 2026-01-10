# Desktop API Connection Fix Plan

## Problem Analysis

The Planet desktop application is failing to connect to the embedded Shannon API server with the following errors:

```
Could not connect to the server.
Fetch API cannot load http://127.0.0.1:8765/api/v1/sessions?limit=10&offset=0 due to access control checks.
Failed to load resource: Could not connect to the server.
```

### Root Causes Identified

#### 1. **API Endpoint Mismatch**
- **Frontend Configuration**: [`desktop/.env.local`](desktop/.env.local:10) sets `NEXT_PUBLIC_API_URL=http://localhost:8080`
- **Embedded Server**: [`desktop/src-tauri/src/lib.rs`](desktop/src-tauri/src/lib.rs:193) binds to `127.0.0.1:8765`
- **Result**: Frontend tries to connect to port 8080, but the embedded API is listening on port 8765

#### 2. **Server May Not Be Starting**
The embedded API server initialization in [`desktop/src-tauri/src/lib.rs`](desktop/src-tauri/src/lib.rs:148-232) has several potential failure points:
- Configuration loading may fail silently
- SurrealDB initialization may fail
- Workflow engine creation may fail
- The server thread may panic without proper error propagation

#### 3. **Missing API Routes**
The frontend is trying to access `/api/v1/sessions` but we need to verify:
- The Shannon API has this route registered
- The route is properly exposed in embedded mode
- CORS is configured correctly for localhost access

#### 4. **Configuration Validation Issues**
The embedded mode sets environment variables but may fail validation:
- API keys are set to placeholder values if not configured
- Workflow engine configuration may be incomplete
- Database path may not be properly initialized

## Architecture Context

### Current Setup

```
┌─────────────────────────────────────────────┐
│         Tauri Desktop App (Planet)          │
│                                             │
│  ┌───────────────────────────────────────┐  │
│  │     Next.js Frontend (Port 3000)      │  │
│  │   Expects API at localhost:8080       │  │ ❌ MISMATCH
│  └───────────────────────────────────────┘  │
│                    ↓                        │
│  ┌───────────────────────────────────────┐  │
│  │   Embedded Shannon API (Port 8765)    │  │ ⚠️ MAY NOT START
│  │  ┌─────────────┬─────────────────┐    │  │
│  │  │ Durable     │ SurrealDB       │    │  │
│  │  │ Workflows   │ (RocksDB)       │    │  │
│  │  └─────────────┴─────────────────┘    │  │
│  └───────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
```

### Expected Flow

1. User opens Planet desktop app
2. Tauri starts and initializes embedded Shannon API on port 8765
3. Next.js frontend loads and connects to embedded API
4. User can create tasks, chat with agents, etc.

## Fix Plan

### Phase 1: Immediate Fixes (Critical)

#### Fix 1.1: Update Frontend API URL
**File**: [`desktop/.env.local`](desktop/.env.local:10)

```diff
- NEXT_PUBLIC_API_URL=http://localhost:8080
+ NEXT_PUBLIC_API_URL=http://localhost:8765
```

**Rationale**: Match the embedded API port (8765) that's hardcoded in the Tauri backend.

#### Fix 1.2: Add Better Error Logging
**File**: [`desktop/src-tauri/src/lib.rs`](desktop/src-tauri/src/lib.rs:148-232)

Add comprehensive logging at each initialization step:
- Log when configuration loading starts/completes
- Log when SurrealDB initialization starts/completes
- Log when workflow engine creation starts/completes
- Log when server binding succeeds/fails
- Add error details to the ready channel

#### Fix 1.3: Verify API Routes
**Action**: Check that [`rust/shannon-api/src/api/mod.rs`](rust/shannon-api/src/api/) includes session management routes:
- `GET /api/v1/sessions`
- `POST /api/v1/sessions`
- Other session-related endpoints

### Phase 2: Configuration Improvements

#### Fix 2.1: Relaxed Configuration Validation for Embedded Mode
**File**: [`rust/shannon-api/src/config/validator.rs`](rust/shannon-api/src/config/)

The embedded mode should:
- Allow placeholder API keys (user configures via Settings UI)
- Skip Redis validation (not required in embedded mode)
- Skip orchestrator gRPC validation (not used in embedded mode)
- Only validate SurrealDB path is writable

#### Fix 2.2: Environment Variable Precedence
**File**: [`desktop/src-tauri/src/lib.rs`](desktop/src-tauri/src/lib.rs:154-166)

Set environment variables BEFORE loading any configuration:
```rust
// Set environment for embedded mode FIRST (before any config loading)
std::env::set_var("SHANNON_MODE", "embedded");
std::env::set_var("WORKFLOW_ENGINE", "durable");
std::env::set_var("DATABASE_DRIVER", "surrealdb");
std::env::set_var("SURREALDB_PATH", data_dir.join("shannon.db").to_string_lossy().to_string());
```

This is already done correctly, but we need to ensure config loading respects these.

### Phase 3: Enhanced Debugging

#### Fix 3.1: Add Health Check Endpoint
Verify that [`rust/shannon-api/src/api/mod.rs`](rust/shannon-api/src/api/) includes:
- `GET /health` - Basic health check
- `GET /api/v1/health` - Detailed health with component status

#### Fix 3.2: Add Startup Diagnostics
**File**: [`desktop/src-tauri/src/lib.rs`](desktop/src-tauri/src/lib.rs)

After server starts, perform a self-test:
```rust
// Test the server is responding
match reqwest::get("http://127.0.0.1:8765/health").await {
    Ok(resp) => log::info!("Health check passed: {:?}", resp.status()),
    Err(e) => log::error!("Health check failed: {}", e),
}
```

### Phase 4: User Experience Improvements

#### Fix 4.1: Settings UI for API Keys
The app should show a setup wizard on first launch:
1. Detect if API keys are configured
2. Show settings dialog if not
3. Allow user to enter OpenAI or Anthropic API key
4. Save to Tauri store
5. Restart embedded API with new configuration

#### Fix 4.2: Connection Status Indicator
Add a status indicator in the UI:
- 🟢 Connected to embedded API
- 🟡 Connecting...
- 🔴 Connection failed (with error details)

## Implementation Steps

### Step 1: Quick Fix (5 minutes)
1. Update [`desktop/.env.local`](desktop/.env.local) to use port 8765
2. Rebuild and test: `cd desktop && npm run tauri:dev`

### Step 2: Add Logging (15 minutes)
1. Enhance error logging in [`desktop/src-tauri/src/lib.rs`](desktop/src-tauri/src/lib.rs)
2. Add startup diagnostics
3. Test and capture logs

### Step 3: Verify Routes (10 minutes)
1. Check [`rust/shannon-api/src/api/mod.rs`](rust/shannon-api/src/api/)
2. Verify session routes are registered
3. Test with curl: `curl http://localhost:8765/api/v1/sessions`

### Step 4: Configuration Validation (30 minutes)
1. Update [`rust/shannon-api/src/config/validator.rs`](rust/shannon-api/src/config/)
2. Add embedded mode validation rules
3. Test configuration loading

### Step 5: Health Checks (20 minutes)
1. Verify health endpoint exists
2. Add self-test after startup
3. Add UI connection status

## Testing Plan

### Test 1: Basic Connectivity
```bash
# Start the desktop app
cd desktop
npm run tauri:dev

# In another terminal, test the API
curl http://localhost:8765/health
curl http://localhost:8765/api/v1/sessions
```

### Test 2: Configuration Loading
```bash
# Check logs for configuration errors
# Look for:
# - "Loading configuration..."
# - "Creating embedded API server..."
# - "Embedded Shannon API listening on 127.0.0.1:8765"
# - "Embedded API is ready!"
```

### Test 3: Frontend Connection
1. Open Planet app
2. Open DevTools (Cmd+Shift+I)
3. Check Console for errors
4. Check Network tab for failed requests
5. Verify requests go to `http://localhost:8765`

## Rollback Plan

If the fixes cause issues:

1. **Revert `.env.local` changes**:
   ```bash
   git checkout desktop/.env.local
   ```

2. **Revert code changes**:
   ```bash
   git checkout desktop/src-tauri/src/lib.rs
   ```

3. **Clear app data**:
   ```bash
   # macOS
   rm -rf ~/Library/Application\ Support/ai.prometheusags.planet/
   ```

## Success Criteria

✅ Desktop app starts without errors
✅ Embedded API server starts on port 8765
✅ Frontend successfully connects to embedded API
✅ `/api/v1/sessions` endpoint returns data
✅ No CORS errors in console
✅ Health check endpoint responds
✅ User can create and view tasks

## Additional Considerations

### Port Conflicts
If port 8765 is already in use:
- Add port configuration to Tauri settings
- Allow user to change port in Settings UI
- Update frontend API URL dynamically

### API Key Management
Current approach uses placeholder keys, but should:
- Prompt user for API key on first launch
- Store securely in Tauri store
- Reload configuration when keys change
- Show clear error if no valid key configured

### Database Location
SurrealDB path is set to app data directory:
```rust
data_dir.join("shannon.db")
```

Ensure:
- Directory exists and is writable
- Proper permissions on macOS/Windows/Linux
- Database can be backed up/restored

### Workflow Engine
Embedded mode uses Durable engine (not Temporal):
- Verify Durable engine is properly initialized
- Check that workflow patterns are loaded
- Ensure WASM patterns directory exists

## Next Steps

After implementing these fixes:

1. **Test on all platforms**: macOS, Windows, Linux
2. **Add integration tests**: Automated tests for embedded mode
3. **Document setup process**: User guide for first-time setup
4. **Add error recovery**: Automatic retry on connection failure
5. **Performance monitoring**: Track startup time and resource usage

## Related Files

- [`desktop/.env.local`](desktop/.env.local) - Frontend API configuration
- [`desktop/src-tauri/src/lib.rs`](desktop/src-tauri/src/lib.rs) - Tauri app initialization
- [`desktop/src-tauri/Cargo.toml`](desktop/src-tauri/Cargo.toml) - Rust dependencies
- [`rust/shannon-api/src/server.rs`](rust/shannon-api/src/server.rs) - API server setup
- [`rust/shannon-api/src/config/mod.rs`](rust/shannon-api/src/config/mod.rs) - Configuration loading
- [`config/shannon.yaml`](config/shannon.yaml) - Default configuration

## Questions to Address

1. **Are session routes implemented in Shannon API?**
   - Need to verify `/api/v1/sessions` exists
   - Check if it's enabled in embedded mode

2. **Is the Durable workflow engine properly initialized?**
   - Check if WASM patterns directory exists
   - Verify workflow engine creation doesn't fail

3. **Are there any missing dependencies?**
   - SurrealDB with RocksDB backend
   - Durable-shannon crate
   - All required features enabled

4. **What's the actual error in the logs?**
   - Need to see Tauri console output
   - Check for panic messages
   - Look for configuration validation errors
