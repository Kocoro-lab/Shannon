package server

import (
	"context"
	"time"

	pb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	serviceerror "go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// StreamingServiceServer implements the gRPC StreamingService backed by the in-process manager.
type StreamingServiceServer struct {
	pb.UnimplementedStreamingServiceServer
	mgr     *streaming.Manager
	logger  *zap.Logger
	tclient client.Client
}

func NewStreamingService(mgr *streaming.Manager, logger *zap.Logger) *StreamingServiceServer {
	return &StreamingServiceServer{mgr: mgr, logger: logger}
}

// SetTemporalClient allows wiring the Temporal client after service construction.
func (s *StreamingServiceServer) SetTemporalClient(c client.Client) {
	s.tclient = c
}

func (s *StreamingServiceServer) StreamTaskExecution(req *pb.StreamRequest, srv pb.StreamingService_StreamTaskExecutionServer) error {
	wf := req.GetWorkflowId()
	if wf == "" {
		return nil
	}
	// Build type filter set
	typeFilter := map[string]struct{}{}
	for _, t := range req.GetTypes() {
		if t != "" {
			typeFilter[t] = struct{}{}
		}
	}

	var lastSentStreamID string
	firstEventSeen := false

	// Replay based on stream ID or seq (prefer stream ID)
	if req.GetLastStreamId() != "" {
		// Resume from Redis stream ID
		for _, ev := range s.mgr.ReplayFromStreamID(wf, req.GetLastStreamId()) {
			// Mark that at least one event exists (even if filtered out)
			firstEventSeen = true
			if len(typeFilter) > 0 {
				if _, ok := typeFilter[ev.Type]; !ok {
					continue
				}
			}
			if ev.StreamID != "" {
				lastSentStreamID = ev.StreamID
			}
			if err := srv.Send(toProto(ev)); err != nil {
				return err
			}
		}
	} else if req.GetLastEventId() > 0 {
		// Backward compat: resume from numeric seq
		for _, ev := range s.mgr.ReplaySince(wf, req.GetLastEventId()) {
			// Mark that at least one event exists (even if filtered out)
			firstEventSeen = true
			if len(typeFilter) > 0 {
				if _, ok := typeFilter[ev.Type]; !ok {
					continue
				}
			}
			if ev.StreamID != "" {
				lastSentStreamID = ev.StreamID
			}
			if err := srv.Send(toProto(ev)); err != nil {
				return err
			}
		}
	}

	// Subscribe to live events, avoiding gaps
	startFrom := "$" // Default to new messages only
	if lastSentStreamID != "" {
		// Continue from last replayed message
		startFrom = lastSentStreamID
	} else if req.GetLastStreamId() == "" && req.GetLastEventId() == 0 {
		// No resume point, start from beginning
		startFrom = "0-0"
	}
	ch := s.mgr.SubscribeFrom(wf, 256, startFrom)
	defer s.mgr.Unsubscribe(wf, ch)

	// First-event timeout for invalid/non-existent workflows
	firstEventTimer := time.NewTimer(30 * time.Second)
	defer firstEventTimer.Stop()

	// Stream live events
	for {
		select {
		case <-srv.Context().Done():
			return nil
		case <-firstEventTimer.C:
			if !firstEventSeen {
				if s.tclient == nil {
					s.logger.Warn("First-event timeout but Temporal client not available", zap.String("workflow_id", wf))
					return grpcstatus.Error(codes.Internal, "workflow validation unavailable")
				}
				ctx, cancel := context.WithTimeout(srv.Context(), 2*time.Second)
				_, err := s.tclient.DescribeWorkflowExecution(ctx, wf, "")
				cancel()
				if err != nil {
					if _, ok := err.(*serviceerror.NotFound); ok {
						return grpcstatus.Error(codes.NotFound, "workflow not found")
					}
					// Other errors (timeout, etc) also indicate invalid workflow
					return grpcstatus.Error(codes.NotFound, "workflow not found or unavailable")
				}
				// Workflow exists but no events yet - reset timer and keep waiting
				firstEventTimer.Reset(30 * time.Second)
			}
		case ev, ok := <-ch:
			if !ok {
				// Channel closed unexpectedly
				return nil
			}

			// Check for WORKFLOW_COMPLETED before filtering to ensure stream closes
			isCompleted := ev.Type == "WORKFLOW_COMPLETED"

			// Any incoming event means the workflow exists; disable first-event detection
			if !firstEventSeen {
				firstEventSeen = true
			}

			// Apply type filter
			if len(typeFilter) > 0 {
				if _, ok := typeFilter[ev.Type]; !ok {
					// Skip sending this event, but still close on completion
					if isCompleted {
						return nil
					}
					continue
				}
			}

			// Send event
			if err := srv.Send(toProto(ev)); err != nil {
				return err
			}

			// Close stream after WORKFLOW_COMPLETED
			if isCompleted {
				return nil
			}
		}
	}
}

func toProto(ev streaming.Event) *pb.TaskUpdate {
	ts := ev.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	return &pb.TaskUpdate{
		WorkflowId: ev.WorkflowID,
		Type:       ev.Type,
		AgentId:    ev.AgentID,
		Message:    ev.Message,
		Timestamp:  timestamppb.New(ts),
		Seq:        ev.Seq,
		StreamId:   ev.StreamID,
	}
}
