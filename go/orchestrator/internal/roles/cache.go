package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
)

// presetPayload mirrors the llm-service /roles response schema.
type presetPayload struct {
	AllowedTools []string `json:"allowed_tools"`
}

var (
	roleAllowlist = map[string][]string{
		// Fallback allowlist used when llm-service /roles is unavailable.
		// Keep this aligned with python/llm-service/llm_service/roles/presets.py.
		"analysis":            {"web_search", "file_read"},
		"research":            {"web_search", "web_fetch", "web_subpage_fetch", "web_crawl"},
		"deep_research_agent": {"web_search", "web_fetch", "web_subpage_fetch", "web_crawl"},
		"writer":              {"file_read"},
		"critic":              {"file_read", "file_list", "bash"},
		"generalist":          {"file_read", "file_list", "bash", "web_search"},
		"research_refiner":    {},
		"developer":           {"file_read", "file_write", "file_list", "bash", "python_executor"},
		"ads_research": {
			"ads_serp_extract",
			"ads_transparency_search",
			"ads_competitor_discover",
			"ads_creative_analyze",
			"lp_visual_analyze",
			"lp_batch_analyze",
			"web_search",
			"web_fetch",
		},
		"browser_use": {
			"browser",
			"web_search",
		},
	}
	roleAllowlistOnce sync.Once
	roleAllowlistMu   sync.RWMutex
)

// ensureLoaded populates the allowlist from llm-service the first time it's requested.
func ensureLoaded() {
	roleAllowlistOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if fetched, err := fetchRoleAllowlist(ctx); err == nil && len(fetched) > 0 {
			roleAllowlistMu.Lock()
			roleAllowlist = fetched
			roleAllowlistMu.Unlock()
		}
	})
}

// fetchRoleAllowlist retrieves role metadata from llm-service.
func fetchRoleAllowlist(ctx context.Context) (map[string][]string, error) {
	base := os.Getenv("LLM_SERVICE_URL")
	if base == "" {
		base = "http://llm-service:8000"
	}
	url := fmt.Sprintf("%s/roles", base)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout:   2 * time.Second,
		Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("llm-service /roles returned status %d", resp.StatusCode)
	}

	var payload map[string]presetPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	out := make(map[string][]string, len(payload))
	for role, cfg := range payload {
		lowerRole := strings.ToLower(role)
		out[lowerRole] = cfg.AllowedTools
	}
	return out, nil
}

// AllowedTools returns the tool allowlist for a given role (case-insensitive).
func AllowedTools(role string) []string {
	if role == "" {
		return nil
	}
	ensureLoaded()

	roleAllowlistMu.RLock()
	defer roleAllowlistMu.RUnlock()
	tools, ok := roleAllowlist[strings.ToLower(role)]
	if !ok || len(tools) == 0 {
		return nil
	}
	return append([]string(nil), tools...)
}

// All returns a copy of the current role allowlist map.
func All() map[string][]string {
	ensureLoaded()
	roleAllowlistMu.RLock()
	defer roleAllowlistMu.RUnlock()

	result := make(map[string][]string, len(roleAllowlist))
	for role, tools := range roleAllowlist {
		result[role] = append([]string(nil), tools...)
	}
	return result
}

// Refresh forces a new fetch from llm-service and replaces the in-memory map.
func Refresh(ctx context.Context) error {
	fetched, err := fetchRoleAllowlist(ctx)
	if err != nil {
		return err
	}
	if len(fetched) == 0 {
		return fmt.Errorf("role allowlist is empty after refresh")
	}
	roleAllowlistMu.Lock()
	roleAllowlist = fetched
	roleAllowlistMu.Unlock()
	return nil
}
