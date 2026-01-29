package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	"github.com/redis/go-redis/v9"
	"go.temporal.io/sdk/activity"
	"go.uber.org/zap"
)

// researchPlanLLMRequest is the HTTP request body for the LLM service
type researchPlanLLMRequest struct {
	Query        string                 `json:"query"`
	Context      map[string]interface{} `json:"context"`
	Conversation []ReviewRound          `json:"conversation"`
}

// researchPlanLLMResponse is the HTTP response from the LLM service
type researchPlanLLMResponse struct {
	Message      string `json:"message"`
	Intent       string `json:"intent"`
	Round        int    `json:"round"`
	Model        string `json:"model"`
	Provider     string `json:"provider"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

// reviewRedisState is the state stored in Redis for the review conversation
type reviewRedisState struct {
	WorkflowID    string                 `json:"workflow_id"`
	Query         string                 `json:"query"`
	Context       map[string]interface{} `json:"context"`
	Status        string                 `json:"status"`
	Round         int                    `json:"round"`
	Version       int                    `json:"version"`
	OwnerUserID   string                 `json:"owner_user_id"`
	OwnerTenantID string                 `json:"owner_tenant_id"`
	Rounds        []ReviewRound          `json:"rounds"`
	CurrentPlan   string                 `json:"current_plan"`
}

// GenerateResearchPlan calls the LLM service to generate an initial research plan
// and initializes the Redis review state. This is a Temporal Activity.
func GenerateResearchPlan(ctx context.Context, in ResearchPlanInput) (ResearchPlanResult, error) {
	logger := activity.GetLogger(ctx)

	// 1. Call LLM service
	base := os.Getenv("LLM_SERVICE_URL")
	if base == "" {
		base = "http://llm-service:8000"
	}
	url := fmt.Sprintf("%s/agent/research-plan", base)

	reqBody := researchPlanLLMRequest{
		Query:        in.Query,
		Context:      in.Context,
		Conversation: nil, // First round: no conversation
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return ResearchPlanResult{}, fmt.Errorf("failed to marshal research plan request: %w", err)
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return ResearchPlanResult{}, fmt.Errorf("failed to create research plan request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return ResearchPlanResult{}, fmt.Errorf("failed to call research plan service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ResearchPlanResult{}, fmt.Errorf("research plan service returned status %d", resp.StatusCode)
	}

	var llmResp researchPlanLLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return ResearchPlanResult{}, fmt.Errorf("failed to decode research plan response: %w", err)
	}

	if llmResp.Message == "" {
		return ResearchPlanResult{}, fmt.Errorf("research plan service returned empty message")
	}

	// Determine intent (default to "feedback" if LLM didn't provide one)
	intent := llmResp.Intent
	if intent == "" {
		intent = "feedback"
	}

	// Strip [RESEARCH_BRIEF]...[/RESEARCH_BRIEF] block (machine-consumed metadata, not for display)
	briefRegex := regexp.MustCompile(`(?s)\[RESEARCH_BRIEF\]\n?(.*?)\n?\[/RESEARCH_BRIEF\]`)
	displayMessage := llmResp.Message
	var researchBrief string
	if match := briefRegex.FindStringSubmatch(displayMessage); len(match) > 1 {
		researchBrief = strings.TrimSpace(match[1])
		displayMessage = strings.TrimSpace(briefRegex.ReplaceAllString(displayMessage, ""))
	}

	// Strip [INTENT:...] tag (already parsed above, not for display)
	// Non-anchored to match tag anywhere in string (LLM may place it mid-response)
	intentTagRegex := regexp.MustCompile(`\s*\[INTENT:\w+\]\s*`)
	displayMessage = strings.TrimSpace(intentTagRegex.ReplaceAllString(displayMessage, " "))

	if researchBrief != "" {
		logger.Info("Extracted research brief from Round 1 plan",
			zap.String("intent", intent),
			zap.Int("brief_len", len(researchBrief)),
		)
	}

	// 2. Initialize Redis review state
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://redis:6379"
	}
	redisOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		logger.Warn("Failed to parse Redis URL for review state", zap.Error(err))
		// Non-fatal: workflow continues, Gateway creates state on first feedback
	} else {
		rdb := redis.NewClient(redisOpts)
		defer rdb.Close()

		// Store stripped message in Redis (no [RESEARCH_BRIEF] or [INTENT:...])
		currentPlan := ""
		if intent == "ready" {
			currentPlan = displayMessage
		}
		state := reviewRedisState{
			WorkflowID:    in.WorkflowID,
			Query:         in.Query,
			Context:       in.Context,
			Status:        "reviewing",
			Round:         1,
			Version:       1,
			OwnerUserID:   in.UserID,
			OwnerTenantID: in.TenantID,
			Rounds: []ReviewRound{
				{Role: "assistant", Message: displayMessage, Timestamp: time.Now().UTC().Format(time.RFC3339)},
			},
			CurrentPlan: currentPlan,
		}

		stateBytes, err := json.Marshal(state)
		if err != nil {
			return ResearchPlanResult{}, fmt.Errorf("failed to marshal review state: %w", err)
		}

		ttl := in.TTL
		if ttl == 0 {
			ttl = 20 * time.Minute // default 15min + 5min buffer
		} else {
			ttl += 5 * time.Minute // add buffer
		}
		key := fmt.Sprintf("review:%s", in.WorkflowID)
		if err := rdb.Set(ctx, key, stateBytes, ttl).Err(); err != nil {
			// Redis failure is critical for HITL - user won't be able to interact
			return ResearchPlanResult{}, fmt.Errorf("failed to initialize HITL review state in Redis: %w", err)
		}
		logger.Info("Initialized HITL review state in Redis",
			zap.String("workflow_id", in.WorkflowID),
			zap.Duration("ttl", ttl),
		)
	}

	return ResearchPlanResult{
		Message: displayMessage,
		Intent:  intent,
		Round:   1,
	}, nil
}
