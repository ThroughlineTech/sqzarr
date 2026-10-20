package transcoder

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const vaDevice = "/dev/dri/renderD128"
const LowPowerQualificationPath = "/etc/sqzarr/vaapi-low-power-qualified.json"
const qualificationSettings = "hevc-vaapi-vbr2300-max4000-buf4600-async2-scale1920-v1"

// LowPowerQualification is an operator attestation of saved-output quality
// validation, not merely an encoder probe. Software/device changes invalidate it.
type LowPowerQualification struct {
	Fingerprint string `json:"fingerprint"`
	InputClass  string `json:"input_class"`
	Settings    string `json:"settings"`
	QualifiedAt string `json:"qualified_at"`
}

// NewLowPowerQualification supplies the current identity for release tooling.
// The caller must write it only after successful quality/correctness validation.
func NewLowPowerQualification() LowPowerQualification {
	return LowPowerQualification{CapabilityFingerprint(), "h264-8bit-sdr", qualificationSettings, time.Now().UTC().Format(time.RFC3339)}
}

func CapabilityFingerprint() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(softwareIdentity())))
}

func lowPowerQualified(path string) bool {
	f, e := os.Open(path)
	if e != nil {
		return false
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !safeQualificationFile(info) {
		return false
	}
	var q LowPowerQualification
	if json.NewDecoder(io.LimitReader(f, 4096)).Decode(&q) != nil {
		return false
	}
	_, e = time.Parse(time.RFC3339, q.QualifiedAt)
	return e == nil && q.Fingerprint == CapabilityFingerprint() && q.InputClass == "h264-8bit-sdr" && q.Settings == qualificationSettings
}

// LowPowerStatus reports qualification, not hardware availability. Each eligible
// job still requires a successful current-boot production-chain probe.
func (t *Transcoder) LowPowerStatus() string {
	t.mu.RLock()
	policy := t.lowPower
	t.mu.RUnlock()
	if policy == "off" {
		return "off"
	}
	if lowPowerQualified(t.qualificationPath) {
		return "qualified"
	}
	return "unqualified"
}

type videoClass struct {
	Codec     string `json:"codec_name"`
	Profile   string `json:"profile"`
	Pixel     string `json:"pix_fmt"`
	Transfer  string `json:"color_transfer"`
	Space     string `json:"color_space"`
	Primaries string `json:"color_primaries"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Level     int    `json:"level"`
	Refs      int    `json:"refs"`
}

func (v videoClass) eligible() bool {
	if v.Codec != "h264" || v.Pixel != "yuv420p" || v.Width <= 0 || v.Height <= 0 {
		return false
	}
	switch v.Profile {
	case "Baseline", "Constrained Baseline", "Main", "High":
	default:
		return false
	}
	return !strings.Contains(v.Transfer, "2084") && v.Transfer != "arib-std-b67" &&
		!strings.Contains(v.Primaries, "2020") && !strings.Contains(v.Space, "2020")
}

// tailBuffer bounds diagnostic memory even for verbose or malicious inputs.
type tailBuffer struct {
	data  []byte
	limit int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n >= b.limit {
		b.data = append(b.data[:0], p[n-b.limit:]...)
		return n, nil
	}
	if len(b.data)+n > b.limit {
		b.data = b.data[len(b.data)+n-b.limit:]
	}
	b.data = append(b.data, p...)
	return n, nil
}

func inspectVideo(ctx context.Context, path string) (videoClass, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "V:0",
		"-show_entries", "stream=codec_name,profile,pix_fmt,color_transfer,color_space,color_primaries,width,height,level,refs",
		"-of", "json", path)
	out := &tailBuffer{limit: 64 * 1024}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		return videoClass{}, err
	}
	var result struct {
		Streams []videoClass `json:"streams"`
	}
	if err := json.Unmarshal(out.data, &result); err != nil {
		return videoClass{}, err
	}
	if len(result.Streams) != 1 {
		return videoClass{}, fmt.Errorf("no primary video stream")
	}
	return result.Streams[0], nil
}

// Identity includes binaries, driver library, kernel boot and render device.
// Restart clears the bounded cache. File changes invalidate it without restart.
func capabilityIdentity() string {
	boot, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return softwareIdentity() + string(boot)
}

func softwareIdentity() string {
	paths := []string{vaDevice}
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if p, e := exec.LookPath(bin); e == nil {
			paths = append(paths, p)
		}
	}
	for _, pattern := range []string{"/usr/lib/*/dri/*_drv_video.so", "/usr/lib64/dri/*_drv_video.so", "/usr/local/lib/dri/*_drv_video.so", "/usr/lib/*/libav*.so*", "/usr/lib/*/libsw*.so*", "/usr/lib/*/libva.so*", "/usr/lib/*/libigdgmm.so*", "/usr/local/lib/libav*.so*", "/usr/local/lib/libva.so*"} {
		matches, _ := filepath.Glob(pattern)
		paths = append(paths, matches...)
	}
	var b strings.Builder
	for _, p := range []string{"/proc/sys/kernel/osrelease", "/sys/module/i915/version", "/sys/class/drm/renderD128/device/uevent"} {
		if value, e := os.ReadFile(p); e == nil {
			b.Write(value)
		}
	}
	for _, p := range paths {
		if s, e := os.Stat(p); e == nil && p != vaDevice {
			fmt.Fprintf(&b, "%s:%d:%d:%s;", p, s.Size(), s.ModTime().UnixNano(), s.Mode())
		}
	}
	return b.String()
}

func vaapiArgs(input, output string, decode bool) []string {
	args := []string{"-y", "-nostdin", "-init_hw_device", "vaapi=va:" + vaDevice, "-filter_hw_device", "va"}
	filter := `format=nv12,hwupload,scale_vaapi=w=min(1920\,iw):h=-2`
	if decode {
		args = append(args, "-hwaccel", "vaapi", "-hwaccel_device", "va", "-hwaccel_output_format", "vaapi", "-extra_hw_frames", "16")
		filter = `scale_vaapi=w=min(1920\,iw):h=-2`
	}
	args = append(args, "-i", input, "-map", "0:V:0", "-map", "0:a?", "-map", "0:s?", "-map", "0:t?",
		"-map_metadata", "0", "-map_chapters", "0", "-vf", filter,
		"-c:v", "hevc_vaapi", "-async_depth", "2", "-b:v", "2300k", "-maxrate", "4000k", "-bufsize", "4600k",
		"-c:a", "copy", "-c:s", "copy", "-c:t", "copy", "-max_muxing_queue_size", "9999", output)
	return args
}

func (t *Transcoder) probeDecode(ctx context.Context, input string, v videoClass) bool {
	return t.probeChain(ctx, input, v, false)
}

func lowPowerArgs(input, output string) []string {
	args := vaapiArgs(input, output, true)
	return append(args[:len(args)-1], "-low_power", "1", "-rc_mode", "VBR", output)
}

func (t *Transcoder) probeChain(ctx context.Context, input string, v videoClass, lowPower bool) bool {
	key := fmt.Sprintf("%s|%+v|%s|LP%t", capabilityIdentity(), v, qualificationSettings, lowPower)
	t.mu.Lock()
	result, ok := t.probes[key]
	t.mu.Unlock()
	if ok {
		return result
	}
	probeCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	args := vaapiArgs(input, "-", true)
	// Exercise the exact video chain while excluding unrelated mux/audio support.
	i := 0
	for n, a := range args {
		if a == "-map" {
			i = n
			break
		}
	}
	videoStart := 0
	for n, a := range args {
		if a == "-vf" {
			videoStart = n
			break
		}
	}
	videoEnd := 0
	for n, a := range args {
		if a == "-c:a" {
			videoEnd = n
			break
		}
	}
	probeArgs := append([]string{}, args[:i]...)
	probeArgs = append(probeArgs, "-map", "0:V:0")
	probeArgs = append(probeArgs, args[videoStart:videoEnd]...)
	if lowPower {
		probeArgs = append(probeArgs, "-low_power", "1", "-rc_mode", "VBR")
	}
	probeArgs = append(probeArgs, "-frames:v", "24", "-an", "-sn", "-progress", "pipe:1", "-f", "null", "-")
	cmd := exec.CommandContext(probeCtx, "ffmpeg", probeArgs...)
	cmd.Env = append(os.Environ(), "LIBVA_DRIVER_NAME=iHD")
	out := &tailBuffer{limit: 8192}
	cmd.Stderr = out
	progress := &tailBuffer{limit: 8192}
	cmd.Stdout = progress
	result = cmd.Run() == nil && strings.Contains(string(progress.data), "frame=24\n")
	if ctx.Err() != nil {
		return false
	}
	// Failures may be corrupt input, transient device load or timeout, not a
	// class-wide lack of capability. Only successful probes are cached.
	if !result {
		return false
	}
	t.mu.Lock()
	if len(t.probes) >= 64 {
		t.probes = make(map[string]bool)
	}
	t.probes[key] = result
	t.mu.Unlock()
	return result
}

// SetPolicy changes future jobs only. Low-power auto remains unqualified until
// release-quality validation exists; a successful initialization alone is not sufficient.
func (t *Transcoder) SetPolicy(hardwareDecode, lowPower string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hardwareDecode = hardwareDecode
	t.lowPower = lowPower
}

// RefreshCapabilities invalidates cached probes (also cleared on process restart).
func (t *Transcoder) RefreshCapabilities() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.probes = make(map[string]bool)
}
