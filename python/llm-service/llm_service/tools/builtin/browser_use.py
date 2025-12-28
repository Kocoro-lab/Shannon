"""
Browser Use Tools for Shannon

Multi-tool approach for browser automation via Playwright.
Each tool represents a specific browser action.

Tools:
- browser_navigate: Navigate to a URL
- browser_click: Click on an element
- browser_type: Type text into an input field
- browser_screenshot: Take a screenshot
- browser_extract: Extract content from page/elements
- browser_scroll: Scroll the page
- browser_close: Close a browser session

Sessions are tied to Shannon session_id and auto-cleanup after TTL.
"""

import logging
import os
from typing import Any, Dict, List, Optional

import aiohttp

from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult

logger = logging.getLogger(__name__)

# Playwright service URL (internal k8s service or local)
PLAYWRIGHT_SERVICE_URL = os.getenv("PLAYWRIGHT_SERVICE_URL", "")

# Timeout for playwright service calls
PLAYWRIGHT_TIMEOUT = int(os.getenv("PLAYWRIGHT_TIMEOUT", "60"))


async def _call_playwright_action(
    session_id: str,
    action: str,
    **kwargs
) -> Dict[str, Any]:
    """
    Call the playwright service browser action endpoint.

    Args:
        session_id: Browser session identifier
        action: Action type (navigate, click, type, etc.)
        **kwargs: Action-specific parameters

    Returns:
        Response dict from playwright service
    """
    if not PLAYWRIGHT_SERVICE_URL:
        return {"success": False, "error": "PLAYWRIGHT_SERVICE_URL not configured"}

    url = f"{PLAYWRIGHT_SERVICE_URL}/browser/action"

    payload = {
        "session_id": session_id,
        "action": action,
        **kwargs
    }

    timeout = aiohttp.ClientTimeout(total=PLAYWRIGHT_TIMEOUT)

    try:
        async with aiohttp.ClientSession(timeout=timeout) as session:
            async with session.post(url, json=payload) as response:
                if response.status != 200:
                    error_text = await response.text()
                    return {
                        "success": False,
                        "error": f"Playwright service error ({response.status}): {error_text[:500]}"
                    }
                return await response.json()
    except aiohttp.ClientError as e:
        logger.error(f"Playwright service request failed: {e}")
        return {
            "success": False,
            "error": f"Failed to connect to browser service: {str(e)}"
        }


async def _close_playwright_session(session_id: str) -> Dict[str, Any]:
    """Close a playwright browser session."""
    if not PLAYWRIGHT_SERVICE_URL:
        return {"success": False, "error": "PLAYWRIGHT_SERVICE_URL not configured"}

    url = f"{PLAYWRIGHT_SERVICE_URL}/browser/close"

    try:
        timeout = aiohttp.ClientTimeout(total=PLAYWRIGHT_TIMEOUT)
        async with aiohttp.ClientSession(timeout=timeout) as session:
            async with session.post(url, json={"session_id": session_id}) as response:
                return await response.json()
    except Exception as e:
        logger.error(f"Failed to close browser session: {e}")
        return {"success": False, "error": str(e)}


def _get_session_id(session_context: Optional[Dict], kwargs: Dict) -> str:
    """Extract session_id from session_context.

    Session context is injected by the orchestrator and contains session_id.
    If not available, generates a unique session ID to prevent cross-task collisions.
    """
    # Get session_id from session_context (injected by orchestrator)
    if session_context and isinstance(session_context, dict):
        session_id = session_context.get("session_id")
        if session_id:
            return session_id

    # Generate unique session ID to prevent cross-task collisions
    # This should rarely happen - session_context should always be provided
    import uuid
    import logging
    logger = logging.getLogger(__name__)
    generated_id = f"browser-{uuid.uuid4().hex[:12]}"
    logger.warning(f"No session_id in session_context, generated: {generated_id}")
    return generated_id


class BrowserNavigateTool(Tool):
    """Navigate to a URL in the browser."""

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="browser_navigate",
            version="1.0.0",
            description=(
                "Navigate to a URL in a browser session. "
                "Opens the page and waits for it to load. "
                "Returns the page title and final URL (after redirects)."
            ),
            category="browser",
            requires_auth=False,
            rate_limit=30,
            timeout_seconds=60,
            cost_per_use=0.001,
            session_aware=True,  # Receive session_context for session_id
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="url",
                type=ToolParameterType.STRING,
                description="URL to navigate to",
                required=True,
            ),
            ToolParameter(
                name="wait_until",
                type=ToolParameterType.STRING,
                description="When to consider navigation done: 'load', 'domcontentloaded', or 'networkidle'",
                required=False,
                default="domcontentloaded",
            ),
            ToolParameter(
                name="timeout_ms",
                type=ToolParameterType.INTEGER,
                description="Navigation timeout in milliseconds",
                required=False,
                default=30000,
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        session_id = _get_session_id(session_context, kwargs)

        result = await _call_playwright_action(
            session_id=session_id,
            action="navigate",
            url=kwargs["url"],
            wait_until=kwargs.get("wait_until", "domcontentloaded"),
            timeout_ms=kwargs.get("timeout_ms", 30000),
        )

        if not result.get("success"):
            return ToolResult(
                success=False,
                output=None,
                error=result.get("error", "Navigation failed"),
            )

        return ToolResult(
            success=True,
            output={
                "url": result.get("url"),
                "title": result.get("title"),
                "elapsed_ms": result.get("elapsed_ms"),
            },
        )


class BrowserClickTool(Tool):
    """Click on an element in the browser."""

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="browser_click",
            version="1.0.0",
            description=(
                "Click on an element matching a CSS or XPath selector. "
                "Waits for the element to be visible before clicking."
            ),
            category="browser",
            requires_auth=False,
            rate_limit=60,
            timeout_seconds=30,
            cost_per_use=0.0005,
            session_aware=True,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="selector",
                type=ToolParameterType.STRING,
                description="CSS or XPath selector for the element to click",
                required=True,
            ),
            ToolParameter(
                name="button",
                type=ToolParameterType.STRING,
                description="Mouse button: 'left', 'right', or 'middle'",
                required=False,
                default="left",
            ),
            ToolParameter(
                name="click_count",
                type=ToolParameterType.INTEGER,
                description="Number of clicks (2 for double-click)",
                required=False,
                default=1,
            ),
            ToolParameter(
                name="timeout_ms",
                type=ToolParameterType.INTEGER,
                description="Timeout in milliseconds",
                required=False,
                default=5000,
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        session_id = _get_session_id(session_context, kwargs)

        result = await _call_playwright_action(
            session_id=session_id,
            action="click",
            selector=kwargs["selector"],
            button=kwargs.get("button", "left"),
            click_count=kwargs.get("click_count", 1),
            timeout_ms=kwargs.get("timeout_ms", 5000),
        )

        if not result.get("success"):
            return ToolResult(
                success=False,
                output=None,
                error=result.get("error", "Click failed"),
            )

        return ToolResult(
            success=True,
            output={"clicked": True, "elapsed_ms": result.get("elapsed_ms")},
        )


class BrowserTypeTool(Tool):
    """Type text into an input field."""

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="browser_type",
            version="1.0.0",
            description=(
                "Type text into an input field matching a selector. "
                "Clears the field first, then types the new text."
            ),
            category="browser",
            requires_auth=False,
            rate_limit=60,
            timeout_seconds=30,
            cost_per_use=0.0005,
            session_aware=True,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="selector",
                type=ToolParameterType.STRING,
                description="CSS or XPath selector for the input field",
                required=True,
            ),
            ToolParameter(
                name="text",
                type=ToolParameterType.STRING,
                description="Text to type into the field",
                required=True,
            ),
            ToolParameter(
                name="timeout_ms",
                type=ToolParameterType.INTEGER,
                description="Timeout in milliseconds",
                required=False,
                default=5000,
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        session_id = _get_session_id(session_context, kwargs)

        result = await _call_playwright_action(
            session_id=session_id,
            action="type",
            selector=kwargs["selector"],
            text=kwargs["text"],
            timeout_ms=kwargs.get("timeout_ms", 5000),
        )

        if not result.get("success"):
            return ToolResult(
                success=False,
                output=None,
                error=result.get("error", "Type failed"),
            )

        return ToolResult(
            success=True,
            output={"typed": True, "elapsed_ms": result.get("elapsed_ms")},
        )


class BrowserScreenshotTool(Tool):
    """Take a screenshot of the current page."""

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="browser_screenshot",
            version="1.0.0",
            description=(
                "Take a screenshot of the current browser page. "
                "Returns base64-encoded PNG image along with page title and URL."
            ),
            category="browser",
            requires_auth=False,
            rate_limit=20,
            timeout_seconds=30,
            cost_per_use=0.002,
            session_aware=True,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="full_page",
                type=ToolParameterType.BOOLEAN,
                description="Capture full scrollable page (true) or just viewport (false)",
                required=False,
                default=False,
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        session_id = _get_session_id(session_context, kwargs)

        result = await _call_playwright_action(
            session_id=session_id,
            action="screenshot",
            full_page=kwargs.get("full_page", False),
        )

        if not result.get("success"):
            return ToolResult(
                success=False,
                output=None,
                error=result.get("error", "Screenshot failed"),
            )

        return ToolResult(
            success=True,
            output={
                "screenshot": result.get("screenshot"),  # base64
                "url": result.get("url"),
                "title": result.get("title"),
                "elapsed_ms": result.get("elapsed_ms"),
            },
        )


class BrowserExtractTool(Tool):
    """Extract content from page or specific elements."""

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="browser_extract",
            version="1.0.0",
            description=(
                "Extract text content or HTML from the page or specific elements. "
                "Can extract text, HTML, or specific attributes from matched elements."
            ),
            category="browser",
            requires_auth=False,
            rate_limit=30,
            timeout_seconds=30,
            cost_per_use=0.001,
            session_aware=True,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="selector",
                type=ToolParameterType.STRING,
                description="CSS selector for elements to extract (omit for whole page)",
                required=False,
            ),
            ToolParameter(
                name="extract_type",
                type=ToolParameterType.STRING,
                description="What to extract: 'text', 'html', or 'attribute'",
                required=False,
                default="text",
            ),
            ToolParameter(
                name="attribute",
                type=ToolParameterType.STRING,
                description="Attribute name to extract (only used with extract_type='attribute')",
                required=False,
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        session_id = _get_session_id(session_context, kwargs)

        result = await _call_playwright_action(
            session_id=session_id,
            action="extract",
            selector=kwargs.get("selector"),
            extract_type=kwargs.get("extract_type", "text"),
            attribute=kwargs.get("attribute"),
        )

        if not result.get("success"):
            return ToolResult(
                success=False,
                output=None,
                error=result.get("error", "Extract failed"),
            )

        return ToolResult(
            success=True,
            output={
                "content": result.get("content"),
                "elements": result.get("elements"),
                "elapsed_ms": result.get("elapsed_ms"),
            },
        )


class BrowserScrollTool(Tool):
    """Scroll the page or scroll an element into view."""

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="browser_scroll",
            version="1.0.0",
            description=(
                "Scroll the page by a specified amount or scroll an element into view. "
                "Use selector to scroll element into view, or x/y for manual scroll."
            ),
            category="browser",
            requires_auth=False,
            rate_limit=60,
            timeout_seconds=10,
            cost_per_use=0.0002,
            session_aware=True,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="selector",
                type=ToolParameterType.STRING,
                description="CSS selector for element to scroll into view",
                required=False,
            ),
            ToolParameter(
                name="x",
                type=ToolParameterType.INTEGER,
                description="Horizontal scroll amount in pixels (positive = right)",
                required=False,
                default=0,
            ),
            ToolParameter(
                name="y",
                type=ToolParameterType.INTEGER,
                description="Vertical scroll amount in pixels (positive = down)",
                required=False,
                default=0,
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        session_id = _get_session_id(session_context, kwargs)

        result = await _call_playwright_action(
            session_id=session_id,
            action="scroll",
            selector=kwargs.get("selector"),
            x=kwargs.get("x", 0),
            y=kwargs.get("y", 0),
        )

        if not result.get("success"):
            return ToolResult(
                success=False,
                output=None,
                error=result.get("error", "Scroll failed"),
            )

        return ToolResult(
            success=True,
            output={"scrolled": True, "elapsed_ms": result.get("elapsed_ms")},
        )


class BrowserWaitTool(Tool):
    """Wait for an element or a specified duration."""

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="browser_wait",
            version="1.0.0",
            description=(
                "Wait for an element to appear or for a specified duration. "
                "Use selector to wait for element, or just timeout_ms for delay."
            ),
            category="browser",
            requires_auth=False,
            rate_limit=60,
            timeout_seconds=30,
            cost_per_use=0.0001,
            session_aware=True,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="selector",
                type=ToolParameterType.STRING,
                description="CSS selector for element to wait for",
                required=False,
            ),
            ToolParameter(
                name="timeout_ms",
                type=ToolParameterType.INTEGER,
                description="Maximum wait time in milliseconds",
                required=False,
                default=5000,
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        session_id = _get_session_id(session_context, kwargs)

        result = await _call_playwright_action(
            session_id=session_id,
            action="wait",
            selector=kwargs.get("selector"),
            timeout_ms=kwargs.get("timeout_ms", 5000),
        )

        if not result.get("success"):
            return ToolResult(
                success=False,
                output=None,
                error=result.get("error", "Wait failed"),
            )

        return ToolResult(
            success=True,
            output={"waited": True, "elapsed_ms": result.get("elapsed_ms")},
        )


class BrowserEvaluateTool(Tool):
    """Execute JavaScript in the browser context."""

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="browser_evaluate",
            version="1.0.0",
            description=(
                "Execute JavaScript code in the browser page context. "
                "Returns the result of the script evaluation. "
                "Use for advanced interactions or data extraction."
            ),
            category="browser",
            requires_auth=False,
            rate_limit=30,
            timeout_seconds=30,
            cost_per_use=0.001,
            dangerous=True,  # Mark as dangerous due to script execution
            session_aware=True,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="script",
                type=ToolParameterType.STRING,
                description="JavaScript code to execute (should be an expression or IIFE)",
                required=True,
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        session_id = _get_session_id(session_context, kwargs)

        result = await _call_playwright_action(
            session_id=session_id,
            action="evaluate",
            script=kwargs["script"],
        )

        if not result.get("success"):
            return ToolResult(
                success=False,
                output=None,
                error=result.get("error", "Evaluate failed"),
            )

        return ToolResult(
            success=True,
            output={
                "result": result.get("result"),
                "elapsed_ms": result.get("elapsed_ms"),
            },
        )


class BrowserCloseTool(Tool):
    """Close the browser session."""

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="browser_close",
            version="1.0.0",
            description=(
                "Close the browser session and free resources. "
                "Call this when done with browser automation to release memory."
            ),
            category="browser",
            requires_auth=False,
            rate_limit=60,
            timeout_seconds=10,
            cost_per_use=0.0,
            session_aware=True,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return []

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        session_id = _get_session_id(session_context, kwargs)

        result = await _close_playwright_session(session_id)

        return ToolResult(
            success=result.get("success", False),
            output={"closed": result.get("success", False)},
            error=result.get("error") if not result.get("success") else None,
        )
