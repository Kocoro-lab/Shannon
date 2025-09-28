package strategies

import (
    "fmt"
    "strconv"
    "strings"
    "time"

    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
    "go.temporal.io/sdk/workflow"
)

// convertHistoryForAgent converts message history to string format for agents
func convertHistoryForAgent(history []Message) []string {
	result := make([]string, len(history))
	for i, msg := range history {
		result[i] = fmt.Sprintf("%s: %s", msg.Role, msg.Content)
	}
	return result
}

// determineModelTier selects a model tier based on context and default
func determineModelTier(context map[string]interface{}, defaultTier string) string {
	// Check for explicit model tier in context
	if tier, ok := context["model_tier"].(string); ok && tier != "" {
		return tier
	}

	// Get thresholds from config (with defaults)
	simpleThreshold := 0.3
	mediumThreshold := 0.5  // Changed default from 0.7 to 0.5
	if cfg, ok := context["config"].(*activities.WorkflowConfig); ok && cfg != nil {
		if cfg.ComplexitySimpleThreshold > 0 {
			simpleThreshold = cfg.ComplexitySimpleThreshold
		}
		if cfg.ComplexityMediumThreshold > 0 {
			mediumThreshold = cfg.ComplexityMediumThreshold
		}
	}

	// Check for complexity score
	if complexity, ok := context["complexity"].(float64); ok {
		if complexity < simpleThreshold {
			return "small"
		} else if complexity < mediumThreshold {
			return "medium"
		}
		return "large"
	}

	// Use default if provided
	if defaultTier != "" {
		return defaultTier
	}

	return "medium"
}

// validateInput validates the input for a workflow
func validateInput(input TaskInput) error {
	if input.Query == "" {
		return fmt.Errorf("query cannot be empty")
	}
	if len(input.Query) > 10000 {
		return fmt.Errorf("query exceeds maximum length of 10000 characters")
	}
	return nil
}

// getBudgetMax extracts the budget maximum from context
func getBudgetMax(context map[string]interface{}) int {
	if v, ok := context["budget_agent_max"].(int); ok {
		return v
	}
	if v, ok := context["budget_agent_max"].(float64); ok && v > 0 {
		return int(v)
	}
	return 0
}

// getWorkflowConfig loads workflow configuration with defaults
func getWorkflowConfig(ctx workflow.Context) activities.WorkflowConfig {
	var config activities.WorkflowConfig
	configActivity := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx,
		workflow.ActivityOptions{StartToCloseTimeout: 10 * time.Second}),
		activities.GetWorkflowConfig,
	)
	if err := configActivity.Get(ctx, &config); err != nil {
		workflow.GetLogger(ctx).Warn("Failed to load config, using defaults", "error", err)
		// Return sensible defaults
		config = activities.WorkflowConfig{
			ExploratoryMaxIterations:         5,
			ExploratoryConfidenceThreshold:   0.85,
			ExploratoryBranchFactor:          3,
			ExploratoryMaxConcurrentAgents:   3,
			ScientificMaxHypotheses:          3,
			ScientificMaxIterations:          4,
			ScientificConfidenceThreshold:    0.85,
			ScientificContradictionThreshold: 0.2,
		}
	}
	return config
}

// extractPersonaHints extracts persona suggestions from context
func extractPersonaHints(context map[string]interface{}) []string {
	hints := []string{}

	// Check for domain keywords
	if domain, ok := context["domain"].(string); ok {
		switch domain {
		case "finance", "trading", "investment":
			hints = append(hints, "financial-analyst")
		case "engineering", "technical", "code":
			hints = append(hints, "software-engineer")
		case "medical", "health", "clinical":
			hints = append(hints, "medical-expert")
		case "legal", "law", "compliance":
			hints = append(hints, "legal-advisor")
		case "research", "academic", "science":
			hints = append(hints, "researcher")
		}
	}

	// Check for task type hints
	if taskType, ok := context["task_type"].(string); ok {
		switch taskType {
		case "analysis":
			hints = append(hints, "analyst")
		case "creative":
			hints = append(hints, "creative-writer")
		case "educational":
			hints = append(hints, "educator")
		case "strategic":
			hints = append(hints, "strategist")
		}
	}

	// Check for explicit persona hint
	if persona, ok := context["persona"].(string); ok && persona != "" {
		hints = append(hints, persona)
	}

	return hints
}

// parseNumericValue attempts to extract a numeric value from a response string
func parseNumericValue(response string) (float64, bool) {
    response = strings.TrimSpace(response)
    if val, err := strconv.ParseFloat(response, 64); err == nil {
        return val, true
    }
    fields := strings.Fields(response)
    var numbers []float64
    for i := 0; i < len(fields); i++ {
        token := strings.Trim(fields[i], ".,!?:;")
        if v, err := strconv.ParseFloat(token, 64); err == nil {
            numbers = append(numbers, v)
        }
        if (strings.EqualFold(token, "equals") || strings.EqualFold(token, "is")) && i+1 < len(fields) {
            next := strings.Trim(fields[i+1], ".,!?:;")
            if v, err := strconv.ParseFloat(next, 64); err == nil {
                return v, true
            }
        }
    }
    if len(numbers) > 0 {
        return numbers[len(numbers)-1], true
    }
    return 0, false
}

// shouldReflect determines if reflection should be applied based on complexity
func shouldReflect(complexity float64, config *activities.WorkflowConfig) bool {
	// Reflect on complex tasks to improve quality
	// Use configurable threshold from config
	threshold := 0.5 // Default fallback
	if config != nil && config.ComplexityMediumThreshold > 0 {
		threshold = config.ComplexityMediumThreshold
	}
	return complexity > threshold
}

// emitTaskUpdate sends a task update event (fire-and-forget with timeout)
func emitTaskUpdate(ctx workflow.Context, input TaskInput, eventType activities.StreamEventType, agentID, message string) {
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	})
	// Use parent workflow ID if this is a child workflow, otherwise use own ID
	wid := input.ParentWorkflowID
	if wid == "" {
		wid = workflow.GetInfo(ctx).WorkflowExecution.ID
	}
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate",
		activities.EmitTaskUpdateInput{
			WorkflowID: wid,
			EventType:  eventType,
			AgentID:    agentID,
			Message:    message,
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
}
