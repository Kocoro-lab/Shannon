package activities

import (
    "context"
)

// AnalyzeComplexity is a legacy compatibility shim used by older workflow histories.
// It returns a DecompositionResult that includes mode/complexity and optional subtasks.
// Implementation delegates to DecomposeTask to avoid duplicating logic.
func (a *Activities) AnalyzeComplexity(ctx context.Context, in DecompositionInput) (DecompositionResult, error) {
    // Delegate to DecomposeTask to keep behavior consistent.
    return a.DecomposeTask(ctx, in)
}

