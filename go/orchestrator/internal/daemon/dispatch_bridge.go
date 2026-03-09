package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	dispatchStreamKey = "shannon:daemon:dispatch"
	dispatchGroup     = "daemon-hub"
	dispatchMaxLen    = 1000
)

// DispatchRequest is published to Redis by the orchestrator and consumed by the gateway Hub.
type DispatchRequest struct {
	TenantID string          `json:"tenant_id"`
	UserID   string          `json:"user_id"`
	Payload  MessagePayload  `json:"payload"`
	Meta     ClaimMetadata   `json:"meta"`
}

// PublishDispatch writes a dispatch request to the Redis stream.
// Called by DaemonDispatchActivity in the orchestrator process.
// The gateway Hub's SubscribeDispatches goroutine picks it up and dispatches to local WS connections.
func (h *Hub) PublishDispatch(ctx context.Context, req DispatchRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal dispatch request: %w", err)
	}

	_, err = h.claims.rc.XAdd(ctx, &redis.XAddArgs{
		Stream: dispatchStreamKey,
		MaxLen: dispatchMaxLen,
		Approx: true,
		Values: map[string]interface{}{"data": string(data)},
	}).Result()
	if err != nil {
		return fmt.Errorf("redis xadd dispatch: %w", err)
	}
	return nil
}

// SubscribeDispatches reads dispatch requests from the Redis stream and dispatches
// them to locally connected WebSocket daemons. Only the gateway process should call this.
// Blocks until ctx is cancelled.
func (h *Hub) SubscribeDispatches(ctx context.Context) {
	rc := h.claims.rc

	// Create consumer group (idempotent)
	rc.XGroupCreateMkStream(ctx, dispatchStreamKey, dispatchGroup, "0")

	consumerName := fmt.Sprintf("hub-%d", time.Now().UnixNano())

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		streams, err := rc.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    dispatchGroup,
			Consumer: consumerName,
			Streams:  []string{dispatchStreamKey, ">"},
			Count:    10,
			Block:    5 * time.Second,
		}).Result()
		if err != nil {
			if err == redis.Nil || ctx.Err() != nil {
				continue
			}
			h.logger.Warn("dispatch subscribe read error", zap.Error(err))
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				h.processDispatchMessage(ctx, rc, msg)
			}
		}
	}
}

func (h *Hub) processDispatchMessage(ctx context.Context, rc redis.Cmdable, msg redis.XMessage) {
	data, ok := msg.Values["data"].(string)
	if !ok {
		rc.XAck(ctx, dispatchStreamKey, dispatchGroup, msg.ID)
		return
	}

	var req DispatchRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		h.logger.Warn("invalid dispatch message", zap.Error(err))
		rc.XAck(ctx, dispatchStreamKey, dispatchGroup, msg.ID)
		return
	}

	// Dispatch to local WS connections
	err := h.Dispatch(ctx, req.TenantID, req.UserID, req.Payload, req.Meta)
	if err != nil {
		h.logger.Warn("dispatch from stream failed",
			zap.String("tenant_id", req.TenantID),
			zap.Error(err),
		)
	}

	// ACK regardless — if no daemon is connected, retrying won't help
	rc.XAck(ctx, dispatchStreamKey, dispatchGroup, msg.ID)
}
