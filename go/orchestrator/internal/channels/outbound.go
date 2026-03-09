package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/daemon"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// OutboundRouter sends daemon replies back to the originating channel (Slack, LINE, etc.).
type OutboundRouter struct {
	registry *Registry
	client   *http.Client
	logger   *zap.Logger
}

// NewOutboundRouter creates an OutboundRouter backed by the channel registry.
func NewOutboundRouter(registry *Registry, logger *zap.Logger) *OutboundRouter {
	return &OutboundRouter{
		registry: registry,
		client:   &http.Client{Timeout: 10 * time.Second},
		logger:   logger,
	}
}

// RouteReply looks up the channel from the claim metadata and dispatches the
// reply to the appropriate channel API.
func (or *OutboundRouter) RouteReply(ctx context.Context, meta daemon.ClaimMetadata, reply daemon.ReplyPayload) error {
	if meta.ChannelID == "" {
		or.logger.Warn("no channel_id in claim metadata, skipping outbound")
		return nil
	}

	channelID, err := uuid.Parse(meta.ChannelID)
	if err != nil {
		return fmt.Errorf("invalid channel_id: %w", err)
	}

	ch, err := or.registry.Get(ctx, channelID)
	if err != nil {
		return fmt.Errorf("load channel: %w", err)
	}

	var routeErr error
	switch ch.Type {
	case "slack":
		routeErr = or.sendSlackReply(ctx, ch, meta, reply)
	case "line":
		routeErr = or.sendLINEReply(ctx, ch, meta, reply)
	case "teams":
		routeErr = fmt.Errorf("teams outbound not yet implemented")
	case "wechat":
		routeErr = fmt.Errorf("wechat outbound not yet implemented")
	case "schedule":
		// Schedule replies are handled via Temporal signals, not channel API.
		return nil
	default:
		routeErr = fmt.Errorf("unsupported outbound channel type: %s", ch.Type)
	}

	if routeErr != nil {
		daemon.OutboundFailures.WithLabelValues(ch.Type).Inc()
	}
	return routeErr
}

func (or *OutboundRouter) sendSlackReply(ctx context.Context, ch *Channel, meta daemon.ClaimMetadata, reply daemon.ReplyPayload) error {
	var creds slackCredentials
	if err := json.Unmarshal(ch.Credentials, &creds); err != nil {
		return fmt.Errorf("invalid slack credentials: %w", err)
	}

	slackChannel, threadTS := parseSlackThreadID(meta.ThreadID)
	if slackChannel == "" {
		return fmt.Errorf("invalid slack thread_id: %s", meta.ThreadID)
	}

	payload := map[string]interface{}{
		"channel": slackChannel,
		"text":    reply.Text,
	}
	if threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+creds.BotToken)

	resp, err := or.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack API call failed: %w", err)
	}
	defer resp.Body.Close()

	// Slack Web API returns HTTP 200 even on logical errors — must decode body
	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&slackResp); err != nil {
		return fmt.Errorf("slack API response decode failed: %w", err)
	}
	if !slackResp.OK {
		return fmt.Errorf("slack API error: %s", slackResp.Error)
	}

	or.logger.Info("slack reply sent",
		zap.String("channel", slackChannel),
		zap.String("thread_ts", threadTS),
	)
	return nil
}

func (or *OutboundRouter) sendLINEReply(ctx context.Context, ch *Channel, meta daemon.ClaimMetadata, reply daemon.ReplyPayload) error {
	var creds lineCredentials
	if err := json.Unmarshal(ch.Credentials, &creds); err != nil {
		return fmt.Errorf("invalid LINE credentials: %w", err)
	}

	// ThreadID = LINE user ID (for Push Message API).
	payload := map[string]interface{}{
		"to": meta.ThreadID,
		"messages": []map[string]string{
			{"type": "text", "text": reply.Text},
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.line.me/v2/bot/message/push", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+creds.ChannelAccessToken)

	resp, err := or.client.Do(req)
	if err != nil {
		return fmt.Errorf("LINE API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// LINE API returns error details in the body
		var lineErr struct {
			Message string `json:"message"`
		}
		json.NewDecoder(resp.Body).Decode(&lineErr)
		return fmt.Errorf("LINE API returned %d: %s", resp.StatusCode, lineErr.Message)
	}

	or.logger.Info("LINE reply sent", zap.String("to", meta.ThreadID))
	return nil
}

// parseSlackThreadID splits "C07ABCDEF-1234567890.123456" into channel and thread_ts.
func parseSlackThreadID(threadID string) (channel, threadTS string) {
	idx := strings.LastIndex(threadID, "-")
	if idx < 0 {
		return threadID, ""
	}
	return threadID[:idx], threadID[idx+1:]
}
