package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	orchpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
)

// ReviewHandler handles HITL research plan review requests.
type ReviewHandler struct {
	orchClient orchpb.OrchestratorServiceClient
	redis      *redis.Client
	logger     *zap.Logger
}

// NewReviewHandler creates a new ReviewHandler.
func NewReviewHandler(
	orchClient orchpb.OrchestratorServiceClient,
	redis *redis.Client,
	logger *zap.Logger,
) *ReviewHandler {
	return &ReviewHandler{
		orchClient: orchClient,
		redis:      redis,
		logger:     logger,
	}
}

// reviewRequest is the HTTP request body for the review endpoint.
type reviewRequest struct {
	Action  string `json:"action"`  // "feedback" or "approve"
	Message string `json:"message"` // User's feedback message (optional for approve)
}

// reviewRound mirrors the activity type for Redis storage.
type reviewRound struct {
	Role      string `json:"role"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// reviewState is the Redis-stored conversation state.
type reviewState struct {
	WorkflowID    string                 `json:"workflow_id"`
	Query         string                 `json:"query"`
	Context       map[string]interface{} `json:"context"`
	Status        string                 `json:"status"`
	Round         int                    `json:"round"`
	Version       int                    `json:"version"`
	OwnerUserID   string                 `json:"owner_user_id"`
	OwnerTenantID string                 `json:"owner_tenant_id"`
	Rounds        []reviewRound          `json:"rounds"`
	CurrentPlan   string                 `json:"current_plan"`
}

// llmResearchPlanRequest is the request to the LLM service.
type llmResearchPlanRequest struct {
	Query        string                 `json:"query"`
	Context      map[string]interface{} `json:"context"`
	Conversation []reviewRound          `json:"conversation"`
}

// llmResearchPlanResponse is the response from the LLM service.
type llmResearchPlanResponse struct {
	Message      string `json:"message"`
	Intent       string `json:"intent"`
	Round        int    `json:"round"`
	Model        string `json:"model"`
	Provider     string `json:"provider"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

// HandleReview processes review feedback or approval.
func (h *ReviewHandler) HandleReview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workflowID := r.PathValue("workflowID")
	if workflowID == "" {
		h.sendError(w, "workflow_id is required", http.StatusBadRequest)
		return
	}

	// Auth check
	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok || userCtx == nil {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request
	var req reviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Action != "feedback" && req.Action != "approve" {
		h.sendError(w, "action must be 'feedback' or 'approve'", http.StatusBadRequest)
		return
	}

	// Load state from Redis
	key := fmt.Sprintf("review:%s", workflowID)
	data, err := h.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		h.sendError(w, "Review session not found or expired", http.StatusNotFound)
		return
	}
	if err != nil {
		h.logger.Error("Failed to read review state from Redis", zap.Error(err))
		h.sendError(w, "Internal error", http.StatusInternalServerError)
		return
	}

	var state reviewState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		h.logger.Error("Failed to unmarshal review state", zap.Error(err))
		h.sendError(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Owner check (skip if owner not set — e.g., dev mode without user_id in context)
	if state.OwnerUserID != "" && state.OwnerUserID != userCtx.UserID.String() {
		h.sendError(w, "Forbidden: not the task owner", http.StatusForbidden)
		return
	}

	switch req.Action {
	case "feedback":
		h.handleFeedback(ctx, w, r, key, &state, req, workflowID)
	case "approve":
		h.handleApprove(ctx, w, r, key, &state, workflowID, userCtx)
	}
}

func (h *ReviewHandler) handleFeedback(
	ctx context.Context, w http.ResponseWriter, r *http.Request,
	key string, state *reviewState, req reviewRequest, workflowID string,
) {
	// Optimistic concurrency check
	if ifMatch := r.Header.Get("If-Match"); ifMatch != "" {
		if ifMatch != strconv.Itoa(state.Version) {
			h.sendError(w, "Conflict: state has been modified", http.StatusConflict)
			return
		}
	}

	if req.Message == "" {
		h.sendError(w, "message is required for feedback", http.StatusBadRequest)
		return
	}

	// Append user message
	state.Rounds = append(state.Rounds, reviewRound{
		Role:      "user",
		Message:   req.Message,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})

	// Call LLM service for updated plan.
	// Use a detached context so client disconnects don't cancel the LLM call.
	llmCtx, llmCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer llmCancel()
	plan, err := h.callResearchPlan(llmCtx, state.Query, state.Context, state.Rounds)
	if err != nil {
		h.logger.Error("Failed to generate research plan", zap.Error(err))
		// Remove the user message we appended (don't persist bad state)
		state.Rounds = state.Rounds[:len(state.Rounds)-1]
		h.sendError(w, "Failed to generate updated plan", http.StatusBadGateway)
		return
	}

	// Record token usage (best-effort, non-blocking)
	go func() {
		rCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := h.orchClient.RecordTokenUsage(rCtx, &orchpb.RecordTokenUsageRequest{
			WorkflowId:   workflowID,
			AgentId:      "research-planner",
			Model:        plan.Model,
			Provider:     plan.Provider,
			InputTokens:  int32(plan.InputTokens),
			OutputTokens: int32(plan.OutputTokens),
		})
		if err != nil {
			h.logger.Warn("Failed to record token usage (best-effort)", zap.Error(err))
		}
	}()

	// Append assistant response
	state.Rounds = append(state.Rounds, reviewRound{
		Role:      "assistant",
		Message:   plan.Message,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	// Only update CurrentPlan when it contains actual research direction,
	// not when LLM outputs a short approval confirmation (which has no value for decompose).
	if plan.Intent != "approve" {
		state.CurrentPlan = plan.Message
	}
	state.Round++
	state.Version++

	// Save to Redis (keep original TTL)
	stateBytes, _ := json.Marshal(state)
	ttl, err := h.redis.TTL(ctx, key).Result()
	if err != nil || ttl <= 0 {
		ttl = 60 * time.Minute // fallback
	}
	h.redis.Set(ctx, key, stateBytes, ttl)

	// Determine intent (default to "feedback" if LLM didn't provide one)
	intent := plan.Intent
	if intent == "" {
		intent = "feedback"
	}

	// Publish review events to Redis stream so they're captured by SSE and persisted.
	// This makes the review conversation visible in session history on page reload.
	roundPayload := map[string]interface{}{"round": state.Round, "version": state.Version}
	h.publishStreamEvent(workflowID, "REVIEW_USER_FEEDBACK", "user", req.Message, roundPayload)
	h.publishStreamEvent(workflowID, "RESEARCH_PLAN_UPDATED", "research-planner", plan.Message,
		map[string]interface{}{"round": state.Round, "version": state.Version, "intent": intent})

	// Response
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", strconv.Itoa(state.Version))
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "reviewing",
		"plan": map[string]interface{}{
			"message": plan.Message,
			"round":   state.Round,
			"version": state.Version,
			"intent":  intent,
		},
	})
}

func (h *ReviewHandler) handleApprove(
	ctx context.Context, w http.ResponseWriter, r *http.Request,
	key string, state *reviewState, workflowID string, userCtx *auth.UserContext,
) {
	// Marshal conversation for gRPC
	convBytes, _ := json.Marshal(state.Rounds)

	// Send via dedicated gRPC (Gateway → Orchestrator → Temporal Signal)
	grpcCtx := withGRPCMetadata(ctx, r)
	_, err := h.orchClient.SubmitReviewDecision(grpcCtx, &orchpb.SubmitReviewDecisionRequest{
		WorkflowId:   workflowID,
		Approved:     true,
		FinalPlan:    state.CurrentPlan,
		Conversation: string(convBytes),
		ApprovedBy:   userCtx.UserID.String(),
	})
	if err != nil {
		h.logger.Error("Failed to submit review decision", zap.Error(err))
		h.sendError(w, "Failed to approve review", http.StatusBadGateway)
		return
	}

	// Mark review as approved in Redis (keep state for page reload, TTL handles cleanup)
	state.Status = "approved"
	if approvedBytes, err := json.Marshal(state); err == nil {
		h.redis.Set(ctx, key, approvedBytes, 60*time.Minute)
	}

	h.logger.Info("HITL review approved via gateway",
		zap.String("workflow_id", workflowID),
		zap.String("approved_by", userCtx.UserID.String()),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "approved",
		"message": "Research started",
	})
}

func (h *ReviewHandler) callResearchPlan(
	ctx context.Context,
	query string,
	taskCtx map[string]interface{},
	rounds []reviewRound,
) (*llmResearchPlanResponse, error) {
	base := os.Getenv("LLM_SERVICE_URL")
	if base == "" {
		base = "http://llm-service:8000"
	}
	url := fmt.Sprintf("%s/agent/research-plan", base)

	reqBody := llmResearchPlanRequest{
		Query:        query,
		Context:      taskCtx,
		Conversation: rounds,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("LLM service call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM service returned %d: %s", resp.StatusCode, string(body))
	}

	var result llmResearchPlanResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode LLM response: %w", err)
	}

	if result.Message == "" {
		return nil, fmt.Errorf("LLM service returned empty plan message")
	}

	return &result, nil
}

// HandleGetReview returns the current review conversation state from Redis.
func (h *ReviewHandler) HandleGetReview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workflowID := r.PathValue("workflowID")
	if workflowID == "" {
		h.sendError(w, "workflow_id is required", http.StatusBadRequest)
		return
	}

	// Auth check
	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok || userCtx == nil {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Load state from Redis
	key := fmt.Sprintf("review:%s", workflowID)
	data, err := h.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		h.sendError(w, "Review session not found or expired", http.StatusNotFound)
		return
	}
	if err != nil {
		h.logger.Error("Failed to read review state from Redis", zap.Error(err))
		h.sendError(w, "Internal error", http.StatusInternalServerError)
		return
	}

	var state reviewState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		h.logger.Error("Failed to unmarshal review state", zap.Error(err))
		h.sendError(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Owner check
	if state.OwnerUserID != "" && state.OwnerUserID != userCtx.UserID.String() {
		h.sendError(w, "Forbidden: not the task owner", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", strconv.Itoa(state.Version))
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       state.Status,
		"round":        state.Round,
		"version":      state.Version,
		"current_plan": state.CurrentPlan,
		"rounds":       state.Rounds,
		"query":        state.Query,
	})
}

// publishStreamEvent publishes a review event to the Redis event stream so it's
// captured by SSE and persisted to DB — same format as orchestrator events.
// This makes review conversation messages first-class citizens in the event system.
func (h *ReviewHandler) publishStreamEvent(workflowID, eventType, agentID, message string, payload map[string]interface{}) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	streamKey := fmt.Sprintf("shannon:workflow:events:%s", workflowID)
	seqKey := fmt.Sprintf("shannon:workflow:events:%s:seq", workflowID)

	seq, err := h.redis.Incr(ctx, seqKey).Result()
	if err != nil {
		h.logger.Warn("Failed to increment event seq", zap.Error(err))
		return
	}

	payloadJSON := "{}"
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			payloadJSON = string(b)
		}
	}

	_, err = h.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		MaxLen: 256,
		Approx: true,
		Values: map[string]interface{}{
			"workflow_id": workflowID,
			"type":        eventType,
			"agent_id":    agentID,
			"message":     message,
			"payload":     payloadJSON,
			"ts_nano":     strconv.FormatInt(time.Now().UnixNano(), 10),
			"seq":         strconv.FormatUint(uint64(seq), 10),
		},
	}).Result()
	if err != nil {
		h.logger.Warn("Failed to publish review event to stream", zap.Error(err), zap.String("type", eventType))
	} else {
		h.logger.Info("Published review event to stream",
			zap.String("workflow_id", workflowID),
			zap.String("type", eventType),
			zap.Int64("seq", seq),
		)
	}
}

func (h *ReviewHandler) sendError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
