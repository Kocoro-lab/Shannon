package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	"github.com/go-redis/redis/v8"
)

// MessageType indicates a simple protocol for P2P
type MessageType string

const (
	MessageTypeRequest    MessageType = "request"
	MessageTypeOffer      MessageType = "offer"
	MessageTypeAccept     MessageType = "accept"
	MessageTypeDelegation MessageType = "delegation"
	MessageTypeInfo       MessageType = "info"
)

type SendAgentMessageInput struct {
	WorkflowID string                 `json:"workflow_id"`
	From       string                 `json:"from"`
	To         string                 `json:"to"`
	Type       MessageType            `json:"type"`
	Payload    map[string]interface{} `json:"payload"`
	Timestamp  time.Time              `json:"timestamp"` // Passed from workflow.Now()
}

type SendAgentMessageResult struct {
	Seq uint64 `json:"seq"`
}

// SendAgentMessage stores a message under deterministic Redis keys and publishes a streaming event
func (a *Activities) SendAgentMessage(ctx context.Context, in SendAgentMessageInput) (SendAgentMessageResult, error) {
	if in.WorkflowID == "" || in.To == "" || in.From == "" {
		return SendAgentMessageResult{}, fmt.Errorf("invalid message args")
	}

	// Validate payload size to prevent memory exhaustion
	const maxPayloadSize = 1 * 1024 * 1024 // 1MB limit
	payloadSize := lenMust(in.Payload)
	if payloadSize > maxPayloadSize {
		return SendAgentMessageResult{}, fmt.Errorf("payload size %d exceeds maximum allowed size of %d bytes", payloadSize, maxPayloadSize)
	}

	// Policy gate via existing team action authorizer
	_, _ = AuthorizeTeamAction(ctx, TeamActionInput{Action: "message_send", SessionID: "", UserID: "", AgentID: in.From, Role: "", Metadata: map[string]interface{}{
		"to": in.To, "type": string(in.Type), "size": payloadSize,
	}})

	rc := a.sessionManager.RedisWrapper().GetClient()
	seqKey := fmt.Sprintf("wf:%s:mbox:%s:seq", in.WorkflowID, in.To)
	listKey := fmt.Sprintf("wf:%s:mbox:%s:msgs", in.WorkflowID, in.To)
	seq := rc.Incr(ctx, seqKey).Val()
	// Use timestamp from workflow for deterministic replay
	ts := in.Timestamp
	if ts.IsZero() {
		ts = time.Now() // Fallback for backward compatibility
	}
	msg := map[string]interface{}{
		"seq":     seq,
		"from":    in.From,
		"to":      in.To,
		"type":    string(in.Type),
		"payload": in.Payload,
		"ts":      ts.UnixNano(),
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return SendAgentMessageResult{}, fmt.Errorf("marshal message: %w", err)
	}
	if err := rc.RPush(ctx, listKey, b).Err(); err != nil {
		return SendAgentMessageResult{}, err
	}
	// Set TTL for resource cleanup (48 hours)
	rc.Expire(ctx, seqKey, 48*time.Hour)
	rc.Expire(ctx, listKey, 48*time.Hour)

	// Publish streaming events using workflow timestamp
	evt := streaming.Event{WorkflowID: in.WorkflowID, Type: string(StreamEventMessageSent), AgentID: in.From, Message: fmt.Sprintf("to=%s type=%s", in.To, in.Type), Timestamp: ts, Seq: 0}
	streaming.Get().Publish(in.WorkflowID, evt)
	// Receiver event (for dashboards)
	evtR := streaming.Event{WorkflowID: in.WorkflowID, Type: string(StreamEventMessageReceived), AgentID: in.To, Message: fmt.Sprintf("from=%s type=%s", in.From, in.Type), Timestamp: ts, Seq: 0}
	streaming.Get().Publish(in.WorkflowID, evtR)

	return SendAgentMessageResult{Seq: uint64(seq)}, nil
}

type FetchAgentMessagesInput struct {
	WorkflowID string `json:"workflow_id"`
	AgentID    string `json:"agent_id"`
	SinceSeq   uint64 `json:"since_seq"`
	Limit      int64  `json:"limit"`
}

type AgentMessage struct {
	Seq     uint64                 `json:"seq"`
	From    string                 `json:"from"`
	To      string                 `json:"to"`
	Type    MessageType            `json:"type"`
	Payload map[string]interface{} `json:"payload"`
	Ts      int64                  `json:"ts"`
}

// FetchAgentMessages returns messages for an agent after SinceSeq (best-effort)
func (a *Activities) FetchAgentMessages(ctx context.Context, in FetchAgentMessagesInput) ([]AgentMessage, error) {
	if in.WorkflowID == "" || in.AgentID == "" {
		return nil, fmt.Errorf("invalid args")
	}
	rc := a.sessionManager.RedisWrapper().GetClient()
	listKey := fmt.Sprintf("wf:%s:mbox:%s:msgs", in.WorkflowID, in.AgentID)
	// Fetch recent N items; simple window to avoid huge scans
	if in.Limit <= 0 {
		in.Limit = 200
	}
	// Get list length and compute range
	llen := rc.LLen(ctx, listKey).Val()
	start := llen - in.Limit
	if start < 0 {
		start = 0
	}
	vals, err := rc.LRange(ctx, listKey, start, llen).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	out := make([]AgentMessage, 0, len(vals))
	for _, v := range vals {
		var m AgentMessage
		if json.Unmarshal([]byte(v), &m) == nil {
			if m.Seq > in.SinceSeq {
				out = append(out, m)
			}
		}
	}
	return out, nil
}

type WorkspaceAppendInput struct {
	WorkflowID string                 `json:"workflow_id"`
	Topic      string                 `json:"topic"`
	Entry      map[string]interface{} `json:"entry"`
	Timestamp  time.Time              `json:"timestamp"` // Passed from workflow.Now()
}

type WorkspaceAppendResult struct {
	Seq uint64 `json:"seq"`
}

// WorkspaceAppend appends an entry to a topic list with global workspace seq
func (a *Activities) WorkspaceAppend(ctx context.Context, in WorkspaceAppendInput) (WorkspaceAppendResult, error) {
	if in.WorkflowID == "" || in.Topic == "" {
		return WorkspaceAppendResult{}, fmt.Errorf("invalid args")
	}

	// Validate entry size to prevent memory exhaustion
	const maxEntrySize = 1 * 1024 * 1024 // 1MB limit
	entrySize := lenMust(in.Entry)
	if entrySize > maxEntrySize {
		return WorkspaceAppendResult{}, fmt.Errorf("entry size %d exceeds maximum allowed size of %d bytes", entrySize, maxEntrySize)
	}

	// Policy gate
	_, _ = AuthorizeTeamAction(ctx, TeamActionInput{Action: "workspace_append", Metadata: map[string]interface{}{"topic": in.Topic, "size": entrySize}})
	rc := a.sessionManager.RedisWrapper().GetClient()
	seqKey := fmt.Sprintf("wf:%s:ws:seq", in.WorkflowID)
	seq := rc.Incr(ctx, seqKey).Val()
	listKey := fmt.Sprintf("wf:%s:ws:%s", in.WorkflowID, in.Topic)
	// Use timestamp from workflow for deterministic replay
	ts := in.Timestamp
	if ts.IsZero() {
		ts = time.Now() // Fallback for backward compatibility
	}
	entry := map[string]interface{}{"seq": seq, "topic": in.Topic, "entry": in.Entry, "ts": ts.UnixNano()}
	b, err := json.Marshal(entry)
	if err != nil {
		return WorkspaceAppendResult{}, fmt.Errorf("marshal entry: %w", err)
	}
	if err := rc.RPush(ctx, listKey, b).Err(); err != nil {
		return WorkspaceAppendResult{}, err
	}
	// Set TTL for resource cleanup (48 hours)
	rc.Expire(ctx, seqKey, 48*time.Hour)
	rc.Expire(ctx, listKey, 48*time.Hour)
	// stream event
	streaming.Get().Publish(in.WorkflowID, streaming.Event{WorkflowID: in.WorkflowID, Type: string(StreamEventWorkspaceUpdated), AgentID: "workspace", Message: in.Topic, Timestamp: ts})
	return WorkspaceAppendResult{Seq: uint64(seq)}, nil
}

type WorkspaceListInput struct {
	WorkflowID string `json:"workflow_id"`
	Topic      string `json:"topic"`
	SinceSeq   uint64 `json:"since_seq"`
	Limit      int64  `json:"limit"`
}

type WorkspaceEntry struct {
	Seq   uint64                 `json:"seq"`
	Topic string                 `json:"topic"`
	Entry map[string]interface{} `json:"entry"`
	Ts    int64                  `json:"ts"`
}

// WorkspaceList returns entries for a topic after SinceSeq
func (a *Activities) WorkspaceList(ctx context.Context, in WorkspaceListInput) ([]WorkspaceEntry, error) {
	if in.WorkflowID == "" || in.Topic == "" {
		return nil, fmt.Errorf("invalid args")
	}
	rc := a.sessionManager.RedisWrapper().GetClient()
	listKey := fmt.Sprintf("wf:%s:ws:%s", in.WorkflowID, in.Topic)
	if in.Limit <= 0 {
		in.Limit = 200
	}
	llen := rc.LLen(ctx, listKey).Val()
	start := llen - in.Limit
	if start < 0 {
		start = 0
	}
	vals, err := rc.LRange(ctx, listKey, start, llen).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	out := make([]WorkspaceEntry, 0, len(vals))
	for _, v := range vals {
		var e WorkspaceEntry
		if json.Unmarshal([]byte(v), &e) == nil {
			if e.Seq > in.SinceSeq {
				out = append(out, e)
			}
		}
	}
	return out, nil
}

// WorkspaceListAllInput scans all workspace topics for a workflow.
type WorkspaceListAllInput struct {
	WorkflowID string `json:"workflow_id"`
	SinceSeq   uint64 `json:"since_seq"`
	MaxEntries int    `json:"max_entries"` // Max entries total across all topics
}

// WorkspaceListAll returns recent entries from ALL workspace topics for a workflow.
func (a *Activities) WorkspaceListAll(ctx context.Context, in WorkspaceListAllInput) ([]WorkspaceEntry, error) {
	if in.WorkflowID == "" {
		return nil, fmt.Errorf("invalid args: empty workflow_id")
	}
	if in.MaxEntries <= 0 {
		in.MaxEntries = 10
	}

	rc := a.sessionManager.RedisWrapper().GetClient()
	prefix := fmt.Sprintf("wf:%s:ws:", in.WorkflowID)
	seqKey := fmt.Sprintf("wf:%s:ws:seq", in.WorkflowID)

	// Scan for workspace topic keys (capped to prevent memory bloat)
	const maxTopics = 20
	var topicKeys []string
	iter := rc.Scan(ctx, 0, prefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		if key != seqKey { // Skip the global seq counter
			topicKeys = append(topicKeys, key)
			if len(topicKeys) >= maxTopics {
				break
			}
		}
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}

	// Read recent entries from each topic
	var out []WorkspaceEntry
	for _, key := range topicKeys {
		llen := rc.LLen(ctx, key).Val()
		start := llen - 5 // Last 5 per topic
		if start < 0 {
			start = 0
		}
		vals, err := rc.LRange(ctx, key, start, llen).Result()
		if err != nil && err != redis.Nil {
			continue
		}
		for _, v := range vals {
			var e WorkspaceEntry
			if json.Unmarshal([]byte(v), &e) == nil {
				if e.Seq > in.SinceSeq {
					out = append(out, e)
				}
			}
		}
	}

	// Sort by seq (entries come from multiple Redis lists, so order isn't guaranteed)
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })

	// Cap at MaxEntries, keeping the most recent (highest seq)
	if len(out) > in.MaxEntries {
		out = out[len(out)-in.MaxEntries:]
	}

	return out, nil
}

// ── File Registry Activities ────────────────────────────────────────────────

// FileRegistryEntry represents a file tracked in the swarm file registry.
type FileRegistryEntry struct {
	Path      string `json:"path"`
	Author    string `json:"author"`
	Size      int    `json:"size"`
	Summary   string `json:"summary"`
	CreatedAt string `json:"created_at"`
}

// RegisterFileInput is the input for RegisterFile activity.
type RegisterFileInput struct {
	WorkflowID string `json:"workflow_id"`
	Path       string `json:"path"`
	Author     string `json:"author"`
	Size       int    `json:"size"`
	Summary    string `json:"summary"`
}

// RegisterFile records a file entry in the Redis-backed file registry.
func (a *Activities) RegisterFile(ctx context.Context, in RegisterFileInput) error {
	if in.WorkflowID == "" || in.Path == "" {
		return fmt.Errorf("invalid args: empty workflow_id or path")
	}
	rc := a.sessionManager.RedisWrapper().GetClient()
	key := fmt.Sprintf("wf:%s:file_registry", in.WorkflowID)
	entry := FileRegistryEntry{
		Path:      in.Path,
		Author:    in.Author,
		Size:      in.Size,
		Summary:   in.Summary,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal file registry entry: %w", err)
	}
	if err := rc.HSet(ctx, key, in.Path, string(data)).Err(); err != nil {
		return err
	}
	rc.Expire(ctx, key, 24*time.Hour)
	return nil
}

// GetFileRegistryInput is the input for GetFileRegistry activity.
type GetFileRegistryInput struct {
	WorkflowID string `json:"workflow_id"`
}

// GetFileRegistry returns all file entries from the Redis-backed file registry, sorted by path.
func (a *Activities) GetFileRegistry(ctx context.Context, in GetFileRegistryInput) ([]FileRegistryEntry, error) {
	if in.WorkflowID == "" {
		return nil, fmt.Errorf("invalid args: empty workflow_id")
	}
	rc := a.sessionManager.RedisWrapper().GetClient()
	key := fmt.Sprintf("wf:%s:file_registry", in.WorkflowID)
	vals, err := rc.HGetAll(ctx, key).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	out := make([]FileRegistryEntry, 0, len(vals))
	for _, v := range vals {
		var e FileRegistryEntry
		if json.Unmarshal([]byte(v), &e) == nil {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// lenMust returns an approximate size of a payload
func lenMust(m map[string]interface{}) int {
	b, _ := json.Marshal(m)
	return len(b)
}

// Structured protocols (v1)
type TaskRequest struct {
	TaskID      string   `json:"task_id"`
	Description string   `json:"description"`
	RequiredBy  int64    `json:"required_by,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	Topic       string   `json:"topic,omitempty"`
}

type TaskOffer struct {
	RequestID     string  `json:"request_id"`
	AgentID       string  `json:"agent_id"`
	Confidence    float64 `json:"confidence,omitempty"`
	EstimateHours int     `json:"estimate_hours,omitempty"`
}

type TaskAccept struct {
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
}

// Convenience wrappers
func (a *Activities) SendTaskRequest(ctx context.Context, wf, from, to string, req TaskRequest, timestamp time.Time) (SendAgentMessageResult, error) {
	payload := map[string]interface{}{
		"task_id": req.TaskID, "description": req.Description, "required_by": req.RequiredBy, "skills": req.Skills, "topic": req.Topic,
	}
	return a.SendAgentMessage(ctx, SendAgentMessageInput{WorkflowID: wf, From: from, To: to, Type: MessageTypeRequest, Payload: payload, Timestamp: timestamp})
}

func (a *Activities) SendTaskOffer(ctx context.Context, wf, from, to string, off TaskOffer, timestamp time.Time) (SendAgentMessageResult, error) {
	payload := map[string]interface{}{
		"request_id": off.RequestID, "agent_id": off.AgentID, "confidence": off.Confidence, "estimate_hours": off.EstimateHours,
	}
	return a.SendAgentMessage(ctx, SendAgentMessageInput{WorkflowID: wf, From: from, To: to, Type: MessageTypeOffer, Payload: payload, Timestamp: timestamp})
}

func (a *Activities) SendTaskAccept(ctx context.Context, wf, from, to string, ac TaskAccept, timestamp time.Time) (SendAgentMessageResult, error) {
	payload := map[string]interface{}{"request_id": ac.RequestID, "agent_id": ac.AgentID}
	return a.SendAgentMessage(ctx, SendAgentMessageInput{WorkflowID: wf, From: from, To: to, Type: MessageTypeAccept, Payload: payload, Timestamp: timestamp})
}

// validateSessionID checks session_id to prevent path traversal attacks.
// Mirrors Python _validate_session_id() in file_ops.py.
func validateSessionID(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("empty session_id")
	}
	if len(sessionID) > 128 {
		return fmt.Errorf("session_id too long (max 128 chars)")
	}
	if strings.Contains(sessionID, "..") || strings.HasPrefix(sessionID, ".") {
		return fmt.Errorf("session_id contains path traversal")
	}
	for _, c := range sessionID {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return fmt.Errorf("session_id contains invalid char: %c", c)
		}
	}
	return nil
}

// SetupWorkspaceDirsInput is the input for the SetupWorkspaceDirs activity.
type SetupWorkspaceDirsInput struct {
	WorkflowID string `json:"workflow_id"`
	SessionID  string `json:"session_id"`
}

// SetupWorkspaceDirs ensures the session workspace root directory exists.
// Subdirectories are created on-demand by file_write (create_dirs=true).
func (a *Activities) SetupWorkspaceDirs(ctx context.Context, in SetupWorkspaceDirsInput) error {
	if err := validateSessionID(in.SessionID); err != nil {
		return fmt.Errorf("invalid session_id: %w", err)
	}
	baseDir := os.Getenv("SHANNON_SESSION_WORKSPACES_DIR")
	if baseDir == "" {
		baseDir = "/tmp/shannon-sessions"
	}
	sessionDir := filepath.Join(baseDir, in.SessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return fmt.Errorf("failed to create workspace dir: %w", err)
	}
	return nil
}

// ── TaskList Activities ─────────────────────────────────────────────────────

// SwarmTask represents a single task in the Redis-backed TaskList.
type SwarmTask struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Status      string   `json:"status"`                // "pending", "in_progress", "completed"
	Owner       string   `json:"owner"`                 // agent ID that owns this task
	CreatedBy   string   `json:"created_by"`            // "decompose" or agent ID
	DependsOn   []string `json:"depends_on"`            // task IDs this depends on
	CreatedAt   string   `json:"created_at"`
	CompletedAt string   `json:"completed_at,omitempty"`
}

// taskListKey returns the Redis hash key for a workflow's task list.
func taskListKey(workflowID string) string {
	return fmt.Sprintf("wf:%s:tasklist", workflowID)
}

// publishTaskListUpdate publishes TASKLIST_UPDATED with full task payload.
// tasks is fetched from Redis by the caller (ctx-aware) to avoid context mismatch.
func publishTaskListUpdate(workflowID, agentID, message string, tasks []SwarmTask) {
	// Convert to generic maps for JSON payload
	taskMaps := make([]interface{}, len(tasks))
	for i, t := range tasks {
		taskMaps[i] = t
	}
	streaming.Get().Publish(workflowID, streaming.Event{
		WorkflowID: workflowID,
		Type:       string(StreamEventTaskListUpdated),
		AgentID:    agentID,
		Message:    message,
		Payload:    map[string]interface{}{"tasks": taskMaps},
		Timestamp:  time.Now(),
	})
}

// fetchAllTasks reads all tasks from the Redis hash for a workflow.
func fetchAllTasks(ctx context.Context, rc *redis.Client, workflowID string) []SwarmTask {
	key := taskListKey(workflowID)
	vals, err := rc.HGetAll(ctx, key).Result()
	if err != nil {
		return nil
	}
	tasks := make([]SwarmTask, 0, len(vals))
	for _, v := range vals {
		var t SwarmTask
		if err := json.Unmarshal([]byte(v), &t); err == nil {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// ValidateStatusTransition checks whether a status transition is allowed.
// Valid transitions: pending->in_progress, in_progress->completed, pending->completed,
// completed->in_progress (reactivate for continuation), in_progress->in_progress (re-assign).
func ValidateStatusTransition(from, to string) error {
	switch {
	case from == "pending" && to == "in_progress":
		return nil
	case from == "in_progress" && to == "completed":
		return nil
	case from == "pending" && to == "completed":
		return nil // Lead can cancel pending tasks directly
	case from == "in_progress" && to == "in_progress":
		return nil // Lead can re-assign an in_progress task
	case from == "completed" && to == "in_progress":
		return nil // Reactivate: agent idle auto-completes task, then Lead assigns continuation
	default:
		return fmt.Errorf("invalid status transition: %s -> %s", from, to)
	}
}

// InitTaskListInput is the input for bulk-initializing a task list.
type InitTaskListInput struct {
	WorkflowID string      `json:"workflow_id"`
	Tasks      []SwarmTask `json:"tasks"`
}

// InitTaskList bulk-writes tasks into a Redis hash keyed by workflow ID.
func (a *Activities) InitTaskList(ctx context.Context, in InitTaskListInput) error {
	if in.WorkflowID == "" {
		return fmt.Errorf("invalid args: empty workflow_id")
	}
	if len(in.Tasks) == 0 {
		return fmt.Errorf("invalid args: empty tasks")
	}

	rc := a.sessionManager.RedisWrapper().GetClient()
	key := taskListKey(in.WorkflowID)

	fields := make(map[string]interface{}, len(in.Tasks))
	for i := range in.Tasks {
		t := &in.Tasks[i]
		if t.ID == "" {
			return fmt.Errorf("task at index %d has empty ID", i)
		}
		if t.Status == "" {
			t.Status = "pending"
		}
		if t.CreatedAt == "" {
			t.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		}
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Errorf("marshal task %s: %w", t.ID, err)
		}
		fields[t.ID] = b
	}

	if err := rc.HSet(ctx, key, fields).Err(); err != nil {
		return fmt.Errorf("HSET tasklist: %w", err)
	}
	rc.Expire(ctx, key, 24*time.Hour)

	// Emit streaming event with full task list (pass tasks directly, no re-read)
	publishTaskListUpdate(in.WorkflowID, "tasklist", fmt.Sprintf("initialized %d tasks", len(in.Tasks)), in.Tasks)

	return nil
}

// GetTaskListInput is the input for retrieving all tasks.
type GetTaskListInput struct {
	WorkflowID string `json:"workflow_id"`
}

// GetTaskList returns all tasks from the Redis hash, sorted by ID.
func (a *Activities) GetTaskList(ctx context.Context, in GetTaskListInput) ([]SwarmTask, error) {
	if in.WorkflowID == "" {
		return nil, fmt.Errorf("invalid args: empty workflow_id")
	}

	rc := a.sessionManager.RedisWrapper().GetClient()
	key := taskListKey(in.WorkflowID)

	vals, err := rc.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("HGETALL tasklist: %w", err)
	}

	tasks := make([]SwarmTask, 0, len(vals))
	for _, v := range vals {
		var t SwarmTask
		if err := json.Unmarshal([]byte(v), &t); err != nil {
			continue // skip malformed entries
		}
		tasks = append(tasks, t)
	}

	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}

// UpdateTaskStatusInput is the input for changing a task's status.
type UpdateTaskStatusInput struct {
	WorkflowID string `json:"workflow_id"`
	TaskID     string `json:"task_id"`
	Status     string `json:"status"`   // target status
	AgentID    string `json:"agent_id"` // who is making the change
}

// UpdateTaskStatus performs a validated status transition on a single task.
func (a *Activities) UpdateTaskStatus(ctx context.Context, in UpdateTaskStatusInput) error {
	if in.WorkflowID == "" || in.TaskID == "" || in.Status == "" {
		return fmt.Errorf("invalid args: workflow_id, task_id, and status are required")
	}

	rc := a.sessionManager.RedisWrapper().GetClient()
	key := taskListKey(in.WorkflowID)

	// Fetch current task
	raw, err := rc.HGet(ctx, key, in.TaskID).Result()
	if err != nil {
		return fmt.Errorf("HGET task %s: %w", in.TaskID, err)
	}

	var task SwarmTask
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		return fmt.Errorf("unmarshal task %s: %w", in.TaskID, err)
	}

	// Validate transition
	if err := ValidateStatusTransition(task.Status, in.Status); err != nil {
		return err
	}

	// Apply changes
	task.Status = in.Status
	if in.Status == "in_progress" {
		task.Owner = in.AgentID
	}
	if in.Status == "completed" {
		task.CompletedAt = time.Now().UTC().Format(time.RFC3339)
		// Preserve owner: if task had no owner (e.g. pending→completed), record who completed it
		if task.Owner == "" && in.AgentID != "" {
			task.Owner = in.AgentID
		}
	}

	b, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task %s: %w", in.TaskID, err)
	}
	if err := rc.HSet(ctx, key, in.TaskID, b).Err(); err != nil {
		return fmt.Errorf("HSET task %s: %w", in.TaskID, err)
	}

	// Emit streaming event with full task list
	allTasks := fetchAllTasks(ctx, rc, in.WorkflowID)
	publishTaskListUpdate(in.WorkflowID, in.AgentID, fmt.Sprintf("task=%s status=%s", in.TaskID, in.Status), allTasks)

	return nil
}

// UpdateTaskDescriptionInput is the input for modifying a task's description.
type UpdateTaskDescriptionInput struct {
	WorkflowID  string `json:"workflow_id"`
	TaskID      string `json:"task_id"`
	Description string `json:"description"`
}

// UpdateTaskDescription modifies an existing task's description in Redis and emits TASKLIST_UPDATED.
func (a *Activities) UpdateTaskDescription(ctx context.Context, in UpdateTaskDescriptionInput) error {
	if in.WorkflowID == "" || in.TaskID == "" || in.Description == "" {
		return fmt.Errorf("invalid args: workflow_id, task_id, and description are required")
	}

	rc := a.sessionManager.RedisWrapper().GetClient()
	key := taskListKey(in.WorkflowID)

	raw, err := rc.HGet(ctx, key, in.TaskID).Result()
	if err != nil {
		return fmt.Errorf("HGET task %s: %w", in.TaskID, err)
	}

	var task SwarmTask
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		return fmt.Errorf("unmarshal task %s: %w", in.TaskID, err)
	}

	task.Description = in.Description

	b, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task %s: %w", in.TaskID, err)
	}
	if err := rc.HSet(ctx, key, in.TaskID, b).Err(); err != nil {
		return fmt.Errorf("HSET task %s: %w", in.TaskID, err)
	}

	allTasks := fetchAllTasks(ctx, rc, in.WorkflowID)
	publishTaskListUpdate(in.WorkflowID, "lead", fmt.Sprintf("Updated task %s", in.TaskID), allTasks)

	return nil
}

// ClaimTaskInput is the input for atomically claiming a pending task.
type ClaimTaskInput struct {
	WorkflowID string `json:"workflow_id"`
	TaskID     string `json:"task_id"`
	AgentID    string `json:"agent_id"`
}

// ClaimTaskResult is the result of a claim attempt.
type ClaimTaskResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// claimTaskScript is a Redis Lua script that atomically claims a pending task (D10).
var claimTaskScript = `
local key = KEYS[1]
local task_id = ARGV[1]
local agent_id = ARGV[2]
local current = redis.call('HGET', key, task_id)
if current then
    local task = cjson.decode(current)
    if task.status == 'pending' and (task.owner == nil or task.owner == '' or task.owner == cjson.null) then
        task.status = 'in_progress'
        task.owner = agent_id
        redis.call('HSET', key, task_id, cjson.encode(task))
        return 1
    end
end
return 0
`

// ClaimTask atomically claims a pending task for an agent using a Redis Lua script (D10).
func (a *Activities) ClaimTask(ctx context.Context, in ClaimTaskInput) (ClaimTaskResult, error) {
	if in.WorkflowID == "" || in.TaskID == "" || in.AgentID == "" {
		return ClaimTaskResult{}, fmt.Errorf("invalid args: workflow_id, task_id, and agent_id are required")
	}

	rc := a.sessionManager.RedisWrapper().GetClient()
	key := fmt.Sprintf("wf:%s:tasklist", in.WorkflowID)

	result, err := rc.Eval(ctx, claimTaskScript, []string{key}, in.TaskID, in.AgentID).Int()
	if err != nil {
		return ClaimTaskResult{}, fmt.Errorf("claim task Lua script failed: %w", err)
	}

	if result == 1 {
		// Emit streaming event with full task list
		allTasks := fetchAllTasks(ctx, rc, in.WorkflowID)
		publishTaskListUpdate(in.WorkflowID, in.AgentID, fmt.Sprintf("claimed task=%s", in.TaskID), allTasks)
		return ClaimTaskResult{Success: true, Message: fmt.Sprintf("Task %s claimed by %s", in.TaskID, in.AgentID)}, nil
	}
	return ClaimTaskResult{Success: false, Message: fmt.Sprintf("Task %s not available for claiming", in.TaskID)}, nil
}

// CreateTaskInput is the input for adding a new task to the list.
type CreateTaskInput struct {
	WorkflowID string    `json:"workflow_id"`
	Task       SwarmTask `json:"task"`
}

// CreateTask adds a new task to the Redis hash.
func (a *Activities) CreateTask(ctx context.Context, in CreateTaskInput) error {
	if in.WorkflowID == "" || in.Task.ID == "" {
		return fmt.Errorf("invalid args: workflow_id and task.id are required")
	}

	rc := a.sessionManager.RedisWrapper().GetClient()
	key := taskListKey(in.WorkflowID)

	// Check for duplicate
	exists, err := rc.HExists(ctx, key, in.Task.ID).Result()
	if err != nil {
		return fmt.Errorf("HEXISTS task %s: %w", in.Task.ID, err)
	}
	if exists {
		return fmt.Errorf("task %s already exists", in.Task.ID)
	}

	// Set defaults
	if in.Task.Status == "" {
		in.Task.Status = "pending"
	}
	if in.Task.CreatedAt == "" {
		in.Task.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	b, err := json.Marshal(in.Task)
	if err != nil {
		return fmt.Errorf("marshal task %s: %w", in.Task.ID, err)
	}
	if err := rc.HSet(ctx, key, in.Task.ID, b).Err(); err != nil {
		return fmt.Errorf("HSET task %s: %w", in.Task.ID, err)
	}

	// Emit streaming event with full task list
	allTasks := fetchAllTasks(ctx, rc, in.WorkflowID)
	publishTaskListUpdate(in.WorkflowID, "tasklist", fmt.Sprintf("created task=%s", in.Task.ID), allTasks)

	return nil
}
