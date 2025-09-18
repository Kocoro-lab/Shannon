import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from prometheus_client import make_asgi_app
import uvicorn

from llm_service.api import (
    health,
    completions,
    embeddings,
    complexity,
    agent,
    tools,
    evaluate,
    context as context_api,
    providers as providers_api,
)
from llm_service.api import mcp_mock
from llm_service.cache import CacheManager
from llm_service.config import Settings
from llm_service.providers import ProviderManager

# OpenTelemetry (minimal) instrumentation
import os
from opentelemetry import trace
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.resources import SERVICE_NAME
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.instrumentation.httpx import HTTPXClientInstrumentor

# Configure logging
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger(__name__)

# Global instances
settings = Settings()
cache_manager = None
provider_manager = None


def setup_tracing(app: FastAPI):
    """Initialize OTLP exporter and instrument FastAPI + httpx."""
    try:
        service_name = os.getenv("OTEL_SERVICE_NAME", "shannon-llm-service")
        endpoint = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")

        provider = TracerProvider(
            resource=Resource.create({SERVICE_NAME: service_name})
        )
        exporter = OTLPSpanExporter(endpoint=endpoint, insecure=True)
        provider.add_span_processor(BatchSpanProcessor(exporter))
        trace.set_tracer_provider(provider)

        FastAPIInstrumentor.instrument_app(app)
        HTTPXClientInstrumentor().instrument()
        logger.info(
            "OpenTelemetry tracing initialized",
            extra={"service": service_name, "endpoint": endpoint},
        )
    except Exception as e:
        logger.warning(f"Failed to initialize OpenTelemetry: {e}")


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Manage application lifecycle"""
    global cache_manager, provider_manager

    logger.info("Starting Shannon LLM Service")

    # Initialize cache
    cache_manager = CacheManager(settings)
    await cache_manager.initialize()

    # Initialize LLM providers
    provider_manager = ProviderManager(settings)
    await provider_manager.initialize()

    # Store in app state
    app.state.cache = cache_manager
    app.state.providers = provider_manager
    app.state.settings = settings

    yield

    # Cleanup
    logger.info("Shutting down Shannon LLM Service")
    await cache_manager.close()
    await provider_manager.close()


# Create FastAPI app
app = FastAPI(
    title="Shannon LLM Service",
    description="LLM integration service for Shannon platform",
    version="0.1.0",
    lifespan=lifespan,
)

# Initialize tracing after app creation
setup_tracing(app)

# Add CORS middleware
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Include routers
app.include_router(health.router, prefix="/health", tags=["health"])
app.include_router(completions.router, prefix="/completions", tags=["completions"])
app.include_router(embeddings.router, prefix="/embeddings", tags=["embeddings"])
app.include_router(complexity.router, prefix="/complexity", tags=["complexity"])
app.include_router(agent.router, tags=["agent"])
app.include_router(tools.router, tags=["tools"])
app.include_router(evaluate.router, tags=["evaluate"])
app.include_router(context_api.router, tags=["context"])
app.include_router(providers_api.router, tags=["providers"])
app.include_router(mcp_mock.router, tags=["mcp-mock"])

# Mount Prometheus metrics
metrics_app = make_asgi_app()
app.mount("/metrics", metrics_app)


@app.get("/")
async def root():
    """Root endpoint"""
    return {"service": "Shannon LLM Service", "version": "0.1.0", "status": "running"}


if __name__ == "__main__":
    uvicorn.run(
        "main:app", host="0.0.0.0", port=8000, reload=settings.debug, log_level="info"
    )
