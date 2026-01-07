# Shannon Desktop App

Multi-platform desktop application for Shannon AI agents built with [Tauri](https://tauri.app/) and [Next.js](https://nextjs.org/).

## 🚀 Quick Start

### Option 1: Local Web UI (Development)

Run the UI as a local web application without building native binaries:

```bash
# Install dependencies
npm install

# Start development server
npm run dev

# Open http://localhost:3000
```

**Features in web mode:**
- Real-time SSE event streaming
- Session and task management
- Visual workflow execution
- Dark mode support
- Instant hot reload for development

### Option 2: Native Desktop App

#### Download Pre-built Binaries (Recommended)

Download the latest release for your platform from [GitHub Releases](https://github.com/Kocoro-lab/Shannon/releases/latest):

- **macOS** (Universal Binary - Intel & Apple Silicon)
  - `.dmg` installer (drag-and-drop installation)
  - `.app.tar.gz` for manual installation

- **Windows**
  - `.msi` installer (Windows Installer)
  - `.exe` NSIS installer (alternative)

- **Linux**
  - `.AppImage` (portable, no installation required)
  - `.deb` package (Debian/Ubuntu)

#### Build from Source

Build the native desktop application for your platform:

```bash
# Install dependencies
npm install

# Build for your platform
npm run tauri:build

# Output locations:
# macOS:   src-tauri/target/universal-apple-darwin/release/bundle/dmg/
# Windows: src-tauri/target/release/bundle/msi/
# Linux:   src-tauri/target/release/bundle/appimage/
```

**Additional build guides:**
- [macOS Build Guide](desktop-app-build-guide.md)
- [Windows Build Guide](desktop-app-windows-build.md)
- [iOS Build Guide](desktop-app-ios-build.md) (requires Xcode)

## 🎯 Why Use the Desktop App?

| Feature | Web UI | Native App |
|---------|--------|------------|
| **Quick Testing** | ✅ Instant (`npm run dev`) | ⚠️ Requires build |
| **System Integration** | ❌ | ✅ System tray, notifications |
| **Offline History** | ❌ | ✅ Dexie.js local database |
| **Performance** | ⚠️ Browser overhead | ✅ Native rendering |
| **File System Access** | ❌ Limited | ✅ Full Tauri APIs |
| **Auto-updates** | ❌ | ✅ Built-in updater |
| **Memory Usage** | ⚠️ Higher (browser) | ✅ Optimized |

## 🛠️ Development

### Project Structure

```
desktop/
├── app/              # Next.js app router pages
├── components/       # React components
│   ├── ui/          # shadcn/ui components
│   └── ...          # Custom components
├── lib/             # Utilities and helpers
├── hooks/           # React hooks
├── src-tauri/       # Tauri Rust backend
│   ├── src/        # Rust source code
│   ├── icons/      # App icons
│   └── Cargo.toml  # Rust dependencies
├── public/          # Static assets
└── package.json    # Node dependencies
```

### Available Scripts

```bash
# Development
npm run dev          # Next.js dev server (web mode)
npm run tauri:dev    # Tauri dev mode (native app with hot reload)

# Production
npm run build        # Build Next.js static export
npm run tauri:build  # Build native app for your platform

# Linting
npm run lint         # Run ESLint
```

### Environment Configuration

The desktop app needs to know how to connect to your Shannon backend. Create a `.env.local` file in the `desktop/` directory:

**Quick Setup (Automated):**

```bash
# From the project root
cd desktop
cp .env.local.example .env.local
```

The `.env.local` file is already configured for local development with these settings:

```bash
# Backend API endpoint (Shannon Gateway)
NEXT_PUBLIC_API_URL=http://localhost:8080

# Development mode - uses default user ID (no login required)
NEXT_PUBLIC_USER_ID=user_01h0000000000000000000000

# Enable debug logging
NEXT_PUBLIC_DEBUG=true
```

**Configuration Options:**

| Variable | Description | Default |
|----------|-------------|---------|
| `NEXT_PUBLIC_API_URL` | Shannon Gateway endpoint | `http://localhost:8080` |
| `NEXT_PUBLIC_USER_ID` | Default user for dev mode (bypasses auth) | `user_01h0000000000000000000000` |
| `NEXT_PUBLIC_API_KEY` | API key for authentication (optional) | - |
| `NEXT_PUBLIC_DEBUG` | Enable debug logging | `false` |

**Authentication Modes:**

1. **Development Mode (No Auth)**: Set `NEXT_PUBLIC_USER_ID` to bypass authentication
2. **API Key Auth**: Set `NEXT_PUBLIC_API_KEY` or log in to get one
3. **JWT Auth**: Use the login page to authenticate with email/password

See [`.env.local.example`](.env.local.example) for more details.

## 📦 Tech Stack

- **Frontend Framework**: [Next.js 16](https://nextjs.org/) with App Router
- **UI Components**: [shadcn/ui](https://ui.shadcn.com/) + [Radix UI](https://www.radix-ui.com/)
- **Styling**: [Tailwind CSS](https://tailwindcss.com/)
- **Desktop Runtime**: [Tauri v2](https://tauri.app/)
- **State Management**: [Zustand](https://zustand-demo.pmnd.rs/) + [Redux Toolkit](https://redux-toolkit.js.org/)
- **Local Database**: [Dexie.js](https://dexie.org/) (IndexedDB wrapper)
- **Flow Diagrams**: [@xyflow/react](https://reactflow.dev/)
- **Markdown Rendering**: [react-markdown](https://github.com/remarkjs/react-markdown)

## 🏗️ Building for Production

### Prerequisites

- **Node.js** 20+
- **Rust** (latest stable) - Install from [rustup.rs](https://rustup.rs/)
- **Platform-specific dependencies**:
  - **macOS**: Xcode Command Line Tools
  - **Windows**: Microsoft C++ Build Tools
  - **Linux**: See [Tauri Prerequisites](https://tauri.app/v2/guides/prerequisites/)

### Build Commands

**Quick Build for Local Docker Compose (Recommended):**

```bash
# Automated build script with checks
./build-local.sh
```

**Manual Build:**

```bash
# Build for current platform
npm run tauri:build

# Platform-specific builds
npm run tauri:build -- --target universal-apple-darwin  # macOS Universal
npm run tauri:build -- --target x86_64-pc-windows-msvc  # Windows
npm run tauri:build -- --target x86_64-unknown-linux-gnu  # Linux
npm run tauri ios build  # iOS (macOS only, requires Xcode)
```

**Important:** The built app will connect to `http://localhost:8080` (your local Docker Compose services). Make sure the backend is running before using the app:

```bash
cd ..
make dev
```

See [TAURI_BUILD_GUIDE.md](TAURI_BUILD_GUIDE.md) for detailed build instructions.

## 🔄 Updates

The desktop app includes automatic update checking:

- **Check on startup**: Looks for new releases from GitHub
- **Background updates**: Downloads updates silently
- **User prompt**: Asks before installing updates

Configure in `src-tauri/tauri.conf.json`.

## 🐛 Troubleshooting

### Web UI won't start

```bash
# Clear Next.js cache
rm -rf .next
npm install
npm run dev
```

### Tauri build fails

```bash
# Update Rust toolchain
rustup update

# Clean build artifacts
cd src-tauri
cargo clean
cd ..
npm run tauri:build
```

## 📚 Additional Resources

- [Tauri Documentation](https://tauri.app/v2/guides/)
- [Next.js Documentation](https://nextjs.org/docs)
- [Shannon Backend API](../docs/)

## 📄 License

MIT License - see [LICENSE](../LICENSE) for details.
