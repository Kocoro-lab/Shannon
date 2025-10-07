"""Example of session management for multi-turn conversations."""

import os
from shannon import ShannonClient

# Initialize client
client = ShannonClient(
    grpc_endpoint="localhost:50052",
    http_endpoint="http://localhost:8081",
    api_key=os.getenv("SHANNON_API_KEY", ""),
)

print("=" * 60)
print("Session Management Demo - Multi-turn Conversation")
print("=" * 60)
print()

# Create a new session
print("Creating new session...")
session = client.create_session(
    user_id="demo-user",
    initial_context={"project": "analytics-dashboard", "role": "developer"},
    max_history=50,
    ttl_seconds=3600,
)

print(f"✓ Session created!")
print(f"  Session ID: {session.session_id}")
print(f"  User ID: {session.user_id}")
print(f"  Max history: {session.max_history} messages")
print(f"  TTL: {session.ttl_seconds}s")
print()

# First turn - Establish context
print("-" * 60)
print("Turn 1: Establishing context")
print("-" * 60)

handle1 = client.submit_task(
    "My name is Alice and I'm working on a Python analytics dashboard. "
    "I need help optimizing database queries.",
    session_id=session.session_id,
    user_id="demo-user",
)

print(f"Task 1 ID: {handle1.task_id}")
result1 = handle1.result(timeout=60)
print(f"Response: {result1[:200]}..." if len(result1) > 200 else f"Response: {result1}")
print()

# Check session state
session_state = client.get_session(session.session_id, include_history=True)
print(f"Session state after turn 1:")
print(f"  Messages in history: {len(session_state.history)}")
print(f"  Total tokens used: {session_state.total_tokens_used}")
print(f"  Total cost: ${session_state.total_cost_usd:.4f}")
print()

# Second turn - Reference previous context
print("-" * 60)
print("Turn 2: Building on previous context")
print("-" * 60)

handle2 = client.submit_task(
    "What specific optimization techniques would work best for my use case?",
    session_id=session.session_id,
    user_id="demo-user",
)

print(f"Task 2 ID: {handle2.task_id}")
result2 = handle2.result(timeout=60)
print(f"Response: {result2[:200]}..." if len(result2) > 200 else f"Response: {result2}")
print()

# Third turn - Continue conversation
print("-" * 60)
print("Turn 3: Follow-up question")
print("-" * 60)

handle3 = client.submit_task(
    "Can you show me a code example for connection pooling?",
    session_id=session.session_id,
    user_id="demo-user",
)

print(f"Task 3 ID: {handle3.task_id}")
result3 = handle3.result(timeout=60)
print(f"Response: {result3[:200]}..." if len(result3) > 200 else f"Response: {result3}")
print()

# Update session context
print("-" * 60)
print("Updating session context...")
print("-" * 60)

client.update_session(
    session.session_id,
    context_updates={"implementation_status": "in_progress", "db_type": "postgresql"},
    extend_ttl_seconds=1800,  # Extend by 30 minutes
)

print("✓ Session context updated")
print()

# Get final session state
final_session = client.get_session(session.session_id, include_history=True)
print("Final session state:")
print(f"  Messages: {len(final_session.history)}")
print(f"  Total tokens: {final_session.total_tokens_used}")
print(f"  Total cost: ${final_session.total_cost_usd:.4f}")
print(f"  Context: {final_session.persistent_context}")
print()

# List user sessions
print("-" * 60)
print("Listing all sessions for user...")
print("-" * 60)

sessions = client.list_sessions(user_id="demo-user")
print(f"✓ Found {len(sessions)} session(s):")
for s in sessions:
    print(f"  - {s.session_id}: {s.message_count} messages, "
          f"{s.total_tokens_used} tokens, active={s.is_active}")
print()

# Get session summary (without full history)
print("-" * 60)
print("Getting session summary (lightweight)...")
print("-" * 60)

summary_session = client.get_session(session.session_id, include_history=False)
print(f"✓ Session summary (no history loaded):")
print(f"  Session ID: {summary_session.session_id}")
print(f"  Updated at: {summary_session.updated_at}")
print(f"  Tokens used: {summary_session.total_tokens_used}")
print()

# Delete session when done (optional)
print("-" * 60)
print("Cleaning up...")
print("-" * 60)

# Uncomment to delete session:
# client.delete_session(session.session_id)
# print(f"✓ Session {session.session_id} deleted")

print("✓ Session demo completed!")
print()
print("Note: Session will auto-expire after TTL (3600s from last activity)")

client.close()
