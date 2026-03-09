package channels

import (
	"context"
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/daemon"
	"go.uber.org/zap"
)

func TestParseSlackThreadID(t *testing.T) {
	tests := []struct {
		name     string
		threadID string
		wantChan string
		wantTS   string
	}{
		{
			name:     "channel and thread_ts",
			threadID: "C07ABCDEF-1234567890.123456",
			wantChan: "C07ABCDEF",
			wantTS:   "1234567890.123456",
		},
		{
			name:     "channel only, no dash",
			threadID: "C07ABCDEF",
			wantChan: "C07ABCDEF",
			wantTS:   "",
		},
		{
			name:     "empty string",
			threadID: "",
			wantChan: "",
			wantTS:   "",
		},
		{
			name:     "multiple dashes uses last",
			threadID: "C07-ABC-1234.5678",
			wantChan: "C07-ABC",
			wantTS:   "1234.5678",
		},
		{
			name:     "dash at start",
			threadID: "-1234567890.123456",
			wantChan: "",
			wantTS:   "1234567890.123456",
		},
		{
			name:     "trailing dash",
			threadID: "C07ABCDEF-",
			wantChan: "C07ABCDEF",
			wantTS:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, ts := parseSlackThreadID(tt.threadID)
			if ch != tt.wantChan {
				t.Errorf("channel: got %q, want %q", ch, tt.wantChan)
			}
			if ts != tt.wantTS {
				t.Errorf("thread_ts: got %q, want %q", ts, tt.wantTS)
			}
		})
	}
}

func TestRouteReply_NoChannelID(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	or := NewOutboundRouter(nil, logger)

	err := or.RouteReply(context.Background(), daemon.ClaimMetadata{}, daemon.ReplyPayload{Text: "hello"})
	if err != nil {
		t.Fatalf("expected nil error for empty channel_id, got: %v", err)
	}
}

func TestRouteReply_InvalidChannelID(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	or := NewOutboundRouter(nil, logger)

	err := or.RouteReply(context.Background(), daemon.ClaimMetadata{
		ChannelID: "not-a-uuid",
	}, daemon.ReplyPayload{Text: "hello"})
	if err == nil {
		t.Fatal("expected error for invalid channel_id")
	}
}
