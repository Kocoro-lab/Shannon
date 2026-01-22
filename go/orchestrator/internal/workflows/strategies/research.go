package strategies

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/agents"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/budget"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/config"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/formatting"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metadata"
	pricing "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/control"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/opts"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns/execution"
)

// FilterCitationsByEntity filters citations based on entity relevance when canonical name is detected.
//
// Scoring System (OR logic, not AND):
//   - Official domain match: +0.6 points (e.g., example.com, jp.example.com)
//   - Alias in URL: +0.4 points (broader domain matching)
//   - Title/snippet/source contains alias: +0.4 points
//   - Threshold: 0.3 (passes with any single match)
//
// Filtering Strategy:
//  1. Always keep ALL official domain citations (bypass threshold)
//  2. Keep non-official citations scoring >= threshold
//  3. Backfill to minKeep (10) using quality×credibility+entity_score
//
// Prevents Over-Filtering:
//   - Lower threshold (0.3) allows title/snippet matches (0.4) to pass
//   - Higher minKeep (10) for deep research coverage
//   - Official sites guaranteed inclusion
//
// Search Engine Variance:
//   - If search doesn't return official sites, filter won't create them
//   - If official sites in results, they're always preserved
//
// Future Improvements (see long-term plan):
//   - Phase 2: Soft rerank (multiply scores vs hard filter)
//   - Phase 3: Verification entity coherence
//   - Phase 4: Adaptive retry if coverage < target

// containsAsWord checks if text contains term as a whole word (word boundary matching).
// This prevents "mind" from matching "Minders.io" or "reminded".
func containsAsWord(text, term string) bool {
	if term == "" {
		return false
	}
	idx := strings.Index(text, term)
	if idx < 0 {
		return false
	}
	// Check left boundary
	if idx > 0 {
		prev := text[idx-1]
		if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
			// Previous char is alphanumeric, not a word boundary
			// Try to find next occurrence
			rest := text[idx+len(term):]
			return containsAsWord(rest, term)
		}
	}
	// Check right boundary
	endIdx := idx + len(term)
	if endIdx < len(text) {
		next := text[endIdx]
		if (next >= 'a' && next <= 'z') || (next >= '0' && next <= '9') {
			// Next char is alphanumeric, not a word boundary
			rest := text[idx+len(term):]
			return containsAsWord(rest, term)
		}
	}
	return true
}

func FilterCitationsByEntity(citations []metadata.Citation, canonicalName string, aliases []string, officialDomains []string) []metadata.Citation {
	if canonicalName == "" || len(citations) == 0 {
		return citations
	}

	const (
		threshold = 0.3 // Minimum relevance score to pass (title/snippet match = 0.4 can pass)
		minKeep   = 10  // Safety floor: keep at least this many for deep research
	)

	// Normalize canonical name and aliases for matching
	canonical := strings.ToLower(strings.TrimSpace(canonicalName))
	aliasSet := make(map[string]bool)
	aliasSet[canonical] = true
	for _, a := range aliases {
		normalized := strings.ToLower(strings.TrimSpace(a))
		if normalized != "" {
			aliasSet[normalized] = true
		}
	}

	// Normalize official domains (extract domain from URL or use as-is)
	domainSet := make(map[string]bool)
	for _, d := range officialDomains {
		normalized := strings.ToLower(strings.TrimSpace(d))
		// Remove protocol if present
		normalized = strings.TrimPrefix(normalized, "https://")
		normalized = strings.TrimPrefix(normalized, "http://")
		normalized = strings.TrimPrefix(normalized, "www.")
		if normalized != "" {
			domainSet[normalized] = true
		}
	}

	type scoredCitation struct {
		citation      metadata.Citation
		score         float64
		isOfficial    bool
		matchedDomain string
		matchedAlias  string
	}

	var scored []scoredCitation
	var officialSites []scoredCitation

	for _, c := range citations {
		score := 0.0
		isOfficial := false
		matchedDomain := ""
		matchedAlias := ""

		// Check 1: Domain match - stronger signal for official domains
		urlLower := strings.ToLower(c.URL)
		for domain := range domainSet {
			if strings.Contains(urlLower, domain) {
				score += 0.6 // Increased weight for domain match
				isOfficial = true
				matchedDomain = domain
				break
			}
		}

		// Also check if URL contains any alias (broader domain matching)
		// For URLs, we use substring matching since domains often contain the brand
		// e.g., "acme.com" contains "acme"
		hostLabels := []string{}
		if parsedURL, err := url.Parse(c.URL); err == nil {
			host := strings.ToLower(parsedURL.Hostname())
			host = strings.TrimPrefix(host, "www.")
			if host != "" {
				hostLabels = strings.Split(host, ".")
			}
		}
		if !isOfficial {
			for alias := range aliasSet {
				// Remove quotes from aliases for URL matching
				cleanAlias := strings.Trim(alias, "\"")
				if cleanAlias == "" {
					continue
				}
				if len(cleanAlias) >= 5 {
					// For long aliases, allow substring match (brand in domain/path).
					if strings.Contains(urlLower, cleanAlias) {
						score += 0.4 // Partial credit for alias in URL
						matchedDomain = "alias-in-url:" + cleanAlias
						break
					}
					continue
				}

				// For short aliases, only match whole hostname labels to avoid false positives
				// like "mind" matching "minders.io".
				for _, label := range hostLabels {
					if label == cleanAlias {
						score += 0.4
						matchedDomain = "alias-in-host:" + cleanAlias
						break
					}
				}
				if matchedDomain != "" {
					break
				}
			}
		}

		// Check 2: Title/snippet contains canonical name or aliases
		titleLower := strings.ToLower(c.Title)
		snippetLower := strings.ToLower(c.Snippet)
		sourceLower := strings.ToLower(c.Source)
		combined := titleLower + " " + snippetLower + " " + sourceLower

		for alias := range aliasSet {
			cleanAlias := strings.Trim(alias, "\"")
			// Use word boundary matching to prevent "mind" matching "Minders.io"
			// For short aliases (<5 chars), require exact word match
			// For longer aliases, allow partial matches (e.g., "acme" in "acme.com")
			if cleanAlias != "" {
				if len(cleanAlias) < 5 {
					// Short alias: require word boundary
					if containsAsWord(combined, cleanAlias) {
						score += 0.4 // Title/snippet match
						matchedAlias = cleanAlias
						break
					}
				} else {
					// Longer alias: allow substring match
					if strings.Contains(combined, cleanAlias) {
						score += 0.4 // Title/snippet match
						matchedAlias = cleanAlias
						break
					}
				}
			}
		}

		sc := scoredCitation{
			citation:      c,
			score:         score,
			isOfficial:    isOfficial,
			matchedDomain: matchedDomain,
			matchedAlias:  matchedAlias,
		}
		scored = append(scored, sc)

		// Track official sites separately for backfill
		if isOfficial {
			officialSites = append(officialSites, sc)
		}
	}

	// Step 1: Always keep official domain citations (bypass threshold)
	var filtered []metadata.Citation
	officialKept := 0
	for _, sc := range officialSites {
		filtered = append(filtered, sc.citation)
		officialKept++
	}

	// Step 2: Add non-official citations that pass threshold
	for _, sc := range scored {
		if !sc.isOfficial && sc.score >= threshold {
			filtered = append(filtered, sc.citation)
		}
	}

	// Step 3: Safety floor with backfill
	if len(filtered) < minKeep {
		// Sort all citations by combined score (quality × credibility + entity relevance)
		sort.Slice(scored, func(i, j int) bool {
			scoreI := (scored[i].citation.QualityScore * scored[i].citation.CredibilityScore) + scored[i].score
			scoreJ := (scored[j].citation.QualityScore * scored[j].citation.CredibilityScore) + scored[j].score
			return scoreI > scoreJ
		})

		// Backfill from top-scored citations
		existingURLs := make(map[string]bool)
		for _, c := range filtered {
			existingURLs[c.URL] = true
		}

		for i := 0; i < len(scored) && len(filtered) < minKeep; i++ {
			if !existingURLs[scored[i].citation.URL] {
				filtered = append(filtered, scored[i].citation)
			}
		}
	}

	return filtered
}

// CitationFilterResult holds the result of citation filtering with fallback logic.
type CitationFilterResult struct {
	Citations []metadata.Citation
	Before    int
	After     int
	Retention float64
	Applied   bool // true if filter applied, false if fallback (kept original)
}

// Constants for citation filter fallback thresholds
const (
	citationFilterMinCount     = 20  // Minimum citations to accept filter result
	citationFilterMinRetention = 0.3 // Minimum retention rate (30%)
)

// Constants for domain discovery
const (
	maxProductHints       = 5 // Maximum product domains to extract from refiner hints
	maxResearchFocusAreas = 3 // Maximum research areas to include in discovery query
	minProductNameLength  = 3 // Minimum length for valid product name extraction
)

// P0-B: Constants for V2 + V1 Supplement logic
const (
	v2MinSupportRate    = 0.1 // 10% - below this, enable V1 supplement
	v2MinCitationsUsed  = 3   // Minimum inline citations to consider V2 sufficient
	v2MinClaimsRequired = 5   // Minimum claims to trigger supplement (avoid short answer false positives)
	v1MaxExtraCitations = 10  // Maximum additional citations from V1 supplement
)

// ApplyCitationFilterWithFallback applies entity-based filtering with automatic
// fallback when the filter would remove too many citations.
// Returns a CitationFilterResult containing the filtered citations and metadata.
func ApplyCitationFilterWithFallback(
	citations []metadata.Citation,
	canonicalName string,
	aliases []string,
	domains []string,
) CitationFilterResult {
	beforeCount := len(citations)
	if beforeCount == 0 {
		return CitationFilterResult{Citations: citations, Applied: false}
	}

	filtered := FilterCitationsByEntity(citations, canonicalName, aliases, domains)
	retention := float64(len(filtered)) / float64(beforeCount)

	result := CitationFilterResult{
		Before:    beforeCount,
		After:     len(filtered),
		Retention: retention,
	}

	// Apply filter only if results are reasonable (>=20 citations AND >=30% retention)
	// Using AND ensures we don't apply aggressive filtering that removes too many citations
	if len(filtered) >= citationFilterMinCount && retention >= citationFilterMinRetention {
		result.Citations = filtered
		result.Applied = true
	} else {
		result.Citations = citations // Keep original
		result.Applied = false
	}

	return result
}

func mergeCitationsPreferFirst(primary []metadata.Citation, secondary []metadata.Citation) []metadata.Citation {
	if len(primary) == 0 {
		return secondary
	}
	if len(secondary) == 0 {
		return primary
	}

	out := make([]metadata.Citation, 0, len(primary)+len(secondary))
	seen := make(map[string]bool)

	citationKey := func(c metadata.Citation) string {
		if strings.TrimSpace(c.URL) != "" {
			if normalized, err := metadata.NormalizeURL(c.URL); err == nil && normalized != "" {
				return normalized
			}
			return strings.ToLower(strings.TrimSpace(c.URL))
		}
		if strings.TrimSpace(c.Source) != "" {
			return strings.ToLower(strings.TrimSpace(c.Source))
		}
		if strings.TrimSpace(c.Title) != "" {
			return strings.ToLower(strings.TrimSpace(c.Title))
		}
		return ""
	}

	add := func(c metadata.Citation) {
		key := citationKey(c)
		if key != "" {
			if seen[key] {
				return
			}
			seen[key] = true
		}
		out = append(out, c)
	}

	for _, c := range primary {
		add(c)
	}
	for _, c := range secondary {
		add(c)
	}

	return out
}

func hasSuccessfulToolExecutions(results []activities.AgentExecutionResult) bool {
	for _, ar := range results {
		for _, te := range ar.ToolExecutions {
			if te.Success {
				return true
			}
		}
	}
	return false
}

// buildCompanyPrefetchURLs constructs a small set of candidate URLs for company research.
// Priority:
//  1. Use official_domains from refinement when available.
//  2. Add config-based aggregator sources (Crunchbase, LinkedIn, etc.)
//  3. Fallback to a simple canonical_name-based .com heuristic.
//
// All outputs are normalized to https://host form and deduplicated.
func buildCompanyPrefetchURLs(canonicalName string, officialDomains []string) []string {
	return buildCompanyPrefetchURLsWithLocale(canonicalName, officialDomains, "")
}

// languageToCode normalizes language strings to 2-letter codes.
// Handles variants like zh-cn, zh-hans, japanese, etc.
func languageToCode(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	// Handle Chinese variants: zh-cn, zh-hans, zh-tw, chinese, etc.
	if strings.HasPrefix(l, "zh") || l == "chinese" {
		return "zh"
	}
	// Handle Japanese variants: ja, ja-jp, japanese, etc.
	if strings.HasPrefix(l, "ja") || l == "japanese" {
		return "ja"
	}
	// Handle Korean variants: ko, ko-kr, korean, etc.
	if strings.HasPrefix(l, "ko") || l == "korean" {
		return "ko"
	}
	if len(l) == 2 {
		return l
	}
	return ""
}

// extractRegionCodeFromTargetLanguages extracts regional language code from LLM-determined target_languages.
// Returns the first regional code found (zh, ja, ko), or empty string if none.
// This is used to determine locale-specific sources based on company region, not query language.
func extractRegionCodeFromTargetLanguages(targetLanguages []string) string {
	for _, lang := range targetLanguages {
		code := languageToCode(lang)
		switch code {
		case "zh", "ja", "ko":
			return code
		}
	}
	return ""
}

// originPrefetchRegionFromTargetLanguages maps entity origin language codes to prefetch region buckets.
// - zh -> cn
// - ja -> jp
// - ko -> kr
func originPrefetchRegionFromTargetLanguages(targetLanguages []string) string {
	switch extractRegionCodeFromTargetLanguages(targetLanguages) {
	case "zh":
		return "cn"
	case "ja":
		return "jp"
	case "ko":
		return "kr"
	default:
		return ""
	}
}

func normalizePrefetchRegion(raw string) string {
	r := strings.ToLower(strings.TrimSpace(raw))
	switch r {
	case "cn", "china", "zh", "zh-cn", "zh-hans", "chinese":
		return "cn"
	case "jp", "japan", "ja", "ja-jp", "japanese":
		return "jp"
	case "kr", "korea", "ko", "ko-kr", "korean":
		return "kr"
	case "eu", "europe", "european", "uk", "u.k.":
		return "eu"
	case "us", "u.s.", "usa", "united states":
		return "us"
	case "global", "worldwide", "intl", "international":
		return "global"
	default:
		return ""
	}
}

func prefetchRegionsFromContext(ctx map[string]interface{}) []string {
	if ctx == nil {
		return nil
	}

	keys := []string{"domain_analysis_regions", "prefetch_regions", "domain_prefetch_regions"}
	for _, key := range keys {
		raw, ok := ctx[key]
		if !ok || raw == nil {
			continue
		}
		seen := make(map[string]bool)
		var out []string
		add := func(v string) {
			r := normalizePrefetchRegion(v)
			if r == "" || seen[r] {
				return
			}
			seen[r] = true
			out = append(out, r)
		}

		switch v := raw.(type) {
		case []string:
			for _, s := range v {
				add(s)
			}
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok {
					add(s)
				}
			}
		case string:
			parts := strings.FieldsFunc(v, func(r rune) bool {
				return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
			})
			for _, p := range parts {
				add(p)
			}
		default:
			// Ignore unknown shapes
		}

		if len(out) > 0 {
			return out
		}
	}

	return nil
}

func prefetchRegionsFromQuery(query string) []string {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}

	seen := make(map[string]bool)
	var out []string
	add := func(region string) {
		r := normalizePrefetchRegion(region)
		if r == "" || seen[r] {
			return
		}
		seen[r] = true
		out = append(out, r)
	}

	// Chinese keywords
	if strings.Contains(q, "日本") {
		add("jp")
	}
	if strings.Contains(q, "中国") || strings.Contains(q, "中國") {
		add("cn")
	}
	if strings.Contains(q, "美国") || strings.Contains(q, "美國") {
		add("us")
	}
	if strings.Contains(q, "欧洲") || strings.Contains(q, "歐洲") || strings.Contains(q, "欧盟") || strings.Contains(q, "歐盟") {
		add("eu")
	}
	if strings.Contains(q, "英国") || strings.Contains(q, "英國") {
		add("eu")
	}

	// English (case-sensitive for US/EU to avoid matching pronouns like "us")
	qLower := strings.ToLower(q)
	if strings.Contains(qLower, "japan") {
		add("jp")
	}
	if strings.Contains(qLower, "china") {
		add("cn")
	}
	if strings.Contains(q, "USA") || strings.Contains(q, "U.S.") || containsAsWord(q, "US") || strings.Contains(qLower, "united states") {
		add("us")
	}
	if containsAsWord(q, "EU") || strings.Contains(qLower, "europe") || strings.Contains(qLower, "european") || containsAsWord(q, "UK") || strings.Contains(q, "U.K.") {
		add("eu")
	}

	return out
}

type domainDiscoverySearch struct {
	Key   string
	Query string
}

func buildCompanyEUDomainDiscoverySearchQuery(canonicalName string) string {
	name := strings.TrimSpace(canonicalName)
	if name == "" {
		return ""
	}
	// Keep this simple: avoid adding disambiguation terms, consistent with buildCompanyDomainDiscoverySearchQuery().
	return fmt.Sprintf("%s official website Europe", name)
}

func buildDomainDiscoverySearches(canonicalName string, disambiguationTerms []string, originRegion string, requestedRegions []string, researchAreas []string, officialDomains []string) []domainDiscoverySearch {
	name := strings.TrimSpace(canonicalName)
	if name == "" {
		return nil
	}

	seen := make(map[string]bool)
	var out []domainDiscoverySearch
	add := func(key, query string) {
		query = strings.TrimSpace(query)
		if query == "" || seen[query] {
			return
		}
		seen[query] = true
		out = append(out, domainDiscoverySearch{Key: key, Query: query})
	}

	// Helper to add topic-based and product searches
	addTopicSearches := func() {
		// Financial IR search
		if containsFinancialTopic(researchAreas) {
			add("ir", fmt.Sprintf("%s investor relations", name))
		}
		// Technical documentation search
		if containsTechnicalTopic(researchAreas) {
			add("docs", fmt.Sprintf("%s documentation", name))
		}
		// Culture/careers search
		if containsCultureTopic(researchAreas) {
			add("careers", fmt.Sprintf("%s careers", name))
		}
		// Product hints from refiner (search grounding)
		productHints := extractProductHints(officialDomains, canonicalName)
		if len(productHints) > maxProductHints {
			productHints = productHints[:maxProductHints]
		}
		for _, hint := range productHints {
			add("product_"+strings.ToLower(hint), fmt.Sprintf("%s official site", hint))
		}
	}

	if len(requestedRegions) > 0 {
		for _, r := range requestedRegions {
			switch r {
			case "cn":
				add("cn", buildCompanyDomainDiscoverySearchQuery(name, disambiguationTerms, "zh"))
			case "jp":
				add("jp", buildCompanyDomainDiscoverySearchQuery(name, disambiguationTerms, "ja"))
			case "kr":
				add("kr", buildCompanyDomainDiscoverySearchQuery(name, disambiguationTerms, "ko"))
			case "eu":
				add("eu", buildCompanyEUDomainDiscoverySearchQuery(name))
			case "us", "global":
				add("global", buildCompanyDomainDiscoverySearchQuery(name, disambiguationTerms, ""))
			default:
				add("global", buildCompanyDomainDiscoverySearchQuery(name, disambiguationTerms, ""))
			}
		}
		addTopicSearches()
		return out
	}

	// Default (multinational coverage): global + EU + CN + JP (+ KR when origin indicates Korea).
	add("global", buildCompanyDomainDiscoverySearchQuery(name, disambiguationTerms, ""))
	add("eu", buildCompanyEUDomainDiscoverySearchQuery(name))
	add("cn", buildCompanyDomainDiscoverySearchQuery(name, disambiguationTerms, "zh"))
	add("jp", buildCompanyDomainDiscoverySearchQuery(name, disambiguationTerms, "ja"))
	if originRegion == "kr" {
		add("kr", buildCompanyDomainDiscoverySearchQuery(name, disambiguationTerms, "ko"))
	}
	addTopicSearches()
	return out
}

// containsFinancialTopic checks if research areas include financial-related topics.
func containsFinancialTopic(areas []string) bool {
	financialKeywords := []string{
		"financial", "finance", "investor", "revenue", "earnings",
		"stock", "market cap", "quarterly", "annual report", "sec filing",
		"profit", "loss", "fiscal", "shareholder", "dividend",
	}
	for _, area := range areas {
		areaLower := strings.ToLower(area)
		for _, kw := range financialKeywords {
			if strings.Contains(areaLower, kw) {
				return true
			}
		}
	}
	return false
}

// containsTechnicalTopic checks if research areas include technical/documentation topics.
func containsTechnicalTopic(areas []string) bool {
	technicalKeywords := []string{
		"api", "sdk", "integration", "developer", "documentation",
		"technical", "architecture", "implementation", "code", "library",
		"programming", "endpoint", "webhook", "oauth",
	}
	for _, area := range areas {
		areaLower := strings.ToLower(area)
		for _, kw := range technicalKeywords {
			if strings.Contains(areaLower, kw) {
				return true
			}
		}
	}
	return false
}

// containsCultureTopic checks if research areas include culture/hiring topics.
func containsCultureTopic(areas []string) bool {
	cultureKeywords := []string{
		"culture", "hiring", "career", "job", "team", "employee",
		"workplace", "values", "diversity", "benefits", "talent",
		"recruitment", "work environment", "company values",
	}
	for _, area := range areas {
		areaLower := strings.ToLower(area)
		for _, kw := range cultureKeywords {
			if strings.Contains(areaLower, kw) {
				return true
			}
		}
	}
	return false
}

// classifyFocusCategories maps ResearchAreas to domain type hints.
// Returns categories like "corporate", "docs", "ir", "store" based on research area keywords.
// This is used for domain prioritization, not as the main focus (which should be input.Query).
func classifyFocusCategories(areas []string) []string {
	categoryMap := map[string][]string{
		"ir":        {"financial", "investor", "earnings", "revenue", "stock", "fiscal", "quarterly", "annual report", "SEC"},
		"docs":      {"api", "documentation", "technical", "developer", "sdk", "integration", "reference"},
		"store":     {"product", "pricing", "purchase", "buy", "shop", "store", "catalog", "features"},
		"corporate": {"organization", "leadership", "management", "executive", "board", "history", "mission", "about"},
		"careers":   {"career", "hiring", "job", "recruitment", "talent", "employee", "culture", "workplace"},
	}

	found := make(map[string]bool)
	for _, area := range areas {
		areaLower := strings.ToLower(area)
		for category, keywords := range categoryMap {
			for _, kw := range keywords {
				if strings.Contains(areaLower, kw) {
					found[category] = true
					break
				}
			}
		}
	}

	var result []string
	for cat := range found {
		result = append(result, cat)
	}
	return result
}

// classifyDomainRole determines the type of a domain based on URL patterns.
// Returns: corporate, docs, ir, store, careers, support, blog_news, or other.
// This is used to provide context to LLM for better target_keywords generation.
func classifyDomainRole(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "other"
	}
	host := strings.ToLower(u.Host)
	path := strings.ToLower(u.Path)

	// Check host-based patterns
	switch {
	case strings.Contains(host, "docs.") || strings.Contains(host, "developer.") ||
		strings.Contains(host, "api.") || strings.Contains(host, "dev."):
		return "docs"
	case strings.Contains(host, "ir.") || strings.Contains(host, "investor"):
		return "ir"
	case strings.Contains(host, "store.") || strings.Contains(host, "shop.") ||
		strings.Contains(host, "buy."):
		return "store"
	case strings.Contains(host, "careers.") || strings.Contains(host, "jobs.") ||
		strings.Contains(host, "talent."):
		return "careers"
	case strings.Contains(host, "support.") || strings.Contains(host, "help."):
		return "support"
	case strings.Contains(host, "blog.") || strings.Contains(host, "news."):
		return "blog_news"
	}

	// Check path-based patterns
	switch {
	case strings.HasPrefix(path, "/docs") || strings.HasPrefix(path, "/developer") ||
		strings.HasPrefix(path, "/api"):
		return "docs"
	case strings.HasPrefix(path, "/ir") || strings.HasPrefix(path, "/investor"):
		return "ir"
	case strings.HasPrefix(path, "/store") || strings.HasPrefix(path, "/shop"):
		return "store"
	case strings.HasPrefix(path, "/careers") || strings.HasPrefix(path, "/jobs"):
		return "careers"
	}

	return "corporate"
}

// DomainAnalysisIntent captures the unified task intent for Domain Analysis.
// This structure normalizes inputs from query, context, and refiner results.
type DomainAnalysisIntent struct {
	FocusText            string   // Original input.Query (primary focus)
	FocusCategories      []string // Derived from ResearchAreas (e.g., ir, docs, corporate)
	ExplicitRegions      []string // User-specified regions from context/query
	TargetLanguages      []string // From Refiner
	LocalizationNeeded   bool     // From Refiner
	MultinationalDefault bool     // Computed: should default to multi-region discovery
}

// multinationalKeywords are used to detect multi-region intent from query text.
var multinationalKeywords = []string{
	"全球", "跨国", "国际", "世界", "各地区", "多国", "海外",
	"worldwide", "global", "international", "multinational", "all regions",
	"across countries", "multi-region", "cross-border",
}

// BuildDomainAnalysisIntent constructs a DomainAnalysisIntent from task inputs.
func BuildDomainAnalysisIntent(
	query string,
	researchAreas []string,
	explicitRegions []string,
	targetLanguages []string,
	localizationNeeded bool,
) DomainAnalysisIntent {
	intent := DomainAnalysisIntent{
		FocusText:          query,
		FocusCategories:    classifyFocusCategories(researchAreas),
		ExplicitRegions:    explicitRegions,
		TargetLanguages:    targetLanguages,
		LocalizationNeeded: localizationNeeded,
	}
	intent.MultinationalDefault = intent.computeMultinational()
	return intent
}

// computeMultinational determines if the task should default to multi-region discovery.
// Returns true if:
// - User explicitly specified multiple regions
// - Refiner detected multiple target languages (len >= 2)
// - Refiner set localization_needed = true
// - Query contains multinational keywords
func (i *DomainAnalysisIntent) computeMultinational() bool {
	// Explicit multi-region
	if len(i.ExplicitRegions) > 1 {
		return true
	}

	// Multiple target languages
	if len(i.TargetLanguages) >= 2 {
		return true
	}

	// Refiner detected localization need
	if i.LocalizationNeeded {
		return true
	}

	// Query contains multinational keywords
	queryLower := strings.ToLower(i.FocusText)
	for _, kw := range multinationalKeywords {
		if strings.Contains(queryLower, kw) {
			return true
		}
	}

	return false
}

// ShouldIncludeRegion determines if a region should be included in discovery.
// Used by buildDomainDiscoverySearches to filter regions.
func (i *DomainAnalysisIntent) ShouldIncludeRegion(region string) bool {
	// If explicit regions specified, only include those
	if len(i.ExplicitRegions) > 0 {
		for _, r := range i.ExplicitRegions {
			if strings.EqualFold(r, region) {
				return true
			}
		}
		return false
	}

	// If multinational, include default set
	if i.MultinationalDefault {
		defaultRegions := []string{"global", "eu", "cn", "jp", "us"}
		for _, r := range defaultRegions {
			if strings.EqualFold(r, region) {
				return true
			}
		}
		return false
	}

	// Non-multinational: only global
	return strings.EqualFold(region, "global")
}

// extractProductHints extracts product names from refiner domains for search hints.
// Only returns domains that don't match the canonical company name.
func extractProductHints(officialDomains []string, canonicalName string) []string {
	canonical := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(canonicalName), " ", ""))
	seen := make(map[string]bool)
	var hints []string
	for _, d := range officialDomains {
		base := extractDomainBase(d)
		if base == "" || len(base) < minProductNameLength {
			continue
		}
		baseLower := strings.ToLower(base)
		// Skip if matches canonical name or already seen
		if strings.Contains(canonical, baseLower) || strings.Contains(baseLower, canonical) {
			continue
		}
		if seen[baseLower] {
			continue
		}
		seen[baseLower] = true
		hints = append(hints, base)
	}
	return hints
}

// extractDomainBase extracts the base name from a domain (youtube.com → youtube)
func extractDomainBase(domain string) string {
	host := normalizeDomainCandidateHost(domain)
	if host == "" {
		return ""
	}
	// Remove common TLDs
	commonTLDs := []string{".com", ".org", ".net", ".io", ".ai", ".co", ".app", ".dev", ".xyz"}
	for _, tld := range commonTLDs {
		if strings.HasSuffix(host, tld) {
			host = strings.TrimSuffix(host, tld)
			break
		}
	}
	// Get first part if subdomain (docs.stripe.com → docs after TLD removal)
	parts := strings.Split(host, ".")
	if len(parts) > 1 {
		// Return the last part (main domain name, not subdomain)
		return parts[len(parts)-1]
	}
	return host
}

func originRegionToDiscoveryLanguageCode(originRegion string) string {
	switch normalizePrefetchRegion(originRegion) {
	case "cn":
		return "zh"
	case "jp":
		return "ja"
	case "kr":
		return "ko"
	default:
		return ""
	}
}

func shouldRunGlobalDomainDiscoveryFallback(discovered []string) bool {
	if len(discovered) < 4 {
		return true
	}

	registrables := make(map[string]bool)
	hasSupport := false
	for _, d := range discovered {
		h := normalizeDomainCandidateHost(d)
		if h == "" {
			continue
		}
		if strings.HasPrefix(h, "help.") || strings.HasPrefix(h, "docs.") || strings.HasPrefix(h, "support.") {
			hasSupport = true
		}
		reg := registrableDomain(h)
		if reg != "" {
			registrables[reg] = true
		}
	}

	// If we only found one registrable domain, we're likely missing product/brand domains.
	if len(registrables) < 2 {
		return true
	}

	// Support/help sites are high-signal for company research; if missing and the set is small, try one more search.
	if !hasSupport && len(discovered) < 6 {
		return true
	}

	return false
}

// containsGenericTerm checks if a term contains any generic tech words that pollute search results.
// This fixes the bug where multi-word terms like "analytics platform" were not filtered
// because only exact matches were checked (e.g., "analytics platform" != "analytics").
func containsGenericTerm(term string, genericTerms map[string]bool) bool {
	termLower := strings.ToLower(term)
	// Split into words and check each word
	words := strings.Fields(termLower)
	for _, word := range words {
		if genericTerms[word] {
			return true
		}
	}
	return false
}

func buildCompanyDomainDiscoverySearchQuery(canonicalName string, disambiguationTerms []string, regionCode string) string {
	name := strings.TrimSpace(canonicalName)
	if name == "" {
		return ""
	}

	// For domain discovery, use ONLY the company name + "official website" keywords.
	// Do NOT add disambiguation terms - they often contain LLM-generated translations
	// or explanations that pollute search results and push official domains down.
	// Example: "Ptmind 官网 官方网站" returns jp.ptmind.com as #1 result,
	// but "Ptmind 官网 官方网站 web optimization..." pushes it out of top 10.
	q := fmt.Sprintf("%s official website", name)
	switch regionCode {
	case "zh":
		q = fmt.Sprintf("%s 官网 官方网站", name)
	case "ja":
		q = fmt.Sprintf("%s 公式サイト official website", name)
	case "ko":
		q = fmt.Sprintf("%s 공식 사이트 official website", name)
	}

	// NOTE: Removed disambiguation term addition for domain discovery.
	// The simple query "{company} official website" is more effective
	// at finding official domains than queries polluted with extra terms.

	return q
}

func stripCodeFences(s string) string {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "```json") {
		t = strings.TrimPrefix(t, "```json")
		t = strings.TrimPrefix(t, "```")
		if idx := strings.LastIndex(t, "```"); idx != -1 {
			t = t[:idx]
		}
		return strings.TrimSpace(t)
	}
	if strings.HasPrefix(t, "```") {
		t = strings.TrimPrefix(t, "```")
		if idx := strings.LastIndex(t, "```"); idx != -1 {
			t = t[:idx]
		}
		return strings.TrimSpace(t)
	}
	return t
}

// appendVerificationWarning appends a warning message to the report
// Used by P0-B to add verification warnings when V2 fails without falling back to V1
func appendVerificationWarning(report, warning string) string {
	if report == "" {
		return warning
	}
	// Append warning as a separate section at the end
	return report + "\n\n---\n\n" + warning
}

func registrableDomain(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.TrimPrefix(h, "www.")
	h = strings.TrimSuffix(h, ".")
	if h == "" {
		return ""
	}
	if strings.Contains(h, ":") {
		// Drop port if present.
		if hh, _, err := net.SplitHostPort(h); err == nil {
			h = hh
		} else {
			// Best-effort: split on last ':' for malformed host:port
			if i := strings.LastIndex(h, ":"); i > 0 {
				h = h[:i]
			}
		}
	}
	if h == "" {
		return ""
	}
	if ip := net.ParseIP(h); ip != nil {
		return ""
	}
	if h == "localhost" {
		return ""
	}

	labels := strings.Split(h, ".")
	if len(labels) < 2 {
		return ""
	}

	// Minimal multi-label public suffix handling for common company research TLDs.
	// This avoids collapsing e.g. sony.co.jp -> co.jp via naive last-2-labels logic.
	suffix2 := labels[len(labels)-2] + "." + labels[len(labels)-1]
	suffix3 := ""
	if len(labels) >= 3 {
		suffix3 = labels[len(labels)-3] + "." + suffix2
	}

	multiLabelSuffixes := map[string]struct{}{
		// Japan
		"co.jp": {}, "ne.jp": {}, "or.jp": {}, "ac.jp": {}, "go.jp": {}, "ed.jp": {}, "lg.jp": {},
		// China
		"com.cn": {}, "net.cn": {}, "org.cn": {}, "gov.cn": {}, "edu.cn": {},
		// Korea
		"co.kr": {}, "or.kr": {}, "go.kr": {}, "ac.kr": {}, "re.kr": {},
		// UK/AU (common in global companies)
		"co.uk": {}, "org.uk": {}, "ac.uk": {},
		"com.au": {}, "net.au": {}, "org.au": {}, "edu.au": {},
	}

	if _, ok := multiLabelSuffixes[suffix3]; ok && len(labels) >= 4 {
		return strings.Join(labels[len(labels)-4:], ".")
	}
	if _, ok := multiLabelSuffixes[suffix2]; ok && len(labels) >= 3 {
		return strings.Join(labels[len(labels)-3:], ".")
	}

	return suffix2
}

func domainsFromWebSearchToolExecutionsAll(toolExecs []activities.ToolExecution) []string {
	seen := make(map[string]bool)
	var out []string

	addURL := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		pu, err := url.Parse(raw)
		if err != nil {
			return
		}

		registrable := registrableDomain(pu.Host)
		if registrable == "" {
			return
		}

		// Exclude common aggregator/social/platform domains (check against registrable)
		disallowed := map[string]struct{}{
			// Social & aggregator
			"wikipedia.org": {}, "crunchbase.com": {}, "linkedin.com": {}, "facebook.com": {}, "x.com": {},
			"twitter.com": {}, "medium.com": {}, "youtube.com": {}, "youtu.be": {}, "instagram.com": {},
			"bloomberg.com": {}, "reuters.com": {}, "sec.gov": {}, "prtimes.jp": {},
			// Generic platforms that pollute results
			"google.com": {}, "salesforce.com": {}, "shopify.com": {}, "optimizely.com": {},
			"zoominfo.com": {}, "g2.com": {}, "capterra.com": {}, "trustpilot.com": {},
			// Domain registrars & info sites
			"register.domains": {}, "porkbun.com": {}, "nominus.com": {}, "squarespace.com": {},
			"github.io": {}, "github.com": {}, "githubusercontent.com": {},
			// Job boards
			"hiredchina.com": {}, "glassdoor.com": {}, "indeed.com": {}, "zhipin.com": {},
			// App stores
			"apps.shopify.com": {}, "chromewebstore.google.com": {}, "play.google.com": {},
			// Investor info sites
			"tracxn.com": {}, "trjcn.com": {},
		}
		if _, ok := disallowed[registrable]; ok {
			return
		}

		fullHost := strings.ToLower(strings.TrimSpace(pu.Host))
		fullHost = strings.TrimPrefix(fullHost, "www.")
		if fullHost == "" {
			return
		}

		if !seen[fullHost] {
			seen[fullHost] = true
			out = append(out, fullHost)
		}
	}

	for _, te := range toolExecs {
		if te.Tool != "web_search" || !te.Success || te.Output == nil {
			continue
		}

		switch v := te.Output.(type) {
		case map[string]interface{}:
			if rawResults, ok := v["results"].([]interface{}); ok {
				for _, rr := range rawResults {
					if m, ok2 := rr.(map[string]interface{}); ok2 {
						if u, ok3 := m["url"].(string); ok3 {
							addURL(u)
						}
					}
				}
			}
		case []interface{}:
			for _, rr := range v {
				if m, ok2 := rr.(map[string]interface{}); ok2 {
					if u, ok3 := m["url"].(string); ok3 {
						addURL(u)
					}
				}
			}
		default:
			if s, ok := te.Output.(string); ok {
				addURL(s)
			}
		}
	}

	return out
}

func domainsFromWebSearchToolExecutionsAllV2(toolExecs []activities.ToolExecution, canonicalName string) []string {
	canonicalLower := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(canonicalName), " ", ""))

	seen := make(map[string]bool)
	var out []string

	allowDisallowed := func(fullHost, registrable string) bool {
		if canonicalLower == "" {
			return false
		}
		// For short canonical names (e.g., "X"), only allow if it matches a full label.
		labels := strings.Split(fullHost, ".")
		if len(labels) > 0 && labels[0] == canonicalLower {
			return true
		}
		// For normal names, allow substring match in host/registrable.
		if len(canonicalLower) >= 3 && (strings.Contains(fullHost, canonicalLower) || strings.Contains(registrable, canonicalLower)) {
			return true
		}
		return false
	}

	addURL := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		pu, err := url.Parse(raw)
		if err != nil {
			return
		}

		fullHost := strings.ToLower(strings.TrimSpace(pu.Host))
		fullHost = strings.TrimPrefix(fullHost, "www.")
		fullHost = strings.TrimSuffix(fullHost, ".")
		if fullHost == "" {
			return
		}

		registrable := registrableDomain(fullHost)
		if registrable == "" {
			return
		}

		// Exclude common directory/social/news domains unless they match the canonical name.
		disallowed := map[string]struct{}{
			"wikipedia.org": {}, "crunchbase.com": {}, "linkedin.com": {}, "facebook.com": {}, "x.com": {},
			"twitter.com": {}, "medium.com": {}, "youtube.com": {}, "youtu.be": {}, "instagram.com": {},
			"bloomberg.com": {}, "reuters.com": {}, "sec.gov": {}, "prtimes.jp": {},
			"zoominfo.com": {}, "g2.com": {}, "capterra.com": {}, "trustpilot.com": {},
		}
		if _, ok := disallowed[registrable]; ok && !allowDisallowed(fullHost, registrable) {
			return
		}

		if !seen[fullHost] {
			seen[fullHost] = true
			out = append(out, fullHost)
		}
	}

	for _, te := range toolExecs {
		if te.Tool != "web_search" || !te.Success || te.Output == nil {
			continue
		}

		switch v := te.Output.(type) {
		case map[string]interface{}:
			if rawResults, ok := v["results"].([]interface{}); ok {
				for _, rr := range rawResults {
					if m, ok2 := rr.(map[string]interface{}); ok2 {
						if u, ok3 := m["url"].(string); ok3 {
							addURL(u)
						}
					}
				}
			}
		case []interface{}:
			for _, rr := range v {
				if m, ok2 := rr.(map[string]interface{}); ok2 {
					if u, ok3 := m["url"].(string); ok3 {
						addURL(u)
					}
				}
			}
		default:
			if s, ok := te.Output.(string); ok {
				addURL(s)
			}
		}
	}

	return out
}

func domainBucketForHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.TrimPrefix(h, "www.")
	h = strings.TrimSuffix(h, ".")

	// Japan
	if strings.HasPrefix(h, "jp.") || strings.Contains(h, ".jp.") || strings.HasSuffix(h, ".jp") || strings.HasSuffix(h, ".co.jp") || strings.HasSuffix(h, ".ne.jp") || strings.HasSuffix(h, ".or.jp") {
		return "jp"
	}

	// China
	if strings.HasPrefix(h, "cn.") || strings.Contains(h, ".cn.") || strings.HasSuffix(h, ".cn") || strings.HasSuffix(h, ".com.cn") {
		return "cn"
	}

	// Korea
	if strings.HasPrefix(h, "kr.") || strings.Contains(h, ".kr.") || strings.HasSuffix(h, ".kr") || strings.HasSuffix(h, ".co.kr") {
		return "kr"
	}

	// Europe (heuristics): EU TLD + common European ccTLDs
	if strings.HasPrefix(h, "eu.") || strings.Contains(h, ".eu.") || strings.HasSuffix(h, ".eu") ||
		strings.HasSuffix(h, ".co.uk") || strings.HasSuffix(h, ".org.uk") || strings.HasSuffix(h, ".ac.uk") ||
		strings.HasSuffix(h, ".de") || strings.HasSuffix(h, ".fr") || strings.HasSuffix(h, ".it") || strings.HasSuffix(h, ".es") ||
		strings.HasSuffix(h, ".nl") || strings.HasSuffix(h, ".se") || strings.HasSuffix(h, ".fi") || strings.HasSuffix(h, ".dk") {
		return "eu"
	}

	return "global"
}

func selectDomainsForPrefetch(discovered []string, requestedRegions []string, originRegion string, max int) []string {
	if max <= 0 || len(discovered) == 0 {
		return nil
	}

	// Deduplicate while preserving order
	seen := make(map[string]bool)
	var uniq []string
	for _, d := range discovered {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		uniq = append(uniq, d)
	}

	// User-scoped: only include requested region buckets.
	if len(requestedRegions) > 0 {
		allowed := make(map[string]bool)
		for _, r := range requestedRegions {
			rr := normalizePrefetchRegion(r)
			if rr == "" {
				continue
			}
			// Map US/global requests to the "global" bucket to match domainBucketForHost().
			if rr == "us" || rr == "global" {
				allowed["global"] = true
				continue
			}
			allowed[rr] = true
		}
		if len(allowed) == 0 {
			return nil
		}
		var filtered []string
		for _, d := range uniq {
			if allowed[domainBucketForHost(d)] {
				filtered = append(filtered, d)
			}
		}
		if len(filtered) == 0 {
			return nil
		}
		if len(filtered) > max {
			return filtered[:max]
		}
		return filtered
	}

	// Default: ensure coverage across origin/us/global/eu/cn/jp when available.
	required := []string{}
	addReq := func(r string) {
		r = normalizePrefetchRegion(r)
		if r == "" {
			return
		}
		for _, existing := range required {
			if existing == r {
				return
			}
		}
		required = append(required, r)
	}

	addReq(originRegion)
	addReq("us") // Treat global .com as US/global coverage
	addReq("eu")
	addReq("cn")
	addReq("jp")

	picked := make(map[string]bool)
	var selected []string

	// Pick one per required bucket first
	for _, bucket := range required {
		matchBucket := bucket
		if bucket == "us" {
			matchBucket = "global"
		}
		for _, d := range uniq {
			if picked[d] {
				continue
			}
			if domainBucketForHost(d) == matchBucket {
				picked[d] = true
				selected = append(selected, d)
				break
			}
		}
		if len(selected) >= max {
			return selected[:max]
		}
	}

	// Fill remaining slots in discovered order
	for _, d := range uniq {
		if picked[d] {
			continue
		}
		picked[d] = true
		selected = append(selected, d)
		if len(selected) >= max {
			break
		}
	}

	if len(selected) > max {
		return selected[:max]
	}
	return selected
}

// selectDomainsForPrefetchWithFocus extends selectDomainsForPrefetch with focus-aware scoring.
// focusCategories are domain types to prioritize (e.g., "ir", "docs", "corporate").
func selectDomainsForPrefetchWithFocus(discovered []string, requestedRegions []string, originRegion string, max int, focusCategories []string) []string {
	if max <= 0 || len(discovered) == 0 {
		return nil
	}

	// Deduplicate while preserving order
	seen := make(map[string]bool)
	var uniq []string
	for _, d := range discovered {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		uniq = append(uniq, d)
	}

	// Score each domain based on focus categories
	type scoredDomain struct {
		Domain string
		Score  int
		Bucket string
	}
	scored := make([]scoredDomain, len(uniq))
	for i, d := range uniq {
		role := classifyDomainRole("https://" + d)
		score := computeDomainFocusScore(role, focusCategories)
		scored[i] = scoredDomain{
			Domain: d,
			Score:  score,
			Bucket: domainBucketForHost(d),
		}
	}

	// User-scoped: only include requested region buckets.
	if len(requestedRegions) > 0 {
		allowed := make(map[string]bool)
		for _, r := range requestedRegions {
			rr := normalizePrefetchRegion(r)
			if rr == "" {
				continue
			}
			if rr == "us" || rr == "global" {
				allowed["global"] = true
				continue
			}
			allowed[rr] = true
		}
		if len(allowed) == 0 {
			return nil
		}

		// Filter and sort by score
		var filtered []scoredDomain
		for _, sd := range scored {
			if allowed[sd.Bucket] {
				filtered = append(filtered, sd)
			}
		}
		if len(filtered) == 0 {
			return nil
		}

		// Sort by score descending, preserve order for equal scores
		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].Score > filtered[j].Score
		})

		result := make([]string, 0, max)
		for i := 0; i < len(filtered) && i < max; i++ {
			result = append(result, filtered[i].Domain)
		}
		return result
	}

	// Default: ensure coverage across origin/us/global/eu/cn/jp when available.
	// But sort within each bucket by focus score.
	required := []string{}
	addReq := func(r string) {
		r = normalizePrefetchRegion(r)
		if r == "" {
			return
		}
		for _, existing := range required {
			if existing == r {
				return
			}
		}
		required = append(required, r)
	}

	addReq(originRegion)
	addReq("us")
	addReq("eu")
	addReq("cn")
	addReq("jp")

	// Group by bucket
	bucketDomains := make(map[string][]scoredDomain)
	for _, sd := range scored {
		bucket := sd.Bucket
		bucketDomains[bucket] = append(bucketDomains[bucket], sd)
	}

	// Sort each bucket by score descending
	for bucket := range bucketDomains {
		sort.SliceStable(bucketDomains[bucket], func(i, j int) bool {
			return bucketDomains[bucket][i].Score > bucketDomains[bucket][j].Score
		})
	}

	picked := make(map[string]bool)
	var selected []string

	// Pick best from each required bucket first
	for _, bucket := range required {
		matchBucket := bucket
		if bucket == "us" {
			matchBucket = "global"
		}
		domains := bucketDomains[matchBucket]
		for _, sd := range domains {
			if picked[sd.Domain] {
				continue
			}
			picked[sd.Domain] = true
			selected = append(selected, sd.Domain)
			break
		}
		if len(selected) >= max {
			return selected[:max]
		}
	}

	// Fill remaining slots: sort all remaining by score descending
	var remaining []scoredDomain
	for _, sd := range scored {
		if !picked[sd.Domain] {
			remaining = append(remaining, sd)
		}
	}
	sort.SliceStable(remaining, func(i, j int) bool {
		return remaining[i].Score > remaining[j].Score
	})

	for _, sd := range remaining {
		picked[sd.Domain] = true
		selected = append(selected, sd.Domain)
		if len(selected) >= max {
			break
		}
	}

	if len(selected) > max {
		return selected[:max]
	}
	return selected
}

// computeDomainFocusScore calculates how well a domain role matches focus categories.
func computeDomainFocusScore(role string, focusCategories []string) int {
	if len(focusCategories) == 0 {
		return 0
	}

	score := 0
	for _, cat := range focusCategories {
		switch {
		// Product focus: docs and store are relevant
		case (cat == "store" || cat == "product") && (role == "store" || role == "docs"):
			score += 10
		// Financial focus: ir is highly relevant
		case (cat == "ir" || cat == "financial") && role == "ir":
			score += 15
		// Organizational focus: corporate is relevant
		case (cat == "corporate" || cat == "organization") && role == "corporate":
			score += 10
		// Technical focus: docs is highly relevant
		case (cat == "docs" || cat == "technical") && role == "docs":
			score += 15
		// Careers focus
		case cat == "careers" && role == "careers":
			score += 10
		// Exact match
		case cat == role:
			score += 10
		}
	}
	return score
}

func normalizeDomainCandidateHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	pu, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	h := strings.ToLower(strings.TrimSpace(pu.Host))
	h = strings.TrimPrefix(h, "www.")
	h = strings.TrimSuffix(h, ".")
	if h == "" {
		return ""
	}
	// Drop port if present.
	if strings.Contains(h, ":") {
		if hh, _, err := net.SplitHostPort(h); err == nil {
			h = hh
		} else if i := strings.LastIndex(h, ":"); i > 0 {
			h = h[:i]
		}
	}
	if h == "" {
		return ""
	}
	if ip := net.ParseIP(h); ip != nil {
		return ""
	}
	if h == "localhost" {
		return ""
	}
	return h
}

func tldLabel(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.TrimPrefix(h, "www.")
	h = strings.TrimSuffix(h, ".")
	if h == "" {
		return ""
	}
	parts := strings.Split(h, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-1]
}

func vettedHybridRefinementDomains(discovered []string, refinement []string, canonicalName string) []string {
	canonicalLower := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(canonicalName), " ", ""))

	discoveredRegistrables := make(map[string]bool)
	discoveredTLDs := make(map[string]bool)
	for _, d := range discovered {
		h := normalizeDomainCandidateHost(d)
		if h == "" {
			continue
		}
		reg := registrableDomain(h)
		if reg == "" {
			continue
		}
		discoveredRegistrables[reg] = true
		tld := tldLabel(reg)
		if tld != "" {
			discoveredTLDs[tld] = true
		}
	}

	seen := make(map[string]bool)
	var out []string
	for _, r := range refinement {
		h := normalizeDomainCandidateHost(r)
		if h == "" || seen[h] {
			continue
		}
		reg := registrableDomain(h)
		if reg == "" {
			continue
		}

		// (1) Anchored by registrable domain already observed in discovery.
		if discoveredRegistrables[reg] {
			seen[h] = true
			out = append(out, h)
			continue
		}

		// (2) Brand-TLD expansion: allow second-level domains under a brand TLD that matches the canonical name.
		// Example: about.google discovered -> allow blog.google when canonical is "Google".
		tld := tldLabel(h)
		if canonicalLower != "" && tld != "" && tld == canonicalLower && discoveredTLDs[tld] {
			seen[h] = true
			out = append(out, h)
			continue
		}
	}

	return out
}

func hostHasLabel(host, label string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	label = strings.ToLower(strings.TrimSpace(label))
	if h == "" || label == "" {
		return false
	}
	return strings.HasPrefix(h, label+".") || strings.Contains(h, "."+label+".")
}

func domainPrefetchScore(host, canonicalName string) int {
	h := normalizeDomainCandidateHost(host)
	if h == "" {
		return -100000
	}
	score := 0

	reg := registrableDomain(h)
	if reg != "" && reg == h {
		score += 100
	} else {
		score += 80
	}

	canonicalLower := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(canonicalName), " ", ""))
	if canonicalLower != "" && strings.Contains(h, canonicalLower) {
		score += 10
	}

	switch {
	case hostHasLabel(h, "about") || hostHasLabel(h, "company"):
		score += 35
	case hostHasLabel(h, "investor") || hostHasLabel(h, "investors") || hostHasLabel(h, "ir"):
		score += 35
	}
	if hostHasLabel(h, "leadership") || hostHasLabel(h, "management") {
		score += 20
	}
	if hostHasLabel(h, "press") || hostHasLabel(h, "newsroom") {
		score += 15
	}
	if hostHasLabel(h, "blog") {
		score += 20
	}
	if hostHasLabel(h, "cloud") {
		score += 15
	}
	if hostHasLabel(h, "store") {
		score += 10
	}

	// Down-rank functional endpoints that rarely help company overview.
	if hostHasLabel(h, "support") || hostHasLabel(h, "accounts") {
		score -= 80
	}
	if hostHasLabel(h, "login") || hostHasLabel(h, "signin") {
		score -= 60
	}
	if hostHasLabel(h, "careers") || hostHasLabel(h, "jobs") {
		score -= 30
	}

	// Prefer shorter hosts slightly (fewer labels).
	labels := strings.Split(h, ".")
	score -= len(labels) * 2

	return score
}

func sortDomainsForHybridPrefetch(domains []string, canonicalName string) []string {
	seen := make(map[string]bool)
	var uniq []string
	for _, d := range domains {
		h := normalizeDomainCandidateHost(d)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		uniq = append(uniq, h)
	}

	scores := make(map[string]int, len(uniq))
	for _, d := range uniq {
		scores[d] = domainPrefetchScore(d, canonicalName)
	}

	sort.SliceStable(uniq, func(i, j int) bool {
		si := scores[uniq[i]]
		sj := scores[uniq[j]]
		if si != sj {
			return si > sj
		}
		return uniq[i] < uniq[j]
	})

	return uniq
}

func buildPrefetchURLsFromDomains(domains []string) []string {
	seen := make(map[string]bool)
	var urls []string
	for _, d := range domains {
		raw := strings.TrimSpace(d)
		if raw == "" {
			continue
		}
		raw = strings.TrimPrefix(raw, "https://")
		raw = strings.TrimPrefix(raw, "http://")
		raw = strings.TrimPrefix(raw, "www.")
		if raw == "" {
			continue
		}
		u := "https://" + raw
		if !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
	}
	return urls
}

func domainsFromWebSearchToolExecutions(toolExecs []activities.ToolExecution, canonicalName string) []string {
	seen := make(map[string]bool)
	var out []string

	// Normalize canonical name for matching
	canonicalLower := strings.ToLower(strings.ReplaceAll(canonicalName, " ", ""))

	addURL := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		pu, err := url.Parse(raw)
		if err != nil {
			return
		}

		// Get registrable domain for filtering (e.g., ptmind.com from jp.ptmind.com)
		registrable := registrableDomain(pu.Host)
		if registrable == "" {
			return
		}

		// Exclude common aggregator/social/platform domains (check against registrable)
		disallowed := map[string]struct{}{
			// Social & aggregator
			"wikipedia.org": {}, "crunchbase.com": {}, "linkedin.com": {}, "facebook.com": {}, "x.com": {},
			"twitter.com": {}, "medium.com": {}, "youtube.com": {}, "youtu.be": {}, "instagram.com": {},
			"bloomberg.com": {}, "reuters.com": {}, "sec.gov": {}, "prtimes.jp": {},
			// Generic platforms that pollute results
			"google.com": {}, "salesforce.com": {}, "shopify.com": {}, "optimizely.com": {},
			"zoominfo.com": {}, "g2.com": {}, "capterra.com": {}, "trustpilot.com": {},
			// Domain registrars & info sites
			"register.domains": {}, "porkbun.com": {}, "nominus.com": {}, "squarespace.com": {},
			"github.io": {}, "github.com": {}, "githubusercontent.com": {},
			// Job boards
			"hiredchina.com": {}, "glassdoor.com": {}, "indeed.com": {}, "zhipin.com": {},
			// App stores
			"apps.shopify.com": {}, "chromewebstore.google.com": {}, "play.google.com": {},
			// Investor info sites
			"tracxn.com": {}, "trjcn.com": {},
		}
		if _, ok := disallowed[registrable]; ok {
			return
		}

		// Preserve full host for company research (jp.ptmind.com, cn.ptmind.com are different sites)
		// Only strip www. prefix
		fullHost := strings.ToLower(strings.TrimSpace(pu.Host))
		fullHost = strings.TrimPrefix(fullHost, "www.")
		if fullHost == "" {
			return
		}

		// Relevance check: domain should be related to canonical name
		// This prevents profitmind.com from being included when searching for PTmind
		hostLower := strings.ToLower(registrable)
		hostBase := strings.TrimSuffix(strings.TrimSuffix(hostLower, ".com"), ".co")
		hostBase = strings.TrimSuffix(hostBase, ".jp")
		hostBase = strings.TrimSuffix(hostBase, ".cn")

		isRelevant := false
		// Check if host contains canonical name (ptengine contains ptengine)
		if strings.Contains(hostLower, canonicalLower) {
			isRelevant = true
		}
		// Check if canonical name contains host base (ptmind contains ptmind from ptmind.com)
		if strings.Contains(canonicalLower, hostBase) && len(hostBase) >= 3 {
			isRelevant = true
		}
		// Allow if no canonical name provided (fallback)
		if canonicalLower == "" {
			isRelevant = true
		}

		if !isRelevant {
			return
		}

		// Add full host (preserving subdomains like jp.ptmind.com)
		if !seen[fullHost] {
			seen[fullHost] = true
			out = append(out, fullHost)
		}
	}

	for _, te := range toolExecs {
		if te.Tool != "web_search" || !te.Success || te.Output == nil {
			continue
		}

		switch v := te.Output.(type) {
		case map[string]interface{}:
			// Common shape: {results:[{url:...}, ...]}
			if rawResults, ok := v["results"].([]interface{}); ok {
				for _, rr := range rawResults {
					if m, ok2 := rr.(map[string]interface{}); ok2 {
						if u, ok3 := m["url"].(string); ok3 {
							addURL(u)
						}
					}
				}
			}
		case []interface{}:
			// Alternate shape: [{url:...}, ...]
			for _, rr := range v {
				if m, ok2 := rr.(map[string]interface{}); ok2 {
					if u, ok3 := m["url"].(string); ok3 {
						addURL(u)
					}
				}
			}
		default:
			// Fallback: best-effort string parse
			if s, ok := te.Output.(string); ok {
				addURL(s)
			}
		}
	}

	return out
}

func domainsFromDiscoveryResponse(resp string) []string {
	type domainDiscoveryResponse struct {
		Domains []string `json:"domains"`
	}

	var parsed domainDiscoveryResponse
	if err := json.Unmarshal([]byte(stripCodeFences(resp)), &parsed); err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var out []string
	for _, d := range parsed.Domains {
		host := registrableDomain(d)
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, host)
	}
	return out
}

func extractFirstJSONObjectContainingDomains(resp string) string {
	idx := strings.Index(resp, "\"domains\"")
	if idx == -1 {
		return ""
	}

	start := strings.LastIndex(resp[:idx], "{")
	if start == -1 {
		return ""
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(resp); i++ {
		ch := resp[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return resp[start : i+1]
			}
		}
	}

	return ""
}

func normalizeDiscoveryDomainCandidate(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	// Best-effort: accept both domains and URLs.
	if strings.Contains(s, "://") {
		if pu, err := url.Parse(s); err == nil {
			s = pu.Hostname()
		}
	} else {
		// Remove obvious schemes/prefixes without needing url.Parse.
		s = strings.TrimPrefix(s, "https://")
		s = strings.TrimPrefix(s, "http://")
		if idx := strings.IndexAny(s, "/?#"); idx != -1 {
			s = s[:idx]
		}
	}

	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "www.")
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return ""
	}
	if strings.Contains(s, ":") {
		if hh, _, err := net.SplitHostPort(s); err == nil {
			s = hh
		} else {
			if i := strings.LastIndex(s, ":"); i > 0 {
				s = s[:i]
			}
		}
	}
	if s == "" {
		return ""
	}
	if ip := net.ParseIP(s); ip != nil {
		return ""
	}
	if s == "localhost" {
		return ""
	}
	if strings.Count(s, ".") < 1 {
		return ""
	}

	// Hard block common non-official domains.
	disallowedRegistrables := map[string]struct{}{
		"wikipedia.org": {}, "crunchbase.com": {}, "linkedin.com": {}, "facebook.com": {}, "x.com": {},
		"twitter.com": {}, "medium.com": {}, "youtube.com": {}, "youtu.be": {}, "instagram.com": {},
		"bloomberg.com": {}, "reuters.com": {}, "sec.gov": {}, "prtimes.jp": {},
		"zoominfo.com": {}, "g2.com": {}, "capterra.com": {}, "trustpilot.com": {},
		"github.com": {}, "github.io": {}, "githubusercontent.com": {},
	}
	if reg := registrableDomain(s); reg != "" {
		if _, ok := disallowedRegistrables[reg]; ok {
			return ""
		}
	}

	return s
}

func domainsFromDiscoveryResponseV2(resp string) []string {
	type domainDiscoveryResponse struct {
		Domains []string `json:"domains"`
	}

	// 1) Fast path: JSON-only response (or codefenced JSON).
	var parsed domainDiscoveryResponse
	if err := json.Unmarshal([]byte(stripCodeFences(resp)), &parsed); err != nil {
		// 2) Common case: response contains sections + a JSON block.
		obj := extractFirstJSONObjectContainingDomains(resp)
		if obj == "" {
			// 3) Also try inside the first code-fenced block, then extract.
			obj = extractFirstJSONObjectContainingDomains(stripCodeFences(resp))
		}
		if obj == "" {
			return nil
		}
		if err2 := json.Unmarshal([]byte(obj), &parsed); err2 != nil {
			return nil
		}
	}

	seen := make(map[string]bool)
	var out []string
	for _, d := range parsed.Domains {
		host := normalizeDiscoveryDomainCandidate(d)
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, host)
	}
	return out
}

// buildCompanyPrefetchURLsWithLocale constructs URLs including locale-specific sources
func buildCompanyPrefetchURLsWithLocale(canonicalName string, officialDomains []string, detectedLanguage string) []string {
	seen := make(map[string]bool)
	var urls []string

	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		// Strip scheme and common prefixes for domain-only inputs
		if !strings.Contains(raw, "/") || strings.HasPrefix(raw, "http") {
			raw = strings.TrimPrefix(raw, "https://")
			raw = strings.TrimPrefix(raw, "http://")
			raw = strings.TrimPrefix(raw, "www.")
		}
		// For domain-only inputs, cut at first path/query/fragment separator
		if !strings.Contains(raw, "/") {
			if idx := strings.IndexAny(raw, "/?#"); idx != -1 {
				raw = raw[:idx]
			}
		}
		if raw == "" {
			return
		}
		url := raw
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			url = "https://" + raw
		}
		if !seen[url] {
			seen[url] = true
			urls = append(urls, url)
		}
	}

	// 1) Official domains (highest priority)
	for _, d := range officialDomains {
		add(d)
	}

	// 2) Config-based aggregator sources (Crunchbase, LinkedIn, etc.)
	if canonicalName != "" {
		// Always include global company sources
		configURLs := config.GetPrefetchURLs(canonicalName, "company")
		for _, u := range configURLs {
			add(u)
		}

		// Add locale-specific sources based on detected language
		if detectedLanguage == "ja" {
			jaURLs := config.GetPrefetchURLs(canonicalName, "company_ja")
			for _, u := range jaURLs {
				add(u)
			}
		} else if detectedLanguage == "zh" {
			zhURLs := config.GetPrefetchURLs(canonicalName, "company_zh")
			for _, u := range zhURLs {
				add(u)
			}
		}
	}

	// 3) Simple fallback: derive from canonical name when no official domains are present
	if len(officialDomains) == 0 && canonicalName != "" {
		name := strings.ToLower(strings.TrimSpace(canonicalName))
		if name != "" {
			var b strings.Builder
			for _, r := range name {
				if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
					b.WriteRune(r)
				}
			}
			host := b.String()
			if host != "" {
				add(host + ".com")
			}
		}
	}

	return urls
}

// ResearchWorkflow demonstrates composed patterns for complex research tasks.
// It combines React loops, parallel research, and reflection patterns.
func ResearchWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting ResearchWorkflow with composed patterns",
		"query", input.Query,
		"session_id", input.SessionID,
		"version", "v2",
	)

	// Configure activity options
	// Increased timeout from 5min to 8min for SynthesizeResultsLLM which can take 5+ minutes
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 8 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Set up workflow ID and emit context for event streaming
	workflowID := input.ParentWorkflowID
	if workflowID == "" {
		workflowID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	// Initialize control signal handler for pause/resume/cancel
	// Skip SSE emissions when running as child workflow (parent already emits)
	controlHandler := &control.SignalHandler{
		WorkflowID:  workflowID,
		AgentID:     "research",
		Logger:      logger,
		EmitCtx:     emitCtx,
		SkipSSEEmit: input.ParentWorkflowID != "",
	}
	controlHandler.Setup(ctx)

	// Prepare base context: start from SessionCtx, then overlay request Context
	// Per-request context must take precedence over persisted session defaults
	baseContext := make(map[string]interface{})
	for k, v := range input.SessionCtx {
		baseContext[k] = v
	}
	for k, v := range input.Context {
		baseContext[k] = v
	}
	if input.ParentWorkflowID != "" {
		baseContext["parent_workflow_id"] = input.ParentWorkflowID
	}

	// Memory retrieval with gate precedence (hierarchical > simple session)
	hierarchicalVersion := workflow.GetVersion(ctx, "memory_retrieval_v1", workflow.DefaultVersion, 1)
	sessionVersion := workflow.GetVersion(ctx, "session_memory_v1", workflow.DefaultVersion, 1)

	if hierarchicalVersion >= 1 && input.SessionID != "" {
		// Use hierarchical memory (combines recent + semantic)
		var hierMemory activities.FetchHierarchicalMemoryResult
		_ = workflow.ExecuteActivity(ctx, activities.FetchHierarchicalMemory,
			activities.FetchHierarchicalMemoryInput{
				Query:        input.Query,
				SessionID:    input.SessionID,
				TenantID:     input.TenantID,
				RecentTopK:   5,    // Fixed for determinism
				SemanticTopK: 5,    // Fixed for determinism
				Threshold:    0.75, // Fixed semantic threshold
			}).Get(ctx, &hierMemory)

		if len(hierMemory.Items) > 0 {
			baseContext["agent_memory"] = hierMemory.Items
			logger.Info("Injected hierarchical memory into research context",
				"session_id", input.SessionID,
				"memory_items", len(hierMemory.Items),
				"sources", hierMemory.Sources,
			)
		}
	} else if sessionVersion >= 1 && input.SessionID != "" {
		// Fallback to simple session memory if hierarchical not enabled
		var sessionMemory activities.FetchSessionMemoryResult
		_ = workflow.ExecuteActivity(ctx, activities.FetchSessionMemory,
			activities.FetchSessionMemoryInput{
				SessionID: input.SessionID,
				TenantID:  input.TenantID,
				TopK:      20, // Fixed for determinism
			}).Get(ctx, &sessionMemory)

		if len(sessionMemory.Items) > 0 {
			baseContext["agent_memory"] = sessionMemory.Items
			logger.Info("Injected session memory into research context",
				"session_id", input.SessionID,
				"memory_items", len(sessionMemory.Items),
			)
		}
	}

	// Context compression (version-gated for determinism)
	compressionVersion := workflow.GetVersion(ctx, "context_compress_v1", workflow.DefaultVersion, 1)
	if compressionVersion >= 1 && input.SessionID != "" && len(input.History) > 20 {
		// Check if compression is needed with rate limiting
		estimatedTokens := activities.EstimateTokens(convertHistoryForAgent(input.History))
		modelTier := determineModelTier(baseContext, "medium")

		var checkResult activities.CheckCompressionNeededResult
		err := workflow.ExecuteActivity(ctx, "CheckCompressionNeeded",
			activities.CheckCompressionNeededInput{
				SessionID:       input.SessionID,
				MessageCount:    len(input.History),
				EstimatedTokens: estimatedTokens,
				ModelTier:       modelTier,
			}).Get(ctx, &checkResult)

		if err == nil && checkResult.ShouldCompress {
			logger.Info("Triggering context compression in research workflow",
				"session_id", input.SessionID,
				"reason", checkResult.Reason,
				"message_count", len(input.History),
			)

			// Compress context via activity
			var compressResult activities.CompressContextResult
			err = workflow.ExecuteActivity(ctx, activities.CompressAndStoreContext,
				activities.CompressContextInput{
					SessionID:        input.SessionID,
					History:          convertHistoryMapForCompression(input.History),
					TargetTokens:     int(float64(activities.GetModelWindowSize(modelTier)) * 0.375),
					ParentWorkflowID: input.ParentWorkflowID,
				}).Get(ctx, &compressResult)

			if err == nil && compressResult.Summary != "" && compressResult.Stored {
				logger.Info("Context compressed and stored",
					"session_id", input.SessionID,
					"summary_length", len(compressResult.Summary),
				)

				// Update compression state in session
				var updateResult activities.UpdateCompressionStateResult
				_ = workflow.ExecuteActivity(ctx, "UpdateCompressionStateActivity",
					activities.UpdateCompressionStateInput{
						SessionID:    input.SessionID,
						MessageCount: len(input.History),
					}).Get(ctx, &updateResult)
			}
		}
	}

	// Step 0: Refine/expand vague research queries
	// Emit refinement start event
	emitTaskUpdate(ctx, input, activities.StreamEventAgentThinking, "research-refiner", "Refining research query")

	// Check pause/cancel before research execution
	if err := controlHandler.CheckPausePoint(ctx, "pre_execution"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Check pause/cancel before query refinement
	if err := controlHandler.CheckPausePoint(ctx, "pre_query_refinement"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	var totalTokens int
	var refineResult activities.RefineResearchQueryResult
	refinedQuery := input.Query // Default to original query
	err := workflow.ExecuteActivity(ctx, constants.RefineResearchQueryActivity,
		activities.RefineResearchQueryInput{
			Query:   input.Query,
			Context: baseContext,
		}).Get(ctx, &refineResult)

	if err == nil && refineResult.RefinedQuery != "" {
		logger.Info("Query refined for research",
			"original", input.Query,
			"refined", refineResult.RefinedQuery,
			"areas", refineResult.ResearchAreas,
			"tokens_used", refineResult.TokensUsed,
		)
		refinedQuery = refineResult.RefinedQuery

		// Deep Research 2.0: Fallback - extract dimension names when research_areas is empty/generic
		// LLM may return structured research_dimensions but leave research_areas sparse
		if len(refineResult.ResearchAreas) == 0 && len(refineResult.ResearchDimensions) > 0 {
			for _, dim := range refineResult.ResearchDimensions {
				refineResult.ResearchAreas = append(refineResult.ResearchAreas, dim.Dimension)
			}
			logger.Info("Used research_dimensions fallback for research_areas",
				"dimension_count", len(refineResult.ResearchDimensions),
			)
		}
		baseContext["research_areas"] = refineResult.ResearchAreas
		baseContext["original_query"] = input.Query
		baseContext["refinement_rationale"] = refineResult.Rationale
		baseContext["refined_query"] = refinedQuery
		if refineResult.DetectedLanguage != "" {
			// Pass target_language early; synthesis embeds language instruction.
			// Post-synthesis language validation/retry has been removed.
			baseContext["target_language"] = refineResult.DetectedLanguage
		}
		if refineResult.CanonicalName != "" {
			baseContext["canonical_name"] = refineResult.CanonicalName
		}
		if len(refineResult.ExactQueries) > 0 {
			baseContext["exact_queries"] = refineResult.ExactQueries
		}
		if len(refineResult.OfficialDomains) > 0 {
			baseContext["official_domains"] = refineResult.OfficialDomains
		}
		if len(refineResult.DisambiguationTerms) > 0 {
			baseContext["disambiguation_terms"] = refineResult.DisambiguationTerms
		}
		// Deep Research 2.0: Pass query type and structured dimensions
		if refineResult.QueryType != "" {
			baseContext["query_type"] = refineResult.QueryType
		}
		if len(refineResult.ResearchDimensions) > 0 {
			baseContext["research_dimensions"] = refineResult.ResearchDimensions
		}
		// Deep Research 2.0: Pass localization information
		if refineResult.LocalizationNeeded {
			baseContext["localization_needed"] = refineResult.LocalizationNeeded
		}
		if len(refineResult.TargetLanguages) > 0 {
			baseContext["target_languages"] = refineResult.TargetLanguages
		}
		if len(refineResult.LocalizedNames) > 0 {
			baseContext["localized_names"] = refineResult.LocalizedNames
		}
		// Account for refinement tokens in the workflow total
		totalTokens += refineResult.TokensUsed

		// Record refinement token usage for accurate cost tracking
		if refineResult.TokensUsed > 0 {
			inTok := 0
			// We only have a total for refinement; approximate split 60/40
			if refineResult.TokensUsed > 0 {
				inTok = int(float64(refineResult.TokensUsed) * 0.6)
			}
			outTok := refineResult.TokensUsed - inTok
			recCtx := opts.WithTokenRecordOptions(ctx)
			_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
				UserID:       input.UserID,
				SessionID:    input.SessionID,
				TaskID:       workflowID,
				AgentID:      "research-refiner",
				Model:        refineResult.ModelUsed,
				Provider:     refineResult.Provider,
				InputTokens:  inTok,
				OutputTokens: outTok,
				Metadata:     map[string]interface{}{"phase": "refine"},
			}).Get(recCtx, nil)
		}

		// Emit refinement complete event with details (include canonical/entity hints for diagnostics)
		dimCount := len(refineResult.ResearchDimensions)
		if dimCount == 0 {
			dimCount = len(refineResult.ResearchAreas) // Fallback to old field count
		}
		emitTaskUpdatePayload(ctx, input, activities.StreamEventProgress, "research-refiner",
			fmt.Sprintf("Expanded query into %d research dimensions", dimCount),
			map[string]interface{}{
				"original_query":       input.Query,
				"refined_query":        refineResult.RefinedQuery,
				"research_areas":       refineResult.ResearchAreas,
				"rationale":            refineResult.Rationale,
				"tokens_used":          refineResult.TokensUsed,
				"model_used":           refineResult.ModelUsed,
				"provider":             refineResult.Provider,
				"canonical_name":       refineResult.CanonicalName,
				"exact_queries":        refineResult.ExactQueries,
				"official_domains":     refineResult.OfficialDomains,
				"disambiguation_terms": refineResult.DisambiguationTerms,
				// Deep Research 2.0 fields
				"query_type":          refineResult.QueryType,
				"research_dimensions": refineResult.ResearchDimensions,
				"localization_needed": refineResult.LocalizationNeeded,
				"target_languages":    refineResult.TargetLanguages,
			})
	} else if err != nil {
		logger.Warn("Query refinement failed, using original query", "error", err)
		// Emit warning but continue with original query
		emitTaskUpdate(ctx, input, activities.StreamEventProgress, "research-refiner", "Query refinement skipped, proceeding with original query")
	}

	// Check pause/cancel after query refinement - signal may have arrived during the activity
	if err := controlHandler.CheckPausePoint(ctx, "post_query_refinement"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Check pause/cancel before decomposition
	if err := controlHandler.CheckPausePoint(ctx, "pre_decomposition"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Step 1: Decompose the (now refined) research query
	var decomp activities.DecompositionResult
	err = workflow.ExecuteActivity(ctx,
		constants.DecomposeTaskActivity,
		activities.DecompositionInput{
			Query:          refinedQuery, // Use refined query here
			Context:        baseContext,
			AvailableTools: []string{},
		}).Get(ctx, &decomp)

	if err != nil {
		logger.Error("Task decomposition failed", "error", err)
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to decompose task: %v", err),
		}, err
	}

	// Validate task contracts before execution
	if err := validateTaskContracts(decomp.Subtasks, logger); err != nil {
		logger.Error("Task contract validation failed", "error", err)
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Task contract validation failed: %v", err),
		}, err
	}

	// Record decomposition token usage for accurate cost tracking (if provided)
	if decomp.TokensUsed > 0 || decomp.InputTokens > 0 || decomp.OutputTokens > 0 {
		inTok := decomp.InputTokens
		outTok := decomp.OutputTokens
		if inTok == 0 && outTok == 0 && decomp.TokensUsed > 0 {
			inTok = int(float64(decomp.TokensUsed) * 0.6)
			outTok = decomp.TokensUsed - inTok
		}
		recCtx := opts.WithTokenRecordOptions(ctx)
		_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
			UserID:       input.UserID,
			SessionID:    input.SessionID,
			TaskID:       workflowID,
			AgentID:      "decompose",
			Model:        decomp.ModelUsed,
			Provider:     decomp.Provider,
			InputTokens:  inTok,
			OutputTokens: outTok,
			Metadata:     map[string]interface{}{"phase": "decompose"},
		}).Get(recCtx, nil)
	}

	// Check pause/cancel after decomposition - signal may have arrived during the activity
	if err := controlHandler.CheckPausePoint(ctx, "post_decomposition"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Check for budget configuration
	agentMaxTokens := 0
	if v, ok := baseContext["budget_agent_max"].(int); ok {
		agentMaxTokens = v
	}
	if v, ok := baseContext["budget_agent_max"].(float64); ok && v > 0 {
		agentMaxTokens = int(v)
	}
	// Apply minimum budget if configured
	if minBudget, ok := baseContext["budget_agent_min"].(int); ok && minBudget > agentMaxTokens {
		agentMaxTokens = minBudget
	}
	if minBudget, ok := baseContext["budget_agent_min"].(float64); ok && int(minBudget) > agentMaxTokens {
		agentMaxTokens = int(minBudget)
	}

	modelTier := determineModelTier(baseContext, "medium")
	// Ensure baseContext always has model_tier for consistent propagation
	// (parallel/hybrid execution may use non-budget path which reads from context)
	baseContext["model_tier"] = modelTier
	var agentResults []activities.AgentExecutionResult
	var domainPrefetchResults []activities.AgentExecutionResult
	domainPrefetchTokens := 0
	var domainAnalysisResult *DomainAnalysisResult

	domainAnalysisWorkflowVersion := workflow.GetVersion(ctx, "domain_analysis_workflow_v1", workflow.DefaultVersion, 1)
	if domainAnalysisWorkflowVersion >= 1 && strings.EqualFold(refineResult.QueryType, "company") {
		enabled := true
		if v, ok := baseContext["enable_domain_analysis"].(bool); ok {
			enabled = v
		} else if v, ok := baseContext["enable_domain_prefetch"].(bool); ok {
			enabled = v
		}
		if mode, ok := baseContext["domain_analysis_mode"].(string); ok && strings.EqualFold(strings.TrimSpace(mode), "off") {
			enabled = false
		}

		if enabled {
			requestedRegions := prefetchRegionsFromContext(baseContext)
			if len(requestedRegions) == 0 {
				requestedRegions = prefetchRegionsFromQuery(input.Query)
			}
			planHints := buildDomainAnalysisPlanHints(decomp.Subtasks)
			domainAnalysisMode, _ := baseContext["domain_analysis_mode"].(string)

			var childResult DomainAnalysisResult
			err := workflow.ExecuteChildWorkflow(ctx, DomainAnalysisWorkflow, DomainAnalysisInput{
				ParentWorkflowID:     workflowID,
				Query:                input.Query,
				CanonicalName:        refineResult.CanonicalName,
				DisambiguationTerms:  refineResult.DisambiguationTerms,
				ResearchAreas:        refineResult.ResearchAreas,
				ResearchDimensions:   refineResult.ResearchDimensions,
				OfficialDomains:      refineResult.OfficialDomains,
				ExactQueries:         refineResult.ExactQueries,
				TargetLanguages:      refineResult.TargetLanguages,
				LocalizationNeeded:   refineResult.LocalizationNeeded,
				PrefetchSubpageLimit: refineResult.PrefetchSubpageLimit,
				RequestedRegions:     requestedRegions,
				PlanHints:            planHints,
				Context:              baseContext,
				UserID:               input.UserID,
				SessionID:            input.SessionID,
				History:              input.History,
				DomainAnalysisMode:   domainAnalysisMode,
			}).Get(ctx, &childResult)

			if err != nil {
				logger.Warn("Domain analysis workflow failed", "error", err)
			} else {
				domainAnalysisResult = &childResult
				if len(childResult.OfficialDomainsSelected) > 0 {
					domains := extractDomainsFromCoverage(childResult.OfficialDomainsSelected)
					if len(domains) > 0 {
						baseContext["official_domains"] = domains
						baseContext["official_domains_source"] = "domain_analysis"
					}
				}
			}
		}
	}

	if domainAnalysisWorkflowVersion == workflow.DefaultVersion {
		// Step 2.1: Programmatic domain prefetch for company research
		// This uses refinement hints (canonical_name, official_domains) to force web_fetch
		// on likely official sites before freeform exploration.
		domainPrefetchVersion := workflow.GetVersion(ctx, "domain_prefetch_v1", workflow.DefaultVersion, 1)
		if domainPrefetchVersion >= 1 && (strings.EqualFold(refineResult.QueryType, "company") || strings.EqualFold(refineResult.QueryType, "comparative")) {
			enabled := true
			if v, ok := baseContext["enable_domain_prefetch"].(bool); ok {
				enabled = v
			}
			if enabled {
				// Use LLM-determined target_languages (based on company region) instead of query language.
				// This ensures Chinese sources (tianyancha, etc.) are only used for Chinese companies.
				regionCode := extractRegionCodeFromTargetLanguages(refineResult.TargetLanguages)

				prefetchDiscoverOnlyVersion := workflow.GetVersion(ctx, "domain_prefetch_discover_only_v1", workflow.DefaultVersion, 3)
				hybridPrefetchEnabled := prefetchDiscoverOnlyVersion == 2

				var urls []string
				if prefetchDiscoverOnlyVersion >= 1 {
					domainDiscoveryVersion := workflow.GetVersion(ctx, "domain_discovery_search_first_v1", workflow.DefaultVersion, 1)
					if domainDiscoveryVersion >= 1 && strings.TrimSpace(refineResult.CanonicalName) != "" {
						originRegion := originPrefetchRegionFromTargetLanguages(refineResult.TargetLanguages)

						requestedRegions := prefetchRegionsFromContext(baseContext)
						if len(requestedRegions) == 0 {
							requestedRegions = prefetchRegionsFromQuery(input.Query)
						}

						// Discover-only mode (v1) / Hybrid mode (v2): reset any refinement-provided domains.
						delete(baseContext, "official_domains")
						delete(baseContext, "official_domains_source")

						// Build DomainAnalysisIntent for multinational strategy (domain_analysis_v1)
						domainAnalysisVersion := workflow.GetVersion(ctx, "domain_analysis_v1", workflow.DefaultVersion, 1)
						intent := BuildDomainAnalysisIntent(
							input.Query,
							refineResult.ResearchAreas,
							requestedRegions,
							refineResult.TargetLanguages,
							refineResult.LocalizationNeeded,
						)

						// maxPrefetch based on multinational strategy
						// - Non-multinational: 5 (global + origin only)
						// - Multinational: 8 (global + eu + cn + jp + topic)
						// - With explicit regions: 5
						// - Max limit: 15 (same as web_subpage_fetch MAX_LIMIT)
						maxPrefetch := 8
						if domainAnalysisVersion >= 1 {
							if len(requestedRegions) > 0 {
								maxPrefetch = 5
							} else if !intent.MultinationalDefault {
								maxPrefetch = 5 // Non-multinational: fewer prefetch slots
							}
						} else if len(requestedRegions) > 0 {
							maxPrefetch = 5
						}
						if v, ok := baseContext["domain_prefetch_max_urls"]; ok {
							switch t := v.(type) {
							case int:
								maxPrefetch = t
							case float64:
								maxPrefetch = int(t)
							}
						}
						if maxPrefetch < 1 {
							maxPrefetch = 1
						}
						if maxPrefetch > 15 { // Same as web_subpage_fetch MAX_LIMIT
							maxPrefetch = 15
						}

						// v3 (discover-only strict) reduces domain_discovery from 4 fixed region searches to:
						// - primary (origin-first when available, else global)
						// - optional global fallback (at most 2 searches total)
						// - topic-based searches (ir, docs, careers, product) are preserved from buildDomainDiscoverySearches
						searches := buildDomainDiscoverySearches(refineResult.CanonicalName, refineResult.DisambiguationTerms, originRegion, requestedRegions, refineResult.ResearchAreas, refineResult.OfficialDomains)
						globalQuery := buildCompanyDomainDiscoverySearchQuery(refineResult.CanonicalName, refineResult.DisambiguationTerms, "")
						if prefetchDiscoverOnlyVersion >= 3 && len(requestedRegions) == 0 {
							originLang := originRegionToDiscoveryLanguageCode(originRegion)
							primaryQuery := buildCompanyDomainDiscoverySearchQuery(refineResult.CanonicalName, refineResult.DisambiguationTerms, originLang)
							if strings.TrimSpace(primaryQuery) == "" {
								primaryQuery = globalQuery
							}
							// Extract topic-based searches (ir, docs, careers, product_*) to preserve them
							var topicSearches []domainDiscoverySearch
							for _, s := range searches {
								if s.Key == "ir" || s.Key == "docs" || s.Key == "careers" || strings.HasPrefix(s.Key, "product_") {
									topicSearches = append(topicSearches, s)
								}
							}
							if strings.TrimSpace(primaryQuery) != "" {
								// Start with primary search, then append topic searches
								searches = []domainDiscoverySearch{{Key: "primary", Query: primaryQuery}}
								searches = append(searches, topicSearches...)
							}
						}

						// domain_analysis_v1: Apply multinational filtering when no explicit regions
						// Non-multinational companies only search global + origin + topic searches
						if domainAnalysisVersion >= 1 && len(requestedRegions) == 0 && !intent.MultinationalDefault {
							var filteredSearches []domainDiscoverySearch
							for _, s := range searches {
								// Keep topic searches (ir, docs, careers, product_*) and primary/global
								if s.Key == "ir" || s.Key == "docs" || s.Key == "careers" ||
									strings.HasPrefix(s.Key, "product_") ||
									s.Key == "primary" || s.Key == "global" ||
									s.Key == originRegion {
									filteredSearches = append(filteredSearches, s)
								}
							}
							if len(filteredSearches) > 0 {
								searches = filteredSearches
								logger.Info("Domain analysis: non-multinational filtering applied",
									"original_count", len(searches),
									"filtered_count", len(filteredSearches),
									"multinational", intent.MultinationalDefault,
								)
							}
						}
						discoveredBySearch := make(map[string][]string)
						var allDiscovered []string
						seenAll := make(map[string]bool)

						searchDomainsFromResults := domainsFromWebSearchToolExecutionsAll
						if prefetchDiscoverOnlyVersion >= 2 {
							searchDomainsFromResults = func(toolExecs []activities.ToolExecution) []string {
								return domainsFromWebSearchToolExecutionsAllV2(toolExecs, refineResult.CanonicalName)
							}
						}

						discoveryContext := map[string]interface{}{
							"user_id":    input.UserID,
							"session_id": input.SessionID,
							"model_tier": "small",
							"role":       "domain_discovery", // Use specialized preset
							"response_format": map[string]interface{}{
								"type": "json_object",
							},
						}
						if input.ParentWorkflowID != "" {
							discoveryContext["parent_workflow_id"] = input.ParentWorkflowID
						}

						// === BATCH DOMAIN DISCOVERY (single LLM call) ===
						// Collect all search queries and execute them in a single agent call
						// This reduces N LLM calls to 1, significantly improving efficiency
						var allQueries []string
						for _, s := range searches {
							allQueries = append(allQueries, s.Query)
						}

						// Build batch discovery prompt with all queries
						var discoveryResult activities.AgentExecutionResult
						discoveryQuery := fmt.Sprintf(
							"Find official domains for %q.\n\n"+
								"STEP 1: Execute web_search for each query:\n",
							refineResult.CanonicalName,
						)
						for _, q := range allQueries {
							discoveryQuery += fmt.Sprintf("- %s\n", q)
						}
						discoveryQuery += "\n" +
							"STEP 2: After ALL searches complete, respond with ONLY this JSON:\n" +
							"{\"domains\":[\"domain1.com\",\"domain2.com\"]}\n\n" +
							"RULES:\n" +
							"- Include: corporate sites, IR sites (abc.xyz), parent company sites\n" +
							"- Exclude: login/accounts, store, support, third-party (wikipedia, linkedin)\n" +
							"- Strip www prefix, no paths\n" +
							"- Max 10 domains\n\n" +
							"CRITICAL: Your response must be ONLY the JSON object, nothing else.\n"

						// Add research focus using original query (not ResearchAreas)
						// ResearchAreas come from Refiner's research_dimensions, which may not match user intent
						discoveryQuery += fmt.Sprintf("\n=== RESEARCH FOCUS ===\n"+
							"Original query: %s\n"+
							"Find official domains most relevant to answering this query.\n",
							input.Query)

						// Use ResearchAreas only as category hints for domain prioritization
						if len(refineResult.ResearchAreas) > 0 {
							focusCategories := classifyFocusCategories(refineResult.ResearchAreas)
							if len(focusCategories) > 0 {
								discoveryQuery += fmt.Sprintf("Domain type hints: %s\n",
									strings.Join(focusCategories, ", "))
							}
						}

						// Single agent call with first query as tool_parameters hint
						// Agent will execute multiple web_search calls based on the prompt
						discoveryErr := workflow.ExecuteActivity(ctx,
							"ExecuteAgent",
							activities.AgentExecutionInput{
								Query:     discoveryQuery,
								AgentID:   "domain_discovery",
								Context:   discoveryContext,
								Mode:      "standard",
								SessionID: input.SessionID,
								History:   convertHistoryForAgent(input.History),
								SuggestedTools: []string{
									"web_search",
								},
								ToolParameters: map[string]interface{}{
									"tool":        "web_search",
									"query":       allQueries[0], // Primary query as hint
									"max_results": 20,
								},
								ParentWorkflowID: input.ParentWorkflowID,
							},
						).Get(ctx, &discoveryResult)

						if discoveryErr != nil || !discoveryResult.Success {
							logger.Warn("Domain discovery batch search failed",
								"canonical_name", refineResult.CanonicalName,
								"queries", allQueries,
								"error", discoveryErr,
								"agent_error", discoveryResult.Error,
							)
						} else {
							// Persist domain_discovery execution for observability
							// Use workflowID (parent task ID) for consistent DB querying across discovery/prefetch
							// Also store child_workflow_id for debugging/traceability
							childWorkflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
							persistAgentExecutionLocalWithMeta(
								ctx,
								workflowID, // Use parent task workflow ID for unified DB queries
								"domain_discovery",
								fmt.Sprintf("Domain discovery: %s (batch, %d queries)", refineResult.CanonicalName, len(allQueries)),
								discoveryResult,
								map[string]interface{}{
									"phase":             "domain_discovery",
									"batch_mode":        true,
									"query_count":       len(allQueries),
									"queries":           allQueries,
									"child_workflow_id": childWorkflowID, // Dual-write for traceability
								},
							)

							searchDomainsAll := searchDomainsFromResults(discoveryResult.ToolExecutions)
							llmDomains := domainsFromDiscoveryResponse(discoveryResult.Response)
							if prefetchDiscoverOnlyVersion >= 3 {
								llmDomains = domainsFromDiscoveryResponseV2(discoveryResult.Response)
							}

							if len(llmDomains) == 0 && prefetchDiscoverOnlyVersion >= 3 {
								logger.Warn("Domain discovery returned no parseable JSON domains",
									"canonical_name", refineResult.CanonicalName,
									"queries", allQueries,
								)
							} else {
								searchSet := make(map[string]bool)
								for _, d := range searchDomainsAll {
									searchSet[d] = true
								}

								isGrounded := func(llmDomain string) bool {
									if searchSet[llmDomain] {
										return true
									}
									suffix := "." + llmDomain
									for sd := range searchSet {
										if strings.HasSuffix(sd, suffix) {
											return true
										}
									}
									return false
								}

								// Extract grounded domains
								for _, d := range llmDomains {
									if isGrounded(d) && !seenAll[d] {
										seenAll[d] = true
										allDiscovered = append(allDiscovered, d)
									}
								}

								// Fallback to search domains if LLM domains not grounded (pre-v3 behavior)
								if len(allDiscovered) == 0 && prefetchDiscoverOnlyVersion < 3 {
									for _, d := range searchDomainsAll {
										if !seenAll[d] {
											seenAll[d] = true
											allDiscovered = append(allDiscovered, d)
										}
									}
								}

								// Store in discoveredBySearch for compatibility
								if len(allDiscovered) > 0 {
									discoveredBySearch["batch"] = allDiscovered
								}
							}

							// Record token usage
							if discoveryResult.TokensUsed > 0 || discoveryResult.InputTokens > 0 || discoveryResult.OutputTokens > 0 {
								inTok := discoveryResult.InputTokens
								outTok := discoveryResult.OutputTokens
								recCtx := opts.WithTokenRecordOptions(ctx)
								_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
									UserID:       input.UserID,
									SessionID:    input.SessionID,
									TaskID:       workflowID,
									AgentID:      "domain_discovery",
									Model:        discoveryResult.ModelUsed,
									Provider:     discoveryResult.Provider,
									InputTokens:  inTok,
									OutputTokens: outTok,
									Metadata:     map[string]interface{}{"phase": "domain_discovery", "batch_mode": true, "query_count": len(allQueries)},
								}).Get(recCtx, nil)
							}
						}

						if len(allDiscovered) > 0 {
							candidateDomains := allDiscovered
							officialDomainsSource := "search_first_discovered_only_v1"
							if prefetchDiscoverOnlyVersion >= 3 {
								officialDomainsSource = "search_first_discovered_only_v3"
							}
							prefetchStrategy := "discover-only"
							var hybridAdded []string
							if hybridPrefetchEnabled {
								hybridAdded = vettedHybridRefinementDomains(allDiscovered, refineResult.OfficialDomains, refineResult.CanonicalName)
								candidateDomains = append(append([]string{}, allDiscovered...), hybridAdded...)
								candidateDomains = sortDomainsForHybridPrefetch(candidateDomains, refineResult.CanonicalName)
								officialDomainsSource = "search_first_hybrid_v2"
								prefetchStrategy = "hybrid"
							}

							// Scope official_domains to requested regions when user explicitly requested a region.
							scoped := candidateDomains
							if len(requestedRegions) > 0 {
								scoped = selectDomainsForPrefetch(candidateDomains, requestedRegions, originRegion, 100)
								if len(scoped) == 0 {
									scoped = nil
								}
							}
							if len(scoped) > 0 {
								baseContext["official_domains"] = scoped
								baseContext["official_domains_source"] = officialDomainsSource
							}

							// Use focus-aware selection in domain_analysis_v1
							var prefetchDomains []string
							if domainAnalysisVersion >= 1 {
								prefetchDomains = selectDomainsForPrefetchWithFocus(candidateDomains, requestedRegions, originRegion, maxPrefetch, intent.FocusCategories)
							} else {
								prefetchDomains = selectDomainsForPrefetch(candidateDomains, requestedRegions, originRegion, maxPrefetch)
							}
							urls = buildPrefetchURLsFromDomains(prefetchDomains)

							logger.Info("Domain discovery (prefetch) completed",
								"canonical_name", refineResult.CanonicalName,
								"origin_region", originRegion,
								"requested_regions", requestedRegions,
								"prefetch_strategy", prefetchStrategy,
								"searches", searches,
								"discovered_by_search", discoveredBySearch,
								"all_discovered_count", len(allDiscovered),
								"hybrid_added_domains", hybridAdded,
								"candidate_domains_count", len(candidateDomains),
								"prefetch_domains", prefetchDomains,
								"prefetch_urls", urls,
							)
						} else {
							logger.Info("Domain discovery returned no domains; skipping discover-only prefetch",
								"canonical_name", refineResult.CanonicalName,
								"origin_region", originRegion,
								"requested_regions", requestedRegions,
								"searches", searches,
							)
						}
					}
				} else {
					officialDomainsForPrefetch := refineResult.OfficialDomains
					domainDiscoveryVersion := workflow.GetVersion(ctx, "domain_discovery_search_first_v1", workflow.DefaultVersion, 1)
					if domainDiscoveryVersion >= 1 && strings.TrimSpace(refineResult.CanonicalName) != "" {
						searchQuery := buildCompanyDomainDiscoverySearchQuery(refineResult.CanonicalName, refineResult.DisambiguationTerms, regionCode)
						if searchQuery != "" {
							discoveryContext := map[string]interface{}{
								"user_id":    input.UserID,
								"session_id": input.SessionID,
								"model_tier": "small",
								"role":       "domain_discovery", // Use specialized preset
								"response_format": map[string]interface{}{
									"type": "json_object",
								},
							}
							if input.ParentWorkflowID != "" {
								discoveryContext["parent_workflow_id"] = input.ParentWorkflowID
							}

							var discoveryResult activities.AgentExecutionResult
							// Build discovery query with detailed rules and research focus
							discoveryQuery2 := fmt.Sprintf(
								"Extract the official website domains for the company %q.\n\n"+
									"Use ONLY domains that appear in the provided web_search results (do not guess or fabricate).\n"+
									"Return JSON ONLY with this schema (no markdown, no prose):\n"+
									"{\"domains\":[\"example.com\",\"docs.example.com\",...]}\n\n"+
									"=== DOMAIN SELECTION PRIORITY (highest to lowest) ===\n"+
									"1. Corporate main site (company.com, about.company.com)\n"+
									"2. Investor relations / IR site (ir.company.com, investors.company.com)\n"+
									"3. Parent company site (if subsidiary, e.g., abc.xyz for Google/Alphabet)\n"+
									"4. Documentation / Developer hub (docs.company.com, developer.company.com)\n"+
									"5. Regional main sites ONLY if highly relevant (jp.company.com) - max 1-2\n\n"+
									"=== MANDATORY EXCLUSIONS (never include these) ===\n"+
									"- Login/account pages: accounts.*, login.*, signin.*, auth.*\n"+
									"- E-commerce/store: store.*, shop.*, buy.*\n"+
									"- News aggregators: news.* (unless it's the company's official newsroom)\n"+
									"- Productivity tools: sites.*, drive.*, calendar.*, mail.*\n"+
									"- Support/help pages: support.*, help.* (low information density)\n"+
									"- Third-party: wikipedia, linkedin, crunchbase, github.io, *.mintlify.app\n\n"+
									"=== OUTPUT RULES ===\n"+
									"- Strip \"www.\" prefix\n"+
									"- No paths (company.com/about → company.com)\n"+
									"- Return at most 5 domains, prioritizing information-rich sites\n"+
									"- If none found, return {\"domains\":[]}\n",
								refineResult.CanonicalName,
							)
							// Add research focus hint if available
							if len(refineResult.ResearchAreas) > 0 {
								focusAreas := refineResult.ResearchAreas
								if len(focusAreas) > maxResearchFocusAreas {
									focusAreas = focusAreas[:maxResearchFocusAreas]
								}
								discoveryQuery2 += fmt.Sprintf("\n=== RESEARCH FOCUS HINT ===\n"+
									"This research focuses on: %s\n"+
									"Prioritize domains containing information about these topics (e.g., IR sites for financial research).\n",
									strings.Join(focusAreas, ", "))
							}

							discoveryErr := workflow.ExecuteActivity(ctx,
								"ExecuteAgent",
								activities.AgentExecutionInput{
									Query:     discoveryQuery2,
									AgentID:   "domain_discovery",
									Context:   discoveryContext,
									Mode:      "standard",
									SessionID: input.SessionID,
									History:   convertHistoryForAgent(input.History),
									SuggestedTools: []string{
										"web_search",
									},
									ToolParameters: map[string]interface{}{
										"tool":        "web_search",
										"query":       searchQuery,
										"max_results": 20,
									},
									ParentWorkflowID: input.ParentWorkflowID,
								},
							).Get(ctx, &discoveryResult)

							if discoveryErr != nil || !discoveryResult.Success {
								logger.Warn("Domain discovery search-first failed; falling back to refinement domains",
									"canonical_name", refineResult.CanonicalName,
									"search_query", searchQuery,
									"error", discoveryErr,
									"agent_error", discoveryResult.Error,
								)
							} else {
								// Persist domain_discovery (global fallback) for observability
								globalDiscoveryWfID := workflow.GetInfo(ctx).WorkflowExecution.ID
								persistAgentExecutionLocalWithMeta(
									ctx,
									globalDiscoveryWfID,
									"domain_discovery",
									fmt.Sprintf("Domain discovery (global_fallback): %s", refineResult.CanonicalName),
									discoveryResult,
									map[string]interface{}{
										"phase":      "domain_discovery",
										"search_key": "global_fallback",
										"query":      searchQuery,
									},
								)

								searchDomains := domainsFromWebSearchToolExecutions(discoveryResult.ToolExecutions, refineResult.CanonicalName)
								llmDomains := domainsFromDiscoveryResponse(discoveryResult.Response)

								// Prefer LLM-selected domains, but only keep domains that are grounded in search result URLs.
								// Build a set of search domains for exact matching
								searchSet := make(map[string]bool)
								for _, d := range searchDomains {
									searchSet[d] = true
								}

								// Helper to check if a domain is grounded:
								// - Exact match (llm said "jp.ptmind.com", search has "jp.ptmind.com")
								// - LLM said root domain, search has subdomain (llm said "ptmind.com", search has "jp.ptmind.com")
								isGrounded := func(llmDomain string) bool {
									if searchSet[llmDomain] {
										return true
									}
									// Check if any search domain is a subdomain of llmDomain
									suffix := "." + llmDomain
									for sd := range searchSet {
										if strings.HasSuffix(sd, suffix) {
											return true
										}
									}
									return false
								}

								var discovered []string
								seenDiscovered := make(map[string]bool)

								// Only use LLM-selected domains (no subdomain expansion)
								// Subdomain expansion was adding back unwanted domains like accounts.*, store.*
								for _, d := range llmDomains {
									if isGrounded(d) && !seenDiscovered[d] {
										seenDiscovered[d] = true
										discovered = append(discovered, d)
									}
								}

								// Fallback: if nothing discovered, use all search domains
								if len(discovered) == 0 {
									discovered = searchDomains
								}

								if len(discovered) > 0 {
									// Merge: search-discovered + refinement-guessed domains (dedup)
									merged := discovered
									seen := make(map[string]bool)
									for _, d := range discovered {
										seen[d] = true
									}
									for _, d := range refineResult.OfficialDomains {
										if !seen[d] {
											merged = append(merged, d)
											seen[d] = true
										}
									}

									baseContext["official_domains"] = merged
									baseContext["official_domains_source"] = "search_first_merged"

									officialDomainsForPrefetch = merged

									// Multinational companies need more prefetch slots
									maxPrefetch := 5
									if len(refineResult.TargetLanguages) > 1 {
										maxPrefetch = 8
									}
									if len(officialDomainsForPrefetch) > maxPrefetch {
										officialDomainsForPrefetch = officialDomainsForPrefetch[:maxPrefetch]
									}

									logger.Info("Domain discovery search-first succeeded",
										"canonical_name", refineResult.CanonicalName,
										"search_query", searchQuery,
										"discovered", discovered,
										"merged", merged,
										"refinement_domains", refineResult.OfficialDomains,
									)
								}

								if discoveryResult.TokensUsed > 0 || discoveryResult.InputTokens > 0 || discoveryResult.OutputTokens > 0 {
									inTok := discoveryResult.InputTokens
									outTok := discoveryResult.OutputTokens
									recCtx := opts.WithTokenRecordOptions(ctx)
									_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
										UserID:       input.UserID,
										SessionID:    input.SessionID,
										TaskID:       workflowID,
										AgentID:      "domain_discovery",
										Model:        discoveryResult.ModelUsed,
										Provider:     discoveryResult.Provider,
										InputTokens:  inTok,
										OutputTokens: outTok,
										Metadata:     map[string]interface{}{"phase": "domain_discovery"},
									}).Get(recCtx, nil)
								}
							}
						}
					}

					urls = buildCompanyPrefetchURLsWithLocale(refineResult.CanonicalName, officialDomainsForPrefetch, regionCode)
					if len(urls) > 0 {
						// Cap prefetch attempts to avoid excessive tool usage (official + top aggregators)
						if len(urls) > 5 {
							urls = urls[:5]
						}
					}
				}

				if len(urls) > 0 {

					var failedDomains []string

					// Calculate base subpage limit from refiner recommendation (default 15, clamp 10-20)
					baseSubpageLimit := 15
					if refineResult.PrefetchSubpageLimit > 0 {
						baseSubpageLimit = refineResult.PrefetchSubpageLimit
						if baseSubpageLimit < 10 {
							baseSubpageLimit = 10
						}
						if baseSubpageLimit > 20 {
							baseSubpageLimit = 20
						}
					}

					// Helper to check if domain is primary (matches canonical name or is official)
					isPrimaryDomain := func(urlStr string) bool {
						host := strings.ToLower(urlStr)
						// Extract hostname
						if idx := strings.Index(host, "://"); idx != -1 {
							host = host[idx+3:]
						}
						if idx := strings.Index(host, "/"); idx != -1 {
							host = host[:idx]
						}
						host = strings.TrimPrefix(host, "www.")

						// Check against canonical name (e.g., "Ptmind" -> "ptmind")
						canonicalLower := strings.ToLower(refineResult.CanonicalName)
						if canonicalLower != "" && strings.Contains(host, canonicalLower) {
							return true
						}

						// Check if in official_domains list
						for _, od := range refineResult.OfficialDomains {
							odLower := strings.ToLower(od)
							if strings.Contains(host, odLower) || strings.Contains(odLower, host) {
								return true
							}
						}
						return false
					}

					logger.Info("Running domain prefetch for company research",
						"urls", urls,
						"base_subpage_limit", baseSubpageLimit,
					)

					type prefetchPayload struct {
						Result activities.AgentExecutionResult
						URL    string
						Index  int
						Err    error
					}

					prefetchChan := workflow.NewChannel(ctx)

					for i, u := range urls {
						url := u
						idx := i + 1

						// Dynamic limit: primary domains get base limit, secondary domains get reduced
						domainLimit := baseSubpageLimit
						if !isPrimaryDomain(url) {
							// Secondary/product domains: reduce by 5, minimum 8
							domainLimit = baseSubpageLimit - 5
							if domainLimit < 8 {
								domainLimit = 8
							}
						}

						workflow.Go(ctx, func(gctx workflow.Context) {
							prefetchContext := make(map[string]interface{})
							for k, v := range baseContext {
								prefetchContext[k] = v
							}
							prefetchContext["research_mode"] = "prefetch"
							prefetchContext["prefetch_url"] = url
							prefetchContext["role"] = "domain_prefetch" // Use specialized preset
							prefetchContext["model_tier"] = "small"     // Downgrade from medium to save cost

							// Use station names with offset for prefetch agents
							prefetchAgentName := agents.GetAgentName(workflowID, agents.IdxDomainPrefetchBase+idx)
							var prefetchResult activities.AgentExecutionResult

							// Build query with research focus for better RELEVANCE judgment
							prefetchQuery := fmt.Sprintf("Use web_subpage_fetch on %s to extract company information.", url)
							if len(refineResult.ResearchAreas) > 0 {
								prefetchQuery += fmt.Sprintf("\n\nResearch focus: %s", strings.Join(refineResult.ResearchAreas, ", "))
							}

							err := workflow.ExecuteActivity(gctx,
								"ExecuteAgent",
								activities.AgentExecutionInput{
									Query:          prefetchQuery,
									AgentID:        prefetchAgentName,
									Context:        prefetchContext,
									Mode:           "standard",
									SessionID:      input.SessionID,
									History:        convertHistoryForAgent(input.History),
									SuggestedTools: []string{"web_subpage_fetch"},
									ToolParameters: map[string]interface{}{
										"tool":            "web_subpage_fetch",
										"url":             url,
										"limit":           domainLimit,
										"target_keywords": "about team leadership company founders management products services",
										"target_paths": []string{
											"/about", "/about-us", "/company",
											"/ir", "/investor-relations", "/investors",
											"/team", "/leadership", "/management",
											"/products", "/services",
										},
									},
									ParentWorkflowID: input.ParentWorkflowID,
								}).Get(gctx, &prefetchResult)

							prefetchChan.Send(gctx, prefetchPayload{
								Result: prefetchResult,
								URL:    url,
								Index:  idx,
								Err:    err,
							})
						})
					}

					// Track failed domains to prevent re-fetching in subsequent iterations
					for range urls {
						var payload prefetchPayload
						prefetchChan.Receive(ctx, &payload)

						if payload.Err != nil {
							logger.Warn("Domain prefetch failed", "url", payload.URL, "error", payload.Err)
							failedDomains = append(failedDomains, payload.URL)
							continue
						}
						if !payload.Result.Success {
							logger.Info("Domain prefetch completed without success", "url", payload.URL)
							failedDomains = append(failedDomains, payload.URL)
							continue
						}

						domainPrefetchResults = append(domainPrefetchResults, payload.Result)
						domainPrefetchTokens += payload.Result.TokensUsed

						// Persist agent and tool execution records with metadata for observability
						prefetchAgentName := agents.GetAgentName(workflowID, agents.IdxDomainPrefetchBase+payload.Index)
						prefetchURLRole := classifyDomainRole(payload.URL) // Re-compute for metadata (deterministic)
						persistAgentExecutionLocalWithMeta(
							ctx,
							workflowID,
							prefetchAgentName,
							fmt.Sprintf("Domain prefetch: %s", payload.URL),
							payload.Result,
							map[string]interface{}{
								"phase":    "domain_prefetch",
								"url":      payload.URL,
								"url_role": prefetchURLRole,
								"index":    payload.Index,
							},
						)

						if payload.Result.TokensUsed > 0 || payload.Result.InputTokens > 0 || payload.Result.OutputTokens > 0 {
							inTok := payload.Result.InputTokens
							outTok := payload.Result.OutputTokens
							recCtx := opts.WithTokenRecordOptions(ctx)
							_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
								UserID:       input.UserID,
								SessionID:    input.SessionID,
								TaskID:       workflowID,
								AgentID:      prefetchAgentName,
								Model:        payload.Result.ModelUsed,
								Provider:     payload.Result.Provider,
								InputTokens:  inTok,
								OutputTokens: outTok,
								Metadata:     map[string]interface{}{"phase": "domain_prefetch"},
							}).Get(recCtx, nil)
						}
					}

					// Store failed domains in context to prevent re-fetching in subsequent iterations
					if len(failedDomains) > 0 {
						baseContext["failed_domains"] = failedDomains
						logger.Info("Tracked failed domains for skip in future fetches",
							"failed_count", len(failedDomains),
							"failed_urls", failedDomains,
						)
					}
				}
			}
		}

	}

	// Check pause/cancel before agent execution phase
	if err := controlHandler.CheckPausePoint(ctx, "pre_agent_execution"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Step 2: Execute based on complexity
	if decomp.ComplexityScore < 0.5 || len(decomp.Subtasks) <= 1 {
		// Simple research - use React pattern for step-by-step exploration
		logger.Info("Using React pattern for simple research",
			"complexity", decomp.ComplexityScore,
		)

		// Allow tuning ReAct iterations via context with safe clamp (2..8)
		// Default depends on strategy: quick -> 2, otherwise 5
		reactMaxIterations := 5
		if sv, ok := baseContext["research_strategy"].(string); ok {
			if strings.ToLower(strings.TrimSpace(sv)) == "quick" {
				reactMaxIterations = 2
			}
		}
		if v, ok := baseContext["react_max_iterations"]; ok {
			switch t := v.(type) {
			case int:
				reactMaxIterations = t
			case float64:
				reactMaxIterations = int(t)
			}
			if reactMaxIterations < 2 {
				reactMaxIterations = 2
			}
			if reactMaxIterations > 8 {
				reactMaxIterations = 8
			}
		}

		reactConfig := patterns.ReactConfig{
			MaxIterations:     reactMaxIterations,
			MinIterations:     2,
			ObservationWindow: 3,
			MaxObservations:   20,
			MaxThoughts:       10,
			MaxActions:        10,
		}

		reactOpts := patterns.Options{
			BudgetAgentMax: agentMaxTokens,
			SessionID:      input.SessionID,
			UserID:         input.UserID,
			EmitEvents:     true,
			ModelTier:      modelTier,
			Context:        baseContext,
		}

		reactResult, err := patterns.ReactLoop(
			ctx,
			refinedQuery,
			baseContext,
			input.SessionID,
			convertHistoryForAgent(input.History),
			reactConfig,
			reactOpts,
		)

		if err != nil {
			return TaskResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("React loop failed: %v", err),
			}, err
		}

		// Use the actual agent results from ReAct (includes tool executions for citation collection)
		agentResults = append(agentResults, reactResult.AgentResults...)
		totalTokens = reactResult.TotalTokens

		// Persist agent executions from ReactLoop
		workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
		for i, result := range reactResult.AgentResults {
			agentID := agents.GetAgentName(workflowID, i)
			persistAgentExecutionLocal(ctx, workflowID, agentID, refinedQuery, result)
		}

	} else {
		// Complex research - check if we should use ReAct per task for deeper reasoning
		useReactPerTask := false
		if v, ok := baseContext["react_per_task"].(bool); ok && v {
			useReactPerTask = true
			logger.Info("react_per_task enabled via context flag")
		}
		// Auto-enable for high complexity only when strategy is deep/academic
		if !useReactPerTask && decomp.ComplexityScore > 0.7 {
			strategy := ""
			if sv, ok := baseContext["research_strategy"].(string); ok {
				strategy = strings.ToLower(strings.TrimSpace(sv))
			}
			if strategy == "deep" || strategy == "academic" {
				useReactPerTask = true
				logger.Info("Auto-enabling react_per_task due to high complexity",
					"complexity", decomp.ComplexityScore,
					"strategy", strategy,
				)
			}
		}

		if useReactPerTask {
			// Use ReAct loop per subtask for deeper reasoning
			logger.Info("Using ReAct per subtask for deep research",
				"complexity", decomp.ComplexityScore,
				"subtasks", len(decomp.Subtasks),
			)

			// Determine execution strategy
			hasDependencies := false
			for _, subtask := range decomp.Subtasks {
				if len(subtask.Dependencies) > 0 {
					hasDependencies = true
					break
				}
			}

			if hasDependencies {
				// Sequential execution with ReAct per subtask, respecting dependencies
				logger.Info("Using sequential ReAct execution due to dependencies")

				// Build execution order via topological sort
				executionOrder := topologicalSort(decomp.Subtasks)
				previousResults := make(map[string]string)

				for _, subtaskID := range executionOrder {
					// Find the subtask
					var subtask activities.Subtask
					for _, st := range decomp.Subtasks {
						if st.ID == subtaskID {
							subtask = st
							break
						}
					}

					// Build context with dependency results
					subtaskContext := make(map[string]interface{})
					for k, v := range baseContext {
						subtaskContext[k] = v
					}
					if contract := buildTaskContractContext(subtask); contract != nil {
						for k, v := range contract {
							subtaskContext[k] = v
						}
					}
					if len(subtask.Dependencies) > 0 {
						depResults := make(map[string]string)
						for _, depID := range subtask.Dependencies {
							if res, ok := previousResults[depID]; ok {
								depResults[depID] = res
							}
						}
						subtaskContext["previous_results"] = depResults
					}

					// Execute this subtask with ReAct
					// Allow tuning ReAct iterations via context with safe clamp (2..8)
					// Default depends on strategy: quick -> 2, otherwise 3
					reactMaxIterations := 3
					if sv, ok := baseContext["research_strategy"].(string); ok {
						if strings.ToLower(strings.TrimSpace(sv)) == "quick" {
							reactMaxIterations = 2
						}
					}
					if v, ok := baseContext["react_max_iterations"]; ok {
						switch t := v.(type) {
						case int:
							reactMaxIterations = t
						case float64:
							reactMaxIterations = int(t)
						}
						if reactMaxIterations < 2 {
							reactMaxIterations = 2
						}
						if reactMaxIterations > 8 {
							reactMaxIterations = 8
						}
					}
					reactConfig := patterns.ReactConfig{
						MaxIterations:     reactMaxIterations,
						MinIterations:     2,
						ObservationWindow: 3,
						MaxObservations:   20,
						MaxThoughts:       10,
						MaxActions:        10,
					}

					reactOpts := patterns.Options{
						BudgetAgentMax: agentMaxTokens,
						SessionID:      input.SessionID,
						UserID:         input.UserID,
						EmitEvents:     true,
						ModelTier:      modelTier,
						Context:        subtaskContext,
					}

					reactResult, err := patterns.ReactLoop(
						ctx,
						subtask.Description,
						subtaskContext,
						input.SessionID,
						convertHistoryForAgent(input.History),
						reactConfig,
						reactOpts,
					)

					if err == nil {
						agentResults = append(agentResults, reactResult.AgentResults...)
						totalTokens += reactResult.TotalTokens
						previousResults[subtaskID] = reactResult.FinalResult

						// Persist agent executions for this subtask
						workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
						for i, result := range reactResult.AgentResults {
							agentID := agents.GetAgentName(workflowID, i)
							persistAgentExecutionLocal(ctx, workflowID, agentID, subtask.Description, result)
						}
					} else {
						logger.Warn("ReAct loop failed for subtask, continuing", "subtask_id", subtaskID, "error", err)
					}
				}

			} else {
				// Parallel execution with ReAct per subtask
				logger.Info("Using parallel ReAct execution (no dependencies)")

				// Determine concurrency limit from context (default 5, clamp 1..20)
				concurrency := 5
				if v, ok := baseContext["max_concurrent_agents"]; ok {
					switch t := v.(type) {
					case int:
						concurrency = t
					case float64:
						if t > 0 {
							concurrency = int(t)
						}
					}
				}
				if concurrency < 1 {
					concurrency = 1
				}
				if concurrency > 20 {
					concurrency = 20
				}

				// Use channel to collect results and gate concurrency
				resultsChan := workflow.NewChannel(ctx)
				active := 0

				for _, subtask := range decomp.Subtasks {
					// Gate concurrency
					if active >= concurrency {
						var result *patterns.ReactLoopResult
						resultsChan.Receive(ctx, &result)
						if result != nil {
							agentResults = append(agentResults, result.AgentResults...)
							totalTokens += result.TotalTokens
						}
						active--
					}

					st := subtask // Capture for goroutine
					workflow.Go(ctx, func(gctx workflow.Context) {
						// Allow tuning ReAct iterations via context with safe clamp (2..8)
						// Default depends on strategy: quick -> 2, otherwise 3
						reactMaxIterations := 3
						if sv, ok := baseContext["research_strategy"].(string); ok {
							if strings.ToLower(strings.TrimSpace(sv)) == "quick" {
								reactMaxIterations = 2
							}
						}
						if v, ok := baseContext["react_max_iterations"]; ok {
							switch t := v.(type) {
							case int:
								reactMaxIterations = t
							case float64:
								reactMaxIterations = int(t)
							}
							if reactMaxIterations < 2 {
								reactMaxIterations = 2
							}
							if reactMaxIterations > 8 {
								reactMaxIterations = 8
							}
						}
						reactConfig := patterns.ReactConfig{
							MaxIterations:     reactMaxIterations,
							MinIterations:     2,
							ObservationWindow: 3,
							MaxObservations:   20,
							MaxThoughts:       10,
							MaxActions:        10,
						}

						reactOpts := patterns.Options{
							BudgetAgentMax: agentMaxTokens,
							SessionID:      input.SessionID,
							UserID:         input.UserID,
							EmitEvents:     true,
							ModelTier:      modelTier,
							Context:        baseContext,
						}

						reactResult, err := patterns.ReactLoop(
							gctx,
							st.Description,
							baseContext,
							input.SessionID,
							convertHistoryForAgent(input.History),
							reactConfig,
							reactOpts,
						)

						if err == nil {
							// Persist agent executions for this parallel subtask
							workflowID := workflow.GetInfo(gctx).WorkflowExecution.ID
							for i, result := range reactResult.AgentResults {
								agentID := agents.GetAgentName(workflowID, i)
								persistAgentExecutionLocal(gctx, workflowID, agentID, st.Description, result)
							}
							resultsChan.Send(gctx, reactResult)
						} else {
							logger.Warn("ReAct loop failed for parallel subtask", "subtask_id", st.ID, "error", err)
							// Send nil to indicate failure
							resultsChan.Send(gctx, (*patterns.ReactLoopResult)(nil))
						}
					})
					active++
				}

				// Drain remaining in-flight tasks
				for active > 0 {
					var result *patterns.ReactLoopResult
					resultsChan.Receive(ctx, &result)
					if result != nil {
						agentResults = append(agentResults, result.AgentResults...)
						totalTokens += result.TotalTokens
					}
					active--
				}
			}

		} else {
			// Complex research - use parallel/hybrid execution with simple agents
			logger.Info("Using parallel execution for complex research",
				"complexity", decomp.ComplexityScore,
				"subtasks", len(decomp.Subtasks),
			)

			// Determine execution strategy
			hasDependencies := false
			for _, subtask := range decomp.Subtasks {
				if len(subtask.Dependencies) > 0 {
					hasDependencies = true
					break
				}
			}

			if hasDependencies {
				// Use hybrid execution for dependency management
				logger.Info("Using hybrid execution due to dependencies")

				hybridTasks := make([]execution.HybridTask, len(decomp.Subtasks))
				for i, subtask := range decomp.Subtasks {
					role := "deep_research_agent"
					if i < len(decomp.AgentTypes) && decomp.AgentTypes[i] != "" {
						role = decomp.AgentTypes[i]
					}

					hybridTasks[i] = execution.HybridTask{
						ID:               subtask.ID,
						Description:      subtask.Description,
						SuggestedTools:   subtask.SuggestedTools,
						ToolParameters:   subtask.ToolParameters,
						PersonaID:        subtask.SuggestedPersona,
						Role:             role,
						ParentArea:       subtask.ParentArea,
						Dependencies:     subtask.Dependencies,
						ContextOverrides: buildTaskContractContext(subtask),
					}
				}

				// Force inject web_search and web_fetch/web_subpage_fetch tools for research workflows
				for i := range hybridTasks {
					hasWebSearch := false
					hasWebFetch := false
					hasWebSubpageFetch := false
					for _, tool := range hybridTasks[i].SuggestedTools {
						if tool == "web_search" {
							hasWebSearch = true
						}
						if tool == "web_fetch" {
							hasWebFetch = true
						}
						if tool == "web_subpage_fetch" {
							hasWebSubpageFetch = true
						}
					}
					if !hasWebSearch {
						hybridTasks[i].SuggestedTools = append(hybridTasks[i].SuggestedTools, "web_search")
					}
					if !hasWebFetch {
						hybridTasks[i].SuggestedTools = append(hybridTasks[i].SuggestedTools, "web_fetch")
					}
					if !hasWebSubpageFetch {
						hybridTasks[i].SuggestedTools = append(hybridTasks[i].SuggestedTools, "web_subpage_fetch")
					}
				}

				// Generate search routing plans for tasks with source guidance
				for i := range hybridTasks {
					subtask := decomp.Subtasks[i]
					if subtask.SourceGuidance != nil && len(subtask.SourceGuidance.Required) > 0 {
						var routeResult activities.SearchRouteResult
						err := workflow.ExecuteActivity(ctx,
							"RouteSearch",
							activities.SearchRouteInput{
								Query:       subtask.Description,
								Dimension:   subtask.ParentArea,
								SourceTypes: subtask.SourceGuidance.Required,
								Priority:    "high",
								Context:     baseContext,
							}).Get(ctx, &routeResult)

						if err == nil && len(routeResult.Routes) > 0 {
							if hybridTasks[i].ContextOverrides == nil {
								hybridTasks[i].ContextOverrides = make(map[string]interface{})
							}
							hybridTasks[i].ContextOverrides["search_routes"] = routeResult.Routes
							hybridTasks[i].ContextOverrides["search_strategy"] = routeResult.Strategy
							logger.Info("Initial research: Search routing plan generated",
								"task", subtask.ID,
								"routes", len(routeResult.Routes),
								"strategy", routeResult.Strategy,
							)
						}
					}
				}

				// Determine concurrency from context (default 5, clamp 1..20)
				hybridMax := 5
				if v, ok := baseContext["max_concurrent_agents"]; ok {
					switch t := v.(type) {
					case int:
						hybridMax = t
					case float64:
						if t > 0 {
							hybridMax = int(t)
						}
					}
				}
				if hybridMax < 1 {
					hybridMax = 1
				}
				if hybridMax > 20 {
					hybridMax = 20
				}

				hybridConfig := execution.HybridConfig{
					MaxConcurrency:           hybridMax,
					EmitEvents:               true,
					Context:                  baseContext,
					DependencyWaitTimeout:    6 * time.Minute,  // Total timeout
					DependencyCheckInterval:  30 * time.Second, // Check every 30s for accurate Temporal UI display
					PassDependencyResults:    true,
					ClearDependentToolParams: true,
				}

				hybridResult, err := execution.ExecuteHybrid(
					ctx,
					hybridTasks,
					input.SessionID,
					convertHistoryForAgent(input.History),
					hybridConfig,
					agentMaxTokens,
					input.UserID,
					modelTier,
				)

				if err != nil {
					return TaskResult{
						Success:      false,
						ErrorMessage: fmt.Sprintf("Hybrid execution failed: %v", err),
					}, err
				}

				// Convert results to agent results
				for _, result := range hybridResult.Results {
					agentResults = append(agentResults, result)
				}
				totalTokens = hybridResult.TotalTokens

			} else {
				// Use pure parallel execution
				logger.Info("Using pure parallel execution")

				parallelTasks := make([]execution.ParallelTask, len(decomp.Subtasks))
				for i, subtask := range decomp.Subtasks {
					role := "deep_research_agent"
					if i < len(decomp.AgentTypes) && decomp.AgentTypes[i] != "" {
						role = decomp.AgentTypes[i]
					}

					parallelTasks[i] = execution.ParallelTask{
						ID:               subtask.ID,
						Description:      subtask.Description,
						SuggestedTools:   subtask.SuggestedTools,
						ToolParameters:   subtask.ToolParameters,
						PersonaID:        subtask.SuggestedPersona,
						Role:             role,
						ParentArea:       subtask.ParentArea,
						ContextOverrides: buildTaskContractContext(subtask),
					}
				}

				// Force inject web_search and web_fetch/web_subpage_fetch tools for research workflows
				for i := range parallelTasks {
					hasWebSearch := false
					hasWebFetch := false
					hasWebSubpageFetch := false
					for _, tool := range parallelTasks[i].SuggestedTools {
						if tool == "web_search" {
							hasWebSearch = true
						}
						if tool == "web_fetch" {
							hasWebFetch = true
						}
						if tool == "web_subpage_fetch" {
							hasWebSubpageFetch = true
						}
					}
					if !hasWebSearch {
						parallelTasks[i].SuggestedTools = append(parallelTasks[i].SuggestedTools, "web_search")
					}
					if !hasWebFetch {
						parallelTasks[i].SuggestedTools = append(parallelTasks[i].SuggestedTools, "web_fetch")
					}
					if !hasWebSubpageFetch {
						parallelTasks[i].SuggestedTools = append(parallelTasks[i].SuggestedTools, "web_subpage_fetch")
					}
				}

				// Generate search routing plans for tasks with source guidance
				for i := range parallelTasks {
					subtask := decomp.Subtasks[i]
					if subtask.SourceGuidance != nil && len(subtask.SourceGuidance.Required) > 0 {
						var routeResult activities.SearchRouteResult
						err := workflow.ExecuteActivity(ctx,
							"RouteSearch",
							activities.SearchRouteInput{
								Query:       subtask.Description,
								Dimension:   subtask.ParentArea,
								SourceTypes: subtask.SourceGuidance.Required,
								Priority:    "high",
								Context:     baseContext,
							}).Get(ctx, &routeResult)

						if err == nil && len(routeResult.Routes) > 0 {
							if parallelTasks[i].ContextOverrides == nil {
								parallelTasks[i].ContextOverrides = make(map[string]interface{})
							}
							parallelTasks[i].ContextOverrides["search_routes"] = routeResult.Routes
							parallelTasks[i].ContextOverrides["search_strategy"] = routeResult.Strategy
							logger.Info("Initial research: Search routing plan generated",
								"task", subtask.ID,
								"routes", len(routeResult.Routes),
								"strategy", routeResult.Strategy,
							)
						}
					}
				}

				// Determine concurrency from context (default 5, clamp 1..20)
				parallelMax := 5
				if v, ok := baseContext["max_concurrent_agents"]; ok {
					switch t := v.(type) {
					case int:
						parallelMax = t
					case float64:
						if t > 0 {
							parallelMax = int(t)
						}
					}
				}
				if parallelMax < 1 {
					parallelMax = 1
				}
				if parallelMax > 20 {
					parallelMax = 20
				}

				parallelConfig := execution.ParallelConfig{
					MaxConcurrency: parallelMax,
					EmitEvents:     true,
					Context:        baseContext,
				}

				parallelResult, err := execution.ExecuteParallel(
					ctx,
					parallelTasks,
					input.SessionID,
					convertHistoryForAgent(input.History),
					parallelConfig,
					agentMaxTokens,
					input.UserID,
					modelTier,
				)

				if err != nil {
					return TaskResult{
						Success:      false,
						ErrorMessage: fmt.Sprintf("Parallel execution failed: %v", err),
					}, err
				}

				agentResults = parallelResult.Results
				totalTokens = parallelResult.TotalTokens
			}
		}
	}

	// Merge domain analysis digest (v1) or legacy prefetch evidence before localized search and gap analysis.
	if domainAnalysisResult != nil && strings.TrimSpace(domainAnalysisResult.DigestMarkdown) != "" {
		agentResults = append([]activities.AgentExecutionResult{domainAnalysisDigestResult(domainAnalysisResult)}, agentResults...)
		if domainAnalysisResult.Stats.DigestTokensUsed > 0 {
			totalTokens += domainAnalysisResult.Stats.DigestTokensUsed
		}
	} else if len(domainPrefetchResults) > 0 {
		agentResults = append(domainPrefetchResults, agentResults...)
		totalTokens += domainPrefetchTokens
	}

	// Step 2.5: Optional localized search for multi-language coverage
	localizedSearchEnabled := false
	if v, ok := baseContext["enable_localized_search"].(bool); ok {
		localizedSearchEnabled = v
	}
	if localizedSearchEnabled {
		// Get target languages from context
		var targetLanguages []string
		if langs, ok := baseContext["target_languages"].([]interface{}); ok {
			for _, l := range langs {
				if ls, ok := l.(string); ok {
					targetLanguages = append(targetLanguages, ls)
				}
			}
		} else if langs, ok := baseContext["target_languages"].([]string); ok {
			targetLanguages = langs
		}

		if len(targetLanguages) > 0 {
			logger.Info("Running localized search",
				"target_languages", targetLanguages,
			)

			// Get entity name for localization
			entityName := ""
			if en, ok := baseContext["canonical_name"].(string); ok {
				entityName = en
			} else {
				entityName = input.Query
			}

			// Step 2.5.1: Detect entity localizations
			var localizedNames map[string][]string
			if presetNames, ok := baseContext["localized_names"].(map[string][]string); ok {
				localizedNames = presetNames
			} else {
				var locResult activities.EntityLocalizationResult
				locErr := workflow.ExecuteActivity(ctx,
					"DetectEntityLocalization",
					activities.EntityLocalizationInput{
						EntityName:       entityName,
						TargetLanguages:  targetLanguages,
						EntityType:       "company", // Default entity type
						ParentWorkflowID: input.ParentWorkflowID,
					}).Get(ctx, &locResult)

				if locErr == nil {
					localizedNames = locResult.LocalizedNames
					totalTokens += locResult.TokensUsed
					if locResult.TokensUsed > 0 || locResult.InputTokens > 0 || locResult.OutputTokens > 0 {
						inTok := locResult.InputTokens
						outTok := locResult.OutputTokens
						if inTok == 0 && outTok == 0 && locResult.TokensUsed > 0 {
							inTok = int(float64(locResult.TokensUsed) * 0.6)
							outTok = locResult.TokensUsed - inTok
						}
						recCtx := opts.WithTokenRecordOptions(ctx)
						_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
							UserID:       input.UserID,
							SessionID:    input.SessionID,
							TaskID:       workflowID,
							AgentID:      "entity_localization",
							Model:        locResult.ModelUsed,
							Provider:     locResult.Provider,
							InputTokens:  inTok,
							OutputTokens: outTok,
							Metadata:     map[string]interface{}{"phase": "localization"},
						}).Get(recCtx, nil)
					}
					logger.Info("Entity localization detected",
						"languages", len(localizedNames),
					)
				} else {
					logger.Warn("Entity localization failed, using original name",
						"error", locErr,
					)
					localizedNames = make(map[string][]string)
					for _, lang := range targetLanguages {
						localizedNames[lang] = []string{entityName}
					}
				}
			}

			// Step 2.5.2: Spawn parallel localized searches using Temporal-safe channels
			if len(localizedNames) > 0 {
				type localizedPayload struct {
					Results []activities.AgentExecutionResult
					Tokens  int
				}

				localizedChan := workflow.NewChannel(ctx)
				numLocalizedSearches := len(targetLanguages)

				for _, lang := range targetLanguages {
					lang := lang // Capture for goroutine
					names := localizedNames[lang]
					if len(names) == 0 {
						names = []string{entityName}
					}

					workflow.Go(ctx, func(gctx workflow.Context) {
						// Build localized search context
						localCtx := make(map[string]interface{})
						for k, v := range baseContext {
							localCtx[k] = v
						}
						localCtx["search_language"] = lang
						localCtx["localized_query"] = names[0] // Use first localized name
						localCtx["is_localized_search"] = true

						// Get regional sites for this language
						regionalSites := activities.GetRegionalSourceSites(lang)
						if len(regionalSites) > 0 {
							localCtx["target_sites"] = regionalSites
						}

						// Query to use for localized search
						localQuery := names[0]

						// Execute the localized search
						reactConfig := patterns.ReactConfig{
							MaxIterations:     2, // Shorter for localized searches
							MinIterations:     1,
							ObservationWindow: 3,
							MaxObservations:   10,
							MaxThoughts:       5,
							MaxActions:        5,
						}
						reactOpts := patterns.Options{
							BudgetAgentMax: agentMaxTokens / 2, // Use half budget for localized
							SessionID:      input.SessionID,
							ModelTier:      modelTier,
							Context:        localCtx,
						}

						result, err := patterns.ReactLoop(
							gctx,
							localQuery,
							localCtx,
							input.SessionID,
							[]string{},
							reactConfig,
							reactOpts,
						)

						payload := localizedPayload{}
						if err == nil && len(result.AgentResults) > 0 {
							// Mark results as localized
							for i := range result.AgentResults {
								result.AgentResults[i].AgentID = fmt.Sprintf("%s-local-%s", result.AgentResults[i].AgentID, lang)
							}
							payload.Results = result.AgentResults
							payload.Tokens = result.TotalTokens
						}
						localizedChan.Send(gctx, payload)
					})
				}

				// Collect localized search results
				for i := 0; i < numLocalizedSearches; i++ {
					var payload localizedPayload
					localizedChan.Receive(ctx, &payload)
					if len(payload.Results) > 0 {
						agentResults = append(agentResults, payload.Results...)
						totalTokens += payload.Tokens
					}
				}

				logger.Info("Localized search complete",
					"total_agent_results", len(agentResults),
					"languages_searched", numLocalizedSearches,
				)
			}
		}
	}

	// Take a snapshot of all agent results BEFORE any filtering, so citation
	// extraction can operate on complete tool outputs regardless of later
	// entity filtering used for synthesis tightness.
	originalAgentResults := make([]activities.AgentExecutionResult, len(agentResults))
	copy(originalAgentResults, agentResults)

	// Optional: filter out agent results that likely belong to the wrong entity
	if v, ok := baseContext["canonical_name"].(string); ok && strings.TrimSpace(v) != "" {
		aliases := []string{v}
		if eqv, ok := baseContext["exact_queries"]; ok {
			switch t := eqv.(type) {
			case []string:
				for _, q := range t {
					aliases = append(aliases, strings.Trim(q, "\""))
				}
			case []interface{}:
				for _, it := range t {
					if s, ok := it.(string); ok {
						aliases = append(aliases, strings.Trim(s, "\""))
					}
				}
			}
		}
		// Use official_domains for additional positive matching
		var domains []string
		if dv, ok := baseContext["official_domains"]; ok {
			switch t := dv.(type) {
			case []string:
				domains = append(domains, t...)
			case []interface{}:
				for _, it := range t {
					if s, ok := it.(string); ok {
						domains = append(domains, s)
					}
				}
			}
		}
		filtered := make([]activities.AgentExecutionResult, 0, len(agentResults))
		removed := 0
		for _, ar := range agentResults {
			txt := strings.ToLower(ar.Response)
			match := false
			for _, a := range aliases {
				if sa := strings.ToLower(strings.TrimSpace(a)); sa != "" && strings.Contains(txt, sa) {
					match = true
					break
				}
			}
			if !match && len(domains) > 0 {
				for _, d := range domains {
					sd := strings.ToLower(strings.TrimSpace(d))
					if sd != "" && strings.Contains(txt, sd) {
						match = true
						break
					}
				}
			}
			// Keep non-search reasoning, drop obvious off-entity tool-driven results
			if match || len(ar.ToolsUsed) == 0 {
				filtered = append(filtered, ar)
			} else {
				removed++
			}
		}
		if len(filtered) > 0 {
			agentResults = filtered
		}
		if removed > 0 {
			logger.Info("Entity filter removed off-entity results",
				"removed", removed,
				"kept", len(agentResults),
				"aliases", aliases,
				"domains", domains,
			)
		}
	}

	// Check pause/cancel after agent execution - signal may have arrived during agent activities
	if err := controlHandler.CheckPausePoint(ctx, "post_agent_execution"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Check pause/cancel before synthesis
	if err := controlHandler.CheckPausePoint(ctx, "pre_synthesis"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Step 3: Synthesize results
	logger.Info("Synthesizing research results",
		"agent_count", len(agentResults),
	)

	// Fallback: if no tool executions were recorded in research phase, force a single search
	if !hasSuccessfulToolExecutions(agentResults) {
		logger.Warn("No successful tool executions found in research phase; running fallback web search")

		fallbackCtx := make(map[string]interface{})
		for k, v := range baseContext {
			fallbackCtx[k] = v
		}
		fallbackCtx["force_research"] = true

		var fallbackResult activities.AgentExecutionResult
		err := workflow.ExecuteActivity(ctx,
			"ExecuteAgent",
			activities.AgentExecutionInput{
				Query:            fmt.Sprintf("Use web_search to gather authoritative information about: %s", refinedQuery),
				AgentID:          "fallback-search",
				Context:          fallbackCtx,
				Mode:             "standard",
				SessionID:        input.SessionID,
				History:          convertHistoryForAgent(input.History),
				SuggestedTools:   []string{"web_search"},
				ParentWorkflowID: input.ParentWorkflowID,
			}).Get(ctx, &fallbackResult)

		if err == nil {
			agentResults = append(agentResults, fallbackResult)
			totalTokens += fallbackResult.TokensUsed
			originalAgentResults = append(originalAgentResults, fallbackResult)
			logger.Info("Fallback web search completed",
				"tokens_used", fallbackResult.TokensUsed,
			)
		} else {
			logger.Warn("Fallback web search failed", "error", err)
		}
	}

	// If fact extraction is requested, use the facts-aware synthesis template unless overridden.
	if enabled, ok := baseContext["enable_fact_extraction"].(bool); ok && enabled {
		if tmpl, ok := baseContext["synthesis_template"].(string); !ok || strings.TrimSpace(tmpl) == "" {
			baseContext["synthesis_template"] = "research_with_facts"
		}
	}

	// Per-agent token usage is recorded inside execution patterns (ReactLoop/Parallel/Hybrid).
	// Avoid double-counting here to prevent duplicate token_usage rows.

	// Collect citations from agent tool outputs and inject into context for synthesis/formatting
	// Also retain them for metadata/verification.
	var collectedCitations []metadata.Citation
	// Build lightweight results array with tool_executions to feed metadata.CollectCitations
	{
		var resultsForCitations []interface{}
		// IMPORTANT: Use original (unfiltered) agent results to preserve all
		// successful tool executions for citation extraction.
		for _, ar := range originalAgentResults {
			// Build tool_executions payload compatible with citations extractor
			var toolExecs []interface{}
			if len(ar.ToolExecutions) > 0 {
				for _, te := range ar.ToolExecutions {
					toolExecs = append(toolExecs, map[string]interface{}{
						"tool":    te.Tool,
						"success": te.Success,
						"output":  te.Output,
						"error":   te.Error,
					})
				}
			}
			resultsForCitations = append(resultsForCitations, map[string]interface{}{
				"agent_id":        ar.AgentID,
				"tool_executions": toolExecs,
				"response":        ar.Response,
			})
		}

		// Use workflow timestamp for determinism; let collector default max to 15
		now := workflow.Now(ctx)
		citations, _ := metadata.CollectCitations(resultsForCitations, now, 0)

		// Apply entity-based filtering if canonical name is present
		if len(citations) > 0 {
			canonicalName, _ := baseContext["canonical_name"].(string)
			if canonicalName != "" {
				// Extract domains and aliases for filtering
				var domains []string
				if d, ok := baseContext["official_domains"].([]string); ok {
					domains = d
				}
				var aliases []string
				if eq, ok := baseContext["exact_queries"].([]string); ok {
					aliases = eq
				}
				// Filter citations by entity relevance with automatic fallback
				logger.Info("Applying citation entity filter",
					"pre_filter_count", len(citations),
					"canonical_name", canonicalName,
					"official_domains", domains,
					"alias_count", len(aliases),
				)
				filterResult := ApplyCitationFilterWithFallback(citations, canonicalName, aliases, domains)
				citations = filterResult.Citations
				if filterResult.Applied {
					logger.Info("Citation filter applied",
						"before", filterResult.Before,
						"after", filterResult.After,
						"retention", filterResult.Retention,
					)
				} else {
					logger.Warn("Citation filter too aggressive, keeping original",
						"before", filterResult.Before,
						"filtered", filterResult.After,
						"retention", filterResult.Retention,
					)
				}
			}
		}

		collectedCitations = citations
		if domainAnalysisResult != nil && len(domainAnalysisResult.Citations) > 0 {
			collectedCitations = mergeCitationsPreferFirst(domainAnalysisResult.Citations, collectedCitations)
		}

		if len(collectedCitations) > 0 {
			// Format into numbered list lines expected by FormatReportWithCitations
			var b strings.Builder
			for i, c := range collectedCitations {
				idx := i + 1
				title := c.Title
				if title == "" {
					title = c.Source
				}
				if c.PublishedDate != nil {
					fmt.Fprintf(&b, "[%d] %s (%s) - %s, %s\n", idx, title, c.URL, c.Source, c.PublishedDate.Format("2006-01-02"))
				} else {
					fmt.Fprintf(&b, "[%d] %s (%s) - %s\n", idx, title, c.URL, c.Source)
				}
			}
			baseContext["available_citations"] = strings.TrimRight(b.String(), "\n")
			baseContext["citation_count"] = len(collectedCitations)

			// Also store structured citations for SSE emission
			out := make([]map[string]interface{}, 0, len(collectedCitations))
			for _, c := range collectedCitations {
				out = append(out, map[string]interface{}{
					"url":               c.URL,
					"title":             c.Title,
					"source":            c.Source,
					"credibility_score": c.CredibilityScore,
					"quality_score":     c.QualityScore,
					"tool_source":       c.ToolSource,
					"status_code":       c.StatusCode,
					"blocked_reason":    c.BlockedReason,
				})
			}
			baseContext["citations"] = out
		}
	}

	// Set synthesis style to comprehensive for research workflows
	baseContext["synthesis_style"] = "comprehensive"
	baseContext["research_areas_count"] = len(refineResult.ResearchAreas)

	// Synthesis tier: use large for deep/academic strategies, medium otherwise
	// This ensures higher quality synthesis output for intensive research modes
	synthTier := "medium"
	if v, ok := baseContext["synthesis_model_tier"].(string); ok && strings.TrimSpace(v) != "" {
		// Explicit override takes precedence
		synthTier = strings.ToLower(strings.TrimSpace(v))
	} else {
		// Auto-select large tier for standard/deep/academic research strategies
		if strategy, ok := baseContext["research_strategy"].(string); ok {
			switch strings.ToLower(strategy) {
			case "standard", "deep", "academic":
				synthTier = "large"
				logger.Info("Using large model tier for synthesis",
					"strategy", strategy,
				)
			}
		}
	}
	baseContext["model_tier"] = synthTier

	var synthesis activities.SynthesisResult
	err = workflow.ExecuteActivity(ctx,
		activities.SynthesizeResultsLLM,
		activities.SynthesisInput{
			// Pass original query through; synthesis embeds its own
			// language instruction (post-synthesis validation removed)
			Query:        input.Query,
			AgentResults: agentResults,
			// Ensure comprehensive report style for research synthesis unless already specified
			Context: func() map[string]interface{} {
				if baseContext == nil {
					baseContext = map[string]interface{}{}
				}
				if _, ok := baseContext["synthesis_style"]; !ok {
					baseContext["synthesis_style"] = "comprehensive"
				}
				return baseContext
			}(),
			CollectedCitations: collectedCitations,
			ParentWorkflowID:   input.ParentWorkflowID,
		}).Get(ctx, &synthesis)

	if err != nil {
		logger.Error("Synthesis failed", "error", err)
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to synthesize results: %v", err),
		}, err
	}

	totalTokens += synthesis.TokensUsed

	// Record synthesis token usage
	if synthesis.TokensUsed > 0 {
		inTok := synthesis.InputTokens
		outTok := synthesis.CompletionTokens
		if inTok == 0 && outTok > 0 {
			// Infer if needed
			est := synthesis.TokensUsed - outTok
			if est > 0 {
				inTok = est
			}
		}
		recCtx := opts.WithTokenRecordOptions(ctx)
		_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
			UserID:       input.UserID,
			SessionID:    input.SessionID,
			TaskID:       workflowID,
			AgentID:      "synthesis",
			Model:        synthesis.ModelUsed,
			Provider:     synthesis.Provider,
			InputTokens:  inTok,
			OutputTokens: outTok,
			Metadata: map[string]interface{}{
				"phase": "synthesis",
			},
		}).Get(recCtx, nil)
	}

	// Step 3.5: Deep Research 2.0 Iterative Loop OR legacy gap-filling
	// Check for iterative research mode first (new Deep Research 2.0 approach)
	// Deep Research 2.0 is now enabled by default; set iterative_research_enabled=false in context to disable
	iterativeResearchVersion := workflow.GetVersion(ctx, "iterative_research_v1", workflow.DefaultVersion, 1)
	iterativeEnabled := true // Default to true for Deep Research 2.0
	if v, ok := baseContext["iterative_research_enabled"]; ok {
		if b, ok := v.(bool); ok {
			iterativeEnabled = b
		}
	}

	if iterativeResearchVersion >= 1 && iterativeEnabled {
		// Deep Research 2.0: New iterative loop with coverage evaluation
		maxIterations := 3 // default
		if v, ok := baseContext["iterative_max_iterations"]; ok {
			switch t := v.(type) {
			case int:
				maxIterations = t
			case float64:
				maxIterations = int(t)
			}
			if maxIterations < 1 {
				maxIterations = 1
			}
			if maxIterations > 5 {
				maxIterations = 5
			}
		}

		logger.Info("Deep Research 2.0: Starting iterative research loop",
			"max_iterations", maxIterations,
		)

		// Track if gap-filling actually added new results
		gapFillingOccurred := false

		// Extract research dimensions from refinement
		var researchDimensions []activities.ResearchDimension
		if dims, ok := baseContext["research_dimensions"]; ok {
			if dimSlice, ok := dims.([]activities.ResearchDimension); ok {
				researchDimensions = dimSlice
			} else if dimSlice, ok := dims.([]interface{}); ok {
				// Convert from interface slice
				for _, d := range dimSlice {
					if dm, ok := d.(map[string]interface{}); ok {
						dim := activities.ResearchDimension{}
						if v, ok := dm["dimension"].(string); ok {
							dim.Dimension = v
						}
						if v, ok := dm["priority"].(string); ok {
							dim.Priority = v
						}
						if qs, ok := dm["questions"].([]interface{}); ok {
							for _, q := range qs {
								if s, ok := q.(string); ok {
									dim.Questions = append(dim.Questions, s)
								}
							}
						}
						if sts, ok := dm["source_types"].([]interface{}); ok {
							for _, st := range sts {
								if s, ok := st.(string); ok {
									dim.SourceTypes = append(dim.SourceTypes, s)
								}
							}
						}
						researchDimensions = append(researchDimensions, dim)
					}
				}
			}
		}

		currentSynthesis := synthesis.FinalResult
		currentKeyFindings := []string{}
		currentCoveredAreas := []string{}

		for iteration := 1; iteration <= maxIterations; iteration++ {
			// Check pause/cancel at start of each iteration
			if err := controlHandler.CheckPausePoint(ctx, fmt.Sprintf("pre_iteration_%d", iteration)); err != nil {
				return TaskResult{Success: false, ErrorMessage: err.Error()}, err
			}

			logger.Info("Deep Research 2.0: Iteration start",
				"iteration", iteration,
				"max_iterations", maxIterations,
			)

			// Step 3.5.1: Intermediate synthesis (combine results so far)
			if iteration > 1 && len(agentResults) > 0 {
				var intermediateSynthResult activities.IntermediateSynthesisResult
				err := workflow.ExecuteActivity(ctx,
					"IntermediateSynthesis",
					activities.IntermediateSynthesisInput{
						Query:             refinedQuery,
						Iteration:         iteration,
						MaxIterations:     maxIterations,
						AgentResults:      agentResults,
						PreviousSynthesis: currentSynthesis,
						CoverageGaps:      []string{}, // Will be filled by coverage evaluator
						Context:           baseContext,
						ParentWorkflowID:  input.ParentWorkflowID,
					}).Get(ctx, &intermediateSynthResult)

				if err == nil {
					currentSynthesis = intermediateSynthResult.Synthesis
					currentKeyFindings = intermediateSynthResult.KeyFindings
					currentCoveredAreas = intermediateSynthResult.CoverageAreas
					totalTokens += intermediateSynthResult.TokensUsed
					if intermediateSynthResult.TokensUsed > 0 || intermediateSynthResult.InputTokens > 0 || intermediateSynthResult.OutputTokens > 0 {
						inTok := intermediateSynthResult.InputTokens
						outTok := intermediateSynthResult.OutputTokens
						if inTok == 0 && outTok == 0 && intermediateSynthResult.TokensUsed > 0 {
							inTok = int(float64(intermediateSynthResult.TokensUsed) * 0.6)
							outTok = intermediateSynthResult.TokensUsed - inTok
						}
						recCtx := opts.WithTokenRecordOptions(ctx)
						_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
							UserID:       input.UserID,
							SessionID:    input.SessionID,
							TaskID:       workflowID,
							AgentID:      "intermediate_synthesis",
							Model:        intermediateSynthResult.ModelUsed,
							Provider:     intermediateSynthResult.Provider,
							InputTokens:  inTok,
							OutputTokens: outTok,
							Metadata:     map[string]interface{}{"phase": "intermediate_synthesis"},
						}).Get(recCtx, nil)
					}

					logger.Info("Deep Research 2.0: Intermediate synthesis complete",
						"iteration", iteration,
						"confidence", intermediateSynthResult.ConfidenceScore,
						"key_findings", len(currentKeyFindings),
					)

					// Early exit if confidence is high enough
					if intermediateSynthResult.ConfidenceScore >= 0.85 && !intermediateSynthResult.NeedsMoreResearch {
						logger.Info("Deep Research 2.0: High confidence achieved, exiting loop early",
							"iteration", iteration,
							"confidence", intermediateSynthResult.ConfidenceScore,
						)
						break
					}
				} else {
					logger.Warn("Deep Research 2.0: Intermediate synthesis failed", "error", err)
				}
			}

			// Check pause/cancel before coverage evaluation
			if err := controlHandler.CheckPausePoint(ctx, fmt.Sprintf("pre_coverage_eval_%d", iteration)); err != nil {
				return TaskResult{Success: false, ErrorMessage: err.Error()}, err
			}

			// Step 3.5.2: Evaluate coverage and identify gaps
			var coverageResult activities.CoverageEvaluationResult
			err := workflow.ExecuteActivity(ctx,
				"EvaluateCoverage",
				activities.CoverageEvaluationInput{
					Query:              refinedQuery,
					ResearchDimensions: researchDimensions,
					CurrentSynthesis:   currentSynthesis,
					CoveredAreas:       currentCoveredAreas,
					KeyFindings:        currentKeyFindings,
					Iteration:          iteration,
					MaxIterations:      maxIterations,
					Context:            baseContext,
					ParentWorkflowID:   input.ParentWorkflowID,
				}).Get(ctx, &coverageResult)

			if err != nil {
				logger.Warn("Deep Research 2.0: Coverage evaluation failed", "error", err)
				break // Exit loop on error
			}

			totalTokens += coverageResult.TokensUsed
			if coverageResult.TokensUsed > 0 || coverageResult.InputTokens > 0 || coverageResult.OutputTokens > 0 {
				inTok := coverageResult.InputTokens
				outTok := coverageResult.OutputTokens
				if inTok == 0 && outTok == 0 && coverageResult.TokensUsed > 0 {
					inTok = int(float64(coverageResult.TokensUsed) * 0.6)
					outTok = coverageResult.TokensUsed - inTok
				}
				recCtx := opts.WithTokenRecordOptions(ctx)
				_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
					UserID:       input.UserID,
					SessionID:    input.SessionID,
					TaskID:       workflowID,
					AgentID:      "coverage_evaluator",
					Model:        coverageResult.ModelUsed,
					Provider:     coverageResult.Provider,
					InputTokens:  inTok,
					OutputTokens: outTok,
					Metadata:     map[string]interface{}{"phase": "coverage_eval"},
				}).Get(recCtx, nil)
			}

			logger.Info("Deep Research 2.0: Coverage evaluation complete",
				"iteration", iteration,
				"overall_coverage", coverageResult.OverallCoverage,
				"critical_gaps", len(coverageResult.CriticalGaps),
				"should_continue", coverageResult.ShouldContinue,
				"recommended_action", coverageResult.RecommendedAction,
			)

			// Check if we should stop
			if !coverageResult.ShouldContinue || coverageResult.RecommendedAction == "complete" {
				logger.Info("Deep Research 2.0: Coverage sufficient, exiting loop",
					"iteration", iteration,
					"coverage", coverageResult.OverallCoverage,
				)
				break
			}

			// No critical gaps to fill - exit
			if len(coverageResult.CriticalGaps) == 0 && len(coverageResult.OptionalGaps) == 0 {
				logger.Info("Deep Research 2.0: No gaps identified, exiting loop",
					"iteration", iteration,
				)
				break
			}

			// Check pause/cancel before subquery generation
			if err := controlHandler.CheckPausePoint(ctx, fmt.Sprintf("pre_subquery_gen_%d", iteration)); err != nil {
				return TaskResult{Success: false, ErrorMessage: err.Error()}, err
			}

			// Step 3.5.3: Generate subqueries to fill gaps
			allGaps := append(coverageResult.CriticalGaps, coverageResult.OptionalGaps...)
			var subqueryResult activities.SubqueryGeneratorResult
			err = workflow.ExecuteActivity(ctx,
				"GenerateSubqueries",
				activities.SubqueryGeneratorInput{
					Query:            refinedQuery,
					CoverageGaps:     allGaps,
					CurrentSynthesis: currentSynthesis,
					Iteration:        iteration,
					MaxSubqueries:    3, // Limit to 3 per iteration
					Context:          baseContext,
					ParentWorkflowID: input.ParentWorkflowID,
					// Deep Research 2.0: Source-type aware search
					EntityName:      refineResult.CanonicalName,
					QueryType:       refineResult.QueryType,
					TargetLanguages: refineResult.TargetLanguages,
				}).Get(ctx, &subqueryResult)

			if err != nil {
				logger.Warn("Deep Research 2.0: Subquery generation failed", "error", err)
				break
			}

			totalTokens += subqueryResult.TokensUsed
			if subqueryResult.TokensUsed > 0 || subqueryResult.InputTokens > 0 || subqueryResult.OutputTokens > 0 {
				inTok := subqueryResult.InputTokens
				outTok := subqueryResult.OutputTokens
				if inTok == 0 && outTok == 0 && subqueryResult.TokensUsed > 0 {
					inTok = int(float64(subqueryResult.TokensUsed) * 0.6)
					outTok = subqueryResult.TokensUsed - inTok
				}
				recCtx := opts.WithTokenRecordOptions(ctx)
				_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
					UserID:       input.UserID,
					SessionID:    input.SessionID,
					TaskID:       workflowID,
					AgentID:      "subquery_generator",
					Model:        subqueryResult.ModelUsed,
					Provider:     subqueryResult.Provider,
					InputTokens:  inTok,
					OutputTokens: outTok,
					Metadata:     map[string]interface{}{"phase": "subquery_generation"},
				}).Get(recCtx, nil)
			}

			if len(subqueryResult.Subqueries) == 0 {
				logger.Info("Deep Research 2.0: No subqueries generated, exiting loop",
					"iteration", iteration,
				)
				break
			}

			logger.Info("Deep Research 2.0: Generated gap-filling subqueries",
				"iteration", iteration,
				"count", len(subqueryResult.Subqueries),
			)

			// Check pause/cancel before gap-filling execution
			if err := controlHandler.CheckPausePoint(ctx, fmt.Sprintf("pre_gap_filling_%d", iteration)); err != nil {
				return TaskResult{Success: false, ErrorMessage: err.Error()}, err
			}

			// Step 3.5.4: Execute gap-filling agents in parallel
			type gapAgentResult struct {
				Results []activities.AgentExecutionResult
				Tokens  int
			}
			gapChan := workflow.NewChannel(ctx)
			numGapAgents := len(subqueryResult.Subqueries)
			sem := workflow.NewSemaphore(ctx, 3) // Max 3 concurrent

			for _, sq := range subqueryResult.Subqueries {
				sq := sq // Capture
				workflow.Go(ctx, func(gctx workflow.Context) {
					if err := sem.Acquire(gctx, 1); err != nil {
						var empty gapAgentResult
						gapChan.Send(gctx, empty)
						return
					}
					defer sem.Release(1)

					// Build context with task contract
					gapContext := make(map[string]interface{})
					for k, v := range baseContext {
						gapContext[k] = v
					}
					gapContext["research_mode"] = "gap_fill"
					gapContext["target_gap"] = sq.TargetGap
					gapContext["iteration"] = iteration

					// Apply task contract if present
					if sq.SourceGuidance != nil {
						gapContext["source_guidance"] = map[string]interface{}{
							"required": sq.SourceGuidance.Required,
							"optional": sq.SourceGuidance.Optional,
							"avoid":    sq.SourceGuidance.Avoid,
						}

						// Generate search routing plan based on source guidance
						var routeResult activities.SearchRouteResult
						routeErr := workflow.ExecuteActivity(gctx,
							"RouteSearch",
							activities.SearchRouteInput{
								Query:       sq.Query,
								Dimension:   sq.TargetGap,
								SourceTypes: sq.SourceGuidance.Required,
								Priority:    "high", // Gap-filling is high priority
								Context:     gapContext,
							}).Get(gctx, &routeResult)

						if routeErr == nil && len(routeResult.Routes) > 0 {
							gapContext["search_routes"] = routeResult.Routes
							gapContext["search_strategy"] = routeResult.Strategy
							logger.Info("Deep Research 2.0: Search routing plan generated",
								"query", sq.Query,
								"routes", len(routeResult.Routes),
								"strategy", routeResult.Strategy,
							)
						} else {
							logger.Warn("Deep Research 2.0: Failed to generate search routing plan",
								"query", sq.Query,
								"error", routeErr,
							)
						}
					}
					if sq.OutputFormat != nil {
						gapContext["output_format"] = map[string]interface{}{
							"type":            sq.OutputFormat.Type,
							"required_fields": sq.OutputFormat.RequiredFields,
							"optional_fields": sq.OutputFormat.OptionalFields,
						}
					}
					if sq.SearchBudget != nil {
						gapContext["search_budget"] = map[string]interface{}{
							"max_queries": sq.SearchBudget.MaxQueries,
							"max_fetches": sq.SearchBudget.MaxFetches,
						}
					}
					if sq.Boundaries != nil {
						gapContext["boundaries"] = map[string]interface{}{
							"in_scope":     sq.Boundaries.InScope,
							"out_of_scope": sq.Boundaries.OutOfScope,
						}
					}

					// Execute agent with ReactLoop for deeper exploration
					reactConfig := patterns.ReactConfig{
						MaxIterations:     2,
						MinIterations:     1,
						ObservationWindow: 3,
						MaxObservations:   10,
						MaxThoughts:       5,
						MaxActions:        5,
					}
					reactOpts := patterns.Options{
						BudgetAgentMax: agentMaxTokens,
						SessionID:      input.SessionID,
						UserID:         input.UserID,
						ModelTier:      modelTier,
						Context:        gapContext,
					}

					reactResult, err := patterns.ReactLoop(
						gctx,
						sq.Query,
						gapContext,
						input.SessionID,
						[]string{},
						reactConfig,
						reactOpts,
					)

					payload := gapAgentResult{}
					if err == nil && len(reactResult.AgentResults) > 0 {
						payload.Results = reactResult.AgentResults
						payload.Tokens = reactResult.TotalTokens
					}
					gapChan.Send(gctx, payload)
				})
			}

			// Collect gap-filling results with value gating (P1-1)
			// Initialize URL tracking from existing agent results
			existingURLs := make(map[string]bool)
			existingDomains := make(map[string]bool)
			updateExistingURLs(agentResults, existingURLs, existingDomains)

			gapMetrics := GapFillMetrics{TotalAgents: numGapAgents}

			for i := 0; i < numGapAgents; i++ {
				var payload gapAgentResult
				gapChan.Receive(ctx, &payload)

				if len(payload.Results) > 0 {
					// Evaluate incremental value before accepting
					accept, newURLs, newDomains := evaluateGapFillValue(
						payload.Results, existingURLs, existingDomains,
					)

					if accept {
						agentResults = append(agentResults, payload.Results...)
						originalAgentResults = append(originalAgentResults, payload.Results...)
						totalTokens += payload.Tokens
						gapFillingOccurred = true

						gapMetrics.AcceptedAgents++
						gapMetrics.NewURLsFound += newURLs
						gapMetrics.NewDomainsFound += newDomains

						// Update tracking sets
						updateExistingURLs(payload.Results, existingURLs, existingDomains)
					} else {
						gapMetrics.SkippedLowValue++
						logger.Info("Gap-fill result skipped (low incremental value)",
							"new_urls", newURLs,
							"new_domains", newDomains,
							"agent_index", i,
						)
					}
				}

				// Early exit: saturation reached (enough evidence collected)
				if gapMetrics.NewDomainsFound >= 5 || gapMetrics.NewURLsFound >= 15 {
					logger.Info("Gap-fill saturation reached, stopping early",
						"new_domains", gapMetrics.NewDomainsFound,
						"new_urls", gapMetrics.NewURLsFound,
						"agents_processed", i+1,
						"agents_remaining", numGapAgents-i-1,
					)
					break
				}
			}

			logger.Info("Deep Research 2.0: Gap-filling iteration complete",
				"iteration", iteration,
				"total_agent_results", len(agentResults),
				"gap_agents_total", gapMetrics.TotalAgents,
				"gap_agents_accepted", gapMetrics.AcceptedAgents,
				"gap_agents_skipped", gapMetrics.SkippedLowValue,
				"new_urls_found", gapMetrics.NewURLsFound,
				"new_domains_found", gapMetrics.NewDomainsFound,
			)
		}

		// After iterative loop, re-collect citations (always) and re-synthesize (only if gap-filling occurred)
		if len(agentResults) > 0 {
			var resultsForCitations []interface{}
			for _, ar := range originalAgentResults {
				var toolExecs []interface{}
				for _, te := range ar.ToolExecutions {
					toolExecs = append(toolExecs, map[string]interface{}{
						"tool":    te.Tool,
						"success": te.Success,
						"output":  te.Output,
						"error":   te.Error,
					})
				}
				resultsForCitations = append(resultsForCitations, map[string]interface{}{
					"agent_id":        ar.AgentID,
					"tool_executions": toolExecs,
					"response":        ar.Response,
				})
			}

			now := workflow.Now(ctx)
			updatedCitations, _ := metadata.CollectCitations(resultsForCitations, now, 0)
			if len(updatedCitations) > 0 {
				// Apply entity-based filtering consistently (same as initial collection)
				canonicalName, _ := baseContext["canonical_name"].(string)
				if canonicalName != "" {
					var domains []string
					if d, ok := baseContext["official_domains"].([]string); ok {
						domains = d
					}
					var aliases []string
					if eq, ok := baseContext["exact_queries"].([]string); ok {
						aliases = eq
					}
					filterResult := ApplyCitationFilterWithFallback(updatedCitations, canonicalName, aliases, domains)
					updatedCitations = filterResult.Citations
					if filterResult.Applied {
						logger.Info("Gap-filling citation filter applied",
							"before", filterResult.Before,
							"after", filterResult.After,
							"retention", filterResult.Retention,
						)
					} else {
						logger.Warn("Gap-filling citation filter too aggressive, keeping original",
							"before", filterResult.Before,
							"filtered", filterResult.After,
							"retention", filterResult.Retention,
						)
					}
				}
				collectedCitations = updatedCitations

				// Update context with new citations
				var b strings.Builder
				for idx, c := range updatedCitations {
					title := c.Title
					if title == "" {
						title = c.Source
					}
					if c.PublishedDate != nil {
						fmt.Fprintf(&b, "[%d] %s (%s) - %s, %s\n", idx+1, title, c.URL, c.Source, c.PublishedDate.Format("2006-01-02"))
					} else {
						fmt.Fprintf(&b, "[%d] %s (%s) - %s\n", idx+1, title, c.URL, c.Source)
					}
				}
				baseContext["available_citations"] = strings.TrimRight(b.String(), "\n")
				baseContext["citation_count"] = len(updatedCitations)
			}

			// Only re-synthesize if gap-filling actually added new research results
			// Skip if no gaps were filled to avoid degrading already-good synthesis
			if gapFillingOccurred {
				var finalSynthesis activities.SynthesisResult
				err := workflow.ExecuteActivity(ctx,
					activities.SynthesizeResultsLLM,
					activities.SynthesisInput{
						Query:              input.Query,
						AgentResults:       agentResults,
						Context:            baseContext,
						CollectedCitations: collectedCitations,
						ParentWorkflowID:   input.ParentWorkflowID,
					}).Get(ctx, &finalSynthesis)

				if err == nil {
					synthesis = finalSynthesis
					totalTokens += finalSynthesis.TokensUsed
					logger.Info("Deep Research 2.0: Final synthesis complete (with gap-filling)",
						"total_citations", len(collectedCitations),
					)
				}
			} else {
				logger.Info("Deep Research 2.0: Skipping final re-synthesis (no gap-filling occurred)",
					"total_citations", len(collectedCitations),
				)
			}
		}

		logger.Info("Deep Research 2.0: Iterative loop complete",
			"total_tokens", totalTokens,
			"total_agents", len(agentResults),
		)

	} else {
		// Legacy gap-filling loop (backward compatibility)
		// Version-gated for safe rollout and Temporal determinism
		gapFillingVersion := workflow.GetVersion(ctx, "gap_filling_v1", workflow.DefaultVersion, 1)
		if gapFillingVersion >= 1 {
			// Check if gap_filling_enabled is explicitly set in context
			gapEnabled := true // default to enabled for backward compat
			gapEnabledExplicit := false
			if v, ok := baseContext["gap_filling_enabled"]; ok {
				gapEnabledExplicit = true
				if b, ok := v.(bool); ok {
					gapEnabled = b
				}
			}

			// If not explicitly set, use legacy strategy-based logic (backward compat)
			if !gapEnabledExplicit {
				strategy := ""
				if sv, ok := baseContext["research_strategy"].(string); ok {
					strategy = strings.ToLower(strings.TrimSpace(sv))
				}
				if strategy == "quick" {
					gapEnabled = false
					logger.Info("Gap-filling disabled for quick strategy (legacy logic)")
				}
			}

			// Skip gap-filling if disabled
			if !gapEnabled {
				logger.Info("Gap-filling disabled via configuration")
			} else {
				// Check iteration count from context (prevents infinite loops)
				iterationCount := 0
				if baseContext != nil {
					if v, ok := baseContext["gap_iteration"].(int); ok {
						iterationCount = v
					}
				}

				// Read max iterations from context with fallback to default and clamping
				maxGapIterations := 2 // default
				if v, ok := baseContext["gap_filling_max_iterations"]; ok {
					switch t := v.(type) {
					case int:
						maxGapIterations = t
					case float64:
						maxGapIterations = int(t)
					}
					// Clamp to reasonable range
					if maxGapIterations < 1 {
						maxGapIterations = 1
					}
					if maxGapIterations > 5 {
						maxGapIterations = 5
					}
				}

				// Only attempt gap-filling if we haven't exceeded max iterations
				if iterationCount < maxGapIterations {
					// Version gate for CJK gap detection phrases (for Temporal replay determinism)
					cjkGapPhrasesVersion := workflow.GetVersion(ctx, "cjk_gap_phrases_v1", workflow.DefaultVersion, 1)
					if cjkGapPhrasesVersion >= 1 {
						baseContext["enable_cjk_gap_phrases"] = true
					}

					// Strategy-aware gap detection (pass baseContext instead of strategy string)
					gapAnalysis := analyzeGaps(synthesis.FinalResult, refineResult.ResearchAreas, baseContext)

					if len(gapAnalysis.UndercoveredAreas) > 0 {
						logger.Info("Detected coverage gaps; triggering targeted re-search",
							"gaps", gapAnalysis.UndercoveredAreas,
							"iteration", iterationCount,
						)

						// Build targeted search queries for gaps
						gapQueries := buildGapQueries(gapAnalysis.UndercoveredAreas, input.Query)

						// Execute targeted searches in parallel using Temporal-safe channels
						var allGapResults []activities.AgentExecutionResult
						var gapTotalTokens int

						// Define payload type once (shared between send and receive)
						type gapResultPayload struct {
							Results []activities.AgentExecutionResult
							Tokens  int
						}

						// Use Temporal-safe channel to collect gap results
						gapResultsChan := workflow.NewChannel(ctx)
						numGapQueries := len(gapQueries)

						// Concurrency cap: limit in-flight gap searches to 3
						sem := workflow.NewSemaphore(ctx, 3)

						for _, gapQuery := range gapQueries {
							gapQuery := gapQuery // Capture for goroutine

							workflow.Go(ctx, func(gctx workflow.Context) {

								// Acquire a permit; on failure, send empty payload to keep counts balanced
								if err := sem.Acquire(gctx, 1); err != nil {
									var empty gapResultPayload
									gapResultsChan.Send(gctx, empty)
									return
								}
								defer sem.Release(1)

								gapContext := make(map[string]interface{})
								for k, v := range baseContext {
									gapContext[k] = v
								}
								gapContext["research_mode"] = "gap_fill"
								gapContext["target_area"] = gapQuery.TargetArea
								gapContext["gap_iteration"] = iterationCount + 1

								// Use react_max_iterations from context if provided, default to 2 for gap-filling efficiency
								gapReactMaxIterations := 2
								if v, ok := baseContext["react_max_iterations"]; ok {
									switch t := v.(type) {
									case int:
										gapReactMaxIterations = t
									case float64:
										gapReactMaxIterations = int(t)
									}
									// Clamp to reasonable range
									if gapReactMaxIterations < 1 {
										gapReactMaxIterations = 1
									}
									if gapReactMaxIterations > 10 {
										gapReactMaxIterations = 10
									}
								}

								reactConfig := patterns.ReactConfig{
									MaxIterations:     gapReactMaxIterations, // Respect react_max_iterations from strategy
									MinIterations:     2,
									ObservationWindow: 3,  // Keep last 3 observations in context
									MaxObservations:   20, // Prevent unbounded growth
									MaxThoughts:       10,
									MaxActions:        10,
								}
								reactOpts := patterns.Options{
									BudgetAgentMax: agentMaxTokens,
									SessionID:      input.SessionID,
									ModelTier:      modelTier,
									Context:        gapContext,
								}

								gapResult, err := patterns.ReactLoop(
									gctx,
									gapQuery.Query,
									gapContext,
									input.SessionID,
									[]string{}, // No history for gap queries
									reactConfig,
									reactOpts,
								)

								// Send result to channel
								payload := gapResultPayload{}
								if err == nil && len(gapResult.AgentResults) > 0 {
									payload.Results = gapResult.AgentResults
									payload.Tokens = gapResult.TotalTokens
								}
								gapResultsChan.Send(gctx, payload)
							})
						}

						// Collect all gap results from channel (Temporal-safe)
						for i := 0; i < numGapQueries; i++ {
							var payload gapResultPayload
							gapResultsChan.Receive(ctx, &payload)
							if len(payload.Results) > 0 {
								allGapResults = append(allGapResults, payload.Results...)
								gapTotalTokens += payload.Tokens
							}
						}
						totalTokens += gapTotalTokens

						// If we got new evidence, re-collect citations and re-synthesize
						if len(allGapResults) > 0 {
							logger.Info("Gap-filling search completed",
								"gap_results", len(allGapResults),
								"iteration", iterationCount+1,
							)

							// Combine for synthesis (filtered) and for citations (unfiltered)
							// Synthesis uses filtered agentResults to keep reasoning on-entity.
							// Citations use the original (unfiltered) results to maximize evidence.
							combinedAgentResults := append(allGapResults, agentResults...)
							combinedForCitations := append(allGapResults, originalAgentResults...)

							var resultsForCitations []interface{}
							for _, ar := range combinedForCitations {
								var toolExecs []interface{}
								if len(ar.ToolExecutions) > 0 {
									for _, te := range ar.ToolExecutions {
										toolExecs = append(toolExecs, map[string]interface{}{
											"tool":    te.Tool,
											"success": te.Success,
											"output":  te.Output,
											"error":   te.Error,
										})
									}
								}
								resultsForCitations = append(resultsForCitations, map[string]interface{}{
									"agent_id":        ar.AgentID,
									"tool_executions": toolExecs,
									"response":        ar.Response,
								})
							}

							now := workflow.Now(ctx)
							allCitations, _ := metadata.CollectCitations(resultsForCitations, now, 0) // Use 0 for default max (15)

							// Apply entity-based filtering consistently
							canonicalName, _ := baseContext["canonical_name"].(string)
							if canonicalName != "" && len(allCitations) > 0 {
								var domains []string
								if d, ok := baseContext["official_domains"].([]string); ok {
									domains = d
								}
								var aliases []string
								if eq, ok := baseContext["exact_queries"].([]string); ok {
									aliases = eq
								}
								filterResult := ApplyCitationFilterWithFallback(allCitations, canonicalName, aliases, domains)
								allCitations = filterResult.Citations
								if filterResult.Applied {
									logger.Info("Iterative gap-filling citation filter applied",
										"before", filterResult.Before,
										"after", filterResult.After,
										"retention", filterResult.Retention,
									)
								} else {
									logger.Warn("Iterative gap-filling citation filter too aggressive, keeping original",
										"before", filterResult.Before,
										"filtered", filterResult.After,
										"retention", filterResult.Retention,
									)
								}
							}

							if len(allCitations) > 0 {

								// Re-synthesize with augmented evidence
								var enhancedSynthesis activities.SynthesisResult

								// Build enhanced context with new citations
								enhancedContext := make(map[string]interface{})
								for k, v := range baseContext {
									enhancedContext[k] = v
								}
								enhancedContext["research_areas"] = refineResult.ResearchAreas
								enhancedContext["gap_iteration"] = iterationCount + 1
								// Inherit synthesis tier from initial synthesis (respects user override)
								if synthTier, ok := baseContext["model_tier"].(string); ok {
									enhancedContext["model_tier"] = synthTier
								}
								// Note: synthesis_model_tier override is already in baseContext, will be used

								// Format citations for synthesis
								if len(allCitations) > 0 {
									var b strings.Builder
									for idx, c := range allCitations {
										title := c.Title
										if title == "" {
											title = c.Source
										}
										if c.PublishedDate != nil {
											fmt.Fprintf(&b, "[%d] %s (%s) - %s, %s\n", idx+1, title, c.URL, c.Source, c.PublishedDate.Format("2006-01-02"))
										} else {
											fmt.Fprintf(&b, "[%d] %s (%s) - %s\n", idx+1, title, c.URL, c.Source)
										}
									}
									enhancedContext["available_citations"] = strings.TrimRight(b.String(), "\n")
									enhancedContext["citation_count"] = len(allCitations)

									// Also store structured citations for SSE emission
									out := make([]map[string]interface{}, 0, len(allCitations))
									for _, c := range allCitations {
										out = append(out, map[string]interface{}{
											"url":               c.URL,
											"title":             c.Title,
											"source":            c.Source,
											"credibility_score": c.CredibilityScore,
											"tool_source":       c.ToolSource,
											"status_code":       c.StatusCode,
											"blocked_reason":    c.BlockedReason,
											"quality_score":     c.QualityScore,
										})
									}
									enhancedContext["citations"] = out
								}

								err = workflow.ExecuteActivity(ctx,
									activities.SynthesizeResultsLLM,
									activities.SynthesisInput{
										Query:              input.Query,
										AgentResults:       combinedAgentResults, // Combined results with global dedup
										Context:            enhancedContext,
										CollectedCitations: allCitations,
										ParentWorkflowID:   input.ParentWorkflowID,
									}).Get(ctx, &enhancedSynthesis)

								if err == nil {
									synthesis = enhancedSynthesis
									collectedCitations = allCitations
									totalTokens += enhancedSynthesis.TokensUsed
									logger.Info("Gap-filling synthesis completed",
										"iteration", iterationCount+1,
										"total_citations", len(allCitations),
									)
								}
							}
						}
					} else {
						// Make it explicit that analysis ran but found nothing
						logger.Info("Gap analysis completed with no gaps detected",
							"iteration", iterationCount,
						)
					}
				}
			}
		}
	}

	// Check pause/cancel after synthesis/iteration - signal may have arrived during synthesis
	if err := controlHandler.CheckPausePoint(ctx, "post_synthesis"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Check pause/cancel before reflection
	if err := controlHandler.CheckPausePoint(ctx, "pre_reflection"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Step 4: Apply reflection pattern for quality improvement
	// NOTE: Reflection runs BEFORE citation agent to prevent re-synthesis from losing citations
	reflectionConfig := patterns.ReflectionConfig{
		Enabled:             true,
		MaxRetries:          2,
		ConfidenceThreshold: 0.8,
		Criteria:            []string{"accuracy", "completeness", "clarity"},
		TimeoutMs:           30000,
	}

	reflectionOpts := patterns.Options{
		BudgetAgentMax: agentMaxTokens,
		SessionID:      input.SessionID,
		ModelTier:      modelTier,
	}

	finalResult, qualityScore, reflectionTokens, err := patterns.ReflectOnResult(
		ctx,
		refinedQuery,
		synthesis.FinalResult,
		agentResults,
		baseContext,
		reflectionConfig,
		reflectionOpts,
	)

	if err != nil {
		logger.Warn("Reflection failed, using original result", "error", err)
		finalResult = synthesis.FinalResult
		qualityScore = 0.5
	}

	totalTokens += reflectionTokens

	// Check pause/cancel after reflection - signal may have arrived during reflection
	if err := controlHandler.CheckPausePoint(ctx, "post_reflection"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Citation Agent: add inline citations to synthesis result (when enabled)
	// IMPORTANT: Runs AFTER reflection to ensure citations aren't lost during re-synthesis
	citationAgentEnabled := true // Default: enabled
	if v, ok := baseContext["enable_citation_agent"].(bool); ok {
		citationAgentEnabled = v
	}
	if citationAgentEnabled && len(collectedCitations) > 0 {
		logger.Info("CitationAgent: starting citation addition",
			"total_citations", len(collectedCitations),
		)

		// Step: Remove ## Sources section before passing to Citation Agent
		// (FormatReportWithCitations in synthesis.go may have added it already)
		// The LLM may modify the Sources section (URL formatting, etc.), causing validation failure
		reportForCitation := finalResult // Use reflection output (was: synthesis.FinalResult)
		var extractedSources string
		if idx := strings.LastIndex(strings.ToLower(reportForCitation), "## sources"); idx != -1 {
			extractedSources = strings.TrimSpace(reportForCitation[idx:])
			reportForCitation = strings.TrimSpace(reportForCitation[:idx])
			logger.Info("CitationAgent: stripped Sources section before processing",
				"sources_length", len(extractedSources),
				"report_length", len(reportForCitation),
			)
		}

		var citationResult activities.CitationAgentResult
		citationCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 360 * time.Second, // Extended for long reports with medium tier
			RetryPolicy: &temporal.RetryPolicy{
				InitialInterval:    time.Second,
				BackoffCoefficient: 2.0,
				MaximumAttempts:    2,
			},
		})

		// P0-D: Metrics tracking for Citation V2 observability
		var citationMetrics struct {
			TotalCitations      int
			FetchOnlyCount      int
			ValidCount          int
			SupportedClaims     int
			TotalClaims         int
			V1SupplementEnabled bool
			V1Fallback          bool
			FinalCitationsUsed  int
		}
		citationMetrics.TotalCitations = len(collectedCitations)

		// ============================================================
		// Citation Agent: Add inline citations to report
		// Simplified flow: LLM directly adds [n] markers to report
		// ============================================================
		logger.Info("CitationAgent: starting citation flow",
			"total_citations", len(collectedCitations),
			"report_length", len(reportForCitation),
		)

		// Convert to CitationForAgent
		citationsForAgent := make([]activities.CitationForAgent, 0, len(collectedCitations))
		for _, c := range collectedCitations {
			citationsForAgent = append(citationsForAgent, activities.CitationForAgent{
				URL:              c.URL,
				Title:            c.Title,
				Source:           c.Source,
				Snippet:          c.Snippet,
				CredibilityScore: c.CredibilityScore,
				QualityScore:     c.QualityScore,
			})
		}

		// Dynamic model tier: use medium for longer reports (better instruction following)
		// Lowered threshold from 20000 to 8000 to improve citation success rate
		citationModelTier := "small"
		if len(reportForCitation) > 8000 {
			citationModelTier = "medium"
		}

		logger.Info("CitationAgent: model tier selected",
			"report_length", len(reportForCitation),
			"model_tier", citationModelTier,
		)

		citationErr := workflow.ExecuteActivity(citationCtx, "AddCitations", activities.CitationAgentInput{
			Report:           reportForCitation,
			Citations:        citationsForAgent,
			ParentWorkflowID: input.ParentWorkflowID,
			Context:          baseContext,
			ModelTier:        citationModelTier,
		}).Get(citationCtx, &citationResult)

		if citationErr != nil {
			logger.Warn("CitationAgent: failed, using original synthesis", "error", citationErr)
			_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: workflowID,
				EventType:  activities.StreamEventProgress,
				AgentID:    "citation_agent",
				Message:    "Citation injection skipped due to service error",
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil)
		} else if citationResult.ValidationPassed {
			// Use cited report and rebuild Sources section
			citationsList := ""
			if v, ok := baseContext["available_citations"].(string); ok {
				citationsList = v
			}
			if citationsList != "" {
				finalResult = formatting.FormatReportWithCitations(citationResult.CitedReport, citationsList)
			} else {
				finalResult = citationResult.CitedReport
				if extractedSources != "" {
					finalResult = strings.TrimSpace(finalResult) + "\n\n" + extractedSources
				}
			}
			totalTokens += citationResult.TokensUsed
			citationMetrics.FinalCitationsUsed = len(citationResult.CitationsUsed)
			logger.Info("CitationAgent: completed successfully",
				"citations_used", len(citationResult.CitationsUsed),
				"model_tier", citationModelTier,
				"validation_passed", true,
			)
		} else {
			// Validation failed - return original report unchanged
			logger.Warn("CitationAgent: validation failed, using original synthesis",
				"error", citationResult.ValidationError,
			)
			_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: workflowID,
				EventType:  activities.StreamEventProgress,
				AgentID:    "citation_agent",
				Message:    "Citation injection skipped due to validation failure",
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil)
		}

		// Log citation metrics summary
		logger.Info("CitationAgent: metrics summary",
			"total_citations", citationMetrics.TotalCitations,
			"final_citations_used", citationMetrics.FinalCitationsUsed,
			"model_tier", citationModelTier,
		)
	}

	// Check pause/cancel before verification
	if err := controlHandler.CheckPausePoint(ctx, "pre_verification"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Optional: verify claims if enabled and we have citations
	var verification activities.VerificationResult
	verifyEnabled := false
	if v, ok := baseContext["enable_verification"].(bool); ok {
		verifyEnabled = v
	}
	if verifyEnabled && len(collectedCitations) > 0 {
		// Convert citations to []interface{} of maps for VerifyClaimsActivity
		var verCitations []interface{}
		for _, c := range collectedCitations {
			m := map[string]interface{}{
				"url":               c.URL,
				"title":             c.Title,
				"source":            c.Source,
				"content":           c.Snippet,
				"credibility_score": c.CredibilityScore,
				"quality_score":     c.QualityScore,
			}
			verCitations = append(verCitations, m)
		}
		verr := workflow.ExecuteActivity(ctx, "VerifyClaimsActivity", activities.VerifyClaimsInput{
			Answer:    finalResult,
			Citations: verCitations,
		}).Get(ctx, &verification)
		if verr != nil {
			logger.Warn("Claim verification failed, skipping verification metadata", "error", verr)
		}
	}

	// Step 5.5: Optional fact extraction
	var extractedFacts *activities.FactExtractionResult
	factExtractionEnabled := false
	if v, ok := baseContext["enable_fact_extraction"].(bool); ok {
		factExtractionEnabled = v
	}
	if factExtractionEnabled && finalResult != "" {
		logger.Info("Running fact extraction on synthesis result")

		// Convert citations to simplified format
		var simpleCitations []activities.FactExtractionCitation
		for _, c := range collectedCitations {
			simpleCitations = append(simpleCitations, activities.FactExtractionCitation{
				URL:    c.URL,
				Title:  c.Title,
				Source: c.Source,
			})
		}

		// Extract research areas
		var areas []string
		if len(refineResult.ResearchAreas) > 0 {
			areas = refineResult.ResearchAreas
		}

		err = workflow.ExecuteActivity(ctx, "ExtractFacts", activities.FactExtractionInput{
			Query:            input.Query,
			SynthesisResult:  finalResult,
			Citations:        simpleCitations,
			ResearchAreas:    areas,
			ParentWorkflowID: input.ParentWorkflowID,
		}).Get(ctx, &extractedFacts)

		if err != nil {
			logger.Warn("Fact extraction failed", "error", err)
		} else {
			totalTokens += extractedFacts.TokensUsed
			if extractedFacts.TokensUsed > 0 || extractedFacts.InputTokens > 0 || extractedFacts.OutputTokens > 0 {
				inTok := extractedFacts.InputTokens
				outTok := extractedFacts.OutputTokens
				if inTok == 0 && outTok == 0 && extractedFacts.TokensUsed > 0 {
					inTok = int(float64(extractedFacts.TokensUsed) * 0.6)
					outTok = extractedFacts.TokensUsed - inTok
				}
				recCtx := opts.WithTokenRecordOptions(ctx)
				_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
					UserID:       input.UserID,
					SessionID:    input.SessionID,
					TaskID:       workflowID,
					AgentID:      "fact_extraction",
					Model:        extractedFacts.ModelUsed,
					Provider:     extractedFacts.Provider,
					InputTokens:  inTok,
					OutputTokens: outTok,
					Metadata:     map[string]interface{}{"phase": "fact_extraction"},
				}).Get(recCtx, nil)
			}
			logger.Info("Fact extraction complete",
				"fact_count", extractedFacts.FactCount,
				"high_confidence", extractedFacts.HighConfidenceFacts,
			)
		}
	}

	// Step 6: Update session and persist results
	// Session update moved to after usage report generation to ensure accurate cost/token tracking

	logger.Info("ResearchWorkflow completed successfully",
		"total_tokens", totalTokens,
		"quality_score", qualityScore,
		"agent_count", len(agentResults),
	)

	// Aggregate tool errors across agent results
	var toolErrors []map[string]string
	for _, ar := range agentResults {
		if len(ar.ToolExecutions) == 0 {
			continue
		}
		for _, te := range ar.ToolExecutions {
			if !te.Success || (te.Error != "") {
				toolErrors = append(toolErrors, map[string]string{
					"agent_id": ar.AgentID,
					"tool":     te.Tool,
					"error":    te.Error,
				})
			}
		}
	}

	meta := map[string]interface{}{
		"version":       "v2",
		"complexity":    decomp.ComplexityScore,
		"quality_score": qualityScore,
		"agent_count":   len(agentResults),
		"patterns_used": []string{"react", "parallel", "reflection"},
	}
	logger.Info("Preparing metadata", "collected_citations_count", len(collectedCitations))
	if len(collectedCitations) > 0 {
		// Export a light citation struct to metadata
		out := make([]map[string]interface{}, 0, len(collectedCitations))
		for _, c := range collectedCitations {
			out = append(out, map[string]interface{}{
				"url":               c.URL,
				"title":             c.Title,
				"source":            c.Source,
				"credibility_score": c.CredibilityScore,
				"quality_score":     c.QualityScore,
				"tool_source":       c.ToolSource,
				"status_code":       c.StatusCode,
				"blocked_reason":    c.BlockedReason,
			})
		}
		meta["citations"] = out
		logger.Info("Added citations to metadata", "count", len(out))
	}
	if verification.TotalClaims > 0 || verification.OverallConfidence > 0 {
		meta["verification"] = verification
	}
	if len(toolErrors) > 0 {
		meta["tool_errors"] = toolErrors
	}

	// Add extracted facts to metadata if available
	if extractedFacts != nil && len(extractedFacts.Facts) > 0 {
		// Convert facts to serializable format
		factsOut := make([]map[string]interface{}, 0, len(extractedFacts.Facts))
		for _, f := range extractedFacts.Facts {
			factsOut = append(factsOut, map[string]interface{}{
				"id":              f.ID,
				"statement":       f.Statement,
				"category":        f.Category,
				"confidence":      f.Confidence,
				"source_citation": f.SourceCitation,
				"entity_mentions": f.EntityMentions,
				"temporal_marker": f.TemporalMarker,
				"is_quantitative": f.IsQuantitative,
			})
		}
		meta["extracted_facts"] = factsOut
		meta["fact_summary"] = map[string]interface{}{
			"total_facts":         extractedFacts.FactCount,
			"high_confidence":     extractedFacts.HighConfidenceFacts,
			"categorized_facts":   extractedFacts.CategorizedFacts,
			"contradiction_count": extractedFacts.ContradictionCount,
		}
	}

	// Aggregate agent metadata (model, provider, tokens)
	agentMeta := metadata.AggregateAgentMetadata(agentResults, synthesis.TokensUsed+reflectionTokens)
	for k, v := range agentMeta {
		meta[k] = v
	}

	// Compute cost estimate from per-phase tokens using centralized pricing
	// Sum per-agent usage using splits; then add synthesis using model from synthesis result
	var estCost float64
	for _, ar := range agentResults {
		if ar.InputTokens > 0 || ar.OutputTokens > 0 {
			estCost += pricing.CostForSplit(ar.ModelUsed, ar.InputTokens, ar.OutputTokens)
		} else if ar.TokensUsed > 0 {
			estCost += pricing.CostForTokens(ar.ModelUsed, ar.TokensUsed)
		}
	}
	if synthesis.TokensUsed > 0 {
		inTok := synthesis.InputTokens
		outTok := synthesis.CompletionTokens
		if inTok == 0 && outTok > 0 {
			est := synthesis.TokensUsed - outTok
			if est > 0 {
				inTok = est
			}
		}
		if synthesis.ModelUsed != "" {
			if inTok > 0 || outTok > 0 {
				estCost += pricing.CostForSplit(synthesis.ModelUsed, inTok, outTok)
			} else {
				estCost += pricing.CostForTokens(synthesis.ModelUsed, synthesis.TokensUsed)
			}
		} else {
			estCost += pricing.CostForTokens("", synthesis.TokensUsed)
		}
	}
	if estCost > 0 {
		meta["cost_usd"] = estCost
	}

	// Finalize accurate cost by aggregating recorded token usage for this task
	// Ensures task_executions.total_cost_usd reflects sum of per-agent and synthesis usage
	var report *budget.UsageReport
	{
		err := workflow.ExecuteActivity(ctx, constants.GenerateUsageReportActivity, activities.UsageReportInput{
			// Aggregate by task_id across all user_id records to avoid partial sums
			UserID:    "",
			SessionID: input.SessionID,
			TaskID:    workflowID,
			// Time range left empty; activity defaults to last 24h
		}).Get(ctx, &report)
		if err == nil && report != nil {
			if report.TotalCostUSD > 0 {
				meta["cost_usd"] = report.TotalCostUSD
			}
			// Note: DB aggregation of token_usage provides the accurate full-workflow totals.
			// This is used by the API layer (service.go) when returning GetTaskStatus for terminal workflows.
		} else if err != nil {
			logger.Warn("Usage report aggregation failed", "error", err)
		}
	}

	// Step 5: Update session and persist results (Moved here to use accurate cost/token data)
	if input.SessionID != "" {
		// Use report values if available, otherwise fallback to local estimates
		finalCost := estCost
		finalTokens := totalTokens
		if report != nil {
			finalCost = report.TotalCostUSD
			finalTokens = report.TotalTokens
		}

		var updRes activities.SessionUpdateResult
		err = workflow.ExecuteActivity(ctx,
			constants.UpdateSessionResultActivity,
			activities.SessionUpdateInput{
				SessionID:  input.SessionID,
				Result:     finalResult,
				TokensUsed: finalTokens,
				AgentsUsed: len(agentResults),
				CostUSD:    finalCost, // Pass explicit cost to avoid default fallback
			}).Get(ctx, &updRes)
		if err != nil {
			logger.Error("Failed to update session", "error", err)
		}

		// Persist to vector store (await result to prevent race condition)
		_ = workflow.ExecuteActivity(ctx,
			activities.RecordQuery,
			activities.RecordQueryInput{
				SessionID: input.SessionID,
				UserID:    input.UserID,
				Query:     input.Query,
				Answer:    finalResult,
				Model:     modelTier,
				Metadata: map[string]interface{}{
					"workflow":      "research_flow_v2",
					"complexity":    decomp.ComplexityScore,
					"quality_score": qualityScore,
					"patterns_used": []string{"react", "parallel", "reflection"},
					"tenant_id":     input.TenantID,
				},
				RedactPII: true,
			}).Get(ctx, nil)
	}

	// Include synthesis finish_reason and requested_max_tokens for observability/debugging
	if synthesis.FinishReason != "" {
		meta["finish_reason"] = synthesis.FinishReason
	}
	if synthesis.RequestedMaxTokens > 0 {
		meta["requested_max_tokens"] = synthesis.RequestedMaxTokens
	}
	if synthesis.CompletionTokens > 0 {
		meta["completion_tokens"] = synthesis.CompletionTokens
	}
	if synthesis.EffectiveMaxCompletion > 0 {
		meta["effective_max_completion"] = synthesis.EffectiveMaxCompletion
	}

	// Check pause/cancel after verification - signal may have arrived during verification
	if err := controlHandler.CheckPausePoint(ctx, "post_verification"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Check pause/cancel before completion
	if err := controlHandler.CheckPausePoint(ctx, "pre_completion"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Emit final clean LLM_OUTPUT for OpenAI-compatible streaming.
	// Agent ID "final_output" signals the streamer to always show this content.
	if finalResult != "" {
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflowID,
			EventType:  activities.StreamEventLLMOutput,
			AgentID:    "final_output",
			Message:    finalResult,
			Timestamp:  workflow.Now(ctx),
			Payload: map[string]interface{}{
				"tokens_used": totalTokens,
			},
		}).Get(ctx, nil)
	}

	// Emit WORKFLOW_COMPLETED before returning
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowCompleted,
		AgentID:    "research",
		Message:    activities.MsgWorkflowCompleted(),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	return TaskResult{
		Result:     finalResult,
		Success:    true,
		TokensUsed: totalTokens,
		Metadata:   meta,
	}, nil
}

func buildTaskContractContext(subtask activities.Subtask) map[string]interface{} {
	contract := make(map[string]interface{})

	if subtask.OutputFormat != nil {
		contract["output_format"] = map[string]interface{}{
			"type":            subtask.OutputFormat.Type,
			"required_fields": subtask.OutputFormat.RequiredFields,
			"optional_fields": subtask.OutputFormat.OptionalFields,
		}
	}

	if subtask.SourceGuidance != nil {
		contract["source_guidance"] = map[string]interface{}{
			"required": subtask.SourceGuidance.Required,
			"optional": subtask.SourceGuidance.Optional,
			"avoid":    subtask.SourceGuidance.Avoid,
		}
	}

	if subtask.SearchBudget != nil {
		contract["search_budget"] = map[string]interface{}{
			"max_queries": subtask.SearchBudget.MaxQueries,
			"max_fetches": subtask.SearchBudget.MaxFetches,
		}
	}

	if subtask.Boundaries != nil {
		contract["boundaries"] = map[string]interface{}{
			"in_scope":     subtask.Boundaries.InScope,
			"out_of_scope": subtask.Boundaries.OutOfScope,
		}
	}

	if len(contract) == 0 {
		return nil
	}
	return contract
}

// GapAnalysis holds information about undercovered research areas
type GapAnalysis struct {
	UndercoveredAreas []string
}

// GapQuery represents a targeted search query for a gap area
type GapQuery struct {
	Query      string
	TargetArea string
}

// analyzeGaps detects which research areas are undercovered in the synthesis
// Reads configuration from context with strategy-based fallbacks for backward compatibility
func analyzeGaps(synthesisText string, researchAreas []string, context map[string]interface{}) GapAnalysis {
	gaps := GapAnalysis{
		UndercoveredAreas: []string{},
	}

	// Determine strategy for fallback defaults
	strategy := ""
	if sv, ok := context["research_strategy"].(string); ok {
		strategy = strings.ToLower(strings.TrimSpace(sv))
	}

	// Read from context with strategy-based fallbacks
	maxGaps := 3 // default for standard/unknown
	if v, ok := context["gap_filling_max_gaps"]; ok {
		switch t := v.(type) {
		case int:
			maxGaps = t
		case float64:
			maxGaps = int(t)
		}
		// Clamp to reasonable range
		if maxGaps < 1 {
			maxGaps = 1
		}
		if maxGaps > 20 {
			maxGaps = 20
		}
	} else {
		// Fallback to strategy-based defaults for backward compatibility
		switch strategy {
		case "deep":
			maxGaps = 2
		case "academic":
			maxGaps = 3
		default:
			maxGaps = 3 // standard or unknown
		}
	}

	checkCitationDensity := false // disabled by default (too aggressive)
	if v, ok := context["gap_filling_check_citations"]; ok {
		if b, ok := v.(bool); ok {
			checkCitationDensity = b
		}
	}
	// Citation density check disabled by default to avoid false positives
	// (well-written sections without citations shouldn't trigger gap-filling)

	for _, area := range researchAreas {
		// Stop if we've already found enough gaps
		if len(gaps.UndercoveredAreas) >= maxGaps {
			break
		}

		areaHeading := "### " + area
		idx := strings.Index(synthesisText, areaHeading)
		if idx == -1 {
			// Missing section heading - this is always a gap
			gaps.UndercoveredAreas = append(gaps.UndercoveredAreas, area)
			continue
		}

		// Extract section content
		content := synthesisText[idx+len(areaHeading):]
		nextSectionIdx := strings.Index(content, "\n### ") // Use ### for subsections
		if nextSectionIdx == -1 {
			nextSectionIdx = strings.Index(content, "\n## ") // Fallback to ## for main sections
		}
		if nextSectionIdx == -1 {
			nextSectionIdx = len(content)
		}
		sectionContent := strings.TrimSpace(content[:nextSectionIdx])

		// Check: Explicit gap indicator phrases (high precision only)
		gapPhrases := []string{
			"limited information available",
			"insufficient data",
			"not enough information",
			"no clear evidence",
			"data unavailable",
			"no information found",
		}

		// CJK gap detection phrases (version-gated for Temporal determinism)
		if enableCJK, ok := context["enable_cjk_gap_phrases"].(bool); ok && enableCJK {
			gapPhrases = append(gapPhrases,
				"未找到足够信息", // Chinese: not enough information found
				"数据不足",    // Chinese: insufficient data
				"信息不足",    // Chinese: insufficient information
				"情報不足",    // Japanese: information insufficient
				"情報が不足",   // Japanese: lacking information
				"정보가 부족",  // Korean: lacking information
			)
		}

		hasExplicitGap := false
		for _, phrase := range gapPhrases {
			if strings.Contains(strings.ToLower(sectionContent), phrase) {
				gaps.UndercoveredAreas = append(gaps.UndercoveredAreas, area)
				hasExplicitGap = true
				break
			}
		}

		// Citation density (only for deep/academic strategies and if no explicit gap found)
		if !hasExplicitGap && checkCitationDensity {
			citationCount := countInlineCitationsInSection(sectionContent)
			// Minimal rule: flag only if there are zero citations
			if citationCount == 0 {
				gaps.UndercoveredAreas = append(gaps.UndercoveredAreas, area)
			}
		}
	}

	return gaps
}

// buildGapQueries creates targeted queries for gap areas
func buildGapQueries(gaps []string, originalQuery string) []GapQuery {
	queries := make([]GapQuery, 0, len(gaps))
	for _, area := range gaps {
		queries = append(queries, GapQuery{
			Query:      fmt.Sprintf("Find detailed information about: %s (related to: %s)", area, originalQuery),
			TargetArea: area,
		})
	}
	return queries
}

// countInlineCitationsInSection counts unique inline citation references [n] in text
func countInlineCitationsInSection(text string) int {
	re := regexp.MustCompile(`\[\d+\]`)
	matches := re.FindAllString(text, -1)
	// Deduplicate (same citation can appear multiple times)
	seen := make(map[string]bool)
	for _, m := range matches {
		seen[m] = true
	}
	return len(seen)
}

// topologicalSort performs topological sort on subtasks based on dependencies
// Returns execution order (list of subtask IDs)
func topologicalSort(subtasks []activities.Subtask) []string {
	// Build adjacency list and in-degree map
	adjList := make(map[string][]string)
	inDegree := make(map[string]int)

	// Initialize all subtasks
	for _, st := range subtasks {
		if _, ok := inDegree[st.ID]; !ok {
			inDegree[st.ID] = 0
		}
		for _, dep := range st.Dependencies {
			adjList[dep] = append(adjList[dep], st.ID)
			inDegree[st.ID]++
		}
	}

	// Find all nodes with in-degree 0
	queue := make([]string, 0)
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	// Process queue
	result := make([]string, 0, len(subtasks))
	for len(queue) > 0 {
		// Pop first element
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		// Reduce in-degree of neighbors
		for _, neighbor := range adjList[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	// If result doesn't contain all subtasks, there's a cycle
	// Fall back to original order
	if len(result) != len(subtasks) {
		result = make([]string, 0, len(subtasks))
		for _, st := range subtasks {
			result = append(result, st.ID)
		}
	}

	return result
}

// persistAgentExecutionLocal persists agent execution results to the database.
// This is a fire-and-forget operation that won't fail the workflow.
// It's local to avoid circular imports with the workflows package.
func persistAgentExecutionLocal(ctx workflow.Context, workflowID, agentID, input string, result activities.AgentExecutionResult) {
	logger := workflow.GetLogger(ctx)

	// Use detached context for fire-and-forget persistence
	detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	detachedCtx = workflow.WithActivityOptions(detachedCtx, activityOpts)

	// Determine state based on success
	state := "COMPLETED"
	if !result.Success {
		state = "FAILED"
	}

	// Persist agent execution asynchronously
	workflow.ExecuteActivity(detachedCtx,
		activities.PersistAgentExecutionStandalone,
		activities.PersistAgentExecutionInput{
			WorkflowID: workflowID,
			AgentID:    agentID,
			Input:      input,
			Output:     result.Response,
			State:      state,
			TokensUsed: result.TokensUsed,
			ModelUsed:  result.ModelUsed,
			DurationMs: result.DurationMs,
			Error:      result.Error,
			Metadata: map[string]interface{}{
				"workflow": "research",
				"strategy": "react",
			},
		},
	)

	// Persist tool executions if any
	if len(result.ToolExecutions) > 0 {
		for _, tool := range result.ToolExecutions {
			// Convert tool output to string
			outputStr := ""
			if tool.Output != nil {
				switch v := tool.Output.(type) {
				case string:
					outputStr = v
				default:
					if jsonBytes, err := json.Marshal(v); err == nil {
						outputStr = string(jsonBytes)
					} else {
						outputStr = "complex output"
					}
				}
			}

			workflow.ExecuteActivity(
				detachedCtx,
				activities.PersistToolExecutionStandalone,
				activities.PersistToolExecutionInput{
					WorkflowID:     workflowID,
					AgentID:        agentID,
					ToolName:       tool.Tool,
					InputParams:    nil, // Tool execution from agent doesn't provide input params
					Output:         outputStr,
					Success:        tool.Success,
					TokensConsumed: 0,               // Not provided by agent
					DurationMs:     tool.DurationMs, // From agent-core proto
					Error:          tool.Error,
				},
			)
		}
	}

	logger.Debug("Agent execution persisted",
		"workflow_id", workflowID,
		"agent_id", agentID,
		"state", state,
	)
}

// persistAgentExecutionLocalWithMeta persists agent execution results with extra metadata.
// This extends persistAgentExecutionLocal to support phase/url/search_key tracking
// for domain_discovery and domain_prefetch agents.
func persistAgentExecutionLocalWithMeta(ctx workflow.Context, workflowID, agentID, input string, result activities.AgentExecutionResult, extraMeta map[string]interface{}) {
	logger := workflow.GetLogger(ctx)

	// Use detached context for fire-and-forget persistence
	detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	detachedCtx = workflow.WithActivityOptions(detachedCtx, activityOpts)

	// Determine state based on success
	state := "COMPLETED"
	if !result.Success {
		state = "FAILED"
	}

	// Merge base metadata with extra metadata
	metadata := map[string]interface{}{
		"workflow": "research",
		"strategy": "react",
	}
	for k, v := range extraMeta {
		metadata[k] = v
	}

	// Persist agent execution asynchronously
	workflow.ExecuteActivity(detachedCtx,
		activities.PersistAgentExecutionStandalone,
		activities.PersistAgentExecutionInput{
			WorkflowID: workflowID,
			AgentID:    agentID,
			Input:      input,
			Output:     result.Response,
			State:      state,
			TokensUsed: result.TokensUsed,
			ModelUsed:  result.ModelUsed,
			DurationMs: result.DurationMs,
			Error:      result.Error,
			Metadata:   metadata,
		},
	)

	// Persist tool executions if any
	if len(result.ToolExecutions) > 0 {
		for _, tool := range result.ToolExecutions {
			// Convert tool output to string
			outputStr := ""
			if tool.Output != nil {
				switch v := tool.Output.(type) {
				case string:
					outputStr = v
				default:
					if jsonBytes, err := json.Marshal(v); err == nil {
						outputStr = string(jsonBytes)
					} else {
						outputStr = "complex output"
					}
				}
			}

			workflow.ExecuteActivity(
				detachedCtx,
				activities.PersistToolExecutionStandalone,
				activities.PersistToolExecutionInput{
					WorkflowID:     workflowID,
					AgentID:        agentID,
					ToolName:       tool.Tool,
					InputParams:    nil,
					Output:         outputStr,
					Success:        tool.Success,
					TokensConsumed: 0,
					DurationMs:     tool.DurationMs,
					Error:          tool.Error,
				},
			)
		}
	}

	logger.Debug("Agent execution persisted with metadata",
		"workflow_id", workflowID,
		"agent_id", agentID,
		"state", state,
		"extra_meta", extraMeta,
	)
}

// simpleLogger is a minimal logger interface for validateTaskContracts
type simpleLogger interface {
	Info(msg string, keyvals ...interface{})
	Warn(msg string, keyvals ...interface{})
}

// validateTaskContracts validates decomposed subtasks against their declared contracts
// Returns an error if any contract violations are found
func validateTaskContracts(subtasks []activities.Subtask, logger simpleLogger) error {
	if len(subtasks) == 0 {
		logger.Warn("Task contract validation: no subtasks to validate")
		return nil // Not an error - allow fallback to simple execution
	}

	validSourceTypes := map[string]bool{
		"official": true, "aggregator": true, "news": true, "academic": true,
		"github": true, "financial": true, "documentation": true,
		"local_cn": true, "local_jp": true, "local_kr": true,
	}

	for i, task := range subtasks {
		// Validate required fields
		if task.ID == "" {
			return fmt.Errorf("subtask %d: missing required field 'id'", i)
		}
		if task.Description == "" {
			return fmt.Errorf("subtask %s: missing required field 'description'", task.ID)
		}

		// Validate source types if specified
		if task.SourceGuidance != nil {
			for _, sourceType := range task.SourceGuidance.Required {
				if !validSourceTypes[sourceType] {
					logger.Warn("Invalid source type in task contract",
						"task_id", task.ID,
						"source_type", sourceType,
						"valid_types", []string{"official", "aggregator", "news", "academic", "github", "financial", "documentation", "local_cn", "local_jp", "local_kr"},
					)
				}
			}
		}

		// Validate output format type is reasonable if specified
		if task.OutputFormat != nil && len(task.OutputFormat.Type) > 100 {
			logger.Warn("Suspiciously long output format type in task contract",
				"task_id", task.ID,
				"type_length", len(task.OutputFormat.Type),
			)
		}

		// Validate dependencies reference existing task IDs
		taskIDs := make(map[string]bool)
		for _, t := range subtasks {
			taskIDs[t.ID] = true
		}
		for _, dep := range task.Dependencies {
			if !taskIDs[dep] {
				return fmt.Errorf("subtask %s: dependency '%s' does not exist", task.ID, dep)
			}
		}
	}

	logger.Info("Task contract validation passed",
		"subtasks_validated", len(subtasks),
	)
	return nil
}

// Note: Post-synthesis language validation was removed.
// Language handling now occurs earlier (refine stage sets target_language),
// and the synthesis activity embeds a language instruction.

// ============================================================================
// Gap-filling Value Gating (P1-1)
// ============================================================================

// GapFillMetrics tracks gap-filling effectiveness
type GapFillMetrics struct {
	TotalAgents     int
	AcceptedAgents  int
	SkippedLowValue int
	NewURLsFound    int
	NewDomainsFound int
}

// extractURLsFromAgentResults extracts unique URLs from agent tool executions
func extractURLsFromAgentResults(results []activities.AgentExecutionResult) []string {
	urlSet := make(map[string]bool)
	var urls []string

	for _, r := range results {
		for _, te := range r.ToolExecutions {
			// Extract URLs from web_fetch and web_search tool inputs
			if te.Tool == "web_fetch" || te.Tool == "web_search" {
				if params, ok := te.InputParams.(map[string]interface{}); ok {
					if urlStr, ok := params["url"].(string); ok && urlStr != "" {
						if !urlSet[urlStr] {
							urlSet[urlStr] = true
							urls = append(urls, urlStr)
						}
					}
				}
			}

			// Also extract URLs from tool outputs (search results)
			if te.Tool == "web_search" && te.Success {
				if output, ok := te.Output.(map[string]interface{}); ok {
					if results, ok := output["results"].([]interface{}); ok {
						for _, result := range results {
							if resultMap, ok := result.(map[string]interface{}); ok {
								if urlStr, ok := resultMap["url"].(string); ok && urlStr != "" {
									if !urlSet[urlStr] {
										urlSet[urlStr] = true
										urls = append(urls, urlStr)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return urls
}

// evaluateGapFillValue determines if gap-fill results add sufficient incremental value
// Returns: (accept, newURLs, newDomains)
func evaluateGapFillValue(
	newResults []activities.AgentExecutionResult,
	existingURLs map[string]bool,
	existingDomains map[string]bool,
) (bool, int, int) {
	newURLs := 0
	newDomains := 0

	urls := extractURLsFromAgentResults(newResults)
	for _, u := range urls {
		if !existingURLs[u] {
			newURLs++
			domain, err := metadata.ExtractDomain(u)
			if err == nil && domain != "" && !existingDomains[domain] {
				newDomains++
			}
		}
	}

	// Value gate: accept if at least 2 new URLs OR 1 new domain
	accept := newURLs >= 2 || newDomains >= 1
	return accept, newURLs, newDomains
}

// updateExistingURLs adds URLs from results to the existing sets
func updateExistingURLs(
	results []activities.AgentExecutionResult,
	existingURLs map[string]bool,
	existingDomains map[string]bool,
) {
	urls := extractURLsFromAgentResults(results)
	for _, u := range urls {
		existingURLs[u] = true
		domain, err := metadata.ExtractDomain(u)
		if err == nil && domain != "" {
			existingDomains[domain] = true
		}
	}
}
