package strategies

import "testing"

func TestNewCompressContextInputUsesConfiguredHistoryBoundaries(t *testing.T) {
	input := newCompressContextInput(
		TaskInput{
			SessionID:        "session-1",
			TenantID:         "tenant-1",
			ParentWorkflowID: "workflow-1",
			Context: map[string]interface{}{
				"primers_count": float64(2),
				"recents_count": 4,
			},
			History: []Message{
				{Role: "system", Content: "primer"},
				{Role: "user", Content: "middle"},
			},
		},
		500,
		true,
	)

	if input.PrimersCount != 2 || input.RecentsCount != 4 {
		t.Fatalf(
			"unexpected boundaries: primers=%d recents=%d",
			input.PrimersCount,
			input.RecentsCount,
		)
	}
	if input.TenantID != "tenant-1" || input.ParentWorkflowID != "workflow-1" {
		t.Fatalf("unexpected identity fields: %#v", input)
	}
	if !input.UseConfiguredBoundaries {
		t.Fatal("expected configured history boundaries")
	}
	if len(input.History) != 2 || input.History[1]["content"] != "middle" {
		t.Fatalf("unexpected history: %#v", input.History)
	}
}

func TestCompressionCountsRejectsNegativeOverrides(t *testing.T) {
	primersCount, recentsCount := compressionCounts(map[string]interface{}{
		"primers_count": -1,
		"recents_count": float64(-2),
	})

	if primersCount != 3 || recentsCount != 20 {
		t.Fatalf(
			"unexpected boundaries: primers=%d recents=%d",
			primersCount,
			recentsCount,
		)
	}
}
