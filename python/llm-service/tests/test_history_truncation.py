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
