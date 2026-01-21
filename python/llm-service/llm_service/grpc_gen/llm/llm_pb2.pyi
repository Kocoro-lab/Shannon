from google.protobuf import struct_pb2 as _struct_pb2
from common import common_pb2 as _common_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Provider(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PROVIDER_UNSPECIFIED: _ClassVar[Provider]
    PROVIDER_OPENAI: _ClassVar[Provider]
    PROVIDER_ANTHROPIC: _ClassVar[Provider]
    PROVIDER_GOOGLE: _ClassVar[Provider]
    PROVIDER_DEEPSEEK: _ClassVar[Provider]
    PROVIDER_QWEN: _ClassVar[Provider]
    PROVIDER_BEDROCK: _ClassVar[Provider]
    PROVIDER_OLLAMA: _ClassVar[Provider]
    PROVIDER_VLLM: _ClassVar[Provider]
PROVIDER_UNSPECIFIED: Provider
PROVIDER_OPENAI: Provider
PROVIDER_ANTHROPIC: Provider
PROVIDER_GOOGLE: Provider
PROVIDER_DEEPSEEK: Provider
PROVIDER_QWEN: Provider
PROVIDER_BEDROCK: Provider
PROVIDER_OLLAMA: Provider
PROVIDER_VLLM: Provider

class Message(_message.Message):
    __slots__ = ("role", "content", "tool_calls", "name")
    ROLE_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALLS_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    role: str
    content: str
    tool_calls: _containers.RepeatedCompositeFieldContainer[_common_pb2.ToolCall]
    name: str
    def __init__(self, role: _Optional[str] = ..., content: _Optional[str] = ..., tool_calls: _Optional[_Iterable[_Union[_common_pb2.ToolCall, _Mapping]]] = ..., name: _Optional[str] = ...) -> None: ...

class GenerateCompletionRequest(_message.Message):
    __slots__ = ("messages", "tier", "specific_model", "config", "metadata", "available_tools")
    MESSAGES_FIELD_NUMBER: _ClassVar[int]
    TIER_FIELD_NUMBER: _ClassVar[int]
    SPECIFIC_MODEL_FIELD_NUMBER: _ClassVar[int]
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_TOOLS_FIELD_NUMBER: _ClassVar[int]
    messages: _containers.RepeatedCompositeFieldContainer[Message]
    tier: _common_pb2.ModelTier
    specific_model: str
    config: GenerationConfig
    metadata: _common_pb2.TaskMetadata
    available_tools: _containers.RepeatedCompositeFieldContainer[ToolDefinition]
    def __init__(self, messages: _Optional[_Iterable[_Union[Message, _Mapping]]] = ..., tier: _Optional[_Union[_common_pb2.ModelTier, str]] = ..., specific_model: _Optional[str] = ..., config: _Optional[_Union[GenerationConfig, _Mapping]] = ..., metadata: _Optional[_Union[_common_pb2.TaskMetadata, _Mapping]] = ..., available_tools: _Optional[_Iterable[_Union[ToolDefinition, _Mapping]]] = ...) -> None: ...

class GenerationConfig(_message.Message):
    __slots__ = ("temperature", "top_p", "max_tokens", "stop_sequences", "presence_penalty", "frequency_penalty", "enable_caching", "cache_key")
    TEMPERATURE_FIELD_NUMBER: _ClassVar[int]
    TOP_P_FIELD_NUMBER: _ClassVar[int]
    MAX_TOKENS_FIELD_NUMBER: _ClassVar[int]
    STOP_SEQUENCES_FIELD_NUMBER: _ClassVar[int]
    PRESENCE_PENALTY_FIELD_NUMBER: _ClassVar[int]
    FREQUENCY_PENALTY_FIELD_NUMBER: _ClassVar[int]
    ENABLE_CACHING_FIELD_NUMBER: _ClassVar[int]
    CACHE_KEY_FIELD_NUMBER: _ClassVar[int]
    temperature: float
    top_p: float
    max_tokens: int
    stop_sequences: _containers.RepeatedScalarFieldContainer[str]
    presence_penalty: float
    frequency_penalty: float
    enable_caching: bool
    cache_key: str
    def __init__(self, temperature: _Optional[float] = ..., top_p: _Optional[float] = ..., max_tokens: _Optional[int] = ..., stop_sequences: _Optional[_Iterable[str]] = ..., presence_penalty: _Optional[float] = ..., frequency_penalty: _Optional[float] = ..., enable_caching: bool = ..., cache_key: _Optional[str] = ...) -> None: ...

class ToolDefinition(_message.Message):
    __slots__ = ("name", "description", "parameters_schema", "requires_confirmation")
    NAME_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    PARAMETERS_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    REQUIRES_CONFIRMATION_FIELD_NUMBER: _ClassVar[int]
    name: str
    description: str
    parameters_schema: _struct_pb2.Struct
    requires_confirmation: bool
    def __init__(self, name: _Optional[str] = ..., description: _Optional[str] = ..., parameters_schema: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., requires_confirmation: bool = ...) -> None: ...

class GenerateCompletionResponse(_message.Message):
    __slots__ = ("completion", "tool_calls", "usage", "model_used", "provider", "cache_hit", "finish_reason")
    COMPLETION_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALLS_FIELD_NUMBER: _ClassVar[int]
    USAGE_FIELD_NUMBER: _ClassVar[int]
    MODEL_USED_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    CACHE_HIT_FIELD_NUMBER: _ClassVar[int]
    FINISH_REASON_FIELD_NUMBER: _ClassVar[int]
    completion: str
    tool_calls: _containers.RepeatedCompositeFieldContainer[_common_pb2.ToolCall]
    usage: _common_pb2.TokenUsage
    model_used: str
    provider: Provider
    cache_hit: bool
    finish_reason: str
    def __init__(self, completion: _Optional[str] = ..., tool_calls: _Optional[_Iterable[_Union[_common_pb2.ToolCall, _Mapping]]] = ..., usage: _Optional[_Union[_common_pb2.TokenUsage, _Mapping]] = ..., model_used: _Optional[str] = ..., provider: _Optional[_Union[Provider, str]] = ..., cache_hit: bool = ..., finish_reason: _Optional[str] = ...) -> None: ...

class CompletionChunk(_message.Message):
    __slots__ = ("delta", "tool_call_delta", "finish_reason", "usage")
    DELTA_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALL_DELTA_FIELD_NUMBER: _ClassVar[int]
    FINISH_REASON_FIELD_NUMBER: _ClassVar[int]
    USAGE_FIELD_NUMBER: _ClassVar[int]
    delta: str
    tool_call_delta: _common_pb2.ToolCall
    finish_reason: str
    usage: _common_pb2.TokenUsage
    def __init__(self, delta: _Optional[str] = ..., tool_call_delta: _Optional[_Union[_common_pb2.ToolCall, _Mapping]] = ..., finish_reason: _Optional[str] = ..., usage: _Optional[_Union[_common_pb2.TokenUsage, _Mapping]] = ...) -> None: ...

class EmbedTextRequest(_message.Message):
    __slots__ = ("texts", "model")
    TEXTS_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    texts: _containers.RepeatedScalarFieldContainer[str]
    model: str
    def __init__(self, texts: _Optional[_Iterable[str]] = ..., model: _Optional[str] = ...) -> None: ...

class EmbedTextResponse(_message.Message):
    __slots__ = ("embeddings", "dimensions", "model_used")
    EMBEDDINGS_FIELD_NUMBER: _ClassVar[int]
    DIMENSIONS_FIELD_NUMBER: _ClassVar[int]
    MODEL_USED_FIELD_NUMBER: _ClassVar[int]
    embeddings: _containers.RepeatedCompositeFieldContainer[Embedding]
    dimensions: int
    model_used: str
    def __init__(self, embeddings: _Optional[_Iterable[_Union[Embedding, _Mapping]]] = ..., dimensions: _Optional[int] = ..., model_used: _Optional[str] = ...) -> None: ...

class Embedding(_message.Message):
    __slots__ = ("vector", "text")
    VECTOR_FIELD_NUMBER: _ClassVar[int]
    TEXT_FIELD_NUMBER: _ClassVar[int]
    vector: _containers.RepeatedScalarFieldContainer[float]
    text: str
    def __init__(self, vector: _Optional[_Iterable[float]] = ..., text: _Optional[str] = ...) -> None: ...

class AnalyzeComplexityRequest(_message.Message):
    __slots__ = ("query", "context", "available_tools")
    QUERY_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_TOOLS_FIELD_NUMBER: _ClassVar[int]
    query: str
    context: _struct_pb2.Struct
    available_tools: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, query: _Optional[str] = ..., context: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., available_tools: _Optional[_Iterable[str]] = ...) -> None: ...

class AnalyzeComplexityResponse(_message.Message):
    __slots__ = ("recommended_mode", "complexity_score", "required_capabilities", "estimated_agents", "estimated_tokens", "estimated_cost_usd", "reasoning")
    RECOMMENDED_MODE_FIELD_NUMBER: _ClassVar[int]
    COMPLEXITY_SCORE_FIELD_NUMBER: _ClassVar[int]
    REQUIRED_CAPABILITIES_FIELD_NUMBER: _ClassVar[int]
    ESTIMATED_AGENTS_FIELD_NUMBER: _ClassVar[int]
    ESTIMATED_TOKENS_FIELD_NUMBER: _ClassVar[int]
    ESTIMATED_COST_USD_FIELD_NUMBER: _ClassVar[int]
    REASONING_FIELD_NUMBER: _ClassVar[int]
    recommended_mode: _common_pb2.ExecutionMode
    complexity_score: float
    required_capabilities: _containers.RepeatedScalarFieldContainer[str]
    estimated_agents: int
    estimated_tokens: int
    estimated_cost_usd: float
    reasoning: str
    def __init__(self, recommended_mode: _Optional[_Union[_common_pb2.ExecutionMode, str]] = ..., complexity_score: _Optional[float] = ..., required_capabilities: _Optional[_Iterable[str]] = ..., estimated_agents: _Optional[int] = ..., estimated_tokens: _Optional[int] = ..., estimated_cost_usd: _Optional[float] = ..., reasoning: _Optional[str] = ...) -> None: ...

class ListModelsRequest(_message.Message):
    __slots__ = ("tier", "provider")
    TIER_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    tier: _common_pb2.ModelTier
    provider: Provider
    def __init__(self, tier: _Optional[_Union[_common_pb2.ModelTier, str]] = ..., provider: _Optional[_Union[Provider, str]] = ...) -> None: ...

class ListModelsResponse(_message.Message):
    __slots__ = ("models",)
    MODELS_FIELD_NUMBER: _ClassVar[int]
    models: _containers.RepeatedCompositeFieldContainer[ModelInfo]
    def __init__(self, models: _Optional[_Iterable[_Union[ModelInfo, _Mapping]]] = ...) -> None: ...

class ModelInfo(_message.Message):
    __slots__ = ("id", "name", "provider", "tier", "context_window", "cost_per_1k_prompt_tokens", "cost_per_1k_completion_tokens", "supports_tools", "supports_streaming", "available")
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    TIER_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_WINDOW_FIELD_NUMBER: _ClassVar[int]
    COST_PER_1K_PROMPT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COST_PER_1K_COMPLETION_TOKENS_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_TOOLS_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_STREAMING_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    provider: Provider
    tier: _common_pb2.ModelTier
    context_window: int
    cost_per_1k_prompt_tokens: float
    cost_per_1k_completion_tokens: float
    supports_tools: bool
    supports_streaming: bool
    available: bool
    def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., provider: _Optional[_Union[Provider, str]] = ..., tier: _Optional[_Union[_common_pb2.ModelTier, str]] = ..., context_window: _Optional[int] = ..., cost_per_1k_prompt_tokens: _Optional[float] = ..., cost_per_1k_completion_tokens: _Optional[float] = ..., supports_tools: bool = ..., supports_streaming: bool = ..., available: bool = ...) -> None: ...
