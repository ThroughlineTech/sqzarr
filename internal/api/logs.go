package api

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// maxLogReadBytes bounds how much of the log file is read from the end when
// tailing it, so an un-cleared log can't blow up server memory on GET /logs.
const maxLogReadBytes = 2 * 1024 * 1024

// GET /logs?n=500 — recent lines from the persistent application log file
// (the same JSON lines also sent to stdout/journald).
func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	n := 500
	if q := r.URL.Query().Get("n"); q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 5000 {
			n = v
		}
	}
	lines, err := tailFile(s.logPath, n)
	if err != nil {
		jsonError(w, "cannot read log file", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"lines": lines})
}

// POST /logs/clear — truncate the persistent application log file.
func (s *Server) handleClearLogs(w http.ResponseWriter, r *http.Request) {
	if err := os.Truncate(s.logPath, 0); err != nil && !os.IsNotExist(err) {
		jsonError(w, "cannot clear log file", http.StatusInternalServerError)
		return
	}
	s.log.Info("log file cleared")
	jsonOK(w, map[string]string{"status": "cleared"})
}

// tailFile returns up to the last n newline-delimited lines of path, reading
// at most maxLogReadBytes from the end of the file.
func tailFile(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	offset := int64(0)
	if info.Size() > maxLogReadBytes {
		offset = info.Size() - maxLogReadBytes
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return []string{}, nil
	}
	lines := strings.Split(trimmed, "\n")
	if offset > 0 && len(lines) > 1 {
		lines = lines[1:] // drop the partial line left by a mid-file seek
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
