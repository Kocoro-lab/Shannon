package skills

import (
	"strings"
	"testing"
)

func TestLoadSkill_ValidFile(t *testing.T) {
	content := `---
name: test-skill
version: 1.2.3
author: Test Author
category: testing
description: A test skill
requires_tools:
  - file_read
  - web_search
requires_role: generalist
budget_max: 5000
dangerous: false
enabled: true
metadata:
  complexity: medium
---

# Test Skill

This is the skill content.

## Instructions

Do something useful.
`

	skill, err := LoadSkill(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if skill.Name != "test-skill" {
		t.Errorf("Expected name 'test-skill', got '%s'", skill.Name)
	}
	if skill.Version != "1.2.3" {
		t.Errorf("Expected version '1.2.3', got '%s'", skill.Version)
	}
	if skill.Author != "Test Author" {
		t.Errorf("Expected author 'Test Author', got '%s'", skill.Author)
	}
	if skill.Category != "testing" {
		t.Errorf("Expected category 'testing', got '%s'", skill.Category)
	}
	if len(skill.RequiresTools) != 2 {
		t.Errorf("Expected 2 required tools, got %d", len(skill.RequiresTools))
	}
	if skill.RequiresRole != "generalist" {
		t.Errorf("Expected role 'generalist', got '%s'", skill.RequiresRole)
	}
	if skill.BudgetMax != 5000 {
		t.Errorf("Expected budget_max 5000, got %d", skill.BudgetMax)
	}
	if skill.Dangerous {
		t.Error("Expected dangerous=false")
	}
	if !strings.Contains(skill.Content, "# Test Skill") {
		t.Error("Expected content to contain '# Test Skill'")
	}
}

func TestLoadSkill_MinimalFile(t *testing.T) {
	content := `---
name: minimal
---

Some content.
`

	skill, err := LoadSkill(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if skill.Name != "minimal" {
		t.Errorf("Expected name 'minimal', got '%s'", skill.Name)
	}
	if skill.Version != "1.0.0" {
		t.Errorf("Expected default version '1.0.0', got '%s'", skill.Version)
	}
	// Enabled should default to true when not specified
	if !skill.Enabled {
		t.Error("Expected Enabled to default to true")
	}
}

func TestLoadSkill_EnabledFalse(t *testing.T) {
	content := `---
name: disabled-skill
enabled: false
---

This skill is disabled.
`

	skill, err := LoadSkill(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if skill.Enabled {
		t.Error("Expected Enabled=false when explicitly set")
	}
}

func TestLoadSkill_MissingName(t *testing.T) {
	content := `---
version: 1.0.0
---

Content without name.
`

	_, err := LoadSkill(strings.NewReader(content))
	if err == nil {
		t.Fatal("Expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("Expected 'name is required' error, got: %v", err)
	}
}

func TestLoadSkill_MissingFrontmatter(t *testing.T) {
	content := `# Not a skill

Just markdown without frontmatter.
`

	_, err := LoadSkill(strings.NewReader(content))
	if err == nil {
		t.Fatal("Expected error for missing frontmatter")
	}
	if !strings.Contains(err.Error(), "must start with YAML frontmatter") {
		t.Errorf("Expected frontmatter error, got: %v", err)
	}
}

func TestLoadSkill_UnterminatedFrontmatter(t *testing.T) {
	content := `---
name: broken
version: 1.0.0
# Missing closing ---
`

	_, err := LoadSkill(strings.NewReader(content))
	if err == nil {
		t.Fatal("Expected error for unterminated frontmatter")
	}
	if !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("Expected unterminated error, got: %v", err)
	}
}

func TestLoadSkill_EmptyContent(t *testing.T) {
	content := `---
name: empty-content
---
`

	_, err := LoadSkill(strings.NewReader(content))
	if err == nil {
		t.Fatal("Expected error for empty content")
	}
	if !strings.Contains(err.Error(), "content is empty") {
		t.Errorf("Expected 'content is empty' error, got: %v", err)
	}
}

func TestLoadSkill_InvalidNameChars(t *testing.T) {
	content := `---
name: invalid/name
---

Content.
`

	_, err := LoadSkill(strings.NewReader(content))
	if err == nil {
		t.Fatal("Expected error for invalid name characters")
	}
	if !strings.Contains(err.Error(), "invalid character") {
		t.Errorf("Expected 'invalid character' error, got: %v", err)
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.1.0", "1.0.0", 1},
		{"2.0.0", "1.9.9", 1},
		{"1.0.0", "2.0.0", -1},
	}

	for _, tt := range tests {
		result := CompareVersions(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("CompareVersions(%s, %s) = %d, expected %d", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestCalculateContentHash(t *testing.T) {
	content := []byte("test content")
	hash1 := CalculateContentHash(content)
	hash2 := CalculateContentHash(content)

	if hash1 != hash2 {
		t.Error("Same content should produce same hash")
	}

	differentContent := []byte("different content")
	hash3 := CalculateContentHash(differentContent)

	if hash1 == hash3 {
		t.Error("Different content should produce different hash")
	}

	if len(hash1) != 64 {
		t.Errorf("SHA256 hash should be 64 hex chars, got %d", len(hash1))
	}
}
