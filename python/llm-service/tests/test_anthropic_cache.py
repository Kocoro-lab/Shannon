"""Tests for Anthropic prompt cache behavior with multi-turn messages."""

import os
import pytest

# Set dummy key before import
os.environ.setdefault("ANTHROPIC_API_KEY", "test-key-for-unit-tests")

from llm_provider.anthropic_provider import AnthropicProvider


_MINIMAL_CONFIG = {
    "api_key": "test-key",
    "models": {
        "claude-sonnet-4-6": {
            "model_id": "claude-sonnet-4-6",
            "tier": "medium",
            "context_window": 200000,
            "max_tokens": 8192,
        },
    },
}


class TestMultiTurnCacheBreakpoints:
    """Verify cache_control placement with multi-turn agent messages."""

    def _make_provider(self):
        return AnthropicProvider(_MINIMAL_CONFIG)

    def test_multi_turn_no_per_message_cache_control(self):
        """Multi-turn messages should NOT have per-message cache_control.

        Top-level automatic caching (extra_body) handles growing-prefix caching.
        Per-message breakpoints on the last assistant would move each iteration,
        creating new cache entries instead of reading old ones.
        """
        provider = self._make_provider()
        messages = [
            {"role": "system", "content": "You are helpful."},
            {"role": "user", "content": "Task: research"},
            {"role": "assistant", "content": "I will search."},
            {"role": "user", "content": "Result: found data."},
            {"role": "assistant", "content": "Analyzing data."},
            {"role": "user", "content": "Budget: 3 calls. Decide."},
        ]
        system_msg, claude_msgs = provider._convert_messages_to_claude_format(messages)

        assert system_msg == "You are helpful."
        assert len(claude_msgs) == 5

        # No assistant message should have cache_control (handled by top-level automatic caching)
        for msg in claude_msgs:
            if msg["role"] == "assistant":
                assert isinstance(msg["content"], str), "Assistant content should be plain string, no cache_control blocks"

    def test_system_message_always_gets_cache_control(self):
        """System message gets cache_control in _build_api_request."""
        provider = self._make_provider()
        from llm_provider.base import CompletionRequest
        request = CompletionRequest(
            messages=[
                {"role": "system", "content": "System prompt text."},
                {"role": "user", "content": "Hello."},
            ],
            temperature=0.3,
            max_tokens=100,
        )
        model_config = type("MC", (), {
            "model_id": "claude-haiku-4-5-20251001",
            "supports_functions": False,
            "context_window": 200000,
            "max_tokens": 8192,
        })()
        api_req = provider._build_api_request(request, model_config)
        assert api_req["system"][0]["cache_control"] == {"type": "ephemeral"}

    def test_no_cache_break_in_multi_turn(self):
        """Multi-turn messages without marker produce plain string content."""
        provider = self._make_provider()
        messages = [
            {"role": "system", "content": "System."},
            {"role": "user", "content": "Context without marker."},
            {"role": "assistant", "content": "Decision."},
            {"role": "user", "content": "Current turn."},
        ]
        _, claude_msgs = provider._convert_messages_to_claude_format(messages)
        for msg in claude_msgs:
            if msg["role"] == "user":
                assert isinstance(msg["content"], str), "User messages without marker should be plain strings"


class TestToolOrdering:
    """Verify tools are sorted by name for cache prefix stability."""

    def _make_provider(self):
        return AnthropicProvider(_MINIMAL_CONFIG)

    def test_tools_sorted_by_name(self):
        provider = self._make_provider()
        functions = [
            {"name": "web_search", "description": "Search", "parameters": {"properties": {}, "required": []}},
            {"name": "calculator", "description": "Calc", "parameters": {"properties": {}, "required": []}},
            {"name": "file_read", "description": "Read", "parameters": {"properties": {}, "required": []}},
        ]
        tools = provider._convert_functions_to_tools(functions)
        names = [t["name"] for t in tools]
        assert names == ["calculator", "file_read", "web_search"]

    def test_cache_control_on_last_sorted_tool(self):
        """After sorting, the last tool alphabetically should be last in the list."""
        provider = self._make_provider()
        functions = [
            {"name": "web_search", "description": "Search", "parameters": {"properties": {}, "required": []}},
            {"name": "calculator", "description": "Calc", "parameters": {"properties": {}, "required": []}},
        ]
        tools = provider._convert_functions_to_tools(functions)
        assert tools[-1]["name"] == "web_search"
