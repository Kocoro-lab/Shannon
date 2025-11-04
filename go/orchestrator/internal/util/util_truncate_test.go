package util

import (
	"testing"
)

func TestTruncateString_UTF8(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		maxLen        int
		preserveWords bool
		wantMaxRunes  int
	}{
		{
			name:          "Chinese characters - no word preserve",
			input:         "查询中文数据库中的用户信息",
			maxLen:        10,
			preserveWords: false,
			wantMaxRunes:  10,
		},
		{
			name:          "Chinese characters with spaces - word preserve",
			input:         "查询 中文 数据库 中的 用户信息",
			maxLen:        15,
			preserveWords: true,
			wantMaxRunes:  15,
		},
		{
			name:          "English with word boundaries",
			input:         "This is a very long string that needs truncation",
			maxLen:        20,
			preserveWords: true,
			wantMaxRunes:  20,
		},
		{
			name:          "Mixed language",
			input:         "Query for 用户信息 in the database system",
			maxLen:        25,
			preserveWords: true,
			wantMaxRunes:  25,
		},
		{
			name:          "Japanese text",
			input:         "データベース システム から ユーザー 情報",
			maxLen:        20,
			preserveWords: true,
			wantMaxRunes:  20,
		},
		{
			name:          "Emoji and special chars",
			input:         "Hello 👋 World 🌍 Testing 🎉 Emoji",
			maxLen:        15,
			preserveWords: false,
			wantMaxRunes:  15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateString(tt.input, tt.maxLen, tt.preserveWords)

			// Verify valid UTF-8
			runes := []rune(result)
			if string(runes) != result {
				t.Errorf("TruncateString() produced invalid UTF-8: %v", result)
			}

			// Check length constraint
			if len(runes) > tt.wantMaxRunes {
				t.Errorf("TruncateString() length = %d runes, want <= %d", len(runes), tt.wantMaxRunes)
			}

			// If truncation occurred, should have ellipsis
			inputRunes := []rune(tt.input)
			if len(inputRunes) > tt.maxLen {
				if len(result) < 3 || result[len(result)-3:] != "..." {
					t.Errorf("TruncateString() should end with '...' when truncated")
				}
			}

			t.Logf("Input: %s (%d runes)", tt.input, len(inputRunes))
			t.Logf("Output: %s (%d runes)", result, len(runes))
		})
	}
}

func TestTruncateString_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		maxLen        int
		preserveWords bool
	}{
		{
			name:          "Empty string",
			input:         "",
			maxLen:        10,
			preserveWords: false,
		},
		{
			name:          "maxLen is 0",
			input:         "test",
			maxLen:        0,
			preserveWords: false,
		},
		{
			name:          "maxLen is negative",
			input:         "test",
			maxLen:        -1,
			preserveWords: false,
		},
		{
			name:          "maxLen equals input length",
			input:         "1234567890",
			maxLen:        10,
			preserveWords: false,
		},
		{
			name:          "Single Chinese character",
			input:         "查",
			maxLen:        5,
			preserveWords: false,
		},
		{
			name:          "Very short maxLen",
			input:         "中文测试字符串",
			maxLen:        3,
			preserveWords: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateString(tt.input, tt.maxLen, tt.preserveWords)

			// Should never panic
			runes := []rune(result)
			
			// Check valid UTF-8
			if string(runes) != result {
				t.Errorf("TruncateString() produced invalid UTF-8")
			}

			// Length should respect maxLen (unless maxLen is 0 or negative)
			if tt.maxLen > 0 && len(runes) > tt.maxLen {
				t.Errorf("TruncateString() length = %d, want <= %d", len(runes), tt.maxLen)
			}
		})
	}
}

func TestTruncateString_WordPreservation(t *testing.T) {
	input := "这是 一个 测试 字符串 包含 中文 空格"
	maxLen := 15

	withWords := TruncateString(input, maxLen, true)
	withoutWords := TruncateString(input, maxLen, false)

	t.Logf("Input: %s", input)
	t.Logf("With word preserve: %s", withWords)
	t.Logf("Without word preserve: %s", withoutWords)

	// Both should be valid UTF-8
	if string([]rune(withWords)) != withWords {
		t.Error("Word-preserved result is invalid UTF-8")
	}
	if string([]rune(withoutWords)) != withoutWords {
		t.Error("Non-word-preserved result is invalid UTF-8")
	}

	// Both should respect maxLen
	if len([]rune(withWords)) > maxLen {
		t.Errorf("Word-preserved length exceeded: %d > %d", len([]rune(withWords)), maxLen)
	}
	if len([]rune(withoutWords)) > maxLen {
		t.Errorf("Non-word-preserved length exceeded: %d > %d", len([]rune(withoutWords)), maxLen)
	}
}

// TestTruncateString_NoByteCutting verifies we never cut UTF-8 multi-byte sequences
func TestTruncateString_NoByteCutting(t *testing.T) {
	// Test with various Unicode characters
	inputs := []string{
		"查询数据",           // Chinese (3 bytes per char)
		"データベース",          // Japanese (3 bytes per char)
		"Hello 👋 World",    // Emoji (4 bytes)
		"Привет мир",        // Cyrillic (2 bytes per char)
		"مرحبا بالعالم",      // Arabic (varies)
	}

	for _, input := range inputs {
		for maxLen := 1; maxLen < len(input)+5; maxLen++ {
			result := TruncateString(input, maxLen, false)

			// Verify UTF-8 validity by rune roundtrip
			runes := []rune(result)
			reconstructed := string(runes)

			if reconstructed != result {
				t.Errorf("TruncateString(%q, %d) produced invalid UTF-8\nGot: %q\nRoundtrip: %q",
					input, maxLen, result, reconstructed)
			}
		}
	}
}

