import asyncio
import pytest

from llm_provider.base import RateLimiter


def test_rate_limiter_throttles_and_advances(monkeypatch):
    # Simulate time progression without real sleeps
    current = [0.0]  # mutable container

    def fake_time():
        return current[0]

    async def fake_sleep(dt):
        # fast-forward time instead of real sleep
        current[0] += dt

    # Patch time and asyncio.sleep used inside RateLimiter
    import llm_provider.base as base
    monkeypatch.setattr(base.time, "time", fake_time)
    monkeypatch.setattr(base.asyncio, "sleep", fake_sleep)

    async def _run():
        rl = RateLimiter(requests_per_minute=3)

        # First 3 acquire calls should pass without wait (t=0)
        await rl.acquire()
        await rl.acquire()
        await rl.acquire()

        # 4th should force a wait ≈ 60s because window is full
        start = fake_time()
        await rl.acquire()
        waited = fake_time() - start
        assert waited >= 59.9

        # Now window has advanced; next two should not need to wait further
        t_before = fake_time()
        await rl.acquire()
        await rl.acquire()
        assert fake_time() == t_before  # no extra waiting
    asyncio.run(_run())
