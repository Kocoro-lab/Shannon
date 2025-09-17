package workflows

import (
    "time"

    "go.temporal.io/sdk/workflow"
    "go.temporal.io/sdk/temporal"

    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
)

// SimpleTaskWorkflow handles simple, single-agent tasks efficiently
// This workflow minimizes events by using a single consolidated activity
func SimpleTaskWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
    logger := workflow.GetLogger(ctx)
    logger.Info("Starting SimpleTaskWorkflow",
        "query", input.Query,
        "user_id", input.UserID,
        "session_id", input.SessionID,
    )
    
    // Configure activity options  
    activityOptions := workflow.ActivityOptions{
        StartToCloseTimeout: 2 * time.Minute, // Simple tasks should be fast
        RetryPolicy: &temporal.RetryPolicy{
            MaximumAttempts: 2, // Fewer retries for simple tasks
        },
    }
    ctx = workflow.WithActivityOptions(ctx, activityOptions)
    
    // Execute the consolidated simple task activity
    // This single activity handles everything: agent execution, session update, etc.
    var result activities.ExecuteSimpleTaskResult
    err := workflow.ExecuteActivity(ctx, activities.ExecuteSimpleTask, activities.ExecuteSimpleTaskInput{
        Query:      input.Query,
        UserID:     input.UserID,
        SessionID:  input.SessionID,
        Context:    input.Context,
        SessionCtx: input.SessionCtx,
        History:    convertHistoryForAgent(input.History),
    }).Get(ctx, &result)
    
    if err != nil {
        logger.Error("Simple task execution failed", "error", err)
        return TaskResult{
            Success:      false,
            ErrorMessage: err.Error(),
        }, err
    }
    
    // Persist to vector store for future context retrieval (fire-and-forget)
    if input.SessionID != "" {
        ro := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 3}}
        detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
        dctx := workflow.WithActivityOptions(detachedCtx, ro)
        // Schedule without waiting on result
        workflow.ExecuteActivity(dctx, activities.RecordQuery, activities.RecordQueryInput{
            SessionID: input.SessionID,
            UserID:    input.UserID,
            Query:     input.Query,
            Answer:    result.Response,
            Model:     "simple-agent",
            Metadata:  map[string]interface{}{"workflow": "simple", "mode": "simple", "tenant_id": input.TenantID},
            RedactPII: true,
        })
    }
    
    // Update session with token usage
    if input.SessionID != "" {
        var sessionUpdateResult activities.SessionUpdateResult
        err = workflow.ExecuteActivity(ctx,
            constants.UpdateSessionResultActivity,
            activities.SessionUpdateInput{
                SessionID:  input.SessionID,
                Result:     result.Response,
                TokensUsed: result.TokensUsed,
                AgentsUsed: 1,
            },
        ).Get(ctx, &sessionUpdateResult)
        if err != nil {
            logger.Warn("Failed to update session with tokens",
                "session_id", input.SessionID,
                "error", err,
            )
        }
    }

    logger.Info("SimpleTaskWorkflow completed successfully",
        "tokens_used", result.TokensUsed,
    )

    return TaskResult{
        Result:     result.Response,
        Success:    true,
        TokensUsed: result.TokensUsed,
        Metadata: map[string]interface{}{
            "mode":       "simple",
            "num_agents": 1,
        },
    }, nil
}
