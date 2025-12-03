# Shannon Desktop - Windows Build Guide

Complete guide for building Windows installers (MSI, NSIS, and portable EXE) for the Shannon Desktop application.

---

## Table of Contents

1. [Build Options](#build-options)
2. [Native Windows Build](#native-windows-build)
3. [Cross-Compilation from macOS/Linux](#cross-compilation-from-macoslinux)
4. [Installer Types](#installer-types)
5. [Configuration](#configuration)
6. [Code Signing](#code-signing)
7. [Distribution](#distribution)
8. [Troubleshooting](#troubleshooting)

---

## Build Options

### Option 1: Native Windows Build (Recommended)
- **Pros**: Fastest, most reliable, produces all installer types
- **Cons**: Requires Windows machine or VM
- **Output**: `.msi`, `.exe` (NSIS), portable `.exe`

### Option 2: Cross-Compilation from macOS/Linux
- **Pros**: Can build from your current dev machine
- **Cons**: More complex setup, limited installer types
- **Output**: Portable `.exe` only (no MSI without Windows)

### Option 3: GitHub Actions CI/CD
- **Pros**: Automated, builds for all platforms
- **Cons**: Requires CI/CD setup
- **Output**: All installer types

---

## Native Windows Build

### Prerequisites

**Required Software:**
- **Windows 10/11** (64-bit)
- **Node.js** 18+ ([nodejs.org](https://nodejs.org))
- **Rust** ([rustup.rs](https://rustup.rs))
- **Visual Studio Build Tools** or **Visual Studio 2022**
  - Workload: "Desktop development with C++"
- **WebView2** Runtime (usually pre-installed on Windows 11)

### Step-by-Step Build

#### 1. Install Dependencies

```powershell
# Install Rust (via rustup)
# Download and run: https://rustup.rs

# Verify installations
node --version
npm --version
rustc --version
cargo --version

# Install WebView2 (if not present)
# Download: https://go.microsoft.com/fwlink/p/?LinkId=2124703
```

#### 2. Install Node Dependencies

```powershell
cd desktop
npm install
```

#### 3. Build Installers

```powershell
# Build all installer types (MSI + NSIS)
npm run tauri:build

# Build specific installer type
npm run tauri:build -- --bundles msi      # MSI only
npm run tauri:build -- --bundles nsis     # NSIS only
```

#### 4. Build Output

**Location:** `desktop\src-tauri\target\release\bundle\`

```
bundle\
├── msi\
│   └── shannon-desktop_0.1.0_x64_en-US.msi    (Windows Installer)
├── nsis\
│   └── shannon-desktop_0.1.0_x64-setup.exe    (NSIS Installer)
└── (portable exe in target\release\shannon-desktop.exe)
```

**File Sizes:**
- MSI: ~5-8 MB
- NSIS: ~5-8 MB
- Portable EXE: ~4-6 MB

---

## Cross-Compilation from macOS/Linux

Cross-compilation is possible but has limitations. Only portable `.exe` can be built without a Windows machine.

### macOS Cross-Compilation Setup

```bash
# Install Windows target
rustup target add x86_64-pc-windows-msvc

# Install Wine (for linking)
brew install wine-stable

# Install cargo-xwin (Windows cross-compilation)
cargo install cargo-xwin

# Install LLVM
brew install llvm
```

### Build Command

```bash
cd desktop

# Build Windows executable (portable only)
cargo xwin build --release --target x86_64-pc-windows-msvc

# Output: target/x86_64-pc-windows-msvc/release/shannon-desktop.exe
```

**⚠️ Limitations:**
- Only produces portable `.exe` (no installers)
- MSI and NSIS require Windows tooling
- WebView2 runtime must be installed separately by users

---

## Installer Types

### 1. MSI (Windows Installer)

**Best for:** Enterprise deployment, Group Policy, silent installs

**Features:**
- ✅ Official Windows installer format
- ✅ Silent install support: `msiexec /i shannon-desktop.msi /quiet`
- ✅ Rollback on installation failure
- ✅ Automatic uninstaller in Control Panel
- ✅ Per-machine or per-user installation

**Usage:**
```powershell
# Standard install
shannon-desktop_0.1.0_x64_en-US.msi

# Silent install
msiexec /i shannon-desktop_0.1.0_x64_en-US.msi /quiet /norestart

# Silent uninstall
msiexec /x shannon-desktop_0.1.0_x64_en-US.msi /quiet
```

---

### 2. NSIS (Nullsoft Scriptable Install System)

**Best for:** Consumer distribution, custom branding

**Features:**
- ✅ Smaller installer size
- ✅ Custom installer UI
- ✅ Desktop shortcut creation
- ✅ Start Menu integration
- ✅ Progress bar during installation

**Usage:**
```powershell
# Run installer
shannon-desktop_0.1.0_x64-setup.exe

# Silent install
shannon-desktop_0.1.0_x64-setup.exe /S

# Custom install directory
shannon-desktop_0.1.0_x64-setup.exe /D=C:\Custom\Path
```

---

### 3. Portable EXE

**Best for:** USB drives, no-install scenarios

**Features:**
- ✅ No installation required
- ✅ Single executable file
- ✅ Runs from any location
- ❌ No automatic updates
- ❌ No Start Menu integration

**Requirements:**
- WebView2 Runtime must be installed separately

**Usage:**
```powershell
# Just run the exe
.\shannon-desktop.exe
```

---

## Configuration

### Customize Installer Behavior

Edit `desktop/src-tauri/tauri.conf.json`:

```json
{
  "bundle": {
    "active": true,
    "targets": ["msi", "nsis"],  // Choose installer types
    "windows": {
      "certificateThumbprint": null,
      "digestAlgorithm": "sha256",
      "timestampUrl": "",
      "wix": {
        "language": "en-US",
        "template": "path/to/custom.wxs"  // Custom MSI template
      },
      "nsis": {
        "license": "LICENSE.txt",
        "installerIcon": "icons/icon.ico",
        "installMode": "currentUser",  // or "perMachine"
        "languages": ["en-US"],
        "displayLanguageSelector": false
      }
    }
  }
}
```

---

### WebView2 Bundling

**Option 1: Require users to install WebView2** (default, smaller installer)

```json
{
  "bundle": {
    "windows": {
      "webviewInstallMode": {
        "type": "downloadBootstrapper"
      }
    }
  }
}
```

**Option 2: Bundle WebView2** (larger installer, ~100MB extra)

```json
{
  "bundle": {
    "windows": {
      "webviewInstallMode": {
        "type": "embedBootstrapper"
      }
    }
  }
}
```

**Option 3: Use fixed runtime** (offline install, largest)

```json
{
  "bundle": {
    "windows": {
      "webviewInstallMode": {
        "type": "fixedRuntime",
        "path": "path/to/webview2/runtime"
      }
    }
  }
}
```

---

## Code Signing

### Why Sign Windows Apps?

- ✅ Prevents "Unknown Publisher" warnings
- ✅ Required for Microsoft Store
- ✅ Builds user trust
- ✅ Avoids SmartScreen warnings

### Obtain a Code Signing Certificate

**Option 1: Buy from Certificate Authority**
- DigiCert, Sectigo, SSL.com
- ~$100-400/year
- EV (Extended Validation) recommended for new publishers

**Option 2: Self-Signed Certificate (testing only)**

```powershell
# Create self-signed cert (testing only, will show warnings)
New-SelfSignedCertificate `
  -Type CodeSigning `
  -Subject "CN=YourCompany" `
  -CertStoreLocation "Cert:\CurrentUser\My"
```

### Sign the Installer

#### Using Visual Studio signtool

```powershell
# Sign MSI
signtool sign /f certificate.pfx /p password /t http://timestamp.digicert.com `
  shannon-desktop_0.1.0_x64_en-US.msi

# Sign NSIS installer
signtool sign /f certificate.pfx /p password /t http://timestamp.digicert.com `
  shannon-desktop_0.1.0_x64-setup.exe

# Verify signature
signtool verify /pa shannon-desktop_0.1.0_x64_en-US.msi
```

#### Automated Signing in Tauri

```json
{
  "bundle": {
    "windows": {
      "certificateThumbprint": "YOUR_CERT_THUMBPRINT",
      "timestampUrl": "http://timestamp.digicert.com",
      "signCommand": "signtool sign /f certificate.pfx /p password /t http://timestamp.digicert.com"
    }
  }
}
```

---

## Distribution

### Verification

```powershell
# Generate SHA256 checksum
CertUtil -hashfile shannon-desktop_0.1.0_x64_en-US.msi SHA256

# Verify signature
Get-AuthenticodeSignature shannon-desktop_0.1.0_x64_en-US.msi
```

### Distribution Checklist

- [ ] **Test on clean Windows VM**
  - Windows 10 21H2+
  - Windows 11

- [ ] **Verify installer types**
  - MSI installs and uninstalls cleanly
  - NSIS installer works
  - Portable EXE launches

- [ ] **Check SmartScreen**
  - Signed installers should not trigger warnings
  - Unsigned installers will show blue warning (expected)

- [ ] **Test silent installation**
  ```powershell
  msiexec /i shannon-desktop.msi /quiet /log install.log
  ```

- [ ] **Document system requirements**
  - Windows 10 version 1809 or later
  - WebView2 Runtime (auto-installed or bundled)
  - ~200MB disk space

---

### User Installation Instructions

**MSI Installer (Recommended):**
1. Download `shannon-desktop_0.1.0_x64_en-US.msi`
2. Double-click to run installer
3. Follow installation wizard
4. Launch from Start Menu

**NSIS Installer:**
1. Download `shannon-desktop_0.1.0_x64-setup.exe`
2. Run installer (may show SmartScreen warning if unsigned)
3. Click "More info" → "Run anyway" if unsigned
4. Complete installation
5. Launch from desktop shortcut or Start Menu

**Portable EXE:**
1. Download `shannon-desktop.exe`
2. Install WebView2 Runtime if not present
3. Run exe from any location

---

## Troubleshooting

### Build Errors

#### Error: "MSVC not found"

**Solution:**
```powershell
# Install Visual Studio Build Tools
# https://visualstudio.microsoft.com/downloads/
# Select "Desktop development with C++" workload
```

#### Error: "WebView2 not found"

**Solution:**
```powershell
# Download and install WebView2 Runtime
# https://go.microsoft.com/fwlink/p/?LinkId=2124703
```

#### Error: "NSIS compiler not found"

**Solution:**
```powershell
# Install NSIS
choco install nsis

# Or download from https://nsis.sourceforge.io/
```

#### Error: "WiX Toolset not found"

**Solution:**
```powershell
# Install WiX Toolset for MSI builds
# https://wixtoolset.org/releases/

# Or via Chocolatey
choco install wixtoolset
```

---

### Runtime Errors

#### Error: "WebView2 Runtime is not installed"

**Solution for users:**
```powershell
# Download and install WebView2 Runtime
# https://go.microsoft.com/fwlink/p/?LinkId=2124703
```

**Solution for devs:** Bundle WebView2 in installer (see Configuration section)

---

#### SmartScreen Warning: "Windows protected your PC"

**For unsigned apps (expected):**
1. Click "More info"
2. Click "Run anyway"

**For signed apps:** Ensure certificate is from trusted CA, not expired

---

## GitHub Actions CI/CD

Automate Windows builds with GitHub Actions:

```yaml
# .github/workflows/build-windows.yml
name: Build Windows

on:
  push:
    tags:
      - 'v*'

jobs:
  build-windows:
    runs-on: windows-latest

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

      - name: Build Windows installers
        run: cd desktop && npm run tauri:build

      - name: Generate checksums
        run: |
          cd desktop/src-tauri/target/release/bundle/msi
          CertUtil -hashfile shannon-desktop_0.1.0_x64_en-US.msi SHA256 > checksum-msi.txt
          cd ../nsis
          CertUtil -hashfile shannon-desktop_0.1.0_x64-setup.exe SHA256 > checksum-nsis.txt

      - name: Upload MSI
        uses: actions/upload-artifact@v3
        with:
          name: shannon-desktop-msi
          path: desktop/src-tauri/target/release/bundle/msi/

      - name: Upload NSIS
        uses: actions/upload-artifact@v3
        with:
          name: shannon-desktop-nsis
          path: desktop/src-tauri/target/release/bundle/nsis/
```

---

## System Requirements

### Minimum Requirements

- **OS:** Windows 10 (version 1809 or later)
- **RAM:** 4GB
- **Disk:** 200MB free space
- **Runtime:** WebView2 Runtime
- **Architecture:** x86_64 (64-bit)

### Recommended Requirements

- **OS:** Windows 11
- **RAM:** 8GB+
- **Disk:** 500MB free space
- **Network:** Broadband for API calls

---

## Version Tracking

| Build Date | Installer Type | SHA256 Checksum | Notes |
|------------|----------------|-----------------|-------|
| TBD        | MSI           | TBD             | Initial Windows release |
| TBD        | NSIS          | TBD             | Initial Windows release |

---

## Additional Resources

- **Tauri Windows Guide:** https://tauri.app/v2/guides/distribution/windows/
- **WiX Toolset Docs:** https://wixtoolset.org/documentation/
- **NSIS Documentation:** https://nsis.sourceforge.io/Docs/
- **Code Signing Guide:** https://docs.microsoft.com/en-us/windows/win32/seccrypto/cryptography-tools

---

**Last Updated:** 2025-11-28
**Platform:** Windows 10/11 (x86_64)
**Version:** 0.1.0
