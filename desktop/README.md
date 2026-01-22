# Shannon Desktop App

Multi-platform desktop application for Shannon AI agents built with [Tauri](https://tauri.app/) and [Next.js](https://nextjs.org/).

## Quick Start

### Prerequisites

The desktop app connects to Shannon backend services. Start the backend first:

```bash
# From repository root
make dev

# Verify services are running
curl http://localhost:8080/health
```

### Option 1: Local Web UI (Development)

Run the UI as a local web application without building native binaries:

```bash
cd desktop

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

#### Download Pre-built Binaries

Download the latest release for your platform from [GitHub Releases](https://github.com/Kocoro-lab/Shannon/releases/latest):

| Platform | Formats |
|----------|---------|
| **macOS** (Intel & Apple Silicon) | `.dmg` installer, `.app.tar.gz` |
| **Windows** | `.msi` installer, `.exe` NSIS installer |
| **Linux** | `.AppImage` (portable), `.deb` (Debian/Ubuntu) |

#### Build from Source

Build the native desktop application for your platform:

```bash
cd desktop

# Install dependencies
npm install

# Build for your platform
npm run tauri:build

# Output locations:
# macOS:   src-tauri/target/universal-apple-darwin/release/bundle/dmg/
# Windows: src-tauri/target/release/bundle/msi/
# Linux:   src-tauri/target/release/bundle/appimage/
```

## Web UI vs Native App

| Feature | Web UI | Native App |
|---------|--------|------------|
| Quick Testing | Instant (`npm run dev`) | Requires build |
| System Integration | Limited | System tray, notifications |
| Offline History | No | Dexie.js local database |
| Performance | Browser overhead | Native rendering |
| File System Access | Limited | Full Tauri APIs |
| Auto-updates | No | Built-in updater |
| Memory Usage | Higher (browser) | Optimized |

## Development

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

Create `.env.local` for development:

```bash
# Backend API endpoint
NEXT_PUBLIC_API_URL=http://localhost:8080

# Optional: Enable debug mode
NEXT_PUBLIC_DEBUG=true
```

See [`.env.local.example`](.env.local.example) for all available options.

## Tech Stack

| Component | Technology |
|-----------|------------|
| Frontend Framework | [Next.js 16](https://nextjs.org/) with App Router |
| UI Components | [shadcn/ui](https://ui.shadcn.com/) + [Radix UI](https://www.radix-ui.com/) |
| Styling | [Tailwind CSS](https://tailwindcss.com/) |
| Desktop Runtime | [Tauri v2](https://tauri.app/) |
| State Management | [Zustand](https://zustand-demo.pmnd.rs/) + [Redux Toolkit](https://redux-toolkit.js.org/) |
| Local Database | [Dexie.js](https://dexie.org/) (IndexedDB wrapper) |
| Flow Diagrams | [@xyflow/react](https://reactflow.dev/) |
| Markdown Rendering | [react-markdown](https://github.com/remarkjs/react-markdown) |

## Building for Production

### Prerequisites

- **Node.js** 20+
- **Rust** (latest stable) — install from [rustup.rs](https://rustup.rs/)
- **Platform-specific dependencies**:
  - **macOS**: Xcode Command Line Tools (`xcode-select --install`)
  - **Windows**: Microsoft C++ Build Tools
  - **Linux**: See [Tauri Prerequisites](https://tauri.app/v2/guides/prerequisites/)

### Build Commands

```bash
# macOS (Universal Binary)
npm run tauri:build -- --target universal-apple-darwin

# Windows
npm run tauri:build -- --target x86_64-pc-windows-msvc

# Linux
npm run tauri:build -- --target x86_64-unknown-linux-gnu

# iOS (macOS only, requires Xcode)
npm run tauri ios build
```

## Auto-Updates

The desktop app includes automatic update checking:

- **Check on startup**: Looks for new releases from GitHub
- **Background downloads**: Downloads updates silently
- **User prompt**: Asks before installing updates

Configure update behavior in `src-tauri/tauri.conf.json`.

## Troubleshooting

### Web UI won't start

```bash
# Clear Next.js cache
rm -rf .next node_modules/.cache
npm install
npm run dev
```

### Connection to backend fails

```bash
# Verify backend is running
curl http://localhost:8080/health

# Check API URL in .env.local
cat .env.local | grep NEXT_PUBLIC_API_URL
```

### Tauri build fails

```bash
# Update Rust toolchain
rustup update

# Clean build artifacts
cd src-tauri && cargo clean && cd ..
npm run tauri:build
```

### macOS: "App is damaged" error

The app is not signed with an Apple Developer certificate. Allow it via:

```bash
# Remove quarantine attribute
xattr -cr /Applications/Shannon.app
```

Or: System Preferences → Privacy & Security → Open Anyway

## Contributing

See the main [CONTRIBUTING.md](../CONTRIBUTING.md) for development guidelines.

For desktop-specific contributions:
1. Follow the existing component patterns in `components/`
2. Use shadcn/ui components where applicable
3. Test both web mode (`npm run dev`) and native mode (`npm run tauri:dev`)

## Additional Resources

- [Tauri Documentation](https://tauri.app/v2/guides/)
- [Next.js Documentation](https://nextjs.org/docs)
- [Shannon API Documentation](../docs/)
- [Release Process](../RELEASING.md)

## License

MIT License — see [LICENSE](../LICENSE) for details.
