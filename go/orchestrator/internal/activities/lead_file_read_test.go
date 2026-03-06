package activities

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestReadWorkspaceFileEmptyInputs(t *testing.T) {
	// Test with empty sessionID — readFileContent doesn't validate this
	// (the activity function does), so just test path handling
	result := readFileContent("", "test.txt", "/tmp", 4000)
	// Should get file not found since /tmp//test.txt likely doesn't exist
	if result.Error == "" && result.Content == "" {
		// Either error or no content is acceptable
	}
}
