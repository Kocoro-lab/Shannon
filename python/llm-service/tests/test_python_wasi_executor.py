"""
Test suite for Python WASI Executor edge cases.

Tests various edge cases including:
- Out of Memory (OOM) conditions
- Infinite loops
- Malformed code
- Large outputs
- Session persistence
- Security boundaries
"""

import asyncio
import pytest
from unittest.mock import Mock, AsyncMock, patch, MagicMock

from llm_service.tools.builtin.python_wasi_executor import (
    PythonWasiExecutorTool,
    ExecutionSession,
)


class TestPythonWasiExecutor:
    """Test cases for Python WASI Executor edge cases"""

    @pytest.fixture
    def executor(self):
        """Create a Python WASI executor instance"""
        return PythonWasiExecutorTool()

    @pytest.fixture
    def mock_grpc_stub(self):
        """Create a mock gRPC stub for testing"""
        stub = AsyncMock()
        return stub

    @pytest.mark.asyncio
    async def test_infinite_loop_timeout(self, executor, mock_grpc_stub):
        """Test that infinite loops are terminated by timeout"""
        # Code with infinite loop
        infinite_loop_code = """
while True:
    x = 1 + 1
"""

        # Mock the gRPC call to simulate timeout
        with patch(
            "llm_service.tools.builtin.python_wasi_executor.grpc.aio.insecure_channel"
        ) as mock_channel:
            mock_channel.return_value.__aenter__ = AsyncMock(return_value=MagicMock())
            mock_channel.return_value.__aexit__ = AsyncMock()

            with patch(
                "llm_service.tools.builtin.python_wasi_executor.agent_pb2_grpc.AgentServiceStub"
            ) as mock_stub_class:
                mock_stub_class.return_value = mock_grpc_stub

                # Simulate timeout
                with patch(
                    "llm_service.tools.builtin.python_wasi_executor.asyncio.wait_for"
                ) as mock_wait:
                    mock_wait.side_effect = asyncio.TimeoutError()

                    result = await executor._execute_impl(
                        code=infinite_loop_code, timeout_seconds=1
                    )

                    assert result.success is False
                    assert "timeout" in result.error.lower()
                    assert result.metadata.get("timeout") is True

    @pytest.mark.asyncio
    async def test_malformed_code_syntax_error(self, executor, mock_grpc_stub):
        """Test handling of malformed Python code with syntax errors"""
        malformed_code = """
def broken_function(
    print("missing closing parenthesis")
    return 42
"""

        with patch(
            "llm_service.tools.builtin.python_wasi_executor.grpc.aio.insecure_channel"
        ) as mock_channel:
            mock_channel.return_value.__aenter__ = AsyncMock(return_value=MagicMock())
            mock_channel.return_value.__aexit__ = AsyncMock()

            with patch(
                "llm_service.tools.builtin.python_wasi_executor.agent_pb2_grpc.AgentServiceStub"
            ) as mock_stub_class:
                mock_stub_class.return_value = mock_grpc_stub

                # Mock response with syntax error
                mock_response = Mock()
                mock_response.result = ""
                mock_response.error_message = "SyntaxError: invalid syntax"
                mock_grpc_stub.ExecuteTask.return_value = mock_response

                result = await executor._execute_impl(code=malformed_code)

                assert result.success is False
                assert "SyntaxError" in result.error or "syntax" in result.error.lower()

    @pytest.mark.asyncio
    async def test_memory_exhaustion(self, executor, mock_grpc_stub):
        """Test handling of code that tries to exhaust memory"""
        memory_bomb_code = """
# Try to allocate massive list
huge_list = []
for i in range(10**9):
    huge_list.append([0] * 10**6)
    print(f"Allocated {i} blocks")
"""

        with patch(
            "llm_service.tools.builtin.python_wasi_executor.grpc.aio.insecure_channel"
        ) as mock_channel:
            mock_channel.return_value.__aenter__ = AsyncMock(return_value=MagicMock())
            mock_channel.return_value.__aexit__ = AsyncMock()

            with patch(
                "llm_service.tools.builtin.python_wasi_executor.agent_pb2_grpc.AgentServiceStub"
            ) as mock_stub_class:
                mock_stub_class.return_value = mock_grpc_stub

                # Mock OOM error response
                mock_response = Mock()
                mock_response.result = ""
                mock_response.error_message = "MemoryError: Unable to allocate memory"
                mock_grpc_stub.ExecuteTask.return_value = mock_response

                result = await executor._execute_impl(code=memory_bomb_code)

                assert result.success is False
                assert "memory" in result.error.lower()

    @pytest.mark.asyncio
    async def test_large_output_handling(self, executor, mock_grpc_stub):
        """Test handling of code that produces very large outputs"""
        large_output_code = """
# Generate large output
for i in range(100000):
    print(f"Line {i}: " + "x" * 100)
"""

        with patch(
            "llm_service.tools.builtin.python_wasi_executor.grpc.aio.insecure_channel"
        ) as mock_channel:
            mock_channel.return_value.__aenter__ = AsyncMock(return_value=MagicMock())
            mock_channel.return_value.__aexit__ = AsyncMock()

            with patch(
                "llm_service.tools.builtin.python_wasi_executor.agent_pb2_grpc.AgentServiceStub"
            ) as mock_stub_class:
                mock_stub_class.return_value = mock_grpc_stub

                # Mock truncated output
                mock_response = Mock()
                mock_response.result = (
                    "Line 0: " + "x" * 100 + "\n" + "[Output truncated]"
                )
                mock_response.error_message = None
                mock_grpc_stub.ExecuteTask.return_value = mock_response

                result = await executor._execute_impl(code=large_output_code)

                assert result.success is True
                assert "[Output truncated]" in result.output or len(result.output) > 0

    @pytest.mark.asyncio
    async def test_session_variable_persistence(self, executor):
        """Test that session variables persist across executions"""
        session_id = "test_session_123"

        # First execution - set variable
        session = await executor._get_or_create_session(session_id)
        assert session is not None
        assert session.session_id == session_id

        # Simulate storing variables
        session.variables["x"] = 42
        session.variables["name"] = "Alice"

        # Second execution - verify variables exist
        session2 = await executor._get_or_create_session(session_id)
        assert session2 is not None
        assert session2.variables["x"] == 42
        assert session2.variables["name"] == "Alice"
        assert session2.execution_count == 2

    @pytest.mark.asyncio
    async def test_recursive_code_stack_overflow(self, executor, mock_grpc_stub):
        """Test handling of code that causes stack overflow"""
        recursive_code = """
def infinite_recursion(n):
    return infinite_recursion(n + 1)

infinite_recursion(0)
"""

        with patch(
            "llm_service.tools.builtin.python_wasi_executor.grpc.aio.insecure_channel"
        ) as mock_channel:
            mock_channel.return_value.__aenter__ = AsyncMock(return_value=MagicMock())
            mock_channel.return_value.__aexit__ = AsyncMock()

            with patch(
                "llm_service.tools.builtin.python_wasi_executor.agent_pb2_grpc.AgentServiceStub"
            ) as mock_stub_class:
                mock_stub_class.return_value = mock_grpc_stub

                # Mock stack overflow error
                mock_response = Mock()
                mock_response.result = ""
                mock_response.error_message = (
                    "RecursionError: maximum recursion depth exceeded"
                )
                mock_grpc_stub.ExecuteTask.return_value = mock_response

                result = await executor._execute_impl(code=recursive_code)

                assert result.success is False
                assert "recursion" in result.error.lower()

    @pytest.mark.asyncio
    async def test_fork_bomb_prevention(self, executor, mock_grpc_stub):
        """Test that fork bombs are prevented"""
        fork_bomb_code = """
import os
import subprocess

# Try to spawn many processes
for i in range(1000):
    subprocess.Popen(['python', '-c', 'while True: pass'])
"""

        with patch(
            "llm_service.tools.builtin.python_wasi_executor.grpc.aio.insecure_channel"
        ) as mock_channel:
            mock_channel.return_value.__aenter__ = AsyncMock(return_value=MagicMock())
            mock_channel.return_value.__aexit__ = AsyncMock()

            with patch(
                "llm_service.tools.builtin.python_wasi_executor.agent_pb2_grpc.AgentServiceStub"
            ) as mock_stub_class:
                mock_stub_class.return_value = mock_grpc_stub

                # Mock sandbox restriction error
                mock_response = Mock()
                mock_response.result = ""
                mock_response.error_message = (
                    "ImportError: No module named 'subprocess'"
                )
                mock_grpc_stub.ExecuteTask.return_value = mock_response

                result = await executor._execute_impl(code=fork_bomb_code)

                assert result.success is False
                # WASI sandbox should prevent subprocess imports

    @pytest.mark.asyncio
    async def test_session_cleanup_on_timeout(self, executor):
        """Test that expired sessions are cleaned up"""
        import time

        # Create multiple sessions
        for i in range(5):
            session = await executor._get_or_create_session(f"session_{i}")
            session.last_accessed = time.time() - (
                executor._session_timeout + 100
            )  # Expired

        # Create one active session
        await executor._get_or_create_session("active_session")

        # Trigger cleanup by creating another session
        await executor._get_or_create_session("new_session")

        # Check that expired sessions are removed
        assert "active_session" in executor._sessions
        assert "new_session" in executor._sessions
        for i in range(5):
            assert f"session_{i}" not in executor._sessions

    @pytest.mark.asyncio
    async def test_concurrent_session_access(self, executor):
        """Test thread-safe concurrent access to sessions"""
        import asyncio

        # Clear any existing sessions
        executor._sessions.clear()

        async def create_and_modify_session(session_id: str, value: int):
            """Create a session and modify its variables"""
            session = await executor._get_or_create_session(session_id)
            await asyncio.sleep(0.01)  # Simulate some work
            session.variables[f"var_{value}"] = value
            return session

        # Create multiple concurrent tasks accessing different sessions
        tasks = []
        for i in range(10):
            # Some tasks use same session, some use different
            session_id = (
                f"session_{i % 3}"  # 3 unique sessions, accessed multiple times
            )
            tasks.append(create_and_modify_session(session_id, i))

        # Run all tasks concurrently
        await asyncio.gather(*tasks)

        # Verify sessions were created correctly
        assert len(executor._sessions) == 3  # Only 3 unique sessions

        # Verify all variables were set (no race condition data loss)
        all_vars = set()
        for session in executor._sessions.values():
            all_vars.update(session.variables.keys())

        # Should have variables from all 10 tasks
        assert len(all_vars) == 10

        # Verify execution counts
        total_executions = sum(s.execution_count for s in executor._sessions.values())
        assert total_executions == 10

    @pytest.mark.asyncio
    async def test_empty_code_handling(self, executor):
        """Test handling of empty code input"""
        result = await executor._execute_impl(code="")

        assert result.success is False
        assert "No code provided" in result.error

    @pytest.mark.asyncio
    async def test_code_with_print_statements(self, executor, mock_grpc_stub):
        """Test that print statements work correctly"""
        print_code = """
print("Hello, World!")
x = 42
print(f"The answer is {x}")
"""

        with patch(
            "llm_service.tools.builtin.python_wasi_executor.grpc.aio.insecure_channel"
        ) as mock_channel:
            mock_channel.return_value.__aenter__ = AsyncMock(return_value=MagicMock())
            mock_channel.return_value.__aexit__ = AsyncMock()

            with patch(
                "llm_service.tools.builtin.python_wasi_executor.agent_pb2_grpc.AgentServiceStub"
            ) as mock_stub_class:
                mock_stub_class.return_value = mock_grpc_stub

                # Mock successful execution with output
                mock_response = Mock()
                mock_response.result = "Hello, World!\nThe answer is 42"
                mock_response.error_message = None
                mock_grpc_stub.ExecuteTask.return_value = mock_response

                result = await executor._execute_impl(code=print_code)

                assert result.success is True
                assert "Hello, World!" in result.output
                assert "The answer is 42" in result.output

    @pytest.mark.asyncio
    async def test_session_state_extraction(self, executor):
        """Test extraction of session state from output"""
        session = ExecutionSession(session_id="test")

        # Simulate output with session state
        output_with_state = """
Hello World
Result: 42
__SESSION_STATE__:{"x": "42", "name": "'Alice'", "data": "[1, 2, 3]"}__END_SESSION__
"""

        clean_output = await executor._extract_session_state(output_with_state, session)

        # Check that state was extracted
        assert session.variables["x"] == 42
        assert session.variables["name"] == "Alice"
        assert session.variables["data"] == [1, 2, 3]

        # Check that output was cleaned
        assert "__SESSION_STATE__" not in clean_output
        assert "Hello World" in clean_output
        assert "Result: 42" in clean_output

    @pytest.mark.asyncio
    async def test_max_timeout_enforcement(self, executor):
        """Test that timeout is capped at maximum value"""
        with patch(
            "llm_service.tools.builtin.python_wasi_executor.grpc.aio.insecure_channel"
        ) as mock_channel:
            mock_channel.return_value.__aenter__ = AsyncMock(return_value=MagicMock())
            mock_channel.return_value.__aexit__ = AsyncMock()

            with patch(
                "llm_service.tools.builtin.python_wasi_executor.agent_pb2_grpc.AgentServiceStub"
            ):
                with patch(
                    "llm_service.tools.builtin.python_wasi_executor.asyncio.wait_for"
                ) as mock_wait:
                    mock_wait.return_value = Mock(result="Success", error_message=None)

                    # Try to set timeout > 60 seconds
                    await executor._execute_impl(
                        code="print('test')",
                        timeout_seconds=300,  # Should be capped at 60
                    )

                    # Check that timeout was capped at 60
                    _, kwargs = mock_wait.call_args
                    assert kwargs["timeout"] == 60
