package util

import (
	"strconv"
	"strings"
)

// ContainsString reports whether slice contains item.
func ContainsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ParseNumericValue attempts to extract a numeric value from a free‑form string.
// Preference order: direct parse, "equals|is N" pattern, then last numeric token.
func ParseNumericValue(response string) (float64, bool) {
	response = strings.TrimSpace(response)
	if val, err := strconv.ParseFloat(response, 64); err == nil {
		return val, true
	}
	fields := strings.Fields(response)
	var numbers []float64
	for i := 0; i < len(fields); i++ {
		token := strings.Trim(fields[i], ".,!?:;")
		if v, err := strconv.ParseFloat(token, 64); err == nil {
			numbers = append(numbers, v)
		}
		if (strings.EqualFold(token, "equals") || strings.EqualFold(token, "is")) && i+1 < len(fields) {
			next := strings.Trim(fields[i+1], ".,!?:;")
			if v, err := strconv.ParseFloat(next, 64); err == nil {
				return v, true
			}
		}
	}
	if len(numbers) > 0 {
		return numbers[len(numbers)-1], true
	}
	return 0, false
}

// TruncateString truncates s to maxLen and appends "..." if truncated (UTF-8 safe).
// If preserveWords is true, truncates at the last space before maxLen when possible.
func TruncateString(s string, maxLen int, preserveWords bool) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	// Reserve space for ellipsis
	if maxLen <= 3 {
		return "..."[:maxLen]
	}
	cut := maxLen - 3
	if preserveWords {
		// Find last space before cut (in rune positions)
		if idx := lastSpaceBeforeRune(s, cut); idx > 0 {
			cut = idx
		}
	}
	return string(runes[:cut]) + "..."
}

func lastSpaceBefore(s string, pos int) int {
	if pos > len(s) {
		pos = len(s)
	}
	for i := pos - 1; i >= 0; i-- {
		if s[i] == ' ' || s[i] == '\t' || s[i] == '\n' {
			return i
		}
	}
	return -1
}

// lastSpaceBeforeRune finds the last space before pos (in rune count, UTF-8 safe)
func lastSpaceBeforeRune(s string, pos int) int {
	runes := []rune(s)
	if pos > len(runes) {
		pos = len(runes)
	}
	for i := pos - 1; i >= 0; i-- {
		if runes[i] == ' ' || runes[i] == '\t' || runes[i] == '\n' {
			return i
		}
	}
	return -1
}
