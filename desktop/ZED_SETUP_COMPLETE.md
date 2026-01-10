# Zed Editor Setup - Complete ✅

This document confirms that the Zed editor debugging setup for the Shannon Tauri desktop application has been completed successfully.

## 📦 What's Been Set Up

### Zed Configuration Files (`.zed/`)

All configuration files have been created in the `.zed/` directory:

1. **`settings.json`** - Project-specific editor and LSP configuration
   - Rust Analyzer with `desktop` feature flag
   - TypeScript/JavaScript with Prettier and ESLint
   - File exclusions (node_modules, target, .next, etc.)
   - Terminal environment with RUST_BACKTRACE and RUST_LOG
   - Inlay hints and code lenses enabled

2. **`tasks.json`** - 20+ predefined development tasks
   - Development tasks (tauri:dev, next:dev, cargo:run)
   - Build tasks (debug, release, complete app)
   - Testing tasks (test, clippy, check, fmt)
   - Utility tasks (clean, install, backend management)

3. **`README.md`** - Zed configuration documentation
   - File explanations
   - Feature overview
   - Customization guide
   - Troubleshooting tips

4. **`QUICKREF.md`** - Quick reference card
   - Essential keyboard shortcuts
   - Common tasks and commands
   - Debugging workflows
   - Quick fixes

### Documentation Files

1. **`ZED_DEBUG_GUIDE.md`** - Comprehensive debugging guide (950+ lines)
   - Prerequisites and setup
   - Configuration overview
   - Debugging workflows (Rust, TypeScript, Full Stack)
   - Using tasks and LSP features
   - Terminal integration
   - Common issues and solutions
   - Tips and tricks

2. **`ZED_SETUP_COMPLETE.md`** - This file (setup summary)

## 🚀 Getting Started

### Step 1: Verify Prerequisites

```bash
# Check Zed is installed
zed --version

# Check Rust toolchain
rustc --version  # Should be 1.91.1+

# Check Node.js
node --version   # Should be 20.x+

# Navigate to desktop directory
cd desktop

# Install dependencies
npm install
```

### Step 2: Open Project in Zed

```bash
cd desktop
zed .
```

Zed will automatically:
- Load settings from `.zed/settings.json`
- Start Rust Analyzer LSP
- Start TypeScript Language Server
- Configure terminal environment

### Step 3: Verify LSP Servers

Check the **status bar** (bottom right):
- **Rust icon** - rust-analyzer should be running
- **TypeScript icon** - typescript-language-server should be running

If not visible, wait a few seconds or restart Zed.

### Step 4: Start Development

**Option A: Use Tasks (Recommended)**

1. Press `Cmd+Shift+P` (macOS) or `Ctrl+Shift+P` (Linux/Windows)
2. Type: `task: spawn`
3. Select: `tauri: dev` or `tauri: dev (with logging)`
4. Task runs in bottom terminal panel

**Option B: Use Terminal**

1. Press `Cmd+\`` (macOS) or `Ctrl+\`` (Linux/Windows) to open terminal
2. Run: `npm run tauri:dev`

**Option C: Start Backend Separately (if using cloud mode)**

```bash
# In one terminal
cd .. && make dev

# In Zed terminal
npm run tauri:dev
```

## 🎯 Key Features

### Rust Development

**Inlay Hints** - Type annotations inline
- Toggle: `Cmd+K Cmd+I` (macOS) / `Ctrl+K Ctrl+I` (Linux/Windows)

**Code Lenses** - Clickable Run/Debug buttons
- Click above functions to execute
- Click "Test" for test functions

**Code Actions** - Quick fixes and refactorings
- Press `Cmd+.` (macOS) / `Ctrl+.` (Linux/Windows)
- Import, extract, derive, etc.

**Go to Definition** - Jump to source
- `F12` or `Cmd+Click` / `Ctrl+Click`

**Find References** - See all usages
- `Shift+F12`

### TypeScript Development

**Auto-formatting** - Prettier on save
**ESLint** - Auto-fix on save
**IntelliSense** - Type-aware completions with auto-imports
**Refactoring** - Extract, rename (F2), organize imports

### Terminal Integration

**Pre-configured environment:**
```bash
RUST_BACKTRACE=1    # Show backtraces
RUST_LOG=debug      # Debug logging
```

**Multiple terminals** - Split with `Cmd+Shift+T` / `Ctrl+Shift+T`
**Clickable paths** - Click errors to jump to code

## 📋 Common Tasks

Access via Command Palette (`Cmd+Shift+P` → `task: spawn`):

### Development
- `tauri: dev` - Full development mode
- `tauri: dev (with logging)` - With debug logs
- `next: dev` - Frontend only
- `cargo: run` - Backend only

### Building
- `cargo: build (debug)` - Fast debug build
- `cargo: build (release)` - Optimized build
- `tauri: build` - Complete app bundle
- `next: build` - Next.js static export

### Testing & Quality
- `cargo: test` - Run tests
- `cargo: clippy` - Lint code
- `cargo: check` - Quick check
- `cargo: fmt` - Format code

### Utilities
- `clean: all` - Clean everything
- `backend: start (docker)` - Start backend
- `backend: logs (docker)` - View logs

## 🔍 Debugging

Zed uses print/log-based debugging (no built-in debugger):

### Rust Debugging

```rust
// Quick debug print
dbg!(variable);

// Formatted output
println!("Debug: {:?}", value);

// Structured logging
log::debug!("Processing: {:?}", data);
log::info!("Status: {}", status);
log::error!("Error: {:?}", error);
```

**Run with logging:**
```bash
# Use task: "tauri: dev (with logging)"
# Or manually:
RUST_LOG=debug npm run tauri:dev
```

**View output:** Terminal panel (Ctrl/Cmd+`)

### TypeScript Debugging

```typescript
console.log('Value:', value);
console.debug('Debug:', details);
console.error('Error:', error);
console.table(arrayOfObjects);
```

**View output:** Browser DevTools (F12) → Console tab

## ⌨️ Essential Keyboard Shortcuts

| Action | macOS | Linux/Windows |
|--------|-------|---------------|
| Command Palette | Cmd+Shift+P | Ctrl+Shift+P |
| Go to File | Cmd+P | Ctrl+P |
| Go to Symbol | Cmd+T | Ctrl+T |
| Go to Definition | F12 | F12 |
| Find References | Shift+F12 | Shift+F12 |
| Rename Symbol | F2 | F2 |
| Format Document | Cmd+Shift+I | Ctrl+Shift+I |
| Quick Fix | Cmd+. | Ctrl+. |
| Open Terminal | Cmd+` | Ctrl+` |
| Split Terminal | Cmd+Shift+T | Ctrl+Shift+T |
| Project Search | Cmd+Shift+F | Ctrl+Shift+F |
| Toggle Inlay Hints | Cmd+K Cmd+I | Ctrl+K Ctrl+I |

## 🔧 Quick Fixes

### Rust Analyzer Not Working
```bash
rustup component add rust-analyzer
# Restart Zed
```

### Changes Not Reflected
```bash
rm -rf .next out src-tauri/target
npm run tauri:dev
```

### Port Already in Use
```bash
lsof -ti:3000 | xargs kill -9   # Next.js
lsof -ti:8765 | xargs kill -9   # Embedded API
```

### TypeScript Server Not Starting
- Open a `.ts` or `.tsx` file
- Wait for server initialization
- Check status bar
- Restart Zed if needed

## 🌐 Default Ports

| Service | Port | URL |
|---------|------|-----|
| Next.js Dev | 3000 | http://localhost:3000 |
| Gateway/Shannon API | 8080 | http://localhost:8080 |
| Embedded API (Tauri) | 8765 | http://localhost:8765 |
| Orchestrator Admin | 8081 | http://localhost:8081 |

## 📚 Documentation

All guides are in the `desktop/` directory:

1. **[ZED_DEBUG_GUIDE.md](./ZED_DEBUG_GUIDE.md)** - Complete debugging guide
   - Detailed workflows
   - LSP features
   - Terminal integration
   - Advanced tips

2. **[.zed/QUICKREF.md](./.zed/QUICKREF.md)** - Quick reference card
   - Common commands
   - Keyboard shortcuts
   - Quick fixes

3. **[.zed/README.md](./.zed/README.md)** - Configuration documentation
   - Settings explained
   - Task definitions
   - Customization guide

4. **[TAURI_BUILD_GUIDE.md](./TAURI_BUILD_GUIDE.md)** - Building and deployment
5. **[CONFIGURATION.md](./CONFIGURATION.md)** - Configuration options
6. **[TROUBLESHOOTING.md](./TROUBLESHOOTING.md)** - Common issues
7. **[DEBUG_GUIDE.md](./DEBUG_GUIDE.md)** - VS Code debugging (complementary)

## 💡 Pro Tips

1. **Use Command Palette** - `Cmd+Shift+P` / `Ctrl+Shift+P` for all commands
2. **Quick file navigation** - `Cmd+P` / `Ctrl+P` and type filename
3. **Symbol search** - `Cmd+T` / `Ctrl+T` for functions across project
4. **Multi-cursor editing** - `Cmd+Click` / `Ctrl+Click` to add cursors
5. **Split panes** - Drag files to side/bottom for side-by-side editing
6. **Zen mode** - `Cmd+K Z` / `Ctrl+K Z` for distraction-free coding
7. **Project-wide search** - `Cmd+Shift+F` / `Ctrl+Shift+F`
8. **Git integration** - Inline blame and diff highlighting built-in
9. **Hover for docs** - Hover over any symbol for documentation
10. **Use tasks** - Faster than typing commands manually

## 🎨 Workflow Examples

### Example 1: Full Stack Development

```bash
# 1. Start backend (if using cloud mode)
cd .. && make dev

# 2. In Zed, run task:
#    Cmd+Shift+P → "task: spawn" → "tauri: dev (with logging)"

# 3. Set breakpoints with logging:
#    Rust: log::debug!("Value: {:?}", value);
#    TypeScript: console.log('Value:', value);

# 4. View output:
#    Terminal panel (Rust logs)
#    Browser DevTools (TypeScript logs)
```

### Example 2: Rust Backend Only

```bash
# In Zed terminal (Cmd+`)
cd src-tauri
RUST_LOG=debug cargo run --features=desktop

# Add debug statements in code:
dbg!(variable);
log::debug!("Processing: {:?}", data);

# View output in terminal
```

### Example 3: Frontend Only

```bash
# In Zed terminal
npm run dev

# Open http://localhost:3000
# Use browser DevTools (F12) for debugging
```

### Example 4: Testing and Quality

```bash
# Run task: "cargo: test"
# Or manually:
cargo test

# Run task: "cargo: clippy"
# Or manually:
cargo clippy

# Format code:
cargo fmt
```

## 🚨 Troubleshooting Checklist

Before asking for help, verify:

- [ ] Zed installed and up to date
- [ ] Rust toolchain installed (`rustc --version`)
- [ ] Node.js 20+ installed (`node --version`)
- [ ] Dependencies installed (`npm install`)
- [ ] Backend running if needed (`cd .. && make ps`)
- [ ] Ports available (3000, 8080, 8765)
- [ ] Environment variables set (`.env.local` exists)
- [ ] Clean build attempted (`rm -rf target .next out`)
- [ ] Rust Analyzer running (check status bar)
- [ ] TypeScript server running (check status bar)

## 🎉 What You Can Do Now

1. ✅ **Develop full stack** - Both Rust and TypeScript with LSP support
2. ✅ **Use tasks** - Quick access to common operations
3. ✅ **Debug with logging** - Print/log-based debugging workflows
4. ✅ **Format automatically** - Code formatted on save
5. ✅ **Navigate efficiently** - Go to definition, find references, symbols
6. ✅ **Split editing** - Multiple panes for side-by-side editing
7. ✅ **Manage backend** - Docker commands via tasks
8. ✅ **Build and test** - Complete development lifecycle

## 📞 Getting Help

If you encounter issues:

1. **Check documentation** - Start with [ZED_DEBUG_GUIDE.md](./ZED_DEBUG_GUIDE.md)
2. **Review quick reference** - [.zed/QUICKREF.md](./.zed/QUICKREF.md)
3. **Verify environment** - `cd .. && make check-env`
4. **Check logs** - Terminal panel or browser console
5. **Clean rebuild** - Run task "clean: all"
6. **Ask team** - Provide:
   - What you were trying to do
   - What you expected
   - What actually happened
   - Relevant logs
   - Steps to reproduce

## 🆚 Zed vs VS Code

**Why use Zed?**
- ⚡ **Faster** - Native performance, instant startup
- 🎨 **Cleaner** - Minimal, focused interface
- 🧠 **Smart** - Built-in LSP support, no extension hunting
- 🤝 **Collaborative** - Built-in pair programming

**When to use VS Code?**
- Need visual debugger with breakpoints
- Require specific extensions not in Zed
- Prefer more mature ecosystem

**Best approach:**
- Use **Zed** for daily development (faster, cleaner)
- Use **VS Code** when you need the debugger
- Both configurations are set up and ready!

## ✅ Setup Complete

Your Zed editor is now fully configured for Shannon Tauri development!

**Next Steps:**

1. Open Zed in the desktop directory: `cd desktop && zed .`
2. Review [ZED_DEBUG_GUIDE.md](./ZED_DEBUG_GUIDE.md) for detailed workflows
3. Try running: `Cmd+Shift+P` → `task: spawn` → `tauri: dev`
4. Start coding! 🚀

---

**Setup completed:** $(date)
**Shannon Desktop Version:** 0.1.0
**Tauri Version:** 2.9.6
**Rust Version:** 1.91.1+
**Node Version:** 20.x+
**Zed Version:** Latest

Happy coding with Zed! ⚡✨