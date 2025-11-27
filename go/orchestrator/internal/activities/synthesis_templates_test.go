package activities

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectSynthesisTemplate(t *testing.T) {
	tests := []struct {
		name         string
		context      map[string]interface{}
		wantTemplate string
		wantExplicit bool
	}{
		{
			name:         "nil context returns normal_default",
			context:      nil,
			wantTemplate: "normal_default",
			wantExplicit: false,
		},
		{
			name:         "empty context returns normal_default",
			context:      map[string]interface{}{},
			wantTemplate: "normal_default",
			wantExplicit: false,
		},
		{
			name: "explicit synthesis_template",
			context: map[string]interface{}{
				"synthesis_template": "my_custom",
			},
			wantTemplate: "my_custom",
			wantExplicit: true,
		},
		{
			name: "empty synthesis_template falls through",
			context: map[string]interface{}{
				"synthesis_template": "",
			},
			wantTemplate: "normal_default",
			wantExplicit: false,
		},
		{
			name: "workflow_type research",
			context: map[string]interface{}{
				"workflow_type": "research",
			},
			wantTemplate: "research_comprehensive",
			wantExplicit: false,
		},
		{
			name: "force_research true",
			context: map[string]interface{}{
				"force_research": true,
			},
			wantTemplate: "research_comprehensive",
			wantExplicit: false,
		},
		{
			name: "force_research false falls through",
			context: map[string]interface{}{
				"force_research": false,
			},
			wantTemplate: "normal_default",
			wantExplicit: false,
		},
		{
			name: "synthesis_style comprehensive",
			context: map[string]interface{}{
				"synthesis_style": "comprehensive",
			},
			wantTemplate: "research_comprehensive",
			wantExplicit: false,
		},
		{
			name: "synthesis_style concise",
			context: map[string]interface{}{
				"synthesis_style": "concise",
			},
			wantTemplate: "research_concise",
			wantExplicit: false,
		},
		{
			name: "research_areas []string present",
			context: map[string]interface{}{
				"research_areas": []string{"area1", "area2"},
			},
			wantTemplate: "research_comprehensive",
			wantExplicit: false,
		},
		{
			name: "research_areas []interface{} present",
			context: map[string]interface{}{
				"research_areas": []interface{}{"area1", "area2"},
			},
			wantTemplate: "research_comprehensive",
			wantExplicit: false,
		},
		{
			name: "research_areas empty []string",
			context: map[string]interface{}{
				"research_areas": []string{},
			},
			wantTemplate: "normal_default",
			wantExplicit: false,
		},
		{
			name: "explicit template takes precedence over workflow_type",
			context: map[string]interface{}{
				"synthesis_template": "custom",
				"workflow_type":      "research",
			},
			wantTemplate: "custom",
			wantExplicit: true,
		},
		{
			name: "workflow_type takes precedence over force_research",
			context: map[string]interface{}{
				"workflow_type":  "research",
				"force_research": false,
			},
			wantTemplate: "research_comprehensive",
			wantExplicit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTemplate, gotExplicit := SelectSynthesisTemplate(tt.context)
			if gotTemplate != tt.wantTemplate {
				t.Errorf("SelectSynthesisTemplate() template = %q, want %q", gotTemplate, tt.wantTemplate)
			}
			if gotExplicit != tt.wantExplicit {
				t.Errorf("SelectSynthesisTemplate() explicit = %v, want %v", gotExplicit, tt.wantExplicit)
			}
		})
	}
}

func TestLoadSynthesisTemplate(t *testing.T) {
	// Create temp directory for test templates
	tmpDir, err := os.MkdirTemp("", "synthesis-templates-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Clear cache before test
	ClearSynthesisTemplateCache()

	// Set env var to use temp dir
	oldEnv := os.Getenv("SYNTHESIS_TEMPLATES_DIR")
	os.Setenv("SYNTHESIS_TEMPLATES_DIR", tmpDir)
	defer os.Setenv("SYNTHESIS_TEMPLATES_DIR", oldEnv)

	t.Run("returns nil for non-existent template", func(t *testing.T) {
		ClearSynthesisTemplateCache()
		tmpl := LoadSynthesisTemplate("nonexistent", nil)
		if tmpl != nil {
			t.Error("expected nil for non-existent template")
		}
	})

	t.Run("loads template without base", func(t *testing.T) {
		ClearSynthesisTemplateCache()

		// Create a simple template
		content := `Hello {{ .Query }}`
		if err := os.WriteFile(filepath.Join(tmpDir, "simple.tmpl"), []byte(content), 0644); err != nil {
			t.Fatalf("failed to write template: %v", err)
		}

		tmpl := LoadSynthesisTemplate("simple", nil)
		if tmpl == nil {
			t.Fatal("expected template, got nil")
		}
	})

	t.Run("loads template with base", func(t *testing.T) {
		ClearSynthesisTemplateCache()

		// Create base template
		baseContent := `{{- define "greeting" -}}Hello{{- end -}}`
		if err := os.WriteFile(filepath.Join(tmpDir, "_base.tmpl"), []byte(baseContent), 0644); err != nil {
			t.Fatalf("failed to write base template: %v", err)
		}

		// Create template that uses base
		namedContent := `{{ template "greeting" }} {{ .Query }}`
		if err := os.WriteFile(filepath.Join(tmpDir, "with_base.tmpl"), []byte(namedContent), 0644); err != nil {
			t.Fatalf("failed to write named template: %v", err)
		}

		tmpl := LoadSynthesisTemplate("with_base", nil)
		if tmpl == nil {
			t.Fatal("expected template, got nil")
		}
	})

	t.Run("caches loaded templates", func(t *testing.T) {
		ClearSynthesisTemplateCache()

		content := `Cached {{ .Query }}`
		if err := os.WriteFile(filepath.Join(tmpDir, "cached.tmpl"), []byte(content), 0644); err != nil {
			t.Fatalf("failed to write template: %v", err)
		}

		// First load
		tmpl1 := LoadSynthesisTemplate("cached", nil)
		if tmpl1 == nil {
			t.Fatal("expected template, got nil")
		}

		// Second load should return cached version
		tmpl2 := LoadSynthesisTemplate("cached", nil)
		if tmpl2 == nil {
			t.Fatal("expected cached template, got nil")
		}

		// Both should be the same pointer
		if tmpl1 != tmpl2 {
			t.Error("expected same cached template instance")
		}
	})

	t.Run("returns nil for invalid template syntax", func(t *testing.T) {
		ClearSynthesisTemplateCache()

		content := `{{ .Invalid syntax here`
		if err := os.WriteFile(filepath.Join(tmpDir, "invalid.tmpl"), []byte(content), 0644); err != nil {
			t.Fatalf("failed to write template: %v", err)
		}

		tmpl := LoadSynthesisTemplate("invalid", nil)
		if tmpl != nil {
			t.Error("expected nil for invalid template syntax")
		}
	})
}

func TestRenderSynthesisTemplate(t *testing.T) {
	// Create temp directory for test templates
	tmpDir, err := os.MkdirTemp("", "synthesis-render-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ClearSynthesisTemplateCache()

	oldEnv := os.Getenv("SYNTHESIS_TEMPLATES_DIR")
	os.Setenv("SYNTHESIS_TEMPLATES_DIR", tmpDir)
	defer os.Setenv("SYNTHESIS_TEMPLATES_DIR", oldEnv)

	t.Run("returns error for nil template", func(t *testing.T) {
		_, err := RenderSynthesisTemplate(nil, SynthesisTemplateData{})
		if err == nil {
			t.Error("expected error for nil template")
		}
	})

	t.Run("renders template with data", func(t *testing.T) {
		content := `Query: {{ .Query }}
Language: {{ .QueryLanguage }}
Areas: {{ len .ResearchAreas }}
Citations: {{ .CitationCount }}
Research: {{ .IsResearch }}`

		if err := os.WriteFile(filepath.Join(tmpDir, "render_test.tmpl"), []byte(content), 0644); err != nil {
			t.Fatalf("failed to write template: %v", err)
		}

		tmpl := LoadSynthesisTemplate("render_test", nil)
		if tmpl == nil {
			t.Fatal("failed to load template")
		}

		data := SynthesisTemplateData{
			Query:          "test query",
			QueryLanguage:  "en",
			ResearchAreas:  []string{"area1", "area2", "area3"},
			CitationCount:  5,
			IsResearch:     true,
		}

		result, err := RenderSynthesisTemplate(tmpl, data)
		if err != nil {
			t.Fatalf("render error: %v", err)
		}

		expected := `Query: test query
Language: en
Areas: 3
Citations: 5
Research: true`

		if result != expected {
			t.Errorf("render result = %q, want %q", result, expected)
		}
	})

	t.Run("template functions work", func(t *testing.T) {
		content := `add: {{ add 2 3 }}
sub: {{ sub 10 4 }}
mul: {{ mul 3 4 }}
gt: {{ gt 5 3 }}
lt: {{ lt 3 5 }}`

		if err := os.WriteFile(filepath.Join(tmpDir, "funcs_test.tmpl"), []byte(content), 0644); err != nil {
			t.Fatalf("failed to write template: %v", err)
		}

		tmpl := LoadSynthesisTemplate("funcs_test", nil)
		if tmpl == nil {
			t.Fatal("failed to load template")
		}

		result, err := RenderSynthesisTemplate(tmpl, SynthesisTemplateData{})
		if err != nil {
			t.Fatalf("render error: %v", err)
		}

		expected := `add: 5
sub: 6
mul: 12
gt: true
lt: true`

		if result != expected {
			t.Errorf("render result = %q, want %q", result, expected)
		}
	})
}

func TestClearSynthesisTemplateCache(t *testing.T) {
	// Create temp directory for test templates
	tmpDir, err := os.MkdirTemp("", "synthesis-cache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	oldEnv := os.Getenv("SYNTHESIS_TEMPLATES_DIR")
	os.Setenv("SYNTHESIS_TEMPLATES_DIR", tmpDir)
	defer os.Setenv("SYNTHESIS_TEMPLATES_DIR", oldEnv)

	// Create template
	content := `Test {{ .Query }}`
	if err := os.WriteFile(filepath.Join(tmpDir, "cache_test.tmpl"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	ClearSynthesisTemplateCache()

	// Load template to populate cache
	tmpl1 := LoadSynthesisTemplate("cache_test", nil)
	if tmpl1 == nil {
		t.Fatal("failed to load template")
	}

	// Clear cache
	ClearSynthesisTemplateCache()

	// Modify the template file
	newContent := `Modified {{ .Query }}`
	if err := os.WriteFile(filepath.Join(tmpDir, "cache_test.tmpl"), []byte(newContent), 0644); err != nil {
		t.Fatalf("failed to write modified template: %v", err)
	}

	// Load again - should get new version
	tmpl2 := LoadSynthesisTemplate("cache_test", nil)
	if tmpl2 == nil {
		t.Fatal("failed to load modified template")
	}

	// Verify they are different instances (cache was cleared)
	if tmpl1 == tmpl2 {
		t.Error("expected different template instances after cache clear")
	}
}

func TestGetSynthesisTemplatesDir(t *testing.T) {
	t.Run("returns env var when set", func(t *testing.T) {
		oldEnv := os.Getenv("SYNTHESIS_TEMPLATES_DIR")
		os.Setenv("SYNTHESIS_TEMPLATES_DIR", "/custom/path")
		defer os.Setenv("SYNTHESIS_TEMPLATES_DIR", oldEnv)

		dir := getSynthesisTemplatesDir()
		if dir != "/custom/path" {
			t.Errorf("got %q, want /custom/path", dir)
		}
	})

	t.Run("returns default when env not set", func(t *testing.T) {
		oldEnv := os.Getenv("SYNTHESIS_TEMPLATES_DIR")
		os.Unsetenv("SYNTHESIS_TEMPLATES_DIR")
		defer os.Setenv("SYNTHESIS_TEMPLATES_DIR", oldEnv)

		dir := getSynthesisTemplatesDir()
		if dir != "config/templates/synthesis" {
			t.Errorf("got %q, want config/templates/synthesis", dir)
		}
	})
}
