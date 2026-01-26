// Package skills implements a markdown-based skill system compatible with
// Anthropic's Agent Skills specification.
//
// Skills are markdown files with YAML frontmatter that define reusable
// system prompts, tool requirements, and execution constraints.
package skills

import (
	"sync"
	"time"
)

// Skill represents a parsed skill definition from a markdown file.
type Skill struct {
	Name          string                 `yaml:"name" json:"name"`
	Version       string                 `yaml:"version" json:"version"`
	Author        string                 `yaml:"author" json:"author,omitempty"`
	Category      string                 `yaml:"category" json:"category"`
	Description   string                 `yaml:"description" json:"description"`
	RequiresTools []string               `yaml:"requires_tools" json:"requires_tools,omitempty"`
	RequiresRole  string                 `yaml:"requires_role" json:"requires_role,omitempty"`
	BudgetMax     int                    `yaml:"budget_max" json:"budget_max,omitempty"`
	Dangerous     bool                   `yaml:"dangerous" json:"dangerous"`
	Enabled       bool                   `yaml:"enabled" json:"enabled"`
	Metadata      map[string]interface{} `yaml:"metadata" json:"metadata,omitempty"`
	Content       string                 `yaml:"-" json:"content,omitempty"` // Markdown content after frontmatter
}

// SkillRegistry manages loaded skills with thread-safe access.
type SkillRegistry struct {
	mu         sync.RWMutex
	skills     map[string]SkillEntry   // Key: "name@version" or "name" for latest
	byCategory map[string][]string     // Category -> skill keys
	byName     map[string][]SkillEntry // Name -> all versions (sorted by version desc)
}

// SkillEntry wraps a skill with loading metadata.
type SkillEntry struct {
	Key         string
	Skill       *Skill
	SourcePath  string
	ContentHash string // SHA256 of file content
	LoadedAt    time.Time
}

// SkillSummary is a lightweight representation for API responses.
type SkillSummary struct {
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Category      string   `json:"category"`
	Description   string   `json:"description"`
	RequiresTools []string `json:"requires_tools"`
	Dangerous     bool     `json:"dangerous"`
	Enabled       bool     `json:"enabled"`
}

// SkillDetail includes full skill information for API responses.
type SkillDetail struct {
	Skill    *Skill                 `json:"skill"`
	Metadata map[string]interface{} `json:"metadata"`
}
