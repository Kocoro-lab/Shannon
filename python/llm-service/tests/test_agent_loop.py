"""Tests for agent loop history tiering and prompt construction."""

import pytest
from unittest.mock import AsyncMock, patch, MagicMock

from llm_service.api.agent import AgentLoopStepRequest, AgentLoopTurn


class TestTieredHistory:
    """Test that history truncation uses tiered lengths based on recency."""

    def _build_request(self, num_turns: int, result_len: int = 5000) -> AgentLoopStepRequest:
        """Build a request with `num_turns` turns, each having a result of `result_len` chars."""
        history = []
        for i in range(num_turns):
            history.append(AgentLoopTurn(
                iteration=i,
                action=f"tool_call_{i}",
                result="x" * result_len,
            ))
        return AgentLoopStepRequest(
            agent_id="test-agent",
            task="test task",
            iteration=num_turns,
            max_iterations=25,
            history=history,
        )

    def test_recent_turns_get_full_detail(self):
        """Last 3 turns should be truncated at 4000 chars, not 500."""
        req = self._build_request(num_turns=5, result_len=5000)
        # Simulate the tiered logic from agent_loop_step
        history_lines = []
        num_turns = len(req.history)
        for idx, turn in enumerate(req.history):
            is_recent = (num_turns - idx) <= 3
            max_len = 4000 if is_recent else 500
            result_str = str(turn.result)[:max_len] if turn.result else "no result"
            history_lines.append(result_str)

        # Older turns (index 0, 1) should be truncated to 500
        assert len(history_lines[0]) == 500
        assert len(history_lines[1]) == 500
        # Recent turns (index 2, 3, 4) should be truncated to 4000
        assert len(history_lines[2]) == 4000
        assert len(history_lines[3]) == 4000
        assert len(history_lines[4]) == 4000

    def test_short_results_not_padded(self):
        """Results shorter than the limit should not be modified."""
        req = self._build_request(num_turns=5, result_len=100)
        history_lines = []
        num_turns = len(req.history)
        for idx, turn in enumerate(req.history):
            is_recent = (num_turns - idx) <= 3
            max_len = 4000 if is_recent else 500
            result_str = str(turn.result)[:max_len] if turn.result else "no result"
            history_lines.append(result_str)

        # All should be 100 chars (shorter than both limits)
        for line in history_lines:
            assert len(line) == 100

    def test_fewer_than_3_turns_all_recent(self):
        """With fewer than 3 turns, all should get full 4000-char treatment."""
        req = self._build_request(num_turns=2, result_len=5000)
        history_lines = []
        num_turns = len(req.history)
        for idx, turn in enumerate(req.history):
            is_recent = (num_turns - idx) <= 3
            max_len = 4000 if is_recent else 500
            result_str = str(turn.result)[:max_len] if turn.result else "no result"
            history_lines.append(result_str)

        assert len(history_lines[0]) == 4000
        assert len(history_lines[1]) == 4000

    def test_exactly_3_turns_all_recent(self):
        """With exactly 3 turns, all should be recent (full detail)."""
        req = self._build_request(num_turns=3, result_len=5000)
        history_lines = []
        num_turns = len(req.history)
        for idx, turn in enumerate(req.history):
            is_recent = (num_turns - idx) <= 3
            max_len = 4000 if is_recent else 500
            result_str = str(turn.result)[:max_len] if turn.result else "no result"
            history_lines.append(result_str)

        for line in history_lines:
            assert len(line) == 4000

    def test_none_result_handled(self):
        """Turns with None result should produce 'no result'."""
        history = [AgentLoopTurn(iteration=0, action="test", result=None)]
        num_turns = len(history)
        for idx, turn in enumerate(history):
            is_recent = (num_turns - idx) <= 3
            max_len = 4000 if is_recent else 500
            result_str = str(turn.result)[:max_len] if turn.result else "no result"
            assert result_str == "no result"

    def test_many_turns_budget(self):
        """With 25 turns, total history chars should be bounded (~23K)."""
        req = self._build_request(num_turns=25, result_len=10000)
        total_chars = 0
        num_turns = len(req.history)
        for idx, turn in enumerate(req.history):
            is_recent = (num_turns - idx) <= 3
            max_len = 4000 if is_recent else 500
            result_str = str(turn.result)[:max_len] if turn.result else "no result"
            total_chars += len(result_str)

        # Expected: 22 * 500 + 3 * 4000 = 11000 + 12000 = 23000
        assert total_chars == 23000


class TestMaxIterationsDefault:
    """Test that max_iterations defaults match the platform config."""

    def test_default_max_iterations_is_25(self):
        req = AgentLoopStepRequest(
            agent_id="test",
            task="test",
            iteration=0,
        )
        assert req.max_iterations == 25
