package activities

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadWorkspaceFiles(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "test-session-123")
	for _, d := range []string{"findings", "data", "synthesis"} {
		os.MkdirAll(filepath.Join(sessionDir, d), 0755)
	}
	os.WriteFile(filepath.Join(sessionDir, "findings", "agent-a.md"),
		[]byte("# AWS Pricing\n- EC2: $0.10/hr\n- S3: $0.023/GB"), 0644)
	os.WriteFile(filepath.Join(sessionDir, "findings", "agent-b.md"),
		[]byte("# Azure Pricing\n- VM: $0.12/hr\n- Blob: $0.018/GB"), 0644)
	os.WriteFile(filepath.Join(sessionDir, "data", "comparison.csv"),
		[]byte("provider,compute,storage\nAWS,0.10,0.023\nAzure,0.12,0.018"), 0644)

	materials := readWorkspaceFiles(sessionDir, 50000, 10000)
	if len(materials) != 3 {
		t.Fatalf("expected 3 files, got %d", len(materials))
	}
	// Should be sorted by path (data/ before findings/)
	if materials[0].Path != "data/comparison.csv" {
		t.Errorf("expected first file data/comparison.csv, got %s", materials[0].Path)
	}
	if materials[0].Content == "" {
		t.Error("expected non-empty content")
	}
}

func TestReadWorkspaceFiles_Truncation(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "test-trunc")
	os.MkdirAll(filepath.Join(sessionDir, "findings"), 0755)
	bigContent := make([]byte, 20000)
	for i := range bigContent {
		bigContent[i] = 'A'
	}
	os.WriteFile(filepath.Join(sessionDir, "findings", "big.txt"), bigContent, 0644)

	materials := readWorkspaceFiles(sessionDir, 50000, 5000)
	if len(materials) != 1 {
		t.Fatalf("expected 1 file, got %d", len(materials))
	}
	if !materials[0].Truncated {
		t.Error("expected truncated=true")
	}
	if len(materials[0].Content) > 5100 {
		t.Errorf("content too long: %d", len(materials[0].Content))
	}
}

func TestReadWorkspaceFiles_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "empty")
	os.MkdirAll(sessionDir, 0755)
	materials := readWorkspaceFiles(sessionDir, 50000, 10000)
	if len(materials) != 0 {
		t.Errorf("expected 0 files, got %d", len(materials))
	}
}

func TestReadWorkspaceFiles_SkipsBinary(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "bin-test")
	os.MkdirAll(filepath.Join(sessionDir, "data"), 0755)
	os.WriteFile(filepath.Join(sessionDir, "data", "image.png"), []byte{0x89, 0x50, 0x4E, 0x47}, 0644)
	os.WriteFile(filepath.Join(sessionDir, "data", "notes.txt"), []byte("hello"), 0644)

	materials := readWorkspaceFiles(sessionDir, 50000, 10000)
	if len(materials) != 1 {
		t.Fatalf("expected 1 file (skip binary), got %d", len(materials))
	}
	if materials[0].Path != "data/notes.txt" {
		t.Errorf("expected notes.txt, got %s", materials[0].Path)
	}
}

func TestReadWorkspaceFiles_MaxTotal(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "cap-test")
	os.MkdirAll(filepath.Join(sessionDir, "findings"), 0755)
	for i := 0; i < 5; i++ {
		content := make([]byte, 3000)
		for j := range content {
			content[j] = byte('A' + i)
		}
		os.WriteFile(filepath.Join(sessionDir, "findings",
			string(rune('a'+i))+".txt"), content, 0644)
	}
	materials := readWorkspaceFiles(sessionDir, 8000, 10000)
	totalChars := 0
	for _, m := range materials {
		totalChars += len(m.Content)
	}
	if totalChars > 8500 {
		t.Errorf("total chars %d exceeds maxTotal 8000", totalChars)
	}
}

func TestReadWorkspaceFiles_NoSessionDir(t *testing.T) {
	materials := readWorkspaceFiles("/nonexistent/path/xyz", 50000, 10000)
	if len(materials) != 0 {
		t.Errorf("expected 0 files, got %d", len(materials))
	}
}

func TestFormatWorkspaceMaterials(t *testing.T) {
	materials := []WorkspaceMaterial{
		{Path: "findings/aws.md", Content: "# AWS\nEC2 pricing: $0.10/hr"},
		{Path: "data/comparison.csv", Content: "provider,price\nAWS,0.10", Truncated: true},
	}
	result := formatWorkspaceMaterials(materials)
	if !strings.Contains(result, "findings/aws.md") {
		t.Error("should contain file path")
	}
	if !strings.Contains(result, "EC2 pricing") {
		t.Error("should contain file content")
	}
	if !strings.Contains(result, "[truncated]") {
		t.Error("should indicate truncation")
	}
}

func TestFormatWorkspaceMaterials_Empty(t *testing.T) {
	result := formatWorkspaceMaterials(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}
