"""Tests for tool-type-aware history truncation in build_agent_messages.

Root cause: HISTORY_RESULT_MAX = 2000 truncates ALL tool results uniformly.
file_read results (agent's working memory) get destroyed between iterations,
forcing agents to waste 9 of 15 iterations re-reading the same file.

Fix: file_read/file_edit results should preserve much more content (15K),
while web_search/web_fetch keep moderate truncation (3000).
"""

import pytest

from llm_service.api.agent import (
    AgentLoopStepRequest,
    AgentLoopTurn,
    build_agent_messages,
)


def _make_request(**kwargs) -> AgentLoopStepRequest:
    defaults = dict(
        agent_id="Akiba",
        task="Analyze React vs Vue",
        iteration=3,
        is_swarm=True,
        role="researcher",
        suggested_tools=["web_search", "file_read", "file_write"],
    )
    defaults.update(kwargs)
    return AgentLoopStepRequest(**defaults)


class TestHistoryTruncation:
    """History truncation must be tool-type-aware."""

    def test_file_read_result_not_truncated_at_2000(self):
        """file_read results should NOT be truncated to 2000 chars.

        A 5000-char file_read result is the agent's working memory — truncating
        it forces the agent to re-read the file on the next iteration.
        """
        file_content = "# Report\n" + "x" * 5000  # 5010 chars total
        history = [
            AgentLoopTurn(
                iteration=1,
                action="tool_call:file_read",
                result=file_content,
            ),
        ]
        msgs = build_agent_messages(_make_request(history=history))
        user_msg = msgs[1]["content"]
        # The full file_read result (5010 chars) must be preserved, not cut to 2000
        assert "x" * 4000 in user_msg, \
            "file_read result was truncated — agent loses working memory"

    def test_web_search_result_still_truncated(self):
        """web_search results should still be truncated to moderate size."""
        search_content = "Search result: " + "y" * 10000  # 10015 chars
        history = [
            AgentLoopTurn(
                iteration=1,
                action="tool_call:web_search",
                result=search_content,
            ),
        ]
        msgs = build_agent_messages(_make_request(history=history))
        user_msg = msgs[1]["content"]
        # web_search result should NOT have all 10000 chars
        assert "y" * 8000 not in user_msg, \
            "web_search result should be truncated, not kept in full"

    def test_file_edit_result_preserved(self):
        """file_edit results should also be preserved like file_read."""
        edit_result = "Edited file: " + "z" * 5000
        history = [
            AgentLoopTurn(
                iteration=1,
                action="tool_call:file_edit",
                result=edit_result,
            ),
        ]
        msgs = build_agent_messages(_make_request(history=history))
        user_msg = msgs[1]["content"]
        assert "z" * 4000 in user_msg, \
            "file_edit result was truncated — agent loses context of its own edits"

    def test_mixed_history_selective_truncation(self):
        """In mixed history, file ops keep full content while web ops truncate."""
        history = [
            AgentLoopTurn(
                iteration=1,
                action="tool_call:web_search",
                result="W" * 6000,
            ),
            AgentLoopTurn(
                iteration=2,
                action="tool_call:file_read",
                result="F" * 6000,
            ),
        ]
        msgs = build_agent_messages(_make_request(history=history, iteration=3))
        user_msg = msgs[1]["content"]
        # file_read: 6000 chars should be preserved
        assert "F" * 5000 in user_msg, "file_read result truncated in mixed history"
        # web_search: 6000 chars should be truncated
        assert "W" * 5000 not in user_msg, "web_search result not truncated in mixed history"


class TestPromptCharBudget:
    """Module-level invariants and trim correctness on list-form content."""

    def test_max_text_total_under_max_prompt_chars(self):
        """Alignment invariant: the text-attachment budget must leave room
        for the user's actual query. If MAX_TEXT_TOTAL ever creeps past
        MAX_PROMPT_CHARS, the (MAX_PROMPT_CHARS - attachment_text_chars)
        computation in query/history trim paths goes negative and clamps to
        the 1_000-char floor — silently truncating every prompt to 1 KB.
        """
        from llm_service.api.agent import (
            MAX_PROMPT_CHARS,
            MAX_TEXT_PER_FILE,
            MAX_TEXT_TOTAL,
        )

        assert MAX_TEXT_TOTAL < MAX_PROMPT_CHARS, (
            f"MAX_TEXT_TOTAL ({MAX_TEXT_TOTAL}) must stay under "
            f"MAX_PROMPT_CHARS ({MAX_PROMPT_CHARS})"
        )
        # Per-file cap should never exceed the total budget — otherwise a
        # single file can monopolize the entire text-attachment quota.
        assert MAX_TEXT_PER_FILE <= MAX_TEXT_TOTAL, (
            f"MAX_TEXT_PER_FILE ({MAX_TEXT_PER_FILE}) cannot exceed "
            f"MAX_TEXT_TOTAL ({MAX_TEXT_TOTAL})"
        )

    def test_multi_turn_trim_sees_list_form_observations(self):
        """`_build_multi_turn_messages` puts historical observations into
        list-form content blocks (`{"role":"user", "content":[{type:"text", text:...}]}`).
        Before the fix, the trim loop's `total_chars` sum only counted string
        content, so 10 turns × 150K chars of list-form observations stayed
        invisible — the prompt could balloon past MAX_PROMPT_CHARS with
        zero trim. After the fix, list-form text blocks are counted and
        oldest turn pairs evict until total falls under cap.
        """
        from llm_service.api.agent import (
            MAX_PROMPT_CHARS,
            _build_multi_turn_messages,
        )

        big_obs = "x" * 150_000  # ~150K chars per frozen observation
        history = [
            AgentLoopTurn(
                iteration=i,
                action=f"tool_call:web_search_{i}",
                result="",
                assistant_replay='{"action":"step"}',
                observation_text=big_obs,
            )
            for i in range(10)
        ]
        body = _make_request(history=history, iteration=11)
        msgs = _build_multi_turn_messages(body, system_prompt="sys", cache_source="agent_loop")

        # Count chars the same way the new measurement helper does so the
        # assertion exercises both the trim AND its accounting.
        def measure(content):
            if isinstance(content, str):
                return len(content)
            if isinstance(content, list):
                return sum(
                    len(b.get("text", ""))
                    for b in content
                    if isinstance(b, dict) and b.get("type") == "text"
                )
            return 0

        total = sum(measure(m["content"]) for m in msgs)
        # With trim working, 10 × 150K = 1.5M raw drops near MAX_PROMPT_CHARS
        # after eviction. The loop also keeps a floor (len(messages) > 5), so
        # not every turn is guaranteed to evict — but total must come in
        # well under the pre-fix 1.5M figure.
        assert total <= MAX_PROMPT_CHARS + 200_000, (
            f"After trim, total_chars={total:,} should be near "
            f"MAX_PROMPT_CHARS={MAX_PROMPT_CHARS:,} (pre-fix would stay near 1.5M)"
        )
