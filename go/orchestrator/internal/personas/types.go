package personas

import (
	"context"
	"time"

	"github.com/samber/lo"
)

// PersonaConfig defines the configuration for a specific persona
type PersonaConfig struct {
	ID           string                 `yaml:"id" json:"id"`
	Description  string                 `yaml:"description" json:"description"`
	SystemPrompt string                 `yaml:"system_prompt" json:"system_prompt"`
	Tools        []string               `yaml:"tools" json:"tools"`
	MaxTokens    int                    `yaml:"max_tokens" json:"max_tokens"`
	TokenBudget  string                 `yaml:"token_budget" json:"token_budget"` // For backward compatibility
	Temperature  float64                `yaml:"temperature" json:"temperature"`
	Keywords     []string               `yaml:"keywords" json:"keywords"`
	Priority     int                    `yaml:"priority" json:"priority"`
	Timeout      time.Duration          `yaml:"timeout" json:"timeout"`
	Capabilities []string               `yaml:"capabilities" json:"capabilities"`
	Metadata     map[string]interface{} `yaml:"metadata,omitempty" json:"metadata,omitempty"`
}

// SelectionConfig defines the configuration for persona selection
type SelectionConfig struct {
	Method                  string        `yaml:"method" json:"method"`
	ComplexityThreshold     float64       `yaml:"complexity_threshold" json:"complexity_threshold"`
	LLMTimeoutMs            int           `yaml:"llm_timeout_ms" json:"llm_timeout_ms"`
	CacheTTL                time.Duration `yaml:"cache_ttl" json:"cache_ttl"`
	EnableSemanticMatching  bool          `yaml:"enable_semantic_matching" json:"enable_semantic_matching"`
	FallbackStrategy        string        `yaml:"fallback_strategy" json:"fallback_strategy"`
	MaxConcurrentSelections int           `yaml:"max_concurrent_selections" json:"max_concurrent_selections"`
	DebugMode               bool          `yaml:"debug_mode" json:"debug_mode"`
}

// SelectionRequest represents a persona selection request
type SelectionRequest struct {
	Description     string                 `json:"description"`
	ComplexityScore float64                `json:"complexity_score"`
	TaskType        string                 `json:"task_type,omitempty"`
	UserID          string                 `json:"user_id,omitempty"`
	SessionID       string                 `json:"session_id,omitempty"`
	Context         map[string]interface{} `json:"context,omitempty"`
	ResourceLimits  *ResourceLimits        `json:"resource_limits,omitempty"`
	Preferences     *UserPreferences       `json:"preferences,omitempty"`
}

// SelectionResult represents the result of persona selection
type SelectionResult struct {
	PersonaID     string              `json:"persona_id"`
	Confidence    float64             `json:"confidence"`
	Reasoning     string              `json:"reasoning,omitempty"`
	Alternatives  []AlternativeChoice `json:"alternatives,omitempty"`
	SelectionTime time.Duration       `json:"selection_time"`
	Method        string              `json:"method"`
	CacheHit      bool                `json:"cache_hit"`
}

// ResourceLimits defines resource constraints for persona selection
type ResourceLimits struct {
	MaxTokens   int           `json:"max_tokens,omitempty"`
	MaxDuration time.Duration `json:"max_duration,omitempty"`
	MaxCostUSD  float64       `json:"max_cost_usd,omitempty"`
}

// UserPreferences defines user-specific preferences
type UserPreferences struct {
	PreferredPersonas []string          `json:"preferred_personas,omitempty"`
	ExcludedPersonas  []string          `json:"excluded_personas,omitempty"`
	CustomSettings    map[string]string `json:"custom_settings,omitempty"`
}

// AlternativeChoice represents an alternative persona choice
type AlternativeChoice struct {
	PersonaID  string  `json:"persona_id"`
	Score      float64 `json:"score"`
	Confidence float64 `json:"confidence"`
}

// PersonaFilter defines filters for listing personas
type PersonaFilter struct {
	Capabilities []string `json:"capabilities,omitempty"`
	MinPriority  int      `json:"min_priority,omitempty"`
	MaxTokens    int      `json:"max_tokens,omitempty"`
	Priority     int      `json:"priority,omitempty"`
	Keywords     []string `json:"keywords,omitempty"`
}

// Matches returns true if the persona matches this filter
func (f *PersonaFilter) Matches(persona *PersonaConfig) bool {
	if f == nil {
		return true
	}

	// Check priority filter
	if f.Priority != 0 && persona.Priority != f.Priority {
		return false
	}

	// Check min priority filter
	if f.MinPriority != 0 && persona.Priority < f.MinPriority {
		return false
	}

	// Check max tokens filter
	if f.MaxTokens != 0 && persona.MaxTokens > f.MaxTokens {
		return false
	}

	// Check capabilities filter
	if len(f.Capabilities) > 0 {
		if !lo.SomeBy(f.Capabilities, func(filterCap string) bool {
			return lo.Contains(persona.Capabilities, filterCap)
		}) {
			return false
		}
	}

	// Check keywords filter
	if len(f.Keywords) > 0 {
		if !lo.SomeBy(f.Keywords, func(filterKeyword string) bool {
			return lo.Contains(persona.Keywords, filterKeyword)
		}) {
			return false
		}
	}

	return true
}

// ExecutionResult represents the result of a persona execution
type ExecutionResult struct {
	PersonaID  string        `json:"persona_id"`
	TaskID     string        `json:"task_id"`
	Success    bool          `json:"success"`
	TokensUsed int           `json:"tokens_used"`
	Duration   time.Duration `json:"duration"`
	Error      string        `json:"error,omitempty"`
	Quality    float64       `json:"quality,omitempty"`
	Timestamp  time.Time     `json:"timestamp"`
}

// CandidatePersona represents a candidate persona during selection
type CandidatePersona struct {
	PersonaID  string         `json:"persona_id"`
	Config     *PersonaConfig `json:"config"`
	Score      float64        `json:"score"`
	Confidence float64        `json:"confidence"`
	Method     string         `json:"method"`
	Reasoning  string         `json:"reasoning,omitempty"`
}

// Selector interface defines the core persona selection functionality
type Selector interface {
	// SelectPersona selects the best persona for a given request
	SelectPersona(ctx context.Context, req *SelectionRequest) (*SelectionResult, error)

	// GetPersona retrieves a specific persona configuration
	GetPersona(personaID string) (*PersonaConfig, error)

	// ListPersonas lists personas matching the given filter
	ListPersonas(filter *PersonaFilter) ([]*PersonaConfig, error)

	// ValidateConfig validates the current configuration
	ValidateConfig() error

	// Close cleanly shuts down the selector
	Close() error
}

// PersonaManager interface extends Selector with management capabilities
type PersonaManager interface {
	Selector

	// ReloadConfig reloads the configuration from file
	ReloadConfig() error

	// GetMetrics returns current metrics
	GetMetrics() *Metrics

	// UpdatePersonaStats updates persona execution statistics
	UpdatePersonaStats(personaID string, result *ExecutionResult) error
}

// Cache interface defines caching functionality
type Cache interface {
	Get(key string) (string, bool)
	Set(key, personaID string)
	Clear()
	Close() error
	Size() int
}

// MatcherConfig defines configuration for semantic matching
type MatcherConfig struct {
	EnableSemanticMatching bool             `yaml:"enable_semantic_matching"`
	TFIDFEnabled           bool             `yaml:"tfidf_enabled"`
	EmbeddingEnabled       bool             `yaml:"embedding_enabled"`
	APITimeout             time.Duration    `yaml:"api_timeout"`
	LocalFallback          bool             `yaml:"local_fallback"`
	EmbeddingConfig        *EmbeddingConfig `yaml:"embedding_config,omitempty"`
}

// EmbeddingAPI interface for external embedding services
type EmbeddingAPI interface {
	GetSimilarity(ctx context.Context, text1, text2 string) (float64, error)
	GetEmbedding(ctx context.Context, text string) ([]float64, error)
}

// TFIDFModelInterface for TF-IDF similarity calculation (interface version)
type TFIDFModelInterface interface {
	Similarity(text1, text2 string) float64
	Train(documents []string) error
}

// Stemmer interface for word stemming
type Stemmer interface {
	Stem(word string) string
}
