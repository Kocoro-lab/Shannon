package workflows

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
)

// buildClosingSummary creates a concise summary for the Lead's closing_checkpoint event.
// Includes agent results and workspace file list.
// currentRunFiles identifies files written by THIS swarm run's agents — their content is prioritized.
// Files from previous runs (multi-turn session) are listed as paths only to avoid crowding out current content.
func buildClosingSummary(results map[string]AgentLoopResult, files []activities.WorkspaceMaterial, currentRunFiles map[string]bool) string {
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

	// Workspace files — prioritize current run's files with full content
	if len(files) > 0 {
		var currentFiles, olderFiles []activities.WorkspaceMaterial
		for _, f := range files {
			if currentRunFiles[f.Path] {
				currentFiles = append(currentFiles, f)
			} else {
				olderFiles = append(olderFiles, f)
			}
		}

		fileLines := []string{fmt.Sprintf("Workspace files (%d total, %d from this run):", len(files), len(currentFiles))}

		// Current run files: include full content (up to 4000 chars each)
		for _, f := range currentFiles {
			content := f.Content
			if len(content) > 4000 {
				content = content[:4000] + "\n... (truncated)"
			}
			fileLines = append(fileLines, fmt.Sprintf("--- %s (%d chars) ---\n%s", f.Path, len(f.Content), content))
		}

		// Older files from previous runs: path only (Lead can file_read if needed)
		if len(olderFiles) > 0 {
			fileLines = append(fileLines, fmt.Sprintf("\nFiles from previous runs (%d, use file_read to access):", len(olderFiles)))
			for _, f := range olderFiles {
				fileLines = append(fileLines, fmt.Sprintf("- %s (%d chars)", f.Path, len(f.Content)))
			}
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
