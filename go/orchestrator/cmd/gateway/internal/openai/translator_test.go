package openai

import (
	"testing"
)

func TestExtractQuery(t *testing.T) {
	registry := newDefaultRegistry()
	translator := NewTranslator(registry)

	tests := []struct {
		name     string
		messages []ChatMessage
		expected string
	}{
		{
			name: "single user message",
			messages: []ChatMessage{
				{Role: "user", Content: "Hello world"},
			},
			expected: "Hello world",
		},
		{
			name: "user message with system",
			messages: []ChatMessage{
				{Role: "system", Content: "You are helpful"},
				{Role: "user", Content: "What is AI?"},
			},
			expected: "What is AI?",
		},
		{
			name: "multi-turn conversation",
			messages: []ChatMessage{
				{Role: "system", Content: "You are helpful"},
				{Role: "user", Content: "First question"},
				{Role: "assistant", Content: "First answer"},
				{Role: "user", Content: "Second question"},
			},
			expected: "Second question",
		},
		{
			name: "empty user message skipped",
			messages: []ChatMessage{
				{Role: "user", Content: ""},
				{Role: "user", Content: "Valid question"},
			},
			expected: "Valid question",
		},
		{
			name:     "no user message",
			messages: []ChatMessage{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := translator.extractQuery(tt.messages)
			if result != tt.expected {
				t.Errorf("extractQuery() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExtractSystemPrompt(t *testing.T) {
	registry := newDefaultRegistry()
	translator := NewTranslator(registry)

	tests := []struct {
		name     string
		messages []ChatMessage
		expected string
	}{
		{
			name: "has system message",
			messages: []ChatMessage{
				{Role: "system", Content: "You are a pirate"},
				{Role: "user", Content: "Hello"},
			},
			expected: "You are a pirate",
		},
		{
			name: "no system message",
			messages: []ChatMessage{
				{Role: "user", Content: "Hello"},
			},
			expected: "",
		},
		{
			name: "multiple system messages - first wins",
			messages: []ChatMessage{
				{Role: "system", Content: "First system"},
				{Role: "system", Content: "Second system"},
				{Role: "user", Content: "Hello"},
			},
			expected: "First system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := translator.extractSystemPrompt(tt.messages)
			if result != tt.expected {
				t.Errorf("extractSystemPrompt() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestBuildConversationHistory(t *testing.T) {
	registry := newDefaultRegistry()
	translator := NewTranslator(registry)

	tests := []struct {
		name         string
		messages     []ChatMessage
		expectedLen  int
		checkContent func([]map[string]string) bool
	}{
		{
			name: "excludes system and last user",
			messages: []ChatMessage{
				{Role: "system", Content: "System prompt"},
				{Role: "user", Content: "First question"},
				{Role: "assistant", Content: "First answer"},
				{Role: "user", Content: "Second question"},
			},
			expectedLen: 2, // first user + assistant (system and last user excluded)
			checkContent: func(history []map[string]string) bool {
				return history[0]["role"] == "user" && history[0]["content"] == "First question" &&
					history[1]["role"] == "assistant" && history[1]["content"] == "First answer"
			},
		},
		{
			name: "single user message - empty history",
			messages: []ChatMessage{
				{Role: "user", Content: "Only question"},
			},
			expectedLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := translator.buildConversationHistory(tt.messages)
			if len(result) != tt.expectedLen {
				t.Errorf("buildConversationHistory() len = %d, want %d", len(result), tt.expectedLen)
			}
			if tt.checkContent != nil && !tt.checkContent(result) {
				t.Errorf("buildConversationHistory() content check failed")
			}
		})
	}
}

func TestDeriveSessionID(t *testing.T) {
	registry := newDefaultRegistry()
	translator := NewTranslator(registry)

	tests := []struct {
		name        string
		req         *ChatCompletionRequest
		checkPrefix string
	}{
		{
			name: "with user field",
			req: &ChatCompletionRequest{
				User: "user-123",
				Messages: []ChatMessage{
					{Role: "user", Content: "Hello"},
				},
			},
			checkPrefix: "openai-user-123",
		},
		{
			name: "without user field - hash based",
			req: &ChatCompletionRequest{
				Messages: []ChatMessage{
					{Role: "system", Content: "System prompt"},
					{Role: "user", Content: "Hello world"},
				},
			},
			checkPrefix: "openai-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := translator.deriveSessionID(tt.req)
			if tt.checkPrefix != "" {
				if tt.req.User != "" {
					if result != tt.checkPrefix {
						t.Errorf("deriveSessionID() = %q, want %q", result, tt.checkPrefix)
					}
				} else {
					if len(result) < len(tt.checkPrefix) {
						t.Errorf("deriveSessionID() = %q, want prefix %q", result, tt.checkPrefix)
					}
				}
			}
		})
	}
}

func TestTranslate(t *testing.T) {
	registry := newDefaultRegistry()
	translator := NewTranslator(registry)

	t.Run("valid request", func(t *testing.T) {
		req := &ChatCompletionRequest{
			Model: "shannon-chat",
			Messages: []ChatMessage{
				{Role: "user", Content: "Hello"},
			},
			Stream:      true,
			MaxTokens:   100,
			Temperature: 0.8,
		}

		result, err := translator.Translate(req, "user-123", "tenant-456")
		if err != nil {
			t.Fatalf("Translate() error = %v", err)
		}

		if result.ModelName != "shannon-chat" {
			t.Errorf("ModelName = %q, want %q", result.ModelName, "shannon-chat")
		}
		if !result.Stream {
			t.Error("Stream = false, want true")
		}
		if result.GRPCRequest.Query != "Hello" {
			t.Errorf("Query = %q, want %q", result.GRPCRequest.Query, "Hello")
		}
		if result.GRPCRequest.Metadata.UserId != "user-123" {
			t.Errorf("UserId = %q, want %q", result.GRPCRequest.Metadata.UserId, "user-123")
		}
	})

	t.Run("empty messages", func(t *testing.T) {
		req := &ChatCompletionRequest{
			Model:    "shannon-chat",
			Messages: []ChatMessage{},
		}

		_, err := translator.Translate(req, "user-123", "tenant-456")
		if err == nil {
			t.Error("Translate() expected error for empty messages")
		}
	})

	t.Run("invalid model", func(t *testing.T) {
		req := &ChatCompletionRequest{
			Model: "invalid-model",
			Messages: []ChatMessage{
				{Role: "user", Content: "Hello"},
			},
		}

		_, err := translator.Translate(req, "user-123", "tenant-456")
		if err == nil {
			t.Error("Translate() expected error for invalid model")
		}
	})

	t.Run("default model", func(t *testing.T) {
		req := &ChatCompletionRequest{
			Model: "", // Empty model should use default
			Messages: []ChatMessage{
				{Role: "user", Content: "Hello"},
			},
		}

		result, err := translator.Translate(req, "user-123", "tenant-456")
		if err != nil {
			t.Fatalf("Translate() error = %v", err)
		}

		if result.ModelName != "shannon-chat" {
			t.Errorf("ModelName = %q, want default %q", result.ModelName, "shannon-chat")
		}
	})
}
