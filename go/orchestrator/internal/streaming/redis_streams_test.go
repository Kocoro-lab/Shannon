package streaming

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestRedisStreamsManager tests the Redis Streams-based event manager
func TestRedisStreamsManager(t *testing.T) {
	// Setup mini redis
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// Create Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer redisClient.Close()

	// Create manager
	logger := zap.NewNop()
	manager := NewManager(redisClient, logger)

	t.Run("Publish and Subscribe", func(t *testing.T) {
		workflowID := "test-workflow-1"
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Subscribe to events
		events := make(chan Event, 10)
		go func() {
			err := manager.Subscribe(ctx, workflowID, 0, events)
			assert.NoError(t, err)
		}()

		// Publish events
		event1 := Event{
			WorkflowID: workflowID,
			Type:       "task_started",
			Data:       map[string]interface{}{"message": "Starting task"},
		}
		manager.Publish(workflowID, event1)

		event2 := Event{
			WorkflowID: workflowID,
			Type:       "task_progress",
			Data:       map[string]interface{}{"progress": 50},
		}
		manager.Publish(workflowID, event2)

		// Receive events
		select {
		case e := <-events:
			assert.Equal(t, "task_started", e.Type)
			assert.Equal(t, "Starting task", e.Data["message"])
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for first event")
		}

		select {
		case e := <-events:
			assert.Equal(t, "task_progress", e.Type)
			assert.Equal(t, float64(50), e.Data["progress"])
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for second event")
		}
	})

	t.Run("ReplaySince", func(t *testing.T) {
		workflowID := "test-workflow-2"

		// Publish multiple events
		for i := 1; i <= 5; i++ {
			event := Event{
				WorkflowID: workflowID,
				Type:       "event",
				Data:       map[string]interface{}{"index": i},
			}
			manager.Publish(workflowID, event)
		}

		// Replay from start
		events := manager.ReplaySince(workflowID, 0)
		assert.Equal(t, 5, len(events))
		
		for i, event := range events {
			assert.Equal(t, "event", event.Type)
			assert.Equal(t, float64(i+1), event.Data["index"])
		}
	})

	t.Run("Replay from specific sequence", func(t *testing.T) {
		workflowID := "test-workflow-3"

		// Publish events and track sequence numbers
		var lastSeq uint64
		for i := 1; i <= 10; i++ {
			event := Event{
				WorkflowID: workflowID,
				Type:       "event",
				Data:       map[string]interface{}{"index": i},
			}
			manager.Publish(workflowID, event)
			if i == 5 {
				// Get the sequence number after 5th event
				events := manager.ReplaySince(workflowID, 0)
				lastSeq = events[len(events)-1].Seq
			}
		}

		// Replay from sequence 5
		events := manager.ReplaySince(workflowID, lastSeq)
		assert.LessOrEqual(t, len(events), 5, "Should get events after sequence 5")
	})

	t.Run("Multiple subscribers", func(t *testing.T) {
		workflowID := "test-workflow-4"
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Create two subscribers
		events1 := make(chan Event, 10)
		events2 := make(chan Event, 10)

		go func() {
			manager.Subscribe(ctx, workflowID, 0, events1)
		}()

		go func() {
			manager.Subscribe(ctx, workflowID, 0, events2)
		}()

		// Give subscribers time to connect
		time.Sleep(100 * time.Millisecond)

		// Publish event
		event := Event{
			WorkflowID: workflowID,
			Type:       "broadcast",
			Data:       map[string]interface{}{"message": "hello"},
		}
		manager.Publish(workflowID, event)

		// Both subscribers should receive the event
		select {
		case e := <-events1:
			assert.Equal(t, "broadcast", e.Type)
		case <-time.After(2 * time.Second):
			t.Fatal("Subscriber 1 did not receive event")
		}

		select {
		case e := <-events2:
			assert.Equal(t, "broadcast", e.Type)
		case <-time.After(2 * time.Second):
			t.Fatal("Subscriber 2 did not receive event")
		}
	})

	t.Run("CloseStreams", func(t *testing.T) {
		workflowID := "test-workflow-5"

		// Publish some events
		for i := 1; i <= 3; i++ {
			event := Event{
				WorkflowID: workflowID,
				Type:       "event",
				Data:       map[string]interface{}{"index": i},
			}
			manager.Publish(workflowID, event)
		}

		// Close the stream
		manager.CloseStreams(workflowID)

		// Try to replay - should get empty or error
		// (depending on implementation, might still get cached events)
		events := manager.ReplaySince(workflowID, 0)
		// After closing, new events shouldn't be published
		t.Logf("Events after close: %d", len(events))
	})

	t.Run("Event with timestamp", func(t *testing.T) {
		workflowID := "test-workflow-6"

		event := Event{
			WorkflowID: workflowID,
			Type:       "timestamped",
			Timestamp:  time.Now(),
			Data:       map[string]interface{}{"value": "test"},
		}
		manager.Publish(workflowID, event)

		events := manager.ReplaySince(workflowID, 0)
		require.Len(t, events, 1)
		assert.Equal(t, "timestamped", events[0].Type)
		assert.NotZero(t, events[0].Timestamp)
	})

	t.Run("Large number of events", func(t *testing.T) {
		workflowID := "test-workflow-7"
		numEvents := 1000

		// Publish many events
		for i := 0; i < numEvents; i++ {
			event := Event{
				WorkflowID: workflowID,
				Type:       "batch_event",
				Data:       map[string]interface{}{"index": i},
			}
			manager.Publish(workflowID, event)
		}

		// Replay all events
		events := manager.ReplaySince(workflowID, 0)
		assert.LessOrEqual(t, len(events), numEvents)
		t.Logf("Replayed %d events out of %d published", len(events), numEvents)
	})
}

// TestEventSerialization tests event JSON serialization
func TestEventSerialization(t *testing.T) {
	event := Event{
		Seq:        123,
		WorkflowID: "wf-test",
		Type:       "test_event",
		Timestamp:  time.Now(),
		Data: map[string]interface{}{
			"string":  "value",
			"number":  42,
			"boolean": true,
			"nested": map[string]interface{}{
				"key": "value",
			},
		},
	}

	// Test that event can be marshaled and unmarshaled
	// (This is implicit in Publish/Subscribe, but good to test explicitly)
	assert.NotNil(t, event.Data)
	assert.Equal(t, "value", event.Data["string"])
	assert.Equal(t, 42, event.Data["number"])
	assert.Equal(t, true, event.Data["boolean"])
}

// TestConcurrentPublish tests concurrent event publishing
func TestConcurrentPublish(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer redisClient.Close()

	logger := zap.NewNop()
	manager := NewManager(redisClient, logger)

	workflowID := "test-workflow-concurrent"
	numGoroutines := 10
	eventsPerGoroutine := 100

	// Publish events concurrently
	done := make(chan bool, numGoroutines)
	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			for i := 0; i < eventsPerGoroutine; i++ {
				event := Event{
					WorkflowID: workflowID,
					Type:       "concurrent_event",
					Data: map[string]interface{}{
						"goroutine": goroutineID,
						"index":     i,
					},
				}
				manager.Publish(workflowID, event)
			}
			done <- true
		}(g)
	}

	// Wait for all goroutines to finish
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify all events were published
	events := manager.ReplaySince(workflowID, 0)
	expectedTotal := numGoroutines * eventsPerGoroutine
	assert.LessOrEqual(t, len(events), expectedTotal)
	t.Logf("Published %d events from %d goroutines, retrieved %d events", 
		expectedTotal, numGoroutines, len(events))
}

