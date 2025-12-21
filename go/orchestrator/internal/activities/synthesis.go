package activities

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/formatting"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/util"
	"go.temporal.io/sdk/activity"
	"go.uber.org/zap"
)

// sanitizeAgentOutput removes duplicate references from agent outputs
// to avoid sending the same URLs/citations twice (once in agent output, once in Available Citations)
func sanitizeAgentOutput(text string) string {
	// First, filter out XML tool tags that may have leaked into agent responses
	toolTagPattern := regexp.MustCompile(`<(search_query|web_fetch|web_search|tool)[^>]*>.*?</(search_query|web_fetch|web_search|tool)>`)
	text = toolTagPattern.ReplaceAllString(text, "")
	// Also filter single/self-closing tags
	singleTagPattern := regexp.MustCompile(`<(search_query|web_fetch|web_search|tool)[^>]*/?>`)
	text = singleTagPattern.ReplaceAllString(text, "")

	lines := strings.Split(text, "\n")
	var result []string
	inSourcesSection := false

	urlPattern := regexp.MustCompile(`^https?://`)
	citationPattern := regexp.MustCompile(`^\[\d+\]\s+https?://`)
	inlineURLPattern := regexp.MustCompile(`https?://\S+`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect Sources section start
		if strings.HasPrefix(trimmed, "## Sources") || strings.HasPrefix(trimmed, "### Sources") {
			inSourcesSection = true
			continue
		}

		// Inside Sources section: keep descriptive context, drop raw URLs/citation-only lines.
		if inSourcesSection {
			// Check if we hit another major section (exit Sources)
			if strings.HasPrefix(trimmed, "##") && !strings.HasPrefix(trimmed, "## Sources") {
				inSourcesSection = false
			} else {
				// Skip empty lines
				if trimmed == "" {
					continue
				}
				// Skip bare URLs
				if urlPattern.MatchString(trimmed) {
					continue
				}
				// Skip citation lines like "[1] https://..."
				if citationPattern.MatchString(trimmed) {
					continue
				}
				// Skip bullet points with only URLs
				if strings.HasPrefix(trimmed, "- http") || strings.HasPrefix(trimmed, "* http") || strings.HasPrefix(trimmed, "• http") {
					continue
				}

				// Keep descriptive source notes but remove inline URLs to avoid duplication/noise
				clean := inlineURLPattern.ReplaceAllString(line, "")
				clean = strings.TrimSpace(clean)
				if clean != "" && clean != "-" && clean != "*" && clean != "•" {
					result = append(result, clean)
				}
				continue
			}
		}

		// Skip bare URLs
		if urlPattern.MatchString(trimmed) {
			continue
		}

		// Skip citation lines like "[1] https://..."
		if citationPattern.MatchString(trimmed) {
			continue
		}

		// Skip bullet points with only URLs
		if strings.HasPrefix(trimmed, "- http") || strings.HasPrefix(trimmed, "* http") || strings.HasPrefix(trimmed, "• http") {
			continue
		}

		// Keep the line
		result = append(result, line)
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}

// normalizeLanguage maps language codes to the full language name used in prompts
func normalizeLanguage(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	switch l {
	case "zh", "zh-cn", "zh-hans", "zh-hant", "cn", "chinese":
		return "Chinese"
	case "en", "en-us", "en-gb", "english":
		return "English"
	case "ja", "jp", "japanese":
		return "Japanese"
	case "ko", "kr", "korean":
		return "Korean"
	case "ru", "russian":
		return "Russian"
	case "ar", "arabic":
		return "Arabic"
	case "es", "spanish":
		return "Spanish"
	case "fr", "french":
		return "French"
	case "de", "german":
		return "German"
	default:
		return ""
	}
}

// --- Result preprocessing (Phase 1 dedup + basic filtering) ---
var (
	nonWordPattern = regexp.MustCompile(`[\p{P}\p{S}]+`)
	// Precise patterns to avoid false positives (complete phrases only)
	noInfoPatterns = []string{
		// English: Access failures (complete phrases)
		"unfortunately, i cannot access",
		"unfortunately, i am unable to access",
		"unfortunately, the domain",
		"cannot connect to host",
		"failed to fetch",
		"unable to access the website",
		"unable to retrieve",
		"could not access",
		"network connection error",
		"dns resolution failed",
		"name or service not known",
		"website is offline",
		"site is unavailable",
		"suggested alternatives",
		"would you like me to try",
		"shall i attempt",

		// English: No info found
		"i couldn't find",
		"no information available",
		"unable to find",
		"no results found",
		"couldn't locate",
		"not able to find",

		// Chinese: Access failures (complete phrases)
		"不幸的是，我无法访问",
		"不幸的是，该域名",
		"无法访问该网站",
		"无法连接到",
		"dns解析失败",
		"域名解析失败",
		"网络连接错误",
		"网站可能离线",
		"网站不可用",
		"建议的替代方案",
		"您希望我尝试",
		"是否需要我",

		// Chinese: No info found
		"没有找到相关",
		"未找到",
		"无法找到",

		// Japanese: Access failures
		"残念ながら、アクセスできません",
		"接続できません",
		"サイトが利用できません",

		// Japanese: No info found
		"見つかりませんでした",
		"情報が見つかりません",
	}
	similarityThresh = 0.85
)

func preprocessAgentResults(results []AgentExecutionResult, logger interface{}) []AgentExecutionResult {
	if len(results) == 0 {
		return results
	}

	original := len(results)
	exact := deduplicateExact(results)
	near := deduplicateSimilar(exact, similarityThresh)
	filtered := filterLowQuality(near)

	// Log using zap directly for consistent structured logging
	zap.L().Info("Preprocessed agent results for synthesis",
		zap.Int("original_count", original),
		zap.Int("after_exact", len(exact)),
		zap.Int("after_similarity", len(near)),
		zap.Int("after_filter", len(filtered)),
	)

	return filtered
}

func deduplicateExact(results []AgentExecutionResult) []AgentExecutionResult {
	seen := make(map[string]bool, len(results))
	var out []AgentExecutionResult

	for _, r := range results {
		normalized := strings.TrimSpace(strings.ToLower(r.Response))
		if normalized == "" {
			continue
		}
		hash := sha256.Sum256([]byte(normalized))
		key := hex.EncodeToString(hash[:])
		if !seen[key] {
			seen[key] = true
			out = append(out, r)
		}
	}
	return out
}

func deduplicateSimilar(results []AgentExecutionResult, threshold float64) []AgentExecutionResult {
	var unique []AgentExecutionResult

	for _, candidate := range results {
		isDup := false
		cTokens := tokenize(candidate.Response)
		for _, existing := range unique {
			sTokens := tokenize(existing.Response)
			if jaccardSimilarity(cTokens, sTokens) > threshold {
				isDup = true
				break
			}
		}
		if !isDup {
			unique = append(unique, candidate)
		}
	}
	return unique
}

func tokenize(text string) map[string]bool {
	lower := strings.ToLower(text)
	clean := nonWordPattern.ReplaceAllString(lower, " ")
	tokens := strings.Fields(clean)
	out := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		if t != "" {
			out[t] = true
		}
	}
	return out
}

func jaccardSimilarity(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	intersection := 0
	union := len(a)
	for token := range b {
		if a[token] {
			intersection++
		} else {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func filterLowQuality(results []AgentExecutionResult) []AgentExecutionResult {
	var filtered []AgentExecutionResult
	for _, r := range results {
		resp := strings.TrimSpace(r.Response)
		if !r.Success || resp == "" {
			continue
		}
		// Filter any response containing error patterns (removed 200-char limit)
		if containsNoInfoPatterns(resp) {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

func containsNoInfoPatterns(text string) bool {
	lower := strings.ToLower(text)
	for _, p := range noInfoPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

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
		payload := map[string]interface{}{
			"tokens_used":          res.TokensUsed,
			"model_used":           res.ModelUsed,
			"provider":             res.Provider,
			"input_tokens":         res.InputTokens,
			"output_tokens":        res.CompletionTokens,
			"cost_usd":             res.CostUsd,
			"finish_reason":        res.FinishReason,
			"requested_max_tokens": res.RequestedMaxTokens,
		}

		// Include citations if available in context
		if input.Context != nil {
			if citations, ok := input.Context["citations"].([]map[string]interface{}); ok && len(citations) > 0 {
				payload["citations"] = citations
			}
		}

		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventLLMOutput),
			AgentID:    "synthesis",
			Message:    truncateQuery(res.FinalResult, MaxSynthesisOutputChars),
			Payload:    payload,
			Timestamp:  time.Now(),
		})
		// Event 2: Lightweight tokens summary
		msgSummary := fmt.Sprintf("~%d tokens", res.TokensUsed)
		if res.ModelUsed != "" {
			msgSummary = fmt.Sprintf("Used %s (~%d tokens)", res.ModelUsed, res.TokensUsed)
		}
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventDataProcessing),
			AgentID:    "synthesis",
			Message:    msgSummary,
			Timestamp:  time.Now(),
		})
		// Event 3: Synthesis completion status
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventDataProcessing),
			AgentID:    "synthesis",
			Message:    MsgFinalAnswer(),
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
	// Use direct zap logger for detailed diagnostic fields (Temporal adapter strips zap fields)
	diagLogger := zap.L().With(zap.String("activity", "SynthesizeResultsLLM"))

	if len(input.AgentResults) == 0 {
		return SynthesisResult{}, fmt.Errorf("no agent results to synthesize")
	}

	input.AgentResults = preprocessAgentResults(input.AgentResults, logger)
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
	// Track citation payload size for diagnostics and save for later appending
	removedCitations := false
	removedCitationsChars := 0
	var savedCitations string // Save citations to append after synthesis
	if input.Context != nil {
		// Extract role to apply role-specific prompts
		if r, ok := input.Context["role"].(string); ok {
			role = r
		}
		// Copy all context (includes prompt_params, language, etc.)
		for k, v := range input.Context {
			contextMap[k] = v
		}
		// Remove large duplicates from LLM prompt but save for post-processing
		if v, ok := contextMap["available_citations"]; ok {
			if s, ok := v.(string); ok {
				removedCitations = true
				removedCitationsChars = len([]rune(s))
				savedCitations = s // Save for later appending
			}
			delete(contextMap, "available_citations")
		}
	}

	// Ensure synthesis uses capable model tier for high-quality output
	// ALWAYS use "large" tier for synthesis regardless of context, since this is
	// the final user-facing output. Agent execution uses smaller tiers for cost
	// optimization, but synthesis quality is critical.
	contextMap["model_tier"] = "large"

	// Build synthesis query that includes agent results
	// Truncation is adjusted later once we know whether this is a research-style synthesis.
	maxPerAgentChars := 4000 // Default for non-research

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
	availableCitations := 0
	// Derive citation count from context if available
	if input.Context != nil {
		if v, ok := input.Context["citation_count"]; ok {
			switch t := v.(type) {
			case int:
				if t < minCitations {
					minCitations = t
				}
				availableCitations = t
			case int32:
				if int(t) < minCitations {
					minCitations = int(t)
				}
				availableCitations = int(t)
			case int64:
				if int(t) < minCitations {
					minCitations = int(t)
				}
				availableCitations = int(t)
			case float64:
				// JSON numbers may be float64; clamp safely
				if int(t) < minCitations {
					minCitations = int(t)
				}
				availableCitations = int(t)
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
			availableCitations = count
		}
	}
	if availableCitations > 0 && minCitations > availableCitations {
		minCitations = availableCitations
	}
	if availableCitations > 0 {
		if minCitations < 3 && availableCitations >= 3 {
			minCitations = 3
		}
	} else if minCitations < 3 {
		minCitations = 3 // Minimum floor for research synthesis
	}

	// Detect language from query for language matching
	queryLanguage := ""
	if input.Context != nil {
		if v, ok := input.Context["target_language"].(string); ok {
			if mapped := normalizeLanguage(v); mapped != "" {
				queryLanguage = mapped
			}
		}
	}
	if queryLanguage == "" {
		queryLanguage = detectLanguage(input.Query)
	}

	// Check if this is a language-retry with stronger emphasis
	forceLanguageMatch := false
	if input.Context != nil {
		if force, ok := input.Context["force_language_match"].(bool); ok {
			forceLanguageMatch = force
		}
	}

	// Build language instruction (stronger for retries)
	var languageInstruction string
	if forceLanguageMatch {
		languageInstruction = fmt.Sprintf(
			"🚨 CRITICAL LANGUAGE REQUIREMENT 🚨\nYou MUST respond ENTIRELY in %s.\nThe user's query is in %s.\nDO NOT use English or any other language.\nDO NOT mix languages.\nEVERY sentence, heading, and word must be in %s.",
			queryLanguage, queryLanguage, queryLanguage,
		)
	} else {
		languageInstruction = fmt.Sprintf(
			"Respond in the same language as the user's query (detected: %s).",
			queryLanguage,
		)
	}

	// Check synthesis style (comprehensive vs. concise)
	synthesisStyle := "concise"
	if input.Context != nil {
		if style, ok := input.Context["synthesis_style"].(string); ok && style != "" {
			synthesisStyle = style
		}
	}

	// Prepare optional organization guidance from research_areas
	areasInstruction := ""
	var areas []string
	if input.Context != nil {
		if rawAreas, ok := input.Context["research_areas"]; ok && rawAreas != nil {
			// Accept []string or []interface{}
			switch t := rawAreas.(type) {
			case []string:
				areas = t
			case []interface{}:
				for _, it := range t {
					if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
						areas = append(areas, s)
					}
				}
			}
			if len(areas) > 0 {
				// Provide explicit heading skeleton for the model to follow
				var sb strings.Builder
				sb.WriteString("## MANDATORY Research Area Coverage:\n")
				sb.WriteString(fmt.Sprintf("You MUST create a subsection for EACH of the %d research areas below.\n", len(areas)))
				sb.WriteString("Each subsection should be 250–400 words with inline citations.\n")
				sb.WriteString("Structure your Detailed Findings section with these exact headings:\n")
				for _, a := range areas {
					if strings.TrimSpace(a) != "" {
						sb.WriteString("### ")
						sb.WriteString(a)
						sb.WriteString("\n")
					}
				}
				sb.WriteString("\nDo NOT skip any research areas. Generate comprehensive content for ALL sections above.\n")
				areasInstruction = sb.String()
			}
		}
	}

	// Determine if this is a research-style synthesis
	isResearch := false
	if strings.EqualFold(synthesisStyle, "comprehensive") || len(areas) > 0 {
		isResearch = true
	}
	if input.Context != nil {
		if util.GetContextBool(input.Context, "force_research") {
			isResearch = true
		}
		if _, ok := input.Context["enable_citations"]; ok {
			isResearch = true
		}
		if rm, ok := input.Context["research_mode"].(string); ok {
			if strings.TrimSpace(rm) != "" || strings.EqualFold(rm, "gap_fill") {
				isResearch = true
			}
		}
	}

	// For research workflows, preserve more detail per agent output, but scale down when many agents exist
	// to avoid oversized prompts that can degrade recall and increase truncation risk upstream.
	if isResearch {
		maxPerAgentChars = 8000
		switch {
		case len(input.AgentResults) > 25:
			maxPerAgentChars = 4000
		case len(input.AgentResults) > 15:
			maxPerAgentChars = 6000
		case len(input.AgentResults) > 10:
			maxPerAgentChars = 7000
		}
		logger.Info("Adjusted per-agent truncation for research synthesis",
			zap.Int("maxPerAgentChars", maxPerAgentChars),
			zap.Int("agent_count", len(input.AgentResults)),
		)
	}

	// Calculate target words for research synthesis
	// Deep Research 2.0: Increased multiplier to capture more intermediate findings
	targetWords := 1200
	if len(areas) > 0 {
		targetWords = len(areas) * 400 // 400 words per area for comprehensive coverage
	}
	// Ensure minimum for comprehensive reports
	if targetWords < 1800 {
		targetWords = 1800
	}

	// Get available citations string
	availableCitationsStr := ""
	if input.Context != nil {
		if citList, ok := input.Context["available_citations"].(string); ok {
			availableCitationsStr = citList
		}
	}

	// Check if Citation Agent is enabled (citations will be added separately)
	// Default: enabled - synthesis should NOT add inline citations, Citation Agent will handle it
	// This check is at the top level so it applies to both template and fallback modes
	citationAgentEnabled := true
	if input.Context != nil {
		if v, ok := input.Context["enable_citation_agent"].(bool); ok {
			citationAgentEnabled = v
		}
	}

	// Try template-based synthesis (Phase 3: Template System)
	templateUsed := false
	if input.Context != nil {
		templateName, explicit := SelectSynthesisTemplate(input.Context)
		logger.Info("Selected synthesis template",
			zap.String("template", templateName),
			zap.Bool("explicit", explicit),
			zap.Bool("isResearch", isResearch),
			zap.Bool("citationAgentEnabled", citationAgentEnabled),
		)

		// Check for verbatim template override first
		if override, ok := input.Context["synthesis_template_override"].(string); ok && override != "" {
			// User provided verbatim template text - use directly
			fmt.Fprintf(&b, "%s\n\n", override)
			templateUsed = true
			logger.Info("Using verbatim synthesis template override")
		} else if tmpl := LoadSynthesisTemplate(templateName, nil); tmpl != nil {
			// Try to render the template
			data := SynthesisTemplateData{
				Query:               input.Query,
				QueryLanguage:       queryLanguage,
				ResearchAreas:       areas,
				AvailableCitations:  availableCitationsStr,
				CitationCount:       availableCitations,
				MinCitations:        minCitations,
				LanguageInstruction: languageInstruction,
				AgentResults:        "", // Agent results appended separately below
				TargetWords:         targetWords,
				IsResearch:          isResearch,
				SynthesisStyle:      synthesisStyle,
				CitationAgentEnabled: citationAgentEnabled,
			}

			rendered, err := RenderSynthesisTemplate(tmpl, data)
			if err != nil {
				logger.Warn("Failed to render synthesis template, using fallback",
					zap.String("template", templateName),
					zap.Error(err),
				)
			} else {
				fmt.Fprintf(&b, "%s\n\n", rendered)
				templateUsed = true
				logger.Info("Successfully rendered synthesis template",
					zap.String("template", templateName),
					zap.Int("rendered_length", len(rendered)),
				)
			}
		}
	}

	// Fallback: Use hardcoded prompt if no template was used
	if !templateUsed {
		logger.Debug("Using hardcoded synthesis prompt (no template)")

		// Define output structure based on synthesis style
		// (citationAgentEnabled already defined at top level)
		outputStructure := ""
		// Citation instructions depend on whether Citation Agent is enabled
		citationInstr := "include inline citations"
		if citationAgentEnabled {
			citationInstr = "cite sources naturally (e.g., 'According to...')"
		}
		if synthesisStyle == "comprehensive" {
			// For deep research: comprehensive multi-section report (no Sources section; system appends it)
			targetWords := 1200
			if len(areas) > 0 {
				// Calculate target based on research areas (250-400 words per area)
				targetWords = len(areas) * 400
			}
			// Use explicit top-level headings and forbid copying instruction text into the answer
			outputStructure = fmt.Sprintf(`## Output Format (do NOT include this section in the final answer):

Use exactly these top-level headings in your response, and start your answer directly with "## Executive Summary" (do NOT include any instruction text):

## Executive Summary
## Detailed Findings
## Limitations and Uncertainties (ONLY if significant gaps/conflicts exist)

Section requirements:
- Executive Summary: 250–400 words; capture key insights and conclusions
- Detailed Findings: %d–%d words total; organize by research areas as subsections; cover ALL areas with roughly equal depth; %s; include quantitative data, timelines, key developments; discuss implications; address contradictions explicitly
- Limitations and Uncertainties: 100–150 words IF evidence is incomplete, contradictory, or outdated; OMIT this section entirely if findings are well-supported and comprehensive
`, targetWords, targetWords+600, citationInstr)
		} else {
			// Default: concise synthesis (no Sources section; system appends it)
			outputStructure = fmt.Sprintf(`## Output Format (do NOT include this section in the final answer):

Use exactly these top-level headings in your response, and start your answer directly with "## Executive Summary" (do NOT include any instruction text):

## Executive Summary
## Detailed Findings
## Limitations and Uncertainties (ONLY if significant gaps exist)

Section requirements:
- Executive Summary: 2–3 sentences; state findings confidently
- Detailed Findings: %s; state facts authoritatively
- Limitations and Uncertainties: OMIT entirely if findings are comprehensive; include ONLY if evidence is genuinely insufficient or contradictory
`, citationInstr)
		}

		if isResearch {
			// Determine whether citations are available in context
			hasCitations := false
			if input.Context != nil {
				if v, ok := input.Context["available_citations"].(string); ok && strings.TrimSpace(v) != "" {
					hasCitations = true
				} else if v, ok := input.Context["citation_count"]; ok {
					switch t := v.(type) {
					case int:
						hasCitations = t > 0
					case int32:
						hasCitations = int(t) > 0
					case int64:
						hasCitations = int(t) > 0
					case float64:
						hasCitations = int(t) > 0
					}
				}
			}

			// Build dynamic checklist and citation guidance (citationAgentEnabled defined at top level)
			coverageExtra := ""
			if hasCitations && !citationAgentEnabled {
				coverageExtra = "    ✓ Each subsection includes ≥2 inline citations [n]\\n" +
					"    ✓ ALL claims supported by Available Citations (no fabrication)\\n" +
					"    ✓ Conflicting sources explicitly noted: \\\"[1] says X, [2] says Y\\\"\\n"
			} else if citationAgentEnabled {
				coverageExtra = "    ✓ Focus on accurate content - citations will be added automatically\\n" +
					"    ✓ Note conflicting information: \\\"Some sources indicate X, while others suggest Y\\\"\\n"
			} else {
				coverageExtra = "    ✓ If no sources are available, do NOT fabricate citations; mark unsupported claims as \\\"unverified\\\"\\n"
			}

			citationGuidance := ""
			if citationAgentEnabled {
				// Citation Agent mode: synthesis should NOT add citations
				citationGuidance = `## Citation Handling:
    - DO NOT add any inline citations [n] to your response
    - A separate Citation Agent will add citations after you finish
    - Focus ONLY on producing accurate, well-organized content
    - When referencing facts from sources, write naturally without citation markers
    - Note conflicting information: "Some sources indicate X, while others suggest Y"
    - Do NOT include a "## Sources" section; the system handles this automatically
`
			} else if hasCitations {
				citationGuidance = fmt.Sprintf(`## Citation Integration:
    - Use inline citations [1], [2] for ALL factual claims that have supporting sources
    - Aim for AT LEAST %d inline citations IF sufficient relevant sources exist
    - Use ONLY the provided Available Citations and their existing indices [n]
    - DO NOT cite irrelevant sources just to meet a quota (e.g., don't cite competitors when researching a specific company)
    - If a research area lacks relevant citations, note explicitly: "Limited information available on [aspect]" rather than citing unrelated sources
    - DO NOT invent new citation numbers; if a claim lacks a matching citation, flag as "unverified"
    - Each unique URL gets ONE citation number only
    - Do NOT include a "## Sources" section; the system will append Sources automatically
`, minCitations)
			} else {
				citationGuidance = `## Citation Guidance:
    - Do NOT fabricate citations.
    - If a claim lacks supporting sources, mark it as "unverified".
`
			}

			// Build conditional sections based on Citation Agent mode
			quantitativeCitationLine := ""
			if !citationAgentEnabled {
				quantitativeCitationLine = "    - Include inline citations [n] for ALL data points in tables\n"
			}

			qualityStandards := ""
			if citationAgentEnabled {
				// Quality standards WITHOUT citation references (Citation Agent will handle)
				qualityStandards = `## Quality Standards:
	- State findings CONFIDENTLY and AUTHORITATIVELY when well-supported by evidence
	- DO NOT add unnecessary cautious disclaimers (e.g., "we were unable to confirm") unless evidence is genuinely missing
	- Present well-evidenced facts as definitive conclusions, not tentative observations
	- Do NOT mention agents, tools, workflows, or internal retrieval; write directly to the user
	- If conflicting information exists, note naturally: "Some sources indicate X, while others suggest Y"
	- Flag gaps ONLY when evidence is genuinely insufficient: "No public data available on [specific aspect]"
	- If ALL research areas have comprehensive findings: OMIT the "Limitations and Uncertainties" section entirely
	- NEVER fabricate or hallucinate information
`
			} else {
				// Quality standards WITH citation references
				qualityStandards = `## Quality Standards:
	- State findings CONFIDENTLY and AUTHORITATIVELY when well-supported by citations
	- DO NOT add unnecessary cautious disclaimers (e.g., "we were unable to confirm") unless evidence is genuinely missing
	- Present well-cited facts as definitive conclusions, not tentative observations
	- Do NOT mention agents, tools, workflows, or internal retrieval; write directly to the user
	- If conflicting information exists, note explicitly: "Source [1] reports X, while [2] suggests Y"
	- Flag gaps ONLY when evidence is genuinely insufficient: "No public data available on [specific aspect]"
	- If ALL research areas have comprehensive citations and findings: OMIT the "Limitations and Uncertainties" section entirely
	- NEVER fabricate or hallucinate sources
	- Ensure each inline citation directly supports the specific claim; prefer primary sources over aggregators
`
			}

			fmt.Fprintf(&b, `# Synthesis Requirements:

    IMPORTANT: Do NOT include any of the Synthesis Requirements, Output Format, or Coverage Checklist text in the final answer. The final answer must contain ONLY the report sections and their content. Begin your answer directly with "## Executive Summary".

	## Coverage Checklist (DO NOT STOP until ALL are satisfied):
	✓ Each of the %d research areas has a dedicated subsection (### heading)
	✓ Each subsection contains 250–400 words minimum
	✓ Executive Summary captures key insights (250–400 words)
%s    ✓ Response written in the SAME language as the query

    ## CRITICAL - Language Matching:
    %s
    The user's query is in %s. You MUST respond in the SAME language.
    DO NOT translate or switch to English unless the query is in English.

    %s

    ## Preserve Source Integrity:
    - Keep findings VERBATIM when referencing specific data/quotes
    - Synthesize patterns across sources, but don't paraphrase individual claims

    ## Quantitative Synthesis Requirements:
    - When data/numbers/metrics are available in agent results: CREATE MARKDOWN TABLES when appropriate
    - Tables should compare: size, growth rates, market share, performance metrics, costs, timelines
%s    - If significant quantitative data exists but isn't tabulated, briefly note limitations: "Data not directly comparable due to..."
    - Prioritize specific numbers over vague descriptors (e.g., "$5.2B revenue" not "significant revenue")

    %s
    %s

%s
    `, len(areas), coverageExtra, languageInstruction, queryLanguage, citationGuidance, quantitativeCitationLine, outputStructure, areasInstruction, qualityStandards)
		} else {
			// Lightweight summarizer (non-research): no heavy structure or checklists
			fmt.Fprintf(&b, "# Synthesis Requirements:\n\n")
			fmt.Fprintf(&b, "%s\n", languageInstruction)
			fmt.Fprintf(&b, "Produce a concise, directly helpful answer. Avoid unnecessary headings.\n")
			fmt.Fprintf(&b, "Do not include a \"Sources\" section; the system appends sources if needed.\n")
			// Add Citation Agent guidance for non-research mode too
			if citationAgentEnabled {
				fmt.Fprintf(&b, "\n## Citation Handling:\n")
				fmt.Fprintf(&b, "- DO NOT add any inline citations [n] to your response\n")
				fmt.Fprintf(&b, "- A separate Citation Agent will add citations after you finish\n")
				fmt.Fprintf(&b, "- When referencing information, write naturally without citation markers\n\n")
			}
		}

		// Include available citations if present (Phase 2.5 fix)
		if input.Context != nil {
			if citationList, ok := input.Context["available_citations"].(string); ok && citationList != "" {
				// Change wording based on Citation Agent mode
				if citationAgentEnabled {
					fmt.Fprintf(&b, "## Reference Sources (for your information - do NOT add [n] markers):\n%s\n", citationList)
				} else {
					fmt.Fprintf(&b, "## Available Citations (use these in your synthesis):\n%s\n", citationList)
				}
			}
		}
	} // End of !templateUsed fallback block

	// Configure maxAgents based on workflow type (must be after isResearch is determined)
	maxAgents := 6
	if isResearch || len(input.AgentResults) > 10 {
		// For research workflows or many agents, include all agents (up to 50)
		// to avoid losing intermediate synthesis results from React loops
		maxAgents = 50
		logger.Info("Increased maxAgents for research synthesis",
			zap.Int("maxAgents", maxAgents),
			zap.Int("totalAgents", len(input.AgentResults)),
		)
	}

	fmt.Fprintf(&b, "Agent results (%d total):\n\n", len(input.AgentResults))

	// Prioritize intermediate synthesis results (react-synthesizer, synthesizer agents)
	// by including them first, then individual agent outputs
	var synthesisResults []AgentExecutionResult
	var otherResults []AgentExecutionResult

	for _, r := range input.AgentResults {
		if !r.Success || r.Response == "" {
			continue
		}
		// Prioritize synthesis/aggregation agents
		if strings.Contains(strings.ToLower(r.AgentID), "synthesis") ||
			strings.Contains(strings.ToLower(r.AgentID), "synthesizer") {
			synthesisResults = append(synthesisResults, r)
		} else {
			otherResults = append(otherResults, r)
		}
	}

	// Ordering matters: for deep research, avoid anchoring on "(Synthesis)" agent outputs.
	// Use raw agent outputs as the primary evidence, and treat synthesis agents as coverage guides.
	count := 0

	emitAgent := func(r AgentExecutionResult, isSynth bool, maxChars int) {
		if count >= maxAgents {
			return
		}
		// Sanitize agent output to remove duplicate sources/citations
		sanitized := sanitizeAgentOutput(r.Response)
		// Apply length cap after sanitization
		if len([]rune(sanitized)) > maxChars {
			sanitized = string([]rune(sanitized)[:maxChars]) + "..."
		}
		if isSynth {
			fmt.Fprintf(&b, "=== Agent %s (Synthesis) ===\n%s\n\n", r.AgentID, sanitized)
		} else {
			fmt.Fprintf(&b, "=== Agent %s ===\n%s\n\n", r.AgentID, sanitized)
		}
		count++
	}

	includeSynthesisFirst := !isResearch
	synthChars := maxPerAgentChars * 2
	if isResearch {
		// Keep synthesis outputs smaller in research mode to reduce "summary-of-summary" dominance.
		synthChars = maxPerAgentChars
	}

	if includeSynthesisFirst {
		for _, r := range synthesisResults {
			emitAgent(r, true, synthChars)
		}
		for _, r := range otherResults {
			emitAgent(r, false, maxPerAgentChars)
		}
	} else {
		for _, r := range otherResults {
			emitAgent(r, false, maxPerAgentChars)
		}
		for _, r := range synthesisResults {
			emitAgent(r, true, synthChars)
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
				Timestamp: time.Now(),
			})
			// Emit friendly summary with tokens
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    MsgTokensUsed(res.TokensUsed),
				Timestamp:  time.Now(),
			})
			// Emit completion
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    MsgFinalAnswer(),
				Timestamp:  time.Now(),
			})
		}
		return res, nil
	}

	// Use /agent/query to leverage role presets and proper model selection
	base := getenv("LLM_SERVICE_URL", "http://llm-service:8000")

	// Calculate max_tokens for synthesis without a hard ceiling.
	// Increase allowance per agent to reduce risk of early stops.
	// Base: 10240, plus 2048 per agent result.
	maxTokens := 10240 + (len(input.AgentResults) * 2048)
	// For deep research (comprehensive style), enforce a 50k floor before provider headroom clamp.
	if strings.EqualFold(synthesisStyle, "comprehensive") && maxTokens < 50000 {
		maxTokens = 50000
	}
	diagLogger.Info("Synthesis max_tokens calculated",
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

	// Explicitly set model_tier if present in context to avoid Python API defaulting to "small"
	if contextMap != nil {
		if tierVal, ok := contextMap["model_tier"]; ok {
			if tierStr, ok := tierVal.(string); ok && tierStr != "" {
				reqBody["model_tier"] = tierStr
			}
		}
	}

	// If role is present, ensure it's in context
	if role != "" {
		reqBody["context"].(map[string]interface{})["role"] = role
		logger.Info("Synthesis using role-aware endpoint", zap.String("role", role))
	}

	// Add synthesis mode for observability
	reqBody["context"].(map[string]interface{})["mode"] = "synthesis"

	// Debug prompt stats (approximate token estimate)
	promptStr := b.String()
	diagLogger.Info("Synthesis prompt stats",
		zap.Int("chars", len([]rune(promptStr))),
		zap.Int("approx_tokens", len([]rune(promptStr))/4),
		zap.Int("agent_results", len(input.AgentResults)),
		zap.Int("requested_max_tokens", maxTokens),
		zap.Bool("removed_available_citations_from_context", removedCitations),
		zap.Int("removed_citations_chars", removedCitationsChars),
	)

	buf, _ := json.Marshal(reqBody)
	url := base + "/agent/query"

	// Timeout based on research mode: deep research needs more time for large context
	timeout := 180 * time.Second // Default: 3 minutes (non-research)
	if isResearch {
		timeout = 300 * time.Second // 5 minutes for all research syntheses (temporarily increased for testing)
	}

	httpClient := &http.Client{
		Timeout:   timeout,
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
		// Emit fallback warning to SSE stream
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventErrorRecovery),
				AgentID:    "synthesis",
				Message:    MsgSynthesisFallback("LLM service unavailable"),
				Timestamp:  time.Now(),
			})
		}
		res, serr := simpleSynthesisNoEvents(ctx, input)
		if serr != nil {
			return res, serr
		}
		// Append citations saved earlier (if any) to ensure Sources are preserved
		if savedCitations != "" {
			res.FinalResult = formatting.FormatReportWithCitations(res.FinalResult, savedCitations)
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
				Timestamp: time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    MsgTokensUsed(res.TokensUsed),
				Timestamp:  time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    MsgFinalAnswer(),
				Timestamp:  time.Now(),
			})
		}
		return res, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Warn("LLM synthesis: non-2xx, falling back", zap.Int("status", resp.StatusCode))
		// Emit fallback warning to SSE stream
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventErrorRecovery),
				AgentID:    "synthesis",
				Message:    MsgSynthesisFallback("LLM returned error"),
				Timestamp:  time.Now(),
			})
		}
		res, serr := simpleSynthesisNoEvents(ctx, input)
		if serr != nil {
			return res, serr
		}
		// Append citations saved earlier (if any) to ensure Sources are preserved
		if savedCitations != "" {
			res.FinalResult = formatting.FormatReportWithCitations(res.FinalResult, savedCitations)
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
				Timestamp: time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    MsgTokensUsed(res.TokensUsed),
				Timestamp:  time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    MsgFinalAnswer(),
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
		// Append citations saved earlier (if any) to ensure Sources are preserved
		if savedCitations != "" {
			res.FinalResult = formatting.FormatReportWithCitations(res.FinalResult, savedCitations)
		}
		// Emit standard 3-event sequence (fallback path)
		if wfID != "" {
			payload := map[string]interface{}{
				"tokens_used": res.TokensUsed,
			}
			// Include citations if available (already in correct format from workflow)
			if input.CollectedCitations != nil {
				payload["citations"] = input.CollectedCitations
			}
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    "synthesis",
				Message:    truncateQuery(res.FinalResult, MaxSynthesisOutputChars),
				Payload:    payload,
				Timestamp:  time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    MsgSynthesisSummary(res.TokensUsed),
				Timestamp:  time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    MsgFinalAnswer(),
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
		// Emit fallback warning to SSE stream
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventErrorRecovery),
				AgentID:    "synthesis",
				Message:    MsgSynthesisFallback("response decode failed"),
				Timestamp:  time.Now(),
			})
		}
		res, serr := simpleSynthesisNoEvents(ctx, input)
		if serr != nil {
			return res, serr
		}
		// Append citations saved earlier (if any) to ensure Sources are preserved
		if savedCitations != "" {
			res.FinalResult = formatting.FormatReportWithCitations(res.FinalResult, savedCitations)
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
				Timestamp: time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    MsgSynthesisSummary(res.TokensUsed),
				Timestamp:  time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    MsgFinalAnswer(),
				Timestamp:  time.Now(),
			})
		}
		return res, nil
	}

	if out.Response == "" {
		logger.Warn("LLM synthesis: empty response, falling back",
			zap.String("raw", truncateForLog(string(rawBody), 2000)),
		)
		// Emit fallback warning to SSE stream
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventErrorRecovery),
				AgentID:    "synthesis",
				Message:    MsgSynthesisFallback("LLM returned empty response"),
				Timestamp:  time.Now(),
			})
		}
		res, serr := simpleSynthesisNoEvents(ctx, input)
		if serr != nil {
			return res, serr
		}
		// Append citations saved earlier (if any) to ensure Sources are preserved
		if savedCitations != "" {
			res.FinalResult = formatting.FormatReportWithCitations(res.FinalResult, savedCitations)
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
				Timestamp: time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    MsgSynthesisSummary(res.TokensUsed),
				Timestamp:  time.Now(),
			})
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventDataProcessing),
				AgentID:    "synthesis",
				Message:    MsgFinalAnswer(),
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
	// Use savedCitations (preserved before deletion) instead of input.Context
	finalResponse := out.Response
	if savedCitations != "" {
		finalResponse = formatting.FormatReportWithCitations(finalResponse, savedCitations)
	}

	// Continuation fallback: if model stopped early and output looks incomplete, ask it to continue
	// This function is STRUCTURE-AGNOSTIC - it only checks for truncation signals,
	// not specific heading formats or per-section requirements (those belong in templates).
	looksComplete := func(s string) bool {
		txt := strings.TrimSpace(s)
		if txt == "" {
			return false
		}
		runes := []rune(txt)
		last := runes[len(runes)-1]

		// Check for sentence-ending punctuation (ASCII + CJK)
		if last == '.' || last == '!' || last == '?' || last == '"' || last == ')' || last == ']' ||
			last == '。' || last == '！' || last == '？' || last == '』' || last == '」' {
			// Also check for incomplete phrases at the end
			tail := strings.ToLower(txt)
			if len(tail) > 40 {
				tail = tail[len(tail)-40:]
			}
			bad := []string{" and", " or", " with", " to", " of", ":", "、", "と", "や", "の"}
			for _, b := range bad {
				if strings.HasSuffix(tail, b) {
					return false
				}
			}

			// Style-aware minimum length check (structure-agnostic)
			// Instead of checking specific headings, use total length thresholds
			minLength := 1000 // Default: ~250 words
			if input.Context != nil {
				// Allow explicit override via context for custom templates
				// This takes highest precedence over style/area-based calculations
				if explicitMin, ok := input.Context["synthesis_min_length"]; ok {
					switch v := explicitMin.(type) {
					case int:
						minLength = v
					case int64:
						minLength = int(v)
					case float64:
						minLength = int(v)
					}
				} else {
					// Style-based defaults
					if style, ok := input.Context["synthesis_style"].(string); ok {
						switch style {
						case "comprehensive":
							minLength = 3000 // ~750 words for deep research
						case "concise":
							minLength = 500 // ~125 words for concise mode
						}
					}
					// If research areas are specified, scale minimum by area count
					if rawAreas, ok := input.Context["research_areas"]; ok && rawAreas != nil {
						var areaCount int
						switch t := rawAreas.(type) {
						case []string:
							areaCount = len(t)
						case []interface{}:
							areaCount = len(t)
						}
						if areaCount > 0 {
							// ~400 chars per area minimum (comprehensive expects more)
							areaMin := areaCount * 400
							if areaMin > minLength {
								minLength = areaMin
							}
						}
					}
				}
			}

			// Check if response meets minimum length threshold
			if len(runes) < minLength {
				return false
			}

			return true
		}

		// Ends with incomplete punctuation or mid-word
		return false
	}

	// Extract finish_reason and completion tokens (may be empty)
	finishReason := ""
	outputTokens := 0
	effectiveMaxCompletion := maxTokens
	if out.Metadata != nil {
		if fr, ok := out.Metadata["finish_reason"].(string); ok {
			if finishReason == "" {
				finishReason = fr
			}
		}
		if ot, ok := out.Metadata["output_tokens"].(float64); ok {
			outputTokens = int(ot)
		} else if ot, ok := out.Metadata["output_tokens"].(int); ok {
			outputTokens = ot
		}
		if emc, ok := out.Metadata["effective_max_completion"].(int); ok && emc > 0 {
			effectiveMaxCompletion = emc
		} else if emc, ok := out.Metadata["effective_max_completion"].(float64); ok && emc > 0 {
			effectiveMaxCompletion = int(emc)
		}
	}

	// Log continuation decision context
	diagLogger.Info("Synthesis continuation decision",
		zap.String("finish_reason", finishReason),
		zap.Int("completion_tokens", outputTokens),
		zap.Int("effective_max_completion", effectiveMaxCompletion),
		zap.Bool("looks_complete", looksComplete(finalResponse)),
	)

	// Trigger continuation if there's insufficient remaining capacity
	// Use adaptive threshold: min(25% of effective_max, 300 tokens absolute margin)
	minMargin := effectiveMaxCompletion / 4
	if minMargin > 300 {
		minMargin = 300
	}
	remainingTokens := effectiveMaxCompletion - outputTokens

	if finishReason == "stop" && !looksComplete(finalResponse) && remainingTokens < minMargin {
		diagLogger.Info("Triggering synthesis continuation",
			zap.Int("completion_tokens", outputTokens),
			zap.Int("effective_max_completion", effectiveMaxCompletion),
			zap.Int("remaining_tokens", remainingTokens),
			zap.Int("min_margin", minMargin),
		)
		rs := []rune(finalResponse)
		start := 0
		if len(rs) > 2000 {
			start = len(rs) - 2000
		}
		excerpt := string(rs[start:])

		contQuery := "Continue the previous synthesis in the SAME language.\n" +
			"Instructions:\n" +
			"- Continue from the last sentence; do NOT repeat earlier content.\n" +
			"- Maintain the same headings and inline citation style.\n" +
			"- Output ONLY the continuation text (no preamble).\n\n" +
			"Previous excerpt:\n" + excerpt

		contMax := maxTokens / 2
		if contMax > 4096 {
			contMax = 4096
		}

		contBody, _ := json.Marshal(map[string]interface{}{
			"query":         contQuery,
			"context":       contextMap,
			"allowed_tools": []string{},
			"agent_id":      "synthesis-continue",
			"max_tokens":    contMax,
		})

		creq, cerr := http.NewRequestWithContext(ctx, http.MethodPost, base+"/agent/query", bytes.NewReader(contBody))
		if cerr == nil {
			creq.Header.Set("Content-Type", "application/json")
			if wfID != "" {
				creq.Header.Set("X-Parent-Workflow-ID", wfID)
			}
			if cresp, cerr := httpClient.Do(creq); cerr == nil && cresp != nil && cresp.StatusCode >= 200 && cresp.StatusCode < 300 {
				defer cresp.Body.Close()
				var cdata struct {
					Success      bool           `json:"success"`
					Response     string         `json:"response"`
					TokensUsed   int            `json:"tokens_used"`
					ModelUsed    string         `json:"model_used"`
					Provider     string         `json:"provider"`
					FinishReason string         `json:"finish_reason"`
					Metadata     map[string]any `json:"metadata"`
				}
				if json.NewDecoder(cresp.Body).Decode(&cdata) == nil && cdata.Success {
					diagLogger.Info("Continuation succeeded",
						zap.Int("cont_tokens_used", cdata.TokensUsed),
						zap.String("cont_finish_reason", cdata.FinishReason),
					)
					finalResponse = strings.TrimRight(finalResponse, "\n") + "\n\n" + strings.TrimSpace(cdata.Response)
					if input.Context != nil {
						if citationList, ok := input.Context["available_citations"].(string); ok && citationList != "" {
							finalResponse = formatting.FormatReportWithCitations(finalResponse, citationList)
						}
					}
					out.TokensUsed += cdata.TokensUsed
					if cdata.FinishReason != "" {
						finishReason = cdata.FinishReason
					}
				}
			}
		}
	} else {
		diagLogger.Info("Continuation not triggered",
			zap.String("reason", func() string {
				if finishReason != "stop" {
					return "finish_reason_not_stop"
				}
				if looksComplete(finalResponse) {
					return "looks_complete"
				}
				return "budget_threshold"
			}()),
		)
	}

	// Extract usage metadata for event payload (finishReason, outputTokens, effectiveMaxCompletion already extracted above)
	provider := ""
	inputTokens := 0
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
		payload := map[string]interface{}{
			"tokens_used":          out.TokensUsed,
			"model_used":           model,
			"provider":             provider,
			"input_tokens":         inputTokens,
			"output_tokens":        outputTokens,
			"cost_usd":             costUsd,
			"finish_reason":        finishReason,
			"requested_max_tokens": maxTokens,
		}

		// Include citations if available (already in correct format from workflow)
		if input.CollectedCitations != nil {
			payload["citations"] = input.CollectedCitations
			diagLogger.Info("Including citations in SSE event")
		}

		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventLLMOutput),
			AgentID:    "synthesis",
			Message:    truncateQuery(finalResponse, MaxSynthesisOutputChars),
			Payload:    payload,
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
			Message:    MsgFinalAnswer(),
			Timestamp:  time.Now(),
		})
	}

	// Set default finish_reason if not already extracted
	if finishReason == "" {
		finishReason = "stop"
	}

	// effectiveMaxCompletion, outputTokens already extracted above for continuation trigger

	// Infer input tokens if not present in metadata
	if inputTokens == 0 && out.TokensUsed > 0 && outputTokens > 0 {
		est := out.TokensUsed - outputTokens
		if est > 0 {
			inputTokens = est
		}
	}

	return SynthesisResult{
		FinalResult:            finalResponse,
		TokensUsed:             out.TokensUsed,
		FinishReason:           finishReason,
		RequestedMaxTokens:     maxTokens,
		CompletionTokens:       outputTokens,
		EffectiveMaxCompletion: effectiveMaxCompletion,
		InputTokens:            inputTokens,
		ModelUsed:              model,
		Provider:               provider,
		CostUsd:                costUsd,
	}, nil
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

	input.AgentResults = preprocessAgentResults(input.AgentResults, logger)
	if len(input.AgentResults) == 0 {
		return SynthesisResult{}, fmt.Errorf("no agent results to synthesize")
	}

	var resultParts []string
	totalTokens := 0
	totalInputTokens := 0
	totalOutputTokens := 0
	var totalCostUsd float64
	var modelUsed string
	var provider string

	for _, result := range input.AgentResults {
		if result.Success && result.Response != "" {
			// Clean up raw outputs for better readability
			cleaned := cleanAgentOutput(result.Response)
			if cleaned != "" {
				resultParts = append(resultParts, cleaned)
				totalTokens += result.TokensUsed
				totalInputTokens += result.InputTokens
				totalOutputTokens += result.OutputTokens

				// Capture model and provider from first successful agent
				if modelUsed == "" && result.ModelUsed != "" {
					modelUsed = result.ModelUsed
				}
				if provider == "" && result.Provider != "" {
					provider = result.Provider
				}
			}
		}
	}

	if len(resultParts) == 0 {
		return SynthesisResult{}, fmt.Errorf("no successful agent results")
	}

	// Combine results without exposing internal details
	finalResult := strings.Join(resultParts, "\n\n")

	// Estimate cost if not already calculated
	if totalInputTokens > 0 && totalOutputTokens > 0 && modelUsed != "" {
		totalCostUsd = pricing.CostForSplit(modelUsed, totalInputTokens, totalOutputTokens)
	}

	logger.Info("Synthesis (simple) completed",
		zap.Int("total_tokens", totalTokens),
		zap.Int("input_tokens", totalInputTokens),
		zap.Int("output_tokens", totalOutputTokens),
		zap.Float64("cost_usd", totalCostUsd),
		zap.String("model", modelUsed),
		zap.String("provider", provider),
		zap.Int("successful_agents", len(resultParts)),
	)

	return SynthesisResult{
		FinalResult:      finalResult,
		TokensUsed:       totalTokens,
		InputTokens:      totalInputTokens,
		CompletionTokens: totalOutputTokens,
		ModelUsed:        modelUsed,
		Provider:         provider,
		CostUsd:          totalCostUsd,
		FinishReason:     "stop", // Simple synthesis always completes
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
			Timestamp: time.Now(),
		})
		// Emit a simple summary with tokens
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventDataProcessing),
			AgentID:    "synthesis",
			Message:    MsgTokensUsed(res.TokensUsed),
			Timestamp:  time.Now(),
		})
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventDataProcessing),
			AgentID:    "synthesis",
			Message:    MsgFinalAnswer(),
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
		// Format search results as a readable list (without header to avoid injection)
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
			// Return clean list without "Research findings:" header
			// This prevents intermediate formatting from appearing in final synthesis
			return strings.Join(formatted, "\n")
		}
	}

	// Return as-is if not JSON or already clean text
	return response
}

// countInlineCitations counts unique inline citation references [n] in text.
// Returns the number of distinct citation numbers found.
func countInlineCitations(text string) int {
	re := regexp.MustCompile(`\[\d+\]`)
	matches := re.FindAllString(text, -1)
	// Deduplicate (same citation can appear multiple times)
	seen := make(map[string]bool)
	for _, m := range matches {
		seen[m] = true
	}
	return len(seen)
}
