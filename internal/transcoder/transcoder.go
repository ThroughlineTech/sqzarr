package transcoder

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ProgressFunc is called periodically with progress [0.0,1.0], encode speed
// relative to realtime (e.g. 3.66 = 3.66× faster), and frames per second.
// Speed is 0 when ffmpeg has not yet reported it (start of encode).
type ProgressFunc func(progress, speed, fps float64)

// Transcoder runs ffmpeg jobs using a detected encoder.
type Transcoder struct {
	encoder *Encoder
	log     *slog.Logger
	tempDir string
}

// New creates a Transcoder. If tempDir is empty, output files are written
// adjacent to the source file with a .sqzarr-tmp suffix.
func New(enc *Encoder, tempDir string, log *slog.Logger) *Transcoder {
	return &Transcoder{encoder: enc, tempDir: tempDir, log: log}
}

// Encoder returns the active encoder.
func (t *Transcoder) Encoder() *Encoder {
	return t.encoder
}

// SetEncoder swaps the active encoder. The change takes effect on the next
// transcode job — any currently running job continues with the old encoder.
func (t *Transcoder) SetEncoder(enc *Encoder) {
	t.encoder = enc
}

// Run transcodes inputPath to a temp file, returning the temp output path.
// The caller is responsible for verifying and renaming the output.
// onLog, if non-nil, is called with each non-metric stderr line (startup info, warnings, errors).
// If the current encoder fails with a device-related error, automatically retries with software encoding.
func (t *Transcoder) Run(ctx context.Context, inputPath string, duration float64, onProgress ProgressFunc, onLog func(string)) (outputPath string, err error) {
	outputPath, err = t.run(ctx, inputPath, duration, onProgress, onLog)

	// Check if error is device-related and current encoder is not already software
	if err != nil && t.encoder.Type != EncoderSoftware && isDeviceError(err) {
		t.log.Warn("Hardware encoder failed with device error, retrying with software encoder",
			"original_error", err,
			"encoder", t.encoder.DisplayName)

		// Clean up the failed output file
		os.Remove(outputPath)

		// Switch to software encoder and retry
		originalEncoder := t.encoder
		t.encoder = softwareEncoder()
		defer func() { t.encoder = originalEncoder }() // Restore original after retry

		outputPath, err = t.run(ctx, inputPath, duration, onProgress, onLog)
		if err == nil {
			t.log.Info("Software encoder fallback succeeded")
		}
	}

	return outputPath, err
}

// run performs the actual ffmpeg transcoding. It's separated to allow retries with fallback.
func (t *Transcoder) run(ctx context.Context, inputPath string, duration float64, onProgress ProgressFunc, onLog func(string)) (outputPath string, err error) {
	outputPath = t.tempOutputPath(inputPath)

	// Ensure temp dir exists.
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	args := rebuildWithProgress(t.encoder.BuildArgs(inputPath, outputPath))

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	// Set LIBVA_DRIVER_NAME for VAAPI.
	if t.encoder.Type == EncoderVAAPI {
		cmd.Env = append(os.Environ(), "LIBVA_DRIVER_NAME=iHD")
	} else {
		cmd.Env = os.Environ()
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start ffmpeg: %w", err)
	}

	// Collect stderr while parsing progress. stderrLines is read after stderrDone is closed.
	var stderrLines []string
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 256*1024), 256*1024)
		var currentTime, currentSpeed, currentFPS float64
		for sc.Scan() {
			line := sc.Text()
			stderrLines = append(stderrLines, line)

			// Auto-detect duration from ffmpeg header if caller didn't supply it.
			if duration == 0 {
				if d := parseDurationLine(line); d > 0 {
					duration = d
				}
			}

			// Emit diagnostic lines (non-metric) to log consumer.
			if onLog != nil && isDiagLine(line) {
				onLog(line)
			}

			switch {
			case strings.HasPrefix(line, "out_time_ms="):
				ms, _ := strconv.ParseFloat(strings.TrimPrefix(line, "out_time_ms="), 64)
				currentTime = ms / 1_000_000
			case strings.HasPrefix(line, "speed="):
				s := strings.TrimPrefix(line, "speed=")
				s = strings.TrimSuffix(s, "x")
				if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
					currentSpeed = v
				}
			case strings.HasPrefix(line, "fps="):
				if v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(line, "fps=")), 64); err == nil {
					currentFPS = v
				}
			case line == "progress=continue" || line == "progress=end":
				if duration > 0 && onProgress != nil {
					pct := currentTime / duration
					if pct > 1 {
						pct = 1
					}
					onProgress(pct, currentSpeed, currentFPS)
				}
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		<-stderrDone
		os.Remove(outputPath)
		diag := ffmpegDiagnostic(stderrLines)
		if diag != "" {
			return "", fmt.Errorf("ffmpeg: %w\n%s", err, diag)
		}
		return "", fmt.Errorf("ffmpeg: %w", err)
	}
	<-stderrDone

	return outputPath, nil
}

// isDiagLine returns true for lines that contain useful diagnostic info
// (not ffmpeg's -progress pipe:2 key=value metrics).
func isDiagLine(line string) bool {
	if line == "" {
		return false
	}
	for _, pfx := range []string{
		"out_time_ms=", "out_time=", "out_time_us=",
		"frame=", "fps=", "stream_", "progress=",
		"speed=", "bitrate=", "total_size=",
		"dup_frames=", "drop_frames=",
	} {
		if strings.HasPrefix(line, pfx) {
			return false
		}
	}
	return true
}

// parseDurationLine extracts seconds from ffmpeg's "  Duration: HH:MM:SS.ms, ..." header line.
var durationLineRx = regexp.MustCompile(`Duration:\s+(\d+):(\d+):(\d+\.\d+)`)

func parseDurationLine(line string) float64 {
	m := durationLineRx.FindStringSubmatch(line)
	if m == nil {
		return 0
	}
	h, _ := strconv.ParseFloat(m[1], 64)
	mn, _ := strconv.ParseFloat(m[2], 64)
	s, _ := strconv.ParseFloat(m[3], 64)
	return h*3600 + mn*60 + s
}

// ffmpegDiagnostic filters progress-only lines and returns the last 30 diagnostic lines.
func ffmpegDiagnostic(lines []string) string {
	var diag []string
	for _, l := range lines {
		if l == "" ||
			strings.HasPrefix(l, "out_time_ms=") ||
			strings.HasPrefix(l, "out_time=") ||
			strings.HasPrefix(l, "frame=") ||
			strings.HasPrefix(l, "fps=") ||
			strings.HasPrefix(l, "stream_") ||
			strings.HasPrefix(l, "progress=") ||
			strings.HasPrefix(l, "speed=") ||
			strings.HasPrefix(l, "bitrate=") ||
			strings.HasPrefix(l, "total_size=") ||
			strings.HasPrefix(l, "out_time_us=") ||
			strings.HasPrefix(l, "dup_frames=") ||
			strings.HasPrefix(l, "drop_frames=") {
			continue
		}
		diag = append(diag, l)
	}
	if len(diag) > 30 {
		diag = diag[len(diag)-30:]
	}
	return strings.Join(diag, "\n")
}

// isDeviceError checks if an error message indicates a hardware device issue.
// These errors typically trigger a fallback to software encoding.
func isDeviceError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())
	devicePatterns := []string{
		"cannot load",
		"could not dynamically load",
		"device creation failed",
		"no device available",
		"hardware device setup failed",
		"permission denied",
		"/dev/dri",
		"cuda",
		"vaapi",
		"device not found",
	}

	for _, pattern := range devicePatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	return false
}

// rebuildWithProgress inserts -progress pipe:2 into the args before the final output path.
func rebuildWithProgress(args []string) []string {
	if len(args) == 0 {
		return args
	}
	output := args[len(args)-1]
	middle := args[:len(args)-1]
	result := make([]string, 0, len(middle)+3)
	result = append(result, middle...)
	result = append(result, "-progress", "pipe:2", output)
	return result
}

func (t *Transcoder) tempOutputPath(inputPath string) string {
	dir := t.tempDir
	if dir == "" {
		dir = filepath.Dir(inputPath)
	}
	base := filepath.Base(inputPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	return filepath.Join(dir, name+".sqzarr-tmp"+ext)
}

// progressRegex matches ffmpeg progress lines like "time=00:01:23.45".
var progressRegex = regexp.MustCompile(`time=(\d+):(\d+):(\d+\.\d+)`)

// ParseFFmpegTime parses an ffmpeg time string "HH:MM:SS.ms" to seconds.
func ParseFFmpegTime(s string) float64 {
	m := progressRegex.FindStringSubmatch(s)
	if len(m) < 4 {
		return 0
	}
	h, _ := strconv.ParseFloat(m[1], 64)
	min, _ := strconv.ParseFloat(m[2], 64)
	sec, _ := strconv.ParseFloat(m[3], 64)
	return h*3600 + min*60 + sec
}

// FormatDuration formats a duration for logging.
func FormatDuration(d time.Duration) string {
	return d.Round(time.Second).String()
}
