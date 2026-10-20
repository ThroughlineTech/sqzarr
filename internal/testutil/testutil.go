package testutil

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/danrichardson/sqzarr/internal/db"
)

// NewTestDB creates an in-memory SQLite DB with schema applied.
func NewTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("testutil: open db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	return database
}

// MakeTestClip creates a short H.264 test video in dir using ffmpeg.
// Requires ffmpeg on PATH. Skips the test if ffmpeg is unavailable.
// Returns the path to the created file.
func MakeTestClip(t *testing.T, dir string, durationSecs int) string {
	t.Helper()

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH, skipping integration test")
	}

	path := filepath.Join(dir, "test.mkv")
	// #nosec G204 -- test fixture uses a fixed executable, numeric duration and test-owned output path; no shell.
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "lavfi",
		"-i", "testsrc=duration="+strconv.Itoa(durationSecs)+":size=640x480:rate=25",
		"-c:v", "libx264",
		"-b:v", "8000k",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create test clip: %v\n%s", err, out)
	}
	return path
}
