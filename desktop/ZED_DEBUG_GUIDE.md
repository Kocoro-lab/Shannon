# Zed Editor - Tauri Debugging Guide

Complete guide for debugging the Shannon Tauri desktop application using Zed editor.

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Zed Setup](#zed-setup)
3. [Configuration Overview](#configuration-overview)
4. [Debugging Workflows](#debugging-workflows)
5. [Using Tasks](#using-tasks)
6. [Rust Development](#rust-development)
7. [TypeScript Development](#typescript-development)
8. [Terminal Integration](#terminal-integration)
9. [LSP Features](#lsp-features)
10. [Common Issues](#common-issues)
11. [Tips and Tricks](#tips-and-tricks)

---

## Prerequisites

### Required Software

1. **Zed Editor** - Latest version
   ```bash
   # macOS
   brew install --cask zed
   
   # Or download from https://zed.dev
   ```

2. **Rust toolchain** (stable channel)
   ```bash
   curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
   rustup default stable
   ```

3. **Node.js 20+** and npm
   ```bash
   node --version  # Should be 20.x or higher
   ```

4. **Platform-specific dependencies** (see TAURI_BUILD_GUIDE.md)

### Verify Installation

```bash
# Check installations
zed --version
rustc --version
cargo --version
node --version
npm --version

# Navigate to desktop directory
cd desktop

# Install Node dependencies
npm install
```

---

## Zed Setup

### Project Structure

Open the `desktop/` directory in Zed:

```bash
cd desktop
zed .
```

### Configuration Files

The `.zed/` directory contains:

1. **`settings.json`** - Editor and LSP configuration
2. **`tasks.json`** - Predefined development tasks

These files are automatically loaded when you open the project.

### Extension Setup

Zed has built-in support for:
- ✅ Rust (rust-analyzer)
- ✅ TypeScript/JavaScript (typescript-language-server)
- ✅ JSON, TOML, Markdown
- ✅ Git integration
- ✅ ESLint
- ✅ Prettier

No additional extensions needed! Everything works out of the box.

---

## Configuration Overview

### LSP Configuration

#### Rust Analyzer
- **Feature flags**: `desktop` enabled by default
- **Clippy**: Enabled on save
- **Inlay hints**: Type hints, parameter hints, chaining hints
- **Code lenses**: Run/Debug buttons above functions
- **Linked project**: `src-tauri/Cargo.toml`

#### TypeScript Language Server
- **Import style**: Relative imports preferred
- **Format on save**: Prettier
- **ESLint**: Auto-fix on save

### File Exclusions

The following directories are excluded from file search and indexing:
- `node_modules/`
- `.next/`, `out/`
- `target/`
- `.tauri/`
- Build artifacts and lock files

### Terminal Environment

Default environment variables:
```bash
RUST_BACKTRACE=1
RUST_LOG=debug
```

---

## Debugging Workflows

### Workflow 1: Full Application Development

**Best for**: Developing both frontend and backend

```bash
# 1. Start backend services (if using cloud mode)
# Open terminal in Zed: Ctrl+` (Cmd+` on macOS)
cd .. && make dev

# 2. In a new terminal pane (Cmd+Shift+T / Ctrl+Shift+T)
# Run task: Cmd+Shift+P → "task: spawn" → "tauri: dev"
# Or manually:
npm run tauri:dev
```

**What this does:**
- Builds Rust backend with hot-reload
- Starts Next.js dev server
- Opens Tauri window
- Watches for file changes

**Set breakpoints:**
- Use `println!()` or `dbg!()` in Rust code
- Use `console.log()` in TypeScript code
- View output in terminal

### Workflow 2: Rust Backend Only

**Best for**: Testing Tauri backend, IPC handlers, embedded API

```bash
# Run task: Cmd+Shift+P → "task: spawn" → "cargo: run"
# Or manually:
cd src-tauri
RUST_LOG=debug RUST_BACKTRACE=1 cargo run --features=desktop
```

**Debug output:**
- Terminal shows all `log::debug!()`, `log::info!()` output
- Backtraces on panics
- Compilation errors with clickable file paths

### Workflow 3: Frontend Only

**Best for**: UI development without Tauri wrapper

```bash
# Run task: Cmd+Shift+P → "task: spawn" → "next: dev"
# Or manually:
npm run dev
```

**Then open**: http://localhost:3000

**Debug in browser:**
- Chrome/Safari DevTools (F12)
- React DevTools extension
- Network tab for API calls

### Workflow 4: Building and Testing

**Build Rust backend:**
```bash
# Run task: "cargo: build (debug)"
# Or manually:
cargo build --manifest-path=src-tauri/Cargo.toml --features=desktop
```

**Build complete app:**
```bash
# Run task: "tauri: build"
# Or manually:
npm run tauri:build
```

**Run tests:**
```bash
# Run task: "cargo: test"
# Or manually:
cargo test --manifest-path=src-tauri/Cargo.toml
```

---

## Using Tasks

### Opening Task Panel

**Keyboard shortcuts:**
- `Cmd+Shift+P` (macOS) / `Ctrl+Shift+P` (Linux/Windows)
- Type: `task: spawn`
- Select task from list

**Or via Command Palette:**
- `Cmd+Shift+P` → Type task name → Press Enter

### Available Tasks

#### Development Tasks

| Task | Description | Use Case |
|------|-------------|----------|
| `tauri: dev` | Start Tauri dev mode | Full app development |
| `tauri: dev (with logging)` | Start with debug logging | Debug issues with full logs |
| `next: dev` | Next.js dev server only | Frontend-only development |
| `cargo: run` | Run Rust backend | Backend testing |

#### Build Tasks

| Task | Description | Output |
|------|-------------|--------|
| `cargo: build (debug)` | Build Rust (debug) | Fast builds with debug info |
| `cargo: build (release)` | Build Rust (optimized) | Optimized for performance |
| `tauri: build` | Build complete app | Platform-specific bundle |
| `tauri: build (desktop features)` | Build with desktop features | Full-featured build |
| `next: build` | Build Next.js | Static export |

#### Testing & Quality

| Task | Description | Output |
|------|-------------|--------|
| `cargo: test` | Run Rust tests | Test results |
| `cargo: clippy` | Run Rust linter | Lint warnings/errors |
| `cargo: check` | Quick compilation check | Faster than full build |
| `cargo: fmt` | Format Rust code | Auto-formatted files |

#### Utility Tasks

| Task | Description |
|------|-------------|
| `cargo: clean` | Clean Rust artifacts |
| `clean: all` | Clean everything (.next, out, target) |
| `install: dependencies` | Install npm packages |

#### Backend Management

| Task | Description |
|------|-------------|
| `backend: start (docker)` | Start Docker services |
| `backend: stop (docker)` | Stop Docker services |
| `backend: logs (docker)` | View backend logs |
| `backend: status (docker)` | Check service status |

### Task Output

Tasks run in Zed's integrated terminal:
- **Bottom panel** shows task output
- **Clickable errors** - Click file paths to jump to code
- **Multiple tasks** - Can run multiple tasks simultaneously
- **Task history** - Access previous task runs

---

## Rust Development

### Language Server Features

#### Inlay Hints
- **Type hints** - Shows inferred types inline
- **Parameter hints** - Shows parameter names in function calls
- **Chaining hints** - Shows types in method chains

Toggle hints: `Cmd+K` `Cmd+I` (macOS) or `Ctrl+K` `Ctrl+I` (Linux/Windows)

#### Code Lenses
Above functions you'll see:
- **Run** - Execute the function
- **Debug** - Run with debugging
- **Test** - Run if it's a test

Click these to run directly from editor.

#### Go to Definition
- `F12` or `Cmd+Click` - Jump to definition
- `Cmd+T` - Go to symbol in project
- `Cmd+Shift+O` - Go to symbol in file

#### Find References
- Right-click → "Find All References"
- `Shift+F12` - Show all references

### Debugging with Print Statements

**Quick debugging:**
```rust
// Quick debug print
dbg!(variable);

// Formatted debug
println!("Debug: {:?}", value);

// Logging (appears in terminal)
log::debug!("Processing: {:?}", data);
log::info!("Status: {}", status);
log::warn!("Warning: {}", warning);
log::error!("Error: {:?}", error);
```

**View output:**
- Terminal panel (bottom)
- Filtered by RUST_LOG level

### Error Navigation

When compilation fails:
1. **Errors appear** in Problems panel
2. **Click error** to jump to file and line
3. **Hover over** red underlines for details
4. **Quick fixes** appear as lightbulb icons

### Code Actions

Place cursor on code and:
- `Cmd+.` (macOS) / `Ctrl+.` (Linux/Windows)
- Select action:
  - Import items
  - Fill struct fields
  - Add derive attributes
  - Extract into function
  - And more...

### Cargo Commands in Terminal

```bash
# Check without building (fast)
cargo check --manifest-path=src-tauri/Cargo.toml

# Build with features
cargo build --features=desktop

# Run with logging
RUST_LOG=debug cargo run --features=desktop

# Test specific module
cargo test --manifest-path=src-tauri/Cargo.toml embedded_api

# Clippy with auto-fix
cargo clippy --fix --manifest-path=src-tauri/Cargo.toml

# Format code
cargo fmt --manifest-path=src-tauri/Cargo.toml
```

---

## TypeScript Development

### Language Server Features

#### IntelliSense
- Auto-completion for variables, functions, types
- Import suggestions
- Type checking in real-time

#### Go to Definition
- `F12` or `Cmd+Click` - Jump to definition
- Works across files and modules

#### Find All References
- Right-click → "Find All References"
- See all usages of a symbol

#### Rename Symbol
- `F2` - Rename across entire project
- Updates all references automatically

### Formatting

**Auto-format:**
- **On save** - Automatically formats with Prettier
- **Manual** - `Cmd+Shift+I` (macOS) / `Ctrl+Shift+I` (Linux/Windows)

**ESLint:**
- Auto-fixes on save
- Red/yellow underlines show issues
- Hover for details and quick fixes

### Debugging with Console

**In code:**
```typescript
// Basic logging
console.log('Value:', value);

// Debug with trace
console.debug('Debug info:', details);

// Warnings
console.warn('Warning:', issue);

// Errors
console.error('Error:', error);

// Show call stack
console.trace('Called from');

// Table view (great for objects)
console.table(arrayOfObjects);
```

**View output:**
- Browser DevTools (F12)
- Console tab
- Network tab for API calls

### React/Next.js Tips

**Component debugging:**
```typescript
useEffect(() => {
  console.log('Component mounted');
  console.log('Props:', props);
  
  return () => {
    console.log('Component unmounted');
  };
}, []);
```

**State debugging:**
```typescript
const [state, setState] = useState(initial);

// Log state changes
useEffect(() => {
  console.log('State changed:', state);
}, [state]);
```

---

## Terminal Integration

### Opening Terminal

**Keyboard shortcut:**
- `Ctrl+` ` (backtick) or `Cmd+` ` on macOS

**Split terminals:**
- `Cmd+Shift+T` (macOS) / `Ctrl+Shift+T` (Linux/Windows)

**Multiple terminal panes:**
- Drag terminal tab to split
- Each pane runs independently

### Terminal Features

#### Working Directory
- Automatically set to project root (`desktop/`)
- Change with `cd` command as needed

#### Environment Variables
Pre-configured:
```bash
RUST_BACKTRACE=1      # Show backtraces on panic
RUST_LOG=debug        # Enable debug logging
```

#### Shell Integration
- Clickable file paths (jumps to file)
- Error detection (highlights errors)
- Command history (up/down arrows)

### Common Terminal Commands

```bash
# Development
npm run tauri:dev              # Start Tauri dev mode
npm run dev                    # Start Next.js only

# Building
cargo build                    # Build Rust
npm run build                  # Build Next.js
npm run tauri:build            # Build complete app

# Testing
cargo test                     # Run Rust tests
cargo clippy                   # Lint Rust code
npm run lint                   # Lint TypeScript

# Cleaning
cargo clean                    # Clean Rust artifacts
rm -rf .next out              # Clean Next.js
rm -rf .next out src-tauri/target  # Clean everything

# Backend management
cd .. && make dev             # Start backend
cd .. && make down            # Stop backend
cd .. && make logs            # View backend logs
cd .. && make ps              # Check status
```

---

## LSP Features

### Rust Analyzer

#### Real-time Diagnostics
- Errors and warnings appear as you type
- Red underlines for errors
- Yellow underlines for warnings
- Hover for details

#### Code Completion
- Type-aware suggestions
- Imports auto-added
- Documentation in completion popup

#### Documentation
- Hover over item to see docs
- `Cmd+K` `Cmd+D` to show documentation panel

#### Macro Expansion
- Right-click on macro → "Expand Macro"
- See what code the macro generates

#### Cargo.toml Support
- Crate version suggestions
- Dependency completion
- Click crate name to open docs.rs

### TypeScript Language Server

#### Type Checking
- Real-time type errors
- Inferred types shown on hover
- Type definitions on `F12`

#### Auto-imports
- Suggestions include unimported items
- Auto-adds import when selected

#### Code Refactoring
- Extract function/variable
- Rename symbol (updates all references)
- Organize imports

#### JSDoc Support
- Documentation in hover tooltip
- Parameter hints from JSDoc

---

## Common Issues

### Issue 1: Rust Analyzer Not Working

**Symptoms:**
- No completions or diagnostics
- "rust-analyzer not found" message

**Solution:**
```bash
# Ensure rust-analyzer is installed
rustup component add rust-analyzer

# Or install standalone
brew install rust-analyzer  # macOS
cargo install rust-analyzer # Universal

# Restart Zed
Cmd+Q → Reopen Zed
```

### Issue 2: Changes Not Reflected

**Cause:** Cache or build artifacts

**Solution:**
```bash
# Clean everything
cd desktop
rm -rf .next out src-tauri/target

# Rebuild
npm run tauri:dev

# Or run task: "clean: all"
```

### Issue 3: Port Already in Use

**Symptoms:**
- "Address already in use" error
- Can't start dev server

**Solution:**
```bash
# Find and kill process on port 3000
lsof -ti:3000 | xargs kill -9

# Or port 8765 (embedded API)
lsof -ti:8765 | xargs kill -9

# Restart
npm run tauri:dev
```

### Issue 4: TypeScript Errors Not Showing

**Cause:** TypeScript server not started

**Solution:**
1. Open a `.ts` or `.tsx` file
2. Wait a few seconds for server to start
3. Check bottom right status bar for "TypeScript ✓"
4. If not showing, restart Zed

### Issue 5: Task Fails to Run

**Symptoms:**
- Task shows error immediately
- "Command not found"

**Solution:**
```bash
# Ensure in correct directory
pwd  # Should be /path/to/Shannon/desktop

# Check npm dependencies installed
ls node_modules  # Should exist

# Install if missing
npm install

# For cargo tasks, ensure manifest exists
ls src-tauri/Cargo.toml  # Should exist
```

### Issue 6: Slow Performance

**Cause:** Too many files indexed

**Solution:**
```bash
# Ensure exclusions are working
# Check .zed/settings.json has file_scan_exclusions

# Clean build artifacts
rm -rf target .next out node_modules/.cache

# Restart Zed
```

---

## Tips and Tricks

### Keyboard Shortcuts

| Action | macOS | Linux/Windows |
|--------|-------|---------------|
| Command Palette | Cmd+Shift+P | Ctrl+Shift+P |
| Go to File | Cmd+P | Ctrl+P |
| Go to Symbol | Cmd+T | Ctrl+T |
| Go to Definition | F12 | F12 |
| Find References | Shift+F12 | Shift+F12 |
| Rename Symbol | F2 | F2 |
| Format Document | Cmd+Shift+I | Ctrl+Shift+I |
| Open Terminal | Cmd+` | Ctrl+` |
| Split Terminal | Cmd+Shift+T | Ctrl+Shift+T |
| Toggle Inlay Hints | Cmd+K Cmd+I | Ctrl+K Ctrl+I |
| Quick Fix | Cmd+. | Ctrl+. |

### Multi-cursor Editing

1. **Add cursor**: `Cmd+Click` (macOS) / `Ctrl+Click` (Windows/Linux)
2. **Select all occurrences**: `Cmd+Shift+L` / `Ctrl+Shift+L`
3. **Edit simultaneously**: Type to edit all at once

### Project-wide Search

1. `Cmd+Shift+F` (macOS) / `Ctrl+Shift+F` (Linux/Windows)
2. Enter search term
3. Click results to jump to file
4. Replace across files with Replace button

### Split Panes

1. **Split editor**: Drag file to side/bottom
2. **Multiple files**: View side-by-side
3. **Terminal + editor**: Split with terminal pane

### Zen Mode

1. `Cmd+K Z` (macOS) / `Ctrl+K Z` (Linux/Windows)
2. Distraction-free editing
3. Exit with `Esc`

### Code Snippets

**Rust snippets** (type and press Tab):
- `fn` → function template
- `test` → test function
- `impl` → implementation block
- `derive` → derive attributes

**TypeScript/React snippets**:
- `rfc` → React functional component
- `usestate` → useState hook
- `useeffect` → useEffect hook

### Debugging Tips

#### Use dbg! Macro (Rust)
```rust
// Quick variable inspection
let result = dbg!(expensive_function());

// Multiple values
dbg!(&variable1, &variable2);
```

#### Conditional Compilation (Rust)
```rust
#[cfg(debug_assertions)]
println!("Debug only: {:?}", debug_info);

#[cfg(not(debug_assertions))]
println!("Release only");
```

#### Environment-specific Logging
```rust
// Will only appear if RUST_LOG=debug or higher
log::debug!("Detailed debug info");

// Always appears
log::error!("Critical error");
```

#### Console Grouping (TypeScript)
```typescript
console.group('API Call');
console.log('Request:', request);
console.log('Response:', response);
console.groupEnd();
```

### Git Integration

- **Inline blame**: See who changed each line
- **Git diff**: Modified lines highlighted
- **Status bar**: Current branch shown
- **Commit**: Use terminal: `git commit -m "message"`

### Performance Optimization

1. **Exclude directories** - Already configured in settings
2. **Close unused files** - Free up memory
3. **Restart Zed periodically** - Clear caches
4. **Use release builds** for performance testing

### Collaborative Features

- **Share workspace** - Via Zed's built-in collaboration
- **Pair programming** - Real-time cursor sharing
- **Code reviews** - Share specific files/lines

---

## Workflow Examples

### Example 1: Fixing a Bug

1. **Reproduce issue** in running app
2. **Add logging** where bug might be:
   ```rust
   log::debug!("Value before: {:?}", value);
   // buggy code
   log::debug!("Value after: {:?}", value);
   ```
3. **Run with logging**:
   ```bash
   RUST_LOG=debug npm run tauri:dev
   ```
4. **Review output** in terminal
5. **Identify issue** from logs
6. **Fix code**
7. **Hot reload** picks up changes automatically
8. **Test fix**

### Example 2: Adding New Feature

1. **Plan changes** - Decide what to modify
2. **Write tests first** (TDD):
   ```rust
   #[test]
   fn test_new_feature() {
       // Test code
   }
   ```
3. **Run tests** to see them fail:
   ```bash
   cargo test --manifest-path=src-tauri/Cargo.toml
   ```
4. **Implement feature**
5. **Run tests** until they pass
6. **Manual testing** in dev mode:
   ```bash
   npm run tauri:dev
   ```
7. **Format and lint**:
   ```bash
   cargo fmt && cargo clippy
   ```

### Example 3: Debugging IPC Issues

1. **Frontend side** - Add logging:
   ```typescript
   console.log('Calling IPC:', command, args);
   const result = await invoke(command, args);
   console.log('IPC result:', result);
   ```
2. **Backend side** - Add logging:
   ```rust
   #[tauri::command]
   async fn my_command(arg: String) -> Result<String, String> {
       log::debug!("Command called with: {}", arg);
       let result = process(&arg);
       log::debug!("Returning: {:?}", result);
       result.map_err(|e| e.to_string())
   }
   ```
3. **Run with logging**:
   ```bash
   RUST_LOG=debug npm run tauri:dev
   ```
4. **Trigger IPC** from UI
5. **Review logs** in both browser console and terminal
6. **Identify where communication breaks**

---

## Additional Resources

### Documentation
- [Zed Documentation](https://zed.dev/docs)
- [Tauri Documentation](https://tauri.app/v2/guides/)
- [Rust Analyzer Manual](https://rust-analyzer.github.io/manual.html)

### Related Files
- [DEBUG_GUIDE.md](./DEBUG_GUIDE.md) - VS Code debugging (complementary info)
- [TAURI_BUILD_GUIDE.md](./TAURI_BUILD_GUIDE.md) - Building and deployment
- [CONFIGURATION.md](./CONFIGURATION.md) - Configuration options
- [TROUBLESHOOTING.md](./TROUBLESHOOTING.md) - Common issues

### Zed-specific Settings
- `.zed/settings.json` - Project settings
- `.zed/tasks.json` - Task definitions
- `~/.config/zed/settings.json` - Global Zed settings (optional)

---

## Quick Reference

### Start Development
```bash
# Full stack
npm run tauri:dev

# Frontend only
npm run dev

# Rust only
cd src-tauri && cargo run --features=desktop
```

### Run Tasks
```
Cmd+Shift+P → "task: spawn" → Select task
```

### Common Commands
```bash
cargo check              # Fast compilation check
cargo build              # Build debug
cargo build --release    # Build optimized
cargo test               # Run tests
cargo clippy             # Lint
cargo fmt                # Format
npm run build            # Build Next.js
npm run tauri:build      # Build complete app
```

### Debugging
```rust
// Rust
dbg!(value);
log::debug!("Debug: {:?}", value);
```

```typescript
// TypeScript
console.log('Value:', value);
console.debug('Debug:', details);
```

### View Output
- **Terminal panel** (Ctrl/Cmd+`) - Rust logs
- **Browser console** (F12) - TypeScript logs
- **Problems panel** - Errors and warnings

---

## Getting Help

1. **Check documentation** in `desktop/` directory
2. **Review logs** in terminal or browser console
3. **Verify environment**: `cd .. && make check-env`
4. **Clean rebuild**: Run task "clean: all"
5. **Ask team** with:
   - What you were trying to do
   - What you expected
   - What actually happened
   - Relevant logs
   - Steps to reproduce

---

**Happy Debugging with Zed! 🚀**
