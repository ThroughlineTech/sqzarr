# Agent handoff: isolate SQZARR transcoding bottlenecks

Implement the investigation below and return a report backed by measurements.
Do not stop at a proposed plan. Complete the authorized low-risk tests, preserve
production state, and document anything blocked or inconclusive.

## Objective

Determine why pve5's i7-1185G7 delivers only slightly higher single-stream
HEVC throughput than pve2's i5-8350U. Identify whether the current bottleneck
is software decode/conversion/upload, synchronization, encoder hardware,
power/thermal policy, or competing workloads. Test whether GPU-resident
decoding and low-power encoding offer useful improvements without changing
the current host power configuration.

Read these repository files first:

- `AGENTS.md` if present and `WORKFLOW.md`.
- `docs/transcode-research-claude.md`: research hypotheses, not measured facts.
- `docs/container-deployment.md`: operational history, latest settings/results.
- This prompt in full.
- Existing ignored scripts in `dist/`, especially
  `benchmark-pve2-thermal.py`, `run-pve2-thermal.sh`,
  `benchmark-performance-profile.py`, `repeat-pve5-benchmark.py`,
  `prepare-repeat-sample.sh`, and `run-repeat-pve5.sh`.

The workspace may contain earlier uncommitted application and documentation
changes. Preserve them. Do not commit, push, or deploy application changes.
Use explicit Git Bash on Windows:
`C:\Program Files\Git\bin\bash.exe`, not an ambiguous `bash.exe`.

## Access and production constraints

- SSH as root to `pve2.throughlinetech.net` and `pve5.throughlinetech.net`.
  SSH from pve2 to `pve5` has also worked.
- CT260 is the old SQZARR container on pve2. KEEP IT STOPPED.
- CT261 on pve5 is the active SQZARR instance. Both configurations use the
  same production IP; never start CT260 alongside CT261.
- CT261 API: from inside the container,
  `http://127.0.0.1:8080/api/v1`.
- Capture the original queue pause state, pause/drain before tests, and
  restore that exact state in a finally/trap path. Do not kill active media jobs.
- Confirm no SQZARR jobs remain running before measurements.
- Other guests and Plex share these hosts. Do not shut them down or stop
  their services. Measure/report contention and wait for idle periods where
  practical. Do not claim a fully isolated host if it is not.
- CT261 has 2 logical CPUs, 512 MiB RAM and 512 MiB swap. CPU pinning `2,3`
  selects separate physical cores on these hosts; verify current topology.
- GPU: `/dev/dri/renderD128`, with `LIBVA_DRIVER_NAME=iHD`.
- Production service: `sqzarr`; binary `/usr/local/bin/sqzarr`.
- Config contains credentials: `/etc/sqzarr/sqzarr.toml`. Never print or copy
  the full config into logs/repository/report. Read only specific needed fields.
- pve2 source media resides under
  `/storePool/subvol-250-disk-0/srv/storage/Videos`.
  Its CT260 bind path is `/media/Videos`.
- pve5 normally accesses media through NFS at
  `/mnt/pve/fileserver-storage/Videos`, bound into CT261 as `/media/Videos`.
- Use local scratch inputs and null outputs for performance tests. Never
  overwrite, rename, delete, or transcode production media in place.
- Build any required custom binaries locally and push them. Do not compile
  inside media containers; SQLite compilation previously caused an OOM.
- Store raw operational logs in ignored `dist/` or host scratch directories.
  Keep credentials, databases, full environment dumps and private configs out
  of tracked files. Reports should contain sanitized metrics and commands.

## Authorized scope and boundaries

Authorized: read-only inventories, temporary benchmark scripts and scratch
files, queue pause/drain/restore, offline CT260 userspace tests, and FFmpeg
experiments described below. Inspect installed tools before adding anything.
Installing missing telemetry-only distro packages is permitted after an apt
simulation verifies no removals, unrelated upgrades, networking changes, or
service disruption. Record installed packages; if those conditions cannot be
met, report the limitation and continue with available tools.

Do NOT change BIOS, CPU governors, cpufreqd/TLP settings, platform profile,
power limits, GPU frequency limits, kernel arguments, firmware, container CPU
or memory configuration, or production SQZARR encoding defaults. Do not reboot.
Do not enable HuC/GuC in this phase. Do not disable thermal protections.
Do not repair the old auto-cpufreq installation or alter pve3.

If evidence supports a disruptive follow-up, give a concrete proposed change,
prerequisites, risks, validation and rollback in the report; do not apply it.

## Current reference state

Latest verified kernel on pve2/pve3/pve5: `7.0.14-25-pve`.
Recheck rather than assume it is unchanged.

Both benchmark hosts use:

- `intel_pstate=disable`, driver `acpi-cpufreq`, turbo enabled.
- cpufreqd 2.4.2-3, cpufrequtils/libcpufreq0 008-2, TLP 1.8.0-1.
- Matching `/etc/cpufreqd.conf`, `/etc/default/cpufrequtils`, `/etc/tlp.conf`.
- Live governor `performance`; energy-performance bias 4.
- cpufreqd polls every 2 seconds, selects 100% min/max on AC, and contains
  a hot-CPU rule selecting an 80% profile at 55–100°C and 50–100% CPU.
  Whether the rule actually triggers must be measured.
- TLP and cpufrequtils request `conservative`; the observed settled governor
  is `performance`. Observe policy changes instead of assuming precedence.

pve5 is a ThinkPad X1 Carbon Gen 9, 20XW004NUS, i7-1185G7.
Previously read BIOS: N32ETA1W 1.77 dated 2026-02-18.
Its custom platform-performance service is disabled; firmware profile
currently reads `balanced`, independently of CPU governor `performance`.
Keep that configuration intact.

Latest three-run results:

| Host | Speeds | Mean | Peak temperatures | Package thermal-counter deltas |
|------|--------|------|-------------------|--------------------------------|
| pve2 | 6.39 / 6.51 / 6.38x | 6.43x | 80 / 81 / 75°C | 119 / 355 / 0 |
| pve5 | 7.07 / 6.58 / 6.69x | 6.78x | 87 / 79 / 68°C | 58 / 0 / 0 |

Both reported MMIO PL1 of 15 W and GPU clock peaks around 1100 MHz.
pve5 GPU PL1 flags were clear except a brief assertion in run 3.
Older pve2 hardware lacks some throttle-reason attributes.
Thermal counters are host-wide, not exclusive to the benchmark.
pve2 fan readings included 0/65535 and cannot all be treated as valid RPM.

Historical findings to interpret carefully:

- Capping pve5 CPU at 1.2 GHz reduced throughput to about 3.8x with
  approximately 177% ffmpeg CPU usage and no GPU PL1 assertion.
  This proves CPU sensitivity at low clocks, NOT CPU saturation at full boost.
- Restoring CPU boost produced about 7.07x, sometimes with thermal events.
- Low-power HEVC bitrate encoding previously failed; CQP appeared available
  while HuC was disabled. Recheck actual capabilities and exact errors.
- Hardware decoding combined with `-stream_loop` previously produced
  filter-reinitialization errors. Failed/incomplete runs are not benchmarks.
- An accidental pve2 USB disconnect previously suspended storage pools.
  It was recovered. Check pool/NFS health before starting.

## Baseline and sample

Source:
`/media/Videos/Just Downloaded/michael.che.shot.in.minnesota.2026.720p.web.h264-jff.mkv`

It is approximately 59 minutes, 1280x720 H.264 at 23.976 fps.
Historical tests stream-copied a 40-second video-only local sample and looped
it for 90 seconds of output. Successful runs produced 2152 frames and
89.964875 seconds.

Historical encoder command:

```sh
LIBVA_DRIVER_NAME=iHD ffmpeg \
  -v error -nostdin \
  -init_hw_device vaapi=va:/dev/dri/renderD128 \
  -filter_hw_device va \
  -stream_loop -1 -i /var/tmp/sqzarr-perf-sample.mkv \
  -t 90 \
  -vf 'format=nv12,hwupload,scale_vaapi=w=min(1920\,iw):h=-2' \
  -c:v hevc_vaapi \
  -b:v 2300k -maxrate 4000k -bufsize 4600k \
  -an -sn -progress pipe:1 -f null -
```

For the new comparisons, prepare ONE video-only, non-looped local sample of
at least 100 seconds from that source and copy identical bytes to both hosts.
Verify checksums. Run the software baseline again on that sample without
`-stream_loop`; compare new variants against this baseline, not exclusively
against the historical looped test. Preserve identical frame range and
duration across each paired comparison.

Use CT260's existing userspace offline on pve2, as the existing script does:
private mount namespace, read-only media bind, taskset CPUs 2,3, no service
or network startup. Unmount and clear only your own mounted state afterward.
On pve5 use `pct exec 261` under its existing cpuset. Record the harness
asymmetry; do not stop production solely to make the harness identical.

## Phase 1: inventory and measurement correctness

Collect concise, sanitized evidence from the actual test userspaces:

1. FFmpeg version/build, libva, intel-media-driver/iHD and libigdgmm packages.
2. `ffmpeg -h encoder=hevc_vaapi` options/defaults, especially async_depth,
   low_power, rc_mode, quality/compression level, GOP/B-frame/ref controls.
3. `vainfo` HEVCMain entrypoints and supported rate-control modes where
   available. Use actual FFmpeg debug initialization or a narrow VA trace
   to establish the selected entrypoint for each encode.
4. i915 binding, enable_guc value, relevant firmware logs, and actual HuC
   authentication status. `vainfo` alone does NOT establish HuC authentication
   or prove which entrypoint an individual encode selected.
5. CPU topology, live policy, container CPU quota/throttle and memory-event
   counters, host GPU clients and background load.
6. GPU configured frequency ceilings, all available throttle-reason files,
   package power limits, and platform profile.

Use bounded output and explicit timeouts. Do not dump all kernel logs,
process arguments or application configs where credentials could appear.

For each timed run record:

- Exact command, environment, host/kernel/userspace versions, sample hash.
- Elapsed wall time, output duration, frame count, fps/speed, exit code.
- FFmpeg CPU time and per-thread CPU utilization.
- Per-engine GPU busy percentage (Video, VideoEnhance, Render/3D) where
  supported, preferably `intel_gpu_top` JSON.
- CPU policy min/max/governor on CPUs 2,3; effective frequency using
  APERF/MPERF tooling where available. Label weaker frequency snapshots.
- GPU actual/requested clocks and all available throttle flags.
- Package temperature, thermal-counter deltas, valid fan readings.
- RAPL energy deltas if available, including wrap handling.
- Container CPU-throttle and memory-event deltas.

Identify package ownership of missing tools for this distro; do not blindly
assume turbostat comes from a particular package. Validate telemetry output
before relying on it. GPU clock readings are not media-engine utilization.

Ensure the harness drains stdout/stderr while the child runs; polling a
process with unread PIPEs can deadlock once progress/debug output fills them.
Timeouts must terminate the actual benchmark process, not merely its local
SSH client. Avoid accumulating stuck benchmark jobs.

## Phase 2: controlled experiments

Run each successful condition at least three times on each applicable host.
Use paired/interleaved order where practical, record starting temperatures,
and allow comparable cooldown. Keep workload, CPU allocation, RC settings,
output duration, and telemetry overhead fixed unless explicitly changed.
Record all failures and exclude them from speed comparisons.

### E0: instrumented baseline

Use the non-looped sample and historical software-decode filter chain.
Use an FFmpeg log level that actually emits `-benchmark` results; `-v error`
can suppress the information you intend to collect.
Confirm CPU-side work and actual encoder entrypoint. Classify utilization,
but do not equate low aggregate GPU busy with proof of a specific CPU bottleneck.

### E1: software decode only

Decode the same range to null, without hardware upload or HEVC encoding.
Compare its ceiling with E0. Ensure FFmpeg really decodes frames rather than
stream-copying packets.

### E2: decode, convert and upload without HEVC encoding

Use `format=nv12,hwupload` with a supported hardware-frame/null sink.
Validate that the sink does not introduce an implicit conversion.
If unsupported, a download-and-null fallback may be used, but label its
additional work and do not call it a direct upload-cost measurement.

Do not subtract stage timings as if pipelined stages were strictly additive.
Use these results to bound costs and formulate hypotheses.

### E3: remove the scaling filter

Run E0 with `format=nv12,hwupload`, removing only `scale_vaapi`.
The source already fits the maximum dimensions. Check output dimensions,
pixel format, color metadata and decoded frame correspondence; do not assume
the existing filter necessarily incurs a hardware pass or is pixel-neutral.

### E4: GPU-resident hardware decode and encode

Use the identical non-looped sample with VAAPI H.264 hardware decoding and
hardware output surfaces. Preserve the same scale and encoder settings first.
Verify no unintended software download/conversion occurs.

Illustrative command to adapt to the installed FFmpeg:

```sh
LIBVA_DRIVER_NAME=iHD ffmpeg \
  -nostdin -benchmark \
  -hwaccel vaapi -hwaccel_device /dev/dri/renderD128 \
  -hwaccel_output_format vaapi \
  -i /var/tmp/sqzarr-investigation-sample.mkv -t 90 \
  -vf 'scale_vaapi=w=min(1920\,iw):h=-2' \
  -c:v hevc_vaapi \
  -b:v 2300k -maxrate 4000k -bufsize 4600k \
  -an -sn -progress pipe:1 -f null -
```

Use matching CPU affinity/container constraints. Compare against E0 on the
same non-looped file. If it fails, diagnose the failure without changing
host firmware/power policy. Report the failed command and concise error.

### E5: async-depth sweep

If supported, test explicit depths 1, 4, 8 against the measured default.
Keep everything else fixed; test both baseline and GPU-resident pipelines
if runtime permits. Check memory consumption against CT261's limit.
Distinguish overlap/latency improvements from output-setting changes.

### E6: standard versus low-power CQP encoding

On pve5, within the working GPU-resident pipeline, compare explicit
`-low_power 0 -rc_mode CQP -qp 26` with
`-low_power 1 -rc_mode CQP -qp 26`.
Remove `-b:v`, `-maxrate`, and `-bufsize` for BOTH CQP tests.
Hold supported GOP/quality settings fixed and document any necessary
capability differences. Confirm the selected entrypoints.

Run the standard CQP condition on pve2 for reference if supported.
Do not assume pve2 supports HEVC low-power encoding.
Do not enable HuC if the low-power test fails; report the capability/error.
Even equal QP does not guarantee equal bitrate or quality between paths.
This is a capability/performance experiment, not a production-equivalent gain.

## Phase 3: correctness and useful gains

For baseline and promising variants, save a short scratch output and verify:

- Decode completes with no errors.
- Frame count/duration, resolution, pixel format, timestamps and color metadata.
- Output size/bitrate, GOP/frame-type structure.
- SSIM and VMAF if available, evaluated against the same aligned source range.
  Do not compile quality-analysis tools on the media containers.

Treat bitrate, quality and speed together. Do not recommend a faster path as
equivalent if it achieved its speed by doing less work or reducing quality.
Do not promise bit-identical hardware/software output without validation.

## Phase 4: bounded sustained follow-up

If a variant shows a useful validated gain, compare baseline and that variant
on both hosts under a longer workload. Aim for at least 5 minutes of actual
encoding wall time per run and report speed/temperature over time. Use the
existing full-length source copied locally if space permits, or another
validated continuous-input strategy without the known looping/reinit issue.
Bound disk usage and clean up only your scratch files.

Source duration is NOT wall-clock load duration: a 20-minute clip at 7x
finishes in under 3 minutes. A 20-minute wall-clock trial would require about
140 minutes of source at that speed. Never use `-re` to manufacture sustained
load; it throttles input and invalidates throughput testing.

If input length or resources prevent a valid sustained test, state that
limitation explicitly instead of claiming sustained performance. Do not
expand into an hours-long exhaustive matrix without a clear reason.

## Interpretation rules

- The 1.2 GHz result proves sensitivity at low CPU clocks, not a present
  CPU bottleneck at full boost.
- More execution units do not imply proportional HEVC speedup.
- HuC authentication, supported entrypoints, and selected entrypoint are
  three different facts. Measure them separately.
- Same nominal bitrate/QP does not prove equivalent quality.
- Same package power limit does not establish identical actual power usage.
- Thermal counters and power measurements include other host workloads.
- A driver/profile change cannot be credited for an improvement when other
  variables changed simultaneously.
- Removing a bottleneck may reveal another; label mixed/uncertain outcomes.
- Verify version-specific technical claims against primary sources as needed.

## Report and handoff

Write `docs/transcode-investigation-results.md` containing:

1. Findings first: what limits each host, with confidence and evidence.
2. A concise inventory and differences between the test environments.
3. Results table: host, condition, runs, median throughput, variability,
   CPU/GPU utilization, peak temperatures, thermal/power flags, quality.
4. Exact reproducible commands and sample/measurement controls.
5. Supported/rejected/unresolved hypotheses, including contradictory evidence.
6. Failed experiments and why their numbers were excluded.
7. Recommendation for SQZARR: specific pipeline/options change, expected
   benefit demonstrated here, capability detection, fallback behavior, and
   quality/correctness requirements. Propose application changes; do not deploy.
8. Whether enabling HuC merits a later approved maintenance window, with
   a version-appropriate proposal and rollback only if evidence supports it.
9. Operational closure: queue state restored, scratch cleanup, CT260 stopped,
   mount cleanup, services healthy, packages installed, no leaked secrets.
10. Raw-log locations and remaining limitations.

Preserve prior documentation/history; link to this report rather than
rewriting old measurements. Check changed files for whitespace errors and
credentials before finishing. Return a short user-facing summary with the
best validated result, its quality tradeoff if any, and the next decision.
