# Tauri Debugging Setup - Complete ✅

This document confirms the debugging setup for the Shannon Tauri desktop application has been completed successfully.

## 📦 What's Been Set Up

### VS Code Configuration Files (`.vscode/`)

All configuration files have been created in the `.vscode/` directory:

1. **`launch.json`** - Debug configurations for Rust and TypeScript
2. **`tasks.json`** - Build, test, and utility tasks
3. **`settings.json`** - Workspace settings optimized for Tauri development
4. **`extensions.json`** - Recommended VS Code extensions
5. **`README.md`** - Documentation for VS Code setup
6. **`DEBUG_QUICKREF.md`** - Quick reference card for common debugging tasks

### Documentation Files

1. **`DEBUG_GUIDE.md`** - Comprehensive debugging guide (900+ lines)
   - Prerequisites and setup instructions
   - Debugging workflows for Rust, TypeScript, and full stack
   - Common issues and solutions
   - Advanced debugging techniques
   - Performance profiling

2. **`DEBUGGING_SETUP_COMPLETE.md`** - This file (setup summary)

## 🚀 Getting Started

### Step 1: Install Required Extensions

When you open VS Code in the `desktop/` directory, you'll be prompted to install recommended extensions. Click "Install All" or install manually:

**Required:**
- `rust-lang.rust-analyzer` - Rust language support
- `vadimcn.vscode-lldb` - LLDB debugger for Rust
- `tauri-apps.tauri-vscode` - Tauri development tools
- `dbaeumer.vscode-eslint` - JavaScript/TypeScript linting
- `esbenp.prettier-vscode` - Code formatting

**Recommended:**
- `bradlc.vscode-tailwindcss` - Tailwind CSS IntelliSense
- `usernamehw.errorlens` - Inline error highlighting
- `ms-azuretools.vscode-docker` - Docker integration

### Step 2: Verify Prerequisites

```bash
# Check Rust toolchain
rustc --version  # Should be 1.91.1+

# Check Node.js
node --version   # Should be 20.x+

# Check dependencies
cd desktop
npm install

# Verify Tauri CLI
npm run tauri --version
```

### Step 3: Start Debugging

**Option A: Full Stack Debugging (Recommended)**
```bash
# 1. Start backend services (if using cloud mode)
cd .. && make dev

# 2. In VS Code
cd desktop
# Press F5 or open Debug panel (Cmd+Shift+D)
# Select "Tauri: Debug Full Stack"
# Press F5 to start
```

**Option B: Rust Backend Only**
```bash
# In VS Code
# Open Debug panel (Cmd+Shift+D)
# Select "Tauri Development Debug (Desktop Features)"
# Press F5
```

**Option C: Frontend Only**
```bash
# In VS Code
# Select "Next.js: Debug Frontend"
# Press F5
# Or run: npm run dev and use Chrome DevTools
```

## 🎯 Available Debug Configurations

Press F5 or use the Debug panel to access these configurations:

1. **Tauri: Debug Full Stack** ⭐
   - Debugs both Rust backend and Next.js frontend
   - Best for full application debugging
   - Synchronized breakpoints across both layers

2. **Tauri Development Debug (Desktop Features)**
   - Rust backend with embedded API enabled
   - Includes SurrealDB and Shannon API
   - Fast iteration for backend work

3. **Tauri Production Build Debug**
   - Release mode with optimizations
   - For debugging performance issues
   - Slower build but realistic performance

4. **Next.js: Debug Frontend**
   - Node.js debugger for Next.js dev server
   - Automatically opens browser with DevTools

5. **Chrome: Debug Frontend**
   - Attaches to running Next.js app
   - Use after starting `npm run dev`

6. **Attach to Tauri Process**
   - Debug already running Tauri application
   - Useful for debugging startup issues

## 📋 Common Tasks

Access via Command Palette (`Cmd+Shift+P` or `Ctrl+Shift+P`) → "Tasks: Run Task":

### Development Tasks
- `tauri: dev` - Start Tauri in development mode
- `next: dev` - Start Next.js dev server only
- `Full Stack: Start Development` - Start backend + Tauri

### Build Tasks
- `cargo: build (debug)` - Build Rust in debug mode
- `cargo: build (release)` - Build Rust optimized
- `tauri: build` - Build complete application bundle
- `next: build` - Build Next.js static export

### Testing Tasks
- `cargo: test` - Run Rust tests
- `cargo: clippy` - Run Rust linter
- `cargo: check` - Quick compilation check

### Utility Tasks
- `cargo: clean` - Clean Rust build artifacts
- `clean: all` - Clean everything (.next, out, target)
- `backend: start (docker)` - Start Docker services
- `backend: stop (docker)` - Stop Docker services
- `backend: logs (docker)` - View backend logs

## 🔍 Setting Breakpoints

### Rust Files (Backend)
Key files to set breakpoints in:
- `src-tauri/src/main.rs` - Application entry point
- `src-tauri/src/lib.rs` - Core initialization
- `src-tauri/src/embedded_api.rs` - Embedded API server
- `src-tauri/src/ipc_events.rs` - IPC command handlers
- `src-tauri/src/ipc_logger.rs` - Logging functionality

### TypeScript Files (Frontend)
Key files to set breakpoints in:
- `app/page.tsx` - Main page component
- `lib/tauri-api.ts` - Tauri IPC calls
- `components/**/*.tsx` - UI components
- `hooks/**/*.ts` - Custom React hooks

### Setting a Breakpoint
1. Open the file
2. Click in the left margin (gutter) next to the line number
3. A red dot appears = breakpoint set
4. Start debugging (F5)
5. Code execution pauses at breakpoint

## 🛠️ Debug Console Commands

### LLDB (Rust Debugging)
```lldb
p variable_name              # Print variable
p/x variable_name           # Print as hex
bt                          # Show backtrace
step                        # Step into function
next                        # Step over line
continue                    # Continue execution
breakpoint list             # List all breakpoints
```

### JavaScript Console
```javascript
$r                          # Selected React component
$$('.class')                # Query all elements
console.table(data)         # Display data as table
console.trace()             # Show call stack
```

## 📊 Logging

### Rust Logging
```rust
use log::{debug, info, warn, error};

debug!("Debug info: {:?}", value);
info!("Information: {}", message);
warn!("Warning: {}", warning);
error!("Error: {:?}", error);
```

Enable with:
```bash
RUST_LOG=debug npm run tauri:dev
RUST_BACKTRACE=1 npm run tauri:dev
```

### TypeScript Logging
```typescript
console.log('Log:', data);
console.debug('Debug:', details);
console.warn('Warning:', issue);
console.error('Error:', error);
```

## 🔧 Quick Fixes

### Breakpoints Not Hitting
```bash
rm -rf .next out src-tauri/target
npm run tauri:dev
```

### Port Already in Use
```bash
lsof -ti:3000 | xargs kill -9   # Next.js
lsof -ti:8765 | xargs kill -9   # Embedded API
```

### Debugger Won't Attach
```bash
# macOS
xcode-select --install

# Linux
sudo apt install lldb

# Check VS Code extension installed: vadimcn.vscode-lldb
```

### Changes Not Reflected
```bash
cd src-tauri && cargo clean && cd ..
rm -rf .next out
npm run tauri:dev
```

## 🌐 Default Ports

| Service | Port | URL |
|---------|------|-----|
| Next.js Dev Server | 3000 | http://localhost:3000 |
| Gateway/Shannon API | 8080 | http://localhost:8080 |
| Embedded API (Tauri) | 8765 | http://localhost:8765 |
| Orchestrator Admin | 8081 | http://localhost:8081 |

## 📦 Feature Flags

The Rust backend supports multiple feature flags:

```bash
# Desktop mode (default) - RocksDB + SurrealDB + embedded API
cargo build --features=desktop

# Mobile mode - SQLite + lighter weight
cargo build --features=mobile

# Cloud mode - Remote API only, no embedded features
cargo build --features=cloud

# No default features
cargo build --no-default-features
```

Configure in `.vscode/settings.json`:
```json
"rust-analyzer.cargo.features": ["desktop"]
```

## 🎯 Keyboard Shortcuts

| Action | macOS | Windows/Linux |
|--------|-------|---------------|
| Start Debugging | F5 | F5 |
| Stop Debugging | Shift+F5 | Shift+F5 |
| Step Over | F10 | F10 |
| Step Into | F11 | F11 |
| Step Out | Shift+F11 | Shift+F11 |
| Continue | F5 | F5 |
| Toggle Breakpoint | F9 | F9 |
| Debug Console | Cmd+Shift+Y | Ctrl+Shift+Y |
| Run Task | Cmd+Shift+P | Ctrl+Shift+P |
| Open Debug Panel | Cmd+Shift+D | Ctrl+Shift+D |

## 📚 Documentation

All documentation is available in the `desktop/` directory:

1. **[DEBUG_GUIDE.md](./DEBUG_GUIDE.md)** - Complete debugging guide (900+ lines)
   - Detailed workflows for all scenarios
   - Advanced debugging techniques
   - Performance profiling
   - Troubleshooting common issues

2. **[.vscode/DEBUG_QUICKREF.md](./.vscode/DEBUG_QUICKREF.md)** - Quick reference card
   - Common commands
   - Quick fixes
   - Debugging scenarios

3. **[.vscode/README.md](./.vscode/README.md)** - VS Code setup documentation
   - File explanations
   - Customization guide
   - Troubleshooting

4. **[TAURI_BUILD_GUIDE.md](./TAURI_BUILD_GUIDE.md)** - Building and deployment
5. **[CONFIGURATION.md](./CONFIGURATION.md)** - Configuration options
6. **[TROUBLESHOOTING.md](./TROUBLESHOOTING.md)** - Common issues

## 🚨 Troubleshooting Checklist

Before asking for help, verify:

- [ ] VS Code extensions installed (check Extensions view)
- [ ] Rust toolchain installed (`rustc --version`)
- [ ] Node.js 20+ installed (`node --version`)
- [ ] Dependencies installed (`npm install`)
- [ ] Backend services running (`cd .. && make ps`)
- [ ] Ports available (3000, 8080, 8765)
- [ ] Environment variables set (`.env.local` exists)
- [ ] Clean build attempted (`cargo clean`, `rm -rf .next out`)

## 💡 Pro Tips

1. **Start with logging** - Often faster than debugging
2. **Use `dbg!()` macro in Rust** - Quick variable inspection
3. **Enable source maps** - Already configured
4. **Use conditional breakpoints** - Right-click breakpoint → Edit
5. **Test in isolation** - Debug components independently
6. **Check the obvious first** - Env vars, paths, permissions
7. **Use type guards in TypeScript** - Better debugging experience
8. **Profile before optimizing** - Use built-in profilers

## 🔗 Related Documentation

From the project root (`Shannon/`):

- **`.cursorrules`** - Project coding standards (especially Rust guidelines)
- **`docs/coding-standards/RUST.md`** - Mandatory Rust coding standards
- **`README.md`** - Project overview
- **`Makefile`** - Available make commands

## 🎉 What You Can Do Now

1. **Debug the full stack** - Set breakpoints in both Rust and TypeScript
2. **Step through IPC calls** - See communication between frontend and backend
3. **Inspect state** - Use React DevTools and LLDB console
4. **Profile performance** - Use built-in profiling tools
5. **Run tests** - Execute and debug unit tests
6. **Build and test** - Create production builds and debug them
7. **Use tasks** - Quick access to common operations
8. **Monitor logs** - Track execution flow in real-time

## 📞 Getting Help

If you encounter issues:

1. **Check documentation** - Start with DEBUG_GUIDE.md or DEBUG_QUICKREF.md
2. **Review logs** - `make logs` for backend, browser console for frontend
3. **Verify environment** - `cd .. && make check-env`
4. **Clean rebuild** - `cargo clean && rm -rf .next out`
5. **Ask for help** - Provide:
   - What you were trying to do
   - What you expected to happen
   - What actually happened
   - Relevant logs and error messages
   - Steps to reproduce

## ✅ Setup Complete

Your Tauri debugging environment is now fully configured! 

**Next Steps:**
1. Install recommended VS Code extensions
2. Review the DEBUG_GUIDE.md for detailed workflows
3. Try the "Tauri: Debug Full Stack" configuration (F5)
4. Set some breakpoints and explore!

Happy debugging! 🐛🔍✨

---

**Setup completed:** $(date)
**Shannon Desktop Version:** 0.1.0
**Tauri Version:** 2.9.6
**Rust Version:** 1.91.1+
**Node Version:** 20.x+