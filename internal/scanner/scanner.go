package scanner

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/danrichardson/sqzarr/internal/db"
)

// VideoExtensions is the set of file extensions treated as video files.
var VideoExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".mov": true,
	".m4v": true, ".ts": true, ".m2ts": true, ".wmv": true,
}

// scratchVideoName reserves the legacy output suffix and the numeric suffix
// added by os.CreateTemp for per-attempt outputs. Never scan an in-flight encode.
var scratchVideoName = regexp.MustCompile(`(?i)\.sqzarr-tmp(?:-[0-9]+)?\.[^.]+$`)

// IsScratchPath identifies outputs reserved for an unfinished transcode attempt.
func IsScratchPath(path string) bool {
	return scratchVideoName.MatchString(filepath.Base(path))
}

// Result summarises a completed scan.
type Result struct {
	FilesScanned int
	FilesQueued  int
	FilesSkipped int
}

// Scanner walks directories and enqueues qualifying files.
type Scanner struct {
	db               *db.DB
	log              *slog.Logger
	processedDirName string // directory name to skip during walks (default: ".processed")
}

// New creates a Scanner. processedDirName is the name of the subdirectory
// (e.g. ".processed") that holds held originals and should be skipped.
func New(database *db.DB, processedDirName string, log *slog.Logger) *Scanner {
	if processedDirName == "" {
		processedDirName = ".processed"
	}
	return &Scanner{db: database, processedDirName: processedDirName, log: log}
}

// ScanDirectory walks dir and enqueues qualifying files according to the
// directory's configured rules.
func (s *Scanner) ScanDirectory(ctx context.Context, dir *db.Directory) (*Result, error) {
	if !dir.Enabled {
		return &Result{}, nil
	}

	runID, err := s.db.InsertScanRun(sql.NullInt64{Int64: dir.ID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("start scan run: %w", err)
	}

	start := time.Now()
	res := &Result{}

	walkErr := filepath.WalkDir(dir.Path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			s.log.Warn("walk error", "path", path, "error", err)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			// Skip the processed directory to avoid walking held originals.
			if d.Name() == s.processedDirName {
				return filepath.SkipDir
			}
			return nil
		}
		if !VideoExtensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		if IsScratchPath(path) {
			return nil
		}

		res.FilesScanned++

		queued, skip, err := s.maybeEnqueue(ctx, path, dir)
		if err != nil {
			s.log.Warn("enqueue error", "path", path, "error", err)
			return nil
		}
		if queued {
			res.FilesQueued++
		}
		if skip != "" {
			res.FilesSkipped++
			s.log.Debug("skip", "path", path, "reason", skip)
		}
		return nil
	})

	durationMS := time.Since(start).Milliseconds()
	errMsg := ""
	if walkErr != nil {
		errMsg = walkErr.Error()
	}
	s.db.FinishScanRun(runID, res.FilesScanned, res.FilesQueued, res.FilesSkipped, durationMS, errMsg)

	if walkErr != nil {
		return res, fmt.Errorf("walk: %w", walkErr)
	}

	s.log.Info("scan complete",
		"directory", dir.Path,
		"scanned", res.FilesScanned,
		"queued", res.FilesQueued,
		"skipped", res.FilesSkipped,
		"duration_ms", durationMS,
	)
	return res, nil
}

// maybeEnqueue evaluates a file against directory rules and inserts a pending
// job if it qualifies. Returns (queued, skipReason, error).
func (s *Scanner) maybeEnqueue(ctx context.Context, path string, dir *db.Directory) (bool, string, error) {
	// Stat the file early — we need size/mtime for the processed-files check.
	info, err := os.Stat(path)
	if err != nil {
		return false, "", fmt.Errorf("stat: %w", err)
	}

	// Check the durable processed_files table first. If the file was already
	// handled and hasn't changed on disk, skip it regardless of job history.
	processed, err := s.db.IsFileProcessed(path, info.Size(), info.ModTime())
	if err != nil {
		return false, "", err
	}
	if processed {
		return false, "already processed", nil
	}

	// Skip if a non-retriable job already exists for this path.
	status, err := s.db.SourcePathStatus(path)
	if err != nil {
		return false, "", err
	}
	switch status {
	case "": // no job yet — proceed
	case db.JobFailed, db.JobRestored, db.JobError:
		// Failed, restored, or errored files can be re-queued on the next scan.
	default:
		return false, "status:" + string(status), nil
	}

	// Skip if this path is the output of a previous transcode job (renamed file).
	isOutput, err := s.db.OutputPathExists(path)
	if err != nil {
		return false, "", err
	}
	if isOutput {
		return false, "already a transcode output", nil
	}

	// Age check.
	age := time.Since(info.ModTime())
	minAge := time.Duration(dir.MinAgeDays) * 24 * time.Hour
	if age < minAge {
		return false, fmt.Sprintf("too new (%.0f days)", age.Hours()/24), nil
	}

	// Size check.
	sizeMB := info.Size() / (1024 * 1024)
	if dir.MinSizeMB > 0 && sizeMB < dir.MinSizeMB {
		return false, fmt.Sprintf("too small (%d MB)", sizeMB), nil
	}

	// ffprobe for codec and bitrate.
	probe, err := probeFile(path)
	if err != nil {
		return false, "", fmt.Errorf("probe: %w", err)
	}

	// Bitrate check — apply skip margin so files near the target are not
	// sent through a full transcode only to be marked uncompressible.
	if dir.MaxBitrate > 0 {
		threshold := int64(float64(dir.MaxBitrate) * (1.0 + dir.BitrateSkipMargin))
		if probe.bitrate <= threshold {
			return false, fmt.Sprintf("bitrate %d within margin of limit %d (threshold %d)", probe.bitrate, dir.MaxBitrate, threshold), nil
		}
	}

	job := &db.Job{
		DirectoryID:    sql.NullInt64{Int64: dir.ID, Valid: true},
		SourcePath:     path,
		SourceSize:     info.Size(),
		SourceCodec:    probe.codec,
		SourceDuration: probe.duration,
		SourceBitrate:  probe.bitrate,
		Status:         db.JobPending,
	}
	_, err = s.db.InsertJob(job)
	if err != nil {
		return false, "", fmt.Errorf("insert job: %w", err)
	}

	s.log.Info("queued",
		"path", path,
		"codec", probe.codec,
		"bitrate", probe.bitrate,
		"size_mb", sizeMB,
	)
	return true, "", nil
}

type probeResult struct {
	codec    string
	bitrate  int64
	duration float64
}

type ffprobeOutput struct {
	Streams []struct {
		CodecName string `json:"codec_name"`
		CodecType string `json:"codec_type"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
		BitRate  string `json:"bit_rate"`
	} `json:"format"`
}

func probeFile(path string) (*probeResult, error) {
	out, err := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
		path,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}

	var p ffprobeOutput
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	r := &probeResult{}
	for _, s := range p.Streams {
		if s.CodecType == "video" {
			r.codec = s.CodecName
			break
		}
	}

	fmt.Sscanf(p.Format.Duration, "%f", &r.duration)
	fmt.Sscanf(p.Format.BitRate, "%d", &r.bitrate)
	return r, nil
}
