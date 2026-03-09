"""Prometheus metrics for Shannon LLM Service"""

import time
from prometheus_client import Counter, Histogram, Gauge, Info

# LLM completion metrics
LLM_REQUESTS_TOTAL = Counter(
    "llm_requests_total",
    "Total number of LLM requests",
    ["provider", "model", "tier", "cache_status"],  # cache_status: hit/miss
)

LLM_REQUEST_DURATION = Histogram(
    "llm_request_duration_seconds",
    "Time spent on LLM requests",
    ["provider", "model", "cache_status"],
    buckets=[0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 25.0, 60.0, 120.0],
)

LLM_TOKENS_TOTAL = Counter(
    "llm_tokens_total",
    "Total tokens processed",
    ["provider", "model", "type"],  # type: prompt/completion
)

LLM_COST_TOTAL = Counter(
    "llm_cost_total", "Total cost of LLM requests in USD", ["provider", "model"]
)

# Service metrics
SERVICE_INFO = Info("llm_service_info", "LLM service information")

ACTIVE_CONNECTIONS = Gauge(
    "llm_service_active_connections", "Number of active HTTP connections"
)

# Error metrics
ERROR_REQUESTS_TOTAL = Counter(
    "llm_service_errors_total",
    "Total number of errors",
    ["error_type", "component"],  # component: cache/llm/service
)

# MCP metrics
MCP_REQUESTS_TOTAL = Counter(
    "llm_mcp_requests_total",
    "Total number of MCP requests",
    ["name", "function", "status"],  # status: success/error
)

MCP_REQUEST_DURATION = Histogram(
    "llm_mcp_request_duration_seconds",
    "Time spent on MCP requests",
    ["name", "function", "status"],
    buckets=[0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0],
)

# Planner fallback metrics
PLANNER_FALLBACKS_TOTAL = Counter(
    "llm_planner_fallbacks_total",
    "Total number of planner fallbacks to heuristic/static plan",
)


class MetricsCollector:
    """Collects and manages metrics for the LLM service"""

    def __init__(
        self, service_name: str = "shannon-llm-service", version: str = "0.1.0"
    ):
        self.service_name = service_name
        self.version = version

        # Set service info
        SERVICE_INFO.info({"service_name": service_name, "version": version})

    def record_cache_request(self, operation: str, status: str, duration: float):
        """No-op: API-layer cache metrics removed (single cache strategy)."""
        return

    # API-layer cache stats removed (single cache strategy)

    def record_llm_request(
        self,
        provider: str,
        model: str,
        tier: str,
        cache_hit: bool,
        duration: float,
        prompt_tokens: int,
        completion_tokens: int,
        cost: float,
    ):
        """Record LLM request metrics"""
        cache_status = "hit" if cache_hit else "miss"

        LLM_REQUESTS_TOTAL.labels(
            provider=provider, model=model, tier=tier, cache_status=cache_status
        ).inc()

        LLM_REQUEST_DURATION.labels(
            provider=provider, model=model, cache_status=cache_status
        ).observe(duration)

        LLM_TOKENS_TOTAL.labels(provider=provider, model=model, type="prompt").inc(
            prompt_tokens
        )
        LLM_TOKENS_TOTAL.labels(provider=provider, model=model, type="completion").inc(
            completion_tokens
        )
        LLM_COST_TOTAL.labels(provider=provider, model=model).inc(cost)

    def record_error(self, error_type: str, component: str):
        """Record error metrics"""
        ERROR_REQUESTS_TOTAL.labels(error_type=error_type, component=component).inc()

    def record_planner_fallback(self):
        """Record a planner fallback occurrence"""
        PLANNER_FALLBACKS_TOTAL.inc()

    def set_active_connections(self, count: int):
        """Update active connections count"""
        ACTIVE_CONNECTIONS.set(count)

    # --- MCP ---
    def record_mcp_request(
        self, name: str, function: str, status: str, duration: float
    ):
        MCP_REQUESTS_TOTAL.labels(name=name, function=function, status=status).inc()
        MCP_REQUEST_DURATION.labels(
            name=name, function=function, status=status
        ).observe(duration)


# Global metrics instance
metrics = MetricsCollector()


class TimedOperation:
    """Context manager for timing operations"""

    def __init__(self, operation_name: str, component: str = "service"):
        self.operation_name = operation_name
        self.component = component
        self.start_time = None
        self.duration = None

    def __enter__(self):
        self.start_time = time.time()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.duration = time.time() - self.start_time

        # Record error if exception occurred
        if exc_type is not None:
            error_type = exc_type.__name__ if exc_type else "unknown"
            metrics.record_error(error_type, self.component)

        return False  # Don't suppress exceptions
