//go:build integration

package verifier_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/danrichardson/sqzarr/internal/verifier"
)

func TestVerifyPassesValidOutput(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "input.mkv")
	output := filepath.Join(dir, "output.mkv")

	run(t, "ffmpeg", "-y", "-f", "lavfi",
		"-i", "testsrc=duration=30:size=1280x720:rate=25",
		"-c:v", "libx264", "-b:v", "8000k", input)

	run(t, "ffmpeg", "-y", "-i", input,
		"-c:v", "libx264", "-b:v", "1000k", output)

	result, err := verifier.Verify(input, output, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Errorf("expected OK, got: %s", result.Reason)
	}
	if result.Uncompressible {
		t.Error("should not be marked uncompressible")
	}
}

func TestVerifyFailsWhenOutputLarger(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.mkv")
	output := filepath.Join(dir, "output.mkv")

	// The size gate precedes probing. Keep both files above its absolute
	// floor so this tests uncompressibility, not a variable encoder bitrate.
	if err := os.WriteFile(input, make([]byte, 2*1024*1024), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, make([]byte, 3*1024*1024), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := verifier.Verify(input, output, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Error("expected verification to fail (output larger than input)")
	}
	if !result.Uncompressible {
		t.Error("expected Uncompressible=true when output >= input size")
	}
}

func TestVerifyFailsSuspiciouslySmallOutput(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "input.mkv")

	run(t, "ffmpeg", "-y", "-f", "lavfi",
		"-i", "testsrc=duration=30:size=1280x720:rate=25",
		"-c:v", "libx264", "-b:v", "8000k", input)

	// Write a tiny fake output that is well below the size floor.
	output := filepath.Join(dir, "output.mkv")
	if err := os.WriteFile(output, []byte("not a real video"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := verifier.Verify(input, output, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Error("expected failure for suspiciously small output")
	}
	if result.Uncompressible {
		t.Error("suspiciously small output should not be marked uncompressible")
	}
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH, skipping integration test")
	}
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
