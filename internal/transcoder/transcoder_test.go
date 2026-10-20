package transcoder

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEligibility(t *testing.T) {
	base := videoClass{Codec: "h264", Profile: "High", Pixel: "yuv420p", Width: 1280, Height: 720, Transfer: "bt709"}
	if !base.eligible() {
		t.Fatal("SDR h264 should qualify")
	}
	for _, change := range []func(*videoClass){
		func(v *videoClass) { v.Pixel = "yuv420p10le" },
		func(v *videoClass) { v.Transfer = "smpte2084" },
		func(v *videoClass) { v.Transfer = "arib-std-b67" },
		func(v *videoClass) { v.Primaries = "bt2020" },
		func(v *videoClass) { v.Codec = "hevc" },
		func(v *videoClass) { v.Profile = "High 10" },
		func(v *videoClass) { v.Width = 0 },
	} {
		v := base
		change(&v)
		if v.eligible() {
			t.Errorf("unexpected eligibility: %+v", v)
		}
	}
}

func TestHardwareArgs(t *testing.T) {
	args := vaapiArgs("input.mkv", "out.mkv", true)
	index := func(s string) int {
		for i, a := range args {
			if a == s {
				return i
			}
		}
		return -1
	}
	if index("-hwaccel") < 0 || index("-hwaccel") > index("-i") {
		t.Fatal("decode options must precede input")
	}
	for k, v := range map[string]string{"-b:v": "2300k", "-maxrate": "4000k", "-bufsize": "4600k", "-async_depth": "2", "-c:a": "copy", "-c:s": "copy", "-c:t": "copy"} {
		i := index(k)
		if i < 0 || args[i+1] != v {
			t.Errorf("%s mismatch", k)
		}
	}
	joined := strings.Join(args, " ")
	for _, s := range []string{"-map 0:V:0", "-map 0:a?", "-map 0:s?", "-map 0:t?", "-map_metadata 0", "-map_chapters 0"} {
		if !strings.Contains(joined, s) {
			t.Error("missing", s)
		}
	}
	if strings.Contains(joined, "hwupload") || strings.Contains(joined, "-qp") {
		t.Fatal("hardware decode must remain GPU resident and VBR")
	}
}

func TestRetryClassification(t *testing.T) {
	for _, s := range []string{
		"Stream mapping hevc_vaapi\nNo space left on device",
		"/dev/dri/renderD128: Permission denied",
		"hevc_vaapi\nInvalid data found when processing input",
		"vaapi initialization\nError opening output",
		"vaapi normal startup\nunknown mux error",
		"failed setup for format vaapi\nError while decoding stream",
	} {
		if isDeviceError(errors.New(s)) {
			t.Error("must not retry", s)
		}
	}
	if !isDeviceError(errors.New("Device creation failed: -5")) {
		t.Fatal("device failure must retry")
	}
}

func TestBoundedTail(t *testing.T) {
	b := &tailBuffer{limit: 8}
	b.Write([]byte("123456"))
	b.Write([]byte("789"))
	b.Write([]byte("abcdefghijk"))
	if string(b.data) != "defghijk" {
		t.Fatal(string(b.data))
	}
}

func fakeCommands(t *testing.T, ffmpeg string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixtures run in locally cross-built Linux test binary")
	}
	dir := t.TempDir()
	for name, body := range map[string]string{
		"ffmpeg":  ffmpeg,
		"ffprobe": `printf '%s' '{"streams":[{"codec_name":"h264","profile":"High","pix_fmt":"yuv420p","width":1280,"height":720,"color_transfer":"bt709"}]}'`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func TestProbeSuccessCacheRefreshAndZeroFrames(t *testing.T) {
	dir := fakeCommands(t, `echo x >> "$PROBE_COUNT"; printf 'frame=24\nprogress=end\n'`)
	count := filepath.Join(dir, "count")
	t.Setenv("PROBE_COUNT", count)
	tr := New(vaapiEncoder(), t.TempDir(), nil)
	v := videoClass{Codec: "h264", Profile: "High", Pixel: "yuv420p", Width: 1280, Height: 720}
	for i := 0; i < 2; i++ {
		if !tr.probeDecode(context.Background(), "source", v) {
			t.Fatal("probe failed")
		}
	}
	data, _ := os.ReadFile(count)
	if string(data) != "x\n" {
		t.Fatal("not cached", string(data))
	}
	tr.RefreshCapabilities()
	if !tr.probeDecode(context.Background(), "source", v) {
		t.Fatal("refresh probe failed")
	}
	data, _ = os.ReadFile(count)
	if string(data) != "x\nx\n" {
		t.Fatal("refresh did not probe")
	}
	// Binary identity changes invalidate previous success; zero-frame success is not enough.
	os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("#!/bin/sh\nprintf 'frame=0\\nprogress=end\\n'\n"), 0700)
	if tr.probeDecode(context.Background(), "source", v) {
		t.Fatal("zero frames incorrectly qualified")
	}
	if len(tr.probes) != 1 {
		t.Fatal("negative result must not poison class cache")
	}
}

func TestQualificationFingerprintIgnoresExecutablePathAliases(t *testing.T) {
	realDir := fakeCommands(t, `printf 'frame=24\nprogress=end\n'`)
	originalPath := os.Getenv("PATH")
	first := CapabilityFingerprint()
	aliasDir := filepath.Join(t.TempDir(), "bin")
	if err := os.Symlink(realDir, aliasDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", aliasDir+string(os.PathListSeparator)+originalPath)
	if got := CapabilityFingerprint(); got != first {
		t.Fatal("PATH directory alias invalidated the same qualified executables")
	}

	// A different installation must still invalidate qualification, even if
	// its executable has the same basename and is found through an alias.
	otherDir := fakeCommands(t, `printf 'different ffmpeg build\n'`)
	t.Setenv("PATH", otherDir+string(os.PathListSeparator)+originalPath)
	if got := CapabilityFingerprint(); got == first {
		t.Fatal("different executable installation retained qualification")
	}
}

func TestPerJobFallbackAndScratchIsolation(t *testing.T) {
	fakeCommands(t, `
case "$*" in
 *hevc_vaapi*) echo 'Device creation failed' >&2; exit 1;;
esac
for last do :; done
printf 'encoded' > "$last"
`)
	dir := t.TempDir()
	tr := New(vaapiEncoder(), dir, nil)
	tr.SetPolicy("off", "off")
	const jobs = 8
	var wg sync.WaitGroup
	paths := make(chan string, jobs)
	errs := make(chan error, jobs)
	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := tr.Run(context.Background(), "same.mkv", 1, nil, nil)
			if e != nil {
				errs <- e
			} else {
				paths <- p
			}
		}()
	}
	for i := 0; i < jobs; i++ {
		tr.SetEncoder(vaapiEncoder())
		tr.SetPolicy("off", "off")
	}
	wg.Wait()
	close(paths)
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	seen := map[string]bool{}
	for p := range paths {
		if seen[p] {
			t.Fatal("scratch collision")
		}
		seen[p] = true
	}
	if len(seen) != jobs {
		t.Fatal("missing outputs")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != jobs {
		t.Fatal("failed scratch leaked")
	}
	if tr.Encoder().Type != EncoderVAAPI {
		t.Fatal("fallback mutated shared encoder")
	}
}

func TestCancellationAndIOFailureDoNotRetry(t *testing.T) {
	dir := fakeCommands(t, `echo x >> "$ATTEMPTS"; echo 'No space left on device (hevc_vaapi)' >&2; exit 1`)
	count := filepath.Join(dir, "count")
	t.Setenv("ATTEMPTS", count)
	outDir := t.TempDir()
	tr := New(vaapiEncoder(), outDir, nil)
	tr.SetPolicy("off", "off")
	if _, err := tr.Run(context.Background(), "source.mkv", 1, nil, nil); err == nil {
		t.Fatal("expected IO error")
	}
	data, _ := os.ReadFile(count)
	if string(data) != "x\n" {
		t.Fatal("IO error retried")
	}
	files, _ := os.ReadDir(outDir)
	if len(files) != 0 {
		t.Fatal("scratch leaked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tr.Run(ctx, "source.mkv", 1, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestOutputReadableByMediaServices(t *testing.T) {
	fakeCommands(t, `
for last do :; done
printf 'encoded' > "$last"
`)
	tr := New(softwareEncoder(), t.TempDir(), nil)
	output, err := tr.Run(context.Background(), "source.mkv", 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("output permissions %o prevent expected media-service access", info.Mode().Perm())
	}
}

func TestProbeCancellation(t *testing.T) {
	fakeCommands(t, `exec sleep 60`)
	tr := New(vaapiEncoder(), t.TempDir(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if tr.probeDecode(ctx, "source", videoClass{}) {
		t.Fatal("cancelled probe succeeded")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancellation was not bounded")
	}
	if !reflect.DeepEqual(tr.probes, map[string]bool{}) {
		t.Fatal("cancelled probe cached")
	}
}

func TestLowPowerQualificationAndFallback(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("root-owned Linux qualification fixture")
	}
	dir := fakeCommands(t, `
echo "$*" >> "$ARG_LOG"
for last do :; done
if [ "$last" = "-" ]; then printf 'frame=24\nprogress=end\n'; exit 0; fi
case "$*" in
 *"-low_power 1"*) echo 'Failed to create encode configuration' >&2; exit 1;;
esac
printf 'encoded' > "$last"
`)
	logPath := filepath.Join(dir, "args")
	t.Setenv("ARG_LOG", logPath)
	path := filepath.Join(dir, "qualified.json")
	record := NewLowPowerQualification()
	write := func() {
		data, _ := json.Marshal(record)
		if e := os.WriteFile(path, data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write()
	if !lowPowerQualified(path) {
		t.Fatal("valid qualification rejected")
	}
	os.Chmod(path, 0666)
	if lowPowerQualified(path) {
		t.Fatal("unsafe writable qualification accepted")
	}
	os.Chmod(path, 0600)
	record.Fingerprint = "wrong"
	write()
	if lowPowerQualified(path) {
		t.Fatal("identity mismatch accepted")
	}
	record = NewLowPowerQualification()
	write()
	tr := New(vaapiEncoder(), t.TempDir(), nil)
	tr.qualificationPath = path
	var logs []string
	out, err := tr.Run(context.Background(), "source.mkv", 1, nil, func(s string) { logs = append(logs, s) })
	if err != nil || out == "" {
		t.Fatal(out, err)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "qualified low-power HEVC VBR") || !strings.Contains(joined, "Hardware capability failed; retrying with VAAPI decode -> standard") {
		t.Fatal("missing bounded LP -> standard fallback", joined)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "-low_power 1 -rc_mode VBR") {
		t.Fatal("LP did not enforce VBR")
	}
	if strings.Contains(string(data), "libx265") {
		t.Fatal("successful standard attempt should stop retries")
	}
}
