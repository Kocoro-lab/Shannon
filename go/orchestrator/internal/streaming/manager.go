package streaming

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/db"
	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// Event is a minimal streaming event used by SSE and future gRPC.
type Event struct {
	WorkflowID string                 `json:"workflow_id"`
	Type       string                 `json:"type"`
	AgentID    string                 `json:"agent_id,omitempty"`
	Message    string                 `json:"message,omitempty"`
	Payload    map[string]interface{} `json:"payload,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	Seq        uint64                 `json:"seq"`
	StreamID   string                 `json:"stream_id,omitempty"` // Redis stream ID for deduplication
}

// subscription tracks a subscriber with its cancellation mechanism
type subscription struct {
	cancel context.CancelFunc
}

// Manager provides Redis Streams-based pub/sub for workflow events.
//
// Lifecycle:
//  1. Subscribe() creates a channel and starts a background reader goroutine
//  2. The reader forwards Redis stream events to the channel
//  3. Unsubscribe() stops the reader and closes the channel
//
// IMPORTANT: Callers must NOT close subscription channels themselves.
// The reader owns the channel lifetime. Always call Unsubscribe() to clean up.
//
// Thread-safety: All methods are goroutine-safe.
type Manager struct {
	mu          sync.RWMutex
	redis       *redis.Client
	dbClient    *db.Client
	persistCh   chan db.EventLog
	batchSize   int
	flushEvery  time.Duration
	subscribers map[string]map[chan Event]*subscription
	capacity    int
	logger      *zap.Logger
	shutdownCh  chan struct{}
	wg          sync.WaitGroup
	persistWg   sync.WaitGroup
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
			subscribers: make(map[string]map[chan Event]*subscription),
			capacity:    defaultCapacity,
			logger:      zap.L(),
			shutdownCh:  make(chan struct{}),
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

// InitializeEventStore sets the persistent store for events.
func InitializeEventStore(store *db.Client, logger *zap.Logger) {
	if defaultMgr == nil {
		Get()
	}
	defaultMgr.mu.Lock()
	defer defaultMgr.mu.Unlock()
	defaultMgr.dbClient = store
	if logger != nil {
		defaultMgr.logger = logger
	}
	if defaultMgr.persistCh == nil {
		// Configure batching from env
		bs := 100
		if v := os.Getenv("EVENTLOG_BATCH_SIZE"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				bs = n
			}
		}
		iv := 100 * time.Millisecond
		if v := os.Getenv("EVENTLOG_BATCH_INTERVAL_MS"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				iv = time.Duration(n) * time.Millisecond
			}
		}
		defaultMgr.persistCh = make(chan db.EventLog, bs*4)
		defaultMgr.batchSize = bs
		defaultMgr.flushEvery = iv
		defaultMgr.persistWg.Add(1)
		go defaultMgr.persistWorker()
		defaultMgr.logger.Info("Initialized event log batcher", zap.Int("batch_size", bs), zap.Duration("interval", iv))
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

	// Create context with cancellation for this subscription
	ctx, cancel := context.WithCancel(context.Background())

	m.mu.Lock()
	subs := m.subscribers[workflowID]
	if subs == nil {
		subs = make(map[chan Event]*subscription)
		m.subscribers[workflowID] = subs
	}
	subs[ch] = &subscription{cancel: cancel}
	m.mu.Unlock()

	// Start Redis stream reader goroutine with specific start position
	m.wg.Add(1)
	go m.streamReaderFrom(ctx, workflowID, ch, startID)

	return ch
}

// streamReaderFrom reads from Redis stream starting from specific ID with context support
func (m *Manager) streamReaderFrom(ctx context.Context, workflowID string, ch chan Event, startID string) {
	defer m.wg.Done()
	defer close(ch) // Always close channel when reader exits

	if m.redis == nil {
		// In-memory mode: keep channel open until cancelled
		select {
		case <-ctx.Done():
		case <-m.shutdownCh:
		}
		return
	}

	streamKey := m.streamKey(workflowID)
	lastID := startID
	retryDelay := time.Second
	maxRetryDelay := 30 * time.Second

	m.logger.Debug("Starting stream reader",
		zap.String("workflow_id", workflowID),
		zap.String("stream_key", streamKey),
		zap.String("start_id", lastID))

	for {
		// Check for context cancellation or shutdown
		select {
		case <-ctx.Done():
			m.logger.Debug("Stream reader stopping - context cancelled",
				zap.String("workflow_id", workflowID))
			return
		case <-m.shutdownCh:
			m.logger.Debug("Stream reader stopping - manager shutdown",
				zap.String("workflow_id", workflowID))
			return
		default:
		}

		// Read from stream with blocking
		result, err := m.redis.XRead(ctx, &redis.XReadArgs{
			Streams: []string{streamKey, lastID},
			Count:   10,
			Block:   5 * time.Second,
		}).Result()

		if err == redis.Nil {
			// Timeout, no new messages - reset retry delay
			retryDelay = time.Second
			continue
		}

		if err != nil {
			// Check if context was cancelled
			if ctx.Err() != nil {
				return
			}

			m.logger.Error("Failed to read from Redis stream",
				zap.String("workflow_id", workflowID),
				zap.String("stream_key", streamKey),
				zap.String("last_id", lastID),
				zap.Duration("retry_in", retryDelay),
				zap.Error(err))

			// Exponential backoff on errors
			select {
			case <-time.After(retryDelay):
				retryDelay = min(retryDelay*2, maxRetryDelay)
			case <-ctx.Done():
				return
			case <-m.shutdownCh:
				return
			}
			continue
		}

		// Success - reset retry delay
		retryDelay = time.Second

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
				if v, ok := message.Values["payload"].(string); ok && v != "" {
					var p map[string]interface{}
					if err := json.Unmarshal([]byte(v), &p); err == nil {
						event.Payload = p
					}
				}

				// Send to channel (non-blocking to avoid deadlock)
				select {
				case ch <- event:
					m.logger.Debug("Sent event to subscriber",
						zap.String("workflow_id", workflowID),
						zap.String("type", event.Type),
						zap.Uint64("seq", event.Seq),
						zap.String("stream_id", message.ID))
				default:
					// Escalate log severity for critical events
					if isCriticalEvent(event.Type) {
						m.logger.Error("CRITICAL: Dropped important event - subscriber slow",
							zap.String("workflow_id", workflowID),
							zap.String("type", event.Type),
							zap.Uint64("seq", event.Seq))
					} else {
						m.logger.Warn("Dropped event - subscriber slow",
							zap.String("workflow_id", workflowID),
							zap.String("type", event.Type),
							zap.Uint64("seq", event.Seq))
					}
				}
			}
		}
	}
}

// min returns the minimum of two durations
func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// isCriticalEvent determines if an event type is critical and should not be dropped silently
func isCriticalEvent(eventType string) bool {
	switch eventType {
	case "WORKFLOW_FAILED",
		"WORKFLOW_COMPLETED",
		"AGENT_FAILED",
		"ERROR_OCCURRED",
		"TOOL_ERROR":
		return true
	default:
		return false
	}
}

// Unsubscribe removes the subscriber channel and cancels its reader goroutine.
// The channel will be closed by the reader goroutine after cancellation.
func (m *Manager) Unsubscribe(workflowID string, ch chan Event) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if subs, ok := m.subscribers[workflowID]; ok {
		if sub, exists := subs[ch]; exists {
			// Cancel the context to stop the reader goroutine
			sub.cancel()
			delete(subs, ch)

			if len(subs) == 0 {
				delete(m.subscribers, workflowID)
			}
		}
	}
}

// Publish sends an event to Redis stream and all local subscribers (for backward compatibility)
func (m *Manager) Publish(workflowID string, evt Event) {
	// Ensure role is always present for observability (best-effort).
	if evt.Payload == nil {
		evt.Payload = map[string]interface{}{}
	}
	if v, ok := evt.Payload["role"].(string); !ok || strings.TrimSpace(v) == "" {
		role := ""
		if strings.TrimSpace(evt.AgentID) != "" {
			role = strings.ReplaceAll(strings.TrimSpace(evt.AgentID), "-", "_")
		}
		if role == "" {
			role = "generalist"
		}
		// Avoid mutating caller-provided payload maps.
		merged := make(map[string]interface{}, len(evt.Payload)+1)
		for k, val := range evt.Payload {
			merged[k] = val
		}
		merged["role"] = role
		evt.Payload = merged
	}

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
		var payloadJSON string
		if evt.Payload != nil {
			if b, err := json.Marshal(evt.Payload); err == nil {
				payloadJSON = string(b)
			}
		}
		streamID, err := m.redis.XAdd(ctx, &redis.XAddArgs{
			Stream: streamKey,
			MaxLen: int64(m.capacity),
			Approx: true,
			Values: map[string]interface{}{
				"workflow_id": evt.WorkflowID,
				"type":        evt.Type,
				"agent_id":    evt.AgentID,
				"message":     evt.Message,
				"payload":     payloadJSON,
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
		// Use longer TTL for sequence counter to prevent resets
		m.redis.Expire(ctx, streamKey, 24*time.Hour)
		m.redis.Expire(ctx, m.seqKey(workflowID), 48*time.Hour)
	}

	// Persist to DB if configured (best-effort, non-blocking)
	// Only persist important events, not streaming deltas
	if m.dbClient != nil && m.persistCh != nil && shouldPersistEvent(evt.Type) {
		// Non-blocking enqueue; drop if full (we never block streaming)
		// Sanitize message and payload to remove large base64 images (browser screenshots)
		el := db.EventLog{
			WorkflowID: evt.WorkflowID,
			Type:       evt.Type,
			AgentID:    evt.AgentID,
			Message:    sanitizeEventMessage(evt.Message),
			Timestamp:  evt.Timestamp,
			Seq:        evt.Seq,
			StreamID:   evt.StreamID,
		}
		if evt.Payload != nil {
			el.Payload = db.JSONB(sanitizeEventPayload(evt.Payload))
		}
		select {
		case m.persistCh <- el:
		default:
			// Escalate log severity for critical events
			if isCriticalEvent(evt.Type) {
				m.logger.Error("CRITICAL: eventlog batcher full; dropping important event",
					zap.String("workflow_id", evt.WorkflowID),
					zap.String("type", evt.Type))
			} else {
				m.logger.Warn("eventlog batcher full; dropping event",
					zap.String("workflow_id", evt.WorkflowID),
					zap.String("type", evt.Type))
			}
		}
	}

	// Only publish to local subscribers if Redis is nil (in-memory mode)
	// When Redis is available, the streamReader will deliver events
	if m.redis == nil {
		m.mu.RLock()
		defer m.mu.RUnlock()
		subs := m.subscribers[workflowID]
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

// shouldPersistEvent determines if an event type should be persisted to PostgreSQL.
// We only persist important events, not streaming deltas, to reduce DB write load.
func shouldPersistEvent(eventType string) bool {
	switch eventType {
	// ✅ Persist: Important workflow events
	case "WORKFLOW_COMPLETED",
		"WORKFLOW_FAILED",
		"AGENT_COMPLETED",
		"AGENT_FAILED",
		"TOOL_INVOKED",
		"TOOL_OBSERVATION",
		"TOOL_ERROR",
		"ERROR_OCCURRED",
		"LLM_OUTPUT",
		"STREAM_END",
		// Phase 2A: Multi-agent coordination events
		"ROLE_ASSIGNED",
		"DELEGATION",
		"BUDGET_THRESHOLD":
		return true

	// ❌ Don't persist: Streaming deltas and heartbeats
	case "LLM_PARTIAL", // thread.message.delta events
		"HEARTBEAT",
		"PING",
		"LLM_PROMPT": // Prompts are logged separately
		return false

	// ✅ Persist AGENT_THINKING so timeline is consistent between live and history
	case "AGENT_THINKING":
		return true

	// Default: persist unknown event types (safe default)
	default:
		return true
	}
}

// sanitizeUTF8 ensures invalid UTF-8 bytes are removed before persistence.
func sanitizeUTF8(s string) string {
	if s == "" || utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			// Skip invalid byte; Postgres rejects malformed UTF-8.
			s = s[size:]
			continue
		}
		b.WriteRune(r)
		s = s[size:]
	}
	return b.String()
}

// SanitizeBase64Image truncates large base64-encoded images in strings to prevent
// Redis/Postgres bloat from browser automation screenshots.
// Returns the sanitized string with base64 data replaced by a placeholder.
func SanitizeBase64Image(s string) string {
	if s == "" {
		return s
	}

	// Common base64 image patterns from browser tools
	patterns := []string{
		`"screenshot": "data:image/`,
		`"screenshot":"data:image/`,
		`"image": "data:image/`,
		`"image":"data:image/`,
		`"base64": "`,
		`"base64":"`,
	}

	result := s
	for _, pattern := range patterns {
		for {
			idx := strings.Index(result, pattern)
			if idx == -1 {
				break
			}

			// Find the start of the base64 data (after the pattern)
			startIdx := idx + len(pattern)
			if startIdx >= len(result) {
				break
			}

			// Find the closing quote
			endIdx := strings.Index(result[startIdx:], `"`)
			if endIdx == -1 {
				break
			}

			dataLen := endIdx
			// Only truncate if the data is larger than a reasonable threshold (1KB)
			if dataLen > 1024 {
				// Replace the base64 data with a placeholder
				placeholder := "[BASE64_IMAGE_TRUNCATED]"
				result = result[:startIdx] + placeholder + result[startIdx+endIdx:]
			} else {
				// Skip past this occurrence to prevent infinite loop
				break
			}
		}
	}

	return result
}

// sanitizeEventMessage sanitizes event message content for storage.
// Removes invalid UTF-8 and truncates large base64 images.
func sanitizeEventMessage(s string) string {
	s = sanitizeUTF8(s)
	s = SanitizeBase64Image(s)
	return s
}

// sanitizeEventPayload sanitizes payload map for storage.
// Truncates large base64 images in string values.
func sanitizeEventPayload(payload map[string]interface{}) map[string]interface{} {
	if payload == nil {
		return nil
	}

	const maxDepth = 4
	var sanitizeValue func(key string, v interface{}, depth int) interface{}
	sanitizeValue = func(key string, v interface{}, depth int) interface{} {
		if v == nil || depth > maxDepth {
			return v
		}

		switch val := v.(type) {
		case string:
			// Handle raw base64 values directly (common for browser_screenshot payloads).
			if (key == "screenshot" || key == "popup_screenshot") && len(val) > 1024 {
				return "[BASE64_IMAGE_TRUNCATED]"
			}
			return SanitizeBase64Image(val)
		case map[string]interface{}:
			return sanitizeEventPayload(val)
		case []interface{}:
			out := make([]interface{}, 0, len(val))
			for _, item := range val {
				out = append(out, sanitizeValue("", item, depth+1))
			}
			return out
		default:
			return v
		}
	}

	sanitized := make(map[string]interface{}, len(payload))
	for k, v := range payload {
		sanitized[k] = sanitizeValue(k, v, 0)
	}
	return sanitized
}

// persistWorker batches event logs and writes them asynchronously.
func (m *Manager) persistWorker() {
	defer m.persistWg.Done()
	batch := make([]db.EventLog, 0, m.batchSize)
	ticker := time.NewTicker(m.flushEvery)
	defer ticker.Stop()
	flush := func() {
		if len(batch) == 0 || m.dbClient == nil {
			return
		}
		// Write sequentially (simple, safe). Could be optimized to a batch insert if needed.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		for i := range batch {
			if err := m.dbClient.SaveEventLog(ctx, &batch[i]); err != nil {
				m.logger.Warn("SaveEventLog failed", zap.String("workflow_id", batch[i].WorkflowID), zap.String("type", batch[i].Type), zap.Uint64("seq", batch[i].Seq), zap.Error(err))
			}
		}
		cancel()
		batch = batch[:0]
	}
	for {
		select {
		case ev, ok := <-m.persistCh:
			if !ok {
				flush()
				return
			}
			batch = append(batch, ev)
			if len(batch) >= m.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
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

// HasEmittedCompletion checks if WORKFLOW_COMPLETED has been emitted for a workflow.
// This is a hint for visibility races (stream may show completion slightly before Temporal does).
func (m *Manager) HasEmittedCompletion(ctx context.Context, workflowID string) bool {
	if m.redis == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}

	checkCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	streamKey := m.streamKey(workflowID)

	// Only scan the tail of the stream to keep this cheap.
	const scanCount int64 = 10
	messages, err := m.redis.XRevRangeN(checkCtx, streamKey, "+", "-", scanCount).Result()
	if err != nil {
		if err == redis.Nil || checkCtx.Err() != nil {
			return false
		}
		m.logger.Debug("Failed to check completion status in Redis",
			zap.String("workflow_id", workflowID),
			zap.Error(err))
		return false
	}

	for _, msg := range messages {
		if eventType, ok := msg.Values["type"].(string); ok && eventType == "WORKFLOW_COMPLETED" {
			return true
		}
	}

	return false
}

// Shutdown gracefully shuts down the manager, stopping all stream readers and flushing persistence.
// It waits for all goroutines to complete with the provided context timeout.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.logger.Info("Shutting down streaming manager")

	// Signal shutdown to all stream readers
	close(m.shutdownCh)

	// Cancel all subscriptions
	m.mu.Lock()
	for workflowID, subs := range m.subscribers {
		for ch, sub := range subs {
			sub.cancel()
			delete(subs, ch)
		}
		delete(m.subscribers, workflowID)
	}
	m.mu.Unlock()

	// Wait for all stream readers to exit
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.logger.Info("All stream readers stopped")
	case <-ctx.Done():
		m.logger.Warn("Shutdown timeout waiting for stream readers")
		return ctx.Err()
	}

	// Close persistence channel and wait for flush
	if m.persistCh != nil {
		close(m.persistCh)

		// Wait for persist worker to exit
		persistDone := make(chan struct{})
		go func() {
			m.persistWg.Wait()
			close(persistDone)
		}()

		select {
		case <-persistDone:
			m.logger.Info("Event persistence flushed")
		case <-ctx.Done():
			m.logger.Warn("Shutdown timeout waiting for persistence flush")
			return ctx.Err()
		}
	}

	m.logger.Info("Streaming manager shutdown complete")
	return nil
}
