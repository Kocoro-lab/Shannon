#!/bin/bash
# Generate test histories for v2 workflows

set -e

HISTORIES_DIR="tests/replay/histories"
mkdir -p "$HISTORIES_DIR"

echo "Generating test histories for v2 workflows..."

# Helper function to submit task and export history
generate_history() {
    local query="$1"
    local workflow_type="$2"
    local output_file="$3"

    echo "Submitting: $query (expecting $workflow_type)"

    # Submit task
    WORKFLOW_ID=$(./scripts/submit_task.sh "$query" | grep "Workflow ID:" | cut -d: -f2 | tr -d ' ')

    if [ -z "$WORKFLOW_ID" ]; then
        echo "Failed to get workflow ID"
        exit 1
    fi

    echo "Workflow ID: $WORKFLOW_ID"

    # Wait for completion
    echo "Waiting for workflow to complete..."
    sleep 10

    # Export history
    echo "Exporting history to $output_file"
    GO111MODULE=on go run ./tools/replay \
        -export \
        -workflow-id "$WORKFLOW_ID" \
        -history "$output_file"

    echo "Generated: $output_file"
}

# Generate DAG v2 histories
echo "=== Generating DAG v2 histories ==="

# Simple task (complexity < 0.3)
generate_history \
    "What is 2+2?" \
    "dag_v2" \
    "$HISTORIES_DIR/dag_v2_simple.json"

# Parallel agents task
generate_history \
    "Compare Python, Go, and Rust for building a web API. List pros and cons." \
    "dag_v2" \
    "$HISTORIES_DIR/dag_v2_parallel.json"

# Complex task with reflection (complexity > 0.7)
generate_history \
    "Design a distributed system for real-time collaborative editing with conflict resolution, considering CAP theorem tradeoffs and providing specific technology recommendations." \
    "dag_v2" \
    "$HISTORIES_DIR/dag_v2_reflection.json"

# Generate React v2 histories
echo "=== Generating React v2 histories ==="

# Basic React task
generate_history \
    "Step by step, calculate the compound interest on \$1000 at 5% annually for 3 years." \
    "react_v2" \
    "$HISTORIES_DIR/react_v2_basic.json"

# React with reflection (many iterations)
generate_history \
    "Debug why my recursive fibonacci function is slow and optimize it step by step." \
    "react_v2" \
    "$HISTORIES_DIR/react_v2_reflection.json"

echo "All test histories generated successfully!"
echo "Run 'make test-replay-v2' to test replay determinism"