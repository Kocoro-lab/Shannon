"""Example of using labels for workflow routing."""

import os
from shannon import ShannonClient, EventType

# Initialize client
client = ShannonClient(
    grpc_endpoint="localhost:50052",
    http_endpoint="http://localhost:8081",
    api_key=os.getenv("SHANNON_API_KEY", ""),
)

print("=" * 60)
print("Example 1: Route to Supervisor Workflow")
print("=" * 60)

# Use labels to route to supervisor workflow for complex multi-step tasks
handle = client.submit_task(
    "First analyze the performance metrics of a web application, then identify bottlenecks, "
    "and finally create an optimization report with specific recommendations",
    user_id="analyst-user",
    session_id="analysis-session",
    labels={"workflow": "supervisor"},  # Route to supervisor workflow
)

print(f"✓ Task submitted with supervisor workflow routing")
print(f"  Task ID: {handle.task_id}")
print(f"  Workflow ID: {handle.workflow_id}")
print()

# Stream to see agent delegation in action
print("Streaming events (watching for delegation and team recruitment)...")
print("-" * 60)

for event in client.stream(
    handle.workflow_id,
    types=[
        EventType.WORKFLOW_STARTED,
        EventType.DELEGATION,
        EventType.TEAM_RECRUITED,
        EventType.AGENT_STARTED,
        EventType.AGENT_COMPLETED,
        EventType.WORKFLOW_COMPLETED,
    ],
):
    prefix = (
        "🚀" if event.type == EventType.WORKFLOW_STARTED else
        "👥" if event.type == EventType.DELEGATION else
        "🎯" if event.type == EventType.TEAM_RECRUITED else
        "🤖" if event.type == EventType.AGENT_STARTED else
        "✅" if event.type == EventType.AGENT_COMPLETED else
        "🏁" if event.type == EventType.WORKFLOW_COMPLETED else
        "📡"
    )

    print(f"{prefix} [{event.type}] {event.message}")

    if event.type == EventType.WORKFLOW_COMPLETED:
        break

print("-" * 60)
print()

# Get final result
status = client.get_status(handle.task_id, include_details=True)
print(f"✓ Supervisor workflow completed")
print(f"  Status: {status.status.value}")
if status.metrics:
    print(f"  Tokens used: {status.metrics.tokens_used}")
    print(f"  Agent tasks: {len(status.agent_statuses)}")
print()

print("=" * 60)
print("Example 2: Custom Labels for Task Categorization")
print("=" * 60)

# Use custom labels for task categorization and routing
handle2 = client.submit_task(
    "Calculate the ROI for our Q3 marketing campaign",
    user_id="finance-team",
    labels={
        "department": "finance",
        "priority": "high",
        "category": "analytics",
    },
)

print(f"✓ Task submitted with custom labels")
print(f"  Task ID: {handle2.task_id}")
print(f"  Labels: department=finance, priority=high, category=analytics")
print()

# Wait for completion
result = handle2.result(timeout=60)
print(f"✓ Task completed: {result}")
print()

print("=" * 60)
print("Example 3: Template with Labels")
print("=" * 60)

# Combine templates with custom labels
handle3 = client.submit_task(
    "user_query=What are the key findings from our user research?",
    user_id="research-team",
    template_name="research_summary",
    labels={
        "workflow": "simple",  # Override to use simple workflow
        "team": "product",
    },
)

print(f"✓ Task submitted with template and labels")
print(f"  Task ID: {handle3.task_id}")
print(f"  Template: research_summary")
print(f"  Workflow override: simple")
print()

# Get result
result3 = handle3.result(timeout=60)
print(f"✓ Result: {result3}")

client.close()
print("\n✓ Examples completed!")
