package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

// WorkspaceHandler handles session workspace file operations
type WorkspaceHandler struct {
	db               *sqlx.DB
	logger           *zap.Logger
	firecrackerURL   string
	workspaceBaseDir string
	httpClient       *http.Client
}

// NewWorkspaceHandler creates a new workspace handler
func NewWorkspaceHandler(db *sqlx.DB, logger *zap.Logger) *WorkspaceHandler {
	firecrackerURL := os.Getenv("FIRECRACKER_EXECUTOR_URL")
	if firecrackerURL == "" {
		firecrackerURL = "http://firecracker-executor:9001"
	}

	workspaceBaseDir := os.Getenv("SHANNON_SESSION_WORKSPACES_DIR")
	if workspaceBaseDir == "" {
		// Backwards-compat: allow older config name
		workspaceBaseDir = os.Getenv("FIRECRACKER_EFS_MOUNT")
	}
	if workspaceBaseDir == "" {
		workspaceBaseDir = "/tmp/shannon-sessions"
	}

	return &WorkspaceHandler{
		db:               db,
		logger:           logger,
		firecrackerURL:   strings.TrimSuffix(firecrackerURL, "/"),
		workspaceBaseDir: workspaceBaseDir,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// DownloadRequest matches the Firecracker executor's request format
type DownloadRequest struct {
	SessionID     string `json:"session_id"`
	FilePath      string `json:"file_path"`
	WorkspacePath string `json:"workspace_path"`
}

// DownloadResponse matches the Firecracker executor's response format
type DownloadResponse struct {
	Success     bool    `json:"success"`
	Content     *string `json:"content,omitempty"`
	ContentType *string `json:"content_type,omitempty"`
	SizeBytes   *uint64 `json:"size_bytes,omitempty"`
	Error       *string `json:"error,omitempty"`
}

// ListFilesRequest matches the Firecracker executor's request format
type ListFilesRequest struct {
	SessionID     string  `json:"session_id"`
	WorkspacePath string  `json:"workspace_path"`
	Path          *string `json:"path,omitempty"`
}

// FileInfo represents a file in the workspace
type FileInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsDir     bool   `json:"is_dir"`
	SizeBytes uint64 `json:"size_bytes"`
}

// ListFilesResponse matches the Firecracker executor's response format
type ListFilesResponse struct {
	Success bool       `json:"success"`
	Files   []FileInfo `json:"files"`
	Error   *string    `json:"error,omitempty"`
}

type sessionMeta struct {
	ID         string         `db:"id"`
	ExternalID sql.NullString `db:"external_id"`
}

// DownloadFile handles GET /api/v1/sessions/{sessionId}/files/{path...}
func (h *WorkspaceHandler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userCtx, ok := ctx.Value(auth.UserContextKey).(*auth.UserContext)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	sessionID := r.PathValue("sessionId")
	filePath := r.PathValue("path")

	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id is required"})
		return
	}

	if filePath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file path is required"})
		return
	}

	// Security: validate path doesn't escape workspace
	if strings.Contains(filePath, "..") || strings.HasPrefix(filePath, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path: cannot contain '..'"})
		return
	}

	workspaceKey, err := h.resolveWorkspaceKey(ctx, sessionID, userCtx)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		h.logger.Error("Failed to resolve workspace key", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	workspacePath, err := h.buildWorkspacePath(workspaceKey)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	h.logger.Info("Downloading file from workspace",
		zap.String("session_id", sessionID),
		zap.String("workspace_key", workspaceKey),
		zap.String("file_path", filePath),
		zap.String("workspace_path", workspacePath),
	)

	// Forward to Firecracker executor
	reqBody := DownloadRequest{
		SessionID:     workspaceKey,
		FilePath:      filePath,
		WorkspacePath: workspacePath,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		h.logger.Error("Failed to marshal request", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.firecrackerURL+"/workspace/download", bytes.NewReader(bodyBytes))
	if err != nil {
		h.logger.Error("Failed to create request", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.httpClient.Do(req)
	if err != nil {
		// Fallback to local filesystem when Firecracker is unavailable
		h.logger.Info("Firecracker unavailable, falling back to local filesystem", zap.Error(err))
		h.downloadFileLocal(w, workspacePath, filePath)
		return
	}
	defer resp.Body.Close()

	// Forward response
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// ListFiles handles GET /api/v1/sessions/{sessionId}/files
func (h *WorkspaceHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userCtx, ok := ctx.Value(auth.UserContextKey).(*auth.UserContext)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	sessionID := r.PathValue("sessionId")

	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id is required"})
		return
	}

	// Optional subdirectory path from query param
	subPath := r.URL.Query().Get("path")

	// Security: validate path doesn't escape workspace
	if strings.Contains(subPath, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path: cannot contain '..'"})
		return
	}

	workspaceKey, err := h.resolveWorkspaceKey(ctx, sessionID, userCtx)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		h.logger.Error("Failed to resolve workspace key", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	workspacePath, err := h.buildWorkspacePath(workspaceKey)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	h.logger.Info("Listing files in workspace",
		zap.String("session_id", sessionID),
		zap.String("workspace_key", workspaceKey),
		zap.String("sub_path", subPath),
		zap.String("workspace_path", workspacePath),
	)

	// Forward to Firecracker executor
	reqBody := ListFilesRequest{
		SessionID:     workspaceKey,
		WorkspacePath: workspacePath,
	}
	if subPath != "" {
		reqBody.Path = &subPath
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		h.logger.Error("Failed to marshal request", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.firecrackerURL+"/workspace/list", bytes.NewReader(bodyBytes))
	if err != nil {
		h.logger.Error("Failed to create request", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.httpClient.Do(req)
	if err != nil {
		// Fallback to local filesystem when Firecracker is unavailable
		h.logger.Info("Firecracker unavailable, falling back to local filesystem", zap.Error(err))
		h.listFilesLocal(w, workspacePath, subPath)
		return
	}
	defer resp.Body.Close()

	// Forward response
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// listFilesLocal reads workspace files directly from the local filesystem.
// Used as fallback when Firecracker executor is unavailable (e.g. local dev).
func (h *WorkspaceHandler) listFilesLocal(w http.ResponseWriter, workspacePath, subPath string) {
	targetDir := workspacePath
	if subPath != "" {
		targetDir = filepath.Join(workspacePath, subPath)
	}

	// Security: ensure resolved path stays within workspace
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	absBase, err2 := filepath.Abs(workspacePath)
	if err2 != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid workspace path"})
		return
	}
	if !strings.HasPrefix(absTarget, absBase+string(filepath.Separator)) && absTarget != absBase {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path escapes workspace"})
		return
	}

	// If workspace directory doesn't exist, return empty list
	entries, err := os.ReadDir(absTarget)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, ListFilesResponse{Success: true, Files: []FileInfo{}})
			return
		}
		h.logger.Error("Failed to read workspace directory", zap.Error(err))
		errMsg := "failed to read directory"
		writeJSON(w, http.StatusInternalServerError, ListFilesResponse{Success: false, Error: &errMsg})
		return
	}

	files := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		relPath := entry.Name()
		if subPath != "" {
			relPath = subPath + "/" + entry.Name()
		}
		files = append(files, FileInfo{
			Name:      entry.Name(),
			Path:      relPath,
			IsDir:     entry.IsDir(),
			SizeBytes: uint64(info.Size()),
		})
	}

	writeJSON(w, http.StatusOK, ListFilesResponse{Success: true, Files: files})
}

// downloadFileLocal reads a single file from the local filesystem.
// Returns JSON response matching the Firecracker executor format.
func (h *WorkspaceHandler) downloadFileLocal(w http.ResponseWriter, workspacePath, filePath string) {
	fullPath := filepath.Join(workspacePath, filePath)

	// Security: ensure resolved path stays within workspace
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	absBase, err2 := filepath.Abs(workspacePath)
	if err2 != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid workspace path"})
		return
	}
	if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) && absPath != absBase {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path escapes workspace"})
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			errMsg := "file not found"
			writeJSON(w, http.StatusNotFound, DownloadResponse{Success: false, Error: &errMsg})
			return
		}
		h.logger.Error("Failed to stat file", zap.Error(err))
		errMsg := "failed to read file"
		writeJSON(w, http.StatusInternalServerError, DownloadResponse{Success: false, Error: &errMsg})
		return
	}

	if info.IsDir() {
		errMsg := "path is a directory"
		writeJSON(w, http.StatusBadRequest, DownloadResponse{Success: false, Error: &errMsg})
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		h.logger.Error("Failed to read file", zap.Error(err))
		errMsg := "failed to read file"
		writeJSON(w, http.StatusInternalServerError, DownloadResponse{Success: false, Error: &errMsg})
		return
	}

	ct := detectContentType(filePath)
	size := uint64(len(data))

	// For text files, return content directly; for binary, base64-encode
	var content string
	if isTextContentType(ct) {
		content = string(data)
	} else {
		content = base64.StdEncoding.EncodeToString(data)
	}

	writeJSON(w, http.StatusOK, DownloadResponse{
		Success:     true,
		Content:     &content,
		ContentType: &ct,
		SizeBytes:   &size,
	})
}

// detectContentType returns a MIME type based on file extension.
func detectContentType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))

	// Common types that mime.TypeByExtension may not cover well
	switch ext {
	case ".md":
		return "text/markdown"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "text/yaml"
	case ".txt", ".log":
		return "text/plain"
	case ".csv":
		return "text/csv"
	case ".py":
		return "text/x-python"
	case ".go":
		return "text/x-go"
	case ".rs":
		return "text/x-rust"
	case ".js":
		return "text/javascript"
	case ".ts":
		return "text/typescript"
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".xml":
		return "text/xml"
	case ".sh":
		return "text/x-shellscript"
	case ".toml":
		return "text/toml"
	}

	// Fall back to stdlib
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// isTextContentType returns true if the content type represents text content.
func isTextContentType(ct string) bool {
	return strings.HasPrefix(ct, "text/") || ct == "application/json" || ct == "application/xml"
}

func (h *WorkspaceHandler) resolveWorkspaceKey(
	ctx context.Context,
	sessionID string,
	userCtx *auth.UserContext,
) (string, error) {
	var meta sessionMeta
	if err := h.db.GetContext(ctx, &meta, `
        SELECT id::text as id, context->>'external_id' as external_id
        FROM sessions
        WHERE (id::text = $1 OR context->>'external_id' = $1)
          AND user_id = $2
          AND tenant_id = $3
          AND deleted_at IS NULL
    `, sessionID, userCtx.UserID.String(), userCtx.TenantID); err != nil {
		return "", err
	}

	// If external_id is present, it's the session identifier used by task execution and workspace manager.
	if meta.ExternalID.Valid && strings.TrimSpace(meta.ExternalID.String) != "" {
		return strings.TrimSpace(meta.ExternalID.String), nil
	}

	return strings.TrimSpace(meta.ID), nil
}

func (h *WorkspaceHandler) buildWorkspacePath(workspaceKey string) (string, error) {
	if err := validateWorkspaceSessionID(workspaceKey); err != nil {
		return "", err
	}

	base := filepath.Clean(h.workspaceBaseDir)
	path := filepath.Join(base, workspaceKey)

	rel, err := filepath.Rel(base, path)
	if err != nil {
		return "", fmt.Errorf("invalid workspace base directory")
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("workspace path escapes base directory")
	}

	return path, nil
}

func validateWorkspaceSessionID(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("invalid session_id")
	}
	if len(sessionID) > 128 {
		return fmt.Errorf("invalid session_id")
	}
	// Reject path traversal attempts
	if strings.Contains(sessionID, "..") || strings.HasPrefix(sessionID, ".") {
		return fmt.Errorf("invalid session_id")
	}
	for _, c := range sessionID {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return fmt.Errorf("invalid session_id")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
