package vectordb

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/circuitbreaker"
    ometrics "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/tracing"
    "github.com/google/uuid"
    "go.uber.org/zap"
)

// Client is a minimal Qdrant HTTP client
type Client struct {
    cfg  Config
    http *http.Client
    base string
    httpw *circuitbreaker.HTTPWrapper
    log  *zap.Logger
}

var global *Client

func Initialize(cfg Config) {
    c := cfg
    if c.Port == 0 { c.Port = 6333 }
    if c.TopK == 0 { c.TopK = 5 }
    if c.Timeout == 0 { c.Timeout = 5 * time.Second }
    if c.TaskEmbeddings == "" { c.TaskEmbeddings = "task_embeddings" }
    logger, _ := zap.NewProduction()
    httpClient := &http.Client{
        Timeout:   c.Timeout,
        Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
    }
    httpw := circuitbreaker.NewHTTPWrapper(httpClient, "qdrant", "vectordb", logger)
    client := &Client{cfg: c, http: httpClient, base: fmt.Sprintf("http://%s:%d", c.Host, c.Port), httpw: httpw, log: logger}
    global = client
}

func Get() *Client { return global }

// qdrant search request/response (simplified)
type qdrantQueryRequest struct {
    Query          []float32              `json:"query"`
    Limit          int                    `json:"limit"`
    ScoreThreshold *float64               `json:"score_threshold,omitempty"`
    WithPayload    bool                   `json:"with_payload"`
    Filter         map[string]interface{} `json:"filter,omitempty"`
}

type qdrantPoint struct {
    ID      interface{}            `json:"id"`
    Score   float64                `json:"score"`
    Payload map[string]interface{} `json:"payload"`
}

type qdrantSearchResponse struct {
    Result []qdrantPoint `json:"result"`
    Status string        `json:"status"`
}

func (c *Client) search(ctx context.Context, collection string, vec []float32, limit int, threshold float64, filter map[string]interface{}) ([]qdrantPoint, error) {
    if c == nil || !c.cfg.Enabled { return nil, fmt.Errorf("vectordb: search called while disabled") }
    start := time.Now()
    
    // Start tracing span for vector search
    ctx, span := tracing.StartHTTPSpan(ctx, "POST", fmt.Sprintf("%s/collections/%s/points/query", c.base, collection))
    defer span.End()
    
    // Prefer modern /points/query; on failure, fallback to /points/search for compatibility
    var thr *float64
    if threshold > 0 { thr = &threshold }
    reqBody := qdrantQueryRequest{Query: vec, Limit: limit, ScoreThreshold: thr, WithPayload: true, Filter: filter}
    buf, _ := json.Marshal(reqBody)

    call := func(url string, body []byte) (*http.Response, error) {
        req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
        if err != nil { return nil, err }
        req.Header.Set("Content-Type", "application/json")
        tracing.InjectTraceparent(ctx, req)
        return c.httpw.Do(req)
    }

    urlQuery := fmt.Sprintf("%s/collections/%s/points/query", c.base, collection)
    resp, err := call(urlQuery, buf)
    if err != nil {
        ometrics.RecordVectorSearchMetrics(collection, "error", time.Since(start).Seconds())
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        // fallback to /points/search
        urlSearch := fmt.Sprintf("%s/collections/%s/points/search", c.base, collection)
        // map to search payload {vector: ...}
        legacy := map[string]interface{}{"vector": vec, "limit": limit, "with_payload": true}
        if threshold > 0 { legacy["score_threshold"] = threshold }
        if filter != nil { legacy["filter"] = filter }
        buf2, _ := json.Marshal(legacy)
        resp2, err2 := call(urlSearch, buf2)
        if err2 != nil {
            ometrics.RecordVectorSearchMetrics(collection, "error", time.Since(start).Seconds())
            return nil, fmt.Errorf("qdrant query/search failed: %w", err2)
        }
        defer resp2.Body.Close()
        if resp2.StatusCode != http.StatusOK {
            ometrics.RecordVectorSearchMetrics(collection, "error", time.Since(start).Seconds())
            return nil, fmt.Errorf("qdrant status %d", resp2.StatusCode)
        }
        var qr qdrantSearchResponse
        if err := json.NewDecoder(resp2.Body).Decode(&qr); err != nil {
            ometrics.RecordVectorSearchMetrics(collection, "error", time.Since(start).Seconds())
            return nil, err
        }
        ometrics.RecordVectorSearchMetrics(collection, "ok", time.Since(start).Seconds())
        return qr.Result, nil
    }
    var qr qdrantSearchResponse
    if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
        ometrics.RecordVectorSearchMetrics(collection, "error", time.Since(start).Seconds())
        return nil, err
    }
    ometrics.RecordVectorSearchMetrics(collection, "ok", time.Since(start).Seconds())
    return qr.Result, nil
}

// Upsert inserts or updates one or more points into a collection
func (c *Client) Upsert(ctx context.Context, collection string, points []UpsertItem) (*UpsertResponse, error) {
    if c == nil || !c.cfg.Enabled { return nil, fmt.Errorf("vectordb: upsert called while disabled") }
    
    // Start tracing span for vector upsert
    url := fmt.Sprintf("%s/collections/%s/points", c.base, collection)
    ctx, span := tracing.StartHTTPSpan(ctx, "PUT", url)
    defer span.End()
    
    body := map[string]interface{}{
        "points": points,
    }
    buf, _ := json.Marshal(body)
    req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(buf))
    if err != nil { return nil, err }
    req.Header.Set("Content-Type", "application/json")
    tracing.InjectTraceparent(ctx, req)
    resp, err := c.httpw.Do(req)
    if err != nil { return nil, err }
    defer resp.Body.Close()
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return nil, fmt.Errorf("qdrant upsert status %d", resp.StatusCode)
    }
    var r UpsertResponse
    if err := json.NewDecoder(resp.Body).Decode(&r); err != nil { return nil, err }
    return &r, nil
}

// UpsertTaskEmbedding helper for inserting a query/answer embedding into TaskEmbeddings collection
func (c *Client) UpsertTaskEmbedding(ctx context.Context, vec []float32, payload map[string]interface{}) (*UpsertResponse, error) {
    p := UpsertItem{
        ID:      uuid.New().String(),
        Vector:  vec,
        Payload: payload,
    }
    return c.Upsert(ctx, c.cfg.TaskEmbeddings, []UpsertItem{p})
}

// UpsertSummaryEmbedding inserts a summary into the configured summaries collection
// Falls back to TaskEmbeddings if Summaries collection is not configured.
func (c *Client) UpsertSummaryEmbedding(ctx context.Context, vec []float32, payload map[string]interface{}) (*UpsertResponse, error) {
    collection := c.cfg.Summaries
    if collection == "" {
        collection = c.cfg.TaskEmbeddings
    }
    p := UpsertItem{
        ID:      uuid.New().String(),
        Vector:  vec,
        Payload: payload,
    }
    return c.Upsert(ctx, collection, []UpsertItem{p})
}
