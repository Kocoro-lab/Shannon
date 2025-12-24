// Package openai provides OpenAI-compatible API endpoints for Shannon Cloud.
package openai

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// ChatCompletionRequest represents an OpenAI-compatible chat completion request.
// See: https://platform.openai.com/docs/api-reference/chat/create
type ChatCompletionRequest struct {
	Model            string         `json:"model"`
	Messages         []ChatMessage  `json:"messages"`
	Stream           bool           `json:"stream,omitempty"`
	MaxTokens        int            `json:"max_tokens,omitempty"`
	Temperature      *float64       `json:"temperature,omitempty"` // Pointer to distinguish 0 from unset
	TopP             *float64       `json:"top_p,omitempty"`       // Pointer to distinguish 0 from unset
	N                int            `json:"n,omitempty"`
	Stop             []string       `json:"stop,omitempty"`
	PresencePenalty  float64        `json:"presence_penalty,omitempty"`
	FrequencyPenalty float64        `json:"frequency_penalty,omitempty"`
	User             string         `json:"user,omitempty"`
	StreamOptions    *StreamOptions `json:"stream_options,omitempty"`
}

// ChatMessage represents a single message in the conversation.
type ChatMessage struct {
	Role    string `json:"role"` // system, user, assistant
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// StreamOptions controls streaming behavior.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// ChatCompletionResponse represents a non-streaming response.
type ChatCompletionResponse struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"` // "chat.completion"
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	Choices           []Choice `json:"choices"`
	Usage             *Usage   `json:"usage,omitempty"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
}

// Choice represents a single completion choice.
type Choice struct {
	Index        int          `json:"index"`
	Message      *ChatMessage `json:"message,omitempty"`       // For non-streaming
	Delta        *ChatDelta   `json:"delta,omitempty"`         // For streaming
	FinishReason string       `json:"finish_reason,omitempty"` // stop, length, tool_calls, null
}

// ChatDelta represents incremental content in streaming responses.
type ChatDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// Usage represents token usage statistics.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChunk represents a single streaming chunk.
type ChatCompletionChunk struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"` // "chat.completion.chunk"
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Choices           []Choice       `json:"choices"`
	Usage             *Usage         `json:"usage,omitempty"` // Only in final chunk if requested
	SystemFingerprint string         `json:"system_fingerprint,omitempty"`
	ShannonEvents     []ShannonEvent `json:"shannon_events,omitempty"` // Shannon-specific agent events
}

// ShannonEvent represents an agent lifecycle or progress event.
// These are Shannon-specific extensions to the OpenAI streaming format.
type ShannonEvent struct {
	Type      string                 `json:"type"`                // Event type (AGENT_STARTED, TOOL_INVOKED, etc.)
	AgentID   string                 `json:"agent_id,omitempty"`  // Agent identifier (e.g., "Ryogoku", "synthesis")
	Message   string                 `json:"message,omitempty"`   // Human-readable message
	Timestamp int64                  `json:"timestamp,omitempty"` // Unix timestamp
	Payload   map[string]interface{} `json:"payload,omitempty"`   // Additional event data
}

// ModelObject represents a model in the /v1/models response.
type ModelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"` // "model"
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelsResponse represents the /v1/models endpoint response.
type ModelsResponse struct {
	Object string        `json:"object"` // "list"
	Data   []ModelObject `json:"data"`
}

// ErrorResponse represents an OpenAI-compatible error response.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains the error information.
type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}

// NewErrorResponse creates a new error response.
func NewErrorResponse(message, errType, code string) *ErrorResponse {
	return &ErrorResponse{
		Error: ErrorDetail{
			Message: message,
			Type:    errType,
			Code:    code,
		},
	}
}

// Common error types matching OpenAI's error taxonomy.
const (
	ErrorTypeInvalidRequest = "invalid_request_error"
	ErrorTypeAuthentication = "authentication_error"
	ErrorTypePermission     = "permission_error"
	ErrorTypeNotFound       = "not_found_error"
	ErrorTypeRateLimit      = "rate_limit_error"
	ErrorTypeServer         = "server_error"
)

// Common error codes.
const (
	ErrorCodeInvalidRequest    = "invalid_request"
	ErrorCodeInvalidAPIKey     = "invalid_api_key"
	ErrorCodeModelNotFound     = "model_not_found"
	ErrorCodeRateLimitExceeded = "rate_limit_exceeded"
	ErrorCodeInsufficientQuota = "insufficient_quota"
	ErrorCodeInternalError     = "internal_error"
)

// GenerateCompletionID generates a unique completion ID.
func GenerateCompletionID() string {
	return "chatcmpl-" + generateID()
}

// generateID creates a unique ID string.
func generateID() string {
	// Use timestamp + random suffix for uniqueness
	return time.Now().Format("20060102150405") + randomSuffix(8)
}

// randomSuffix generates a cryptographically secure random alphanumeric suffix.
func randomSuffix(length int) string {
	bytes := make([]byte, (length+1)/2)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based (less secure but functional)
		return fmt.Sprintf("%016x", time.Now().UnixNano())[:length]
	}
	return hex.EncodeToString(bytes)[:length]
}
