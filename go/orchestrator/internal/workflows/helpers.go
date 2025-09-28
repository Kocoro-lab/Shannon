package workflows

import (
    "fmt"
    "strconv"
    "strings"

    "go.temporal.io/sdk/workflow"
    "go.temporal.io/sdk/log"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
)

// convertHistoryForAgent formats session history into a simple string slice for agents
func convertHistoryForAgent(messages []Message) []string {
	result := make([]string, len(messages))
	for i, msg := range messages {
		result[i] = fmt.Sprintf("%s: %s", msg.Role, msg.Content)
	}
	return result
}

// parseNumericValue attempts to extract a numeric value from a response string
func parseNumericValue(response string) (float64, bool) {
    response = strings.TrimSpace(response)
    // Direct parse of whole string
    if val, err := strconv.ParseFloat(response, 64); err == nil {
        return val, true
    }

    // Tokenize and collect all numeric tokens (handle punctuation)
    fields := strings.Fields(response)
    var numbers []float64
    for i := 0; i < len(fields); i++ {
        token := strings.Trim(fields[i], ".,!?:;")
        if v, err := strconv.ParseFloat(token, 64); err == nil {
            numbers = append(numbers, v)
        }
        // Prefer patterns like "equals N" or "is N"
        if (strings.EqualFold(token, "equals") || strings.EqualFold(token, "is")) && i+1 < len(fields) {
            next := strings.Trim(fields[i+1], ".,!?:;")
            if v, err := strconv.ParseFloat(next, 64); err == nil {
                return v, true
            }
        }
    }

    // Fallback: return the last number found (often the final answer)
    if len(numbers) > 0 {
        return numbers[len(numbers)-1], true
    }
    return 0, false
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// convertHistoryMapForCompression converts Message history to map format for compression
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

// fallbackToBasicMemory loads basic hierarchical memory for supervisor workflow
func fallbackToBasicMemory(ctx workflow.Context, input *TaskInput, logger log.Logger) {
	var memoryResult activities.FetchHierarchicalMemoryResult
	memoryInput := activities.FetchHierarchicalMemoryInput{
		Query:         input.Query,
		SessionID:     input.SessionID,
		TenantID:      input.TenantID,
		RecentTopK:    5,
		SemanticTopK:  3,
		SummaryTopK:   2,
		Threshold:     0.7,
	}

	if err := workflow.ExecuteActivity(ctx, "FetchHierarchicalMemory", memoryInput).Get(ctx, &memoryResult); err == nil {
		if len(memoryResult.Items) > 0 {
			if input.Context == nil {
				input.Context = make(map[string]interface{})
			}
			input.Context["agent_memory"] = memoryResult.Items
			logger.Info("Injected basic memory into supervisor context",
				"session_id", input.SessionID,
				"memory_items", len(memoryResult.Items))
		}
	}
}

// extractSubtaskDescriptions extracts descriptions from subtasks
func extractSubtaskDescriptions(subtasks []activities.Subtask) []string {
	descriptions := make([]string, len(subtasks))
	for i, st := range subtasks {
		descriptions[i] = st.Description
	}
	return descriptions
}
