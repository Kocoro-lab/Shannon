#!/bin/bash

# Shannon Desktop - Find Embedded API Port
# This script helps locate the actual port the embedded API is running on

set -e

SCRIPT_NAME="find-api-port.sh"
COLORS_ENABLED=true

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Functions for colored output
print_info() {
    if [ "$COLORS_ENABLED" = true ]; then
        echo -e "${BLUE}ℹ ${NC}$1"
    else
        echo "INFO: $1"
    fi
}

print_success() {
    if [ "$COLORS_ENABLED" = true ]; then
        echo -e "${GREEN}✅ ${NC}$1"
    else
        echo "SUCCESS: $1"
    fi
}

print_warning() {
    if [ "$COLORS_ENABLED" = true ]; then
        echo -e "${YELLOW}⚠️  ${NC}$1"
    else
        echo "WARNING: $1"
    fi
}

print_error() {
    if [ "$COLORS_ENABLED" = true ]; then
        echo -e "${RED}❌ ${NC}$1"
    else
        echo "ERROR: $1"
    fi
}

print_header() {
    if [ "$COLORS_ENABLED" = true ]; then
        echo -e "${CYAN}═══════════════════════════════════════════════════${NC}"
        echo -e "${CYAN}  $1${NC}"
        echo -e "${CYAN}═══════════════════════════════════════════════════${NC}"
    else
        echo "==================================================="
        echo "  $1"
        echo "==================================================="
    fi
}

# Check if process is running
check_process() {
    print_header "Checking for Shannon Desktop Process"

    if pgrep -f "shannon-desktop" > /dev/null; then
        PID=$(pgrep -f "shannon-desktop" | head -1)
        print_success "Shannon Desktop is running (PID: $PID)"
        return 0
    else
        print_warning "Shannon Desktop process not found"
        print_info "Start the app with: npm run tauri:dev"
        return 1
    fi
}

# Method 1: Check network connections
find_port_network() {
    print_header "Method 1: Network Connections"

    if command -v lsof &> /dev/null; then
        print_info "Searching for listening ports..."

        # Look for shannon-desktop process listening on TCP
        PORTS=$(lsof -iTCP -sTCP:LISTEN -n -P 2>/dev/null | grep shannon | awk '{print $9}' | cut -d: -f2 | sort -u)

        if [ -n "$PORTS" ]; then
            print_success "Found listening port(s):"
            for port in $PORTS; do
                echo "  → Port: $port"
                echo "  → URL:  http://127.0.0.1:$port"

                # Try to hit health endpoint
                if curl -s -m 2 "http://127.0.0.1:$port/health" > /dev/null 2>&1; then
                    print_success "Health check passed on port $port"
                    return 0
                fi
            done
            return 0
        else
            print_warning "No listening ports found for shannon-desktop"
            return 1
        fi
    else
        print_warning "lsof command not found"
        return 1
    fi
}

# Method 2: Check recent log files
find_port_logs() {
    print_header "Method 2: Log Files"

    LOG_LOCATIONS=(
        "tauri.log"
        "debug.log"
        "shannon.log"
        "$HOME/Library/Logs/ai.prometheusags.planet/shannon.log"
    )

    for log_file in "${LOG_LOCATIONS[@]}"; do
        if [ -f "$log_file" ]; then
            print_info "Checking $log_file..."

            # Look for "listening on" message
            PORT_LINE=$(grep -E "listening on 127\.0\.0\.1:|listening on.*:[0-9]+" "$log_file" 2>/dev/null | tail -1)

            if [ -n "$PORT_LINE" ]; then
                # Extract port number
                PORT=$(echo "$PORT_LINE" | grep -oE ':[0-9]+' | tail -1 | cut -d: -f2)
                if [ -n "$PORT" ]; then
                    print_success "Found in logs: Port $PORT"
                    echo "  → Log: $log_file"
                    echo "  → URL: http://127.0.0.1:$PORT"
                    return 0
                fi
            fi

            # Look for "Server ready event emitted"
            EVENT_LINE=$(grep "Server ready event emitted" "$log_file" 2>/dev/null | tail -1)
            if [ -n "$EVENT_LINE" ]; then
                PORT=$(echo "$EVENT_LINE" | grep -oE 'port: [0-9]+' | grep -oE '[0-9]+')
                if [ -n "$PORT" ]; then
                    print_success "Found in logs: Port $PORT"
                    echo "  → Log: $log_file"
                    echo "  → URL: http://127.0.0.1:$PORT"
                    return 0
                fi
            fi
        fi
    done

    print_warning "No port information found in log files"
    return 1
}

# Method 3: Scan common port range
scan_ports() {
    print_header "Method 3: Port Scanning"

    print_info "Scanning common dynamic port range (49152-49200)..."
    print_warning "This may take a moment..."

    for port in {49152..49200}; do
        # Show progress every 10 ports
        if [ $((port % 10)) -eq 0 ]; then
            echo -n "."
        fi

        # Try to connect to /health endpoint
        if curl -s -m 1 "http://localhost:$port/health" > /dev/null 2>&1; then
            echo ""
            print_success "Found API on port $port"
            echo "  → URL: http://127.0.0.1:$port"

            # Try to get version info
            VERSION=$(curl -s -m 1 "http://localhost:$port/health" 2>/dev/null)
            if [ -n "$VERSION" ]; then
                echo "  → Response: $VERSION"
            fi
            return 0
        fi
    done

    echo ""
    print_warning "No API found in scanned range"
    return 1
}

# Method 4: Check macOS Console logs
check_console_logs() {
    print_header "Method 4: macOS Console Logs"

    if [[ "$OSTYPE" == "darwin"* ]]; then
        print_info "Checking recent Console logs..."

        # Check logs from last 5 minutes
        LOG_OUTPUT=$(log show --predicate 'process == "shannon-desktop"' --last 5m 2>/dev/null | grep -E "listening on|Server ready event" | tail -5)

        if [ -n "$LOG_OUTPUT" ]; then
            print_success "Found in Console logs:"
            echo "$LOG_OUTPUT"

            # Extract port
            PORT=$(echo "$LOG_OUTPUT" | grep -oE ':[0-9]+' | tail -1 | cut -d: -f2)
            if [ -n "$PORT" ]; then
                echo ""
                print_success "Extracted port: $PORT"
                echo "  → URL: http://127.0.0.1:$PORT"
                return 0
            fi
        else
            print_warning "No relevant logs found in Console"
        fi
    else
        print_info "Skipping (macOS only)"
    fi

    return 1
}

# Main execution
main() {
    echo ""
    print_header "Shannon Desktop - Find Embedded API Port"
    echo ""

    # Check if app is running
    if ! check_process; then
        echo ""
        print_error "Shannon Desktop is not running"
        print_info "Please start the app first:"
        echo "  cd desktop"
        echo "  npm run tauri:dev"
        echo ""
        exit 1
    fi

    echo ""

    # Try each method in order
    FOUND=false

    if find_port_network; then
        FOUND=true
    fi

    echo ""

    if [ "$FOUND" = false ]; then
        if find_port_logs; then
            FOUND=true
        fi
    fi

    echo ""

    if [ "$FOUND" = false ]; then
        print_warning "Trying additional methods..."
        echo ""

        check_console_logs
        echo ""

        scan_ports
    fi

    echo ""

    # Final status
    if [ "$FOUND" = true ]; then
        print_header "Success!"
        print_success "Embedded API port located"
        echo ""
        print_info "Test the connection:"
        echo "  curl http://127.0.0.1:\$PORT/health"
        echo ""
    else
        print_header "Not Found"
        print_error "Could not locate the embedded API port"
        echo ""
        print_info "Troubleshooting steps:"
        echo "  1. Check if the app is fully started (wait 30-90 seconds)"
        echo "  2. Look for 'Server ready' messages in the app console"
        echo "  3. Run with verbose logging: RUST_LOG=debug npm run tauri:dev"
        echo "  4. Check logs manually:"
        echo "     grep 'listening on' tauri.log"
        echo ""
    fi
}

# Run main function
main
