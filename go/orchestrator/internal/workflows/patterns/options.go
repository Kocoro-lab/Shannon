package patterns

// Options provides common configuration for pattern execution
type Options struct {
	BudgetAgentMax int                    // Per-agent token budget
	SessionID      string                 // Session identifier
	UserID         string                 // User identifier for budget/recording
	EmitEvents     bool                   // Whether to emit streaming events
	ModelTier      string                 // Model tier (small/medium/large)
	Context        map[string]interface{} // Additional context
}

// ReflectionConfig controls reflection behavior
type ReflectionConfig struct {
	Enabled             bool     // Whether reflection is enabled
	MaxRetries          int      // Maximum reflection iterations
	ConfidenceThreshold float64  // Minimum acceptable quality score
	Criteria            []string // Evaluation criteria
	TimeoutMs           int      // Timeout per reflection attempt
}
