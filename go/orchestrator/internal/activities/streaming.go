package activities

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"
)

// StreamExecuteInput represents input for streaming execution
type StreamExecuteInput struct {
	Query     string                 `json:"query"`
	Context   map[string]interface{} `json:"context"`
	SessionID string                 `json:"session_id"`
	AgentID   string                 `json:"agent_id"`
	Mode      string                 `json:"mode"`
}

// StreamProgress represents the progress of a streaming operation
type StreamProgress struct {
	Tokens        int    `json:"tokens"`
	PartialResult string `json:"partial_result"`
	Complete      bool   `json:"complete"`
	Error         string `json:"error,omitempty"`
}

// StreamingActivities handles streaming execution of agents
type StreamingActivities struct {
	// In production, this would connect to the actual LLM service
	// For now, we'll simulate streaming
}

// NewStreamingActivities creates a new streaming activities handler
func NewStreamingActivities() *StreamingActivities {
	return &StreamingActivities{}
}

// StreamExecute executes an agent with streaming output
func (s *StreamingActivities) StreamExecute(ctx context.Context, input StreamExecuteInput) (string, error) {
	logger := activity.GetLogger(ctx)
	info := activity.GetInfo(ctx)
	
	logger.Info("Starting streaming execution",
		"agent_id", input.AgentID,
		"session_id", input.SessionID,
		"activity_id", info.ActivityID,
	)
	
	// Simulate streaming by breaking the response into chunks
	// In production, this would connect to the LLM service's streaming API
	fullResponse := s.simulateAgentResponse(input.Query, input.Context)
	tokens := strings.Fields(fullResponse)
	
	var buffer strings.Builder
	tokenCount := 0
	heartbeatInterval := 10 // Send heartbeat every 10 tokens
	
	for i, token := range tokens {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		
		// Add token to buffer
		if i > 0 {
			buffer.WriteString(" ")
		}
		buffer.WriteString(token)
		tokenCount++
		
		// Send heartbeat with progress
		if tokenCount%heartbeatInterval == 0 || i == len(tokens)-1 {
			progress := StreamProgress{
				Tokens:        tokenCount,
				PartialResult: buffer.String(),
				Complete:      i == len(tokens)-1,
			}
			
			activity.RecordHeartbeat(ctx, progress)
			
			logger.Debug("Streaming progress",
				"tokens", tokenCount,
				"complete", progress.Complete,
			)
		}
	}
	
	finalResult := buffer.String()
	
	logger.Info("Streaming execution completed",
		"agent_id", input.AgentID,
		"total_tokens", tokenCount,
		"result_length", len(finalResult),
	)
	
	return finalResult, nil
}

// StreamExecuteWithCallback executes with a callback for each chunk
func (s *StreamingActivities) StreamExecuteWithCallback(
	ctx context.Context,
	input StreamExecuteInput,
	callback func(StreamProgress) error,
) error {
	logger := activity.GetLogger(ctx)
	
	logger.Info("Starting streaming with callback",
		"agent_id", input.AgentID,
		"session_id", input.SessionID,
	)
	
	// Simulate streaming
	fullResponse := s.simulateAgentResponse(input.Query, input.Context)
	tokens := strings.Fields(fullResponse)
	
	var buffer strings.Builder
	tokenCount := 0
	
	for i, token := range tokens {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		
		// Add token to buffer
		if i > 0 {
			buffer.WriteString(" ")
		}
		buffer.WriteString(token)
		tokenCount++
		
		// Create progress update
		progress := StreamProgress{
			Tokens:        tokenCount,
			PartialResult: buffer.String(),
			Complete:      i == len(tokens)-1,
		}
		
		// Call the callback
		if err := callback(progress); err != nil {
			logger.Error("Callback error", "error", err)
			return fmt.Errorf("callback error: %w", err)
		}
	}
	
	return nil
}

// BatchStreamExecute executes multiple agents with streaming
func (s *StreamingActivities) BatchStreamExecute(
	ctx context.Context,
	inputs []StreamExecuteInput,
) ([]string, error) {
	logger := activity.GetLogger(ctx)
	
	logger.Info("Starting batch streaming execution",
		"num_agents", len(inputs),
	)
	
	results := make([]string, len(inputs))
	totalTokens := 0
	
	for i, input := range inputs {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		
		// Execute with streaming
		result, err := s.StreamExecute(ctx, input)
		if err != nil {
			logger.Error("Stream execution failed",
				"index", i,
				"agent_id", input.AgentID,
				"error", err,
			)
			return nil, fmt.Errorf("agent %s failed: %w", input.AgentID, err)
		}
		
		results[i] = result
		totalTokens += len(strings.Fields(result))
		
		// Record progress for the batch
		activity.RecordHeartbeat(ctx, map[string]interface{}{
			"completed": i + 1,
			"total":     len(inputs),
			"tokens":    totalTokens,
		})
	}
	
	logger.Info("Batch streaming completed",
		"num_agents", len(inputs),
		"total_tokens", totalTokens,
	)
	
	return results, nil
}

// simulateAgentResponse simulates an agent's response
// In production, this would call the actual LLM service
func (s *StreamingActivities) simulateAgentResponse(query string, context map[string]interface{}) string {
	// Generate a response based on the query
	response := fmt.Sprintf("Based on your query '%s', here is my analysis: ", query)
	
	// Add some context-aware content
	if val, exists := context["complexity_score"]; exists {
		response += fmt.Sprintf("The task has a complexity score of %.2f. ", val)
	}
	
	// Add some generic response text
	response += "I have analyzed the requirements and identified several key points. "
	response += "First, we need to consider the overall architecture and design patterns. "
	response += "Second, the implementation should follow best practices for maintainability. "
	response += "Third, performance optimization is crucial for scalability. "
	response += "Finally, comprehensive testing ensures reliability and correctness."
	
	return response
}

// StreamingResult represents a complete streaming result
type StreamingResult struct {
	AgentID      string    `json:"agent_id"`
	Result       string    `json:"result"`
	TokensUsed   int       `json:"tokens_used"`
	Duration     int64     `json:"duration_ms"`
	StreamedAt   time.Time `json:"streamed_at"`
}

// GetStreamingMetrics returns metrics for streaming operations
func (s *StreamingActivities) GetStreamingMetrics(ctx context.Context) (map[string]interface{}, error) {
	// In production, this would return actual metrics
	return map[string]interface{}{
		"total_streams":     42,
		"active_streams":    3,
		"avg_tokens_per_stream": 150,
		"avg_duration_ms":   2500,
		"error_rate":        0.02,
	}, nil
}