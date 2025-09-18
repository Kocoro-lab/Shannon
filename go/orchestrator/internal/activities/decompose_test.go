package activities

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"go.uber.org/zap"
)

func TestDecomposeTask_Success(t *testing.T) {
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
