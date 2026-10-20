"""Anonymous Parallel Search MCP adapter for the built-in web_search tool."""

import asyncio
import json
import os
import uuid
from datetime import timedelta
from typing import Any
from urllib.parse import urlparse

import httpx
from mcp import ClientSession
from mcp.client.streamable_http import streamablehttp_client

MCP_URL = "https://search.parallel.ai/mcp"
USER_AGENT = "Shannon (+https://github.com/Kocoro-lab/Shannon)"


def _http_client(headers=None, timeout=None, auth=None):
    # Keep the fixed destination inside the operator's domain permission check.
    # Redirects must not silently move requests to a different MCP host.
    return httpx.AsyncClient(
        headers=headers, timeout=timeout, auth=auth, follow_redirects=False
    )


class ParallelSearchClient:
    """Use standard Streamable HTTP MCP, without ambient Parallel credentials.

    Connections are request scoped. Conversation identity comes from Shannon's
    session context, independently of connections and the cached tool instance.
    """

    def __init__(self, timeout: float = 20):
        self.timeout = float(os.getenv("MCP_TIMEOUT_SECONDS", str(timeout)))
        # A private random namespace keeps common host session names isolated
        # between independent runtime owners without storing every past session.
        self._conversation_namespace = uuid.uuid4()

    async def search(
        self,
        query: str,
        max_results: int,
        session_context: dict[str, Any] | None = None,
    ) -> list[dict[str, Any]]:
        allowed = os.getenv("MCP_ALLOWED_DOMAINS")
        if allowed is not None:
            domains = [
                domain.strip().lower()
                for domain in allowed.split(",")
                if domain.strip()
            ]
            host = urlparse(MCP_URL).hostname
            if "*" not in domains and not any(
                host == domain or host.endswith("." + domain) for domain in domains
            ):
                raise ValueError(
                    "Parallel MCP host is not allowed by MCP_ALLOWED_DOMAINS"
                )
        context = session_context or {}
        conversation = context.get("session_id")
        # Hash only the runtime conversation identifier, never user_id or query.
        # This also keeps arbitrary/long host session identifiers off the wire.
        session_id = (
            str(uuid.uuid5(self._conversation_namespace, conversation))
            if isinstance(conversation, str) and conversation
            else str(uuid.uuid4())
        )
        arguments = {
            "objective": query,
            "search_queries": [query],
            "session_id": session_id,
        }
        try:
            async with asyncio.timeout(self.timeout):
                async with streamablehttp_client(
                    MCP_URL,
                    headers={"User-Agent": USER_AGENT},
                    timeout=timedelta(seconds=self.timeout),
                    sse_read_timeout=timedelta(seconds=self.timeout),
                    httpx_client_factory=_http_client,
                ) as (read, write, _), ClientSession(read, write) as session:
                    await session.initialize()
                    available = await session.list_tools()
                    if not any(t.name == "web_search" for t in available.tools):
                        raise ValueError("Parallel MCP did not advertise web_search")
                    response = await session.call_tool("web_search", arguments)
                    if response.isError:
                        raise ValueError("Parallel MCP search failed")
                    return self._results(response, max_results)
        except TimeoutError as exc:
            raise RuntimeError(
                "Search service temporarily unavailable (timeout)"
            ) from exc
        except Exception as exc:
            # SDK task groups can wrap transport failures. Do not expose remote
            # bodies or credentials, and never switch providers after an error.
            raise RuntimeError(
                "Search service temporarily unavailable (Parallel MCP)"
            ) from exc

    @staticmethod
    def _results(response: Any, max_results: int) -> list[dict[str, Any]]:
        data = response.structuredContent
        if data is None:
            text = "\n".join(
                block.text for block in response.content if block.type == "text"
            )
            data = json.loads(text)
        if not isinstance(data, dict) or not isinstance(data.get("results"), list):
            raise TypeError("Invalid Parallel MCP search response")
        results = []
        for item in data["results"]:
            if not isinstance(item, dict):
                raise TypeError("Invalid Parallel MCP search result")
            url = item.get("url", "")
            parsed = urlparse(url)
            if parsed.scheme not in ("http", "https") or not parsed.hostname:
                continue
            excerpts = item.get("excerpts", [])
            if not isinstance(excerpts, list) or not all(
                isinstance(excerpt, str) for excerpt in excerpts
            ):
                raise ValueError("Invalid Parallel MCP excerpts")
            content = "\n\n".join(excerpts)
            results.append(
                {
                    "title": item.get("title") or url,
                    "url": url,
                    "snippet": content[:500],
                    "content": content,
                    "source": "parallel",
                }
            )
            if len(results) >= max_results:
                break
        return results
