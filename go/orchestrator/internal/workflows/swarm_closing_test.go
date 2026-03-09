package workflows

import (
	"strings"
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/stretchr/testify/assert"
)

func TestBuildClosingSummary(t *testing.T) {
	results := map[string]AgentLoopResult{
		"Akiba": {
			AgentID:  "Akiba",
			Role:     "coder",
			Response: "Implemented the API server with 3 endpoints for user management.",
			Success:  true,
		},
		"Maji": {
			AgentID:  "Maji",
			Role:     "coder",
			Response: "Created unit tests for all endpoints. Tests pass.",
			Success:  true,
		},
	}

	files := []activities.WorkspaceMaterial{
		{Path: "api/server.py", Content: "content here", Truncated: false},
		{Path: "tests/test_api.py", Content: "test content", Truncated: false},
	}

	summary := buildClosingSummary(results, files)

	assert.Contains(t, summary, "2 agents completed")
	assert.Contains(t, summary, "Akiba")
	assert.Contains(t, summary, "Maji")
	assert.Contains(t, summary, "coder")
	assert.Contains(t, summary, "api/server.py")
	assert.Contains(t, summary, "tests/test_api.py")
	assert.Contains(t, summary, "Implemented the API server")
}

func TestBuildClosingSummaryNoFiles(t *testing.T) {
	results := map[string]AgentLoopResult{
		"Akiba": {
			AgentID:  "Akiba",
			Role:     "researcher",
			Response: "Found pricing data for AWS and Azure.",
			Success:  true,
		},
	}

	summary := buildClosingSummary(results, nil)

	assert.Contains(t, summary, "1 agents completed")
	assert.Contains(t, summary, "No workspace files")
}

func TestBuildClosingSummaryTruncatesLongResponses(t *testing.T) {
	longResponse := strings.Repeat("x", 500)
	results := map[string]AgentLoopResult{
		"Akiba": {
			AgentID:  "Akiba",
			Role:     "researcher",
			Response: longResponse,
			Success:  true,
		},
	}

	summary := buildClosingSummary(results, nil)
	assert.True(t, len(summary) < len(longResponse))
}

func TestIsLeadReplyValid(t *testing.T) {
	files := []activities.WorkspaceMaterial{
		{Path: "api/server.py", Content: "..."},
	}

	// Valid: long enough + references file
	assert.True(t, isLeadReplyValid(
		"Task complete. I created the API server module. Key file: api/server.py with 3 endpoints.",
		files,
	))

	// Invalid: too short
	assert.False(t, isLeadReplyValid("Done.", files))

	// Valid: long enough reply (file path reference no longer required)
	assert.True(t, isLeadReplyValid(
		"I completed the task and created all the necessary files for the API server module.",
		files,
	))

	// Valid: no files, just text reply
	assert.True(t, isLeadReplyValid(
		"Based on the research, AWS is 15% cheaper than Azure for compute workloads. Here are the details...",
		nil,
	))
}
