package patterns

// Budget thresholds (in tokens) for triggering degradation.
// Updated for modern LLMs with 128K+ context windows (2025).
// Previous thresholds were designed for GPT-3.5 era (4K-8K context).
var defaultDegradeThresholds = map[PatternType]int{
	PatternTreeOfThoughts: 8000,  // Was 2000
	PatternChainOfThought: 4000,  // Was 1000
	PatternDebate:         6000,  // Was 1500
	PatternReflection:     3000,  // Was 800
}

// degradeChain defines the next strategy when degrading from a given pattern.
var degradeChain = map[PatternType]PatternType{
	PatternTreeOfThoughts: PatternChainOfThought,
	PatternChainOfThought: PatternReact,
	PatternDebate:         PatternReflection,
	PatternReflection:     PatternReact,
}

// DegradationThresholds returns a copy of the default thresholds map.
func DegradationThresholds() map[PatternType]int {
	out := make(map[PatternType]int, len(defaultDegradeThresholds))
	for k, v := range defaultDegradeThresholds {
		out[k] = v
	}
	return out
}

// DegradeByBudget returns the most complex strategy that fits within the supplied budget.
// If no degradation is applied, the returned pattern matches the input.
func DegradeByBudget(strategy PatternType, budget int, thresholds map[PatternType]int) (PatternType, bool) {
	if budget <= 0 {
		return strategy, false
	}

	table := thresholds
	if table == nil {
		table = defaultDegradeThresholds
	}

	current := strategy
	degraded := false

	for {
		threshold, ok := table[current]
		if !ok || threshold <= 0 || budget >= threshold {
			break
		}
		next, ok := degradeChain[current]
		if !ok || next == "" {
			break
		}
		current = next
		degraded = true
	}

	return current, degraded
}
