package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	"go.temporal.io/sdk/activity"
)

// IntermediateSynthesisInput is the input for intermediate synthesis between iterations
type IntermediateSynthesisInput struct {
	Query             string                   `json:"query"`
	Iteration         int                      `json:"iteration"`
	MaxIterations     int                      `json:"max_iterations"`
	AgentResults      []AgentExecutionResult   `json:"agent_results"`
	PreviousSynthesis string                   `json:"previous_synthesis,omitempty"` // From prior iteration
	CoverageGaps      []string                 `json:"coverage_gaps,omitempty"`       // Gaps identified so far
	Context           map[string]interface{}   `json:"context,omitempty"`
	ParentWorkflowID  string                   `json:"parent_workflow_id,omitempty"`
}

// IntermediateSynthesisResult is the result of intermediate synthesis
type IntermediateSynthesisResult struct {
	Synthesis         string   `json:"synthesis"`           // Combined understanding so far
	KeyFindings       []string `json:"key_findings"`        // Extracted key findings
	CoverageAreas     []string `json:"coverage_areas"`      // Areas covered so far
	ConfidenceScore   float64  `json:"confidence_score"`    // 0.0-1.0 confidence in completeness
	NeedsMoreResearch bool     `json:"needs_more_research"` // Whether another iteration is needed
	SuggestedFocus    []string `json:"suggested_focus"`     // Suggested areas for next iteration
	TokensUsed        int      `json:"tokens_used"`
	ModelUsed         string   `json:"model_used"`
	Provider          string   `json:"provider"`
	InputTokens       int      `json:"input_tokens"`
	OutputTokens      int      `json:"output_tokens"`
}

// IntermediateSynthesis combines partial results between research iterations
func (a *Activities) IntermediateSynthesis(ctx context.Context, input IntermediateSynthesisInput) (*IntermediateSynthesisResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("IntermediateSynthesis: starting",
		"query", truncateStr(input.Query, 100),
		"iteration", input.Iteration,
		"agent_results", len(input.AgentResults),
		"has_previous", input.PreviousSynthesis != "",
	)

	// Build system prompt for intermediate synthesis
	systemPrompt := buildIntermediateSynthesisPrompt(input)
	userContent := buildAgentResultsContent(input)

	// Call LLM service
	llmServiceURL := getenvDefault("LLM_SERVICE_URL", "http://llm-service:8000")
	url := fmt.Sprintf("%s/agent/query", llmServiceURL)

	reqBody := map[string]interface{}{
		"query":       userContent,
		"max_tokens":  8192, // Extended for comprehensive intermediate synthesis
		"temperature": 0.3,
		"agent_id":    "intermediate_synthesis",
		"model_tier":  "small",
		"context": map[string]interface{}{
			"system_prompt":      systemPrompt,
			"parent_workflow_id": input.ParentWorkflowID,
		},
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	client := &http.Client{
		Timeout:   120 * time.Second, // Extended for LLM processing time
		Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(reqJSON)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "intermediate_synthesis")
	if input.ParentWorkflowID != "" {
		req.Header.Set("X-Workflow-ID", input.ParentWorkflowID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LLM service call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d from LLM service", resp.StatusCode)
	}

	// Parse response
	var llmResp struct {
		Success  bool   `json:"success"`
		Response string `json:"response"`
		Metadata struct {
			InputTokens  int     `json:"input_tokens"`
			OutputTokens int     `json:"output_tokens"`
			CostUSD      float64 `json:"cost_usd"`
		} `json:"metadata"`
		TokensUsed int    `json:"tokens_used"`
		ModelUsed  string `json:"model_used"`
		Provider   string `json:"provider"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	result := &IntermediateSynthesisResult{
		TokensUsed:   llmResp.TokensUsed,
		ModelUsed:    llmResp.ModelUsed,
		Provider:     llmResp.Provider,
		InputTokens:  llmResp.Metadata.InputTokens,
		OutputTokens: llmResp.Metadata.OutputTokens,
	}

	// Try to parse structured response
	if err := parseIntermediateSynthesisResponse(llmResp.Response, result); err != nil {
		logger.Warn("IntermediateSynthesis: failed to parse structured response, using raw",
			"error", err,
		)
		result.Synthesis = llmResp.Response
		result.ConfidenceScore = 0.5
		result.NeedsMoreResearch = input.Iteration < input.MaxIterations
	}

	logger.Info("IntermediateSynthesis: complete",
		"confidence", result.ConfidenceScore,
		"needs_more_research", result.NeedsMoreResearch,
		"key_findings", len(result.KeyFindings),
		"coverage_areas", len(result.CoverageAreas),
	)

	return result, nil
}

// buildIntermediateSynthesisPrompt creates the system prompt for intermediate synthesis
func buildIntermediateSynthesisPrompt(input IntermediateSynthesisInput) string {
	var sb strings.Builder

	sb.WriteString(`You are a research synthesis assistant. Your task is to combine partial research results
into a coherent intermediate synthesis.

## Your Goals:
1. Combine findings from multiple research agents into a unified understanding
2. Identify key findings and confidence levels
3. Assess coverage - what areas have been well-researched vs gaps
4. Determine if more research iterations are needed

## Iteration Context:
`)
	sb.WriteString(fmt.Sprintf("- Current iteration: %d of %d\n", input.Iteration, input.MaxIterations))

	if input.PreviousSynthesis != "" {
		sb.WriteString("- Building on previous synthesis (provided below)\n")
	} else {
		sb.WriteString("- First iteration - establishing baseline understanding\n")
	}

	if len(input.CoverageGaps) > 0 {
		sb.WriteString(fmt.Sprintf("- Known gaps to address: %s\n", strings.Join(input.CoverageGaps, ", ")))
	}

	sb.WriteString(`
## Response Format:
Return a JSON object with these fields:
{
  "synthesis": "Combined understanding so far...",
  "key_findings": ["Finding 1", "Finding 2", ...],
  "coverage_areas": ["Area 1", "Area 2", ...],
  "confidence_score": 0.7,
  "needs_more_research": true,
  "suggested_focus": ["Gap 1 to explore", "Gap 2 to explore"]
}

## Guidelines:
- confidence_score: 0.0-1.0, set to 0.8+ if you have comprehensive coverage
- needs_more_research: false if confidence >= 0.8 or if max iterations reached
- suggested_focus: only if needs_more_research is true
- Be concise but thorough in the synthesis
`)

	return sb.String()
}

// buildAgentResultsContent builds the user content from agent results
func buildAgentResultsContent(input IntermediateSynthesisInput) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Original Query:\n%s\n\n", input.Query))

	if input.PreviousSynthesis != "" {
		sb.WriteString(fmt.Sprintf("## Previous Synthesis:\n%s\n\n", input.PreviousSynthesis))
	}

	sb.WriteString("## New Agent Results:\n\n")
	for i, result := range input.AgentResults {
		sb.WriteString(fmt.Sprintf("### Agent %d (%s):\n", i+1, result.AgentID))
		if result.Success {
			response := result.Response
			if len(response) > 10000 {
				response = response[:10000] + "...[truncated]"
			}
			sb.WriteString(response)
		} else {
			sb.WriteString(fmt.Sprintf("(Failed: %s)", result.Error))
		}
		sb.WriteString("\n\n---\n\n")
	}

	return sb.String()
}

// parseIntermediateSynthesisResponse parses the LLM response into structured result
func parseIntermediateSynthesisResponse(response string, result *IntermediateSynthesisResult) error {
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")

	if start == -1 || end == -1 || end <= start {
		return fmt.Errorf("no JSON object found in response")
	}

	jsonStr := response[start : end+1]

	var parsed struct {
		Synthesis         string   `json:"synthesis"`
		KeyFindings       []string `json:"key_findings"`
		CoverageAreas     []string `json:"coverage_areas"`
		ConfidenceScore   float64  `json:"confidence_score"`
		NeedsMoreResearch bool     `json:"needs_more_research"`
		SuggestedFocus    []string `json:"suggested_focus"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	result.Synthesis = parsed.Synthesis
	result.KeyFindings = parsed.KeyFindings
	result.CoverageAreas = parsed.CoverageAreas
	result.ConfidenceScore = parsed.ConfidenceScore
	result.NeedsMoreResearch = parsed.NeedsMoreResearch
	result.SuggestedFocus = parsed.SuggestedFocus

	return nil
}

// truncateStr truncates a string to maxLen
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// getenvDefault gets an environment variable with a default value
func getenvDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
