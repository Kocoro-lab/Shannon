package execution

import (
    "fmt"
    "time"

    "go.temporal.io/sdk/workflow"

    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
)

// HybridConfig controls hybrid parallel/sequential execution with dependencies
type HybridConfig struct {
	MaxConcurrency          int                    // Maximum concurrent agents
	EmitEvents              bool                   // Whether to emit streaming events
	Context                 map[string]interface{} // Base context for all agents
	DependencyWaitTimeout   time.Duration          // Max time to wait for dependencies
	PassDependencyResults   bool                   // Pass dependency results to dependent tasks
	ClearDependentToolParams bool                  // Clear tool params for dependent tasks
}

// HybridTask represents a task with dependencies
type HybridTask struct {
	ID             string
	Description    string
	SuggestedTools []string
	ToolParameters map[string]interface{}
	PersonaID      string
	Role           string
	Dependencies   []string // IDs of tasks this depends on
}

// HybridResult contains results from hybrid execution
type HybridResult struct {
	Results     map[string]activities.AgentExecutionResult // Keyed by task ID
	TotalTokens int
	Metadata    map[string]interface{}
}

// ExecuteHybrid runs tasks with dependency management.
// Tasks without dependencies run in parallel up to MaxConcurrency.
// Tasks with dependencies wait for their dependencies to complete first.
func ExecuteHybrid(
    ctx workflow.Context,
    tasks []HybridTask,
    sessionID string,
    history []string,
    config HybridConfig,
    budgetPerAgent int,
    userID string,
    modelTier string,
) (*HybridResult, error) {

	logger := workflow.GetLogger(ctx)
	logger.Info("Starting hybrid execution",
		"task_count", len(tasks),
		"max_concurrency", config.MaxConcurrency,
	)

	// Create channels for coordination
	semaphore := workflow.NewSemaphore(ctx, int64(config.MaxConcurrency))
	resultsChan := workflow.NewChannel(ctx)

	// Track task completion status
	completedTasks := make(map[string]bool)
	taskResults := make(map[string]activities.AgentExecutionResult)
	totalTokens := 0

	// Build task index for quick lookup
	taskIndex := make(map[string]*HybridTask)
	for i := range tasks {
		taskIndex[tasks[i].ID] = &tasks[i]
	}

	// Launch task executor for each task
	for i := range tasks {
		task := tasks[i]
		workflow.Go(ctx, func(ctx workflow.Context) {
			executeHybridTask(
				ctx,
				task,
				taskIndex,
				completedTasks,
				taskResults,
				semaphore,
				resultsChan,
				sessionID,
				history,
				config,
				budgetPerAgent,
				userID,
				modelTier,
			)
		})
	}

	// Collect results
	successCount := 0
	errorCount := 0

	for i := 0; i < len(tasks); i++ {
		var result taskExecutionResult
		resultsChan.Receive(ctx, &result)

		if result.Error != nil {
			logger.Error("Task execution failed",
				"task_id", result.TaskID,
				"error", result.Error,
			)
			errorCount++
		} else {
			completedTasks[result.TaskID] = true
			taskResults[result.TaskID] = result.Result
			totalTokens += result.Result.TokensUsed
			successCount++
		}
	}

	logger.Info("Hybrid execution completed",
		"total_tasks", len(tasks),
		"successful", successCount,
		"failed", errorCount,
		"total_tokens", totalTokens,
	)

	return &HybridResult{
		Results:     taskResults,
		TotalTokens: totalTokens,
		Metadata: map[string]interface{}{
			"total_tasks": len(tasks),
			"successful":  successCount,
			"failed":      errorCount,
		},
	}, nil
}

// taskExecutionResult is sent through the results channel
type taskExecutionResult struct {
	TaskID string
	Result activities.AgentExecutionResult
	Error  error
}

// executeHybridTask handles execution of a single task with dependency management
func executeHybridTask(
    ctx workflow.Context,
    task HybridTask,
    taskIndex map[string]*HybridTask,
    completedTasks map[string]bool,
    taskResults map[string]activities.AgentExecutionResult,
    semaphore workflow.Semaphore,
    resultsChan workflow.Channel,
    sessionID string,
    history []string,
    config HybridConfig,
    budgetPerAgent int,
    userID string,
    modelTier string,
) {
	logger := workflow.GetLogger(ctx)

	// Wait for dependencies if any
	if len(task.Dependencies) > 0 {
		logger.Info("Waiting for dependencies",
			"task_id", task.ID,
			"dependencies", task.Dependencies,
		)

		if !waitForDependencies(ctx, task.Dependencies, completedTasks, config.DependencyWaitTimeout) {
			resultsChan.Send(ctx, taskExecutionResult{
				TaskID: task.ID,
				Error:  fmt.Errorf("timeout waiting for dependencies"),
			})
			return
		}

		logger.Info("Dependencies satisfied",
			"task_id", task.ID,
		)
	}

	// Acquire semaphore for execution
	if err := semaphore.Acquire(ctx, 1); err != nil {
		resultsChan.Send(ctx, taskExecutionResult{
			TaskID: task.ID,
			Error:  fmt.Errorf("failed to acquire semaphore: %w", err),
		})
		return
	}
	defer semaphore.Release(1)

	// Prepare task context
	taskContext := make(map[string]interface{})
	for k, v := range config.Context {
		taskContext[k] = v
	}
	taskContext["role"] = task.Role
	taskContext["task_id"] = task.ID

	// Add dependency results if configured
	if config.PassDependencyResults && len(task.Dependencies) > 0 {
		depResults := make(map[string]interface{})
		for _, depID := range task.Dependencies {
			if result, ok := taskResults[depID]; ok {
				depResults[depID] = map[string]interface{}{
					"response": result.Response,
					"tokens":   result.TokensUsed,
					"success":  result.Success,
				}
			}
		}
		taskContext["dependency_results"] = depResults
	}

	// Clear tool parameters for dependent tasks if configured
	if config.ClearDependentToolParams && len(task.Dependencies) > 0 && task.ToolParameters != nil {
		logger.Info("Clearing tool_parameters for dependent task",
			"task_id", task.ID,
		)
		task.ToolParameters = nil
	}

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

	// Execute the task using parallel or sequential execution patterns
	parallelTask := ParallelTask{
		ID:             task.ID,
		Description:    task.Description,
		SuggestedTools: task.SuggestedTools,
		ToolParameters: task.ToolParameters,
		PersonaID:      task.PersonaID,
		Role:           task.Role,
	}

	parallelConfig := ParallelConfig{
		MaxConcurrency: 1, // Single task execution
		Context:        taskContext,
		EmitEvents:     false, // Already handled
	}

	result, err := ExecuteParallel(
		ctx,
		[]ParallelTask{parallelTask},
		sessionID,
		history,
		parallelConfig,
		budgetPerAgent,
		userID,
		modelTier,
	)

	if err != nil {
		// Emit error event
		if config.EmitEvents {
			wid := workflow.GetInfo(ctx).WorkflowExecution.ID
			_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
				activities.EmitTaskUpdateInput{
					WorkflowID: wid,
					EventType:  activities.StreamEventErrorOccurred,
					AgentID:    fmt.Sprintf("agent-%s", task.ID),
					Message:    err.Error(),
					Timestamp:  workflow.Now(ctx),
				}).Get(ctx, nil)
		}

		resultsChan.Send(ctx, taskExecutionResult{
			TaskID: task.ID,
			Error:  err,
		})
		return
	}

	// Emit completion event
	if config.EmitEvents && len(result.Results) > 0 {
		wid := workflow.GetInfo(ctx).WorkflowExecution.ID
		_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
			activities.EmitTaskUpdateInput{
				WorkflowID: wid,
				EventType:  activities.StreamEventAgentCompleted,
				AgentID:    fmt.Sprintf("agent-%s", task.ID),
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil)
	}

	// Send result
	if len(result.Results) > 0 {
		resultsChan.Send(ctx, taskExecutionResult{
			TaskID: task.ID,
			Result: result.Results[0],
		})
	} else {
		resultsChan.Send(ctx, taskExecutionResult{
			TaskID: task.ID,
			Error:  fmt.Errorf("no result from execution"),
		})
	}
}

// waitForDependencies waits for all dependencies to complete
func waitForDependencies(
	ctx workflow.Context,
	dependencies []string,
	completedTasks map[string]bool,
	timeout time.Duration,
) bool {
	logger := workflow.GetLogger(ctx)

	// Calculate deadline
	deadline := workflow.Now(ctx).Add(timeout)

	// Check dependencies with polling
	for {
		allComplete := true
		for _, depID := range dependencies {
			if !completedTasks[depID] {
				allComplete = false
				break
			}
		}

		if allComplete {
			return true
		}

		// Check timeout
		if workflow.Now(ctx).After(deadline) {
			logger.Warn("Dependency wait timeout",
				"dependencies", dependencies,
				"timeout", timeout,
			)
			return false
		}

		// Wait before next check
		workflow.Sleep(ctx, 3*time.Second)
	}
}
