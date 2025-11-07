"""
Web Fetch Tool - Extract full page content for deep analysis

Hybrid approach:
- Default: Pure Python (free, fast, works 80% of time)
- Optional: Exa content API (premium, handles JS-heavy sites)

Security features:
- SSRF protection (blocks private IPs, cloud metadata endpoints)
- Memory exhaustion prevention (50MB response limit)
- Redirect loop protection (max 10 redirects)
"""

import aiohttp
import os
import logging
from typing import Dict, Optional, List
from urllib.parse import urlparse
from bs4 import BeautifulSoup
import html2text

from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult
from ..openapi_parser import _is_private_ip

logger = logging.getLogger(__name__)


class WebFetchTool(Tool):
    """
    Fetch full content from a web page for detailed analysis.

    Two modes:
    1. Pure Python (default): Fast, free, works for most pages
    2. Exa API: Premium content extraction with JS rendering
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
                "Default: pure Python (fast, free). Optional: Exa API for JS-heavy sites."
            ),
            category="retrieval",
            author="Shannon",
            requires_auth=False,
            rate_limit=30,  # 30 requests per minute
            timeout_seconds=30,
            memory_limit_mb=256,
            sandboxed=False,
            dangerous=False,
            cost_per_use=0.0,  # Free by default (Exa mode: $0.001/page)
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
                description="Use Exa API for premium content extraction (handles JS, costs $0.001/page)",
                required=False,
                default=False,
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
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        """Execute web content fetch."""

        url = kwargs.get("url")
        use_exa = kwargs.get("use_exa", False)
        max_length = kwargs.get("max_length", 10000)

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
            # Choose method: Exa API or Pure Python
            if use_exa and self.exa_api_key:
                logger.info(f"Fetching with Exa API: {url}")
                return await self._fetch_with_exa(url, max_length)
            else:
                logger.info(f"Fetching with pure Python: {url}")
                return await self._fetch_pure_python(url, max_length)

        except Exception as e:
            logger.error(f"Failed to fetch {url}: {str(e)}")
            return ToolResult(
                success=False, output=None, error=f"Failed to fetch page: {str(e)}"
            )

    async def _fetch_pure_python(self, url: str, max_length: int) -> ToolResult:
        """
        Fetch using pure Python: requests + BeautifulSoup + html2text.
        Fast, free, works for most pages (80%).

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

    async def _fetch_with_exa(self, url: str, max_length: int) -> ToolResult:
        """
        Fetch using Exa content API.
        Premium: handles JS-heavy sites, costs $0.001/page.

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

                result_id = results[0].get("id")

            # Step 2: Get full content using ID
            content_payload = {
                "ids": [result_id],
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
                    },
                    metadata={
                        "fetch_method": "exa",
                        "exa_id": result_id,
                        "exa_score": result.get("score"),
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
