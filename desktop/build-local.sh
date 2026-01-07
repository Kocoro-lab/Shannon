#!/bin/bash
# Build Tauri app for local Docker Compose usage

set -e

echo "🔧 Building Planet Desktop App for Local Docker Compose"
echo "========================================================"
echo ""

# Check if we're in the desktop directory
if [ ! -f "package.json" ]; then
    echo "❌ Error: Please run this script from the desktop directory"
    exit 1
fi

# Check if .env.production exists
if [ ! -f ".env.production" ]; then
    echo "⚠️  .env.production not found, creating from .env.local..."
    if [ -f ".env.local" ]; then
        cp .env.local .env.production
        echo "✅ Created .env.production"
    else
        echo "❌ Error: Neither .env.production nor .env.local found"
        echo "Please create .env.production with:"
        echo "  NEXT_PUBLIC_API_URL=http://localhost:8080"
        exit 1
    fi
fi

# Display configuration
echo "📋 Build Configuration:"
echo "----------------------"
grep "NEXT_PUBLIC_API_URL" .env.production || echo "  NEXT_PUBLIC_API_URL not set"
grep "NEXT_PUBLIC_USER_ID" .env.production || echo "  NEXT_PUBLIC_USER_ID not set"
echo ""

# Check if backend is running
echo "🔍 Checking if backend is running..."
if curl -s http://localhost:8080/health > /dev/null 2>&1; then
    echo "✅ Backend is running on http://localhost:8080"
else
    echo "⚠️  Warning: Backend doesn't seem to be running"
    echo "   Start it with: cd .. && make dev"
    echo ""
    read -p "Continue anyway? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Check Rust installation
echo ""
echo "🦀 Checking Rust installation..."
if ! command -v rustc &> /dev/null; then
    echo "❌ Error: Rust is not installed"
    echo "Install from: https://rustup.rs/"
    exit 1
fi
echo "✅ Rust $(rustc --version)"

# Check Node.js
echo ""
echo "📦 Checking Node.js..."
if ! command -v node &> /dev/null; then
    echo "❌ Error: Node.js is not installed"
    exit 1
fi
echo "✅ Node.js $(node --version)"

# Install dependencies if needed
if [ ! -d "node_modules" ]; then
    echo ""
    echo "📥 Installing dependencies..."
    npm install
fi

# Clean previous builds
echo ""
echo "🧹 Cleaning previous builds..."
rm -rf .next out
cd src-tauri && cargo clean && cd ..
echo "✅ Clean complete"

# Build the app
echo ""
echo "🏗️  Building Tauri app..."
echo "This may take several minutes..."
echo ""

npm run tauri:build

# Check build output
echo ""
echo "✅ Build complete!"
echo ""
echo "📦 Build artifacts:"
echo "==================="

if [ "$(uname)" == "Darwin" ]; then
    # macOS
    if [ -d "src-tauri/target/universal-apple-darwin/release/bundle" ]; then
        echo "Universal Binary:"
        find src-tauri/target/universal-apple-darwin/release/bundle -name "*.dmg" -o -name "*.app" | head -5
    fi
    if [ -d "src-tauri/target/aarch64-apple-darwin/release/bundle" ]; then
        echo "Apple Silicon:"
        find src-tauri/target/aarch64-apple-darwin/release/bundle -name "*.dmg" -o -name "*.app" | head -5
    fi
    if [ -d "src-tauri/target/x86_64-apple-darwin/release/bundle" ]; then
        echo "Intel:"
        find src-tauri/target/x86_64-apple-darwin/release/bundle -name "*.dmg" -o -name "*.app" | head -5
    fi
elif [ "$(expr substr $(uname -s) 1 5)" == "Linux" ]; then
    # Linux
    find src-tauri/target/release/bundle -name "*.AppImage" -o -name "*.deb" | head -5
else
    # Windows
    find src-tauri/target/release/bundle -name "*.msi" -o -name "*.exe" | head -5
fi

echo ""
echo "🎉 Success!"
echo ""
echo "📝 Next steps:"
echo "1. Install the app from the build artifacts above"
echo "2. Make sure backend is running: cd .. && make dev"
echo "3. Launch the Planet app"
echo ""
echo "💡 The app will connect to: http://localhost:8080"
