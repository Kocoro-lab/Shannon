"""
Browser Session Manager for Playwright Service

Manages stateful browser sessions using Playwright Browser Contexts.
Each session has isolated cookies, localStorage, and state.

Session lifecycle:
1. First action for session_id → creates new context + page
2. Subsequent actions → reuses existing context/page
3. Session TTL expires OR explicit close → cleanup
"""

import asyncio
import logging
import time
from dataclasses import dataclass, field
from typing import Dict, Optional, Tuple

from playwright.async_api import Browser, BrowserContext, Page

logger = logging.getLogger(__name__)

# Session configuration
SESSION_TTL_SECONDS = int(300)  # 5 minutes idle TTL
SESSION_CLEANUP_INTERVAL = int(60)  # Check every 60 seconds
MAX_SESSIONS = int(50)  # Maximum concurrent sessions


@dataclass
class BrowserSession:
    """Represents a single browser session with its context and page."""
    session_id: str
    context: BrowserContext
    page: Page
    created_at: float = field(default_factory=time.time)
    last_accessed: float = field(default_factory=time.time)

    def touch(self):
        """Update last accessed time."""
        self.last_accessed = time.time()

    @property
    def is_expired(self) -> bool:
        """Check if session has exceeded TTL."""
        return (time.time() - self.last_accessed) > SESSION_TTL_SECONDS


class BrowserSessionManager:
    """
    Manages browser sessions with automatic cleanup.

    Uses Playwright Browser Context for session isolation:
    - Each session gets its own context (isolated cookies, storage)
    - Contexts share the same browser process (efficient)
    - Automatic cleanup of expired sessions
    """

    def __init__(self, browser: Browser):
        self.browser = browser
        self.sessions: Dict[str, BrowserSession] = {}
        self._lock = asyncio.Lock()
        self._cleanup_task: Optional[asyncio.Task] = None

    async def start(self):
        """Start the background cleanup task."""
        self._cleanup_task = asyncio.create_task(self._cleanup_loop())
        logger.info("Session manager started with cleanup interval=%ds, TTL=%ds",
                   SESSION_CLEANUP_INTERVAL, SESSION_TTL_SECONDS)

    async def stop(self):
        """Stop cleanup task and close all sessions."""
        if self._cleanup_task:
            self._cleanup_task.cancel()
            try:
                await self._cleanup_task
            except asyncio.CancelledError:
                pass

        # Close all sessions
        async with self._lock:
            for session_id in list(self.sessions.keys()):
                await self._close_session_unlocked(session_id)

        logger.info("Session manager stopped, all sessions closed")

    async def get_or_create_session(
        self,
        session_id: str,
        viewport_width: int = 1280,
        viewport_height: int = 720,
        locale: str = "en-US",
        user_agent: Optional[str] = None,
    ) -> Tuple[Page, bool]:
        """
        Get existing session or create a new one.

        Args:
            session_id: Unique session identifier
            viewport_width: Browser viewport width
            viewport_height: Browser viewport height
            locale: Browser locale (e.g., "ja-JP", "en-US")
            user_agent: Custom user agent (optional)

        Returns:
            Tuple of (Page, is_new_session)
        """
        async with self._lock:
            # Check if session exists
            if session_id in self.sessions:
                session = self.sessions[session_id]
                session.touch()
                logger.debug("Reusing session %s", session_id)
                return session.page, False

            # Check session limit
            if len(self.sessions) >= MAX_SESSIONS:
                # Try to evict oldest expired session
                await self._evict_oldest_unlocked()
                if len(self.sessions) >= MAX_SESSIONS:
                    raise RuntimeError(f"Maximum sessions ({MAX_SESSIONS}) reached")

            # Create new session
            context = await self.browser.new_context(
                viewport={"width": viewport_width, "height": viewport_height},
                locale=locale,
                user_agent=user_agent or (
                    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
                    "AppleWebKit/537.36 (KHTML, like Gecko) "
                    "Chrome/120.0.0.0 Safari/537.36"
                ),
            )
            page = await context.new_page()

            session = BrowserSession(
                session_id=session_id,
                context=context,
                page=page,
            )
            self.sessions[session_id] = session

            logger.info("Created new session %s (total: %d)", session_id, len(self.sessions))
            return page, True

    async def get_session(self, session_id: str) -> Optional[Page]:
        """Get existing session page, or None if not found."""
        async with self._lock:
            if session_id in self.sessions:
                session = self.sessions[session_id]
                session.touch()
                return session.page
            return None

    async def close_session(self, session_id: str) -> bool:
        """Close and remove a session. Returns True if session existed."""
        async with self._lock:
            return await self._close_session_unlocked(session_id)

    async def _close_session_unlocked(self, session_id: str) -> bool:
        """Close session without lock (internal use)."""
        if session_id not in self.sessions:
            return False

        session = self.sessions.pop(session_id)
        try:
            await session.page.close()
            await session.context.close()
            logger.info("Closed session %s (remaining: %d)", session_id, len(self.sessions))
        except Exception as e:
            logger.warning("Error closing session %s: %s", session_id, e)

        return True

    async def _evict_oldest_unlocked(self):
        """Evict the oldest expired session, or oldest session if none expired."""
        if not self.sessions:
            return

        # First try to find expired sessions
        expired = [s for s in self.sessions.values() if s.is_expired]
        if expired:
            oldest = min(expired, key=lambda s: s.last_accessed)
            await self._close_session_unlocked(oldest.session_id)
            return

        # No expired sessions, evict oldest
        oldest = min(self.sessions.values(), key=lambda s: s.last_accessed)
        logger.warning("Evicting non-expired session %s due to limit", oldest.session_id)
        await self._close_session_unlocked(oldest.session_id)

    async def _cleanup_loop(self):
        """Background task to cleanup expired sessions."""
        while True:
            try:
                await asyncio.sleep(SESSION_CLEANUP_INTERVAL)
                await self._cleanup_expired()
            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.error("Cleanup error: %s", e)

    async def _cleanup_expired(self):
        """Remove all expired sessions."""
        async with self._lock:
            expired_ids = [
                session_id
                for session_id, session in self.sessions.items()
                if session.is_expired
            ]

            for session_id in expired_ids:
                await self._close_session_unlocked(session_id)

            if expired_ids:
                logger.info("Cleaned up %d expired sessions", len(expired_ids))

    def get_stats(self) -> Dict:
        """Get session manager statistics.

        Note: Takes a snapshot of sessions to avoid RuntimeError during concurrent access.
        """
        # Take a snapshot to avoid "dictionary changed size during iteration"
        sessions_snapshot = list(self.sessions.values())
        return {
            "active_sessions": len(sessions_snapshot),
            "max_sessions": MAX_SESSIONS,
            "ttl_seconds": SESSION_TTL_SECONDS,
            "sessions": [
                {
                    "session_id": s.session_id,
                    "age_seconds": int(time.time() - s.created_at),
                    "idle_seconds": int(time.time() - s.last_accessed),
                    "expired": s.is_expired,
                }
                for s in sessions_snapshot
            ]
        }
