package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danrichardson/sqzarr/internal/db"
)

func TestRestoreRejectsMalformedBodyBeforeMovingFiles(t *testing.T) {
	srv, database := newTestServer(t)
	root := t.TempDir()
	original := filepath.Join(root, "original.mkv")
	held := filepath.Join(root, "held.mkv")
	if err := os.WriteFile(held, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	jobID, err := database.InsertJob(&db.Job{SourcePath: original, Status: db.JobStaged})
	if err != nil {
		t.Fatal(err)
	}
	id, err := database.InsertOriginal(&db.OriginalRecord{
		JobID: jobID, OriginalPath: original, HeldPath: held,
		OutputPath: original, ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("/api/v1/originals/%d/restore", id)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("POST", url, strings.NewReader(`{"exclude":`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(held); err != nil {
		t.Fatalf("invalid request moved held file: %v", err)
	}
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Fatalf("invalid request changed restore destination: %v", err)
	}

	// An absent body is still allowed. A coincident output path must not delete
	// the just-restored original.
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("POST", url, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("empty body: %d %s", w.Code, w.Body.String())
	}
	data, err := os.ReadFile(original)
	if err != nil || string(data) != "original" {
		t.Fatalf("restored original missing or changed: %q, %v", data, err)
	}
}
