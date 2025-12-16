"""
Web Crawl Tool - Exploratory multi-page crawling

Uses Firecrawl Crawl API for automatic link discovery and content extraction.
This is an async operation that may take 30-60 seconds.

Use Cases:
- Unknown website structure
- Discovering what content exists
- Sites with dynamic/nested navigation

For targeted page extraction where you know the paths, use web_subpage_fetch instead.
"""

import aiohttp
import asyncio
import os
import logging
from typing import Dict, Optional, List, Any
from urllib.parse import urlparse

from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult

logger = logging.getLogger(__name__)

# Constants
MAX_LIMIT = 20  # Maximum pages to crawl
DEFAULT_LIMIT = 10
DEFAULT_MAX_LENGTH = 8000
CRAWL_TIMEOUT = int(os.getenv("WEB_FETCH_CRAWL_TIMEOUT", "120"))
POLL_INTERVAL = 2  # seconds between status checks
MAX_POLL_ATTEMPTS = 60  # 60 * 2s = 2 minutes max


class WebCrawlTool(Tool):
    """
    Crawl a website to discover and extract content from multiple pages.

    Uses Firecrawl Crawl API for automatic link discovery.
    This is async and may take 30-60 seconds.
    """

    def __init__(self):
        self.firecrawl_api_key = os.getenv("FIRECRAWL_API_KEY")

        # Validate Firecrawl key
        self.firecrawl_available = bool(
            self.firecrawl_api_key and
            len(self.firecrawl_api_key.strip()) >= 10 and
            self.firecrawl_api_key.lower() not in ["test", "demo", "xxx"]
        )

        if self.firecrawl_available:
            logger.info("WebCrawlTool initialized with Firecrawl provider")
        else:
            logger.warning("WebCrawlTool: Firecrawl not available")

        super().__init__()

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="web_crawl",
            version="1.0.0",
            description=(
                "Crawl a website to discover and extract content from multiple pages. "
                "Automatically follows links and extracts content from discovered pages. "
                "\n\n"
                "USE WHEN:\n"
                "• You don't know the website structure\n"
                "• You want to explore and discover what content exists\n"
                "• The site has dynamic/nested structure\n"
                "\n"
                "NOTE: This is async and may take 30-60 seconds. "
                "For targeted page extraction where you know paths, use web_subpage_fetch instead."
            ),
            category="retrieval",
            author="Shannon",
            requires_auth=False,
            rate_limit=10,  # Lower rate limit due to resource intensive
            timeout_seconds=180,  # 3 minutes max
            memory_limit_mb=512,
            sandboxed=False,
            dangerous=False,
            cost_per_use=0.01,  # Higher cost for crawl
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="url",
                type=ToolParameterType.STRING,
                description="Starting URL to crawl (e.g., https://example.com)",
                required=True,
            ),
            ToolParameter(
                name="limit",
                type=ToolParameterType.INTEGER,
                description=f"Maximum number of pages to crawl (1-{MAX_LIMIT}). Default: {DEFAULT_LIMIT}",
                required=False,
                default=DEFAULT_LIMIT,
                min_value=1,
                max_value=MAX_LIMIT,
            ),
            ToolParameter(
                name="max_length",
                type=ToolParameterType.INTEGER,
                description="Maximum content length per page in characters",
                required=False,
                default=DEFAULT_MAX_LENGTH,
                min_value=1000,
                max_value=30000,
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        """Execute exploratory crawl using Firecrawl Crawl API."""
        url = kwargs.get("url")
        limit = kwargs.get("limit", DEFAULT_LIMIT)
        max_length = kwargs.get("max_length", DEFAULT_MAX_LENGTH)

        if not url:
            return ToolResult(success=False, output=None, error="URL parameter required")

        # Validate URL
        try:
            parsed = urlparse(url)
            if not parsed.scheme or not parsed.netloc:
                return ToolResult(success=False, output=None, error=f"Invalid URL: {url}")
            if parsed.scheme not in ["http", "https"]:
                return ToolResult(success=False, output=None, error="Only HTTP/HTTPS allowed")
        except Exception as e:
            return ToolResult(success=False, output=None, error=f"Invalid URL: {e}")

        if not self.firecrawl_available:
            return ToolResult(
                success=False,
                output=None,
                error="web_crawl requires Firecrawl API. Configure FIRECRAWL_API_KEY."
            )

        try:
            result = await self._crawl(url, limit, max_length)
            return ToolResult(
                success=True,
                output=result,
                metadata={"provider": "firecrawl", "strategy": "crawl"}
            )
        except Exception as e:
            logger.error(f"Crawl failed: {e}")
            return ToolResult(
                success=False,
                output=None,
                error=f"Crawl failed: {str(e)[:200]}"
            )

    async def _crawl(self, url: str, limit: int, max_length: int) -> Dict[str, Any]:
        """
        Execute async crawl using Firecrawl Crawl API.

        Steps:
        1. Start crawl job
        2. Poll for completion
        3. Collect and merge results
        """
        timeout = aiohttp.ClientTimeout(total=CRAWL_TIMEOUT)
        async with aiohttp.ClientSession(timeout=timeout) as session:
            # Step 1: Start crawl
            crawl_id = await self._start_crawl(session, url, limit)
            logger.info(f"Started crawl job {crawl_id} for {url}")

            # Step 2: Poll for completion
            results = await self._poll_crawl(session, crawl_id)
            if not results:
                raise Exception("Crawl returned no results")

            logger.info(f"Crawl completed with {len(results)} pages")

            # Step 3: Merge results
            return self._merge_results(results, url, max_length)

    async def _start_crawl(self, session: aiohttp.ClientSession, url: str, limit: int) -> str:
        """Start a crawl job and return the crawl ID."""
        headers = {
            "Authorization": f"Bearer {self.firecrawl_api_key}",
            "Content-Type": "application/json"
        }
        payload = {
            "url": url,
            "limit": limit,
            "scrapeOptions": {
                "formats": ["markdown"],
                "onlyMainContent": True
            }
        }

        async with session.post(
            "https://api.firecrawl.dev/v1/crawl",
            json=payload,
            headers=headers
        ) as response:
            if response.status != 200:
                error = await response.text()
                raise Exception(f"Failed to start crawl ({response.status}): {error[:200]}")

            data = await response.json()
            crawl_id = data.get("id")
            if not crawl_id:
                raise Exception("Crawl API did not return job ID")

            return crawl_id

    async def _poll_crawl(self, session: aiohttp.ClientSession, crawl_id: str) -> List[Dict]:
        """Poll crawl status until completion."""
        headers = {
            "Authorization": f"Bearer {self.firecrawl_api_key}"
        }

        all_results = []

        for attempt in range(MAX_POLL_ATTEMPTS):
            async with session.get(
                f"https://api.firecrawl.dev/v1/crawl/{crawl_id}",
                headers=headers
            ) as response:
                if response.status != 200:
                    error = await response.text()
                    logger.warning(f"Poll error ({response.status}): {error[:100]}")
                    await asyncio.sleep(POLL_INTERVAL)
                    continue

                data = await response.json()
                status = data.get("status", "unknown")

                # Collect results
                page_data = data.get("data", [])
                if page_data:
                    all_results.extend(page_data)

                if status in ["completed", "failed"]:
                    if status == "failed":
                        logger.warning(f"Crawl {crawl_id} failed")
                    break

                # Continue polling
                await asyncio.sleep(POLL_INTERVAL)

        return all_results

    def _merge_results(self, results: List[Dict], original_url: str, max_length: int) -> Dict:
        """Merge crawl results into a single output."""
        # Filter and deduplicate
        seen_urls = set()
        unique_results = []
        for r in results:
            url = r.get("metadata", {}).get("url", r.get("url", ""))
            if url and url not in seen_urls:
                seen_urls.add(url)
                unique_results.append(r)

        if not unique_results:
            raise Exception("No valid pages in crawl results")

        if len(unique_results) == 1:
            r = unique_results[0]
            content = r.get("markdown", "")[:max_length]
            return {
                "url": r.get("metadata", {}).get("url", original_url),
                "title": r.get("metadata", {}).get("title", ""),
                "content": content,
                "method": "firecrawl_crawl",
                "pages_fetched": 1,
                "word_count": len(content.split()),
                "char_count": len(content),
            }

        # Multiple pages - merge with markdown separators
        merged_content = []
        total_chars = 0
        char_budget = max_length * len(unique_results)  # Total budget across pages

        for i, r in enumerate(unique_results):
            metadata = r.get("metadata", {})
            page_url = metadata.get("url", "")
            page_title = metadata.get("title", "")
            page_content = r.get("markdown", "")

            # Per-page truncation
            if len(page_content) > max_length:
                page_content = page_content[:max_length]

            if i == 0:
                merged_content.append(f"# Main Page: {page_url}\n")
                if page_title:
                    merged_content.append(f"**{page_title}**\n")
            else:
                merged_content.append(f"\n---\n\n## Page {i+1}: {page_url}\n")
                if page_title:
                    merged_content.append(f"**{page_title}**\n")

            merged_content.append(f"\n{page_content}\n")
            total_chars += len(page_content)

            # Stop if we've hit the char budget
            if total_chars >= char_budget:
                break

        final_content = "".join(merged_content)

        return {
            "url": original_url,
            "title": unique_results[0].get("metadata", {}).get("title", ""),
            "content": final_content,
            "method": "firecrawl_crawl",
            "pages_fetched": len(unique_results),
            "word_count": len(final_content.split()),
            "char_count": total_chars,
            "metadata": {
                "total_crawled": len(results),
                "unique_pages": len(unique_results),
                "urls": list(seen_urls)
            }
        }
