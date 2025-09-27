package activities

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/embeddings"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/vectordb"
	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"
)

// SupervisorMemoryContext enriches raw memory with strategic insights
type SupervisorMemoryContext struct {
	// Raw conversation history (what we have now)
	ConversationHistory []map[string]interface{} `json:"conversation_history"`

	// Strategic memory (what we need)
	DecompositionHistory []DecompositionMemory    `json:"decomposition_history"`
	StrategyPerformance  map[string]StrategyStats `json:"strategy_performance"`
	TeamCompositions     []TeamMemory             `json:"team_compositions"`
	FailurePatterns      []FailurePattern         `json:"failure_patterns"`
	UserPreferences      UserProfile              `json:"user_preferences"`
}

type DecompositionMemory struct {
	QueryPattern string    `json:"query_pattern"` // "optimize API endpoint"
	Subtasks     []string  `json:"subtasks"`
	Strategy     string    `json:"strategy"`    // "parallel", "sequential"
	SuccessRate  float64   `json:"success_rate"`
	AvgDuration  int64     `json:"avg_duration_ms"`
	LastUsed     time.Time `json:"last_used"`
}

type StrategyStats struct {
	TotalRuns    int     `json:"total_runs"`
	SuccessRate  float64 `json:"success_rate"`
	AvgDuration  int64   `json:"avg_duration_ms"`
	AvgTokenCost int     `json:"avg_token_cost"`
}

type TeamMemory struct {
	TaskType         string   `json:"task_type"`
	AgentRoles       []string `json:"agent_roles"`
	Coordination     string   `json:"coordination"`
	PerformanceScore float64  `json:"performance_score"`
}

type FailurePattern struct {
	Pattern     string   `json:"pattern"`
	Indicators  []string `json:"indicators"`
	Mitigation  string   `json:"mitigation"`
	Occurrences int      `json:"occurrences"`
}

type UserProfile struct {
	ExpertiseLevel  string   `json:"expertise_level"`   // "beginner", "intermediate", "expert"
	PreferredStyle  string   `json:"preferred_style"`   // "detailed", "concise", "educational"
	DomainFocus     []string `json:"domain_focus"`      // ["ml", "web", "data"]
	SpeedVsAccuracy float64  `json:"speed_vs_accuracy"` // 0.0 (speed) to 1.0 (accuracy)
}

type DecompositionSuggestion struct {
	UsesPreviousSuccess bool
	SuggestedSubtasks   []string
	Strategy            string
	Confidence          float64
	Warnings            []string
	AvoidStrategies     []string
	PreferSequential    bool
	AddExplanations     bool
}

// FetchSupervisorMemoryInput for the enhanced activity
type FetchSupervisorMemoryInput struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	TenantID  string `json:"tenant_id"`
	Query     string `json:"query"`
}

// FetchSupervisorMemory fetches and enriches memory for strategic decisions
func FetchSupervisorMemory(ctx context.Context, input FetchSupervisorMemoryInput) (*SupervisorMemoryContext, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Fetching enhanced supervisor memory",
		"session_id", input.SessionID,
		"query", input.Query)

	memory := &SupervisorMemoryContext{
		StrategyPerformance: make(map[string]StrategyStats),
	}

	// 1. Get conversation history (existing implementation)
	hierarchicalInput := FetchHierarchicalMemoryInput{
		Query:        input.Query,
		SessionID:    input.SessionID,
		TenantID:     input.TenantID,
		RecentTopK:   5,
		SemanticTopK: 3,
		SummaryTopK:  2,
		Threshold:    0.7,
	}

	hierarchicalResult, err := FetchHierarchicalMemory(ctx, hierarchicalInput)
	if err == nil && len(hierarchicalResult.Items) > 0 {
		memory.ConversationHistory = hierarchicalResult.Items
	}

	// 2. Fetch decomposition patterns for similar queries
	if err := fetchDecompositionPatterns(ctx, memory, input.Query, input.SessionID); err != nil {
		logger.Warn("Failed to fetch decomposition patterns", "error", err)
	}

	// 3. Aggregate strategy performance for this session/user
	if err := fetchStrategyPerformance(ctx, memory, input.SessionID, input.UserID); err != nil {
		logger.Warn("Failed to fetch strategy performance", "error", err)
	}

	// 4. Identify relevant failure patterns
	if err := identifyFailurePatterns(ctx, memory, input.Query); err != nil {
		logger.Warn("Failed to identify failure patterns", "error", err)
	}

	// 5. Load user preferences from session metadata
	if err := loadUserPreferences(ctx, memory, input.SessionID, input.UserID); err != nil {
		logger.Warn("Failed to load user preferences", "error", err)
		// Use defaults
		memory.UserPreferences = UserProfile{
			ExpertiseLevel:  "intermediate",
			PreferredStyle:  "concise",
			SpeedVsAccuracy: 0.7,
		}
	}

	// Record metrics
	metrics.MemoryFetches.WithLabelValues("supervisor", "enhanced", "hit").Inc()
	metrics.MemoryItemsRetrieved.WithLabelValues("supervisor", "enhanced").Observe(float64(len(memory.DecompositionHistory)))

	return memory, nil
}

func fetchDecompositionPatterns(ctx context.Context, memory *SupervisorMemoryContext, query, sessionID string) error {
	// Generate embedding for the query
	svc := embeddings.Get()
	vdb := vectordb.Get()
	if svc == nil || vdb == nil {
		return fmt.Errorf("vector services unavailable")
	}

	queryEmbedding, err := svc.GenerateEmbedding(ctx, query, "")
	if err != nil {
		return err
	}

	// Search for similar decompositions using GetSessionContextSemanticByEmbedding
	// Since we don't have a generic Search method, we'll reuse existing search
	// This searches in task_embeddings collection which stores Q&A pairs
	results, err := vdb.GetSessionContextSemanticByEmbedding(ctx, queryEmbedding, sessionID, "", 5, 0.7)
	if err != nil {
		// Collection might not exist yet
		return nil
	}

	for _, result := range results {
		if pattern, ok := result.Payload["pattern"].(string); ok {
			dm := DecompositionMemory{
				QueryPattern: pattern,
			}

			// Extract subtasks
			if subtasks, ok := result.Payload["subtasks"].([]interface{}); ok {
				for _, st := range subtasks {
					if s, ok := st.(string); ok {
						dm.Subtasks = append(dm.Subtasks, s)
					}
				}
			}

			// Extract strategy
			if strategy, ok := result.Payload["strategy"].(string); ok {
				dm.Strategy = strategy
			}

			// Extract performance metrics
			if sr, ok := result.Payload["success_rate"].(float64); ok {
				dm.SuccessRate = sr
			}
			if dur, ok := result.Payload["avg_duration_ms"].(float64); ok {
				dm.AvgDuration = int64(dur)
			}

			memory.DecompositionHistory = append(memory.DecompositionHistory, dm)
		}
	}

	return nil
}

func fetchStrategyPerformance(ctx context.Context, memory *SupervisorMemoryContext, sessionID, userID string) error {
	dbClient := GetGlobalDBClient()
	if dbClient == nil {
		return fmt.Errorf("database client unavailable")
	}

	db := dbClient.GetDB()
	if db == nil {
		return fmt.Errorf("database connection unavailable")
	}

	// Query agent_executions table for strategy performance
	query := `
		SELECT
			strategy,
			COUNT(*) as total_runs,
			AVG(CASE WHEN status = 'COMPLETED' THEN 1.0 ELSE 0.0 END) as success_rate,
			AVG(duration_ms) as avg_duration_ms,
			AVG(tokens_used) as avg_token_cost
		FROM agent_executions
		WHERE session_id = $1 OR user_id = $2
		GROUP BY strategy
	`

	rows, err := db.QueryContext(ctx, query, sessionID, userID)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var strategy string
		var stats StrategyStats

		err := rows.Scan(&strategy, &stats.TotalRuns, &stats.SuccessRate,
			&stats.AvgDuration, &stats.AvgTokenCost)
		if err != nil {
			continue
		}

		memory.StrategyPerformance[strategy] = stats
	}

	return nil
}

func identifyFailurePatterns(ctx context.Context, memory *SupervisorMemoryContext, query string) error {
	// Check for known failure indicators in the query
	queryLower := strings.ToLower(query)

	// Common failure patterns
	patterns := []FailurePattern{
		{
			Pattern:     "rate_limit",
			Indicators:  []string{"quickly", "fast", "urgent", "asap", "immediately"},
			Mitigation:  "Consider sequential execution to avoid rate limits",
			Occurrences: 0,
		},
		{
			Pattern:     "context_overflow",
			Indicators:  []string{"analyze", "review", "entire codebase", "all files", "everything"},
			Mitigation:  "Break down into smaller, focused subtasks",
			Occurrences: 0,
		},
		{
			Pattern:     "ambiguous_request",
			Indicators:  []string{"something", "somehow", "maybe", "probably", "i think"},
			Mitigation:  "Clarify requirements before decomposition",
			Occurrences: 0,
		},
	}

	for _, pattern := range patterns {
		for _, indicator := range pattern.Indicators {
			if strings.Contains(queryLower, indicator) {
				memory.FailurePatterns = append(memory.FailurePatterns, pattern)
				break
			}
		}
	}

	return nil
}

func loadUserPreferences(ctx context.Context, memory *SupervisorMemoryContext, sessionID, userID string) error {
	// Analyze past interactions to infer preferences
	dbClient := GetGlobalDBClient()
	if dbClient == nil {
		return fmt.Errorf("database client unavailable")
	}

	db := dbClient.GetDB()
	if db == nil {
		return fmt.Errorf("database connection unavailable")
	}

	// Get average response length preference
	var avgResponseLength float64
	err := db.QueryRowContext(ctx, `
		SELECT AVG(LENGTH(response))
		FROM tasks
		WHERE session_id = $1 OR user_id = $2
		LIMIT 100
	`, sessionID, userID).Scan(&avgResponseLength)

	if err == nil {
		if avgResponseLength < 500 {
			memory.UserPreferences.PreferredStyle = "concise"
		} else if avgResponseLength > 2000 {
			memory.UserPreferences.PreferredStyle = "detailed"
		} else {
			memory.UserPreferences.PreferredStyle = "balanced"
		}
	}

	// Infer expertise level from query complexity
	var avgComplexity float64
	err = db.QueryRowContext(ctx, `
		SELECT AVG(estimated_complexity)
		FROM tasks
		WHERE session_id = $1 OR user_id = $2
		LIMIT 100
	`, sessionID, userID).Scan(&avgComplexity)

	if err == nil {
		if avgComplexity < 3 {
			memory.UserPreferences.ExpertiseLevel = "beginner"
		} else if avgComplexity > 7 {
			memory.UserPreferences.ExpertiseLevel = "expert"
		} else {
			memory.UserPreferences.ExpertiseLevel = "intermediate"
		}
	}

	// Speed vs accuracy preference (based on retry patterns)
	memory.UserPreferences.SpeedVsAccuracy = 0.7 // Default balanced

	return nil
}

// DecompositionAdvisor suggests decomposition based on memory
type DecompositionAdvisor struct {
	Memory *SupervisorMemoryContext
}

// NewDecompositionAdvisor creates a new advisor with memory context
func NewDecompositionAdvisor(memory *SupervisorMemoryContext) *DecompositionAdvisor {
	return &DecompositionAdvisor{Memory: memory}
}

// SuggestDecomposition provides intelligent decomposition suggestions
func (da *DecompositionAdvisor) SuggestDecomposition(query string) DecompositionSuggestion {
	suggestion := DecompositionSuggestion{
		Strategy:   "parallel", // Default
		Confidence: 0.5,
	}

	// 1. Check decomposition history for similar successful patterns
	for _, prev := range da.Memory.DecompositionHistory {
		similarity := calculateSimilarity(query, prev.QueryPattern)
		if similarity > 0.8 && prev.SuccessRate > 0.7 {
			suggestion.UsesPreviousSuccess = true
			suggestion.SuggestedSubtasks = prev.Subtasks
			suggestion.Strategy = prev.Strategy
			suggestion.Confidence = prev.SuccessRate * similarity
			break
		}
	}

	// 2. Select optimal strategy based on performance history
	if !suggestion.UsesPreviousSuccess {
		suggestion.Strategy = da.selectOptimalStrategy()
	}

	// 3. Check for failure patterns and add warnings
	for _, pattern := range da.Memory.FailurePatterns {
		if matchesPattern(query, pattern) {
			suggestion.Warnings = append(suggestion.Warnings, pattern.Mitigation)
			if pattern.Pattern == "rate_limit" {
				suggestion.PreferSequential = true
				suggestion.Strategy = "sequential"
			}
		}
	}

	// 4. Adjust for user preferences
	if da.Memory.UserPreferences.ExpertiseLevel == "beginner" {
		suggestion.PreferSequential = true
		suggestion.AddExplanations = true
	} else if da.Memory.UserPreferences.ExpertiseLevel == "expert" {
		// Expert users can handle parallel complexity
		if suggestion.Strategy == "" {
			suggestion.Strategy = "parallel"
		}
	}

	// 5. Consider speed vs accuracy preference
	if da.Memory.UserPreferences.SpeedVsAccuracy < 0.3 {
		// Prioritize speed
		suggestion.Strategy = "parallel"
		suggestion.PreferSequential = false
	} else if da.Memory.UserPreferences.SpeedVsAccuracy > 0.8 {
		// Prioritize accuracy
		suggestion.Strategy = "sequential"
		suggestion.PreferSequential = true
	}

	return suggestion
}

func (da *DecompositionAdvisor) selectOptimalStrategy() string {
	// Use epsilon-greedy selection based on performance history
	epsilon := 0.1 // 10% exploration

	if rand.Float64() < epsilon {
		// Explore: try less-used strategies
		return da.selectLeastUsedStrategy()
	}

	// Exploit: use best performing strategy
	var bestStrategy string
	var bestScore float64

	for strategy, stats := range da.Memory.StrategyPerformance {
		// Balance success rate with speed based on user preference
		maxDuration := float64(30000) // 30 seconds as baseline
		speedScore := 1.0 - float64(stats.AvgDuration)/maxDuration
		if speedScore < 0 {
			speedScore = 0
		}

		score := stats.SuccessRate*da.Memory.UserPreferences.SpeedVsAccuracy +
			speedScore*(1.0-da.Memory.UserPreferences.SpeedVsAccuracy)

		if score > bestScore {
			bestScore = score
			bestStrategy = strategy
		}
	}

	if bestStrategy == "" {
		bestStrategy = "parallel" // Default fallback
	}

	return bestStrategy
}

func (da *DecompositionAdvisor) selectLeastUsedStrategy() string {
	strategies := []string{"parallel", "sequential", "hierarchical", "iterative"}

	minRuns := int(^uint(0) >> 1) // Max int
	leastUsed := "parallel"

	for _, strategy := range strategies {
		if stats, exists := da.Memory.StrategyPerformance[strategy]; exists {
			if stats.TotalRuns < minRuns {
				minRuns = stats.TotalRuns
				leastUsed = strategy
			}
		} else {
			// Never used - highest priority for exploration
			return strategy
		}
	}

	return leastUsed
}

// Helper functions
func calculateSimilarity(a, b string) float64 {
	// In production, this would use embedding similarity
	// For now, use simple string comparison
	aLower := strings.ToLower(a)
	bLower := strings.ToLower(b)

	if aLower == bLower {
		return 1.0
	}

	// Count common words
	aWords := strings.Fields(aLower)
	bWords := strings.Fields(bLower)

	common := 0
	for _, aw := range aWords {
		for _, bw := range bWords {
			if aw == bw {
				common++
				break
			}
		}
	}

	if len(aWords) == 0 || len(bWords) == 0 {
		return 0
	}

	return float64(common) / float64(max(len(aWords), len(bWords)))
}

func matchesPattern(query string, pattern FailurePattern) bool {
	queryLower := strings.ToLower(query)
	for _, indicator := range pattern.Indicators {
		if strings.Contains(queryLower, indicator) {
			return true
		}
	}
	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RecordDecompositionResult stores the decomposition outcome for future learning
type RecordDecompositionInput struct {
	SessionID    string   `json:"session_id"`
	Query        string   `json:"query"`
	Subtasks     []string `json:"subtasks"`
	Strategy     string   `json:"strategy"`
	Success      bool     `json:"success"`
	DurationMs   int64    `json:"duration_ms"`
	TokensUsed   int      `json:"tokens_used"`
	ErrorMessage string   `json:"error_message,omitempty"`
}

// RecordDecomposition stores decomposition results for future reference
func RecordDecomposition(ctx context.Context, input RecordDecompositionInput) error {
	logger := activity.GetLogger(ctx)

	// Generate embedding for the query pattern
	svc := embeddings.Get()
	vdb := vectordb.Get()
	if svc == nil || vdb == nil {
		logger.Warn("Vector services unavailable, skipping decomposition recording")
		return nil
	}

	embedding, err := svc.GenerateEmbedding(ctx, input.Query, "")
	if err != nil {
		return err
	}

	// Prepare payload
	payload := map[string]interface{}{
		"pattern":       input.Query,
		"subtasks":      input.Subtasks,
		"strategy":      input.Strategy,
		"success":       input.Success,
		"duration_ms":   input.DurationMs,
		"tokens_used":   input.TokensUsed,
		"session_id":    input.SessionID,
		"timestamp":     time.Now().Unix(),
	}

	if input.ErrorMessage != "" {
		payload["error_message"] = input.ErrorMessage
	}

	// Store in decomposition_patterns collection
	collection := "decomposition_patterns"
	point := vectordb.UpsertItem{
		ID:      uuid.New().String(),
		Vector:  embedding,
		Payload: payload,
	}

	if _, err := vdb.Upsert(ctx, collection, []vectordb.UpsertItem{point}); err != nil {
		logger.Error("Failed to store decomposition pattern", "error", err)
		// Non-critical error, don't fail the activity
		return nil
	}

	logger.Info("Recorded decomposition pattern",
		"strategy", input.Strategy,
		"success", input.Success,
		"subtasks", len(input.Subtasks))

	return nil
}