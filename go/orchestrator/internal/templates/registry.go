package templates

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	ometrics "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
)

// Registry maintains an in-memory catalogue of templates loaded from disk.
type Registry struct {
	mu        sync.RWMutex
	templates map[string]Entry

	// watcher is reserved for future hot-reload; nil in the MVP implementation.
	watcher interface{}
}

// Entry captures a loaded template alongside bookkeeping data.
type Entry struct {
	Key         string
	Template    *Template
	SourcePath  string
	ContentHash string
	LoadedAt    time.Time
}

// TemplateSummary exposes lightweight information about a registered template.
type TemplateSummary struct {
	Name        string
	Version     string
	Key         string
	ContentHash string
	SourcePath  string
}

// NewRegistry constructs an empty registry.
func NewRegistry() *Registry {
	return &Registry{templates: make(map[string]Entry)}
}

// LoadDirectory loads every YAML template under the provided directory.
func (r *Registry) LoadDirectory(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("stat template directory %s: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("template path %s is not a directory", root)
	}

	var failures []string
	walkFn := func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, walkErr))
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !isYAML(path) {
			return nil
		}
		if err := r.loadFile(path); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
		}
		return nil
	}

	if err := filepath.WalkDir(root, walkFn); err != nil {
		return fmt.Errorf("walk template directory %s: %w", root, err)
	}

	if len(failures) > 0 {
		return &LoadError{Failures: failures}
	}
	return nil
}

// Get returns the template entry that matches the supplied key.
func (r *Registry) Get(key string) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.templates[key]
	return entry, ok
}

// List summaries of all currently loaded templates.
func (r *Registry) List() []TemplateSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	summaries := make([]TemplateSummary, 0, len(r.templates))
	for _, entry := range r.templates {
		summaries = append(summaries, TemplateSummary{
			Name:        entry.Template.Name,
			Version:     entry.Template.Version,
			Key:         entry.Key,
			ContentHash: entry.ContentHash,
			SourcePath:  entry.SourcePath,
		})
	}
	sortSummaries(summaries)
	return summaries
}

func (r *Registry) loadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	tpl, err := LoadTemplate(bytes.NewReader(data))
	if err != nil {
		ometrics.TemplateValidationErrors.WithLabelValues("decode").Inc()
		return err
	}

	if len(tpl.Extends) == 0 {
		if err := ValidateTemplate(tpl); err != nil {
			if vErr, ok := err.(*ValidationError); ok {
				for _, issue := range vErr.Issues {
					ometrics.TemplateValidationErrors.WithLabelValues(issue.Code).Inc()
				}
			} else {
				ometrics.TemplateValidationErrors.WithLabelValues("validate").Inc()
			}
			return err
		}
	}

	key := MakeKey(tpl.Name, tpl.Version)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.templates[key]; exists {
		ometrics.TemplateValidationErrors.WithLabelValues("duplicate").Inc()
		return fmt.Errorf("duplicate template key '%s'", key)
	}

	hash := sha256.Sum256(data)
	entry := Entry{
		Key:         key,
		Template:    tpl,
		SourcePath:  path,
		ContentHash: hex.EncodeToString(hash[:]),
		LoadedAt:    time.Now().UTC(),
	}
	r.templates[key] = entry
	ometrics.TemplatesLoaded.WithLabelValues(tpl.Name).Inc()
	return nil
}

// MakeKey produces the canonical map key for a template name/version pair.
func MakeKey(name, version string) string {
	n := strings.TrimSpace(name)
	v := strings.TrimSpace(version)
	if v == "" {
		return n
	}
	return fmt.Sprintf("%s@%s", n, v)
}

func isYAML(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

// Find attempts to locate a template entry by name and optional version.
// When version is empty, the first matching version (sorted by name/version) is returned.
func (r *Registry) Find(name, version string) (Entry, bool) {
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if name == "" {
		return Entry{}, false
	}

	if entry, ok := r.Get(MakeKey(name, version)); ok {
		return entry, true
	}

	if version != "" {
		return Entry{}, false
	}

	summaries := r.List()
	for i := len(summaries) - 1; i >= 0; i-- {
		if summaries[i].Name == name {
			if entry, ok := r.Get(summaries[i].Key); ok {
				return entry, true
			}
		}
	}
	return Entry{}, false
}

func sortSummaries(summaries []TemplateSummary) {
	if len(summaries) < 2 {
		return
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Name == summaries[j].Name {
			return summaries[i].Version < summaries[j].Version
		}
		return summaries[i].Name < summaries[j].Name
	})
}

// Finalize resolves template inheritance/composition and re-validates the registry.
func (r *Registry) Finalize() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	resolved := make(map[string]*Template)
	visiting := make(map[string]bool)

	for key := range r.templates {
		tpl, err := r.resolveTemplateLocked(key, resolved, visiting)
		if err != nil {
			return err
		}
		if err := ValidateTemplate(tpl); err != nil {
			return fmt.Errorf("template %s validation failed after inheritance: %w", key, err)
		}
		entry := r.templates[key]
		entry.Template = tpl
		entry.Template.Extends = nil
		r.templates[key] = entry
	}

	return nil
}

func (r *Registry) resolveTemplateLocked(key string, cache map[string]*Template, visiting map[string]bool) (*Template, error) {
	if tpl, ok := cache[key]; ok {
		return cloneTemplate(tpl), nil
	}
	if visiting[key] {
		return nil, fmt.Errorf("template inheritance cycle detected for '%s'", key)
	}
	entry, ok := r.templates[key]
	if !ok {
		return nil, fmt.Errorf("template '%s' not found", key)
	}

	visiting[key] = true
	child := cloneTemplate(entry.Template)
	parents := append([]string(nil), child.Extends...)
	child.Extends = nil

	var merged *Template
	for _, parentRef := range parents {
		parentKey, err := r.lookupTemplateKeyLocked(parentRef)
		if err != nil {
			return nil, err
		}
		parentTpl, err := r.resolveTemplateLocked(parentKey, cache, visiting)
		if err != nil {
			return nil, err
		}
		if merged == nil {
			merged = parentTpl
		} else {
			merged = mergeTemplates(merged, parentTpl)
		}
	}

	var result *Template
	if merged != nil {
		result = mergeTemplates(merged, child)
	} else {
		result = child
	}

	cache[key] = cloneTemplate(result)
	visiting[key] = false
	return result, nil
}

func (r *Registry) lookupTemplateKeyLocked(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("template extends reference cannot be empty")
	}
	if strings.Contains(ref, "@") {
		if _, ok := r.templates[ref]; ok {
			return ref, nil
		}
		return "", fmt.Errorf("template '%s' referenced by extends not found", ref)
	}

	var best string
	for key := range r.templates {
		if key == ref {
			return key, nil
		}
		if strings.HasPrefix(key, ref+"@") {
			if best == "" || key > best {
				best = key
			}
		}
	}
	if best != "" {
		return best, nil
	}
	return "", fmt.Errorf("template '%s' referenced by extends not found", ref)
}

func mergeTemplates(base, overlay *Template) *Template {
	result := cloneTemplate(base)

	if overlay.Defaults.ModelTier != "" {
		result.Defaults.ModelTier = overlay.Defaults.ModelTier
	}
	if overlay.Defaults.BudgetAgentMax != 0 {
		result.Defaults.BudgetAgentMax = overlay.Defaults.BudgetAgentMax
	}
	if overlay.Defaults.RequireApproval != nil {
		val := *overlay.Defaults.RequireApproval
		result.Defaults.RequireApproval = &val
	}

	if len(overlay.Metadata) > 0 {
		if result.Metadata == nil {
			result.Metadata = make(map[string]any, len(overlay.Metadata))
		}
		for k, v := range overlay.Metadata {
			result.Metadata[k] = v
		}
	}

	nodeIndex := make(map[string]int, len(result.Nodes))
	for i := range result.Nodes {
		nodeIndex[result.Nodes[i].ID] = i
	}
	for _, node := range overlay.Nodes {
		if idx, ok := nodeIndex[node.ID]; ok {
			result.Nodes[idx] = mergeTemplateNode(result.Nodes[idx], node)
		} else {
			result.Nodes = append(result.Nodes, cloneTemplateNode(node))
		}
	}

	if len(overlay.Edges) > 0 {
		result.Edges = cloneEdges(overlay.Edges)
	}

	return result
}

func mergeTemplateNode(base, overlay TemplateNode) TemplateNode {
	merged := cloneTemplateNode(base)

	if overlay.Type != "" {
		merged.Type = overlay.Type
	}
	if overlay.Strategy != "" {
		merged.Strategy = overlay.Strategy
	}
	if len(overlay.DependsOn) > 0 {
		merged.DependsOn = cloneStringSlice(overlay.DependsOn)
	}
	if overlay.BudgetMax != nil {
		val := *overlay.BudgetMax
		merged.BudgetMax = &val
	}
	if len(overlay.ToolsAllowlist) > 0 {
		merged.ToolsAllowlist = cloneStringSlice(overlay.ToolsAllowlist)
	}
	if overlay.OnFail != nil {
		clone := *overlay.OnFail
		merged.OnFail = &clone
	}
	if len(overlay.Metadata) > 0 {
		if merged.Metadata == nil {
			merged.Metadata = make(map[string]any, len(overlay.Metadata))
		}
		for k, v := range overlay.Metadata {
			merged.Metadata[k] = v
		}
	}

	return merged
}

func cloneTemplate(tpl *Template) *Template {
	if tpl == nil {
		return nil
	}
	clone := *tpl
	clone.Extends = cloneStringSlice(tpl.Extends)
	clone.Metadata = cloneMetadata(tpl.Metadata)
	if tpl.Defaults.RequireApproval != nil {
		val := *tpl.Defaults.RequireApproval
		clone.Defaults.RequireApproval = &val
	}
	clone.Nodes = make([]TemplateNode, len(tpl.Nodes))
	for i := range tpl.Nodes {
		clone.Nodes[i] = cloneTemplateNode(tpl.Nodes[i])
	}
	clone.Edges = cloneEdges(tpl.Edges)
	return &clone
}

func cloneTemplateNode(node TemplateNode) TemplateNode {
	clone := node
	clone.DependsOn = cloneStringSlice(node.DependsOn)
	clone.ToolsAllowlist = cloneStringSlice(node.ToolsAllowlist)
	clone.Metadata = cloneMetadata(node.Metadata)
	if node.BudgetMax != nil {
		val := *node.BudgetMax
		clone.BudgetMax = &val
	}
	if node.OnFail != nil {
		c := *node.OnFail
		clone.OnFail = &c
	}
	return clone
}

func cloneEdges(edges []TemplateEdge) []TemplateEdge {
	if len(edges) == 0 {
		return nil
	}
	out := make([]TemplateEdge, len(edges))
	copy(out, edges)
	return out
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func cloneMetadata(m map[string]interface{}) map[string]interface{} {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// LoadError aggregates template loading failures.
type LoadError struct {
	Failures []string
}

// Error implements the error interface.
func (e *LoadError) Error() string {
	if len(e.Failures) == 0 {
		return "template load failed"
	}
	return fmt.Sprintf("%d template(s) failed to load: %s", len(e.Failures), strings.Join(e.Failures, "; "))
}

// IsLoadError returns true when err represents aggregated template load failures.
func IsLoadError(err error) bool {
	_, ok := err.(*LoadError)
	return ok
}
