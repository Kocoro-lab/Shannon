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
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/formatting"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	"go.temporal.io/sdk/activity"
	"go.uber.org/zap"
)

// SynthesizeResults synthesizes results from multiple agents (baseline concatenation)
func SynthesizeResults(ctx context.Context, input SynthesisInput) (SynthesisResult, error) {
    // Emit synthesis start once for the simple (non-LLM) path
    wfID := input.ParentWorkflowID
    // Fallback to context-provided parent_workflow_id for correlation
    if wfID == "" && input.Context != nil {
        if v, ok := input.Context["parent_workflow_id"].(string); ok && v != "" {
            wfID = v
        }
    }
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
	// Emit 3-event sequence for synthesis completion:
	// 1. LLM_OUTPUT (content) - shows synthesized result to user
	// 2. DATA_PROCESSING (summary) - shows token usage metadata
	// 3. DATA_PROCESSING (completion) - final status message "Final answer ready"
	// This ordering ensures content is visible before status changes to "ready"
	if wfID != "" {
		// Event 1: LLM_OUTPUT with final content (simple path)
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventLLMOutput),
			AgentID:    "synthesis",
			Message:    truncateQuery(res.FinalResult, MaxSynthesisOutputChars),
			Payload: map[string]interface{}{
				"tokens_used": res.TokensUsed,
			},
			Timestamp:  time.Now(),
		})
		// Event 2: Lightweight tokens summary
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventDataProcessing),
			AgentID:    "synthesis",
			Message:    fmt.Sprintf("~%d tokens", res.TokensUsed),
			Timestamp:  time.Now(),
		})
		// Event 3: Synthesis completion status
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
    // Fallback to context-provided parent_workflow_id for correlation
    if wfID == "" && input.Context != nil {
        if v, ok := input.Context["parent_workflow_id"].(string); ok && v != "" {
            wfID = v
        }
    }
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

	// Ensure synthesis uses capable model tier for high-quality output
	// Default to "large" if not specified, since synthesis is the final user-facing output
	if _, hasModelTier := contextMap["model_tier"]; !hasModelTier {
		contextMap["model_tier"] = "large"
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

	// Add citation instructions for research workflows
	// Calculate minimum citations required (default to 6, clamp by available citations)
	minCitations := 6
	// Derive citation count from context if available
	if input.Context != nil {
		if v, ok := input.Context["citation_count"]; ok {
			switch t := v.(type) {
			case int:
				if t < minCitations {
					minCitations = t
				}
			case int32:
				if int(t) < minCitations {
					minCitations = int(t)
				}
			case int64:
				if int(t) < minCitations {
					minCitations = int(t)
				}
			case float64:
				// JSON numbers may be float64; clamp safely
				if int(t) < minCitations {
					minCitations = int(t)
				}
			}
		} else if citationList, ok := input.Context["available_citations"].(string); ok && citationList != "" {
			// Fallback: count non-empty lines
			lines := strings.Split(citationList, "\n")
			count := 0
			for _, ln := range lines {
				if strings.TrimSpace(ln) != "" {
					count++
				}
			}
			if count > 0 && count < minCitations {
				minCitations = count
			}
		}
	}
	if minCitations < 3 {
		minCitations = 3 // Minimum floor for research synthesis
	}

    // Detect language from query for language matching
    queryLanguage := detectLanguage(input.Query)
    // Keep instruction generic to avoid brittle per-language templates
    languageInstruction := fmt.Sprintf(
        "Respond in the same language as the user's query (detected: %s).",
        queryLanguage,
    )

	fmt.Fprintf(&b, `# Synthesis Requirements:

	## CRITICAL - Language Matching:
	%s
	The user's query is in %s. You MUST respond in the SAME language.
	DO NOT translate or switch to English unless the query is in English.

	## Citation Integration:
	- You MUST use AT LEAST %d inline citations from Available Citations
	- Use inline citations [1], [2] for ALL factual claims
	- Number sources sequentially WITHOUT GAPS (1, 2, 3, 4... not 1, 3, 5...)
	- Each unique URL gets ONE citation number only

	## Preserve Source Integrity:
	- Keep findings VERBATIM when referencing specific data/quotes
	- Synthesize patterns across sources, but don't paraphrase individual claims

	## Output Structure:
	1. Executive Summary (2-3 sentences)
	2. Detailed Findings (with inline citations)
	3. ## Sources (numbered list at end with format: [1] Title (URL))

	## Quality Standards:
	- If conflicting information exists, note explicitly: "Source [1] reports X, while [2] suggests Y"
	- Flag gaps: "Limited information available on [aspect]"
	- NEVER fabricate or hallucinate sources
	- Ensure each inline citation directly supports the specific claim; prefer primary sources (publisher/DOI) over aggregators (e.g., Crossref, Semantic Scholar)

	`, languageInstruction, queryLanguage, minCitations)

	// Include available citations if present (Phase 2.5 fix)
	if input.Context != nil {
		if citationList, ok := input.Context["available_citations"].(string); ok && citationList != "" {
			fmt.Fprintf(&b, "## Available Citations (use these in your synthesis):\n%s\n", citationList)
		}
	}

	fmt.Fprintf(&b, "Agent results (%d total):\n\n", len(input.AgentResults))

	count := 0
	for _, r := range input.AgentResults {
		if !r.Success || r.Response == "" {
			continue
		}
		trimmed := r.Response
		if len([]rune(trimmed)) > maxPerAgentChars {
			trimmed = string([]rune(trimmed)[:maxPerAgentChars]) + "..."
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
				Message:    truncateQuery(res.FinalResult, MaxSynthesisOutputChars),
				Payload: map[string]interface{}{
					"tokens_used": res.TokensUsed,
				},
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

    // Calculate max_tokens for synthesis without a hard ceiling.
    // Industry practice: do not truncate final, user-facing output; guide length via prompt.
    // Base: 4096, plus 1024 per agent result, no artificial cap.
    maxTokens := 4096 + (len(input.AgentResults) * 1024)
    logger.Info("Synthesis max_tokens calculated",
        zap.Int("agent_count", len(input.AgentResults)),
        zap.Int("max_tokens", maxTokens),
    )

	reqBody := map[string]interface{}{
		"query":         b.String(),
		"context":       contextMap,
		"allowed_tools": []string{},  // Disable tools during synthesis - we only want formatting
		"agent_id":      "synthesis", // For observability
		"max_tokens":    maxTokens,   // Scale with agent count to avoid truncation
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
    if wfID != "" {
        req.Header.Set("X-Parent-Workflow-ID", wfID)
    }

	resp, err := httpClient.Do(req)
	if err != nil {
		logger.Warn("LLM synthesis: HTTP error, falling back", zap.Error(err))
		res, serr := simpleSynthesisNoEvents(ctx, input)
		if serr != nil {
			return res, serr
		}
		// Emit standard 3-event sequence (fallback path)
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    "synthesis",
				Message:    truncateQuery(res.FinalResult, MaxSynthesisOutputChars),
				Payload: map[string]interface{}{
					"tokens_used": res.TokensUsed,
				},
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
		// Emit standard 3-event sequence (fallback path)
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    "synthesis",
				Message:    truncateQuery(res.FinalResult, MaxSynthesisOutputChars),
				Payload: map[string]interface{}{
					"tokens_used": res.TokensUsed,
				},
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
		// Emit standard 3-event sequence (fallback path)
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    "synthesis",
				Message:    truncateQuery(res.FinalResult, MaxSynthesisOutputChars),
				Payload: map[string]interface{}{
					"tokens_used": res.TokensUsed,
				},
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
				Message:    "Final answer ready",
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
		// Emit standard 3-event sequence (fallback path)
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    "synthesis",
				Message:    truncateQuery(res.FinalResult, MaxSynthesisOutputChars),
				Payload: map[string]interface{}{
					"tokens_used": res.TokensUsed,
				},
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
				Message:    "Final answer ready",
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
		// Emit standard 3-event sequence (fallback path)
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    "synthesis",
				Message:    truncateQuery(res.FinalResult, MaxSynthesisOutputChars),
				Payload: map[string]interface{}{
					"tokens_used": res.TokensUsed,
				},
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
				Message:    "Final answer ready",
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

	// Apply report formatting to ensure all citations appear in Sources
	finalResponse := out.Response
	if input.Context != nil {
		if citationList, ok := input.Context["available_citations"].(string); ok && citationList != "" {
			finalResponse = formatting.FormatReportWithCitations(finalResponse, citationList)
		}
	}

	// Extract usage metadata for event payload
	provider := ""
	inputTokens := 0
	outputTokens := 0
	costUsd := 0.0
	if out.Metadata != nil {
		if p, ok := out.Metadata["provider"].(string); ok {
			provider = p
		}
		if it, ok := out.Metadata["input_tokens"].(float64); ok {
			inputTokens = int(it)
		} else if it, ok := out.Metadata["input_tokens"].(int); ok {
			inputTokens = it
		}
		if ot, ok := out.Metadata["output_tokens"].(float64); ok {
			outputTokens = int(ot)
		} else if ot, ok := out.Metadata["output_tokens"].(int); ok {
			outputTokens = ot
		}
		if cost, ok := out.Metadata["cost_usd"].(float64); ok {
			costUsd = cost
		}
	}

	// Emit 3-event sequence for synthesis completion:
	// 1. LLM_OUTPUT (content) - shows synthesized result to user
	// 2. DATA_PROCESSING (summary) - shows model and token usage metadata
	// 3. DATA_PROCESSING (completion) - final status message "Final answer ready"
	// This ordering ensures content is visible before status changes to "ready"
	if wfID != "" {
			// Event 1: LLM_OUTPUT with final content (LLM path)
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    "synthesis",
				Message:    truncateQuery(finalResponse, MaxSynthesisOutputChars),
				Payload: map[string]interface{}{
					"tokens_used":   out.TokensUsed,
					"model_used":    model,
					"provider":      provider,
					"input_tokens":  inputTokens,
					"output_tokens": outputTokens,
					"cost_usd":      costUsd,
				},
				Timestamp:  time.Now(),
			})
		// Event 2: Synthesis summary with model and token usage (omit model if unknown)
		summary := fmt.Sprintf("~%d tokens", out.TokensUsed)
		if model != "" && model != "unknown" {
			summary = fmt.Sprintf("Used %s (~%d tokens)", model, out.TokensUsed)
		}
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventDataProcessing),
			AgentID:    "synthesis",
			Message:    summary,
			Timestamp:  time.Now(),
		})
		// Event 3: Synthesis completion status
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventDataProcessing),
			AgentID:    "synthesis",
			Message:    "Final answer ready",
			Timestamp:  time.Now(),
		})
	}

	// Extract finish_reason safely from metadata
	finishReason := "stop"
	if fr, ok := out.Metadata["finish_reason"].(string); ok && fr != "" {
		finishReason = fr
	}

	return SynthesisResult{
		FinalResult:  finalResponse,
		TokensUsed:   out.TokensUsed,
		FinishReason: finishReason,
	}, nil
}

// detectLanguage performs simple heuristic language detection based on character ranges
func detectLanguage(query string) string {
	if query == "" {
		return "English"
	}

	// Count characters by Unicode range
	var cjk, cyrillic, arabic, latin int
	for _, r := range query {
		switch {
		case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
			cjk++
		case r >= 0x3040 && r <= 0x309F: // Hiragana
			cjk++
		case r >= 0x30A0 && r <= 0x30FF: // Katakana
			cjk++
		case r >= 0xAC00 && r <= 0xD7AF: // Hangul Syllables
			cjk++
		case r >= 0x0400 && r <= 0x04FF: // Cyrillic
			cyrillic++
		case r >= 0x0600 && r <= 0x06FF: // Arabic
			arabic++
		case (r >= 0x0041 && r <= 0x005A) || (r >= 0x0061 && r <= 0x007A): // Latin
			latin++
		}
	}

	total := cjk + cyrillic + arabic + latin
	if total == 0 {
		return "English" // Default if no recognized characters
	}

	// Determine language based on character composition
	cjkPercent := float64(cjk) / float64(total)
	if cjkPercent > 0.3 {
		// Distinguish Chinese/Japanese/Korean by character patterns
		var hiragana, katakana, hangul int
		for _, r := range query {
			if r >= 0x3040 && r <= 0x309F {
				hiragana++
			}
			if r >= 0x30A0 && r <= 0x30FF {
				katakana++
			}
			if r >= 0xAC00 && r <= 0xD7AF {
				hangul++
			}
		}
		if hangul > 0 {
			return "Korean"
		}
		if hiragana > 0 || katakana > 0 {
			return "Japanese"
		}
		return "Chinese"
	}

	cyrillicPercent := float64(cyrillic) / float64(total)
	if cyrillicPercent > 0.3 {
		return "Russian"
	}

	arabicPercent := float64(arabic) / float64(total)
	if arabicPercent > 0.3 {
		return "Arabic"
	}

	// Check for common non-English Latin script patterns
	lowerQuery := strings.ToLower(query)
	if strings.Contains(lowerQuery, "ñ") || strings.Contains(lowerQuery, "¿") || strings.Contains(lowerQuery, "¡") {
		return "Spanish"
	}
	if strings.Contains(lowerQuery, "ç") || strings.Contains(lowerQuery, "à") || strings.Contains(lowerQuery, "è") {
		return "French"
	}
	if strings.Contains(lowerQuery, "ä") || strings.Contains(lowerQuery, "ö") || strings.Contains(lowerQuery, "ü") || strings.Contains(lowerQuery, "ß") {
		return "German"
	}

	return "English" // Default for Latin scripts
}

// truncateForLog returns s truncated to max characters for safe logging
func truncateForLog(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "...(truncated)"
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
		FinalResult:  finalResult,
		TokensUsed:   totalTokens,
		FinishReason: "stop", // Simple synthesis always completes
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
			Message:    truncateQuery(res.FinalResult, MaxSynthesisOutputChars),
			Payload: map[string]interface{}{
				"tokens_used": res.TokensUsed,
			},
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
			Message:    "Final answer ready",
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
