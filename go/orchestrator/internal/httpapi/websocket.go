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
    CheckOrigin: func(r *http.Request) bool { return true }, // Dev-friendly, secure via proxy in prod
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
    if err != nil { return }
    defer conn.Close()

    // Optional filters
    typeFilter := map[string]struct{}{}
    if s := r.URL.Query().Get("types"); s != "" {
        for _, t := range strings.Split(s, ",") {
            t = strings.TrimSpace(t)
            if t != "" { typeFilter[t] = struct{}{} }
        }
    }
    var lastID uint64
    if q := r.URL.Query().Get("last_event_id"); q != "" {
        if n, err := strconv.ParseUint(q, 10, 64); err == nil { lastID = n }
    }

    ch := h.mgr.Subscribe(wf, 256)
    defer h.mgr.Unsubscribe(wf, ch)

    // Replay backlog
    if lastID > 0 {
        for _, ev := range h.mgr.ReplaySince(wf, lastID) {
            if len(typeFilter) > 0 {
                if _, ok := typeFilter[ev.Type]; !ok { continue }
            }
            if err := conn.WriteJSON(ev); err != nil { return }
        }
    }

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
            if _, _, err := conn.ReadMessage(); err != nil { return }
        }
    }()

    // Writer pump
    for {
        select {
        case <-r.Context().Done():
            return
        case ev := <-ch:
            if len(typeFilter) > 0 {
                if _, ok := typeFilter[ev.Type]; !ok { continue }
            }
            if err := conn.WriteJSON(ev); err != nil { return }
        case <-ticker.C:
            if err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(10*time.Second)); err != nil { return }
        }
    }
}
