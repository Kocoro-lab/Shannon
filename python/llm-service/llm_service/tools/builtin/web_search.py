"""
Web Search Tool supporting multiple providers: Exa, Firecrawl, Google, Serper, and Bing
"""

import aiohttp
import os
import json
import re
from typing import List, Dict, Any, Optional
from urllib.parse import quote_plus
import logging
from enum import Enum

from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult

logger = logging.getLogger(__name__)


class SearchProvider(Enum):
    EXA = "exa"
    FIRECRAWL = "firecrawl"
    GOOGLE = "google"
    SERPER = "serper"
    BING = "bing"


class WebSearchProvider:
    """Base class for web search providers"""

    @staticmethod
    def validate_api_key(api_key: str) -> bool:
        """Validate API key format and presence"""
        if not api_key or not isinstance(api_key, str):
            return False
        # Basic validation: should be at least 10 chars and not contain obvious test values
        if len(api_key.strip()) < 10:
            return False
        if api_key.lower() in ['test', 'demo', 'example', 'your_api_key_here', 'xxx']:
            return False
        # Check for reasonable API key pattern (alphanumeric with some special chars)
        if not re.match(r'^[A-Za-z0-9\-_\.]+$', api_key.strip()):
            return False
        return True

    @staticmethod
    def sanitize_error_message(error: str) -> str:
        """Sanitize error messages to prevent information disclosure"""
        # Remove URLs that might contain API keys or sensitive endpoints
        sanitized = re.sub(r'https?://[^\s]+', '[URL_REDACTED]', str(error))
        # Remove potential API keys (common patterns)
        sanitized = re.sub(r'\b[A-Za-z0-9]{32,}\b', '[KEY_REDACTED]', sanitized)
        sanitized = re.sub(r'api[_\-]?key[\s=:]+[\w\-]+', 'api_key=[REDACTED]', sanitized, flags=re.IGNORECASE)
        sanitized = re.sub(r'bearer\s+[\w\-\.]+', 'Bearer [REDACTED]', sanitized, flags=re.IGNORECASE)
        # Remove potential file paths
        sanitized = re.sub(r'/[\w/\-\.]+\.(py|json|yml|yaml|env)', '[PATH_REDACTED]', sanitized)
        # Limit length to prevent excessive logging
        if len(sanitized) > 200:
            sanitized = sanitized[:200] + '...'
        return sanitized

    async def search(self, query: str, max_results: int = 5) -> List[Dict[str, Any]]:
        raise NotImplementedError


class ExaSearchProvider(WebSearchProvider):
    """Exa AI search provider - Semantic search optimized for AI applications"""

    def __init__(self, api_key: str):
        if not self.validate_api_key(api_key):
            raise ValueError("Invalid or missing API key")
        self.api_key = api_key
        self.base_url = "https://api.exa.ai/search"
    
    async def search(self, query: str, max_results: int = 5) -> List[Dict[str, Any]]:
        headers = {
            "x-api-key": self.api_key,
            "Content-Type": "application/json"
        }
        
        # Use Exa's latest API parameters with proper text content extraction
        payload = {
            "query": query,
            "numResults": max_results,
            "type": "auto",  # Let Exa choose between neural and keyword search
            "useAutoprompt": True,  # Enhance query for better results
            "contents": {
                "text": {
                    "maxCharacters": 2000,  # Get substantial text content for each result
                    "includeHtmlTags": False,  # Clean text without HTML
                },
                "highlights": {
                    "numSentences": 3,  # Extract key highlights
                    "highlightsPerUrl": 2,
                }
            },
            "liveCrawl": "fallback",  # Use live crawl if cached results are stale
        }
        
        async with aiohttp.ClientSession() as session:
            async with session.post(self.base_url, json=payload, headers=headers, timeout=15) as response:
                if response.status != 200:
                    error_text = await response.text()
                    sanitized_error = self.sanitize_error_message(error_text)
                    logger.error(f"Exa API error: status={response.status}, details logged")
                    logger.debug(f"Exa API raw error: {sanitized_error}")
                    raise Exception(f"Search service temporarily unavailable (Error {response.status})")
                
                data = await response.json()
                results = []

                # Log the first result to see what fields are available
                if data.get("results"):
                    logger.debug(f"Exa response sample - First result keys: {list(data['results'][0].keys())}")

                for result in data.get("results", []):
                    # Extract text content and highlights
                    text_content = result.get("text", "")
                    highlights = result.get("highlights", [])

                    # Use highlights if available, otherwise use text content
                    snippet = ""
                    if highlights:
                        snippet = " ... ".join(highlights[:2])  # Join first 2 highlights
                    elif text_content:
                        snippet = text_content[:500]  # Use first 500 chars of text

                    results.append({
                        "title": result.get("title", ""),
                        "snippet": snippet,
                        "content": text_content[:2000] if text_content else "",  # Include fuller content
                        "url": result.get("url", ""),
                        "source": "exa",
                        "score": result.get("score", 0),
                        "published_date": result.get("publishedDate"),
                        "author": result.get("author"),
                        "highlights": highlights,
                    })
                
                return results


class FirecrawlSearchProvider(WebSearchProvider):
    """Firecrawl search provider - V2 API with search + scrape capabilities"""

    def __init__(self, api_key: str):
        if not self.validate_api_key(api_key):
            raise ValueError("Invalid or missing API key")
        self.api_key = api_key
        self.base_url = "https://api.firecrawl.dev/v2/search"
    
    async def search(self, query: str, max_results: int = 5) -> List[Dict[str, Any]]:
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json"
        }
        
        # Use Firecrawl's latest V2 search API
        payload = {
            "query": query,
            "limit": min(max_results, 20),  # Firecrawl alpha caps at lower limits
            "sources": ["web"],  # Search the web
            "scrapeOptions": {
                "formats": ["markdown"],  # Get markdown content
                "onlyMainContent": True,  # Skip navigation, ads, etc.
            }
        }
        
        async with aiohttp.ClientSession() as session:
            async with session.post(self.base_url, json=payload, headers=headers, timeout=20) as response:
                if response.status != 200:
                    error_text = await response.text()
                    sanitized_error = self.sanitize_error_message(error_text)
                    logger.error(f"Firecrawl API error: status={response.status}, details logged")
                    logger.debug(f"Firecrawl API raw error: {sanitized_error}")
                    raise Exception(f"Search service temporarily unavailable (Error {response.status})")
                
                data = await response.json()
                results = []
                
                # Firecrawl returns data in 'data' field
                for result in data.get("data", []):
                    # Extract content from markdown if available
                    content = ""
                    if result.get("markdown"):
                        content = result["markdown"][:300]  # First 300 chars
                    elif result.get("description"):
                        content = result["description"]
                    
                    results.append({
                        "title": result.get("title", ""),
                        "snippet": content,
                        "url": result.get("url", ""),
                        "source": "firecrawl",
                        "markdown": result.get("markdown", ""),  # Full markdown available
                    })
                
                return results


class GoogleSearchProvider(WebSearchProvider):
    """Google Custom Search JSON API provider"""

    def __init__(self, api_key: str, search_engine_id: str = None):
        if not self.validate_api_key(api_key):
            raise ValueError("Invalid or missing API key")
        self.api_key = api_key
        self.search_engine_id = search_engine_id or os.getenv("GOOGLE_SEARCH_ENGINE_ID", "")
        if not self.search_engine_id:
            raise ValueError("Google Search Engine ID is required")
        self.base_url = "https://customsearch.googleapis.com/customsearch/v1"

    async def search(self, query: str, max_results: int = 5) -> List[Dict[str, Any]]:
        params = {
            "key": self.api_key,
            "cx": self.search_engine_id,
            "q": query,
            "num": min(max_results, 10),  # Google limits to 10 per request
        }

        async with aiohttp.ClientSession() as session:
            async with session.get(self.base_url, params=params, timeout=15) as response:
                if response.status != 200:
                    error_text = await response.text()
                    sanitized_error = self.sanitize_error_message(error_text)
                    logger.error(f"Google Search API error: status={response.status}, details logged")
                    logger.debug(f"Google Search API raw error: {sanitized_error}")
                    if response.status == 403:
                        raise Exception("Search service access denied. Please check your API credentials.")
                    elif response.status == 429:
                        raise Exception("Search rate limit exceeded. Please try again later.")
                    else:
                        raise Exception(f"Search service temporarily unavailable (Error {response.status})")

                data = await response.json()
                results = []

                for item in data.get("items", []):
                    # Extract content from various fields
                    snippet = item.get("snippet", "")
                    page_map = item.get("pagemap", {})
                    metatags = page_map.get("metatags", [{}])[0]

                    # Try to get more detailed content from metatags
                    content = snippet
                    if metatags.get("og:description"):
                        content = metatags.get("og:description", "")[:500]
                    elif metatags.get("description"):
                        content = metatags.get("description", "")[:500]

                    results.append({
                        "title": item.get("title", ""),
                        "snippet": snippet,
                        "content": content,
                        "url": item.get("link", ""),
                        "source": "google",
                        "display_link": item.get("displayLink", ""),
                    })

                return results


class SerperSearchProvider(WebSearchProvider):
    """Serper API provider - Fast and affordable Google search results"""

    def __init__(self, api_key: str):
        if not self.validate_api_key(api_key):
            raise ValueError("Invalid or missing API key")
        self.api_key = api_key
        self.base_url = "https://google.serper.dev/search"

    async def search(self, query: str, max_results: int = 5) -> List[Dict[str, Any]]:
        headers = {
            "X-API-KEY": self.api_key,
            "Content-Type": "application/json"
        }

        payload = {
            "q": query,
            "num": max_results,
        }

        async with aiohttp.ClientSession() as session:
            async with session.post(self.base_url, json=payload, headers=headers, timeout=15) as response:
                if response.status != 200:
                    error_text = await response.text()
                    sanitized_error = self.sanitize_error_message(error_text)
                    logger.error(f"Serper API error: status={response.status}, details logged")
                    logger.debug(f"Serper API raw error: {sanitized_error}")
                    if response.status == 401:
                        raise Exception("Search service authentication failed. Please check your API credentials.")
                    elif response.status == 429:
                        raise Exception("Search rate limit exceeded. Please try again later.")
                    else:
                        raise Exception(f"Search service temporarily unavailable (Error {response.status})")

                data = await response.json()
                results = []

                # Process organic search results
                for result in data.get("organic", []):
                    results.append({
                        "title": result.get("title", ""),
                        "snippet": result.get("snippet", ""),
                        "content": result.get("snippet", ""),  # Serper doesn't provide full content
                        "url": result.get("link", ""),
                        "source": "serper",
                        "position": result.get("position", 0),
                        "date": result.get("date"),
                    })

                # Include knowledge graph if available
                if data.get("knowledgeGraph"):
                    kg = data["knowledgeGraph"]
                    results.insert(0, {
                        "title": kg.get("title", ""),
                        "snippet": kg.get("description", ""),
                        "content": kg.get("description", ""),
                        "url": kg.get("website", ""),
                        "source": "serper_knowledge_graph",
                        "type": kg.get("type", ""),
                    })

                return results


class BingSearchProvider(WebSearchProvider):
    """Bing Search API v7 provider (Azure Cognitive Services)"""

    def __init__(self, api_key: str):
        if not self.validate_api_key(api_key):
            raise ValueError("Invalid or missing API key")
        self.api_key = api_key
        self.base_url = "https://api.cognitive.microsoft.com/bing/v7.0/search"

    async def search(self, query: str, max_results: int = 5) -> List[Dict[str, Any]]:
        headers = {
            "Ocp-Apim-Subscription-Key": self.api_key,
        }

        params = {
            "q": query,
            "count": max_results,
            "textDecorations": True,
            "textFormat": "HTML",
        }

        async with aiohttp.ClientSession() as session:
            async with session.get(self.base_url, params=params, headers=headers, timeout=15) as response:
                if response.status != 200:
                    error_text = await response.text()
                    sanitized_error = self.sanitize_error_message(error_text)
                    logger.error(f"Bing Search API error: status={response.status}, details logged")
                    logger.debug(f"Bing Search API raw error: {sanitized_error}")
                    if response.status == 401:
                        raise Exception("Search service authentication failed. Please check your API credentials.")
                    elif response.status == 429:
                        raise Exception("Search rate limit exceeded. Please try again later.")
                    else:
                        raise Exception(f"Search service temporarily unavailable (Error {response.status})")

                data = await response.json()
                results = []

                # Process web pages results
                web_pages = data.get("webPages", {})
                for result in web_pages.get("value", []):
                    results.append({
                        "title": result.get("name", ""),
                        "snippet": result.get("snippet", ""),
                        "content": result.get("snippet", ""),  # Bing doesn't provide full content in search
                        "url": result.get("url", ""),
                        "source": "bing",
                        "display_url": result.get("displayUrl", ""),
                        "date_published": result.get("dateLastCrawled"),
                    })

                return results


class WebSearchTool(Tool):
    """
    Web search tool supporting multiple providers:
    - Google Custom Search (default)
    - Serper API
    - Bing Search API
    - Exa AI (semantic search)
    - Firecrawl (with content extraction)
    """
    
    def __init__(self):
        self.provider = self._initialize_provider()
        super().__init__()
    
    def _initialize_provider(self) -> Optional[WebSearchProvider]:
        """Initialize the search provider based on environment configuration"""
        
        # Default to Google, then Serper, then others
        default_provider = SearchProvider.GOOGLE.value
        
        # Check which provider is configured via environment
        provider_name = os.getenv("WEB_SEARCH_PROVIDER", default_provider).lower()
        
        # Provider configuration
        providers_config = {
            SearchProvider.GOOGLE.value: {
                "class": GoogleSearchProvider,
                "api_key_env": "GOOGLE_API_KEY",
                "requires_extra": "GOOGLE_SEARCH_ENGINE_ID"
            },
            SearchProvider.SERPER.value: {
                "class": SerperSearchProvider,
                "api_key_env": "SERPER_API_KEY"
            },
            SearchProvider.BING.value: {
                "class": BingSearchProvider,
                "api_key_env": "BING_API_KEY"
            },
            SearchProvider.EXA.value: {
                "class": ExaSearchProvider,
                "api_key_env": "EXA_API_KEY"
            },
            SearchProvider.FIRECRAWL.value: {
                "class": FirecrawlSearchProvider,
                "api_key_env": "FIRECRAWL_API_KEY"
            }
        }
        
        # Try configured provider first
        if provider_name in providers_config:
            config = providers_config[provider_name]
            api_key = os.getenv(config["api_key_env"])
            if api_key and WebSearchProvider.validate_api_key(api_key):
                # Special handling for Google which needs search engine ID
                if provider_name == SearchProvider.GOOGLE.value:
                    search_engine_id = os.getenv("GOOGLE_SEARCH_ENGINE_ID")
                    if not search_engine_id:
                        logger.warning("Google Search Engine ID not found. Please set GOOGLE_SEARCH_ENGINE_ID")
                    else:
                        logger.info(f"Initializing {provider_name} search provider")
                        return config["class"](api_key, search_engine_id)
                else:
                    logger.info(f"Initializing {provider_name} search provider")
                    return config["class"](api_key)
            else:
                logger.warning(f"{provider_name} API key not found in environment variable {config['api_key_env']}")
        
        # Fallback: try other providers in priority order
        priority_order = [
            SearchProvider.GOOGLE.value,
            SearchProvider.SERPER.value,
            SearchProvider.BING.value,
            SearchProvider.EXA.value,
            SearchProvider.FIRECRAWL.value
        ]

        for name in priority_order:
            if name != provider_name and name in providers_config:  # Skip already tried provider
                config = providers_config[name]
                api_key = os.getenv(config["api_key_env"])
                if api_key and WebSearchProvider.validate_api_key(api_key):
                    # Special handling for Google
                    if name == SearchProvider.GOOGLE.value:
                        search_engine_id = os.getenv("GOOGLE_SEARCH_ENGINE_ID")
                        if search_engine_id:
                            logger.info(f"Falling back to {name} search provider")
                            return config["class"](api_key, search_engine_id)
                    else:
                        logger.info(f"Falling back to {name} search provider")
                        return config["class"](api_key)
        
        logger.error(
            "No web search provider configured. Please set one of:\n"
            "- GOOGLE_API_KEY and GOOGLE_SEARCH_ENGINE_ID for Google Custom Search\n"
            "- SERPER_API_KEY for Serper search\n"
            "- BING_API_KEY for Bing search\n"
            "- EXA_API_KEY for Exa search\n"
            "- FIRECRAWL_API_KEY for Firecrawl search\n"
            "And optionally set WEB_SEARCH_PROVIDER=google|serper|bing|exa|firecrawl"
        )
        return None
    
    def _get_metadata(self) -> ToolMetadata:
        provider_name = "none"
        if self.provider:
            provider_name = self.provider.__class__.__name__.replace("SearchProvider", "")
        
        return ToolMetadata(
            name="web_search",
            version="3.0.0",
            description=f"Search the web for real-time information using {provider_name}",
            category="search",
            author="Shannon",
            requires_auth=True,
            rate_limit=30,  # 30 searches per minute
            timeout_seconds=20,
            memory_limit_mb=256,
            sandboxed=True,
            dangerous=False,
            cost_per_use=0.001,  # Approximate cost per search
        )
    
    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="query",
                type=ToolParameterType.STRING,
                description="The search query",
                required=True,
            ),
            ToolParameter(
                name="max_results",
                type=ToolParameterType.INTEGER,
                description="Maximum number of results to return",
                required=False,
                default=5,
                min_value=1,
                max_value=20,
            ),
        ]
    
    async def _execute_impl(self, **kwargs) -> ToolResult:
        """
        Execute web search using configured provider
        """
        if not self.provider:
            return ToolResult(
                success=False,
                output=None,
                error=(
                    "No web search provider configured. Please set one of:\n"
                    "- GOOGLE_API_KEY and GOOGLE_SEARCH_ENGINE_ID for Google Custom Search\n"
                    "- SERPER_API_KEY for Serper search\n"
                    "- BING_API_KEY for Bing search\n"
                    "- EXA_API_KEY for Exa search\n"
                    "- FIRECRAWL_API_KEY for Firecrawl search"
                )
            )
        
        query = kwargs["query"]
        max_results = kwargs.get("max_results", 5)
        
        try:
            logger.info(f"Executing web search with {self.provider.__class__.__name__}: {query}")
            results = await self.provider.search(query, max_results)
            
            if not results:
                return ToolResult(
                    success=True,
                    output=[],
                    metadata={
                        "query": query,
                        "provider": self.provider.__class__.__name__,
                        "result_count": 0,
                    }
                )
            
            logger.info(f"Web search returned {len(results)} results")
            return ToolResult(
                success=True,
                output=results,
                metadata={
                    "query": query,
                    "provider": self.provider.__class__.__name__,
                    "result_count": len(results),
                }
            )
            
        except ValueError as e:
            # Configuration errors - these are safe to show
            logger.error(f"Search configuration error: {e}")
            return ToolResult(
                success=False,
                output=None,
                error=f"Search configuration error: {str(e)}"
            )
        except Exception as e:
            # Runtime errors - sanitize these
            sanitized_error = WebSearchProvider.sanitize_error_message(str(e))
            logger.error(f"Search failed with {self.provider.__class__.__name__}: {sanitized_error}")

            # Return user-friendly error message
            error_message = str(e)
            if "temporarily unavailable" in error_message or "rate limit" in error_message or "authentication failed" in error_message:
                # These are already sanitized messages from our providers
                return ToolResult(
                    success=False,
                    output=None,
                    error=error_message
                )
            else:
                # Generic error for unexpected failures
                return ToolResult(
                    success=False,
                    output=None,
                    error="Search service encountered an error. Please try again later."
                )