from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class StreamRequest(_message.Message):
    __slots__ = ("workflow_id", "types", "last_event_id", "last_stream_id")
    WORKFLOW_ID_FIELD_NUMBER: _ClassVar[int]
    TYPES_FIELD_NUMBER: _ClassVar[int]
    LAST_EVENT_ID_FIELD_NUMBER: _ClassVar[int]
    LAST_STREAM_ID_FIELD_NUMBER: _ClassVar[int]
    workflow_id: str
    types: _containers.RepeatedScalarFieldContainer[str]
    last_event_id: int
    last_stream_id: str
    def __init__(self, workflow_id: _Optional[str] = ..., types: _Optional[_Iterable[str]] = ..., last_event_id: _Optional[int] = ..., last_stream_id: _Optional[str] = ...) -> None: ...

class TaskUpdate(_message.Message):
    __slots__ = ("workflow_id", "type", "agent_id", "message", "timestamp", "seq", "stream_id")
    WORKFLOW_ID_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    TIMESTAMP_FIELD_NUMBER: _ClassVar[int]
    SEQ_FIELD_NUMBER: _ClassVar[int]
    STREAM_ID_FIELD_NUMBER: _ClassVar[int]
    workflow_id: str
    type: str
    agent_id: str
    message: str
    timestamp: _timestamp_pb2.Timestamp
    seq: int
    stream_id: str
    def __init__(self, workflow_id: _Optional[str] = ..., type: _Optional[str] = ..., agent_id: _Optional[str] = ..., message: _Optional[str] = ..., timestamp: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., seq: _Optional[int] = ..., stream_id: _Optional[str] = ...) -> None: ...
