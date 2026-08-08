package activities

import "testing"

func TestClampCompressionTargetTokens(t *testing.T) {
	tests := []struct {
		name   string
		target int
		want   int
	}{
		{name: "zero", target: 0, want: minCompressionTargetTokens},
		{name: "below minimum", target: 63, want: minCompressionTargetTokens},
		{name: "minimum", target: 64, want: 64},
		{name: "within range", target: 400, want: 400},
		{name: "above maximum", target: 48000, want: maxCompressionTargetTokens},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := clampCompressionTargetTokens(test.target); got != test.want {
				t.Fatalf("got %d, want %d", got, test.want)
			}
		})
	}
}

func TestSelectMiddleHistoryPreservesConfiguredBoundaries(t *testing.T) {
	history := []map[string]string{
		{"content": "primer-1"},
		{"content": "primer-2"},
		{"content": "middle-1"},
		{"content": "middle-2"},
		{"content": "recent-1"},
		{"content": "recent-2"},
	}

	selected := selectMiddleHistory(history, 2, 2)

	if len(selected) != 2 {
		t.Fatalf("got %d messages, want 2", len(selected))
	}
	if selected[0]["content"] != "middle-1" || selected[1]["content"] != "middle-2" {
		t.Fatalf("unexpected middle history: %#v", selected)
	}
}

func TestSelectMiddleHistoryHandlesOverlappingBoundaries(t *testing.T) {
	history := []map[string]string{
		{"content": "primer"},
		{"content": "recent"},
	}

	selected := selectMiddleHistory(history, 3, 20)

	if len(selected) != 0 {
		t.Fatalf("got unexpected middle history: %#v", selected)
	}
}
