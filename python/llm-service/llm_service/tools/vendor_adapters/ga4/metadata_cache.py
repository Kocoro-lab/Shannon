"""GA4 metadata cache for dimension/metric validation.

This module provides caching and formatting of GA4 metadata (available dimensions
and metrics) for use in LLM tool descriptions and validation.
"""

from functools import lru_cache
from typing import TYPE_CHECKING, Dict

from google.analytics.data_v1beta.types import GetMetadataRequest

if TYPE_CHECKING:
    from .client import GA4Client


class GA4MetadataCache:
    """Cache available dimensions/metrics for validation and LLM context."""

    def __init__(self, client: "GA4Client"):
        """Initialize metadata cache.

        Args:
            client: GA4Client instance
        """
        self.client = client

    @lru_cache(maxsize=1)
    def get_metadata(self) -> Dict[str, Dict]:
        """Fetch and cache available dimensions/metrics.

        This method is cached to avoid repeated API calls. The cache is
        automatically invalidated when the instance is recreated.

        Returns:
            Dict with 'dimensions' and 'metrics' containing field metadata
        """
        request = GetMetadataRequest(name=f"{self.client.property_id}/metadata")
        metadata = self.client.client.get_metadata(request)

        return {
            "dimensions": {
                d.api_name: {
                    "ui_name": d.ui_name,
                    "description": d.description,
                    "category": d.category,
                }
                for d in metadata.dimensions
            },
            "metrics": {
                m.api_name: {
                    "ui_name": m.ui_name,
                    "description": m.description,
                    "type": m.type_,
                    "category": m.category,
                }
                for m in metadata.metrics
            },
        }

    def get_common_fields_for_llm(self) -> str:
        """Return formatted field list for LLM context.

        This generates a concise summary of the most commonly used dimensions
        and metrics for inclusion in tool descriptions.

        Returns:
            Formatted string with common fields and counts
        """
        meta = self.get_metadata()

        # Most commonly used dimensions
        common_dims = [
            "date",
            "country",
            "city",
            "region",
            "deviceCategory",
            "operatingSystem",
            "browser",
            "source",
            "medium",
            "campaignName",
            "landingPage",
            "sessionSource",
            "sessionMedium",
            "sessionCampaignName",
            "pageTitle",
            "pagePath",
        ]

        # Most commonly used metrics
        common_metrics = [
            "activeUsers",
            "newUsers",
            "sessions",
            "screenPageViews",
            "totalRevenue",
            "purchaseRevenue",
            "conversions",
            "engagementRate",
            "bounceRate",
            "averageSessionDuration",
            "eventCount",
            "sessionsPerUser",
        ]

        # Filter to only include fields that actually exist in this property
        available_dims = [d for d in common_dims if d in meta["dimensions"]]
        available_metrics = [m for m in common_metrics if m in meta["metrics"]]

        total_dims = len(meta["dimensions"])
        total_metrics = len(meta["metrics"])
        other_dims = total_dims - len(available_dims)
        other_metrics = total_metrics - len(available_metrics)

        return f"""Common Dimensions ({len(available_dims)}/{total_dims}):
{', '.join(available_dims)}
{f"(+ {other_dims} more available)" if other_dims > 0 else ""}

Common Metrics ({len(available_metrics)}/{total_metrics}):
{', '.join(available_metrics)}
{f"(+ {other_metrics} more available)" if other_metrics > 0 else ""}

Note: Use getMetadata() tool to see all available dimensions and metrics for this property."""

    def get_field_description(self, field_name: str, is_metric: bool = True) -> str:
        """Get human-readable description of a dimension or metric.

        Args:
            field_name: API name of the field (e.g., 'activeUsers', 'country')
            is_metric: True for metrics, False for dimensions

        Returns:
            Human-readable description, or field name if not found
        """
        meta = self.get_metadata()
        field_dict = meta["metrics"] if is_metric else meta["dimensions"]

        if field_name in field_dict:
            field_info = field_dict[field_name]
            ui_name = field_info.get("ui_name", field_name)
            description = field_info.get("description", "")
            return f"{ui_name}: {description}" if description else ui_name

        return field_name

    def validate_fields(
        self, metrics: list[str], dimensions: list[str] = None
    ) -> Dict[str, list]:
        """Validate that requested metrics and dimensions exist.

        Args:
            metrics: List of metric API names
            dimensions: List of dimension API names

        Returns:
            Dict with 'valid' and 'invalid' lists for both metrics and dimensions
        """
        meta = self.get_metadata()
        dimensions = dimensions or []

        valid_metrics = [m for m in metrics if m in meta["metrics"]]
        invalid_metrics = [m for m in metrics if m not in meta["metrics"]]

        valid_dims = [d for d in dimensions if d in meta["dimensions"]]
        invalid_dims = [d for d in dimensions if d not in meta["dimensions"]]

        return {
            "metrics": {"valid": valid_metrics, "invalid": invalid_metrics},
            "dimensions": {"valid": valid_dims, "invalid": invalid_dims},
        }
