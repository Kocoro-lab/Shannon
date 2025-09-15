package session

import (
	"errors"
	"time"
)

var (
	// ErrSessionNotFound is returned when a session doesn't exist
	ErrSessionNotFound = errors.New("session not found")
	
	// ErrSessionExpired is returned when a session has expired
	ErrSessionExpired = errors.New("session expired")
	
	// ErrInvalidSession is returned when session data is invalid
	ErrInvalidSession = errors.New("invalid session")
)

// Session represents a user session with context continuity
type Session struct {
    ID        string                 `json:"id"`
    UserID    string                 `json:"user_id"`
    TenantID  string                 `json:"tenant_id"`
    CreatedAt time.Time              `json:"created_at"`
    UpdatedAt time.Time              `json:"updated_at"`
    ExpiresAt time.Time              `json:"expires_at"`
	Metadata  map[string]interface{} `json:"metadata"`
	Context   map[string]interface{} `json:"context"`
	History   []Message              `json:"history"`
	
	// Agent-specific context
	AgentStates map[string]*AgentState `json:"agent_states,omitempty"`
	
	// Token tracking
	TotalTokensUsed int     `json:"total_tokens_used"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
}

// Message represents a message in the session history
type Message struct {
	ID        string                 `json:"id"`
	Role      string                 `json:"role"` // "user", "assistant", "system"
	Content   string                 `json:"content"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	
	// Token tracking for this message
	TokensUsed int     `json:"tokens_used,omitempty"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
}

// AgentState represents the state of a specific agent in the session
type AgentState struct {
	AgentID      string                 `json:"agent_id"`
	LastActive   time.Time              `json:"last_active"`
	State        string                 `json:"state"`
	Memory       map[string]interface{} `json:"memory"`
	ToolsUsed    []string               `json:"tools_used"`
	TokensUsed   int                    `json:"tokens_used"`
}

// IsExpired checks if the session has expired
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// GetContextValue retrieves a value from the session context
func (s *Session) GetContextValue(key string) (interface{}, bool) {
	if s.Context == nil {
		return nil, false
	}
	val, ok := s.Context[key]
	return val, ok
}

// SetContextValue sets a value in the session context
func (s *Session) SetContextValue(key string, value interface{}) {
	if s.Context == nil {
		s.Context = make(map[string]interface{})
	}
	s.Context[key] = value
	s.UpdatedAt = time.Now()
}

// GetAgentState retrieves the state for a specific agent
func (s *Session) GetAgentState(agentID string) (*AgentState, bool) {
	if s.AgentStates == nil {
		return nil, false
	}
	state, ok := s.AgentStates[agentID]
	return state, ok
}

// SetAgentState sets the state for a specific agent
func (s *Session) SetAgentState(agentID string, state *AgentState) {
	if s.AgentStates == nil {
		s.AgentStates = make(map[string]*AgentState)
	}
	s.AgentStates[agentID] = state
	s.UpdatedAt = time.Now()
}

// GetRecentHistory returns the most recent messages from history
func (s *Session) GetRecentHistory(count int) []Message {
	if len(s.History) <= count {
		return s.History
	}
	return s.History[len(s.History)-count:]
}

// GetHistorySummary returns a summary suitable for LLM context
func (s *Session) GetHistorySummary(maxTokens int) string {
	// Simple implementation - in production, use proper summarization
	summary := ""
	currentTokens := 0
	
	// Start from most recent messages
	for i := len(s.History) - 1; i >= 0; i-- {
		msg := s.History[i]
		// Rough token estimate: 1 token per 4 characters
		msgTokens := len(msg.Content) / 4
		
		if currentTokens+msgTokens > maxTokens {
			break
		}
		
		// Prepend to maintain chronological order
		summary = formatMessage(msg) + "\n" + summary
		currentTokens += msgTokens
	}
	
	return summary
}

func formatMessage(msg Message) string {
	return msg.Role + ": " + msg.Content
}

// UpdateTokenUsage updates the token usage for the session
func (s *Session) UpdateTokenUsage(tokens int, cost float64) {
	s.TotalTokensUsed += tokens
	s.TotalCostUSD += cost
	s.UpdatedAt = time.Now()
}
