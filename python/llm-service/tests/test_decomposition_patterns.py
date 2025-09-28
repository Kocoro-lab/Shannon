"""Test the improved heuristic decomposition patterns."""

import pytest
import unittest.mock as mock
from llm_service.grpc_gen import agent_pb2


# Mock the missing modules
class MockRequest:
    def __init__(self):
        self.state = {}


def test_calculation_simple():
    """Test single-step calculation decomposition."""
    with mock.patch("llm_service.api.agent.APIRouter"), \
         mock.patch("llm_service.api.agent.HTTPException"), \
         mock.patch("llm_service.api.agent.Request"):
        from llm_service.api.agent import DecomposeTask

        req = agent_pb2.DecomposeTaskRequest(
            query="Calculate 100 divided by 4",
            user_id="test-user",
            session_id="test-session"
        )
        context = MockRequest()
        result = DecomposeTask(req, context)

        assert result is not None
        assert len(result.subtasks) >= 1
        # Should suggest calculator tool for simple calculation
        assert any("calculator" in st.suggested_tools for st in result.subtasks)


def test_calculation_multi_step():
    """Test multi-step calculation decomposition."""
    with mock.patch("llm_service.api.agent.APIRouter"), \
         mock.patch("llm_service.api.agent.HTTPException"), \
         mock.patch("llm_service.api.agent.Request"):
        from llm_service.api.agent import DecomposeTask

        req = agent_pb2.DecomposeTaskRequest(
            query="Calculate 500 + 300 - 200 and then multiply by 2",
            user_id="test-user",
            session_id="test-session"
        )
        context = MockRequest()
        result = DecomposeTask(req, context)

        assert result is not None
        assert len(result.subtasks) >= 1
        # Complex calculation might be broken down
        assert any("calculator" in st.suggested_tools for st in result.subtasks)


def test_research_task():
    """Test research task decomposition."""
    with mock.patch("llm_service.api.agent.APIRouter"), \
         mock.patch("llm_service.api.agent.HTTPException"), \
         mock.patch("llm_service.api.agent.Request"):
        from llm_service.api.agent import DecomposeTask

        req = agent_pb2.DecomposeTaskRequest(
            query="Research the history of artificial intelligence",
            user_id="test-user",
            session_id="test-session"
        )
        context = MockRequest()
        result = DecomposeTask(req, context)

        assert result is not None
        assert len(result.subtasks) >= 1
        # Research tasks should suggest web search
        assert any("web_search" in st.suggested_tools for st in result.subtasks)


def test_code_generation():
    """Test code generation task decomposition."""
    with mock.patch("llm_service.api.agent.APIRouter"), \
         mock.patch("llm_service.api.agent.HTTPException"), \
         mock.patch("llm_service.api.agent.Request"):
        from llm_service.api.agent import DecomposeTask

        req = agent_pb2.DecomposeTaskRequest(
            query="Write a Python function to sort a list",
            user_id="test-user",
            session_id="test-session"
        )
        context = MockRequest()
        result = DecomposeTask(req, context)

        assert result is not None
        assert len(result.subtasks) >= 1
        # Code generation might suggest python execution
        assert any("python_executor" in st.suggested_tools for st in result.subtasks)


def test_analysis_task():
    """Test analysis task decomposition."""
    with mock.patch("llm_service.api.agent.APIRouter"), \
         mock.patch("llm_service.api.agent.HTTPException"), \
         mock.patch("llm_service.api.agent.Request"):
        from llm_service.api.agent import DecomposeTask

        req = agent_pb2.DecomposeTaskRequest(
            query="Analyze the performance of our database",
            user_id="test-user",
            session_id="test-session"
        )
        context = MockRequest()
        result = DecomposeTask(req, context)

        assert result is not None
        assert len(result.subtasks) >= 1


def test_comparison_task():
    """Test comparison task decomposition."""
    with mock.patch("llm_service.api.agent.APIRouter"), \
         mock.patch("llm_service.api.agent.HTTPException"), \
         mock.patch("llm_service.api.agent.Request"):
        from llm_service.api.agent import DecomposeTask

        req = agent_pb2.DecomposeTaskRequest(
            query="Compare Python and JavaScript for web development",
            user_id="test-user",
            session_id="test-session"
        )
        context = MockRequest()
        result = DecomposeTask(req, context)

        assert result is not None
        assert len(result.subtasks) >= 1
        # Comparison might involve research
        assert any("web_search" in st.suggested_tools for st in result.subtasks)
