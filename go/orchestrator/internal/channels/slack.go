package channels

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/daemon"
)

const (
	slackTimestampHeader = "X-Slack-Request-Timestamp"
	slackSignatureHeader = "X-Slack-Signature"
	slackMaxClockSkew    = 5 * time.Minute
)

type slackCredentials struct {
	SigningSecret string `json:"signing_secret"`
	BotToken     string `json:"bot_token"`
	AppID        string `json:"app_id"`
}

// slackEvent represents the outer Slack Events API payload.
type slackEvent struct {
	Type      string          `json:"type"`
	Token     string          `json:"token"`
	Challenge string          `json:"challenge"`
	Event     json.RawMessage `json:"event"`
}

// slackInnerEvent represents the inner event object.
type slackInnerEvent struct {
	Type     string `json:"type"`
	User     string `json:"user"`
	BotID    string `json:"bot_id"`
	Text     string `json:"text"`
	Channel  string `json:"channel"`
	ThreadTS string `json:"thread_ts"`
	TS       string `json:"ts"`
}

// handleSlackWebhook verifies the Slack request signature, handles URL
// verification challenges, extracts message events, and returns a
// MessagePayload for dispatch. Returns nil payload (no error) when the
// handler has already written the response (e.g. challenge echo).
func handleSlackWebhook(w http.ResponseWriter, r *http.Request, ch *Channel) (*daemon.MessagePayload, error) {
	// Read body with size limit (needed for both signature verification and parsing).
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB max
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	defer r.Body.Close()

	// Parse credentials from channel record.
	var creds slackCredentials
	if err := json.Unmarshal(ch.Credentials, &creds); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}

	// Verify HMAC-SHA256 signature.
	if err := verifySlackSignature(r.Header, body, creds.SigningSecret); err != nil {
		http.Error(w, `{"error":"invalid signature"}`, http.StatusUnauthorized)
		// Return nil, nil — we already wrote the error response.
		return nil, nil
	}

	// Parse the outer event envelope.
	var evt slackEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		return nil, fmt.Errorf("parse event: %w", err)
	}

	// Handle URL verification challenge.
	if evt.Type == "url_verification" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"challenge": evt.Challenge})
		return nil, nil
	}

	// Only process event_callback types.
	if evt.Type != "event_callback" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ignored"}`))
		return nil, nil
	}

	// Parse inner event.
	var inner slackInnerEvent
	if err := json.Unmarshal(evt.Event, &inner); err != nil {
		return nil, fmt.Errorf("parse inner event: %w", err)
	}

	// Ignore bot messages to prevent loops.
	if inner.BotID != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ignored_bot"}`))
		return nil, nil
	}

	// Only process message and app_mention events.
	if inner.Type != "message" && inner.Type != "app_mention" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ignored"}`))
		return nil, nil
	}

	// Build thread ID: "slackChannel-threadTS" (or message TS if no thread).
	threadTS := inner.ThreadTS
	if threadTS == "" {
		threadTS = inner.TS
	}
	threadID := inner.Channel + "-" + threadTS

	return &daemon.MessagePayload{
		Channel:   "slack",
		ThreadID:  threadID,
		Sender:    inner.User,
		Text:      inner.Text,
		Timestamp: inner.TS,
	}, nil
}

// verifySlackSignature checks the HMAC-SHA256 signature per Slack's
// verification protocol: https://api.slack.com/authentication/verifying-requests-from-slack
func verifySlackSignature(headers http.Header, body []byte, signingSecret string) error {
	tsStr := headers.Get(slackTimestampHeader)
	if tsStr == "" {
		return fmt.Errorf("missing %s header", slackTimestampHeader)
	}
	sig := headers.Get(slackSignatureHeader)
	if sig == "" {
		return fmt.Errorf("missing %s header", slackSignatureHeader)
	}

	// Verify timestamp is within tolerance to prevent replay attacks.
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}
	diff := time.Duration(math.Abs(float64(time.Now().Unix()-ts))) * time.Second
	if diff > slackMaxClockSkew {
		return fmt.Errorf("timestamp too old: %v", diff)
	}

	// Compute expected signature: v0=HMAC-SHA256("v0:timestamp:body", signingSecret)
	baseString := fmt.Sprintf("v0:%s:%s", tsStr, string(body))
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(baseString))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}
