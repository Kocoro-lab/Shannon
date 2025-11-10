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
	"go.uber.org/zap"
)

// RefineResearchQueryInput is the input for refining vague research queries
type RefineResearchQueryInput struct {
	Query   string         `json:"query"`
	Context map[string]any `json:"context"`
}

// RefineResearchQueryResult contains the expanded research scope
type RefineResearchQueryResult struct {
    OriginalQuery string   `json:"original_query"`
    RefinedQuery  string   `json:"refined_query"`
    ResearchAreas []string `json:"research_areas"`
    Rationale     string   `json:"rationale"`
    TokensUsed    int      `json:"tokens_used"`
    ModelUsed     string   `json:"model_used,omitempty"`
    Provider      string   `json:"provider,omitempty"`
    // Entity disambiguation and search guidance
    CanonicalName      string   `json:"canonical_name,omitempty"`
    ExactQueries       []string `json:"exact_queries,omitempty"`
    OfficialDomains    []string `json:"official_domains,omitempty"`
    DisambiguationTerms []string `json:"disambiguation_terms,omitempty"`
}

// RefineResearchQuery expands vague queries into structured research plans
// This is called before decomposition in ResearchWorkflow to clarify scope.
func (a *Activities) RefineResearchQuery(ctx context.Context, in RefineResearchQueryInput) (*RefineResearchQueryResult, error) {
	base := os.Getenv("LLM_SERVICE_URL")
	if base == "" {
		base = "http://llm-service:8000"
	}
	url := fmt.Sprintf("%s/agent/query", base)

	// Build prompt for query refinement
    refinementPrompt := fmt.Sprintf(`You are a research query expansion expert.

IMPORTANT: This is the PLANNING stage only. Plan first; do NOT start writing the final report or conducting searches. Return ONLY a structured plan.

Your task is to take a vague or broad query and expand it into a comprehensive research plan.

Original query: %s

Analyze this query and expand it into:
1. A refined, clearer version of the query (<= 200 characters)
2. Specific research areas that should be explored (5–7 items, specific and non‑overlapping)
3. Brief rationale for the expansion (1–2 sentences)

Constraints:
- Do NOT include sources, URLs, or citations.
- Output JSON ONLY; no prose before/after.
- If the query mentions a company or product, PRESERVE the exact string (do not split/normalize). Provide exact, quoted search queries.
- Provide domains and disambiguation terms to avoid entity mix-ups (e.g., wrong 'Mind' companies).

For example, if the query is "analyze company X":
- Refined query: "Comprehensive analysis of company X including market position, competitive landscape, leadership, and product portfolio"
- Research areas: ["Company X profile and history", "X's competitors and market share", "X's board members and leadership team", "X's products compared to market alternatives", "Financial performance and growth metrics"]
- Rationale: "A comprehensive company analysis requires examining multiple dimensions: the company itself, its competitive context, leadership quality, product differentiation, and financial health."

Respond in JSON format:
{
  "refined_query": "...",
  "research_areas": ["...", "..."],
  "rationale": "...",
  "canonical_name": "...",               // e.g., "Acme Analytics"
  "exact_queries": ["\"Acme Analytics\"", "\"Acme Analytics Inc.\"", "\"AcmeAnalytics\""],
  "official_domains": ["acme.com", "acme-analytics.com"],
  "disambiguation_terms": ["software analytics", "Japan", "SaaS"]
}`, in.Query)

    // Prepare request body. Role should be passed via context, not top-level.
    ctxMap := in.Context
    if ctxMap == nil {
        ctxMap = map[string]any{}
    }
    ctxMap["role"] = "research_refiner"
    // Request JSON-structured output when provider supports it; non-supporting providers will ignore
    ctxMap["response_format"] = map[string]any{"type": "json_object"}

    reqBody := map[string]any{
        "query":   refinementPrompt,
        "context": ctxMap,
    }

	body, err := json.Marshal(reqBody)
    if err != nil {
        ometrics.RefinementErrors.Inc()
        return nil, fmt.Errorf("failed to marshal request: %w", err)
    }

	// HTTP client with workflow interceptor for tracing
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
    if err != nil {
        ometrics.RefinementErrors.Inc()
        return nil, fmt.Errorf("failed to create request: %w", err)
    }
	req.Header.Set("Content-Type", "application/json")

    resp, err := client.Do(req)
    if err != nil {
        ometrics.RefinementErrors.Inc()
        return nil, fmt.Errorf("failed to call LLM service: %w", err)
    }
	defer resp.Body.Close()

    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        ometrics.RefinementErrors.Inc()
        return nil, fmt.Errorf("LLM service returned status %d", resp.StatusCode)
    }

	// Parse response
	var llmResp struct {
		Response   string `json:"response"`
		TokensUsed int    `json:"tokens_used"`
		ModelUsed  string `json:"model_used"`
		Provider   string `json:"provider"`
	}
    if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
        ometrics.RefinementErrors.Inc()
        return nil, fmt.Errorf("failed to decode response: %w", err)
    }

	// Parse JSON from response (strip markdown fences if present)
    responseText := llmResp.Response
    responseText = strings.TrimSpace(responseText)
    if strings.HasPrefix(responseText, "```json") {
        responseText = strings.TrimPrefix(responseText, "```json")
        responseText = strings.TrimPrefix(responseText, "```")
        if idx := strings.LastIndex(responseText, "```"); idx != -1 {
            responseText = responseText[:idx]
        }
        responseText = strings.TrimSpace(responseText)
    } else if strings.HasPrefix(responseText, "```") {
        responseText = strings.TrimPrefix(responseText, "```")
        if idx := strings.LastIndex(responseText, "```"); idx != -1 {
            responseText = responseText[:idx]
        }
        responseText = strings.TrimSpace(responseText)
    }

    var refinedData struct {
        RefinedQuery       string   `json:"refined_query"`
        ResearchAreas      []string `json:"research_areas"`
        Rationale          string   `json:"rationale"`
        CanonicalName      string   `json:"canonical_name"`
        ExactQueries       []string `json:"exact_queries"`
        OfficialDomains    []string `json:"official_domains"`
        DisambiguationTerms []string `json:"disambiguation_terms"`
    }
    if err := json.Unmarshal([]byte(responseText), &refinedData); err != nil {
        // If JSON parsing fails, fallback to using original query
        a.logger.Warn("Failed to parse refinement JSON, using original query",
            zap.Error(err),
            zap.String("response", llmResp.Response),
        )
        return &RefineResearchQueryResult{
            OriginalQuery: in.Query,
            RefinedQuery:  in.Query,
            ResearchAreas: []string{in.Query},
            Rationale:     "Query refinement failed, using original query",
            TokensUsed:    llmResp.TokensUsed,
            ModelUsed:     llmResp.ModelUsed,
            Provider:      llmResp.Provider,
        }, nil
    }

    result := &RefineResearchQueryResult{
        OriginalQuery: in.Query,
        RefinedQuery:  refinedData.RefinedQuery,
        ResearchAreas: refinedData.ResearchAreas,
        Rationale:     refinedData.Rationale,
        TokensUsed:    llmResp.TokensUsed,
        ModelUsed:     llmResp.ModelUsed,
        Provider:      llmResp.Provider,
        CanonicalName: refinedData.CanonicalName,
        ExactQueries:  refinedData.ExactQueries,
        OfficialDomains: refinedData.OfficialDomains,
        DisambiguationTerms: refinedData.DisambiguationTerms,
    }

    // Tiny fallback: if canonical_name is empty, derive from the first exact_queries entry (strip quotes)
    if result.CanonicalName == "" && len(result.ExactQueries) > 0 {
        candidate := result.ExactQueries[0]
        // Remove surrounding quotes if present (e.g., "\"Acme Analytics\"")
        for len(candidate) >= 2 {
            if (candidate[0] == '"' && candidate[len(candidate)-1] == '"') ||
               (candidate[0] == '\'' && candidate[len(candidate)-1] == '\'') {
                candidate = candidate[1:len(candidate)-1]
                continue
            }
            break
        }
        if candidate != "" {
            result.CanonicalName = candidate
        }
    }

    // Record latency
    ometrics.RefinementLatency.Observe(time.Since(start).Seconds())

	return result, nil
}
