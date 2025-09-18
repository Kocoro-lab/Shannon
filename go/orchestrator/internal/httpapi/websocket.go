package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true }, // Dev-friendly, secure via proxy in prod
}

// RegisterWebSocket registers /stream/ws endpoint.
func (h *StreamingHandler) RegisterWebSocket(mux *http.ServeMux) {
	mux.HandleFunc("/stream/ws", h.handleWS)
}

func (h *StreamingHandler) handleWS(w http.ResponseWriter, r *http.Request) {
	wf := r.URL.Query().Get("workflow_id")
	if wf == "" {
		http.Error(w, "workflow_id required", http.StatusBadRequest)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Optional filters
	typeFilter := map[string]struct{}{}
	if s := r.URL.Query().Get("types"); s != "" {
		for _, t := range strings.Split(s, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				typeFilter[t] = struct{}{}
			}
		}
	}

	// Parse Last-Event-ID for resume support (StreamID or seq)
	var lastSeq uint64
	var lastStreamID string
	var lastSentStreamID string
	lastEventID := r.URL.Query().Get("last_event_id")

	if lastEventID != "" {
		// Check if it's a Redis stream ID (contains "-")
		if strings.Contains(lastEventID, "-") {
			lastStreamID = lastEventID
		} else {
			// Try to parse as numeric sequence
			if n, err := strconv.ParseUint(lastEventID, 10, 64); err == nil {
				lastSeq = n
			}
		}
	}

	// Replay missed events based on resume point
	if lastStreamID != "" {
		// Resume from Redis stream ID
		events := h.mgr.ReplayFromStreamID(wf, lastStreamID)
		for _, ev := range events {
			if len(typeFilter) > 0 {
				if _, ok := typeFilter[ev.Type]; !ok {
					continue
				}
			}
			// Track last stream ID from replay
			if ev.StreamID != "" {
				lastSentStreamID = ev.StreamID
			}
			if err := conn.WriteJSON(ev); err != nil {
				return
			}
		}
	} else if lastSeq > 0 {
		// Resume from numeric sequence
		events := h.mgr.ReplaySince(wf, lastSeq)
		for _, ev := range events {
			if len(typeFilter) > 0 {
				if _, ok := typeFilter[ev.Type]; !ok {
					continue
				}
			}
			// Track last stream ID from replay
			if ev.StreamID != "" {
				lastSentStreamID = ev.StreamID
			}
			if err := conn.WriteJSON(ev); err != nil {
				return
			}
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

	// Heartbeat ping
	conn.SetReadLimit(512)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	// Reader pump (discard client messages)
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Writer pump
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			if len(typeFilter) > 0 {
				if _, ok := typeFilter[ev.Type]; !ok {
					continue
				}
			}
			if err := conn.WriteJSON(ev); err != nil {
				return
			}
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(10*time.Second)); err != nil {
				return
			}
		}
	}
}
