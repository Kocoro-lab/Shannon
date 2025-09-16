package main

import (
    "flag"
    "fmt"
    "log"
    "os"

    "go.temporal.io/sdk/worker"

    // Register workflows from the orchestrator module
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/strategies"
)

func main() {
    historyPath := flag.String("history", "", "Path to Temporal workflow history JSON (from tctl --output json)")
    flag.Parse()

    if *historyPath == "" {
        fmt.Fprintln(os.Stderr, "usage: replay -history /path/to/history.json")
        os.Exit(2)
    }

    // Create a replayer and register all known workflows.
    replayer := worker.NewWorkflowReplayer()
    replayer.RegisterWorkflow(workflows.OrchestratorWorkflow)
    replayer.RegisterWorkflow(workflows.SimpleTaskWorkflow)
    replayer.RegisterWorkflow(workflows.SupervisorWorkflow)
    replayer.RegisterWorkflow(workflows.StreamingWorkflow)
    replayer.RegisterWorkflow(workflows.ParallelStreamingWorkflow)
    replayer.RegisterWorkflow(workflows.AgentDAGWorkflow)
    replayer.RegisterWorkflow(strategies.DAGWorkflow)
    replayer.RegisterWorkflow(strategies.ReactWorkflow)
    replayer.RegisterWorkflow(strategies.ResearchWorkflow)
    replayer.RegisterWorkflow(strategies.ExploratoryWorkflow)
    replayer.RegisterWorkflow(strategies.ScientificWorkflow)
    // Approval and budget are now middleware; no separate workflows to register

    // Replay from file; this will error on any non-determinism between history and code.
    if err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, *historyPath); err != nil {
        log.Fatalf("Replay failed (non-deterministic change or invalid history): %v", err)
    }

    log.Printf("Replay succeeded for %s", *historyPath)
}
