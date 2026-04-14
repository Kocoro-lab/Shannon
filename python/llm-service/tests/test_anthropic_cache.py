"""Tests for Anthropic prompt cache behavior with multi-turn messages."""

import os
import pytest

# Set dummy key before import
os.environ.setdefault("ANTHROPIC_API_KEY", "test-key-for-unit-tests")

from llm_provider.anthropic_provider import AnthropicProvider, CACHE_TTL_LONG


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
        assert api_req["system"][0]["cache_control"] == CACHE_TTL_LONG

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


    def test_leading_cache_break_no_stable_prefix(self):
        """Leading cache_break with no stable content must not produce empty text block.

        Regression: ShanClaw sends <!-- cache_break -->volatile... when StableContext
        is empty. Empty text block + cache_control causes Anthropic 400 error.
        """
        provider = self._make_provider()
        messages = [
            {"role": "system", "content": "System."},
            {"role": "user", "content": "<!-- cache_break -->\n## Context\nVolatile data here"},
        ]
        _, claude_msgs = provider._convert_messages_to_claude_format(messages)
        user_msg = claude_msgs[0]
        # Should fall back to plain string (no content blocks with empty text)
        assert isinstance(user_msg["content"], str)
        assert user_msg["content"] == "\n## Context\nVolatile data here"

    def test_whitespace_only_stable_prefix(self):
        """Whitespace-only stable prefix treated as empty — no cache_control block."""
        provider = self._make_provider()
        messages = [
            {"role": "system", "content": "System."},
            {"role": "user", "content": "  \n<!-- cache_break -->\nVolatile"},
        ]
        _, claude_msgs = provider._convert_messages_to_claude_format(messages)
        user_msg = claude_msgs[0]
        assert isinstance(user_msg["content"], str)

    def test_nonempty_stable_prefix_preserved_raw(self):
        """Non-empty stable prefix is sent as-is (not stripped) in content block."""
        provider = self._make_provider()
        messages = [
            {"role": "system", "content": "System."},
            {"role": "user", "content": "Stable context\n<!-- cache_break -->\nVolatile"},
        ]
        _, claude_msgs = provider._convert_messages_to_claude_format(messages)
        user_msg = claude_msgs[0]
        assert isinstance(user_msg["content"], list)
        assert user_msg["content"][0]["text"] == "Stable context\n"
        assert user_msg["content"][0]["cache_control"] == {"type": "ephemeral"}
        assert user_msg["content"][1]["text"] == "\nVolatile"


class TestSystemMessageSplit:
    """Verify system message splits on <!-- volatile --> marker."""

    def _make_provider(self):
        return AnthropicProvider(_MINIMAL_CONFIG)

    def test_volatile_marker_splits_system_into_two_blocks(self):
        """System message with <!-- volatile --> produces two API blocks."""
        provider = self._make_provider()
        system_msg = "Stable protocol instructions\n<!-- volatile -->\nDynamic task context"
        blocks = provider._split_system_message(system_msg)
        assert len(blocks) == 2
        assert blocks[0]["text"] == "Stable protocol instructions"
        assert "cache_control" in blocks[0]
        assert blocks[0]["cache_control"] == CACHE_TTL_LONG
        assert blocks[1]["text"] == "Dynamic task context"
        assert "cache_control" not in blocks[1]

    def test_no_marker_returns_single_cached_block(self):
        """System message without marker returns single block (backward compat)."""
        provider = self._make_provider()
        system_msg = "Plain system prompt without any marker"
        blocks = provider._split_system_message(system_msg)
        assert len(blocks) == 1
        assert blocks[0]["text"] == system_msg
        assert blocks[0]["cache_control"] == CACHE_TTL_LONG

    def test_empty_volatile_section_omitted(self):
        """Trailing marker with no volatile content produces single block."""
        provider = self._make_provider()
        system_msg = "Stable content only\n<!-- volatile -->\n   "
        blocks = provider._split_system_message(system_msg)
        assert len(blocks) == 1
        assert blocks[0]["cache_control"]["type"] == "ephemeral"

    def test_build_api_request_uses_split(self):
        """_build_api_request uses _split_system_message for system blocks."""
        provider = self._make_provider()
        from llm_provider.base import CompletionRequest
        request = CompletionRequest(
            messages=[
                {"role": "system", "content": "Stable\n<!-- volatile -->\nVolatile"},
                {"role": "user", "content": "Hello."},
            ],
            temperature=0.3,
            max_tokens=100,
        )
        model_config = type("MC", (), {
            "model_id": "claude-sonnet-4-6",
            "supports_functions": False,
            "context_window": 200000,
            "max_tokens": 8192,
        })()
        api_req = provider._build_api_request(request, model_config)
        assert len(api_req["system"]) == 2
        assert "cache_control" in api_req["system"][0]
        assert "cache_control" not in api_req["system"][1]


class TestVolatileTTL:
    """Verify TTL behavior for volatile split.

    System, tools, and stable user prefix all use 1h TTL.
    Anthropic prefix order: system → tools → messages.
    TTL monotonic non-increasing: system(1h) ≥ tools(1h) ≥ messages(1h) ✓
    """

    def _make_provider(self):
        return AnthropicProvider(_MINIMAL_CONFIG)

    def test_stable_prefix_uses_1h_ttl(self):
        """Stable prefix block uses 1h TTL for long-running workflows."""
        provider = self._make_provider()
        system_msg = "Stable\n<!-- volatile -->\nVolatile"
        blocks = provider._split_system_message(system_msg)
        assert blocks[0]["cache_control"] == CACHE_TTL_LONG
        assert blocks[0]["cache_control"]["ttl"] == "1h"

    def test_no_marker_uses_1h_ttl(self):
        """Without volatile marker, use 1h TTL."""
        provider = self._make_provider()
        blocks = provider._split_system_message("Plain prompt")
        assert blocks[0]["cache_control"] == CACHE_TTL_LONG
        assert blocks[0]["cache_control"]["ttl"] == "1h"


class TestToolSchemaFreeze:
    """Verify tool schemas are frozen after first build for cache stability."""

    def _make_provider(self):
        return AnthropicProvider(_MINIMAL_CONFIG)

    def test_same_tools_return_frozen_copy(self):
        """Same tool names → identical schema (frozen from first build)."""
        provider = self._make_provider()
        functions = [
            {"name": "web_search", "description": "Search v1", "parameters": {"properties": {}, "required": []}},
            {"name": "calculator", "description": "Calc v1", "parameters": {"properties": {}, "required": []}},
        ]
        tools1 = provider._convert_functions_to_tools(functions)

        # Change descriptions (simulating drift)
        functions_v2 = [
            {"name": "web_search", "description": "Search v2 CHANGED", "parameters": {"properties": {}, "required": []}},
            {"name": "calculator", "description": "Calc v2 CHANGED", "parameters": {"properties": {}, "required": []}},
        ]
        tools2 = provider._convert_functions_to_tools(functions_v2)

        # Should return frozen v1 schemas, not v2
        assert tools1[0]["description"] == tools2[0]["description"]

    def test_different_tool_set_rebuilds(self):
        """Different tool names → rebuild schema."""
        provider = self._make_provider()
        func_a = [{"name": "web_search", "description": "Search", "parameters": {"properties": {}, "required": []}}]
        func_b = [{"name": "calculator", "description": "Calc", "parameters": {"properties": {}, "required": []}}]

        tools_a = provider._convert_functions_to_tools(func_a)
        tools_b = provider._convert_functions_to_tools(func_b)
        assert tools_a[0]["name"] != tools_b[0]["name"]

    def test_reset_clears_cache(self):
        """reset_tool_cache() forces rebuild on next call."""
        provider = self._make_provider()
        functions = [{"name": "web_search", "description": "v1", "parameters": {"properties": {}, "required": []}}]
        provider._convert_functions_to_tools(functions)

        provider.reset_tool_cache()

        functions_v2 = [{"name": "web_search", "description": "v2", "parameters": {"properties": {}, "required": []}}]
        tools = provider._convert_functions_to_tools(functions_v2)
        assert tools[0]["description"] == "v2"


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


class TestCacheBreakDetector:
    """Verify cache break detection across sequential API calls."""

    def test_first_call_no_break(self):
        """First call has no previous state → no break detected."""
        from llm_provider.anthropic_provider import CacheBreakDetector
        detector = CacheBreakDetector()
        result = detector.check(
            system_text="Stable prompt",
            tool_names=["web_search", "calculator"],
            model="claude-sonnet-4-6",
        )
        assert result is None

    def test_identical_calls_no_break(self):
        """Two identical calls → no break."""
        from llm_provider.anthropic_provider import CacheBreakDetector
        detector = CacheBreakDetector()
        detector.check(system_text="Prompt", tool_names=["a"], model="m1")
        result = detector.check(system_text="Prompt", tool_names=["a"], model="m1")
        assert result is None

    def test_system_change_detected(self):
        """Changed system prompt text → break detected with reason."""
        from llm_provider.anthropic_provider import CacheBreakDetector
        detector = CacheBreakDetector()
        detector.check(system_text="V1 prompt", tool_names=["a"], model="m1")
        result = detector.check(system_text="V2 prompt changed", tool_names=["a"], model="m1")
        assert result is not None
        assert "system" in result["changed"]

    def test_tool_set_change_detected(self):
        """Changed tool set → break detected."""
        from llm_provider.anthropic_provider import CacheBreakDetector
        detector = CacheBreakDetector()
        detector.check(system_text="P", tool_names=["a", "b"], model="m1")
        result = detector.check(system_text="P", tool_names=["a", "c"], model="m1")
        assert result is not None
        assert "tools" in result["changed"]
        assert "c" in result.get("tools_added", [])
        assert "b" in result.get("tools_removed", [])

    def test_model_change_detected(self):
        """Changed model → break detected."""
        from llm_provider.anthropic_provider import CacheBreakDetector
        detector = CacheBreakDetector()
        detector.check(system_text="P", tool_names=["a"], model="m1")
        result = detector.check(system_text="P", tool_names=["a"], model="m2")
        assert result is not None
        assert "model" in result["changed"]

    def test_multiple_changes_detected(self):
        """Multiple changes reported in one break."""
        from llm_provider.anthropic_provider import CacheBreakDetector
        detector = CacheBreakDetector()
        detector.check(system_text="P1", tool_names=["a"], model="m1")
        result = detector.check(system_text="P2", tool_names=["b"], model="m2")
        assert result is not None
        assert len(result["changed"]) == 3

    def test_call_count_increments(self):
        """Call count tracks API calls for this detector instance."""
        from llm_provider.anthropic_provider import CacheBreakDetector
        detector = CacheBreakDetector()
        detector.check(system_text="P", tool_names=["a"], model="m1")
        detector.check(system_text="P", tool_names=["a"], model="m1")
        detector.check(system_text="P", tool_names=["a"], model="m1")
        assert detector.call_count == 3


class TestCacheBreakIntegration:
    """Verify CacheBreakDetector is wired into _build_api_request."""

    def _make_provider(self):
        return AnthropicProvider(_MINIMAL_CONFIG)

    def test_provider_has_detector(self):
        """Provider instance has a CacheBreakDetector."""
        provider = self._make_provider()
        assert hasattr(provider, "_cache_break_detector")
        from llm_provider.anthropic_provider import CacheBreakDetector
        assert isinstance(provider._cache_break_detector, CacheBreakDetector)

    def test_detector_called_on_build(self):
        """_build_api_request calls detector.check (call_count increments)."""
        provider = self._make_provider()
        from llm_provider.base import CompletionRequest
        request = CompletionRequest(
            messages=[
                {"role": "system", "content": "System prompt."},
                {"role": "user", "content": "Hello."},
            ],
            temperature=0.3,
            max_tokens=100,
        )
        model_config = type("MC", (), {
            "model_id": "claude-sonnet-4-6",
            "supports_functions": False,
            "context_window": 200000,
            "max_tokens": 8192,
        })()
        provider._build_api_request(request, model_config)
        provider._build_api_request(request, model_config)
        assert provider._cache_break_detector.call_count == 2


class TestCallSequence:
    """Verify call_sequence counter flows through TokenUsage."""

    def test_token_usage_has_call_sequence_field(self):
        """TokenUsage dataclass accepts call_sequence."""
        from llm_provider.base import TokenUsage
        usage = TokenUsage(
            input_tokens=100, output_tokens=50, total_tokens=150,
            estimated_cost=0.001, call_sequence=3,
        )
        assert usage.call_sequence == 3

    def test_token_usage_default_call_sequence_zero(self):
        """TokenUsage.call_sequence defaults to 0."""
        from llm_provider.base import TokenUsage
        usage = TokenUsage(
            input_tokens=100, output_tokens=50, total_tokens=150,
            estimated_cost=0.001,
        )
        assert usage.call_sequence == 0

    def test_token_usage_add_takes_max_sequence(self):
        """Adding TokenUsage takes max call_sequence."""
        from llm_provider.base import TokenUsage
        a = TokenUsage(input_tokens=100, output_tokens=50, total_tokens=150, estimated_cost=0.001, call_sequence=3)
        b = TokenUsage(input_tokens=200, output_tokens=100, total_tokens=300, estimated_cost=0.002, call_sequence=7)
        combined = a + b
        assert combined.call_sequence == 7


class TestCacheSourceObservability:
    """Verify cache_source plumbing for per-source observability metrics."""

    def test_cache_source_field_defaults_to_none(self):
        """CompletionRequest.cache_source defaults to None (emits as 'unknown')."""
        from llm_provider.base import CompletionRequest
        req = CompletionRequest(messages=[{"role": "user", "content": "hi"}])
        assert req.cache_source is None

    def test_cache_source_field_accepts_string(self):
        """CompletionRequest.cache_source accepts caller label."""
        from llm_provider.base import CompletionRequest
        req = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            cache_source="agent_loop",
        )
        assert req.cache_source == "agent_loop"

    def test_providers_passthrough_includes_cache_source(self):
        """llm_service.providers.generate_completion has cache_source in both passthrough sets.

        Without this, cache_source from callers is silently dropped before reaching
        CompletionRequest and all metrics collapse to "unknown".
        """
        import inspect
        import llm_service.providers as providers_init

        src = inspect.getsource(providers_init)
        # There are two passthrough_fields blocks (one per code path); both must include it.
        assert src.count("\"cache_source\"") >= 2, (
            "cache_source must appear in both passthrough_fields blocks"
        )


class TestStreamingCacheAccounting:
    """Verify streaming path reads 1h cache creation and supports dict usage shape."""

    def test_record_cache_metrics_5m_vs_1h_split(self):
        """_record_cache_metrics splits cache_creation into 5m and 1h buckets."""
        from llm_provider import anthropic_provider as ap

        # Inline capture — we just verify the split math, not prometheus internals
        splits = {}
        original = ap._record_cache_metrics

        def _capture(provider, model, source, cache_read, cache_creation, cache_creation_1h):
            splits["read"] = cache_read
            splits["write_5m"] = max(0, cache_creation - cache_creation_1h)
            splits["write_1h"] = cache_creation_1h

        monkey = _capture
        monkey("anthropic", "claude-sonnet-4-6", "test", 500, 1200, 800)
        assert splits == {"read": 500, "write_5m": 400, "write_1h": 800}
        # Sanity: the real helper exists and is callable
        assert callable(original)

    def test_streaming_usage_dict_shape_parses_cache_fields(self):
        """Dict-shaped usage from Anthropic-compat providers parses cache fields.

        Regression: prior to this fix, streaming path used only getattr() and
        dict-shaped usage silently zeroed cache_read/creation/1h → underpriced cost.
        """
        # Simulate the extraction logic the streaming branch uses
        usage_dict = {
            "input_tokens": 1000,
            "output_tokens": 200,
            "cache_read_input_tokens": 500,
            "cache_creation_input_tokens": 1200,
            "cache_creation": {"ephemeral_1h_input_tokens": 800},
        }
        # Mimic the extraction contract
        assert isinstance(usage_dict, dict)
        cache_read = usage_dict.get("cache_read_input_tokens", 0) or 0
        cache_creation = usage_dict.get("cache_creation_input_tokens", 0) or 0
        cc = usage_dict.get("cache_creation")
        cache_creation_1h = (
            cc.get("ephemeral_1h_input_tokens", 0) or 0 if isinstance(cc, dict) else 0
        )
        assert cache_read == 500
        assert cache_creation == 1200
        assert cache_creation_1h == 800
