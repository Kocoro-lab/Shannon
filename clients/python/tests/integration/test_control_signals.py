#!/usr/bin/env python3
"""
Test script for Shannon SDK v0.4.0 control signals.
Tests pause/resume/control-state functionality with real tasks.

NOTE: These are integration tests requiring a running Shannon stack.
They are skipped by default. To run:
    SHANNON_INTEGRATION_TESTS=1 pytest tests/integration/
    OR
    python tests/integration/test_control_signals.py
"""

import os
import time
import sys

import pytest

from shannon import ShannonClient, ControlState

# Skip in pytest unless explicitly enabled
pytestmark = pytest.mark.skipif(
    not os.environ.get("SHANNON_INTEGRATION_TESTS"),
    reason="Integration tests require running Shannon stack. Set SHANNON_INTEGRATION_TESTS=1 to run."
)


def test_control_signals():
    """Test pause, resume, and control-state features."""

    client = ShannonClient(base_url="http://localhost:8080")

    print("=" * 60)
    print("Shannon SDK v0.4.0 - Control Signals Test")
    print("=" * 60)

    # Test 1: Submit a task that will take some time
    print("\n[1] Submitting a research task...")
    handle = client.submit_task(
        "Analyze the latest developments in AI safety research in 2024",
        session_id="sdk-control-test",
        context={
            "force_research": True,
            "research_strategy": "quick"
        }
    )

    print(f"✓ Task submitted: {handle.task_id}")
    print(f"  Workflow ID: {handle.workflow_id}")

    # Wait a few seconds to let the task start
    print("\n[2] Waiting 5 seconds for task to start running...")
    time.sleep(5)

    # Check initial status
    status = client.get_status(handle.task_id)
    print(f"✓ Task status: {status.status}")

    # Test 2: Pause the task
    print("\n[3] Testing pause_task()...")
    try:
        client.pause_task(handle.task_id, reason="Testing SDK pause feature")
        print("✓ Pause request sent successfully")
    except Exception as e:
        print(f"✗ Pause failed: {e}")
        return False

    # Wait for pause to take effect
    print("  Waiting 3 seconds for pause to take effect at checkpoint...")
    time.sleep(3)

    # Test 3: Get control state
    print("\n[4] Testing get_control_state()...")
    try:
        state = client.get_control_state(handle.task_id)
        print(f"✓ Control state retrieved:")
        print(f"  - is_paused: {state.is_paused}")
        print(f"  - is_cancelled: {state.is_cancelled}")
        if state.paused_at:
            print(f"  - paused_at: {state.paused_at}")
        if state.pause_reason:
            print(f"  - pause_reason: {state.pause_reason}")
        if state.paused_by:
            print(f"  - paused_by: {state.paused_by}")

        # Verify pause worked
        if state.is_paused:
            print("✓ Task successfully paused")
        else:
            print("⚠ Task not yet paused (may need more time or already completed)")
    except Exception as e:
        print(f"✗ Get control state failed: {e}")
        return False

    # Test 4: Resume the task
    print("\n[5] Testing resume_task()...")
    try:
        client.resume_task(handle.task_id, reason="Testing SDK resume feature")
        print("✓ Resume request sent successfully")
    except Exception as e:
        print(f"✗ Resume failed: {e}")
        return False

    # Wait a bit and check control state again
    print("  Waiting 2 seconds...")
    time.sleep(2)

    state = client.get_control_state(handle.task_id)
    print(f"✓ Control state after resume:")
    print(f"  - is_paused: {state.is_paused}")

    if not state.is_paused:
        print("✓ Task successfully resumed")
    else:
        print("⚠ Task still shows as paused")

    # Test 5: Wait for completion
    print("\n[6] Waiting for task to complete (timeout: 120s)...")
    try:
        final = client.wait(handle.task_id, timeout=120)
        print(f"✓ Task completed with status: {final.status}")

        # Verify metadata is present
        if final.model_used:
            print(f"  - Model used: {final.model_used}")
        if final.provider:
            print(f"  - Provider: {final.provider}")
        if final.usage:
            print(f"  - Total tokens: {final.usage.get('total_tokens')}")
            print(f"  - Cost: ${final.usage.get('cost_usd', 0):.6f}")

        # Show result preview
        if final.result:
            preview = final.result[:200] + "..." if len(final.result) > 200 else final.result
            print(f"\n  Result preview:\n  {preview}")

    except Exception as e:
        print(f"✗ Wait failed: {e}")
        return False

    print("\n" + "=" * 60)
    print("✓ All control signal tests passed!")
    print("=" * 60)

    client.close()
    return True


def test_cli_commands():
    """Test CLI commands for control signals."""

    print("\n" + "=" * 60)
    print("Shannon SDK v0.4.0 - CLI Commands Test")
    print("=" * 60)

    client = ShannonClient(base_url="http://localhost:8080")

    # Submit a task for CLI testing
    print("\n[1] Submitting task for CLI testing...")
    handle = client.submit_task(
        "What are the key benefits of multi-agent AI systems?",
        session_id="sdk-cli-test"
    )
    print(f"✓ Task ID: {handle.task_id}")

    client.close()

    # Wait for task to start
    time.sleep(3)

    # Test CLI pause
    print(f"\n[2] Testing CLI: shannon pause {handle.task_id}")
    import subprocess
    result = subprocess.run(
        ["python3", "-m", "shannon.cli", "--base-url", "http://localhost:8080",
         "pause", handle.task_id, "--reason", "CLI test pause"],
        capture_output=True,
        text=True
    )
    print(result.stdout)
    if result.returncode != 0:
        print(f"✗ CLI pause failed: {result.stderr}")
        return False

    # Test CLI control-state
    time.sleep(2)
    print(f"\n[3] Testing CLI: shannon control-state {handle.task_id}")
    result = subprocess.run(
        ["python3", "-m", "shannon.cli", "--base-url", "http://localhost:8080",
         "control-state", handle.task_id],
        capture_output=True,
        text=True
    )
    print(result.stdout)
    if result.returncode != 0:
        print(f"✗ CLI control-state failed: {result.stderr}")
        return False

    # Test CLI resume
    print(f"\n[4] Testing CLI: shannon resume {handle.task_id}")
    result = subprocess.run(
        ["python3", "-m", "shannon.cli", "--base-url", "http://localhost:8080",
         "resume", handle.task_id, "--reason", "CLI test resume"],
        capture_output=True,
        text=True
    )
    print(result.stdout)
    if result.returncode != 0:
        print(f"✗ CLI resume failed: {result.stderr}")
        return False

    print("\n" + "=" * 60)
    print("✓ All CLI command tests passed!")
    print("=" * 60)

    return True


if __name__ == "__main__":
    # Test programmatic API
    api_success = test_control_signals()

    # Test CLI commands
    cli_success = test_cli_commands()

    # Summary
    print("\n" + "=" * 60)
    print("TEST SUMMARY")
    print("=" * 60)
    print(f"Programmatic API: {'✓ PASSED' if api_success else '✗ FAILED'}")
    print(f"CLI Commands:     {'✓ PASSED' if cli_success else '✗ FAILED'}")
    print("=" * 60)

    sys.exit(0 if (api_success and cli_success) else 1)
