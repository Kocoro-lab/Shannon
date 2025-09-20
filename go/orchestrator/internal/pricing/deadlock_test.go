package pricing

import (
	"sync"
	"testing"
	"time"
)

// TestNonDeadlock verifies that the double-checked locking pattern doesn't deadlock
func TestNoDeadlock(t *testing.T) {
	// Reset state
	mu.Lock()
	initialized = false
	loaded = nil
	mu.Unlock()

	// This should complete without deadlocking
	done := make(chan bool)
	go func() {
		// Call get() which will trigger loadLocked()
		_ = get()
		done <- true
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-time.After(1 * time.Second):
		t.Fatal("Deadlock detected - get() did not complete within 1 second")
	}
}

// TestConcurrentAccess verifies thread-safety of the double-checked locking
func TestConcurrentAccess(t *testing.T) {
	// Reset state
	mu.Lock()
	initialized = false
	loaded = nil
	mu.Unlock()

	// Launch multiple goroutines to access get() concurrently
	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfg := get()
			if cfg == nil {
				t.Error("get() returned nil")
			}
		}()
	}

	// Wait for all goroutines to complete
	done := make(chan bool)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Success - all goroutines completed
	case <-time.After(2 * time.Second):
		t.Fatal("Concurrent access test timed out - possible deadlock")
	}
}

// TestReloadNoDeadlock verifies that Reload doesn't deadlock
func TestReloadNoDeadlock(t *testing.T) {
	// Initialize first
	_ = get()

	// This should complete without deadlocking
	done := make(chan bool)
	go func() {
		Reload()
		done <- true
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-time.After(1 * time.Second):
		t.Fatal("Deadlock detected - Reload() did not complete within 1 second")
	}
}
