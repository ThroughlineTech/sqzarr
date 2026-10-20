package verifier

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	minSizeFloorBytes = 1024 * 1024 // 1 MB absolute minimum
	minSizeFloorRatio = 0.10        // output must be at least 10% of input
)

// Result holds the outcome of an output file verification.
type Result struct {
	InputSize    int64
	OutputSize   int64
	InputDur     float64
	OutputDur    float64
	DurationDiff float64
	OK           bool
	Reason       string
	// Uncompressible is true when the output is not smaller than the input,
	// indicating the file won't benefit from re-encoding. The job should be
	// permanently excluded rather than retried.
	Uncompressible bool
}

// Verify checks that outputPath is valid:
//   - smaller than inputPath (sets Uncompressible if not)
//   - at least 10% of inputPath size (catches corrupt/empty output)
//   - contains a decodable video stream
//   - duration matches within toleranceSecs
//   - spot-decodes at 10%, 50%, and 90% to catch corruption
func Verify(inputPath, outputPath string, toleranceSecs float64) (*Result, error) {
	inputInfo, err := os.Stat(inputPath)
	if err != nil {
		return nil, fmt.Errorf("stat input: %w", err)
	}
	outputInfo, err := os.Stat(outputPath)
	if err != nil {
		return nil, fmt.Errorf("stat output: %w", err)
	}

	r := &Result{
		InputSize:  inputInfo.Size(),
		OutputSize: outputInfo.Size(),
	}

	// Minimum size floor — catch empty or near-empty outputs.
	floor := int64(float64(r.InputSize) * minSizeFloorRatio)
	if floor < minSizeFloorBytes {
		floor = minSizeFloorBytes
	}
	if r.OutputSize < floor {
		r.Reason = fmt.Sprintf(
			"output is suspiciously small (%d bytes < %d byte floor)", r.OutputSize, floor)
		return r, nil
	}

	// Size comparison — if output is not smaller, mark uncompressible.
	if r.OutputSize >= r.InputSize {
		r.Uncompressible = true
		r.Reason = fmt.Sprintf(
			"output (%d bytes) is not smaller than input (%d bytes)", r.OutputSize, r.InputSize)
		return r, nil
	}

	// Check that the output contains at least one video stream.
	if err := checkVideoStream(outputPath); err != nil {
		r.Reason = fmt.Sprintf("no decodable video stream: %v", err)
		return r, nil
	}

	// Duration probe.
	outputDur, err := probeDuration(outputPath)
	if err != nil {
		return nil, fmt.Errorf("probe output duration: %w", err)
	}
	inputDur, err := probeDuration(inputPath)
	if err != nil {
		return nil, fmt.Errorf("probe input duration: %w", err)
	}

	r.InputDur = inputDur
	r.OutputDur = outputDur
	r.DurationDiff = math.Abs(outputDur - inputDur)

	if r.DurationDiff > toleranceSecs {
		r.Reason = fmt.Sprintf(
			"duration mismatch: input %.2fs, output %.2fs (diff %.2fs > tolerance %.2fs)",
			inputDur, outputDur, r.DurationDiff, toleranceSecs)
		return r, nil
	}

	// Spot decode at 10%, 50%, and 90% of the output duration.
	for _, frac := range []float64{0.10, 0.50, 0.90} {
		pos := outputDur * frac
		if err := spotDecode(outputPath, pos); err != nil {
			r.Reason = fmt.Sprintf("spot decode at %.0f%% (%.1fs) failed: %v",
				frac*100, pos, err)
			return r, nil
		}
	}

	r.OK = true
	return r, nil
}

// checkVideoStream uses ffprobe to verify a decodable video stream is present.
func checkVideoStream(path string) error {
	type stream struct {
		CodecType string `json:"codec_type"`
	}
	type probeOut struct {
		Streams []stream `json:"streams"`
	}

	out, err := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		path,
	).Output()
	if err != nil {
		return fmt.Errorf("ffprobe: %w", err)
	}

	var result probeOut
	if err := json.Unmarshal(out, &result); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, s := range result.Streams {
		if s.CodecType == "video" {
			return nil
		}
	}
	return fmt.Errorf("no video stream found")
}

// spotDecode asks ffmpeg to decode a single frame at posSecs.
func spotDecode(path string, posSecs float64) error {
	pos := strconv.FormatFloat(posSecs, 'f', 2, 64)
	cmd := exec.Command("ffmpeg",
		"-hide_banner",
		"-ss", pos,
		"-i", path,
		"-frames:v", "1",
		"-f", "null", "-",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Include the last line of output for context.
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		tail := ""
		if len(lines) > 0 {
			tail = lines[len(lines)-1]
		}
		return fmt.Errorf("ffmpeg decode: %w — %s", err, tail)
	}
	return nil
}

type ffprobeFormat struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func probeDuration(path string) (float64, error) {
	out, err := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		path,
	).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}

	var result ffprobeFormat
	if err := json.Unmarshal(out, &result); err != nil {
		return 0, fmt.Errorf("parse ffprobe output: %w", err)
	}

	return parseDuration(result.Format.Duration)
}

func parseDuration(value string) (float64, error) {
	dur, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(dur) || math.IsInf(dur, 0) || dur <= 0 {
		return 0, fmt.Errorf("invalid media duration %q", value)
	}
	return dur, nil
}
