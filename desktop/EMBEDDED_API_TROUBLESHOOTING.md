# Embedded API Troubleshooting Guide

This guide helps diagnose and fix issues with the embedded Shannon API in the Tauri desktop application.

## 🔴 Common Issues

### Issue 1: Server Startup Timeout

**Symptoms:**
```
[ERROR] Embedded API startup failed to initiate
```

**Cause:**
- Server takes longer than 5 seconds to initialize
- SurrealDB initialization is slow (especially first time)
- The old code waited for server ready before continuing

**Solution Applied:**
The code has been updated to:
1. **Remove the 5-second timeout** - Server starts in background without blocking app initialization
2. **Async initialization** - App starts immediately, server initializes in parallel
3. **Event-based ready signal** - Frontend waits for `server-ready` event instead

**What changed:**
```rust
// OLD: Blocked app startup waiting for server
match ready_rx.recv_timeout(Duration::from_secs(5)) {
    Ok(true) => log::info!("Ready"),
    _ => log::error!("Failed"),  // ❌ Timeout after 5 seconds
}

// NEW: Non-blocking startup
std::thread::spawn(move || {
    rt.block_on(async move {
        match start_embedded_api(...).await {
            Ok(handle) => state.set_handle(handle),  // ✅ No timeout
            Err(e) => log::error!("Failed: {}", e),
        }
    });
});
log::info!("Startup initiated in background");
```

### Issue 2: Port Resolution (Port 0 Logged)

**Symptoms:**
```
[INFO] ✅ Embedded API started on port 0
```

**Cause:**
- Code returned from `start_embedded_api()` too quickly
- Port resolution happened asynchronously in spawned task
- Handle was created before actual port was assigned

**Solution Applied:**
```rust
// OLD: Waited only 100ms
tokio::time::sleep(Duration::from_millis(100)).await;
let final_port = *actual_port.lock().await;  // Might still be 0

// NEW: Retry with exponential backoff
let mut final_port = 0u16;
for attempt in 0..10 {
    tokio::time::sleep(Duration::from_millis(50 * (attempt + 1))).await;
    let port = *actual_port.lock().await;
    if port != 0 {
        final_port = port;
        break;  // Port resolved!
    }
}
```

**Behavior:**
- Attempt 1: Wait 50ms, check port
- Attempt 2: Wait 100ms, check port
- Attempt 3: Wait 150ms, check port
- ...continues up to 10 attempts (2.75 seconds total)

### Issue 3: Slow Process Startup (macOS)

**Symptoms:**
```
GPU process took 83.048494 seconds to launch
Networking process took 82.948561 seconds to launch
WebContent process took 82.890676 seconds to launch
```

**Cause:**
- macOS sandbox restrictions
- First launch on a system
- System resource contention
- Multiple processes starting simultaneously

**Solutions:**

1. **Ensure proper entitlements** (handled by Tauri):
   - Network access
   - File system access
   - GPU access

2. **System-level fixes**:
   ```bash
   # Clear Tauri cache
   rm -rf ~/Library/Application\ Support/ai.prometheusags.planet
   
   # Reset macOS permissions
   tccutil reset All ai.prometheusags.planet
   
   # Rebuild app
   cd desktop
   rm -rf src-tauri/target
   npm run tauri:build
   ```

3. **Development mode optimization**:
   ```bash
   # Use release mode for better performance
   cargo build --release --manifest-path=src-tauri/Cargo.toml
   ```

### Issue 4: SurrealDB Transaction Warning

**Symptoms:**
```
[ERROR] A transaction was dropped without being committed or cancelled
```

**Cause:**
- SurrealDB transaction not properly finalized
- Typically harmless but should be fixed in Shannon API

**Impact:**
- Warning only, doesn't affect functionality
- Will be fixed in future Shannon API update

**Workaround:**
- Safe to ignore
- Does not prevent server from working

## 🔍 Diagnostic Steps

### Step 1: Check Logs

**Run with full logging:**
```bash
RUST_LOG=debug npm run tauri:dev
```

**Look for these key messages:**

✅ **Successful startup:**
```
[INFO] Desktop mode enabled, starting embedded Shannon API...
[INFO] Data directory: /Users/.../Library/Application Support/...
[INFO] 🚀 Starting embedded Shannon API with dynamic port...
[INFO] Embedded API startup initiated in background thread
[INFO] ✅ Configuration loaded (mode: embedded)
[INFO] Creating Shannon API application...
[INFO] ✅ Application created successfully (XXXms)
[INFO] Binding server to 0.0.0.0:0...
[INFO] ✅ Server bound to 0.0.0.0:0
[INFO] 🎉 Embedded Shannon API listening on 127.0.0.1:XXXXX
[INFO] ✅ Server ready event emitted (port: XXXXX)
```

❌ **Failed startup:**
```
[ERROR] ❌ Failed to load config: ...
[ERROR] ❌ Failed to create app: ...
[ERROR] ❌ Failed to bind to 0.0.0.0:0: ...
```

### Step 2: Verify Port Assignment

**Check that port is not 0:**
```bash
# Look for this in logs:
[INFO] ✅ Embedded API started on port 54321  # Should be > 1024
```

**NOT this:**
```bash
[INFO] ✅ Embedded API started on port 0  # ❌ Wrong!
```

### Step 3: Test Frontend Connection

**Open browser DevTools (F12) and check console:**
```javascript
// Should see:
Server ready at http://127.0.0.1:54321

// Test connection:
fetch('http://127.0.0.1:54321/health')
  .then(r => r.json())
  .then(console.log);
// Should return: { status: "healthy" }
```

### Step 4: Verify Process Status

**Check running processes:**
```bash
# Find the shannon-desktop process
ps aux | grep shannon-desktop

# Check if ports are open
lsof -i -P | grep shannon
```

**Should see:**
```
shannon-desktop ... *:54321 (LISTEN)
```

## 🛠️ Manual Testing

### Test 1: Quick Start Test

```bash
# 1. Clean build
cd desktop
rm -rf .next out src-tauri/target

# 2. Build in debug mode
cargo build --manifest-path=src-tauri/Cargo.toml --features=desktop

# 3. Run with logging
RUST_LOG=debug npm run tauri:dev

# 4. Wait for "Server ready" message (may take 30-90 seconds on first run)

# 5. Check frontend can connect
# Open DevTools (F12) and verify no connection errors
```

### Test 2: Port Resolution Test

```bash
# Run app and capture logs
RUST_LOG=debug npm run tauri:dev 2>&1 | tee tauri.log

# Wait for startup (30-90 seconds)

# Check port in logs
grep "Embedded API started on port" tauri.log
# Should show port > 1024, NOT 0

# Verify server-ready event
grep "Server ready event emitted" tauri.log
# Should show same port number
```

### Test 3: Multiple Instances

```bash
# Terminal 1
npm run tauri:dev

# Wait for first instance to start (check port, e.g., 54321)

# Terminal 2
npm run tauri:dev

# Second instance should get different port (e.g., 54322)
```

## 📊 Performance Expectations

### First Launch (Cold Start)

```
0-5s   : App window appears
5-30s  : SurrealDB initializing (lots of RocksDB logs)
30-90s : Server ready (especially on first run)
90s+   : Frontend connects and ready to use
```

### Subsequent Launches (Warm Start)

```
0-5s   : App window appears
5-15s  : SurrealDB initializing (faster with existing data)
15-30s : Server ready
30s+   : Frontend connects
```

### Development Mode

```
0-5s   : App window appears
5-20s  : Server ready (debug symbols slow things down)
20s+   : Frontend connects
```

### Release Mode

```
0-2s   : App window appears
2-10s  : Server ready (optimized binaries)
10s+   : Frontend connects
```

## 🚀 Performance Optimization

### For Development

1. **Use release builds for Rust**:
   ```bash
   cargo build --release --manifest-path=src-tauri/Cargo.toml
   npm run dev  # Frontend still in dev mode
   ```

2. **Increase RocksDB cache**:
   ```rust
   // In Shannon API config
   block_cache_size: 1_073_741_824  // 1GB instead of 512MB
   ```

3. **Use SSD for data directory**:
   ```bash
   # macOS
   # Data automatically goes to SSD in ~/Library/Application Support
   ```

### For Production

1. **Build with optimizations**:
   ```bash
   npm run tauri:build
   ```

2. **Strip debug symbols**:
   ```toml
   # Cargo.toml
   [profile.release]
   strip = true
   lto = true
   ```

## 🔧 Common Fixes

### Fix 1: Reset Everything

```bash
# Stop any running instances
killall shannon-desktop

# Remove data directory
rm -rf ~/Library/Application\ Support/ai.prometheusags.planet

# Clean build artifacts
cd desktop
rm -rf .next out src-tauri/target node_modules

# Reinstall
npm install

# Rebuild
npm run tauri:build
```

### Fix 2: Check File Permissions

```bash
# Ensure data directory is writable
ls -la ~/Library/Application\ Support/ai.prometheusags.planet

# Should show:
drwxr-xr-x ... yourusername ... ai.prometheusags.planet

# If wrong owner, fix it:
sudo chown -R $USER ~/Library/Application\ Support/ai.prometheusags.planet
```

### Fix 3: Clear macOS Caches

```bash
# Clear system caches
sudo rm -rf /Library/Caches/com.apple.LaunchServices.dv.csstore

# Restart LaunchServices
killall Dock

# Rebuild app
cd desktop
npm run tauri:build
```

### Fix 4: Verify Environment Variables

```bash
# Check required variables are set
env | grep -E 'SHANNON|OPENAI|ANTHROPIC'

# Should see:
SHANNON_MODE=embedded
WORKFLOW_ENGINE=durable
DATABASE_DRIVER=surrealdb
```

## 📝 Logging Reference

### Log Levels

```rust
RUST_LOG=error   // Only errors
RUST_LOG=warn    // Warnings and errors
RUST_LOG=info    // Info, warnings, and errors (default)
RUST_LOG=debug   // Everything including debug messages
RUST_LOG=trace   // Maximum verbosity
```

### Component-specific Logging

```bash
# Only Shannon API logs
RUST_LOG=shannon_api=debug

# Only embedded API logs
RUST_LOG=app_lib::embedded_api=debug

# Only SurrealDB logs
RUST_LOG=surrealdb=debug

# Multiple components
RUST_LOG=shannon_api=debug,surrealdb=info
```

## 🆘 Getting Help

### Collect Diagnostic Information

```bash
# 1. System info
uname -a
sw_vers  # macOS

# 2. Rust version
rustc --version
cargo --version

# 3. Node version
node --version
npm --version

# 4. Tauri version
cd desktop
npm run tauri --version

# 5. Full logs
RUST_LOG=debug npm run tauri:dev 2>&1 | tee debug.log

# 6. Attach debug.log when asking for help
```

### Information to Include

When reporting issues, provide:

1. **Operating System** and version
2. **Error messages** from logs
3. **Steps to reproduce**
4. **Expected vs actual behavior**
5. **Full logs** (debug.log file)
6. **Timing information** (how long until failure)
7. **System resources** (RAM, disk space)

## ✅ Success Indicators

Your embedded API is working correctly when you see:

1. ✅ App window opens within 5 seconds
2. ✅ "Embedded API startup initiated in background thread" message
3. ✅ SurrealDB initialization completes (lots of RocksDB logs)
4. ✅ "Server ready event emitted (port: XXXXX)" with port > 1024
5. ✅ Frontend console shows "Server ready at http://127.0.0.1:XXXXX"
6. ✅ Health check succeeds: `curl http://localhost:XXXXX/health`
7. ✅ No connection errors in browser DevTools
8. ✅ Can submit tasks and see responses

## 🎯 Quick Reference

### Rebuild from Scratch
```bash
cd desktop
rm -rf node_modules .next out src-tauri/target
npm install
npm run tauri:build
```

### Run with Maximum Logging
```bash
RUST_LOG=debug RUST_BACKTRACE=full npm run tauri:dev
```

### Check Server Status
```bash
# From logs
grep "Server ready event emitted" tauri.log

# From network
lsof -i -P | grep shannon

# From API
curl http://localhost:{PORT}/health
```

### Force Clean Restart
```bash
killall shannon-desktop
rm -rf ~/Library/Application\ Support/ai.prometheusags.planet
npm run tauri:dev
```

---

**Last Updated:** 2026-01-09
**Version:** Desktop v0.1.0
**Tauri:** 2.9.6