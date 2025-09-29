package config

import (
	"fmt"
	"time"

	"go.uber.org/zap"
)

// ShannonConfig represents the main Shannon orchestrator configuration
type ShannonConfig struct {
	// Service configuration
	Service ServiceConfig `json:"service" yaml:"service"`

	// Authentication configuration
	Auth AuthConfig `json:"auth" yaml:"auth"`

	// Circuit breaker configurations
	CircuitBreakers CircuitBreakersConfig `json:"circuit_breakers" yaml:"circuit_breakers"`

	// Degradation system configuration
	Degradation DegradationConfig `json:"degradation" yaml:"degradation"`

	// Health check configuration
	Health HealthConfig `json:"health" yaml:"health"`

	// Agent management configuration
	Agents AgentsConfig `json:"agents" yaml:"agents"`

	// Temporal workflow configuration
	Temporal TemporalConfig `json:"temporal" yaml:"temporal"`

	// Logging configuration
	Logging LoggingConfig `json:"logging" yaml:"logging"`

	// Policy engine configuration
	Policy PolicyConfig `json:"policy" yaml:"policy"`

	// Vector/Embedding configuration
	Vector VectorConfig `json:"vector" yaml:"vector"`

	// Embeddings service configuration
	Embeddings EmbeddingsConfig `json:"embeddings" yaml:"embeddings"`

	// Tracing configuration
	Tracing TracingConfig `json:"tracing" yaml:"tracing"`

	// Streaming configuration (v1)
	Streaming StreamingConfig `json:"streaming" yaml:"streaming"`

	// Workflow orchestration behavior (evaluation/synthesis toggles)
	Workflow WorkflowConfig `json:"workflow" yaml:"workflow"`

	// Session management configuration
	Session SessionConfig `json:"session" yaml:"session"`
}

// AuthConfig contains authentication configuration
type AuthConfig struct {
	Enabled                  bool          `json:"enabled" yaml:"enabled"`
	SkipAuth                 bool          `json:"skip_auth" yaml:"skip_auth"` // Development mode
	JWTSecret                string        `json:"jwt_secret" yaml:"jwt_secret"`
	AccessTokenExpiry        time.Duration `json:"access_token_expiry" yaml:"access_token_expiry"`
	RefreshTokenExpiry       time.Duration `json:"refresh_token_expiry" yaml:"refresh_token_expiry"`
	APIKeyRateLimit          int           `json:"api_key_rate_limit" yaml:"api_key_rate_limit"`
	DefaultTenantLimit       int           `json:"default_tenant_limit" yaml:"default_tenant_limit"`
	EnableRegistration       bool          `json:"enable_registration" yaml:"enable_registration"`
	RequireEmailVerification bool          `json:"require_email_verification" yaml:"require_email_verification"`
}

// ServiceConfig contains basic service configuration
type ServiceConfig struct {
	Port            int           `json:"port" yaml:"port"`
	HealthPort      int           `json:"health_port" yaml:"health_port"`
	GracefulTimeout time.Duration `json:"graceful_timeout" yaml:"graceful_timeout"`
	ReadTimeout     time.Duration `json:"read_timeout" yaml:"read_timeout"`
	WriteTimeout    time.Duration `json:"write_timeout" yaml:"write_timeout"`
	MaxHeaderBytes  int           `json:"max_header_bytes" yaml:"max_header_bytes"`
}

// CircuitBreakersConfig contains all circuit breaker configurations
type CircuitBreakersConfig struct {
	Redis    CircuitBreakerConfig `json:"redis" yaml:"redis"`
	Database CircuitBreakerConfig `json:"database" yaml:"database"`
	GRPC     CircuitBreakerConfig `json:"grpc" yaml:"grpc"`
}

// CircuitBreakerConfig represents circuit breaker settings
type CircuitBreakerConfig struct {
	MaxRequests   uint32        `json:"max_requests" yaml:"max_requests"`
	Interval      time.Duration `json:"interval" yaml:"interval"`
	Timeout       time.Duration `json:"timeout" yaml:"timeout"`
	MaxFailures   uint32        `json:"max_failures" yaml:"max_failures"`
	OnStateChange bool          `json:"on_state_change" yaml:"on_state_change"`
	Enabled       bool          `json:"enabled" yaml:"enabled"`
}

// DegradationConfig contains degradation system settings
type DegradationConfig struct {
	Enabled           bool                  `json:"enabled" yaml:"enabled"`
	CheckInterval     time.Duration         `json:"check_interval" yaml:"check_interval"`
	ThresholdConfig   DegradationThresholds `json:"thresholds" yaml:"thresholds"`
	ModeDowngrade     ModeDowngradeConfig   `json:"mode_downgrade" yaml:"mode_downgrade"`
	PartialResults    PartialResultsConfig  `json:"partial_results" yaml:"partial_results"`
	FallbackBehaviors map[string]string     `json:"fallback_behaviors" yaml:"fallback_behaviors"`
}

// DegradationThresholds defines when to enter different degradation levels
type DegradationThresholds struct {
	Minor    DegradationLevelThreshold `json:"minor" yaml:"minor"`
	Moderate DegradationLevelThreshold `json:"moderate" yaml:"moderate"`
	Severe   DegradationLevelThreshold `json:"severe" yaml:"severe"`
}

// DegradationLevelThreshold defines conditions for a degradation level
type DegradationLevelThreshold struct {
	CircuitBreakersOpen         int     `json:"circuit_breakers_open" yaml:"circuit_breakers_open"`
	CriticalDependenciesDown    int     `json:"critical_dependencies_down" yaml:"critical_dependencies_down"`
	NonCriticalDependenciesDown int     `json:"non_critical_dependencies_down" yaml:"non_critical_dependencies_down"`
	ErrorRatePercentage         float64 `json:"error_rate_percentage" yaml:"error_rate_percentage"`
	LatencyThresholdMs          int64   `json:"latency_threshold_ms" yaml:"latency_threshold_ms"`
}

// ModeDowngradeConfig contains mode downgrade settings
type ModeDowngradeConfig struct {
	Enabled                  bool                          `json:"enabled" yaml:"enabled"`
	MinorDegradationRules    ModeDowngradeRules            `json:"minor_degradation_rules" yaml:"minor_degradation_rules"`
	ModerateDegradationRules ModeDowngradeRules            `json:"moderate_degradation_rules" yaml:"moderate_degradation_rules"`
	SevereDegradationRules   ModeDowngradeRules            `json:"severe_degradation_rules" yaml:"severe_degradation_rules"`
	CustomRules              map[string]ModeDowngradeRules `json:"custom_rules" yaml:"custom_rules"`
}

// ModeDowngradeRules defines how modes should be downgraded
type ModeDowngradeRules struct {
	ComplexToStandard bool `json:"complex_to_standard" yaml:"complex_to_standard"`
	StandardToSimple  bool `json:"standard_to_simple" yaml:"standard_to_simple"`
	ComplexToSimple   bool `json:"complex_to_simple" yaml:"complex_to_simple"`
	ForceSimpleMode   bool `json:"force_simple_mode" yaml:"force_simple_mode"`
}

// PartialResultsConfig contains partial results settings
type PartialResultsConfig struct {
	Enabled           bool                       `json:"enabled" yaml:"enabled"`
	SuccessThresholds map[string]float64         `json:"success_thresholds" yaml:"success_thresholds"`
	TimeoutOverride   time.Duration              `json:"timeout_override" yaml:"timeout_override"`
	MaxWaitTime       time.Duration              `json:"max_wait_time" yaml:"max_wait_time"`
	AggregationRules  map[string]AggregationRule `json:"aggregation_rules" yaml:"aggregation_rules"`
}

// AggregationRule defines how to aggregate partial results
type AggregationRule struct {
	MinimumResults   int     `json:"minimum_results" yaml:"minimum_results"`
	SuccessThreshold float64 `json:"success_threshold" yaml:"success_threshold"`
	Strategy         string  `json:"strategy" yaml:"strategy"` // "first_success", "best_effort", "consensus"
}

// HealthConfig contains health check settings
type HealthConfig struct {
	Enabled       bool          `json:"enabled" yaml:"enabled"`
	CheckInterval time.Duration `json:"check_interval" yaml:"check_interval"`
	Timeout       time.Duration `json:"timeout" yaml:"timeout"`
	Port          int           `json:"port" yaml:"port"`

	// Individual check configurations
	Checks map[string]HealthCheckConfig `json:"checks" yaml:"checks"`
}

// HealthCheckConfig represents individual health check settings
type HealthCheckConfig struct {
	Enabled  bool          `json:"enabled" yaml:"enabled"`
	Critical bool          `json:"critical" yaml:"critical"`
	Timeout  time.Duration `json:"timeout" yaml:"timeout"`
	Interval time.Duration `json:"interval" yaml:"interval"`
}

// AgentsConfig contains agent management settings
type AgentsConfig struct {
	MaxConcurrent   int           `json:"max_concurrent" yaml:"max_concurrent"`
	DefaultTimeout  time.Duration `json:"default_timeout" yaml:"default_timeout"`
	HealthCheckPort int           `json:"health_check_port" yaml:"health_check_port"`
	RetryCount      int           `json:"retry_count" yaml:"retry_count"`
	RetryBackoff    time.Duration `json:"retry_backoff" yaml:"retry_backoff"`

	// Service endpoints
	AgentCoreEndpoint  string `json:"agent_core_endpoint" yaml:"agent_core_endpoint"`
	LLMServiceEndpoint string `json:"llm_service_endpoint" yaml:"llm_service_endpoint"`
}

// TemporalConfig contains Temporal workflow settings
type TemporalConfig struct {
	HostPort    string            `json:"host_port" yaml:"host_port"`
	Namespace   string            `json:"namespace" yaml:"namespace"`
	TaskQueue   string            `json:"task_queue" yaml:"task_queue"`
	Timeout     time.Duration     `json:"timeout" yaml:"timeout"`
	RetryPolicy RetryPolicyConfig `json:"retry_policy" yaml:"retry_policy"`
}

// RetryPolicyConfig contains retry policy settings
type RetryPolicyConfig struct {
	InitialInterval    time.Duration `json:"initial_interval" yaml:"initial_interval"`
	BackoffCoefficient float64       `json:"backoff_coefficient" yaml:"backoff_coefficient"`
	MaximumInterval    time.Duration `json:"maximum_interval" yaml:"maximum_interval"`
	MaximumAttempts    int32         `json:"maximum_attempts" yaml:"maximum_attempts"`
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	Level       string `json:"level" yaml:"level"`
	Development bool   `json:"development" yaml:"development"`
	Encoding    string `json:"encoding" yaml:"encoding"` // "json" or "console"

	// Log output configuration
	OutputPaths      []string `json:"output_paths" yaml:"output_paths"`
	ErrorOutputPaths []string `json:"error_output_paths" yaml:"error_output_paths"`
}

// VectorConfig contains vector DB and embedding settings
type VectorConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
	// Qdrant
	Host           string        `json:"host" yaml:"host"`
	Port           int           `json:"port" yaml:"port"`
	TaskEmbeddings string        `json:"task_embeddings" yaml:"task_embeddings"`
	ToolResults    string        `json:"tool_results" yaml:"tool_results"`
	Cases          string        `json:"cases" yaml:"cases"`
	DocumentChunks string        `json:"document_chunks" yaml:"document_chunks"`
	Summaries      string        `json:"summaries" yaml:"summaries"`
	TopK           int           `json:"top_k" yaml:"top_k"`
	Threshold      float64       `json:"threshold" yaml:"threshold"`
	Timeout        time.Duration `json:"timeout" yaml:"timeout"`
	// Embeddings service
	DefaultModel         string        `json:"default_model" yaml:"default_model"`
	CacheTTL             time.Duration `json:"cache_ttl" yaml:"cache_ttl"`
	ExpectedEmbeddingDim int           `json:"expected_embedding_dim" yaml:"expected_embedding_dim"`
	UseRedisCache bool          `json:"use_redis_cache" yaml:"use_redis_cache"`
	RedisAddr     string        `json:"redis_addr" yaml:"redis_addr"`

    // MMR re-ranking (diversity)
    MmrEnabled        bool    `json:"mmr_enabled" yaml:"mmr_enabled"`
    MmrLambda         float64 `json:"mmr_lambda" yaml:"mmr_lambda"`
    MmrPoolMultiplier int     `json:"mmr_pool_multiplier" yaml:"mmr_pool_multiplier"`
}

// EmbeddingsConfig contains embeddings service settings
type EmbeddingsConfig struct {
	BaseURL      string                    `json:"base_url" yaml:"base_url"`
	DefaultModel string                    `json:"default_model" yaml:"default_model"`
	Timeout      time.Duration             `json:"timeout" yaml:"timeout"`
	CacheTTL     time.Duration             `json:"cache_ttl" yaml:"cache_ttl"`
	MaxLRU       int                       `json:"max_lru" yaml:"max_lru"`
	Chunking     EmbeddingsChunkingConfig  `json:"chunking" yaml:"chunking"`
}

// EmbeddingsChunkingConfig contains chunking settings for embeddings
type EmbeddingsChunkingConfig struct {
	Enabled          bool `json:"enabled" yaml:"enabled"`
	MaxTokens        int  `json:"max_tokens" yaml:"max_tokens"`
	OverlapTokens    int  `json:"overlap_tokens" yaml:"overlap_tokens"`
	MinChunkTokens   int  `json:"min_chunk_tokens" yaml:"min_chunk_tokens"`
}

// TracingConfig contains OpenTelemetry tracing settings
type TracingConfig struct {
	Enabled      bool   `json:"enabled" yaml:"enabled"`
	ServiceName  string `json:"service_name" yaml:"service_name"`
	OTLPEndpoint string `json:"otlp_endpoint" yaml:"otlp_endpoint"`
}

// StreamingConfig contains streaming settings (ring buffer)
type StreamingConfig struct {
	RingCapacity int `json:"ring_capacity" yaml:"ring_capacity"`
}

// SessionConfig contains session management settings
type SessionConfig struct {
    MaxHistory int           `json:"max_history" yaml:"max_history"` // Maximum messages to keep in Redis per session
    TTL        time.Duration `json:"ttl" yaml:"ttl"`                 // Session expiry time
    CacheSize  int           `json:"cache_size" yaml:"cache_size"`   // Max sessions to keep in local cache

    // Context window presets for server-side history retrieval
    ContextWindowDefault   int `json:"context_window_default" yaml:"context_window_default"`
    ContextWindowDebugging int `json:"context_window_debugging" yaml:"context_window_debugging"`

    // Token budgets (guidance for planning/enforcement)
    TokenBudgetPerAgent int `json:"token_budget_per_agent" yaml:"token_budget_per_agent"`
    TokenBudgetPerTask  int `json:"token_budget_per_task" yaml:"token_budget_per_task"`

    // Sliding-window shaping parameters
    PrimersCount int `json:"primers_count" yaml:"primers_count"`
    RecentsCount int `json:"recents_count" yaml:"recents_count"`
}

// WorkflowConfig controls workflow behavior
type WorkflowConfig struct {
	// BypassSingleResult: skip synthesis if only one successful result
	BypassSingleResult bool `json:"bypass_single_result" yaml:"bypass_single_result"`
}

// PolicyConfig contains policy engine settings
type PolicyConfig struct {
	Enabled     bool              `json:"enabled" yaml:"enabled"`
	Mode        string            `json:"mode" yaml:"mode"` // "off", "dry-run", "enforce"
	Path        string            `json:"path" yaml:"path"`
	FailClosed  bool              `json:"fail_closed" yaml:"fail_closed"`
	Environment string            `json:"environment" yaml:"environment"`
	Audit       PolicyAuditConfig `json:"audit" yaml:"audit"`
}

// PolicyAuditConfig contains policy audit settings
type PolicyAuditConfig struct {
	Enabled         bool   `json:"enabled" yaml:"enabled"`
	LogLevel        string `json:"log_level" yaml:"log_level"`
	IncludeInput    bool   `json:"include_input" yaml:"include_input"`
	IncludeDecision bool   `json:"include_decision" yaml:"include_decision"`
}

// DefaultShannonConfig returns the default configuration
func DefaultShannonConfig() *ShannonConfig {
    return &ShannonConfig{
		Auth: AuthConfig{
			Enabled:                  false,
			SkipAuth:                 true,
			JWTSecret:                "change-this-to-a-secure-32-char-minimum-secret",
			AccessTokenExpiry:        30 * time.Minute,
			RefreshTokenExpiry:       7 * 24 * time.Hour,
			APIKeyRateLimit:          1000,
			DefaultTenantLimit:       10000,
			EnableRegistration:       true,
			RequireEmailVerification: false,
		},
		Service: ServiceConfig{
			Port:            50052,
			HealthPort:      8080,
			GracefulTimeout: 30 * time.Second,
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    10 * time.Second,
			MaxHeaderBytes:  1 << 20, // 1MB
		},
		CircuitBreakers: CircuitBreakersConfig{
			Redis: CircuitBreakerConfig{
				MaxRequests:   5,
				Interval:      30 * time.Second,
				Timeout:       60 * time.Second,
				MaxFailures:   5,
				OnStateChange: true,
				Enabled:       true,
			},
			Database: CircuitBreakerConfig{
				MaxRequests:   3,
				Interval:      30 * time.Second,
				Timeout:       60 * time.Second,
				MaxFailures:   3,
				OnStateChange: true,
				Enabled:       true,
			},
			GRPC: CircuitBreakerConfig{
				MaxRequests:   10,
				Interval:      30 * time.Second,
				Timeout:       60 * time.Second,
				MaxFailures:   10,
				OnStateChange: true,
				Enabled:       true,
			},
		},
		Degradation: DegradationConfig{
			Enabled:       true,
			CheckInterval: 30 * time.Second,
			ThresholdConfig: DegradationThresholds{
				Minor: DegradationLevelThreshold{
					CircuitBreakersOpen:         1,
					CriticalDependenciesDown:    0,
					NonCriticalDependenciesDown: 1,
					ErrorRatePercentage:         5.0,
					LatencyThresholdMs:          1000,
				},
				Moderate: DegradationLevelThreshold{
					CircuitBreakersOpen:         2,
					CriticalDependenciesDown:    1,
					NonCriticalDependenciesDown: 2,
					ErrorRatePercentage:         15.0,
					LatencyThresholdMs:          2000,
				},
				Severe: DegradationLevelThreshold{
					CircuitBreakersOpen:         3,
					CriticalDependenciesDown:    2,
					NonCriticalDependenciesDown: 3,
					ErrorRatePercentage:         30.0,
					LatencyThresholdMs:          5000,
				},
			},
			ModeDowngrade: ModeDowngradeConfig{
				Enabled: true,
				MinorDegradationRules: ModeDowngradeRules{
					ComplexToStandard: true,
					StandardToSimple:  false,
					ComplexToSimple:   false,
					ForceSimpleMode:   false,
				},
				ModerateDegradationRules: ModeDowngradeRules{
					ComplexToStandard: true,
					StandardToSimple:  true,
					ComplexToSimple:   false,
					ForceSimpleMode:   false,
				},
				SevereDegradationRules: ModeDowngradeRules{
					ComplexToStandard: true,
					StandardToSimple:  true,
					ComplexToSimple:   true,
					ForceSimpleMode:   true,
				},
			},
			PartialResults: PartialResultsConfig{
				Enabled: true,
				SuccessThresholds: map[string]float64{
					"simple":    1.0, // Need 100% for simple
					"standard":  0.5, // Need 50% for standard
					"complex":   0.6, // Need 60% for complex
					"agent_dag": 0.4, // Need 40% for agent DAG
				},
				TimeoutOverride: 5 * time.Second,
				MaxWaitTime:     30 * time.Second,
			},
			FallbackBehaviors: map[string]string{
				"llm_generation":   "cache",
				"vector_search":    "skip",
				"agent_execution":  "degrade",
				"result_synthesis": "proceed",
				"session_update":   "proceed",
			},
		},
		Health: HealthConfig{
			Enabled:       true,
			CheckInterval: 30 * time.Second,
			Timeout:       5 * time.Second,
			Port:          8081,
			Checks: map[string]HealthCheckConfig{
				"redis": {
					Enabled:  true,
					Critical: true,
					Timeout:  5 * time.Second,
					Interval: 30 * time.Second,
				},
				"database": {
					Enabled:  true,
					Critical: true,
					Timeout:  5 * time.Second,
					Interval: 30 * time.Second,
				},
				"agent_core": {
					Enabled:  true,
					Critical: true,
					Timeout:  5 * time.Second,
					Interval: 30 * time.Second,
				},
				"llm_service": {
					Enabled:  true,
					Critical: false,
					Timeout:  5 * time.Second,
					Interval: 60 * time.Second,
				},
			},
		},
		Agents: AgentsConfig{
			MaxConcurrent:      5,
			DefaultTimeout:     30 * time.Second,
			HealthCheckPort:    2113,
			RetryCount:         3,
			RetryBackoff:       2 * time.Second,
			AgentCoreEndpoint:  "localhost:50051",
			LLMServiceEndpoint: "http://localhost:8000",
		},
		Temporal: TemporalConfig{
			HostPort:  "localhost:7233",
			Namespace: "default",
			TaskQueue: "shannon-task-queue",
			Timeout:   30 * time.Second,
			RetryPolicy: RetryPolicyConfig{
				InitialInterval:    1 * time.Second,
				BackoffCoefficient: 2.0,
				MaximumInterval:    100 * time.Second,
				MaximumAttempts:    10,
			},
		},
		Logging: LoggingConfig{
			Level:            "info",
			Development:      false,
			Encoding:         "json",
			OutputPaths:      []string{"stdout"},
			ErrorOutputPaths: []string{"stderr"},
		},
		Policy: PolicyConfig{
			Enabled:     false,
			Mode:        "off",
			Path:        "/app/config/opa/policies",
			FailClosed:  false,
			Environment: "dev",
			Audit: PolicyAuditConfig{
				Enabled:         true,
				LogLevel:        "info",
				IncludeInput:    true,
				IncludeDecision: true,
			},
		},
		Vector: VectorConfig{
			Enabled:        false,
			Host:           "qdrant",
			Port:           6333,
			TaskEmbeddings: "task_embeddings",
			ToolResults:    "tool_results",
			Cases:          "cases",
			DocumentChunks: "document_chunks",
			Summaries:      "",
			TopK:           5,
			Threshold:      0.75,  // Standardized default threshold for semantic search
			Timeout:        3 * time.Second,
			DefaultModel:   "text-embedding-3-small",
			CacheTTL:       time.Hour,
			UseRedisCache:  false,
			RedisAddr:      "redis:6379",
            // MMR defaults
            MmrEnabled:        false,
            MmrLambda:         0.7,
            MmrPoolMultiplier: 3,
		},
		Embeddings: EmbeddingsConfig{
			BaseURL:      "",
			DefaultModel: "text-embedding-3-small",
			Timeout:      5 * time.Second,
			CacheTTL:     time.Hour,
			MaxLRU:       2048,
			Chunking: EmbeddingsChunkingConfig{
				Enabled:        true,
				MaxTokens:      2000,
				OverlapTokens:  200,
				MinChunkTokens: 100,
			},
		},
		Tracing: TracingConfig{
			Enabled:      false,
			ServiceName:  "shannon-orchestrator",
			OTLPEndpoint: "localhost:4317",
		},
		Streaming: StreamingConfig{
			RingCapacity: 256,
		},
        Workflow: WorkflowConfig{
            BypassSingleResult: true,
        },
        Session: SessionConfig{
            MaxHistory:            500,
            TTL:                   30 * 24 * time.Hour,
            CacheSize:             1024,
            ContextWindowDefault:  50,
            ContextWindowDebugging: 75,
            TokenBudgetPerAgent:   50000,
            TokenBudgetPerTask:    200000,
            PrimersCount:          3,
            RecentsCount:          20,
        },
    }
}

// ValidateShannonConfig validates the Shannon configuration
func ValidateShannonConfig(config map[string]interface{}) error {
	// Convert to ShannonConfig struct for validation (placeholder for future unmarshalling)
	// shannonConfig := &ShannonConfig{}

	// This is a simplified validation - in production you'd use a proper
	// unmarshaling and validation library like go-playground/validator

	if service, ok := config["service"].(map[string]interface{}); ok {
		if port, ok := service["port"].(float64); ok && (port < 1 || port > 65535) {
			return fmt.Errorf("service port must be between 1 and 65535, got %v", port)
		}
		if healthPort, ok := service["health_port"].(float64); ok && (healthPort < 1 || healthPort > 65535) {
			return fmt.Errorf("health port must be between 1 and 65535, got %v", healthPort)
		}
	}

	if degradation, ok := config["degradation"].(map[string]interface{}); ok {
		if enabled, ok := degradation["enabled"].(bool); ok && enabled {
			if thresholds, ok := degradation["thresholds"].(map[string]interface{}); ok {
				// Validate threshold configuration
				levels := []string{"minor", "moderate", "severe"}
				for _, level := range levels {
					if levelConfig, ok := thresholds[level].(map[string]interface{}); ok {
						if errorRate, ok := levelConfig["error_rate_percentage"].(float64); ok {
							if errorRate < 0 || errorRate > 100 {
								return fmt.Errorf("error rate percentage for %s level must be between 0 and 100, got %v", level, errorRate)
							}
						}
					}
				}
			}
		}
	}

    if agents, ok := config["agents"].(map[string]interface{}); ok {
        if maxConcurrent, ok := agents["max_concurrent"].(float64); ok && maxConcurrent < 1 {
            return fmt.Errorf("max concurrent agents must be at least 1, got %v", maxConcurrent)
        }
        if retryCount, ok := agents["retry_count"].(float64); ok && retryCount < 0 {
            return fmt.Errorf("retry count cannot be negative, got %v", retryCount)
        }
    }

    // Validate session-related context windows and token budgets
    if session, ok := config["session"].(map[string]interface{}); ok {
        // Context windows must be [5, 200]
        inWindow := func(v float64) bool { return v >= 5 && v <= 200 }
        if v, ok := session["context_window_default"].(float64); ok {
            if !inWindow(v) { return fmt.Errorf("context_window_default must be between 5 and 200, got %v", v) }
        }
        if v, ok := session["context_window_debugging"].(float64); ok {
            if !inWindow(v) { return fmt.Errorf("context_window_debugging must be between 5 and 200, got %v", v) }
        }
        // Primers/recents must be non-negative and bounded
        if v, ok := session["primers_count"].(float64); ok {
            if v < 0 || v > 1000 { return fmt.Errorf("primers_count must be between 0 and 1000, got %v", v) }
        }
        if v, ok := session["recents_count"].(float64); ok {
            if v < 0 || v > 1000 { return fmt.Errorf("recents_count must be between 0 and 1000, got %v", v) }
        }
        // Token budgets must be [1000, 200000] and per-task >= per-agent
        inBudget := func(v float64) bool { return v >= 1000 && v <= 200000 }
        var perAgent, perTask float64
        if v, ok := session["token_budget_per_agent"].(float64); ok {
            if !inBudget(v) { return fmt.Errorf("token_budget_per_agent must be between 1000 and 200000, got %v", v) }
            perAgent = v
        }
        if v, ok := session["token_budget_per_task"].(float64); ok {
            if !inBudget(v) { return fmt.Errorf("token_budget_per_task must be between 1000 and 200000, got %v", v) }
            perTask = v
        }
        if perAgent > 0 && perTask > 0 && perTask < perAgent {
            return fmt.Errorf("token_budget_per_task (%v) must be >= token_budget_per_agent (%v)", perTask, perAgent)
        }
    }

    return nil
}

// ConfigurationCallback is called when significant configuration changes occur
type ConfigurationCallback func(oldConfig, newConfig *ShannonConfig) error

// ShannonConfigManager provides typed access to Shannon configuration
type ShannonConfigManager struct {
	configManager *ConfigManager
	currentConfig *ShannonConfig
	logger        *zap.Logger
	callbacks     []ConfigurationCallback
}

// NewShannonConfigManager creates a new Shannon-specific configuration manager
func NewShannonConfigManager(configManager *ConfigManager, logger *zap.Logger) *ShannonConfigManager {
	return &ShannonConfigManager{
		configManager: configManager,
		currentConfig: DefaultShannonConfig(),
		logger:        logger,
		callbacks:     make([]ConfigurationCallback, 0),
	}
}

// GetConfig returns the current Shannon configuration
func (scm *ShannonConfigManager) GetConfig() *ShannonConfig {
	// Return a copy to prevent external modification
	config := *scm.currentConfig
	return &config
}

// Initialize sets up configuration management for Shannon
func (scm *ShannonConfigManager) Initialize() error {
	// Register validator for shannon.yaml
	scm.configManager.RegisterValidator("shannon.yaml", ValidateShannonConfig)
	scm.configManager.RegisterValidator("shannon.json", ValidateShannonConfig)

	// Register handler for configuration changes
	scm.configManager.RegisterHandler("shannon.yaml", scm.handleConfigChange)
	scm.configManager.RegisterHandler("shannon.json", scm.handleConfigChange)

	// Try to load existing configuration
	if config, exists := scm.configManager.GetConfig("shannon.yaml"); exists {
		if err := scm.updateConfigFromMap(config); err != nil {
			scm.logger.Error("Failed to load shannon.yaml", zap.Error(err))
		}
	} else if config, exists := scm.configManager.GetConfig("shannon.json"); exists {
		if err := scm.updateConfigFromMap(config); err != nil {
			scm.logger.Error("Failed to load shannon.json", zap.Error(err))
		}
	}

	return nil
}

// handleConfigChange processes Shannon configuration changes
func (scm *ShannonConfigManager) handleConfigChange(event ChangeEvent) error {
	scm.logger.Info("Shannon configuration changed",
		zap.String("file", event.File),
		zap.String("action", event.Action),
	)

	if event.Action == "delete" {
		// Revert to default configuration
		scm.currentConfig = DefaultShannonConfig()
		scm.logger.Info("Reverted to default Shannon configuration")
		return nil
	}

	return scm.updateConfigFromMap(event.Config)
}

// updateConfigFromMap updates the current config from a map
func (scm *ShannonConfigManager) updateConfigFromMap(configMap map[string]interface{}) error {
	newConfig := DefaultShannonConfig()

	// Update service config
	if service, ok := configMap["service"].(map[string]interface{}); ok {
		if port, ok := service["port"].(float64); ok {
			newConfig.Service.Port = int(port)
		}
		if healthPort, ok := service["health_port"].(float64); ok {
			newConfig.Service.HealthPort = int(healthPort)
		}
		if timeout, ok := service["graceful_timeout"].(string); ok {
			if d, err := time.ParseDuration(timeout); err == nil {
				newConfig.Service.GracefulTimeout = d
			}
		}
		if timeout, ok := service["read_timeout"].(string); ok {
			if d, err := time.ParseDuration(timeout); err == nil {
				newConfig.Service.ReadTimeout = d
			}
		}
		if timeout, ok := service["write_timeout"].(string); ok {
			if d, err := time.ParseDuration(timeout); err == nil {
				newConfig.Service.WriteTimeout = d
			}
		}
	}

	// Update circuit breaker configs
	if cb, ok := configMap["circuit_breakers"].(map[string]interface{}); ok {
		scm.updateCircuitBreakerConfig(cb, &newConfig.CircuitBreakers)
	}

	// Update degradation config
	if degradation, ok := configMap["degradation"].(map[string]interface{}); ok {
		scm.updateDegradationConfig(degradation, &newConfig.Degradation)
	}

	// Update health config
	if health, ok := configMap["health"].(map[string]interface{}); ok {
		scm.updateHealthConfig(health, &newConfig.Health)
	}

	// Update agents config
	if agents, ok := configMap["agents"].(map[string]interface{}); ok {
		scm.updateAgentsConfig(agents, &newConfig.Agents)
	}

	// Update temporal config
	if temporal, ok := configMap["temporal"].(map[string]interface{}); ok {
		scm.updateTemporalConfig(temporal, &newConfig.Temporal)
	}

	// Update logging config
	if logging, ok := configMap["logging"].(map[string]interface{}); ok {
		scm.updateLoggingConfig(logging, &newConfig.Logging)
	}

	// Update auth config
	if auth, ok := configMap["auth"].(map[string]interface{}); ok {
		scm.updateAuthConfig(auth, &newConfig.Auth)
	}

	// Update policy config
	if policy, ok := configMap["policy"].(map[string]interface{}); ok {
		scm.updatePolicyConfig(policy, &newConfig.Policy)
	}

	// Update streaming config
	if streaming, ok := configMap["streaming"].(map[string]interface{}); ok {
		if capv, ok := streaming["ring_capacity"].(float64); ok {
			if capv > 0 {
				newConfig.Streaming.RingCapacity = int(capv)
			}
		}
	}

	// Update workflow config
	if wf, ok := configMap["workflow"].(map[string]interface{}); ok {
		if v, ok := wf["bypass_single_result"].(bool); ok {
			newConfig.Workflow.BypassSingleResult = v
		}
	}

    // Update session config
    if session, ok := configMap["session"].(map[string]interface{}); ok {
        if v, ok := session["max_history"].(float64); ok {
            newConfig.Session.MaxHistory = int(v)
        }
        if v, ok := session["ttl"].(string); ok {
            if d, err := time.ParseDuration(v); err == nil {
                newConfig.Session.TTL = d
            }
        }
        if v, ok := session["cache_size"].(float64); ok {
            newConfig.Session.CacheSize = int(v)
        }
        if v, ok := session["context_window_default"].(float64); ok {
            newConfig.Session.ContextWindowDefault = int(v)
        }
        if v, ok := session["context_window_debugging"].(float64); ok {
            newConfig.Session.ContextWindowDebugging = int(v)
        }
        if v, ok := session["token_budget_per_agent"].(float64); ok {
            newConfig.Session.TokenBudgetPerAgent = int(v)
        }
        if v, ok := session["token_budget_per_task"].(float64); ok {
            newConfig.Session.TokenBudgetPerTask = int(v)
        }
        if v, ok := session["primers_count"].(float64); ok {
            newConfig.Session.PrimersCount = int(v)
        }
        if v, ok := session["recents_count"].(float64); ok {
            newConfig.Session.RecentsCount = int(v)
        }
    }

	// Update vector config
	if vector, ok := configMap["vector"].(map[string]interface{}); ok {
		scm.updateVectorConfig(vector, &newConfig.Vector)
	}

	// Update embeddings config
	if embeddings, ok := configMap["embeddings"].(map[string]interface{}); ok {
		scm.updateEmbeddingsConfig(embeddings, &newConfig.Embeddings)
	}

	oldConfig := scm.currentConfig
	scm.currentConfig = newConfig
	scm.logger.Info("Shannon configuration updated successfully")

	// Trigger change notifications for significant changes
	scm.notifyConfigChanges(oldConfig, newConfig)

	// Trigger callbacks for configuration updates
	scm.triggerCallbacks(oldConfig, newConfig)

	return nil
}

// updateCircuitBreakerConfig updates circuit breaker configuration
func (scm *ShannonConfigManager) updateCircuitBreakerConfig(cbMap map[string]interface{}, config *CircuitBreakersConfig) {
	if redis, ok := cbMap["redis"].(map[string]interface{}); ok {
		scm.updateSingleCircuitBreakerConfig(redis, &config.Redis)
	}
	if database, ok := cbMap["database"].(map[string]interface{}); ok {
		scm.updateSingleCircuitBreakerConfig(database, &config.Database)
	}
	if grpc, ok := cbMap["grpc"].(map[string]interface{}); ok {
		scm.updateSingleCircuitBreakerConfig(grpc, &config.GRPC)
	}
}

// updateSingleCircuitBreakerConfig updates a single circuit breaker config
func (scm *ShannonConfigManager) updateSingleCircuitBreakerConfig(cbMap map[string]interface{}, config *CircuitBreakerConfig) {
	if maxReq, ok := cbMap["max_requests"].(float64); ok {
		config.MaxRequests = uint32(maxReq)
	}
	if interval, ok := cbMap["interval"].(string); ok {
		if d, err := time.ParseDuration(interval); err == nil {
			config.Interval = d
		}
	}
	if timeout, ok := cbMap["timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			config.Timeout = d
		}
	}
	if maxFail, ok := cbMap["max_failures"].(float64); ok {
		config.MaxFailures = uint32(maxFail)
	}
	if enabled, ok := cbMap["enabled"].(bool); ok {
		config.Enabled = enabled
	}
}

// updateDegradationConfig updates degradation configuration
func (scm *ShannonConfigManager) updateDegradationConfig(degMap map[string]interface{}, config *DegradationConfig) {
	if enabled, ok := degMap["enabled"].(bool); ok {
		config.Enabled = enabled
	}
	if interval, ok := degMap["check_interval"].(string); ok {
		if d, err := time.ParseDuration(interval); err == nil {
			config.CheckInterval = d
		}
	}

	// Update thresholds
	if thresholds, ok := degMap["thresholds"].(map[string]interface{}); ok {
		scm.updateDegradationThresholds(thresholds, &config.ThresholdConfig)
	}

	// Update partial results config
	if partial, ok := degMap["partial_results"].(map[string]interface{}); ok {
		scm.updatePartialResultsConfig(partial, &config.PartialResults)
	}

	// Update fallback behaviors
	if behaviors, ok := degMap["fallback_behaviors"].(map[string]interface{}); ok {
		config.FallbackBehaviors = make(map[string]string)
		for k, v := range behaviors {
			if str, ok := v.(string); ok {
				config.FallbackBehaviors[k] = str
			}
		}
	}
}

// updateDegradationThresholds updates degradation threshold configuration
func (scm *ShannonConfigManager) updateDegradationThresholds(thresholds map[string]interface{}, config *DegradationThresholds) {
	if minor, ok := thresholds["minor"].(map[string]interface{}); ok {
		scm.updateSingleThreshold(minor, &config.Minor)
	}
	if moderate, ok := thresholds["moderate"].(map[string]interface{}); ok {
		scm.updateSingleThreshold(moderate, &config.Moderate)
	}
	if severe, ok := thresholds["severe"].(map[string]interface{}); ok {
		scm.updateSingleThreshold(severe, &config.Severe)
	}
}

// updateSingleThreshold updates a single degradation threshold
func (scm *ShannonConfigManager) updateSingleThreshold(threshold map[string]interface{}, config *DegradationLevelThreshold) {
	if cb, ok := threshold["circuit_breakers_open"].(float64); ok {
		config.CircuitBreakersOpen = int(cb)
	}
	if critical, ok := threshold["critical_dependencies_down"].(float64); ok {
		config.CriticalDependenciesDown = int(critical)
	}
	if nonCritical, ok := threshold["non_critical_dependencies_down"].(float64); ok {
		config.NonCriticalDependenciesDown = int(nonCritical)
	}
	if errorRate, ok := threshold["error_rate_percentage"].(float64); ok {
		config.ErrorRatePercentage = errorRate
	}
	if latency, ok := threshold["latency_threshold_ms"].(float64); ok {
		config.LatencyThresholdMs = int64(latency)
	}
}

// updatePartialResultsConfig updates partial results configuration
func (scm *ShannonConfigManager) updatePartialResultsConfig(partial map[string]interface{}, config *PartialResultsConfig) {
	if enabled, ok := partial["enabled"].(bool); ok {
		config.Enabled = enabled
	}
	if thresholds, ok := partial["success_thresholds"].(map[string]interface{}); ok {
		config.SuccessThresholds = make(map[string]float64)
		for k, v := range thresholds {
			if f, ok := v.(float64); ok {
				config.SuccessThresholds[k] = f
			}
		}
	}
}

// updateHealthConfig updates health configuration
func (scm *ShannonConfigManager) updateHealthConfig(healthMap map[string]interface{}, config *HealthConfig) {
	if enabled, ok := healthMap["enabled"].(bool); ok {
		config.Enabled = enabled
	}
	if interval, ok := healthMap["check_interval"].(string); ok {
		if d, err := time.ParseDuration(interval); err == nil {
			config.CheckInterval = d
		}
	}
	if timeout, ok := healthMap["timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			config.Timeout = d
		}
	}
	if port, ok := healthMap["port"].(float64); ok {
		config.Port = int(port)
	}

	// Update per-check configuration
	if checks, ok := healthMap["checks"].(map[string]interface{}); ok {
		config.Checks = make(map[string]HealthCheckConfig)
		for checkName, checkData := range checks {
			if checkMap, ok := checkData.(map[string]interface{}); ok {
				var checkConfig HealthCheckConfig
				if enabled, ok := checkMap["enabled"].(bool); ok {
					checkConfig.Enabled = enabled
				}
				if critical, ok := checkMap["critical"].(bool); ok {
					checkConfig.Critical = critical
				}
				if timeout, ok := checkMap["timeout"].(string); ok {
					if d, err := time.ParseDuration(timeout); err == nil {
						checkConfig.Timeout = d
					}
				}
				if interval, ok := checkMap["interval"].(string); ok {
					if d, err := time.ParseDuration(interval); err == nil {
						checkConfig.Interval = d
					}
				}
				config.Checks[checkName] = checkConfig
			}
		}
	}
}

// updateAgentsConfig updates agents configuration
func (scm *ShannonConfigManager) updateAgentsConfig(agentsMap map[string]interface{}, config *AgentsConfig) {
	if maxConcurrent, ok := agentsMap["max_concurrent"].(float64); ok {
		config.MaxConcurrent = int(maxConcurrent)
	}
	if timeout, ok := agentsMap["default_timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			config.DefaultTimeout = d
		}
	}
	if port, ok := agentsMap["health_check_port"].(float64); ok {
		config.HealthCheckPort = int(port)
	}
	if retryCount, ok := agentsMap["retry_count"].(float64); ok {
		config.RetryCount = int(retryCount)
	}
	if backoff, ok := agentsMap["retry_backoff"].(string); ok {
		if d, err := time.ParseDuration(backoff); err == nil {
			config.RetryBackoff = d
		}
	}
	if endpoint, ok := agentsMap["agent_core_endpoint"].(string); ok {
		config.AgentCoreEndpoint = endpoint
	}
	if endpoint, ok := agentsMap["llm_service_endpoint"].(string); ok {
		config.LLMServiceEndpoint = endpoint
	}
}

// updateTemporalConfig updates temporal configuration
func (scm *ShannonConfigManager) updateTemporalConfig(temporalMap map[string]interface{}, config *TemporalConfig) {
	if hostPort, ok := temporalMap["host_port"].(string); ok {
		config.HostPort = hostPort
	}
	if namespace, ok := temporalMap["namespace"].(string); ok {
		config.Namespace = namespace
	}
	if taskQueue, ok := temporalMap["task_queue"].(string); ok {
		config.TaskQueue = taskQueue
	}
	if timeout, ok := temporalMap["timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			config.Timeout = d
		}
	}
}

// updateLoggingConfig updates logging configuration
func (scm *ShannonConfigManager) updateLoggingConfig(loggingMap map[string]interface{}, config *LoggingConfig) {
	if level, ok := loggingMap["level"].(string); ok {
		config.Level = level
	}
	if dev, ok := loggingMap["development"].(bool); ok {
		config.Development = dev
	}
	if encoding, ok := loggingMap["encoding"].(string); ok {
		config.Encoding = encoding
	}
	if paths, ok := loggingMap["output_paths"].([]interface{}); ok {
		config.OutputPaths = make([]string, len(paths))
		for i, p := range paths {
			if str, ok := p.(string); ok {
				config.OutputPaths[i] = str
			}
		}
	}
	if paths, ok := loggingMap["error_output_paths"].([]interface{}); ok {
		config.ErrorOutputPaths = make([]string, len(paths))
		for i, p := range paths {
			if str, ok := p.(string); ok {
				config.ErrorOutputPaths[i] = str
			}
		}
	}
}

// updateAuthConfig updates authentication configuration
func (scm *ShannonConfigManager) updateAuthConfig(authMap map[string]interface{}, config *AuthConfig) {
	if enabled, ok := authMap["enabled"].(bool); ok {
		config.Enabled = enabled
	}
	if skip, ok := authMap["skip_auth"].(bool); ok {
		config.SkipAuth = skip
	}
	if secret, ok := authMap["jwt_secret"].(string); ok {
		config.JWTSecret = secret
	}
	if v, ok := authMap["access_token_expiry"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			config.AccessTokenExpiry = d
		} else {
			scm.logger.Warn("Invalid access_token_expiry, using default", zap.String("value", v), zap.Error(err))
		}
	}
	if v, ok := authMap["refresh_token_expiry"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			config.RefreshTokenExpiry = d
		} else {
			scm.logger.Warn("Invalid refresh_token_expiry, using default", zap.String("value", v), zap.Error(err))
		}
	}
	if rl, ok := authMap["api_key_rate_limit"].(float64); ok {
		config.APIKeyRateLimit = int(rl)
	}
	if tl, ok := authMap["default_tenant_limit"].(float64); ok {
		config.DefaultTenantLimit = int(tl)
	}
	if reg, ok := authMap["enable_registration"].(bool); ok {
		config.EnableRegistration = reg
	}
	if rev, ok := authMap["require_email_verification"].(bool); ok {
		config.RequireEmailVerification = rev
	}
}

// notifyConfigChanges triggers notifications for configuration changes
func (scm *ShannonConfigManager) notifyConfigChanges(oldConfig, newConfig *ShannonConfig) {
	// Check for endpoint changes that require service restarts or reconnections
	if oldConfig.Agents.AgentCoreEndpoint != newConfig.Agents.AgentCoreEndpoint {
		scm.logger.Info("Agent Core endpoint changed",
			zap.String("old", oldConfig.Agents.AgentCoreEndpoint),
			zap.String("new", newConfig.Agents.AgentCoreEndpoint),
		)
	}

	if oldConfig.Agents.LLMServiceEndpoint != newConfig.Agents.LLMServiceEndpoint {
		scm.logger.Info("LLM Service endpoint changed",
			zap.String("old", oldConfig.Agents.LLMServiceEndpoint),
			zap.String("new", newConfig.Agents.LLMServiceEndpoint),
		)
	}

	if oldConfig.Health.Port != newConfig.Health.Port {
		scm.logger.Info("Health server port changed",
			zap.Int("old", oldConfig.Health.Port),
			zap.Int("new", newConfig.Health.Port),
		)
	}

	// Check for health check configuration changes
	if oldConfig.Health.Enabled != newConfig.Health.Enabled ||
		oldConfig.Health.CheckInterval != newConfig.Health.CheckInterval {
		scm.logger.Info("Health check global settings changed")
	}

	// Notify of workflow behavior changes (useful in logs)
	if oldConfig.Workflow != newConfig.Workflow {
		scm.logger.Info("Workflow behavior changed",
			zap.Bool("bypass_single_result", newConfig.Workflow.BypassSingleResult),
		)
	}
}

// RegisterCallback registers a callback to be called when configuration changes
func (scm *ShannonConfigManager) RegisterCallback(callback ConfigurationCallback) {
	scm.callbacks = append(scm.callbacks, callback)
	scm.logger.Info("Configuration callback registered")
}

// updatePolicyConfig updates policy configuration
func (scm *ShannonConfigManager) updatePolicyConfig(policyMap map[string]interface{}, config *PolicyConfig) {
	if enabled, ok := policyMap["enabled"].(bool); ok {
		config.Enabled = enabled
	}
	if mode, ok := policyMap["mode"].(string); ok {
		config.Mode = mode
	}
	if path, ok := policyMap["path"].(string); ok {
		config.Path = path
	}
	if failClosed, ok := policyMap["fail_closed"].(bool); ok {
		config.FailClosed = failClosed
	}
	if environment, ok := policyMap["environment"].(string); ok {
		config.Environment = environment
	}

	// Update audit config
	if audit, ok := policyMap["audit"].(map[string]interface{}); ok {
		if enabled, ok := audit["enabled"].(bool); ok {
			config.Audit.Enabled = enabled
		}
		if logLevel, ok := audit["log_level"].(string); ok {
			config.Audit.LogLevel = logLevel
		}
		if includeInput, ok := audit["include_input"].(bool); ok {
			config.Audit.IncludeInput = includeInput
		}
		if includeDecision, ok := audit["include_decision"].(bool); ok {
			config.Audit.IncludeDecision = includeDecision
		}
	}
}

// updateVectorConfig updates vector configuration
func (scm *ShannonConfigManager) updateVectorConfig(vectorMap map[string]interface{}, config *VectorConfig) {
	if enabled, ok := vectorMap["enabled"].(bool); ok {
		config.Enabled = enabled
	}
	if host, ok := vectorMap["host"].(string); ok {
		config.Host = host
	}
	if port, ok := vectorMap["port"].(float64); ok {
		config.Port = int(port)
	}
	if taskEmbeddings, ok := vectorMap["task_embeddings"].(string); ok {
		config.TaskEmbeddings = taskEmbeddings
	}
	if toolResults, ok := vectorMap["tool_results"].(string); ok {
		config.ToolResults = toolResults
	}
	if cases, ok := vectorMap["cases"].(string); ok {
		config.Cases = cases
	}
	if documentChunks, ok := vectorMap["document_chunks"].(string); ok {
		config.DocumentChunks = documentChunks
	}
	if summaries, ok := vectorMap["summaries"].(string); ok {
		config.Summaries = summaries
	}
	if topK, ok := vectorMap["top_k"].(float64); ok {
		config.TopK = int(topK)
	}
	if threshold, ok := vectorMap["threshold"].(float64); ok {
		config.Threshold = threshold
	}
	if timeout, ok := vectorMap["timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			config.Timeout = d
		}
	}
	if defaultModel, ok := vectorMap["default_model"].(string); ok {
		config.DefaultModel = defaultModel
	}
	if cacheTTL, ok := vectorMap["cache_ttl"].(string); ok {
		if d, err := time.ParseDuration(cacheTTL); err == nil {
			config.CacheTTL = d
		}
	}
	if expectedEmbeddingDim, ok := vectorMap["expected_embedding_dim"].(float64); ok {
		config.ExpectedEmbeddingDim = int(expectedEmbeddingDim)
	}
	if useRedisCache, ok := vectorMap["use_redis_cache"].(bool); ok {
		config.UseRedisCache = useRedisCache
	}
	if redisAddr, ok := vectorMap["redis_addr"].(string); ok {
		config.RedisAddr = redisAddr
	}
}

// updateEmbeddingsConfig updates embeddings configuration
func (scm *ShannonConfigManager) updateEmbeddingsConfig(embeddingsMap map[string]interface{}, config *EmbeddingsConfig) {
	if baseURL, ok := embeddingsMap["base_url"].(string); ok {
		config.BaseURL = baseURL
	}
	if defaultModel, ok := embeddingsMap["default_model"].(string); ok {
		config.DefaultModel = defaultModel
	}
	if timeout, ok := embeddingsMap["timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			config.Timeout = d
		}
	}
	if cacheTTL, ok := embeddingsMap["cache_ttl"].(string); ok {
		if d, err := time.ParseDuration(cacheTTL); err == nil {
			config.CacheTTL = d
		}
	}
	if maxLRU, ok := embeddingsMap["max_lru"].(float64); ok {
		config.MaxLRU = int(maxLRU)
	}
	// Parse chunking configuration
	if chunking, ok := embeddingsMap["chunking"].(map[string]interface{}); ok {
		if enabled, ok := chunking["enabled"].(bool); ok {
			config.Chunking.Enabled = enabled
		}
		if maxTokens, ok := chunking["max_tokens"].(float64); ok {
			config.Chunking.MaxTokens = int(maxTokens)
		}
		if overlapTokens, ok := chunking["overlap_tokens"].(float64); ok {
			config.Chunking.OverlapTokens = int(overlapTokens)
		}
		if minChunkTokens, ok := chunking["min_chunk_tokens"].(float64); ok {
			config.Chunking.MinChunkTokens = int(minChunkTokens)
		}
	}
}

// triggerCallbacks calls all registered callbacks with configuration changes
func (scm *ShannonConfigManager) triggerCallbacks(oldConfig, newConfig *ShannonConfig) {
	for i, callback := range scm.callbacks {
		if err := callback(oldConfig, newConfig); err != nil {
			scm.logger.Error("Configuration callback failed",
				zap.Int("callback_index", i),
				zap.Error(err),
			)
		}
	}
}
