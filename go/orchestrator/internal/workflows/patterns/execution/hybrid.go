package execution

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/agents"
)

// HybridConfig controls hybrid parallel/sequential execution with dependencies
type HybridConfig struct {
	MaxConcurrency           int                                       // Maximum concurrent agents
	EmitEvents               bool                                      // Whether to emit streaming events
	Context                  map[string]interface{}                    // Base context for all agents
	DependencyWaitTimeout    time.Duration                             // Max time to wait for dependencies
	DependencyCheckInterval  time.Duration                             // Interval between dependency checks (default 30s)
	PassDependencyResults    bool                                      // Pass dependency results to dependent tasks
	ClearDependentToolParams bool                                      // Clear tool params for dependent tasks
	PrefilledResults         map[string]activities.AgentExecutionResult // Pre-populated results from external sources (child workflows, previous phases)
}

// HybridTask represents a task with dependencies
type HybridTask struct {
	ID             string
	Description    string
	SuggestedTools []string
	ToolParameters map[string]interface{}
	PersonaID      string
	Role           string
	ParentArea     string
	Dependencies   []string // IDs of tasks this depends on
	// Optional per-task context overrides (e.g., task contracts)
	ContextOverrides map[string]interface{}
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
//
// ## Concurrency Model
//
// This function uses workflow.Go to launch concurrent coroutines. However, Temporal
// workflows execute in a **single-threaded, deterministic** event loop. The "concurrency"
// is cooperative, not preemptive:
//
//   - All workflow.Go coroutines run on the same thread
//   - Coroutines yield control at blocking operations (channel reads, activity calls, workflow.Sleep)
//   - Shared state (completedTasks, taskResults) is safe to access without mutexes
//   - The workflow scheduler ensures deterministic replay from event history
//
// This means:
//   - No race conditions between goroutines accessing shared maps
//   - No need for sync.Mutex on completedTasks or taskResults
//   - Execution is fully deterministic and replayable
//
// For more details, see: https://docs.temporal.io/develop/go/foundations#workflows
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

	// Track task completion status (safe without mutex due to Temporal's single-threaded model)
	completedTasks := make(map[string]bool)
	taskResults := make(map[string]activities.AgentExecutionResult)
	totalTokens := 0

	// Merge prefilled results from external sources (child workflows, previous execution phases)
	// This enables cross-phase dependency resolution
	if config.PrefilledResults != nil && len(config.PrefilledResults) > 0 {
		for id, result := range config.PrefilledResults {
			taskResults[id] = result
			completedTasks[id] = true
		}
		logger.Info("Prefilled dependency results from external sources",
			"prefilled_count", len(config.PrefilledResults),
		)
	}

	// Build task index for quick lookup
	taskIndex := make(map[string]*HybridTask)
	for i := range tasks {
		taskIndex[tasks[i].ID] = &tasks[i]
	}

	// Launch task executor for each task
	for i := range tasks {
		task := tasks[i]
		idx := i // Capture index for closure
		workflow.Go(ctx, func(ctx workflow.Context) {
			executeHybridTask(
				ctx,
				task,
				idx,
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
			completedTasks[result.TaskID] = true // Mark failed task as completed to unblock dependents
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
	originalIndex int,
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

		if !waitForDependencies(ctx, task.Dependencies, completedTasks, config.DependencyWaitTimeout, config.DependencyCheckInterval) {
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
	if task.ContextOverrides != nil {
		for k, v := range task.ContextOverrides {
			taskContext[k] = v
		}
	}
	taskContext["role"] = task.Role
	taskContext["task_id"] = task.ID
	if task.ParentArea != "" {
		taskContext["parent_area"] = task.ParentArea
	}

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
		if len(depResults) > 0 {
			taskContext["dependency_results"] = depResults
			logger.Info("Injected dependency_results into task context",
				"task_id", task.ID,
				"dependency_count", len(depResults),
			)
		}
	}

	// Clear tool parameters for dependent tasks if configured
	if config.ClearDependentToolParams && len(task.Dependencies) > 0 && task.ToolParameters != nil {
		task.ToolParameters = nil
	}

	// Compute agent name using station names for deterministic, human-readable IDs
	wid := workflow.GetInfo(ctx).WorkflowExecution.ID
	if config.Context != nil {
		if p, ok := config.Context["parent_workflow_id"].(string); ok && p != "" {
			wid = p
		}
	}
	agentName := agents.GetAgentName(wid, originalIndex)

	// Emit agent started event (parent workflow when available)
	if config.EmitEvents {
		_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
			activities.EmitTaskUpdateInput{
				WorkflowID: wid,
				EventType:  activities.StreamEventAgentStarted,
				AgentID:    agentName,
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil)
	}

	// Execute the task using parallel or sequential execution patterns
	parallelTask := ParallelTask{
		ID:               task.ID,
		Description:      task.Description,
		SuggestedTools:   task.SuggestedTools,
		ToolParameters:   task.ToolParameters,
		PersonaID:        task.PersonaID,
		Role:             task.Role,
		ContextOverrides: task.ContextOverrides,
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
		// Emit error event (parent workflow when available)
		if config.EmitEvents {
			_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
				activities.EmitTaskUpdateInput{
					WorkflowID: wid,
					EventType:  activities.StreamEventErrorOccurred,
					AgentID:    agentName,
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

	// Emit completion event (parent workflow when available)
	if config.EmitEvents && len(result.Results) > 0 {
		_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
			activities.EmitTaskUpdateInput{
				WorkflowID: wid,
				EventType:  activities.StreamEventAgentCompleted,
				AgentID:    agentName,
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

// waitForDependencies waits for all dependencies to complete using incremental timeouts.
// Instead of a single long timeout (e.g., 6 minutes), it uses multiple short check intervals
// (e.g., 30 seconds) so that Temporal UI shows more accurate timer durations.
func waitForDependencies(
	ctx workflow.Context,
	dependencies []string,
	completedTasks map[string]bool,
	timeout time.Duration,
	checkInterval time.Duration,
) bool {
	logger := workflow.GetLogger(ctx)

	// Default check interval: 30 seconds
	if checkInterval == 0 {
		checkInterval = 30 * time.Second
	}

	startTime := workflow.Now(ctx)
	deadline := startTime.Add(timeout)

	for workflow.Now(ctx).Before(deadline) {
		// Calculate wait time: minimum of check interval and remaining time
		remaining := deadline.Sub(workflow.Now(ctx))
		waitTime := checkInterval
		if remaining < waitTime {
			waitTime = remaining
		}

		ok, err := workflow.AwaitWithTimeout(ctx, waitTime, func() bool {
			for _, depID := range dependencies {
				if !completedTasks[depID] {
					return false
				}
			}
			return true
		})

		if err != nil {
			logger.Debug("Context cancelled during dependency wait", "error", err)
			return false
		}

		if ok {
			return true
		}

		// Condition not met, log progress and continue next iteration
		logger.Debug("Dependency check iteration",
			"dependencies", dependencies,
			"elapsed", workflow.Now(ctx).Sub(startTime),
			"remaining", remaining,
		)
	}

	logger.Warn("Dependency wait timeout",
		"dependencies", dependencies,
		"timeout", timeout,
	)
	return false
}
