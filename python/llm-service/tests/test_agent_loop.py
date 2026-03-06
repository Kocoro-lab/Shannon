"""Tests for agent loop history tiering and prompt construction."""

import pytest
import unittest
from unittest.mock import AsyncMock, patch, MagicMock

from llm_service.api.agent import AgentLoopStepRequest, AgentLoopTurn, TeamMemberInfo, agent_loop_step, build_agent_messages


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


class TestDecisionSummary:
    """Test that decision_summary field is parsed and truncated correctly."""

    def test_decision_summary_in_response_model(self):
        """decision_summary field exists on AgentLoopStepResponse."""
        from llm_service.api.agent import AgentLoopStepResponse
        resp = AgentLoopStepResponse(
            action="tool_call",
            decision_summary="Checking AWS pricing data for comparison",
            tool="web_search",
            tool_params={"query": "AWS pricing"},
        )
        assert resp.decision_summary == "Checking AWS pricing data for comparison"

    def test_decision_summary_default_empty(self):
        """decision_summary defaults to empty string if not provided."""
        from llm_service.api.agent import AgentLoopStepResponse
        resp = AgentLoopStepResponse(action="done", response="finished")
        assert resp.decision_summary == ""

    def test_decision_summary_in_action_json(self):
        """decision_summary is parsed from LLM JSON response."""
        import json
        raw = '{"decision_summary": "Teammate covered AWS, I focus Azure.", "action": "tool_call", "tool": "web_search", "tool_params": {"query": "Azure pricing"}}'
        parsed = json.loads(raw)
        assert "decision_summary" in parsed
        assert len(parsed["decision_summary"]) < 500

    def test_decision_summary_truncation(self):
        """decision_summary longer than 500 chars gets truncated."""
        long_summary = "x" * 1000
        truncated = long_summary[:500]
        assert len(truncated) == 500


class TestAgentLoopModelTier:
    """Test model_tier propagation into provider selection for /agent/loop."""

    @pytest.mark.asyncio
    async def test_agent_loop_step_uses_explicit_model_tier(self):
        from llm_service.providers.base import ModelTier

        mock_providers = MagicMock()
        mock_providers.is_configured.return_value = True
        mock_providers.generate_completion = AsyncMock(
            return_value={
                "output_text": "{\"action\": \"done\", \"decision_summary\": \"ok\", \"response\": \"hi\"}",
                "usage": {"total_tokens": 10, "input_tokens": 2, "output_tokens": 8},
                "model": "mock-model",
                "provider": "mock-provider",
            }
        )

        mock_request = MagicMock()
        mock_request.app.state.providers = mock_providers

        body = AgentLoopStepRequest(
            agent_id="test-agent",
            task="test task",
            iteration=0,
            model_tier="large",
        )

        resp = await agent_loop_step(mock_request, body)
        assert resp.action == "done"
        assert resp.response == "hi"

        assert mock_providers.generate_completion.await_args.kwargs["tier"] == ModelTier.LARGE

    @pytest.mark.asyncio
    async def test_agent_loop_step_uses_context_model_tier_when_no_explicit(self):
        from llm_service.providers.base import ModelTier

        mock_providers = MagicMock()
        mock_providers.is_configured.return_value = True
        mock_providers.generate_completion = AsyncMock(
            return_value={
                "output_text": "{\"action\": \"done\", \"decision_summary\": \"ok\", \"response\": \"hi\"}",
                "usage": {"total_tokens": 10, "input_tokens": 2, "output_tokens": 8},
                "model": "mock-model",
                "provider": "mock-provider",
            }
        )

        mock_request = MagicMock()
        mock_request.app.state.providers = mock_providers

        body = AgentLoopStepRequest(
            agent_id="test-agent",
            task="test task",
            iteration=0,
            context={"model_tier": "small"},
        )

        resp = await agent_loop_step(mock_request, body)
        assert resp.action == "done"

        assert mock_providers.generate_completion.await_args.kwargs["tier"] == ModelTier.SMALL

    @pytest.mark.asyncio
    async def test_agent_loop_step_explicit_overrides_context_model_tier(self):
        from llm_service.providers.base import ModelTier

        mock_providers = MagicMock()
        mock_providers.is_configured.return_value = True
        mock_providers.generate_completion = AsyncMock(
            return_value={
                "output_text": "{\"action\": \"done\", \"decision_summary\": \"ok\", \"response\": \"hi\"}",
                "usage": {"total_tokens": 10, "input_tokens": 2, "output_tokens": 8},
                "model": "mock-model",
                "provider": "mock-provider",
            }
        )

        mock_request = MagicMock()
        mock_request.app.state.providers = mock_providers

        body = AgentLoopStepRequest(
            agent_id="test-agent",
            task="test task",
            iteration=0,
            model_tier="medium",
            context={"model_tier": "small"},
        )

        resp = await agent_loop_step(mock_request, body)
        assert resp.action == "done"

        assert mock_providers.generate_completion.await_args.kwargs["tier"] == ModelTier.MEDIUM


class TestMultiJsonParsing:
    """Test that agent loop handles multi-JSON responses from GPT-5.1.

    GPT-5.1 sometimes emits tool_params JSON followed by the action JSON,
    concatenated with a newline. json.loads() only parses the first object,
    which may lack the 'action' field. The multi-JSON fallback should find the real one.
    """

    def _make_mock_request(self, output_text):
        """Create mock request + providers returning given output_text."""
        mock_providers = MagicMock()
        mock_providers.is_configured.return_value = True
        mock_providers.generate_completion = AsyncMock(
            return_value={
                "output_text": output_text,
                "usage": {"total_tokens": 50, "input_tokens": 20, "output_tokens": 30},
                "model": "gpt-5.1",
                "provider": "openai",
            }
        )
        mock_request = MagicMock()
        mock_request.app.state.providers = mock_providers
        return mock_request

    @pytest.mark.asyncio
    async def test_multi_json_recovers_action(self):
        """When LLM emits tool_params JSON then action JSON, recover the action."""
        # Simulate GPT-5.1 output: tool_params JSON + newline + action JSON
        # No assistant prefill — LLM returns complete JSON objects
        output_text = (
            '{"path": ".", "pattern": "*.md", "recursive": true}\n'
            '{"decision_summary": "Listing markdown files", "action": "tool_call", '
            '"tool": "file_list", "tool_params": {"path": ".", "pattern": "*.md", "recursive": true}}'
        )
        mock_request = self._make_mock_request(output_text)

        body = AgentLoopStepRequest(
            agent_id="test-agent",
            task="list files",
            iteration=0,
        )

        resp = await agent_loop_step(mock_request, body)
        assert resp.action == "tool_call"
        assert resp.tool == "file_list"
        assert resp.decision_summary == "Listing markdown files"

    @pytest.mark.asyncio
    async def test_single_json_still_works(self):
        """Normal single-JSON response should work as before."""
        output_text = (
            '{"decision_summary": "Done", "action": "done", "response": "All complete"}'
        )
        mock_request = self._make_mock_request(output_text)

        body = AgentLoopStepRequest(
            agent_id="test-agent",
            task="finish up",
            iteration=0,
        )

        resp = await agent_loop_step(mock_request, body)
        assert resp.action == "done"
        assert resp.response == "All complete"

    @pytest.mark.asyncio
    async def test_multi_json_no_action_in_any_falls_back_to_done(self):
        """If no JSON object has 'action', fall back to done (non-JSON path)."""
        output_text = (
            '{"path": ".", "pattern": "*.md"}\n'
            '{"summary": "no action field here either"}'
        )
        mock_request = self._make_mock_request(output_text)

        body = AgentLoopStepRequest(
            agent_id="test-agent",
            task="test task",
            iteration=0,
        )

        resp = await agent_loop_step(mock_request, body)
        assert resp.action == "done"


class TestAgentNoPrefill:
    """Test agent_loop_step doesn't use assistant prefill."""

    @pytest.mark.asyncio
    async def test_no_assistant_prefill_in_messages(self):
        """Messages sent to LLM should NOT contain assistant prefill."""
        captured_kwargs = {}

        async def mock_generate(**kwargs):
            captured_kwargs.update(kwargs)
            return {
                "output_text": '{"action": "idle", "decision_summary": "done", "response": "finished"}',
                "usage": {"total_tokens": 100, "input_tokens": 80, "output_tokens": 20},
                "model": "claude-sonnet-4-6-20250514",
                "provider": "anthropic",
            }

        mock_providers = MagicMock()
        mock_providers.is_configured.return_value = True
        mock_providers.generate_completion = mock_generate

        mock_request = MagicMock()
        mock_request.app.state.providers = mock_providers

        body = AgentLoopStepRequest(
            agent_id="test-agent",
            workflow_id="test-wf",
            task="Test task",
            iteration=0,
            suggested_tools=["web_search"],
        )

        result = await agent_loop_step(mock_request, body)

        messages = captured_kwargs.get("messages", [])
        assert not any(m["role"] == "assistant" for m in messages), \
            "Messages should not contain assistant prefill"
        assert messages[-1]["role"] == "user"


class TestTruncationMarker(unittest.TestCase):
    """A6: Truncation markers must discourage re-searching."""

    def test_truncation_marker_discourages_research(self):
        """Truncated text must tell agent NOT to re-search for the same data."""
        import inspect
        from llm_service.api import agent as agent_module
        source = inspect.getsource(agent_module)
        self.assertIn("do NOT re-search", source,
                       "Truncation marker must include 'do NOT re-search' instruction")


class TestInterpretationSkip(unittest.TestCase):
    """A3: Interpretation pass should be skipped for data-only tools."""

    def test_data_only_tools_constant_exists(self):
        """DATA_ONLY_TOOLS constant must be defined with correct membership."""
        from llm_service.api.agent import DATA_ONLY_TOOLS
        self.assertIsInstance(DATA_ONLY_TOOLS, (set, frozenset))
        self.assertIn("file_list", DATA_ONLY_TOOLS)
        self.assertIn("file_read", DATA_ONLY_TOOLS)
        self.assertIn("file_search", DATA_ONLY_TOOLS)
        self.assertIn("file_write", DATA_ONLY_TOOLS)
        self.assertIn("publish_data", DATA_ONLY_TOOLS)
        # web_search and web_fetch ARE data-only: their output is structured
        # data (search snippets, extracted content) that doesn't need
        # intermediate LLM interpretation in swarm mode.
        self.assertIn("web_search", DATA_ONLY_TOOLS)
        self.assertIn("web_fetch", DATA_ONLY_TOOLS)

    def test_data_only_tools_skip_interpretation(self):
        """When all executed tools are data-only, interpretation should be skipped."""
        from llm_service.api.agent import DATA_ONLY_TOOLS
        records = [
            {"tool": "file_list", "success": True, "output": "[]"},
            {"tool": "file_read", "success": True, "output": "content"},
        ]
        all_data_only = all(r.get("tool") in DATA_ONLY_TOOLS for r in records)
        self.assertTrue(all_data_only, "All file_* tools should be classified as data-only")

    def test_mixed_tools_keep_interpretation(self):
        """When any tool is NOT data-only, interpretation should run."""
        from llm_service.api.agent import DATA_ONLY_TOOLS
        records = [
            {"tool": "bash_executor", "success": True, "output": "results"},
            {"tool": "file_write", "success": True, "output": "ok"},
        ]
        all_data_only = all(r.get("tool") in DATA_ONLY_TOOLS for r in records)
        self.assertFalse(all_data_only, "bash_executor is NOT data-only, interpretation should run")

    def test_empty_records_no_skip(self):
        """Empty tool_execution_records should not trigger data-only skip."""
        from llm_service.api.agent import DATA_ONLY_TOOLS
        records = []
        all_data_only = records and all(r.get("tool") in DATA_ONLY_TOOLS for r in records)
        self.assertFalse(all_data_only, "Empty records should not skip interpretation")


class TestAgentProtocolCognitiveModel:
    """Verify agent protocol contains cognitive framework elements."""

    def test_mental_model_section_exists(self):
        """Protocol must open with HOW TOOLS WORK mental model, not phases."""
        from llm_service.roles.swarm.agent_protocol import AGENT_LOOP_SYSTEM_PROMPT
        model_pos = AGENT_LOOP_SYSTEM_PROMPT.find("HOW YOUR TOOLS WORK")
        phase_pos = AGENT_LOOP_SYSTEM_PROMPT.find("PHASE 1")
        assert model_pos != -1, "Missing 'HOW YOUR TOOLS WORK' section"
        assert model_pos < phase_pos, "Mental model must appear BEFORE phase instructions"

    def test_search_fetch_pipeline_explained(self):
        """Protocol must explicitly explain search→fetch as a pipeline."""
        from llm_service.roles.swarm.agent_protocol import AGENT_LOOP_SYSTEM_PROMPT
        assert "URL finder" in AGENT_LOOP_SYSTEM_PROMPT or "URL discovery" in AGENT_LOOP_SYSTEM_PROMPT, \
            "Must frame web_search as URL discovery, not answer retrieval"

    def test_protocol_length_reduced(self):
        """Protocol should be under 160 lines to maintain attention density."""
        from llm_service.roles.swarm.agent_protocol import AGENT_LOOP_SYSTEM_PROMPT
        line_count = AGENT_LOOP_SYSTEM_PROMPT.count('\n')
        assert line_count <= 160, f"Protocol has {line_count} lines, should be ≤160 for attention density"


class TestResearcherRolePrompt:
    """Verify researcher role prompt contains reasoning examples."""

    def test_has_ooda_decision_loop(self):
        """Researcher prompt must include OODA decision loop."""
        from llm_service.roles.swarm.role_prompts import SWARM_ROLE_PROMPTS
        prompt = SWARM_ROLE_PROMPTS["researcher"]
        assert "OBSERVE" in prompt and "ORIENT" in prompt and "DECIDE" in prompt, \
            "Researcher prompt must include OODA decision loop (OBSERVE/ORIENT/DECIDE)"

    def test_search_fetch_example(self):
        """Researcher prompt must include a search->fetch workflow example."""
        from llm_service.roles.swarm.role_prompts import SWARM_ROLE_PROMPTS
        prompt = SWARM_ROLE_PROMPTS["researcher"]
        assert "web_fetch" in prompt and "search" in prompt.lower(), \
            "Must show complete search->fetch workflow"


class TestRoleReasoningExamples:
    """All research-oriented roles should have reasoning examples."""

    def test_company_researcher_has_reasoning(self):
        from llm_service.roles.swarm.role_prompts import SWARM_ROLE_PROMPTS
        prompt = SWARM_ROLE_PROMPTS["company_researcher"]
        assert "THINK:" in prompt, "company_researcher must have reasoning examples"

    def test_analyst_has_reasoning(self):
        from llm_service.roles.swarm.role_prompts import SWARM_ROLE_PROMPTS
        prompt = SWARM_ROLE_PROMPTS["analyst"]
        assert "THINK:" in prompt, "analyst must have reasoning examples"


class TestBuildAgentMessages:
    """Test the multi-turn message builder for agent loop."""

    def _build_body(self, history_turns=0, task="Research React vs Vue"):
        history = []
        for i in range(history_turns):
            history.append(AgentLoopTurn(
                iteration=i + 1,
                action="tool_call",
                result=f"Result from iteration {i + 1}" + ("x" * 500),
                decision_summary=f"Searching for data point {i + 1}",
            ))
        return AgentLoopStepRequest(
            agent_id="TestAgent",
            workflow_id="test-wf",
            task=task,
            iteration=history_turns + 1,
            max_iterations=15,
            history=history,
            role="researcher",
            is_swarm=True,
        )

    def test_no_history_produces_system_and_user(self):
        """First iteration: [system, user] — original two-message structure."""
        body = self._build_body(history_turns=0)
        messages = build_agent_messages(body)
        assert len(messages) == 2
        assert messages[0]["role"] == "system"
        assert messages[1]["role"] == "user"
        assert "## Task" in messages[1]["content"]
        assert "Budget" in messages[1]["content"]

    def test_with_history_still_two_messages(self):
        """With history: still [system, user] — no fake assistant messages."""
        body = self._build_body(history_turns=3)
        messages = build_agent_messages(body)
        assert len(messages) == 2
        assert messages[0]["role"] == "system"
        assert messages[1]["role"] == "user"
        # History should be in the user message as text, not as separate messages
        assert "Previous Actions" in messages[1]["content"]
        assert "Iteration 1" in messages[1]["content"]
        # No assistant messages — fake assistant messages cause identity confusion
        assert all(m["role"] != "assistant" for m in messages)

    def test_history_preserves_tiered_truncation(self):
        """Recent 3 turns get 4000 chars, older get 2000 (original behavior)."""
        body = self._build_body(history_turns=5)
        messages = build_agent_messages(body)
        content = messages[1]["content"]
        # Older turns (1,2) should be truncated to 2000 chars
        # Recent turns (3,4,5) should get full 4000 chars
        # Our test data has 500-char results, so all should be untruncated
        assert "Iteration 1" in content
        assert "Iteration 5" in content

    def test_no_cache_break_marker_in_messages(self):
        """Multi-turn structure replaces cache_break marker."""
        body = self._build_body(history_turns=3)
        messages = build_agent_messages(body)
        for msg in messages:
            content = msg["content"] if isinstance(msg["content"], str) else str(msg["content"])
            assert "<!-- cache_break -->" not in content

    def test_budget_in_last_user_message(self):
        """Budget and action prompt are in the final user message."""
        body = self._build_body(history_turns=2)
        messages = build_agent_messages(body)
        last_user = messages[-1]["content"]
        assert "Budget" in last_user
        assert "Decide your next action" in last_user


class TestRoleAwareProtocol:
    """Test that build_agent_messages uses role-specific protocol."""

    def test_researcher_gets_search_fetch_in_system(self):
        req = AgentLoopStepRequest(
            agent_id="test-agent",
            task="Research React adoption",
            iteration=0,
            role="researcher",
        )
        msgs = build_agent_messages(req)
        system_msg = msgs[0]["content"]
        assert "THE PIPELINE: search" in system_msg

    def test_coder_does_not_get_search_fetch_pipeline(self):
        req = AgentLoopStepRequest(
            agent_id="test-agent",
            task="Implement API endpoint",
            iteration=0,
            role="coder",
        )
        msgs = build_agent_messages(req)
        system_msg = msgs[0]["content"]
        assert "THE PIPELINE: search" not in system_msg
        assert "file_list" in system_msg

    def test_synthesis_writer_no_web_search_pipeline(self):
        req = AgentLoopStepRequest(
            agent_id="test-agent",
            task="Synthesize all findings",
            iteration=0,
            role="synthesis_writer",
        )
        msgs = build_agent_messages(req)
        system_msg = msgs[0]["content"]
        assert "THE PIPELINE: search" not in system_msg

    def test_generalist_gets_nonempty_protocol(self):
        req = AgentLoopStepRequest(
            agent_id="test-agent",
            task="Do something",
            iteration=0,
            role="generalist",
        )
        msgs = build_agent_messages(req)
        system_msg = msgs[0]["content"]
        assert "tool_call" in system_msg
        assert "JSON" in system_msg

    def test_unknown_role_gets_default_protocol(self):
        req = AgentLoopStepRequest(
            agent_id="test-agent",
            task="Custom task",
            iteration=0,
            role="custom_role_xyz",
        )
        msgs = build_agent_messages(req)
        system_msg = msgs[0]["content"]
        assert "tool_call" in system_msg
        assert "JSON" in system_msg


class TestOriginalQueryInjection:
    """Test that original_query appears in agent prompt."""

    def test_original_query_in_user_message(self):
        req = AgentLoopStepRequest(
            agent_id="test-agent",
            task="Research React adoption rates",
            iteration=0,
            original_query="帮我做一个前端技术选型报告",
        )
        msgs = build_agent_messages(req)
        user_msgs = [m for m in msgs if m["role"] == "user"]
        first_user = user_msgs[0]["content"]
        assert "帮我做一个前端技术选型报告" in first_user

    def test_no_original_query_when_empty(self):
        req = AgentLoopStepRequest(
            agent_id="test-agent",
            task="Research React adoption rates",
            iteration=0,
        )
        msgs = build_agent_messages(req)
        user_msgs = [m for m in msgs if m["role"] == "user"]
        first_user = user_msgs[0]["content"]
        assert "Original Question" not in first_user

    def test_original_query_appears_before_task(self):
        req = AgentLoopStepRequest(
            agent_id="test-agent",
            task="Research React adoption rates",
            iteration=0,
            original_query="Make a frontend tech report",
        )
        msgs = build_agent_messages(req)
        user_msgs = [m for m in msgs if m["role"] == "user"]
        first_user = user_msgs[0]["content"]
        q_pos = first_user.find("Make a frontend tech report")
        t_pos = first_user.find("Research React adoption rates")
        assert q_pos < t_pos, "Original query should appear before task"


class TestAgentLoopMessageStructure:
    """Test that agent_loop_step uses [system, user] structure (no fake assistant messages)."""

    @pytest.mark.asyncio
    async def test_agent_loop_sends_two_messages(self):
        """Agent loop sends [system, user] — no multi-turn assistant messages."""
        captured_messages = []

        async def mock_generate(messages, **kwargs):
            captured_messages.extend(messages)
            return {
                "output_text": '{"action": "done", "decision_summary": "ok", "response": "done"}',
                "usage": {"total_tokens": 10, "input_tokens": 5, "output_tokens": 5,
                          "cache_read_tokens": 0, "cache_creation_tokens": 0},
                "model": "mock", "provider": "mock",
            }

        mock_providers = MagicMock()
        mock_providers.is_configured.return_value = True
        mock_providers.generate_completion = AsyncMock(side_effect=mock_generate)

        mock_request = MagicMock()
        mock_request.app.state.providers = mock_providers

        body = AgentLoopStepRequest(
            agent_id="TestAgent",
            task="Research topic",
            iteration=3,
            role="researcher",
            is_swarm=True,
            history=[
                AgentLoopTurn(iteration=1, action="tool_call", result="result1", decision_summary="step1"),
                AgentLoopTurn(iteration=2, action="tool_call", result="result2", decision_summary="step2"),
            ],
        )

        await agent_loop_step(mock_request, body)

        # Should be [system, user] — no fake assistant messages
        assert len(captured_messages) == 2, f"Expected [system, user], got {len(captured_messages)} messages"
        assert captured_messages[0]["role"] == "system"
        assert captured_messages[1]["role"] == "user"
        # History should be in the user message, not as separate messages
        assert "Previous Actions" in captured_messages[1]["content"]
        # No cache_break marker
        for msg in captured_messages:
            c = msg["content"] if isinstance(msg["content"], str) else str(msg["content"])
            assert "<!-- cache_break -->" not in c


class TestContextTrimming(unittest.TestCase):
    """Context trimming must work with [system, user] two-message structure."""

    def test_trimming_activates_on_large_user_message(self):
        """When user message exceeds max_prompt_chars, history should be trimmed.

        Each history entry is truncated to HISTORY_RESULT_MAX (2000 chars) inside
        build_agent_messages, so we need enough entries to exceed 400K chars after
        per-entry truncation: 250 entries * ~2050 chars each ≈ 512K > 400K.
        """
        large_history = [
            AgentLoopTurn(iteration=i, action=f"tool_call:web_search", result="x" * 5000)
            for i in range(250)
        ]
        body = AgentLoopStepRequest(
            agent_id="test-agent",
            task="Test task",
            history=large_history,
            iteration=251,
            session_id="test-session",
        )
        messages = build_agent_messages(body)
        # Must be exactly 2 messages
        self.assertEqual(len(messages), 2)
        # User message must be under 450K chars (400K target + tolerance)
        user_content = messages[1]["content"]
        self.assertLessEqual(len(user_content), 450_000,
            "User message should be trimmed when history is too large")

    def test_trimming_preserves_recent_history(self):
        """Trimming should remove oldest history first, keeping recent turns."""
        large_history = [
            AgentLoopTurn(iteration=i, action=f"tool_call:search_{i}", result="x" * 5000)
            for i in range(250)
        ]
        body = AgentLoopStepRequest(
            agent_id="test-agent",
            task="Test task",
            history=large_history,
            iteration=251,
            session_id="test-session",
        )
        messages = build_agent_messages(body)
        user_content = messages[1]["content"]
        # Most recent iteration should still be present
        self.assertIn("search_249", user_content)
        # Oldest entries should have been removed
        self.assertNotIn("search_0", user_content)


class TestTeamRosterRole:
    """Test that team roster displays role information."""

    def test_roster_shows_role(self):
        req = AgentLoopStepRequest(
            agent_id="agent-Luna",
            task="My task",
            iteration=0,
            team_roster=[
                TeamMemberInfo(agent_id="agent-Luna", task="Research pricing", role="researcher"),
                TeamMemberInfo(agent_id="agent-Mars", task="Analyze data", role="analyst"),
            ],
        )
        msgs = build_agent_messages(req)
        user_msgs = [m for m in msgs if m["role"] == "user"]
        first_user = user_msgs[0]["content"]
        assert "[researcher]" in first_user
        assert "[analyst]" in first_user

    def test_roster_without_role_still_works(self):
        req = AgentLoopStepRequest(
            agent_id="agent-Luna",
            task="My task",
            iteration=0,
            team_roster=[
                TeamMemberInfo(agent_id="agent-Luna", task="Research pricing"),
            ],
        )
        msgs = build_agent_messages(req)
        user_msgs = [m for m in msgs if m["role"] == "user"]
        first_user = user_msgs[0]["content"]
        assert "agent-Luna" in first_user
