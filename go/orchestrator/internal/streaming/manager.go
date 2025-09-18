package streaming

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// Event is a minimal streaming event used by SSE and future gRPC.
type Event struct {
	WorkflowID string    `json:"workflow_id"`
	Type       string    `json:"type"`
	AgentID    string    `json:"agent_id,omitempty"`
	Message    string    `json:"message,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	Seq        uint64    `json:"seq"`
	StreamID   string    `json:"stream_id,omitempty"`  // Redis stream ID for deduplication
}

// Manager provides Redis Streams-based pub/sub for workflow events.
type Manager struct {
	mu          sync.RWMutex
	redis       *redis.Client
	subscribers map[string]map[chan Event]struct{}
	capacity    int
	logger      *zap.Logger
}

var (
	defaultMgr      *Manager
	once            sync.Once
	defaultCapacity = 256
)

// Get returns the global streaming manager, initializing it lazily.
func Get() *Manager {
	once.Do(func() {
		// This will be properly initialized via InitializeRedis
		defaultMgr = &Manager{
			subscribers: make(map[string]map[chan Event]struct{}),
			capacity:    defaultCapacity,
			logger:      zap.L(),
		}
	})
	return defaultMgr
}

// InitializeRedis initializes the manager with a Redis client
func InitializeRedis(redisClient *redis.Client, logger *zap.Logger) {
	if defaultMgr == nil {
		Get()
	}
	defaultMgr.mu.Lock()
	defer defaultMgr.mu.Unlock()
	defaultMgr.redis = redisClient
	if logger != nil {
		defaultMgr.logger = logger
	}
}

// Configure sets default capacity for new/empty managers and rings.
func Configure(capacity int) {
	if capacity <= 0 {
		return
	}
	defaultCapacity = capacity
	if defaultMgr != nil {
		defaultMgr.mu.Lock()
		defaultMgr.capacity = capacity
		defaultMgr.mu.Unlock()
	}
}

// streamKey returns the Redis stream key for a workflow
func (m *Manager) streamKey(workflowID string) string {
	return fmt.Sprintf("shannon:workflow:events:%s", workflowID)
}

// seqKey returns the Redis key for sequence counter
func (m *Manager) seqKey(workflowID string) string {
	return fmt.Sprintf("shannon:workflow:events:%s:seq", workflowID)
}

// Subscribe adds a subscriber channel for a workflowID; caller must drain and call Unsubscribe.
func (m *Manager) Subscribe(workflowID string, buffer int) chan Event {
	return m.SubscribeFrom(workflowID, buffer, "0-0")
}

// SubscribeFrom adds a subscriber starting from a specific stream ID
func (m *Manager) SubscribeFrom(workflowID string, buffer int, startID string) chan Event {
	ch := make(chan Event, buffer)
	m.mu.Lock()
	subs := m.subscribers[workflowID]
	if subs == nil {
		subs = make(map[chan Event]struct{})
		m.subscribers[workflowID] = subs
	}
	subs[ch] = struct{}{}
	m.mu.Unlock()

	// Start Redis stream reader goroutine with specific start position
	go m.streamReaderFrom(workflowID, ch, startID)

	return ch
}

// streamReader reads from Redis stream and forwards to channel
func (m *Manager) streamReader(workflowID string, ch chan Event) {
	m.streamReaderFrom(workflowID, ch, "0-0")
}

// streamReaderFrom reads from Redis stream starting from specific ID
func (m *Manager) streamReaderFrom(workflowID string, ch chan Event, startID string) {
	if m.redis == nil {
		m.logger.Warn("Redis client not initialized for streaming")
		return
	}

	ctx := context.Background()
	streamKey := m.streamKey(workflowID)
	lastID := startID

	m.logger.Debug("Starting stream reader",
		zap.String("workflow_id", workflowID),
		zap.String("stream_key", streamKey),
		zap.String("start_id", lastID))

	for {
		// Check if channel is still subscribed
		m.mu.RLock()
		subs, ok := m.subscribers[workflowID]
		if !ok {
			m.mu.RUnlock()
			m.logger.Debug("Stream reader stopping - workflow unsubscribed",
				zap.String("workflow_id", workflowID))
			close(ch) // Reader closes the channel
			break
		}
		if _, exists := subs[ch]; !exists {
			m.mu.RUnlock()
			m.logger.Debug("Stream reader stopping - channel unsubscribed",
				zap.String("workflow_id", workflowID))
			close(ch) // Reader closes the channel
			break
		}
		m.mu.RUnlock()

		// Read from stream with blocking
		result, err := m.redis.XRead(ctx, &redis.XReadArgs{
			Streams: []string{streamKey, lastID},
			Count:   10,
			Block:   5 * time.Second,
		}).Result()

		if err == redis.Nil {
			// Timeout, no new messages
			continue
		}
		if err != nil {
			m.logger.Error("Failed to read from Redis stream",
				zap.String("workflow_id", workflowID),
				zap.String("stream_key", streamKey),
				zap.String("last_id", lastID),
				zap.Error(err))
			time.Sleep(1 * time.Second)
			continue
		}

		// Process messages
		for _, stream := range result {
			for _, message := range stream.Messages {
				lastID = message.ID

				// Parse event from Redis stream
				event := Event{
					WorkflowID: workflowID,
					StreamID:   message.ID,
				}

				if v, ok := message.Values["type"].(string); ok {
					event.Type = v
				}
				if v, ok := message.Values["agent_id"].(string); ok {
					event.AgentID = v
				}
				if v, ok := message.Values["message"].(string); ok {
					event.Message = v
				}
				if v, ok := message.Values["seq"].(string); ok {
					if seq, err := strconv.ParseUint(v, 10, 64); err == nil {
						event.Seq = seq
					}
				}
				if v, ok := message.Values["ts_nano"].(string); ok {
					if nano, err := strconv.ParseInt(v, 10, 64); err == nil {
						event.Timestamp = time.Unix(0, nano)
					}
				}

				// Send to channel (non-blocking)
				select {
				case ch <- event:
					m.logger.Debug("Sent event to subscriber",
						zap.String("workflow_id", workflowID),
						zap.String("type", event.Type),
						zap.Uint64("seq", event.Seq),
						zap.String("stream_id", message.ID))
				default:
					// Drop if subscriber is slow
					m.logger.Warn("Dropped event - subscriber slow",
						zap.String("workflow_id", workflowID),
						zap.String("type", event.Type),
						zap.Uint64("seq", event.Seq))
				}
			}
		}
	}
}

// Unsubscribe removes the subscriber channel (channel should be closed by reader).
func (m *Manager) Unsubscribe(workflowID string, ch chan Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if subs, ok := m.subscribers[workflowID]; ok {
		delete(subs, ch)
		// Don't close channel here - let the reader detect and close it
		if len(subs) == 0 {
			delete(m.subscribers, workflowID)
		}
	}
}

// Publish sends an event to Redis stream and all local subscribers (for backward compatibility)
func (m *Manager) Publish(workflowID string, evt Event) {
	if m.redis != nil {
		ctx := context.Background()

		// Increment sequence number
		seq, err := m.redis.Incr(ctx, m.seqKey(workflowID)).Result()
		if err != nil {
			m.logger.Error("Failed to increment sequence",
				zap.String("workflow_id", workflowID),
				zap.Error(err))
			seq = 0
		}
		evt.Seq = uint64(seq)

		// Add to Redis stream
		streamKey := m.streamKey(workflowID)
		streamID, err := m.redis.XAdd(ctx, &redis.XAddArgs{
			Stream: streamKey,
			MaxLen: int64(m.capacity),
			Approx: true,
			Values: map[string]interface{}{
				"workflow_id": evt.WorkflowID,
				"type":        evt.Type,
				"agent_id":    evt.AgentID,
				"message":     evt.Message,
				"ts_nano":     strconv.FormatInt(evt.Timestamp.UnixNano(), 10),
				"seq":         strconv.FormatUint(evt.Seq, 10),
			},
		}).Result()

		if err != nil {
			m.logger.Error("Failed to publish to Redis stream",
				zap.String("workflow_id", workflowID),
				zap.Error(err))
		} else {
			evt.StreamID = streamID // Store the Redis stream ID
			m.logger.Debug("Published event to Redis stream",
				zap.String("workflow_id", workflowID),
				zap.String("type", evt.Type),
				zap.Uint64("seq", evt.Seq),
				zap.String("stream_id", streamID))
		}

		// Set TTL on stream key (24 hours)
		m.redis.Expire(ctx, streamKey, 24*time.Hour)
		m.redis.Expire(ctx, m.seqKey(workflowID), 24*time.Hour)
	}

	// Only publish to local subscribers if Redis is nil (in-memory mode)
	// When Redis is available, the streamReader will deliver events
	if m.redis == nil {
		m.mu.RLock()
		subs := m.subscribers[workflowID]
		m.mu.RUnlock()
		if len(subs) == 0 {
			return
		}
		for ch := range subs {
			select {
			case ch <- evt:
			default:
				// Drop if subscriber is slow
			}
		}
	}
}

// Marshal returns JSON for event payloads in SSE or logs.
func (e Event) Marshal() []byte {
	b, _ := json.Marshal(e)
	return b
}

// ReplaySince returns events with Seq > since (from Redis stream)
func (m *Manager) ReplaySince(workflowID string, since uint64) []Event {
	if m.redis == nil {
		return nil
	}

	ctx := context.Background()
	streamKey := m.streamKey(workflowID)

	// Read all messages from the stream
	messages, err := m.redis.XRange(ctx, streamKey, "-", "+").Result()
	if err != nil {
		m.logger.Error("Failed to read replay from Redis stream",
			zap.String("workflow_id", workflowID),
			zap.Error(err))
		return nil
	}

	var events []Event
	for _, msg := range messages {
		event := Event{
			WorkflowID: workflowID,
			StreamID:   msg.ID,
		}

		// Parse sequence
		if v, ok := msg.Values["seq"].(string); ok {
			if seq, err := strconv.ParseUint(v, 10, 64); err == nil {
				event.Seq = seq
				// Skip if not after 'since'
				if seq <= since {
					continue
				}
			}
		}

		// Parse other fields
		if v, ok := msg.Values["type"].(string); ok {
			event.Type = v
		}
		if v, ok := msg.Values["agent_id"].(string); ok {
			event.AgentID = v
		}
		if v, ok := msg.Values["message"].(string); ok {
			event.Message = v
		}
		if v, ok := msg.Values["ts_nano"].(string); ok {
			if nano, err := strconv.ParseInt(v, 10, 64); err == nil {
				event.Timestamp = time.Unix(0, nano)
			}
		}

		events = append(events, event)
	}

	return events
}

// ReplayFromStreamID returns events starting from a specific Redis stream ID
func (m *Manager) ReplayFromStreamID(workflowID string, streamID string) []Event {
	if m.redis == nil {
		return nil
	}

	ctx := context.Background()
	streamKey := m.streamKey(workflowID)

	// Read messages after the given stream ID
	messages, err := m.redis.XRange(ctx, streamKey, "("+streamID, "+").Result()
	if err != nil {
		m.logger.Error("Failed to read replay from Redis stream",
			zap.String("workflow_id", workflowID),
			zap.String("stream_id", streamID),
			zap.Error(err))
		return nil
	}

	var events []Event
	for _, msg := range messages {
		event := Event{
			WorkflowID: workflowID,
			StreamID:   msg.ID,
		}

		// Parse fields
		if v, ok := msg.Values["seq"].(string); ok {
			if seq, err := strconv.ParseUint(v, 10, 64); err == nil {
				event.Seq = seq
			}
		}
		if v, ok := msg.Values["type"].(string); ok {
			event.Type = v
		}
		if v, ok := msg.Values["agent_id"].(string); ok {
			event.AgentID = v
		}
		if v, ok := msg.Values["message"].(string); ok {
			event.Message = v
		}
		if v, ok := msg.Values["ts_nano"].(string); ok {
			if nano, err := strconv.ParseInt(v, 10, 64); err == nil {
				event.Timestamp = time.Unix(0, nano)
			}
		}

		events = append(events, event)
	}

	return events
}

// GetLastStreamID returns the ID of the last message in the stream
func (m *Manager) GetLastStreamID(workflowID string) string {
	if m.redis == nil {
		return ""
	}

	ctx := context.Background()
	streamKey := m.streamKey(workflowID)

	// Get only the last message efficiently with XRevRangeN
	messages, err := m.redis.XRevRangeN(ctx, streamKey, "+", "-", 1).Result()
	if err != nil || len(messages) == 0 {
		return ""
	}

	return messages[0].ID
}
