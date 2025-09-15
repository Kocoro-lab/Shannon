from fastapi import APIRouter, Request
from datetime import datetime

router = APIRouter()

@router.get("/")
async def health_check(request: Request):
    """Basic health check endpoint"""
    return {
        "status": "healthy",
        "service": "shannon-llm-service",
        "timestamp": datetime.utcnow().isoformat()
    }

@router.get("/ready")
async def readiness_check(request: Request):
    """Readiness check - verifies all dependencies are accessible"""
    cache = request.app.state.cache
    providers = request.app.state.providers
    
    checks = {
        "cache": cache.enabled,
        "providers": len(providers.providers) > 0,
        "models": len(providers.model_registry) > 0
    }

    # In debug/dev mode, allow readiness without providers/models
    debug_mode = getattr(request.app.state.settings, "debug", False)
    all_ready = all(checks.values()) or debug_mode

    return {
        "ready": all_ready,
        "checks": checks,
        "debug_mode": debug_mode,
        "timestamp": datetime.utcnow().isoformat()
    }

@router.get("/live")
async def liveness_check():
    """Liveness check - simple ping"""
    return {"alive": True}
