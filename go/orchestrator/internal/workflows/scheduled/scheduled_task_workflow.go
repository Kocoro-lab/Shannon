package scheduled

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/schedules"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows"
)

// ScheduledTaskWorkflow wraps existing workflows for scheduled execution
func ScheduledTaskWorkflow(ctx workflow.Context, input schedules.ScheduledTaskInput) error {
	logger := workflow.GetLogger(ctx)

	scheduleID, _ := uuid.Parse(input.ScheduleID)
	userID, _ := uuid.Parse(input.UserID)
	tenantID, _ := uuid.Parse(input.TenantID)

	logger.Info("Scheduled task execution started",
		"schedule_id", input.ScheduleID,
		"query", input.TaskQuery,
	)

	// Activity context for recording
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	})

	// Generate unique child workflow ID - this is the task_id for unified tracking
	parentWorkflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
	childWorkflowID := fmt.Sprintf("%s-exec", parentWorkflowID)

	// 1. Record execution start with task_executions persistence
	err := workflow.ExecuteActivity(activityCtx, "RecordScheduleExecutionStart",
		activities.RecordScheduleExecutionInput{
			ScheduleID: scheduleID,
			TaskID:     childWorkflowID, // Use child workflow ID for task_executions
			Query:      input.TaskQuery,
			UserID:     input.UserID,
			TenantID:   input.TenantID,
		},
	).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to record execution start", "error", err)
		// Continue anyway - don't block execution
	}

	// 2. Prepare task input for existing workflow
	taskInput := workflows.TaskInput{
		Query:    input.TaskQuery,
		Context:  input.TaskContext,
		UserID:   userID.String(),
		TenantID: tenantID.String(),
	}

	// Add budget to context if specified
	if input.MaxBudgetPerRunUSD > 0 {
		if taskInput.Context == nil {
			taskInput.Context = make(map[string]interface{})
		}
		taskInput.Context["max_budget_usd"] = input.MaxBudgetPerRunUSD
	}

	// 3. Execute main workflow (use orchestrator router to select appropriate workflow type)
	childWorkflowOptions := workflow.ChildWorkflowOptions{
		WorkflowID:          childWorkflowID,
		TaskQueue:           "shannon-tasks",
		WorkflowRunTimeout:  workflow.GetInfo(ctx).WorkflowRunTimeout, // Inherit timeout
		WorkflowTaskTimeout: 10 * time.Second,
		Memo: map[string]interface{}{
			"schedule_id":  input.ScheduleID,
			"user_id":      input.UserID,
			"tenant_id":    input.TenantID,
			"trigger_type": "schedule",
		},
	}

	childCtx := workflow.WithChildOptions(ctx, childWorkflowOptions)

	var result workflows.TaskResult
	err = workflow.ExecuteChildWorkflow(childCtx, workflows.OrchestratorWorkflow, taskInput).Get(ctx, &result)

	// 4. Record execution result with task_executions persistence
	status := "COMPLETED"
	errorMsg := ""
	totalCost := 0.0
	resultText := ""

	// Metadata fields to extract from child workflow (Option A: unified model)
	var modelUsed, provider string
	var totalTokens, promptTokens, completionTokens int

	if err != nil {
		status = "FAILED"
		errorMsg = err.Error()
		logger.Error("Scheduled task failed", "error", err)
	} else if !result.Success {
		status = "FAILED"
		errorMsg = result.ErrorMessage
		logger.Warn("Scheduled task completed with failure", "error", errorMsg)
	} else {
		logger.Info("Scheduled task completed successfully")
		resultText = result.Result

		// Extract all metadata from child workflow result for unified task_executions
		if result.Metadata != nil {
			// Cost: try total_cost_usd first, then cost_usd
			if cost, ok := result.Metadata["total_cost_usd"].(float64); ok {
				totalCost = cost
			}
			if cost, ok := result.Metadata["cost_usd"].(float64); ok && totalCost == 0 {
				totalCost = cost
			}

			// Model: try model_used first, then model
			if m, ok := result.Metadata["model_used"].(string); ok && m != "" {
				modelUsed = m
			} else if m, ok := result.Metadata["model"].(string); ok && m != "" {
				modelUsed = m
			}

			// Provider
			if p, ok := result.Metadata["provider"].(string); ok {
				provider = p
			}

			// Tokens: handle both int and float64 (JSON unmarshals numbers as float64)
			if t, ok := result.Metadata["total_tokens"].(int); ok {
				totalTokens = t
			} else if t, ok := result.Metadata["total_tokens"].(float64); ok {
				totalTokens = int(t)
			}

			if t, ok := result.Metadata["input_tokens"].(int); ok {
				promptTokens = t
			} else if t, ok := result.Metadata["input_tokens"].(float64); ok {
				promptTokens = int(t)
			}

			if t, ok := result.Metadata["output_tokens"].(int); ok {
				completionTokens = t
			} else if t, ok := result.Metadata["output_tokens"].(float64); ok {
				completionTokens = int(t)
			}

			logger.Info("Extracted metadata from child workflow",
				"model", modelUsed,
				"provider", provider,
				"total_tokens", totalTokens,
				"cost", totalCost,
			)
		}
	}

	err = workflow.ExecuteActivity(activityCtx, "RecordScheduleExecutionComplete",
		activities.RecordScheduleExecutionCompleteInput{
			ScheduleID:       scheduleID,
			TaskID:           childWorkflowID,
			Status:           status,
			TotalCost:        totalCost,
			ErrorMsg:         errorMsg,
			Result:           resultText,
			ModelUsed:        modelUsed,
			Provider:         provider,
			TotalTokens:      totalTokens,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
		},
	).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to record execution completion", "error", err)
	}

	// 5. Return error if task failed (for Temporal's built-in retry/failure handling)
	if status == "FAILED" {
		return fmt.Errorf("scheduled task failed: %s", errorMsg)
	}

	return nil
}
