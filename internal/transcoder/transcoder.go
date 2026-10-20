package transcoder

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProgressFunc is called periodically with progress [0.0,1.0], encode speed
// relative to realtime (e.g. 3.66 = 3.66× faster), and frames per second.
// Speed is 0 when ffmpeg has not yet reported it (start of encode).
type ProgressFunc func(progress, speed, fps float64)

// Transcoder runs ffmpeg jobs using a detected encoder.
type Transcoder struct {
	mu                sync.RWMutex
	encoder           *Encoder
	log               *slog.Logger
	tempDir           string
	hardwareDecode    string
	lowPower          string
	probes            map[string]bool
	qualificationPath string
}

// New creates a Transcoder. If tempDir is empty, output files are written
// adjacent to the source file with a .sqzarr-tmp suffix.
func New(enc *Encoder, tempDir string, log *slog.Logger) *Transcoder {
	copy := *enc
	return &Transcoder{encoder: &copy, tempDir: tempDir, log: log, hardwareDecode: "auto", lowPower: "auto", probes: make(map[string]bool), qualificationPath: LowPowerQualificationPath}
}

// Encoder returns the active encoder.
func (t *Transcoder) Encoder() *Encoder {
	t.mu.RLock()
	defer t.mu.RUnlock()
	copy := *t.encoder
	return &copy
}

// SetEncoder swaps the active encoder. The change takes effect on the next
// transcode job — any currently running job continues with the old encoder.
func (t *Transcoder) SetEncoder(enc *Encoder) {
	t.mu.Lock()
	defer t.mu.Unlock()
	copy := *enc
	t.encoder = &copy
}

// Run transcodes inputPath to a temp file, returning the temp output path.
// The caller is responsible for verifying and renaming the output.
// onLog, if non-nil, is called with each non-metric stderr line (startup info, warnings, errors).
// If the current encoder fails with a device-related error, automatically retries with software encoding.
func (t *Transcoder) Run(ctx context.Context, inputPath string, duration float64, onProgress ProgressFunc, onLog func(string)) (outputPath string, err error) {
	t.mu.RLock()
	enc := *t.encoder
	decodePolicy, lowPower := t.hardwareDecode, t.lowPower
	t.mu.RUnlock()
	emit := func(message string) {
		if onLog != nil {
			onLog(message)
		}
		if t.log != nil {
			t.log.Info(message)
		}
	}
	attempts := []*Encoder{}
	if enc.Type == EncoderVAAPI {
		enc.DisplayName = "Software decode -> standard VAAPI HEVC VBR (/dev/dri/renderD128)"
		qualified := lowPower == "auto" && lowPowerQualified(t.qualificationPath)
		if lowPower == "auto" {
			status := "unqualified"
			if qualified {
				status = "qualified"
			}
			emit("Low-power VBR qualification: " + status)
		}
		if decodePolicy != "off" {
			v, inspectErr := inspectVideo(ctx, inputPath)
			if inspectErr == nil && v.eligible() && t.probeDecode(ctx, inputPath, v) {
				if qualified && t.probeChain(ctx, inputPath, v, true) {
					attempts = append(attempts, &Encoder{Type: EncoderVAAPI, DisplayName: "VAAPI decode -> qualified low-power HEVC VBR (/dev/dri/renderD128)", BuildArgs: lowPowerArgs})
				} else if lowPower == "auto" {
					emit("Low-power VBR unavailable or unqualified; standard hardware VBR selected")
				}
				attempts = append(attempts, &Encoder{Type: EncoderVAAPI, DisplayName: "VAAPI decode -> standard HEVC VBR (/dev/dri/renderD128)",
					BuildArgs: func(i, o string) []string { return vaapiArgs(i, o, true) }})
			} else {
				emit("Hardware decode optimization unavailable for this input/probe; software decode selected")
			}
		}
	}
	attempts = append(attempts, &enc)
	if enc.Type != EncoderSoftware {
		attempts = append(attempts, softwareEncoder())
	}
	for n, attempt := range attempts {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if n > 0 {
			emit("Hardware capability failed; retrying with " + attempt.DisplayName)
			if onProgress != nil {
				onProgress(0, 0, 0)
			}
		}
		emit("Encoding pipeline: " + attempt.DisplayName)
		outputPath, err = t.run(ctx, attempt, inputPath, duration, onProgress, onLog)
		if err == nil {
			return outputPath, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !isDeviceError(err) {
			return "", err
		}
	}
	return "", err
}

// run performs the actual ffmpeg transcoding. It's separated to allow retries with fallback.
func (t *Transcoder) run(ctx context.Context, encoder *Encoder, inputPath string, duration float64, onProgress ProgressFunc, onLog func(string)) (outputPath string, err error) {
	outputPath = t.tempOutputPath(inputPath)

	// Ensure temp dir exists.
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	ext := filepath.Ext(outputPath)
	scratch, err := os.CreateTemp(filepath.Dir(outputPath), strings.TrimSuffix(filepath.Base(outputPath), ext)+"-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create scratch: %w", err)
	}
	outputPath = scratch.Name()
	// CreateTemp defaults to 0600. FFmpeg preserves an existing file's mode,
	// and this output becomes library media after verification/replacement.
	// Match the previous ordinary media-file mode so Plex can read it.
	if err = scratch.Chmod(0o644); err != nil {
		scratch.Close()
		os.Remove(outputPath)
		return "", fmt.Errorf("set scratch media permissions: %w", err)
	}
	if err = scratch.Close(); err != nil {
		os.Remove(outputPath)
		return "", err
	}
	ownedPath := outputPath
	defer func() {
		if err != nil {
			os.Remove(ownedPath)
		}
	}()
	args := rebuildWithProgress(encoder.BuildArgs(inputPath, outputPath))

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	// Set LIBVA_DRIVER_NAME for VAAPI.
	if encoder.Type == EncoderVAAPI {
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
		reader := bufio.NewReaderSize(stderr, 64*1024)
		var currentTime, currentSpeed, currentFPS float64
		for {
			fragment, readErr := reader.ReadSlice('\n')
			line := strings.TrimSpace(string(fragment))
			// ReadSlice continues draining oversized lines in bounded fragments.
			if isDiagLine(line) {
				stderrLines = append(stderrLines, line)
				if len(stderrLines) > 30 {
					stderrLines = stderrLines[1:]
				}
			}

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
			if readErr != nil && readErr != bufio.ErrBufferFull {
				break
			}
		}
		io.Copy(io.Discard, reader)
	}()

	<-stderrDone
	if err := cmd.Wait(); err != nil {
		os.Remove(outputPath)
		diag := ffmpegDiagnostic(stderrLines)
		if diag != "" {
			return "", fmt.Errorf("ffmpeg: %w\n%s", err, diag)
		}
		return "", fmt.Errorf("ffmpeg: %w", err)
	}

	info, statErr := os.Stat(outputPath)
	if statErr != nil || info.Size() == 0 {
		return "", fmt.Errorf("ffmpeg produced no usable output")
	}
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
	for _, p := range []string{"no space left", "permission denied", "invalid data found", "error opening input", "error opening output", "input/output error", "read-only file system", "no such file or directory", "corrupt", "error while decoding"} {
		if strings.Contains(errMsg, p) {
			return false
		}
	}
	devicePatterns := []string{
		"cannot load libcuda",
		"cannot load libnvidia",
		"device creation failed",
		"no device available",
		"hardware device setup failed",
		"device not found",
		"failed to initialise vaapi connection",
		"failed to initialize vaapi connection",
		"failed to create a vaapi",
		"failed to create decode configuration",
		"failed to create encode configuration",
		"no usable encoding entrypoint",
		"no support for codec",
		"failed setup for format vaapi",
		"hardware does not support",
		"failed to sync surface",
		"failed to synchronise surface",
		"failed to issue encode",
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
