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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateError(tt.input)
			if result != tt.expected {
				t.Errorf("truncateError() = %q, want %q", result, tt.expected)
			}
			if len(result) != tt.wantLen {
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
