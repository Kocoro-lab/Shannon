package workflows

import (
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/control"
)

// Re-export signal names for backward compatibility
const (
	SignalPause       = control.SignalPause
	SignalResume      = control.SignalResume
	SignalCancel      = control.SignalCancel
	QueryControlState = control.QueryControlState
)

// Re-export types for backward compatibility
type (
	PauseRequest         = control.PauseRequest
	ResumeRequest        = control.ResumeRequest
	CancelRequest        = control.CancelRequest
	WorkflowControlState = control.WorkflowControlState
)
