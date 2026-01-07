# Tauri Desktop App Build Guide

This guide explains how to build the Shannon desktop app to work with your local Docker Compose services.

## Prerequisites

### 1. System Requirements

**macOS:**
```bash
# Install Xcode Command Line Tools
xcode-select --install

# Install Rust
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```

**Linux:**
```bash
# Ubuntu/Debian
sudo apt update
sudo apt install libwebkit2gtk-4.1-dev \
  build-essential \
  curl \
  wget \
  file \
  libxdo-dev \
  libssl-dev \
  libayatana-appindicator3-dev \
  librsvg2-dev

# Install Rust
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```

**Windows:**
```powershell
# Install Microsoft C++ Build Tools
# Download from: https://visualstudio.microsoft.com/visual-cpp-build-tools/

# Install Rust
# Download from: https://rustup.rs/
```

### 2. Node.js and Dependencies

```bash
# Ensure you have Node.js 20+
node --version

# Install dependencies
cd desktop
npm install
```

### 3. Backend Services Running

```bash
# Start Docker Compose services
cd ..
make dev

# Verify services are running
make ps

# Check Gateway is accessible
curl http://localhost:8080/health
```

## Configuration for Local Docker Compose

### Environment Setup

The desktop app needs to know where to find your backend services. There are two environment files:

1. **`.env.local`** - Used during development (`npm run dev`)
2. **`.env.production`** - Used during build (`npm run build`)

Both should point to `localhost:8080` to connect to Docker Compose services.

**Verify `.env.production` exists:**

```bash
cat .env.production
```

Should contain:
```bash
NEXT_PUBLIC_API_URL=http://localhost:8080
NEXT_PUBLIC_USER_ID=user_01h0000000000000000000000
NEXT_PUBLIC_DEBUG=false
```

If it doesn't exist, copy from `.env.local`:
```bash
cp .env.local .env.production
```

## Building the Tauri App

### Quick Build (Current Platform)

```bash
# From the desktop directory
npm run tauri:build
```

This will:
1. Build the Next.js frontend (using `.env.production`)
2. Compile the Rust backend
3. Create a native app bundle

### Build Output Locations

**macOS:**
```bash
# Universal binary (Intel + Apple Silicon)
src-tauri/target/universal-apple-darwin/release/bundle/dmg/Shannon_0.1.0_universal.dmg
src-tauri/target/universal-apple-darwin/release/bundle/macos/Shannon.app

# Intel only
src-tauri/target/x86_64-apple-darwin/release/bundle/dmg/
src-tauri/target/x86_64-apple-darwin/release/bundle/macos/

# Apple Silicon only
src-tauri/target/aarch64-apple-darwin/release/bundle/dmg/
src-tauri/target/aarch64-apple-darwin/release/bundle/macos/
```

**Linux:**
```bash
src-tauri/target/release/bundle/appimage/shannon_0.1.0_amd64.AppImage
src-tauri/target/release/bundle/deb/shannon_0.1.0_amd64.deb
```

**Windows:**
```bash
src-tauri/target/release/bundle/msi/Shannon_0.1.0_x64_en-US.msi
src-tauri/target/release/bundle/nsis/Shannon_0.1.0_x64-setup.exe
```

### Platform-Specific Builds

**macOS Universal Binary (Recommended):**
```bash
npm run tauri:build -- --target universal-apple-darwin
```

**macOS Intel:**
```bash
npm run tauri:build -- --target x86_64-apple-darwin
```

**macOS Apple Silicon:**
```bash
npm run tauri:build -- --target aarch64-apple-darwin
```

**Linux:**
```bash
npm run tauri:build -- --target x86_64-unknown-linux-gnu
```

**Windows:**
```bash
npm run tauri:build -- --target x86_64-pc-windows-msvc
```

## Using the Built App

### Installation

**macOS:**
1. Open the `.dmg` file from the build output
2. Drag `Shannon.app` to Applications folder
3. Or use the `.app` directly from the bundle folder

**Linux:**
```bash
# AppImage (no installation needed)
chmod +x shannon_0.1.0_amd64.AppImage
./shannon_0.1.0_amd64.AppImage

# Or install .deb package
sudo dpkg -i shannon_0.1.0_amd64.deb
```

**Windows:**
- Run the `.msi` installer
- Or run the `.exe` NSIS installer

### Running the App

1. **Start Docker Compose services first:**
   ```bash
   cd /path/to/Shannon
   make dev
   ```

2. **Launch the Shannon app:**
   - macOS: Open from Applications or Launchpad
   - Linux: Run from application menu or terminal
   - Windows: Run from Start Menu

3. **The app will connect to:**
   - Gateway: `http://localhost:8080`
   - All backend services through the Gateway

### Verifying Connection

1. Open the Shannon app
2. Check the browser console (if debug mode is enabled)
3. Try submitting a task
4. Check backend logs: `make logs`

## Development vs Production Build

### Development Mode (`npm run tauri:dev`)

- Uses `.env.local`
- Hot reload enabled
- Faster iteration
- Opens dev tools automatically
- Connects to `http://localhost:8080`

```bash
# Start backend
cd .. && make dev

# Start Tauri dev mode
cd desktop
npm run tauri:dev
```

### Production Build (`npm run tauri:build`)

- Uses `.env.production`
- Optimized bundle
- No dev tools
- Native performance
- Still connects to `http://localhost:8080`

```bash
# Build the app
npm run tauri:build

# Backend must be running when you use the app
cd .. && make dev
```

## Configuration Options

### Changing Backend URL

If your Docker Compose services run on a different port or host:

1. **Update `.env.production`:**
   ```bash
   NEXT_PUBLIC_API_URL=http://localhost:8081  # Changed port
   # or
   NEXT_PUBLIC_API_URL=http://192.168.1.100:8080  # Different host
   ```

2. **Rebuild the app:**
   ```bash
   npm run tauri:build
   ```

### Authentication Modes

**Development Mode (No Auth):**
```bash
# In .env.production
NEXT_PUBLIC_USER_ID=user_01h0000000000000000000000
```

**API Key Mode:**
```bash
# In .env.production
NEXT_PUBLIC_API_KEY=sk_test_123456
# Remove or comment out NEXT_PUBLIC_USER_ID
```

**JWT Mode:**
- Remove both `NEXT_PUBLIC_USER_ID` and `NEXT_PUBLIC_API_KEY`
- Users will need to log in through the app

### Debug Mode

Enable debug logging in the built app:

```bash
# In .env.production
NEXT_PUBLIC_DEBUG=true
```

Then rebuild the app.

## Troubleshooting

### Build Fails

**Clear build cache:**
```bash
# Clear Next.js cache
rm -rf .next out

# Clear Rust cache
cd src-tauri
cargo clean
cd ..

# Rebuild
npm run tauri:build
```

**Update Rust:**
```bash
rustup update
```

**Reinstall dependencies:**
```bash
rm -rf node_modules package-lock.json
npm install
```

### App Can't Connect to Backend

**Check backend is running:**
```bash
cd ..
make ps
curl http://localhost:8080/health
```

**Verify environment variables were baked in:**
```bash
# The app uses the values from .env.production at build time
cat .env.production
```

**Check firewall:**
- Ensure localhost connections are allowed
- Check if port 8080 is accessible

**Try rebuilding:**
```bash
# Update .env.production
nano .env.production

# Rebuild
npm run tauri:build
```

### Port Conflicts

If port 8080 is in use:

1. **Change Gateway port in Docker Compose:**
   ```yaml
   # In deploy/compose/docker-compose.yml
   gateway:
     ports:
       - "8081:8080"  # Changed from 8080:8080
   ```

2. **Update .env.production:**
   ```bash
   NEXT_PUBLIC_API_URL=http://localhost:8081
   ```

3. **Restart backend and rebuild app:**
   ```bash
   cd .. && make down && make dev
   cd desktop && npm run tauri:build
   ```

### macOS Gatekeeper Issues

If macOS blocks the app:

```bash
# Remove quarantine attribute
xattr -cr /Applications/Shannon.app

# Or allow in System Preferences
# System Preferences > Security & Privacy > General > "Open Anyway"
```

### Linux AppImage Won't Run

```bash
# Make executable
chmod +x shannon_0.1.0_amd64.AppImage

# Install FUSE if needed
sudo apt install libfuse2
```

## Advanced Configuration

### Custom Tauri Config

Edit `src-tauri/tauri.conf.json` for advanced options:

```json
{
  "app": {
    "windows": [
      {
        "title": "Shannon",
        "width": 1400,
        "height": 900,
        "minWidth": 800,
        "minHeight": 600
      }
    ]
  }
}
```

### Bundle Identifier

For distribution, update the bundle identifier:

```json
{
  "identifier": "com.yourcompany.shannon"
}
```

### Icons

Replace icons in `src-tauri/icons/` with your custom icons.

## Distribution

### Code Signing (macOS)

```bash
# Sign the app
codesign --force --deep --sign "Developer ID Application: Your Name" \
  src-tauri/target/universal-apple-darwin/release/bundle/macos/Shannon.app

# Create notarized DMG
# See: https://tauri.app/v2/guides/distribution/sign-macos/
```

### Creating Installers

The build process automatically creates installers:
- **macOS**: `.dmg` file
- **Linux**: `.deb` and `.AppImage`
- **Windows**: `.msi` and `.exe`

## Quick Reference

```bash
# Development
npm run tauri:dev              # Dev mode with hot reload

# Building
npm run tauri:build            # Build for current platform
npm run tauri:build -- --target universal-apple-darwin  # macOS universal

# Cleaning
rm -rf .next out               # Clean Next.js
cd src-tauri && cargo clean    # Clean Rust

# Backend
cd .. && make dev              # Start services
cd .. && make ps               # Check status
cd .. && make logs             # View logs
```

## Related Documentation

- [Desktop README](README.md)
- [Configuration Guide](CONFIGURATION.md)
- [Troubleshooting Guide](TROUBLESHOOTING.md)
- [Tauri Documentation](https://tauri.app/v2/guides/)
- [Next.js Static Export](https://nextjs.org/docs/app/building-your-application/deploying/static-exports)

## Support

For issues:
1. Check logs: `make logs` (backend) and app console (frontend)
2. Verify `.env.production` configuration
3. Ensure Docker Compose services are running
4. Try rebuilding with clean cache
