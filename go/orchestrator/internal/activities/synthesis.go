package activities

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "strings"
    "time"

    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
    "go.temporal.io/sdk/activity"
    "go.uber.org/zap"
)

// SynthesizeResults synthesizes results from multiple agents (baseline concatenation)
func SynthesizeResults(ctx context.Context, input SynthesisInput) (SynthesisResult, error) {
    return simpleSynthesis(ctx, input)
}

// SynthesizeResultsLLM synthesizes results using the LLM service, with fallback to simple synthesis
func SynthesizeResultsLLM(ctx context.Context, input SynthesisInput) (SynthesisResult, error) {
    // Use activity logger for Temporal correlation
    activity.GetLogger(ctx).Info("Synthesizing results using LLM",
        "query", input.Query,
        "num_results", len(input.AgentResults),
    )
    // Use zap.L() for places needing *zap.Logger
    logger := zap.L()

    if len(input.AgentResults) == 0 {
        return SynthesisResult{}, fmt.Errorf("no agent results to synthesize")
    }

    // LLM-first; fallback to simple synthesis on any failure

    // Build compact messages for /completions
    sys := "You are a synthesis assistant. Merge multiple agent responses into a single, coherent answer. " +
        "Be concise, remove duplicates, resolve conflicts, and present a clear final answer."

    // Check for reflection feedback in context
    if input.Context != nil {
        if feedback, ok := input.Context["reflection_feedback"].(string); ok && feedback != "" {
            sys += fmt.Sprintf("\n\nIMPORTANT: The previous response was evaluated and needs improvement. Feedback: %s", feedback)
        }
        if _, ok := input.Context["improvement_needed"].(bool); ok {
            sys += "\n\nPlease address the feedback and provide an improved response."
        }
    }

    // Limit included agent outputs to keep prompt small
    const maxAgents = 6
    const maxPerAgentChars = 800

    var b strings.Builder
    fmt.Fprintf(&b, "Original task: %s\n\n", input.Query)

    // Include previous response if this is a reflection retry
    if input.Context != nil {
        if prevResponse, ok := input.Context["previous_response"].(string); ok && prevResponse != "" {
            fmt.Fprintf(&b, "Previous response (needs improvement):\n%s\n\n", prevResponse)
        }
    }
    fmt.Fprintf(&b, "Agent results (%d total, showing up to %d):\n", len(input.AgentResults), maxAgents)
    count := 0
    for _, r := range input.AgentResults {
        if !r.Success || r.Response == "" {
            continue
        }
        trimmed := r.Response
        if len(trimmed) > maxPerAgentChars {
            trimmed = trimmed[:maxPerAgentChars] + "..."
        }
        fmt.Fprintf(&b, "\n- [%s]: %s\n", r.AgentID, trimmed)
        count++
        if count >= maxAgents {
            break
        }
    }

    messages := []map[string]string{
        {"role": "system", "content": sys},
        {"role": "user", "content": b.String()},
    }

    base := getenv("LLM_SERVICE_URL", "http://llm-service:8000")
    reqBody := map[string]interface{}{
        "messages":     messages,
        "model_tier":   "small",
        "temperature":  0.2,
        "max_tokens":   800,
    }
    buf, _ := json.Marshal(reqBody)
    url := base + "/completions/"

    httpClient := &http.Client{
        Timeout:   8 * time.Second,
        Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
    }

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
    if err != nil {
        logger.Warn("LLM synthesis: request build failed, falling back", zap.Error(err))
        return simpleSynthesis(ctx, input)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := httpClient.Do(req)
    if err != nil {
        logger.Warn("LLM synthesis: HTTP error, falling back", zap.Error(err))
        return simpleSynthesis(ctx, input)
    }
    defer resp.Body.Close()
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        logger.Warn("LLM synthesis: non-2xx, falling back", zap.Int("status", resp.StatusCode))
        return simpleSynthesis(ctx, input)
    }

    var out struct {
        Completion string                 `json:"completion"`
        Usage      map[string]interface{} `json:"usage"`
        ModelUsed  string                 `json:"model_used"`
        Provider   string                 `json:"provider"`
        CacheHit   bool                   `json:"cache_hit"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        logger.Warn("LLM synthesis: decode error, falling back", zap.Error(err))
        return simpleSynthesis(ctx, input)
    }
    outCompletion := out.Completion
    model := out.ModelUsed
    cacheHit := out.CacheHit
    tokens := 0
    if out.Usage != nil {
        if v, ok := out.Usage["total_tokens"]; ok {
            switch t := v.(type) {
            case float64:
                tokens = int(t)
            case int:
                tokens = t
            }
        }
    }

    logger.Info("Synthesis (LLM) completed",
        zap.Int("tokens_used", tokens),
        zap.String("model", model),
        zap.Bool("cache_hit", cacheHit),
    )

    return SynthesisResult{
        FinalResult:  outCompletion,
        TokensUsed:   tokens,
    }, nil
}

// simpleSynthesis concatenates successful agent outputs with light formatting
func simpleSynthesis(ctx context.Context, input SynthesisInput) (SynthesisResult, error) {
    logger := zap.L()
    logger.Info("Synthesizing results (simple)",
        zap.String("query", input.Query),
        zap.Int("num_results", len(input.AgentResults)),
    )

    if len(input.AgentResults) == 0 {
        return SynthesisResult{}, fmt.Errorf("no agent results to synthesize")
    }

    var resultParts []string
    totalTokens := 0

    for _, result := range input.AgentResults {
        if result.Success && result.Response != "" {
            // Clean up raw outputs for better readability
            cleaned := cleanAgentOutput(result.Response)
            if cleaned != "" {
                resultParts = append(resultParts, cleaned)
                totalTokens += result.TokensUsed
            }
        }
    }

    if len(resultParts) == 0 {
        return SynthesisResult{}, fmt.Errorf("no successful agent results")
    }

    // Combine results without exposing internal details
    finalResult := strings.Join(resultParts, "\n\n")

    logger.Info("Synthesis (simple) completed",
        zap.Int("total_tokens", totalTokens),
        zap.Int("successful_agents", len(resultParts)),
    )

    return SynthesisResult{
        FinalResult: finalResult,
        TokensUsed:  totalTokens,
    }, nil
}

// cleanAgentOutput processes raw agent outputs to be more user-friendly
func cleanAgentOutput(response string) string {
    // Try to parse as JSON array (common for web_search results)
    var results []map[string]interface{}
    if err := json.Unmarshal([]byte(response), &results); err == nil && len(results) > 0 {
        // Format search results as a readable list
        var formatted []string
        for i, result := range results {
            if i >= 5 { // Limit to top 5 results
                break
            }
            title, _ := result["title"].(string)
            url, _ := result["url"].(string)
            if title != "" && url != "" {
                formatted = append(formatted, fmt.Sprintf("• %s\n  %s", title, url))
            }
        }
        if len(formatted) > 0 {
            return "Research findings:\n" + strings.Join(formatted, "\n")
        }
    }
    
    // Return as-is if not JSON or already clean text
    return response
}
