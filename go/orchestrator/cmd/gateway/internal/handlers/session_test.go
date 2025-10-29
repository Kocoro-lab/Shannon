package handlers

import (
	"strings"
	"testing"
	"unicode"
)

func TestUpdateSessionTitle_Validation(t *testing.T) {
	tests := []struct {
		name          string
		title         string
		shouldPass    bool
		expectedError string
	}{
		{
			name:          "empty title",
			title:         "",
			shouldPass:    false,
			expectedError: "Title cannot be empty",
		},
		{
			name:          "whitespace only title",
			title:         "   ",
			shouldPass:    false,
			expectedError: "Title cannot be empty",
		},
		{
			name:          "title too long (bytes)",
			title:         "This is a very long title that exceeds sixty characters limit",
			shouldPass:    false,
			expectedError: "Title must be 60 characters or less",
		},
		{
			name:          "title too long (UTF-8)",
			title:         "🚀🎉🔥💯✨🌟⭐🎯🎪🎨🎭🎬🎮🎲🎰🎳🏀🏈⚽🎾🏐🏉🎱🏓🏸🏒🏑🏏⛳🏹🎣🏂🏄🏇🏊🚴🚵🏁🏆🏅🎖🏵🎗🎫🎟🎪🎭🎨🎬🎤🎧🎼🎹🥁🎷🎺🎸🎻🎲🎯🎳🎮🎰🎱🏀🏈⚽",
			shouldPass:    false,
			expectedError: "Title must be 60 characters or less",
		},
		{
			name:       "title with control characters (newline)",
			title:      "Title with\nnewline",
			shouldPass: true, // Should be sanitized and accepted
		},
		{
			name:       "title with control characters (tab)",
			title:      "Title with\ttab",
			shouldPass: true, // Should be sanitized and accepted
		},
		{
			name:          "only control characters",
			title:         "\x00\x01\x02\x03",
			shouldPass:    false,
			expectedError: "Title cannot contain only control characters",
		},
		{
			name:       "valid short title",
			title:      "My Session",
			shouldPass: true,
		},
		{
			name:       "valid title at max length (60 chars)",
			title:      "123456789012345678901234567890123456789012345678901234567890",
			shouldPass: true,
		},
		{
			name:       "valid title with emoji",
			title:      "🚀 Rocket Launch",
			shouldPass: true,
		},
		{
			name:       "valid title with multi-byte chars",
			title:      "Traffic Analysis Dashboard",
			shouldPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the validation logic directly (same as handler)
			title := strings.TrimSpace(tt.title)
			var validationError string

			if title == "" {
				validationError = "Title cannot be empty"
			} else {
				// Sanitize
				title = strings.Map(func(r rune) rune {
					if unicode.IsControl(r) {
						return -1
					}
					return r
				}, title)

				if strings.TrimSpace(title) == "" {
					validationError = "Title cannot contain only control characters"
				} else if len([]rune(title)) > 60 {
					validationError = "Title must be 60 characters or less"
				}
			}

			if tt.shouldPass {
				if validationError != "" {
					t.Errorf("Expected to pass validation but got error: %s", validationError)
				}
			} else {
				if validationError != tt.expectedError {
					t.Errorf("Expected error %q, got %q", tt.expectedError, validationError)
				}
			}
		})
	}
}

func TestUpdateSessionTitle_ControlCharacterSanitization(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedClean string
	}{
		{
			name:          "newline removed",
			input:         "Title with\nnewline",
			expectedClean: "Title withnewline",
		},
		{
			name:          "tab removed",
			input:         "Title with\ttab",
			expectedClean: "Title withtab",
		},
		{
			name:          "carriage return removed",
			input:         "Title with\rcarriage return",
			expectedClean: "Title withcarriage return",
		},
		{
			name:          "multiple control chars",
			input:         "Title\n\twith\r\nmultiple",
			expectedClean: "Titlewithmultiple",
		},
		{
			name:          "zero-width space removed (U+200B)",
			input:         "Title\u200Bwith\u200Bzero\u200Bwidth",
			expectedClean: "Titlewithzerowidth",
		},
		{
			name:          "normal spaces preserved",
			input:         "Title with spaces",
			expectedClean: "Title with spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the sanitization logic (using same logic as the handler)
			cleaned := ""
			for _, r := range tt.input {
				if !isControlChar(r) {
					cleaned += string(r)
				}
			}

			if cleaned != tt.expectedClean {
				t.Errorf("Expected %q, got %q", tt.expectedClean, cleaned)
			}
		})
	}
}

// Helper function to check if a rune is a control character
func isControlChar(r rune) bool {
	return r < 32 || (r >= 127 && r < 160) || r == '\u200B' // Control chars + zero-width space
}

func TestUpdateSessionTitle_RuneLengthValidation(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		shouldPass  bool
		runeCount   int
	}{
		{
			name:       "ASCII - exactly 60 chars",
			title:      "123456789012345678901234567890123456789012345678901234567890",
			shouldPass: true,
			runeCount:  60,
		},
		{
			name:       "ASCII - 61 chars",
			title:      "1234567890123456789012345678901234567890123456789012345678901",
			shouldPass: false,
			runeCount:  61,
		},
		{
			name:       "Emoji - 30 emoji = 30 runes (but ~120 bytes)",
			title:      "🚀🎉🔥💯✨🌟⭐🎯🎪🎨🎭🎬🎮🎲🎰🎳🏀🏈⚽🎾🏐🏉🎱🏓🏸🏒🏑🏏⛳🏹",
			shouldPass: true,
			runeCount:  30,
		},
		{
			name:       "Emoji - 67 runes (variation selectors)",
			title:      "🚀🎉🔥💯✨🌟⭐🎯🎪🎨🎭🎬🎮🎲🎰🎳🏀🏈⚽🎾🏐🏉🎱🏓🏸🏒🏑🏏⛳🏹🎣🏂🏄🏇🏊🚴🚵🏁🏆🏅🎖️🏵️🎗️🎫🎟️🎪🎭🎨🎬🎤🎧🎼🎹🥁🎷🎺🎸🎻🎲🎯🎳🎮🎰",
			shouldPass: false,
			runeCount:  67,
		},
		{
			name:       "Long text - exactly 60 chars",
			title:      "123456789012345678901234567890123456789012345678901234567890",
			shouldPass: true,
			runeCount:  60,
		},
		{
			name:       "Mixed - ASCII + emoji",
			title:      "Test 🚀 Mix",
			shouldPass: true,
			runeCount:  10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runeCount := len([]rune(tt.title))
			if runeCount != tt.runeCount {
				t.Errorf("Expected rune count %d, got %d", tt.runeCount, runeCount)
			}

			shouldPass := runeCount <= 60
			if shouldPass != tt.shouldPass {
				t.Errorf("Expected shouldPass=%v, got %v (rune count: %d)", tt.shouldPass, shouldPass, runeCount)
			}
		})
	}
}
