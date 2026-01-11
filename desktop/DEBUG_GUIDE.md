# Tauri Desktop App - Debugging Guide

This guide provides comprehensive instructions for debugging the Shannon desktop application, which is built with Tauri (Rust backend) and Next.js (frontend).

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [VS Code Setup](#vs-code-setup)
3. [Debugging Configurations](#debugging-configurations)
4. [Debugging Workflows](#debugging-workflows)
5. [Rust Backend Debugging](#rust-backend-debugging)
6. [Next.js Frontend Debugging](#nextjs-frontend-debugging)
7. [Full Stack Debugging](#full-stack-debugging)
8. [Common Issues](#common-issues)
9. [Advanced Debugging](#advanced-debugging)
10. [Performance Profiling](#performance-profiling)

---

## Prerequisites

### Required Tools

1. **Visual Studio Code** - Primary IDE
2. **Rust toolchain** (stable channel)
   ```bash
   curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
   ```

3. **Node.js 20+** and npm
   ```bash
   node --version  # Should be 20.x or higher
   ```

4. **VS Code Extensions** (see `.vscode/extensions.json`):
   - `rust-lang.rust-analyzer` - Rust language support
   - `vadimcn.vscode-lldb` - LLDB debugger for Rust
   - `tauri-apps.tauri-vscode` - Tauri development tools
   - `dbaeumer.vscode-eslint` - JavaScript/TypeScript linting
   - `esbenp.prettier-vscode` - Code formatting

### Platform-Specific Requirements

**macOS:**
```bash
xcode-select --install
```

**Linux (Ubuntu/Debian):**
```bash
sudo apt update
sudo apt install libwebkit2gtk-4.1-dev \
  build-essential \
  curl \
  wget \
  file \
  libxdo-dev \
  libssl-dev \
  libayatana-appindicator3-dev \
  librsvg2-dev
```

**Windows:**
- Install Microsoft C++ Build Tools
- Install Windows SDK

### Initial Setup

```bash
# Navigate to desktop directory
cd desktop

# Install Node dependencies
npm install

# Verify Tauri CLI is available
npm run tauri --version

# Build backend services (optional for embedded mode testing)
cd ..
make dev
```

---

## VS Code Setup

The `.vscode` directory contains pre-configured files for debugging:

- **launch.json** - Debug configurations
- **tasks.json** - Build and run tasks
- **settings.json** - Workspace settings
- **extensions.json** - Recommended extensions

### Install Recommended Extensions

When you open the project, VS Code will prompt you to install recommended extensions. Click "Install All" or manually install them from the Extensions view.

---

## Debugging Configurations

### Available Debug Configurations (F5 or Debug Panel)

#### 1. **Tauri Development Debug**
- Builds the Rust backend in debug mode (no default features)
- Useful for minimal testing without embedded features
- Fast compilation time

#### 2. **Tauri Development Debug (Desktop Features)** ⭐ RECOMMENDED
- Builds with `desktop` feature flag enabled
- Includes embedded Shannon API and SurrealDB
- Full desktop functionality
- Default for most development

#### 3. **Tauri Production Build Debug**
- Builds in release mode with optimizations
- Useful for debugging performance issues
- Longer compilation time

#### 4. **Next.js: Debug Frontend**
- Starts Next.js dev server with debugging enabled
- Automatically attaches debugger on port 9229
- Opens browser with DevTools

#### 5. **Chrome: Debug Frontend**
- Attaches Chrome debugger to running Next.js app
- Requires Next.js dev server to be running
- Use this after starting `npm run dev`

#### 6. **Attach to Tauri Process**
- Attach debugger to already running Tauri process
- Useful for debugging issues that occur after startup

#### 7. **Tauri: Debug Full Stack** (Compound) ⭐ RECOMMENDED
- Starts both Rust backend and Next.js frontend debugging
- Synchronized debugging experience
- Best for full application debugging

---

## Debugging Workflows

### Quick Start: Debug Full Application

1. **Start backend services** (if using cloud mode):
   ```bash
   cd .. && make dev
   ```

2. **Open Debug Panel** in VS Code (`Cmd+Shift+D` / `Ctrl+Shift+D`)

3. **Select "Tauri: Debug Full Stack"** from dropdown

4. **Press F5** to start debugging

5. **Set breakpoints** in both Rust and TypeScript code

### Workflow 1: Debug Rust Backend Only

**Use when:** Testing Tauri backend, IPC handlers, embedded API

1. Open Rust source file (e.g., `src-tauri/src/main.rs`)
2. Set breakpoints by clicking left of line numbers
3. Select "Tauri Development Debug (Desktop Features)"
4. Press F5
5. Application launches with debugger attached

**Breakpoint Tips:**
- `src-tauri/src/main.rs` - App initialization
- `src-tauri/src/embedded_api.rs` - Embedded API server
- `src-tauri/src/ipc_events.rs` - IPC event handlers
- `src-tauri/src/lib.rs` - Core library functions

### Workflow 2: Debug Next.js Frontend Only

**Use when:** Testing React components, UI logic, API calls

1. Open TypeScript/JSX file (e.g., `app/page.tsx`)
2. Set breakpoints in your code
3. Select "Next.js: Debug Frontend"
4. Press F5
5. Browser opens with debugger attached

**Alternative: Chrome DevTools**
```bash
npm run dev
# Then use Chrome DevTools (F12) or attach VS Code debugger
```

### Workflow 3: Debug IPC Communication

**Use when:** Debugging communication between frontend and backend

1. **Backend side:** Set breakpoint in `src-tauri/src/ipc_events.rs`
2. **Frontend side:** Set breakpoint in IPC call (e.g., `lib/tauri-api.ts`)
3. Start "Tauri: Debug Full Stack"
4. Trigger IPC call from frontend
5. Step through both sides

**Example IPC flow:**
```
Frontend (TypeScript) → invoke('command') → 
Backend (Rust) → #[tauri::command] → 
Process → Return → 
Frontend receives response
```

### Workflow 4: Debug with Docker Backend

**Use when:** Testing integration with real backend services

1. **Start Docker services:**
   ```bash
   cd .. && make dev
   ```

2. **Verify services are running:**
   ```bash
   make ps
   curl http://localhost:8080/health
   ```

3. **Configure environment:**
   ```bash
   # In desktop/.env.local
   NEXT_PUBLIC_API_URL=http://localhost:8080
   NEXT_PUBLIC_USER_ID=user_01h0000000000000000000000
   ```

4. **Start debugging:**
   - Select "Tauri: Debug Full Stack"
   - Press F5

5. **Monitor backend logs:**
   ```bash
   cd .. && make logs
   ```

---

## Rust Backend Debugging

### Setting Breakpoints

**File locations to debug:**
- `src-tauri/src/main.rs` - Application entry point
- `src-tauri/src/lib.rs` - Core library and initialization
- `src-tauri/src/embedded_api.rs` - Embedded API server
- `src-tauri/src/ipc_events.rs` - IPC command handlers
- `src-tauri/src/ipc_logger.rs` - Logging functionality

### Using LLDB Debugger

**Commands in Debug Console:**

```lldb
# Print variable
p variable_name

# Print with formatting
p/x variable_name  # Hex
p/d variable_name  # Decimal
p/t variable_name  # Binary

# Call function
call function_name(args)

# Step commands
step    # Step into
next    # Step over
finish  # Step out
continue # Continue execution

# Breakpoints
breakpoint list
breakpoint delete <number>
breakpoint disable <number>

# Backtrace
bt      # Show call stack
frame <n> # Switch to frame n
```

### Logging and Tracing

**Enable detailed logging:**

```rust
// In Rust code
use log::{debug, info, warn, error};

debug!("Detailed debug info: {:?}", value);
info!("General information: {}", message);
warn!("Warning: {}", warning);
error!("Error occurred: {:?}", err);
```

**Set log level:**
```bash
# In terminal before running
export RUST_LOG=debug
npm run tauri:dev

# Or in .vscode/settings.json (already configured)
"rust-analyzer.server.extraEnv": {
  "RUST_LOG": "debug"
}
```

**View logs:**
- Debug Console in VS Code
- Application Console (if running standalone)
- Tauri DevTools (when enabled)

### Debugging Crashes

**Enable backtrace:**
```bash
RUST_BACKTRACE=1 npm run tauri:dev
# Or
RUST_BACKTRACE=full npm run tauri:dev
```

**Common crash locations:**
- Panics in `.unwrap()` calls - Use breakpoints before unwrap
- Memory issues - Use `cargo clippy` to catch issues
- Thread panics - Check async/await code

### Feature Flags Debugging

Test different feature combinations:

```bash
# Desktop features (default)
cargo build --manifest-path=src-tauri/Cargo.toml --features=desktop

# Mobile features
cargo build --manifest-path=src-tauri/Cargo.toml --features=mobile

# Cloud mode only
cargo build --manifest-path=src-tauri/Cargo.toml --features=cloud

# No default features
cargo build --manifest-path=src-tauri/Cargo.toml --no-default-features
```

---

## Next.js Frontend Debugging

### Browser DevTools

**Chrome DevTools (Recommended):**
1. Press F12 or Cmd+Option+I (Mac) / Ctrl+Shift+I (Windows/Linux)
2. Navigate to Sources tab
3. Set breakpoints in webpack:// sources
4. Use Console for logging

**React DevTools:**
```bash
# Install React DevTools extension
# Chrome: https://chrome.google.com/webstore/detail/react-developer-tools/
```

### VS Code Debugging

**Setup:**
1. Start dev server: `npm run dev`
2. Attach Chrome debugger: Select "Chrome: Debug Frontend" → F5
3. Set breakpoints in `.tsx`/`.ts` files
4. Interact with app to trigger breakpoints

**Debug Console commands:**
```javascript
// Inspect React component
$r  // Selected component in React DevTools

// Query DOM
$$('.className')  // All elements with class

// Check state
console.log(state)
console.table(data)
```

### Common Frontend Debugging Scenarios

#### 1. API Call Issues

**Set breakpoints in:**
- `lib/tauri-api.ts` - Tauri IPC calls
- API route handlers (if using Next.js API routes)
- Component useEffect hooks

**Check network:**
```javascript
// In component
useEffect(() => {
  console.log('Fetching data...');
  fetchData()
    .then(data => console.log('Success:', data))
    .catch(err => console.error('Error:', err));
}, []);
```

#### 2. State Management Issues

**Redux DevTools:**
```bash
# Install Redux DevTools extension
# Monitor actions, state changes, time-travel debugging
```

**Zustand debugging:**
```javascript
// In store definition
const useStore = create(
  devtools((set) => ({
    // Your store
  }))
);
```

#### 3. Rendering Issues

**React DevTools Profiler:**
1. Open React DevTools
2. Go to Profiler tab
3. Click record
4. Interact with app
5. Stop recording
6. Analyze component render times

**Console logging:**
```javascript
useEffect(() => {
  console.log('Component mounted');
  return () => console.log('Component unmounted');
}, []);

console.log('Render:', { props, state });
```

---

## Full Stack Debugging

### Debugging IPC Communication

**Frontend (TypeScript):**
```typescript
// lib/tauri-api.ts
export async function invokeCommand(command: string, args?: any) {
  console.log('Invoking:', command, args);  // Debug log
  try {
    const result = await invoke(command, args);
    console.log('Result:', result);  // Debug log
    return result;
  } catch (error) {
    console.error('IPC Error:', error);  // Debug log
    throw error;
  }
}
```

**Backend (Rust):**
```rust
// src-tauri/src/ipc_events.rs
#[tauri::command]
async fn my_command(arg: String) -> Result<String, String> {
    log::debug!("Command called with arg: {}", arg);  // Debug log
    
    let result = do_something(&arg);
    
    log::debug!("Command returning: {:?}", result);  // Debug log
    result.map_err(|e| e.to_string())
}
```

**Set breakpoints on both sides and trace the flow:**
1. Frontend calls `invoke()`
2. Tauri IPC layer
3. Rust command handler
4. Return value
5. Frontend receives result

### Debugging Embedded API

**When using embedded Shannon API:**

```rust
// src-tauri/src/embedded_api.rs

// Set breakpoints in:
// - start_embedded_server() - Server initialization
// - handle_request() - Request handling
// - route handlers - Specific endpoint logic
```

**Monitor embedded server:**
```bash
# Check if server is running
curl http://localhost:8765/health

# Test endpoint
curl http://localhost:8765/v1/tasks -H "Content-Type: application/json"
```

**Debug Console Logs:**
- Open the in-app Debug Console to view embedded server logs.
- Startup logs are buffered until the renderer is ready, then replayed.
- Use this view to confirm readiness gating and port selection events.

### Cross-Layer Debugging Checklist

- [ ] Frontend sends correct data format
- [ ] IPC serialization works (JSON compatible types)
- [ ] Backend receives and deserializes correctly
- [ ] Backend processes request successfully
- [ ] Backend serializes response correctly
- [ ] Frontend receives and handles response
- [ ] Error handling works on both sides

---

## Common Issues

### Issue 1: Breakpoints Not Hitting

**Cause:** Source maps not loaded or wrong configuration

**Solution:**
```json
// Check .vscode/launch.json
{
  "sourceMaps": true,
  "sourceMapPathOverrides": {
    "webpack:///./*": "${webRoot}/*"
  }
}
```

**Rust:**
```bash
# Ensure debug symbols are included
cargo build  # Not --release
```

### Issue 2: "Cannot find module" in debugger

**Cause:** Node modules not installed or wrong working directory

**Solution:**
```bash
npm install
# Verify node_modules exists
ls -la node_modules

# Check working directory in launch.json
"cwd": "${workspaceFolder}"
```

### Issue 3: Rust debugger won't attach

**Cause:** LLDB not installed or wrong debugger type

**Solution:**
```bash
# macOS - Install Xcode Command Line Tools
xcode-select --install

# Linux - Install LLDB
sudo apt install lldb

# Check VS Code extension
# Install: vadimcn.vscode-lldb
```

### Issue 4: "Port already in use"

**Cause:** Previous dev server still running

**Solution:**
```bash
# Kill process on port 3000
lsof -ti:3000 | xargs kill -9

# Or use different port
PORT=3001 npm run dev
```

### Issue 5: Changes not reflected

**Cause:** Cache issues

**Solution:**
```bash
# Clear Next.js cache
rm -rf .next out

# Clear Rust cache
cd src-tauri && cargo clean

# Restart debugger
```

### Issue 6: Embedded API not starting

**Cause:** Missing features or port conflict

**Solution:**
```bash
# Check features are enabled
cargo build --features=desktop

# Check port 8765 is available
lsof -i :8765

# Check logs
RUST_LOG=debug npm run tauri:dev
```

---

## Advanced Debugging

### Memory Leak Detection

**Rust:**
```bash
# Use valgrind (Linux)
valgrind --leak-check=full ./target/debug/shannon-desktop

# Use instruments (macOS)
instruments -t Leaks target/debug/shannon-desktop
```

**JavaScript:**
```javascript
// Chrome DevTools → Memory → Take Heap Snapshot
// Compare snapshots to find leaks
```

### Performance Profiling

**Rust:**
```bash
# Install cargo-flamegraph
cargo install flamegraph

# Generate flamegraph
cd src-tauri
cargo flamegraph --bin shannon-desktop
```

**JavaScript:**
```javascript
// Use React Profiler API
import { Profiler } from 'react';

<Profiler id="App" onRender={onRenderCallback}>
  <App />
</Profiler>
```

### Network Traffic Inspection

**Monitor Tauri IPC:**
```rust
// Add middleware logging
#[tauri::command]
async fn logged_command(arg: String) -> Result<String, String> {
    let start = std::time::Instant::now();
    let result = actual_command(arg).await;
    log::info!("Command took {:?}", start.elapsed());
    result
}
```

**Monitor HTTP requests:**
```bash
# Use browser DevTools → Network tab
# Or use Charles Proxy / Wireshark
```

### Conditional Breakpoints

**VS Code:**
1. Right-click breakpoint
2. Select "Edit Breakpoint"
3. Choose "Expression" or "Hit Count"
4. Enter condition: `value > 100` or `hitCount % 10 == 0`

**LLDB:**
```lldb
breakpoint set -n function_name -c 'value > 100'
```

### Remote Debugging

**Debug on different machine:**

```bash
# Start LLDB server on target machine
lldb-server platform --listen 0.0.0.0:1234

# Connect from VS Code
# Add to launch.json:
{
  "type": "lldb",
  "request": "custom",
  "targetCreateCommands": ["target create ${workspaceFolder}/target/debug/app"],
  "processCreateCommands": ["gdb-remote 192.168.1.100:1234"]
}
```

### Debugging Tests

**Rust tests:**
```bash
# Run with debugger
rust-lldb --test target/debug/deps/test_name

# Or use VS Code test explorer
# Install: hbenl.vscode-test-explorer
```

**JavaScript tests:**
```bash
# Add to package.json
"scripts": {
  "test:debug": "node --inspect-brk node_modules/.bin/jest --runInBand"
}

# Then attach debugger
```

---

## Performance Profiling

### Rust Performance

**Benchmarking:**
```bash
# Add to Cargo.toml
[dev-dependencies]
criterion = "0.5"

# Run benchmarks
cargo bench
```

**CPU Profiling:**
```bash
# Install perf (Linux)
sudo apt install linux-tools-common linux-tools-generic

# Profile
perf record -g target/release/shannon-desktop
perf report
```

### Frontend Performance

**Lighthouse:**
```bash
# In Chrome DevTools → Lighthouse
# Run audit on localhost:3000
```

**Bundle Analysis:**
```bash
# Add to package.json
"analyze": "ANALYZE=true npm run build"

# Install analyzer
npm install -D @next/bundle-analyzer
```

---

## VS Code Tasks

Use these tasks from Command Palette (`Cmd+Shift+P` → "Tasks: Run Task"):

### Development Tasks
- **tauri: dev** - Start Tauri development mode
- **next: dev** - Start Next.js dev server only
- **Full Stack: Start Development** - Start backend + Tauri

### Build Tasks
- **cargo: build (debug)** - Build Rust in debug mode
- **cargo: build (release)** - Build Rust optimized
- **tauri: build** - Build complete Tauri application
- **next: build** - Build Next.js static export

### Testing Tasks
- **cargo: test** - Run Rust tests
- **cargo: clippy** - Run Rust linter
- **cargo: check** - Quick compilation check

### Utility Tasks
- **cargo: clean** - Clean Rust build artifacts
- **clean: all** - Clean both Rust and Next.js caches
- **backend: start (docker)** - Start Docker services
- **backend: stop (docker)** - Stop Docker services
- **backend: logs (docker)** - View Docker logs

---

## Tips and Best Practices

### General Debugging Tips

1. **Start with logging before using debugger** - Often faster to identify issues
2. **Use descriptive log messages** - Include context and values
3. **Test in isolation** - Debug individual components before full integration
4. **Reproduce reliably** - Find consistent steps to trigger the issue
5. **Binary search** - Comment out code sections to isolate problems
6. **Check the obvious** - Environment variables, file paths, permissions

### Rust-Specific Tips

1. **Use `dbg!()` macro** for quick debugging:
   ```rust
   let result = dbg!(expensive_function());
   ```

2. **Implement Debug trait** for custom types:
   ```rust
   #[derive(Debug)]
   struct MyStruct { /* ... */ }
   ```

3. **Use `expect()` with descriptive messages**:
   ```rust
   value.expect("Value should exist at this point")
   ```

4. **Enable overflow checks in release mode**:
   ```toml
   [profile.release]
   overflow-checks = true
   ```

### TypeScript-Specific Tips

1. **Use type guards**:
   ```typescript
   if (typeof value === 'string') {
     // TypeScript knows value is string here
   }
   ```

2. **Enable strict mode**:
   ```json
   "compilerOptions": {
     "strict": true
   }
   ```

3. **Use optional chaining**:
   ```typescript
   const value = obj?.prop?.nested;
   ```

4. **Type your errors**:
   ```typescript
   try {
     // ...
   } catch (error) {
     if (error instanceof Error) {
       console.error(error.message);
     }
   }
   ```

---

## Additional Resources

### Documentation
- [Tauri Debugging Guide](https://tauri.app/v2/guides/debug/)
- [VS Code Debugging](https://code.visualstudio.com/docs/editor/debugging)
- [Rust Debugging](https://www.rust-lang.org/learn)
- [Next.js Debugging](https://nextjs.org/docs/advanced-features/debugging)

### Tools
- [LLDB Documentation](https://lldb.llvm.org/)
- [Chrome DevTools](https://developer.chrome.com/docs/devtools/)
- [React DevTools](https://react.dev/learn/react-developer-tools)

### Related Files
- [TAURI_BUILD_GUIDE.md](./TAURI_BUILD_GUIDE.md) - Building and deployment
- [CONFIGURATION.md](./CONFIGURATION.md) - Configuration options
- [TROUBLESHOOTING.md](./TROUBLESHOOTING.md) - Common issues
- [README.md](./README.md) - Project overview

---

## Getting Help

If you encounter issues not covered in this guide:

1. **Check existing documentation** in the `desktop/` directory
2. **Review Tauri documentation** at https://tauri.app
3. **Check logs** using `make logs` for backend or browser console for frontend
4. **Verify environment** with `make check-env`
5. **Ask in team channels** with:
   - What you were trying to do
   - What you expected to happen
   - What actually happened
   - Relevant logs and error messages
   - Steps to reproduce

Happy debugging! 🐛🔍
