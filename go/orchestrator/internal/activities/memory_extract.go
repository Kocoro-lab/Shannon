package activities

import (
	"context"

	"go.temporal.io/sdk/activity"
	"go.uber.org/zap"
)

// MemoryExtractInput is the input for the memory extraction activity.
// Enterprise feature: extracts key facts from conversations into persistent user memory.
type MemoryExtractInput struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	Query     string `json:"query"`
	Response  string `json:"response"`
}

// ExtractMemoryActivity is a no-op stub in the OSS build.
// Enterprise builds extract and persist user memories from conversations.
func ExtractMemoryActivity(ctx context.Context, input MemoryExtractInput) error {
	activity.GetLogger(ctx).Debug("Memory extraction is an enterprise feature (no-op in OSS)",
		zap.String("user_id", input.UserID),
	)
	return nil
}
