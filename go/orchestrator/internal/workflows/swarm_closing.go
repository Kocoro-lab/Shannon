package workflows

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
)

// buildClosingSummary creates a concise summary for the Lead's closing_checkpoint event.
// Includes agent results and workspace file list.
func buildClosingSummary(results map[string]AgentLoopResult, files []activities.WorkspaceMaterial) string {
	var parts []string

	// Agent summary
	ids := make([]string, 0, len(results))
	for id := range results {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	parts = append(parts, fmt.Sprintf("%d agents completed.", len(results)))
	for _, id := range ids {
		r := results[id]
		status := "success"
		if !r.Success {
			status = "failed"
		}
		summary := r.Response
		if len(summary) > 200 {
			summary = summary[:200] + "..."
		}
		parts = append(parts, fmt.Sprintf("- %s [%s, %s]: %s", r.AgentID, r.Role, status, summary))
	}

	// Workspace files — include content so Lead can write a proper reply
	if len(files) > 0 {
		fileLines := []string{fmt.Sprintf("Workspace files (%d):", len(files))}
		for _, f := range files {
			content := f.Content
			if len(content) > 4000 {
				content = content[:4000] + "\n... (truncated)"
			}
			fileLines = append(fileLines, fmt.Sprintf("--- %s (%d chars) ---\n%s", f.Path, len(f.Content), content))
		}
		parts = append(parts, strings.Join(fileLines, "\n"))
	} else {
		parts = append(parts, "No workspace files produced.")
	}

	return strings.Join(parts, "\n")
}

// isLeadReplyValid checks if a Lead reply meets minimum quality bar.
// Only rejects truly empty or trivial replies — agents already did the heavy lifting.
func isLeadReplyValid(reply string, _ []activities.WorkspaceMaterial) bool {
	return len(strings.TrimSpace(reply)) >= 50
}
