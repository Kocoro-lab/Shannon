"""Test streaming functionality."""

import pytest
from unittest.mock import AsyncMock, MagicMock, patch
from datetime import datetime
import asyncio

from shannon import AsyncShannonClient, EventType
from shannon.models import Event
import grpc


@pytest.fixture
async def client():
    """Create client with mocked channel."""
    with patch("grpc.aio.insecure_channel"):
        client = AsyncShannonClient(grpc_endpoint="localhost:50052")
        yield client
        await client.close()


@pytest.mark.asyncio
async def test_stream_grpc_basic(client):
    """Test basic gRPC streaming."""
    from shannon.generated.orchestrator import streaming_pb2
    from google.protobuf.timestamp_pb2 import Timestamp

    # Create mock events
    ts = Timestamp()
    ts.GetCurrentTime()

    event1 = streaming_pb2.TaskUpdate(
        type="WORKFLOW_STARTED",
        workflow_id="workflow-123",
        message="Workflow started",
        timestamp=ts,
        seq=1,
        stream_id="0-1",
    )

    event2 = streaming_pb2.TaskUpdate(
        type="LLM_PARTIAL",
        workflow_id="workflow-123",
        message="Thinking...",
        timestamp=ts,
        seq=2,
        stream_id="0-2",
    )

    # Mock streaming stub
    async def mock_stream(*args, **kwargs):
        yield event1
        yield event2

    mock_stub = MagicMock()
    mock_stub.StreamTaskExecution = mock_stream
    client._streaming_stub = mock_stub

    # Collect events
    events = []
    async for event in client._stream_grpc("workflow-123"):
        events.append(event)

    # Verify
    assert len(events) == 2
    assert events[0].type == "WORKFLOW_STARTED"
    assert events[0].workflow_id == "workflow-123"
    assert events[0].seq == 1
    assert events[0].stream_id == "0-1"
    assert events[1].type == "LLM_PARTIAL"


@pytest.mark.asyncio
async def test_stream_grpc_resume_with_stream_id(client):
    """Test gRPC streaming with resume using stream_id."""
    from shannon.generated.orchestrator import streaming_pb2
    from google.protobuf.timestamp_pb2 import Timestamp

    ts = Timestamp()
    ts.GetCurrentTime()

    event = streaming_pb2.TaskUpdate(
        type="LLM_PARTIAL",
        workflow_id="workflow-123",
        message="Resumed",
        timestamp=ts,
        seq=5,
        stream_id="0-5",
    )

    async def mock_stream(request, *args, **kwargs):
        # Verify resume parameters
        assert request.last_stream_id == "0-4"
        yield event

    mock_stub = MagicMock()
    mock_stub.StreamTaskExecution = mock_stream
    client._streaming_stub = mock_stub

    # Stream with resume
    events = []
    async for evt in client._stream_grpc("workflow-123", last_stream_id="0-4"):
        events.append(evt)

    assert len(events) == 1
    assert events[0].stream_id == "0-5"


@pytest.mark.asyncio
async def test_stream_grpc_reconnect(client):
    """Test gRPC reconnection on error."""
    from shannon.generated.orchestrator import streaming_pb2
    from google.protobuf.timestamp_pb2 import Timestamp

    ts = Timestamp()
    ts.GetCurrentTime()

    # First call fails, second succeeds
    call_count = 0

    async def mock_stream(*args, **kwargs):
        nonlocal call_count
        call_count += 1
        if call_count == 1:
            raise grpc.aio.AioRpcError(
                code=grpc.StatusCode.UNAVAILABLE,
                initial_metadata=None,
                trailing_metadata=None,
                details="Connection lost",
            )
        else:
            yield streaming_pb2.TaskUpdate(
                type="LLM_PARTIAL",
                workflow_id="workflow-123",
                message="Reconnected",
                timestamp=ts,
                seq=1,
            )

    mock_stub = MagicMock()
    mock_stub.StreamTaskExecution = mock_stream
    client._streaming_stub = mock_stub

    # Should reconnect and succeed
    events = []
    with patch("asyncio.sleep"):  # Speed up backoff
        async for evt in client._stream_grpc(
            "workflow-123", reconnect=True, max_retries=2
        ):
            events.append(evt)

    assert len(events) == 1
    assert call_count == 2  # Failed once, succeeded on retry


@pytest.mark.asyncio
async def test_stream_sse_basic(client):
    """Test SSE streaming."""
    import httpx

    # Mock SSE response
    sse_data = b"""id: 1
data: {"type":"WORKFLOW_STARTED","workflow_id":"workflow-123","message":"Started","timestamp":"2025-10-05T10:00:00Z","seq":1}

id: 2
data: {"type":"LLM_PARTIAL","workflow_id":"workflow-123","message":"Thinking","timestamp":"2025-10-05T10:00:01Z","seq":2}

"""

    async def mock_aiter_lines():
        for line in sse_data.decode().split("\n"):
            yield line

    mock_response = MagicMock()
    mock_response.aiter_lines = mock_aiter_lines

    async def mock_stream(*args, **kwargs):
        return mock_response

    with patch("httpx.AsyncClient") as MockClient:
        mock_client = MockClient.return_value.__aenter__.return_value
        mock_client.stream = mock_stream

        # Stream via SSE
        events = []
        async for event in client._stream_sse("workflow-123"):
            events.append(event)

        # Verify
        assert len(events) == 2
        assert events[0].type == "WORKFLOW_STARTED"
        assert events[1].type == "LLM_PARTIAL"


@pytest.mark.asyncio
async def test_stream_auto_fallback(client):
    """Test auto-fallback from gRPC to SSE."""
    # gRPC fails
    mock_streaming_stub = MagicMock()

    async def failing_grpc(*args, **kwargs):
        raise grpc.aio.AioRpcError(
            code=grpc.StatusCode.UNAVAILABLE,
            initial_metadata=None,
            trailing_metadata=None,
            details="gRPC unavailable",
        )

    mock_streaming_stub.StreamTaskExecution = failing_grpc
    client._streaming_stub = mock_streaming_stub

    # SSE succeeds
    sse_data = b"""id: 1
data: {"type":"LLM_PARTIAL","workflow_id":"workflow-123","message":"SSE fallback","timestamp":"2025-10-05T10:00:00Z","seq":1}

"""

    async def mock_aiter_lines():
        for line in sse_data.decode().split("\n"):
            yield line

    mock_response = MagicMock()
    mock_response.aiter_lines = mock_aiter_lines

    async def mock_sse_stream(*args, **kwargs):
        return mock_response

    with patch("httpx.AsyncClient") as MockClient:
        mock_client = MockClient.return_value.__aenter__.return_value
        mock_client.stream = mock_sse_stream

        # Should fallback to SSE
        events = []
        async for event in client.stream("workflow-123", use_grpc=None):
            events.append(event)

        assert len(events) == 1
        assert events[0].message == "SSE fallback"


@pytest.mark.asyncio
async def test_stream_filter_by_type(client):
    """Test event filtering by type."""
    from shannon.generated.orchestrator import streaming_pb2
    from google.protobuf.timestamp_pb2 import Timestamp

    ts = Timestamp()
    ts.GetCurrentTime()

    events_all = [
        streaming_pb2.TaskUpdate(
            type="WORKFLOW_STARTED",
            workflow_id="workflow-123",
            message="Started",
            timestamp=ts,
            seq=1,
        ),
        streaming_pb2.TaskUpdate(
            type="LLM_PARTIAL",
            workflow_id="workflow-123",
            message="Partial",
            timestamp=ts,
            seq=2,
        ),
        streaming_pb2.TaskUpdate(
            type="TOOL_INVOKED",
            workflow_id="workflow-123",
            message="Tool",
            timestamp=ts,
            seq=3,
        ),
    ]

    async def mock_stream(request, *args, **kwargs):
        # Verify filter was sent
        assert EventType.LLM_PARTIAL.value in request.types
        for evt in events_all:
            yield evt

    mock_stub = MagicMock()
    mock_stub.StreamTaskExecution = mock_stream
    client._streaming_stub = mock_stub

    # Stream with filter
    events = []
    async for evt in client._stream_grpc(
        "workflow-123", types=[EventType.LLM_PARTIAL]
    ):
        events.append(evt)

    # Note: Filtering happens server-side in real implementation
    # Client just passes the filter to request
    assert len(events) >= 1


@pytest.mark.asyncio
async def test_stream_max_retries_exceeded(client):
    """Test max retries exceeded."""
    async def failing_grpc(*args, **kwargs):
        raise grpc.aio.AioRpcError(
            code=grpc.StatusCode.UNAVAILABLE,
            initial_metadata=None,
            trailing_metadata=None,
            details="Persistent error",
        )

    mock_stub = MagicMock()
    mock_stub.StreamTaskExecution = failing_grpc
    client._streaming_stub = mock_stub

    # Should raise after max retries
    with patch("asyncio.sleep"):  # Speed up
        with pytest.raises(Exception):  # Will raise ConnectionError or similar
            events = []
            async for evt in client._stream_grpc(
                "workflow-123", reconnect=True, max_retries=2
            ):
                events.append(evt)
