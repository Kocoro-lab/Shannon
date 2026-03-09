package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestToolsHandler_FetchMetadata_DangerousFlag(t *testing.T) {
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/metadata") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":      "bash_executor",
				"dangerous": true,
				"category":  "system",
			})
			return
		}
	}))
	defer mockLLM.Close()

	h := &ToolsHandler{
		llmServiceURL: mockLLM.URL,
		logger:        zap.NewNop(),
		httpClient:    http.DefaultClient,
		metaCache:     make(map[string]*toolMetaCacheEntry),
	}

	meta, err := h.fetchToolMetadata(context.Background(), "bash_executor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta == nil {
		t.Fatal("expected metadata, got nil")
	}
	if !meta.Dangerous {
		t.Error("expected dangerous=true")
	}
}

func TestToolsHandler_FetchMetadata_NotFound(t *testing.T) {
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{"detail": "not found"})
	}))
	defer mockLLM.Close()

	h := &ToolsHandler{
		llmServiceURL: mockLLM.URL,
		logger:        zap.NewNop(),
		httpClient:    http.DefaultClient,
		metaCache:     make(map[string]*toolMetaCacheEntry),
	}

	meta, err := h.fetchToolMetadata(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta != nil {
		t.Errorf("expected nil for missing tool, got %+v", meta)
	}
}

func TestToolsHandler_FetchMetadata_CacheHit(t *testing.T) {
	callCount := 0
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":      "calculator",
			"dangerous": false,
			"category":  "calculation",
		})
	}))
	defer mockLLM.Close()

	h := &ToolsHandler{
		llmServiceURL: mockLLM.URL,
		logger:        zap.NewNop(),
		httpClient:    http.DefaultClient,
		metaCache:     make(map[string]*toolMetaCacheEntry),
	}

	h.fetchToolMetadata(context.Background(), "calculator")
	h.fetchToolMetadata(context.Background(), "calculator")

	if callCount != 1 {
		t.Errorf("expected 1 upstream call (cached), got %d", callCount)
	}
}

func TestToolsHandler_ProxyRequestShape(t *testing.T) {
	req := toolExecuteProxyRequest{
		ToolName:   "web_search",
		Parameters: map[string]interface{}{"query": "test"},
		SessionContext: map[string]interface{}{
			"user_id": "u-123",
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)

	if parsed["tool_name"] != "web_search" {
		t.Errorf("expected tool_name=web_search, got %v", parsed["tool_name"])
	}
	params := parsed["parameters"].(map[string]interface{})
	if params["query"] != "test" {
		t.Errorf("expected query=test, got %v", params["query"])
	}
	ctx := parsed["session_context"].(map[string]interface{})
	if ctx["user_id"] != "u-123" {
		t.Errorf("expected user_id=u-123, got %v", ctx["user_id"])
	}
	if _, ok := ctx["tenant_id"]; ok {
		t.Error("tenant_id should not be in session_context")
	}
}
