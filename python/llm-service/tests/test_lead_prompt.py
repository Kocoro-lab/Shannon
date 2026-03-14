"""Tests for Lead user prompt builder (_build_lead_user_prompt)."""

import pytest

from llm_service.api.lead import (
    _build_lead_user_prompt,
    LeadDecisionRequest,
    LeadEvent,
    LeadBudget,
    AgentState,
)


class TestBuildLeadUserPrompt:
    """Test _build_lead_user_prompt renders all sections correctly."""

    def _make_budget(self, **kwargs):
        defaults = dict(
            total_llm_calls=5,
            remaining_llm_calls=195,
            total_tokens=1000,
            remaining_tokens=999000,
            elapsed_seconds=60,
            max_wall_clock_seconds=1800,
        )
        defaults.update(kwargs)
        return LeadBudget(**defaults)

    def test_workspace_files_rendered(self):
        """Lead user prompt should include workspace file inventory when provided."""
        body = LeadDecisionRequest(
            workflow_id="wf-test",
            event=LeadEvent(type="checkpoint", agent_id=""),
            task_list=[],
            agent_states=[],
            budget=self._make_budget(),
            workspace_files=["findings/react.md", "findings/vue.md", "data/comparison.csv"],
        )
        prompt = _build_lead_user_prompt(body)
        assert "## Workspace Files (3 files)" in prompt
        assert "findings/react.md" in prompt
        assert "findings/vue.md" in prompt
        assert "data/comparison.csv" in prompt

    def test_no_workspace_files_section_when_empty(self):
        """When no workspace files, the section should not appear."""
        body = LeadDecisionRequest(
            workflow_id="wf-test",
            event=LeadEvent(type="checkpoint", agent_id=""),
            task_list=[],
            agent_states=[],
            budget=self._make_budget(),
        )
        prompt = _build_lead_user_prompt(body)
        assert "## Workspace Files" not in prompt

    def test_workspace_files_capped_at_30(self):
        """When more than 30 files, display is capped with a '... and N more' line."""
        files = [f"file_{i:03d}.md" for i in range(50)]
        body = LeadDecisionRequest(
            workflow_id="wf-test",
            event=LeadEvent(type="checkpoint", agent_id=""),
            task_list=[],
            agent_states=[],
            budget=self._make_budget(),
            workspace_files=files,
        )
        prompt = _build_lead_user_prompt(body)
        assert "## Workspace Files (50 files)" in prompt
        assert "file_029.md" in prompt  # 30th file (index 29) should be shown
        assert "file_030.md" not in prompt  # 31st file should be hidden
        assert "... and 20 more" in prompt

    def test_budget_section_always_present(self):
        """Budget section should always be rendered."""
        body = LeadDecisionRequest(
            workflow_id="wf-test",
            event=LeadEvent(type="checkpoint", agent_id=""),
            budget=self._make_budget(),
        )
        prompt = _build_lead_user_prompt(body)
        assert "## Budget" in prompt

    def test_human_input_event_rendered(self):
        """human_input events should render the user's message."""
        body = LeadDecisionRequest(
            workflow_id="wf-test",
            event=LeadEvent(type="human_input", human_message="Translate all files to Chinese"),
            budget=self._make_budget(),
        )
        prompt = _build_lead_user_prompt(body)
        assert "[HUMAN INPUT]" in prompt
        assert "Translate all files to Chinese" in prompt

    def test_original_query_injected_for_non_initial_plan(self):
        """original_query should appear for non-initial_plan events."""
        body = LeadDecisionRequest(
            workflow_id="wf-test",
            event=LeadEvent(type="checkpoint", agent_id=""),
            budget=self._make_budget(),
            original_query="Compare React vs Vue",
        )
        prompt = _build_lead_user_prompt(body)
        assert "## Original User Query" in prompt
        assert "Compare React vs Vue" in prompt

    def test_original_query_not_injected_for_initial_plan(self):
        """original_query should NOT appear for initial_plan events."""
        body = LeadDecisionRequest(
            workflow_id="wf-test",
            event=LeadEvent(type="initial_plan"),
            budget=self._make_budget(),
            original_query="Compare React vs Vue",
        )
        prompt = _build_lead_user_prompt(body)
        assert "## Original User Query" not in prompt

    def test_agent_states_rendered(self):
        """Agent states section should show agent details."""
        body = LeadDecisionRequest(
            workflow_id="wf-test",
            event=LeadEvent(type="checkpoint", agent_id=""),
            agent_states=[
                AgentState(agent_id="Akiba", status="running", role="researcher", current_task="T1", iterations_used=3),
                AgentState(agent_id="Maji", status="idle", role="analyst"),
            ],
            budget=self._make_budget(),
        )
        prompt = _build_lead_user_prompt(body)
        assert "## Agent States" in prompt
        assert "Akiba" in prompt
        assert "Maji" in prompt
        assert "1 running" in prompt
        assert "1 idle" in prompt

    def test_workspace_files_appear_between_agent_states_and_budget(self):
        """Workspace files section should appear after Agent States and before Budget."""
        body = LeadDecisionRequest(
            workflow_id="wf-test",
            event=LeadEvent(type="checkpoint", agent_id=""),
            agent_states=[
                AgentState(agent_id="Akiba", status="idle", role="researcher"),
            ],
            budget=self._make_budget(),
            workspace_files=["findings/test.md"],
        )
        prompt = _build_lead_user_prompt(body)
        agent_pos = prompt.index("## Agent States")
        ws_pos = prompt.index("## Workspace Files")
        budget_pos = prompt.index("## Budget")
        assert agent_pos < ws_pos < budget_pos

    def test_hitl_messages_rendered_for_non_human_input_events(self):
        """HITL message history should appear in non-human_input events."""
        body = LeadDecisionRequest(
            workflow_id="wf-test",
            event=LeadEvent(type="checkpoint", agent_id=""),
            budget=self._make_budget(),
            hitl_messages=["把报告翻译成英文", "加上Litestar对比"],
        )
        prompt = _build_lead_user_prompt(body)
        assert "## User Requests During Execution" in prompt
        assert "把报告翻译成英文" in prompt
        assert "加上Litestar对比" in prompt
        assert "MANDATORY" in prompt

    def test_hitl_messages_not_rendered_for_human_input_event(self):
        """HITL messages should NOT duplicate when current event IS human_input."""
        body = LeadDecisionRequest(
            workflow_id="wf-test",
            event=LeadEvent(type="human_input", human_message="翻译成日语"),
            budget=self._make_budget(),
            hitl_messages=["翻译成日语"],
        )
        prompt = _build_lead_user_prompt(body)
        assert "## User Requests During Execution" not in prompt
        assert "[HUMAN INPUT]" in prompt
