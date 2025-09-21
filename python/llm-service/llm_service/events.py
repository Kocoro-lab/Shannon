import asyncio
import json
import os
import time
from typing import Optional, Dict, Any

import httpx
import contextlib


class EventEmitter:
    """Async, non-blocking event emitter posting to orchestrator /events."""

    def __init__(self,
                 ingest_url: str = "http://orchestrator:8081/events",
                 auth_token: Optional[str] = None,
                 queue_max: int = 1000,
                 timeout_seconds: float = 2.0):
        self.ingest_url = ingest_url
        self.auth_token = auth_token
        self.timeout = timeout_seconds
        self._q: asyncio.Queue = asyncio.Queue(maxsize=queue_max)
        self._task: Optional[asyncio.Task] = None
        self._client: Optional[httpx.AsyncClient] = None

    async def start(self):
        if self._client is None:
            self._client = httpx.AsyncClient(timeout=self.timeout)
        if self._task is None:
            self._task = asyncio.create_task(self._worker())

    async def close(self):
        if self._task:
            self._task.cancel()
            with contextlib.suppress(Exception):
                await self._task
            self._task = None
        if self._client:
            await self._client.aclose()
            self._client = None

    def emit(self, workflow_id: str, etype: str, agent_id: Optional[str] = None, message: str = "", timestamp: Optional[float] = None, payload: Optional[Dict[str, Any]] = None):
        if not workflow_id or not etype:
            return
        ts = timestamp if timestamp is not None else time.time()
        data = {
            "workflow_id": workflow_id,
            "type": etype,
            "agent_id": agent_id or "",
            "message": message or "",
            "timestamp": time.strftime('%Y-%m-%dT%H:%M:%S', time.gmtime(ts)),
            "payload": payload or {},
        }
        try:
            self._q.put_nowait(data)
        except asyncio.QueueFull:
            pass  # drop silently

    async def _worker(self):
        headers = {"Content-Type": "application/json"}
        if self.auth_token:
            headers["Authorization"] = f"Bearer {self.auth_token}"
        while True:
            try:
                batch = [await self._q.get()]
                # small batch drain
                for _ in range(50):
                    try:
                        batch.append(self._q.get_nowait())
                    except asyncio.QueueEmpty:
                        break
                if len(batch) == 1:
                    data = batch[0]
                else:
                    data = batch
                if self._client is None:
                    self._client = httpx.AsyncClient(timeout=self.timeout)
                await self._client.post(self.ingest_url, headers=headers, content=json.dumps(data))
            except asyncio.CancelledError:
                break
            except Exception:
                await asyncio.sleep(0.1)


def build_default_emitter_from_env() -> EventEmitter:
    url = os.getenv("EVENTS_INGEST_URL", "http://orchestrator:8081/events")
    token = os.getenv("EVENTS_AUTH_TOKEN", "") or None
    return EventEmitter(ingest_url=url, auth_token=token)
