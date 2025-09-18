package workflows

import (
	"fmt"
	"strconv"
	"strings"
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
	if val, err := strconv.ParseFloat(response, 64); err == nil {
		return val, true
	}
	fields := strings.Fields(response)
	for _, field := range fields {
		field = strings.Trim(field, ".,!?:;")
		if val, err := strconv.ParseFloat(field, 64); err == nil {
			return val, true
		}
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
