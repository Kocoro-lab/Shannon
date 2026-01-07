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

// citationNumberPattern matches inline citations like [1], [2], etc. with capture group.
// Compiled once at package level for performance.
var citationNumberPattern = regexp.MustCompile(`\[(\d+)\]`)

// citationMarkerPattern matches inline citations without capture group (for position finding).
var citationMarkerPattern = regexp.MustCompile(`\[\d+\]`)

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
	Report           string                 `json:"report"`
	Citations        []CitationForAgent     `json:"citations"`
	ParentWorkflowID string                 `json:"parent_workflow_id,omitempty"`
	Context          map[string]interface{} `json:"context,omitempty"`
	ModelTier        string                 `json:"model_tier,omitempty"` // "small", "medium", "large"; default "small"
}

// CitationAgentResult is the result of the Citation Agent activity
type CitationAgentResult struct {
	Role              string   `json:"role,omitempty"`
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

	// Extract role for observability (default: citation_agent)
	role := "citation_agent"
	if input.Context != nil {
		if v, ok := input.Context["role"].(string); ok && strings.TrimSpace(v) != "" {
			role = strings.TrimSpace(v)
		}
	}

	logger.Info("CitationAgent: starting",
		"report_length", len(input.Report),
		"citations_count", len(input.Citations),
		"role", role,
	)

	// If no citations available, return original report
	if len(input.Citations) == 0 {
		return &CitationAgentResult{
			Role:             role,
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

	// Determine model tier: use input.ModelTier if set, otherwise default to "small"
	modelTier := input.ModelTier
	if modelTier == "" {
		modelTier = "small"
	}

	reqBody := map[string]interface{}{
		"query":       userContent,
		"max_tokens":  8192,
		"temperature": 0.0,
		"agent_id":    "citation_agent",
		"model_tier":  modelTier,
		"context": map[string]interface{}{
			"system_prompt":      systemPrompt,
			"parent_workflow_id": input.ParentWorkflowID,
		},
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Dynamic timeout based on report length: base 120s + 30s per 1000 chars, max 300s
	reportLen := len(input.Report)
	timeoutSec := 120 + (reportLen/1000)*30
	if timeoutSec > 300 {
		timeoutSec = 300
	}

	client := &http.Client{
		Timeout:   time.Duration(timeoutSec) * time.Second,
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
			Role:             role,
			CitedReport:      input.Report,
			ValidationPassed: false,
			ValidationError:  err.Error(),
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Warn("CitationAgent: HTTP error, returning original report", "status", resp.StatusCode)
		return &CitationAgentResult{
			Role:             role,
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
			Role:             role,
			CitedReport:      input.Report,
			ValidationPassed: false,
			ValidationError:  err.Error(),
		}, nil
	}

	// Extract cited report from tags
	citedReport := extractCitedReport(llmResp.Response)
	if citedReport == "" {
		// Fallback: clean any partial tags from raw response
		citedReport = strings.ReplaceAll(llmResp.Response, "<cited_report>", "")
		citedReport = strings.ReplaceAll(citedReport, "</cited_report>", "")
		citedReport = strings.TrimSpace(citedReport)
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
		result.Role = role
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
		// Return original report and mark validation as failed (citations were not added)
		result.Role = role
		result.CitedReport = input.Report
		result.CitationsUsed = nil
		result.ValidationPassed = false
		result.ValidationError = "content modified beyond citations"
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

	result.Role = role
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
	return `You are a citation specialist. Add [n] markers to support factual claims.

## YOUR GOAL

Add citations that help readers verify key facts. Balance:
- PRECISION: Only cite when source actually supports the claim
- COVERAGE: Most verifiable facts should have citations

Aim to cite 30-50% of factual claims. Not every sentence needs a citation,
but important facts, statistics, and named entities should be cited.

## ABSOLUTE RULE - DO NOT MODIFY TEXT

The ONLY modification allowed is inserting [n] markers. NOTHING ELSE.
Your response will be REJECTED if you change ANY character of the original text.

## CLAIM CLASSIFICATION

Before citing, classify each claim:

### HIGH CONFIDENCE (Always Cite)
- Exact numbers, dates, percentages that appear in source
- Direct quotes or close paraphrases
- Named entities with matching context

Example:
  Claim: "Revenue grew 19% in Q3 2024"
  Source [3]: "...quarterly revenue increased 19%..."
  → CITE [3] - exact data match

### MEDIUM CONFIDENCE (Cite if URL/Title Confirms)
- General facts where snippet is weak but URL/title clearly relevant
- Statements about company/person where source is official site
- Industry facts from authoritative domain

Example:
  Claim: "The company is headquartered in Tokyo"
  Source [5]: (snippet empty, but URL: company.jp/about)
  → CITE [5] - official source, reasonable inference

### LOW CONFIDENCE (Do Not Cite)
- Vague topic overlap without specific fact match
- Source discusses different aspect of same entity
- Your inference, not stated in source

Example:
  Claim: "The team is highly innovative"
  Source [7]: "...announced new AI product..."
  → DO NOT CITE - product announcement ≠ "innovative" judgment

## CITATION PLACEMENT

Insert [n] at END of sentences or clauses:
- ✓ "Revenue grew 19%.[1]"
- ✓ "Founded in 2020[2], the company expanded."
- ✗ "[1] Revenue grew 19%."

## SOURCE PRIORITY

When multiple sources support same claim, prefer:
1. Official sources (.gov, .edu, company sites)
2. Data aggregators (Crunchbase, LinkedIn)
3. Major news (Reuters, TechCrunch)
4. Other sources

## WHAT TO CITE

✓ Statistics, financial figures, dates
✓ Company facts (founding, location, size)
✓ Named people with roles/titles
✓ Specific claims readers would verify
✓ Conclusions based on cited evidence

## WHAT TO SKIP

✗ Section headers, transitions
✗ Common knowledge
✗ Your synthesis language ("This shows that...")
✗ Claims with NO matching source

## AVOID REDUNDANT CITATIONS

- Same source per sentence: cite at most TWICE
- No adjacent duplicates: NEVER use [1][1]

## EXAMPLES

### Example 1 - High Confidence Match
Report: "Tesla delivered 1.8 million vehicles in 2023."
Source [3]: "Tesla Inc. reported annual deliveries of 1.81 million vehicles..."
→ "Tesla delivered 1.8 million vehicles in 2023.[3]"

### Example 2 - Medium Confidence (Official Source)
Report: "PTMind was founded in 2010."
Source [1]: "About Ptengine - Leading A/B Testing..." (ptengine.cn/about_us)
→ "PTMind was founded in 2010.[1]" (official about page)

### Example 3 - Medium Confidence (Empty Snippet, Clear URL)
Report: "The company has offices in Beijing."
Source [8]: (snippet: "", URL: cn.company.com/contact)
→ "The company has offices in Beijing.[8]" (contact page likely has location)

### Example 4 - Low Confidence (Topic Only)
Report: "The company expanded into European markets."
Source [5]: "TechCorp announced AI product launch..."
→ "The company expanded into European markets." (no citation - wrong topic)

### Example 5 - Correctly Uncited
Report: "This demonstrates strong market positioning."
→ "This demonstrates strong market positioning." (your analysis, not citable)

### Example 6 - Multiple Sources
Report: "Series B funding was led by Investor X."
Source [10]: Crunchbase funding page
Source [15]: News article mentioning same
→ "Series B funding was led by Investor X.[10]" (prefer Crunchbase)

## OUTPUT FORMAT

<cited_report>
[Original text with [n] markers inserted - NO OTHER CHANGES]
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
		if len(snippet) > 500 {
			snippet = snippet[:500] + "..."
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
// Returns (valid, warning, error) - warning is non-empty if minor diffs were tolerated
func validateContentImmutability(original, cited string) (bool, error) {
	// Remove all citation markers [n] (with optional preceding space)
	citationPattern := regexp.MustCompile(`\s*\[\d{1,3}\]`)
	stripped := citationPattern.ReplaceAllString(cited, "")

	// Normalize whitespace: trim both ends and collapse multiple spaces/newlines
	originalNorm := normalizeForComparison(original)
	strippedNorm := normalizeForComparison(stripped)

	if originalNorm == strippedNorm {
		return true, nil
	}

	// Calculate true edit distance ratio using Levenshtein algorithm
	origRunes := []rune(originalNorm)
	strippedRunes := []rune(strippedNorm)

	// For very long texts, use sampled comparison to avoid O(n*m) complexity
	const maxLenForFullEdit = 10000
	var diffRatio float64

	if len(origRunes) > maxLenForFullEdit || len(strippedRunes) > maxLenForFullEdit {
		// Use sampled edit distance for long texts
		diffRatio = sampledEditDistanceRatio(origRunes, strippedRunes)
	} else {
		// Use full Levenshtein for shorter texts
		editDist := levenshteinDistance(origRunes, strippedRunes)
		maxLen := len(origRunes)
		if len(strippedRunes) > maxLen {
			maxLen = len(strippedRunes)
		}
		// Guard against division by zero when both strings are empty
		if maxLen == 0 {
			diffRatio = 0.0
		} else {
			diffRatio = float64(editDist) / float64(maxLen)
		}
	}

	// Tolerate up to 5% difference (LLMs sometimes make minor changes)
	if diffRatio < 0.05 {
		return true, nil
	}

	// Find first difference for error message context
	diffPos := findFirstDifference(origRunes, strippedRunes)

	// Extract context around first difference
	start := diffPos - 20
	if start < 0 {
		start = 0
	}
	end := diffPos + 20
	if end > len(origRunes) {
		end = len(origRunes)
	}
	context := ""
	if end > start {
		context = string(origRunes[start:end])
	}

	return false, fmt.Errorf("content modified (edit_distance=%.2f%%): ...%s...",
		diffRatio*100, context)
}

// levenshteinDistance calculates the true Levenshtein edit distance between two rune slices
// Uses space-optimized O(min(m,n)) algorithm
func levenshteinDistance(s1, s2 []rune) int {
	// Ensure s1 is the shorter one for space optimization
	if len(s1) > len(s2) {
		s1, s2 = s2, s1
	}

	m, n := len(s1), len(s2)
	if m == 0 {
		return n
	}
	if n == 0 {
		return m
	}

	// Only need two rows: previous and current
	prev := make([]int, m+1)
	curr := make([]int, m+1)

	// Initialize first row
	for i := 0; i <= m; i++ {
		prev[i] = i
	}

	// Fill the matrix row by row
	for j := 1; j <= n; j++ {
		curr[0] = j
		for i := 1; i <= m; i++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}
			// min of: delete, insert, replace
			curr[i] = min(min(prev[i]+1, curr[i-1]+1), prev[i-1]+cost)
		}
		// Swap rows
		prev, curr = curr, prev
	}

	return prev[m]
}

// sampledEditDistanceRatio calculates approximate edit distance ratio for long texts
// by sampling multiple chunks and averaging the results
func sampledEditDistanceRatio(s1, s2 []rune) float64 {
	const chunkSize = 500
	const numSamples = 10

	len1, len2 := len(s1), len(s2)
	if len1 == 0 || len2 == 0 {
		if len1 == len2 {
			return 0.0
		}
		return 1.0
	}

	totalRatio := 0.0
	validSamples := 0

	// Sample from beginning, middle, and end
	for i := 0; i < numSamples; i++ {
		// Calculate sample position (spread across the text)
		pos1 := (len1 - chunkSize) * i / (numSamples - 1)
		pos2 := (len2 - chunkSize) * i / (numSamples - 1)

		if pos1 < 0 {
			pos1 = 0
		}
		if pos2 < 0 {
			pos2 = 0
		}

		end1 := pos1 + chunkSize
		end2 := pos2 + chunkSize
		if end1 > len1 {
			end1 = len1
		}
		if end2 > len2 {
			end2 = len2
		}

		chunk1 := s1[pos1:end1]
		chunk2 := s2[pos2:end2]

		if len(chunk1) > 0 && len(chunk2) > 0 {
			editDist := levenshteinDistance(chunk1, chunk2)
			maxChunkLen := len(chunk1)
			if len(chunk2) > maxChunkLen {
				maxChunkLen = len(chunk2)
			}
			totalRatio += float64(editDist) / float64(maxChunkLen)
			validSamples++
		}
	}

	if validSamples == 0 {
		return 1.0
	}

	// Also factor in length difference
	lenDiff := len1 - len2
	if lenDiff < 0 {
		lenDiff = -lenDiff
	}
	maxLen := len1
	if len2 > maxLen {
		maxLen = len2
	}
	lenRatio := float64(lenDiff) / float64(maxLen)

	// Combine sampled edit ratio with length ratio
	avgEditRatio := totalRatio / float64(validSamples)
	// Weight: 80% edit distance, 20% length difference
	return avgEditRatio*0.8 + lenRatio*0.2
}

// findFirstDifference finds the position of the first differing character
func findFirstDifference(s1, s2 []rune) int {
	minLen := len(s1)
	if len(s2) < minLen {
		minLen = len(s2)
	}
	for i := 0; i < minLen; i++ {
		if s1[i] != s2[i] {
			return i
		}
	}
	return minLen
}

// normalizeForComparison normalizes a string for content comparison
// by trimming whitespace, collapsing multiple spaces, and normalizing line endings
func normalizeForComparison(s string) string {
	// Step 1: Remove zero-width and invisible characters that LLMs may add/remove
	invisibleChars := []string{
		"\u200B", // ZERO WIDTH SPACE
		"\u200C", // ZERO WIDTH NON-JOINER
		"\u200D", // ZERO WIDTH JOINER
		"\uFEFF", // BYTE ORDER MARK (BOM)
		"\u2060", // WORD JOINER
		"\u00AD", // SOFT HYPHEN
		"\u200E", // LEFT-TO-RIGHT MARK
		"\u200F", // RIGHT-TO-LEFT MARK
	}
	for _, c := range invisibleChars {
		s = strings.ReplaceAll(s, c, "")
	}

	// Step 2: Trim leading and trailing whitespace
	s = strings.TrimSpace(s)

	// Step 3: Normalize line endings to \n
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	// Step 4: Collapse multiple newlines to double newline (paragraph break)
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}

	// Step 5: Normalize various space characters to regular space
	s = strings.ReplaceAll(s, "\u3000", " ") // Ideographic space
	s = strings.ReplaceAll(s, "\u00A0", " ") // Non-breaking space
	s = strings.ReplaceAll(s, "\u2007", " ") // Figure space
	s = strings.ReplaceAll(s, "\u2008", " ") // Punctuation space
	s = strings.ReplaceAll(s, "\u2009", " ") // Thin space
	s = strings.ReplaceAll(s, "\u200A", " ") // Hair space
	s = strings.ReplaceAll(s, "\u202F", " ") // Narrow no-break space
	s = strings.ReplaceAll(s, "\u205F", " ") // Medium mathematical space

	// Step 6: Collapse multiple spaces to single space (but preserve newlines)
	spacePattern := regexp.MustCompile(`[ \t]+`)
	s = spacePattern.ReplaceAllString(s, " ")

	// Step 7: Trim trailing spaces on each line
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	s = strings.Join(lines, "\n")

	// Step 8: Normalize ellipsis variations
	s = strings.ReplaceAll(s, "…", "...")     // Unicode ellipsis to three dots
	s = strings.ReplaceAll(s, "。。。", "...") // Chinese periods as ellipsis
	s = strings.ReplaceAll(s, "．．．", "...") // Fullwidth periods as ellipsis

	// Step 9: Normalize common Chinese/fullwidth punctuation to ASCII
	punctReplacements := map[string]string{
		"：": ":",  // fullwidth colon to ASCII
		"，": ",",  // Chinese comma to ASCII
		"。": ".",  // Chinese period to ASCII
		"！": "!",  // fullwidth exclamation
		"？": "?",  // fullwidth question mark
		"（": "(",  // fullwidth parentheses
		"）": ")",
		"【": "[",  // Chinese brackets
		"】": "]",
		"「": `"`,  // Japanese quotation marks
		"」": `"`,
		"『": `"`,
		"』": `"`,
		"\u201c": `"`, // left double quotation mark
		"\u201d": `"`, // right double quotation mark
		"\u2018": "'", // left single quotation mark
		"\u2019": "'", // right single quotation mark
		"、": ",",     // Chinese enumeration comma
		"；": ";",     // fullwidth semicolon
		"～": "~",     // fullwidth tilde
		"＋": "+",     // fullwidth plus
		"＝": "=",     // fullwidth equals
		"＜": "<",     // fullwidth less-than
		"＞": ">",     // fullwidth greater-than
		"％": "%",     // fullwidth percent
		"＃": "#",     // fullwidth hash
		"＆": "&",     // fullwidth ampersand
		"＊": "*",     // fullwidth asterisk
		"／": "/",     // fullwidth slash
		"＼": "\\",    // fullwidth backslash
	}
	for old, repl := range punctReplacements {
		s = strings.ReplaceAll(s, old, repl)
	}

	// Step 10: Normalize various hyphen/dash characters to ASCII hyphen
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
		"\u2043", // HYPHEN BULLET
	}
	for _, h := range hyphenChars {
		s = strings.ReplaceAll(s, h, "-")
	}

	// Step 11: Normalize fullwidth digits to ASCII digits
	fullwidthDigits := map[string]string{
		"０": "0", "１": "1", "２": "2", "３": "3", "４": "4",
		"５": "5", "６": "6", "７": "7", "８": "8", "９": "9",
	}
	for old, repl := range fullwidthDigits {
		s = strings.ReplaceAll(s, old, repl)
	}

	// Step 12: Normalize fullwidth Latin letters to ASCII (A-Z, a-z)
	// Fullwidth A-Z: U+FF21 to U+FF3A
	// Fullwidth a-z: U+FF41 to U+FF5A
	runes := []rune(s)
	for i, r := range runes {
		if r >= 0xFF21 && r <= 0xFF3A { // Fullwidth A-Z
			runes[i] = r - 0xFF21 + 'A'
		} else if r >= 0xFF41 && r <= 0xFF5A { // Fullwidth a-z
			runes[i] = r - 0xFF41 + 'a'
		}
	}
	s = string(runes)

	return s
}

// validateCitationNumbers returns invalid citation numbers (out of range)
func validateCitationNumbers(cited string, maxCitations int) []int {
	matches := citationNumberPattern.FindAllStringSubmatch(cited, -1)

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
	matches := citationMarkerPattern.FindAllStringIndex(cited, -1)

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

	for _, sent := range sentences {
		matches := citationNumberPattern.FindAllStringSubmatch(sent, -1)
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
	matches := citationNumberPattern.FindAllStringSubmatch(cited, -1)

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
