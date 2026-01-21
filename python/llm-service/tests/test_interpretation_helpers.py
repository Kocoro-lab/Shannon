from llm_service.api.agent import should_use_source_format, validate_interpretation_output


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
