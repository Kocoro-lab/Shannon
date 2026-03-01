"""
Tests for web_fetch prompt-guided extraction feature (issue #38).

Tests the extract_prompt parameter across all three web tools:
- web_fetch (single URL, batch mode)
- web_subpage_fetch
- web_crawl

Verifies: disabled path unchanged, extraction happy path, fallback on failure,
timeout handling, cost/token tracking, and batch mode.
"""

import asyncio
import pytest
from unittest.mock import AsyncMock, MagicMock, patch
from dataclasses import dataclass

from llm_service.tools.builtin.web_fetch import (
    WebFetchTool,
    extract_with_llm,
    apply_extraction,
    EXTRACTION_INTERNAL_MAX,
    EXTRACTION_CONTENT_CAP,
)
from llm_service.tools.base import ToolResult


# --- Helpers ---

def _make_tool_result(content: str, max_length: int = 10000) -> ToolResult:
    """Create a ToolResult simulating a fetched page."""
    return ToolResult(
        success=True,
        output={
            "url": "https://example.com",
            "title": "Example",
            "content": content,
            "char_count": len(content),
            "word_count": len(content.split()),
            "truncated": len(content) >= max_length,
            "method": "pure_python",
            "pages_fetched": 1,
            "tool_source": "fetch",
            "status_code": 200,
            "blocked_reason": None,
        },
        metadata={"fetch_method": "pure_python"},
    )


@dataclass
class FakeUsage:
    total_tokens: int = 500
    estimated_cost: float = 0.001


@dataclass
class FakeResponse:
    content: str = "Extracted: pricing is $10/mo"
    model: str = "claude-haiku-4-5-20251001"
    usage: FakeUsage = None

    def __post_init__(self):
        if self.usage is None:
            self.usage = FakeUsage()


def _mock_llm_manager():
    """Create a mock LLMManager that returns a FakeResponse."""
    manager = MagicMock()
    manager.complete = AsyncMock(return_value=FakeResponse())
    return manager


# --- Tests for extract_with_llm ---

class TestExtractWithLlm:
    """Tests for the extract_with_llm helper function."""

    @pytest.mark.asyncio
    async def test_happy_path(self):
        """Extraction returns (text, tokens, cost, model) on success."""
        manager = _mock_llm_manager()
        with patch("llm_provider.manager.get_llm_manager", return_value=manager):
            result = await extract_with_llm("long content here", "extract pricing")

        assert result is not None
        text, tokens, cost, model = result
        assert text == "Extracted: pricing is $10/mo"
        assert tokens == 500
        assert cost == 0.001
        assert "haiku" in model

    @pytest.mark.asyncio
    async def test_returns_none_on_exception(self):
        """Extraction returns None when LLM call raises."""
        manager = MagicMock()
        manager.complete = AsyncMock(side_effect=RuntimeError("API down"))
        with patch("llm_provider.manager.get_llm_manager", return_value=manager):
            result = await extract_with_llm("content", "extract something")

        assert result is None

    @pytest.mark.asyncio
    async def test_returns_none_on_timeout(self):
        """Extraction returns None when LLM call times out."""
        async def slow_complete(*args, **kwargs):
            await asyncio.sleep(100)
            return FakeResponse()

        manager = MagicMock()
        manager.complete = slow_complete
        with patch("llm_provider.manager.get_llm_manager", return_value=manager):
            result = await extract_with_llm("content", "extract something", timeout=0.01)

        assert result is None

    @pytest.mark.asyncio
    async def test_caps_input_content(self):
        """Content exceeding EXTRACTION_CONTENT_CAP is truncated before sending to LLM."""
        manager = _mock_llm_manager()
        huge_content = "x" * (EXTRACTION_CONTENT_CAP + 10000)

        with patch("llm_provider.manager.get_llm_manager", return_value=manager):
            result = await extract_with_llm(huge_content, "extract something")

        assert result is not None
        # Verify the content sent to LLM was capped
        call_args = manager.complete.call_args
        user_msg = call_args.kwargs["messages"][1]["content"]
        # The page content portion should be capped
        assert len(user_msg) <= EXTRACTION_CONTENT_CAP + 200  # +200 for prompt prefix


# --- Tests for apply_extraction ---

class TestApplyExtraction:
    """Tests for the apply_extraction post-processing function."""

    @pytest.mark.asyncio
    async def test_generic_extraction_when_prompt_is_none(self):
        """When extract_prompt is None, generic extraction still runs on over-length content."""
        original_content = "x" * 20000
        result = _make_tool_result(original_content)

        manager = _mock_llm_manager()
        with patch("llm_provider.manager.get_llm_manager", return_value=manager):
            processed = await apply_extraction(result, None, 10000)

        # Extraction runs even without extract_prompt
        assert processed.output["extracted"] is True
        manager.complete.assert_called_once()

    @pytest.mark.asyncio
    async def test_no_extraction_when_content_fits(self):
        """When content fits within max_length, no extraction occurs."""
        short_content = "short page content"
        result = _make_tool_result(short_content)

        manager = _mock_llm_manager()
        with patch("llm_provider.manager.get_llm_manager", return_value=manager):
            processed = await apply_extraction(result, "extract pricing", 10000)

        assert processed.output["content"] == short_content
        manager.complete.assert_not_called()

    @pytest.mark.asyncio
    async def test_extraction_replaces_content(self):
        """When extraction succeeds, content is replaced with extracted text."""
        long_content = "x" * 20000
        result = _make_tool_result(long_content)

        manager = _mock_llm_manager()
        with patch("llm_provider.manager.get_llm_manager", return_value=manager):
            processed = await apply_extraction(result, "extract pricing", 10000)

        assert processed.output["content"] == "Extracted: pricing is $10/mo"
        assert processed.output["extracted"] is True
        assert processed.output["truncated"] is True
        assert processed.metadata["extraction_model"] == "claude-haiku-4-5-20251001"
        assert processed.metadata["extraction_tokens"] == 500
        assert processed.metadata["extraction_cost_usd"] == 0.001
        assert processed.tokens_used == 500
        assert processed.cost_usd == 0.001

    @pytest.mark.asyncio
    async def test_fallback_to_truncation_on_failure(self):
        """When extraction fails, content is hard-truncated to original max_length."""
        long_content = "a" * 5000 + "b" * 5000 + "c" * 10000
        result = _make_tool_result(long_content)
        original_max = 10000

        manager = MagicMock()
        manager.complete = AsyncMock(side_effect=RuntimeError("API down"))
        with patch("llm_provider.manager.get_llm_manager", return_value=manager):
            processed = await apply_extraction(result, "extract pricing", original_max)

        assert len(processed.output["content"]) == original_max
        assert processed.output["content"] == long_content[:original_max]
        assert processed.output["extracted"] is False
        assert processed.output["truncated"] is True

    @pytest.mark.asyncio
    async def test_no_extraction_on_failed_result(self):
        """Failed ToolResults are returned unchanged."""
        result = ToolResult(success=False, output=None, error="fetch failed")
        processed = await apply_extraction(result, "extract pricing", 10000)

        assert processed.success is False
        assert processed.error == "fetch failed"

    @pytest.mark.asyncio
    async def test_cost_fields_accumulate(self):
        """Extraction cost adds to existing ToolResult cost fields."""
        long_content = "x" * 20000
        result = _make_tool_result(long_content)
        result.tokens_used = 100  # Pre-existing tokens from tool
        result.cost_usd = 0.005  # Pre-existing cost

        manager = _mock_llm_manager()
        with patch("llm_provider.manager.get_llm_manager", return_value=manager):
            processed = await apply_extraction(result, "extract pricing", 10000)

        assert processed.tokens_used == 600  # 100 + 500
        assert processed.cost_usd == pytest.approx(0.006)  # 0.005 + 0.001


# --- Tests for WebFetchTool integration ---

class TestWebFetchExtractionIntegration:
    """Integration tests for extract_prompt in WebFetchTool._execute_impl."""

    @pytest.fixture
    def tool(self):
        tool = WebFetchTool()
        tool.firecrawl_provider = None
        tool.exa_api_key = None
        return tool

    @pytest.mark.asyncio
    async def test_short_content_no_extraction(self):
        """Short content (fits within max_length) is returned as-is, no extraction."""
        tool = WebFetchTool()
        tool.firecrawl_provider = None
        tool.exa_api_key = None

        short_content = "short page"

        async def mock_fetch(url, max_length, subpages=0):
            return ToolResult(
                success=True,
                output={
                    "url": url,
                    "title": "Test",
                    "content": short_content,
                    "char_count": len(short_content),
                    "word_count": 2,
                    "truncated": False,
                    "method": "pure_python",
                    "pages_fetched": 1,
                    "tool_source": "fetch",
                    "status_code": 200,
                    "blocked_reason": None,
                },
                metadata={"fetch_method": "pure_python"},
            )

        with patch.object(tool, "_fetch_pure_python", side_effect=mock_fetch):
            result = await tool.execute(url="https://example.com", max_length=10000)

        assert result.success
        assert result.output["content"] == short_content
        assert "extracted" not in result.output

    @pytest.mark.asyncio
    async def test_always_inflates_max_length_and_extracts(self):
        """Internal max_length is always inflated; extraction runs on over-length content."""
        tool = WebFetchTool()
        tool.firecrawl_provider = None
        tool.exa_api_key = None

        captured_max_length = None

        async def mock_fetch(url, max_length, subpages=0):
            nonlocal captured_max_length
            captured_max_length = max_length
            content = "x" * min(50000, max_length)
            return ToolResult(
                success=True,
                output={
                    "url": url,
                    "title": "Test",
                    "content": content,
                    "char_count": len(content),
                    "word_count": 1,
                    "truncated": False,
                    "method": "pure_python",
                    "pages_fetched": 1,
                    "tool_source": "fetch",
                    "status_code": 200,
                    "blocked_reason": None,
                },
                metadata={"fetch_method": "pure_python"},
            )

        manager = _mock_llm_manager()
        with patch.object(tool, "_fetch_pure_python", side_effect=mock_fetch), \
             patch("llm_provider.manager.get_llm_manager", return_value=manager):
            # No extract_prompt — generic extraction should still run
            result = await tool.execute(
                url="https://example.com",
                max_length=10000,
            )

        # Provider should have received the inflated max_length
        assert captured_max_length == EXTRACTION_INTERNAL_MAX
        # Result should have extracted content (generic extraction)
        assert result.success
        assert result.output["extracted"] is True
        assert result.output["truncated"] is True


# --- Edge case tests for length clamping and truncated consistency ---

class TestExtractionLengthClamping:
    """Verify extracted content respects max_length and truncated is always set."""

    @pytest.mark.asyncio
    async def test_low_max_length_clamps_extraction(self):
        """When max_length=1000, extracted content must not exceed 1000 chars."""
        long_content = "x" * 20000
        result = _make_tool_result(long_content)

        # LLM returns 2000 chars — exceeds max_length of 1000
        long_extraction = "y" * 2000
        manager = MagicMock()
        manager.complete = AsyncMock(return_value=FakeResponse(content=long_extraction))

        with patch("llm_provider.manager.get_llm_manager", return_value=manager):
            processed = await apply_extraction(result, None, 1000)

        assert len(processed.output["content"]) <= 1000
        assert processed.output["extracted"] is True
        assert processed.output["truncated"] is True

    @pytest.mark.asyncio
    async def test_max_output_tokens_capped_to_max_length(self):
        """extract_with_llm max_tokens should be min(max_length, 4000)."""
        long_content = "x" * 20000
        result = _make_tool_result(long_content)

        manager = _mock_llm_manager()
        with patch("llm_provider.manager.get_llm_manager", return_value=manager):
            await apply_extraction(result, None, 800)

        # Verify max_tokens passed to LLM is capped at 800 (not 4000)
        call_kwargs = manager.complete.call_args.kwargs
        assert call_kwargs["max_tokens"] == 800

    @pytest.mark.asyncio
    async def test_success_and_fallback_both_set_truncated(self):
        """Both extraction success and fallback paths set truncated=True."""
        long_content = "x" * 20000

        # Success path
        result_ok = _make_tool_result(long_content)
        manager = _mock_llm_manager()
        with patch("llm_provider.manager.get_llm_manager", return_value=manager):
            processed_ok = await apply_extraction(result_ok, None, 10000)
        assert processed_ok.output["truncated"] is True
        assert processed_ok.output["extracted"] is True

        # Fallback path
        result_fail = _make_tool_result(long_content)
        manager_fail = MagicMock()
        manager_fail.complete = AsyncMock(side_effect=RuntimeError("fail"))
        with patch("llm_provider.manager.get_llm_manager", return_value=manager_fail):
            processed_fail = await apply_extraction(result_fail, None, 10000)
        assert processed_fail.output["truncated"] is True
        assert processed_fail.output["extracted"] is False



# --- Tests for research_mode skipping extraction ---

class TestResearchModeSkipsExtraction:
    """Research mode OODA loop does many fast fetches — extraction adds ~40-60s
    latency per call, causing timeout.  Skip auto-extraction when
    session_context.research_mode=True (issue #43)."""

    @pytest.fixture
    def tool(self):
        tool = WebFetchTool()
        tool.firecrawl_provider = None
        tool.exa_api_key = None
        return tool

    def _mock_fetch(self, content: str):
        """Return an async mock for _fetch_pure_python that returns given content."""
        async def mock_fetch(url, max_length, subpages=0):
            return ToolResult(
                success=True,
                output={
                    "url": url,
                    "title": "Test",
                    "content": content,
                    "char_count": len(content),
                    "word_count": len(content.split()),
                    "truncated": False,
                    "method": "pure_python",
                    "pages_fetched": 1,
                    "tool_source": "fetch",
                    "status_code": 200,
                    "blocked_reason": None,
                },
                metadata={"fetch_method": "pure_python"},
            )
        return mock_fetch

    @pytest.mark.asyncio
    async def test_single_url_skips_extraction_in_research_mode(self, tool):
        """research_mode=True => extract_with_llm NOT called, content hard-truncated."""
        long_content = "x" * 20000
        max_length = 10000

        with patch.object(tool, "_fetch_pure_python", side_effect=self._mock_fetch(long_content)), \
             patch("llm_service.tools.builtin.web_fetch.apply_extraction") as mock_apply, \
             patch("llm_service.tools.builtin.web_fetch.extract_with_llm") as mock_extract:
            result = await tool.execute(
                session_context={"research_mode": True},
                url="https://example.com",
                max_length=max_length,
            )

        # Extraction must NOT be called
        mock_apply.assert_not_called()
        mock_extract.assert_not_called()

        # Content should be hard-truncated to max_length
        assert result.success
        assert len(result.output["content"]) == max_length
        assert result.output["truncated"] is True

    @pytest.mark.asyncio
    async def test_single_url_still_extracts_without_research_mode(self, tool):
        """No research_mode => apply_extraction IS called (existing behavior)."""
        long_content = "x" * 20000

        manager = _mock_llm_manager()
        with patch.object(tool, "_fetch_pure_python", side_effect=self._mock_fetch(long_content)), \
             patch("llm_provider.manager.get_llm_manager", return_value=manager):
            result = await tool.execute(
                url="https://example.com",
                max_length=10000,
            )

        # Extraction should have run (generic extraction on over-length content)
        assert result.success
        assert result.output.get("extracted") is True
        manager.complete.assert_called_once()

    @pytest.mark.asyncio
    async def test_batch_skips_extraction_in_research_mode(self):
        """Batch mode: research_mode=True => no per-page LLM extraction."""
        tool = WebFetchTool()
        long_content = "x" * 20000

        with patch.object(tool, "_fetch_pure_python", new_callable=AsyncMock) as mock_fetch:
            mock_fetch.return_value = ToolResult(
                success=True,
                output={
                    "url": "https://example.com",
                    "title": "Test",
                    "content": long_content,
                    "char_count": len(long_content),
                    "method": "pure_python",
                    "status_code": 200,
                    "blocked_reason": None,
                },
            )

            with patch(
                "llm_service.tools.builtin.web_fetch.extract_with_llm",
                new_callable=AsyncMock,
            ) as mock_extract:
                result = await tool._fetch_batch(
                    ["https://example.com/1", "https://example.com/2"],
                    session_context={"research_mode": True},
                    max_length=10000,
                )

                # extract_with_llm should NOT be called for batch pages
                mock_extract.assert_not_called()
                assert result.success

    @pytest.mark.asyncio
    async def test_extract_prompt_ignored_in_research_mode(self, tool):
        """extract_prompt set + research_mode=True => extraction still skipped (issue #46)."""
        long_content = "x" * 20000

        manager = _mock_llm_manager()
        with patch.object(tool, "_fetch_pure_python", side_effect=self._mock_fetch(long_content)), \
             patch("llm_provider.manager.get_llm_manager", return_value=manager):
            result = await tool.execute(
                session_context={"research_mode": True},
                url="https://example.com",
                max_length=10000,
                extract_prompt="extract pricing info",
            )

        # Research mode always skips extraction, even with extract_prompt
        assert result.success
        assert result.output.get("extracted") is not True
        manager.complete.assert_not_called()
