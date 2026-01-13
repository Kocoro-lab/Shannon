package metadata

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Snippet length constants for citation extraction
const (
	MaxSnippetLength    = 500 // Maximum characters for snippet content
	MinSnippetLength    = 30  // Minimum characters for a valid snippet
	MinLLMSignalMatches = 2   // Required matches for weak signals and structured fields
)

// Citation represents a single source citation with quality metrics
type Citation struct {
	URL              string     `json:"url"`
	Title            string     `json:"title"`
	Source           string     `json:"source"`      // domain name
	SourceType       string     `json:"source_type"` // web|news|academic|social
	ToolSource       string     `json:"tool_source"` // Citation V2: "search" or "fetch" (origin tool type)
	RetrievedAt      time.Time  `json:"retrieved_at"`
	PublishedDate    *time.Time `json:"published_date,omitempty"`
	RelevanceScore   float64    `json:"relevance_score"`   // from search tool
	QualityScore     float64    `json:"quality_score"`     // recency + completeness
	CredibilityScore float64    `json:"credibility_score"` // domain reputation
	AgentID          string     `json:"agent_id"`
	Snippet          string     `json:"snippet"`
}

// CitationStats provides aggregate metrics for collected citations
type CitationStats struct {
	TotalSources    int     `json:"total_sources"`
	UniqueDomains   int     `json:"unique_domains"`
	AvgQuality      float64 `json:"avg_quality"`
	AvgCredibility  float64 `json:"avg_credibility"`
	SourceDiversity float64 `json:"source_diversity"` // unique_domains / total_sources

	// P1-4: Extended metrics for observability
	QualityBuckets map[string]int `json:"quality_buckets,omitempty"` // "low", "medium", "high" → count
	TopDomains     []DomainCount  `json:"top_domains,omitempty"`     // Top 10 domains by count
	DuplicateURLs  int            `json:"duplicate_urls,omitempty"`  // Number of duplicate URLs removed
	PerAgentCount  map[string]int `json:"per_agent_count,omitempty"` // agent_id → citation count
}

// DomainCount represents a domain and its citation count
type DomainCount struct {
	Domain string `json:"domain"`
	Count  int    `json:"count"`
}

// CredibilityConfig holds domain credibility scoring rules
type CredibilityConfig struct {
	CredibilityRules struct {
		TLDPatterns []struct {
			Suffix      string  `yaml:"suffix"`
			Score       float64 `yaml:"score"`
			Description string  `yaml:"description"`
		} `yaml:"tld_patterns"`

		DomainGroups []struct {
			Category    string   `yaml:"category"`
			Score       float64  `yaml:"score"`
			Description string   `yaml:"description"`
			Domains     []string `yaml:"domains"`
		} `yaml:"domain_groups"`

		DefaultScore float64 `yaml:"default_score"`
	} `yaml:"credibility_rules"`

	QualityGates struct {
		MinCredibilityScore float64 `yaml:"min_credibility_score"`
		PreferredScore      float64 `yaml:"preferred_score"`
		HighQualityScore    float64 `yaml:"high_quality_score"`
	} `yaml:"quality_gates"`

	DiversityRules struct {
		MaxPerDomain     int     `yaml:"max_per_domain"`
		MinUniqueDomains int     `yaml:"min_unique_domains"`
		DiversityBonus   float64 `yaml:"diversity_bonus"`
	} `yaml:"diversity_rules"`
}

var (
	credibilityConfig     *CredibilityConfig
	credibilityConfigOnce sync.Once
)

// GetCredibilityConfigPath returns the config path, checking env var first
func GetCredibilityConfigPath() string {
	if envPath := os.Getenv("CITATION_CREDIBILITY_CONFIG"); envPath != "" {
		return envPath
	}
	return "/app/config/citation_credibility.yaml"
}

// LoadCredibilityConfig loads credibility scoring rules from config file
func LoadCredibilityConfig() *CredibilityConfig {
	credibilityConfigOnce.Do(func() {
		configPath := GetCredibilityConfigPath()

		data, err := os.ReadFile(configPath)
		if err != nil {
			log.Printf("Warning: Failed to load citation credibility config from %s: %v. Using defaults.", configPath, err)
			credibilityConfig = getDefaultCredibilityConfig()
			return
		}

		var config CredibilityConfig
		if err := yaml.Unmarshal(data, &config); err != nil {
			log.Printf("Warning: Failed to parse citation credibility config: %v. Using defaults.", err)
			credibilityConfig = getDefaultCredibilityConfig()
			return
		}

		credibilityConfig = &config
		log.Printf("Loaded citation credibility config from %s", configPath)
	})

	return credibilityConfig
}

// isCitationsDebugEnabled returns true when verbose citation debug logging is enabled
func isCitationsDebugEnabled() bool {
	v := os.Getenv("CITATIONS_DEBUG")
	if v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "on") {
		return true
	}
	return false
}

// ResetCredibilityConfigForTest resets the singleton for testing purposes
// This should only be called from test code
func ResetCredibilityConfigForTest() {
	credibilityConfigOnce = sync.Once{}
	credibilityConfig = nil
}

// getDefaultCredibilityConfig returns fallback config when file is unavailable
func getDefaultCredibilityConfig() *CredibilityConfig {
	return &CredibilityConfig{
		CredibilityRules: struct {
			TLDPatterns []struct {
				Suffix      string  `yaml:"suffix"`
				Score       float64 `yaml:"score"`
				Description string  `yaml:"description"`
			} `yaml:"tld_patterns"`
			DomainGroups []struct {
				Category    string   `yaml:"category"`
				Score       float64  `yaml:"score"`
				Description string   `yaml:"description"`
				Domains     []string `yaml:"domains"`
			} `yaml:"domain_groups"`
			DefaultScore float64 `yaml:"default_score"`
		}{
			TLDPatterns: []struct {
				Suffix      string  `yaml:"suffix"`
				Score       float64 `yaml:"score"`
				Description string  `yaml:"description"`
			}{
				{Suffix: ".edu", Score: 0.85, Description: "Educational"},
				{Suffix: ".gov", Score: 0.80, Description: "Government"},
			},
			DomainGroups: []struct {
				Category    string   `yaml:"category"`
				Score       float64  `yaml:"score"`
				Description string   `yaml:"description"`
				Domains     []string `yaml:"domains"`
			}{},
			DefaultScore: 0.60,
		},
	}
}

// NormalizeURL cleans and normalizes a URL for deduplication
// - Converts to lowercase
// - Removes trailing slashes
// - Removes common query parameters (utm_*, fbclid, etc.)
// - Removes fragment identifiers (#)
func NormalizeURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	// Normalize scheme to lowercase
	parsed.Scheme = strings.ToLower(parsed.Scheme)

	// Normalize host to lowercase
	parsed.Host = strings.ToLower(parsed.Host)

	// Remove www. prefix for consistency
	if strings.HasPrefix(parsed.Host, "www.") {
		parsed.Host = parsed.Host[4:]
	}

	// Remove fragment
	parsed.Fragment = ""

	// Remove tracking query parameters
	if parsed.RawQuery != "" {
		q := parsed.Query()
		trackingParams := []string{
			"utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content",
			"fbclid", "gclid", "msclkid",
			"ref", "source",
		}
		for _, param := range trackingParams {
			q.Del(param)
		}
		parsed.RawQuery = q.Encode()
	}

	// Remove trailing slash from path (including root path "/" -> "")
	if strings.HasSuffix(parsed.Path, "/") {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	}

	return parsed.String(), nil
}

// ExtractDomain returns the lowercase host from a URL, removing any port and a
// leading "www." but preserving other subdomains when present.
// Example: "https://blog.example.com/path" -> "blog.example.com"
func ExtractDomain(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	host := strings.ToLower(parsed.Host)

	// Remove port if present
	if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
		host = host[:colonIndex]
	}

	// Remove www. prefix
	if strings.HasPrefix(host, "www.") {
		host = host[4:]
	}

	return host, nil
}

// ScoreQuality calculates quality score based on recency and completeness
// Formula: relevance * 0.7 + recency * 0.3 + completeness bonus
//
// Recency scoring (30-day decay):
// - < 7 days: 1.0
// - 7-30 days: 0.7
// - 30-90 days: 0.4
// - > 90 days: 0.2
//
// Completeness bonus (+0.1 if has published_date, title, snippet)
func ScoreQuality(relevance float64, publishedDate *time.Time, hasTitle, hasSnippet bool, now time.Time) float64 {
	// Base score from relevance (70% weight)
	score := relevance * 0.7

	// Recency score (30% weight)
	recencyScore := 0.2 // default for old/unknown dates
	if publishedDate != nil {
		daysSincePublished := now.Sub(*publishedDate).Hours() / 24
		switch {
		case daysSincePublished < 7:
			recencyScore = 1.0
		case daysSincePublished < 30:
			recencyScore = 0.7
		case daysSincePublished < 90:
			recencyScore = 0.4
		default:
			recencyScore = 0.2
		}
	}
	score += recencyScore * 0.3

	// Completeness bonus
	completeness := 0.0
	if publishedDate != nil {
		completeness += 0.033
	}
	if hasTitle {
		completeness += 0.033
	}
	if hasSnippet {
		completeness += 0.034
	}
	score += completeness

	// Cap at 1.0
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// ScoreCredibility calculates credibility score based on domain reputation
// Loads rules from config/citation_credibility.yaml
// Falls back to hardcoded defaults if config unavailable
func ScoreCredibility(domain string) float64 {
	config := LoadCredibilityConfig()
	domain = strings.ToLower(domain)

	// Check TLD patterns first (highest priority)
	for _, tldPattern := range config.CredibilityRules.TLDPatterns {
		if strings.HasSuffix(domain, tldPattern.Suffix) {
			return tldPattern.Score
		}
	}

	// Helper for safe domain matching (exact match or subdomain boundary)
	domainMatches := func(host, pattern string) bool {
		host = strings.ToLower(host)
		pattern = strings.ToLower(pattern)
		if host == pattern {
			return true
		}
		// Allow subdomains (e.g., docs.github.com matches github.com)
		if strings.HasSuffix(host, "."+pattern) {
			return true
		}
		return false
	}

	// Check domain groups
	for _, group := range config.CredibilityRules.DomainGroups {
		for _, knownDomain := range group.Domains {
			if domainMatches(domain, knownDomain) {
				return group.Score
			}
		}
	}

	// Return default score
	if config.CredibilityRules.DefaultScore > 0 {
		return config.CredibilityRules.DefaultScore
	}
	return 0.60
}

// CalculateSourceDiversity calculates diversity metric
// Formula: unique_domains / total_sources
// Higher is better (more diverse sources)
func CalculateSourceDiversity(citations []Citation) float64 {
	if len(citations) == 0 {
		return 0.0
	}

	domainSet := make(map[string]bool)
	for _, c := range citations {
		domainSet[c.Source] = true
	}

	return float64(len(domainSet)) / float64(len(citations))
}

// extractCitationFromSearchResult extracts a Citation from a web_search result
func extractCitationFromSearchResult(result map[string]interface{}, agentID string, now time.Time) (*Citation, error) {
	// Extract required fields
	urlStr, ok := result["url"].(string)
	if !ok || urlStr == "" {
		return nil, fmt.Errorf("missing or invalid url")
	}

	title, _ := result["title"].(string)
	snippet, _ := result["text"].(string)
	if snippet == "" {
		snippet, _ = result["snippet"].(string)
	}

	// Try to get content for snippet fallback
	content, _ := result["content"].(string)

	// Normalize URL for deduplication
	normalizedURL, err := NormalizeURL(urlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize URL: %w", err)
	}

	// Extract domain
	domain, err := ExtractDomain(normalizedURL)
	if err != nil {
		return nil, fmt.Errorf("failed to extract domain: %w", err)
	}

	// Ensure snippet is not empty
	snippet = ensureSnippet(snippet, content, title, normalizedURL, MinSnippetLength)

	// Extract scores and metadata
	relevanceScore := 0.5 // default
	if score, ok := result["score"].(float64); ok {
		relevanceScore = score
	}

	// Try to parse published date
	var publishedDate *time.Time
	if pubDateStr, ok := result["published_date"].(string); ok && pubDateStr != "" {
		if parsed, err := time.Parse(time.RFC3339, pubDateStr); err == nil {
			publishedDate = &parsed
		}
	}

	// Determine source type from domain or category
	sourceType := "web"
	if category, ok := result["category"].(string); ok {
		sourceType = category
	}

	// Calculate quality and credibility scores
	qualityScore := ScoreQuality(relevanceScore, publishedDate, title != "", snippet != "", now)
	credibilityScore := ScoreCredibility(domain)

	// Citation V2: extract tool_source field
	toolSource := "search" // default for search results
	if ts, ok := result["tool_source"].(string); ok && ts != "" {
		toolSource = ts
	}

	return &Citation{
		URL:              normalizedURL,
		Title:            title,
		Source:           domain,
		SourceType:       sourceType,
		ToolSource:       toolSource,
		RetrievedAt:      now,
		PublishedDate:    publishedDate,
		RelevanceScore:   relevanceScore,
		QualityScore:     qualityScore,
		CredibilityScore: credibilityScore,
		AgentID:          agentID,
		Snippet:          snippet,
	}, nil
}

// extractCitationFromFetchResult extracts a Citation from a web_fetch result
func extractCitationFromFetchResult(result map[string]interface{}, agentID string, now time.Time) (*Citation, error) {
	// Extract required fields
	urlStr, ok := result["url"].(string)
	if !ok || urlStr == "" {
		return nil, fmt.Errorf("missing or invalid url")
	}

	title, _ := result["title"].(string)
	content, _ := result["content"].(string)

	// Try explicit snippet field first, then fall back to content
	snippet := ""
	if s, ok := result["snippet"].(string); ok && len(s) > 0 {
		snippet = s
	} else if len(content) > 0 {
		snippet = truncateRunes(content, MaxSnippetLength)
	}

	// Normalize URL
	normalizedURL, err := NormalizeURL(urlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize URL: %w", err)
	}

	// Extract domain
	domain, err := ExtractDomain(normalizedURL)
	if err != nil {
		return nil, fmt.Errorf("failed to extract domain: %w", err)
	}

	// Ensure snippet is not empty
	snippet = ensureSnippet(snippet, content, title, normalizedURL, MinSnippetLength)

	// Try to parse published date
	var publishedDate *time.Time
	if pubDateStr, ok := result["published_date"].(string); ok && pubDateStr != "" {
		if parsed, err := time.Parse(time.RFC3339, pubDateStr); err == nil {
			publishedDate = &parsed
		}
	}

	// web_fetch results are fetched for detailed analysis, give higher relevance
	relevanceScore := 0.8

	// Calculate quality and credibility scores
	qualityScore := ScoreQuality(relevanceScore, publishedDate, title != "", snippet != "", now)
	credibilityScore := ScoreCredibility(domain)

	// Citation V2: extract tool_source field
	toolSource := "fetch" // default for fetch results
	if ts, ok := result["tool_source"].(string); ok && ts != "" {
		toolSource = ts
	}

	return &Citation{
		URL:              normalizedURL,
		Title:            title,
		Source:           domain,
		SourceType:       "web",
		ToolSource:       toolSource,
		RetrievedAt:      now,
		PublishedDate:    publishedDate,
		RelevanceScore:   relevanceScore,
		QualityScore:     qualityScore,
		CredibilityScore: credibilityScore,
		AgentID:          agentID,
		Snippet:          snippet,
	}, nil
}

// extractCitationsFromResponse attempts to parse citations from agent response text
// This is a fallback when tool_executions are missing but the response contains structured search results
func extractCitationsFromResponse(response string, agentID string, now time.Time) []Citation {
	var citations []Citation

	// Try to parse response as JSON array of search results
	// Common format: [{"url": "...", "title": "...", "snippet": "...", ...}, ...]
	var results []map[string]interface{}
	if err := json.Unmarshal([]byte(response), &results); err != nil {
		return citations // Not JSON array, no fallback possible
	}

	// Extract citations from parsed results
	for _, result := range results {
		if citation, err := extractCitationFromSearchResult(result, agentID, now); err == nil {
			citations = append(citations, *citation)
		}
	}

	return citations
}

// containsLLMSignals detects if text contains LLM-generated content signals.
// Uses a three-tier approach:
// 1. Strong signals: immediate pollution (tool-call/XML residue)
// 2. JSON detection: requires ≥2 JSON keys to trigger
// 3. Weak signals: need ≥2 matches AND at least one extraction keyword
func containsLLMSignals(text string) bool {
	if text == "" {
		return false
	}

	// Strong signals: any match = immediate pollution (high-confidence tool/XML residue only)
	strongSignals := []string{
		// XML/function call residue (generalized prefix matching)
		"</invoke>", "</function_calls>", "<function_calls>",
		"<parameter>", "</parameter>", "<invoke ",
		"<web_fetch>", "</web_fetch>", "<web_search>", "</web_search>",
		"<web_subpage_fetch>", "</web_subpage_fetch>",
		"<attempt_", "</attempt_", // Generalized matching for <attempt_*> series

		// Tool call structure residue (standalone words and JSON format)
		"tool_executions",
		"web_subpage_fetch", // standalone match
		`"tool": "web_fetch"`, `"tool": "web_search"`,
		`'tool': 'web_fetch'`, `'tool': 'web_search'`,

		// High-confidence Agent templates (complete action+object combinations)
		"信息提取结果", "情報抽出結果",
		"公司信息提取", "子页面分析",
		"我已成功从", "successfully extracted from",
		"深入抓取", "サブページから抽出",

		// Agent planning/meta-commentary statements (Phase 3)
		"Direct fetch of",                  // Agent suggested action
		"were not included in the initial", // Agent meta-commentary
		"would be needed (these",           // Exact match with parenthesis

		// Agent structured output format
		"**URL:**",                    // Agent extracted URL field
		"Key Business Products",       // Agent analysis header
		"Products Identified",         // Agent analysis header variant
	}

	for _, sig := range strongSignals {
		if strings.Contains(text, sig) {
			return true
		}
	}

	// Extraction keywords: weak signals must be combined with these to trigger
	extractionKeywords := []string{
		// Chinese
		"提取", "抽取", "整理", "获取结果",
		// Japanese
		"抽出", "取得",
		// English
		"extract", "retriev",
	}

	hasExtractionKeyword := false
	for _, kw := range extractionKeywords {
		if strings.Contains(text, kw) {
			hasExtractionKeyword = true
			break
		}
	}

	// JSON-like structure detection: requires ≥2 JSON keys present
	jsonKeys := []string{
		`"results":`, `'results':`,
		`"title":`, `{'title':`, `'title':`,
		`"url":`, `'url':`,
		`"source":`, `'source':`,
		`"position":`, `'position':`,
	}
	jsonKeyCount := 0
	for _, jk := range jsonKeys {
		if strings.Contains(text, jk) {
			jsonKeyCount++
		}
	}
	if jsonKeyCount >= MinLLMSignalMatches {
		return true
	}

	// Structured field detection (Phase 3): requires ≥2 fields to trigger
	structuredFields := []string{
		"- **Industry:**",
		"- **Website:**",
		"- **Founded:**",
		"- **Headquarters:**",
		"- **CEO:**",
		"- **Revenue:**",
		"- **Employees:**",
		"- **Company:**",
	}
	fieldCount := 0
	for _, f := range structuredFields {
		if strings.Contains(text, f) {
			fieldCount++
		}
	}
	if fieldCount >= MinLLMSignalMatches {
		return true
	}

	// Weak signals: require ≥2 matches + extraction keyword
	if !hasExtractionKeyword {
		return false // No extraction keyword, weak signals don't trigger
	}

	weakSignals := []string{
		// Chinese weak signals (excluding high-frequency words like "以下是")
		"我需要", "让我", "基于我的", "关键信息",

		// Japanese weak signals (excluding "以下の", "情報を", etc.)
		"ツールで", "抽出しました", "取得しました",

		// English weak signals
		"I found", "Let me", "Based on my", "Here is the",

		// Markdown structure weak signals
		"## 基本", "## 概要", "## Summary",
		"**公司", "**企業", "**Company",
	}

	weakCount := 0
	for _, sig := range weakSignals {
		if strings.Contains(text, sig) {
			weakCount++
			if weakCount >= MinLLMSignalMatches {
				return true
			}
		}
	}

	return false
}

// extractCitationsFromPlainTextResponse extracts citations by scanning plain-text
// agent responses for HTTP/HTTPS URLs when structured tool outputs are absent.
// This is a conservative fallback to ensure metadata contains citations even when
// agents only mention sources narratively.
func extractCitationsFromPlainTextResponse(response string, agentID string, now time.Time) []Citation {
	var citations []Citation
	if strings.TrimSpace(response) == "" {
		return citations
	}

	// Regex for http(s) URLs, stop at whitespace or common trailing punctuation / tag delimiters
	urlRe := regexp.MustCompile(`https?://[^\s\]\)\>\<\"']+`)
	matches := urlRe.FindAllStringIndex(response, -1)
	if len(matches) == 0 {
		return citations
	}

	// Deduplicate per-response by normalized URL
	seen := make(map[string]bool)

	// Some models wrap URLs with tags like <url>...</url> or include encoded variants.
	// Strip common closing-tag suffix artifacts so citations remain valid URLs.
	trimSuffixFold := func(s, suffixLower string) (string, bool) {
		if strings.HasSuffix(strings.ToLower(s), suffixLower) {
			return s[:len(s)-len(suffixLower)], true
		}
		return s, false
	}
	stripURLTagSuffix := func(s string) string {
		for {
			before := s
			if v, ok := trimSuffixFold(s, "</url>"); ok {
				s = v
			}
			if v, ok := trimSuffixFold(s, "</url"); ok {
				s = v
			}
			if v, ok := trimSuffixFold(s, "%3c/url%3e"); ok {
				s = v
			}
			if v, ok := trimSuffixFold(s, "%3c/url"); ok {
				s = v
			}
			if v, ok := trimSuffixFold(s, "&lt;/url&gt;"); ok {
				s = v
			}
			if v, ok := trimSuffixFold(s, "&lt;/url&gt"); ok {
				s = v
			}
			if v, ok := trimSuffixFold(s, "&lt;/url"); ok {
				s = v
			}
			if s == before {
				break
			}
		}
		return s
	}

	// trimNonASCIISuffix truncates the URL at the first non-ASCII character
	// This handles cases where Chinese text (e.g., "（个人介绍）") is attached to URLs
	trimNonASCIISuffix := func(s string) string {
		for i := 0; i < len(s); i++ {
			if s[i] > 127 {
				return s[:i]
			}
		}
		return s
	}

	// Helper to build a small context snippet around the URL (UTF-8 safe)
	getSnippet := func(start, end int) string {
		r := []rune(response)
		// Map byte indices to rune indices conservatively
		// Fallback: slice by bytes if mapping fails
		// Simplicity: approximate by converting entire string to runes and searching substring
		urlText := response[start:end]
		ri := strings.Index(string(r), urlText)
		if ri < 0 {
			// Fallback: just return first 200 runes of the tail starting at start
			if start < len(response) {
				tail := []rune(response[start:])
				if len(tail) > 200 {
					return string(tail[:200]) + "..."
				}
				return string(tail)
			}
			return ""
		}
		lo := ri - 120
		hi := ri + len([]rune(urlText)) + 120
		if lo < 0 {
			lo = 0
		}
		if hi > len(r) {
			hi = len(r)
		}
		// Ensure lo <= hi after clamping (can happen with multi-byte chars at boundaries)
		if lo > hi {
			lo = 0
			if hi < len(r) {
				hi = len(r)
			}
		}
		if lo >= len(r) {
			return ""
		}
		snippet := string(r[lo:hi])
		if len([]rune(snippet)) > 220 {
			snippet = string([]rune(snippet)[:220]) + "..."
		}

		// Filter out LLM-generated content pollution
		// This prevents agent response text from being stored as webpage snippets
		if containsLLMSignals(snippet) {
			return ""
		}

		return snippet
	}

	for _, m := range matches {
		start, end := m[0], m[1]
		raw := response[start:end]
		// Trim trailing punctuation that often gets attached in prose
		raw = strings.TrimRight(raw, ",.;:)]}")
		raw = stripURLTagSuffix(raw)
		// Truncate at first non-ASCII character (e.g., Chinese parentheses)
		raw = trimNonASCIISuffix(raw)
		// Normalize URL
		normalized, err := NormalizeURL(raw)
		if err != nil || normalized == "" {
			continue
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true

		// Skip sitemap, robots.txt, static assets, etc.
		if shouldSkipURL(normalized) {
			continue
		}

		domain, err := ExtractDomain(normalized)
		if err != nil || domain == "" {
			continue
		}

		snippet := getSnippet(start, end)

		// Conservative relevance for narrative references
		relevance := 0.4
		quality := ScoreQuality(relevance, nil, false, snippet != "", now)
		credibility := ScoreCredibility(domain)

		citations = append(citations, Citation{
			URL:              normalized,
			Title:            "", // Unknown in plain text; formatter will display URL/domain
			Source:           domain,
			SourceType:       "web",
			ToolSource:       "", // Citation V2: unknown origin (URL scan fallback)
			RetrievedAt:      now,
			PublishedDate:    nil,
			RelevanceScore:   relevance,
			QualityScore:     quality,
			CredibilityScore: credibility,
			AgentID:          agentID,
			Snippet:          snippet,
		})
	}

	if isCitationsDebugEnabled() {
		log.Printf("[citations] plain_text_extracted=%d", len(citations))
	}
	return citations
}

// extractCitationsFromToolOutput extracts citations from a tool execution output
func extractCitationsFromToolOutput(toolName string, output interface{}, agentID string, now time.Time) []Citation {
	var citations []Citation

	if isCitationsDebugEnabled() {
		log.Printf("[citations] tool=%s output_type=%T", toolName, output)
	}

	switch toolName {
	case "web_search":
		// Case 1: Direct array (proto sometimes returns []interface{} directly)
		if arr, ok := output.([]interface{}); ok {
			for _, item := range arr {
				if resultMap, ok := item.(map[string]interface{}); ok {
					if citation, err := extractCitationFromSearchResult(resultMap, agentID, now); err == nil {
						citations = append(citations, *citation)
					}
				}
			}
			// Case 2: Wrapped in map {"results": [...]}
		} else if outputMap, ok := output.(map[string]interface{}); ok {
			if results, ok := outputMap["results"].([]interface{}); ok {
				for _, resultInterface := range results {
					if resultMap, ok := resultInterface.(map[string]interface{}); ok {
						if citation, err := extractCitationFromSearchResult(resultMap, agentID, now); err == nil {
							citations = append(citations, *citation)
						}
					}
				}
			}
			// Case 3: JSON string
		} else if s, ok := output.(string); ok && s != "" {
			// Fallback: output encoded as JSON string
			var decoded interface{}
			if err := json.Unmarshal([]byte(s), &decoded); err == nil {
				if arr, ok := decoded.([]interface{}); ok {
					for _, item := range arr {
						if resultMap, ok := item.(map[string]interface{}); ok {
							if citation, err := extractCitationFromSearchResult(resultMap, agentID, now); err == nil {
								citations = append(citations, *citation)
							}
						}
					}
				} else if m, ok := decoded.(map[string]interface{}); ok {
					if results, ok := m["results"].([]interface{}); ok {
						for _, item := range results {
							if resultMap, ok := item.(map[string]interface{}); ok {
								if citation, err := extractCitationFromSearchResult(resultMap, agentID, now); err == nil {
									citations = append(citations, *citation)
								}
							}
						}
					}
				}
			}
		}

	case "web_fetch":
		// Handle both single page and batch fetch formats
		if outputMap, ok := output.(map[string]interface{}); ok {
			// Check for batch format: {pages: [...], succeeded: N, failed: M}
			if pages, ok := outputMap["pages"].([]interface{}); ok {
				for _, page := range pages {
					if pageMap, ok := page.(map[string]interface{}); ok {
						// Only extract from successful pages
						if success, _ := pageMap["success"].(bool); success {
							if citation, err := extractCitationFromFetchResult(pageMap, agentID, now); err == nil {
								citations = append(citations, *citation)
							}
						}
					}
				}
			} else {
				// Single page format (backward compatible)
				if citation, err := extractCitationFromFetchResult(outputMap, agentID, now); err == nil {
					citations = append(citations, *citation)
				}
			}
		} else if s, ok := output.(string); ok && s != "" {
			var m map[string]interface{}
			if err := json.Unmarshal([]byte(s), &m); err == nil {
				// Check for batch format in JSON string
				if pages, ok := m["pages"].([]interface{}); ok {
					for _, page := range pages {
						if pageMap, ok := page.(map[string]interface{}); ok {
							if success, _ := pageMap["success"].(bool); success {
								if citation, err := extractCitationFromFetchResult(pageMap, agentID, now); err == nil {
									citations = append(citations, *citation)
								}
							}
						}
					}
				} else {
					if citation, err := extractCitationFromFetchResult(m, agentID, now); err == nil {
						citations = append(citations, *citation)
					}
				}
			}
		}

	case "web_subpage_fetch", "web_crawl":
		// Multi-page tools: try to extract citations from metadata.urls
		if outputMap, ok := output.(map[string]interface{}); ok {
			multiCitations := extractCitationsFromMultiPageResult(outputMap, agentID, now)
			if len(multiCitations) > 0 {
				citations = append(citations, multiCitations...)
			} else {
				// Fallback: treat as single page fetch
				if citation, err := extractCitationFromFetchResult(outputMap, agentID, now); err == nil {
					citations = append(citations, *citation)
				}
			}
		} else if s, ok := output.(string); ok && s != "" {
			var m map[string]interface{}
			if err := json.Unmarshal([]byte(s), &m); err == nil {
				multiCitations := extractCitationsFromMultiPageResult(m, agentID, now)
				if len(multiCitations) > 0 {
					citations = append(citations, multiCitations...)
				} else {
					if citation, err := extractCitationFromFetchResult(m, agentID, now); err == nil {
						citations = append(citations, *citation)
					}
				}
			}
		}
	}

	if isCitationsDebugEnabled() {
		log.Printf("[citations] tool=%s extracted=%d", toolName, len(citations))
	}
	return citations
}

// CollectCitations extracts, deduplicates, scores, and ranks citations from agent execution results
// Returns top N citations with aggregate statistics
func CollectCitations(results []interface{}, now time.Time, maxCitations int) ([]Citation, CitationStats) {
	if maxCitations <= 0 {
		maxCitations = 200 // default
	}

	var allCitations []Citation

	// Step 1: Extract from tool executions
	for _, resultInterface := range results {
		// Try to cast to map[string]interface{} first (common case)
		resultMap, ok := resultInterface.(map[string]interface{})
		if !ok {
			continue
		}

		agentID, _ := resultMap["agent_id"].(string)
		if agentID == "" {
			agentID = "unknown"
		}

		// Extract tool_executions array if present; allow nil/missing
		var toolExecutions []interface{}
		if tev, ok := resultMap["tool_executions"]; ok && tev != nil {
			if arr, ok := tev.([]interface{}); ok {
				toolExecutions = arr
			}
		}

		for _, toolExecInterface := range toolExecutions {
			toolExecMap, ok := toolExecInterface.(map[string]interface{})
			if !ok {
				continue
			}

			toolName, _ := toolExecMap["tool"].(string)
			success, _ := toolExecMap["success"].(bool)
			output := toolExecMap["output"]

			// Only process successful executions of web_search or web_fetch tools
			if !success || (toolName != "web_search" && toolName != "web_fetch" && toolName != "web_subpage_fetch" && toolName != "web_crawl") {
				continue
			}

			citations := extractCitationsFromToolOutput(toolName, output, agentID, now)
			allCitations = append(allCitations, citations...)
		}

		// Also attempt extraction from the agent's response text (helps when tool payloads are truncated or missing)
		if responseStr, ok := resultMap["response"].(string); ok && strings.TrimSpace(responseStr) != "" {
			// 1) Try structured JSON array fallback
			fallbackCitations := extractCitationsFromResponse(responseStr, agentID, now)
			if len(fallbackCitations) > 0 {
				allCitations = append(allCitations, fallbackCitations...)
			} else {
				// 2) Plain-text URL scan fallback
				plain := extractCitationsFromPlainTextResponse(responseStr, agentID, now)
				allCitations = append(allCitations, plain...)
			}
		}
	}

	if isCitationsDebugEnabled() {
		log.Printf("[citations] raw_extracted=%d", len(allCitations))
	}

	// Sanitize citation titles/metadata (e.g., fix arXiv reCAPTCHA titles)
	allCitations = sanitizeCitations(allCitations)

	// Step 2: Deduplicate by normalized URL
	dedupedCitations := deduplicateCitations(allCitations)

	// Step 3: Enforce diversity (max N per domain)
	config := LoadCredibilityConfig()
	maxPerDomain := 3
	if config.DiversityRules.MaxPerDomain > 0 {
		maxPerDomain = config.DiversityRules.MaxPerDomain
	}
	diverseCitations := enforceDiversity(dedupedCitations, maxPerDomain)

	// Step 4: Rank by combined score and limit to top N
	rankedCitations := rankAndLimit(diverseCitations, maxCitations)

	// Step 5: Calculate aggregate stats
	stats := calculateCitationStats(rankedCitations)

	if isCitationsDebugEnabled() {
		log.Printf("[citations] final_total=%d unique_domains=%d avg_quality=%.2f avg_cred=%.2f",
			len(rankedCitations), stats.UniqueDomains, stats.AvgQuality, stats.AvgCredibility)

		// P1-4: Extended metrics logging
		log.Printf("[citations] quality_buckets: low=%d medium=%d high=%d",
			stats.QualityBuckets["low"], stats.QualityBuckets["medium"], stats.QualityBuckets["high"])

		if len(stats.TopDomains) > 0 {
			var topDomainsStr string
			for i, d := range stats.TopDomains {
				if i > 0 {
					topDomainsStr += ", "
				}
				topDomainsStr += fmt.Sprintf("%s(%d)", d.Domain, d.Count)
				if i >= 4 { // Show top 5 in log
					break
				}
			}
			log.Printf("[citations] top_domains: %s", topDomainsStr)
		}

		if len(stats.PerAgentCount) > 0 {
			log.Printf("[citations] per_agent_count: %v", stats.PerAgentCount)
		}

		if stats.DuplicateURLs > 0 {
			log.Printf("[citations] duplicate_urls_removed=%d", stats.DuplicateURLs)
		}
	}

	return rankedCitations, stats
}

// deduplicateCitations removes duplicate citations by normalized URL (keeping first occurrence)
func deduplicateCitations(citations []Citation) []Citation {
	// Keep one entry per canonical key; merge to preserve best scores/metadata
	index := make(map[string]int)
	var deduped []Citation

	for _, citation := range citations {
		key := citation.URL
		// Prefer DOI-based key for cross-domain canonicalization
		if parsed, err := url.Parse(citation.URL); err == nil {
			if doi := extractDOIFromURL(parsed); doi != "" {
				key = "doi:" + strings.ToLower(doi)
			} else if norm, err2 := NormalizeURL(citation.URL); err2 == nil && norm != "" {
				key = norm
			}
		} else if norm, err2 := NormalizeURL(citation.URL); err2 == nil && norm != "" {
			key = norm
		}

		if idx, ok := index[key]; ok {
			// Merge: keep higher quality/credibility; fill missing metadata when available
			if citation.QualityScore > deduped[idx].QualityScore {
				deduped[idx].QualityScore = citation.QualityScore
			}
			if citation.CredibilityScore > deduped[idx].CredibilityScore {
				deduped[idx].CredibilityScore = citation.CredibilityScore
			}
			if deduped[idx].Title == "" && citation.Title != "" {
				deduped[idx].Title = citation.Title
			}
			if deduped[idx].Snippet == "" && citation.Snippet != "" {
				deduped[idx].Snippet = citation.Snippet
			}
			if deduped[idx].PublishedDate == nil && citation.PublishedDate != nil {
				deduped[idx].PublishedDate = citation.PublishedDate
			}
			// Relevance: prefer higher
			if citation.RelevanceScore > deduped[idx].RelevanceScore {
				deduped[idx].RelevanceScore = citation.RelevanceScore
			}
		} else {
			index[key] = len(deduped)
			deduped = append(deduped, citation)
		}
	}

	return deduped
}

// sanitizeCitations performs light cleanup on citation metadata
func sanitizeCitations(citations []Citation) []Citation {
	for i := range citations {
		citations[i].Title = sanitizeTitle(citations[i].Title, citations[i].URL, citations[i].Source)
	}
	return citations
}

func sanitizeTitle(title, rawURL, source string) string {
	t := strings.TrimSpace(title)
	lower := strings.ToLower(t)
	if strings.EqualFold(source, "arxiv.org") {
		if t == "" || strings.Contains(lower, "recaptcha") {
			if id := extractArxivID(rawURL); id != "" {
				return "arXiv:" + id
			}
			return "arXiv"
		}
	}
	return t
}

// extractArxivID returns the arXiv identifier from common arxiv.org URLs
// Examples: https://arxiv.org/abs/2408.13687 -> 2408.13687
//
//	https://arxiv.org/pdf/2408.13687.pdf -> 2408.13687
func extractArxivID(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	if !strings.Contains(strings.ToLower(u.Host), "arxiv.org") {
		return ""
	}
	p := strings.Trim(u.Path, "/")
	parts := strings.Split(p, "/")
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	// Strip .pdf if present
	if strings.HasSuffix(last, ".pdf") {
		last = strings.TrimSuffix(last, ".pdf")
	}
	// abs/<id> or pdf/<id>
	if len(parts) >= 2 {
		return last
	}
	return ""
}

// extractDOIFromURL attempts to extract a DOI from the URL or query
// Recognizes: host contains doi.org, query param "doi", or DOI pattern in path
func extractDOIFromURL(u *url.URL) string {
	// doi.org host
	if strings.Contains(strings.ToLower(u.Host), "doi.org") {
		doi := strings.Trim(u.Path, "/")
		if doi != "" {
			return doi
		}
	}
	// doi param in query (e.g., crossmark)
	if q := u.Query().Get("doi"); q != "" {
		return q
	}
	// DOI pattern in path (case-insensitive)
	// 10.XXXX/...
	re := regexp.MustCompile(`(?i)10\.[0-9]{4,9}/[-._;()/:A-Z0-9]+`)
	if m := re.FindString(u.Path); m != "" {
		return m
	}
	return ""
}

// truncateRunes returns s truncated to at most max runes, appending "..." when truncated.
func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}

// ensureSnippet guarantees a non-empty snippet from available fields.
// Priority: existing snippet (if clean) -> content -> title + url fallback
func ensureSnippet(snippet, content, title, urlStr string, minLen int) string {
	// Clear polluted snippets that contain LLM-generated content
	if containsLLMSignals(snippet) {
		snippet = ""
	}

	// Already has sufficient clean snippet
	if len([]rune(snippet)) >= minLen {
		return snippet
	}

	// Try content (also check for pollution)
	if len([]rune(content)) >= minLen && !containsLLMSignals(content) {
		return truncateRunes(content, MaxSnippetLength)
	}

	// Fallback to title + url for minimal context
	if title != "" {
		return fmt.Sprintf("%s - %s", title, urlStr)
	}

	// Return whatever we have (only if it's not polluted)
	if !containsLLMSignals(snippet) {
		return snippet
	}
	return ""
}

// enforceDiversity limits citations per domain to maintain source diversity
func enforceDiversity(citations []Citation, maxPerDomain int) []Citation {
	domainCount := make(map[string]int)
	var diverse []Citation

	// Sort by quality * credibility first to keep best sources per domain
	sorted := make([]Citation, len(citations))
	copy(sorted, citations)
	sortByScore(sorted)

	for _, citation := range sorted {
		if domainCount[citation.Source] < maxPerDomain {
			domainCount[citation.Source]++
			diverse = append(diverse, citation)
		}
	}

	return diverse
}

// rankAndLimit sorts citations by combined score and returns top N
func rankAndLimit(citations []Citation, limit int) []Citation {
	if len(citations) == 0 {
		return citations
	}

	// Sort by combined score (quality * credibility)
	sorted := make([]Citation, len(citations))
	copy(sorted, citations)
	sortByScore(sorted)

	// Limit to top N
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}

	return sorted
}

// sortByScore sorts citations by combined score (quality * credibility) descending
func sortByScore(citations []Citation) {
	sort.Slice(citations, func(i, j int) bool {
		scoreI := citations[i].QualityScore * citations[i].CredibilityScore
		scoreJ := citations[j].QualityScore * citations[j].CredibilityScore
		return scoreI > scoreJ // descending order
	})
}

// calculateCitationStats computes aggregate statistics
func calculateCitationStats(citations []Citation) CitationStats {
	if len(citations) == 0 {
		return CitationStats{}
	}

	uniqueDomains := make(map[string]bool)
	domainCounts := make(map[string]int)
	perAgentCount := make(map[string]int)
	qualityBuckets := map[string]int{"low": 0, "medium": 0, "high": 0}
	urlSet := make(map[string]bool)
	duplicateURLs := 0
	totalQuality := 0.0
	totalCredibility := 0.0

	for _, c := range citations {
		uniqueDomains[c.Source] = true
		domainCounts[c.Source]++
		totalQuality += c.QualityScore
		totalCredibility += c.CredibilityScore

		// P1-4: Quality buckets
		switch {
		case c.QualityScore < 0.3:
			qualityBuckets["low"]++
		case c.QualityScore < 0.6:
			qualityBuckets["medium"]++
		default:
			qualityBuckets["high"]++
		}

		// P1-4: Duplicate URL tracking
		if urlSet[c.URL] {
			duplicateURLs++
		}
		urlSet[c.URL] = true

		// P1-4: Per-agent count
		if c.AgentID != "" {
			perAgentCount[c.AgentID]++
		}
	}

	// P1-4: Top domains (sorted by count, top 10)
	topDomains := getTopDomains(domainCounts, 10)

	return CitationStats{
		TotalSources:    len(citations),
		UniqueDomains:   len(uniqueDomains),
		AvgQuality:      totalQuality / float64(len(citations)),
		AvgCredibility:  totalCredibility / float64(len(citations)),
		SourceDiversity: CalculateSourceDiversity(citations),
		QualityBuckets:  qualityBuckets,
		TopDomains:      topDomains,
		DuplicateURLs:   duplicateURLs,
		PerAgentCount:   perAgentCount,
	}
}

// getTopDomains returns the top N domains by citation count
func getTopDomains(domainCounts map[string]int, n int) []DomainCount {
	// Convert to slice for sorting
	domains := make([]DomainCount, 0, len(domainCounts))
	for domain, count := range domainCounts {
		domains = append(domains, DomainCount{Domain: domain, Count: count})
	}

	// Sort by count descending
	sort.Slice(domains, func(i, j int) bool {
		return domains[i].Count > domains[j].Count
	})

	// Take top N
	if len(domains) > n {
		domains = domains[:n]
	}
	return domains
}

// pageInfo holds title and snippet extracted from merged multi-page content
type pageInfo struct {
	Title   string
	Snippet string
}

// extractCitationsFromMultiPageResult extracts citations from web_subpage_fetch or web_crawl results
// that contain multiple pages merged into a single content with metadata.urls listing all page URLs
func extractCitationsFromMultiPageResult(output map[string]interface{}, agentID string, now time.Time) []Citation {
	var citations []Citation

	// 1. Get metadata.urls
	metadata, ok := output["metadata"].(map[string]interface{})
	if !ok {
		return nil
	}
	urlsRaw, ok := metadata["urls"].([]interface{})
	if !ok || len(urlsRaw) == 0 {
		return nil
	}

	// 2. Get merged content
	content := ""
	if c, ok := output["content"].(string); ok {
		content = c
	}

	// 3. Parse content to build url -> {title, snippet} mapping
	// Note: content may be truncated (~2000 chars), so not all pages will have snippet info
	pageInfoMap := parseMultiPageContent(content)

	if isCitationsDebugEnabled() {
		log.Printf("[citations] multi-page: urls=%d, pageInfoMap=%d", len(urlsRaw), len(pageInfoMap))
	}

	// 4. Create citation for each URL
	for _, urlVal := range urlsRaw {
		urlStr, ok := urlVal.(string)
		if !ok || urlStr == "" {
			continue
		}

		// Filter URLs that should not become sources
		if shouldSkipURL(urlStr) {
			continue
		}

		normalizedURL, err := NormalizeURL(urlStr)
		if err != nil {
			continue
		}

		domain, err := ExtractDomain(urlStr)
		if err != nil {
			domain = ""
		}

		title := ""
		snippet := ""

		// Look up page info (content may be truncated, so some pages won't have info)
		if info, found := pageInfoMap[normalizedURL]; found {
			title = info.Title
			snippet = info.Snippet
		} else if info, found := pageInfoMap[urlStr]; found {
			title = info.Title
			snippet = info.Snippet
		}

		// Ensure snippet is not empty - use title + url as fallback
		snippet = ensureSnippet(snippet, "", title, normalizedURL, MinSnippetLength)

		// Build complete Citation with proper scoring
		// Citation V2: extract tool_source from output
		toolSource := "fetch" // default for multi-page fetch
		if ts, ok := output["tool_source"].(string); ok && ts != "" {
			toolSource = ts
		}

		citation := Citation{
			URL:            normalizedURL,
			Title:          title,
			Source:         domain,
			SourceType:     "web",
			ToolSource:     toolSource,
			RetrievedAt:    now,
			RelevanceScore: 0.8, // fetch tools get fixed 0.8
			AgentID:        agentID,
			Snippet:        snippet,
		}

		// Calculate quality and credibility scores using existing functions
		citation.QualityScore = ScoreQuality(citation.RelevanceScore, citation.PublishedDate, title != "", snippet != "", now)
		citation.CredibilityScore = ScoreCredibility(domain)

		citations = append(citations, citation)
	}

	return citations
}

// parseMultiPageContent parses merged markdown content to extract title and snippet for each page
// Format patterns:
//   - web_subpage_fetch: "# Main Page: {url}" and "## Subpage N: {url}"
//   - web_crawl:         "# Main Page: {url}" and "## Page N: {url}"
func parseMultiPageContent(content string) map[string]pageInfo {
	result := make(map[string]pageInfo)

	if content == "" {
		return result
	}

	// Match page headers: # Main Page: URL or ## Subpage N: URL or ## Page N: URL
	headerPattern := regexp.MustCompile(`(?m)^#{1,2}\s+(?:Main Page|Subpage \d+|Page \d+):\s*(.+)$`)
	matches := headerPattern.FindAllStringSubmatchIndex(content, -1)

	for i, match := range matches {
		if len(match) < 4 {
			continue
		}

		// Extract URL from the header
		urlStart, urlEnd := match[2], match[3]
		pageURL := strings.TrimSpace(content[urlStart:urlEnd])

		// Determine content range for this page
		contentStart := match[1] // end of header line
		contentEnd := len(content)
		if i+1 < len(matches) {
			contentEnd = matches[i+1][0] // start of next header
		}

		pageContent := content[contentStart:contentEnd]

		// Extract title (may be on next line as **title**)
		title := extractTitleFromPageContent(pageContent)

		// Extract snippet (first 500 chars of actual content)
		snippet := extractSnippetFromPageContent(pageContent, MaxSnippetLength)

		result[pageURL] = pageInfo{Title: title, Snippet: snippet}

		// Also store with normalized URL as key
		if normalized, err := NormalizeURL(pageURL); err == nil && normalized != pageURL {
			result[normalized] = pageInfo{Title: title, Snippet: snippet}
		}
	}

	return result
}

// extractTitleFromPageContent extracts title from page content
// Looks for **title** pattern on the first non-empty line after the header
func extractTitleFromPageContent(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Look for **title** pattern
		if strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") {
			return strings.Trim(line, "*")
		}
		// If first non-empty line doesn't match pattern, stop looking
		break
	}
	return ""
}

// extractSnippetFromPageContent extracts first N characters of meaningful content
func extractSnippetFromPageContent(content string, maxLen int) string {
	lines := strings.Split(content, "\n")
	var sb strings.Builder
	skipFirstLines := true

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip title line if present
		if skipFirstLines && strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") {
			skipFirstLines = false
			continue
		}
		skipFirstLines = false

		// Skip markdown separators
		if line == "---" || strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") {
			continue
		}

		if sb.Len() > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(line)

		if sb.Len() >= maxLen {
			break
		}
	}

	result := sb.String()
	if len(result) > maxLen {
		result = result[:maxLen]
	}
	return result
}

// shouldSkipURL returns true if the URL should be skipped (not become a source)
// Filters out sitemap, robots.txt, static assets, etc.
func shouldSkipURL(urlStr string) bool {
	lower := strings.ToLower(urlStr)

	// Skip sitemaps, robots, and common non-content URLs
	skipPatterns := []string{
		".xml",
		"sitemap",
		"robots.txt",
		".css",
		".js",
		".ico",
		".png",
		".jpg",
		".jpeg",
		".gif",
		".svg",
		".woff",
		".woff2",
		".ttf",
		".eot",
		"favicon",
		"/feed",
		"/rss",
		"/atom",
	}

	for _, pattern := range skipPatterns {
		if strings.HasSuffix(lower, pattern) || strings.Contains(lower, pattern+"/") || strings.Contains(lower, pattern+"?") {
			return true
		}
	}

	// Skip low-value page types (authentication, search, error, legal, forms)
	lowValuePatterns := []string{
		// Authentication pages
		"/login", "/signin", "/sign-in", "/auth",
		"/register", "/signup", "/sign-up",
		"/logout", "/signout", "/sign-out",
		// Search/results pages (usually not useful as citations)
		"/search", "/results",
		// Error pages
		"/404", "/error", "not-found", "/500",
		// Legal/policy pages (boilerplate, not useful for research)
		"/terms", "/tos", "/terms-of-service",
		"/privacy", "/privacy-policy",
		"/legal", "/legal-notice",
		"/cookie", "/cookies",
		// Contact/form pages (mostly forms, not content)
		"/contact", "/contact-us", "/get-in-touch",
		"/support", "/help",
	}

	for _, pattern := range lowValuePatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	// Skip URLs with search query parameters
	if strings.Contains(lower, "?q=") || strings.Contains(lower, "?search=") || strings.Contains(lower, "?query=") {
		return true
	}

	return false
}

// ============================================================================
// Citation V2: Filter functions for Deep Research workflow
// ============================================================================

// CitationWithID wraps Citation with a sequential ID for verification tracking
type CitationWithID struct {
	ID       int      `json:"id"`       // Sequential ID (1, 2, 3...)
	Citation Citation `json:"citation"` // Original citation data
}

// FilterFetchOnlyAndAssignIDs filters citations to fetch-only sources and assigns sequential IDs.
// This is used by Deep Research workflow to:
// 1. Only use citations from actual page content (web_fetch/web_subpage_fetch/web_crawl)
// 2. Exclude search snippets which are often incomplete/unreliable for verification
// 3. Assign stable IDs for claim-citation mapping in verification
//
// Returns citations sorted by quality*credibility descending, with IDs 1, 2, 3...
func FilterFetchOnlyAndAssignIDs(citations []Citation) []CitationWithID {
	// Filter to fetch-only citations
	var fetchCitations []Citation
	for _, c := range citations {
		if c.ToolSource == "fetch" {
			fetchCitations = append(fetchCitations, c)
		}
	}

	// Sort by combined score (quality * credibility) descending
	sort.Slice(fetchCitations, func(i, j int) bool {
		scoreI := fetchCitations[i].QualityScore * fetchCitations[i].CredibilityScore
		scoreJ := fetchCitations[j].QualityScore * fetchCitations[j].CredibilityScore
		return scoreI > scoreJ
	})

	// Assign sequential IDs (1-indexed)
	result := make([]CitationWithID, len(fetchCitations))
	for i, c := range fetchCitations {
		result[i] = CitationWithID{
			ID:       i + 1, // 1-indexed
			Citation: c,
		}
	}

	if isCitationsDebugEnabled() {
		log.Printf("[citations] FilterFetchOnlyAndAssignIDs: input=%d fetch_only=%d", len(citations), len(result))
	}

	return result
}

// FilterByIDs returns only citations matching the given ID set.
// Used after verification to filter to only the citations that support claims.
// Preserves original order and IDs.
func FilterByIDs(citations []CitationWithID, ids []int) []CitationWithID {
	if len(ids) == 0 {
		return nil
	}

	// Build ID lookup set
	idSet := make(map[int]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	// Filter to matching IDs
	var result []CitationWithID
	for _, c := range citations {
		if idSet[c.ID] {
			result = append(result, c)
		}
	}

	if isCitationsDebugEnabled() {
		log.Printf("[citations] FilterByIDs: input=%d ids=%d matched=%d", len(citations), len(ids), len(result))
	}

	return result
}

// GetAllCitationIDs extracts all IDs from a slice of CitationWithID
func GetAllCitationIDs(citations []CitationWithID) []int {
	ids := make([]int, len(citations))
	for i, c := range citations {
		ids[i] = c.ID
	}
	return ids
}

// CitationWithIDToCitation extracts Citation slice from CitationWithID slice
func CitationWithIDToCitation(citations []CitationWithID) []Citation {
	result := make([]Citation, len(citations))
	for i, c := range citations {
		result[i] = c.Citation
	}
	return result
}
