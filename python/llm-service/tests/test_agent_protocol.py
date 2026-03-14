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

    def test_financial_analyst_uses_analyst_protocol(self):
        """financial_analyst must use ANALYST protocol (compute pipeline), not RESEARCH."""
        protocol = get_work_protocol("financial_analyst")
        assert "python_executor" in protocol, \
            "financial_analyst must have python_executor in its protocol (analyst pipeline)"
        assert "Compute" in protocol or "Collect data" in protocol, \
            "financial_analyst must have analyst compute pipeline, not research search pipeline"

    def test_writer_has_explicit_mapping(self):
        """writer role should have an explicit mapping in _ROLE_PROTOCOL_MAP, not fall through."""
        from llm_service.roles.swarm.agent_protocol import _ROLE_PROTOCOL_MAP
        assert "writer" in _ROLE_PROTOCOL_MAP, "writer must be explicitly mapped, not a fallback"

    def test_writer_gets_general_work_protocol(self):
        """writer role should get the general work protocol (includes writing workflow)."""
        protocol = get_work_protocol("writer")
        assert "Adaptive approach" in protocol


class TestPythonExecutorProtocol:
    """Tests for python_executor execution model and path rules in agent protocol."""

    def test_python_executor_isolation_warning(self):
        """Agent protocol must warn that each python_executor call is isolated."""
        protocol = get_work_protocol("analyst")
        assert "ISOLATED" in protocol or "isolated" in protocol, \
            "Protocol must warn that python_executor calls are isolated (no shared state)"

    def test_python_executor_no_custom_imports(self):
        """Agent protocol must forbid importing custom modules."""
        protocol = get_work_protocol("analyst")
        assert "NEVER import custom modules" in protocol, \
            "Protocol must explicitly forbid custom module imports in python_executor"

    def test_python_executor_workspace_prefix(self):
        """Agent protocol must require /workspace/ prefix for file I/O in python_executor."""
        protocol = get_work_protocol("analyst")
        assert "/workspace/" in protocol, \
            "Protocol must specify /workspace/ prefix for python_executor file I/O"

    def test_module_not_found_error_recovery(self):
        """Error recovery must cover ModuleNotFoundError."""
        protocol = get_work_protocol("researcher")
        assert "ModuleNotFoundError" in protocol, \
            "Error recovery must address ModuleNotFoundError (inline-only stdlib)"

    def test_wasi_stack_overflow_recovery(self):
        """Error recovery must cover WASI stack overflow from recursive code.

        Root cause: WASI sandbox has much lower stack limit than normal Python.
        Agent tried recursive quicksort 15 times without knowing to switch to iterative.
        """
        protocol = get_work_protocol("analyst")
        assert "recursion" in protocol.lower() or "recursive" in protocol.lower(), \
            "Protocol must warn about recursion limits in WASI sandbox"
        assert "iterative" in protocol.lower(), \
            "Protocol must suggest iterative alternative when recursion fails"

    def test_analyst_output_standard(self):
        """Analyst protocol must require both CSV data and MD summary."""
        protocol = get_work_protocol("analyst")
        assert "CSV" in protocol or "csv" in protocol, \
            "Analyst protocol must mention CSV output requirement"
        assert "Both files are REQUIRED" in protocol, \
            "Analyst protocol must enforce dual deliverable (data + summary)"

    def test_analyst_output_standard_has_makedirs(self):
        """Analyst OUTPUT STANDARD must inline os.makedirs next to the CSV save instruction.

        Root cause: LLM follows the nearest instruction. If os.makedirs is only in
        FILE RULES (100+ lines away), LLM skips it when writing python_executor code
        from the analyst OUTPUT STANDARD section.
        """
        protocol = get_work_protocol("analyst")
        # os.makedirs must appear IN the output standard section, not just in FILE RULES
        output_std_start = protocol.find("OUTPUT STANDARD")
        assert output_std_start > 0, "OUTPUT STANDARD section must exist"
        output_std_section = protocol[output_std_start:output_std_start + 500]
        assert "os.makedirs" in output_std_section, \
            "os.makedirs must be inlined in OUTPUT STANDARD, not just FILE RULES"

    def test_financial_analyst_has_output_standard(self):
        """financial_analyst must also have the analyst output standard."""
        protocol = get_work_protocol("financial_analyst")
        assert "Both files are REQUIRED" in protocol, \
            "financial_analyst must inherit analyst output standard"


class TestGeneralistRolePrompt:
    """Test that generalist role has actual guidance."""

    def test_generalist_prompt_not_empty(self):
        from llm_service.roles.swarm.role_prompts import SWARM_ROLE_PROMPTS
        assert len(SWARM_ROLE_PROMPTS["generalist"]) > 50

    def test_generalist_prompt_mentions_adapting(self):
        from llm_service.roles.swarm.role_prompts import SWARM_ROLE_PROMPTS
        prompt = SWARM_ROLE_PROMPTS["generalist"].lower()
        assert "adapt" in prompt or "flexible" in prompt
