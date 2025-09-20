package personas

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// Config holds the complete personas configuration
type Config struct {
	Personas  map[string]*PersonaConfig `yaml:"personas"`
	Selection *SelectionConfig          `yaml:"selection"`
	Embedding *EmbeddingConfig          `yaml:"embedding,omitempty"`
}

// BudgetCalculator handles token budget calculations
type BudgetCalculator struct {
	taskTypeMultipliers map[string]float64
	baseTokens          map[string]int
	userQuotas          map[string]int
}

// LoadConfig loads personas configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, NewConfigError(path, "", "", fmt.Errorf("failed to read config file: %w", err))
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, NewConfigError(path, "", "", fmt.Errorf("failed to parse YAML: %w", err))
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Set defaults for missing fields
	config.setDefaults()

	return &config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Personas == nil || len(c.Personas) == 0 {
		return NewConfigError("", "personas", "", fmt.Errorf("no personas defined"))
	}

	// Validate each persona
	for id, persona := range c.Personas {
		if err := c.validatePersona(id, persona); err != nil {
			return err
		}
	}

	// Validate selection config
	if c.Selection != nil {
		if err := c.validateSelection(); err != nil {
			return err
		}
	}

	// Validate embedding config
	if c.Embedding != nil {
		if err := c.validateEmbedding(); err != nil {
			return err
		}
	}

	return nil
}

// validatePersona validates a single persona configuration
func (c *Config) validatePersona(id string, persona *PersonaConfig) error {
	if persona.ID == "" {
		persona.ID = id // Use map key as ID if not set
	}

	if persona.ID != id {
		return NewConfigError("", "personas", id, fmt.Errorf("persona ID mismatch: %s != %s", persona.ID, id))
	}

	if persona.Description == "" {
		return NewConfigError("", "personas", id+".description", fmt.Errorf("description is required"))
	}

	if persona.Temperature < 0 || persona.Temperature > 2 {
		return NewConfigError("", "personas", id+".temperature", fmt.Errorf("temperature must be between 0 and 2, got %f", persona.Temperature))
	}

	if persona.MaxTokens < 0 {
		return NewConfigError("", "personas", id+".max_tokens", fmt.Errorf("max_tokens cannot be negative"))
	}

	return nil
}

// validateSelection validates the selection configuration
func (c *Config) validateSelection() error {
	if c.Selection.Method != "" {
		validMethods := []string{"auto", "keyword", "llm", "semantic", "enhanced"}
		valid := false
		for _, method := range validMethods {
			if c.Selection.Method == method {
				valid = true
				break
			}
		}
		if !valid {
			return NewConfigError("", "selection", "method", fmt.Errorf("invalid method: %s", c.Selection.Method))
		}
	}

	if c.Selection.ComplexityThreshold < 0 || c.Selection.ComplexityThreshold > 1 {
		return NewConfigError("", "selection", "complexity_threshold", fmt.Errorf("complexity_threshold must be between 0 and 1"))
	}

	if c.Selection.MaxConcurrentSelections < 0 {
		return NewConfigError("", "selection", "max_concurrent_selections", fmt.Errorf("max_concurrent_selections cannot be negative"))
	}

	return nil
}

// validateEmbedding validates the embedding configuration
func (c *Config) validateEmbedding() error {
	if c.Embedding.Provider == "" {
		return NewConfigError("", "embedding", "provider", fmt.Errorf("embedding provider is required"))
	}

	validProviders := []EmbeddingProvider{ProviderOpenAI, ProviderAzureAI, ProviderLocal, ProviderHuggingFace}
	valid := false
	for _, provider := range validProviders {
		if c.Embedding.Provider == provider {
			valid = true
			break
		}
	}
	if !valid {
		return NewConfigError("", "embedding", "provider", fmt.Errorf("invalid provider: %s", c.Embedding.Provider))
	}

	if c.Embedding.Timeout <= 0 {
		return NewConfigError("", "embedding", "timeout", fmt.Errorf("timeout must be positive"))
	}

	if c.Embedding.MaxRetries < 0 {
		return NewConfigError("", "embedding", "max_retries", fmt.Errorf("max_retries cannot be negative"))
	}

	if c.Embedding.RequestsPerMinute < 0 {
		return NewConfigError("", "embedding", "requests_per_minute", fmt.Errorf("requests_per_minute cannot be negative"))
	}

	// Validate individual provider configs
	for i, providerConfig := range c.Embedding.Providers {
		if providerConfig.Name == "" {
			return NewConfigError("", "embedding", fmt.Sprintf("providers[%d].name", i), fmt.Errorf("provider name is required"))
		}
		if providerConfig.Weight < 0 {
			return NewConfigError("", "embedding", fmt.Sprintf("providers[%d].weight", i), fmt.Errorf("provider weight cannot be negative"))
		}
	}

	return nil
}

// setDefaults sets default values for missing configuration fields
func (c *Config) setDefaults() {
	// Set persona defaults
	for _, persona := range c.Personas {
		if persona.Temperature == 0 {
			persona.Temperature = 0.7 // Default temperature
		}
		if persona.Priority == 0 {
			persona.Priority = 1 // Default priority
		}
		if persona.Timeout == 0 {
			persona.Timeout = 30 * time.Second // Default timeout
		}
		if persona.MaxTokens == 0 && persona.TokenBudget != "" {
			// Convert string budget to int
			persona.MaxTokens = GetTokenBudgetValue(persona.TokenBudget)
		}
		if persona.MaxTokens == 0 {
			persona.MaxTokens = 5000 // Default token limit
		}
	}

	// Set selection defaults
	if c.Selection == nil {
		c.Selection = &SelectionConfig{}
	}
	if c.Selection.Method == "" {
		c.Selection.Method = "auto"
	}
	if c.Selection.ComplexityThreshold == 0 {
		c.Selection.ComplexityThreshold = 0.3
	}
	if c.Selection.LLMTimeoutMs == 0 {
		c.Selection.LLMTimeoutMs = 200
	}
	if c.Selection.CacheTTL == 0 {
		c.Selection.CacheTTL = time.Hour
	}
	if c.Selection.MaxConcurrentSelections == 0 {
		c.Selection.MaxConcurrentSelections = 10
	}
	if c.Selection.FallbackStrategy == "" {
		c.Selection.FallbackStrategy = "generalist"
	}

	// Set embedding defaults
	if c.Embedding != nil {
		if c.Embedding.Provider == "" {
			c.Embedding.Provider = ProviderLocal // Default to local if not specified
		}
		if c.Embedding.Model == "" {
			switch c.Embedding.Provider {
			case ProviderOpenAI:
				c.Embedding.Model = "text-embedding-3-small"
			case ProviderAzureAI:
				c.Embedding.Model = "text-embedding-ada-002"
			default:
				c.Embedding.Model = "default"
			}
		}
		if c.Embedding.Timeout == 0 {
			c.Embedding.Timeout = 30 * time.Second
		}
		if c.Embedding.MaxRetries == 0 {
			c.Embedding.MaxRetries = 3
		}
		if c.Embedding.CacheTTL == 0 {
			c.Embedding.CacheTTL = 24 * time.Hour // Cache for 24 hours by default
		}
		if c.Embedding.RequestsPerMinute == 0 {
			c.Embedding.RequestsPerMinute = 60 // Default rate limit
		}
		// Default to enabling cache and local fallback
		if !c.Embedding.EnableCache {
			c.Embedding.EnableCache = true
		}
		if !c.Embedding.FallbackToLocal {
			c.Embedding.FallbackToLocal = true
		}
	}
}

// GetPersona retrieves a persona by ID
func (c *Config) GetPersona(id string) (*PersonaConfig, error) {
	persona, exists := c.Personas[id]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrPersonaNotFound, id)
	}
	return persona, nil
}

// ListPersonas returns all personas matching the filter
func (c *Config) ListPersonas(filter *PersonaFilter) []*PersonaConfig {
	var result []*PersonaConfig

	for _, persona := range c.Personas {
		if c.matchesFilter(persona, filter) {
			result = append(result, persona)
		}
	}

	return result
}

// matchesFilter checks if a persona matches the given filter
func (c *Config) matchesFilter(persona *PersonaConfig, filter *PersonaFilter) bool {
	if filter == nil {
		return true
	}

	// Check priority filter
	if filter.MinPriority > 0 && persona.Priority < filter.MinPriority {
		return false
	}

	// Check max tokens filter
	if filter.MaxTokens > 0 && persona.MaxTokens > filter.MaxTokens {
		return false
	}

	// Check capabilities filter
	if len(filter.Capabilities) > 0 {
		if !lo.EveryBy(filter.Capabilities, func(requiredCap string) bool {
			return lo.Contains(persona.Capabilities, requiredCap)
		}) {
			return false
		}
	}

	return true
}

// NewBudgetCalculator creates a new budget calculator
func NewBudgetCalculator() *BudgetCalculator {
	return &BudgetCalculator{
		taskTypeMultipliers: map[string]float64{
			"research":    1.5, // Research tasks need more tokens
			"coding":      1.2, // Programming tasks
			"analysis":    1.8, // Data analysis
			"chat":        0.8, // Simple conversation
			"translation": 1.0, // Translation tasks
			"debugging":   1.3, // Debugging tasks
			"writing":     1.1, // Content writing
		},
		baseTokens: map[string]int{
			"low":    2000,
			"medium": 5000,
			"high":   10000,
			"ultra":  20000,
		},
		userQuotas: make(map[string]int),
	}
}

// GetTokenBudgetValue converts a token budget string to an integer value
func GetTokenBudgetValue(budget string) int {
	calculator := NewBudgetCalculator()
	return calculator.CalculateTokenBudget(budget, "", "")
}

// CalculateTokenBudget calculates the token budget based on multiple factors
func (bc *BudgetCalculator) CalculateTokenBudget(budget string, taskType string, userID string) int {
	// 1. Get base budget
	baseTokens := bc.parseBaseBudget(budget)
	if baseTokens <= 0 {
		return 5000 // Default fallback
	}

	// 2. Apply task type multiplier
	if taskType != "" {
		if multiplier, exists := bc.taskTypeMultipliers[strings.ToLower(taskType)]; exists {
			baseTokens = int(float64(baseTokens) * multiplier)
		}
	}

	// 3. Check user quota limits
	if userID != "" {
		if userQuota, exists := bc.userQuotas[userID]; exists && baseTokens > userQuota {
			baseTokens = userQuota
		}
	}

	return baseTokens
}

// parseBaseBudget parses the base budget string
func (bc *BudgetCalculator) parseBaseBudget(budget string) int {
	if budget == "" {
		return 5000 // Default
	}

	// Check predefined mappings
	if tokens, exists := bc.baseTokens[strings.ToLower(budget)]; exists {
		return tokens
	}

	// Try to parse as direct number
	if tokens, err := strconv.Atoi(budget); err == nil && tokens > 0 {
		return tokens
	}

	// Try to parse as percentage
	if strings.HasSuffix(budget, "%") {
		percentStr := strings.TrimSuffix(budget, "%")
		if percent, err := strconv.ParseFloat(percentStr, 64); err == nil && percent > 0 {
			baseTokens := 5000 // Base for percentage calculation
			return int(float64(baseTokens) * percent / 100)
		}
	}

	// Try to parse as multiplier (e.g., "2x", "1.5x")
	if strings.HasSuffix(strings.ToLower(budget), "x") {
		multiplierStr := strings.TrimSuffix(strings.ToLower(budget), "x")
		if multiplier, err := strconv.ParseFloat(multiplierStr, 64); err == nil && multiplier > 0 {
			baseTokens := 5000 // Base for multiplier calculation
			return int(float64(baseTokens) * multiplier)
		}
	}

	return 5000 // Default fallback
}

// SetUserQuota sets a token quota for a specific user
func (bc *BudgetCalculator) SetUserQuota(userID string, quota int) {
	bc.userQuotas[userID] = quota
}

// GetUserQuota gets the token quota for a specific user
func (bc *BudgetCalculator) GetUserQuota(userID string) (int, bool) {
	quota, exists := bc.userQuotas[userID]
	return quota, exists
}

// UpdateTaskTypeMultiplier updates the multiplier for a specific task type
func (bc *BudgetCalculator) UpdateTaskTypeMultiplier(taskType string, multiplier float64) {
	bc.taskTypeMultipliers[strings.ToLower(taskType)] = multiplier
}

// GetTaskTypeMultiplier gets the multiplier for a specific task type
func (bc *BudgetCalculator) GetTaskTypeMultiplier(taskType string) float64 {
	if multiplier, exists := bc.taskTypeMultipliers[strings.ToLower(taskType)]; exists {
		return multiplier
	}
	return 1.0 // Default multiplier
}

// ReloadConfig reloads configuration from file
func (c *Config) ReloadConfig(path string, logger *zap.Logger) error {
	newConfig, err := LoadConfig(path)
	if err != nil {
		if logger != nil {
			logger.Error("Failed to reload config", zap.Error(err))
		}
		return err
	}

	// Atomic update
	c.Personas = newConfig.Personas
	c.Selection = newConfig.Selection

	if logger != nil {
		logger.Info("Configuration reloaded successfully",
			zap.String("path", path),
			zap.Int("persona_count", len(c.Personas)))
	}

	return nil
}

// DefaultPersonaConfig returns a persona configuration with default values
func DefaultPersonaConfig() *PersonaConfig {
	return &PersonaConfig{
		Temperature:  0.7,
		MaxTokens:    4000,
		Priority:     1,
		Timeout:      30 * time.Second,
		Keywords:     []string{},
		Tools:        []string{},
		Capabilities: []string{},
		Metadata:     make(map[string]interface{}),
	}
}
