// +build integration

package activities

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/embeddings"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/vectordb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

// MemoryIntegrationSuite tests the memory system with real services
type MemoryIntegrationSuite struct {
	suite.Suite
	ctx       context.Context
	sessionID string
	userID    string
	tenantID  string
}

func (s *MemoryIntegrationSuite) SetupSuite() {
	s.ctx = context.Background()
	s.userID = "test-user-" + uuid.New().String()[:8]
	s.tenantID = "test-tenant"

	// Initialize services (assumes services are running)
	s.initializeServices()
}

func (s *MemoryIntegrationSuite) SetupTest() {
	// Fresh session for each test
	s.sessionID = uuid.New().String()
}

func (s *MemoryIntegrationSuite) initializeServices() {
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

	// Initialize vector DB
	vdbConfig := vectordb.Config{
		Enabled:              true,
		Host:                 getEnvOrDefault("QDRANT_HOST", "localhost"),
		Port:                 6333,
		TaskEmbeddings:       "task_embeddings",
		Summaries:            "summaries",
		TopK:                 5,
		Threshold:            0.7,
		Timeout:              3 * time.Second,
		ExpectedEmbeddingDim: 1536,
		MMREnabled:           false, // Will toggle in specific tests
		MMRLambda:            0.7,
		MMRPoolMultiplier:    3,
	}
	vectordb.Initialize(vdbConfig)

	// Session manager would be initialized here if needed
	// For integration tests, we assume Redis is configured via environment
}

// Test 1: Chunking Pipeline Integration
func (s *MemoryIntegrationSuite) TestChunkingPipelineIntegration() {
	// Create a long answer that should trigger chunking
	longAnswer := s.generateLongText(5000) // ~5000 tokens

	input := RecordQueryInput{
		SessionID: s.sessionID,
		UserID:    s.userID,
		TenantID:  s.tenantID,
		Query:     "Explain distributed systems in detail",
		Answer:    longAnswer,
		Model:     "gpt-4",
		Metadata: map[string]interface{}{
			"test_type": "chunking_integration",
		},
	}

	// Record the Q&A pair
	result, err := RecordQuery(s.ctx, input)
	s.Require().NoError(err)
	s.T().Logf("RecordQuery result: Stored=%v, ChunksCreated=%d, Error=%s",
		result.Stored, result.ChunksCreated, result.Error)
	s.T().Logf("Long answer length: %d chars, approx %d words",
		len(longAnswer), len(strings.Fields(longAnswer)))
	s.T().Logf("Using SessionID: %s, TenantID: %s", s.sessionID, s.tenantID)
	s.Assert().True(result.Stored)
	s.Assert().Greater(result.ChunksCreated, 0, "Should create chunks for long text")
	s.Assert().Equal(3, result.ChunksCreated, "Should create ~3 chunks for 5000 tokens")

	// Wait a moment for indexing
	time.Sleep(500 * time.Millisecond)

	// Verify chunks can be retrieved
	fetchInput := FetchSemanticMemoryInput{
		Query:     "distributed systems",
		SessionID: s.sessionID,
		TenantID:  s.tenantID,
		TopK:      5,
		Threshold: 0.0, // Lower threshold to ensure we get results
	}

	fetchResult, err := FetchSemanticMemoryChunked(s.ctx, fetchInput)
	s.Require().NoError(err)
	s.T().Logf("FetchResult: %d items retrieved for session %s", len(fetchResult.Items), s.sessionID)
	s.Assert().NotEmpty(fetchResult.Items)

	// Verify reconstruction accuracy
	reconstructed := ""
	for _, item := range fetchResult.Items {
		if answer, ok := item["answer"].(string); ok {
			reconstructed = answer
			break
		}
	}

	s.Assert().NotEmpty(reconstructed, "Should reconstruct answer from chunks")
	// Note: With overlap handling, exact reconstruction is challenging
	// 50% similarity indicates the chunks are being assembled in the right order
	similarity := s.calculateSimilarity(longAnswer, reconstructed)
	s.T().Logf("Reconstruction similarity: %.2f%%", similarity*100)
	s.Assert().Greater(similarity, 0.5, "Reconstruction should be >50% accurate")
}

// Test 2: Batch Embedding Performance
func (s *MemoryIntegrationSuite) TestBatchEmbeddingIntegration() {
	// Create multiple chunks worth of data
	veryLongAnswer := s.generateLongText(10000) // Should create ~5 chunks

	startTime := time.Now()

	input := RecordQueryInput{
		SessionID: s.sessionID,
		UserID:    s.userID,
		TenantID:  s.tenantID,
		Query:     "Test batch embeddings",
		Answer:    veryLongAnswer,
		Model:     "gpt-4",
	}

	result, err := RecordQuery(s.ctx, input)
	duration := time.Since(startTime)

	s.Require().NoError(err)
	s.Assert().True(result.Stored)
	s.Assert().GreaterOrEqual(result.ChunksCreated, 4, "Should create 4+ chunks")

	// Batch embedding should be faster than sequential
	// With 5 chunks, batch should take <2s, sequential would take >5s
	s.Assert().Less(duration, 3*time.Second,
		"Batch embedding should complete quickly")
}

// Test 3: MMR Diversity Integration
func (s *MemoryIntegrationSuite) TestMMRDiversityIntegration() {
	// Store multiple similar but different Q&A pairs
	topics := []struct {
		query  string
		answer string
	}{
		{"How does Kubernetes work?", "Kubernetes orchestrates containers using pods and services"},
		{"Explain Kubernetes architecture", "Kubernetes has control plane and worker nodes"},
		{"What are Kubernetes components?", "Key components include etcd, API server, scheduler"},
		{"Describe Kubernetes networking", "Kubernetes uses CNI plugins for pod networking"},
		{"How does Kubernetes scheduling work?", "The scheduler assigns pods to nodes based on resources"},
	}

	for _, topic := range topics {
		input := RecordQueryInput{
			SessionID: s.sessionID,
			UserID:    s.userID,
			TenantID:  s.tenantID,
			Query:     topic.query,
			Answer:    topic.answer,
			Model:     "gpt-4",
		}
		_, err := RecordQuery(s.ctx, input)
		s.Require().NoError(err)
		time.Sleep(100 * time.Millisecond) // Small delay between writes
	}

	// Test without MMR first
	fetchInput := FetchSemanticMemoryInput{
		Query:     "Tell me about Kubernetes",
		SessionID: s.sessionID,
		TenantID:  s.tenantID,
		TopK:      3,
		Threshold: 0.5,
	}

	resultWithoutMMR, err := FetchSemanticMemoryChunked(s.ctx, fetchInput)
	s.Require().NoError(err)
	s.Assert().Len(resultWithoutMMR.Items, 3)

	// Enable MMR
	vdb := vectordb.Get()
	vdbConfig := vdb.GetConfig()
	vdbConfig.MMREnabled = true
	vdbConfig.MMRLambda = 0.5 // Balance relevance and diversity
	vectordb.Initialize(vdbConfig)

	// Test with MMR
	resultWithMMR, err := FetchSemanticMemoryChunked(s.ctx, fetchInput)
	s.Require().NoError(err)
	s.Assert().Len(resultWithMMR.Items, 3)

	// Calculate diversity scores
	diversityWithout := s.calculateDiversity(resultWithoutMMR.Items)
	diversityWith := s.calculateDiversity(resultWithMMR.Items)

	s.Assert().Greater(diversityWith, diversityWithout,
		"MMR should produce more diverse results")

	// Disable MMR for other tests
	vdbConfig.MMREnabled = false
	vectordb.Initialize(vdbConfig)
}

// Test 4: Idempotency Integration
func (s *MemoryIntegrationSuite) TestIdempotencyIntegration() {
	longAnswer := s.generateLongText(3000)

	input := RecordQueryInput{
		SessionID: s.sessionID,
		UserID:    s.userID,
		TenantID:  s.tenantID,
		Query:     "Test idempotency with chunks",
		Answer:    longAnswer,
		Model:     "gpt-4",
	}

	// First write
	result1, err := RecordQuery(s.ctx, input)
	s.Require().NoError(err)
	s.Assert().True(result1.Stored)
	chunks1 := result1.ChunksCreated

	// Second write (should be idempotent)
	result2, err := RecordQuery(s.ctx, input)
	s.Require().NoError(err)
	s.Assert().True(result2.Stored)
	chunks2 := result2.ChunksCreated

	// Should create same number of chunks
	s.Assert().Equal(chunks1, chunks2)

	// Retrieve and verify no duplicates
	fetchInput := FetchSemanticMemoryInput{
		Query:     "Test idempotency",
		SessionID: s.sessionID,
		TenantID:  s.tenantID,
		TopK:      10, // Get extra to check for duplicates
	}

	fetchResult, err := FetchSemanticMemoryChunked(s.ctx, fetchInput)
	s.Require().NoError(err)

	// Should only have one Q&A pair despite writing twice
	uniqueQueries := make(map[string]bool)
	for _, item := range fetchResult.Items {
		if query, ok := item["query"].(string); ok {
			uniqueQueries[query] = true
		}
	}
	s.Assert().Equal(1, len(uniqueQueries), "Should not have duplicate Q&A pairs")
}

// Test 5: Chunk Reconstruction Accuracy
func (s *MemoryIntegrationSuite) TestChunkReconstructionAccuracy() {
	// Create text with specific markers
	markedText := `
START_MARKER
This is section one with important information that must be preserved.
It contains critical details about the system architecture.
MIDDLE_MARKER
This is section two with additional context and explanations.
The chunking system should handle this overlap correctly.
END_MARKER
Final section with conclusions and summary points.
All markers should be preserved in the reconstruction.
`
	// Repeat to make it long enough to chunk
	longMarkedText := strings.Repeat(markedText, 10)

	input := RecordQueryInput{
		SessionID: s.sessionID,
		UserID:    s.userID,
		TenantID:  s.tenantID,
		Query:     "Test reconstruction with markers",
		Answer:    longMarkedText,
		Model:     "gpt-4",
	}

	result, err := RecordQuery(s.ctx, input)
	s.Require().NoError(err)
	s.Assert().True(result.Stored)
	s.Assert().Greater(result.ChunksCreated, 0)

	// Retrieve and reconstruct
	fetchInput := FetchSemanticMemoryInput{
		Query:     "Test reconstruction with markers",
		SessionID: s.sessionID,
		TenantID:  s.tenantID,
		TopK:      10,
		Threshold: 0.3,
	}

	fetchResult, err := FetchSemanticMemoryChunked(s.ctx, fetchInput)
	s.Require().NoError(err)
	s.Assert().NotEmpty(fetchResult.Items)

	// Check reconstruction
	reconstructed := ""
	for _, item := range fetchResult.Items {
		if answer, ok := item["answer"].(string); ok {
			reconstructed = answer
			break
		}
	}

	// Verify all markers are present
	s.Assert().Contains(reconstructed, "START_MARKER")
	s.Assert().Contains(reconstructed, "MIDDLE_MARKER")
	s.Assert().Contains(reconstructed, "END_MARKER")

	// Verify order is preserved
	startIdx := strings.Index(reconstructed, "START_MARKER")
	middleIdx := strings.Index(reconstructed, "MIDDLE_MARKER")
	endIdx := strings.Index(reconstructed, "END_MARKER")

	s.Assert().Less(startIdx, middleIdx, "START should come before MIDDLE")
	s.Assert().Less(middleIdx, endIdx, "MIDDLE should come before END")
}

// Test 6: Cross-Session Memory Isolation
func (s *MemoryIntegrationSuite) TestCrossSessionIsolation() {
	session1 := uuid.New().String()
	session2 := uuid.New().String()

	// Store in session 1
	input1 := RecordQueryInput{
		SessionID: session1,
		UserID:    s.userID,
		TenantID:  s.tenantID,
		Query:     "Secret information for session 1",
		Answer:    "This is confidential data only for session 1",
		Model:     "gpt-4",
	}
	_, err := RecordQuery(s.ctx, input1)
	s.Require().NoError(err)

	// Store in session 2
	input2 := RecordQueryInput{
		SessionID: session2,
		UserID:    s.userID,
		TenantID:  s.tenantID,
		Query:     "Different information for session 2",
		Answer:    "This is data only for session 2",
		Model:     "gpt-4",
	}
	_, err = RecordQuery(s.ctx, input2)
	s.Require().NoError(err)

	// Try to retrieve session 1 data from session 2
	fetchInput := FetchSemanticMemoryInput{
		Query:     "Secret information",
		SessionID: session2, // Using session 2
		TenantID:  s.tenantID,
		TopK:      5,
		Threshold: 0.3,
	}

	fetchResult, err := FetchSemanticMemoryChunked(s.ctx, fetchInput)
	s.Require().NoError(err)

	// Should not find session 1's secret information
	for _, item := range fetchResult.Items {
		if answer, ok := item["answer"].(string); ok {
			s.Assert().NotContains(answer, "session 1",
				"Should not leak data from session 1")
			s.Assert().Contains(answer, "session 2",
				"Should only get session 2 data")
		}
	}
}

// Test 7: Performance Under Load
func (s *MemoryIntegrationSuite) TestPerformanceUnderLoad() {
	concurrency := 10
	errors := make(chan error, concurrency)
	durations := make(chan time.Duration, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			start := time.Now()
			sessionID := fmt.Sprintf("%s-%d", s.sessionID, idx)

			input := RecordQueryInput{
				SessionID: sessionID,
				UserID:    s.userID,
				TenantID:  s.tenantID,
				Query:     fmt.Sprintf("Concurrent query %d", idx),
				Answer:    s.generateLongText(2000),
				Model:     "gpt-4",
			}

			_, err := RecordQuery(context.Background(), input)
			errors <- err
			durations <- time.Since(start)
		}(i)
	}

	// Collect results
	var totalDuration time.Duration
	for i := 0; i < concurrency; i++ {
		err := <-errors
		duration := <-durations
		s.Assert().NoError(err, "Concurrent write should succeed")
		totalDuration += duration
	}

	avgDuration := totalDuration / time.Duration(concurrency)
	s.Assert().Less(avgDuration, 2*time.Second,
		"Average duration should be reasonable under load")
}

// Test 8: Hierarchical Memory Integration
func (s *MemoryIntegrationSuite) TestHierarchicalMemoryIntegration() {
	// Store various Q&A pairs over time
	for i := 0; i < 10; i++ {
		input := RecordQueryInput{
			SessionID: s.sessionID,
			UserID:    s.userID,
			TenantID:  s.tenantID,
			Query:     fmt.Sprintf("Query %d about topic %d", i, i%3),
			Answer:    fmt.Sprintf("Answer %d with details about topic %d", i, i%3),
			Model:     "gpt-4",
		}
		_, err := RecordQuery(s.ctx, input)
		s.Require().NoError(err)
		time.Sleep(50 * time.Millisecond)
	}

	// Test hierarchical retrieval
	fetchInput := FetchHierarchicalMemoryInput{
		Query:        "topic 1",
		SessionID:    s.sessionID,
		TenantID:     s.tenantID,
		RecentTopK:   3,
		SemanticTopK: 3,
		SummaryTopK:  2,
		Threshold:    0.5,
	}

	result, err := FetchHierarchicalMemory(s.ctx, fetchInput)
	s.Require().NoError(err)

	// Verify we got results from different sources
	s.Assert().Greater(result.Sources["recent"], 0)
	s.Assert().Greater(result.Sources["semantic"], 0)
	s.Assert().NotEmpty(result.Items)

	// Verify diversity in results
	topicCounts := make(map[int]int)
	for _, item := range result.Items {
		if answer, ok := item["answer"].(string); ok {
			for topicID := 0; topicID < 3; topicID++ {
				if strings.Contains(answer, fmt.Sprintf("topic %d", topicID)) {
					topicCounts[topicID]++
				}
			}
		}
	}

	// Should have results about topic 1 primarily
	s.Assert().Greater(topicCounts[1], 0, "Should find topic 1 results")
}

// Helper methods

func (s *MemoryIntegrationSuite) generateLongText(tokens int) string {
	// Approximate 1 token = 4 characters
	chars := tokens * 4
	base := "This is a test sentence that will be repeated many times to create long text. "
	repeats := chars / len(base)
	return strings.Repeat(base, repeats)
}

func (s *MemoryIntegrationSuite) calculateSimilarity(text1, text2 string) float64 {
	if len(text1) == 0 || len(text2) == 0 {
		return 0.0
	}

	// Simple character-based similarity
	minLen := len(text1)
	if len(text2) < minLen {
		minLen = len(text2)
	}

	matches := 0
	for i := 0; i < minLen; i++ {
		if text1[i] == text2[i] {
			matches++
		}
	}

	maxLen := len(text1)
	if len(text2) > maxLen {
		maxLen = len(text2)
	}

	return float64(matches) / float64(maxLen)
}

func (s *MemoryIntegrationSuite) calculateDiversity(items []map[string]interface{}) float64 {
	if len(items) <= 1 {
		return 0.0
	}

	queries := make([]string, 0, len(items))
	for _, item := range items {
		if q, ok := item["query"].(string); ok {
			queries = append(queries, q)
		}
	}

	// Calculate average pairwise difference
	totalDiff := 0.0
	comparisons := 0

	for i := 0; i < len(queries); i++ {
		for j := i + 1; j < len(queries); j++ {
			diff := s.stringDifference(queries[i], queries[j])
			totalDiff += diff
			comparisons++
		}
	}

	if comparisons == 0 {
		return 0.0
	}

	return totalDiff / float64(comparisons)
}

func (s *MemoryIntegrationSuite) stringDifference(s1, s2 string) float64 {
	if s1 == s2 {
		return 0.0
	}

	// Simple character-based difference
	maxLen := len(s1)
	if len(s2) > maxLen {
		maxLen = len(s2)
	}

	if maxLen == 0 {
		return 0.0
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

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Run the suite
func TestMemoryIntegrationSuite(t *testing.T) {
	// Skip if not in integration test mode
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration tests. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	suite.Run(t, new(MemoryIntegrationSuite))
}