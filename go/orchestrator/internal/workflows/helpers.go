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
