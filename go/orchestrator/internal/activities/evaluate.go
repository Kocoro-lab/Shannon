package activities

import (
	"context"
	"strings"
)

// EvaluateResult provides a minimal, fast heuristic to score a response.
// It avoids external calls to keep workflows deterministic and CI friendly.
func (a *Activities) EvaluateResult(ctx context.Context, in EvaluateResultInput) (EvaluateResultOutput, error) { // nolint:revive
	// Very lightweight scoring:
	// - Empty or very short responses score low
	// - Otherwise return a neutral/high score with simple feedback
	trimmed := strings.TrimSpace(in.Response)
	if trimmed == "" || len(trimmed) < 8 {
		return EvaluateResultOutput{
			Score:    0.3,
			Feedback: "Response seems too short or empty; add detail and specifics.",
		}, nil
	}

	// If criteria are provided, include them in feedback to guide improvements
	var fb string
	if len(in.Criteria) > 0 {
		fb = "Check criteria: " + strings.Join(in.Criteria, ", ")
	} else {
		fb = "Looks reasonable; verify correctness and clarity."
	}

	return EvaluateResultOutput{
		Score:    0.85,
		Feedback: fb,
	}, nil
}
