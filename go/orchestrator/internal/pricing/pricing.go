package pricing

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	pmetrics "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
)

// Config structure for pricing section in config/models.yaml
type config struct {
	Pricing struct {
		Defaults struct {
			CombinedPer1K float64 `yaml:"combined_per_1k"`
		} `yaml:"defaults"`
		Models map[string]map[string]struct {
			InputPer1K    float64 `yaml:"input_per_1k"`
			OutputPer1K   float64 `yaml:"output_per_1k"`
			CombinedPer1K float64 `yaml:"combined_per_1k"`
		} `yaml:"models"`
	} `yaml:"pricing"`
}

var (
	mu          sync.RWMutex
	loaded      *config
	initialized bool
)

// default locations inside containers / local dev
var defaultPaths = []string{
	os.Getenv("MODELS_CONFIG_PATH"),
	"/app/config/models.yaml",
	"./config/models.yaml",
	"../../config/models.yaml",    // from go/orchestrator
	"../../../config/models.yaml", // from go/orchestrator/internal/*
}

// findUpConfig searches parent directories for config/models.yaml starting at CWD.
func findUpConfig() (string, bool) {
	wd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	// Walk upwards up to 6 levels to be safe in test/package paths
	for i := 0; i < 6; i++ {
		cand := filepath.Join(wd, "config", "models.yaml")
		if _, err := os.Stat(cand); err == nil {
			return cand, true
		}
		// Also try repo root style: look for a sibling "config/models.yaml" while we traverse up
		wd = filepath.Dir(wd)
	}
	return "", false
}

// loadLocked loads the configuration - must be called while holding mu.Lock()
func loadLocked() {
	var cfg config
	// 1) Try explicit and common defaults
	for _, p := range defaultPaths {
		if p == "" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var tmp config
		if err := yaml.Unmarshal(data, &tmp); err != nil {
			log.Printf("WARNING: Failed to unmarshal pricing config from %s: %v", p, err)
			continue
		}
		cfg = tmp
		log.Printf("Loaded pricing configuration from %s", p)
		break
	}
	// 2) If not loaded yet, search upwards from current working directory
	if cfg.Pricing.Defaults.CombinedPer1K == 0 && len(cfg.Pricing.Models) == 0 {
		if path, ok := findUpConfig(); ok {
			if data, err := os.ReadFile(path); err == nil {
				var tmp config
				if err := yaml.Unmarshal(data, &tmp); err == nil {
					cfg = tmp
					log.Printf("Loaded pricing configuration from %s", path)
				}
			}
		}
	}
	// No locking needed - caller must hold the lock
	loaded = &cfg
	initialized = true
}

func get() *config {
	mu.RLock()
	if initialized {
		defer mu.RUnlock()
		return loaded
	}
	mu.RUnlock()

	// Need to initialize - use write lock to prevent races
	mu.Lock()
	defer mu.Unlock()
	// Double-check after acquiring write lock
	if !initialized {
		loadLocked() // Call the version that doesn't lock
	}
	return loaded
}

// ModifiedTime returns the mtime of the config file used (best-effort)
func ModifiedTime() time.Time {
	for _, p := range defaultPaths {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil {
			return st.ModTime()
		}
	}
	return time.Time{}
}

// Reload forces a re-read of pricing configuration.
// Thread-safe: uses mutex to prevent race conditions.
func Reload() {
	mu.Lock()
	defer mu.Unlock()

	// Mark as uninitialized to force reload
	initialized = false
	// Load new configuration
	loadLocked()
}

// DefaultPerToken returns default combined price per token
func DefaultPerToken() float64 {
	cfg := get()
	if cfg.Pricing.Defaults.CombinedPer1K > 0 {
		return cfg.Pricing.Defaults.CombinedPer1K / 1000.0
	}
	// Fallback: $0.002 per 1K tokens (gpt-3.5-ish)
	return 0.000002
}

// PricePerTokenForModel returns combined price per token for a model if available
func PricePerTokenForModel(model string) (float64, bool) {
	if model == "" {
		return 0, false
	}
	cfg := get()
	for _, models := range cfg.Pricing.Models {
		if m, ok := models[model]; ok {
			if m.CombinedPer1K > 0 {
				return m.CombinedPer1K / 1000.0, true
			}
			// If only input/output provided, approximate combined as average
			if m.InputPer1K > 0 && m.OutputPer1K > 0 {
				return ((m.InputPer1K + m.OutputPer1K) / 2.0) / 1000.0, true
			}
		}
	}
	return 0, false
}

// CostForTokens returns cost in USD for total tokens with optional model
func CostForTokens(model string, tokens int) float64 {
	// Validate token count
	if tokens < 0 {
		tokens = 0 // Treat negative as zero to avoid negative costs
	}

	if price, ok := PricePerTokenForModel(model); ok {
		return float64(tokens) * price
	}
	if model == "" {
		pmetrics.PricingFallbacks.WithLabelValues("missing_model").Inc()
	} else {
		pmetrics.PricingFallbacks.WithLabelValues("unknown_model").Inc()
	}
	return float64(tokens) * DefaultPerToken()
}

// CostForSplit computes cost using input/output token split when available.
// Falls back to combined pricing or default if model not found.
func CostForSplit(model string, inputTokens, outputTokens int) float64 {
	// Validate token counts
	if inputTokens < 0 {
		inputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}

	cfg := get()
	// Find model pricing
	for _, models := range cfg.Pricing.Models {
		if m, ok := models[model]; ok {
			in := m.InputPer1K
			out := m.OutputPer1K
			if in > 0 && out > 0 {
				return (float64(inputTokens)/1000.0)*in + (float64(outputTokens)/1000.0)*out
			}
			// If only combined provided, approximate
			if m.CombinedPer1K > 0 {
				return (float64(inputTokens+outputTokens) / 1000.0) * m.CombinedPer1K
			}
			break
		}
	}
	// Unknown or missing model -> fallback
	if model == "" {
		pmetrics.PricingFallbacks.WithLabelValues("missing_model").Inc()
	} else {
		pmetrics.PricingFallbacks.WithLabelValues("unknown_model").Inc()
	}
	return float64(inputTokens+outputTokens) * DefaultPerToken()
}

// ValidateMap validates the pricing section in a raw config map for the config manager.
func ValidateMap(m map[string]interface{}) error {
	p, ok := m["pricing"].(map[string]interface{})
	if !ok {
		return nil
	}
	if d, ok := p["defaults"].(map[string]interface{}); ok {
		if v, ok := d["combined_per_1k"].(float64); ok && v < 0 {
			return errors.New("pricing.defaults.combined_per_1k must be >= 0")
		}
	}
	if provs, ok := p["models"].(map[string]interface{}); ok {
		for provName, pm := range provs {
			models, ok := pm.(map[string]interface{})
			if !ok {
				continue
			}
			for modelName, mv := range models {
				entry, ok := mv.(map[string]interface{})
				if !ok {
					continue
				}
				if v, ok := entry["input_per_1k"].(float64); ok && v < 0 {
					return errors.New("negative input_per_1k for " + provName + ":" + modelName)
				}
				if v, ok := entry["output_per_1k"].(float64); ok && v < 0 {
					return errors.New("negative output_per_1k for " + provName + ":" + modelName)
				}
				if v, ok := entry["combined_per_1k"].(float64); ok && v < 0 {
					return errors.New("negative combined_per_1k for " + provName + ":" + modelName)
				}
			}
		}
	}
	return nil
}
