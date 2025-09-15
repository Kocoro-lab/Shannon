package replay

import (
    "os"
    "testing"

    "go.temporal.io/sdk/worker"

    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/strategies"
)

// TestDAGWorkflowReplay tests replay determinism for DAGWorkflow
func TestDAGWorkflowReplay(t *testing.T) {
	testCases := []struct {
		name        string
		historyFile string
		workflowID  string
	}{
		{
			name:        "simple_task",
			historyFile: "histories/dag_v2_simple.json",
			workflowID:  "dag-v2-simple-test",
		},
		{
			name:        "parallel_agents",
			historyFile: "histories/dag_v2_parallel.json",
			workflowID:  "dag-v2-parallel-test",
		},
		{
			name:        "with_reflection",
			historyFile: "histories/dag_v2_reflection.json",
			workflowID:  "dag-v2-reflection-test",
		},
	}

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            if _, err := os.Stat(tc.historyFile); err != nil {
                t.Skipf("history file not found (%s); generate via make replay-export", tc.historyFile)
            }
            replayer := worker.NewWorkflowReplayer()

			// Register the workflow
			replayer.RegisterWorkflow(strategies.DAGWorkflow)

			// Note: Activities don't need to be registered for replay testing
			// The replayer only validates workflow determinism, not activity execution

			// Attempt replay
            err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, tc.historyFile)
            if err != nil {
                t.Fatalf("Replay failed for %s: %v", tc.name, err)
            }
        })
    }
}

// TestReactWorkflowReplay tests replay determinism for ReactWorkflow
func TestReactWorkflowReplay(t *testing.T) {
	testCases := []struct {
		name        string
		historyFile string
		workflowID  string
	}{
		{
			name:        "basic_react",
			historyFile: "histories/react_v2_basic.json",
			workflowID:  "react-v2-basic-test",
		},
		{
			name:        "with_reflection",
			historyFile: "histories/react_v2_reflection.json",
			workflowID:  "react-v2-reflection-test",
		},
	}

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            if _, err := os.Stat(tc.historyFile); err != nil {
                t.Skipf("history file not found (%s); generate via make replay-export", tc.historyFile)
            }
            replayer := worker.NewWorkflowReplayer()

			// Register the workflow
			replayer.RegisterWorkflow(strategies.ReactWorkflow)

			// Note: Activities don't need to be registered for replay testing
			// The replayer only validates workflow determinism, not activity execution

			// Attempt replay
            err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, tc.historyFile)
            if err != nil {
                t.Fatalf("Replay failed for %s: %v", tc.name, err)
            }
        })
    }
}
