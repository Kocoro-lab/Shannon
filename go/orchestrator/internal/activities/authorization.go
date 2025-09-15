package activities

import (
    "context"
    "time"
    policy "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/policy"
)

// TeamActionInput captures a dynamic team action request for policy evaluation
type TeamActionInput struct {
    Action     string                 `json:"action"` // "recruit" | "retire"
    SessionID  string                 `json:"session_id"`
    UserID     string                 `json:"user_id"`
    AgentID    string                 `json:"agent_id"`
    Role       string                 `json:"role"`
    Metadata   map[string]interface{} `json:"metadata"`
}

// TeamActionDecision is a minimal decision wrapper
type TeamActionDecision struct {
    Allow  bool   `json:"allow"`
    Reason string `json:"reason"`
}

// AuthorizeTeamAction evaluates dynamic team actions via the policy engine
func AuthorizeTeamAction(ctx context.Context, in TeamActionInput) (TeamActionDecision, error) {
    if policyEngine == nil || !policyEngine.IsEnabled() {
        return TeamActionDecision{Allow: true, Reason: "policy engine disabled"}, nil
    }
    // Build policy input using existing structure
    pi := policyInputForTeam(in)
    start := time.Now()
    dec, err := policyEngine.Evaluate(ctx, pi)
    _ = start // metrics can be added in future; keep function minimal
    if err != nil {
        return TeamActionDecision{Allow: false, Reason: err.Error()}, nil
    }
    return TeamActionDecision{Allow: dec.Allow, Reason: dec.Reason}, nil
}

// policyInputForTeam adapts team action to the existing PolicyInput
func policyInputForTeam(in TeamActionInput) *policy.PolicyInput {
    ctx := map[string]interface{}{}
    for k, v := range in.Metadata { ctx[k] = v }
    ctx["action"] = in.Action
    if in.Role != "" { ctx["role"] = in.Role }
    return &policy.PolicyInput{
        SessionID:   in.SessionID,
        UserID:      in.UserID,
        AgentID:     in.AgentID,
        Query:       "",   // not applicable
        Mode:        "",    // not applicable
        Context:     ctx,
        Environment: "dev",
        Timestamp:   time.Now(),
    }
}
