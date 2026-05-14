"""
Anthropic Claude Provider Implementation
"""

import hashlib
import json
import os
import logging
import time
from collections import OrderedDict
from typing import Dict, List, Any, AsyncIterator, Optional
import anthropic
from anthropic import AsyncAnthropic

from .base import (
    LLMProvider,
    CompletionRequest,
    CompletionResponse,
    TokenUsage,
    TokenCounter,
    extract_text_from_content,
    _ensure_tool_id,
)


def _block_to_dict(content_block: Any, block_type: Optional[str]) -> Optional[Dict[str, Any]]:
    """Serialize a single Anthropic content block into a verbatim dict.

    Strategy (most → least preferred):
      1. SDK Pydantic shape: `model_dump(exclude_none=True)` — preserves any
         forward-compat fields like new `redacted_thinking` subtypes without
         this serializer needing to know about them.
      2. Generic `.to_dict()` — older Anthropic SDK / some compat providers.
      3. Plain dict input (e.g., MiniMax compat path) — copy.
      4. Per-type fallback — manual field extraction for the four block types
         we know exist (`text` / `thinking` / `redacted_thinking` / `tool_use`).
         Unknown types are skipped with a warning rather than emitting a
         shape that Anthropic won't accept on the round-trip.

    Returns None if all strategies fail (caller should not append a None into
    `content_blocks` — the legacy `content` / `tool_calls` fields still carry
    enough info for older clients).
    """
    # 1. Pydantic SDK shape.
    model_dump = getattr(content_block, "model_dump", None)
    if callable(model_dump):
        try:
            d = model_dump(exclude_none=True)
            if isinstance(d, dict):
                return d
        except Exception:  # noqa: BLE001
            pass

    # 2. Generic .to_dict() on older SDK shapes.
    to_dict = getattr(content_block, "to_dict", None)
    if callable(to_dict):
        try:
            d = to_dict()
            if isinstance(d, dict):
                return d
        except Exception:  # noqa: BLE001
            pass

    # 3. Plain dict input (MiniMax, etc.). Make a shallow copy so callers
    # can't mutate the upstream object via our returned dict.
    if isinstance(content_block, dict):
        return dict(content_block)

    # 4. Per-type fallback for the four known shapes.
    if block_type == "text":
        return {"type": "text", "text": getattr(content_block, "text", "") or ""}
    if block_type == "thinking":
        return {
            "type": "thinking",
            "thinking": getattr(content_block, "thinking", "") or "",
            "signature": getattr(content_block, "signature", "") or "",
        }
    if block_type == "redacted_thinking":
        return {"type": "redacted_thinking", "data": getattr(content_block, "data", "") or ""}
    if block_type == "tool_use":
        return {
            "type": "tool_use",
            "id": getattr(content_block, "id", "") or "",
            "name": getattr(content_block, "name", "") or "",
            "input": getattr(content_block, "input", {}) or {},
        }

    # Unknown / unhandled type: don't emit a partial / malformed shape.
    return None

logger = logging.getLogger(__name__)

# Optional prompt-cache metrics (per-source observability).
# Labels: provider, model, source. `source` comes from CompletionRequest.cache_source
# so we can see which call sites (agent_loop/decompose/tool_select/synthesis) are
# paying the 2.0x 1h write premium without enough reads to break even.
try:
    from prometheus_client import Counter as _PromCounter

    _CACHE_METRICS_ENABLED = True
    ANTHROPIC_CACHE_READ_TOKENS = _PromCounter(
        "anthropic_cache_read_tokens_total",
        "Anthropic prompt cache read tokens (billed at 0.1x input)",
        labelnames=("provider", "model", "source"),
    )
    ANTHROPIC_CACHE_WRITE_5M_TOKENS = _PromCounter(
        "anthropic_cache_write_5m_tokens_total",
        "Anthropic prompt cache write tokens at 5min TTL (billed at 1.25x input)",
        labelnames=("provider", "model", "source"),
    )
    ANTHROPIC_CACHE_WRITE_1H_TOKENS = _PromCounter(
        "anthropic_cache_write_1h_tokens_total",
        "Anthropic prompt cache write tokens at 1h TTL (billed at 2.0x input)",
        labelnames=("provider", "model", "source"),
    )
except Exception:
    _CACHE_METRICS_ENABLED = False


def _record_cache_metrics(
    provider: str,
    model: str,
    source: Optional[str],
    cache_read: int,
    cache_creation: int,
    cache_creation_1h: int,
) -> None:
    """Emit per-source cache metrics. Cheap no-op if prometheus is unavailable."""
    if not _CACHE_METRICS_ENABLED:
        return
    src = source or "unknown"
    try:
        if cache_read:
            ANTHROPIC_CACHE_READ_TOKENS.labels(provider, model, src).inc(cache_read)
        cache_5m = max(0, cache_creation - cache_creation_1h)
        if cache_5m:
            ANTHROPIC_CACHE_WRITE_5M_TOKENS.labels(provider, model, src).inc(cache_5m)
        if cache_creation_1h:
            ANTHROPIC_CACHE_WRITE_1H_TOKENS.labels(provider, model, src).inc(cache_creation_1h)
    except Exception:
        # Metrics must never break the request path
        pass


CACHE_BREAK_MARKER = "<!-- cache_break -->"
VOLATILE_MARKER = "<!-- volatile -->"

# Anthropic prompt cache TTL blocks.
# 1h: write premium 2x, break-even when cache_read amortizes across >=3 calls
# 5m: write premium 1.25x, break-even from 1 read — correct for one-shot paths
# Ordering across breakpoints: system >= tools >= messages — monotonic non-increasing.
# Which TTL applies per call is resolved by `_ttl_block(request)` based on cache_source.
CACHE_TTL_LONG = {"type": "ephemeral", "ttl": "1h"}
CACHE_TTL_SHORT = {"type": "ephemeral"}

# Sources that amortize across many turns → worth the 1h write premium.
# Human-conversation channels where idle > 5m is common. Keep in sync with
# docs/cache-strategy.md. Unknown/unset → treated as short (fail cheap).
_LONG_CACHE_SOURCES = frozenset({
    "slack", "line", "feishu", "lark", "telegram",
    "tui", "shanclaw", "oneshot_interactive", "cache_bench",
})


def resolve_prompt_cache_ttl_block(cache_source: Optional[str]) -> Optional[Dict[str, str]]:
    """Pure function: map a cache_source string to the cache_control block.

    Public API so upstream callers (e.g. llm_service.api.agent) can resolve
    the same TTL as the provider without depending on a CompletionRequest
    object. Returns None when SHANNON_FORCE_TTL=off to suppress prompt-cache
    writes entirely.

    Precedence (matches _ttl_block):
      1. SHANNON_FORCE_TTL env (off / 5m / 1h) — operator escape hatch
      2. cache_source → _LONG_CACHE_SOURCES map → 1h
      3. Fallback → 5m (short is the safe default for unknown / internal
         subagent paths; 1.25x write premium vs 2x for 1h)
    """
    force = os.environ.get("SHANNON_FORCE_TTL", "").strip().lower()
    if force == "off":
        return None
    if force == "5m":
        return CACHE_TTL_SHORT
    if force == "1h":
        return CACHE_TTL_LONG

    src = (cache_source or "").strip().lower()
    if src in _LONG_CACHE_SOURCES:
        return CACHE_TTL_LONG
    return CACHE_TTL_SHORT


def _ttl_block(request) -> Optional[Dict[str, str]]:
    """Resolve the cache_control block for a CompletionRequest.

    Thin wrapper over resolve_prompt_cache_ttl_block so provider and upstream
    callers share one source of truth. Note: request.cache_ttl is NOT consulted
    here — that field drives manager.py's response-cache TTL, a different layer.
    """
    return resolve_prompt_cache_ttl_block(getattr(request, "cache_source", None))


# Per-session memo of the last applied rolling marker hash.
# Why: Anthropic prefix cache only matches at positions with explicit
# cache_control. Without remembering last turn's marker, the new turn's
# rolling marker writes a fresh block while the previous block becomes
# unreachable — long-session CER drops from 16.85x (3-turn) to 2.35x
# (30-turn). Preserving prev marker keeps both writes readable.
#
# Concurrency: _apply_rolling_cache_marker is currently NOT wired into
# _build_api_request (disabled by the 2026-04-15 bench regression — see
# docs/cache-strategy.md). That means this dict never sees concurrent writes
# in production today. If the preservation path is ever re-enabled, wrap
# _remember_marker / _recall_marker with an asyncio.Lock (or move the memo
# to a per-session object) before the first request hits them — FastAPI
# dispatches async handlers concurrently and OrderedDict.popitem is not
# coroutine-safe.
_PREV_ROLLING_MAX = 1000
_prev_rolling = OrderedDict()  # session_id → marker hash; LRU evicted


def _msg_stable_hash(msg: Dict[str, Any]) -> str:
    """Semantic hash of a claude_message for cross-turn marker matching.

    Why semantic instead of full JSON: clients re-serialize messages between
    turns with structural variations (content as str vs [{"type":"text",...}],
    optional fields present/absent, dict ordering). A pure JSON hash drifts
    even when the underlying message is "the same one." This hash extracts
    just (role, semantic content) — invariant to those representation shifts.

    Includes tool_use IDs and tool_result tool_use_ids since those are stable
    Anthropic-generated identifiers that uniquely tag a turn's content.
    """
    try:
        role = msg.get("role", "")
        sig = _semantic_signature(msg.get("content", ""))
        return hashlib.sha1((role + "|" + sig).encode("utf-8")).hexdigest()[:16]
    except Exception:
        return ""


def _semantic_signature(content: Any) -> str:
    """Build a normalized string signature of a message's content.
    String content -> raw text. List content -> "T:text" / "U:tool_use_id" /
    "R:tool_result_id:text" tokens joined by newline. Excludes cache_control,
    type field shape, and other structural noise.
    """
    if isinstance(content, str):
        return "S:" + content
    if isinstance(content, list):
        parts = []
        for b in content:
            if not isinstance(b, dict):
                continue
            btype = b.get("type", "")
            if btype == "text":
                parts.append("T:" + str(b.get("text", "")))
            elif btype == "tool_use":
                # Tool use ID is stable across turns (Anthropic-assigned)
                parts.append("U:" + str(b.get("id", "")))
            elif btype == "tool_result":
                tid = str(b.get("tool_use_id", ""))
                tc = b.get("content", "")
                tc_str = tc if isinstance(tc, str) else json.dumps(tc, sort_keys=True, default=str)
                parts.append("R:" + tid + ":" + tc_str)
            elif btype in ("image", "document"):
                # Hash by source URL/data presence; ignore noise like media_type ordering
                src = b.get("source", {}) if isinstance(b.get("source"), dict) else {}
                parts.append(btype[0].upper() + ":" + str(src.get("url") or src.get("data", ""))[:64])
            else:
                # Unknown block: hash whole thing (rare; keep stable as best we can)
                parts.append("X:" + json.dumps(b, sort_keys=True, default=str))
        return "\n".join(parts)
    # Fallback for unexpected shapes
    return "F:" + json.dumps(content, sort_keys=True, default=str)


def _strip_cache_control_for_hash(obj: Any) -> Any:
    """Return a deep copy of obj with all `cache_control` keys removed.
    Kept for backward-compat / debugging only — _msg_stable_hash no longer uses it."""
    if isinstance(obj, dict):
        return {k: _strip_cache_control_for_hash(v) for k, v in obj.items() if k != "cache_control"}
    if isinstance(obj, list):
        return [_strip_cache_control_for_hash(item) for item in obj]
    return obj


def _remember_marker(session_id: str, msg_hash: str) -> None:
    if not session_id or not msg_hash:
        return
    if session_id in _prev_rolling:
        _prev_rolling.move_to_end(session_id)
    _prev_rolling[session_id] = msg_hash
    while len(_prev_rolling) > _PREV_ROLLING_MAX:
        _prev_rolling.popitem(last=False)


def _recall_marker(session_id: str) -> Optional[str]:
    if not session_id:
        return None
    return _prev_rolling.get(session_id)


def _build_beta_header(
    *,
    thinking: bool,
    any_deferred: bool,
) -> Optional[str]:
    """Build comma-separated anthropic-beta header value. Returns None when no beta is needed.

    Combines interleaved-thinking-2025-05-14 (when thinking enabled) and
    advanced-tool-use-2025-11-20 (when any deferred tool is present, unless
    SHANNON_NO_ADVANCED_TOOL_USE_BETA=1 is set as an endpoint escape hatch for
    GA paths that reject the beta token).
    """
    tokens: list[str] = []
    if thinking:
        tokens.append("interleaved-thinking-2025-05-14")
    if any_deferred and os.environ.get("SHANNON_NO_ADVANCED_TOOL_USE_BETA") != "1":
        tokens.append("advanced-tool-use-2025-11-20")
    if not tokens:
        return None
    return ",".join(tokens)


class CacheBreakDetector:
    """Tracks API request state across calls to detect cache-breaking changes.

    Compares system prompt hash, tool name set, and model ID between sequential
    calls. When a change is detected, returns a dict describing what changed.
    Designed for per-provider-instance use (one detector per AnthropicProvider).
    """

    def __init__(self):
        self._prev_system_hash: str = ""
        self._prev_tool_names: list[str] = []
        self._prev_model: str = ""
        self.call_count: int = 0

    def check(
        self,
        system_text: str,
        tool_names: list[str],
        model: str,
    ) -> Optional[dict]:
        """Compare current state against previous. Returns None if no break.

        Returns dict with keys: changed (list[str]), tools_added, tools_removed,
        prev_model, new_model, system_char_delta.
        """
        self.call_count += 1

        sys_hash = hashlib.md5(system_text.encode()).hexdigest()[:12]
        sorted_names = sorted(tool_names)

        if not self._prev_system_hash:
            # First call — record state, no comparison possible
            self._prev_system_hash = sys_hash
            self._prev_tool_names = sorted_names
            self._prev_model = model
            return None

        changed = []
        result = {}

        if sys_hash != self._prev_system_hash:
            changed.append("system")
            result["system_char_delta"] = len(system_text)

        if sorted_names != self._prev_tool_names:
            changed.append("tools")
            prev_set = set(self._prev_tool_names)
            curr_set = set(sorted_names)
            result["tools_added"] = sorted(curr_set - prev_set)
            result["tools_removed"] = sorted(prev_set - curr_set)

        if model != self._prev_model:
            changed.append("model")
            result["prev_model"] = self._prev_model
            result["new_model"] = model

        # Update state
        self._prev_system_hash = sys_hash
        self._prev_tool_names = sorted_names
        self._prev_model = model

        if changed:
            result["changed"] = changed
            result["call_count"] = self.call_count
            return result
        return None


class AnthropicProvider(LLMProvider):
    """Anthropic Claude API provider implementation"""

    def __init__(self, config: Dict[str, Any]):
        # Initialize Anthropic client
        api_key = config.get("api_key") or os.getenv("ANTHROPIC_API_KEY")
        if not api_key:
            raise ValueError("Anthropic API key not provided")

        # Support custom base_url for Anthropic-compatible providers (e.g. MiniMax)
        base_url = config.get("base_url")
        self.client = AsyncAnthropic(api_key=api_key, base_url=base_url)

        # Tool schema freeze: cache converted tool schemas by name set to prevent
        # description-drift cache breaks. Legitimate schema changes take effect on
        # process restart (acceptable for Docker deployment model).
        self._frozen_tools: list = []
        self._frozen_tools_key: str = ""
        self._cache_break_detector = CacheBreakDetector()

        super().__init__(config)

    def _initialize_models(self):
        """Load Anthropic model configurations from YAML-driven config."""

        self._load_models_from_config()

    def count_tokens(self, messages: List[Dict[str, Any]], model: str) -> int:
        """
        Count tokens for Claude models.
        Note: Anthropic doesn't provide a public tokenizer, so we estimate.
        """
        # Use the base token counter for estimation
        return TokenCounter.count_messages_tokens(messages, model)

    def _convert_messages_to_claude_format(
        self,
        messages: List[Dict[str, Any]],
        ttl_block: Optional[Dict[str, str]] = CACHE_TTL_LONG,
    ) -> tuple[str, List[Dict]]:
        """Convert OpenAI-style messages to Claude format.

        ttl_block: the cache_control dict to apply at cache_break positions and
        the rolling [-2] marker. Explicit ``None`` suppresses prompt-cache writes
        entirely (used when ``SHANNON_FORCE_TTL=off``). Default ``CACHE_TTL_LONG``
        preserves legacy behavior for callers that invoke this helper directly
        (e.g. unit tests) without going through ``_build_api_request``.
        """
        system_message = ""
        claude_messages = []

        for message in messages:
            role = message["role"]
            content = message["content"]

            if role == "system":
                # Claude uses a separate system parameter (must be a string)
                system_message = extract_text_from_content(content)
            elif role == "user":
                if isinstance(content, str) and CACHE_BREAK_MARKER in content:
                    parts = content.split(CACHE_BREAK_MARKER, 1)
                    raw_stable = parts[0]
                    volatile = parts[1]
                    if raw_stable.strip():
                        stable_block: Dict[str, Any] = {"type": "text", "text": raw_stable}
                        if ttl_block is not None:
                            stable_block["cache_control"] = ttl_block
                        claude_messages.append({
                            "role": "user",
                            "content": [
                                stable_block,
                                {"type": "text", "text": volatile},
                            ]
                        })
                    else:
                        # No stable prefix — skip cache_control to avoid Anthropic 400
                        # on empty text blocks (happens when ShanClaw has no sticky context)
                        claude_messages.append({"role": "user", "content": volatile})
                else:
                    claude_messages.append({"role": "user", "content": content})
            elif role == "assistant":
                # Anthropic rejects assistant messages ending with whitespace
                if isinstance(content, str):
                    content = content.rstrip()
                # Convert OpenAI-style tool_calls field to Anthropic tool_use content blocks
                tool_calls = message.get("tool_calls")
                if tool_calls and isinstance(tool_calls, list):
                    blocks = []
                    if content:
                        text = content if isinstance(content, str) else str(content)
                        if text.strip():
                            blocks.append({"type": "text", "text": text})
                    for tc in tool_calls:
                        if not isinstance(tc, dict):
                            continue
                        # Handle OpenAI format: {"id":"...", "type":"function", "function":{"name":"...", "arguments":"..."}}
                        # and Shannon format: {"id":"...", "name":"...", "arguments":...}
                        fn = tc.get("function", {}) if tc.get("type") == "function" else tc
                        tc_id = _ensure_tool_id(tc.get("id", ""))
                        name = fn.get("name", "") if isinstance(fn, dict) else tc.get("name", "")
                        arguments = fn.get("arguments", {}) if isinstance(fn, dict) else tc.get("arguments", {})
                        if isinstance(arguments, str):
                            try:
                                arguments = json.loads(arguments)
                            except (json.JSONDecodeError, TypeError):
                                arguments = {}
                        blocks.append({
                            "type": "tool_use",
                            "id": tc_id,
                            "name": name,
                            "input": arguments,
                        })
                    claude_messages.append({"role": "assistant", "content": blocks if blocks else content})
                else:
                    claude_messages.append({"role": "assistant", "content": content})
            elif role == "tool":
                # Convert OpenAI-style tool result to Anthropic tool_result content block.
                # Content may be a string (legacy plain-text tool results) or a list of
                # content blocks (e.g. tool_reference blocks from a tool_search call).
                # Preserve list shape so Anthropic sees the structured blocks; stringify
                # only when something truly non-list/non-string slips through (defensive).
                if isinstance(content, list):
                    inner_content = content
                elif isinstance(content, str):
                    inner_content = content
                else:
                    inner_content = str(content or "")
                tool_result_block = {
                    "type": "tool_result",
                    "tool_use_id": _ensure_tool_id(message.get("tool_call_id", "")),
                    "content": inner_content,
                }
                # Merge into previous user message to maintain Anthropic's alternating role requirement
                if claude_messages and claude_messages[-1]["role"] == "user":
                    prev_content = claude_messages[-1]["content"]
                    if isinstance(prev_content, list):
                        prev_content.append(tool_result_block)
                    elif isinstance(prev_content, str):
                        claude_messages[-1]["content"] = [
                            {"type": "text", "text": prev_content},
                            tool_result_block,
                        ]
                    else:
                        claude_messages[-1]["content"] = [tool_result_block]
                else:
                    claude_messages.append({
                        "role": "user",
                        "content": [tool_result_block],
                    })
            elif role == "function":
                # Convert function results to user messages
                claude_messages.append(
                    {"role": "user", "content": f"Function result: {content}"}
                )

        # Basic rolling marker on [-2] (no prev-turn preservation here because
        # session_id isn't available at this layer). _build_api_request calls
        # _apply_rolling_cache_marker afterwards to add prev preservation when
        # a session_id is available; that call is idempotent on this marker.
        if len(claude_messages) >= 2 and ttl_block is not None:
            self._mark_last_block(claude_messages[-2], ttl_block)
        return system_message, claude_messages

    @staticmethod
    def _mark_last_block(msg: Dict[str, Any], ttl_block: Dict[str, str]) -> bool:
        """Add cache_control to the last block of msg. Returns True if applied.
        No-op if any block already has cache_control (de-dup)."""
        content = msg.get("content")
        if isinstance(content, str):
            msg["content"] = [{"type": "text", "text": content, "cache_control": ttl_block}]
            return True
        if isinstance(content, list) and content:
            already = any(isinstance(b, dict) and b.get("cache_control") for b in content)
            if already:
                return False
            last_block = content[-1]
            if isinstance(last_block, dict):
                last_block["cache_control"] = ttl_block
                return True
        return False

    @staticmethod
    def _strip_message_cache_control(msg: Dict[str, Any]) -> None:
        """Remove cache_control from all blocks in msg (used to free a breakpoint
        slot when promoting prev_rolling marker — user_1's bytes remain readable
        as part of prev_rolling's larger cached prefix, no info loss)."""
        content = msg.get("content")
        if isinstance(content, list):
            for block in content:
                if isinstance(block, dict):
                    block.pop("cache_control", None)

    def _apply_rolling_cache_marker(
        self,
        claude_messages: List[Dict[str, Any]],
        session_id: Optional[str],
        ttl_block: Optional[Dict[str, str]] = CACHE_TTL_LONG,
    ) -> None:
        """Apply rolling cache_control marker on penultimate message; when a
        previous turn's marker is still present in messages, also preserve it.

        Why preserve prev marker: Anthropic prefix cache only matches at positions
        with explicit cache_control. Without preserving prev marker, every turn
        rewrites a fresh rolling block while the previous rolling cache becomes
        unreachable. Long-session bench observed CHR 86%->68%, CER 16.85x->2.35x.

        Breakpoint accounting (Anthropic cap = 4):
        - Always: system (1) + tools (1) = 2 reserved
        - Without prev_rolling: user_1 (cache_break) + rolling@-2 = 4 ✓
        - With prev_rolling preserved: prev_rolling + rolling@-2 = 4
          (user_1's cache_control stripped — its bytes are inside prev_rolling's
          cached prefix, readable for free without an extra breakpoint)
        """
        if ttl_block is None:
            return
        if len(claude_messages) < 2:
            return

        target_idx = len(claude_messages) - 2
        target = claude_messages[target_idx]

        # Locate prev marker by hash (None if first turn or compaction event).
        prev_idx: Optional[int] = None
        if session_id:
            prev_hash = _recall_marker(session_id)
            if prev_hash:
                for i, msg in enumerate(claude_messages[:target_idx]):
                    if _msg_stable_hash(msg) == prev_hash:
                        prev_idx = i
                        break

        # Apply current rolling marker on [-2] (existing behavior).
        self._mark_last_block(target, ttl_block)

        # Preserve prev marker: free a breakpoint by stripping earlier
        # cache_control (user_1), then mark the prev message.
        if prev_idx is not None and prev_idx < target_idx:
            for i in range(prev_idx):
                self._strip_message_cache_control(claude_messages[i])
            self._mark_last_block(claude_messages[prev_idx], ttl_block)
            logger.debug(
                "rolling cache: preserved prev marker at idx=%d (target=%d, session=%s)",
                prev_idx, target_idx, (session_id or "")[:12],
            )

        # Memo current target hash for next turn's lookup.
        if session_id:
            _remember_marker(session_id, _msg_stable_hash(target))

    def _split_system_message(
        self,
        system_message: str,
        ttl_block: Optional[Dict[str, str]] = CACHE_TTL_LONG,
    ) -> list[dict]:
        """Split system message at <!-- volatile --> marker.

        Returns a list of content blocks for the Anthropic API 'system' parameter.
        The stable prefix (before marker) gets cache_control if ttl_block is
        provided; volatile suffix never does. If no marker is present, returns
        a single (cached iff ttl_block is non-None) block.

        Monotonic TTL ordering across breakpoints is preserved because all
        cache_control blocks in a single request share the same ttl_block.
        """
        if VOLATILE_MARKER in system_message:
            stable, volatile = system_message.split(VOLATILE_MARKER, 1)
            blocks: list[dict] = []
            if stable.strip():
                stable_block: Dict[str, Any] = {"type": "text", "text": stable.strip()}
                if ttl_block is not None:
                    stable_block["cache_control"] = ttl_block
                blocks.append(stable_block)
            if volatile.strip():
                blocks.append({"type": "text", "text": volatile.strip()})
            if blocks:
                return blocks
        full_block: Dict[str, Any] = {"type": "text", "text": system_message}
        if ttl_block is not None:
            full_block["cache_control"] = ttl_block
        return [full_block]

    def _convert_functions_to_tools(self, functions: List[Dict]) -> List[Dict]:
        """Convert OpenAI function format to Claude tools format.

        Frozen by tool name set: same names → return cached schemas (prevents
        description-drift cache breaks). Different name set → rebuild.
        """
        # Cache key: sorted (name, defer_loading) tuples. Including defer flag in the
        # key ensures the frozen cache rebuilds when a tool toggles between deferred
        # and non-deferred modes across sessions.
        key = str(sorted(
            (
                (f.get("name") or (f.get("function") or {}).get("name", "") or ""),
                bool(
                    f.get("defer_loading")
                    or (f.get("function") or {}).get("defer_loading")
                ),
            )
            for f in functions
        ))

        if self._frozen_tools and self._frozen_tools_key == key:
            return [dict(t) for t in self._frozen_tools]

        tools = []
        for func in functions:
            # Handle both OpenAI format ({"type": "function", "function": {...}})
            # and direct function schema format ({"name": "...", ...})
            if func.get("type") == "function" and "function" in func:
                func = func["function"]

            # Skip if function schema doesn't have required 'name' field
            if "name" not in func:
                continue

            tool = {
                "name": func["name"],
                "description": func.get("description", ""),
                "input_schema": {
                    "type": "object",
                    "properties": func.get("parameters", {}).get("properties", {}),
                    "required": func.get("parameters", {}).get("required", []),
                },
            }
            # Task 3.1: Anthropic strips defer_loading:true tools from the prefix-hash
            # before caching, so they don't invalidate the tools cache_control breakpoint
            # even as the deferred set grows across sessions. Omit the field entirely
            # when False so absence == disabled (avoids wire-format drift).
            if func.get("defer_loading") is True:
                tool["defer_loading"] = True
            tools.append(tool)
        # Sort by name for cache prefix stability across requests
        tools.sort(key=lambda t: t["name"])

        self._frozen_tools = tools
        self._frozen_tools_key = key
        return [dict(t) for t in tools]

    def reset_tool_cache(self):
        """Clear frozen tool schemas. Call on explicit tool set change."""
        self._frozen_tools = []
        self._frozen_tools_key = ""



    def _convert_attachments_for_anthropic(self, messages: List[Dict]) -> List[Dict]:
        """Convert shannon_attachment blocks in user messages to Anthropic-native format.

        shannon_attachment is a Shannon-internal marker that must never be sent raw
        to any LLM API. This converts them to Anthropic image/document blocks.
        """
        for msg in messages:
            content = msg.get("content")
            if isinstance(content, list):
                msg["content"] = [
                    self._shannon_att_to_anthropic(b)
                    if isinstance(b, dict) and b.get("type") == "shannon_attachment"
                    else b
                    for b in content
                ]
        return messages

    @staticmethod
    def _shannon_att_to_anthropic(block: Dict) -> Dict:
        """Convert a single shannon_attachment block to Anthropic format."""
        media_type = block["media_type"]
        # URL-based: route by media type
        if block.get("source") == "url":
            if media_type == "application/pdf":
                # Anthropic doesn't support URL-based PDF documents; degrade to text
                return {"type": "text", "text": f"[PDF document: {block['url']}]"}
            return {
                "type": "image",
                "source": {"type": "url", "url": block["url"]},
            }
        if media_type == "application/pdf":
            return {
                "type": "document",
                "source": {
                    "type": "base64",
                    "media_type": media_type,
                    "data": block["data"],
                },
            }
        if not media_type.startswith("image/"):
            # Unsupported binary: degrade to text
            fname = block.get("filename", "file")
            return {"type": "text", "text": f"[Unsupported attachment: {fname} ({media_type})]"}
        return {
            "type": "image",
            "source": {
                "type": "base64",
                "media_type": media_type,
                "data": block["data"],
            },
        }

    def _build_api_request(self, request: CompletionRequest, model_config) -> Dict[str, Any]:
        """Build the Anthropic API request dict shared by complete() and stream_complete().

        Handles: message conversion, context window headroom, temperature/top_p
        mutual exclusivity, system message, stop sequences, tools, and output_config.
        """
        model = model_config.model_id

        # Resolve prompt-cache TTL once for the whole request. All cache_control
        # blocks below share this single value to keep Anthropic's monotonic
        # TTL-ordering invariant (system >= tools >= messages) trivially satisfied.
        ttl_block = _ttl_block(request)

        # Convert messages to Claude format
        system_message, claude_messages = self._convert_messages_to_claude_format(
            request.messages, ttl_block
        )

        # Convert shannon_attachment blocks to Anthropic-native format
        claude_messages = self._convert_attachments_for_anthropic(claude_messages)

        # Rolling cache_control marker on [-2] is applied inside
        # _convert_messages_to_claude_format. Cross-turn prev_marker
        # preservation (_apply_rolling_cache_marker) is intentionally NOT
        # called here. Empirically (bench 2026-04-15 Phase 3 attempt):
        # re-enabling it regressed 30-turn CHR from 93% → 61% and CER
        # 15.6x → 4.0x, tripling uncached input tokens. Root cause: the
        # preservation path calls _strip_message_cache_control on user_1 to
        # free a breakpoint slot, but stripping cache_control mutates the
        # block's byte representation on the wire. Even though the
        # non-cache_control content is identical, Anthropic's prefix match
        # appears to break at that boundary — so the "free" cached prefix
        # up to msg[prev_idx] no longer matches, and every turn falls back
        # to writing fresh cache. Single rolling marker on [-2] (applied by
        # _convert_messages_to_claude_format already) gives us the observed
        # 93% CHR / 15.6x CER on 30-turn sessions, which is optimal under
        # the public API's 4-breakpoint cap. To revive prev_marker safely,
        # we'd need either an Anthropic API change (treat cache_control as
        # cache-key-insensitive) or a byte-rewriting scheme that preserves
        # exact bytes while freeing a slot — neither is available today.

        # Compute safe max_tokens based on context window headroom (OpenAI-style)
        prompt_tokens_est = self.count_tokens(request.messages, model)
        safety_margin = 256
        model_context = getattr(model_config, "context_window", 200000)
        model_max_output = getattr(model_config, "max_tokens", 8192)
        requested_max = int(request.max_tokens) if request.max_tokens else model_max_output
        headroom = model_context - prompt_tokens_est - safety_margin

        # Check if there's sufficient context window headroom
        if headroom <= 0:
            raise ValueError(
                f"Insufficient context window: prompt uses ~{prompt_tokens_est} tokens, "
                f"max context is {model_context}, leaving no room for output. "
                f"Please reduce prompt length."
            )

        adjusted_max = min(requested_max, model_max_output, headroom)

        # SDK 0.64.0+ rejects non-streaming requests that may take >10 minutes.
        # Cap max_tokens to avoid this for non-streaming complete() calls.
        # Haiku ~800 tok/s → 8192 tokens ≈ 10s. 16384 is safe for all models.
        MAX_NON_STREAMING = 16384
        if adjusted_max > MAX_NON_STREAMING and not request.stream:
            logger.info(f"Capping max_tokens {adjusted_max} → {MAX_NON_STREAMING} for non-streaming Anthropic call")
            adjusted_max = MAX_NON_STREAMING

        api_request: Dict[str, Any] = {
            "model": model,
            "messages": claude_messages,
            "max_tokens": adjusted_max,
        }

        # Anthropic API requires temperature and top_p to be mutually exclusive.
        # Note: `0.0` is a valid temperature; do not use truthiness checks here.
        if request.temperature is not None and request.top_p is not None:
            # Prefer temperature when both are present.
            api_request["temperature"] = request.temperature
            logger.warning(
                "Anthropic API: both temperature and top_p were set; "
                "using temperature and ignoring top_p"
            )
        elif request.temperature is not None:
            api_request["temperature"] = request.temperature
        elif request.top_p is not None:
            api_request["top_p"] = request.top_p
        # If neither is set, omit both and let the API defaults apply.

        if system_message:
            api_request["system"] = self._split_system_message(system_message, ttl_block)

        if request.stop:
            api_request["stop_sequences"] = request.stop

        # Handle functions/tools
        if request.functions and model_config.supports_functions:
            tools = self._convert_functions_to_tools(request.functions)
            if tools and ttl_block is not None:
                tools[-1]["cache_control"] = ttl_block
            api_request["tools"] = tools

            # Handle function calling / tool_choice
            if request.function_call:
                if isinstance(request.function_call, str):
                    if request.function_call == "auto":
                        api_request["tool_choice"] = {"type": "auto"}
                    elif request.function_call == "any":
                        # Force model to use at least one tool
                        api_request["tool_choice"] = {"type": "any"}
                    elif request.function_call == "none":
                        api_request["tool_choice"] = {"type": "none"}
                elif isinstance(request.function_call, dict):
                    api_request["tool_choice"] = {
                        "type": "tool",
                        "name": request.function_call.get("name"),
                    }

        # NOTE: Automatic caching (top-level cache_control) tested extensively but
        # NET NEGATIVE for swarm: 25% write premium on every call, but parallel agents
        # constantly change shared state (team roster, task board, workspace) → prefix
        # breaks between nearly every call → ~7% hit rate → net -17% cost increase.
        # Explicit system message cache_control (line 190) is sufficient: Sonnet/Lead
        # (>1024 threshold, ~8K stable system prompt) benefits; Haiku agents (<4096
        # threshold) silently skip without paying write premium.

        # Structured outputs: inject output_config for constrained JSON decoding.
        # SDK <0.42 doesn't have native output_config param; pass via extra_body.
        if request.output_config:
            api_request["extra_body"] = {"output_config": request.output_config}
            schema_keys = list(request.output_config.get("format", {}).get("schema", {}).get("properties", {}).keys())
            logger.info(f"Anthropic structured output enabled: schema keys={schema_keys}")

        # Extended thinking: force temperature=1 and pass config via extra_body
        # (SDK 0.40.0 doesn't accept 'thinking' as a named kwarg in stream())
        if request.thinking:
            thinking_config = dict(request.thinking)
            # Adaptive thinking only supported on Opus models.
            # For other models, convert adaptive → enabled with a budget.
            if thinking_config.get("type") == "adaptive" and "opus" not in model.lower():
                thinking_config = {"type": "enabled", "budget_tokens": thinking_config.get("budget_tokens", 10000)}
                logger.info(f"Converted adaptive thinking to enabled (budget=10000) for model {model}")
            api_request["temperature"] = 1
            api_request.pop("top_p", None)
            extra = api_request.get("extra_body", {})
            extra["thinking"] = thinking_config
            api_request["extra_body"] = extra

        # Cache break detection
        system_text = system_message or ""
        tool_names = [t["name"] for t in api_request.get("tools", [])]
        break_info = self._cache_break_detector.check(
            system_text=system_text,
            tool_names=tool_names,
            model=model,
        )
        if break_info:
            parts = [f"Cache break detected (call #{break_info['call_count']}): changed={break_info['changed']}"]
            if "tools_added" in break_info:
                parts.append(f"tools_added={break_info['tools_added']}")
            if "tools_removed" in break_info:
                parts.append(f"tools_removed={break_info['tools_removed']}")
            if "model" in break_info.get("changed", []):
                parts.append(f"model={break_info.get('prev_model', '')}→{break_info.get('new_model', '')}")
            logger.warning(" ".join(parts))

        # Defensive: upstream callers (agent.py agent loop, history replay)
        # inject cache_control with a hardcoded TTL that may not match the
        # one resolved for this request's cache_source. Anthropic requires
        # TTLs to be monotonic non-increasing across tools → system → messages.
        # Force every existing cache_control to the single resolved ttl_block
        # (or strip them entirely when SHANNON_FORCE_TTL=off).
        self._force_uniform_cache_ttl(api_request, ttl_block)

        return api_request

    @staticmethod
    def _force_uniform_cache_ttl(api_request: Dict[str, Any], ttl_block: Optional[Dict[str, str]]) -> None:
        """Boundary invariant: enforce a single uniform TTL on every cache_control
        block before the request leaves the provider.

        Behavior — note this IS a semantic override, not a merge:
          - ttl_block is a dict: every existing cache_control value is REPLACED
            with ttl_block, including any values an upstream caller explicitly
            set. This is intentional: Anthropic requires TTL monotonic-non-
            increasing across tools → system → messages, and the current design
            contract is "single resolved TTL per request" so that the invariant
            is trivially satisfied at every breakpoint. Heterogeneous TTL
            layouts (e.g. 1h prefix + 5m tail) are not a supported path today;
            if a future design requires them, this guard must be revisited.
          - ttl_block is None (SHANNON_FORCE_TTL=off): every cache_control is
            stripped so no prompt-cache writes happen.

        Emits a debug log of how many blocks were modified. Persistent non-zero
        counts for internal cache_sources signal an upstream caller (e.g. the
        hardcoded CACHE_TTL_LONG injections in agent.py) whose TTL disagrees
        with this request's resolved ttl_block — file a follow-up.
        """
        modified = 0

        def _visit(container: Any) -> None:
            nonlocal modified
            if isinstance(container, dict):
                # cache_control always sits directly on content-block dicts
                # (tools, system blocks, message content blocks); there's no
                # need to recurse into other dict values for Anthropic's schema.
                if "cache_control" in container:
                    if ttl_block is None:
                        container.pop("cache_control", None)
                        modified += 1
                    elif container["cache_control"] != ttl_block:
                        container["cache_control"] = ttl_block
                        modified += 1
            elif isinstance(container, list):
                for item in container:
                    _visit(item)

        for t in api_request.get("tools", []) or []:
            _visit(t)
        _visit(api_request.get("system"))
        for m in api_request.get("messages", []) or []:
            _visit(m.get("content"))

        if modified:
            logger.debug(
                "cache_control uniform-TTL guard modified %d block(s) (ttl_block=%r)",
                modified, ttl_block,
            )

    async def complete(self, request: CompletionRequest) -> CompletionResponse:
        """Generate a completion using Anthropic API"""

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model = model_config.model_id

        api_request = self._build_api_request(request, model_config)

        # Make API call
        start_time = time.time()

        try:
            create_kwargs = dict(api_request)
            beta = _build_beta_header(
                thinking=bool(request.thinking),
                any_deferred=any(t.get("defer_loading") for t in api_request.get("tools", [])),
            )
            if beta:
                create_kwargs["extra_headers"] = {"anthropic-beta": beta}
            response = await self.client.messages.create(**create_kwargs)
        except anthropic.APIError as e:
            raise Exception(f"Anthropic API error: {e}")

        latency_ms = int((time.time() - start_time) * 1000)

        # Extract response content
        content = ""
        function_calls = []
        # Verbatim ordered copy of the assistant message's content blocks.
        # Preserved in original index order so Anthropic's "thinking blocks
        # must be unmodified across the assistant trajectory" requirement
        # is satisfied — including interleaved thinking that appears between
        # consecutive tool_use blocks.
        content_blocks_out: List[Dict[str, Any]] = []

        for content_block in response.content:
            # Handle both Anthropic SDK objects (.type attr) and plain dicts (MiniMax compat)
            block_type = getattr(content_block, "type", None) or (content_block.get("type") if isinstance(content_block, dict) else None)

            # Verbatim copy: try the SDK's own serializer first (preserves
            # forward-compat fields like new "redacted_thinking" subtypes
            # without us needing to know about them); fall back to per-type
            # extraction only when no native serializer is available.
            block_dict = _block_to_dict(content_block, block_type)
            if block_dict is not None:
                content_blocks_out.append(block_dict)

            # Populate legacy flat fields alongside for older clients.
            if block_type == "text":
                content = getattr(content_block, "text", None) or (content_block.get("text", "") if isinstance(content_block, dict) else "")
            elif block_type == "tool_use":
                block_id = getattr(content_block, "id", None) or (content_block.get("id") if isinstance(content_block, dict) else None)
                block_name = getattr(content_block, "name", None) or (content_block.get("name") if isinstance(content_block, dict) else None)
                block_input = getattr(content_block, "input", None) or (content_block.get("input") if isinstance(content_block, dict) else None)
                function_calls.append({
                    "id": block_id,
                    "name": block_name,
                    "arguments": block_input,
                })

        function_call = function_calls[0] if function_calls else None

        # Get token usage — handle both SDK objects and dicts (MiniMax compat)
        usage = response.usage
        if isinstance(usage, dict):
            output_tokens = usage.get("output_tokens", 0)
            input_tokens = usage.get("input_tokens", 0)
            cache_read = usage.get("cache_read_input_tokens", 0) or 0
            cache_creation = usage.get("cache_creation_input_tokens", 0) or 0
        else:
            output_tokens = usage.output_tokens
            input_tokens = usage.input_tokens
            cache_read = getattr(usage, "cache_read_input_tokens", 0) or 0
            cache_creation = getattr(usage, "cache_creation_input_tokens", 0) or 0
        # Extract per-TTL cache creation breakdown (1h vs 5min)
        cache_creation_1h = 0
        cache_creation_5m = 0
        if isinstance(usage, dict):
            cc = usage.get("cache_creation")
            if isinstance(cc, dict):
                cache_creation_1h = cc.get("ephemeral_1h_input_tokens", 0) or 0
                cache_creation_5m = cc.get("ephemeral_5m_input_tokens", 0) or 0
        else:
            cc = getattr(usage, "cache_creation", None)
            if cc is not None:
                cache_creation_1h = getattr(cc, "ephemeral_1h_input_tokens", 0) or 0
                cache_creation_5m = getattr(cc, "ephemeral_5m_input_tokens", 0) or 0

        total_tokens = input_tokens + output_tokens
        if cache_read > 0 or cache_creation > 0:
            logger.info(f"Anthropic prompt cache: read={cache_read}, creation={cache_creation} (5m={cache_creation_5m}, 1h={cache_creation_1h}), input={input_tokens}")
        logger.info(f"Anthropic complete: model={model}, structured_output={bool(request.output_config)}, input={input_tokens}, output={output_tokens}")

        # Calculate cost (including prompt cache pricing)
        cost = self.estimate_cost(
            input_tokens, output_tokens, model,
            cache_read_tokens=cache_read, cache_creation_tokens=cache_creation,
            cache_creation_1h_tokens=cache_creation_1h,
        )

        _record_cache_metrics(
            self.config.get("name", "anthropic"),
            model,
            request.cache_source,
            cache_read,
            cache_creation,
            cache_creation_1h,
        )

        # Build response
        return CompletionResponse(
            content=content,
            model=model,
            provider=self.config.get("name", "anthropic"),
            usage=TokenUsage(
                input_tokens=input_tokens,
                output_tokens=output_tokens,
                total_tokens=total_tokens,
                estimated_cost=cost,
                cache_read_tokens=cache_read,
                cache_creation_tokens=cache_creation,
                cache_creation_5m_tokens=cache_creation_5m,
                cache_creation_1h_tokens=cache_creation_1h,
                call_sequence=self._cache_break_detector.call_count,
            ),
            finish_reason=response.stop_reason or "stop",
            function_call=function_call,
            tool_calls=function_calls if function_calls else None,
            content_blocks=content_blocks_out or None,
            request_id=response.id,
            latency_ms=latency_ms,
        )

    async def stream_complete(self, request: CompletionRequest) -> AsyncIterator[str]:
        """Stream a completion using Anthropic API"""

        # Select model based on tier or explicit override
        model_config = self.resolve_model_config(request)
        model = model_config.model_id

        api_request = self._build_api_request(request, model_config)

        # Make streaming API call
        try:
            stream_kwargs = dict(api_request)
            beta = _build_beta_header(
                thinking=bool(request.thinking),
                any_deferred=any(t.get("defer_loading") for t in api_request.get("tools", [])),
            )
            if beta:
                stream_kwargs["extra_headers"] = {"anthropic-beta": beta}
            async with self.client.messages.stream(**stream_kwargs) as stream:
                async for text in stream.text_stream:
                    yield text

                # After streaming completes, get the final message with usage and tool calls
                final_message = await stream.get_final_message()

                # Extract tool_use AND content_blocks (parity with non-stream
                # complete(): both are required for the round-trip — function
                # calls drive tool execution, content_blocks drive the verbatim
                # assistant trajectory including thinking blocks the client
                # echoes back on the next turn).
                function_calls = []
                content_blocks_out: List[Dict[str, Any]] = []
                if final_message and hasattr(final_message, "content"):
                    for content_block in final_message.content:
                        block_type = getattr(content_block, "type", None) or (
                            content_block.get("type") if isinstance(content_block, dict) else None
                        )
                        # Verbatim block copy — preserves order including
                        # interleaved thinking. Unknown types return None and
                        # are skipped (don't emit a malformed shape).
                        bdict = _block_to_dict(content_block, block_type)
                        if bdict is not None:
                            content_blocks_out.append(bdict)
                        # Legacy tool_calls list for backward-compat clients.
                        if block_type != "tool_use":
                            continue
                        block_id = getattr(content_block, "id", None) or (
                            content_block.get("id") if isinstance(content_block, dict) else None
                        )
                        block_name = getattr(content_block, "name", None) or (
                            content_block.get("name") if isinstance(content_block, dict) else None
                        )
                        block_input = getattr(content_block, "input", None) or (
                            content_block.get("input") if isinstance(content_block, dict) else None
                        )
                        function_calls.append({
                            "id": block_id,
                            "name": block_name,
                            "arguments": block_input,
                        })

                if final_message and hasattr(final_message, "usage"):
                    # Handle both SDK objects and dicts (MiniMax / Anthropic-compat
                    # provider parity with the non-stream complete() path above).
                    usage = final_message.usage
                    if isinstance(usage, dict):
                        input_tokens = usage.get("input_tokens", 0) or 0
                        output_tokens = usage.get("output_tokens", 0) or 0
                        cache_read = usage.get("cache_read_input_tokens", 0) or 0
                        cache_creation = usage.get("cache_creation_input_tokens", 0) or 0
                        cc = usage.get("cache_creation")
                        cache_creation_1h = (
                            cc.get("ephemeral_1h_input_tokens", 0) or 0
                            if isinstance(cc, dict) else 0
                        )
                        cache_creation_5m = (
                            cc.get("ephemeral_5m_input_tokens", 0) or 0
                            if isinstance(cc, dict) else 0
                        )
                    else:
                        input_tokens = usage.input_tokens
                        output_tokens = usage.output_tokens
                        cache_read = getattr(usage, "cache_read_input_tokens", 0) or 0
                        cache_creation = getattr(usage, "cache_creation_input_tokens", 0) or 0
                        # Per-TTL cache creation breakdown (1h vs 5min). Without
                        # this the 1h TTL slice silently drops from cost accounting.
                        cache_creation_1h = 0
                        cache_creation_5m = 0
                        cc = getattr(usage, "cache_creation", None)
                        if cc is not None:
                            cache_creation_1h = getattr(cc, "ephemeral_1h_input_tokens", 0) or 0
                            cache_creation_5m = getattr(cc, "ephemeral_5m_input_tokens", 0) or 0
                    cost = self.estimate_cost(
                        input_tokens,
                        output_tokens,
                        model,
                        cache_read_tokens=cache_read,
                        cache_creation_tokens=cache_creation,
                        cache_creation_1h_tokens=cache_creation_1h,
                    )
                    _record_cache_metrics(
                        self.config.get("name", "anthropic"),
                        model,
                        request.cache_source,
                        cache_read,
                        cache_creation,
                        cache_creation_1h,
                    )
                    result = {
                        "usage": {
                            "total_tokens": input_tokens + output_tokens,
                            "input_tokens": input_tokens,
                            "output_tokens": output_tokens,
                            "cache_read_tokens": cache_read,
                            "cache_creation_tokens": cache_creation,
                            "cache_creation_5m_tokens": cache_creation_5m,
                            "cache_creation_1h_tokens": cache_creation_1h,
                            "cost_usd": cost,
                            "call_sequence": self._cache_break_detector.call_count,
                        },
                        "model": final_message.model,
                        "provider": self.config.get("name", "anthropic"),
                        "finish_reason": final_message.stop_reason or "stop",
                    }
                    if function_calls:
                        result["function_call"] = function_calls[0]
                        result["function_calls"] = function_calls
                    if content_blocks_out:
                        result["content_blocks"] = content_blocks_out
                    yield result

        except anthropic.APIError as e:
            raise Exception(f"Anthropic API error: {e}")
