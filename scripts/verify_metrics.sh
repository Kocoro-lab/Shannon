#!/bin/bash
# Verify metrics configuration and endpoints

set -e

echo "=== Shannon Metrics Verification ==="
echo

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if services are running
check_service() {
    local service=$1
    local port=$2
    
    if nc -z localhost $port 2>/dev/null; then
        echo -e "${GREEN}✓${NC} $service is running on port $port"
        return 0
    else
        echo -e "${RED}✗${NC} $service is not accessible on port $port"
        return 1
    fi
}

# Check metrics endpoint
check_metrics() {
    local service=$1
    local url=$2
    
    echo -e "\n${YELLOW}Testing $service metrics...${NC}"
    
    if response=$(curl -s -w "\n%{http_code}" $url 2>/dev/null); then
        http_code=$(echo "$response" | tail -n1)
        body=$(echo "$response" | head -n-1)
        
        if [ "$http_code" = "200" ]; then
            # Check for Prometheus format
            if echo "$body" | grep -q "^# HELP\|^# TYPE"; then
                echo -e "${GREEN}✓${NC} $service metrics endpoint working (Prometheus format)"
                echo "  Sample metrics:"
                echo "$body" | grep -E "^shannon_|^agent_core_" | head -3 | sed 's/^/    /'
            else
                echo -e "${YELLOW}⚠${NC} $service returns 200 but not Prometheus format"
            fi
        else
            echo -e "${RED}✗${NC} $service metrics returned HTTP $http_code"
        fi
    else
        echo -e "${RED}✗${NC} Cannot reach $service metrics endpoint"
    fi
}

# Check config file
echo -e "${YELLOW}Checking configuration...${NC}"
CONFIG_PATH="${CONFIG_PATH:-/app/config/features.yaml}"
if [ -f "$CONFIG_PATH" ]; then
    echo -e "${GREEN}✓${NC} Config file found at $CONFIG_PATH"
    if grep -q "observability:" "$CONFIG_PATH"; then
        echo "  Observability config:"
        grep -A5 "observability:" "$CONFIG_PATH" | sed 's/^/    /'
    fi
else
    # Check local config
    LOCAL_CONFIG="config/features.yaml"
    if [ -f "$LOCAL_CONFIG" ]; then
        echo -e "${YELLOW}⚠${NC} Config not at $CONFIG_PATH, but found at $LOCAL_CONFIG"
        if grep -q "observability:" "$LOCAL_CONFIG"; then
            echo "  Observability config:"
            grep -A5 "observability:" "$LOCAL_CONFIG" | sed 's/^/    /'
        fi
    else
        echo -e "${RED}✗${NC} Config file not found"
    fi
fi

echo -e "\n${YELLOW}Checking service availability...${NC}"

# Check Orchestrator
check_service "Orchestrator gRPC" 50052
check_service "Orchestrator Metrics" 2112

# Check Agent-Core
check_service "Agent-Core gRPC" 50051
check_service "Agent-Core Metrics" 2113

# Check LLM Service
check_service "LLM Service API" 8000

# Test metrics endpoints
check_metrics "Orchestrator" "http://localhost:2112/metrics"
check_metrics "Agent-Core" "http://localhost:2113"
check_metrics "LLM Service" "http://localhost:8000/metrics"

# Check for custom ports via env
echo -e "\n${YELLOW}Environment overrides:${NC}"
if [ ! -z "$ORCHESTRATOR_METRICS_PORT" ]; then
    echo -e "  Orchestrator metrics port override: $ORCHESTRATOR_METRICS_PORT"
    check_metrics "Orchestrator (custom)" "http://localhost:$ORCHESTRATOR_METRICS_PORT/metrics"
fi
if [ ! -z "$AGENT_CORE_METRICS_PORT" ]; then
    echo -e "  Agent-Core metrics port override: $AGENT_CORE_METRICS_PORT"
    check_metrics "Agent-Core (custom)" "http://localhost:$AGENT_CORE_METRICS_PORT"
fi

echo -e "\n${GREEN}=== Verification Complete ===${NC}"

# Test gRPC reflection
echo -e "\n${YELLOW}Bonus: Testing gRPC reflection...${NC}"
if command -v grpcurl &> /dev/null; then
    echo "Orchestrator services:"
    grpcurl -plaintext localhost:50052 list 2>/dev/null | head -5 || echo "  Failed to list"
    
    echo "Agent-Core services:"
    grpcurl -plaintext localhost:50051 list 2>/dev/null | head -5 || echo "  Failed to list"
else
    echo -e "${YELLOW}⚠${NC} grpcurl not installed, skipping reflection test"
fi