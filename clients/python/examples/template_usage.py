"""Example of using templates for structured task execution."""

import os
from shannon import ShannonClient, EventType

# Initialize client
client = ShannonClient(
    grpc_endpoint="localhost:50052",
    http_endpoint="http://localhost:8081",
    api_key=os.getenv("SHANNON_API_KEY", ""),
)

print("=" * 60)
print("Template Usage Examples")
print("=" * 60)
print()

# Example 1: Basic template usage
print("-" * 60)
print("Example 1: Using a template for structured queries")
print("-" * 60)

handle1 = client.submit_task(
    "user_query=What are the key findings from our Q3 user research?",
    user_id="researcher",
    template_name="research_summary",
    template_version="v1",
)

print(f"✓ Task submitted with template")
print(f"  Task ID: {handle1.task_id}")
print(f"  Template: research_summary (v1)")
print()

result1 = handle1.result(timeout=60)
print(f"Result: {result1[:200]}..." if len(result1) > 200 else f"Result: {result1}")
print()

# Example 2: Template-only mode (disable AI)
print("-" * 60)
print("Example 2: Template-only mode (no AI processing)")
print("-" * 60)

handle2 = client.submit_task(
    "user_id=12345&action=get_profile",
    user_id="api-service",
    template_name="user_profile_lookup",
    disable_ai=True,  # Skip LLM, just execute template
)

print(f"✓ Task submitted in template-only mode")
print(f"  Task ID: {handle2.task_id}")
print(f"  Template: user_profile_lookup")
print(f"  AI processing: Disabled")
print()

result2 = handle2.result(timeout=30)
print(f"Result: {result2}")
print()

# Example 3: Template with context
print("-" * 60)
print("Example 3: Template with additional context")
print("-" * 60)

handle3 = client.submit_task(
    "Calculate monthly recurring revenue",
    user_id="finance-team",
    template_name="financial_analysis",
    context={
        "currency": "USD",
        "fiscal_year": 2024,
        "department": "sales",
        "include_projections": True,
    },
)

print(f"✓ Task submitted with template and context")
print(f"  Task ID: {handle3.task_id}")
print(f"  Template: financial_analysis")
print(f"  Context: currency=USD, fiscal_year=2024, department=sales")
print()

# Stream to see template execution
print("Streaming template execution...")
for event in client.stream(
    handle3.workflow_id,
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

    print(f"{prefix} {event.message[:100]}..." if len(event.message) > 100 else f"{prefix} {event.message}")

    if event.type == EventType.WORKFLOW_COMPLETED:
        break

print()

# Get final status
status3 = client.get_status(handle3.task_id, include_details=True)
print(f"✓ Template execution completed")
if status3.metrics:
    print(f"  Tokens used: {status3.metrics.tokens_used}")
    print(f"  Cost: ${status3.metrics.cost_usd:.4f}")
print()

# Example 4: Template versioning
print("-" * 60)
print("Example 4: Using specific template versions")
print("-" * 60)

# Use older template version for compatibility
handle4a = client.submit_task(
    "product_id=PROD-123",
    user_id="legacy-system",
    template_name="product_report",
    template_version="v1.0",  # Specific version for backward compatibility
)

print(f"✓ Task with template v1.0: {handle4a.task_id}")

# Use latest template version with new features
handle4b = client.submit_task(
    "product_id=PROD-456",
    user_id="modern-system",
    template_name="product_report",
    template_version="v2.0",  # Latest version with enhanced features
)

print(f"✓ Task with template v2.0: {handle4b.task_id}")
print()

print("Waiting for both tasks to complete...")
result4a = handle4a.result(timeout=60)
result4b = handle4b.result(timeout=60)

print(f"✓ v1.0 result: {result4a[:100]}..." if len(result4a) > 100 else f"✓ v1.0 result: {result4a}")
print(f"✓ v2.0 result: {result4b[:100]}..." if len(result4b) > 100 else f"✓ v2.0 result: {result4b}")
print()

# Example 5: Combining templates with labels
print("-" * 60)
print("Example 5: Templates with workflow routing")
print("-" * 60)

handle5 = client.submit_task(
    "Analyze market trends and generate strategic recommendations",
    user_id="strategy-team",
    template_name="market_analysis",
    labels={
        "workflow": "supervisor",  # Route to supervisor for complex analysis
        "priority": "high",
    },
)

print(f"✓ Task with template and supervisor workflow")
print(f"  Task ID: {handle5.task_id}")
print(f"  Template: market_analysis")
print(f"  Workflow: supervisor (via labels)")
print()

result5 = handle5.result(timeout=120)
print(f"Result: {result5[:200]}..." if len(result5) > 200 else f"Result: {result5}")

client.close()
print("\n✓ Template examples completed!")
print()
print("Templates provide structured, reusable task patterns that can:")
print("  • Enforce consistent output formats")
print("  • Pre-configure tool selections and parameters")
print("  • Support versioning for backward compatibility")
print("  • Enable template-only mode for deterministic execution")
print("  • Combine with workflow routing for complex scenarios")
