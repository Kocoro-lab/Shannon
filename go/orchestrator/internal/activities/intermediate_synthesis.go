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

	sb.WriteString(`You are a research synthesis assistant. Your job is to preserve information fidelity while
organizing partial research results into an intermediate state used for coverage evaluation.

IMPORTANT: This is NOT the final user-facing report. Do NOT write a polished narrative. Optimize for:
1) lossless capture of concrete details and edge cases, 2) clear indexing/coverage, 3) actionable gaps.

## Your Goals (fidelity-first):
1. Extract and retain concrete facts (numbers, dates, names, versions, constraints, exceptions)
2. Deduplicate ONLY exact repeats; do NOT merge distinct facts into abstract summaries
3. Track what areas have evidence vs what areas are still missing
4. Be conservative on completeness; only claim high confidence when coverage is truly comprehensive

## Iteration Context:
`)
	sb.WriteString(fmt.Sprintf("- Current iteration: %d of %d\n", input.Iteration, input.MaxIterations))

	if input.PreviousSynthesis != "" {
		sb.WriteString("- Previous intermediate state exists (provided below); treat it as a working index, not ground truth\n")
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
  "synthesis": "A compact INDEX of the evidence so far (not a narrative). Use short bullet-like lines; keep numbers/dates/names verbatim.",
  "key_findings": ["Atomic fact 1 with concrete details", "Atomic fact 2 with concrete details", ...],
  "coverage_areas": ["Area label 1", "Area label 2", ...],
  "confidence_score": 0.7,
  "needs_more_research": true,
  "suggested_focus": ["Gap 1 to explore", "Gap 2 to explore"]
}

	## Guidelines:
	- key_findings MUST be factual and detail-rich. Prefer 20-60 items over 5-10 vague bullets.
	- Preserve exact values: "$12.3M", "2024-08", "v0.12.1", "SSE", "Temporal", etc.
	- Include important constraints/conditions (e.g., "only for X", "requires Y") as part of the fact.
	- Structural fidelity: if agent results contain structured artifacts (tables, checklists, JSON/YAML, code blocks), treat them as high-signal. Do NOT discard them.
	  - Prefer converting each table row / checklist item / key-value field into atomic key_findings entries (single-line) while preserving headers/field names and values verbatim.
	  - Use stable prefixes like "TABLE:", "CHECKLIST:", "KV:", "CODE:" so downstream steps can recognize them.
	- JSON safety: output MUST be valid JSON. Avoid raw newlines inside JSON strings; keep each key_findings entry as a single line (no markdown code fences).
	- Do NOT invent citations or URLs. Do NOT add a Sources section.
	- confidence_score: set >=0.85 ONLY if the major dimensions of the query have multiple concrete facts and there are no critical unknowns.
	- needs_more_research: true unless confidence is genuinely high or max iterations reached.
	- suggested_focus: only if needs_more_research is true; make each item a concrete missing question.
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

	// Truncation policy: preserve details when result count is small, but avoid exploding context.
	// Note: this is a heuristic; the LLM output is structured and should prioritize atomic facts.
	maxPerAgentChars := 3000
	switch {
	case len(input.AgentResults) <= 6:
		maxPerAgentChars = 8000
	case len(input.AgentResults) <= 12:
		maxPerAgentChars = 6000
	case len(input.AgentResults) <= 20:
		maxPerAgentChars = 4000
	default:
		maxPerAgentChars = 3000
	}

	for i, result := range input.AgentResults {
		sb.WriteString(fmt.Sprintf("### Agent %d (%s):\n", i+1, result.AgentID))
		if result.Success {
			response := result.Response
			if len(response) > maxPerAgentChars {
				response = response[:maxPerAgentChars] + "...[truncated]"
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
