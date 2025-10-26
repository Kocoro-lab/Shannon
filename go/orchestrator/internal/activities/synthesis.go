package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	"go.temporal.io/sdk/activity"
	"go.uber.org/zap"
)

// SynthesizeResults synthesizes results from multiple agents (baseline concatenation)
func SynthesizeResults(ctx context.Context, input SynthesisInput) (SynthesisResult, error) {
	// Emit synthesis start once for the simple (non-LLM) path
	wfID := input.ParentWorkflowID
	if wfID == "" {
		if info := activity.GetInfo(ctx); info.WorkflowExecution.ID != "" {
			wfID = info.WorkflowExecution.ID
		}
	}
	if wfID != "" {
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventDataProcessing),
			AgentID:    "synthesis",
			Message:    MsgCombiningResults(),
			Timestamp:  time.Now(),
		})
	}
	// Compute result without emitting completion here, then emit once
	res, err := simpleSynthesisNoEvents(ctx, input)
	if err != nil {
		return res, err
	}
	// Emit final synthesized content and completion
	if wfID != "" {
		// LLM_OUTPUT with final content (simple path)
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventLLMOutput),
			AgentID:    "synthesis",
			Message:    truncateQuery(res.FinalResult, 10000),
			Timestamp:  time.Now(),
		})
		// Lightweight tokens summary
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventDataProcessing),
			AgentID:    "synthesis",
			Message:    fmt.Sprintf("~%d tokens", res.TokensUsed),
			Timestamp:  time.Now(),
		})
	}
    // Emit synthesis completed (DATA_PROCESSING)
    if wfID != "" {
        streaming.Get().Publish(wfID, streaming.Event{
            WorkflowID: wfID,
            Type:       string(StreamEventDataProcessing),
            AgentID:    "synthesis",
            Message:    "Final answer ready",
            Timestamp:  time.Now(),
        })
    }
	return res, nil
}

// SynthesizeResultsLLM synthesizes results using the LLM service, with fallback to simple synthesis
func SynthesizeResultsLLM(ctx context.Context, input SynthesisInput) (SynthesisResult, error) {
	// Use activity logger for Temporal correlation
	activity.GetLogger(ctx).Info("Synthesizing results using LLM",
		"query", input.Query,
		"num_results", len(input.AgentResults),
	)
	// Use activity-scoped logger so logs appear in Temporal activity logs
	logger := activity.GetLogger(ctx)

	if len(input.AgentResults) == 0 {
		return SynthesisResult{}, fmt.Errorf("no agent results to synthesize")
	}

	// LLM-first; fallback to simple synthesis on any failure

	// Emit synthesis start once at the beginning of the LLM attempt
	wfID := input.ParentWorkflowID
	if wfID == "" {
		if info := activity.GetInfo(ctx); info.WorkflowExecution.ID != "" {
			wfID = info.WorkflowExecution.ID
		}
	}
	if wfID != "" {
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventDataProcessing),
			AgentID:    "synthesis",
			Message:    MsgCombiningResults(),
			Timestamp:  time.Now(),
		})
	}

	// Extract context for role-aware synthesis
	role := ""
	contextMap := make(map[string]interface{})
	if input.Context != nil {
		// Extract role to apply role-specific prompts
		if r, ok := input.Context["role"].(string); ok {
			role = r
		}
		// Copy all context (includes prompt_params, language, etc.)
		for k, v := range input.Context {
			contextMap[k] = v
		}
	}

	// Build synthesis query that includes agent results
	const maxAgents = 6
	const maxPerAgentChars = 10000 // Increased for data-heavy responses (analytics, structured data)

	var b strings.Builder

	// Include reflection feedback if present
	if input.Context != nil {
		if feedback, ok := input.Context["reflection_feedback"].(string); ok && feedback != "" {
			fmt.Fprintf(&b, "IMPORTANT: The previous response needs improvement. Feedback: %s\n\n", feedback)
		}
		if prevResponse, ok := input.Context["previous_response"].(string); ok && prevResponse != "" {
			fmt.Fprintf(&b, "Previous response (needs improvement):\n%s\n\n", prevResponse)
		}
	}

	fmt.Fprintf(&b, "Please synthesize the following agent results for the query: %s\n\n", input.Query)
	fmt.Fprintf(&b, "Agent results (%d total):\n\n", len(input.AgentResults))

	count := 0
	for _, r := range input.AgentResults {
		if !r.Success || r.Response == "" {
			continue
		}
		trimmed := r.Response
		if len(trimmed) > maxPerAgentChars {
			trimmed = trimmed[:maxPerAgentChars] + "..."
		}
		fmt.Fprintf(&b, "=== Agent %s ===\n%s\n\n", r.AgentID, trimmed)
		count++
		if count >= maxAgents {
			break
		}
	}

	if count == 0 {
		logger.Warn("No successful agent results to synthesize")
		// Fallback: simple synthesis without emitting completion here; emit below
		res, err := simpleSynthesisNoEvents(ctx, input)
		if err != nil {
			return res, err
		}
		if wfID != "" {
			// Emit final synthesized content
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    "synthesis",
				Message:    truncateQuery(res.FinalResult, 10000),
				Timestamp:  time.Now(),
			})
			// Emit friendly summary with tokens
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    fmt.Sprintf("~%d tokens", res.TokensUsed),
				Timestamp:  time.Now(),
			})
            // Emit completion
            streaming.Get().Publish(wfID, streaming.Event{
                WorkflowID: wfID,
                Type:       string(StreamEventDataProcessing),
                AgentID:    "synthesis",
                Message:    "Final answer ready",
                Timestamp:  time.Now(),
            })
		}
		return res, nil
	}

	// Use /agent/query to leverage role presets and proper model selection
	base := getenv("LLM_SERVICE_URL", "http://llm-service:8000")
	reqBody := map[string]interface{}{
		"query":         b.String(),
		"context":       contextMap,
		"allowed_tools": []string{},  // Disable tools during synthesis - we only want formatting
		"agent_id":      "synthesis", // For observability
	}

	// If role is present, ensure it's in context
	if role != "" {
		reqBody["context"].(map[string]interface{})["role"] = role
		logger.Info("Synthesis using role-aware endpoint", zap.String("role", role))
	}

	// Add synthesis mode for observability
	reqBody["context"].(map[string]interface{})["mode"] = "synthesis"

	buf, _ := json.Marshal(reqBody)
	url := base + "/agent/query"

	httpClient := &http.Client{
		Timeout:   60 * time.Second, // Allow up to 1 minute for role-aware LLM synthesis
		Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		logger.Warn("LLM synthesis: request build failed, falling back", zap.Error(err))
		return simpleSynthesis(ctx, input)
	}
	req.Header.Set("Content-Type", "application/json")
	if input.ParentWorkflowID != "" {
		req.Header.Set("X-Parent-Workflow-ID", input.ParentWorkflowID)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		logger.Warn("LLM synthesis: HTTP error, falling back", zap.Error(err))
		res, serr := simpleSynthesisNoEvents(ctx, input)
		if serr != nil {
			return res, serr
		}
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    "synthesis",
				Message:    truncateQuery(res.FinalResult, 10000),
				Timestamp:  time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    fmt.Sprintf("~%d tokens", res.TokensUsed),
				Timestamp:  time.Now(),
			})
            streaming.Get().Publish(wfID, streaming.Event{
                WorkflowID: wfID,
                Type:       string(StreamEventDataProcessing),
                AgentID:    "synthesis",
                Message:    "Final answer ready",
                Timestamp:  time.Now(),
            })
		}
		return res, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Warn("LLM synthesis: non-2xx, falling back", zap.Int("status", resp.StatusCode))
		res, serr := simpleSynthesisNoEvents(ctx, input)
		if serr != nil {
			return res, serr
		}
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    "synthesis",
				Message:    truncateQuery(res.FinalResult, 10000),
				Timestamp:  time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    fmt.Sprintf("~%d tokens", res.TokensUsed),
				Timestamp:  time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
                Message:    "Final answer ready",
				Timestamp:  time.Now(),
			})
		}
		return res, nil
	}

	// Parse /agent/query response format (read body for diagnostics)
	rawBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		logger.Warn("LLM synthesis: read body failed, falling back", zap.Error(readErr))
		res, serr := simpleSynthesisNoEvents(ctx, input)
		if serr != nil {
			return res, serr
		}
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    "synthesis",
				Message:    truncateQuery(res.FinalResult, 10000),
				Timestamp:  time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    fmt.Sprintf("Synthesis summary: tokens=%d", res.TokensUsed),
				Timestamp:  time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
                Message:    "Analyzation completed",
				Timestamp:  time.Now(),
			})
		}
		return res, nil
	}

    var out struct {
        Response   string                 `json:"response"`
        Metadata   map[string]interface{} `json:"metadata"`
        TokensUsed int                    `json:"tokens_used"`
        ModelUsed  string                 `json:"model_used"`
    }
	if err := json.Unmarshal(rawBody, &out); err != nil {
		logger.Warn("LLM synthesis: decode error, falling back",
			zap.Error(err),
			zap.String("raw", truncateForLog(string(rawBody), 2000)),
		)
		res, serr := simpleSynthesisNoEvents(ctx, input)
		if serr != nil {
			return res, serr
		}
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    "synthesis",
				Message:    truncateQuery(res.FinalResult, 10000),
				Timestamp:  time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    fmt.Sprintf("Synthesis summary: tokens=%d", res.TokensUsed),
				Timestamp:  time.Now(),
			})
            streaming.Get().Publish(wfID, streaming.Event{
                WorkflowID: wfID,
                Type:       string(StreamEventDataProcessing),
                AgentID:    "synthesis",
                Message:    "Analyzation completed",
                Timestamp:  time.Now(),
            })
		}
		return res, nil
	}

	if out.Response == "" {
		logger.Warn("LLM synthesis: empty response, falling back",
			zap.String("raw", truncateForLog(string(rawBody), 2000)),
		)
		res, serr := simpleSynthesisNoEvents(ctx, input)
		if serr != nil {
			return res, serr
		}
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    "synthesis",
				Message:    truncateQuery(res.FinalResult, 10000),
				Timestamp:  time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    fmt.Sprintf("Synthesis summary: tokens=%d", res.TokensUsed),
				Timestamp:  time.Now(),
			})
            streaming.Get().Publish(wfID, streaming.Event{
                WorkflowID: wfID,
                Type:       string(StreamEventDataProcessing),
                AgentID:    "synthesis",
                Message:    "Analyzation completed",
                Timestamp:  time.Now(),
            })
		}
		return res, nil
	}

    // Extract model info: prefer top-level model_used; fallback to metadata.model
    model := out.ModelUsed
    if model == "" && out.Metadata != nil {
        if m, ok := out.Metadata["model"].(string); ok {
            model = m
        }
        // Also check allowed_tools to confirm role was applied
        if tools, ok := out.Metadata["allowed_tools"].([]interface{}); ok && len(tools) > 0 {
            logger.Info("Role-aware synthesis applied", zap.Int("allowed_tools_count", len(tools)))
        }
    }

	logger.Info("Synthesis (role-aware LLM) completed",
		zap.Int("tokens_used", out.TokensUsed),
		zap.String("model", model),
		zap.String("role", role),
	)

	// Emit synthesis result as LLM_OUTPUT so clients can see the final result
	if wfID != "" {
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventLLMOutput),
			AgentID:    "synthesis",
			Message:    truncateQuery(out.Response, 10000),
			Timestamp:  time.Now(),
		})
		// Also emit a friendly synthesis summary (model + tokens)
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventDataProcessing),
			AgentID:    "synthesis",
			Message:    fmt.Sprintf("Used %s (~%d tokens)", model, out.TokensUsed),
			Timestamp:  time.Now(),
		})
	}

	// Emit synthesis completed (DATA_PROCESSING)
	if wfID != "" {
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventDataProcessing),
			AgentID:    "synthesis",
            Message:    "Final answer ready",
			Timestamp:  time.Now(),
		})
	}

	return SynthesisResult{
		FinalResult: out.Response,
		TokensUsed:  out.TokensUsed,
	}, nil
}

// truncateForLog returns s truncated to max characters for safe logging
func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

// simpleSynthesis concatenates successful agent outputs with light formatting
// simpleSynthesisNoEvents performs simple synthesis without emitting streaming events
func simpleSynthesisNoEvents(ctx context.Context, input SynthesisInput) (SynthesisResult, error) {
	// Use activity-scoped logger for consistency with Temporal activity logging
	logger := activity.GetLogger(ctx)
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

// simpleSynthesis wraps simpleSynthesisNoEvents and emits a completion event
func simpleSynthesis(ctx context.Context, input SynthesisInput) (SynthesisResult, error) {
	res, err := simpleSynthesisNoEvents(ctx, input)
	if err != nil {
		return res, err
	}
	wfID := input.ParentWorkflowID
	if wfID == "" {
		if info := activity.GetInfo(ctx); info.WorkflowExecution.ID != "" {
			wfID = info.WorkflowExecution.ID
		}
	}
	if wfID != "" {
		// Emit synthesized content (simple path)
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventLLMOutput),
			AgentID:    "synthesis",
			Message:    truncateQuery(res.FinalResult, 10000),
			Timestamp:  time.Now(),
		})
        // Emit a simple summary with tokens
        streaming.Get().Publish(wfID, streaming.Event{
            WorkflowID: wfID,
            Type:       string(StreamEventDataProcessing),
            AgentID:    "synthesis",
            Message:    fmt.Sprintf("~%d tokens", res.TokensUsed),
            Timestamp:  time.Now(),
        })
        streaming.Get().Publish(wfID, streaming.Event{
            WorkflowID: wfID,
            Type:       string(StreamEventDataProcessing),
            AgentID:    "synthesis",
            Message:    "Analyzation completed",
            Timestamp:  time.Now(),
        })
	}
	return res, nil
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
