package daemon

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 20 * time.Second
)

// ReplyCallback is called when a daemon sends a reply. The Hub calls this
// to route replies back to the originating channel.
type ReplyCallback func(ctx context.Context, meta ClaimMetadata, reply ReplyPayload)

// HandleConnection runs the read/write loop for a single daemon WebSocket connection.
// Blocks until the connection is closed.
func HandleConnection(ctx context.Context, hub *Hub, ws *websocket.Conn, tenantID, userID string, onReply ReplyCallback, logger *zap.Logger) {
	connID := uuid.New().String()

	conn := &DaemonConn{
		ID:          connID,
		TenantID:    tenantID,
		UserID:      userID,
		ConnectedAt: time.Now(),
		LastActive:  time.Now(),
		sendFn: func(msg ServerMessage) error {
			ws.SetWriteDeadline(time.Now().Add(writeWait))
			return ws.WriteJSON(msg)
		},
	}

	hub.Register(conn)
	defer func() {
		released, _ := hub.HandleDisconnect(ctx, connID)
		if len(released) > 0 {
			logger.Warn("daemon disconnected with active claims",
				zap.String("conn_id", connID),
				zap.Strings("released_messages", released),
			)
		}
		ws.Close()
	}()

	// Send connected confirmation.
	conn.Send(ServerMessage{Type: MsgTypeConnected})

	// Configure WebSocket.
	ws.SetReadLimit(64 * 1024)
	ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Ping ticker.
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	// Read pump in separate goroutine.
	msgCh := make(chan DaemonMessage, 16)
	go func() {
		defer close(msgCh)
		for {
			var msg DaemonMessage
			if err := ws.ReadJSON(&msg); err != nil {
				return
			}
			msgCh <- msg
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			handleDaemonMessage(ctx, hub, conn, msg, onReply, logger)
		case <-ticker.C:
			// Acquire conn mutex — gorilla/websocket requires single-writer serialization.
			// Send() also holds this lock, so pings are serialized with data writes.
			conn.mu.Lock()
			ws.SetWriteDeadline(time.Now().Add(writeWait))
			err := ws.WriteMessage(websocket.PingMessage, nil)
			conn.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func handleDaemonMessage(ctx context.Context, hub *Hub, conn *DaemonConn, msg DaemonMessage, onReply ReplyCallback, logger *zap.Logger) {
	if msg.Type == "" {
		return // Ignore empty-type messages (e.g. keep-alive frames)
	}

	if msg.MessageID == "" && (msg.Type == MsgTypeClaim || msg.Type == MsgTypeReply || msg.Type == MsgTypeProgress) {
		logger.Warn("daemon message missing message_id", zap.String("type", msg.Type))
		return
	}

	switch msg.Type {
	case MsgTypeClaim:
		granted, err := hub.HandleClaim(ctx, conn.ID, msg.MessageID, ClaimMetadata{
			ConnID:    conn.ID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			logger.Error("claim failed", zap.Error(err), zap.String("message_id", msg.MessageID))
			return
		}
		ackPayload, _ := json.Marshal(ClaimAckPayload{Granted: granted})
		conn.Send(ServerMessage{
			Type:      MsgTypeClaimAck,
			MessageID: msg.MessageID,
			Payload:   ackPayload,
		})

	case MsgTypeProgress:
		if err := hub.HandleProgress(ctx, msg.MessageID); err != nil {
			logger.Warn("progress heartbeat failed", zap.Error(err), zap.String("message_id", msg.MessageID))
		}

	case MsgTypeReply:
		meta, err := hub.HandleReply(ctx, msg.MessageID)
		if err != nil {
			logger.Warn("reply for unknown claim", zap.Error(err), zap.String("message_id", msg.MessageID))
			return
		}
		var reply ReplyPayload
		if err := json.Unmarshal(msg.Payload, &reply); err != nil {
			logger.Error("invalid reply payload", zap.Error(err))
			return
		}
		if onReply != nil {
			onReply(ctx, meta, reply)
		}
		logger.Info("daemon reply received",
			zap.String("message_id", msg.MessageID),
			zap.String("channel_type", meta.ChannelType),
			zap.String("conn_id", conn.ID),
		)

	case MsgTypeDisconnect:
		logger.Info("daemon graceful disconnect", zap.String("conn_id", conn.ID))

	default:
		logger.Warn("unknown daemon message type", zap.String("type", msg.Type))
	}
}
