# Shannon Desktop App - Build & Distribution Guide

Complete guide for building and distributing the Shannon Desktop macOS application (Tauri-based).

---

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Build Commands](#build-commands)
3. [Troubleshooting](#troubleshooting)
4. [Distribution](#distribution)
5. [Verification](#verification)
6. [Architecture](#architecture)

---

## Prerequisites

### Required Software

- **Node.js** 18+ (with npm)
- **Rust** 1.70+ (via rustup)
- **Xcode Command Line Tools** (macOS only)
- **Tauri CLI** (installed via npm)

### Installation

```bash
# Install Node.js dependencies
cd desktop
npm install

# Verify Tauri setup
npm run tauri --version
```

### Required Dependencies

The desktop app requires these key packages:

```json
{
  "@tauri-apps/api": "^2.x",
  "@tauri-apps/plugin-shell": "^2.x",
  "next": "16.x",
  "react": "^19.x"
}
```

---

## Build Commands

### Development Build

```bash
cd desktop
npm run tauri:dev
```

This launches the app in development mode with hot-reload enabled.

### Production Build (DMG)

```bash
cd desktop

# Clean previous builds (recommended)
rm -rf .next out src-tauri/target/release/bundle

# Build production DMG
npm run tauri:build
```

**Build Output:**
- **App Bundle**: `src-tauri/target/release/bundle/macos/shannon-desktop.app`
- **DMG Installer**: `src-tauri/target/release/bundle/dmg/shannon-desktop_0.1.0_aarch64.dmg`

**Expected Build Time:** ~45-90 seconds (M1/M2/M3 Mac)

---

## Troubleshooting

### Common Errors

#### 1. Module Not Found: `@tauri-apps/plugin-shell`

**Error:**
```
Module not found: Can't resolve '@tauri-apps/plugin-shell'
```

**Fix:**
```bash
npm install @tauri-apps/plugin-shell
```

---

#### 2. TypeScript Type Error in `run-detail/page.tsx`

**Error:**
```
Type error: This comparison appears to be unintentional because the types
'"idle" | "failed"' and '"error"' have no overlap.
```

**Root Cause:** `runStatus` type is `"idle" | "running" | "completed" | "failed"`, but code checks for `"error"` which doesn't exist.

**Fix:** Remove the invalid `"error"` check:

```typescript
// ❌ BEFORE (incorrect):
runStatus === "error" || runStatus === "failed"

// ✅ AFTER (correct):
runStatus === "failed"
```

**File Location:** `app/run-detail/page.tsx:1307`

**Valid Status Values:**
- `"idle"` - No task running
- `"running"` - Task in progress
- `"completed"` - Task finished successfully
- `"failed"` - Task encountered an error

---

#### 3. Stale Build Cache

**Symptoms:**
- Old code still appears in built app
- Build succeeds but changes not reflected

**Fix:**
```bash
# Full clean rebuild
cd desktop
rm -rf .next out src-tauri/target/release/bundle node_modules/.cache
npm run tauri:build
```

---

#### 4. Rust Compilation Errors

**Error:**
```
error: could not compile `app`
```

**Fix:**
```bash
# Update Rust toolchain
rustup update stable

# Clean Rust build cache
cd src-tauri
cargo clean
cd ..
npm run tauri:build
```

---

## Distribution

### File Verification

After building, verify the DMG:

```bash
cd desktop/src-tauri/target/release/bundle/dmg

# Check file size and timestamp
ls -lh shannon-desktop_0.1.0_aarch64.dmg

# Generate SHA256 checksum
shasum -a 256 shannon-desktop_0.1.0_aarch64.dmg
```

**Expected Output:**
```
-rw-r--r--  1 user  staff   3.8M Nov 28 14:14 shannon-desktop_0.1.0_aarch64.dmg
719073145c456d0e9a6940e78ecea23e5b755def58f29b928f6e15ac95683436
```

---

### Distribution Checklist

Before distributing the DMG:

- [ ] **Test the DMG locally**:
  ```bash
  open shannon-desktop_0.1.0_aarch64.dmg
  ```

- [ ] **Verify app launches**: Drag to Applications and open

- [ ] **Check core functionality**:
  - API connection works
  - Session list loads
  - Task submission works
  - Real-time updates stream correctly

- [ ] **Document the checksum**: Save SHA256 for integrity verification

- [ ] **Test on clean Mac**: Install on a machine without dev tools

---

### User Installation Instructions

Provide these instructions to end users:

1. **Download** `shannon-desktop_0.1.0_aarch64.dmg`
2. **Verify integrity** (optional but recommended):
   ```bash
   shasum -a 256 shannon-desktop_0.1.0_aarch64.dmg
   # Compare with published checksum
   ```
3. **Install**:
   - Double-click the DMG file
   - Drag "Shannon Desktop" to Applications folder
   - Eject the DMG
4. **Launch**:
   - Open from Applications folder
   - On first launch, right-click → Open (to bypass Gatekeeper)
   - Configure API endpoint in settings

---

## Verification

### Pre-Distribution Tests

```bash
# 1. Verify DMG integrity
cd desktop/src-tauri/target/release/bundle/dmg
shasum -a 256 shannon-desktop_0.1.0_aarch64.dmg

# 2. Mount DMG and check contents
hdiutil attach shannon-desktop_0.1.0_aarch64.dmg
ls -la /Volumes/shannon-desktop/

# 3. Check app signature (if signed)
codesign -dvv /Volumes/shannon-desktop/shannon-desktop.app

# 4. Unmount
hdiutil detach /Volumes/shannon-desktop
```

---

### Version Tracking

Track each distribution build:

| Build Date | SHA256 Checksum | Notes |
|------------|-----------------|-------|
| 2025-11-28 14:14 | `719073145c456d0e9a6940e78ecea23e5b755def58f29b928f6e15ac95683436` | Fixed TypeScript errors, added shell plugin |
| 2025-11-28 11:59 | `9579d8461f52cd64473da2c2f83bc9f868a4cd191140a8c72c46338f9e39d6d2` | Initial DMG build |

---

## Architecture

### Technology Stack

- **Framework**: Next.js 16.0.3 (React 19, Turbopack)
- **Desktop Runtime**: Tauri 2.x
- **UI Components**: Shadcn/ui (Radix primitives)
- **State Management**: Redux Toolkit
- **Styling**: Tailwind CSS
- **Backend API**: Shannon Gateway (Go)

### Build Process

```
┌─────────────────────────────────────────┐
│  1. Next.js Build (npm run build)      │
│     - Compile TypeScript to JavaScript │
│     - Generate static pages             │
│     - Optimize bundles (Turbopack)      │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│  2. Tauri Build (cargo build)           │
│     - Compile Rust backend              │
│     - Embed Next.js static export       │
│     - Create app bundle (.app)          │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│  3. DMG Creation (bundle_dmg.sh)        │
│     - Package .app into DMG             │
│     - Set installer window size         │
│     - Generate final distributable      │
└─────────────────────────────────────────┘
```

### Project Structure

```
desktop/
├── app/                    # Next.js pages
│   ├── page.tsx           # Home page
│   ├── runs/page.tsx      # Task list
│   ├── run-detail/        # Task details
│   └── agents/page.tsx    # Agent selector
├── components/            # React components
│   └── ui/               # Shadcn/ui components
├── lib/                   # Utilities
│   ├── features/         # Redux slices
│   ├── store.ts          # Redux store
│   └── utils.ts          # Helper functions
├── src-tauri/             # Tauri Rust backend
│   ├── src/
│   │   └── main.rs       # Rust entry point
│   ├── Cargo.toml        # Rust dependencies
│   └── tauri.conf.json   # Tauri configuration
├── package.json           # Node.js dependencies
└── next.config.mjs        # Next.js config
```

---

## Advanced Topics

### Code Signing (macOS)

For official distribution, sign the app:

```bash
# Find signing identity
security find-identity -v -p codesigning

# Sign the app
codesign --deep --force --verify --verbose \
  --sign "Developer ID Application: Your Name" \
  src-tauri/target/release/bundle/macos/shannon-desktop.app

# Verify signature
codesign -dvv src-tauri/target/release/bundle/macos/shannon-desktop.app
```

### Notarization (macOS)

For distribution outside the App Store:

```bash
# Create a DMG from signed app
# Submit for notarization
xcrun notarytool submit shannon-desktop_0.1.0_aarch64.dmg \
  --apple-id your@email.com \
  --team-id TEAMID \
  --password app-specific-password

# Staple notarization ticket
xcrun stapler staple shannon-desktop_0.1.0_aarch64.dmg
```

### Custom Build Scripts

Add to `package.json`:

```json
{
  "scripts": {
    "clean": "rm -rf .next out src-tauri/target/release/bundle",
    "build:clean": "npm run clean && npm run tauri:build",
    "verify": "cd src-tauri/target/release/bundle/dmg && shasum -a 256 *.dmg"
  }
}
```

---

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Build Desktop App

on:
  push:
    tags:
      - 'desktop-v*'

jobs:
  build-macos:
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v3

      - name: Setup Node.js
        uses: actions/setup-node@v3
        with:
          node-version: '20'

      - name: Setup Rust
        uses: dtolnay/rust-toolchain@stable

      - name: Install dependencies
        run: cd desktop && npm install

      - name: Build DMG
        run: cd desktop && npm run tauri:build

      - name: Generate checksum
        run: |
          cd desktop/src-tauri/target/release/bundle/dmg
          shasum -a 256 *.dmg > checksums.txt

      - name: Upload artifacts
        uses: actions/upload-artifact@v3
        with:
          name: shannon-desktop-dmg
          path: desktop/src-tauri/target/release/bundle/dmg/
```

---

## Support

For build issues:
- Check GitHub Issues: `https://github.com/Kocoro-lab/Shannon/issues`
- Tauri Docs: `https://tauri.app/v2/guides/`
- Next.js Docs: `https://nextjs.org/docs`

---

**Last Updated:** 2025-11-28
**Version:** 0.1.0
**Platform:** macOS Apple Silicon (aarch64)
