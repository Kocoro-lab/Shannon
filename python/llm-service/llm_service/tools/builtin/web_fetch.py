"""
Web Fetch Tool - Extract full page content for deep analysis

Hybrid approach:
- Default: Exa content API when configured (handles JS-heavy sites)
- Fallback: Pure Python (free, fast, works for most pages)

Security features:
- SSRF protection (blocks private IPs, cloud metadata endpoints)
- Memory exhaustion prevention (50MB response limit)
- Redirect loop protection (max 10 redirects)
"""

import aiohttp
import asyncio
import os
import logging
from typing import Dict, Optional, List
from urllib.parse import urlparse, urljoin
from bs4 import BeautifulSoup
import html2text

from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult
from ..openapi_parser import _is_private_ip

logger = logging.getLogger(__name__)

# Constants
MAX_SUBPAGES = 5
CRAWL_DELAY_SECONDS = 0.5  # Rate limiting between crawl requests


class WebFetchTool(Tool):
    """
    Fetch full content from a web page for detailed analysis.

    Two modes:
    1. Exa API (default when configured): Premium content extraction with JS rendering
    2. Pure Python (fallback): Fast, free, works for most pages
    """

    def __init__(self):
        self.exa_api_key = os.getenv("EXA_API_KEY")
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
        return ToolMetadata(
            name="web_fetch",
            version="1.0.0",
            description=(
                "Fetch full content from a web page for detailed analysis. "
                "Extracts clean markdown text from any URL. "
                "Use this after web_search to deep-dive into specific pages. "
                "Supports fetching subpages from the same domain (set subpages>0). "
                "Default: Exa API when available (handles JS-heavy sites, premium extraction). "
                "Fallback: pure Python (fast, free) when Exa is unavailable or disabled."
            ),
            category="retrieval",
            author="Shannon",
            requires_auth=False,
            rate_limit=30,  # 30 requests per minute
            timeout_seconds=30,
            memory_limit_mb=256,
            sandboxed=False,
            dangerous=False,
            cost_per_use=0.001,  # Default: Exa ($0.001/page). Free fallback: pure Python
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
                name="use_exa",
                type=ToolParameterType.BOOLEAN,
                description="Whether to use Exa API (default: true, recommended for best results). Handles JS-heavy sites. Set to false only for debugging or when Exa is unavailable.",
                required=False,
                default=True,
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
            ToolParameter(
                name="subpages",
                type=ToolParameterType.INTEGER,
                description=(
                    f"Number of same-domain subpages to include (0-{MAX_SUBPAGES}). "
                    "0 = only main page (default). "
                    "Use subpages>0 for: documentation sites, API references, multi-part guides. "
                    "Use subpages=0 for: blog posts, news articles, PDFs, single-page content. "
                    "WARNING: Higher values consume more tokens/cost."
                ),
                required=False,
                default=0,
                min_value=0,
                max_value=MAX_SUBPAGES,
            ),
            ToolParameter(
                name="subpage_target",
                type=ToolParameterType.STRING,
                description=(
                    "Optional keywords to prioritize certain subpages (Exa only). "
                    "Example: 'API' to focus on API documentation subpages. "
                    "Ignored in pure-Python mode."
                ),
                required=False,
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        """Execute web content fetch."""

        url = kwargs.get("url")
        use_exa = kwargs.get("use_exa", True)
        max_length = kwargs.get("max_length", 10000)
        subpages = kwargs.get("subpages", 0)
        subpage_target = kwargs.get("subpage_target")

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
            if _is_private_ip(parsed.netloc.split(":")[0]):  # Remove port if present
                return ToolResult(
                    success=False,
                    output=None,
                    error=f"Access to private/internal IP addresses is not allowed: {parsed.netloc}",
                )

        except Exception as e:
            return ToolResult(
                success=False, output=None, error=f"Invalid URL format: {str(e)}"
            )

        try:
            # Emit progress
            observer = kwargs.get("observer")
            if observer:
                try:
                    observer("progress", {"message": f"Fetching content from {url}..."})
                except Exception:
                    pass

            # Choose method: Exa API or Pure Python
            if use_exa and self.exa_api_key:
                logger.info(f"Fetching with Exa API: {url} (subpages={subpages})")
                return await self._fetch_with_exa(url, max_length, subpages, subpage_target)
            else:
                logger.info(f"Fetching with pure Python: {url} (subpages={subpages})")
                return await self._fetch_pure_python(url, max_length, subpages)

        except Exception as e:
            logger.error(f"Failed to fetch {url}: {str(e)}")
            return ToolResult(
                success=False, output=None, error=f"Failed to fetch page: {str(e)}"
            )

    def _normalize_url(self, url: str) -> str:
        """
        Normalize URL for deduplication:
        - Remove trailing slash (except for root path)
        - Sort query parameters
        - Remove fragments
        """
        parsed = urlparse(url)
        path = parsed.path
        # Remove trailing slash (except for root)
        if path != "/" and path.endswith("/"):
            path = path[:-1]
        # Build normalized URL
        normalized = f"{parsed.scheme}://{parsed.netloc}{path}"
        # Sort query parameters for consistent deduplication
        if parsed.query:
            sorted_params = "&".join(sorted(parsed.query.split("&")))
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
            href = a_tag["href"]
            # Skip anchors, javascript, mailto, etc.
            if href.startswith(("#", "javascript:", "mailto:", "tel:")):
                continue
            # Resolve relative URLs
            full_url = urljoin(base_url, href)
            parsed = urlparse(full_url)
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
        self, session: aiohttp.ClientSession, url: str, max_length: int
    ) -> Optional[Dict]:
        """
        Fetch a single page and return structured data.
        Returns None on failure (silent fail for subpages).
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

                # Content-Type validation: skip non-HTML responses
                content_type = response.headers.get("Content-Type", "")
                if content_type and "text/html" not in content_type.lower():
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
                root_domain = urlparse(url).netloc
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
        - Rate limiting between requests (500ms delay)
        - URL normalization for deduplication
        """
        root_domain = urlparse(url).netloc
        visited = set()
        normalized_start = self._normalize_url(url)
        queue = [normalized_start]
        visited.add(normalized_start)
        pages = []
        max_pages = subpages + 1  # Main page + N subpages

        logger.info(f"Starting pure-Python crawl: {url} (max_pages={max_pages})")

        is_first_request = True
        while queue and len(pages) < max_pages:
            current_url = queue.pop(0)

            # Rate limiting: delay between requests (skip first request)
            if not is_first_request:
                await asyncio.sleep(CRAWL_DELAY_SECONDS)
            is_first_request = False

            # SSRF check before fetching
            if not self._is_safe_url(current_url):
                logger.warning(f"Skipping unsafe URL (SSRF protection): {current_url}")
                continue

            # Fetch the page
            page_data = await self._fetch_single_page(session, current_url, max_length)
            if page_data:
                pages.append(page_data)
                logger.debug(f"Crawled page {len(pages)}/{max_pages}: {current_url}")

                # Add new links to queue (only if we need more pages)
                if len(pages) < max_pages:
                    for link in page_data.get("links", []):
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
        session = None
        try:
            # Configure session with security limits
            timeout = aiohttp.ClientTimeout(total=self.timeout)
            connector = aiohttp.TCPConnector(limit=10)

            session = aiohttp.ClientSession(
                timeout=timeout,
                connector=connector,
            )

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
        finally:
            # Ensure session is closed to prevent resource leaks
            if session and not session.closed:
                await session.close()

    async def _fetch_with_exa(
        self, url: str, max_length: int, subpages: int = 0, subpage_target: str = None
    ) -> ToolResult:
        """
        Fetch using Exa content API.
        Premium: handles JS-heavy sites, costs $0.001/page.

        Supports fetching subpages when subpages > 0.

        Improved error handling with detailed logging for auth/rate limit failures.
        """
        session = None
        try:
            # Exa requires searching first to get IDs, then fetching content
            # For direct URL fetch, we use a workaround: search for exact URL
            timeout = aiohttp.ClientTimeout(total=30)
            session = aiohttp.ClientSession(timeout=timeout)

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

                        merged_content.append(f"\n{page_content}\n")
                        total_words += len(page_content.split())
                        total_chars += len(page_content)

                    final_content = "".join(merged_content)

                    return ToolResult(
                        success=True,
                        output={
                            "url": results[0].get("url", url),
                            "title": results[0].get("title", ""),
                            "content": final_content,
                            "author": results[0].get("author"),
                            "published_date": results[0].get("publishedDate"),
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
        finally:
            # Ensure session is closed to prevent resource leaks
            if session and not session.closed:
                await session.close()
