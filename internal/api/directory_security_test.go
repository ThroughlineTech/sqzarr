package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danrichardson/sqzarr/internal/config"
)

func TestDirectoryProbeDoesNotFollowPredictableSentinel(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "must-not-change")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, ".sqzarr_write_test")
	if err := os.Symlink(target, sentinel); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Scanner.RootDirs = []string{root}
	srv := &Server{cfg: cfg}
	if msg, code := srv.validateDirPath(root); msg != "" {
		t.Fatalf("validate directory: %d %s", code, msg)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "keep" {
		t.Fatalf("sentinel target was modified: %q, %v", data, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != ".sqzarr_write_test" {
		t.Fatalf("probe left files behind or removed existing sentinel: %v", entries)
	}
}
