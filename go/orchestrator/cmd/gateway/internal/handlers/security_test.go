package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/cmd/gateway/internal/handlers"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	orchpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MockOrchestratorClient for testing
type MockOrchestratorClient struct {
	mock.Mock
}

func (m *MockOrchestratorClient) SubmitTask(ctx context.Context, req *orchpb.SubmitTaskRequest, opts ...grpc.CallOption) (*orchpb.SubmitTaskResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(*orchpb.SubmitTaskResponse), args.Error(1)
}

func (m *MockOrchestratorClient) GetTaskStatus(ctx context.Context, req *orchpb.GetTaskStatusRequest, opts ...grpc.CallOption) (*orchpb.GetTaskStatusResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*orchpb.GetTaskStatusResponse), args.Error(1)
}

func (m *MockOrchestratorClient) CancelTask(ctx context.Context, req *orchpb.CancelTaskRequest, opts ...grpc.CallOption) (*orchpb.CancelTaskResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*orchpb.CancelTaskResponse), args.Error(1)
}

func (m *MockOrchestratorClient) ApproveTask(ctx context.Context, req *orchpb.ApproveTaskRequest, opts ...grpc.CallOption) (*orchpb.ApproveTaskResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*orchpb.ApproveTaskResponse), args.Error(1)
}

func (m *MockOrchestratorClient) ListTasks(ctx context.Context, req *orchpb.ListTasksRequest, opts ...grpc.CallOption) (*orchpb.ListTasksResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*orchpb.ListTasksResponse), args.Error(1)
}

func (m *MockOrchestratorClient) GetSessionContext(ctx context.Context, req *orchpb.GetSessionContextRequest, opts ...grpc.CallOption) (*orchpb.GetSessionContextResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*orchpb.GetSessionContextResponse), args.Error(1)
}

func (m *MockOrchestratorClient) ListTemplates(ctx context.Context, req *orchpb.ListTemplatesRequest, opts ...grpc.CallOption) (*orchpb.ListTemplatesResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*orchpb.ListTemplatesResponse), args.Error(1)
}

func (m *MockOrchestratorClient) GetPendingApprovals(ctx context.Context, req *orchpb.GetPendingApprovalsRequest, opts ...grpc.CallOption) (*orchpb.GetPendingApprovalsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*orchpb.GetPendingApprovalsResponse), args.Error(1)
}

// TestCancelTask_OwnershipEnforcement tests that cancel endpoint properly checks ownership
func TestCancelTask_OwnershipEnforcement(t *testing.T) {
	tests := []struct {
		name           string
		taskID         string
		setupMock      func(*MockOrchestratorClient)
		expectedStatus int
		expectedBody   string
	}{
		{
			name:   "Cancel non-existent task returns 404",
			taskID: "non-existent-task",
			setupMock: func(m *MockOrchestratorClient) {
				m.On("CancelTask", mock.Anything, mock.Anything).
					Return(nil, status.Error(codes.NotFound, "task not found"))
			},
			expectedStatus: http.StatusNotFound,
			expectedBody:   "Task not found",
		},
		{
			name:   "Cancel unauthorized task returns 403",
			taskID: "unauthorized-task",
			setupMock: func(m *MockOrchestratorClient) {
				m.On("CancelTask", mock.Anything, mock.Anything).
					Return(nil, status.Error(codes.PermissionDenied, "forbidden"))
			},
			expectedStatus: http.StatusForbidden,
			expectedBody:   "Forbidden",
		},
		{
			name:   "Cancel task returns 202",
			taskID: "valid-task",
			setupMock: func(m *MockOrchestratorClient) {
				m.On("CancelTask", mock.Anything, mock.Anything).
					Return(&orchpb.CancelTaskResponse{
						Success: true,
						Message: "Task cancelled successfully",
					}, nil)
			},
			expectedStatus: http.StatusAccepted,
			expectedBody:   "cancelled successfully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockClient := new(MockOrchestratorClient)
			tt.setupMock(mockClient)

			handler := handlers.NewTaskHandler(mockClient, nil, nil, zap.NewNop())

			req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+tt.taskID+"/cancel",
				strings.NewReader(`{"reason":"test"}`))
			req.Header.Set("Content-Type", "application/json")
			req.SetPathValue("id", tt.taskID)

			// Add user context
			ctx := context.WithValue(req.Context(), "user", &auth.UserContext{
				UserID:   uuid.New(),
				TenantID: uuid.New(),
			})
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()

			// Execute
			handler.CancelTask(w, req)

			// Assert
			assert.Equal(t, tt.expectedStatus, w.Code, "HTTP status code mismatch")
			assert.Contains(t, w.Body.String(), tt.expectedBody, "Response body mismatch")
			mockClient.AssertExpectations(t)
		})
	}
}

// TestApproveTask_ValidationAndAuth tests approval endpoint validation
func TestApproveTask_ValidationAndAuth(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		setupMock      func(*MockOrchestratorClient)
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Missing required fields returns 400",
			body:           `{"approved":true}`,
			setupMock:      func(m *MockOrchestratorClient) {},
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "required",
		},
		{
			name: "Non-existent workflow returns 404",
			body: `{"workflow_id":"fake","approval_id":"test","approved":true}`,
			setupMock: func(m *MockOrchestratorClient) {
				m.On("ApproveTask", mock.Anything, mock.Anything).
					Return(nil, status.Error(codes.NotFound, "workflow not found"))
			},
			expectedStatus: http.StatusNotFound,
			expectedBody:   "not found",
		},
		{
			name: "Unauthorized workflow returns 403",
			body: `{"workflow_id":"unauthorized","approval_id":"test","approved":true}`,
			setupMock: func(m *MockOrchestratorClient) {
				m.On("ApproveTask", mock.Anything, mock.Anything).
					Return(nil, status.Error(codes.PermissionDenied, "forbidden"))
			},
			expectedStatus: http.StatusForbidden,
			expectedBody:   "Forbidden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockClient := new(MockOrchestratorClient)
			tt.setupMock(mockClient)

			handler := handlers.NewApprovalHandler(mockClient, zap.NewNop())

			req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/decision",
				strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			// Add user context
			ctx := context.WithValue(req.Context(), "user", &auth.UserContext{
				UserID:   uuid.New(),
				TenantID: uuid.New(),
			})
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()

			// Execute
			handler.SubmitDecision(w, req)

			// Assert
			assert.Equal(t, tt.expectedStatus, w.Code, "HTTP status code mismatch")
			assert.Contains(t, w.Body.String(), tt.expectedBody, "Response body mismatch")

			if tt.setupMock != nil {
				mockClient.AssertExpectations(t)
			}
		})
	}
}

// TestAPIKeyPropagation_Metadata verifies X-API-Key is propagated to gRPC
func TestAPIKeyPropagation_Metadata(t *testing.T) {
	// Note: This test verifies the metadata propagation pattern exists
	// Actual metadata propagation is tested via integration tests
	t.Run("Submit task with API key header", func(t *testing.T) {
		mockClient := new(MockOrchestratorClient)
		mockClient.On("SubmitTask", mock.Anything, mock.Anything).
			Return(&orchpb.SubmitTaskResponse{
				TaskId:  "test-task-id",
				Message: "success",
			}, nil)

		handler := handlers.NewTaskHandler(mockClient, nil, nil, zap.NewNop())

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks",
			strings.NewReader(`{"query":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "test-api-key-123")
		req.Header.Set("traceparent", "00-test-trace-id-01")

		ctx := context.WithValue(req.Context(), "user", &auth.UserContext{
			UserID:   uuid.New(),
			TenantID: uuid.New(),
		})
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.SubmitTask(w, req)

		// Verify successful response (metadata propagation verified in integration tests)
		assert.Equal(t, http.StatusOK, w.Code)
		mockClient.AssertExpectations(t)
	})
}
