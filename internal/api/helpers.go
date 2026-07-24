package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func pathID(r *http.Request) (int64, bool) {
	s := r.PathValue("id")
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil
}

// safePathIn checks that path is safe to enqueue: it must fall under either
// a configured root directory (the same boundary the file browser and
// directory picker use, cfg.Scanner.RootDirs) or a registered scan directory
// (db.directories). Root dirs are checked first because every registered
// scan directory is already required to live under one (see
// validateDirPath) — so a file the browser can show you should always be
// queueable, even if its specific subfolder was never registered as an
// automated scan directory.
func (s *Server) safePathIn(path string) bool {
	if strings.Contains(path, "..") {
		return false
	}
	clean := filepath.Clean(path)
	if pathUnderAnyRoot(clean, s.cfg.Scanner.RootDirs) {
		return true
	}
	dirs, err := s.db.ListDirectories()
	if err != nil {
		return false
	}
	for _, d := range dirs {
		if pathUnderAnyRoot(clean, []string{filepath.Clean(d.Path)}) {
			return true
		}
	}
	return false
}
