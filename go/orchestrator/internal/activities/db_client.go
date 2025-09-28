package activities

import (
	"sync"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/db"
)

var (
	globalDBClient *db.Client
	dbClientMutex  sync.RWMutex
)

// SetGlobalDBClient sets the global database client for use by activities
// This should be called once during application initialization
func SetGlobalDBClient(client *db.Client) {
	dbClientMutex.Lock()
	defer dbClientMutex.Unlock()
	globalDBClient = client
}

// GetGlobalDBClient returns the global database client
// Returns nil if not initialized
func GetGlobalDBClient() *db.Client {
	dbClientMutex.RLock()
	defer dbClientMutex.RUnlock()
	return globalDBClient
}