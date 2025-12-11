"""GA4 Data API client wrapper.

Minimal wrapper around Google Analytics Data API - LLM provides structured params.
Includes robust filter building (string/numeric, AND/OR/NOT/IN),
basic validation, and quota-aware retry/backoff.
"""

from typing import Any, Dict, List, Optional
import logging

from google.analytics.data_v1beta import BetaAnalyticsDataClient
from google.analytics.data_v1beta.types import (
    DateRange,
    Dimension,
    Filter,
    FilterExpression,
    FilterExpressionList,
    NumericValue,
    Metric,
    OrderBy,
    RunRealtimeReportRequest,
    RunReportRequest,
)
from google.api_core.exceptions import ResourceExhausted, TooManyRequests
from tenacity import (
    retry,
    stop_after_attempt,
    wait_exponential,
    retry_if_exception_type,
    before_sleep_log,
)
from google.oauth2 import service_account
import os
import yaml


# Valid realtime API dimensions/metrics (strict - GA4 API rejects others)
# https://developers.google.com/analytics/devguides/reporting/data/v1/realtime-api-schema
REALTIME_VALID_DIMENSIONS = frozenset([
    "appVersion", "audienceId", "audienceName", "audienceResourceName",
    "city", "cityId", "country", "countryId", "deviceCategory",
    "eventName", "minutesAgo", "platform", "streamId", "streamName",
    "unifiedScreenName",
])
REALTIME_VALID_METRICS = frozenset([
    "activeUsers", "eventCount", "keyEvents", "screenPageViews",
])


class GA4Client:
    """Minimal wrapper for GA4 Data API.

    This client provides a simple interface to GA4's reporting APIs.
    The LLM handles query understanding and parameter structuring.
    """

    def __init__(self, property_id: str, credentials_path: str):
        """Initialize GA4 client.

        Args:
            property_id: GA4 property ID (e.g., "123456789")
            credentials_path: Path to service account JSON key file
        """
        credentials = service_account.Credentials.from_service_account_file(
            credentials_path,
            scopes=["https://www.googleapis.com/auth/analytics.readonly"],
        )
        self.client = BetaAnalyticsDataClient(credentials=credentials)
        self.property_id = f"properties/{property_id}"
        self._logger = logging.getLogger(__name__)
        self._vendor_adapter = None  # Lazy-loaded vendor adapter
        self._adapter_loaded = False  # Track if we tried to load it

    def _get_vendor_adapter(self):
        """Load vendor adapter from config (lazy, cached).

        Returns:
            Vendor adapter instance or None if not configured
        """
        # Return cached adapter (or None if already tried and failed)
        if self._adapter_loaded:
            return self._vendor_adapter

        self._adapter_loaded = True  # Mark as attempted

        try:
            cfg_path = (
                os.getenv("SHANNON_CONFIG_PATH")
                or os.getenv("CONFIG_PATH")
                or "/app/config/shannon.yaml"
            )

            if not os.path.exists(cfg_path):
                self._logger.info(f"[GA4 Client] Config not found: {cfg_path}")
                return None

            with open(cfg_path, "r") as f:
                cfg = yaml.safe_load(f) or {}

            ga4_cfg = cfg.get("ga4") or {}
            vendor_adapter_name = ga4_cfg.get("vendor_adapter")

            if not vendor_adapter_name:
                self._logger.info("[GA4 Client] No vendor_adapter configured")
                return None

            self._logger.info(f"[GA4 Client] Loading vendor adapter: {vendor_adapter_name}")

            # Import dynamically to avoid circular imports
            from .. import get_ga4_adapter

            self._vendor_adapter = get_ga4_adapter(vendor_adapter_name, ga4_cfg)

            if self._vendor_adapter:
                self._logger.info(f"[GA4 Client] ✓ Loaded vendor adapter: {vendor_adapter_name}")
            else:
                self._logger.warning(f"[GA4 Client] ✗ Failed to load adapter: {vendor_adapter_name}")

            return self._vendor_adapter

        except (ImportError, ModuleNotFoundError) as e:
            # Expected: adapter module not installed
            self._logger.info(f"[GA4 Client] Vendor adapter module not available: {e}")
            return None
        except Exception as e:
            # Unexpected: configuration or runtime error
            self._logger.error(f"[GA4 Client] Error loading vendor adapter: {e}", exc_info=True)
            return None

    # ----------------------------
    # Validation helpers
    # ----------------------------
    def _validate_filter(self, filter_spec: Dict[str, Any]) -> None:
        """Validate filter structure before API call.

        Raises ValueError with actionable messages for malformed filters.
        Accepts both canonical GA4 and simplified forms.
        """
        if not filter_spec:
            return

        # Canonical groups
        if any(k in filter_spec for k in ("andGroup", "orGroup", "notExpression")):
            if "andGroup" in filter_spec:
                grp = filter_spec.get("andGroup") or {}
                exprs = grp.get("expressions")
                if not isinstance(exprs, list) or not exprs:
                    raise ValueError("andGroup.expressions must be a non-empty list")
                for e in exprs:
                    self._validate_filter(e)
            if "orGroup" in filter_spec:
                grp = filter_spec.get("orGroup") or {}
                exprs = grp.get("expressions")
                if not isinstance(exprs, list) or not exprs:
                    raise ValueError("orGroup.expressions must be a non-empty list")
                for e in exprs:
                    self._validate_filter(e)
            if "notExpression" in filter_spec:
                inner = filter_spec.get("notExpression")
                if not isinstance(inner, dict):
                    raise ValueError("notExpression must be an object")
                self._validate_filter(inner)
            return

        # Canonical leaf
        if "filter" in filter_spec:
            leaf = filter_spec["filter"]
            if not isinstance(leaf, dict):
                raise ValueError("filter must be an object")
            field_name = leaf.get("fieldName") or leaf.get("field")
            if not field_name:
                raise ValueError("filter must include 'fieldName' or 'field'")
            types = ["stringFilter", "inListFilter", "numericFilter", "betweenFilter"]
            present = [t for t in types if t in leaf]
            if not present:
                raise ValueError(
                    "filter must include one of: stringFilter, inListFilter, numericFilter, betweenFilter"
                )
            if len(present) > 1:
                raise ValueError(
                    f"filter can only include one filter type, found: {', '.join(present)}"
                )
            # spot checks
            if "inListFilter" in leaf:
                vals = (leaf.get("inListFilter") or {}).get("values")
                if not isinstance(vals, list) or not vals:
                    raise ValueError("inListFilter.values must be a non-empty list")
            return

        # Simple groups
        if "and" in filter_spec:
            exprs = filter_spec.get("and")
            if not isinstance(exprs, list) or not exprs:
                raise ValueError("and must be a non-empty list")
            for e in exprs:
                self._validate_filter(e)
            return
        if "or" in filter_spec:
            exprs = filter_spec.get("or")
            if not isinstance(exprs, list) or not exprs:
                raise ValueError("or must be a non-empty list")
            for e in exprs:
                self._validate_filter(e)
            return
        if "not" in filter_spec:
            inner = filter_spec.get("not")
            if not isinstance(inner, dict):
                raise ValueError("not must be an object")
            self._validate_filter(inner)
            return

        # Simple leaf
        if "field" in filter_spec:
            if filter_spec.get("operator") in ("IN", "IN_LIST"):
                if not isinstance(filter_spec.get("values"), list):
                    raise ValueError("IN operator requires 'values' list")
            elif "match_type" in filter_spec:
                mt = str(filter_spec["match_type"]).upper()
                if mt not in ("EXACT", "CONTAINS", "BEGINS_WITH", "ENDS_WITH"):
                    raise ValueError(
                        f"Invalid match_type '{filter_spec['match_type']}'. "
                        f"GA4 only supports: EXACT, CONTAINS, BEGINS_WITH, ENDS_WITH. "
                        f"REGEXP is NOT supported. To exclude values, use {{'not': {{...}}}} wrapper."
                    )
            return

        raise ValueError(
            "Filter must be canonical (andGroup/orGroup/notExpression/filter) or simple (field/...)"
        )

    @retry(
        retry=retry_if_exception_type((ResourceExhausted, TooManyRequests)),
        wait=wait_exponential(multiplier=1, min=2, max=30),
        stop=stop_after_attempt(3),
        reraise=True,
        before_sleep=before_sleep_log(logging.getLogger(__name__), logging.WARNING),
    )
    def run_report(
        self,
        start_date: str,
        end_date: str,
        metrics: List[str],
        dimensions: Optional[List[str]] = None,
        dimension_filter: Optional[Dict[str, Any]] = None,
        metric_filter: Optional[Dict[str, Any]] = None,
        order_by: Optional[List[Dict[str, Any]]] = None,
        limit: int = 100,
    ) -> Dict[str, Any]:
        """Query GA4 historical data.

        Args:
            start_date: Start date (YYYY-MM-DD, 'today', '7daysAgo', 'yesterday')
            end_date: End date (YYYY-MM-DD, 'today', '7daysAgo', 'yesterday')
            metrics: List of metric names (e.g., ['activeUsers', 'sessions'])
            dimensions: List of dimension names (e.g., ['city', 'deviceCategory'])
            dimension_filter: Filter spec {'field': 'country', 'value': 'US', 'match_type': 'EXACT'}
            metric_filter: Metric filter spec (same format as dimension_filter)
            order_by: List of sort specs [{'metric': 'activeUsers', 'desc': True}]
            limit: Max rows (default 100, max 250000)

        Returns:
            Dict with 'rows', 'row_count', 'metadata', and 'quota' fields

        Common metrics:
            - activeUsers: Number of distinct users
            - sessions: Number of sessions
            - screenPageViews: Page/screen view count
            - totalRevenue: Total revenue (requires ecommerce)
            - conversions: Total conversions
            - engagementRate: Percentage of engaged sessions
            - bounceRate: Percentage of non-engaged sessions

        Common dimensions:
            - date: Date (YYYYMMDD format)
            - country: Country name
            - city: City name
            - deviceCategory: Device type (desktop, mobile, tablet)
            - source: Traffic source
            - medium: Traffic medium
            - campaignName: Campaign name
            - landingPage: Landing page path
            - sessionSource: Session source (GA4 attribution)

        Date format examples:
            - Absolute: "2025-01-01", "2025-01-31"
            - Relative: "today", "yesterday", "7daysAgo", "30daysAgo"
        """
        # Apply vendor adapter filter (e.g., vendor-specific ad exclusion) BEFORE validation
        adapter = self._get_vendor_adapter()
        if adapter:
            self._logger.info("[GA4 Client] Applying vendor adapter filter (realtime=False)")
            dimension_filter = adapter.transform_dimension_filter(dimension_filter, realtime=False)
            self._logger.info(f"[GA4 Client] Vendor filter applied: {dimension_filter is not None}")

        # Pre-validate filters
        if dimension_filter:
            self._validate_filter(dimension_filter)
        if metric_filter:
            self._validate_filter(metric_filter)

        request = RunReportRequest(
            property=self.property_id,
            date_ranges=[DateRange(start_date=start_date, end_date=end_date)],
            metrics=[Metric(name=m) for m in metrics],
            dimensions=[Dimension(name=d) for d in (dimensions or [])],
            limit=limit,
            return_property_quota=True,  # Always monitor quota
        )

        # Add optional filters
        if dimension_filter:
            request.dimension_filter = self._build_filter(dimension_filter)
        if metric_filter:
            request.metric_filter = self._build_filter(metric_filter)
        if order_by:
            request.order_bys = self._build_order_bys(order_by)

        response = self.client.run_report(request)

        # Quota awareness
        try:
            if response.property_quota and response.property_quota.tokens_per_day:
                consumed = response.property_quota.tokens_per_day.consumed
                remaining = response.property_quota.tokens_per_day.remaining
                total = consumed + remaining
                if total > 0:
                    usage_pct = (consumed / total) * 100.0
                    if usage_pct >= 80.0:
                        self._logger.warning(
                            f"GA4 quota usage high: {usage_pct:.1f}% ({consumed}/{total} tokens/day)"
                        )
        except Exception:
            pass

        return self._format_response(response, include_quota=True)

    @retry(
        retry=retry_if_exception_type((ResourceExhausted, TooManyRequests)),
        wait=wait_exponential(multiplier=1, min=2, max=30),
        stop=stop_after_attempt(3),
        reraise=True,
        before_sleep=before_sleep_log(logging.getLogger(__name__), logging.WARNING),
    )
    def run_realtime_report(
        self,
        metrics: List[str],
        dimensions: Optional[List[str]] = None,
        dimension_filter: Optional[Dict[str, Any]] = None,
        limit: int = 100,
    ) -> Dict[str, Any]:
        """Query GA4 real-time data (last 30 minutes).

        Args:
            metrics: List of real-time metric names
            dimensions: List of real-time dimension names
            dimension_filter: Filter spec (same as run_report)
            limit: Max rows (default 100)

        Returns:
            Dict with 'rows', 'row_count', 'metadata', and 'quota' fields

        Real-time metrics:
            - activeUsers: Currently active users
            - screenPageViews: Recent page views
            - eventCount: Total events

        Real-time dimensions:
            - country: User country
            - city: User city
            - unifiedScreenName: Screen/page name
            - deviceCategory: Device type
        """
        # Validate and auto-correct realtime dimensions/metrics
        if dimensions:
            invalid_dims = [d for d in dimensions if d not in REALTIME_VALID_DIMENSIONS and not d.startswith("customUser:")]
            if invalid_dims:
                self._logger.warning(f"[GA4 Realtime] Removing invalid dimensions: {invalid_dims}. Valid: {list(REALTIME_VALID_DIMENSIONS)}")
                dimensions = [d for d in dimensions if d in REALTIME_VALID_DIMENSIONS or d.startswith("customUser:")]
                if not dimensions:
                    # Fallback to safe default
                    dimensions = ["deviceCategory"]
                    self._logger.info("[GA4 Realtime] Using fallback dimension: deviceCategory")

        invalid_metrics = [m for m in metrics if m not in REALTIME_VALID_METRICS]
        if invalid_metrics:
            self._logger.warning(f"[GA4 Realtime] Removing invalid metrics: {invalid_metrics}. Valid: {list(REALTIME_VALID_METRICS)}")
            metrics = [m for m in metrics if m in REALTIME_VALID_METRICS]
            if not metrics:
                metrics = ["activeUsers"]
                self._logger.info("[GA4 Realtime] Using fallback metric: activeUsers")

        # Apply vendor adapter filter (realtime mode - limited dimensions)
        adapter = self._get_vendor_adapter()
        if adapter:
            self._logger.info("[GA4 Client] Applying vendor adapter filter (realtime=True)")
            dimension_filter = adapter.transform_dimension_filter(dimension_filter, realtime=True)
            self._logger.info(f"[GA4 Client] Vendor filter applied (realtime): {dimension_filter is not None}")

        # Pre-validate filters
        if dimension_filter:
            self._validate_filter(dimension_filter)

        request = RunRealtimeReportRequest(
            property=self.property_id,
            metrics=[Metric(name=m) for m in metrics],
            dimensions=[Dimension(name=d) for d in (dimensions or [])],
            limit=limit,
            return_property_quota=True,
        )

        if dimension_filter:
            request.dimension_filter = self._build_filter(dimension_filter)

        response = self.client.run_realtime_report(request)
        return self._format_response(response, include_quota=True)

    def _build_filter(self, filter_spec: Dict[str, Any]) -> FilterExpression:
        """Build GA4 FilterExpression from simple or canonical structures.

        Simple:
          - {"field":"country","value":"US","match_type":"EXACT","case_sensitive":false}
          - {"field":"country","values":["US","CA"],"operator":"IN"}
          - {"not": {...}} | {"and": [..]} | {"or": [..]}

        Canonical:
          - {"notExpression": {"orGroup": {"expressions": [ {"filter": {...}}, ... ]}}}
          - {"andGroup"|"orGroup": {"expressions": [ {...}, {...} ]}}
          - {"filter": {"fieldName": "sessionSourceMedium", "stringFilter": {"matchType": "CONTAINS", "value": "paid"}}}
        """

        if not filter_spec:
            return FilterExpression()

        # Canonical groups
        if isinstance(filter_spec.get("andGroup"), dict):
            exprs = (filter_spec["andGroup"] or {}).get("expressions") or []
            return FilterExpression(
                and_group=FilterExpressionList(
                    expressions=[self._build_filter(e) for e in exprs]
                )
            )
        if isinstance(filter_spec.get("orGroup"), dict):
            exprs = (filter_spec["orGroup"] or {}).get("expressions") or []
            return FilterExpression(
                or_group=FilterExpressionList(
                    expressions=[self._build_filter(e) for e in exprs]
                )
            )
        if isinstance(filter_spec.get("notExpression"), dict):
            return FilterExpression(
                not_expression=self._build_filter(filter_spec["notExpression"])  # type: ignore
            )

        # Canonical leaf { filter: {...} }
        if isinstance(filter_spec.get("filter"), dict):
            leaf = filter_spec["filter"] or {}
            field_name = leaf.get("fieldName") or leaf.get("field")
            if not field_name:
                return FilterExpression()
            # inListFilter
            if isinstance(leaf.get("inListFilter"), dict):
                il = leaf["inListFilter"] or {}
                vals = [str(v) for v in (il.get("values") or [])]
                return FilterExpression(
                    filter=Filter(
                        field_name=str(field_name),
                        in_list_filter=Filter.InListFilter(values=vals),
                    )
                )
            # stringFilter
            if isinstance(leaf.get("stringFilter"), dict):
                sf = leaf["stringFilter"] or {}
                mt = str(sf.get("matchType", "EXACT")).upper()
                match_type_map = {
                    "EXACT": Filter.StringFilter.MatchType.EXACT,
                    "CONTAINS": Filter.StringFilter.MatchType.CONTAINS,
                    "BEGINS_WITH": Filter.StringFilter.MatchType.BEGINS_WITH,
                    "ENDS_WITH": Filter.StringFilter.MatchType.ENDS_WITH,
                }
                return FilterExpression(
                    filter=Filter(
                        field_name=str(field_name),
                        string_filter=Filter.StringFilter(
                            value=str(sf.get("value", "")),
                            match_type=match_type_map.get(
                                mt, Filter.StringFilter.MatchType.EXACT
                            ),
                            case_sensitive=bool(sf.get("caseSensitive", False)),
                        ),
                    )
                )
            # numericFilter
            if isinstance(leaf.get("numericFilter"), dict):
                nf = leaf["numericFilter"] or {}
                op_raw = str(nf.get("operation", "EQUAL")).upper()
                op_map = {
                    "EQUAL": Filter.NumericFilter.Operation.EQUAL,
                    "LESS_THAN": Filter.NumericFilter.Operation.LESS_THAN,
                    "GREATER_THAN": Filter.NumericFilter.Operation.GREATER_THAN,
                    "LESS_THAN_OR_EQUAL": Filter.NumericFilter.Operation.LESS_THAN_OR_EQUAL,
                    "GREATER_THAN_OR_EQUAL": Filter.NumericFilter.Operation.GREATER_THAN_OR_EQUAL,
                }
                op = op_map.get(op_raw, Filter.NumericFilter.Operation.EQUAL)
                val = nf.get("value") or {}
                # value can be {"int64Value": "100"} or {"doubleValue": 0.5}
                if "int64Value" in val:
                    nv = NumericValue(int64_value=int(val["int64Value"]))
                elif "doubleValue" in val:
                    nv = NumericValue(double_value=float(val["doubleValue"]))
                else:
                    raise ValueError("numericFilter.value must include int64Value or doubleValue")
                return FilterExpression(
                    filter=Filter(
                        field_name=str(field_name),
                        numeric_filter=Filter.NumericFilter(operation=op, value=nv),
                    )
                )
            # betweenFilter
            if isinstance(leaf.get("betweenFilter"), dict):
                bf = leaf["betweenFilter"] or {}
                fv = bf.get("fromValue") or {}
                tv = bf.get("toValue") or {}
                if "int64Value" in fv:
                    from_nv = NumericValue(int64_value=int(fv["int64Value"]))
                elif "doubleValue" in fv:
                    from_nv = NumericValue(double_value=float(fv["doubleValue"]))
                else:
                    raise ValueError("betweenFilter.fromValue must include int64Value or doubleValue")
                if "int64Value" in tv:
                    to_nv = NumericValue(int64_value=int(tv["int64Value"]))
                elif "doubleValue" in tv:
                    to_nv = NumericValue(double_value=float(tv["doubleValue"]))
                else:
                    raise ValueError("betweenFilter.toValue must include int64Value or doubleValue")
                return FilterExpression(
                    filter=Filter(
                        field_name=str(field_name),
                        between_filter=Filter.BetweenFilter(from_value=from_nv, to_value=to_nv),
                    )
                )
            return FilterExpression()

        # Simple groups
        if "and" in filter_spec and isinstance(filter_spec["and"], list):
            return FilterExpression(
                and_group=FilterExpressionList(
                    expressions=[self._build_filter(f) for f in filter_spec["and"]]
                )
            )
        if "or" in filter_spec and isinstance(filter_spec["or"], list):
            return FilterExpression(
                or_group=FilterExpressionList(
                    expressions=[self._build_filter(f) for f in filter_spec["or"]]
                )
            )
        if "not" in filter_spec and isinstance(filter_spec["not"], dict):
            return FilterExpression(not_expression=self._build_filter(filter_spec["not"]))

        # Simple leaves
        field = filter_spec.get("field")
        if not field:
            raise ValueError("Invalid filter: missing 'field'")

        op = str(filter_spec.get("operator", "")).upper()
        values = filter_spec.get("values")
        if op in {"IN", "IN_LIST"} and isinstance(values, list) and values:
            return FilterExpression(
                filter=Filter(
                    field_name=str(field),
                    in_list_filter=Filter.InListFilter(values=[str(v) for v in values]),
                )
            )

        # Numeric operators
        numeric_ops = {
            ">": Filter.NumericFilter.Operation.GREATER_THAN,
            "GT": Filter.NumericFilter.Operation.GREATER_THAN,
            "GREATER_THAN": Filter.NumericFilter.Operation.GREATER_THAN,
            ">=": Filter.NumericFilter.Operation.GREATER_THAN_OR_EQUAL,
            "GTE": Filter.NumericFilter.Operation.GREATER_THAN_OR_EQUAL,
            "GE": Filter.NumericFilter.Operation.GREATER_THAN_OR_EQUAL,
            "GREATER_THAN_OR_EQUAL": Filter.NumericFilter.Operation.GREATER_THAN_OR_EQUAL,
            "<": Filter.NumericFilter.Operation.LESS_THAN,
            "LT": Filter.NumericFilter.Operation.LESS_THAN,
            "LESS_THAN": Filter.NumericFilter.Operation.LESS_THAN,
            "<=": Filter.NumericFilter.Operation.LESS_THAN_OR_EQUAL,
            "LTE": Filter.NumericFilter.Operation.LESS_THAN_OR_EQUAL,
            "LE": Filter.NumericFilter.Operation.LESS_THAN_OR_EQUAL,
            "LESS_THAN_OR_EQUAL": Filter.NumericFilter.Operation.LESS_THAN_OR_EQUAL,
            "=": Filter.NumericFilter.Operation.EQUAL,
            "==": Filter.NumericFilter.Operation.EQUAL,
            "EQ": Filter.NumericFilter.Operation.EQUAL,
            "EQUAL": Filter.NumericFilter.Operation.EQUAL,
        }

        if op in numeric_ops:
            # BETWEEN support via simple form
            if op == "BETWEEN" or op == "RANGE":
                frm = filter_spec.get("from") if "from" in filter_spec else filter_spec.get("min")
                to = filter_spec.get("to") if "to" in filter_spec else filter_spec.get("max")
                if frm is None or to is None:
                    raise ValueError("BETWEEN filter requires 'from' and 'to'")
                from_nv = NumericValue(double_value=float(frm)) if (isinstance(frm, float) or (isinstance(frm, str) and "." in frm)) else NumericValue(int64_value=int(frm))
                to_nv = NumericValue(double_value=float(to)) if (isinstance(to, float) or (isinstance(to, str) and "." in to)) else NumericValue(int64_value=int(to))
                return FilterExpression(
                    filter=Filter(
                        field_name=str(field),
                        between_filter=Filter.BetweenFilter(from_value=from_nv, to_value=to_nv),
                    )
                )

            # Standard numeric filter
            val = filter_spec.get("value")
            if val is None:
                raise ValueError("Numeric filter requires 'value'")
            nv = NumericValue(double_value=float(val)) if (isinstance(val, float) or (isinstance(val, str) and "." in val)) else NumericValue(int64_value=int(val))
            return FilterExpression(
                filter=Filter(
                    field_name=str(field),
                    numeric_filter=Filter.NumericFilter(operation=numeric_ops[op], value=nv),
                )
            )

        match_type_map = {
            "EXACT": Filter.StringFilter.MatchType.EXACT,
            "CONTAINS": Filter.StringFilter.MatchType.CONTAINS,
            "BEGINS_WITH": Filter.StringFilter.MatchType.BEGINS_WITH,
            "ENDS_WITH": Filter.StringFilter.MatchType.ENDS_WITH,
        }
        match_type = match_type_map.get(
            str(filter_spec.get("match_type", "EXACT")).upper(),
            Filter.StringFilter.MatchType.EXACT,
        )
        case_sensitive = bool(filter_spec.get("case_sensitive", False))
        value = str(filter_spec.get("value", ""))

        return FilterExpression(
            filter=Filter(
                field_name=str(field),
                string_filter=Filter.StringFilter(
                    value=value, match_type=match_type, case_sensitive=case_sensitive
                ),
            )
        )

    def _build_order_bys(self, order_specs: List[Dict[str, Any]]) -> List[OrderBy]:
        """Build OrderBy objects from simple dicts.

        Args:
            order_specs: [{'metric': 'activeUsers', 'desc': True}] or
                        [{'dimension': 'date', 'desc': False}]

        Returns:
            List of OrderBy objects for GA4 API
        """
        order_bys = []
        for spec in order_specs:
            # Metric simple
            if "metric" in spec and isinstance(spec.get("metric"), str):
                order_bys.append(
                    OrderBy(
                        metric=OrderBy.MetricOrderBy(metric_name=str(spec["metric"])),
                        desc=bool(spec.get("desc", True)),
                    )
                )
                continue

            # Dimension simple
            if "dimension" in spec and isinstance(spec.get("dimension"), str):
                order_bys.append(
                    OrderBy(
                        dimension=OrderBy.DimensionOrderBy(
                            dimension_name=str(spec["dimension"])
                        ),
                        desc=bool(spec.get("desc", False)),
                    )
                )
                continue

            # Canonical dimension form
            if isinstance(spec.get("dimension"), dict):
                d = spec.get("dimension") or {}
                name = str(d.get("dimensionName") or d.get("name") or "")
                order_type_map = {
                    "ALPHANUMERIC": OrderBy.DimensionOrderBy.OrderType.ALPHANUMERIC,
                    "NUMERIC": OrderBy.DimensionOrderBy.OrderType.NUMERIC,
                    "CASE_INSENSITIVE_ALPHANUMERIC": OrderBy.DimensionOrderBy.OrderType.CASE_INSENSITIVE_ALPHANUMERIC,
                }
                ot = order_type_map.get(str(d.get("orderType", "")).upper())
                order_bys.append(
                    OrderBy(
                        dimension=OrderBy.DimensionOrderBy(
                            dimension_name=name,
                            order_type=(
                                ot
                                if ot is not None
                                else OrderBy.DimensionOrderBy.OrderType.ORDER_TYPE_UNSPECIFIED
                            ),
                        ),
                        desc=bool(spec.get("desc", False)),
                    )
                )
        return order_bys

    def _format_response(
        self, response: Any, include_quota: bool = False
    ) -> Dict[str, Any]:
        """Convert GA4 response to simple dict structure.

        Args:
            response: GA4 API response (RunReportResponse or RunRealtimeReportResponse)
            include_quota: Whether to include quota information

        Returns:
            Dict with structured data and optional quota info
        """
        rows = []
        for row in response.rows:
            row_dict = {}

            # Add dimensions
            for i, dim_value in enumerate(row.dimension_values):
                dim_name = response.dimension_headers[i].name
                row_dict[dim_name] = dim_value.value

            # Add metrics
            for i, metric_value in enumerate(row.metric_values):
                metric_name = response.metric_headers[i].name
                row_dict[metric_name] = metric_value.value

            rows.append(row_dict)

        result = {
            "rows": rows,
            "row_count": response.row_count,
            "metadata": {
                "dimension_headers": [h.name for h in response.dimension_headers],
                "metric_headers": [h.name for h in response.metric_headers],
            },
        }

        # Add quota information if requested and available
        if include_quota and response.property_quota:
            quota = response.property_quota
            result["quota"] = {
                "tokens_per_day": {
                    "consumed": quota.tokens_per_day.consumed,
                    "remaining": quota.tokens_per_day.remaining,
                },
                "tokens_per_hour": {
                    "consumed": quota.tokens_per_hour.consumed,
                    "remaining": quota.tokens_per_hour.remaining,
                },
                "concurrent_requests": {
                    "consumed": quota.concurrent_requests.consumed,
                    "remaining": quota.concurrent_requests.remaining,
                },
            }

        return result
