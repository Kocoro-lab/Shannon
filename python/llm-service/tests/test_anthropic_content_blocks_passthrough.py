"""Verify the Anthropic provider relays the full ordered content_blocks list
end-to-end, including interleaved thinking blocks and their signatures.

This is the wire-shape change that lets ShanClaw (and any other client) echo
the assistant's trajectory back to Anthropic on the next turn — without it
the model loses the native thinking context, which empirically caused empty
`think({})` tool emissions and 14-minute agent-loop hangs.

See plan 2026-05-14-thinking-blocks-cc-alignment.md Phase A.
"""

import os
from types import SimpleNamespace

# Set dummy key before import.
os.environ.setdefault("ANTHROPIC_API_KEY", "test-key-for-unit-tests")

from llm_provider.anthropic_provider import _block_to_dict
from llm_provider.base import CompletionResponse, TokenUsage


# ---------- CompletionResponse dataclass field ----------

def test_completion_response_carries_content_blocks():
    resp = CompletionResponse(
        content="visible reply",
        model="claude-sonnet-4-6",
        provider="anthropic",
        usage=TokenUsage(input_tokens=10, output_tokens=5, total_tokens=15, estimated_cost=0.0),
        finish_reason="tool_use",
        content_blocks=[
            {"type": "thinking", "thinking": "plan", "signature": "sig1"},
            {"type": "text", "text": "visible reply"},
            {"type": "tool_use", "id": "toolu_X", "name": "file_read", "input": {"path": "/x"}},
        ],
    )
    assert resp.content_blocks is not None
    assert len(resp.content_blocks) == 3
    assert [b["type"] for b in resp.content_blocks] == ["thinking", "text", "tool_use"]


def test_completion_response_content_blocks_defaults_none():
    resp = CompletionResponse(
        content="hi",
        model="claude-sonnet-4-6",
        provider="anthropic",
        usage=TokenUsage(input_tokens=1, output_tokens=1, total_tokens=2, estimated_cost=0.0),
        finish_reason="stop",
    )
    assert resp.content_blocks is None


# ---------- _block_to_dict helper ----------

def test_block_to_dict_pydantic_shape():
    """SDK Pydantic objects (most common case) — use model_dump."""
    class FakePydantic:
        def model_dump(self, exclude_none=True):
            return {"type": "thinking", "thinking": "x", "signature": "s"}

    out = _block_to_dict(FakePydantic(), "thinking")
    assert out == {"type": "thinking", "thinking": "x", "signature": "s"}


def test_block_to_dict_to_dict_shape():
    """Older SDKs / compat providers expose .to_dict()."""
    class FakeOldSdk:
        def to_dict(self):
            return {"type": "text", "text": "hello"}

    out = _block_to_dict(FakeOldSdk(), "text")
    assert out == {"type": "text", "text": "hello"}


def test_block_to_dict_plain_dict_input():
    """MiniMax compat: input is already a plain dict."""
    inp = {"type": "tool_use", "id": "t1", "name": "x", "input": {"a": 1}}
    out = _block_to_dict(inp, "tool_use")
    # Output is a copy, not the same reference.
    assert out == inp
    assert out is not inp
    # Mutating output must not affect input.
    out["mutated"] = True
    assert "mutated" not in inp


def test_block_to_dict_per_type_fallback_thinking():
    """SDK shape with neither model_dump nor to_dict — falls back to per-type."""
    block = SimpleNamespace(type="thinking", thinking="fallback-text", signature="sig-fallback")
    # Strip the model_dump shape so the helper exercises the fallback path.
    out = _block_to_dict(block, "thinking")
    # SimpleNamespace has no model_dump/to_dict, so we hit the per-type branch.
    assert out is not None
    assert out["type"] == "thinking"
    assert out["thinking"] == "fallback-text"
    assert out["signature"] == "sig-fallback"


def test_block_to_dict_per_type_fallback_redacted_thinking():
    block = SimpleNamespace(type="redacted_thinking", data="opaque-blob")
    out = _block_to_dict(block, "redacted_thinking")
    assert out == {"type": "redacted_thinking", "data": "opaque-blob"}


def test_block_to_dict_unknown_type_returns_none():
    """Unknown / unsupported block types: emit None so caller skips them."""
    block = SimpleNamespace(type="future_block_type")
    out = _block_to_dict(block, "future_block_type")
    # No model_dump, no to_dict, no per-type match → None.
    assert out is None


# ---------- _serialize_completion wire shape ----------

def test_serialize_completion_includes_content_blocks():
    # Lazy import to avoid loading the whole providers package at module top.
    from llm_service.providers import ProviderManager

    resp = CompletionResponse(
        content="visible",
        model="claude-sonnet-4-6",
        provider="anthropic",
        usage=TokenUsage(input_tokens=1, output_tokens=1, total_tokens=2, estimated_cost=0.0),
        finish_reason="stop",
        content_blocks=[
            {"type": "thinking", "thinking": "x", "signature": "s"},
            {"type": "text", "text": "visible"},
        ],
    )

    mgr = ProviderManager.__new__(ProviderManager)
    out = mgr._serialize_completion(resp)

    assert "content_blocks" in out
    assert out["content_blocks"][0] == {"type": "thinking", "thinking": "x", "signature": "s"}
    # Legacy fields still populated for older clients.
    assert out["output_text"] == "visible"


def test_serialize_completion_omits_content_blocks_when_none():
    """Backward compat: when provider didn't populate the new field, the wire
    response must NOT carry an empty content_blocks key — that would force
    older clients to handle a None / [] value they don't expect."""
    from llm_service.providers import ProviderManager

    resp = CompletionResponse(
        content="hi",
        model="claude-sonnet-4-6",
        provider="anthropic",
        usage=TokenUsage(input_tokens=1, output_tokens=1, total_tokens=2, estimated_cost=0.0),
        finish_reason="stop",
        content_blocks=None,
    )
    mgr = ProviderManager.__new__(ProviderManager)
    out = mgr._serialize_completion(resp)
    assert "content_blocks" not in out


# ---------- Redis cache serialize/deserialize roundtrip ----------

def test_redis_cache_roundtrip_preserves_content_blocks():
    """A Redis cache hit must return content_blocks intact. Without this fix,
    every cached response silently reverted to {output_text, tool_calls} only
    and the thinking trajectory broke for any cached upstream call."""
    from llm_provider.manager import _serialize_response, _deserialize_response

    resp = CompletionResponse(
        content="visible",
        model="claude-sonnet-4-6",
        provider="anthropic",
        usage=TokenUsage(input_tokens=10, output_tokens=5, total_tokens=15, estimated_cost=0.0),
        finish_reason="tool_use",
        tool_calls=[{"id": "t1", "name": "f", "arguments": {}}],
        content_blocks=[
            {"type": "thinking", "thinking": "private reasoning", "signature": "sigA"},
            {"type": "text", "text": "visible"},
            {"type": "tool_use", "id": "t1", "name": "f", "input": {}},
            {"type": "thinking", "thinking": "interleaved reasoning", "signature": "sigB"},
        ],
    )

    serialized = _serialize_response(resp)
    assert "content_blocks" in serialized, "serializer dropped content_blocks"
    assert len(serialized["content_blocks"]) == 4
    assert [b["type"] for b in serialized["content_blocks"]] == ["thinking", "text", "tool_use", "thinking"]
    assert serialized["content_blocks"][0]["signature"] == "sigA"
    assert serialized["content_blocks"][3]["signature"] == "sigB"

    deserialized = _deserialize_response(serialized)
    assert deserialized.content_blocks is not None
    assert len(deserialized.content_blocks) == 4
    assert deserialized.content_blocks[0]["thinking"] == "private reasoning"
    assert deserialized.content_blocks[0]["signature"] == "sigA"
    assert deserialized.cached is True  # cache marker still flips


def test_redis_cache_legacy_response_no_content_blocks():
    """Cache entries written before the new field exists must still deserialize
    cleanly with content_blocks=None — no exception, no fabricated empty list."""
    from llm_provider.manager import _deserialize_response

    legacy_payload = {
        "content": "hi",
        "model": "claude-sonnet-4-6",
        "provider": "anthropic",
        "usage": {"input_tokens": 1, "output_tokens": 1, "total_tokens": 2, "estimated_cost": 0.0},
        "finish_reason": "stop",
        # No content_blocks key — pre-2026-05 cache entry.
    }
    resp = _deserialize_response(legacy_payload)
    assert resp.content_blocks is None
    assert resp.content == "hi"
