"""Tests for GA4 client filter validation and building."""

import pytest
from unittest.mock import Mock, patch, MagicMock


class TestGA4FilterValidation:
    """Test GA4 filter validation logic."""

    @pytest.fixture
    def client(self):
        """Create a GA4Client with mocked credentials."""
        with patch(
            "llm_service.tools.vendor_adapters.ga4.client.service_account.Credentials.from_service_account_file"
        ) as mock_creds, patch(
            "llm_service.tools.vendor_adapters.ga4.client.BetaAnalyticsDataClient"
        ) as mock_client:
            mock_creds.return_value = Mock()
            mock_client.return_value = Mock()

            from llm_service.tools.vendor_adapters.ga4.client import GA4Client

            return GA4Client(property_id="123456789", credentials_path="/fake/path.json")

    def test_validate_empty_filter(self, client):
        """Empty filter should pass validation."""
        client._validate_filter({})
        client._validate_filter(None)

    def test_validate_simple_leaf_filter(self, client):
        """Simple leaf filter with field and match_type should pass."""
        client._validate_filter({"field": "country", "value": "US", "match_type": "EXACT"})
        client._validate_filter({"field": "city", "value": "Tokyo", "match_type": "CONTAINS"})

    def test_validate_invalid_match_type(self, client):
        """Invalid match_type should raise ValueError."""
        with pytest.raises(ValueError) as exc:
            client._validate_filter({"field": "country", "value": "US", "match_type": "REGEXP"})
        assert "REGEXP" in str(exc.value)
        assert "NOT supported" in str(exc.value)

    def test_validate_in_operator_requires_values_list(self, client):
        """IN operator requires 'values' list."""
        with pytest.raises(ValueError) as exc:
            client._validate_filter({"field": "country", "operator": "IN", "value": "US"})
        assert "values" in str(exc.value).lower()

    def test_validate_in_operator_with_values(self, client):
        """IN operator with values list should pass."""
        client._validate_filter({"field": "country", "operator": "IN", "values": ["US", "JP", "GB"]})

    def test_validate_and_group(self, client):
        """AND group with expressions should pass."""
        client._validate_filter(
            {
                "and": [
                    {"field": "country", "value": "US", "match_type": "EXACT"},
                    {"field": "city", "value": "NYC", "match_type": "CONTAINS"},
                ]
            }
        )

    def test_validate_and_group_empty_fails(self, client):
        """AND group with empty expressions should fail."""
        with pytest.raises(ValueError) as exc:
            client._validate_filter({"and": []})
        assert "non-empty list" in str(exc.value)

    def test_validate_or_group(self, client):
        """OR group with expressions should pass."""
        client._validate_filter(
            {
                "or": [
                    {"field": "country", "value": "US", "match_type": "EXACT"},
                    {"field": "country", "value": "JP", "match_type": "EXACT"},
                ]
            }
        )

    def test_validate_not_expression(self, client):
        """NOT expression with inner filter should pass."""
        client._validate_filter(
            {"not": {"field": "country", "value": "US", "match_type": "EXACT"}}
        )

    def test_validate_not_expression_requires_object(self, client):
        """NOT expression requires an object."""
        with pytest.raises(ValueError) as exc:
            client._validate_filter({"not": "invalid"})
        assert "object" in str(exc.value)

    def test_validate_canonical_and_group(self, client):
        """Canonical GA4 andGroup format should pass."""
        client._validate_filter(
            {
                "andGroup": {
                    "expressions": [
                        {
                            "filter": {
                                "fieldName": "country",
                                "stringFilter": {"value": "US", "matchType": "EXACT"},
                            }
                        }
                    ]
                }
            }
        )

    def test_validate_canonical_filter_requires_field(self, client):
        """Canonical filter must include fieldName or field."""
        with pytest.raises(ValueError) as exc:
            client._validate_filter(
                {"filter": {"stringFilter": {"value": "US", "matchType": "EXACT"}}}
            )
        assert "fieldName" in str(exc.value) or "field" in str(exc.value)

    def test_validate_canonical_filter_requires_type(self, client):
        """Canonical filter must include a filter type."""
        with pytest.raises(ValueError) as exc:
            client._validate_filter({"filter": {"fieldName": "country"}})
        assert "stringFilter" in str(exc.value)

    def test_validate_canonical_inlist_requires_values(self, client):
        """Canonical inListFilter requires non-empty values."""
        with pytest.raises(ValueError) as exc:
            client._validate_filter(
                {"filter": {"fieldName": "country", "inListFilter": {"values": []}}}
            )
        assert "non-empty list" in str(exc.value)

    def test_validate_nested_complex_filter(self, client):
        """Complex nested filter should pass validation."""
        client._validate_filter(
            {
                "and": [
                    {
                        "or": [
                            {"field": "country", "value": "US", "match_type": "EXACT"},
                            {"field": "country", "value": "JP", "match_type": "EXACT"},
                        ]
                    },
                    {"not": {"field": "city", "value": "Unknown", "match_type": "EXACT"}},
                ]
            }
        )


class TestGA4ClientVendorAdapter:
    """Test vendor adapter loading error handling."""

    @pytest.fixture
    def client(self):
        """Create a GA4Client with mocked credentials."""
        with patch(
            "llm_service.tools.vendor_adapters.ga4.client.service_account.Credentials.from_service_account_file"
        ) as mock_creds, patch(
            "llm_service.tools.vendor_adapters.ga4.client.BetaAnalyticsDataClient"
        ) as mock_client:
            mock_creds.return_value = Mock()
            mock_client.return_value = Mock()

            from llm_service.tools.vendor_adapters.ga4.client import GA4Client

            return GA4Client(property_id="123456789", credentials_path="/fake/path.json")

    def test_vendor_adapter_returns_none_when_config_missing(self, client):
        """Vendor adapter should return None when config file missing."""
        with patch("os.path.exists", return_value=False):
            result = client._get_vendor_adapter()
            assert result is None

    def test_vendor_adapter_returns_none_when_not_configured(self, client):
        """Vendor adapter should return None when not configured."""
        with patch("os.path.exists", return_value=True), patch(
            "builtins.open", MagicMock()
        ), patch(
            "llm_service.tools.vendor_adapters.ga4.client.yaml.safe_load",
            return_value={"ga4": {}},
        ):
            result = client._get_vendor_adapter()
            assert result is None

    def test_vendor_adapter_caches_result(self, client):
        """Vendor adapter loading should be cached."""
        with patch("os.path.exists", return_value=False):
            client._get_vendor_adapter()
            assert client._adapter_loaded is True

            # Second call should not check os.path.exists again
            with patch("os.path.exists") as mock_exists:
                client._get_vendor_adapter()
                mock_exists.assert_not_called()


class TestRealtimeValidFields:
    """Test realtime API field validation."""

    def test_realtime_dimensions_frozen(self):
        """Realtime dimensions should be a frozenset."""
        from llm_service.tools.vendor_adapters.ga4.client import REALTIME_VALID_DIMENSIONS

        assert isinstance(REALTIME_VALID_DIMENSIONS, frozenset)
        assert "activeUsers" not in REALTIME_VALID_DIMENSIONS  # That's a metric
        assert "country" in REALTIME_VALID_DIMENSIONS

    def test_realtime_metrics_frozen(self):
        """Realtime metrics should be a frozenset."""
        from llm_service.tools.vendor_adapters.ga4.client import REALTIME_VALID_METRICS

        assert isinstance(REALTIME_VALID_METRICS, frozenset)
        assert "activeUsers" in REALTIME_VALID_METRICS
        assert "country" not in REALTIME_VALID_METRICS  # That's a dimension


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
