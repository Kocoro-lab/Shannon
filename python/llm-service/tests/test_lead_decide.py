"""Tests for Lead Agent decision endpoint."""

import json
import pytest
from unittest.mock import AsyncMock, MagicMock, patch

from llm_service.api.lead import (
    LeadDecisionRequest,
    LeadDecisionResponse,
    LeadEvent,
    AgentState,
    LeadBudget,
    LeadAction,
    LEAD_SYSTEM_PROMPT,
    _auto_link_task_ids,
    _best_match,
)


class TestLeadModels:
    """Test Pydantic models for Lead endpoint."""

    def test_lead_event_model(self):
        event = LeadEvent(type="agent_completed", agent_id="Maji", result_summary="Found AWS pricing")
        assert event.type == "agent_completed"
        assert event.agent_id == "Maji"
        assert event.result_summary == "Found AWS pricing"

    def test_lead_event_defaults(self):
        event = LeadEvent(type="checkpoint")
        assert event.agent_id == ""
        assert event.result_summary == ""

    def test_lead_decision_request(self):
        req = LeadDecisionRequest(
            workflow_id="test-wf",
            event=LeadEvent(type="checkpoint"),
        )
        assert req.workflow_id == "test-wf"
        assert len(req.task_list) == 0
        assert len(req.agent_states) == 0
        assert len(req.history) == 0

    def test_lead_decision_request_full(self):
        req = LeadDecisionRequest(
            workflow_id="wf-123",
            event=LeadEvent(type="agent_completed", agent_id="Akiba"),
            task_list=[
                {"id": "T1", "description": "Research pricing", "status": "completed", "owner": "Akiba"},
                {"id": "T2", "description": "Analyze competitors", "status": "pending", "owner": ""},
            ],
            agent_states=[
                AgentState(agent_id="Akiba", status="completed", current_task="T1", iterations_used=5),
                AgentState(agent_id="Maji", status="idle"),
            ],
            budget=LeadBudget(total_llm_calls=50, remaining_llm_calls=150),
            history=[{"decision_summary": "Assigned T1 to Akiba"}],
        )
        assert len(req.task_list) == 2
        assert len(req.agent_states) == 2
        assert req.budget.total_llm_calls == 50

    def test_lead_decision_response(self):
        resp = LeadDecisionResponse(
            decision_summary="All tasks done",
            actions=[LeadAction(type="done")],
        )
        assert resp.decision_summary == "All tasks done"
        assert len(resp.actions) == 1
        assert resp.actions[0].type == "done"

    def test_lead_action_types(self):
        for action_type in ["assign_task", "spawn_agent", "send_message", "broadcast", "revise_plan", "done"]:
            action = LeadAction(type=action_type)
            assert action.type == action_type

    def test_lead_action_assign_task(self):
        action = LeadAction(type="assign_task", task_id="T3", agent_id="Maji")
        assert action.task_id == "T3"
        assert action.agent_id == "Maji"

    def test_lead_action_spawn_agent(self):
        action = LeadAction(type="spawn_agent", role="specialist", task_description="Deep dive into X")
        assert action.role == "specialist"
        assert action.task_description == "Deep dive into X"

    def test_lead_action_send_message(self):
        action = LeadAction(type="send_message", to="Maji", content="Focus on pricing")
        assert action.to == "Maji"
        assert action.content == "Focus on pricing"

    def test_lead_action_revise_plan(self):
        action = LeadAction(
            type="revise_plan",
            create=[{"id": "T5", "description": "New task"}],
            cancel=["T3"],
        )
        assert len(action.create) == 1
        assert action.cancel == ["T3"]

    def test_lead_action_defaults(self):
        action = LeadAction(type="done")
        assert action.task_id == ""
        assert action.agent_id == ""
        assert action.role == ""
        assert action.to == ""
        assert action.content == ""
        assert action.create == []
        assert action.cancel == []

    def test_lead_system_prompt_exists(self):
        assert len(LEAD_SYSTEM_PROMPT) > 100
        assert "Lead orchestrator" in LEAD_SYSTEM_PROMPT
        assert "assign_task" in LEAD_SYSTEM_PROMPT
        assert "spawn_agent" in LEAD_SYSTEM_PROMPT
        assert "send_message" in LEAD_SYSTEM_PROMPT
        assert "broadcast" in LEAD_SYSTEM_PROMPT
        assert "revise_plan" in LEAD_SYSTEM_PROMPT
        assert "done" in LEAD_SYSTEM_PROMPT

    def test_lead_system_prompt_rules(self):
        assert "NEVER return empty actions" in LEAD_SYSTEM_PROMPT

    def test_budget_defaults(self):
        budget = LeadBudget()
        assert budget.total_llm_calls == 0
        assert budget.remaining_llm_calls == 200
        assert budget.total_tokens == 0
        assert budget.remaining_tokens == 1000000
        assert budget.elapsed_seconds == 0
        assert budget.max_wall_clock_seconds == 1800

    def test_agent_state_model(self):
        state = AgentState(agent_id="Akiba", status="running", current_task="T1", iterations_used=3)
        assert state.agent_id == "Akiba"
        assert state.status == "running"
        assert state.current_task == "T1"
        assert state.iterations_used == 3

    def test_agent_state_defaults(self):
        state = AgentState(agent_id="Maji", status="idle")
        assert state.current_task == ""
        assert state.iterations_used == 0

    def test_response_token_fields(self):
        resp = LeadDecisionResponse(
            tokens_used=100,
            input_tokens=80,
            output_tokens=20,
            model_used="claude-3-5-sonnet",
            provider="anthropic",
        )
        assert resp.tokens_used == 100
        assert resp.input_tokens == 80
        assert resp.output_tokens == 20
        assert resp.model_used == "claude-3-5-sonnet"
        assert resp.provider == "anthropic"


class TestLeadEndpoint:
    """Test the /lead/decide endpoint with mocked LLM."""

    @pytest.fixture
    def mock_providers(self):
        providers = MagicMock()
        providers.is_configured.return_value = True
        return providers

    @pytest.fixture
    def mock_request(self, mock_providers):
        request = MagicMock()
        request.app.state.providers = mock_providers
        return request

    @pytest.mark.asyncio
    async def test_lead_decide_assign_task(self, mock_request, mock_providers):
        """Lead should parse an assign_task action from LLM response."""
        llm_response = {
            "output_text": '{"decision_summary": "Akiba completed T1, assigning T2 to idle Maji.", "actions": [{"type": "assign_task", "task_id": "T2", "agent_id": "Maji"}]}',
            "usage": {"total_tokens": 150, "input_tokens": 120, "output_tokens": 30},
            "model": "claude-3-5-sonnet",
            "provider": "anthropic",
        }
        mock_providers.generate_completion = AsyncMock(return_value=llm_response)

        from llm_service.api.lead import lead_decide

        body = LeadDecisionRequest(
            workflow_id="wf-test",
            event=LeadEvent(type="agent_completed", agent_id="Akiba", result_summary="Done"),
            task_list=[
                {"id": "T1", "status": "completed", "owner": "Akiba", "description": "Research"},
                {"id": "T2", "status": "pending", "owner": "", "description": "Analyze"},
            ],
            agent_states=[
                AgentState(agent_id="Akiba", status="completed"),
                AgentState(agent_id="Maji", status="idle"),
            ],
        )

        resp = await lead_decide(mock_request, body)

        assert resp.decision_summary == "Akiba completed T1, assigning T2 to idle Maji."
        assert len(resp.actions) == 1
        assert resp.actions[0].type == "assign_task"
        assert resp.actions[0].task_id == "T2"
        assert resp.actions[0].agent_id == "Maji"
        assert resp.tokens_used == 150
        assert resp.model_used == "claude-3-5-sonnet"

    @pytest.mark.asyncio
    async def test_lead_decide_done(self, mock_request, mock_providers):
        """Lead should return done when all tasks are complete."""
        llm_response = {
            "output_text": '{"decision_summary": "All tasks completed. Triggering synthesis.", "actions": [{"type": "done"}]}',
            "usage": {"total_tokens": 80, "input_tokens": 60, "output_tokens": 20},
            "model": "gpt-4o",
            "provider": "openai",
        }
        mock_providers.generate_completion = AsyncMock(return_value=llm_response)

        from llm_service.api.lead import lead_decide

        body = LeadDecisionRequest(
            workflow_id="wf-done",
            event=LeadEvent(type="checkpoint"),
        )

        resp = await lead_decide(mock_request, body)

        assert resp.actions[0].type == "done"
        assert resp.provider == "openai"

    @pytest.mark.asyncio
    async def test_lead_decide_non_json_fallback(self, mock_request, mock_providers):
        """When LLM returns non-JSON, Lead should fallback to noop."""
        llm_response = {
            "output_text": "I think we should assign the task to Maji",
            "usage": {"total_tokens": 50, "input_tokens": 40, "output_tokens": 10},
            "model": "claude-3-5-sonnet",
            "provider": "anthropic",
        }
        mock_providers.generate_completion = AsyncMock(return_value=llm_response)

        from llm_service.api.lead import lead_decide

        body = LeadDecisionRequest(
            workflow_id="wf-bad",
            event=LeadEvent(type="checkpoint"),
        )

        resp = await lead_decide(mock_request, body)

        assert "Failed to parse" in resp.decision_summary
        assert len(resp.actions) == 1
        assert resp.actions[0].type == "noop"

    @pytest.mark.asyncio
    async def test_lead_decide_empty_actions_fallback(self, mock_request, mock_providers):
        """When LLM returns empty actions array, Lead should default to noop."""
        llm_response = {
            "output_text": '{"decision_summary": "Nothing to do.", "actions": []}',
            "usage": {"total_tokens": 50, "input_tokens": 40, "output_tokens": 10},
            "model": "claude-3-5-sonnet",
            "provider": "anthropic",
        }
        mock_providers.generate_completion = AsyncMock(return_value=llm_response)

        from llm_service.api.lead import lead_decide

        body = LeadDecisionRequest(
            workflow_id="wf-empty",
            event=LeadEvent(type="checkpoint"),
        )

        resp = await lead_decide(mock_request, body)

        assert len(resp.actions) == 1
        assert resp.actions[0].type == "noop"

    @pytest.mark.asyncio
    async def test_lead_decide_multiple_actions(self, mock_request, mock_providers):
        """Lead should handle multiple actions in a single decision."""
        llm_response = {
            "output_text": '{"decision_summary": "Assigning tasks and spawning specialist.", "actions": [{"type": "assign_task", "task_id": "T2", "agent_id": "Maji"}, {"type": "spawn_agent", "role": "pricing_specialist", "task_description": "Deep pricing analysis"}, {"type": "broadcast", "content": "New specialist joining the team"}]}',
            "usage": {"total_tokens": 200, "input_tokens": 150, "output_tokens": 50},
            "model": "claude-3-5-sonnet",
            "provider": "anthropic",
        }
        mock_providers.generate_completion = AsyncMock(return_value=llm_response)

        from llm_service.api.lead import lead_decide

        body = LeadDecisionRequest(
            workflow_id="wf-multi",
            event=LeadEvent(type="agent_completed", agent_id="Akiba"),
        )

        resp = await lead_decide(mock_request, body)

        assert len(resp.actions) == 3
        assert resp.actions[0].type == "assign_task"
        assert resp.actions[1].type == "spawn_agent"
        assert resp.actions[1].role == "pricing_specialist"
        assert resp.actions[2].type == "broadcast"

    @pytest.mark.asyncio
    async def test_lead_decide_providers_not_configured(self):
        """Should return 503 when providers not configured."""
        from llm_service.api.lead import lead_decide
        from fastapi import HTTPException

        request = MagicMock()
        request.app.state.providers = None

        body = LeadDecisionRequest(
            workflow_id="wf-err",
            event=LeadEvent(type="checkpoint"),
        )

        with pytest.raises(HTTPException) as exc_info:
            await lead_decide(request, body)
        assert exc_info.value.status_code == 503

    @pytest.mark.asyncio
    async def test_lead_decide_uses_medium_tier(self, mock_request, mock_providers):
        """Lead should use ModelTier.MEDIUM for LLM calls."""
        llm_response = {
            "output_text": '{"decision_summary": "ok", "actions": [{"type": "done"}]}',
            "usage": {"total_tokens": 50, "input_tokens": 40, "output_tokens": 10},
            "model": "test",
            "provider": "test",
        }
        mock_providers.generate_completion = AsyncMock(return_value=llm_response)

        from llm_service.api.lead import lead_decide

        body = LeadDecisionRequest(
            workflow_id="wf-tier",
            event=LeadEvent(type="checkpoint"),
        )

        await lead_decide(mock_request, body)

        # Verify generate_completion was called with MEDIUM tier
        call_kwargs = mock_providers.generate_completion.call_args
        from llm_service.providers.base import ModelTier
        assert call_kwargs.kwargs.get("tier") == ModelTier.MEDIUM

    @pytest.mark.asyncio
    async def test_lead_decide_revise_plan(self, mock_request, mock_providers):
        """Lead should parse revise_plan action with create and cancel."""
        llm_response = {
            "output_text": '{"decision_summary": "Revising plan to add competitor analysis.", "actions": [{"type": "revise_plan", "create": [{"id": "T5", "description": "Competitor analysis"}], "cancel": ["T3"]}]}',
            "usage": {"total_tokens": 100, "input_tokens": 80, "output_tokens": 20},
            "model": "claude-3-5-sonnet",
            "provider": "anthropic",
        }
        mock_providers.generate_completion = AsyncMock(return_value=llm_response)

        from llm_service.api.lead import lead_decide

        body = LeadDecisionRequest(
            workflow_id="wf-revise",
            event=LeadEvent(type="checkpoint"),
        )

        resp = await lead_decide(mock_request, body)

        assert resp.actions[0].type == "revise_plan"
        assert len(resp.actions[0].create) == 1
        assert resp.actions[0].create[0]["id"] == "T5"
        assert resp.actions[0].cancel == ["T3"]


class TestLeadClosingCheckpoint:
    """Test max_tokens adjustment for closing_checkpoint events."""

    @pytest.fixture
    def mock_providers(self):
        providers = MagicMock()
        providers.is_configured.return_value = True
        return providers

    @pytest.fixture
    def mock_request(self, mock_providers):
        request = MagicMock()
        request.app.state.providers = mock_providers
        return request

    def _make_llm_response(self):
        return {
            "output_text": '{"decision_summary": "ok", "actions": [{"type": "done"}]}',
            "usage": {"total_tokens": 50, "input_tokens": 40, "output_tokens": 10},
            "model": "test-model",
            "provider": "test-provider",
        }

    @pytest.mark.asyncio
    async def test_closing_checkpoint_uses_higher_max_tokens(self, mock_request, mock_providers):
        """When event.type is closing_checkpoint, max_tokens should be 4096."""
        mock_providers.generate_completion = AsyncMock(return_value=self._make_llm_response())

        from llm_service.api.lead import lead_decide

        body = LeadDecisionRequest(
            workflow_id="wf-closing",
            event=LeadEvent(type="closing_checkpoint"),
        )

        await lead_decide(mock_request, body)

        call_kwargs = mock_providers.generate_completion.call_args
        assert call_kwargs.kwargs.get("max_tokens") == 16000

    @pytest.mark.asyncio
    async def test_normal_event_uses_default_max_tokens(self, mock_request, mock_providers):
        """Normal events use 4096 max_tokens; only closing_checkpoint uses 16000."""
        mock_providers.generate_completion = AsyncMock(return_value=self._make_llm_response())

        from llm_service.api.lead import lead_decide

        body = LeadDecisionRequest(
            workflow_id="wf-normal",
            event=LeadEvent(type="checkpoint"),
        )

        await lead_decide(mock_request, body)

        call_kwargs = mock_providers.generate_completion.call_args
        assert call_kwargs.kwargs.get("max_tokens") == 4096


class TestLeadStructuredOutput:
    """Test Lead endpoint uses structured output instead of prefill."""

    @pytest.fixture
    def mock_providers(self):
        providers = MagicMock()
        providers.is_configured.return_value = True
        return providers

    @pytest.fixture
    def mock_request(self, mock_providers):
        request = MagicMock()
        request.app.state.providers = mock_providers
        return request

    @pytest.mark.asyncio
    async def test_no_assistant_prefill_in_messages(self, mock_request, mock_providers):
        """Messages sent to LLM should NOT contain assistant prefill."""
        from llm_service.api.lead import lead_decide, LEAD_DECISION_SCHEMA

        captured_kwargs = {}

        async def mock_generate(**kwargs):
            captured_kwargs.update(kwargs)
            return {
                "output_text": '{"decision_summary": "test", "actions": [{"type": "noop"}]}',
                "usage": {"total_tokens": 100, "input_tokens": 80, "output_tokens": 20},
                "model": "claude-sonnet-4-6",
                "provider": "anthropic",
            }

        mock_providers.generate_completion = mock_generate

        body = LeadDecisionRequest(
            workflow_id="test-wf",
            event=LeadEvent(type="checkpoint"),
        )

        result = await lead_decide(mock_request, body)

        messages = captured_kwargs["messages"]
        assert not any(m["role"] == "assistant" for m in messages), \
            "Messages should not contain assistant prefill"
        assert messages[-1]["role"] == "user"

    @pytest.mark.asyncio
    async def test_output_config_passed_for_structured_output(self, mock_request, mock_providers):
        """output_config should be passed to enable Anthropic structured output."""
        from llm_service.api.lead import lead_decide, LEAD_DECISION_SCHEMA

        captured_kwargs = {}

        async def mock_generate(**kwargs):
            captured_kwargs.update(kwargs)
            return {
                "output_text": '{"decision_summary": "test", "actions": [{"type": "noop"}]}',
                "usage": {"total_tokens": 100, "input_tokens": 80, "output_tokens": 20},
                "model": "claude-sonnet-4-6",
                "provider": "anthropic",
            }

        mock_providers.generate_completion = mock_generate

        body = LeadDecisionRequest(
            workflow_id="test-wf",
            event=LeadEvent(type="checkpoint"),
        )

        await lead_decide(mock_request, body)

        assert captured_kwargs.get("output_config") == LEAD_DECISION_SCHEMA

    @pytest.mark.asyncio
    async def test_structured_output_fallback_on_error(self, mock_request, mock_providers):
        """When structured output fails (e.g. grammar timeout), should fallback to prompt-based."""
        from llm_service.api.lead import lead_decide

        call_count = 0

        async def mock_generate(**kwargs):
            nonlocal call_count
            call_count += 1
            if call_count == 1:
                # First call with output_config → simulate grammar timeout
                assert "output_config" in kwargs
                raise Exception("Grammar compilation timed out")
            # Second call without output_config → prompt-based fallback
            assert "output_config" not in kwargs
            return {
                "output_text": '{"decision_summary": "fallback worked", "actions": [{"type": "noop"}]}',
                "usage": {"total_tokens": 100, "input_tokens": 80, "output_tokens": 20},
                "model": "claude-sonnet-4-6",
                "provider": "anthropic",
            }

        mock_providers.generate_completion = mock_generate

        body = LeadDecisionRequest(
            workflow_id="test-wf",
            event=LeadEvent(type="checkpoint"),
        )

        resp = await lead_decide(mock_request, body)

        assert call_count == 2
        assert resp.decision_summary == "fallback worked"
        assert resp.actions[0].type == "noop"

    @pytest.mark.asyncio
    async def test_lead_max_tokens_always_4096(self, mock_request, mock_providers):
        """All events should use 4096 max_tokens to prevent JSON truncation."""
        from llm_service.api.lead import lead_decide

        captured_kwargs = {}

        async def mock_generate(**kwargs):
            captured_kwargs.update(kwargs)
            return {
                "output_text": '{"decision_summary": "test", "actions": [{"type": "noop"}]}',
                "usage": {"total_tokens": 100, "input_tokens": 80, "output_tokens": 20},
                "model": "test", "provider": "test",
            }

        mock_providers.generate_completion = mock_generate

        body = LeadDecisionRequest(
            workflow_id="test-wf",
            event=LeadEvent(type="checkpoint"),
        )

        await lead_decide(mock_request, body)

        assert captured_kwargs["max_tokens"] == 4096


class TestLeadOriginalQueryLanguage:
    """Test original_query language-matching directive injection."""

    @pytest.fixture
    def mock_providers(self):
        providers = MagicMock()
        providers.is_configured.return_value = True
        return providers

    @pytest.fixture
    def mock_request(self, mock_providers):
        request = MagicMock()
        request.app.state.providers = mock_providers
        return request

    def _make_llm_response(self):
        return {
            "output_text": '{"decision_summary": "ok", "actions": [{"type": "noop"}]}',
            "usage": {"total_tokens": 50, "input_tokens": 40, "output_tokens": 10},
            "model": "test-model",
            "provider": "test-provider",
        }

    @pytest.mark.asyncio
    async def test_language_directive_injected_for_non_initial_plan(self, mock_request, mock_providers):
        """When original_query is set and event is NOT initial_plan, language directive is injected."""
        captured_kwargs = {}

        async def mock_generate(**kwargs):
            captured_kwargs.update(kwargs)
            return self._make_llm_response()

        mock_providers.generate_completion = mock_generate

        from llm_service.api.lead import lead_decide

        body = LeadDecisionRequest(
            workflow_id="wf-lang",
            event=LeadEvent(type="agent_completed", agent_id="Akiba"),
            original_query="AWS の料金を調べてください",
        )

        await lead_decide(mock_request, body)

        messages = captured_kwargs["messages"]
        user_msg = next(m for m in messages if m["role"] == "user")
        assert "Original User Query" in user_msg["content"]
        assert "AWS の料金を調べてください" in user_msg["content"]
        assert "SAME LANGUAGE" in user_msg["content"]

    @pytest.mark.asyncio
    async def test_language_directive_not_injected_for_initial_plan(self, mock_request, mock_providers):
        """When event type IS initial_plan, language directive should NOT be injected."""
        captured_kwargs = {}

        async def mock_generate(**kwargs):
            captured_kwargs.update(kwargs)
            return self._make_llm_response()

        mock_providers.generate_completion = mock_generate

        from llm_service.api.lead import lead_decide

        body = LeadDecisionRequest(
            workflow_id="wf-lang-init",
            event=LeadEvent(type="initial_plan"),
            original_query="AWS の料金を調べてください",
        )

        await lead_decide(mock_request, body)

        messages = captured_kwargs["messages"]
        user_msg = next(m for m in messages if m["role"] == "user")
        assert "Original User Query" not in user_msg["content"]
        assert "SAME LANGUAGE" not in user_msg["content"]

    @pytest.mark.asyncio
    async def test_no_language_directive_when_original_query_empty(self, mock_request, mock_providers):
        """When original_query is empty, language directive should NOT be injected."""
        captured_kwargs = {}

        async def mock_generate(**kwargs):
            captured_kwargs.update(kwargs)
            return self._make_llm_response()

        mock_providers.generate_completion = mock_generate

        from llm_service.api.lead import lead_decide

        body = LeadDecisionRequest(
            workflow_id="wf-lang-empty",
            event=LeadEvent(type="agent_completed", agent_id="Maji"),
            original_query="",
        )

        await lead_decide(mock_request, body)

        messages = captured_kwargs["messages"]
        user_msg = next(m for m in messages if m["role"] == "user")
        assert "Original User Query" not in user_msg["content"]
        assert "SAME LANGUAGE" not in user_msg["content"]


class TestAutoLinkTaskIds:
    """Test _auto_link_task_ids: match spawn_agent to tasks by description."""

    def test_initial_plan_exact_match(self):
        """revise_plan + spawn_agent in same response, identical descriptions."""
        actions = [
            LeadAction(type="revise_plan", create=[
                {"id": "T1", "description": "Research AWS pricing"},
                {"id": "T2", "description": "Research Azure pricing"},
                {"id": "T3", "description": "Compare results", "depends_on": ["T1", "T2"]},
            ]),
            LeadAction(type="spawn_agent", role="researcher", task_description="Research AWS pricing"),
            LeadAction(type="spawn_agent", role="researcher", task_description="Research Azure pricing"),
        ]
        _auto_link_task_ids(actions, [])
        assert actions[1].task_id == "T1"
        assert actions[2].task_id == "T2"

    def test_initial_plan_substring_match(self):
        """spawn_agent has longer description that starts with task description."""
        actions = [
            LeadAction(type="revise_plan", create=[
                {"id": "T1", "description": "Research React performance"},
            ]),
            LeadAction(type="spawn_agent", role="researcher",
                        task_description="Research React performance: What are bundle sizes?"),
        ]
        _auto_link_task_ids(actions, [])
        assert actions[1].task_id == "T1"

    def test_word_overlap_match(self):
        """Descriptions differ but share enough words to match."""
        actions = [
            LeadAction(type="revise_plan", create=[
                {"id": "T1", "description": "Research PostgreSQL performance benchmarks"},
            ]),
            LeadAction(type="spawn_agent", role="researcher",
                        task_description="Research PostgreSQL and MySQL performance benchmarks and scalability"),
        ]
        _auto_link_task_ids(actions, [])
        assert actions[1].task_id == "T1"

    def test_no_match_below_threshold(self):
        """Completely different descriptions should not match."""
        actions = [
            LeadAction(type="revise_plan", create=[
                {"id": "T1", "description": "Research AWS pricing tiers"},
            ]),
            LeadAction(type="spawn_agent", role="coder",
                        task_description="Build a Python REST API for user management"),
        ]
        _auto_link_task_ids(actions, [])
        assert actions[1].task_id == ""

    def test_mid_execution_from_task_list(self):
        """Mid-execution spawn matched against pending tasks from body.task_list."""
        actions = [
            LeadAction(type="spawn_agent", role="synthesis_writer",
                        task_description="Synthesize comprehensive comparison report"),
        ]
        task_list = [
            {"id": "T1", "description": "Research performance", "status": "completed"},
            {"id": "T2", "description": "Research ecosystem", "status": "completed"},
            {"id": "T5", "description": "Create comprehensive comparison report", "status": "pending"},
        ]
        _auto_link_task_ids(actions, task_list)
        assert actions[0].task_id == "T5"

    def test_already_has_task_id_not_overwritten(self):
        """spawn_agent that already has task_id should not be touched."""
        actions = [
            LeadAction(type="revise_plan", create=[
                {"id": "T1", "description": "Research AWS"},
            ]),
            LeadAction(type="spawn_agent", role="researcher",
                        task_description="Research AWS", task_id="T1"),
        ]
        _auto_link_task_ids(actions, [])
        assert actions[1].task_id == "T1"

    def test_no_double_claim(self):
        """Each task_id can only be claimed by one spawn_agent."""
        actions = [
            LeadAction(type="revise_plan", create=[
                {"id": "T1", "description": "Research React"},
                {"id": "T2", "description": "Research Vue"},
            ]),
            LeadAction(type="spawn_agent", role="researcher", task_description="Research React"),
            LeadAction(type="spawn_agent", role="researcher", task_description="Research React frameworks"),
        ]
        _auto_link_task_ids(actions, [])
        assert actions[1].task_id == "T1"
        assert actions[2].task_id != "T1"

    def test_empty_actions(self):
        """No actions should not crash."""
        _auto_link_task_ids([], [])

    def test_no_revise_plan_no_task_list(self):
        """spawn_agent without any tasks available stays unlinked."""
        actions = [
            LeadAction(type="spawn_agent", role="researcher", task_description="Research something"),
        ]
        _auto_link_task_ids(actions, [])
        assert actions[0].task_id == ""


class TestBestMatch:
    """Test _best_match helper function."""

    def test_exact_substring(self):
        available = {"T1": "Research AWS pricing", "T2": "Research Azure pricing"}
        result = _best_match("Research AWS pricing tiers and discounts", available, set())
        assert result == "T1"

    def test_skips_claimed(self):
        available = {"T1": "Research AWS", "T2": "Research Azure"}
        result = _best_match("Research AWS", available, {"T1"})
        assert result != "T1"

    def test_word_overlap_threshold(self):
        available = {"T1": "Research PostgreSQL performance benchmarks scalability"}
        result = _best_match("Research PostgreSQL performance and optimization", available, set())
        assert result == "T1"

    def test_below_threshold_returns_empty(self):
        available = {"T1": "Research AWS pricing tiers compute storage"}
        result = _best_match("Build Python REST API", available, set())
        assert result == ""
