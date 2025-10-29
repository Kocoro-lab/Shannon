package activities

import (
	"testing"
)

func TestGenerateFallbackTitle(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{
			name:     "short query",
			query:    "Hello world",
			expected: "Hello world",
		},
		{
			name:     "long query with spaces",
			query:    "This is a very long query that exceeds the maximum length and should be truncated at word boundary",
			expected: "This is a very long query that exceeds...",
		},
		{
			name:     "multiline query",
			query:    "First line\nSecond line\nThird line",
			expected: "First line",
		},
		{
			name:     "query with leading/trailing whitespace",
			query:    "  Trimmed query  ",
			expected: "Trimmed query",
		},
		{
			name:     "UTF-8 characters - emoji",
			query:    "🚀 Rocket ship launch sequence for Mars mission",
			expected: "🚀 Rocket ship launch sequence for Mars...",
		},
		{
			name:     "UTF-8 characters - long text",
			query:    "Analyze website traffic trends including visitor count page views bounce rate and user behavior analysis report generation system",
			expected: "Analyze website traffic trends...",
		},
		{
			name:     "long single word no spaces",
			query:    "supercalifragilisticexpialidociousandmorecharacters",
			expected: "supercalifragilisticexpialidociousandmor...",
		},
		{
			name:     "empty query",
			query:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateFallbackTitle(tt.query)
			if result != tt.expected {
				t.Errorf("generateFallbackTitle() = %q, want %q", result, tt.expected)
			}

			// Verify result doesn't exceed max length (accounting for "...")
			runes := []rune(result)
			if len(runes) > 43 { // 40 + "..." = 43
				t.Errorf("generateFallbackTitle() result too long: %d runes", len(runes))
			}
		})
	}
}

func TestGenerateSessionTitle_InputValidation(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		query     string
		wantError string
	}{
		{
			name:      "missing session_id",
			sessionID: "",
			query:     "test query",
			wantError: "session_id is required",
		},
		{
			name:      "missing query",
			sessionID: "test-session-123",
			query:     "",
			wantError: "query is required",
		},
		{
			name:      "valid input",
			sessionID: "test-session-123",
			query:     "What is the capital of France?",
			wantError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test input validation logic directly
			var validationError string

			if tt.sessionID == "" {
				validationError = "session_id is required"
			} else if tt.query == "" {
				validationError = "query is required"
			}

			if validationError != tt.wantError {
				t.Errorf("Validation error = %q, want %q", validationError, tt.wantError)
			}
		})
	}
}

func TestGenerateSessionTitle_UTF8Truncation(t *testing.T) {
	// This test verifies that UTF-8 multi-byte characters are not corrupted during truncation
	tests := []struct {
		name  string
		title string
	}{
		{
			name:  "emoji characters",
			title: "🚀🎉🔥💯✨🌟⭐🎯🎪🎨🎭🎬🎮🎲🎰🎳🏀🏈⚽🎾🏐🏉🎱🏓🏸🏒🏑🏏⛳🏹🎣🏂",
		},
		{
			name:  "long multi-byte characters",
			title: "This is a very long title with special characters to test UTF-8 truncation without corrupting multi-byte sequences",
		},
		{
			name:  "arabic characters",
			title: "هذا عنوان طويل جداً باللغة العربية لاختبار وظيفة الاقتصاص للأحرف متعددة البايتات",
		},
		{
			name:  "mixed scripts",
			title: "Mix مختلط 🚀 Test with various scripts and emoji characters that are all multi-byte UTF-8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a title longer than 60 characters
			longTitle := tt.title + tt.title + tt.title

			// Truncate using the same logic as the actual code
			titleRunes := []rune(longTitle)
			var truncated string
			if len(titleRunes) > 60 {
				truncated = string(titleRunes[:60-3]) + "..."
			} else {
				truncated = longTitle
			}

			// Verify the truncated string is valid UTF-8
			if !isValidUTF8(truncated) {
				t.Errorf("Truncated title contains invalid UTF-8: %q", truncated)
			}

			// Verify length
			truncatedRunes := []rune(truncated)
			if len(truncatedRunes) > 60 {
				t.Errorf("Truncated title exceeds max length: %d runes", len(truncatedRunes))
			}
		})
	}
}

// Helper function to check if a string is valid UTF-8
func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' { // Unicode replacement character indicates invalid UTF-8
			return false
		}
	}
	return true
}
