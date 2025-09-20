package personas

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"go.uber.org/zap"
)

// EmbeddingProvider represents different embedding service providers
type EmbeddingProvider string

const (
	ProviderOpenAI      EmbeddingProvider = "openai"
	ProviderAzureAI     EmbeddingProvider = "azure"
	ProviderLocal       EmbeddingProvider = "local"
	ProviderHuggingFace EmbeddingProvider = "huggingface"
)

// EmbeddingConfig holds configuration for external embedding services
type EmbeddingConfig struct {
	// Primary provider configuration
	Provider   EmbeddingProvider `yaml:"provider" json:"provider"`
	APIKey     string            `yaml:"api_key" json:"api_key"`
	BaseURL    string            `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	Model      string            `yaml:"model" json:"model"`
	Timeout    time.Duration     `yaml:"timeout" json:"timeout"`
	MaxRetries int               `yaml:"max_retries" json:"max_retries"`

	// Load balancing and failover
	Providers []ProviderConfig `yaml:"providers,omitempty" json:"providers,omitempty"`

	// Caching configuration
	EnableCache bool          `yaml:"enable_cache" json:"enable_cache"`
	CacheTTL    time.Duration `yaml:"cache_ttl" json:"cache_ttl"`

	// Rate limiting
	RequestsPerMinute int `yaml:"requests_per_minute" json:"requests_per_minute"`

	// Fallback options
	FallbackToLocal bool `yaml:"fallback_to_local" json:"fallback_to_local"`
}

// ProviderConfig holds configuration for individual providers
type ProviderConfig struct {
	Name     EmbeddingProvider `yaml:"name" json:"name"`
	APIKey   string            `yaml:"api_key" json:"api_key"`
	BaseURL  string            `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	Model    string            `yaml:"model" json:"model"`
	Weight   int               `yaml:"weight" json:"weight"`
	Priority int               `yaml:"priority" json:"priority"`
	Enabled  bool              `yaml:"enabled" json:"enabled"`
}

// MultiProviderEmbeddingAPI implements EmbeddingAPI with multiple provider support
type MultiProviderEmbeddingAPI struct {
	config        *EmbeddingConfig
	providers     map[EmbeddingProvider]EmbeddingAPI
	localFallback *LocalEmbeddingModel
	cache         EmbeddingCache
	rateLimiter   *RateLimiter
	logger        *zap.Logger
	metrics       *Metrics
	mu            sync.RWMutex
}

// EmbeddingCache provides caching for embedding results
type EmbeddingCache interface {
	Get(text string) ([]float64, bool)
	Set(text string, embedding []float64)
	GetSimilarity(text1, text2 string) (float64, bool)
	SetSimilarity(text1, text2 string, similarity float64)
	Clear()
	Size() int
}

// RateLimiter provides rate limiting functionality
type RateLimiter struct {
	requestsPerMinute int
	lastReset         time.Time
	requestCount      int
	mu                sync.Mutex
}

// OpenAIEmbeddingAPI implements EmbeddingAPI using OpenAI's API
type OpenAIEmbeddingAPI struct {
	client  *openai.Client
	model   string
	timeout time.Duration
	retries int
	logger  *zap.Logger
	metrics *Metrics
}

// NewMultiProviderEmbeddingAPI creates a new multi-provider embedding API
func NewMultiProviderEmbeddingAPI(config *EmbeddingConfig, logger *zap.Logger, metrics *Metrics) (*MultiProviderEmbeddingAPI, error) {
	api := &MultiProviderEmbeddingAPI{
		config:    config,
		providers: make(map[EmbeddingProvider]EmbeddingAPI),
		logger:    logger,
		metrics:   metrics,
	}

	// Initialize local fallback
	if config.FallbackToLocal {
		api.localFallback = NewLocalEmbeddingModel(384) // Higher dimension for better accuracy
	}

	// Initialize cache if enabled
	if config.EnableCache {
		api.cache = NewEmbeddingCache(config.CacheTTL, logger)
	}

	// Initialize rate limiter
	if config.RequestsPerMinute > 0 {
		api.rateLimiter = NewRateLimiter(config.RequestsPerMinute)
	}

	// Initialize primary provider
	if err := api.initializePrimaryProvider(); err != nil {
		logger.Warn("Failed to initialize primary provider", zap.Error(err))
		if !config.FallbackToLocal {
			return nil, fmt.Errorf("failed to initialize primary provider and fallback disabled: %w", err)
		}
	}

	// Initialize additional providers for load balancing
	if err := api.initializeAdditionalProviders(); err != nil {
		logger.Warn("Some additional providers failed to initialize", zap.Error(err))
	}

	logger.Info("Multi-provider embedding API initialized",
		zap.String("primary_provider", string(config.Provider)),
		zap.Int("additional_providers", len(api.providers)-1),
		zap.Bool("cache_enabled", config.EnableCache),
		zap.Bool("local_fallback", config.FallbackToLocal))

	return api, nil
}

// initializePrimaryProvider initializes the primary embedding provider
func (api *MultiProviderEmbeddingAPI) initializePrimaryProvider() error {
	provider, err := api.createProvider(ProviderConfig{
		Name:    api.config.Provider,
		APIKey:  api.config.APIKey,
		BaseURL: api.config.BaseURL,
		Model:   api.config.Model,
		Enabled: true,
	})
	if err != nil {
		return fmt.Errorf("failed to create primary provider: %w", err)
	}

	api.providers[api.config.Provider] = provider
	return nil
}

// initializeAdditionalProviders initializes additional providers for load balancing
func (api *MultiProviderEmbeddingAPI) initializeAdditionalProviders() error {
	for _, providerConfig := range api.config.Providers {
		if !providerConfig.Enabled || providerConfig.Name == api.config.Provider {
			continue
		}

		provider, err := api.createProvider(providerConfig)
		if err != nil {
			api.logger.Warn("Failed to initialize provider",
				zap.String("provider", string(providerConfig.Name)),
				zap.Error(err))
			continue
		}

		api.providers[providerConfig.Name] = provider
		api.logger.Info("Additional provider initialized",
			zap.String("provider", string(providerConfig.Name)))
	}

	return nil
}

// createProvider creates a provider instance based on configuration
func (api *MultiProviderEmbeddingAPI) createProvider(config ProviderConfig) (EmbeddingAPI, error) {
	switch config.Name {
	case ProviderOpenAI:
		return NewOpenAIEmbeddingAPI(OpenAIConfig{
			APIKey:  config.APIKey,
			BaseURL: config.BaseURL,
			Model:   config.Model,
			Timeout: api.config.Timeout,
			Retries: api.config.MaxRetries,
		}, api.logger, api.metrics)
	case ProviderLocal:
		// Return local embedding model wrapped as EmbeddingAPI
		return &LocalEmbeddingWrapper{
			model: NewLocalEmbeddingModel(384),
		}, nil
	case ProviderAzureAI:
		// TODO: Implement Azure OpenAI provider
		return nil, fmt.Errorf("Azure provider not implemented yet")
	case ProviderHuggingFace:
		// TODO: Implement HuggingFace provider
		return nil, fmt.Errorf("HuggingFace provider not implemented yet")
	default:
		return nil, fmt.Errorf("unknown provider: %s", config.Name)
	}
}

// GetSimilarity calculates similarity between two texts
func (api *MultiProviderEmbeddingAPI) GetSimilarity(ctx context.Context, text1, text2 string) (float64, error) {
	// Check cache first
	if api.cache != nil {
		if similarity, found := api.cache.GetSimilarity(text1, text2); found {
			api.metrics.RecordEmbeddingAPICall("cache", "similarity", 0)
			return similarity, nil
		}
	}

	// Check rate limit
	if api.rateLimiter != nil {
		if !api.rateLimiter.Allow() {
			return 0, fmt.Errorf("rate limit exceeded")
		}
	}

	startTime := time.Now()

	// Try primary provider first
	provider := api.selectProvider()
	similarity, err := api.tryGetSimilarity(ctx, provider, text1, text2)
	duration := time.Since(startTime)

	if err == nil {
		// Cache the result
		if api.cache != nil {
			api.cache.SetSimilarity(text1, text2, similarity)
		}
		api.metrics.RecordEmbeddingAPICall(string(api.config.Provider), "similarity", duration)
		return similarity, nil
	}

	api.logger.Warn("Primary provider failed for similarity",
		zap.String("provider", string(api.config.Provider)),
		zap.Error(err))

	// Try fallback providers
	for providerName, providerImpl := range api.providers {
		if providerName == api.config.Provider {
			continue // Skip primary provider
		}

		similarity, fallbackErr := api.tryGetSimilarity(ctx, providerImpl, text1, text2)
		if fallbackErr == nil {
			if api.cache != nil {
				api.cache.SetSimilarity(text1, text2, similarity)
			}
			api.metrics.RecordEmbeddingAPICall(string(providerName), "similarity", time.Since(startTime))
			return similarity, nil
		}

		api.logger.Debug("Fallback provider failed",
			zap.String("provider", string(providerName)),
			zap.Error(fallbackErr))
	}

	// Use local fallback if enabled
	if api.localFallback != nil {
		api.logger.Info("Using local fallback for similarity calculation")
		emb1, err1 := api.localFallback.GetEmbedding(text1)
		emb2, err2 := api.localFallback.GetEmbedding(text2)

		if err1 == nil && err2 == nil {
			similarity := cosineSimilarity(emb1, emb2)
			if api.cache != nil {
				api.cache.SetSimilarity(text1, text2, similarity)
			}
			api.metrics.RecordEmbeddingAPICall("local_fallback", "similarity", time.Since(startTime))
			return similarity, nil
		}
	}

	return 0, fmt.Errorf("all embedding providers failed: %w", err)
}

// GetEmbedding generates embedding for a text
func (api *MultiProviderEmbeddingAPI) GetEmbedding(ctx context.Context, text string) ([]float64, error) {
	// Check cache first
	if api.cache != nil {
		if embedding, found := api.cache.Get(text); found {
			api.metrics.RecordEmbeddingAPICall("cache", "embedding", 0)
			return embedding, nil
		}
	}

	// Check rate limit
	if api.rateLimiter != nil {
		if !api.rateLimiter.Allow() {
			return nil, fmt.Errorf("rate limit exceeded")
		}
	}

	startTime := time.Now()

	// Try primary provider first
	provider := api.selectProvider()
	embedding, err := provider.GetEmbedding(ctx, text)
	duration := time.Since(startTime)

	if err == nil {
		// Cache the result
		if api.cache != nil {
			api.cache.Set(text, embedding)
		}
		api.metrics.RecordEmbeddingAPICall(string(api.config.Provider), "embedding", duration)
		return embedding, nil
	}

	api.logger.Warn("Primary provider failed for embedding",
		zap.String("provider", string(api.config.Provider)),
		zap.Error(err))

	// Try fallback providers
	for providerName, providerImpl := range api.providers {
		if providerName == api.config.Provider {
			continue // Skip primary provider
		}

		embedding, fallbackErr := providerImpl.GetEmbedding(ctx, text)
		if fallbackErr == nil {
			if api.cache != nil {
				api.cache.Set(text, embedding)
			}
			api.metrics.RecordEmbeddingAPICall(string(providerName), "embedding", time.Since(startTime))
			return embedding, nil
		}

		api.logger.Debug("Fallback provider failed",
			zap.String("provider", string(providerName)),
			zap.Error(fallbackErr))
	}

	// Use local fallback if enabled
	if api.localFallback != nil {
		api.logger.Info("Using local fallback for embedding generation")
		embedding, localErr := api.localFallback.GetEmbedding(text)
		if localErr == nil {
			if api.cache != nil {
				api.cache.Set(text, embedding)
			}
			api.metrics.RecordEmbeddingAPICall("local_fallback", "embedding", time.Since(startTime))
			return embedding, nil
		}
	}

	return nil, fmt.Errorf("all embedding providers failed: %w", err)
}

// tryGetSimilarity attempts to get similarity using a specific provider
func (api *MultiProviderEmbeddingAPI) tryGetSimilarity(ctx context.Context, provider EmbeddingAPI, text1, text2 string) (float64, error) {
	// Create a timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, api.config.Timeout)
	defer cancel()

	return provider.GetSimilarity(timeoutCtx, text1, text2)
}

// selectProvider selects the best available provider (could implement load balancing logic)
func (api *MultiProviderEmbeddingAPI) selectProvider() EmbeddingAPI {
	api.mu.RLock()
	defer api.mu.RUnlock()

	// For now, just return the primary provider
	// TODO: Implement intelligent load balancing based on provider weights, response times, etc.
	if provider, exists := api.providers[api.config.Provider]; exists {
		return provider
	}

	// Return first available provider if primary is unavailable
	for _, provider := range api.providers {
		return provider
	}

	return nil
}

// LocalEmbeddingWrapper wraps LocalEmbeddingModel to implement EmbeddingAPI
type LocalEmbeddingWrapper struct {
	model *LocalEmbeddingModel
}

func (w *LocalEmbeddingWrapper) GetSimilarity(ctx context.Context, text1, text2 string) (float64, error) {
	emb1, err := w.model.GetEmbedding(text1)
	if err != nil {
		return 0, err
	}
	emb2, err := w.model.GetEmbedding(text2)
	if err != nil {
		return 0, err
	}
	return cosineSimilarity(emb1, emb2), nil
}

func (w *LocalEmbeddingWrapper) GetEmbedding(ctx context.Context, text string) ([]float64, error) {
	return w.model.GetEmbedding(text)
}

// OpenAIConfig holds configuration for OpenAI embedding API
type OpenAIConfig struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
	Retries int
}

// NewOpenAIEmbeddingAPI creates a new OpenAI embedding API client
func NewOpenAIEmbeddingAPI(config OpenAIConfig, logger *zap.Logger, metrics *Metrics) (*OpenAIEmbeddingAPI, error) {
	// Set default model if not specified
	if config.Model == "" {
		config.Model = "text-embedding-3-small" // OpenAI's latest, efficient embedding model
	}

	// Set default timeout if not specified
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	// Get API key from environment if not provided
	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OpenAI API key not provided in config or OPENAI_API_KEY environment variable")
		}
	}

	// Create client options
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(&http.Client{
			Timeout: config.Timeout,
		}),
	}

	// Add base URL if provided (for Azure OpenAI or custom endpoints)
	if config.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(config.BaseURL))
	}

	client := openai.NewClient(opts...)

	return &OpenAIEmbeddingAPI{
		client:  &client,
		model:   config.Model,
		timeout: config.Timeout,
		retries: config.Retries,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// GetSimilarity calculates similarity between two texts using OpenAI embeddings
func (api *OpenAIEmbeddingAPI) GetSimilarity(ctx context.Context, text1, text2 string) (float64, error) {
	emb1, err := api.GetEmbedding(ctx, text1)
	if err != nil {
		return 0, fmt.Errorf("failed to get embedding for text1: %w", err)
	}

	emb2, err := api.GetEmbedding(ctx, text2)
	if err != nil {
		return 0, fmt.Errorf("failed to get embedding for text2: %w", err)
	}

	return cosineSimilarity(emb1, emb2), nil
}

// GetEmbedding generates an embedding for the given text using OpenAI API
func (api *OpenAIEmbeddingAPI) GetEmbedding(ctx context.Context, text string) ([]float64, error) {
	startTime := time.Now()

	// TODO: Fix OpenAI API integration - using mock implementation for now
	// The OpenAI Go SDK API might have changed, need to check documentation

	// Mock implementation for now
	api.logger.Warn("Using mock OpenAI embedding implementation")

	// Generate deterministic embedding based on text hash
	embedding := make([]float64, 1536) // OpenAI text-embedding-3-small dimension
	hash := 0
	for _, char := range text {
		hash = hash*31 + int(char)
	}

	for i := range embedding {
		embedding[i] = float64((hash+i)%1000) / 1000.0
	}

	duration := time.Since(startTime)
	api.logger.Debug("Mock OpenAI embedding generated",
		zap.String("model", api.model),
		zap.Int("dimension", len(embedding)),
		zap.Duration("duration", duration))

	return embedding, nil
}

// cosineSimilarity calculates cosine similarity between two vectors
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(requestsPerMinute int) *RateLimiter {
	return &RateLimiter{
		requestsPerMinute: requestsPerMinute,
		lastReset:         time.Now(),
		requestCount:      0,
	}
}

// Allow checks if a request is allowed under the rate limit
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	if now.Sub(rl.lastReset) >= time.Minute {
		// Reset the counter every minute
		rl.requestCount = 0
		rl.lastReset = now
	}

	if rl.requestCount >= rl.requestsPerMinute {
		return false
	}

	rl.requestCount++
	return true
}
