package streaming

import (
    "encoding/json"
    "sync"
    "time"
)

// Event is a minimal streaming event used by SSE and future gRPC.
type Event struct {
    WorkflowID string    `json:"workflow_id"`
    Type       string    `json:"type"`
    AgentID    string    `json:"agent_id,omitempty"`
    Message    string    `json:"message,omitempty"`
    Timestamp  time.Time `json:"timestamp"`
    Seq        uint64    `json:"seq"`
}

// Manager provides in-memory pub/sub for workflow events.
type Manager struct {
    mu          sync.RWMutex
    subscribers map[string]map[chan Event]struct{}
    // per-workflow ring buffer for replay and Last-Event-ID support
    history map[string]*ring
    capacity int
}

var (
    defaultMgr *Manager
    once       sync.Once
    defaultCapacity = 256
)

// Get returns the global streaming manager, initializing it lazily.
func Get() *Manager {
    once.Do(func() {
        defaultMgr = &Manager{
            subscribers: make(map[string]map[chan Event]struct{}),
            history:     make(map[string]*ring),
            capacity:    defaultCapacity,
        }
    })
    return defaultMgr
}

// Configure sets default capacity for new/empty managers and rings.
// Safe to call anytime; updates existing manager's capacity for future rings.
func Configure(capacity int) {
    if capacity <= 0 { return }
    defaultCapacity = capacity
    if defaultMgr != nil {
        defaultMgr.mu.Lock()
        defaultMgr.capacity = capacity
        defaultMgr.mu.Unlock()
    }
}

// Subscribe adds a subscriber channel for a workflowID; caller must drain and call Unsubscribe.
func (m *Manager) Subscribe(workflowID string, buffer int) chan Event {
    ch := make(chan Event, buffer)
    m.mu.Lock()
    defer m.mu.Unlock()
    subs := m.subscribers[workflowID]
    if subs == nil {
        subs = make(map[chan Event]struct{})
        m.subscribers[workflowID] = subs
    }
    subs[ch] = struct{}{}
    return ch
}

// Unsubscribe removes the subscriber channel and closes it.
func (m *Manager) Unsubscribe(workflowID string, ch chan Event) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if subs, ok := m.subscribers[workflowID]; ok {
        delete(subs, ch)
        close(ch)
        if len(subs) == 0 {
            delete(m.subscribers, workflowID)
        }
    }
}

// Publish sends an event to all subscribers of workflowID (non-blocking).
func (m *Manager) Publish(workflowID string, evt Event) {
    m.mu.Lock()
    // update history with seq assignment
    rg := m.history[workflowID]
    if rg == nil {
        rg = newRing(m.capacity)
        m.history[workflowID] = rg
    }
    evt.Seq = rg.nextSeq
    rg.nextSeq++
    rg.push(evt)
    subs := m.subscribers[workflowID]
    m.mu.Unlock()
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

// Marshal returns JSON for event payloads in SSE or logs.
func (e Event) Marshal() []byte {
    b, _ := json.Marshal(e)
    return b
}

// ReplaySince returns events with Seq > since (best-effort within ring capacity).
func (m *Manager) ReplaySince(workflowID string, since uint64) []Event {
    m.mu.RLock()
    rg := m.history[workflowID]
    m.mu.RUnlock()
    if rg == nil { return nil }
    return rg.since(since)
}

// ring is a fixed-capacity ring buffer of events
type ring struct {
    buf     []Event
    start   int
    count   int
    nextSeq uint64
}

func newRing(capacity int) *ring { return &ring{buf: make([]Event, capacity)} }

func (r *ring) push(e Event) {
    if len(r.buf) == 0 { return }
    if r.count < len(r.buf) {
        r.buf[(r.start+r.count)%len(r.buf)] = e
        r.count++
        return
    }
    // overwrite oldest
    r.buf[r.start] = e
    r.start = (r.start + 1) % len(r.buf)
}

func (r *ring) since(seq uint64) []Event {
    if r.count == 0 { return nil }
    out := make([]Event, 0, r.count)
    for i := 0; i < r.count; i++ {
        idx := (r.start + i) % len(r.buf)
        ev := r.buf[idx]
        if ev.Seq > seq {
            out = append(out, ev)
        }
    }
    return out
}
