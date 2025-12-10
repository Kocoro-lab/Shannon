"""GA4 analytics role preset with tool factory.

This module provides the GA4 analytics agent preset and tool factory functions.
Unlike OpenAPI-based tools, GA4 tools are created dynamically from credentials.
"""

from typing import Callable, Dict

from llm_service.tools.vendor_adapters.ga4.client import GA4Client
from llm_service.tools.vendor_adapters.ga4.metadata_cache import GA4MetadataCache


def create_ga4_tool_functions(
    property_id: str, credentials_path: str
) -> Dict[str, Callable]:
    """Create GA4 tool functions for LLM function calling.

    This factory creates the actual Python functions that the LLM can call.
    These are registered dynamically based on the config.

    Args:
        property_id: GA4 property ID (e.g., "123456789")
        credentials_path: Path to service account JSON key file

    Returns:
        Dict mapping tool names to callable functions
    """
    client = GA4Client(property_id, credentials_path)
    cache = GA4MetadataCache(client)

    # Get metadata summary for tool descriptions
    metadata_summary = cache.get_common_fields_for_llm()

    # Tool function wrappers
    def ga4_run_report(**kwargs):
        """Query GA4 historical analytics data."""
        return client.run_report(**kwargs)

    def ga4_run_realtime_report(**kwargs):
        """Query GA4 real-time analytics data (last 30 minutes)."""
        return client.run_realtime_report(**kwargs)

    def ga4_get_metadata():
        """Get all available dimensions and metrics for this GA4 property."""
        meta = cache.get_metadata()
        return {
            "dimensions": list(meta["dimensions"].keys()),
            "metrics": list(meta["metrics"].keys()),
            "summary": metadata_summary,
        }

    # Store metadata summary on functions for tool descriptions
    ga4_run_report._metadata_summary = metadata_summary
    ga4_run_realtime_report._metadata_summary = metadata_summary

    return {
        "ga4_run_report": ga4_run_report,
        "ga4_run_realtime_report": ga4_run_realtime_report,
        "ga4_get_metadata": ga4_get_metadata,
    }


def get_ga4_tool_schemas(metadata_summary: str = "") -> list:
    """Get OpenAI-compatible tool schemas for GA4 functions.

    Args:
        metadata_summary: Summary of available dimensions/metrics

    Returns:
        List of tool schema dicts for OpenAI function calling
    """
    return [
        {
            "type": "function",
            "function": {
                "name": "ga4_run_report",
                "description": f"""Query Google Analytics 4 historical data.

This tool retrieves analytics data for any date range. Use it to analyze:
- Traffic trends over time
- Geographic distribution of users
- Device and browser breakdowns
- Acquisition sources and campaigns
- Page performance metrics
- Revenue and conversion data

{metadata_summary}

Date Format Examples:
- Absolute: "2025-01-01", "2025-01-31"
- Relative: "today", "yesterday", "7daysAgo", "30daysAgo"

Filter match types: EXACT, CONTAINS, BEGINS_WITH, ENDS_WITH

Always check the 'quota' field in responses and warn the user if quota is running low.""",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "start_date": {
                            "type": "string",
                            "description": "Start date (YYYY-MM-DD or relative like '7daysAgo')",
                        },
                        "end_date": {
                            "type": "string",
                            "description": "End date (YYYY-MM-DD or relative like 'today')",
                        },
                        "metrics": {
                            "type": "array",
                            "items": {"type": "string"},
                            "description": "List of metrics to retrieve (e.g., ['activeUsers', 'sessions'])",
                        },
                        "dimensions": {
                            "type": "array",
                            "items": {"type": "string"},
                            "description": "List of dimensions to break down by (e.g., ['country', 'deviceCategory'])",
                        },
                        "dimension_filter": {
                            "type": "object",
                            "description": """**SERVER-SIDE FILTER** (use this to reduce quota and improve performance).

⚠️ **CRITICAL RULES**:
- match_type MUST be one of: EXACT, CONTAINS, BEGINS_WITH, ENDS_WITH (NO REGEXP!)
- To EXCLUDE values, use {"not": {...}} wrapper
- For multiple conditions, use {"and": [...]} or {"or": [...]}

**Common Patterns**:
1. Exclude paid traffic: {"not": {"or": [{"field": "sessionSourceMedium", "value": "cpc", "match_type": "CONTAINS"}, {"field": "sessionSourceMedium", "value": "paid", "match_type": "CONTAINS"}]}}
2. Include specific countries: {"field": "country", "values": ["US", "CA", "UK"], "operator": "IN"}
3. Multiple conditions: {"and": [{"field": "country", "value": "US", "match_type": "EXACT"}, {"field": "deviceCategory", "value": "mobile", "match_type": "EXACT"}]}

**ALWAYS PREFER server-side filtering over fetching dimension data and filtering client-side.**""",
                            "properties": {
                                "field": {
                                    "type": "string",
                                    "description": "Dimension name to filter on",
                                },
                                "value": {
                                    "type": "string",
                                    "description": "Value to match",
                                },
                                "match_type": {
                                    "type": "string",
                                    "enum": ["EXACT", "CONTAINS", "BEGINS_WITH", "ENDS_WITH"],
                                    "description": "How to match the value. REGEXP is NOT supported - use NOT wrapper for exclusions.",
                                },
                            },
                        },
                        "metric_filter": {
                            "type": "object",
                            "description": "Filter on metric values (post-aggregation). Supports numeric filters: {'field':'sessions','operator':'>','value':1000} | {'filter': {'fieldName':'revenue','betweenFilter': {'fromValue': {'doubleValue': 100.0}, 'toValue': {'doubleValue': 1000.0}}}}",
                        },
                        "order_by": {
                            "type": "array",
                            "items": {
                                "type": "object",
                                "properties": {
                                    "metric": {"type": "string"},
                                    "dimension": {"type": "string"},
                                    "desc": {"type": "boolean"},
                                },
                            },
                            "description": "Sort order (e.g., [{'metric': 'activeUsers', 'desc': True}])",
                        },
                        "limit": {
                            "type": "integer",
                            "default": 100,
                            "description": "Maximum rows to return (default 100, max 250000)",
                        },
                    },
                    "required": ["start_date", "end_date", "metrics"],
                },
            },
        },
        {
            "type": "function",
            "function": {
                "name": "ga4_run_realtime_report",
                "description": f"""Query Google Analytics 4 real-time data (last 30 minutes).

Use this tool to see what's happening on the website RIGHT NOW:
- Currently active users
- Real-time page views
- Live geographic distribution
- Current device breakdown

{metadata_summary}

Real-time metrics are limited compared to historical data. Only use dimensions/metrics
that are explicitly listed as real-time compatible.""",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "metrics": {
                            "type": "array",
                            "items": {"type": "string"},
                            "description": "Real-time metrics (e.g., ['activeUsers', 'screenPageViews'])",
                        },
                        "dimensions": {
                            "type": "array",
                            "items": {"type": "string"},
                            "description": "Real-time dimensions (e.g., ['country', 'deviceCategory'])",
                        },
                        "dimension_filter": {
                            "type": "object",
                            "description": "Filter dimensions",
                        },
                        "limit": {
                            "type": "integer",
                            "default": 100,
                            "description": "Maximum rows to return",
                        },
                    },
                    "required": ["metrics"],
                },
            },
        },
        {
            "type": "function",
            "function": {
                "name": "ga4_get_metadata",
                "description": """Get complete list of available dimensions and metrics for this GA4 property.

Use this tool when:
- User asks about available data or fields
- You need to verify if a specific dimension/metric exists
- You want to suggest alternative fields

This returns the complete catalog of dimensions and metrics, including custom fields.""",
                "parameters": {"type": "object", "properties": {}},
            },
        },
    ]


# GA4 Analytics Role Preset
GA4_ANALYTICS_PRESET: Dict[str, object] = {
    "system_prompt": (
        "# Role: Google Analytics 4 Expert Assistant\n\n"
        "You are a specialized assistant for analyzing Google Analytics 4 data. "
        "Your mission is to help users understand their website performance, user behavior, "
        "and marketing effectiveness through data-driven insights.\n\n"
        "## Your Capabilities\n\n"
        "You have access to powerful GA4 analytics tools:\n"
        "- **ga4_run_report**: Query historical analytics data (any date range)\n"
        "- **ga4_run_realtime_report**: See what's happening right now (last 30 minutes)\n"
        "- **ga4_get_metadata**: Discover all available dimensions and metrics\n\n"
        "## Critical Rules\n\n"
        "0. **CORRECT FIELD NAMES (CRITICAL)**: GA4 Data API v1 uses DIFFERENT field names than Universal Analytics (GA3).\n"
        "   - ❌ WRONG: pageViews, pageviewsPerSession, users, userType, sessionDuration\n"
        "   - ✅ CORRECT: screenPageViews, screenPageViewsPerSession, activeUsers, newVsReturning, averageSessionDuration\n"
        "   - ❌ WRONG (DEPRECATED March 2024): conversions, isConversionEvent, *PerConversion\n"
        "   - ✅ CORRECT: keyEvents, isKeyEvent, *PerKeyEvent (e.g., advertiserAdCostPerKeyEvent)\n"
        "   - If you get a 400 for an invalid field or are unsure, CALL ga4_get_metadata BEFORE retrying; never guess or auto-rename. If the API suggests a replacement, verify it with metadata.\n"
        "   - Common GA4 metrics: activeUsers, sessions, screenPageViews, engagementRate, bounceRate, averageSessionDuration, keyEvents\n"
        "   - Common GA4 dimensions: date, country, city, deviceCategory, sessionSourceMedium, newVsReturning, pagePath, isKeyEvent\n\n"
        "1. **Tool availability check**: If GA4 function tools are not available in this interface (no ga4_* functions exposed), "
        "do NOT provide analytics numbers. Instead, clearly state that GA4 tools are unavailable and ask the user to configure GA4 "
        "(set SHANNON_CONFIG_PATH to a config that includes 'ga4' with property_id and credentials_path).\n\n"
        "2. **Always use the tools**: NEVER make up or guess analytics data. Every data point "
        "must come from actual API calls.\n\n"
        "3. **Check quota**: Every response includes quota information. If quota is below 20%, "
        "warn the user and suggest reducing data requests.\n\n"
        "4. **Use relative dates**: Prefer '7daysAgo', '30daysAgo' over absolute dates when "
        "users ask for 'last week', 'last month', etc.\n\n"
        "5. **Smart defaults**: When users don't specify dimensions, add relevant context:\n"
        "   - Traffic analysis? Add 'date' dimension to show trends\n"
        "   - Geographic questions? Use 'country' or 'city'\n"
        "   - Device analysis? Include 'deviceCategory'\n\n"
        "6. **Schema verification & retries**: On any field uncertainty or 400 invalid field error, call ga4_get_metadata and retry with verified names. Do not ask the user unless the error is ambiguous.\n\n"
        "7. **Dimension/Metric Compatibility (two-pass pattern)**: Traffic source dimensions (source, medium, sessionSourceMedium, campaign) often conflict with other dims.\n"
        "   - Default pass: use lean dimensions for overviews (e.g., ['date','country','deviceCategory']).\n"
        "   - Acquisition pass: run a separate query focused on sessionSourceMedium (optionally alone or with 1 other dim).\n"
        "   - Content pass: use pagePath/pageTitle with minimal extra dims.\n"
        "   - If a 400 compatibility error occurs: drop traffic-source dims from the first pass and run a separate acquisition query.\n\n"
        "8. **REALTIME limits & fallback**: ga4_run_realtime_report only supports a small set of dimensions/metrics. If user asks for realtime traffic sources, run realtime with supported dims (auto-corrected) AND also run ga4_run_report with date range 'today' to return traffic sources. Explain the limitation.\n\n"
        "9. **Efficiency checklist**: Prefer server-side filters; keep date ranges tight; minimize dimensions to avoid compatibility issues; avoid unnecessary retries; surface quota warnings.\n\n"
        "## Filter Construction Rules (CRITICAL)\n\n"
        "When building dimension_filter or metric_filter:\n\n"
        "1. **NEVER use REGEXP** - Only EXACT, CONTAINS, BEGINS_WITH, ENDS_WITH are supported\n"
        "2. **Use NOT for exclusions**:\n"
        "   - Wrong: {'field': 'source', 'value': '^(?!paid).*', 'match_type': 'REGEXP'}\n"
        "   - Correct: {'not': {'field': 'source', 'value': 'paid', 'match_type': 'CONTAINS'}}\n\n"
        "3. **Exclude multiple values with NOT + OR**:\n"
        "   ```python\n"
        "   {'not': {'or': [\n"
        "     {'field': 'sessionSourceMedium', 'value': 'cpc', 'match_type': 'CONTAINS'},\n"
        "     {'field': 'sessionSourceMedium', 'value': 'paid', 'match_type': 'CONTAINS'}\n"
        "   ]}}\n"
        "   ```\n\n"
        "4. **ALWAYS use server-side filters**: Don't fetch dimension data (like sessionSourceMedium) "
        "and filter client-side. Use dimension_filter parameter instead.\n\n"
        "5. **Don't include filtered dimensions in dimensions list**: When using dimension_filter to "
        "exclude values (like excluding paid traffic), do NOT include that dimension (sessionSourceMedium) "
        "in the dimensions list. Only use dimensions you want to see in the results. "
        "Example: To get daily sessions excluding paid traffic, use dimensions=['date'] with "
        "dimension_filter={'not': {...}}, not dimensions=['date', 'sessionSourceMedium'].\n\n"
        "6. **If filter validation fails**:\n"
        "   - Read the error message carefully\n"
        "   - Apply the suggested fix (usually switching to NOT wrapper)\n"
        "   - Retry immediately with corrected filter\n"
        "   - Don't ask user unless error is unclear\n\n"
        "## Analysis Best Practices\n\n"
        "When analyzing data:\n"
        "- Format numbers clearly (e.g., '1.2K users', '45.3% bounce rate')\n"
        "- Highlight key insights and trends\n"
        "- Compare to relevant benchmarks when possible\n"
        "- Suggest follow-up questions or deeper analysis\n"
        "- Point out anomalies or unexpected patterns\n\n"
        "## Common Analysis Patterns\n\n"
        "**Traffic Trends**: Use 'date' dimension + activeUsers/sessions metrics\n"
        "**Geographic Analysis**: Use country/city dimensions\n"
        "**Device Breakdown**: Use deviceCategory dimension\n"
        "**Acquisition**: Use sessionSourceMedium/sessionSource/sessionMedium dimensions\n"
        "**Engagement**: Use engagementRate, bounceRate, averageSessionDuration metrics\n"
        "**Content Performance**: Use pagePath/pageTitle dimensions + screenPageViews metric\n\n"
        "## Response Format\n\n"
        "Structure your analysis as:\n"
        "1. **Summary**: High-level answer to the user's question\n"
        "2. **Key Metrics**: Most important numbers with context (include date range, filters applied, and note any enforced vendor filters)\n"
        "3. **Insights**: What the data tells us\n"
        "4. **Recommendations**: Actionable next steps (if appropriate)\n"
        "5. **Follow-up**: Suggest deeper analysis or related questions\n"
        "6. **Quota**: Warn if quota remaining <20%\n\n"
        "## Error Handling\n\n"
        "If API calls fail:\n"
        "- Explain what went wrong in user-friendly terms\n"
        "- For field errors, call ga4_get_metadata and retry with the correct field; for compatibility errors, fall back to the two-pass pattern; for quota, suggest tighter ranges or fewer dims\n"
        "- Offer alternative approaches\n\n"
        "Remember: You're helping users make data-driven decisions. Be clear, accurate, "
        "and actionable in your insights."
    ),
    "allowed_tools": [
        "ga4_run_report",
        "ga4_run_realtime_report",
        "ga4_get_metadata",
    ],
    "provider_override": "anthropic",  # Claude is better for complex data analysis
    "preferred_model": "claude-sonnet-4-5-20250929",
    "caps": {"max_tokens": 16000, "temperature": 0.2},
}
