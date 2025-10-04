package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	"go.uber.org/zap"
)

type IngestHandler struct {
	logger    *zap.Logger
	authToken string
}

func NewIngestHandler(logger *zap.Logger, authToken string) *IngestHandler {
	return &IngestHandler{logger: logger, authToken: authToken}
}

func (h *IngestHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/events", h.handleIngest)
}

type ingestEvent struct {
	WorkflowID string                 `json:"workflow_id"`
	Type       string                 `json:"type"`
	AgentID    string                 `json:"agent_id,omitempty"`
	Message    string                 `json:"message,omitempty"`
	Payload    map[string]interface{} `json:"payload,omitempty"`
	Timestamp  string                 `json:"timestamp,omitempty"`
}

func (h *IngestHandler) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if h.authToken != "" {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != h.authToken {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
	}
	// Limit request body to 10MB to prevent DoS attacks
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	// Accept single object or array
	var single ingestEvent
	var arr []ingestEvent
	if err := json.Unmarshal(body, &single); err == nil && single.WorkflowID != "" {
		arr = []ingestEvent{single}
	} else {
		if err := json.Unmarshal(body, &arr); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
	}
	// Publish
	for _, e := range arr {
		if e.WorkflowID == "" || e.Type == "" {
			continue
		}
		ts := time.Now()
		if e.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, e.Timestamp); err == nil {
				ts = t
			}
		}
		streaming.Get().Publish(e.WorkflowID, streaming.Event{
			WorkflowID: e.WorkflowID,
			Type:       e.Type,
			AgentID:    e.AgentID,
			Message:    e.Message,
			Payload:    e.Payload,
			Timestamp:  ts,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
