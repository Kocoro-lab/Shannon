package workflows

import (
	"fmt"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/models"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/workflow"
)

// convertHistoryForAgent formats session history into a simple string slice for agents
func convertHistoryForAgent(messages []Message) []string {
	result := make([]string, len(messages))
	for i, msg := range messages {
		result[i] = fmt.Sprintf("%s: %s", msg.Role, msg.Content)
	}
	return result
}

// parseNumericValue attempts to extract a numeric value from a response string
// parseNumericValue wrappers removed; use util.ParseNumericValue at call sites

// convertHistoryMapForCompression converts Message history to map format for compression
func convertHistoryMapForCompression(messages []Message) []map[string]string {
	result := make([]map[string]string, len(messages))
	for i, msg := range messages {
		result[i] = map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}
	return result
}

// getPrimersRecents extracts primers/recents counts from context with defaults.
func getPrimersRecents(ctx map[string]interface{}, defPrimers, defRecents int) (int, int) {
	p, r := defPrimers, defRecents
	if ctx == nil {
		return p, r
	}
	if v, ok := ctx["primers_count"].(int); ok {
		if v >= 0 {
			p = v
		}
	}
	if v, ok := ctx["primers_count"].(float64); ok {
		if v >= 0 {
			p = int(v)
		}
	}
	if v, ok := ctx["recents_count"].(int); ok {
		if v >= 0 {
			r = v
		}
	}
	if v, ok := ctx["recents_count"].(float64); ok {
		if v >= 0 {
			r = int(v)
		}
	}
	return p, r
}

// getCompressionRatios reads compression trigger/target ratios from context
// falling back to provided defaults. Values are clamped to sane bounds.
func getCompressionRatios(ctx map[string]interface{}, defTrigger, defTarget float64) (float64, float64) {
	tr, tg := defTrigger, defTarget
	clamp := func(v float64, lo, hi float64) float64 {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	if ctx != nil {
		if v, ok := ctx["compression_trigger_ratio"].(float64); ok {
			tr = v
		}
		if v, ok := ctx["compression_trigger_ratio"].(int); ok {
			tr = float64(v)
		}
		if v, ok := ctx["compression_target_ratio"].(float64); ok {
			tg = v
		}
		if v, ok := ctx["compression_target_ratio"].(int); ok {
			tg = float64(v)
		}
	}
	// Sane bounds to avoid extremes
	tr = clamp(tr, 0.1, 0.95)
	tg = clamp(tg, 0.1, 0.9)
	return tr, tg
}

// shapeHistory returns a sliced history keeping the first nPrimers and last nRecents messages
// when there are enough messages to have a middle section. Otherwise it returns the original.
func shapeHistory(messages []Message, nPrimers, nRecents int) []Message {
	m := len(messages)
	if m == 0 {
		return messages
	}
	if nPrimers < 0 {
		nPrimers = 0
	}
	if nRecents < 0 {
		nRecents = 0
	}
	if nPrimers+nRecents >= m {
		return messages
	}
	primersEnd := nPrimers
	if primersEnd > m {
		primersEnd = m
	}
	recentsStart := m - nRecents
	if recentsStart < primersEnd {
		recentsStart = primersEnd
	}
	shaped := make([]Message, 0, primersEnd+(m-recentsStart))
	shaped = append(shaped, messages[:primersEnd]...)
	shaped = append(shaped, messages[recentsStart:]...)
	return shaped
}

// fallbackToBasicMemory loads basic hierarchical memory for supervisor workflow
func fallbackToBasicMemory(ctx workflow.Context, input *TaskInput, logger log.Logger) {
	var memoryResult activities.FetchHierarchicalMemoryResult
	memoryInput := activities.FetchHierarchicalMemoryInput{
		Query:        input.Query,
		SessionID:    input.SessionID,
		TenantID:     input.TenantID,
		RecentTopK:   5,
		SemanticTopK: 3,
		SummaryTopK:  2,
		Threshold:    0.7,
	}

	if err := workflow.ExecuteActivity(ctx, "FetchHierarchicalMemory", memoryInput).Get(ctx, &memoryResult); err == nil {
		if len(memoryResult.Items) > 0 {
			if input.Context == nil {
				input.Context = make(map[string]interface{})
			}
			input.Context["agent_memory"] = memoryResult.Items
			logger.Info("Injected basic memory into supervisor context",
				"session_id", input.SessionID,
				"memory_items", len(memoryResult.Items))
		}
	}
}

// extractSubtaskDescriptions extracts descriptions from subtasks
func extractSubtaskDescriptions(subtasks []activities.Subtask) []string {
	descriptions := make([]string, len(subtasks))
	for i, st := range subtasks {
		descriptions[i] = st.Description
	}
	return descriptions
}

// detectProviderFromModel determines the provider based on the model name
// Delegates to shared models.DetectProvider for consistent provider detection
func detectProviderFromModel(model string) string {
	return models.DetectProvider(model)
}

// aggregateAgentMetadata extracts model, provider, and token information from agent results
// Returns metadata map with model_used, provider, input_tokens, output_tokens
// aggregateAgentMetadata removed; use metadata.AggregateAgentMetadata at call sites
