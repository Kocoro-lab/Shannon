package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/skills"
	"go.uber.org/zap"
)

// SkillHandler handles skill-related HTTP requests.
type SkillHandler struct {
	registry *skills.SkillRegistry
	logger   *zap.Logger
}

// NewSkillHandler creates a new skill handler.
func NewSkillHandler(registry *skills.SkillRegistry, logger *zap.Logger) *SkillHandler {
	return &SkillHandler{
		registry: registry,
		logger:   logger,
	}
}

// ListSkills handles GET /api/v1/skills
func (h *SkillHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")

	var skillList []skills.SkillSummary
	if category != "" {
		skillList = h.registry.ListByCategory(category)
	} else {
		skillList = h.registry.List()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"skills":     skillList,
		"count":      len(skillList),
		"categories": h.registry.Categories(),
	})
}

// GetSkill handles GET /api/v1/skills/{name}
func (h *SkillHandler) GetSkill(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		h.sendError(w, "Skill name is required", http.StatusBadRequest)
		return
	}

	entry, ok := h.registry.Get(name)
	if !ok {
		h.sendError(w, "Skill not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"skill": entry.Skill,
		"metadata": map[string]interface{}{
			"source_path":  entry.SourcePath,
			"content_hash": entry.ContentHash,
			"loaded_at":    entry.LoadedAt,
		},
	})
}

// GetSkillVersions handles GET /api/v1/skills/{name}/versions
func (h *SkillHandler) GetSkillVersions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		h.sendError(w, "Skill name is required", http.StatusBadRequest)
		return
	}

	versions := h.registry.GetVersions(name)
	if len(versions) == 0 {
		h.sendError(w, "Skill not found", http.StatusNotFound)
		return
	}

	// Convert to summaries
	var summaries []skills.SkillSummary
	for _, entry := range versions {
		summaries = append(summaries, skills.SkillSummary{
			Name:          entry.Skill.Name,
			Version:       entry.Skill.Version,
			Category:      entry.Skill.Category,
			Description:   entry.Skill.Description,
			RequiresTools: entry.Skill.RequiresTools,
			Dangerous:     entry.Skill.Dangerous,
			Enabled:       entry.Skill.Enabled,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"name":     name,
		"versions": summaries,
		"count":    len(summaries),
	})
}

// sendError sends an error response.
func (h *SkillHandler) sendError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
