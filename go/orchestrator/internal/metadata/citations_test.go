package metadata

import (
    "fmt"
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "testing"
    "time"
)

func init() {
    // Set config path for tests to use the actual config file
    // Reset singleton FIRST before setting env var
    ResetCredibilityConfigForTest()

    // Resolve repo-root config robustly by walking up from this test file's directory
    // until a config/citation_credibility.yaml is found. Avoid absolute, machine-specific paths.
    if _, thisFile, _, ok := runtime.Caller(0); ok {
        dir := filepath.Dir(thisFile)
        for i := 0; i < 10; i++ { // walk up to repo root within reasonable bounds
            candidate := filepath.Join(dir, "config", "citation_credibility.yaml")
            if _, err := os.Stat(candidate); err == nil {
                os.Setenv("CITATION_CREDIBILITY_CONFIG", candidate)
                break
            }
            parent := filepath.Dir(dir)
            if parent == dir { // reached filesystem root
                break
            }
            dir = parent
        }
    }
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "basic URL",
			input:    "https://example.com/path",
			expected: "https://example.com/path",
			wantErr:  false,
		},
		{
			name:     "remove www prefix",
			input:    "https://www.example.com/path",
			expected: "https://example.com/path",
			wantErr:  false,
		},
		{
			name:     "remove trailing slash",
			input:    "https://example.com/path/",
			expected: "https://example.com/path",
			wantErr:  false,
		},
		{
			name:     "remove fragment",
			input:    "https://example.com/path#section",
			expected: "https://example.com/path",
			wantErr:  false,
		},
		{
			name:     "remove utm parameters",
			input:    "https://example.com/path?utm_source=google&utm_medium=cpc&id=123",
			expected: "https://example.com/path?id=123",
			wantErr:  false,
		},
		{
			name:     "remove fbclid",
			input:    "https://example.com/path?fbclid=xyz123",
			expected: "https://example.com/path",
			wantErr:  false,
		},
		{
			name:     "lowercase scheme and host",
			input:    "HTTPS://EXAMPLE.COM/Path",
			expected: "https://example.com/Path",
			wantErr:  false,
		},
		{
			name:     "strip root slash",
			input:    "https://example.com/",
			expected: "https://example.com",
			wantErr:  false,
		},
		{
			name:     "reject empty URL",
			input:    "",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "reject relative URL",
			input:    "/path/to/page",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "reject javascript scheme",
			input:    "javascript:alert(1)",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "reject ftp scheme",
			input:    "ftp://files.example.com/file.txt",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "accept http scheme",
			input:    "http://example.com/path",
			expected: "http://example.com/path",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NormalizeURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "simple domain",
			input:    "https://example.com/path",
			expected: "example.com",
			wantErr:  false,
		},
		{
			name:     "subdomain",
			input:    "https://blog.example.com/path",
			expected: "blog.example.com",
			wantErr:  false,
		},
		{
			name:     "remove www",
			input:    "https://www.example.com/path",
			expected: "example.com",
			wantErr:  false,
		},
		{
			name:     "with port",
			input:    "https://example.com:8080/path",
			expected: "example.com",
			wantErr:  false,
		},
		{
			name:     "mixed case",
			input:    "https://Example.COM/path",
			expected: "example.com",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractDomain(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestScoreQuality(t *testing.T) {
	now := time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		relevance     float64
		publishedDate *time.Time
		hasTitle      bool
		hasSnippet    bool
		expectedMin   float64
		expectedMax   float64
	}{
		{
			name:          "perfect recent article",
			relevance:     1.0,
			publishedDate: timePtr(now.AddDate(0, 0, -3)), // 3 days ago
			hasTitle:      true,
			hasSnippet:    true,
			expectedMin:   0.95,
			expectedMax:   1.0,
		},
		{
			name:          "good article, 2 weeks old",
			relevance:     0.9,
			publishedDate: timePtr(now.AddDate(0, 0, -14)), // 14 days ago
			hasTitle:      true,
			hasSnippet:    true,
			expectedMin:   0.93,
			expectedMax:   0.95,
		},
		{
			name:          "older article, 60 days",
			relevance:     0.8,
			publishedDate: timePtr(now.AddDate(0, 0, -60)), // 60 days ago
			hasTitle:      true,
			hasSnippet:    true,
			expectedMin:   0.77,
			expectedMax:   0.79,
		},
		{
			name:          "very old article, 180 days",
			relevance:     0.7,
			publishedDate: timePtr(now.AddDate(0, 0, -180)), // 180 days ago
			hasTitle:      true,
			hasSnippet:    true,
			expectedMin:   0.64,
			expectedMax:   0.66,
		},
		{
			name:          "no date information",
			relevance:     0.8,
			publishedDate: nil,
			hasTitle:      true,
			hasSnippet:    true,
			expectedMin:   0.68,
			expectedMax:   0.70,
		},
		{
			name:          "incomplete metadata",
			relevance:     0.8,
			publishedDate: nil,
			hasTitle:      false,
			hasSnippet:    false,
			expectedMin:   0.61,
			expectedMax:   0.63,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ScoreQuality(tt.relevance, tt.publishedDate, tt.hasTitle, tt.hasSnippet, now)
			if score < tt.expectedMin || score > tt.expectedMax {
				t.Errorf("expected score between %.3f and %.3f, got %.3f", tt.expectedMin, tt.expectedMax, score)
			}
		})
	}
}

func TestScoreCredibility(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		expected float64
	}{
		{
			name:     "edu domain",
			domain:   "mit.edu",
			expected: 0.85,
		},
		{
			name:     "gov domain",
			domain:   "nasa.gov",
			expected: 0.80,
		},
		{
			name:     "arxiv",
			domain:   "arxiv.org",
			expected: 0.85,
		},
		{
			name:     "nature",
			domain:   "nature.com",
			expected: 0.85,
		},
		{
			name:     "new york times",
			domain:   "nytimes.com",
			expected: 0.75,
		},
		{
			name:     "github",
			domain:   "github.com",
			expected: 0.75, // tech_documentation category in config
		},
		{
			name:     "twitter",
			domain:   "twitter.com",
			expected: 0.50,
		},
		{
			name:     "unknown domain",
			domain:   "random-blog.com",
			expected: 0.60,
		},
		{
			name:     "case insensitive",
			domain:   "MIT.EDU",
			expected: 0.85,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ScoreCredibility(tt.domain)
			if score != tt.expected {
				t.Errorf("expected %.2f, got %.2f", tt.expected, score)
			}
		})
	}
}

func TestCalculateSourceDiversity(t *testing.T) {
	tests := []struct {
		name     string
		citations []Citation
		expected float64
	}{
		{
			name:      "empty citations",
			citations: []Citation{},
			expected:  0.0,
		},
		{
			name: "all same domain",
			citations: []Citation{
				{Source: "example.com"},
				{Source: "example.com"},
				{Source: "example.com"},
			},
			expected: 0.333, // 1/3
		},
		{
			name: "all different domains",
			citations: []Citation{
				{Source: "example.com"},
				{Source: "test.com"},
				{Source: "demo.com"},
			},
			expected: 1.0, // 3/3
		},
		{
			name: "mixed diversity",
			citations: []Citation{
				{Source: "example.com"},
				{Source: "example.com"},
				{Source: "test.com"},
				{Source: "test.com"},
				{Source: "demo.com"},
			},
			expected: 0.6, // 3/5
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateSourceDiversity(tt.citations)
			// Allow small floating point differences
			if abs(result-tt.expected) > 0.01 {
				t.Errorf("expected %.3f, got %.3f", tt.expected, result)
			}
		})
	}
}

// Helper functions

func timePtr(t time.Time) *time.Time {
	return &t
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestCollectCitations(t *testing.T) {
	now := time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC)

	t.Run("empty results", func(t *testing.T) {
		citations, stats := CollectCitations([]interface{}{}, now, 15)
		if len(citations) != 0 {
			t.Errorf("expected 0 citations, got %d", len(citations))
		}
		if stats.TotalSources != 0 {
			t.Errorf("expected 0 total_sources, got %d", stats.TotalSources)
		}
	})

	t.Run("web_search results", func(t *testing.T) {
		results := []interface{}{
			map[string]interface{}{
				"agent_id": "researcher-1",
				"tool_executions": []interface{}{
					map[string]interface{}{
						"tool":    "web_search",
						"success": true,
						"output": map[string]interface{}{
							"results": []interface{}{
								map[string]interface{}{
									"url":     "https://arxiv.org/abs/2401.00001",
									"title":   "Quantum Computing Breakthrough",
									"text":    "A new quantum algorithm achieves...",
									"score":   0.95,
									"published_date": "2025-01-05T10:00:00Z",
								},
								map[string]interface{}{
									"url":   "https://nature.com/articles/quantum-2025",
									"title": "Nature: Quantum Entanglement Study",
									"text":  "Researchers discover...",
									"score": 0.90,
								},
							},
						},
					},
				},
			},
		}

		citations, stats := CollectCitations(results, now, 15)

		if len(citations) != 2 {
			t.Fatalf("expected 2 citations, got %d", len(citations))
		}

		// Check first citation (should be arxiv with higher score)
		if !strings.Contains(citations[0].URL, "arxiv.org") {
			t.Errorf("expected arxiv.org first, got %s", citations[0].URL)
		}
		if citations[0].CredibilityScore != 0.85 {
			t.Errorf("expected credibility 0.85 for arxiv, got %.2f", citations[0].CredibilityScore)
		}

		// Check stats
		if stats.TotalSources != 2 {
			t.Errorf("expected 2 sources, got %d", stats.TotalSources)
		}
		if stats.UniqueDomains != 2 {
			t.Errorf("expected 2 unique domains, got %d", stats.UniqueDomains)
		}
		if stats.SourceDiversity != 1.0 {
			t.Errorf("expected diversity 1.0, got %.2f", stats.SourceDiversity)
		}
	})

	t.Run("deduplication", func(t *testing.T) {
		results := []interface{}{
			map[string]interface{}{
				"agent_id": "researcher-1",
				"tool_executions": []interface{}{
					map[string]interface{}{
						"tool":    "web_search",
						"success": true,
						"output": map[string]interface{}{
							"results": []interface{}{
								map[string]interface{}{
									"url":   "https://example.com/article",
									"title": "Article 1",
									"score": 0.9,
								},
								map[string]interface{}{
									"url":   "https://example.com/article?utm_source=google",
									"title": "Article 1 Duplicate",
									"score": 0.8,
								},
								map[string]interface{}{
									"url":   "https://www.example.com/article/",
									"title": "Article 1 Another Duplicate",
									"score": 0.85,
								},
							},
						},
					},
				},
			},
		}

		citations, stats := CollectCitations(results, now, 15)

		// Should deduplicate to 1 citation
		if len(citations) != 1 {
			t.Errorf("expected 1 citation after deduplication, got %d", len(citations))
		}
		if stats.TotalSources != 1 {
			t.Errorf("expected 1 source after deduplication, got %d", stats.TotalSources)
		}
	})

	t.Run("diversity enforcement", func(t *testing.T) {
		// Create 5 results from same domain
		results := []interface{}{
			map[string]interface{}{
				"agent_id": "researcher-1",
				"tool_executions": []interface{}{
					map[string]interface{}{
						"tool":    "web_search",
						"success": true,
						"output": map[string]interface{}{
							"results": []interface{}{
								map[string]interface{}{
									"url":   "https://example.com/article1",
									"title": "Article 1",
									"score": 0.9,
								},
								map[string]interface{}{
									"url":   "https://example.com/article2",
									"title": "Article 2",
									"score": 0.85,
								},
								map[string]interface{}{
									"url":   "https://example.com/article3",
									"title": "Article 3",
									"score": 0.80,
								},
								map[string]interface{}{
									"url":   "https://example.com/article4",
									"title": "Article 4",
									"score": 0.75,
								},
								map[string]interface{}{
									"url":   "https://example.com/article5",
									"title": "Article 5",
									"score": 0.70,
								},
							},
						},
					},
				},
			},
		}

		citations, stats := CollectCitations(results, now, 15)

		// With max_per_domain=50 (config), all 5 citations from same domain are kept
		if len(citations) != 5 {
			t.Errorf("expected 5 citations (all same domain, within max_per_domain limit), got %d", len(citations))
		}
		// Diversity = unique_domains / total = 1 / 5 = 0.20
		if abs(stats.SourceDiversity-0.20) > 0.01 {
			t.Errorf("expected diversity ~0.20 (all same domain), got %.2f", stats.SourceDiversity)
		}

		// Should keep the highest scored ones
		if !strings.Contains(citations[0].URL, "article1") {
			t.Errorf("expected article1 (highest score) first, got %s", citations[0].URL)
		}
	})

	t.Run("web_fetch integration", func(t *testing.T) {
		results := []interface{}{
			map[string]interface{}{
				"agent_id": "researcher-1",
				"tool_executions": []interface{}{
					map[string]interface{}{
						"tool":    "web_fetch",
						"success": true,
						"output": map[string]interface{}{
							"url":     "https://anthropic.com/news/claude-3-5",
							"title":   "Claude 3.5 Announcement",
							"content": "Anthropic announces Claude 3.5 Sonnet...",
							"author":  "Anthropic Team",
							"published_date": "2024-06-20T10:00:00Z",
						},
					},
				},
			},
		}

		citations, _ := CollectCitations(results, now, 15)

		if len(citations) != 1 {
			t.Fatalf("expected 1 citation, got %d", len(citations))
		}

		// web_fetch should have high relevance (0.8)
		if citations[0].RelevanceScore != 0.8 {
			t.Errorf("expected relevance 0.8 for web_fetch, got %.2f", citations[0].RelevanceScore)
		}

		// Check snippet was extracted from content
		if citations[0].Snippet == "" {
			t.Error("expected snippet to be extracted from content")
		}
	})

	t.Run("mixed web_search and web_fetch", func(t *testing.T) {
		results := []interface{}{
			map[string]interface{}{
				"agent_id": "researcher-1",
				"tool_executions": []interface{}{
					map[string]interface{}{
						"tool":    "web_search",
						"success": true,
						"output": map[string]interface{}{
							"results": []interface{}{
								map[string]interface{}{
									"url":   "https://arxiv.org/abs/2401.00001",
									"title": "Quantum Research",
									"score": 0.95,
								},
							},
						},
					},
					map[string]interface{}{
						"tool":    "web_fetch",
						"success": true,
						"output": map[string]interface{}{
							"url":     "https://nature.com/article/quantum-2025",
							"title":   "Nature Article",
							"content": "Detailed content...",
						},
					},
				},
			},
		}

		citations, stats := CollectCitations(results, now, 15)

		if len(citations) != 2 {
			t.Fatalf("expected 2 citations, got %d", len(citations))
		}
		if stats.UniqueDomains != 2 {
			t.Errorf("expected 2 unique domains, got %d", stats.UniqueDomains)
		}
	})

	t.Run("plain text URL tag wrapper cleanup", func(t *testing.T) {
		results := []interface{}{
			map[string]interface{}{
				"agent_id": "researcher-1",
				"response": "Source: <url>https://waylandz.com/blog/tensor-logic-brain-like-architecture</url>",
			},
		}

		citations, _ := CollectCitations(results, now, 15)
		if len(citations) != 1 {
			t.Fatalf("expected 1 citation, got %d", len(citations))
		}
		if citations[0].URL != "https://waylandz.com/blog/tensor-logic-brain-like-architecture" {
			t.Errorf("expected cleaned URL, got %s", citations[0].URL)
		}
		if citations[0].Source != "waylandz.com" {
			t.Errorf("expected source waylandz.com, got %s", citations[0].Source)
		}
	})

	t.Run("plain text URL encoded tag suffix cleanup", func(t *testing.T) {
		results := []interface{}{
			map[string]interface{}{
				"agent_id": "researcher-1",
				"response": "Source: https://waylandz.com/blog/shannon-agentkit-alternative%3C/url",
			},
		}

		citations, _ := CollectCitations(results, now, 15)
		if len(citations) != 1 {
			t.Fatalf("expected 1 citation, got %d", len(citations))
		}
		if citations[0].URL != "https://waylandz.com/blog/shannon-agentkit-alternative" {
			t.Errorf("expected cleaned URL, got %s", citations[0].URL)
		}
		if citations[0].Source != "waylandz.com" {
			t.Errorf("expected source waylandz.com, got %s", citations[0].Source)
		}
	})

	t.Run("limit to max citations", func(t *testing.T) {
		// Create 20 search results
		searchResults := make([]interface{}, 20)
		for i := 0; i < 20; i++ {
			searchResults[i] = map[string]interface{}{
				"url":   fmt.Sprintf("https://example%d.com/article", i),
				"title": fmt.Sprintf("Article %d", i),
				"score": 0.9 - float64(i)*0.01,
			}
		}

		results := []interface{}{
			map[string]interface{}{
				"agent_id": "researcher-1",
				"tool_executions": []interface{}{
					map[string]interface{}{
						"tool":    "web_search",
						"success": true,
						"output": map[string]interface{}{
							"results": searchResults,
						},
					},
				},
			},
		}

		citations, _ := CollectCitations(results, now, 10)

		// Should limit to 10
		if len(citations) != 10 {
			t.Errorf("expected 10 citations (limit), got %d", len(citations))
		}

		// Should keep highest scored
		if !strings.Contains(citations[0].URL, "example0.com") {
			t.Errorf("expected example0.com (highest score) first, got %s", citations[0].URL)
		}
	})
}

func TestExtractCitationFromSearchResult(t *testing.T) {
	now := time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC)

	t.Run("valid result", func(t *testing.T) {
		result := map[string]interface{}{
			"url":            "https://arxiv.org/abs/2401.00001",
			"title":          "Quantum Computing",
			"text":           "Abstract...",
			"score":          0.95,
			"published_date": "2025-01-05T10:00:00Z",
		}

		citation, err := extractCitationFromSearchResult(result, "agent-1", now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if citation.Title != "Quantum Computing" {
			t.Errorf("expected title 'Quantum Computing', got %s", citation.Title)
		}
		if citation.RelevanceScore != 0.95 {
			t.Errorf("expected relevance 0.95, got %.2f", citation.RelevanceScore)
		}
		if citation.Source != "arxiv.org" {
			t.Errorf("expected source 'arxiv.org', got %s", citation.Source)
		}
	})

	t.Run("missing url", func(t *testing.T) {
		result := map[string]interface{}{
			"title": "Article without URL",
		}

		_, err := extractCitationFromSearchResult(result, "agent-1", now)
		if err == nil {
			t.Error("expected error for missing URL")
		}
	})
}

// safeTruncate safely truncates string for error messages
func safeTruncate(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen]) + "..."
}

func TestContainsLLMSignals(t *testing.T) {
	// Must be detected as pollution
	polluted := []struct {
		name string
		text string
	}{
		{"strong: XML residue", `</invoke></function_calls>`},
		{"strong: Chinese Agent template", `# PTMind 信息提取结果 我已成功从网站提取`},
		{"strong: Japanese Agent template", `サブページから抽出した情報`},
		{"json: multi-key leak", `{'title': 'Example', 'results': [...]}`},
		{"weak+extraction: Chinese combo", `让我提取一下关键信息`},
		{"weak+extraction: Japanese combo", `ツールで抽出しました`},
		{"strong: tool_executions", `Processing tool_executions from agent`},
		// Phase 3: Agent planning/meta-commentary
		{"strong: Agent planning", `**To obtain complete leadership roster:** Direct fetch of https://openai.com/about`},
		{"strong: Agent meta-comment", `these were not included in the initial subpage_fetch results`},
		{"strong: Agent reasoning", `would be needed (these pages were not fetched)`},
		// Phase 3: Structured field combination (≥2)
		{"structured: multi-field", "- **Industry:** Software\n- **Website:** https://example.com"},
		{"structured: three-field", "- **CEO:** John\n- **Founded:** 2020\n- **Revenue:** $10M"},
		// Phase 3.1: Generalized tool residue + Agent structured output
		{"strong: attempt_tag", `<attempt_web_search query="google products">`},
		{"strong: attempt_close", `</attempt_fetch>`},
		{"strong: URL field", `**URL:** https://alphabet.com/about - Main corporate page`},
		{"strong: Products header", `Key Business Products Identified from web search`},
	}

	// Must NOT be falsely flagged (normal web content)
	clean := []struct {
		name string
		text string
	}{
		{"normal English page", `## Company Overview PTMind was founded in 2010`},
		{"single weak signal no extraction", `I found this article helpful`},
		{"normal Chinese page", `以下是我们的产品介绍`},
		{"normal HTML", `<div class="content">Hello World</div>`},
		{"single JSON key", `{"title": "Normal API Doc"}`},
		{"normal Japanese page", `以下の情報をご確認ください`},
		{"web fetch article", `How to use web fetch API in JavaScript`},
		{"empty string", ``},
		// Phase 3: Single structured field should not trigger
		{"single field no trigger", `- **Industry:** Software Development and AI Research`},
		{"real profile page", "Our company profile:\n- **Industry:** Technology\nWe focus on innovation."},
	}

	for _, tc := range polluted {
		t.Run("polluted:"+tc.name, func(t *testing.T) {
			if !containsLLMSignals(tc.text) {
				t.Errorf("should detect pollution: %s", safeTruncate(tc.text, 40))
			}
		})
	}

	for _, tc := range clean {
		t.Run("clean:"+tc.name, func(t *testing.T) {
			if containsLLMSignals(tc.text) {
				t.Errorf("false positive: %s", safeTruncate(tc.text, 40))
			}
		})
	}
}

func TestEnsureSnippet(t *testing.T) {
	const minLen = MinSnippetLength // 30

	tests := []struct {
		name     string
		snippet  string
		content  string
		title    string
		url      string
		minLen   int
		expected string
	}{
		// Priority 1: Clean snippet of sufficient length
		{
			name:     "returns clean snippet when sufficient length",
			snippet:  "This is a valid snippet with more than thirty characters.",
			content:  "Some content here",
			title:    "Some Title",
			url:      "https://example.com",
			minLen:   minLen,
			expected: "This is a valid snippet with more than thirty characters.",
		},

		// Priority 1b: Polluted snippet should be cleared
		{
			name:     "clears polluted snippet and uses content",
			snippet:  "</invoke></function_calls> some polluted text here",
			content:  "Clean content that is more than thirty chars.",
			title:    "Title",
			url:      "https://example.com",
			minLen:   minLen,
			expected: "Clean content that is more than thirty chars.",
		},

		// Priority 2: Use content when snippet is too short
		{
			name:     "uses content when snippet too short",
			snippet:  "Short",
			content:  "This content is long enough to be used as snippet.",
			title:    "Title",
			url:      "https://example.com",
			minLen:   minLen,
			expected: "This content is long enough to be used as snippet.",
		},

		// Priority 2b: Skip polluted content
		{
			name:     "skips polluted content and uses title fallback",
			snippet:  "Short",
			content:  "信息提取结果 polluted content that should be skipped",
			title:    "Article Title",
			url:      "https://example.com/page",
			minLen:   minLen,
			expected: "Article Title - https://example.com/page",
		},

		// Priority 3: Title + URL fallback
		{
			name:     "uses title+url fallback when content too short",
			snippet:  "Short",
			content:  "Also short",
			title:    "Page Title",
			url:      "https://example.com/article",
			minLen:   minLen,
			expected: "Page Title - https://example.com/article",
		},

		// Priority 4: Return short but clean snippet
		{
			name:     "returns short clean snippet when no alternatives",
			snippet:  "Short",
			content:  "",
			title:    "",
			url:      "https://example.com",
			minLen:   minLen,
			expected: "Short",
		},

		// Edge case: Empty everything
		{
			name:     "returns empty when all inputs empty",
			snippet:  "",
			content:  "",
			title:    "",
			url:      "",
			minLen:   minLen,
			expected: "",
		},

		// Edge case: Polluted snippet with no fallbacks
		{
			name:     "returns empty when snippet polluted and no fallbacks",
			snippet:  "Direct fetch of https://example.com would be needed",
			content:  "",
			title:    "",
			url:      "",
			minLen:   minLen,
			expected: "",
		},

		// Edge case: Content truncation (MaxSnippetLength = 4000)
		{
			name:    "truncates long content to max length",
			snippet: "Short",
			content: strings.Repeat("A", MaxSnippetLength+500), // content longer than max
			title:   "Title",
			url:     "https://example.com",
			minLen:  minLen,
			// truncateRunes returns first MaxSnippetLength chars + "..."
			expected: strings.Repeat("A", MaxSnippetLength) + "...",
		},

		// Edge case: Unicode handling (30+ runes required)
		{
			name:     "handles unicode correctly in length check",
			snippet:  "日本語テキストが三十文字以上あります。これは有効なスニペットです。", // 32 runes
			content:  "Content",
			title:    "Title",
			url:      "https://example.com",
			minLen:   minLen,
			expected: "日本語テキストが三十文字以上あります。これは有効なスニペットです。",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ensureSnippet(tc.snippet, tc.content, tc.title, tc.url, tc.minLen)
			if result != tc.expected {
				t.Errorf("ensureSnippet() = %q, want %q", safeTruncate(result, 60), safeTruncate(tc.expected, 60))
			}
		})
	}
}

// Benchmark tests for containsLLMSignals to measure performance
// and guide optimization decisions (Priority 2 follow-up)

func BenchmarkContainsLLMSignals_CleanText(b *testing.B) {
	// Worst case: clean text requires checking all patterns
	text := `## Company Overview

	PTMind was founded in 2010 as a technology company focused on data analytics
	and marketing solutions. The company has grown to serve over 10,000 customers
	across Asia Pacific, with headquarters in Tokyo, Japan. Our flagship product
	Datadeck provides real-time business intelligence dashboards for enterprises.`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		containsLLMSignals(text)
	}
}

func BenchmarkContainsLLMSignals_StrongSignal(b *testing.B) {
	// Best case: strong signal found early
	text := `</invoke></function_calls> some additional text here`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		containsLLMSignals(text)
	}
}

func BenchmarkContainsLLMSignals_JSONKeys(b *testing.B) {
	// Moderate case: JSON key detection
	text := `{'title': 'Example Page', 'results': [{'url': 'https://example.com'}]}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		containsLLMSignals(text)
	}
}

func BenchmarkContainsLLMSignals_StructuredFields(b *testing.B) {
	// Moderate case: structured field combination detection
	text := `- **Industry:** Software Development
- **Website:** https://example.com
- **Founded:** 2010`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		containsLLMSignals(text)
	}
}

func BenchmarkContainsLLMSignals_WeakSignals(b *testing.B) {
	// Moderate case: weak signals with extraction keyword
	text := `Let me extract the information. I found several items. Based on my research...`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		containsLLMSignals(text)
	}
}

func BenchmarkContainsLLMSignals_LongText(b *testing.B) {
	// Stress test: long text (simulating real web content)
	text := strings.Repeat("This is some normal web content. ", 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		containsLLMSignals(text)
	}
}

func BenchmarkEnsureSnippet(b *testing.B) {
	snippet := "Short snippet"
	content := strings.Repeat("This is content. ", 50)
	title := "Page Title"
	url := "https://example.com/page"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ensureSnippet(snippet, content, title, url, MinSnippetLength)
	}
}

// ============================================================================
// isFallbackSnippet Tests
// ============================================================================

func TestIsFallbackSnippet(t *testing.T) {
	tests := []struct {
		name    string
		snippet string
		title   string
		url     string
		want    bool
	}{
		{
			name:    "empty snippet",
			snippet: "",
			title:   "Some Title",
			url:     "https://example.com",
			want:    true,
		},
		{
			name:    "exact fallback format",
			snippet: "Innovation and Entrepreneurship in Japan - https://fsi9-prod.s3.us-west-1.amazonaws.com/s3fs-public/innovation_and_entrepreneurship_in_japan.pdf",
			title:   "Innovation and Entrepreneurship in Japan",
			url:     "https://fsi9-prod.s3.us-west-1.amazonaws.com/s3fs-public/innovation_and_entrepreneurship_in_japan.pdf",
			want:    true,
		},
		{
			name:    "real content snippet",
			snippet: "Yuan Zheng founded Ptmind while still an undergrad at Nihon University. The company focuses on data analysis and monitoring.",
			title:   "Innovation and Entrepreneurship in Japan",
			url:     "https://fsi9-prod.s3.us-west-1.amazonaws.com/s3fs-public/innovation_and_entrepreneurship_in_japan.pdf",
			want:    false,
		},
		{
			name:    "short but real content",
			snippet: "Ptmind provides data analytics tools for enterprises.",
			title:   "Ptmind",
			url:     "https://ptmind.com",
			want:    false,
		},
		{
			name:    "title empty with real snippet",
			snippet: "Some real content about the topic.",
			title:   "",
			url:     "https://example.com",
			want:    false,
		},
		{
			name:    "simple domain fallback",
			snippet: "Example Site - https://example.com",
			title:   "Example Site",
			url:     "https://example.com",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isFallbackSnippet(tt.snippet, tt.title, tt.url)
			if got != tt.want {
				t.Errorf("isFallbackSnippet(%q, %q, %q) = %v, want %v",
					tt.snippet, tt.title, tt.url, got, tt.want)
			}
		})
	}
}

func TestFilterFallbackSnippets(t *testing.T) {
	citations := []Citation{
		{Title: "Real Article", URL: "https://example.com/article", Snippet: "This article discusses important findings about AI research."},
		{Title: "PDF Title", URL: "https://s3.amazonaws.com/doc.pdf", Snippet: "PDF Title - https://s3.amazonaws.com/doc.pdf"},
		{Title: "Another Real", URL: "https://news.com/story", Snippet: "Breaking news: major development in technology sector."},
		{Title: "Empty Snippet", URL: "https://blocked.com/page", Snippet: ""},
	}

	filtered := filterFallbackSnippets(citations)

	if len(filtered) != 2 {
		t.Errorf("filterFallbackSnippets: got %d citations, want 2", len(filtered))
	}
	if filtered[0].Title != "Real Article" {
		t.Errorf("first filtered citation: got %q, want %q", filtered[0].Title, "Real Article")
	}
	if filtered[1].Title != "Another Real" {
		t.Errorf("second filtered citation: got %q, want %q", filtered[1].Title, "Another Real")
	}
}
