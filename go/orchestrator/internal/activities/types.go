package activities

import "time"

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

// OutputFormatSpec defines expected output structure for a subtask
type OutputFormatSpec struct {
	Type           string   `json:"type,omitempty"`            // "structured", "narrative", "list"
	RequiredFields []string `json:"required_fields,omitempty"` // Fields that must be present
	OptionalFields []string `json:"optional_fields,omitempty"` // Nice-to-have fields
}

// SourceGuidanceSpec defines source type recommendations for a subtask
type SourceGuidanceSpec struct {
	Required []string `json:"required,omitempty"` // Must use these source types (official, aggregator, news, academic)
	Optional []string `json:"optional,omitempty"` // May use these source types
	Avoid    []string `json:"avoid,omitempty"`    // Should not use (social, forums)
}

// SearchBudgetSpec defines search limits for a subtask
type SearchBudgetSpec struct {
	MaxQueries int `json:"max_queries,omitempty"` // Maximum web_search calls
	MaxFetches int `json:"max_fetches,omitempty"` // Maximum web_fetch calls
}

// BoundariesSpec defines scope boundaries for a subtask
type BoundariesSpec struct {
	InScope    []string `json:"in_scope,omitempty"`     // Topics explicitly within scope
	OutOfScope []string `json:"out_of_scope,omitempty"` // Topics to avoid (prevent overlap)
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
	// Deep Research 2.0: Task Contract fields for explicit boundaries
	OutputFormat   *OutputFormatSpec   `json:"output_format,omitempty"`   // Expected output structure
	SourceGuidance *SourceGuidanceSpec `json:"source_guidance,omitempty"` // Source type recommendations
	SearchBudget   *SearchBudgetSpec   `json:"search_budget,omitempty"`   // Search limits
	Boundaries     *BoundariesSpec     `json:"boundaries,omitempty"`      // Scope boundaries
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
	Role         string `json:"role,omitempty"`
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
	Tool        string      `json:"tool"`
	Success     bool        `json:"success"`
	Output      interface{} `json:"output,omitempty"`
	Error       string      `json:"error,omitempty"`
	DurationMs  int64       `json:"duration_ms,omitempty"`
	InputParams interface{} `json:"tool_input,omitempty"`
}

// SynthesisInput is the input for result synthesis
type SynthesisInput struct {
	Query        string
	AgentResults []AgentExecutionResult
	Context      map[string]interface{} // Optional context for synthesis
	// Parent workflow ID for unified event streaming
	ParentWorkflowID   string      `json:"parent_workflow_id,omitempty"`
	CollectedCitations interface{} `json:"collected_citations,omitempty"` // []metadata.Citation to avoid import cycle
}

// SynthesisResult is the result of synthesis
type SynthesisResult struct {
	FinalResult            string
	TokensUsed             int
	FinishReason           string // Reason model stopped: "stop", "length", "content_filter", etc.
	RequestedMaxTokens     int    // Max completion tokens requested from provider for this synthesis
	CompletionTokens       int    // Output tokens (excludes prompt)
	EffectiveMaxCompletion int    // Actual max completion after provider headroom clamp
	// Added for accurate cost tracking
	InputTokens int     // Prompt tokens (when available or inferred)
	ModelUsed   string  // Model used for synthesis
	Provider    string  // Provider used for synthesis
	CostUsd     float64 // Reported cost from provider metadata when available
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

// VerificationResult contains claim verification analysis (V2 format with three-category classification)
type VerificationResult struct {
	OverallConfidence float64          `json:"overall_confidence"` // 0.0-1.0
	TotalClaims       int              `json:"total_claims"`       // Number of claims extracted
	SupportedClaims   int              `json:"supported_claims"`   // Claims with supporting citations (count)
	Conflicts         []ConflictReport `json:"conflicts"`          // Conflicting information found

	// V2 three-category fields
	UnsupportedClaims          int `json:"unsupported_claims"`           // Claims contradicted by sources (count)
	InsufficientEvidenceClaims int `json:"insufficient_evidence_claims"` // Claims without sufficient evidence (count)

	// V2 claim text lists
	SupportedClaimTexts    []string `json:"supported_claim_texts,omitempty"`
	UnsupportedClaimTexts  []string `json:"unsupported_claim_texts,omitempty"`
	InsufficientClaimTexts []string `json:"insufficient_claim_texts,omitempty"`

	// V2 claim details with verdict and retrieval info
	ClaimDetails []ClaimVerification `json:"claim_details"`

	// V2 quality metrics
	EvidenceCoverage  float64 `json:"evidence_coverage"`   // % of claims with definitive verdict
	AvgRetrievalScore float64 `json:"avg_retrieval_score"` // Average top-1 BM25 relevance
}

// ClaimVerification contains verification for a single claim (V2 format)
type ClaimVerification struct {
	Claim                string  `json:"claim"`                 // The factual claim text
	SupportingCitations  []int   `json:"supporting_citations"`  // Citation numbers supporting this claim
	ConflictingCitations []int   `json:"conflicting_citations"` // Citation numbers conflicting with this claim
	Confidence           float64 `json:"confidence"`            // 0.0-1.0 (weighted by citation credibility)

	// V2 fields
	Verdict         string             `json:"verdict,omitempty"`          // "supported" | "unsupported" | "insufficient_evidence"
	RetrievalScores map[int]float64    `json:"retrieval_scores,omitempty"` // citation_id -> BM25 relevance score
	Reasoning       string             `json:"reasoning,omitempty"`        // Brief explanation for verdict
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

// ── HITL Research Review Types ──

// ResearchPlanInput is the input for the GenerateResearchPlan activity
type ResearchPlanInput struct {
	Query      string                 `json:"query"`
	Context    map[string]interface{} `json:"context"`
	WorkflowID string                `json:"workflow_id"`
	SessionID  string                `json:"session_id"`
	UserID     string                `json:"user_id"`
	TenantID   string                `json:"tenant_id"`
	TTL        time.Duration         `json:"ttl"`
}

// ResearchPlanResult is the result of the GenerateResearchPlan activity
type ResearchPlanResult struct {
	Message string `json:"message"`
	Intent  string `json:"intent"`
	Round   int    `json:"round"`
}

// ResearchReviewResult is the Signal payload sent when user approves the review
type ResearchReviewResult struct {
	Approved      bool          `json:"approved"`
	FinalPlan     string        `json:"final_plan"`
	Conversation  []ReviewRound `json:"conversation"`
	ResearchBrief string        `json:"research_brief,omitempty"`
}

// ReviewRound represents a single round in the review conversation
type ReviewRound struct {
	Role      string `json:"role"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}
