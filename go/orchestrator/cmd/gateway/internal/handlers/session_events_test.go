package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap/zaptest"

	auth "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/google/uuid"
)

func TestGetSessionEvents_GroupedTurns_Defaults(t *testing.T) {
	// Setup mock DB
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	sqlxdb := sqlx.NewDb(db, "sqlmock")

	logger := zaptest.NewLogger(t)
	h := NewSessionHandler(sqlxdb, nil, logger)

	// Prepare inputs
	sessionInput := "s123"
	sessionUUID := "11111111-1111-1111-1111-111111111111"
	userUUID := "00000000-0000-0000-0000-000000000002"
	tenantUUID := "22222222-2222-2222-2222-222222222222"

	// 1) Session ownership lookup
	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT id, user_id, context->>'external_id' as external_id FROM sessions WHERE (id::text = $1 OR context->>'external_id' = $1) AND user_id = $2 AND tenant_id = $3 AND deleted_at IS NULL",
	)).WithArgs(sessionInput, userUUID, tenantUUID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "external_id"}).
			AddRow(sessionUUID, userUUID, nil))

	// 2) Count turns
	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT COUNT(*) FROM task_executions WHERE session_id = $1 AND user_id = $2 AND tenant_id = $3",
	)).WithArgs(sessionUUID, userUUID, tenantUUID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	// 3) Select turns (two rows)
	now := time.Now().UTC().Truncate(time.Second)
	rows := sqlmock.NewRows([]string{
		"id", "workflow_id", "query", "result", "started_at", "completed_at", "total_tokens", "duration_ms",
	}).
		AddRow("task-001", "wf-1", "What is 2+2?", sql.NullString{String: "2 + 2 equals 4.", Valid: true}, now, sql.NullTime{Valid: true, Time: now.Add(8 * time.Second)}, 150, 8000).
		AddRow("task-002", "wf-2", "Now multiply that by 3", sql.NullString{String: "", Valid: false}, now.Add(15*time.Second), sql.NullTime{Valid: true, Time: now.Add(23 * time.Second)}, 200, 8000)

	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT id, workflow_id, query, result, started_at, completed_at, COALESCE(total_tokens,0) as total_tokens, COALESCE(duration_ms,0) as duration_ms FROM task_executions WHERE session_id = $1 AND user_id = $2 AND tenant_id = $3 ORDER BY started_at ASC LIMIT $4 OFFSET $5",
	)).WithArgs(sessionUUID, userUUID, tenantUUID, 10, 0).WillReturnRows(rows)

	// 4) Events for both workflows
	evQuery := `
            SELECT workflow_id, type, COALESCE(agent_id,''), COALESCE(message,''), timestamp, COALESCE(seq,0), COALESCE(stream_id,'')
            FROM event_logs
            WHERE workflow_id IN ($1, $2) AND type <> 'LLM_PARTIAL'
            ORDER BY timestamp ASC
        `
	evRows := sqlmock.NewRows([]string{"workflow_id", "type", "agent_id", "message", "timestamp", "seq", "stream_id"}).
		AddRow("wf-1", "LLM_PROMPT", "planner", "Prompt", now.Add(1*time.Second), 1, "").
		AddRow("wf-1", "LLM_OUTPUT", "planner", "2 + 2 equals 4.", now.Add(2*time.Second), 2, "").
		AddRow("wf-2", "LLM_PROMPT", "simple-agent", "Prompt2", now.Add(16*time.Second), 1, "").
		AddRow("wf-2", "LLM_OUTPUT", "simple-agent", "4 × 3 equals 12.", now.Add(17*time.Second), 2, "")

	mock.ExpectQuery(regexp.QuoteMeta(evQuery)).WithArgs("wf-1", "wf-2").
		WillReturnRows(evRows)

	// Build request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sessionInput+"/events", nil)
	// Inject user context
	uid := uuid.MustParse(userUUID)
	tid := uuid.MustParse(tenantUUID)
	req = req.WithContext(context.WithValue(req.Context(), "user", &auth.UserContext{UserID: uid, TenantID: tid}))

	// Use ServeMux to bind path parameter
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/sessions/{sessionId}/events", http.HandlerFunc(h.GetSessionEvents))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		SessionID string `json:"session_id"`
		Count     int    `json:"count"`
		Turns     []struct {
			Turn        int       `json:"turn"`
			TaskID      string    `json:"task_id"`
			UserQuery   string    `json:"user_query"`
			FinalOutput string    `json:"final_output"`
			Timestamp   time.Time `json:"timestamp"`
			Events      []any     `json:"events"`
			Metadata    struct {
				TokensUsed      int      `json:"tokens_used"`
				ExecutionTimeMs int      `json:"execution_time_ms"`
				AgentsInvolved  []string `json:"agents_involved"`
			} `json:"metadata"`
		} `json:"turns"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if resp.Count != 2 || len(resp.Turns) != 2 {
		t.Fatalf("expected 2 turns, got count=%d len=%d", resp.Count, len(resp.Turns))
	}
	if resp.Turns[0].Turn != 1 || resp.Turns[1].Turn != 2 {
		t.Fatalf("unexpected turn numbering: %+v", []int{resp.Turns[0].Turn, resp.Turns[1].Turn})
	}
	if resp.Turns[0].TaskID != "task-001" || resp.Turns[1].TaskID != "task-002" {
		t.Fatalf("unexpected task ids: %s, %s", resp.Turns[0].TaskID, resp.Turns[1].TaskID)
	}
	// Final output fallback: second turn should pick from LLM_OUTPUT
	if !strings.Contains(resp.Turns[1].FinalOutput, "equals 12") {
		t.Fatalf("fallback final_output not applied: %q", resp.Turns[1].FinalOutput)
	}
	if resp.Turns[0].Metadata.TokensUsed != 150 || resp.Turns[0].Metadata.ExecutionTimeMs != 8000 {
		t.Fatalf("metadata mismatch for turn 1: %+v", resp.Turns[0].Metadata)
	}
	if len(resp.Turns[0].Metadata.AgentsInvolved) == 0 || len(resp.Turns[1].Metadata.AgentsInvolved) == 0 {
		t.Fatalf("agents_involved should not be empty")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// mustParseUUID is a helper to create a uuid.UUID without importing extra deps here.
// It mirrors the shape used in tests by building a 16-byte array from a string placeholder.
func mustParseUUID(_ string) [16]byte { return [16]byte{} }

// withPathValue adds a path parameter to the request context (Go 1.22+).
// Use reflection-free wrapper to avoid importing net/http internals in tests across versions.
// no-op helper retained only for clarity; using http.WithPathValue above.
