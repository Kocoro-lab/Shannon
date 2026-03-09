package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	authpkg "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/daemon"
	"go.uber.org/zap"
)

var signalClient = &http.Client{Timeout: 10 * time.Second}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type DaemonHandler struct {
	hub         *daemon.Hub
	adminURL    string // orchestrator admin server URL for Temporal signals
	eventsToken string // auth token for admin server
	logger      *zap.Logger
}

func NewDaemonHandler(hub *daemon.Hub, adminURL, eventsToken string, logger *zap.Logger) *DaemonHandler {
	return &DaemonHandler{hub: hub, adminURL: adminURL, eventsToken: eventsToken, logger: logger}
}

func (dh *DaemonHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	userCtx, ok := r.Context().Value(authpkg.UserContextKey).(*authpkg.UserContext)
	if !ok || userCtx == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ws, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		dh.logger.Error("websocket upgrade failed", zap.Error(err))
		return
	}

	// onReply callback — routes replies back to the originating channel and/or Temporal workflow.
	onReply := func(ctx context.Context, meta daemon.ClaimMetadata, reply daemon.ReplyPayload) {
		// Signal Temporal workflow if this reply is for a scheduled task
		if meta.WorkflowID != "" {
			dh.signalWorkflow(ctx, meta, reply)
		}
	}

	daemon.HandleConnection(r.Context(), dh.hub, ws, userCtx.TenantID.String(), userCtx.UserID.String(), onReply, dh.logger)
}

// signalWorkflow sends a daemon reply signal to the Temporal workflow via the admin server.
func (dh *DaemonHandler) signalWorkflow(ctx context.Context, meta daemon.ClaimMetadata, reply daemon.ReplyPayload) {
	if dh.adminURL == "" {
		dh.logger.Warn("cannot signal workflow: adminURL not configured", zap.String("workflow_id", meta.WorkflowID))
		return
	}

	payload := map[string]interface{}{
		"workflow_id":     meta.WorkflowID,
		"workflow_run_id": meta.WorkflowRunID,
		"reply":           reply,
	}
	body, _ := json.Marshal(payload)

	signalCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := dh.adminURL + "/daemon/signal"
	req, err := http.NewRequestWithContext(signalCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		dh.logger.Error("failed to create signal request", zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if dh.eventsToken != "" {
		req.Header.Set("Authorization", "Bearer "+dh.eventsToken)
	}

	resp, err := signalClient.Do(req)
	if err != nil {
		dh.logger.Error("failed to signal workflow",
			zap.String("workflow_id", meta.WorkflowID),
			zap.Error(err),
		)
		return
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		dh.logger.Error("workflow signal returned non-200",
			zap.String("workflow_id", meta.WorkflowID),
			zap.Int("status", resp.StatusCode),
		)
	}
}

func (dh *DaemonHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	userCtx, ok := r.Context().Value(authpkg.UserContextKey).(*authpkg.UserContext)
	if !ok || userCtx == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	count := dh.hub.ConnectedCount(userCtx.TenantID.String(), userCtx.UserID.String())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"connected_daemons": count})
}
