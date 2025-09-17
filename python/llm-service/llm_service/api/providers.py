from fastapi import APIRouter, Request, HTTPException
from typing import Dict, Any, List, Optional

from ..providers.base import ModelTier

router = APIRouter(prefix="/providers", tags=["providers"])


def _model_info_to_dict(mi) -> Dict[str, Any]:
    return {
        "id": mi.id,
        "name": mi.name,
        "tier": mi.tier.value if isinstance(mi.tier, ModelTier) else str(mi.tier),
        "context_window": mi.context_window,
        "cost_per_1k_prompt_tokens": mi.cost_per_1k_prompt_tokens,
        "cost_per_1k_completion_tokens": mi.cost_per_1k_completion_tokens,
        "supports_tools": mi.supports_tools,
        "supports_streaming": mi.supports_streaming,
        "available": mi.available,
    }


@router.get("/models")
async def list_provider_models(
    request: Request, tier: Optional[str] = None
) -> Dict[str, List[Dict[str, Any]]]:
    """Return live model registries per provider.

    - Aggregates per configured provider using provider.list_models().
    - Optional `tier` query filters to small|medium|large.
    """
    pm = getattr(request.app.state, "providers", None)
    if pm is None:
        raise HTTPException(status_code=503, detail="Provider manager not initialized")

    # Optional tier filter
    tier_enum: Optional[ModelTier] = None
    if tier:
        v = tier.lower()
        if v == "small":
            tier_enum = ModelTier.SMALL
        elif v == "medium":
            tier_enum = ModelTier.MEDIUM
        elif v == "large":
            tier_enum = ModelTier.LARGE

    out: Dict[str, List[Dict[str, Any]]] = {}
    for ptype, provider in pm.providers.items():
        try:
            models = provider.list_models()
            if tier_enum is not None:
                models = [m for m in models if m.tier == tier_enum]
            out[ptype.value] = [_model_info_to_dict(m) for m in models]
        except Exception as e:
            out[ptype.value] = [{"error": str(e)}]
    return out
