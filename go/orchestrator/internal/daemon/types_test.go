package daemon

import (
	"encoding/json"
	"testing"
)

func TestServerMessageMarshal(t *testing.T) {
	payload, _ := json.Marshal(MessagePayload{
		Channel:   "slack",
		ThreadID:  "C07-1234",
		Sender:    "@alice",
		Text:      "check logs",
		AgentName: "ops-bot",
		Timestamp: "2026-03-07T12:00:00Z",
	})
	msg := ServerMessage{
		Type:      MsgTypeMessage,
		MessageID: "msg-abc",
		Payload:   payload,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ServerMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != MsgTypeMessage {
		t.Errorf("got type %q, want %q", decoded.Type, MsgTypeMessage)
	}
	if decoded.MessageID != "msg-abc" {
		t.Errorf("got message_id %q, want %q", decoded.MessageID, "msg-abc")
	}
	var p MessagePayload
	if err := json.Unmarshal(decoded.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Channel != "slack" || p.AgentName != "ops-bot" {
		t.Errorf("payload mismatch: channel=%q agent=%q", p.Channel, p.AgentName)
	}
}

func TestDaemonMessageMarshal(t *testing.T) {
	payload, _ := json.Marshal(ReplyPayload{
		Channel:  "slack",
		ThreadID: "C07-1234",
		Text:     "Logs show OOM at 3:42am",
		Format:   FormatText,
	})
	msg := DaemonMessage{
		Type:      MsgTypeReply,
		MessageID: "msg-abc",
		Payload:   payload,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded DaemonMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != MsgTypeReply {
		t.Errorf("got type %q, want %q", decoded.Type, MsgTypeReply)
	}
}

func TestIsSystemChannel(t *testing.T) {
	if !IsSystemChannel("system") {
		t.Error("expected system to be a system channel")
	}
	if IsSystemChannel("slack") {
		t.Error("expected slack NOT to be a system channel")
	}
}

func TestMessagePayloadOmitEmpty(t *testing.T) {
	p := MessagePayload{
		Channel:   "slack",
		Text:      "hello",
		Timestamp: "2026-03-07T12:00:00Z",
	}
	data, _ := json.Marshal(p)
	s := string(data)
	if contains(s, "agent_name") {
		t.Error("expected agent_name to be omitted when empty")
	}
}

func TestReplyPayloadFormatOmitEmpty(t *testing.T) {
	p := ReplyPayload{Channel: "slack", ThreadID: "t1", Text: "hi"}
	data, _ := json.Marshal(p)
	if contains(string(data), "format") {
		t.Error("expected format to be omitted when empty")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
