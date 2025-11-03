package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	serviceerror "go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

// StreamingHandler serves SSE endpoints for workflow events.
type StreamingHandler struct {
	mgr     *streaming.Manager
	logger  *zap.Logger
	tclient client.Client
}

func NewStreamingHandler(mgr *streaming.Manager, logger *zap.Logger) *StreamingHandler {
	return &StreamingHandler{mgr: mgr, logger: logger}
}

// SetTemporalClient allows wiring the Temporal client after handler construction.
func (h *StreamingHandler) SetTemporalClient(c client.Client) {
	h.tclient = c
}

// RegisterRoutes registers SSE routes on the provided mux.
func (h *StreamingHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/stream/sse", h.handleSSE)
	h.RegisterWebSocket(mux)
}

// handleSSE streams events for a workflow via Server-Sent Events.
// GET /stream/sse?workflow_id=<id>
func (h *StreamingHandler) handleSSE(w http.ResponseWriter, r *http.Request) {
	wf := r.URL.Query().Get("workflow_id")
	if wf == "" {
		http.Error(w, `{"error":"workflow_id required"}`, http.StatusBadRequest)
		return
	}
	// Optional: type filter (comma-separated)
	typeFilter := map[string]struct{}{}
	if s := r.URL.Query().Get("types"); s != "" {
		for _, t := range strings.Split(s, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				typeFilter[t] = struct{}{}
			}
		}
	}

	// Parse Last-Event-ID for resume support
	var lastSeq uint64
	var lastStreamID string
	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID == "" {
		lastEventID = r.URL.Query().Get("last_event_id")
	}

	if lastEventID != "" {
		// Check if it's a Redis stream ID (contains "-")
		if strings.Contains(lastEventID, "-") {
			lastStreamID = lastEventID
			h.logger.Debug("Resume from Redis stream ID",
				zap.String("workflow_id", wf),
				zap.String("stream_id", lastStreamID))
		} else {
			// Try to parse as numeric sequence
			if n, err := strconv.ParseUint(lastEventID, 10, 64); err == nil {
				lastSeq = n
				h.logger.Debug("Resume from sequence",
					zap.String("workflow_id", wf),
					zap.Uint64("seq", lastSeq))
			}
		}
	}

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Keep-Alive", "timeout=65")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send an initial comment to establish the stream
	fmt.Fprintf(w, ": connected to workflow %s\n\n", wf)
	flusher.Flush()

	// Track last stream ID for event deduplication
	var lastSentStreamID string
	firstEventSeen := false

	// Replay missed events based on resume point
	if lastStreamID != "" {
		// Resume from Redis stream ID
		events := h.mgr.ReplayFromStreamID(wf, lastStreamID)
		for _, ev := range events {
			// Mark that at least one event exists (even if filtered)
			firstEventSeen = true
			if len(typeFilter) > 0 {
				if _, ok := typeFilter[ev.Type]; !ok {
					continue
				}
			}
			// Track last stream ID from replay
			if ev.StreamID != "" {
				lastSentStreamID = ev.StreamID
			}
			// Prefer stream ID, fallback to seq
			if ev.StreamID != "" {
				fmt.Fprintf(w, "id: %s\n", ev.StreamID)
			} else if ev.Seq > 0 {
				fmt.Fprintf(w, "id: %d\n", ev.Seq)
			}
			if ev.Type != "" {
				fmt.Fprintf(w, "event: %s\n", ev.Type)
			}
			fmt.Fprintf(w, "data: %s\n\n", string(ev.Marshal()))
			flusher.Flush()
		}
	} else if lastSeq > 0 {
		// Resume from numeric sequence
		events := h.mgr.ReplaySince(wf, lastSeq)
		for _, ev := range events {
			// Mark that at least one event exists (even if filtered)
			firstEventSeen = true
			if len(typeFilter) > 0 {
				if _, ok := typeFilter[ev.Type]; !ok {
					continue
				}
			}
			// Track last stream ID from replay
			if ev.StreamID != "" {
				lastSentStreamID = ev.StreamID
			}
			// Prefer stream ID, fallback to seq
			if ev.StreamID != "" {
				fmt.Fprintf(w, "id: %s\n", ev.StreamID)
			} else if ev.Seq > 0 {
				fmt.Fprintf(w, "id: %d\n", ev.Seq)
			}
			if ev.Type != "" {
				fmt.Fprintf(w, "event: %s\n", ev.Type)
			}
			fmt.Fprintf(w, "data: %s\n\n", string(ev.Marshal()))
			flusher.Flush()
		}
	}

	// Subscribe to live events starting from where replay ended
	// Use last stream ID if available to avoid gaps, otherwise start fresh
	startFrom := "$" // Default to new messages only
	if lastSentStreamID != "" {
		// Continue from last replayed message to avoid gaps
		startFrom = lastSentStreamID
	} else if lastStreamID == "" && lastSeq == 0 {
		// No resume point, start from beginning
		startFrom = "0-0"
	}
	ch := h.mgr.SubscribeFrom(wf, 256, startFrom)
	defer h.mgr.Unsubscribe(wf, ch)

	// Heartbeat ticker (shorter to keep intermediaries happy)
	hb := time.NewTicker(10 * time.Second)
	defer hb.Stop()

	// First-event timeout timer
	firstEventTimer := time.NewTimer(30 * time.Second)
	defer firstEventTimer.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			h.logger.Info("SSE client disconnected", zap.String("workflow_id", wf))
			return
		case <-firstEventTimer.C:
			if !firstEventSeen {
				if h.tclient == nil {
					h.logger.Warn("First-event timeout but Temporal client not available", zap.String("workflow_id", wf))
					fmt.Fprintf(w, "event: ERROR_OCCURRED\n")
					fmt.Fprintf(w, "data: {\"workflow_id\":\"%s\",\"type\":\"ERROR_OCCURRED\",\"message\":\"Workflow validation unavailable\"}\n\n", wf)
					flusher.Flush()
					return
				}
				cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				_, err := h.tclient.DescribeWorkflowExecution(cctx, wf, "")
				cancel()
				if err != nil {
					if _, ok := err.(*serviceerror.NotFound); ok {
						// Emit an error event and close
						fmt.Fprintf(w, "event: ERROR_OCCURRED\n")
						fmt.Fprintf(w, "data: {\"workflow_id\":\"%s\",\"type\":\"ERROR_OCCURRED\",\"message\":\"Workflow not found\"}\n\n", wf)
						flusher.Flush()
						return
					}
					// Other errors (timeout, etc) also indicate invalid workflow
					fmt.Fprintf(w, "event: ERROR_OCCURRED\n")
					fmt.Fprintf(w, "data: {\"workflow_id\":\"%s\",\"type\":\"ERROR_OCCURRED\",\"message\":\"Workflow not found or unavailable\"}\n\n", wf)
					flusher.Flush()
					return
				}
				// Workflow exists but no events yet - reset timer and continue waiting
				firstEventTimer.Reset(30 * time.Second)
			}
		case evt := <-ch:
			if len(typeFilter) > 0 {
				if _, ok := typeFilter[evt.Type]; !ok {
					continue
				}
			}
			// Write event type and data lines (SSE format)
			// Prefer stream ID for robustness, fallback to seq for backward compatibility
			if evt.StreamID != "" {
				fmt.Fprintf(w, "id: %s\n", evt.StreamID)
			} else if evt.Seq > 0 {
				fmt.Fprintf(w, "id: %d\n", evt.Seq)
			}
			if evt.Type != "" {
				fmt.Fprintf(w, "event: %s\n", evt.Type)
			}
			fmt.Fprintf(w, "data: %s\n\n", string(evt.Marshal()))
			flusher.Flush()

			if !firstEventSeen {
				firstEventSeen = true
			}
		case <-hb.C:
			// Heartbeat to keep connections alive through proxies
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
