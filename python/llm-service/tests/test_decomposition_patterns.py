#!/usr/bin/env python3
"""Test the improved heuristic decomposition patterns."""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from llm_service.proto import agent_pb2

# Mock the missing modules
class MockRequest:
    def __init__(self):
        self.state = {}

import unittest.mock as mock
with mock.patch('llm_service.api.agent.APIRouter'):
    with mock.patch('llm_service.api.agent.HTTPException'):
        with mock.patch('llm_service.api.agent.Request'):
            from llm_service.api.agent import DecomposeTask

def test_decomposition(query: str, expected_type: str):
    """Test decomposition for a given query."""
    req = agent_pb2.DecomposeTaskRequest(
        query=query,
        user_id='test-user',
        session_id='test-session'
    )
    
    # Mock context
    context = MockRequest()
    
    try:
        result = DecomposeTask(req, context)
        print(f"\n{expected_type} Query: '{query}'")
        print(f"  Subtasks: {len(result.subtasks)}")
        for i, st in enumerate(result.subtasks, 1):
            tools = f" [Tools: {', '.join(st.suggested_tools)}]" if st.suggested_tools else ""
            print(f"  {i}. {st.description}{tools}")
        return result
    except Exception as e:
        print(f"Error: {e}")
        return None

# Test different query types
print("=" * 60)
print("Testing Improved Heuristic Decomposition Patterns")
print("=" * 60)

# Single-step calculation
test_decomposition("Calculate 100 divided by 4", "Calculation")

# Multi-step calculation
test_decomposition("Calculate 500 + 300 - 200 and then multiply by 2", "Multi-step Calculation")

# Research task
test_decomposition("Research the history of artificial intelligence", "Research")

# Code generation
test_decomposition("Write a Python function to sort a list", "Code Generation")

# Analysis task
test_decomposition("Analyze the performance of our database", "Analysis")

# Summary task
test_decomposition("Summarize the key points of machine learning", "Summary")

# Comparison task
test_decomposition("Compare Python and JavaScript for web development", "Comparison")

# Generic question
test_decomposition("What is the weather like today?", "Generic Question")