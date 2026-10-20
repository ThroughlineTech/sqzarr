//go:build integration

package scanner_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/danrichardson/sqzarr/internal/db"
	"github.com/danrichardson/sqzarr/internal/scanner"
	"github.com/danrichardson/sqzarr/internal/testutil"
)

func testLog(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.Default()
}

func TestScanFindsH264File(t *testing.T) {
	database := testutil.NewTestDB(t)
	dir := t.TempDir()

	clipPath := testutil.MakeTestClip(t, dir, 30)
	past := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(clipPath, past, past)

	dirID, err := database.InsertDirectory(&db.Directory{
		Path:       dir,
		Enabled:    true,
		MinAgeDays: 7,
		MaxBitrate: 0, // disabled — test clip bitrate is not deterministic
		MinSizeMB:  0,
	})
	if err != nil {
		t.Fatal(err)
	}

	dbDir, _ := database.GetDirectory(dirID)
	s := scanner.New(database, ".processed", testLog(t))
	result, err := s.ScanDirectory(context.Background(), dbDir)
	if err != nil {
		t.Fatal(err)
	}

	if result.FilesQueued != 1 {
		t.Errorf("expected 1 queued, got %d", result.FilesQueued)
	}

	jobs, err := database.ListJobs(db.JobPending, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 pending job, got %d", len(jobs))
	}
	if jobs[0].SourcePath != clipPath {
		t.Errorf("expected source path %s, got %s", clipPath, jobs[0].SourcePath)
	}
}

// TestScannerEnqueuesOversizedHEVC verifies that an HEVC file above the bitrate
// threshold IS enqueued — codec alone must not cause a skip.
func TestScannerEnqueuesOversizedHEVC(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}

	database := testutil.NewTestDB(t)
	dir := t.TempDir()

	// A requested ABR is not a guaranteed bitrate for a simple test pattern.
	// Seeded temporal noise plus lossless HEVC makes this small fixture reliably
	// exceed the scan threshold without a costly 1080p encode.
	clipPath := filepath.Join(dir, "hevc_big.mkv")
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "lavfi",
		"-i", "testsrc2=duration=3:size=640x360:rate=25,noise=alls=50:allf=t+u:all_seed=42",
		"-c:v", "libx265", "-preset", "ultrafast",
		"-x265-params", "lossless=1:pools=2:frame-threads=1",
		clipPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot create HEVC test clip: %v\n%s", err, out)
	}
	const threshold = 2_222_000
	probe, err := exec.Command("ffprobe", "-v", "error", "-show_streams", "-show_format", "-of", "json", clipPath).Output()
	if err != nil {
		t.Fatal(err)
	}
	var measured struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
		} `json:"streams"`
		Format struct {
			BitRate string `json:"bit_rate"`
		} `json:"format"`
	}
	if err := json.Unmarshal(probe, &measured); err != nil {
		t.Fatal(err)
	}
	bitrate, err := strconv.ParseInt(measured.Format.BitRate, 10, 64)
	if err != nil || bitrate <= threshold || len(measured.Streams) != 1 || measured.Streams[0].CodecName != "hevc" {
		t.Fatalf("invalid oversized HEVC fixture: measured bitrate=%d threshold=%d parse error=%v", bitrate, threshold, err)
	}

	past := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(clipPath, past, past)

	dirID, _ := database.InsertDirectory(&db.Directory{
		Path:       dir,
		Enabled:    true,
		MinAgeDays: 7,
		MaxBitrate: threshold, // fixture bitrate asserted above
	})
	dbDir, _ := database.GetDirectory(dirID)

	s := scanner.New(database, ".processed", testLog(t))
	result, _ := s.ScanDirectory(context.Background(), dbDir)

	if result.FilesQueued != 1 {
		t.Errorf("expected oversized HEVC to be queued, got FilesQueued=%d", result.FilesQueued)
	}
}

// TestScannerSkipsUndersizedHEVC verifies that an HEVC file below the bitrate
// threshold is NOT enqueued (bitrate gate works regardless of codec).
func TestScannerSkipsUndersizedHEVC(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}

	database := testutil.NewTestDB(t)
	dir := t.TempDir()

	clipPath := filepath.Join(dir, "hevc_small.mkv")
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "lavfi",
		"-i", "testsrc=duration=10:size=640x480:rate=25",
		"-c:v", "libx265", "-crf", "40",
		clipPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot create HEVC test clip: %v\n%s", err, out)
	}

	past := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(clipPath, past, past)

	dirID, _ := database.InsertDirectory(&db.Directory{
		Path:       dir,
		Enabled:    true,
		MinAgeDays: 7,
		MaxBitrate: 2_222_000,
	})
	dbDir, _ := database.GetDirectory(dirID)

	s := scanner.New(database, ".processed", testLog(t))
	result, _ := s.ScanDirectory(context.Background(), dbDir)

	if result.FilesQueued != 0 {
		t.Errorf("expected low-bitrate HEVC to be skipped, got FilesQueued=%d", result.FilesQueued)
	}
}

// TestScannerEnqueuesOversizedAV1 verifies that AV1 files are subject to the
// same bitrate gate as any other codec.
func TestScannerEnqueuesOversizedAV1(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}

	database := testutil.NewTestDB(t)
	dir := t.TempDir()

	clipPath := filepath.Join(dir, "av1_big.mkv")
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "lavfi",
		"-i", "testsrc2=duration=2:size=320x180:rate=25,noise=alls=50:allf=t+u:all_seed=42",
		"-c:v", "libaom-av1", "-crf", "0", "-b:v", "0", "-cpu-used", "8",
		"-threads", "2", "-row-mt", "1",
		clipPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot create AV1 test clip (libaom-av1 may not be available): %v\n%s", err, out)
	}
	// Requested ABR on a simple pattern can undershoot. Check the actual
	// lossless noisy fixture before exercising the scanner's bitrate gate.
	const threshold = 2_222_000
	probe, err := exec.Command("ffprobe", "-v", "error", "-show_streams", "-show_format", "-of", "json", clipPath).Output()
	if err != nil {
		t.Fatal(err)
	}
	var measured struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
		} `json:"streams"`
		Format struct {
			BitRate string `json:"bit_rate"`
		} `json:"format"`
	}
	if err := json.Unmarshal(probe, &measured); err != nil {
		t.Fatal(err)
	}
	bitrate, err := strconv.ParseInt(measured.Format.BitRate, 10, 64)
	if err != nil || bitrate <= threshold || len(measured.Streams) != 1 || measured.Streams[0].CodecName != "av1" {
		t.Fatalf("invalid oversized AV1 fixture: measured bitrate=%d threshold=%d parse error=%v", bitrate, threshold, err)
	}

	past := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(clipPath, past, past)

	dirID, _ := database.InsertDirectory(&db.Directory{
		Path:       dir,
		Enabled:    true,
		MinAgeDays: 7,
		MaxBitrate: threshold,
	})
	dbDir, _ := database.GetDirectory(dirID)

	s := scanner.New(database, ".processed", testLog(t))
	result, _ := s.ScanDirectory(context.Background(), dbDir)

	if result.FilesQueued != 1 {
		t.Errorf("expected oversized AV1 to be queued, got FilesQueued=%d", result.FilesQueued)
	}
}

func TestScanSkipsNewFile(t *testing.T) {
	database := testutil.NewTestDB(t)
	dir := t.TempDir()

	testutil.MakeTestClip(t, dir, 10)
	// mtime is now — must fail the min_age_days=7 check.

	dirID, _ := database.InsertDirectory(&db.Directory{
		Path:       dir,
		Enabled:    true,
		MinAgeDays: 7,
		MaxBitrate: 0, // disabled — we're testing age, not bitrate
	})
	dbDir, _ := database.GetDirectory(dirID)

	s := scanner.New(database, ".processed", testLog(t))
	result, _ := s.ScanDirectory(context.Background(), dbDir)

	if result.FilesQueued != 0 {
		t.Errorf("expected 0 queued for new file, got %d", result.FilesQueued)
	}
}

func TestScanDeduplicates(t *testing.T) {
	database := testutil.NewTestDB(t)
	dir := t.TempDir()

	clipPath := testutil.MakeTestClip(t, dir, 30)
	past := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(clipPath, past, past)

	dirID, _ := database.InsertDirectory(&db.Directory{
		Path:       dir,
		Enabled:    true,
		MinAgeDays: 7,
		MaxBitrate: 0, // disabled
	})
	dbDir, _ := database.GetDirectory(dirID)
	s := scanner.New(database, ".processed", testLog(t))

	s.ScanDirectory(context.Background(), dbDir)
	result, _ := s.ScanDirectory(context.Background(), dbDir)

	if result.FilesQueued != 0 {
		t.Errorf("expected 0 queued on second scan, got %d", result.FilesQueued)
	}
}

// TestClearHistoryDoesNotRequeueExcluded verifies that excluded files with a
// processed_files record are NOT re-queued after ClearHistory deletes the
// job rows.
func TestClearHistoryDoesNotRequeueExcluded(t *testing.T) {
	database := testutil.NewTestDB(t)
	dir := t.TempDir()

	clipPath := testutil.MakeTestClip(t, dir, 30)
	past := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(clipPath, past, past)

	dirID, _ := database.InsertDirectory(&db.Directory{
		Path:       dir,
		Enabled:    true,
		MinAgeDays: 7,
		MaxBitrate: 0,
	})
	dbDir, _ := database.GetDirectory(dirID)
	s := scanner.New(database, ".processed", testLog(t))

	// First scan queues the file.
	s.ScanDirectory(context.Background(), dbDir)

	// Simulate the worker excluding the file: mark the job as excluded and
	// upsert a durable processed_files record.
	jobs, _ := database.ListJobs(db.JobPending, 10, 0)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 pending job, got %d", len(jobs))
	}
	database.ExcludeJob(jobs[0].ID, "uncompressible")

	fi, _ := os.Stat(clipPath)
	database.UpsertProcessedFile(clipPath, "excluded", "uncompressible", fi.Size(), fi.ModTime())

	// Clear history — this deletes the excluded job row from the jobs table.
	n, err := database.ClearHistory()
	if err != nil {
		t.Fatalf("clear history: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 deleted, got %d", n)
	}

	// Scan again — the file should NOT be re-queued because the
	// processed_files record still exists.
	result, err := s.ScanDirectory(context.Background(), dbDir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if result.FilesQueued != 0 {
		t.Errorf("expected 0 queued after clear+rescan, got %d (bug: excluded file re-queued)", result.FilesQueued)
	}
}
