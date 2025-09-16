#!/bin/bash
# Test gRPC reflection in services

set -e

echo "Testing gRPC reflection..."

# Test Orchestrator reflection
echo "Testing Orchestrator service reflection..."
grpcurl -plaintext localhost:50052 list || echo "Orchestrator reflection not available"

# Test Agent-Core reflection
echo "Testing Agent-Core service reflection..."
grpcurl -plaintext localhost:50051 list || echo "Agent-Core reflection not available"

# List specific service methods if reflection works
echo ""
echo "Listing Agent-Core methods..."
grpcurl -plaintext localhost:50051 describe shannon.agent.AgentService || echo "Could not describe AgentService"

echo ""
echo "Listing Orchestrator methods..."
grpcurl -plaintext localhost:50052 describe shannon.orchestrator.OrchestratorService || echo "Could not describe OrchestratorService"

echo ""
echo "Listing Session methods..."
grpcurl -plaintext localhost:50052 describe shannon.session.SessionService || echo "Could not describe SessionService"

echo "Reflection test complete!"