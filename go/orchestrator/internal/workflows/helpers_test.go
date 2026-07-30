package workflows

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewCompressContextInputPreservesConfiguredHistoryBoundaries(t *testing.T) {
	history := []Message{
		{Role: "system", Content: "primer"},
		{Role: "user", Content: "middle"},
		{Role: "assistant", Content: "recent"},
	}
	input := newCompressContextInput(
		"session-1",
		"tenant-1",
		history,
		400,
		"workflow-1",
		map[string]interface{}{
			"primers_count": 1,
			"recents_count": float64(1),
		},
		true,
	)

	if input.PrimersCount != 1 || input.RecentsCount != 1 {
		t.Fatalf(
			"unexpected boundaries: primers=%d recents=%d",
			input.PrimersCount,
			input.RecentsCount,
		)
	}
	if input.TenantID != "tenant-1" {
		t.Fatalf("unexpected tenant ID: %q", input.TenantID)
	}
	if !input.UseConfiguredBoundaries {
		t.Fatal("expected configured history boundaries")
	}
	if len(input.History) != 3 || input.History[1]["content"] != "middle" {
		t.Fatalf("unexpected history: %#v", input.History)
	}
}

func TestNewCompressContextInputPreservesLegacyWorkflowInput(t *testing.T) {
	input := newCompressContextInput(
		"session-1",
		"tenant-1",
		[]Message{{Role: "user", Content: "history"}},
		400,
		"workflow-1",
		map[string]interface{}{
			"primers_count": 1,
			"recents_count": 1,
		},
		false,
	)

	if input.UseConfiguredBoundaries || input.PrimersCount != 0 || input.RecentsCount != 0 {
		t.Fatalf("unexpected boundary fields: %#v", input)
	}
	if input.TenantID != "" {
		t.Fatalf("unexpected tenant ID for legacy workflow: %q", input.TenantID)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	for _, field := range []string{
		"tenant_id",
		"primers_count",
		"recents_count",
		"use_configured_boundaries",
	} {
		if strings.Contains(string(encoded), field) {
			t.Fatalf("legacy input unexpectedly contains %q: %s", field, encoded)
		}
	}
}

func TestConvertHistoryForAgent_NewlineEscaping(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		want     []string
	}{
		{
			name: "single-line messages unchanged",
			messages: []Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi there"},
			},
			want: []string{
				"user: hello",
				"assistant: hi there",
			},
		},
		{
			name: "multi-line assistant content is escaped",
			messages: []Message{
				{Role: "user", Content: "help me"},
				{Role: "assistant", Content: "Line one\nLine two\nLine three"},
			},
			want: []string{
				"user: help me",
				`assistant: Line one\nLine two\nLine three`,
			},
		},
		{
			name: "multi-line user content is escaped",
			messages: []Message{
				{Role: "user", Content: "First line\nSecond line"},
			},
			want: []string{
				`user: First line\nSecond line`,
			},
		},
		{
			name: "content with existing backslash-n literal unchanged",
			messages: []Message{
				{Role: "assistant", Content: `Use \n for newlines`},
			},
			want: []string{
				// Literal backslash+n in source is NOT a newline byte — ReplaceAll doesn't touch it
				`assistant: Use \n for newlines`,
			},
		},
		{
			name: "empty content",
			messages: []Message{
				{Role: "user", Content: ""},
			},
			want: []string{
				"user: ",
			},
		},
		{
			name: "Japanese multi-line content",
			messages: []Message{
				{Role: "assistant", Content: "ご質問ありがとうございます。\n\nキーワードを教えてください。"},
			},
			want: []string{
				`assistant: ご質問ありがとうございます。\n\nキーワードを教えてください。`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertHistoryForAgent(tt.messages)
			if len(got) != len(tt.want) {
				t.Fatalf("length mismatch: got %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("index %d:\ngot:  %q\nwant: %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestConvertHistoryForAgent_RoundTrip(t *testing.T) {
	// Verify that escaped content can be unescaped back to original
	original := "Line one\nLine two\nLine three"
	messages := []Message{{Role: "assistant", Content: original}}

	result := convertHistoryForAgent(messages)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}

	// The result should not contain any real newlines
	if strings.Contains(result[0], "\n") {
		t.Error("result contains real newline — escaping failed")
	}

	// Extract content after "assistant: " and unescape
	content := strings.TrimPrefix(result[0], "assistant: ")
	unescaped := strings.ReplaceAll(content, `\n`, "\n")
	if unescaped != original {
		t.Errorf("round-trip failed:\ngot:  %q\nwant: %q", unescaped, original)
	}
}
