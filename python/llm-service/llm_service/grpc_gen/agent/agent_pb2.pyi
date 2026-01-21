from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from common import common_pb2 as _common_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AgentState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_STATE_UNSPECIFIED: _ClassVar[AgentState]
    AGENT_STATE_IDLE: _ClassVar[AgentState]
    AGENT_STATE_PLANNING: _ClassVar[AgentState]
    AGENT_STATE_EXECUTING: _ClassVar[AgentState]
    AGENT_STATE_WAITING: _ClassVar[AgentState]
    AGENT_STATE_COMPLETED: _ClassVar[AgentState]
    AGENT_STATE_FAILED: _ClassVar[AgentState]
AGENT_STATE_UNSPECIFIED: AgentState
AGENT_STATE_IDLE: AgentState
AGENT_STATE_PLANNING: AgentState
AGENT_STATE_EXECUTING: AgentState
AGENT_STATE_WAITING: AgentState
AGENT_STATE_COMPLETED: AgentState
AGENT_STATE_FAILED: AgentState

class ExecuteTaskRequest(_message.Message):
    __slots__ = ("metadata", "query", "context", "mode", "available_tools", "config", "session_context")
    METADATA_FIELD_NUMBER: _ClassVar[int]
    QUERY_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_TOOLS_FIELD_NUMBER: _ClassVar[int]
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    SESSION_CONTEXT_FIELD_NUMBER: _ClassVar[int]
    metadata: _common_pb2.TaskMetadata
    query: str
    context: _struct_pb2.Struct
    mode: _common_pb2.ExecutionMode
    available_tools: _containers.RepeatedScalarFieldContainer[str]
    config: AgentConfig
    session_context: SessionContext
    def __init__(self, metadata: _Optional[_Union[_common_pb2.TaskMetadata, _Mapping]] = ..., query: _Optional[str] = ..., context: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., mode: _Optional[_Union[_common_pb2.ExecutionMode, str]] = ..., available_tools: _Optional[_Iterable[str]] = ..., config: _Optional[_Union[AgentConfig, _Mapping]] = ..., session_context: _Optional[_Union[SessionContext, _Mapping]] = ...) -> None: ...

class SessionContext(_message.Message):
    __slots__ = ("session_id", "history", "persistent_context", "files_created", "tools_used", "total_tokens_used", "total_cost_usd")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    HISTORY_FIELD_NUMBER: _ClassVar[int]
    PERSISTENT_CONTEXT_FIELD_NUMBER: _ClassVar[int]
    FILES_CREATED_FIELD_NUMBER: _ClassVar[int]
    TOOLS_USED_FIELD_NUMBER: _ClassVar[int]
    TOTAL_TOKENS_USED_FIELD_NUMBER: _ClassVar[int]
    TOTAL_COST_USD_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    history: _containers.RepeatedScalarFieldContainer[str]
    persistent_context: _struct_pb2.Struct
    files_created: _containers.RepeatedScalarFieldContainer[str]
    tools_used: _containers.RepeatedScalarFieldContainer[str]
    total_tokens_used: int
    total_cost_usd: float
    def __init__(self, session_id: _Optional[str] = ..., history: _Optional[_Iterable[str]] = ..., persistent_context: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., files_created: _Optional[_Iterable[str]] = ..., tools_used: _Optional[_Iterable[str]] = ..., total_tokens_used: _Optional[int] = ..., total_cost_usd: _Optional[float] = ...) -> None: ...

class ConversationMessage(_message.Message):
    __slots__ = ("role", "content", "timestamp", "tokens_used")
    ROLE_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    TIMESTAMP_FIELD_NUMBER: _ClassVar[int]
    TOKENS_USED_FIELD_NUMBER: _ClassVar[int]
    role: str
    content: str
    timestamp: _timestamp_pb2.Timestamp
    tokens_used: int
    def __init__(self, role: _Optional[str] = ..., content: _Optional[str] = ..., timestamp: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., tokens_used: _Optional[int] = ...) -> None: ...

class AgentConfig(_message.Message):
    __slots__ = ("max_iterations", "timeout_seconds", "enable_sandbox", "memory_limit_mb", "enable_learning")
    MAX_ITERATIONS_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_SECONDS_FIELD_NUMBER: _ClassVar[int]
    ENABLE_SANDBOX_FIELD_NUMBER: _ClassVar[int]
    MEMORY_LIMIT_MB_FIELD_NUMBER: _ClassVar[int]
    ENABLE_LEARNING_FIELD_NUMBER: _ClassVar[int]
    max_iterations: int
    timeout_seconds: int
    enable_sandbox: bool
    memory_limit_mb: int
    enable_learning: bool
    def __init__(self, max_iterations: _Optional[int] = ..., timeout_seconds: _Optional[int] = ..., enable_sandbox: bool = ..., memory_limit_mb: _Optional[int] = ..., enable_learning: bool = ...) -> None: ...

class ExecuteTaskResponse(_message.Message):
    __slots__ = ("task_id", "status", "result", "tool_calls", "tool_results", "metrics", "error_message", "final_state")
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    RESULT_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALLS_FIELD_NUMBER: _ClassVar[int]
    TOOL_RESULTS_FIELD_NUMBER: _ClassVar[int]
    METRICS_FIELD_NUMBER: _ClassVar[int]
    ERROR_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    FINAL_STATE_FIELD_NUMBER: _ClassVar[int]
    task_id: str
    status: _common_pb2.StatusCode
    result: str
    tool_calls: _containers.RepeatedCompositeFieldContainer[_common_pb2.ToolCall]
    tool_results: _containers.RepeatedCompositeFieldContainer[_common_pb2.ToolResult]
    metrics: _common_pb2.ExecutionMetrics
    error_message: str
    final_state: AgentState
    def __init__(self, task_id: _Optional[str] = ..., status: _Optional[_Union[_common_pb2.StatusCode, str]] = ..., result: _Optional[str] = ..., tool_calls: _Optional[_Iterable[_Union[_common_pb2.ToolCall, _Mapping]]] = ..., tool_results: _Optional[_Iterable[_Union[_common_pb2.ToolResult, _Mapping]]] = ..., metrics: _Optional[_Union[_common_pb2.ExecutionMetrics, _Mapping]] = ..., error_message: _Optional[str] = ..., final_state: _Optional[_Union[AgentState, str]] = ...) -> None: ...

class TaskUpdate(_message.Message):
    __slots__ = ("task_id", "state", "message", "tool_call", "tool_result", "progress", "delta")
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALL_FIELD_NUMBER: _ClassVar[int]
    TOOL_RESULT_FIELD_NUMBER: _ClassVar[int]
    PROGRESS_FIELD_NUMBER: _ClassVar[int]
    DELTA_FIELD_NUMBER: _ClassVar[int]
    task_id: str
    state: AgentState
    message: str
    tool_call: _common_pb2.ToolCall
    tool_result: _common_pb2.ToolResult
    progress: float
    delta: str
    def __init__(self, task_id: _Optional[str] = ..., state: _Optional[_Union[AgentState, str]] = ..., message: _Optional[str] = ..., tool_call: _Optional[_Union[_common_pb2.ToolCall, _Mapping]] = ..., tool_result: _Optional[_Union[_common_pb2.ToolResult, _Mapping]] = ..., progress: _Optional[float] = ..., delta: _Optional[str] = ...) -> None: ...

class GetCapabilitiesRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetCapabilitiesResponse(_message.Message):
    __slots__ = ("supported_tools", "supported_modes", "max_memory_mb", "max_concurrent_tasks", "version")
    SUPPORTED_TOOLS_FIELD_NUMBER: _ClassVar[int]
    SUPPORTED_MODES_FIELD_NUMBER: _ClassVar[int]
    MAX_MEMORY_MB_FIELD_NUMBER: _ClassVar[int]
    MAX_CONCURRENT_TASKS_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    supported_tools: _containers.RepeatedScalarFieldContainer[str]
    supported_modes: _containers.RepeatedScalarFieldContainer[_common_pb2.ExecutionMode]
    max_memory_mb: int
    max_concurrent_tasks: int
    version: str
    def __init__(self, supported_tools: _Optional[_Iterable[str]] = ..., supported_modes: _Optional[_Iterable[_Union[_common_pb2.ExecutionMode, str]]] = ..., max_memory_mb: _Optional[int] = ..., max_concurrent_tasks: _Optional[int] = ..., version: _Optional[str] = ...) -> None: ...

class HealthCheckRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class HealthCheckResponse(_message.Message):
    __slots__ = ("healthy", "message", "uptime_seconds", "active_tasks", "memory_usage_percent")
    HEALTHY_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    UPTIME_SECONDS_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_TASKS_FIELD_NUMBER: _ClassVar[int]
    MEMORY_USAGE_PERCENT_FIELD_NUMBER: _ClassVar[int]
    healthy: bool
    message: str
    uptime_seconds: int
    active_tasks: int
    memory_usage_percent: float
    def __init__(self, healthy: bool = ..., message: _Optional[str] = ..., uptime_seconds: _Optional[int] = ..., active_tasks: _Optional[int] = ..., memory_usage_percent: _Optional[float] = ...) -> None: ...

class DiscoverToolsRequest(_message.Message):
    __slots__ = ("query", "categories", "tags", "exclude_dangerous", "max_results")
    QUERY_FIELD_NUMBER: _ClassVar[int]
    CATEGORIES_FIELD_NUMBER: _ClassVar[int]
    TAGS_FIELD_NUMBER: _ClassVar[int]
    EXCLUDE_DANGEROUS_FIELD_NUMBER: _ClassVar[int]
    MAX_RESULTS_FIELD_NUMBER: _ClassVar[int]
    query: str
    categories: _containers.RepeatedScalarFieldContainer[str]
    tags: _containers.RepeatedScalarFieldContainer[str]
    exclude_dangerous: bool
    max_results: int
    def __init__(self, query: _Optional[str] = ..., categories: _Optional[_Iterable[str]] = ..., tags: _Optional[_Iterable[str]] = ..., exclude_dangerous: bool = ..., max_results: _Optional[int] = ...) -> None: ...

class DiscoverToolsResponse(_message.Message):
    __slots__ = ("tools",)
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    tools: _containers.RepeatedCompositeFieldContainer[ToolCapability]
    def __init__(self, tools: _Optional[_Iterable[_Union[ToolCapability, _Mapping]]] = ...) -> None: ...

class GetToolCapabilityRequest(_message.Message):
    __slots__ = ("tool_id",)
    TOOL_ID_FIELD_NUMBER: _ClassVar[int]
    tool_id: str
    def __init__(self, tool_id: _Optional[str] = ...) -> None: ...

class GetToolCapabilityResponse(_message.Message):
    __slots__ = ("tool",)
    TOOL_FIELD_NUMBER: _ClassVar[int]
    tool: ToolCapability
    def __init__(self, tool: _Optional[_Union[ToolCapability, _Mapping]] = ...) -> None: ...

class ToolCapability(_message.Message):
    __slots__ = ("id", "name", "description", "category", "input_schema", "output_schema", "required_permissions", "estimated_duration_ms", "is_dangerous", "version", "author", "tags", "examples", "rate_limit", "cache_ttl_ms")
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    CATEGORY_FIELD_NUMBER: _ClassVar[int]
    INPUT_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    REQUIRED_PERMISSIONS_FIELD_NUMBER: _ClassVar[int]
    ESTIMATED_DURATION_MS_FIELD_NUMBER: _ClassVar[int]
    IS_DANGEROUS_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    AUTHOR_FIELD_NUMBER: _ClassVar[int]
    TAGS_FIELD_NUMBER: _ClassVar[int]
    EXAMPLES_FIELD_NUMBER: _ClassVar[int]
    RATE_LIMIT_FIELD_NUMBER: _ClassVar[int]
    CACHE_TTL_MS_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    description: str
    category: str
    input_schema: _struct_pb2.Struct
    output_schema: _struct_pb2.Struct
    required_permissions: _containers.RepeatedScalarFieldContainer[str]
    estimated_duration_ms: int
    is_dangerous: bool
    version: str
    author: str
    tags: _containers.RepeatedScalarFieldContainer[str]
    examples: _containers.RepeatedCompositeFieldContainer[ToolExample]
    rate_limit: RateLimit
    cache_ttl_ms: int
    def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., description: _Optional[str] = ..., category: _Optional[str] = ..., input_schema: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., output_schema: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., required_permissions: _Optional[_Iterable[str]] = ..., estimated_duration_ms: _Optional[int] = ..., is_dangerous: bool = ..., version: _Optional[str] = ..., author: _Optional[str] = ..., tags: _Optional[_Iterable[str]] = ..., examples: _Optional[_Iterable[_Union[ToolExample, _Mapping]]] = ..., rate_limit: _Optional[_Union[RateLimit, _Mapping]] = ..., cache_ttl_ms: _Optional[int] = ...) -> None: ...

class ToolExample(_message.Message):
    __slots__ = ("description", "input", "output")
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    description: str
    input: _struct_pb2.Struct
    output: _struct_pb2.Struct
    def __init__(self, description: _Optional[str] = ..., input: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., output: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class RateLimit(_message.Message):
    __slots__ = ("requests_per_minute", "requests_per_hour")
    REQUESTS_PER_MINUTE_FIELD_NUMBER: _ClassVar[int]
    REQUESTS_PER_HOUR_FIELD_NUMBER: _ClassVar[int]
    requests_per_minute: int
    requests_per_hour: int
    def __init__(self, requests_per_minute: _Optional[int] = ..., requests_per_hour: _Optional[int] = ...) -> None: ...
