package server

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/common"
	pb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/session"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/mocks"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestExecutionModeRouting verifies that different execution modes route to correct workflows
func TestExecutionModeRouting(t *testing.T) {
	tests := []struct {
		name             string
		mode             common.ExecutionMode
		expectedWorkflow string
		expectedModeStr  string
	}{
		{
			name:             "Simple mode routes to OrchestratorWorkflow",
			mode:             common.ExecutionMode_EXECUTION_MODE_SIMPLE,
			expectedWorkflow: "OrchestratorWorkflow",
			expectedModeStr:  "standard", // Simple is converted to standard for workflow analysis
		},
		{
			name:             "Standard mode routes to OrchestratorWorkflow",
			mode:             common.ExecutionMode_EXECUTION_MODE_STANDARD,
			expectedWorkflow: "OrchestratorWorkflow",
			expectedModeStr:  "standard",
		},
		{
			name:             "Complex mode routes to OrchestratorWorkflow",
			mode:             common.ExecutionMode_EXECUTION_MODE_COMPLEX,
			expectedWorkflow: "OrchestratorWorkflow",
			expectedModeStr:  "complex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock Temporal client
			mockClient := &mocks.Client{}
			mockWorkflowRun := &mocks.WorkflowRun{}

			// Setup expectations
			// GetID is called before we know the actual workflow ID, so return anything
			mockWorkflowRun.On("GetID").Return(func() string {
				// Will be task-test-user-{timestamp}
				return "task-test-user-12345"
			}())
			mockWorkflowRun.On("GetRunID").Return("test-run-id")

			// Capture the workflow function used
			var capturedWorkflow interface{}
			var capturedInput workflows.TaskInput
			var capturedMemo map[string]interface{}

			mockClient.On("ExecuteWorkflow",
				mock.Anything, // context
				mock.MatchedBy(func(opts client.StartWorkflowOptions) bool {
					capturedMemo = opts.Memo
					return true
				}),
				mock.Anything, // workflow function
				mock.AnythingOfType("workflows.TaskInput"),
			).Run(func(args mock.Arguments) {
				capturedWorkflow = args.Get(2)
				capturedInput = args.Get(3).(workflows.TaskInput)
			}).Return(mockWorkflowRun, nil)

			// Create session manager
			sessionMgr, err := session.NewManager("localhost:6379", zap.NewNop())
			if err != nil {
				t.Fatalf("Failed to create session manager: %v", err)
			}

			// Create service
			service := &OrchestratorService{
				temporalClient: mockClient,
				sessionManager: sessionMgr,
				logger:         zap.NewNop(),
			}

			// Create request with manual decomposition
			req := &pb.SubmitTaskRequest{
				Metadata: &common.TaskMetadata{
					UserId: "test-user",
				},
				Query: "test query",
				ManualDecomposition: &pb.TaskDecomposition{
					Mode:            tt.mode,
					ComplexityScore: 0.5,
				},
				Context: &structpb.Struct{
					Fields: make(map[string]*structpb.Value),
				},
			}

			// Execute
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := service.SubmitTask(ctx, req)

			// Verify
			assert.NoError(t, err)
			assert.NotNil(t, resp)
			assert.Contains(t, resp.WorkflowId, "task-test-user-") // Workflow ID is generated
			assert.Equal(t, tt.mode, resp.Decomposition.Mode)

			// Verify correct workflow was called (check function name)
			workflowName := ""
			if capturedWorkflow != nil {
				// Get function name using reflection
				funcName := runtime.FuncForPC(reflect.ValueOf(capturedWorkflow).Pointer()).Name()
				if strings.Contains(funcName, "SimpleTaskWorkflow") {
					workflowName = "SimpleTaskWorkflow"
				} else if strings.Contains(funcName, "AgentDAGWorkflow") {
					workflowName = "AgentDAGWorkflow"
				} else if strings.Contains(funcName, "OrchestratorWorkflow") {
					workflowName = "OrchestratorWorkflow"
				}
			}
			assert.Equal(t, tt.expectedWorkflow, workflowName)

			// Verify input mode
			assert.Equal(t, tt.expectedModeStr, capturedInput.Mode)

			// Verify memo contains mode
			assert.Equal(t, tt.expectedModeStr, capturedMemo["mode"])

			mockClient.AssertExpectations(t)
			mockWorkflowRun.AssertExpectations(t)
		})
	}
}

// TestDefaultExecutionMode verifies that when no mode is specified, it defaults to STANDARD
func TestDefaultExecutionMode(t *testing.T) {
	// Create mock Temporal client
	mockClient := &mocks.Client{}
	mockWorkflowRun := &mocks.WorkflowRun{}

	// Setup expectations
	mockWorkflowRun.On("GetID").Return("test-workflow-id")
	mockWorkflowRun.On("GetRunID").Return("test-run-id")

	var capturedInput workflows.TaskInput
	var capturedWorkflow interface{}
	mockClient.On("ExecuteWorkflow",
		mock.Anything,
		mock.MatchedBy(func(opts client.StartWorkflowOptions) bool {
			return true // Accept any workflow options
		}),
		mock.Anything, // Workflow function
		mock.AnythingOfType("workflows.TaskInput"),
	).Run(func(args mock.Arguments) {
		capturedWorkflow = args.Get(2)
		capturedInput = args.Get(3).(workflows.TaskInput)
	}).Return(mockWorkflowRun, nil)

	// Create session manager
	sessionMgr, err := session.NewManager("localhost:6379", zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to create session manager: %v", err)
	}

	// Create service
	service := &OrchestratorService{
		temporalClient: mockClient,
		sessionManager: sessionMgr,
		logger:         zap.NewNop(),
	}

	// Create request WITHOUT manual decomposition
	req := &pb.SubmitTaskRequest{
		Metadata: &common.TaskMetadata{
			UserId: "test-user",
		},
		Query: "test query",
		Context: &structpb.Struct{
			Fields: make(map[string]*structpb.Value),
		},
	}

	// Execute
	ctx := context.Background()
	resp, err := service.SubmitTask(ctx, req)

	// Verify
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, common.ExecutionMode_EXECUTION_MODE_STANDARD, resp.Decomposition.Mode)
	assert.Equal(t, "standard", capturedInput.Mode)

	// Verify OrchestratorWorkflow was called (default for standard mode)
	workflowName := ""
	if capturedWorkflow != nil {
		funcName := runtime.FuncForPC(reflect.ValueOf(capturedWorkflow).Pointer()).Name()
		if strings.Contains(funcName, "OrchestratorWorkflow") {
			workflowName = "OrchestratorWorkflow"
		}
	}
	assert.Equal(t, "OrchestratorWorkflow", workflowName)

	mockClient.AssertExpectations(t)
	mockWorkflowRun.AssertExpectations(t)
}

// TestPriorityQueueRouting verifies that priority labels correctly route to appropriate task queues
func TestPriorityQueueRouting(t *testing.T) {
	tests := []struct {
		name          string
		priority      string
		expectedQueue string
	}{
		{
			name:          "Critical priority routes to critical queue",
			priority:      "critical",
			expectedQueue: "shannon-tasks-critical",
		},
		{
			name:          "High priority routes to high queue",
			priority:      "high",
			expectedQueue: "shannon-tasks-high",
		},
		{
			name:          "Normal priority routes to default queue",
			priority:      "normal",
			expectedQueue: "shannon-tasks",
		},
		{
			name:          "Low priority routes to low queue",
			priority:      "low",
			expectedQueue: "shannon-tasks-low",
		},
		{
			name:          "Invalid priority uses default queue",
			priority:      "invalid",
			expectedQueue: "shannon-tasks",
		},
		{
			name:          "Empty priority uses default queue",
			priority:      "",
			expectedQueue: "shannon-tasks",
		},
		{
			name:          "Mixed case priority is handled correctly",
			priority:      "CRITICAL",
			expectedQueue: "shannon-tasks-critical",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock Temporal client
			mockClient := &mocks.Client{}
			mockWorkflowRun := &mocks.WorkflowRun{}

			// Setup expectations
			mockWorkflowRun.On("GetID").Return("test-workflow-id")
			mockWorkflowRun.On("GetRunID").Return("test-run-id")

			var capturedOptions client.StartWorkflowOptions
			mockClient.On("ExecuteWorkflow",
				mock.Anything, // context
				mock.MatchedBy(func(opts client.StartWorkflowOptions) bool {
					capturedOptions = opts
					return true
				}),
				mock.Anything, // workflow function
				mock.AnythingOfType("workflows.TaskInput"),
			).Return(mockWorkflowRun, nil)

			// Create session manager
			sessionMgr, err := session.NewManager("localhost:6379", zap.NewNop())
			if err != nil {
				t.Fatalf("Failed to create session manager: %v", err)
			}

			// Create service
			service := &OrchestratorService{
				temporalClient: mockClient,
				sessionManager: sessionMgr,
				logger:         zap.NewNop(),
			}

			// Create request with priority label
			var req *pb.SubmitTaskRequest
			if tt.priority != "" {
				req = &pb.SubmitTaskRequest{
					Metadata: &common.TaskMetadata{
						UserId: "test-user",
						Labels: map[string]string{
							"priority": tt.priority,
						},
					},
					Query: "test query",
					Context: &structpb.Struct{
						Fields: make(map[string]*structpb.Value),
					},
				}
			} else {
				// Test case for no priority label
				req = &pb.SubmitTaskRequest{
					Metadata: &common.TaskMetadata{
						UserId: "test-user",
					},
					Query: "test query",
					Context: &structpb.Struct{
						Fields: make(map[string]*structpb.Value),
					},
				}
			}

			// Execute
			ctx := context.Background()
			resp, err := service.SubmitTask(ctx, req)

			// Verify
			assert.NoError(t, err)
			assert.NotNil(t, resp)

			// Verify the correct task queue was used
			assert.Equal(t, tt.expectedQueue, capturedOptions.TaskQueue,
				"Priority '%s' should route to queue '%s'", tt.priority, tt.expectedQueue)

			mockClient.AssertExpectations(t)
			mockWorkflowRun.AssertExpectations(t)
		})
	}
}
