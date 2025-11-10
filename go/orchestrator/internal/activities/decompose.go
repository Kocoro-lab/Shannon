package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	ometrics "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
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
    // Usage and provider/model metadata (optional)
    InputTokens  int    `json:"input_tokens"`
    OutputTokens int    `json:"output_tokens"`
    TokensUsed   int    `json:"total_tokens"`
    CostUSD      float64 `json:"cost_usd"`
    ModelUsed    string `json:"model_used"`
    Provider     string `json:"provider"`
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
	// Timeout configurable via DECOMPOSE_TIMEOUT_SECONDS (default: 30s)
	timeoutSec := 30
	if v := os.Getenv("DECOMPOSE_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeoutSec = n
		}
	}
	// Increased default timeout to 30s to accommodate complex system prompts and LLM planning
	client := &http.Client{
		Timeout:   time.Duration(timeoutSec) * time.Second,
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

	// TODO: Assign personas to each subtask when personas package is complete
	// logger := activity.GetLogger(ctx)
	// for i := range out.Subtasks {
	//     personaID := personas.SelectPersona(out.Subtasks[i].Description, out.ComplexityScore)
	//     out.Subtasks[i].SuggestedPersona = personaID
	//     logger.Debug("Assigned persona to subtask",
	//         "subtask_id", out.Subtasks[i].ID,
	//         "description", out.Subtasks[i].Description,
	//         "persona", personaID)
	// }

	ometrics.DecompositionLatency.Observe(time.Since(start).Seconds())
	return out, nil
}
