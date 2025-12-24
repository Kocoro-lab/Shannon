package openai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateCompletionID(t *testing.T) {
	id := GenerateCompletionID()

	if !strings.HasPrefix(id, "chatcmpl-") {
		t.Errorf("GenerateCompletionID() = %q, want prefix 'chatcmpl-'", id)
	}

	// Check uniqueness
	id2 := GenerateCompletionID()
	if id == id2 {
		t.Error("GenerateCompletionID() generated duplicate IDs")
	}
}

func TestNewErrorResponse(t *testing.T) {
	resp := NewErrorResponse("Test error", ErrorTypeInvalidRequest, ErrorCodeInvalidRequest)

	if resp.Error.Message != "Test error" {
		t.Errorf("Error.Message = %q, want %q", resp.Error.Message, "Test error")
	}
	if resp.Error.Type != ErrorTypeInvalidRequest {
		t.Errorf("Error.Type = %q, want %q", resp.Error.Type, ErrorTypeInvalidRequest)
	}
	if resp.Error.Code != ErrorCodeInvalidRequest {
		t.Errorf("Error.Code = %q, want %q", resp.Error.Code, ErrorCodeInvalidRequest)
	}
}

func TestChatCompletionRequestJSON(t *testing.T) {
	jsonStr := `{
		"model": "shannon-chat",
		"messages": [
			{"role": "system", "content": "You are helpful"},
			{"role": "user", "content": "Hello"}
		],
		"stream": true,
		"max_tokens": 100,
		"temperature": 0.8,
		"top_p": 0.9,
		"stop": ["END"],
		"user": "user-123"
	}`

	var req ChatCompletionRequest
	err := json.Unmarshal([]byte(jsonStr), &req)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if req.Model != "shannon-chat" {
		t.Errorf("Model = %q, want %q", req.Model, "shannon-chat")
	}
	if len(req.Messages) != 2 {
		t.Errorf("len(Messages) = %d, want 2", len(req.Messages))
	}
	if !req.Stream {
		t.Error("Stream = false, want true")
	}
	if req.MaxTokens != 100 {
		t.Errorf("MaxTokens = %d, want 100", req.MaxTokens)
	}
	if req.Temperature != 0.8 {
		t.Errorf("Temperature = %f, want 0.8", req.Temperature)
	}
	if req.TopP != 0.9 {
		t.Errorf("TopP = %f, want 0.9", req.TopP)
	}
	if len(req.Stop) != 1 || req.Stop[0] != "END" {
		t.Errorf("Stop = %v, want [END]", req.Stop)
	}
	if req.User != "user-123" {
		t.Errorf("User = %q, want %q", req.User, "user-123")
	}
}

func TestChatCompletionResponseJSON(t *testing.T) {
	resp := &ChatCompletionResponse{
		ID:      "chatcmpl-abc123",
		Object:  "chat.completion",
		Created: 1703318400,
		Model:   "shannon-chat",
		Choices: []Choice{
			{
				Index: 0,
				Message: &ChatMessage{
					Role:    "assistant",
					Content: "Hello there!",
				},
				FinishReason: "stop",
			},
		},
		Usage: &Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Verify JSON structure
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if parsed["id"] != "chatcmpl-abc123" {
		t.Errorf("id = %v, want chatcmpl-abc123", parsed["id"])
	}
	if parsed["object"] != "chat.completion" {
		t.Errorf("object = %v, want chat.completion", parsed["object"])
	}

	choices := parsed["choices"].([]interface{})
	if len(choices) != 1 {
		t.Errorf("len(choices) = %d, want 1", len(choices))
	}

	usage := parsed["usage"].(map[string]interface{})
	if usage["total_tokens"].(float64) != 15 {
		t.Errorf("total_tokens = %v, want 15", usage["total_tokens"])
	}
}

func TestChatCompletionChunkJSON(t *testing.T) {
	chunk := ChatCompletionChunk{
		ID:      "chatcmpl-abc123",
		Object:  "chat.completion.chunk",
		Created: 1703318400,
		Model:   "shannon-chat",
		Choices: []Choice{
			{
				Index: 0,
				Delta: &ChatDelta{
					Role:    "assistant",
					Content: "Hello",
				},
			},
		},
	}

	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if parsed["object"] != "chat.completion.chunk" {
		t.Errorf("object = %v, want chat.completion.chunk", parsed["object"])
	}

	choices := parsed["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	delta := choice["delta"].(map[string]interface{})
	if delta["content"] != "Hello" {
		t.Errorf("delta.content = %v, want Hello", delta["content"])
	}
}

func TestModelObjectJSON(t *testing.T) {
	model := ModelObject{
		ID:      "shannon-chat",
		Object:  "model",
		Created: 1703318400,
		OwnedBy: "shannon",
	}

	data, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if parsed["id"] != "shannon-chat" {
		t.Errorf("id = %v, want shannon-chat", parsed["id"])
	}
	if parsed["owned_by"] != "shannon" {
		t.Errorf("owned_by = %v, want shannon", parsed["owned_by"])
	}
}

func TestErrorConstants(t *testing.T) {
	// Verify error type constants match OpenAI's format
	expectedTypes := map[string]string{
		ErrorTypeInvalidRequest: "invalid_request_error",
		ErrorTypeAuthentication: "authentication_error",
		ErrorTypePermission:     "permission_error",
		ErrorTypeNotFound:       "not_found_error",
		ErrorTypeRateLimit:      "rate_limit_error",
		ErrorTypeServer:         "server_error",
	}

	for constant, expected := range expectedTypes {
		if constant != expected {
			t.Errorf("Error type constant = %q, want %q", constant, expected)
		}
	}

	// Verify error code constants
	expectedCodes := map[string]string{
		ErrorCodeInvalidRequest:    "invalid_request",
		ErrorCodeInvalidAPIKey:     "invalid_api_key",
		ErrorCodeModelNotFound:     "model_not_found",
		ErrorCodeRateLimitExceeded: "rate_limit_exceeded",
		ErrorCodeInternalError:     "internal_error",
	}

	for constant, expected := range expectedCodes {
		if constant != expected {
			t.Errorf("Error code constant = %q, want %q", constant, expected)
		}
	}
}
