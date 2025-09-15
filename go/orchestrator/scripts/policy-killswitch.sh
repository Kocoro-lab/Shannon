#!/bin/bash
# Shannon Policy Engine Kill-Switch Management Script
# This script provides emergency controls for the OPA policy engine

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_DIR="${SCRIPT_DIR}/../config"
COMPOSE_FILE="${SCRIPT_DIR}/../../../deploy/compose/compose.yml"

show_usage() {
    cat << EOF
Shannon Policy Engine Kill-Switch Management

Usage: $0 <command> [options]

Commands:
    status              Show current policy engine status
    enable-killswitch   Activate emergency kill-switch (force all requests to dry-run)
    disable-killswitch  Deactivate emergency kill-switch (return to canary mode)
    set-canary PERCENT  Set canary enforce percentage (0-100)
    add-enforce-user USER    Add user to enforce list
    remove-enforce-user USER Remove user from enforce list
    show-config         Display current policy configuration

Environment Variables:
    ORCHESTRATOR_HOST   Orchestrator hostname (default: localhost:50052)
    CONFIG_PATH         Path to config directory (default: ./config)

Examples:
    $0 status                    # Check current status
    $0 enable-killswitch         # EMERGENCY: Force all to dry-run
    $0 disable-killswitch        # Return to normal operation
    $0 set-canary 5              # Set 5% of requests to enforce mode
    $0 add-enforce-user wayland  # Add specific user to enforce list

EOF
}

# Configuration
ORCHESTRATOR_HOST="${ORCHESTRATOR_HOST:-localhost:50052}"
CONFIG_PATH="${CONFIG_PATH:-$CONFIG_DIR}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

check_prerequisites() {
    # Check if grpcurl is available
    if ! command -v grpcurl &> /dev/null; then
        log_error "grpcurl is required but not installed"
        log_info "Install with: brew install grpcurl"
        exit 1
    fi
    
    # Check if docker compose is available for service management
    if ! command -v docker &> /dev/null; then
        log_warn "docker not available - cannot restart services automatically"
    fi
}

get_policy_status() {
    log_info "Checking policy engine status..."
    
    # Try to get status from orchestrator
    if grpcurl -plaintext "$ORCHESTRATOR_HOST" list 2>/dev/null | grep -q "orchestrator.OrchestratorService"; then
        log_success "Orchestrator service is reachable at $ORCHESTRATOR_HOST"
        
        # Check health endpoint for policy status
        if curl -s "http://${ORCHESTRATOR_HOST%:*}:8081/health" 2>/dev/null | grep -q "healthy"; then
            log_success "Health endpoint is responding"
        else
            log_warn "Health endpoint not responding"
        fi
    else
        log_error "Cannot reach orchestrator service at $ORCHESTRATOR_HOST"
        log_info "Make sure the orchestrator service is running"
        return 1
    fi
}

enable_kill_switch() {
    log_warn "ACTIVATING EMERGENCY KILL-SWITCH"
    log_warn "This will force ALL policy requests to dry-run mode"
    
    # Update environment variable approach
    export SHANNON_POLICY_EMERGENCY_KILL_SWITCH=true
    
    # Write to environment file for persistence
    ENV_FILE="${SCRIPT_DIR}/../../../.env.policy"
    echo "# Policy Engine Emergency Kill-Switch - $(date)" > "$ENV_FILE"
    echo "SHANNON_POLICY_EMERGENCY_KILL_SWITCH=true" >> "$ENV_FILE"
    
    log_error "EMERGENCY KILL-SWITCH ACTIVATED"
    log_info "All policy requests will now be forced to dry-run mode"
    log_info "To restore normal operation, run: $0 disable-killswitch"
    
    # Restart orchestrator to pick up new configuration
    restart_orchestrator
}

disable_kill_switch() {
    log_info "Disabling emergency kill-switch..."
    
    # Remove from environment
    unset SHANNON_POLICY_EMERGENCY_KILL_SWITCH
    
    # Remove from environment file
    ENV_FILE="${SCRIPT_DIR}/../../../.env.policy"
    if [[ -f "$ENV_FILE" ]]; then
        grep -v "SHANNON_POLICY_EMERGENCY_KILL_SWITCH" "$ENV_FILE" > "${ENV_FILE}.tmp" || true
        mv "${ENV_FILE}.tmp" "$ENV_FILE"
    fi
    
    log_success "Emergency kill-switch disabled"
    log_info "Policy engine will return to canary mode"
    
    # Restart orchestrator to pick up new configuration
    restart_orchestrator
}

set_canary_percentage() {
    local percentage="$1"
    
    if [[ ! "$percentage" =~ ^[0-9]+$ ]] || [[ "$percentage" -lt 0 ]] || [[ "$percentage" -gt 100 ]]; then
        log_error "Invalid percentage: $percentage (must be 0-100)"
        exit 1
    fi
    
    log_info "Setting canary enforce percentage to $percentage%"
    
    # Update environment file
    ENV_FILE="${SCRIPT_DIR}/../../../.env.policy"
    
    # Remove existing percentage setting
    if [[ -f "$ENV_FILE" ]]; then
        grep -v "SHANNON_POLICY_CANARY_ENFORCE_PERCENTAGE" "$ENV_FILE" > "${ENV_FILE}.tmp" || true
        mv "${ENV_FILE}.tmp" "$ENV_FILE"
    fi
    
    # Add new setting
    echo "SHANNON_POLICY_CANARY_ENFORCE_PERCENTAGE=$percentage" >> "$ENV_FILE"
    
    log_success "Canary percentage set to $percentage%"
    log_info "Approximately $percentage% of requests will use enforce mode"
    log_info "Remaining $((100-percentage))% will use dry-run mode"
    
    restart_orchestrator
}

add_enforce_user() {
    local user="$1"
    
    if [[ -z "$user" ]]; then
        log_error "User ID cannot be empty"
        exit 1
    fi
    
    log_info "Adding user '$user' to enforce list"
    
    ENV_FILE="${SCRIPT_DIR}/../../../.env.policy"
    
    # Get current users
    current_users=""
    if [[ -f "$ENV_FILE" ]] && grep -q "SHANNON_POLICY_CANARY_ENFORCE_USERS" "$ENV_FILE"; then
        current_users=$(grep "SHANNON_POLICY_CANARY_ENFORCE_USERS" "$ENV_FILE" | cut -d'=' -f2)
    fi
    
    # Add new user if not already present
    if [[ "$current_users" == *"$user"* ]]; then
        log_warn "User '$user' is already in enforce list"
        return 0
    fi
    
    if [[ -n "$current_users" ]]; then
        new_users="$current_users,$user"
    else
        new_users="$user"
    fi
    
    # Update environment file
    if [[ -f "$ENV_FILE" ]]; then
        grep -v "SHANNON_POLICY_CANARY_ENFORCE_USERS" "$ENV_FILE" > "${ENV_FILE}.tmp" || true
        mv "${ENV_FILE}.tmp" "$ENV_FILE"
    fi
    
    echo "SHANNON_POLICY_CANARY_ENFORCE_USERS=$new_users" >> "$ENV_FILE"
    
    log_success "User '$user' added to enforce list"
    log_info "This user will always get enforce mode regardless of canary percentage"
    
    restart_orchestrator
}

remove_enforce_user() {
    local user="$1"
    
    if [[ -z "$user" ]]; then
        log_error "User ID cannot be empty"
        exit 1
    fi
    
    log_info "Removing user '$user' from enforce list"
    
    ENV_FILE="${SCRIPT_DIR}/../../../.env.policy"
    
    if [[ ! -f "$ENV_FILE" ]] || ! grep -q "SHANNON_POLICY_CANARY_ENFORCE_USERS" "$ENV_FILE"; then
        log_warn "No enforce users configured"
        return 0
    fi
    
    current_users=$(grep "SHANNON_POLICY_CANARY_ENFORCE_USERS" "$ENV_FILE" | cut -d'=' -f2)
    
    if [[ "$current_users" != *"$user"* ]]; then
        log_warn "User '$user' not found in enforce list"
        return 0
    fi
    
    # Remove user from list
    new_users=$(echo "$current_users" | sed "s/,$user//g" | sed "s/$user,//g" | sed "s/^$user$//g")
    
    # Update environment file
    grep -v "SHANNON_POLICY_CANARY_ENFORCE_USERS" "$ENV_FILE" > "${ENV_FILE}.tmp" || true
    mv "${ENV_FILE}.tmp" "$ENV_FILE"
    
    if [[ -n "$new_users" ]]; then
        echo "SHANNON_POLICY_CANARY_ENFORCE_USERS=$new_users" >> "$ENV_FILE"
    fi
    
    log_success "User '$user' removed from enforce list"
    
    restart_orchestrator
}

show_config() {
    log_info "Current Policy Configuration:"
    
    ENV_FILE="${SCRIPT_DIR}/../../../.env.policy"
    
    if [[ -f "$ENV_FILE" ]]; then
        echo -e "${BLUE}Environment Configuration:${NC}"
        cat "$ENV_FILE" | grep -E "SHANNON_POLICY_" || echo "No policy configuration found"
    else
        echo -e "${YELLOW}No policy environment file found${NC}"
    fi
    
    echo -e "\n${BLUE}Docker Environment:${NC}"
    if command -v docker &> /dev/null && docker compose -f "$COMPOSE_FILE" config | grep -A 5 -B 5 "SHANNON_POLICY" 2>/dev/null; then
        docker compose -f "$COMPOSE_FILE" config | grep -A 5 -B 5 "SHANNON_POLICY"
    else
        echo "Cannot read docker compose configuration"
    fi
}

restart_orchestrator() {
    if command -v docker &> /dev/null && [[ -f "$COMPOSE_FILE" ]]; then
        log_info "Restarting orchestrator to apply configuration changes..."
        
        # Add environment file to docker compose
        ENV_FILE="${SCRIPT_DIR}/../../../.env.policy"
        if [[ -f "$ENV_FILE" ]]; then
            export $(grep -v '^#' "$ENV_FILE" | xargs)
        fi
        
        # Restart orchestrator service
        if docker compose -f "$COMPOSE_FILE" restart orchestrator; then
            log_success "Orchestrator restarted successfully"
            sleep 3
            log_info "Waiting for service to be ready..."
            
            # Wait for service to be ready
            for i in {1..30}; do
                if get_policy_status >/dev/null 2>&1; then
                    log_success "Service is ready and responding"
                    break
                fi
                sleep 1
            done
        else
            log_error "Failed to restart orchestrator"
            log_info "Manual restart may be required"
        fi
    else
        log_warn "Docker not available - manual orchestrator restart required"
        log_info "Configuration changes will take effect after restart"
    fi
}

main() {
    if [[ $# -eq 0 ]]; then
        show_usage
        exit 1
    fi
    
    check_prerequisites
    
    case "$1" in
        status)
            get_policy_status
            ;;
        enable-killswitch)
            log_warn "Are you sure you want to activate the emergency kill-switch?"
            log_warn "This will force ALL requests to dry-run mode."
            read -p "Type 'yes' to confirm: " -r
            if [[ $REPLY == "yes" ]]; then
                enable_kill_switch
            else
                log_info "Kill-switch activation cancelled"
            fi
            ;;
        disable-killswitch)
            disable_kill_switch
            ;;
        set-canary)
            if [[ -z "$2" ]]; then
                log_error "Percentage required for set-canary command"
                show_usage
                exit 1
            fi
            set_canary_percentage "$2"
            ;;
        add-enforce-user)
            if [[ -z "$2" ]]; then
                log_error "User ID required for add-enforce-user command"
                show_usage
                exit 1
            fi
            add_enforce_user "$2"
            ;;
        remove-enforce-user)
            if [[ -z "$2" ]]; then
                log_error "User ID required for remove-enforce-user command"
                show_usage
                exit 1
            fi
            remove_enforce_user "$2"
            ;;
        show-config)
            show_config
            ;;
        *)
            log_error "Unknown command: $1"
            show_usage
            exit 1
            ;;
    esac
}

main "$@"