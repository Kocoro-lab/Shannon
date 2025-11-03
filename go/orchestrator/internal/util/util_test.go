package util

import (
	"testing"
)

func TestContainsString(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		item     string
		expected bool
	}{
		{
			name:     "item exists in slice",
			slice:    []string{"apple", "banana", "orange"},
			item:     "banana",
			expected: true,
		},
		{
			name:     "item does not exist in slice",
			slice:    []string{"apple", "banana", "orange"},
			item:     "grape",
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			item:     "apple",
			expected: false,
		},
		{
			name:     "empty item in slice",
			slice:    []string{"", "apple"},
			item:     "",
			expected: true,
		},
		{
			name:     "case sensitive match",
			slice:    []string{"Apple", "Banana"},
			item:     "apple",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ContainsString(tt.slice, tt.item)
			if result != tt.expected {
				t.Errorf("ContainsString(%v, %q) = %v, want %v", tt.slice, tt.item, result, tt.expected)
			}
		})
	}
}

func TestParseNumericValue(t *testing.T) {
	tests := []struct {
		name        string
		response    string
		expectedVal float64
		expectedOk  bool
	}{
		{
			name:        "direct numeric parse",
			response:    "42",
			expectedVal: 42.0,
			expectedOk:  true,
		},
		{
			name:        "direct float parse",
			response:    "3.14",
			expectedVal: 3.14,
			expectedOk:  true,
		},
		{
			name:        "equals pattern",
			response:    "The answer equals 42",
			expectedVal: 42.0,
			expectedOk:  true,
		},
		{
			name:        "is pattern",
			response:    "The result is 104",
			expectedVal: 104.0,
			expectedOk:  true,
		},
		{
			name:        "last numeric token",
			response:    "I calculated 15 plus 27 and got 42",
			expectedVal: 42.0,
			expectedOk:  true,
		},
		{
			name:        "with punctuation",
			response:    "The answer is: 42.",
			expectedVal: 42.0,
			expectedOk:  true,
		},
		{
			name:        "no numbers",
			response:    "There are no numbers here",
			expectedVal: 0.0,
			expectedOk:  false,
		},
		{
			name:        "empty string",
			response:    "",
			expectedVal: 0.0,
			expectedOk:  false,
		},
		{
			name:        "whitespace only",
			response:    "   ",
			expectedVal: 0.0,
			expectedOk:  false,
		},
		{
			name:        "negative number",
			response:    "The temperature is -5 degrees",
			expectedVal: -5.0,
			expectedOk:  true,
		},
		{
			name:        "equals with punctuation",
			response:    "The sum equals 30!",
			expectedVal: 30.0,
			expectedOk:  true,
		},
		{
			name:        "number without punctuation",
			response:    "The total is 1234",
			expectedVal: 1234.0,
			expectedOk:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := ParseNumericValue(tt.response)
			if ok != tt.expectedOk {
				t.Errorf("ParseNumericValue(%q) ok = %v, want %v", tt.response, ok, tt.expectedOk)
			}
			if ok && val != tt.expectedVal {
				t.Errorf("ParseNumericValue(%q) val = %v, want %v", tt.response, val, tt.expectedVal)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		maxLen        int
		preserveWords bool
		expected      string
	}{
		{
			name:          "no truncation needed",
			input:         "short text",
			maxLen:        20,
			preserveWords: false,
			expected:      "short text",
		},
		{
			name:          "simple truncation",
			input:         "This is a long text that needs truncation",
			maxLen:        20,
			preserveWords: false,
			expected:      "This is a long te...",
		},
		{
			name:          "word-preserving truncation",
			input:         "This is a long text that needs truncation",
			maxLen:        20,
			preserveWords: true,
			expected:      "This is a long...",
		},
		{
			name:          "maxLen zero",
			input:         "any text",
			maxLen:        0,
			preserveWords: false,
			expected:      "",
		},
		{
			name:          "maxLen smaller than ellipsis",
			input:         "text",
			maxLen:        2,
			preserveWords: false,
			expected:      "..",
		},
		{
			name:          "exact length match",
			input:         "exact",
			maxLen:        5,
			preserveWords: false,
			expected:      "exact",
		},
		{
			name:          "preserve words but no space found",
			input:         "verylongtextwithoutspaces",
			maxLen:        15,
			preserveWords: true,
			expected:      "verylongtext...",
		},
		{
			name:          "truncate with newline",
			input:         "First line\nSecond line that is very long",
			maxLen:        20,
			preserveWords: true,
			expected:      "First line...",
		},
		{
			name:          "empty string",
			input:         "",
			maxLen:        10,
			preserveWords: false,
			expected:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateString(tt.input, tt.maxLen, tt.preserveWords)
			if result != tt.expected {
				t.Errorf("TruncateString(%q, %d, %v) = %q, want %q", tt.input, tt.maxLen, tt.preserveWords, result, tt.expected)
			}
		})
	}
}
