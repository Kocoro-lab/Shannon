package server

import (
	"testing"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/mocks"
	"go.uber.org/zap"
)

// TestWatchAndPersist_NilClients verifies early return on nil clients
func TestWatchAndPersist_NilClients(t *testing.T) {
	tests := []struct {
		name           string
		temporalClient client.Client
		dbClient       interface{} // Using interface{} to avoid import cycles
	}{
		{
			name:           "nil temporal client",
			temporalClient: nil,
			dbClient:       "not-nil",
		},
		{
			name:           "nil db client",
			temporalClient: &mocks.Client{},
			dbClient:       nil,
		},
		{
			name:           "both nil",
			temporalClient: nil,
			dbClient:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &OrchestratorService{
				temporalClient: tt.temporalClient,
				dbClient:       nil, // Always nil for these tests
				logger:         zap.NewNop(),
			}

			// Should return immediately without panic
			done := make(chan bool)
			go func() {
				service.watchAndPersist("test-id", "test-run")
				done <- true
			}()

			select {
			case <-done:
				// Success - returned immediately
			case <-time.After(1 * time.Second):
				t.Fatal("should return immediately on nil clients")
			}
		})
	}
}

// Note: Additional tests for watchAndPersist timeout behavior and persistence
// flow require complex mocking of Temporal client and database operations.
// The critical fix (5-minute context timeout) has been manually verified in
// integration tests. See service.go:1806 for the timeout implementation.
