"""Example of streaming events and handling approvals."""

import os
import re

from shannon import ShannonClient, EventType

# Initialize client
client = ShannonClient(
    grpc_endpoint="localhost:50052",
    http_endpoint="http://localhost:8081",
    api_key=os.getenv("SHANNON_API_KEY", ""),
)

# Submit a task requiring approval
print("Submitting task with approval requirement...")
handle = client.submit_task(
    "Analyze market data and execute a trade if conditions are favorable",
    user_id="trader-user",
    session_id="trading-session",
    require_approval=True,  # Will pause for approval before executing actions
)

print(f"✓ Task submitted:")
print(f"  Task ID: {handle.task_id}")
print(f"  Workflow ID: {handle.workflow_id}")
print()

# Stream events and handle approvals
print("Streaming events...")
print("-" * 60)

for event in client.stream(
    handle.workflow_id,
    types=[
        EventType.AGENT_STARTED,
        EventType.LLM_PARTIAL,
        EventType.TOOL_INVOKED,
        EventType.APPROVAL_REQUESTED,
        EventType.APPROVAL_DECISION,
        EventType.WORKFLOW_COMPLETED,
    ],
):
    # Display event
    prefix = "🤖" if event.type == EventType.AGENT_STARTED else \
             "💭" if event.type == EventType.LLM_PARTIAL else \
             "🔧" if event.type == EventType.TOOL_INVOKED else \
             "⏸️ " if event.type == EventType.APPROVAL_REQUESTED else \
             "✅" if event.type == EventType.APPROVAL_DECISION else \
             "🏁" if event.type == EventType.WORKFLOW_COMPLETED else "📡"

    print(f"{prefix} [{event.type}] {event.message}")

    # Handle approval requests
    if event.type == EventType.APPROVAL_REQUESTED:
        print()
        print("⏸️  APPROVAL REQUIRED")
        print("-" * 60)

        # Option A: Get pending approvals (recommended)
        pending = client.get_pending_approvals(
            user_id="trader-user",
            session_id="trading-session"
        )

        if pending:
            approval = pending[0]
            print(f"Approval ID: {approval.approval_id}")
            print(f"Workflow: {approval.workflow_id}")
            print(f"Request: {approval.message}")
            print(f"Time: {approval.requested_at}")

            # Simulate user decision
            print()
            decision = input("Approve? (y/n): ").strip().lower()

            if decision == 'y':
                success = client.approve(
                    approval_id=approval.approval_id,
                    workflow_id=approval.workflow_id,
                    run_id=handle.run_id,
                    approved=True,
                    feedback="Approved by user",
                    approved_by="trader-user"
                )
                print(f"✓ Approval submitted: {success}")
            else:
                success = client.approve(
                    approval_id=approval.approval_id,
                    workflow_id=approval.workflow_id,
                    run_id=handle.run_id,
                    approved=False,
                    feedback="Rejected by user - conditions not met",
                    approved_by="trader-user"
                )
                print(f"✗ Rejection submitted: {success}")

        else:
            # Option B: Parse approval ID from message (fallback)
            match = re.search(r'id=([a-f0-9-]{36})', event.message)
            if match:
                approval_id = match.group(1)
                print(f"Parsed approval ID: {approval_id}")

                # Auto-approve for demo
                success = client.approve(
                    approval_id=approval_id,
                    workflow_id=handle.workflow_id,
                    run_id=handle.run_id,
                    approved=True,
                    feedback="Auto-approved for demo"
                )
                print(f"✓ Auto-approved: {success}")

        print("-" * 60)
        print()

    # Exit on completion
    if event.type == EventType.WORKFLOW_COMPLETED:
        break

print()
print("-" * 60)
print("Stream complete. Getting final status...")

# Get final status
status = client.get_status(handle.task_id, include_details=True)
print(f"Final status: {status.status.value}")
if status.result:
    print(f"Result: {status.result}")
if status.metrics:
    print(f"Tokens used: {status.metrics.tokens_used}")
    print(f"Cost: ${status.metrics.cost_usd:.4f}")

client.close()
