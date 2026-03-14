package activities

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.temporal.io/sdk/activity"
)

// ReadWorkspaceFileInput is the input for the ReadWorkspaceFile activity.
type ReadWorkspaceFileInput struct {
	SessionID string `json:"session_id"`
	Path      string `json:"path"`
	MaxChars  int    `json:"max_chars"`
}

// ReadWorkspaceFileResult is the output of the ReadWorkspaceFile activity.
type ReadWorkspaceFileResult struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	Error     string `json:"error,omitempty"`
}

// ReadWorkspaceFile reads a single file from the session workspace.
// Used by Lead's file_read action to independently verify agent output.
func ReadWorkspaceFile(ctx context.Context, in ReadWorkspaceFileInput) (ReadWorkspaceFileResult, error) {
	logger := activity.GetLogger(ctx)

	if in.SessionID == "" || in.Path == "" {
		return ReadWorkspaceFileResult{Path: in.Path, Error: "empty session_id or path"}, nil
	}
	// Validate SessionID — only alphanumeric, hyphen, and underscore allowed.
	// Rejects path separators that would shift the workspace root before the
	// symlink containment check runs (e.g. session_id="../other-session").
	if !isValidSessionID(in.SessionID) {
		return ReadWorkspaceFileResult{Path: in.Path, Error: "invalid session_id"}, nil
	}
	if in.MaxChars <= 0 {
		in.MaxChars = 4000
	}

	// Sanitize path — prevent directory traversal and symlink escape
	cleanPath := filepath.Clean(in.Path)
	// Strip common LLM-hallucinated prefixes — Lead sometimes outputs
	// "workspace/file.md" or "/workspace/file.md" because agent protocol
	// teaches /workspace/ prefix for python_executor. ReadWorkspaceFile
	// already roots at the session dir, so the prefix is redundant.
	cleanPath = strings.TrimPrefix(cleanPath, "workspace/")
	cleanPath = strings.TrimPrefix(cleanPath, "workspace")
	if cleanPath == "" || cleanPath == "." {
		cleanPath = "."
	}
	if strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
		return ReadWorkspaceFileResult{Path: in.Path, Error: "invalid path"}, nil
	}

	baseDir := os.Getenv("SHANNON_SESSION_WORKSPACES_DIR")
	if baseDir == "" {
		baseDir = "/tmp/shannon-sessions"
	}
	fullPath := filepath.Join(baseDir, in.SessionID, cleanPath)

	// Resolve symlinks and verify the real path stays within the session workspace.
	// Both the session dir and the target are resolved so that path-level symlinks
	// (e.g. macOS /tmp -> /private/tmp) do not cause false prefix mismatches,
	// while symlink escapes (e.g. workspace/link -> /etc/passwd) are still blocked.
	sessionDir := filepath.Join(baseDir, in.SessionID)
	realSessionDir, _ := filepath.EvalSymlinks(sessionDir)
	if realSessionDir == "" {
		realSessionDir = sessionDir
	}
	realPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		// File does not exist or cannot be resolved (e.g. broken symlink).
		logger.Info("Lead file_read: file not found", "path", in.Path, "error", err)
		return ReadWorkspaceFileResult{Path: in.Path, Error: fmt.Sprintf("file not found: %s", in.Path)}, nil
	}
	if !strings.HasPrefix(realPath, realSessionDir+string(filepath.Separator)) {
		return ReadWorkspaceFileResult{Path: in.Path, Error: "invalid path"}, nil
	}

	data, err := os.ReadFile(realPath)
	if err != nil {
		logger.Info("Lead file_read: file not found", "path", in.Path, "error", err)
		return ReadWorkspaceFileResult{Path: in.Path, Error: fmt.Sprintf("file not found: %s", in.Path)}, nil
	}

	content := string(data)
	truncated := false
	if len(content) > in.MaxChars {
		content = content[:in.MaxChars]
		truncated = true
	}

	logger.Info("Lead file_read successful", "path", in.Path, "size", len(data), "truncated", truncated)
	return ReadWorkspaceFileResult{
		Path:      in.Path,
		Content:   content,
		Truncated: truncated,
	}, nil
}

// isValidSessionID returns true if the session ID is non-empty, at most 128
// characters, and contains only ASCII alphanumeric characters, hyphens, and
// underscores. This matches the validation in the Python file_ops layer
// (_validate_session_id) and prevents path traversal via the session ID itself.
func isValidSessionID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// readFileContent is the pure logic extracted for testing without activity context.
func readFileContent(sessionID, path, baseDir string, maxChars int) ReadWorkspaceFileResult {
	if !isValidSessionID(sessionID) {
		return ReadWorkspaceFileResult{Path: path, Error: "invalid session_id"}
	}
	cleanPath := filepath.Clean(path)
	// Strip LLM-hallucinated workspace/ prefix (mirrors ReadWorkspaceFile above)
	cleanPath = strings.TrimPrefix(cleanPath, "workspace/")
	cleanPath = strings.TrimPrefix(cleanPath, "workspace")
	if cleanPath == "" || cleanPath == "." {
		cleanPath = "."
	}
	if strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
		return ReadWorkspaceFileResult{Path: path, Error: "invalid path"}
	}
	fullPath := filepath.Join(baseDir, sessionID, cleanPath)
	sessionDir := filepath.Join(baseDir, sessionID)
	realSessionDir, _ := filepath.EvalSymlinks(sessionDir)
	if realSessionDir == "" {
		realSessionDir = sessionDir
	}
	realPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return ReadWorkspaceFileResult{Path: path, Error: "file not found: " + path}
	}
	if !strings.HasPrefix(realPath, realSessionDir+string(filepath.Separator)) {
		return ReadWorkspaceFileResult{Path: path, Error: "invalid path"}
	}
	data, err := os.ReadFile(realPath)
	if err != nil {
		return ReadWorkspaceFileResult{Path: path, Error: "file not found: " + path}
	}
	content := string(data)
	truncated := false
	if len(content) > maxChars {
		content = content[:maxChars]
		truncated = true
	}
	return ReadWorkspaceFileResult{Path: path, Content: content, Truncated: truncated}
}
