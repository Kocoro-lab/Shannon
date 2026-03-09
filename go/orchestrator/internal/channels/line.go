package channels

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/daemon"
)

type lineCredentials struct {
	ChannelSecret      string `json:"channel_secret"`
	ChannelAccessToken string `json:"channel_access_token"`
}

type lineWebhookBody struct {
	Events []lineEvent `json:"events"`
}

type lineEvent struct {
	Type       string `json:"type"`
	ReplyToken string `json:"replyToken"`
	Source     struct {
		Type   string `json:"type"`
		UserID string `json:"userId"`
	} `json:"source"`
	Message struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"message"`
	Timestamp int64 `json:"timestamp"`
}

func handleLINEWebhook(w http.ResponseWriter, r *http.Request, ch *Channel) (*daemon.MessagePayload, error) {
	var creds lineCredentials
	if err := json.Unmarshal(ch.Credentials, &creds); err != nil {
		return nil, fmt.Errorf("invalid LINE credentials: %w", err)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB max
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	// Verify LINE signature.
	if err := verifyLINESignature(r, body, creds.ChannelSecret); err != nil {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return nil, nil
	}

	var webhook lineWebhookBody
	if err := json.Unmarshal(body, &webhook); err != nil {
		return nil, fmt.Errorf("parse webhook: %w", err)
	}

	// Process only the first text message event.
	for _, event := range webhook.Events {
		if event.Type != "message" || event.Message.Type != "text" {
			continue
		}

		// Send immediate "thinking..." reply using reply token (burns it, 30s expiry).
		if event.ReplyToken != "" {
			go sendLINEThinkingReply(event.ReplyToken, creds.ChannelAccessToken)
		}

		return &daemon.MessagePayload{
			Channel:   daemon.ChannelLINE,
			ThreadID:  event.Source.UserID,
			Sender:    event.Source.UserID,
			Text:      event.Message.Text,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	// No text message events — return 200 OK silently.
	w.WriteHeader(http.StatusOK)
	return nil, nil
}

func verifyLINESignature(r *http.Request, body []byte, channelSecret string) error {
	signature := r.Header.Get("X-Line-Signature")
	if signature == "" {
		return fmt.Errorf("missing X-Line-Signature header")
	}

	mac := hmac.New(sha256.New, []byte(channelSecret))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

// sendLINEThinkingReply sends an immediate "thinking..." message using the reply token.
// This burns the reply token (30s expiry). Actual response will use Push Message API.
func sendLINEThinkingReply(replyToken, accessToken string) {
	payload := map[string]interface{}{
		"replyToken": replyToken,
		"messages": []map[string]string{
			{"type": "text", "text": "Thinking..."},
		},
	}
	body, _ := json.Marshal(payload)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.line.me/v2/bot/message/reply", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}
