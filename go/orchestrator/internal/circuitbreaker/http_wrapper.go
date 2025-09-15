package circuitbreaker

import (
    "net/http"
    "time"

    "go.uber.org/zap"
)

// HTTPWrapper wraps an http.Client with a circuit breaker and records metrics consistently
type HTTPWrapper struct {
    client  *http.Client
    cb      *CircuitBreaker
    name    string
    service string
    logger  *zap.Logger
}

// NewHTTPWrapper creates a new HTTP wrapper with circuit breaker and metrics
func NewHTTPWrapper(client *http.Client, name, service string, logger *zap.Logger) *HTTPWrapper {
    if client == nil {
        client = &http.Client{Timeout: 5 * time.Second}
    }
    if logger == nil {
        logger, _ = zap.NewProduction()
    }
    cb := NewCircuitBreaker(name, GetHTTPConfig().ToConfig(), logger)
    GlobalMetricsCollector.RegisterCircuitBreaker(name, service, cb)
    return &HTTPWrapper{client: client, cb: cb, name: name, service: service, logger: logger}
}

// Do executes an HTTP request through the circuit breaker. 5xx responses are treated as failures
// for breaker purposes; 4xx do not trip the breaker.
func (hw *HTTPWrapper) Do(req *http.Request) (*http.Response, error) {
    var resp *http.Response
    err := hw.cb.Execute(req.Context(), func() error {
        var err2 error
        resp, err2 = hw.client.Do(req)
        // If transport error, propagate
        if err2 != nil {
            return err2
        }
        // Classify 5xx as breaker failures
        if resp.StatusCode >= 500 {
            return &httpStatusError{code: resp.StatusCode}
        }
        return nil
    })

    // Record metrics
    state := hw.cb.State()
    success := err == nil
    GlobalMetricsCollector.RecordRequest(hw.name, hw.service, state, success)

    // If breaker failed due to 5xx classification, still return the underlying response to caller
    // If err is httpStatusError, we already have a valid resp; return it with nil error
    if _, ok := err.(*httpStatusError); ok {
        return resp, nil
    }
    return resp, err
}

// httpStatusError marks 5xx responses for breaker accounting
type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return http.StatusText(e.code) }
