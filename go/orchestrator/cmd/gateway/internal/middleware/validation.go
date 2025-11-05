package middleware

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// ValidationMiddleware performs basic input validation for common params
type ValidationMiddleware struct {
	logger *zap.Logger
}

func NewValidationMiddleware(logger *zap.Logger) *ValidationMiddleware {
	return &ValidationMiddleware{logger: logger}
}

func (vm *ValidationMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		method := r.Method

		// Validate by route. Keep this minimal and fast.
		switch {
		case method == http.MethodGet && path == "/api/v1/tasks":
			if !vm.validatePagination(w, r, 1, 100) {
				return
			}
			if !vm.validateOptionalSessionID(w, r) {
				return
			}
			if !vm.validateOptionalStatus(w, r) {
				return
			}

		case method == http.MethodGet && strings.HasPrefix(path, "/api/v1/tasks/") && strings.HasSuffix(path, "/events"):
			if !vm.validatePathID(w, r) {
				return
			}
			if !vm.validatePagination(w, r, 1, 200) {
				return
			}

		case method == http.MethodGet && strings.HasPrefix(path, "/api/v1/tasks/") && strings.HasSuffix(path, "/stream"):
			if !vm.validatePathID(w, r) {
				return
			}
			if !vm.validateOptionalTypes(w, r) {
				return
			}
			if !vm.validateOptionalLastEventID(w, r) {
				return
			}

		case method == http.MethodGet && strings.HasPrefix(path, "/api/v1/tasks/"):
			// e.g., GET /api/v1/tasks/{id}
			if !vm.validatePathID(w, r) {
				return
			}

		case strings.HasPrefix(path, "/api/v1/stream/sse") || strings.HasPrefix(path, "/api/v1/stream/ws"):
			if !vm.validateWorkflowID(w, r) {
				return
			}
			if !vm.validateOptionalTypes(w, r) {
				return
			}
			if !vm.validateOptionalLastEventID(w, r) {
				return
			}

		case method == http.MethodGet && strings.HasPrefix(path, "/api/v1/sessions/") && strings.HasSuffix(path, "/events"):
			// Validate sessions events pagination (turns)
			// Note: pathValue("sessionId") is validated in handler (dual ID allowed)
			if !vm.validatePagination(w, r, 1, 100) {
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// --- helpers ---

var idRe = regexp.MustCompile(`^[A-Za-z0-9:_\-\.]{1,128}$`)

func (vm *ValidationMiddleware) validatePathID(w http.ResponseWriter, r *http.Request) bool {
	id := r.PathValue("id")
	if id == "" || !idRe.MatchString(id) {
		vm.sendBadRequest(w, "Invalid task ID format")
		return false
	}
	return true
}

func (vm *ValidationMiddleware) validateWorkflowID(w http.ResponseWriter, r *http.Request) bool {
	wf := r.URL.Query().Get("workflow_id")
	if wf == "" || !idRe.MatchString(wf) {
		vm.sendBadRequest(w, "Invalid or missing workflow_id")
		return false
	}
	return true
}

func (vm *ValidationMiddleware) validatePagination(w http.ResponseWriter, r *http.Request, minLimit, maxLimit int) bool {
	q := r.URL.Query()
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < minLimit || n > maxLimit {
			vm.sendBadRequest(w, "Invalid limit parameter")
			return false
		}
	}
	if o := q.Get("offset"); o != "" {
		n, err := strconv.Atoi(o)
		if err != nil || n < 0 {
			vm.sendBadRequest(w, "Invalid offset parameter")
			return false
		}
	}
	return true
}

func (vm *ValidationMiddleware) validateOptionalSessionID(w http.ResponseWriter, r *http.Request) bool {
	s := r.URL.Query().Get("session_id")
	if s == "" {
		return true
	}
	if !idRe.MatchString(s) {
		vm.sendBadRequest(w, "Invalid session_id format")
		return false
	}
	return true
}

var allowedStatuses = map[string]struct{}{
	"QUEUED":    {},
	"RUNNING":   {},
	"COMPLETED": {},
	"FAILED":    {},
	"CANCELLED": {},
	"CANCELED":  {},
	"TIMEOUT":   {},
}

func (vm *ValidationMiddleware) validateOptionalStatus(w http.ResponseWriter, r *http.Request) bool {
	s := r.URL.Query().Get("status")
	if s == "" {
		return true
	}
	if _, ok := allowedStatuses[strings.ToUpper(s)]; !ok {
		vm.sendBadRequest(w, "Invalid status value")
		return false
	}
	return true
}

func (vm *ValidationMiddleware) validateOptionalTypes(w http.ResponseWriter, r *http.Request) bool {
    // Do not enforce a static allowlist here.
    // The orchestrator/admin server performs filtering; unknown types simply yield no events.
    // Accept any non-empty comma-separated tokens and let downstream handle semantics.
    _ = r.URL.Query().Get("types")
    return true
}

func (vm *ValidationMiddleware) validateOptionalLastEventID(w http.ResponseWriter, r *http.Request) bool {
    v := r.URL.Query().Get("last_event_id")
    if v == "" {
        return true
    }
    // Accept either a numeric sequence or a Redis stream ID (e.g., 1700000000000-0)
    if strings.Contains(v, "-") {
        parts := strings.SplitN(v, "-", 2)
        if len(parts) != 2 {
            vm.sendBadRequest(w, "Invalid last_event_id")
            return false
        }
        if _, err := strconv.ParseInt(parts[0], 10, 64); err != nil {
            vm.sendBadRequest(w, "Invalid last_event_id")
            return false
        }
        if _, err := strconv.ParseInt(parts[1], 10, 64); err != nil {
            vm.sendBadRequest(w, "Invalid last_event_id")
            return false
        }
        return true
    }
    if n, err := strconv.ParseInt(v, 10, 64); err != nil || n < 0 {
        vm.sendBadRequest(w, "Invalid last_event_id")
        return false
    }
    return true
}

func (vm *ValidationMiddleware) sendBadRequest(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
