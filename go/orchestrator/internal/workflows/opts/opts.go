package opts

import (
    "time"

    "go.temporal.io/sdk/temporal"
    "go.temporal.io/sdk/workflow"
)

// TokenRecordActivityOptions returns standardized activity options for token recording
func TokenRecordActivityOptions() workflow.ActivityOptions {
    return workflow.ActivityOptions{
        StartToCloseTimeout: 10 * time.Second,
        RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
    }
}

// WithTokenRecordOptions applies standardized token record activity options to a context
func WithTokenRecordOptions(ctx workflow.Context) workflow.Context {
    return workflow.WithActivityOptions(ctx, TokenRecordActivityOptions())
}

