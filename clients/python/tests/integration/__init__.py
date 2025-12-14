"""
Integration tests for Shannon Python SDK.

These tests require a running Shannon stack (localhost:8080) and are
SKIPPED by default when running pytest. They test control signal
features (pause/resume/cancel).

To run with pytest (requires running Shannon stack):
    SHANNON_INTEGRATION_TESTS=1 pytest tests/integration/ -v

To run directly (requires running Shannon stack):
    python tests/integration/test_control_signals.py
    python tests/integration/test_control_simple.py
"""
