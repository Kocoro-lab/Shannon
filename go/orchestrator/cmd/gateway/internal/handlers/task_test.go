package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	orchpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/skills"
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

// Schedule methods (stubs for interface completeness)
func (f *fakeOrchClient) CreateSchedule(ctx context.Context, in *orchpb.CreateScheduleRequest, opts ...grpc.CallOption) (*orchpb.CreateScheduleResponse, error) {
	return nil, nil
}
func (f *fakeOrchClient) GetSchedule(ctx context.Context, in *orchpb.GetScheduleRequest, opts ...grpc.CallOption) (*orchpb.GetScheduleResponse, error) {
	return nil, nil
}
func (f *fakeOrchClient) ListSchedules(ctx context.Context, in *orchpb.ListSchedulesRequest, opts ...grpc.CallOption) (*orchpb.ListSchedulesResponse, error) {
	return nil, nil
}
func (f *fakeOrchClient) UpdateSchedule(ctx context.Context, in *orchpb.UpdateScheduleRequest, opts ...grpc.CallOption) (*orchpb.UpdateScheduleResponse, error) {
	return nil, nil
}
func (f *fakeOrchClient) DeleteSchedule(ctx context.Context, in *orchpb.DeleteScheduleRequest, opts ...grpc.CallOption) (*orchpb.DeleteScheduleResponse, error) {
	return nil, nil
}
func (f *fakeOrchClient) PauseSchedule(ctx context.Context, in *orchpb.PauseScheduleRequest, opts ...grpc.CallOption) (*orchpb.PauseScheduleResponse, error) {
	return nil, nil
}
func (f *fakeOrchClient) ResumeSchedule(ctx context.Context, in *orchpb.ResumeScheduleRequest, opts ...grpc.CallOption) (*orchpb.ResumeScheduleResponse, error) {
	return nil, nil
}

func (f *fakeOrchClient) RecordTokenUsage(ctx context.Context, in *orchpb.RecordTokenUsageRequest, opts ...grpc.CallOption) (*orchpb.RecordTokenUsageResponse, error) {
	return &orchpb.RecordTokenUsageResponse{Success: true}, nil
}
func (f *fakeOrchClient) SubmitReviewDecision(ctx context.Context, in *orchpb.SubmitReviewDecisionRequest, opts ...grpc.CallOption) (*orchpb.SubmitReviewDecisionResponse, error) {
	return &orchpb.SubmitReviewDecisionResponse{Success: true}, nil
}
func (f *fakeOrchClient) SendSwarmMessage(ctx context.Context, in *orchpb.SendSwarmMessageRequest, opts ...grpc.CallOption) (*orchpb.SendSwarmMessageResponse, error) {
	return &orchpb.SendSwarmMessageResponse{Success: true, Status: "sent"}, nil
}

func newHandlerWithFake(t *testing.T, fc *fakeOrchClient) *TaskHandler {
	t.Helper()
	logger := zap.NewNop()
	var db *sqlx.DB
	var rdb *redis.Client
	return NewTaskHandler(fc, db, rdb, nil, logger)
}

func addUserContext(req *http.Request) *http.Request {
	uc := &auth.UserContext{UserID: uuid.New(), TenantID: uuid.New(), Username: "tester", Email: "t@example.com"}
	ctx := context.WithValue(req.Context(), auth.UserContextKey, uc)
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

// --- Dangerous Skill Authorization Tests ---

// createTestSkillRegistry creates a registry with test skills for authorization testing.
func createTestSkillRegistry(t *testing.T) *skills.SkillRegistry {
	t.Helper()
	reg := skills.NewRegistry()

	// We can't easily add skills without files, so we'll use reflection or
	// a workaround. For simplicity, create a temp directory with skill files.
	tmpDir := t.TempDir()

	// Create a dangerous skill
	dangerousSkill := `---
name: dangerous-test-skill
version: "1.0"
description: A dangerous skill for testing
dangerous: true
enabled: true
---
This is a dangerous skill content.
`
	if err := os.WriteFile(filepath.Join(tmpDir, "dangerous.md"), []byte(dangerousSkill), 0644); err != nil {
		t.Fatalf("failed to write dangerous skill: %v", err)
	}

	// Create a safe skill
	safeSkill := `---
name: safe-test-skill
version: "1.0"
description: A safe skill for testing
dangerous: false
enabled: true
---
This is a safe skill content.
`
	if err := os.WriteFile(filepath.Join(tmpDir, "safe.md"), []byte(safeSkill), 0644); err != nil {
		t.Fatalf("failed to write safe skill: %v", err)
	}

	if err := reg.LoadDirectory(tmpDir); err != nil {
		t.Fatalf("failed to load skills: %v", err)
	}
	if err := reg.Finalize(); err != nil {
		t.Fatalf("failed to finalize registry: %v", err)
	}

	return reg
}

func newHandlerWithSkills(t *testing.T, fc *fakeOrchClient, reg *skills.SkillRegistry) *TaskHandler {
	t.Helper()
	logger := zap.NewNop()
	var db *sqlx.DB
	var rdb *redis.Client
	return NewTaskHandler(fc, db, rdb, reg, logger)
}

func addUserContextWithRoleAndScopes(req *http.Request, role string, scopes []string) *http.Request {
	uc := &auth.UserContext{
		UserID:   uuid.New(),
		TenantID: uuid.New(),
		Username: "tester",
		Email:    "t@example.com",
		Role:     role,
		Scopes:   scopes,
	}
	ctx := context.WithValue(req.Context(), auth.UserContextKey, uc)
	return req.WithContext(ctx)
}

func TestDangerousSkillAuthorization(t *testing.T) {
	cases := []struct {
		name           string
		skillName      string
		role           string
		scopes         []string
		expectedStatus int
	}{
		{
			name:           "admin can use dangerous skill",
			skillName:      "dangerous-test-skill",
			role:           auth.RoleAdmin,
			scopes:         nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "owner can use dangerous skill",
			skillName:      "dangerous-test-skill",
			role:           auth.RoleOwner,
			scopes:         nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "user with skills:dangerous scope can use dangerous skill",
			skillName:      "dangerous-test-skill",
			role:           auth.RoleUser,
			scopes:         []string{auth.ScopeSkillsDangerous},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "regular user blocked from dangerous skill",
			skillName:      "dangerous-test-skill",
			role:           auth.RoleUser,
			scopes:         nil,
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "user with unrelated scope blocked from dangerous skill",
			skillName:      "dangerous-test-skill",
			role:           auth.RoleUser,
			scopes:         []string{auth.ScopeWorkflowsRead, auth.ScopeSessionsWrite},
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "regular user can use safe skill",
			skillName:      "safe-test-skill",
			role:           auth.RoleUser,
			scopes:         nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "admin can use safe skill",
			skillName:      "safe-test-skill",
			role:           auth.RoleAdmin,
			scopes:         nil,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakeOrchClient{}
			reg := createTestSkillRegistry(t)
			h := newHandlerWithSkills(t, fc, reg)

			body := map[string]any{
				"query": "test query",
				"skill": tc.skillName,
			}

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", mustJSON(t, body))
			req.Header.Set("Content-Type", "application/json")
			req = addUserContextWithRoleAndScopes(req, tc.role, tc.scopes)

			h.SubmitTask(rr, req)

			if rr.Code != tc.expectedStatus {
				t.Fatalf("expected %d, got %d; body: %s", tc.expectedStatus, rr.Code, rr.Body.String())
			}

			// For successful requests, verify the skill was applied
			if tc.expectedStatus == http.StatusOK && fc.lastReq != nil {
				ctxMap := getContextMap(t, fc.lastReq.GetContext())
				if ctxMap["skill"] != tc.skillName {
					t.Fatalf("expected skill %q in context, got %v", tc.skillName, ctxMap["skill"])
				}
			}
		})
	}
}

func TestTaskStatusResponse_UnifiedResponseField(t *testing.T) {
	resp := TaskStatusResponse{
		TaskID: "task-123",
		Status: "TASK_STATUS_COMPLETED",
		Result: "Hello world",
		Unified: map[string]interface{}{
			"task_id":     "task-123",
			"status":      "completed",
			"result":      "Hello world",
			"stop_reason": "completed",
			"usage": map[string]interface{}{
				"input_tokens":  1000,
				"output_tokens": 500,
				"total_tokens":  1500,
				"cost_usd":      0.015,
			},
			"performance": map[string]interface{}{
				"execution_time_ms": 4500,
			},
			"metadata": map[string]interface{}{
				"model":          "claude-sonnet-4-5-20250929",
				"execution_mode": "browser_use",
			},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// unified_response field must be present
	unified, ok := decoded["unified_response"].(map[string]interface{})
	if !ok {
		t.Fatalf("unified_response missing or wrong type, keys: %v", keysOf(decoded))
	}

	// Check key fields
	if unified["task_id"] != "task-123" {
		t.Errorf("unified_response.task_id = %v, want %q", unified["task_id"], "task-123")
	}
	if unified["status"] != "completed" {
		t.Errorf("unified_response.status = %v, want %q", unified["status"], "completed")
	}
	if unified["stop_reason"] != "completed" {
		t.Errorf("unified_response.stop_reason = %v, want %q", unified["stop_reason"], "completed")
	}

	usage, ok := unified["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("unified_response.usage missing")
	}
	if usage["total_tokens"] != float64(1500) {
		t.Errorf("unified_response.usage.total_tokens = %v, want 1500", usage["total_tokens"])
	}
}

func TestTaskStatusResponse_UnifiedResponseOmittedWhenNil(t *testing.T) {
	resp := TaskStatusResponse{
		TaskID: "task-456",
		Status: "TASK_STATUS_RUNNING",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := decoded["unified_response"]; ok {
		t.Errorf("unified_response should be omitted when nil, but is present")
	}
}

func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestDangerousSkillLoading(t *testing.T) {
	reg := createTestSkillRegistry(t)

	// Verify dangerous skill is loaded
	entry, ok := reg.Get("dangerous-test-skill")
	if !ok {
		t.Fatal("dangerous-test-skill not found in registry")
	}
	if entry.Skill == nil {
		t.Fatal("skill entry has nil Skill")
	}
	if !entry.Skill.Dangerous {
		t.Fatalf("expected Dangerous=true, got false; skill: %+v", entry.Skill)
	}
	t.Logf("Skill loaded correctly: name=%s, dangerous=%v", entry.Skill.Name, entry.Skill.Dangerous)

	// Verify safe skill is not dangerous
	safeEntry, ok := reg.Get("safe-test-skill")
	if !ok {
		t.Fatal("safe-test-skill not found in registry")
	}
	if safeEntry.Skill.Dangerous {
		t.Fatal("expected safe skill to have Dangerous=false")
	}
}
