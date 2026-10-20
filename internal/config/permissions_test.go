package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUpdateFileRestrictsExistingConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file mode enforcement")
	}
	path := filepath.Join(t.TempDir(), "sqzarr.toml")
	if err := os.WriteFile(path, []byte("# keep this comment\n[plex]\ntoken = \"old\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpdateFile(path, map[string]string{"token": `"new"`}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config containing secrets has mode %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# keep this comment") || !strings.Contains(string(data), `token = "new"`) {
		t.Fatalf("config contents not preserved/updated: %s", data)
	}
}
