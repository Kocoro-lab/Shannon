package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	"go.temporal.io/sdk/activity"
)

// CitationForAgent is a simplified citation structure for the Citation Agent
// (avoids import cycle with metadata package)
type CitationForAgent struct {
	URL              string  `json:"url"`
	Title            string  `json:"title"`
	Source           string  `json:"source"`
	Snippet          string  `json:"snippet"`
	CredibilityScore float64 `json:"credibility_score"`
	QualityScore     float64 `json:"quality_score"`
}

// CitationAgentInput is the input for the Citation Agent activity
type CitationAgentInput struct {
	Report           string             `json:"report"`
	Citations        []CitationForAgent `json:"citations"`
	ParentWorkflowID string             `json:"parent_workflow_id,omitempty"`
	Context          map[string]interface{} `json:"context,omitempty"`
}

// CitationAgentResult is the result of the Citation Agent activity
type CitationAgentResult struct {
	CitedReport       string   `json:"cited_report"`
	CitationsUsed     []int    `json:"citations_used"`
	ValidationPassed  bool     `json:"validation_passed"`
	ValidationError   string   `json:"validation_error,omitempty"`
	PlacementWarnings []string `json:"placement_warnings,omitempty"`
	RedundantCount    int      `json:"redundant_count"`
	TokensUsed        int      `json:"tokens_used"`
	ModelUsed         string   `json:"model_used"`
	Provider          string   `json:"provider"`
	InputTokens       int      `json:"input_tokens"`
	OutputTokens      int      `json:"output_tokens"`
}

// AddCitations adds inline citations to a report using LLM
func (a *Activities) AddCitations(ctx context.Context, input CitationAgentInput) (*CitationAgentResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("CitationAgent: starting",
		"report_length", len(input.Report),
		"citations_count", len(input.Citations),
	)

	// If no citations available, return original report
	if len(input.Citations) == 0 {
		return &CitationAgentResult{
			CitedReport:      input.Report,
			ValidationPassed: true,
		}, nil
	}

	// Build the prompt
	systemPrompt := buildCitationAgentPrompt()
	userContent := buildCitationUserContent(input.Report, input.Citations)

	// Call LLM service
	llmServiceURL := os.Getenv("LLM_SERVICE_URL")
	if llmServiceURL == "" {
		llmServiceURL = "http://llm-service:8000"
	}
	url := fmt.Sprintf("%s/agent/query", llmServiceURL)

	reqBody := map[string]interface{}{
		"query":       userContent,
		"max_tokens":  8192,
		"temperature": 0.1, // Low temperature for consistent citation placement
		"agent_id":    "citation_agent",
		"model_tier":  "small", // Use small model for cost efficiency
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
		Timeout:   90 * time.Second,
		Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(reqJSON)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "citation_agent")
	if input.ParentWorkflowID != "" {
		req.Header.Set("X-Workflow-ID", input.ParentWorkflowID)
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("CitationAgent: LLM call failed, returning original report", "error", err)
		return &CitationAgentResult{
			CitedReport:      input.Report,
			ValidationPassed: false,
			ValidationError:  err.Error(),
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Warn("CitationAgent: HTTP error, returning original report", "status", resp.StatusCode)
		return &CitationAgentResult{
			CitedReport:      input.Report,
			ValidationPassed: false,
			ValidationError:  fmt.Sprintf("HTTP %d", resp.StatusCode),
		}, nil
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
		logger.Warn("CitationAgent: failed to parse response, returning original", "error", err)
		return &CitationAgentResult{
			CitedReport:      input.Report,
			ValidationPassed: false,
			ValidationError:  err.Error(),
		}, nil
	}

	// Extract cited report from tags
	citedReport := extractCitedReport(llmResp.Response)
	if citedReport == "" {
		citedReport = llmResp.Response // Fallback to raw response
	}

	result := &CitationAgentResult{
		TokensUsed:   llmResp.TokensUsed,
		ModelUsed:    llmResp.ModelUsed,
		Provider:     llmResp.Provider,
		InputTokens:  llmResp.Metadata.InputTokens,
		OutputTokens: llmResp.Metadata.OutputTokens,
	}

	// Extract used citations first to check if any were added
	usedCitations := extractUsedCitationNumbers(citedReport)

	// If no citations were added, LLM may have made minor modifications
	// In this case, just return the original report as "valid" (no citations needed)
	if len(usedCitations) == 0 {
		logger.Info("CitationAgent: no citations added by LLM, using original report")
		result.CitedReport = input.Report
		result.CitationsUsed = nil
		result.ValidationPassed = true
		result.PlacementWarnings = []string{"LLM did not add any citations - using original report"}
		return result, nil
	}

	// Level 1 Validation: Content immutability (only if citations were added)
	if valid, err := validateContentImmutability(input.Report, citedReport); !valid {
		logger.Warn("CitationAgent: content modified, using original report",
			"error", err,
			"citations_added", len(usedCitations),
		)
		// Content was modified beyond just adding citations
		// Return original report but mark as success (we're returning valid content)
		result.CitedReport = input.Report
		result.CitationsUsed = nil
		result.ValidationPassed = true
		result.PlacementWarnings = []string{
			fmt.Sprintf("LLM modified content beyond citations (%s) - using original report", err.Error()),
		}
		return result, nil
	}

	// Level 1 Validation: Citation number validity
	if invalid := validateCitationNumbers(citedReport, len(input.Citations)); len(invalid) > 0 {
		logger.Warn("CitationAgent: invalid citation numbers, removing", "invalid", invalid)
		citedReport = removeInvalidCitations(citedReport, invalid)
		// Re-extract after removal
		usedCitations = extractUsedCitationNumbers(citedReport)
	}

	// Level 2 Validation: Placement warnings (non-blocking)
	warnings := validateCitationPlacement(citedReport)
	if len(warnings) > 0 {
		logger.Info("CitationAgent: placement warnings", "count", len(warnings))
	}

	// Level 2 Validation: Redundancy detection
	redundant := detectRedundantCitations(citedReport)

	result.CitedReport = citedReport
	result.CitationsUsed = usedCitations
	result.ValidationPassed = true
	result.PlacementWarnings = warnings
	result.RedundantCount = len(redundant)

	logger.Info("CitationAgent: complete",
		"citations_used", len(usedCitations),
		"warnings", len(warnings),
		"redundant", len(redundant),
	)

	return result, nil
}

// buildCitationAgentPrompt returns the system prompt for the Citation Agent
func buildCitationAgentPrompt() string {
	return `You are a citation specialist. Your ONLY job is to add [n] citation markers to a report.

## CRITICAL RULES - FOLLOW EXACTLY

### Rule 1: PERFECT COPYING
You MUST output the EXACT same text as the input, character for character.
- Do NOT rephrase, correct grammar, fix typos, or improve wording
- Do NOT add explanations, introductions, or conclusions
- Do NOT remove any content
- Do NOT add a "## Sources" section (the system handles this)
- The ONLY change allowed: inserting [n] markers

### Rule 2: CITE BASED ON RELEVANCE
- Add [n] when the citation CLEARLY relates to the claim
- Use ALL available signals: URL path, title, domain, AND snippet content
- If snippet is empty, you can still cite if URL/title clearly matches the topic
- Example: URL "example.com/blog/2024-review" supports claims about "2024 review"
- Do NOT cite sources that have NO connection to the claim
- When genuinely uncertain, leave uncited rather than guessing

### Rule 3: HOW TO ADD CITATIONS
- Insert [n] at the END of sentences/clauses that contain factual claims
- Match citation [n] to the source that SUPPORTS that specific claim
- Use the citation number from the Available Citations list (e.g., [1], [2], [3])

Examples of CORRECT placement:
- "Revenue grew 19% year over year. [1]"
- "The company was founded in 2020 [2] and has since expanded globally."

Examples of WRONG placement:
- "[1] Revenue grew 19%." ← citation at start
- "Revenue [1] grew 19%." ← citation in middle of fact

### Rule 4: WHAT TO CITE
✓ Cite: Statistics, dates, named entities, specific claims, quotations
✓ Cite: Conclusions that depend on source evidence
✗ Skip: Common knowledge, section headers, transitions, opinions without data
✗ Skip: Claims where no available citation provides supporting evidence

### Rule 5: MATCHING STRATEGY
Look at each Available Citation's title, URL, and Content snippet.
Match claims in the report to citations based on:
1. Topic relevance (same subject matter)
2. Data correspondence (same numbers, dates, names)
3. Source authority (official sites, news outlets)

If no citation clearly matches a claim, DO NOT add a citation there.

## OUTPUT FORMAT
<cited_report>
[Your cited version of the report - EXACTLY the same text with [n] markers added]
</cited_report>
`
}

// buildCitationUserContent builds the user content for the Citation Agent
func buildCitationUserContent(report string, citations []CitationForAgent) string {
	var sb strings.Builder

	sb.WriteString("## Available Citations:\n")
	for i, c := range citations {
		title := c.Title
		if title == "" {
			title = c.Source
		}
		snippet := c.Snippet
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		sb.WriteString(fmt.Sprintf("[%d] %s (%s)\n", i+1, title, c.URL))
		if snippet != "" {
			sb.WriteString(fmt.Sprintf("    Content: %s\n", snippet))
		}
	}

	sb.WriteString("\n## Report to Cite:\n")
	sb.WriteString(report)
	sb.WriteString("\n\nAdd citations and output within <cited_report> tags:")

	return sb.String()
}

// extractCitedReport extracts content from <cited_report> tags
func extractCitedReport(response string) string {
	startTag := "<cited_report>"
	endTag := "</cited_report>"

	startIdx := strings.Index(response, startTag)
	endIdx := strings.LastIndex(response, endTag)

	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return ""
	}

	return strings.TrimSpace(response[startIdx+len(startTag) : endIdx])
}

// validateContentImmutability checks that cited report only differs by [n] markers
func validateContentImmutability(original, cited string) (bool, error) {
	// Remove all citation markers [n] (with optional preceding space)
	citationPattern := regexp.MustCompile(`\s*\[\d{1,3}\]`)
	stripped := citationPattern.ReplaceAllString(cited, "")

	// Normalize whitespace: trim both ends and collapse multiple spaces/newlines
	originalNorm := normalizeForComparison(original)
	strippedNorm := normalizeForComparison(stripped)

	if originalNorm != strippedNorm {
		// Find first difference for debugging
		minLen := len(originalNorm)
		if len(strippedNorm) < minLen {
			minLen = len(strippedNorm)
		}
		diffPos := 0
		for i := 0; i < minLen; i++ {
			if originalNorm[i] != strippedNorm[i] {
				diffPos = i
				break
			}
		}
		// If lengths differ but content matched up to minLen, diff is at end
		if diffPos == 0 && len(originalNorm) != len(strippedNorm) {
			diffPos = minLen
		}
		context := ""
		start := diffPos - 20
		if start < 0 {
			start = 0
		}
		end := diffPos + 20
		if end > len(originalNorm) {
			end = len(originalNorm)
		}
		if end > start {
			context = originalNorm[start:end]
		}
		return false, fmt.Errorf("content modified near position %d: ...%s...", diffPos, context)
	}
	return true, nil
}

// normalizeForComparison normalizes a string for content comparison
// by trimming whitespace, collapsing multiple spaces, and normalizing line endings
func normalizeForComparison(s string) string {
	// Trim leading and trailing whitespace
	s = strings.TrimSpace(s)
	// Normalize line endings to \n
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	// Collapse multiple newlines to single newline
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	// Collapse multiple spaces to single space (but preserve newlines)
	spacePattern := regexp.MustCompile(`[ \t]+`)
	s = spacePattern.ReplaceAllString(s, " ")
	// Trim trailing spaces on each line
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	s = strings.Join(lines, "\n")

	// Normalize Unicode: some LLMs may output different Unicode representations
	// for visually identical characters (e.g., fullwidth vs halfwidth punctuation)
	// Normalize common Chinese punctuation variations
	replacements := map[string]string{
		"：": ":",  // fullwidth colon to ASCII
		"，": ",",  // Chinese comma to ASCII
		"。": ".",  // Chinese period to ASCII
		"！": "!",  // fullwidth exclamation
		"？": "?",  // fullwidth question mark
		"（": "(",  // fullwidth parentheses
		"）": ")",
		"【": "[",  // Chinese brackets
		"】": "]",
		"\u201c": `"`, // left double quotation mark
		"\u201d": `"`, // right double quotation mark
		"\u2018": "'", // left single quotation mark
		"\u2019": "'", // right single quotation mark
		"、": ",",  // Chinese enumeration comma
		"；": ";",  // fullwidth semicolon
	}
	for old, repl := range replacements {
		s = strings.ReplaceAll(s, old, repl)
	}

	// Normalize various hyphen/dash characters to ASCII hyphen
	// LLMs often convert between these variants
	hyphenChars := []string{
		"\u2010", // HYPHEN
		"\u2011", // NON-BREAKING HYPHEN
		"\u2012", // FIGURE DASH
		"\u2013", // EN DASH
		"\u2014", // EM DASH
		"\u2015", // HORIZONTAL BAR
		"\u2212", // MINUS SIGN
		"\uFE58", // SMALL EM DASH
		"\uFE63", // SMALL HYPHEN-MINUS
		"\uFF0D", // FULLWIDTH HYPHEN-MINUS
	}
	for _, h := range hyphenChars {
		s = strings.ReplaceAll(s, h, "-")
	}

	return s
}

// validateCitationNumbers returns invalid citation numbers (out of range)
func validateCitationNumbers(cited string, maxCitations int) []int {
	re := regexp.MustCompile(`\[(\d+)\]`)
	matches := re.FindAllStringSubmatch(cited, -1)

	var invalid []int
	seen := make(map[int]bool)
	for _, m := range matches {
		n, _ := strconv.Atoi(m[1])
		if (n < 1 || n > maxCitations) && !seen[n] {
			invalid = append(invalid, n)
			seen[n] = true
		}
	}
	return invalid
}

// removeInvalidCitations removes citations with invalid numbers
func removeInvalidCitations(cited string, invalid []int) string {
	result := cited
	for _, n := range invalid {
		pattern := regexp.MustCompile(fmt.Sprintf(`\s*\[%d\]`, n))
		result = pattern.ReplaceAllString(result, "")
	}
	return result
}

// validateCitationPlacement returns warnings about citation placement issues
func validateCitationPlacement(cited string) []string {
	var warnings []string
	re := regexp.MustCompile(`\[\d+\]`)
	matches := re.FindAllStringIndex(cited, -1)

	for _, m := range matches {
		start := m[0]

		// Check: Citation inside a word
		if start > 0 && isAlphanumeric(cited[start-1]) {
			warnings = append(warnings, fmt.Sprintf("citation inside word at position %d", start))
		}

		// Check: Citation at start of content (likely wrong)
		if start == 0 {
			warnings = append(warnings, "citation at very start of content")
		}
	}

	return warnings
}

// detectRedundantCitations finds same citation used multiple times in same sentence
func detectRedundantCitations(cited string) []string {
	var redundant []string
	sentences := splitIntoSentences(cited)
	re := regexp.MustCompile(`\[(\d+)\]`)

	for _, sent := range sentences {
		matches := re.FindAllStringSubmatch(sent, -1)
		seen := make(map[string]int)
		for _, m := range matches {
			seen[m[1]]++
		}
		for num, count := range seen {
			if count > 1 {
				redundant = append(redundant, fmt.Sprintf("[%s] x%d in sentence", num, count))
			}
		}
	}
	return redundant
}

// extractUsedCitationNumbers extracts unique citation numbers used
func extractUsedCitationNumbers(cited string) []int {
	re := regexp.MustCompile(`\[(\d+)\]`)
	matches := re.FindAllStringSubmatch(cited, -1)

	seen := make(map[int]bool)
	var used []int
	for _, m := range matches {
		n, _ := strconv.Atoi(m[1])
		if !seen[n] {
			seen[n] = true
			used = append(used, n)
		}
	}
	return used
}

// isAlphanumeric checks if a byte is alphanumeric
func isAlphanumeric(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// splitIntoSentences splits text into sentences (simple implementation)
func splitIntoSentences(text string) []string {
	// Simple split on sentence-ending punctuation
	re := regexp.MustCompile(`[.!?]+\s+`)
	parts := re.Split(text, -1)
	var sentences []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			sentences = append(sentences, p)
		}
	}
	return sentences
}
