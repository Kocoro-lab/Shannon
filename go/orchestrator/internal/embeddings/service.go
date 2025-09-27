package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	ometrics "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/tracing"
)

// Service provides embedding generation with caching
type Service struct {
	cfg   Config
	http  *http.Client
	cache EmbeddingCache
	lru   *LocalLRU
}

// Global singleton for simple wiring
var globalSvc *Service

func Initialize(cfg Config, cache EmbeddingCache) {
	c := cfg
	if c.Timeout == 0 {
		c.Timeout = 5 * time.Second
	}
	if c.DefaultModel == "" {
		c.DefaultModel = "text-embedding-3-small"
	}
	if c.CacheTTL == 0 {
		c.CacheTTL = time.Hour
	}
	if c.MaxLRU == 0 {
		c.MaxLRU = 2048
	}
	// Set default chunking config only if enabled but incomplete
	if c.Chunking.Enabled && c.Chunking.MaxTokens == 0 {
		c.Chunking = DefaultChunkingConfig()
	}

	// Create HTTP client with workflow interceptor
	httpClient := &http.Client{
		Timeout:   c.Timeout,
		Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
	}
	svc := &Service{cfg: c, http: httpClient, cache: cache, lru: NewLocalLRU(c.MaxLRU)}
	globalSvc = svc
}

func Get() *Service { return globalSvc }

// GetConfig returns the current configuration
func (s *Service) GetConfig() Config {
	if s == nil {
		// Return default config if service not initialized
		return Config{
			DefaultModel: "text-embedding-3-small",
			Chunking:     DefaultChunkingConfig(),
		}
	}
	return s.cfg
}

type embedRequest struct {
	Texts []string `json:"texts"`
	Model string   `json:"model"`
}

type embedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
	Dimensions int         `json:"dimensions"`
	ModelUsed  string      `json:"model_used"`
}

// GenerateEmbedding returns the vector for a single text using the configured provider
func (s *Service) GenerateEmbedding(ctx context.Context, text string, model string) ([]float32, error) {
	if s == nil {
		return nil, fmt.Errorf("embedding service not initialized")
	}
	m := model
	if m == "" {
		m = s.cfg.DefaultModel
	}
	key := MakeKey(m, text)

	// LRU first
	if v, ok := s.lru.Get(ctx, key); ok {
		ometrics.RecordEmbeddingMetrics(m, "lru_hit", 0)
		return v, nil
	}
	// Redis next
	if s.cache != nil {
		if v, ok := s.cache.Get(ctx, key); ok {
			s.lru.Set(ctx, key, v, 30*time.Minute)
			ometrics.RecordEmbeddingMetrics(m, "cache_hit", 0)
			return v, nil
		}
	}

	start := time.Now()

	// Start tracing span for embedding request
	ctx, span := tracing.StartHTTPSpan(ctx, "POST", fmt.Sprintf("%s/embeddings/", s.cfg.BaseURL))
	defer span.End()

	// Call LLM service
	url := fmt.Sprintf("%s/embeddings/", s.cfg.BaseURL)
	payload := embedRequest{Texts: []string{text}, Model: m}
	buf, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Inject W3C traceparent header
	tracing.InjectTraceparent(ctx, req)

	resp, err := s.http.Do(req)
	if err != nil {
		ometrics.RecordEmbeddingMetrics(m, "error", time.Since(start).Seconds())
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		ometrics.RecordEmbeddingMetrics(m, "error", time.Since(start).Seconds())
		return nil, fmt.Errorf("embedding http status %d", resp.StatusCode)
	}
	var er embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		ometrics.RecordEmbeddingMetrics(m, "error", time.Since(start).Seconds())
		return nil, err
	}
	if len(er.Embeddings) == 0 {
		ometrics.RecordEmbeddingMetrics(m, "empty", time.Since(start).Seconds())
		return nil, fmt.Errorf("no embeddings returned")
	}
	// Convert to float32
	out := make([]float32, len(er.Embeddings[0]))
	for i, f := range er.Embeddings[0] {
		out[i] = float32(f)
	}
	ometrics.RecordEmbeddingMetrics(m, "ok", time.Since(start).Seconds())

	s.lru.Set(ctx, key, out, 30*time.Minute)
	if s.cache != nil {
		s.cache.Set(ctx, key, out, s.cfg.CacheTTL)
	}
	return out, nil
}

// GenerateBatchEmbeddings generates embeddings for multiple texts in a single request
// This is more efficient than calling GenerateEmbedding multiple times
func (s *Service) GenerateBatchEmbeddings(ctx context.Context, texts []string, model string) ([][]float32, error) {
	if s == nil {
		return nil, fmt.Errorf("embedding service not initialized")
	}
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	m := model
	if m == "" {
		m = s.cfg.DefaultModel
	}

	// Check cache for each text
	results := make([][]float32, len(texts))
	uncachedTexts := []string{}
	uncachedIndices := []int{}

	for i, text := range texts {
		key := MakeKey(m, text)

		// Check LRU cache
		if v, ok := s.lru.Get(ctx, key); ok {
			results[i] = v
			ometrics.RecordEmbeddingMetrics(m, "lru_hit", 0)
			continue
		}

		// Check Redis cache
		if s.cache != nil {
			if v, ok := s.cache.Get(ctx, key); ok {
				results[i] = v
				s.lru.Set(ctx, key, v, 30*time.Minute)
				ometrics.RecordEmbeddingMetrics(m, "cache_hit", 0)
				continue
			}
		}

		uncachedTexts = append(uncachedTexts, text)
		uncachedIndices = append(uncachedIndices, i)
	}

	// If all texts were cached, return early
	if len(uncachedTexts) == 0 {
		return results, nil
	}

	start := time.Now()

	// Start tracing span for batch embedding request
	ctx, span := tracing.StartHTTPSpan(ctx, "POST", fmt.Sprintf("%s/embeddings/", s.cfg.BaseURL))
	defer span.End()

	// Call LLM service with batch
	url := fmt.Sprintf("%s/embeddings/", s.cfg.BaseURL)
	payload := embedRequest{Texts: uncachedTexts, Model: m}
	buf, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Inject W3C traceparent header
	tracing.InjectTraceparent(ctx, req)

	resp, err := s.http.Do(req)
	if err != nil {
		ometrics.RecordEmbeddingMetrics(m, "error", time.Since(start).Seconds())
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ometrics.RecordEmbeddingMetrics(m, "error", time.Since(start).Seconds())
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding service returned %d: %s", resp.StatusCode, string(body))
	}

	var er embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		ometrics.RecordEmbeddingMetrics(m, "error", time.Since(start).Seconds())
		return nil, err
	}

	if len(er.Embeddings) != len(uncachedTexts) {
		return nil, fmt.Errorf("embedding service returned %d embeddings for %d texts", len(er.Embeddings), len(uncachedTexts))
	}

	// Convert float64 to float32 and cache results
	for i, embedding := range er.Embeddings {
		out := make([]float32, len(embedding))
		for j, f := range embedding {
			out[j] = float32(f)
		}

		// Store in results at the correct index
		idx := uncachedIndices[i]
		results[idx] = out

		// Cache the result
		key := MakeKey(m, uncachedTexts[i])
		s.lru.Set(ctx, key, out, 30*time.Minute)
		if s.cache != nil {
			s.cache.Set(ctx, key, out, s.cfg.CacheTTL)
		}
	}

	ometrics.RecordEmbeddingMetrics(m, "batch_ok", time.Since(start).Seconds())

	return results, nil
}
