package queue

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/danrichardson/sqzarr/internal/db"
)

// OriginalsGC runs as a background goroutine, deleting held original files
// whose retention period has expired (every hour).
type OriginalsGC struct {
	db  *db.DB
	log *slog.Logger
}

// NewOriginalsGC creates an OriginalsGC.
func NewOriginalsGC(database *db.DB, log *slog.Logger) *OriginalsGC {
	return &OriginalsGC{db: database, log: log}
}

// Run starts the GC loop. Blocks until ctx is cancelled.
func (gc *OriginalsGC) Run(ctx context.Context) {
	gc.log.Info("originals GC started")
	// Run immediately on startup, then every hour.
	gc.Sweep()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			gc.log.Info("originals GC stopped")
			return
		case <-ticker.C:
			gc.Sweep()
		}
	}
}

// Sweep deletes all expired original files immediately.
// Exported for use in tests.
func (gc *OriginalsGC) Sweep() {
	records, err := gc.db.ExpiredOriginals()
	if err != nil {
		gc.log.Error("originals GC: list expired", "error", err)
		return
	}
	for _, r := range records {
		if err := os.Remove(r.HeldPath); err != nil && !os.IsNotExist(err) {
			gc.log.Error("originals GC: remove file", "path", r.HeldPath, "error", err)
			continue
		}
		if err := gc.db.MarkOriginalDeleted(r.ID); err != nil {
			gc.log.Error("originals GC: mark deleted", "id", r.ID, "error", err)
			continue
		}
		if err := gc.db.UpdateJobStatus(r.JobID, db.JobDone, ""); err != nil {
			gc.log.Error("originals GC: update job status", "job_id", r.JobID, "error", err)
			continue
		}
		gc.log.Info("original expired and deleted",
			"held_path", r.HeldPath,
			"job_id", r.JobID,
		)
	}
}
