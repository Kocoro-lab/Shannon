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
		"analysis":   {"web_search", "code_reader"},
		"research":   {"web_search"},
		"writer":     {"code_reader"},
		"critic":     {"code_reader"},
		"generalist": {},
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
