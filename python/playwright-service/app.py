"""
Playwright Screenshot Service

A FastAPI service for capturing screenshots with popup dismissal.
Extended with browser session management for general browser automation.
"""

import asyncio
import base64
import ipaddress
import logging
import os
import socket
from contextlib import asynccontextmanager
from enum import Enum
from typing import Any, Dict, List, Optional
from urllib.parse import urlparse

from fastapi import FastAPI, HTTPException
from playwright.async_api import async_playwright, Browser, TimeoutError as PlaywrightTimeout
from pydantic import BaseModel, HttpUrl, Field

from session_manager import BrowserSessionManager

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Configuration (read from environment variables set by Helm)
MAX_CONCURRENT_BROWSERS = int(os.getenv("MAX_CONCURRENT_BROWSERS", "2"))
REQUEST_TIMEOUT_MS = int(os.getenv("REQUEST_TIMEOUT_MS", "30000"))
VIEWPORT_WIDTH = 1280
VIEWPORT_HEIGHT = 800

# Concurrency control
_semaphore: asyncio.Semaphore = None
_browser: Browser = None
_playwright = None
_session_manager: BrowserSessionManager = None


# Popup dismissal selectors (click-based)
POPUP_SELECTORS = [
    # Cookie consent buttons
    "#onetrust-accept-btn-handler",
    ".cc-btn.cc-allow",
    "[class*='cookie'] button[class*='accept']",
    "button[id*='cookie'][id*='accept']",

    # Japanese patterns
    "button:has-text('同意')",
    "button:has-text('同意する')",
    "button:has-text('閉じる')",
    "[aria-label='閉じる']",
    "button:has-text('OK')",
    "button:has-text('はい')",

    # Generic close buttons
    "[aria-label='Close']",
    "[aria-label='close']",
    ".modal-close",
    "button.close",
    "[class*='modal'] [class*='close']",
    "[class*='popup'] [class*='close']",
    "[class*='overlay'] button[class*='close']",

    # Newsletter/promo popups
    "[class*='newsletter'] button[class*='close']",
    "[class*='promo'] button[class*='close']",
]

# JavaScript to detect if popup/modal is visible
POPUP_DETECTION_JS = """
() => {
    const selectors = [
        '[class*="popup"]',
        '[class*="modal"]',
        '[class*="overlay"]',
        '[class*="cookie"]',
        '[class*="consent"]',
        '[aria-modal="true"]',
        '[role="dialog"]',
    ];

    for (const sel of selectors) {
        const elements = document.querySelectorAll(sel);
        for (const el of elements) {
            if (el.tagName === 'BODY' || el.tagName === 'HTML') continue;

            const style = getComputedStyle(el);
            const rect = el.getBoundingClientRect();

            // Check if element is visible and covers significant area
            const isVisible = style.display !== 'none' &&
                              style.visibility !== 'hidden' &&
                              style.opacity !== '0' &&
                              rect.width > 0 && rect.height > 0;

            const coversViewport = rect.width > window.innerWidth * 0.3 ||
                                   rect.height > window.innerHeight * 0.3;

            const isFixed = style.position === 'fixed' || style.position === 'absolute';
            const hasHighZ = parseInt(style.zIndex) > 100 || style.zIndex === 'auto';

            if (isVisible && (isFixed || hasHighZ) && coversViewport) {
                return true;
            }
        }
    }
    return false;
}
"""

# Readability-like content extraction JavaScript
# Simplified version of Mozilla's Readability algorithm
READABILITY_JS = """
() => {
    // Helper to get text density (text length / element length)
    const getTextDensity = (el) => {
        const text = el.textContent || '';
        const html = el.innerHTML || '';
        return html.length > 0 ? text.length / html.length : 0;
    };

    // Helper to score content nodes
    const scoreNode = (el) => {
        let score = 0;
        const tagName = el.tagName.toLowerCase();
        
        // Boost article-like tags
        if (['article', 'main', 'section'].includes(tagName)) score += 5;
        if (['div', 'span'].includes(tagName)) score += 1;
        if (['p', 'pre', 'blockquote'].includes(tagName)) score += 3;
        
        // Boost by class/id hints
        const classId = (el.className + ' ' + el.id).toLowerCase();
        if (/article|body|content|entry|main|post|text|blog/.test(classId)) score += 25;
        if (/comment|meta|nav|sidebar|footer|header|ad/.test(classId)) score -= 25;
        
        // Boost by text density
        const density = getTextDensity(el);
        if (density > 0.3) score += 10;
        
        // Boost by paragraph count
        const paragraphs = el.querySelectorAll('p');
        score += Math.min(paragraphs.length * 3, 30);
        
        return score;
    };

    // Find the best content container
    let bestElement = document.body;
    let bestScore = -1;

    const candidates = document.querySelectorAll('article, main, [role="main"], .content, .post, .article, .entry, #content, #main');
    if (candidates.length > 0) {
        candidates.forEach(el => {
            const score = scoreNode(el);
            if (score > bestScore) {
                bestScore = score;
                bestElement = el;
            }
        });
    } else {
        // Fall back to scoring all divs
        document.querySelectorAll('div, section').forEach(el => {
            if (el.textContent.length > 200) {
                const score = scoreNode(el);
                if (score > bestScore) {
                    bestScore = score;
                    bestElement = el;
                }
            }
        });
    }

    // Extract article metadata
    const getMetaContent = (name) => {
        const meta = document.querySelector(`meta[name="${name}"], meta[property="${name}"]`);
        return meta ? meta.content : null;
    };

    // Get title
    let title = document.querySelector('h1')?.textContent?.trim();
    if (!title) title = getMetaContent('og:title') || document.title;

    // Get author
    const author = getMetaContent('author') || 
                   document.querySelector('[rel="author"], .author, .byline')?.textContent?.trim();

    // Get excerpt/description
    const excerpt = getMetaContent('og:description') || getMetaContent('description');

    // Get site name
    const siteName = getMetaContent('og:site_name') || window.location.hostname;

    // Clean the content
    const clone = bestElement.cloneNode(true);
    
    // Remove unwanted elements
    clone.querySelectorAll('script, style, nav, footer, header, aside, .ad, .advertisement, .social, .share, .comments, form, iframe').forEach(el => el.remove());

    // Get clean text content
    const textContent = clone.textContent
        .replace(/\\s+/g, ' ')
        .replace(/\\n\\s*\\n/g, '\\n\\n')
        .trim();

    // Get HTML content
    const htmlContent = clone.innerHTML;

    return {
        title: title,
        content: htmlContent,
        text_content: textContent,
        author: author,
        excerpt: excerpt,
        site_name: siteName,
        word_count: textContent.split(/\\s+/).length,
        url: window.location.href,
    };
}
"""

# JavaScript to remove popup elements from DOM
POPUP_REMOVAL_JS = """
() => {
    const selectors = [
        '[class*="popup"]',
        '[class*="modal"]',
        '[class*="overlay"]',
        '[class*="cookie"]',
        '[class*="consent"]',
        '[aria-modal="true"]',
        '[role="dialog"]',
    ];

    let removed = 0;
    selectors.forEach(sel => {
        document.querySelectorAll(sel).forEach(el => {
            // Skip if it's the main content
            if (el.tagName === 'BODY' || el.tagName === 'HTML') return;
            // Check if element covers significant viewport
            const rect = el.getBoundingClientRect();
            const coversViewport = rect.width > window.innerWidth * 0.5 ||
                                   rect.height > window.innerHeight * 0.5;
            const isFixed = getComputedStyle(el).position === 'fixed';
            const hasHighZ = parseInt(getComputedStyle(el).zIndex) > 100;

            if ((isFixed || hasHighZ) && coversViewport) {
                el.remove();
                removed++;
            }
        });
    });

    // Reset body overflow (popups often set overflow:hidden)
    document.body.style.overflow = 'auto';
    document.documentElement.style.overflow = 'auto';

    return removed;
}
"""


class CaptureRequest(BaseModel):
    url: HttpUrl
    full_page: bool = True
    wait_ms: int = 3000


class CaptureResponse(BaseModel):
    success: bool
    screenshot: Optional[str] = None  # base64 encoded (clean page)
    popup_screenshot: Optional[str] = None  # base64 encoded (before dismissal, only if popup detected)
    popup_detected: bool = False
    title: Optional[str] = None
    error: Optional[str] = None
    popups_dismissed: int = 0


def is_private_ip(hostname: str) -> bool:
    """Check if hostname resolves to a private IP (SSRF protection)."""
    try:
        # Resolve hostname to IP
        ip_str = socket.gethostbyname(hostname)
        ip = ipaddress.ip_address(ip_str)

        # Block private, loopback, link-local, and reserved ranges
        if ip.is_private or ip.is_loopback or ip.is_link_local or ip.is_reserved:
            return True

        # Also block common internal ranges explicitly
        private_ranges = [
            ipaddress.ip_network("10.0.0.0/8"),
            ipaddress.ip_network("172.16.0.0/12"),
            ipaddress.ip_network("192.168.0.0/16"),
            ipaddress.ip_network("127.0.0.0/8"),
            ipaddress.ip_network("169.254.0.0/16"),
        ]
        for network in private_ranges:
            if ip in network:
                return True

        return False
    except socket.gaierror:
        # Can't resolve hostname - reject for safety
        return True


def validate_url(url: str) -> None:
    """Validate URL for SSRF protection."""
    parsed = urlparse(url)

    # Only allow http/https
    if parsed.scheme not in ("http", "https"):
        raise HTTPException(status_code=400, detail="Only HTTP/HTTPS URLs allowed")

    # Check hostname
    hostname = parsed.hostname
    if not hostname:
        raise HTTPException(status_code=400, detail="Invalid URL: no hostname")

    # Block private IPs
    if is_private_ip(hostname):
        raise HTTPException(status_code=400, detail="Access to internal URLs is not allowed")


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Manage browser lifecycle."""
    global _semaphore, _browser, _playwright, _session_manager

    _semaphore = asyncio.Semaphore(MAX_CONCURRENT_BROWSERS)
    _playwright = await async_playwright().start()
    _browser = await _playwright.chromium.launch(
        headless=True,
        args=[
            "--no-sandbox",
            "--disable-setuid-sandbox",
            "--disable-dev-shm-usage",
            "--disable-gpu",
        ]
    )
    logger.info("Browser started")

    # Initialize session manager for browser automation
    _session_manager = BrowserSessionManager(_browser)
    await _session_manager.start()
    logger.info("Session manager started")

    yield

    # Cleanup
    await _session_manager.stop()
    await _browser.close()
    await _playwright.stop()
    logger.info("Browser stopped")


app = FastAPI(
    title="Playwright Screenshot Service",
    version="1.0.0",
    lifespan=lifespan
)


@app.get("/health")
async def health():
    """Health check endpoint."""
    return {"status": "ok", "browser_connected": _browser is not None and _browser.is_connected()}


@app.post("/capture", response_model=CaptureResponse)
async def capture(request: CaptureRequest):
    """Capture screenshot with popup dismissal."""
    url = str(request.url)

    # SSRF protection
    validate_url(url)

    # Check if service is initialized (lifespan startup completed)
    if _semaphore is None or _browser is None:
        raise HTTPException(status_code=503, detail="Service not ready - please retry")

    # Acquire semaphore for concurrency control
    async with _semaphore:
        context = None
        page = None
        try:
            # Create new context for isolation
            context = await _browser.new_context(
                viewport={"width": VIEWPORT_WIDTH, "height": VIEWPORT_HEIGHT},
                user_agent="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
            )
            page = await context.new_page()

            # Navigate with timeout (use domcontentloaded to avoid waiting for all resources)
            await page.goto(url, timeout=REQUEST_TIMEOUT_MS, wait_until="domcontentloaded")

            # Wait for page to settle
            await page.wait_for_timeout(request.wait_ms)

            # Detect if popup is present
            popup_detected = False
            popup_screenshot_b64 = None
            try:
                popup_detected = await page.evaluate(POPUP_DETECTION_JS)
            except Exception as e:
                logger.warning(f"Popup detection failed: {e}")

            # If popup detected, capture it first before dismissal
            if popup_detected:
                logger.info(f"Popup detected on {url}, capturing before dismissal")
                popup_bytes = await page.screenshot(full_page=False)  # Viewport only for popup
                popup_screenshot_b64 = base64.b64encode(popup_bytes).decode("utf-8")

            # Try to dismiss popups by clicking close buttons
            popups_dismissed = 0
            for selector in POPUP_SELECTORS:
                try:
                    element = page.locator(selector).first
                    if await element.is_visible(timeout=500):
                        await element.click(timeout=1000)
                        popups_dismissed += 1
                        await page.wait_for_timeout(500)  # Wait for animation
                except Exception:
                    continue  # Selector not found or not clickable

            # DOM-based popup removal as fallback
            try:
                removed = await page.evaluate(POPUP_REMOVAL_JS)
                popups_dismissed += removed
            except Exception as e:
                logger.warning(f"DOM removal failed: {e}")

            # Final wait for any animations
            if popups_dismissed > 0:
                await page.wait_for_timeout(500)

            # Capture clean screenshot
            screenshot_bytes = await page.screenshot(full_page=request.full_page)
            screenshot_b64 = base64.b64encode(screenshot_bytes).decode("utf-8")

            # Get page title
            title = await page.title()

            return CaptureResponse(
                success=True,
                screenshot=screenshot_b64,
                popup_screenshot=popup_screenshot_b64,
                popup_detected=popup_detected,
                title=title,
                popups_dismissed=popups_dismissed
            )

        except PlaywrightTimeout:
            return CaptureResponse(
                success=False,
                error=f"Timeout loading page (>{REQUEST_TIMEOUT_MS}ms)"
            )
        except Exception as e:
            logger.exception(f"Capture failed for {url}")
            return CaptureResponse(
                success=False,
                error=str(e)
            )
        finally:
            if page:
                await page.close()
            if context:
                await context.close()


# =============================================================================
# Browser Session API - Stateful browser automation
# =============================================================================

class BrowserActionType(str, Enum):
    """Supported browser actions."""
    NAVIGATE = "navigate"
    CLICK = "click"
    TYPE = "type"
    SCREENSHOT = "screenshot"
    SCROLL = "scroll"
    WAIT = "wait"
    EXTRACT = "extract"
    EVALUATE = "evaluate"
    # Enhanced research capabilities
    READABILITY = "readability"  # Extract clean article content
    MULTI_TAB_OPEN = "multi_tab_open"  # Open multiple URLs in tabs
    MULTI_TAB_EXTRACT = "multi_tab_extract"  # Extract from all open tabs


class BrowserActionRequest(BaseModel):
    """Request for a browser action."""
    session_id: str = Field(..., description="Unique session identifier")
    action: BrowserActionType = Field(..., description="Action to perform")

    # Navigation
    url: Optional[str] = Field(None, description="URL to navigate to (for navigate action)")
    wait_until: Optional[str] = Field("domcontentloaded", description="Wait condition: load, domcontentloaded, networkidle")

    # Element interaction
    selector: Optional[str] = Field(None, description="CSS/XPath selector for element")
    text: Optional[str] = Field(None, description="Text to type (for type action)")

    # Click options
    button: Optional[str] = Field("left", description="Mouse button: left, right, middle")
    click_count: Optional[int] = Field(1, description="Number of clicks")

    # Scroll
    x: Optional[int] = Field(None, description="Horizontal scroll amount or X coordinate")
    y: Optional[int] = Field(None, description="Vertical scroll amount or Y coordinate")

    # Wait
    timeout_ms: Optional[int] = Field(5000, description="Timeout in milliseconds")

    # Screenshot
    full_page: Optional[bool] = Field(False, description="Capture full page screenshot")

    # Extract
    extract_type: Optional[str] = Field("text", description="What to extract: text, html, attribute")
    attribute: Optional[str] = Field(None, description="Attribute name to extract")

    # Evaluate
    script: Optional[str] = Field(None, description="JavaScript to evaluate")

    # Session options (used when creating new session)
    viewport_width: Optional[int] = Field(1280, description="Viewport width")
    viewport_height: Optional[int] = Field(720, description="Viewport height")
    locale: Optional[str] = Field("en-US", description="Browser locale")

    # Multi-tab research
    urls: Optional[List[str]] = Field(None, description="URLs for multi-tab operations")
    max_tabs: Optional[int] = Field(5, description="Maximum concurrent tabs for multi-tab operations")


class ElementInfo(BaseModel):
    """Information about a page element."""
    tag: str
    text: Optional[str] = None
    attributes: Dict[str, str] = Field(default_factory=dict)
    visible: bool = True
    bounding_box: Optional[Dict[str, float]] = None


class BrowserActionResponse(BaseModel):
    """Response from a browser action."""
    success: bool
    session_id: str
    action: str
    error: Optional[str] = None

    # Navigation result
    url: Optional[str] = None
    title: Optional[str] = None

    # Screenshot result
    screenshot: Optional[str] = None  # base64 encoded

    # Extract result
    content: Optional[str] = None
    elements: Optional[List[ElementInfo]] = None

    # Evaluate result
    result: Optional[Any] = None

    # Readability/article extraction result
    article: Optional[Dict[str, Any]] = None  # {title, content, text_content, author, excerpt, site_name}

    # Multi-tab results
    tab_results: Optional[List[Dict[str, Any]]] = None

    # Metadata
    elapsed_ms: Optional[int] = None


class SessionCloseRequest(BaseModel):
    """Request to close a browser session."""
    session_id: str


class SessionStatsResponse(BaseModel):
    """Response with session statistics."""
    active_sessions: int
    max_sessions: int
    ttl_seconds: int
    sessions: List[Dict[str, Any]]


@app.post("/browser/action", response_model=BrowserActionResponse)
async def browser_action(request: BrowserActionRequest):
    """
    Execute a browser action within a session.

    Sessions are created automatically on first action and persist until:
    - Explicit close via /browser/close
    - TTL expiration (5 minutes idle)
    - Server restart
    """
    import time
    start_time = time.time()

    if _session_manager is None:
        raise HTTPException(status_code=503, detail="Service not ready")

    try:
        # Get or create session
        page, _ = await _session_manager.get_or_create_session(
            session_id=request.session_id,
            viewport_width=request.viewport_width,
            viewport_height=request.viewport_height,
            locale=request.locale,
        )

        result = BrowserActionResponse(
            success=True,
            session_id=request.session_id,
            action=request.action.value,
        )

        # Execute action
        if request.action == BrowserActionType.NAVIGATE:
            if not request.url:
                raise HTTPException(status_code=400, detail="URL required for navigate action")

            # SSRF protection
            validate_url(request.url)

            await page.goto(
                request.url,
                timeout=request.timeout_ms,
                wait_until=request.wait_until,
            )
            result.url = page.url
            result.title = await page.title()

        elif request.action == BrowserActionType.CLICK:
            if not request.selector:
                raise HTTPException(status_code=400, detail="Selector required for click action")

            await page.click(
                request.selector,
                button=request.button,
                click_count=request.click_count,
                timeout=request.timeout_ms,
            )

        elif request.action == BrowserActionType.TYPE:
            if not request.selector:
                raise HTTPException(status_code=400, detail="Selector required for type action")
            if request.text is None:
                raise HTTPException(status_code=400, detail="Text required for type action")

            await page.fill(request.selector, request.text, timeout=request.timeout_ms)

        elif request.action == BrowserActionType.SCREENSHOT:
            screenshot_bytes = await page.screenshot(full_page=request.full_page)
            result.screenshot = base64.b64encode(screenshot_bytes).decode("utf-8")
            result.url = page.url
            result.title = await page.title()

        elif request.action == BrowserActionType.SCROLL:
            if request.selector:
                # Scroll element into view
                await page.locator(request.selector).scroll_into_view_if_needed(
                    timeout=request.timeout_ms
                )
            else:
                # Scroll by amount
                await page.evaluate(f"window.scrollBy({request.x or 0}, {request.y or 0})")

        elif request.action == BrowserActionType.WAIT:
            if request.selector:
                await page.wait_for_selector(request.selector, timeout=request.timeout_ms)
            else:
                await page.wait_for_timeout(request.timeout_ms)

        elif request.action == BrowserActionType.EXTRACT:
            if not request.selector:
                # Extract from whole page
                if request.extract_type == "text":
                    result.content = await page.inner_text("body")
                elif request.extract_type == "html":
                    result.content = await page.content()
            else:
                elements = page.locator(request.selector)
                count = await elements.count()

                extracted_elements = []
                for i in range(min(count, 50)):  # Limit to 50 elements
                    el = elements.nth(i)
                    try:
                        elem_info = ElementInfo(
                            tag=await el.evaluate("el => el.tagName.toLowerCase()"),
                            text=await el.inner_text() if request.extract_type == "text" else None,
                            visible=await el.is_visible(),
                        )
                        if request.extract_type == "attribute" and request.attribute:
                            attr_val = await el.get_attribute(request.attribute)
                            elem_info.attributes[request.attribute] = attr_val or ""

                        # Get bounding box
                        try:
                            box = await el.bounding_box()
                            if box:
                                elem_info.bounding_box = box
                        except Exception:
                            pass

                        extracted_elements.append(elem_info)
                    except Exception:
                        continue

                result.elements = extracted_elements
                if extracted_elements and request.extract_type == "text":
                    result.content = "\n".join(e.text for e in extracted_elements if e.text)

        elif request.action == BrowserActionType.EVALUATE:
            if not request.script:
                raise HTTPException(status_code=400, detail="Script required for evaluate action")

            eval_result = await page.evaluate(request.script)
            result.result = eval_result

        elif request.action == BrowserActionType.READABILITY:
            # Extract clean article content using Readability-like algorithm
            if request.url:
                # Navigate first if URL provided
                validate_url(request.url)
                await page.goto(request.url, timeout=request.timeout_ms, wait_until=request.wait_until)
                await page.wait_for_timeout(1000)  # Wait for dynamic content

            # Dismiss popups first
            for selector in POPUP_SELECTORS[:5]:  # Try first 5 selectors
                try:
                    element = page.locator(selector).first
                    if await element.is_visible(timeout=300):
                        await element.click(timeout=500)
                        await page.wait_for_timeout(300)
                except Exception:
                    continue

            # Extract article using Readability algorithm
            article_data = await page.evaluate(READABILITY_JS)
            result.article = article_data
            result.content = article_data.get("text_content", "")
            result.title = article_data.get("title", "")
            result.url = page.url

        elif request.action == BrowserActionType.MULTI_TAB_OPEN:
            # Open multiple URLs in parallel tabs for research
            if not request.urls:
                raise HTTPException(status_code=400, detail="URLs required for multi_tab_open action")

            # Validate all URLs first
            for url in request.urls:
                validate_url(url)

            # Get the context from the page
            context = page.context

            # Limit the number of tabs
            urls_to_open = request.urls[:request.max_tabs]

            # Open tabs concurrently
            async def open_tab(url: str):
                try:
                    new_page = await context.new_page()
                    await new_page.goto(url, timeout=request.timeout_ms, wait_until=request.wait_until)
                    title = await new_page.title()
                    return {"url": url, "title": title, "success": True}
                except Exception as e:
                    return {"url": url, "error": str(e), "success": False}

            tab_results = await asyncio.gather(*[open_tab(url) for url in urls_to_open])
            result.tab_results = tab_results
            result.result = {"tabs_opened": len([r for r in tab_results if r.get("success")])}

        elif request.action == BrowserActionType.MULTI_TAB_EXTRACT:
            # Extract content from all open tabs in the session
            context = page.context
            pages = context.pages

            async def extract_from_page(p):
                try:
                    url = p.url
                    if url == "about:blank":
                        return None

                    # Use Readability extraction
                    article_data = await p.evaluate(READABILITY_JS)
                    return {
                        "url": url,
                        "title": article_data.get("title", ""),
                        "content": article_data.get("text_content", ""),
                        "word_count": article_data.get("word_count", 0),
                        "success": True,
                    }
                except Exception as e:
                    return {"url": p.url, "error": str(e), "success": False}

            tab_results = await asyncio.gather(*[extract_from_page(p) for p in pages])
            # Filter out None results (blank pages)
            tab_results = [r for r in tab_results if r is not None]
            result.tab_results = tab_results

            # Combine all content
            all_content = []
            for r in tab_results:
                if r.get("success") and r.get("content"):
                    all_content.append(f"## {r.get('title', 'Untitled')}\n\nSource: {r.get('url')}\n\n{r.get('content')}")

            result.content = "\n\n---\n\n".join(all_content)
            result.result = {"tabs_extracted": len(tab_results)}

        result.elapsed_ms = int((time.time() - start_time) * 1000)
        return result

    except PlaywrightTimeout as e:
        return BrowserActionResponse(
            success=False,
            session_id=request.session_id,
            action=request.action.value,
            error=f"Timeout: {str(e)}",
            elapsed_ms=int((time.time() - start_time) * 1000),
        )
    except HTTPException:
        raise
    except Exception as e:
        logger.exception(f"Browser action failed: {request.action}")
        return BrowserActionResponse(
            success=False,
            session_id=request.session_id,
            action=request.action.value,
            error=str(e),
            elapsed_ms=int((time.time() - start_time) * 1000),
        )


@app.post("/browser/close")
async def browser_close(request: SessionCloseRequest):
    """Close a browser session and free resources."""
    if _session_manager is None:
        raise HTTPException(status_code=503, detail="Service not ready")

    closed = await _session_manager.close_session(request.session_id)
    return {"success": closed, "session_id": request.session_id}


@app.get("/browser/sessions", response_model=SessionStatsResponse)
async def browser_sessions():
    """Get statistics about active browser sessions."""
    if _session_manager is None:
        raise HTTPException(status_code=503, detail="Service not ready")

    return _session_manager.get_stats()


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8002)
