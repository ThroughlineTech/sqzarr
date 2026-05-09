package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danrichardson/sqzarr/internal/api"
	"github.com/danrichardson/sqzarr/internal/config"
	"github.com/danrichardson/sqzarr/internal/db"
	"github.com/danrichardson/sqzarr/internal/queue"
	"github.com/danrichardson/sqzarr/internal/scanner"
	"github.com/danrichardson/sqzarr/internal/transcoder"
	"github.com/danrichardson/sqzarr/internal/testutil"
)

func newTestServer(t *testing.T) (*api.Server, *db.DB) {
	t.Helper()
	database := testutil.NewTestDB(t)
	cfg := config.Defaults()
	cfg.Server.DataDir = t.TempDir()
	cfg.Auth.JWTSecret = "test-secret"

	enc := &transcoder.Encoder{
		Type:        transcoder.EncoderSoftware,
		DisplayName: "Software (test)",
		BuildArgs:   func(in, out string) []string { return nil },
	}
	log := slog.Default()
	tc := transcoder.New(enc, "", log)
	worker := queue.New(database, cfg, tc, nil, log)
	scan := scanner.New(database, cfg.Safety.ProcessedDirName, log)
	allEncoders := []*transcoder.Encoder{enc}
	return api.New(cfg, "", database, worker, scan, nil, enc, allEncoders, tc, log), database
}

func TestGetStatus(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if _, ok := result["version"]; !ok {
		t.Error("expected version field in status response")
	}
}

func TestJobsEndpoint(t *testing.T) {
	srv, database := newTestServer(t)

	dir := t.TempDir()
	database.InsertDirectory(&db.Directory{
		Path:       dir,
		Enabled:    true,
		MinAgeDays: 7,
		MaxBitrate: 4_000_000,
	})

	body, _ := json.Marshal(map[string]string{"path": dir + "/test.mkv"})
	req := httptest.NewRequest("POST", "/api/v1/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	req2 := httptest.NewRequest("GET", "/api/v1/jobs", nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 from GET /jobs, got %d", w2.Code)
	}
}

func TestCancelJob(t *testing.T) {
	srv, database := newTestServer(t)

	jobID, _ := database.InsertJob(&db.Job{
		SourcePath:     "/media/Videos/test.mkv",
		SourceSize:     100,
		SourceCodec:    "h264",
		SourceDuration: 30,
		SourceBitrate:  8_000_000,
		Status:         db.JobPending,
	})

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/jobs/%d", jobID), nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	job, _ := database.GetJob(jobID)
	if job.Status != db.JobCancelled {
		t.Errorf("expected job status cancelled, got %s", job.Status)
	}
}

func TestPathTraversalBlocked(t *testing.T) {
	srv, _ := newTestServer(t)

	body, _ := json.Marshal(map[string]string{"path": "../etc/passwd"})
	req := httptest.NewRequest("POST", "/api/v1/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for path traversal, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSavingsBreakdownMatchesTotal(t *testing.T) {
	srv, database := newTestServer(t)

	// Create some completed jobs with savings
	jobs := []struct {
		path       string
		sourceSize int64
		outputSize int64
		bytesSaved int64
	}{
		{"/media/movie1.mkv", 1000000, 600000, 400000},
		{"/media/movie2.mkv", 2000000, 1200000, 800000},
		{"/media/movie3.mkv", 500000, 450000, 50000},
	}

	for _, j := range jobs {
		id, err := database.InsertJob(&db.Job{
			SourcePath:    j.path,
			SourceSize:    j.sourceSize,
			SourceCodec:   "h264",
			SourceBitrate: 8_000_000,
			Status:        db.JobPending,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := database.CompleteJob(id, j.path+".out", j.outputSize, "sw", j.bytesSaved); err != nil {
			t.Fatal(err)
		}
	}

	// Get total from /status
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /status: expected 200, got %d", w.Code)
	}
	var statusResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &statusResp); err != nil {
		t.Fatal(err)
	}
	totalGB := statusResp["total_saved_gb"].(float64)
	totalFromStatus := int64(totalGB * 1024 * 1024 * 1024)

	// Get breakdown from /jobs/savings
	req2 := httptest.NewRequest("GET", "/api/v1/jobs/savings", nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET /jobs/savings: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var entries []struct {
		BytesSaved int64 `json:"bytes_saved"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}

	var sumFromBreakdown int64
	for _, e := range entries {
		sumFromBreakdown += e.BytesSaved
	}

	// The total from /status is converted through float64 GB, so allow small rounding delta
	expectedTotal := int64(400000 + 800000 + 50000)
	if sumFromBreakdown != expectedTotal {
		t.Errorf("breakdown sum = %d, expected %d", sumFromBreakdown, expectedTotal)
	}
	// Verify /status total is consistent (within float64 rounding of GB conversion)
	if abs64(totalFromStatus-expectedTotal) > 1 {
		t.Errorf("status total_saved_gb converted = %d, expected ~%d", totalFromStatus, expectedTotal)
	}
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func TestRetryJob(t *testing.T) {
	srv, database := newTestServer(t)

	insertJob := func(status db.JobStatus) int64 {
		id, err := database.InsertJob(&db.Job{
			SourcePath:     "/media/Videos/test.mkv",
			SourceSize:     100,
			SourceCodec:    "h264",
			SourceDuration: 30,
			SourceBitrate:  8_000_000,
			Status:         db.JobPending,
		})
		if err != nil {
			t.Fatal(err)
		}
		if status != db.JobPending {
			if err := database.UpdateJobStatus(id, status, "test error"); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}

	retryable := []db.JobStatus{db.JobFailed, db.JobError, db.JobCancelled}
	for _, status := range retryable {
		id := insertJob(status)
		req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/jobs/%d/retry", id), nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Errorf("status %q: expected 204, got %d: %s", status, w.Code, w.Body.String())
		}
		job, _ := database.GetJob(id)
		if job.Status != db.JobPending {
			t.Errorf("status %q: expected job to be pending after retry, got %s", status, job.Status)
		}
	}

	// Non-retryable status must return 400.
	id := insertJob(db.JobDone)
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/jobs/%d/retry", id), nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("done job: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBatchCreateDirectories(t *testing.T) {
	srv, database := newTestServer(t)

	// Create two temp directories to use as paths.
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Set root_dirs so path validation passes — use parent of temp dirs.
	// TempDir paths are unique, so set roots broadly.
	cfg := config.Defaults()
	cfg.Scanner.RootDirs = []string{dir1, dir2}
	cfg.Server.DataDir = t.TempDir()
	cfg.Auth.JWTSecret = "test-secret"
	log := slog.Default()
	enc := &transcoder.Encoder{Type: transcoder.EncoderSoftware, DisplayName: "test",
		BuildArgs: func(in, out string) []string { return nil }}
	tc := transcoder.New(enc, "", log)
	worker := queue.New(database, cfg, tc, nil, log)
	scan := scanner.New(database, cfg.Safety.ProcessedDirName, log)
	srv = api.New(cfg, "", database, worker, scan, nil, enc, []*transcoder.Encoder{enc}, tc, log)

	t.Run("happy path", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"paths":       []string{dir1, dir2},
			"enabled":     true,
			"min_age_days": 14,
			"max_bitrate":  3000000,
			"min_size_mb":  200,
		})
		req := httptest.NewRequest("POST", "/api/v1/directories/batch", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var created []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		if len(created) != 2 {
			t.Errorf("expected 2 directories, got %d", len(created))
		}
	})

	t.Run("empty paths rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"paths": []string{}})
		req := httptest.NewRequest("POST", "/api/v1/directories/batch", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for empty paths, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid path rolls back all", func(t *testing.T) {
		dir3 := t.TempDir()
		body, _ := json.Marshal(map[string]any{
			"paths":       []string{dir3, "/nonexistent/path/abc123"},
			"min_age_days": 5,
		})
		req := httptest.NewRequest("POST", "/api/v1/directories/batch", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid path, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestAuthRequired(t *testing.T) {
	database := testutil.NewTestDB(t)
	cfg := config.Defaults()
	cfg.Server.DataDir = t.TempDir()
	cfg.Auth.PasswordHash = "$2a$10$aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cfg.Auth.JWTSecret = "test-secret"

	log := slog.Default()
	enc := &transcoder.Encoder{Type: transcoder.EncoderSoftware, DisplayName: "test",
		BuildArgs: func(in, out string) []string { return nil }}
	tc := transcoder.New(enc, "", log)
	worker := queue.New(database, cfg, tc, nil, log)
	scan := scanner.New(database, cfg.Safety.ProcessedDirName, log)
	srv := api.New(cfg, "", database, worker, scan, nil, enc, []*transcoder.Encoder{enc}, tc, log)

	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", w.Code)
	}
}

func TestAuthLogin(t *testing.T) {
	database := testutil.NewTestDB(t)
	cfg := config.Defaults()
	cfg.Server.DataDir = t.TempDir()
	// bcrypt hash of "testpassword"
	cfg.Auth.PasswordHash = "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi"
	cfg.Auth.JWTSecret = "test-secret"

	log := slog.Default()
	enc := &transcoder.Encoder{Type: transcoder.EncoderSoftware, DisplayName: "test",
		BuildArgs: func(in, out string) []string { return nil }}
	tc := transcoder.New(enc, "", log)
	worker := queue.New(database, cfg, tc, nil, log)
	scan := scanner.New(database, cfg.Safety.ProcessedDirName, log)
	srv := api.New(cfg, "", database, worker, scan, nil, enc, []*transcoder.Encoder{enc}, tc, log)

	// Wrong password — should get 401.
	body := strings.NewReader(`{"password":"wrongpassword"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong password should return 401, got %d: %s", w.Code, w.Body)
	}
}
