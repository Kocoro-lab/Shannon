"""Example of passing context with tasks and streaming events."""

import os
from shannon import ShannonClient, EventType

# Initialize client
client = ShannonClient(
    base_url="http://localhost:8080",
    api_key=os.getenv("SHANNON_API_KEY", ""),
)

print("=" * 60)
print("Context + Streaming Examples")
print("=" * 60)
print()

# Example 1: Simple task
print("-" * 60)
print("Example 1: Simple query")
print("-" * 60)
handle1 = client.submit_task("What are the key findings from our Q3 user research?")
print(f"✓ Task submitted: {handle1.task_id}")
print(handle1.result(timeout=60))

# Example 2: Task with additional context
print("-" * 60)
print("Example 2: Query with context")
print("-" * 60)
handle2 = client.submit_task(
    "Calculate monthly recurring revenue",
    context={
        "currency": "USD",
        "fiscal_year": 2024,
        "department": "sales",
        "include_projections": True,
    },
)
print(f"✓ Task submitted: {handle2.task_id}")

# Stream to see execution
print("Streaming execution events...")
for event in client.stream(
    handle2.workflow_id,
    types=[
        EventType.WORKFLOW_STARTED,
        EventType.TOOL_INVOKED,
        EventType.TOOL_OBSERVATION,
        EventType.LLM_OUTPUT,
        EventType.WORKFLOW_COMPLETED,
    ],
):
    prefix = (
        "🚀" if event.type == EventType.WORKFLOW_STARTED else
        "🔧" if event.type == EventType.TOOL_INVOKED else
        "📊" if event.type == EventType.TOOL_OBSERVATION else
        "💭" if event.type == EventType.LLM_OUTPUT else
        "🏁" if event.type == EventType.WORKFLOW_COMPLETED else
        "📡"
    )
    msg = event.message
    print(f"{prefix} {msg[:100]}..." if len(msg) > 100 else f"{prefix} {msg}")
    if event.type == EventType.WORKFLOW_COMPLETED:
        break

print()
print(handle2.result(timeout=60))

client.close()
print("\n✓ Examples completed!")
