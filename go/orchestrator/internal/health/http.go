package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
)

// HTTPHandler provides HTTP endpoints for health checks
type HTTPHandler struct {
	manager *Manager
	logger  *zap.Logger
}

// NewHTTPHandler creates a new HTTP handler for health checks
func NewHTTPHandler(manager *Manager, logger *zap.Logger) *HTTPHandler {
	return &HTTPHandler{
		manager: manager,
		logger:  logger,
	}
}

// RegisterRoutes registers health check endpoints with an HTTP mux
func (h *HTTPHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/health/ready", h.handleReadiness)
	mux.HandleFunc("/health/live", h.handleLiveness)
	mux.HandleFunc("/health/detailed", h.handleDetailedHealth)
}

// handleHealth returns overall health status (for general monitoring)
func (h *HTTPHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()
	overall := h.manager.GetOverallHealth(ctx)

	// Set HTTP status based on health
	var statusCode int
	switch overall.Status {
	case StatusHealthy:
		statusCode = http.StatusOK
	case StatusDegraded:
		statusCode = http.StatusOK // Still OK but with warning
	case StatusUnhealthy:
		statusCode = http.StatusServiceUnavailable
	default:
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"status":    overall.Status.String(),
		"message":   overall.Message,
		"timestamp": overall.Timestamp.Unix(),
		"duration":  overall.Duration.String(),
		"degraded":  overall.Degraded,
		"ready":     overall.Ready,
		"live":      overall.Live,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("Failed to encode health response", zap.Error(err))
	}
}

// handleReadiness returns readiness status (for k8s readiness probes)
func (h *HTTPHandler) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()
	ready := h.manager.IsReady(ctx)

	var statusCode int
	var message string

	if ready {
		statusCode = http.StatusOK
		message = "ready"
	} else {
		statusCode = http.StatusServiceUnavailable
		message = "not ready"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"status":    message,
		"ready":     ready,
		"timestamp": time.Now().Unix(),
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("Failed to encode readiness response", zap.Error(err))
	}
}

// handleLiveness returns liveness status (for k8s liveness probes)
func (h *HTTPHandler) handleLiveness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()
	alive := h.manager.IsLive(ctx)

	var statusCode int
	var message string

	if alive {
		statusCode = http.StatusOK
		message = "alive"
	} else {
		statusCode = http.StatusServiceUnavailable
		message = "not alive"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"status":    message,
		"live":      alive,
		"timestamp": time.Now().Unix(),
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("Failed to encode liveness response", zap.Error(err))
	}
}

// handleDetailedHealth returns detailed health information (for debugging)
func (h *HTTPHandler) handleDetailedHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()
	
	// Check for cached results parameter
	cached := r.URL.Query().Get("cached") == "true"
	
	var detailed DetailedHealth
	if cached {
		// Return cached results without running new checks
		lastResults := h.manager.GetLastResults()
		components := make(map[string]CheckResult)
		for name, result := range lastResults {
			components[name] = result
		}
		
		// Calculate summary from cached results
		summary := HealthSummary{Total: len(components)}
		for _, result := range components {
			switch result.Status {
			case StatusHealthy:
				summary.Healthy++
			case StatusDegraded:
				summary.Degraded++
			case StatusUnhealthy:
				summary.Unhealthy++
			}
			if result.Critical {
				summary.Critical++
			} else {
				summary.NonCritical++
			}
		}
		
		// Use manager's calculateOverallStatus method (need to make it accessible)
		overall := h.calculateOverallStatusForHTTP(components, summary)
		
		detailed = DetailedHealth{
			Overall:    overall,
			Components: components,
			Summary:    summary,
			Timestamp:  time.Now(),
		}
	} else {
		detailed = h.manager.GetDetailedHealth(ctx)
	}

	// Set HTTP status based on overall health
	var statusCode int
	switch detailed.Overall.Status {
	case StatusHealthy:
		statusCode = http.StatusOK
	case StatusDegraded:
		statusCode = http.StatusOK
	case StatusUnhealthy:
		statusCode = http.StatusServiceUnavailable
	default:
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(detailed); err != nil {
		h.logger.Error("Failed to encode detailed health response", zap.Error(err))
	}
}

// calculateOverallStatusForHTTP duplicates the manager's logic for HTTP responses
func (h *HTTPHandler) calculateOverallStatusForHTTP(components map[string]CheckResult, summary HealthSummary) OverallHealth {
	if summary.Total == 0 {
		return OverallHealth{
			Status:  StatusUnknown,
			Message: "No health checks registered",
			Ready:   false,
			Live:    false,
		}
	}

	criticalFailures := 0
	nonCriticalFailures := 0
	degradedComponents := 0

	for _, result := range components {
		if result.Status == StatusDegraded {
			degradedComponents++
		}
		
		if result.Status == StatusUnhealthy {
			if result.Critical {
				criticalFailures++
			} else {
				nonCriticalFailures++
			}
		}
	}

	var status CheckStatus
	var message string
	var ready, live bool

	if criticalFailures > 0 {
		status = StatusUnhealthy
		message = fmt.Sprintf("%d critical component(s) failing", criticalFailures)
		ready = false
		live = true
	} else if degradedComponents > 0 {
		status = StatusDegraded
		message = fmt.Sprintf("%d component(s) degraded", degradedComponents)
		ready = true
		live = true
	} else if nonCriticalFailures > 0 {
		status = StatusDegraded
		message = fmt.Sprintf("%d non-critical component(s) failing", nonCriticalFailures)
		ready = true
		live = true
	} else {
		status = StatusHealthy
		message = fmt.Sprintf("All %d components healthy", summary.Total)
		ready = true
		live = true
	}

	return OverallHealth{
		Status:   status,
		Message:  message,
		Degraded: status == StatusDegraded || degradedComponents > 0,
		Ready:    ready,
		Live:     live,
	}
}

// writeError writes an error response
func (h *HTTPHandler) writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"error":     message,
		"timestamp": time.Now().Unix(),
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("Failed to encode error response", zap.Error(err))
	}
}

// StartHealthServer starts a dedicated HTTP server for health checks
func StartHealthServer(manager *Manager, port int, logger *zap.Logger) *http.Server {
	handler := NewHTTPHandler(manager, logger)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	server := &http.Server{
		Addr:         ":" + strconv.Itoa(port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("Starting health check server", zap.Int("port", port))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Health check server failed", zap.Error(err))
		}
	}()

	return server
}