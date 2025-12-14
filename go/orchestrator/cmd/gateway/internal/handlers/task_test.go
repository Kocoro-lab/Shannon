package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	orchpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"
)

// --- Fake Orchestrator client capturing SubmitTask requests ---
type fakeOrchClient struct {
	lastReq *orchpb.SubmitTaskRequest
}

func (f *fakeOrchClient) SubmitTask(ctx context.Context, in *orchpb.SubmitTaskRequest, opts ...grpc.CallOption) (*orchpb.SubmitTaskResponse, error) {
	// Capture incoming request (strip gRPC metadata)
	if md, ok := metadata.FromOutgoingContext(ctx); ok && md.Len() > 0 {
		_ = md
	}
	f.lastReq = in
	return &orchpb.SubmitTaskResponse{WorkflowId: "wf-123", TaskId: "task-123"}, nil
}

// Unused methods for interface completeness
func (f *fakeOrchClient) GetTaskStatus(ctx context.Context, in *orchpb.GetTaskStatusRequest, opts ...grpc.CallOption) (*orchpb.GetTaskStatusResponse, error) {
	return nil, nil
}
func (f *fakeOrchClient) CancelTask(ctx context.Context, in *orchpb.CancelTaskRequest, opts ...grpc.CallOption) (*orchpb.CancelTaskResponse, error) {
	return nil, nil
}
func (f *fakeOrchClient) ListTasks(ctx context.Context, in *orchpb.ListTasksRequest, opts ...grpc.CallOption) (*orchpb.ListTasksResponse, error) {
	return nil, nil
}
func (f *fakeOrchClient) GetSessionContext(ctx context.Context, in *orchpb.GetSessionContextRequest, opts ...grpc.CallOption) (*orchpb.GetSessionContextResponse, error) {
	return nil, nil
}
func (f *fakeOrchClient) ListTemplates(ctx context.Context, in *orchpb.ListTemplatesRequest, opts ...grpc.CallOption) (*orchpb.ListTemplatesResponse, error) {
	return nil, nil
}
func (f *fakeOrchClient) ApproveTask(ctx context.Context, in *orchpb.ApproveTaskRequest, opts ...grpc.CallOption) (*orchpb.ApproveTaskResponse, error) {
	return nil, nil
}
func (f *fakeOrchClient) GetPendingApprovals(ctx context.Context, in *orchpb.GetPendingApprovalsRequest, opts ...grpc.CallOption) (*orchpb.GetPendingApprovalsResponse, error) {
	return nil, nil
}
func (f *fakeOrchClient) PauseTask(ctx context.Context, in *orchpb.PauseTaskRequest, opts ...grpc.CallOption) (*orchpb.PauseTaskResponse, error) {
	return &orchpb.PauseTaskResponse{Success: true}, nil
}
func (f *fakeOrchClient) ResumeTask(ctx context.Context, in *orchpb.ResumeTaskRequest, opts ...grpc.CallOption) (*orchpb.ResumeTaskResponse, error) {
	return &orchpb.ResumeTaskResponse{Success: true}, nil
}
func (f *fakeOrchClient) GetControlState(ctx context.Context, in *orchpb.GetControlStateRequest, opts ...grpc.CallOption) (*orchpb.GetControlStateResponse, error) {
	return &orchpb.GetControlStateResponse{IsPaused: false, IsCancelled: false}, nil
}

func newHandlerWithFake(t *testing.T, fc *fakeOrchClient) *TaskHandler {
	t.Helper()
	logger := zap.NewNop()
	var db *sqlx.DB
	var rdb *redis.Client
	return NewTaskHandler(fc, db, rdb, logger)
}

func addUserContext(req *http.Request) *http.Request {
	uc := &auth.UserContext{UserID: uuid.New(), TenantID: uuid.New(), Username: "tester", Email: "t@example.com"}
	ctx := context.WithValue(req.Context(), "user", uc)
	return req.WithContext(ctx)
}

func mustJSON(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewBuffer(b)
}

func getContextMap(t *testing.T, st *structpb.Struct) map[string]interface{} {
	if st == nil {
		return map[string]interface{}{}
	}
	return st.AsMap()
}

func TestModeAcceptedAndLabelled(t *testing.T) {
	modes := []string{"simple", "standard", "complex", "supervisor"}
	for _, m := range modes {
		fc := &fakeOrchClient{}
		h := newHandlerWithFake(t, fc)
		body := map[string]any{
			"query": "hello",
			"mode":  m,
		}
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", mustJSON(t, body))
		req.Header.Set("Content-Type", "application/json")
		req = addUserContext(req)
		h.SubmitTask(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("mode %s: expected 200, got %d", m, rr.Code)
		}
		if fc.lastReq == nil || fc.lastReq.Metadata == nil {
			t.Fatalf("mode %s: missing captured request/metadata", m)
		}
		got := fc.lastReq.Metadata.GetLabels()["mode"]
		if got != m {
			t.Fatalf("mode %s: label mismatch: got %q", m, got)
		}
	}
}

func TestModeInvalidRejected(t *testing.T) {
	fc := &fakeOrchClient{}
	h := newHandlerWithFake(t, fc)
	body := map[string]any{"query": "hello", "mode": "invalid"}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", mustJSON(t, body))
	req.Header.Set("Content-Type", "application/json")
	req = addUserContext(req)
	h.SubmitTask(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid mode, got %d", rr.Code)
	}
}

func TestDisableAIConflicts(t *testing.T) {
	cases := []map[string]any{
		{"query": "x", "model_tier": "large", "context": map[string]any{"disable_ai": true}},
		{"query": "x", "context": map[string]any{"disable_ai": true, "model_override": "gpt-5-2025-08-07"}},
		{"query": "x", "provider_override": "openai", "context": map[string]any{"disable_ai": true}},
	}
	for i, body := range cases {
		fc := &fakeOrchClient{}
		h := newHandlerWithFake(t, fc)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", mustJSON(t, body))
		req.Header.Set("Content-Type", "application/json")
		req = addUserContext(req)
		h.SubmitTask(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("case %d: expected 400, got %d", i, rr.Code)
		}
	}
}

func TestTemplateAliasNormalized(t *testing.T) {
	fc := &fakeOrchClient{}
	h := newHandlerWithFake(t, fc)
	body := map[string]any{
		"query":   "x",
		"context": map[string]any{"template_name": "research_summary"},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", mustJSON(t, body))
	req.Header.Set("Content-Type", "application/json")
	req = addUserContext(req)
	h.SubmitTask(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	ctxMap := getContextMap(t, fc.lastReq.GetContext())
	if ctxMap["template"] != "research_summary" {
		t.Fatalf("expected template normalized, got: %v", ctxMap["template"])
	}
}

func TestModelAndProviderOverridesInjected(t *testing.T) {
	fc := &fakeOrchClient{}
	h := newHandlerWithFake(t, fc)
	body := map[string]any{
		"query":             "x",
		"model_override":    "gpt-5-2025-08-07",
		"provider_override": "openai",
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", mustJSON(t, body))
	req.Header.Set("Content-Type", "application/json")
	req = addUserContext(req)
	h.SubmitTask(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	ctxMap := getContextMap(t, fc.lastReq.GetContext())
	if ctxMap["model_override"] != "gpt-5-2025-08-07" {
		t.Fatalf("expected model_override injected, got: %v", ctxMap["model_override"])
	}
	if ctxMap["provider_override"] != "openai" {
		t.Fatalf("expected provider_override injected, got: %v", ctxMap["provider_override"])
	}
}

func TestProviderValidation(t *testing.T) {
	validProviders := []string{"openai", "anthropic", "google", "groq", "xai", "deepseek", "qwen", "zai", "ollama", "mistral", "cohere"}

	// Test valid providers are accepted
	for _, provider := range validProviders {
		fc := &fakeOrchClient{}
		h := newHandlerWithFake(t, fc)
		body := map[string]any{
			"query":             "test query",
			"provider_override": provider,
		}
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", mustJSON(t, body))
		req.Header.Set("Content-Type", "application/json")
		req = addUserContext(req)
		h.SubmitTask(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("provider %s: expected 200, got %d (should be valid)", provider, rr.Code)
		}
		ctxMap := getContextMap(t, fc.lastReq.GetContext())
		if ctxMap["provider_override"] != provider {
			t.Fatalf("provider %s: expected provider injected, got: %v", provider, ctxMap["provider_override"])
		}
	}

	// Test invalid provider is rejected
	invalidProviders := []string{"invalid_provider", "unknown", "fake", "gpt", "claude"}
	for _, provider := range invalidProviders {
		fc := &fakeOrchClient{}
		h := newHandlerWithFake(t, fc)
		body := map[string]any{
			"query":             "test query",
			"provider_override": provider,
		}
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", mustJSON(t, body))
		req.Header.Set("Content-Type", "application/json")
		req = addUserContext(req)
		h.SubmitTask(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("provider %s: expected 400, got %d (should be invalid)", provider, rr.Code)
		}
	}
}

func TestDisableAIWithVariousFormats(t *testing.T) {
	// Test disable_ai with different value types
	cases := []struct {
		name         string
		body         map[string]any
		expectReject bool
	}{
		{
			name: "disable_ai as boolean true with model_tier",
			body: map[string]any{
				"query":      "x",
				"model_tier": "medium",
				"context":    map[string]any{"disable_ai": true},
			},
			expectReject: true,
		},
		{
			name: "disable_ai as string 'true' with model_override",
			body: map[string]any{
				"query":          "x",
				"model_override": "gpt-5-2025-08-07",
				"context":        map[string]any{"disable_ai": "true"},
			},
			expectReject: true,
		},
		{
			name: "disable_ai as number 1 with provider_override",
			body: map[string]any{
				"query":             "x",
				"provider_override": "anthropic",
				"context":           map[string]any{"disable_ai": 1},
			},
			expectReject: true,
		},
		{
			name: "disable_ai as boolean false with model_tier (should allow)",
			body: map[string]any{
				"query":      "x",
				"model_tier": "large",
				"context":    map[string]any{"disable_ai": false},
			},
			expectReject: false,
		},
		{
			name: "disable_ai as string 'false' with model_tier (should allow)",
			body: map[string]any{
				"query":      "x",
				"model_tier": "small",
				"context":    map[string]any{"disable_ai": "false"},
			},
			expectReject: false,
		},
		{
			name: "disable_ai true with no model controls (should allow)",
			body: map[string]any{
				"query":   "x",
				"context": map[string]any{"disable_ai": true},
			},
			expectReject: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakeOrchClient{}
			h := newHandlerWithFake(t, fc)
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", mustJSON(t, tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = addUserContext(req)
			h.SubmitTask(rr, req)

			if tc.expectReject {
				if rr.Code != http.StatusBadRequest {
					t.Fatalf("expected 400, got %d", rr.Code)
				}
			} else {
				if rr.Code != http.StatusOK {
					t.Fatalf("expected 200, got %d", rr.Code)
				}
			}
		})
	}
}
