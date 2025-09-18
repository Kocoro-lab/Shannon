package vectordb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/tracing"
)

// FindSimilarQueries queries the task_embeddings (or cases) collection for similar historical queries
func (c *Client) FindSimilarQueries(ctx context.Context, embedding []float32, limit int) ([]SimilarQuery, error) {
	if limit <= 0 {
		limit = c.cfg.TopK
	}
	// Extract tenant_id from context for filtering if available
	var filter map[string]interface{}
	if userCtx, ok := ctx.Value(auth.UserContextKey).(*auth.UserContext); ok && userCtx.TenantID.String() != "00000000-0000-0000-0000-000000000000" {
		filter = map[string]interface{}{
			"must": []map[string]interface{}{
				{"key": "tenant_id", "match": map[string]interface{}{"value": userCtx.TenantID.String()}},
			},
		}
	}
	pts, err := c.search(ctx, c.cfg.TaskEmbeddings, embedding, limit, c.cfg.Threshold, filter)
	if err != nil {
		return nil, err
	}
	out := make([]SimilarQuery, 0, len(pts))
	for _, p := range pts {
		sq := SimilarQuery{Confidence: p.Score}
		if q, ok := p.Payload["query"].(string); ok {
			sq.Query = q
		}
		if o, ok := p.Payload["outcome"].(string); ok {
			sq.Outcome = o
		}
		if ts, ok := p.Payload["timestamp"].(float64); ok {
			sq.Timestamp = time.Unix(int64(ts), 0)
		}
		out = append(out, sq)
	}
	return out, nil
}

// GetSessionContext searches for recent points with the same session_id
func (c *Client) GetSessionContext(ctx context.Context, sessionID string, tenantID string, topK int) ([]ContextItem, error) {
	if topK <= 0 {
		topK = c.cfg.TopK
	}
	// Use Qdrant Scroll API for filter-only retrieval
	url := fmt.Sprintf("%s/collections/%s/points/scroll", c.base, c.cfg.TaskEmbeddings)

	// Start tracing span for session context retrieval
	ctx, span := tracing.StartHTTPSpan(ctx, "POST", url)
	defer span.End()
	must := []map[string]interface{}{
		{"key": "session_id", "match": map[string]interface{}{"value": sessionID}},
	}
	if tenantID != "" {
		must = append(must, map[string]interface{}{"key": "tenant_id", "match": map[string]interface{}{"value": tenantID}})
	}
	body := map[string]interface{}{
		"limit":        topK,
		"with_payload": true,
		"filter":       map[string]interface{}{"must": must},
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	tracing.InjectTraceparent(ctx, req)
	resp, err := c.httpw.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qdrant scroll status %d", resp.StatusCode)
	}
	var r struct {
		Result struct {
			Points []struct {
				Payload map[string]interface{} `json:"payload"`
				Score   float64                `json:"score,omitempty"`
			} `json:"points"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	out := make([]ContextItem, 0, len(r.Result.Points))
	for _, p := range r.Result.Points {
		out = append(out, ContextItem{SessionID: sessionID, Payload: p.Payload, Score: p.Score})
	}
	return out, nil
}
