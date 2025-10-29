package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	"go.uber.org/zap"
)

const (
	maxTitleLength = 60
	// sessionContextKeyTitle is the key for session title in the context JSONB field
	sessionContextKeyTitle = "title"
)

// GenerateSessionTitleInput is the input for generating a session title
type GenerateSessionTitleInput struct {
	SessionID string `json:"session_id"`
	Query     string `json:"query"`
}

// GenerateSessionTitleResult is the result of title generation
type GenerateSessionTitleResult struct {
	Title   string `json:"title"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// GenerateSessionTitle generates a short title for a session based on the first query
// Uses LLM to create a concise 3-5 word title, with fallback to query truncation
func (a *Activities) GenerateSessionTitle(ctx context.Context, input GenerateSessionTitleInput) (GenerateSessionTitleResult, error) {
	a.logger.Info("Generating session title",
		zap.String("session_id", input.SessionID),
		zap.Int("query_length", len(input.Query)),
	)

	// Validate input
	if input.SessionID == "" {
		return GenerateSessionTitleResult{
			Success: false,
			Error:   "session_id is required",
		}, fmt.Errorf("session_id is required")
	}

	if input.Query == "" {
		return GenerateSessionTitleResult{
			Success: false,
			Error:   "query is required",
		}, fmt.Errorf("query is required")
	}

	// Check if session exists and already has a title
	sess, err := a.sessionManager.GetSession(ctx, input.SessionID)
	if err != nil {
		a.logger.Error("Failed to get session for title generation",
			zap.Error(err),
			zap.String("session_id", input.SessionID),
		)
		return GenerateSessionTitleResult{
			Success: false,
			Error:   fmt.Sprintf("session not found: %v", err),
		}, err
	}

	if sess == nil {
		err := fmt.Errorf("session is nil for id: %s", input.SessionID)
		a.logger.Error("Session is nil after successful fetch",
			zap.String("session_id", input.SessionID),
		)
		return GenerateSessionTitleResult{
			Success: false,
			Error:   "session not found",
		}, err
	}

	// Check if session already has a title
	if sess.Context != nil {
		if existingTitle, ok := sess.Context[sessionContextKeyTitle].(string); ok && existingTitle != "" {
			a.logger.Info("Session already has a title, skipping generation",
				zap.String("session_id", input.SessionID),
				zap.String("existing_title", existingTitle),
			)
			return GenerateSessionTitleResult{
				Title:   existingTitle,
				Success: true,
			}, nil
		}
	}

	// Try LLM-based title generation
	title, err := a.generateTitleWithLLM(ctx, input.Query)
	if err != nil {
		a.logger.Warn("LLM title generation failed, using fallback",
			zap.Error(err),
			zap.String("session_id", input.SessionID),
		)
		// Fallback to truncated query
		title = generateFallbackTitle(input.Query)
	}

	// Ensure title meets length constraints (use runes to avoid UTF-8 corruption)
	titleRunes := []rune(title)
	if len(titleRunes) > maxTitleLength {
		title = string(titleRunes[:maxTitleLength-3]) + "..."
	}

	// Update session context with the generated title
	// First update Redis via session manager
	if err := a.sessionManager.UpdateContext(ctx, input.SessionID, sessionContextKeyTitle, title); err != nil {
		a.logger.Error("Failed to update session with title in Redis",
			zap.Error(err),
			zap.String("session_id", input.SessionID),
		)
		return GenerateSessionTitleResult{
			Title:   title,
			Success: false,
			Error:   fmt.Sprintf("failed to save title to Redis: %v", err),
		}, err
	}

	// Also update Postgres so HTTP APIs can see the title
	dbClient := GetGlobalDBClient()
	if dbClient != nil {
		db := dbClient.GetDB()

		// First, fetch existing context from PostgreSQL to preserve external_id and other DB-only fields
		var existingContext json.RawMessage
		err := db.QueryRowContext(ctx, `
			SELECT context
			FROM sessions
			WHERE (id::text = $1 OR context->>'external_id' = $1) AND deleted_at IS NULL
		`, input.SessionID).Scan(&existingContext)

		// Build updated context JSON with title
		contextData := make(map[string]interface{})

		// First load PostgreSQL context (to preserve external_id)
		if err == nil && existingContext != nil {
			if err := json.Unmarshal(existingContext, &contextData); err != nil {
				a.logger.Warn("Failed to unmarshal existing Postgres context",
					zap.Error(err),
					zap.String("session_id", input.SessionID),
				)
			}
		}

		// Then overlay Redis context (to get latest updates)
		if sess.Context != nil {
			for k, v := range sess.Context {
				contextData[k] = v
			}
		}

		// Finally add the title
		contextData[sessionContextKeyTitle] = title

		contextJSON, err := json.Marshal(contextData)
		if err != nil {
			a.logger.Warn("Failed to marshal context for Postgres update",
				zap.Error(err),
				zap.String("session_id", input.SessionID),
			)
		} else {
			// Update Postgres sessions.context (support both UUID and external_id)
			_, err = db.ExecContext(ctx, `
				UPDATE sessions
				SET context = $1, updated_at = NOW()
				WHERE (id::text = $2 OR context->>'external_id' = $2) AND deleted_at IS NULL
			`, contextJSON, input.SessionID)
			if err != nil {
				a.logger.Warn("Failed to update Postgres with title",
					zap.Error(err),
					zap.String("session_id", input.SessionID),
				)
			}
		}
	}

	a.logger.Info("Session title generated",
		zap.String("session_id", input.SessionID),
		zap.String("title", title),
	)

	return GenerateSessionTitleResult{
		Title:   title,
		Success: true,
	}, nil
}

// generateTitleWithLLM uses LLM to generate a concise title
func (a *Activities) generateTitleWithLLM(ctx context.Context, query string) (string, error) {
	// Prepare request to LLM service
	prompt := fmt.Sprintf("Summarize this user query in 3-5 words for a chat session title. Be concise and descriptive:\n\n%s", query)

	llmServiceURL := getEnvOrDefaultTitle("LLM_SERVICE_URL", "http://llm-service:8000")
	url := fmt.Sprintf("%s/agent/query", llmServiceURL)

	reqBody := map[string]interface{}{
		"query": prompt,
		"context": map[string]interface{}{
			"max_tokens":  20,
			"temperature": 0.3, // Low temperature for consistency
		},
		"agent_id": "title_generator",
		"session_context": map[string]interface{}{
			"system_prompt": "You are a title generator. Generate very short, descriptive titles (3-5 words) for chat sessions based on user queries. Output only the title, nothing else.",
		},
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Call LLM service
	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(reqJSON)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "title_generator")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("LLM service call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d from LLM service", resp.StatusCode)
	}

	// Parse response
	var result struct {
		Success  bool   `json:"success"`
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse LLM response: %w", err)
	}

	if !result.Success {
		return "", fmt.Errorf("LLM service returned success=false")
	}

	// Clean up the response
	title := strings.TrimSpace(result.Response)
	title = strings.Trim(title, `"'`)

	if title == "" {
		return "", fmt.Errorf("LLM returned empty title")
	}

	// Truncate by runes (characters) not bytes to avoid corrupting UTF-8
	titleRunes := []rune(title)
	if len(titleRunes) > maxTitleLength {
		title = string(titleRunes[:maxTitleLength-3]) + "..."
	}

	return title, nil
}

// getEnvOrDefaultTitle is a helper to get environment variables
func getEnvOrDefaultTitle(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// generateFallbackTitle creates a simple title from the query by truncating
func generateFallbackTitle(query string) string {
	// Remove leading/trailing whitespace
	title := strings.TrimSpace(query)

	// Take first line if multiline
	if idx := strings.Index(title, "\n"); idx > 0 {
		title = title[:idx]
	}

	// Truncate to reasonable length (use runes to handle UTF-8 properly)
	maxLen := 40
	titleRunes := []rune(title)
	if len(titleRunes) > maxLen {
		// Try to break at word boundary
		truncated := string(titleRunes[:maxLen])
		if lastSpace := strings.LastIndex(truncated, " "); lastSpace > 20 {
			truncated = truncated[:lastSpace]
		}
		title = truncated + "..."
	}

	return title
}
