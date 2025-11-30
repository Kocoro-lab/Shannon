package strategies

import (
	"fmt"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/models"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
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
	// Check for explicit model tier in context (highest priority)
	if tier, ok := context["model_tier"].(string); ok && strings.TrimSpace(tier) != "" {
		return strings.ToLower(strings.TrimSpace(tier))
	}

	// Get thresholds from config (with defaults)
	simpleThreshold := 0.3
	mediumThreshold := 0.5 // Changed default from 0.7 to 0.5
	if cfg, ok := context["config"].(*activities.WorkflowConfig); ok && cfg != nil {
		if cfg.ComplexitySimpleThreshold > 0 {
			simpleThreshold = cfg.ComplexitySimpleThreshold
		}
		if cfg.ComplexityMediumThreshold > 0 {
			mediumThreshold = cfg.ComplexityMediumThreshold
		}
	}

	// Strategy-aware overrides for research workflows (applied before pure complexity routing)
	// NOTE: These tiers apply to AGENT EXECUTION only.
	// Final synthesis always uses "large" tier (hardcoded in synthesis.go:361).
	// Utility activities (coverage_eval, fact_extraction, etc.) use "small" tier.
	if strategyRaw, ok := context["research_strategy"].(string); ok && strings.TrimSpace(strategyRaw) != "" {
		strategy := strings.ToLower(strings.TrimSpace(strategyRaw))

		switch strategy {
		case "quick":
			// Fast, cheap research regardless of complexity.
			return "small"
		case "standard":
			// Default research path.
			return "medium"
		case "deep":
			// Use medium for agent execution; final synthesis will use large.
			// Research shows agentic workflows with smaller models + iteration
			// match or outperform single large-model calls at lower cost.
			return "medium"
		case "academic":
			// Academic uses medium for agent execution; final synthesis uses large.
			// Cost optimization: only synthesis benefits from large tier.
			return "medium"
		}
	}

	// Fall back to complexity-based routing
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
// parseNumericValue removed; use util.ParseNumericValue at call sites

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

// emitTaskUpdatePayload sends a task update event with an optional payload
func emitTaskUpdatePayload(ctx workflow.Context, input TaskInput, eventType activities.StreamEventType, agentID, message string, payload map[string]interface{}) {
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
            Payload:    payload,
        }).Get(ctx, nil)
}

// detectProviderFromModel determines the provider based on the model name
// Delegates to shared models.DetectProvider for consistent provider detection
func detectProviderFromModel(model string) string {
	return models.DetectProvider(model)
}

// aggregateAgentMetadata extracts model, provider, and token information from agent results
// Returns metadata map with model_used, provider, input_tokens, output_tokens
// aggregateAgentMetadata removed; use metadata.AggregateAgentMetadata at call sites

// getPriorityModelForTier resolves a tier string to an actual model name using config.
// Returns the priority-1 model for the tier, or empty string if not found.
func getPriorityModelForTier(tier string) string {
	return pricing.GetPriorityOneModel(tier)
}
