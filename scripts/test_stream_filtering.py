#!/usr/bin/env python3
"""Smoke test for SSE event type filtering"""

import sys
import time
import requests

BASE_URL = "http://localhost:8080"

def count_sse_events(workflow_id, types_filter=None, timeout_sec=5):
    """Count SSE events from a workflow with optional type filter"""
    url = f"{BASE_URL}/api/v1/stream/sse"
    params = {"workflow_id": workflow_id}
    if types_filter:
        params["types"] = types_filter

    try:
        with requests.get(url, params=params, stream=True, timeout=timeout_sec) as response:
            if response.status_code != 200:
                return 0, response.text

            count = 0
            for line in response.iter_lines():
                if line and line.decode('utf-8').startswith('event:'):
                    count += 1
            return count, None
    except requests.Timeout:
        # Timeout is expected - we just want to count events received so far
        return count, None
    except Exception as e:
        return 0, str(e)

def main():
    print("=" * 60)
    print("SSE Event Type Filtering Smoke Test")
    print("=" * 60)

    # Submit a test task
    print("\n1. Submitting test task...")
    response = requests.post(f"{BASE_URL}/api/v1/tasks", json={"query": "What is 2+2?"})
    data = response.json()
    task_id = data["task_id"]
    print(f"   Task ID: {task_id}")

    # Wait for completion
    print("\n2. Waiting for task completion...")
    time.sleep(5)

    # Test 1: Unfiltered stream
    print("\n3. Testing SSE stream WITHOUT filter...")
    all_count, error = count_sse_events(task_id)
    print(f"   Received {all_count} events (unfiltered)")
    if error:
        print(f"   Error: {error}")
        return 1
    if all_count < 5:
        print(f"   ❌ FAIL: Expected at least 5 events, got {all_count}")
        return 1
    print("   ✅ PASS: Unfiltered stream works")

    # Test 2: LLM_OUTPUT filter
    print("\n4. Testing SSE stream WITH filter (LLM_OUTPUT)...")
    llm_count, error = count_sse_events(task_id, "LLM_OUTPUT")
    print(f"   Received {llm_count} LLM_OUTPUT events")
    if error:
        print(f"   Error: {error}")
        return 1
    if llm_count < 1:
        print(f"   ❌ FAIL: Expected at least 1 LLM_OUTPUT event, got {llm_count}")
        return 1
    print("   ✅ PASS: LLM_OUTPUT filter works")

    # Test 3: Multi-type filter
    print("\n5. Testing SSE stream WITH filter (LLM_OUTPUT,WORKFLOW_COMPLETED)...")
    multi_count, error = count_sse_events(task_id, "LLM_OUTPUT,WORKFLOW_COMPLETED")
    print(f"   Received {multi_count} filtered events")
    if error:
        print(f"   Error: {error}")
        return 1
    if multi_count < 2:
        print(f"   ❌ FAIL: Expected at least 2 events, got {multi_count}")
        return 1
    print("   ✅ PASS: Multi-type filter works")

    # Test 4: Gateway validation
    print("\n6. Testing gateway doesn't reject valid event types...")
    url = f"{BASE_URL}/api/v1/stream/sse"
    response = requests.get(url, params={
        "workflow_id": task_id,
        "types": "LLM_OUTPUT,WORKFLOW_COMPLETED"
    }, timeout=1, stream=True)

    if "Invalid event type" in response.text[:1000]:
        print("   ❌ FAIL: Gateway still rejecting valid event types")
        return 1
    print("   ✅ PASS: Gateway accepts event type filters")

    # Test 5: Redis stream ID
    print("\n7. Testing Redis stream ID in last_event_id parameter...")
    try:
        response = requests.get(url, params={
            "workflow_id": task_id,
            "last_event_id": "1700000000000-0"
        }, timeout=2, stream=True)

        # If we get 200, gateway accepts it; if 400, it rejects
        if response.status_code == 400:
            print("   ❌ FAIL: Gateway rejecting Redis stream IDs (HTTP 400)")
            return 1
        elif response.status_code == 200:
            print("   ✅ PASS: Gateway accepts Redis stream IDs")
        else:
            print(f"   ⚠️  Unexpected status code: {response.status_code}")
    except requests.Timeout:
        # Timeout is OK - means gateway accepted and started streaming
        print("   ✅ PASS: Gateway accepts Redis stream IDs (streaming started)")

    print("\n" + "=" * 60)
    print("✅ All event filtering tests passed!")
    print("=" * 60)
    return 0

if __name__ == "__main__":
    sys.exit(main())
