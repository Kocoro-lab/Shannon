package control

import "time"

// Signal names for workflow control
const (
	SignalPause       = "pause_v1"
	SignalResume      = "resume_v1"
	SignalCancel      = "cancel_v1"
	QueryControlState = "control_state_v1"
)

// PauseRequest is sent when pausing a workflow
type PauseRequest struct {
	Reason      string `json:"reason"`
	RequestedBy string `json:"requested_by"`
}

// ResumeRequest is sent when resuming a paused workflow
type ResumeRequest struct {
	Reason      string `json:"reason"`
	RequestedBy string `json:"requested_by"`
}

// CancelRequest is sent when gracefully cancelling a workflow
type CancelRequest struct {
	Reason      string `json:"reason"`
	RequestedBy string `json:"requested_by"`
}

// WorkflowControlState tracks pause/cancel state for query handlers
type WorkflowControlState struct {
	IsPaused     bool      `json:"is_paused"`
	IsCancelled  bool      `json:"is_cancelled"`
	PausedAt     time.Time `json:"paused_at,omitempty"`
	PauseReason  string    `json:"pause_reason,omitempty"`
	PausedBy     string    `json:"paused_by,omitempty"`
	CancelReason string    `json:"cancel_reason,omitempty"`
	CancelledBy  string    `json:"cancelled_by,omitempty"`
}
