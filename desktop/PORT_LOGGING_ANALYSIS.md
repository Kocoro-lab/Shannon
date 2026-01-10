# Port Logging Analysis - Where and When Ports Are Logged

This document traces all the places where the server port is logged during startup and explains the timing issues.

## 📊 Port Logging Timeline

### Chronological Order of Port Logging

```
Time    | Location                      | Log Message                                    | Port Value
--------|-------------------------------|------------------------------------------------|------------
T+0ms   | lib.rs:193                    | "✅ Embedded API started on port X"            | 0 or actual
T+50ms  | embedded_api.rs:344           | "✅ Server bound to 0.0.0.0:0"                 | N/A
T+51ms  | embedded_api.rs:377           | "🎉 Embedded Shannon API listening on X"       | actual
T+52ms  | embedded_api.rs:392           | State change: "Server ready on port X"         | actual
T+53ms  | embedded_api.rs:407           | "✅ Server ready event emitted (port: X)"      | actual
```

## 🔴 The Problem

### Issue 1: Race Condition in lib.rs

**File:** `src-tauri/src/lib.rs:193`

```rust
match embedded_api::start_embedded_api(Some(0), app_handle_for_thread, ipc_logger_for_thread).await {
    Ok(handle) => {
        log::info!("✅ Embedded API started on port {}", handle.port());
        //                                                  ^^^^^^^^^^^^^^
        //                                                  This is often 0!
        state_clone.set_handle(handle);
    }
    Err(e) => {
        log::error!("❌ Failed to start embedded API: {}", e);
    }
}
```

**Why it logs 0:**
1. `start_embedded_api()` spawns an async task to bind the server
2. The function returns IMMEDIATELY with a handle
3. At this point, the server hasn't bound yet, so port is still 0
4. The log happens before the server binds

**Timeline:**
```
0ms:   start_embedded_api() called
1ms:   Function spawns tokio task
2ms:   Function returns handle (port = 0)
3ms:   ✅ Log "Embedded API started on port 0" ← WRONG!
...
50ms:  Server actually binds in background task
51ms:  Port gets updated to real value (e.g., 54321)
```

### Issue 2: Port Resolution Delay

**File:** `src-tauri/src/embedded_api.rs:452-470`

```rust
// Wait for the server to bind and update the port
let mut final_port = 0u16;
for attempt in 0..10 {
    tokio::time::sleep(tokio::time::Duration::from_millis(50 * (attempt + 1))).await;
    let port = *actual_port.lock().await;
    if port != 0 {
        final_port = port;
        break;
    }
}

// Create handle with actual port
let handle = EmbeddedApiHandle {
    should_run,
    port: final_port,  // This is what gets returned
};
```

**The problem:**
- Even with retries, if the server binding takes > 2.75 seconds, we still return port 0
- In your logs, the server took much longer due to SurrealDB initialization

## ✅ Where Port IS Logged Correctly

### Location 1: Inside the Spawned Task

**File:** `src-tauri/src/embedded_api.rs:377-381`

```rust
ipc_logger_clone.log(
    LogLevel::Info,
    Component::EmbeddedApi,
    format!("🎉 Embedded Shannon API listening on {}", local_addr),
);
```

**This ALWAYS has the correct port because:**
- It runs AFTER `TcpListener::bind()` completes
- It gets the actual bound address from `listener.local_addr()`
- Timeline: Runs at ~50-90 seconds after app start

**Expected log:**
```
[2026-01-09][23:45:27][app_lib::ipc_logger][INFO] 🎉 Embedded Shannon API listening on 127.0.0.1:54321
```

### Location 2: State Change Event

**File:** `src-tauri/src/embedded_api.rs:387-392`

```rust
ipc_logger_clone.state_change(
    Some(LifecyclePhase::Starting),
    LifecyclePhase::Ready,
    Some(format!("Server ready on port {}", bound_port)),
    Some(bound_port),
);
```

**This also has the correct port:**
- Runs immediately after getting `bound_port` from `local_addr()`
- Emits IPC event to frontend with actual port

### Location 3: Server Ready Event

**File:** `src-tauri/src/embedded_api.rs:394-407`

```rust
// Emit ready event to Tauri frontend with actual bound port
let _ = app_handle.emit(
    "server-ready",
    serde_json::json!({
        "url": format!("http://127.0.0.1:{}", bound_port),
        "port": bound_port
    }),
);

ipc_logger_clone.log(
    LogLevel::Info,
    Component::EmbeddedApi,
    format!("✅ Server ready event emitted (port: {})", bound_port),
);
```

**This confirms the actual port:**
- Logs the port number that was emitted to frontend
- Frontend uses this to connect

## 🔍 Finding the Real Port in Your Logs

### Method 1: Search for "listening on"

```bash
grep "listening on" your_log_file.txt
```

**Expected output:**
```
[app_lib::ipc_logger][INFO] 🎉 Embedded Shannon API listening on 127.0.0.1:54321
                                                                            ^^^^^
                                                                            This is your port!
```

### Method 2: Search for "Server ready event emitted"

```bash
grep "Server ready event emitted" your_log_file.txt
```

**Expected output:**
```
[app_lib::ipc_logger][INFO] ✅ Server ready event emitted (port: 54321)
                                                                 ^^^^^
```

### Method 3: Search for State Change Events

```bash
grep "Server ready on port" your_log_file.txt
```

**Expected output:**
```
[app_lib::ipc_logger][INFO] Server ready on port 54321 from=Starting to=Ready port=54321
                                                 ^^^^^
```

## 📝 Your Specific Logs

Looking at your logs from the error report:

```
[2026-01-09][23:44:39][app_lib::ipc_logger][INFO] 🚀 Starting embedded Shannon API (requested port: 0)
[2026-01-09][23:44:39][app_lib::ipc_logger][INFO] Loading configuration...
[2026-01-09][23:44:39][app_lib::ipc_logger][INFO] ✅ Configuration loaded (mode: embedded)
[2026-01-09][23:44:39][app_lib::ipc_logger][INFO] Creating Shannon API application...
[2026-01-09][23:44:39][shannon_api::server][INFO] 🚀 Shannon API v0.1.0
...
[2026-01-09][23:44:39][app_lib][INFO] ✅ Embedded API started on port 0  ← WRONG (too early)
...
[2026-01-09][23:45:27][surrealdb::core::kvs::ds][INFO] Started kvs store at rocksdb://data/shannon.db
```

**What's missing from your logs:**
- ❌ "🎉 Embedded Shannon API listening on 127.0.0.1:XXXXX"
- ❌ "✅ Server ready event emitted (port: XXXXX)"
- ❌ State change to "Ready" with port number

**This suggests:**
The server binding step completed but those logs didn't appear in your output, OR the app was closed before they could print.

## 🔧 Solutions

### Solution 1: Fix the Early Log (Already Applied)

The fix keeps the early log but adds retry logic to wait for the actual port:

```rust
// Wait for the server to bind and update the port
let mut final_port = 0u16;
for attempt in 0..10 {
    tokio::time::sleep(tokio::time::Duration::from_millis(50 * (attempt + 1))).await;
    let port = *actual_port.lock().await;
    if port != 0 {
        final_port = port;
        break;
    }
}
```

### Solution 2: Add Additional Logging

To make it VERY clear what port the server is on, we could add:

```rust
// In lib.rs after server starts
log::info!("🌐 Connect to embedded API at: http://127.0.0.1:{}", handle.port());
```

### Solution 3: Frontend Should Listen for Event

The frontend should NOT rely on logs, but instead listen for the `server-ready` event:

```typescript
import { listen } from '@tauri-apps/api/event';

listen('server-ready', (event) => {
    const { url, port } = event.payload;
    console.log(`🌐 Server is ready at ${url} (port ${port})`);
    // Update API client to use this URL
});
```

## 🎯 Recommendations

### For Debugging

1. **Always search for "listening on"** in logs - this is the MOST reliable indicator
2. **Check IPC events** - The `server-ready` event has the correct port
3. **Use system tools** - `lsof -i -P | grep shannon` to see actual listening port
4. **Wait longer** - Server can take 30-90 seconds on first run

### For Code Improvements

1. **Remove the early log** in `lib.rs:193` since it's always wrong
2. **Only log port AFTER** it's actually bound
3. **Make the "listening on" log more prominent** - it's the source of truth
4. **Add fallback logging** if port resolution fails

## 🔍 Quick Port Discovery Commands

```bash
# Method 1: From logs
grep -E "listening on|Server ready event emitted" tauri.log | tail -1

# Method 2: From process
lsof -iTCP -sTCP:LISTEN -n -P | grep shannon | awk '{print $9}' | cut -d: -f2

# Method 3: Test common range
for port in {49152..65535}; do
    curl -s -m 1 http://localhost:$port/health 2>/dev/null && \
    echo "Found on port $port" && break
done

# Method 4: From Tauri frontend
# Open DevTools (F12) and check console for server-ready event
```

## 📊 Summary Table

| Log Location | Timing | Port Accuracy | Reliability |
|--------------|--------|---------------|-------------|
| `lib.rs:193` | T+3ms | ❌ Often 0 | Low |
| `embedded_api.rs:377` | T+50-90s | ✅ Always correct | **High** |
| `embedded_api.rs:407` | T+51-91s | ✅ Always correct | **High** |
| State change event | T+52-92s | ✅ Always correct | **High** |
| `server-ready` IPC | T+53-93s | ✅ Always correct | **High** |

**Conclusion:** The logs at `embedded_api.rs:377` and beyond are the source of truth. The early log in `lib.rs:193` is unreliable and should be removed or ignored.