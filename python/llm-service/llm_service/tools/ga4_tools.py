"""GA4 Tool wrappers for registry-based execution.

These Tool classes wrap the GA4 client so the agent can call GA4
via the standard tool registry pipeline.
"""

from __future__ import annotations

from typing import Dict, List, Optional
import os
import yaml
import logging
import threading
from google.api_core.exceptions import ResourceExhausted, PermissionDenied, TooManyRequests

from .base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult
from .vendor_adapters.ga4.client import GA4Client
from .vendor_adapters.ga4.metadata_cache import GA4MetadataCache
from .vendor_adapters import get_ga4_adapter


def _resolve_config_path() -> str:
    return (
        os.getenv("SHANNON_CONFIG_PATH")
        or os.getenv("CONFIG_PATH")
        or "/app/config/shannon.yaml"
    )


_GA4_CLIENT: Optional[GA4Client] = None
_GA4_CACHE: Optional[GA4MetadataCache] = None
_GA4_LOCK = threading.Lock()
_logger = logging.getLogger(__name__)


def _get_ga4_client(session_context: Optional[Dict] = None) -> GA4Client:
    """Get GA4 client, supporting both service account and OAuth modes.

    Args:
        session_context: Optional context containing OAuth credentials:
            - ga4_access_token: OAuth access token from user auth flow
            - ga4_property_id: GA4 property ID (required with access_token)

    Returns:
        GA4Client instance (per-request for OAuth, cached for service account)
    """
    global _GA4_CLIENT, _GA4_CACHE

    # Check for OAuth mode (per-request client)
    if session_context:
        access_token = session_context.get("ga4_access_token")
        property_id = session_context.get("ga4_property_id")

        if access_token:
            if not property_id:
                raise ValueError(
                    "ga4_property_id is required when using ga4_access_token"
                )
            _logger.info(f"[GA4 Tools] Creating OAuth client for property {property_id}")
            # Create per-request client (not cached - token may expire)
            return GA4Client(property_id=str(property_id), access_token=access_token)

    # Service account mode (cached global client)
    if _GA4_CLIENT is not None:
        return _GA4_CLIENT

    with _GA4_LOCK:
        # Double-check after acquiring lock
        if _GA4_CLIENT is not None:
            return _GA4_CLIENT

        cfg_path = _resolve_config_path()
        if not os.path.exists(cfg_path):
            raise ValueError(
                f"GA4 config not found. Set SHANNON_CONFIG_PATH (tried {cfg_path})."
            )

        with open(cfg_path, "r") as f:
            cfg = yaml.safe_load(f) or {}

        ga4_cfg = cfg.get("ga4") or {}
        property_id = ga4_cfg.get("property_id")
        credentials_path = ga4_cfg.get("credentials_path")
        if not property_id or not credentials_path:
            raise ValueError(
                "Missing GA4 configuration. Provide ga4.property_id and ga4.credentials_path in config."
            )

        _GA4_CLIENT = GA4Client(property_id=str(property_id), credentials_path=str(credentials_path))
        _GA4_CACHE = GA4MetadataCache(_GA4_CLIENT)
        return _GA4_CLIENT


def _get_ga4_cache() -> GA4MetadataCache:
    global _GA4_CACHE
    if _GA4_CACHE is None:
        _get_ga4_client()
    assert _GA4_CACHE is not None
    return _GA4_CACHE


def _get_ga4_vendor_adapter():
    """Load GA4 vendor adapter from config if configured.

    Returns:
        Vendor adapter instance or None if not configured
    """
    logger = logging.getLogger(__name__)
    cfg_path = _resolve_config_path()
    logger.info(f"[GA4 Adapter] Config path: {cfg_path}")

    if not os.path.exists(cfg_path):
        logger.warning(f"[GA4 Adapter] Config file not found: {cfg_path}")
        return None

    try:
        with open(cfg_path, "r") as f:
            cfg = yaml.safe_load(f) or {}

        ga4_cfg = cfg.get("ga4") or {}
        vendor_adapter_name = ga4_cfg.get("vendor_adapter")

        logger.info(f"[GA4 Adapter] vendor_adapter from config: {vendor_adapter_name}")

        if not vendor_adapter_name:
            logger.info("[GA4 Adapter] No vendor_adapter configured, skipping filter")
            return None

        # Get adapter instance with full ga4 config
        logger.info(f"[GA4 Adapter] Attempting to load adapter: {vendor_adapter_name}")
        adapter = get_ga4_adapter(vendor_adapter_name, ga4_cfg)

        if adapter:
            logger.info(f"[GA4 Adapter] ✓ Successfully loaded: {vendor_adapter_name}")
        else:
            logger.warning(f"[GA4 Adapter] ✗ Failed to load adapter: {vendor_adapter_name}")

        return adapter

    except Exception as e:
        logger.error(f"[GA4 Adapter] Exception loading adapter: {e}", exc_info=True)
        return None


class GA4RunReportTool(Tool):
    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="ga4_run_report",
            version="1.0.0",
            description=(
                "Query GA4 historical analytics data for a date range. "
                "Use relative dates like '7daysAgo' and 'today'."
            ),
            category="analytics",
            requires_auth=True,
            rate_limit=20,
            timeout_seconds=45,
            sandboxed=True,
            cost_per_use=0.0,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="start_date",
                type=ToolParameterType.STRING,
                description="Start date (YYYY-MM-DD or relative like '7daysAgo')",
                required=False,
            ),
            ToolParameter(
                name="end_date",
                type=ToolParameterType.STRING,
                description="End date (YYYY-MM-DD or relative like 'today')",
                required=False,
            ),
            ToolParameter(
                name="metrics",
                type=ToolParameterType.ARRAY,
                description="List of metric names (e.g., ['activeUsers', 'sessions'])",
                required=True,
            ),
            ToolParameter(
                name="dimensions",
                type=ToolParameterType.ARRAY,
                description="List of dimension names (e.g., ['country', 'deviceCategory'])",
                required=False,
            ),
            ToolParameter(
                name="dimension_filter",
                type=ToolParameterType.OBJECT,
                description="Filter for dimensions: {field, value, match_type}",
                required=False,
            ),
            ToolParameter(
                name="metric_filter",
                type=ToolParameterType.OBJECT,
                description="Filter for metrics: {field, value, match_type}",
                required=False,
            ),
            ToolParameter(
                name="order_by",
                type=ToolParameterType.ARRAY,
                description=(
                    "Sort definitions, e.g. [{'metric':'activeUsers','desc':True}] or "
                    "[{'dimension':'date','desc':False}]"
                ),
                required=False,
            ),
            ToolParameter(
                name="limit",
                type=ToolParameterType.INTEGER,
                description="Max rows to return (default 100).",
                required=False,
            ),
        ]

    async def _execute_impl(self, session_context: Optional[Dict] = None, **kwargs) -> ToolResult:
        try:
            client = _get_ga4_client(session_context)
            start_date = kwargs.get("start_date") or "7daysAgo"
            end_date = kwargs.get("end_date") or "today"
            metrics = kwargs.get("metrics") or []
            dimensions = kwargs.get("dimensions")
            dimension_filter = kwargs.get("dimension_filter")
            metric_filter = kwargs.get("metric_filter")
            order_by = kwargs.get("order_by")
            limit = int(kwargs.get("limit") or 100)

            # Note: Vendor adapter filtering is applied in GA4Client.run_report() to ensure
            # all code paths (both Tool class and function tools) get the filter

            # Emit progress via observer if available
            observer = kwargs.get("observer")
            if observer:
                try:
                    observer("progress", {"message": "Querying GA4 Analytics..."})
                except Exception:
                    pass

            output = client.run_report(
                start_date=start_date,
                end_date=end_date,
                metrics=metrics,
                dimensions=dimensions,
                dimension_filter=dimension_filter,
                metric_filter=metric_filter,
                order_by=order_by,
                limit=limit,
            )
            return ToolResult(success=True, output=output)
        except ValueError as e:
            logging.getLogger(__name__).error(f"GA4 filter validation error: {e}")
            error_msg = str(e)
            # Add retry guidance for common errors
            if "match_type" in error_msg or "REGEXP" in error_msg:
                error_msg += " Hint: To exclude values, use {'not': {'field': '...', 'value': '...', 'match_type': 'CONTAINS'}} instead of REGEXP."
            return ToolResult(
                success=False,
                output=None,
                error=f"Filter validation failed: {error_msg} Please retry with corrected filter format.",
            )
        except (ResourceExhausted, TooManyRequests) as e:
            logging.getLogger(__name__).error(f"GA4 quota exhausted: {e}")
            return ToolResult(
                success=False,
                output=None,
                error="GA4 quota limit reached. Retry later or reduce query frequency.",
            )
        except PermissionDenied as e:
            logging.getLogger(__name__).error(f"GA4 permission denied: {e}")
            return ToolResult(
                success=False,
                output=None,
                error="GA4 API access denied. Check service account permissions.",
            )
        except Exception as e:
            logging.getLogger(__name__).error(f"GA4 API error ({type(e).__name__}): {e}")
            return ToolResult(success=False, output=None, error=f"GA4 API error: {e}")


class GA4RunRealtimeReportTool(Tool):
    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="ga4_run_realtime_report",
            version="1.0.0",
            description="Query GA4 real-time analytics for the last 30 minutes.",
            category="analytics",
            requires_auth=True,
            rate_limit=30,
            timeout_seconds=30,
            sandboxed=True,
            cost_per_use=0.0,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="metrics",
                type=ToolParameterType.ARRAY,
                description="List of real-time metric names",
                required=True,
            ),
            ToolParameter(
                name="dimensions",
                type=ToolParameterType.ARRAY,
                description="List of real-time dimension names",
                required=False,
            ),
            ToolParameter(
                name="dimension_filter",
                type=ToolParameterType.OBJECT,
                description="Filter for dimensions: {field, value, match_type}",
                required=False,
            ),
            ToolParameter(
                name="limit",
                type=ToolParameterType.INTEGER,
                description="Max rows to return (default 100).",
                required=False,
            ),
        ]

    async def _execute_impl(self, session_context: Optional[Dict] = None, **kwargs) -> ToolResult:
        try:
            client = _get_ga4_client(session_context)
            metrics = kwargs.get("metrics") or []
            dimensions = kwargs.get("dimensions")
            dimension_filter = kwargs.get("dimension_filter")
            limit = int(kwargs.get("limit") or 100)

            # Note: Vendor adapter filtering is applied in GA4Client.run_realtime_report() to ensure
            # all code paths (both Tool class and function tools) get the filter

            # Emit progress via observer if available
            observer = kwargs.get("observer")
            if observer:
                try:
                    observer("progress", {"message": "Querying GA4 Realtime Analytics..."})
                except Exception:
                    pass

            output = client.run_realtime_report(
                metrics=metrics,
                dimensions=dimensions,
                dimension_filter=dimension_filter,
                limit=limit,
            )
            return ToolResult(success=True, output=output)
        except ValueError as e:
            logging.getLogger(__name__).error(f"GA4 filter validation error: {e}")
            error_msg = str(e)
            # Add retry guidance for common errors
            if "match_type" in error_msg or "REGEXP" in error_msg:
                error_msg += " Hint: To exclude values, use {'not': {'field': '...', 'value': '...', 'match_type': 'CONTAINS'}} instead of REGEXP."
            return ToolResult(
                success=False,
                output=None,
                error=f"Filter validation failed: {error_msg} Please retry with corrected filter format.",
            )
        except (ResourceExhausted, TooManyRequests) as e:
            logging.getLogger(__name__).error(f"GA4 quota exhausted: {e}")
            return ToolResult(
                success=False,
                output=None,
                error="GA4 quota limit reached. Retry later or reduce query frequency.",
            )
        except PermissionDenied as e:
            logging.getLogger(__name__).error(f"GA4 permission denied: {e}")
            return ToolResult(
                success=False,
                output=None,
                error="GA4 API access denied. Check service account permissions.",
            )
        except Exception as e:
            logging.getLogger(__name__).error(f"GA4 API error ({type(e).__name__}): {e}")
            return ToolResult(success=False, output=None, error=f"GA4 API error: {e}")


class GA4GetMetadataTool(Tool):
    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="ga4_get_metadata",
            version="1.0.0",
            description="Get available GA4 dimensions and metrics for this property.",
            category="analytics",
            requires_auth=True,
            rate_limit=30,
            timeout_seconds=30,
            sandboxed=True,
            cost_per_use=0.0,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return []

    async def _execute_impl(self, session_context: Optional[Dict] = None, **kwargs) -> ToolResult:
        try:
            # Emit progress via observer if available
            observer = kwargs.get("observer")
            if observer:
                try:
                    observer("progress", {"message": "Fetching GA4 Metadata..."})
                except Exception:
                    pass

            cache = _get_ga4_cache()
            meta = cache.get_metadata()
            summary = cache.get_common_fields_for_llm()
            return ToolResult(
                success=True,
                output={
                    "dimensions": list(meta.get("dimensions", {}).keys()),
                    "metrics": list(meta.get("metrics", {}).keys()),
                    "summary": summary,
                },
            )
        except Exception as e:
            import logging
            logger = logging.getLogger(__name__)
            logger.error(f"GA4 API Error: {type(e).__name__}: {str(e)}")
            return ToolResult(success=False, output=None, error=str(e))
