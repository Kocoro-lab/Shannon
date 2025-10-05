package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
)

// SelectAgentForTask uses performance-based selection (epsilon-greedy or UCB1)
// to choose an agent based on historical performance data.
//
// This is a utility function that can be called when performance-based agent
// selection is needed. Currently not wired into default execution paths.
//
// Example usage:
//
//	agentID, err := SelectAgentForTask(ctx, "task-123", []string{"agent-a", "agent-b"}, "agent-a")
func SelectAgentForTask(
	ctx workflow.Context,
	taskID string,
	availableAgents []string,
	defaultAgent string,
) (string, error) {
	logger := workflow.GetLogger(ctx)

	// Version gate for safe rollout
	selectionVersion := workflow.GetVersion(ctx, "agent_selection_v1", workflow.DefaultVersion, 1)
	if selectionVersion < 1 {
		// Use default agent (no selection logic)
		return defaultAgent, nil
	}

	// Fetch agent performance data
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
	})

	var performances []activities.AgentPerformance
	err := workflow.ExecuteActivity(activityCtx,
		activities.FetchAgentPerformances,
		activities.FetchAgentPerformancesInput{
			AgentIDs:  availableAgents,
			SessionID: "", // Can be scoped to session if needed
		}).Get(ctx, &performances)

	if err != nil {
		logger.Warn("Failed to fetch agent performances, using default",
			"task_id", taskID,
			"default_agent", defaultAgent,
			"error", err)
		return defaultAgent, nil
	}

	// Use epsilon-greedy selection (10% exploration)
	var selectionResult activities.SelectAgentEpsilonGreedyResult
	err = workflow.ExecuteActivity(activityCtx,
		activities.SelectAgentEpsilonGreedy,
		activities.SelectAgentEpsilonGreedyInput{
			Performances:      performances,
			AvailableAgentIDs: availableAgents,
			DefaultAgentID:    defaultAgent,
			Epsilon:           0.1, // 10% exploration rate
		}).Get(ctx, &selectionResult)

	if err != nil {
		logger.Warn("Agent selection failed, using default",
			"task_id", taskID,
			"default_agent", defaultAgent,
			"error", err)
		return defaultAgent, nil
	}

	logger.Info("Agent selected",
		"task_id", taskID,
		"selected_agent", selectionResult.SelectedAgentID,
		"is_exploration", selectionResult.IsExploration)

	return selectionResult.SelectedAgentID, nil
}

// SelectAgentUCB is an alternative using UCB1 algorithm instead of epsilon-greedy.
// UCB1 provides better exploration/exploitation balance for some scenarios.
func SelectAgentUCB(
	ctx workflow.Context,
	taskID string,
	availableAgents []string,
	defaultAgent string,
	totalSelections int,
) (string, error) {
	logger := workflow.GetLogger(ctx)

	selectionVersion := workflow.GetVersion(ctx, "agent_selection_ucb_v1", workflow.DefaultVersion, 1)
	if selectionVersion < 1 {
		return defaultAgent, nil
	}

	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
	})

	var performances []activities.AgentPerformance
	err := workflow.ExecuteActivity(activityCtx,
		activities.FetchAgentPerformances,
		activities.FetchAgentPerformancesInput{
			AgentIDs:  availableAgents,
			SessionID: "",
		}).Get(ctx, &performances)

	if err != nil {
		logger.Warn("Failed to fetch agent performances for UCB, using default",
			"task_id", taskID,
			"error", err)
		return defaultAgent, nil
	}

	var selectionResult activities.SelectAgentUCB1Result
	err = workflow.ExecuteActivity(activityCtx,
		activities.SelectAgentUCB1,
		activities.SelectAgentUCB1Input{
			Performances:    performances,
			TotalSelections: totalSelections,
			DefaultAgentID:  defaultAgent,
		}).Get(ctx, &selectionResult)

	if err != nil {
		logger.Warn("UCB1 selection failed, using default",
			"task_id", taskID,
			"error", err)
		return defaultAgent, nil
	}

	logger.Info("Agent selected via UCB1",
		"task_id", taskID,
		"selected_agent", selectionResult.SelectedAgentID,
		"ucb_score", selectionResult.UCBScore)

	return selectionResult.SelectedAgentID, nil
}
