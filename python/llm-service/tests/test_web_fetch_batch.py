"""
Tests for web_fetch batch URL support

Tests the batch mode functionality where multiple URLs can be fetched in parallel
using the urls=[] parameter instead of making multiple single-URL calls.
"""

import pytest
from unittest.mock import AsyncMock, MagicMock, patch
import asyncio

from llm_service.tools.builtin.web_fetch import WebFetchTool
from llm_service.tools.base import ToolResult


class TestWebFetchParameterValidation:
    """Tests for parameter validation (url vs urls)"""

    @pytest.fixture
    def tool(self):
        """Create WebFetchTool instance"""
        return WebFetchTool()

    @pytest.mark.asyncio
    async def test_error_when_neither_url_nor_urls_provided(self, tool):
        """Should error when neither url nor urls are provided"""
        result = await tool.execute()
        assert result.success is False
        assert "url or urls" in result.error.lower()

    @pytest.mark.asyncio
    async def test_error_when_both_url_and_urls_provided(self, tool):
        """Should error when both url and urls are provided"""
        result = await tool.execute(
            url="https://example.com",
            urls=["https://a.com", "https://b.com"]
        )
        assert result.success is False
        assert "url OR urls" in result.error

    @pytest.mark.asyncio
    async def test_error_when_urls_is_empty_list(self, tool):
        """Should error when urls is an empty list"""
        result = await tool.execute(urls=[])
        assert result.success is False
        # Empty list is falsy, so error says "Must provide url or urls"
        assert "url" in result.error.lower()

    @pytest.mark.asyncio
    async def test_error_when_urls_has_invalid_entries(self, tool):
        """Should error when all URLs in list are invalid"""
        result = await tool.execute(urls=["", None, "not-a-url"])
        assert result.success is False


class TestWebFetchSingleUrlMode:
    """Tests for single URL mode (backwards compatibility)"""

    @pytest.fixture
    def tool(self):
        """Create WebFetchTool instance"""
        return WebFetchTool()

    @pytest.mark.asyncio
    async def test_single_url_mode_still_works(self, tool):
        """Single URL mode should work as before"""
        mock_result = ToolResult(
            success=True,
            output={
                "url": "https://example.com",
                "title": "Example",
                "content": "Hello World",
                "word_count": 2,
                "char_count": 11,
            }
        )

        with patch.object(tool, '_fetch_pure_python', return_value=mock_result):
            result = await tool.execute(url="https://example.com")
            assert result.success is True
            # Single URL mode returns the output directly (not in pages[])
            assert "url" in result.output or "pages" not in result.output


class TestWebFetchBatchMode:
    """Tests for batch URL fetching"""

    @pytest.fixture
    def tool(self):
        """Create WebFetchTool instance"""
        return WebFetchTool()

    @pytest.mark.asyncio
    async def test_batch_mode_fetches_all_urls(self, tool):
        """Batch mode should fetch all URLs and return aggregated results"""
        test_urls = ["https://a.com", "https://b.com", "https://c.com"]

        async def mock_fetch(url, max_length, subpages=0):
            return ToolResult(
                success=True,
                output={
                    "url": url,
                    "title": f"Title for {url}",
                    "content": f"Content from {url}",
                }
            )

        with patch.object(tool, '_fetch_pure_python', side_effect=mock_fetch):
            result = await tool.execute(urls=test_urls)

            assert result.success is True
            assert result.output["succeeded"] == 3
            assert result.output["failed"] == 0
            assert len(result.output["pages"]) == 3
            assert result.output["partial_success"] is False

    @pytest.mark.asyncio
    async def test_batch_mode_partial_success(self, tool):
        """Batch mode should handle partial failures correctly"""
        test_urls = ["https://good.com", "https://bad.com"]

        call_count = 0

        async def mock_fetch(url, max_length, subpages=0):
            nonlocal call_count
            call_count += 1
            if "good" in url:
                return ToolResult(
                    success=True,
                    output={"url": url, "content": "OK"}
                )
            else:
                return ToolResult(
                    success=False,
                    error="Connection failed"
                )

        with patch.object(tool, '_fetch_pure_python', side_effect=mock_fetch):
            result = await tool.execute(urls=test_urls)

            assert result.success is True  # At least 1 succeeded
            assert result.output["succeeded"] == 1
            assert result.output["failed"] == 1
            assert result.output["partial_success"] is True

    @pytest.mark.asyncio
    async def test_batch_mode_all_fail(self, tool):
        """Batch mode should return failure when all URLs fail"""
        test_urls = ["https://bad1.com", "https://bad2.com"]

        async def mock_fetch(url, max_length, subpages=0):
            return ToolResult(success=False, error="Failed")

        with patch.object(tool, '_fetch_pure_python', side_effect=mock_fetch):
            result = await tool.execute(urls=test_urls)

            assert result.success is False
            assert result.output["succeeded"] == 0
            assert result.output["failed"] == 2
            assert "All URLs failed" in result.error

    @pytest.mark.asyncio
    async def test_batch_mode_respects_total_chars_cap(self, tool):
        """Batch mode should stop when total_chars_cap is reached"""
        test_urls = [
            "https://a.com",
            "https://b.com",
            "https://c.com",
            "https://d.com",
        ]

        async def mock_fetch(url, max_length, subpages=0):
            # Each returns 15000 chars
            return ToolResult(
                success=True,
                output={
                    "url": url,
                    "content": "x" * 15000,
                }
            )

        with patch.object(tool, '_fetch_pure_python', side_effect=mock_fetch):
            # Set cap to 40000 - should stop after 2-3 URLs
            result = await tool.execute(urls=test_urls, total_chars_cap=40000)

            assert result.success is True
            # Should have stopped before fetching all 4
            assert result.output["total_chars"] <= 60000  # Allow some buffer

    @pytest.mark.asyncio
    async def test_batch_mode_concurrency_limit(self, tool):
        """Batch mode should respect concurrency limit"""
        concurrent_count = 0
        max_concurrent = 0

        async def mock_fetch(url, max_length, subpages=0):
            nonlocal concurrent_count, max_concurrent
            concurrent_count += 1
            max_concurrent = max(max_concurrent, concurrent_count)
            await asyncio.sleep(0.05)  # Simulate network delay
            concurrent_count -= 1
            return ToolResult(
                success=True,
                output={"url": url, "content": "x"}
            )

        test_urls = [f"https://site{i}.com" for i in range(10)]

        with patch.object(tool, '_fetch_pure_python', side_effect=mock_fetch):
            await tool.execute(urls=test_urls, concurrency=3)

        # Should never exceed concurrency limit
        assert max_concurrent <= 3

    @pytest.mark.asyncio
    async def test_batch_mode_skips_invalid_urls(self, tool):
        """Batch mode should skip invalid URLs but process valid ones"""
        test_urls = [
            "https://valid.com",
            "not-a-url",
            "ftp://invalid-scheme.com",
            "https://also-valid.com",
        ]

        async def mock_fetch(url, max_length, subpages=0):
            return ToolResult(
                success=True,
                output={"url": url, "content": "OK"}
            )

        with patch.object(tool, '_fetch_pure_python', side_effect=mock_fetch):
            result = await tool.execute(urls=test_urls)

            # Should only process the 2 valid https URLs
            assert result.success is True
            assert result.output["succeeded"] == 2

    @pytest.mark.asyncio
    async def test_batch_mode_returns_correct_metadata(self, tool):
        """Batch mode should return proper metadata"""
        test_urls = ["https://a.com", "https://b.com"]

        async def mock_fetch(url, max_length, subpages=0):
            return ToolResult(
                success=True,
                output={"url": url, "content": "OK"}
            )

        with patch.object(tool, '_fetch_pure_python', side_effect=mock_fetch):
            result = await tool.execute(urls=test_urls, concurrency=2)

            assert result.metadata is not None
            assert result.metadata["fetch_method"] == "batch"
            assert result.metadata["concurrency"] == 2
            assert "urls_succeeded" in result.metadata
            assert "urls_failed" in result.metadata


class TestWebFetchToolMetadata:
    """Tests for tool metadata and schema"""

    def test_tool_has_urls_parameter(self):
        """Test that tool schema includes urls parameter"""
        tool = WebFetchTool()
        params = tool.parameters
        param_names = [p.name for p in params]

        assert "urls" in param_names
        assert "url" in param_names

    def test_url_parameter_is_optional(self):
        """Test that url parameter is optional (for batch mode)"""
        tool = WebFetchTool()
        params = tool.parameters

        url_param = next((p for p in params if p.name == "url"), None)
        assert url_param is not None
        assert url_param.required is False

    def test_urls_parameter_is_array_type(self):
        """Test that urls parameter is array type"""
        tool = WebFetchTool()
        params = tool.parameters

        urls_param = next((p for p in params if p.name == "urls"), None)
        assert urls_param is not None
        # ToolParameterType.ARRAY should be the type
        assert "array" in str(urls_param.type).lower()

    def test_tool_has_batch_parameters(self):
        """Test that tool schema includes batch-related parameters"""
        tool = WebFetchTool()
        params = tool.parameters
        param_names = [p.name for p in params]

        assert "concurrency" in param_names
        assert "total_chars_cap" in param_names

    def test_tool_description_mentions_batch_mode(self):
        """Test that tool description explains batch mode"""
        tool = WebFetchTool()
        metadata = tool.metadata

        assert "batch" in metadata.description.lower() or "urls" in metadata.description.lower()
        assert "BATCH MODE" in metadata.description or "urls=" in metadata.description


class TestWebFetchSSRFProtection:
    """SSRF protection should apply to both single and batch modes."""

    @pytest.fixture
    def tool(self):
        return WebFetchTool()

    @pytest.mark.asyncio
    async def test_single_url_blocks_private_ip(self, tool):
        result = await tool.execute(url="http://127.0.0.1/")
        assert result.success is False
        assert result.error is not None
        assert "not allowed" in result.error.lower() or "private" in result.error.lower()

    @pytest.mark.asyncio
    async def test_batch_mode_blocks_private_ip(self, tool):
        result = await tool.execute(urls=["http://127.0.0.1/"])
        assert result.success is False
        assert result.output is not None
        assert result.output["succeeded"] == 0
        assert result.output["failed"] == 1
        assert "private" in result.output["pages"][0]["error"].lower() or "not allowed" in result.output["pages"][0]["error"].lower()
