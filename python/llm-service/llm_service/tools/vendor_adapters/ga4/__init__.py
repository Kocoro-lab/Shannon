"""Google Analytics 4 (GA4) Data API integration.

This module provides a thin wrapper around the GA4 Data API, allowing Shannon's
LLM agents to query analytics data using natural language.

The integration follows Shannon's vendor adapter pattern:
- Minimal wrapper (LLM handles query understanding)
- Self-documenting tools (rich function descriptions)
- Quota monitoring (return usage info in responses)
- Graceful fallback (Shannon works without GA4 module)

Example usage:
    from .client import GA4Client

    client = GA4Client(
        property_id="123456789",
        credentials_path="/path/to/service-account.json"
    )

    result = client.run_report(
        start_date="7daysAgo",
        end_date="today",
        metrics=["activeUsers", "sessions"],
        dimensions=["country", "city"],
        limit=10
    )

See docs/vendor-adapters.md for integration guide.
"""

from .client import GA4Client
from .metadata_cache import GA4MetadataCache

__all__ = ["GA4Client", "GA4MetadataCache"]
