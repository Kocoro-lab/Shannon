from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf import struct_pb2 as _struct_pb2
from common import common_pb2 as _common_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class CreateSessionRequest(_message.Message):
    __slots__ = ("user_id", "initial_context", "max_history", "ttl_seconds")
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    INITIAL_CONTEXT_FIELD_NUMBER: _ClassVar[int]
    MAX_HISTORY_FIELD_NUMBER: _ClassVar[int]
    TTL_SECONDS_FIELD_NUMBER: _ClassVar[int]
    user_id: str
    initial_context: _struct_pb2.Struct
    max_history: int
    ttl_seconds: int
    def __init__(self, user_id: _Optional[str] = ..., initial_context: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., max_history: _Optional[int] = ..., ttl_seconds: _Optional[int] = ...) -> None: ...

class CreateSessionResponse(_message.Message):
    __slots__ = ("session_id", "created_at", "expires_at", "status", "message")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    created_at: _timestamp_pb2.Timestamp
    expires_at: _timestamp_pb2.Timestamp
    status: _common_pb2.StatusCode
    message: str
    def __init__(self, session_id: _Optional[str] = ..., created_at: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., expires_at: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., status: _Optional[_Union[_common_pb2.StatusCode, str]] = ..., message: _Optional[str] = ...) -> None: ...

class GetSessionRequest(_message.Message):
    __slots__ = ("session_id", "include_history")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    INCLUDE_HISTORY_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    include_history: bool
    def __init__(self, session_id: _Optional[str] = ..., include_history: bool = ...) -> None: ...

class GetSessionResponse(_message.Message):
    __slots__ = ("session", "status", "message")
    SESSION_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    session: Session
    status: _common_pb2.StatusCode
    message: str
    def __init__(self, session: _Optional[_Union[Session, _Mapping]] = ..., status: _Optional[_Union[_common_pb2.StatusCode, str]] = ..., message: _Optional[str] = ...) -> None: ...

class UpdateSessionRequest(_message.Message):
    __slots__ = ("session_id", "context_updates", "extend_ttl_seconds")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_UPDATES_FIELD_NUMBER: _ClassVar[int]
    EXTEND_TTL_SECONDS_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    context_updates: _struct_pb2.Struct
    extend_ttl_seconds: int
    def __init__(self, session_id: _Optional[str] = ..., context_updates: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., extend_ttl_seconds: _Optional[int] = ...) -> None: ...

class UpdateSessionResponse(_message.Message):
    __slots__ = ("success", "new_expires_at", "status", "message")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    NEW_EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    success: bool
    new_expires_at: _timestamp_pb2.Timestamp
    status: _common_pb2.StatusCode
    message: str
    def __init__(self, success: bool = ..., new_expires_at: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., status: _Optional[_Union[_common_pb2.StatusCode, str]] = ..., message: _Optional[str] = ...) -> None: ...

class DeleteSessionRequest(_message.Message):
    __slots__ = ("session_id",)
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    def __init__(self, session_id: _Optional[str] = ...) -> None: ...

class DeleteSessionResponse(_message.Message):
    __slots__ = ("success", "status", "message")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    success: bool
    status: _common_pb2.StatusCode
    message: str
    def __init__(self, success: bool = ..., status: _Optional[_Union[_common_pb2.StatusCode, str]] = ..., message: _Optional[str] = ...) -> None: ...

class ListSessionsRequest(_message.Message):
    __slots__ = ("user_id", "active_only", "limit", "offset")
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_ONLY_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    OFFSET_FIELD_NUMBER: _ClassVar[int]
    user_id: str
    active_only: bool
    limit: int
    offset: int
    def __init__(self, user_id: _Optional[str] = ..., active_only: bool = ..., limit: _Optional[int] = ..., offset: _Optional[int] = ...) -> None: ...

class ListSessionsResponse(_message.Message):
    __slots__ = ("sessions", "total_count", "status", "message")
    SESSIONS_FIELD_NUMBER: _ClassVar[int]
    TOTAL_COUNT_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    sessions: _containers.RepeatedCompositeFieldContainer[SessionSummary]
    total_count: int
    status: _common_pb2.StatusCode
    message: str
    def __init__(self, sessions: _Optional[_Iterable[_Union[SessionSummary, _Mapping]]] = ..., total_count: _Optional[int] = ..., status: _Optional[_Union[_common_pb2.StatusCode, str]] = ..., message: _Optional[str] = ...) -> None: ...

class AddMessageRequest(_message.Message):
    __slots__ = ("session_id", "message")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    message: Message
    def __init__(self, session_id: _Optional[str] = ..., message: _Optional[_Union[Message, _Mapping]] = ...) -> None: ...

class AddMessageResponse(_message.Message):
    __slots__ = ("success", "history_size", "status", "message")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    HISTORY_SIZE_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    success: bool
    history_size: int
    status: _common_pb2.StatusCode
    message: str
    def __init__(self, success: bool = ..., history_size: _Optional[int] = ..., status: _Optional[_Union[_common_pb2.StatusCode, str]] = ..., message: _Optional[str] = ...) -> None: ...

class ClearHistoryRequest(_message.Message):
    __slots__ = ("session_id", "keep_context")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    KEEP_CONTEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    keep_context: bool
    def __init__(self, session_id: _Optional[str] = ..., keep_context: bool = ...) -> None: ...

class ClearHistoryResponse(_message.Message):
    __slots__ = ("success", "status", "message")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    success: bool
    status: _common_pb2.StatusCode
    message: str
    def __init__(self, success: bool = ..., status: _Optional[_Union[_common_pb2.StatusCode, str]] = ..., message: _Optional[str] = ...) -> None: ...

class Session(_message.Message):
    __slots__ = ("id", "user_id", "context", "history", "created_at", "last_active", "expires_at", "metrics")
    ID_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    HISTORY_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_ACTIVE_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    METRICS_FIELD_NUMBER: _ClassVar[int]
    id: str
    user_id: str
    context: _struct_pb2.Struct
    history: _containers.RepeatedCompositeFieldContainer[Message]
    created_at: _timestamp_pb2.Timestamp
    last_active: _timestamp_pb2.Timestamp
    expires_at: _timestamp_pb2.Timestamp
    metrics: SessionMetrics
    def __init__(self, id: _Optional[str] = ..., user_id: _Optional[str] = ..., context: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., history: _Optional[_Iterable[_Union[Message, _Mapping]]] = ..., created_at: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., last_active: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., expires_at: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., metrics: _Optional[_Union[SessionMetrics, _Mapping]] = ...) -> None: ...

class SessionSummary(_message.Message):
    __slots__ = ("id", "user_id", "created_at", "last_active", "message_count", "total_tokens", "is_active")
    ID_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_ACTIVE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_COUNT_FIELD_NUMBER: _ClassVar[int]
    TOTAL_TOKENS_FIELD_NUMBER: _ClassVar[int]
    IS_ACTIVE_FIELD_NUMBER: _ClassVar[int]
    id: str
    user_id: str
    created_at: _timestamp_pb2.Timestamp
    last_active: _timestamp_pb2.Timestamp
    message_count: int
    total_tokens: int
    is_active: bool
    def __init__(self, id: _Optional[str] = ..., user_id: _Optional[str] = ..., created_at: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., last_active: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., message_count: _Optional[int] = ..., total_tokens: _Optional[int] = ..., is_active: bool = ...) -> None: ...

class Message(_message.Message):
    __slots__ = ("id", "role", "content", "timestamp", "tokens_used", "metadata")
    ID_FIELD_NUMBER: _ClassVar[int]
    ROLE_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    TIMESTAMP_FIELD_NUMBER: _ClassVar[int]
    TOKENS_USED_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    id: str
    role: str
    content: str
    timestamp: _timestamp_pb2.Timestamp
    tokens_used: int
    metadata: _struct_pb2.Struct
    def __init__(self, id: _Optional[str] = ..., role: _Optional[str] = ..., content: _Optional[str] = ..., timestamp: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., tokens_used: _Optional[int] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class SessionMetrics(_message.Message):
    __slots__ = ("total_messages", "total_tokens", "total_cost_usd", "tools_invoked", "errors_encountered", "avg_response_time_ms")
    TOTAL_MESSAGES_FIELD_NUMBER: _ClassVar[int]
    TOTAL_TOKENS_FIELD_NUMBER: _ClassVar[int]
    TOTAL_COST_USD_FIELD_NUMBER: _ClassVar[int]
    TOOLS_INVOKED_FIELD_NUMBER: _ClassVar[int]
    ERRORS_ENCOUNTERED_FIELD_NUMBER: _ClassVar[int]
    AVG_RESPONSE_TIME_MS_FIELD_NUMBER: _ClassVar[int]
    total_messages: int
    total_tokens: int
    total_cost_usd: float
    tools_invoked: int
    errors_encountered: int
    avg_response_time_ms: float
    def __init__(self, total_messages: _Optional[int] = ..., total_tokens: _Optional[int] = ..., total_cost_usd: _Optional[float] = ..., tools_invoked: _Optional[int] = ..., errors_encountered: _Optional[int] = ..., avg_response_time_ms: _Optional[float] = ...) -> None: ...
