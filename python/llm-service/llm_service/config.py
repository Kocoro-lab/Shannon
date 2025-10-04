from typing import Optional
from pydantic_settings import BaseSettings
from pydantic import Field


class Settings(BaseSettings):
    """Application settings"""

    # Service configuration
    debug: bool = Field(default=False, env="DEBUG")
    service_name: str = Field(default="shannon-llm-service", env="SERVICE_NAME")

    # Redis configuration
    redis_host: str = Field(default="redis", env="REDIS_HOST")
    redis_port: int = Field(default=6379, env="REDIS_PORT")
    redis_password: Optional[str] = Field(default=None, env="REDIS_PASSWORD")
    redis_ttl_seconds: int = Field(default=3600, env="REDIS_TTL_SECONDS")

    # PostgreSQL configuration
    postgres_host: str = Field(default="postgres", env="POSTGRES_HOST")
    postgres_port: int = Field(default=5432, env="POSTGRES_PORT")
    postgres_db: str = Field(default="shannon", env="POSTGRES_DB")
    postgres_user: str = Field(default="shannon", env="POSTGRES_USER")
    postgres_password: str = Field(default="shannon", env="POSTGRES_PASSWORD")

    # LLM Provider API keys (optional, can be configured at runtime)
    openai_api_key: Optional[str] = Field(default=None, env="OPENAI_API_KEY")
    anthropic_api_key: Optional[str] = Field(default=None, env="ANTHROPIC_API_KEY")
    google_api_key: Optional[str] = Field(default=None, env="GOOGLE_API_KEY")
    deepseek_api_key: Optional[str] = Field(default=None, env="DEEPSEEK_API_KEY")
    qwen_api_key: Optional[str] = Field(default=None, env="QWEN_API_KEY")
    mistral_api_key: Optional[str] = Field(default=None, env="MISTRAL_API_KEY")
    aws_access_key: Optional[str] = Field(default=None, env="AWS_ACCESS_KEY_ID")
    aws_secret_key: Optional[str] = Field(default=None, env="AWS_SECRET_ACCESS_KEY")
    aws_region: str = Field(default="us-east-1", env="AWS_REGION")

    # Model configuration
    default_model_tier: str = Field(default="small", env="DEFAULT_MODEL_TIER")
    max_tokens: int = Field(default=2000, env="MAX_TOKENS")
    temperature: float = Field(default=0.7, env="TEMPERATURE")
    # Dedicated models for workflow stages (optional overrides)
    # If set, these take precedence over tier-based selection
    complexity_model_id: Optional[str] = Field(default=None, env="COMPLEXITY_MODEL_ID")
    decomposition_model_id: Optional[str] = Field(
        default=None, env="DECOMPOSITION_MODEL_ID"
    )

    # Cache configuration
    enable_cache: bool = Field(default=True, env="ENABLE_CACHE")
    cache_similarity_threshold: float = Field(
        default=0.95, env="CACHE_SIMILARITY_THRESHOLD"
    )

    # Rate limiting
    rate_limit_requests: int = Field(default=100, env="RATE_LIMIT_REQUESTS")
    rate_limit_window: int = Field(default=60, env="RATE_LIMIT_WINDOW")

    # Tool-specific rate limits (per minute)
    web_search_rate_limit: int = Field(default=60, env="WEB_SEARCH_RATE_LIMIT")
    calculator_rate_limit: int = Field(default=1000, env="CALCULATOR_RATE_LIMIT")
    python_executor_rate_limit: int = Field(
        default=30, env="PYTHON_EXECUTOR_RATE_LIMIT"
    )

    # Token budget management
    max_tokens_per_request: int = Field(default=4000, env="MAX_TOKENS_PER_REQUEST")
    max_cost_per_request: float = Field(default=0.10, env="MAX_COST_PER_REQUEST")

    # Event emission to orchestrator
    enable_llm_events: bool = Field(default=True, env="ENABLE_LLM_EVENTS")
    enable_llm_partials: bool = Field(default=True, env="ENABLE_LLM_PARTIALS")
    partial_chunk_chars: int = Field(default=512, env="PARTIAL_CHUNK_CHARS")
    events_ingest_url: str = Field(
        default="http://orchestrator:8081/events", env="EVENTS_INGEST_URL"
    )
    events_auth_token: Optional[str] = Field(default=None, env="EVENTS_AUTH_TOKEN")

    class Config:
        env_file = ".env"
        case_sensitive = False

    @property
    def database_url(self) -> str:
        """Get PostgreSQL database URL"""
        return f"postgresql+asyncpg://{self.postgres_user}:{self.postgres_password}@{self.postgres_host}:{self.postgres_port}/{self.postgres_db}"

    @property
    def redis_url(self) -> str:
        """Get Redis URL"""
        if self.redis_password:
            return f"redis://:{self.redis_password}@{self.redis_host}:{self.redis_port}"
        return f"redis://{self.redis_host}:{self.redis_port}"
