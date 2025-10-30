"""Example of using labels for workflow routing."""

import os
from shannon import ShannonClient, EventType

# Initialize client
client = ShannonClient(
    base_url="http://localhost:8080",
    api_key=os.getenv("SHANNON_API_KEY", ""),
)

print("=" * 60)
print("Example: Streaming selected events")
print("=" * 60)

handle = client.submit_task(
    "Analyze the performance metrics of a web application and summarize key bottlenecks.",
    session_id="analysis-session",
)

print(f"✓ Task submitted")
print(f"  Task ID: {handle.task_id}")
print(f"  Workflow ID: {handle.workflow_id}")
print()

# Stream to see agent delegation in action
print("Streaming events (delegation/team events if present will appear)...")
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
result = handle.result(timeout=60)
print(f"✓ Final result: {result}")

client.close()
print("\n✓ Examples completed!")
