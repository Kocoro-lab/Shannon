"""
Web Subpage Fetch Tool - Intelligent multi-page extraction from a website

Uses Firecrawl Map + Scrape strategy:
1. Map API: Get all URLs on the website (fast, up to 200 URLs)
2. Score URLs by relevance (path matching, depth, keywords)
3. Batch scrape top N most relevant pages

Use Cases:
- Company research: /about, /team, /ir, /products
- Documentation: /docs, /api, /guides
- Known domain with specific target pages

For exploratory crawling where structure is unknown, use web_crawl instead.
"""

import aiohttp
import asyncio
import os
import logging
import socket
from typing import Dict, Optional, List, Any, Tuple
from urllib.parse import urlparse

from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult
from ..openapi_parser import _is_private_ip

logger = logging.getLogger(__name__)

# Constants
MAX_LIMIT = 15  # Maximum pages to fetch
MAP_URL_LIMIT = 200  # Max URLs from map API
DEFAULT_LIMIT = 5
DEFAULT_MAX_LENGTH = 10000
BATCH_CONCURRENCY = int(os.getenv("WEB_FETCH_BATCH_CONCURRENCY", "3"))  
SCRAPE_TIMEOUT = int(os.getenv("WEB_FETCH_SCRAPE_TIMEOUT", "30"))
MAP_TIMEOUT = int(os.getenv("WEB_FETCH_MAP_TIMEOUT", "15"))

# Retry configuration
RETRY_CONFIG = {
    408: {"max_retries": 3, "delays": [2, 4, 8]},      # Timeout: exponential backoff
    429: {"max_retries": 2, "delays": [5, 10]},       # Rate limit: reduced retries
    500: {"max_retries": 2, "delays": [1, 2]},         # Server error: quick retry
    502: {"max_retries": 2, "delays": [2, 4]},         # Bad gateway
    503: {"max_retries": 2, "delays": [3, 6]},         # Service unavailable
}

# Important keywords for relevance scoring
IMPORTANT_KEYWORDS = [
    "about", "team", "company", "product", "pricing", "docs", "api",
    "contact", "careers", "blog", "news", "ir", "investors", "leadership",
    "services", "features", "solutions", "press", "overview"
]

# Keyword to path mappings for target inference
KEYWORD_PATH_MAP = {
    "api": ["/api", "/api-reference", "/reference", "/api-docs"],
    "doc": ["/docs", "/documentation", "/guide", "/guides", "/manual"],
    "about": ["/about", "/about-us", "/company", "/who-we-are"],
    "team": ["/team", "/people", "/leadership", "/management", "/our-team"],
    "product": ["/products", "/product", "/features", "/solutions", "/offerings"],
    "pricing": ["/pricing", "/plans", "/price", "/packages"],
    "contact": ["/contact", "/contact-us", "/reach-us", "/get-in-touch"],
    "career": ["/careers", "/jobs", "/join-us", "/opportunities", "/hiring"],
    "blog": ["/blog", "/news", "/articles", "/insights", "/posts"],
    "investor": ["/ir", "/investors", "/investor-relations", "/stockholders"],
}


class WebSubpageFetchTool(Tool):
    """
    Fetch multiple pages from a website using intelligent selection.

    Uses Map + Scrape strategy with relevance scoring to select
    the most important pages from a website.
    """

    def __init__(self):
        self.firecrawl_api_key = os.getenv("FIRECRAWL_API_KEY")
        self.exa_api_key = os.getenv("EXA_API_KEY")
        self.preferred_provider = os.getenv("WEB_FETCH_PROVIDER", "firecrawl").lower()

        # Validate Firecrawl key
        self.firecrawl_available = bool(
            self.firecrawl_api_key and
            len(self.firecrawl_api_key.strip()) >= 10 and
            self.firecrawl_api_key.lower() not in ["test", "demo", "xxx"]
        )

        if self.firecrawl_available:
            logger.info("WebSubpageFetchTool initialized with Firecrawl provider")
        else:
            logger.warning("WebSubpageFetchTool: Firecrawl not available, will use fallback")

        super().__init__()

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="web_subpage_fetch",
            version="1.0.0",
            description=(
                "Fetch multiple pages from a known website using intelligent selection via Map API (Firecrawl). "
                "Implements Firecrawl Map (discover URLs) + Scrape (fetch content) strategy. "
                "Discovers all URLs, then scores and selects the most relevant pages to fetch based on your targets."
                "\n\n"
                "USE WHEN:\n"
                "• You have a specific domain and need specific sections (e.g., 'check company team', 'find API docs')\n"
                "• You want to efficiently grab the most important pages without crawling everything\n"
                "\n"
                "NOT FOR:\n"
                "• Blind exploration of unknown sites (use web_crawl instead)\n"
                "• Single page fetching (use web_fetch instead)\n"
                "\n"
                "Returns: {url, title, content, method, pages_fetched, word_count, char_count}. "
                "Content is merged markdown when multiple pages are fetched."
                "\n\n"
                "Example usage:\n"
                "• Research OpenAI: url='https://openai.com', limit=15, "
                "target_paths=['/about', '/our-team', '/board', '/careers', '/research', '/blog', '/product', '/company', '/leadership', '/pricing']\n"
                "• Find Stripe API docs: url='https://stripe.com', limit=10, target_keywords='API documentation developer reference'\n"
                "• Tesla investor relations: url='https://ir.tesla.com', limit=8, "
                "target_paths=['/news', '/press', '/financials', '/events', '/governance', '/stock']"
            ),
            category="retrieval",
            author="Shannon",
            requires_auth=False,
            rate_limit=20,
            timeout_seconds=120,
            memory_limit_mb=256,
            sandboxed=False,
            dangerous=False,
            cost_per_use=0.005,  # Multiple pages
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="url",
                type=ToolParameterType.STRING,
                description="Base URL of the website (e.g., https://example.com)",
                required=True,
            ),
            ToolParameter(
                name="limit",
                type=ToolParameterType.INTEGER,
                description=f"Maximum number of pages to fetch (1-{MAX_LIMIT}). Default: {DEFAULT_LIMIT}",
                required=False,
                default=DEFAULT_LIMIT,
                min_value=1,
                max_value=MAX_LIMIT,
            ),
            ToolParameter(
                name="target_paths",
                type=ToolParameterType.ARRAY,
                description=(
                    "Specific URL paths to prioritize (e.g., ['/about', '/team', '/ir']). "
                    "Pages matching these paths get highest scores."
                ),
                required=False,
            ),
            ToolParameter(
                name="target_keywords",
                type=ToolParameterType.STRING,
                description=(
                    "Keywords to infer target paths (e.g., 'API documentation'). "
                    "Auto-converts to paths like /api, /docs. "
                    "Use target_paths for precise control."
                ),
                required=False,
            ),
            ToolParameter(
                name="max_length",
                type=ToolParameterType.INTEGER,
                description="Maximum content length per page in characters",
                required=False,
                default=DEFAULT_MAX_LENGTH,
                min_value=1000,
                max_value=50000,
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        """Execute multi-page fetch using Map + Scrape strategy."""
        url = kwargs.get("url")
        limit = kwargs.get("limit", DEFAULT_LIMIT)
        target_paths = kwargs.get("target_paths", [])
        target_keywords = kwargs.get("target_keywords")
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
            # DNS resolution guard to avoid wasted provider calls
            host = parsed.hostname
            if not host:
                return ToolResult(success=False, output=None, error="Invalid host in URL")
            # SSRF protection: Block private/internal IPs
            if _is_private_ip(host):
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Access to private/internal IP addresses is not allowed: {host}",
                )
            try:
                socket.getaddrinfo(host, 443)
            except Exception as e:
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"DNS resolution failed for host {host}: {e}",
                )
        except Exception as e:
            return ToolResult(success=False, output=None, error=f"Invalid URL: {e}")

        # Infer paths from keywords if provided
        effective_paths = list(target_paths) if target_paths else []
        if target_keywords and not effective_paths:
            effective_paths = self._infer_paths_from_keywords(target_keywords)
            logger.info(f"Inferred paths from keywords '{target_keywords}': {effective_paths}")

        last_error = ""

        # Execute with Firecrawl (primary) or fallback
        if self.firecrawl_available:
            try:
                result, scrape_meta = await self._map_and_scrape(url, limit, effective_paths, max_length)
                return ToolResult(
                    success=True,
                    output=result,
                    metadata={
                        "provider": "firecrawl",
                        "strategy": "map_and_scrape",
                        **scrape_meta,
                    }
                )
            except Exception as e:
                last_error = f"Firecrawl map+scrape failed: {e}"
                logger.error(last_error)
                # Fall through to Exa or error

        # Fallback: Exa with subpages (if available)
        if self.exa_api_key:
            try:
                result = await self._fetch_with_exa(url, limit, effective_paths, max_length)
                return ToolResult(
                    success=True,
                    output=result,
                    metadata={
                        "provider": "exa",
                        "strategy": "subpage_search",
                        "urls_requested": [url],
                        "urls_attempted": [url],
                        "urls_succeeded": [url],
                        "urls_failed": [],
                        "partial_success": False,
                        "failure_summary": {"failed_count": 0, "total_count": 1},
                    }
                )
            except Exception as e:
                error_msg = f"Exa fallback failed: {e}"
                last_error = f"{last_error}; {error_msg}" if last_error else error_msg
                logger.error(error_msg)

        if not last_error:
            if not self.firecrawl_available and not self.exa_api_key:
                last_error = "Firecrawl/Exa not configured"
            else:
                last_error = "No provider returned content"

        return ToolResult(
            success=False,
            output=None,
            error=f"web_subpage_fetch failed: {last_error}",
            metadata={
                "provider": "firecrawl" if self.firecrawl_available else "exa",
                "strategy": "map_and_scrape",
                "urls_requested": [],
                "urls_attempted": [],
                "urls_succeeded": [],
                "urls_failed": [],
                "partial_success": False,
                "failure_summary": {"failed_count": 0, "total_count": 0},
            },
        )

    def _infer_paths_from_keywords(self, keywords: str) -> List[str]:
        """Infer URL paths from keyword string."""
        paths = []
        keywords_lower = keywords.lower()
        for keyword, keyword_paths in KEYWORD_PATH_MAP.items():
            if keyword in keywords_lower:
                paths.extend(keyword_paths)
        return list(dict.fromkeys(paths))  # Deduplicate while preserving order

    def _calculate_relevance_score(
        self,
        url: str,
        target_paths: List[str],
        total_pages: int
    ) -> float:
        """
        Calculate relevance score for a URL (0.0-1.0).

        Scoring factors:
        - Path matching: 0.5 weight
        - URL depth: 0.2 weight (shallow = better)
        - URL length: 0.1 weight (shorter = better)
        - Keywords: 0.2 weight
        """
        score = 0.0
        parsed = urlparse(url)
        path = parsed.path.lower().rstrip("/")

        # 1. Path matching (0.5 weight)
        if target_paths:
            for target in target_paths:
                target_clean = target.lower().rstrip("/")
                if path == target_clean or path.startswith(target_clean + "/"):
                    score += 0.5
                    break
                elif target_clean.strip("/") in path:
                    score += 0.3
                    break

        # 2. URL depth (0.2 weight) - fewer slashes = better
        depth = path.count("/") if path else 0
        if depth <= 1:
            score += 0.2
        elif depth == 2:
            score += 0.1
        # depth > 2: no bonus

        # 3. URL length (0.1 weight) - shorter = better
        if len(url) < 50:
            score += 0.1
        elif len(url) < 80:
            score += 0.05

        # 4. Keywords (0.2 weight)
        for keyword in IMPORTANT_KEYWORDS:
            if keyword in path:
                score += 0.05
                if score >= 1.0:
                    break

        # Size adjustment: boost top-level pages on large sites
        if total_pages > 50 and depth <= 1:
            score *= 1.2

        return min(score, 1.0)

    async def _map_and_scrape(
        self,
        url: str,
        limit: int,
        target_paths: List[str],
        max_length: int
    ) -> Tuple[Dict[str, Any], Dict]:
        """Map website URLs, score by relevance, and scrape top pages."""
        # Step 1: Map to get all URLs
        all_urls = await self._map(url)
        if not all_urls:
            raise Exception("Map API returned no URLs")

        logger.info(f"Map returned {len(all_urls)} URLs for {url}")

        # Step 2: Score and sort URLs
        scored_urls = []
        for u in all_urls:
            score = self._calculate_relevance_score(u, target_paths, len(all_urls))
            if score >= 0.1:  # Minimum threshold
                scored_urls.append((u, score))

        scored_urls.sort(key=lambda x: x[1], reverse=True)

        # Step 3: Select top N
        selected_urls = [u for u, _ in scored_urls[:limit]]

        # Always include the base URL if not already present (with normalization to avoid duplicates)
        base_url = url.rstrip("/")
        normalized_selected = [u.rstrip("/") for u in selected_urls]
        if base_url not in normalized_selected:
            selected_urls.insert(0, base_url)
            if len(selected_urls) > limit:
                selected_urls = selected_urls[:limit]

        logger.info(f"Selected {len(selected_urls)} URLs for scraping: {selected_urls}")

        # Step 4: Batch scrape
        results, scrape_meta = await self._batch_scrape(selected_urls, max_length)
        if not results:
            raise Exception("Batch scrape returned no results")

        merged = self._merge_results(results, url)
        scrape_meta["urls_requested"] = selected_urls
        scrape_meta["partial_success"] = len(scrape_meta.get("urls_failed", [])) > 0
        scrape_meta["failure_summary"] = {
            "failed_count": len(scrape_meta.get("urls_failed", [])),
            "total_count": len(selected_urls),
        }

        return merged, scrape_meta

    async def _map(self, url: str) -> List[str]:
        """Use Firecrawl Map API to get all URLs on a website."""
        timeout = aiohttp.ClientTimeout(total=MAP_TIMEOUT)
        async with aiohttp.ClientSession(timeout=timeout) as session:
            headers = {
                "Authorization": f"Bearer {self.firecrawl_api_key}",
                "Content-Type": "application/json"
            }
            payload = {"url": url, "limit": MAP_URL_LIMIT}

            async with session.post(
                "https://api.firecrawl.dev/v1/map",
                json=payload,
                headers=headers
            ) as response:
                if response.status != 200:
                    error = await response.text()
                    raise Exception(f"Map API failed ({response.status}): {error[:200]}")

                data = await response.json()
                return data.get("links", [])

    async def _batch_scrape(self, urls: List[str], max_length: int) -> Tuple[List[Dict], Dict]:
        """
        Batch scrape multiple URLs with concurrency control and retry.

        Returns:
            (results, meta):
                results: successful scrape dicts
                meta: {"urls_attempted", "urls_succeeded", "urls_failed"}
        """
        semaphore = asyncio.Semaphore(BATCH_CONCURRENCY)

        async def limited_scrape(url: str) -> Optional[Dict]:
            async with semaphore:
                return await self._scrape_with_retry(url, max_length)

        tasks = [limited_scrape(url) for url in urls]
        results = await asyncio.gather(*tasks, return_exceptions=True)

        # Filter out failures
        valid_results = []
        urls_succeeded: List[str] = []
        urls_failed: List[Dict] = []

        for url, r in zip(urls, results):
            if isinstance(r, dict) and r.get("content"):
                valid_results.append(r)
                urls_succeeded.append(url)
                continue

            reason = ""
            if isinstance(r, Exception):
                reason = str(r)
            elif isinstance(r, dict):
                reason = "empty content"
            else:
                reason = "scrape failed"

            urls_failed.append({"url": url, "reason": reason[:200]})

        meta = {
            "urls_attempted": urls,
            "urls_succeeded": urls_succeeded,
            "urls_failed": urls_failed,
        }

        return valid_results, meta

    async def _scrape_with_retry(self, url: str, max_length: int) -> Optional[Dict]:
        """Scrape a single URL with retry logic."""
        last_error = None

        for attempt in range(3):  # Max 3 attempts
            try:
                return await self._scrape(url, max_length)
            except Exception as e:
                last_error = e
                error_str = str(e)

                # Check if retryable
                status_code = None
                for code in RETRY_CONFIG.keys():
                    if str(code) in error_str:
                        status_code = code
                        break

                if status_code and attempt < RETRY_CONFIG[status_code]["max_retries"]:
                    delay = RETRY_CONFIG[status_code]["delays"][attempt]
                    logger.warning(f"Retry {attempt+1} for {url} after {delay}s (status {status_code})")
                    await asyncio.sleep(delay)
                else:
                    break

        logger.warning(f"Failed to scrape {url} after retries: {last_error}")
        return None

    async def _scrape(self, url: str, max_length: int) -> Dict:
        """Scrape a single URL using Firecrawl."""
        timeout = aiohttp.ClientTimeout(total=SCRAPE_TIMEOUT)
        async with aiohttp.ClientSession(timeout=timeout) as session:
            headers = {
                "Authorization": f"Bearer {self.firecrawl_api_key}",
                "Content-Type": "application/json"
            }
            payload = {
                "url": url,
                "formats": ["markdown"],
                "onlyMainContent": True,
                "timeout": SCRAPE_TIMEOUT * 1000  # Firecrawl uses ms
            }

            async with session.post(
                "https://api.firecrawl.dev/v1/scrape",
                json=payload,
                headers=headers
            ) as response:
                if response.status != 200:
                    error = await response.text()
                    raise Exception(f"Scrape failed ({response.status}): {error[:100]}")

                data = await response.json()
                result_data = data.get("data", {})

                content = result_data.get("markdown", "")
                if len(content) > max_length:
                    content = content[:max_length]

                return {
                    "url": result_data.get("metadata", {}).get("url", url),
                    "title": result_data.get("metadata", {}).get("title", ""),
                    "content": content,
                }

    def _merge_results(self, results: List[Dict], original_url: str) -> Dict:
        """Merge multiple page results into a single output."""
        if len(results) == 1:
            r = results[0]
            return {
                "url": r.get("url", original_url),
                "title": r.get("title", ""),
                "content": r.get("content", ""),
                "method": "firecrawl",
                "pages_fetched": 1,
                "word_count": len(r.get("content", "").split()),
                "char_count": len(r.get("content", "")),
                "tool_source": "fetch",  # Citation V2: mark as fetch-origin
            }

        # Multiple pages - merge with markdown separators
        merged_content = []
        total_chars = 0

        for i, r in enumerate(results):
            page_url = r.get("url", "")
            page_title = r.get("title", "")
            page_content = r.get("content", "")

            if i == 0:
                merged_content.append(f"# Main Page: {page_url}\n")
                if page_title:
                    merged_content.append(f"**{page_title}**\n")
            else:
                merged_content.append(f"\n---\n\n## Subpage {i}: {page_url}\n")
                if page_title:
                    merged_content.append(f"**{page_title}**\n")

            merged_content.append(f"\n{page_content}\n")
            total_chars += len(page_content)

        final_content = "".join(merged_content)

        return {
            "url": original_url,
            "title": results[0].get("title", ""),
            "content": final_content,
            "method": "firecrawl",
            "pages_fetched": len(results),
            "word_count": len(final_content.split()),
            "char_count": total_chars,
            "tool_source": "fetch",  # Citation V2: mark as fetch-origin
            "metadata": {
                "urls": [r.get("url") for r in results]
            }
        }

    async def _fetch_with_exa(
        self,
        url: str,
        limit: int,
        target_paths: List[str],
        max_length: int
    ) -> Dict[str, Any]:
        """Fallback: Use Exa with subpages feature."""
        timeout = aiohttp.ClientTimeout(total=60)
        async with aiohttp.ClientSession(timeout=timeout) as session:
            headers = {
                "x-api-key": self.exa_api_key,
                "Content-Type": "application/json"
            }

            # Convert target_paths to subpage_target keywords
            subpage_target = " ".join([p.strip("/") for p in target_paths]) if target_paths else None

            search_payload = {
                "query": url,
                "numResults": 1,
                "includeDomains": [urlparse(url).netloc],
                "subpages": limit,
                "livecrawl": "preferred"
            }
            if subpage_target:
                search_payload["subpageTarget"] = subpage_target

            async with session.post(
                "https://api.exa.ai/search",
                json=search_payload,
                headers=headers
            ) as response:
                if response.status != 200:
                    raise Exception(f"Exa search failed: {response.status}")

                data = await response.json()
                results = data.get("results", [])
                if not results:
                    raise Exception("Exa returned no results")

                result_ids = [r.get("id") for r in results if r.get("id")]

            # Get full content
            content_payload = {
                "ids": result_ids,
                "text": {"maxCharacters": max_length, "includeHtmlTags": False}
            }

            async with session.post(
                "https://api.exa.ai/contents",
                json=content_payload,
                headers=headers
            ) as response:
                if response.status != 200:
                    raise Exception(f"Exa contents failed: {response.status}")

                content_data = await response.json()
                content_results = content_data.get("results", [])

                if len(content_results) == 1:
                    r = content_results[0]
                    return {
                        "url": r.get("url", url),
                        "title": r.get("title", ""),
                        "content": r.get("text", ""),
                        "method": "exa",
                        "pages_fetched": 1,
                        "word_count": len(r.get("text", "").split()),
                        "char_count": len(r.get("text", "")),
                        "tool_source": "fetch",  # Citation V2: mark as fetch-origin
                    }

                return self._merge_exa_results(content_results, url)

    def _merge_exa_results(self, results: List[Dict], original_url: str) -> Dict:
        """Merge Exa results into single output."""
        merged_content = []
        total_chars = 0

        for i, r in enumerate(results):
            page_url = r.get("url", "")
            page_title = r.get("title", "")
            page_content = r.get("text", "")

            if i == 0:
                merged_content.append(f"# Main Page: {page_url}\n")
                if page_title:
                    merged_content.append(f"**{page_title}**\n")
            else:
                merged_content.append(f"\n---\n\n## Subpage {i}: {page_url}\n")
                if page_title:
                    merged_content.append(f"**{page_title}**\n")

            merged_content.append(page_content)
            total_chars += len(page_content)

        final_content = "\n".join(merged_content)

        return {
            "url": original_url,
            "title": results[0].get("title", ""),
            "content": final_content,
            "method": "exa",
            "pages_fetched": len(results),
            "word_count": len(final_content.split()),
            "char_count": total_chars,
            "tool_source": "fetch",  # Citation V2: mark as fetch-origin
        }
