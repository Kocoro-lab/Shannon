package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	"go.uber.org/zap"
)

// StreamingHandler serves SSE endpoints for workflow events.
type StreamingHandler struct {
	mgr    *streaming.Manager
	logger *zap.Logger
}

func NewStreamingHandler(mgr *streaming.Manager, logger *zap.Logger) *StreamingHandler {
	return &StreamingHandler{mgr: mgr, logger: logger}
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
	// Optional: Last-Event-ID header or query param to replay from
	var lastID uint64
	if lei := r.Header.Get("Last-Event-ID"); lei != "" {
		if n, err := strconv.ParseUint(lei, 10, 64); err == nil {
			lastID = n
		}
	}
	if q := r.URL.Query().Get("last_event_id"); q != "" && lastID == 0 {
		if n, err := strconv.ParseUint(q, 10, 64); err == nil {
			lastID = n
		}
	}

	// CORS (dev-friendly)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Subscribe
	ch := h.mgr.Subscribe(wf, 256)
	defer h.mgr.Unsubscribe(wf, ch)

	// Send an initial comment to establish the stream
	fmt.Fprintf(w, ": connected to workflow %s\n\n", wf)
	flusher.Flush()

	// Replay backlog since lastID (best-effort)
	if lastID > 0 {
		events := h.mgr.ReplaySince(wf, lastID)
		for _, ev := range events {
			if len(typeFilter) > 0 {
				if _, ok := typeFilter[ev.Type]; !ok {
					continue
				}
			}
			if ev.Seq > 0 {
				fmt.Fprintf(w, "id: %d\n", ev.Seq)
			}
			if ev.Type != "" {
				fmt.Fprintf(w, "event: %s\n", ev.Type)
			}
			fmt.Fprintf(w, "data: %s\n\n", string(ev.Marshal()))
		}
		flusher.Flush()
	}

	// Heartbeat ticker
	hb := time.NewTicker(15 * time.Second)
	defer hb.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			h.logger.Info("SSE client disconnected", zap.String("workflow_id", wf))
			return
		case evt := <-ch:
			if len(typeFilter) > 0 {
				if _, ok := typeFilter[evt.Type]; !ok {
					continue
				}
			}
			// Write event type and data lines (SSE format)
			if evt.Seq > 0 {
				fmt.Fprintf(w, "id: %d\n", evt.Seq)
			}
			if evt.Type != "" {
				fmt.Fprintf(w, "event: %s\n", evt.Type)
			}
			fmt.Fprintf(w, "data: %s\n\n", string(evt.Marshal()))
			flusher.Flush()
		case <-hb.C:
			// Heartbeat to keep connections alive through proxies
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
