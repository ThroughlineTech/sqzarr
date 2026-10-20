package scanner_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/danrichardson/sqzarr/internal/db"
	"github.com/danrichardson/sqzarr/internal/scanner"
	"github.com/danrichardson/sqzarr/internal/testutil"
)

func TestScanExcludesScratchVideoBeforeProbing(t *testing.T) {
	database := testutil.NewTestDB(t)
	root := t.TempDir()
	for _, name := range []string{
		"movie.sqzarr-tmp.mkv",
		"movie.sqzarr-tmp-123456789.mkv",
		"movie.SQZARR-TMP-234.MP4",
		"movie.sqzarr-tmp-story.mkv", // a similarly named real video remains eligible
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("not yet a video"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	id, err := database.InsertDirectory(&db.Directory{Path: root, Enabled: true, MinAgeDays: 0, MinSizeMB: 1})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := database.GetDirectory(id)
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.New(database, ".processed", slog.Default()).ScanDirectory(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	// Only the real filename should reach eligibility checking. Its size skips
	// ffprobe, making the test independent of the workstation's media tools.
	if result.FilesScanned != 1 || result.FilesSkipped != 1 || result.FilesQueued != 0 {
		t.Fatalf("scratch files reached eligibility checking: %+v", result)
	}
}
