package activities

import (
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/session"
	"go.uber.org/zap"
)

// Activities struct holds dependencies for activities
type Activities struct {
	sessionManager *session.Manager
	logger         *zap.Logger
}

// NewActivities creates a new activities instance with dependencies
func NewActivities(sessionManager *session.Manager, logger *zap.Logger) *Activities {
	return &Activities{
		sessionManager: sessionManager,
		logger:         logger,
	}
}
