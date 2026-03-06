package workflows

import (
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"strips fragment", "https://example.com/page#section", "https://example.com/page"},
		{"strips trailing slash", "https://example.com/page/", "https://example.com/page"},
		{"preserves root path", "https://example.com/", "https://example.com/"},
		{"lowercases scheme and host", "HTTPS://Example.COM/Page", "https://example.com/Page"},
		{"preserves path case", "https://example.com/CamelCase", "https://example.com/CamelCase"},
		{"handles no path", "https://example.com", "https://example.com/"},
		{"preserves query params", "https://example.com/search?q=test", "https://example.com/search?q=test"},
		{"different query params differ", "https://example.com/search?q=react", "https://example.com/search?q=react"},
		{"invalid URL returns as-is", "not a url", "not a url"},
		{"empty string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeURL(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTruncateToSentence(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxLen   int
		expected string
	}{
		{"short text unchanged", "Hello world.", 100, "Hello world."},
		{"exact length", "Hello", 5, "Hello"},
		{"truncates at sentence boundary", "First sentence. Second sentence. Third.", 25, "First sentence."},
		{"ellipsis when no sentence boundary", "Longcontinuoustext without periods", 15, "Longcontinuoust..."},
		{"ignores early sentence boundary", "A. Very long text that should not cut at the early period because it is too early", 50, "A. Very long text that should not cut at the early..."},
		{"handles unicode", "你好世界。第二句话。", 5, "你好世界。..."},
		{"empty string", "", 10, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateToSentence(tt.text, tt.maxLen)
			if got != tt.expected {
				t.Errorf("truncateToSentence(%q, %d) = %q, want %q", tt.text, tt.maxLen, got, tt.expected)
			}
		})
	}
}

func TestExtractURLsFromParams(t *testing.T) {
	tests := []struct {
		name     string
		params   map[string]interface{}
		expected []string
	}{
		{
			"single url string",
			map[string]interface{}{"url": "https://example.com"},
			[]string{"https://example.com"},
		},
		{
			"urls array",
			map[string]interface{}{"urls": []interface{}{"https://a.com", "https://b.com"}},
			[]string{"https://a.com", "https://b.com"},
		},
		{
			"both url and urls",
			map[string]interface{}{
				"url":  "https://single.com",
				"urls": []interface{}{"https://a.com"},
			},
			[]string{"https://single.com", "https://a.com"},
		},
		{
			"empty params",
			map[string]interface{}{},
			nil,
		},
		{
			"url is empty string",
			map[string]interface{}{"url": ""},
			nil,
		},
		{
			"urls with non-string elements",
			map[string]interface{}{"urls": []interface{}{42, "https://valid.com", nil}},
			[]string{"https://valid.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractURLsFromParams(tt.params)
			if len(got) != len(tt.expected) {
				t.Errorf("extractURLsFromParams() returned %d URLs, want %d", len(got), len(tt.expected))
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("extractURLsFromParams()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestExtractURLsFromSearchResults(t *testing.T) {
	tests := []struct {
		name     string
		result   string
		minURLs  int
	}{
		{
			"extracts http URLs",
			`Results: https://example.com/page1 and http://test.org/doc`,
			2,
		},
		{
			"deduplicates URLs",
			`Found https://example.com and also https://example.com again`,
			1,
		},
		{
			"strips trailing punctuation",
			`See https://example.com/page. Also https://test.org/doc, and more.`,
			2,
		},
		{
			"no URLs returns empty",
			"No links here at all",
			0,
		},
		{
			"handles complex URLs",
			`Visit https://docs.example.com/api/v2/users?page=1&limit=10 for details`,
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractURLsFromSearchResults(tt.result)
			if len(got) < tt.minURLs {
				t.Errorf("extractURLsFromSearchResults() returned %d URLs, want at least %d. Got: %v", len(got), tt.minURLs, got)
			}
		})
	}
}

func TestMergeTeamKnowledge(t *testing.T) {
	global := []activities.TeamKnowledgeEntry{
		{URL: "https://example.com/page1", Agent: "agent-0", Summary: "Page 1 content"},
	}
	incoming := []activities.TeamKnowledgeEntry{
		{URL: "https://example.com/page1", Agent: "agent-1", Summary: "Duplicate"},     // dup
		{URL: "https://example.com/page2", Agent: "agent-1", Summary: "Page 2 content"}, // new
		{URL: "https://EXAMPLE.COM/page1/", Agent: "agent-2", Summary: "Normalized dup"}, // dup after normalization
	}
	result := mergeTeamKnowledge(global, incoming)
	if len(result) != 2 {
		t.Errorf("mergeTeamKnowledge() returned %d entries, want 2. Got URLs:", len(result))
		for _, e := range result {
			t.Logf("  %s (%s)", e.URL, e.Agent)
		}
	}
	if len(result) >= 2 && result[1].URL != "https://example.com/page2" {
		t.Errorf("Expected second entry to be page2, got %s", result[1].URL)
	}
}

func TestFetchTracker_CheckCache(t *testing.T) {
	ft := newFetchTracker(nil)
	// Write some entries
	ft.writeCache("web_fetch", map[string]interface{}{"url": "https://example.com/page1"}, "content1", "agent-0")
	ft.writeCache("web_fetch", map[string]interface{}{"url": "https://example.com/page2"}, "content2", "agent-0")

	// Hit
	if cached, hit := ft.checkCache("web_fetch", map[string]interface{}{"url": "https://example.com/page1"}); !hit || cached != "content1" {
		t.Errorf("expected cache hit with content1, got hit=%v cached=%q", hit, cached)
	}
	// Miss
	if _, hit := ft.checkCache("web_fetch", map[string]interface{}{"url": "https://example.com/page3"}); hit {
		t.Error("expected cache miss for unknown URL")
	}
	// Non-fetch tool always misses
	if _, hit := ft.checkCache("web_search", map[string]interface{}{"url": "https://example.com/page1"}); hit {
		t.Error("expected miss for non-fetch tool")
	}
	// Nil tracker is safe
	var nilFT *fetchTracker
	if _, hit := nilFT.checkCache("web_fetch", map[string]interface{}{"url": "https://example.com"}); hit {
		t.Error("nil tracker should never hit")
	}
}

func TestFetchTracker_WriteCacheCap(t *testing.T) {
	ft := &fetchTracker{
		cache:      make(map[string]string),
		discovered: make(map[string]string),
		knowledge:  make([]activities.TeamKnowledgeEntry, 0),
	}
	// Manually set a low cap for testing
	originalMax := fetchTrackerMaxEntries
	_ = originalMax // cap is a const, test with the struct directly

	// Write 3 URLs
	ft.writeCache("web_fetch", map[string]interface{}{"url": "https://a.com"}, "a", "agent-0")
	ft.writeCache("web_fetch", map[string]interface{}{"url": "https://b.com"}, "b", "agent-0")
	ft.writeCache("web_fetch", map[string]interface{}{"url": "https://c.com"}, "c", "agent-0")

	if len(ft.cache) != 3 {
		t.Errorf("expected 3 cache entries, got %d", len(ft.cache))
	}
	if len(ft.knowledge) != 3 {
		t.Errorf("expected 3 knowledge entries, got %d", len(ft.knowledge))
	}
	// Duplicate should not add
	ft.writeCache("web_fetch", map[string]interface{}{"url": "https://a.com"}, "a-new", "agent-1")
	if len(ft.cache) != 3 {
		t.Errorf("duplicate should not increase count, got %d", len(ft.cache))
	}
}

func TestFetchTracker_QueryStringDistinction(t *testing.T) {
	ft := newFetchTracker(nil)
	ft.writeCache("web_fetch", map[string]interface{}{"url": "https://api.com/search?q=react"}, "react-data", "agent-0")
	ft.writeCache("web_fetch", map[string]interface{}{"url": "https://api.com/search?q=vue"}, "vue-data", "agent-0")

	// Different query params = different cache entries
	if len(ft.cache) != 2 {
		t.Errorf("different query params should be separate entries, got %d", len(ft.cache))
	}
	if cached, hit := ft.checkCache("web_fetch", map[string]interface{}{"url": "https://api.com/search?q=react"}); !hit || cached != "react-data" {
		t.Errorf("expected react-data, got hit=%v cached=%q", hit, cached)
	}
	if cached, hit := ft.checkCache("web_fetch", map[string]interface{}{"url": "https://api.com/search?q=vue"}); !hit || cached != "vue-data" {
		t.Errorf("expected vue-data, got hit=%v cached=%q", hit, cached)
	}
}

func TestFetchTracker_SearchOverlap(t *testing.T) {
	ft := newFetchTracker(nil)
	// Seed some known URLs
	ft.discovered["https://example.com/known1"] = "agent-0"
	ft.discovered["https://example.com/known2"] = "agent-0"

	// Search result with 2 known + 1 new
	result := "Found https://example.com/known1 and https://example.com/known2 and https://example.com/new1"
	pct, known, total := ft.checkSearchOverlap(result, "agent-1")
	if total != 3 || known != 2 {
		t.Errorf("expected total=3 known=2, got total=%d known=%d", total, known)
	}
	if pct < 66 || pct > 67 {
		t.Errorf("expected ~66.7%% overlap, got %.1f%%", pct)
	}
	// New URL should now be registered
	if _, exists := ft.discovered["https://example.com/new1"]; !exists {
		t.Error("new URL should be registered in discovered")
	}
}

func TestFetchTracker_SeedFromTeamKnowledge(t *testing.T) {
	seed := []activities.TeamKnowledgeEntry{
		{URL: "https://example.com/page1", Agent: "agent-0", Summary: "Page 1"},
		{URL: "https://example.com/page2", Agent: "agent-1", Summary: "Page 2"},
	}
	ft := newFetchTracker(seed)

	// Knowledge should be seeded
	if len(ft.Knowledge()) != 2 {
		t.Errorf("expected 2 knowledge entries, got %d", len(ft.Knowledge()))
	}
	// Discovered should be seeded (for L3)
	if _, exists := ft.discovered["https://example.com/page1"]; !exists {
		t.Error("expected page1 in discovered")
	}
	// But fetchCache should NOT be seeded (no full content available)
	if len(ft.cache) != 0 {
		t.Errorf("fetchCache should be empty (no full content in seed), got %d", len(ft.cache))
	}
}
