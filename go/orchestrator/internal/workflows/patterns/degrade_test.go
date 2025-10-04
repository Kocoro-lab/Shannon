package patterns

import "testing"

func TestDegradeByBudget_DefaultThresholds(t *testing.T) {
	// Updated test for new thresholds (ToT: 8000, CoT: 4000)
	strategy, degraded := DegradeByBudget(PatternTreeOfThoughts, 6000, nil)
	if !degraded {
		t.Fatalf("expected degradation when budget below threshold")
	}
	if strategy != PatternChainOfThought {
		t.Fatalf("expected degrade to ChainOfThought, got %s", strategy)
	}

	strategy, degraded = DegradeByBudget(PatternChainOfThought, 2000, nil)
	if !degraded || strategy != PatternReact {
		t.Fatalf("expected degrade ChainOfThought -> React, got %s (degraded=%v)", strategy, degraded)
	}

	strategy, degraded = DegradeByBudget(PatternReact, 100, nil)
	if degraded {
		t.Fatalf("expected React to remain unchanged, but degraded to %s", strategy)
	}

	// Verify no degradation when budget is sufficient
	strategy, degraded = DegradeByBudget(PatternTreeOfThoughts, 10000, nil)
	if degraded {
		t.Fatalf("should not degrade with sufficient budget")
	}
	if strategy != PatternTreeOfThoughts {
		t.Fatalf("strategy should remain unchanged, got %s", strategy)
	}
}

func TestDegradeByBudget_CustomThresholds(t *testing.T) {
	thresholds := DegradationThresholds()
	// Set custom threshold lower than default (6000)
	thresholds[PatternDebate] = 5000

	// Budget below custom threshold should trigger degradation
	strategy, degraded := DegradeByBudget(PatternDebate, 4500, thresholds)
	if !degraded || strategy != PatternReflection {
		t.Fatalf("expected Debate to degrade to Reflection with custom thresholds, got %s (degraded=%v)", strategy, degraded)
	}

	// Budget above custom threshold should not degrade
	strategy, degraded = DegradeByBudget(PatternDebate, 5500, thresholds)
	if degraded {
		t.Fatalf("should not degrade when budget exceeds custom threshold")
	}
	if strategy != PatternDebate {
		t.Fatalf("strategy should remain Debate, got %s", strategy)
	}
}
