package activities

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

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
	Role              string          `json:"role,omitempty"`
	CitedReport       string          `json:"cited_report"`
	CitationsUsed     []int           `json:"citations_used"`
	ValidationPassed  bool            `json:"validation_passed"`
	ValidationError   string          `json:"validation_error,omitempty"`
	PlacementWarnings []string        `json:"placement_warnings,omitempty"`
	RedundantCount    int             `json:"redundant_count"`
	TokensUsed        int             `json:"tokens_used"`
	ModelUsed         string          `json:"model_used"`
	Provider          string          `json:"provider"`
	InputTokens       int             `json:"input_tokens"`
	OutputTokens      int             `json:"output_tokens"`
	PlacementStats    *PlacementStats `json:"placement_stats,omitempty"` // V2: placement statistics
}

// PlacementStats contains statistics about citation placement (V2)
type PlacementStats struct {
	Total       int     `json:"total"`        // Total placements requested
	Applied     int     `json:"applied"`      // Successfully applied
	Failed      int     `json:"failed"`       // Failed to apply
	SuccessRate float64 `json:"success_rate"` // Applied / Total
}

// CitationPlacement represents a single citation placement instruction (V2)
type CitationPlacement struct {
	SentenceIndex int    `json:"sentence_index"` // 0-based index of the sentence
	SentenceHash  string `json:"sentence_hash"`  // First 6 chars of MD5(normalized_sentence)
	CitationIDs   []int  `json:"citation_ids"`   // Array of citation numbers
	Confidence    string `json:"confidence"`     // "high" | "medium" | "low"
	Reason        string `json:"reason"`         // Brief explanation
}

// PlacementPlan is the LLM output structure for V2 citation placement
type PlacementPlan struct {
	Placements []CitationPlacement `json:"placements"`
}

// PlacementResult contains the result of applying placements
type PlacementResult struct {
	Applied    int   // Successfully applied placements
	Failed     int   // Failed placements
	FailedIdxs []int // Sentence indices that failed
}

// AddCitations adds inline citations to a report using LLM (V2 with fallback to legacy)
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
		"model_tier", input.ModelTier,
	)

	// If no citations available, return original report
	if len(input.Citations) == 0 {
		return &CitationAgentResult{
			Role:             role,
			CitedReport:      input.Report,
			ValidationPassed: true,
		}, nil
	}

	// Use direct LLM approach (simpler, more reliable)
	return a.addCitationsLegacy(ctx, input, role)
}

// addCitationsLegacy adds citations using direct LLM output approach
func (a *Activities) addCitationsLegacy(ctx context.Context, input CitationAgentInput, role string) (*CitationAgentResult, error) {
	logger := activity.GetLogger(ctx)

	logger.Info("CitationAgent: starting direct LLM approach",
		"report_length", len(input.Report),
		"citations_count", len(input.Citations),
		"role", role,
	)

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

	// Dynamic max_tokens based on report length to prevent truncation
	// Formula: reportLen/3 (chars to tokens ratio ~3:1) + 2000 (overhead for tags/formatting)
	reportLen := len(input.Report)
	minTokens := reportLen/3 + 2000

	// Base max_tokens by tier
	maxTokens := 8192 // default for small tier
	if modelTier == "medium" {
		maxTokens = 16384
	}

	// Ensure max_tokens is sufficient for the report
	if minTokens > maxTokens {
		maxTokens = minTokens
	}
	// Cap at model limits
	if modelTier == "small" && maxTokens > 16384 {
		maxTokens = 16384
	} else if modelTier == "medium" && maxTokens > 32000 {
		maxTokens = 32000
	}

	logger.Info("CitationAgent: dynamic max_tokens",
		"report_length", reportLen,
		"model_tier", modelTier,
		"max_tokens", maxTokens,
	)

	reqBody := map[string]interface{}{
		"query":       userContent,
		"max_tokens":  maxTokens,
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
	return `You are a citation specialist. Insert [n] markers ONLY when the source URL has SUFFICIENT INFORMATION to fully support the claim.

## CRITICAL RULE — SUFFICIENT EVIDENCE REQUIRED

- Only cite when the source Content/URL has SUFFICIENT INFORMATION that explicitly and fully supports the claim
- Source must provide enough detail, not just be "related" or "relevant"
- If Content is too vague, too short, or only partially mentions the claim, DO NOT cite
- If multiple sources support the same claim, choose the ONE with most sufficient information (prefer official sources)
- When in doubt, DO NOT cite
- Better to have too few citations than too many

## ABSOLUTE RULE - DO NOT MODIFY TEXT

The ONLY modification allowed is inserting [n] markers. NOTHING ELSE.
Your response will be REJECTED if you change ANY character of the original text.
If you cannot guarantee exact character preservation, output the original report unchanged (no citations).

## WHAT TO CITE (SUFFICIENT INFORMATION REQUIRED)

✓ Specific quantitative data (ONLY if source Content has sufficient detail):
  • Revenue/valuation: "$50M revenue" → source Content must explicitly state the exact number with context
  • User counts: "2 million users" → source Content must explicitly state the exact count
  • Growth rates: "150% YoY" → source Content must explicitly state the percentage
  • Funding: "raised $20M Series A" → source Content must explicitly state amount + round

  CRITICAL: "Related URL" or "title mentions it" is NOT enough — the source Content must have sufficient information

✓ Controversial or disputed statements:
  • ONLY if source Content provides sufficient evidence to support the claim
  • Not just "related" — must have enough detail

✓ Key milestones (ONLY if source Content confirms):
  • "founded in 2020" → source Content must explicitly state the year
  • "acquired by X in 2023" → source Content must explicitly state the acquisition with year

## INSUFFICIENT EVIDENCE EXAMPLES (DO NOT CITE)

✗ Source URL looks relevant but Content is empty/too short
✗ Source title mentions the topic but Content doesn't provide enough detail
✗ Source Content only partially mentions the claim without sufficient context
✗ Source is "about" the company but doesn't state the specific fact

## WHAT TO SKIP (STRICT)

✗ ANY lists (even if sourced):
  • Domain lists: "link.com/fr, link.com/de, link.com/jp" — NEVER cite
  • Feature lists: "supports A, B, C" — NEVER cite
  • Product lists: "offers X, Y, Z" — NEVER cite
  • RULE: Lists are descriptive enumerations, not individual claims

✗ Company names and general descriptions:
  • Company names, industry classifications
  • General background ("a SaaS company")

✗ Common knowledge or background information

✗ Section headers, transitions, synthesis language

## CITATION LIMITS (PER CLAIM)

- **Per sentence/claim**: Maximum 1 citation
- **Per list**: 0 citations (lists never get citations)
- **Same fact repeated**: Cite only FIRST mention

CRITICAL: If 5 sources all support the same claim, choose the ONE most authoritative/explicit source. NEVER use [1][2][3][4][5].

## SOURCE SELECTION (When Multiple Sources Support Same Claim)

Step 1: Filter to sources whose Content has SUFFICIENT INFORMATION (not just "mentions" or "related")
Step 2: Among filtered sources with sufficient information, choose ONE based on priority:
  1. Official sources (.gov, .edu, official company sites)
  2. Data aggregators (Crunchbase, LinkedIn, Bloomberg)
  3. Major news outlets (Reuters, TechCrunch, WSJ)
  4. Other sources

CRITICAL: Sufficient information > Source authority
- If official source has vague Content but news source has detailed Content, choose news source
- Only apply priority when sources have comparable levels of information

Example:
- Claim: "Company raised $50M Series B"
- Source [1] (TechCrunch): Content states "$50M Series B from X investors"
- Source [3] (Company blog): Content states "$50M Series B announcement"
- Source [7] (Crunchbase): Content shows "$50M Series B" with date
→ Choose [3] (official) because all three have sufficient information, prefer official

## CITATION PLACEMENT

Insert [n] at END of sentences or clauses:
- ✓ "Revenue grew 19%.[1]"
- ✓ "Founded in 2020[2], the company expanded rapidly."
- ✗ "Revenue grew 19%.[1][2][3]" — FORBIDDEN (choose ONE)
- ✗ "[1] Revenue grew 19%." — Wrong position

## AVOID REDUNDANT CITATIONS

- Never stack citations: [1][2][3] is FORBIDDEN
- Same source per sentence: cite at most ONCE
- No adjacent duplicates: NEVER [1][1]
- If same fact appears multiple times in report, cite only first occurrence

## EXAMPLES

### GOOD EXAMPLE 1 (Sufficient information):
Input sources:
- [1] TechCrunch: Content="TechCrunch reports Company raised $50M in Series B led by..."
- [3] Official blog: Content="Today we announce our $50M Series B round..."
- [7] Crunchbase: Content="Funding: $50M Series B, Date: 2024"

Report: "The company raised $50M in Series B.[3]"
(All have sufficient information. Choose [3] because it's official.)

### GOOD EXAMPLE 2 (No citation for lists):
Report: "Link offers localization across France, Germany, Japan, Sweden, and Mexico."
(No citations — lists never get citations, even if sources mention all domains)

### GOOD EXAMPLE 3 (Insufficient information - do not cite):
Input sources:
- [5] News article: URL mentions "Company funding" but Content="Company announces new round" (no amount)
- [8] Blog: Title="$50M Series B" but Content is empty/too short

Report: "The company raised $50M in Series B."
(No citation — sources lack sufficient information despite relevant URLs)

### BAD EXAMPLE 1 (Too many citations):
Report: "The company raised $50M[1] in Series B[3] from Acme Ventures[7]."
(Should be: "...raised $50M in Series B.[3]" — only ONE citation)

### BAD EXAMPLE 2 (Citing lists):
Report: "Link at link.com/fr[1], link.com/de[2], link.com/jp[3]."
(Lists should NEVER have citations)

### BAD EXAMPLE 3 (Insufficient evidence):
Input source [10]: URL="company-funding.html" but Content="Company is well-funded"
Report: "The company raised $50M.[10]"
(Do NOT cite — Content lacks sufficient information about the $50M amount)

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

	// Tolerate up to 15% difference (LLMs sometimes make minor changes or truncate long outputs)
	// Increased from 5% to 15% to handle longer reports where some content modification is acceptable
	if diffRatio < 0.15 {
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
	s = strings.ReplaceAll(s, "…", "...")   // Unicode ellipsis to three dots
	s = strings.ReplaceAll(s, "。。。", "...") // Chinese periods as ellipsis
	s = strings.ReplaceAll(s, "．．．", "...") // Fullwidth periods as ellipsis

	// Step 9: Normalize common Chinese/fullwidth punctuation to ASCII
	punctReplacements := map[string]string{
		"：":      ":", // fullwidth colon to ASCII
		"，":      ",", // Chinese comma to ASCII
		"。":      ".", // Chinese period to ASCII
		"！":      "!", // fullwidth exclamation
		"？":      "?", // fullwidth question mark
		"（":      "(", // fullwidth parentheses
		"）":      ")",
		"【":      "[", // Chinese brackets
		"】":      "]",
		"「":      `"`, // Japanese quotation marks
		"」":      `"`,
		"『":      `"`,
		"』":      `"`,
		"\u201c": `"`,  // left double quotation mark
		"\u201d": `"`,  // right double quotation mark
		"\u2018": "'",  // left single quotation mark
		"\u2019": "'",  // right single quotation mark
		"、":      ",",  // Chinese enumeration comma
		"；":      ";",  // fullwidth semicolon
		"～":      "~",  // fullwidth tilde
		"＋":      "+",  // fullwidth plus
		"＝":      "=",  // fullwidth equals
		"＜":      "<",  // fullwidth less-than
		"＞":      ">",  // fullwidth greater-than
		"％":      "%",  // fullwidth percent
		"＃":      "#",  // fullwidth hash
		"＆":      "&",  // fullwidth ampersand
		"＊":      "*",  // fullwidth asterisk
		"／":      "/",  // fullwidth slash
		"＼":      "\\", // fullwidth backslash
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

// ============================================================================
// V2 Citation Placement Functions (Indexed Placement Plan approach)
// ============================================================================

// addCitationsV2 implements the indexed placement plan approach
// LLM outputs JSON with sentence_index + hash, code applies deterministically
func (a *Activities) addCitationsV2(ctx context.Context, input CitationAgentInput, role string) (*CitationAgentResult, error) {
	logger := activity.GetLogger(ctx)

	// Step 1: Preprocess report - add sentence numbers
	sentences := splitSentencesV2(input.Report)
	numberedReport, sentenceHashes := addSentenceNumbers(sentences)

	logger.Info("CitationAgent V2: preprocessed report",
		"sentence_count", len(sentences),
	)

	// Step 2: Build V2 prompt and call LLM
	// Calculate max placements based on sentence count (roughly 30% of sentences, capped at 100)
	maxPlacements := len(sentences) * 30 / 100
	if maxPlacements < 25 {
		maxPlacements = 25
	}
	if maxPlacements > 100 {
		maxPlacements = 100
	}
	logger.Info("CitationAgent V2: calculated max placements",
		"sentence_count", len(sentences),
		"max_placements", maxPlacements,
	)
	systemPrompt := buildCitationPlacementPromptV2(maxPlacements)
	userContent := buildCitationUserContentV2(numberedReport, input.Citations, sentenceHashes)

	// Call LLM service
	llmServiceURL := os.Getenv("LLM_SERVICE_URL")
	if llmServiceURL == "" {
		llmServiceURL = "http://llm-service:8000"
	}
	url := fmt.Sprintf("%s/agent/query", llmServiceURL)

	// Use medium tier for V2 (better instruction following)
	modelTier := input.ModelTier
	if modelTier == "" {
		modelTier = "medium"
	}

	reqBody := map[string]interface{}{
		"query":       userContent,
		"max_tokens":  4096,
		"temperature": 0.0,
		"agent_id":    "citation_agent_v2",
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

	// Timeout: 90s for V2 (simpler output)
	client := &http.Client{
		Timeout:   90 * time.Second,
		Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(reqJSON)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "citation_agent_v2")
	if input.ParentWorkflowID != "" {
		req.Header.Set("X-Workflow-ID", input.ParentWorkflowID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d from LLM service", resp.StatusCode)
	}

	// Parse LLM response
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

	// Step 3: Parse placement plan from LLM response
	placementPlan, err := parsePlacementPlan(llmResp.Response)
	if err != nil {
		logger.Warn("CitationAgent V2: failed to parse placement plan", "error", err)
		return nil, fmt.Errorf("failed to parse placement plan: %w", err)
	}

	logger.Info("CitationAgent V2: parsed placement plan",
		"placements", len(placementPlan.Placements),
	)

	if len(placementPlan.Placements) == 0 {
		return &CitationAgentResult{
			Role:             role,
			CitedReport:      input.Report,
			CitationsUsed:    nil,
			ValidationPassed: true,
			TokensUsed:       llmResp.TokensUsed,
			ModelUsed:        llmResp.ModelUsed,
			Provider:         llmResp.Provider,
			InputTokens:      llmResp.Metadata.InputTokens,
			OutputTokens:     llmResp.Metadata.OutputTokens,
			PlacementStats:   &PlacementStats{Total: 0, Applied: 0, Failed: 0, SuccessRate: 0},
		}, nil
	}

	// Step 4: Apply placements deterministically
	citedReport, placementResult := applyPlacementsV2(sentences, placementPlan, sentenceHashes, len(input.Citations))

	logger.Info("CitationAgent V2: applied placements",
		"applied", placementResult.Applied,
		"failed", placementResult.Failed,
	)

	// Calculate success rate
	total := placementResult.Applied + placementResult.Failed
	successRate := 0.0
	if total > 0 {
		successRate = float64(placementResult.Applied) / float64(total)
	}

	// Step 5: Determine validation status based on partial success strategy
	// Accept if: ≥50% success rate OR ≥5 placements applied
	validationPassed := successRate >= 0.5 || placementResult.Applied >= 5

	var warnings []string
	if placementResult.Failed > 0 {
		warnings = append(warnings, fmt.Sprintf("%d/%d placements failed", placementResult.Failed, total))
	}

	return &CitationAgentResult{
		Role:              role,
		CitedReport:       citedReport,
		CitationsUsed:     extractUsedCitationNumbers(citedReport),
		ValidationPassed:  validationPassed,
		PlacementWarnings: warnings,
		TokensUsed:        llmResp.TokensUsed,
		ModelUsed:         llmResp.ModelUsed,
		Provider:          llmResp.Provider,
		InputTokens:       llmResp.Metadata.InputTokens,
		OutputTokens:      llmResp.Metadata.OutputTokens,
		PlacementStats: &PlacementStats{
			Total:       total,
			Applied:     placementResult.Applied,
			Failed:      placementResult.Failed,
			SuccessRate: successRate,
		},
	}, nil
}

// splitSentencesV2 splits text into sentences preserving original format
// Handles Chinese (。！？), English (.!?), and newlines
// Preserves blank lines to maintain markdown structure
func splitSentencesV2(text string) []string {
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		current.WriteRune(r)

		// Check for sentence-ending punctuation
		isSentenceEnd := false
		switch r {
		case '.':
			// Check if this is a decimal point (digit.digit) - NOT a sentence boundary
			// e.g., "$30.8 billion", "3.14", "v1.2.3"
			prevIsDigit := i > 0 && unicode.IsDigit(runes[i-1])
			nextIsDigit := i+1 < len(runes) && unicode.IsDigit(runes[i+1])
			if prevIsDigit && nextIsDigit {
				// This is a decimal point, not a sentence boundary
				isSentenceEnd = false
			} else {
				isSentenceEnd = true
			}
		case '!', '?', '。', '！', '？':
			isSentenceEnd = true
		case '\n':
			// Newline after content is also a sentence boundary
			if current.Len() > 1 {
				isSentenceEnd = true
			}
		}

		if isSentenceEnd {
			// Include trailing whitespace in the sentence (except newlines to preserve paragraph breaks)
			for i+1 < len(runes) && unicode.IsSpace(runes[i+1]) && runes[i+1] != '\n' {
				i++
				current.WriteRune(runes[i])
			}

			s := current.String()
			// Preserve blank lines (only newlines) for markdown formatting
			// But skip truly empty strings
			if len(s) > 0 {
				sentences = append(sentences, s)
			}
			current.Reset()
		}
	}

	// Don't forget the last sentence
	if current.Len() > 0 {
		s := current.String()
		if len(s) > 0 {
			sentences = append(sentences, s)
		}
	}

	return sentences
}

// addSentenceNumbers adds [0], [1], etc. prefixes and computes hashes
func addSentenceNumbers(sentences []string) (string, []string) {
	var sb strings.Builder
	hashes := make([]string, len(sentences))

	for i, s := range sentences {
		// Compute hash of normalized sentence
		hashes[i] = computeSentenceHash(s)

		// Add numbered prefix
		sb.WriteString(fmt.Sprintf("[%d] %s", i, s))
	}

	return sb.String(), hashes
}

// computeSentenceHash returns first 6 chars of MD5(normalized_sentence)
func computeSentenceHash(s string) string {
	normalized := normalizeForHash(s)
	hash := md5.Sum([]byte(normalized))
	return hex.EncodeToString(hash[:])[:6]
}

// normalizeForHash removes whitespace and punctuation for hash computation
func normalizeForHash(s string) string {
	var sb strings.Builder
	for _, r := range s {
		// Keep only letters and digits (CJK + Latin)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// buildCitationPlacementPromptV2 returns the V2 system prompt
func buildCitationPlacementPromptV2(maxPlacements int) string {
	return fmt.Sprintf(`You are a citation placement specialist.

## YOUR TASK
Analyze the ENTIRE report from beginning to end and identify where citations should be placed.
Output a JSON placement plan - DO NOT output the report text.
IMPORTANT: Distribute citations throughout ALL sections of the report, not just the beginning.

## INPUT
You will receive:
1. Report text with numbered sentences: [0] First sentence. [1] Second sentence...
2. Citation list with content snippets
3. Sentence hashes for verification

## OUTPUT FORMAT (JSON only)
{
  "placements": [
    {
      "sentence_index": 3,
      "sentence_hash": "a1b2c3",
      "citation_ids": [7, 12],
      "confidence": "high",
      "reason": "revenue data 23.4B matches source"
    }
  ]
}

## FIELDS
- sentence_index: 0-based index of the sentence (from numbered input)
- sentence_hash: First 6 chars of the hash shown for that sentence
- citation_ids: Array of citation numbers (1-indexed) that support this sentence
- confidence: "high" | "medium" | "low"
- reason: Brief explanation of why this citation supports the claim

## RULES
1. Only cite factual claims (statistics, dates, names, figures)
2. Skip section headers, transitions, synthesis language
3. Maximum %d placements - DISTRIBUTE EVENLY across ALL report sections
4. If unsure, use confidence="low" or skip entirely
5. Prefer official sources over news
6. IMPORTANT: Do NOT concentrate all citations in the first few sentences

## WHAT TO CITE
✓ Statistics, financial figures, dates
✓ Company facts (founding, location, size)
✓ Named people with roles/titles
✓ Specific claims readers would verify

## WHAT TO SKIP
✗ Section headers, transitions
✗ Common knowledge
✗ Synthesis language ("This shows that...")
✗ Claims with NO matching source

## EXAMPLE OUTPUT
{
  "placements": [
    {
      "sentence_index": 5,
      "sentence_hash": "d4e5f6",
      "citation_ids": [3],
      "confidence": "high",
      "reason": "revenue figure $2.3B exact match"
    },
    {
      "sentence_index": 12,
      "sentence_hash": "abc123",
      "citation_ids": [1, 7],
      "confidence": "medium",
      "reason": "company founding year from official page"
    }
  ]
}

Output ONLY the JSON, nothing else.`, maxPlacements)
}

// buildCitationUserContentV2 builds the user content for V2
func buildCitationUserContentV2(numberedReport string, citations []CitationForAgent, hashes []string) string {
	var sb strings.Builder

	sb.WriteString("## Available Citations:\n")
	for i, c := range citations {
		title := c.Title
		if title == "" {
			title = c.Source
		}
		snippet := c.Snippet
		if len(snippet) > 400 {
			snippet = snippet[:400] + "..."
		}
		sb.WriteString(fmt.Sprintf("[%d] %s (%s)\n", i+1, title, c.URL))
		if snippet != "" {
			sb.WriteString(fmt.Sprintf("    Content: %s\n", snippet))
		}
	}

	sb.WriteString("\n## Sentence Hashes:\n")
	for i, h := range hashes {
		sb.WriteString(fmt.Sprintf("[%d] hash=%s\n", i, h))
	}

	sb.WriteString("\n## Report to Analyze:\n")
	sb.WriteString(numberedReport)
	sb.WriteString("\n\nOutput your placement plan as JSON:")

	return sb.String()
}

// parsePlacementPlan parses the LLM response into a PlacementPlan
func parsePlacementPlan(response string) (*PlacementPlan, error) {
	response = strings.TrimSpace(response)

	// Try to find JSON in the response
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")

	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		return nil, fmt.Errorf("no JSON object found in response")
	}

	jsonStr := response[jsonStart : jsonEnd+1]

	var plan PlacementPlan
	if err := json.Unmarshal([]byte(jsonStr), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &plan, nil
}

// applyPlacementsV2 applies the placement plan to the original sentences
func applyPlacementsV2(sentences []string, plan *PlacementPlan, hashes []string, maxCitationNum int) (string, PlacementResult) {
	result := PlacementResult{}

	// Track which sentences have been modified
	modified := make([]string, len(sentences))
	copy(modified, sentences)

	// Sort placements by sentence index (descending) to avoid index shifts
	placements := make([]CitationPlacement, len(plan.Placements))
	copy(placements, plan.Placements)
	sort.Slice(placements, func(i, j int) bool {
		return placements[i].SentenceIndex > placements[j].SentenceIndex
	})

	for _, p := range placements {
		// Bounds check
		if p.SentenceIndex < 0 || p.SentenceIndex >= len(sentences) {
			result.Failed++
			result.FailedIdxs = append(result.FailedIdxs, p.SentenceIndex)
			continue
		}

		// Filter valid citation IDs
		validCitationIDs := filterValidCitationIDs(p.CitationIDs, maxCitationNum)
		if len(validCitationIDs) == 0 {
			result.Failed++
			result.FailedIdxs = append(result.FailedIdxs, p.SentenceIndex)
			continue
		}

		// Hash verification with adjacent sentence fallback
		targetIdx := p.SentenceIndex
		if p.SentenceHash != "" && p.SentenceHash != hashes[targetIdx] {
			// Try adjacent sentences (±1)
			found := false
			for _, offset := range []int{-1, 1} {
				adjIdx := targetIdx + offset
				if adjIdx >= 0 && adjIdx < len(hashes) && hashes[adjIdx] == p.SentenceHash {
					targetIdx = adjIdx
					found = true
					break
				}
			}
			if !found {
				// Hash mismatch and no adjacent match - record as FAILED to prevent wrong placement
				// This is a P0 fix: lenient apply could insert citations at wrong positions
				result.Failed++
				result.FailedIdxs = append(result.FailedIdxs, p.SentenceIndex)
				continue
			}
		}

		// Apply citations to the sentence
		modified[targetIdx] = appendCitationsToSentence(modified[targetIdx], validCitationIDs)
		result.Applied++
	}

	// Reconstruct the report
	return strings.Join(modified, ""), result
}

// filterValidCitationIDs filters citation IDs to only valid ones
func filterValidCitationIDs(ids []int, maxNum int) []int {
	var valid []int
	seen := make(map[int]bool)
	for _, id := range ids {
		if id >= 1 && id <= maxNum && !seen[id] {
			valid = append(valid, id)
			seen[id] = true
		}
	}
	return valid
}

// appendCitationsToSentence appends [n] markers to the end of a sentence
func appendCitationsToSentence(sentence string, citationIDs []int) string {
	if len(citationIDs) == 0 {
		return sentence
	}

	// Find the position to insert citations (before trailing whitespace/newline)
	trimmed := strings.TrimRight(sentence, " \t\n\r")
	trailing := sentence[len(trimmed):]

	// Build citation markers
	var markers strings.Builder
	for _, id := range citationIDs {
		markers.WriteString(fmt.Sprintf("[%d]", id))
	}

	return trimmed + markers.String() + trailing
}

// ============================================================================
// Citation V2: Deep Research with pre-computed ClaimMappings from Verify
// ============================================================================

// ClaimMappingInput represents a claim with its supporting citation IDs from Verify
type ClaimMappingInput struct {
	Claim         string  `json:"claim"`
	Verdict       string  `json:"verdict"` // "supported" | "unsupported" | "insufficient_evidence"
	SupportingIDs []int   `json:"supporting_ids"`
	Confidence    float64 `json:"confidence"`
}

// CitationWithIDForAgent is a citation with a pre-assigned sequential ID
type CitationWithIDForAgent struct {
	ID               int     `json:"id"` // Sequential ID (1, 2, 3...)
	URL              string  `json:"url"`
	Title            string  `json:"title"`
	Source           string  `json:"source"`
	Snippet          string  `json:"snippet"`
	CredibilityScore float64 `json:"credibility_score"`
	QualityScore     float64 `json:"quality_score"`
}

// CitationAgentInputV2 is the input for Citation Agent V2 (Deep Research)
type CitationAgentInputV2 struct {
	Report           string                   `json:"report"`
	Citations        []CitationWithIDForAgent `json:"citations"`      // Fetch-only citations for inline placement
	AllCitations     []CitationWithIDForAgent `json:"all_citations"`  // P1: All citations for Sources section output
	ClaimMappings    []ClaimMappingInput      `json:"claim_mappings"` // From Verify batch endpoint
	ParentWorkflowID string                   `json:"parent_workflow_id,omitempty"`
	Context          map[string]interface{}   `json:"context,omitempty"`
	ModelTier        string                   `json:"model_tier,omitempty"`
}

// AddCitationsWithVerify adds inline citations using pre-computed ClaimMappings from Verify.
// This is used by Deep Research workflow for efficient citation placement.
// Unlike AddCitations (V1), this function doesn't need the LLM to find claim-citation matches -
// it just needs to place the citations at appropriate positions.
func (a *Activities) AddCitationsWithVerify(ctx context.Context, input CitationAgentInputV2) (*CitationAgentResult, error) {
	logger := activity.GetLogger(ctx)

	role := "citation_agent_v2"
	if input.Context != nil {
		if v, ok := input.Context["role"].(string); ok && strings.TrimSpace(v) != "" {
			role = strings.TrimSpace(v)
		}
	}

	logger.Info("CitationAgent V2 (with Verify): starting",
		"report_length", len(input.Report),
		"citations_count", len(input.Citations),
		"claim_mappings", len(input.ClaimMappings),
		"role", role,
	)

	// If no citations or claim mappings, return original report
	if len(input.Citations) == 0 || len(input.ClaimMappings) == 0 {
		return &CitationAgentResult{
			Role:             role,
			CitedReport:      input.Report,
			ValidationPassed: true,
		}, nil
	}

	// Filter to only supported claims with supporting IDs
	supportedMappings := filterSupportedClaims(input.ClaimMappings)
	if len(supportedMappings) == 0 {
		logger.Info("CitationAgent V2: no supported claims found")
		return &CitationAgentResult{
			Role:             role,
			CitedReport:      input.Report,
			ValidationPassed: true,
		}, nil
	}

	logger.Info("CitationAgent V2: filtered to supported claims",
		"total_mappings", len(input.ClaimMappings),
		"supported_mappings", len(supportedMappings),
	)

	// Build citation ID to citation map
	citationMap := make(map[int]CitationWithIDForAgent)
	for _, c := range input.Citations {
		citationMap[c.ID] = c
	}

	// Collect all unique citation IDs that should be used
	usedCitationIDs := collectUsedCitationIDs(supportedMappings)

	// Build prompt for simple placement task
	systemPrompt := buildCitationPlacementPromptWithMappings()
	userContent := buildCitationUserContentWithMappings(input.Report, input.Citations, supportedMappings)

	// Call LLM service
	llmServiceURL := os.Getenv("LLM_SERVICE_URL")
	if llmServiceURL == "" {
		llmServiceURL = "http://llm-service:8000"
	}
	url := fmt.Sprintf("%s/agent/query", llmServiceURL)

	modelTier := input.ModelTier
	if modelTier == "" {
		modelTier = "small" // V2 is simpler, can use small tier
	}

	// Calculate max_tokens based on report length
	// Chinese text: ~1.5 tokens per character; add 20% buffer for citations
	reportLen := len(input.Report)
	maxTokens := (reportLen * 2) // Conservative estimate for CJK text
	if maxTokens < 8192 {
		maxTokens = 8192
	}
	if maxTokens > 32000 {
		maxTokens = 32000 // Cap at 32K to avoid excessive costs
	}

	logger.Info("CitationAgent V2: calculated max_tokens",
		"report_length", reportLen,
		"max_tokens", maxTokens,
	)

	reqBody := map[string]interface{}{
		"query":       userContent,
		"max_tokens":  maxTokens,
		"temperature": 0.0,
		"agent_id":    "citation_agent_v2_verify",
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

	// Timeout based on report length (reportLen already calculated above)
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
	req.Header.Set("X-Agent-ID", "citation_agent_v2_verify")
	if input.ParentWorkflowID != "" {
		req.Header.Set("X-Workflow-ID", input.ParentWorkflowID)
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("CitationAgent V2: LLM call failed, returning original report", "error", err)
		return &CitationAgentResult{
			Role:             role,
			CitedReport:      input.Report,
			ValidationPassed: false,
			ValidationError:  err.Error(),
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Warn("CitationAgent V2: HTTP error, returning original report", "status", resp.StatusCode)
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
		logger.Warn("CitationAgent V2: failed to parse response, returning original", "error", err)
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

	// Extract actual citations used in the report
	actualUsedCitations := extractUsedCitationNumbers(citedReport)

	if len(actualUsedCitations) == 0 {
		logger.Info("CitationAgent V2: no citations added, using original report")
		result.Role = role
		result.CitedReport = input.Report
		result.CitationsUsed = nil
		result.ValidationPassed = true
		result.PlacementWarnings = []string{"LLM did not add any citations - using original report"}
		return result, nil
	}

	// Validate content immutability
	if valid, err := validateContentImmutability(input.Report, citedReport); !valid {
		logger.Warn("CitationAgent V2: content modified, using original report", "error", err)
		result.Role = role
		result.CitedReport = input.Report
		result.CitationsUsed = nil
		result.ValidationPassed = false
		result.ValidationError = "content modified beyond citations"
		return result, nil
	}

	// Validate citation numbers (only allow IDs that were in supportedMappings)
	invalidCitations := validateCitationNumbersV2(citedReport, usedCitationIDs)
	if len(invalidCitations) > 0 {
		logger.Warn("CitationAgent V2: invalid citation numbers, removing", "invalid", invalidCitations)
		citedReport = removeInvalidCitations(citedReport, invalidCitations)
		actualUsedCitations = extractUsedCitationNumbers(citedReport)
	}

	// Build Sources section with used citations and additional sources (P1)
	// Use AllCitations if provided, otherwise fall back to Citations
	allCitationsForSources := input.AllCitations
	if len(allCitationsForSources) == 0 {
		allCitationsForSources = input.Citations
	}
	sourcesSection := buildSourcesSectionV2(actualUsedCitations, allCitationsForSources, citationMap)
	citedReportWithSources := citedReport + "\n\n" + sourcesSection

	result.Role = role
	result.CitedReport = citedReportWithSources
	result.CitationsUsed = actualUsedCitations
	result.ValidationPassed = true
	result.RedundantCount = len(detectRedundantCitations(citedReport))

	logger.Info("CitationAgent V2 (with Verify): complete",
		"expected_citations", len(usedCitationIDs),
		"actual_citations", len(actualUsedCitations),
	)

	return result, nil
}

// filterSupportedClaims returns only claims with verdict="supported" and non-empty supporting_ids
func filterSupportedClaims(mappings []ClaimMappingInput) []ClaimMappingInput {
	var result []ClaimMappingInput
	for _, m := range mappings {
		if m.Verdict == "supported" && len(m.SupportingIDs) > 0 {
			result = append(result, m)
		}
	}
	return result
}

// collectUsedCitationIDs extracts all unique citation IDs from supported mappings
func collectUsedCitationIDs(mappings []ClaimMappingInput) []int {
	idSet := make(map[int]bool)
	for _, m := range mappings {
		for _, id := range m.SupportingIDs {
			idSet[id] = true
		}
	}

	ids := make([]int, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// validateCitationNumbersV2 returns citation numbers that are not in the allowed set
func validateCitationNumbersV2(cited string, allowedIDs []int) []int {
	allowedSet := make(map[int]bool)
	for _, id := range allowedIDs {
		allowedSet[id] = true
	}

	matches := citationNumberPattern.FindAllStringSubmatch(cited, -1)
	var invalid []int
	seen := make(map[int]bool)
	for _, m := range matches {
		n, _ := strconv.Atoi(m[1])
		if !allowedSet[n] && !seen[n] {
			invalid = append(invalid, n)
			seen[n] = true
		}
	}
	return invalid
}

// buildCitationPlacementPromptWithMappings returns the system prompt for V2 with ClaimMappings
func buildCitationPlacementPromptWithMappings() string {
	return `You are a citation placement specialist.

## YOUR TASK
Add [n] citation markers to the report based on pre-verified claim-citation mappings.
The mappings tell you which claims are supported by which citations.
Your job is simply to place [n] markers at the END of sentences containing those claims.

## ABSOLUTE RULE - DO NOT MODIFY TEXT
The ONLY modification allowed is inserting [n] markers. NOTHING ELSE.
Your response will be REJECTED if you change ANY character of the original text.

## HOW TO USE MAPPINGS
For each mapping:
1. Find the sentence in the report that contains the claim
2. Add the citation marker(s) at the END of that sentence
3. Format: "sentence text.[1]" or "sentence text.[1][3]"

## CITATION PLACEMENT RULES
- Place citations at END of sentences, before the period/newline
- ✓ "Revenue grew 19%.[1]"
- ✓ "Founded in 2020[2], the company expanded rapidly.[3]"
- ✗ "[1] Revenue grew 19%."

## OUTPUT FORMAT
<cited_report>
[Original text with [n] markers inserted - NO OTHER CHANGES]
</cited_report>

IMPORTANT: Only use citation IDs from the provided mappings. Do NOT add any other citations.`
}

// buildCitationUserContentWithMappings builds user content for V2 with ClaimMappings
func buildCitationUserContentWithMappings(report string, citations []CitationWithIDForAgent, mappings []ClaimMappingInput) string {
	var sb strings.Builder

	sb.WriteString("## Verified Claim-Citation Mappings:\n")
	sb.WriteString("(These are pre-verified - just place the citations where the claims appear)\n\n")
	for i, m := range mappings {
		idsStr := make([]string, len(m.SupportingIDs))
		for j, id := range m.SupportingIDs {
			idsStr[j] = fmt.Sprintf("[%d]", id)
		}
		sb.WriteString(fmt.Sprintf("%d. Claim: \"%s\"\n", i+1, m.Claim))
		sb.WriteString(fmt.Sprintf("   Citations: %s (confidence: %.2f)\n\n", strings.Join(idsStr, " "), m.Confidence))
	}

	sb.WriteString("\n## Citation Reference (for context only):\n")
	for _, c := range citations {
		title := c.Title
		if title == "" {
			title = c.Source
		}
		sb.WriteString(fmt.Sprintf("[%d] %s (%s)\n", c.ID, title, c.URL))
	}

	sb.WriteString("\n## Report to Cite:\n")
	sb.WriteString(report)
	sb.WriteString("\n\nAdd citations based on the mappings above and output within <cited_report> tags:")

	return sb.String()
}

// buildSourcesSectionV2 builds the Sources section with all citations
// P1: All citations shown with [n] numbering, marked as "Used inline" or "Additional source"
// Format matches V1: [n] Title (URL) - domain - Status
func buildSourcesSectionV2(usedIDs []int, allCitations []CitationWithIDForAgent, citationMap map[int]CitationWithIDForAgent) string {
	var sb strings.Builder
	sb.WriteString("## Sources\n\n")

	// Build used IDs set for quick lookup
	usedSet := make(map[int]bool)
	for _, id := range usedIDs {
		usedSet[id] = true
	}

	// Output all citations with [n] prefix, sorted by ID
	// First collect all citations with their IDs
	type citationEntry struct {
		id     int
		cite   CitationWithIDForAgent
		isUsed bool
	}
	var entries []citationEntry

	// Add all citations from allCitations list
	for _, c := range allCitations {
		entries = append(entries, citationEntry{
			id:     c.ID,
			cite:   c,
			isUsed: usedSet[c.ID],
		})
	}

	// Sort by ID
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].id < entries[j].id
	})

	// Output all citations
	for _, entry := range entries {
		c := entry.cite
		title := c.Title
		if title == "" {
			title = c.Source
		}
		if title == "" {
			title = c.URL
		}

		status := "Additional source"
		if entry.isUsed {
			status = "Used inline"
		}

		// Decode URL for better readability (e.g., %E4%B8%AD%E6%96%87 → 中文)
		displayURL := c.URL
		if decoded, err := url.PathUnescape(c.URL); err == nil {
			displayURL = decoded
		}

		// Format: [n] Title (URL) - domain - Status
		sb.WriteString(fmt.Sprintf("[%d] %s (%s) - %s - %s\n", entry.id, title, displayURL, c.Source, status))
	}

	return sb.String()
}
