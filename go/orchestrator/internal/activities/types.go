package activities

// ComplexityAnalysisInput is the input for complexity analysis
type ComplexityAnalysisInput struct {
	Query   string
	Context map[string]interface{}
}

// ComplexityAnalysisResult is the result of complexity analysis
type ComplexityAnalysisResult struct {
	ComplexityScore float64
	Mode            string
	Subtasks        []Subtask
}

// Subtask represents a decomposed subtask
type Subtask struct {
    ID              string
    Description     string
    Dependencies    []string
    EstimatedTokens int
	// Structured subtask classification to avoid brittle string matching
	TaskType string `json:"task_type,omitempty"`
    // Plan IO (optional, plan_io_v1): topics produced/consumed by this subtask
    Produces []string
    Consumes []string
    // Optional grouping for research-area-driven decomposition
    ParentArea string `json:"parent_area,omitempty"`
    // LLM-native tool selection
    SuggestedTools []string               `json:"suggested_tools"`
    ToolParameters map[string]interface{} `json:"tool_parameters"`
    // Persona assignment for specialized agent behavior
    SuggestedPersona string `json:"suggested_persona"`
}

// AgentExecutionInput is the input for agent execution
type AgentExecutionInput struct {
	Query     string
	AgentID   string
	Context   map[string]interface{}
	Mode      string
	SessionID string   // Session identifier
	History   []string // Conversation history
	// LLM-native tool selection
	SuggestedTools []string               `json:"suggested_tools"`
	ToolParameters map[string]interface{} `json:"tool_parameters"`
	// Persona for specialized agent behavior
	PersonaID string `json:"persona_id"`
	// Parent workflow ID for unified event streaming
	ParentWorkflowID string `json:"parent_workflow_id,omitempty"`
}

// AgentExecutionResult is the result of agent execution
type AgentExecutionResult struct {
	AgentID      string
	Response     string
	TokensUsed   int
	ModelUsed    string
	Provider     string
	InputTokens  int
	OutputTokens int
	DurationMs   int64
	Success      bool
	Error        string
	// Tools used and their outputs (when applicable)
	ToolsUsed      []string        `json:"tools_used,omitempty"`
	ToolExecutions []ToolExecution `json:"tool_executions,omitempty"`
}

// ToolExecution summarizes a single tool invocation result returned by Agent-Core
type ToolExecution struct {
	Tool    string      `json:"tool"`
	Success bool        `json:"success"`
	Output  interface{} `json:"output,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// SynthesisInput is the input for result synthesis
type SynthesisInput struct {
	Query        string
	AgentResults []AgentExecutionResult
	Context      map[string]interface{} // Optional context for synthesis
	// Parent workflow ID for unified event streaming
	ParentWorkflowID string `json:"parent_workflow_id,omitempty"`
}

// SynthesisResult is the result of synthesis
type SynthesisResult struct {
    FinalResult  string
    TokensUsed   int
    FinishReason string // Reason model stopped: "stop", "length", "content_filter", etc.
    RequestedMaxTokens int // Max completion tokens requested from provider for this synthesis
    CompletionTokens int // Output tokens (excludes prompt)
    EffectiveMaxCompletion int // Actual max completion after provider headroom clamp
}

// EvaluateResultInput carries data for reflection/quality checks
type EvaluateResultInput struct {
	Query    string
	Response string
	Criteria []string
}

// EvaluateResultOutput returns a simple quality score and feedback
type EvaluateResultOutput struct {
	Score    float64
	Feedback string
}

// VerifyClaimsInput is the input for claim verification
type VerifyClaimsInput struct {
	Answer    string        // Synthesis result to verify
	Citations []interface{} // Available citations (from metadata.CollectCitations)
}

// VerificationResult contains claim verification analysis
type VerificationResult struct {
	OverallConfidence float64             `json:"overall_confidence"` // 0.0-1.0
	TotalClaims       int                 `json:"total_claims"`       // Number of claims extracted
	SupportedClaims   int                 `json:"supported_claims"`   // Claims with supporting citations
	UnsupportedClaims []string            `json:"unsupported_claims"` // List of unsupported claim texts
	Conflicts         []ConflictReport    `json:"conflicts"`          // Conflicting information found
	ClaimDetails      []ClaimVerification `json:"claim_details"`      // Per-claim analysis
}

// ClaimVerification contains verification for a single claim
type ClaimVerification struct {
	Claim                string   `json:"claim"`                  // The factual claim text
	SupportingCitations  []int    `json:"supporting_citations"`   // Citation numbers supporting this claim
	ConflictingCitations []int    `json:"conflicting_citations"`  // Citation numbers conflicting with this claim
	Confidence           float64  `json:"confidence"`             // 0.0-1.0 (weighted by citation credibility)
}

// ConflictReport describes conflicting information
type ConflictReport struct {
	Claim       string `json:"claim"`        // The claim in question
	Source1     int    `json:"source1"`      // Citation number 1
	Source1Text string `json:"source1_text"` // What source 1 says
	Source2     int    `json:"source2"`      // Citation number 2
	Source2Text string `json:"source2_text"` // What source 2 says
}

// SessionUpdateInput is the input for updating session
type SessionUpdateInput struct {
	SessionID  string
	Result     string
	TokensUsed int
	AgentsUsed int
	CostUSD    float64
	// Optional: model used for this update when single-model
	ModelUsed string
	// Optional: per-agent usage for accurate cost across multiple models
	AgentUsage []AgentUsage `json:"agent_usage,omitempty"`
}

// SessionUpdateResult is the result of session update
type SessionUpdateResult struct {
	Success bool
	Error   string
}

// AgentUsage captures model-specific token usage for cost calculation
type AgentUsage struct {
	Model        string `json:"model"`
	Tokens       int    `json:"tokens"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
}
