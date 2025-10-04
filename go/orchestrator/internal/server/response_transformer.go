package server

import (
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows"
)

// UnifiedResponse represents the standardized API response format
type UnifiedResponse struct {
	TaskID    string                 `json:"task_id"`
	SessionID string                 `json:"session_id"`
	Status    string                 `json:"status"`
	Result    string                 `json:"result"`
	Metadata  ResponseMetadata       `json:"metadata"`
	Usage     ResponseUsage          `json:"usage"`
    Performance ResponsePerformance `json:"performance,omitempty"`
    StopReason string               `json:"stop_reason"`
    Error     *string                `json:"error"`
    Timestamp string                 `json:"timestamp"`
    ToolErrors []ToolError           `json:"tool_errors,omitempty"`
}

// ToolError represents a single tool failure surfaced to clients
type ToolError struct {
    AgentID string `json:"agent_id,omitempty"`
    Tool    string `json:"tool"`
    Message string `json:"message"`
}

// ResponseMetadata contains execution metadata
type ResponseMetadata struct {
	Model          string   `json:"model,omitempty"`           // Single agent
	Models         []string `json:"models,omitempty"`          // Multi-agent (when > 1)
	ExecutionMode  string   `json:"execution_mode"`
	ComplexityScore float64  `json:"complexity_score"`
	ServiceTier    string   `json:"service_tier"`
	AgentsUsed     int      `json:"agents_used"`
}

// ResponseUsage contains token and cost information
type ResponseUsage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	CacheHits    *int    `json:"cache_hits,omitempty"`    // Optional until wired
	CacheScore   *float64 `json:"cache_score,omitempty"`  // Optional until wired
}

// ResponsePerformance contains timing metrics
type ResponsePerformance struct {
	ExecutionTimeMs int64 `json:"execution_time_ms"`
	LatencyMs       int64 `json:"latency_ms,omitempty"`
	QueueTimeMs     int64 `json:"queue_time_ms,omitempty"`
}

// TransformToUnifiedResponse converts internal task result to unified API response
func TransformToUnifiedResponse(result workflows.TaskResult, sessionID string, executionTime int64) UnifiedResponse {
	status := "completed"
	if !result.Success {
		status = "failed"
	}

	// Extract metadata from result
	metadata := extractMetadata(result)
	usage := extractUsage(result)

	// Determine stop reason
	stopReason := determineStopReason(result)

	// Handle error
	var errorPtr *string
	if result.ErrorMessage != "" {
		errorPtr = &result.ErrorMessage
	}

    // Extract tool errors from metadata (if present)
    toolErrors := extractToolErrors(result)

    return UnifiedResponse{
        TaskID:    extractTaskID(result),
        SessionID: sessionID,
        Status:    status,
        Result:    result.Result,
        Metadata:  metadata,
        Usage:     usage,
        Performance: ResponsePerformance{
            ExecutionTimeMs: executionTime,
        },
        StopReason: stopReason,
        Error:      errorPtr,
        Timestamp:  time.Now().UTC().Format(time.RFC3339),
        ToolErrors: toolErrors,
    }
}

// extractToolErrors builds a client-friendly list of tool errors from result metadata
func extractToolErrors(result workflows.TaskResult) []ToolError {
    if result.Metadata == nil {
        return nil
    }
    raw, ok := result.Metadata["tool_errors"]
    if !ok || raw == nil {
        return nil
    }

    out := make([]ToolError, 0)
    // Try typed []map[string]string first
    if arr, ok := raw.([]map[string]string); ok {
        for _, m := range arr {
            out = append(out, ToolError{
                AgentID: m["agent_id"],
                Tool:    m["tool"],
                Message: truncateError(m["error"]),
            })
        }
        return out
    }
    // Fallback to []interface{} of maps
    if arr, ok := raw.([]interface{}); ok {
        for _, v := range arr {
            if m, ok := v.(map[string]interface{}); ok {
                te := ToolError{}
                if a, ok := m["agent_id"].(string); ok { te.AgentID = a }
                if t, ok := m["tool"].(string); ok { te.Tool = t }
                if e, ok := m["error"].(string); ok { te.Message = truncateError(e) }
                if te.Tool != "" || te.Message != "" {
                    out = append(out, te)
                }
            }
        }
        return out
    }
    return nil
}

// truncateError limits error messages to 500 characters to prevent response bloat
func truncateError(msg string) string {
    const maxLen = 500
    if len(msg) <= maxLen {
        return msg
    }
    return msg[:maxLen] + "... (truncated)"
}

func extractMetadata(result workflows.TaskResult) ResponseMetadata {
	meta := ResponseMetadata{
		ExecutionMode: "standard", // Default
		ServiceTier:   "medium",   // Default
	}

	// Guard against nil metadata
	if result.Metadata == nil {
		return meta
	}

	// Process metadata (safe to access after nil check)
	// Extract execution mode with fallbacks
	if mode, ok := result.Metadata["execution_mode"].(string); ok {
			meta.ExecutionMode = mode
		} else if mode, ok := result.Metadata["mode"].(string); ok {
			// Fallback: workflows use "mode" instead of "execution_mode"
			meta.ExecutionMode = mode
		}

		// Extract complexity score with fallbacks
		if complexity, ok := result.Metadata["complexity"].(float64); ok {
			meta.ComplexityScore = complexity
			meta.ServiceTier = determineServiceTier(complexity)
		} else if complexity, ok := result.Metadata["complexity_score"].(float64); ok {
			// Fallback: workflows use "complexity_score"
			meta.ComplexityScore = complexity
			meta.ServiceTier = determineServiceTier(complexity)
		}

		// Extract agent count with fallbacks (handle both int and float64)
		if agents, ok := result.Metadata["agents_used"].(int); ok {
			meta.AgentsUsed = agents
		} else if agents, ok := result.Metadata["agents_used"].(float64); ok {
			meta.AgentsUsed = int(agents)
		} else if agents, ok := result.Metadata["num_agents"].(int); ok {
			// Fallback: simple_workflow uses "num_agents"
			meta.AgentsUsed = agents
		} else if agents, ok := result.Metadata["num_agents"].(float64); ok {
			meta.AgentsUsed = int(agents)
		} else if agents, ok := result.Metadata["num_children"].(int); ok {
			// Fallback: supervisor_workflow uses "num_children"
			meta.AgentsUsed = agents
		} else if agents, ok := result.Metadata["num_children"].(float64); ok {
			meta.AgentsUsed = int(agents)
		} else if agents, ok := result.Metadata["num_streams"].(int); ok {
			// Fallback: streaming_workflow uses "num_streams"
			meta.AgentsUsed = agents
		} else if agents, ok := result.Metadata["num_streams"].(float64); ok {
			meta.AgentsUsed = int(agents)
		}

		// Extract models used with fallbacks
		if models, ok := result.Metadata["models"].([]string); ok && len(models) > 1 {
			meta.Models = models
		} else if model, ok := result.Metadata["model"].(string); ok {
			meta.Model = model
		} else if modelUsed, ok := result.Metadata["model_used"].(string); ok {
			// Fallback for legacy field
			meta.Model = modelUsed
		} else if modelUsed, ok := result.Metadata["ModelUsed"].(string); ok {
			// Fallback for Go-style field naming
			meta.Model = modelUsed
		}

		// Handle multi-agent results
		if agentResults, ok := result.Metadata["agent_results"].([]activities.AgentExecutionResult); ok {
			models := extractUniqueModels(agentResults)
			if len(models) > 1 {
				meta.Models = models
				meta.Model = "" // Clear single model field
			} else if len(models) == 1 {
				meta.Model = models[0]
			}
			meta.AgentsUsed = len(agentResults)
		}

		// Try to extract models from agent_usages if available
		if meta.Models == nil && meta.Model == "" {
			if usages, ok := result.Metadata["agent_usages"].([]activities.AgentUsage); ok {
				models := extractUniqueModelsFromUsages(usages)
				if len(models) > 1 {
					meta.Models = models
				} else if len(models) == 1 {
					meta.Model = models[0]
				}
			}
		}

	return meta
}

func extractUsage(result workflows.TaskResult) ResponseUsage {
	usage := ResponseUsage{
		TotalTokens: result.TokensUsed,
	}

	if result.Metadata != nil {
		// Extract detailed token counts (handle both int and float64)
		if input, ok := result.Metadata["input_tokens"].(int); ok {
			usage.InputTokens = input
		} else if input, ok := result.Metadata["input_tokens"].(float64); ok {
			usage.InputTokens = int(input)
		}
		if output, ok := result.Metadata["output_tokens"].(int); ok {
			usage.OutputTokens = output
		} else if output, ok := result.Metadata["output_tokens"].(float64); ok {
			usage.OutputTokens = int(output)
		}

		// Try to aggregate from agent_usages if individual tokens not set
		if usage.InputTokens == 0 && usage.OutputTokens == 0 {
			if usages, ok := result.Metadata["agent_usages"].([]activities.AgentUsage); ok {
				for _, u := range usages {
					usage.InputTokens += u.InputTokens
					usage.OutputTokens += u.OutputTokens
				}
			}
		}

		// Calculate cost with fallbacks
		if cost, ok := result.Metadata["cost_usd"].(float64); ok {
			usage.CostUSD = cost
		} else if cost, ok := result.Metadata["total_cost"].(float64); ok {
			usage.CostUSD = cost
		} else if cost, ok := result.Metadata["cost"].(float64); ok {
			usage.CostUSD = cost
		}

		// Optional cache metrics (when available)
		if cacheHits, ok := result.Metadata["cache_hits"].(int); ok {
			usage.CacheHits = &cacheHits
		}
		if cacheScore, ok := result.Metadata["cache_score"].(float64); ok {
			usage.CacheScore = &cacheScore
		}
	}

	// Ensure total matches sum if we have both
	if usage.InputTokens > 0 && usage.OutputTokens > 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}

	return usage
}

func determineStopReason(result workflows.TaskResult) string {
	if !result.Success {
		if result.Metadata != nil {
			if reason, ok := result.Metadata["stop_reason"].(string); ok {
				return reason
			}
		}
		return "error"
	}

	// Check for specific stop reasons in metadata
	if result.Metadata != nil {
		if reason, ok := result.Metadata["stop_reason"].(string); ok {
			return reason
		}
		if _, ok := result.Metadata["max_tokens_reached"].(bool); ok {
			return "max_tokens"
		}
		if _, ok := result.Metadata["timeout"].(bool); ok {
			return "timeout"
		}
	}

	return "completed"
}

func determineServiceTier(complexity float64) string {
	if complexity < 0.3 {
		return "small"
	} else if complexity < 0.5 {
		return "medium"
	}
	return "large"
}

func extractTaskID(result workflows.TaskResult) string {
	if result.Metadata != nil {
		if taskID, ok := result.Metadata["task_id"].(string); ok {
			return taskID
		}
		if workflowID, ok := result.Metadata["workflow_id"].(string); ok {
			return workflowID
		}
	}
	return ""
}

func extractUniqueModels(results []activities.AgentExecutionResult) []string {
	// Validate input
	if len(results) == 0 {
		return nil
	}

	modelMap := make(map[string]bool)
	var models []string

	for _, r := range results {
		if r.ModelUsed != "" && !modelMap[r.ModelUsed] {
			modelMap[r.ModelUsed] = true
			models = append(models, r.ModelUsed)
		}
	}

	return models
}

func extractUniqueModelsFromUsages(usages []activities.AgentUsage) []string {
	// Validate input
	if len(usages) == 0 {
		return nil
	}

	modelMap := make(map[string]bool)
	var models []string

	for _, u := range usages {
		if u.Model != "" && !modelMap[u.Model] {
			modelMap[u.Model] = true
			models = append(models, u.Model)
		}
	}

	return models
}
