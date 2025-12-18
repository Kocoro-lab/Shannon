package activities

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"text/template"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/util"
	"go.uber.org/zap"
)

// SynthesisTemplateData contains all variables available to synthesis templates.
type SynthesisTemplateData struct {
	Query               string
	QueryLanguage       string
	ResearchAreas       []string
	AvailableCitations  string
	CitationCount       int
	MinCitations        int
	LanguageInstruction string
	AgentResults        string
	TargetWords         int
	IsResearch          bool
	SynthesisStyle      string
	CitationAgentEnabled bool // When true, synthesis should NOT add [n] markers - Citation Agent handles it
}

// synthesisTemplateCache caches loaded templates for performance.
var (
	synthesisTemplateCache = make(map[string]*template.Template)
	synthesisTemplateMutex sync.RWMutex
)

// Default template directory - can be overridden via SYNTHESIS_TEMPLATES_DIR env var.
func getSynthesisTemplatesDir() string {
	if dir := os.Getenv("SYNTHESIS_TEMPLATES_DIR"); dir != "" {
		return dir
	}
	// Default path relative to working directory
	return "config/templates/synthesis"
}

// templateFuncs provides helper functions available in templates.
var templateFuncs = template.FuncMap{
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
	"mul": func(a, b int) int { return a * b },
	"len": func(s interface{}) int {
		switch v := s.(type) {
		case []string:
			return len(v)
		case string:
			return len(v)
		default:
			return 0
		}
	},
	"gt": func(a, b int) bool { return a > b },
	"lt": func(a, b int) bool { return a < b },
}

// SynthesisLogger is an interface for logging in synthesis templates.
// It accepts both zap.Logger and Temporal workflow loggers.
type SynthesisLogger interface {
	Info(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Debug(msg string, fields ...interface{})
}

// LoadSynthesisTemplate loads a synthesis template by name.
// It first checks the cache, then loads from disk if not cached.
// Returns nil if the template doesn't exist (caller should use fallback).
// logger can be nil if logging is not needed.
//
// IMPORTANT: Temporal Determinism Warning
// This function reads from the filesystem on cache miss. It is safe to call
// from Temporal ACTIVITY code (like SynthesizeResultsLLM), but must NOT be
// called directly from WORKFLOW code as filesystem access breaks determinism
// and would cause replay failures. The caching mitigates repeated disk reads.
func LoadSynthesisTemplate(name string, logger *zap.Logger) *template.Template {
	// Check cache first
	synthesisTemplateMutex.RLock()
	if tmpl, ok := synthesisTemplateCache[name]; ok {
		synthesisTemplateMutex.RUnlock()
		return tmpl
	}
	synthesisTemplateMutex.RUnlock()

	// Load from disk
	dir := getSynthesisTemplatesDir()

	// Load base template first
	basePath := filepath.Join(dir, "_base.tmpl")
	namedPath := filepath.Join(dir, name+".tmpl")

	// Check if files exist
	if _, err := os.Stat(namedPath); os.IsNotExist(err) {
		if logger != nil {
			logger.Debug("Synthesis template not found", zap.String("name", name), zap.String("path", namedPath))
		}
		return nil
	}

	// Create template with base + named
	tmpl := template.New(name).Funcs(templateFuncs)

	// Parse base template if it exists
	if _, err := os.Stat(basePath); err == nil {
		baseContent, err := os.ReadFile(basePath)
		if err != nil {
			if logger != nil {
				logger.Warn("Failed to read base template", zap.Error(err))
			}
		} else {
			if _, err := tmpl.Parse(string(baseContent)); err != nil {
				if logger != nil {
					logger.Warn("Failed to parse base template", zap.Error(err))
				}
			}
		}
	}

	// Parse named template
	namedContent, err := os.ReadFile(namedPath)
	if err != nil {
		if logger != nil {
			logger.Warn("Failed to read synthesis template", zap.String("name", name), zap.Error(err))
		}
		return nil
	}

	if _, err := tmpl.Parse(string(namedContent)); err != nil {
		if logger != nil {
			logger.Warn("Failed to parse synthesis template", zap.String("name", name), zap.Error(err))
		}
		return nil
	}

	// Cache for future use
	synthesisTemplateMutex.Lock()
	synthesisTemplateCache[name] = tmpl
	synthesisTemplateMutex.Unlock()

	if logger != nil {
		logger.Info("Loaded synthesis template", zap.String("name", name))
	}

	return tmpl
}

// RenderSynthesisTemplate renders a synthesis template with the given data.
// Returns rendered string and nil error on success.
// Returns empty string and error if rendering fails.
func RenderSynthesisTemplate(tmpl *template.Template, data SynthesisTemplateData) (string, error) {
	if tmpl == nil {
		return "", fmt.Errorf("template is nil")
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render template: %w", err)
	}

	return buf.String(), nil
}

// SelectSynthesisTemplate determines which template to use based on context.
// Returns template name and whether it was explicitly specified.
//
// Selection priority:
// 1. context["synthesis_template"] - explicit template name
// 2. context["workflow_type"] == "research" -> research_comprehensive
// 3. context["force_research"] == true -> research_comprehensive
// 4. context["synthesis_style"] == "comprehensive" -> research_comprehensive
// 5. context["synthesis_style"] == "concise" -> research_concise
// 6. len(research_areas) > 0 -> research_comprehensive
// 7. Default -> normal_default
func SelectSynthesisTemplate(context map[string]interface{}) (templateName string, explicit bool) {
	if context == nil {
		return "normal_default", false
	}

	// 1. Explicit template specification
	if name, ok := context["synthesis_template"].(string); ok && name != "" {
		return name, true
	}

	// 2. Workflow type
	if wfType, ok := context["workflow_type"].(string); ok && wfType == "research" {
		return "research_comprehensive", false
	}

	// 3. Force research flag (handles both bool and string "true" from proto)
	if util.GetContextBool(context, "force_research") {
		return "research_comprehensive", false
	}

	// 4-5. Synthesis style
	if style, ok := context["synthesis_style"].(string); ok {
		switch style {
		case "comprehensive":
			return "research_comprehensive", false
		case "concise":
			return "research_concise", false
		}
	}

	// 6. Research areas presence
	if rawAreas, ok := context["research_areas"]; ok && rawAreas != nil {
		switch t := rawAreas.(type) {
		case []string:
			if len(t) > 0 {
				return "research_comprehensive", false
			}
		case []interface{}:
			if len(t) > 0 {
				return "research_comprehensive", false
			}
		}
	}

	// 7. Default
	return "normal_default", false
}

// ClearSynthesisTemplateCache clears the template cache (useful for testing or hot-reload).
func ClearSynthesisTemplateCache() {
	synthesisTemplateMutex.Lock()
	synthesisTemplateCache = make(map[string]*template.Template)
	synthesisTemplateMutex.Unlock()
}
