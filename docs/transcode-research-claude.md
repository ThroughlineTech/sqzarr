# pve2 vs pve5 HEVC transcode parity - research findings

Scope: research only. Nothing here was run against the hosts. "Established" = backed by Intel/kernel/FFmpeg sources or by your own measurements. "Hypothesis" = inference that still needs a test.

---

## 0. Bottom line

1. **Both hosts are almost certainly running the same class of encoder path, and it is not the one where Tiger Lake's media engine pulls ahead.**
   - intel-media-driver lists Kaby Lake family (KBLx, which covers the i5-8350U) HEVC 8-bit encode as `Es` only: PAK fixed-function plus shader kernels (media kernel + VME). No HEVC VDEnc on that generation.
   - Tiger Lake lists both `E` (VDEnc/HuC low-power) and `Es`.
   - HuC firmware is required for VDEnc bitrate control (CBR/VBR). Intel's README states the default kernel config does not load HuC on TGL/RKL; it is on by default only from ADL onward. The i915 source carries an explicit "don't enable GuC/HuC on Tiger Lake/Rocket Lake" default.
   - Your observation ("low-power HEVC with bitrate settings failed, only CQP, HuC disabled") matches this exactly. Your command does not set `-low_power 1`, so with `-b:v`/`-maxrate` it lands on the `Es` entrypoint (`VAEntrypointEncSlice`) on both hosts.
2. **The pipeline is probably bounded by serialized per-frame work, mostly CPU-side, not by GPU encode capacity.**
   - Software H.264 decode, `format=nv12` (swscale), `hwupload` (CPU copy into a GPU surface), a no-op `scale_vaapi` pass, then encode with a shallow in-flight queue, all on 2 cores.
   - Your BIOS experiment (CPU capped at 1.2 GHz -> ~3.8x, GPU flags clear, temps 52-55 C, ffmpeg ~177% of 200% CPU) is the strongest evidence in the dataset. CPU speed moves the result a lot; GPU headroom was present.
   - Both GPUs sit around 900-1000 MHz mean actual. If the GPU were the ceiling, the 96-EU Xe-LP part should have separated from the 24-EU UHD 620 on a shader-assisted path.
3. **Answer to F: no, a large single-stream speedup should not be expected from this command on this 720p source.** The generation gap should appear in: (a) VDEnc low-power encode (needs HuC for VBR, or CQP without it), (b) multiple concurrent streams, (c) higher resolutions where GPU work dominates, and (d) a GPU-resident pipeline (hardware decode, no CPU conversion/upload). A ~5% single-stream gap under the current pipeline is consistent with a CPU-front-end ceiling plus a modest IPC/clock edge for the 1185G7.

---

## A. Ranked hypotheses

### H1 - CPU-side front end (SW decode + swscale + hwupload) sets the ceiling. Confidence: high
- **Supports:** 1.2 GHz cap dropped throughput ~45% with GPU unconstrained; ffmpeg near 2-core saturation (~177%); GPU clocks similar on both hosts; i7-1185G7 at ~4.3 GHz vs i5-8350U at ~3.5-3.6 GHz is roughly consistent with the small observed delta.
- **Against:** 720p H.264 software decode alone on two modern cores should run well above ~155 fps (6.5x of 23.976). So decode alone is likely not the whole ceiling; serialization (H2) or upload cost probably adds to it.
- **Falsify:** decode-only and decode+upload runs (E1, E2) on pve5 reaching far above 155 fps, and full-GPU pipeline (E4) showing no gain.

### H2 - Shallow async queue / synchronous handoffs make it latency-bound. Confidence: medium
- Recent FFmpeg VAAPI encoders expose `async_depth` (max in-flight frames). Default is small (confirm with `ffmpeg -h encoder=hevc_vaapi`; 2 in recent releases). With CPU upload and GPU encode in series and little overlap, per-frame wall time = CPU stage + GPU stage, and a faster GPU only shrinks one term.
- **Falsify:** `async_depth` sweep (E5) produces no change on either host.

### H3 - Both hosts use the `Es` path; TGL's VDEnc advantage is not engaged. Confidence: high that the path is `Es`; medium that this matters for the current ceiling
- Established from driver docs and your failed LP attempt. Whether it matters depends on H1/H2: if the front end is the ceiling, switching encoder path changes little until the front end is fixed.
- **Falsify:** `vainfo` on pve5 showing HuC loaded, or the LP-CQP test (E6) not running faster than `Es`-CQP.

### H4 - pve5 GPU capped below its max clock by power/platform policy. Confidence: medium-low for the current results, high for the earlier history
- **Supports:** actual peaks at 1100 MHz on pve5 while requests sit at ~1340 MHz (TGL GPU max is 1350). History items 1-3 show MMIO PL1 dropping to 8 W and PL1 flags pinning GPU near 600-700 MHz under the earlier platform-profile states.
- **Against:** latest runs show PL1 flag mostly clear, and GPU-boundness is doubtful per H1.
- **Unknown:** which non-PL1 throttle reason (if any) is capping at 1100. Read all `throttle_reason_*` files, not just PL1.
- **Falsify:** throttle reasons all clear and `rps_RP0` / `gt_max` read 1100 (i.e. the cap is a configured ceiling, not throttling).

### H5 - cpufreqd's hot-CPU rule throttles CPU mid-run. Confidence: low-medium
- Rule needs temp 55-100 C AND CPU usage 50-100%. pve5 package temps exceed 55 C during runs. Whether "CPU usage" is system-wide (2 of 8 busy threads = ~25%, likely not triggering) or per-CPU decides it. With acpi-cpufreq, an 80% cap of a 3.001 GHz "turbo" max would likely land below base and remove turbo, which would matter a lot under H1.
- Both hosts carry the same rule, so if it fires it would also explain pve2 variance.
- **Falsify:** 250 ms sampling of `scaling_max_freq`/`scaling_governor` on the test CPUs plus cpufreqd log output shows no profile change during runs.

### H6 - Software/driver/quality differences between the two userspaces. Confidence: medium (unverified)
- pve2 runs CT260's userspace via chroot; pve5 runs inside CT261. FFmpeg, libva and iHD versions are not yet inventoried. The free build of intel-media-driver lacks shader (`Es`) encode; the non-free build has it.
- Unset quality level (`-compression_level`), GOP, B-frame and reference settings resolve to driver defaults that can differ per platform/path. Faster can mean "doing less work per frame".
- **Falsify:** identical versions and identical output stream parameters (frame types, ref count, bitrate, VMAF) on both hosts.

### H7 - Host contention on pve5 (Plex, other guests). Confidence: low-medium
- pve5 runs other guests; pve2 testing was offline. Even with pinning, shared LLC, memory bandwidth, GPU contention (Plex transcodes would share the same render node) and package power budget are all shared.
- **Falsify:** pve5 run with other guests confirmed idle (or GPU engine-busy at 0 before start) gives the same result.

### H8 - Memory configuration. Confidence: low
- Uploads are memory-bound copies. Single- vs dual-channel memory on pve2 would affect upload cost. X1 Carbon Gen 9 uses soldered LPDDR4x. Inventory only.

---

## B. Read-only inventory and telemetry checklist

Run on the host unless noted. `card0` may be `card1`; check `ls /sys/class/drm`.

**Software versions (inside each test userspace: CT261, and the CT260 chroot)**
```sh
ffmpeg -hide_banner -version | head -3
ffmpeg -hide_banner -h encoder=hevc_vaapi        # defaults: async_depth, rc_mode, bf, g, compression_level, low_power
LIBVA_DRIVER_NAME=iHD vainfo --display drm --device /dev/dri/renderD128   # driver version; HEVCMain entrypoints: EncSlice vs EncSliceLP
dpkg -l | grep -Ei 'intel-media|libva|libigdgmm|ffmpeg|jellyfin-ffmpeg'  # free vs non-free iHD build
```

**Kernel / firmware**
```sh
uname -r; cat /proc/cmdline
lspci -nnk -s 00:02.0                         # i915 vs xe bound
cat /sys/module/i915/parameters/enable_guc   # expect 0 on TGL by default
dmesg | grep -iE 'guc|huc|dmc|i915.*firmware'
cat /sys/kernel/debug/dri/0/gt/uc/huc_info 2>/dev/null
cat /sys/kernel/debug/dri/0/gt/uc/guc_info 2>/dev/null
ls /lib/firmware/i915/ | grep -E '^(tgl|kbl)_'
```

**GPU frequency ceilings and throttle reasons**
```sh
D=/sys/class/drm/card0
grep . $D/gt_{min,max,boost,RP0,RP1,RPn,cur,act}_freq_mhz 2>/dev/null
grep . $D/gt/gt0/throttle_reason_* 2>/dev/null   # status, pl1, pl2, pl4, thermal, prochot, ratl, vr_thermalert, vr_tdc
```

**Power limits (MSR vs MMIO)**
```sh
for z in /sys/class/powercap/intel-rapl*/; do echo "$z $(cat $z/name)"; grep . $z/constraint_*_{name,power_limit_uw,time_window_us} 2>/dev/null; done
cat /sys/firmware/acpi/platform_profile /sys/firmware/acpi/platform_profile_choices
```

**CPU policy, effective state**
```sh
lscpu -e                                       # confirm 2,3 are distinct cores and their siblings
grep . /sys/devices/system/cpu/cpu{2,3}/cpufreq/{scaling_driver,scaling_governor,scaling_min_freq,scaling_max_freq,cpuinfo_max_freq}
cat /sys/devices/system/cpu/cpufreq/boost
grep . /sys/devices/system/cpu/cpu{2,3}/thermal_throttle/*
systemctl status cpufreqd tlp --no-pager
journalctl -u cpufreqd -b --no-pager | tail -50
tlp-stat -p                                    # read-only report of what TLP applied
```

**Container and contention (pve5)**
```sh
pct config 261
cat /sys/fs/cgroup/lxc/261/cpu.stat            # nr_throttled / throttled_usec before and after a run
cat /sys/fs/cgroup/lxc/261/memory.events
qm list; pct list                              # what else is running
```

**Memory**
```sh
dmidecode -t memory | grep -E 'Size|Speed|Locator|Type:'
```

**During each run (250 ms loop, timestamped)**
- `scaling_cur_freq` and `scaling_max_freq` for CPUs 2,3 (catches cpufreqd profile switches).
- `gt_act_freq_mhz`, `gt_cur_freq_mhz`, all `throttle_reason_*`.
- Package temp, thermal_throttle counters.
- Per-thread CPU of ffmpeg: `top -H -b -d 1 -p <pid>` (shows whether one decode/upload thread is pinned at 100%).

**Needs a package install (separate, low-risk, ask first)**
- `intel-gpu-tools` on the host: `intel_gpu_top -J -s 250` gives per-engine busy % (Render/3D, Video, VideoEnhance). This is the single most diagnostic measurement: `Es` encode should show Render/3D (EU kernels) plus Video (PAK) activity. Low busy % on both engines with ffmpeg at ~200% CPU confirms H1/H2.
- `linux-cpupower` (turbostat): actual per-core MHz from APERF/MPERF, Busy%, PkgWatt, GFXWatt, GFXMHz. `/proc/cpuinfo` snapshots are not reliable for this.

---

## C. Experiment matrix

All runs: local sample, null output, CPUs 2,3, three runs minimum, record the telemetry set above. "Base" = your current command. Each row changes one thing relative to Base.

### Low risk (no reboot, no config change)

**E0 - Instrumented baseline**
- Delta: none. Add `-benchmark` and the telemetry loop, plus `intel_gpu_top` if installed.
- Metrics: fps, utime/stime/rtime from `-benchmark`, per-thread CPU, engine busy %, GPU act/cur, throttle reasons.
- Reading: ffmpeg near 200% CPU + low engine busy = CPU/serialization bound. Render or Video engine near 100% = GPU bound on that engine.
- Risk: none beyond current tests.

**E1 - Decode-only ceiling**
```sh
taskset -c 2,3 ffmpeg -v error -nostdin -benchmark -stream_loop -1 -i /var/tmp/sqzarr-perf-sample.mkv -t 90 -an -sn -f null -
```
- Metrics: fps, CPU%.
- Reading: if this is only modestly above Base, decode is the ceiling. If it is far above (e.g. 3-5x), look at upload/sync (E2, E5).

**E2 - Decode + nv12 + upload, no encode**
```sh
... -vf 'format=nv12,hwupload' -c:v wrapped_avframe -f null -
```
(with the same `-init_hw_device`/`-filter_hw_device` args as Base)
- If `wrapped_avframe` rejects hardware frames on your build, fall back to `format=nv12,hwupload,hwdownload,format=nv12` and treat the result as a lower bound on front-end throughput (it adds a download).
- Reading: gap between E1 and E2 = conversion + upload cost. Gap between E2 and Base = encode + sync cost.

**E3 - Drop the no-op scale**
- Delta: `-vf 'format=nv12,hwupload'` (remove `scale_vaapi`).
- Reading: gain = cost of an extra VPP pass per frame. Output identical for 720p sources; SQZARR still needs the scale for >1080p sources, so this is diagnostic, or a conditional in SQZARR.
- Quality: none for 720p input.

**E4 - Full GPU-resident pipeline, no loop**
- Prereq: stream-copy a longer local sample (>=100 s video only) so `-stream_loop` is not used. This avoids the filter-reinit artifact you hit.
```sh
ffmpeg -ss 0 -t 100 -i <source> -map 0:v:0 -c copy -an -sn /var/tmp/sqzarr-perf-sample-100s.mkv
LIBVA_DRIVER_NAME=iHD taskset -c 2,3 ffmpeg -v error -nostdin -benchmark \
  -hwaccel vaapi -hwaccel_device /dev/dri/renderD128 -hwaccel_output_format vaapi \
  -i /var/tmp/sqzarr-perf-sample-100s.mkv -t 90 \
  -vf 'scale_vaapi=w=min(1920\,iw):h=-2' \
  -c:v hevc_vaapi -b:v 2300k -maxrate 4000k -bufsize 4600k \
  -an -sn -f null -
```
- Reading: large gain on both hosts = front end was the ceiling (H1 confirmed). Larger gain on pve5 than pve2 = generation gap starts showing once the CPU is out of the path.
- Comparability: Base must be rerun on the same 100 s non-looped file for an apples-to-apples pair. Decoded pixels are bit-identical for conformant H.264 hardware vs software decode, so quality impact should be nil; verify with E9.

**E5 - async_depth sweep**
- Delta: `-async_depth 1`, `4`, `8` (one value per run set).
- Reading: monotonic gain = latency-bound (H2). Flat = not the limit. Run on Base and on E4; it matters more once the CPU stage is removed.

**E6 - Encoder-path capability: Es-CQP vs LP-CQP (pve5), Es-CQP (pve2)**
```sh
# Es path, CQP
... -c:v hevc_vaapi -rc_mode CQP -qp 26 ...
# VDEnc path, CQP (pve5 only; HuC not required for CQP)
... -c:v hevc_vaapi -low_power 1 -rc_mode CQP -qp 26 ...
```
- Run inside the E4 pipeline so the CPU front end is out of the way.
- Reading: LP faster than Es on pve5 = VDEnc headroom exists and HuC-enabled VBR (E11) is worth the disruption. Similar = encoder path is not the limit.
- Comparability: CQP is not the production RC; output size will differ. Use only for capability, and compare quality per bit in E9.

**E7 - Quality-level parity**
- Delta: explicit `-compression_level N` (try the driver's documented range; record default from `-h`). Also pin `-bf`, `-g`, `-refs` to the same values on both hosts.
- Reading: if pve5's default is a slower/higher-quality preset than pve2's, matching it closes or widens the gap.

**E8 - Concurrency**
- Delta: 1, 2, 3, 4 simultaneous instances of the E4 pipeline. CPU allocation must be fixed and stated (e.g. all instances confined to the same 2 cores, then a separate set on 4 cores). Report aggregate fps and per-stream fps.
- Reading: pve5 aggregate rising further before plateau than pve2 = GPU capacity advantage that single-stream hides.

**E9 - Quality / correctness check (scratch output)**
- Delta: write `-f matroska /scratch/out-<host>-<test>.mkv` instead of null for 30 s.
- Metrics on the workstation: bitrate, frame-type counts (`ffprobe -show_frames -select_streams v`), VMAF/SSIM vs source.
- Reading: any speed result whose output is meaningfully lower VMAF at similar bitrate is not a like-for-like gain.

**E10 - Higher resolution**
- Delta: a real 1080p and a real 2160p H.264 or HEVC source, stream-copied locally, run through E4 with `scale_vaapi` to 1920 wide.
- Reading: newer hardware advantage should grow with pixel count when the pipeline is GPU-resident.

### Requires change approval (container config, no reboot)

**E12 - More CPU for the front end (Base pipeline)**
- Delta: allow 4 physical cores instead of 2 (offline chroot on pve2; on pve5 either use the same offline-chroot method or temporarily change CT261 cores/pinning).
- Reading: near-linear gain = H1. Rollback: restore `pct` config from the saved copy.
- Prefer running pve5's benchmark via chroot on the host like pve2, so both hosts use the same harness. That also removes the container/host asymmetry noted in H6.

### Disruptive (reboot / kernel parameter / BIOS / firmware) - separate window

**E11 - Enable HuC on pve5 and test LP VBR**
- Delta: `options i915 enable_guc=2` in `/etc/modprobe.d/i915.conf`, update initramfs, reboot.
- Prereqs: `tgl_huc_*.bin` and `tgl_guc_*.bin` present in `/lib/firmware/i915`; confirm after reboot with `huc_info` / dmesg ("authenticated").
- Test: `-low_power 1` with production `-b:v/-maxrate/-bufsize`.
- Tradeoffs: kernel flags this parameter as unsafe and taints the kernel; it is a non-default configuration on TGL. VDEnc quality per bit vs the `Es` path must be measured (E9), not assumed. Check that Plex on the same host still behaves.
- Rollback: remove the modprobe line, update initramfs, reboot.

**E13 - Platform profile and power limits** (only if E0 shows GPU throttle reasons asserted)
- One change at a time: platform profile balanced vs performance with everything else fixed. No raw PL overrides.

---

## D. Run these three first

1. **E0 with engine utilization and full throttle reasons.** Zero change to the system beyond installing `intel-gpu-tools`. Tells you directly whether you are CPU-bound, encode-engine bound or EU bound, and what caps pve5's GPU at 1100 MHz.
2. **E1 + E2 (front-end ceilings).** Two short runs per host. Quantifies how much of Base is decode vs conversion/upload vs encode.
3. **E4 (GPU-resident pipeline on a non-looped 100 s sample), then E6 inside it.** First shows whether removing the CPU path unlocks the newer GPU; second shows whether VDEnc has headroom worth enabling HuC for.

---

## E. Sustained-test design

- **Input:** a local, non-looped video-only sample of >=20 minutes (stream copy from the same source), so both SW-decode and HW-decode variants can run without `-stream_loop`.
- **Duration:** 20 minutes of encode per run. Discard nothing; report first 60 s and steady state (minute 5 to end) separately.
- **Logging:** `-progress pipe:1` parsed to fps per 10 s window; telemetry every 1 s (CPU per-core MHz and max-freq, GPU act/cur, throttle reasons, package temp, RAPL energy counters to compute average package watts, fan RPM).
- **Order:** interleave hosts and conditions (A B A B), never all runs of one condition back to back. 5 runs per condition.
- **Controls:** same harness on both hosts (offline chroot); other guests idle and GPU engines at 0% busy for 60 s before start; 5-minute cooldown between runs with start temperature recorded and within a fixed band; SQZARR queue drained and restored.
- **Report:** median and IQR of steady-state fps, fps-vs-time curve per run, joules per frame, and output VMAF from one 30 s scratch-output segment per condition.
- **Pass criterion for "thermal/power adaptation present":** steady-state fps more than ~5% below first-minute fps with a corresponding drop in GPU act freq or CPU MHz.

---

## F. Should a large single-stream speedup be expected?

**Not with the current command on 720p.** The command uses the same encoder class on both hosts (`Es`, because HuC is off on TGL by default), and the evidence points at a CPU-side, serialized front end on 2 cores as the ceiling. The 1185G7's higher single-core speed explains the ~5% edge.

**Where a real gap should appear:**
- GPU-resident pipeline (hardware decode, no `format=nv12,hwupload`, no no-op scale).
- VDEnc low-power encode on TGL (CQP now; VBR after HuC), subject to quality-per-bit checks.
- Multiple concurrent streams and higher-resolution sources.

How large that gap is should come from E4/E6/E8/E10, not from spec sheets.

---

## Section 9: documented issues for i7-1185G7 / X1 Carbon Gen 9 under Linux

- **Established:** HuC not loaded by default on TGL in i915; required for VDEnc CBR/VBR. Intel's documented workaround is `enable_guc=2`. Intel support confirmed the same on an i7-1185G7 when LP mode errored, and stated CQP works without HuC because it does not use VDEnc BRC.
- **Established (your data):** firmware-side MMIO PL1 changes with platform profile and load (8 W under load in an earlier state, 15 W now), separate from the MSR limit. This is consistent with Lenovo firmware managing the package limit, but the exact firmware logic is not documented in sources found here; treat as observed behavior, not a known bug.
- **Not found:** an Intel- or Lenovo-documented bug that specifically caps HEVC throughput on this model. Forum reports exist but none are established fixes.

---

## Sources

- intel-media-driver README (codec/platform matrix, E vs Es, HuC requirement, TGL default): https://github.com/intel/media-driver/blob/master/README.md
- intel-media-driver README (HuC default off for TGL/RKL, on from ADL): https://chromium.googlesource.com/chromiumos/third_party/libva-intel-media-driver/+/refs/heads/factory-nissa-16077.B/README.md
- i915 commit setting GuC/HuC defaults (TGL/RKL disabled): https://github.molgen.mpg.de/mariux64/linux/commit/1e58215f06d6dae93e2c312d07ca433b7e5ce80d
- i915 HuC source comments (HuC optional, may affect media power/perf): https://git.zx2c4.com/linux-dev/plain/drivers/gpu/drm/i915/gt/uc/intel_huc.c
- Intel Community, i7-1185G7 LP encode error and HuC guidance: https://community.intel.com/t5/Media-Intel-Video-Processing/Return-sts-MFX-ERR-DEVICE-FAILED-17-SynchronizeFirstTask-when/td-p/1411828
- FFmpeg patch, HEVC VAAPI low-power requirements (64x64 CTU, low-delay B): https://ffmpeg.org/pipermail/ffmpeg-devel/2020-March/258009.html
- FFmpeg patch notes, HEVC VDEnc supported from Ice Lake: https://lists.ffmpeg.org/pipermail/ffmpeg-devel/2018-November/236002.html
- FFmpeg vaapi_encode.h (low_power, async_depth, RC modes): https://ffmpeg.org/doxygen/7.0/vaapi__encode_8h.html

Limitations: driver matrix rows are per platform family, not per SKU. FFmpeg option defaults vary by version; confirm with `-h` on the actual binaries. Hardware specs not cited above (EU counts, GPU max clocks) are from general knowledge and should be confirmed against Intel ARK.