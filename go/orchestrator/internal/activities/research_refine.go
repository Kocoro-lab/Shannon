package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	ometrics "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
	"go.uber.org/zap"
)

var (
	urlRegex    = regexp.MustCompile(`https?://[^\s]+`)
	wwwRegex    = regexp.MustCompile(`www\.[^\s]+`)
	domainRegex = regexp.MustCompile(`(?i)\b(?:[a-z0-9][-a-z0-9]*\.)+[a-z]{2,}\b`)
)

// RefineResearchQueryInput is the input for refining vague research queries
type RefineResearchQueryInput struct {
	Query   string         `json:"query"`
	Context map[string]any `json:"context"`
}

// ResearchDimension represents a structured research area with source guidance
type ResearchDimension struct {
    Dimension   string   `json:"dimension"`              // Name of the research dimension (e.g., "Entity Identity")
    Questions   []string `json:"questions"`              // Specific questions to answer
    SourceTypes []string `json:"source_types"`           // Recommended source types (official, aggregator, news, academic)
    Priority    string   `json:"priority"`               // high, medium, low
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
    DetectedLanguage string `json:"detected_language,omitempty"` // Language detected from query
    // Entity disambiguation and search guidance
    CanonicalName      string   `json:"canonical_name,omitempty"`
    ExactQueries       []string `json:"exact_queries,omitempty"`
    OfficialDomains    []string `json:"official_domains,omitempty"`
    DisambiguationTerms []string `json:"disambiguation_terms,omitempty"`
    // Deep Research 2.0: Dynamic dimension generation
    QueryType           string              `json:"query_type,omitempty"`            // company, industry, scientific, comparative, exploratory
    ResearchDimensions  []ResearchDimension `json:"research_dimensions,omitempty"`   // Structured dimensions with source guidance
    LocalizationNeeded  bool                `json:"localization_needed,omitempty"`   // Whether to search in local languages
    TargetLanguages     []string            `json:"target_languages,omitempty"`      // Languages to search (e.g., ["en", "zh", "ja"])
    LocalizedNames      map[string][]string `json:"localized_names,omitempty"`       // Entity names in local languages
}

// RefineResearchQuery expands vague queries into structured research plans
// This is called before decomposition in ResearchWorkflow to clarify scope.
func (a *Activities) RefineResearchQuery(ctx context.Context, in RefineResearchQueryInput) (*RefineResearchQueryResult, error) {
	logger := activity.GetLogger(ctx)

	base := os.Getenv("LLM_SERVICE_URL")
	if base == "" {
		base = "http://llm-service:8000"
	}
	url := fmt.Sprintf("%s/agent/query", base)

	// Build prompt for query refinement with dynamic dimension generation
    refinementPrompt := fmt.Sprintf(`You are a research query expansion expert.

IMPORTANT: This is the PLANNING stage only. Plan first; do NOT start writing the final report or conducting searches. Return ONLY a structured plan.

Your task is to take a vague or broad query and expand it into a comprehensive research plan with structured dimensions.

Original query: %s

## Step 1: Classify the Query Type
Determine which category best fits:
- "company": Analysis of a specific organization (business, startup, corporation)
- "industry": Analysis of a market sector or industry trends
- "scientific": Scientific topic, technology, or research question
- "comparative": Comparison between entities (products, companies, technologies)
- "exploratory": Open-ended exploration of a topic or concept

## Step 2: Generate Research Dimensions
Based on the query type, create 4-7 research dimensions. Each dimension should have:
- A clear name
- 2-4 specific questions to answer
- Recommended source types: "official" (company sites, .gov, .edu), "aggregator" (crunchbase, wikipedia), "news" (recent articles), "academic" (papers, journals), "local_cn", "local_jp"
- Priority: "high", "medium", or "low"

### Dimension Templates by Query Type:

**Company Research:**
- Entity Identity (official, aggregator) - founding, leadership, location
- Business Model (official, news) - products, services, revenue model
- Market Position (aggregator, news) - competitors, market share
- Financial Performance (aggregator, news) - funding, revenue, growth
- Leadership & Team (official, aggregator, news) - founders, executives
- Recent Developments (news) - announcements, partnerships, launches

**Industry Research:**
- Industry Definition (aggregator, academic) - scope, segments
- Market Size & Growth (aggregator, news) - TAM, growth rates
- Key Players (aggregator, news) - major companies, market share
- Technology Trends (news, academic) - innovations, disruptions
- Challenges & Risks (news, academic) - barriers, regulatory

**Scientific Research:**
- Background & Context (academic, aggregator) - history, fundamentals
- Current State (academic, news) - latest findings, breakthroughs
- Key Researchers (academic, official) - leading labs, experts
- Applications (news, official) - practical uses, commercialization
- Open Questions (academic) - unsolved problems, future directions

**Comparative Research:**
- Entity Profiles (official, aggregator) - individual summaries
- Comparison Criteria (aggregator, news) - features, metrics
- Strengths & Weaknesses (news, aggregator) - pros/cons analysis
- Use Cases (news, official) - when to choose each

**Exploratory Research:**
- Core Concepts (aggregator, academic) - definitions, basics
- Historical Context (aggregator, academic) - evolution, milestones
- Current Landscape (news, aggregator) - state of affairs
- Expert Perspectives (news, academic) - opinions, debates
- Future Outlook (news, academic) - predictions, trends

## Step 3: Localization Assessment
If the entity has non-English presence (e.g., Chinese company, Japanese market), set:
- localization_needed: true
- target_languages: relevant language codes (e.g., ["en", "zh"] for Chinese companies)
- localized_names: entity names in those languages

## Step 4: Domain Discovery (IMPORTANT for companies)
For company research, identify ALL relevant domains including:
- Corporate domains (company name variations: acme.com, acme.co, acme.io, acme.ai)
- Product/brand domains (if company operates products under different names)
- Regional domains with local TLDs:
  - Global: .com, .co, .io, .ai
  - Japan: .jp, .co.jp (include BOTH)
  - China: .cn, .com.cn (include BOTH)
- Service-specific domains (e.g., app.acme.com, platform.acme.com)

Example: A company "Acme Corp" might operate a product called "AcmeCloud" with domains:
- acme.com, acmecorp.com, acme.ai (corporate)
- acmecloud.com, acmecloud.jp, acmecloud.co.jp, acmecloud.cn, acmecloud.com.cn (product brand sites)

## Output Format (JSON only, no prose):
{
  "refined_query": "...",
  "research_areas": ["...", "..."],
  "rationale": "...",
  "query_type": "company|industry|scientific|comparative|exploratory",
  "research_dimensions": [
    {
      "dimension": "Entity Identity",
      "questions": ["What is the official name?", "When was it founded?", "Who are the founders?"],
      "source_types": ["official", "aggregator"],
      "priority": "high"
    }
  ],
  "canonical_name": "...",
  "exact_queries": ["\"Acme Analytics\"", "\"Acme Analytics Inc.\""],
  "official_domains": ["acme.com", "acme.ai", "acme-product.jp", "acme-product.co.jp", "acme-product.cn"],
  "disambiguation_terms": ["software analytics", "Japan"],
  "localization_needed": false,
  "target_languages": ["en"],
  "localized_names": {}
}

Constraints:
- Do NOT include sources, URLs, or citations.
- Output JSON ONLY; no prose before/after.
- PRESERVE exact entity strings (do not split/normalize).
- Provide disambiguation terms to avoid entity mix-ups.`, in.Query)

    // Prepare request body. Role should be passed via context, not top-level.
    ctxMap := in.Context
    if ctxMap == nil {
        ctxMap = map[string]any{}
    }
    ctxMap["role"] = "research_refiner"
    // Request JSON-structured output when provider supports it; non-supporting providers will ignore
    ctxMap["response_format"] = map[string]any{"type": "json_object"}

    reqBody := map[string]any{
        "query":      refinementPrompt,
        "context":    ctxMap,
        "max_tokens": 8192, // Refinement produces structured JSON output; 4096 default can truncate
    }

	body, err := json.Marshal(reqBody)
    if err != nil {
        ometrics.RefinementErrors.Inc()
        return nil, fmt.Errorf("failed to marshal request: %w", err)
    }

	// HTTP client with workflow interceptor for tracing
	client := &http.Client{
		Timeout:   300 * time.Second,
		Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
	}

	start := time.Now()

	// Retry logic for transient failures (immediate retries, no backoff to avoid workflow non-determinism)
	// Note: Backoff delays should be handled at workflow level via RetryPolicy instead
	maxRetries := 3
	var resp *http.Response

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Create request for each attempt (body needs to be reset)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			ometrics.RefinementErrors.Inc()
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err = client.Do(req)
		if err != nil {
			logger.Warn("LLM service call failed",
				"attempt", attempt+1,
				"max_attempts", maxRetries+1,
				"error", err.Error(),
			)

			// Immediate retry for transient errors
			if attempt < maxRetries {
				continue
			}
			// Final attempt failed
			ometrics.RefinementErrors.Inc()
			return nil, fmt.Errorf("failed to call LLM service after %d attempts: %w", maxRetries+1, err)
		}

		// Check status code
		if resp.StatusCode >= 500 {
			// Server error - retry
			resp.Body.Close()
			logger.Warn("LLM service returned server error",
				"attempt", attempt+1,
				"status_code", resp.StatusCode,
			)

			if attempt < maxRetries {
				continue
			}
			// Final attempt failed
			ometrics.RefinementErrors.Inc()
			return nil, fmt.Errorf("LLM service returned status %d after %d attempts", resp.StatusCode, maxRetries+1)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			// Client error (4xx) - don't retry
			resp.Body.Close()
			ometrics.RefinementErrors.Inc()
			return nil, fmt.Errorf("LLM service returned status %d", resp.StatusCode)
		}

		// Success
		logger.Info("LLM service call succeeded",
			"attempt", attempt+1,
		)
		break
	}
	defer resp.Body.Close()

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
        // Deep Research 2.0 fields
        QueryType          string              `json:"query_type"`
        ResearchDimensions []ResearchDimension `json:"research_dimensions"`
        LocalizationNeeded bool                `json:"localization_needed"`
        TargetLanguages    []string            `json:"target_languages"`
        // Use interface{} to flexibly handle both map[string]string and map[string][]string
        LocalizedNames     interface{} `json:"localized_names"`
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

    // Detect language from original query
    detectedLang := detectLanguage(in.Query)

    // Validate language detection quality
    langConfidence := validateLanguageDetection(in.Query, detectedLang, logger)
    if langConfidence < 0.5 {
        logger.Warn("Low confidence in language detection - results may be unreliable",
            "detected_language", detectedLang,
            "confidence", langConfidence,
            "query", truncateStr(in.Query, 100),
        )
    }

    // Convert LocalizedNames from interface{} to map[string][]string
    // LLM may return either map[string]string or map[string][]string
    localizedNames := make(map[string][]string)
    if refinedData.LocalizedNames != nil {
        if rawMap, ok := refinedData.LocalizedNames.(map[string]interface{}); ok {
            for lang, val := range rawMap {
                switch v := val.(type) {
                case string:
                    // Single string: convert to single-element array
                    localizedNames[lang] = []string{v}
                case []interface{}:
                    // Array of values: convert to string array
                    strs := make([]string, 0, len(v))
                    for _, elem := range v {
                        if s, ok := elem.(string); ok {
                            strs = append(strs, s)
                        }
                    }
                    localizedNames[lang] = strs
                }
            }
        }
    }

    result := &RefineResearchQueryResult{
        OriginalQuery: in.Query,
        RefinedQuery:  refinedData.RefinedQuery,
        ResearchAreas: refinedData.ResearchAreas,
        Rationale:     refinedData.Rationale,
        TokensUsed:    llmResp.TokensUsed,
        ModelUsed:     llmResp.ModelUsed,
        Provider:      llmResp.Provider,
        DetectedLanguage: detectedLang,
        CanonicalName: refinedData.CanonicalName,
        ExactQueries:  refinedData.ExactQueries,
        OfficialDomains: refinedData.OfficialDomains,
        DisambiguationTerms: refinedData.DisambiguationTerms,
        // Deep Research 2.0 fields
        QueryType:          refinedData.QueryType,
        ResearchDimensions: refinedData.ResearchDimensions,
        LocalizationNeeded: refinedData.LocalizationNeeded,
        TargetLanguages:    refinedData.TargetLanguages,
        LocalizedNames:     localizedNames,
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

// removeURLs strips URLs/domains to reduce false English detections when the query contains links.
func removeURLs(text string) string {
	cleaned := urlRegex.ReplaceAllString(text, "")
	cleaned = wwwRegex.ReplaceAllString(cleaned, "")
	cleaned = domainRegex.ReplaceAllString(cleaned, "")
	return strings.TrimSpace(cleaned)
}

// detectLanguage performs simple heuristic language detection based on character ranges
func detectLanguage(query string) string {
    if query == "" {
        return "English"
    }

    cleanedQuery := removeURLs(query)
    // If URL/domain stripping leaves any text, prefer it even if it's short (e.g. "总结https://...").
    if strings.TrimSpace(cleanedQuery) != "" {
        query = cleanedQuery
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

    // Default to English for Latin scripts (most common for research queries)
    return "English"
}

// validateLanguageDetection returns a confidence score (0.0-1.0) for language detection
// and logs warnings if confidence is low
func validateLanguageDetection(query string, detectedLang string, logger log.Logger) float64 {
	if query == "" {
		return 0.0
	}

	cleanedQuery := removeURLs(query)
	if strings.TrimSpace(cleanedQuery) != "" {
		query = cleanedQuery
	}

	// Count characters by category
	var cjk, cyrillic, arabic, latin, other int
	for _, r := range query {
		switch {
		case r >= 0x4E00 && r <= 0x9FFF, r >= 0x3040 && r <= 0x309F, r >= 0x30A0 && r <= 0x30FF, r >= 0xAC00 && r <= 0xD7AF:
			cjk++
		case r >= 0x0400 && r <= 0x04FF:
			cyrillic++
		case r >= 0x0600 && r <= 0x06FF:
			arabic++
		case (r >= 0x0041 && r <= 0x005A) || (r >= 0x0061 && r <= 0x007A):
			latin++
		default:
			other++
		}
	}

	total := cjk + cyrillic + arabic + latin
	if total == 0 {
		logger.Warn("Language detection: no recognizable characters",
			"query_length", len(query),
			"detected", detectedLang,
		)
		return 0.3 // Low confidence for unusual input
	}

	// Calculate confidence based on character distribution
	var confidence float64
	switch detectedLang {
	case "Chinese", "Japanese", "Korean":
		cjkPercent := float64(cjk) / float64(total)
		confidence = cjkPercent
		if cjkPercent < 0.5 {
			logger.Warn("Language detection: low CJK percentage for CJK language",
				"detected", detectedLang,
				"cjk_percent", cjkPercent,
				"confidence", confidence,
			)
		}
	case "Russian":
		cyrillicPercent := float64(cyrillic) / float64(total)
		confidence = cyrillicPercent
		if cyrillicPercent < 0.5 {
			logger.Warn("Language detection: low Cyrillic percentage for Russian",
				"cyrillic_percent", cyrillicPercent,
				"confidence", confidence,
			)
		}
	case "Arabic":
		arabicPercent := float64(arabic) / float64(total)
		confidence = arabicPercent
		if arabicPercent < 0.5 {
			logger.Warn("Language detection: low Arabic percentage for Arabic",
				"arabic_percent", arabicPercent,
				"confidence", confidence,
			)
		}
	case "English", "Spanish", "French", "German":
		latinPercent := float64(latin) / float64(total)
		confidence = latinPercent
		// For Latin-script languages, we expect high Latin percentage
		if latinPercent < 0.7 {
			logger.Warn("Language detection: low Latin percentage for Latin-script language",
				"detected", detectedLang,
				"latin_percent", latinPercent,
				"confidence", confidence,
			)
		}
	default:
		confidence = 0.5 // Medium confidence for unknown language
		logger.Warn("Language detection: unknown language detected",
			"detected", detectedLang,
		)
	}

	// Warn if too many "other" characters (numbers, punctuation, special chars)
	if total > 0 && float64(other)/float64(total+other) > 0.5 {
		logger.Warn("Language detection: high proportion of non-linguistic characters",
			"other_percent", float64(other)/float64(total+other),
		)
		confidence *= 0.8 // Reduce confidence
	}

	return confidence
}
