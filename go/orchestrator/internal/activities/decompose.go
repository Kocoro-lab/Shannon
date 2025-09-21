package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	ometrics "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/personas"
	"go.temporal.io/sdk/activity"
	"go.uber.org/zap"
)

// DecompositionInput is the input for DecomposeTask activity
type DecompositionInput struct {
	Query          string                 `json:"query"`
	Context        map[string]interface{} `json:"context"`
	AvailableTools []string               `json:"available_tools"`
}

// DecompositionResult is the result from the Python LLM service
type DecompositionResult struct {
	Mode                 string    `json:"mode"`
	ComplexityScore      float64   `json:"complexity_score"`
	Subtasks             []Subtask `json:"subtasks"`
	TotalEstimatedTokens int       `json:"total_estimated_tokens"`
	// Extended planning schema (plan_schema_v2)
	ExecutionStrategy string         `json:"execution_strategy"`
	AgentTypes        []string       `json:"agent_types"`
	ConcurrencyLimit  int            `json:"concurrency_limit"`
	TokenEstimates    map[string]int `json:"token_estimates"`
	// Cognitive routing fields for intelligent strategy selection
	CognitiveStrategy string  `json:"cognitive_strategy"`
	Confidence        float64 `json:"confidence"`
	FallbackStrategy  string  `json:"fallback_strategy"`
}

// DecomposeTask calls the LLM service to decompose a task into subtasks
func (a *Activities) DecomposeTask(ctx context.Context, in DecompositionInput) (DecompositionResult, error) {
	base := os.Getenv("LLM_SERVICE_URL")
	if base == "" {
		base = "http://llm-service:8000"
	}
	url := fmt.Sprintf("%s/agent/decompose", base)

	body, _ := json.Marshal(map[string]interface{}{
		"query":   in.Query,
		"context": in.Context,
		"tools":   in.AvailableTools, // Fixed: "tools" not "available_tools"
		"mode":    "standard",
	})

	// HTTP client with workflow interceptor to inject headers
	// In tests, this might not be in a Temporal context, so we handle it gracefully
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		ometrics.DecompositionErrors.Inc()
		return DecompositionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		// Fallback: empty subtasks to allow simple execution path
		ometrics.DecompositionErrors.Inc()
		return DecompositionResult{Mode: "standard", ComplexityScore: 0.5, Subtasks: nil, TotalEstimatedTokens: 0}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		ometrics.DecompositionErrors.Inc()
		return DecompositionResult{Mode: "standard", ComplexityScore: 0.5, Subtasks: nil, TotalEstimatedTokens: 0}, nil
	}

	var out DecompositionResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		ometrics.DecompositionErrors.Inc()
		return DecompositionResult{Mode: "standard", ComplexityScore: 0.5, Subtasks: nil, TotalEstimatedTokens: 0}, nil
	}

	// Assign personas to each subtask
	logger := activity.GetLogger(ctx)
	for i := range out.Subtasks {
		personaID, err := assignPersonaToSubtask(ctx, out.Subtasks[i].Description, out.ComplexityScore)
		if err != nil {
			logger.Warn("Failed to assign persona to subtask, using generalist",
				zap.String("subtask_id", out.Subtasks[i].ID),
				zap.Error(err))
			personaID = "generalist"
		}
		out.Subtasks[i].SuggestedPersona = personaID
		logger.Debug("Assigned persona to subtask",
			zap.String("subtask_id", out.Subtasks[i].ID),
			zap.String("description", out.Subtasks[i].Description),
			zap.String("persona", personaID))
	}

	ometrics.DecompositionLatency.Observe(time.Since(start).Seconds())
	return out, nil
}

// assignPersonaToSubtask assigns a persona to a subtask based on its description
func assignPersonaToSubtask(ctx context.Context, description string, complexityScore float64) (string, error) {
	// Get the global persona manager (this would be initialized at startup)
	manager := getPersonaManager()
	if manager == nil {
		return "generalist", fmt.Errorf("persona manager not available")
	}

	// Create selection request
	req := &personas.SelectionRequest{
		Description:     description,
		ComplexityScore: complexityScore,
		TaskType:        inferTaskType(description),
	}

	// Select persona
	result, err := manager.SelectPersona(ctx, req)
	if err != nil {
		return "generalist", err
	}

	return result.PersonaID, nil
}

// inferTaskType infers the task type from the description
func inferTaskType(description string) string {
	description = strings.ToLower(description)
	
	if strings.Contains(description, "code") || strings.Contains(description, "implement") || 
	   strings.Contains(description, "debug") || strings.Contains(description, "program") {
		return "coding"
	}
	
	if strings.Contains(description, "research") || strings.Contains(description, "search") || 
	   strings.Contains(description, "find") || strings.Contains(description, "investigate") {
		return "research"
	}
	
	if strings.Contains(description, "analyze") || strings.Contains(description, "data") || 
	   strings.Contains(description, "statistics") || strings.Contains(description, "chart") {
		return "analysis"
	}
	
	return "general"
}

// Global persona manager instance
var globalPersonaManager personas.PersonaManager

// getPersonaManager returns the global persona manager instance
func getPersonaManager() personas.PersonaManager {
	return globalPersonaManager
}

// SetPersonaManager sets the global persona manager instance
func SetPersonaManager(manager personas.PersonaManager) {
	globalPersonaManager = manager
}
