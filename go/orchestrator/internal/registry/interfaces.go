package registry

import (
	"go.temporal.io/sdk/worker"
)

// WorkflowRegistrar defines the interface for registering workflows
type WorkflowRegistrar interface {
	RegisterWorkflows(w worker.Worker) error
}

// ActivityRegistrar defines the interface for registering activities  
type ActivityRegistrar interface {
	RegisterActivities(w worker.Worker) error
}

// Registry combines both workflow and activity registration
type Registry interface {
	WorkflowRegistrar
	ActivityRegistrar
}

// RegistryConfig holds configuration for the registry
type RegistryConfig struct {
	// EnableBudgetedWorkflows controls whether budget-aware workflows are registered
	EnableBudgetedWorkflows bool
	// EnableStreamingWorkflows controls whether streaming workflows are registered  
	EnableStreamingWorkflows bool
	// EnableApprovalWorkflows controls whether human approval workflows are registered
	EnableApprovalWorkflows bool
}