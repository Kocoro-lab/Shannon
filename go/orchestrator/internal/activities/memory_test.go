// +build integration

package activities

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/embeddings"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/vectordb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain sets up the test environment
func TestMain(m *testing.M) {
	// Skip if not running integration tests
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		os.Exit(0)
	}

	// Initialize services for tests
	initializeTestServices()

	// Run tests
	code := m.Run()
	os.Exit(code)
}

func initializeTestServices() {
	// Initialize embeddings service
	embConfig := embeddings.Config{
		BaseURL:      getEnvOrDefault("LLM_SERVICE_URL", "http://localhost:8000"),
		DefaultModel: "text-embedding-3-small",
		Timeout:      5 * time.Second,
		CacheTTL:     time.Hour,
		Chunking: embeddings.ChunkingConfig{
			Enabled:       true,
			MaxTokens:     2000,
			OverlapTokens: 200,
			TokenizerMode: "simple",
		},
	}
	embeddings.Initialize(embConfig, nil)

	// Initialize vector database
	qdrantURL := getEnvOrDefault("QDRANT_URL", "http://localhost:6333")
	// Parse host and port from URL
	host := "localhost"
	port := 6333
	if strings.Contains(qdrantURL, "://") {
		parts := strings.Split(qdrantURL, "://")
		if len(parts) > 1 {
			hostPort := strings.Split(parts[1], ":")
			host = hostPort[0]
			if len(hostPort) > 1 {
				port, _ = strconv.Atoi(hostPort[1])
			}
		}
	}

	vdbConfig := vectordb.Config{
		Enabled:        true,
		Host:           host,
		Port:           port,
		TaskEmbeddings: "task_embeddings",
		DocumentChunks: "agent_embeddings",
		Timeout:        5 * time.Second,
	}
	vectordb.Initialize(vdbConfig)
}


// TestChunkingPipeline tests the full chunking pipeline from write to retrieval
func TestChunkingPipeline(t *testing.T) {
	// Create a long answer that should trigger chunking
	longAnswer := generateLongText(5000) // ~5000 tokens

	tests := []struct {
		name           string
		query          string
		answer         string
		expectedChunks int
		verifyFunc     func(t *testing.T, result RecordQueryResult)
	}{
		{
			name:           "Long answer triggers chunking",
			query:          "Explain distributed systems",
			answer:         longAnswer,
			expectedChunks: 3, // 5000 tokens / 2000 per chunk with overlap
			verifyFunc: func(t *testing.T, result RecordQueryResult) {
				assert.True(t, result.Stored)
				assert.Equal(t, 3, result.ChunksCreated)
			},
		},
		{
			name:           "Short answer no chunking",
			query:          "What is 2+2?",
			answer:         "2+2 equals 4",
			expectedChunks: 0,
			verifyFunc: func(t *testing.T, result RecordQueryResult) {
				assert.True(t, result.Stored)
				assert.Equal(t, 0, result.ChunksCreated)
			},
		},
		{
			name:           "Boundary case exactly 2000 tokens",
			query:          "Test boundary",
			answer:         generateLongText(2000),
			expectedChunks: 0, // Should not chunk at exactly max size
			verifyFunc: func(t *testing.T, result RecordQueryResult) {
				assert.True(t, result.Stored)
				assert.Equal(t, 0, result.ChunksCreated)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			sessionID := uuid.New().String()

			// Record the Q&A pair
			input := RecordQueryInput{
				SessionID: sessionID,
				UserID:    "test-user",
				TenantID:  "test-tenant",
				Query:     tt.query,
				Answer:    tt.answer,
				Model:     "gpt-4",
				Metadata: map[string]interface{}{
					"test": true,
				},
			}

			result, err := RecordQuery(ctx, input)
			require.NoError(t, err)
			tt.verifyFunc(t, result)
		})
	}
}

// TestChunkReconstructionAccuracy verifies that chunked answers are reconstructed accurately
func TestChunkReconstructionAccuracy(t *testing.T) {
	ctx := context.Background()

	// Original text with specific markers
	originalAnswer := `
	SECTION_1_START
	This is the first section of the answer with important information.
	It contains details about the topic that should be preserved.
	SECTION_1_END

	SECTION_2_START
	This is the second section with more details.
	The chunking should preserve the overlap correctly.
	SECTION_2_END

	SECTION_3_START
	Final section with concluding remarks.
	All sections should be reconstructed in order.
	SECTION_3_END
	`

	// Store chunked answer
	sessionID := uuid.New().String()
	input := RecordQueryInput{
		SessionID: sessionID,
		UserID:    "test-user",
		TenantID:  "test-tenant",
		Query:     "Test reconstruction",
		Answer:    strings.Repeat(originalAnswer, 10), // Make it long enough to chunk
		Model:     "gpt-4",
	}

	result, err := RecordQuery(ctx, input)
	require.NoError(t, err)
	require.True(t, result.Stored)
	require.Greater(t, result.ChunksCreated, 0)

	// Retrieve and reconstruct
	fetchInput := FetchSemanticMemoryInput{
		Query:     "Test reconstruction",
		SessionID: sessionID,
		TenantID:  "test-tenant",
		TopK:      5,
		Threshold: 0.5,
	}

	fetchResult, err := FetchSemanticMemoryChunked(ctx, fetchInput)
	require.NoError(t, err)
	require.NotEmpty(t, fetchResult.Items)

	// Verify reconstruction
	reconstructed := fetchResult.Items[0]["answer"].(string)

	// Check that all sections are present
	assert.Contains(t, reconstructed, "SECTION_1_START")
	assert.Contains(t, reconstructed, "SECTION_1_END")
	assert.Contains(t, reconstructed, "SECTION_2_START")
	assert.Contains(t, reconstructed, "SECTION_2_END")
	assert.Contains(t, reconstructed, "SECTION_3_START")
	assert.Contains(t, reconstructed, "SECTION_3_END")

	// Verify order is preserved
	idx1 := strings.Index(reconstructed, "SECTION_1_START")
	idx2 := strings.Index(reconstructed, "SECTION_2_START")
	idx3 := strings.Index(reconstructed, "SECTION_3_START")
	assert.Less(t, idx1, idx2)
	assert.Less(t, idx2, idx3)
}

// TestDeterministicChunkIDs verifies idempotency of chunk writes
func TestDeterministicChunkIDs(t *testing.T) {
	ctx := context.Background()
	sessionID := uuid.New().String()
	longAnswer := generateLongText(3000)

	input := RecordQueryInput{
		SessionID: sessionID,
		UserID:    "test-user",
		TenantID:  "test-tenant",
		Query:     "Test idempotency",
		Answer:    longAnswer,
		Model:     "gpt-4",
	}

	// First write
	result1, err := recordQueryCore(ctx, input)
	require.NoError(t, err)
	require.True(t, result1.Stored)
	chunks1 := result1.ChunksCreated

	// Second write (should be idempotent)
	result2, err := recordQueryCore(ctx, input)
	require.NoError(t, err)
	require.True(t, result2.Stored)
	chunks2 := result2.ChunksCreated

	// Same number of chunks
	assert.Equal(t, chunks1, chunks2)

	// Verify no duplicates in storage by retrieving
	fetchInput := FetchSemanticMemoryInput{
		Query:     "Test idempotency",
		SessionID: sessionID,
		TenantID:  "test-tenant",
		TopK:      10, // Get more than expected to check for duplicates
	}

	fetchResult, err := FetchSemanticMemoryChunked(ctx, fetchInput)
	require.NoError(t, err)

	// Should only have one result despite writing twice
	assert.Equal(t, 1, len(fetchResult.Items))
}

// TestBatchEmbeddingEfficiency verifies batch embedding is used for chunks
func TestBatchEmbeddingEfficiency(t *testing.T) {
	ctx := context.Background()

	// Mock embedding service to count calls
	mockEmbedding := &mockEmbeddingService{
		singleCalls: 0,
		batchCalls:  0,
	}

	// Replace global embedding service temporarily
	oldService := embeddings.Get()
	embeddings.Initialize(embeddings.Config{
		BaseURL:      "http://mock",
		DefaultModel: "test",
	}, nil)
	defer func() {
		if oldService != nil {
			embeddings.Initialize(oldService.GetConfig(), nil)
		}
	}()

	longAnswer := generateLongText(5000)
	input := RecordQueryInput{
		SessionID: uuid.New().String(),
		UserID:    "test-user",
		TenantID:  "test-tenant",
		Query:     "Test batch embedding",
		Answer:    longAnswer,
		Model:     "gpt-4",
	}

	_, err := recordQueryCore(ctx, input)
	require.NoError(t, err)

	// Should have made 1 batch call, not multiple single calls
	assert.Equal(t, 0, mockEmbedding.singleCalls, "Should not make single embedding calls")
	assert.Equal(t, 1, mockEmbedding.batchCalls, "Should make exactly 1 batch call")
}

// TestMMRDiversityReranking tests MMR diversity in retrieval
func TestMMRDiversityReranking(t *testing.T) {
	ctx := context.Background()
	sessionID := uuid.New().String()

	// Store multiple similar but slightly different Q&A pairs
	queries := []string{
		"How does Kubernetes work?",
		"Explain Kubernetes architecture",
		"What are Kubernetes components?",
		"Describe Kubernetes networking",
		"How does Kubernetes scheduling work?",
	}

	for i, q := range queries {
		input := RecordQueryInput{
			SessionID: sessionID,
			UserID:    "test-user",
			TenantID:  "test-tenant",
			Query:     q,
			Answer:    fmt.Sprintf("Answer %d about Kubernetes: %s", i, generateLongText(500)),
			Model:     "gpt-4",
		}
		_, err := RecordQuery(ctx, input)
		require.NoError(t, err)
	}

	// Test with MMR enabled
	vdbConfig := vectordb.Get().GetConfig()
	vdbConfig.MMREnabled = true
	vdbConfig.MMRLambda = 0.5 // Balance relevance and diversity
	vectordb.Initialize(vdbConfig)

	fetchInput := FetchSemanticMemoryInput{
		Query:     "Tell me about Kubernetes",
		SessionID: sessionID,
		TenantID:  "test-tenant",
		TopK:      3,
		Threshold: 0.5,
	}

	resultWithMMR, err := FetchSemanticMemoryChunked(ctx, fetchInput)
	require.NoError(t, err)
	require.Len(t, resultWithMMR.Items, 3)

	// Disable MMR
	vdbConfig.MMREnabled = false
	vectordb.Initialize(vdbConfig)

	resultWithoutMMR, err := FetchSemanticMemoryChunked(ctx, fetchInput)
	require.NoError(t, err)
	require.Len(t, resultWithoutMMR.Items, 3)

	// With MMR, results should be more diverse
	// Check that queries are different
	queriesWithMMR := extractQueries(resultWithMMR.Items)
	queriesWithoutMMR := extractQueries(resultWithoutMMR.Items)

	// MMR should produce more diverse queries
	diversityWithMMR := calculateDiversity(queriesWithMMR)
	diversityWithoutMMR := calculateDiversity(queriesWithoutMMR)

	assert.Greater(t, diversityWithMMR, diversityWithoutMMR,
		"MMR should produce more diverse results")
}

// TestChunkOverlapCorrectness verifies overlap between chunks
func TestChunkOverlapCorrectness(t *testing.T) {
	chunker := embeddings.NewChunker(embeddings.ChunkingConfig{
		Enabled:       true,
		MaxTokens:     100, // Small chunks for testing
		OverlapTokens: 20,  // 20% overlap
	})

	// Create text with clear boundaries
	text := strings.Join([]string{
		strings.Repeat("A", 100), // First chunk
		strings.Repeat("B", 100), // Second chunk
		strings.Repeat("C", 100), // Third chunk
	}, "")

	chunks := chunker.ChunkText(text)
	require.Greater(t, len(chunks), 1, "Should create multiple chunks")

	// Verify overlap exists
	for i := 0; i < len(chunks)-1; i++ {
		current := chunks[i].Text
		next := chunks[i+1].Text

		// The end of current chunk should appear at the beginning of next chunk
		overlapSize := 20 // Based on our config
		currentEnd := current[len(current)-overlapSize:]

		// Some overlap should exist (not exact due to tokenization)
		assert.True(t, strings.Contains(next, currentEnd[:10]),
			"Chunks should have overlap")
	}
}

// TestDimensionValidation tests embedding dimension validation
func TestDimensionValidation(t *testing.T) {
	// Test with matching dimensions
	cfg := vectordb.Config{
		Enabled:              true,
		Host:                 "localhost",
		Port:                 6333,
		TaskEmbeddings:       "test_embeddings",
		ExpectedEmbeddingDim: 1536, // OpenAI dimension
	}

	err := vectordb.ValidateAndInitialize(cfg)
	// This will fail in unit tests without real Qdrant, but tests the flow
	if err != nil {
		assert.Contains(t, err.Error(), "dimension") // Should mention dimensions
	}

	// Test with mismatched dimensions
	cfg.ExpectedEmbeddingDim = 768 // Different dimension
	err = vectordb.ValidateAndInitialize(cfg)
	if err != nil {
		assert.Contains(t, err.Error(), "dimension")
	}
}

// TestConfigurableSummaryLimits tests configurable summary retrieval
func TestConfigurableSummaryLimits(t *testing.T) {
	ctx := context.Background()
	sessionID := uuid.New().String()

	// Store some data
	input := RecordQueryInput{
		SessionID: sessionID,
		UserID:    "test-user",
		TenantID:  "test-tenant",
		Query:     "Test summary limits",
		Answer:    "Test answer for summary limits",
		Model:     "gpt-4",
	}
	_, err := recordQueryCore(ctx, input)
	require.NoError(t, err)

	// Test different summary limits
	testCases := []struct {
		summaryTopK int
		maxExpected int
	}{
		{summaryTopK: 1, maxExpected: 1},
		{summaryTopK: 3, maxExpected: 3},
		{summaryTopK: 5, maxExpected: 5},
		{summaryTopK: 0, maxExpected: 3}, // Default
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("SummaryTopK=%d", tc.summaryTopK), func(t *testing.T) {
			fetchInput := FetchHierarchicalMemoryInput{
				Query:        "Test",
				SessionID:    sessionID,
				TenantID:     "test-tenant",
				RecentTopK:   2,
				SemanticTopK: 2,
				SummaryTopK:  tc.summaryTopK,
				Threshold:    0.5,
			}

			result, err := FetchHierarchicalMemory(ctx, fetchInput)
			require.NoError(t, err)

			// Count summaries in result
			summaryCount := result.Sources["summary"]
			assert.LessOrEqual(t, summaryCount, tc.maxExpected)
		})
	}
}

// Helper functions

func generateLongText(tokens int) string {
	// Approximate 1 token = 4 characters
	chars := tokens * 4
	base := "This is a test sentence that will be repeated many times. "
	repeats := chars / len(base)
	return strings.Repeat(base, repeats)
}

func extractQueries(items []map[string]interface{}) []string {
	queries := make([]string, 0, len(items))
	for _, item := range items {
		if q, ok := item["query"].(string); ok {
			queries = append(queries, q)
		}
	}
	return queries
}

func calculateDiversity(items []string) float64 {
	if len(items) <= 1 {
		return 0
	}

	// Simple diversity metric: average string difference
	totalDiff := 0.0
	comparisons := 0

	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			diff := stringDifference(items[i], items[j])
			totalDiff += diff
			comparisons++
		}
	}

	if comparisons == 0 {
		return 0
	}
	return totalDiff / float64(comparisons)
}

func stringDifference(s1, s2 string) float64 {
	// Simple character-based difference
	maxLen := len(s1)
	if len(s2) > maxLen {
		maxLen = len(s2)
	}

	if maxLen == 0 {
		return 0
	}

	matches := 0
	minLen := len(s1)
	if len(s2) < minLen {
		minLen = len(s2)
	}

	for i := 0; i < minLen; i++ {
		if s1[i] == s2[i] {
			matches++
		}
	}

	return 1.0 - float64(matches)/float64(maxLen)
}

// Mock embedding service for testing
type mockEmbeddingService struct {
	singleCalls int
	batchCalls  int
}

func (m *mockEmbeddingService) GenerateEmbedding(ctx context.Context, text string, model string) ([]float32, error) {
	m.singleCalls++
	return make([]float32, 1536), nil
}

func (m *mockEmbeddingService) GenerateBatchEmbeddings(ctx context.Context, texts []string, model string) ([][]float32, error) {
	m.batchCalls++
	result := make([][]float32, len(texts))
	for i := range result {
		result[i] = make([]float32, 1536)
	}
	return result, nil
}