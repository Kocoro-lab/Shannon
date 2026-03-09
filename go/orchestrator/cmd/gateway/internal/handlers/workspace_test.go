package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestDownloadFileLocal_RejectsLargeFile(t *testing.T) {
	dir := t.TempDir()
	largePath := filepath.Join(dir, "large.bin")

	// Create a sparse file that reports large size
	f, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(101*1024*1024, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	f.Close()

	handler := &WorkspaceHandler{logger: zap.NewNop()}
	rr := httptest.NewRecorder()
	handler.downloadFileLocal(rr, dir, "large.bin")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for large file, got %d", rr.Code)
	}
}
