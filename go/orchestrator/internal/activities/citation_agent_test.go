package activities

import (
	"strings"
	"testing"
)

// ============================================================================
// Levenshtein Distance Tests
// ============================================================================

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		name     string
		s1       string
		s2       string
		expected int
	}{
		// Basic cases
		{"identical strings", "hello", "hello", 0},
		{"empty strings", "", "", 0},
		{"one empty", "hello", "", 5},
		{"other empty", "", "world", 5},

		// Single operations
		{"single insertion", "hello", "helllo", 1},
		{"single deletion", "hello", "helo", 1},
		{"single substitution", "hello", "hallo", 1},

		// Citation marker insertion (the main use case)
		{"add [1] marker", "Revenue grew 19%.", "Revenue grew 19%.[1]", 3},
		{"add [12] marker", "Revenue grew 19%.", "Revenue grew 19%.[12]", 4},
		{"add marker with space", "Revenue grew 19%.", "Revenue grew 19%. [1]", 4},

		// Multiple changes
		{"multiple insertions", "abc", "aXbYcZ", 3},
		{"kitten to sitting", "kitten", "sitting", 3},
		{"saturday to sunday", "saturday", "sunday", 3},

		// Unicode handling
		{"chinese same", "你好世界", "你好世界", 0},
		{"chinese diff", "你好世界", "你好中国", 2},
		{"mixed unicode", "hello世界", "hello世界[1]", 3},

		// Longer strings
		{"paragraph same", "The quick brown fox jumps over the lazy dog.",
			"The quick brown fox jumps over the lazy dog.", 0},
		{"paragraph with citation", "The quick brown fox jumps over the lazy dog.",
			"The quick brown fox jumps over the lazy dog.[1]", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s1 := []rune(tt.s1)
			s2 := []rune(tt.s2)
			result := levenshteinDistance(s1, s2)
			if result != tt.expected {
				t.Errorf("levenshteinDistance(%q, %q) = %d, want %d",
					tt.s1, tt.s2, result, tt.expected)
			}

			// Test symmetry: distance(a, b) == distance(b, a)
			resultReverse := levenshteinDistance(s2, s1)
			if result != resultReverse {
				t.Errorf("levenshteinDistance is not symmetric: (%q,%q)=%d, (%q,%q)=%d",
					tt.s1, tt.s2, result, tt.s2, tt.s1, resultReverse)
			}
		})
	}
}

func TestLevenshteinDistance_EdgeCases(t *testing.T) {
	// Test with very different strings
	s1 := []rune("abcdefghij")
	s2 := []rune("klmnopqrst")
	result := levenshteinDistance(s1, s2)
	if result != 10 {
		t.Errorf("completely different strings: got %d, want 10", result)
	}

	// Test space optimization (s1 should be shorter)
	long := []rune("this is a longer string")
	short := []rune("short")
	result1 := levenshteinDistance(long, short)
	result2 := levenshteinDistance(short, long)
	if result1 != result2 {
		t.Errorf("space optimization broke symmetry: %d != %d", result1, result2)
	}
}

// ============================================================================
// Find First Difference Tests
// ============================================================================

func TestFindFirstDifference(t *testing.T) {
	tests := []struct {
		name     string
		s1       string
		s2       string
		expected int
	}{
		{"identical", "hello", "hello", 5},
		{"diff at start", "hello", "jello", 0},
		{"diff at end", "hello", "hellp", 4},
		{"diff in middle", "hello", "hallo", 1},
		{"one longer", "hello", "hello world", 5},
		{"one shorter", "hello world", "hello", 5},
		{"both empty", "", "", 0},
		{"one empty", "hello", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s1 := []rune(tt.s1)
			s2 := []rune(tt.s2)
			result := findFirstDifference(s1, s2)
			if result != tt.expected {
				t.Errorf("findFirstDifference(%q, %q) = %d, want %d",
					tt.s1, tt.s2, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// Sampled Edit Distance Tests
// ============================================================================

func TestSampledEditDistanceRatio(t *testing.T) {
	tests := []struct {
		name     string
		s1       string
		s2       string
		maxRatio float64 // expected ratio should be <= this
	}{
		{"identical", "hello world", "hello world", 0.01},
		{"one empty", "hello", "", 1.0},
		{"small diff", "hello world", "hello world!", 0.1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s1 := []rune(tt.s1)
			s2 := []rune(tt.s2)
			result := sampledEditDistanceRatio(s1, s2)
			if result > tt.maxRatio {
				t.Errorf("sampledEditDistanceRatio(%q, %q) = %.4f, want <= %.4f",
					tt.s1, tt.s2, result, tt.maxRatio)
			}
		})
	}

	// Test with longer strings (the actual use case)
	t.Run("long identical strings", func(t *testing.T) {
		long := make([]rune, 15000)
		for i := range long {
			long[i] = rune('a' + (i % 26))
		}
		result := sampledEditDistanceRatio(long, long)
		if result > 0.01 {
			t.Errorf("identical long strings should have ratio ~0, got %.4f", result)
		}
	})
}

// ============================================================================
// Normalize For Comparison Tests
// ============================================================================

func TestNormalizeForComparison(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Whitespace normalization
		{"trim spaces", "  hello  ", "hello"},
		{"collapse spaces", "hello   world", "hello world"},
		{"normalize newlines", "hello\r\nworld", "hello\nworld"},
		{"collapse newlines", "hello\n\n\n\nworld", "hello\n\nworld"},

		// Invisible character removal
		{"remove ZWS", "hello\u200Bworld", "helloworld"},
		{"remove BOM", "\uFEFFhello", "hello"},

		// Punctuation normalization
		{"fullwidth colon", "key：value", "key:value"},
		{"chinese comma", "a，b", "a,b"},
		{"chinese period", "end。", "end."},
		{"smart quotes", "\u201chello\u201d", `"hello"`},

		// Fullwidth characters
		{"fullwidth digits", "１２３", "123"},
		{"fullwidth letters", "ＡＢＣ", "ABC"},
		{"fullwidth lowercase", "ａｂｃ", "abc"},

		// Hyphen normalization
		{"en dash", "2020–2021", "2020-2021"},
		{"em dash", "hello—world", "hello-world"},

		// Ellipsis
		{"unicode ellipsis", "hello…world", "hello...world"},

		// Combined normalization
		{"complex normalization", "  Ｈｅｌｌｏ　世界！  ", "Hello 世界!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeForComparison(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeForComparison(%q) = %q, want %q",
					tt.input, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// Content Immutability Validation Tests
// ============================================================================

func TestValidateContentImmutability(t *testing.T) {
	tests := []struct {
		name          string
		original      string
		cited         string
		expectValid   bool
		expectContain string // substring expected in error message
	}{
		// Valid cases - only citation markers added
		{
			name:        "identical (no citations)",
			original:    "Hello world.",
			cited:       "Hello world.",
			expectValid: true,
		},
		{
			name:        "single citation added",
			original:    "Revenue grew 19%.",
			cited:       "Revenue grew 19%.[1]",
			expectValid: true,
		},
		{
			name:        "citation with space",
			original:    "Revenue grew 19%.",
			cited:       "Revenue grew 19%. [1]",
			expectValid: true,
		},
		{
			name:        "multiple citations",
			original:    "Revenue grew 19%. Profit increased 25%.",
			cited:       "Revenue grew 19%.[1] Profit increased 25%.[2]",
			expectValid: true,
		},
		{
			name:        "citation in middle of text",
			original:    "The company was founded in 2020 and has expanded globally.",
			cited:       "The company was founded in 2020[1] and has expanded globally.",
			expectValid: true,
		},
		{
			name:        "multiple citations same sentence",
			original:    "Revenue grew 19% in Q4.",
			cited:       "Revenue grew 19%[1][2] in Q4.[3]",
			expectValid: true,
		},

		// Invalid cases - content modified
		{
			name:          "word changed",
			original:      "Revenue grew 19%.",
			cited:         "Revenue increased 19%.[1]",
			expectValid:   false,
			expectContain: "content modified",
		},
		{
			name:          "significant addition",
			original:      "Hello.",
			cited:         "Hello. This is new content.[1]",
			expectValid:   false,
			expectContain: "content modified",
		},
		{
			name:          "content removed",
			original:      "This is a long sentence with important information.",
			cited:         "This is a sentence.[1]",
			expectValid:   false,
			expectContain: "content modified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := validateContentImmutability(tt.original, tt.cited)

			if valid != tt.expectValid {
				t.Errorf("validateContentImmutability() valid = %v, want %v (err: %v)",
					valid, tt.expectValid, err)
			}

			if !tt.expectValid && tt.expectContain != "" {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.expectContain)
				} else if !contains(err.Error(), tt.expectContain) {
					t.Errorf("error %q should contain %q", err.Error(), tt.expectContain)
				}
			}
		})
	}
}

func TestValidateContentImmutability_MinorChangesTolerated(t *testing.T) {
	// LLMs sometimes make minor whitespace changes that should be tolerated
	tests := []struct {
		name     string
		original string
		cited    string
	}{
		{
			name:     "extra trailing space",
			original: "Hello world.",
			cited:    "Hello world. [1]",
		},
		{
			name:     "different quote style",
			original: `He said "hello".`,
			cited:    `He said "hello".[1]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := validateContentImmutability(tt.original, tt.cited)
			if !valid {
				t.Errorf("minor change should be tolerated, got error: %v", err)
			}
		})
	}
}

// ============================================================================
// Citation Validation Tests
// ============================================================================

func TestValidateCitationNumbers(t *testing.T) {
	tests := []struct {
		name         string
		cited        string
		maxCitations int
		expectInv    []int
	}{
		{
			name:         "all valid",
			cited:        "Text[1] more[2] end[3].",
			maxCitations: 5,
			expectInv:    nil,
		},
		{
			name:         "zero invalid",
			cited:        "Text[0] more[1].",
			maxCitations: 5,
			expectInv:    []int{0},
		},
		{
			name:         "over max",
			cited:        "Text[1] more[10].",
			maxCitations: 5,
			expectInv:    []int{10},
		},
		{
			name:         "multiple invalid",
			cited:        "Text[0] more[100] end[1].",
			maxCitations: 5,
			expectInv:    []int{0, 100},
		},
		{
			name:         "no citations",
			cited:        "Just plain text.",
			maxCitations: 5,
			expectInv:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateCitationNumbers(tt.cited, tt.maxCitations)
			if !intSlicesEqual(result, tt.expectInv) {
				t.Errorf("validateCitationNumbers() = %v, want %v", result, tt.expectInv)
			}
		})
	}
}

func TestExtractUsedCitationNumbers(t *testing.T) {
	tests := []struct {
		name   string
		cited  string
		expect []int
	}{
		{
			name:   "sequential",
			cited:  "Text[1] more[2] end[3].",
			expect: []int{1, 2, 3},
		},
		{
			name:   "non-sequential",
			cited:  "Text[3] more[1] end[5].",
			expect: []int{3, 1, 5},
		},
		{
			name:   "duplicates removed",
			cited:  "Text[1] more[1] end[2].",
			expect: []int{1, 2},
		},
		{
			name:   "no citations",
			cited:  "Just plain text.",
			expect: nil,
		},
		{
			name:   "multi-digit",
			cited:  "Text[10] more[99] end[1].",
			expect: []int{10, 99, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractUsedCitationNumbers(tt.cited)
			if !intSlicesEqual(result, tt.expect) {
				t.Errorf("extractUsedCitationNumbers() = %v, want %v", result, tt.expect)
			}
		})
	}
}

func TestRemoveInvalidCitations(t *testing.T) {
	tests := []struct {
		name    string
		cited   string
		invalid []int
		expect  string
	}{
		{
			name:    "remove single",
			cited:   "Text[1] more[99].",
			invalid: []int{99},
			expect:  "Text[1] more.",
		},
		{
			name:    "remove multiple",
			cited:   "Text[0] more[1] end[100].",
			invalid: []int{0, 100},
			expect:  "Text more[1] end.",
		},
		{
			name:    "remove with space",
			cited:   "Text [99] more.",
			invalid: []int{99},
			expect:  "Text more.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeInvalidCitations(tt.cited, tt.invalid)
			if result != tt.expect {
				t.Errorf("removeInvalidCitations() = %q, want %q", result, tt.expect)
			}
		})
	}
}

// ============================================================================
// Citation Placement Tests
// ============================================================================

func TestValidateCitationPlacement(t *testing.T) {
	tests := []struct {
		name        string
		cited       string
		expectWarns int
	}{
		{
			name:        "valid placement",
			cited:       "Revenue grew 19%.[1] Profit increased.[2]",
			expectWarns: 0,
		},
		{
			name:        "citation at very start",
			cited:       "[1]Revenue grew 19%.",
			expectWarns: 1,
		},
		{
			name:        "citation inside word",
			cited:       "Reve[1]nue grew 19%.",
			expectWarns: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := validateCitationPlacement(tt.cited)
			if len(warnings) != tt.expectWarns {
				t.Errorf("validateCitationPlacement() returned %d warnings, want %d: %v",
					len(warnings), tt.expectWarns, warnings)
			}
		})
	}
}

func TestDetectRedundantCitations(t *testing.T) {
	tests := []struct {
		name        string
		cited       string
		expectCount int
	}{
		{
			name:        "no redundancy",
			cited:       "Revenue grew[1]. Profit increased[2].",
			expectCount: 0,
		},
		{
			name:        "same citation twice in sentence",
			cited:       "Revenue[1] grew[1]. Profit increased.",
			expectCount: 1,
		},
		{
			name:        "different citations same sentence",
			cited:       "Revenue[1] grew[2]. Profit increased.",
			expectCount: 0,
		},
		{
			name:        "multiple redundancies",
			cited:       "Revenue[1] grew[1] fast[1]. Profit[2] increased[2].",
			expectCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redundant := detectRedundantCitations(tt.cited)
			if len(redundant) != tt.expectCount {
				t.Errorf("detectRedundantCitations() found %d redundancies, want %d: %v",
					len(redundant), tt.expectCount, redundant)
			}
		})
	}
}

// ============================================================================
// Extract Cited Report Tests
// ============================================================================

func TestExtractCitedReport(t *testing.T) {
	tests := []struct {
		name     string
		response string
		expect   string
	}{
		{
			name:     "simple extraction",
			response: "<cited_report>Hello world.[1]</cited_report>",
			expect:   "Hello world.[1]",
		},
		{
			name:     "with surrounding text",
			response: "Here is the report:\n<cited_report>Hello.[1]</cited_report>\nDone.",
			expect:   "Hello.[1]",
		},
		{
			name:     "multiline content",
			response: "<cited_report>\nLine 1.[1]\nLine 2.[2]\n</cited_report>",
			expect:   "Line 1.[1]\nLine 2.[2]",
		},
		{
			name:     "no tags",
			response: "Just plain text without tags",
			expect:   "",
		},
		{
			name:     "only start tag",
			response: "<cited_report>Hello",
			expect:   "",
		},
		{
			name:     "only end tag",
			response: "Hello</cited_report>",
			expect:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCitedReport(tt.response)
			if result != tt.expect {
				t.Errorf("extractCitedReport() = %q, want %q", result, tt.expect)
			}
		})
	}
}

// ============================================================================
// Helper Functions
// ============================================================================

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr) >= 0))
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func intSlicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

// ============================================================================
// Benchmark Tests
// ============================================================================

func BenchmarkLevenshteinDistance_Short(b *testing.B) {
	s1 := []rune("hello world")
	s2 := []rune("hello world[1]")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		levenshteinDistance(s1, s2)
	}
}

func BenchmarkLevenshteinDistance_Medium(b *testing.B) {
	// Simulate a typical paragraph
	s1 := []rune("The quick brown fox jumps over the lazy dog. This is a test sentence with some additional content for benchmarking purposes.")
	s2 := []rune("The quick brown fox jumps over the lazy dog.[1] This is a test sentence with some additional content for benchmarking purposes.[2]")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		levenshteinDistance(s1, s2)
	}
}

func BenchmarkLevenshteinDistance_Long(b *testing.B) {
	// 5000 characters
	base := make([]rune, 5000)
	for i := range base {
		base[i] = rune('a' + (i % 26))
	}
	s1 := base
	s2 := append([]rune{}, base...)
	s2 = append(s2, []rune("[1]")...)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		levenshteinDistance(s1, s2)
	}
}

func BenchmarkSampledEditDistanceRatio(b *testing.B) {
	// 15000 characters (triggers sampling)
	base := make([]rune, 15000)
	for i := range base {
		base[i] = rune('a' + (i % 26))
	}
	s1 := base
	s2 := append([]rune{}, base...)
	s2 = append(s2, []rune("[1]")...)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sampledEditDistanceRatio(s1, s2)
	}
}

func BenchmarkNormalizeForComparison(b *testing.B) {
	input := "  The quick　brown fox\u200B jumps over the lazy dog。This is a test，with various　unicode characters！  "
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		normalizeForComparison(input)
	}
}

// ============================================================================
// SplitSentencesV2 Tests - Decimal Point Detection
// ============================================================================

func TestSplitSentencesV2_DecimalPoints(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedCount  int
		expectedFirst  string // First sentence (trimmed)
		expectedSecond string // Second sentence if exists (trimmed)
	}{
		{
			name:          "decimal in dollar amount - single sentence",
			input:         "The market grew to $30.8 billion in 2024.",
			expectedCount: 1,
			expectedFirst: "The market grew to $30.8 billion in 2024.",
		},
		{
			name:           "decimal in dollar amount - two sentences",
			input:          "Revenue was $30.8 billion. NVIDIA leads the market.",
			expectedCount:  2,
			expectedFirst:  "Revenue was $30.8 billion. ",
			expectedSecond: "NVIDIA leads the market.",
		},
		{
			name:          "percentage with decimal",
			input:         "Intel's share dropped 3.5% last quarter.",
			expectedCount: 1,
			expectedFirst: "Intel's share dropped 3.5% last quarter.",
		},
		{
			name:          "multiple decimals in one sentence",
			input:         "Revenue was $30.8B with 3.5% growth and 2.1x multiple.",
			expectedCount: 1,
			expectedFirst: "Revenue was $30.8B with 3.5% growth and 2.1x multiple.",
		},
		{
			name:          "Chinese with decimal percentage",
			input:         "Tokyo Electron占全球市场份额的10.2%。",
			expectedCount: 1,
			expectedFirst: "Tokyo Electron占全球市场份额的10.2%。",
		},
		{
			name:           "mixed language with decimals",
			input:          "市场规模达到$30.8B。增长率为15.3%。",
			expectedCount:  2,
			expectedFirst:  "市场规模达到$30.8B。",
			expectedSecond: "增长率为15.3%。",
		},
		{
			name:          "version number",
			input:         "Using Python v3.11.2 for this project.",
			expectedCount: 1,
			expectedFirst: "Using Python v3.11.2 for this project.",
		},
		{
			name:          "IP address style numbers",
			input:         "The ratio is 1.2.3 formatted.",
			expectedCount: 1,
			expectedFirst: "The ratio is 1.2.3 formatted.",
		},
		{
			name:           "sentence ending with number then period",
			input:          "The price is $30. That's expensive.",
			expectedCount:  2,
			expectedFirst:  "The price is $30. ",
			expectedSecond: "That's expensive.",
		},
		{
			name:          "decimal at end of sentence",
			input:         "The growth rate was 3.5.",
			expectedCount: 1,
			expectedFirst: "The growth rate was 3.5.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitSentencesV2(tt.input)

			if len(result) != tt.expectedCount {
				t.Errorf("splitSentencesV2(%q) returned %d sentences, want %d.\nGot: %v",
					tt.input, len(result), tt.expectedCount, result)
				return
			}

			if len(result) > 0 && result[0] != tt.expectedFirst {
				t.Errorf("splitSentencesV2(%q) first sentence = %q, want %q",
					tt.input, result[0], tt.expectedFirst)
			}

			if len(result) > 1 && tt.expectedSecond != "" && result[1] != tt.expectedSecond {
				t.Errorf("splitSentencesV2(%q) second sentence = %q, want %q",
					tt.input, result[1], tt.expectedSecond)
			}
		})
	}
}

func TestSplitSentencesV2_PreservesMarkdown(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedCount int
	}{
		{
			name:          "preserves newline as sentence boundary",
			input:         "First line.\nSecond line.",
			expectedCount: 2,
		},
		{
			name:          "preserves blank lines",
			input:         "Paragraph one.\n\nParagraph two.",
			expectedCount: 3, // sentence, blank, sentence
		},
		{
			name:          "header followed by content",
			input:         "## Market Analysis\nThe market grew.",
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitSentencesV2(tt.input)
			if len(result) != tt.expectedCount {
				t.Errorf("splitSentencesV2(%q) returned %d sentences, want %d.\nGot: %v",
					tt.input, len(result), tt.expectedCount, result)
			}
		})
	}
}

func TestAppendCitationsToSentence(t *testing.T) {
	tests := []struct {
		name        string
		sentence    string
		citationIDs []int
		expected    string
	}{
		{
			name:        "single citation",
			sentence:    "Revenue grew 19%.",
			citationIDs: []int{1},
			expected:    "Revenue grew 19%.[1]",
		},
		{
			name:        "multiple citations",
			sentence:    "Revenue grew 19%.",
			citationIDs: []int{1, 3, 5},
			expected:    "Revenue grew 19%.[1][3][5]",
		},
		{
			name:        "sentence with trailing space",
			sentence:    "Revenue grew 19%. ",
			citationIDs: []int{1},
			expected:    "Revenue grew 19%.[1] ",
		},
		{
			name:        "sentence with trailing newline",
			sentence:    "Revenue grew 19%.\n",
			citationIDs: []int{1},
			expected:    "Revenue grew 19%.[1]\n",
		},
		{
			name:        "empty citation list",
			sentence:    "Revenue grew 19%.",
			citationIDs: []int{},
			expected:    "Revenue grew 19%.",
		},
		{
			name:        "Chinese sentence",
			sentence:    "收入增长了19%。",
			citationIDs: []int{2},
			expected:    "收入增长了19%。[2]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := appendCitationsToSentence(tt.sentence, tt.citationIDs)
			if result != tt.expected {
				t.Errorf("appendCitationsToSentence(%q, %v) = %q, want %q",
					tt.sentence, tt.citationIDs, result, tt.expected)
			}
		})
	}
}

func TestBuildCitationAgentPrompt_PrecisionFirst(t *testing.T) {
	prompt := buildCitationAgentPrompt()

	// Ensure we don't encourage "density targets" or URL/title-only citation behavior.
	for _, banned := range []string{
		"30-50%",
		"Cite if URL/Title Confirms",
		"reasonable inference",
		"contact page likely has location",
	} {
		if strings.Contains(prompt, banned) {
			t.Fatalf("prompt contains banned guidance %q", banned)
		}
	}

	// Ensure strict, evidence-first guidance is present.
	for _, required := range []string{
		"SUFFICIENT EVIDENCE REQUIRED",
		"Content",
		"When in doubt",
		"DO NOT MODIFY TEXT",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt missing required guidance %q", required)
		}
	}
}

func BenchmarkValidateContentImmutability(b *testing.B) {
	original := `The quick brown fox jumps over the lazy dog. This sentence contains multiple facts that need citations.
Revenue grew 19% year over year. The company was founded in 2020 and has since expanded to 50 countries.
Profit margins improved significantly due to operational efficiency. The CEO announced plans for further expansion.`

	cited := `The quick brown fox jumps over the lazy dog.[1] This sentence contains multiple facts that need citations.
Revenue grew 19% year over year.[2] The company was founded in 2020[3] and has since expanded to 50 countries.[4]
Profit margins improved significantly due to operational efficiency.[5] The CEO announced plans for further expansion.[6]`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validateContentImmutability(original, cited)
	}
}
