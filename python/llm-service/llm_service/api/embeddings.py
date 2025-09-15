from fastapi import APIRouter, Request, HTTPException
from pydantic import BaseModel, Field
from typing import List, Optional

router = APIRouter()

class EmbeddingRequest(BaseModel):
    texts: List[str] = Field(..., description="Texts to embed")
    model: Optional[str] = Field(default="text-embedding-3-small", description="Embedding model to use")
    normalize: Optional[bool] = Field(default=False, description="L2 normalize output vectors")

class EmbeddingResponse(BaseModel):
    embeddings: List[List[float]]
    dimensions: int
    model_used: str

def _l2_normalize(vec: List[float]) -> List[float]:
    s = sum(v*v for v in vec) or 1.0
    norm = s ** 0.5
    return [v / norm for v in vec]

@router.post("/", response_model=EmbeddingResponse)
async def generate_embeddings(request: Request, body: EmbeddingRequest):
    """Generate embeddings for the provided texts"""
    providers = request.app.state.providers
    
    try:
        embeddings = []
        for text in body.texts:
            embedding = await providers.generate_embedding(text, body.model)
            if body.normalize:
                embedding = _l2_normalize(embedding)
            embeddings.append(embedding)
        
        return EmbeddingResponse(
            embeddings=embeddings,
            dimensions=len(embeddings[0]) if embeddings else 0,
            model_used=body.model
        )
        
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))
