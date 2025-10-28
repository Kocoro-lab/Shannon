package handlers

import (
    "encoding/json"
    "fmt"
    "net/http"

    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
    orchpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
    "go.uber.org/zap"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

// ApprovalHandler handles approval-related HTTP requests
type ApprovalHandler struct {
	orchClient orchpb.OrchestratorServiceClient
	logger     *zap.Logger
}

// NewApprovalHandler creates a new approval handler
func NewApprovalHandler(
	orchClient orchpb.OrchestratorServiceClient,
	logger *zap.Logger,
) *ApprovalHandler {
	return &ApprovalHandler{
		orchClient: orchClient,
		logger:     logger,
	}
}

// ApprovalDecisionRequest represents the approval decision request
type ApprovalDecisionRequest struct {
	WorkflowID     string `json:"workflow_id"`
	RunID          string `json:"run_id,omitempty"`
	ApprovalID     string `json:"approval_id"`
	Approved       bool   `json:"approved"`
	Feedback       string `json:"feedback,omitempty"`
	ModifiedAction string `json:"modified_action,omitempty"`
	ApprovedBy     string `json:"approved_by,omitempty"`
}

// SubmitDecision handles POST /api/v1/approvals/decision
func (h *ApprovalHandler) SubmitDecision(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user context
	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req ApprovalDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.WorkflowID == "" || req.ApprovalID == "" {
		h.sendError(w, "workflow_id and approval_id are required", http.StatusBadRequest)
		return
	}

	// Use authenticated user as approved_by if not provided
	if req.ApprovedBy == "" {
		req.ApprovedBy = userCtx.UserID.String()
	}

    // Propagate auth/tracing headers to gRPC metadata
    ctx = withGRPCMetadata(ctx, r)

	// Build gRPC request
	grpcReq := &orchpb.ApproveTaskRequest{
		ApprovalId:     req.ApprovalID,
		WorkflowId:     req.WorkflowID,
		RunId:          req.RunID,
		Approved:       req.Approved,
		Feedback:       req.Feedback,
		ModifiedAction: req.ModifiedAction,
		ApprovedBy:     req.ApprovedBy,
	}

	// Call ApproveTask gRPC
	resp, err := h.orchClient.ApproveTask(ctx, grpcReq)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			switch st.Code() {
			case codes.InvalidArgument:
				h.sendError(w, st.Message(), http.StatusBadRequest)
			case codes.NotFound:
				h.sendError(w, "Workflow or approval not found", http.StatusNotFound)
			case codes.Unauthenticated:
				h.sendError(w, "Unauthorized", http.StatusUnauthorized)
			case codes.PermissionDenied:
				h.sendError(w, "Forbidden", http.StatusForbidden)
			default:
				h.sendError(w, fmt.Sprintf("Failed to submit approval: %v", st.Message()), http.StatusInternalServerError)
			}
		} else {
			h.sendError(w, fmt.Sprintf("Failed to submit approval: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// Log approval
	h.logger.Info("Approval decision submitted",
		zap.String("workflow_id", req.WorkflowID),
		zap.String("approval_id", req.ApprovalID),
		zap.Bool("approved", req.Approved),
		zap.String("user_id", userCtx.UserID.String()),
	)

	// Send response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "sent",
		"success":     resp.Success,
		"message":     resp.Message,
		"workflow_id": req.WorkflowID,
		"run_id":      req.RunID,
		"approval_id": req.ApprovalID,
	})
}

// sendError sends an error response
func (h *ApprovalHandler) sendError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
