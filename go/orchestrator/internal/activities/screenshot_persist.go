package activities

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// persistScreenshot decodes base64 PNG data and writes it to the session workspace.
// Returns the relative path from session root (e.g. "screenshots/1709312345678_abc12345_0.png").
// All errors are non-fatal — returns empty string on failure.
func persistScreenshot(logger *zap.Logger, sessionID, workflowID string, index int, b64Data string) string {
	// Guard: skip if data is too short to be a plausible image
	if len(b64Data) < 100 {
		return ""
	}

	// Validate session ID to prevent path traversal
	if err := validateSessionID(sessionID); err != nil {
		logger.Warn("persistScreenshot: invalid session_id", zap.Error(err))
		return ""
	}

	// Strip data URI prefix if present (e.g. "data:image/png;base64,...")
	if idx := strings.Index(b64Data, ","); idx >= 0 && idx < 100 {
		b64Data = b64Data[idx+1:]
	}

	// Strip whitespace and newlines that some encoders insert
	b64Data = strings.NewReplacer("\n", "", "\r", "", " ", "", "\t", "").Replace(b64Data)

	// Try multiple base64 variants for robustness:
	// 1. Standard (with padding)
	// 2. Raw standard (no padding)
	// 3. URL-safe (with padding) — some HTTP transports use this
	// 4. Raw URL-safe (no padding)
	var data []byte
	var err error
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		data, err = enc.DecodeString(b64Data)
		if err == nil {
			break
		}
	}
	if err != nil {
		logger.Warn("persistScreenshot: base64 decode failed (all variants)",
			zap.Error(err),
			zap.Int("input_len", len(b64Data)),
		)
		return ""
	}

	// Build file path
	baseDir := os.Getenv("SHANNON_SESSION_WORKSPACES_DIR")
	if baseDir == "" {
		baseDir = "/tmp/shannon-sessions"
	}

	// Use last 8 chars of workflow ID as suffix for readability
	wfSuffix := workflowID
	if len(wfSuffix) > 8 {
		wfSuffix = wfSuffix[len(wfSuffix)-8:]
	}

	ts := time.Now().UnixMilli()
	filename := fmt.Sprintf("%d_%s_%d.png", ts, wfSuffix, index)
	relPath := filepath.Join("screenshots", filename)
	absDir := filepath.Join(baseDir, sessionID, "screenshots")
	absPath := filepath.Join(absDir, filename)

	// Create directory
	if err := os.MkdirAll(absDir, 0755); err != nil {
		logger.Warn("persistScreenshot: mkdir failed", zap.String("dir", absDir), zap.Error(err))
		return ""
	}

	// Write file
	if err := os.WriteFile(absPath, data, 0644); err != nil {
		logger.Warn("persistScreenshot: write failed", zap.String("path", absPath), zap.Error(err))
		return ""
	}

	logger.Info("persistScreenshot: saved",
		zap.String("session_id", sessionID),
		zap.String("path", relPath),
		zap.Int("bytes", len(data)),
	)
	return relPath
}

// extractScreenshotPathsFromMetadata scans tool_execution_records in agent response
// metadata for screenshot_path values set by Python's browser tool persistence.
// This covers the agent loop path where tools execute inside Python and Go only
// receives metadata (not raw ToolResults).
func extractScreenshotPathsFromMetadata(metadata map[string]interface{}) []string {
	if metadata == nil {
		return nil
	}
	records, ok := metadata["tool_execution_records"].([]interface{})
	if !ok {
		return nil
	}
	var paths []string
	for _, rec := range records {
		m, ok := rec.(map[string]interface{})
		if !ok {
			continue
		}
		tool, _ := m["tool"].(string)
		if tool != "browser" {
			continue
		}
		out, ok := m["output"].(map[string]interface{})
		if !ok {
			continue
		}
		if p, ok := out["screenshot_path"].(string); ok && p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}
