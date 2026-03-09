package channels

import (
	"encoding/json"
	"net/http"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/daemon"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// InboundHandler dispatches incoming channel webhooks to the daemon hub.
type InboundHandler struct {
	registry *Registry
	hub      *daemon.Hub
	logger   *zap.Logger
}

// NewInboundHandler creates a new InboundHandler.
func NewInboundHandler(registry *Registry, hub *daemon.Hub, logger *zap.Logger) *InboundHandler {
	return &InboundHandler{registry: registry, hub: hub, logger: logger}
}

// HandleWebhook is the common entry point for all inbound channel webhooks.
// It looks up the channel by ID, verifies it is enabled, delegates to the
// channel-specific handler for signature verification and message extraction,
// then dispatches the resulting message to the daemon hub.
func (ih *InboundHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	channelID, err := uuid.Parse(r.PathValue("channel_id"))
	if err != nil {
		http.Error(w, `{"error":"invalid channel_id"}`, http.StatusBadRequest)
		return
	}

	ch, err := ih.registry.Get(r.Context(), channelID)
	if err != nil {
		http.Error(w, `{"error":"channel not found"}`, http.StatusNotFound)
		return
	}
	if !ch.Enabled {
		http.Error(w, `{"error":"channel disabled"}`, http.StatusForbidden)
		return
	}

	var msg *daemon.MessagePayload
	switch ch.Type {
	case "slack":
		msg, err = handleSlackWebhook(w, r, ch)
	case "line":
		msg, err = handleLINEWebhook(w, r, ch)
	case "teams":
		msg, err = handleTeamsWebhook(w, r, ch)
	case "wechat":
		msg, err = handleWeChatWebhook(w, r, ch)
	default:
		http.Error(w, `{"error":"unsupported channel type"}`, http.StatusBadRequest)
		return
	}

	if err != nil {
		ih.logger.Error("webhook handler failed",
			zap.String("channel_id", channelID.String()),
			zap.String("type", ch.Type),
			zap.Error(err),
		)
		http.Error(w, `{"error":"webhook processing failed"}`, http.StatusInternalServerError)
		return
	}
	if msg == nil {
		// Handler already wrote response (e.g., Slack URL verification challenge).
		return
	}

	// Extract agent_name from channel config.
	var cfg struct {
		AgentName string `json:"agent_name"`
	}
	_ = json.Unmarshal(ch.Config, &cfg)
	msg.AgentName = cfg.AgentName

	userID := ""
	if ch.UserID != nil {
		userID = ch.UserID.String()
	}

	claimMeta := daemon.ClaimMetadata{
		ChannelID:   channelID.String(),
		ChannelType: ch.Type,
		ThreadID:    msg.ThreadID,
	}

	// OSS is single-tenant; pass empty string for tenantID.
	if err := ih.hub.Dispatch(r.Context(), "", userID, *msg, claimMeta); err != nil {
		if err == daemon.ErrNoDaemonConnected {
			ih.logger.Warn("no daemon connected for webhook",
				zap.String("channel_id", channelID.String()),
			)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"no_daemon_connected"}`))
			return
		}
		http.Error(w, `{"error":"dispatch failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"dispatched"}`))
}
