"""
Web Fetch Tool - Extract full page content for deep analysis

Multi-provider architecture:
- Exa: Semantic search + content extraction (handles JS-heavy sites)
- Firecrawl: Smart crawling + structured extraction + actions
- Pure Python: Free, fast, works for most pages

Security features:
- SSRF protection (blocks private IPs, cloud metadata endpoints)
- Memory exhaustion prevention (50MB response limit)
- Redirect loop protection (max 10 redirects)
"""

import aiohttp
import asyncio
import os
import re
import logging
from typing import Dict, Optional, List, Any
from urllib.parse import urlparse, urljoin, parse_qsl, urlencode
from bs4 import BeautifulSoup
import html2text
from enum import Enum

from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult
from ..openapi_parser import _is_private_ip

logger = logging.getLogger(__name__)

# Constants
MAX_SUBPAGES = 15  # Balanced limit for comprehensive research
CRAWL_DELAY_SECONDS = float(os.getenv("WEB_FETCH_CRAWL_DELAY", "0.5"))
MAX_TOTAL_CRAWL_CHARS = int(os.getenv("WEB_FETCH_MAX_CRAWL_CHARS", "150000"))  # 150KB total
CRAWL_TIMEOUT_SECONDS = int(os.getenv("WEB_FETCH_CRAWL_TIMEOUT", "90"))  # 90s total crawl timeout


class FetchProvider(Enum):
    """Available fetch providers"""
    EXA = "exa"
    FIRECRAWL = "firecrawl"
    PYTHON = "python"
    AUTO = "auto"


class WebFetchProvider:
    """Base class for web fetch providers"""

    # Standardized timeout for all providers (in seconds)
    # Increased from 30s to 60s for slow websites (e.g., Japanese domains)
    DEFAULT_TIMEOUT = 60
    # Timeout for aiohttp client (should be > Firecrawl timeout to allow retry)
    AIOHTTP_TIMEOUT = 90

    @staticmethod
    def validate_api_key(api_key: str) -> bool:
        """Validate API key format and presence"""
        if not api_key or not isinstance(api_key, str):
            return False
        if len(api_key.strip()) < 10:
            return False
        if api_key.lower() in ["test", "demo", "example", "your_api_key_here", "xxx"]:
            return False
        return True

    @staticmethod
    def sanitize_error_message(error: str) -> str:
        """Sanitize error messages to prevent information disclosure"""
        sanitized = re.sub(r"https?://[^\s]+", "[URL_REDACTED]", str(error))
        sanitized = re.sub(r"\b[A-Za-z0-9]{32,}\b", "[KEY_REDACTED]", sanitized)
        sanitized = re.sub(
            r"api[_\-]?key[\s=:]+[\w\-]+",
            "api_key=[REDACTED]",
            sanitized,
            flags=re.IGNORECASE,
        )
        if len(sanitized) > 200:
            sanitized = sanitized[:200] + "..."
        return sanitized

    async def fetch(
        self,
        url: str,
        max_length: int = 10000,
        subpages: int = 0,
        subpage_target: Optional[str] = None,
        **kwargs
    ) -> Dict[str, Any]:
        """
        Fetch content from a URL.

        Args:
            url: The URL to fetch
            max_length: Maximum content length in characters
            subpages: Number of subpages to crawl (0 = main page only)
            subpage_target: Keywords to prioritize certain subpages

        Returns:
            Dict with: url, title, content, method, pages_fetched, etc.
        """
        raise NotImplementedError


class FirecrawlFetchProvider(WebFetchProvider):
    """
    Firecrawl fetch provider - Smart crawling with JS rendering and structured extraction.

    Features:
    - Scrape: Single page extraction with JS rendering
    - Crawl: Multi-page crawling with automatic link discovery
    - Markdown output with clean formatting
    - Supports actions (click, scroll, wait) for complex pages
    """

    def __init__(self, api_key: str):
        if not self.validate_api_key(api_key):
            raise ValueError("Invalid or missing Firecrawl API key")
        self.api_key = api_key
        self.scrape_url = "https://api.firecrawl.dev/v1/scrape"
        self.crawl_url = "https://api.firecrawl.dev/v1/crawl"

    def _infer_paths_from_target(self, subpage_target: str) -> List[str]:
        """
        Infer URL paths from subpage_target keywords.
        
        This enables intelligent page selection even when LLM doesn't provide
        explicit required_paths. Maps common keywords to likely URL paths.
        
        Args:
            subpage_target: Keywords like "API documentation", "team information"
        
        Returns:
            List of inferred URL paths like ["/api", "/docs"]
        
        Example:
            _infer_paths_from_target("API documentation")
            -> ["/api", "/api-reference", "/reference", "/docs", "/documentation", "/guide"]
        """
        if not subpage_target:
            return []
        
        target_lower = subpage_target.lower()
        inferred = []
        
        # Keyword to path mapping
        keyword_map = {
            "api": ["/api", "/api-reference", "/reference"],
            "doc": ["/docs", "/documentation", "/guide"],
            "tutorial": ["/tutorial", "/tutorials", "/learn", "/getting-started"],
            "about": ["/about", "/about-us", "/company"],
            "team": ["/team", "/people", "/leadership", "/management"],
            "product": ["/products", "/product", "/features"],
            "pricing": ["/pricing", "/plans", "/price"],
            "blog": ["/blog", "/news", "/articles", "/posts"],
            "contact": ["/contact", "/contact-us"],
            "career": ["/careers", "/jobs", "/join"],
        }
        
        for keyword, paths in keyword_map.items():
            if keyword in target_lower:
                inferred.extend(paths)
        
        return inferred

    async def fetch(
        self,
        url: str,
        max_length: int = 10000,
        subpages: int = 0,
        subpage_target: Optional[str] = None,
        required_paths: Optional[List[str]] = None,
        query_type: Optional[str] = None,
        **kwargs
    ) -> Dict[str, Any]:
        """
        Fetch using Firecrawl API with unified intelligent strategy.

        Strategy:
        - Single page (subpages=0): Use scrape
        - Multi-page (subpages>0): Use map+scrape with intelligent selection
            * With required_paths: Use explicit paths (most accurate, Deep Research)
            * With subpage_target: Auto-infer paths from keywords (accurate)
            * Neither: Use depth/length/keywords scoring (basic intelligence, still better than random)
        - Fallback: If map+scrape fails, use crawl
        
        All methods raise exception on failure to trigger provider fallback (Exa/Python).
        """
        # Single page mode
        if subpages == 0:
            return await self._scrape(url, max_length)

        # === UNIFIED MAP+SCRAPE STRATEGY FOR ALL MULTI-PAGE ===
        
        # Step 1: Prepare effective_paths
        effective_paths = required_paths  # Explicit paths from Deep Research
        
        # Step 2: Auto-infer from subpage_target if no explicit paths
        if not effective_paths and subpage_target:
            inferred = self._infer_paths_from_target(subpage_target)
            if inferred:
                effective_paths = inferred
                logger.info(f"Auto-inferred paths from '{subpage_target}': {inferred}")
        
        # Step 3: Log strategy
        if effective_paths:
            logger.info(f"Using map+scrape with {len(effective_paths)} path hints for {url}")
        else:
            logger.info(f"Using map+scrape with depth/keywords scoring for {url}")
        
        # Level 1: Map+scrape (UNIVERSAL INTELLIGENT SELECTION)
        try:
            result = await self._map_and_scrape(url, max_length, subpages, effective_paths)
            if result.get("pages_fetched", 0) > 0:
                return result
            logger.warning(f"Map+scrape returned 0 pages for {url}")
        except Exception as e:
            error_msg = str(e)
            if "408" in error_msg or "timeout" in error_msg.lower():
                logger.warning(f"Map+scrape timeout for {url}: {e}")
            elif "429" in error_msg or "rate limit" in error_msg.lower():
                logger.warning(f"Map+scrape rate limited for {url}: {e}")
            else:
                logger.warning(f"Map+scrape failed for {url}: {e}")
        
        # Level 2: Crawl (UNIVERSAL FALLBACK)
        try:
            logger.info(f"Fallback to crawl for {url}")
            result = await self._crawl(url, max_length, subpages)
            if result.get("pages_fetched", 0) > 0:
                return result
            logger.warning(f"Crawl returned 0 pages for {url}")
        except Exception as e:
            logger.warning(f"Crawl also failed for {url}: {e}")
        
        # All Firecrawl methods failed - raise to trigger provider fallback
        logger.error(f"All Firecrawl methods failed for {url}, raising to trigger fallback")
        raise Exception(f"Firecrawl: all methods failed for {url}")


    async def _scrape(self, url: str, max_length: int) -> Dict[str, Any]:
        """Scrape a single page using Firecrawl scrape API"""
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }

        payload = {
            "url": url,
            "formats": ["markdown"],
            "onlyMainContent": True,
            "timeout": self.DEFAULT_TIMEOUT * 1000,  # Firecrawl uses milliseconds
        }

        timeout = aiohttp.ClientTimeout(total=self.AIOHTTP_TIMEOUT)
        async with aiohttp.ClientSession(timeout=timeout) as session:
            async with session.post(
                self.scrape_url, json=payload, headers=headers
            ) as response:
                if response.status == 401:
                    raise Exception("Firecrawl authentication failed (401)")
                elif response.status == 429:
                    raise Exception("Firecrawl rate limit exceeded (429)")
                elif response.status != 200:
                    error_text = await response.text()
                    sanitized = self.sanitize_error_message(error_text)
                    logger.error(f"Firecrawl scrape error: {response.status}")
                    raise Exception(f"Firecrawl error: {response.status}")

                data = await response.json()

                if not data.get("success"):
                    raise Exception("Firecrawl scrape returned unsuccessful result")

                result_data = data.get("data", {})
                content = result_data.get("markdown", "")

                # Truncate if needed
                if len(content) > max_length:
                    content = content[:max_length]

                return {
                    "url": result_data.get("url", url),
                    "title": result_data.get("metadata", {}).get("title", ""),
                    "content": content,
                    "author": result_data.get("metadata", {}).get("author"),
                    "published_date": result_data.get("metadata", {}).get("publishedDate"),
                    "word_count": len(content.split()),
                    "char_count": len(content),
                    "truncated": len(content) >= max_length,
                    "method": "firecrawl",
                    "pages_fetched": 1,
                }

    async def _crawl(self, url: str, max_length: int, limit: int) -> Dict[str, Any]:
        """
        Crawl multiple pages using Firecrawl crawl API.

        This is an async operation - we start the crawl, then poll for results.
        """
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }

        # Start crawl job
        payload = {
            "url": url,
            "limit": min(limit + 1, MAX_SUBPAGES + 1),  # +1 for main page
            "scrapeOptions": {
                "formats": ["markdown"],
                "onlyMainContent": True,
            },
        }

        timeout = aiohttp.ClientTimeout(total=self.DEFAULT_TIMEOUT)
        async with aiohttp.ClientSession(timeout=timeout) as session:
            # Start crawl
            logger.info(f"Firecrawl: Starting crawl job for {url} with limit={limit}")
            async with session.post(
                self.crawl_url, json=payload, headers=headers
            ) as response:
                if response.status == 401:
                    raise Exception("Firecrawl authentication failed (401)")
                elif response.status == 429:
                    raise Exception("Firecrawl rate limit exceeded (429)")
                elif response.status != 200:
                    error_text = await response.text()
                    logger.error(f"Firecrawl crawl start error {response.status}: {error_text[:200]}")
                    raise Exception(f"Firecrawl crawl start error: {response.status}")

                data = await response.json()
                if not data.get("success"):
                    logger.error(f"Firecrawl crawl start failed: {data}")
                    raise Exception("Firecrawl crawl start failed")

                crawl_id = data.get("id")
                if not crawl_id:
                    logger.error(f"Firecrawl did not return crawl ID: {data}")
                    raise Exception("Firecrawl did not return crawl ID")

                logger.info(f"Firecrawl: Crawl job started, ID={crawl_id}")

            # Poll for crawl completion
            status_url = f"{self.crawl_url}/{crawl_id}"
            max_polls = 60  # Max 60 polls (with 2s delay = 2 minutes max)
            poll_delay = 2.0

            for poll_count in range(max_polls):
                await asyncio.sleep(poll_delay)

                async with session.get(status_url, headers=headers) as status_response:
                    if status_response.status != 200:
                        logger.warning(f"Firecrawl: Poll {poll_count+1} status check failed: {status_response.status}")
                        continue

                    status_data = await status_response.json()
                    status = status_data.get("status", "")
                    total = status_data.get("total", 0)
                    completed = status_data.get("completed", 0)

                    logger.info(f"Firecrawl: Poll {poll_count+1}/{max_polls} - status={status}, completed={completed}/{total}")

                    if status == "completed":
                        # Crawl finished, process results
                        results = status_data.get("data", [])
                        logger.info(f"Firecrawl: Crawl completed, got {len(results)} pages")
                        
                        # Debug: Log URLs from Firecrawl response
                        for i, page_data in enumerate(results[:5]):  # Log first 5 URLs
                            page_url = page_data.get("url", "")
                            logger.info(f"Firecrawl page {i+1} URL: {page_url}")
                        
                        return self._merge_crawl_results(results, url, max_length)

                    elif status == "failed":
                        error = status_data.get("error", "Unknown error")
                        logger.error(f"Firecrawl: Crawl job failed - {error}")
                        raise Exception(f"Firecrawl crawl job failed: {error}")

                    # Still in progress (scraping, partial, etc.), continue polling

            logger.error(f"Firecrawl: Crawl timeout after {max_polls} polls")
            raise Exception("Firecrawl crawl timeout - job did not complete in time")


    def _sanitize_url(self, url: str) -> str:
        """Sanitize URL by removing common error patterns"""
        if not url:
            return ""
        
        # Remove known error patterns
        url = url.replace('</parameter', '')
        url = url.replace('%3C/parameter', '')
        url = url.replace('%3E', '')
        url = url.replace('<', '')
        url = url.replace('>', '')
        
        # Remove trailing/leading whitespace
        url = url.strip()
        
        return url
    
    def _merge_crawl_results(
        self, results: List[Dict], original_url: str, max_length: int
    ) -> Dict[str, Any]:
        """Merge multiple crawl results with improved filtering and deduplication"""
        
        if not results:
            return {
                "url": original_url,
                "title": "",
                "content": "",
                "method": "firecrawl",
                "pages_fetched": 0,
            }
        
        # Step 1: Filter failed pages (statusCode != 200)
        valid_pages = []
        failed_count = 0
        for page_data in results:
            metadata = page_data.get("metadata", {})
            status_code = metadata.get("statusCode", 200)
            
            if status_code != 200:
                logger.warning(
                    f"Skipping failed page: {page_data.get('url')} "
                    f"(status={status_code})"
                )
                failed_count += 1
                continue
            
            valid_pages.append(page_data)
        
        # Step 2: Deduplicate by URL
        seen_urls = set()
        unique_pages = []
        duplicate_count = 0
        
        for page_data in valid_pages:
            page_url = page_data.get("url", "")
            
            # Sanitize URL to remove error patterns
            page_url = self._sanitize_url(page_url)
            
            # Skip empty URLs after sanitization
            if not page_url:
                logger.warning("Skipping page with empty URL after sanitization")
                duplicate_count += 1
                continue
            
            if page_url in seen_urls:
                logger.info(f"Skipping duplicate URL: {page_url}")
                duplicate_count += 1
                continue
            
            seen_urls.add(page_url)
            # Update page_data with sanitized URL
            page_data["url"] = page_url
            unique_pages.append(page_data)
        
        if not unique_pages:
            return {
                "url": original_url,
                "title": "",
                "content": "",
                "method": "firecrawl",
                "pages_fetched": 0,
                "metadata": {
                    "total_crawled": len(results),
                    "failed_pages": failed_count,
                    "duplicate_pages": duplicate_count,
                }
            }
        
        # Step 3: Single page optimization
        if len(unique_pages) == 1:
            result = unique_pages[0]
            content = result.get("markdown", "")
            if len(content) > max_length:
                content = content[:max_length]
            
            return {
                "url": result.get("url", original_url),
                "title": result.get("metadata", {}).get("title", ""),
                "content": content,
                "author": result.get("metadata", {}).get("author"),
                "published_date": result.get("metadata", {}).get("publishedDate"),
                "word_count": len(content.split()),
                "char_count": len(content),
                "truncated": len(content) >= max_length,
                "method": "firecrawl",
                "pages_fetched": 1,
            }
        
        # Step 4: Multiple pages - smart truncation
        # Allocate space evenly across pages
        per_page_limit = max_length // len(unique_pages)
        
        merged_content = []
        total_chars = 0
        pages_included = 0
        
        for i, page_data in enumerate(unique_pages):
            page_url = page_data.get("url", "")
            page_title = page_data.get("metadata", {}).get("title", "")
            page_content = page_data.get("markdown", "")
            
            # Truncate individual page
            if len(page_content) > per_page_limit:
                page_content = page_content[:per_page_limit] + "\n\n[Content truncated...]"
            
            # Check total length limit
            if total_chars + len(page_content) > max_length:
                logger.info(
                    f"Reached max_length limit at page {i+1}/{len(unique_pages)}, "
                    f"stopping merge"
                )
                break
            
            # Add page with index (don't expose URLs in content to avoid citation extraction)
            if i == 0:
                page_header = f"# Main Page\n"
                if page_title:
                    page_header += f"**{page_title}**\n\n"
            else:
                page_header = f"\n---\n\n## Subpage {i+1}\n"
                if page_title:
                    page_header += f"**{page_title}**\n\n"
            
            merged_content.append(page_header + page_content)
            total_chars += len(page_content)
            pages_included += 1
        
        # Step 5: Combine all pages
        final_content = "".join(merged_content)
        
        return {
            "url": original_url,
            "title": unique_pages[0].get("metadata", {}).get("title", ""),
            "content": final_content,
            "word_count": len(final_content.split()),
            "char_count": len(final_content),
            "truncated": pages_included < len(unique_pages),
            "method": "firecrawl",
            "pages_fetched": pages_included,
            "metadata": {
                "total_crawled": len(results),
                "valid_pages": len(valid_pages),
                "unique_pages": len(unique_pages),
                "pages_included": pages_included,
                "failed_pages": failed_count,
                "duplicate_pages": duplicate_count,
                "urls": [p.get("url") for p in unique_pages[:pages_included]],
            }
        }

    async def _map(self, url: str, limit: int = 100) -> List[str]:
        """
        Use Firecrawl Map API to quickly get all URLs on a website.

        Returns:
            List[str]: URL list from the website
        """
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }

        payload = {
            "url": url,
            "limit": limit,
        }

        timeout = aiohttp.ClientTimeout(total=self.AIOHTTP_TIMEOUT)
        async with aiohttp.ClientSession(timeout=timeout) as session:
            async with session.post(
                "https://api.firecrawl.dev/v1/map",
                json=payload,
                headers=headers
            ) as response:
                if response.status == 401:
                    raise Exception("Firecrawl authentication failed (401)")
                elif response.status == 429:
                    raise Exception("Firecrawl rate limit exceeded (429)")
                elif response.status != 200:
                    raise Exception(f"Firecrawl map error: {response.status}")

                data = await response.json()
                if not data.get("success"):
                    raise Exception("Firecrawl map failed")

                urls = data.get("links", [])
                logger.info(f"Firecrawl map returned {len(urls)} URLs for {url}")
                return urls

    async def _batch_scrape(
        self, urls: List[str], max_length: int
    ) -> Dict[str, Any]:
        """Batch scrape multiple URLs and merge results with retry for 408/429."""
        if not urls:
            return {
                "url": "",
                "title": "",
                "content": "",
                "method": "firecrawl",
                "pages_fetched": 0,
            }

        max_retries = 2
        retry_delay = 5  # seconds

        # Scrape each URL with retry logic
        results = []
        for url in urls:
            success = False
            for retry in range(max_retries + 1):
                try:
                    result = await self._scrape(url, max_length // len(urls))
                    if result and result.get("content"):
                        results.append({
                            "url": result.get("url", url),
                            "markdown": result.get("content", ""),
                            "metadata": {
                                "title": result.get("title", ""),
                                "author": result.get("author"),
                                "publishedDate": result.get("published_date"),
                            },
                        })
                        success = True
                        break
                except Exception as e:
                    error_msg = str(e)
                    
                    # Handle 408 timeout - retry after delay
                    if "408" in error_msg:
                        if retry < max_retries:
                            logger.warning(f"408 timeout for {url}, retry {retry+1}/{max_retries}")
                            await asyncio.sleep(retry_delay)
                            continue
                        else:
                            logger.error(f"408 timeout for {url} after {max_retries} retries")
                    
                    # Handle 429 rate limit - wait longer and retry
                    elif "429" in error_msg:
                        wait_time = retry_delay * (retry + 2)  # 10s, 15s, 20s
                        if retry < max_retries:
                            logger.warning(f"429 rate limit for {url}, waiting {wait_time}s, retry {retry+1}/{max_retries}")
                            await asyncio.sleep(wait_time)
                            continue
                        else:
                            logger.error(f"429 rate limit for {url} after {max_retries} retries")
                    
                    else:
                        logger.warning(f"Failed to scrape {url}: {e}")

                    break  # Don't retry for other errors

            # Rate limit: add delay between URLs to avoid triggering 429s
            if success and url != urls[-1]:  # Skip delay after last URL
                await asyncio.sleep(0.3)

        return self._merge_crawl_results(results, urls[0] if urls else "", max_length)

    def _calculate_relevance_score(
        self,
        url: str,
        required_paths: Optional[List[str]] = None,
        total_pages: int = 0
    ) -> float:
        """
        Calculate relevance score for a URL (0.0-1.0).
        
        Args:
            url: URL to score
            required_paths: Optional list of required paths (e.g., ['/about', '/team'])
            total_pages: Total number of pages on the website (for context)
        
        Returns:
            Relevance score between 0.0 and 1.0
        """
        score = 0.0
        url_lower = url.lower()
        
        # Factor 1: Required paths matching (weight: 0.5)
        if required_paths:
            for path in required_paths:
                path_lower = path.lower()
                if url_lower.endswith(path_lower) or f"{path_lower}/" in url_lower:
                    score += 0.5  # Exact match
                    break
                elif path_lower.strip('/') in url_lower:
                    score += 0.3  # Partial match
                    break
        
        # Factor 2: URL depth (weight: 0.2)
        # Shallower URLs are generally more important
        depth = url.count('/') - 2  # -2 for https://
        if depth <= 1:
            score += 0.2  # Top-level page
        elif depth == 2:
            score += 0.1  # Second-level page
        
        # Factor 3: URL length (weight: 0.1)
        # Shorter URLs tend to be more important
        if len(url) < 50:
            score += 0.1
        elif len(url) < 80:
            score += 0.05
        
        # Factor 4: Important keywords (weight: 0.2)
        important_keywords = [
            'about', 'team', 'company', 'leadership', 'management',
            'product', 'service', 'pricing', 'features',
            'investor', 'ir', 'investors', 'investor-relations',
            'contact', 'careers', 'jobs',
            'docs', 'documentation', 'guide', 'tutorial', 'api',
            'blog', 'news', 'press',
        ]
        
        for keyword in important_keywords:
            if keyword in url_lower:
                score += 0.05
                break  # Only count once
        
        # Factor 5: Website size adjustment (dynamic weight)
        if total_pages > 0:
            if total_pages > 100:
                # Large website: boost top-level pages
                if depth <= 1:
                    score *= 1.2
            elif total_pages < 20:
                # Small website: all pages are relatively important
                score = max(score, 0.3)
        
        return min(score, 1.0)

    async def _map_and_scrape(
        self,
        url: str,
        max_length: int,
        limit: int,
        required_paths: Optional[List[str]] = None
    ) -> Dict[str, Any]:
        """
        Map website URLs, score by relevance, and scrape top pages.
        
        Uses Firecrawl's map API to get all URLs, then:
        1. Filters by required_paths (if provided)
        2. Scores all URLs by relevance
        3. Selects top N pages
        4. Batch scrapes selected pages
        
        Args:
            url: Base URL to map
            max_length: Max content length
            limit: Number of pages to scrape
            required_paths: Optional paths to prioritize (e.g. ["/about", "/ir"])
        """
        # Step 1: Map to get all URLs
        all_urls = await self._map(url, limit=200)

        if not all_urls:
            raise Exception("Firecrawl map returned no URLs")

        logger.info(f"Map found {len(all_urls)} URLs for {url}")

        # Step 2: Score all URLs by relevance
        urls_with_scores = []
        for page_url in all_urls:
            score = self._calculate_relevance_score(
                page_url,
                required_paths=required_paths,
                total_pages=len(all_urls)
            )
            urls_with_scores.append((page_url, score))

        # Step 3: Sort by score (descending)
        urls_with_scores.sort(key=lambda x: x[1], reverse=True)

        # Step 4: Select top N URLs
        # Filter out very low scores (< 0.2)
        MIN_SCORE_THRESHOLD = 0.2
        selected_urls = [
            url_item for url_item, score in urls_with_scores[:limit]
            if score >= MIN_SCORE_THRESHOLD
        ]

        if not selected_urls:
            # Fallback: if all scores are too low, take top N anyway
            logger.warning(
                f"All URLs scored below {MIN_SCORE_THRESHOLD}, "
                f"taking top {limit} anyway"
            )
            selected_urls = [url_item for url_item, _ in urls_with_scores[:limit]]

        # Log selection summary
        avg_score = sum(s for _, s in urls_with_scores[:len(selected_urls)]) / len(selected_urls)
        logger.info(
            f"Selected {len(selected_urls)}/{len(all_urls)} URLs "
            f"(avg_score={avg_score:.2f}, limit={limit})"
        )

        # Step 5: Batch scrape selected URLs
        return await self._batch_scrape(selected_urls, max_length)




class WebFetchTool(Tool):
    """
    Fetch full content from a web page for detailed analysis.

    Multi-provider architecture:
    1. Exa API: Semantic search + content extraction (handles JS-heavy sites)
    2. Firecrawl: Smart crawling + structured extraction + actions
    3. Pure Python: Free, fast, works for most pages

    Provider selection:
    - Set WEB_FETCH_PROVIDER env var for default (auto|exa|firecrawl|python)
    - Or specify 'provider' parameter per request for runtime override
    """

    def __init__(self):
        # API keys
        self.exa_api_key = os.getenv("EXA_API_KEY")
        self.firecrawl_api_key = os.getenv("FIRECRAWL_API_KEY")

        # Default provider from env (auto = select based on available keys)
        self.default_provider = os.getenv("WEB_FETCH_PROVIDER", "auto").lower()

        # Initialize Firecrawl provider if configured
        self.firecrawl_provider: Optional[FirecrawlFetchProvider] = None
        if self.firecrawl_api_key and WebFetchProvider.validate_api_key(self.firecrawl_api_key):
            try:
                self.firecrawl_provider = FirecrawlFetchProvider(self.firecrawl_api_key)
                logger.info("Initializing firecrawl fetch provider")
            except ValueError as e:
                logger.warning(f"Failed to initialize Firecrawl provider: {e}")


        # Other settings
        self.user_agent = os.getenv(
            "WEB_FETCH_USER_AGENT", "Shannon-Research/1.0 (+https://shannon.kocoro.dev)"
        )
        self.timeout = int(os.getenv("WEB_FETCH_TIMEOUT", "30"))
        # Security limits
        self.max_response_bytes = int(
            os.getenv("WEB_FETCH_MAX_RESPONSE_BYTES", str(50 * 1024 * 1024))
        )  # 50MB
        self.max_redirects = int(os.getenv("WEB_FETCH_MAX_REDIRECTS", "10"))
        super().__init__()

    def _get_metadata(self) -> ToolMetadata:
        # Determine active provider for description
        active_providers = []
        if self.exa_api_key:
            active_providers.append("Exa")
        if self.firecrawl_provider:
            active_providers.append("Firecrawl")
        active_providers.append("Python")  # Always available
        provider_str = ", ".join(active_providers)

        return ToolMetadata(
            name="web_fetch",
            version="3.0.0",  # Bumped for single-page focus
            description=(
                "Fetch full content from a single web page for detailed analysis. "
                "Returns clean markdown text from the specified URL. "
                "Use after web_search to read a specific page."
                "\n\n"
                "Returns: {url, title, content, method, word_count, char_count}."
                "\n\n"
                "For fetching MULTIPLE pages from a website, use:\n"
                "• web_subpage_fetch: Targeted multi-page with path selection (Map + Scrape)\n"
                "• web_crawl: Exploratory crawl for unknown structure"
            ),
            category="retrieval",
            author="Shannon",
            requires_auth=False,
            rate_limit=30,
            timeout_seconds=30,
            memory_limit_mb=256,
            sandboxed=False,
            dangerous=False,
            cost_per_use=0.001,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="url",
                type=ToolParameterType.STRING,
                description="URL of the page to fetch",
                required=True,
            ),
            ToolParameter(
                name="max_length",
                type=ToolParameterType.INTEGER,
                description="Maximum content length in characters (for LLM context efficiency)",
                required=False,
                default=10000,
                min_value=1000,
                max_value=50000,
            ),
            # DEPRECATED: Keep for backward compatibility with Go orchestrator
            # These will be removed in a future version
            ToolParameter(
                name="subpages",
                type=ToolParameterType.INTEGER,
                description=(
                    "[DEPRECATED] Use web_subpage_fetch or web_crawl for multi-page. "
                    "This parameter is ignored. Kept for backward compatibility."
                ),
                required=False,
                default=0,
                min_value=0,
                max_value=MAX_SUBPAGES,
            ),
            ToolParameter(
                name="subpage_target",
                type=ToolParameterType.STRING,
                description="[DEPRECATED] Use web_subpage_fetch target_keywords instead.",
                required=False,
            ),
            ToolParameter(
                name="required_paths",
                type=ToolParameterType.ARRAY,
                description="[DEPRECATED] Use web_subpage_fetch target_paths instead.",
                required=False,
            ),
            ToolParameter(
                name="query_type",
                type=ToolParameterType.STRING,
                description="[DEPRECATED] No longer used in simplified web_fetch.",
                required=False,
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        """Execute web content fetch using selected provider."""

        url = kwargs.get("url")
        # Provider is now controlled by WEB_FETCH_PROVIDER env var only
        provider_name = self.default_provider
        max_length = kwargs.get("max_length", 10000)

        # DEPRECATED: These parameters are ignored in v3.0.0
        # Log warning if caller uses deprecated params
        deprecated_subpages = kwargs.get("subpages", 0)
        deprecated_subpage_target = kwargs.get("subpage_target")
        deprecated_required_paths = kwargs.get("required_paths")
        deprecated_query_type = kwargs.get("query_type")

        if deprecated_subpages > 0 or deprecated_subpage_target or deprecated_required_paths:
            logger.warning(
                "web_fetch deprecated params used: subpages=%d, subpage_target=%s, required_paths=%s. "
                "Use web_subpage_fetch or web_crawl instead. Params will be ignored.",
                deprecated_subpages, deprecated_subpage_target, deprecated_required_paths
            )

        # Force single-page mode (deprecated params are ignored)
        subpages = 0
        subpage_target = None
        required_paths = None
        query_type = None

        if not url:
            return ToolResult(success=False, output=None, error="URL parameter required")

        # Validate URL
        try:
            parsed = urlparse(url)
            if not parsed.scheme or not parsed.netloc:
                return ToolResult(
                    success=False, output=None, error=f"Invalid URL: {url}"
                )

            # Only allow HTTP/HTTPS
            if parsed.scheme not in ["http", "https"]:
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Only HTTP/HTTPS protocols allowed, got: {parsed.scheme}",
                )

            # SSRF protection: Block private/internal IPs
            if _is_private_ip(parsed.netloc.split(":")[0]):
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Access to private/internal IP addresses is not allowed: {parsed.netloc}",
                )

            # Check if this URL/domain was previously marked as failed
            if session_context:
                failed_domains = session_context.get("failed_domains", [])
                for failed_url in failed_domains:
                    if url == failed_url or parsed.netloc in failed_url:
                        logger.info(f"Skipping previously failed domain: {url}")
                        return ToolResult(
                            success=False,
                            output=None,
                            error=f"Domain previously failed, skipping: {parsed.netloc}",
                        )

        except Exception as e:
            return ToolResult(
                success=False, output=None, error=f"Invalid URL format: {str(e)}"
            )

        attempts = []

        def _attach_metadata(result: ToolResult, provider_label: str) -> ToolResult:
            meta = result.metadata or {}
            meta.setdefault("fetch_method", provider_label)
            meta["provider_used"] = provider_label
            meta["attempts"] = attempts
            meta.setdefault("urls_requested", [url])
            meta.setdefault("urls_attempted", [url])
            if result.success:
                meta.setdefault("urls_succeeded", [url])
                meta.setdefault("urls_failed", [])
                meta["partial_success"] = len(meta.get("urls_failed", [])) > 0
                meta["failure_summary"] = {
                    "failed_count": len(meta.get("urls_failed", [])),
                    "total_count": len(meta.get("urls_attempted", [url])),
                }
            else:
                meta.setdefault("urls_succeeded", [])
                meta.setdefault(
                    "urls_failed", [{"url": url, "reason": result.error or "unknown"}]
                )
                meta["partial_success"] = False
                meta["failure_summary"] = {
                    "failed_count": len(meta.get("urls_failed", [])),
                    "total_count": len(meta.get("urls_attempted", [url])),
                }
            result.metadata = meta
            return result

        try:
            # Emit progress
            observer = kwargs.get("observer")
            if observer:
                try:
                    observer("progress", {"message": f"Fetching content from {url}..."})
                except Exception:
                    pass

            # Resolve provider: auto-select or use specified
            selected_provider = self._resolve_provider(provider_name, subpages)
            logger.info(f"Fetching with {selected_provider}: {url} (subpages={subpages})")

            # Convert required_paths to subpage_target for non-firecrawl providers
            effective_subpage_target = subpage_target
            if required_paths and selected_provider != "firecrawl":
                # Exa supports subpage_target for keyword filtering
                effective_subpage_target = " ".join([p.strip("/") for p in required_paths])
                logger.info(f"Converted required_paths to subpage_target: {effective_subpage_target}")

            # Execute with selected provider
            if selected_provider == "firecrawl":
                if not self.firecrawl_provider:
                    logger.warning("Firecrawl requested but not configured, falling back")
                    attempts.append(
                        {
                            "provider": "firecrawl",
                            "status": "skipped",
                            "reason": "not_configured",
                        }
                    )
                    selected_provider = "exa" if self.exa_api_key else "python"
                else:
                    try:
                        result_data = await self.firecrawl_provider.fetch(
                            url, max_length, subpages, subpage_target,
                            required_paths=required_paths,
                            query_type=query_type
                        )
                        attempts.append(
                            {"provider": "firecrawl", "status": "success"}
                        )
                        return _attach_metadata(
                            ToolResult(
                                success=True,
                                output=result_data,
                                metadata={"fetch_method": "firecrawl"},
                            ),
                            "firecrawl",
                        )
                    except Exception as e:
                        logger.error(f"Firecrawl fetch failed: {e}, falling back")
                        attempts.append(
                            {
                                "provider": "firecrawl",
                                "status": "failed",
                                "error": str(e)[:200],
                            }
                        )
                        selected_provider = "exa" if self.exa_api_key else "python"

            if selected_provider == "exa":
                if self.exa_api_key:
                    result = await self._fetch_with_exa(
                        url, max_length, subpages, effective_subpage_target
                    )
                    attempts.append(
                        {"provider": "exa", "status": "success" if result.success else "failed"}
                    )
                    provider_label = (
                        (result.metadata or {}).get("fetch_method") or "exa"
                    )
                    return _attach_metadata(result, provider_label)
                else:
                    logger.warning("Exa requested but not configured, falling back to Python")
                    attempts.append(
                        {
                            "provider": "exa",
                            "status": "skipped",
                            "reason": "not_configured",
                        }
                    )
                    selected_provider = "python"

            # Fallback to pure Python
            result = await self._fetch_pure_python(url, max_length, subpages)
            attempts.append(
                {"provider": "python", "status": "success" if result.success else "failed"}
            )
            return _attach_metadata(result, "python")

        except Exception as e:
            logger.error(f"Failed to fetch {url}: {str(e)}")
            attempts.append(
                {"provider": "unknown", "status": "failed", "error": str(e)[:200]}
            )
            return _attach_metadata(
                ToolResult(
                    success=False, output=None, error=f"Failed to fetch page: {str(e)}"
                ),
                "unknown",
            )

    def _resolve_provider(self, provider_name: str, subpages: int) -> str:
        """
        Resolve which provider to use based on request and availability.

        Auto-selection logic:
        - For multi-page (subpages > 0): prefer Firecrawl > Exa > Python
        - For single page: prefer Exa > Firecrawl > Python
        """
        # Handle explicit provider request
        if provider_name in ["exa", "firecrawl", "python"]:
            return provider_name

        # Auto-select based on task and availability
        if provider_name == "auto" or provider_name == self.default_provider:
            if subpages > 0:
                # Multi-page: prefer Firecrawl for its crawl capabilities
                if self.firecrawl_provider:
                    return "firecrawl"
                elif self.exa_api_key:
                    return "exa"
                else:
                    return "python"
            else:
                # Single page: prefer Exa for semantic extraction
                if self.exa_api_key:
                    return "exa"
                elif self.firecrawl_provider:
                    return "firecrawl"
                else:
                    return "python"

        # Default fallback
        return self.default_provider if self.default_provider != "auto" else "python"

    def _normalize_url(self, url: str) -> str:
        """
        Normalize URL for deduplication:
        - Remove trailing slash (except for root path)
        - Sort query parameters properly (handles duplicates and encoded chars)
        - Remove fragments
        """
        parsed = urlparse(url)
        path = parsed.path or "/"
        # Remove trailing slash (except for root)
        if path != "/" and path.endswith("/"):
            path = path[:-1]
        # Build normalized URL
        normalized = f"{parsed.scheme}://{parsed.netloc}{path}"
        # Sort query parameters properly using parse_qsl/urlencode
        # This handles duplicate params (?a=1&a=2) and encoded chars correctly
        if parsed.query:
            params = parse_qsl(parsed.query, keep_blank_values=True)
            sorted_params = urlencode(sorted(params))
            normalized += f"?{sorted_params}"
        return normalized

    def _is_safe_url(self, url: str) -> bool:
        """
        SSRF protection: check if URL is safe to fetch.
        Returns False for private IPs, internal networks, cloud metadata endpoints.
        """
        try:
            parsed = urlparse(url)
            host = parsed.netloc.split(":")[0]  # Remove port if present
            return not _is_private_ip(host)
        except Exception:
            return False

    def _extract_same_domain_links(self, soup: BeautifulSoup, base_url: str, root_domain: str) -> List[str]:
        """Extract all same-domain links from parsed HTML with SSRF protection."""
        links = []
        seen = set()
        for a_tag in soup.find_all("a", href=True):
            href = a_tag["href"].strip()
            # Skip empty hrefs, anchors, and non-HTTP schemes
            if not href or href.startswith(("#", "javascript:", "mailto:", "tel:", "data:", "ftp:", "file:")):
                continue
            # Resolve relative URLs
            try:
                full_url = urljoin(base_url, href)
                parsed = urlparse(full_url)
            except Exception:
                continue  # Skip malformed URLs
            # Only same-domain links with HTTP/HTTPS
            if parsed.netloc == root_domain and parsed.scheme in ("http", "https"):
                # Normalize URL for deduplication
                clean_url = self._normalize_url(full_url)
                # SSRF protection for each extracted link
                if clean_url not in seen and self._is_safe_url(clean_url):
                    seen.add(clean_url)
                    links.append(clean_url)
        return links

    async def _fetch_single_page(
        self, session: aiohttp.ClientSession, url: str, max_length: int, root_domain: str
    ) -> Optional[Dict]:
        """
        Fetch a single page and return structured data.
        Returns None on failure (silent fail for subpages).

        Security: Validates redirect destination stays on same domain.
        """
        try:
            headers = {"User-Agent": self.user_agent}
            async with session.get(
                url,
                headers=headers,
                allow_redirects=True,
                max_redirects=self.max_redirects,
            ) as response:
                if response.status != 200:
                    return None

                # SSRF protection: Check redirect destination is same domain
                final_host = response.url.host
                if final_host != root_domain:
                    logger.debug(f"Redirect to different domain blocked: {url} -> {response.url}")
                    return None

                # SSRF protection: Check final URL is not private IP
                if not self._is_safe_url(str(response.url)):
                    logger.debug(f"Redirect to unsafe URL blocked: {url} -> {response.url}")
                    return None

                # Content-Type validation: skip explicitly non-HTML responses
                # Allow missing Content-Type (attempt to parse anyway)
                content_type = response.headers.get("Content-Type", "")
                if content_type and "text/html" not in content_type.lower() and "text/plain" not in content_type.lower():
                    logger.debug(f"Skipping non-HTML content: {url} ({content_type})")
                    return None

                html_content = await response.text(errors="ignore")
                if len(html_content) > self.max_response_bytes:
                    return None

                soup = BeautifulSoup(html_content, "lxml")

                # Extract metadata
                title = soup.find("title")
                title = title.get_text().strip() if title else ""

                # Extract links before removing elements
                links = self._extract_same_domain_links(soup, str(response.url), root_domain)

                # Remove unwanted elements
                for element in soup(
                    ["script", "style", "nav", "header", "footer", "aside", "iframe", "noscript"]
                ):
                    element.decompose()

                # Extract main content
                main_content = (
                    soup.find("article")
                    or soup.find("main")
                    or soup.find("div", class_="content")
                    or soup.find("div", class_="post")
                    or soup.find("body")
                    or soup
                )

                # Convert to markdown
                h = html2text.HTML2Text()
                h.ignore_links = False
                h.ignore_images = False
                h.ignore_emphasis = False
                h.body_width = 0
                h.unicode_snob = True
                h.skip_internal_links = True

                markdown = h.handle(str(main_content))

                # Clean up whitespace
                lines = [line.rstrip() for line in markdown.split("\n")]
                markdown = "\n".join(lines)
                while "\n\n\n" in markdown:
                    markdown = markdown.replace("\n\n\n", "\n\n")

                # Truncate if needed
                if len(markdown) > max_length:
                    markdown = markdown[:max_length]

                return {
                    "url": str(response.url),
                    "title": title,
                    "content": markdown,
                    "links": links,
                }
        except Exception as e:
            logger.debug(f"Failed to fetch subpage {url}: {str(e)}")
            return None

    async def _crawl_with_subpages(
        self, session: aiohttp.ClientSession, url: str, max_length: int, subpages: int
    ) -> ToolResult:
        """
        Crawl main page plus subpages using BFS with page count limit.
        Returns merged markdown content from all fetched pages.

        Security features:
        - SSRF protection on all crawled URLs
        - Redirect domain validation
        - Rate limiting between requests (configurable delay)
        - URL normalization for deduplication
        - Total crawl timeout
        - Cumulative content size limit
        """
        root_domain = urlparse(url).netloc
        visited = set()
        normalized_start = self._normalize_url(url)
        queue = [normalized_start]
        visited.add(normalized_start)
        pages = []
        max_pages = subpages + 1  # Main page + N subpages
        total_chars = 0

        logger.info(f"Starting pure-Python crawl: {url} (max_pages={max_pages})")

        crawl_start = asyncio.get_event_loop().time()
        is_first_request = True

        while queue and len(pages) < max_pages:
            # Check total crawl timeout
            elapsed = asyncio.get_event_loop().time() - crawl_start
            if elapsed > CRAWL_TIMEOUT_SECONDS:
                logger.info(f"Crawl timeout reached ({CRAWL_TIMEOUT_SECONDS}s), stopping")
                break

            # Check cumulative content size
            if total_chars >= MAX_TOTAL_CRAWL_CHARS:
                logger.info(f"Content size limit reached ({MAX_TOTAL_CRAWL_CHARS} chars), stopping")
                break

            current_url = queue.pop(0)

            # Rate limiting: delay between requests (skip first request)
            if not is_first_request:
                await asyncio.sleep(CRAWL_DELAY_SECONDS)
            is_first_request = False

            # SSRF check before fetching
            if not self._is_safe_url(current_url):
                logger.debug(f"Skipping unsafe URL (SSRF protection): {current_url}")
                continue

            # Fetch the page (includes redirect domain validation)
            page_data = await self._fetch_single_page(session, current_url, max_length, root_domain)
            if page_data:
                pages.append(page_data)
                total_chars += len(page_data.get("content", ""))
                logger.debug(f"Crawled page {len(pages)}/{max_pages}: {current_url}")

                # Add new links to queue (only what we need)
                if len(pages) < max_pages:
                    remaining_pages = max_pages - len(pages)
                    new_links = page_data.get("links", [])[:remaining_pages * 3]  # Buffer for failed fetches
                    for link in new_links:
                        normalized_link = self._normalize_url(link)
                        if normalized_link not in visited:
                            visited.add(normalized_link)
                            queue.append(normalized_link)

        if not pages:
            return ToolResult(
                success=False,
                output=None,
                error="Failed to fetch any pages",
            )

        # Merge pages into markdown format
        if len(pages) == 1:
            # Single page result
            page = pages[0]
            return ToolResult(
                success=True,
                output={
                    "url": page["url"],
                    "title": page["title"],
                    "content": page["content"],
                    "author": None,
                    "published_date": None,
                    "word_count": len(page["content"].split()),
                    "char_count": len(page["content"]),
                    "truncated": False,
                    "method": "pure_python",
                    "pages_fetched": 1,
                },
                metadata={
                    "fetch_method": "pure_python_crawl",
                    "pages_requested": max_pages,
                },
            )
        else:
            # Multiple pages - merge with markdown separators
            merged_content = []
            total_words = 0
            total_chars = 0

            for i, page in enumerate(pages):
                page_url = page.get("url", "")
                page_title = page.get("title", "")
                page_content = page.get("content", "")

                if i == 0:
                    # Main page
                    merged_content.append(f"# Main Page: {page_url}\n")
                    if page_title:
                        merged_content.append(f"**{page_title}**\n")
                else:
                    # Subpage
                    merged_content.append(f"\n---\n\n## Subpage {i}: {page_url}\n")
                    if page_title:
                        merged_content.append(f"**{page_title}**\n")

                merged_content.append(f"\n{page_content}\n")
                total_words += len(page_content.split())
                total_chars += len(page_content)

            final_content = "".join(merged_content)
            logger.info(f"Crawl complete: {len(pages)} pages, {total_chars} chars")

            return ToolResult(
                success=True,
                output={
                    "url": pages[0]["url"],
                    "title": pages[0]["title"],
                    "content": final_content,
                    "author": None,
                    "published_date": None,
                    "word_count": total_words,
                    "char_count": total_chars,
                    "truncated": False,
                    "method": "pure_python",
                    "pages_fetched": len(pages),
                },
                metadata={
                    "fetch_method": "pure_python_crawl",
                    "pages_requested": max_pages,
                    "pages_fetched": len(pages),
                    "urls_crawled": [p["url"] for p in pages],
                },
            )

    async def _fetch_pure_python(self, url: str, max_length: int, subpages: int = 0) -> ToolResult:
        """
        Fetch using pure Python: requests + BeautifulSoup + html2text.
        Fast, free, works for most pages (80%).

        When subpages > 0, crawls same-domain links up to the specified count.

        Security features:
        - Max response size limit (prevents memory exhaustion)
        - Redirect loop protection
        - Resource leak prevention with proper session cleanup
        """
        # Configure session with security limits
        timeout = aiohttp.ClientTimeout(total=self.timeout)
        connector = aiohttp.TCPConnector(limit=10)

        async with aiohttp.ClientSession(timeout=timeout, connector=connector) as session:
            try:
                # Crawl mode: fetch main page + subpages
                if subpages > 0:
                    return await self._crawl_with_subpages(session, url, max_length, subpages)

                # Single page mode (original behavior)
                headers = {"User-Agent": self.user_agent}

                async with session.get(
                    url,
                    headers=headers,
                    allow_redirects=True,
                    max_redirects=self.max_redirects,
                ) as response:
                    if response.status != 200:
                        return ToolResult(
                            success=False,
                            output=None,
                            error=f"HTTP {response.status}: Failed to fetch page",
                        )

                    # Check content length before reading (if provided)
                    content_length = response.headers.get("Content-Length")
                    if content_length and int(content_length) > self.max_response_bytes:
                        return ToolResult(
                            success=False,
                            output=None,
                            error=f"Response too large: {content_length} bytes (max: {self.max_response_bytes})",
                        )

                    # Read with size limit
                    html_content = await response.text(
                        errors="ignore"
                    )  # Ignore encoding errors

                    # Check actual size after reading
                    if len(html_content) > self.max_response_bytes:
                        return ToolResult(
                            success=False,
                            output=None,
                            error=f"Response too large: {len(html_content)} bytes (max: {self.max_response_bytes})",
                        )

                    # Parse HTML (outside the response context manager to avoid blocking)
                    soup = BeautifulSoup(html_content, "lxml")

                # Extract metadata
                title = soup.find("title")
                title = title.get_text().strip() if title else ""

                # Author
                meta_author = soup.find("meta", attrs={"name": "author"})
                if not meta_author:
                    meta_author = soup.find("meta", property="article:author")
                author = meta_author.get("content") if meta_author else None

                # Published date
                meta_date = soup.find(
                    "meta", attrs={"property": "article:published_time"}
                )
                if not meta_date:
                    meta_date = soup.find("meta", attrs={"name": "date"})
                if not meta_date:
                    meta_date = soup.find("meta", attrs={"name": "publish-date"})
                published_date = meta_date.get("content") if meta_date else None

                # Remove unwanted elements
                for element in soup(
                    [
                        "script",
                        "style",
                        "nav",
                        "header",
                        "footer",
                        "aside",
                        "iframe",
                        "noscript",
                    ]
                ):
                    element.decompose()

                # Extract main content (prioritize article > main > body)
                main_content = (
                    soup.find("article")
                    or soup.find("main")
                    or soup.find("div", class_="content")
                    or soup.find("div", class_="post")
                    or soup.find("body")
                    or soup
                )

                # Convert to markdown
                h = html2text.HTML2Text()
                h.ignore_links = False
                h.ignore_images = False
                h.ignore_emphasis = False
                h.body_width = 0  # No line wrapping
                h.unicode_snob = True  # Better character handling
                h.skip_internal_links = True

                markdown = h.handle(str(main_content))

                # Clean up excessive whitespace
                lines = [line.rstrip() for line in markdown.split("\n")]
                markdown = "\n".join(lines)

                # Remove excessive blank lines
                while "\n\n\n" in markdown:
                    markdown = markdown.replace("\n\n\n", "\n\n")

                # Truncate if needed
                truncated = False
                if len(markdown) > max_length:
                    markdown = markdown[:max_length]
                    truncated = True

                return ToolResult(
                    success=True,
                    output={
                        "url": str(response.url),  # Final URL after redirects
                        "title": title,
                        "content": markdown,
                        "author": author,
                        "published_date": published_date,
                        "word_count": len(markdown.split()),
                        "char_count": len(markdown),
                        "truncated": truncated,
                        "method": "pure_python",
                        "pages_fetched": 1,
                    },
                    metadata={
                        "fetch_method": "pure_python",
                        "status_code": response.status,
                        "content_type": response.headers.get("Content-Type", ""),
                    },
                )

            except aiohttp.TooManyRedirects:
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Too many redirects (max: {self.max_redirects})",
                )

            except aiohttp.ClientError as e:
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Network error: {str(e)}",
                )
            except Exception as e:
                logger.error(f"Unexpected error fetching {url}: {str(e)}")
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Parsing error: {str(e)}",
                )

    async def _fetch_with_exa(
        self, url: str, max_length: int, subpages: int = 0, subpage_target: str = None
    ) -> ToolResult:
        """
        Fetch using Exa content API.
        Premium: handles JS-heavy sites, costs $0.001/page.

        Supports fetching subpages when subpages > 0.

        Improved error handling with detailed logging for auth/rate limit failures.
        """
        # Exa requires searching first to get IDs, then fetching content
        # For direct URL fetch, we use a workaround: search for exact URL
        timeout = aiohttp.ClientTimeout(total=30)
        async with aiohttp.ClientSession(timeout=timeout) as session:
            try:
                headers = {
                    "x-api-key": self.exa_api_key,
                    "Content-Type": "application/json",
                }

                # Step 1: Search for the exact URL
                search_payload = {
                    "query": url,
                    "numResults": 1,
                    "includeDomains": [urlparse(url).netloc],
                }

                # Add subpages parameters if requested
                if subpages > 0:
                    search_payload["subpages"] = subpages
                    search_payload["livecrawl"] = "preferred"  # Force fresh crawling for subpages
                    if subpage_target:
                        search_payload["subpageTarget"] = subpage_target
                    logger.info(f"Exa subpages enabled: count={subpages}, target={subpage_target}")
                else:
                    search_payload["livecrawl"] = "fallback"  # Use cached when possible for single pages

                async with session.post(
                    "https://api.exa.ai/search",
                    json=search_payload,
                    headers=headers,
                ) as search_response:
                    # Detailed error logging for auth/rate limit failures
                    if search_response.status == 401:
                        logger.error("Exa API authentication failed (401 Unauthorized)")
                        logger.info("Falling back to pure Python mode")
                        return await self._fetch_pure_python(url, max_length)
                    elif search_response.status == 429:
                        logger.warning("Exa API rate limit exceeded (429 Too Many Requests)")
                        logger.info("Falling back to pure Python mode")
                        return await self._fetch_pure_python(url, max_length)
                    elif search_response.status != 200:
                        logger.warning(
                            f"Exa search failed with status {search_response.status}"
                        )
                        logger.info("Falling back to pure Python mode")
                        return await self._fetch_pure_python(url, max_length)

                    search_data = await search_response.json()
                    results = search_data.get("results", [])

                    if not results:
                        logger.info("Exa found no results, falling back to pure Python")
                        return await self._fetch_pure_python(url, max_length)

                    # Collect all result IDs (main page + subpages if applicable)
                    result_ids = [r.get("id") for r in results if r.get("id")]
                    logger.info(f"Exa search returned {len(result_ids)} result(s)")

                # Step 2: Get full content using ID(s)
                content_payload = {
                    "ids": result_ids,
                    "text": {
                        "maxCharacters": max_length,
                        "includeHtmlTags": False,
                    },
                }

                async with session.post(
                    "https://api.exa.ai/contents",
                    json=content_payload,
                    headers=headers,
                ) as content_response:
                    if content_response.status == 401:
                        logger.error("Exa API authentication failed (401 Unauthorized)")
                        logger.info("Falling back to pure Python mode")
                        return await self._fetch_pure_python(url, max_length)
                    elif content_response.status == 429:
                        logger.warning("Exa API rate limit exceeded (429 Too Many Requests)")
                        logger.info("Falling back to pure Python mode")
                        return await self._fetch_pure_python(url, max_length)
                    elif content_response.status != 200:
                        logger.warning(
                            f"Exa contents failed with status {content_response.status}"
                        )
                        logger.info("Falling back to pure Python mode")
                        return await self._fetch_pure_python(url, max_length)

                    content_data = await content_response.json()
                    results = content_data.get("results", [])

                    if not results:
                        logger.warning("Exa API returned no content")
                        return ToolResult(
                            success=False,
                            output=None,
                            error="Exa API returned no content",
                        )

                    # Handle single page vs multiple pages (subpages)
                    if len(results) == 1:
                        # Single page - return as before
                        result = results[0]
                        content = result.get("text", "")

                        return ToolResult(
                            success=True,
                            output={
                                "url": result.get("url", url),
                                "title": result.get("title", ""),
                                "content": content,
                                "author": result.get("author"),
                                "published_date": result.get("publishedDate"),
                                "word_count": len(content.split()),
                                "char_count": len(content),
                                "truncated": len(content) >= max_length,
                                "method": "exa",
                                "pages_fetched": 1,
                            },
                            metadata={
                                "fetch_method": "exa",
                                "exa_id": result_ids[0],
                                "exa_score": result.get("score"),
                            },
                        )
                    else:
                        # Multiple pages - merge with markdown separators
                        merged_content = []
                        total_words = 0
                        total_chars = 0

                        for i, result in enumerate(results):
                            page_url = result.get("url", "")
                            page_title = result.get("title", "")
                            page_content = result.get("text", "")

                            if i == 0:
                                # Main page
                                merged_content.append(f"# Main Page: {page_url}\n")
                                if page_title:
                                    merged_content.append(f"**{page_title}**\n")
                            else:
                                # Subpage
                                merged_content.append(f"\n---\n\n## Subpage {i}: {page_url}\n")
                                if page_title:
                                    merged_content.append(f"**{page_title}**\n")

                            merged_content.append(page_content)
                            total_words += len(page_content.split())
                            total_chars += len(page_content)

                        final_content = "\n".join(merged_content)

                        return ToolResult(
                            success=True,
                            output={
                                "url": url,
                                "title": results[0].get("title", ""),
                                "content": final_content,
                                "word_count": total_words,
                                "char_count": total_chars,
                                "truncated": False,
                                "method": "exa",
                                "pages_fetched": len(results),
                            },
                            metadata={
                                "fetch_method": "exa",
                                "exa_ids": result_ids,
                                "num_pages": len(results),
                            },
                        )

            except aiohttp.ClientError as e:
                logger.error(f"Exa network error: {str(e)}, falling back to pure Python")
                return await self._fetch_pure_python(url, max_length)
            except Exception as e:
                logger.error(f"Exa unexpected error: {str(e)}, falling back to pure Python")
                return await self._fetch_pure_python(url, max_length)
