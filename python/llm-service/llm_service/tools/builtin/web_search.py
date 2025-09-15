"""
Web Search Tool with Exa and Firecrawl support
"""

import aiohttp
import os
import json
from typing import List, Dict, Any, Optional
from urllib.parse import quote_plus
import logging
from enum import Enum

from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult

logger = logging.getLogger(__name__)


class SearchProvider(Enum):
    EXA = "exa"
    FIRECRAWL = "firecrawl"


class WebSearchProvider:
    """Base class for web search providers"""
    
    async def search(self, query: str, max_results: int = 5) -> List[Dict[str, Any]]:
        raise NotImplementedError


class ExaSearchProvider(WebSearchProvider):
    """Exa AI search provider - Latest API implementation"""
    
    def __init__(self, api_key: str):
        self.api_key = api_key
        self.base_url = "https://api.exa.ai/search"
    
    async def search(self, query: str, max_results: int = 5) -> List[Dict[str, Any]]:
        headers = {
            "x-api-key": self.api_key,
            "Content-Type": "application/json"
        }
        
        # Use Exa's latest API parameters
        payload = {
            "query": query,
            "numResults": max_results,
            "type": "auto",  # Let Exa choose between neural and keyword search
            "useAutoprompt": True,  # Enhance query for better results
            "text": True,  # Include text content in results
            "liveCrawl": "fallback",  # Use live crawl if cached results are stale
        }
        
        async with aiohttp.ClientSession() as session:
            async with session.post(self.base_url, json=payload, headers=headers, timeout=15) as response:
                if response.status != 200:
                    error_text = await response.text()
                    raise Exception(f"Exa API error (status {response.status}): {error_text}")
                
                data = await response.json()
                results = []
                
                for result in data.get("results", []):
                    results.append({
                        "title": result.get("title", ""),
                        "snippet": result.get("text", "")[:300] if result.get("text") else "",
                        "url": result.get("url", ""),
                        "source": "exa",
                        "score": result.get("score", 0),
                        "published_date": result.get("publishedDate"),
                        "author": result.get("author"),
                    })
                
                return results


class FirecrawlSearchProvider(WebSearchProvider):
    """Firecrawl search provider - V2 API with search + scrape capabilities"""
    
    def __init__(self, api_key: str):
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
                    raise Exception(f"Firecrawl API error (status {response.status}): {error_text}")
                
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


class WebSearchTool(Tool):
    """
    Web search tool with Exa and Firecrawl support
    Default provider: Exa (if API key available), fallback to Firecrawl
    """
    
    def __init__(self):
        self.provider = self._initialize_provider()
        super().__init__()
    
    def _initialize_provider(self) -> Optional[WebSearchProvider]:
        """Initialize the search provider based on environment configuration"""
        
        # Default to Exa if available
        default_provider = SearchProvider.EXA.value
        
        # Check which provider is configured via environment
        provider_name = os.getenv("WEB_SEARCH_PROVIDER", default_provider).lower()
        
        # Provider configuration
        providers_config = {
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
            if api_key:
                logger.info(f"Initializing {provider_name} search provider")
                return config["class"](api_key)
            else:
                logger.warning(f"{provider_name} API key not found in environment variable {config['api_key_env']}")
        
        # Fallback: try other providers
        for name, config in providers_config.items():
            if name != provider_name:  # Skip already tried provider
                api_key = os.getenv(config["api_key_env"])
                if api_key:
                    logger.info(f"Falling back to {name} search provider")
                    return config["class"](api_key)
        
        logger.error(
            "No web search provider configured. Please set either:\n"
            "- EXA_API_KEY for Exa search\n"
            "- FIRECRAWL_API_KEY for Firecrawl search\n"
            "And optionally set WEB_SEARCH_PROVIDER=exa or WEB_SEARCH_PROVIDER=firecrawl"
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
                    "- EXA_API_KEY environment variable for Exa search\n"
                    "- FIRECRAWL_API_KEY environment variable for Firecrawl search"
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
            
        except Exception as e:
            logger.error(f"Search failed with {self.provider.__class__.__name__}: {e}")
            return ToolResult(
                success=False,
                output=None,
                error=f"Search failed: {str(e)}"
            )