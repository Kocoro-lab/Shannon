package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	"go.temporal.io/sdk/activity"
)

// LocalizedSearchInput is the input for a localized search across multiple languages
type LocalizedSearchInput struct {
	Query            string                 `json:"query"`
	EntityName       string                 `json:"entity_name,omitempty"`       // Primary entity being researched
	TargetLanguages  []string               `json:"target_languages"`            // e.g., ["zh", "ja", "ko"]
	LocalizedNames   map[string][]string    `json:"localized_names,omitempty"`   // Language -> names
	SourceTypes      []string               `json:"source_types,omitempty"`      // Source types to search
	MaxResultsPerLang int                   `json:"max_results_per_lang"`
	Context          map[string]interface{} `json:"context,omitempty"`
	ParentWorkflowID string                 `json:"parent_workflow_id,omitempty"`
}

// LocalizedSearchResult contains results from localized searches
type LocalizedSearchResult struct {
	Results          map[string][]LocalizedResult `json:"results"`           // Language -> results
	TotalResults     int                          `json:"total_results"`
	LanguagesCovered []string                     `json:"languages_covered"`
	TokensUsed       int                          `json:"tokens_used"`
	ModelUsed        string                       `json:"model_used"`
	Provider         string                       `json:"provider"`
}

// LocalizedResult represents a single result from a localized search
type LocalizedResult struct {
	URL           string  `json:"url"`
	Title         string  `json:"title"`
	Snippet       string  `json:"snippet"`
	Language      string  `json:"language"`
	PublishedDate string  `json:"published_date,omitempty"`
	Score         float64 `json:"score"`
	SourceType    string  `json:"source_type"`
}

// EntityLocalizationInput is the input for detecting entity name localizations
type EntityLocalizationInput struct {
	EntityName       string                 `json:"entity_name"`
	TargetLanguages  []string               `json:"target_languages"` // e.g., ["zh", "ja", "ko"]
	EntityType       string                 `json:"entity_type,omitempty"` // "company", "person", "product"
	Context          map[string]interface{} `json:"context,omitempty"`
	ParentWorkflowID string                 `json:"parent_workflow_id,omitempty"`
}

// EntityLocalizationResult contains detected name localizations
type EntityLocalizationResult struct {
	EntityName       string              `json:"entity_name"`
	LocalizedNames   map[string][]string `json:"localized_names"` // Language -> possible names
	Confidence       map[string]float64  `json:"confidence"`      // Language -> confidence
	TokensUsed       int                 `json:"tokens_used"`
	ModelUsed        string              `json:"model_used"`
	Provider         string              `json:"provider"`
	InputTokens      int                 `json:"input_tokens"`
	OutputTokens     int                 `json:"output_tokens"`
}

// DetectEntityLocalization detects localized names for an entity
func (a *Activities) DetectEntityLocalization(ctx context.Context, input EntityLocalizationInput) (*EntityLocalizationResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("DetectEntityLocalization: starting",
		"entity", input.EntityName,
		"target_languages", input.TargetLanguages,
		"entity_type", input.EntityType,
	)

	// Build prompt for entity localization detection
	systemPrompt := buildEntityLocalizationPrompt(input)
	userContent := buildEntityLocalizationContent(input)

	// Call LLM service
	llmServiceURL := getenvDefault("LLM_SERVICE_URL", "http://llm-service:8000")
	url := fmt.Sprintf("%s/agent/query", llmServiceURL)

	reqBody := map[string]interface{}{
		"query":       userContent,
		"max_tokens":  4096, // Extended for entity localization output
		"temperature": 0.1,
		"agent_id":    "entity_localization",
		"model_tier":  "small",
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
		Timeout:   120 * time.Second, // Extended for LLM processing time
		Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(reqJSON)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "entity_localization")
	if input.ParentWorkflowID != "" {
		req.Header.Set("X-Workflow-ID", input.ParentWorkflowID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LLM service call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d from LLM service", resp.StatusCode)
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
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	result := &EntityLocalizationResult{
		EntityName:     input.EntityName,
		LocalizedNames: make(map[string][]string),
		Confidence:     make(map[string]float64),
		TokensUsed:     llmResp.TokensUsed,
		ModelUsed:      llmResp.ModelUsed,
		Provider:       llmResp.Provider,
		InputTokens:    llmResp.Metadata.InputTokens,
		OutputTokens:   llmResp.Metadata.OutputTokens,
	}

	// Parse structured response
	if err := parseEntityLocalizationResponse(llmResp.Response, result); err != nil {
		logger.Warn("DetectEntityLocalization: failed to parse structured response",
			"error", err,
		)
		// Fallback: use original name for all languages
		for _, lang := range input.TargetLanguages {
			result.LocalizedNames[lang] = []string{input.EntityName}
			result.Confidence[lang] = 0.3
		}
	}

	logger.Info("DetectEntityLocalization: complete",
		"languages_detected", len(result.LocalizedNames),
	)

	return result, nil
}

// buildEntityLocalizationPrompt creates the system prompt
func buildEntityLocalizationPrompt(input EntityLocalizationInput) string {
	var sb strings.Builder

	sb.WriteString(`You are a multilingual entity name expert. Your task is to identify localized versions
of entity names for targeted web searches.

## Your Goals:
1. Identify official/common names in each target language
2. Include transliterations, translations, and local variants
3. Provide confidence levels for each localization
4. Distinguish between verified names and guesses

## Guidelines:
- For companies: Look for official local subsidiaries, common press names
- For products: Consider local product names, translations, transliterations
- For people: Use standard romanization/transliteration conventions
- Include multiple variants if commonly used
- Higher confidence for official names, lower for guesses

## Response Format:
Return a JSON object:
{
  "localizations": {
    "zh": {
      "names": ["Chinese Name 1", "Chinese Name 2"],
      "confidence": 0.9,
      "notes": "Official Chinese subsidiary name"
    },
    "ja": {
      "names": ["Japanese Name"],
      "confidence": 0.7,
      "notes": "Common press transliteration"
    }
  }
}
`)

	return sb.String()
}

// buildEntityLocalizationContent builds user content
func buildEntityLocalizationContent(input EntityLocalizationInput) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Entity Name: %s\n", input.EntityName))

	if input.EntityType != "" {
		sb.WriteString(fmt.Sprintf("## Entity Type: %s\n", input.EntityType))
	}

	sb.WriteString(fmt.Sprintf("## Target Languages: %s\n\n", strings.Join(input.TargetLanguages, ", ")))

	sb.WriteString("Please identify localized versions of this entity name for each target language.\n")
	sb.WriteString("Include:\n")
	sb.WriteString("- Official local names (subsidiaries, translations)\n")
	sb.WriteString("- Common press/media names\n")
	sb.WriteString("- Standard transliterations\n")
	sb.WriteString("- Any popular alternative spellings\n")

	return sb.String()
}

// parseEntityLocalizationResponse parses the LLM response
func parseEntityLocalizationResponse(response string, result *EntityLocalizationResult) error {
	// Find JSON in response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")

	if start == -1 || end == -1 || end <= start {
		return fmt.Errorf("no JSON object found in response")
	}

	jsonStr := response[start : end+1]

	var parsed struct {
		Localizations map[string]struct {
			Names      []string `json:"names"`
			Confidence float64  `json:"confidence"`
			Notes      string   `json:"notes"`
		} `json:"localizations"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	for lang, loc := range parsed.Localizations {
		result.LocalizedNames[lang] = loc.Names
		result.Confidence[lang] = loc.Confidence
	}

	return nil
}

// GetRegionalSourceSites returns recommended sites for a specific language/region
func GetRegionalSourceSites(language string) []string {
	// Default regional sites for common languages
	regionalSites := map[string][]string{
		"zh": {
			"36kr.com",        // Chinese tech news
			"iyiou.com",       // Chinese industry analysis
			"tianyancha.com",  // Chinese company data
			"pedaily.cn",      // Chinese investment news
			"chinaventure.com.cn",
			"baike.baidu.com", // Chinese Wikipedia
		},
		"ja": {
			"nikkei.com",      // Japanese business news
			"prtimes.jp",      // Japanese press releases
			"startup-db.com",  // Japanese startup database
			"initial.inc",     // Japanese startup platform
			"bridgej.com",     // Japanese tech news
		},
		"ko": {
			"platum.kr",       // Korean startup news
			"thevc.kr",        // Korean VC news
			"venturesquare.net", // Korean startup community
			"besuccess.com",   // Korean tech news
		},
		"de": {
			"handelsblatt.com", // German business
			"gruenderszene.de", // German startups
			"t3n.de",          // German tech
		},
		"fr": {
			"lesechos.fr",     // French business
			"maddyness.com",   // French startups
			"journaldunet.com", // French tech
		},
	}

	if sites, ok := regionalSites[language]; ok {
		return sites
	}
	return nil
}
