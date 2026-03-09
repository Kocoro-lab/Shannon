package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/daemon"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

// SignalDaemonReply is the Temporal signal name used to deliver daemon replies
// back to scheduled task workflows waiting for a daemon response.
const SignalDaemonReply = "daemon-reply-v1"

// DaemonSignalHandler signals Temporal workflows when daemon replies arrive.
type DaemonSignalHandler struct {
	temporal  client.Client
	logger    *zap.Logger
	authToken string
}

func NewDaemonSignalHandler(t client.Client, logger *zap.Logger, authToken string) *DaemonSignalHandler {
	return &DaemonSignalHandler{temporal: t, logger: logger, authToken: authToken}
}

type daemonSignalRequest struct {
	WorkflowID    string             `json:"workflow_id"`
	WorkflowRunID string             `json:"workflow_run_id"`
	Reply         daemon.ReplyPayload `json:"reply"`
}

// HandleSignal receives a daemon reply and signals the corresponding Temporal workflow.
// POST /daemon/signal (admin server, internal only)
func (h *DaemonSignalHandler) HandleSignal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Auth via internal token (same pattern as approval/events)
	if h.authToken != "" {
		token := r.Header.Get("Authorization")
		if token != "Bearer "+h.authToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	var req daemonSignalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	if req.WorkflowID == "" {
		http.Error(w, `{"error":"workflow_id required"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	err := h.temporal.SignalWorkflow(ctx, req.WorkflowID, req.WorkflowRunID, SignalDaemonReply, req.Reply)
	if err != nil {
		h.logger.Error("failed to signal daemon reply",
			zap.String("workflow_id", req.WorkflowID),
			zap.Error(err),
		)
		http.Error(w, `{"error":"failed to signal workflow"}`, http.StatusBadGateway)
		return
	}

	h.logger.Info("daemon reply signaled to workflow",
		zap.String("workflow_id", req.WorkflowID),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "signaled", "workflow_id": req.WorkflowID})
}

// RegisterRoutes registers the daemon signal endpoint on the admin HTTP mux.
func (h *DaemonSignalHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/daemon/signal", h.HandleSignal)
}
