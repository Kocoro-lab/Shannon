# Zed Editor Configuration for Shannon Tauri Desktop

This directory contains Zed-specific configuration files for developing the Shannon Tauri desktop application.

## 📁 Files Overview

### `settings.json`
Project-specific editor and Language Server Protocol (LSP) settings.

**Key configurations:**
- **Rust Analyzer** - Configured with `desktop` feature flag
  - Clippy on save
  - Inlay hints (types, parameters, chaining)
  - Code lenses for Run/Debug
  - Linked to `src-tauri/Cargo.toml`
- **TypeScript/JavaScript** - Prettier formatting with ESLint
- **File exclusions** - Ignores `node_modules`, `.next`, `target`, etc.
- **Terminal environment** - Pre-set `RUST_BACKTRACE=1` and `RUST_LOG=debug`

### `tasks.json`
Predefined development tasks accessible via Command Palette.

**Task categories:**
- Development: `tauri: dev`, `next: dev`, `cargo: run`
- Building: `cargo: build`, `tauri: build`, `next: build`
- Testing: `cargo: test`, `cargo: clippy`, `cargo: check`
- Utilities: Clean, install, backend management (Docker)

### `QUICKREF.md`
Quick reference card with common commands, shortcuts, and debugging tips.

## 🚀 Getting Started

### 1. Open Project in Zed

```bash
cd desktop
zed .
```

Zed will automatically load settings from `.zed/settings.json`.

### 2. Verify LSP Servers

Check the status bar (bottom right):
- **Rust** - Should show rust-analyzer icon
- **TypeScript** - Should show typescript icon

If not showing, wait a few seconds or restart Zed.

### 3. Run a Task

**Via Command Palette:**
1. Press `Cmd+Shift+P` (macOS) or `Ctrl+Shift+P` (Linux/Windows)
2. Type `task: spawn`
3. Select task (e.g., `tauri: dev`)
4. Task runs in bottom terminal panel

**Via Terminal:**
Alternatively, use the integrated terminal (`Cmd+\`` or `Ctrl+\``):
```bash
npm run tauri:dev
```

## 🎯 Key Features

### Rust Development

**Inlay Hints:**
- Type annotations appear inline
- Parameter names shown in function calls
- Toggle: `Cmd+K Cmd+I` (macOS) or `Ctrl+K Ctrl+I` (Linux/Windows)

**Code Lenses:**
- Clickable "Run" and "Debug" buttons above functions
- "Test" button above test functions

**Code Actions:**
- Quick fixes and refactorings
- Press `Cmd+.` (macOS) or `Ctrl+.` (Linux/Windows)
- Import items, extract functions, add derives, etc.

**Go to Definition:**
- `F12` or `Cmd+Click` (macOS) / `Ctrl+Click` (Linux/Windows)
- Works across files and crates

### TypeScript Development

**Auto-formatting:**
- Prettier on save
- ESLint auto-fix on save

**IntelliSense:**
- Type-aware completions
- Auto-imports
- Documentation on hover

**Refactoring:**
- Rename symbol: `F2`
- Extract function/variable
- Organize imports

### Terminal Integration

**Pre-configured environment:**
```bash
RUST_BACKTRACE=1    # Show backtraces on panic
RUST_LOG=debug      # Enable debug logging
```

**Multiple terminals:**
- Split with `Cmd+Shift+T` (macOS) or `Ctrl+Shift+T` (Linux/Windows)
- Each pane runs independently

**Clickable file paths:**
- Click error messages to jump to code

## 📋 Common Tasks

### Development
- `tauri: dev` - Full Tauri development mode
- `tauri: dev (with logging)` - With debug logging enabled
- `next: dev` - Next.js dev server only
- `cargo: run` - Rust backend only

### Building
- `cargo: build (debug)` - Fast debug build
- `cargo: build (release)` - Optimized release build
- `tauri: build` - Complete application bundle
- `next: build` - Next.js static export

### Testing & Quality
- `cargo: test` - Run Rust tests
- `cargo: clippy` - Lint Rust code
- `cargo: check` - Quick compilation check
- `cargo: fmt` - Format Rust code

### Utilities
- `clean: all` - Clean all build artifacts
- `cargo: clean` - Clean Rust artifacts only
- `backend: start (docker)` - Start backend services
- `backend: logs (docker)` - View backend logs

## 🔍 Debugging

Zed doesn't have a built-in debugger, but provides excellent support for print/log-based debugging.

### Rust Debugging

**Add debug statements:**
```rust
// Quick debug macro
dbg!(variable);

// Formatted print
println!("Debug: {:?}", value);

// Structured logging (appears in terminal)
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

**View output:**
Terminal panel (bottom) - Press `Cmd+\`` or `Ctrl+\``

### TypeScript Debugging

**Add console statements:**
```typescript
console.log('Value:', value);
console.debug('Debug details:', details);
console.error('Error:', error);
console.table(arrayOfObjects);  // Table view
```

**View output:**
Browser DevTools (F12) → Console tab

## 🛠️ Customization

### Changing Feature Flags

Edit `.zed/settings.json`:
```json
"lsp": {
  "rust-analyzer": {
    "initialization_options": {
      "cargo": {
        "features": ["mobile"]  // Change from "desktop" to "mobile"
      }
    }
  }
}
```

### Adding Custom Tasks

Edit `.zed/tasks.json`:
```json
{
  "label": "my-custom-task",
  "command": "echo",
  "args": ["Hello World"],
  "tags": ["custom"],
  "reveal": "always"
}
```

### Adjusting Terminal Environment

Edit `.zed/settings.json`:
```json
"terminal": {
  "env": {
    "RUST_LOG": "trace",  // More verbose logging
    "MY_CUSTOM_VAR": "value"
  }
}
```

## ⚙️ Settings Explained

### LSP Configuration

**rust-analyzer:**
- `check.command: "clippy"` - Use Clippy for checks
- `cargo.features: ["desktop"]` - Enable desktop features
- `procMacro.enable: true` - Process procedural macros
- `inlayHints.enable: true` - Show type hints inline

**typescript-language-server:**
- Relative imports preferred
- Auto-formatting with Prettier

**eslint:**
- Auto-fix on save
- Validate TypeScript and JavaScript files

### File Exclusions

Excluded from indexing and search:
- `node_modules/` - Node dependencies
- `.next/`, `out/` - Next.js build artifacts
- `target/` - Rust build artifacts
- `.tauri/` - Tauri cache
- `Cargo.lock`, `package-lock.json` - Lock files

### Format on Save

Enabled for:
- Rust (rust-analyzer formatter)
- TypeScript/JavaScript (Prettier)
- JSON (Prettier)
- TOML (built-in)

## 🎨 Editor Preferences

These can be customized in `.zed/settings.json`:

```json
{
  "tab_size": 2,                    // Default tab size
  "hard_tabs": false,                // Use spaces
  "show_whitespaces": "selection",   // Show whitespace
  "remove_trailing_whitespace_on_save": true,
  "ensure_final_newline_on_save": true,
  "format_on_save": "on",            // Auto-format
  "preferred_line_length": 100,      // Wrap guide
  "soft_wrap": "none"                // Don't wrap lines
}
```

## 📚 Documentation

- **[ZED_DEBUG_GUIDE.md](../ZED_DEBUG_GUIDE.md)** - Complete debugging guide
- **[QUICKREF.md](./QUICKREF.md)** - Quick reference card
- **[TAURI_BUILD_GUIDE.md](../TAURI_BUILD_GUIDE.md)** - Building and deployment
- **[CONFIGURATION.md](../CONFIGURATION.md)** - Configuration options
- **[TROUBLESHOOTING.md](../TROUBLESHOOTING.md)** - Common issues

## 🔧 Troubleshooting

### Rust Analyzer Not Starting

```bash
# Install rust-analyzer component
rustup component add rust-analyzer

# Or install standalone
cargo install rust-analyzer

# Restart Zed
```

### TypeScript Server Not Working

- Open a `.ts` or `.tsx` file
- Wait a few seconds for initialization
- Check status bar for TypeScript icon
- Restart Zed if not showing

### Settings Not Taking Effect

- Ensure `.zed/settings.json` has valid JSON
- Check for syntax errors (trailing commas, missing quotes)
- Restart Zed: `Cmd+Q` → Reopen

### Tasks Not Running

- Verify you're in the `desktop/` directory
- Check `npm install` has been run
- Ensure task commands exist (`cargo`, `npm`, etc.)

### Slow Performance

```bash
# Clean build artifacts
rm -rf target .next out node_modules/.cache

# Restart Zed
```

## 💡 Tips

1. **Use Command Palette** - `Cmd+Shift+P` / `Ctrl+Shift+P` for all commands
2. **Quick file navigation** - `Cmd+P` / `Ctrl+P` and type filename
3. **Symbol search** - `Cmd+T` / `Ctrl+T` for functions/types
4. **Multi-cursor** - `Cmd+Click` / `Ctrl+Click` to add cursors
5. **Split panes** - Drag files to side/bottom for side-by-side
6. **Zen mode** - `Cmd+K Z` / `Ctrl+K Z` for distraction-free
7. **Project search** - `Cmd+Shift+F` / `Ctrl+Shift+F` for find across files
8. **Git integration** - Inline blame and diff highlighting built-in

## 🌐 Environment Variables

Set in `.env.local` (project root):
```bash
NEXT_PUBLIC_API_URL=http://localhost:8080
NEXT_PUBLIC_USER_ID=user_01h0000000000000000000000
NEXT_PUBLIC_DEBUG=true
```

Backend configuration:
```bash
RUST_LOG=debug
RUST_BACKTRACE=1
```

## 🚀 Quick Start Commands

```bash
# Full development
npm run tauri:dev

# Frontend only
npm run dev

# Backend only
cd src-tauri && cargo run --features=desktop

# Run tests
cargo test

# Lint and format
cargo clippy && cargo fmt

# Build complete app
npm run tauri:build
```

## 📞 Getting Help

1. Check [ZED_DEBUG_GUIDE.md](../ZED_DEBUG_GUIDE.md) for detailed workflows
2. Review [QUICKREF.md](./QUICKREF.md) for quick solutions
3. Verify environment: `cd .. && make check-env`
4. View logs: Terminal panel or browser console
5. Ask team with context, expected behavior, actual behavior, and logs

---

**Zed Configuration for Shannon Tauri Desktop** - Optimized for Rust and TypeScript development 🚀