package openai

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseContent_PlainString(t *testing.T) {
	raw := json.RawMessage(`"hello world"`)
	result, err := ParseContent(raw)
	require.NoError(t, err)
	assert.Equal(t, "hello world", result.Text)
	assert.Empty(t, result.Attachments)
}

func TestParseContent_ArrayWithTextOnly(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"hello"}]`)
	result, err := ParseContent(raw)
	require.NoError(t, err)
	assert.Equal(t, "hello", result.Text)
	assert.Empty(t, result.Attachments)
}

func TestParseContent_ArrayWithImage(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"text","text":"what is this?"},
		{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}
	]`)
	result, err := ParseContent(raw)
	require.NoError(t, err)
	assert.Equal(t, "what is this?", result.Text)
	require.Len(t, result.Attachments, 1)
	assert.Equal(t, "image/png", result.Attachments[0].MediaType)
	assert.Equal(t, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, result.Attachments[0].Data)
}

func TestParseContent_ArrayWithImageURL(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"text","text":"describe"},
		{"type":"image_url","image_url":{"url":"https://example.com/img.png"}}
	]`)
	result, err := ParseContent(raw)
	require.NoError(t, err)
	assert.Equal(t, "describe", result.Text)
	require.Len(t, result.Attachments, 1)
	assert.Equal(t, "url", result.Attachments[0].SourceType)
	assert.Equal(t, "https://example.com/img.png", result.Attachments[0].URL)
}

func TestParseContent_ArrayWithFile(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"text","text":"summarize this PDF"},
		{"type":"file","file":{"file_data":"data:application/pdf;base64,JVBERi0=","filename":"report.pdf"}}
	]`)
	result, err := ParseContent(raw)
	require.NoError(t, err)
	assert.Equal(t, "summarize this PDF", result.Text)
	require.Len(t, result.Attachments, 1)
	assert.Equal(t, "application/pdf", result.Attachments[0].MediaType)
	assert.Equal(t, "report.pdf", result.Attachments[0].Filename)
	assert.Equal(t, "base64", result.Attachments[0].SourceType)
}

func TestParseContent_RequestSizeLimit(t *testing.T) {
	// Create valid base64 data that decodes to > MaxDecodedAttachmentBytes
	// (25 MB after attachment-format-parity §2 P0). Constant kept in sync via
	// the attachments package — if the cap changes, this test follows.
	bigData := make([]byte, 26*1024*1024)
	for i := range bigData {
		bigData[i] = 'A'
	}
	b64Str := base64.StdEncoding.EncodeToString(bigData)
	dataURI := "data:image/png;base64," + b64Str
	raw, _ := json.Marshal([]map[string]interface{}{
		{"type": "image_url", "image_url": map[string]string{"url": dataURI}},
	})
	_, err := ParseContent(json.RawMessage(raw))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestParseContent_DataURIEmptyMediaType(t *testing.T) {
	// "data:;base64,..." has no media type — must be rejected
	raw := json.RawMessage(`[
		{"type":"image_url","image_url":{"url":"data:;base64,aGVsbG8="}}
	]`)
	_, err := ParseContent(json.RawMessage(raw))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing media type")
}

func TestParseContent_EmptyInput(t *testing.T) {
	result, err := ParseContent(nil)
	require.NoError(t, err)
	assert.Equal(t, "", result.Text)
	assert.Empty(t, result.Attachments)
}

func TestParseContent_MultipleTextBlocks(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"text","text":"line one"},
		{"type":"text","text":"line two"}
	]`)
	result, err := ParseContent(raw)
	require.NoError(t, err)
	assert.Equal(t, "line one\nline two", result.Text)
	assert.Empty(t, result.Attachments)
}

func TestParseContent_InvalidJSON(t *testing.T) {
	raw := json.RawMessage(`{invalid}`)
	_, err := ParseContent(raw)
	assert.Error(t, err)
}

func TestChatMessage_UnmarshalJSON(t *testing.T) {
	// Test backward compatibility: string content
	jsonStr := `{"role":"user","content":"hello"}`
	var msg ChatMessage
	err := json.Unmarshal([]byte(jsonStr), &msg)
	require.NoError(t, err)
	assert.Equal(t, "user", msg.Role)
	assert.Equal(t, "hello", msg.Content)

	// Test new format: array content
	jsonArr := `{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}]}`
	var msg2 ChatMessage
	err = json.Unmarshal([]byte(jsonArr), &msg2)
	require.NoError(t, err)
	assert.Equal(t, "hi", msg2.Content)
	require.Len(t, msg2.RawAttachments, 1)
}

func TestChatMessage_MarshalJSON_BackwardCompatible(t *testing.T) {
	// Ensure response marshalling still produces {"role":"...","content":"..."}
	msg := ChatMessage{
		Role:    "assistant",
		Content: "Hello there!",
	}
	data, err := json.Marshal(&msg)
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "assistant", parsed["role"])
	assert.Equal(t, "Hello there!", parsed["content"])
}

func TestChatMessage_UnmarshalJSON_WithName(t *testing.T) {
	jsonStr := `{"role":"user","content":"hello","name":"bob"}`
	var msg ChatMessage
	err := json.Unmarshal([]byte(jsonStr), &msg)
	require.NoError(t, err)
	assert.Equal(t, "bob", msg.Name)
	assert.Equal(t, "hello", msg.Content)
}

func TestChatMessage_ContentWithAttachmentSummary(t *testing.T) {
	jsonArr := `{"role":"user","content":[{"type":"text","text":"check this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}]}`
	var msg ChatMessage
	err := json.Unmarshal([]byte(jsonArr), &msg)
	require.NoError(t, err)
	assert.Contains(t, msg.ContentWithAttachmentSummary, "check this")
	assert.Contains(t, msg.ContentWithAttachmentSummary, "[Attached: image/png]")
}
