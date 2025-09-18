from fastapi import APIRouter, Request
from pydantic import BaseModel, Field
from typing import List, Optional, Dict, Any
import json
import re

from ..providers.base import ModelTier

router = APIRouter()


class ComplexityRequest(BaseModel):
    query: str = Field(..., description="Query to analyze")
    context: Optional[Dict[str, Any]] = Field(
        default_factory=dict, description="Additional context"
    )
    available_tools: Optional[List[str]] = Field(
        default_factory=list, description="Available tools"
    )


class ComplexityResponse(BaseModel):
    recommended_mode: str
    complexity_score: float
    required_capabilities: List[str]
    estimated_agents: int
    estimated_tokens: int
    estimated_cost_usd: float
    reasoning: str
    source: Optional[str] = Field(
        default="heuristic", description="'model' when provider used, else 'heuristic'"
    )
    provider: Optional[str] = Field(
        default=None, description="Provider used when source=model"
    )
    raw_output: Optional[str] = Field(
        default=None, description="Raw model output (debug only)"
    )


def _heuristic_analysis(body: ComplexityRequest) -> ComplexityResponse:
    query_lower = body.query.lower()
    query_length = len(body.query)

    # Check for calculation/tool-based tasks first - these need standard mode for tool execution
    if any(
        word in query_lower
        for word in ["calculate", "compute", "multiply", "divide", "add", "subtract"]
    ):
        mode = "standard"
        score = 0.4
        agents = 1
        tokens = 200
        cost = 0.002
        capabilities = ["calculation", "tool_use"]
        reasoning = "Calculation task requiring tool execution"
    elif (
        any(word in query_lower for word in ["simple", "what is", "define", "list"])
        or query_length < 50
    ):
        mode = "simple"
        score = 0.2
        agents = 0
        tokens = 100
        cost = 0.001
        capabilities = ["basic_qa"]
        reasoning = "Simple query that can be answered directly"
    elif any(
        word in query_lower for word in ["analyze", "compare", "explain", "describe"]
    ):
        mode = "standard"
        score = 0.5
        agents = 1
        tokens = 500
        cost = 0.005
        capabilities = ["analysis", "reasoning"]
        reasoning = "Standard analysis task requiring single agent"
    elif (
        any(word in query_lower for word in ["implement", "design", "create", "build"])
        or query_length > 200
    ):
        mode = "complex"
        score = 0.8
        agents = 3
        tokens = 2000
        cost = 0.02
        capabilities = ["planning", "execution", "validation"]
        reasoning = "Complex task requiring multiple specialized agents"
    else:
        mode = "standard"
        score = 0.4
        agents = 1
        tokens = 300
        cost = 0.003
        capabilities = ["general"]
        reasoning = "Standard task with moderate complexity"

    if body.available_tools and len(body.available_tools) > 5:
        score = min(score + 0.2, 1.0)
        agents = max(agents, 2)
        if "tool_use" not in capabilities:
            capabilities.append("tool_use")
        reasoning += f"; Multiple tools available ({len(body.available_tools)} tools)"

    return ComplexityResponse(
        recommended_mode=mode,
        complexity_score=score,
        required_capabilities=capabilities,
        estimated_agents=agents,
        estimated_tokens=tokens,
        estimated_cost_usd=cost,
        reasoning=reasoning,
        source="heuristic",
    )


def _extract_json_block(text: str) -> Optional[Dict[str, Any]]:
    try:
        return json.loads(text)
    except Exception:
        pass
    # Try fenced JSON or first {...} block
    fence = re.search(r"```(?:json)?\s*(\{[\s\S]*?\})\s*```", text, re.IGNORECASE)
    blob = fence.group(1) if fence else None
    if not blob:
        brace = re.search(r"\{[\s\S]*\}", text)
        blob = brace.group(0) if brace else None
    if not blob:
        return None
    try:
        return json.loads(blob)
    except Exception:
        return None


@router.post("/analyze", response_model=ComplexityResponse)
async def analyze_complexity(
    request: Request, body: ComplexityRequest, debug: Optional[bool] = False
):
    """Analyze the complexity of a query. If providers are configured, ask a model; otherwise, use heuristics."""
    providers = getattr(request.app.state, "providers", None)

    # If no providers configured, use heuristics
    if not providers or not providers.is_configured():
        return _heuristic_analysis(body)

    # Build a tight classification prompt
    sys = (
        "You classify tasks into simple, standard, or complex. "
        "IMPORTANT: Tasks requiring calculations or tool usage must be 'standard' mode (not 'simple'). "
        "Simple mode is ONLY for direct Q&A without tools. "
        'Respond with compact JSON only: {"recommended_mode": one of [simple, standard, complex], '
        '"complexity_score": 0..1, "required_capabilities": [strings], "estimated_agents": int, '
        '"estimated_tokens": int, "estimated_cost_usd": number, "reasoning": string}.'
    )
    context_summary = f"tools={len(body.available_tools or [])}; ctx_keys={list((body.context or {}).keys())[:5]}"
    user = f"Query: {body.query}\nContext: {context_summary}"

    try:
        settings = getattr(request.app.state, "settings", None)
        result = await providers.generate_completion(
            messages=[
                {"role": "system", "content": sys},
                {"role": "user", "content": user},
            ],
            tier=ModelTier.SMALL,
            max_tokens=250,
            temperature=0.0,
            response_format={"type": "json_object"},
            specific_model=(
                settings.complexity_model_id
                if settings and settings.complexity_model_id
                else None
            ),
        )
        raw = result.get("completion", "")
        data = _extract_json_block(raw)
        if not data:
            # fallback to heuristic if model response isn't parseable
            heur = _heuristic_analysis(body)
            if debug:
                heur.raw_output = raw
            return heur

        mode = str(data.get("recommended_mode", "standard")).lower()
        if mode not in ("simple", "standard", "complex"):
            mode = "standard"

        resp = ComplexityResponse(
            recommended_mode=mode,
            complexity_score=float(data.get("complexity_score", 0.5)),
            required_capabilities=list(data.get("required_capabilities", [])),
            estimated_agents=int(data.get("estimated_agents", 1)),
            estimated_tokens=int(data.get("estimated_tokens", 300)),
            estimated_cost_usd=float(data.get("estimated_cost_usd", 0.003)),
            reasoning=str(data.get("reasoning", "Model-derived classification")),
            source="model",
            provider=result.get("provider"),
        )
        if debug:
            resp.raw_output = raw
        return resp
    except Exception:
        # Any model error -> heuristic fallback to keep endpoint reliable
        heur = _heuristic_analysis(body)
        if debug:
            heur.raw_output = "<error calling provider>"
        return heur


# Task Analysis API for FSM modernization
class TaskAnalysisRequest(BaseModel):
    task: str = Field(..., description="Task to analyze")
    context: Optional[Dict[str, Any]] = Field(
        default_factory=dict, description="Additional context"
    )


class TaskAnalysisResponse(BaseModel):
    task_type: str = Field(
        ...,
        description="Type of task: Query, Analysis, Generation, Transformation, Execution, Unknown",
    )
    complexity_score: float = Field(
        ..., ge=0.0, le=1.0, description="Complexity score between 0 and 1"
    )
    key_entities: List[str] = Field(
        default_factory=list, description="Key entities extracted from the task"
    )
    required_capabilities: List[str] = Field(
        default_factory=list, description="Required capabilities for the task"
    )
    constraints: List[str] = Field(default_factory=list, description="Task constraints")
    success_criteria: List[str] = Field(
        default_factory=list, description="Success criteria for the task"
    )
    reasoning: str = Field(
        default_factory=str, description="Reasoning for the analysis"
    )
    source: str = Field(
        default="heuristic", description="'model' when provider used, else 'heuristic'"
    )


def _heuristic_task_analysis(task: str) -> TaskAnalysisResponse:
    """Heuristic task analysis that mirrors the Rust FSM logic"""
    lower_task = task.lower()

    # Classify task type
    if any(
        word in lower_task for word in ["what", "who", "when", "where", "why", "how"]
    ):
        task_type = "Query"
    elif any(
        word in lower_task for word in ["analyze", "compare", "evaluate", "assess"]
    ):
        task_type = "Analysis"
    elif any(
        word in lower_task for word in ["create", "write", "generate", "build", "make"]
    ):
        task_type = "Generation"
    elif any(
        word in lower_task for word in ["convert", "transform", "translate", "change"]
    ):
        task_type = "Transformation"
    elif any(word in lower_task for word in ["run", "execute", "perform", "do"]):
        task_type = "Execution"
    else:
        task_type = "Unknown"

    # Calculate complexity score (matching Rust logic)
    complexity = 0.0
    complexity += min(len(task) / 500.0, 0.3)  # Length factor
    word_count = len(task.split())
    complexity += min(word_count / 50.0, 0.2)  # Word count factor
    special_chars = sum(1 for c in task if not c.isalnum() and not c.isspace())
    complexity += min(special_chars / 20.0, 0.2)  # Special characters
    questions = task.count("?")
    complexity += min(questions / 3.0, 0.3)  # Question marks
    complexity = min(complexity, 1.0)

    # Extract entities (simple version)
    entities = []
    # Extract quoted strings
    import re

    quoted = re.findall(r'"([^"]+)"', task)
    entities.extend(quoted)
    # Extract capitalized words (potential proper nouns)
    words = task.split()
    for word in words:
        if word and word[0].isupper() and len(word) > 2:
            entities.append(word.strip(".,!?"))
    entities = list(set(entities))  # Remove duplicates

    # Identify required capabilities
    capabilities = []
    if "search" in lower_task or "find" in lower_task:
        capabilities.append("search")
    if "calculate" in lower_task or "compute" in lower_task:
        capabilities.append("calculation")
    if "translate" in lower_task:
        capabilities.append("translation")
    if "summarize" in lower_task:
        capabilities.append("summarization")
    if "code" in lower_task or "program" in lower_task:
        capabilities.append("coding")
    if not capabilities:
        capabilities.append("general")

    # Extract constraints (simple heuristics)
    constraints = []
    if "must" in lower_task:
        constraints.append("Contains mandatory requirements")
    if "limit" in lower_task or "max" in lower_task or "min" in lower_task:
        constraints.append("Has numeric limits")
    if "before" in lower_task or "after" in lower_task or "deadline" in lower_task:
        constraints.append("Has time constraints")

    # Identify success criteria
    success_criteria = []
    if task_type == "Query":
        success_criteria.append("Provide accurate answer")
    elif task_type == "Analysis":
        success_criteria.append("Complete analysis with insights")
    elif task_type == "Generation":
        success_criteria.append("Generate requested content")
    elif task_type == "Transformation":
        success_criteria.append("Successfully transform input")
    elif task_type == "Execution":
        success_criteria.append("Execute task successfully")

    return TaskAnalysisResponse(
        task_type=task_type,
        complexity_score=complexity,
        key_entities=entities,
        required_capabilities=capabilities,
        constraints=constraints,
        success_criteria=success_criteria,
        reasoning=f"Heuristic analysis: {task_type} task with complexity {complexity:.2f}",
        source="heuristic",
    )


@router.post("/analyze_task", response_model=TaskAnalysisResponse)
async def analyze_task(request: Request, body: TaskAnalysisRequest):
    """
    Analyze a task to extract understanding for FSM processing.
    Replaces hardcoded Rust FSM classification logic.
    """
    providers = getattr(request.app.state, "providers", None)

    # If no providers configured, use heuristics
    if not providers or not providers.is_configured():
        return _heuristic_task_analysis(body.task)

    # Build prompt for model-based analysis
    sys_prompt = (
        "You are a task analyzer. Analyze the given task and provide structured information. "
        "Respond with JSON containing: "
        '{"task_type": one of [Query, Analysis, Generation, Transformation, Execution, Unknown], '
        '"complexity_score": 0.0 to 1.0, '
        '"key_entities": [list of key entities/names/concepts], '
        '"required_capabilities": [list of capabilities needed], '
        '"constraints": [list of constraints or requirements], '
        '"success_criteria": [list of success criteria], '
        '"reasoning": "brief explanation"}'
    )

    user_prompt = f"Task: {body.task}"
    if body.context:
        user_prompt += f"\nContext: {json.dumps(body.context)[:200]}"

    try:
        result = await providers.generate_completion(
            messages=[
                {"role": "system", "content": sys_prompt},
                {"role": "user", "content": user_prompt},
            ],
            tier=ModelTier.SMALL,
            max_tokens=300,
            temperature=0.0,
            response_format={"type": "json_object"},
        )

        raw = result.get("completion", "")
        data = _extract_json_block(raw)

        if not data:
            return _heuristic_task_analysis(body.task)

        # Validate and normalize task_type
        task_type = str(data.get("task_type", "Unknown"))
        if task_type not in [
            "Query",
            "Analysis",
            "Generation",
            "Transformation",
            "Execution",
            "Unknown",
        ]:
            task_type = "Unknown"

        return TaskAnalysisResponse(
            task_type=task_type,
            complexity_score=float(data.get("complexity_score", 0.5)),
            key_entities=list(data.get("key_entities", [])),
            required_capabilities=list(data.get("required_capabilities", [])),
            constraints=list(data.get("constraints", [])),
            success_criteria=list(data.get("success_criteria", [])),
            reasoning=str(data.get("reasoning", "Model-based analysis")),
            source="model",
        )
    except Exception:
        # Fallback to heuristic on any error
        return _heuristic_task_analysis(body.task)
