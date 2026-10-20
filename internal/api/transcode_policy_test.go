package api_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danrichardson/sqzarr/internal/db"
)

func TestTranscodePolicyAPI(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, body := range []string{
		`{"hardware_decode":"force","low_power":"off"}`,
		`{"hardware_decode":"off","low_power":"cqp"}`,
	} {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest("PUT", "/api/v1/config", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("invalid policies accepted: %d %s", w.Code, w.Body.String())
		}
	}
	get := func() map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/config", nil))
		var cfg map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	cfg := get()
	if cfg["hardware_decode"] != "auto" || cfg["low_power"] != "auto" {
		t.Fatalf("invalid request partially changed policies: %v", cfg)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("PUT", "/api/v1/config", strings.NewReader(`{"hardware_decode":"off","low_power":"off"}`)))
	if w.Code != 200 {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	cfg = get()
	if cfg["hardware_decode"] != "off" || cfg["low_power"] != "off" || cfg["low_power_status"] != "off" {
		t.Fatalf("unexpected policies/status: %v", cfg)
	}
}

func TestManualEnqueueRejectsScratch(t *testing.T) {
	srv, database := newTestServer(t)
	root := t.TempDir()
	_, err := database.InsertDirectory(&db.Directory{Path: root, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"movie.sqzarr-tmp.mkv", "movie.sqzarr-tmp-12345.mkv"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("partial"), 0600); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]string{"path": path})
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/jobs", strings.NewReader(string(body))))
		if w.Code != 400 {
			t.Fatalf("scratch accepted: %d %s", w.Code, w.Body.String())
		}
	}
	body, _ := json.Marshal(map[string]string{"path": root})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/jobs/enqueue-dir", strings.NewReader(string(body))))
	if w.Code != 200 {
		t.Fatalf("directory enqueue: %d %s", w.Code, w.Body.String())
	}
	var counts map[string]int
	if err := json.Unmarshal(w.Body.Bytes(), &counts); err != nil {
		t.Fatal(err)
	}
	if counts["queued"] != 0 || counts["skipped"] != 2 {
		t.Fatalf("directory queued scratch: %v", counts)
	}
}
