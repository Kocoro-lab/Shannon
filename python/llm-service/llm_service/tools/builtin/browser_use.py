"""
Browser Tool for Shannon

Single unified tool for browser automation via Playwright.
Uses an `action` parameter to dispatch: navigate, click, type,
screenshot, extract, scroll, wait, close.

Sessions are tied to Shannon session_id and auto-cleanup after TTL.
"""

import base64
import logging
import os
import time
from pathlib import Path
from typing import Any, Dict, List, Optional

import aiohttp

from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult

logger = logging.getLogger(__name__)


def _persist_screenshot(session_id: str, b64_data: str, index: int = 0) -> Optional[str]:
    """Persist base64 PNG screenshot to session workspace.

    Returns relative path from session root (e.g. 'screenshots/1709312345_0.png'),
    or None on any failure. All errors are non-fatal.
    """
    if not session_id or not b64_data or len(b64_data) < 100:
        return None

    # Validate session_id (prevent path traversal)
    if ".." in session_id or session_id.startswith(".") or len(session_id) > 128:
        logger.warning("_persist_screenshot: invalid session_id")
        return None

    try:
        image_bytes = base64.b64decode(b64_data)
    except Exception:
        logger.warning("_persist_screenshot: base64 decode failed")
        return None

    base_dir = os.getenv("SHANNON_SESSION_WORKSPACES_DIR", "/tmp/shannon-sessions")
    ts = int(time.time() * 1000)
    filename = f"{ts}_{index}.png"
    rel_path = f"screenshots/{filename}"
    abs_dir = Path(base_dir) / session_id / "screenshots"

    try:
        abs_dir.mkdir(parents=True, exist_ok=True)
        abs_path = abs_dir / filename
        abs_path.write_bytes(image_bytes)
        logger.info(
            f"_persist_screenshot: saved {rel_path} ({len(image_bytes)} bytes) "
            f"for session {session_id}"
        )
        return rel_path
    except Exception as e:
        logger.warning(f"_persist_screenshot: write failed: {e}")
        return None

# Playwright service URL (internal k8s service or local)
PLAYWRIGHT_SERVICE_URL = os.getenv("PLAYWRIGHT_SERVICE_URL", "")

# Timeout for playwright service calls
PLAYWRIGHT_TIMEOUT = int(os.getenv("PLAYWRIGHT_TIMEOUT", "60"))

# Actions advertised to the LLM (safe actions only)
SAFE_ACTIONS = ("navigate", "click", "type", "screenshot", "extract", "scroll", "wait", "close")

# Actions that require special runtime permission
GATED_ACTIONS = {"evaluate"}


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
    """Extract session_id from session_context."""
    if session_context and isinstance(session_context, dict):
        session_id = session_context.get("session_id")
        if session_id:
            return session_id

    import uuid
    generated_id = f"browser-{uuid.uuid4().hex[:12]}"
    logger.warning(f"No session_id in session_context, generated: {generated_id}")
    return generated_id


# Required parameters per action (runtime validation)
_ACTION_REQUIRED_PARAMS: Dict[str, List[str]] = {
    "navigate": ["url"],
    "click": ["selector"],
    "type": ["selector", "text"],
    "screenshot": [],
    "extract": [],
    "scroll": [],
    "wait": [],
    "evaluate": ["script"],
    "close": [],
}


class BrowserTool(Tool):
    """Unified browser automation tool with action-based dispatch."""

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="browser",
            version="2.0.0",
            description=(
                "Browser automation tool. Use the 'action' parameter to specify "
                "what to do: navigate to a URL, click elements, type text, take "
                "screenshots, extract page content, scroll, or wait for elements."
            ),
            category="browser",
            requires_auth=False,
            rate_limit=30,
            timeout_seconds=60,
            cost_per_use=0.001,
            session_aware=True,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            # --- Core parameter ---
            ToolParameter(
                name="action",
                type=ToolParameterType.STRING,
                description=(
                    "Browser action to perform. One of: "
                    "navigate (go to URL), "
                    "click (click element by selector), "
                    "type (type text into input), "
                    "screenshot (capture page image), "
                    "extract (get page/element text or HTML), "
                    "scroll (scroll page or element into view), "
                    "wait (wait for element or duration), "
                    "close (end browser session)"
                ),
                required=True,
                enum=list(SAFE_ACTIONS),
            ),
            # --- navigate ---
            ToolParameter(
                name="url",
                type=ToolParameterType.STRING,
                description="URL to navigate to (required for action=navigate)",
                required=False,
            ),
            ToolParameter(
                name="wait_until",
                type=ToolParameterType.STRING,
                description="When navigation is done: 'load', 'domcontentloaded', or 'networkidle' (action=navigate)",
                required=False,
                default="domcontentloaded",
            ),
            # --- click ---
            ToolParameter(
                name="selector",
                type=ToolParameterType.STRING,
                description="CSS or XPath selector for the target element (action=click/type/extract/scroll/wait)",
                required=False,
            ),
            ToolParameter(
                name="button",
                type=ToolParameterType.STRING,
                description="Mouse button: 'left', 'right', or 'middle' (action=click)",
                required=False,
                default="left",
            ),
            ToolParameter(
                name="click_count",
                type=ToolParameterType.INTEGER,
                description="Number of clicks, 2 for double-click (action=click)",
                required=False,
                default=1,
            ),
            # --- type ---
            ToolParameter(
                name="text",
                type=ToolParameterType.STRING,
                description="Text to type into the field (required for action=type)",
                required=False,
            ),
            # --- screenshot ---
            ToolParameter(
                name="full_page",
                type=ToolParameterType.BOOLEAN,
                description="Capture full scrollable page (action=screenshot)",
                required=False,
                default=False,
            ),
            # --- extract ---
            ToolParameter(
                name="extract_type",
                type=ToolParameterType.STRING,
                description="What to extract: 'text', 'html', or 'attribute' (action=extract)",
                required=False,
                default="text",
            ),
            ToolParameter(
                name="attribute",
                type=ToolParameterType.STRING,
                description="Attribute name to extract when extract_type='attribute' (action=extract)",
                required=False,
            ),
            # --- scroll ---
            ToolParameter(
                name="x",
                type=ToolParameterType.INTEGER,
                description="Horizontal scroll pixels, positive=right (action=scroll)",
                required=False,
                default=0,
            ),
            ToolParameter(
                name="y",
                type=ToolParameterType.INTEGER,
                description="Vertical scroll pixels, positive=down (action=scroll)",
                required=False,
                default=0,
            ),
            # --- shared ---
            ToolParameter(
                name="timeout_ms",
                type=ToolParameterType.INTEGER,
                description="Timeout in milliseconds (action=navigate/click/type/wait)",
                required=False,
                default=5000,
            ),
            # --- evaluate (hidden, not in enum) ---
            ToolParameter(
                name="script",
                type=ToolParameterType.STRING,
                description="JavaScript code to execute (requires special permission)",
                required=False,
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        action = kwargs.get("action", "").lower().strip()

        # --- Validate action ---
        all_actions = set(SAFE_ACTIONS) | GATED_ACTIONS
        if action not in all_actions:
            return ToolResult(
                success=False, output=None,
                error=f"Unknown action '{action}'. Valid actions: {', '.join(SAFE_ACTIONS)}",
            )

        # --- Gate dangerous actions ---
        if action in GATED_ACTIONS:
            allow = (
                session_context
                and isinstance(session_context, dict)
                and session_context.get("allow_browser_evaluate")
            )
            if not allow:
                return ToolResult(
                    success=False, output=None,
                    error=f"Action '{action}' is disabled. To enable, add allow_browser_evaluate to session context sanitizer safe_keys in tools.py and agent.py",
                )

        # --- Validate required params for this action ---
        required = _ACTION_REQUIRED_PARAMS.get(action, [])
        missing = [p for p in required if not kwargs.get(p)]
        if missing:
            return ToolResult(
                success=False, output=None,
                error=f"Action '{action}' requires parameters: {', '.join(missing)}",
            )

        session_id = _get_session_id(session_context, kwargs)

        # --- Dispatch ---
        if action == "close":
            result = await _close_playwright_session(session_id)
            return ToolResult(
                success=result.get("success", False),
                output={"closed": result.get("success", False), "action": "close"},
                error=result.get("error") if not result.get("success") else None,
            )

        # Build action-specific kwargs for playwright
        action_kwargs: Dict[str, Any] = {}

        if action == "navigate":
            action_kwargs["url"] = kwargs["url"]
            action_kwargs["wait_until"] = kwargs.get("wait_until", "domcontentloaded")
            action_kwargs["timeout_ms"] = kwargs.get("timeout_ms", 30000)
        elif action == "click":
            action_kwargs["selector"] = kwargs["selector"]
            action_kwargs["button"] = kwargs.get("button", "left")
            action_kwargs["click_count"] = kwargs.get("click_count", 1)
            action_kwargs["timeout_ms"] = kwargs.get("timeout_ms", 5000)
        elif action == "type":
            action_kwargs["selector"] = kwargs["selector"]
            action_kwargs["text"] = kwargs["text"]
            action_kwargs["timeout_ms"] = kwargs.get("timeout_ms", 5000)
        elif action == "screenshot":
            action_kwargs["full_page"] = kwargs.get("full_page", False)
        elif action == "extract":
            if kwargs.get("selector"):
                action_kwargs["selector"] = kwargs["selector"]
            action_kwargs["extract_type"] = kwargs.get("extract_type", "text")
            if kwargs.get("attribute"):
                action_kwargs["attribute"] = kwargs["attribute"]
        elif action == "scroll":
            if kwargs.get("selector"):
                action_kwargs["selector"] = kwargs["selector"]
            action_kwargs["x"] = kwargs.get("x", 0)
            action_kwargs["y"] = kwargs.get("y", 0)
        elif action == "wait":
            if kwargs.get("selector"):
                action_kwargs["selector"] = kwargs["selector"]
            action_kwargs["timeout_ms"] = kwargs.get("timeout_ms", 5000)
        elif action == "evaluate":
            action_kwargs["script"] = kwargs["script"]

        result = await _call_playwright_action(
            session_id=session_id,
            action=action,
            **action_kwargs,
        )

        if not result.get("success"):
            return ToolResult(
                success=False, output=None,
                error=result.get("error", f"{action} failed"),
            )

        # --- Format output per action ---
        output: Dict[str, Any] = {"action": action}

        if action == "navigate":
            output["url"] = result.get("url")
            output["title"] = result.get("title")
        elif action == "click":
            output["clicked"] = True
        elif action == "type":
            output["typed"] = True
        elif action == "screenshot":
            b64_screenshot = result.get("screenshot")
            output["url"] = result.get("url")
            output["title"] = result.get("title")
            # Persist to session workspace and replace base64 with path reference
            screenshot_path = None
            if b64_screenshot and session_id:
                screenshot_path = _persist_screenshot(session_id, b64_screenshot)
            if screenshot_path:
                output["screenshot"] = f"[stored:{screenshot_path}]"
                output["screenshot_path"] = screenshot_path
            else:
                output["screenshot"] = b64_screenshot
        elif action == "extract":
            output["content"] = result.get("content")
            output["elements"] = result.get("elements")
        elif action == "scroll":
            output["scrolled"] = True
        elif action == "wait":
            output["waited"] = True
        elif action == "evaluate":
            output["result"] = result.get("result")

        output["elapsed_ms"] = result.get("elapsed_ms")

        return ToolResult(success=True, output=output)
