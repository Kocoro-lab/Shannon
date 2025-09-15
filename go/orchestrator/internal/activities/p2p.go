package activities

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/go-redis/redis/v8"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
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
}

type SendAgentMessageResult struct {
    Seq uint64 `json:"seq"`
}

// SendAgentMessage stores a message under deterministic Redis keys and publishes a streaming event
func (a *Activities) SendAgentMessage(ctx context.Context, in SendAgentMessageInput) (SendAgentMessageResult, error) {
    if in.WorkflowID == "" || in.To == "" || in.From == "" {
        return SendAgentMessageResult{}, fmt.Errorf("invalid message args")
    }
    // Policy gate via existing team action authorizer
    _, _ = AuthorizeTeamAction(ctx, TeamActionInput{Action: "message_send", SessionID: "", UserID: "", AgentID: in.From, Role: "", Metadata: map[string]interface{}{
        "to": in.To, "type": string(in.Type), "size": lenMust(in.Payload),
    }})

    rc := a.sessionManager.RedisWrapper().GetClient()
    seqKey := fmt.Sprintf("wf:%s:mbox:%s:seq", in.WorkflowID, in.To)
    listKey := fmt.Sprintf("wf:%s:mbox:%s:msgs", in.WorkflowID, in.To)
    seq := rc.Incr(ctx, seqKey).Val()
    msg := map[string]interface{}{
        "seq": seq,
        "from": in.From,
        "to": in.To,
        "type": string(in.Type),
        "payload": in.Payload,
        "ts": time.Now().UnixNano(), // Note: In real workflow this should come from workflow.Now()
    }
    b, _ := json.Marshal(msg)
    if err := rc.RPush(ctx, listKey, b).Err(); err != nil {
        return SendAgentMessageResult{}, err
    }
    // Set TTL for resource cleanup (48 hours)
    rc.Expire(ctx, seqKey, 48*time.Hour)
    rc.Expire(ctx, listKey, 48*time.Hour)

    // Publish streaming events
    now := time.Now() // Use same timestamp for both events
    evt := streaming.Event{WorkflowID: in.WorkflowID, Type: string(StreamEventMessageSent), AgentID: in.From, Message: fmt.Sprintf("to=%s type=%s", in.To, in.Type), Timestamp: now, Seq: 0}
    streaming.Get().Publish(in.WorkflowID, evt)
    // Receiver event (for dashboards)
    evtR := streaming.Event{WorkflowID: in.WorkflowID, Type: string(StreamEventMessageReceived), AgentID: in.To, Message: fmt.Sprintf("from=%s type=%s", in.From, in.Type), Timestamp: now, Seq: 0}
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
    if in.WorkflowID == "" || in.AgentID == "" { return nil, fmt.Errorf("invalid args") }
    rc := a.sessionManager.RedisWrapper().GetClient()
    listKey := fmt.Sprintf("wf:%s:mbox:%s:msgs", in.WorkflowID, in.AgentID)
    // Fetch recent N items; simple window to avoid huge scans
    if in.Limit <= 0 { in.Limit = 200 }
    // Get list length and compute range
    llen := rc.LLen(ctx, listKey).Val()
    start := llen - in.Limit; if start < 0 { start = 0 }
    vals, err := rc.LRange(ctx, listKey, start, llen).Result()
    if err != nil && err != redis.Nil { return nil, err }
    out := make([]AgentMessage, 0, len(vals))
    for _, v := range vals {
        var m AgentMessage
        if json.Unmarshal([]byte(v), &m) == nil {
            if m.Seq > in.SinceSeq { out = append(out, m) }
        }
    }
    return out, nil
}

type WorkspaceAppendInput struct {
    WorkflowID string                 `json:"workflow_id"`
    Topic      string                 `json:"topic"`
    Entry      map[string]interface{} `json:"entry"`
}

type WorkspaceAppendResult struct { Seq uint64 `json:"seq"` }

// WorkspaceAppend appends an entry to a topic list with global workspace seq
func (a *Activities) WorkspaceAppend(ctx context.Context, in WorkspaceAppendInput) (WorkspaceAppendResult, error) {
    if in.WorkflowID == "" || in.Topic == "" { return WorkspaceAppendResult{}, fmt.Errorf("invalid args") }
    // Policy gate
    _, _ = AuthorizeTeamAction(ctx, TeamActionInput{Action: "workspace_append", Metadata: map[string]interface{}{ "topic": in.Topic, "size": lenMust(in.Entry) }})
    rc := a.sessionManager.RedisWrapper().GetClient()
    seqKey := fmt.Sprintf("wf:%s:ws:seq", in.WorkflowID)
    seq := rc.Incr(ctx, seqKey).Val()
    listKey := fmt.Sprintf("wf:%s:ws:%s", in.WorkflowID, in.Topic)
    now := time.Now()
    entry := map[string]interface{}{"seq": seq, "topic": in.Topic, "entry": in.Entry, "ts": now.UnixNano()}
    b, _ := json.Marshal(entry)
    if err := rc.RPush(ctx, listKey, b).Err(); err != nil { return WorkspaceAppendResult{}, err }
    // Set TTL for resource cleanup (48 hours)
    rc.Expire(ctx, seqKey, 48*time.Hour)
    rc.Expire(ctx, listKey, 48*time.Hour)
    // stream event
    streaming.Get().Publish(in.WorkflowID, streaming.Event{WorkflowID: in.WorkflowID, Type: string(StreamEventWorkspaceUpdated), AgentID: "", Message: in.Topic, Timestamp: now})
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
    if in.WorkflowID == "" || in.Topic == "" { return nil, fmt.Errorf("invalid args") }
    rc := a.sessionManager.RedisWrapper().GetClient()
    listKey := fmt.Sprintf("wf:%s:ws:%s", in.WorkflowID, in.Topic)
    if in.Limit <= 0 { in.Limit = 200 }
    llen := rc.LLen(ctx, listKey).Val()
    start := llen - in.Limit; if start < 0 { start = 0 }
    vals, err := rc.LRange(ctx, listKey, start, llen).Result()
    if err != nil && err != redis.Nil { return nil, err }
    out := make([]WorkspaceEntry, 0, len(vals))
    for _, v := range vals {
        var e WorkspaceEntry
        if json.Unmarshal([]byte(v), &e) == nil {
            if e.Seq > in.SinceSeq { out = append(out, e) }
        }
    }
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
func (a *Activities) SendTaskRequest(ctx context.Context, wf, from, to string, req TaskRequest) (SendAgentMessageResult, error) {
    payload := map[string]interface{}{
        "task_id": req.TaskID, "description": req.Description, "required_by": req.RequiredBy, "skills": req.Skills, "topic": req.Topic,
    }
    return a.SendAgentMessage(ctx, SendAgentMessageInput{WorkflowID: wf, From: from, To: to, Type: MessageTypeRequest, Payload: payload})
}

func (a *Activities) SendTaskOffer(ctx context.Context, wf, from, to string, off TaskOffer) (SendAgentMessageResult, error) {
    payload := map[string]interface{}{
        "request_id": off.RequestID, "agent_id": off.AgentID, "confidence": off.Confidence, "estimate_hours": off.EstimateHours,
    }
    return a.SendAgentMessage(ctx, SendAgentMessageInput{WorkflowID: wf, From: from, To: to, Type: MessageTypeOffer, Payload: payload})
}

func (a *Activities) SendTaskAccept(ctx context.Context, wf, from, to string, ac TaskAccept) (SendAgentMessageResult, error) {
    payload := map[string]interface{}{"request_id": ac.RequestID, "agent_id": ac.AgentID}
    return a.SendAgentMessage(ctx, SendAgentMessageInput{WorkflowID: wf, From: from, To: to, Type: MessageTypeAccept, Payload: payload})
}
