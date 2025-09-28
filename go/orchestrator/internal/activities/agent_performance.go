package activities

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// rngGuard protects the shared RNG to keep epsilon-greedy selection goroutine-safe.
var (
	rngGuard sync.Mutex
	rng      = rand.New(rand.NewSource(time.Now().UnixNano()))
)

func randFloat64() float64 {
	rngGuard.Lock()
	defer rngGuard.Unlock()
	return rng.Float64()
}

func randIntn(n int) int {
	rngGuard.Lock()
	defer rngGuard.Unlock()
	return rng.Intn(n)
}

// AgentPerformanceInput requests performance metrics for agent selection
type AgentPerformanceInput struct {
	Mode           string        `json:"mode"`            // Task mode to filter by (e.g., "simple", "research")
	LookbackPeriod time.Duration `json:"lookback_period"` // How far back to look (e.g., 7 days)
	MinSamples     int           `json:"min_samples"`     // Minimum executions required for consideration
}

// AgentPerformance represents an agent's historical performance
type AgentPerformance struct {
	AgentID     string  `json:"agent_id"`
	SuccessRate float64 `json:"success_rate"` // 0.0 to 1.0
	TotalRuns   int     `json:"total_runs"`
	AvgTokens   int     `json:"avg_tokens"`
	AvgDuration int64   `json:"avg_duration_ms"`
}

// AgentPerformanceResult contains performance metrics for agent selection
type AgentPerformanceResult struct {
	Performances []AgentPerformance `json:"performances"`
	BestAgentID  string             `json:"best_agent_id"`
}

// GetAgentPerformanceMetrics queries historical agent performance
// This activity is used for performance-based agent selection (Phase 2)
func GetAgentPerformanceMetrics(ctx context.Context, db *sql.DB, in AgentPerformanceInput) (AgentPerformanceResult, error) {
	if db == nil {
		// Return default if no database
		return AgentPerformanceResult{}, nil
	}

	const defaultAgentLimit = 50

	// Join with task_executions to filter by mode and time window
	query := `
        SELECT
            ae.agent_id,
            AVG(CASE WHEN ae.state = 'COMPLETED' THEN 1.0 ELSE 0.0 END) AS success_rate,
            COUNT(*) as total_runs,
            AVG(ae.tokens_used) as avg_tokens,
            AVG(ae.duration_ms) as avg_duration_ms
        FROM agent_executions ae
        LEFT JOIN task_executions te ON ae.workflow_id = te.workflow_id
        WHERE ae.created_at > $1
            AND ($2 = '' OR te.mode = $2)
        GROUP BY ae.agent_id
        HAVING COUNT(*) >= $3
        ORDER BY success_rate DESC
        LIMIT $4
    `

	lookbackTime := time.Now().Add(-in.LookbackPeriod)
	minSamples := in.MinSamples
	if minSamples <= 0 {
		minSamples = 5 // Default minimum samples
	}

	rows, err := db.QueryContext(ctx, query, lookbackTime, in.Mode, minSamples, defaultAgentLimit)
	if err != nil {
		return AgentPerformanceResult{}, fmt.Errorf("query agent performance: %w", err)
	}
	defer rows.Close()

	var performances []AgentPerformance
	for rows.Next() {
		var perf AgentPerformance
		var avgTokens, avgDuration sql.NullFloat64

		err := rows.Scan(
			&perf.AgentID,
			&perf.SuccessRate,
			&perf.TotalRuns,
			&avgTokens,
			&avgDuration,
		)
		if err != nil {
			continue // Skip malformed rows
		}

		if avgTokens.Valid {
			perf.AvgTokens = int(avgTokens.Float64)
		}
		if avgDuration.Valid {
			perf.AvgDuration = int64(avgDuration.Float64)
		}

		performances = append(performances, perf)
	}

	// Identify best performer
	bestAgentID := ""
	if len(performances) > 0 {
		bestAgentID = performances[0].AgentID // Already sorted by success_rate DESC
	}

	return AgentPerformanceResult{
		Performances: performances,
		BestAgentID:  bestAgentID,
	}, nil
}

// SelectAgentEpsilonGreedy implements epsilon-greedy agent selection
// With probability epsilon, explore (random selection)
// With probability 1-epsilon, exploit (best performer)
type SelectAgentEpsilonGreedyInput struct {
	Performances      []AgentPerformance `json:"performances"`
	Epsilon           float64            `json:"epsilon"`             // Exploration rate (0.0 to 1.0)
	DefaultAgentID    string             `json:"default_agent_id"`    // Fallback if no performance data
	AvailableAgentIDs []string           `json:"available_agent_ids"` // Pool of available agents
}

type SelectAgentEpsilonGreedyResult struct {
	SelectedAgentID string `json:"selected_agent_id"`
	IsExploration   bool   `json:"is_exploration"`
}

// SelectAgentEpsilonGreedy selects an agent using epsilon-greedy strategy
func SelectAgentEpsilonGreedy(ctx context.Context, in SelectAgentEpsilonGreedyInput) (SelectAgentEpsilonGreedyResult, error) {
	// If no performance data, use default
	if len(in.Performances) == 0 {
		return SelectAgentEpsilonGreedyResult{
			SelectedAgentID: in.DefaultAgentID,
			IsExploration:   false,
		}, nil
	}

	// Epsilon-greedy selection using shared RNG guarded by mutex
	r := randFloat64()

	if r < in.Epsilon {
		// Exploration: random selection from available agents
		if len(in.AvailableAgentIDs) > 0 {
			idx := randIntn(len(in.AvailableAgentIDs))
			return SelectAgentEpsilonGreedyResult{
				SelectedAgentID: in.AvailableAgentIDs[idx],
				IsExploration:   true,
			}, nil
		}
		// Fallback to random from performances
		idx := randIntn(len(in.Performances))
		return SelectAgentEpsilonGreedyResult{
			SelectedAgentID: in.Performances[idx].AgentID,
			IsExploration:   true,
		}, nil
	}

	// Exploitation: select best performer
	return SelectAgentEpsilonGreedyResult{
		SelectedAgentID: in.Performances[0].AgentID, // Already sorted by performance
		IsExploration:   false,
	}, nil
}

// Alternative: UCB1 (Upper Confidence Bound) selection for better exploration
type SelectAgentUCB1Input struct {
	Performances    []AgentPerformance `json:"performances"`
	TotalSelections int                `json:"total_selections"` // Total times any agent has been selected
	DefaultAgentID  string             `json:"default_agent_id"`
}

type SelectAgentUCB1Result struct {
	SelectedAgentID string  `json:"selected_agent_id"`
	UCBScore        float64 `json:"ucb_score"`
}

// SelectAgentUCB1 implements Upper Confidence Bound selection
// Balances exploration and exploitation better than epsilon-greedy
func SelectAgentUCB1(ctx context.Context, in SelectAgentUCB1Input) (SelectAgentUCB1Result, error) {
	if len(in.Performances) == 0 {
		return SelectAgentUCB1Result{
			SelectedAgentID: in.DefaultAgentID,
			UCBScore:        0,
		}, nil
	}

	// Calculate UCB1 score for each agent
	// UCB1 = success_rate + sqrt(2 * ln(total_selections) / agent_runs)
	bestAgentID := ""
	bestScore := -1.0

	for _, perf := range in.Performances {
		// Exploration bonus decreases as agent is selected more
		explorationBonus := 0.0
		if perf.TotalRuns > 0 && in.TotalSelections > 0 {
			explorationBonus = 1.41421356 * // sqrt(2)
				(float64(in.TotalSelections) / float64(perf.TotalRuns))
		}

		ucbScore := perf.SuccessRate + explorationBonus

		if ucbScore > bestScore {
			bestScore = ucbScore
			bestAgentID = perf.AgentID
		}
	}

	return SelectAgentUCB1Result{
		SelectedAgentID: bestAgentID,
		UCBScore:        bestScore,
	}, nil
}
