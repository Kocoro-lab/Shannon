from __future__ import annotations

import asyncio
import os
import time
from typing import Any, Awaitable, Callable, Dict, Optional
from urllib.parse import urlparse

import httpx

from .metrics import metrics

# --- Simple per-URL circuit breaker (process-local) ---
class _SimpleBreaker:
    def __init__(self, failure_threshold: int, recovery_timeout: float) -> None:
        self.failure_threshold = max(1, failure_threshold)
        self.recovery_timeout = max(1.0, recovery_timeout)
        self.failures = 0
        self.open_until: float = 0.0
        self.half_open = False

    def allow(self, now: float) -> bool:
        if self.open_until > now:
            return False
        if self.open_until != 0.0 and self.open_until <= now:
            # move to half-open and allow one trial
            self.half_open = True
            self.open_until = 0.0
        return True

    def on_success(self) -> None:
        self.failures = 0
        self.half_open = False
        self.open_until = 0.0

    def on_failure(self, now: float) -> None:
        self.failures += 1
        if self.failures >= self.failure_threshold:
            self.open_until = now + self.recovery_timeout
            self.half_open = False


_breakers: Dict[str, _SimpleBreaker] = {}


class HttpStatelessClient:
    """
    Minimal stateless MCP HTTP client with basic hardening.

    - URL allowlist via MCP_ALLOWED_DOMAINS (comma‑separated). Defaults to localhost,127.0.0.1
    - Response size limit via MCP_MAX_RESPONSE_BYTES (default 10 MB)
    - Retries via MCP_RETRIES (default 3)
    - Timeout via MCP_TIMEOUT_SECONDS (default 10s)

    Convention: POST to `url` with JSON body {"function": <name>, "args": {...}} and expect JSON response.
    """

    def __init__(self, name: str, url: str, headers: Optional[Dict[str, str]] = None, timeout: Optional[float] = None) -> None:
        self.name = name
        self.url = url
        self.headers = headers or {}
        # Config from env with safe defaults
        self.allowed_domains = [d.strip() for d in os.getenv("MCP_ALLOWED_DOMAINS", "localhost,127.0.0.1").split(",") if d.strip()]
        self.max_response_bytes = int(os.getenv("MCP_MAX_RESPONSE_BYTES", str(10 * 1024 * 1024)))
        self.retries = max(1, int(os.getenv("MCP_RETRIES", "3")))
        self.timeout = float(os.getenv("MCP_TIMEOUT_SECONDS", str(timeout if timeout is not None else 10.0)))

        self._validate_url()
        # Circuit breaker config
        self.cb_failures = max(1, int(os.getenv("MCP_CB_FAILURES", "5")))
        self.cb_recovery = float(os.getenv("MCP_CB_RECOVERY_SECONDS", "60"))

    def _validate_url(self) -> None:
        host = urlparse(self.url).hostname or ""
        # Allow exact match or suffix match (subdomains)
        if not any(host == d or host.endswith("." + d) for d in self.allowed_domains):
            raise ValueError(f"MCP URL host '{host}' not in allowed domains: {self.allowed_domains}")

    def _client(self) -> httpx.AsyncClient:
        # httpx 0.28+ doesn't support max_response_body_size, using defaults
        return httpx.AsyncClient(timeout=self.timeout)

    async def _invoke(self, func_name: str, **kwargs: Any) -> Any:
        payload = {"function": func_name, "args": kwargs}
        status = "error"
        start = time.time()
        try:
            last_exc: Optional[Exception] = None
            async with self._client() as client:
                # Circuit breaker per URL
                br = _breakers.setdefault(self.url, _SimpleBreaker(self.cb_failures, self.cb_recovery))
                for attempt in range(1, self.retries + 1):
                    try:
                        now = time.time()
                        if not br.allow(now):
                            raise httpx.RequestError("circuit_open")
                        resp = await client.post(self.url, json=payload, headers=self.headers)
                        resp.raise_for_status()
                        br.on_success()
                        status = "success"
                        return resp.json()
                    except Exception as e:  # network or HTTP errors
                        last_exc = e
                        br.on_failure(time.time())
                        if attempt >= self.retries:
                            raise
                        # simple exponential backoff: 0.5, 1.0, 2.0 ...
                        delay = min(2.0 ** (attempt - 1) * 0.5, 5.0)
                        await asyncio.sleep(delay)
        finally:
            duration = time.time() - start
            try:
                metrics.record_mcp_request(self.name, func_name, status, duration)
            except Exception:
                pass

    async def get_callable_function(self, func_name: str) -> Callable[..., Awaitable[Any]]:
        async def _callable(**kwargs: Any) -> Any:
            return await self._invoke(func_name, **kwargs)

        return _callable
