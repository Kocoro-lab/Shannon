package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"go.temporal.io/sdk/activity"
)

// MemoryExtractInput is the input for the ExtractMemoryActivity.
type MemoryExtractInput struct {
	UserID           string `json:"user_id"`
	TenantID         string `json:"tenant_id"`
	SessionID        string `json:"session_id"`
	Query            string `json:"query"`
	Result           string `json:"result"`
	ParentWorkflowID string `json:"parent_workflow_id"`
}

// MemoryExtractResult is the output of the ExtractMemoryActivity.
type MemoryExtractResult struct {
	Extracted  bool   `json:"extracted"`
	FilePath   string `json:"file_path,omitempty"`
	SkipReason string `json:"skip_reason,omitempty"`
}

type memoryTriple struct {
	H string `json:"h"`
	R string `json:"r"`
	T string `json:"t"`
}

type memoryTripleRecord struct {
	H   string `json:"h"`
	R   string `json:"r"`
	T   string `json:"t"`
	Src string `json:"src"`
	Ts  string `json:"ts"`
}

// memoryIndexMu serializes MEMORY.md read-dedupe-write operations to prevent corruption.
var memoryIndexMu sync.Mutex

// safePathRe only allows lowercase alphanumeric, hyphens, underscores, and dots.
var safePathRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*\.md$`)

// validUserID validates user IDs before they are used for filesystem paths.
func validUserID(userID string) error {
	if userID == "" {
		return fmt.Errorf("empty user_id")
	}
	if len(userID) > 128 {
		return fmt.Errorf("user_id exceeds maximum length of 128")
	}
	for _, ch := range userID {
		if (ch >= '0' && ch <= '9') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= 'a' && ch <= 'z') ||
			ch == '-' ||
			ch == '_' {
			continue
		}
		return fmt.Errorf("user_id contains invalid character: %q", ch)
	}
	if userID == "." || userID == ".." || strings.HasPrefix(userID, ".") || strings.Contains(userID, "..") {
		return fmt.Errorf("user_id cannot contain path traversal characters")
	}
	return nil
}

// ExtractMemoryActivity calls the LLM to extract key findings from a task result
// and writes them to the user's /memory directory. Best-effort: never returns error.
func ExtractMemoryActivity(ctx context.Context, input MemoryExtractInput) (*MemoryExtractResult, error) {
	logger := activity.GetLogger(ctx)

	skip := func(reason string) (*MemoryExtractResult, error) {
		logger.Info("memory.extraction.skip", "reason", reason, "user_id", input.UserID)
		return &MemoryExtractResult{Extracted: false, SkipReason: reason}, nil
	}

	// Guard: feature gate
	if os.Getenv("USER_MEMORY_ENABLED") != "1" {
		return skip("disabled")
	}

	// Guard: no user or short result
	if input.UserID == "" {
		return skip("no_user_id")
	}
	if err := validUserID(input.UserID); err != nil {
		logger.Warn("memory.extraction.invalid_user_id", "user_id", input.UserID, "error", err)
		return skip("invalid_user_id")
	}
	if len(input.Result) < 500 {
		return skip("short_result")
	}

	// Call LLM service /memory/extract
	llmServiceURL := getenvDefault("LLM_SERVICE_URL", "http://llm-service:8000")
	extractURL := fmt.Sprintf("%s/memory/extract", llmServiceURL)

	// Truncate to control cost
	query := input.Query
	if len(query) > 1000 {
		query = query[:1000]
	}
	result := input.Result
	if len(result) > 4000 {
		result = result[:4000]
	}

	reqBody, _ := json.Marshal(map[string]string{
		"query":  query,
		"result": result,
	})

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, extractURL, strings.NewReader(string(reqBody)))
	if err != nil {
		logger.Warn("memory.extraction.http_error", "error", err)
		return skip("http_build_error")
	}
	req.Header.Set("Content-Type", "application/json")
	if input.ParentWorkflowID != "" {
		req.Header.Set("X-Workflow-ID", input.ParentWorkflowID)
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("memory.extraction.http_error", "error", err)
		return skip("http_call_error")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Warn("memory.extraction.http_status", "status", resp.StatusCode)
		return skip("http_status_error")
	}

	var extraction struct {
		WorthRemembering bool     `json:"worth_remembering"`
		Title            string   `json:"title"`
		Summary          string   `json:"summary"`
		Content          string   `json:"content"`
		SuggestedPath    string   `json:"suggested_path"`
		Tags             []string `json:"tags"`
		Triples          []memoryTriple `json:"triples"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&extraction); err != nil {
		logger.Warn("memory.extraction.decode_error", "error", err)
		return skip("decode_error")
	}

	if !extraction.WorthRemembering {
		return skip("not_worth_remembering")
	}

	// Quality gate
	if len(extraction.Summary) < 20 || len(extraction.Content) < 100 {
		return skip("low_quality")
	}

	// Cap summary length to keep MEMORY.md index dense
	if len(extraction.Summary) > 400 {
		extraction.Summary = extraction.Summary[:400]
	}

	// Validate and sanitize suggested path
	cleanPath, err := validateMemoryPath(extraction.SuggestedPath)
	if err != nil {
		logger.Warn("memory.extraction.invalid_path", "path", extraction.SuggestedPath, "error", err)
		return skip("invalid_path")
	}

	// Resolve base memory directory
	memDir := getenvDefault("SHANNON_USER_MEMORY_DIR", "/tmp/shannon-users")
	userMemDir := filepath.Join(memDir, input.UserID, "memory")

	// Write the extracted content file
	targetPath := filepath.Join(userMemDir, cleanPath)

	// Final safety: ensure resolved path is inside userMemDir
	absTarget, _ := filepath.Abs(targetPath)
	absBase, _ := filepath.Abs(userMemDir)
	if !strings.HasPrefix(absTarget, absBase+string(filepath.Separator)) {
		logger.Warn("memory.extraction.path_escape", "target", absTarget, "base", absBase)
		return skip("path_escape")
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		logger.Warn("memory.extraction.mkdir_error", "error", err)
		return skip("mkdir_error")
	}

	fileContent := fmt.Sprintf("# %s\n\n%s\n", extraction.Title, extraction.Content)
	if err := os.WriteFile(targetPath, []byte(fileContent), 0644); err != nil {
		logger.Warn("memory.extraction.write_error", "error", err)
		return skip("write_error")
	}

	if err := appendMemoryTriples(userMemDir, cleanPath, extraction.Triples); err != nil {
		logger.Warn("memory.extraction.triple_append_error", "error", err)
	}

	// Update MEMORY.md index
	if err := updateMemoryIndex(userMemDir, cleanPath, extraction.Summary); err != nil {
		logger.Warn("memory.extraction.index_error", "error", err)
		// File was written, just index failed — still count as extracted
	}

	logger.Info("memory.extraction.success",
		"user_id", input.UserID,
		"path", cleanPath,
		"content_len", len(extraction.Content),
	)

	return &MemoryExtractResult{
		Extracted: true,
		FilePath:  cleanPath,
	}, nil
}

// appendMemoryTriples appends validated triples to triples.jsonl as JSONL records.
func appendMemoryTriples(memoryDir, sourceFile string, triples []memoryTriple) error {
	if sourceFile == "" || len(triples) == 0 {
		return nil
	}

	var records []memoryTripleRecord
	for _, triple := range triples {
		h := strings.TrimSpace(triple.H)
		r := strings.TrimSpace(triple.R)
		t := strings.TrimSpace(triple.T)
		if h == "" || r == "" || t == "" {
			continue
		}
		records = append(records, memoryTripleRecord{
			H:   h,
			R:   r,
			T:   t,
			Src: sourceFile,
			Ts:  time.Now().UTC().Format(time.RFC3339),
		})
	}
	if len(records) == 0 {
		return nil
	}

	memoryIndexMu.Lock()
	defer memoryIndexMu.Unlock()

	triplesPath := filepath.Join(memoryDir, "triples.jsonl")
	f, err := os.OpenFile(triplesPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open triples file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, record := range records {
		if err := enc.Encode(record); err != nil {
			return fmt.Errorf("encode triple: %w", err)
		}
	}

	return nil
}

// validateMemoryPath validates and sanitizes the suggested path from the LLM.
func validateMemoryPath(suggestedPath string) (string, error) {
	if suggestedPath == "" {
		return "", fmt.Errorf("empty path")
	}

	// Reject absolute paths
	if filepath.IsAbs(suggestedPath) {
		return "", fmt.Errorf("absolute path not allowed")
	}

	// Clean the path
	cleaned := filepath.Clean(suggestedPath)

	// Reject traversal after clean
	if strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("path traversal not allowed")
	}

	// Flatten: strip directory prefix if LLM suggests subdirectories (e.g. "caching/redis.md" → "redis.md")
	if strings.Contains(cleaned, string(filepath.Separator)) {
		cleaned = filepath.Base(cleaned)
	}

	// Must end in .md
	if !strings.HasSuffix(cleaned, ".md") {
		return "", fmt.Errorf("must end in .md")
	}

	// Reject empty segment names or hidden files
	if strings.HasPrefix(cleaned, ".") {
		return "", fmt.Errorf("hidden files not allowed")
	}

	// Only safe characters
	if !safePathRe.MatchString(cleaned) {
		return "", fmt.Errorf("unsafe characters in path: %s", cleaned)
	}

	return cleaned, nil
}

// maxMemoryIndexEntries is the maximum number of entries kept in the MEMORY.md index.
// When exceeded, the oldest entries (by last-updated timestamp) are evicted from the
// index. Content files remain on disk — only the index reference is removed.
const maxMemoryIndexEntries = 50

// memoryEntry represents a single parsed section from MEMORY.md.
type memoryEntry struct {
	Path      string // filename from ## heading
	Summary   string // description text
	UpdatedAt time.Time
}

// parseMemoryIndex parses MEMORY.md into structured entries.
// Each entry has format:
//
//	## filename.md
//	<!-- updated:2006-01-02T15:04:05Z -->
//	Summary text here
func parseMemoryIndex(content string) (header string, entries []memoryEntry) {
	// Split on ## headings
	sections := strings.Split(content, "\n## ")
	if len(sections) == 0 {
		return "# User Memory\n", nil
	}
	header = sections[0]

	for _, sec := range sections[1:] {
		lines := strings.SplitN(sec, "\n", 2)
		if len(lines) == 0 {
			continue
		}
		path := strings.TrimSpace(lines[0])
		if path == "" {
			continue
		}
		body := ""
		if len(lines) > 1 {
			body = lines[1]
		}

		e := memoryEntry{Path: path}

		// Extract timestamp from <!-- updated:... --> comment.
		// Always strip the comment line to prevent duplication on re-render.
		if strings.Contains(body, "<!-- updated:") {
			if ts := extractTimestamp(body); !ts.IsZero() {
				e.UpdatedAt = ts
			}
			body = stripTimestampComment(body)
		}

		e.Summary = strings.TrimSpace(body)
		entries = append(entries, e)
	}
	return header, entries
}

// extractTimestamp parses <!-- updated:RFC3339 --> from a block of text.
func extractTimestamp(body string) time.Time {
	const prefix = "<!-- updated:"
	const suffix = " -->"
	idx := strings.Index(body, prefix)
	if idx < 0 {
		return time.Time{}
	}
	start := idx + len(prefix)
	end := strings.Index(body[start:], suffix)
	if end < 0 {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, body[start:start+end])
	if err != nil {
		return time.Time{}
	}
	return t
}

// stripTimestampComment removes the <!-- updated:... --> line from body text.
func stripTimestampComment(body string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "<!-- updated:") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// renderMemoryIndex renders the header + entries back into MEMORY.md format.
func renderMemoryIndex(header string, entries []memoryEntry) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(header, "\n"))
	b.WriteString("\n")
	for _, e := range entries {
		b.WriteString("\n## ")
		b.WriteString(e.Path)
		b.WriteString("\n")
		if !e.UpdatedAt.IsZero() {
			b.WriteString("<!-- updated:")
			b.WriteString(e.UpdatedAt.UTC().Format(time.RFC3339))
			b.WriteString(" -->\n")
		}
		b.WriteString(e.Summary)
		b.WriteString("\n")
	}
	return b.String()
}

// updateMemoryIndex atomically updates MEMORY.md with an entry for the given file.
// If a heading for the path already exists, its description is updated (no duplicate).
// When the index exceeds maxMemoryIndexEntries, the oldest entries are evicted.
func updateMemoryIndex(memoryDir, canonicalPath, summary string) error {
	memoryIndexMu.Lock()
	defer memoryIndexMu.Unlock()

	indexPath := filepath.Join(memoryDir, "MEMORY.md")

	// Read existing or create skeleton
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read MEMORY.md: %w", err)
		}
		data = []byte("# User Memory\n\n")
	}

	header, entries := parseMemoryIndex(string(data))
	now := time.Now().UTC()

	// Check if entry already exists — update in place
	found := false
	for i := range entries {
		if entries[i].Path == canonicalPath {
			entries[i].Summary = summary
			entries[i].UpdatedAt = now
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, memoryEntry{
			Path:      canonicalPath,
			Summary:   summary,
			UpdatedAt: now,
		})
	}

	// Evict oldest entries if over cap.
	// Stable tiebreaker on Path ensures deterministic eviction when timestamps match.
	if len(entries) > maxMemoryIndexEntries {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].UpdatedAt.Equal(entries[j].UpdatedAt) {
				return entries[i].Path < entries[j].Path
			}
			return entries[i].UpdatedAt.Before(entries[j].UpdatedAt)
		})
		entries = entries[len(entries)-maxMemoryIndexEntries:]
	}

	return os.WriteFile(indexPath, []byte(renderMemoryIndex(header, entries)), 0644)
}
