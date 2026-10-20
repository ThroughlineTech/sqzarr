package queue

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/danrichardson/sqzarr/internal/config"
	"github.com/danrichardson/sqzarr/internal/db"
	"github.com/danrichardson/sqzarr/internal/rename"
	"github.com/danrichardson/sqzarr/internal/transcoder"
	"github.com/danrichardson/sqzarr/internal/verifier"
)

// EventType identifies the kind of worker event.
type EventType string

const (
	EventProgress EventType = "progress"
	EventDone     EventType = "done"
	EventFailed   EventType = "failed"
	EventPaused   EventType = "paused"
)

// Event is published to observers when job state changes.
type Event struct {
	Type     EventType
	JobID    int64
	Progress float64
	Speed    float64 // encode speed relative to realtime (e.g. 3.66 = 3.66×); 0 = unknown
	FPS      float64 // output frames per second; 0 = unknown
	Error    string
}

// Observer receives queue events.
type Observer func(Event)

// PlexNotifier is the interface for triggering Plex library rescans.
type PlexNotifier interface {
	NotifyFileReplaced(path string)
}

const maxJobLog = 100 // max stderr lines kept per active job

// Worker processes transcode jobs from the database queue.
type Worker struct {
	db         *db.DB
	cfg        *config.Config
	transcoder *transcoder.Transcoder
	plex       PlexNotifier
	log        *slog.Logger

	mu             sync.RWMutex
	observers      []Observer
	paused         bool
	activeJobs     atomic.Int32
	maxConcurrency atomic.Int32
	cancels        map[int64]context.CancelFunc // per-job cancel funcs

	logBufMu sync.Mutex
	logBuf   map[int64][]string // recent diagnostic lines per active job
}

// New creates a Worker.
func New(database *db.DB, cfg *config.Config, enc *transcoder.Transcoder, plex PlexNotifier, log *slog.Logger) *Worker {
	w := &Worker{
		db:         database,
		cfg:        cfg,
		transcoder: enc,
		plex:       plex,
		log:        log,
		cancels:    make(map[int64]context.CancelFunc),
		logBuf:     make(map[int64][]string),
	}
	concurrency := cfg.Scanner.WorkerConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	w.maxConcurrency.Store(int32(concurrency))
	return w
}

// SetPlexNotifier replaces the integration used by subsequent notifications.
func (w *Worker) SetPlexNotifier(notifier PlexNotifier) {
	w.mu.Lock()
	w.plex = notifier
	w.mu.Unlock()
}

// SetConcurrency updates the maximum number of concurrent transcode jobs.
func (w *Worker) SetConcurrency(n int) {
	if n < 1 {
		n = 1
	}
	if n > 8 {
		n = 8
	}
	w.maxConcurrency.Store(int32(n))
}

// Subscribe registers an observer for job events.
func (w *Worker) Subscribe(obs Observer) {
	w.mu.Lock()
	w.observers = append(w.observers, obs)
	w.mu.Unlock()
}

// IsPaused returns whether the worker is currently paused.
func (w *Worker) IsPaused() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.paused
}

// SetPaused pauses or resumes the worker.
func (w *Worker) SetPaused(paused bool) {
	w.mu.Lock()
	w.paused = paused
	w.mu.Unlock()
}

// appendLog adds a log line to the ring buffer for a job.
func (w *Worker) appendLog(jobID int64, line string) {
	w.logBufMu.Lock()
	defer w.logBufMu.Unlock()
	buf := append(w.logBuf[jobID], line)
	if len(buf) > maxJobLog {
		buf = buf[len(buf)-maxJobLog:]
	}
	w.logBuf[jobID] = buf
}

// RecentLog returns up to n recent diagnostic log lines for a job.
func (w *Worker) RecentLog(jobID int64, n int) []string {
	w.logBufMu.Lock()
	defer w.logBufMu.Unlock()
	buf := w.logBuf[jobID]
	if len(buf) == 0 {
		return nil
	}
	if n <= 0 || n > len(buf) {
		n = len(buf)
	}
	out := make([]string, n)
	copy(out, buf[len(buf)-n:])
	return out
}

// CancelJob cancels an actively running job by ID.
func (w *Worker) CancelJob(id int64) bool {
	w.mu.Lock()
	cancel, ok := w.cancels[id]
	w.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

// Run starts the worker loop. It blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	w.log.Info("worker started")
	for {
		select {
		case <-ctx.Done():
			w.log.Info("worker stopped")
			return
		case <-time.After(5 * time.Second):
			if err := w.tick(ctx); err != nil {
				w.log.Error("worker tick error", "error", err)
			}
		}
	}
}

func (w *Worker) tick(ctx context.Context) error {
	if w.IsPaused() {
		return nil
	}

	for {
		active := w.activeJobs.Load()
		max := w.maxConcurrency.Load()
		if active >= max {
			return nil
		}

		job, err := w.db.NextPendingJob()
		if err != nil {
			return fmt.Errorf("next job: %w", err)
		}
		if job == nil {
			return nil
		}

		// Per-job temp space check — skip rather than pause the queue.
		ok, err := hasTempSpace(job.SourcePath, w.cfg.Transcoder.TempDir)
		if err != nil || !ok {
			needed := "unknown"
			if fi, serr := os.Stat(job.SourcePath); serr == nil {
				needed = fmt.Sprintf("%.1f GB", float64(fi.Size())*1.2/(1024*1024*1024))
			}
			w.log.Warn("skipping job: insufficient temp space",
				"job_id", job.ID, "path", job.SourcePath, "need", needed)
			w.db.UpdateJobStatus(job.ID, db.JobSkipped, "insufficient temp space")
			continue
		}

		// Claim the job atomically before spawning.
		if err := w.db.UpdateJobStatus(job.ID, db.JobRunning, ""); err != nil {
			return fmt.Errorf("set running: %w", err)
		}
		jobCtx, jobCancel := context.WithCancel(ctx)
		w.mu.Lock()
		w.cancels[job.ID] = jobCancel
		w.mu.Unlock()

		w.activeJobs.Add(1)
		go func(j *db.Job, jctx context.Context, jcancel context.CancelFunc) {
			defer func() {
				jcancel()
				w.mu.Lock()
				delete(w.cancels, j.ID)
				w.mu.Unlock()
				w.activeJobs.Add(-1)
				// Retain log buffer briefly so the UI can do a final read.
				jID := j.ID
				time.AfterFunc(30*time.Second, func() {
					w.logBufMu.Lock()
					delete(w.logBuf, jID)
					w.logBufMu.Unlock()
				})
			}()
			w.processJob(jctx, j)
		}(job, jobCtx, jobCancel)
	}
}

// processJob runs a job that has already been claimed (status = running).
func (w *Worker) processJob(ctx context.Context, job *db.Job) {
	w.log.Info("starting job", "job_id", job.ID, "source", job.SourcePath)

	onProgress := func(p, speed, fps float64) {
		w.db.UpdateJobProgress(job.ID, p)
		w.emit(Event{Type: EventProgress, JobID: job.ID, Progress: p, Speed: speed, FPS: fps})
	}
	onLog := func(line string) { w.appendLog(job.ID, line) }

	outputPath, err := w.transcoder.Run(ctx, job.SourcePath, job.SourceDuration, onProgress, onLog)
	if err != nil {
		if ctx.Err() != nil {
			w.log.Info("job cancelled", "job_id", job.ID)
			w.db.UpdateJobStatus(job.ID, db.JobCancelled, "cancelled by user")
			w.emit(Event{Type: EventFailed, JobID: job.ID, Error: "cancelled"})
		} else {
			w.log.Error("transcode failed", "job_id", job.ID, "error", err)
			w.handleFailure(job, err.Error())
		}
		return
	}

	// Verify output before touching the original.
	result, err := verifier.Verify(job.SourcePath, outputPath, 1.0)
	if err != nil {
		os.Remove(outputPath)
		w.log.Error("verify error", "job_id", job.ID, "error", err)
		w.handleFailure(job, err.Error())
		return
	}
	if !result.OK {
		os.Remove(outputPath)
		w.log.Error("verify failed", "job_id", job.ID, "reason", result.Reason)
		if result.Uncompressible {
			// File won't benefit from re-encoding — exclude permanently.
			w.db.ExcludeJob(job.ID, "uncompressible: "+result.Reason)
			// Durably record as excluded so it survives history clears.
			if fi, serr := os.Stat(job.SourcePath); serr == nil {
				w.db.UpsertProcessedFile(job.SourcePath, "excluded", "uncompressible: "+result.Reason, fi.Size(), fi.ModTime())
			}
			w.emit(Event{Type: EventFailed, JobID: job.ID, Error: result.Reason})
			return
		}
		w.handleFailure(job, result.Reason)
		return
	}

	// Cancellation may arrive while output verification is running. Never
	// stage a replacement for a job the user has already cancelled.
	if ctx.Err() != nil {
		os.Remove(outputPath)
		w.db.UpdateJobStatus(job.ID, db.JobCancelled, "cancelled by user")
		w.emit(Event{Type: EventFailed, JobID: job.ID, Error: "cancelled"})
		return
	}

	// Compute the new output filename with codec token replacement.
	newName := rename.OutputName(filepath.Base(job.SourcePath))
	finalOutputPath := rename.OutputPath(
		filepath.Dir(job.SourcePath), newName, rename.FileExists)

	// Rename the temp output to the final codec-aware name.
	if err := os.Rename(outputPath, finalOutputPath); err != nil {
		os.Remove(outputPath)
		w.log.Error("rename output failed", "job_id", job.ID, "error", err)
		w.handleFailure(job, "rename output: "+err.Error())
		return
	}

	// Move original to processed dir (mirrored path structure).
	heldPath, err := w.moveToProcessed(job)
	if err != nil {
		// Rollback: remove the transcoded file we already placed.
		os.Remove(finalOutputPath)
		w.log.Error("move to processed failed", "job_id", job.ID, "error", err)
		w.handleFailure(job, "move to processed: "+err.Error())
		return
	}

	// Record the staged state.
	bytesSaved := result.InputSize - result.OutputSize
	encoderName := string(w.transcoder.Encoder().Type)

	retentionDays := w.cfg.Safety.OriginalsRetentionDays
	if retentionDays < 1 {
		retentionDays = 10
	}
	expiresAt := time.Now().Add(time.Duration(retentionDays) * 24 * time.Hour)

	if _, err := w.db.InsertOriginal(&db.OriginalRecord{
		JobID:        job.ID,
		OriginalPath: job.SourcePath,
		HeldPath:     heldPath,
		OutputPath:   finalOutputPath,
		OriginalSize: result.InputSize,
		OutputSize:   result.OutputSize,
		ExpiresAt:    expiresAt,
	}); err != nil {
		w.log.Error("insert original record failed", "job_id", job.ID, "error", err)
		// Non-fatal: the files are in the right place; just mark staged.
	}

	if err := w.db.StageJob(job.ID, finalOutputPath, result.OutputSize, encoderName, bytesSaved); err != nil {
		w.log.Error("stage job in db", "job_id", job.ID, "error", err)
	}

	// Durably record the file as processed so it survives history clears.
	if fi, serr := os.Stat(finalOutputPath); serr == nil {
		if uerr := w.db.UpsertProcessedFile(job.SourcePath, "done", "", fi.Size(), fi.ModTime()); uerr != nil {
			w.log.Error("upsert processed file", "job_id", job.ID, "error", uerr)
		}
	}

	w.log.Info("job staged",
		"job_id", job.ID,
		"output", finalOutputPath,
		"held", heldPath,
		"bytes_saved", bytesSaved,
	)
	w.emit(Event{Type: EventDone, JobID: job.ID, Progress: 1.0})

	// Notify Plex of the new file — non-fatal.
	w.mu.RLock()
	notifier := w.plex
	w.mu.RUnlock()
	if notifier != nil {
		go notifier.NotifyFileReplaced(finalOutputPath)
	}
}

// isIOError returns true if the error message indicates an I/O or permission
// problem that needs human intervention rather than a transcode logic failure.
func isIOError(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	patterns := []string{
		"permission denied",
		"access is denied",
		"read-only file system",
		"no space left on device",
		"disk full",
		"input/output error",
		"i/o error",
		"operation not permitted",
		"is a directory",
		"too many open files",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// handleFailure increments the fail counter for a job and either excludes it
// (if the per-file threshold is reached) or marks it failed/error. I/O errors
// (permission denied, disk full, etc.) become "error" status which needs human
// attention; non-I/O failures become "excluded" after hitting the threshold.
func (w *Worker) handleFailure(job *db.Job, reason string) {
	failCount, err := w.db.IncrementFailCount(job.ID)
	if err != nil {
		w.log.Error("increment fail count", "job_id", job.ID, "error", err)
		failCount = job.FailCount + 1
	}

	threshold := w.cfg.Safety.FailThreshold
	if threshold < 1 {
		threshold = 1
	}

	if failCount >= threshold {
		if isIOError(reason) {
			// I/O errors need human intervention — mark as error, not excluded.
			w.log.Warn("marking job as error (I/O failure)",
				"job_id", job.ID, "fail_count", failCount, "reason", reason)
			w.db.UpdateJobStatus(job.ID, db.JobError, reason)
			if fi, serr := os.Stat(job.SourcePath); serr == nil {
				w.db.UpsertProcessedFile(job.SourcePath, "error", reason, fi.Size(), fi.ModTime())
			}
		} else {
			w.log.Warn("excluding job after repeated failures",
				"job_id", job.ID, "fail_count", failCount, "reason", reason)
			w.db.ExcludeJob(job.ID, reason)
			// Durably record as excluded so it survives history clears.
			if fi, serr := os.Stat(job.SourcePath); serr == nil {
				w.db.UpsertProcessedFile(job.SourcePath, "excluded", reason, fi.Size(), fi.ModTime())
			}
		}
	} else {
		w.db.UpdateJobStatus(job.ID, db.JobFailed, reason)
	}

	w.emit(Event{Type: EventFailed, JobID: job.ID, Error: reason})

	// Check system-level consecutive failure threshold.
	sysThreshold := w.cfg.Safety.SystemFailThreshold
	if sysThreshold < 1 {
		sysThreshold = 5
	}
	consecFails, err := w.db.ConsecutiveFailCount()
	if err != nil {
		return
	}
	if consecFails >= sysThreshold {
		w.log.Warn("auto-pausing: too many consecutive failures",
			"consecutive_fails", consecFails, "threshold", sysThreshold)
		w.SetPaused(true)
		w.emit(Event{Type: EventPaused})
	}
}

// moveToProcessed moves the source file to the processed directory, mirroring
// the path structure relative to the closest configured root directory.
// Returns the new held path.
func (w *Worker) moveToProcessed(job *db.Job) (string, error) {
	rootDir := w.findRootDir(job.SourcePath)
	processedDir := w.cfg.ProcessedDirFor(rootDir)

	rel, err := filepath.Rel(rootDir, job.SourcePath)
	if err != nil || strings.HasPrefix(rel, "..") {
		// Fallback: use basename only.
		rel = filepath.Base(job.SourcePath)
	}
	heldPath := filepath.Join(processedDir, rel)

	if err := os.MkdirAll(filepath.Dir(heldPath), 0o755); err != nil {
		return "", fmt.Errorf("create processed dir: %w", err)
	}
	if err := os.Rename(job.SourcePath, heldPath); err != nil {
		return "", fmt.Errorf("move original: %w", err)
	}
	return heldPath, nil
}

// findRootDir returns the longest configured directory path that is a prefix
// of sourcePath. Falls back to the source file's parent directory.
func (w *Worker) findRootDir(sourcePath string) string {
	dirs, _ := w.db.ListDirectories()
	best := ""
	for _, d := range dirs {
		prefix := d.Path
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		if strings.HasPrefix(sourcePath, prefix) && len(d.Path) > len(best) {
			best = d.Path
		}
	}
	if best == "" {
		return filepath.Dir(sourcePath)
	}
	return best
}

// hasTempSpace checks whether the partition holding the temp output file has
// at least 120% of the source file size available.
func hasTempSpace(sourcePath, tempDir string) (bool, error) {
	if runtime.GOOS != "linux" {
		return true, nil
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		return false, err
	}
	needed := uint64(float64(info.Size()) * 1.2)

	checkDir := tempDir
	if checkDir == "" {
		checkDir = filepath.Dir(sourcePath)
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(checkDir, &stat); err != nil {
		return true, nil // Can't check — don't block.
	}
	free := stat.Bavail * uint64(stat.Bsize)
	return free >= needed, nil
}

func (w *Worker) emit(e Event) {
	w.mu.RLock()
	obs := make([]Observer, len(w.observers))
	copy(obs, w.observers)
	w.mu.RUnlock()

	for _, o := range obs {
		o(e)
	}
}
