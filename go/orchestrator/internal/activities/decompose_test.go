package activities

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"go.uber.org/zap"
)

func TestDecomposeTask_Success(t *testing.T) {
	// In restricted sandboxes, binding a local port may be disallowed.
	// If creating an httptest server fails due to bind restrictions,
	// skip this test rather than failing the suite.
	// Quick preflight: try binding a loopback port.
	// If this fails, we assume the environment forbids listeners.
	// Try IPv6 first to mirror httptest defaults; then IPv4.
	if ln6, err6 := net.Listen("tcp6", "[::1]:0"); err6 == nil {
		_ = ln6.Close()
	} else if ln4, err4 := net.Listen("tcp4", "127.0.0.1:0"); err4 == nil {
		_ = ln4.Close()
	} else {
		t.Skip("port binding not permitted in this environment; skipping")
	}

	// Mock Python service
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent/decompose" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		resp := DecompositionResult{
			Mode:            "standard",
			ComplexityScore: 0.55,
			Subtasks: []Subtask{
				{ID: "a", Description: "do A", Dependencies: []string{}, EstimatedTokens: 100},
				{ID: "b", Description: "do B", Dependencies: []string{"a"}, EstimatedTokens: 200},
			},
			TotalEstimatedTokens: 300,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	os.Setenv("LLM_SERVICE_URL", srv.URL)
	defer os.Unsetenv("LLM_SERVICE_URL")

	// Create a test instance that can handle non-Temporal contexts
	a := &Activities{logger: zap.NewNop()}

	// Test the decomposition logic directly without the HTTP client interceptor issues
	input := DecompositionInput{
		Query:          "test query",
		Context:        map[string]interface{}{"k": "v"},
		AvailableTools: []string{"tool1"},
	}

	out, err := a.DecomposeTask(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Mode != "standard" || len(out.Subtasks) != 2 || out.TotalEstimatedTokens != 300 {
		t.Fatalf("unexpected output: %+v", out)
	}
}
