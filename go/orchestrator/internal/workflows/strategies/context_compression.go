package strategies

import "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"

func compressionCounts(ctx map[string]interface{}) (int, int) {
	primersCount, recentsCount := 3, 20
	if ctx == nil {
		return primersCount, recentsCount
	}
	if value, ok := nonNegativeInt(ctx["primers_count"]); ok {
		primersCount = value
	}
	if value, ok := nonNegativeInt(ctx["recents_count"]); ok {
		recentsCount = value
	}
	return primersCount, recentsCount
}

func nonNegativeInt(value interface{}) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, typed >= 0
	case float64:
		return int(typed), typed >= 0
	default:
		return 0, false
	}
}

func newCompressContextInput(
	input TaskInput,
	targetTokens int,
	useConfiguredBoundaries bool,
) activities.CompressContextInput {
	compressInput := activities.CompressContextInput{
		SessionID:        input.SessionID,
		History:          convertHistoryMapForCompression(input.History),
		TargetTokens:     targetTokens,
		ParentWorkflowID: input.ParentWorkflowID,
	}
	if useConfiguredBoundaries {
		compressInput.PrimersCount, compressInput.RecentsCount = compressionCounts(
			input.Context,
		)
		compressInput.TenantID = input.TenantID
		compressInput.UseConfiguredBoundaries = true
	}
	return compressInput
}

// convertHistoryMapForCompression converts Message history to map format for compression.
func convertHistoryMapForCompression(messages []Message) []map[string]string {
	result := make([]map[string]string, len(messages))
	for i, msg := range messages {
		result[i] = map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}
	return result
}
