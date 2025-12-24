package openai

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	commonpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/common"
	orchpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"google.golang.org/protobuf/types/known/structpb"
)

// TranslatedRequest contains the Shannon-native request and metadata.
type TranslatedRequest struct {
	GRPCRequest *orchpb.SubmitTaskRequest
	SessionID   string
	ModelName   string
	Stream      bool
}

// Translator converts OpenAI requests to Shannon format.
type Translator struct {
	registry *Registry
}

// NewTranslator creates a new request translator.
func NewTranslator(registry *Registry) *Translator {
	return &Translator{registry: registry}
}

// Translate converts an OpenAI ChatCompletionRequest to a Shannon SubmitTaskRequest.
func (t *Translator) Translate(req *ChatCompletionRequest, userID, tenantID string) (*TranslatedRequest, error) {
	// Validate request
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("messages array is required")
	}

	// Get model configuration
	modelName := req.Model
	if modelName == "" {
		modelName = t.registry.GetDefaultModel()
	}

	modelConfig, err := t.registry.GetModel(modelName)
	if err != nil {
		return nil, fmt.Errorf("invalid model: %s", modelName)
	}

	// Extract the query from messages
	query := t.extractQuery(req.Messages)
	if query == "" {
		return nil, fmt.Errorf("no user message found in messages array")
	}

	// Generate or derive session ID
	sessionID := t.deriveSessionID(req)

	// Build context map from model config + request parameters
	ctxMap := t.buildContext(req, modelConfig)

	// Convert context to protobuf struct
	ctxStruct, err := structpb.NewStruct(ctxMap)
	if err != nil {
		return nil, fmt.Errorf("failed to build context: %w", err)
	}

	// Build labels for mode routing
	labels := map[string]string{}
	switch modelConfig.WorkflowMode {
	case "simple":
		labels["mode"] = "simple"
	case "research":
		labels["mode"] = "standard" // Research uses standard routing with force_research
	case "supervisor":
		labels["mode"] = "supervisor"
	default:
		labels["mode"] = "simple"
	}

	// Build the gRPC request
	grpcReq := &orchpb.SubmitTaskRequest{
		Metadata: &commonpb.TaskMetadata{
			UserId:    userID,
			TenantId:  tenantID,
			SessionId: sessionID,
			Labels:    labels,
		},
		Query:   query,
		Context: ctxStruct,
	}

	return &TranslatedRequest{
		GRPCRequest: grpcReq,
		SessionID:   sessionID,
		ModelName:   modelName,
		Stream:      req.Stream,
	}, nil
}

// TranslateWithSession converts an OpenAI request using an existing session result.
func (t *Translator) TranslateWithSession(req *ChatCompletionRequest, userID, tenantID string, session *SessionResult) (*TranslatedRequest, error) {
	// Validate request
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("messages array is required")
	}

	// Get model configuration
	modelName := req.Model
	if modelName == "" {
		modelName = t.registry.GetDefaultModel()
	}

	modelConfig, err := t.registry.GetModel(modelName)
	if err != nil {
		return nil, fmt.Errorf("invalid model: %s", modelName)
	}

	// Extract the query from messages
	query := t.extractQuery(req.Messages)
	if query == "" {
		return nil, fmt.Errorf("no user message found in messages array")
	}

	// Use session from session manager
	sessionID := ""
	if session != nil {
		sessionID = session.ShannonSession
	}
	if sessionID == "" {
		sessionID = t.deriveSessionID(req)
	}

	// Build context map from model config + request parameters
	ctxMap := t.buildContext(req, modelConfig)

	// Convert context to protobuf struct
	ctxStruct, err := structpb.NewStruct(ctxMap)
	if err != nil {
		return nil, fmt.Errorf("failed to build context: %w", err)
	}

	// Build labels for mode routing
	labels := map[string]string{}
	switch modelConfig.WorkflowMode {
	case "simple":
		labels["mode"] = "simple"
	case "research":
		labels["mode"] = "standard"
	case "supervisor":
		labels["mode"] = "supervisor"
	default:
		labels["mode"] = "simple"
	}

	// Build the gRPC request
	grpcReq := &orchpb.SubmitTaskRequest{
		Metadata: &commonpb.TaskMetadata{
			UserId:    userID,
			TenantId:  tenantID,
			SessionId: sessionID,
			Labels:    labels,
		},
		Query:   query,
		Context: ctxStruct,
	}

	return &TranslatedRequest{
		GRPCRequest: grpcReq,
		SessionID:   sessionID,
		ModelName:   modelName,
		Stream:      req.Stream,
	}, nil
}

// extractQuery extracts the query from the messages array.
// Uses the last user message as the primary query.
func (t *Translator) extractQuery(messages []ChatMessage) string {
	// Find the last user message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
			return messages[i].Content
		}
	}
	return ""
}

// deriveSessionID generates a session ID from the conversation.
// Uses hash of system message + first user message for consistency.
func (t *Translator) deriveSessionID(req *ChatCompletionRequest) string {
	// If user provided a custom user ID, use it as session basis
	if req.User != "" {
		return "openai-" + req.User
	}

	// Build a hash from conversation start
	var parts []string
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			parts = append(parts, msg.Content)
			break
		}
	}
	// Add first user message
	for _, msg := range req.Messages {
		if msg.Role == "user" {
			parts = append(parts, msg.Content[:min(100, len(msg.Content))])
			break
		}
	}

	if len(parts) == 0 {
		// Fallback: generate unique session
		return "openai-" + GenerateCompletionID()
	}

	// Hash the parts
	h := sha256.New()
	h.Write([]byte(strings.Join(parts, "|")))
	hash := hex.EncodeToString(h.Sum(nil))[:16]
	return "openai-" + hash
}

// buildContext creates the Shannon context map from request parameters.
func (t *Translator) buildContext(req *ChatCompletionRequest, modelConfig *ModelConfig) map[string]interface{} {
	ctx := make(map[string]interface{})

	// Copy model config context
	for k, v := range modelConfig.Context {
		ctx[k] = v
	}

	// Apply max_tokens
	if req.MaxTokens > 0 {
		maxLimit := t.registry.GetMaxTokensLimit()
		if req.MaxTokens > maxLimit {
			ctx["max_tokens"] = maxLimit
		} else {
			ctx["max_tokens"] = req.MaxTokens
		}
	} else if modelConfig.MaxTokensDefault > 0 {
		ctx["max_tokens"] = modelConfig.MaxTokensDefault
	}

	// Apply temperature (pointer check to properly handle temperature=0)
	if req.Temperature != nil {
		ctx["temperature"] = *req.Temperature
	}

	// Apply top_p (pointer check to properly handle top_p=0)
	if req.TopP != nil {
		ctx["top_p"] = *req.TopP
	}

	// Apply stop sequences - convert to []interface{} for structpb compatibility
	if len(req.Stop) > 0 {
		stopSeqs := make([]interface{}, len(req.Stop))
		for i, s := range req.Stop {
			stopSeqs[i] = s
		}
		ctx["stop"] = stopSeqs
	}

	// Apply user ID for tracking
	if req.User != "" {
		ctx["openai_user"] = req.User
	}

	// Build system prompt from conversation context
	systemPrompt := t.extractSystemPrompt(req.Messages)
	if systemPrompt != "" {
		ctx["system_prompt"] = systemPrompt
	}

	// Include conversation history for context (except last user message which is the query)
	history := t.buildConversationHistory(req.Messages)
	if len(history) > 0 {
		// Convert to []interface{} with map[string]interface{} for structpb compatibility
		historyIntf := make([]interface{}, len(history))
		for i, h := range history {
			historyIntf[i] = map[string]interface{}{
				"role":    h["role"],
				"content": h["content"],
			}
		}
		ctx["conversation_history"] = historyIntf
	}

	return ctx
}

// extractSystemPrompt extracts the system prompt from messages.
func (t *Translator) extractSystemPrompt(messages []ChatMessage) string {
	for _, msg := range messages {
		if msg.Role == "system" {
			return msg.Content
		}
	}
	return ""
}

// buildConversationHistory formats previous messages for context.
func (t *Translator) buildConversationHistory(messages []ChatMessage) []map[string]string {
	var history []map[string]string

	// Skip system messages and the last user message (which is the query)
	lastUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}

	for i, msg := range messages {
		if msg.Role == "system" {
			continue // System prompt handled separately
		}
		if i == lastUserIdx {
			continue // Last user message is the query
		}
		history = append(history, map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	return history
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
