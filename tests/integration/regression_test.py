#!/usr/bin/env python3
"""Regression test for Rust/Go changes - verify existing features still work"""

import sys
import os

# Add Shannon SDK to path (relative to this test file)
sdk_path = os.path.join(os.path.dirname(__file__), '../../clients/python/src')
sys.path.insert(0, os.path.abspath(sdk_path))

from shannon import ShannonClient
import time
from datetime import datetime

client = ShannonClient(base_url="http://localhost:8080")

def test_simple_query():
    """Test 1: Simple math query"""
    print("=" * 70)
    print("Test 1: Simple Math Query (small tier)")
    print("=" * 70)

    handle = client.submit_task(
        "What is 42 + 17?",
        model_tier="small",
        mode="simple",
    )
    print(f"Task ID: {handle.task_id}")

    time.sleep(5)
    status = client.get_status(handle.task_id)

    result_ok = status.result and len(status.result) > 0 and "59" in status.result
    print(f"✅ Status: {status.status}" if status.status == "COMPLETED" else f"❌ Status: {status.status}")
    print(f"✅ Result: {status.result[:100]}" if result_ok else f"❌ Result: {status.result}")

    return result_ok

def test_complex_query():
    """Test 2: Complex query with longer response"""
    print("\n" + "=" * 70)
    print("Test 2: Complex Query with Long Response")
    print("=" * 70)

    handle = client.submit_task(
        "Explain the differences between TCP and UDP protocols in networking.",
        model_tier="small",
        mode="simple",
    )
    print(f"Task ID: {handle.task_id}")

    time.sleep(12)
    status = client.get_status(handle.task_id)

    result_ok = status.result and len(status.result) > 500
    print(f"✅ Status: {status.status}" if status.status == "COMPLETED" else f"❌ Status: {status.status}")
    print(f"✅ Result length: {len(status.result) if status.result else 0}" if result_ok else f"❌ Result length: {len(status.result) if status.result else 0}")
    print(f"Result preview: {status.result[:150] if status.result else 'EMPTY'}...")

    return result_ok

def test_session_continuity():
    """Test 3: Session continuity with multiple turns"""
    print("\n" + "=" * 70)
    print("Test 3: Session Continuity (3 turns)")
    print("=" * 70)

    session_id = f"regression-test-{int(datetime.now().timestamp())}"
    print(f"Session ID: {session_id}")

    # Turn 1
    print("\nTurn 1:")
    h1 = client.submit_task(
        "My favorite color is blue.",
        session_id=session_id,
        model_tier="small",
        mode="simple",
    )
    time.sleep(5)
    s1 = client.get_status(h1.task_id)
    print(f"  Result: {s1.result[:80] if s1.result else 'EMPTY'}...")

    # Turn 2 - should reference previous context
    print("\nTurn 2:")
    h2 = client.submit_task(
        "What color did I just tell you about?",
        session_id=session_id,
        model_tier="small",
        mode="simple",
    )
    time.sleep(10)  # Wait longer for result
    s2 = client.get_status(h2.task_id)
    print(f"  Result: {s2.result[:80] if s2.result else 'EMPTY'}...")

    # Check if context was maintained
    context_ok = s2.result and "blue" in s2.result.lower()
    print(f"\n✅ Context maintained (mentions 'blue')" if context_ok else f"❌ Context NOT maintained (result: {s2.result[:100] if s2.result else 'NONE'}...)")

    # Get session history
    try:
        history = client.get_session_history(session_id)
        print(f"✅ Session history: {len(history)} turns" if len(history) >= 2 else f"❌ Session history: {len(history)} turns")
        return context_ok and len(history) >= 2
    except Exception as e:
        print(f"❌ Failed to get session history: {e}")
        return False

def test_model_tiers():
    """Test 4: Different model tiers"""
    print("\n" + "=" * 70)
    print("Test 4: Model Tier Selection")
    print("=" * 70)

    results = []

    for tier in ["small", "medium"]:
        print(f"\nTesting tier: {tier}")
        handle = client.submit_task(
            f"Say 'Hello from {tier} tier'",
            model_tier=tier,
            mode="simple",
        )
        time.sleep(5)
        status = client.get_status(handle.task_id)

        tier_ok = status.result and len(status.result) > 0
        print(f"  ✅ {tier}: {status.result[:60]}" if tier_ok else f"  ❌ {tier}: EMPTY")
        results.append(tier_ok)

    return all(results)

def test_error_handling():
    """Test 5: Error handling (invalid session lookup)"""
    print("\n" + "=" * 70)
    print("Test 5: Error Handling")
    print("=" * 70)

    try:
        # Try to get non-existent session
        client.get_session("non-existent-session-id-12345")
        print("❌ Should have raised an error for non-existent session")
        return False
    except Exception as e:
        print(f"✅ Correctly raised error: {type(e).__name__}")
        return True

# Run all tests
print("\n" + "=" * 70)
print("REGRESSION TEST SUITE - Post Rust/Go Changes")
print("=" * 70)

results = {
    "Simple Query": test_simple_query(),
    "Complex Query": test_complex_query(),
    "Session Continuity": test_session_continuity(),
    "Model Tiers": test_model_tiers(),
    "Error Handling": test_error_handling(),
}

# Summary
print("\n" + "=" * 70)
print("TEST SUMMARY")
print("=" * 70)

for test_name, passed in results.items():
    status = "✅ PASS" if passed else "❌ FAIL"
    print(f"{status}: {test_name}")

total = len(results)
passed = sum(results.values())
print(f"\nTotal: {passed}/{total} tests passed")

if passed == total:
    print("\n🎉 All regression tests passed!")
else:
    print(f"\n⚠️  {total - passed} test(s) failed - review above for details")

client.close()
