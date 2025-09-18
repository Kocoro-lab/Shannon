package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

// ApprovalHandler handles human approval decisions via HTTP and forwards them to Temporal as signals.
type ApprovalHandler struct {
	temporal  client.Client
	logger    *zap.Logger
	authToken string
}

// NewApprovalHandler creates a new handler.
func NewApprovalHandler(t client.Client, logger *zap.Logger, authToken string) *ApprovalHandler {
	return &ApprovalHandler{temporal: t, logger: logger, authToken: authToken}
}

// RegisterRoutes registers approval routes on the provided mux.
func (h *ApprovalHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/approvals/decision", h.handleDecision)
}

// approvalDecisionRequest is the expected payload for approval decisions.
type approvalDecisionRequest struct {
	WorkflowID     string `json:"workflow_id"`
	RunID          string `json:"run_id,omitempty"`
	ApprovalID     string `json:"approval_id"`
	Approved       bool   `json:"approved"`
	Feedback       string `json:"feedback,omitempty"`
	ModifiedAction string `json:"modified_action,omitempty"`
	ApprovedBy     string `json:"approved_by,omitempty"`
}

func (h *ApprovalHandler) handleDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Auth: Bearer token
	if h.authToken != "" {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != h.authToken {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
	}

	var req approvalDecisionRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		h.logger.Warn("approval decode error", zap.Error(err))
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.WorkflowID == "" || req.ApprovalID == "" {
		http.Error(w, `{"error":"workflow_id and approval_id are required"}`, http.StatusBadRequest)
		return
	}

	// Build result payload for signal
	payload := activities.HumanApprovalResult{
		ApprovalID:     req.ApprovalID,
		Approved:       req.Approved,
		Feedback:       req.Feedback,
		ModifiedAction: req.ModifiedAction,
		ApprovedBy:     req.ApprovedBy,
		Timestamp:      time.Now(),
	}

	signalName := fmt.Sprintf("human-approval-%s", req.ApprovalID)

	// Send signal with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := h.temporal.SignalWorkflow(ctx, req.WorkflowID, req.RunID, signalName, payload); err != nil {
		h.logger.Error("failed to signal workflow", zap.String("workflow_id", req.WorkflowID), zap.String("run_id", req.RunID), zap.String("signal", signalName), zap.Error(err))
		http.Error(w, `{"error":"failed to signal workflow"}`, http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":      "sent",
		"workflow_id": req.WorkflowID,
		"run_id":      req.RunID,
		"approval_id": req.ApprovalID,
	})
}

// StartApprovalServer starts a dedicated HTTP server for approval decisions.
func StartApprovalServer(port int, authToken string, t client.Client, logger *zap.Logger) *http.Server {
	handler := NewApprovalHandler(t, logger, authToken)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	go func() {
		logger.Info("Starting approvals API server", zap.Int("port", port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Approvals API server failed", zap.Error(err))
		}
	}()
	return srv
}
