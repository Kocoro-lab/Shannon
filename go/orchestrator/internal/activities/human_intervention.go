package activities

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
)

// HumanApprovalInput represents a request for human approval
type HumanApprovalInput struct {
	SessionID      string                 `json:"session_id"`
	WorkflowID     string                 `json:"workflow_id"`
	RunID          string                 `json:"run_id"`
	Query          string                 `json:"query"`
	Context        map[string]interface{} `json:"context"`
	ProposedAction string                 `json:"proposed_action"`
	Reason         string                 `json:"reason"`
	Metadata       map[string]interface{} `json:"metadata"`
}

// HumanApprovalResult represents the human's response
type HumanApprovalResult struct {
	ApprovalID     string    `json:"approval_id"`
	Approved       bool      `json:"approved"`
	Feedback       string    `json:"feedback"`
	ModifiedAction string    `json:"modified_action"`
	ApprovedBy     string    `json:"approved_by"`
	Timestamp      time.Time `json:"timestamp"`
}

// HumanInterventionActivities handles human-in-the-loop approvals
type HumanInterventionActivities struct {
	// In production, these would be real database and notification services
	// For now, we'll use in-memory storage
	approvals map[string]*HumanApprovalResult
	mu        sync.RWMutex // Protect concurrent access to approvals map
}

// NewHumanInterventionActivities creates a new human intervention activities handler
func NewHumanInterventionActivities() *HumanInterventionActivities {
	return &HumanInterventionActivities{
		approvals: make(map[string]*HumanApprovalResult),
	}
}

// RequestApproval creates an approval request and returns an approval ID
// The workflow will wait for a signal with the actual approval
func (h *HumanInterventionActivities) RequestApproval(ctx context.Context, input HumanApprovalInput) (HumanApprovalResult, error) {
	// Generate approval ID
	approvalID := uuid.New().String()

	// Log the approval request
	fmt.Printf("Human approval requested:\n")
	fmt.Printf("  Approval ID: %s\n", approvalID)
	fmt.Printf("  Query: %s\n", input.Query)
	fmt.Printf("  Reason: %s\n", input.Reason)
	fmt.Printf("  Proposed Action: %s\n", input.ProposedAction)

	// In production, this would:
	// 1. Store the request in a database
	// 2. Send notifications (webhook, email, etc.)
	// 3. Return the approval ID

	// Return approval ID for the workflow to track
	return HumanApprovalResult{
		ApprovalID: approvalID,
		Timestamp:  time.Now(),
	}, nil
}

// ProcessApprovalResponse processes the human's response to an approval request
func (h *HumanInterventionActivities) ProcessApprovalResponse(ctx context.Context, response HumanApprovalResult) error {
	// Store the response with mutex protection
	h.mu.Lock()
	h.approvals[response.ApprovalID] = &response
	h.mu.Unlock()

	fmt.Printf("Approval response processed:\n")
	fmt.Printf("  Approval ID: %s\n", response.ApprovalID)
	fmt.Printf("  Approved: %v\n", response.Approved)
	fmt.Printf("  Feedback: %s\n", response.Feedback)

	return nil
}

// GetApprovalStatus retrieves the current status of an approval request
func (h *HumanInterventionActivities) GetApprovalStatus(ctx context.Context, approvalID string) (HumanApprovalResult, error) {
	h.mu.RLock()
	result, exists := h.approvals[approvalID]
	h.mu.RUnlock()

	if exists {
		return *result, nil
	}

	// Return empty result if not found (still pending)
	return HumanApprovalResult{
		ApprovalID: approvalID,
	}, nil
}

// ApprovalPolicy defines when human intervention is required
type ApprovalPolicy struct {
	ComplexityThreshold float64  `json:"complexity_threshold"`
	TokenBudgetExceeded bool     `json:"token_budget_exceeded"`
	RequireForTools     []string `json:"require_for_tools"`
}

// EvaluateApprovalPolicy determines if human approval is required
func EvaluateApprovalPolicy(policy ApprovalPolicy, context map[string]interface{}) (bool, string) {
	// Check complexity threshold
	if complexity, ok := context["complexity_score"].(float64); ok {
		if policy.ComplexityThreshold > 0 && complexity >= policy.ComplexityThreshold {
			return true, fmt.Sprintf("Complexity score %.2f exceeds threshold %.2f", complexity, policy.ComplexityThreshold)
		}
	}

	// Check token budget (handle both int and float64 from JSON)
	if policy.TokenBudgetExceeded {
		var tokensUsed, tokenBudget int

		// Handle tokens_used as either int or float64
		switch v := context["tokens_used"].(type) {
		case int:
			tokensUsed = v
		case float64:
			tokensUsed = int(math.Round(v))
		}

		// Handle token_budget as either int or float64
		switch v := context["token_budget"].(type) {
		case int:
			tokenBudget = v
		case float64:
			tokenBudget = int(math.Round(v))
		}

		if tokensUsed > 0 && tokenBudget > 0 && tokensUsed >= tokenBudget {
			return true, fmt.Sprintf("Token budget exceeded: %d/%d", tokensUsed, tokenBudget)
		}
	}

	// Check tools
	if len(policy.RequireForTools) > 0 {
		// Handle tools_to_use as []string or []interface{} (from JSON)
		switch tools := context["tools_to_use"].(type) {
		case []string:
			for _, tool := range tools {
				for _, requiredTool := range policy.RequireForTools {
					if tool == requiredTool {
						return true, fmt.Sprintf("Tool '%s' requires approval", tool)
					}
				}
			}
		case []interface{}:
			for _, toolInterface := range tools {
				if tool, ok := toolInterface.(string); ok {
					for _, requiredTool := range policy.RequireForTools {
						if tool == requiredTool {
							return true, fmt.Sprintf("Tool '%s' requires approval", tool)
						}
					}
				}
			}
		}
	}

	return false, ""
}
