package execution

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
)

// ParallelConfig controls parallel execution behavior
type ParallelConfig struct {
	MaxConcurrency int                    // Maximum concurrent agents
	Semaphore      workflow.Semaphore     // Concurrency control (interface, not pointer)
	EmitEvents     bool                   // Whether to emit streaming events
	Context        map[string]interface{} // Base context for all agents
}

// ParallelTask represents a task to execute in parallel
type ParallelTask struct {
	ID             string
	Description    string
	SuggestedTools []string
	ToolParameters map[string]interface{}
	PersonaID      string
	Role           string
	Dependencies   []string // For hybrid parallel/sequential execution
}

// ParallelResult contains results from parallel execution
type ParallelResult struct {
	Results     []activities.AgentExecutionResult
	TotalTokens int
	Metadata    map[string]interface{}
}

// ExecuteParallel runs multiple tasks in parallel with concurrency control.
// It supports optional budget enforcement and streaming events.
func ExecuteParallel(
	ctx workflow.Context,
	tasks []ParallelTask,
	sessionID string,
	history []string,
	config ParallelConfig,
	budgetPerAgent int,
	userID string,
	modelTier string,
) (*ParallelResult, error) {

	logger := workflow.GetLogger(ctx)
	logger.Info("Starting parallel execution",
		"task_count", len(tasks),
		"max_concurrency", config.MaxConcurrency,
	)

	// Create semaphore if not provided
	if config.Semaphore == nil {
		config.Semaphore = workflow.NewSemaphore(ctx, int64(config.MaxConcurrency))
	}

	// Channel for collecting in-flight futures with a release handshake
	futuresChan := workflow.NewChannel(ctx)

	// Track futures with their original index
	type futureWithIndex struct {
		Index   int
		Future  workflow.Future
		Release workflow.Channel // send a signal when it's safe to release the semaphore
	}

	// Activity options
	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOpts)

	// Launch parallel executions
	for i, task := range tasks {
		i := i       // Capture for closure
		task := task // Capture for closure

		workflow.Go(ctx, func(ctx workflow.Context) {
			// Acquire semaphore
			if err := config.Semaphore.Acquire(ctx, 1); err != nil {
				logger.Error("Failed to acquire semaphore",
					"task_id", task.ID,
					"error", err,
				)
				futuresChan.Send(ctx, futureWithIndex{Index: i, Future: nil, Release: nil})
				return
			}
			// Create a release channel so the collector can signal when to release
			rel := workflow.NewChannel(ctx)

			// Prepare task context
			taskContext := make(map[string]interface{})
			for k, v := range config.Context {
				taskContext[k] = v
			}
			taskContext["role"] = task.Role
			taskContext["task_id"] = task.ID

			// Emit agent started event
			if config.EmitEvents {
				wid := workflow.GetInfo(ctx).WorkflowExecution.ID
				_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
					activities.EmitTaskUpdateInput{
						WorkflowID: wid,
						EventType:  activities.StreamEventAgentStarted,
						AgentID:    fmt.Sprintf("agent-%s", task.ID),
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil)
			}

			// Execute agent
			var future workflow.Future

			if budgetPerAgent > 0 {
				// Execute with budget
				wid := workflow.GetInfo(ctx).WorkflowExecution.ID
				future = workflow.ExecuteActivity(ctx,
					constants.ExecuteAgentWithBudgetActivity,
					activities.BudgetedAgentInput{
						AgentInput: activities.AgentExecutionInput{
							Query:          task.Description,
							AgentID:        fmt.Sprintf("agent-%s", task.ID),
							Context:        taskContext,
							Mode:           "standard",
							SessionID:      sessionID,
							History:        history,
							SuggestedTools: task.SuggestedTools,
							ToolParameters: task.ToolParameters,
							PersonaID:      task.PersonaID,
						},
						MaxTokens: budgetPerAgent,
						UserID:    userID,
						TaskID:    wid,
						ModelTier: modelTier,
					})
			} else {
				// Execute without budget
				future = workflow.ExecuteActivity(ctx,
					activities.ExecuteAgent,
					activities.AgentExecutionInput{
						Query:          task.Description,
						AgentID:        fmt.Sprintf("agent-%s", task.ID),
						Context:        taskContext,
						Mode:           "standard",
						SessionID:      sessionID,
						History:        history,
						SuggestedTools: task.SuggestedTools,
						ToolParameters: task.ToolParameters,
						PersonaID:      task.PersonaID,
					})
			}

			futuresChan.Send(ctx, futureWithIndex{Index: i, Future: future, Release: rel})

			// Hold the permit until the collector signals that it has finished processing the result
			var _sig struct{}
			rel.Receive(ctx, &_sig)
			// Now safe to release the semaphore
			config.Semaphore.Release(1)
		})
	}

	// Collect results
	results := make([]activities.AgentExecutionResult, len(tasks))
	totalTokens := 0
	successCount := 0
	errorCount := 0

	for i := 0; i < len(tasks); i++ {
		var fwi futureWithIndex
		futuresChan.Receive(ctx, &fwi)

		if fwi.Future == nil {
			errorCount++
			// Nothing was acquired; no release needed
			continue
		}

		var result activities.AgentExecutionResult
		err := fwi.Future.Get(ctx, &result)

		if err != nil {
			logger.Error("Agent execution failed",
				"task_id", tasks[fwi.Index].ID,
				"error", err,
			)
			errorCount++

			// Emit error event
			if config.EmitEvents {
				wid := workflow.GetInfo(ctx).WorkflowExecution.ID
				_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
					activities.EmitTaskUpdateInput{
						WorkflowID: wid,
						EventType:  activities.StreamEventErrorOccurred,
						AgentID:    fmt.Sprintf("agent-%s", tasks[fwi.Index].ID),
						Message:    err.Error(),
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil)
			}
		} else {
			results[fwi.Index] = result
			totalTokens += result.TokensUsed
			successCount++

			// Emit completion event
			if config.EmitEvents {
				wid := workflow.GetInfo(ctx).WorkflowExecution.ID
				_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
					activities.EmitTaskUpdateInput{
						WorkflowID: wid,
						EventType:  activities.StreamEventAgentCompleted,
						AgentID:    fmt.Sprintf("agent-%s", tasks[fwi.Index].ID),
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil)
			}

			// Record agent memory if session exists
			if sessionID != "" {
				detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
				workflow.ExecuteActivity(detachedCtx,
					activities.RecordAgentMemory,
					activities.RecordAgentMemoryInput{
						SessionID: sessionID,
						UserID:    userID,
						AgentID:   result.AgentID,
						Role:      tasks[fwi.Index].Role,
						Query:     tasks[fwi.Index].Description,
						Answer:    result.Response,
						Model:     result.ModelUsed,
						RedactPII: true,
						Extra: map[string]interface{}{
							"task_id": tasks[fwi.Index].ID,
						},
					})
			}

			// Signal the producer goroutine that we're done with this future
			if fwi.Release != nil {
				var sig struct{}
				fwi.Release.Send(ctx, sig)
			}
		}
	}

	logger.Info("Parallel execution completed",
		"total_tasks", len(tasks),
		"successful", successCount,
		"failed", errorCount,
		"total_tokens", totalTokens,
	)

	return &ParallelResult{
		Results:     results,
		TotalTokens: totalTokens,
		Metadata: map[string]interface{}{
			"total_tasks": len(tasks),
			"successful":  successCount,
			"failed":      errorCount,
		},
	}, nil
}

// no local helpers; callers must pass []string history
