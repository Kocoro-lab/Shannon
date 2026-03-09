from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class StatusCode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    STATUS_CODE_UNSPECIFIED: _ClassVar[StatusCode]
    STATUS_CODE_OK: _ClassVar[StatusCode]
    STATUS_CODE_ERROR: _ClassVar[StatusCode]
    STATUS_CODE_TIMEOUT: _ClassVar[StatusCode]
    STATUS_CODE_RATE_LIMITED: _ClassVar[StatusCode]
    STATUS_CODE_BUDGET_EXCEEDED: _ClassVar[StatusCode]

class ExecutionMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    EXECUTION_MODE_UNSPECIFIED: _ClassVar[ExecutionMode]
    EXECUTION_MODE_SIMPLE: _ClassVar[ExecutionMode]
    EXECUTION_MODE_STANDARD: _ClassVar[ExecutionMode]
    EXECUTION_MODE_COMPLEX: _ClassVar[ExecutionMode]

class ModelTier(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    MODEL_TIER_UNSPECIFIED: _ClassVar[ModelTier]
    MODEL_TIER_SMALL: _ClassVar[ModelTier]
    MODEL_TIER_MEDIUM: _ClassVar[ModelTier]
    MODEL_TIER_LARGE: _ClassVar[ModelTier]

class CognitiveStrategy(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    COGNITIVE_UNSPECIFIED: _ClassVar[CognitiveStrategy]
    COGNITIVE_DIRECT: _ClassVar[CognitiveStrategy]
    COGNITIVE_DECOMPOSE: _ClassVar[CognitiveStrategy]
    COGNITIVE_EXPLORATORY: _ClassVar[CognitiveStrategy]
    COGNITIVE_REACT: _ClassVar[CognitiveStrategy]
    COGNITIVE_RESEARCH: _ClassVar[CognitiveStrategy]
    COGNITIVE_SCIENTIFIC: _ClassVar[CognitiveStrategy]
STATUS_CODE_UNSPECIFIED: StatusCode
STATUS_CODE_OK: StatusCode
STATUS_CODE_ERROR: StatusCode
STATUS_CODE_TIMEOUT: StatusCode
STATUS_CODE_RATE_LIMITED: StatusCode
STATUS_CODE_BUDGET_EXCEEDED: StatusCode
EXECUTION_MODE_UNSPECIFIED: ExecutionMode
EXECUTION_MODE_SIMPLE: ExecutionMode
EXECUTION_MODE_STANDARD: ExecutionMode
EXECUTION_MODE_COMPLEX: ExecutionMode
MODEL_TIER_UNSPECIFIED: ModelTier
MODEL_TIER_SMALL: ModelTier
MODEL_TIER_MEDIUM: ModelTier
MODEL_TIER_LARGE: ModelTier
COGNITIVE_UNSPECIFIED: CognitiveStrategy
COGNITIVE_DIRECT: CognitiveStrategy
COGNITIVE_DECOMPOSE: CognitiveStrategy
COGNITIVE_EXPLORATORY: CognitiveStrategy
COGNITIVE_REACT: CognitiveStrategy
COGNITIVE_RESEARCH: CognitiveStrategy
COGNITIVE_SCIENTIFIC: CognitiveStrategy

class TaskMetadata(_message.Message):
    __slots__ = ("task_id", "user_id", "session_id", "tenant_id", "created_at", "labels", "max_agents", "token_budget")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    TENANT_ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    MAX_AGENTS_FIELD_NUMBER: _ClassVar[int]
    TOKEN_BUDGET_FIELD_NUMBER: _ClassVar[int]
    task_id: str
    user_id: str
    session_id: str
    tenant_id: str
    created_at: _timestamp_pb2.Timestamp
    labels: _containers.ScalarMap[str, str]
    max_agents: int
    token_budget: float
    def __init__(self, task_id: _Optional[str] = ..., user_id: _Optional[str] = ..., session_id: _Optional[str] = ..., tenant_id: _Optional[str] = ..., created_at: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ..., max_agents: _Optional[int] = ..., token_budget: _Optional[float] = ...) -> None: ...

class TokenUsage(_message.Message):
    __slots__ = ("prompt_tokens", "completion_tokens", "total_tokens", "cost_usd", "model", "tier", "provider")
    PROMPT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COMPLETION_TOKENS_FIELD_NUMBER: _ClassVar[int]
    TOTAL_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COST_USD_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    TIER_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    prompt_tokens: int
    completion_tokens: int
    total_tokens: int
    cost_usd: float
    model: str
    tier: ModelTier
    provider: str
    def __init__(self, prompt_tokens: _Optional[int] = ..., completion_tokens: _Optional[int] = ..., total_tokens: _Optional[int] = ..., cost_usd: _Optional[float] = ..., model: _Optional[str] = ..., tier: _Optional[_Union[ModelTier, str]] = ..., provider: _Optional[str] = ...) -> None: ...

class ExecutionMetrics(_message.Message):
    __slots__ = ("latency_ms", "token_usage", "cache_hit", "cache_score", "agents_used", "mode")
    LATENCY_MS_FIELD_NUMBER: _ClassVar[int]
    TOKEN_USAGE_FIELD_NUMBER: _ClassVar[int]
    CACHE_HIT_FIELD_NUMBER: _ClassVar[int]
    CACHE_SCORE_FIELD_NUMBER: _ClassVar[int]
    AGENTS_USED_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    latency_ms: int
    token_usage: TokenUsage
    cache_hit: bool
    cache_score: float
    agents_used: int
    mode: ExecutionMode
    def __init__(self, latency_ms: _Optional[int] = ..., token_usage: _Optional[_Union[TokenUsage, _Mapping]] = ..., cache_hit: bool = ..., cache_score: _Optional[float] = ..., agents_used: _Optional[int] = ..., mode: _Optional[_Union[ExecutionMode, str]] = ...) -> None: ...

class ToolCall(_message.Message):
    __slots__ = ("name", "parameters", "tool_id")
    NAME_FIELD_NUMBER: _ClassVar[int]
    PARAMETERS_FIELD_NUMBER: _ClassVar[int]
    TOOL_ID_FIELD_NUMBER: _ClassVar[int]
    name: str
    parameters: _struct_pb2.Struct
    tool_id: str
    def __init__(self, name: _Optional[str] = ..., parameters: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., tool_id: _Optional[str] = ...) -> None: ...

class ToolResult(_message.Message):
    __slots__ = ("tool_id", "output", "status", "error_message", "execution_time_ms")
    TOOL_ID_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    ERROR_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_TIME_MS_FIELD_NUMBER: _ClassVar[int]
    tool_id: str
    output: _struct_pb2.Value
    status: StatusCode
    error_message: str
    execution_time_ms: int
    def __init__(self, tool_id: _Optional[str] = ..., output: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., status: _Optional[_Union[StatusCode, str]] = ..., error_message: _Optional[str] = ..., execution_time_ms: _Optional[int] = ...) -> None: ...
