"""Simple example of submitting a task and checking status."""

import os
import time

from shannon import ShannonClient, TaskStatusEnum

# Initialize client
client = ShannonClient(
    base_url="http://localhost:8080",
    api_key=os.getenv("SHANNON_API_KEY", ""),  # or use bearer_token
)

# Submit a simple task
print("Submitting task...")
handle = client.submit_task(
    "What is 15 + 25?",
    session_id="example-session",
)

print(f"✓ Task submitted!")
print(f"  Task ID: {handle.task_id}")
print(f"  Workflow ID: {handle.workflow_id}")
print()

# Poll for completion
print("Waiting for completion...")
status = client.wait(handle.task_id, timeout=60, poll_interval=2.0)

# Display result
print()
if status.status == TaskStatusEnum.COMPLETED:
    print("✓ Task completed successfully!")
    print(f"  Result: {status.result}")
    # Optional: fetch usage totals from task listings
    tasks, _ = client.list_tasks(limit=50)
    usage = next((t.total_token_usage for t in tasks if t.task_id == handle.task_id), None)
    if usage:
        print(f"  Tokens: total={usage.total_tokens}, prompt={usage.prompt_tokens}, completion={usage.completion_tokens}")
        print(f"  Cost: ${usage.cost_usd:.6f}")
elif status.status == TaskStatusEnum.FAILED:
    print("✗ Task failed!")
    print(f"  Error: {status.error_message}")
else:
    print(f"Task ended with status: {status.status.value}")

client.close()
