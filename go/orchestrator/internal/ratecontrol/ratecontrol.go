package ratecontrol

import (
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type config struct {
	RateLimits struct {
		DefaultRPM    int `yaml:"default_rpm"`
		DefaultTPM    int `yaml:"default_tpm"`
		TierOverrides map[string]struct {
			RPM int `yaml:"rpm"`
			TPM int `yaml:"tpm"`
		} `yaml:"tier_overrides"`
		ProviderOverrides map[string]struct {
			RPM int `yaml:"rpm"`
			TPM int `yaml:"tpm"`
		} `yaml:"provider_overrides"`
	} `yaml:"rate_limits"`
}

type RateLimit struct {
	RPM int
	TPM int
}

var (
	mu          sync.RWMutex
	loaded      *config
	initialized bool
)

var defaultPaths = []string{
	os.Getenv("MODELS_CONFIG_PATH"),
	"/app/config/models.yaml",
	"./config/models.yaml",
	"../../config/models.yaml",
	"../../../config/models.yaml",
}

func loadLocked() {
	var cfg config
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
			log.Printf("WARNING: failed to unmarshal rate limit config from %s: %v", p, err)
			continue
		}
		cfg = tmp
		log.Printf("Loaded rate limit configuration from %s", p)
		break
	}
	if cfg.RateLimits.DefaultRPM == 0 && cfg.RateLimits.DefaultTPM == 0 && len(cfg.RateLimits.TierOverrides) == 0 && len(cfg.RateLimits.ProviderOverrides) == 0 {
		if path, ok := findUpConfig(); ok {
			if data, err := os.ReadFile(path); err == nil {
				var tmp config
				if err := yaml.Unmarshal(data, &tmp); err == nil {
					cfg = tmp
					log.Printf("Loaded rate limit configuration from %s", path)
				}
			}
		}
	}
	loaded = &cfg
	initialized = true
}

func findUpConfig() (string, bool) {
	wd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for i := 0; i < 6; i++ {
		cand := filepath.Join(wd, "config", "models.yaml")
		if _, err := os.Stat(cand); err == nil {
			return cand, true
		}
		wd = filepath.Dir(wd)
	}
	return "", false
}

func get() *config {
	mu.RLock()
	if initialized {
		defer mu.RUnlock()
		return loaded
	}
	mu.RUnlock()

	mu.Lock()
	defer mu.Unlock()
	if !initialized {
		loadLocked()
	}
	return loaded
}

func LimitForTier(tier string) RateLimit {
	cfg := get()
	if cfg == nil {
		return RateLimit{}
	}
	if cfg.RateLimits.TierOverrides != nil {
		if override, ok := cfg.RateLimits.TierOverrides[strings.ToLower(strings.TrimSpace(tier))]; ok {
			return RateLimit{RPM: override.RPM, TPM: override.TPM}
		}
	}
	return RateLimit{RPM: cfg.RateLimits.DefaultRPM, TPM: cfg.RateLimits.DefaultTPM}
}

func LimitForProvider(provider string) RateLimit {
	cfg := get()
	if cfg != nil && cfg.RateLimits.ProviderOverrides != nil {
		if override, ok := cfg.RateLimits.ProviderOverrides[strings.ToLower(strings.TrimSpace(provider))]; ok {
			return RateLimit{RPM: override.RPM, TPM: override.TPM}
		}
	}
	if limit, ok := builtInProviderLimits[strings.ToLower(strings.TrimSpace(provider))]; ok {
		return limit
	}
	return RateLimit{}
}

var builtInProviderLimits = map[string]RateLimit{
	"openai":    {RPM: 30, TPM: 60000},
	"anthropic": {RPM: 20, TPM: 40000},
	"google":    {RPM: 40, TPM: 80000},
	"meta":      {RPM: 60, TPM: 120000},
	"mistral":   {RPM: 50, TPM: 100000},
	"cohere":    {RPM: 45, TPM: 90000},
	"unknown":   {RPM: 45, TPM: 90000},
}

func CombineLimits(a, b RateLimit) RateLimit {
	limit := RateLimit{}
	limit.RPM = minPositive(a.RPM, b.RPM)
	limit.TPM = minPositive(a.TPM, b.TPM)
	if limit.RPM == 0 {
		limit.RPM = max(a.RPM, b.RPM)
	}
	if limit.TPM == 0 {
		limit.TPM = max(a.TPM, b.TPM)
	}
	return limit
}

func DelayForRequest(provider, tier string, estimatedTokens int) time.Duration {
	tierLimit := LimitForTier(tier)
	providerLimit := LimitForProvider(provider)
	combined := CombineLimits(tierLimit, providerLimit)
	return delayForLimit(combined, estimatedTokens)
}

func delayForLimit(limit RateLimit, estimatedTokens int) time.Duration {
	if (limit.RPM <= 0 && limit.TPM <= 0) || estimatedTokens < 0 {
		return 0
	}
	var delayMs float64
	if limit.RPM > 0 {
		delayMs = math.Max(delayMs, 60000.0/float64(limit.RPM))
	}
	if limit.TPM > 0 && estimatedTokens > 0 {
		perToken := 60000.0 / float64(limit.TPM)
		delayMs = math.Max(delayMs, perToken*float64(estimatedTokens))
	}
	if delayMs <= 0 {
		return 0
	}
	if delayMs > 60000 {
		delayMs = 60000
	}
	return time.Duration(math.Ceil(delayMs)) * time.Millisecond
}

func minPositive(a, b int) int {
	switch {
	case a <= 0 && b <= 0:
		return 0
	case a <= 0:
		return b
	case b <= 0:
		return a
	default:
		if a < b {
			return a
		}
		return b
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func Reload() {
	mu.Lock()
	defer mu.Unlock()
	initialized = false
	loadLocked()
}
