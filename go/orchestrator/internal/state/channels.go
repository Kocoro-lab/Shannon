package state

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// StateChannel represents a typed, validated state container
type StateChannel struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Data        interface{}     `json:"data"`
	Metadata    ChannelMetadata `json:"metadata"`
	mu          sync.RWMutex
	validators  []Validator
	checkpoints map[string]Checkpoint
}

// ChannelMetadata contains metadata about the state channel
type ChannelMetadata struct {
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	UpdateCount    int       `json:"update_count"`
	LastCheckpoint string    `json:"last_checkpoint"`
}

// Checkpoint represents a saved state snapshot
type Checkpoint struct {
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	State     json.RawMessage        `json:"state"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// Validator is a function that validates state
type Validator func(interface{}) error

// Validatable interface for types that can validate themselves
type Validatable interface {
	Validate() error
}

// NewStateChannel creates a new state channel
func NewStateChannel(name string) *StateChannel {
	return &StateChannel{
		Name:    name,
		Version: "1.0.0",
		Metadata: ChannelMetadata{
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		checkpoints: make(map[string]Checkpoint),
		validators:  []Validator{},
	}
}

// AddValidator adds a validation function to the channel
func (sc *StateChannel) AddValidator(v Validator) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.validators = append(sc.validators, v)
}

// Set updates the state with validation
func (sc *StateChannel) Set(data interface{}) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Validate if data implements Validatable
	if v, ok := data.(Validatable); ok {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}
	}

	// Run custom validators
	for _, validator := range sc.validators {
		if err := validator(data); err != nil {
			return fmt.Errorf("custom validation failed: %w", err)
		}
	}

	// Update state
	sc.Data = data
	sc.Metadata.UpdatedAt = time.Now()
	sc.Metadata.UpdateCount++

	return nil
}

// Get retrieves the current state
func (sc *StateChannel) Get() interface{} {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.Data
}

// Checkpoint saves the current state
func (sc *StateChannel) Checkpoint(metadata map[string]interface{}) (string, error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	data, err := json.Marshal(sc.Data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal state: %w", err)
	}

	checkpointID := uuid.New().String()
	checkpoint := Checkpoint{
		ID:        checkpointID,
		Timestamp: time.Now(),
		State:     data,
		Metadata:  metadata,
	}

	sc.checkpoints[checkpointID] = checkpoint
	sc.Metadata.LastCheckpoint = checkpointID

	return checkpointID, nil
}

// Restore restores state from a checkpoint
func (sc *StateChannel) Restore(checkpointID string, target interface{}) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	checkpoint, exists := sc.checkpoints[checkpointID]
	if !exists {
		return fmt.Errorf("checkpoint not found: %s", checkpointID)
	}

	if err := json.Unmarshal(checkpoint.State, target); err != nil {
		return fmt.Errorf("failed to unmarshal checkpoint: %w", err)
	}

	// Validate restored state
	if v, ok := target.(Validatable); ok {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("restored state validation failed: %w", err)
		}
	}

	sc.Data = target
	sc.Metadata.UpdatedAt = time.Now()
	sc.Metadata.UpdateCount++

	return nil
}

// ListCheckpoints returns all checkpoint IDs
func (sc *StateChannel) ListCheckpoints() []string {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	ids := make([]string, 0, len(sc.checkpoints))
	for id := range sc.checkpoints {
		ids = append(ids, id)
	}
	return ids
}

// GetCheckpoint retrieves a specific checkpoint
func (sc *StateChannel) GetCheckpoint(checkpointID string) (Checkpoint, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	checkpoint, exists := sc.checkpoints[checkpointID]
	return checkpoint, exists
}

// Clear removes all checkpoints except the most recent N
func (sc *StateChannel) Clear(keepRecent int) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if len(sc.checkpoints) <= keepRecent {
		return
	}

	// Sort checkpoints by timestamp
	type checkpointPair struct {
		id        string
		timestamp time.Time
	}

	pairs := make([]checkpointPair, 0, len(sc.checkpoints))
	for id, cp := range sc.checkpoints {
		pairs = append(pairs, checkpointPair{id: id, timestamp: cp.Timestamp})
	}

	// Sort by timestamp (newest first)
	for i := 0; i < len(pairs)-1; i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].timestamp.After(pairs[i].timestamp) {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}

	// Keep only the most recent N
	newCheckpoints := make(map[string]Checkpoint)
	for i := 0; i < keepRecent && i < len(pairs); i++ {
		id := pairs[i].id
		newCheckpoints[id] = sc.checkpoints[id]
	}

	sc.checkpoints = newCheckpoints
}
