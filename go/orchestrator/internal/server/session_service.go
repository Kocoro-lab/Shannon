package server

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	auth "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	common "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/common"
	pb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/session"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/session"
)

// SessionServiceImpl implements the Session gRPC service
type SessionServiceImpl struct {
	pb.UnimplementedSessionServiceServer
	manager *session.Manager
	logger  *zap.Logger
}

// NewSessionService creates a new session service
func NewSessionService(manager *session.Manager, logger *zap.Logger) *SessionServiceImpl {
	return &SessionServiceImpl{
		manager: manager,
		logger:  logger,
	}
}

// CreateSession creates a new session
func (s *SessionServiceImpl) CreateSession(ctx context.Context, req *pb.CreateSessionRequest) (*pb.CreateSessionResponse, error) {
	s.logger.Info("Creating new session",
		zap.String("user_id", req.UserId),
		zap.Int32("max_history", req.MaxHistory),
		zap.Int32("ttl_seconds", req.TtlSeconds),
	)

	// Convert protobuf struct to map
	initialContext := make(map[string]interface{})
	if req.InitialContext != nil {
		initialContext = req.InitialContext.AsMap()
	}

	// Set max history if provided
	if req.MaxHistory > 0 {
		initialContext["max_history"] = req.MaxHistory
	}

	// Get tenant from auth context if available
	var tenantID string
	if uc, err := auth.GetUserContext(ctx); err == nil && uc != nil {
		tenantID = uc.TenantID.String()
	}

	// Create session (tenant-aware)
	sess, err := s.manager.CreateSession(ctx, req.UserId, tenantID, initialContext)
	if err != nil {
		s.logger.Error("Failed to create session", zap.Error(err))
		return &pb.CreateSessionResponse{
			Status:  common.StatusCode_STATUS_CODE_ERROR,
			Message: err.Error(),
		}, nil
	}

	// Calculate expiration time
	expiresAt := time.Now().Add(720 * time.Hour) // Default 30 days
	if req.TtlSeconds > 0 {
		expiresAt = time.Now().Add(time.Duration(req.TtlSeconds) * time.Second)
	}

	return &pb.CreateSessionResponse{
		SessionId: sess.ID,
		CreatedAt: timestamppb.New(sess.CreatedAt),
		ExpiresAt: timestamppb.New(expiresAt),
		Status:    common.StatusCode_STATUS_CODE_OK,
		Message:   "Session created successfully",
	}, nil
}

// GetSession retrieves session details
func (s *SessionServiceImpl) GetSession(ctx context.Context, req *pb.GetSessionRequest) (*pb.GetSessionResponse, error) {
	s.logger.Info("Getting session",
		zap.String("session_id", req.SessionId),
		zap.Bool("include_history", req.IncludeHistory),
	)

	sess, err := s.manager.GetSession(ctx, req.SessionId)
	if err != nil {
		if err == session.ErrSessionNotFound {
			return &pb.GetSessionResponse{
				Status:  common.StatusCode_STATUS_CODE_ERROR,
				Message: "Session not found",
			}, nil
		}
		s.logger.Error("Failed to get session", zap.Error(err))
		return &pb.GetSessionResponse{
			Status:  common.StatusCode_STATUS_CODE_ERROR,
			Message: err.Error(),
		}, nil
	}

	// Convert session to protobuf
	pbSession := s.sessionToProto(sess, req.IncludeHistory)

	return &pb.GetSessionResponse{
		Session: pbSession,
		Status:  common.StatusCode_STATUS_CODE_OK,
		Message: "Session retrieved successfully",
	}, nil
}

// UpdateSession updates session context
func (s *SessionServiceImpl) UpdateSession(ctx context.Context, req *pb.UpdateSessionRequest) (*pb.UpdateSessionResponse, error) {
	s.logger.Info("Updating session",
		zap.String("session_id", req.SessionId),
		zap.Int32("extend_ttl_seconds", req.ExtendTtlSeconds),
	)

	// Get existing session
	sess, err := s.manager.GetSession(ctx, req.SessionId)
	if err != nil {
		if err == session.ErrSessionNotFound {
			return &pb.UpdateSessionResponse{
				Success: false,
				Status:  common.StatusCode_STATUS_CODE_ERROR,
				Message: "Session not found",
			}, nil
		}
		return &pb.UpdateSessionResponse{
			Success: false,
			Status:  common.StatusCode_STATUS_CODE_ERROR,
			Message: err.Error(),
		}, nil
	}

	// Update context if provided
	if req.ContextUpdates != nil {
		updates := req.ContextUpdates.AsMap()
		if sess.Context == nil {
			sess.Context = make(map[string]interface{})
		}
		for k, v := range updates {
			sess.Context[k] = v
		}
	}

	// Extend TTL if requested
	var newExpires time.Time
	if req.ExtendTtlSeconds > 0 {
		ttlDuration := time.Duration(req.ExtendTtlSeconds) * time.Second
		err = s.manager.ExtendSession(ctx, req.SessionId, ttlDuration)
		if err != nil {
			s.logger.Error("Failed to extend session TTL", zap.Error(err))
			return &pb.UpdateSessionResponse{
				Success: false,
				Status:  common.StatusCode_STATUS_CODE_ERROR,
				Message: fmt.Sprintf("Failed to extend TTL: %v", err),
			}, nil
		}
		newExpires = time.Now().Add(ttlDuration)
		sess.ExpiresAt = newExpires
	}

	// Update session
	err = s.manager.UpdateSession(ctx, sess)
	if err != nil {
		s.logger.Error("Failed to update session", zap.Error(err))
		return &pb.UpdateSessionResponse{
			Success: false,
			Status:  common.StatusCode_STATUS_CODE_ERROR,
			Message: err.Error(),
		}, nil
	}

	return &pb.UpdateSessionResponse{
		Success:      true,
		NewExpiresAt: timestamppb.New(newExpires),
		Status:       common.StatusCode_STATUS_CODE_OK,
		Message:      "Session updated successfully",
	}, nil
}

// DeleteSession deletes a session
func (s *SessionServiceImpl) DeleteSession(ctx context.Context, req *pb.DeleteSessionRequest) (*pb.DeleteSessionResponse, error) {
	s.logger.Info("Deleting session", zap.String("session_id", req.SessionId))

	err := s.manager.DeleteSession(ctx, req.SessionId)
	if err != nil {
		if err == session.ErrSessionNotFound {
			return &pb.DeleteSessionResponse{
				Success: false,
				Status:  common.StatusCode_STATUS_CODE_ERROR,
				Message: "Session not found",
			}, nil
		}
		s.logger.Error("Failed to delete session", zap.Error(err))
		return &pb.DeleteSessionResponse{
			Success: false,
			Status:  common.StatusCode_STATUS_CODE_ERROR,
			Message: err.Error(),
		}, nil
	}

	return &pb.DeleteSessionResponse{
		Success: true,
		Status:  common.StatusCode_STATUS_CODE_OK,
		Message: "Session deleted successfully",
	}, nil
}

// ListSessions lists user sessions
func (s *SessionServiceImpl) ListSessions(ctx context.Context, req *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	s.logger.Info("Listing sessions",
		zap.String("user_id", req.UserId),
		zap.Bool("active_only", req.ActiveOnly),
		zap.Int32("limit", req.Limit),
	)

	// In production, would query Redis for user sessions
	// For now, return empty list
	return &pb.ListSessionsResponse{
		Sessions:   []*pb.SessionSummary{},
		TotalCount: 0,
		Status:     common.StatusCode_STATUS_CODE_OK,
		Message:    "Sessions retrieved successfully",
	}, nil
}

// AddMessage adds a message to session history
func (s *SessionServiceImpl) AddMessage(ctx context.Context, req *pb.AddMessageRequest) (*pb.AddMessageResponse, error) {
	s.logger.Info("Adding message to session",
		zap.String("session_id", req.SessionId),
		zap.String("role", req.Message.Role),
	)

	// Convert protobuf message to internal format
	msg := session.Message{
		ID:        req.Message.Id,
		Role:      req.Message.Role,
		Content:   req.Message.Content,
		Timestamp: req.Message.Timestamp.AsTime(),
	}

	err := s.manager.AddMessage(ctx, req.SessionId, msg)
	if err != nil {
		if err == session.ErrSessionNotFound {
			return &pb.AddMessageResponse{
				Success: false,
				Status:  common.StatusCode_STATUS_CODE_ERROR,
				Message: "Session not found",
			}, nil
		}
		s.logger.Error("Failed to add message", zap.Error(err))
		return &pb.AddMessageResponse{
			Success: false,
			Status:  common.StatusCode_STATUS_CODE_ERROR,
			Message: err.Error(),
		}, nil
	}

	// Get updated history size
	sess, _ := s.manager.GetSession(ctx, req.SessionId)
	historySize := 0
	if sess != nil {
		historySize = len(sess.History)
	}

	return &pb.AddMessageResponse{
		Success:     true,
		HistorySize: int32(historySize),
		Status:      common.StatusCode_STATUS_CODE_OK,
		Message:     "Message added successfully",
	}, nil
}

// ClearHistory clears session message history
func (s *SessionServiceImpl) ClearHistory(ctx context.Context, req *pb.ClearHistoryRequest) (*pb.ClearHistoryResponse, error) {
	s.logger.Info("Clearing session history",
		zap.String("session_id", req.SessionId),
		zap.Bool("keep_context", req.KeepContext),
	)

	// Get session
	sess, err := s.manager.GetSession(ctx, req.SessionId)
	if err != nil {
		if err == session.ErrSessionNotFound {
			return &pb.ClearHistoryResponse{
				Success: false,
				Status:  common.StatusCode_STATUS_CODE_ERROR,
				Message: "Session not found",
			}, nil
		}
		return &pb.ClearHistoryResponse{
			Success: false,
			Status:  common.StatusCode_STATUS_CODE_ERROR,
			Message: err.Error(),
		}, nil
	}

	// Clear history
	sess.History = []session.Message{}

	// Clear context if requested
	if !req.KeepContext {
		sess.Context = make(map[string]interface{})
	}

	// Update session
	err = s.manager.UpdateSession(ctx, sess)
	if err != nil {
		s.logger.Error("Failed to clear history", zap.Error(err))
		return &pb.ClearHistoryResponse{
			Success: false,
			Status:  common.StatusCode_STATUS_CODE_ERROR,
			Message: err.Error(),
		}, nil
	}

	return &pb.ClearHistoryResponse{
		Success: true,
		Status:  common.StatusCode_STATUS_CODE_OK,
		Message: "History cleared successfully",
	}, nil
}

// Helper function to convert internal session to protobuf
func (s *SessionServiceImpl) sessionToProto(sess *session.Session, includeHistory bool) *pb.Session {
	pbSession := &pb.Session{
		Id:         sess.ID,
		UserId:     sess.UserID,
		CreatedAt:  timestamppb.New(sess.CreatedAt),
		LastActive: timestamppb.New(sess.UpdatedAt),
		ExpiresAt:  timestamppb.New(sess.CreatedAt.Add(720 * time.Hour)), // Default 30 days
	}

	// Convert context to protobuf struct
	if sess.Context != nil {
		pbContext, _ := structpb.NewStruct(sess.Context)
		pbSession.Context = pbContext
	}

	// Include history if requested
	if includeHistory {
		pbSession.History = make([]*pb.Message, len(sess.History))
		for i, msg := range sess.History {
			pbSession.History[i] = &pb.Message{
				Id:        msg.ID,
				Role:      msg.Role,
				Content:   msg.Content,
				Timestamp: timestamppb.New(msg.Timestamp),
			}
		}
	}

	// Add metrics (from session totals)
	pbSession.Metrics = &pb.SessionMetrics{
		TotalMessages: int32(len(sess.History)),
		TotalTokens:   int32(sess.TotalTokensUsed),
		TotalCostUsd:  sess.TotalCostUSD,
	}

	return pbSession
}

// RegisterSessionServiceServer registers the service with the gRPC server
func RegisterSessionServiceServer(s *grpc.Server, srv pb.SessionServiceServer) {
	pb.RegisterSessionServiceServer(s, srv)
}
