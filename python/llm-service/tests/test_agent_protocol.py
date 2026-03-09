"""Tests for role-aware agent protocol generation."""

import pytest
from llm_service.roles.swarm.agent_protocol import get_work_protocol, COMMON_PROTOCOL_BASE


class TestGetWorkProtocol:
    def test_researcher_gets_search_fetch_pipeline(self):
        protocol = get_work_protocol("researcher")
        assert "web_search" in protocol
        assert "web_fetch" in protocol

    def test_coder_gets_read_implement_pipeline(self):
        protocol = get_work_protocol("coder")
        assert "file_read" in protocol or "file_list" in protocol
        assert "THE PIPELINE: search" not in protocol

    def test_synthesis_writer_gets_file_read_all(self):
        protocol = get_work_protocol("synthesis_writer")
        assert "file_list" in protocol or "file_read" in protocol
        assert "THE PIPELINE: search" not in protocol

    def test_analyst_gets_compute_pipeline(self):
        protocol = get_work_protocol("analyst")
        assert "python_executor" in protocol or "calculate" in protocol.lower()
        assert "THE PIPELINE: search" not in protocol

    def test_critic_gets_review_pipeline(self):
        protocol = get_work_protocol("critic")
        assert "file_read" in protocol or "cross-check" in protocol.lower()
        assert "THE PIPELINE: search" not in protocol

    def test_generalist_gets_balanced_protocol(self):
        protocol = get_work_protocol("generalist")
        assert len(protocol) > 100

    def test_unknown_role_gets_default(self):
        protocol = get_work_protocol("unknown_role_xyz")
        assert len(protocol) > 100

    def test_common_base_always_present(self):
        for role in ["researcher", "coder", "analyst", "critic", "synthesis_writer", "generalist"]:
            protocol = get_work_protocol(role)
            assert "valid JSON" in protocol

    def test_all_protocols_end_with_json_constraint(self):
        for role in ["researcher", "coder", "analyst", "critic", "synthesis_writer", "generalist"]:
            protocol = get_work_protocol(role)
            last_lines = protocol.strip().split("\n")[-3:]
            assert any("JSON" in line for line in last_lines)

    def test_company_researcher_uses_research_protocol(self):
        cr_proto = get_work_protocol("company_researcher")
        assert "web_search" in cr_proto
        assert "web_fetch" in cr_proto

    def test_financial_analyst_uses_research_protocol(self):
        protocol = get_work_protocol("financial_analyst")
        assert "web_search" in protocol or "web_fetch" in protocol


class TestGeneralistRolePrompt:
    """Test that generalist role has actual guidance."""

    def test_generalist_prompt_not_empty(self):
        from llm_service.roles.swarm.role_prompts import SWARM_ROLE_PROMPTS
        assert len(SWARM_ROLE_PROMPTS["generalist"]) > 50

    def test_generalist_prompt_mentions_adapting(self):
        from llm_service.roles.swarm.role_prompts import SWARM_ROLE_PROMPTS
        prompt = SWARM_ROLE_PROMPTS["generalist"].lower()
        assert "adapt" in prompt or "flexible" in prompt
