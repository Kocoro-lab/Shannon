"""
Enhanced Agent API with Tool Support
"""

from fastapi import APIRouter, Request, HTTPException
from pydantic import BaseModel, Field
from typing import List, Dict, Any, Optional
import logging

from ..tools import get_registry

logger = logging.getLogger(__name__)
router = APIRouter(prefix="/agent", tags=["agent"])


class ToolCall(BaseModel):
    """Represents a tool call request from the agent"""

    tool_name: str
    parameters: Dict[str, Any]
    call_id: Optional[str] = None  # For tracking


class AgentQueryWithTools(BaseModel):
    """Agent query that can use tools"""

    query: str = Field(..., description="The user query or task")
    agent_id: str = Field(..., description="Unique identifier for the agent")
    mode: str = Field(default="standard", description="Execution mode")
    context: Optional[Dict[str, Any]] = Field(
        default_factory=dict, description="Additional context"
    )
    available_tools: Optional[List[str]] = Field(
        default=None, description="List of tools available to agent"
    )
    max_tokens: Optional[int] = Field(
        default=1000, description="Maximum tokens for response"
    )
    temperature: Optional[float] = Field(
        default=0.7, description="Temperature for generation"
    )

    class Config:
        schema_extra = {
            "example": {
                "query": "What is 25 * 4 + 10?",
                "agent_id": "agent-123",
                "mode": "standard",
                "available_tools": ["calculator"],
                "max_tokens": 500,
                "temperature": 0.3,
            }
        }


class AgentResponseWithTools(BaseModel):
    """Agent response that may include tool calls and results"""

    response: str = Field(..., description="The agent's response")
    tool_calls: List[ToolCall] = Field(
        default_factory=list, description="Tools the agent wants to call"
    )
    tool_results: List[Dict[str, Any]] = Field(
        default_factory=list, description="Results from tool executions"
    )
    tokens_used: int = Field(..., description="Number of tokens used")
    model_used: str = Field(..., description="Model that was used")
    metadata: Dict[str, Any] = Field(
        default_factory=dict, description="Additional metadata"
    )


@router.post("/query-with-tools", response_model=AgentResponseWithTools)
async def agent_query_with_tools(request: Request, query: AgentQueryWithTools):
    """
    Process an agent query with tool support.

    This endpoint:
    1. Receives a query from the agent
    2. Determines if tools are needed based on the query
    3. Executes tools if necessary
    4. Returns results including tool outputs
    """
    try:
        logger.info(f"Agent query with tools: {query.query[:100]}...")

        # Get tool registry
        registry = get_registry()

        # Filter available tools based on agent's request
        if query.available_tools:
            available_tools = [
                tool
                for tool in query.available_tools
                if registry.get_tool(tool) is not None
            ]
        else:
            # Default to safe tools only
            available_tools = registry.filter_tools_for_agent(
                exclude_dangerous=True, max_cost=0.01
            )

        # Analyze query to determine if tools are needed
        tool_calls = await _analyze_query_for_tools(query.query, available_tools)

        tool_results = []
        if tool_calls:
            # Execute tools
            for tool_call in tool_calls:
                tool = registry.get_tool(tool_call.tool_name)
                if tool:
                    result = await tool.execute(**tool_call.parameters)
                    tool_results.append(
                        {
                            "tool": tool_call.tool_name,
                            "success": result.success,
                            "output": result.output,
                            "error": result.error,
                        }
                    )

        # Generate response based on query and tool results
        response_text = await _generate_response_with_tools(
            query.query,
            tool_results,
            request.app.state if hasattr(request, "app") else None,
        )

        # Estimate token usage (simplified)
        tokens = len(query.query.split()) + len(response_text.split())

        return AgentResponseWithTools(
            response=response_text,
            tool_calls=tool_calls,
            tool_results=tool_results,
            tokens_used=tokens * 2,  # Rough estimate
            model_used="gpt-3.5-turbo",  # Would come from actual provider
            metadata={
                "available_tools": available_tools,
                "tools_executed": len(tool_results),
            },
        )

    except Exception as e:
        logger.error(f"Error in agent query with tools: {e}")
        raise HTTPException(status_code=500, detail=str(e))


async def _analyze_query_for_tools(
    query: str, available_tools: List[str]
) -> List[ToolCall]:
    """
    Analyze the query to determine which tools to use.

    In production, this would use an LLM to understand the query
    and determine appropriate tool calls.
    """
    tool_calls = []

    # Simple heuristic-based tool detection for now
    query_lower = query.lower()

    # Check for calculator needs
    if "calculator" in available_tools:
        # Look for mathematical expressions
        math_keywords = [
            "calculate",
            "compute",
            "+",
            "-",
            "*",
            "/",
            "sqrt",
            "sum",
            "average",
        ]
        if any(keyword in query_lower for keyword in math_keywords):
            # Extract the expression (simplified)
            expression = query
            for word in ["calculate", "compute", "what is", "what's", "evaluate"]:
                expression = expression.lower().replace(word, "").strip()

            if expression:
                tool_calls.append(
                    ToolCall(
                        tool_name="calculator",
                        parameters={"expression": expression},
                        call_id="calc_1",
                    )
                )

    # Check for web search needs
    if "web_search" in available_tools:
        search_keywords = [
            "search",
            "find",
            "look up",
            "google",
            "what is",
            "who is",
            "when was",
        ]
        if any(keyword in query_lower for keyword in search_keywords):
            # Extract search query
            search_query = query
            for word in ["search for", "find", "look up", "google"]:
                search_query = search_query.lower().replace(word, "").strip()

            tool_calls.append(
                ToolCall(
                    tool_name="web_search",
                    parameters={"query": search_query, "max_results": 3},
                    call_id="search_1",
                )
            )

    # Check for file operations
    if "file_read" in available_tools and (
        "read" in query_lower or "open" in query_lower
    ):
        # Extract file path (simplified - would use NER in production)
        import re

        path_match = re.search(r'["\']([^"\']+)["\']', query)
        if path_match:
            file_path = path_match.group(1)
            tool_calls.append(
                ToolCall(
                    tool_name="file_read",
                    parameters={"path": file_path},
                    call_id="read_1",
                )
            )

    return tool_calls


async def _generate_response_with_tools(
    query: str, tool_results: List[Dict[str, Any]], app_state: Optional[Any] = None
) -> str:
    """
    Generate a response incorporating tool results.

    In production, this would use an LLM to generate a natural response
    that incorporates the tool results.
    """
    # If we have tool results, incorporate them
    if tool_results:
        response_parts = []

        for result in tool_results:
            if result["success"]:
                if result["tool"] == "calculator":
                    response_parts.append(
                        f"The calculation result is: {result['output']}"
                    )
                elif result["tool"] == "web_search":
                    response_parts.append("Here's what I found:")
                    for item in result["output"][:3]:
                        response_parts.append(
                            f"- {item.get('title', '')}: {item.get('snippet', '')}"
                        )
                elif result["tool"] == "file_read":
                    content = str(result["output"])[:500]  # Limit content length
                    response_parts.append(f"File contents: {content}")
                else:
                    response_parts.append(
                        f"Tool {result['tool']} returned: {result['output']}"
                    )
            else:
                response_parts.append(
                    f"Tool {result['tool']} failed: {result.get('error', 'Unknown error')}"
                )

        return "\n".join(response_parts)

    # No tools used, return a simple response
    return f"I understand your query: '{query}'. Based on the available information, I can help you with that."


@router.get("/available-tools")
async def get_available_tools_for_agent(
    agent_id: str,
    exclude_dangerous: bool = True,
) -> List[Dict[str, Any]]:
    """
    Get list of tools available for a specific agent.

    In production, this would check agent permissions and quotas.
    """
    registry = get_registry()

    # Get filtered tools
    tool_names = registry.filter_tools_for_agent(
        exclude_dangerous=exclude_dangerous,
        max_cost=0.01,  # Default cost limit
    )

    # Get schemas for each tool
    tools_info = []
    for tool_name in tool_names:
        schema = registry.get_tool_schema(tool_name)
        if schema:
            tools_info.append(
                {
                    "name": tool_name,
                    "description": schema["description"],
                    "parameters": schema["parameters"],
                }
            )

    return tools_info
