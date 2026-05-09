package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/danrichardson/sqzarr/internal/config"
	"github.com/danrichardson/sqzarr/internal/db"
	"github.com/danrichardson/sqzarr/internal/transcoder"
)

// GET /status
func (s *Server) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.GetStats()
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}

	diskFree, diskPath := s.diskFreeGB()
	cpuPct, gpuMHz, gpuPct := s.sysStats()
	s.mu.Lock()
	encoderName := s.encoder.DisplayName
	s.mu.Unlock()
	out := map[string]any{
		"version":            "1.0.0",
		"encoder":            encoderName,
		"paused":             s.worker.IsPaused(),
		"total_saved_gb":     float64(stats.TotalBytesSaved) / (1024 * 1024 * 1024),
		"jobs_done":          stats.TotalJobsDone,
		"jobs_failed":        stats.TotalJobsFailed,
		"disk_free_gb": diskFree,
		"disk_path":    diskPath,
		"cpu_percent":  cpuPct,
		"gpu_mhz":            gpuMHz,
		"gpu_percent":        gpuPct, // -1 = unavailable, use gpu_mhz instead
	}
	if s.sched != nil {
		next := s.sched.NextScanAt()
		out["next_scan_at"] = next
		if last := s.sched.LastScanAt(); last != nil {
			out["last_scan_at"] = last
		} else {
			out["last_scan_at"] = nil
		}
	}
	jsonOK(w, out)
}

// GET /stats
func (s *Server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.GetStats()
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, stats)
}

// GET /jobs
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	statusFilter := db.JobStatus(r.URL.Query().Get("status"))
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 50
	offset := 0
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}
	if offsetStr != "" {
		if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
			offset = v
		}
	}

	jobs, err := s.db.ListJobs(statusFilter, limit, offset)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, jobs)
}

// GET /jobs/savings — per-file savings breakdown
func (s *Server) handleListSavings(w http.ResponseWriter, r *http.Request) {
	entries, err := s.db.ListSavingsBreakdown()
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []*db.SavingsEntry{}
	}
	jsonOK(w, entries)
}

// POST /jobs — manually enqueue a file
func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		jsonError(w, "path is required", http.StatusBadRequest)
		return
	}
	if !s.safePathIn(req.Path) {
		jsonError(w, "path is not within a configured directory", http.StatusBadRequest)
		return
	}

	exists, _ := s.db.SourcePathExists(req.Path)
	if exists {
		jsonError(w, "job already exists for this path", http.StatusConflict)
		return
	}

	id, err := s.db.InsertJob(&db.Job{
		SourcePath: req.Path,
		Status:     db.JobPending,
		Priority:   1, // manual jobs get slightly higher priority
	})
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	job, _ := s.db.GetJob(id)
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, job)
}

// GET /jobs/{id}
func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		jsonError(w, "invalid job id", http.StatusBadRequest)
		return
	}
	job, err := s.db.GetJob(id)
	if err != nil || job == nil {
		jsonError(w, "job not found", http.StatusNotFound)
		return
	}
	jsonOK(w, job)
}

// DELETE /jobs/{id} — cancel a pending or running job
func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		jsonError(w, "invalid job id", http.StatusBadRequest)
		return
	}
	job, err := s.db.GetJob(id)
	if err != nil || job == nil {
		jsonError(w, "job not found", http.StatusNotFound)
		return
	}
	switch job.Status {
	case db.JobPending:
		s.db.UpdateJobStatus(id, db.JobCancelled, "cancelled by user")
	case db.JobRunning:
		// Signal the running ffmpeg process to stop; the worker goroutine
		// will update the status once it exits.
		if !s.worker.CancelJob(id) {
			// Race: job just finished — treat as already done.
			jsonError(w, "job is no longer running", http.StatusConflict)
			return
		}
	default:
		jsonError(w, "only pending or running jobs can be cancelled", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /jobs/enqueue-dir — recursively enqueue all video files under a directory
func (s *Server) handleEnqueueDir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		jsonError(w, "path is required", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(req.Path)
	if err != nil {
		jsonError(w, "invalid path", http.StatusBadRequest)
		return
	}
	if !s.safePathIn(abs) {
		jsonError(w, "path is not within a configured directory", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		jsonError(w, "path is not a directory", http.StatusBadRequest)
		return
	}

	var queued, skipped int
	err = filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if !videoExtensions[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		exists, _ := s.db.SourcePathExists(path)
		if exists {
			skipped++
			return nil
		}
		if _, err := s.db.InsertJob(&db.Job{
			SourcePath: path,
			Status:     db.JobPending,
			Priority:   1,
		}); err == nil {
			queued++
		}
		return nil
	})
	if err != nil {
		jsonError(w, "error walking directory", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]int{"queued": queued, "skipped": skipped})
}

// POST /files/reprocess — remove a file's processed_files record so it can be re-queued.
// Body: {"path": "/path/to/file"}
func (s *Server) handleReprocessFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		jsonError(w, "path is required", http.StatusBadRequest)
		return
	}
	pf, err := s.db.GetProcessedFile(req.Path)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	if pf == nil {
		jsonError(w, "file not found in processed records", http.StatusNotFound)
		return
	}
	if err := s.db.DeleteProcessedFile(req.Path); err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "ok", "path": req.Path})
}

// POST /jobs/clear — delete failed/cancelled/skipped/released jobs from history
func (s *Server) handleClearHistory(w http.ResponseWriter, r *http.Request) {
	n, err := s.db.ClearHistory()
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]int{"cleared": n})
}

// POST /jobs/{id}/retry
func (s *Server) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		jsonError(w, "invalid job id", http.StatusBadRequest)
		return
	}
	job, err := s.db.GetJob(id)
	if err != nil || job == nil {
		jsonError(w, "job not found", http.StatusNotFound)
		return
	}
	if job.Status != db.JobFailed && job.Status != db.JobCancelled && job.Status != db.JobError {
		jsonError(w, "only failed, error, or cancelled jobs can be retried", http.StatusBadRequest)
		return
	}
	s.db.UpdateJobStatus(id, db.JobPending, "")
	w.WriteHeader(http.StatusNoContent)
}

// GET /jobs/{id}/log — recent ffmpeg diagnostic lines for an active job
func (s *Server) handleGetJobLog(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		jsonError(w, "invalid job id", http.StatusBadRequest)
		return
	}
	n := 20
	if q := r.URL.Query().Get("n"); q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 200 {
			n = v
		}
	}
	lines := s.worker.RecentLog(id, n)
	if lines == nil {
		lines = []string{}
	}
	jsonOK(w, map[string]any{"lines": lines})
}

// GET /directories
func (s *Server) handleListDirectories(w http.ResponseWriter, r *http.Request) {
	dirs, err := s.db.ListDirectories()
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, dirs)
}

// validateDirPath checks that path is safe to use as a scan directory:
// it must exist, be a directory, be writable, and — if root_dirs are configured —
// fall within one of them.
func (s *Server) validateDirPath(path string) (string, int) {
	if path == "" {
		return "path is required", http.StatusBadRequest
	}
	if strings.Contains(path, "..") {
		return "path must not contain ..", http.StatusBadRequest
	}

	// Root-dirs constraint: if any roots are configured, path must be within one.
	if len(s.cfg.Scanner.RootDirs) > 0 {
		if !pathUnderAnyRoot(path, s.cfg.Scanner.RootDirs) {
			return "path is outside all configured root directories", http.StatusBadRequest
		}
	} else {
		// No roots configured — refuse to save any directory until roots are set.
		return "configure root directories in Settings before adding scan directories", http.StatusBadRequest
	}

	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "directory does not exist", http.StatusBadRequest
		}
		return "cannot access path: " + err.Error(), http.StatusBadRequest
	}
	if !fi.IsDir() {
		return "path is not a directory", http.StatusBadRequest
	}

	// Writability check: try creating and removing a sentinel file.
	sentinel := filepath.Join(path, ".sqzarr_write_test")
	f, err := os.Create(sentinel)
	if err != nil {
		return "directory is not writable", http.StatusBadRequest
	}
	f.Close()
	os.Remove(sentinel)

	return "", 0
}

// POST /directories
func (s *Server) handleCreateDirectory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path              string   `json:"path"`
		Enabled           *bool    `json:"enabled"`
		MinAgeDays        int      `json:"min_age_days"`
		MaxBitrate        int64    `json:"max_bitrate"`
		MinSizeMB         int64    `json:"min_size_mb"`
		BitrateSkipMargin *float64 `json:"bitrate_skip_margin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if msg, code := s.validateDirPath(req.Path); msg != "" {
		jsonError(w, msg, code)
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.MinAgeDays == 0 {
		req.MinAgeDays = 7
	}
	if req.MaxBitrate == 0 {
		req.MaxBitrate = 2_222_000
	}
	if req.MinSizeMB == 0 {
		req.MinSizeMB = 500
	}
	bitrateSkipMargin := 0.10
	if req.BitrateSkipMargin != nil {
		bitrateSkipMargin = *req.BitrateSkipMargin
	}

	dir := &db.Directory{
		Path:              req.Path,
		Enabled:           enabled,
		MinAgeDays:        req.MinAgeDays,
		MaxBitrate:        req.MaxBitrate,
		MinSizeMB:         req.MinSizeMB,
		BitrateSkipMargin: bitrateSkipMargin,
	}
	id, err := s.db.InsertDirectory(dir)
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	created, _ := s.db.GetDirectory(id)
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, created)
}

// POST /directories/batch
func (s *Server) handleBatchCreateDirectories(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths             []string `json:"paths"`
		Enabled           *bool    `json:"enabled"`
		MinAgeDays        int      `json:"min_age_days"`
		MaxBitrate        int64    `json:"max_bitrate"`
		MinSizeMB         int64    `json:"min_size_mb"`
		BitrateSkipMargin *float64 `json:"bitrate_skip_margin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Paths) == 0 {
		jsonError(w, "paths is required and must not be empty", http.StatusBadRequest)
		return
	}

	// Validate every path before inserting any.
	for _, p := range req.Paths {
		if msg, code := s.validateDirPath(p); msg != "" {
			jsonError(w, p+": "+msg, code)
			return
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.MinAgeDays == 0 {
		req.MinAgeDays = 7
	}
	if req.MaxBitrate == 0 {
		req.MaxBitrate = 2_222_000
	}
	if req.MinSizeMB == 0 {
		req.MinSizeMB = 500
	}
	bitrateSkipMargin := 0.10
	if req.BitrateSkipMargin != nil {
		bitrateSkipMargin = *req.BitrateSkipMargin
	}

	dirs := make([]*db.Directory, len(req.Paths))
	for i, p := range req.Paths {
		dirs[i] = &db.Directory{
			Path:              p,
			Enabled:           enabled,
			MinAgeDays:        req.MinAgeDays,
			MaxBitrate:        req.MaxBitrate,
			MinSizeMB:         req.MinSizeMB,
			BitrateSkipMargin: bitrateSkipMargin,
		}
	}

	ids, err := s.db.InsertDirectories(dirs)
	if err != nil {
		jsonError(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	created := make([]*db.Directory, 0, len(ids))
	for _, id := range ids {
		d, _ := s.db.GetDirectory(id)
		if d != nil {
			created = append(created, d)
		}
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, created)
}

// GET /directories/{id}
func (s *Server) handleGetDirectory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	d, err := s.db.GetDirectory(id)
	if err != nil || d == nil {
		jsonError(w, "directory not found", http.StatusNotFound)
		return
	}
	jsonOK(w, d)
}

// PUT /directories/{id}
func (s *Server) handleUpdateDirectory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	d, err := s.db.GetDirectory(id)
	if err != nil || d == nil {
		jsonError(w, "directory not found", http.StatusNotFound)
		return
	}

	var req struct {
		Path              *string  `json:"path"`
		Enabled           *bool    `json:"enabled"`
		MinAgeDays        *int     `json:"min_age_days"`
		MaxBitrate        *int64   `json:"max_bitrate"`
		MinSizeMB         *int64   `json:"min_size_mb"`
		BitrateSkipMargin *float64 `json:"bitrate_skip_margin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Path != nil {
		if msg, code := s.validateDirPath(*req.Path); msg != "" {
			jsonError(w, msg, code)
			return
		}
		d.Path = *req.Path
	}
	if req.Enabled != nil {
		d.Enabled = *req.Enabled
	}
	if req.MinAgeDays != nil {
		d.MinAgeDays = *req.MinAgeDays
	}
	if req.MaxBitrate != nil {
		d.MaxBitrate = *req.MaxBitrate
	}
	if req.MinSizeMB != nil {
		d.MinSizeMB = *req.MinSizeMB
	}
	if req.BitrateSkipMargin != nil {
		d.BitrateSkipMargin = *req.BitrateSkipMargin
	}

	if err := s.db.UpdateDirectory(d); err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	updated, _ := s.db.GetDirectory(id)
	jsonOK(w, updated)
}

// DELETE /directories/{id}
func (s *Server) handleDeleteDirectory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.db.DeleteDirectory(id); err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /scan — trigger an immediate scan
func (s *Server) handleTriggerScan(w http.ResponseWriter, r *http.Request) {
	dirs, err := s.db.ListDirectories()
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	go func() {
		for _, d := range dirs {
			if _, err := s.scanner.ScanDirectory(r.Context(), d); err != nil {
				s.log.Error("scan error", "directory", d.Path, "error", err)
			}
		}
		if s.sched != nil {
			s.sched.RecordManualScan()
		}
	}()
	jsonOK(w, map[string]string{"status": "scan started"})
}

// GET /scan/last — most recent completed scan run
func (s *Server) handleLastScan(w http.ResponseWriter, r *http.Request) {
	run, err := s.db.LastScanRun()
	if err != nil || run == nil {
		jsonOK(w, nil)
		return
	}
	jsonOK(w, run)
}

// persistPaused writes the pause state to the config file so it survives restarts.
func (s *Server) persistPaused(paused bool) {
	s.cfg.Scanner.Paused = paused
	if s.cfgPath != "" {
		if err := config.UpdateFile(s.cfgPath, map[string]string{
			"paused": strconv.FormatBool(paused),
		}); err != nil {
			s.log.Warn("could not persist pause state", "error", err)
		}
	}
}

// POST /queue/pause
func (s *Server) handlePauseQueue(w http.ResponseWriter, r *http.Request) {
	s.worker.SetPaused(true)
	s.persistPaused(true)
	jsonOK(w, map[string]bool{"paused": true})
}

// POST /queue/resume
func (s *Server) handleResumeQueue(w http.ResponseWriter, r *http.Request) {
	s.worker.SetPaused(false)
	s.persistPaused(false)
	jsonOK(w, map[string]bool{"paused": false})
}

// POST /auth/login
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Auth.PasswordHash == "" {
		jsonError(w, "authentication not configured", http.StatusNotFound)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(s.cfg.Auth.PasswordHash),
		[]byte(req.Password),
	); err != nil {
		jsonError(w, "invalid password", http.StatusUnauthorized)
		return
	}

	token, err := s.issueJWT()
	if err != nil {
		jsonError(w, "could not issue token", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"token": token})
}

// POST /auth/change-password — change or set the user's password
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.NewPassword == "" {
		jsonError(w, "new_password is required", http.StatusBadRequest)
		return
	}
	if len(req.NewPassword) < 8 {
		jsonError(w, "new password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	// If a password is already set, verify the current one.
	if s.cfg.Auth.PasswordHash != "" {
		if req.CurrentPassword == "" {
			jsonError(w, "current_password is required", http.StatusBadRequest)
			return
		}
		if err := bcrypt.CompareHashAndPassword(
			[]byte(s.cfg.Auth.PasswordHash),
			[]byte(req.CurrentPassword),
		); err != nil {
			jsonError(w, "current password is incorrect", http.StatusForbidden)
			return
		}
	}

	// Hash the new password.
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		jsonError(w, "failed to hash password", http.StatusInternalServerError)
		return
	}

	// Update in memory.
	s.cfg.Auth.PasswordHash = string(hash)

	// Persist to config file.
	if s.cfgPath != "" {
		if err := config.UpdateFile(s.cfgPath, map[string]string{
			"password_hash": `"` + string(hash) + `"`,
		}); err != nil {
			s.log.Warn("could not persist password change", "error", err)
		}
	}

	jsonOK(w, map[string]string{"status": "password changed"})
}

// DELETE /auth/password — remove the password (requires current password)
func (s *Server) handleRemovePassword(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Auth.PasswordHash == "" {
		jsonError(w, "no password is set", http.StatusBadRequest)
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.CurrentPassword == "" {
		jsonError(w, "current_password is required", http.StatusBadRequest)
		return
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(s.cfg.Auth.PasswordHash),
		[]byte(req.CurrentPassword),
	); err != nil {
		jsonError(w, "current password is incorrect", http.StatusForbidden)
		return
	}

	s.cfg.Auth.PasswordHash = ""

	if s.cfgPath != "" {
		if err := config.UpdateFile(s.cfgPath, map[string]string{
			"password_hash": `""`,
		}); err != nil {
			s.log.Warn("could not persist password removal", "error", err)
		}
	}

	jsonOK(w, map[string]string{"status": "password removed"})
}

// GET /encoders — returns all available encoders with active flag
func (s *Server) handleGetEncoders(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	active := s.encoder.Type
	s.mu.Unlock()

	type encoderInfo struct {
		Type        string `json:"type"`
		DisplayName string `json:"display_name"`
		Active      bool   `json:"active"`
	}
	out := make([]encoderInfo, 0, len(s.availableEncoders))
	for _, enc := range s.availableEncoders {
		out = append(out, encoderInfo{
			Type:        string(enc.Type),
			DisplayName: enc.DisplayName,
			Active:      enc.Type == active,
		})
	}
	jsonOK(w, out)
}

// GET /config — returns runtime-editable settings
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	plexToken := ""
	if s.cfg.Plex.Token != "" {
		plexToken = "SET"
	}
	roots := s.cfg.Scanner.RootDirs
	if roots == nil {
		roots = []string{}
	}
	s.mu.Lock()
	activeEncoder := string(s.encoder.Type)
	s.mu.Unlock()
	jsonOK(w, map[string]any{
		"root_dirs":                  roots,
		"worker_concurrency":         s.cfg.Scanner.WorkerConcurrency,
		"scan_interval_hours":        s.cfg.Scanner.IntervalHours,
		"processed_dir_name":         s.cfg.Safety.ProcessedDirName,
		"originals_retention_days":   s.cfg.Safety.OriginalsRetentionDays,
		"fail_threshold":             s.cfg.Safety.FailThreshold,
		"system_fail_threshold":      s.cfg.Safety.SystemFailThreshold,
		"delete_confirm_single":      s.cfg.Safety.DeleteConfirmSingle,
		"plex_enabled":               s.cfg.Plex.Enabled,
		"plex_base_url":              s.cfg.Plex.BaseURL,
		"plex_token":                 plexToken,
		"encoder":                    activeEncoder,
		"has_password":               s.cfg.Auth.PasswordHash != "",
	})
}

// PUT /config — updates runtime-editable settings in memory and on disk
func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RootDirs                []string `json:"root_dirs"`
		WorkerConcurrency       *int     `json:"worker_concurrency"`
		ScanIntervalHours       *int     `json:"scan_interval_hours"`
		ProcessedDirName        *string  `json:"processed_dir_name"`
		OriginalsRetentionDays  *int     `json:"originals_retention_days"`
		FailThreshold           *int     `json:"fail_threshold"`
		SystemFailThreshold     *int     `json:"system_fail_threshold"`
		DeleteConfirmSingle     *bool    `json:"delete_confirm_single"`
		PlexEnabled             *bool    `json:"plex_enabled"`
		PlexBaseURL             *string  `json:"plex_base_url"`
		PlexToken               *string  `json:"plex_token"`
		Encoder                 *string  `json:"encoder"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	fileUpdates := map[string]string{}

	if req.RootDirs != nil {
		// Validate each root: must exist and be a real directory.
		for _, root := range req.RootDirs {
			if strings.Contains(root, "..") {
				jsonError(w, "root_dirs: path must not contain ..: "+root, http.StatusBadRequest)
				return
			}
			fi, err := os.Stat(root)
			if err != nil {
				jsonError(w, "root_dirs: "+root+": "+err.Error(), http.StatusBadRequest)
				return
			}
			if !fi.IsDir() {
				jsonError(w, "root_dirs: not a directory: "+root, http.StatusBadRequest)
				return
			}
		}
		s.cfg.Scanner.RootDirs = req.RootDirs
		// Encode as TOML inline array: ["a", "b"]
		parts := make([]string, len(req.RootDirs))
		for i, r := range req.RootDirs {
			parts[i] = `"` + strings.ReplaceAll(r, `"`, `\"`) + `"`
		}
		fileUpdates["root_dirs"] = "[" + strings.Join(parts, ", ") + "]"
	}

	if req.WorkerConcurrency != nil {
		n := *req.WorkerConcurrency
		if n < 1 || n > 8 {
			jsonError(w, "worker_concurrency must be between 1 and 8", http.StatusBadRequest)
			return
		}
		s.cfg.Scanner.WorkerConcurrency = n
		s.worker.SetConcurrency(n)
		fileUpdates["worker_concurrency"] = strconv.Itoa(n)
	}
	if req.ScanIntervalHours != nil {
		hrs := *req.ScanIntervalHours
		if hrs < 1 {
			jsonError(w, "scan_interval_hours must be at least 1", http.StatusBadRequest)
			return
		}
		s.cfg.Scanner.IntervalHours = hrs
		if s.sched != nil {
			s.sched.SetInterval(hrs)
		}
		fileUpdates["interval_hours"] = strconv.Itoa(hrs)
	}
	if req.ProcessedDirName != nil {
		s.cfg.Safety.ProcessedDirName = *req.ProcessedDirName
		fileUpdates["processed_dir_name"] = `"` + *req.ProcessedDirName + `"`
	}
	if req.OriginalsRetentionDays != nil {
		days := *req.OriginalsRetentionDays
		if days < 1 {
			jsonError(w, "originals_retention_days must be at least 1", http.StatusBadRequest)
			return
		}
		s.cfg.Safety.OriginalsRetentionDays = days
		fileUpdates["originals_retention_days"] = strconv.Itoa(days)
	}
	if req.FailThreshold != nil {
		n := *req.FailThreshold
		if n < 1 {
			jsonError(w, "fail_threshold must be at least 1", http.StatusBadRequest)
			return
		}
		s.cfg.Safety.FailThreshold = n
		fileUpdates["fail_threshold"] = strconv.Itoa(n)
	}
	if req.SystemFailThreshold != nil {
		n := *req.SystemFailThreshold
		if n < 1 {
			jsonError(w, "system_fail_threshold must be at least 1", http.StatusBadRequest)
			return
		}
		s.cfg.Safety.SystemFailThreshold = n
		fileUpdates["system_fail_threshold"] = strconv.Itoa(n)
	}
	if req.DeleteConfirmSingle != nil {
		s.cfg.Safety.DeleteConfirmSingle = *req.DeleteConfirmSingle
		fileUpdates["delete_confirm_single"] = strconv.FormatBool(*req.DeleteConfirmSingle)
	}
	if req.PlexEnabled != nil {
		s.cfg.Plex.Enabled = *req.PlexEnabled
		fileUpdates["enabled"] = strconv.FormatBool(*req.PlexEnabled)
	}
	if req.PlexBaseURL != nil {
		s.cfg.Plex.BaseURL = *req.PlexBaseURL
		fileUpdates["base_url"] = `"` + *req.PlexBaseURL + `"`
	}
	if req.PlexToken != nil && *req.PlexToken != "" {
		s.cfg.Plex.Token = *req.PlexToken
		fileUpdates["token"] = `"` + *req.PlexToken + `"`
	}
	if req.Encoder != nil {
		chosen := *req.Encoder
		// Validate against available encoders.
		var match *transcoder.Encoder
		for _, enc := range s.availableEncoders {
			if string(enc.Type) == chosen {
				match = enc
				break
			}
		}
		if match == nil {
			jsonError(w, "encoder not available: "+chosen, http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.encoder = match
		s.mu.Unlock()
		s.transcoder.SetEncoder(match)
		s.cfg.Transcoder.Encoder = chosen
		fileUpdates["encoder"] = `"` + chosen + `"`
		s.log.Info("encoder changed", "encoder", match.DisplayName)
	}

	if s.cfgPath != "" && len(fileUpdates) > 0 {
		if err := config.UpdateFile(s.cfgPath, fileUpdates); err != nil {
			s.log.Warn("could not write config file", "error", err)
			// Non-fatal: in-memory update already applied.
		}
	}

	s.handleGetConfig(w, r)
}

// GET /originals — list active (non-deleted) original records
func (s *Server) handleListOriginals(w http.ResponseWriter, r *http.Request) {
	records, err := s.db.ActiveOriginals()
	if err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	type row struct {
		ID           int64     `json:"id"`
		JobID        int64     `json:"job_id"`
		OriginalPath string    `json:"original_path"`
		HeldPath     string    `json:"held_path"`
		OutputPath   string    `json:"output_path"`
		OriginalSize int64     `json:"original_size"`
		OutputSize   int64     `json:"output_size"`
		ExpiresAt    time.Time `json:"expires_at"`
		CreatedAt    time.Time `json:"created_at"`
		DaysRemaining int      `json:"days_remaining"`
	}
	out := make([]row, 0, len(records))
	for _, rec := range records {
		days := int(time.Until(rec.ExpiresAt).Hours() / 24)
		if days < 0 {
			days = 0
		}
		out = append(out, row{
			ID:            rec.ID,
			JobID:         rec.JobID,
			OriginalPath:  rec.OriginalPath,
			HeldPath:      rec.HeldPath,
			OutputPath:    rec.OutputPath,
			OriginalSize:  rec.OriginalSize,
			OutputSize:    rec.OutputSize,
			ExpiresAt:     rec.ExpiresAt,
			CreatedAt:     rec.CreatedAt,
			DaysRemaining: days,
		})
	}
	jsonOK(w, out)
}

// DELETE /originals/{id} — delete the held original (accept the transcoded version)
func (s *Server) handleDeleteOriginal(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	rec, err := s.db.GetOriginal(id)
	if err != nil || rec == nil {
		jsonError(w, "original record not found", http.StatusNotFound)
		return
	}
	if rerr := os.Remove(rec.HeldPath); rerr != nil && !os.IsNotExist(rerr) {
		jsonError(w, "failed to delete original file", http.StatusInternalServerError)
		return
	}
	if err := s.db.MarkOriginalDeleted(id); err != nil {
		jsonError(w, "database error", http.StatusInternalServerError)
		return
	}
	s.db.UpdateJobStatus(rec.JobID, db.JobDone, "")
	w.WriteHeader(http.StatusNoContent)
}

// POST /originals/{id}/restore — move original back, remove transcoded version,
// mark job as restored. Optionally exclude the job (body: {"exclude": true}).
func (s *Server) handleRestoreOriginal(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	rec, err := s.db.GetOriginal(id)
	if err != nil || rec == nil {
		jsonError(w, "original record not found", http.StatusNotFound)
		return
	}

	var body struct {
		Exclude bool `json:"exclude"`
	}
	json.NewDecoder(r.Body).Decode(&body) // ignore decode error (body optional)

	// Move original back to its original location.
	if err := os.MkdirAll(filepath.Dir(rec.OriginalPath), 0o755); err != nil {
		jsonError(w, "failed to prepare restore path", http.StatusInternalServerError)
		return
	}
	if err := os.Rename(rec.HeldPath, rec.OriginalPath); err != nil {
		jsonError(w, "failed to restore original file", http.StatusInternalServerError)
		return
	}

	// Remove the transcoded output.
	if rerr := os.Remove(rec.OutputPath); rerr != nil && !os.IsNotExist(rerr) {
		s.log.Warn("restore: could not remove transcoded file",
			"path", rec.OutputPath, "error", rerr)
		// Non-fatal — original is restored, user can delete manually.
	}

	s.db.MarkOriginalDeleted(id)

	if body.Exclude {
		s.db.ExcludeJob(rec.JobID, "restored by user and excluded from future scans")
	} else {
		s.db.UpdateJobStatus(rec.JobID, db.JobRestored, "original restored by user")
	}

	w.WriteHeader(http.StatusNoContent)
}

// videoExtensions is the set of file extensions treated as video files.
var videoExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".m4v": true,
	".mov": true, ".ts": true, ".m2ts": true, ".wmv": true,
}

// fsFileEntry is returned by GET /fs for each video file.
type fsFileEntry struct {
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Modified  time.Time `json:"modified"`
	Codec     string    `json:"codec,omitempty"`
	Bitrate   int64     `json:"bitrate,omitempty"`
	Duration  float64   `json:"duration,omitempty"`
	JobStatus string    `json:"job_status,omitempty"`
	BytesSaved int64   `json:"bytes_saved,omitempty"`
}

// GET /fs?path=<dir>&files=1&unrestricted=1 — list subdirectories (and optionally video files with metadata)
// unrestricted=1 bypasses the root-dir ceiling; used only for the root-directory picker in Settings.
func (s *Server) handleBrowseFS(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	showFiles := r.URL.Query().Get("files") == "1"
	unrestricted := r.URL.Query().Get("unrestricted") == "1"

	// Root dirs from config define the navigation ceiling (unless unrestricted).
	roots := s.cfg.Scanner.RootDirs

	// Default starting path: first root dir, or / if none configured (or unrestricted).
	if reqPath == "" {
		if !unrestricted && len(roots) > 0 {
			reqPath = roots[0]
		} else {
			reqPath = "/"
		}
	}

	abs, err := filepath.Abs(reqPath)
	if err != nil {
		jsonError(w, "invalid path", http.StatusBadRequest)
		return
	}

	// If configured roots exist and the requested path is outside all of them,
	// redirect to the first root instead of exposing arbitrary filesystem paths.
	// Skip this check when unrestricted (root-directory picker in Settings).
	if !unrestricted && len(roots) > 0 && !pathUnderAnyRoot(abs, roots) {
		abs = roots[0]
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		jsonError(w, "cannot read directory", http.StatusBadRequest)
		return
	}

	dirs := []string{}
	var filePaths []string
	var fileEntries []fsFileEntry

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(abs, e.Name()))
		} else if showFiles && videoExtensions[strings.ToLower(filepath.Ext(e.Name()))] {
			fullPath := filepath.Join(abs, e.Name())
			info, err := e.Info()
			if err != nil {
				continue
			}
			fileEntries = append(fileEntries, fsFileEntry{
				Path:     fullPath,
				Name:     e.Name(),
				Size:     info.Size(),
				Modified: info.ModTime(),
			})
			filePaths = append(filePaths, fullPath)
		}
	}

	// Enrich with job metadata in a single query.
	if len(filePaths) > 0 {
		jobMeta, _ := s.db.GetJobMetaByPaths(filePaths)
		if jobMeta != nil {
			for i := range fileEntries {
				if m, ok := jobMeta[fileEntries[i].Path]; ok {
					fileEntries[i].Codec      = m.Codec
					fileEntries[i].Bitrate    = m.Bitrate
					fileEntries[i].Duration   = m.Duration
					fileEntries[i].JobStatus  = m.Status
					fileEntries[i].BytesSaved = m.BytesSaved
				}
			}
		}
	}

	if fileEntries == nil {
		fileEntries = []fsFileEntry{}
	}

	// Compute parent, clamped so it never goes above a configured root (unless unrestricted).
	parent := filepath.Dir(abs)
	if parent == abs {
		parent = "" // filesystem root — can't go higher
	} else if !unrestricted && len(roots) > 0 {
		// Suppress parent if abs is already at a configured root boundary.
		for _, root := range roots {
			if abs == root {
				parent = ""
				break
			}
		}
	}

	jsonOK(w, map[string]any{
		"current": abs,
		"parent":  parent,
		"dirs":    dirs,
		"files":   fileEntries,
	})
}

// pathUnderAnyRoot returns true if path equals or is inside any of the given roots.
func pathUnderAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

// diskFreeGB returns free GB and the path being measured.
// Uses the first enabled media directory; falls back to data dir.
func (s *Server) diskFreeGB() (float64, string) {
	if runtime.GOOS == "windows" {
		return -1, ""
	}
	checkPath := s.cfg.Server.DataDir
	dirs, _ := s.db.ListDirectories()
	for _, d := range dirs {
		if d.Enabled {
			checkPath = d.Path
			break
		}
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(checkPath, &stat); err != nil {
		return -1, ""
	}
	return float64(stat.Bavail) * float64(stat.Bsize) / (1024 * 1024 * 1024), checkPath
}

