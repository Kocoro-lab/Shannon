package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/channels"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var validChannelTypes = map[string]bool{
	"slack":  true,
	"line":   true,
	"teams":  true,
	"wechat": true,
}

type ChannelHandler struct {
	registry *channels.Registry
	logger   *zap.Logger
}

func NewChannelHandler(registry *channels.Registry, logger *zap.Logger) *ChannelHandler {
	return &ChannelHandler{
		registry: registry,
		logger:   logger,
	}
}

// Create handles POST /api/v1/channels
func (h *ChannelHandler) Create(w http.ResponseWriter, r *http.Request) {
	userCtx, ok := r.Context().Value(auth.UserContextKey).(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req channels.CreateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Type == "" || req.Name == "" {
		h.sendError(w, "type and name are required", http.StatusBadRequest)
		return
	}
	if !validChannelTypes[req.Type] {
		h.sendError(w, "Invalid channel type. Must be one of: slack, line, teams, wechat", http.StatusBadRequest)
		return
	}
	if len(req.Credentials) == 0 {
		h.sendError(w, "credentials are required", http.StatusBadRequest)
		return
	}

	userID := userCtx.UserID

	ch := &channels.Channel{
		UserID:      &userID,
		Type:        req.Type,
		Name:        req.Name,
		Credentials: req.Credentials,
		Config:      req.Config,
		Enabled:     true,
	}
	if ch.Config == nil {
		ch.Config = json.RawMessage(`{}`)
	}

	if err := h.registry.Create(r.Context(), ch); err != nil {
		h.logger.Error("Failed to create channel", zap.Error(err))
		h.sendError(w, "Failed to create channel", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Channel created",
		zap.String("channel_id", ch.ID.String()),
		zap.String("user_id", userID.String()),
		zap.String("type", ch.Type),
	)

	ch.Credentials = nil
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ch)
}

// List handles GET /api/v1/channels
func (h *ChannelHandler) List(w http.ResponseWriter, r *http.Request) {
	userCtx, ok := r.Context().Value(auth.UserContextKey).(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	chs, err := h.registry.ListByUser(r.Context(), userCtx.UserID)
	if err != nil {
		h.logger.Error("Failed to list channels", zap.Error(err))
		h.sendError(w, "Failed to list channels", http.StatusInternalServerError)
		return
	}

	for i := range chs {
		chs[i].Credentials = nil
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chs)
}

// Get handles GET /api/v1/channels/{id}
func (h *ChannelHandler) Get(w http.ResponseWriter, r *http.Request) {
	_, ok := r.Context().Value(auth.UserContextKey).(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	if idStr == "" {
		h.sendError(w, "Channel ID required", http.StatusBadRequest)
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.sendError(w, "Invalid channel ID", http.StatusBadRequest)
		return
	}

	ch, err := h.registry.Get(r.Context(), id)
	if err != nil {
		h.sendError(w, "Channel not found", http.StatusNotFound)
		return
	}

	ch.Credentials = nil
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ch)
}

// Update handles PUT /api/v1/channels/{id}
func (h *ChannelHandler) Update(w http.ResponseWriter, r *http.Request) {
	userCtx, ok := r.Context().Value(auth.UserContextKey).(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	if idStr == "" {
		h.sendError(w, "Channel ID required", http.StatusBadRequest)
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.sendError(w, "Invalid channel ID", http.StatusBadRequest)
		return
	}

	var req channels.UpdateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.registry.Update(r.Context(), id, req); err != nil {
		h.logger.Error("Failed to update channel", zap.Error(err))
		h.sendError(w, "Failed to update channel", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Channel updated",
		zap.String("channel_id", id.String()),
		zap.String("user_id", userCtx.UserID.String()),
	)

	updated, err := h.registry.Get(r.Context(), id)
	if err != nil {
		h.sendError(w, "Failed to retrieve updated channel", http.StatusInternalServerError)
		return
	}
	updated.Credentials = nil
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

// Delete handles DELETE /api/v1/channels/{id}
func (h *ChannelHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userCtx, ok := r.Context().Value(auth.UserContextKey).(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	if idStr == "" {
		h.sendError(w, "Channel ID required", http.StatusBadRequest)
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.sendError(w, "Invalid channel ID", http.StatusBadRequest)
		return
	}

	if err := h.registry.Delete(r.Context(), id); err != nil {
		h.logger.Error("Failed to delete channel", zap.Error(err))
		h.sendError(w, "Failed to delete channel", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Channel deleted",
		zap.String("channel_id", id.String()),
		zap.String("user_id", userCtx.UserID.String()),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Channel deleted"})
}

func (h *ChannelHandler) sendError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
