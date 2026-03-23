package activities

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadWorkspaceFile(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "test-session")
	os.MkdirAll(filepath.Join(sessionDir, "research"), 0o755)
	os.WriteFile(filepath.Join(sessionDir, "research", "report.md"), []byte("# Report\nKey finding: React is 22% larger"), 0o644)

	// Normal read
	result := readFileContent("test-session", "research/report.md", dir, 4000)
	if result.Content == "" {
		t.Fatal("expected content, got empty")
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Content, "React is 22% larger") {
		t.Errorf("content missing expected text: %s", result.Content)
	}
}

func TestReadWorkspaceFileTruncation(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "test-session")
	os.MkdirAll(sessionDir, 0o755)
	bigContent := make([]byte, 10000)
	for i := range bigContent {
		bigContent[i] = 'x'
	}
	os.WriteFile(filepath.Join(sessionDir, "big.txt"), bigContent, 0o644)

	result := readFileContent("test-session", "big.txt", dir, 100)
	if len(result.Content) != 100 {
		t.Errorf("expected truncated to 100, got %d", len(result.Content))
	}
	if !result.Truncated {
		t.Error("expected Truncated=true")
	}
}

func TestReadWorkspaceFileTraversal(t *testing.T) {
	result := readFileContent("test-session", "../../../etc/passwd", "/tmp", 4000)
	if result.Error == "" {
		t.Error("expected error for path traversal")
	}
}

func TestReadWorkspaceFileAbsPath(t *testing.T) {
	result := readFileContent("test-session", "/etc/passwd", "/tmp", 4000)
	if result.Error == "" {
		t.Error("expected error for absolute path")
	}
}

func TestReadWorkspaceFileNotFound(t *testing.T) {
	dir := t.TempDir()
	result := readFileContent("test-session", "nonexistent.txt", dir, 4000)
	if result.Error == "" {
		t.Error("expected error for missing file")
	}
	if !strings.Contains(result.Error, "file not found") {
		t.Errorf("error should mention 'file not found', got: %s", result.Error)
	}
}

func TestReadWorkspaceFileSessionIDTraversal(t *testing.T) {
	dir := t.TempDir()

	// Create a file outside the target session workspace
	outsideSession := filepath.Join(dir, "victim-session")
	os.MkdirAll(outsideSession, 0o755)
	os.WriteFile(filepath.Join(outsideSession, "secret.txt"), []byte("secret"), 0o644)

	// A session ID containing path separators should be rejected outright.
	cases := []string{
		"../victim-session",
		"../../etc",
		"a/b",
		".hidden",
		strings.Repeat("a", 129), // exceeds 128-char cap
	}
	for _, badID := range cases {
		result := readFileContent(badID, "secret.txt", dir, 4000)
		if result.Error == "" {
			t.Errorf("session_id %q: expected error, got content %q", badID, result.Content)
		}
		if result.Content != "" {
			t.Errorf("session_id %q: expected no content, got %q", badID, result.Content)
		}
	}
}

func TestReadWorkspaceFileSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "test-session")
	os.MkdirAll(sessionDir, 0o755)

	// Create a file outside the workspace that the symlink will point to
	outsideFile := filepath.Join(dir, "secret.txt")
	os.WriteFile(outsideFile, []byte("secret content"), 0o644)

	// Create a symlink inside the workspace pointing outside
	symlinkPath := filepath.Join(sessionDir, "escape.txt")
	os.Symlink(outsideFile, symlinkPath)

	result := readFileContent("test-session", "escape.txt", dir, 4000)
	if result.Error == "" {
		t.Error("expected error for symlink escape, got none")
	}
	if result.Content != "" {
		t.Errorf("expected no content for symlink escape, got: %s", result.Content)
	}
}

func TestReadWorkspaceFileWorkspacePrefix(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "test-session")
	os.MkdirAll(filepath.Join(sessionDir, "findings"), 0o755)
	os.WriteFile(filepath.Join(sessionDir, "findings", "react.md"), []byte("# React Research"), 0o644)

	// Lead often hallucinate "workspace/findings/react.md" instead of "findings/react.md"
	// because agent protocol teaches /workspace/ prefix for python_executor.
	cases := []struct {
		name string
		path string
	}{
		{"bare relative", "findings/react.md"},
		{"workspace/ prefix", "workspace/findings/react.md"},
		{"workspace prefix no slash", "workspace/findings/react.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := readFileContent("test-session", tc.path, dir, 4000)
			if result.Error != "" {
				t.Errorf("path %q: unexpected error: %s", tc.path, result.Error)
			}
			if !strings.Contains(result.Content, "React Research") {
				t.Errorf("path %q: expected content, got: %q", tc.path, result.Content)
			}
		})
	}
}

func TestReadWorkspaceFileEmptyInputs(t *testing.T) {
	// Test with empty sessionID — readFileContent doesn't validate this
	// (the activity function does), so test that it handles gracefully.
	result := readFileContent("", "test.txt", "/tmp", 4000)
	// With empty sessionID, path resolves to /tmp//test.txt which shouldn't exist.
	// Must return an error or empty content — never panic.
	assert.True(t, result.Error != "" || result.Content == "",
		"Empty sessionID should produce an error or empty content, got: error=%q content=%q",
		result.Error, result.Content)
}
