package activities

import (
	"testing"
)

func TestTruncateQuery_UTF8(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxLen    int
		wantLen   int  // Expected rune count (not byte count)
		wantValid bool // Should be valid UTF-8
	}{
		{
			name:      "Chinese characters",
			input:     "查询中文数据库中的用户信息",
			maxLen:    10,
			wantLen:   10, // 7 runes + "..."
			wantValid: true,
		},
		{
			name:      "Mixed English and Chinese",
			input:     "Query for 用户信息 in database",
			maxLen:    15,
			wantLen:   15,
			wantValid: true,
		},
		{
			name:      "English only - no truncation needed",
			input:     "Short query",
			maxLen:    20,
			wantLen:   11,
			wantValid: true,
		},
		{
			name:      "English with truncation",
			input:     "This is a very long query that needs to be truncated",
			maxLen:    20,
			wantLen:   20,
			wantValid: true,
		},
		{
			name:      "Japanese characters",
			input:     "データベースからユーザー情報を取得する",
			maxLen:    15,
			wantLen:   15,
			wantValid: true,
		},
		{
			name:      "Emoji test",
			input:     "Hello 👋 World 🌍 with emojis 🎉",
			maxLen:    20,
			wantLen:   20,
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateQuery(tt.input, tt.maxLen)

			// Check valid UTF-8
			if tt.wantValid {
				runes := []rune(result)
				if string(runes) != result {
					t.Errorf("truncateQuery() produced invalid UTF-8: %v", result)
				}
			}

			// Check length
			resultRunes := []rune(result)
			if len(resultRunes) > tt.maxLen {
				t.Errorf("truncateQuery() length = %v, want <= %v", len(resultRunes), tt.maxLen)
			}

			// Verify truncation happened correctly
			inputRunes := []rune(tt.input)
			if len(inputRunes) > tt.maxLen {
				// Should have ellipsis
				if len(result) < 3 || result[len(result)-3:] != "..." {
					t.Errorf("truncateQuery() should end with '...' for truncated string")
				}
			}

			t.Logf("Input: %s (%d runes)", tt.input, len(inputRunes))
			t.Logf("Output: %s (%d runes)", result, len(resultRunes))
		})
	}
}

func TestTruncateQuery_EdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
	}{
		{
			name:   "Empty string",
			input:  "",
			maxLen: 10,
		},
		{
			name:   "Very short maxLen",
			input:  "中文测试",
			maxLen: 5,
		},
		{
			name:   "Exact maxLen boundary",
			input:  "1234567890",
			maxLen: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateQuery(tt.input, tt.maxLen)

			// Should never panic or produce invalid UTF-8
			runes := []rune(result)
			if string(runes) != result {
				t.Errorf("truncateQuery() produced invalid UTF-8")
			}

			// Length check
			if len(runes) > tt.maxLen {
				t.Errorf("truncateQuery() length = %v, want <= %v", len(runes), tt.maxLen)
			}
		})
	}
}

// TestTruncateQuery_NoByteSlicing verifies we never cut a multi-byte character
func TestTruncateQuery_NoByteSlicing(t *testing.T) {
	// Chinese characters are 3 bytes each in UTF-8
	input := "查" // 0xE6 0x9F 0xA5

	// Try various maxLen values
	for maxLen := 1; maxLen < 10; maxLen++ {
		result := truncateQuery(input, maxLen)

		// Convert to runes and back - if this differs from result, we have invalid UTF-8
		runes := []rune(result)
		reconstructed := string(runes)

		if reconstructed != result {
			t.Errorf("truncateQuery(maxLen=%d) produced invalid UTF-8: got %q, rune roundtrip: %q",
				maxLen, result, reconstructed)
		}
	}
}
