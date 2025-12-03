# Shannon Desktop - iOS Build Guide

Complete guide for building and testing the Shannon Desktop iOS application using Tauri 2.x.

---

## Quick Start (TL;DR)

### For Simulator (No Account Required)

```bash
cd desktop
npm run tauri ios build -- --target aarch64-sim
open -a Simulator
# Drag ~/Library/Developer/Xcode/DerivedData/app-*/Build/Products/release-iphonesimulator/shannon-desktop.app to Simulator
```

### For Physical iPhone (Free Apple ID)

```bash
# 1. One-time setup: Configure Xcode
sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer
xcodebuild -downloadPlatform iOS
open desktop/src-tauri/gen/apple/app.xcodeproj
# In Xcode: Add Apple ID, enable "Automatically manage signing"

# 2. Build and install
cd desktop
npm run tauri ios build -- --target aarch64
xcrun devicectl list devices  # Get DEVICE_ID
xcrun devicectl device install app --device DEVICE_ID \
  ~/Library/Developer/Xcode/DerivedData/app-*/Build/Products/release-iphoneos/shannon-desktop.app

# 3. On iPhone: Settings → General → VPN & Device Management → Trust
```

### Update App Icons

```bash
cd desktop
npm run tauri icon src-tauri/icons/icon.png  # Generate all sizes
npm run tauri ios build -- --target aarch64  # Rebuild
# Reinstall (use command above)
```

---

## Table of Contents

1. [Quick Start](#quick-start-tldr)
2. [Prerequisites](#prerequisites)
3. [Build Commands](#build-commands)
4. [Running in Simulator](#running-in-simulator)
5. [Build Output](#build-output)
6. [Device Testing](#device-testing)
7. [Customizing App Icons](#customizing-app-icons)
8. [Troubleshooting](#troubleshooting)
9. [Distribution](#distribution)

---

## Prerequisites

### Required Software

- **macOS** with Xcode installed
- **Xcode Command Line Tools** (configured to use Xcode.app)
- **Node.js** 18+
- **Rust** 1.70+
- **iOS Simulator Runtime** (iOS 13.0+)
- **CocoaPods** (for iOS dependencies)

### Initial Setup

The iOS platform has already been initialized for this project. If starting fresh, you would run:

```bash
cd desktop
npm run tauri ios init
```

### Dependencies Installed

The following dependencies were installed during setup:
- CocoaPods (via Homebrew)
- libimobiledevice
- xcodegen
- Rust iOS targets:
  - `aarch64-apple-ios` (iOS devices)
  - `aarch64-apple-ios-sim` (Apple Silicon simulators)
  - `x86_64-apple-ios` (Intel simulators)

### Xcode Configuration

**IMPORTANT**: Xcode must be set as the active developer directory:

```bash
# Set Xcode as active developer directory
sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer

# Verify
xcode-select -p
# Should output: /Applications/Xcode.app/Contents/Developer
```

### iOS Simulator Runtime

Download iOS simulator runtime (done automatically):

```bash
# Download iOS platform
xcodebuild -downloadPlatform iOS

# Verify simulators are available
xcrun simctl list devices available
```

---

## Build Commands

### Simulator Build (No Developer Account Required)

```bash
cd desktop

# Build for Apple Silicon simulator (M1/M2/M3 Macs)
npm run tauri ios build -- --target aarch64-sim

# Build for Intel simulator (older Macs)
npm run tauri ios build -- --target x86_64-sim
```

**Build Time**: ~2-5 minutes (first build), ~30-60 seconds (subsequent builds)

### Device Build (Requires Apple Developer Account)

```bash
cd desktop

# Build for physical iOS devices
npm run tauri ios build -- --target aarch64
```

**Note**: Device builds require code signing with Apple Developer certificate.

### Development Build with Live Reload

```bash
cd desktop

# Run in simulator with hot reload
npm run tauri ios dev
```

---

## Running in Simulator

### Launch Specific Simulator

```bash
# List available simulators
xcrun simctl list devices available

# Example output:
# == Devices ==
# -- iOS 26.1 --
#     iPhone 17 Pro (A278825B-AA30-45E0-B72A-DAF01C792F3A) (Shutdown)
#     iPhone 17 Pro Max (62373CAA-7A9C-46C1-95F7-B1AFB2DBC8A3) (Shutdown)
#     iPad Pro 13-inch (M5) (2E2C47F1-F60F-4F7F-A9AA-DC9D7BF0D081) (Shutdown)
```

### Install and Run App

```bash
# Boot simulator (replace UDID with your simulator)
xcrun simctl boot A278825B-AA30-45E0-B72A-DAF01C792F3A

# Install app to simulator
xcrun simctl install booted /path/to/shannon-desktop.app

# Launch app
xcrun simctl launch booted com.kocoro.shannon
```

### Using Xcode Simulator App

Alternatively, open the simulator app and drag-and-drop the `.app` bundle:

```bash
# Open Simulator.app
open -a Simulator

# Drag and drop the app bundle from Finder to install
```

---

## Build Output

### Simulator Build

**Location:**
```
/Users/[username]/Library/Developer/Xcode/DerivedData/app-[hash]/Build/Products/release-iphonesimulator/shannon-desktop.app
```

**Structure:**
```
shannon-desktop.app/
├── _CodeSignature/          # Simulator code signature
├── assets/                  # Frontend static files
├── LaunchScreen.storyboardc/
├── AppIcon*.png            # App icons
├── Assets.car              # Asset catalog (251 KB)
├── Info.plist              # App metadata
├── PkgInfo
└── shannon-desktop         # Rust binary (5.1 MB)
```

**Total Size**: ~5.4 MB

### Device Build

**Location:**
```
desktop/src-tauri/target/aarch64-apple-ios/release/bundle/ios/shannon-desktop.ipa
```

**Note**: IPA files are created only for device builds with proper code signing.

---

## Device Testing

### Option 1: Free Apple ID (No $99/year fee)

You can test on your own devices using your **free Apple ID** (iCloud account):

**Limitations:**
- ⚠️ Apps expire after **7 days** (need to rebuild and reinstall)
- ⚠️ Limited to **3 devices**
- ❌ No TestFlight distribution
- ❌ No App Store publishing
- ✅ Full app functionality works on your devices

**Setup:**
1. Open Xcode project: `open src-tauri/gen/apple/app.xcodeproj`
2. In Xcode: Preferences → Accounts → Add your Apple ID
3. In project settings: Enable "Automatically manage signing"
4. Select your Apple ID as the team
5. Connect iPhone and select it as target
6. Build will automatically sign with your free certificate

### Option 2: Paid Apple Developer Account ($99/year)

**Benefits:**
- ✅ Apps never expire
- ✅ Unlimited devices
- ✅ TestFlight distribution
- ✅ App Store publishing

### Quick Install Method (Recommended)

**Bypass Xcode's Run button issues** - use command line directly:

```bash
# 1. Build for device
cd desktop
npm run tauri ios build -- --target aarch64

# 2. Connect iPhone via USB and trust the computer

# 3. List connected devices
xcrun devicectl list devices

# 4. Install directly to device (replace DEVICE_ID)
xcrun devicectl device install app --device DEVICE_ID \
  ~/Library/Developer/Xcode/DerivedData/app-*/Build/Products/release-iphoneos/shannon-desktop.app
```

### Trust Developer Certificate (First Time)

After installation, on your iPhone:
1. **Settings** → **General** → **VPN & Device Management**
2. Tap your name under "DEVELOPER APP"
3. Tap **"Trust [Your Name]"**
4. Tap **"Trust"** again to confirm
5. Launch the app from home screen

### Find Device ID

```bash
xcrun devicectl list devices

# Example output:
# Name            Identifier
# ZHUO's iPhone   E5210789-6264-57C9-B0F9-9C8060EDBD11
```

---

## Customizing App Icons

### Generate All iOS Icon Sizes

Tauri can automatically generate all required iOS icon sizes from a single source image:

```bash
cd desktop

# Generate from existing source icon (512×512 or larger)
npm run tauri icon src-tauri/icons/icon.png

# Or use a custom icon file
npm run tauri icon /path/to/your-icon.png
```

**Requirements:**
- Source image should be **512×512** or larger (1024×1024 recommended)
- PNG format
- Square aspect ratio

**Generated Icons:**
- AppIcon-20x20@2x.png (40×40) - Notifications
- AppIcon-29x29@2x.png (58×58) - Settings
- AppIcon-40x40@2x.png (80×80) - Spotlight
- AppIcon-60x60@2x.png (120×120) - App Icon (iPhone)
- AppIcon-60x60@3x.png (180×180) - App Icon (iPhone)
- AppIcon-76x76@2x.png (152×152) - App Icon (iPad)
- AppIcon-83.5x83.5@2x.png (167×167) - App Icon (iPad Pro)
- AppIcon-512@2x.png (1024×1024) - App Store

### Update App with New Icons

After generating icons, rebuild and reinstall:

```bash
# 1. Generate icons
npm run tauri icon src-tauri/icons/icon.png

# 2. Rebuild app
npm run tauri ios build -- --target aarch64

# 3. Reinstall on device
xcrun devicectl device install app --device DEVICE_ID \
  ~/Library/Developer/Xcode/DerivedData/app-*/Build/Products/release-iphoneos/shannon-desktop.app
```

**Note:** You may need to force-quit the app or restart your iPhone to see the new icon.

---

## Troubleshooting

### Error: "iOS platform not installed"

**Solution**: Download iOS simulator runtime:
```bash
xcodebuild -downloadPlatform iOS
```

### Error: "xcrun: error: unable to find utility 'simctl'"

**Solution**: Switch to Xcode developer directory:
```bash
sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer
```

### Error: "No code signing certificates found"

**Expected for simulator builds** - simulators don't require real code signing.

**For device builds**: Add your Apple Developer Team ID to `tauri.conf.json` or set `APPLE_DEVELOPMENT_TEAM` environment variable.

### Build Fails with "command failed to run"

**Solution**: Clean build and retry:
```bash
cd desktop

# Clean Xcode build
rm -rf src-tauri/gen/apple/app.xcodeproj
rm -rf ~/Library/Developer/Xcode/DerivedData/app-*

# Reinitialize iOS
npm run tauri ios init

# Rebuild
npm run tauri ios build -- --target aarch64-sim
```

### Simulator Shows Black Screen

**Solution**: Ensure Next.js build completed:
```bash
cd desktop
npm run build

# Then rebuild iOS app
npm run tauri ios build -- --target aarch64-sim
```

### CocoaPods Installation Errors

**Solution**: Install via Homebrew instead of gem:
```bash
brew install cocoapods
```

### Error: "Command PhaseScriptExecution failed with a nonzero exit code"

This error occurs when using Xcode's Run button (▶️) to build and install.

**Solution**: Use command line installation instead:
```bash
# Don't use Xcode's Run button - use command line instead:
npm run tauri ios build -- --target aarch64
xcrun devicectl device install app --device DEVICE_ID \
  ~/Library/Developer/Xcode/DerivedData/app-*/Build/Products/release-iphoneos/shannon-desktop.app
```

**Why this happens**: Xcode's build script execution conflicts with Tauri's build process. Command line build bypasses this issue.

### App Expires After 7 Days (Free Apple ID)

**Expected behavior** with free Apple ID. To continue using:

```bash
# Rebuild and reinstall (same commands as initial install)
cd desktop
npm run tauri ios build -- --target aarch64
xcrun devicectl device install app --device DEVICE_ID \
  ~/Library/Developer/Xcode/DerivedData/app-*/Build/Products/release-iphoneos/shannon-desktop.app
```

**Your app data is preserved** - only the app binary needs reinstallation.

---

## Distribution

### TestFlight (Beta Testing)

1. **Build signed IPA**:
   ```bash
   npm run tauri ios build -- --target aarch64
   ```

2. **Upload to App Store Connect**:
   - Use Xcode or Transporter app
   - Submit for TestFlight review

3. **Invite testers**:
   - Internal testers: Up to 100 users
   - External testers: Up to 10,000 users (requires Apple review)

### App Store Distribution

1. **Configure app in App Store Connect**
2. **Build release IPA** with distribution certificate
3. **Submit for review**
4. **Wait for approval** (typically 1-3 days)

### Ad Hoc Distribution (Without App Store)

1. **Register device UDIDs** in Apple Developer Portal
2. **Create ad hoc provisioning profile**
3. **Build with ad hoc profile**
4. **Distribute IPA** to registered devices

---

## Configuration Reference

### tauri.conf.json

```json
{
  "bundle": {
    "iOS": {
      "minimumSystemVersion": "13.0",
      "bundleVersion": "1",
      "developmentTeam": "YOUR_TEAM_ID"
    }
  }
}
```

### Supported Targets

| Target | Platform | Use Case |
|--------|----------|----------|
| `aarch64-sim` | Apple Silicon Simulator | M1/M2/M3 Macs |
| `x86_64-sim` | Intel Simulator | Intel Macs |
| `aarch64` | Physical Devices | iPhone, iPad (iOS 13+) |

---

## Build Sizes

| Component | Size |
|-----------|------|
| Rust Binary | 5.1 MB |
| Assets & Icons | 300 KB |
| **Total App** | **5.4 MB** |

---

## Comparison: Desktop vs iOS

| Feature | macOS DMG | iOS App |
|---------|-----------|---------|
| **Size** | 3.8 MB | 5.4 MB |
| **Platform** | macOS 10.15+ | iOS 13.0+ |
| **Distribution** | Direct download | App Store / TestFlight |
| **Code Signing** | Optional | Required for devices |
| **Developer Account** | Not required | Required ($99/year) |

---

## Next Steps

### Without Apple Developer Account

✅ Build for iOS Simulator
✅ Test app locally
✅ Develop and iterate
❌ Test on physical devices
❌ Distribute via App Store

### With Apple Developer Account

✅ All above
✅ Test on physical devices
✅ Distribute via TestFlight
✅ Publish to App Store

---

## Useful Commands

```bash
# Check Tauri iOS setup
npm run tauri info

# List iOS targets
rustup target list | grep ios

# Clean iOS build
rm -rf src-tauri/gen/apple

# Reinstall iOS dependencies
cd src-tauri && pod install

# View simulator logs
xcrun simctl spawn booted log stream --level=debug
```

---

## Resources

- **Tauri iOS Guide**: https://tauri.app/v2/guides/distribution/ios/
- **Apple Developer**: https://developer.apple.com
- **TestFlight**: https://developer.apple.com/testflight/
- **Xcode**: https://developer.apple.com/xcode/

---

**Last Updated**: 2025-11-30
**iOS Version**: 13.0+
**Tauri Version**: 2.9.2
**App Version**: 0.1.0
**Tested On**: iPhone 14 Pro, iOS 26.1 Simulator
