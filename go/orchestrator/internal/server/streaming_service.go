package server

import (
	"time"

	pb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// StreamingServiceServer implements the gRPC StreamingService backed by the in-process manager.
type StreamingServiceServer struct {
	pb.UnimplementedStreamingServiceServer
	mgr    *streaming.Manager
	logger *zap.Logger
}

func NewStreamingService(mgr *streaming.Manager, logger *zap.Logger) *StreamingServiceServer {
	return &StreamingServiceServer{mgr: mgr, logger: logger}
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

	ch := s.mgr.Subscribe(wf, 256)
	defer s.mgr.Unsubscribe(wf, ch)

	// Replay if requested
	if req.GetLastEventId() > 0 {
		for _, ev := range s.mgr.ReplaySince(wf, req.GetLastEventId()) {
			if len(typeFilter) > 0 {
				if _, ok := typeFilter[ev.Type]; !ok {
					continue
				}
			}
			if err := srv.Send(toProto(ev)); err != nil {
				return err
			}
		}
	}

	// Stream live
	for {
		select {
		case <-srv.Context().Done():
			return nil
		case ev := <-ch:
			if len(typeFilter) > 0 {
				if _, ok := typeFilter[ev.Type]; !ok {
					continue
				}
			}
			if err := srv.Send(toProto(ev)); err != nil {
				return err
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
	}
}
