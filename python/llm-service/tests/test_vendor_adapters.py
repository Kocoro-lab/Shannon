"""Tests for vendor adapter pattern and loading."""

import pytest
from llm_service.tools.vendor_adapters import get_vendor_adapter


class TestVendorAdapterRegistry:
    """Test vendor adapter registry and loading."""

    def test_get_vendor_adapter_returns_none_for_empty(self):
        """get_vendor_adapter should return None for empty string."""
        adapter = get_vendor_adapter("")
        assert adapter is None

    def test_get_vendor_adapter_returns_none_for_none(self):
        """get_vendor_adapter should return None for None input."""
        adapter = get_vendor_adapter(None)
        assert adapter is None

    def test_get_vendor_adapter_returns_none_for_unknown(self):
        """get_vendor_adapter should return None for unknown vendor."""
        adapter = get_vendor_adapter("unknown_vendor_12345")
        assert adapter is None

    def test_vendor_adapter_graceful_fallback(self):
        """get_vendor_adapter should not raise exceptions on import errors."""
        # Should not raise even if vendor module doesn't exist
        try:
            adapter = get_vendor_adapter("nonexistent_vendor")
            assert adapter is None
        except Exception as e:
            pytest.fail(f"get_vendor_adapter raised exception: {e}")


class MockVendorAdapter:
    """Mock vendor adapter for testing."""

    def transform_body(self, body, operation_id, prompt_params=None):
        """Mock transformation that prefixes metric names."""
        if not isinstance(body, dict):
            return body

        # Transform metrics
        if "metrics" in body and isinstance(body["metrics"], list):
            body["metrics"] = [f"mock:{m}" for m in body["metrics"]]

        # Inject session params
        if prompt_params:
            for key, value in prompt_params.items():
                if key not in body or not body[key]:
                    body[key] = value

        # Normalize time range
        if "timeRange" in body and isinstance(body["timeRange"], dict):
            tr = body["timeRange"]
            if "start" in tr and "startTime" not in tr:
                tr["startTime"] = tr.pop("start")
            if "end" in tr and "endTime" not in tr:
                tr["endTime"] = tr.pop("end")

        return body


class TestVendorAdapterInterface:
    """Test vendor adapter transformation interface."""

    def test_adapter_metric_transformation(self):
        """Test adapter transforms metric names."""
        adapter = MockVendorAdapter()
        body = {"metrics": ["users", "sessions", "pageviews"]}

        result = adapter.transform_body(body, "queryMetrics")

        assert result["metrics"] == ["mock:users", "mock:sessions", "mock:pageviews"]

    def test_adapter_session_param_injection(self):
        """Test adapter injects session parameters."""
        adapter = MockVendorAdapter()
        body = {"metrics": ["users"]}
        prompt_params = {
            "account_id": "acct_123",
            "user_id": "user_456",
            "profile_id": "prof_789",
        }

        result = adapter.transform_body(body, "queryMetrics", prompt_params)

        assert result["account_id"] == "acct_123"
        assert result["user_id"] == "user_456"
        assert result["profile_id"] == "prof_789"

    def test_adapter_does_not_override_existing_fields(self):
        """Test adapter doesn't override existing field values."""
        adapter = MockVendorAdapter()
        body = {"metrics": ["users"], "account_id": "existing_account"}
        prompt_params = {"account_id": "new_account", "user_id": "user_123"}

        result = adapter.transform_body(body, "queryMetrics", prompt_params)

        # Should not override existing account_id
        assert result["account_id"] == "existing_account"
        # Should inject missing user_id
        assert result["user_id"] == "user_123"

    def test_adapter_time_range_normalization(self):
        """Test adapter normalizes time range format."""
        adapter = MockVendorAdapter()
        body = {
            "metrics": ["users"],
            "timeRange": {"start": "2025-01-01", "end": "2025-01-31"},
        }

        result = adapter.transform_body(body, "queryMetrics")

        assert "startTime" in result["timeRange"]
        assert "endTime" in result["timeRange"]
        assert result["timeRange"]["startTime"] == "2025-01-01"
        assert result["timeRange"]["endTime"] == "2025-01-31"
        assert "start" not in result["timeRange"]
        assert "end" not in result["timeRange"]

    def test_adapter_handles_non_dict_body(self):
        """Test adapter handles non-dict body gracefully."""
        adapter = MockVendorAdapter()

        # String body
        result = adapter.transform_body("string_body", "operation")
        assert result == "string_body"

        # None body
        result = adapter.transform_body(None, "operation")
        assert result is None

        # List body
        result = adapter.transform_body([1, 2, 3], "operation")
        assert result == [1, 2, 3]

    def test_adapter_handles_missing_fields(self):
        """Test adapter handles missing fields gracefully."""
        adapter = MockVendorAdapter()
        body = {}  # Empty body

        # Should not raise
        result = adapter.transform_body(body, "queryMetrics")
        assert isinstance(result, dict)

    def test_adapter_operation_specific_logic(self):
        """Test adapter can apply different logic per operation."""
        adapter = MockVendorAdapter()
        body1 = {"metrics": ["users"]}
        body2 = {"metrics": ["users"]}

        result1 = adapter.transform_body(body1, "queryMetrics")
        result2 = adapter.transform_body(body2, "getDimensionValues")

        # Both should have the transformation
        assert result1["metrics"] == ["mock:users"]
        assert result2["metrics"] == ["mock:users"]


class TestComplexVendorAdapter:
    """Test complex vendor adapter scenarios."""

    class ComplexVendorAdapter:
        """Vendor adapter with more complex transformations."""

        def transform_body(self, body, operation_id, prompt_params=None):
            if not isinstance(body, dict):
                return body

            # Field aliasing
            if operation_id == "queryData":
                self._alias_fields(body)

            # Sort normalization
            if "sort" in body:
                body["sort"] = self._normalize_sort(body["sort"])

            # Filter normalization
            if "filters" in body:
                body["filters"] = self._normalize_filters(body["filters"])

            return body

        def _alias_fields(self, body):
            """Alias field names."""
            aliases = {
                "users": "analytics:unique_users",
                "sessions": "analytics:total_sessions",
                "revenue": "analytics:total_revenue",
            }

            if "metrics" in body and isinstance(body["metrics"], list):
                body["metrics"] = [aliases.get(m, m) for m in body["metrics"]]

        def _normalize_sort(self, sort):
            """Normalize sort to consistent format."""
            if isinstance(sort, dict):
                field = sort.get("field") or sort.get("field_name")
                order = (sort.get("order") or sort.get("direction") or "DESC").upper()
                return {"field": field, "order": order}
            elif isinstance(sort, list) and sort:
                return [self._normalize_sort(s) for s in sort]
            return sort

        def _normalize_filters(self, filters):
            """Normalize filters to object with logic and conditions."""
            if isinstance(filters, list):
                return {"logic": "and", "conditions": filters}
            return filters

    def test_complex_field_aliasing(self):
        """Test complex field aliasing."""
        adapter = self.ComplexVendorAdapter()
        body = {"metrics": ["users", "sessions", "revenue"]}

        result = adapter.transform_body(body, "queryData")

        assert result["metrics"] == [
            "analytics:unique_users",
            "analytics:total_sessions",
            "analytics:total_revenue",
        ]

    def test_complex_sort_normalization(self):
        """Test complex sort normalization."""
        adapter = self.ComplexVendorAdapter()

        # Test dict sort with various field names
        body1 = {"sort": {"field_name": "date", "sort_by": "asc"}}
        result1 = adapter.transform_body(body1, "queryData")
        assert result1["sort"] == {"field": "date", "order": "ASC"}

        # Test dict sort with different keys
        body2 = {"sort": {"field": "revenue", "direction": "desc"}}
        result2 = adapter.transform_body(body2, "queryData")
        assert result2["sort"] == {"field": "revenue", "order": "DESC"}

        # Test list of sorts
        body3 = {
            "sort": [
                {"field": "date", "order": "ASC"},
                {"field": "revenue", "order": "DESC"},
            ]
        }
        result3 = adapter.transform_body(body3, "queryData")
        assert len(result3["sort"]) == 2
        assert result3["sort"][0]["order"] == "ASC"
        assert result3["sort"][1]["order"] == "DESC"

    def test_complex_filter_normalization(self):
        """Test complex filter normalization."""
        adapter = self.ComplexVendorAdapter()

        # List filters should be wrapped in object
        body = {
            "filters": [
                {"field": "country", "operator": "eq", "value": "US"},
                {"field": "revenue", "operator": "gt", "value": 100},
            ]
        }

        result = adapter.transform_body(body, "queryData")

        assert result["filters"]["logic"] == "and"
        assert len(result["filters"]["conditions"]) == 2
        assert result["filters"]["conditions"][0]["field"] == "country"


class TestVendorAdapterEdgeCases:
    """Test edge cases and error handling."""

    def test_adapter_with_none_prompt_params(self):
        """Test adapter handles None prompt_params."""
        adapter = MockVendorAdapter()
        body = {"metrics": ["users"]}

        result = adapter.transform_body(body, "queryMetrics", None)

        assert result["metrics"] == ["mock:users"]

    def test_adapter_with_empty_prompt_params(self):
        """Test adapter handles empty prompt_params."""
        adapter = MockVendorAdapter()
        body = {"metrics": ["users"]}

        result = adapter.transform_body(body, "queryMetrics", {})

        assert result["metrics"] == ["mock:users"]

    def test_adapter_preserves_complex_nested_structures(self):
        """Test adapter preserves complex nested data structures."""
        adapter = MockVendorAdapter()
        body = {
            "metrics": ["users"],
            "nested": {
                "level1": {"level2": {"level3": "value"}},
                "array": [1, 2, 3],
            },
        }

        result = adapter.transform_body(body, "queryMetrics")

        assert result["nested"]["level1"]["level2"]["level3"] == "value"
        assert result["nested"]["array"] == [1, 2, 3]


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
