from llm_service.api.agent import (
    should_use_source_format,
    validate_interpretation_output,
    build_interpretation_messages,
)


def test_should_use_source_format_by_role():
    """Only deep_research_agent and research roles use source format."""
    assert should_use_source_format("deep_research_agent") is True
    assert should_use_source_format("research") is True
    assert should_use_source_format("generalist") is False
    assert should_use_source_format("research_supervisor") is False  # Not in the list
    assert should_use_source_format(None) is False
    assert should_use_source_format("") is False


def test_validate_interpretation_output_general_allows_non_source():
    """General format: lenient validation, no format checks."""
    output = "Answer: " + ("x" * 100)
    is_valid, _ = validate_interpretation_output(
        output,
        total_tool_output_chars=2000,
        expect_sources_format=False,
    )
    assert is_valid is True


def test_validate_interpretation_output_general_rejects_too_short():
    """General format: still rejects very short output."""
    output = "Short"
    is_valid, reason = validate_interpretation_output(
        output,
        total_tool_output_chars=2000,
        expect_sources_format=False,
    )
    assert is_valid is False
    assert reason == "too_short"


def test_validate_interpretation_output_general_rejects_continuation():
    """General format: rejects continuation patterns."""
    output = "I'll execute the search now and get back to you with results."
    is_valid, reason = validate_interpretation_output(
        output,
        total_tool_output_chars=2000,
        expect_sources_format=False,
    )
    assert is_valid is False
    assert reason == "continuation_pattern"


def test_validate_interpretation_output_source_requires_format_when_short():
    """Source format: requires PART format when output is short."""
    output = "This is a plain answer without source headings." + ("x" * 260)
    is_valid, reason = validate_interpretation_output(
        output,
        total_tool_output_chars=1000,
        expect_sources_format=True,
    )
    assert is_valid is False
    assert reason == "no_format_and_short"


def test_validate_interpretation_output_source_accepts_part_format():
    """Source format: accepts proper PART format."""
    output = (
        "# PART 1 - RETRIEVED INFORMATION\n\n"
        "## Source 1: example.com\n"
        "- Detail\n\n"
        + ("x" * 200)
    )
    is_valid, _ = validate_interpretation_output(
        output,
        total_tool_output_chars=500,
        expect_sources_format=True,
    )
    assert is_valid is True


# --- OODA pattern detection tests ---


def test_validate_interpretation_output_rejects_ooda_pattern():
    """Interpretation output with OODA sections should be rejected.

    This is the exact bug scenario: the LLM follows the OODA framework
    from the system prompt instead of synthesizing research results.
    """
    ooda_output = (
        "## Observe\n"
        "Based on the search results, I found information about the company's revenue.\n\n"
        "## Orient\n"
        "Current coverage is approximately 40%. Key gaps remain in leadership and funding.\n\n"
        "## Decide\n"
        "Need more data on leadership team and recent funding rounds.\n\n"
        "## Act\n"
        "I'll search for more information about the company's leadership structure."
    )
    is_valid, reason = validate_interpretation_output(
        ooda_output,
        total_tool_output_chars=5000,
        expect_sources_format=True,
    )
    assert is_valid is False
    assert reason == "ooda_pattern"


def test_validate_interpretation_output_rejects_ooda_h1_headers():
    """OODA sections with # (h1) headers should also be rejected."""
    ooda_output = (
        "# Observe\n"
        "Search results show partial coverage of the topic.\n\n"
        "# Orient\n"
        "The data is fragmented across multiple sources.\n\n"
        + ("x" * 500)
    )
    is_valid, reason = validate_interpretation_output(
        ooda_output,
        total_tool_output_chars=5000,
        expect_sources_format=False,
    )
    assert is_valid is False
    assert reason == "ooda_pattern"


def test_validate_interpretation_output_allows_single_observe_mention():
    """A single mention of 'Observe' (e.g., in body text) should NOT trigger rejection.

    Only reject when 2+ OODA section markers are present, indicating the output
    is structured as an OODA loop rather than incidental word usage.
    """
    output = (
        "## Key Findings\n"
        "We can observe that the company has grown significantly.\n\n"
        "## Comprehensive Summary\n"
        "The analysis reveals strong market positioning."
        + ("x" * 500)
    )
    is_valid, _ = validate_interpretation_output(
        output,
        total_tool_output_chars=5000,
        expect_sources_format=True,
    )
    assert is_valid is True


# --- build_interpretation_messages tests ---


def test_build_interpretation_messages_uses_interpretation_system_prompt():
    """When interpretation_system_prompt is provided, it should replace the tool-loop system prompt.

    This is the primary fix: interpretation pass should NOT see OODA/tool-planning instructions.
    """
    tool_loop_prompt = "You are a research agent. Follow OODA loop. Use web_search..."
    interp_prompt = "You are a report writer. Synthesize findings."

    messages = build_interpretation_messages(
        system_prompt=tool_loop_prompt,
        original_query="test query",
        tool_results_summary="tool results here",
        interpretation_prompt="Synthesize now.",
        interpretation_system_prompt=interp_prompt,
    )

    assert len(messages) == 2
    assert messages[0]["role"] == "system"
    # Must use the interpretation system prompt, NOT the tool-loop prompt
    assert messages[0]["content"] == interp_prompt
    assert "OODA" not in messages[0]["content"]
    assert "web_search" not in messages[0]["content"]


def test_build_interpretation_messages_falls_back_to_system_prompt():
    """When no interpretation_system_prompt is provided, fall back to the original system prompt."""
    tool_loop_prompt = "You are a research agent."

    messages = build_interpretation_messages(
        system_prompt=tool_loop_prompt,
        original_query="test query",
        tool_results_summary="results",
        interpretation_prompt="Synthesize.",
        interpretation_system_prompt=None,
    )

    assert messages[0]["content"] == tool_loop_prompt


def test_build_interpretation_messages_user_message_structure():
    """User message should contain query, tool results, and interpretation prompt."""
    messages = build_interpretation_messages(
        system_prompt="sys",
        original_query="What is X?",
        tool_results_summary="Found: X is Y",
        interpretation_prompt="Write report.",
    )

    user_content = messages[1]["content"]
    assert "What is X?" in user_content
    assert "Found: X is Y" in user_content
    assert "Write report." in user_content
    assert "=== TOOL RESULTS ===" in user_content
    assert "=== YOUR TASK ===" in user_content


# --- Preset contract tests ---


def test_deep_research_preset_has_interpretation_system_prompt():
    """deep_research_agent preset MUST have interpretation_system_prompt.

    This is a contract test: if someone removes the field, the OODA contamination
    bug will return. This test ensures the fix persists.
    """
    from llm_service.roles.deep_research.deep_research_agent import DEEP_RESEARCH_AGENT_PRESET

    assert "interpretation_system_prompt" in DEEP_RESEARCH_AGENT_PRESET
    interp_sys = DEEP_RESEARCH_AGENT_PRESET["interpretation_system_prompt"]
    assert isinstance(interp_sys, str)
    assert len(interp_sys) > 100  # Non-trivial content


def test_deep_research_interpretation_system_prompt_has_no_ooda():
    """interpretation_system_prompt must NOT contain OODA or tool-planning instructions.

    These belong only in the tool-loop system_prompt. Their presence in interpretation
    causes the LLM to output OODA analysis instead of synthesized reports.
    """
    from llm_service.roles.deep_research.deep_research_agent import DEEP_RESEARCH_AGENT_PRESET

    interp_sys = DEEP_RESEARCH_AGENT_PRESET["interpretation_system_prompt"]
    forbidden_patterns = [
        "OODA",
        "Observe",  # as section header concept
        "Orient",
        "web_search",
        "web_fetch",
        "tool call",
        "TOOL USAGE",
        "Coverage ≥70%",
        "coverage",
        "search strategy",
        "FETCH FIRST",
    ]
    interp_lower = interp_sys.lower()
    for pattern in forbidden_patterns:
        assert pattern.lower() not in interp_lower, (
            f"interpretation_system_prompt must not contain '{pattern}' — "
            f"this belongs in the tool-loop system_prompt only"
        )


def test_deep_research_tool_loop_prompt_has_ooda():
    """The tool-loop system_prompt SHOULD still contain OODA for agent research behavior."""
    from llm_service.roles.deep_research.deep_research_agent import DEEP_RESEARCH_AGENT_PRESET

    sys_prompt = DEEP_RESEARCH_AGENT_PRESET["system_prompt"]
    assert "OODA" in sys_prompt
    assert "OBSERVE" in sys_prompt  # "## 1. OBSERVE" in the prompt
