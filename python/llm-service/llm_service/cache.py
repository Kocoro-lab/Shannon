import json
import hashlib
from typing import Optional
import redis.asyncio as redis
from tenacity import retry, stop_after_attempt, wait_exponential
import logging
from .metrics import metrics, TimedOperation

logger = logging.getLogger(__name__)


class CacheManager:
    """Manages caching for LLM responses"""

    def __init__(self, settings):
        self.settings = settings
        self.redis_client = None
        self.enabled = settings.enable_cache

    async def initialize(self):
        """Initialize Redis connection"""
        if not self.enabled:
            logger.info("Cache disabled")
            return

        try:
            self.redis_client = redis.from_url(
                self.settings.redis_url, encoding="utf-8", decode_responses=True
            )
            await self.redis_client.ping()
            logger.info("Redis cache initialized")

            # Update initial cache stats
            await self._update_cache_stats()
        except Exception as e:
            logger.error(f"Failed to connect to Redis: {e}")
            metrics.record_error("ConnectionError", "cache")
            self.enabled = False

    async def close(self):
        """Close Redis connection"""
        if self.redis_client:
            await self.redis_client.close()

    def _generate_key(self, messages: list, model: str, **kwargs) -> str:
        """Generate cache key from request parameters"""
        cache_data = {"messages": messages, "model": model, **kwargs}
        cache_str = json.dumps(cache_data, sort_keys=True)
        return f"llm:cache:{hashlib.sha256(cache_str.encode()).hexdigest()}"

    @retry(
        stop=stop_after_attempt(3), wait=wait_exponential(multiplier=1, min=1, max=10)
    )
    async def get(self, messages: list, model: str, **kwargs) -> Optional[dict]:
        """Get cached response with metrics tracking"""
        if not self.enabled or not self.redis_client:
            return None

        status = "miss"
        result = None
        with TimedOperation("cache_get", "cache") as timer:
            try:
                key = self._generate_key(messages, model, **kwargs)
                cached = await self.redis_client.get(key)
                if cached:
                    logger.debug(f"Cache hit for key: {key}")
                    status = "hit"
                    result = json.loads(cached)
                else:
                    status = "miss"
            except Exception as e:
                logger.error(f"Cache get error: {e}")
                status = "error"
        # Record duration after exiting context (timer.duration is set)
        metrics.record_cache_request("get", status, timer.duration or 0.0)
        return result

    @retry(
        stop=stop_after_attempt(3), wait=wait_exponential(multiplier=1, min=1, max=10)
    )
    async def set(self, messages: list, model: str, response: dict, **kwargs):
        """Cache response with metrics tracking"""
        if not self.enabled or not self.redis_client:
            return

        status = "success"
        with TimedOperation("cache_set", "cache") as timer:
            try:
                key = self._generate_key(messages, model, **kwargs)
                await self.redis_client.setex(
                    key, self.settings.redis_ttl_seconds, json.dumps(response)
                )
                logger.debug(f"Cached response for key: {key}")
                status = "success"
            except Exception as e:
                logger.error(f"Cache set error: {e}")
                status = "error"
        metrics.record_cache_request("set", status, timer.duration or 0.0)

    async def invalidate_pattern(self, pattern: str):
        """Invalidate cache entries matching pattern with metrics tracking"""
        if not self.enabled or not self.redis_client:
            return

        status = "success"
        with TimedOperation("cache_invalidate", "cache") as timer:
            try:
                deleted_count = 0
                async for key in self.redis_client.scan_iter(
                    match=f"llm:cache:{pattern}*"
                ):
                    await self.redis_client.delete(key)
                    deleted_count += 1
                logger.info(
                    f"Invalidated {deleted_count} cache entries for pattern: {pattern}"
                )
                status = "success"
            except Exception as e:
                logger.error(f"Cache invalidation error: {e}")
                status = "error"
        metrics.record_cache_request("invalidate", status, timer.duration or 0.0)

    async def get_stats(self) -> dict:
        """Get cache statistics"""
        if not self.enabled or not self.redis_client:
            return {"enabled": False}

        try:
            info = await self.redis_client.info("stats")
            return {
                "enabled": True,
                "hits": info.get("keyspace_hits", 0),
                "misses": info.get("keyspace_misses", 0),
                "hit_rate": info.get("keyspace_hits", 0)
                / max(info.get("keyspace_hits", 0) + info.get("keyspace_misses", 0), 1),
            }
        except Exception as e:
            logger.error(f"Failed to get cache stats: {e}")
            return {"enabled": True, "error": str(e)}

    async def _update_cache_stats(self):
        """Update cache statistics metrics"""
        if not self.enabled or not self.redis_client:
            return

        try:
            # Get Redis stats
            info = await self.redis_client.info("stats")

            # Calculate metrics
            hits = info.get("keyspace_hits", 0)
            misses = info.get("keyspace_misses", 0)
            total_requests = hits + misses
            hit_rate = hits / max(total_requests, 1)

            # Count keys matching our pattern
            keys_count = 0
            async for _ in self.redis_client.scan_iter(match="llm:cache:*"):
                keys_count += 1

            # Record metrics
            metrics.record_cache_stats(keys_count, hit_rate)

        except Exception as e:
            logger.error(f"Failed to update cache stats: {e}")
