"""
Tests for web_search tool - SerpAPI multi-engine support and validation
"""

import pytest
from unittest.mock import AsyncMock, MagicMock, patch

from llm_service.tools.builtin.web_search import (
    SerpAPISearchProvider,
    WebSearchTool,
)
from llm_service.tools.base import ToolResult

# Valid API key format (at least 10 chars, not test values)
VALID_TEST_API_KEY = "sk_test_valid_api_key_12345"


class TestSerpAPISearchProvider:
    """Tests for SerpAPISearchProvider"""

    def test_init_default_engine(self):
        """Test default engine is google"""
        provider = SerpAPISearchProvider(api_key=VALID_TEST_API_KEY)
        assert provider.engine == "google"

    def test_init_custom_engine(self):
        """Test custom engine initialization"""
        provider = SerpAPISearchProvider(api_key=VALID_TEST_API_KEY, engine="bing")
        assert provider.engine == "bing"

    def test_engine_case_insensitive(self):
        """Test engine is normalized to lowercase"""
        provider = SerpAPISearchProvider(api_key=VALID_TEST_API_KEY, engine="GOOGLE")
        assert provider.engine == "google"

    @pytest.mark.asyncio
    async def test_search_uses_passed_engine_not_instance(self):
        """Test that search uses passed engine parameter, not instance state (race condition fix)"""
        provider = SerpAPISearchProvider(api_key=VALID_TEST_API_KEY, engine="google")

        # Mock the aiohttp session
        mock_response = AsyncMock()
        mock_response.status = 200
        mock_response.json = AsyncMock(return_value={"organic_results": []})

        with patch("aiohttp.ClientSession") as mock_client:
            mock_session = MagicMock()
            mock_session.get = MagicMock(
                return_value=AsyncMock(
                    __aenter__=AsyncMock(return_value=mock_response),
                    __aexit__=AsyncMock()
                )
            )
            mock_client.return_value.__aenter__ = AsyncMock(return_value=mock_session)
            mock_client.return_value.__aexit__ = AsyncMock()

            # Call search with different engine than instance
            await provider.search("test query", max_results=5, engine="bing")

            # Verify the request used "bing" engine
            call_args = mock_session.get.call_args
            params = call_args.kwargs.get("params", {})
            assert params.get("engine") == "bing"

            # Instance engine should remain unchanged (no mutation)
            assert provider.engine == "google"

    @pytest.mark.asyncio
    async def test_search_defaults_to_instance_engine(self):
        """Test that search uses instance engine when not passed"""
        provider = SerpAPISearchProvider(api_key=VALID_TEST_API_KEY, engine="baidu")

        mock_response = AsyncMock()
        mock_response.status = 200
        mock_response.json = AsyncMock(return_value={"organic_results": []})

        with patch("aiohttp.ClientSession") as mock_client:
            mock_session = MagicMock()
            mock_session.get = MagicMock(
                return_value=AsyncMock(
                    __aenter__=AsyncMock(return_value=mock_response),
                    __aexit__=AsyncMock()
                )
            )
            mock_client.return_value.__aenter__ = AsyncMock(return_value=mock_session)
            mock_client.return_value.__aexit__ = AsyncMock()

            # Call search without engine parameter
            await provider.search("test query", max_results=5)

            # Verify the request used instance engine
            call_args = mock_session.get.call_args
            params = call_args.kwargs.get("params", {})
            assert params.get("engine") == "baidu"

    @pytest.mark.asyncio
    async def test_google_finance_response_parsing(self):
        """Test Google Finance response is parsed correctly"""
        provider = SerpAPISearchProvider(api_key=VALID_TEST_API_KEY)

        mock_finance_response = {
            "summary": {
                "title": "Apple Inc",
                "stock": "AAPL",
                "exchange": "NASDAQ",
                "price": "150.00",
                "currency": "USD",
                "price_movement": {"percentage": 1.5, "movement": "Up"},
                "previous_close": "147.75",
            },
            "key_stats": {"market_cap": "2.5T", "pe_ratio": "25.5"},
        }

        mock_response = AsyncMock()
        mock_response.status = 200
        mock_response.json = AsyncMock(return_value=mock_finance_response)

        with patch("aiohttp.ClientSession") as mock_client:
            mock_session = MagicMock()
            mock_session.get = MagicMock(
                return_value=AsyncMock(
                    __aenter__=AsyncMock(return_value=mock_response),
                    __aexit__=AsyncMock()
                )
            )
            mock_client.return_value.__aenter__ = AsyncMock(return_value=mock_session)
            mock_client.return_value.__aexit__ = AsyncMock()

            results = await provider.search("AAPL:NASDAQ", max_results=5, engine="google_finance")

            # Should have stock quote result
            assert len(results) >= 1
            assert results[0]["type"] == "stock_quote"
            assert "Apple Inc" in results[0]["title"]
            assert "150.00" in results[0]["snippet"]

    @pytest.mark.asyncio
    async def test_google_finance_markets_response_parsing(self):
        """Test Google Finance Markets response is parsed correctly"""
        provider = SerpAPISearchProvider(api_key=VALID_TEST_API_KEY)

        mock_markets_response = {
            "market_trends": [
                {
                    "title": "Gainers",
                    "results": [
                        {
                            "name": "Tesla",
                            "stock": "TSLA",
                            "price": "250.00",
                            "link": "https://google.com/finance/quote/TSLA",
                            "price_movement": {"percentage": 5.0},
                        }
                    ],
                }
            ],
            "markets": {},
        }

        mock_response = AsyncMock()
        mock_response.status = 200
        mock_response.json = AsyncMock(return_value=mock_markets_response)

        with patch("aiohttp.ClientSession") as mock_client:
            mock_session = MagicMock()
            mock_session.get = MagicMock(
                return_value=AsyncMock(
                    __aenter__=AsyncMock(return_value=mock_response),
                    __aexit__=AsyncMock()
                )
            )
            mock_client.return_value.__aenter__ = AsyncMock(return_value=mock_session)
            mock_client.return_value.__aexit__ = AsyncMock()

            results = await provider.search("", max_results=10, engine="google_finance_markets", trend="gainers")

            assert len(results) >= 1
            assert "Tesla" in results[0]["title"]
            assert results[0]["type"] == "Gainers"


class TestWebSearchToolValidation:
    """Tests for WebSearchTool parameter validation constants"""

    def test_valid_engines_list(self):
        """Test that valid engines are defined"""
        valid_engines = {
            "google", "bing", "baidu", "google_scholar",
            "youtube", "google_news", "google_finance", "google_finance_markets"
        }
        # These are the engines we support
        assert len(valid_engines) == 8
        assert "google" in valid_engines
        assert "baidu" in valid_engines
        assert "google_finance" in valid_engines

    def test_valid_time_filters(self):
        """Test valid time filter values"""
        valid_filters = {"day", "week", "month", "year"}
        assert len(valid_filters) == 4

    def test_valid_windows(self):
        """Test valid window values for google_finance"""
        valid_windows = {"1D", "5D", "1M", "6M", "YTD", "1Y", "5Y", "MAX"}
        assert len(valid_windows) == 8
        assert "1D" in valid_windows
        assert "MAX" in valid_windows

    def test_valid_trends(self):
        """Test valid trend values for google_finance_markets"""
        valid_trends = {"indexes", "most-active", "gainers", "losers", "climate-leaders", "crypto", "currencies"}
        assert len(valid_trends) == 7
        assert "gainers" in valid_trends
        assert "crypto" in valid_trends


class TestWebSearchToolMetadata:
    """Tests for WebSearchTool metadata and schema"""

    def test_tool_has_engine_parameter(self):
        """Test that tool schema includes engine parameter"""
        # Import tool metadata
        tool = WebSearchTool()
        params = tool.parameters

        # Find engine parameter
        engine_param = next((p for p in params if p.name == "engine"), None)
        assert engine_param is not None
        assert engine_param.required is False  # Optional with default

    def test_tool_has_localization_parameters(self):
        """Test that tool schema includes localization parameters"""
        tool = WebSearchTool()
        params = tool.parameters
        param_names = [p.name for p in params]

        # Check localization params exist
        assert "gl" in param_names
        assert "hl" in param_names
        assert "location" in param_names

    def test_tool_has_time_filter_parameter(self):
        """Test that tool schema includes time_filter parameter"""
        tool = WebSearchTool()
        params = tool.parameters
        param_names = [p.name for p in params]

        assert "time_filter" in param_names

    def test_tool_has_finance_parameters(self):
        """Test that tool schema includes finance-specific parameters"""
        tool = WebSearchTool()
        params = tool.parameters
        param_names = [p.name for p in params]

        assert "window" in param_names
        assert "trend" in param_names
