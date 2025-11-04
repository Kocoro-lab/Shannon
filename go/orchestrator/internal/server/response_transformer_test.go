package server

import (
	"strings"
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows"
)

func TestTruncateError(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantLen  int
	}{
		{
			name:     "short error unchanged",
			input:    "Connection refused",
			expected: "Connection refused",
			wantLen:  18,
		},
		{
			name:     "exactly 500 chars unchanged",
			input:    strings.Repeat("a", 500),
			expected: strings.Repeat("a", 500),
			wantLen:  500,
		},
		{
			name:     "501 chars gets truncated",
			input:    strings.Repeat("a", 501),
			expected: strings.Repeat("a", 500) + "... (truncated)",
			wantLen:  515,
		},
		{
			name:     "very long error truncated",
			input:    strings.Repeat("error", 1000),
			expected: strings.Repeat("error", 100) + "... (truncated)",
			wantLen:  515,
		},
		{
			name:     "empty string unchanged",
			input:    "",
			expected: "",
			wantLen:  0,
		},
		{
			name:     "Chinese characters - no truncation",
			input:    "数据库连接失败：无法建立连接",
			expected: "数据库连接失败：无法建立连接",
			wantLen:  len("数据库连接失败：无法建立连接"),
		},
		{
			name:     "Japanese characters - no truncation",
			input:    "データベース接続エラー：接続できません",
			expected: "データベース接続エラー：接続できません",
			wantLen:  len("データベース接続エラー：接続できません"),
		},
		{
			name:  "long Japanese text gets truncated at rune boundary",
			input: strings.Repeat("エラーが発生しました。", 60), // ~600 runes total
			// Should truncate to 500 runes and remain valid UTF-8
			expected: "", // Will be verified by UTF-8 validity check below
			wantLen:  -1, // Will check for reasonable length
		},
		{
			name:  "mixed English and Chinese",
			input: "Error: " + strings.Repeat("数据库错误 ", 100),
			// Should truncate to 500 runes and remain valid UTF-8
			expected: "", // Will be verified by UTF-8 validity check below
			wantLen:  -1, // Will check for reasonable length
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateError(tt.input)

			// For UTF-8 test cases (wantLen == -1), verify UTF-8 validity and reasonable bounds
			if tt.wantLen == -1 {
				// Verify UTF-8 validity
				runes := []rune(result)
				if string(runes) != result {
					t.Errorf("truncateError() produced invalid UTF-8")
				}

				// Should be truncated and have suffix
				inputRunes := []rune(tt.input)
				if len(inputRunes) > 500 {
					if !strings.HasSuffix(result, "... (truncated)") {
						t.Errorf("truncateError() should end with '... (truncated)' when truncated")
					}
					// Length should be around 500 runes + suffix
					if len(runes) < 500 || len(runes) > 520 {
						t.Errorf("truncateError() length = %d runes, expected around 500-520", len(runes))
					}
				}
				return
			}

			// For exact match test cases
			if tt.expected != "" && result != tt.expected {
				t.Errorf("truncateError() = %q, want %q", result, tt.expected)
			}
			if tt.wantLen > 0 && len(result) != tt.wantLen {
				t.Errorf("truncateError() length = %d, want %d", len(result), tt.wantLen)
			}
		})
	}
}

func TestExtractToolErrors(t *testing.T) {
	tests := []struct {
		name     string
		result   workflows.TaskResult
		expected []ToolError
	}{
		{
			name: "typed map format with short errors",
			result: workflows.TaskResult{
				Metadata: map[string]interface{}{
					"tool_errors": []map[string]string{
						{"agent_id": "a1", "tool": "web_search", "error": "Rate limit exceeded"},
						{"agent_id": "a2", "tool": "calculator", "error": "Division by zero"},
					},
				},
			},
			expected: []ToolError{
				{AgentID: "a1", Tool: "web_search", Message: "Rate limit exceeded"},
				{AgentID: "a2", Tool: "calculator", Message: "Division by zero"},
			},
		},
		{
			name: "long error gets truncated",
			result: workflows.TaskResult{
				Metadata: map[string]interface{}{
					"tool_errors": []map[string]string{
						{
							"agent_id": "a1",
							"tool":     "api_call",
							"error":    strings.Repeat("Very long error message. ", 50), // 1250 chars
						},
					},
				},
			},
			expected: []ToolError{
				{
					AgentID: "a1",
					Tool:    "api_call",
					Message: strings.Repeat("Very long error message. ", 20) + "... (truncated)",
				},
			},
		},
		{
			name: "interface format with mixed errors",
			result: workflows.TaskResult{
				Metadata: map[string]interface{}{
					"tool_errors": []interface{}{
						map[string]interface{}{
							"agent_id": "a1",
							"tool":     "short",
							"error":    "OK",
						},
						map[string]interface{}{
							"agent_id": "a2",
							"tool":     "long",
							"error":    strings.Repeat("x", 600),
						},
					},
				},
			},
			expected: []ToolError{
				{AgentID: "a1", Tool: "short", Message: "OK"},
				{AgentID: "a2", Tool: "long", Message: strings.Repeat("x", 500) + "... (truncated)"},
			},
		},
		{
			name: "nil metadata returns nil",
			result: workflows.TaskResult{
				Metadata: nil,
			},
			expected: nil,
		},
		{
			name: "missing tool_errors key returns nil",
			result: workflows.TaskResult{
				Metadata: map[string]interface{}{
					"other": "value",
				},
			},
			expected: nil,
		},
		{
			name: "empty tool_errors array",
			result: workflows.TaskResult{
				Metadata: map[string]interface{}{
					"tool_errors": []map[string]string{},
				},
			},
			expected: []ToolError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractToolErrors(tt.result)

			// Handle nil vs empty slice comparison
			if len(tt.expected) == 0 && len(result) == 0 {
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("extractToolErrors() returned %d errors, want %d", len(result), len(tt.expected))
				return
			}

			for i := range result {
				if result[i].AgentID != tt.expected[i].AgentID {
					t.Errorf("error[%d].AgentID = %q, want %q", i, result[i].AgentID, tt.expected[i].AgentID)
				}
				if result[i].Tool != tt.expected[i].Tool {
					t.Errorf("error[%d].Tool = %q, want %q", i, result[i].Tool, tt.expected[i].Tool)
				}
				if result[i].Message != tt.expected[i].Message {
					t.Errorf("error[%d].Message = %q, want %q", i, result[i].Message, tt.expected[i].Message)
				}
				// Verify truncation happened if original was long
				if len(result[i].Message) > 515 {
					t.Errorf("error[%d].Message length = %d, should be truncated to ≤515", i, len(result[i].Message))
				}
			}
		})
	}
}

func TestExtractToolErrorsTruncation(t *testing.T) {
	// Test that messages over 500 chars are properly truncated
	longError := strings.Repeat("ERROR ", 200) // 1200 chars
	result := workflows.TaskResult{
		Metadata: map[string]interface{}{
			"tool_errors": []map[string]string{
				{
					"agent_id": "test-agent",
					"tool":     "test-tool",
					"error":    longError,
				},
			},
		},
	}

	errors := extractToolErrors(result)
	if len(errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errors))
	}

	if len(errors[0].Message) != 515 { // 500 + len("... (truncated)")
		t.Errorf("expected message length 515, got %d", len(errors[0].Message))
	}

	if !strings.HasSuffix(errors[0].Message, "... (truncated)") {
		t.Errorf("expected truncated message to end with '... (truncated)', got: %q", errors[0].Message[490:])
	}

	// Verify the first 500 chars match original
	if errors[0].Message[:500] != longError[:500] {
		t.Errorf("truncated message doesn't match original first 500 chars")
	}
}

func TestTruncateError_UTF8Safety(t *testing.T) {
	// Test that truncation never produces invalid UTF-8 sequences
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "Chinese characters only",
			input: strings.Repeat("查询数据库", 150), // ~750 chars
		},
		{
			name:  "Japanese characters",
			input: strings.Repeat("データベースエラー", 100), // ~900 chars
		},
		{
			name:  "Emoji characters",
			input: strings.Repeat("Error 🚨 ", 100), // ~900 chars
		},
		{
			name:  "Mixed multibyte",
			input: "Error: " + strings.Repeat("数据库错误 データベース 🔥 ", 50),
		},
		{
			name:  "Arabic text",
			input: strings.Repeat("خطأ في قاعدة البيانات ", 60),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateError(tt.input)

			// Verify UTF-8 validity by rune roundtrip
			runes := []rune(result)
			reconstructed := string(runes)

			if reconstructed != result {
				t.Errorf("truncateError() produced invalid UTF-8\nInput: %q\nOutput: %q\nRoundtrip: %q",
					tt.input, result, reconstructed)
			}

			// Verify length constraint (500 runes + suffix)
			if len(runes) > 515 { // 500 + len("... (truncated)")
				t.Errorf("truncateError() length = %d runes, should be <= 515", len(runes))
			}

			// If truncation happened, should have suffix
			inputRunes := []rune(tt.input)
			if len(inputRunes) > 500 {
				if !strings.HasSuffix(result, "... (truncated)") {
					t.Errorf("truncateError() should end with '... (truncated)' when truncated")
				}
			}
		})
	}
}

func TestTruncateError_NoByteCutting(t *testing.T) {
	// Verify we never cut UTF-8 multi-byte sequences
	// Chinese char "查" is 3 bytes: [0xE6, 0x9F, 0xA5]
	// Japanese char "デ" is also 3 bytes: [0xE3, 0x83, 0x87]
	// If we cut at a byte boundary, we get invalid UTF-8

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "Chinese characters",
			input: strings.Repeat("查", 600),
		},
		{
			name:  "Japanese characters",
			input: strings.Repeat("データベース", 100),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateError(tt.input)

			// Check UTF-8 validity
			runes := []rune(result)
			reconstructed := string(runes)

			if reconstructed != result {
				t.Fatalf("truncateError() produced invalid UTF-8 by cutting multi-byte sequence")
			}

			// Should be truncated to 500 runes + suffix
			expectedRunes := 500 + len([]rune("... (truncated)"))
			if len(runes) != expectedRunes {
				t.Errorf("Expected %d runes, got %d", expectedRunes, len(runes))
			}
		})
	}
}
