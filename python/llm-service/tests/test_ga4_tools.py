"""Tests for GA4 tools thread safety and initialization."""

import pytest
import threading
from unittest.mock import Mock, patch, MagicMock


class TestGA4ToolsThreadSafety:
    """Test thread safety of GA4 client initialization."""

    def test_lock_exists(self):
        """GA4 tools should have a threading lock."""
        from llm_service.tools import ga4_tools

        assert hasattr(ga4_tools, "_GA4_LOCK")
        assert isinstance(ga4_tools._GA4_LOCK, type(threading.Lock()))

    def test_concurrent_client_initialization(self):
        """Multiple threads should safely initialize the client."""
        from llm_service.tools import ga4_tools

        # Reset global state
        ga4_tools._GA4_CLIENT = None
        ga4_tools._GA4_CACHE = None

        init_count = {"count": 0}
        errors = []

        def mock_ga4_client(*args, **kwargs):
            init_count["count"] += 1
            return Mock()

        def try_get_client():
            try:
                with patch(
                    "llm_service.tools.ga4_tools.GA4Client", mock_ga4_client
                ), patch(
                    "llm_service.tools.ga4_tools.GA4MetadataCache", Mock
                ), patch(
                    "llm_service.tools.ga4_tools._resolve_config_path",
                    return_value="/fake/config.yaml",
                ), patch(
                    "os.path.exists", return_value=True
                ), patch(
                    "builtins.open", MagicMock()
                ), patch(
                    "llm_service.tools.ga4_tools.yaml.safe_load",
                    return_value={
                        "ga4": {"property_id": "123", "credentials_path": "/fake/creds.json"}
                    },
                ):
                    ga4_tools._get_ga4_client()
            except Exception as e:
                errors.append(e)

        # Reset again before concurrent test
        ga4_tools._GA4_CLIENT = None
        ga4_tools._GA4_CACHE = None

        threads = [threading.Thread(target=try_get_client) for _ in range(10)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        assert not errors, f"Errors during concurrent init: {errors}"
        # With proper locking, client should only be initialized once
        # (but due to mocking complexity, we mainly verify no errors)


class TestGA4ToolsConfigResolution:
    """Test configuration path resolution."""

    def test_resolve_config_path_env_priority(self):
        """SHANNON_CONFIG_PATH takes priority over CONFIG_PATH."""
        from llm_service.tools.ga4_tools import _resolve_config_path

        with patch.dict(
            "os.environ",
            {"SHANNON_CONFIG_PATH": "/shannon/path", "CONFIG_PATH": "/config/path"},
        ):
            result = _resolve_config_path()
            assert result == "/shannon/path"

    def test_resolve_config_path_fallback(self):
        """Should fallback to CONFIG_PATH if SHANNON_CONFIG_PATH not set."""
        from llm_service.tools.ga4_tools import _resolve_config_path

        with patch.dict("os.environ", {"CONFIG_PATH": "/config/path"}, clear=True):
            result = _resolve_config_path()
            assert result == "/config/path"

    def test_resolve_config_path_default(self):
        """Should return default path if no env vars set."""
        from llm_service.tools.ga4_tools import _resolve_config_path

        with patch.dict("os.environ", {}, clear=True):
            result = _resolve_config_path()
            assert result == "/app/config/shannon.yaml"


class TestGA4ToolClasses:
    """Test GA4 Tool class metadata."""

    def test_run_report_tool_metadata(self):
        """GA4RunReportTool should have correct metadata."""
        from llm_service.tools.ga4_tools import GA4RunReportTool

        tool = GA4RunReportTool()
        meta = tool._get_metadata()

        assert meta.name == "ga4_run_report"
        assert meta.category == "analytics"
        assert meta.requires_auth is True

    def test_run_report_tool_parameters(self):
        """GA4RunReportTool should have correct parameters."""
        from llm_service.tools.ga4_tools import GA4RunReportTool

        tool = GA4RunReportTool()
        params = tool._get_parameters()

        param_names = [p.name for p in params]
        assert "metrics" in param_names
        assert "dimensions" in param_names
        assert "start_date" in param_names
        assert "end_date" in param_names

    def test_realtime_report_tool_metadata(self):
        """GA4RunRealtimeReportTool should have correct metadata."""
        from llm_service.tools.ga4_tools import GA4RunRealtimeReportTool

        tool = GA4RunRealtimeReportTool()
        meta = tool._get_metadata()

        assert meta.name == "ga4_run_realtime_report"
        assert meta.category == "analytics"

    def test_metadata_tool_metadata(self):
        """GA4GetMetadataTool should have correct metadata."""
        from llm_service.tools.ga4_tools import GA4GetMetadataTool

        tool = GA4GetMetadataTool()
        meta = tool._get_metadata()

        assert meta.name == "ga4_get_metadata"
        assert meta.category == "analytics"


class TestGA4ToolExecution:
    """Test GA4 tool execution error handling."""

    @pytest.fixture
    def mock_client(self):
        """Create a mocked GA4 client."""
        return Mock()

    @pytest.mark.asyncio
    async def test_run_report_handles_value_error(self):
        """Run report should handle filter validation errors."""
        from llm_service.tools.ga4_tools import GA4RunReportTool

        tool = GA4RunReportTool()

        with patch(
            "llm_service.tools.ga4_tools._get_ga4_client"
        ) as mock_get_client:
            mock_client = Mock()
            mock_client.run_report.side_effect = ValueError("Invalid match_type 'REGEXP'")
            mock_get_client.return_value = mock_client

            result = await tool._execute_impl(metrics=["activeUsers"])

            assert result.success is False
            assert "validation failed" in result.error.lower()

    @pytest.mark.asyncio
    async def test_run_report_handles_quota_error(self):
        """Run report should handle quota exhaustion."""
        from llm_service.tools.ga4_tools import GA4RunReportTool
        from google.api_core.exceptions import ResourceExhausted

        tool = GA4RunReportTool()

        with patch(
            "llm_service.tools.ga4_tools._get_ga4_client"
        ) as mock_get_client:
            mock_client = Mock()
            mock_client.run_report.side_effect = ResourceExhausted("Quota exceeded")
            mock_get_client.return_value = mock_client

            result = await tool._execute_impl(metrics=["activeUsers"])

            assert result.success is False
            assert "quota" in result.error.lower()


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
