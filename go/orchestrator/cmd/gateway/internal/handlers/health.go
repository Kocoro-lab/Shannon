package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	orchpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// HealthHandler handles health check requests
type HealthHandler struct {
	orchClient orchpb.OrchestratorServiceClient
	logger     *zap.Logger
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(orchClient orchpb.OrchestratorServiceClient, logger *zap.Logger) *HealthHandler {
	return &HealthHandler{
		orchClient: orchClient,
		logger:     logger,
	}
}

// HealthResponse represents a health check response
type HealthResponse struct {
	Status  string            `json:"status"`
	Version string            `json:"version"`
	Time    time.Time         `json:"time"`
	Checks  map[string]string `json:"checks,omitempty"`
}

// Health handles GET /health
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:  "healthy",
		Version: "0.1.0",
		Time:    time.Now(),
		Checks:  make(map[string]string),
	}

	// Basic health check - gateway is up
	response.Checks["gateway"] = "ok"

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// Readiness handles GET /readiness
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:  "ready",
		Version: "0.1.0",
		Time:    time.Now(),
		Checks:  make(map[string]string),
	}

	// Check orchestrator connection
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	// Try to get task status for a non-existent task to test connection
	_, err := h.orchClient.GetTaskStatus(ctx, &orchpb.GetTaskStatusRequest{
		TaskId: "health-check-test",
	})

	if err != nil {
		// Check if it's a "not found" error (expected) vs connection error
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			// This is expected - orchestrator is responding
			response.Checks["orchestrator"] = "ok"
		} else {
			// Real error - orchestrator is not reachable
			response.Status = "not ready"
			response.Checks["orchestrator"] = "failed"
			h.logger.Warn("Orchestrator health check failed", zap.Error(err))

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(response)
			return
		}
	} else {
		response.Checks["orchestrator"] = "ok"
	}

	// All checks passed
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
