package transcoder

import (
	"bytes"
	"fmt"
	"log/slog"
	"os/exec"
)

// EncoderType identifies the hardware/software encoder to use.
type EncoderType string

const (
	EncoderVAAPI        EncoderType = "vaapi"
	EncoderVideoToolbox EncoderType = "videotoolbox"
	EncoderNVENC        EncoderType = "nvenc"
	EncoderSoftware     EncoderType = "software"
)

// Encoder holds a detected encoder and the ffmpeg flags needed to invoke it.
type Encoder struct {
	Type        EncoderType
	DisplayName string
	// BuildArgs returns the full ffmpeg argument slice for encoding a file.
	// inputPath is the source, outputPath is the destination.
	BuildArgs func(inputPath, outputPath string) []string
}

// Detect probes the system and returns the best available encoder.
func Detect() (*Encoder, error) {
	if enc := probeVAAPI(); enc != nil {
		return enc, nil
	}
	if enc := probeVideoToolbox(); enc != nil {
		return enc, nil
	}
	if enc := probeNVENC(); enc != nil {
		return enc, nil
	}
	return softwareEncoder(), nil
}

// DetectWithLogging probes the system and returns the best available encoder.
// It logs detailed information about each probe attempt for diagnostics.
func DetectWithLogging(log *slog.Logger) (*Encoder, error) {
	log.Info("Starting hardware encoder detection")

	probes := []struct {
		name   string
		detect func(*slog.Logger) *Encoder
	}{
		{"VAAPI (Intel/AMD)", func(l *slog.Logger) *Encoder { return probeVAAPIWithLogging(l) }},
		{"VideoToolbox (Apple)", func(l *slog.Logger) *Encoder { return probeVideoToolboxWithLogging(l) }},
		{"NVENC (NVIDIA)", func(l *slog.Logger) *Encoder { return probeNVENCWithLogging(l) }},
	}

	var selected *Encoder
	for _, p := range probes {
		enc := p.detect(log)
		if enc != nil {
			selected = enc
			log.Info("Hardware encoder available", "encoder", p.name)
			break
		}
	}

	if selected == nil {
		selected = softwareEncoder()
		log.Info("No hardware encoders available, using software fallback")
	}

	log.Info("Encoder selection complete", "selected", selected.DisplayName)
	return selected, nil
}

// DetectAll probes every encoder independently and returns all that are
// available. The software encoder is always included as the last entry.
func DetectAll() []*Encoder {
	var encoders []*Encoder
	if enc := probeVAAPI(); enc != nil {
		encoders = append(encoders, enc)
	}
	if enc := probeVideoToolbox(); enc != nil {
		encoders = append(encoders, enc)
	}
	if enc := probeNVENC(); enc != nil {
		encoders = append(encoders, enc)
	}
	encoders = append(encoders, softwareEncoder())
	return encoders
}

// DetectByType returns the encoder for a specific type without probing.
// Returns nil if the type is unknown.
func DetectByType(t EncoderType) *Encoder {
	switch t {
	case EncoderVAAPI:
		return vaapiEncoder()
	case EncoderVideoToolbox:
		return videoToolboxEncoder()
	case EncoderNVENC:
		return nvencEncoder()
	case EncoderSoftware:
		return softwareEncoder()
	default:
		return nil
	}
}

func probeVAAPI() *Encoder {
	// Check that /dev/dri/renderD128 exists and hevc_vaapi is available.
	if err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Run(); err != nil {
		return nil
	}
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil {
		return nil
	}
	// Require hevc_vaapi encoder in ffmpeg output.
	if !containsBytes(out, []byte("hevc_vaapi")) {
		return nil
	}
	// Quick device probe.
	probe := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-init_hw_device", "vaapi=va:/dev/dri/renderD128",
		"-f", "lavfi", "-i", "nullsrc=s=64x64:d=0.1",
		"-vf", "format=nv12,hwupload",
		"-c:v", "hevc_vaapi",
		"-frames:v", "1",
		"-f", "null", "-")
	if probe.Run() != nil {
		return nil
	}
	return vaapiEncoder()
}

func probeVAAPIWithLogging(log *slog.Logger) *Encoder {
	log.Debug("Probing VAAPI encoder", "device", "/dev/dri/renderD128")

	// Check hevc_vaapi availability
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil {
		log.Debug("Failed to query ffmpeg encoders", "error", err)
		return nil
	}
	if !containsBytes(out, []byte("hevc_vaapi")) {
		log.Debug("VAAPI encoder hevc_vaapi not found in ffmpeg")
		return nil
	}

	// Test device availability
	probe := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-init_hw_device", "vaapi=va:/dev/dri/renderD128",
		"-f", "lavfi", "-i", "nullsrc=s=64x64:d=0.1",
		"-vf", "format=nv12,hwupload",
		"-c:v", "hevc_vaapi",
		"-frames:v", "1",
		"-f", "null", "-")
	if err := probe.Run(); err != nil {
		log.Debug("VAAPI device test failed", "device", "/dev/dri/renderD128", "error", err)
		return nil
	}

	log.Debug("VAAPI probe successful")
	return vaapiEncoder()
}

func probeVideoToolbox() *Encoder {
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil || !containsBytes(out, []byte("hevc_videotoolbox")) {
		return nil
	}
	return videoToolboxEncoder()
}

func probeVideoToolboxWithLogging(log *slog.Logger) *Encoder {
	log.Debug("Probing VideoToolbox encoder")

	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil {
		log.Debug("Failed to query ffmpeg encoders", "error", err)
		return nil
	}
	if !containsBytes(out, []byte("hevc_videotoolbox")) {
		log.Debug("VideoToolbox encoder hevc_videotoolbox not found in ffmpeg")
		return nil
	}

	log.Debug("VideoToolbox probe successful")
	return videoToolboxEncoder()
}

func probeNVENC() *Encoder {
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil || !containsBytes(out, []byte("hevc_nvenc")) {
		return nil
	}
	return nvencEncoder()
}

func probeNVENCWithLogging(log *slog.Logger) *Encoder {
	log.Debug("Probing NVENC encoder")

	// Check hevc_nvenc availability
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil {
		log.Debug("Failed to query ffmpeg encoders", "error", err)
		return nil
	}
	if !containsBytes(out, []byte("hevc_nvenc")) {
		log.Debug("NVENC encoder hevc_nvenc not found in ffmpeg")
		return nil
	}

	// Test CUDA device availability
	probe := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-init_hw_device", "cuda=0",
		"-f", "lavfi", "-i", "nullsrc=s=64x64:d=0.1",
		"-c:v", "hevc_h264", // Dummy encoder, just testing device
		"-frames:v", "1",
		"-f", "null", "-")
	if err := probe.Run(); err != nil {
		log.Debug("NVENC CUDA device test failed", "error", err)
		return nil
	}

	log.Debug("NVENC probe successful")
	return nvencEncoder()
}

func vaapiEncoder() *Encoder {
	return &Encoder{
		Type:        EncoderVAAPI,
		DisplayName: "Intel VAAPI (hevc_vaapi)",
		BuildArgs: func(inputPath, outputPath string) []string {
			return []string{
				"-y",
				"-init_hw_device", "vaapi=va:/dev/dri/renderD128",
				"-filter_hw_device", "va",
				"-i", inputPath,
				"-vf", `format=nv12,hwupload,scale_vaapi=w=min(1920\,iw):h=-2`,
				"-c:v", "hevc_vaapi",
				"-b:v", "2300k",
				"-maxrate", "4000k",
				"-bufsize", "4600k",
				"-c:a", "copy",
				"-c:s", "copy",
				"-max_muxing_queue_size", "9999",
				outputPath,
			}
		},
	}
}

func videoToolboxEncoder() *Encoder {
	return &Encoder{
		Type:        EncoderVideoToolbox,
		DisplayName: "Apple VideoToolbox (hevc_videotoolbox)",
		BuildArgs: func(inputPath, outputPath string) []string {
			return []string{
				"-y",
				"-i", inputPath,
				"-c:v", "hevc_videotoolbox",
				"-b:v", "2300k",
				"-maxrate", "4000k",
				"-bufsize", "4600k",
				"-c:a", "copy",
				"-c:s", "copy",
				"-max_muxing_queue_size", "9999",
				outputPath,
			}
		},
	}
}

func nvencEncoder() *Encoder {
	return &Encoder{
		Type:        EncoderNVENC,
		DisplayName: "NVIDIA NVENC (hevc_nvenc)",
		BuildArgs: func(inputPath, outputPath string) []string {
			return []string{
				"-y",
				"-i", inputPath,
				"-c:v", "hevc_nvenc",
				"-preset", "p4",
				"-b:v", "2300k",
				"-maxrate", "4000k",
				"-bufsize", "4600k",
				"-c:a", "copy",
				"-c:s", "copy",
				"-max_muxing_queue_size", "9999",
				outputPath,
			}
		},
	}
}

func softwareEncoder() *Encoder {
	return &Encoder{
		Type:        EncoderSoftware,
		DisplayName: "Software (libx265)",
		BuildArgs: func(inputPath, outputPath string) []string {
			return []string{
				"-y",
				"-i", inputPath,
				"-c:v", "libx265",
				"-crf", "23",
				"-preset", "medium",
				"-b:v", "0",
				"-c:a", "copy",
				"-c:s", "copy",
				"-max_muxing_queue_size", "9999",
				outputPath,
			}
		},
	}
}

func (e *Encoder) String() string {
	return fmt.Sprintf("%s", e.DisplayName)
}

func containsBytes(haystack, needle []byte) bool {
	return bytes.Contains(haystack, needle)
}
