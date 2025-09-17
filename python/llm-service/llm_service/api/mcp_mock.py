from fastapi import APIRouter, HTTPException
from pydantic import BaseModel
from typing import Any, Dict, Optional

router = APIRouter(prefix="/mcp", tags=["mcp-mock"])


class MCPInvokeRequest(BaseModel):
    function: str
    args: Optional[Dict[str, Any]] = None


@router.post("/mock")
async def mcp_mock(req: MCPInvokeRequest) -> Dict[str, Any]:
    """Simple MCP mock endpoint used for smoke testing.

    - function: name of the function (supports only "echo")
    - args: payload object
    """
    if req.function.lower() == "echo":
        return {"ok": True, "function": req.function, "echo": req.args or {}}
    raise HTTPException(status_code=400, detail="Unknown function")
