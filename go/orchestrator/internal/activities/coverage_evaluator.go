package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	"go.temporal.io/sdk/activity"
)

// CoverageEvaluationInput is the input for coverage evaluation
type CoverageEvaluationInput struct {
	Query               string                 `json:"query"`
	ResearchDimensions  []ResearchDimension    `json:"research_dimensions,omitempty"` // From refinement (uses type from research_refine.go)
	CurrentSynthesis    string                 `json:"current_synthesis"`
	CoveredAreas        []string               `json:"covered_areas"`
	KeyFindings         []string               `json:"key_findings"`
	Iteration           int                    `json:"iteration"`
	MaxIterations       int                    `json:"max_iterations"`
	Context             map[string]interface{} `json:"context,omitempty"`
	ParentWorkflowID    string                 `json:"parent_workflow_id,omitempty"`
}

// CoverageEvaluationResult is the result of coverage evaluation
type CoverageEvaluationResult struct {
	OverallCoverage    float64           `json:"overall_coverage"`     // 0.0-1.0
	DimensionCoverage  map[string]float64 `json:"dimension_coverage"`  // Per-dimension coverage
	CriticalGaps       []CoverageGap     `json:"critical_gaps"`        // Must-fill gaps
	OptionalGaps       []CoverageGap     `json:"optional_gaps"`        // Nice-to-have gaps
	RecommendedAction  string            `json:"recommended_action"`   // "continue", "complete", "pivot"
	ConfidenceLevel    string            `json:"confidence_level"`     // "high", "medium", "low"
	ShouldContinue     bool              `json:"should_continue"`      // Whether to continue iterating
	Reasoning          string            `json:"reasoning"`            // Explanation for decision
	TokensUsed         int               `json:"tokens_used"`
	ModelUsed          string            `json:"model_used"`
	Provider           string            `json:"provider"`
	InputTokens        int               `json:"input_tokens"`
	OutputTokens       int               `json:"output_tokens"`
}

// CoverageGap represents a gap in research coverage
type CoverageGap struct {
	Area        string   `json:"area"`         // What area is missing
	Importance  string   `json:"importance"`   // "critical", "important", "minor"
	Questions   []string `json:"questions"`    // Specific questions to answer
	SourceTypes []string `json:"source_types"` // Suggested source types
}

// EvaluateCoverage assesses research coverage and identifies gaps
func (a *Activities) EvaluateCoverage(ctx context.Context, input CoverageEvaluationInput) (*CoverageEvaluationResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("EvaluateCoverage: starting",
		"query", truncateStr(input.Query, 100),
		"iteration", input.Iteration,
		"covered_areas", len(input.CoveredAreas),
		"dimensions", len(input.ResearchDimensions),
	)

	// Build evaluation prompt
	systemPrompt := buildCoverageEvaluationPrompt(input)
	userContent := buildCoverageEvaluationContent(input)

	// Call LLM service
	llmServiceURL := getenvDefault("LLM_SERVICE_URL", "http://llm-service:8000")
	url := fmt.Sprintf("%s/agent/query", llmServiceURL)

	reqBody := map[string]interface{}{
		"query":       userContent,
		"max_tokens":  8192, // Extended for detailed coverage analysis JSON
		"temperature": 0.2,
		"agent_id":    "coverage_evaluator",
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
	req.Header.Set("X-Agent-ID", "coverage_evaluator")
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

	result := &CoverageEvaluationResult{
		TokensUsed:   llmResp.TokensUsed,
		ModelUsed:    llmResp.ModelUsed,
		Provider:     llmResp.Provider,
		InputTokens:  llmResp.Metadata.InputTokens,
		OutputTokens: llmResp.Metadata.OutputTokens,
	}

	// Parse structured response
	if err := parseCoverageEvaluationResponse(llmResp.Response, result); err != nil {
		logger.Warn("EvaluateCoverage: failed to parse structured response",
			"error", err,
		)
		// Fallback: determine based on iteration
		result.OverallCoverage = 0.6
		result.ShouldContinue = input.Iteration < input.MaxIterations
		result.RecommendedAction = "continue"
		result.ConfidenceLevel = "medium"
		result.Reasoning = "Unable to parse coverage evaluation, defaulting to continue"
	}

	// === DETERMINISTIC GUARDRAILS ===
	// Override LLM judgments with deterministic safety rules for replay consistency
	originalShouldContinue := result.ShouldContinue
	originalReasoning := result.Reasoning

	// Rule 1: First iteration with low coverage → always continue
	if input.Iteration == 1 && result.OverallCoverage < 0.5 {
		result.ShouldContinue = true
		result.RecommendedAction = "continue"
		logger.Info("Guardrail: First iteration with low coverage, forcing continue",
			"coverage", result.OverallCoverage,
		)
	}

	// Rule 2: Critical gaps exist and iterations remain → always continue
	if len(result.CriticalGaps) > 0 && input.Iteration < input.MaxIterations {
		result.ShouldContinue = true
		result.RecommendedAction = "continue"
		logger.Info("Guardrail: Critical gaps exist, forcing continue",
			"critical_gaps", len(result.CriticalGaps),
			"iteration", input.Iteration,
		)
	}

	// Rule 3: Very low coverage → always continue (unless max iterations)
	if result.OverallCoverage < 0.3 && input.Iteration < input.MaxIterations {
		result.ShouldContinue = true
		result.RecommendedAction = "continue"
		logger.Info("Guardrail: Very low coverage, forcing continue",
			"coverage", result.OverallCoverage,
		)
	}

	// Rule 4: Suspiciously short synthesis → reduce coverage confidence
	if len(input.CurrentSynthesis) < 500 && result.OverallCoverage > 0.7 {
		logger.Warn("Guardrail: Synthesis too short for claimed high coverage",
			"synthesis_length", len(input.CurrentSynthesis),
			"claimed_coverage", result.OverallCoverage,
		)
		// Don't force continue, but flag low confidence
		result.ConfidenceLevel = "low"
	}

	// Rule 5: Max iterations reached → always stop
	if input.Iteration >= input.MaxIterations {
		result.ShouldContinue = false
		result.RecommendedAction = "complete"
		logger.Info("Guardrail: Max iterations reached, forcing complete",
			"iteration", input.Iteration,
			"max_iterations", input.MaxIterations,
		)
	}

	// Log if guardrails overrode LLM decision
	if originalShouldContinue != result.ShouldContinue {
		logger.Info("Guardrail override applied",
			"llm_decision", originalShouldContinue,
			"final_decision", result.ShouldContinue,
			"original_reasoning", truncateStr(originalReasoning, 100),
		)
	}

	logger.Info("EvaluateCoverage: complete",
		"overall_coverage", result.OverallCoverage,
		"critical_gaps", len(result.CriticalGaps),
		"should_continue", result.ShouldContinue,
		"recommended_action", result.RecommendedAction,
	)

	return result, nil
}

// buildCoverageEvaluationPrompt creates the system prompt for coverage evaluation
func buildCoverageEvaluationPrompt(input CoverageEvaluationInput) string {
	var sb strings.Builder

	sb.WriteString(`You are a research coverage evaluator. Your task is to assess how well the current
research covers the original query and identify any gaps.

## Your Goals:
1. Evaluate overall research coverage (0.0-1.0)
2. Assess coverage per research dimension if provided
3. Identify critical gaps that MUST be filled
4. Identify optional gaps that would improve quality
5. Recommend whether to continue, complete, or pivot

## CRITICAL: Distinguishing Real Coverage from Acknowledged Gaps

**Real coverage** means ACTUAL SUBSTANTIVE INFORMATION was found and presented:
- Specific facts, figures, dates, names
- Verified claims with supporting evidence
- Concrete details about the target entity

**NOT real coverage** (these should be counted as GAPS):
- Statements like "we could not find information about X"
- "No data available for Y"
- "This remains unverified/unknown"
- Using competitor/industry information as a substitute
- Generic market context without target-specific data

IMPORTANT: If the synthesis says "we don't know X about [target], but here's what competitors do" -
that is a GAP for [target], not coverage. Do NOT count proxy/substitute information as coverage.

## Evaluation Criteria:
- CRITICAL gap: Primary entity information missing (founding date, products, team, financials)
- IMPORTANT gap: Missing context that significantly improves understanding
- MINOR gap: Nice-to-have information for completeness

## Decision Logic:
- coverage >= 0.85 + no critical gaps + actual substantive info → recommend "complete"
- If synthesis is mostly "we don't know" statements → coverage should be LOW (<0.4)
- If target entity has < 3 verifiable facts → critical gaps exist → recommend "continue"
- coverage >= 0.6 + critical gaps exist + iterations left → recommend "continue"
- coverage < 0.6 + many gaps → recommend "continue"
- max iterations reached → recommend "complete" regardless

`)

	sb.WriteString(fmt.Sprintf("Current iteration: %d of %d\n\n", input.Iteration, input.MaxIterations))

	sb.WriteString(`## Response Format:
Return a JSON object:
{
  "overall_coverage": 0.75,
  "dimension_coverage": {"dimension1": 0.8, "dimension2": 0.6},
  "critical_gaps": [
    {"area": "...", "importance": "critical", "questions": ["..."], "source_types": ["official", "news"]}
  ],
  "optional_gaps": [
    {"area": "...", "importance": "minor", "questions": ["..."], "source_types": ["aggregator"]}
  ],
  "recommended_action": "continue",
  "confidence_level": "medium",
  "should_continue": true,
  "reasoning": "..."
}
`)

	return sb.String()
}

// buildCoverageEvaluationContent builds user content for coverage evaluation
func buildCoverageEvaluationContent(input CoverageEvaluationInput) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Original Query:\n%s\n\n", input.Query))

	if len(input.ResearchDimensions) > 0 {
		sb.WriteString("## Expected Research Dimensions:\n")
		for _, dim := range input.ResearchDimensions {
			sb.WriteString(fmt.Sprintf("- **%s** (priority: %s)\n", dim.Dimension, dim.Priority))
			for _, q := range dim.Questions {
				sb.WriteString(fmt.Sprintf("  - %s\n", q))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Current Coverage:\n")
	if len(input.CoveredAreas) > 0 {
		for _, area := range input.CoveredAreas {
			sb.WriteString(fmt.Sprintf("- %s\n", area))
		}
	} else {
		sb.WriteString("(No areas explicitly covered yet)\n")
	}
	sb.WriteString("\n")

	if len(input.KeyFindings) > 0 {
		sb.WriteString("## Key Findings So Far:\n")
		for _, finding := range input.KeyFindings {
			sb.WriteString(fmt.Sprintf("- %s\n", finding))
		}
		sb.WriteString("\n")
	}

	if input.CurrentSynthesis != "" {
		synthesis := input.CurrentSynthesis
		if len(synthesis) > 2000 {
			synthesis = synthesis[:2000] + "...[truncated]"
		}
		sb.WriteString(fmt.Sprintf("## Current Synthesis:\n%s\n", synthesis))
	}

	return sb.String()
}

// parseCoverageEvaluationResponse parses the LLM response
func parseCoverageEvaluationResponse(response string, result *CoverageEvaluationResult) error {
	// Find JSON in response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")

	if start == -1 || end == -1 || end <= start {
		return fmt.Errorf("no JSON object found in response")
	}

	jsonStr := response[start : end+1]

	var parsed struct {
		OverallCoverage   float64            `json:"overall_coverage"`
		DimensionCoverage map[string]float64 `json:"dimension_coverage"`
		CriticalGaps      []CoverageGap      `json:"critical_gaps"`
		OptionalGaps      []CoverageGap      `json:"optional_gaps"`
		RecommendedAction string             `json:"recommended_action"`
		ConfidenceLevel   string             `json:"confidence_level"`
		ShouldContinue    bool               `json:"should_continue"`
		Reasoning         string             `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	result.OverallCoverage = parsed.OverallCoverage
	result.DimensionCoverage = parsed.DimensionCoverage
	result.CriticalGaps = parsed.CriticalGaps
	result.OptionalGaps = parsed.OptionalGaps
	result.RecommendedAction = parsed.RecommendedAction
	result.ConfidenceLevel = parsed.ConfidenceLevel
	result.ShouldContinue = parsed.ShouldContinue
	result.Reasoning = parsed.Reasoning

	return nil
}
