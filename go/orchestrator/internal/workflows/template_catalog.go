package workflows

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/templates"
	"go.uber.org/zap"
)

var (
	templateRegistry     *templates.Registry
	templateRegistryLock sync.RWMutex
)

// InitTemplateRegistry loads templates from the provided directories and stores the registry for workflow access.
func InitTemplateRegistry(logger *zap.Logger, dirs ...string) (*templates.Registry, error) {
	reg := templates.NewRegistry()

	var failures []string
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		info, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				if logger != nil {
					logger.Debug("template directory not found, skipping", zap.String("path", dir))
				}
				continue
			}
			failures = append(failures, fmt.Sprintf("%s: %v", dir, err))
			continue
		}
		if !info.IsDir() {
			failures = append(failures, fmt.Sprintf("%s: not a directory", dir))
			continue
		}
		if err := reg.LoadDirectory(dir); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", dir, err))
		} else if logger != nil {
			logger.Info("templates loaded", zap.String("path", dir), zap.Int("count", len(reg.List())))
		}
	}

	if err := reg.Finalize(); err != nil {
		failures = append(failures, err.Error())
	}

	templateRegistryLock.Lock()
	templateRegistry = reg
	templateRegistryLock.Unlock()

	if len(failures) > 0 {
		return reg, fmt.Errorf("template load errors: %s", strings.Join(failures, "; "))
	}
	return reg, nil
}

// TemplateRegistry returns the currently initialised registry (may be empty).
func TemplateRegistry() *templates.Registry {
	templateRegistryLock.RLock()
	if templateRegistry != nil {
		reg := templateRegistry
		templateRegistryLock.RUnlock()
		return reg
	}
	templateRegistryLock.RUnlock()

	templateRegistryLock.Lock()
	defer templateRegistryLock.Unlock()
	if templateRegistry == nil {
		templateRegistry = templates.NewRegistry()
	}
	return templateRegistry
}
