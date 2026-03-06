package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

// ToolsHandler proxies tool operations to the Python LLM-service.
type ToolsHandler struct {
	llmServiceURL string
	db            *sqlx.DB
	quotaRecorder interface{} // Enterprise: quota.Recorder (nil in OSS)
	logger        *zap.Logger
	httpClient    *http.Client

	// In-memory metadata cache (tool name -> cached entry)
	metaCacheMu sync.RWMutex
	metaCache   map[string]*toolMetaCacheEntry
}

type toolMetaCacheEntry struct {
	metadata  toolMetadataResponse
	fetchedAt time.Time
}

const toolMetaCacheTTL = 5 * time.Minute

// Response types matching Python LLM-service.

type toolSchemaItem struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type toolMetadataResponse struct {
	Name           string  `json:"name"`
	Version        string  `json:"version"`
	Description    string  `json:"description"`
	Category       string  `json:"category"`
	Author         string  `json:"author"`
	RequiresAuth   bool    `json:"requires_auth"`
	RateLimit      *int    `json:"rate_limit"`
	TimeoutSeconds int     `json:"timeout_seconds"`
	Dangerous      bool    `json:"dangerous"`
	CostPerUse     float64 `json:"cost_per_use"`
}

type toolExecuteRequest struct {
	Arguments map[string]interface{} `json:"arguments"`
	SessionID string                 `json:"session_id,omitempty"`
}

type toolExecuteProxyRequest struct {
	ToolName       string                 `json:"tool_name"`
	Parameters     map[string]interface{} `json:"parameters"`
	SessionContext map[string]interface{} `json:"session_context,omitempty"`
}

type toolExecuteProxyResponse struct {
	Success         bool                   `json:"success"`
	Output          interface{}            `json:"output"`
	Text            *string                `json:"text"`
	Error           *string                `json:"error"`
	Metadata        map[string]interface{} `json:"metadata"`
	ExecutionTimeMs *int                   `json:"execution_time_ms"`
}

type toolExecuteResponse struct {
	Success         bool                   `json:"success"`
	Output          interface{}            `json:"output"`
	Text            *string                `json:"text"`
	Error           *string                `json:"error"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	ExecutionTimeMs *int                   `json:"execution_time_ms,omitempty"`
	Usage           *toolUsage             `json:"usage,omitempty"`
}

type toolUsage struct {
	Tokens  int     `json:"tokens"`
	CostUSD float64 `json:"cost_usd"`
}

// NewToolsHandler creates a ToolsHandler.
func NewToolsHandler(db *sqlx.DB, logger *zap.Logger) *ToolsHandler {
	llmURL := os.Getenv("LLM_SERVICE_URL")
	if llmURL == "" {
		llmURL = "http://llm-service:8000"
	}
	return &ToolsHandler{
		llmServiceURL: strings.TrimRight(llmURL, "/"),
		db:            db,
		logger:        logger,
		httpClient:    &http.Client{Timeout: 120 * time.Second},
		metaCache:     make(map[string]*toolMetaCacheEntry),
	}
}

// ListTools handles GET /api/v1/tools
// Returns tool schemas (name + description + parameters) from Python /tools/schemas.
// Always excludes dangerous tools at gateway level.
func (h *ToolsHandler) ListTools(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")

	// Build URL -- always exclude dangerous tools at gateway level
	q := url.Values{}
	q.Set("exclude_dangerous", "true")
	if category != "" {
		q.Set("category", category)
	}
	proxyURL := h.llmServiceURL + "/tools/schemas?" + q.Encode()

	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, proxyURL, nil)
	if err != nil {
		h.sendError(w, "Internal error", http.StatusInternalServerError)
		return
	}

	resp, err := h.httpClient.Do(proxyReq)
	if err != nil {
		h.logger.Error("Failed to proxy to LLM service", zap.Error(err))
		h.sendError(w, "Tool service unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.sendError(w, "Failed to read tool service response", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

// fetchToolMetadata returns cached metadata, fetching from Python if stale.
func (h *ToolsHandler) fetchToolMetadata(ctx context.Context, toolName string) (*toolMetadataResponse, error) {
	h.metaCacheMu.RLock()
	entry := h.metaCache[toolName]
	h.metaCacheMu.RUnlock()

	if entry != nil && time.Since(entry.fetchedAt) < toolMetaCacheTTL {
		return &entry.metadata, nil
	}

	proxyURL := fmt.Sprintf("%s/tools/%s/metadata", h.llmServiceURL, url.PathEscape(toolName))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, nil // tool not found
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("LLM service returned %d for tool metadata", resp.StatusCode)
	}

	var meta toolMetadataResponse
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, err
	}

	h.metaCacheMu.Lock()
	h.metaCache[toolName] = &toolMetaCacheEntry{metadata: meta, fetchedAt: time.Now()}
	h.metaCacheMu.Unlock()

	return &meta, nil
}

// GetTool handles GET /api/v1/tools/{name}
// Returns merged metadata + schema. Returns 403 for dangerous tools.
func (h *ToolsHandler) GetTool(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		h.sendError(w, "Tool name is required", http.StatusBadRequest)
		return
	}

	meta, err := h.fetchToolMetadata(r.Context(), name)
	if err != nil {
		h.logger.Error("Failed to fetch tool metadata", zap.String("tool", name), zap.Error(err))
		h.sendError(w, "Tool service unavailable", http.StatusBadGateway)
		return
	}
	if meta == nil {
		h.sendError(w, fmt.Sprintf("Tool '%s' not found", name), http.StatusNotFound)
		return
	}
	if meta.Dangerous {
		h.sendError(w, "Tool not available via direct execution", http.StatusForbidden)
		return
	}

	// Fetch schema -- check response code before decoding
	schemaURL := fmt.Sprintf("%s/tools/%s/schema", h.llmServiceURL, url.PathEscape(name))
	schemaReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, schemaURL, nil)
	if err != nil {
		h.sendError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	schemaResp, err := h.httpClient.Do(schemaReq)
	if err != nil {
		h.sendError(w, "Tool service unavailable", http.StatusBadGateway)
		return
	}
	defer schemaResp.Body.Close()

	if schemaResp.StatusCode != 200 {
		h.logger.Warn("Schema fetch failed",
			zap.String("tool", name),
			zap.Int("status", schemaResp.StatusCode))
		h.sendError(w, fmt.Sprintf("Failed to fetch schema for '%s'", name), http.StatusBadGateway)
		return
	}

	var schema toolSchemaItem
	if err := json.NewDecoder(schemaResp.Body).Decode(&schema); err != nil {
		h.sendError(w, "Failed to parse schema", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":            meta.Name,
		"description":     meta.Description,
		"category":        meta.Category,
		"version":         meta.Version,
		"parameters":      schema.Parameters,
		"timeout_seconds": meta.TimeoutSeconds,
		"cost_per_use":    meta.CostPerUse,
	})
}

// ExecuteTool handles POST /api/v1/tools/{name}/execute
func (h *ToolsHandler) ExecuteTool(w http.ResponseWriter, r *http.Request) {
	toolName := r.PathValue("name")
	if toolName == "" {
		h.sendError(w, "Tool name is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Auth context
	userCtx, ok := ctx.Value(auth.UserContextKey).(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check dangerous flag (cached)
	meta, err := h.fetchToolMetadata(ctx, toolName)
	if err != nil {
		h.logger.Error("Failed to fetch tool metadata", zap.String("tool", toolName), zap.Error(err))
		h.sendError(w, "Tool service unavailable", http.StatusBadGateway)
		return
	}
	if meta == nil {
		h.sendError(w, fmt.Sprintf("Tool '%s' not found", toolName), http.StatusNotFound)
		return
	}
	if meta.Dangerous {
		h.sendError(w, "Tool not available via direct execution", http.StatusForbidden)
		return
	}

	// Parse request body
	var req toolExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Arguments == nil {
		req.Arguments = make(map[string]interface{})
	}

	// Build session_context -- only pass keys that Python _sanitize_session_context allows:
	// session_id, user_id (tenant_id is stripped by Python sanitizer)
	sessionCtx := map[string]interface{}{
		"user_id": userCtx.UserID.String(),
	}
	if req.SessionID != "" {
		sessionCtx["session_id"] = req.SessionID
	}

	proxyBody := toolExecuteProxyRequest{
		ToolName:       toolName,
		Parameters:     req.Arguments,
		SessionContext: sessionCtx,
	}

	bodyBytes, err := json.Marshal(proxyBody)
	if err != nil {
		h.sendError(w, "Internal error", http.StatusInternalServerError)
		return
	}

	requestID := uuid.New().String()

	proxyURL := h.llmServiceURL + "/tools/execute"
	proxyReq, err := http.NewRequestWithContext(ctx, http.MethodPost, proxyURL, bytes.NewReader(bodyBytes))
	if err != nil {
		h.sendError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("X-User-ID", userCtx.UserID.String())
	proxyReq.Header.Set("X-Tenant-ID", userCtx.TenantID.String())
	proxyReq.Header.Set("X-Workflow-ID", requestID)

	// Execute
	resp, err := h.httpClient.Do(proxyReq)
	if err != nil {
		h.logger.Error("Tool execute proxy failed", zap.String("tool", toolName), zap.Error(err))
		h.sendError(w, "Tool service unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.sendError(w, "Failed to read tool response", http.StatusBadGateway)
		return
	}

	// Parse response for usage recording.
	// Only record usage if we can parse the upstream response envelope.
	var proxyResp toolExecuteProxyResponse
	parseErr := json.Unmarshal(respBody, &proxyResp)
	if parseErr != nil {
		h.logger.Warn("Failed to parse tool response, forwarding raw",
			zap.String("tool", toolName),
			zap.Error(parseErr))
		// Forward raw upstream response without quota charge
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	// Compute usage: cost_usd from response metadata, or cost_per_use from tool metadata
	costUSD := meta.CostPerUse
	if proxyResp.Metadata != nil {
		if c, ok := proxyResp.Metadata["cost_usd"].(float64); ok && c > 0 {
			costUSD = c
		}
	}

	// Synthetic tokens: cost / $0.000002, minimum 100
	syntheticTokens := 100
	if costUSD > 0 {
		computed := int(costUSD / 0.000002)
		if computed > syntheticTokens {
			syntheticTokens = computed
		}
	}
	usage := &toolUsage{Tokens: syntheticTokens, CostUSD: costUSD}

	// Fire-and-forget usage recording
	go h.recordToolUsage(userCtx.UserID, userCtx.TenantID, requestID, toolName, syntheticTokens, costUSD)

	h.logger.Info("Tool executed",
		zap.String("tool", toolName),
		zap.String("request_id", requestID),
		zap.String("user_id", userCtx.UserID.String()),
		zap.Bool("success", proxyResp.Success),
		zap.Int("tokens", syntheticTokens),
	)

	// Build response
	result := toolExecuteResponse{
		Success:         proxyResp.Success,
		Output:          proxyResp.Output,
		Text:            proxyResp.Text,
		Error:           proxyResp.Error,
		Metadata:        proxyResp.Metadata,
		ExecutionTimeMs: proxyResp.ExecutionTimeMs,
		Usage:           usage,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	json.NewEncoder(w).Encode(result)
}

// recordToolUsage writes tool usage to DB and updates tenant quota.
func (h *ToolsHandler) recordToolUsage(
	userID, _ uuid.UUID,
	requestID, toolName string,
	tokens int, costUSD float64,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := h.db.ExecContext(ctx, `
		INSERT INTO token_usage (
			user_id, task_id, agent_id, provider, model,
			prompt_tokens, completion_tokens, total_tokens, cost_usd
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, userID, nil, "tool_"+toolName, "shannon-tools", "tool_"+toolName,
		0, 0, tokens, costUSD)
	if err != nil {
		h.logger.Warn("Failed to record tool token usage",
			zap.String("request_id", requestID),
			zap.Error(err))
	}

	// Enterprise: quota recording handled by quota.Recorder when available
}

func (h *ToolsHandler) sendError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
