# Shannon Desktop App

Cross-platform desktop application for Shannon multi-agent AI platform, built with Next.js + Tauri.

**Supported Platforms:** macOS (Apple Silicon & Intel), Windows 10/11, Linux, **iOS 13.0+**

---

## Quick Start

```bash
# Development
npm install
npm run tauri:dev

# Production Build (Universal DMG for macOS)
npm run tauri:build
```

**Output:** `src-tauri/target/release/bundle/dmg/shannon-desktop_0.1.0_universal.dmg`

**Note:** Builds are configured as **universal binaries** by default, supporting both Apple Silicon (ARM64) and Intel (x86_64) Macs.

---

## Prerequisites

- Node.js 18+
- Rust 1.70+
- Xcode Command Line Tools

---

## Common Commands

```bash
# Clean build
rm -rf .next out src-tauri/target/release/bundle && npm run tauri:build

# Verify DMG checksum
cd src-tauri/target/release/bundle/dmg
shasum -a 256 shannon-desktop_0.1.0_universal.dmg

# Open built DMG
open src-tauri/target/release/bundle/dmg/shannon-desktop_0.1.0_universal.dmg

# Build for specific architecture only (if needed)
npm run tauri:build -- --target aarch64-apple-darwin  # Apple Silicon only
npm run tauri:build -- --target x86_64-apple-darwin   # Intel only
```

---

## Project Structure

```
desktop/
├── app/              # Next.js pages (React 19)
├── components/       # UI components (Shadcn/ui)
├── lib/              # Redux store, utilities
├── src-tauri/        # Rust backend
├── public/           # Static assets
└── package.json
```

---

## Troubleshooting

### Build Fails: Module Not Found

```bash
npm install @tauri-apps/plugin-shell
```

### TypeScript Errors in run-detail/page.tsx

Valid `runStatus` values: `"idle" | "running" | "completed" | "failed"`

Don't check for `"error"` - it's not a valid status.

### Universal Binary Requirements

**Required Rust targets** for universal macOS builds:

```bash
rustup target add aarch64-apple-darwin  # Apple Silicon
rustup target add x86_64-apple-darwin   # Intel
```

**Verify targets installed:**

```bash
rustup target list --installed
```

If a target is missing, the build will fail with architecture-specific errors.

---

## Architecture Notes

**Universal Binary Support:**
- macOS builds are configured as **universal binaries** by default
- Supports both Intel (x86_64) and Apple Silicon (ARM64) Macs in a single DMG
- Configured via `tauri.conf.json` → `bundle.macOS.targets: ["universal"]`
- Uses `lipo` under the hood to combine architecture-specific binaries

**Build Process:**
1. Tauri compiles Rust backend for both `aarch64-apple-darwin` and `x86_64-apple-darwin`
2. Next.js frontend is bundled (architecture-agnostic)
3. Both Rust binaries are combined into a universal binary
4. DMG is created with the universal binary

---

## Technology Stack

- **Framework:** Next.js 16.0.3 (Turbopack)
- **Runtime:** Tauri 2.x
- **UI:** Shadcn/ui + Tailwind CSS
- **State:** Redux Toolkit
- **Backend:** Shannon Gateway API (Go)

---

## API Configuration

Set Shannon API endpoint via environment variable:

```bash
# Copy example and configure
cp .env.local.example .env.local

# .env.local (default: localhost)
NEXT_PUBLIC_API_URL=http://localhost:8080
```

---

## Development Tips

1. **Hot Reload:** Changes to `app/` auto-reload in dev mode
2. **Rust Changes:** Require full rebuild (`npm run tauri:build`)
3. **Type Safety:** Run `npm run build` to catch TypeScript errors
4. **Redux DevTools:** Available in development mode

---

## Distribution

**Build Artifacts:**
- **macOS:** Universal DMG (`shannon-desktop_0.1.0_universal.dmg`) - supports both Intel and Apple Silicon
- **iOS:** `.app` bundle for simulator/device testing (see [iOS Build Guide](desktop-app-ios-build.md))
- **Windows:** MSI installer (see [Windows Build Guide](desktop-app-windows-build.md))
- **Linux:** AppImage/DEB (planned)

### iOS Build (Quick)

```bash
# Simulator
npm run tauri ios build -- --target aarch64-sim

# Physical device (requires Apple ID)
npm run tauri ios build -- --target aarch64
```

**Full iOS documentation:** See [desktop-app-ios-build.md](desktop-app-ios-build.md)

---

**Platforms:** macOS (universal: aarch64 + x86_64), iOS (aarch64), Windows (x86_64), Linux (x86_64)
**Version:** 0.1.0
**License:** See root LICENSE
