package activities

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/embeddings"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/vectordb"
	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
)

func getFloatEnv(key string, defaultValue float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultValue
}

// getFloatEnvWithRange gets a float environment variable within a specified range
func getFloatEnvWithRange(key string, defaultValue, min, max float64) float64 {
	val := getFloatEnv(key, defaultValue)
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

// Configuration constants for supervisor memory
var (
	// Similarity threshold for matching decomposition patterns (0.0 to 1.0)
	DecompositionSimilarityThreshold = getFloatEnvWithRange("DECOMPOSITION_SIMILARITY_THRESHOLD", 0.8, 0.0, 1.0)

	// Success rate threshold for considering a pattern successful (0.0 to 1.0)
	PatternSuccessThreshold = getFloatEnvWithRange("PATTERN_SUCCESS_THRESHOLD", 0.7, 0.0, 1.0)

	// Exploration rate for epsilon-greedy strategy selection (0.0 to 1.0)
	StrategyExplorationRate = getFloatEnvWithRange("STRATEGY_EXPLORATION_RATE", 0.1, 0.0, 1.0)

	// Speed vs accuracy thresholds
	SpeedPriorityThreshold    = getFloatEnvWithRange("SPEED_PRIORITY_THRESHOLD", 0.3, 0.0, 1.0)
	AccuracyPriorityThreshold = getFloatEnvWithRange("ACCURACY_PRIORITY_THRESHOLD", 0.8, 0.0, 1.0)

	// Default speed vs accuracy balance
	DefaultSpeedVsAccuracy = getFloatEnvWithRange("DEFAULT_SPEED_VS_ACCURACY", 0.7, 0.0, 1.0)

	// Maximum duration baseline for speed scoring (milliseconds)
	MaxDurationBaseline = getFloatEnv("MAX_DURATION_BASELINE_MS", 30000)

	// TTL for strategy performance cache entries (24 hours)
	StrategyPerformanceTTL = 24 * time.Hour

	// Maximum number of strategy performance entries to keep
	MaxStrategyPerformanceEntries = 100
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
	Strategy     string    `json:"strategy"` // "parallel", "sequential"
	SuccessRate  float64   `json:"success_rate"`
	AvgDuration  int64     `json:"avg_duration_ms"`
	LastUsed     time.Time `json:"last_used"`
}

type StrategyStats struct {
	TotalRuns    int       `json:"total_runs"`
	SuccessRate  float64   `json:"success_rate"`
	AvgDuration  int64     `json:"avg_duration_ms"`
	AvgTokenCost int       `json:"avg_token_cost"`
	LastAccessed time.Time `json:"last_accessed"`
}

type strategyCandidate struct {
	name      string
	success   float64
	duration  int64
	tokenCost int
	totalRuns int
	score     float64
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

// RecommendStrategyInput captures the request for a strategy recommendation.
type RecommendStrategyInput struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	TenantID  string `json:"tenant_id"`
	Query     string `json:"query"`
}

// RecommendStrategyOutput returns the suggested strategy (if any).
type RecommendStrategyOutput struct {
	Strategy   string  `json:"strategy"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source"`
}

// FetchSupervisorMemoryInput for the enhanced activity
type FetchSupervisorMemoryInput struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	TenantID  string `json:"tenant_id"`
	Query     string `json:"query"`
}

// getLoggerSafe returns a logger that works in both activity and regular contexts
func getLoggerSafe(ctx context.Context) (logger log.Logger) {
	// Try to get activity logger, catch panic if not in activity context
	defer func() {
		if r := recover(); r != nil {
			// Panic caught, return fallback logger
			logger = &fallbackLogger{}
		}
	}()

	// This will panic if not in an activity context
	return activity.GetLogger(ctx)
}

// fallbackLogger implements log.Logger for non-activity contexts
type fallbackLogger struct{}

func (f *fallbackLogger) Debug(msg string, keyvals ...interface{}) {}
func (f *fallbackLogger) Info(msg string, keyvals ...interface{})  {}
func (f *fallbackLogger) Warn(msg string, keyvals ...interface{})  {}
func (f *fallbackLogger) Error(msg string, keyvals ...interface{}) {}

// FetchSupervisorMemory fetches and enriches memory for strategic decisions
// FetchSupervisorMemory fetches enhanced supervisor memory with strategic insights.
// Implements TTL-based cleanup for strategy performance cache to prevent memory leaks.
func FetchSupervisorMemory(ctx context.Context, input FetchSupervisorMemoryInput) (*SupervisorMemoryContext, error) {
	logger := getLoggerSafe(ctx)
	logger.Info("Fetching enhanced supervisor memory",
		"session_id", input.SessionID,
		"query", input.Query)

	memory := &SupervisorMemoryContext{
		StrategyPerformance:  make(map[string]StrategyStats),
		DecompositionHistory: []DecompositionMemory{},
		TeamCompositions:     []TeamMemory{},
		FailurePatterns:      []FailurePattern{},
		ConversationHistory:  []map[string]interface{}{},
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

	// Clean up expired entries after fetching to prevent memory leak
	cleanupStrategyPerformance(memory, logger)

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

	// Search for similar decompositions recorded for this session
	results, err := vdb.SearchDecompositionPatterns(ctx, queryEmbedding, sessionID, "", 5, 0.7)
	if err != nil {
		// Collection might not exist yet, log but don't fail
		logger := getLoggerSafe(ctx)
		logger.Info("Decomposition patterns collection not found or search failed",
			"error", err, "session_id", sessionID)
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

			// Extract performance indicators (support both aggregated and raw keys)
			if sr, ok := result.Payload["success_rate"].(float64); ok {
				dm.SuccessRate = sr
			} else if s, ok := result.Payload["success"].(bool); ok {
				if s {
					dm.SuccessRate = 1.0
				} else {
					dm.SuccessRate = 0.0
				}
			}
			if dur, ok := result.Payload["avg_duration_ms"].(float64); ok {
				dm.AvgDuration = int64(dur)
			} else if d2, ok := result.Payload["duration_ms"].(float64); ok {
				dm.AvgDuration = int64(d2)
			}

			memory.DecompositionHistory = append(memory.DecompositionHistory, dm)
		}
	}

	return nil
}

func fetchStrategyPerformance(ctx context.Context, memory *SupervisorMemoryContext, sessionID, userID string) error {
	logger := getLoggerSafe(ctx)

	// Wrap database operation in circuit breaker
	return WithCircuitBreaker(ctx, func(ctx context.Context) error {
		dbClient := GetGlobalDBClient()
		if dbClient == nil {
			return fmt.Errorf("database client unavailable")
		}

		db := dbClient.GetDB()
		if db == nil {
			return fmt.Errorf("database connection unavailable")
		}

		// Query agent_executions joined with task_executions for session/user filtering
		query := `
					SELECT
						COALESCE(ae.strategy, ae.metadata->>'strategy') AS strategy,
						COUNT(*) AS total_runs,
						AVG(CASE WHEN ae.state = 'COMPLETED' THEN 1.0 ELSE 0.0 END) AS success_rate,
						AVG(ae.duration_ms)::bigint AS avg_duration_ms,
						AVG(ae.tokens_used)::int AS avg_token_cost
					FROM agent_executions ae
					LEFT JOIN task_executions te ON te.workflow_id = ae.workflow_id
					WHERE (te.session_id = $1 OR te.user_id::text = $2)
						AND COALESCE(ae.strategy, ae.metadata->>'strategy') IS NOT NULL
					GROUP BY 1
					LIMIT 20
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
				logger.Error("Failed to scan strategy performance row",
					"error", err, "strategy", strategy,
					"session_id", sessionID, "user_id", userID)
				// TODO: Add metrics.DatabaseErrors when available
				continue
			}

			// Set last accessed time for TTL tracking
			stats.LastAccessed = time.Now()
			memory.StrategyPerformance[strategy] = stats
		}

		return nil
	})
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
        SELECT AVG(LENGTH(result))
        FROM task_executions
        WHERE session_id = $1 OR user_id::text = $2
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
        SELECT AVG(complexity_score)
        FROM task_executions
        WHERE session_id = $1 OR user_id::text = $2
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
	memory.UserPreferences.SpeedVsAccuracy = DefaultSpeedVsAccuracy // Default balanced

	// Ensure SpeedVsAccuracy is within valid range [0.0, 1.0]
	if memory.UserPreferences.SpeedVsAccuracy < 0.0 {
		memory.UserPreferences.SpeedVsAccuracy = 0.0
	} else if memory.UserPreferences.SpeedVsAccuracy > 1.0 {
		memory.UserPreferences.SpeedVsAccuracy = 1.0
	}

	// TODO: Calculate and update user preference inference accuracy metric
	// This would compare inferred preferences against actual user behavior
	// metrics.UserPreferenceInferenceAccuracy.Set(calculatedAccuracy)

	return nil
}

// RecommendWorkflowStrategy provides a suggested workflow strategy using supervisor memory.
func RecommendWorkflowStrategy(ctx context.Context, input RecommendStrategyInput) (RecommendStrategyOutput, error) {
	memory, err := FetchSupervisorMemory(ctx, FetchSupervisorMemoryInput{
		SessionID: input.SessionID,
		UserID:    input.UserID,
		TenantID:  input.TenantID,
		Query:     input.Query,
	})
	if err != nil {
		return RecommendStrategyOutput{}, err
	}

	if memory == nil || len(memory.StrategyPerformance) == 0 {
		return RecommendStrategyOutput{Source: "memory_empty"}, nil
	}

	candidates := make([]strategyCandidate, 0, len(memory.StrategyPerformance))
	for name, stats := range memory.StrategyPerformance {
		if stats.TotalRuns == 0 {
			continue
		}
		candidates = append(candidates, strategyCandidate{
			name:      strings.ToLower(strings.TrimSpace(name)),
			success:   stats.SuccessRate,
			duration:  stats.AvgDuration,
			tokenCost: stats.AvgTokenCost,
			totalRuns: stats.TotalRuns,
		})
	}

	if len(candidates) == 0 {
		return RecommendStrategyOutput{Source: "insufficient_data"}, nil
	}

	totalRuns := 0
	for _, cand := range candidates {
		totalRuns += cand.totalRuns
	}

	best := candidates[0]
	best.score = scoreCandidate(best, totalRuns, input.Query)
	for i := 1; i < len(candidates); i++ {
		cand := candidates[i]
		cand.score = scoreCandidate(cand, totalRuns, input.Query)
		if cand.score > best.score {
			best = cand
		}
	}

	if StrategyExplorationRate > 0 {
		if rand.Float64() < StrategyExplorationRate {
			return RecommendStrategyOutput{Source: "explore"}, nil
		}
	}

	return RecommendStrategyOutput{
		Strategy:   best.name,
		Confidence: clamp01(best.score),
		Source:     "memory",
	}, nil
}

func scoreCandidate(c strategyCandidate, totalRuns int, query string) float64 {
	if c.totalRuns == 0 {
		return 1.0 + keywordBoostForStrategy(query, c.name)
	}
	denominator := float64(c.totalRuns)
	if denominator <= 0 {
		denominator = 1
	}
	logComponent := math.Log(float64(totalRuns) + 1)
	if logComponent < 1 {
		logComponent = 1
	}
	exploration := math.Sqrt((2 * logComponent) / denominator)
	durationPenalty := 0.0
	if c.duration > 0 {
		durationPenalty = float64(c.duration) / 60000.0
	}
	tokenPenalty := 0.0
	if c.tokenCost > 0 {
		tokenPenalty = float64(c.tokenCost) / 50000.0
	}
	boost := keywordBoostForStrategy(query, c.name)
	score := c.success + exploration + boost - 0.05*durationPenalty - 0.02*tokenPenalty
	if score < 0 {
		score = 0
	}
	return score
}

var strategyKeywords = map[string][]string{
	"react":            {"quick", "fast", "simple", "lookup", "immediately"},
	"exploratory":      {"brainstorm", "ideas", "explore", "options"},
	"research":         {"research", "market", "study", "analysis"},
	"scientific":       {"experiment", "hypothesis", "evidence", "data"},
	"simple":           {"summary", "brief", "short", "concise"},
	"chain_of_thought": {"reason", "explain", "steps", "detailed"},
}

func keywordBoostForStrategy(query, strategy string) float64 {
	keywords := strategyKeywords[strategy]
	if len(keywords) == 0 {
		return 0
	}
	lower := strings.ToLower(query)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return 0.05
		}
	}
	return 0
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
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
		if similarity > DecompositionSimilarityThreshold && prev.SuccessRate > PatternSuccessThreshold {
			suggestion.UsesPreviousSuccess = true
			suggestion.SuggestedSubtasks = prev.Subtasks
			suggestion.Strategy = prev.Strategy
			suggestion.Confidence = prev.SuccessRate * similarity
			metrics.DecompositionPatternCacheHits.Inc()
			break
		}
	}

	if !suggestion.UsesPreviousSuccess {
		metrics.DecompositionPatternCacheMisses.Inc()
	}

	// 2. Select optimal strategy based on performance history
	if !suggestion.UsesPreviousSuccess {
		suggestion.Strategy = da.selectOptimalStrategy()
	}

	// Track strategy selection
	if suggestion.Strategy != "" {
		metrics.StrategySelectionDistribution.WithLabelValues(suggestion.Strategy).Inc()
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
	if da.Memory.UserPreferences.SpeedVsAccuracy < SpeedPriorityThreshold {
		// Prioritize speed
		suggestion.Strategy = "parallel"
		suggestion.PreferSequential = false
	} else if da.Memory.UserPreferences.SpeedVsAccuracy > AccuracyPriorityThreshold {
		// Prioritize accuracy
		suggestion.Strategy = "sequential"
		suggestion.PreferSequential = true
	}

	return suggestion
}

func (da *DecompositionAdvisor) selectOptimalStrategy() string {
	// Use epsilon-greedy selection based on performance history
	epsilon := StrategyExplorationRate

	// Create per-goroutine random source for thread safety
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	if r.Float64() < epsilon {
		// Explore: try less-used strategies
		return da.selectLeastUsedStrategy()
	}

	// Exploit: use best performing strategy
	var bestStrategy string
	var bestScore float64

	for strategy, stats := range da.Memory.StrategyPerformance {
		// Balance success rate with speed based on user preference
		maxDuration := MaxDurationBaseline
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
	logger := getLoggerSafe(ctx)

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
		"pattern":     input.Query,
		"subtasks":    input.Subtasks,
		"strategy":    input.Strategy,
		"success":     input.Success,
		"duration_ms": input.DurationMs,
		"tokens_used": input.TokensUsed,
		"session_id":  input.SessionID,
		"timestamp":   time.Now().Unix(),
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
		logger.Error("Failed to store decomposition pattern",
			"error", err, "collection", collection,
			"session_id", input.SessionID, "strategy", input.Strategy)
		// TODO: Add metrics.VectorDBErrors when available

		// Fallback: try storing in generic task_embeddings so retrieval still has signal
		if _, fbErr := vdb.UpsertTaskEmbedding(ctx, embedding, payload); fbErr != nil {
			logger.Error("Fallback store to task_embeddings also failed",
				"error", fbErr, "session_id", input.SessionID)
			// TODO: Add metrics.VectorDBErrors when available
		} else {
			logger.Info("Successfully stored decomposition pattern in fallback collection",
				"session_id", input.SessionID)
		}
		// Non-critical error, don't fail the activity
		return nil
	}

	logger.Info("Recorded decomposition pattern",
		"strategy", input.Strategy,
		"success", input.Success,
		"subtasks", len(input.Subtasks))

	metrics.DecompositionPatternsRecorded.Inc()

	return nil
}

// cleanupStrategyPerformance removes expired entries from the strategy performance cache
// to prevent unbounded memory growth. Uses both TTL and size limits.
func cleanupStrategyPerformance(memory *SupervisorMemoryContext, logger log.Logger) {
	if memory == nil || memory.StrategyPerformance == nil {
		return
	}

	now := time.Now()
	expiredCount := 0

	// Remove entries older than TTL
	for strategy, stats := range memory.StrategyPerformance {
		if now.Sub(stats.LastAccessed) > StrategyPerformanceTTL {
			delete(memory.StrategyPerformance, strategy)
			expiredCount++
		}
	}

	// If still over limit, remove least recently accessed
	if len(memory.StrategyPerformance) > MaxStrategyPerformanceEntries {
		// Create a slice for sorting by LastAccessed
		type strategyEntry struct {
			strategy string
			stats    StrategyStats
		}
		entries := make([]strategyEntry, 0, len(memory.StrategyPerformance))
		for k, v := range memory.StrategyPerformance {
			entries = append(entries, strategyEntry{k, v})
		}

		// Sort by LastAccessed (oldest first)
		for i := 0; i < len(entries)-1; i++ {
			for j := i + 1; j < len(entries); j++ {
				if entries[i].stats.LastAccessed.After(entries[j].stats.LastAccessed) {
					entries[i], entries[j] = entries[j], entries[i]
				}
			}
		}

		// Remove oldest entries to stay within limit
		toRemove := len(entries) - MaxStrategyPerformanceEntries
		for i := 0; i < toRemove; i++ {
			delete(memory.StrategyPerformance, entries[i].strategy)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		logger.Debug("Cleaned up strategy performance cache",
			"removed_count", expiredCount,
			"remaining_count", len(memory.StrategyPerformance))
	}
}

// RecordLearningRouterMetrics records metrics for learning router recommendations
func RecordLearningRouterMetrics(ctx context.Context, input map[string]interface{}) error {
	latency, _ := input["latency_seconds"].(float64)
	strategy, _ := input["strategy"].(string)
	source, _ := input["source"].(string)
	confidence, _ := input["confidence"].(float64)
	success, _ := input["success"].(bool)

	successStr := "false"
	if success {
		successStr = "true"
	}

	// Record metrics using global metrics package
	metrics.LearningRouterLatency.Observe(latency)
	metrics.LearningRouterRecommendations.WithLabelValues(strategy, source, successStr).Inc()
	if success && strategy != "none" {
		metrics.LearningRouterConfidence.WithLabelValues(strategy).Observe(confidence)
	}

	return nil
}
