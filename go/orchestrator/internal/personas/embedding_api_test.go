package personas

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Note: NewTestMetrics is defined in selector_test.go

// MockEmbeddingAPI implements EmbeddingAPI for testing
type MockEmbeddingAPI struct {
	embeddings   map[string][]float64
	similarities map[string]float64
	failureRate  float64 // 0.0 = never fail, 1.0 = always fail
	callCount    int
	delay        time.Duration
}

func NewMockEmbeddingAPI() *MockEmbeddingAPI {
	return &MockEmbeddingAPI{
		embeddings:   make(map[string][]float64),
		similarities: make(map[string]float64),
		failureRate:  0.0,
	}
}

func (m *MockEmbeddingAPI) SetFailureRate(rate float64) {
	m.failureRate = rate
}

func (m *MockEmbeddingAPI) SetDelay(delay time.Duration) {
	m.delay = delay
}

func (m *MockEmbeddingAPI) GetCallCount() int {
	return m.callCount
}

func (m *MockEmbeddingAPI) GetSimilarity(ctx context.Context, text1, text2 string) (float64, error) {
	m.callCount++

	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	if m.failureRate > 0 && float64(m.callCount%int(1/m.failureRate)) == 0 {
		return 0, fmt.Errorf("mock failure")
	}

	key := fmt.Sprintf("%s|%s", text1, text2)
	if similarity, exists := m.similarities[key]; exists {
		return similarity, nil
	}

	// Generate mock similarity based on text similarity
	if text1 == text2 {
		return 1.0, nil
	}
	if len(text1) == len(text2) {
		return 0.8, nil
	}
	return 0.5, nil
}

func (m *MockEmbeddingAPI) GetEmbedding(ctx context.Context, text string) ([]float64, error) {
	m.callCount++

	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	if m.failureRate > 0 && float64(m.callCount%int(1/m.failureRate)) == 0 {
		return nil, fmt.Errorf("mock failure")
	}

	if embedding, exists := m.embeddings[text]; exists {
		return embedding, nil
	}

	// Generate deterministic mock embedding based on text hash
	embedding := make([]float64, 384)
	hash := 0
	for _, char := range text {
		hash = hash*31 + int(char)
	}

	for i := range embedding {
		embedding[i] = float64((hash+i)%100) / 100.0
	}

	m.embeddings[text] = embedding
	return embedding, nil
}

func TestEmbeddingCache_BasicOperations(t *testing.T) {
	logger := zap.NewNop()
	cache := NewEmbeddingCache(time.Hour, logger)
	defer cache.(*InMemoryEmbeddingCache).Close()

	t.Run("Set and Get embedding", func(t *testing.T) {
		text := "test embedding"
		expectedEmbedding := []float64{0.1, 0.2, 0.3, 0.4, 0.5}

		cache.Set(text, expectedEmbedding)

		retrievedEmbedding, found := cache.Get(text)
		assert.True(t, found)
		assert.Equal(t, expectedEmbedding, retrievedEmbedding)
	})

	t.Run("Set and Get similarity", func(t *testing.T) {
		text1 := "first text"
		text2 := "second text"
		expectedSimilarity := 0.85

		cache.SetSimilarity(text1, text2, expectedSimilarity)

		retrievedSimilarity, found := cache.GetSimilarity(text1, text2)
		assert.True(t, found)
		assert.Equal(t, expectedSimilarity, retrievedSimilarity)

		// Test bidirectional similarity
		retrievedSimilarity2, found2 := cache.GetSimilarity(text2, text1)
		assert.True(t, found2)
		assert.Equal(t, expectedSimilarity, retrievedSimilarity2)
	})

	t.Run("Cache miss", func(t *testing.T) {
		_, found := cache.Get("nonexistent")
		assert.False(t, found)

		_, found = cache.GetSimilarity("nonexistent1", "nonexistent2")
		assert.False(t, found)
	})

	t.Run("Cache size", func(t *testing.T) {
		cache.Clear()
		assert.Equal(t, 0, cache.Size())

		cache.Set("text1", []float64{1, 2, 3})
		cache.Set("text2", []float64{4, 5, 6})
		cache.SetSimilarity("text1", "text2", 0.5)

		assert.Equal(t, 3, cache.Size()) // 2 embeddings + 1 similarity
	})
}

func TestEmbeddingCache_TTLExpiration(t *testing.T) {
	logger := zap.NewNop()
	cache := NewEmbeddingCache(100*time.Millisecond, logger) // Short TTL for testing
	defer cache.(*InMemoryEmbeddingCache).Close()

	text := "expiring text"
	embedding := []float64{1, 2, 3}

	cache.Set(text, embedding)

	// Should be available immediately
	_, found := cache.Get(text)
	assert.True(t, found)

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired now
	_, found = cache.Get(text)
	assert.False(t, found)
}

func TestRateLimiter(t *testing.T) {
	t.Run("Basic rate limiting", func(t *testing.T) {
		limiter := NewRateLimiter(5) // 5 requests per minute

		// First 5 requests should be allowed
		for i := 0; i < 5; i++ {
			assert.True(t, limiter.Allow(), "Request %d should be allowed", i+1)
		}

		// 6th request should be denied
		assert.False(t, limiter.Allow(), "6th request should be denied")
	})

	t.Run("Rate limiter reset", func(t *testing.T) {
		limiter := NewRateLimiter(2)

		// Use up the quota
		assert.True(t, limiter.Allow())
		assert.True(t, limiter.Allow())
		assert.False(t, limiter.Allow())

		// Manually reset the timer
		limiter.lastReset = time.Now().Add(-2 * time.Minute)

		// Should allow requests again
		assert.True(t, limiter.Allow())
	})
}

func TestMultiProviderEmbeddingAPI_WithMocks(t *testing.T) {
	logger := zap.NewNop()
	metrics := NewTestMetrics() // Use proper test metrics

	t.Run("Basic functionality with local fallback", func(t *testing.T) {
		config := &EmbeddingConfig{
			Provider:          ProviderLocal,
			Timeout:           time.Second,
			MaxRetries:        3,
			EnableCache:       true,
			CacheTTL:          time.Hour,
			FallbackToLocal:   true,
			RequestsPerMinute: 60,
		}

		api, err := NewMultiProviderEmbeddingAPI(config, logger, metrics)
		require.NoError(t, err)
		require.NotNil(t, api)

		ctx := context.Background()

		// Test embedding generation
		embedding, err := api.GetEmbedding(ctx, "test text")
		assert.NoError(t, err)
		assert.NotEmpty(t, embedding)

		// Test similarity calculation
		similarity, err := api.GetSimilarity(ctx, "text1", "text2")
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, similarity, 0.0)
		assert.LessOrEqual(t, similarity, 1.0)
	})

	t.Run("Cache functionality", func(t *testing.T) {
		config := &EmbeddingConfig{
			Provider:          ProviderLocal,
			Timeout:           time.Second,
			EnableCache:       true,
			CacheTTL:          time.Hour,
			FallbackToLocal:   true,
			RequestsPerMinute: 60,
		}

		api, err := NewMultiProviderEmbeddingAPI(config, logger, metrics)
		require.NoError(t, err)

		ctx := context.Background()
		text := "cached text"

		// First call should generate embedding
		embedding1, err := api.GetEmbedding(ctx, text)
		assert.NoError(t, err)

		// Second call should return cached result
		embedding2, err := api.GetEmbedding(ctx, text)
		assert.NoError(t, err)
		assert.Equal(t, embedding1, embedding2)

		// Test similarity caching
		similarity1, err := api.GetSimilarity(ctx, "text1", "text2")
		assert.NoError(t, err)

		similarity2, err := api.GetSimilarity(ctx, "text1", "text2")
		assert.NoError(t, err)
		assert.Equal(t, similarity1, similarity2)
	})

	t.Run("Rate limiting", func(t *testing.T) {
		config := &EmbeddingConfig{
			Provider:          ProviderLocal,
			Timeout:           time.Second,
			EnableCache:       false, // Disable cache to test rate limiting
			FallbackToLocal:   true,
			RequestsPerMinute: 2, // Very low limit for testing
		}

		api, err := NewMultiProviderEmbeddingAPI(config, logger, metrics)
		require.NoError(t, err)

		ctx := context.Background()

		// First two requests should succeed
		_, err = api.GetEmbedding(ctx, "text1")
		assert.NoError(t, err)

		_, err = api.GetEmbedding(ctx, "text2")
		assert.NoError(t, err)

		// Third request should be rate limited
		_, err = api.GetEmbedding(ctx, "text3")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "rate limit exceeded")
	})
}

func TestOpenAIEmbeddingAPI_Integration(t *testing.T) {
	// Skip if no API key is provided
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping OpenAI integration test - no API key provided")
	}

	logger := zap.NewNop()
	metrics := &Metrics{}

	config := OpenAIConfig{
		APIKey:  apiKey,
		Model:   "text-embedding-3-small",
		Timeout: 30 * time.Second,
		Retries: 3,
	}

	api, err := NewOpenAIEmbeddingAPI(config, logger, metrics)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("Generate embedding", func(t *testing.T) {
		embedding, err := api.GetEmbedding(ctx, "The quick brown fox jumps over the lazy dog")
		if err != nil {
			// If we get a network or API error, skip the test
			t.Skipf("OpenAI API call failed: %v", err)
		}

		assert.NotEmpty(t, embedding)
		assert.Equal(t, 1536, len(embedding)) // text-embedding-3-small produces 1536-dim vectors
	})

	t.Run("Calculate similarity", func(t *testing.T) {
		similarity, err := api.GetSimilarity(ctx, "cat", "kitten")
		if err != nil {
			t.Skipf("OpenAI API call failed: %v", err)
		}

		assert.GreaterOrEqual(t, similarity, 0.0)
		assert.LessOrEqual(t, similarity, 1.0)
		assert.Greater(t, similarity, 0.5) // "cat" and "kitten" should be quite similar
	})
}

func TestEmbeddingConfig_Validation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorField  string
	}{
		{
			name: "Valid OpenAI config",
			config: &Config{
				Personas: map[string]*PersonaConfig{
					"test": {ID: "test", Description: "test"},
				},
				Embedding: &EmbeddingConfig{
					Provider:          ProviderOpenAI,
					Model:             "text-embedding-3-small",
					Timeout:           30 * time.Second,
					MaxRetries:        3,
					RequestsPerMinute: 60,
				},
			},
			expectError: false,
		},
		{
			name: "Missing provider",
			config: &Config{
				Personas: map[string]*PersonaConfig{
					"test": {ID: "test", Description: "test"},
				},
				Embedding: &EmbeddingConfig{
					Timeout:    30 * time.Second,
					MaxRetries: 3,
				},
			},
			expectError: true,
			errorField:  "provider",
		},
		{
			name: "Invalid provider",
			config: &Config{
				Personas: map[string]*PersonaConfig{
					"test": {ID: "test", Description: "test"},
				},
				Embedding: &EmbeddingConfig{
					Provider:   "invalid",
					Timeout:    30 * time.Second,
					MaxRetries: 3,
				},
			},
			expectError: true,
			errorField:  "provider",
		},
		{
			name: "Negative timeout",
			config: &Config{
				Personas: map[string]*PersonaConfig{
					"test": {ID: "test", Description: "test"},
				},
				Embedding: &EmbeddingConfig{
					Provider:   ProviderOpenAI,
					Timeout:    -1 * time.Second,
					MaxRetries: 3,
				},
			},
			expectError: true,
			errorField:  "timeout",
		},
		{
			name: "Negative max retries",
			config: &Config{
				Personas: map[string]*PersonaConfig{
					"test": {ID: "test", Description: "test"},
				},
				Embedding: &EmbeddingConfig{
					Provider:   ProviderOpenAI,
					Timeout:    30 * time.Second,
					MaxRetries: -1,
				},
			},
			expectError: true,
			errorField:  "max_retries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorField != "" {
					assert.Contains(t, err.Error(), tt.errorField)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float64
		b        []float64
		expected float64
	}{
		{
			name:     "Identical vectors",
			a:        []float64{1, 2, 3},
			b:        []float64{1, 2, 3},
			expected: 1.0,
		},
		{
			name:     "Orthogonal vectors",
			a:        []float64{1, 0},
			b:        []float64{0, 1},
			expected: 0.0,
		},
		{
			name:     "Opposite vectors",
			a:        []float64{1, 2, 3},
			b:        []float64{-1, -2, -3},
			expected: -1.0,
		},
		{
			name:     "Different length vectors",
			a:        []float64{1, 2, 3},
			b:        []float64{1, 2},
			expected: 0.0, // Should return 0 for mismatched lengths
		},
		{
			name:     "Zero vectors",
			a:        []float64{0, 0, 0},
			b:        []float64{1, 2, 3},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cosineSimilarity(tt.a, tt.b)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}

func BenchmarkEmbeddingCache(b *testing.B) {
	logger := zap.NewNop()
	cache := NewEmbeddingCache(time.Hour, logger)
	defer cache.(*InMemoryEmbeddingCache).Close()

	embedding := make([]float64, 384)
	for i := range embedding {
		embedding[i] = float64(i) / 384.0
	}

	b.ResetTimer()

	b.Run("Set", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache.Set(fmt.Sprintf("text_%d", i), embedding)
		}
	})

	b.Run("Get", func(b *testing.B) {
		// Pre-populate cache
		for i := 0; i < 1000; i++ {
			cache.Set(fmt.Sprintf("text_%d", i), embedding)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cache.Get(fmt.Sprintf("text_%d", i%1000))
		}
	})

	b.Run("SetSimilarity", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache.SetSimilarity(fmt.Sprintf("text_%d", i), fmt.Sprintf("text_%d", i+1), 0.5)
		}
	})

	b.Run("GetSimilarity", func(b *testing.B) {
		// Pre-populate cache
		for i := 0; i < 1000; i++ {
			cache.SetSimilarity(fmt.Sprintf("text_%d", i), fmt.Sprintf("text_%d", i+1), 0.5)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cache.GetSimilarity(fmt.Sprintf("text_%d", i%1000), fmt.Sprintf("text_%d", (i%1000)+1))
		}
	})
}

func BenchmarkCosineSimilarity(b *testing.B) {
	a := make([]float64, 384)
	bb := make([]float64, 384)

	for i := range a {
		a[i] = float64(i) / 384.0
		bb[i] = float64(i+1) / 384.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cosineSimilarity(a, bb)
	}
}
