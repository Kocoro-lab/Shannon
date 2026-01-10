# Zed Editor - Quick Reference for Tauri Development

## 🚀 Quick Start

```bash
# Open project in Zed
cd desktop
zed .

# Start development
npm run tauri:dev
```

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

## 📋 Running Tasks

**Open Task Menu:**
```
Cmd+Shift+P → "task: spawn" → Select task
```

### Common Tasks

| Task | Purpose |
|------|---------|
| `tauri: dev` | Start full development mode |
| `tauri: dev (with logging)` | Start with debug logs |
| `next: dev` | Frontend only |
| `cargo: run` | Rust backend only |
| `cargo: build (debug)` | Build Rust debug mode |
| `cargo: build (release)` | Build optimized Rust |
| `cargo: test` | Run Rust tests |
| `cargo: clippy` | Lint Rust code |
| `cargo: fmt` | Format Rust code |
| `cargo: check` | Quick compilation check |
| `tauri: build` | Build complete app |
| `clean: all` | Clean all build artifacts |
| `backend: start (docker)` | Start backend services |
| `backend: logs (docker)` | View backend logs |

## 🔍 Debugging

### Rust Debugging

**Add debug statements:**
```rust
// Quick debug print
dbg!(variable);

// Formatted output
println!("Debug: {:?}", value);

// Logging (appears in terminal)
log::debug!("Debug info: {:?}", data);
log::info!("Information: {}", message);
log::warn!("Warning: {}", warning);
log::error!("Error: {:?}", error);
```

**Run with logging:**
```bash
RUST_LOG=debug npm run tauri:dev
# Or use task: "tauri: dev (with logging)"
```

**View output:**
- Terminal panel (Ctrl/Cmd+`)
- Look for colored log output

### TypeScript Debugging

**Add console statements:**
```typescript
console.log('Value:', value);
console.debug('Debug:', details);
console.warn('Warning:', issue);
console.error('Error:', error);
console.trace(); // Show call stack
console.table(arrayOfObjects); // Table view
```

**View output:**
- Browser DevTools (F12)
- Console tab

## 🛠️ Terminal Commands

### Development
```bash
npm run tauri:dev          # Full Tauri development
npm run dev                # Next.js only
cargo run --features=desktop  # Rust only
```

### Building
```bash
cargo build                # Build Rust debug
cargo build --release      # Build Rust optimized
npm run build              # Build Next.js
npm run tauri:build        # Build complete app
```

### Testing
```bash
cargo test                 # Run Rust tests
cargo clippy               # Lint Rust
cargo check                # Quick check
npm run lint               # Lint TypeScript
```

### Cleaning
```bash
cargo clean                # Clean Rust artifacts
rm -rf .next out          # Clean Next.js
rm -rf target .next out   # Clean everything
```

### Backend (Docker)
```bash
cd .. && make dev         # Start services
cd .. && make down        # Stop services
cd .. && make logs        # View logs
cd .. && make ps          # Check status
```

## 🎯 LSP Features

### Rust Analyzer

**Inlay Hints:**
- Type hints show inferred types
- Parameter hints show parameter names
- Toggle: `Cmd+K Cmd+I` / `Ctrl+K Ctrl+I`

**Code Lenses:**
- Click "Run" above functions to execute
- Click "Debug" for debug mode
- Click "Test" for test functions

**Code Actions:**
- Place cursor on code
- Press `Cmd+.` / `Ctrl+.`
- Select action (import, extract, derive, etc.)

**Go to Definition:**
- `F12` or `Cmd+Click` / `Ctrl+Click`

**Find References:**
- `Shift+F12` or right-click → "Find All References"

### TypeScript Language Server

**Auto-completion:**
- Type-aware suggestions
- Auto-imports added
- Documentation in popup

**Quick fixes:**
- `Cmd+.` / `Ctrl+.` on errors
- Auto-fix ESLint issues on save

**Refactoring:**
- Extract function/variable
- Rename symbol (F2)
- Organize imports

## 📁 Important Files

### Configuration
- `.zed/settings.json` - Project settings
- `.zed/tasks.json` - Task definitions
- `.env.local` - Environment variables
- `src-tauri/Cargo.toml` - Rust dependencies
- `package.json` - Node dependencies

### Source Files
**Rust:**
- `src-tauri/src/main.rs` - Entry point
- `src-tauri/src/lib.rs` - Core library
- `src-tauri/src/embedded_api.rs` - Embedded API
- `src-tauri/src/ipc_events.rs` - IPC handlers

**TypeScript:**
- `app/page.tsx` - Main page
- `lib/tauri-api.ts` - Tauri IPC calls
- `components/**/*.tsx` - UI components

## 🔧 Quick Fixes

### Breakpoints Not Available in Zed
Zed doesn't have a built-in debugger. Use:
- **Print debugging** - `dbg!()`, `println!()`, `console.log()`
- **Logging** - `log::debug!()`, `log::info!()`
- **External debuggers** - lldb, gdb for Rust; browser DevTools for TypeScript

### Rust Analyzer Not Working
```bash
# Install/update rust-analyzer
rustup component add rust-analyzer

# Restart Zed
Cmd+Q → Reopen
```

### Changes Not Reflected
```bash
# Clean build artifacts
rm -rf .next out src-tauri/target

# Restart dev mode
npm run tauri:dev
```

### Port Already in Use
```bash
# Kill process on port 3000
lsof -ti:3000 | xargs kill -9

# Kill process on port 8765
lsof -ti:8765 | xargs kill -9
```

### TypeScript Errors Not Showing
- Open a `.ts` or `.tsx` file
- Wait for server to start (check status bar)
- Restart Zed if needed

### Slow Performance
```bash
# Clean caches
rm -rf target .next out node_modules/.cache

# Restart Zed
```

## 🌐 Default Ports

| Service | Port | URL |
|---------|------|-----|
| Next.js Dev | 3000 | http://localhost:3000 |
| Gateway/Shannon API | 8080 | http://localhost:8080 |
| Embedded API (Tauri) | 8765 | http://localhost:8765 |
| Orchestrator Admin | 8081 | http://localhost:8081 |

## 🎨 Environment Variables

**Set in terminal:**
```bash
export RUST_LOG=debug
export RUST_BACKTRACE=1
export RUST_BACKTRACE=full  # More detailed
```

**Or in `.env.local`:**
```bash
NEXT_PUBLIC_API_URL=http://localhost:8080
NEXT_PUBLIC_USER_ID=user_01h0000000000000000000000
NEXT_PUBLIC_DEBUG=true
```

## 💡 Pro Tips

1. **Use split panes** - Drag files to side/bottom for side-by-side editing
2. **Multi-cursor editing** - Cmd+Click / Ctrl+Click to add cursors
3. **Project-wide search** - Cmd+Shift+F / Ctrl+Shift+F
4. **Quick file switching** - Cmd+P / Ctrl+P and type filename
5. **Symbol search** - Cmd+T / Ctrl+T for functions/types across project
6. **Zen mode** - Cmd+K Z / Ctrl+K Z for distraction-free coding
7. **Terminal splits** - Multiple terminals in bottom panel
8. **Git integration** - Inline blame and diff highlighting
9. **Hover for docs** - Hover over any symbol for documentation
10. **Code snippets** - Type `fn`, `impl`, `test` in Rust and press Tab

## 🐛 Debugging Workflows

### Full Stack Debugging
1. Run task: `tauri: dev (with logging)`
2. Add `log::debug!()` in Rust code
3. Add `console.log()` in TypeScript code
4. Trigger functionality
5. View terminal output (Rust) and browser console (TypeScript)

### IPC Communication Debugging
**Frontend (TypeScript):**
```typescript
console.log('Invoking IPC:', command, args);
const result = await invoke(command, args);
console.log('IPC result:', result);
```

**Backend (Rust):**
```rust
#[tauri::command]
async fn my_command(arg: String) -> Result<String, String> {
    log::debug!("Command received: {}", arg);
    // ... process ...
    log::debug!("Returning result");
    Ok(result)
}
```

### Backend Only Testing
```bash
# Terminal with logging
cd src-tauri
RUST_LOG=debug cargo run --features=desktop
```

### Frontend Only Testing
```bash
npm run dev
# Open http://localhost:3000
# Use browser DevTools (F12)
```

## 📚 Documentation

- [ZED_DEBUG_GUIDE.md](./ZED_DEBUG_GUIDE.md) - Complete Zed debugging guide
- [DEBUG_GUIDE.md](./DEBUG_GUIDE.md) - VS Code debugging (complementary)
- [TAURI_BUILD_GUIDE.md](./TAURI_BUILD_GUIDE.md) - Building and deployment
- [CONFIGURATION.md](./CONFIGURATION.md) - Configuration options
- [TROUBLESHOOTING.md](./TROUBLESHOOTING.md) - Common issues

## 🚨 Troubleshooting Checklist

- [ ] Rust toolchain installed? (`rustc --version`)
- [ ] Node.js 20+ installed? (`node --version`)
- [ ] Dependencies installed? (`npm install`)
- [ ] Backend running? (`cd .. && make ps`)
- [ ] Ports available? (3000, 8080, 8765)
- [ ] Environment set? (`.env.local` exists)
- [ ] Clean build? (`rm -rf target .next out`)
- [ ] Rust Analyzer working? (Check status bar)

## 📞 Getting Help

1. Check [ZED_DEBUG_GUIDE.md](./ZED_DEBUG_GUIDE.md)
2. Review terminal output and browser console
3. Verify environment: `cd .. && make check-env`
4. Clean rebuild: Run task "clean: all"
5. Ask team with: what, expected, actual, logs, steps to reproduce

---

**Zed Quick Reference for Shannon Tauri Desktop** 🚀