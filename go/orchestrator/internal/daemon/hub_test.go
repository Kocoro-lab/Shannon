package daemon

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func setupHub(t *testing.T) (*Hub, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	logger := zap.NewNop()
	return NewHub(rc, logger), mr
}

type capturedSender struct {
	mu   sync.Mutex
	msgs []ServerMessage
}

func (cs *capturedSender) sendFn(msg ServerMessage) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.msgs = append(cs.msgs, msg)
	return nil
}

func (cs *capturedSender) count() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return len(cs.msgs)
}

func (cs *capturedSender) last() ServerMessage {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.msgs[len(cs.msgs)-1]
}

func makeConn(id, tenantID, userID string, sender *capturedSender) *DaemonConn {
	return &DaemonConn{
		ID:          id,
		TenantID:    tenantID,
		UserID:      userID,
		ConnectedAt: time.Now(),
		LastActive:  time.Now(),
		sendFn:      sender.sendFn,
	}
}

func TestRegisterAndGetConnections(t *testing.T) {
	hub, _ := setupHub(t)

	s1 := &capturedSender{}
	s2 := &capturedSender{}
	c1 := makeConn("conn-1", "tenant-a", "user-1", s1)
	c2 := makeConn("conn-2", "tenant-a", "user-1", s2)

	hub.Register(c1)
	hub.Register(c2)

	conns := hub.GetConnections("tenant-a", "user-1")
	if len(conns) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(conns))
	}

	if hub.ConnectedCount("tenant-a", "user-1") != 2 {
		t.Fatalf("expected ConnectedCount=2, got %d", hub.ConnectedCount("tenant-a", "user-1"))
	}

	// Different tenant:user has zero.
	if hub.ConnectedCount("tenant-b", "user-2") != 0 {
		t.Fatalf("expected 0 for unknown tenant:user")
	}
}

func TestUnregisterDropsCount(t *testing.T) {
	hub, _ := setupHub(t)

	s1 := &capturedSender{}
	s2 := &capturedSender{}
	c1 := makeConn("conn-1", "tenant-a", "user-1", s1)
	c2 := makeConn("conn-2", "tenant-a", "user-1", s2)

	hub.Register(c1)
	hub.Register(c2)

	hub.Unregister("conn-1")

	conns := hub.GetConnections("tenant-a", "user-1")
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection after unregister, got %d", len(conns))
	}
	if conns[0].ID != "conn-2" {
		t.Fatalf("expected remaining conn to be conn-2, got %s", conns[0].ID)
	}
	if hub.ConnectedCount("tenant-a", "user-1") != 1 {
		t.Fatalf("expected ConnectedCount=1")
	}

	// Unregister last one.
	hub.Unregister("conn-2")
	if hub.ConnectedCount("tenant-a", "user-1") != 0 {
		t.Fatalf("expected ConnectedCount=0 after all unregistered")
	}

	// Unregister unknown ID is a no-op.
	hub.Unregister("nonexistent")
}

func TestStickyRouting(t *testing.T) {
	hub, _ := setupHub(t)

	hub.SetSticky(ChannelSlack, "thread-1", "conn-a")

	connID, ok := hub.GetSticky(ChannelSlack, "thread-1")
	if !ok || connID != "conn-a" {
		t.Fatalf("expected sticky conn-a, got %s (ok=%v)", connID, ok)
	}

	// Unknown thread returns false.
	_, ok = hub.GetSticky(ChannelSlack, "thread-unknown")
	if ok {
		t.Fatal("expected no sticky for unknown thread")
	}

	// ClearStickyForConn removes entries.
	hub.SetSticky(ChannelLINE, "thread-2", "conn-a")
	hub.SetSticky(ChannelSlack, "thread-3", "conn-b")

	hub.ClearStickyForConn("conn-a")

	_, ok = hub.GetSticky(ChannelSlack, "thread-1")
	if ok {
		t.Fatal("expected sticky for thread-1 to be cleared")
	}
	_, ok = hub.GetSticky(ChannelLINE, "thread-2")
	if ok {
		t.Fatal("expected sticky for thread-2 to be cleared")
	}

	// conn-b entries remain.
	connID, ok = hub.GetSticky(ChannelSlack, "thread-3")
	if !ok || connID != "conn-b" {
		t.Fatal("expected conn-b sticky to remain")
	}
}

func TestUnregisterClearsSticky(t *testing.T) {
	hub, _ := setupHub(t)

	s := &capturedSender{}
	c := makeConn("conn-x", "t", "u", s)
	hub.Register(c)

	hub.SetSticky(ChannelSlack, "thread-99", "conn-x")
	hub.Unregister("conn-x")

	_, ok := hub.GetSticky(ChannelSlack, "thread-99")
	if ok {
		t.Fatal("expected sticky cleared on unregister")
	}
}

func TestDispatchNoConnections(t *testing.T) {
	hub, _ := setupHub(t)

	err := hub.Dispatch(context.Background(), "tenant-a", "user-1", MessagePayload{
		Channel: ChannelSlack,
		Text:    "hello",
	}, ClaimMetadata{ChannelType: ChannelSlack})
	if err != ErrNoDaemonConnected {
		t.Fatalf("expected ErrNoDaemonConnected, got %v", err)
	}
}

func TestDispatchBroadcast(t *testing.T) {
	hub, _ := setupHub(t)

	s1 := &capturedSender{}
	s2 := &capturedSender{}
	c1 := makeConn("conn-1", "tenant-a", "user-1", s1)
	c2 := makeConn("conn-2", "tenant-a", "user-1", s2)

	hub.Register(c1)
	hub.Register(c2)

	err := hub.Dispatch(context.Background(), "tenant-a", "user-1", MessagePayload{
		Channel: ChannelSlack,
		Text:    "hello everyone",
	}, ClaimMetadata{ChannelType: ChannelSlack})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s1.count() != 1 {
		t.Fatalf("expected 1 message to conn-1, got %d", s1.count())
	}
	if s2.count() != 1 {
		t.Fatalf("expected 1 message to conn-2, got %d", s2.count())
	}

	// Verify message structure.
	msg := s1.last()
	if msg.Type != MsgTypeMessage {
		t.Fatalf("expected type %s, got %s", MsgTypeMessage, msg.Type)
	}
	if msg.MessageID == "" {
		t.Fatal("expected non-empty message ID")
	}

	var payload MessagePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Text != "hello everyone" {
		t.Fatalf("expected text 'hello everyone', got %q", payload.Text)
	}
}

func TestDispatchStickyRouting(t *testing.T) {
	hub, _ := setupHub(t)

	s1 := &capturedSender{}
	s2 := &capturedSender{}
	c1 := makeConn("conn-1", "tenant-a", "user-1", s1)
	c2 := makeConn("conn-2", "tenant-a", "user-1", s2)

	hub.Register(c1)
	hub.Register(c2)

	// Set sticky so thread-5 goes to conn-1 only.
	hub.SetSticky(ChannelSlack, "thread-5", "conn-1")

	err := hub.Dispatch(context.Background(), "tenant-a", "user-1", MessagePayload{
		Channel:  ChannelSlack,
		ThreadID: "thread-5",
		Text:     "sticky message",
	}, ClaimMetadata{ChannelType: ChannelSlack, ThreadID: "thread-5"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s1.count() != 1 {
		t.Fatalf("expected 1 message to sticky conn-1, got %d", s1.count())
	}
	if s2.count() != 0 {
		t.Fatalf("expected 0 messages to conn-2 (not sticky), got %d", s2.count())
	}
}

func TestDispatchSystemChannel(t *testing.T) {
	hub, _ := setupHub(t)

	s1 := &capturedSender{}
	s2 := &capturedSender{}
	c1 := makeConn("conn-1", "tenant-a", "user-1", s1)
	c2 := makeConn("conn-2", "tenant-a", "user-1", s2)

	hub.Register(c1)
	hub.Register(c2)

	err := hub.Dispatch(context.Background(), "tenant-a", "user-1", MessagePayload{
		Channel: ChannelSystem,
		Text:    "system broadcast",
	}, ClaimMetadata{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both receive system messages.
	if s1.count() != 1 {
		t.Fatalf("expected 1 system message to conn-1, got %d", s1.count())
	}
	if s2.count() != 1 {
		t.Fatalf("expected 1 system message to conn-2, got %d", s2.count())
	}

	// System messages use MsgTypeSystem.
	msg := s1.last()
	if msg.Type != MsgTypeSystem {
		t.Fatalf("expected type %s, got %s", MsgTypeSystem, msg.Type)
	}
}

func TestDispatchSystemChannelIgnoresSticky(t *testing.T) {
	hub, _ := setupHub(t)

	s1 := &capturedSender{}
	s2 := &capturedSender{}
	c1 := makeConn("conn-1", "tenant-a", "user-1", s1)
	c2 := makeConn("conn-2", "tenant-a", "user-1", s2)

	hub.Register(c1)
	hub.Register(c2)

	// Set sticky for system channel (should be ignored).
	hub.SetSticky(ChannelSystem, "thread-sys", "conn-1")

	err := hub.Dispatch(context.Background(), "tenant-a", "user-1", MessagePayload{
		Channel:  ChannelSystem,
		ThreadID: "thread-sys",
		Text:     "system msg",
	}, ClaimMetadata{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both still receive it (sticky ignored for system).
	if s1.count() != 1 || s2.count() != 1 {
		t.Fatalf("expected both to receive system message, got %d and %d", s1.count(), s2.count())
	}
}

func TestHandleClaimAndReply(t *testing.T) {
	hub, _ := setupHub(t)
	ctx := context.Background()

	s := &capturedSender{}
	c := makeConn("conn-1", "t", "u", s)
	hub.Register(c)

	// First claim succeeds.
	granted, err := hub.HandleClaim(ctx, "conn-1", "msg-100", ClaimMetadata{
		ChannelType: ChannelSlack,
		ThreadID:    "thread-10",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !granted {
		t.Fatal("expected claim to be granted")
	}

	// Sticky should be set.
	connID, ok := hub.GetSticky(ChannelSlack, "thread-10")
	if !ok || connID != "conn-1" {
		t.Fatal("expected sticky to be set after claim")
	}

	// Second claim for same message fails.
	granted2, err := hub.HandleClaim(ctx, "conn-2", "msg-100", ClaimMetadata{
		ChannelType: ChannelSlack,
		ThreadID:    "thread-10",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if granted2 {
		t.Fatal("expected second claim to be denied")
	}

	// Reply retrieves and releases.
	meta, err := hub.HandleReply(ctx, "msg-100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.ConnID != "conn-1" {
		t.Fatalf("expected claim conn_id=conn-1, got %s", meta.ConnID)
	}

	// Claim is now released; reply again should fail.
	_, err = hub.HandleReply(ctx, "msg-100")
	if err != ErrClaimNotFound {
		t.Fatalf("expected ErrClaimNotFound after release, got %v", err)
	}
}

func TestHandleProgress(t *testing.T) {
	hub, _ := setupHub(t)
	ctx := context.Background()

	_, err := hub.HandleClaim(ctx, "conn-1", "msg-200", ClaimMetadata{
		ChannelType: ChannelSlack,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := hub.HandleProgress(ctx, "msg-200"); err != nil {
		t.Fatalf("unexpected error extending claim: %v", err)
	}
}

func TestHandleDisconnect(t *testing.T) {
	hub, _ := setupHub(t)
	ctx := context.Background()

	s := &capturedSender{}
	c := makeConn("conn-1", "t", "u", s)
	hub.Register(c)

	// Create a claim.
	_, _ = hub.HandleClaim(ctx, "conn-1", "msg-300", ClaimMetadata{
		ChannelType: ChannelSlack,
		ThreadID:    "thread-d",
	})

	released, err := hub.HandleDisconnect(ctx, "conn-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(released) != 1 || released[0] != "msg-300" {
		t.Fatalf("expected released=[msg-300], got %v", released)
	}

	// Connection should be gone.
	if hub.ConnectedCount("t", "u") != 0 {
		t.Fatal("expected 0 connections after disconnect")
	}
}

func TestGetConn(t *testing.T) {
	hub, _ := setupHub(t)

	s := &capturedSender{}
	c := makeConn("conn-1", "t", "u", s)
	hub.Register(c)

	got, ok := hub.GetConn("conn-1")
	if !ok {
		t.Fatal("expected to find conn-1")
	}
	if got.ID != "conn-1" {
		t.Fatalf("expected ID=conn-1, got %s", got.ID)
	}

	_, ok = hub.GetConn("nonexistent")
	if ok {
		t.Fatal("expected not found for nonexistent conn")
	}
}

func TestClaimsAccessor(t *testing.T) {
	hub, _ := setupHub(t)
	if hub.Claims() == nil {
		t.Fatal("expected non-nil ClaimManager")
	}
}
