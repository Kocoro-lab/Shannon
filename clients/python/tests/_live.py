"""Helpers for opt-in live SDK validation tests."""

from __future__ import annotations

import os

import httpx
import pytest


def _backend_available() -> bool:
    """Check whether a local Shannon gateway is available."""
    try:
        resp = httpx.get("http://localhost:8080/health", timeout=2)
        return resp.status_code == 200
    except Exception:
        return False


def live_backend_required():
    """Skip live tests unless explicitly enabled and a local backend is up."""
    return pytest.mark.skipif(
        os.environ.get("SHANNON_LIVE_TESTS") != "1" or not _backend_available(),
        reason="Live SDK tests require SHANNON_LIVE_TESTS=1 and a backend at localhost:8080",
    )
