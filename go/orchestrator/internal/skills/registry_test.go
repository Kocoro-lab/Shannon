package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if r.Count() != 0 {
		t.Errorf("New registry should be empty, got count %d", r.Count())
	}
}

func TestRegistryLoadDirectory(t *testing.T) {
	// Create a temporary directory with test skills
	tmpDir, err := os.MkdirTemp("", "skills-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test skill files
	skill1 := `---
name: skill-one
version: 1.0.0
category: testing
description: First test skill
---

# Skill One

Content for skill one.
`
	skill2 := `---
name: skill-two
version: 2.0.0
category: testing
description: Second test skill
---

# Skill Two

Content for skill two.
`
	skill3 := `---
name: skill-three
version: 1.0.0
category: other
description: Third test skill
---

# Skill Three

Content for skill three.
`

	if err := os.WriteFile(filepath.Join(tmpDir, "skill-one.md"), []byte(skill1), 0644); err != nil {
		t.Fatalf("Failed to write skill1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "skill-two.md"), []byte(skill2), 0644); err != nil {
		t.Fatalf("Failed to write skill2: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "skill-three.md"), []byte(skill3), 0644); err != nil {
		t.Fatalf("Failed to write skill3: %v", err)
	}

	// Load skills
	r := NewRegistry()
	if err := r.LoadDirectory(tmpDir); err != nil {
		t.Fatalf("LoadDirectory failed: %v", err)
	}

	if r.Count() != 3 {
		t.Errorf("Expected 3 skills, got %d", r.Count())
	}

	// Test Get by name
	entry, ok := r.Get("skill-one")
	if !ok {
		t.Error("Failed to get skill-one")
	}
	if entry.Skill.Name != "skill-one" {
		t.Errorf("Expected name 'skill-one', got '%s'", entry.Skill.Name)
	}

	// Test Get by versioned key
	entry, ok = r.Get("skill-two@2.0.0")
	if !ok {
		t.Error("Failed to get skill-two@2.0.0")
	}
	if entry.Skill.Version != "2.0.0" {
		t.Errorf("Expected version '2.0.0', got '%s'", entry.Skill.Version)
	}

	// Test List
	list := r.List()
	if len(list) != 3 {
		t.Errorf("Expected 3 skills in list, got %d", len(list))
	}

	// Test ListByCategory
	testingSkills := r.ListByCategory("testing")
	if len(testingSkills) != 2 {
		t.Errorf("Expected 2 skills in 'testing' category, got %d", len(testingSkills))
	}

	otherSkills := r.ListByCategory("other")
	if len(otherSkills) != 1 {
		t.Errorf("Expected 1 skill in 'other' category, got %d", len(otherSkills))
	}

	// Test Categories
	categories := r.Categories()
	if len(categories) != 2 {
		t.Errorf("Expected 2 categories, got %d", len(categories))
	}
}

func TestRegistryLoadDirectory_NonExistent(t *testing.T) {
	r := NewRegistry()
	// Should not error for non-existent directory (silently skip)
	err := r.LoadDirectory("/nonexistent/path/12345")
	if err != nil {
		t.Errorf("Expected no error for non-existent dir, got: %v", err)
	}
	if r.Count() != 0 {
		t.Error("Registry should be empty after loading non-existent dir")
	}
}

func TestRegistryLoadDirectory_SkipsReadme(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a README.md file
	readme := "# This is a README\n\nNot a skill."
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte(readme), 0644); err != nil {
		t.Fatalf("Failed to write README: %v", err)
	}

	r := NewRegistry()
	if err := r.LoadDirectory(tmpDir); err != nil {
		t.Fatalf("LoadDirectory failed: %v", err)
	}

	if r.Count() != 0 {
		t.Errorf("Registry should skip README.md, got count %d", r.Count())
	}
}

func TestRegistryMultipleVersions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create multiple versions of same skill
	v1 := `---
name: versioned-skill
version: 1.0.0
category: testing
description: Version 1
---

Content v1.
`
	v2 := `---
name: versioned-skill
version: 2.0.0
category: testing
description: Version 2
---

Content v2.
`

	if err := os.WriteFile(filepath.Join(tmpDir, "skill-v1.md"), []byte(v1), 0644); err != nil {
		t.Fatalf("Failed to write v1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "skill-v2.md"), []byte(v2), 0644); err != nil {
		t.Fatalf("Failed to write v2: %v", err)
	}

	r := NewRegistry()
	if err := r.LoadDirectory(tmpDir); err != nil {
		t.Fatalf("LoadDirectory failed: %v", err)
	}

	// Count should be 1 (unique names)
	if r.Count() != 1 {
		t.Errorf("Expected 1 unique skill, got %d", r.Count())
	}

	// Get by name should return latest version
	entry, ok := r.Get("versioned-skill")
	if !ok {
		t.Fatal("Failed to get versioned-skill")
	}
	if entry.Skill.Version != "2.0.0" {
		t.Errorf("Expected latest version '2.0.0', got '%s'", entry.Skill.Version)
	}

	// Can still get specific version
	entry, ok = r.Get("versioned-skill@1.0.0")
	if !ok {
		t.Fatal("Failed to get versioned-skill@1.0.0")
	}
	if entry.Skill.Version != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got '%s'", entry.Skill.Version)
	}

	// GetVersions should return all versions
	versions := r.GetVersions("versioned-skill")
	if len(versions) != 2 {
		t.Errorf("Expected 2 versions, got %d", len(versions))
	}
}

func TestRegistryFinalize_DangerousWithoutDescription(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create dangerous skill without description
	dangerous := `---
name: dangerous-skill
version: 1.0.0
dangerous: true
---

Dangerous content.
`
	if err := os.WriteFile(filepath.Join(tmpDir, "dangerous.md"), []byte(dangerous), 0644); err != nil {
		t.Fatalf("Failed to write dangerous skill: %v", err)
	}

	r := NewRegistry()
	if err := r.LoadDirectory(tmpDir); err != nil {
		t.Fatalf("LoadDirectory failed: %v", err)
	}

	err = r.Finalize()
	if err == nil {
		t.Fatal("Expected Finalize to fail for dangerous skill without description")
	}
	if !contains(err.Error(), "must have a description") {
		t.Errorf("Expected 'must have a description' error, got: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
