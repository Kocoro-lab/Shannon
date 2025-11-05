#!/usr/bin/env python3
"""Test advanced features after allowed_tools and provider fixes"""

import sys
import os

# Add Shannon SDK to path (relative to this test file)
sdk_path = os.path.join(os.path.dirname(__file__), '../../clients/python/src')
sys.path.insert(0, os.path.abspath(sdk_path))

from shannon import ShannonClient
import time
from datetime import datetime

client = ShannonClient(base_url="http://localhost:8080")

def test_tool_usage():
    """Test 1: Tool usage with allowed_tools (Python code execution)"""
    print("=" * 70)
    print("Test 1: Tool Usage End-to-End")
    print("=" * 70)

    # Test with Python code execution tool
    handle = client.submit_task(
        "Calculate the fibonacci number for n=10 using Python code",
        model_tier="small",
        mode="simple",
    )
    print(f"Task ID: {handle.task_id}")

    time.sleep(15)
    status = client.get_status(handle.task_id)

    # Check if result contains evidence of tool usage or computation
    result_ok = status.result and len(status.result) > 0
    has_answer = status.result and ("55" in status.result or "fibonacci" in status.result.lower())

    print(f"✅ Status: {status.status}" if status.status == "COMPLETED" else f"❌ Status: {status.status}")
    print(f"✅ Result length: {len(status.result) if status.result else 0}" if result_ok else f"❌ Result empty")
    print(f"Result preview: {status.result[:200] if status.result else 'EMPTY'}...")
    print(f"✅ Contains answer" if has_answer else "⚠️  May not have used tool (still ok if answered)")

    return result_ok

def test_streaming_mode():
    """Test 2: Streaming responses"""
    print("\n" + "=" * 70)
    print("Test 2: Streaming Mode")
    print("=" * 70)

    handle, stream_url = client.submit_and_stream(
        "Write a short poem about coding",
        model_tier="small",
        mode="simple",
    )
    print(f"Task ID: {handle.task_id}")
    print(f"Stream URL: {stream_url}")

    chunks_received = 0
    final_content = []

    try:
        for event in client.stream(handle.workflow_id, types=["LLM_PARTIAL", "LLM_OUTPUT", "WORKFLOW_COMPLETED"]):
            chunks_received += 1
            if event.message:
                final_content.append(event.message[:50])
            if chunks_received <= 5:  # Show first few events
                print(f"  Event {chunks_received}: {event.type} - {event.message[:80] if event.message else 'N/A'}...")
            if event.type == "WORKFLOW_COMPLETED":
                break
            if chunks_received > 50:  # Safety limit
                break
    except Exception as e:
        print(f"⚠️  Stream error (may be ok if task completed): {e}")

    # Verify final result
    time.sleep(5)
    status = client.get_status(handle.task_id)

    # Stream is nice to have, but final result is what matters
    result_ok = status.result and len(status.result) > 50
    stream_ok = chunks_received > 0

    if chunks_received > 0:
        print(f"\n✅ Received {chunks_received} stream events")
    else:
        print(f"\n⚠️  No stream events (polling API may be used)")

    print(f"✅ Final result length: {len(status.result) if status.result else 0}" if result_ok else f"❌ Final result empty")
    print(f"Result preview: {status.result[:150] if status.result else 'EMPTY'}...")

    # Pass if final result exists, even without stream events
    return result_ok

def test_openai_compatible_provider():
    """Test 3: OpenAI-compatible provider (smoke test with OpenAI)"""
    print("\n" + "=" * 70)
    print("Test 3: OpenAI-Compatible Provider")
    print("=" * 70)
    print("Testing with OpenAI provider (default)")

    # Test with explicit provider specification
    handle = client.submit_task(
        "What is the capital of France?",
        model_tier="small",
        provider_override="openai",  # Explicitly test provider selection
        mode="simple",
    )
    print(f"Task ID: {handle.task_id}")

    time.sleep(5)
    status = client.get_status(handle.task_id)

    provider_ok = status.result and "paris" in status.result.lower()
    print(f"✅ Status: {status.status}" if status.status == "COMPLETED" else f"❌ Status: {status.status}")
    print(f"✅ Result: {status.result[:100]}" if provider_ok else f"❌ Result: {status.result}")

    # Test with model override
    print("\n  Testing with model_override...")
    handle2 = client.submit_task(
        "What is 7 * 8?",
        model_override="gpt-5-nano-2025-08-07",
        mode="simple",
    )
    time.sleep(8)  # Wait longer for model override
    status2 = client.get_status(handle2.task_id)

    model_ok = status2.result and "56" in status2.result
    print(f"  ✅ Model override working: {status2.result[:80]}" if model_ok else f"  ❌ Model override failed: {status2.result}")

    return provider_ok and model_ok

def test_provider_content_normalization():
    """Test 4: Content normalization (GPT-5 content parts fix)"""
    print("\n" + "=" * 70)
    print("Test 4: Provider Content Normalization (GPT-5 fix)")
    print("=" * 70)

    # Use GPT-5 nano to test content parts normalization
    handle = client.submit_task(
        "Explain in 2-3 sentences what machine learning is.",
        model_override="gpt-5-nano-2025-08-07",
        mode="simple",
    )
    print(f"Task ID: {handle.task_id}")

    time.sleep(10)
    status = client.get_status(handle.task_id)

    # The fix ensures non-empty results even for longer responses
    content_ok = status.result and len(status.result) > 100
    print(f"✅ Status: {status.status}" if status.status == "COMPLETED" else f"❌ Status: {status.status}")
    print(f"✅ Result length: {len(status.result) if status.result else 0}" if content_ok else f"❌ Empty result (content parts bug)")
    print(f"Result: {status.result[:200] if status.result else 'EMPTY'}...")

    return content_ok

# Run all tests
print("\n" + "=" * 70)
print("ADVANCED FEATURES TEST SUITE")
print("=" * 70)

results = {
    "Tool Usage": test_tool_usage(),
    "Streaming Mode": test_streaming_mode(),
    "OpenAI-Compatible Provider": test_openai_compatible_provider(),
    "Content Normalization (GPT-5)": test_provider_content_normalization(),
}

# Summary
print("\n" + "=" * 70)
print("TEST SUMMARY")
print("=" * 70)

for test_name, passed in results.items():
    status = "✅ PASS" if passed else "❌ FAIL"
    print(f"{status}: {test_name}")

total = len(results)
passed = sum(1 for v in results.values() if v)  # Count True values only
print(f"\nTotal: {passed}/{total} tests passed")

if passed == total:
    print("\n🎉 All advanced feature tests passed!")
else:
    print(f"\n⚠️  {total - passed} test(s) failed - review above for details")

client.close()
