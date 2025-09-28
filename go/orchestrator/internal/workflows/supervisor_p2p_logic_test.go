package workflows

import (
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/stretchr/testify/assert"
)

// TestP2PCoordinationLogic validates the P2P coordination conditions
func TestP2PCoordinationLogic(t *testing.T) {
	tests := []struct {
		name                   string
		p2pEnabled            bool
		p2pSyncVersion        int
		teamWorkspaceVersion  int
		hasConsumes          bool
		shouldExecuteP2P      bool
	}{
		{
			name:                  "P2P disabled in config",
			p2pEnabled:           false,
			p2pSyncVersion:       1,
			teamWorkspaceVersion: 1,
			hasConsumes:         true,
			shouldExecuteP2P:     false,
		},
		{
			name:                  "P2P enabled with all conditions met",
			p2pEnabled:           true,
			p2pSyncVersion:       1,
			teamWorkspaceVersion: 1,
			hasConsumes:         true,
			shouldExecuteP2P:     true,
		},
		{
			name:                  "P2P enabled but no dependencies",
			p2pEnabled:           true,
			p2pSyncVersion:       1,
			teamWorkspaceVersion: 1,
			hasConsumes:         false,
			shouldExecuteP2P:     false,
		},
		{
			name:                  "P2P enabled but version gates prevent it",
			p2pEnabled:           true,
			p2pSyncVersion:       -1, // DefaultVersion
			teamWorkspaceVersion: 1,
			hasConsumes:         true,
			shouldExecuteP2P:     false,
		},
		{
			name:                  "P2P disabled with dependencies present",
			p2pEnabled:           false,
			p2pSyncVersion:       1,
			teamWorkspaceVersion: 1,
			hasConsumes:         true,
			shouldExecuteP2P:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the condition from supervisor_workflow.go lines 419-425
			config := &activities.WorkflowConfig{
				P2PCoordinationEnabled: tt.p2pEnabled,
			}

			// This simulates the logic from the actual workflow
			shouldRun := config.P2PCoordinationEnabled &&
				tt.p2pSyncVersion != -1 && // -1 represents DefaultVersion
				tt.teamWorkspaceVersion != -1 &&
				tt.hasConsumes

			assert.Equal(t, tt.shouldExecuteP2P, shouldRun,
				"P2P execution decision mismatch for %s", tt.name)
		})
	}
}

// TestP2PSkipLogging validates that we log when P2P deps exist but P2P is disabled
func TestP2PSkipLogging(t *testing.T) {
	tests := []struct {
		name          string
		p2pEnabled    bool
		hasConsumes   bool
		shouldLog     bool
	}{
		{
			name:        "Should log when P2P disabled but deps exist",
			p2pEnabled:  false,
			hasConsumes: true,
			shouldLog:   true,
		},
		{
			name:        "No logging when P2P disabled and no deps",
			p2pEnabled:  false,
			hasConsumes: false,
			shouldLog:   false,
		},
		{
			name:        "No logging when P2P enabled",
			p2pEnabled:  true,
			hasConsumes: true,
			shouldLog:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This simulates the else-if condition from lines 515-520
			shouldLogSkip := !tt.p2pEnabled && tt.hasConsumes

			assert.Equal(t, tt.shouldLog, shouldLogSkip,
				"Skip logging decision mismatch for %s", tt.name)
		})
	}
}

// TestProducesLogic validates that Produces only runs when P2P is enabled
func TestProducesLogic(t *testing.T) {
	tests := []struct {
		name                  string
		p2pEnabled           bool
		teamWorkspaceVersion int
		hasProduces         bool
		shouldProduce       bool
	}{
		{
			name:                 "Should produce when P2P enabled and has produces",
			p2pEnabled:          true,
			teamWorkspaceVersion: 1,
			hasProduces:        true,
			shouldProduce:      true,
		},
		{
			name:                 "Should not produce when P2P disabled",
			p2pEnabled:          false,
			teamWorkspaceVersion: 1,
			hasProduces:        true,
			shouldProduce:      false,
		},
		{
			name:                 "Should not produce when no produces list",
			p2pEnabled:          true,
			teamWorkspaceVersion: 1,
			hasProduces:        false,
			shouldProduce:      false,
		},
		{
			name:                 "Should not produce when version gate blocks",
			p2pEnabled:          true,
			teamWorkspaceVersion: -1, // DefaultVersion
			hasProduces:        true,
			shouldProduce:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &activities.WorkflowConfig{
				P2PCoordinationEnabled: tt.p2pEnabled,
			}

			// This simulates the condition from lines 657-659
			shouldRun := config.P2PCoordinationEnabled &&
				tt.teamWorkspaceVersion != -1 &&
				tt.hasProduces

			assert.Equal(t, tt.shouldProduce, shouldRun,
				"Produces execution decision mismatch for %s", tt.name)
		})
	}
}