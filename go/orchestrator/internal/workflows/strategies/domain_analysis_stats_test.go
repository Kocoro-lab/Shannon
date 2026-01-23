package strategies

import (
	"encoding/json"
	"testing"
)

// TestDomainAnalysisStatsDiscoveryFields tests that DiscoveryFailed and DiscoveryError
// fields are properly serialized and accessible in DomainAnalysisStats.
func TestDomainAnalysisStatsDiscoveryFields(t *testing.T) {
	tests := []struct {
		name            string
		stats           DomainAnalysisStats
		expectFailed    bool
		expectError     string
		expectJSON      bool // whether to also test JSON serialization
	}{
		{
			name: "successful discovery has no failure flags",
			stats: DomainAnalysisStats{
				PrefetchAttempted: 3,
				PrefetchSucceeded: 2,
				PrefetchFailed:    1,
				DiscoveryFailed:   false,
				DiscoveryError:    "",
			},
			expectFailed: false,
			expectError:  "",
			expectJSON:   true,
		},
		{
			name: "failed discovery sets both fields",
			stats: DomainAnalysisStats{
				PrefetchAttempted: 0,
				PrefetchSucceeded: 0,
				PrefetchFailed:    0,
				DiscoveryFailed:   true,
				DiscoveryError:    "web_search tool returned error: timeout",
			},
			expectFailed: true,
			expectError:  "web_search tool returned error: timeout",
			expectJSON:   true,
		},
		{
			name: "discovery failed with agent error",
			stats: DomainAnalysisStats{
				DiscoveryFailed: true,
				DiscoveryError:  "agent execution failed: context deadline exceeded",
			},
			expectFailed: true,
			expectError:  "agent execution failed: context deadline exceeded",
			expectJSON:   true,
		},
		{
			name: "discovery failed with default error message",
			stats: DomainAnalysisStats{
				DiscoveryFailed: true,
				DiscoveryError:  "discovery returned unsuccessful result",
			},
			expectFailed: true,
			expectError:  "discovery returned unsuccessful result",
			expectJSON:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test direct field access
			if tt.stats.DiscoveryFailed != tt.expectFailed {
				t.Errorf("DiscoveryFailed = %v, want %v", tt.stats.DiscoveryFailed, tt.expectFailed)
			}
			if tt.stats.DiscoveryError != tt.expectError {
				t.Errorf("DiscoveryError = %q, want %q", tt.stats.DiscoveryError, tt.expectError)
			}

			// Test JSON serialization
			if tt.expectJSON {
				jsonBytes, err := json.Marshal(tt.stats)
				if err != nil {
					t.Fatalf("Failed to marshal stats: %v", err)
				}

				var unmarshaled DomainAnalysisStats
				if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
					t.Fatalf("Failed to unmarshal stats: %v", err)
				}

				if unmarshaled.DiscoveryFailed != tt.expectFailed {
					t.Errorf("After JSON round-trip: DiscoveryFailed = %v, want %v",
						unmarshaled.DiscoveryFailed, tt.expectFailed)
				}
				if unmarshaled.DiscoveryError != tt.expectError {
					t.Errorf("After JSON round-trip: DiscoveryError = %q, want %q",
						unmarshaled.DiscoveryError, tt.expectError)
				}

				// Verify omitempty works correctly
				jsonStr := string(jsonBytes)
				if !tt.expectFailed {
					// When false, field should be omitted
					if contains(jsonStr, "discovery_failed") {
						t.Errorf("JSON should omit discovery_failed when false, got: %s", jsonStr)
					}
				}
				if tt.expectError == "" {
					// When empty, field should be omitted
					if contains(jsonStr, "discovery_error") {
						t.Errorf("JSON should omit discovery_error when empty, got: %s", jsonStr)
					}
				}
			}
		})
	}
}

// TestDomainAnalysisResultIncludesStats tests that DomainAnalysisResult properly
// includes Stats with discovery failure information.
func TestDomainAnalysisResultIncludesStats(t *testing.T) {
	result := DomainAnalysisResult{
		DomainAnalysisDigest: "",
		PrefetchURLs:         []string{},
		Stats: DomainAnalysisStats{
			PrefetchAttempted: 0,
			DiscoveryFailed:   true,
			DiscoveryError:    "rate limit exceeded",
		},
	}

	// Verify stats are accessible
	if !result.Stats.DiscoveryFailed {
		t.Error("Expected Stats.DiscoveryFailed to be true")
	}
	if result.Stats.DiscoveryError != "rate limit exceeded" {
		t.Errorf("Expected Stats.DiscoveryError = %q, got %q",
			"rate limit exceeded", result.Stats.DiscoveryError)
	}

	// Verify empty digest with failed discovery
	if result.DomainAnalysisDigest != "" {
		t.Error("Expected empty digest when discovery failed")
	}
}

// contains checks if substr is in s
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr) >= 0))
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
