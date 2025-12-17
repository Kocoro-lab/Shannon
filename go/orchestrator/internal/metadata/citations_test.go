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
			name:     "keep root slash",
			input:    "https://example.com/",
			expected: "https://example.com/",
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

		// Should limit to max 3 per domain
		if len(citations) != 3 {
			t.Errorf("expected 3 citations (max per domain), got %d", len(citations))
		}
		// Diversity = unique_domains / total = 1 / 3 = 0.33
		if abs(stats.SourceDiversity-0.333) > 0.01 {
			t.Errorf("expected diversity ~0.33 (all same domain), got %.2f", stats.SourceDiversity)
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
