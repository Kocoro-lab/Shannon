package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupTestRedis(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rc, func() { rc.Close(); mr.Close() }
}

func TestClaimManager_TryClaim(t *testing.T) {
	rc, cleanup := setupTestRedis(t)
	defer cleanup()
	cm := NewClaimManager(rc)
	ctx := context.Background()

	meta := ClaimMetadata{
		ConnID:      "conn-1",
		ChannelID:   "ch-uuid",
		ChannelType: "slack",
		ThreadID:    "C07-1234",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	ok, err := cm.TryClaim(ctx, "msg-1", meta)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("expected first claim to succeed")
	}

	meta2 := meta
	meta2.ConnID = "conn-2"
	ok2, err := cm.TryClaim(ctx, "msg-1", meta2)
	if err != nil {
		t.Fatal(err)
	}
	if ok2 {
		t.Error("expected second claim to fail")
	}
}

func TestClaimManager_ExtendAndRelease(t *testing.T) {
	rc, cleanup := setupTestRedis(t)
	defer cleanup()
	cm := NewClaimManager(rc)
	ctx := context.Background()

	meta := ClaimMetadata{ConnID: "conn-1", ChannelType: "slack"}
	cm.TryClaim(ctx, "msg-2", meta)

	if err := cm.ExtendClaim(ctx, "msg-2", 60*time.Second); err != nil {
		t.Fatal(err)
	}

	got, err := cm.GetClaim(ctx, "msg-2")
	if err != nil {
		t.Fatal(err)
	}
	if got.ConnID != "conn-1" {
		t.Errorf("got conn %q, want conn-1", got.ConnID)
	}

	if err := cm.ReleaseClaim(ctx, "msg-2"); err != nil {
		t.Fatal(err)
	}

	_, err = cm.GetClaim(ctx, "msg-2")
	if err != ErrClaimNotFound {
		t.Errorf("expected ErrClaimNotFound, got %v", err)
	}
}

func TestClaimManager_ReleaseAllForConn(t *testing.T) {
	rc, cleanup := setupTestRedis(t)
	defer cleanup()
	cm := NewClaimManager(rc)
	ctx := context.Background()

	cm.TryClaim(ctx, "msg-a", ClaimMetadata{ConnID: "conn-1", ChannelType: "slack"})
	cm.TryClaim(ctx, "msg-b", ClaimMetadata{ConnID: "conn-1", ChannelType: "line"})
	cm.TryClaim(ctx, "msg-c", ClaimMetadata{ConnID: "conn-2", ChannelType: "slack"})

	released, err := cm.ReleaseAllForConn(ctx, "conn-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(released) != 2 {
		t.Errorf("expected 2 released, got %d", len(released))
	}

	// conn-2's claim should still exist
	_, err = cm.GetClaim(ctx, "msg-c")
	if err != nil {
		t.Errorf("conn-2 claim should still exist: %v", err)
	}
}
